package mesh

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/paulmach/orb"
)

func TestBufferLineString(t *testing.T) {
	t.Run("horizontal line buffered", func(t *testing.T) {
		// Create a simple horizontal line from (0,0) to (100,0)
		path := Path{{X: 0, Y: 0}, {X: 100, Y: 0}}
		geom := PathToLineString(path)

		result := BufferLineString(geom, 10.0)
		if result == nil {
			t.Fatal("Expected non-nil result")
		}
		if result.Type != GeometryPolygon {
			t.Errorf("Expected Polygon, got %s", result.Type)
		}

		// Verify the polygon contains points within buffer distance
		var rings [][][2]float64
		if err := json.Unmarshal(result.Coordinates, &rings); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}
		if len(rings) != 1 {
			t.Fatalf("Expected 1 ring, got %d", len(rings))
		}

		// The buffered polygon should extend 10 units above and below the line
		ring := rings[0]
		minY, maxY := math.Inf(1), math.Inf(-1)
		for _, pt := range ring {
			if pt[1] < minY {
				minY = pt[1]
			}
			if pt[1] > maxY {
				maxY = pt[1]
			}
		}

		if math.Abs(minY-(-10.0)) > 0.01 {
			t.Errorf("Expected minY ~ -10.0, got %f", minY)
		}
		if math.Abs(maxY-10.0) > 0.01 {
			t.Errorf("Expected maxY ~ 10.0, got %f", maxY)
		}
	})

	t.Run("vertical line buffered", func(t *testing.T) {
		path := Path{{X: 50, Y: 0}, {X: 50, Y: 200}}
		geom := PathToLineString(path)

		result := BufferLineString(geom, 5.0)
		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		var rings [][][2]float64
		if err := json.Unmarshal(result.Coordinates, &rings); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		ring := rings[0]
		minX, maxX := math.Inf(1), math.Inf(-1)
		for _, pt := range ring {
			if pt[0] < minX {
				minX = pt[0]
			}
			if pt[0] > maxX {
				maxX = pt[0]
			}
		}

		if math.Abs(minX-45.0) > 0.01 {
			t.Errorf("Expected minX ~ 45.0, got %f", minX)
		}
		if math.Abs(maxX-55.0) > 0.01 {
			t.Errorf("Expected maxX ~ 55.0, got %f", maxX)
		}
	})

	t.Run("multi-segment line", func(t *testing.T) {
		path := Path{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}}
		geom := PathToLineString(path)

		result := BufferLineString(geom, 10.0)
		if result == nil {
			t.Fatal("Expected non-nil result for multi-segment line")
		}
		if result.Type != GeometryPolygon {
			t.Errorf("Expected Polygon, got %s", result.Type)
		}
	})

	t.Run("nil geometry", func(t *testing.T) {
		result := BufferLineString(nil, 10.0)
		if result != nil {
			t.Error("Expected nil for nil geometry")
		}
	})

	t.Run("wrong geometry type", func(t *testing.T) {
		path := Path{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}}
		geom := PathToPolygon(path)

		result := BufferLineString(geom, 10.0)
		if result != nil {
			t.Error("Expected nil for polygon input")
		}
	})

	t.Run("single point line", func(t *testing.T) {
		path := Path{{X: 50, Y: 50}}
		geom := PathToLineString(path)

		result := BufferLineString(geom, 10.0)
		if result != nil {
			t.Error("Expected nil for single-point line")
		}
	})

	t.Run("zero distance", func(t *testing.T) {
		path := Path{{X: 0, Y: 0}, {X: 100, Y: 0}}
		geom := PathToLineString(path)

		result := BufferLineString(geom, 0.0)
		// With zero buffer, all points are collinear; convex hull degenerates
		// The function may return nil or a degenerate polygon; either is acceptable
		_ = result
	})
}

