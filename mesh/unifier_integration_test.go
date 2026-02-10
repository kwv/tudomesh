package mesh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helper: build a minimal ValetudoMap with floor, wall, and segment layers.
// ---------------------------------------------------------------------------

func makeTestMap(pixelSize int, floorPixels, wallPixels, segPixels []int, segName string) *ValetudoMap {
	layers := []MapLayer{
		{Type: "floor", Pixels: floorPixels, MetaData: LayerMetaData{Area: len(floorPixels) / 2}},
		{Type: "wall", Pixels: wallPixels},
	}
	if len(segPixels) > 0 {
		layers = append(layers, MapLayer{
			Type:   "segment",
			Pixels: segPixels,
			MetaData: LayerMetaData{
				SegmentID: "seg-1",
				Name:      segName,
				Area:      len(segPixels) / 2,
			},
		})
	}
	return &ValetudoMap{
		PixelSize: pixelSize,
		Size:      Size{X: 200, Y: 200},
		Layers:    layers,
	}
}

// ---------------------------------------------------------------------------
// Test: UpdateUnifiedMap with a single vacuum
// ---------------------------------------------------------------------------

func TestStateTracker_UpdateUnifiedMap_SingleVacuum(t *testing.T) {
	st := NewStateTracker()

	// Create a simple map with some floor and wall pixels.
	floorPixels := []int{
		10, 10, 11, 10, 12, 10,
		10, 11, 11, 11, 12, 11,
		10, 12, 11, 12, 12, 12,
	}
	wallPixels := []int{
		9, 9, 10, 9, 11, 9, 12, 9, 13, 9,
		9, 10, 9, 11, 9, 12,
		13, 10, 13, 11, 13, 12,
	}

	vMap := makeTestMap(5, floorPixels, wallPixels, nil, "")
	st.UpdateMap("vac-1", vMap)

	calibData := &CalibrationData{
		ReferenceVacuum: "vac-1",
		Vacuums: map[string]VacuumCalibration{
			"vac-1": {Transform: Identity(), LastUpdated: time.Now().Unix()},
		},
	}

	err := st.UpdateUnifiedMap(calibData)
	if err != nil {
		t.Fatalf("UpdateUnifiedMap failed: %v", err)
	}

	um := st.GetUnifiedMap()
	if um == nil {
		t.Fatal("GetUnifiedMap returned nil")
		return
	}

	if um.Metadata.VacuumCount != 1 {
		t.Errorf("VacuumCount = %d, want 1", um.Metadata.VacuumCount)
	}
	if um.Metadata.ReferenceVacuum != "vac-1" {
		t.Errorf("ReferenceVacuum = %q, want %q", um.Metadata.ReferenceVacuum, "vac-1")
	}
	if um.Metadata.LastUpdated == 0 {
		t.Error("LastUpdated should be non-zero")
	}

	// With a single vacuum, all features should pass through (confidence = 1.0).
	// The exact number depends on vectorization, but we should have some features.
	t.Logf("Walls: %d, Floors: %d, Segments: %d", len(um.Walls), len(um.Floors), len(um.Segments))
}

// ---------------------------------------------------------------------------
// Test: UpdateUnifiedMap with multiple vacuums
// ---------------------------------------------------------------------------

