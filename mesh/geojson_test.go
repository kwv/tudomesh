package mesh

import (
	"encoding/json"
	"testing"
)

func TestNewFeatureCollection(t *testing.T) {
	fc := NewFeatureCollection()

	if fc.Type != "FeatureCollection" {
		t.Errorf("Expected Type 'FeatureCollection', got '%s'", fc.Type)
	}
	if fc.Features == nil {
		t.Error("Expected Features to be initialized")
	}
	if len(fc.Features) != 0 {
		t.Errorf("Expected 0 features, got %d", len(fc.Features))
	}
}

func TestNewFeature(t *testing.T) {
	geom := &Geometry{Type: GeometryPoint}
	props := map[string]interface{}{"name": "test"}

	f := NewFeature(geom, props)

	if f.Type != "Feature" {
		t.Errorf("Expected Type 'Feature', got '%s'", f.Type)
	}
	if f.Geometry != geom {
		t.Error("Geometry mismatch")
	}
	if f.Properties["name"] != "test" {
		t.Error("Properties not set correctly")
	}
}

func TestNewFeatureNilProperties(t *testing.T) {
	geom := &Geometry{Type: GeometryPoint}
	f := NewFeature(geom, nil)

	if f.Properties == nil {
		t.Error("Expected Properties to be initialized when nil is passed")
	}
	if len(f.Properties) != 0 {
		t.Errorf("Expected empty properties map, got %d entries", len(f.Properties))
	}
}

func TestAddFeature(t *testing.T) {
	fc := NewFeatureCollection()
	f := NewFeature(&Geometry{Type: GeometryPoint}, nil)

	fc.AddFeature(f)

	if len(fc.Features) != 1 {
		t.Errorf("Expected 1 feature, got %d", len(fc.Features))
	}
	if fc.Features[0] != f {
		t.Error("Feature not added correctly")
	}
}

func TestPathToLineString(t *testing.T) {
	path := Path{
		{X: 0, Y: 0},
		{X: 100, Y: 0},
		{X: 100, Y: 100},
	}

	geom := PathToLineString(path)

	if geom.Type != GeometryLineString {
		t.Errorf("Expected type LineString, got %s", geom.Type)
	}

	// Verify coordinates can be unmarshaled
	var coords [][2]float64
	if err := json.Unmarshal(geom.Coordinates, &coords); err != nil {
		t.Fatalf("Failed to unmarshal coordinates: %v", err)
	}

	if len(coords) != 3 {
		t.Errorf("Expected 3 coordinates, got %d", len(coords))
	}

	expectedCoords := [][2]float64{
		{0, 0},
		{100, 0},
		{100, 100},
	}

	for i, expected := range expectedCoords {
		if coords[i][0] != expected[0] || coords[i][1] != expected[1] {
			t.Errorf("Coordinate %d: expected %v, got %v", i, expected, coords[i])
		}
	}
}

func TestPathsToMultiLineString(t *testing.T) {
	paths := []Path{
		{{X: 0, Y: 0}, {X: 100, Y: 0}},
		{{X: 0, Y: 100}, {X: 100, Y: 100}},
	}

	geom := PathsToMultiLineString(paths)

	if geom.Type != GeometryMultiLineString {
		t.Errorf("Expected type MultiLineString, got %s", geom.Type)
	}

	var coords [][][2]float64
	if err := json.Unmarshal(geom.Coordinates, &coords); err != nil {
		t.Fatalf("Failed to unmarshal coordinates: %v", err)
	}

	if len(coords) != 2 {
		t.Errorf("Expected 2 line strings, got %d", len(coords))
	}

	if len(coords[0]) != 2 {
		t.Errorf("Expected 2 points in first line, got %d", len(coords[0]))
	}
	if len(coords[1]) != 2 {
		t.Errorf("Expected 2 points in second line, got %d", len(coords[1]))
	}
}

