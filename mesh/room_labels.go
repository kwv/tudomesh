package mesh

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Room labels and outlines for the live/alert map (PNG and SVG). Both use
// only the reference map's segments.

// pngLabelSize is the room label font size in PNG pixels.
const pngLabelSize = 12

// roomLabel returns the display name of a segment layer, or false when
// Valetudo reports it unnamed (empty, or the segment id as name) or it is in
// ignore (see IsIgnoredRoom).
func roomLabel(layer MapLayer, ignore ...string) (string, bool) {
	name := strings.TrimSpace(layer.MetaData.Name)
	if layer.Type != "segment" || name == "" || name == layer.MetaData.SegmentID || IsIgnoredRoom(name, ignore) {
		return "", false
	}
	return name, true
}

// cellMask is a set of integer cells inside the box [x0,x0+w) x [y0,y0+h).
type cellMask struct {
	x0, y0, w, h int
	on           []bool
}

// newCellMask builds a mask from grid pixels [x1,y1,x2,y2,...].
func newCellMask(pixels []int) *cellMask {
	if len(pixels) < 2 {
		return &cellMask{}
	}
	minX, minY, maxX, maxY := math.MaxInt, math.MaxInt, math.MinInt, math.MinInt
	for i := 0; i+1 < len(pixels); i += 2 {
		minX, maxX = min(minX, pixels[i]), max(maxX, pixels[i])
		minY, maxY = min(minY, pixels[i+1]), max(maxY, pixels[i+1])
	}
	m := &cellMask{x0: minX, y0: minY, w: maxX - minX + 1, h: maxY - minY + 1}
	m.on = make([]bool, m.w*m.h)
	for i := 0; i+1 < len(pixels); i += 2 {
		m.on[(pixels[i+1]-minY)*m.w+pixels[i]-minX] = true
	}
	return m
}

func (m *cellMask) at(x, y int) bool {
	x, y = x-m.x0, y-m.y0
	return x >= 0 && y >= 0 && x < m.w && y < m.h && m.on[y*m.w+x]
}

// filledHoles returns a copy of m with enclosed holes (furniture, obstacles)
// filled, except cells for which blocked returns true (e.g. another room).
// A hole is a 4-connected group of off cells that does not reach the box edge.
func (m *cellMask) filledHoles(blocked func(x, y int) bool) *cellMask {
	out := &cellMask{x0: m.x0, y0: m.y0, w: m.w, h: m.h, on: make([]bool, len(m.on))}
	copy(out.on, m.on)
	outside := make([]bool, len(m.on))
	var stack []int
	push := func(x, y int) {
		if x < 0 || y < 0 || x >= m.w || y >= m.h {
			return
		}
		if i := y*m.w + x; !m.on[i] && !outside[i] {
			outside[i] = true
			stack = append(stack, i)
		}
	}
	for x := 0; x < m.w; x++ {
		push(x, 0)
		push(x, m.h-1)
	}
	for y := 0; y < m.h; y++ {
		push(0, y)
		push(m.w-1, y)
	}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := i%m.w, i/m.w
		push(x-1, y)
		push(x+1, y)
		push(x, y-1)
		push(x, y+1)
	}
	for i := range out.on {
		if !out.on[i] && !outside[i] && (blocked == nil || !blocked(i%m.w+m.x0, i/m.w+m.y0)) {
			out.on[i] = true
		}
	}
	return out
}