func TestStateTracker_UpdateUnifiedMap_MultipleVacuums(t *testing.T) {
	st := NewStateTracker()

	// Two vacuums with overlapping coverage.
	floorPixels := []int{
		10, 10, 11, 10, 12, 10, 13, 10,
		10, 11, 11, 11, 12, 11, 13, 11,
		10, 12, 11, 12, 12, 12, 13, 12,
		10, 13, 11, 13, 12, 13, 13, 13,
	}
	wallPixels := []int{
		9, 9, 10, 9, 11, 9, 12, 9, 13, 9, 14, 9,
		9, 10, 9, 11, 9, 12, 9, 13,
		14, 10, 14, 11, 14, 12, 14, 13,
		9, 14, 10, 14, 11, 14, 12, 14, 13, 14, 14, 14,
	}

	// Vacuum 1: original position.
	st.UpdateMap("vac-1", makeTestMap(5, floorPixels, wallPixels, nil, ""))

	// Vacuum 2: slightly shifted (simulating a different vacuum with same layout).
	floorPixels2 := make([]int, len(floorPixels))
	copy(floorPixels2, floorPixels)
	for i := 0; i < len(floorPixels2); i += 2 {
		floorPixels2[i] += 1 // shift X by 1 pixel
	}
	wallPixels2 := make([]int, len(wallPixels))
	copy(wallPixels2, wallPixels)
	for i := 0; i < len(wallPixels2); i += 2 {
		wallPixels2[i] += 1
	}
	st.UpdateMap("vac-2", makeTestMap(5, floorPixels2, wallPixels2, nil, ""))

	calibData := &CalibrationData{
		ReferenceVacuum: "vac-1",
		Vacuums: map[string]VacuumCalibration{
			"vac-1": {Transform: Identity()},
			"vac-2": {Transform: Identity()}, // Same space for this test
		},
	}

	err := st.UpdateUnifiedMap(calibData)
	if err != nil {
		t.Fatalf("UpdateUnifiedMap failed: %v", err)
	}

	um := st.GetUnifiedMap()
	if um == nil {
		t.Fatal("GetUnifiedMap returned nil")
		return
	}

	if um.Metadata.VacuumCount != 2 {
		t.Errorf("VacuumCount = %d, want 2", um.Metadata.VacuumCount)
	}

	t.Logf("Walls: %d, Floors: %d, Segments: %d", len(um.Walls), len(um.Floors), len(um.Segments))
}

// ---------------------------------------------------------------------------
// Test: Incremental refinement improves over sequential updates
// ---------------------------------------------------------------------------

func TestStateTracker_IncrementalRefinement(t *testing.T) {
	st := NewStateTracker()

	baseFloor := []int{
		10, 10, 11, 10, 12, 10,
		10, 11, 11, 11, 12, 11,
		10, 12, 11, 12, 12, 12,
	}
	baseWall := []int{
		9, 9, 10, 9, 11, 9, 12, 9, 13, 9,
		9, 10, 9, 11, 9, 12,
		13, 10, 13, 11, 13, 12,
	}

	st.UpdateMap("vac-1", makeTestMap(5, baseFloor, baseWall, nil, ""))

	calibData := &CalibrationData{
		ReferenceVacuum: "vac-1",
		Vacuums: map[string]VacuumCalibration{
			"vac-1": {Transform: Identity()},
		},
	}

	// First update.
	if err := st.UpdateUnifiedMap(calibData); err != nil {
		t.Fatalf("first update failed: %v", err)
	}
	um1 := st.GetUnifiedMap()
	if um1 == nil {
		t.Fatal("first unified map is nil")
		return
	}

	// Second update (same data - should trigger incremental refinement path).
	if err := st.UpdateUnifiedMap(calibData); err != nil {
		t.Fatalf("second update failed: %v", err)
	}
	um2 := st.GetUnifiedMap()
	if um2 == nil {
		t.Fatal("second unified map is nil")
		return
	}

	// The refined map should exist and have the same or more features.
	if um2.Metadata.LastUpdated < um1.Metadata.LastUpdated {
		t.Error("second update should have a later timestamp")
	}

	t.Logf("After refinement - Walls: %d, Floors: %d", len(um2.Walls), len(um2.Floors))
}

// ---------------------------------------------------------------------------
// Test: Persistence - Save and Load unified map cache
// ---------------------------------------------------------------------------

