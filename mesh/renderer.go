package mesh

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// VacuumColor defines the color for each vacuum's map elements
type VacuumColor struct {
	Floor color.NRGBA
	Wall  color.NRGBA
	Robot color.NRGBA
}

// DefaultColors returns distinct colors for up to 4 vacuums
func DefaultColors() []VacuumColor {
	return []VacuumColor{
		{ // Reference - Blue
			Floor: color.NRGBA{100, 149, 237, 180}, // Cornflower blue
			Wall:  color.NRGBA{0, 0, 139, 255},     // Dark blue
			Robot: color.NRGBA{0, 0, 255, 255},     // Blue
		},
		{ // Vacuum 2 - Red
			Floor: color.NRGBA{255, 99, 71, 150}, // Tomato
			Wall:  color.NRGBA{139, 0, 0, 255},   // Dark red
			Robot: color.NRGBA{255, 0, 0, 255},   // Red
		},
		{ // Vacuum 3 - Green
			Floor: color.NRGBA{144, 238, 144, 150}, // Light green
			Wall:  color.NRGBA{0, 100, 0, 255},     // Dark green
			Robot: color.NRGBA{0, 255, 0, 255},     // Green
		},
		{ // Vacuum 4 - Yellow
			Floor: color.NRGBA{255, 255, 150, 150}, // Light yellow
			Wall:  color.NRGBA{184, 134, 11, 255},  // Dark goldenrod
			Robot: color.NRGBA{255, 215, 0, 255},   // Gold
		},
	}
}

// CompositeRenderer renders multiple vacuum maps into a single image
type CompositeRenderer struct {
	Maps           map[string]*ValetudoMap
	Transforms     map[string]AffineMatrix
	Colors         map[string]VacuumColor
	Reference      string
	Scale          float64 // Pixels per map unit (default 0.1 = 10 map units per pixel)
	Padding        int     // Padding around the image
	GlobalRotation float64 // Rotate entire output (0, 90, 180, 270 degrees CCW)
}

// NewCompositeRenderer creates a renderer with default settings
func NewCompositeRenderer(maps map[string]*ValetudoMap, transforms map[string]AffineMatrix, reference string) *CompositeRenderer {
	colors := DefaultColors()
	colorMap := make(map[string]VacuumColor)

	i := 0
	for id := range maps {
		if id == reference {
			colorMap[id] = colors[0] // Reference gets first color
		} else {
			colorMap[id] = colors[(i%3)+1] // Others get remaining colors
			i++
		}
	}

	return &CompositeRenderer{
		Maps:           maps,
		Transforms:     transforms,
		Colors:         colorMap,
		Reference:      reference,
		Scale:          1.0, // 1:1 map units to pixels
		Padding:        30,
		GlobalRotation: 0,
	}
}

// HasDrawableContent returns true if any map contains drawable pixels in floor/segment/wall layers.
func (r *CompositeRenderer) HasDrawableContent() bool {
	for _, m := range r.Maps {
		for _, layer := range m.Layers {
			if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
				if len(layer.Pixels) > 0 {
					return true
				}
			}
		}
	}
	return false
}

// applyGlobalRotation rotates a point around the center by the global rotation angle
func (r *CompositeRenderer) applyGlobalRotation(p Point, centerX, centerY float64) Point {
	if r.GlobalRotation == 0 {
		return p
	}

	// Translate to origin
	x := p.X - centerX
	y := p.Y - centerY

	// Rotate CCW
	rad := r.GlobalRotation * math.Pi / 180
	cos := math.Cos(rad)
	sin := math.Sin(rad)

	newX := x*cos - y*sin
	newY := x*sin + y*cos

	// Translate back
	return Point{X: newX + centerX, Y: newY + centerY}
}

