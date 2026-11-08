package mesh

import (
	"testing"
)

// rectPx returns grid pixels for the inclusive rectangle [x0,x1]x[y0,y1].
func rectPx(x0, y0, x1, y1 int) []int {
	var px []int
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			px = append(px, x, y)
		}
	}
	return px
}

func without(px []int, drop []int) []int {
	skip := map[[2]int]bool{}
	for i := 0; i+1 < len(drop); i += 2 {
		skip[[2]int{drop[i], drop[i+1]}] = true
	}
	var out []int
	for i := 0; i+1 < len(px); i += 2 {
		if !skip[[2]int{px[i], px[i+1]}] {
			out = append(out, px[i], px[i+1])
		}
	}
	return out
}

func segLayer(id, name string, px []int) MapLayer {
	return MapLayer{Type: "segment", Pixels: px, MetaData: LayerMetaData{SegmentID: id, Name: name}}
}

func wallLayer(px []int) MapLayer  { return MapLayer{Type: "wall", Pixels: px} }
func floorLayer(px []int) MapLayer { return MapLayer{Type: "floor", Pixels: px} }

func gridMap(layers ...MapLayer) *ValetudoMap {
	return &ValetudoMap{PixelSize: 5, Layers: layers}
}

// boxRoom is a w×h room of floor at the origin with a one-cell wall around it.
func boxRoom(id, name string, w, h int) *ValetudoMap {
	outer := rectPx(-1, -1, w, h)
	inner := rectPx(0, 0, w-1, h-1)
	return gridMap(segLayer(id, name, inner), wallLayer(without(outer, inner)))
}

// cellKinds returns the consensus kind per cell: "wall", "floor" or a room name.
func cellKinds(m *ValetudoMap) map[[2]int]string {
	out := map[[2]int]string{}
	for _, l := range m.Layers {
		k := l.Type
		if l.Type == "segment" {
			k = l.MetaData.Name
		}
		for i := 0; i+1 < len(l.Pixels); i += 2 {
			out[[2]int{l.Pixels[i], l.Pixels[i+1]}] = k
		}
	}
	return out
}

func identities(ids ...string) map[string]AffineMatrix {
	out := map[string]AffineMatrix{}
	for _, id := range ids {
		out[id] = Identity()
	}
	return out
}

func TestConsensusSingleVacuumKeepsEverything(t *testing.T) {
	m := boxRoom("1", "Kitchen", 10, 6)
	got := BuildConsensusMap(map[string]*ValetudoMap{"a": m}, identities("a"), "a")
	want := cellKinds(m)
	have := cellKinds(got)
	if len(have) != len(want) {
		t.Fatalf("got %d cells, want %d", len(have), len(want))
	}
	for c, k := range want {
		if have[c] != k {
			t.Errorf("cell %v: got %q want %q", c, have[c], k)
		}
	}
	if got.MetaData.TotalLayerArea != len(want)*25 {
		t.Errorf("TotalLayerArea %d, want %d", got.MetaData.TotalLayerArea, len(want)*25)
	}
}

func TestConsensusTwoVacuumsUnionWallsWin(t *testing.T) {
	a := boxRoom("1", "Room", 10, 6)
	// b sees a wall where a sees floor, and misses a cell a sees as floor.
	b := boxRoom("1", "Room", 10, 6)
	b.Layers[0].Pixels = without(b.Layers[0].Pixels, []int{4, 3, 7, 2})
	b.Layers[1].Pixels = append(b.Layers[1].Pixels, 4, 3)

	got := cellKinds(BuildConsensusMap(map[string]*ValetudoMap{"a": a, "b": b}, identities("a", "b"), "a"))
	if got[[2]int{4, 3}] != "wall" {
		t.Errorf("conflict cell: got %q, want wall", got[[2]int{4, 3}])
	}
	if got[[2]int{7, 2}] != "Room" {
		t.Errorf("cell only a sees: got %q, want Room", got[[2]int{7, 2}])
	}
}

