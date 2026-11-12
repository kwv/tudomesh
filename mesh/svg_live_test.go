package mesh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// parseHexColor
// ---------------------------------------------------------------------------

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		name string
		hex  string
		want color.RGBA
	}{
		{"with hash", "#FF0000", color.RGBA{255, 0, 0, 255}},
		{"without hash", "00FF00", color.RGBA{0, 255, 0, 255}},
		{"blue", "#0000FF", color.RGBA{0, 0, 255, 255}},
		{"mixed case", "#aaBBcc", color.RGBA{170, 187, 204, 255}},
		{"too short", "#FFF", color.RGBA{255, 0, 0, 255}},        // existing impl defaults to red
		{"empty", "", color.RGBA{255, 0, 0, 255}},                // existing impl defaults to red
		{"invalid chars", "#GGGGGG", color.RGBA{255, 0, 0, 255}}, // existing impl defaults to red
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHexColor(tt.hex)
			if got != tt.want {
				t.Errorf("parseHexColor(%q) = %v, want %v", tt.hex, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RenderLiveToSVG
// ---------------------------------------------------------------------------

func TestRenderLiveToSVG_Basic(t *testing.T) {
	m := &ValetudoMap{
		PixelSize: 5,
		MetaData:  MapMetaData{TotalLayerArea: 100},
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{0, 0, 10, 10, 20, 20},
			},
			{
				Type:   "wall",
				Pixels: []int{0, 0, 0, 1, 0, 2},
			},
		},
	}

	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}

	r := NewVectorRenderer(maps, transforms, "vac1")

	positions := map[string]*LivePosition{
		"vac1": {
			VacuumID:  "vac1",
			X:         25.0,
			Y:         25.0,
			Angle:     45.0,
			Timestamp: time.Now(),
			Color:     "#FF0000",
		},
	}

	var buf bytes.Buffer
	err := r.RenderLiveToSVG(&buf, positions)
	if err != nil {
		t.Fatalf("RenderLiveToSVG failed: %v", err)
	}

	svgContent := buf.String()
	if len(svgContent) == 0 {
		t.Fatal("SVG output is empty")
	}

	// Verify valid SVG structure.
	if !strings.Contains(svgContent, "<svg") {
		t.Error("Output does not contain <svg tag")
	}
	if !strings.HasSuffix(strings.TrimSpace(svgContent), "</svg>") {
		t.Error("SVG does not end with </svg> tag")
	}

	// Verify valid XML.
	var result interface{}
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Errorf("SVG is not valid XML: %v", err)
	}

	// Should contain path elements (floor, wall, vacuum circle, direction line, tag).
	if !strings.Contains(svgContent, "path") {
		t.Error("Output does not contain path elements")
	}

	t.Logf("Live SVG length: %d bytes", len(svgContent))
}

// Valetudo angles are 0 = up, clockwise; the heading line must point that way.
func TestRenderLiveToSVG_HeadingDirection(t *testing.T) {
	m := &ValetudoMap{
		PixelSize: 5,
		MetaData:  MapMetaData{TotalLayerArea: 100},
		Layers:    []MapLayer{{Type: "floor", Pixels: []int{0, 0, 10, 10, 20, 20}}},
	}
	r := NewVectorRenderer(map[string]*ValetudoMap{"vac1": m}, map[string]AffineMatrix{"vac1": Identity()}, "vac1")

	tests := []struct {
		angle  float64
		dx, dy int // expected sign of the line end
	}{
		{0, 0, -1},  // up
		{90, 1, 0},  // right
		{180, 0, 1}, // down
		{270, -1, 0},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		pos := map[string]*LivePosition{"vac1": {X: 25, Y: 25, Angle: tt.angle, Color: "#FF0000"}}
		if err := r.RenderLiveToSVG(&buf, pos); err != nil {
			t.Fatal(err)
		}
		var deg float64
		svg := buf.String()
		i := strings.Index(svg, `class="heading"`)
		if i < 0 {
			t.Fatal("no heading arrow")
		}
		if _, err := fmt.Sscanf(svg[i:], `class="heading" transform="rotate(%f)"`, &deg); err != nil {
			t.Fatalf("parse heading: %v", err)
		}
		// The arrow points +X before rotation.
		x, y := math.Cos(deg*math.Pi/180), math.Sin(deg*math.Pi/180)
		if sign(x) != tt.dx || sign(y) != tt.dy {
			t.Errorf("angle %g: arrow points (%.2f, %.2f), want sign (%d, %d)", tt.angle, x, y, tt.dx, tt.dy)
		}
	}
}

func sign(v float64) int {
	switch {
	case v > 0.01:
		return 1
	case v < -0.01:
		return -1
	}
	return 0
}

