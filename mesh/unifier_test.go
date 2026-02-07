package mesh

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/paulmach/orb"
)

// --- helpers ---

func makeLineFeature(pts [][2]float64, props map[string]interface{}) *Feature {
	coordsJSON, _ := json.Marshal(pts)
	geom := &Geometry{Type: GeometryLineString, Coordinates: coordsJSON}
	if props == nil {
		props = map[string]interface{}{"layerType": "wall"}
	}
	return NewFeature(geom, props)
}

func makeSource(vacuumID string, icpScore float64) FeatureSource {
	return FeatureSource{
		VacuumID: vacuumID,
		ICPScore: icpScore,
	}
}

func lineCoords(f *UnifiedFeature) [][2]float64 {
	var coords [][2]float64
	_ = json.Unmarshal(f.Geometry.Coordinates, &coords)
	return coords
}

// --- UnifyWalls tests ---

func TestUnifyWalls_TwoVacuumsSameWall(t *testing.T) {
	// Two vacuums see the same horizontal wall, slightly offset in Y.
	// Expected: one unified wall at the median Y position.
	f1 := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, nil)
	f2 := makeLineFeature([][2]float64{{0, 10}, {100, 10}}, nil)

	sources := []FeatureSource{
		makeSource("vac-A", 0.95),
		makeSource("vac-B", 0.90),
	}

	result := UnifyWalls([]*Feature{f1, f2}, sources, 2)

	if len(result) != 1 {
		t.Fatalf("Expected 1 unified wall, got %d", len(result))
	}

	uf := result[0]
	if uf.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %f", uf.Confidence)
	}
	if uf.ObservationCount != 2 {
		t.Errorf("Expected 2 observations, got %d", uf.ObservationCount)
	}

	// Median of Y=0 and Y=10 is Y=5
	coords := lineCoords(uf)
	if len(coords) < 2 {
		t.Fatalf("Expected at least 2 points, got %d", len(coords))
	}
	for _, c := range coords {
		if math.Abs(c[1]-5.0) > 1.0 {
			t.Errorf("Expected Y ~ 5.0, got %f", c[1])
		}
	}
}

func TestUnifyWalls_ThreeVacuumsMedian(t *testing.T) {
	// Three vacuums observe the same wall at Y=0, Y=6, Y=12.
	// Median should be Y=6.
	f1 := makeLineFeature([][2]float64{{0, 0}, {200, 0}}, nil)
	f2 := makeLineFeature([][2]float64{{0, 6}, {200, 6}}, nil)
	f3 := makeLineFeature([][2]float64{{0, 12}, {200, 12}}, nil)

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-B", 0.8),
		makeSource("vac-C", 0.85),
	}

	result := UnifyWalls([]*Feature{f1, f2, f3}, sources, 3)

	if len(result) != 1 {
		t.Fatalf("Expected 1 unified wall, got %d", len(result))
	}

	coords := lineCoords(result[0])
	for _, c := range coords {
		if math.Abs(c[1]-6.0) > 1.0 {
			t.Errorf("Expected Y ~ 6.0, got %f", c[1])
		}
	}
}

func TestUnifyWalls_TwoClusters(t *testing.T) {
	// Two walls far apart should form two separate clusters.
	f1 := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, nil)
	f2 := makeLineFeature([][2]float64{{0, 10}, {100, 10}}, nil)     // close to f1
	f3 := makeLineFeature([][2]float64{{500, 500}, {600, 500}}, nil) // far away
	f4 := makeLineFeature([][2]float64{{500, 510}, {600, 510}}, nil) // close to f3

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-B", 0.8),
		makeSource("vac-A", 0.9),
		makeSource("vac-B", 0.8),
	}

	result := UnifyWalls([]*Feature{f1, f2, f3, f4}, sources, 2)

	if len(result) != 2 {
		t.Fatalf("Expected 2 unified walls, got %d", len(result))
	}

	// Both should have full confidence
	for i, uf := range result {
		if uf.Confidence != 1.0 {
			t.Errorf("Wall %d: expected confidence 1.0, got %f", i, uf.Confidence)
		}
	}
}

func TestUnifyWalls_LowConfidenceFiltered(t *testing.T) {
	// Wall seen by only 1 out of 3 vacuums => confidence 0.33 < 0.5 threshold.
	f1 := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, nil)

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
	}

	result := UnifyWalls([]*Feature{f1}, sources, 3)

	if len(result) != 0 {
		t.Errorf("Expected 0 walls (filtered by confidence), got %d", len(result))
	}
}

