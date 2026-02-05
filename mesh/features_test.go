package mesh

import (
	"math"
	"testing"
)

func createTestMap() *ValetudoMap {
	// Create an asymmetric L-shaped room (in pixels)
	// Main room: (10,10) to (19,19)
	// Corridor: (20,10) to (25,12)
	pixels := []int{}
	for y := 10; y < 20; y++ {
		for x := 10; x < 20; x++ {
			pixels = append(pixels, x, y)
		}
	}
	for y := 10; y < 13; y++ {
		for x := 20; x < 26; x++ {
			pixels = append(pixels, x, y)
		}
	}

	// Wall points: simplified for testing
	wallPixels := []int{}
	// Top wall of main room and corridor
	for x := 9; x <= 26; x++ {
		wallPixels = append(wallPixels, x, 9)
	}
	// Bottom wall of main room
	for x := 9; x <= 19; x++ {
		wallPixels = append(wallPixels, x, 20)
	}

	return &ValetudoMap{
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
				Points: []int{15 * 5, 15 * 5}, // Center of the room in world coords
			},
		},
	}
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

	// Centroid of floor pixels (indices)
	// Main room: 10x10 = 100 pixels, center (14.5, 14.5)
	// Corridor: 6x3 = 18 pixels, center (22.5, 11)
	// Weighted center X: (100*14.5 + 18*22.5) / 118 = (1450 + 405) / 118 = 1855 / 118 ≈ 15.72
	// Weighted center Y: (100*14.5 + 18*11) / 118 = (1450 + 198) / 118 = 1648 / 118 ≈ 13.97
	expectedCentroidX := 15.72
	expectedCentroidY := 13.97
	if math.Abs(fs.Centroid.X-expectedCentroidX) > 0.1 || math.Abs(fs.Centroid.Y-expectedCentroidY) > 0.1 {
		t.Errorf("Expected centroid around (%.2f, %.2f), got %+v", expectedCentroidX, expectedCentroidY, fs.Centroid)
	}
}

func TestExtractGridPoints(t *testing.T) {
	pixels := []Point{}
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			pixels = append(pixels, Point{X: float64(x), Y: float64(y)})
		}
	}

	// Sampling with gridSpacing = 50
	// Should pick (0,0), (0,50), (0,100)... but wait, math.Round(p.X / 50)
	// Pixels 0-24 -> 0
	// Pixels 25-74 -> 50
	// Pixels 75-100 -> 100
	gridPts := extractGridPoints(pixels, 1, 50)

	// In 100x100, we expect grid points at (0,0), (0,50), (0,100), (50,0), (50,50), (50,100), (100,0), (100,50), (100,100)
	// Actually depends on which pixels are present.
	if len(gridPts) == 0 {
		t.Errorf("Expected grid points, got 0")
	}

	// Check if (50,50) is present
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
	// 3x3 square of pixels
	pixels := []Point{
		{10, 10}, {15, 10}, {20, 10},
		{10, 15}, {15, 15}, {20, 15},
		{10, 20}, {15, 20}, {20, 20},
	}
	pixelSize := 5

	boundary := extractBoundary(pixels, pixelSize)

	// In a 3x3 square, the middle point (15,15) has all 4 neighbors.
	// All other 8 points are boundary points.
	if len(boundary) != 8 {
		t.Errorf("Expected 8 boundary points, got %d", len(boundary))
	}

	// Check that (15,15) is NOT in boundary
	for _, p := range boundary {
		if p.X == 15 && p.Y == 15 {
			t.Errorf("Point (15,15) should not be in boundary")
		}
	}
}

func TestExtractCorners(t *testing.T) {
	// A simple square boundary
	boundary := []Point{
		{0, 0}, {5, 0}, {10, 0},
		{10, 5}, {10, 10},
		{5, 10}, {0, 10},
		{0, 5},
	}

	corners := extractCorners(boundary, 60.0)

	// Should find 4 corners: (0,0), (10,0), (10,10), (0,10)
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
	// First point should be charger
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
	// For symmetric wall histograms, 0 and 180 are equally likely
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
	if analysis.Confidence < 0.1 {
		t.Errorf("Expected some confidence for identical maps using feature matching, got %v", analysis.Confidence)
	}
}
