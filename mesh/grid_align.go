package mesh

import (
	"math"
	"runtime"
	"sync"
)

// Grid alignment: rasterize both maps into floor/wall occupancy grids, then
// search rotation and translation for the placement where floor lands on floor
// and wall on wall. Uses map geometry only (no room names), is deterministic,
// and runs coarse-to-fine to stay well under a second per map pair.

const (
	cellEmpty int8 = 0
	cellFloor int8 = 1
	cellWall  int8 = 2
)

// gridScore[src][tgt] is the reward for a source cell of one kind landing on a
// target cell of another. Floor on wall (or wall on floor) means the placement
// puts open space where the other robot saw a wall.
var gridScore = [3][3]int{
	cellEmpty: {0, 0, 0},
	cellFloor: {0, 1, -2},
	cellWall:  {0, -1, 3},
}

// GridAlignOptions controls AlignByGrid. Zero values pick defaults.
type GridAlignOptions struct {
	// RotationHint restricts the coarse rotation search to HintWindowDeg
	// around this angle (degrees). nil searches the full circle.
	RotationHint  *float64
	HintWindowDeg float64 // default 20
	CoarseStepDeg float64 // default 2
}

// GridAlignment is the result of AlignByGrid.
type GridAlignment struct {
	Transform   AffineMatrix
	RotationDeg float64
	Score       int
	// Confidence is the winner's score divided by the best score at a rotation
	// more than 15° away, both refined at fine resolution. Values near 1 mean another orientation fits
	// about as well. +Inf when nothing else scored above zero or a
	// RotationHint was given.
	Confidence float64
}

// MinGridConfidence is the Confidence below which a grid alignment is treated
// as ambiguous.
const MinGridConfidence = 1.25

type occGrid struct {
	minX, minY, w, h int
	cell             float64
	v                []int8
}

type occCell struct {
	x, y int
	kind int8
}

func cellIndex(v, cell float64) int { return int(math.Floor(v / cell)) }

func newOccGrid(floor, wall []Point, cell float64) occGrid {
	minX, minY := math.MaxInt, math.MaxInt
	maxX, maxY := math.MinInt, math.MinInt
	for _, set := range [][]Point{floor, wall} {
		for _, p := range set {
			x, y := cellIndex(p.X, cell), cellIndex(p.Y, cell)
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	g := occGrid{minX: minX, minY: minY, w: maxX - minX + 1, h: maxY - minY + 1, cell: cell}
	g.v = make([]int8, g.w*g.h)
	for _, p := range floor {
		g.v[(cellIndex(p.Y, cell)-minY)*g.w+cellIndex(p.X, cell)-minX] = cellFloor
	}
	// Walls are written last so they win where both kinds share a cell.
	for _, p := range wall {
		g.v[(cellIndex(p.Y, cell)-minY)*g.w+cellIndex(p.X, cell)-minX] = cellWall
	}
	return g
}

func (g occGrid) at(x, y int) int8 {
	x -= g.minX
	y -= g.minY
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return cellEmpty
	}
	return g.v[y*g.w+x]
}

func (g occGrid) cells() []occCell {
	var out []occCell
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			if k := g.v[y*g.w+x]; k != cellEmpty {
				out = append(out, occCell{x + g.minX, y + g.minY, k})
			}
		}
	}
	return out
}

func scorePlacement(src []occCell, tgt occGrid, dx, dy int) int {
	s := 0
	for _, c := range src {
		s += gridScore[c.kind][tgt.at(c.x+dx, c.y+dy)]
	}
	return s
}

// mapGeometry returns floor/segment points and wall points of a map.
func mapGeometry(m *ValetudoMap) (floor, wall []Point) {
	for _, l := range m.Layers {
		switch l.Type {
		case "wall":
			wall = append(wall, PixelsToPoints(l.Pixels)...)
		case "floor", "segment":
			floor = append(floor, PixelsToPoints(l.Pixels)...)
		}
	}
	return floor, wall
}

func rotationAbout(c Point, deg float64) AffineMatrix {
	return MultiplyMatrices(Translation(c.X, c.Y), MultiplyMatrices(RotationDeg(deg), Translation(-c.X, -c.Y)))
}

type placement struct {
	rot    float64
	dx, dy int
	score  int
}

// bestShift searches integer cell shifts in [dx0,dx1]x[dy0,dy1] for one rotation.
func bestShift(src occGrid, tgt occGrid, rot float64, dx0, dx1, dy0, dy1 int) placement {
	cells := src.cells()
	best := placement{rot: rot, score: math.MinInt}
	for dx := dx0; dx <= dx1; dx++ {
		for dy := dy0; dy <= dy1; dy++ {
			if s := scorePlacement(cells, tgt, dx, dy); s > best.score {
				best = placement{rot, dx, dy, s}
			}
		}
	}
	return best
}

