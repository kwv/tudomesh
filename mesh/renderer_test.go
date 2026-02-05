package mesh

import (
	"image/color"
	"testing"
)

// Helper to create a simple mock map
func createMockMap(wallPixels []int, floorPixels []int) *ValetudoMap {
	layers := []MapLayer{}
	if len(wallPixels) > 0 {
		layers = append(layers, MapLayer{
			Type:   "wall",
			Pixels: wallPixels,
		})
	}
	if len(floorPixels) > 0 {
		layers = append(layers, MapLayer{
			Type:   "floor",
			Pixels: floorPixels,
		})
	}

	// Create map with basic metadata
	return &ValetudoMap{
		MetaData: MapMetaData{Version: 2},
		Size:     Size{X: 100, Y: 100},
		Layers:   layers,
		Entities: []MapEntity{}, // Empty entities for now
	}
}

func TestHasDrawableContent(t *testing.T) {
	// Case 1: no maps
	r := &CompositeRenderer{Maps: map[string]*ValetudoMap{}}
	if r.HasDrawableContent() {
		t.Fatalf("expected no drawable content when maps empty")
	}

	// Case 2: map present but layers empty
	m1 := &ValetudoMap{Layers: []MapLayer{}}
	r = &CompositeRenderer{Maps: map[string]*ValetudoMap{"vac1": m1}}
	if r.HasDrawableContent() {
		t.Fatalf("expected no drawable content when layers empty")
	}

	// Case 3: map with a floor layer but zero pixels
	m2 := &ValetudoMap{Layers: []MapLayer{{Type: "floor", Pixels: []int{}}, {Type: "wall", Pixels: []int{}}}}
	r = &CompositeRenderer{Maps: map[string]*ValetudoMap{"vac2": m2}}
	if r.HasDrawableContent() {
		t.Fatalf("expected no drawable content when layers have zero pixels")
	}

	// Case 4: map with a floor layer with pixels
	m3 := &ValetudoMap{Layers: []MapLayer{{Type: "floor", Pixels: []int{1, 2}}}}
	r = &CompositeRenderer{Maps: map[string]*ValetudoMap{"vac3": m3}}
	if !r.HasDrawableContent() {
		t.Fatalf("expected drawable content when a layer contains pixels")
	}
}

func TestCompositeRenderer_Initialization(t *testing.T) {
	maps := map[string]*ValetudoMap{
		"vac1": createMockMap(nil, nil),
		"vac2": createMockMap(nil, nil),
	}
	transforms := map[string]AffineMatrix{
		"vac1": Identity(),
		"vac2": Identity(),
	}

	renderer := NewCompositeRenderer(maps, transforms, "vac1")

	if renderer.Reference != "vac1" {
		t.Errorf("Reference ID not set correctly")
	}

	if len(renderer.Colors) != 2 {
		t.Errorf("Expected 2 colors assigned, got %d", len(renderer.Colors))
	}

	// Check if reference vac1 gets first color (Blue)
	defaults := DefaultColors()
	if renderer.Colors["vac1"] != defaults[0] {
		t.Errorf("Reference vacuum did not get default color 0")
	}
}

func TestCalculateBounds(t *testing.T) {
	// Map with points from (10,10) to (20,20)
	// Pixels: [10,10, 20,20]
	m := createMockMap([]int{10, 10, 20, 20}, nil)

	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}

	renderer := NewCompositeRenderer(maps, transforms, "vac1")

	minX, minY, maxX, maxY, _, _ := renderer.CalculateBounds()

	if minX != 10 || minY != 10 {
		t.Errorf("Expected min bounds (10,10), got (%f,%f)", minX, minY)
	}
	if maxX != 20 || maxY != 20 {
		t.Errorf("Expected max bounds (20,20), got (%f,%f)", maxX, maxY)
	}
}

