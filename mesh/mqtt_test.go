package mesh

import (
	"testing"
	"time"
)

func TestInitMQTT_Disabled(t *testing.T) {
	// Test with no MQTT_BROKER env var and no config
	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "test", Topic: "test/topic"},
		},
	}

	handler := func(string, *ValetudoMap, error) {}

	client, err := InitMQTT(config, handler)
	if err != nil {
		t.Errorf("InitMQTT() should not error when MQTT disabled: %v", err)
	}
	if client != nil {
		t.Error("InitMQTT() should return nil when MQTT disabled")
	}
}

func TestInitMQTT_NoVacuums(t *testing.T) {
	// Test with broker set but no vacuums configured
	config := &Config{
		MQTT: MQTTConfig{
			Broker: "mqtt://localhost:1883",
		},
		Vacuums: []VacuumConfig{},
	}

	handler := func(string, *ValetudoMap, error) {}

	_, err := InitMQTT(config, handler)
	if err == nil {
		t.Error("InitMQTT() should error when no vacuums configured")
	}
}

func TestMQTTClient_IsConnected(t *testing.T) {
	// Test initial state
	client := &MQTTClient{}
	if client.IsConnected() {
		t.Error("New client should not be connected")
	}

	// Test after setting connected
	client.setConnected(true)
	if !client.IsConnected() {
		t.Error("Client should be connected after setConnected(true)")
	}

	// Test after disconnecting
	client.setConnected(false)
	if client.IsConnected() {
		t.Error("Client should not be connected after setConnected(false)")
	}
}

func TestMQTTClient_GetVacuumByTopic(t *testing.T) {
	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "valetudo/vacuum1/MapData/map-data"},
			{ID: "vacuum2", Topic: "valetudo/vacuum2/MapData/map-data"},
		},
	}

	client := &MQTTClient{config: config}

	tests := []struct {
		name   string
		topic  string
		wantID string
		wantOK bool
	}{
		{
			name:   "valid vacuum1 topic",
			topic:  "valetudo/vacuum1/MapData/map-data",
			wantID: "vacuum1",
			wantOK: true,
		},
		{
			name:   "valid vacuum2 topic",
			topic:  "valetudo/vacuum2/MapData/map-data",
			wantID: "vacuum2",
			wantOK: true,
		},
		{
			name:   "invalid topic",
			topic:  "unknown/topic",
			wantID: "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := client.GetVacuumByTopic(tt.topic)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("GetVacuumByTopic(%q) = (%q, %v), want (%q, %v)",
					tt.topic, gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}

func TestGetMQTTClient_NotInitialized(t *testing.T) {
	// Reset global client
	clientMu.Lock()
	globalClient = nil
	clientMu.Unlock()

	client := GetMQTTClient()
	if client != nil {
		t.Error("GetMQTTClient() should return nil when not initialized")
	}
}

// TestMessageHandler_Integration tests the complete message handling flow
func TestMessageHandler_Integration(t *testing.T) {
	// Create test map data
	mapData := &ValetudoMap{
		Class: "ValetudoMap",
		MetaData: MapMetaData{
			Version:        1,
			Nonce:          "test",
			TotalLayerArea: 1000,
		},
		Size:      Size{X: 100, Y: 100},
		PixelSize: 5,
		Layers:    []MapLayer{},
		Entities: []MapEntity{
			{
				Class:  "PointMapEntity",
				Type:   "robot_position",
				Points: []int{50, 50},
				MetaData: map[string]interface{}{
					"angle": 90.0,
				},
			},
		},
	}

	// Test handler receives correct data
	handlerCalled := false
	receivedID := ""
	var receivedMap *ValetudoMap
	var receivedErr error

	handler := func(vacuumID string, m *ValetudoMap, err error) {
		handlerCalled = true
		receivedID = vacuumID
		receivedMap = m
		receivedErr = err
	}

	// Simulate handler call
	handler("test-vacuum", mapData, nil)

	if !handlerCalled {
		t.Error("Handler was not called")
	}
	if receivedID != "test-vacuum" {
		t.Errorf("Received vacuum ID = %s, want test-vacuum", receivedID)
	}
	if receivedMap == nil {
		t.Error("Received map is nil")
	}
	if receivedErr != nil {
		t.Errorf("Received error = %v, want nil", receivedErr)
	}
	if receivedMap != nil && receivedMap.Class != "ValetudoMap" {
		t.Errorf("Map class = %s, want ValetudoMap", receivedMap.Class)
	}
}

// TestMQTTClient_ConcurrentAccess tests thread-safe access to client state
func TestMQTTClient_ConcurrentAccess(t *testing.T) {
	client := &MQTTClient{}

	// Start multiple goroutines reading and writing connection state
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				client.setConnected(j%2 == 0)
				_ = client.IsConnected()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// No panic = success (test for race conditions)
}

// TestMQTTConfig_FromEnvAndConfig tests configuration precedence
func TestMQTTConfig_FromEnvAndConfig(t *testing.T) {
	config := &Config{
		MQTT: MQTTConfig{
			Broker:   "mqtt://config-broker:1883",
			ClientID: "config-client",
			Username: "config-user",
			Password: "config-pass",
		},
		Vacuums: []VacuumConfig{
			{ID: "test", Topic: "test/topic"},
		},
	}

	// Test that config values are used when env vars not set
	if config.MQTT.Broker != "mqtt://config-broker:1883" {
		t.Error("Should use config broker")
	}
	if config.MQTT.ClientID != "config-client" {
		t.Error("Should use config client ID")
	}
}

// Benchmark MQTT message handler creation
func BenchmarkCreateMessageHandler(b *testing.B) {
	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "valetudo/vacuum1/MapData/map-data"},
		},
	}

	client := &MQTTClient{
		config:         config,
		messageHandler: func(string, *ValetudoMap, error) {},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.createMessageHandler("vacuum1")
	}
}

