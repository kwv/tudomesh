package mesh

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ParseMapFile reads and parses a Valetudo map JSON file
func ParseMapFile(path string) (*ValetudoMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	return ParseMapJSON(data)
}

// ParseMapJSON parses Valetudo map JSON data
func ParseMapJSON(data []byte) (*ValetudoMap, error) {
	var m ValetudoMap
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	expandCompressedPixels(&m)
	return &m, nil
}

// expandCompressedPixels rebuilds layer.Pixels as grid indices from
// layer.CompressedPixels when present. Valetudo's compressed form is
// authoritative; this also normalizes exports whose plain pixels were
// pre-scaled by pixelSize.
func expandCompressedPixels(m *ValetudoMap) {
	for i := range m.Layers {
		layer := &m.Layers[i]
		if len(layer.CompressedPixels) >= 3 {
			layer.Pixels = DecodeCompressedPixels(layer.CompressedPixels)
		}
	}
}

// DecodeCompressedPixels expands Valetudo run-length triplets
// [gridX, gridY, runLength, ...] into grid-index pairs [x1,y1,x2,y2,...].
// A trailing incomplete triplet is ignored.
func DecodeCompressedPixels(compressed []int) []int {
	var out []int
	for i := 0; i+2 < len(compressed); i += 3 {
		x, y, n := compressed[i], compressed[i+1], compressed[i+2]
		for j := 0; j < n; j++ {
			out = append(out, x+j, y)
		}
	}
	return out
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

// MinAreaRatio is the minimum ratio of new map area to last known good map area
// required for the new map to be considered complete.
const MinAreaRatio = 0.8

// Sentinel errors for map validation failures.
var (
	ErrNilMap            = errors.New("map is nil")
	ErrNoDrawablePixels  = errors.New("map has no drawable pixels")
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

// HasDrawablePixels returns true if the map contains any pixels in floor, wall or segment layers
func HasDrawablePixels(m *ValetudoMap) bool {
	if m == nil {
		return false
	}
	for _, layer := range m.Layers {
		if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
			if len(layer.Pixels) > 0 {
				return true
			}
		}
	}
	return false
}
