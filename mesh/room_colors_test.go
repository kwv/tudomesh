package mesh

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"
	"testing"
)

func TestRoomColors_StableNumericOrder(t *testing.T) {
	m := &ValetudoMap{Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "10"}},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "2"}},
		{Type: "floor"},
	}}
	got := RoomColors(m)
	if len(got) != 2 || got["2"] != roomPalette[0] || got["10"] != roomPalette[1] {
		t.Errorf("RoomColors = %v", got)
	}
}

// alertTestMaps returns a reference map (Kitchen above Hall, 20x20 cells) and
// a robot map whose "Den" segment lands on reference cells (0,5)-(9,14),
// straddling both reference rooms.
func alertTestMaps() (map[string]*ValetudoMap, map[string]AffineMatrix) {
	ref := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1", Name: "Kitchen"}, Pixels: cells(0, 0, 20, 10)},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "2", Name: "Hall"}, Pixels: cells(0, 10, 20, 10)},
		{Type: "wall", Pixels: []int{20, 0, 20, 1, 20, 2}},
	}}
	robot := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "5", Name: "Den"}, Pixels: cells(100, 200, 10, 10)},
	}}
	return map[string]*ValetudoMap{"ref": ref, "rob": robot},
		map[string]AffineMatrix{"ref": Identity(), "rob": Translation(-100, -195)}
}

// cellSet turns [x1,y1,...] into a sorted list of "x,y" keys.
func cellSet(px []int) []string {
	var out []string
	for i := 0; i+1 < len(px); i += 2 {
		out = append(out, fmt.Sprintf("%d,%d", px[i], px[i+1]))
	}
	sort.Strings(out)
	return out
}

func sameCells(a, b []int) bool {
	sa, sb := cellSet(a), cellSet(b)
	if len(sa) != len(sb) {
		return false
	}
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

func TestAlertShape_FollowsRobotSegment(t *testing.T) {
	maps, tr := alertTestMaps()
	pos := &LivePosition{X: 5, Y: 8} // reference Kitchen, robot Den
	room := alertShape(maps, tr, map[string]bool{"rob": true}, "ref", "rob", pos)
	if room == nil {
		t.Fatal("no alert room")
	}
	if room.SegmentID != "5" || room.Name != "Den" || room.Vacuum != "rob" {
		t.Errorf("room = %+v, want Den (5) for rob", room)
	}
	if !sameCells(room.Pixels, cells(0, 5, 10, 10)) {
		t.Errorf("shape has %d cells, want the 10x10 block at (0,5)", len(room.Pixels)/2)
	}
}

func TestAlertShape_FallsBackToReference(t *testing.T) {
	maps, tr := alertTestMaps()
	pos := &LivePosition{X: 5, Y: 8}
	kitchen := cells(0, 0, 20, 10)

	noTransform := map[string]AffineMatrix{"ref": Identity()}
	farRobot := map[string]*ValetudoMap{"ref": maps["ref"], "rob": {PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "5"}, Pixels: cells(400, 400, 3, 3)},
	}}}

	tests := []struct {
		name       string
		maps       map[string]*ValetudoMap
		transforms map[string]AffineMatrix
		calibrated map[string]bool
		vacID      string
	}{
		{"uncalibrated", maps, tr, map[string]bool{"rob": false}, "rob"},
		{"no transform", maps, noTransform, nil, "rob"},
		{"no segment at position", farRobot, tr, nil, "rob"},
		{"robot is reference", maps, tr, nil, "ref"},
		{"robot has no map", maps, tr, nil, "ghost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			room := alertShape(tt.maps, tt.transforms, tt.calibrated, "ref", tt.vacID, pos)
			if room == nil || room.SegmentID != "1" || room.Name != "Kitchen" || room.Vacuum != tt.vacID {
				t.Fatalf("room = %+v, want reference Kitchen", room)
			}
			if !sameCells(room.Pixels, kitchen) {
				t.Errorf("shape is not the reference Kitchen")
			}
		})
	}

	if room := alertShape(maps, tr, nil, "ref", "rob", &LivePosition{X: 500, Y: 500}); room != nil {
		t.Errorf("far from any room: got %+v, want nil", room)
	}
	if room := alertShape(maps, tr, nil, "ref", "rob", nil); room != nil {
		t.Errorf("no position: got %+v, want nil", room)
	}
}

