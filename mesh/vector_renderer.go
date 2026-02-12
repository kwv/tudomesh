package mesh

import (
	"fmt"
	"image/color"
	"image/png"
	"io"
	"math"
	"sort"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/tdewolff/canvas/renderers/svg"
)

// nrgbaToRGBA converts color.NRGBA to color.RGBA by premultiplying alpha
// This is needed for the canvas library which expects premultiplied RGBA
func nrgbaToRGBA(c color.NRGBA) color.RGBA {
	if c.A == 0 {
		return color.RGBA{0, 0, 0, 0}
	}
	if c.A == 255 {
		return color.RGBA{c.R, c.G, c.B, 255}
	}
	// Premultiply: multiply RGB by alpha
	alpha32 := uint32(c.A)
	return color.RGBA{
		R: uint8((uint32(c.R) * alpha32) / 255),
		G: uint8((uint32(c.G) * alpha32) / 255),
		B: uint8((uint32(c.B) * alpha32) / 255),
		A: c.A,
	}
}

// VectorRenderer renders multiple vacuum maps as vector graphics
type VectorRenderer struct {
	Maps           map[string]*ValetudoMap
	Transforms     map[string]AffineMatrix
	Colors         map[string]VacuumColor
	Reference      string
	Scale          float64 // Scale factor for rendering
	Padding        float64 // Padding in world units
	GlobalRotation float64
	Resolution     canvas.Resolution // Resolution for PNG output (default: 300 DPI)
	GridSpacing    float64           // Grid line spacing in millimeters
}

// NewVectorRenderer creates a vector renderer with default settings
func NewVectorRenderer(maps map[string]*ValetudoMap, transforms map[string]AffineMatrix, reference string) *VectorRenderer {
	colors := DefaultColors()
	colorMap := make(map[string]VacuumColor)

	i := 0
	for id := range maps {
		if id == reference {
			colorMap[id] = colors[0]
		} else {
			colorMap[id] = colors[(i%3)+1]
			i++
		}
	}

	return &VectorRenderer{
		Maps:           maps,
		Transforms:     transforms,
		Colors:         colorMap,
		Reference:      reference,
		Scale:          1.0,
		Padding:        500.0, // 500mm padding
		GlobalRotation: 0,
		Resolution:     canvas.DPI(300), // 300 DPI default for PNG output
		GridSpacing:    1000.0,          // 1000mm grid spacing
	}
}

// canvasRenderer is an interface that both svg and rasterizer renderers implement
type canvasRenderer interface {
	RenderPath(path *canvas.Path, style canvas.Style, m canvas.Matrix)
}

// RenderToSVG writes the map as an SVG to the provided writer
func (r *VectorRenderer) RenderToSVG(w io.Writer) error {
	// 1. Calculate world-space bounds
	minX, minY, maxX, maxY, centerX, centerY := r.calculateWorldBounds()

	width := (maxX - minX) + 2*r.Padding
	height := (maxY - minY) + 2*r.Padding

	// 2. Create SVG renderer
	svgRenderer := svg.New(w, width, height, nil)

	// 3. Render to canvas
	r.renderToCanvas(svgRenderer, minX, minY, maxX, maxY, centerX, centerY, width, height)

	// 4. Close SVG renderer to write closing tags
	if err := svgRenderer.Close(); err != nil {
		return err
	}

	return nil
}

// RenderToPNG writes the map as a PNG to the provided writer
func (r *VectorRenderer) RenderToPNG(w io.Writer) error {
	// 1. Calculate world-space bounds
	minX, minY, maxX, maxY, centerX, centerY := r.calculateWorldBounds()

	width := (maxX - minX) + 2*r.Padding
	height := (maxY - minY) + 2*r.Padding

	// 2. Create rasterizer renderer
	rast := rasterizer.New(width, height, r.Resolution, canvas.DefaultColorSpace)

	// 3. Render to canvas
	r.renderToCanvas(rast, minX, minY, maxX, maxY, centerX, centerY, width, height)

	// 4. Encode to PNG
	// Rasterizer implements draw.Image interface, which embeds image.Image
	return png.Encode(w, rast)
}

