package mesh

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
)

func TestRoomLabel(t *testing.T) {
	tests := []struct {
		name, id, want string
		ok             bool
	}{
		{"Küche", "1", "Küche", true},
		{"  Salle à manger ", "2", "Salle à manger", true},
		{"", "3", "", false},
		{"4", "4", "", false},
		{"   ", "5", "", false},
	}
	for _, tt := range tests {
		got, ok := roomLabel(MapLayer{Type: "segment", MetaData: LayerMetaData{SegmentID: tt.id, Name: tt.name}})
		if got != tt.want || ok != tt.ok {
			t.Errorf("roomLabel(%q, id %q) = %q, %v; want %q, %v", tt.name, tt.id, got, ok, tt.want, tt.ok)
		}
	}
}

func TestCellMaskPoleLShape(t *testing.T) {
	// An L whose centroid (about 13,13) lies in the missing corner.
	px := append(cells(0, 0, 40, 8), cells(0, 8, 8, 32)...)
	m := newCellMask(px)
	x, y, ok := m.pole()
	if !ok || !m.at(x, y) {
		t.Fatalf("pole (%d,%d) ok=%v is not inside the L", x, y, ok)
	}
	if m.at(13, 13) {
		t.Fatal("test setup: centroid should be outside")
	}
	// Deepest cells are in the 8-wide arms, 4 cells from the edge.
	if x < 2 || x > 37 || y < 2 || y > 37 || (x > 5 && y > 5) {
		t.Errorf("pole (%d,%d) is not well inside an arm", x, y)
	}
}

func TestCellMaskPoleCentersRectangle(t *testing.T) {
	x, y, _ := newCellMask(cells(10, 20, 30, 9)).pole()
	if x < 23 || x > 26 || y != 24 {
		t.Errorf("pole of 30x9 rectangle at (10,20) = (%d,%d), want about (24.5,24)", x, y)
	}
}

func TestCellMaskFilledHoles(t *testing.T) {
	var px []int
	for y := 0; y < 7; y++ {
		for x := 0; x < 7; x++ {
			if (x == 2 || x == 3) && (y == 2 || y == 3) {
				continue // hole
			}
			px = append(px, x, y)
		}
	}
	m := newCellMask(px).filledHoles(func(x, y int) bool { return x == 3 && y == 3 })
	for _, c := range [][2]int{{2, 2}, {3, 2}, {2, 3}} {
		if !m.at(c[0], c[1]) {
			t.Errorf("hole cell %v not filled", c)
		}
	}
	if m.at(3, 3) {
		t.Error("blocked cell was filled")
	}

	// A notch open to the edge is not a hole.
	notch := newCellMask(append(cells(0, 0, 5, 1), 0, 1, 4, 1)).filledHoles(nil)
	if notch.at(2, 1) {
		t.Error("notch open to the edge was filled")
	}
}

// outlineLength sums the lengths of an "M x yhN"/"M x yvN" path.
func outlineLength(t *testing.T, d string) int {
	t.Helper()
	total := 0
	for _, part := range strings.Split(d, "M")[1:] {
		i := strings.IndexAny(part, "hv")
		n, err := strconv.Atoi(part[i+1:])
		if err != nil {
			t.Fatalf("bad segment %q", part)
		}
		total += n
	}
	return total
}

func TestCellOutlinePath(t *testing.T) {
	if got := outlineLength(t, cellOutlinePath(newCellMask(cells(3, 4, 5, 2)))); got != 14 {
		t.Errorf("5x2 outline length = %d, want 14", got)
	}
	// L of three cells: perimeter 8.
	if got := outlineLength(t, cellOutlinePath(newCellMask([]int{0, 0, 1, 0, 0, 1}))); got != 8 {
		t.Errorf("L outline length = %d, want 8", got)
	}
	if d := cellOutlinePath(newCellMask(cells(0, 0, 1, 1))); d != "M0 0h1M0 1h1M0 0v1M1 0v1" {
		t.Errorf("single cell outline = %q", d)
	}
}

