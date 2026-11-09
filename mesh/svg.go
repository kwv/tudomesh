package mesh

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image/color"
	"io"
	"math"
	"sort"
	"strings"
)

// VacuumMeta is optional per-vacuum context written into the live SVG as
// data attributes and classes (set VectorRenderer.Meta).
type VacuumMeta struct {
	Name  string // display label; defaults to the vacuum id
	Room  string // room the vacuum is in
	State string // last Valetudo status, e.g. "error"
}

// SVG structure (stable ids/classes for CSS and scripts).
//
// Live / floorplan view (RenderLiveToSVG): the largest map, greyscale.
//
//	<style>                       default colors; override freely
//	<g id="map" transform=...>    map geometry in grid cells (1 unit = 1 Valetudo pixel)
//	  <g id="rooms">              one <path class="room" data-room data-segment-id> per segment,
//	                              plus <path class="floor"> for unsegmented floor
//	  <path id="alert-room">      highlighted vacuum's room (class "alert-room", data-room
//	                              data-segment-id data-vacuum); only with a highlight
//	  <g id="walls">              one <path class="wall"> (filled cells)
//	  <g id="room-outlines">      one <path class="room-outline" data-segment-id> per segment
//	<g id="grid">                 one dashed <path class="grid-line">
//	<g id="room-labels">          one <text class="room-label" data-segment-id> per named
//	                              room whose name fits inside it, in canvas coordinates
//	<g id="vacuums">              one <g id="vacuum-<id>" class="vacuum state-<state> [highlight]"
//	                              data-id data-name data-room data-state transform="translate(x y)">
//	                                <circle class="ring"> (highlight only) <circle class="body">
//	                                <line class="heading"> <text class="label">
//
// Each room has its own light fill (RoomColors). The alert room is the
// highlighted vacuum's own segment under its position, mapped into the base
// map's grid, or the base map's room there when the vacuum is the base map,
// uncalibrated, or has no segment there (see alertShape). Its outline traces
// the room's boundary, so CSS strokes show the shape, not the cell runs.
//
// Composite view (RenderCompositeToSVG): every map overlaid in its own color.
//
//	<g id="maps">                 one <g id="map-<id>" class="map" data-id transform=...> per vacuum,
//	                              reference first; each holds <g class="rooms"> and <g class="walls">
//	                              with the same room/floor/wall paths as above; colors are
//	                              per-map rules in <style> (#map-<id> .rooms / .walls)
//	<g id="grid">
//	<g id="chargers">             one <circle class="charger" data-id> per vacuum with a dock

func svgEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// cssToken makes a value safe for use inside a class name.
func cssToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

func svgPathData(b *strings.Builder, pts [][2]float64, closed bool) {
	for i, p := range pts {
		if i == 0 {
			fmt.Fprintf(b, "M%.1f %.1f", p[0], p[1])
		} else {
			fmt.Fprintf(b, "L%.1f %.1f", p[0], p[1])
		}
	}
	if closed {
		b.WriteString("Z")
	}
}

func svgColor(c color.NRGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func pixelSizeOf(m *ValetudoMap) float64 {
	if m.PixelSize > 0 {
		return float64(m.PixelSize)
	}
	return 5
}

// svgFrame maps world coordinates to SVG canvas coordinates: bounds plus
// padding, with the global rotation applied around the bounds' center.
type svgFrame struct {
	minX, minY, maxX, maxY float64
	padding, rotation      float64
}

func newSVGFrame(padding, rotation float64) *svgFrame {
	return &svgFrame{
		minX: math.MaxFloat64, minY: math.MaxFloat64,
		maxX: -math.MaxFloat64, maxY: -math.MaxFloat64,
		padding: padding, rotation: rotation,
	}
}

func (f *svgFrame) grow(p Point) {
	f.minX, f.minY = math.Min(f.minX, p.X), math.Min(f.minY, p.Y)
	f.maxX, f.maxY = math.Max(f.maxX, p.X), math.Max(f.maxY, p.Y)
}

// growMap extends the bounds by every floor, segment and wall cell of m.
func (f *svgFrame) growMap(m *ValetudoMap, t AffineMatrix) {
	ps := pixelSizeOf(m)
	for _, layer := range m.Layers {
		if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
			for _, p := range PixelsToPoints(layer.Pixels) {
				tp := TransformPoint(p, t)
				f.grow(Point{X: tp.X * ps, Y: tp.Y * ps})
			}
		}
	}
}