// pole returns the cell farthest from the mask's edge (a pole of
// inaccessibility), using a two-pass chamfer distance transform; ties go to
// the cell nearest the mask's centroid. ok is false for an empty mask.
func (m *cellMask) pole() (x, y int, ok bool) {
	if len(m.on) == 0 {
		return 0, 0, false
	}
	const diag = math.Sqrt2
	inf := math.Inf(1)
	d := make([]float64, len(m.on))
	get := func(x, y int) float64 {
		if x < 0 || y < 0 || x >= m.w || y >= m.h {
			return 0 // outside the box is outside the mask
		}
		return d[y*m.w+x]
	}
	var sx, sy, n float64
	for i, on := range m.on {
		if on {
			d[i] = inf
			sx += float64(i % m.w)
			sy += float64(i / m.w)
			n++
		}
	}
	if n == 0 {
		return 0, 0, false
	}
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			i := y*m.w + x
			if d[i] == 0 {
				continue
			}
			d[i] = math.Min(d[i], math.Min(get(x-1, y)+1, get(x, y-1)+1))
			d[i] = math.Min(d[i], math.Min(get(x-1, y-1)+diag, get(x+1, y-1)+diag))
		}
	}
	for y := m.h - 1; y >= 0; y-- {
		for x := m.w - 1; x >= 0; x-- {
			i := y*m.w + x
			if d[i] == 0 {
				continue
			}
			d[i] = math.Min(d[i], math.Min(get(x+1, y)+1, get(x, y+1)+1))
			d[i] = math.Min(d[i], math.Min(get(x+1, y+1)+diag, get(x-1, y+1)+diag))
		}
	}
	cx, cy := sx/n, sy/n
	best, bestDist, bestOff := -1, -1.0, inf
	for i, v := range d {
		if v <= 0 {
			continue
		}
		off := math.Hypot(float64(i%m.w)-cx, float64(i/m.w)-cy)
		if v > bestDist+1e-9 || (math.Abs(v-bestDist) <= 1e-9 && off < bestOff) {
			best, bestDist, bestOff = i, v, off
		}
	}
	return best%m.w + m.x0, best/m.w + m.y0, true
}

// rectInside reports whether every sample of the axis-aligned rectangle
// centered at (cx,cy) with size w x h, taken at most step apart, is inside.
func rectInside(cx, cy, w, h, step float64, inside func(x, y float64) bool) bool {
	nx, ny := int(math.Ceil(w/step)), int(math.Ceil(h/step))
	for j := 0; j <= ny; j++ {
		y := cy - h/2 + h*float64(j)/float64(max(ny, 1))
		for i := 0; i <= nx; i++ {
			if !inside(cx-w/2+w*float64(i)/float64(max(nx, 1)), y) {
				return false
			}
		}
	}
	return true
}

// --- PNG ---

// drawRoomOverlay draws a 1px darker outline around each of the reference
// map's rooms and each named room's label, on an image renderBase drew with
// the renderer's current bounds and Scale. Outlines only darken pixels still
// showing a room fill, so walls drawn on top stay as they are.
func (r *CompositeRenderer) drawRoomOverlay(img *image.RGBA) {
	m := r.Maps[r.Reference]
	if m == nil {
		return
	}
	transform := r.Transforms[r.Reference]
	minX, minY, _, _, centerX, centerY := r.CalculateBounds()
	toImage := func(p Point) (int, int) {
		rp := r.applyGlobalRotation(p, centerX, centerY)
		return int((rp.X-minX)*r.Scale) + r.Padding, int((rp.Y-minY)*r.Scale) + r.Padding
	}
	cell := max(1, int(math.Ceil(r.Scale)))
	b := img.Bounds()

	// Rasterize segments the way renderBase fills them; later layers win.
	var segs []MapLayer
	for _, layer := range m.Layers {
		if layer.Type == "segment" {
			segs = append(segs, layer)
		}
	}
	if len(segs) == 0 {
		return
	}
	owner := make([]int32, b.Dx()*b.Dy()) // segment index + 1; 0 = none
	pixels := make([][]int, len(segs))
	for k, layer := range segs {
		for _, p := range PixelsToPoints(layer.Pixels) {
			ix, iy := toImage(TransformPoint(p, transform))
			for dy := 0; dy < cell; dy++ {
				for dx := 0; dx < cell; dx++ {
					if px, py := ix+dx, iy+dy; px >= 0 && px < b.Dx() && py >= 0 && py < b.Dy() {
						owner[py*b.Dx()+px] = int32(k + 1)
					}
				}
			}
		}
	}
	ownerAt := func(x, y int) int32 {
		if x < 0 || y < 0 || x >= b.Dx() || y >= b.Dy() {
			return 0
		}
		return owner[y*b.Dx()+x]
	}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if k := owner[y*b.Dx()+x]; k > 0 {
				pixels[k-1] = append(pixels[k-1], x, y)
			}
		}
	}

	// Outlines.
	fills := map[color.RGBA]bool{toRGBA(AlertRoomColor): true}
	for _, c := range RoomColors(m) {
		fills[toRGBA(c)] = true
	}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			k := owner[y*b.Dx()+x]
			if k == 0 || (ownerAt(x-1, y) == k && ownerAt(x+1, y) == k && ownerAt(x, y-1) == k && ownerAt(x, y+1) == k) {
				continue
			}
			if c := img.RGBAAt(x, y); fills[c] {
				img.SetRGBA(x, y, color.RGBA{c.R * 7 / 10, c.G * 7 / 10, c.B * 7 / 10, 255})
			}
		}
	}

	// Labels.
	face := newTextFace(pngLabelSize)
	defer func() { _ = face.Close() }()
	metrics := face.Metrics()
	ascent, descent := metrics.Ascent.Ceil(), metrics.Descent.Ceil()
	const pad = 2 // keep text off the outline and wall edges
	for k, layer := range segs {
		name, ok := roomLabel(layer, r.IgnoreRooms...)
		if !ok {
			continue
		}
		k32 := int32(k + 1)
		mask := newCellMask(pixels[k]).filledHoles(func(x, y int) bool {
			o := ownerAt(x, y)
			return o != 0 && o != k32
		})
		px, py, ok := mask.pole()
		if !ok {
			continue
		}
		w := font.MeasureString(face, name).Ceil()
		h := ascent + descent
		cx, cy := float64(px)+0.5, float64(py)+0.5
		inside := func(x, y float64) bool { return mask.at(int(math.Floor(x)), int(math.Floor(y))) }
		if !rectInside(cx, cy, float64(w+2*pad), float64(h+2*pad), 1, inside) {
			continue
		}
		// A 1px halo in the room's fill keeps text readable over obstacles.
		dot := fixed.P(int(math.Round(cx-float64(w)/2)), int(math.Round(cy-float64(h)/2))+ascent)
		halo := image.NewUniform(roomFillAt(img, pixels[k], fills))
		for _, off := range []fixed.Point26_6{fixed.P(-1, 0), fixed.P(1, 0), fixed.P(0, -1), fixed.P(0, 1)} {
			d := &font.Drawer{Dst: img, Src: halo, Face: face, Dot: dot.Add(off)}
			d.DrawString(name)
		}
		d := &font.Drawer{Dst: img, Src: image.NewUniform(color.RGBA{48, 48, 48, 255}), Face: face, Dot: dot}
		d.DrawString(name)
	}
}

