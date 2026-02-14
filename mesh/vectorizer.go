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

// VectorizeLayer converts a map layer into a set of simplified vector paths.
// It supports two Valetudo map formats:
//
//   - HTTP format (pixel data populated): After NormalizeToMM, layer.Pixels
//     contain mm values. The function converts them back to grid indices for
//     contour tracing, then scales the traced paths back to mm.
//
//   - MQTT format (pixels empty, entities populated): For floor/segment layers,
//     path entity points (already in mm) are extracted and simplified. Wall
//     layers with no pixels return nil since MQTT maps lack wall pixel data.
//
// All returned paths are in local-mm coordinates.
func VectorizeLayer(m *ValetudoMap, layer *MapLayer, tolerance float64) []Path {
	if m == nil || layer == nil {
		return nil
	}

	if len(layer.Pixels) > 0 {
		return vectorizeFromPixels(layer, m.PixelSize, tolerance)
	}

	// MQTT format: no pixel data. Use entity paths for floor/segment layers.
	// Wall layers have no entity data in MQTT format.
	if layer.Type == "floor" || layer.Type == "segment" {
		return vectorizeFromEntities(m, tolerance)
	}

	return nil
}

// vectorizeFromPixels handles the HTTP format where layer.Pixels are populated.
// After NormalizeToMM, pixels are in mm. We convert back to grid indices for
// contour tracing, then scale the output paths back to mm.
func vectorizeFromPixels(layer *MapLayer, pixelSize int, tolerance float64) []Path {
	if pixelSize <= 0 {
		return nil
	}

	// Convert mm values back to grid indices for contour tracing.
	gridPixels := make([]int, len(layer.Pixels))
	for i, v := range layer.Pixels {
		gridPixels[i] = v / pixelSize
	}

	// 1. Reconstruct dense grid from sparse grid-index pixels
	grid, minX, minY, width, height := pixelsToGrid(gridPixels, pixelSize)

	// 2. Trace contours
	contours := traceContours(grid, width, height)

	// 3. Transform back to mm coordinates and simplify
	ps := float64(pixelSize)
	var result []Path
	for _, contour := range contours {
		mmContour := make(Path, len(contour))
		for i, p := range contour {
			mmContour[i] = Point{
				X: (p.X + float64(minX)) * ps,
				Y: (p.Y + float64(minY)) * ps,
			}
		}

		// Tolerance is in mm, contour is now in mm -- use directly.
		simplified := SimplifyRDP(mmContour, tolerance)
		if len(simplified) >= 2 {
			result = append(result, simplified)
		}
	}

	return result
}