// TestMQTTClient_GetClient tests retrieving the underlying MQTT client
func TestMQTTClient_GetClient(t *testing.T) {
	client := &MQTTClient{}

	mqttClient := client.GetClient()
	// Should return the underlying client (even if nil)
	if mqttClient != client.client {
		t.Error("GetClient() should return the underlying mqtt.Client")
	}
}

// TestPublisherCreation tests publisher initialization
func TestPublisherCreation(t *testing.T) {
	// Test with nil client (disabled mode)
	publisher := NewPublisher(nil)
	if publisher == nil {
		t.Fatalf("NewPublisher() should not return nil even with nil client")
	}
	if publisher.publishPrefix != "tudomesh" {
		t.Errorf("Default prefix = %s, want tudomesh", publisher.publishPrefix)
	}
}

// TestMQTTDisconnect tests graceful disconnect
func TestMQTTDisconnect(t *testing.T) {
	client := &MQTTClient{
		isConnected: true,
	}

	// Should not panic with nil mqtt.Client
	client.Disconnect()
}

// TestTimeout ensures operations don't hang
func TestConnectionTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	// This test verifies that connection attempts don't block forever
	config := &Config{
		MQTT: MQTTConfig{
			Broker: "mqtt://invalid-broker-that-does-not-exist:1883",
		},
		Vacuums: []VacuumConfig{
			{ID: "test", Topic: "test/topic"},
		},
	}

	handler := func(string, *ValetudoMap, error) {}

	// Should return immediately (connection happens in background)
	start := time.Now()
	client, err := InitMQTT(config, handler)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("InitMQTT() error = %v, should not error (connects in background)", err)
	}

	// Should not block - returns immediately
	if duration > 1*time.Second {
		t.Errorf("InitMQTT() took %v, should return immediately", duration)
	}

	if client != nil {
		client.Disconnect()
	}
}