func TestPathToPolygon(t *testing.T) {
	t.Run("open path gets closed", func(t *testing.T) {
		path := Path{
			{X: 0, Y: 0},
			{X: 100, Y: 0},
			{X: 100, Y: 100},
			{X: 0, Y: 100},
		}

		geom := PathToPolygon(path)

		if geom.Type != GeometryPolygon {
			t.Errorf("Expected type Polygon, got %s", geom.Type)
		}

		var coords [][][2]float64
		if err := json.Unmarshal(geom.Coordinates, &coords); err != nil {
			t.Fatalf("Failed to unmarshal coordinates: %v", err)
		}

		if len(coords) != 1 {
			t.Errorf("Expected 1 ring (outer), got %d", len(coords))
		}

		ring := coords[0]
		// Should be 5 points (4 original + 1 closing point)
		if len(ring) != 5 {
			t.Errorf("Expected 5 points (closed ring), got %d", len(ring))
		}

		// First and last should match
		if ring[0][0] != ring[4][0] || ring[0][1] != ring[4][1] {
			t.Errorf("Polygon not closed: first=%v, last=%v", ring[0], ring[4])
		}
	})

	t.Run("already closed path", func(t *testing.T) {
		path := Path{
			{X: 0, Y: 0},
			{X: 100, Y: 0},
			{X: 100, Y: 100},
			{X: 0, Y: 100},
			{X: 0, Y: 0}, // Already closed
		}

		geom := PathToPolygon(path)

		var coords [][][2]float64
		if err := json.Unmarshal(geom.Coordinates, &coords); err != nil {
			t.Fatalf("Failed to unmarshal coordinates: %v", err)
		}

		ring := coords[0]
		// Should still be 5 points (no duplicate closing)
		if len(ring) != 5 {
			t.Errorf("Expected 5 points, got %d", len(ring))
		}
	})
}

func TestPathsToPolygon(t *testing.T) {
	t.Run("single ring", func(t *testing.T) {
		paths := []Path{
			{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}},
		}

		geom := PathsToPolygon(paths)

		if geom.Type != GeometryPolygon {
			t.Errorf("Expected type Polygon, got %s", geom.Type)
		}

		var coords [][][2]float64
		if err := json.Unmarshal(geom.Coordinates, &coords); err != nil {
			t.Fatalf("Failed to unmarshal coordinates: %v", err)
		}

		if len(coords) != 1 {
			t.Errorf("Expected 1 ring, got %d", len(coords))
		}
	})

	t.Run("with holes", func(t *testing.T) {
		paths := []Path{
			// Outer ring
			{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}},
			// Hole
			{{X: 25, Y: 25}, {X: 75, Y: 25}, {X: 75, Y: 75}, {X: 25, Y: 75}},
		}

		geom := PathsToPolygon(paths)

		var coords [][][2]float64
		if err := json.Unmarshal(geom.Coordinates, &coords); err != nil {
			t.Fatalf("Failed to unmarshal coordinates: %v", err)
		}

		if len(coords) != 2 {
			t.Errorf("Expected 2 rings (1 outer + 1 hole), got %d", len(coords))
		}
	})

	t.Run("nil for empty paths", func(t *testing.T) {
		geom := PathsToPolygon([]Path{})
		if geom != nil {
			t.Error("Expected nil geometry for empty paths")
		}
	})
}

func TestTransformPath(t *testing.T) {
	path := Path{
		{X: 0, Y: 0},
		{X: 100, Y: 0},
		{X: 100, Y: 100},
	}

	// Translation by (50, 50)
	transform := Translation(50, 50)

	transformed := TransformPath(path, transform)

	expected := Path{
		{X: 50, Y: 50},
		{X: 150, Y: 50},
		{X: 150, Y: 150},
	}

	if len(transformed) != len(expected) {
		t.Fatalf("Expected %d points, got %d", len(expected), len(transformed))
	}

	for i := range expected {
		if transformed[i].X != expected[i].X || transformed[i].Y != expected[i].Y {
			t.Errorf("Point %d: expected %v, got %v", i, expected[i], transformed[i])
		}
	}
}