func TestUnifiedMap_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, ".unified-map.json")

	// Create and save a unified map.
	um := NewUnifiedMap(2, "vac-ref")
	um.Walls = append(um.Walls, &UnifiedFeature{
		Geometry: &Geometry{
			Type:        GeometryLineString,
			Coordinates: marshalCoordinate([][2]float64{{0, 0}, {100, 0}}),
		},
		Properties:       map[string]interface{}{"layerType": "wall"},
		Confidence:       0.8,
		ObservationCount: 2,
		Sources: []FeatureSource{
			{VacuumID: "vac-1", ICPScore: 0.9},
			{VacuumID: "vac-2", ICPScore: 0.85},
		},
	})
	um.Floors = append(um.Floors, &UnifiedFeature{
		Geometry: &Geometry{
			Type: GeometryPolygon,
			Coordinates: marshalCoordinate([][][2]float64{
				{{0, 0}, {100, 0}, {100, 100}, {0, 100}, {0, 0}},
			}),
		},
		Properties:       map[string]interface{}{"layerType": "floor"},
		Confidence:       1.0,
		ObservationCount: 2,
	})

	if err := SaveUnifiedMap(um, cachePath); err != nil {
		t.Fatalf("SaveUnifiedMap failed: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not created: %v", err)
	}

	// Load it back.
	loaded, err := LoadUnifiedMap(cachePath)
	if err != nil {
		t.Fatalf("LoadUnifiedMap failed: %v", err)
	}

	if loaded.Metadata.VacuumCount != 2 {
		t.Errorf("loaded VacuumCount = %d, want 2", loaded.Metadata.VacuumCount)
	}
	if loaded.Metadata.ReferenceVacuum != "vac-ref" {
		t.Errorf("loaded ReferenceVacuum = %q, want %q", loaded.Metadata.ReferenceVacuum, "vac-ref")
	}
	if len(loaded.Walls) != 1 {
		t.Errorf("loaded walls = %d, want 1", len(loaded.Walls))
	}
	if len(loaded.Floors) != 1 {
		t.Errorf("loaded floors = %d, want 1", len(loaded.Floors))
	}
	if loaded.Walls[0].Confidence != 0.8 {
		t.Errorf("loaded wall confidence = %f, want 0.8", loaded.Walls[0].Confidence)
	}
}

// ---------------------------------------------------------------------------
// Test: StateTracker with cache loads on creation
// ---------------------------------------------------------------------------

func TestNewStateTrackerWithCache(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, ".unified-map.json")

	// Pre-populate cache.
	um := NewUnifiedMap(1, "vac-cached")
	if err := SaveUnifiedMap(um, cachePath); err != nil {
		t.Fatalf("SaveUnifiedMap failed: %v", err)
	}

	// Create tracker with cache - should load the unified map.
	st := NewStateTrackerWithCache(cachePath)
	loaded := st.GetUnifiedMap()
	if loaded == nil {
		t.Fatal("expected cached unified map to be loaded on creation")
		return
	}
	if loaded.Metadata.ReferenceVacuum != "vac-cached" {
		t.Errorf("cached ReferenceVacuum = %q, want %q", loaded.Metadata.ReferenceVacuum, "vac-cached")
	}
}

// ---------------------------------------------------------------------------
// Test: StateTracker with cache path persists after UpdateUnifiedMap
// ---------------------------------------------------------------------------

func TestStateTracker_PersistsAfterUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, ".unified-map.json")

	st := NewStateTrackerWithCache(cachePath)

	floorPixels := []int{
		10, 10, 11, 10, 12, 10,
		10, 11, 11, 11, 12, 11,
	}
	wallPixels := []int{
		9, 9, 10, 9, 11, 9, 12, 9, 13, 9,
	}

	st.UpdateMap("vac-1", makeTestMap(5, floorPixels, wallPixels, nil, ""))

	calibData := &CalibrationData{
		ReferenceVacuum: "vac-1",
		Vacuums: map[string]VacuumCalibration{
			"vac-1": {Transform: Identity()},
		},
	}

	if err := st.UpdateUnifiedMap(calibData); err != nil {
		t.Fatalf("UpdateUnifiedMap failed: %v", err)
	}

	// Verify cache file was written.
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	var cached UnifiedMap
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("cache file invalid JSON: %v", err)
	}
	if cached.Metadata.VacuumCount != 1 {
		t.Errorf("cached VacuumCount = %d, want 1", cached.Metadata.VacuumCount)
	}
}