func (f *svgFrame) finish() {
	if f.minX > f.maxX {
		f.minX, f.minY, f.maxX, f.maxY = 0, 0, 0, 0
	}
}

func (f *svgFrame) size() (w, h float64) {
	return f.maxX - f.minX + 2*f.padding, f.maxY - f.minY + 2*f.padding
}

func (f *svgFrame) toCanvas(p Point) [2]float64 {
	if f.rotation != 0 {
		cx, cy := (f.minX+f.maxX)/2, (f.minY+f.maxY)/2
		rad := f.rotation * math.Pi / 180
		x, y := p.X-cx, p.Y-cy
		p = Point{X: x*math.Cos(rad) - y*math.Sin(rad) + cx, Y: x*math.Sin(rad) + y*math.Cos(rad) + cy}
	}
	return [2]float64{p.X - f.minX + f.padding, p.Y - f.minY + f.padding}
}

// mapMatrix is the SVG transform taking m's grid cells to the canvas.
func (f *svgFrame) mapMatrix(m *ValetudoMap, t AffineMatrix) string {
	ps := pixelSizeOf(m)
	world := func(p Point) Point {
		tp := TransformPoint(p, t)
		return Point{X: tp.X * ps, Y: tp.Y * ps}
	}
	o, ex, ey := f.toCanvas(world(Point{})), f.toCanvas(world(Point{X: 1})), f.toCanvas(world(Point{Y: 1}))
	return fmt.Sprintf("matrix(%.5f %.5f %.5f %.5f %.2f %.2f)",
		ex[0]-o[0], ex[1]-o[1], ey[0]-o[0], ey[1]-o[1], o[0], o[1])
}

// svgSizes are stroke and marker sizes scaled to the shorter map side.
type svgSizes struct{ radius, stroke float64 }

func (f *svgFrame) sizes() svgSizes {
	span := math.Max(1, math.Min(f.maxX-f.minX, f.maxY-f.minY))
	r := span * 0.012
	return svgSizes{radius: r, stroke: r * 0.15}
}

func writeSVGOpen(out *bytes.Buffer, f *svgFrame, ps float64, css string) {
	w, h := f.size()
	fmt.Fprintf(out, `<svg xmlns="http://www.w3.org/2000/svg" version="1.1" viewBox="0 0 %.1f %.1f" width="%.0f" height="%.0f">`+"\n",
		w, h, w/ps, h/ps)
	out.WriteString("<style>\n" + css + "</style>\n")
	fmt.Fprintf(out, `<rect class="background" width="%.1f" height="%.1f"/>`+"\n", w, h)
}