// CalculateBounds computes the bounding box of all transformed maps
func (r *CompositeRenderer) CalculateBounds() (minX, minY, maxX, maxY, centerX, centerY float64) {
	minX, minY = math.MaxFloat64, math.MaxFloat64
	maxX, maxY = -math.MaxFloat64, -math.MaxFloat64

	// First pass: get bounds without global rotation to find center
	for id, m := range r.Maps {
		transform := r.Transforms[id]

		for _, layer := range m.Layers {
			if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
				points := PixelsToPoints(layer.Pixels)
				for _, p := range points {
					tp := TransformPoint(p, transform)
					if tp.X < minX {
						minX = tp.X
					}
					if tp.Y < minY {
						minY = tp.Y
					}
					if tp.X > maxX {
						maxX = tp.X
					}
					if tp.Y > maxY {
						maxY = tp.Y
					}
				}
			}
		}
	}

	// Calculate center
	centerX = (minX + maxX) / 2
	centerY = (minY + maxY) / 2

	// If we have global rotation, recalculate bounds with rotation applied
	if r.GlobalRotation != 0 {
		minX, minY = math.MaxFloat64, math.MaxFloat64
		maxX, maxY = -math.MaxFloat64, -math.MaxFloat64

		for id, m := range r.Maps {
			transform := r.Transforms[id]

			for _, layer := range m.Layers {
				if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
					points := PixelsToPoints(layer.Pixels)
					for _, p := range points {
						tp := TransformPoint(p, transform)
						tp = r.applyGlobalRotation(tp, centerX, centerY)
						if tp.X < minX {
							minX = tp.X
						}
						if tp.Y < minY {
							minY = tp.Y
						}
						if tp.X > maxX {
							maxX = tp.X
						}
						if tp.Y > maxY {
							maxY = tp.Y
						}
					}
				}
			}
		}
	}

	return
}

// Render creates the composite image
func (r *CompositeRenderer) Render() *image.RGBA {
	// Calculate bounds
	minX, minY, maxX, maxY, centerX, centerY := r.CalculateBounds()

	// Calculate image dimensions
	width := int((maxX-minX)*r.Scale) + 2*r.Padding
	height := int((maxY-minY)*r.Scale) + 2*r.Padding

	// Limit size
	if width > 4000 {
		r.Scale *= float64(4000) / float64(width)
		width = 4000
		height = int((maxY-minY)*r.Scale) + 2*r.Padding
	}
	if height > 4000 {
		r.Scale *= float64(4000) / float64(height)
		height = 4000
		width = int((maxX-minX)*r.Scale) + 2*r.Padding
	}

	// If bounds are invalid (e.g., no map data), ensure positive, reasonable dimensions
	if width <= 0 || height <= 0 {
		// No drawable area; create a minimal image (include padding if set)
		minSize := 1
		if 2*r.Padding+1 > minSize {
			minSize = 2*r.Padding + 1
		}
		if width <= 0 {
			width = minSize
		}
		if height <= 0 {
			height = minSize
		}
	}

	// Create image with white background
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{240, 240, 240, 255})
		}
	}

	// Helper to convert world coords to image coords (with global rotation)
	toImage := func(p Point) (int, int) {
		// Apply global rotation around center
		rp := r.applyGlobalRotation(p, centerX, centerY)
		x := int((rp.X-minX)*r.Scale) + r.Padding
		y := int((rp.Y-minY)*r.Scale) + r.Padding
		return x, y
	}

	// Render each vacuum's map
	// First pass: floors/segments (semi-transparent)
	for id, m := range r.Maps {
		transform := r.Transforms[id]
		vc := r.Colors[id]

		for _, layer := range m.Layers {
			if layer.Type == "floor" || layer.Type == "segment" {
				points := PixelsToPoints(layer.Pixels)
				for _, p := range points {
					tp := TransformPoint(p, transform)
					ix, iy := toImage(tp)
					if ix >= 0 && ix < width && iy >= 0 && iy < height {
						// Alpha blend with existing color
						existing := img.RGBAAt(ix, iy)
						blended := blendColors(existing, vc.Floor)
						img.Set(ix, iy, blended)
					}
				}
			}
		}
	}

	// Second pass: walls (opaque)
	for id, m := range r.Maps {
		transform := r.Transforms[id]
		vc := r.Colors[id]

		for _, layer := range m.Layers {
			if layer.Type == "wall" {
				points := PixelsToPoints(layer.Pixels)
				for _, p := range points {
					tp := TransformPoint(p, transform)
					ix, iy := toImage(tp)
					// Draw wall as 2x2 block for visibility
					for dx := -1; dx <= 1; dx++ {
						for dy := -1; dy <= 1; dy++ {
							px, py := ix+dx, iy+dy
							if px >= 0 && px < width && py >= 0 && py < height {
								img.Set(px, py, vc.Wall)
							}
						}
					}
				}
			}
		}
	}

	// Third pass: chargers and robots
	for id, m := range r.Maps {
		transform := r.Transforms[id]
		vc := r.Colors[id]

		// Draw charger as square
		if charger, ok := ExtractChargerPosition(m); ok {
			tc := TransformPoint(charger, transform)
			ix, iy := toImage(tc)
			drawSquare(img, ix, iy, 8, color.RGBA{255, 215, 0, 255}) // Gold charger
		}

		// Draw robot as circle
		if robot, _, ok := ExtractRobotPosition(m); ok {
			tr := TransformPoint(robot, transform)
			ix, iy := toImage(tr)
			// Convert NRGBA to RGBA for image rendering
			robotRGBA := color.RGBA{vc.Robot.R, vc.Robot.G, vc.Robot.B, vc.Robot.A}
			drawCircle(img, ix, iy, 6, robotRGBA)
		}
	}

	// Add legend
	r.drawLegend(img, width, height)

	return img
}

