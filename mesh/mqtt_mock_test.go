package mesh

import (
	"log"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/mock"
)

// MockToken implements mqtt.Token for testing
type MockToken struct {
	err       error
	completed bool
	mu        sync.RWMutex
}

func NewMockToken(err error) *MockToken {
	return &MockToken{
		err:       err,
		completed: true,
	}
}

func (t *MockToken) Wait() bool {
	return t.WaitTimeout(30 * time.Second)
}

func (t *MockToken) WaitTimeout(duration time.Duration) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.completed
}

func (t *MockToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (t *MockToken) Error() error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.err
}

// MockClient implements MQTTClientInterface using testify/mock
type MockClient struct {
	mock.Mock
	mu              sync.RWMutex
	connected       bool
	messageHandlers map[string]mqtt.MessageHandler
}

// NewMockClient creates a new mock MQTT client
func NewMockClient() *MockClient {
	m := &MockClient{
		messageHandlers: make(map[string]mqtt.MessageHandler),
		connected:       true, // Default to connected for typical tests
	}

	// Set default permissive stubs
	m.On("IsConnected").Return(true).Maybe()
	m.On("Connect").Return(NewMockToken(nil)).Maybe()
	m.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(NewMockToken(nil)).Run(func(args mock.Arguments) {
		topic := args.String(0)
		handler := args.Get(2).(mqtt.MessageHandler)
		m.mu.Lock()
		m.messageHandlers[topic] = handler
		m.mu.Unlock()
	}).Maybe()
	m.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(NewMockToken(nil)).Maybe()
	m.On("Disconnect", mock.Anything).Return().Maybe()

	return m
}

func (m *MockClient) Connect() mqtt.Token {
	args := m.Called()
	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()
	if t, ok := args.Get(0).(mqtt.Token); ok {
		return t
	}
	return NewMockToken(nil)
}

func (m *MockClient) Disconnect(quiesce uint) {
	m.Called(quiesce)
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
}

func (m *MockClient) IsConnected() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	args := m.Called(topic, qos, retained, payload)
	if t, ok := args.Get(0).(mqtt.Token); ok {
		return t
	}
	return NewMockToken(nil)
}

func (m *MockClient) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	args := m.Called(topic, qos, callback)
	if t, ok := args.Get(0).(mqtt.Token); ok {
		return t
	}
	return NewMockToken(nil)
}

// --- Helper methods for tests ---

// SetConnected sets the connection state directly (for simple test setup)
func (m *MockClient) SetConnected(connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = connected
}

// SimulateMessage simulates receiving a message on a topic
func (m *MockClient) SimulateMessage(topic string, payload []byte) {
	m.mu.RLock()
	handler, ok := m.messageHandlers[topic]
	m.mu.RUnlock()

	if ok && handler != nil {
		msg := &mockMessage{
			topic:   topic,
			payload: payload,
		}
		handler(nil, msg) // Passing nil for the client as it's often not used in handlers
	} else {
		log.Printf("MockClient: No handler for topic %s", topic)
	}
}

// GetPublishedMessages is a helper to extract messages from mock calls.
// This is used to maintain compatibility with existing tests.
func (m *MockClient) GetPublishedMessages() []MockMessage {
	var messages []MockMessage
	for _, call := range m.Calls {
		if call.Method == "Publish" {
			topic := call.Arguments.String(0)
			qos := call.Arguments.Get(1).(byte)
			retained := call.Arguments.Bool(2)
			payload := call.Arguments.Get(3)

			var payloadBytes []byte
			switch v := payload.(type) {
			case []byte:
				payloadBytes = v
			case string:
				payloadBytes = []byte(v)
			}

			messages = append(messages, MockMessage{
				Topic:   topic,
				Payload: payloadBytes,
				QoS:     qos,
				Retain:  retained,
			})
		}
	}
	return messages
}

type MockMessage struct {
	Topic   string
	Payload []byte
	QoS     byte
	Retain  bool
}

// mockMessage implements mqtt.Message for testing
type mockMessage struct {
	topic     string
	payload   []byte
	qos       byte
	retained  bool
	messageID uint16
	duplicate bool
}

func (m *mockMessage) Duplicate() bool     { return m.duplicate }
func (m *mockMessage) Qos() byte           { return m.qos }
func (m *mockMessage) Retained() bool      { return m.retained }
func (m *mockMessage) Topic() string       { return m.topic }
func (m *mockMessage) MessageID() uint16   { return m.messageID }
func (m *mockMessage) Payload() []byte     { return m.payload }
func (m *mockMessage) Ack()                {}
func (m *mockMessage) AutoAckOff()         {}
func (m *mockMessage) AutoAckOn()          {}
func (m *mockMessage) SetAutoAck(bool)     {}
func (m *mockMessage) SetRetained(bool)    {}
func (m *mockMessage) SetQoS(byte)         {}
func (m *mockMessage) SetDuplicate(bool)   {}
func (m *mockMessage) SetMessageID(uint16) {}