// labelTestMap has a named room, an unnamed one, one named by its id, and a
// room too small for its long name.
func labelTestMap() *ValetudoMap {
	return &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1", Name: "Küche"}, Pixels: cells(0, 0, 100, 60)},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "2", Name: ""}, Pixels: cells(0, 60, 100, 60)},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "3", Name: "3"}, Pixels: cells(100, 0, 60, 60)},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "4", Name: "Salle à manger et salon"}, Pixels: cells(100, 60, 20, 20)},
	}}
}

func TestRenderLiveToSVG_RoomLabels(t *testing.T) {
	for _, rot := range []float64{0, 90} {
		t.Run(fmt.Sprint(rot), func(t *testing.T) {
			m := labelTestMap()
			r := NewVectorRenderer(map[string]*ValetudoMap{"ref": m}, map[string]AffineMatrix{"ref": Identity()}, "ref")
			r.GlobalRotation = rot
			root := renderLive(t, r, map[string]*LivePosition{"v": {X: 10, Y: 10}})

			// Order: map (fills, walls, outlines) < labels < vacuums.
			index := map[string]int{}
			for i, c := range root.Children {
				index[c.attr("id")] = i
			}
			if index["map"] >= index["room-labels"] || index["room-labels"] >= index["vacuums"] {
				t.Errorf("group order: %v", index)
			}

			groups := root.find(byID("room-labels"))
			if len(groups) != 1 {
				t.Fatalf("want one #room-labels, got %d", len(groups))
			}
			labels := groups[0].find(func(n svgNode) bool { return n.XMLName.Local == "text" })
			if len(labels) != 1 {
				t.Fatalf("want 1 label (Küche only), got %d: %+v", len(labels), labels)
			}
			l := labels[0]
			if l.attr("class") != "room-label" || l.attr("data-segment-id") != "1" || l.Text != "Küche" {
				t.Errorf("label: class=%q seg=%q text=%q", l.attr("class"), l.attr("data-segment-id"), l.Text)
			}
			if l.attr("transform") != "" {
				t.Errorf("label must not be transformed (stay upright): %q", l.attr("transform"))
			}

			// The label sits inside room 1 (grid 0..100 x 0..60, 5 units per cell).
			x, _ := strconv.ParseFloat(l.attr("x"), 64)
			y, _ := strconv.ParseFloat(l.attr("y"), 64)
			f := newSVGFrame(r.Padding, rot)
			f.growMap(m, Identity())
			f.finish()
			a, b := f.toCanvas(Point{}), f.toCanvas(Point{X: 500, Y: 300})
			if x < min(a[0], b[0]) || x > max(a[0], b[0]) || y < min(a[1], b[1]) || y > max(a[1], b[1]) {
				t.Errorf("label at (%.1f,%.1f) outside room 1 [%v..%v]", x, y, a, b)
			}

			outlines := root.find(func(n svgNode) bool { return n.attr("class") == "room-outline" })
			if len(outlines) != 4 {
				t.Errorf("want 4 room outlines, got %d", len(outlines))
			}
			if len(root.find(byID("map"))[0].find(byID("room-outlines"))) != 1 {
				t.Error("#room-outlines should be inside #map")
			}
		})
	}
}

func TestRenderLiveToSVG_RoomLabelCSS(t *testing.T) {
	r := NewVectorRenderer(map[string]*ValetudoMap{"ref": labelTestMap()}, map[string]AffineMatrix{"ref": Identity()}, "ref")
	var out bytes.Buffer
	if err := r.RenderLiveToSVG(&out, nil); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{".room-label{", ".room-outline{"} {
		if !strings.Contains(out.String(), rule) {
			t.Errorf("style is missing %s", rule)
		}
	}
}

// darkPixels counts near-black pixels (text) in rect.
func darkPixels(img *image.RGBA, rect image.Rectangle) int {
	n := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if c := img.RGBAAt(x, y); c.R < 100 && c.G < 100 && c.B < 100 {
				n++
			}
		}
	}
	return n
}