func TestCalculateBounds_WithTransform(t *testing.T) {
	// Map points (0,0) and (10,0)
	m := createMockMap([]int{0, 0, 10, 0}, nil)
	maps := map[string]*ValetudoMap{"vac1": m}

	// Translate by (50, 50)
	tx := Translation(50, 50)
	transforms := map[string]AffineMatrix{"vac1": tx}

	renderer := NewCompositeRenderer(maps, transforms, "vac1")

	minX, minY, maxX, maxY, _, _ := renderer.CalculateBounds()

	// Expected: (50,50) to (60,50)
	if minX != 50 || minY != 50 {
		t.Errorf("Expected min bounds (50,50), got (%f,%f)", minX, minY)
	}
	if maxX != 60 || maxY != 50 {
		t.Errorf("Expected max bounds (60,50), got (%f,%f)", maxX, maxY)
	}
}

func TestRender_Content(t *testing.T) {
	// Create a map with a single wall pixel at (50,50)
	m := createMockMap([]int{50, 50}, nil)
	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}

	renderer := NewCompositeRenderer(maps, transforms, "vac1")
	// Reduce padding to simplify coord checks
	renderer.Padding = 0
	renderer.Scale = 1.0

	_ = renderer.Render() // Initial render with single point

	// Use 2 points to give it dimension. (0,0) and (10,10)
	m = createMockMap([]int{0, 0, 10, 10}, nil)
	renderer.Maps["vac1"] = m

	img := renderer.Render()

	// Check if pixel at (0,0) relative to image is colored.
	// World (0,0) -> Image (0,0) (since minX=0, minY=0, padding=0)
	// Color should be Wall color for vac1 (Blue)

	c := img.RGBAAt(0, 0)
	vacColor := renderer.Colors["vac1"].Wall

	// Wall is drawn as block
	if c != color.RGBA(vacColor) {
		t.Errorf("Expected pixel at (0,0) to be colored %v, got %v", vacColor, c)
	}
}

func TestRender_GlobalRotation(t *testing.T) {
	// Points at (0,0) and (10,0) -> Horizontal line length 10
	m := createMockMap([]int{0, 0, 10, 0}, nil)
	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}

	renderer := NewCompositeRenderer(maps, transforms, "vac1")
	renderer.GlobalRotation = 90 // Rotate 90 degrees CCW
	renderer.Padding = 0

	// Original bounds: (0,0) x (10,0). Center (5,0).
	// Rotated 90 deg around (5,0).
	// (0,0) -> (-5, 0) -> rot(-5,0) -> (0, -5) -> shift back -> (5, -5)
	// (10,0) -> (5, 0) -> rot(5,0) -> (0, 5) -> shift back -> (5, 5)

	// Bounds should be roughly (5,-5) to (5,5).
	// Width = 0, Height = 10.

	img := renderer.Render()

	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	// Allow for some floating point fuzziness in bounds calc, but conceptually height > width
	if width > height {
		t.Errorf("Expected portrait orientation (height > width) for 90 deg rotation. Got %dx%d", width, height)
	}
}

func TestBlendColors(t *testing.T) {
	// Case 1: Opaque foreground over opaque background
	bg := color.RGBA{R: 255, G: 255, B: 255, A: 255} // White
	fg := color.NRGBA{R: 0, G: 0, B: 0, A: 255}      // Black
	result := blendColors(bg, fg)

	// Should be fully black
	if result.R != 0 || result.G != 0 || result.B != 0 || result.A != 255 {
		t.Errorf("Expected Black, got %v", result)
	}

	// Case 2: 50% transparent Black over White
	fg = color.NRGBA{R: 0, G: 0, B: 0, A: 128} // ~50% Black
	result = blendColors(bg, fg)

	// Should be grey ~127
	// Formula: fg*alpha + bg*(1-alpha)
	// 0*0.5 + 255*0.5 = 127.5
	if result.R < 125 || result.R > 130 {
		t.Errorf("Expected Grey (~127), got %v", result)
	}

	// Case 3: Fully transparent foreground
	fg = color.NRGBA{R: 0, G: 0, B: 0, A: 0}
	result = blendColors(bg, fg)

	// Should match background
	if result.R != 255 || result.G != 255 || result.B != 255 {
		t.Errorf("Expected White, got %v", result)
	}
}