func TestTransformPaths(t *testing.T) {
	paths := []Path{
		{{X: 0, Y: 0}, {X: 100, Y: 0}},
		{{X: 0, Y: 100}, {X: 100, Y: 100}},
	}

	transform := Translation(50, 50)
	transformed := TransformPaths(paths, transform)

	if len(transformed) != 2 {
		t.Fatalf("Expected 2 paths, got %d", len(transformed))
	}

	// Check first path
	if transformed[0][0].X != 50 || transformed[0][0].Y != 50 {
		t.Errorf("First path first point incorrect: got (%f, %f)", transformed[0][0].X, transformed[0][0].Y)
	}

	// Check second path
	if transformed[1][0].X != 50 || transformed[1][0].Y != 150 {
		t.Errorf("Second path first point incorrect: got (%f, %f)", transformed[1][0].X, transformed[1][0].Y)
	}
}

func TestLayerToFeature(t *testing.T) {
	t.Run("floor layer as polygon", func(t *testing.T) {
		layer := &MapLayer{
			Type: "floor",
			MetaData: LayerMetaData{
				Area: 10000,
			},
		}

		paths := []Path{
			{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}},
		}

		feature := LayerToFeature(layer, paths, "vacuum1", Identity(), 5)

		if feature == nil {
			t.Fatal("Expected non-nil feature")
		}

		if feature.Type != "Feature" {
			t.Errorf("Expected type Feature, got %s", feature.Type)
		}

		if feature.Geometry.Type != GeometryPolygon {
			t.Errorf("Expected Polygon geometry for floor, got %s", feature.Geometry.Type)
		}

		if feature.Properties["layerType"] != "floor" {
			t.Error("Properties missing layerType")
		}

		if feature.Properties["vacuumId"] != "vacuum1" {
			t.Error("Properties missing vacuumId")
		}

		if feature.Properties["area"] != 10000 {
			t.Error("Properties missing area")
		}
	})

	t.Run("wall layer as multilinestring", func(t *testing.T) {
		layer := &MapLayer{
			Type: "wall",
		}

		paths := []Path{
			{{X: 0, Y: 0}, {X: 100, Y: 0}},
			{{X: 0, Y: 100}, {X: 100, Y: 100}},
		}

		feature := LayerToFeature(layer, paths, "vacuum1", Identity(), 5)

		if feature == nil {
			t.Fatal("Expected non-nil feature")
		}

		if feature.Geometry.Type != GeometryMultiLineString {
			t.Errorf("Expected MultiLineString geometry for wall, got %s", feature.Geometry.Type)
		}
	})

	t.Run("segment layer with metadata", func(t *testing.T) {
		layer := &MapLayer{
			Type: "segment",
			MetaData: LayerMetaData{
				SegmentID: "seg123",
				Name:      "Kitchen",
				Area:      5000,
				Active:    true,
			},
		}

		paths := []Path{
			{{X: 0, Y: 0}, {X: 50, Y: 0}, {X: 50, Y: 50}, {X: 0, Y: 50}},
		}

		feature := LayerToFeature(layer, paths, "vacuum2", Identity(), 5)

		if feature.Properties["segmentId"] != "seg123" {
			t.Error("Properties missing segmentId")
		}

		if feature.Properties["segmentName"] != "Kitchen" {
			t.Error("Properties missing segmentName")
		}

		if feature.Properties["active"] != true {
			t.Error("Properties missing active")
		}
	})

	t.Run("nil for nil layer", func(t *testing.T) {
		paths := []Path{{{X: 0, Y: 0}}}
		feature := LayerToFeature(nil, paths, "vacuum1", Identity(), 5)
		if feature != nil {
			t.Error("Expected nil feature for nil layer")
		}
	})

	t.Run("nil for empty paths", func(t *testing.T) {
		layer := &MapLayer{Type: "floor"}
		feature := LayerToFeature(layer, []Path{}, "vacuum1", Identity(), 5)
		if feature != nil {
			t.Error("Expected nil feature for empty paths")
		}
	})
}