// writeMapLayers writes room/floor and wall paths for one map. single gives
// the single-map ids (rooms, walls, room-<seg>); otherwise groups use classes
// so several maps can share a document. alert, if set, is written as
// <path id="alert-room"> between the rooms and the walls (cells in m's grid).
func writeMapLayers(out *bytes.Buffer, m *ValetudoMap, single bool, alert *alertRoom) {
	groupAttr := func(name string) string {
		if single {
			return fmt.Sprintf(`id="%s"`, name)
		}
		return fmt.Sprintf(`class="%s"`, name)
	}
	fmt.Fprintf(out, "<g %s>\n", groupAttr("rooms"))
	for _, layer := range m.Layers {
		if layer.Type != "floor" && layer.Type != "segment" {
			continue
		}
		d := cellRectsPath(layer.Pixels)
		if d == "" {
			continue
		}
		if layer.Type == "floor" {
			fmt.Fprintf(out, `<path class="floor" d="%s"/>`+"\n", d)
			continue
		}
		class := "room"
		id := ""
		if single {
			id = fmt.Sprintf(`id="room-%s" `, svgEscape(cssToken(layer.MetaData.SegmentID)))
		}
		fmt.Fprintf(out, `<path %sclass="%s" data-room="%s" data-segment-id="%s" d="%s"/>`+"\n",
			id, class, svgEscape(layer.MetaData.Name), svgEscape(layer.MetaData.SegmentID), d)
	}
	out.WriteString("</g>\n")

	if alert != nil {
		if d := cellOutlinePath(alert.Pixels); d != "" {
			fmt.Fprintf(out, `<path id="alert-room" class="alert-room" data-room="%s" data-segment-id="%s" data-vacuum="%s" d="%s"/>`+"\n",
				svgEscape(alert.Name), svgEscape(alert.SegmentID), svgEscape(alert.Vacuum), d)
		}
	}

	fmt.Fprintf(out, "<g %s>", groupAttr("walls"))
	for _, layer := range m.Layers {
		if layer.Type == "wall" {
			if d := cellRectsPath(layer.Pixels); d != "" {
				fmt.Fprintf(out, `<path class="wall" d="%s"/>`, d)
			}
		}
	}
	out.WriteString("</g>\n")
}

func (r *VectorRenderer) writeGrid(out *bytes.Buffer, f *svgFrame) {
	out.WriteString(`<g id="grid">`)
	if r.GridSpacing > 0 {
		var g strings.Builder
		for x := math.Floor(f.minX/r.GridSpacing) * r.GridSpacing; x <= f.maxX; x += r.GridSpacing {
			svgPathData(&g, [][2]float64{f.toCanvas(Point{X: x, Y: f.minY}), f.toCanvas(Point{X: x, Y: f.maxY})}, false)
		}
		for y := math.Floor(f.minY/r.GridSpacing) * r.GridSpacing; y <= f.maxY; y += r.GridSpacing {
			svgPathData(&g, [][2]float64{f.toCanvas(Point{X: f.minX, Y: y}), f.toCanvas(Point{X: f.maxX, Y: y})}, false)
		}
		fmt.Fprintf(out, `<path class="grid-line" d="%s"/>`, g.String())
	}
	out.WriteString("</g>\n")
}

func gridCSS(s svgSizes) string {
	return fmt.Sprintf(".grid-line{fill:none;stroke:#999;stroke-width:%.1f;stroke-dasharray:%.1f %.1f;opacity:.5}\n",
		s.stroke, s.radius, s.radius)
}