func angleDiff(a, b float64) float64 {
	d := math.Abs(NormalizeAngle(a - b))
	if d > 180 {
		d = 360 - d
	}
	return d
}

// gridCellSizes picks coarse and fine cell sizes in grid units (layer pixels):
// coarse is about 1/30 of the target map's extent, clamped to 2..10 pixels;
// fine is 1/5 of that, at least 1 pixel.
func gridCellSizes(floor, wall []Point) (coarse, fine float64) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, set := range [][]Point{floor, wall} {
		for _, p := range set {
			minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
			maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
		}
	}
	extent := math.Max(maxX-minX, maxY-minY)
	coarse = math.Max(2, math.Min(10, extent/30))
	fine = math.Max(1, coarse/5)
	return coarse, fine
}

// AlignByGrid finds the rigid transform mapping source into target by
// maximizing floor/wall overlap. ok is false if either map has no geometry.
func AlignByGrid(source, target *ValetudoMap, opts GridAlignOptions) (GridAlignment, bool) {
	sFloor, sWall := mapGeometry(source)
	tFloor, tWall := mapGeometry(target)
	if len(sFloor) == 0 || len(tFloor) == 0 {
		return GridAlignment{}, false
	}

	coarse, fine := gridCellSizes(tFloor, tWall)
	step := opts.CoarseStepDeg
	if step <= 0 {
		step = 2
	}

	var rotations []float64
	if opts.RotationHint != nil {
		win := opts.HintWindowDeg
		if win <= 0 {
			win = 20
		}
		for d := -win; d <= win; d += step {
			rotations = append(rotations, *opts.RotationHint+d)
		}
	} else {
		for r := 0.0; r < 360; r += step {
			rotations = append(rotations, r)
		}
	}

	pivot := Centroid(sFloor)
	tCoarse := newOccGrid(tFloor, tWall, coarse)

	results := make([]placement, len(rotations))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, rot := range rotations {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, rot float64) {
			defer func() { <-sem; wg.Done() }()
			R := rotationAbout(pivot, rot)
			sg := newOccGrid(TransformPoints(sFloor, R), TransformPoints(sWall, R), coarse)
			// Shifts that keep at least one cell of overlap.
			dx0 := tCoarse.minX - (sg.minX + sg.w - 1)
			dx1 := tCoarse.minX + tCoarse.w - 1 - sg.minX
			dy0 := tCoarse.minY - (sg.minY + sg.h - 1)
			dy1 := tCoarse.minY + tCoarse.h - 1 - sg.minY
			results[i] = bestShift(sg, tCoarse, rot, dx0, dx1, dy0, dy1)
		}(i, rot)
	}
	wg.Wait()

	best := results[0]
	for _, r := range results {
		if r.score > best.score {
			best = r
		}
	}

	// Fine pass: finer grid, rotation within one coarse step, shift within one coarse cell.
	tFine := newOccGrid(tFloor, tWall, fine)
	k := int(math.Round(coarse / fine))
	refine := func(c placement) placement {
		out := placement{score: math.MinInt}
		for d := -step; d <= step; d += step / 4 {
			rot := c.rot + d
			R := rotationAbout(pivot, rot)
			sg := newOccGrid(TransformPoints(sFloor, R), TransformPoints(sWall, R), fine)
			p := bestShift(sg, tFine, rot, c.dx*k-k, c.dx*k+k, c.dy*k-k, c.dy*k+k)
			if p.score > out.score {
				out = p
			}
		}
		return out
	}
	winner := refine(best)

	// A rotation hint already settles orientation. For a full sweep, refine the
	// best clearly different orientation too and compare at fine resolution;
	// coarse cells can hide a near-tie (e.g. a rectangle at 0° vs 180°).
	conf := math.Inf(1)
	if opts.RotationHint == nil {
		rival := placement{score: 0}
		for _, r := range results {
			if angleDiff(r.rot, best.rot) > 15 && r.score > rival.score {
				rival = r
			}
		}
		if rival.score > 0 {
			other := refine(rival)
			if other.score > winner.score {
				winner, other = other, winner
			}
			if other.score > 0 {
				conf = float64(winner.score) / float64(other.score)
			}
			// On a near-tie, prefer the smaller rotation so the result is stable.
			if conf < MinGridConfidence && angleDiff(other.rot, 0) < angleDiff(winner.rot, 0) {
				winner = other
			}
		}
	}

	transform := MultiplyMatrices(
		Translation(float64(winner.dx)*fine, float64(winner.dy)*fine),
		rotationAbout(pivot, winner.rot),
	)
	return GridAlignment{
		Transform:   transform,
		RotationDeg: NormalizeAngle(winner.rot),
		Score:       winner.score,
		Confidence:  conf,
	}, true
}
