package mesh

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// DockingHandler is called when a vacuum enters the 'docked' state
type DockingHandler func(vacuumID string)

// MQTTClient manages MQTT connection and subscriptions for vacuum map data
type MQTTClient struct {
	client         mqtt.Client
	config         *Config
	messageHandler MessageHandler
	dockingHandler DockingHandler
	isConnected    bool
	mu             sync.RWMutex
}

// MessageHandler is called when a map data message is received
// Parameters: vacuumID, rawPayload, mapData, error
// rawPayload is provided so callers can handle raw PNG images that lack zTXt metadata
type MessageHandler func(vacuumID string, rawPayload []byte, mapData *ValetudoMap, err error)

var (
	globalClient *MQTTClient
	clientMu     sync.Mutex
)

// InitMQTT initializes the global MQTT client with the provided configuration
// If MQTT_BROKER env var is empty, MQTT is disabled and this returns nil
func InitMQTT(config *Config, handler MessageHandler) (*MQTTClient, error) {
	clientMu.Lock()
	defer clientMu.Unlock()

	// Check if MQTT is enabled via env var or config
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" && config != nil && config.MQTT.Broker != "" {
		broker = config.MQTT.Broker
	}

	if broker == "" {
		log.Println("MQTT disabled: MQTT_BROKER not set")
		return nil, nil
	}

	if config == nil || len(config.Vacuums) == 0 {
		return nil, fmt.Errorf("MQTT enabled but no vacuum configuration provided")
	}

	client := &MQTTClient{
		config:         config,
		messageHandler: handler,
	}

	// Build MQTT client options
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)

	// Client ID
	clientID := os.Getenv("MQTT_CLIENT_ID")
	if clientID == "" && config.MQTT.ClientID != "" {
		clientID = config.MQTT.ClientID
	}
	if clientID == "" {
		clientID = "tudomesh"
	}
	opts.SetClientID(clientID)

	// Authentication
	username := os.Getenv("MQTT_USERNAME")
	if username == "" && config.MQTT.Username != "" {
		username = config.MQTT.Username
	}
	if username != "" {
		opts.SetUsername(username)
		password := os.Getenv("MQTT_PASSWORD")
		if password == "" && config.MQTT.Password != "" {
			password = config.MQTT.Password
		}
		opts.SetPassword(password)
	}

	// Connection settings
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(60 * time.Second)
	opts.SetKeepAlive(60 * time.Second)   // Longer than default 30s to reduce spurious disconnects
	opts.SetPingTimeout(10 * time.Second) // Timeout for ping response
	opts.SetCleanSession(false)           // Preserve subscriptions on reconnect
	opts.SetOrderMatters(false)           // Allow concurrent processing

	// Callbacks
	opts.SetOnConnectHandler(client.onConnect)
	opts.SetConnectionLostHandler(client.onConnectionLost)
	opts.SetReconnectingHandler(client.onReconnecting)

	client.client = mqtt.NewClient(opts)

	// Connect asynchronously with retry
	go client.connectWithRetry()

	globalClient = client
	return client, nil
}

// GetMQTTClient returns the global MQTT client instance
func GetMQTTClient() *MQTTClient {
	clientMu.Lock()
	defer clientMu.Unlock()
	return globalClient
}

// connectWithRetry attempts to connect to the MQTT broker with exponential backoff
func (c *MQTTClient) connectWithRetry() {
	retryDelay := 1 * time.Second
	maxRetryDelay := 60 * time.Second

	for {
		log.Println("Connecting to MQTT broker...")

		token := c.client.Connect()
		if token.WaitTimeout(10 * time.Second) {
			if token.Error() == nil {
				log.Println("Successfully connected to MQTT broker")
				c.setConnected(true)
				return
			}
			log.Printf("MQTT connection failed: %v", token.Error())
		} else {
			log.Println("MQTT connection timeout")
		}

		// Exponential backoff
		log.Printf("Retrying MQTT connection in %v...", retryDelay)
		time.Sleep(retryDelay)
		retryDelay *= 2
		if retryDelay > maxRetryDelay {
			retryDelay = maxRetryDelay
		}
	}
}

// onConnect is called when the MQTT connection is established
func (c *MQTTClient) onConnect(client mqtt.Client) {
	log.Println("MQTT connected, subscribing to vacuum topics...")
	c.setConnected(true)

	// Subscribe to all vacuum topics from config
	for _, vacuum := range c.config.Vacuums {
		if vacuum.Topic == "" {
			log.Printf("Warning: vacuum %s has no topic configured", vacuum.ID)
			continue
		}

		log.Printf("Subscribing to %s for vacuum %s", vacuum.Topic, vacuum.ID)
		token := client.Subscribe(vacuum.Topic, 0, c.createMessageHandler(vacuum.ID))

		if token.WaitTimeout(5*time.Second) && token.Error() != nil {
			log.Printf("Error subscribing to %s: %v", vacuum.Topic, token.Error())
		} else {
			log.Printf("Successfully subscribed to %s", vacuum.Topic)
		}

		// Subscribe to state topic for docking detection
		if stateTopic, ok := deriveStateTopic(vacuum.Topic); ok {
			log.Printf("Subscribing to %s for vacuum %s state", stateTopic, vacuum.ID)
			stateToken := client.Subscribe(stateTopic, 0, c.createStateMessageHandler(vacuum.ID))

			if stateToken.WaitTimeout(5*time.Second) && stateToken.Error() != nil {
				log.Printf("Error subscribing to %s: %v", stateTopic, stateToken.Error())
			} else {
				log.Printf("Successfully subscribed to %s", stateTopic)
			}
		}
	}
}

