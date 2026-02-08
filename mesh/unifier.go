package mesh

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/simplify"
)

// UnifiedMap represents a composite map built from multiple vacuum observations.
// Each feature category (walls, floors, segments) contains consensus features
// derived by clustering and merging observations from individual vacuums.
type UnifiedMap struct {
	Walls    []*UnifiedFeature `json:"walls"`
	Floors   []*UnifiedFeature `json:"floors"`
	Segments []*UnifiedFeature `json:"segments"`
	Metadata UnifiedMetadata   `json:"metadata"`
}

// UnifiedFeature is a feature derived from multiple vacuum observations.
// It carries the merged geometry, a confidence score indicating how many
// vacuums observed it, and provenance information via Sources.
type UnifiedFeature struct {
	Geometry         *Geometry              `json:"geometry"`
	Properties       map[string]interface{} `json:"properties"`
	Sources          []FeatureSource        `json:"sources"`
	Confidence       float64                `json:"confidence"`
	ObservationCount int                    `json:"observationCount"`
}

// FeatureSource tracks which vacuum contributed to a unified feature.
type FeatureSource struct {
	VacuumID     string    `json:"vacuumId"`
	OriginalGeom *Geometry `json:"originalGeometry"`
	Timestamp    int64     `json:"timestamp"`
	ICPScore     float64   `json:"icpScore"`
}

// UnifiedMetadata provides provenance information for a UnifiedMap.
type UnifiedMetadata struct {
	VacuumCount     int     `json:"vacuumCount"`
	ReferenceVacuum string  `json:"referenceVacuum"`
	LastUpdated     int64   `json:"lastUpdated"`
	TotalArea       float64 `json:"totalArea"`
	CoverageOverlap float64 `json:"coverageOverlap"`
}

// DefaultWallClusterDistance is the maximum distance (in mm) between wall
// feature centroids for them to be grouped into the same cluster.
const DefaultWallClusterDistance = 50.0

// DefaultConfidenceThreshold is the minimum confidence score a unified
// feature must reach to be included in the final map.
const DefaultConfidenceThreshold = 0.5

// UnifyWalls clusters wall features by proximity, computes a median line for
// each cluster, and returns unified features that meet the confidence threshold.
//
// Parameters:
//   - features: wall features from all vacuums (LineString geometry expected)
//   - sources: one FeatureSource per feature, in the same order
//   - totalVacuums: total number of vacuums contributing to the map
//
// The function uses ClusterByProximity with DefaultWallClusterDistance,
// computes the median line for each cluster, and filters by
// DefaultConfidenceThreshold.
func UnifyWalls(features []*Feature, sources []FeatureSource, totalVacuums int) []*UnifiedFeature {
	return UnifyWallsWithOptions(features, sources, totalVacuums, DefaultWallClusterDistance, DefaultConfidenceThreshold)
}

// UnifyWallsWithOptions is like UnifyWalls but accepts custom clustering
// distance and confidence threshold parameters.
func UnifyWallsWithOptions(features []*Feature, sources []FeatureSource, totalVacuums int, clusterDist, confidenceThreshold float64) []*UnifiedFeature {
	if len(features) == 0 || totalVacuums <= 0 {
		return nil
	}

	// Build a lookup from feature pointer to its source.
	sourceMap := make(map[*Feature]FeatureSource, len(features))
	for i, f := range features {
		if i < len(sources) {
			sourceMap[f] = sources[i]
		}
	}

	// Cluster walls by proximity of their bounding-box centroids.
	clusters := ClusterByProximity(features, clusterDist)

	var result []*UnifiedFeature
	for _, cluster := range clusters {
		// Collect the orb.LineStrings and sources for this cluster.
		var lines []orb.LineString
		var clusterSources []FeatureSource
		for _, f := range cluster {
			ls := orbLineString(f.Geometry)
			if ls == nil {
				continue
			}
			lines = append(lines, ls)
			if s, ok := sourceMap[f]; ok {
				clusterSources = append(clusterSources, s)
			}
		}
		if len(lines) == 0 {
			continue
		}

		// Count distinct vacuums that observed this wall cluster.
		vacuumSet := make(map[string]struct{})
		for _, s := range clusterSources {
			vacuumSet[s.VacuumID] = struct{}{}
		}
		observationCount := len(vacuumSet)
		confidence := float64(observationCount) / float64(totalVacuums)

		if confidence < confidenceThreshold {
			continue
		}

		// Compute the median line from all lines in the cluster.
		medianGeom := medianLine(lines)

		// Merge properties: collect all unique property keys, preferring values
		// from the source with the highest ICP score.
		mergedProps := mergeProperties(cluster, clusterSources)
		mergedProps["observationCount"] = observationCount
		mergedProps["confidence"] = confidence

		uf := &UnifiedFeature{
			Geometry:         medianGeom,
			Properties:       mergedProps,
			Sources:          clusterSources,
			Confidence:       confidence,
			ObservationCount: observationCount,
		}
		result = append(result, uf)
	}

	// Sort by confidence descending for deterministic output.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Confidence > result[j].Confidence
	})

	return result
}

