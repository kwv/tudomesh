package mesh

import (
	"encoding/json"
	"math"
	"testing"
)

func TestNewPublisher(t *testing.T) {
	publisher := NewPublisher(nil)
	if publisher == nil {
		t.Fatal("NewPublisher() returned nil")
	}

	if publisher.publishPrefix != "tudomesh" {
		t.Errorf("Default prefix = %s, want tudomesh", publisher.publishPrefix)
	}

	if publisher.qos != 0 {
		t.Errorf("Default QoS = %d, want 0", publisher.qos)
	}

	if !publisher.retain {
		t.Error("Default retain should be true")
	}

	if publisher.positions == nil {
		t.Error("Positions map should be initialized")
	}
}

func TestPublisher_GetPosition(t *testing.T) {
	publisher := NewPublisher(nil)

	// Test with no position stored
	_, ok := publisher.GetPosition("vacuum1")
	if ok {
		t.Error("GetPosition() should return false for non-existent vacuum")
	}

	// Store a position
	testPos := &VacuumPosition{
		VacuumID:  "vacuum1",
		X:         100.0,
		Y:         200.0,
		Angle:     90.0,
		Timestamp: 1234567890,
	}
	publisher.positions["vacuum1"] = testPos

	// Retrieve position
	pos, ok := publisher.GetPosition("vacuum1")
	if !ok {
		t.Fatal("GetPosition() should return true for existing vacuum")
	}

	if pos.VacuumID != testPos.VacuumID {
		t.Errorf("VacuumID = %s, want %s", pos.VacuumID, testPos.VacuumID)
	}
	if pos.X != testPos.X {
		t.Errorf("X = %.0f, want %.0f", pos.X, testPos.X)
	}
	if pos.Y != testPos.Y {
		t.Errorf("Y = %.0f, want %.0f", pos.Y, testPos.Y)
	}
	if pos.Angle != testPos.Angle {
		t.Errorf("Angle = %.0f, want %.0f", pos.Angle, testPos.Angle)
	}
}

func TestPublisher_GetAllPositions(t *testing.T) {
	publisher := NewPublisher(nil)

	// Test with no positions
	positions := publisher.GetAllPositions()
	if len(positions) != 0 {
		t.Errorf("GetAllPositions() with empty state = %d positions, want 0", len(positions))
	}

	// Add some positions
	publisher.positions["vacuum1"] = &VacuumPosition{
		VacuumID: "vacuum1",
		X:        100.0,
		Y:        200.0,
		Angle:    90.0,
	}
	publisher.positions["vacuum2"] = &VacuumPosition{
		VacuumID: "vacuum2",
		X:        300.0,
		Y:        400.0,
		Angle:    180.0,
	}

	// Get all positions
	positions = publisher.GetAllPositions()
	if len(positions) != 2 {
		t.Errorf("GetAllPositions() = %d positions, want 2", len(positions))
	}

	// Verify positions exist
	if _, ok := positions["vacuum1"]; !ok {
		t.Error("vacuum1 not found in positions")
	}
	if _, ok := positions["vacuum2"]; !ok {
		t.Error("vacuum2 not found in positions")
	}

	// Verify returned data is a copy (not references to internal state)
	positions["vacuum1"].X = 999.0
	if publisher.positions["vacuum1"].X == 999.0 {
		t.Error("GetAllPositions() should return a copy, not internal references")
	}
}

func TestPublisher_ClearPosition(t *testing.T) {
	publisher := NewPublisher(nil)

	// Add a position
	publisher.positions["vacuum1"] = &VacuumPosition{
		VacuumID: "vacuum1",
		X:        100.0,
		Y:        200.0,
	}

	// Verify it exists
	if _, ok := publisher.GetPosition("vacuum1"); !ok {
		t.Fatal("Position should exist before clearing")
	}

	// Clear it
	publisher.ClearPosition("vacuum1")

	// Verify it's gone
	if _, ok := publisher.GetPosition("vacuum1"); ok {
		t.Error("Position should not exist after clearing")
	}
}

func TestPublisher_SetQoS(t *testing.T) {
	publisher := NewPublisher(nil)

	tests := []struct {
		name     string
		qos      byte
		expected byte
	}{
		{"QoS 0", 0, 0},
		{"QoS 1", 1, 1},
		{"QoS 2", 2, 2},
		{"Invalid QoS 3", 3, 0}, // Should be rejected, keep default
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher.qos = 0 // Reset
			publisher.SetQoS(tt.qos)
			if publisher.qos != tt.expected {
				t.Errorf("After SetQoS(%d), qos = %d, want %d", tt.qos, publisher.qos, tt.expected)
			}
		})
	}
}

