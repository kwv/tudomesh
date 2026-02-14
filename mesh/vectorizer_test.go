package mesh

import (
	"math"
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

	m := &ValetudoMap{
		PixelSize: 1,
		Layers: []MapLayer{
			{
				Type:   "wall",
				Pixels: pixels,
			},
		},
	}
	layer := &m.Layers[0]

	// PixelSize = 1, Tolerance = 0 (lossless)
	paths := VectorizeLayer(m, layer, 0.0)

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

	// RDP with higher tolerance should reduce points or keep them the same
	// (for very simple shapes like squares, simplification may not reduce further)
	pathsSimplified := VectorizeLayer(m, layer, 2.0)
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

func TestVectorizeLayer_EntityFallback(t *testing.T) {
	// MQTT format: no pixel data, but path entities are present.
	m := &ValetudoMap{
		PixelSize: 5,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: nil, // empty -- MQTT style
			},
			{
				Type:   "wall",
				Pixels: nil,
			},
		},
		Entities: []MapEntity{
			{
				Type:   "path",
				Points: []int{100, 200, 150, 250, 200, 300, 250, 350},
			},
		},
	}

	// Floor layer should fall back to entity paths.
	floorPaths := VectorizeLayer(m, &m.Layers[0], 0.0)
	if len(floorPaths) == 0 {
		t.Fatal("Expected entity-based paths for floor layer with no pixels")
	}
	// Verify points are in mm (entity points are already mm).
	for _, pt := range floorPaths[0] {
		if pt.X < 100 || pt.X > 250 || pt.Y < 200 || pt.Y > 350 {
			t.Errorf("Point (%v,%v) outside expected entity range", pt.X, pt.Y)
		}
	}

	// Wall layer should return nil (no wall data in MQTT format).
	wallPaths := VectorizeLayer(m, &m.Layers[1], 0.0)
	if wallPaths != nil {
		t.Errorf("Expected nil for wall layer with no pixels, got %d paths", len(wallPaths))
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
		name         string
		setupGrid    func() ([]bool, int, int)
		expectPaths  int
		expectMinPts int
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
				grid[6] = true // (1,1)
				grid[7] = true // (2,1)
				grid[8] = true // (3,1)
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

// TestTraceWallCenterlines verifies that centerline extraction produces
// single paths through wall pixel centers, unlike boundary tracing which
// produces parallel outline paths.
func TestTraceWallCenterlines(t *testing.T) {
	tests := []struct {
		name        string
		setupGrid   func() ([]bool, int, int)
		expectPaths int    // expected number of chains
		expectLen   int    // expected length of the first chain
		desc        string // test description
	}{
		{
			name: "horizontal line produces single chain",
			setupGrid: func() ([]bool, int, int) {
				// . . . . . . .
				// . X X X X X .
				// . . . . . . .
				grid := make([]bool, 21)
				for x := 1; x <= 5; x++ {
					grid[1*7+x] = true
				}
				return grid, 7, 3
			},
			expectPaths: 1,
			expectLen:   5,
			desc:        "5 pixels in a row should yield 1 path with 5 points",
		},
		{
			name: "vertical line produces single chain",
			setupGrid: func() ([]bool, int, int) {
				// . . .
				// . X .
				// . X .
				// . X .
				// . X .
				// . . .
				grid := make([]bool, 18)
				for y := 1; y <= 4; y++ {
					grid[y*3+1] = true
				}
				return grid, 3, 6
			},
			expectPaths: 1,
			expectLen:   4,
			desc:        "4 pixels in a column should yield 1 path with 4 points",
		},
		{
			name: "L shape produces single chain",
			setupGrid: func() ([]bool, int, int) {
				// . . . . .
				// . X . . .
				// . X . . .
				// . X X X .
				// . . . . .
				grid := make([]bool, 25)
				grid[1*5+1] = true // (1,1)
				grid[2*5+1] = true // (1,2)
				grid[3*5+1] = true // (1,3)
				grid[3*5+2] = true // (2,3)
				grid[3*5+3] = true // (3,3)
				return grid, 5, 5
			},
			expectPaths: 1,
			expectLen:   5,
			desc:        "L-shape (5 pixels) should yield 1 chain with 5 points",
		},
		{
			name: "two disconnected segments",
			setupGrid: func() ([]bool, int, int) {
				// . . . . . . .
				// . X X . X X .
				// . . . . . . .
				grid := make([]bool, 21)
				grid[1*7+1] = true // (1,1)
				grid[1*7+2] = true // (2,1)
				grid[1*7+4] = true // (4,1)
				grid[1*7+5] = true // (5,1)
				return grid, 7, 3
			},
			expectPaths: 2,
			expectLen:   2,
			desc:        "Two separate wall segments should yield 2 paths",
		},
		{
			name: "single pixel is skipped",
			setupGrid: func() ([]bool, int, int) {
				// . . .
				// . X .
				// . . .
				grid := make([]bool, 9)
				grid[4] = true
				return grid, 3, 3
			},
			expectPaths: 0,
			expectLen:   0,
			desc:        "Isolated pixel should not produce a path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grid, width, height := tt.setupGrid()

			paths := traceWallCenterlines(grid, width, height)

			t.Logf("%s", tt.desc)
			t.Logf("Got %d paths", len(paths))
			for i, p := range paths {
				t.Logf("  Path %d: %d points", i, len(p))
				for j, pt := range p {
					t.Logf("    [%d] (%.0f, %.0f)", j, pt.X, pt.Y)
				}
			}

			if len(paths) != tt.expectPaths {
				t.Errorf("Expected %d paths, got %d", tt.expectPaths, len(paths))
			}

			if tt.expectLen > 0 && len(paths) > 0 && len(paths[0]) != tt.expectLen {
				t.Errorf("Expected first path to have %d points, got %d", tt.expectLen, len(paths[0]))
			}
		})
	}
}

