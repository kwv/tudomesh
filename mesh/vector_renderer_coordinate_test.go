package mesh

import (
	"math"
	"testing"
)

// TestVectorRendererCoordinateScale verifies that calculateWorldBounds
// produces correct world-mm bounds when layer pixels are already in mm
// (after NormalizeToMM) and the ICP transform is mm-to-mm.
func TestVectorRendererCoordinateScale(t *testing.T) {
	// After NormalizeToMM, pixel coordinates are already in mm.
	// Map1: pixels at (50,100), (55,100), (50,105), (55,105) in mm
	// Map2: pixels at (75,125), (80,125), (75,130), (80,130) in mm
	// Transform: translation by (25, 25) mm (ICP operates in mm)
	//
	// map1 pixels (50,100) -> identity -> (50,100)
	// map1 pixels (55,105) -> identity -> (55,105)
	// map2 pixels (75,125) -> +25,+25 -> (100,150)
	// map2 pixels (80,130) -> +25,+25 -> (105,155)

	map1 := &ValetudoMap{
		PixelSize:  5,
		Normalized: true,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{50, 100, 55, 100, 50, 105, 55, 105}, // already mm
			},
		},
	}

	map2 := &ValetudoMap{
		PixelSize:  5,
		Normalized: true,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{75, 125, 80, 125, 75, 130, 80, 130}, // already mm
			},
		},
	}

	// Translation of (25, 25) mm — ICP now operates in mm space
	transform := Translation(25, 25)

	maps := map[string]*ValetudoMap{
		"map1": map1,
		"map2": map2,
	}

	transforms := map[string]AffineMatrix{
		"map1": Identity(),
		"map2": transform,
	}

	renderer := NewVectorRenderer(maps, transforms, "map1")

	minX, minY, maxX, maxY, _, _ := renderer.calculateWorldBounds()

	expectedMinX := 50.0  // map1 at (50, 100)
	expectedMinY := 100.0 // map1 at (50, 100)
	expectedMaxX := 105.0 // map2 at (80+25, 130+25)
	expectedMaxY := 155.0

	tolerance := 0.01

	if math.Abs(minX-expectedMinX) > tolerance {
		t.Errorf("minX mismatch: got %.2f, expected %.2f", minX, expectedMinX)
	}
	if math.Abs(minY-expectedMinY) > tolerance {
		t.Errorf("minY mismatch: got %.2f, expected %.2f", minY, expectedMinY)
	}
	if math.Abs(maxX-expectedMaxX) > tolerance {
		t.Errorf("maxX mismatch: got %.2f, expected %.2f", maxX, expectedMaxX)
	}
	if math.Abs(maxY-expectedMaxY) > tolerance {
		t.Errorf("maxY mismatch: got %.2f, expected %.2f", maxY, expectedMaxY)
	}
}

// TestVectorizerReturnsLocalMMCoordinates verifies that VectorizeLayer
// returns coordinates in the same space as the input pixels (local-mm
// after NormalizeToMM).
func TestVectorizerReturnsLocalMMCoordinates(t *testing.T) {
	// After NormalizeToMM with pixelSize=5, grid index 10 becomes 50mm.
	// Create a 3x3 block at mm positions (50,100) to (60,110).
	pixels := []int{}
	for y := 100; y <= 110; y += 5 {
		for x := 50; x <= 60; x += 5 {
			pixels = append(pixels, x, y)
		}
	}

	layer := &MapLayer{
		Type:   "floor",
		Pixels: pixels,
	}

	pixelSize := 5
	tolerance := 0.0

	m := &ValetudoMap{
		PixelSize:  pixelSize,
		Normalized: true,
		Layers:     []MapLayer{*layer},
	}
	paths := VectorizeLayer(m, layer, tolerance)

	if len(paths) == 0 {
		t.Fatal("Expected at least one path")
	}

	// VectorizeLayer should return coordinates in the same mm space as the input.
	// Points should be near 50-60 (X) and 100-110 (Y).
	for _, path := range paths {
		for _, pt := range path {
			if pt.X < 45 || pt.X > 65 {
				t.Errorf("Point X coordinate %.2f is not in expected mm range [45,65]", pt.X)
			}
			if pt.Y < 95 || pt.Y > 115 {
				t.Errorf("Point Y coordinate %.2f is not in expected mm range [95,115]", pt.Y)
			}
		}
	}
}