func TestPublisher_SetRetain(t *testing.T) {
	publisher := NewPublisher(nil)

	publisher.SetRetain(true)
	if !publisher.retain {
		t.Error("SetRetain(true) did not set retain flag")
	}

	publisher.SetRetain(false)
	if publisher.retain {
		t.Error("SetRetain(false) did not clear retain flag")
	}
}

func TestPublisher_PublishPositionFormat(t *testing.T) {
	publisher := NewPublisher(nil)

	// Store a position (simulates what PublishPosition would do)
	publisher.positions["vacuum1"] = &VacuumPosition{
		VacuumID:  "vacuum1",
		X:         25600.0,
		Y:         30000.0,
		Angle:     90.0,
		Timestamp: 1706140800,
	}

	pos := publisher.positions["vacuum1"]

	// Verify JSON marshaling works correctly
	jsonBytes, err := json.Marshal(pos)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Verify JSON structure
	var decoded VacuumPosition
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.VacuumID != "vacuum1" {
		t.Errorf("Decoded VacuumID = %s, want vacuum1", decoded.VacuumID)
	}
	if decoded.X != 25600.0 {
		t.Errorf("Decoded X = %.0f, want 25600", decoded.X)
	}
	if decoded.Y != 30000.0 {
		t.Errorf("Decoded Y = %.0f, want 30000", decoded.Y)
	}
	if decoded.Angle != 90.0 {
		t.Errorf("Decoded Angle = %.0f, want 90", decoded.Angle)
	}
}

func TestPublisher_CombinedMessageFormat(t *testing.T) {
	publisher := NewPublisher(nil)

	// Add multiple positions
	publisher.positions["vacuum1"] = &VacuumPosition{
		VacuumID: "vacuum1",
		X:        100.0,
		Y:        200.0,
		Angle:    0.0,
	}
	publisher.positions["vacuum2"] = &VacuumPosition{
		VacuumID: "vacuum2",
		X:        300.0,
		Y:        400.0,
		Angle:    180.0,
	}

	// Build combined message (simulates publishCombined)
	positions := publisher.GetAllPositions()
	positionList := make([]*VacuumPosition, 0, len(positions))
	for _, pos := range positions {
		positionList = append(positionList, pos)
	}

	message := map[string]interface{}{
		"vacuums":   positionList,
		"timestamp": int64(1706140800),
	}

	// Verify JSON marshaling
	jsonBytes, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Verify JSON can be decoded
	var decoded map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if _, ok := decoded["vacuums"]; !ok {
		t.Error("Combined message should have 'vacuums' field")
	}
	if _, ok := decoded["timestamp"]; !ok {
		t.Error("Combined message should have 'timestamp' field")
	}
}

func TestPublisher_TransformAngleCalculation(t *testing.T) {
	tests := []struct {
		name          string
		transform     AffineMatrix
		localAngle    float64
		expectedAngle float64
		tolerance     float64
	}{
		{
			name:          "identity transform",
			transform:     Identity(),
			localAngle:    90.0,
			expectedAngle: 90.0,
			tolerance:     0.1,
		},
		{
			name:          "90 degree rotation",
			transform:     RotationDeg(90),
			localAngle:    0.0,
			expectedAngle: 90.0,
			tolerance:     0.1,
		},
		{
			name:          "180 degree rotation",
			transform:     RotationDeg(180),
			localAngle:    0.0,
			expectedAngle: 180.0,
			tolerance:     0.1,
		},
		{
			name:          "270 degree rotation",
			transform:     RotationDeg(270),
			localAngle:    0.0,
			expectedAngle: 270.0,
			tolerance:     0.1,
		},
		{
			name:          "local 45 + transform 90",
			transform:     RotationDeg(90),
			localAngle:    45.0,
			expectedAngle: 135.0,
			tolerance:     0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract rotation from transform matrix
			transformAngle := math.Atan2(tt.transform.C, tt.transform.A) * 180 / math.Pi
			worldAngle := tt.localAngle + transformAngle

			// Normalize to [0, 360)
			for worldAngle >= 360 {
				worldAngle -= 360
			}
			for worldAngle < 0 {
				worldAngle += 360
			}

			diff := math.Abs(worldAngle - tt.expectedAngle)
			if diff > tt.tolerance {
				t.Errorf("Angle = %.2f°, want %.2f° (±%.2f°)",
					worldAngle, tt.expectedAngle, tt.tolerance)
			}
		})
	}
}