// renderToCanvas renders the maps to a canvas renderer (shared logic for SVG and PNG)
func (r *VectorRenderer) renderToCanvas(renderer canvasRenderer, minX, minY, maxX, maxY, centerX, centerY, width, height float64) {
	// Draw white background
	bgStyle := canvas.DefaultStyle
	bgStyle.Fill = canvas.Paint{Color: canvas.White}
	renderer.RenderPath(canvas.Rectangle(width, height), bgStyle, canvas.Identity)

	// Helper to transform world points to canvas points
	toCanvas := func(p Point) (float64, float64) {
		rp := r.applyGlobalRotation(p, centerX, centerY)
		tx := (rp.X - minX) + r.Padding
		ty := (rp.Y - minY) + r.Padding
		return tx, ty
	}

	// Trace and draw each map
	for id, m := range r.Maps {
		transform := r.Transforms[id]
		vc := r.Colors[id]

		// Render Floor/Segments first (filled)
		floorStyle := canvas.DefaultStyle
		floorStyle.Fill = canvas.Paint{Color: nrgbaToRGBA(vc.Floor)}
		floorStyle.Stroke = canvas.Paint{Color: canvas.Transparent}

		for _, layer := range m.Layers {
			if layer.Type == "floor" || layer.Type == "segment" {
				// VectorizeLayer returns paths in local-mm (pixels already normalized).
				// Transform maps local-mm to world-mm directly.
				paths := VectorizeLayer(&layer, m.PixelSize, 5.0)
				for _, p := range paths {
					cp := &canvas.Path{}
					for i, pt := range p {
						worldPt := TransformPoint(pt, transform)
						cx, cy := toCanvas(worldPt)
						if i == 0 {
							cp.MoveTo(cx, cy)
						} else {
							cp.LineTo(cx, cy)
						}
					}
					cp.Close()
					renderer.RenderPath(cp, floorStyle, canvas.Identity)
				}
			}
		}

		// Render Walls (stroked)
		wallStyle := canvas.DefaultStyle
		wallStyle.Fill = canvas.Paint{Color: canvas.Transparent}
		wallStyle.Stroke = canvas.Paint{Color: nrgbaToRGBA(vc.Wall)}
		wallStyle.StrokeWidth = 3.0 // 3mm thick walls
		wallStyle.StrokeCapper = canvas.RoundCapper{}
		wallStyle.StrokeJoiner = canvas.RoundJoiner{}

		for _, layer := range m.Layers {
			if layer.Type == "wall" {
				paths := VectorizeLayer(&layer, m.PixelSize, 2.0)
				for _, p := range paths {
					cp := &canvas.Path{}
					for i, pt := range p {
						worldPt := TransformPoint(pt, transform)
						cx, cy := toCanvas(worldPt)
						if i == 0 {
							cp.MoveTo(cx, cy)
						} else {
							cp.LineTo(cx, cy)
						}
					}
					renderer.RenderPath(cp, wallStyle, canvas.Identity)
				}
			}
		}
	}

	// 5. Render grid lines
	if r.GridSpacing > 0 {
		gridStyle := canvas.DefaultStyle
		gridStyle.Fill = canvas.Paint{Color: canvas.Transparent}
		gridStyle.Stroke = canvas.Paint{Color: canvas.Gray}
		gridStyle.StrokeWidth = 2.0
		gridStyle.Dashes = []float64{10.0, 10.0}

		// Vertical grid lines
		for x := math.Floor(minX/r.GridSpacing) * r.GridSpacing; x <= maxX; x += r.GridSpacing {
			gridPath := &canvas.Path{}
			x1, y1 := toCanvas(Point{X: x, Y: minY})
			x2, y2 := toCanvas(Point{X: x, Y: maxY})
			gridPath.MoveTo(x1, y1)
			gridPath.LineTo(x2, y2)
			renderer.RenderPath(gridPath, gridStyle, canvas.Identity)
		}

		// Horizontal grid lines
		for y := math.Floor(minY/r.GridSpacing) * r.GridSpacing; y <= maxY; y += r.GridSpacing {
			gridPath := &canvas.Path{}
			x1, y1 := toCanvas(Point{X: minX, Y: y})
			x2, y2 := toCanvas(Point{X: maxX, Y: y})
			gridPath.MoveTo(x1, y1)
			gridPath.LineTo(x2, y2)
			renderer.RenderPath(gridPath, gridStyle, canvas.Identity)
		}
	}

	// 6. Render charger icons
	for id, m := range r.Maps {
		transform := r.Transforms[id]
		vc := r.Colors[id]

		if chargerPt, ok := ExtractChargerPosition(m); ok {
			// Charger position is already in local-mm (entity points are natively mm).
			// Transform maps local-mm to world-mm directly.
			worldPt := TransformPoint(chargerPt, transform)
			cx, cy := toCanvas(worldPt)

			// Render as circle with vacuum's wall color
			chargerStyle := canvas.DefaultStyle
			chargerStyle.Fill = canvas.Paint{Color: nrgbaToRGBA(vc.Wall)}
			chargerStyle.Stroke = canvas.Paint{Color: canvas.Black}
			chargerStyle.StrokeWidth = 2.0
			chargerStyle.StrokeCapper = canvas.RoundCapper{}
			chargerStyle.StrokeJoiner = canvas.RoundJoiner{}

			chargerPath := canvas.Circle(100.0)
			chargerPath = chargerPath.Translate(cx, cy)
			renderer.RenderPath(chargerPath, chargerStyle, canvas.Identity)
		}
	}

	// 7. Render coordinate labels
	if r.GridSpacing > 0 {
		textStyle := canvas.DefaultStyle
		textStyle.Fill = canvas.Paint{Color: canvas.Black}
		textStyle.Stroke = canvas.Paint{Color: canvas.Transparent}

		// Note: Text rendering in tdewolff/canvas requires a font family
		// This is a simplified implementation - full text support would require
		// loading a font face. For now, we'll skip text rendering to keep
		// the implementation focused on the core vector layers.
		// TODO: Add text rendering with proper font support
		_ = textStyle // Silence unused variable warning
	}
}

