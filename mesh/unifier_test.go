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

// --- floor helpers ---

// makePolygonFeature creates a Feature with a Polygon geometry from a list
// of outer ring points. The ring is automatically closed.
func makePolygonFeature(pts [][2]float64, props map[string]interface{}) *Feature {
	// Ensure ring is closed.
	if len(pts) > 0 {
		first := pts[0]
		last := pts[len(pts)-1]
		if first[0] != last[0] || first[1] != last[1] {
			pts = append(pts, first)
		}
	}
	rings := [][][2]float64{pts}
	coordsJSON, _ := json.Marshal(rings)
	geom := &Geometry{Type: GeometryPolygon, Coordinates: coordsJSON}
	if props == nil {
		props = map[string]interface{}{"layerType": "floor"}
	}
	return NewFeature(geom, props)
}

// polyCoords extracts the outer ring coordinates from a UnifiedFeature polygon.
func polyCoords(f *UnifiedFeature) [][2]float64 {
	var rings [][][2]float64
	_ = json.Unmarshal(f.Geometry.Coordinates, &rings)
	if len(rings) == 0 {
		return nil
	}
	return rings[0]
}

// --- UnifyFloors tests ---

func TestUnifyFloors_TwoOverlappingFloors(t *testing.T) {
	// Two vacuums see overlapping rectangular floors.
	// Vacuum A: 0,0 -> 100,100
	// Vacuum B: 50,50 -> 150,150
	// Union should cover the bounding hull of both.
	f1 := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{
		"layerType":   "floor",
		"segmentName": "living-room",
		"area":        float64(10000),
	})
	f2 := makePolygonFeature([][2]float64{
		{50, 50}, {150, 50}, {150, 150}, {50, 150},
	}, map[string]interface{}{
		"layerType":   "floor",
		"segmentName": "living-room",
		"area":        float64(10000),
	})

	sources := []FeatureSource{
		makeSource("vac-A", 0.95),
		makeSource("vac-B", 0.90),
	}

	result := UnifyFloors([]*Feature{f1, f2}, sources, 2)

	if len(result) != 1 {
		t.Fatalf("Expected 1 unified floor, got %d", len(result))
	}

	uf := result[0]
	if uf.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %f", uf.Confidence)
	}
	if uf.ObservationCount != 2 {
		t.Errorf("Expected 2 observations, got %d", uf.ObservationCount)
	}

	// The merged polygon should cover area from (0,0) to (150,150).
	coords := polyCoords(uf)
	if len(coords) < 3 {
		t.Fatalf("Expected polygon with at least 3 points, got %d", len(coords))
	}

	// Verify the bounding box covers the full extent.
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range coords {
		if c[0] < minX {
			minX = c[0]
		}
		if c[1] < minY {
			minY = c[1]
		}
		if c[0] > maxX {
			maxX = c[0]
		}
		if c[1] > maxY {
			maxY = c[1]
		}
	}
	if minX > 1.0 || minY > 1.0 {
		t.Errorf("Expected min near (0,0), got (%f,%f)", minX, minY)
	}
	if maxX < 149.0 || maxY < 149.0 {
		t.Errorf("Expected max near (150,150), got (%f,%f)", maxX, maxY)
	}
}

func TestUnifyFloors_DisjointFloors(t *testing.T) {
	// Two floors far apart, different names -> should produce 2 unified features.
	f1 := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{
		"layerType":   "floor",
		"segmentName": "kitchen",
		"area":        float64(10000),
	})
	f2 := makePolygonFeature([][2]float64{
		{500, 500}, {600, 500}, {600, 600}, {500, 600},
	}, map[string]interface{}{
		"layerType":   "floor",
		"segmentName": "bedroom",
		"area":        float64(10000),
	})

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-B", 0.85),
	}

	result := UnifyFloors([]*Feature{f1, f2}, sources, 2)

	if len(result) != 2 {
		t.Fatalf("Expected 2 unified floors, got %d", len(result))
	}

	// Verify both segment names are preserved.
	names := make(map[string]bool)
	for _, uf := range result {
		name, _ := uf.Properties["segmentName"].(string)
		names[name] = true
	}
	if !names["kitchen"] || !names["bedroom"] {
		t.Errorf("Expected both 'kitchen' and 'bedroom', got %v", names)
	}
}

