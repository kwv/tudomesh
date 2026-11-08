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

func TestRenderCompositeToSVG_Structure(t *testing.T) {
	ref := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1", Name: "Kitchen"}, Pixels: cells(0, 0, 10, 10)},
		{Type: "wall", Pixels: []int{10, 0, 10, 1}},
	}, Entities: []MapEntity{{Type: "charger_location", Points: []int{25, 25}}}}
	other := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1", Name: "Kitchen"}, Pixels: cells(0, 0, 10, 10)},
	}, Entities: []MapEntity{{Type: "charger_location", Points: []int{25, 25}}}}
	maps := map[string]*ValetudoMap{"zref": ref, "a": other}
	transforms := map[string]AffineMatrix{"zref": Identity(), "a": Translation(20, 0)}
	r := NewVectorRenderer(maps, transforms, "zref")
	r.Padding, r.GridSpacing = 0, 0

	var buf bytes.Buffer
	if err := r.RenderCompositeToSVG(&buf); err != nil {
		t.Fatal(err)
	}
	var root svgNode
	if err := xml.Unmarshal(buf.Bytes(), &root); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, buf.String())
	}

	groups := root.find(func(n svgNode) bool { return n.attr("class") == "map" })
	if len(groups) != 2 || groups[0].attr("data-id") != "zref" || groups[1].attr("data-id") != "a" {
		t.Fatalf("map groups (reference first): %+v", groups)
	}
	// Ids stay unique when maps share segment ids.
	if n := len(root.find(byID("room-1"))); n != 0 {
		t.Errorf("composite should not emit per-room ids, got %d", n)
	}
	if len(groups[1].find(func(n svgNode) bool { return n.attr("class") == "rooms" })) != 1 {
		t.Error("map group should contain a rooms group")
	}
	// Colors are stylesheet rules, not presentation attributes.
	style := root.find(func(n svgNode) bool { return n.XMLName.Local == "style" })[0].Text
	if !strings.Contains(style, "#map-a .rooms{fill:"+svgColor(r.Colors["a"].Floor)) {
		t.Errorf("missing per-map rule in style:\n%s", style)
	}
	if strings.Contains(buf.String(), ` fill="`) {
		t.Error("composite should not use fill attributes")
	}
	if groups[1].attr("transform") != "matrix(5.00000 0.00000 0.00000 5.00000 100.00 0.00)" {
		t.Errorf("transform for shifted map = %q", groups[1].attr("transform"))
	}

	// Charger entity (25,25) = grid (5,5); the second map is shifted 20 cells.
	chargers := root.find(func(n svgNode) bool { return n.attr("class") == "charger" })
	want := map[string][2]string{"zref": {"25.0", "25.0"}, "a": {"125.0", "25.0"}}
	if len(chargers) != 2 {
		t.Fatalf("chargers = %d, want 2", len(chargers))
	}
	for _, c := range chargers {
		w := want[c.attr("data-id")]
		if c.attr("cx") != w[0] || c.attr("cy") != w[1] {
			t.Errorf("charger %s at (%s,%s), want %v", c.attr("data-id"), c.attr("cx"), c.attr("cy"), w)
		}
	}
}

func TestRenderCompositeToSVG_NoMaps(t *testing.T) {
	r := NewVectorRenderer(map[string]*ValetudoMap{}, nil, "")
	if err := r.RenderCompositeToSVG(&bytes.Buffer{}); err == nil {
		t.Error("expected error for no maps")
	}
}

func TestRenderLiveToSVG_NoPositionsIsFloorplan(t *testing.T) {
	root := renderLive(t, liveTestRenderer(), nil)
	if len(root.find(byID("room-16"))) != 1 {
		t.Error("floorplan should contain rooms")
	}
	if n := len(root.find(func(n svgNode) bool { return strings.HasPrefix(n.attr("class"), "vacuum") })); n != 0 {
		t.Errorf("floorplan should have no vacuums, got %d", n)
	}
}

func TestRenderLiveToSVG_HighlightsRoomByPosition(t *testing.T) {
	r := liveTestRenderer()
	// The vacuum calls its room something else; the room under it is highlighted.
	r.Meta = map[string]VacuumMeta{"rocky7": {Room: "Cuisine"}}
	r.Highlight = "rocky7"
	root := renderLive(t, r, map[string]*LivePosition{"rocky7": {X: 5, Y: 15}})
	if c := root.find(byID("room-17"))[0].attr("class"); c != "room alert-room" {
		t.Errorf("room under the vacuum: class %q", c)
	}
	if c := root.find(byID("room-16"))[0].attr("class"); c != "room" {
		t.Errorf("other room: class %q", c)
	}
}

func TestRenderLiveToSVG_DrawsFloorplan(t *testing.T) {
	r := liveTestRenderer()
	r.Floorplan = &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "40", Name: "Hall"}, Pixels: cells(0, 0, 4, 4)},
	}}
	root := renderLive(t, r, nil)
	if len(root.find(byID("room-40"))) != 1 || len(root.find(byID("room-16"))) != 0 {
		t.Error("live view should draw the floorplan instead of the maps")
	}
}
