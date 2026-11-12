package mesh

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"sort"

	"golang.org/x/image/font"
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
	// IgnoreRooms lists reference room names drawn as plain floor in the live
	// views (see IsIgnoredRoom).
	IgnoreRooms []string
	// Names maps a vacuum id (LivePosition key) to the name shown in the live
	// legend, e.g. its MQTT identifier. Missing ids show the id.
	Names map[string]string
	// Calibrated lists vacuums whose Transforms entry is a real calibration;
	// only those get their own room highlighted. nil trusts every transform.
	Calibrated map[string]bool
	// Theme colors the live and alert views (RenderLive, RenderGreyscale,
	// RenderLiveHighlight). The zero value is LightTheme.
	Theme Theme
	// Crop limits RenderLiveHighlight to the highlighted vacuum's room or a
	// radius around it (see Crop). The cropped image has no robot legend.
	Crop Crop
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

// textSize is the caption and legend font size in pixels.
const textSize = 13

// drawText renders text onto an image with its baseline starting at (x, y).
func drawText(img *image.RGBA, x, y int, text string, c color.RGBA) {
	face := newTextFace(textSize)
	defer func() { _ = face.Close() }()
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	d.DrawString(text)
}

// textWidth is the pixel width of text as drawText renders it.
func textWidth(text string) int {
	face := newTextFace(textSize)
	defer func() { _ = face.Close() }()
	return font.MeasureString(face, text).Ceil()
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

		var result Alignment
		if id == vacuumID {
			result, _ = AlignMapsAtRotation(m, refMap, rotationDeg)
		} else {
			result, _ = AlignMaps(m, refMap)
		}
		transforms[id] = result.Transform
	}

	renderer := NewCompositeRenderer(maps, transforms, refID)
	renderer.GlobalRotation = globalRotation
	return renderer.SavePNG(outputPath)
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

// Light-theme floorplan colors; renderers use CompositeRenderer.Theme.
var (
	GreyscaleFloor = LightTheme.Floor
	GreyscaleWall  = LightTheme.Wall
	GreyscaleBG    = LightTheme.Background
)

// renderBase draws the greyscale base map. Every map's floor is grey; the
// reference map is drawn last with each room in its own light color. Rooms in
// IgnoreRooms stay grey. alert, if set, is filled on top (see drawAlertRoom),
// before the walls. Walls come from the reference map only, so maps a cell or
// two apart don't double them; other maps only add floor.
func (r *CompositeRenderer) renderBase(alert *alertRoom) *image.RGBA {
	theme := r.Theme.orLight()
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
			img.Set(x, y, theme.Background)
		}
	}

	// Helper to convert world coords to image coords (with global rotation)
	toImage := func(p Point) (int, int) {
		rp := r.applyGlobalRotation(p, centerX, centerY)
		x := int((rp.X-minX)*r.Scale) + r.Padding
		y := int((rp.Y-minY)*r.Scale) + r.Padding
		return x, y
	}

	// Fill whole cells so floors and rooms read as solid areas.
	cell := max(1, int(math.Ceil(r.Scale)))
	fillCell := func(ix, iy int, c color.Color) {
		for dy := 0; dy < cell; dy++ {
			for dx := 0; dx < cell; dx++ {
				if px, py := ix+dx, iy+dy; px >= 0 && px < width && py >= 0 && py < height {
					img.Set(px, py, c)
				}
			}
		}
	}

	// First pass: floors/segments. Other maps first, then the reference map
	// with its rooms shaded.
	order := make([]string, 0, len(r.Maps))
	for _, id := range sortedKeys(r.Maps) {
		if id != r.Reference {
			order = append(order, id)
		}
	}
	if _, ok := r.Maps[r.Reference]; ok {
		order = append(order, r.Reference)
	}
	for _, id := range order {
		m := r.Maps[id]
		transform := r.Transforms[id]
		var roomColors map[string]color.NRGBA
		if id == r.Reference {
			roomColors = theme.RoomColors(m, r.IgnoreRooms...)
		}

		for _, layer := range m.Layers {
			if layer.Type != "floor" && layer.Type != "segment" {
				continue
			}
			var c color.Color = theme.Floor
			if rc, ok := roomColors[layer.MetaData.SegmentID]; ok && layer.Type == "segment" {
				c = rc
			}
			for _, p := range PixelsToPoints(layer.Pixels) {
				ix, iy := toImage(TransformPoint(p, transform))
				fillCell(ix, iy, c)
			}
		}
	}

	if alert != nil {
		refT, ok := r.Transforms[r.Reference]
		if !ok {
			refT = Identity()
		}
		drawAlertRoom(img, alert, refT, toImage, cell, theme)
	}

	// Second pass: walls (dark grey), from the reference map (every map if
	// the reference has none).
	wallMaps := r.Maps
	if m, ok := r.Maps[r.Reference]; ok {
		wallMaps = map[string]*ValetudoMap{r.Reference: m}
	}
	for id, m := range wallMaps {
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
								img.Set(px, py, theme.Wall)
							}
						}
					}
				}
			}
		}
	}

	return img
}