// medianLine computes a consensus line from multiple observed lines.
// It resamples each line to a common number of points, then computes the
// coordinate-wise median at each station along the line.
func medianLine(lines []orb.LineString) *Geometry {
	if len(lines) == 0 {
		return nil
	}
	if len(lines) == 1 {
		return lineStringToGeometry(lines[0])
	}

	// Determine the number of sample points. Use the maximum point count
	// among input lines, clamped to a reasonable range.
	maxPts := 0
	for _, ls := range lines {
		if len(ls) > maxPts {
			maxPts = len(ls)
		}
	}
	numSamples := maxPts
	if numSamples < 2 {
		numSamples = 2
	}
	if numSamples > 100 {
		numSamples = 100
	}

	// Resample each line to numSamples equidistant points.
	resampled := make([][]orb.Point, len(lines))
	for i, ls := range lines {
		resampled[i] = resampleLine(ls, numSamples)
	}

	// Compute the coordinate-wise median at each station.
	median := make(orb.LineString, numSamples)
	xs := make([]float64, len(lines))
	ys := make([]float64, len(lines))
	for s := 0; s < numSamples; s++ {
		for i := range lines {
			xs[i] = resampled[i][s][0]
			ys[i] = resampled[i][s][1]
		}
		sort.Float64s(xs)
		sort.Float64s(ys)
		median[s] = orb.Point{
			medianOfSorted(xs),
			medianOfSorted(ys),
		}
	}

	return lineStringToGeometry(median)
}

// resampleLine returns n equidistant points along the given line string.
// The first and last points of the result correspond to the first and last
// points of the input.
func resampleLine(ls orb.LineString, n int) []orb.Point {
	if len(ls) < 2 || n < 2 {
		pts := make([]orb.Point, n)
		if len(ls) > 0 {
			for i := range pts {
				pts[i] = ls[0]
			}
		}
		return pts
	}

	// Compute cumulative arc lengths along the line.
	cumLen := make([]float64, len(ls))
	for i := 1; i < len(ls); i++ {
		dx := ls[i][0] - ls[i-1][0]
		dy := ls[i][1] - ls[i-1][1]
		cumLen[i] = cumLen[i-1] + math.Hypot(dx, dy)
	}
	totalLen := cumLen[len(cumLen)-1]
	if totalLen == 0 {
		pts := make([]orb.Point, n)
		for i := range pts {
			pts[i] = ls[0]
		}
		return pts
	}

	result := make([]orb.Point, n)
	result[0] = ls[0]
	result[n-1] = ls[len(ls)-1]

	segIdx := 0
	for i := 1; i < n-1; i++ {
		targetLen := totalLen * float64(i) / float64(n-1)

		// Advance segIdx to the segment containing targetLen.
		for segIdx < len(cumLen)-2 && cumLen[segIdx+1] < targetLen {
			segIdx++
		}

		segLen := cumLen[segIdx+1] - cumLen[segIdx]
		if segLen == 0 {
			result[i] = ls[segIdx]
			continue
		}

		t := (targetLen - cumLen[segIdx]) / segLen
		result[i] = orb.Point{
			ls[segIdx][0] + t*(ls[segIdx+1][0]-ls[segIdx][0]),
			ls[segIdx][1] + t*(ls[segIdx+1][1]-ls[segIdx][1]),
		}
	}

	return result
}

// medianOfSorted returns the median of a pre-sorted slice of float64 values.
func medianOfSorted(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2.0
}

// mergeProperties combines properties from all features in a cluster.
// When multiple features define the same key, the value from the feature
// whose source has the highest ICP score wins.
func mergeProperties(cluster []*Feature, sources []FeatureSource) map[string]interface{} {
	merged := make(map[string]interface{})
	bestScore := make(map[string]float64)

	for i, f := range cluster {
		var score float64
		if i < len(sources) {
			score = sources[i].ICPScore
		}

		for k, v := range f.Properties {
			prev, exists := bestScore[k]
			if !exists || score > prev {
				merged[k] = v
				bestScore[k] = score
			}
		}
	}

	return merged
}

// NewUnifiedMap creates an empty UnifiedMap with initialized slices and metadata.
func NewUnifiedMap(vacuumCount int, referenceVacuum string) *UnifiedMap {
	return &UnifiedMap{
		Walls:    make([]*UnifiedFeature, 0),
		Floors:   make([]*UnifiedFeature, 0),
		Segments: make([]*UnifiedFeature, 0),
		Metadata: UnifiedMetadata{
			VacuumCount:     vacuumCount,
			ReferenceVacuum: referenceVacuum,
			LastUpdated:     time.Now().Unix(),
		},
	}
}

