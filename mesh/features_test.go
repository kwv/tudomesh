package mesh

import (
	"math"
	"testing"
)

// createTestMap builds a small L-shaped map with floor, wall, and charger
// entities. Pixel data uses Valetudo RLE format [x, y, count, ...] where
// each triplet represents a horizontal run of count pixels starting at (x, y).
// It calls NormalizeToMM so all layer pixel coordinates are in local-mm,
// matching the production ingest pipeline.
func createTestMap() *ValetudoMap {
	// Create an asymmetric L-shaped room (in grid indices) using RLE triplets.
	// Main room: rows y=10..19, each row x=10..19 (10 pixels) -> [10, y, 10]
	// Corridor:  rows y=10..12, each row x=20..25 (6 pixels)  -> [20, y, 6]
	pixels := []int{}
	for y := 10; y < 20; y++ {
		pixels = append(pixels, 10, y, 10) // 10 pixels starting at x=10
	}
	for y := 10; y < 13; y++ {
		pixels = append(pixels, 20, y, 6) // 6 pixels starting at x=20
	}

	// Wall points (grid indices) as RLE triplets.
	wallPixels := []int{}
	// Top wall of main room and corridor: x=9..26 at y=9 (18 pixels).
	wallPixels = append(wallPixels, 9, 9, 18)
	// Bottom wall of main room: x=9..19 at y=20 (11 pixels).
	wallPixels = append(wallPixels, 9, 20, 11)

	m := &ValetudoMap{
		PixelSize: 5,
		Size:      Size{X: 100, Y: 100},
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: pixels,
			},
			{
				Type:   "wall",
				Pixels: wallPixels,
			},
		},
		Entities: []MapEntity{
			{
				Type:   "charger_location",
				Points: []int{15 * 5, 15 * 5}, // 75mm, 75mm -- already in mm
			},
		},
	}
	NormalizeToMM(m)
	return m
}

func TestExtractFeatures(t *testing.T) {
	m := createTestMap()
	fs := ExtractFeatures(m)

	if len(fs.BoundaryPoints) == 0 {
		t.Errorf("Expected boundary points, got 0")
	}
	if len(fs.Corners) == 0 {
		t.Errorf("Expected corners, got 0")
	}
	if len(fs.WallPoints) == 0 {
		t.Errorf("Expected wall points, got 0")
	}
	if len(fs.GridPoints) == 0 {
		t.Errorf("Expected grid points, got 0")
	}
	if !fs.HasCharger {
		t.Errorf("Expected charger to be detected")
	}

	// Centroid of floor pixels (now in mm = grid_index * 5).
	// Main room: 100 pixels, center at grid (14.5, 14.5) -> mm (72.5, 72.5)
	// Corridor:  18 pixels,  center at grid (22.5, 11.0) -> mm (112.5, 55.0)
	// Weighted center X: (100*72.5 + 18*112.5) / 118 = (7250 + 2025) / 118 = 9275 / 118 ~ 78.60
	// Weighted center Y: (100*72.5 + 18*55.0)  / 118 = (7250 + 990)  / 118 = 8240 / 118 ~ 69.83
	expectedCentroidX := 78.60
	expectedCentroidY := 69.83
	if math.Abs(fs.Centroid.X-expectedCentroidX) > 0.5 || math.Abs(fs.Centroid.Y-expectedCentroidY) > 0.5 {
		t.Errorf("Expected centroid around (%.2f, %.2f), got (%.2f, %.2f)",
			expectedCentroidX, expectedCentroidY, fs.Centroid.X, fs.Centroid.Y)
	}

	// Charger should be at 75mm, 75mm.
	if math.Abs(fs.ChargerPosition.X-75) > 0.1 || math.Abs(fs.ChargerPosition.Y-75) > 0.1 {
		t.Errorf("Expected charger at (75, 75), got (%.2f, %.2f)", fs.ChargerPosition.X, fs.ChargerPosition.Y)
	}
}

func TestExtractFeaturesFromEntityPaths(t *testing.T) {
	// Map with no pixel data but entity path points (local-mm).
	m := &ValetudoMap{
		PixelSize: 5,
		Size:      Size{X: 200, Y: 200},
		Layers:    []MapLayer{},
		Entities: []MapEntity{
			{
				Type:   "path",
				Points: []int{100, 100, 200, 100, 200, 200, 100, 200}, // square path in mm
			},
			{
				Type:   "charger_location",
				Points: []int{150, 150},
			},
		},
	}
	NormalizeToMM(m) // no-op for entities, they are already mm

	fs := ExtractFeatures(m)

	if len(fs.BoundaryPoints) == 0 && len(fs.GridPoints) == 0 {
		t.Errorf("Expected features from path entities, got none")
	}
	if fs.Centroid.X == 0 && fs.Centroid.Y == 0 {
		t.Errorf("Expected non-zero centroid from path entities")
	}
	if !fs.HasCharger {
		t.Errorf("Expected charger to be detected")
	}
}

func TestExtractGridPoints(t *testing.T) {
	// 100x100 points in mm-space (0..99 mm).
	pixels := []Point{}
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			pixels = append(pixels, Point{X: float64(x), Y: float64(y)})
		}
	}

	// gridSpacing = 50mm.
	// Points 0-24 -> cell 0 -> center 0mm
	// Points 25-74 -> cell 1 -> center 50mm
	// Points 75-99 -> cell 2 -> center 100mm
	gridPts := extractGridPoints(pixels, 50)

	if len(gridPts) == 0 {
		t.Errorf("Expected grid points, got 0")
	}

	// Check if (50,50) is present (center of cell (1,1)).
	found := false
	for _, p := range gridPts {
		if p.X == 50 && p.Y == 50 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected grid point at (50,50) to be found")
	}
}

