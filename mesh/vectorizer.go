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

	// 3. Transform back to world coordinates and simplify
	var result []Path
	for _, contour := range contours {
		// Convert grid coordinates to world coordinates
		worldContour := make(Path, len(contour))
		for i, p := range contour {
			worldContour[i] = Point{
				X: (p.X + float64(minX)) * float64(pixelSize),
				Y: (p.Y + float64(minY)) * float64(pixelSize),
			}
		}

		// Simplify using Ramer-Douglas-Peucker
		simplified := SimplifyRDP(worldContour, tolerance)
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

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !isSet(x, y) {
				continue
			}

			// Check left neighbor (Outer boundary start candidate)
			if !isSet(x-1, y) && !seen[VisitKey{idx(x, y), 3}] { // 3 = entered from left (West)
				path := traceBoundary(x, y, 3, grid, width, height, seen)
				if len(path) > 2 {
					paths = append(paths, path)
				}
			}

			// Check right neighbor (Hole boundary start candidate)
			if !isSet(x+1, y) && !seen[VisitKey{idx(x, y), 1}] { // 1 = entered from right (East)
				path := traceBoundary(x, y, 1, grid, width, height, seen)
				if len(path) > 2 {
					paths = append(paths, path)
				}
			}
		}
	}
	return paths
}

// traceBoundary follows the edge using Moore-Neighbor tracing
// entryDir: 0=N, 1=E, 2=S, 3=W (direction we came FROM)
func traceBoundary(startX, startY, startEntryDir int, grid []bool, width, height int, seen map[VisitKey]bool) Path {
	var path Path

	curX, curY := startX, startY
	entryDir := startEntryDir

	// Helper to check pixel
	isSet := func(x, y int) bool {
		if x < 0 || x >= width || y < 0 || y >= height {
			return false
		}
		return grid[y*width+x]
	}

	// Robust stopping: break if we hit a state we've already visited
	for {
		key := VisitKey{Idx: curY*width + curX, Dir: entryDir}

		if seen[key] {
			// We returned to start or merged
			if len(path) > 0 && curX == startX && curY == startY {
				path = append(path, Point{X: float64(curX), Y: float64(curY)})
			}
			break
		}

		// Mark as seen
		seen[key] = true
		path = append(path, Point{X: float64(curX), Y: float64(curY)})

		// Moore neighbor tracing:
		dirs := []Point{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

		// Left Hand Rule
		facing := (entryDir + 2) % 4
		nextDir := (facing + 3) % 4
		found := false

		for i := 0; i < 4; i++ {
			scanDir := (nextDir + i) % 4
			dx, dy := int(dirs[scanDir].X), int(dirs[scanDir].Y)
			nx, ny := curX+dx, curY+dy

			if isSet(nx, ny) {
				curX, curY = nx, ny
				entryDir = (scanDir + 2) % 4
				found = true
				break
			}
		}

		if !found {
			// Single pixel island
			break
		}

		// Safety break
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