// RenderLiveToSVG writes the live view: the reference map (the largest map
// when Reference has none) as a greyscale base (rooms, walls, grid) and every
// vacuum position as its own labelled group, matching the PNG live view.
// Positions outside the base map expand the viewport. With no positions it
// is the plain floorplan.
func (r *VectorRenderer) RenderLiveToSVG(w io.Writer, positions map[string]*LivePosition) error {
	if len(r.Maps) == 0 {
		return fmt.Errorf("no maps available for live rendering")
	}

	baseID := r.Reference
	if r.Maps[baseID] == nil {
		baseID = SelectReferenceVacuum(r.Maps, nil)
	}
	baseMap := r.Maps[baseID]
	baseTransform := r.Transforms[baseID]
	ps := pixelSizeOf(baseMap)

	f := newSVGFrame(r.Padding, r.GlobalRotation)
	f.growMap(baseMap, baseTransform)
	for _, pos := range positions {
		f.grow(Point{X: pos.X * ps, Y: pos.Y * ps})
	}
	f.finish()
	s := f.sizes()

	alert := alertShape(r.Maps, r.Transforms, r.Calibrated, baseID, r.Highlight, positions[r.Highlight], r.IgnoreRooms...)

	var roomCSS strings.Builder
	roomColors := RoomColors(baseMap, r.IgnoreRooms...)
	for _, seg := range sortedKeys(roomColors) {
		fmt.Fprintf(&roomCSS, "#room-%s{fill:%s}\n", cssToken(seg), svgColor(roomColors[seg]))
	}

	out := &bytes.Buffer{}
	writeSVGOpen(out, f, ps, fmt.Sprintf(`.background{fill:#fff}
.floor{fill:#c8c8c8}
.room{fill:#c8c8c8}
%s.alert-room{fill:%s;stroke:%s;stroke-width:1.2;stroke-dasharray:2.4 1.2}
.wall{fill:#505050}
%s%s.vacuum .body{stroke:#000;stroke-width:%.1f}
.vacuum .heading{stroke-width:%.1f;stroke-linecap:round}
.vacuum .label{font:bold %.1fpx sans-serif;text-anchor:middle;fill:#222;paint-order:stroke;stroke:#fff;stroke-width:%.1f}
.vacuum .ring{fill:none;stroke:#e61e1e;stroke-width:%.1f}
.vacuum.highlight .ring{animation:tudomesh-pulse 1.5s ease-in-out infinite}
@keyframes tudomesh-pulse{50%%{opacity:.25}}
`, roomCSS.String(), svgColor(AlertRoomColor), svgColor(color.NRGBA(alertBorderColor)), gridCSS(s), roomLabelCSS(s, ps), s.stroke, s.stroke*1.5, s.radius*1.6, s.radius*0.4, s.radius*0.35))

	fmt.Fprintf(out, `<g id="map" transform="%s">`+"\n", f.mapMatrix(baseMap, baseTransform))
	writeMapLayers(out, baseMap, true, alert)
	writeRoomOutlines(out, baseMap, r.IgnoreRooms...)
	out.WriteString("</g>\n")

	r.writeGrid(out, f)
	writeRoomLabels(out, f, baseMap, baseTransform, svgLabelSize(ps), r.IgnoreRooms...)

	out.WriteString(`<g id="vacuums">` + "\n")
	for _, id := range sortedKeys(positions) {
		pos := positions[id]
		meta := r.Meta[id]
		name := meta.Name
		if name == "" {
			name = id
		}
		class := "vacuum"
		if meta.State != "" {
			class += " state-" + cssToken(meta.State)
		}
		highlighted := id == r.Highlight
		if highlighted {
			class += " highlight"
		}
		c := f.toCanvas(Point{X: pos.X * ps, Y: pos.Y * ps})
		col := pos.Color
		if col == "" {
			col = "#ff0000"
		}
		rad := (pos.Angle + r.GlobalRotation) * math.Pi / 180

		fmt.Fprintf(out, `<g id="vacuum-%s" class="%s" data-id="%s" data-name="%s" data-room="%s" data-state="%s" transform="translate(%.1f %.1f)">`,
			svgEscape(cssToken(id)), class, svgEscape(id), svgEscape(name), svgEscape(meta.Room), svgEscape(meta.State), c[0], c[1])
		if highlighted {
			fmt.Fprintf(out, `<circle class="ring" r="%.1f"/>`, s.radius*2.5)
		}
		fmt.Fprintf(out, `<circle class="body" r="%.1f" fill="%s"/>`, s.radius, svgEscape(col))
		fmt.Fprintf(out, `<line class="heading" x2="%.1f" y2="%.1f" stroke="%s"/>`,
			s.radius*1.75*math.Cos(rad), s.radius*1.75*math.Sin(rad), svgEscape(col))
		fmt.Fprintf(out, `<text class="label" y="%.1f">%s</text>`, -s.radius*1.5, svgEscape(name))
		out.WriteString("</g>\n")
	}
	out.WriteString("</g>\n</svg>\n")
	_, err := out.WriteTo(w)
	return err
}

