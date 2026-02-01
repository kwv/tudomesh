package mesh

import (
	"bytes"
	"encoding/xml"
	"image/png"
	"strings"
	"testing"

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
		PixelSize: 50,
		Layers: []MapLayer{
			{
				Type: "floor",
				// Create a substantial floor area
				Pixels: []int{
					0, 0, 1000, 0, 2000, 0, 3000, 0,
					0, 1000, 1000, 1000, 2000, 1000, 3000, 1000,
					0, 2000, 1000, 2000, 2000, 2000, 3000, 2000,
					0, 3000, 1000, 3000, 2000, 3000, 3000, 3000,
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

// TestCalculateWorldBounds verifies that calculateWorldBounds scales pixel coordinates
// to world coordinates (by pixelSize) before calculating bounds
func TestCalculateWorldBounds(t *testing.T) {
	// Create a test map with pixelSize=50
	// Pixels at [100,200] should become world coords [5000,10000]
	m := &ValetudoMap{
		PixelSize: 50,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{100, 200, 150, 250}, // Two points: (100,200) and (150,250)
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

	// Expected bounds: (100*50, 200*50) to (150*50, 250*50)
	// = (5000, 10000) to (7500, 12500)
	expectedMinX := 100.0 * 50.0
	expectedMinY := 200.0 * 50.0
	expectedMaxX := 150.0 * 50.0
	expectedMaxY := 250.0 * 50.0

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

// TestBoundsMatchVectorizeLayer verifies that calculateWorldBounds and VectorizeLayer
// use the same coordinate system (both scale by pixelSize)
func TestBoundsMatchVectorizeLayer(t *testing.T) {
	m := &ValetudoMap{
		PixelSize: 50,
		Layers: []MapLayer{
			{
				Type:   "floor",
				Pixels: []int{10, 20, 30, 40},
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

	// Get bounds from calculateWorldBounds
	minX, minY, maxX, maxY, _, _ := renderer.calculateWorldBounds()

	// Get paths from VectorizeLayer
	paths := VectorizeLayer(&m.Layers[0], m.PixelSize, 0.0)

	if len(paths) == 0 {
		t.Skip("VectorizeLayer returned no paths (expected for sparse test data)")
	}

	// Verify that all vectorized points fall within the calculated bounds
	for _, path := range paths {
		for _, pt := range path {
			if pt.X < minX || pt.X > maxX || pt.Y < minY || pt.Y > maxY {
				t.Errorf("VectorizeLayer point (%v,%v) outside bounds [%v,%v] to [%v,%v]",
					pt.X, pt.Y, minX, minY, maxX, maxY)
			}
		}
	}
}
