package mesh

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ParseMapFile reads and parses a Valetudo map JSON file, then normalizes
// all layer pixel coordinates from grid indices to millimeters.
func ParseMapFile(path string) (*ValetudoMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	m, err := ParseMapJSON(data)
	if err != nil {
		return nil, err
	}
	NormalizeToMM(m)
	return m, nil
}

// ParseMapJSON parses Valetudo map JSON data
func ParseMapJSON(data []byte) (*ValetudoMap, error) {
	var m ValetudoMap
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return &m, nil
}

// ExtractRobotPosition finds the robot_position entity and returns its coordinates
func ExtractRobotPosition(m *ValetudoMap) (Point, float64, bool) {
	for _, entity := range m.Entities {
		if entity.Type == "robot_position" && len(entity.Points) >= 2 {
			angle := 0.0
			if a, ok := entity.MetaData["angle"].(float64); ok {
				angle = a
			}
			return Point{
				X: float64(entity.Points[0]),
				Y: float64(entity.Points[1]),
			}, angle, true
		}
	}
	return Point{}, 0, false
}

// ExtractChargerPosition finds the charger_location entity and returns its coordinates
func ExtractChargerPosition(m *ValetudoMap) (Point, bool) {
	for _, entity := range m.Entities {
		if entity.Type == "charger_location" && len(entity.Points) >= 2 {
			return Point{
				X: float64(entity.Points[0]),
				Y: float64(entity.Points[1]),
			}, true
		}
	}
	return Point{}, false
}

// ExtractSegments returns all segment layers with their names
func ExtractSegments(m *ValetudoMap) []MapLayer {
	var segments []MapLayer
	for _, layer := range m.Layers {
		if layer.Type == "segment" {
			segments = append(segments, layer)
		}
	}
	return segments
}

// ExtractFloorLayer returns the floor layer if present
func ExtractFloorLayer(m *ValetudoMap) (*MapLayer, bool) {
	for _, layer := range m.Layers {
		if layer.Type == "floor" {
			return &layer, true
		}
	}
	return nil, false
}

// ExtractWallLayer returns the wall layer if present
func ExtractWallLayer(m *ValetudoMap) (*MapLayer, bool) {
	for _, layer := range m.Layers {
		if layer.Type == "wall" {
			return &layer, true
		}
	}
	return nil, false
}

// PixelsToPoints converts a flat pixel array [x1,y1,x2,y2,...] to Point slice
func PixelsToPoints(pixels []int) []Point {
	points := make([]Point, 0, len(pixels)/2)
	for i := 0; i+1 < len(pixels); i += 2 {
		points = append(points, Point{
			X: float64(pixels[i]),
			Y: float64(pixels[i+1]),
		})
	}
	return points
}

// MapSummary provides a summary of map contents
type MapSummary struct {
	Version         int
	TotalLayerArea  int
	Size            Size
	PixelSize       int
	RobotPosition   Point
	RobotAngle      float64
	ChargerPosition Point
	SegmentCount    int
	SegmentNames    []string
	HasFloor        bool
	HasWall         bool
}

// Summarize extracts key information from a map
func Summarize(m *ValetudoMap) MapSummary {
	summary := MapSummary{
		Version:        m.MetaData.Version,
		TotalLayerArea: m.MetaData.TotalLayerArea,
		Size:           m.Size,
		PixelSize:      m.PixelSize,
	}

	if pos, angle, ok := ExtractRobotPosition(m); ok {
		summary.RobotPosition = pos
		summary.RobotAngle = angle
	}

	if pos, ok := ExtractChargerPosition(m); ok {
		summary.ChargerPosition = pos
	}

	segments := ExtractSegments(m)
	summary.SegmentCount = len(segments)
	for _, seg := range segments {
		if seg.MetaData.Name != "" {
			summary.SegmentNames = append(summary.SegmentNames, seg.MetaData.Name)
		}
	}

	_, summary.HasFloor = ExtractFloorLayer(m)
	_, summary.HasWall = ExtractWallLayer(m)

	return summary
}

// MinAreaRatio is the minimum ratio of new map area to last known good map area
// required for the new map to be considered complete.
const MinAreaRatio = 0.8