func TestPublisher_AngleNormalization(t *testing.T) {
	tests := []struct {
		name     string
		angle    float64
		expected float64
	}{
		{"0 degrees", 0.0, 0.0},
		{"90 degrees", 90.0, 90.0},
		{"180 degrees", 180.0, 180.0},
		{"270 degrees", 270.0, 270.0},
		{"360 degrees", 360.0, 0.0},
		{"450 degrees", 450.0, 90.0},
		{"-90 degrees", -90.0, 270.0},
		{"-180 degrees", -180.0, 180.0},
		{"720 degrees", 720.0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			angle := tt.angle

			// Normalize to [0, 360)
			for angle >= 360 {
				angle -= 360
			}
			for angle < 0 {
				angle += 360
			}

			if angle != tt.expected {
				t.Errorf("Normalized angle = %.0f, want %.0f", angle, tt.expected)
			}
		})
	}
}

func TestPublisher_ConcurrentAccess(t *testing.T) {
	publisher := NewPublisher(nil)

	// Test concurrent reads and writes using the public API
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			vacuumID := string(rune('A' + id))
			for j := 0; j < 100; j++ {
				// Write using mutex-protected access
				publisher.mu.Lock()
				publisher.positions[vacuumID] = &VacuumPosition{
					VacuumID: vacuumID,
					X:        float64(j),
					Y:        float64(j * 2),
				}
				publisher.mu.Unlock()

				// Read
				_ = publisher.GetAllPositions()
				_, _ = publisher.GetPosition(vacuumID)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// No panic = success
}

func TestPublisher_PublishWithNilClient(t *testing.T) {
	publisher := NewPublisher(nil)

	// Should not panic, should return error
	err := publisher.PublishPosition("vacuum1", 100, 200, 90)
	if err == nil {
		t.Error("PublishPosition() with nil client should return error")
	}
}

func TestPublisher_PublishWithMockClient(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	publisher := NewPublisher(mock)

	// Should succeed with connected mock
	err := publisher.PublishPosition("vacuum1", 100, 200, 90)
	if err != nil {
		t.Errorf("PublishPosition() error = %v, want nil", err)
	}

	// Verify position was stored
	pos, ok := publisher.GetPosition("vacuum1")
	if !ok {
		t.Error("Position should be stored")
	}
	if pos.X != 100 || pos.Y != 200 || pos.Angle != 90 {
		t.Errorf("Stored position = (%.0f, %.0f, %.0f°), want (100, 200, 90°)",
			pos.X, pos.Y, pos.Angle)
	}

	// Verify MQTT messages were published
	messages := mock.GetPublishedMessages()
	if len(messages) != 2 {
		t.Errorf("Published messages count = %d, want 2 (individual + combined)", len(messages))
	}
}

// Benchmark position publishing operations
func BenchmarkPublisher_GetPosition(b *testing.B) {
	publisher := NewPublisher(nil)
	publisher.positions["vacuum1"] = &VacuumPosition{
		VacuumID: "vacuum1",
		X:        100.0,
		Y:        200.0,
		Angle:    90.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = publisher.GetPosition("vacuum1")
	}
}

func BenchmarkPublisher_GetAllPositions(b *testing.B) {
	publisher := NewPublisher(nil)
	for i := 0; i < 5; i++ {
		id := string(rune('A' + i))
		publisher.positions[id] = &VacuumPosition{
			VacuumID: id,
			X:        float64(i * 100),
			Y:        float64(i * 200),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		publisher.GetAllPositions()
	}
}

func BenchmarkPublisher_TransformCalculation(b *testing.B) {
	transform := RotationDeg(180)
	localPos := Point{X: 25600, Y: 25600}
	localAngle := 90.0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		worldPos := TransformPoint(localPos, transform)
		transformAngle := math.Atan2(transform.C, transform.A) * 180 / math.Pi
		worldAngle := localAngle + transformAngle
		for worldAngle >= 360 {
			worldAngle -= 360
		}
		for worldAngle < 0 {
			worldAngle += 360
		}
		_ = worldPos
		_ = worldAngle
	}
}

func BenchmarkPublisher_JSONMarshal(b *testing.B) {
	pos := &VacuumPosition{
		VacuumID:  "vacuum1",
		X:         25600.0,
		Y:         30000.0,
		Angle:     90.0,
		Timestamp: 1706140800,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(pos); err != nil {
			b.Fatalf("json.Marshal: %v", err)
		}
	}
}