func (r *VectorRenderer) calculateWorldBounds() (minX, minY, maxX, maxY, centerX, centerY float64) {
	minX, minY = math.MaxFloat64, math.MaxFloat64
	maxX, maxY = -math.MaxFloat64, -math.MaxFloat64

	for id, m := range r.Maps {
		transform := r.Transforms[id]
		for _, layer := range m.Layers {
			if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
				// Layer pixels are already in local-mm after NormalizeToMM.
				// The ICP transform maps local-mm to world-mm directly.
				points := PixelsToPoints(layer.Pixels)
				for _, p := range points {
					wp := TransformPoint(p, transform)
					if wp.X < minX {
						minX = wp.X
					}
					if wp.Y < minY {
						minY = wp.Y
					}
					if wp.X > maxX {
						maxX = wp.X
					}
					if wp.Y > maxY {
						maxY = wp.Y
					}
				}
			}
		}
	}

	centerX = (minX + maxX) / 2
	centerY = (minY + maxY) / 2

	return
}

func (r *VectorRenderer) applyGlobalRotation(p Point, centerX, centerY float64) Point {
	if r.GlobalRotation == 0 {
		return p
	}
	rad := r.GlobalRotation * math.Pi / 180
	x := p.X - centerX
	y := p.Y - centerY
	newX := x*math.Cos(rad) - y*math.Sin(rad)
	newY := x*math.Sin(rad) + y*math.Cos(rad)
	return Point{X: newX + centerX, Y: newY + centerY}
}

// RenderLiveToSVG renders a live view SVG with a single greyscale base map
// and colored vacuum position overlays. The base map is selected using
// SelectReferenceVacuum (largest total layer area). Each vacuum position is
// drawn as a colored circle with the vacuum ID as a label.
//
// The positions parameter maps vacuum IDs to their current LivePosition.
// Positions with coordinates outside the base map bounds are still rendered
// (the SVG viewport is expanded to include them).
func (r *VectorRenderer) RenderLiveToSVG(w io.Writer, positions map[string]*LivePosition) error {
	if len(r.Maps) == 0 {
		return fmt.Errorf("no maps available for live rendering")
	}

	// Select the base map (largest area).
	baseID := SelectReferenceVacuum(r.Maps, nil)
	baseMap := r.Maps[baseID]
	baseTransform := r.Transforms[baseID]

	// Calculate world-space bounds from the base map only.
	// Layer pixels are already in local-mm after NormalizeToMM.
	// The ICP transform maps local-mm to world-mm directly.
	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64

	for _, layer := range baseMap.Layers {
		if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
			points := PixelsToPoints(layer.Pixels)
			for _, p := range points {
				wp := TransformPoint(p, baseTransform)
				if wp.X < minX {
					minX = wp.X
				}
				if wp.Y < minY {
					minY = wp.Y
				}
				if wp.X > maxX {
					maxX = wp.X
				}
				if wp.Y > maxY {
					maxY = wp.Y
				}
			}
		}
	}

	// Expand bounds to include all vacuum positions.
	// Positions are already in world-mm (transformed by caller before storage).
	for _, pos := range positions {
		if pos.X < minX {
			minX = pos.X
		}
		if pos.Y < minY {
			minY = pos.Y
		}
		if pos.X > maxX {
			maxX = pos.X
		}
		if pos.Y > maxY {
			maxY = pos.Y
		}
	}

	centerX := (minX + maxX) / 2
	centerY := (minY + maxY) / 2

	width := (maxX - minX) + 2*r.Padding
	height := (maxY - minY) + 2*r.Padding

	svgRenderer := svg.New(w, width, height, nil)

	r.renderLiveToCanvas(svgRenderer, baseMap, baseTransform, positions,
		minX, minY, maxX, maxY, centerX, centerY, width, height)

	return svgRenderer.Close()
}