// ToFeatureCollection converts the UnifiedMap into a GeoJSON FeatureCollection.
// Each unified feature becomes a GeoJSON Feature with confidence and source
// count stored in properties.
func (um *UnifiedMap) ToFeatureCollection() *FeatureCollection {
	fc := NewFeatureCollection()

	addFeatures := func(features []*UnifiedFeature, layerType string) {
		for _, uf := range features {
			props := make(map[string]interface{})
			for k, v := range uf.Properties {
				props[k] = v
			}
			props["layerType"] = layerType
			props["confidence"] = uf.Confidence
			props["observationCount"] = uf.ObservationCount
			props["sourceVacuums"] = sourceVacuumIDs(uf.Sources)

			f := NewFeature(uf.Geometry, props)
			fc.AddFeature(f)
		}
	}

	addFeatures(um.Walls, "wall")
	addFeatures(um.Floors, "floor")
	addFeatures(um.Segments, "segment")

	return fc
}

// sourceVacuumIDs extracts unique vacuum IDs from a slice of FeatureSource.
func sourceVacuumIDs(sources []FeatureSource) []string {
	seen := make(map[string]struct{}, len(sources))
	var ids []string
	for _, s := range sources {
		if _, ok := seen[s.VacuumID]; !ok {
			seen[s.VacuumID] = struct{}{}
			ids = append(ids, s.VacuumID)
		}
	}
	sort.Strings(ids)
	return ids
}

// ComputeConfidence calculates the confidence score for a set of sources
// relative to the total number of vacuums. It counts distinct vacuum IDs
// and returns the ratio to totalVacuums.
func ComputeConfidence(sources []FeatureSource, totalVacuums int) float64 {
	if totalVacuums <= 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(sources))
	for _, s := range sources {
		seen[s.VacuumID] = struct{}{}
	}
	return float64(len(seen)) / float64(totalVacuums)
}

// AlignLinesToDirection ensures all lines in a cluster point in a consistent
// direction. Lines whose start-to-end vector is more than 90 degrees from
// the reference direction are reversed. This prevents the median computation
// from averaging misaligned endpoints.
func alignLinesToDirection(lines []orb.LineString) []orb.LineString {
	if len(lines) == 0 {
		return lines
	}

	// Use the first line's direction as reference.
	ref := lines[0]
	refDx := ref[len(ref)-1][0] - ref[0][0]
	refDy := ref[len(ref)-1][1] - ref[0][1]

	aligned := make([]orb.LineString, len(lines))
	aligned[0] = ref

	for i := 1; i < len(lines); i++ {
		ls := lines[i]
		dx := ls[len(ls)-1][0] - ls[0][0]
		dy := ls[len(ls)-1][1] - ls[0][1]

		// Dot product: if negative, the line points in the opposite direction.
		dot := refDx*dx + refDy*dy
		if dot < 0 {
			reversed := make(orb.LineString, len(ls))
			for j := range ls {
				reversed[j] = ls[len(ls)-1-j]
			}
			aligned[i] = reversed
		} else {
			aligned[i] = ls
		}
	}

	return aligned
}

// extractWallFeatures filters features to only include wall-type features
// (LayerType property is "wall" and geometry is LineString).
func extractWallFeatures(features []*Feature) []*Feature {
	var walls []*Feature
	for _, f := range features {
		if f.Geometry == nil {
			continue
		}
		if f.Geometry.Type != GeometryLineString {
			continue
		}
		lt, _ := f.Properties["layerType"].(string)
		if lt == "wall" || lt == "" {
			walls = append(walls, f)
		}
	}
	return walls
}

// DefaultFloorClusterDistance is the maximum distance (in mm) between floor
// polygon centroids for them to be grouped into the same cluster.
const DefaultFloorClusterDistance = 100.0

// UnifyFloors clusters floor/segment polygon features by proximity, unions
// overlapping polygons within each cluster, and returns unified features.
//
// Parameters:
//   - features: floor/segment features from all vacuums (Polygon geometry expected)
//   - sources: one FeatureSource per feature, in the same order
//   - totalVacuums: total number of vacuums contributing to the map
//
// Floor polygons are clustered by centroid proximity. Within each cluster,
// polygons are merged via UnionPolygons. Segment names are resolved by choosing
// the name from the vacuum with the highest coverage area for that segment.
func UnifyFloors(features []*Feature, sources []FeatureSource, totalVacuums int) []*UnifiedFeature {
	return UnifyFloorsWithOptions(features, sources, totalVacuums, DefaultFloorClusterDistance)
}

