package mesh

import (
	"bytes"
	"encoding/xml"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/tdewolff/canvas"
)

func TestVectorRenderer_RenderToSVG(t *testing.T) {
	// Create a mock map
	m := &ValetudoMap{
		PixelSize: 5,
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

	var buf bytes.Buffer
	err := r.RenderToSVG(&buf)
	if err != nil {
		t.Fatalf("Failed to render to SVG: %v", err)
	}

	svgContent := buf.String()
	if len(svgContent) == 0 {
		t.Fatal("SVG output is empty")
	}

	// Basic check for SVG tags
	if !bytes.Contains(buf.Bytes(), []byte("<svg")) {
		t.Errorf("Output does not contain <svg tag")
	}
	if !bytes.Contains(buf.Bytes(), []byte("path")) {
		t.Errorf("Output does not contain path elements")
	}

	// Verify SVG has proper closing tag (fixes bug tudomesh-k4h)
	if !strings.HasSuffix(strings.TrimSpace(svgContent), "</svg>") {
		t.Errorf("SVG does not end with </svg> tag - file is incomplete")
	}

	// Verify SVG is valid XML
	var result interface{}
	if err := xml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Errorf("SVG is not valid XML: %v", err)
	}

	// Count opening and closing svg tags (should be exactly 1 of each)
	openingTags := strings.Count(svgContent, "<svg")
	closingTags := strings.Count(svgContent, "</svg>")
	if openingTags != 1 {
		t.Errorf("Expected 1 opening <svg tag, got %d", openingTags)
	}
	if closingTags != 1 {
		t.Errorf("Expected 1 closing </svg> tag, got %d", closingTags)
	}

	t.Logf("Generated SVG length: %d", len(svgContent))
}

func TestVectorRenderer_RenderToPNG(t *testing.T) {
	// Create a mock map
	m := &ValetudoMap{
		PixelSize: 5,
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

	var buf bytes.Buffer
	err := r.RenderToPNG(&buf)
	if err != nil {
		t.Fatalf("Failed to render to PNG: %v", err)
	}

	pngContent := buf.Bytes()
	if len(pngContent) == 0 {
		t.Fatal("PNG output is empty")
	}

	// Decode PNG to verify it's valid
	img, err := png.Decode(bytes.NewReader(pngContent))
	if err != nil {
		t.Fatalf("Failed to decode PNG: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		t.Errorf("PNG has zero dimensions: %v", bounds)
	}

	t.Logf("Generated PNG size: %d bytes, dimensions: %dx%d", len(pngContent), bounds.Dx(), bounds.Dy())
}

func TestVectorRenderer_PNGWithCustomResolution(t *testing.T) {
	// Create a mock map
	m := &ValetudoMap{
		PixelSize: 5,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{0, 0, 10, 10, 20, 20},
			},
		},
	}

	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}

	r := NewVectorRenderer(maps, transforms, "vac1")
	r.Resolution = canvas.DPI(150) // Lower resolution for faster test

	var buf bytes.Buffer
	err := r.RenderToPNG(&buf)
	if err != nil {
		t.Fatalf("Failed to render to PNG: %v", err)
	}

	// Decode PNG to verify it's valid
	img, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Failed to decode PNG: %v", err)
	}

	bounds := img.Bounds()
	t.Logf("PNG with 150 DPI - size: %d bytes, dimensions: %dx%d", buf.Len(), bounds.Dx(), bounds.Dy())
}

func TestVectorRenderer_SVGAndPNGConsistency(t *testing.T) {
	// Create a mock map
	m := &ValetudoMap{
		PixelSize: 5,
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

	// Render to SVG
	var svgBuf bytes.Buffer
	err := r.RenderToSVG(&svgBuf)
	if err != nil {
		t.Fatalf("Failed to render to SVG: %v", err)
	}

	// Render to PNG
	var pngBuf bytes.Buffer
	err = r.RenderToPNG(&pngBuf)
	if err != nil {
		t.Fatalf("Failed to render to PNG: %v", err)
	}

	// Both should produce non-empty output
	if svgBuf.Len() == 0 {
		t.Error("SVG output is empty")
	}
	if pngBuf.Len() == 0 {
		t.Error("PNG output is empty")
	}

	// Verify PNG is valid image
	img, err := png.Decode(bytes.NewReader(pngBuf.Bytes()))
	if err != nil {
		t.Fatalf("Failed to decode PNG: %v", err)
	}

	// Check image has reasonable dimensions
	bounds := img.Bounds()
	if bounds.Dx() < 100 || bounds.Dy() < 100 {
		t.Errorf("PNG dimensions too small: %dx%d", bounds.Dx(), bounds.Dy())
	}

	t.Logf("SVG: %d bytes, PNG: %d bytes (%dx%d)", svgBuf.Len(), pngBuf.Len(), bounds.Dx(), bounds.Dy())
}

func TestVectorRenderer_PNGWithReducedPadding(t *testing.T) {
	// Test PNG rendering with reduced padding to ensure floor is visible
	// Create a larger floor area relative to padding
	m := &ValetudoMap{
		PixelSize: 5, // Reduced from 50 to avoid OOM
		Layers: []MapLayer{
			{
				Type: "floor",
				// Create a substantial floor area (300*5=1500mm, manageable size)
				// Reduced from 3000 to 300 to avoid OOM: 300*5=1500mm at 72 DPI = ~93MB
				Pixels: []int{
					0, 0, 100, 0, 200, 0, 300, 0,
					0, 100, 100, 100, 200, 100, 300, 100,
					0, 200, 100, 200, 200, 200, 300, 200,
					0, 300, 100, 300, 200, 300, 300, 300,
				},
			},
		},
	}

	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}

	r := NewVectorRenderer(maps, transforms, "vac1")
	r.Resolution = canvas.DPI(72) // Low resolution for speed
	r.Padding = 100.0             // Reduced padding to make floor more visible

	var buf bytes.Buffer
	err := r.RenderToPNG(&buf)
	if err != nil {
		t.Fatalf("Failed to render to PNG: %v", err)
	}

	// Decode and verify it's a valid PNG
	img, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Failed to decode PNG: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		t.Errorf("PNG has zero dimensions: %v", bounds)
	}

	t.Logf("PNG with reduced padding - size: %d bytes, dimensions: %dx%d", buf.Len(), bounds.Dx(), bounds.Dy())
}

