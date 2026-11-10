package mesh

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Crop limits an alert image to the highlighted vacuum's surroundings. The
// zero value means no crop. Lengths are world units (cm).
type Crop struct {
	Room   bool    // fit the vacuum's room plus CropRoomMargin; Radius if the room is unknown
	Radius float64 // half the side of the square around the vacuum
}

const (
	CropDefaultRadius = 400.0 // 4 m; also the fallback for crop=room
	CropRoomMargin    = 100.0 // 1 m around the room
	cropMinSide       = 400.0 // smallest crop side, so a closet still shows context
	cropMaxRadius     = 5000.0
)

// ParseCrop parses the ?crop= query value: "" (off), "room", or a radius
// with an "m" or "cm" suffix ("4m", "250cm"); a bare number is metres.
func ParseCrop(s string) (Crop, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "":
		return Crop{}, nil
	case "room":
		return Crop{Room: true, Radius: CropDefaultRadius}, nil
	}
	unit := 100.0
	num := s
	switch {
	case strings.HasSuffix(s, "cm"):
		unit, num = 1, strings.TrimSuffix(s, "cm")
	case strings.HasSuffix(s, "m"):
		num = strings.TrimSuffix(s, "m")
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || v <= 0 || math.IsInf(v, 0) || v*unit > cropMaxRadius {
		return Crop{}, fmt.Errorf("crop must be \"room\" or a radius like 4m or 250cm (max %gm), got %q", cropMaxRadius/100, s)
	}
	return Crop{Radius: v * unit}, nil
}

// Enabled reports whether c crops anything.
func (c Crop) Enabled() bool { return c.Room || c.Radius > 0 }

// cropPoints returns the reference-grid points the crop must contain and the
// margin to add around them, in grid cells. ps is the reference pixel size.
// ok is false when there is nothing to crop around (no position).
func (c Crop) cropPoints(alert *alertRoom, refT AffineMatrix, pos *LivePosition, ps float64) (pts []Point, margin float64, ok bool) {
	if !c.Enabled() || pos == nil {
		return nil, 0, false
	}
	center := Point{X: pos.X, Y: pos.Y}
	if c.Room && alert != nil && len(alert.Pixels) >= 2 {
		pts = append(pts, center)
		for _, p := range PixelsToPoints(alert.Pixels) {
			pts = append(pts, TransformPoint(p, refT), TransformPoint(Point{X: p.X + 1, Y: p.Y + 1}, refT))
		}
		return pts, CropRoomMargin / ps, true
	}
	r := c.Radius
	if r <= 0 {
		r = CropDefaultRadius
	}
	r /= ps
	return []Point{
		{X: center.X - r, Y: center.Y - r}, {X: center.X + r, Y: center.Y - r},
		{X: center.X - r, Y: center.Y + r}, {X: center.X + r, Y: center.Y + r},
	}, 0, true
}

// cropRect is an axis-aligned box in output (image or canvas) coordinates.
type cropRect struct{ minX, minY, maxX, maxY float64 }

// fitCrop maps pts to output coordinates with toOut, takes their bounding
// box, grows it by margin, widens each side to at least minSide around its
// center, shifts it inside [0,w]x[0,h] where it fits, and clips it there.
func fitCrop(pts []Point, toOut func(Point) (float64, float64), margin, minSide, w, h float64) cropRect {
	r := cropRect{math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64}
	for _, p := range pts {
		x, y := toOut(p)
		r.minX, r.minY = math.Min(r.minX, x), math.Min(r.minY, y)
		r.maxX, r.maxY = math.Max(r.maxX, x), math.Max(r.maxY, y)
	}
	r.minX, r.minY, r.maxX, r.maxY = r.minX-margin, r.minY-margin, r.maxX+margin, r.maxY+margin
	fit := func(lo, hi *float64, limit float64) {
		if d := minSide - (*hi - *lo); d > 0 {
			*lo, *hi = *lo-d/2, *hi+d/2
		}
		if *lo < 0 {
			*lo, *hi = 0, *hi-*lo
		}
		if *hi > limit {
			*lo, *hi = *lo-(*hi-limit), limit
		}
		*lo = math.Max(0, *lo)
	}
	fit(&r.minX, &r.maxX, w)
	fit(&r.minY, &r.maxY, h)
	return r
}