// UnifyFloorsWithOptions is like UnifyFloors but accepts a custom clustering
// distance parameter.
func UnifyFloorsWithOptions(features []*Feature, sources []FeatureSource, totalVacuums int, clusterDist float64) []*UnifiedFeature {
	if len(features) == 0 || totalVacuums <= 0 {
		return nil
	}

	// Filter to polygon features only.
	var polyFeatures []*Feature
	var polySources []FeatureSource
	for i, f := range features {
		if f.Geometry == nil || f.Geometry.Type != GeometryPolygon {
			continue
		}
		polyFeatures = append(polyFeatures, f)
		if i < len(sources) {
			polySources = append(polySources, sources[i])
		}
	}

	if len(polyFeatures) == 0 {
		return nil
	}

	// Group features by segment name. Features with the same segment name
	// are merged together regardless of spatial proximity. Features without
	// a segment name are grouped by spatial proximity instead.
	namedGroups := make(map[string][]*floorEntry)
	var unnamedFeatures []*Feature
	var unnamedSources []FeatureSource

	for i, f := range polyFeatures {
		name := segmentName(f)
		var src FeatureSource
		if i < len(polySources) {
			src = polySources[i]
		}
		if name != "" {
			namedGroups[name] = append(namedGroups[name], &floorEntry{
				feature: f,
				source:  src,
			})
		} else {
			unnamedFeatures = append(unnamedFeatures, f)
			if i < len(polySources) {
				unnamedSources = append(unnamedSources, polySources[i])
			}
		}
	}

	var result []*UnifiedFeature

	// Process named groups: merge all features sharing the same segment name.
	for name, entries := range namedGroups {
		uf := mergeFloorGroup(entries, name, totalVacuums)
		if uf != nil {
			result = append(result, uf)
		}
	}

	// Process unnamed features: cluster by proximity, then merge each cluster.
	if len(unnamedFeatures) > 0 {
		sourceMap := make(map[*Feature]FeatureSource, len(unnamedFeatures))
		for i, f := range unnamedFeatures {
			if i < len(unnamedSources) {
				sourceMap[f] = unnamedSources[i]
			}
		}

		clusters := ClusterByProximity(unnamedFeatures, clusterDist)
		for _, cluster := range clusters {
			var entries []*floorEntry
			for _, f := range cluster {
				src := sourceMap[f]
				entries = append(entries, &floorEntry{feature: f, source: src})
			}
			uf := mergeFloorGroup(entries, "", totalVacuums)
			if uf != nil {
				result = append(result, uf)
			}
		}
	}

	// Sort by area descending for deterministic output.
	sort.Slice(result, func(i, j int) bool {
		areaI := floorArea(result[i])
		areaJ := floorArea(result[j])
		return areaI > areaJ
	})

	return result
}

// floorEntry pairs a feature with its provenance source.
type floorEntry struct {
	feature *Feature
	source  FeatureSource
}

// mergeFloorGroup unions the polygons from a group of floor entries and
// produces a single UnifiedFeature. The segment name is resolved using
// resolveSegmentName (highest area wins). The confidence score reflects
// how many distinct vacuums observed the floor region.
func mergeFloorGroup(entries []*floorEntry, name string, totalVacuums int) *UnifiedFeature {
	if len(entries) == 0 {
		return nil
	}

	// Collect polygon geometries for union.
	var geoms []*Geometry
	var clusterSources []FeatureSource
	var clusterFeatures []*Feature
	for _, e := range entries {
		geoms = append(geoms, e.feature.Geometry)
		clusterSources = append(clusterSources, e.source)
		clusterFeatures = append(clusterFeatures, e.feature)
	}

	// Union all polygons into one.
	var mergedGeom *Geometry
	if len(geoms) == 1 {
		mergedGeom = geoms[0]
	} else {
		mergedGeom = UnionPolygons(geoms)
	}
	if mergedGeom == nil {
		return nil
	}

	// Count distinct vacuums.
	vacuumSet := make(map[string]struct{})
	for _, s := range clusterSources {
		if s.VacuumID != "" {
			vacuumSet[s.VacuumID] = struct{}{}
		}
	}
	observationCount := len(vacuumSet)
	confidence := float64(observationCount) / float64(totalVacuums)

	// Resolve segment name: highest area wins among conflicting names.
	resolvedName := name
	if resolvedName == "" {
		resolvedName = resolveSegmentName(clusterFeatures)
	}

	// Merge properties, preferring highest area source for conflicts.
	mergedProps := mergeFloorProperties(clusterFeatures, clusterSources)
	if resolvedName != "" {
		mergedProps["segmentName"] = resolvedName
	}
	mergedProps["observationCount"] = observationCount
	mergedProps["confidence"] = confidence

	return &UnifiedFeature{
		Geometry:         mergedGeom,
		Properties:       mergedProps,
		Sources:          clusterSources,
		Confidence:       confidence,
		ObservationCount: observationCount,
	}
}