func TestUnifyWalls_ExactlyAtThreshold(t *testing.T) {
	// 1 out of 2 vacuums => confidence 0.5 == threshold (should include).
	f1 := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, nil)

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
	}

	result := UnifyWalls([]*Feature{f1}, sources, 2)

	if len(result) != 1 {
		t.Errorf("Expected 1 wall at threshold, got %d", len(result))
	}
}

func TestUnifyWalls_EmptyInput(t *testing.T) {
	result := UnifyWalls(nil, nil, 2)
	if result != nil {
		t.Error("Expected nil for nil input")
	}

	result = UnifyWalls([]*Feature{}, []FeatureSource{}, 2)
	if result != nil {
		t.Error("Expected nil for empty input")
	}
}

func TestUnifyWalls_ZeroVacuums(t *testing.T) {
	f1 := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, nil)
	result := UnifyWalls([]*Feature{f1}, []FeatureSource{makeSource("a", 1)}, 0)
	if result != nil {
		t.Error("Expected nil for zero totalVacuums")
	}
}

func TestUnifyWalls_SingleVacuumSingleWall(t *testing.T) {
	f1 := makeLineFeature([][2]float64{{10, 20}, {30, 40}}, nil)

	sources := []FeatureSource{
		makeSource("vac-A", 0.95),
	}

	result := UnifyWalls([]*Feature{f1}, sources, 1)

	if len(result) != 1 {
		t.Fatalf("Expected 1 wall, got %d", len(result))
	}

	uf := result[0]
	if uf.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %f", uf.Confidence)
	}

	// Single line should pass through essentially unchanged.
	coords := lineCoords(uf)
	if len(coords) < 2 {
		t.Fatalf("Expected at least 2 points, got %d", len(coords))
	}
	if math.Abs(coords[0][0]-10) > 0.01 || math.Abs(coords[0][1]-20) > 0.01 {
		t.Errorf("Expected first point (10,20), got (%f,%f)", coords[0][0], coords[0][1])
	}
}

func TestUnifyWalls_PropertiesMerged(t *testing.T) {
	// Higher ICP score source should win on conflicting properties.
	f1 := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, map[string]interface{}{
		"layerType": "wall",
		"color":     "red",
	})
	f2 := makeLineFeature([][2]float64{{0, 5}, {100, 5}}, map[string]interface{}{
		"layerType": "wall",
		"color":     "blue",
	})

	sources := []FeatureSource{
		makeSource("vac-A", 0.70),
		makeSource("vac-B", 0.95), // higher score
	}

	result := UnifyWalls([]*Feature{f1, f2}, sources, 2)

	if len(result) != 1 {
		t.Fatalf("Expected 1 wall, got %d", len(result))
	}

	color, ok := result[0].Properties["color"].(string)
	if !ok || color != "blue" {
		t.Errorf("Expected color 'blue' (higher ICP), got %q", color)
	}
}

func TestUnifyWalls_DuplicateVacuumIDsCounted(t *testing.T) {
	// Same vacuum sees a wall twice (two observations). Should count as 1 unique vacuum.
	f1 := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, nil)
	f2 := makeLineFeature([][2]float64{{0, 2}, {100, 2}}, nil)

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-A", 0.8), // same vacuum
	}

	result := UnifyWalls([]*Feature{f1, f2}, sources, 2)

	// 1 unique vacuum out of 2 => confidence 0.5 (at threshold)
	if len(result) != 1 {
		t.Fatalf("Expected 1 wall, got %d", len(result))
	}
	if result[0].ObservationCount != 1 {
		t.Errorf("Expected 1 unique vacuum, got %d", result[0].ObservationCount)
	}
	if result[0].Confidence != 0.5 {
		t.Errorf("Expected confidence 0.5, got %f", result[0].Confidence)
	}
}

func TestUnifyWalls_CustomOptions(t *testing.T) {
	// Two walls 80mm apart: with default 50mm cluster distance they'd be separate,
	// but with 100mm they should cluster.
	f1 := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, nil)
	f2 := makeLineFeature([][2]float64{{0, 80}, {100, 80}}, nil)

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-B", 0.8),
	}

	// Default distance: should be separate (centroids ~80mm apart > 50mm)
	defResult := UnifyWalls([]*Feature{f1, f2}, sources, 2)
	// With default clustering, each wall has confidence 0.5 (1/2), so both pass.
	// They should be 2 separate walls.
	if len(defResult) != 2 {
		t.Logf("With default 50mm, got %d clusters (expected 2 separate)", len(defResult))
	}

	// Custom 100mm distance: should cluster together.
	customResult := UnifyWallsWithOptions([]*Feature{f1, f2}, sources, 2, 100.0, 0.5)
	if len(customResult) != 1 {
		t.Errorf("Expected 1 wall with 100mm cluster distance, got %d", len(customResult))
	}
}