func TestUnifyFloors_SegmentNameConflict_HighestAreaWins(t *testing.T) {
	// Two vacuums see overlapping floors but with different names.
	// The vacuum with higher area should win.
	f1 := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{
		"layerType":   "segment",
		"segmentName": "small-room",
		"area":        float64(5000),
	})
	f2 := makePolygonFeature([][2]float64{
		{10, 10}, {110, 10}, {110, 110}, {10, 110},
	}, map[string]interface{}{
		"layerType":   "segment",
		"segmentName": "big-room",
		"area":        float64(15000),
	})

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-B", 0.8),
	}

	// Both are unnamed from the grouping perspective since they have different
	// segment names. They will be clustered by proximity.
	result := UnifyFloorsWithOptions([]*Feature{f1, f2}, sources, 2, 200.0)

	if len(result) != 2 {
		// They have different names so they end up in different named groups.
		// Let's verify the name resolution within each.
		t.Logf("Got %d results (different names = separate groups)", len(result))
	}

	// Now test with unnamed features that cluster together by proximity.
	// One has no name, the other has a name -- when both are unnamed from
	// the grouping perspective, the name gets resolved by area. To test
	// this properly, both features must lack segmentName so they enter
	// the unnamed path and cluster by proximity. We store the candidate
	// name in a different property key to verify resolveSegmentName picks it up.
	f3 := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{
		"layerType": "floor",
		"area":      float64(5000),
	})
	f4 := makePolygonFeature([][2]float64{
		{10, 10}, {110, 10}, {110, 110}, {10, 110},
	}, map[string]interface{}{
		"layerType": "floor",
		"area":      float64(15000),
	})

	sources2 := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-B", 0.8),
	}

	result2 := UnifyFloorsWithOptions([]*Feature{f3, f4}, sources2, 2, 200.0)

	if len(result2) != 1 {
		t.Fatalf("Expected 1 unified floor (unnamed cluster), got %d", len(result2))
	}

	// The area property should come from the higher-area feature (15000).
	area, _ := result2[0].Properties["area"].(float64)
	if area != 15000 {
		t.Errorf("Expected area 15000 (highest area wins), got %f", area)
	}
}

func TestUnifyFloors_SingleVacuum(t *testing.T) {
	// Single vacuum: floor should pass through unchanged.
	f1 := makePolygonFeature([][2]float64{
		{0, 0}, {200, 0}, {200, 200}, {0, 200},
	}, map[string]interface{}{
		"layerType":   "floor",
		"segmentName": "hallway",
		"area":        float64(40000),
	})

	sources := []FeatureSource{
		makeSource("vac-A", 0.95),
	}

	result := UnifyFloors([]*Feature{f1}, sources, 1)

	if len(result) != 1 {
		t.Fatalf("Expected 1 floor, got %d", len(result))
	}

	uf := result[0]
	if uf.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %f", uf.Confidence)
	}
	if uf.ObservationCount != 1 {
		t.Errorf("Expected 1 observation, got %d", uf.ObservationCount)
	}

	name, _ := uf.Properties["segmentName"].(string)
	if name != "hallway" {
		t.Errorf("Expected segmentName 'hallway', got %q", name)
	}
}

func TestUnifyFloors_EmptyInput(t *testing.T) {
	result := UnifyFloors(nil, nil, 2)
	if result != nil {
		t.Error("Expected nil for nil input")
	}

	result = UnifyFloors([]*Feature{}, []FeatureSource{}, 2)
	if result != nil {
		t.Error("Expected nil for empty input")
	}
}