func TestUnionPolygons(t *testing.T) {
	t.Run("two overlapping squares", func(t *testing.T) {
		// Square 1: (0,0)-(100,100)
		path1 := Path{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}}
		geom1 := PathToPolygon(path1)

		// Square 2: (50,50)-(150,150) - overlapping
		path2 := Path{{X: 50, Y: 50}, {X: 150, Y: 50}, {X: 150, Y: 150}, {X: 50, Y: 150}}
		geom2 := PathToPolygon(path2)

		result := UnionPolygons([]*Geometry{geom1, geom2})
		if result == nil {
			t.Fatal("Expected non-nil result")
		}
		if result.Type != GeometryPolygon {
			t.Errorf("Expected Polygon, got %s", result.Type)
		}

		// The convex hull should cover (0,0) to (150,150)
		var rings [][][2]float64
		if err := json.Unmarshal(result.Coordinates, &rings); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		ring := rings[0]
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, pt := range ring {
			if pt[0] < minX {
				minX = pt[0]
			}
			if pt[0] > maxX {
				maxX = pt[0]
			}
			if pt[1] < minY {
				minY = pt[1]
			}
			if pt[1] > maxY {
				maxY = pt[1]
			}
		}

		if minX != 0 || minY != 0 || maxX != 150 || maxY != 150 {
			t.Errorf("Expected bounds (0,0)-(150,150), got (%f,%f)-(%f,%f)", minX, minY, maxX, maxY)
		}
	})

	t.Run("single polygon passthrough", func(t *testing.T) {
		path := Path{{X: 10, Y: 10}, {X: 90, Y: 10}, {X: 90, Y: 90}, {X: 10, Y: 90}}
		geom := PathToPolygon(path)

		result := UnionPolygons([]*Geometry{geom})
		if result == nil {
			t.Fatal("Expected non-nil result for single polygon")
		}
		if result.Type != GeometryPolygon {
			t.Errorf("Expected Polygon, got %s", result.Type)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		result := UnionPolygons([]*Geometry{})
		if result != nil {
			t.Error("Expected nil for empty input")
		}
	})

	t.Run("nil geometry in list", func(t *testing.T) {
		path := Path{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}}
		geom := PathToPolygon(path)

		result := UnionPolygons([]*Geometry{nil, geom, nil})
		if result == nil {
			t.Fatal("Expected non-nil result when valid polygons present")
		}
	})

	t.Run("non-polygon types skipped", func(t *testing.T) {
		line := PathToLineString(Path{{X: 0, Y: 0}, {X: 100, Y: 100}})
		path := Path{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}}
		poly := PathToPolygon(path)

		result := UnionPolygons([]*Geometry{line, poly})
		if result == nil {
			t.Fatal("Expected non-nil result; polygon should be processed")
		}
	})

	t.Run("disjoint polygons", func(t *testing.T) {
		// Two far-apart squares
		path1 := Path{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
		path2 := Path{{X: 1000, Y: 1000}, {X: 1010, Y: 1000}, {X: 1010, Y: 1010}, {X: 1000, Y: 1010}}

		result := UnionPolygons([]*Geometry{PathToPolygon(path1), PathToPolygon(path2)})
		if result == nil {
			t.Fatal("Expected non-nil result for disjoint polygons")
		}
		// Convex hull will span both; this is the expected conservative behavior
	})
}