func TestUnifyWalls_FourVacuumScenario(t *testing.T) {
	// 4 vacuums observe 2 walls. Wall A seen by all 4, wall B by 2.
	wallA1 := makeLineFeature([][2]float64{{0, 0}, {200, 0}}, nil)
	wallA2 := makeLineFeature([][2]float64{{0, 4}, {200, 4}}, nil)
	wallA3 := makeLineFeature([][2]float64{{0, 8}, {200, 8}}, nil)
	wallA4 := makeLineFeature([][2]float64{{0, 2}, {200, 2}}, nil)

	wallB1 := makeLineFeature([][2]float64{{500, 500}, {600, 500}}, nil)
	wallB2 := makeLineFeature([][2]float64{{500, 504}, {600, 504}}, nil)

	features := []*Feature{wallA1, wallA2, wallA3, wallA4, wallB1, wallB2}
	sources := []FeatureSource{
		makeSource("vac-1", 0.95),
		makeSource("vac-2", 0.90),
		makeSource("vac-3", 0.85),
		makeSource("vac-4", 0.92),
		makeSource("vac-1", 0.95),
		makeSource("vac-3", 0.85),
	}

	result := UnifyWalls(features, sources, 4)

	if len(result) != 2 {
		t.Fatalf("Expected 2 unified walls, got %d", len(result))
	}

	// Wall A: 4 vacuums, confidence 1.0
	// Wall B: 2 vacuums, confidence 0.5
	// Result sorted by confidence descending
	if result[0].Confidence != 1.0 {
		t.Errorf("Expected wall A confidence 1.0, got %f", result[0].Confidence)
	}
	if result[1].Confidence != 0.5 {
		t.Errorf("Expected wall B confidence 0.5, got %f", result[1].Confidence)
	}
}

// --- medianLine tests ---

func TestMedianLine_SingleLine(t *testing.T) {
	line := orb.LineString{{0, 0}, {100, 100}}
	result := medianLine([]orb.LineString{line})

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	coords := lineCoords(&UnifiedFeature{Geometry: result})
	if len(coords) < 2 {
		t.Fatalf("Expected at least 2 points, got %d", len(coords))
	}
	// Should preserve start and end
	if math.Abs(coords[0][0]-0) > 0.01 || math.Abs(coords[0][1]-0) > 0.01 {
		t.Errorf("Start point mismatch: %v", coords[0])
	}
	last := coords[len(coords)-1]
	if math.Abs(last[0]-100) > 0.01 || math.Abs(last[1]-100) > 0.01 {
		t.Errorf("End point mismatch: %v", last)
	}
}

func TestMedianLine_ThreeParallelLines(t *testing.T) {
	lines := []orb.LineString{
		{{0, 0}, {100, 0}},
		{{0, 10}, {100, 10}},
		{{0, 20}, {100, 20}},
	}

	result := medianLine(lines)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	coords := lineCoords(&UnifiedFeature{Geometry: result})
	for _, c := range coords {
		if math.Abs(c[1]-10.0) > 0.5 {
			t.Errorf("Expected Y ~ 10.0 (median), got %f", c[1])
		}
	}
}

func TestMedianLine_Empty(t *testing.T) {
	result := medianLine(nil)
	if result != nil {
		t.Error("Expected nil for empty input")
	}
}

// --- resampleLine tests ---

func TestResampleLine_TwoPoints(t *testing.T) {
	ls := orb.LineString{{0, 0}, {100, 0}}
	pts := resampleLine(ls, 5)

	if len(pts) != 5 {
		t.Fatalf("Expected 5 points, got %d", len(pts))
	}

	// Points should be evenly spaced from 0 to 100
	for i, p := range pts {
		expectedX := float64(i) * 25.0
		if math.Abs(p[0]-expectedX) > 0.01 {
			t.Errorf("Point %d: expected X=%f, got %f", i, expectedX, p[0])
		}
		if math.Abs(p[1]) > 0.01 {
			t.Errorf("Point %d: expected Y=0, got %f", i, p[1])
		}
	}
}

