# Design: Unified World Map Composite

**Issue**: `tudomesh-i7n`
**Status**: Planning
**Created**: 2026-02-07

## Problem Statement

After aligning individual vacuum maps into a world coordinate system using ICP, we need to **composite/merge** these maps into a single normalized representation. This unified map should:

1. **Consensus-based features**: If 3 out of 4 vacuums see a wall, include it. If only 1 vacuum sees a "ghost room", exclude it (likely a scan error).
2. **Normalized geometry**: Use median/centroid points to normalize walls and features across multiple observations.
3. **Incremental refinement**: Each vacuum pass should improve the unified map quality.
4. **Handle partial coverage**: Different vacuums may cover different areas. The composite should intelligently merge overlapping and non-overlapping regions.

## Current State

### Existing Architecture

**Map Alignment** (✓ Complete):
- Each vacuum map is aligned to a reference coordinate system using ICP (`mesh/icp.go`)
- Transforms stored in `CalibrationData` (`.calibration-cache.json`)
- Auto-calibration on docking
- Real-time position transforms via MQTT

**Map Representation**:
- `ValetudoMap` structure with layers (floor, segment, wall)
- GeoJSON export via `MapToFeatureCollection()` in `mesh/geojson.go`
- Features include:
  - Walls: `LineString` or `MultiLineString`
  - Floors/Segments: `Polygon` with properties (segmentId, name, area)
- Each feature has properties: `layerType`, `vacuumId`, `segmentId`, `segmentName`, `area`

**Current Output**:
- Individual per-vacuum GeoJSON FeatureCollections
- No merging or normalization across vacuums
- HTTP endpoint serves separate maps

### Files Involved

- `mesh/types.go` - Data structures (`ValetudoMap`, `MapLayer`, `Point`, `AffineMatrix`)
- `mesh/geojson.go` - GeoJSON conversion (176 LOC)
- `mesh/transform.go` - Coordinate transformations (234 LOC)
- `mesh/icp.go` - ICP alignment (835 LOC)
- `mesh/calibration.go` - Calibration management (229 LOC)
- `mesh/state.go` - StateTracker for live positions (100 LOC)
- `handlers.go` - HTTP endpoints

## Proposed Solution

### Architecture

```
┌──────────────────┐
│ Individual Maps  │
│ (aligned to ref) │
└────────┬─────────┘
         │ Transform
         ▼
┌──────────────────┐
│ World Coordinate │
│  GeoJSON (each)  │
└────────┬─────────┘
         │ Merge/Normalize
         ▼
┌──────────────────┐
│ Unified Composite│
│     GeoJSON      │
└──────────────────┘
```

### Phase 1: GeoJSON Library Selection

**Research Go GeoJSON libraries** for merging/union operations:

1. **`paulmach/orb`** - Popular, well-maintained
   - Supports GeoJSON parsing/writing
   - Has spatial operations (union, intersection, simplification)
   - Used by Uber, Mapbox

2. **`tidwall/gjson`** - Fast JSON parsing
   - Good for reading, but limited spatial operations
   - Could be used for preprocessing

3. **`peterstace/simplefeatures`** - Geometry operations
   - OGC Simple Features compliance
   - Supports unions, intersections, buffers

**Recommendation**: Start with `paulmach/orb` for GeoJSON operations, potentially augmented with `peterstace/simplefeatures` for advanced spatial operations.

### Phase 2: Feature Normalization Strategy

#### Wall Normalization

Walls are the most critical structural features. Strategy:

1. **Buffer-based clustering**:
   - Apply a small buffer (e.g., 50mm) to wall LineStrings
   - Cluster overlapping buffered walls from different vacuums

2. **Median line computation**:
   - For each cluster, compute the median/centroid line
   - Use Douglas-Peucker simplification to reduce noise

3. **Confidence scoring**:
   - Weight by number of vacuums observing the wall
   - Weight by ICP alignment quality (from `ICPResult.Score`)
   - Threshold: Include if seen by ≥50% of vacuums

#### Floor/Segment Normalization

1. **Union overlapping polygons** from different vacuums
2. **Preserve segment names** from the vacuum with highest area coverage
3. **Merge adjacent segments** with same name

#### Outlier Detection

1. **Ghost room detection**:
   - If a floor polygon is only observed by 1 vacuum
   - AND it doesn't overlap with any other vacuum's coverage
   - AND the vacuum has low ICP score
   - → Mark as potential outlier, exclude from composite

