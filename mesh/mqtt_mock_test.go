package mesh

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestMockClient_Connect(t *testing.T) {
	mock := NewMockClient()

	// Test successful connection
	token := mock.Connect()
	if !token.WaitTimeout(1 * time.Second) {
		t.Error("Connect should complete immediately")
	}
	if token.Error() != nil {
		t.Errorf("Connect error = %v, want nil", token.Error())
	}
	if !mock.IsConnected() {
		t.Error("Client should be connected after Connect()")
	}
}

func TestMockClient_ConnectWithError(t *testing.T) {
	mock := NewMockClient()
	expectedErr := errors.New("connection failed")
	mock.SetConnectError(expectedErr)

	token := mock.Connect()
	if token.Error() != expectedErr {
		t.Errorf("Connect error = %v, want %v", token.Error(), expectedErr)
	}
	if mock.IsConnected() {
		t.Error("Client should not be connected after failed Connect()")
	}
}

func TestMockClient_Publish(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	payload := []byte(`{"test": "data"}`)
	token := mock.Publish("test/topic", 0, true, payload)

	if !token.WaitTimeout(1 * time.Second) {
		t.Error("Publish should complete immediately")
	}
	if token.Error() != nil {
		t.Errorf("Publish error = %v, want nil", token.Error())
	}

	messages := mock.GetPublishedMessages()
	if len(messages) != 1 {
		t.Fatalf("Published messages count = %d, want 1", len(messages))
	}

	msg := messages[0]
	if msg.Topic != "test/topic" {
		t.Errorf("Published topic = %s, want test/topic", msg.Topic)
	}
	if string(msg.Payload) != string(payload) {
		t.Errorf("Published payload = %s, want %s", msg.Payload, payload)
	}
	if !msg.Retain {
		t.Error("Message should be retained")
	}
}

func TestMockClient_PublishNotConnected(t *testing.T) {
	mock := NewMockClient()
	// Don't set connected

	token := mock.Publish("test/topic", 0, false, []byte("data"))
	if token.Error() == nil {
		t.Error("Publish should error when not connected")
	}
}

func TestMockClient_Subscribe(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	handlerCalled := false
	var receivedTopic string
	var receivedPayload []byte

	handler := func(client mqtt.Client, msg mqtt.Message) {
		handlerCalled = true
		receivedTopic = msg.Topic()
		receivedPayload = msg.Payload()
	}

	token := mock.Subscribe("test/topic", 0, handler)
	if token.Error() != nil {
		t.Errorf("Subscribe error = %v, want nil", token.Error())
	}

	// Simulate message
	payload := []byte(`{"vacuum_id": "test"}`)
	mock.SimulateMessage("test/topic", payload)

	if !handlerCalled {
		t.Error("Message handler was not called")
	}
	if receivedTopic != "test/topic" {
		t.Errorf("Received topic = %s, want test/topic", receivedTopic)
	}
	if string(receivedPayload) != string(payload) {
		t.Errorf("Received payload = %s, want %s", receivedPayload, payload)
	}
}

func TestMockClient_Disconnect(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	mock.Disconnect(250)

	if mock.IsConnected() {
		t.Error("Client should not be connected after Disconnect()")
	}
}

func TestMQTTClient_WithMock_OnConnect(t *testing.T) {
	mock := NewMockClient()
	// Mock must be connected for Subscribe to succeed
	mock.SetConnected(true)

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "test/vacuum1"},
			{ID: "vacuum2", Topic: "test/vacuum2"},
		},
	}

	handlerCalls := 0
	handler := func(vacuumID string, mapData *ValetudoMap, err error) {
		handlerCalls++
	}

	client := newMQTTClientWithMock(mock, config, handler)

	// Simulate connection callback
	client.onConnect(mock)

	// Check that client is marked connected
	if !client.IsConnected() {
		t.Error("Client should be connected after onConnect callback")
	}

	// Verify subscriptions were created
	mock.mu.RLock()
	handlers := len(mock.messageHandlers)
	mock.mu.RUnlock()

	if handlers != 2 {
		t.Errorf("Number of subscriptions = %d, want 2", handlers)
	}
}

