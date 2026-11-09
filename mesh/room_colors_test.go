package mesh

import (
	"bytes"
	"image/color"
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

// The alert room is the one under the vacuum's position, even when the
// robot's reported room name points elsewhere.
func TestRenderLiveToSVG_AlertRoomByPosition(t *testing.T) {
	r := liveTestRenderer()
	r.Meta = map[string]VacuumMeta{"rocky7": {Name: "rocky7", Room: "Kitchen"}}
	r.Highlight = "rocky7"
	root := renderLive(t, r, map[string]*LivePosition{"rocky7": {X: 5, Y: 15}})

	if c := root.find(byID("room-16"))[0].attr("class"); c != "room" {
		t.Errorf("kitchen class = %q, want room", c)
	}
	if c := root.find(byID("room-17"))[0].attr("class"); c != "room alert-room" {
		t.Errorf("room under vacuum class = %q, want room alert-room", c)
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
		"#rooms .room.alert-room{fill:" + svgColor(AlertRoomColor) + "}",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing CSS %q", want)
		}
	}
}

func TestRenderLiveHighlight_PNGRoomFills(t *testing.T) {
	m := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1", Name: "Kitchen"}, Pixels: cells(0, 0, 20, 10)},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "2", Name: "Hall"}, Pixels: cells(0, 10, 20, 10)},
	}}
	r := NewCompositeRenderer(map[string]*ValetudoMap{"ref": m}, map[string]AffineMatrix{"ref": Identity()}, "ref")
	r.Scale, r.Padding = 4, 0

	at := func(img interface{ At(x, y int) color.Color }, gx, gy int) color.NRGBA {
		return color.NRGBAModel.Convert(img.At(gx*4+1, gy*4+1)).(color.NRGBA)
	}

	plain := r.RenderGreyscale()
	if got := at(plain, 3, 3); got != roomPalette[0] {
		t.Errorf("kitchen = %v, want %v", got, roomPalette[0])
	}
	if got := at(plain, 3, 13); got != roomPalette[1] {
		t.Errorf("hall = %v, want %v", got, roomPalette[1])
	}

	alert := r.RenderLiveHighlight(map[string]*LivePosition{"v": {X: 15, Y: 15, Color: "#000000"}}, "v", "")
	if got := at(alert, 3, 13); got != AlertRoomColor {
		t.Errorf("hall with vacuum = %v, want alert %v", got, AlertRoomColor)
	}
	if got := at(alert, 17, 8); got != roomPalette[0] { // clear of the legend
		t.Errorf("kitchen = %v, want %v", got, roomPalette[0])
	}
}
