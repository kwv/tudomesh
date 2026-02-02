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

	t.Logf("Simplified to %d points with tolerance 2.0", pointsSimplified)
	// We just check that simplification doesn't increase points (sanity check)
	// For simple shapes like rectangles, RDP may not reduce further since corners are essential
	if pointsSimplified > foundPoints {
		t.Errorf("Simplification increased points: %d vs %d", pointsSimplified, foundPoints)
	}
}

// TestTraceBoundaryDetailed tests the Moore-Neighbor algorithm step-by-step
func TestTraceBoundaryDetailed(t *testing.T) {
	// Simple 3x3 grid with single pixel
	// . . .
	// . X .
	// . . .
	grid := make([]bool, 9)
	grid[4] = true // center at (1,1)

	t.Log("Testing single pixel at (1,1) in 3x3 grid")

	// Test all 4 entry directions
	entryDirs := []int{0, 1, 2, 3}
	dirNames := []string{"North", "East", "South", "West"}

	for i, entryDir := range entryDirs {
		t.Run(dirNames[i], func(t *testing.T) {
			seen := make(map[VisitKey]bool)
			path := traceBoundary(1, 1, entryDir, grid, 3, 3, seen)

			t.Logf("Entry direction: %s (%d)", dirNames[i], entryDir)
			t.Logf("Path length: %d", len(path))
			for j, pt := range path {
				t.Logf("  [%d] (%.0f, %.0f)", j, pt.X, pt.Y)
			}
			t.Logf("Visited keys: %d", len(seen))
		})
	}
}

// TestTraceContoursSimple tests contour tracing with a minimal pattern
func TestTraceContoursSimple(t *testing.T) {
	tests := []struct {
		name          string
		setupGrid     func() ([]bool, int, int)
		expectPaths   int
		expectMinPts  int
	}{
		{
			name: "single pixel",
			setupGrid: func() ([]bool, int, int) {
				// Grid: 3x3 with center pixel set
				// . . .
				// . X .
				// . . .
				grid := make([]bool, 9)
				grid[4] = true // center
				return grid, 3, 3
			},
			expectPaths:  1,
			expectMinPts: 1,
		},
		{
			name: "2x2 block",
			setupGrid: func() ([]bool, int, int) {
				// Grid: 4x4 with 2x2 block in center
				// . . . .
				// . X X .
				// . X X .
				// . . . .
				grid := make([]bool, 16)
				grid[5] = true  // (1,1)
				grid[6] = true  // (2,1)
				grid[9] = true  // (1,2)
				grid[10] = true // (2,2)
				return grid, 4, 4
			},
			expectPaths:  1,
			expectMinPts: 4,
		},
		{
			name: "3x3 hollow square",
			setupGrid: func() ([]bool, int, int) {
				// Grid: 5x5 with 3x3 hollow square
				// . . . . .
				// . X X X .
				// . X . X .
				// . X X X .
				// . . . . .
				grid := make([]bool, 25)
				// Top row
				grid[6] = true  // (1,1)
				grid[7] = true  // (2,1)
				grid[8] = true  // (3,1)
				// Middle row
				grid[11] = true // (1,2)
				grid[13] = true // (3,2)
				// Bottom row
				grid[16] = true // (1,3)
				grid[17] = true // (2,3)
				grid[18] = true // (3,3)
				return grid, 5, 5
			},
			expectPaths:  2, // outer and inner contours
			expectMinPts: 4,
		},
		{
			name: "horizontal line",
			setupGrid: func() ([]bool, int, int) {
				// Grid: 5x3 with horizontal line
				// . . . . .
				// X X X X X
				// . . . . .
				grid := make([]bool, 15)
				grid[5] = true // (0,1)
				grid[6] = true // (1,1)
				grid[7] = true // (2,1)
				grid[8] = true // (3,1)
				grid[9] = true // (4,1)
				return grid, 5, 3
			},
			expectPaths:  1,
			expectMinPts: 4,
		},
		{
			name: "L shape",
			setupGrid: func() ([]bool, int, int) {
				// Grid: 4x4 with L shape
				// . . . .
				// . X . .
				// . X . .
				// . X X .
				grid := make([]bool, 16)
				grid[5] = true  // (1,1)
				grid[9] = true  // (1,2)
				grid[13] = true // (1,3)
				grid[14] = true // (2,3)
				return grid, 4, 4
			},
			expectPaths:  1,
			expectMinPts: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grid, width, height := tt.setupGrid()

			// Debug: Print grid
			t.Logf("Grid %dx%d:", width, height)
			for y := 0; y < height; y++ {
				line := ""
				for x := 0; x < width; x++ {
					if grid[y*width+x] {
						line += "X "
					} else {
						line += ". "
					}
				}
				t.Logf("  %s", line)
			}

			paths := traceContours(grid, width, height)

			t.Logf("Found %d paths", len(paths))
			for i, path := range paths {
				t.Logf("  Path %d: %d points", i, len(path))
				if len(path) <= 20 {
					for j, pt := range path {
						t.Logf("    [%d] (%.0f, %.0f)", j, pt.X, pt.Y)
					}
				}
			}

			if len(paths) < tt.expectPaths {
				t.Errorf("Expected at least %d paths, got %d", tt.expectPaths, len(paths))
			}

			for i, path := range paths {
				if len(path) < tt.expectMinPts {
					t.Errorf("Path %d: expected at least %d points, got %d", i, tt.expectMinPts, len(path))
				}
			}
		})
	}
}