// resolveSegmentName picks the segment name from the feature with the highest
// reported area. When features have conflicting names, the largest-area
// observation is considered the most authoritative.
func resolveSegmentName(features []*Feature) string {
	var bestName string
	var bestArea float64

	for _, f := range features {
		name := segmentName(f)
		if name == "" {
			continue
		}
		area := featureArea(f)
		if bestName == "" || area > bestArea {
			bestName = name
			bestArea = area
		}
	}

	return bestName
}

// segmentName extracts the segmentName property from a feature.
func segmentName(f *Feature) string {
	if f == nil || f.Properties == nil {
		return ""
	}
	name, _ := f.Properties["segmentName"].(string)
	return name
}

// featureArea extracts the area property from a feature.
// Returns 0 if the property is missing or not a number.
func featureArea(f *Feature) float64 {
	if f == nil || f.Properties == nil {
		return 0
	}
	switch v := f.Properties["area"].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	default:
		return 0
	}
}

// mergeFloorProperties combines properties from floor features, preferring
// values from the feature with the highest area when keys conflict.
func mergeFloorProperties(features []*Feature, sources []FeatureSource) map[string]interface{} {
	merged := make(map[string]interface{})
	bestArea := make(map[string]float64)

	for i, f := range features {
		area := featureArea(f)
		// Fall back to ICP score if no area is set.
		if area == 0 && i < len(sources) {
			area = sources[i].ICPScore
		}

		for k, v := range f.Properties {
			prev, exists := bestArea[k]
			if !exists || area > prev {
				merged[k] = v
				bestArea[k] = area
			}
		}
	}

	return merged
}

// floorArea extracts the area from a UnifiedFeature's properties.
func floorArea(uf *UnifiedFeature) float64 {
	if uf == nil || uf.Properties == nil {
		return 0
	}
	switch v := uf.Properties["area"].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return 0
	}
}

// extractFloorFeatures filters features to only include floor/segment-type
// features (LayerType property is "floor" or "segment" and geometry is Polygon).
func extractFloorFeatures(features []*Feature) []*Feature {
	var floors []*Feature
	for _, f := range features {
		if f.Geometry == nil {
			continue
		}
		if f.Geometry.Type != GeometryPolygon {
			continue
		}
		lt, _ := f.Properties["layerType"].(string)
		if lt == "floor" || lt == "segment" || lt == "" {
			floors = append(floors, f)
		}
	}
	return floors
}

// --- Outlier Detection ---

// DefaultOutlierConfidenceThreshold is the minimum confidence score a feature
// must have to avoid being flagged as an outlier. Features below this threshold
// are excluded from the unified map.
const DefaultOutlierConfidenceThreshold = 0.3

// DefaultIsolationDistanceMultiplier controls how far a feature's centroid must
// be from the map centroid (as a multiple of the mean distance) to be considered
// spatially isolated.
const DefaultIsolationDistanceMultiplier = 3.0

// OutlierConfig holds configurable thresholds for outlier detection.
type OutlierConfig struct {
	// ConfidenceThreshold is the minimum confidence score; features below
	// this value are excluded. Default: 0.3.
	ConfidenceThreshold float64

	// IsolationMultiplier is the factor of mean centroid distance beyond which
	// a feature is considered spatially isolated. Default: 3.0.
	IsolationMultiplier float64

	// MinICPScore is the minimum ICP alignment score for a source to be
	// considered high quality. Sources below this contribute less to
	// confidence. Default: 0.5.
	MinICPScore float64

	// TotalVacuums is the total number of vacuums in the system. Required
	// for computing observation-based confidence.
	TotalVacuums int
}

// DefaultOutlierConfig returns an OutlierConfig with sensible defaults.
func DefaultOutlierConfig(totalVacuums int) OutlierConfig {
	return OutlierConfig{
		ConfidenceThreshold: DefaultOutlierConfidenceThreshold,
		IsolationMultiplier: DefaultIsolationDistanceMultiplier,
		MinICPScore:         0.5,
		TotalVacuums:        totalVacuums,
	}
}

// OutlierReason describes why a feature was flagged as an outlier.
type OutlierReason string

const (
	// OutlierGhostRoom indicates the feature was observed by only one vacuum.
	OutlierGhostRoom OutlierReason = "ghost_room"

	// OutlierLowConfidence indicates the feature's weighted confidence fell
	// below the configured threshold.
	OutlierLowConfidence OutlierReason = "low_confidence"

	// OutlierIsolated indicates the feature is spatially far from the map
	// centroid relative to other features.
	OutlierIsolated OutlierReason = "isolated"
)

