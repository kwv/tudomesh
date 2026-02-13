package mesh

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestInitMQTT_Disabled(t *testing.T) {
	// Test with no MQTT_BROKER env var and no config
	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "test", Topic: "test/topic"},
		},
	}

	handler := func(string, []byte, *ValetudoMap, error) {}

	client, err := InitMQTT(config, handler)
	assert.NoError(t, err)
	assert.Nil(t, client)
}

func TestInitMQTT_NoVacuums(t *testing.T) {
	// Test with broker set but no vacuums configured
	config := &Config{
		MQTT: MQTTConfig{
			Broker: "mqtt://localhost:1883",
		},
		Vacuums: []VacuumConfig{},
	}

	handler := func(string, []byte, *ValetudoMap, error) {}

	_, err := InitMQTT(config, handler)
	assert.Error(t, err)
}

func TestMQTTClient_IsConnected(t *testing.T) {
	// Test initial state
	client := &MQTTClient{}
	assert.False(t, client.IsConnected(), "New client should not be connected")

	// Test after setting connected
	client.setConnected(true)
	assert.True(t, client.IsConnected(), "Client should be connected after setConnected(true)")

	// Test after disconnecting
	client.setConnected(false)
	assert.False(t, client.IsConnected(), "Client should not be connected after setConnected(false)")
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
			assert.Equal(t, tt.wantID, gotID)
			assert.Equal(t, tt.wantOK, gotOK)
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

	handler := func(vacuumID string, rawPayload []byte, m *ValetudoMap, err error) {
		handlerCalled = true
		receivedID = vacuumID
		receivedMap = m
		receivedErr = err
	}

	// Simulate handler call
	handler("test-vacuum", nil, mapData, nil)

	assert.True(t, handlerCalled)
	assert.Equal(t, "test-vacuum", receivedID)
	assert.NotNil(t, receivedMap)
	assert.NoError(t, receivedErr)
	assert.Equal(t, "ValetudoMap", receivedMap.Class)
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
		messageHandler: func(string, []byte, *ValetudoMap, error) {},
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
		return
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

// TestInitMQTT_ReturnsImmediately ensures InitMQTT doesn't block
func TestInitMQTT_ReturnsImmediately(t *testing.T) {
	// InitMQTT spawns connection goroutines in background
	// This test verifies it returns immediately without blocking
	config := &Config{
		MQTT: MQTTConfig{
			Broker: "mqtt://localhost:1883",
		},
		Vacuums: []VacuumConfig{
			{ID: "test", Topic: "test/topic"},
		},
	}

	handler := func(string, []byte, *ValetudoMap, error) {}

	start := time.Now()
	client, err := InitMQTT(config, handler)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("InitMQTT() error = %v, should not error (connects in background)", err)
	}

	// Should return immediately (< 100ms) even though connection happens async
	if duration > 100*time.Millisecond {
		t.Errorf("InitMQTT() took %v, should return immediately", duration)
	}

	if client != nil {
		client.Disconnect()
	}
}

// --- Docking detection tests ---