// SavePNG saves the composite image to a file
func (r *CompositeRenderer) SavePNG(path string) error {
	img := r.Render()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	return png.Encode(f, img)
}

// RenderSingleMap renders a single vacuum map to PNG
func RenderSingleMap(m *ValetudoMap, outputPath string, floorColor, wallColor color.RGBA) error {
	return RenderSingleMapWithRotation(m, outputPath, floorColor, wallColor, 0)
}

// RenderSingleMapWithRotation renders a single vacuum map with rotation applied
func RenderSingleMapWithRotation(m *ValetudoMap, outputPath string, floorColor, wallColor color.RGBA, rotationDeg float64) error {
	// Build rotation transform around centroid
	features := ExtractFeatures(m)
	centroid := features.Centroid

	// Create transform: translate to origin, rotate, translate back
	toOrigin := Translation(-centroid.X, -centroid.Y)
	rotate := RotationDeg(rotationDeg)
	fromOrigin := Translation(centroid.X, centroid.Y)
	transform := MultiplyMatrices(fromOrigin, MultiplyMatrices(rotate, toOrigin))

	// Find bounds after rotation
	var minX, minY, maxX, maxY = math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64

	for _, layer := range m.Layers {
		if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
			points := PixelsToPoints(layer.Pixels)
			for _, p := range points {
				tp := TransformPoint(p, transform)
				if tp.X < minX {
					minX = tp.X
				}
				if tp.Y < minY {
					minY = tp.Y
				}
				if tp.X > maxX {
					maxX = tp.X
				}
				if tp.Y > maxY {
					maxY = tp.Y
				}
			}
		}
	}

	scale := 1.0
	padding := 30
	width := int((maxX-minX)*scale) + 2*padding
	height := int((maxY-minY)*scale) + 2*padding

	// Create image
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{240, 240, 240, 255})
		}
	}

	toImage := func(p Point) (int, int) {
		tp := TransformPoint(p, transform)
		x := int((tp.X-minX)*scale) + padding
		y := int((tp.Y-minY)*scale) + padding
		return x, y
	}

	// Draw floor/segments
	for _, layer := range m.Layers {
		if layer.Type == "floor" || layer.Type == "segment" {
			points := PixelsToPoints(layer.Pixels)
			for _, p := range points {
				ix, iy := toImage(p)
				if ix >= 0 && ix < width && iy >= 0 && iy < height {
					img.Set(ix, iy, floorColor)
				}
			}
		}
	}

	// Draw walls
	for _, layer := range m.Layers {
		if layer.Type == "wall" {
			points := PixelsToPoints(layer.Pixels)
			for _, p := range points {
				ix, iy := toImage(p)
				for dx := -1; dx <= 1; dx++ {
					for dy := -1; dy <= 1; dy++ {
						px, py := ix+dx, iy+dy
						if px >= 0 && px < width && py >= 0 && py < height {
							img.Set(px, py, wallColor)
						}
					}
				}
			}
		}
	}

	// Draw charger
	if charger, ok := ExtractChargerPosition(m); ok {
		ix, iy := toImage(charger)
		drawSquare(img, ix, iy, 8, color.RGBA{255, 215, 0, 255})
	}

	// Draw robot
	if robot, _, ok := ExtractRobotPosition(m); ok {
		ix, iy := toImage(robot)
		drawCircle(img, ix, iy, 6, color.RGBA{255, 0, 0, 255})
	}

	// Draw origin (0,0) as purple triangle
	ox, oy := toImage(Point{X: 0, Y: 0})
	drawTriangle(img, ox, oy, 12, color.RGBA{128, 0, 128, 255}) // Purple

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	return png.Encode(f, img)
}