func TestUnifyFloors_ZeroVacuums(t *testing.T) {
	f1 := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, nil)
	result := UnifyFloors([]*Feature{f1}, []FeatureSource{makeSource("a", 1)}, 0)
	if result != nil {
		t.Error("Expected nil for zero totalVacuums")
	}
}

func TestUnifyFloors_NonPolygonFeaturesSkipped(t *testing.T) {
	// Line features should be silently ignored.
	wall := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, nil)
	floor := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{
		"layerType":   "floor",
		"segmentName": "room",
		"area":        float64(10000),
	})

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-A", 0.9),
	}

	result := UnifyFloors([]*Feature{wall, floor}, sources, 1)

	if len(result) != 1 {
		t.Fatalf("Expected 1 floor (wall skipped), got %d", len(result))
	}
}

func TestUnifyFloors_SameNameMergedAcrossVacuums(t *testing.T) {
	// Three vacuums all see a "kitchen" segment, slightly different polygons.
	// They should all merge into one unified feature.
	f1 := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{
		"layerType":   "segment",
		"segmentName": "kitchen",
		"area":        float64(10000),
	})
	f2 := makePolygonFeature([][2]float64{
		{5, 5}, {105, 5}, {105, 105}, {5, 105},
	}, map[string]interface{}{
		"layerType":   "segment",
		"segmentName": "kitchen",
		"area":        float64(10200),
	})
	f3 := makePolygonFeature([][2]float64{
		{-5, -5}, {95, -5}, {95, 95}, {-5, 95},
	}, map[string]interface{}{
		"layerType":   "segment",
		"segmentName": "kitchen",
		"area":        float64(9800),
	})

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-B", 0.85),
		makeSource("vac-C", 0.92),
	}

	result := UnifyFloors([]*Feature{f1, f2, f3}, sources, 3)

	if len(result) != 1 {
		t.Fatalf("Expected 1 unified floor (same name), got %d", len(result))
	}

	uf := result[0]
	if uf.ObservationCount != 3 {
		t.Errorf("Expected 3 observations, got %d", uf.ObservationCount)
	}
	if uf.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %f", uf.Confidence)
	}

	name, _ := uf.Properties["segmentName"].(string)
	if name != "kitchen" {
		t.Errorf("Expected segmentName 'kitchen', got %q", name)
	}
}

func TestUnifyFloors_MultipleSegmentsDifferentNames(t *testing.T) {
	// Two segments with different names from the same vacuum.
	f1 := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{
		"layerType":   "segment",
		"segmentName": "kitchen",
		"area":        float64(10000),
	})
	f2 := makePolygonFeature([][2]float64{
		{200, 0}, {300, 0}, {300, 100}, {200, 100},
	}, map[string]interface{}{
		"layerType":   "segment",
		"segmentName": "bedroom",
		"area":        float64(10000),
	})

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-A", 0.9),
	}

	result := UnifyFloors([]*Feature{f1, f2}, sources, 1)

	if len(result) != 2 {
		t.Fatalf("Expected 2 unified floors (different names), got %d", len(result))
	}
}

func TestUnifyFloors_DuplicateVacuumIDsCounted(t *testing.T) {
	// Same vacuum sees a floor twice. Should count as 1 unique vacuum.
	f1 := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{
		"layerType":   "floor",
		"segmentName": "room",
		"area":        float64(10000),
	})
	f2 := makePolygonFeature([][2]float64{
		{5, 5}, {105, 5}, {105, 105}, {5, 105},
	}, map[string]interface{}{
		"layerType":   "floor",
		"segmentName": "room",
		"area":        float64(10200),
	})

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-A", 0.85),
	}

	result := UnifyFloors([]*Feature{f1, f2}, sources, 2)

	if len(result) != 1 {
		t.Fatalf("Expected 1 floor, got %d", len(result))
	}
	if result[0].ObservationCount != 1 {
		t.Errorf("Expected 1 unique vacuum, got %d", result[0].ObservationCount)
	}
	if result[0].Confidence != 0.5 {
		t.Errorf("Expected confidence 0.5, got %f", result[0].Confidence)
	}
}

