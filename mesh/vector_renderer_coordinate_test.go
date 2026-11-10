package mesh

import (
	"math"
	"testing"
)

// TestVectorRendererCoordinateScale verifies that alignment transforms are applied
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
	// This simulates an alignment result in grid units
	transform := Translation(5, 5)

	maps := map[string]*ValetudoMap{
		"map1": map1,
		"map2": map2,
	}

	transforms := map[string]AffineMatrix{
		"map1": Identity(),
		"map2": transform,
	}

	// Bounds must apply the transform in grid units, then scale by pixel size.
	f := newSVGFrame(0, 0)
	for id, m := range maps {
		f.growMap(m, transforms[id])
	}
	minX, minY, maxX, maxY := f.minX, f.minY, f.maxX, f.maxY

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
