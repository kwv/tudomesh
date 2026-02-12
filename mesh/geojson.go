package mesh

import "encoding/json"

// GeometryType represents the GeoJSON geometry type
type GeometryType string

const (
	GeometryPoint           GeometryType = "Point"
	GeometryLineString      GeometryType = "LineString"
	GeometryPolygon         GeometryType = "Polygon"
	GeometryMultiPoint      GeometryType = "MultiPoint"
	GeometryMultiLineString GeometryType = "MultiLineString"
	GeometryMultiPolygon    GeometryType = "MultiPolygon"
)

// Geometry represents a GeoJSON geometry object
type Geometry struct {
	Type        GeometryType    `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// Feature represents a GeoJSON feature with geometry and properties
type Feature struct {
	Type       string                 `json:"type"`
	Geometry   *Geometry              `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
	ID         interface{}            `json:"id,omitempty"`
}

// FeatureCollection represents a GeoJSON FeatureCollection
type FeatureCollection struct {
	Type     string     `json:"type"`
	Features []*Feature `json:"features"`
}

// NewFeatureCollection creates a new empty FeatureCollection
func NewFeatureCollection() *FeatureCollection {
	return &FeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]*Feature, 0),
	}
}

// AddFeature appends a feature to the collection
func (fc *FeatureCollection) AddFeature(f *Feature) {
	fc.Features = append(fc.Features, f)
}

// NewFeature creates a Feature with the given geometry and properties
func NewFeature(geom *Geometry, props map[string]interface{}) *Feature {
	if props == nil {
		props = make(map[string]interface{})
	}
	return &Feature{
		Type:       "Feature",
		Geometry:   geom,
		Properties: props,
	}
}

// PathToLineString converts a Path to a GeoJSON LineString geometry
// Coordinates are in world/millimeter space (x, y)
func PathToLineString(path Path) *Geometry {
	coords := make([][2]float64, len(path))
	for i, p := range path {
		coords[i] = [2]float64{p.X, p.Y}
	}

	coordsJSON, _ := json.Marshal(coords)
	return &Geometry{
		Type:        GeometryLineString,
		Coordinates: coordsJSON,
	}
}

// PathsToMultiLineString converts multiple Paths to a GeoJSON MultiLineString geometry
// Coordinates are in world/millimeter space (x, y)
func PathsToMultiLineString(paths []Path) *Geometry {
	lines := make([][][2]float64, len(paths))
	for i, path := range paths {
		coords := make([][2]float64, len(path))
		for j, p := range path {
			coords[j] = [2]float64{p.X, p.Y}
		}
		lines[i] = coords
	}

	coordsJSON, _ := json.Marshal(lines)
	return &Geometry{
		Type:        GeometryMultiLineString,
		Coordinates: coordsJSON,
	}
}

// PathToPolygon converts a closed Path to a GeoJSON Polygon geometry
// The path is assumed to be a closed outer ring (counter-clockwise)
// Coordinates are in world/millimeter space (x, y)
func PathToPolygon(path Path) *Geometry {
	// Ensure the polygon is closed (first point == last point)
	coords := make([][2]float64, len(path))
	for i, p := range path {
		coords[i] = [2]float64{p.X, p.Y}
	}

	// Close the polygon if not already closed
	if len(coords) > 0 {
		first := coords[0]
		last := coords[len(coords)-1]
		if first[0] != last[0] || first[1] != last[1] {
			coords = append(coords, first)
		}
	}

	// GeoJSON Polygon coordinates are an array of linear rings
	// First ring is outer boundary, subsequent rings are holes
	rings := [][][2]float64{coords}

	coordsJSON, _ := json.Marshal(rings)
	return &Geometry{
		Type:        GeometryPolygon,
		Coordinates: coordsJSON,
	}
}

// PathsToPolygon converts multiple Paths to a GeoJSON Polygon geometry with holes
// The first path is the outer ring (counter-clockwise), subsequent paths are holes (clockwise)
// Coordinates are in world/millimeter space (x, y)
func PathsToPolygon(paths []Path) *Geometry {
	if len(paths) == 0 {
		return nil
	}

	rings := make([][][2]float64, len(paths))
	for i, path := range paths {
		coords := make([][2]float64, len(path))
		for j, p := range path {
			coords[j] = [2]float64{p.X, p.Y}
		}

		// Ensure ring is closed
		if len(coords) > 0 {
			first := coords[0]
			last := coords[len(coords)-1]
			if first[0] != last[0] || first[1] != last[1] {
				coords = append(coords, first)
			}
		}

		rings[i] = coords
	}

	coordsJSON, _ := json.Marshal(rings)
	return &Geometry{
		Type:        GeometryPolygon,
		Coordinates: coordsJSON,
	}
}

