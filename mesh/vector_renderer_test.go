package mesh

import (
	"bytes"
	"testing"
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