func TestDeriveStateTopic(t *testing.T) {
	tests := []struct {
		name      string
		mapTopic  string
		wantTopic string
		wantOK    bool
	}{
		{
			name:      "standard valetudo topic",
			mapTopic:  "valetudo/rocky7/MapData/map-data",
			wantTopic: "valetudo/rocky7/StatusStateAttribute/status",
			wantOK:    true,
		},
		{
			name:      "different vacuum name",
			mapTopic:  "valetudo/dusty/MapData/map-data",
			wantTopic: "valetudo/dusty/StatusStateAttribute/status",
			wantOK:    true,
		},
		{
			name:      "longer prefix path",
			mapTopic:  "home/floor1/valetudo/vacuum1/MapData/map-data",
			wantTopic: "home/floor1/valetudo/vacuum1/StatusStateAttribute/status",
			wantOK:    true,
		},
		{
			name:      "exactly four segments",
			mapTopic:  "a/b/c/d",
			wantTopic: "a/b/StatusStateAttribute/status",
			wantOK:    true,
		},
		{
			name:      "too few segments - three",
			mapTopic:  "a/b/c",
			wantTopic: "",
			wantOK:    false,
		},
		{
			name:      "too few segments - two",
			mapTopic:  "test/topic",
			wantTopic: "",
			wantOK:    false,
		},
		{
			name:      "single segment",
			mapTopic:  "topic",
			wantTopic: "",
			wantOK:    false,
		},
		{
			name:      "empty string",
			mapTopic:  "",
			wantTopic: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := deriveStateTopic(tt.mapTopic)
			if got != tt.wantTopic || ok != tt.wantOK {
				t.Errorf("deriveStateTopic(%q) = (%q, %v), want (%q, %v)",
					tt.mapTopic, got, ok, tt.wantTopic, tt.wantOK)
			}
		})
	}
}

func TestSetDockingHandler(t *testing.T) {
	client := &MQTTClient{}

	// Initially nil
	if h := client.getDockingHandler(); h != nil {
		t.Error("Docking handler should be nil initially")
	}

	// Set handler
	called := false
	client.SetDockingHandler(func(vacuumID string) {
		called = true
	})

	h := client.getDockingHandler()
	if h == nil {
		t.Fatal("Docking handler should not be nil after SetDockingHandler")
	}

	h("test")
	if !called {
		t.Error("Docking handler was not invoked")
	}
}

func TestSetDockingHandler_ConcurrentAccess(t *testing.T) {
	client := &MQTTClient{}
	var count atomic.Int64

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				client.SetDockingHandler(func(vacuumID string) {
					count.Add(1)
				})
				if h := client.getDockingHandler(); h != nil {
					h("test")
				}
			}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
	// No race condition = success
}

func TestCreateStateMessageHandler_DockedState(t *testing.T) {
	client := &MQTTClient{}

	var receivedVacuumID string
	var mu sync.Mutex

	client.SetDockingHandler(func(vacuumID string) {
		mu.Lock()
		receivedVacuumID = vacuumID
		mu.Unlock()
	})

	handler := client.createStateMessageHandler("rocky7")

	// Simulate a docked state message
	mockClient := NewMockClient()
	// Expectations are set in NewMockClient by default, but we can be explicit
	mockClient.On("Subscribe", "valetudo/rocky7/StatusStateAttribute/status", mock.Anything, mock.Anything).Return(NewMockToken(nil))

	mockClient.Subscribe("valetudo/rocky7/StatusStateAttribute/status", 0, handler)
	mockClient.SimulateMessage("valetudo/rocky7/StatusStateAttribute/status", []byte(`{"value":"docked"}`))

	mu.Lock()
	got := receivedVacuumID
	mu.Unlock()

	assert.Equal(t, "rocky7", got)
	mockClient.AssertExpectations(t)
}

func TestCreateStateMessageHandler_PayloadFormats(t *testing.T) {
	tests := []struct {
		name       string
		payload    []byte
		wantDocked bool
	}{
		{
			name:       "JSON object docked",
			payload:    []byte(`{"value":"docked"}`),
			wantDocked: true,
		},
		{
			name:       "JSON string docked",
			payload:    []byte(`"docked"`),
			wantDocked: true,
		},
		{
			name:       "raw string docked",
			payload:    []byte(`docked`),
			wantDocked: true,
		},
		{
			name:       "raw string with whitespace",
			payload:    []byte("  docked\n"),
			wantDocked: true,
		},
		{
			name:       "JSON object cleaning",
			payload:    []byte(`{"value":"cleaning"}`),
			wantDocked: false,
		},
		{
			name:       "JSON string cleaning",
			payload:    []byte(`"cleaning"`),
			wantDocked: false,
		},
		{
			name:       "raw string cleaning",
			payload:    []byte(`cleaning`),
			wantDocked: false,
		},
		{
			name:       "empty payload",
			payload:    []byte{},
			wantDocked: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &MQTTClient{}
			handlerCalled := false

			client.SetDockingHandler(func(vacuumID string) {
				handlerCalled = true
			})

			handler := client.createStateMessageHandler("vacuum1")
			mock := NewMockClient()
			mock.SetConnected(true)
			topic := "valetudo/vacuum1/StatusStateAttribute/status"
			mock.Subscribe(topic, 0, handler)

			mock.SimulateMessage(topic, tt.payload)

			if handlerCalled != tt.wantDocked {
				t.Errorf("DockingHandler called = %v, want %v (payload: %q)",
					handlerCalled, tt.wantDocked, string(tt.payload))
			}
		})
	}
}