// vectorizeFromEntities builds paths from entity path points (MQTT format).
// Entity points are already in local-mm. The function collects all "path"
// entities, groups them into a single path, and applies RDP simplification.
func vectorizeFromEntities(m *ValetudoMap, tolerance float64) []Path {
	var allPts Path
	for _, entity := range m.Entities {
		if entity.Type == "path" && len(entity.Points) >= 2 {
			for i := 0; i+1 < len(entity.Points); i += 2 {
				allPts = append(allPts, Point{
					X: float64(entity.Points[i]),
					Y: float64(entity.Points[i+1]),
				})
			}
		}
	}

	if len(allPts) < 2 {
		return nil
	}

	if tolerance > 0 {
		allPts = SimplifyRDP(allPts, tolerance)
	}

	if len(allPts) >= 2 {
		return []Path{allPts}
	}
	return nil
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

// VectorizeWallCenterlines converts wall layer pixels into centerline paths.
// Unlike VectorizeLayer which traces boundary outlines (producing parallel
// paths for thin walls), this function walks through pixel centers to produce
// a single path per connected wall segment. This yields solid stroked lines
// instead of hatched/railroad-track artifacts.
//
// The algorithm:
//  1. Build a boolean grid from wall pixel coordinates
//  2. Find connected components using flood-fill (8-connected)
//  3. For each component, find an endpoint (degree <= 1) to start from
//  4. Walk the chain of pixels using DFS, producing a single ordered path
//  5. Apply RDP simplification
//
// Returns paths in local-mm coordinates, matching VectorizeLayer's contract.
func VectorizeWallCenterlines(m *ValetudoMap, layer *MapLayer, tolerance float64) []Path {
	if m == nil || layer == nil || len(layer.Pixels) == 0 {
		return nil
	}

	pixelSize := m.PixelSize
	if pixelSize <= 0 {
		return nil
	}

	// Convert mm values back to grid indices.
	gridPixels := make([]int, len(layer.Pixels))
	for i, v := range layer.Pixels {
		gridPixels[i] = v / pixelSize
	}

	// Build dense boolean grid with 1px padding.
	grid, minX, minY, width, height := pixelsToGrid(gridPixels, pixelSize)
	if grid == nil {
		return nil
	}

	// Extract centerline chains from the grid.
	chains := traceWallCenterlines(grid, width, height)

	// Convert grid coordinates back to mm and simplify.
	ps := float64(pixelSize)
	var result []Path
	for _, chain := range chains {
		mmPath := make(Path, len(chain))
		for i, p := range chain {
			mmPath[i] = Point{
				X: (p.X + float64(minX)) * ps,
				Y: (p.Y + float64(minY)) * ps,
			}
		}

		if tolerance > 0 {
			mmPath = SimplifyRDP(mmPath, tolerance)
		}
		if len(mmPath) >= 2 {
			result = append(result, mmPath)
		}
	}

	return result
}

// traceWallCenterlines extracts ordered chains of pixels from a boolean grid.
// Each connected component of set pixels becomes one Path, walked through
// pixel centers. Uses 8-connectivity for neighbor detection to handle
// diagonal wall segments.
func traceWallCenterlines(grid []bool, width, height int) []Path {
	visited := make([]bool, len(grid))
	var paths []Path

	isSet := func(x, y int) bool {
		if x < 0 || x >= width || y < 0 || y >= height {
			return false
		}
		return grid[y*width+x]
	}

	// 8-connected neighbors (cardinal first, then diagonal).
	neighbors8 := [][2]int{
		{1, 0}, {-1, 0}, {0, 1}, {0, -1},
		{1, 1}, {1, -1}, {-1, 1}, {-1, -1},
	}

	// floodCollect gathers all pixels in a connected component using BFS.
	floodCollect := func(startX, startY int) []Point {
		var component []Point
		queue := []Point{{X: float64(startX), Y: float64(startY)}}
		visited[startY*width+startX] = true

		for len(queue) > 0 {
			p := queue[0]
			queue = queue[1:]
			component = append(component, p)

			px, py := int(p.X), int(p.Y)
			for _, n := range neighbors8 {
				nx, ny := px+n[0], py+n[1]
				if isSet(nx, ny) && !visited[ny*width+nx] {
					visited[ny*width+nx] = true
					queue = append(queue, Point{X: float64(nx), Y: float64(ny)})
				}
			}
		}
		return component
	}

	// orderChain takes a set of component pixels and orders them into a path
	// by walking from an endpoint. For loops (all degree >= 2), starts from
	// any pixel and walks until revisiting.
	orderChain := func(component []Point) Path {
		if len(component) <= 1 {
			return component
		}

		// Build a set for fast lookup.
		type coord struct{ x, y int }
		inComponent := make(map[coord]bool, len(component))
		for _, p := range component {
			inComponent[coord{int(p.X), int(p.Y)}] = true
		}

		// Find an endpoint (degree 1 within the component) to start from.
		// This produces a clean chain from one end to the other.
		componentDegree := func(x, y int) int {
			d := 0
			for _, n := range neighbors8 {
				nx, ny := x+n[0], y+n[1]
				if inComponent[coord{nx, ny}] {
					d++
				}
			}
			return d
		}

		startPt := component[0]
		for _, p := range component {
			if componentDegree(int(p.X), int(p.Y)) <= 1 {
				startPt = p
				break
			}
		}

		// Walk the chain using DFS (prefer cardinal over diagonal for smoother lines).
		walked := make(map[coord]bool, len(component))
		var chain Path
		cx, cy := int(startPt.X), int(startPt.Y)
		walked[coord{cx, cy}] = true
		chain = append(chain, Point{X: float64(cx), Y: float64(cy)})

		for {
			found := false
			for _, n := range neighbors8 {
				nx, ny := cx+n[0], cy+n[1]
				c := coord{nx, ny}
				if inComponent[c] && !walked[c] {
					walked[c] = true
					chain = append(chain, Point{X: float64(nx), Y: float64(ny)})
					cx, cy = nx, ny
					found = true
					break
				}
			}
			if !found {
				break
			}
		}

		// If we didn't visit all component pixels, it means the component
		// has branches (junction). Handle by appending remaining pixels
		// as separate sub-chains connected to the main chain.
		if len(walked) < len(component) {
			// Find unvisited branch starts adjacent to walked pixels.
			for _, p := range component {
				c := coord{int(p.X), int(p.Y)}
				if walked[c] {
					continue
				}
				// Walk this branch.
				var branch Path
				bx, by := int(p.X), int(p.Y)
				walked[coord{bx, by}] = true
				branch = append(branch, Point{X: float64(bx), Y: float64(by)})
				for {
					found := false
					for _, n := range neighbors8 {
						nx, ny := bx+n[0], by+n[1]
						bc := coord{nx, ny}
						if inComponent[bc] && !walked[bc] {
							walked[bc] = true
							branch = append(branch, Point{X: float64(nx), Y: float64(ny)})
							bx, by = nx, ny
							found = true
							break
						}
					}
					if !found {
						break
					}
				}
				if len(branch) >= 2 {
					// Find the junction point on the main chain and insert.
					// For simplicity, append as a separate path segment.
					chain = append(chain, branch...)
				}
			}
		}

		return chain
	}

	// Scan all pixels, find connected components, order each into a chain.
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !isSet(x, y) || visited[y*width+x] {
				continue
			}

			component := floodCollect(x, y)
			if len(component) == 0 {
				continue
			}

			// Single isolated pixel: skip (produces no useful line).
			if len(component) == 1 {
				continue
			}

			chain := orderChain(component)
			if len(chain) >= 2 {
				paths = append(paths, chain)
			}
		}
	}

	return paths
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