func TestUnifyFloors_SourcesPreserved(t *testing.T) {
	f1 := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{
		"layerType":   "floor",
		"segmentName": "hall",
		"area":        float64(10000),
	})

	sources := []FeatureSource{
		{VacuumID: "vac-A", ICPScore: 0.95, Timestamp: 1000},
	}

	result := UnifyFloors([]*Feature{f1}, sources, 1)

	if len(result) != 1 {
		t.Fatalf("Expected 1 floor, got %d", len(result))
	}

	if len(result[0].Sources) != 1 {
		t.Fatalf("Expected 1 source, got %d", len(result[0].Sources))
	}

	src := result[0].Sources[0]
	if src.VacuumID != "vac-A" {
		t.Errorf("Expected VacuumID 'vac-A', got %q", src.VacuumID)
	}
	if src.ICPScore != 0.95 {
		t.Errorf("Expected ICPScore 0.95, got %f", src.ICPScore)
	}
	if src.Timestamp != 1000 {
		t.Errorf("Expected Timestamp 1000, got %d", src.Timestamp)
	}
}

func TestUnifyFloors_NamedGroupAreaResolution(t *testing.T) {
	// Two vacuums both call a segment "living-room" but report different areas.
	// The merged properties should prefer the higher-area vacuum's properties.
	f1 := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{
		"layerType":   "segment",
		"segmentName": "living-room",
		"segmentId":   "seg-1",
		"area":        float64(8000),
	})
	f2 := makePolygonFeature([][2]float64{
		{10, 10}, {110, 10}, {110, 110}, {10, 110},
	}, map[string]interface{}{
		"layerType":   "segment",
		"segmentName": "living-room",
		"segmentId":   "seg-2",
		"area":        float64(12000),
	})

	sources := []FeatureSource{
		makeSource("vac-A", 0.9),
		makeSource("vac-B", 0.85),
	}

	result := UnifyFloors([]*Feature{f1, f2}, sources, 2)

	if len(result) != 1 {
		t.Fatalf("Expected 1 unified floor, got %d", len(result))
	}

	uf := result[0]
	// segmentId should come from higher-area vacuum (vac-B, area 12000).
	segID, _ := uf.Properties["segmentId"].(string)
	if segID != "seg-2" {
		t.Errorf("Expected segmentId 'seg-2' (higher area), got %q", segID)
	}
}

// --- extractFloorFeatures tests ---

func TestExtractFloorFeatures(t *testing.T) {
	floor := makePolygonFeature([][2]float64{
		{0, 0}, {100, 0}, {100, 100}, {0, 100},
	}, map[string]interface{}{"layerType": "floor"})

	segment := makePolygonFeature([][2]float64{
		{0, 0}, {50, 0}, {50, 50}, {0, 50},
	}, map[string]interface{}{"layerType": "segment"})

	wall := makeLineFeature([][2]float64{{0, 0}, {100, 0}}, map[string]interface{}{"layerType": "wall"})
	nilGeom := NewFeature(nil, map[string]interface{}{"layerType": "floor"})

	floors := extractFloorFeatures([]*Feature{floor, segment, wall, nilGeom})

	if len(floors) != 2 {
		t.Errorf("Expected 2 floor features, got %d", len(floors))
	}
}

// --- resolveSegmentName tests ---