// roomFillAt returns the first room fill color found at pixels [x1,y1,...]
// of img, or white.
func roomFillAt(img *image.RGBA, pixels []int, fills map[color.RGBA]bool) color.RGBA {
	for i := 0; i+1 < len(pixels); i += 2 {
		if c := img.RGBAAt(pixels[i], pixels[i+1]); fills[c] {
			return c
		}
	}
	return color.RGBA{255, 255, 255, 255}
}

func toRGBA(c color.NRGBA) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

// --- SVG ---

// writeRoomOutlines writes <g id="room-outlines"> with one
// <path class="room-outline" data-segment-id> per segment of m: the cell
// edges where the room borders anything else, in grid units. Rooms in ignore
// get no outline.
func writeRoomOutlines(out *bytes.Buffer, m *ValetudoMap, ignore ...string) {
	out.WriteString(`<g id="room-outlines">`)
	for _, layer := range m.Layers {
		if layer.Type != "segment" || IsIgnoredRoom(layer.MetaData.Name, ignore) {
			continue
		}
		if d := cellEdgePath(newCellMask(layer.Pixels)); d != "" {
			fmt.Fprintf(out, `<path class="room-outline" data-segment-id="%s" d="%s"/>`,
				svgEscape(layer.MetaData.SegmentID), d)
		}
	}
	out.WriteString("</g>\n")
}

// cellEdgePath returns the boundary edges of mask as an SVG path (stroke only),
// merging collinear runs of edges.
func cellEdgePath(m *cellMask) string {
	var b strings.Builder
	// Horizontal edge lines y: the top of cell (x,y) or the bottom of (x,y-1)
	// where the cell across the line is off. Vertical lines likewise.
	for y := m.y0; y <= m.y0+m.h; y++ {
		for _, top := range []bool{true, false} {
			edgeRuns(m.x0, m.x0+m.w, func(x int) bool {
				if top {
					return m.at(x, y) && !m.at(x, y-1)
				}
				return m.at(x, y-1) && !m.at(x, y)
			}, func(start, n int) { fmt.Fprintf(&b, "M%d %dh%d", start, y, n) })
		}
	}
	for x := m.x0; x <= m.x0+m.w; x++ {
		for _, left := range []bool{true, false} {
			edgeRuns(m.y0, m.y0+m.h, func(y int) bool {
				if left {
					return m.at(x, y) && !m.at(x-1, y)
				}
				return m.at(x-1, y) && !m.at(x, y)
			}, func(start, n int) { fmt.Fprintf(&b, "M%d %dv%d", x, start, n) })
		}
	}
	return b.String()
}