// RenderCompositeToSVG writes every map overlaid in its vacuum's color
// (r.Colors), the grid, and each vacuum's charger. Use it to check alignment.
func (r *VectorRenderer) RenderCompositeToSVG(w io.Writer) error {
	if len(r.Maps) == 0 {
		return fmt.Errorf("no maps available for composite rendering")
	}

	// Reference first (bottom layer), the rest sorted by id.
	ids := sortedKeys(r.Maps)
	sort.SliceStable(ids, func(i, j int) bool { return ids[i] == r.Reference && ids[j] != r.Reference })

	f := newSVGFrame(r.Padding, r.GlobalRotation)
	for _, id := range ids {
		f.growMap(r.Maps[id], r.Transforms[id])
	}
	f.finish()
	s := f.sizes()

	// Per-vacuum colors live in the stylesheet, one rule per selector.
	var css strings.Builder
	fmt.Fprintf(&css, ".background{fill:#fff}\n%s.charger{stroke:#000;stroke-width:%.1f}\n", gridCSS(s), s.stroke)
	for _, id := range ids {
		vc := r.Colors[id]
		sel := "#map-" + cssToken(id)
		fmt.Fprintf(&css, "%s .rooms{fill:%s;fill-opacity:%.2f}\n", sel, svgColor(vc.Floor), float64(vc.Floor.A)/255)
		fmt.Fprintf(&css, "%s .walls{fill:%s}\n", sel, svgColor(vc.Wall))
		fmt.Fprintf(&css, ".charger[data-id=\"%s\"]{fill:%s}\n", svgEscape(id), svgColor(vc.Wall))
	}

	ps := pixelSizeOf(r.Maps[ids[0]])
	out := &bytes.Buffer{}
	writeSVGOpen(out, f, ps, css.String())

	out.WriteString(`<g id="maps">` + "\n")
	for _, id := range ids {
		m := r.Maps[id]
		fmt.Fprintf(out, `<g id="map-%s" class="map" data-id="%s" transform="%s">`+"\n",
			svgEscape(cssToken(id)), svgEscape(id), f.mapMatrix(m, r.Transforms[id]))
		writeMapLayers(out, m, false, nil)
		out.WriteString("</g>\n")
	}
	out.WriteString("</g>\n")

	r.writeGrid(out, f)

	out.WriteString(`<g id="chargers">`)
	for _, id := range ids {
		m := r.Maps[id]
		cp, ok := ExtractChargerPosition(m)
		if !ok {
			continue
		}
		// Entity points are world units in the map's own frame; the
		// transform operates on grid cells.
		mps := pixelSizeOf(m)
		tp := TransformPoint(Point{X: cp.X / mps, Y: cp.Y / mps}, r.Transforms[id])
		c := f.toCanvas(Point{X: tp.X * mps, Y: tp.Y * mps})
		fmt.Fprintf(out, `<circle class="charger" data-id="%s" cx="%.1f" cy="%.1f" r="%.1f"/>`,
			svgEscape(id), c[0], c[1], s.radius*0.8)
	}
	out.WriteString("</g>\n</svg>\n")
	_, err := out.WriteTo(w)
	return err
}