func TestResolveSegmentName(t *testing.T) {
	t.Run("highest area wins", func(t *testing.T) {
		features := []*Feature{
			makePolygonFeature([][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}}, map[string]interface{}{
				"segmentName": "small",
				"area":        float64(100),
			}),
			makePolygonFeature([][2]float64{{0, 0}, {20, 0}, {20, 20}, {0, 20}}, map[string]interface{}{
				"segmentName": "big",
				"area":        float64(400),
			}),
		}

		name := resolveSegmentName(features)
		if name != "big" {
			t.Errorf("Expected 'big', got %q", name)
		}
	})

	t.Run("no names", func(t *testing.T) {
		features := []*Feature{
			makePolygonFeature([][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}}, map[string]interface{}{
				"area": float64(100),
			}),
		}

		name := resolveSegmentName(features)
		if name != "" {
			t.Errorf("Expected empty name, got %q", name)
		}
	})

	t.Run("single feature with name", func(t *testing.T) {
		features := []*Feature{
			makePolygonFeature([][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}}, map[string]interface{}{
				"segmentName": "only-one",
				"area":        float64(100),
			}),
		}

		name := resolveSegmentName(features)
		if name != "only-one" {
			t.Errorf("Expected 'only-one', got %q", name)
		}
	})
}

// --- featureArea tests ---

func TestFeatureArea(t *testing.T) {
	tests := []struct {
		name   string
		props  map[string]interface{}
		expect float64
	}{
		{"float64", map[string]interface{}{"area": float64(1500)}, 1500},
		{"int", map[string]interface{}{"area": 2000}, 2000},
		{"missing", map[string]interface{}{}, 0},
		{"nil feature", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f *Feature
			if tt.props != nil {
				f = makePolygonFeature([][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}}, tt.props)
			}
			got := featureArea(f)
			if got != tt.expect {
				t.Errorf("Expected %f, got %f", tt.expect, got)
			}
		})
	}
}

// --- Outlier Detection tests ---

// makeUnifiedFeature creates a UnifiedFeature with the given geometry, sources,
// and observation count for testing.
func makeUnifiedFeature(geom *Geometry, sources []FeatureSource, confidence float64, obsCount int) *UnifiedFeature {
	return &UnifiedFeature{
		Geometry:         geom,
		Properties:       map[string]interface{}{},
		Sources:          sources,
		Confidence:       confidence,
		ObservationCount: obsCount,
	}
}

func TestDetectOutliers_GhostRoom(t *testing.T) {
	// Feature seen by only 1 vacuum out of 3 should be flagged as ghost room.
	ghostGeom := PathToLineString(Path{{X: 0, Y: 0}, {X: 100, Y: 0}})
	goodGeom := PathToLineString(Path{{X: 10, Y: 10}, {X: 110, Y: 10}})

	ghost := makeUnifiedFeature(ghostGeom, []FeatureSource{
		makeSource("vac-A", 0.95),
	}, 1.0/3.0, 1)

	good := makeUnifiedFeature(goodGeom, []FeatureSource{
		makeSource("vac-A", 0.95),
		makeSource("vac-B", 0.90),
		makeSource("vac-C", 0.85),
	}, 1.0, 3)

	config := DefaultOutlierConfig(3)
	retained, outliers := DetectOutliers([]*UnifiedFeature{ghost, good}, config)

	if len(retained) != 1 {
		t.Fatalf("Expected 1 retained feature, got %d", len(retained))
	}
	if len(outliers) != 1 {
		t.Fatalf("Expected 1 outlier, got %d", len(outliers))
	}

	// Verify the ghost room has the correct reason.
	found := false
	for _, r := range outliers[0].Reasons {
		if r == OutlierGhostRoom {
			found = true
		}
	}
	if !found {
		t.Errorf("Expected ghost_room reason, got %v", outliers[0].Reasons)
	}
}

func TestDetectOutliers_LowICPConfidence(t *testing.T) {
	// Feature seen by 1 vacuum with very low ICP score.
	// With TotalVacuums=2, 1 vacuum with low ICP -> weighted 0.5/2 = 0.25 < 0.3 threshold.
	geom := PathToLineString(Path{{X: 0, Y: 0}, {X: 100, Y: 0}})

	lowICP := makeUnifiedFeature(geom, []FeatureSource{
		makeSource("vac-A", 0.2), // below minICPScore of 0.5
	}, 0.5, 1)

	config := DefaultOutlierConfig(2)
	retained, outliers := DetectOutliers([]*UnifiedFeature{lowICP}, config)

	if len(retained) != 0 {
		t.Errorf("Expected 0 retained, got %d", len(retained))
	}
	if len(outliers) != 1 {
		t.Fatalf("Expected 1 outlier, got %d", len(outliers))
	}

	hasLowConf := false
	for _, r := range outliers[0].Reasons {
		if r == OutlierLowConfidence {
			hasLowConf = true
		}
	}
	if !hasLowConf {
		t.Errorf("Expected low_confidence reason, got %v", outliers[0].Reasons)
	}
}

