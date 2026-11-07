package mesh

import "math"

// RoomLocation is the segment a point falls in on one vacuum's own map.
type RoomLocation struct {
	Name      string  `json:"name"`      // segment name, or "segment <id>" when unnamed
	SegmentID string  `json:"segmentId"` // Valetudo segment id
	Distance  float64 `json:"distance"`  // to the nearest pixel of that segment, entity units (0 = inside)
}

// RoomAt returns the segment of m nearest to p, searching up to maxDist away
// (robots often sit on a wall or doorway edge rather than inside a segment).
// p and maxDist are in entity units (e.g. a robot_position entity), the same
// units as RoomLocation.Distance. maxDist <= 0 uses 10 pixels.
// ok is false if no segment is in range.
func RoomAt(m *ValetudoMap, p Point, maxDist float64) (RoomLocation, bool) {
	if m == nil {
		return RoomLocation{}, false
	}
	ps := float64(m.PixelSize)
	if ps <= 0 {
		ps = 5
	}
	if maxDist <= 0 {
		maxDist = 10 * ps
	}
	// Layer pixels are grid indices; compare in entity units.

	best := RoomLocation{Distance: math.Inf(1)}
	for _, layer := range m.Layers {
		if layer.Type != "segment" {
			continue
		}
		px := layer.Pixels
		for i := 0; i+1 < len(px); i += 2 {
			d := math.Hypot(float64(px[i])*ps-p.X, float64(px[i+1])*ps-p.Y)
			if d < best.Distance {
				best.Distance = d
				best.SegmentID = layer.MetaData.SegmentID
				best.Name = layer.MetaData.Name
			}
		}
	}
	if math.IsInf(best.Distance, 1) || best.Distance > maxDist {
		return RoomLocation{}, false
	}
	// A pixel marks a cell corner; anything within one cell is inside.
	if best.Distance < ps {
		best.Distance = 0
	}
	if best.Name == "" {
		best.Name = "segment " + best.SegmentID
	}
	return best, true
}
