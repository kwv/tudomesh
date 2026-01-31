package mesh

import (
	"encoding/json"
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