// blendColors performs alpha blending of two colors
func blendColors(bg color.RGBA, fg color.NRGBA) color.NRGBA {
	// Convert RGBA background to NRGBA for proper blending
	// RGBA is premultiplied, so we need to un-premultiply it first
	var bgNRGBA color.NRGBA
	switch bg.A {
	case 0:
		bgNRGBA = color.NRGBA{0, 0, 0, 0}
	case 255:
		bgNRGBA = color.NRGBA{bg.R, bg.G, bg.B, 255}
	default:
		// Un-premultiply: divide RGB by alpha
		alpha32 := uint32(bg.A)
		bgNRGBA = color.NRGBA{
			R: uint8((uint32(bg.R) * 255) / alpha32),
			G: uint8((uint32(bg.G) * 255) / alpha32),
			B: uint8((uint32(bg.B) * 255) / alpha32),
			A: bg.A,
		}
	}

	// Now perform standard alpha blending with non-premultiplied colors
	alpha := float64(fg.A) / 255.0
	invAlpha := 1.0 - alpha

	return color.NRGBA{
		R: uint8(float64(fg.R)*alpha + float64(bgNRGBA.R)*invAlpha),
		G: uint8(float64(fg.G)*alpha + float64(bgNRGBA.G)*invAlpha),
		B: uint8(float64(fg.B)*alpha + float64(bgNRGBA.B)*invAlpha),
		A: 255,
	}
}

// drawCircle draws a filled circle
func drawCircle(img *image.RGBA, cx, cy, radius int, c color.RGBA) {
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if dx*dx+dy*dy <= radius*radius {
				x, y := cx+dx, cy+dy
				if x >= 0 && x < img.Bounds().Max.X && y >= 0 && y < img.Bounds().Max.Y {
					img.Set(x, y, c)
				}
			}
		}
	}
}

// drawSquare draws a filled square
func drawSquare(img *image.RGBA, cx, cy, size int, c color.RGBA) {
	half := size / 2
	for dy := -half; dy <= half; dy++ {
		for dx := -half; dx <= half; dx++ {
			x, y := cx+dx, cy+dy
			if x >= 0 && x < img.Bounds().Max.X && y >= 0 && y < img.Bounds().Max.Y {
				img.Set(x, y, c)
			}
		}
	}
}

// drawTriangle draws a filled triangle pointing up
func drawTriangle(img *image.RGBA, cx, cy, size int, c color.RGBA) {
	half := size / 2
	for dy := -half; dy <= half; dy++ {
		// Width of triangle at this row
		progress := float64(dy+half) / float64(size)
		width := int(progress * float64(half))
		for dx := -width; dx <= width; dx++ {
			x, y := cx+dx, cy+dy
			if x >= 0 && x < img.Bounds().Max.X && y >= 0 && y < img.Bounds().Max.Y {
				img.Set(x, y, c)
			}
		}
	}
}