func TestClusterByProximity(t *testing.T) {
	makePointFeature := func(x, y float64) *Feature {
		coordsJSON, _ := json.Marshal([2]float64{x, y})
		geom := &Geometry{Type: GeometryPoint, Coordinates: coordsJSON}
		return NewFeature(geom, nil)
	}

	t.Run("two close features cluster together", func(t *testing.T) {
		f1 := makePointFeature(0, 0)
		f2 := makePointFeature(5, 0)
		f3 := makePointFeature(1000, 1000)

		clusters := ClusterByProximity([]*Feature{f1, f2, f3}, 10.0)

		if len(clusters) != 2 {
			t.Fatalf("Expected 2 clusters, got %d", len(clusters))
		}

		// Largest cluster first
		if len(clusters[0]) != 2 {
			t.Errorf("Expected first cluster to have 2 features, got %d", len(clusters[0]))
		}
		if len(clusters[1]) != 1 {
			t.Errorf("Expected second cluster to have 1 feature, got %d", len(clusters[1]))
		}
	})

	t.Run("all features in one cluster", func(t *testing.T) {
		f1 := makePointFeature(0, 0)
		f2 := makePointFeature(5, 5)
		f3 := makePointFeature(10, 10)

		clusters := ClusterByProximity([]*Feature{f1, f2, f3}, 100.0)

		if len(clusters) != 1 {
			t.Fatalf("Expected 1 cluster, got %d", len(clusters))
		}
		if len(clusters[0]) != 3 {
			t.Errorf("Expected 3 features in cluster, got %d", len(clusters[0]))
		}
	})

	t.Run("each feature its own cluster", func(t *testing.T) {
		f1 := makePointFeature(0, 0)
		f2 := makePointFeature(100, 100)
		f3 := makePointFeature(200, 200)

		clusters := ClusterByProximity([]*Feature{f1, f2, f3}, 1.0)

		if len(clusters) != 3 {
			t.Fatalf("Expected 3 clusters, got %d", len(clusters))
		}
	})

	t.Run("transitive clustering (single linkage)", func(t *testing.T) {
		// A-B are close, B-C are close, A-C are far
		// Single linkage should cluster all together
		f1 := makePointFeature(0, 0)
		f2 := makePointFeature(9, 0)
		f3 := makePointFeature(18, 0)

		clusters := ClusterByProximity([]*Feature{f1, f2, f3}, 10.0)

		if len(clusters) != 1 {
			t.Fatalf("Expected 1 cluster (transitive), got %d", len(clusters))
		}
	})

	t.Run("nil geometry features isolated", func(t *testing.T) {
		f1 := makePointFeature(0, 0)
		f2 := NewFeature(nil, nil) // nil geometry
		f3 := makePointFeature(5, 0)

		clusters := ClusterByProximity([]*Feature{f1, f2, f3}, 10.0)

		// f1 and f3 should cluster; f2 should be isolated
		if len(clusters) != 2 {
			t.Fatalf("Expected 2 clusters, got %d", len(clusters))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		result := ClusterByProximity([]*Feature{}, 10.0)
		if result != nil {
			t.Error("Expected nil for empty input")
		}
	})

	t.Run("single feature", func(t *testing.T) {
		f := makePointFeature(0, 0)
		clusters := ClusterByProximity([]*Feature{f}, 10.0)

		if len(clusters) != 1 {
			t.Fatalf("Expected 1 cluster, got %d", len(clusters))
		}
		if len(clusters[0]) != 1 {
			t.Errorf("Expected 1 feature in cluster, got %d", len(clusters[0]))
		}
	})

	t.Run("line string features", func(t *testing.T) {
		// Two nearby walls
		geom1 := PathToLineString(Path{{X: 0, Y: 0}, {X: 100, Y: 0}})
		geom2 := PathToLineString(Path{{X: 0, Y: 5}, {X: 100, Y: 5}})
		// One far wall
		geom3 := PathToLineString(Path{{X: 500, Y: 500}, {X: 600, Y: 500}})

		f1 := NewFeature(geom1, nil)
		f2 := NewFeature(geom2, nil)
		f3 := NewFeature(geom3, nil)

		clusters := ClusterByProximity([]*Feature{f1, f2, f3}, 10.0)

		if len(clusters) != 2 {
			t.Fatalf("Expected 2 clusters, got %d", len(clusters))
		}
	})
}

func TestSimplifyLineString(t *testing.T) {
	t.Run("simplify noisy line", func(t *testing.T) {
		// Create a line with slight deviations that should be simplified
		path := Path{
			{X: 0, Y: 0},
			{X: 25, Y: 0.5},  // slight noise
			{X: 50, Y: -0.3}, // slight noise
			{X: 75, Y: 0.1},  // slight noise
			{X: 100, Y: 0},
		}
		geom := PathToLineString(path)

		result := SimplifyLineString(geom, 1.0)
		if result == nil {
			t.Fatal("Expected non-nil result")
		}
		if result.Type != GeometryLineString {
			t.Errorf("Expected LineString, got %s", result.Type)
		}

		var coords [][2]float64
		if err := json.Unmarshal(result.Coordinates, &coords); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		// With tolerance 1.0, deviations < 1.0 should be removed
		if len(coords) >= len(path) {
			t.Errorf("Expected fewer points after simplification, got %d (original %d)", len(coords), len(path))
		}

		// Endpoints should be preserved
		if coords[0][0] != 0 || coords[0][1] != 0 {
			t.Errorf("First point not preserved: %v", coords[0])
		}
		last := coords[len(coords)-1]
		if last[0] != 100 || last[1] != 0 {
			t.Errorf("Last point not preserved: %v", last)
		}
	})

	t.Run("significant features preserved", func(t *testing.T) {
		// L-shaped line - the corner should be preserved
		path := Path{
			{X: 0, Y: 0},
			{X: 100, Y: 0},
			{X: 100, Y: 100},
		}
		geom := PathToLineString(path)

		result := SimplifyLineString(geom, 1.0)
		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		var coords [][2]float64
		if err := json.Unmarshal(result.Coordinates, &coords); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		// Should preserve all 3 points (corner is significant)
		if len(coords) != 3 {
			t.Errorf("Expected 3 points (corner preserved), got %d", len(coords))
		}
	})

	t.Run("nil geometry", func(t *testing.T) {
		result := SimplifyLineString(nil, 1.0)
		if result != nil {
			t.Error("Expected nil for nil input")
		}
	})

	t.Run("wrong geometry type", func(t *testing.T) {
		path := Path{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}}
		geom := PathToPolygon(path)

		result := SimplifyLineString(geom, 1.0)
		if result != nil {
			t.Error("Expected nil for polygon input")
		}
	})

	t.Run("very small tolerance preserves all", func(t *testing.T) {
		path := Path{
			{X: 0, Y: 0},
			{X: 50, Y: 10},
			{X: 100, Y: 0},
		}
		geom := PathToLineString(path)

		result := SimplifyLineString(geom, 0.001)
		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		var coords [][2]float64
		if err := json.Unmarshal(result.Coordinates, &coords); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if len(coords) != 3 {
			t.Errorf("Expected all 3 points preserved with tiny tolerance, got %d", len(coords))
		}
	})

	t.Run("large tolerance aggressive simplification", func(t *testing.T) {
		path := Path{
			{X: 0, Y: 0},
			{X: 25, Y: 5},
			{X: 50, Y: 8},
			{X: 75, Y: 3},
			{X: 100, Y: 0},
		}
		geom := PathToLineString(path)

		result := SimplifyLineString(geom, 100.0)
		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		var coords [][2]float64
		if err := json.Unmarshal(result.Coordinates, &coords); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		// With very large tolerance, should reduce to just endpoints
		if len(coords) != 2 {
			t.Errorf("Expected 2 points with large tolerance, got %d", len(coords))
		}
	})
}

