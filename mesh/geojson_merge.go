package mesh

import (
	"encoding/json"
	"math"
	"sort"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/planar"
	"github.com/paulmach/orb/simplify"
)

// orbLineString converts a mesh Geometry of type LineString to an orb.LineString.
// Returns nil if the geometry is nil, not a LineString, or has invalid coordinates.
func orbLineString(geom *Geometry) orb.LineString {
	if geom == nil || geom.Type != GeometryLineString {
		return nil
	}
	var coords [][2]float64
	if err := json.Unmarshal(geom.Coordinates, &coords); err != nil {
		return nil
	}
	ls := make(orb.LineString, len(coords))
	for i, c := range coords {
		ls[i] = orb.Point{c[0], c[1]}
	}
	return ls
}

// orbPolygon converts a mesh Geometry of type Polygon to an orb.Polygon.
// Returns nil if the geometry is nil, not a Polygon, or has invalid coordinates.
func orbPolygon(geom *Geometry) orb.Polygon {
	if geom == nil || geom.Type != GeometryPolygon {
		return nil
	}
	var rings [][][2]float64
	if err := json.Unmarshal(geom.Coordinates, &rings); err != nil {
		return nil
	}
	poly := make(orb.Polygon, len(rings))
	for i, ring := range rings {
		r := make(orb.Ring, len(ring))
		for j, c := range ring {
			r[j] = orb.Point{c[0], c[1]}
		}
		poly[i] = r
	}
	return poly
}

// lineStringToGeometry converts an orb.LineString back to a mesh Geometry.
func lineStringToGeometry(ls orb.LineString) *Geometry {
	coords := make([][2]float64, len(ls))
	for i, p := range ls {
		coords[i] = [2]float64{p[0], p[1]}
	}
	coordsJSON, _ := json.Marshal(coords)
	return &Geometry{
		Type:        GeometryLineString,
		Coordinates: coordsJSON,
	}
}

// polygonToGeometry converts an orb.Polygon back to a mesh Geometry.
func polygonToGeometry(poly orb.Polygon) *Geometry {
	rings := make([][][2]float64, len(poly))
	for i, ring := range poly {
		r := make([][2]float64, len(ring))
		for j, p := range ring {
			r[j] = [2]float64{p[0], p[1]}
		}
		rings[i] = r
	}
	coordsJSON, _ := json.Marshal(rings)
	return &Geometry{
		Type:        GeometryPolygon,
		Coordinates: coordsJSON,
	}
}

// BufferLineString expands a LineString into a Polygon by buffering it at the
// given distance. The result is an axis-aligned polygon that encloses all points
// within distance of the original line. This is a simplified rectangular buffer
// computed per-segment and merged into a single bounding polygon.
//
// The distance parameter is in the same units as the geometry coordinates
// (typically millimeters in this project).
//
// Returns nil if the input geometry is nil, not a LineString, or has fewer
// than 2 points.
func BufferLineString(geom *Geometry, distance float64) *Geometry {
	ls := orbLineString(geom)
	if len(ls) < 2 {
		return nil
	}

	// Build a buffered polygon by offsetting each segment perpendicular
	// to its direction, then collecting the outer hull.
	// For each segment we generate 4 corner points (a rectangle around
	// the segment at the given distance). We collect all points and
	// compute a convex-hull-like outline.
	var allPoints []orb.Point

	for i := 0; i < len(ls)-1; i++ {
		p0 := ls[i]
		p1 := ls[i+1]

		dx := p1[0] - p0[0]
		dy := p1[1] - p0[1]
		length := math.Hypot(dx, dy)
		if length == 0 {
			continue
		}

		// Unit normal perpendicular to segment direction
		nx := -dy / length * distance
		ny := dx / length * distance

		// Four corners of the buffered rectangle around this segment
		allPoints = append(allPoints,
			orb.Point{p0[0] + nx, p0[1] + ny},
			orb.Point{p0[0] - nx, p0[1] - ny},
			orb.Point{p1[0] + nx, p1[1] + ny},
			orb.Point{p1[0] - nx, p1[1] - ny},
		)
	}

	if len(allPoints) < 3 {
		return nil
	}

	// Compute convex hull of all buffer points
	hull := convexHull(allPoints)

	// Close the ring
	if len(hull) > 0 && (hull[0][0] != hull[len(hull)-1][0] || hull[0][1] != hull[len(hull)-1][1]) {
		hull = append(hull, hull[0])
	}

	poly := orb.Polygon{orb.Ring(hull)}
	return polygonToGeometry(poly)
}