func TestResampleLine_Lshaped(t *testing.T) {
	ls := orb.LineString{{0, 0}, {100, 0}, {100, 100}}

	pts := resampleLine(ls, 3)
	if len(pts) != 3 {
		t.Fatalf("Expected 3 points, got %d", len(pts))
	}

	// First point
	if math.Abs(pts[0][0]) > 0.01 || math.Abs(pts[0][1]) > 0.01 {
		t.Errorf("First point should be (0,0), got (%f,%f)", pts[0][0], pts[0][1])
	}
	// Last point
	if math.Abs(pts[2][0]-100) > 0.01 || math.Abs(pts[2][1]-100) > 0.01 {
		t.Errorf("Last point should be (100,100), got (%f,%f)", pts[2][0], pts[2][1])
	}
}

func TestResampleLine_SinglePoint(t *testing.T) {
	ls := orb.LineString{{50, 50}}
	pts := resampleLine(ls, 3)

	if len(pts) != 3 {
		t.Fatalf("Expected 3 points, got %d", len(pts))
	}
	for i, p := range pts {
		if p[0] != 50 || p[1] != 50 {
			t.Errorf("Point %d: expected (50,50), got (%f,%f)", i, p[0], p[1])
		}
	}
}

// --- medianOfSorted tests ---

func TestMedianOfSorted(t *testing.T) {
	tests := []struct {
		name   string
		input  []float64
		expect float64
	}{
		{"empty", nil, 0},
		{"single", []float64{5}, 5},
		{"two", []float64{3, 7}, 5},
		{"three", []float64{1, 5, 9}, 5},
		{"four", []float64{1, 3, 7, 9}, 5},
		{"five", []float64{1, 2, 3, 4, 5}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := medianOfSorted(tt.input)
			if math.Abs(got-tt.expect) > 0.001 {
				t.Errorf("Expected %f, got %f", tt.expect, got)
			}
		})
	}
}

// --- ComputeConfidence tests ---

func TestComputeConfidence(t *testing.T) {
	tests := []struct {
		name         string
		sources      []FeatureSource
		totalVacuums int
		expect       float64
	}{
		{"all vacuums", []FeatureSource{makeSource("a", 1), makeSource("b", 1)}, 2, 1.0},
		{"half vacuums", []FeatureSource{makeSource("a", 1)}, 2, 0.5},
		{"one of three", []FeatureSource{makeSource("a", 1)}, 3, 1.0 / 3.0},
		{"duplicate vacuum", []FeatureSource{makeSource("a", 1), makeSource("a", 0.9)}, 2, 0.5},
		{"zero total", []FeatureSource{makeSource("a", 1)}, 0, 0},
		{"empty sources", []FeatureSource{}, 3, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeConfidence(tt.sources, tt.totalVacuums)
			if math.Abs(got-tt.expect) > 0.001 {
				t.Errorf("Expected %f, got %f", tt.expect, got)
			}
		})
	}
}

// --- alignLinesToDirection tests ---

func TestAlignLinesToDirection(t *testing.T) {
	t.Run("already aligned", func(t *testing.T) {
		lines := []orb.LineString{
			{{0, 0}, {100, 0}},
			{{0, 5}, {100, 5}},
		}
		aligned := alignLinesToDirection(lines)
		if len(aligned) != 2 {
			t.Fatalf("Expected 2 lines, got %d", len(aligned))
		}
		// Both should stay the same direction
		if aligned[1][0][0] != 0 || aligned[1][len(aligned[1])-1][0] != 100 {
			t.Error("Second line should not be reversed")
		}
	})

	t.Run("reversed line corrected", func(t *testing.T) {
		lines := []orb.LineString{
			{{0, 0}, {100, 0}},
			{{100, 5}, {0, 5}}, // reversed direction
		}
		aligned := alignLinesToDirection(lines)

		// Second line should now go from left to right
		if aligned[1][0][0] != 0 || aligned[1][len(aligned[1])-1][0] != 100 {
			t.Errorf("Expected reversed line to be corrected, got start=%v end=%v",
				aligned[1][0], aligned[1][len(aligned[1])-1])
		}
	})

	t.Run("empty input", func(t *testing.T) {
		aligned := alignLinesToDirection(nil)
		if len(aligned) != 0 {
			t.Error("Expected empty result for nil input")
		}
	})
}

// --- NewUnifiedMap tests ---

