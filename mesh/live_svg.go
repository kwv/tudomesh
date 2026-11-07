package mesh

import (
	"bytes"
	"encoding/xml"
	"fmt"
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

// Live SVG structure (stable ids/classes for CSS and scripts):
//
//	<style>                       default colors; override freely
//	<g id="map" transform=...>    map geometry in grid cells (1 unit = 1 Valetudo pixel)
//	  <g id="rooms">              one <path class="room" data-room data-segment-id> per segment,
//	                              plus <path class="floor"> for unsegmented floor
//	  <g id="walls">              one <path class="wall"> (filled cells)
//	<g id="grid">                 one dashed <path class="grid-line">
//	<g id="vacuums">              one <g id="vacuum-<id>" class="vacuum state-<state> [highlight]"
//	                              data-id data-name data-room data-state transform="translate(x y)">
//	                                <circle class="ring"> (highlight only) <circle class="body">
//	                                <line class="heading"> <text class="label">
//
// A room whose name matches the highlighted vacuum's room also gets class "alert-room".

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

// RenderLiveToSVG writes the live view: the largest map as a greyscale base
// (rooms, walls, grid) and every vacuum position as its own labelled group.
// Positions outside the base map expand the viewport.
func (r *VectorRenderer) RenderLiveToSVG(w io.Writer, positions map[string]*LivePosition) error {
	if len(r.Maps) == 0 {
		return fmt.Errorf("no maps available for live rendering")
	}

	baseID := SelectReferenceVacuum(r.Maps, nil)
	baseMap := r.Maps[baseID]
	baseTransform := r.Transforms[baseID]
	ps := float64(baseMap.PixelSize)
	if ps <= 0 {
		ps = 5
	}
	toWorld := func(p Point) Point {
		t := TransformPoint(p, baseTransform)
		return Point{X: t.X * ps, Y: t.Y * ps}
	}

	// Bounds from base map geometry plus vacuum positions (world units).
	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64
	grow := func(p Point) {
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}
	for _, layer := range baseMap.Layers {
		if layer.Type == "floor" || layer.Type == "segment" || layer.Type == "wall" {
			for _, p := range PixelsToPoints(layer.Pixels) {
				grow(toWorld(p))
			}
		}
	}
	for _, pos := range positions {
		grow(Point{X: pos.X * ps, Y: pos.Y * ps})
	}
	if minX > maxX {
		minX, minY, maxX, maxY = 0, 0, 0, 0
	}
	centerX, centerY := (minX+maxX)/2, (minY+maxY)/2
	width := (maxX - minX) + 2*r.Padding
	height := (maxY - minY) + 2*r.Padding

	toCanvas := func(p Point) [2]float64 {
		rp := r.applyGlobalRotation(p, centerX, centerY)
		return [2]float64{rp.X - minX + r.Padding, rp.Y - minY + r.Padding}
	}

	// Indicator sizes scale with the shorter map side.
	span := math.Max(1, math.Min(maxX-minX, maxY-minY))
	radius := span * 0.012
	stroke := radius * 0.15

	highlightRoom := ""
	if m, ok := r.Meta[r.Highlight]; ok && m.Room != "" {
		highlightRoom = NormalizeRoomName(m.Room)
	}

	out := &bytes.Buffer{}
	fmt.Fprintf(out, `<svg xmlns="http://www.w3.org/2000/svg" version="1.1" viewBox="0 0 %.1f %.1f" width="%.0f" height="%.0f">`+"\n",
		width, height, width/ps, height/ps)
	fmt.Fprintf(out, `<style>
.background{fill:#fff}
.floor{fill:#c8c8c8}
.room{fill:#c8c8c8}
.room.alert-room{fill:#f6c6c6}
.wall{fill:#505050}
.grid-line{fill:none;stroke:#999;stroke-width:%.1f;stroke-dasharray:%.1f %.1f;opacity:.5}
.vacuum .body{stroke:#000;stroke-width:%.1f}
.vacuum .heading{stroke-width:%.1f;stroke-linecap:round}
.vacuum .label{font:bold %.1fpx sans-serif;text-anchor:middle;fill:#222;paint-order:stroke;stroke:#fff;stroke-width:%.1f}
.vacuum .ring{fill:none;stroke:#e61e1e;stroke-width:%.1f}
.vacuum.highlight .ring{animation:tudomesh-pulse 1.5s ease-in-out infinite}
@keyframes tudomesh-pulse{50%%{opacity:.25}}
</style>
`, stroke, radius, radius, stroke, stroke*1.5, radius*1.6, radius*0.4, radius*0.35)
	fmt.Fprintf(out, `<rect class="background" width="%.1f" height="%.1f"/>`+"\n", width, height)

	// Map geometry is written in grid units inside a group whose transform
	// maps grid -> canvas (base calibration, pixel size, global rotation).
	o, ex, ey := toCanvas(toWorld(Point{})), toCanvas(toWorld(Point{X: 1})), toCanvas(toWorld(Point{Y: 1}))
	fmt.Fprintf(out, `<g id="map" transform="matrix(%.5f %.5f %.5f %.5f %.2f %.2f)">`+"\n",
		ex[0]-o[0], ex[1]-o[1], ey[0]-o[0], ey[1]-o[1], o[0], o[1])

	out.WriteString(`<g id="rooms">` + "\n")
	for _, layer := range baseMap.Layers {
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
		if highlightRoom != "" && NormalizeRoomName(layer.MetaData.Name) == highlightRoom {
			class += " alert-room"
		}
		fmt.Fprintf(out, `<path id="room-%s" class="%s" data-room="%s" data-segment-id="%s" d="%s"/>`+"\n",
			svgEscape(cssToken(layer.MetaData.SegmentID)), class, svgEscape(layer.MetaData.Name),
			svgEscape(layer.MetaData.SegmentID), d)
	}
	out.WriteString("</g>\n")

	out.WriteString(`<g id="walls">`)
	for _, layer := range baseMap.Layers {
		if layer.Type == "wall" {
			if d := cellRectsPath(layer.Pixels); d != "" {
				fmt.Fprintf(out, `<path class="wall" d="%s"/>`, d)
			}
		}
	}
	out.WriteString("</g>\n</g>\n")

	// Grid.
	out.WriteString(`<g id="grid">`)
	if r.GridSpacing > 0 {
		var g strings.Builder
		for x := math.Floor(minX/r.GridSpacing) * r.GridSpacing; x <= maxX; x += r.GridSpacing {
			svgPathData(&g, [][2]float64{toCanvas(Point{X: x, Y: minY}), toCanvas(Point{X: x, Y: maxY})}, false)
		}
		for y := math.Floor(minY/r.GridSpacing) * r.GridSpacing; y <= maxY; y += r.GridSpacing {
			svgPathData(&g, [][2]float64{toCanvas(Point{X: minX, Y: y}), toCanvas(Point{X: maxX, Y: y})}, false)
		}
		fmt.Fprintf(out, `<path class="grid-line" d="%s"/>`, g.String())
	}
	out.WriteString("</g>\n")

	// Vacuums, sorted for stable output.
	ids := make([]string, 0, len(positions))
	for id := range positions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out.WriteString(`<g id="vacuums">` + "\n")
	for _, id := range ids {
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
		c := toCanvas(Point{X: pos.X * ps, Y: pos.Y * ps})
		color := pos.Color
		if color == "" {
			color = "#ff0000"
		}
		rad := (pos.Angle + r.GlobalRotation) * math.Pi / 180

		fmt.Fprintf(out, `<g id="vacuum-%s" class="%s" data-id="%s" data-name="%s" data-room="%s" data-state="%s" transform="translate(%.1f %.1f)">`,
			svgEscape(cssToken(id)), class, svgEscape(id), svgEscape(name), svgEscape(meta.Room), svgEscape(meta.State), c[0], c[1])
		if highlighted {
			fmt.Fprintf(out, `<circle class="ring" r="%.1f"/>`, radius*2.5)
		}
		fmt.Fprintf(out, `<circle class="body" r="%.1f" fill="%s"/>`, radius, svgEscape(color))
		fmt.Fprintf(out, `<line class="heading" x2="%.1f" y2="%.1f" stroke="%s"/>`,
			radius*1.75*math.Cos(rad), radius*1.75*math.Sin(rad), svgEscape(color))
		fmt.Fprintf(out, `<text class="label" y="%.1f">%s</text>`, -radius*1.5, svgEscape(name))
		out.WriteString("</g>\n")
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
