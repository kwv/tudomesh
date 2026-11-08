package mesh

import (
	"math"
	"sort"
	"strconv"
)

// Consensus floorplan: every aligned vacuum votes on each cell of the
// reference grid (issue #115).
//
//   - A vacuum covers a cell when it has floor or wall within ConsensusCoverRadius
//     cells; outside that it casts no vote.
//   - Floor votes are exact (floor or segment on that cell). Wall votes allow
//     ConsensusWallTolerance cells of misalignment, but a cell only becomes wall
//     if some vacuum has a wall on it, and parallel lines one cell apart are
//     drawn once (the reference's line first).
//   - A kind is kept when its votes reach at least half of the covering
//     vacuums, walls first (walls win ties). With one covering vacuum its
//     observation is kept; with two, the union with walls winning conflicts;
//     with three, two of three.
//   - Rooms are matched by overlap, not by name: a vacuum's segment joins the
//     room (reference's first) that covers at least half of it, else it starts
//     a new room. Each floor cell takes the room most vacuums put it in; ties
//     go to the larger segment. Rooms left with fewer than
//     ConsensusMinRoomCells cells become plain floor. Names are labels only.

const (
	ConsensusCoverRadius   = 1 // cells
	ConsensusWallTolerance = 1 // cells
	// Rooms smaller than this after voting become plain floor.
	ConsensusMinRoomCells = 25
)

// robotRaster is one vacuum's map resampled into the consensus window.
type robotRaster struct {
	kind []int8  // cellEmpty / cellFloor / cellWall
	seg  []int32 // index into segs, -1 for none
	segs []consensusSeg
}

type consensusSeg struct {
	name, id string
	pixels   int
	room     int // index into the matched rooms
}

// consensusRoom is one room after matching: its label and, for rooms the
// reference has, the reference's segment id.
type consensusRoom struct {
	name, id string
}

type cellWindow struct{ minX, minY, w, h int }

func (win cellWindow) idx(x, y int) int { return (y-win.minY)*win.w + x - win.minX }

// BuildConsensusMap merges the maps into one map in refID's grid. transforms
// map each vacuum's grid into the reference grid; vacuums without a transform
// are left out (the reference defaults to identity). Returns nil when no map
// has geometry.
func BuildConsensusMap(maps map[string]*ValetudoMap, transforms map[string]AffineMatrix, refID string) *ValetudoMap {
	ids := make([]string, 0, len(maps))
	for _, id := range sortedKeys(maps) {
		if maps[id] == nil {
			continue
		}
		if _, ok := transforms[id]; ok || id == refID {
			ids = append(ids, id)
		}
	}
	// Reference first: its wall line wins where lines from several vacuums
	// run side by side.
	sort.SliceStable(ids, func(i, j int) bool { return ids[i] == refID && ids[j] != refID })
	tf := func(id string) AffineMatrix {
		if t, ok := transforms[id]; ok {
			return t
		}
		return Identity()
	}

	// Window: union of every transformed map's bounds plus the cover radius.
	minX, minY, maxX, maxY := math.MaxInt, math.MaxInt, math.MinInt, math.MinInt
	for _, id := range ids {
		for _, l := range maps[id].Layers {
			if l.Type != "floor" && l.Type != "segment" && l.Type != "wall" {
				continue
			}
			t := tf(id)
			for i := 0; i+1 < len(l.Pixels); i += 2 {
				p := TransformPoint(Point{X: float64(l.Pixels[i]), Y: float64(l.Pixels[i+1])}, t)
				x, y := int(math.Round(p.X)), int(math.Round(p.Y))
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			}
		}
	}
	if minX > maxX {
		return nil
	}
	pad := ConsensusCoverRadius + 1
	win := cellWindow{minX - pad, minY - pad, maxX - minX + 1 + 2*pad, maxY - minY + 1 + 2*pad}
	n := win.w * win.h

	rasters := make([]robotRaster, 0, len(ids))
	for _, id := range ids {
		rasters = append(rasters, rasterizeInto(maps[id], tf(id), win))
	}
	rooms := matchRooms(rasters, n, ids[0] == refID)

	// Per-vacuum coverage and wall-near masks.
	covered := make([][]bool, len(rasters))
	wallNear := make([][]bool, len(rasters))
	for i, r := range rasters {
		occ := make([]bool, n)
		wall := make([]bool, n)
		for c, k := range r.kind {
			occ[c] = k != cellEmpty
			wall[c] = k == cellWall
		}
		covered[i] = dilate(occ, win, ConsensusCoverRadius)
		wallNear[i] = dilate(wall, win, ConsensusWallTolerance)
	}

	result := make([]int8, n)
	for c := 0; c < n; c++ {
		cover, wallVotes, floorVotes := 0, 0, 0
		firstNear, firstExact := -1, -1 // first vacuum (in ids order) with a wall near / on c
		for i, r := range rasters {
			if !covered[i][c] {
				continue
			}
			cover++
			if wallNear[i][c] {
				wallVotes++
				if firstNear < 0 {
					firstNear = i
				}
			}
			switch r.kind[c] {
			case cellWall:
				if firstExact < 0 {
					firstExact = i
				}
			case cellFloor:
				floorVotes++
			}
		}
		// A wall cell is drawn from the first vacuum with a wall near it, so
		// lines one cell apart don't double up.
		switch {
		case firstExact >= 0 && firstExact == firstNear && 2*wallVotes >= cover:
			result[c] = cellWall
		case floorVotes > 0 && 2*floorVotes >= cover:
			result[c] = cellFloor
		}
	}

	return assembleConsensus(maps[refID], rasters, rooms, result, win)
}