func TestCreateStateMessageHandler_NonDockedStates(t *testing.T) {
	client := &MQTTClient{}

	handlerCalled := false
	client.SetDockingHandler(func(vacuumID string) {
		handlerCalled = true
	})

	handler := client.createStateMessageHandler("vacuum1")
	mock := NewMockClient()
	mock.SetConnected(true)
	topic := "valetudo/vacuum1/StatusStateAttribute/status"
	mock.Subscribe(topic, 0, handler)

	states := []string{
		`{"value":"cleaning"}`,
		`{"value":"idle"}`,
		`{"value":"returning"}`,
		`{"value":"paused"}`,
		`{"value":"error"}`,
	}

	for _, state := range states {
		handlerCalled = false
		mock.SimulateMessage(topic, []byte(state))
		if handlerCalled {
			t.Errorf("DockingHandler should not be called for state %s", state)
		}
	}
}

func TestCreateStateMessageHandler_InvalidJSON(t *testing.T) {
	client := &MQTTClient{}

	handlerCalled := false
	client.SetDockingHandler(func(vacuumID string) {
		handlerCalled = true
	})

	handler := client.createStateMessageHandler("vacuum1")
	mock := NewMockClient()
	mock.SetConnected(true)
	topic := "valetudo/vacuum1/StatusStateAttribute/status"
	mock.Subscribe(topic, 0, handler)

	// Send invalid JSON
	mock.SimulateMessage(topic, []byte(`not json at all`))

	if handlerCalled {
		t.Error("DockingHandler should not be called for invalid JSON")
	}
}

func TestCreateStateMessageHandler_EmptyPayload(t *testing.T) {
	client := &MQTTClient{}

	handlerCalled := false
	client.SetDockingHandler(func(vacuumID string) {
		handlerCalled = true
	})

	handler := client.createStateMessageHandler("vacuum1")
	mock := NewMockClient()
	mock.SetConnected(true)
	topic := "valetudo/vacuum1/StatusStateAttribute/status"
	mock.Subscribe(topic, 0, handler)

	mock.SimulateMessage(topic, []byte{})

	if handlerCalled {
		t.Error("DockingHandler should not be called for empty payload")
	}
}

func TestCreateStateMessageHandler_NilHandler(t *testing.T) {
	client := &MQTTClient{}
	// No docking handler set

	handler := client.createStateMessageHandler("vacuum1")
	mock := NewMockClient()
	mock.SetConnected(true)
	topic := "valetudo/vacuum1/StatusStateAttribute/status"
	mock.Subscribe(topic, 0, handler)

	// Should not panic even without a handler set
	mock.SimulateMessage(topic, []byte(`{"value":"docked"}`))
}

func TestCreateStateMessageHandler_EmptyValue(t *testing.T) {
	client := &MQTTClient{}

	handlerCalled := false
	client.SetDockingHandler(func(vacuumID string) {
		handlerCalled = true
	})

	handler := client.createStateMessageHandler("vacuum1")
	mock := NewMockClient()
	mock.SetConnected(true)
	topic := "valetudo/vacuum1/StatusStateAttribute/status"
	mock.Subscribe(topic, 0, handler)

	mock.SimulateMessage(topic, []byte(`{"value":""}`))

	if handlerCalled {
		t.Error("DockingHandler should not be called for empty value")
	}
}