// TransformPath applies an affine transformation to all points in a path
// Returns a new transformed path (coordinates in world/millimeter space)
func TransformPath(path Path, matrix AffineMatrix) Path {
	return TransformPoints(path, matrix)
}

// TransformPaths applies an affine transformation to all paths
// Returns new transformed paths (coordinates in world/millimeter space)
func TransformPaths(paths []Path, matrix AffineMatrix) []Path {
	result := make([]Path, len(paths))
	for i, path := range paths {
		result[i] = TransformPath(path, matrix)
	}
	return result
}

// LayerToFeature converts a map layer with its vectorized paths to a GeoJSON Feature
// The geometry type is chosen based on the layer type:
// - "floor" or "segment" -> Polygon (for closed boundaries)
// - "wall" -> MultiLineString (for wall segments)
// Properties include layer metadata (segment name, vacuum ID, area, etc.)
//
// Paths are in local-mm (after NormalizeToMM). The transform maps local-mm
// to world-mm directly (ICP now operates in mm). The pixelSize parameter is
// retained for API compatibility but is no longer used for coordinate scaling.
func LayerToFeature(layer *MapLayer, paths []Path, vacuumID string, transform AffineMatrix, pixelSize int) *Feature {
	if layer == nil || len(paths) == 0 {
		return nil
	}

	// Transform paths from local-mm to world-mm.
	// After NormalizeToMM, paths are already in mm; the ICP transform
	// produces mm-to-mm mappings, so no additional pixelSize scaling is needed.
	worldPaths := TransformPaths(paths, transform)

	// Create geometry based on layer type
	var geom *Geometry
	switch layer.Type {
	case "floor", "segment":
		// Floor and segments are polygons
		if len(worldPaths) == 1 {
			geom = PathToPolygon(worldPaths[0])
		} else {
			geom = PathsToPolygon(worldPaths)
		}
	case "wall":
		// Walls are line strings
		if len(worldPaths) == 1 {
			geom = PathToLineString(worldPaths[0])
		} else {
			geom = PathsToMultiLineString(worldPaths)
		}
	default:
		// Default to MultiLineString for unknown types
		if len(worldPaths) == 1 {
			geom = PathToLineString(worldPaths[0])
		} else {
			geom = PathsToMultiLineString(worldPaths)
		}
	}

	// Build properties from layer metadata
	props := map[string]interface{}{
		"layerType": layer.Type,
		"vacuumId":  vacuumID,
	}

	if layer.MetaData.SegmentID != "" {
		props["segmentId"] = layer.MetaData.SegmentID
	}
	if layer.MetaData.Name != "" {
		props["segmentName"] = layer.MetaData.Name
	}
	if layer.MetaData.Area > 0 {
		props["area"] = layer.MetaData.Area
	}
	if layer.MetaData.Active {
		props["active"] = layer.MetaData.Active
	}

	return NewFeature(geom, props)
}

// MapToFeatureCollection converts a complete ValetudoMap to a GeoJSON FeatureCollection
// Each layer is vectorized and transformed to world coordinates, then added as a feature
func MapToFeatureCollection(valetudoMap *ValetudoMap, vacuumID string, transform AffineMatrix, tolerance float64) *FeatureCollection {
	fc := NewFeatureCollection()

	if valetudoMap == nil {
		return fc
	}

	// Process each layer
	for i := range valetudoMap.Layers {
		layer := &valetudoMap.Layers[i]

		// Vectorize the layer
		paths := VectorizeLayer(layer, valetudoMap.PixelSize, tolerance)
		if len(paths) == 0 {
			continue
		}

		// Convert to feature
		feature := LayerToFeature(layer, paths, vacuumID, transform, valetudoMap.PixelSize)
		if feature != nil {
			fc.AddFeature(feature)
		}
	}

	return fc
}