// rasterizeInto resamples m into win: each window cell is mapped back into
// m's grid with the inverse transform and reads the nearest cell, so rotated
// maps leave no holes.
func rasterizeInto(m *ValetudoMap, t AffineMatrix, win cellWindow) robotRaster {
	n := win.w * win.h
	r := robotRaster{kind: make([]int8, n), seg: make([]int32, n)}
	for i := range r.seg {
		r.seg[i] = -1
	}

	// Source grid in m's own cells.
	type srcCell struct {
		kind int8
		seg  int32
	}
	src := map[[2]int]srcCell{}
	sMinX, sMinY, sMaxX, sMaxY := math.MaxInt, math.MaxInt, math.MinInt, math.MinInt
	put := func(x, y int, k int8, seg int32) {
		cur, ok := src[[2]int{x, y}]
		if ok && cur.kind == cellWall {
			return // walls win where layers overlap
		}
		if ok && k == cellFloor && seg < 0 && cur.seg >= 0 {
			return // keep the segment over plain floor
		}
		src[[2]int{x, y}] = srcCell{k, seg}
		sMinX, sMinY, sMaxX, sMaxY = min(sMinX, x), min(sMinY, y), max(sMaxX, x), max(sMaxY, y)
	}
	for _, l := range m.Layers {
		seg := int32(-1)
		var k int8
		switch l.Type {
		case "wall":
			k = cellWall
		case "floor":
			k = cellFloor
		case "segment":
			k = cellFloor
			r.segs = append(r.segs, consensusSeg{
				name: l.MetaData.Name, id: l.MetaData.SegmentID, pixels: len(l.Pixels) / 2,
			})
			seg = int32(len(r.segs) - 1)
		default:
			continue
		}
		for i := 0; i+1 < len(l.Pixels); i += 2 {
			put(l.Pixels[i], l.Pixels[i+1], k, seg)
		}
	}
	if len(src) == 0 {
		return r
	}

	// Window cells whose inverse lands inside the source bounds.
	inv := InvertMatrix(t)
	corners := []Point{{X: float64(sMinX), Y: float64(sMinY)}, {X: float64(sMaxX), Y: float64(sMinY)},
		{X: float64(sMinX), Y: float64(sMaxY)}, {X: float64(sMaxX), Y: float64(sMaxY)}}
	bx0, by0, bx1, by1 := math.MaxInt, math.MaxInt, math.MinInt, math.MinInt
	for _, p := range corners {
		q := TransformPoint(p, t)
		bx0, by0 = min(bx0, int(math.Floor(q.X))-1), min(by0, int(math.Floor(q.Y))-1)
		bx1, by1 = max(bx1, int(math.Ceil(q.X))+1), max(by1, int(math.Ceil(q.Y))+1)
	}
	bx0, by0 = max(bx0, win.minX), max(by0, win.minY)
	bx1, by1 = min(bx1, win.minX+win.w-1), min(by1, win.minY+win.h-1)
	for y := by0; y <= by1; y++ {
		for x := bx0; x <= bx1; x++ {
			s := TransformPoint(Point{X: float64(x), Y: float64(y)}, inv)
			if c, ok := src[[2]int{int(math.Round(s.X)), int(math.Round(s.Y))}]; ok {
				i := win.idx(x, y)
				r.kind[i] = c.kind
				r.seg[i] = c.seg
			}
		}
	}
	return r
}

// dilate marks every cell within rad cells (square window) of a set cell.
func dilate(mask []bool, win cellWindow, rad int) []bool {
	if rad <= 0 {
		return mask
	}
	tmp := make([]bool, len(mask))
	for y := 0; y < win.h; y++ {
		row := y * win.w
		last := math.MinInt // last set x seen
		for x := 0; x < win.w+rad; x++ {
			if x < win.w && mask[row+x] {
				last = x
			}
			if t := x - rad; t >= 0 && t < win.w && last >= t-rad {
				tmp[row+t] = true
			}
		}
	}
	out := make([]bool, len(mask))
	for x := 0; x < win.w; x++ {
		last := math.MinInt
		for y := 0; y < win.h+rad; y++ {
			if y < win.h && tmp[y*win.w+x] {
				last = y
			}
			if t := y - rad; t >= 0 && t < win.h && last >= t-rad {
				out[t*win.w+x] = true
			}
		}
	}
	return out
}