// A 90° rotation maps the robot's 7x4 room onto an exact 4x7 block.
func TestAlertShape_Rotated90HasNoHoles(t *testing.T) {
	ref := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1"}, Pixels: cells(0, 0, 20, 20)},
	}}
	robot := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "9", Name: "Nook"}, Pixels: cells(0, 0, 7, 4)},
	}}
	// (x,y) -> (10-y, 2+x): robot cells x 0..6, y 0..3 land on x 6..9, y 2..8.
	toRef := MultiplyMatrices(Translation(10, 2), RotationDeg(90))
	maps := map[string]*ValetudoMap{"ref": ref, "rob": robot}
	transforms := map[string]AffineMatrix{"ref": Identity(), "rob": toRef}

	room := alertShape(maps, transforms, nil, "ref", "rob", &LivePosition{X: 7.5, Y: 5.5})
	if room == nil || room.SegmentID != "9" {
		t.Fatalf("room = %+v, want Nook", room)
	}
	if !sameCells(room.Pixels, cells(6, 2, 4, 7)) {
		t.Errorf("rotated shape = %v, want the 4x7 block at (6,2)", room.Pixels)
	}
}

// Any rotation gives a shape whose outside is one connected region (no holes).
func TestAlertShape_Rotated30HasNoHoles(t *testing.T) {
	ref := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1"}, Pixels: cells(0, 0, 60, 60)},
	}}
	robot := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "9"}, Pixels: cells(0, 0, 25, 12)},
	}}
	toRef := MultiplyMatrices(Translation(20, 10), RotationDeg(30))
	maps := map[string]*ValetudoMap{"ref": ref, "rob": robot}
	transforms := map[string]AffineMatrix{"ref": Identity(), "rob": toRef}
	c := TransformPoint(Point{X: 12, Y: 6}, toRef)

	room := alertShape(maps, transforms, nil, "ref", "rob", &LivePosition{X: c.X, Y: c.Y})
	if room == nil || room.SegmentID != "9" {
		t.Fatalf("room = %+v, want robot segment", room)
	}
	if n := len(room.Pixels) / 2; n < 280 || n > 320 {
		t.Errorf("rotated shape has %d cells, want about 300", n)
	}

	in := map[image.Point]bool{}
	b := image.Rectangle{}
	for i := 0; i+1 < len(room.Pixels); i += 2 {
		p := image.Pt(room.Pixels[i], room.Pixels[i+1])
		in[p] = true
		b = b.Union(image.Rectangle{Min: p, Max: p.Add(image.Pt(1, 1))})
	}
	b = b.Inset(-1)
	seen := map[image.Point]bool{b.Min: true}
	queue := []image.Point{b.Min}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, d := range []image.Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			q := p.Add(d)
			if q.In(b) && !in[q] && !seen[q] {
				seen[q] = true
				queue = append(queue, q)
			}
		}
	}
	if outside := b.Dx()*b.Dy() - len(in); len(seen) != outside {
		t.Errorf("%d cells enclosed by the shape", outside-len(seen))
	}
}

// The alert room is the one under the vacuum's position, even when the
// robot's reported room name points elsewhere.
func TestRenderLiveToSVG_AlertRoomByPosition(t *testing.T) {
	r := liveTestRenderer()
	r.Meta = map[string]VacuumMeta{"rocky7": {Name: "rocky7", Room: "Kitchen"}}
	r.Highlight = "rocky7"
	root := renderLive(t, r, map[string]*LivePosition{"rocky7": {X: 5, Y: 15}})

	for _, id := range []string{"room-16", "room-17"} {
		if c := root.find(byID(id))[0].attr("class"); c != "room" {
			t.Errorf("%s class = %q, want room", id, c)
		}
	}
	alert := root.find(byID("alert-room"))
	if len(alert) != 1 {
		t.Fatalf("want one #alert-room, got %d", len(alert))
	}
	if a := alert[0]; a.attr("class") != "alert-room" || a.attr("data-segment-id") != "17" || a.attr("data-vacuum") != "rocky7" {
		t.Errorf("alert-room attrs: %+v", a.Attrs)
	}
}