// TestVectorizeWallCenterlines_VsBoundary demonstrates that centerline
// extraction produces fewer paths than boundary tracing for the same
// wall pixel data.
func TestVectorizeWallCenterlines_VsBoundary(t *testing.T) {
	// Build a horizontal wall: 20 pixels at y=5.
	var pixels []int
	for x := 0; x < 20; x++ {
		pixels = append(pixels, x, 5)
	}

	m := &ValetudoMap{
		PixelSize: 1,
		Layers: []MapLayer{
			{Type: "wall", Pixels: pixels},
		},
	}
	layer := &m.Layers[0]

	// Boundary tracing (old method): produces parallel outlines.
	boundaryPaths := VectorizeLayer(m, layer, 0.0)

	// Centerline extraction (new method): produces single line.
	centerPaths := VectorizeWallCenterlines(m, layer, 0.0)

	t.Logf("Boundary tracing: %d paths", len(boundaryPaths))
	for i, p := range boundaryPaths {
		t.Logf("  Path %d: %d points", i, len(p))
	}
	t.Logf("Centerline extraction: %d paths", len(centerPaths))
	for i, p := range centerPaths {
		t.Logf("  Path %d: %d points", i, len(p))
	}

	// Centerline should produce exactly 1 path for a simple wall.
	if len(centerPaths) != 1 {
		t.Errorf("Expected 1 centerline path, got %d", len(centerPaths))
	}

	// Centerline path count should be less than or equal to boundary path count.
	if len(centerPaths) > len(boundaryPaths) {
		t.Errorf("Centerline produced more paths (%d) than boundary (%d)",
			len(centerPaths), len(boundaryPaths))
	}
}

// TestVectorizeWallCenterlines_NilInputs verifies safe handling of nil/empty inputs.
func TestVectorizeWallCenterlines_NilInputs(t *testing.T) {
	// Nil map.
	paths := VectorizeWallCenterlines(nil, &MapLayer{Type: "wall"}, 0.0)
	if paths != nil {
		t.Errorf("Expected nil for nil map, got %d paths", len(paths))
	}

	// Nil layer.
	m := &ValetudoMap{PixelSize: 1}
	paths = VectorizeWallCenterlines(m, nil, 0.0)
	if paths != nil {
		t.Errorf("Expected nil for nil layer, got %d paths", len(paths))
	}

	// Empty pixels.
	paths = VectorizeWallCenterlines(m, &MapLayer{Type: "wall", Pixels: nil}, 0.0)
	if paths != nil {
		t.Errorf("Expected nil for empty pixels, got %d paths", len(paths))
	}
}

// TestVectorizeWallCenterlines_WithPixelSize verifies mm coordinate conversion.
func TestVectorizeWallCenterlines_WithPixelSize(t *testing.T) {
	// Wall pixels at grid positions (0,0), (1,0), (2,0) with pixelSize=50mm.
	// After NormalizeToMM, pixels are in mm: (0,0), (50,0), (100,0).
	// VectorizeWallCenterlines converts back to grid, traces, and converts to mm.
	m := &ValetudoMap{
		PixelSize: 50,
		Layers: []MapLayer{
			{
				Type:   "wall",
				Pixels: []int{0, 0, 50, 0, 100, 0},
			},
		},
	}
	layer := &m.Layers[0]

	paths := VectorizeWallCenterlines(m, layer, 0.0)
	if len(paths) != 1 {
		t.Fatalf("Expected 1 path, got %d", len(paths))
	}

	// All points should be multiples of pixelSize.
	for _, pt := range paths[0] {
		if math.Mod(pt.X, 50) != 0 || math.Mod(pt.Y, 50) != 0 {
			t.Errorf("Point (%.0f, %.0f) not aligned to pixelSize 50", pt.X, pt.Y)
		}
	}

	t.Logf("Path with pixelSize=50: %d points", len(paths[0]))
	for i, pt := range paths[0] {
		t.Logf("  [%d] (%.0f, %.0f)", i, pt.X, pt.Y)
	}
}