// Second cues for the alert room, so it doesn't rely on hue alone: a 2px
// inner border and a 1px diagonal hatch (Theme.AlertBorder, AlertHatch).
var (
	alertBorderColor = toRGBA(LightTheme.AlertBorder) // light theme, for tests
	alertBorderWidth = 2                              // image pixels
	alertHatchPeriod = 6                              // image pixels between hatch lines
)

// drawAlertRoom fills alert's cells (reference-map grid, placed with refT and
// toImage) in theme.Alert with a diagonal hatch and an inner border.
// cell is the image size of one grid cell.
func drawAlertRoom(img *image.RGBA, alert *alertRoom, refT AffineMatrix, toImage func(Point) (int, int), cell int, theme Theme) {
	fill, hatch, edge := toRGBA(theme.Alert), toRGBA(theme.AlertHatch), toRGBA(theme.AlertBorder)
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	mask := make([]bool, w*h)
	for _, p := range PixelsToPoints(alert.Pixels) {
		ix, iy := toImage(TransformPoint(p, refT))
		for py := max(iy, 0); py < min(iy+cell, h); py++ {
			for px := max(ix, 0); px < min(ix+cell, w); px++ {
				mask[py*w+px] = true
			}
		}
	}
	inside := func(x, y int) bool { return x >= 0 && x < w && y >= 0 && y < h && mask[y*w+x] }
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !mask[y*w+x] {
				continue
			}
			c := fill
			if (x+y)%alertHatchPeriod == 0 {
				c = hatch
			}
		border:
			for dy := -alertBorderWidth; dy <= alertBorderWidth; dy++ {
				for dx := -alertBorderWidth; dx <= alertBorderWidth; dx++ {
					if !inside(x+dx, y+dy) {
						c = edge
						break border
					}
				}
			}
			img.SetRGBA(x, y, c)
		}
	}
}

// drawVacuumIcon draws a filled circle with a dark outline and a white
// arrowhead inside pointing along the heading (same shape as the live SVG).
// cx, cy: center position
// size: diameter of the body in pixels (~20-25 recommended)
// angleRad: heading in radians from +X with Y down (see headingRad)
// c: fill color for the robot
func drawVacuumIcon(img *image.RGBA, cx, cy, size int, angleRad float64, c color.RGBA) {
	bounds := img.Bounds()
	radius := float64(size) / 2.0
	outline := radius + 2
	outlineColor := color.RGBA{40, 40, 40, 255}
	arrowColor := color.RGBA{255, 255, 255, 255}

	// Arrowhead in local coords (pointing +X), as fractions of radius.
	arrow := []Point{{0.65, 0}, {-0.45, 0.5}, {-0.2, 0}, {-0.45, -0.5}}
	for i := range arrow {
		arrow[i].X *= radius
		arrow[i].Y *= radius
	}
	cos, sin := math.Cos(angleRad), math.Sin(angleRad)

	r := int(outline) + 1
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			x, y := cx+dx, cy+dy
			if x < bounds.Min.X || x >= bounds.Max.X || y < bounds.Min.Y || y >= bounds.Max.Y {
				continue
			}
			fx, fy := float64(dx), float64(dy)
			dist := math.Hypot(fx, fy)
			switch {
			case dist > outline:
				continue
			case dist > radius:
				img.SetRGBA(x, y, outlineColor)
			case pointInPolygon(Point{X: fx*cos + fy*sin, Y: -fx*sin + fy*cos}, arrow):
				img.SetRGBA(x, y, arrowColor)
			default:
				img.SetRGBA(x, y, c)
			}
		}
	}
}

// pointInPolygon reports whether p is inside poly (even-odd rule).
func pointInPolygon(p Point, poly []Point) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

