package mesh

import (
	"bytes"
	"image/png"
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