func TestExtractBoundary(t *testing.T) {
	// 3x3 square of points in mm, spaced 5mm apart (pixelSize=5).
	// Grid indices: (2,2) (3,2) (4,2)
	//               (2,3) (3,3) (4,3)
	//               (2,4) (3,4) (4,4)
	pixels := []Point{
		{10, 10}, {15, 10}, {20, 10},
		{10, 15}, {15, 15}, {20, 15},
		{10, 20}, {15, 20}, {20, 20},
	}
	pixelSize := 5

	boundary := extractBoundary(pixels, pixelSize)

	// The center point (15,15) = grid (3,3) has all 4 neighbors.
	// All other 8 points are boundary points.
	if len(boundary) != 8 {
		t.Errorf("Expected 8 boundary points, got %d", len(boundary))
	}

	// Check that (15,15) is NOT in boundary.
	for _, p := range boundary {
		if p.X == 15 && p.Y == 15 {
			t.Errorf("Point (15,15) should not be in boundary")
		}
	}
}

func TestExtractCorners(t *testing.T) {
	// A simple square boundary (coordinates in mm).
	boundary := []Point{
		{0, 0}, {5, 0}, {10, 0},
		{10, 5}, {10, 10},
		{5, 10}, {0, 10},
		{0, 5},
	}

	corners := extractCorners(boundary, 60.0)

	// Should find 4 corners: (0,0), (10,0), (10,10), (0,10).
	if len(corners) != 4 {
		t.Errorf("Expected 4 corners, got %d", len(corners))
		for i, p := range corners {
			t.Logf("Corner %d: %+v", i, p)
		}
	}
}

func TestSampleFeatures(t *testing.T) {
	fs := FeatureSet{
		HasCharger:      true,
		ChargerPosition: Point{10, 10},
		WallPoints:      make([]Point, 100),
		GridPoints:      make([]Point, 100),
		Corners:         make([]Point, 100),
		BoundaryPoints:  make([]Point, 100),
	}

	sampled := SampleFeatures(fs, 30)

	if len(sampled) > 30 {
		t.Errorf("Sampled more than maxPoints: %d", len(sampled))
	}
	if len(sampled) == 0 {
		t.Errorf("Expected sampled points, got 0")
	}
	// First point should be charger.
	if sampled[0].X != 10 || sampled[0].Y != 10 {
		t.Errorf("First sampled point should be charger")
	}
}

func TestFeatureDistance(t *testing.T) {
	set1 := []Point{{0, 0}, {10, 0}}
	set2 := []Point{{0, 0}, {10, 0}}

	dist := FeatureDistance(set1, set2)
	if dist != 0 {
		t.Errorf("Expected distance 0 for identical sets, got %v", dist)
	}

	set3 := []Point{{2, 0}, {12, 0}}
	dist2 := FeatureDistance(set1, set3)
	if dist2 != 2.0 {
		t.Errorf("Expected distance 2.0, got %v", dist2)
	}
}

func TestExtractWallAngles(t *testing.T) {
	m := createTestMap()
	hist := ExtractWallAngles(m)

	if hist.TotalEdges == 0 {
		t.Errorf("Expected wall edges to be analyzed, got 0")
	}

	dominant := hist.DominantAngles(2)
	if len(dominant) == 0 {
		t.Errorf("Expected dominant angles, got 0")
	}
}

func TestWallAngleHistogram_DominantAngles(t *testing.T) {
	var hist WallAngleHistogram
	hist.RawCounts[45] = 10
	hist.RawCounts[90] = 20
	hist.RawCounts[0] = 5

	dominant := hist.DominantAngles(2)
	if len(dominant) != 2 {
		t.Errorf("Expected 2 dominant angles, got %d", len(dominant))
	}
	if dominant[0] != 90 {
		t.Errorf("First dominant angle should be 90, got %v", dominant[0])
	}
	if dominant[1] != 45 {
		t.Errorf("Second dominant angle should be 45, got %v", dominant[1])
	}
}

func TestCompareHistograms(t *testing.T) {
	var h1, h2 WallAngleHistogram
	h1.Bins[0] = 1.0
	h1.TotalEdges = 1
	h2.Bins[0] = 1.0
	h2.TotalEdges = 1

	score := CompareHistograms(h1, h2, 0)
	if score < 0.99 {
		t.Errorf("Expected high similarity for identical histograms, got %v", score)
	}

	score90 := CompareHistograms(h1, h2, 90)
	if score90 > 0.1 {
		t.Errorf("Expected low similarity for offset histograms, got %v", score90)
	}
}

func TestDetectRotation(t *testing.T) {
	source := createTestMap()
	target := createTestMap()

	analysis := DetectRotation(source, target)
	// For symmetric wall histograms, 0 and 180 are equally likely.
	if analysis.BestRotation != 0 && analysis.BestRotation != 180 {
		t.Errorf("Expected best rotation 0 or 180, got %v", analysis.BestRotation)
	}
}

func TestDetectRotationWithFeatures(t *testing.T) {
	source := createTestMap()
	target := createTestMap()

	analysis := DetectRotationWithFeatures(source, target)
	if analysis.BestRotation != 0 {
		t.Errorf("Expected best rotation 0, got %v", analysis.BestRotation)
	}
	// With a tiny, nearly symmetric test map, confidence is low because all
	// rotation scores are very close. We only check that confidence > 0
	// (the best rotation is correct, which is the important assertion).
	if analysis.Confidence <= 0 {
		t.Errorf("Expected positive confidence for identical maps using feature matching, got %v", analysis.Confidence)
	}
}