// onConnectionLost is called when the MQTT connection is lost
// Auto-reconnect is enabled, so this is typically a transient event
func (c *MQTTClient) onConnectionLost(client mqtt.Client, err error) {
	log.Printf("MQTT connection interrupted (%v), auto-reconnect will retry", err)
	c.setConnected(false)
}

// onReconnecting is called when the client attempts to reconnect
func (c *MQTTClient) onReconnecting(client mqtt.Client, opts *mqtt.ClientOptions) {
	log.Println("MQTT reconnecting...")
}

// createMessageHandler creates a handler function for a specific vacuum's topic
func (c *MQTTClient) createMessageHandler(vacuumID string) mqtt.MessageHandler {
	return func(client mqtt.Client, msg mqtt.Message) {
		payload := msg.Payload()
		log.Printf("Received map data for %s (topic: %s, size: %d bytes)",
			vacuumID, msg.Topic(), len(payload))

		// Decode the map data (handles PNG with zTXt, raw JSON, or compressed JSON)
		mapData, err := DecodeMapData(payload)
		if err != nil {
			log.Printf("Error decoding map data for %s: %v", vacuumID, err)
			if c.messageHandler != nil {
				// Pass raw payload so caller can handle raw PNGs
				c.messageHandler(vacuumID, payload, nil, err)
			}
			return
		}

		// Call the user's message handler with raw payload and decoded data
		if c.messageHandler != nil {
			c.messageHandler(vacuumID, payload, mapData, nil)
		}
	}
}

// SetDockingHandler registers a callback that is invoked when a vacuum docks
func (c *MQTTClient) SetDockingHandler(handler DockingHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dockingHandler = handler
}

// getDockingHandler returns the current docking handler in a thread-safe manner
func (c *MQTTClient) getDockingHandler() DockingHandler {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dockingHandler
}

// deriveStateTopic converts a map data topic to a state topic.
// Example: "valetudo/rocky7/MapData/map-data" -> "valetudo/rocky7/StatusStateAttribute/status"
// Returns the derived topic and true if the conversion succeeded, or empty string and false otherwise.
func deriveStateTopic(mapDataTopic string) (string, bool) {
	// Expected format: valetudo/{name}/MapData/map-data
	parts := strings.Split(mapDataTopic, "/")
	if len(parts) < 4 {
		return "", false
	}
	// Replace the last two segments with StatusStateAttribute/status
	parts[len(parts)-2] = "StatusStateAttribute"
	parts[len(parts)-1] = "status"
	return strings.Join(parts, "/"), true
}

// statePayload represents the JSON structure of a Valetudo state message
type statePayload struct {
	Value string `json:"value"`
}

// createStateMessageHandler creates a handler for state topic messages that
// detects docking events and invokes the docking handler
func (c *MQTTClient) createStateMessageHandler(vacuumID string) mqtt.MessageHandler {
	return func(client mqtt.Client, msg mqtt.Message) {
		payload := msg.Payload()
		log.Printf("Received state update for %s (topic: %s, size: %d bytes)",
			vacuumID, msg.Topic(), len(payload))

		var stateValue string

		// Try parsing as JSON object {"value": "..."}
		var state statePayload
		if err := json.Unmarshal(payload, &state); err == nil {
			stateValue = state.Value
		} else {
			// Try parsing as JSON string "docked"
			var plainStr string
			if err2 := json.Unmarshal(payload, &plainStr); err2 == nil {
				stateValue = plainStr
				log.Printf("State payload for %s is JSON string (not object), value: %s", vacuumID, plainStr)
			} else {
				// Use raw string with whitespace trimmed
				stateValue = strings.TrimSpace(string(payload))
				if stateValue == "" {
					log.Printf("Empty state payload for %s, skipping", vacuumID)
					return
				}
				log.Printf("State payload for %s is raw string (not JSON), value: %s", vacuumID, stateValue)
			}
		}

		log.Printf("Vacuum %s state: %s", vacuumID, stateValue)

		if stateValue == "docked" {
			handler := c.getDockingHandler()
			if handler != nil {
				handler(vacuumID)
			}
		}
	}
}

// IsConnected returns true if the MQTT client is connected
func (c *MQTTClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isConnected
}

// setConnected updates the connection status
func (c *MQTTClient) setConnected(connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isConnected = connected
}

// Disconnect gracefully closes the MQTT connection
func (c *MQTTClient) Disconnect() {
	if c.client != nil && c.client.IsConnected() {
		log.Println("Disconnecting from MQTT broker...")
		c.client.Disconnect(250) // 250ms quiesce time
		c.setConnected(false)
	}
}

// GetVacuumByTopic returns the vacuum ID for a given topic
func (c *MQTTClient) GetVacuumByTopic(topic string) (string, bool) {
	for _, vacuum := range c.config.Vacuums {
		if vacuum.Topic == topic {
			return vacuum.ID, true
		}
	}
	return "", false
}

// GetClient returns the underlying MQTT client for publishing
func (c *MQTTClient) GetClient() mqtt.Client {
	return c.client
}

// newMQTTClientWithMock creates an MQTTClient with a provided mqtt.Client
// This is used for testing with mock clients
func newMQTTClientWithMock(client mqtt.Client, config *Config, handler MessageHandler) *MQTTClient {
	return &MQTTClient{
		client:         client,
		config:         config,
		messageHandler: handler,
	}
}