func TestConsensusThreeVacuumsMajority(t *testing.T) {
	a := boxRoom("1", "Room", 10, 6)
	b := boxRoom("1", "Room", 10, 6)
	c := boxRoom("1", "Room", 10, 6)
	// a alone sees a wall inside the room; b and c see floor.
	a.Layers[0].Pixels = without(a.Layers[0].Pixels, []int{4, 3})
	a.Layers[1].Pixels = append(a.Layers[1].Pixels, 4, 3)
	// c alone sees floor beyond the right wall, within b's and a's coverage.
	c.Layers[0].Pixels = append(c.Layers[0].Pixels, 11, 2)

	got := cellKinds(BuildConsensusMap(map[string]*ValetudoMap{"a": a, "b": b, "c": c}, identities("a", "b", "c"), "a"))
	if got[[2]int{4, 3}] != "Room" {
		t.Errorf("1-of-3 wall: got %q, want Room", got[[2]int{4, 3}])
	}
	if k, ok := got[[2]int{11, 2}]; ok {
		t.Errorf("1-of-3 floor: got %q, want empty", k)
	}
}

func TestConsensusOutsideCoverageIsNoVote(t *testing.T) {
	a := boxRoom("1", "Room", 10, 6)
	b := boxRoom("1", "Room", 10, 6)
	c := boxRoom("1", "Room", 10, 6)
	// Only a explored a second room far to the right.
	extra := boxRoom("2", "Office", 5, 5)
	for i := range extra.Layers {
		px := extra.Layers[i].Pixels
		for j := 0; j < len(px); j += 2 {
			px[j] += 40
		}
		a.Layers = append(a.Layers, extra.Layers[i])
	}

	got := cellKinds(BuildConsensusMap(map[string]*ValetudoMap{"a": a, "b": b, "c": c}, identities("a", "b", "c"), "a"))
	if got[[2]int{42, 2}] != "Office" {
		t.Errorf("room only a reached: got %q, want Office", got[[2]int{42, 2}])
	}
	if got[[2]int{39, 2}] != "wall" {
		t.Errorf("its wall: got %q, want wall", got[[2]int{39, 2}])
	}
}

func TestConsensusWallToleratesOneCellOffset(t *testing.T) {
	// A wall across an open floor: a has it at x=10, b one cell over at x=11,
	// c saw an open door (floor only). With an exact match the wall would be
	// 1 of 3; with one cell of tolerance it is 2 of 3.
	floorPx := rectPx(0, 0, 20, 5)
	wallAt := func(x int) []int { return rectPx(x, 0, x, 5) }
	a := gridMap(floorLayer(without(floorPx, wallAt(10))), wallLayer(wallAt(10)))
	b := gridMap(floorLayer(without(floorPx, wallAt(11))), wallLayer(wallAt(11)))
	c := gridMap(floorLayer(floorPx))

	got := cellKinds(BuildConsensusMap(map[string]*ValetudoMap{"a": a, "b": b, "c": c}, identities("a", "b", "c"), "a"))
	if got[[2]int{10, 2}] != "wall" {
		t.Errorf("x=10: got %q, want wall", got[[2]int{10, 2}])
	}
	if got[[2]int{5, 2}] != "floor" {
		t.Errorf("x=5: got %q, want floor", got[[2]int{5, 2}])
	}
}

func TestConsensusRoomsMatchByOverlapNotName(t *testing.T) {
	// Same place, different names: one room, labelled and numbered by the reference.
	a := boxRoom("16", "Kitchen", 10, 6)
	b := boxRoom("4", "Cuisine", 10, 6)
	// Same name, different places: two rooms.
	far := boxRoom("9", "Kitchen", 6, 6)
	for i := range far.Layers {
		for j := 0; j < len(far.Layers[i].Pixels); j += 2 {
			far.Layers[i].Pixels[j] += 40
		}
	}
	b.Layers = append(b.Layers, far.Layers...)

	got := BuildConsensusMap(map[string]*ValetudoMap{"a": a, "b": b}, identities("a", "b"), "a")
	var segs []LayerMetaData
	for _, l := range got.Layers {
		if l.Type == "segment" {
			segs = append(segs, l.MetaData)
		}
	}
	if len(segs) != 2 {
		t.Fatalf("segments %+v, want 2", segs)
	}
	if segs[0].Name != "Kitchen" || segs[0].SegmentID != "16" {
		t.Errorf("shared room %+v, want Kitchen with the reference id 16", segs[0])
	}
	if segs[1].Name != "Kitchen" || segs[1].SegmentID != "17" {
		t.Errorf("room only b has %+v, want Kitchen with new id 17", segs[1])
	}
}