func TestMQTTClient_WithMock_MessageHandling(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "test/vacuum1"},
		},
	}

	var receivedVacuumID string
	var receivedMapData *ValetudoMap
	var receivedErr error

	handler := func(vacuumID string, mapData *ValetudoMap, err error) {
		receivedVacuumID = vacuumID
		receivedMapData = mapData
		receivedErr = err
	}

	client := newMQTTClientWithMock(mock, config, handler)

	// Subscribe using the client's createMessageHandler
	mqttHandler := client.createMessageHandler("vacuum1")
	mock.Subscribe("test/vacuum1", 0, mqttHandler)

	// Create valid map data
	mapData := &ValetudoMap{
		Class: "ValetudoMap",
		MetaData: MapMetaData{
			Version: 1,
			Nonce:   "test-nonce",
		},
		Size:      Size{X: 100, Y: 100},
		PixelSize: 5,
		Layers:    []MapLayer{},
		Entities:  []MapEntity{},
	}

	payload, err := json.Marshal(mapData)
	if err != nil {
		t.Fatalf("Failed to marshal test data: %v", err)
	}

	// Simulate message
	mock.SimulateMessage("test/vacuum1", payload)

	// Verify handler was called with correct data
	if receivedVacuumID != "vacuum1" {
		t.Errorf("Received vacuum ID = %s, want vacuum1", receivedVacuumID)
	}
	if receivedMapData == nil {
		t.Error("Received map data is nil")
	}
	if receivedErr != nil {
		t.Errorf("Received error = %v, want nil", receivedErr)
	}
	if receivedMapData != nil && receivedMapData.Class != "ValetudoMap" {
		t.Errorf("Map class = %s, want ValetudoMap", receivedMapData.Class)
	}
}

func TestMQTTClient_WithMock_InvalidMapData(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "test/vacuum1"},
		},
	}

	var receivedErr error
	handler := func(vacuumID string, mapData *ValetudoMap, err error) {
		receivedErr = err
	}

	client := newMQTTClientWithMock(mock, config, handler)
	mqttHandler := client.createMessageHandler("vacuum1")
	mock.Subscribe("test/vacuum1", 0, mqttHandler)

	// Send invalid JSON
	mock.SimulateMessage("test/vacuum1", []byte(`{invalid json`))

	if receivedErr == nil {
		t.Error("Should have received error for invalid JSON")
	}
}