func TestRenderLiveToSVG_AlertRoomIsRobotSegment(t *testing.T) {
	maps, tr := alertTestMaps()
	r := NewVectorRenderer(maps, tr, "ref")
	r.Highlight = "rob"
	r.Calibrated = map[string]bool{"rob": true}
	root := renderLive(t, r, map[string]*LivePosition{"rob": {X: 5, Y: 8}})

	mapGroup := root.find(byID("map"))
	if len(mapGroup) != 1 {
		t.Fatal("missing #map")
	}
	var order []string
	for _, c := range mapGroup[0].Children {
		order = append(order, c.attr("id"))
	}
	if strings.Join(order, ",") != "rooms,alert-room,walls" {
		t.Errorf("#map children = %v, want rooms, alert-room, walls", order)
	}

	a := root.find(byID("alert-room"))[0]
	if a.attr("data-room") != "Den" || a.attr("data-segment-id") != "5" || a.attr("data-vacuum") != "rob" {
		t.Errorf("alert-room attrs: %+v", a.Attrs)
	}
	if d := a.attr("d"); d != "M0 5H10V15H0z" {
		t.Errorf("alert-room d = %q, want the 10x10 outline at (0,5)", d)
	}

	// Uncalibrated: the reference room under the position instead.
	r.Calibrated = map[string]bool{}
	root = renderLive(t, r, map[string]*LivePosition{"rob": {X: 5, Y: 8}})
	if a := root.find(byID("alert-room"))[0]; a.attr("data-room") != "Kitchen" || a.attr("data-segment-id") != "1" {
		t.Errorf("uncalibrated alert-room attrs: %+v", a.Attrs)
	}
}

func TestRenderLiveToSVG_NoHighlightNoAlertRoom(t *testing.T) {
	root := renderLive(t, liveTestRenderer(), map[string]*LivePosition{"rocky7": {X: 5, Y: 15}})
	if n := len(root.find(byID("alert-room"))); n != 0 {
		t.Errorf("got %d #alert-room without a highlight", n)
	}
}

func TestRenderLiveToSVG_RoomFills(t *testing.T) {
	r := liveTestRenderer()
	var buf bytes.Buffer
	if err := r.RenderLiveToSVG(&buf, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"#room-16{fill:" + svgColor(roomPalette[0]) + "}",
		"#room-17{fill:" + svgColor(roomPalette[1]) + "}",
		".alert-room{fill:" + svgColor(AlertRoomColor) + ";stroke:" + svgColor(color.NRGBA(alertBorderColor)),
		"stroke-dasharray:",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing CSS %q", want)
		}
	}
}

func TestCellOutlinePath(t *testing.T) {
	tests := []struct {
		name   string
		pixels []int
		want   string
	}{
		{"empty", nil, ""},
		{"single cell", []int{3, 4}, "M3 4H4V5H3z"},
		{"block", cells(0, 0, 3, 2), "M0 0H3V2H0z"},
		{"L shape", append(cells(0, 0, 2, 1), 0, 1), "M0 0H2V1H1V2H0z"},
		{"ring has a hole", ringCells(), "M0 0H3V3H0zM2 1H1V2H2z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cellOutlinePath(tt.pixels); got != tt.want {
				t.Errorf("cellOutlinePath = %q, want %q", got, tt.want)
			}
		})
	}
}

// ringCells is a 3x3 block without its center.
func ringCells() []int {
	var px []int
	c := cells(0, 0, 3, 3)
	for i := 0; i+1 < len(c); i += 2 {
		if c[i] != 1 || c[i+1] != 1 {
			px = append(px, c[i], c[i+1])
		}
	}
	return px
}

