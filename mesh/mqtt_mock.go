package mesh

import (
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
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

// MockClient implements mqtt.Client for testing
type MockClient struct {
	connected         bool
	connectError      error
	publishError      error
	subscribeError    error
	messageHandlers   map[string]mqtt.MessageHandler
	publishedMessages []MockMessage
	mu                sync.RWMutex
	connectDelay time.Duration
	onConnect    mqtt.OnConnectHandler
}

type MockMessage struct {
	Topic   string
	Payload []byte
	QoS     byte
	Retain  bool
}

// NewMockClient creates a new mock MQTT client
func NewMockClient() *MockClient {
	return &MockClient{
		messageHandlers:   make(map[string]mqtt.MessageHandler),
		publishedMessages: []MockMessage{},
		connected:         false,
	}
}

// SetConnected sets the connection state
func (c *MockClient) SetConnected(connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = connected
}

// SetConnectError sets the error returned on Connect
func (c *MockClient) SetConnectError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectError = err
}

// SetPublishError sets the error returned on Publish
func (c *MockClient) SetPublishError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publishError = err
}

// SetSubscribeError sets the error returned on Subscribe
func (c *MockClient) SetSubscribeError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribeError = err
}

// SetConnectDelay sets a delay for Connect operations (simulates network latency)
func (c *MockClient) SetConnectDelay(delay time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectDelay = delay
}

// GetPublishedMessages returns all published messages
func (c *MockClient) GetPublishedMessages() []MockMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]MockMessage, len(c.publishedMessages))
	copy(result, c.publishedMessages)
	return result
}

// SimulateMessage simulates receiving a message on a topic
func (c *MockClient) SimulateMessage(topic string, payload []byte) {
	c.mu.RLock()
	handler, ok := c.messageHandlers[topic]
	c.mu.RUnlock()

	if ok && handler != nil {
		msg := &mockMessage{
			topic:   topic,
			payload: payload,
		}
		handler(c, msg)
	}
}

// IsConnected returns the connection status
func (c *MockClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// IsConnectionOpen returns whether the connection is open
func (c *MockClient) IsConnectionOpen() bool {
	return c.IsConnected()
}

// Connect simulates connecting to the broker
func (c *MockClient) Connect() mqtt.Token {
	c.mu.Lock()
	delay := c.connectDelay
	err := c.connectError
	c.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}

	if err == nil {
		c.mu.Lock()
		c.connected = true
		onConnect := c.onConnect
		c.mu.Unlock()

		// Call onConnect handler if set
		if onConnect != nil {
			go onConnect(c)
		}
	}

	return NewMockToken(err)
}

// Disconnect simulates disconnecting from the broker
func (c *MockClient) Disconnect(quiesce uint) {
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()
}

// Publish simulates publishing a message
func (c *MockClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return NewMockToken(mqtt.ErrNotConnected)
	}

	if c.publishError != nil {
		return NewMockToken(c.publishError)
	}

	// Convert payload to []byte
	var payloadBytes []byte
	switch v := payload.(type) {
	case []byte:
		payloadBytes = v
	case string:
		payloadBytes = []byte(v)
	}

	c.publishedMessages = append(c.publishedMessages, MockMessage{
		Topic:   topic,
		Payload: payloadBytes,
		QoS:     qos,
		Retain:  retained,
	})

	return NewMockToken(nil)
}

// Subscribe simulates subscribing to a topic
func (c *MockClient) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return NewMockToken(mqtt.ErrNotConnected)
	}

	if c.subscribeError != nil {
		return NewMockToken(c.subscribeError)
	}

	c.messageHandlers[topic] = callback
	return NewMockToken(nil)
}

// SubscribeMultiple simulates subscribing to multiple topics
func (c *MockClient) SubscribeMultiple(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return NewMockToken(mqtt.ErrNotConnected)
	}

	if c.subscribeError != nil {
		return NewMockToken(c.subscribeError)
	}

	for topic := range filters {
		c.messageHandlers[topic] = callback
	}

	return NewMockToken(nil)
}

// Unsubscribe simulates unsubscribing from a topic
func (c *MockClient) Unsubscribe(topics ...string) mqtt.Token {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, topic := range topics {
		delete(c.messageHandlers, topic)
	}

	return NewMockToken(nil)
}

// AddRoute adds a message handler for a topic
func (c *MockClient) AddRoute(topic string, callback mqtt.MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messageHandlers[topic] = callback
}

// OptionsReader returns the client options (not implemented for mock)
func (c *MockClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.ClientOptionsReader{}
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

func (m *mockMessage) Duplicate() bool      { return m.duplicate }
func (m *mockMessage) Qos() byte            { return m.qos }
func (m *mockMessage) Retained() bool       { return m.retained }
func (m *mockMessage) Topic() string        { return m.topic }
func (m *mockMessage) MessageID() uint16    { return m.messageID }
func (m *mockMessage) Payload() []byte      { return m.payload }
func (m *mockMessage) Ack()                 {}
func (m *mockMessage) AutoAckOff()          {}
func (m *mockMessage) AutoAckOn()           {}
func (m *mockMessage) SetAutoAck(bool)      {}
func (m *mockMessage) SetRetained(bool)     {}
func (m *mockMessage) SetQoS(byte)          {}
func (m *mockMessage) SetDuplicate(bool)    {}
func (m *mockMessage) SetMessageID(uint16)  {}