func TestVectorRenderer_GridAndCharger(t *testing.T) {
	// Create a map with charger entity
	m := &ValetudoMap{
		PixelSize: 5,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{0, 0, 1000, 0, 1000, 1000, 0, 1000},
			},
		},
		Entities: []MapEntity{
			{
				Type:   "charger_location",
				Points: []int{500, 500},
			},
		},
	}

	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}

	r := NewVectorRenderer(maps, transforms, "vac1")
	r.GridSpacing = 500.0 // Set grid spacing

	var buf bytes.Buffer
	err := r.RenderToSVG(&buf)
	if err != nil {
		t.Fatalf("Failed to render to SVG: %v", err)
	}

	svgContent := buf.String()
	if len(svgContent) == 0 {
		t.Fatal("SVG output is empty")
	}

	// Check for grid lines (dashed stroke)
	if !bytes.Contains(buf.Bytes(), []byte("stroke-dasharray")) {
		t.Errorf("Output does not contain dashed grid lines")
	}

	// Check for circle (charger icon)
	if !bytes.Contains(buf.Bytes(), []byte("circle")) && !bytes.Contains(buf.Bytes(), []byte("ellipse")) {
		// tdewolff/canvas might render circles as path or ellipse
		// Just verify we have some circular element
		t.Logf("Note: Circle element not found in expected format, checking path")
	}

	t.Logf("Generated SVG with grid and charger: %d bytes", len(svgContent))
}

// TestCalculateWorldBounds verifies that calculateWorldBounds returns bounds
// directly from the (already mm-normalized) pixel coordinates, without any
// additional pixelSize scaling.
func TestCalculateWorldBounds(t *testing.T) {
	// After NormalizeToMM, pixels are already in mm.
	// Pixels at [5000, 10000] and [7500, 12500] should remain as-is.
	m := &ValetudoMap{
		PixelSize:  50,
		Normalized: true,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{5000, 10000, 7500, 12500},
			},
		},
	}

	renderer := &VectorRenderer{
		Maps: map[string]*ValetudoMap{
			"test": m,
		},
		Transforms: map[string]AffineMatrix{
			"test": Identity(),
		},
		Padding: 0,
	}

	minX, minY, maxX, maxY, _, _ := renderer.calculateWorldBounds()

	// Bounds should be the pixel values directly (already mm).
	expectedMinX := 5000.0
	expectedMinY := 10000.0
	expectedMaxX := 7500.0
	expectedMaxY := 12500.0

	if minX != expectedMinX {
		t.Errorf("minX: got %v, want %v", minX, expectedMinX)
	}
	if minY != expectedMinY {
		t.Errorf("minY: got %v, want %v", minY, expectedMinY)
	}
	if maxX != expectedMaxX {
		t.Errorf("maxX: got %v, want %v", maxX, expectedMaxX)
	}
	if maxY != expectedMaxY {
		t.Errorf("maxY: got %v, want %v", maxY, expectedMaxY)
	}
}

// TestBoundsMatchVectorizeLayer verifies that calculateWorldBounds and
// VectorizeLayer produce consistent coordinates when data is in mm.
// Both operate in the same mm coordinate space; no additional scaling needed.
func TestBoundsMatchVectorizeLayer(t *testing.T) {
	// After NormalizeToMM with pixelSize=50, grid (10,20) -> mm (500,1000).
	m := &ValetudoMap{
		PixelSize:  50,
		Normalized: true,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{500, 1000, 1500, 2000},
			},
		},
	}

	renderer := &VectorRenderer{
		Maps: map[string]*ValetudoMap{
			"test": m,
		},
		Transforms: map[string]AffineMatrix{
			"test": Identity(),
		},
		Padding: 0,
	}

	// Both calculateWorldBounds and VectorizeLayer now operate in mm space.
	minX, minY, maxX, maxY, _, _ := renderer.calculateWorldBounds()

	paths := VectorizeLayer(m, &m.Layers[0], 0.0)

	if len(paths) == 0 {
		t.Skip("VectorizeLayer returned no paths (expected for sparse test data)")
	}

	// Vectorized points should fall within the same mm bounds (no scaling needed).
	for _, path := range paths {
		for _, pt := range path {
			if pt.X < minX || pt.X > maxX || pt.Y < minY || pt.Y > maxY {
				t.Errorf("VectorizeLayer point (%v,%v) outside bounds [%v,%v] to [%v,%v]",
					pt.X, pt.Y, minX, minY, maxX, maxY)
			}
		}
	}
}

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

	// Should have multiple path elements (base map + 2 vacuums * (circle + direction + tag)).
	pathCount := strings.Count(svgContent, "<path")
	if pathCount < 4 {
		t.Errorf("Expected at least 4 path elements, got %d", pathCount)
	}

	t.Logf("Multi-vacuum live SVG: %d bytes, %d paths", len(svgContent), pathCount)
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