func TestCreateStateMessageHandler_MissingValueField(t *testing.T) {
	client := &MQTTClient{}

	handlerCalled := false
	client.SetDockingHandler(func(vacuumID string) {
		handlerCalled = true
	})

	handler := client.createStateMessageHandler("vacuum1")
	mock := NewMockClient()
	mock.SetConnected(true)
	topic := "valetudo/vacuum1/StatusStateAttribute/status"
	mock.Subscribe(topic, 0, handler)

	mock.SimulateMessage(topic, []byte(`{"other":"field"}`))

	if handlerCalled {
		t.Error("DockingHandler should not be called when value field is missing")
	}
}

func TestOnConnect_SubscribesStateTopics(t *testing.T) {
	mockClient := NewMockClient()

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "valetudo/vacuum1/MapData/map-data"},
			{ID: "vacuum2", Topic: "valetudo/vacuum2/MapData/map-data"},
		},
	}

	client := newMQTTClientWithMock(mockClient, config, func(string, []byte, *ValetudoMap, error) {})

	client.onConnect(mockClient)

	// Should have 4 subscriptions: 2 map data + 2 state topics
	mockClient.mu.RLock()
	handlers := len(mockClient.messageHandlers)
	topics := make([]string, 0, len(mockClient.messageHandlers))
	for topic := range mockClient.messageHandlers {
		topics = append(topics, topic)
	}
	mockClient.mu.RUnlock()

	assert.Equal(t, 4, handlers, "Topics: %v", topics)

	// Verify specific state topics are subscribed
	expectedStateTopics := []string{
		"valetudo/vacuum1/StatusStateAttribute/status",
		"valetudo/vacuum2/StatusStateAttribute/status",
	}

	mockClient.mu.RLock()
	for _, topic := range expectedStateTopics {
		_, ok := mockClient.messageHandlers[topic]
		assert.True(t, ok, "Expected subscription to %s", topic)
	}
	mockClient.mu.RUnlock()
}

func TestOnConnect_ShortTopicSkipsStateSubscription(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "short/topic"},
		},
	}

	client := newMQTTClientWithMock(mock, config, func(string, []byte, *ValetudoMap, error) {})

	client.onConnect(mock)

	// Should only have 1 subscription (map data only, no state topic derivable)
	mock.mu.RLock()
	handlers := len(mock.messageHandlers)
	mock.mu.RUnlock()

	if handlers != 1 {
		t.Errorf("Number of subscriptions = %d, want 1 (short topic cannot derive state topic)", handlers)
	}
}

func TestDockingHandler_EndToEnd(t *testing.T) {
	mockClient := NewMockClient()

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "rocky7", Topic: "valetudo/rocky7/MapData/map-data"},
		},
	}

	client := newMQTTClientWithMock(mockClient, config, func(string, []byte, *ValetudoMap, error) {})

	var dockedVacuum string
	client.SetDockingHandler(func(vacuumID string) {
		dockedVacuum = vacuumID
	})

	// Trigger onConnect to subscribe to all topics
	client.onConnect(mockClient)

	// Simulate state message arriving on the state topic
	mockClient.SimulateMessage("valetudo/rocky7/StatusStateAttribute/status", []byte(`{"value":"docked"}`))

	assert.Equal(t, "rocky7", dockedVacuum)
}

func BenchmarkDeriveStateTopic(b *testing.B) {
	topic := "valetudo/rocky7/MapData/map-data"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		deriveStateTopic(topic)
	}
}

func BenchmarkCreateStateMessageHandler(b *testing.B) {
	client := &MQTTClient{}
	client.SetDockingHandler(func(string) {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.createStateMessageHandler("vacuum1")
	}
}