// drawLegend adds a legend with text labels to the image
func (r *CompositeRenderer) drawLegend(img *image.RGBA, width, height int) {
	// Sort vacuum IDs for consistent ordering
	ids := make([]string, 0, len(r.Maps))
	for id := range r.Maps {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Legend in top-left corner
	y := 15
	for _, id := range ids {
		vc := r.Colors[id]

		// Draw color swatch (12x12 square)
		for dy := 0; dy < 12; dy++ {
			for dx := 0; dx < 12; dx++ {
				img.Set(10+dx, y+dy-6, vc.Wall)
			}
		}

		drawText(img, 28, y, id, color.RGBA{0, 0, 0, 255})

		y += 18
	}
}

// drawText renders text onto an image at the specified position
func drawText(img *image.RGBA, x, y int, text string, c color.RGBA) {
	face := basicfont.Face7x13
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	d.DrawString(text)
}

// RenderCompositeMap is a convenience function to render all maps with ICP alignment
func RenderCompositeMap(maps map[string]*ValetudoMap, outputPath string, referenceOverride string, globalRotation float64) error {
	if len(maps) < 2 {
		return nil
	}

	// Select reference
	refID := referenceOverride
	if refID == "" {
		refID = SelectReferenceVacuum(maps, nil)
	}

	// Compute transforms
	transforms := make(map[string]AffineMatrix)
	transforms[refID] = Identity()

	refMap := maps[refID]
	config := DefaultICPConfig()

	for id, m := range maps {
		if id == refID {
			continue
		}
		result := AlignMaps(m, refMap, config)
		transforms[id] = result.Transform
	}

	// Render
	renderer := NewCompositeRenderer(maps, transforms, refID)
	renderer.GlobalRotation = globalRotation
	return renderer.SavePNG(outputPath)
}

// RenderWithForcedRotation renders with a specific rotation override for a vacuum
func RenderWithForcedRotation(maps map[string]*ValetudoMap, vacuumID string, rotationDeg float64, outputPath string, referenceOverride string, globalRotation float64) error {
	if len(maps) < 2 {
		return nil
	}

	refID := referenceOverride
	if refID == "" {
		refID = SelectReferenceVacuum(maps, nil)
	}
	refMap := maps[refID]

	transforms := make(map[string]AffineMatrix)
	transforms[refID] = Identity()

	for id, m := range maps {
		if id == refID {
			continue
		}

		if id == vacuumID {
			// Use forced rotation
			srcFeatures := ExtractFeatures(m)
			tgtFeatures := ExtractFeatures(refMap)
			transforms[id] = buildInitialTransform(srcFeatures, tgtFeatures, rotationDeg)
		} else {
			// Use ICP
			config := DefaultICPConfig()
			result := AlignMaps(m, refMap, config)
			transforms[id] = result.Transform
		}
	}

	renderer := NewCompositeRenderer(maps, transforms, refID)
	renderer.GlobalRotation = globalRotation
	return renderer.SavePNG(outputPath)
}

// RenderWithCalibration renders with specific rotation and translation overrides
func RenderWithCalibration(maps map[string]*ValetudoMap, rotations map[string]float64, translations map[string]TranslationOffset, outputPath string, referenceID string, rotateAll float64) error {
	if len(maps) < 2 {
		return nil
	}

	refID := referenceID
	if refID == "" {
		refID = SelectReferenceVacuum(maps, nil)
	}
	refMap := maps[refID]

	transforms := make(map[string]AffineMatrix)
	transforms[refID] = Identity()

	for id, m := range maps {
		if id == refID {
			continue
		}

		if rotDeg, ok := rotations[id]; ok {
			// Use forced rotation
			srcFeatures := ExtractFeatures(m)
			tgtFeatures := ExtractFeatures(refMap)
			transforms[id] = buildInitialTransform(srcFeatures, tgtFeatures, rotDeg)

			// Apply translation if available
			if trans, ok := translations[id]; ok && (trans.X != 0 || trans.Y != 0) {
				transforms[id] = MultiplyMatrices(Translation(trans.X, trans.Y), transforms[id])
			}

		} else {
			// Use ICP
			config := DefaultICPConfig()
			result := AlignMaps(m, refMap, config)
			transforms[id] = result.Transform
		}
	}

	renderer := NewCompositeRenderer(maps, transforms, refID)
	renderer.GlobalRotation = rotateAll
	return renderer.SavePNG(outputPath)
}

// RenderWithForcedRotations renders with specific rotation overrides for multiple vacuums
// Deprecated: Use RenderWithCalibration instead
func RenderWithForcedRotations(maps map[string]*ValetudoMap, forcedRotations map[string]float64, outputPath string, referenceOverride string, globalRotation float64) error {
	return RenderWithCalibration(maps, forcedRotations, nil, outputPath, referenceOverride, globalRotation)
}

// RenderRotationComparison renders 4 images showing all rotation options for a vacuum
func RenderRotationComparison(maps map[string]*ValetudoMap, vacuumID string, outputPrefix string, referenceOverride string, globalRotation float64) error {
	rotations := []float64{0, 90, 180, 270}

	for _, rot := range rotations {
		outputPath := fmt.Sprintf("%s_%d.png", outputPrefix, int(rot))
		if err := RenderWithForcedRotation(maps, vacuumID, rot, outputPath, referenceOverride, globalRotation); err != nil {
			return err
		}
	}
	return nil
}

// Greyscale colors for floorplan rendering
var (
	GreyscaleFloor = color.NRGBA{200, 200, 200, 255} // Light grey for floor
	GreyscaleWall  = color.NRGBA{60, 60, 60, 255}    // Dark grey for walls
	GreyscaleBG    = color.NRGBA{240, 240, 240, 255} // Background
)

// RenderGreyscale creates a greyscale composite image without color coding or legend
func (r *CompositeRenderer) RenderGreyscale() *image.RGBA {
	// Calculate bounds
	minX, minY, maxX, maxY, centerX, centerY := r.CalculateBounds()

	// Calculate image dimensions
	width := int((maxX-minX)*r.Scale) + 2*r.Padding
	height := int((maxY-minY)*r.Scale) + 2*r.Padding

	// Limit size
	if width > 4000 {
		r.Scale *= float64(4000) / float64(width)
		width = 4000
		height = int((maxY-minY)*r.Scale) + 2*r.Padding
	}
	if height > 4000 {
		r.Scale *= float64(4000) / float64(height)
		height = 4000
		width = int((maxX-minX)*r.Scale) + 2*r.Padding
	}

	// If bounds are invalid (e.g., no map data), ensure positive, reasonable dimensions
	if width <= 0 || height <= 0 {
		// No drawable area; create a minimal image (include padding if set)
		minSize := 1
		if 2*r.Padding+1 > minSize {
			minSize = 2*r.Padding + 1
		}
		if width <= 0 {
			width = minSize
		}
		if height <= 0 {
			height = minSize
		}
	}

	// Create image with background
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, GreyscaleBG)
		}
	}

	// Helper to convert world coords to image coords (with global rotation)
	toImage := func(p Point) (int, int) {
		rp := r.applyGlobalRotation(p, centerX, centerY)
		x := int((rp.X-minX)*r.Scale) + r.Padding
		y := int((rp.Y-minY)*r.Scale) + r.Padding
		return x, y
	}

	// First pass: floors/segments (greyscale)
	for id, m := range r.Maps {
		transform := r.Transforms[id]

		for _, layer := range m.Layers {
			if layer.Type == "floor" || layer.Type == "segment" {
				points := PixelsToPoints(layer.Pixels)
				for _, p := range points {
					tp := TransformPoint(p, transform)
					ix, iy := toImage(tp)
					if ix >= 0 && ix < width && iy >= 0 && iy < height {
						img.Set(ix, iy, GreyscaleFloor)
					}
				}
			}
		}
	}

	// Second pass: walls (dark grey)
	for id, m := range r.Maps {
		transform := r.Transforms[id]

		for _, layer := range m.Layers {
			if layer.Type == "wall" {
				points := PixelsToPoints(layer.Pixels)
				for _, p := range points {
					tp := TransformPoint(p, transform)
					ix, iy := toImage(tp)
					// Draw wall as 3x3 block for visibility
					for dx := -1; dx <= 1; dx++ {
						for dy := -1; dy <= 1; dy++ {
							px, py := ix+dx, iy+dy
							if px >= 0 && px < width && py >= 0 && py < height {
								img.Set(px, py, GreyscaleWall)
							}
						}
					}
				}
			}
		}
	}

	return img
}