func TestApplyGlobalRotation(t *testing.T) {
	renderer := &CompositeRenderer{GlobalRotation: 90}

	p := Point{X: 10, Y: 0}
	center := Point{X: 0, Y: 0} // Rotation around origin

	rotated := renderer.applyGlobalRotation(p, center.X, center.Y)

	// (10,0) rotated 90 deg CCW -> (0, 10)
	// Floating point checks
	if rotated.X > 0.001 || rotated.X < -0.001 {
		t.Errorf("Expected X close to 0, got %f", rotated.X)
	}
	if rotated.Y < 9.999 || rotated.Y > 10.001 {
		t.Errorf("Expected Y close to 10, got %f", rotated.Y)
	}
}

func TestRender_Layers(t *testing.T) {
	// Verify that walls overwrite floors
	// Place floor and wall at same location (10,10)
	floorLayer := MapLayer{Type: "floor", Pixels: []int{10, 10}}
	wallLayer := MapLayer{Type: "wall", Pixels: []int{10, 10}}

	m := &ValetudoMap{
		MetaData: MapMetaData{Version: 2},
		Size:     Size{X: 100, Y: 100},
		Layers:   []MapLayer{floorLayer, wallLayer},
	}

	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}

	renderer := NewCompositeRenderer(maps, transforms, "vac1")
	renderer.Padding = 0
	renderer.Scale = 1.0

	img := renderer.Render()

	// (10,10) is min bounds. So (0,0) in image.
	c := img.RGBAAt(0, 0)

	// Should be wall color, not floor color (blended) or background
	wallColor := renderer.Colors["vac1"].Wall

	if c != color.RGBA(wallColor) {
		t.Errorf("Expected Wall color %v, got %v", wallColor, c)
	}
}

func TestRender_Entities(t *testing.T) {
	// Create map with charger at (10,10) and robot at (20,20)
	// Need to check how ExtractChargerPosition works.
	// It usually looks for specific entities in MapData.Entities

	chargerEntity := MapEntity{
		Type:   "charger_location",
		Points: []int{10, 10},
	}
	robotEntity := MapEntity{
		Type:   "robot_position",
		Points: []int{20, 20},
	}

	m := &ValetudoMap{
		MetaData: MapMetaData{Version: 2},
		Size:     Size{X: 100, Y: 100},
		Layers:   []MapLayer{{Type: "floor", Pixels: []int{10, 10, 20, 20}}}, // Need some bounds
		Entities: []MapEntity{chargerEntity, robotEntity},
	}

	maps := map[string]*ValetudoMap{"vac1": m}
	transforms := map[string]AffineMatrix{"vac1": Identity()}

	renderer := NewCompositeRenderer(maps, transforms, "vac1")
	renderer.Padding = 0
	renderer.Scale = 1.0

	img := renderer.Render()

	// Check Charger (Gold) at relative (0,0) [World 10,10]
	// Bounds min is (10,10). So (10,10) -> (0,0).
	c1 := img.RGBAAt(0, 0)
	gold := color.RGBA{255, 215, 0, 255}
	if c1 != gold {
		t.Errorf("Expected Charger (Gold) at charger loc, got %v", c1)
	}

	// Check Robot (Blue for vac1) at relative (10,10) [World 20,20]
	// (20-10) = 10.
	c2 := img.RGBAAt(10, 10)
	robotColor := renderer.Colors["vac1"].Robot
	expected := color.RGBA(robotColor)

	// The robot is a circle radius 6. Center (10,10) should be colored.
	if c2 != expected {
		t.Errorf("Expected Robot color at robot loc, got %v", c2)
	}
}
