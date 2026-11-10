package mesh

import (
	"bytes"
	"fmt"
	"regexp"
	"testing"
)

func TestParseCrop(t *testing.T) {
	ok := map[string]Crop{
		"":      {},
		"room":  {Room: true, Radius: CropDefaultRadius},
		"ROOM":  {Room: true, Radius: CropDefaultRadius},
		"4m":    {Radius: 400},
		"2.5":   {Radius: 250},
		"250cm": {Radius: 250},
	}
	for in, want := range ok {
		got, err := ParseCrop(in)
		if err != nil || got != want {
			t.Errorf("ParseCrop(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"yes", "0", "-1m", "m", "100m", "4km"} {
		if _, err := ParseCrop(in); err == nil {
			t.Errorf("ParseCrop(%q) accepted", in)
		}
	}
}

// cropTestMap is 10 m x 3 m (200 x 60 cells of 5 cm): Kitchen is the left
// 4 m, Hall the rest.
func cropTestMap() map[string]*ValetudoMap {
	return map[string]*ValetudoMap{"ref": {PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1", Name: "Kitchen"}, Pixels: cells(0, 0, 80, 60)},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "2", Name: "Hall"}, Pixels: cells(80, 0, 120, 60)},
	}}}
}

var viewBoxRe = regexp.MustCompile(`viewBox="([-\d.]+) ([-\d.]+) ([-\d.]+) ([-\d.]+)"`)

func svgViewBox(t *testing.T, r *VectorRenderer, pos *LivePosition) string {
	t.Helper()
	var buf bytes.Buffer
	if err := r.RenderLiveToSVG(&buf, map[string]*LivePosition{"v": pos}); err != nil {
		t.Fatal(err)
	}
	m := viewBoxRe.FindStringSubmatch(buf.String())
	if m == nil {
		t.Fatalf("no viewBox in %s", buf.String())
	}
	return fmt.Sprintf("%s %s %s %s", m[1], m[2], m[3], m[4])
}

func TestRenderLiveToSVG_Crop(t *testing.T) {
	newR := func(c Crop) *VectorRenderer {
		r := NewVectorRenderer(cropTestMap(), map[string]AffineMatrix{"ref": Identity()}, "ref")
		r.Highlight, r.Crop, r.Padding = "v", c, 50
		return r
	}
	// Cell origins span 995 x 295 cm; with 50 cm padding the canvas is 1095 x 395.
	cases := []struct {
		name string
		crop Crop
		pos  *LivePosition
		want string
	}{
		{"off", Crop{}, &LivePosition{X: 40, Y: 30}, "0.0 0.0 1095.0 395.0"},
		// Kitchen 0..400 cm plus 1 m each side, shifted inside the canvas.
		{"room", Crop{Room: true, Radius: CropDefaultRadius}, &LivePosition{X: 40, Y: 30}, "0.0 0.0 600.0 395.0"},
		// 3 m around x=500 cm (canvas 550); taller than the map, so clipped.
		{"radius", Crop{Radius: 300}, &LivePosition{X: 100, Y: 30}, "250.0 0.0 600.0 395.0"},
		// Small radius is widened to the 4 m minimum.
		{"min side", Crop{Radius: 50}, &LivePosition{X: 100, Y: 30}, "350.0 0.0 400.0 395.0"},
	}
	for _, c := range cases {
		if got := svgViewBox(t, newR(c.crop), c.pos); got != c.want {
			t.Errorf("%s: viewBox %q, want %q", c.name, got, c.want)
		}
	}
	// No position: nothing to crop around.
	r := newR(Crop{Radius: 300})
	var buf bytes.Buffer
	if err := r.RenderLiveToSVG(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if m := viewBoxRe.FindStringSubmatch(buf.String()); m == nil || m[3] != "1095.0" {
		t.Errorf("no position: viewBox %v, want full width", m)
	}
}

func TestRenderLiveHighlight_PNGCrop(t *testing.T) {
	newR := func(c Crop) *CompositeRenderer {
		r := NewCompositeRenderer(cropTestMap(), map[string]AffineMatrix{"ref": Identity()}, "ref")
		r.Crop = c
		return r
	}
	pos := map[string]*LivePosition{"v": {X: 40, Y: 30, Color: "#000000"}}

	full := newR(Crop{}).RenderLiveHighlight(pos, "v", "").Bounds()
	if full.Dx() != 259 || full.Dy() != 119 {
		t.Fatalf("full image %v, want 259x119", full.Size())
	}
	// Kitchen is image x 30..110; plus 20 px (1 m) each side.
	room := newR(Crop{Room: true, Radius: CropDefaultRadius}).RenderLiveHighlight(pos, "v", "").Bounds()
	if room.Dx() != 120 || room.Dy() != 100 {
		t.Errorf("room crop %v, want 120x100", room.Size())
	}
	// The caption strip is kept at least as wide as its text.
	caption := "rocky7 | Kitchen | error | a long caption"
	img := newR(Crop{Radius: 50}).RenderLiveHighlight(pos, "v", caption).Bounds()
	if want := textWidth(caption) + 16; img.Dx() < want {
		t.Errorf("caption crop width %d, want >= %d", img.Dx(), want)
	}
}
