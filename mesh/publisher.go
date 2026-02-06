package mesh

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Publisher manages publishing transformed vacuum positions to MQTT
type Publisher struct {
	client        mqtt.Client
	publishPrefix string
	qos           byte
	retain        bool
	positions     map[string]*VacuumPosition
	mu            sync.RWMutex
}

// NewPublisher creates a new position publisher
// If client is nil, publishing is disabled (for testing)
func NewPublisher(client mqtt.Client) *Publisher {
	prefix := os.Getenv("MQTT_PUBLISH_PREFIX")
	if prefix == "" {
		prefix = "tudomesh"
	}

	return &Publisher{
		client:        client,
		publishPrefix: prefix,
		qos:           0,    // QoS 0 for position updates (fire and forget)
		retain:        true, // Retain for latest position
		positions:     make(map[string]*VacuumPosition),
	}
}

// PublishPosition publishes a single vacuum's transformed position to MQTT
// Publishes to both individual topic and combined positions topic
func (p *Publisher) PublishPosition(vacuumID string, x, y, angle float64) error {
	if p.client == nil || !p.client.IsConnected() {
		return fmt.Errorf("MQTT client not connected")
	}

	position := &VacuumPosition{
		VacuumID:  vacuumID,
		X:         x,
		Y:         y,
		Angle:     angle,
		Timestamp: time.Now().Unix(),
	}

	// Store position for combined message
	p.mu.Lock()
	p.positions[vacuumID] = position
	p.mu.Unlock()

	// Publish to individual topic: tudomesh/{vacuumID}
	if err := p.publishIndividual(position); err != nil {
		log.Printf("Error publishing individual position for %s: %v", vacuumID, err)
		return err
	}

	// Publish to combined topic: tudomesh/positions
	if err := p.publishCombined(); err != nil {
		log.Printf("Error publishing combined positions: %v", err)
		return err
	}

	return nil
}

// publishIndividual publishes a single vacuum position to its individual topic
func (p *Publisher) publishIndividual(pos *VacuumPosition) error {
	topic := fmt.Sprintf("%s/%s", p.publishPrefix, pos.VacuumID)

	payload, err := json.Marshal(pos)
	if err != nil {
		return fmt.Errorf("marshaling position: %w", err)
	}

	token := p.client.Publish(topic, p.qos, p.retain, payload)
	if token.WaitTimeout(2*time.Second) && token.Error() != nil {
		return fmt.Errorf("publishing to %s: %w", topic, token.Error())
	}

	log.Printf("Published position for %s: (%.0f, %.0f) angle=%.0f°",
		pos.VacuumID, pos.X, pos.Y, pos.Angle)
	return nil
}

// publishCombined publishes all vacuum positions to the combined topic
func (p *Publisher) publishCombined() error {
	p.mu.RLock()
	positions := make([]*VacuumPosition, 0, len(p.positions))
	for _, pos := range p.positions {
		positions = append(positions, pos)
	}
	p.mu.RUnlock()

	if len(positions) == 0 {
		return nil
	}

	topic := fmt.Sprintf("%s/positions", p.publishPrefix)

	// Create combined message
	message := map[string]interface{}{
		"vacuums":   positions,
		"timestamp": time.Now().Unix(),
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshaling combined positions: %w", err)
	}

	token := p.client.Publish(topic, p.qos, p.retain, payload)
	if token.WaitTimeout(2*time.Second) && token.Error() != nil {
		return fmt.Errorf("publishing to %s: %w", topic, token.Error())
	}

	return nil
}

// GetPosition returns the last known position for a vacuum
func (p *Publisher) GetPosition(vacuumID string) (*VacuumPosition, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	pos, ok := p.positions[vacuumID]
	return pos, ok
}

// GetAllPositions returns all known vacuum positions
func (p *Publisher) GetAllPositions() map[string]*VacuumPosition {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Return a copy to avoid race conditions
	positions := make(map[string]*VacuumPosition, len(p.positions))
	for id, pos := range p.positions {
		posCopy := *pos
		positions[id] = &posCopy
	}
	return positions
}

// ClearPosition removes a vacuum's position (e.g., when offline)
func (p *Publisher) ClearPosition(vacuumID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.positions, vacuumID)
}

// SetQoS sets the Quality of Service level for publishing (0, 1, or 2)
func (p *Publisher) SetQoS(qos byte) {
	if qos <= 2 {
		p.qos = qos
	}
}

// SetRetain sets whether published messages should be retained by the broker
func (p *Publisher) SetRetain(retain bool) {
	p.retain = retain
}

// PublishTransformedPosition applies a transform and publishes the result
// This is a convenience function for the main service loop
func (p *Publisher) PublishTransformedPosition(vacuumID string, localPos Point, localAngle float64, transform AffineMatrix) error {
	// Transform position from local to world coordinates
	worldPos := TransformPoint(localPos, transform)

	// Transform angle using the rotation component of the affine matrix
	worldAngle := TransformAngle(localAngle, transform)

	return p.PublishPosition(vacuumID, worldPos.X, worldPos.Y, worldAngle)
}
