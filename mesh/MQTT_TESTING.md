# MQTT Testing Guide

This document explains how to use mocks for testing MQTT functionality in tudomesh.

## Overview

The MQTT implementation uses the `github.com/eclipse/paho.mqtt.golang` package, which provides the `mqtt.Client` interface. We've created a mock implementation that allows for fast, deterministic testing without requiring a real MQTT broker.

## Mock Components

### MockClient

Located in `mqtt_mock.go`, implements the `mqtt.Client` interface:

```go
mock := NewMockClient()
mock.SetConnected(true)

// Configure behavior
mock.SetConnectError(someError)
mock.SetPublishError(someError)
mock.SetSubscribeError(someError)
mock.SetConnectDelay(100 * time.Millisecond)

// Use like a real client
publisher := NewPublisher(mock)
err := publisher.PublishPosition("vacuum1", 100, 200, 90)
```

### MockToken

Implements `mqtt.Token` for async operation results:

```go
token := mock.Publish("topic", 0, false, []byte("payload"))
if token.WaitTimeout(1 * time.Second) && token.Error() != nil {
    // Handle error
}
```

## Testing Patterns

### 1. Basic Unit Tests

Test business logic without network dependencies:

```go
func TestPublisher_PublishPosition(t *testing.T) {
    mock := NewMockClient()
    mock.SetConnected(true)

    publisher := NewPublisher(mock)
    err := publisher.PublishPosition("vacuum1", 100, 200, 90)

    if err != nil {
        t.Errorf("Unexpected error: %v", err)
    }

    // Verify messages were published
    messages := mock.GetPublishedMessages()
    if len(messages) != 2 {
        t.Errorf("Expected 2 messages, got %d", len(messages))
    }
}
```

### 2. Message Handling Tests

Test message processing with simulated messages:

```go
func TestMQTTClient_MessageHandling(t *testing.T) {
    mock := NewMockClient()
    mock.SetConnected(true)

    var receivedData *ValetudoMap
    handler := func(vacuumID string, data *ValetudoMap, err error) {
        receivedData = data
    }

    client := newMQTTClientWithMock(mock, config, handler)
    mqttHandler := client.createMessageHandler("vacuum1")
    mock.Subscribe("test/topic", 0, mqttHandler)

    // Simulate receiving a message
    payload, _ := json.Marshal(testMapData)
    mock.SimulateMessage("test/topic", payload)

    // Verify handler was called with correct data
    if receivedData == nil {
        t.Error("Handler was not called")
    }
}
```

### 3. Error Condition Tests

Test error handling without flakiness:

```go
func TestPublisher_HandlePublishError(t *testing.T) {
    mock := NewMockClient()
    mock.SetConnected(true)
    mock.SetPublishError(errors.New("publish failed"))

    publisher := NewPublisher(mock)
    err := publisher.PublishPosition("vacuum1", 100, 200, 90)

    if err == nil {
        t.Error("Expected error from failed publish")
    }
}
```

### 4. Concurrency Tests

Test thread-safety efficiently:

```go
func TestConcurrentAccess(t *testing.T) {
    mock := NewMockClient()
    mock.SetConnected(true)

    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            mock.Publish("topic", 0, false, []byte("data"))
        }()
    }
    wg.Wait()

    // Verify all publishes succeeded
    messages := mock.GetPublishedMessages()
    if len(messages) != 100 {
        t.Errorf("Expected 100 messages, got %d", len(messages))
    }
}
```

## Mock API Reference

### MockClient Methods

**Configuration:**
- `SetConnected(bool)` - Set connection state
- `SetConnectError(error)` - Error to return from Connect()
- `SetPublishError(error)` - Error to return from Publish()
- `SetSubscribeError(error)` - Error to return from Subscribe()
- `SetConnectDelay(duration)` - Simulate connection latency

**Inspection:**
- `GetPublishedMessages()` - Retrieve all published messages
- `SimulateMessage(topic, payload)` - Trigger message handlers

**mqtt.Client Interface:**
- `Connect()` - Simulate connection
- `Disconnect(quiesce)` - Simulate disconnection
- `Publish(topic, qos, retained, payload)` - Simulate publish
- `Subscribe(topic, qos, handler)` - Register message handler
- `IsConnected()` - Check connection state

### MockMessage Structure

```go
type MockMessage struct {
    Topic   string
    Payload []byte
    QoS     byte
    Retain  bool
}
```

## Performance Benefits

Mock-based tests are significantly faster than integration tests:

- **Mock Subscribe**: ~65 ns/op (1 alloc)
- **Mock Publish**: ~50 ns/op (1 alloc)
- **Simulated Message**: ~50 ns/op (1 alloc)

Compare this to real broker connections which require:
- Network round trips (milliseconds to seconds)
- Connection establishment overhead
- Potential timeouts and retries

## Best Practices

1. **Use mocks for unit tests** - Test business logic in isolation
2. **Use real broker for integration tests** - Verify end-to-end behavior
3. **Test error paths** - Use mock error injection to test failure handling
4. **Verify message content** - Use `GetPublishedMessages()` to inspect payloads
5. **Test concurrency** - Use mocks to efficiently test race conditions
6. **Keep tests fast** - Avoid time.Sleep() in favor of synchronous mock operations

## Integration vs Unit Tests

### Unit Tests (with mocks)
- Fast execution (microseconds)
- Deterministic behavior
- No external dependencies
- Easy error injection
- Run in CI without setup

### Integration Tests (with real broker)
- Test actual MQTT behavior
- Verify network resilience
- Catch protocol issues
- Require broker setup
- Use build tags: `// +build integration`

## Example Test Structure

```go
// Unit test - fast, always run
func TestPublisher_Logic(t *testing.T) {
    mock := NewMockClient()
    mock.SetConnected(true)
    // ... test logic ...
}

// Integration test - slow, optional
// +build integration
func TestPublisher_Integration(t *testing.T) {
    broker := os.Getenv("TEST_MQTT_BROKER")
    if broker == "" {
        t.Skip("TEST_MQTT_BROKER not set")
    }
    // ... test with real broker ...
}
```

## Troubleshooting

**Mock not receiving messages:**
- Ensure `SetConnected(true)` is called before Subscribe
- Check that topic strings match exactly
- Verify handler was registered with Subscribe before SimulateMessage

**Race detector warnings:**
- All mock operations use proper locking
- If you see races, they're likely in your test code
- Use `go test -race` to identify issues

**Unexpected nil clients:**
- Use `newMQTTClientWithMock()` for testing MQTTClient behavior
- Use `NewPublisher(mock)` for testing Publisher behavior
- Check that mock is passed correctly to constructors
