package mesh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

type svgNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []svgNode  `xml:",any"`
	Text     string     `xml:",chardata"`
}

func (n svgNode) attr(name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (n svgNode) find(pred func(svgNode) bool) []svgNode {
	var out []svgNode
	if pred(n) {
		out = append(out, n)
	}
	for _, c := range n.Children {
		out = append(out, c.find(pred)...)
	}
	return out
}

func byID(id string) func(svgNode) bool {
	return func(n svgNode) bool { return n.attr("id") == id }
}

// cells returns every grid pixel in a w x h block at (x0,y0).
func cells(x0, y0, w, h int) []int {
	var px []int
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			px = append(px, x, y)
		}
	}
	return px
}

func liveTestRenderer() *VectorRenderer {
	m := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "16", Name: "Kitchen"}, Pixels: cells(0, 0, 20, 10)},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "17", Name: `Kid's "Room" <2>`}, Pixels: cells(0, 10, 20, 10)},
		{Type: "wall", Pixels: []int{20, 0, 20, 1, 20, 2}},
	}}
	maps := map[string]*ValetudoMap{"ref": m}
	return NewVectorRenderer(maps, map[string]AffineMatrix{"ref": Identity()}, "ref")
}

func renderLive(t *testing.T, r *VectorRenderer, positions map[string]*LivePosition) svgNode {
	t.Helper()
	var buf bytes.Buffer
	if err := r.RenderLiveToSVG(&buf, positions); err != nil {
		t.Fatal(err)
	}
	var root svgNode
	if err := xml.Unmarshal(buf.Bytes(), &root); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, buf.String())
	}
	return root
}

func TestRenderLiveToSVG_Structure(t *testing.T) {
	r := liveTestRenderer()
	r.Meta = map[string]VacuumMeta{"rocky7": {Name: "rocky7", Room: "kitchen", State: "error"}}
	r.Highlight = "rocky7"
	root := renderLive(t, r, map[string]*LivePosition{
		"rocky7": {X: 5, Y: 5, Color: "#057dcd"},
		"other":  {X: 15, Y: 15, Color: "#43b0f1"},
	})

	for _, id := range []string{"map", "rooms", "walls", "grid", "vacuums", "room-16", "room-17", "vacuum-rocky7", "vacuum-other"} {
		if len(root.find(byID(id))) != 1 {
			t.Errorf("expected exactly one element with id %q", id)
		}
	}

	kitchen := root.find(byID("room-16"))[0]
	if kitchen.attr("data-room") != "Kitchen" || kitchen.attr("class") != "room alert-room" {
		t.Errorf("kitchen attrs: class=%q room=%q", kitchen.attr("class"), kitchen.attr("data-room"))
	}
	other := root.find(byID("room-17"))[0]
	if other.attr("data-room") != `Kid's "Room" <2>` || other.attr("class") != "room" {
		t.Errorf("escaped room attrs: class=%q room=%q", other.attr("class"), other.attr("data-room"))
	}

	v := root.find(byID("vacuum-rocky7"))[0]
	if v.attr("class") != "vacuum state-error highlight" || v.attr("data-room") != "kitchen" || v.attr("data-state") != "error" {
		t.Errorf("vacuum attrs: %+v", v.Attrs)
	}
	if len(v.find(func(n svgNode) bool { return n.attr("class") == "ring" })) != 1 {
		t.Error("highlighted vacuum should have a ring")
	}
	labels := v.find(func(n svgNode) bool { return n.attr("class") == "label" })
	if len(labels) != 1 || labels[0].Text != "rocky7" {
		t.Errorf("label = %+v", labels)
	}

	plain := root.find(byID("vacuum-other"))[0]
	if plain.attr("class") != "vacuum" || plain.attr("data-name") != "other" {
		t.Errorf("unhighlighted vacuum attrs: %+v", plain.Attrs)
	}
	if len(plain.find(func(n svgNode) bool { return n.attr("class") == "ring" })) != 0 {
		t.Error("unhighlighted vacuum should not have a ring")
	}
}

func TestCellRectsPath(t *testing.T) {
	tests := []struct {
		name   string
		pixels []int
		want   string
	}{
		{"empty", nil, ""},
		{"single cell", []int{3, 4}, "M3 4h1v1h-1z"},
		{"row run", []int{1, 0, 2, 0, 3, 0}, "M1 0h3v1h-3z"},
		{"block merges rows", cells(0, 0, 3, 2), "M0 0h3v2h-3z"},
		{"gap row does not merge", []int{0, 0, 0, 2}, "M0 0h1v1h-1zM0 2h1v1h-1z"},
		{"two runs in a row", []int{0, 0, 2, 0}, "M0 0h1v1h-1zM2 0h1v1h-1z"},
		{"run changes width", []int{0, 0, 1, 0, 0, 1}, "M0 0h2v1h-2zM0 1h1v1h-1z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cellRectsPath(tt.pixels); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCellRectsPathCoversEveryCell(t *testing.T) {
	// L-shape plus a stray cell; total area must equal pixel count.
	px := append(cells(0, 0, 10, 4), cells(0, 4, 3, 6)...)
	px = append(px, 20, 20)
	area := 0
	for _, part := range strings.Split(cellRectsPath(px), "M")[1:] {
		var x, y, w, h, w2 int
		if _, err := fmt.Sscanf(part, "%d %dh%dv%dh-%dz", &x, &y, &w, &h, &w2); err != nil {
			t.Fatalf("parse %q: %v", part, err)
		}
		area += w * h
	}
	if area != len(px)/2 {
		t.Errorf("rect area %d, want %d cells", area, len(px)/2)
	}
}