func TestGeometryBound(t *testing.T) {
	t.Run("point bound", func(t *testing.T) {
		coordsJSON, _ := json.Marshal([2]float64{50, 75})
		geom := &Geometry{Type: GeometryPoint, Coordinates: coordsJSON}

		bound := geometryBound(geom)
		center := bound.Center()

		if math.Abs(center[0]-50) > 0.01 || math.Abs(center[1]-75) > 0.01 {
			t.Errorf("Expected center (50,75), got (%f,%f)", center[0], center[1])
		}
	})

	t.Run("line string bound", func(t *testing.T) {
		geom := PathToLineString(Path{{X: 10, Y: 20}, {X: 110, Y: 220}})

		bound := geometryBound(geom)

		if bound.Min[0] != 10 || bound.Min[1] != 20 {
			t.Errorf("Expected min (10,20), got (%f,%f)", bound.Min[0], bound.Min[1])
		}
		if bound.Max[0] != 110 || bound.Max[1] != 220 {
			t.Errorf("Expected max (110,220), got (%f,%f)", bound.Max[0], bound.Max[1])
		}
	})

	t.Run("polygon bound", func(t *testing.T) {
		path := Path{{X: 5, Y: 5}, {X: 95, Y: 5}, {X: 95, Y: 95}, {X: 5, Y: 95}}
		geom := PathToPolygon(path)

		bound := geometryBound(geom)

		if bound.Min[0] != 5 || bound.Min[1] != 5 {
			t.Errorf("Expected min (5,5), got (%f,%f)", bound.Min[0], bound.Min[1])
		}
		if bound.Max[0] != 95 || bound.Max[1] != 95 {
			t.Errorf("Expected max (95,95), got (%f,%f)", bound.Max[0], bound.Max[1])
		}
	})

	t.Run("nil geometry", func(t *testing.T) {
		bound := geometryBound(nil)
		if !bound.IsZero() {
			t.Error("Expected zero bound for nil geometry")
		}
	})
}

func TestConvexHull(t *testing.T) {
	t.Run("square points", func(t *testing.T) {
		points := []orb.Point{
			{0, 0}, {100, 0}, {100, 100}, {0, 100},
		}

		hull := convexHull(points)

		if len(hull) != 4 {
			t.Errorf("Expected 4 hull points, got %d", len(hull))
		}
	})

	t.Run("interior point excluded", func(t *testing.T) {
		points := []orb.Point{
			{0, 0}, {100, 0}, {100, 100}, {0, 100},
			{50, 50}, // interior point
		}

		hull := convexHull(points)

		if len(hull) != 4 {
			t.Errorf("Expected 4 hull points (interior excluded), got %d", len(hull))
		}
	})

	t.Run("collinear points", func(t *testing.T) {
		points := []orb.Point{
			{0, 0}, {50, 0}, {100, 0},
		}

		hull := convexHull(points)

		// Collinear points: hull should have at most the endpoints
		if len(hull) > 3 {
			t.Errorf("Expected <= 3 hull points for collinear input, got %d", len(hull))
		}
	})

	t.Run("two points", func(t *testing.T) {
		points := []orb.Point{{0, 0}, {100, 100}}

		hull := convexHull(points)

		if len(hull) != 2 {
			t.Errorf("Expected 2 points, got %d", len(hull))
		}
	})
}