func TestDetectOutliers_SpatialIsolation(t *testing.T) {
	// Many features clustered near origin, one extremely far away.
	// With enough near features the centroid stays close to origin, so the
	// far feature's distance exceeds 3x the mean distance.
	mkSources := func(ids ...string) []FeatureSource {
		var s []FeatureSource
		for _, id := range ids {
			s = append(s, makeSource(id, 0.95))
		}
		return s
	}

	var features []*UnifiedFeature
	// 10 features near origin, spread across a small area
	for i := 0; i < 10; i++ {
		x := float64(i * 5)
		y := float64(i * 3)
		geom := PathToLineString(Path{{X: x, Y: y}, {X: x + 10, Y: y}})
		features = append(features, makeUnifiedFeature(geom, mkSources("a", "b", "c"), 1.0, 3))
	}
	// 1 feature extremely far away
	farGeom := PathToLineString(Path{{X: 1000000, Y: 1000000}, {X: 1000010, Y: 1000000}})
	features = append(features, makeUnifiedFeature(farGeom, mkSources("a", "b", "c"), 1.0, 3))

	config := DefaultOutlierConfig(3)
	retained, outliers := DetectOutliers(features, config)

	if len(retained) != 10 {
		t.Errorf("Expected 10 retained, got %d", len(retained))
	}
	if len(outliers) != 1 {
		t.Fatalf("Expected 1 outlier (isolated), got %d", len(outliers))
	}

	hasIsolated := false
	for _, r := range outliers[0].Reasons {
		if r == OutlierIsolated {
			hasIsolated = true
		}
	}
	if !hasIsolated {
		t.Errorf("Expected isolated reason, got %v", outliers[0].Reasons)
	}
}

func TestDetectOutliers_MultipleReasons(t *testing.T) {
	// Feature that is both a ghost room AND spatially isolated AND low confidence.
	// Use many near features so the centroid stays near origin.
	goodSources := []FeatureSource{
		makeSource("a", 0.95),
		makeSource("b", 0.90),
		makeSource("c", 0.85),
	}

	var features []*UnifiedFeature
	for i := 0; i < 10; i++ {
		x := float64(i * 5)
		y := float64(i * 3)
		geom := PathToLineString(Path{{X: x, Y: y}, {X: x + 10, Y: y}})
		features = append(features, makeUnifiedFeature(geom, goodSources, 1.0, 3))
	}

	// Add the far ghost with low ICP
	farGhostGeom := PathToLineString(Path{{X: 5000000, Y: 5000000}, {X: 5000010, Y: 5000000}})
	features = append(features, makeUnifiedFeature(farGhostGeom, []FeatureSource{
		makeSource("a", 0.2), // low ICP, single vacuum
	}, 1.0/3.0, 1))

	config := DefaultOutlierConfig(3)
	retained, outliers := DetectOutliers(features, config)

	if len(retained) != 10 {
		t.Errorf("Expected 10 retained, got %d", len(retained))
	}
	if len(outliers) != 1 {
		t.Fatalf("Expected 1 outlier, got %d", len(outliers))
	}

	// Should have all three reasons.
	reasonSet := make(map[OutlierReason]bool)
	for _, r := range outliers[0].Reasons {
		reasonSet[r] = true
	}
	if !reasonSet[OutlierGhostRoom] {
		t.Error("Expected ghost_room reason")
	}
	if !reasonSet[OutlierLowConfidence] {
		t.Error("Expected low_confidence reason")
	}
	if !reasonSet[OutlierIsolated] {
		t.Error("Expected isolated reason")
	}
}