// OutlierResult describes a feature that was flagged as an outlier.
type OutlierResult struct {
	Feature    *UnifiedFeature `json:"feature"`
	Reasons    []OutlierReason `json:"reasons"`
	Confidence float64         `json:"confidence"`
}

// DetectOutliers examines a slice of unified features and returns two slices:
// retained features that pass quality checks, and outlier results for those
// that do not. A feature is flagged if any of the following hold:
//
//   - Ghost room: observed by only 1 vacuum when TotalVacuums > 1
//   - Low confidence: ICP-weighted confidence is below ConfidenceThreshold
//   - Spatially isolated: centroid distance from map center exceeds
//     IsolationMultiplier * mean distance
//
// Features may carry multiple reasons simultaneously.
func DetectOutliers(features []*UnifiedFeature, config OutlierConfig) (retained []*UnifiedFeature, outliers []OutlierResult) {
	if len(features) == 0 {
		return nil, nil
	}

	if config.TotalVacuums <= 0 {
		config.TotalVacuums = 1
	}

	// Compute the map-wide centroid and per-feature centroids.
	centroids := make([]orb.Point, len(features))
	var sumX, sumY float64
	validCount := 0
	for i, f := range features {
		if c, ok := geometryCentroid(f.Geometry); ok {
			centroids[i] = c
			sumX += c[0]
			sumY += c[1]
			validCount++
		}
	}

	var mapCentroid orb.Point
	if validCount > 0 {
		mapCentroid = orb.Point{sumX / float64(validCount), sumY / float64(validCount)}
	}

	// Compute distances from each feature centroid to the map centroid.
	distances := make([]float64, len(features))
	var totalDist float64
	for i := range features {
		dx := centroids[i][0] - mapCentroid[0]
		dy := centroids[i][1] - mapCentroid[1]
		distances[i] = math.Hypot(dx, dy)
		totalDist += distances[i]
	}

	var meanDist float64
	if validCount > 0 {
		meanDist = totalDist / float64(validCount)
	}

	isolationThreshold := config.IsolationMultiplier * meanDist

	for i, f := range features {
		var reasons []OutlierReason

		// 1. Ghost room: seen by only 1 vacuum when multiple exist.
		if config.TotalVacuums > 1 && f.ObservationCount <= 1 {
			reasons = append(reasons, OutlierGhostRoom)
		}

		// 2. Compute ICP-weighted confidence.
		weightedConf := ComputeConfidenceWeighted(f.Sources, config.TotalVacuums, config.MinICPScore)

		if weightedConf < config.ConfidenceThreshold {
			reasons = append(reasons, OutlierLowConfidence)
		}

		// 3. Spatial isolation.
		if meanDist > 0 && distances[i] > isolationThreshold {
			reasons = append(reasons, OutlierIsolated)
		}

		if len(reasons) > 0 {
			outliers = append(outliers, OutlierResult{
				Feature:    f,
				Reasons:    reasons,
				Confidence: weightedConf,
			})
		} else {
			retained = append(retained, f)
		}
	}

	return retained, outliers
}

// ComputeConfidenceWeighted calculates a confidence score that accounts for
// both observation coverage and ICP alignment quality. It extends
// ComputeConfidence by applying a quality penalty: sources with an ICP score
// below minICPScore are counted at half weight.
//
// The formula is:
//
//	weightedCount = sum(weight_i for each unique vacuum)
//	weight_i = 1.0 if best ICP score for vacuum >= minICPScore, else 0.5
//	confidence = weightedCount / totalVacuums
//
// This means a vacuum with poor ICP alignment contributes only half as much
// confidence as one with good alignment.
func ComputeConfidenceWeighted(sources []FeatureSource, totalVacuums int, minICPScore float64) float64 {
	if totalVacuums <= 0 || len(sources) == 0 {
		return 0
	}

	// For each unique vacuum, take the best ICP score among its observations.
	bestScores := make(map[string]float64)
	for _, s := range sources {
		if s.VacuumID == "" {
			continue
		}
		if prev, ok := bestScores[s.VacuumID]; !ok || s.ICPScore > prev {
			bestScores[s.VacuumID] = s.ICPScore
		}
	}

	var weightedCount float64
	for _, score := range bestScores {
		if score >= minICPScore {
			weightedCount += 1.0
		} else {
			weightedCount += 0.5
		}
	}

	return weightedCount / float64(totalVacuums)
}

