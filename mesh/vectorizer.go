package mesh

import (
	"math"
)

// Path represents a sequential list of points
type Path []Point

// VisitKey uniquely identifies an edge visit
type VisitKey struct {
	Idx int
	Dir int
}


// VectorizeLayer converts a map layer into a set of simplified vector paths
// It uses contour tracing and RDP to simplify them
func VectorizeLayer(layer *MapLayer, pixelSize int, tolerance float64) []Path {
	if layer == nil || len(layer.Pixels) == 0 {
		return nil
	}

	// 1. Reconstruct dense grid from sparse pixels
	grid, minX, minY, width, height := pixelsToGrid(layer.Pixels, pixelSize)

	// 2. Trace contours
	contours := traceContours(grid, width, height)

	// 3. Transform back to pixel coordinates and simplify
	var result []Path
	for _, contour := range contours {
		// Convert grid coordinates to pixel coordinates (not world coordinates)
		// ICP transforms operate at pixel scale, so we must not scale here
		pixelContour := make(Path, len(contour))
		for i, p := range contour {
			pixelContour[i] = Point{
				X: p.X + float64(minX),
				Y: p.Y + float64(minY),
			}
		}

		// Simplify using Ramer-Douglas-Peucker
		// Note: tolerance is in world units, so scale it to pixel units for simplification
		pixelTolerance := tolerance / float64(pixelSize)
		simplified := SimplifyRDP(pixelContour, pixelTolerance)
		if len(simplified) >= 2 {
			result = append(result, simplified)
		}
	}

	return result
}

// pixelsToGrid converts flat pixel array to a 2D boolean grid
func pixelsToGrid(pixels []int, pixelSize int) (grid []bool, minX, minY, width, height int) {
	if len(pixels) == 0 {
		return nil, 0, 0, 0, 0
	}

	// Calculate bounds in grid coordinates
	minX, minY = math.MaxInt, math.MaxInt
	maxX, maxY := math.MinInt, math.MinInt

	points := make([]Point, 0, len(pixels)/2)
	for i := 0; i+1 < len(pixels); i += 2 {
		px := pixels[i]
		py := pixels[i+1]

		gx := px
		gy := py

		if gx < minX {
			minX = gx
		}
		if gy < minY {
			minY = gy
		}
		if gx > maxX {
			maxX = gx
		}
		if gy > maxY {
			maxY = gy
		}

		points = append(points, Point{X: float64(gx), Y: float64(gy)})
	}

	width = maxX - minX + 1
	height = maxY - minY + 1

	// Create grid with 1px padding
	pad := 1
	gridWidth := width + 2*pad
	gridHeight := height + 2*pad
	grid = make([]bool, gridWidth*gridHeight)

	for _, p := range points {
		x := int(p.X) - minX + pad
		y := int(p.Y) - minY + pad
		idx := y*gridWidth + x
		if idx >= 0 && idx < len(grid) {
			grid[idx] = true
		}
	}

	return grid, minX - pad, minY - pad, gridWidth, gridHeight
}

// traceContours implements the path tracing algorithm using Moore-Neighbor tracing
func traceContours(grid []bool, width, height int) []Path {
	var paths []Path

	seen := make(map[VisitKey]bool)

	idx := func(x, y int) int { return y*width + x }
	isSet := func(x, y int) bool {
		if x < 0 || x >= width || y < 0 || y >= height {
			return false
		}
		return grid[idx(x, y)]
	}

	// Scan for contour starting points
	// A starting point is any set pixel with at least one empty neighbor
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !isSet(x, y) {
				continue
			}

			// Check if this is an isolated pixel (no set neighbors in cardinal directions)
			hasNeighbor := isSet(x-1, y) || isSet(x+1, y) || isSet(x, y-1) || isSet(x, y+1)

			if !hasNeighbor {
				// Isolated pixel: create a minimal contour at the pixel position
				// Check if we haven't already processed this pixel
				key := VisitKey{idx(x, y), 0}
				if !seen[key] {
					// Mark all directions as seen for this pixel
					for dir := 0; dir < 4; dir++ {
						seen[VisitKey{idx(x, y), dir}] = true
					}

					// For an isolated pixel, create a degenerate path that repeats the pixel position
					// This ensures the path stays within bounds while satisfying the requirement
					// for a closed contour. SimplifyRDP will preserve this as it has < 3 points.
					px := float64(x)
					py := float64(y)
					path := Path{
						{px, py},
						{px, py},
						{px, py},
					}
					paths = append(paths, path)
				}
				continue
			}

			// Check all four neighbors for potential contour starts
			// Direction encoding: 0=N, 1=E, 2=S, 3=W
			neighbors := []struct {
				dx, dy int
				dir    int // direction we would be FACING when starting
			}{
				{-1, 0, 3}, // Left empty: face West
				{1, 0, 1},  // Right empty: face East
				{0, -1, 0}, // Top empty: face North
				{0, 1, 2},  // Bottom empty: face South
			}

			for _, n := range neighbors {
				nx, ny := x+n.dx, y+n.dy
				if !isSet(nx, ny) {
					// Found an edge: this pixel has an empty neighbor
					// Check if we've already traced this edge
					key := VisitKey{idx(x, y), n.dir}
					if !seen[key] {
						path := traceBoundary(x, y, n.dir, grid, width, height, seen)
						if len(path) > 2 {
							paths = append(paths, path)
						}
					}
				}
			}
		}
	}
	return paths
}