// drawVacuumIcon draws a stylized vacuum robot icon
// cx, cy: center position
// size: approximate size in pixels (diameter of robot body, ~20-25 recommended)
// angleDeg: direction in degrees (0 = East/right, 90 = South/down in image coords)
// c: fill color for the robot
// Note: In image coordinates, Y increases downward, so 90 degrees points down
func drawVacuumIcon(img *image.RGBA, cx, cy, size int, angleDeg float64, c color.RGBA) {
	bounds := img.Bounds()
	angleRad := angleDeg * math.Pi / 180.0

	// Robot body radius (main circle)
	radius := float64(size) / 2.0

	// Colors
	outlineColor := color.RGBA{40, 40, 40, 255}   // Dark outline for visibility
	bumperColor := color.RGBA{60, 60, 60, 255}    // Dark grey for front bumper
	sensorColor := color.RGBA{200, 200, 200, 255} // Light grey for sensor area

	// Helper to check if point is in bounds and set pixel
	setPixel := func(x, y int, col color.RGBA) {
		if x >= bounds.Min.X && x < bounds.Max.X && y >= bounds.Min.Y && y < bounds.Max.Y {
			img.Set(x, y, col)
		}
	}

	// Helper to rotate a point around center
	rotatePoint := func(px, py float64) (float64, float64) {
		cos := math.Cos(angleRad)
		sin := math.Sin(angleRad)
		return px*cos - py*sin, px*sin + py*cos
	}

	// Draw outline circle (slightly larger)
	outlineRadius := radius + 2
	for dy := -int(outlineRadius) - 1; dy <= int(outlineRadius)+1; dy++ {
		for dx := -int(outlineRadius) - 1; dx <= int(outlineRadius)+1; dx++ {
			dist := math.Sqrt(float64(dx*dx + dy*dy))
			if dist <= outlineRadius && dist > radius {
				setPixel(cx+dx, cy+dy, outlineColor)
			}
		}
	}

	// Draw main body circle with fill color
	for dy := -int(radius) - 1; dy <= int(radius)+1; dy++ {
		for dx := -int(radius) - 1; dx <= int(radius)+1; dx++ {
			dist := math.Sqrt(float64(dx*dx + dy*dy))
			if dist <= radius {
				setPixel(cx+dx, cy+dy, c)
			}
		}
	}

	// Draw front bumper/direction indicator (arc at the front)
	// The bumper is a crescent shape at the front of the robot
	bumperDepth := radius * 0.25
	bumperWidth := radius * 0.7
	for dy := -int(radius); dy <= int(radius); dy++ {
		for dx := -int(radius); dx <= int(radius); dx++ {
			// Rotate point to robot's local coordinates (unrotated)
			localX, localY := rotatePoint(float64(dx), float64(dy))

			// Check if in front bumper area
			dist := math.Sqrt(float64(dx*dx + dy*dy))
			if dist <= radius && localX > radius-bumperDepth && math.Abs(localY) < bumperWidth {
				setPixel(cx+dx, cy+dy, bumperColor)
			}
		}
	}

	// Draw sensor/LiDAR turret (small circle slightly offset from center toward front)
	sensorRadius := radius * 0.25
	sensorOffsetX, sensorOffsetY := rotatePoint(radius*0.15, 0)
	sensorCX := float64(cx) + sensorOffsetX
	sensorCY := float64(cy) + sensorOffsetY

	for dy := -int(sensorRadius) - 1; dy <= int(sensorRadius)+1; dy++ {
		for dx := -int(sensorRadius) - 1; dx <= int(sensorRadius)+1; dx++ {
			dist := math.Sqrt(float64(dx*dx + dy*dy))
			if dist <= sensorRadius {
				setPixel(int(sensorCX)+dx, int(sensorCY)+dy, sensorColor)
			}
		}
	}

	// Draw direction indicator line (small line extending from center toward front)
	// This makes the direction more obvious
	lineLength := radius * 0.6
	lineStartX, lineStartY := rotatePoint(sensorRadius+1, 0)
	for t := 0.0; t <= 1.0; t += 0.05 {
		lx := lineStartX + t*(lineLength-lineStartX)
		ly := lineStartY
		rx, ry := lx, ly // Already in rotated space, just offset from sensor
		// Actually need to compute from rotated offset
		px := float64(cx) + lx*math.Cos(angleRad) - ly*math.Sin(angleRad)
		py := float64(cy) + lx*math.Sin(angleRad) + ly*math.Cos(angleRad)
		_ = rx
		_ = ry
		setPixel(int(px), int(py), outlineColor)
		// Make line thicker
		setPixel(int(px)+1, int(py), outlineColor)
		setPixel(int(px), int(py)+1, outlineColor)
	}
}