// UnionPolygons merges multiple polygon geometries into a single polygon that
// represents the bounding area covering all inputs. This implementation computes
// the convex hull of all polygon vertices, which is a conservative union suitable
// for floor/segment merging where exact boundary preservation is less critical
// than coverage.
//
// Non-polygon geometries in the input slice are silently skipped.
// Returns nil if no valid polygons are provided.
func UnionPolygons(geoms []*Geometry) *Geometry {
	var allPoints []orb.Point

	for _, g := range geoms {
		poly := orbPolygon(g)
		if poly == nil {
			continue
		}
		for _, ring := range poly {
			for _, p := range ring {
				allPoints = append(allPoints, p)
			}
		}
	}

	if len(allPoints) < 3 {
		return nil
	}

	hull := convexHull(allPoints)

	// Close the ring
	if len(hull) > 0 && (hull[0][0] != hull[len(hull)-1][0] || hull[0][1] != hull[len(hull)-1][1]) {
		hull = append(hull, hull[0])
	}

	poly := orb.Polygon{orb.Ring(hull)}
	return polygonToGeometry(poly)
}

// ClusterByProximity groups features whose geometries are within maxDist of each
// other. Two features are considered proximate if the minimum distance between
// the centroids of their bounding boxes is less than or equal to maxDist.
//
// The algorithm uses single-linkage clustering: if feature A is near feature B,
// and feature B is near feature C, all three end up in the same cluster even if
// A and C are far apart.
//
// Features with nil geometry are placed into their own singleton cluster.
// Returns nil if the input slice is empty.
func ClusterByProximity(features []*Feature, maxDist float64) [][]*Feature {
	if len(features) == 0 {
		return nil
	}

	// Compute centroid for each feature's bounding box
	type entry struct {
		feature  *Feature
		centroid orb.Point
		valid    bool
	}

	entries := make([]entry, len(features))
	for i, f := range features {
		entries[i].feature = f
		if f.Geometry == nil {
			continue
		}
		c, ok := geometryCentroid(f.Geometry)
		if !ok {
			continue
		}
		entries[i].centroid = c
		entries[i].valid = true
	}

	uf := newUnionFind(len(entries))

	// Compare all pairs for proximity
	for i := 0; i < len(entries); i++ {
		if !entries[i].valid {
			continue
		}
		for j := i + 1; j < len(entries); j++ {
			if !entries[j].valid {
				continue
			}
			dist := planar.Distance(entries[i].centroid, entries[j].centroid)
			if dist <= maxDist {
				uf.union(i, j)
			}
		}
	}

	// Collect clusters
	clusters := make(map[int][]*Feature)
	for i := range entries {
		root := uf.find(i)
		clusters[root] = append(clusters[root], entries[i].feature)
	}

	result := make([][]*Feature, 0, len(clusters))
	for _, cluster := range clusters {
		result = append(result, cluster)
	}

	// Sort clusters by size descending for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return len(result[i]) > len(result[j])
	})

	return result
}

// SimplifyLineString applies the Douglas-Peucker algorithm to reduce the number
// of points in a LineString while preserving its shape within the given tolerance.
//
// The tolerance parameter controls how much the simplified line can deviate from
// the original, in coordinate units (typically millimeters).
//
// Returns nil if the input is nil or not a LineString.
func SimplifyLineString(geom *Geometry, tolerance float64) *Geometry {
	ls := orbLineString(geom)
	if ls == nil {
		return nil
	}

	simplified := simplify.DouglasPeucker(tolerance).Simplify(ls.Clone())
	result, ok := simplified.(orb.LineString)
	if !ok {
		return nil
	}

	return lineStringToGeometry(result)
}