// renderLiveToCanvas draws the live view onto a canvas renderer. It renders
// the base map in greyscale, overlays grid lines, then draws each vacuum
// position as a colored circle with a text label.
func (r *VectorRenderer) renderLiveToCanvas(
	renderer canvasRenderer,
	baseMap *ValetudoMap,
	baseTransform AffineMatrix,
	positions map[string]*LivePosition,
	minX, minY, maxX, maxY, centerX, centerY, width, height float64,
) {
	// White background.
	bgStyle := canvas.DefaultStyle
	bgStyle.Fill = canvas.Paint{Color: canvas.White}
	renderer.RenderPath(canvas.Rectangle(width, height), bgStyle, canvas.Identity)

	toCanvas := func(p Point) (float64, float64) {
		rp := r.applyGlobalRotation(p, centerX, centerY)
		tx := (rp.X - minX) + r.Padding
		ty := (rp.Y - minY) + r.Padding
		return tx, ty
	}

	// Greyscale colours for the base map.
	greyFloor := color.RGBA{R: 200, G: 200, B: 200, A: 255}
	greyWall := color.RGBA{R: 80, G: 80, B: 80, A: 255}

	// Render floor/segment layers (filled, greyscale).
	// VectorizeLayer returns paths in local-mm (pixels already normalized).
	// Transform maps local-mm to world-mm directly.
	floorStyle := canvas.DefaultStyle
	floorStyle.Fill = canvas.Paint{Color: greyFloor}
	floorStyle.Stroke = canvas.Paint{Color: canvas.Transparent}

	for _, layer := range baseMap.Layers {
		if layer.Type == "floor" || layer.Type == "segment" {
			paths := VectorizeLayer(&layer, baseMap.PixelSize, 5.0)
			for _, p := range paths {
				cp := &canvas.Path{}
				for i, pt := range p {
					worldPt := TransformPoint(pt, baseTransform)
					cx, cy := toCanvas(worldPt)
					if i == 0 {
						cp.MoveTo(cx, cy)
					} else {
						cp.LineTo(cx, cy)
					}
				}
				cp.Close()
				renderer.RenderPath(cp, floorStyle, canvas.Identity)
			}
		}
	}

	// Render wall layers (stroked, greyscale).
	wallStyle := canvas.DefaultStyle
	wallStyle.Fill = canvas.Paint{Color: canvas.Transparent}
	wallStyle.Stroke = canvas.Paint{Color: greyWall}
	wallStyle.StrokeWidth = 3.0
	wallStyle.StrokeCapper = canvas.RoundCapper{}
	wallStyle.StrokeJoiner = canvas.RoundJoiner{}

	for _, layer := range baseMap.Layers {
		if layer.Type == "wall" {
			paths := VectorizeLayer(&layer, baseMap.PixelSize, 2.0)
			for _, p := range paths {
				cp := &canvas.Path{}
				for i, pt := range p {
					worldPt := TransformPoint(pt, baseTransform)
					cx, cy := toCanvas(worldPt)
					if i == 0 {
						cp.MoveTo(cx, cy)
					} else {
						cp.LineTo(cx, cy)
					}
				}
				renderer.RenderPath(cp, wallStyle, canvas.Identity)
			}
		}
	}

	// Render grid lines.
	if r.GridSpacing > 0 {
		gridStyle := canvas.DefaultStyle
		gridStyle.Fill = canvas.Paint{Color: canvas.Transparent}
		gridStyle.Stroke = canvas.Paint{Color: canvas.Gray}
		gridStyle.StrokeWidth = 2.0
		gridStyle.Dashes = []float64{10.0, 10.0}

		for x := math.Floor(minX/r.GridSpacing) * r.GridSpacing; x <= maxX; x += r.GridSpacing {
			gridPath := &canvas.Path{}
			x1, y1 := toCanvas(Point{X: x, Y: minY})
			x2, y2 := toCanvas(Point{X: x, Y: maxY})
			gridPath.MoveTo(x1, y1)
			gridPath.LineTo(x2, y2)
			renderer.RenderPath(gridPath, gridStyle, canvas.Identity)
		}

		for y := math.Floor(minY/r.GridSpacing) * r.GridSpacing; y <= maxY; y += r.GridSpacing {
			gridPath := &canvas.Path{}
			x1, y1 := toCanvas(Point{X: minX, Y: y})
			x2, y2 := toCanvas(Point{X: maxX, Y: y})
			gridPath.MoveTo(x1, y1)
			gridPath.LineTo(x2, y2)
			renderer.RenderPath(gridPath, gridStyle, canvas.Identity)
		}
	}

	// Render vacuum positions as colored circles.
	// Sort by vacuum ID for deterministic rendering order.
	vacIDs := make([]string, 0, len(positions))
	for id := range positions {
		vacIDs = append(vacIDs, id)
	}
	sort.Strings(vacIDs)

	// Derive indicator sizes from the map extent so they scale with the floor plan.
	// Use the shorter axis to keep proportions consistent across aspect ratios.
	mapSpan := maxX - minX
	if h := maxY - minY; h < mapSpan {
		mapSpan = h
	}
	if mapSpan < 1 {
		mapSpan = 1 // guard against degenerate maps
	}
	// The circle radius is ~0.4% of the shorter map dimension.
	// For a 5 000 mm floor plan this gives a 20 mm radius -- visible but unobtrusive.
	vacRadius := mapSpan * 0.004
	vacDirLen := vacRadius * 1.75
	vacStroke := vacRadius * 0.075
	vacTagW := vacRadius * 1.25
	vacTagH := vacRadius * 0.5

	for _, id := range vacIDs {
		pos := positions[id]
		// Positions are already in world-mm (transformed by caller).
		cx, cy := toCanvas(Point{X: pos.X, Y: pos.Y})
		vacColor := parseHexColor(pos.Color)

		// Outer circle (border).
		outerStyle := canvas.DefaultStyle
		outerStyle.Fill = canvas.Paint{Color: vacColor}
		outerStyle.Stroke = canvas.Paint{Color: canvas.Black}
		outerStyle.StrokeWidth = vacStroke

		outerPath := canvas.Circle(vacRadius)
		outerPath = outerPath.Translate(cx, cy)
		renderer.RenderPath(outerPath, outerStyle, canvas.Identity)

		// Direction indicator: a small line from center in the heading direction.
		rad := pos.Angle * math.Pi / 180
		dx := vacDirLen * math.Cos(rad)
		dy := vacDirLen * math.Sin(rad)

		dirStyle := canvas.DefaultStyle
		dirStyle.Fill = canvas.Paint{Color: canvas.Transparent}
		dirStyle.Stroke = canvas.Paint{Color: vacColor}
		dirStyle.StrokeWidth = vacStroke * 1.5

		dirPath := &canvas.Path{}
		dirPath.MoveTo(cx, cy)
		dirPath.LineTo(cx+dx, cy+dy)
		renderer.RenderPath(dirPath, dirStyle, canvas.Identity)

		// Label: render vacuum ID as a simple marker below the circle.
		// Full text rendering requires font loading in tdewolff/canvas.
		// For now, render a small unique-color rectangle as an identifier tag.
		tagStyle := canvas.DefaultStyle
		tagStyle.Fill = canvas.Paint{Color: vacColor}
		tagStyle.Stroke = canvas.Paint{Color: canvas.Black}
		tagStyle.StrokeWidth = vacStroke * 0.5

		tagPath := canvas.Rectangle(vacTagW, vacTagH)
		tagPath = tagPath.Translate(cx-vacTagW/2, cy-vacRadius*1.5)
		renderer.RenderPath(tagPath, tagStyle, canvas.Identity)
	}
}
