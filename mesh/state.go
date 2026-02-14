package mesh

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LivePosition represents a vacuum's current position in world coordinates
type LivePosition struct {
	VacuumID  string    `json:"vacuumId"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	Angle     float64   `json:"angle"` // degrees, 0 = East, CCW
	Timestamp time.Time `json:"timestamp"`
	Color     string    `json:"color"` // hex color for this vacuum
}

// StateTracker tracks live vacuum positions for HTTP endpoints
type StateTracker struct {
	mu         sync.RWMutex
	positions  map[string]*LivePosition
	maps       map[string]*ValetudoMap
	colors     map[string]string // vacuum ID -> hex color
	unifiedMap *UnifiedMap
	cachePath  string // path to .unified-map.json cache file; empty disables persistence
}

// NewStateTracker creates a new state tracker
func NewStateTracker() *StateTracker {
	return &StateTracker{
		positions: make(map[string]*LivePosition),
		maps:      make(map[string]*ValetudoMap),
		colors:    make(map[string]string),
	}
}

// NewStateTrackerWithCache creates a state tracker that persists the unified map
// to the given cache file path. If the file exists, the cached unified map is
// loaded on creation.
func NewStateTrackerWithCache(cachePath string) *StateTracker {
	st := &StateTracker{
		positions: make(map[string]*LivePosition),
		maps:      make(map[string]*ValetudoMap),
		colors:    make(map[string]string),
		cachePath: cachePath,
	}
	if cachePath != "" {
		if um, err := LoadUnifiedMap(cachePath); err == nil {
			st.unifiedMap = um
		}
	}
	return st
}

// SetColor sets the color for a vacuum
func (st *StateTracker) SetColor(vacuumID, hexColor string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.colors[vacuumID] = hexColor
}

// UpdatePosition updates a vacuum's position
func (st *StateTracker) UpdatePosition(vacuumID string, x, y, angle float64) {
	st.mu.Lock()
	defer st.mu.Unlock()

	color := st.colors[vacuumID]
	if color == "" {
		color = "#FF0000" // default red
	}

	st.positions[vacuumID] = &LivePosition{
		VacuumID:  vacuumID,
		X:         x,
		Y:         y,
		Angle:     angle,
		Timestamp: time.Now(),
		Color:     color,
	}
}

// UpdateMap stores the latest map data for a vacuum
func (st *StateTracker) UpdateMap(vacuumID string, m *ValetudoMap) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.maps[vacuumID] = m
}

// GetPositions returns all current positions
func (st *StateTracker) GetPositions() map[string]*LivePosition {
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make(map[string]*LivePosition)
	for k, v := range st.positions {
		copy := *v
		result[k] = &copy
	}
	return result
}

// GetMaps returns all current maps
func (st *StateTracker) GetMaps() map[string]*ValetudoMap {
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make(map[string]*ValetudoMap)
	for k, v := range st.maps {
		result[k] = v
	}
	return result
}

// HasMaps returns true if we have at least one map
func (st *StateTracker) HasMaps() bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.maps) > 0
}

// GetUnifiedMap returns the current unified map, or nil if none exists.
func (st *StateTracker) GetUnifiedMap() *UnifiedMap {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.unifiedMap
}

// UpdateUnifiedMap rebuilds the unified map from all stored vacuum maps using
// the provided calibration data. Each vacuum's map is vectorized and
// transformed to world coordinates, then walls, floors, and segments are
// unified via clustering and outlier filtering.
//
// If a previous unified map exists, incremental refinement is applied via
// weighted averaging of geometry coordinates.
//
// The resulting unified map is persisted to the cache file when a cache path
// is configured.
func (st *StateTracker) UpdateUnifiedMap(calibData *CalibrationData) error {
	if calibData == nil {
		return fmt.Errorf("calibration data is nil")
	}

	st.mu.RLock()
	maps := make(map[string]*ValetudoMap, len(st.maps))
	for k, v := range st.maps {
		maps[k] = v
	}
	previousMap := st.unifiedMap
	cachePath := st.cachePath
	st.mu.RUnlock()

	if len(maps) == 0 {
		return fmt.Errorf("no vacuum maps available")
	}

	totalVacuums := len(maps)

	// Extract and transform features from each vacuum map into world coordinates.
	var allWallFeatures []*Feature
	var allWallSources []FeatureSource
	var allFloorFeatures []*Feature
	var allFloorSources []FeatureSource

	for vacuumID, vMap := range maps {
		vc, ok := calibData.Vacuums[vacuumID]
		if !ok {
			// No calibration for this vacuum; use identity transform.
			vc = VacuumCalibration{Transform: Identity()}
		}

		// Convert the vacuum map to a GeoJSON feature collection in world coordinates.
		// Use native pixelSize as the simplification tolerance.
		fc := MapToFeatureCollection(vMap, vacuumID, vc.Transform, float64(vMap.PixelSize))

		src := FeatureSource{
			VacuumID:  vacuumID,
			Timestamp: time.Now().Unix(),
			ICPScore:  1.0, // default; real ICP score could be stored in calibration
		}

		for _, f := range fc.Features {
			lt, _ := f.Properties["layerType"].(string)
			featureSrc := src
			featureSrc.OriginalGeom = f.Geometry

			switch lt {
			case "wall":
				allWallFeatures = append(allWallFeatures, f)
				allWallSources = append(allWallSources, featureSrc)
			case "floor", "segment":
				allFloorFeatures = append(allFloorFeatures, f)
				allFloorSources = append(allFloorSources, featureSrc)
			}
		}
	}

	// Determine resolution-aware clustering distances.
	// We want the clustering to be at least a few pixels wide even on low-res maps.
	maxPixelSize := 5
	for _, vMap := range maps {
		if vMap.PixelSize > maxPixelSize {
			maxPixelSize = vMap.PixelSize
		}
	}
	// Defaults are 50mm and 100mm. If pixelSize > 5, we scale them up.
	// 10 pixels for walls, 20 pixels for floors.
	wallClusterDist := math.Max(DefaultWallClusterDistance, 10.0*float64(maxPixelSize))
	floorClusterDist := math.Max(DefaultFloorClusterDistance, 20.0*float64(maxPixelSize))

	// Unify walls.
	unifiedWalls := UnifyWallsWithOptions(
		extractWallFeatures(allWallFeatures),
		allWallSources,
		totalVacuums,
		wallClusterDist,
		DefaultConfidenceThreshold,
	)

	// Unify floors/segments.
	unifiedFloors := UnifyFloorsWithOptions(
		extractFloorFeatures(allFloorFeatures),
		allFloorSources,
		totalVacuums,
		floorClusterDist,
	)

	// Apply outlier detection.
	outlierCfg := DefaultOutlierConfig(totalVacuums)

	retainedWalls, _ := DetectOutliers(unifiedWalls, outlierCfg)
	retainedFloors, _ := DetectOutliers(unifiedFloors, outlierCfg)

	// Separate floors from segments by checking properties.
	var floors, segments []*UnifiedFeature
	for _, f := range retainedFloors {
		lt, _ := f.Properties["layerType"].(string)
		if lt == "segment" {
			segments = append(segments, f)
		} else {
			floors = append(floors, f)
		}
	}

	newMap := &UnifiedMap{
		Walls:    retainedWalls,
		Floors:   floors,
		Segments: segments,
		Metadata: UnifiedMetadata{
			VacuumCount:     totalVacuums,
			ReferenceVacuum: calibData.ReferenceVacuum,
			LastUpdated:     time.Now().Unix(),
		},
	}

	// Ensure nil slices become empty slices for consistent JSON output.
	if newMap.Walls == nil {
		newMap.Walls = make([]*UnifiedFeature, 0)
	}
	if newMap.Floors == nil {
		newMap.Floors = make([]*UnifiedFeature, 0)
	}
	if newMap.Segments == nil {
		newMap.Segments = make([]*UnifiedFeature, 0)
	}

	// Incremental refinement: blend with previous map if available.
	if previousMap != nil {
		newMap.Walls = refineFeatures(previousMap.Walls, newMap.Walls)
		newMap.Floors = refineFeatures(previousMap.Floors, newMap.Floors)
		newMap.Segments = refineFeatures(previousMap.Segments, newMap.Segments)
	}

	// Apply geometry simplification.
	// Use scale-aware tolerances: at least 2 pixels or the default mm tolerance.
	wallSimplify := math.Max(DefaultWallSimplifyTolerance, 2.0*float64(maxPixelSize))
	floorSimplify := math.Max(DefaultFloorSimplifyTolerance, 4.0*float64(maxPixelSize))

	simplifyUnifiedFeatures(newMap.Walls, wallSimplify)
	simplifyUnifiedFeatures(newMap.Floors, floorSimplify)
	simplifyUnifiedFeatures(newMap.Segments, floorSimplify)

	// Store the unified map.
	st.mu.Lock()
	st.unifiedMap = newMap
	st.mu.Unlock()

	// Persist to cache.
	if cachePath != "" {
		if err := SaveUnifiedMap(newMap, cachePath); err != nil {
			log.Printf("warning: failed to save unified map cache: %v", err)
		}
	}

	return nil
}

// SaveUnifiedMap writes a UnifiedMap to disk as JSON.
func SaveUnifiedMap(um *UnifiedMap, path string) error {
	data, err := json.MarshalIndent(um, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal unified map: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write unified map cache: %w", err)
	}
	return nil
}

// LoadUnifiedMap reads a UnifiedMap from a JSON file on disk.
func LoadUnifiedMap(path string) (*UnifiedMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read unified map cache: %w", err)
	}
	var um UnifiedMap
	if err := json.Unmarshal(data, &um); err != nil {
		return nil, fmt.Errorf("unmarshal unified map cache: %w", err)
	}
	return &um, nil
}