// cellRectsPath turns grid pixels [x1,y1,...] into an SVG path of filled
// cells: runs along each row, merged downwards while consecutive rows repeat
// the same run. Exact and compact (integer coordinates, relative moves).
func cellRectsPath(pixels []int) string {
	if len(pixels) < 2 {
		return ""
	}
	rows := map[int][]int{}
	for i := 0; i+1 < len(pixels); i += 2 {
		rows[pixels[i+1]] = append(rows[pixels[i+1]], pixels[i])
	}
	ys := make([]int, 0, len(rows))
	for y := range rows {
		ys = append(ys, y)
	}
	sort.Ints(ys)

	type run struct{ x, w int }
	type rect struct{ x, y, w, h int }
	var done []rect
	open := map[run]*rect{} // runs that ended on the previous row
	prevY := math.MinInt
	for _, y := range ys {
		xs := rows[y]
		sort.Ints(xs)
		var runs []run
		for i := 0; i < len(xs); {
			j := i
			for j+1 < len(xs) && xs[j+1] <= xs[j]+1 {
				j++
			}
			runs = append(runs, run{xs[i], xs[j] - xs[i] + 1})
			i = j + 1
		}
		next := map[run]*rect{}
		for _, rn := range runs {
			if rc, ok := open[rn]; ok && prevY == y-1 {
				rc.h++
				next[rn] = rc
				delete(open, rn)
			} else {
				next[rn] = &rect{rn.x, y, rn.w, 1}
			}
		}
		for _, rc := range open {
			done = append(done, *rc)
		}
		open, prevY = next, y
	}
	for _, rc := range open {
		done = append(done, *rc)
	}
	sort.Slice(done, func(i, j int) bool {
		if done[i].y != done[j].y {
			return done[i].y < done[j].y
		}
		return done[i].x < done[j].x
	})

	var b strings.Builder
	for _, rc := range done {
		fmt.Fprintf(&b, "M%d %dh%dv%dh-%dz", rc.x, rc.y, rc.w, rc.h, rc.w)
	}
	return b.String()
}

// cellOutlinePath turns grid pixels [x1,y1,...] into an SVG path tracing the
// boundary of the cell set: one closed loop per outline (holes included),
// corners only. Outer loops run clockwise and holes counter-clockwise, so the
// default nonzero fill matches the cells and a stroke follows only the edge.
func cellOutlinePath(pixels []int) string {
	type pt struct{ x, y int }
	in := map[pt]bool{}
	var cs []pt
	for i := 0; i+1 < len(pixels); i += 2 {
		c := pt{pixels[i], pixels[i+1]}
		if !in[c] {
			in[c] = true
			cs = append(cs, c)
		}
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].y != cs[j].y {
			return cs[i].y < cs[j].y
		}
		return cs[i].x < cs[j].x
	})

	// Directed boundary edges, clockwise around each cell (y down).
	next := map[pt][]pt{}
	var starts []pt
	add := func(a, b pt) {
		if len(next[a]) == 0 {
			starts = append(starts, a)
		}
		next[a] = append(next[a], b)
	}
	for _, c := range cs {
		x, y := c.x, c.y
		if !in[pt{x, y - 1}] {
			add(pt{x, y}, pt{x + 1, y})
		}
		if !in[pt{x + 1, y}] {
			add(pt{x + 1, y}, pt{x + 1, y + 1})
		}
		if !in[pt{x, y + 1}] {
			add(pt{x + 1, y + 1}, pt{x, y + 1})
		}
		if !in[pt{x - 1, y}] {
			add(pt{x, y + 1}, pt{x, y})
		}
	}

	var b strings.Builder
	for _, start := range starts {
		for len(next[start]) > 0 {
			// Walk one loop; in-degree equals out-degree at every vertex,
			// so the walk closes at start.
			loop := []pt{start}
			for v := start; ; {
				w := next[v][0]
				next[v] = next[v][1:]
				if w == start {
					break
				}
				loop = append(loop, w)
				v = w
			}
			n := len(loop)
			corner := func(i int) bool {
				p, c, q := loop[(i+n-1)%n], loop[i], loop[(i+1)%n]
				return (c.x-p.x)*(q.y-c.y)-(c.y-p.y)*(q.x-c.x) != 0
			}
			first := -1
			for i := range loop {
				if corner(i) {
					first = i
					break
				}
			}
			if first < 0 {
				continue
			}
			c := loop[first]
			fmt.Fprintf(&b, "M%d %d", c.x, c.y)
			for k := 1; k < n; k++ {
				p := loop[(first+k)%n]
				if !corner((first + k) % n) {
					continue
				}
				if p.x != c.x {
					fmt.Fprintf(&b, "H%d", p.x)
				} else {
					fmt.Fprintf(&b, "V%d", p.y)
				}
				c = p
			}
			b.WriteString("z")
		}
	}
	return b.String()
}