// RenderLive creates a greyscale map with live position triangles
func (r *CompositeRenderer) RenderLive(positions map[string]*LivePosition) *image.RGBA {
	// Start with greyscale base
	img := r.RenderGreyscale()

	if len(positions) == 0 {
		return img
	}

	// Calculate bounds for coordinate conversion
	minX, minY, _, _, centerX, centerY := r.CalculateBounds()

	// Helper to convert grid coords to image coords
	toImage := func(p Point) (int, int) {
		rp := r.applyGlobalRotation(p, centerX, centerY)
		x := int((rp.X-minX)*r.Scale) + r.Padding
		y := int((rp.Y-minY)*r.Scale) + r.Padding
		return x, y
	}

	// Draw position triangles for each vacuum
	for _, pos := range positions {
		// Convert grid position to image coordinates
		// (positions are stored in grid coords to match map rendering)
		gridPoint := Point{X: pos.X, Y: pos.Y}
		ix, iy := toImage(gridPoint)

		// Parse color from hex string
		robotColor := parseHexColor(pos.Color)

		// Adjust angle for global rotation and image coordinate system
		// In image coords, Y increases downward, and global rotation is CCW
		displayAngle := pos.Angle + r.GlobalRotation

		// Draw vacuum robot icon (size 22 pixels for good visibility)
		drawVacuumIcon(img, ix, iy, 22, displayAngle, robotColor)
	}

	// Add legend with vacuum IDs and colors
	drawLiveLegend(img, positions)

	return img
}