// Sentinel errors for map validation failures.
var (
	ErrNilMap            = errors.New("map is nil")
	ErrNoDrawablePixels  = errors.New("map has no drawable content (no pixels, layer area, or sufficient path entities)")
	ErrNoRobotPosition   = errors.New("map is missing robot_position entity")
	ErrNoChargerLocation = errors.New("map is missing charger_location entity")
	ErrAreaTooSmall      = errors.New("map area is too small compared to last known good map")
)

// ValidateMapForCalibration checks that a map has all required data for calibration.
// It returns a descriptive error for the first validation failure found, or nil if valid.
func ValidateMapForCalibration(m *ValetudoMap) error {
	if m == nil {
		return ErrNilMap
	}
	if !HasDrawablePixels(m) {
		return ErrNoDrawablePixels
	}
	if _, _, ok := ExtractRobotPosition(m); !ok {
		return ErrNoRobotPosition
	}
	if _, ok := ExtractChargerPosition(m); !ok {
		return ErrNoChargerLocation
	}
	return nil
}

// IsMapComplete validates that newMap is a complete, usable map that should replace
// the lastKnownGood map. It checks structural completeness via ValidateMapForCalibration,
// then optionally verifies that the new map's total layer area has not shrunk
// below MinAreaRatio of the last known good map (to guard against partial updates).
// If lastKnownGood is nil, only structural checks are performed.
func IsMapComplete(newMap, lastKnownGood *ValetudoMap) bool {
	if err := ValidateMapForCalibration(newMap); err != nil {
		return false
	}
	if lastKnownGood != nil && lastKnownGood.MetaData.TotalLayerArea > 0 {
		ratio := float64(newMap.MetaData.TotalLayerArea) / float64(lastKnownGood.MetaData.TotalLayerArea)
		if ratio < MinAreaRatio {
			return false
		}
	}
	return true
}

// MinEntityPoints is the minimum total number of entity path points required
// to consider a map drawable when layer pixels are empty.
const MinEntityPoints = 100

// HasDrawablePixels returns true if the map contains drawable spatial data.
// A map is drawable if any of the following hold:
//   - Any floor/segment/wall layer has pixel data (len(Pixels) > 0).
//   - Any floor/segment/wall layer has metaData.area > 0 (API reports area
//     even when pixels are empty).
//   - Path entities contain at least MinEntityPoints total coordinate values,
//     indicating the vacuum has traversed enough of the space to define it.
func HasDrawablePixels(m *ValetudoMap) bool {
	if m == nil {
		return false
	}

	// Check layer pixels.
	for _, layer := range m.Layers {
		if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
			if len(layer.Pixels) > 0 {
				return true
			}
		}
	}

	// Check layer metaData.area — the API may report area without pixels.
	for _, layer := range m.Layers {
		if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
			if layer.MetaData.Area > 0 {
				return true
			}
		}
	}

	// Check entity path point coverage.
	totalPoints := 0
	for _, entity := range m.Entities {
		if entity.Type == "path" {
			totalPoints += len(entity.Points)
		}
	}
	return totalPoints >= MinEntityPoints
}

// NormalizeToMM converts all layer pixel coordinates from grid indices to
// millimeters using the map's PixelSize. Entity points (robot_position,
// charger_location, path) are already in mm and pass through unchanged.
// Layer metaData.area is already in mm-squared and is not modified.
//
// After normalization, layer.Pixels contains coordinate pairs in mm:
//
//	mm = gridIndex * pixelSize
//
// If CompressedPixels is present and Pixels is empty, CompressedPixels is
// decoded into Pixels first (future-proofing for Valetudo API changes).
//
// This function is idempotent: it sets m.Normalized and skips subsequent calls.
// It is safe to call on a nil map or a map with pixelSize 0 (no-op).
func NormalizeToMM(m *ValetudoMap) {
	if m == nil || m.Normalized {
		return
	}
	m.Normalized = true

	ps := m.PixelSize
	if ps <= 0 {
		return
	}

	for i := range m.Layers {
		layer := &m.Layers[i]

		// Future-proofing: if Pixels is empty but CompressedPixels is
		// present, copy CompressedPixels into Pixels so downstream code
		// has a single field to consume.
		if len(layer.Pixels) == 0 && len(layer.CompressedPixels) > 0 {
			layer.Pixels = make([]int, len(layer.CompressedPixels))
			copy(layer.Pixels, layer.CompressedPixels)
		}

		// Convert grid indices to mm.
		for j := range layer.Pixels {
			layer.Pixels[j] *= ps
		}
	}

	// Entity points are already in mm — no conversion needed.
}