// traceBoundary follows the edge using Moore-Neighbor tracing with right-hand rule
// startFacing: direction we're initially FACING (0=N, 1=E, 2=S, 3=W)
func traceBoundary(startX, startY, startFacing int, grid []bool, width, height int, seen map[VisitKey]bool) Path {
	var path Path

	curX, curY := startX, startY
	facing := startFacing

	// Helper to check pixel
	isSet := func(x, y int) bool {
		if x < 0 || x >= width || y < 0 || y >= height {
			return false
		}
		return grid[y*width+x]
	}

	// Direction vectors: N, E, S, W
	dirs := []struct{ dx, dy int }{
		{0, -1}, // 0: North
		{1, 0},  // 1: East
		{0, 1},  // 2: South
		{-1, 0}, // 3: West
	}

	// Trace boundary using right-hand rule
	for {
		key := VisitKey{Idx: curY*width + curX, Dir: facing}

		if seen[key] {
			// We've returned to the start state - close the loop
			if curX == startX && curY == startY && len(path) > 0 {
				// Add closing point to complete the loop
				path = append(path, Point{X: float64(curX), Y: float64(curY)})
			}
			break
		}

		// Mark current state as seen
		seen[key] = true
		path = append(path, Point{X: float64(curX), Y: float64(curY)})

		// Right-hand rule: turn right and scan clockwise until we find a set pixel
		// Start from (facing - 1) which is one position to the right
		startScan := (facing - 1 + 4) % 4
		found := false

		for i := 0; i < 4; i++ {
			scanDir := (startScan + i) % 4
			dx := dirs[scanDir].dx
			dy := dirs[scanDir].dy
			nx, ny := curX+dx, curY+dy

			if isSet(nx, ny) {
				// Found next pixel - move there
				curX, curY = nx, ny
				// Update facing: we're now facing the direction we moved
				facing = scanDir
				found = true
				break
			}
		}

		if !found {
			// Isolated pixel or dead end
			break
		}

		// Safety break for infinite loops
		if len(path) > 100000 {
			break
		}
	}

	return path
}

// SimplifyRDP reduces points using Ramer-Douglas-Peucker algorithm
func SimplifyRDP(points Path, epsilon float64) Path {
	if len(points) < 3 {
		return points
	}

	dmax := 0.0
	index := 0
	end := len(points) - 1

	for i := 1; i < end; i++ {
		d := perpendicularDistance(points[i], points[0], points[end])
		if d > dmax {
			dmax = d
			index = i
		}
	}

	if dmax > epsilon {
		// Recursive call
		recResults1 := SimplifyRDP(points[:index+1], epsilon)
		recResults2 := SimplifyRDP(points[index:], epsilon)

		// Build the result list
		return append(recResults1[:len(recResults1)-1], recResults2...)
	} else {
		return Path{points[0], points[end]}
	}
}

func perpendicularDistance(pt, lineStart, lineEnd Point) float64 {
	dx := lineEnd.X - lineStart.X
	dy := lineEnd.Y - lineStart.Y

	// Visualize using hypot instead of manual sqrt for magnitude
	mag := math.Hypot(dx, dy)

	if mag > 0.0 {
		// Standard point-line distance
		dx /= mag
		dy /= mag

		pdx := pt.X - lineStart.X
		pdy := pt.Y - lineStart.Y

		num := math.Abs(dy*pdx - dx*pdy)
		return num
	} else {
		// Line is a point; return distance to point
		return math.Hypot(pt.X-lineStart.X, pt.Y-lineStart.Y)
	}
}