// drawLiveLegend adds a legend with vacuum IDs and colors to the live position image
func drawLiveLegend(img *image.RGBA, positions map[string]*LivePosition) {
	if len(positions) == 0 {
		return
	}

	// Sort vacuum IDs for consistent ordering
	ids := make([]string, 0, len(positions))
	for id := range positions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Legend in top-left corner
	y := 15
	for _, id := range ids {
		pos := positions[id]
		vc := parseHexColor(pos.Color)

		// Draw color swatch (12x12 square)
		for dy := 0; dy < 12; dy++ {
			for dx := 0; dx < 12; dx++ {
				img.Set(10+dx, y+dy-6, vc)
			}
		}

		drawText(img, 28, y, id, color.RGBA{0, 0, 0, 255})

		y += 18
	}
}

// parseHexColor parses a hex color string like "#FF6B6B" to color.RGBA
func parseHexColor(hex string) color.RGBA {
	// Default to red if parsing fails
	defaultColor := color.RGBA{255, 0, 0, 255}

	if len(hex) == 0 {
		return defaultColor
	}

	// Remove # prefix if present
	if hex[0] == '#' {
		hex = hex[1:]
	}

	if len(hex) != 6 {
		return defaultColor
	}

	var r, g, b uint8
	_, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	if err != nil {
		return defaultColor
	}

	return color.RGBA{r, g, b, 255}
}