func TestConsensusRoomVoteAndTieBreak(t *testing.T) {
	// a and b split a corridor at x=10, c at x=13; c's segments still match
	// a's rooms by overlap. Cells x=10..12 are Right for a and b, Left for c.
	two := func(split int) *ValetudoMap {
		return gridMap(segLayer("1", "Left", rectPx(0, 0, split-1, 5)), segLayer("2", "Right", rectPx(split, 0, 19, 5)))
	}
	a, b, c := two(10), two(10), two(13)

	got := cellKinds(BuildConsensusMap(map[string]*ValetudoMap{"a": a, "b": b, "c": c}, identities("a", "b", "c"), "a"))
	if got[[2]int{11, 2}] != "Right" {
		t.Errorf("2 of 3: got %q, want Right", got[[2]int{11, 2}])
	}
	// With only a and c it is a tie; c's Left segment (78 cells) beats a's Right (60).
	got = cellKinds(BuildConsensusMap(map[string]*ValetudoMap{"a": a, "c": c}, identities("a", "c"), "a"))
	if got[[2]int{11, 2}] != "Left" {
		t.Errorf("tie: got %q, want Left", got[[2]int{11, 2}])
	}
}

func TestConsensusRotatedMapHasNoHoles(t *testing.T) {
	ref := boxRoom("1", "Room", 12, 8)
	// b is the same room in a frame rotated by 90° and shifted.
	truth := CreateRotationTranslation(90, 30, -7)
	b := transformMap(ref, InvertMatrix(truth))

	got := cellKinds(BuildConsensusMap(map[string]*ValetudoMap{"ref": ref, "b": b},
		map[string]AffineMatrix{"ref": Identity(), "b": truth}, "ref"))
	want := cellKinds(ref)
	for c, k := range want {
		if got[c] != k {
			t.Errorf("cell %v: got %q want %q", c, got[c], k)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d cells, want %d", len(got), len(want))
	}
}

func TestConsensusSkipsVacuumWithoutTransform(t *testing.T) {
	a := boxRoom("1", "Room", 10, 6)
	stray := boxRoom("1", "Room", 4, 4)
	for i := range stray.Layers {
		for j := range stray.Layers[i].Pixels {
			stray.Layers[i].Pixels[j] += 100
		}
	}
	got := cellKinds(BuildConsensusMap(map[string]*ValetudoMap{"a": a, "stray": stray}, identities("a"), "a"))
	if _, ok := got[[2]int{101, 101}]; ok {
		t.Error("uncalibrated vacuum was included")
	}
}

func TestConsensusEmpty(t *testing.T) {
	if m := BuildConsensusMap(map[string]*ValetudoMap{"a": gridMap()}, identities("a"), "a"); m != nil {
		t.Errorf("got %+v, want nil", m)
	}
}

func TestConsensusTinyRoomBecomesFloor(t *testing.T) {
	a := gridMap(segLayer("1", "Hall", rectPx(0, 0, 19, 5)), segLayer("2", "", rectPx(20, 0, 22, 1)))
	got := cellKinds(BuildConsensusMap(map[string]*ValetudoMap{"a": a}, identities("a"), "a"))
	if got[[2]int{21, 0}] != "floor" {
		t.Errorf("6-cell room: got %q, want floor", got[[2]int{21, 0}])
	}
	if got[[2]int{5, 2}] != "Hall" {
		t.Errorf("Hall: got %q", got[[2]int{5, 2}])
	}
}