// edgeRuns calls emit for each run of consecutive i in [from,to) where edge is true.
func edgeRuns(from, to int, edge func(int) bool, emit func(start, n int)) {
	start, open := 0, false
	for i := from; i <= to; i++ {
		on := i < to && edge(i)
		if on && !open {
			start, open = i, true
		}
		if !on && open {
			emit(start, i-start)
			open = false
		}
	}
}

// svgLabelSize is the room label font size in canvas units: the PNG size
// in grid cells, so both outputs label the same rooms.
func svgLabelSize(ps float64) float64 { return pngLabelSize * ps }

// writeRoomLabels writes <g id="room-labels"> with one
// <text class="room-label" data-segment-id> per named segment of m whose
// label fits inside the room. Labels are in canvas coordinates, so they stay
// upright under the global rotation.
func writeRoomLabels(out *bytes.Buffer, f *svgFrame, m *ValetudoMap, t AffineMatrix, size float64, ignore ...string) {
	ps := pixelSizeOf(m)
	gridToCanvas := func(p Point) [2]float64 {
		tp := TransformPoint(p, t)
		return f.toCanvas(Point{X: tp.X * ps, Y: tp.Y * ps})
	}
	// Invert the (affine) grid -> canvas map to test canvas points against cells.
	o, ex, ey := gridToCanvas(Point{}), gridToCanvas(Point{X: 1}), gridToCanvas(Point{Y: 1})
	a, c := ex[0]-o[0], ex[1]-o[1]
	bb, d := ey[0]-o[0], ey[1]-o[1]
	det := a*d - bb*c
	if det == 0 {
		return
	}
	toGrid := func(x, y float64) Point {
		x, y = x-o[0], y-o[1]
		return Point{X: (d*x - bb*y) / det, Y: (-c*x + a*y) / det}
	}
	step := math.Max(1e-6, math.Min(math.Hypot(a, c), math.Hypot(bb, d))/2)

	face := newTextFace(64) // only for measuring; scaled to size
	defer func() { _ = face.Close() }()
	scale := size / 64

	var masks []*cellMask
	for _, layer := range m.Layers {
		if layer.Type == "segment" {
			masks = append(masks, newCellMask(layer.Pixels))
		}
	}

	out.WriteString(`<g id="room-labels">`)
	k := -1
	for _, layer := range m.Layers {
		if layer.Type != "segment" {
			continue
		}
		k++
		name, ok := roomLabel(layer, ignore...)
		if !ok {
			continue
		}
		self := masks[k]
		mask := self.filledHoles(func(x, y int) bool {
			for _, other := range masks {
				if other != self && other.at(x, y) {
					return true
				}
			}
			return false
		})
		px, py, ok := mask.pole()
		if !ok {
			continue
		}
		center := gridToCanvas(Point{X: float64(px) + 0.5, Y: float64(py) + 0.5})
		w := float64(font.MeasureString(face, name)) / 64 * scale
		h := size * 1.2
		inside := func(x, y float64) bool {
			g := toGrid(x, y)
			return mask.at(int(math.Floor(g.X)), int(math.Floor(g.Y)))
		}
		if !rectInside(center[0], center[1], w+size*0.4, h, step, inside) {
			continue
		}
		fmt.Fprintf(out, `<text class="room-label" data-segment-id="%s" x="%.1f" y="%.1f">%s</text>`,
			svgEscape(layer.MetaData.SegmentID), center[0], center[1]+size*0.35, svgEscape(name))
	}
	out.WriteString("</g>\n")
}

func roomLabelCSS(s svgSizes, ps float64) string {
	return fmt.Sprintf(`.room-outline{fill:none;stroke:#000;stroke-opacity:.3;stroke-width:%.2f;stroke-linecap:square}
.room-label{font:%.1fpx sans-serif;text-anchor:middle;fill:#303030}
`, s.stroke/ps, svgLabelSize(ps))
}