// geometryCentroid returns the center point of a geometry's bounding box.
// The bool return indicates whether a valid centroid was computed.
func geometryCentroid(geom *Geometry) (orb.Point, bool) {
	if geom == nil {
		return orb.Point{}, false
	}

	switch geom.Type {
	case GeometryPoint:
		var coords [2]float64
		if err := json.Unmarshal(geom.Coordinates, &coords); err != nil {
			return orb.Point{}, false
		}
		return orb.Point{coords[0], coords[1]}, true

	case GeometryLineString:
		ls := orbLineString(geom)
		if ls == nil {
			return orb.Point{}, false
		}
		return ls.Bound().Center(), true

	case GeometryPolygon:
		poly := orbPolygon(geom)
		if poly == nil {
			return orb.Point{}, false
		}
		return poly.Bound().Center(), true

	default:
		bound := geometryBound(geom)
		center := bound.Center()
		// Check that we got a valid parse by verifying the bound is not
		// the default zero-value (which has Min > Max).
		if bound.Min[0] > bound.Max[0] {
			return orb.Point{}, false
		}
		return center, true
	}
}

// geometryBound computes the bounding box of a Geometry by parsing its
// coordinates. Supports Point, LineString, Polygon, MultiLineString, and
// MultiPolygon types.
func geometryBound(geom *Geometry) orb.Bound {
	if geom == nil {
		return orb.Bound{}
	}

	switch geom.Type {
	case GeometryPoint:
		var coords [2]float64
		if err := json.Unmarshal(geom.Coordinates, &coords); err != nil {
			return orb.Bound{}
		}
		p := orb.Point{coords[0], coords[1]}
		return p.Bound()

	case GeometryLineString:
		ls := orbLineString(geom)
		if ls == nil {
			return orb.Bound{}
		}
		return ls.Bound()

	case GeometryPolygon:
		poly := orbPolygon(geom)
		if poly == nil {
			return orb.Bound{}
		}
		return poly.Bound()

	case GeometryMultiLineString:
		var lines [][][2]float64
		if err := json.Unmarshal(geom.Coordinates, &lines); err != nil {
			return orb.Bound{}
		}
		var mls orb.MultiLineString
		for _, line := range lines {
			ls := make(orb.LineString, len(line))
			for j, c := range line {
				ls[j] = orb.Point{c[0], c[1]}
			}
			mls = append(mls, ls)
		}
		return mls.Bound()

	case GeometryMultiPolygon:
		var polys [][][][2]float64
		if err := json.Unmarshal(geom.Coordinates, &polys); err != nil {
			return orb.Bound{}
		}
		var mp orb.MultiPolygon
		for _, rings := range polys {
			poly := make(orb.Polygon, len(rings))
			for i, ring := range rings {
				r := make(orb.Ring, len(ring))
				for j, c := range ring {
					r[j] = orb.Point{c[0], c[1]}
				}
				poly[i] = r
			}
			mp = append(mp, poly)
		}
		return mp.Bound()
	}

	return orb.Bound{}
}

// unionFind implements a disjoint-set data structure with path compression.
type unionFind struct {
	parent []int
}

func newUnionFind(n int) *unionFind {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	return &unionFind{parent: p}
}

func (uf *unionFind) find(x int) int {
	for uf.parent[x] != x {
		uf.parent[x] = uf.parent[uf.parent[x]]
		x = uf.parent[x]
	}
	return x
}

func (uf *unionFind) union(a, b int) {
	ra, rb := uf.find(a), uf.find(b)
	if ra != rb {
		uf.parent[ra] = rb
	}
}

// convexHull computes the convex hull of a set of 2D points using the
// Andrew's monotone chain algorithm. Returns points in counter-clockwise order.
func convexHull(points []orb.Point) []orb.Point {
	if len(points) < 3 {
		result := make([]orb.Point, len(points))
		copy(result, points)
		return result
	}

	// Sort by x, then y
	sorted := make([]orb.Point, len(points))
	copy(sorted, points)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i][0] != sorted[j][0] {
			return sorted[i][0] < sorted[j][0]
		}
		return sorted[i][1] < sorted[j][1]
	})

	// cross returns the cross product of vectors OA and OB where O is origin
	cross := func(o, a, b orb.Point) float64 {
		return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0])
	}

	n := len(sorted)
	hull := make([]orb.Point, 0, 2*n)

	// Lower hull
	for _, p := range sorted {
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}

	// Upper hull
	lower := len(hull) + 1
	for i := n - 2; i >= 0; i-- {
		p := sorted[i]
		for len(hull) >= lower && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}

	// Remove last point (duplicate of first)
	return hull[:len(hull)-1]
}