2. **Scan error detection**:
   - Isolated features far from centroid
   - Features with very different geometry from consensus

### Phase 3: Data Structures

```go
// UnifiedMap represents a composite map from multiple vacuums
type UnifiedMap struct {
    Walls    []*UnifiedFeature `json:"walls"`
    Floors   []*UnifiedFeature `json:"floors"`
    Segments []*UnifiedFeature `json:"segments"`
    Metadata UnifiedMetadata   `json:"metadata"`
}

// UnifiedFeature is a feature derived from multiple vacuum observations
type UnifiedFeature struct {
    Geometry       *Geometry              `json:"geometry"`        // Merged geometry
    Properties     map[string]interface{} `json:"properties"`      // Merged properties
    Sources        []FeatureSource        `json:"sources"`         // Contributing vacuums
    Confidence     float64                `json:"confidence"`      // 0-1 confidence score
    ObservationCount int                  `json:"observationCount"` // How many vacuums saw this
}

// FeatureSource tracks which vacuum contributed to a unified feature
type FeatureSource struct {
    VacuumID     string        `json:"vacuumId"`
    OriginalGeom *Geometry     `json:"originalGeometry"`
    Timestamp    int64         `json:"timestamp"`
    ICPScore     float64       `json:"icpScore"` // Alignment quality
}

// UnifiedMetadata provides provenance information
type UnifiedMetadata struct {
    VacuumCount      int       `json:"vacuumCount"`
    ReferenceVacuum  string    `json:"referenceVacuum"`
    LastUpdated      int64     `json:"lastUpdated"`
    TotalArea        float64   `json:"totalArea"` // sq mm
    CoverageOverlap  float64   `json:"coverageOverlap"` // 0-1, how much overlap
}
```

### Phase 4: Implementation

#### New Files

1. **`mesh/unifier.go`** - Main unification logic
   ```go
   func UnifyMaps(maps map[string]*ValetudoMap, calibration *CalibrationData) (*UnifiedMap, error)
   func UnifyWalls(features []*Feature, sources []FeatureSource) []*UnifiedFeature
   func UnifyFloors(features []*Feature, sources []FeatureSource) []*UnifiedFeature
   func ComputeConfidence(sources []FeatureSource) float64
   ```

2. **`mesh/geojson_merge.go`** - GeoJSON spatial operations
   ```go
   func BufferLineString(geom *Geometry, distance float64) *Geometry
   func UnionPolygons(geoms []*Geometry) *Geometry
   func ClusterByProximity(features []*Feature, maxDist float64) [][]*Feature
   func MedianLine(lines []*Geometry) *Geometry
   ```

3. **`mesh/unifier_test.go`** - Comprehensive tests

#### Modified Files

1. **`handlers.go`** - New HTTP endpoint
   - Add `/unified-map` endpoint returning unified GeoJSON
   - Keep existing `/map` for individual vacuum maps

2. **`mesh/state.go`** - Cache unified map
   - Add `UnifiedMap` field to `StateTracker`
   - Recompute unified map when any vacuum map updates

3. **`go.mod`** - Add dependencies
   ```
   github.com/paulmach/orb v0.11.1
   github.com/peterstace/simplefeatures v0.49.0 (optional)
   ```

### Phase 5: Incremental Refinement

**Strategy**: Each new map update refines the unified map

1. **Weighted averaging**:
   - New observations update existing features via weighted average
   - Weight = `(old_confidence * old_count + new_score) / (old_count + 1)`

2. **Outlier pruning**:
   - Features with declining confidence (as more data arrives) get removed
   - Threshold: Drop features with confidence < 0.3

3. **Geometry refinement**:
   - Apply Douglas-Peucker simplification with adaptive tolerance
   - Snap vertices to grid (e.g., 10mm) to reduce noise

## Implementation Plan

### Milestone 1: Library Integration (2-3 hours)
- [ ] Add `paulmach/orb` dependency
- [ ] Create `mesh/geojson_merge.go` with basic spatial operations
- [ ] Test: Buffer, union, simplification functions

### Milestone 2: Wall Unification (3-4 hours)
- [ ] Implement wall clustering by proximity
- [ ] Compute median line for wall clusters
- [ ] Add confidence scoring based on observation count
- [ ] Test: 2-4 vacuum wall alignment scenarios