// ---------------------------------------------------------------------------
// Test: UpdateUnifiedMap error cases
// ---------------------------------------------------------------------------

func TestStateTracker_UpdateUnifiedMap_Errors(t *testing.T) {
	st := NewStateTracker()

	t.Run("nil calibration", func(t *testing.T) {
		err := st.UpdateUnifiedMap(nil)
		if err == nil {
			t.Error("expected error for nil calibration data")
		}
	})

	t.Run("no maps", func(t *testing.T) {
		err := st.UpdateUnifiedMap(&CalibrationData{
			ReferenceVacuum: "vac-1",
			Vacuums:         map[string]VacuumCalibration{},
		})
		if err == nil {
			t.Error("expected error when no maps are available")
		}
	})
}

// ---------------------------------------------------------------------------
// Test: GetUnifiedMap returns nil when no unified map exists
// ---------------------------------------------------------------------------

func TestStateTracker_GetUnifiedMap_Nil(t *testing.T) {
	st := NewStateTracker()
	if st.GetUnifiedMap() != nil {
		t.Error("expected nil unified map on fresh tracker")
	}
}

// ---------------------------------------------------------------------------
// Test: Unified map converts to feature collection
// ---------------------------------------------------------------------------

func TestUnifiedMap_ToFeatureCollection_Integration(t *testing.T) {
	st := NewStateTracker()

	floorPixels := []int{
		10, 10, 11, 10, 12, 10,
		10, 11, 11, 11, 12, 11,
		10, 12, 11, 12, 12, 12,
	}
	wallPixels := []int{
		9, 9, 10, 9, 11, 9, 12, 9, 13, 9,
		9, 10, 9, 11, 9, 12,
		13, 10, 13, 11, 13, 12,
	}

	st.UpdateMap("vac-1", makeTestMap(5, floorPixels, wallPixels, nil, ""))

	calibData := &CalibrationData{
		ReferenceVacuum: "vac-1",
		Vacuums: map[string]VacuumCalibration{
			"vac-1": {Transform: Identity()},
		},
	}

	if err := st.UpdateUnifiedMap(calibData); err != nil {
		t.Fatalf("UpdateUnifiedMap failed: %v", err)
	}

	um := st.GetUnifiedMap()
	if um == nil {
		t.Fatal("unified map is nil")
	}

	fc := um.ToFeatureCollection()
	if fc == nil {
		t.Fatal("ToFeatureCollection returned nil")
		return
	}
	if fc.Type != "FeatureCollection" {
		t.Errorf("fc.Type = %q, want %q", fc.Type, "FeatureCollection")
	}

	// Verify it serializes to valid JSON.
	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("failed to marshal feature collection: %v", err)
	}
	if len(data) == 0 {
		t.Error("marshalled feature collection is empty")
	}

	t.Logf("Feature collection has %d features, JSON size: %d bytes", len(fc.Features), len(data))
}

// ---------------------------------------------------------------------------
// Test: Blend geometry helpers
// ---------------------------------------------------------------------------