func TestRenderGreyscale_RoomLabelsAndOutlines(t *testing.T) {
	m := labelTestMap()
	r := NewCompositeRenderer(map[string]*ValetudoMap{"ref": m}, map[string]AffineMatrix{"ref": Identity()}, "ref")
	img := r.RenderGreyscale()
	p := r.Padding // grid cell (x,y) is image pixel (x+p, y+p) at Scale 1

	room := func(x0, y0, w, h int) image.Rectangle {
		return image.Rect(x0+p+1, y0+p+1, x0+w+p-1, y0+h+p-1) // interior, without the outline
	}
	if n := darkPixels(img, room(0, 0, 100, 60)); n < 30 {
		t.Errorf("Küche label: %d text pixels, want a drawn label", n)
	}
	for _, rc := range []image.Rectangle{room(0, 60, 100, 60), room(100, 0, 60, 60), room(100, 60, 20, 20)} {
		if n := darkPixels(img, rc); n != 0 {
			t.Errorf("room %v should have no label, found %d text pixels", rc, n)
		}
	}

	fill := toRGBA(RoomColors(m)["1"])
	edge := color.RGBA{fill.R * 7 / 10, fill.G * 7 / 10, fill.B * 7 / 10, 255}
	if got := img.RGBAAt(p+50, p); got != edge {
		t.Errorf("outline pixel = %v, want %v", got, edge)
	}
	if got := img.RGBAAt(p+5, p+5); got != fill {
		t.Errorf("interior pixel = %v, want fill %v", got, fill)
	}
}

func TestRenderGreyscale_NonASCIILabelHasNoNotdef(t *testing.T) {
	face := newTextFace(pngLabelSize)
	defer func() { _ = face.Close() }()
	for _, r := range "Küche Salle à manger Ελληνικά Кухня" {
		if !face.hasGlyph(r) {
			t.Errorf("bundled font has no glyph for %q", r)
		}
		if _, ok := face.GlyphAdvance(r); !ok {
			t.Errorf("GlyphAdvance(%q) failed", r)
		}
	}

	// "ü" must not draw the same pixels as the notdef box.
	render := func(s string) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 40, 20))
		drawText(img, 2, 15, s, color.RGBA{0, 0, 0, 255})
		return img
	}
	u, notdef := render("ü"), render("￿")
	if string(u.Pix) == string(notdef.Pix) {
		t.Error("ü renders as the notdef glyph")
	}
}

func TestSetFontFile(t *testing.T) {
	t.Cleanup(func() { _ = SetFontFile("") })

	if err := SetFontFile(filepath.Join(t.TempDir(), "missing.ttf")); err == nil {
		t.Error("missing font: want error")
	}
	bad := filepath.Join(t.TempDir(), "bad.ttf")
	if err := os.WriteFile(bad, []byte("not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetFontFile(bad); err == nil {
		t.Error("invalid font: want error")
	}
	if n := len(newTextFace(12).fonts); n != 1 {
		t.Errorf("after failed loads, want only the bundled font, got %d", n)
	}

	mono := filepath.Join(t.TempDir(), "mono.ttf")
	if err := os.WriteFile(mono, gomono.TTF, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetFontFile(mono); err != nil {
		t.Fatal(err)
	}
	face := newTextFace(12)
	if len(face.fonts) != 2 || face.pick('a') != face.faces[0] {
		t.Error("custom font should be tried first")
	}
	// Go Mono and Go Regular differ in width: the custom font is in use.
	wm, _ := face.GlyphAdvance('i')
	wr, _ := face.faces[1].GlyphAdvance('i')
	if wm == wr {
		t.Error("custom font advance equals bundled font; custom font not used")
	}

	if err := SetFontFile(""); err != nil || len(newTextFace(12).fonts) != 1 {
		t.Error("empty path should restore the bundled font only")
	}
}