### Milestone 3: Floor/Segment Unification (2-3 hours)
- [ ] Implement polygon union for floors
- [ ] Merge segment metadata (names, areas)
- [ ] Handle non-overlapping regions
- [ ] Test: Overlapping and disjoint floor scenarios

### Milestone 4: Outlier Detection (2 hours)
- [ ] Implement ghost room detection
- [ ] Add ICP score weighting
- [ ] Test: Scenarios with intentional outliers

### Milestone 5: HTTP Integration (1-2 hours)
- [ ] Add `/unified-map` endpoint
- [ ] Cache unified map in `StateTracker`
- [ ] Add metadata to response
- [ ] Test: HTTP endpoint with multiple vacuums

### Milestone 6: Incremental Refinement (3-4 hours)
- [ ] Implement weighted update mechanism
- [ ] Add geometry refinement (simplification, snapping)
- [ ] Test: Sequential map updates improve quality
- [ ] Performance test: 10+ vacuum updates

## Testing Strategy

### Unit Tests

1. **Spatial Operations** (`geojson_merge_test.go`)
   - Buffer expansion/contraction
   - Polygon union with holes
   - LineString clustering

2. **Unification Logic** (`unifier_test.go`)
   - 2 vacuums, perfect overlap → single feature
   - 3 vacuums, 2 agree, 1 outlier → consensus wins
   - 4 vacuums, non-overlapping coverage → all included

3. **Edge Cases**
   - Single vacuum (should pass through unchanged)
   - All vacuums have different geometries (use centroid/median)
   - Empty map from one vacuum

### Integration Tests

1. **Real Map Data** (`unifier_integration_test.go`)
   - Use sample Valetudo JSON maps
   - Test full pipeline: Load → Align → Unify
   - Verify output GeoJSON is valid

2. **Performance**
   - 10 vacuums, 100 features each → unified map
   - Target: < 500ms for unification

## Open Questions

1. **Segment naming conflicts**: If two vacuums name overlapping segments differently, which name wins?
   - **Proposal**: Use name from vacuum with highest coverage area

2. **Wall thickness**: Should we normalize wall thickness or keep as LineStrings?
   - **Proposal**: Keep as LineStrings (centerlines) for simplicity. Future: could buffer to polygons.

3. **Temporal weighting**: Should newer observations have higher weight?
   - **Proposal**: Phase 1 uses equal weighting. Phase 2 adds time decay (e.g., exponential decay with 7-day half-life).

4. **Persistence**: Should unified map be cached to disk?
   - **Proposal**: Yes, save to `.unified-map.json` for faster startup. Regenerate if any vacuum calibration changes.

5. **Memory usage**: With many vacuums, keeping all feature sources could be expensive.
   - **Proposal**: Limit to last N sources per feature (e.g., N=10). Compact older sources into aggregated statistics.

## Success Criteria

1. ✅ **Consensus-based merging**: 3/4 vacuums see a wall → included. 1/4 sees ghost room → excluded.
2. ✅ **Normalized geometry**: Overlapping walls from multiple vacuums → single median wall.
3. ✅ **Incremental refinement**: Adding a new vacuum observation improves map quality (reduced noise, better alignment).
4. ✅ **HTTP endpoint**: `/unified-map` returns valid GeoJSON with confidence scores.
5. ✅ **Performance**: Unification completes in < 500ms for 10 vacuums.

## Non-Goals

- **Real-time 3D reconstruction**: Out of scope (2D only).
- **Dynamic obstacle tracking**: Unified map represents static structure, not moving objects.
- **Path planning**: Unified map is for visualization and analytics, not navigation.
- **Historical map versions**: No time-series storage (only current unified map).

## References

- [paulmach/orb](https://github.com/paulmach/orb) - GeoJSON and spatial operations
- [peterstace/simplefeatures](https://github.com/peterstace/simplefeatures) - OGC Simple Features
- [Douglas-Peucker Algorithm](https://en.wikipedia.org/wiki/Ramer%E2%80%93Douglas%E2%80%93Peucker_algorithm) - Polyline simplification
- [ICP Algorithm](https://en.wikipedia.org/wiki/Iterative_closest_point) - Point cloud alignment (already implemented)

## Dependencies on Other Work

- **Requires**: ICP alignment (✓ Complete - `mesh/icp.go`)
- **Requires**: GeoJSON export (✓ Complete - `mesh/geojson.go`)
- **Blocks**: Advanced analytics (e.g., coverage heatmaps, temporal analysis)
- **Blocks**: Multi-floor support (needs unified map per floor first)
