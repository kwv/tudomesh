package mesh

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"math"
)

// RenderRoomSyncSVG draws robot map m in the reference frame (toRef) for
// reviewing a room sync plan: each segment in the color of the reference room
// it matches (refColors, as on the live map), unmatched segments grey, cells
// whose reference room differs from their segment's in red, the robot's
// walls, the cut lines of split ops, and each segment's id and name.
func RenderRoomSyncSVG(w io.Writer, m *ValetudoMap, toRef AffineMatrix, refColors map[string]color.NRGBA,
	assigned map[string]string, mismatch []int, ops []RoomOp, title string) error {
	ps := pixelSizeOf(m)
	f := newSVGFrame(500, 0)
	f.growMap(m, toRef)
	f.finish()
	s := f.sizes()
	font := s.radius * 1.6

	var css bytes.Buffer
	fmt.Fprintf(&css, `.background{fill:#fff}
.floor,.room{fill:#c8c8c8}
.wall{fill:#505050}
.mismatch{fill:#e61e1e;fill-opacity:.55}
.cut{stroke:#000;stroke-width:%.1f;stroke-dasharray:%.1f %.1f;fill:none}
.label{font:bold %.1fpx sans-serif;text-anchor:middle;fill:#222;paint-order:stroke;stroke:#fff;stroke-width:%.1f}
.title{font:bold %.1fpx sans-serif;fill:#222}
`, s.stroke*3, s.radius, s.radius*0.6, font, font*0.25, font*1.2)
	for _, seg := range sortedKeys(assigned) {
		if c, ok := refColors[assigned[seg]]; ok {
			fmt.Fprintf(&css, "#room-%s{fill:%s}\n", cssToken(seg), svgColor(c))
		}
	}

	out := &bytes.Buffer{}
	writeSVGOpen(out, f, ps, css.String(), nil)
	fmt.Fprintf(out, `<g id="map" transform="%s">`+"\n", f.mapMatrix(m, toRef))
	writeMapLayers(out, m, true, nil)
	if d := cellRectsPath(mismatch); d != "" {
		fmt.Fprintf(out, `<path class="mismatch" d="%s"/>`+"\n", d)
	}
	out.WriteString("</g>\n")

	canvas := func(grid Point) [2]float64 {
		tp := TransformPoint(grid, toRef)
		return f.toCanvas(Point{X: tp.X * ps, Y: tp.Y * ps})
	}

	out.WriteString(`<g id="cuts">`)
	for _, op := range ops {
		if op.Action != "split_segment" || op.PA == nil || op.PB == nil {
			continue
		}
		a := canvas(Point{X: op.PA.X / ps, Y: op.PA.Y / ps})
		b := canvas(Point{X: op.PB.X / ps, Y: op.PB.Y / ps})
		fmt.Fprintf(out, `<line class="cut" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`, a[0], a[1], b[0], b[1])
	}
	out.WriteString("</g>\n")

	out.WriteString(`<g id="labels">`)
	for _, layer := range m.Layers {
		if layer.Type != "segment" || len(layer.Pixels) < 2 {
			continue
		}
		var sx, sy float64
		n := float64(len(layer.Pixels) / 2)
		for i := 0; i+1 < len(layer.Pixels); i += 2 {
			sx += float64(layer.Pixels[i])
			sy += float64(layer.Pixels[i+1])
		}
		c := canvas(Point{X: sx / n, Y: sy / n})
		label := layer.MetaData.SegmentID
		if layer.MetaData.Name != "" {
			label += " " + layer.MetaData.Name
		}
		fmt.Fprintf(out, `<text class="label" x="%.1f" y="%.1f">%s</text>`, c[0], c[1], svgEscape(label))
	}
	out.WriteString("</g>\n")

	fmt.Fprintf(out, `<text class="title" x="%.1f" y="%.1f">%s</text>`+"\n",
		font, math.Max(font*1.5, f.padding/2), svgEscape(title))
	out.WriteString("</svg>\n")
	_, err := out.WriteTo(w)
	return err
}