// matchRooms groups segments across vacuums by overlap, in rasters order
// (reference first when firstIsRef). A segment joins the room already covering
// at least half of its cells; otherwise it starts a new room. Sets each
// segment's room and returns the rooms.
func matchRooms(rasters []robotRaster, n int, firstIsRef bool) []consensusRoom {
	var rooms []consensusRoom
	roomAt := make([]int32, n)
	for i := range roomAt {
		roomAt[i] = -1
	}
	for ri := range rasters {
		r := &rasters[ri]
		size := make([]int, len(r.segs))
		overlap := make([]map[int32]int, len(r.segs))
		for c, s := range r.seg {
			if s < 0 {
				continue
			}
			size[s]++
			if roomAt[c] >= 0 {
				if overlap[s] == nil {
					overlap[s] = map[int32]int{}
				}
				overlap[s][roomAt[c]]++
			}
		}
		for si := range r.segs {
			best, bestN := int32(-1), 0
			for room, cnt := range overlap[si] {
				if cnt > bestN || (cnt == bestN && room < best) {
					best, bestN = room, cnt
				}
			}
			seg := &r.segs[si]
			if best >= 0 && 2*bestN >= size[si] {
				seg.room = int(best)
				if rooms[best].name == "" {
					rooms[best].name = seg.name
				}
				continue
			}
			room := consensusRoom{name: seg.name}
			if ri == 0 && firstIsRef {
				room.id = seg.id
			}
			seg.room = len(rooms)
			rooms = append(rooms, room)
		}
		// Paint after matching so a vacuum's segments only match earlier vacuums.
		for c, s := range r.seg {
			if s >= 0 && roomAt[c] < 0 {
				roomAt[c] = int32(r.segs[s].room)
			}
		}
	}
	return rooms
}

// assembleConsensus assigns rooms to the consensus floor cells and builds the
// output map: one segment layer per room, a floor layer for cells no vacuum
// places in a room, and one wall layer.
func assembleConsensus(ref *ValetudoMap, rasters []robotRaster, rooms []consensusRoom, result []int8, win cellWindow) *ValetudoMap {
	pixels := make([][]int, len(rooms))
	var floor, wall []int

	type vote struct{ room, n, best int } // voters, largest voter segment
	votes := make([]vote, 0, len(rasters))

	for y := 0; y < win.h; y++ {
		for x := 0; x < win.w; x++ {
			c := y*win.w + x
			gx, gy := x+win.minX, y+win.minY
			switch result[c] {
			case cellWall:
				wall = append(wall, gx, gy)
				continue
			case cellEmpty:
				continue
			}

			votes = votes[:0]
			for _, r := range rasters {
				if r.seg[c] < 0 {
					continue
				}
				s := r.segs[r.seg[c]]
				j := 0
				for j < len(votes) && votes[j].room != s.room {
					j++
				}
				if j == len(votes) {
					votes = append(votes, vote{room: s.room})
				}
				votes[j].n++
				votes[j].best = max(votes[j].best, s.pixels)
			}
			if len(votes) == 0 {
				floor = append(floor, gx, gy)
				continue
			}
			b := votes[0]
			for _, v := range votes[1:] {
				if v.n > b.n || (v.n == b.n && v.best > b.best) || (v.n == b.n && v.best == b.best && v.room < b.room) {
					b = v
				}
			}
			pixels[b.room] = append(pixels[b.room], gx, gy)
		}
	}

	// Rooms the reference doesn't have get ids above the reference's numeric ids.
	nextID := 1
	for _, rm := range rooms {
		if v, err := strconv.Atoi(rm.id); err == nil && v >= nextID {
			nextID = v + 1
		}
	}

	out := &ValetudoMap{Class: "ValetudoMap", PixelSize: 5}
	if ref != nil {
		out.PixelSize = int(pixelSizeOf(ref))
		out.Size = ref.Size
		out.MetaData.Version = ref.MetaData.Version
	}
	ps2 := out.PixelSize * out.PixelSize
	area := 0
	add := func(l MapLayer) {
		n := len(l.Pixels) / 2
		l.MetaData.PixelCount = n
		l.MetaData.Area = n * ps2
		area += l.MetaData.Area
		out.Layers = append(out.Layers, l)
	}

	var segments []MapLayer
	for i, rm := range rooms {
		if len(pixels[i]) == 0 {
			continue
		}
		if len(pixels[i])/2 < ConsensusMinRoomCells {
			floor = append(floor, pixels[i]...)
			continue
		}
		id := rm.id
		if id == "" {
			id = strconv.Itoa(nextID)
			nextID++
		}
		segments = append(segments, MapLayer{Class: "MapLayer", Type: "segment", Pixels: pixels[i],
			MetaData: LayerMetaData{SegmentID: id, Name: rm.name}})
	}
	if len(floor) > 0 {
		add(MapLayer{Class: "MapLayer", Type: "floor", Pixels: floor})
	}
	for _, l := range segments {
		add(l)
	}
	if len(wall) > 0 {
		add(MapLayer{Class: "MapLayer", Type: "wall", Pixels: wall})
	}
	out.MetaData.TotalLayerArea = area
	return out
}