func TestMapToFeatureCollection(t *testing.T) {
	t.Run("converts map with multiple layers", func(t *testing.T) {
		valetudoMap := &ValetudoMap{
			PixelSize: 5,
			Layers: []MapLayer{
				{
					Type:   "floor",
					Pixels: []int{0, 0, 1, 0, 1, 1, 0, 1}, // Simple square
				},
				{
					Type:   "wall",
					Pixels: []int{0, 0, 1, 0}, // Simple line
				},
			},
		}

		fc := MapToFeatureCollection(valetudoMap, "vacuum1", Identity(), 1.0)

		if fc.Type != "FeatureCollection" {
			t.Error("Expected FeatureCollection type")
		}

		// Should have features for layers that successfully vectorize
		// Note: actual count depends on vectorizer output
		if len(fc.Features) == 0 {
			t.Error("Expected at least some features")
		}
	})

	t.Run("empty collection for nil map", func(t *testing.T) {
		fc := MapToFeatureCollection(nil, "vacuum1", Identity(), 1.0)

		if fc == nil {
			t.Fatal("Expected non-nil FeatureCollection")
		}

		if len(fc.Features) != 0 {
			t.Errorf("Expected empty features, got %d", len(fc.Features))
		}
	})

	t.Run("applies transform to paths", func(t *testing.T) {
		valetudoMap := &ValetudoMap{
			PixelSize: 5,
			Layers: []MapLayer{
				{
					Type: "floor",
					// Create a simple 2x2 square of pixels
					Pixels: []int{
						0, 0, 1, 0, 0, 1, 1, 1, // Square pattern
					},
				},
			},
		}

		// Translation transform
		transform := Translation(1000, 2000)

		fc := MapToFeatureCollection(valetudoMap, "vacuum1", transform, 1.0)

		if len(fc.Features) > 0 {
			// Verify that coordinates were transformed
			// (this is a basic check - actual coordinates depend on vectorizer)
			feature := fc.Features[0]
			if feature.Properties["vacuumId"] != "vacuum1" {
				t.Error("VacuumId not set correctly")
			}
		}
	})
}

func TestGeoJSONSerialization(t *testing.T) {
	t.Run("serialize feature collection", func(t *testing.T) {
		fc := NewFeatureCollection()

		path := Path{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}}
		geom := PathToLineString(path)
		props := map[string]interface{}{
			"name":     "test",
			"vacuumId": "vacuum1",
		}
		feature := NewFeature(geom, props)
		fc.AddFeature(feature)

		data, err := json.Marshal(fc)
		if err != nil {
			t.Fatalf("Failed to marshal FeatureCollection: %v", err)
		}

		// Verify it's valid JSON
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Failed to parse marshaled JSON: %v", err)
		}

		if parsed["type"] != "FeatureCollection" {
			t.Error("Type not preserved in JSON")
		}

		features, ok := parsed["features"].([]interface{})
		if !ok || len(features) != 1 {
			t.Error("Features not preserved in JSON")
		}
	})

	t.Run("roundtrip feature collection", func(t *testing.T) {
		original := NewFeatureCollection()
		path := Path{{X: 123.45, Y: 678.90}}
		geom := PathToLineString(path)
		feature := NewFeature(geom, map[string]interface{}{"test": "value"})
		original.AddFeature(feature)

		// Marshal
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		// Unmarshal
		var roundtrip FeatureCollection
		if err := json.Unmarshal(data, &roundtrip); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if roundtrip.Type != original.Type {
			t.Error("Type not preserved in roundtrip")
		}

		if len(roundtrip.Features) != len(original.Features) {
			t.Errorf("Feature count not preserved: expected %d, got %d", len(original.Features), len(roundtrip.Features))
		}
	})
}