// RenderLive creates a greyscale map with live position triangles
func (r *CompositeRenderer) RenderLive(positions map[string]*LivePosition) *image.RGBA {
	return r.renderLive(positions, nil, true)
}

func (r *CompositeRenderer) renderLive(positions map[string]*LivePosition, alert *alertRoom, legend bool) *image.RGBA {
	img := r.renderBase(alert)
	r.drawRoomOverlay(img)

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

		drawVacuumIcon(img, ix, iy, 22, headingRad(pos.Angle+r.GlobalRotation), robotColor)
	}

	if legend {
		drawLiveLegend(img, positions, r.Names, toRGBA(r.Theme.orLight().Text))
	}

	return img
}

// RenderLiveHighlight renders the live map like RenderLive, then draws a ring
// around highlightID's position and adds a caption strip above the map. If the
// vacuum has no position, only the caption is added. The vacuum's room (see
// alertShape) is filled in AlertRoomColor with a hatch and darker border.
// With r.Crop set and a known position, the map is cropped around the vacuum
// (kept at least as wide as the caption) and the robot legend is left out.
func (r *CompositeRenderer) RenderLiveHighlight(positions map[string]*LivePosition, highlightID, caption string) *image.RGBA {
	pos := positions[highlightID]
	alert := alertShape(r.Maps, r.Transforms, r.Calibrated, r.Reference, highlightID, pos, r.IgnoreRooms...)
	refT, ok := r.Transforms[r.Reference]
	if !ok {
		refT = Identity()
	}
	ps := 5.0
	if m := r.Maps[r.Reference]; m != nil {
		ps = pixelSizeOf(m)
	}
	cropPts, cropMargin, cropping := r.Crop.cropPoints(alert, refT, pos, ps)
	img := r.renderLive(positions, alert, !cropping)

	minX, minY, _, _, centerX, centerY := r.CalculateBounds()
	toImage := func(p Point) (float64, float64) {
		rp := r.applyGlobalRotation(p, centerX, centerY)
		return (rp.X-minX)*r.Scale + float64(r.Padding), (rp.Y-minY)*r.Scale + float64(r.Padding)
	}
	if pos != nil {
		x, y := toImage(Point{X: pos.X, Y: pos.Y})
		drawRing(img, int(x), int(y), 34, 5, color.RGBA{230, 30, 30, 255})
	}

	if cropping {
		minSide := cropMinSide / ps * r.Scale
		if caption != "" {
			minSide = math.Max(minSide, float64(textWidth(caption)+16))
		}
		b := img.Bounds()
		c := fitCrop(cropPts, toImage, cropMargin*r.Scale, minSide, float64(b.Dx()), float64(b.Dy()))
		rect := image.Rect(int(c.minX), int(c.minY), int(math.Ceil(c.maxX)), int(math.Ceil(c.maxY))).Intersect(b)
		if !rect.Empty() {
			cropped := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
			draw.Draw(cropped, cropped.Bounds(), img, rect.Min, draw.Src)
			img = cropped
		}
	}

	if caption == "" {
		return img
	}
	const bar = 24
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()+bar))
	draw.Draw(out, image.Rect(0, 0, b.Dx(), bar), image.NewUniform(color.RGBA{30, 30, 30, 255}), image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(0, bar, b.Dx(), b.Dy()+bar), img, b.Min, draw.Src)
	drawText(out, 8, 17, caption, color.RGBA{255, 255, 255, 255})
	return out
}

// drawRing draws a circle outline of the given thickness.
func drawRing(img *image.RGBA, cx, cy, radius, thickness int, c color.RGBA) {
	inner := (radius - thickness) * (radius - thickness)
	outer := radius * radius
	b := img.Bounds()
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			d := dx*dx + dy*dy
			x, y := cx+dx, cy+dy
			if d <= outer && d >= inner && x >= b.Min.X && x < b.Max.X && y >= b.Min.Y && y < b.Max.Y {
				img.Set(x, y, c)
			}
		}
	}
}

// drawLiveLegend adds a legend with each vacuum's color and name (names[id],
// else the id) to the live position image.
func drawLiveLegend(img *image.RGBA, positions map[string]*LivePosition, names map[string]string, text color.RGBA) {
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

		label := id
		if n := names[id]; n != "" {
			label = n
		}
		drawText(img, 28, y, label, text)

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
