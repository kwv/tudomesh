package mesh

import (
	"io"
	"math"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/svg"
)

// VectorRenderer renders multiple vacuum maps as vector graphics
type VectorRenderer struct {
	Maps           map[string]*ValetudoMap
	Transforms     map[string]AffineMatrix
	Colors         map[string]VacuumColor
	Reference      string
	Scale          float64 // Scale factor for rendering
	Padding        float64 // Padding in world units
	GlobalRotation float64
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
	}
}

// RenderToSVG writes the map as an SVG to the provided writer
func (r *VectorRenderer) RenderToSVG(w io.Writer) error {
	// 1. Calculate world-space bounds
	minX, minY, maxX, maxY, centerX, centerY := r.calculateWorldBounds()

	width := (maxX - minX) + 2*r.Padding
	height := (maxY - minY) + 2*r.Padding

	// 2. Create SVG renderer
	svgRenderer := svg.New(w, width, height, nil)

	// Draw white background
	bgStyle := canvas.DefaultStyle
	bgStyle.Fill = canvas.Paint{Color: canvas.White}
	svgRenderer.RenderPath(canvas.Rectangle(width, height), bgStyle, canvas.Identity)

	// 3. Helper to transform world points to canvas points
	toCanvas := func(p Point) (float64, float64) {
		rp := r.applyGlobalRotation(p, centerX, centerY)
		tx := (rp.X - minX) + r.Padding
		ty := (rp.Y - minY) + r.Padding
		return tx, ty
	}

	// 4. Trace and draw each map
	for id, m := range r.Maps {
		transform := r.Transforms[id]
		vc := r.Colors[id]

		// Render Floor/Segments first (filled)
		floorStyle := canvas.DefaultStyle
		floorStyle.Fill = canvas.Paint{Color: vc.Floor}
		floorStyle.Stroke = canvas.Paint{Color: canvas.Transparent}

		for _, layer := range m.Layers {
			if layer.Type == "floor" || layer.Type == "segment" {
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
					svgRenderer.RenderPath(cp, floorStyle, canvas.Identity)
				}
			}
		}

		// Render Walls (stroked)
		wallStyle := canvas.DefaultStyle
		wallStyle.Fill = canvas.Paint{Color: canvas.Transparent}
		wallStyle.Stroke = canvas.Paint{Color: vc.Wall}
		wallStyle.StrokeWidth = 20.0 // 20mm thick walls

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
					svgRenderer.RenderPath(cp, wallStyle, canvas.Identity)
				}
			}
		}
	}

	return nil
}

func (r *VectorRenderer) calculateWorldBounds() (minX, minY, maxX, maxY, centerX, centerY float64) {
	minX, minY = math.MaxFloat64, math.MaxFloat64
	maxX, maxY = -math.MaxFloat64, -math.MaxFloat64

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