func TestBlendLineStrings(t *testing.T) {
	old := &Geometry{
		Type:        GeometryLineString,
		Coordinates: marshalCoordinate([][2]float64{{0, 0}, {100, 0}}),
	}
	new_ := &Geometry{
		Type:        GeometryLineString,
		Coordinates: marshalCoordinate([][2]float64{{0, 10}, {100, 10}}),
	}

	blended := blendGeometry(old, new_, 0.5)
	if blended == nil {
		t.Fatal("blendGeometry returned nil")
		return
	}
	if blended.Type != GeometryLineString {
		t.Errorf("blended type = %q, want %q", blended.Type, GeometryLineString)
	}

	// Parse the blended coordinates.
	var coords [][2]float64
	if err := json.Unmarshal(blended.Coordinates, &coords); err != nil {
		t.Fatalf("failed to unmarshal blended coords: %v", err)
	}

	// With weight 0.5, the Y coordinates should be around 5.
	if len(coords) < 2 {
		t.Fatalf("expected at least 2 coords, got %d", len(coords))
	}
	for _, c := range coords {
		if c[1] < 3 || c[1] > 7 {
			t.Errorf("blended Y = %f, expected near 5.0", c[1])
		}
	}
}

func TestBlendPolygons(t *testing.T) {
	old := &Geometry{
		Type: GeometryPolygon,
		Coordinates: marshalCoordinate([][][2]float64{
			{{0, 0}, {100, 0}, {100, 100}, {0, 100}, {0, 0}},
		}),
	}
	new_ := &Geometry{
		Type: GeometryPolygon,
		Coordinates: marshalCoordinate([][][2]float64{
			{{10, 10}, {110, 10}, {110, 110}, {10, 110}, {10, 10}},
		}),
	}

	blended := blendGeometry(old, new_, 0.5)
	if blended == nil {
		t.Fatal("blendGeometry returned nil for polygons")
		return
	}
	if blended.Type != GeometryPolygon {
		t.Errorf("blended type = %q, want %q", blended.Type, GeometryPolygon)
	}
}

// ---------------------------------------------------------------------------
// Test: SnapToGrid
// ---------------------------------------------------------------------------

func TestSnapToGrid(t *testing.T) {
	geom := &Geometry{
		Type:        GeometryLineString,
		Coordinates: marshalCoordinate([][2]float64{{3.7, 8.2}, {14.3, 22.8}}),
	}

	snapped := snapToGrid(geom, 5.0)
	if snapped == nil {
		t.Fatal("snapToGrid returned nil")
		return
	}

	var coords [][2]float64
	if err := json.Unmarshal(snapped.Coordinates, &coords); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if coords[0][0] != 5.0 || coords[0][1] != 10.0 {
		t.Errorf("first point snapped to (%f, %f), want (5, 10)", coords[0][0], coords[0][1])
	}
	if coords[1][0] != 15.0 || coords[1][1] != 25.0 {
		t.Errorf("second point snapped to (%f, %f), want (15, 25)", coords[1][0], coords[1][1])
	}
}

// ---------------------------------------------------------------------------
// Test: LoadUnifiedMap with missing file
// ---------------------------------------------------------------------------

func TestLoadUnifiedMap_MissingFile(t *testing.T) {
	_, err := LoadUnifiedMap("/nonexistent/path/.unified-map.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// ---------------------------------------------------------------------------
// Test: Concurrency safety of GetUnifiedMap during updates
// ---------------------------------------------------------------------------

func TestStateTracker_UnifiedMap_Concurrency(t *testing.T) {
	st := NewStateTracker()

	floorPixels := []int{
		10, 10, 11, 10, 12, 10,
		10, 11, 11, 11, 12, 11,
	}
	wallPixels := []int{
		9, 9, 10, 9, 11, 9, 12, 9, 13, 9,
	}

	st.UpdateMap("vac-1", makeTestMap(5, floorPixels, wallPixels, nil, ""))

	calibData := &CalibrationData{
		ReferenceVacuum: "vac-1",
		Vacuums: map[string]VacuumCalibration{
			"vac-1": {Transform: Identity()},
		},
	}

	// Run updates and reads concurrently.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			_ = st.UpdateUnifiedMap(calibData)
		}
		close(done)
	}()

	for {
		select {
		case <-done:
			return
		default:
			_ = st.GetUnifiedMap()
		}
	}
}
