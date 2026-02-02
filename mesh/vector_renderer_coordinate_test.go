package mesh

import (
	"math"
	"testing"
)

// TestVectorRendererCoordinateScale verifies that ICP transforms are applied
// at the correct scale (pixel scale before world scale conversion)
func TestVectorRendererCoordinateScale(t *testing.T) {
	// Create two simple maps with a known transformation
	// Map1: single pixel at (10, 20) with pixelSize=5mm
	// Map2: single pixel at (15, 25) with pixelSize=5mm
	// Expected transform: translation by (5, 5) pixels = (25, 25) mm in world coords

	map1 := &ValetudoMap{
		PixelSize: 5,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{10, 20, 11, 20, 10, 21, 11, 21}, // 2x2 block
			},
		},
	}

	map2 := &ValetudoMap{
		PixelSize: 5,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{15, 25, 16, 25, 15, 26, 16, 26}, // 2x2 block
			},
		},
	}

	// Create a transform that translates by (5, 5) pixels
	// This simulates an ICP result at pixel scale
	transform := Translation(5, 5)

	maps := map[string]*ValetudoMap{
		"map1": map1,
		"map2": map2,
	}

	transforms := map[string]AffineMatrix{
		"map1": Identity(),
		"map2": transform,
	}

	renderer := NewVectorRenderer(maps, transforms, "map1")

	// Calculate world bounds - this should apply transform at pixel scale first
	minX, minY, maxX, maxY, _, _ := renderer.calculateWorldBounds()

	// After transform:
	// map1 pixels (10,20) -> (10,20) -> world (50,100)
	// map1 pixels (11,21) -> (11,21) -> world (55,105)
	// map2 pixels (15,25) -> transform -> (20,30) -> world (100,150)
	// map2 pixels (16,26) -> transform -> (21,31) -> world (105,155)

	expectedMinX := 50.0  // map1 at (10*5, 20*5)
	expectedMinY := 100.0 // map1 at (10*5, 20*5)
	expectedMaxX := 105.0 // map2 at (21*5, 31*5)
	expectedMaxY := 155.0 // map2 at (21*5, 31*5)

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

// TestVectorizerReturnsPixelCoordinates verifies that VectorizeLayer
// returns pixel coordinates (not world coordinates)
func TestVectorizerReturnsPixelCoordinates(t *testing.T) {
	// Create a simple 3x3 block at pixel position (10, 20)
	pixels := []int{}
	for y := 20; y < 23; y++ {
		for x := 10; x < 13; x++ {
			pixels = append(pixels, x, y)
		}
	}

	layer := &MapLayer{
		Type:   "floor",
		Pixels: pixels,
	}

	pixelSize := 5 // 5mm per pixel
	tolerance := 0.0

	paths := VectorizeLayer(layer, pixelSize, tolerance)

	if len(paths) == 0 {
		t.Fatal("Expected at least one path")
	}

	// VectorizeLayer should return pixel coordinates (around 10-13, 20-23)
	// NOT world coordinates (around 50-65, 100-115)
	for _, path := range paths {
		for _, pt := range path {
			// Check that coordinates are in pixel range, not world range
			if pt.X < 8 || pt.X > 15 {
				t.Errorf("Point X coordinate %.2f is not in pixel range [8,15]", pt.X)
			}
			if pt.Y < 18 || pt.Y > 25 {
				t.Errorf("Point Y coordinate %.2f is not in pixel range [18,25]", pt.Y)
			}

			// Make sure it's NOT in world coordinate range
			if pt.X > 40 || pt.Y > 90 {
				t.Errorf("Point (%.2f, %.2f) appears to be in world coordinates, not pixel coordinates", pt.X, pt.Y)
			}
		}
	}
}