func TestRenderLiveToSVG_MultipleVacuums(t *testing.T) {
	// Two maps: vac1 is larger (should be selected as base).
	m1 := &ValetudoMap{
		PixelSize: 5,
		MetaData:  MapMetaData{TotalLayerArea: 500},
		Layers: []MapLayer{
			{Type: "floor", Pixels: []int{0, 0, 100, 0, 100, 100, 0, 100}},
			{Type: "wall", Pixels: []int{0, 0, 100, 0}},
		},
	}
	m2 := &ValetudoMap{
		PixelSize: 5,
		MetaData:  MapMetaData{TotalLayerArea: 200},
		Layers: []MapLayer{
			{Type: "floor", Pixels: []int{10, 10, 50, 50}},
		},
	}

	maps := map[string]*ValetudoMap{"vac1": m1, "vac2": m2}
	transforms := map[string]AffineMatrix{
		"vac1": Identity(),
		"vac2": Identity(),
	}

	r := NewVectorRenderer(maps, transforms, "vac1")

	positions := map[string]*LivePosition{
		"vac1": {
			VacuumID: "vac1", X: 50, Y: 50, Angle: 0,
			Timestamp: time.Now(), Color: "#0000FF",
		},
		"vac2": {
			VacuumID: "vac2", X: 200, Y: 200, Angle: 180,
			Timestamp: time.Now(), Color: "#00FF00",
		},
	}

	var buf bytes.Buffer
	err := r.RenderLiveToSVG(&buf, positions)
	if err != nil {
		t.Fatalf("RenderLiveToSVG failed: %v", err)
	}

	svgContent := buf.String()

	// Valid XML.
	var result interface{}
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Errorf("SVG is not valid XML: %v", err)
	}

	// One group per vacuum.
	if n := strings.Count(svgContent, `class="vacuum`); n != 2 {
		t.Errorf("expected 2 vacuum groups, got %d", n)
	}
}

func TestRenderLiveToSVG_EmptyPositions(t *testing.T) {
	m := &ValetudoMap{
		PixelSize: 5,
		MetaData:  MapMetaData{TotalLayerArea: 100},
		Layers: []MapLayer{
			{Type: "floor", Pixels: []int{0, 0, 10, 10}},
		},
	}

	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}
	r := NewVectorRenderer(maps, transforms, "vac1")

	// Empty positions map - should render base map only.
	var buf bytes.Buffer
	err := r.RenderLiveToSVG(&buf, map[string]*LivePosition{})
	if err != nil {
		t.Fatalf("RenderLiveToSVG with empty positions failed: %v", err)
	}

	svgContent := buf.String()
	if !strings.Contains(svgContent, "<svg") {
		t.Error("Output does not contain <svg tag")
	}

	// Valid XML.
	var result interface{}
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Errorf("SVG is not valid XML: %v", err)
	}
}

func TestRenderLiveToSVG_NoMaps(t *testing.T) {
	r := &VectorRenderer{
		Maps:       map[string]*ValetudoMap{},
		Transforms: map[string]AffineMatrix{},
		Padding:    500.0,
	}

	var buf bytes.Buffer
	err := r.RenderLiveToSVG(&buf, map[string]*LivePosition{})
	if err == nil {
		t.Fatal("Expected error for empty maps, got nil")
	}
	if !strings.Contains(err.Error(), "no maps") {
		t.Errorf("Expected 'no maps' error, got: %v", err)
	}
}

func TestRenderLiveToSVG_GridOverlay(t *testing.T) {
	m := &ValetudoMap{
		PixelSize: 5,
		MetaData:  MapMetaData{TotalLayerArea: 100},
		Layers: []MapLayer{
			{Type: "floor", Pixels: []int{0, 0, 1000, 0, 1000, 1000, 0, 1000}},
		},
	}

	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}
	r := NewVectorRenderer(maps, transforms, "vac1")
	r.GridSpacing = 500.0

	positions := map[string]*LivePosition{
		"vac1": {
			VacuumID: "vac1", X: 2500, Y: 2500, Angle: 90,
			Timestamp: time.Now(), Color: "#FF00FF",
		},
	}

	var buf bytes.Buffer
	err := r.RenderLiveToSVG(&buf, positions)
	if err != nil {
		t.Fatalf("RenderLiveToSVG failed: %v", err)
	}

	svgContent := buf.String()

	// Grid lines should produce dashed strokes.
	if !strings.Contains(svgContent, "stroke-dasharray") {
		t.Error("Live SVG does not contain dashed grid lines")
	}

	t.Logf("Live SVG with grid: %d bytes", len(svgContent))
}