func TestMQTTClient_WithMock_GetVacuumByTopic(t *testing.T) {
	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "test/vacuum1"},
			{ID: "vacuum2", Topic: "test/vacuum2"},
		},
	}

	client := newMQTTClientWithMock(nil, config, nil)

	tests := []struct {
		topic  string
		wantID string
		wantOK bool
	}{
		{"test/vacuum1", "vacuum1", true},
		{"test/vacuum2", "vacuum2", true},
		{"test/unknown", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			id, ok := client.GetVacuumByTopic(tt.topic)
			if id != tt.wantID || ok != tt.wantOK {
				t.Errorf("GetVacuumByTopic(%s) = (%s, %v), want (%s, %v)",
					tt.topic, id, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}

func TestPublisher_WithMock(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	publisher := NewPublisher(mock)

	// Publish a position
	err := publisher.PublishPosition("vacuum1", 100, 200, 90)
	if err != nil {
		t.Errorf("PublishPosition error = %v, want nil", err)
	}

	// Verify published messages
	messages := mock.GetPublishedMessages()
	if len(messages) != 2 {
		t.Fatalf("Published messages count = %d, want 2 (individual + combined)", len(messages))
	}

	// Check individual message
	individualMsg := messages[0]
	if individualMsg.Topic != "tudomesh/vacuum1" {
		t.Errorf("Individual topic = %s, want tudomesh/vacuum1", individualMsg.Topic)
	}
	if !individualMsg.Retain {
		t.Error("Individual message should be retained")
	}

	var pos VacuumPosition
	if err := json.Unmarshal(individualMsg.Payload, &pos); err != nil {
		t.Fatalf("Failed to unmarshal individual message: %v", err)
	}
	if pos.VacuumID != "vacuum1" {
		t.Errorf("Position vacuum ID = %s, want vacuum1", pos.VacuumID)
	}
	if pos.X != 100 || pos.Y != 200 || pos.Angle != 90 {
		t.Errorf("Position = (%.0f, %.0f, %.0f°), want (100, 200, 90°)",
			pos.X, pos.Y, pos.Angle)
	}

	// Check combined message
	combinedMsg := messages[1]
	if combinedMsg.Topic != "tudomesh/positions" {
		t.Errorf("Combined topic = %s, want tudomesh/positions", combinedMsg.Topic)
	}

	var combined map[string]interface{}
	if err := json.Unmarshal(combinedMsg.Payload, &combined); err != nil {
		t.Fatalf("Failed to unmarshal combined message: %v", err)
	}
	if _, ok := combined["vacuums"]; !ok {
		t.Error("Combined message should have 'vacuums' field")
	}
	if _, ok := combined["timestamp"]; !ok {
		t.Error("Combined message should have 'timestamp' field")
	}
}

func TestPublisher_WithMock_NotConnected(t *testing.T) {
	mock := NewMockClient()
	// Don't set connected

	publisher := NewPublisher(mock)

	err := publisher.PublishPosition("vacuum1", 100, 200, 90)
	if err == nil {
		t.Error("PublishPosition should error when client not connected")
	}
}

func TestPublisher_WithMock_PublishError(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)
	mock.SetPublishError(errors.New("publish failed"))

	publisher := NewPublisher(mock)

	err := publisher.PublishPosition("vacuum1", 100, 200, 90)
	if err == nil {
		t.Error("PublishPosition should return error from mock")
	}
}

func TestPublisher_WithMock_TransformedPosition(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	publisher := NewPublisher(mock)

	localPos := Point{X: 100, Y: 200}
	localAngle := 45.0
	transform := RotationDeg(90) // Rotate 90 degrees

	err := publisher.PublishTransformedPosition("vacuum1", localPos, localAngle, transform)
	if err != nil {
		t.Errorf("PublishTransformedPosition error = %v, want nil", err)
	}

	messages := mock.GetPublishedMessages()
	if len(messages) == 0 {
		t.Fatal("No messages published")
	}

	// Verify the position was transformed
	var pos VacuumPosition
	if err := json.Unmarshal(messages[0].Payload, &pos); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	// After 90-degree rotation, (100, 200) should become (-200, 100)
	// Angle should be 45 + 90 = 135 degrees
	const tolerance = 0.01
	if abs(pos.X - (-200)) > tolerance {
		t.Errorf("Transformed X = %.2f, want -200.00", pos.X)
	}
	if abs(pos.Y-100) > tolerance {
		t.Errorf("Transformed Y = %.2f, want 100.00", pos.Y)
	}
	if abs(pos.Angle-135) > tolerance {
		t.Errorf("Transformed angle = %.2f°, want 135.00°", pos.Angle)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestMockClient_ConcurrentOperations(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 50; j++ {
				// Concurrent publishes
				topic := "test/topic"
				mock.Publish(topic, 0, false, []byte("test"))

				// Concurrent subscribes
				handler := func(client mqtt.Client, msg mqtt.Message) {}
				mock.Subscribe(topic, 0, handler)

				// Concurrent message simulation
				mock.SimulateMessage(topic, []byte("data"))
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// No panic = success (test for race conditions)
}

// Benchmark mock operations
func BenchmarkMockClient_Publish(b *testing.B) {
	mock := NewMockClient()
	mock.SetConnected(true)
	payload := []byte(`{"test": "data"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock.Publish("test/topic", 0, false, payload)
	}
}

func BenchmarkMockClient_Subscribe(b *testing.B) {
	mock := NewMockClient()
	mock.SetConnected(true)
	handler := func(client mqtt.Client, msg mqtt.Message) {}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock.Subscribe("test/topic", 0, handler)
	}
}

func BenchmarkMockClient_SimulateMessage(b *testing.B) {
	mock := NewMockClient()
	mock.SetConnected(true)

	callCount := 0
	handler := func(client mqtt.Client, msg mqtt.Message) {
		callCount++
	}
	mock.Subscribe("test/topic", 0, handler)

	payload := []byte(`{"test": "data"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock.SimulateMessage("test/topic", payload)
	}
}
