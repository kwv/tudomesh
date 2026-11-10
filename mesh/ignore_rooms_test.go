package mesh

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestIsIgnoredRoom(t *testing.T) {
	ignore := []string{"Front Porch", "deck", "Küche"}
	tests := []struct {
		name string
		want bool
	}{
		{"Front Porch", true},
		{"front porch", true},
		{"FRONT_PORCH", true},
		{"front-porch", true},
		{"FrontPorch", true},
		{" front  porch ", true},
		{"Deck", true},
		{"KÜCHE", true},
		{"Porch", false},
		{"Kitchen", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsIgnoredRoom(tt.name, ignore); got != tt.want {
			t.Errorf("IsIgnoredRoom(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
	if IsIgnoredRoom("Porch", nil) {
		t.Error("nil ignore list matched")
	}
	if IsIgnoredRoom("", []string{"", "-"}) {
		t.Error("empty name matched an empty entry")
	}
}

func TestRoomColors_SkipsIgnored(t *testing.T) {
	m := &ValetudoMap{Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1", Name: "Porch"}},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "2", Name: "Hall"}},
	}}
	got := RoomColors(m, "porch")
	if _, ok := got["1"]; ok {
		t.Errorf("ignored room has a color: %v", got)
	}
	// The ignored room takes no palette slot.
	if got["2"] != roomPalette[0] {
		t.Errorf("hall = %v, want %v", got["2"], roomPalette[0])
	}
}

// porchMap has a Hall (segment 1) above a Porch (segment 2).
func porchMap() *ValetudoMap {
	return &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1", Name: "Hall"}, Pixels: cells(0, 0, 20, 10)},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "2", Name: "Porch"}, Pixels: cells(0, 10, 20, 10)},
	}}
}

func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func TestRenderLiveHighlight_IgnoredRoomIsFloor(t *testing.T) {
	r := NewCompositeRenderer(map[string]*ValetudoMap{"ref": porchMap()}, map[string]AffineMatrix{"ref": Identity()}, "ref")
	r.Scale, r.Padding = 4, 0
	r.IgnoreRooms = []string{"porch"}

	// The vacuum stands on the porch: no alert room, porch stays grey.
	img := r.RenderLiveHighlight(map[string]*LivePosition{"v": {X: 15, Y: 15, Color: "#000000"}}, "v", "")
	if got := nrgbaAt(img, 3*4+1, 13*4+1); got != GreyscaleFloor {
		t.Errorf("porch = %v, want floor %v", got, GreyscaleFloor)
	}
	if got := nrgbaAt(img, 17*4+1, 8*4+1); got != roomPalette[0] {
		t.Errorf("hall = %v, want %v", got, roomPalette[0])
	}
	if roomUnder(porchMap(), Identity(), &LivePosition{X: 15, Y: 15}, "Porch") != "" {
		t.Error("roomUnder picked the ignored porch")
	}
	if got := roomUnder(porchMap(), Identity(), &LivePosition{X: 15, Y: 5}, "Porch"); got != "1" {
		t.Errorf("roomUnder in hall = %q, want 1", got)
	}
}

func TestRenderLiveToSVG_IgnoredRoom(t *testing.T) {
	r := NewVectorRenderer(map[string]*ValetudoMap{"ref": porchMap()}, map[string]AffineMatrix{"ref": Identity()}, "ref")
	r.IgnoreRooms = []string{"PORCH"}
	r.Highlight = "v"
	var buf bytes.Buffer
	if err := r.RenderLiveToSVG(&buf, map[string]*LivePosition{"v": {X: 15, Y: 15}}); err != nil {
		t.Fatal(err)
	}
	svg := buf.String()
	if strings.Contains(svg, "#room-2{") {
		t.Error("ignored room has a fill rule")
	}
	if !strings.Contains(svg, "#room-1{fill:var(--tm-room-0)}") {
		t.Error("hall lost its fill")
	}
	if strings.Contains(svg, `class="room alert-room"`) {
		t.Error("ignored room highlighted as alert room")
	}
}

// Live PNG walls come only from the reference map; the composite view
// (Render) still draws every map's walls.
func TestRenderGreyscale_ReferenceWallsOnly(t *testing.T) {
	ref := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "floor", Pixels: cells(0, 0, 20, 20)},
		{Type: "wall", Pixels: cells(20, 0, 1, 20)},
	}}
	other := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "floor", Pixels: cells(0, 0, 30, 20)},
		{Type: "wall", Pixels: cells(10, 0, 1, 20)},
	}}
	maps := map[string]*ValetudoMap{"ref": ref, "other": other}
	tr := map[string]AffineMatrix{"ref": Identity(), "other": Identity()}
	r := NewCompositeRenderer(maps, tr, "ref")
	r.Scale, r.Padding = 4, 0

	img := r.RenderGreyscale()
	if got := nrgbaAt(img, 20*4, 5*4); got != GreyscaleWall {
		t.Errorf("reference wall = %v, want %v", got, GreyscaleWall)
	}
	if got := nrgbaAt(img, 10*4, 5*4); got != GreyscaleFloor {
		t.Errorf("other map's wall = %v, want floor %v", got, GreyscaleFloor)
	}
	// Floor only the other map has is still drawn.
	if got := nrgbaAt(img, 25*4+1, 5*4+1); got != GreyscaleFloor {
		t.Errorf("other map's floor = %v, want %v", got, GreyscaleFloor)
	}

	comp := r.Render()
	b := comp.Bounds()
	compWall := false
	for y := b.Min.Y; y < b.Max.Y && !compWall; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if nrgbaAt(comp, x, y) == r.Colors["other"].Wall {
				compWall = true
				break
			}
		}
	}
	if !compWall {
		t.Error("composite view lost the other map's walls")
	}
}

// The legend shows Names[id] in place of the id.
func TestRenderLive_LegendShowsNames(t *testing.T) {
	m := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{{Type: "floor", Pixels: cells(0, 0, 40, 40)}}}
	newR := func(id string) *CompositeRenderer {
		r := NewCompositeRenderer(map[string]*ValetudoMap{id: m}, map[string]AffineMatrix{id: Identity()}, id)
		r.Scale, r.Padding = 4, 0
		return r
	}
	pos := func(id string) map[string]*LivePosition {
		return map[string]*LivePosition{id: {X: 30, Y: 30, Color: "#ff0000"}}
	}

	named := newR("AmberQuietOtter")
	named.Names = map[string]string{"AmberQuietOtter": "robo5"}
	got := named.RenderLive(pos("AmberQuietOtter"))
	want := newR("robo5").RenderLive(pos("robo5"))
	unnamed := newR("AmberQuietOtter").RenderLive(pos("AmberQuietOtter"))

	legend := image.Rect(0, 0, 160, 30)
	same := func(a, b *image.RGBA) bool {
		for y := legend.Min.Y; y < legend.Max.Y; y++ {
			for x := legend.Min.X; x < legend.Max.X; x++ {
				if a.At(x, y) != b.At(x, y) {
					return false
				}
			}
		}
		return true
	}
	if !same(got, want) {
		t.Error("legend does not show the name")
	}
	if same(got, unnamed) {
		t.Error("legend still shows the id")
	}
}
