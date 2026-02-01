package mesh

import (
	"testing"
)

func TestSimplifyRDP(t *testing.T) {
	// A straight line with minimal noise
	points := Path{
		{0, 0},
		{1, 0.1},
		{2, -0.1},
		{3, 0},
	}

	result := SimplifyRDP(points, 0.5)

	if len(result) != 2 {
		t.Errorf("Expected 2 points, got %d", len(result))
	}
	if result[0].X != 0 || result[1].X != 3 {
		t.Errorf("Endpoints moved")
	}
}

func TestVectorizeLayer(t *testing.T) {
	// Create a simple map layer: 10x10 hollow square
	pixels := []int{}

	// Top and bottom walls
	for x := 0; x < 10; x++ {
		pixels = append(pixels, x, 0)
		pixels = append(pixels, x, 9)
	}
	// Left and right walls
	for y := 1; y < 9; y++ {
		pixels = append(pixels, 0, y)
		pixels = append(pixels, 9, y)
	}

	layer := &MapLayer{
		Type:   "wall",
		Pixels: pixels,
	}

	// PixelSize = 1, Tolerance = 0 (lossless)
	paths := VectorizeLayer(layer, 1, 0.0)

	if len(paths) == 0 {
		t.Fatalf("No paths found")
	}

	// Should find at least one closed loop (likely 2 if we consider inner/outer,
	// but Moore tracer on thin lines is tricky.
	// My implementation treats pixels as solid blocks.
	// A 1px wide line usually generates a loop going around it.

	foundPoints := 0
	for _, p := range paths {
		foundPoints += len(p)
	}
	t.Logf("Found %d paths with total %d points", len(paths), foundPoints)

	// RDP with higher tolerance should reduce points
	pathsSimplified := VectorizeLayer(layer, 1, 2.0)
	pointsSimplified := 0
	for _, p := range pathsSimplified {
		pointsSimplified += len(p)
	}

	if pointsSimplified >= foundPoints {
		t.Errorf("Simplification failed to reduce points: %d vs %d", pointsSimplified, foundPoints)
	}
}