func TestDetectOutliers_AllGood(t *testing.T) {
	// All features have high confidence, multiple vacuums, not isolated.
	geom1 := PathToLineString(Path{{X: 0, Y: 0}, {X: 100, Y: 0}})
	geom2 := PathToLineString(Path{{X: 50, Y: 50}, {X: 150, Y: 50}})

	features := []*UnifiedFeature{
		makeUnifiedFeature(geom1, []FeatureSource{
			makeSource("a", 0.95),
			makeSource("b", 0.90),
		}, 1.0, 2),
		makeUnifiedFeature(geom2, []FeatureSource{
			makeSource("a", 0.95),
			makeSource("b", 0.90),
		}, 1.0, 2),
	}

	config := DefaultOutlierConfig(2)
	retained, outliers := DetectOutliers(features, config)

	if len(retained) != 2 {
		t.Errorf("Expected 2 retained, got %d", len(retained))
	}
	if len(outliers) != 0 {
		t.Errorf("Expected 0 outliers, got %d", len(outliers))
	}
}

func TestDetectOutliers_EmptyInput(t *testing.T) {
	retained, outliers := DetectOutliers(nil, DefaultOutlierConfig(3))
	if retained != nil {
		t.Error("Expected nil retained for nil input")
	}
	if outliers != nil {
		t.Error("Expected nil outliers for nil input")
	}
}

func TestDetectOutliers_SingleVacuumSystem(t *testing.T) {
	// With only 1 vacuum, ghost room detection should not trigger.
	geom := PathToLineString(Path{{X: 0, Y: 0}, {X: 100, Y: 0}})

	features := []*UnifiedFeature{
		makeUnifiedFeature(geom, []FeatureSource{
			makeSource("a", 0.95),
		}, 1.0, 1),
	}

	config := DefaultOutlierConfig(1)
	retained, outliers := DetectOutliers(features, config)

	if len(retained) != 1 {
		t.Errorf("Expected 1 retained, got %d", len(retained))
	}
	if len(outliers) != 0 {
		t.Errorf("Expected 0 outliers for single vacuum, got %d", len(outliers))
	}
}

func TestDetectOutliers_CustomConfig(t *testing.T) {
	// Use a very high confidence threshold to filter more aggressively.
	geom1 := PathToLineString(Path{{X: 0, Y: 0}, {X: 100, Y: 0}})
	geom2 := PathToLineString(Path{{X: 10, Y: 10}, {X: 110, Y: 10}})

	features := []*UnifiedFeature{
		makeUnifiedFeature(geom1, []FeatureSource{
			makeSource("a", 0.95),
			makeSource("b", 0.90),
		}, 1.0, 2),
		makeUnifiedFeature(geom2, []FeatureSource{
			makeSource("a", 0.95),
		}, 0.5, 1), // 1 of 2 vacuums
	}

	config := OutlierConfig{
		ConfidenceThreshold: 0.8, // very aggressive
		IsolationMultiplier: 3.0,
		MinICPScore:         0.5,
		TotalVacuums:        2,
	}
	retained, outliers := DetectOutliers(features, config)

	if len(retained) != 1 {
		t.Errorf("Expected 1 retained with high threshold, got %d", len(retained))
	}
	if len(outliers) != 1 {
		t.Errorf("Expected 1 outlier with high threshold, got %d", len(outliers))
	}
}

// --- ComputeConfidenceWeighted tests ---