// alertPNGRenderer renders the reference map at 4 image pixels per cell.
func alertPNGRenderer(maps map[string]*ValetudoMap, tr map[string]AffineMatrix) *CompositeRenderer {
	r := NewCompositeRenderer(maps, tr, "ref")
	r.Scale, r.Padding = 4, 0
	return r
}

func pixelAt(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func TestRenderLiveHighlight_PNGRoomFills(t *testing.T) {
	m := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1", Name: "Kitchen"}, Pixels: cells(0, 0, 20, 10)},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "2", Name: "Hall"}, Pixels: cells(0, 10, 20, 10)},
	}}
	r := alertPNGRenderer(map[string]*ValetudoMap{"ref": m}, map[string]AffineMatrix{"ref": Identity()})

	plain := r.RenderGreyscale()
	if got := pixelAt(plain, 13, 13); got != roomPalette[0] {
		t.Errorf("kitchen = %v, want %v", got, roomPalette[0])
	}
	if got := pixelAt(plain, 13, 53); got != roomPalette[1] {
		t.Errorf("hall = %v, want %v", got, roomPalette[1])
	}

	alert := r.RenderLiveHighlight(map[string]*LivePosition{"v": {X: 15, Y: 15, Color: "#000000"}}, "v", "")
	if got := pixelAt(alert, 14, 53); got != AlertRoomColor { // off the hatch lines
		t.Errorf("hall with vacuum = %v, want alert %v", got, AlertRoomColor)
	}
	if got := pixelAt(alert, 69, 33); got != roomPalette[0] { // clear of the legend
		t.Errorf("kitchen = %v, want %v", got, roomPalette[0])
	}
}

// The alert room also has a darker inner border and a hatch, so it does not
// rely on hue alone.
func TestRenderLiveHighlight_PNGSecondCue(t *testing.T) {
	maps, tr := alertTestMaps()
	r := alertPNGRenderer(maps, tr)
	r.Scale = 8
	r.Calibrated = map[string]bool{"rob": true}
	img := r.RenderLiveHighlight(map[string]*LivePosition{"rob": {X: 5, Y: 8, Color: "#000000"}}, "rob", "")

	// Den covers reference cells x 0..9, y 5..14: image x 0..79, y 40..119.
	// The vacuum icon and ring sit around (40,64); the checks avoid them.
	border := color.NRGBA(alertBorderColor)
	for _, p := range []image.Point{{70, 40}, {70, 41}, {78, 100}, {79, 100}, {10, 119}, {0, 100}} {
		if got := pixelAt(img, p.X, p.Y); got != border {
			t.Errorf("edge pixel %v = %v, want border %v", p, got, border)
		}
	}
	if got := pixelAt(img, 70, 39); got != roomPalette[0] {
		t.Errorf("just above Den = %v, want kitchen %v", got, roomPalette[0])
	}
	if got := pixelAt(img, 80, 100); got != roomPalette[1] {
		t.Errorf("right of Den = %v, want hall %v", got, roomPalette[1])
	}
	hatch, fill := 0, 0
	for y := 82; y < 116; y++ {
		for x := 3; x < 77; x++ {
			switch pixelAt(img, x, y) {
			case color.NRGBA(alertHatchColor):
				hatch++
			case AlertRoomColor:
				fill++
			}
		}
	}
	if hatch == 0 || fill == 0 || hatch > fill/3 {
		t.Errorf("interior: %d hatch, %d fill pixels; want a sparse hatch", hatch, fill)
	}
}

// A robot on a room of its own that is ignored gets no highlight, not the
// reference room next to it.
func TestAlertShape_IgnoredOwnRoom(t *testing.T) {
	maps, tr := alertTestMaps()
	pos := &LivePosition{X: 5, Y: 8} // reference Kitchen, robot Den
	if room := alertShape(maps, tr, nil, "ref", "rob", pos, "den"); room != nil {
		t.Errorf("room = %+v, want nil for an ignored own room", room)
	}
	if room := alertShape(maps, tr, nil, "ref", "ref", pos, "kitchen"); room != nil {
		t.Errorf("room = %+v, want nil for an ignored reference room", room)
	}
}