// FilterByConfidence removes features whose confidence score falls below the
// given threshold. This is a convenience function for post-processing unified
// features independently of full outlier detection.
func FilterByConfidence(features []*UnifiedFeature, threshold float64) []*UnifiedFeature {
	if len(features) == 0 {
		return nil
	}

	result := make([]*UnifiedFeature, 0, len(features))
	for _, f := range features {
		if f.Confidence >= threshold {
			result = append(result, f)
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// marshalCoordinate is a test helper - kept unexported. Encodes a 2-element
// float array to JSON for creating test geometries.
func marshalCoordinate(coords interface{}) json.RawMessage {
	b, _ := json.Marshal(coords)
	return b
}

// --- Incremental Refinement ---

// DefaultRefinementWeight controls how much weight the new observation gets
// relative to the previous observation when blending geometries.
// A value of 0.3 means the new observation contributes 30% and the previous
// 70%, providing gradual convergence.
const DefaultRefinementWeight = 0.3

// DefaultWallSimplifyTolerance is the Douglas-Peucker tolerance (in mm)
// applied to wall geometries after unification.
const DefaultWallSimplifyTolerance = 10.0

// DefaultFloorSimplifyTolerance is the Douglas-Peucker tolerance (in mm)
// applied to floor/segment polygon geometries after unification.
const DefaultFloorSimplifyTolerance = 20.0

// DefaultGridSnap is the grid spacing (in mm) for snapping vertices to reduce
// noise in unified features.
const DefaultGridSnap = 10.0

// refineFeatures blends previous unified features with newly computed ones
// using weighted averaging. Features are matched by proximity of their
// geometry centroids. Unmatched new features are appended as-is.
func refineFeatures(previous, current []*UnifiedFeature) []*UnifiedFeature {
	if len(previous) == 0 {
		return current
	}
	if len(current) == 0 {
		return current
	}

	// Build centroid index for previous features.
	type indexed struct {
		feature  *UnifiedFeature
		centroid orb.Point
		used     bool
	}
	prevIdx := make([]indexed, len(previous))
	for i, f := range previous {
		c, _ := geometryCentroid(f.Geometry)
		prevIdx[i] = indexed{feature: f, centroid: c}
	}

	result := make([]*UnifiedFeature, 0, len(current))

	for _, cur := range current {
		curCentroid, curOk := geometryCentroid(cur.Geometry)
		if !curOk {
			result = append(result, cur)
			continue
		}

		// Find the closest previous feature.
		bestDist := math.MaxFloat64
		bestIdx := -1
		for i, prev := range prevIdx {
			if prev.used {
				continue
			}
			dist := math.Hypot(curCentroid[0]-prev.centroid[0], curCentroid[1]-prev.centroid[1])
			if dist < bestDist {
				bestDist = dist
				bestIdx = i
			}
		}

		// Match threshold: features within 200mm are considered the same.
		const matchThreshold = 200.0
		if bestIdx >= 0 && bestDist <= matchThreshold {
			prevIdx[bestIdx].used = true
			blended := blendGeometry(prevIdx[bestIdx].feature.Geometry, cur.Geometry, DefaultRefinementWeight)
			if blended != nil {
				cur.Geometry = blended
			}
			// Update observation count to accumulate.
			cur.ObservationCount = max(cur.ObservationCount, prevIdx[bestIdx].feature.ObservationCount)
		}

		result = append(result, cur)
	}

	return result
}

// blendGeometry interpolates between two geometries of the same type.
// The weight parameter (0-1) controls how much the new geometry contributes.
// Returns nil if the geometries cannot be blended.
func blendGeometry(old, new_ *Geometry, weight float64) *Geometry {
	if old == nil || new_ == nil || old.Type != new_.Type {
		return new_
	}

	switch old.Type {
	case GeometryLineString:
		return blendLineStrings(old, new_, weight)
	case GeometryPolygon:
		return blendPolygons(old, new_, weight)
	default:
		return new_
	}
}

// blendLineStrings interpolates between two LineString geometries by
// resampling both to the same number of points and blending coordinates.
func blendLineStrings(old, new_ *Geometry, weight float64) *Geometry {
	oldLS := orbLineString(old)
	newLS := orbLineString(new_)
	if oldLS == nil || newLS == nil {
		return new_
	}

	// Resample both to the same number of points.
	n := max(len(oldLS), len(newLS))
	if n < 2 {
		n = 2
	}
	if n > 100 {
		n = 100
	}

	oldResampled := resampleLine(oldLS, n)
	newResampled := resampleLine(newLS, n)

	blended := make(orb.LineString, n)
	oldWeight := 1.0 - weight
	for i := 0; i < n; i++ {
		blended[i] = orb.Point{
			oldResampled[i][0]*oldWeight + newResampled[i][0]*weight,
			oldResampled[i][1]*oldWeight + newResampled[i][1]*weight,
		}
	}

	return lineStringToGeometry(blended)
}

// blendPolygons interpolates between two Polygon geometries by blending
// the outer ring vertices. Only the outer ring is blended; inner rings
// are taken from the new geometry.
func blendPolygons(old, new_ *Geometry, weight float64) *Geometry {
	oldPoly := orbPolygon(old)
	newPoly := orbPolygon(new_)
	if oldPoly == nil || newPoly == nil || len(oldPoly) == 0 || len(newPoly) == 0 {
		return new_
	}

	oldRing := oldPoly[0]
	newRing := newPoly[0]
	if len(oldRing) < 3 || len(newRing) < 3 {
		return new_
	}

	// Resample both outer rings to the same number of points.
	n := max(len(oldRing), len(newRing))
	if n > 200 {
		n = 200
	}

	oldResampled := resampleRing(oldRing, n)
	newResampled := resampleRing(newRing, n)

	blendedRing := make(orb.Ring, n)
	oldWeight := 1.0 - weight
	for i := 0; i < n; i++ {
		blendedRing[i] = orb.Point{
			oldResampled[i][0]*oldWeight + newResampled[i][0]*weight,
			oldResampled[i][1]*oldWeight + newResampled[i][1]*weight,
		}
	}

	// Build result polygon with blended outer ring and new inner rings.
	result := make(orb.Polygon, 0, len(newPoly))
	result = append(result, blendedRing)
	if len(newPoly) > 1 {
		result = append(result, newPoly[1:]...)
	}

	return polygonToGeometry(result)
}

// resampleRing resamples a polygon ring to n equidistant points.
// The ring is treated as a closed loop.
func resampleRing(ring orb.Ring, n int) []orb.Point {
	if len(ring) < 2 || n < 3 {
		pts := make([]orb.Point, n)
		if len(ring) > 0 {
			for i := range pts {
				pts[i] = ring[0]
			}
		}
		return pts
	}

	// Convert ring to line string for resampling.
	ls := make(orb.LineString, len(ring))
	copy(ls, ring)

	// Ensure the ring is closed for arc length computation.
	if len(ls) > 0 && (ls[0][0] != ls[len(ls)-1][0] || ls[0][1] != ls[len(ls)-1][1]) {
		ls = append(ls, ls[0])
	}

	return resampleLine(ls, n)
}

// simplifyUnifiedFeatures applies Douglas-Peucker simplification to all
// features in the slice. LineString features are simplified directly;
// Polygon features have their outer ring simplified.
func simplifyUnifiedFeatures(features []*UnifiedFeature, tolerance float64) {
	if tolerance <= 0 {
		return
	}
	for _, f := range features {
		if f.Geometry == nil {
			continue
		}
		switch f.Geometry.Type {
		case GeometryLineString:
			simplified := SimplifyLineString(f.Geometry, tolerance)
			if simplified != nil {
				f.Geometry = simplified
			}
		case GeometryPolygon:
			simplified := simplifyPolygon(f.Geometry, tolerance)
			if simplified != nil {
				f.Geometry = simplified
			}
		}
	}
}

// simplifyPolygon applies Douglas-Peucker simplification to a polygon's
// outer ring while preserving the polygon structure.
func simplifyPolygon(geom *Geometry, tolerance float64) *Geometry {
	poly := orbPolygon(geom)
	if len(poly) == 0 {
		return nil
	}

	simplified := make(orb.Polygon, len(poly))
	for i, ring := range poly {
		ls := orb.LineString(ring)
		s := simplify.DouglasPeucker(tolerance).Simplify(ls.Clone())
		result, ok := s.(orb.LineString)
		if !ok || len(result) < 3 {
			simplified[i] = ring
			continue
		}
		simplified[i] = orb.Ring(result)
	}

	return polygonToGeometry(simplified)
}

// snapToGrid rounds all geometry coordinates to the nearest grid point.
// This reduces floating-point noise from repeated blending operations.
func snapToGrid(geom *Geometry, gridSize float64) *Geometry {
	if geom == nil || gridSize <= 0 {
		return geom
	}

	snap := func(v float64) float64 {
		return math.Round(v/gridSize) * gridSize
	}

	switch geom.Type {
	case GeometryLineString:
		ls := orbLineString(geom)
		if ls == nil {
			return geom
		}
		for i := range ls {
			ls[i] = orb.Point{snap(ls[i][0]), snap(ls[i][1])}
		}
		return lineStringToGeometry(ls)

	case GeometryPolygon:
		poly := orbPolygon(geom)
		if poly == nil {
			return geom
		}
		for i, ring := range poly {
			for j := range ring {
				ring[j] = orb.Point{snap(ring[j][0]), snap(ring[j][1])}
			}
			poly[i] = ring
		}
		return polygonToGeometry(poly)

	default:
		return geom
	}
}