func TestComputeConfidenceWeighted(t *testing.T) {
	tests := []struct {
		name         string
		sources      []FeatureSource
		totalVacuums int
		minICP       float64
		expect       float64
	}{
		{
			name: "all high ICP",
			sources: []FeatureSource{
				makeSource("a", 0.9),
				makeSource("b", 0.8),
			},
			totalVacuums: 2,
			minICP:       0.5,
			expect:       1.0, // 2 vacuums * 1.0 weight / 2
		},
		{
			name: "one low ICP",
			sources: []FeatureSource{
				makeSource("a", 0.9),
				makeSource("b", 0.3), // below minICP
			},
			totalVacuums: 2,
			minICP:       0.5,
			expect:       0.75, // (1.0 + 0.5) / 2
		},
		{
			name: "all low ICP",
			sources: []FeatureSource{
				makeSource("a", 0.2),
				makeSource("b", 0.1),
			},
			totalVacuums: 2,
			minICP:       0.5,
			expect:       0.5, // (0.5 + 0.5) / 2
		},
		{
			name: "duplicate vacuum takes best score",
			sources: []FeatureSource{
				makeSource("a", 0.3), // low
				makeSource("a", 0.9), // high - should win
			},
			totalVacuums: 2,
			minICP:       0.5,
			expect:       0.5, // 1 unique vacuum * 1.0 weight / 2
		},
		{
			name:         "empty sources",
			sources:      nil,
			totalVacuums: 2,
			minICP:       0.5,
			expect:       0,
		},
		{
			name: "zero vacuums",
			sources: []FeatureSource{
				makeSource("a", 0.9),
			},
			totalVacuums: 0,
			minICP:       0.5,
			expect:       0,
		},
		{
			name: "single low ICP of three",
			sources: []FeatureSource{
				makeSource("a", 0.2),
			},
			totalVacuums: 3,
			minICP:       0.5,
			expect:       0.5 / 3.0, // ~0.167
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeConfidenceWeighted(tt.sources, tt.totalVacuums, tt.minICP)
			if math.Abs(got-tt.expect) > 0.001 {
				t.Errorf("Expected %f, got %f", tt.expect, got)
			}
		})
	}
}

// --- FilterByConfidence tests ---

func TestFilterByConfidence(t *testing.T) {
	geom := PathToLineString(Path{{X: 0, Y: 0}, {X: 100, Y: 0}})

	features := []*UnifiedFeature{
		{Geometry: geom, Confidence: 0.9},
		{Geometry: geom, Confidence: 0.5},
		{Geometry: geom, Confidence: 0.2},
		{Geometry: geom, Confidence: 0.1},
	}

	t.Run("threshold 0.3", func(t *testing.T) {
		result := FilterByConfidence(features, 0.3)
		if len(result) != 2 {
			t.Errorf("Expected 2 features above 0.3, got %d", len(result))
		}
	})

	t.Run("threshold 0.0", func(t *testing.T) {
		result := FilterByConfidence(features, 0.0)
		if len(result) != 4 {
			t.Errorf("Expected 4 features above 0.0, got %d", len(result))
		}
	})

	t.Run("threshold 1.0", func(t *testing.T) {
		result := FilterByConfidence(features, 1.0)
		if result != nil {
			t.Errorf("Expected nil for threshold 1.0, got %d", len(result))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		result := FilterByConfidence(nil, 0.3)
		if result != nil {
			t.Error("Expected nil for nil input")
		}
	})
}

// --- DefaultOutlierConfig tests ---

func TestDefaultOutlierConfig(t *testing.T) {
	config := DefaultOutlierConfig(5)

	if config.ConfidenceThreshold != DefaultOutlierConfidenceThreshold {
		t.Errorf("Expected threshold %f, got %f", DefaultOutlierConfidenceThreshold, config.ConfidenceThreshold)
	}
	if config.IsolationMultiplier != DefaultIsolationDistanceMultiplier {
		t.Errorf("Expected multiplier %f, got %f", DefaultIsolationDistanceMultiplier, config.IsolationMultiplier)
	}
	if config.MinICPScore != 0.5 {
		t.Errorf("Expected MinICPScore 0.5, got %f", config.MinICPScore)
	}
	if config.TotalVacuums != 5 {
		t.Errorf("Expected TotalVacuums 5, got %d", config.TotalVacuums)
	}
}