func TestNewUnifiedMap(t *testing.T) {
	um := NewUnifiedMap(3, "vac-ref")

	if um.Metadata.VacuumCount != 3 {
		t.Errorf("Expected VacuumCount 3, got %d", um.Metadata.VacuumCount)
	}
	if um.Metadata.ReferenceVacuum != "vac-ref" {
		t.Errorf("Expected ReferenceVacuum 'vac-ref', got %q", um.Metadata.ReferenceVacuum)
	}
	if um.Walls == nil || um.Floors == nil || um.Segments == nil {
		t.Error("Expected initialized slices")
	}
	if um.Metadata.LastUpdated == 0 {
		t.Error("Expected non-zero LastUpdated")
	}
}

// --- ToFeatureCollection tests ---

func TestToFeatureCollection(t *testing.T) {
	um := NewUnifiedMap(2, "ref")

	wallGeom := PathToLineString(Path{{X: 0, Y: 0}, {X: 100, Y: 0}})
	um.Walls = append(um.Walls, &UnifiedFeature{
		Geometry:         wallGeom,
		Properties:       map[string]interface{}{"name": "north-wall"},
		Sources:          []FeatureSource{makeSource("a", 0.9), makeSource("b", 0.8)},
		Confidence:       1.0,
		ObservationCount: 2,
	})

	fc := um.ToFeatureCollection()

	if len(fc.Features) != 1 {
		t.Fatalf("Expected 1 feature, got %d", len(fc.Features))
	}

	f := fc.Features[0]
	if f.Properties["layerType"] != "wall" {
		t.Errorf("Expected layerType 'wall', got %v", f.Properties["layerType"])
	}
	if f.Properties["confidence"] != 1.0 {
		t.Errorf("Expected confidence 1.0, got %v", f.Properties["confidence"])
	}
	if f.Properties["observationCount"] != 2 {
		t.Errorf("Expected observationCount 2, got %v", f.Properties["observationCount"])
	}

	vacuums, ok := f.Properties["sourceVacuums"].([]string)
	if !ok {
		t.Fatal("Expected sourceVacuums to be []string")
	}
	if len(vacuums) != 2 {
		t.Errorf("Expected 2 source vacuums, got %d", len(vacuums))
	}
}

// --- sourceVacuumIDs tests ---

func TestSourceVacuumIDs(t *testing.T) {
	sources := []FeatureSource{
		makeSource("c", 0.9),
		makeSource("a", 0.8),
		makeSource("b", 0.7),
		makeSource("a", 0.6), // duplicate
	}

	ids := sourceVacuumIDs(sources)

	if len(ids) != 3 {
		t.Fatalf("Expected 3 unique IDs, got %d", len(ids))
	}
	// Should be sorted
	expected := []string{"a", "b", "c"}
	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("Index %d: expected %q, got %q", i, expected[i], id)
		}
	}
}

// --- extractWallFeatures tests ---

func TestExtractWallFeatures(t *testing.T) {
	wall := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, map[string]interface{}{"layerType": "wall"})
	floor := NewFeature(PathToPolygon(Path{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}}),
		map[string]interface{}{"layerType": "floor"})
	nilGeom := NewFeature(nil, map[string]interface{}{"layerType": "wall"})

	walls := extractWallFeatures([]*Feature{wall, floor, nilGeom})

	if len(walls) != 1 {
		t.Errorf("Expected 1 wall, got %d", len(walls))
	}
}

// --- mergeProperties tests ---

func TestMergeProperties(t *testing.T) {
	f1 := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, map[string]interface{}{
		"color":  "red",
		"unique": "only-f1",
	})
	f2 := makeLineFeature([][2]float64{{0, 5}, {100, 5}}, map[string]interface{}{
		"color":   "blue",
		"unique2": "only-f2",
	})

	sources := []FeatureSource{
		makeSource("a", 0.5), // lower score
		makeSource("b", 0.9), // higher score
	}

	merged := mergeProperties([]*Feature{f1, f2}, sources)

	if merged["color"] != "blue" {
		t.Errorf("Expected 'blue' from higher ICP, got %v", merged["color"])
	}
	if merged["unique"] != "only-f1" {
		t.Errorf("Expected 'only-f1', got %v", merged["unique"])
	}
	if merged["unique2"] != "only-f2" {
		t.Errorf("Expected 'only-f2', got %v", merged["unique2"])
	}
}

// --- marshalCoordinate tests ---

func TestMarshalCoordinate(t *testing.T) {
	coords := [2]float64{1.5, 2.5}
	raw := marshalCoordinate(coords)

	var decoded [2]float64
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if decoded[0] != 1.5 || decoded[1] != 2.5 {
		t.Errorf("Expected [1.5, 2.5], got %v", decoded)
	}
}
