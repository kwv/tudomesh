package mesh

import (
	"image/color"
	"math"
	"sort"
)

// roomPalette and AlertRoomColor are the light theme's room and alert fills.
var (
	roomPalette    = LightTheme.Rooms
	AlertRoomColor = LightTheme.Alert
)

// IsIgnoredRoom reports whether a segment named name is listed in ignore
// (Config.IgnoreRooms), compared with NormalizeRoomName. Ignored rooms are
// drawn as plain floor: no fill color, no label, no alert highlight. name is
// the segment's own name (LayerMetaData.Name); an empty name never matches.
func IsIgnoredRoom(name string, ignore []string) bool {
	n := NormalizeRoomName(name)
	if n == "" {
		return false
	}
	for _, ig := range ignore {
		if NormalizeRoomName(ig) == n {
			return true
		}
	}
	return false
}

// RoomColors gives each segment of m its light-theme fill (see
// roomIndexes and Theme.RoomColors).
func RoomColors(m *ValetudoMap, ignore ...string) map[string]color.NRGBA {
	return LightTheme.RoomColors(m, ignore...)
}

// roomIndexes gives each segment of m a palette slot. Segments are taken in
// id order (numeric ids sort numerically) so colors stay stable between
// renders. Segments whose name is in ignore (IsIgnoredRoom) get no slot.
func roomIndexes(m *ValetudoMap, ignore ...string) map[string]int {
	var ids []string
	for _, layer := range m.Layers {
		if layer.Type == "segment" && !IsIgnoredRoom(layer.MetaData.Name, ignore) {
			ids = append(ids, layer.MetaData.SegmentID)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) < len(ids[j])
		}
		return ids[i] < ids[j]
	})
	out := make(map[string]int, len(ids))
	for i, id := range ids {
		out[id] = i
	}
	return out
}

// roomUnder returns the segment id of m's room at pos, a position in
// reference-grid cells; transform maps m's grid to the reference grid.
// "" if pos is not in or near a room, or that room is in ignore.
func roomUnder(m *ValetudoMap, transform AffineMatrix, pos *LivePosition, ignore ...string) string {
	if m == nil || pos == nil {
		return ""
	}
	ps := pixelSizeOf(m)
	local := TransformPoint(Point{X: pos.X, Y: pos.Y}, InvertMatrix(transform))
	room, ok := RoomAt(m, Point{X: local.X * ps, Y: local.Y * ps}, 0)
	if !ok || IsIgnoredRoom(segmentName(m, room.SegmentID), ignore) {
		return ""
	}
	return room.SegmentID
}

// segmentName returns the name of m's segment with id seg ("" if unnamed or
// not found).
func segmentName(m *ValetudoMap, seg string) string {
	for _, layer := range m.Layers {
		if layer.Type == "segment" && layer.MetaData.SegmentID == seg {
			return layer.MetaData.Name
		}
	}
	return ""
}

// alertRoom is the room highlighted for a vacuum on the live/alert map.
type alertRoom struct {
	SegmentID string // segment id in the map it came from
	Name      string // room name, "" if the segment is unnamed
	Vacuum    string // the highlighted vacuum's id
	Pixels    []int  // [x1,y1,...] cells in the reference map's grid
}

// alertShape picks the room to highlight for vacuum vacID at pos (reference
// grid cells): the vacuum's own segment under pos, mapped into refID's grid
// through transforms (grid -> reference grid). It falls back to refID's
// segment under pos when vacID is refID, is missing from transforms, is not
// in calibrated (nil trusts every transform), or has no segment there.
// Rooms named in ignore (see IsIgnoredRoom) are never highlighted.
// Returns nil when neither map has a room at pos.
func alertShape(maps map[string]*ValetudoMap, transforms map[string]AffineMatrix, calibrated map[string]bool,
	refID, vacID string, pos *LivePosition, ignore ...string) *alertRoom {
	ref := maps[refID]
	if ref == nil || pos == nil {
		return nil
	}
	refT, ok := transforms[refID]
	if !ok {
		refT = Identity()
	}
	robotT, ok := transforms[vacID]
	trusted := ok && vacID != refID && (calibrated == nil || calibrated[vacID])
	if robot := maps[vacID]; trusted && robot != nil {
		// On a room of its own that is ignored (e.g. a deck): no highlight,
		// rather than a nearby reference room.
		if seg := roomUnder(robot, robotT, pos); seg != "" && IsIgnoredRoom(segmentName(robot, seg), ignore) {
			return nil
		}
		if room := ownRoomShape(robot, robotT, refT, pos, ignore...); room != nil {
			room.Vacuum = vacID
			return room
		}
	}
	seg := roomUnder(ref, refT, pos, ignore...)
	if seg == "" {
		return nil
	}
	if layer := segmentLayer(ref, seg); layer != nil {
		return &alertRoom{SegmentID: seg, Name: layer.MetaData.Name, Vacuum: vacID, Pixels: layer.Pixels}
	}
	return nil
}

// segmentLayer returns m's segment layer with id seg, or nil.
func segmentLayer(m *ValetudoMap, seg string) *MapLayer {
	for i := range m.Layers {
		if l := &m.Layers[i]; l.Type == "segment" && l.MetaData.SegmentID == seg {
			return l
		}
	}
	return nil
}

// ownRoomShape maps robot's segment under pos into the reference map's grid.
// robotT and refT take each map's grid to the shared reference grid. Each
// reference cell in the segment's bounding box is sampled at its center and
// mapped back into the robot grid, so rotated shapes have no holes.
func ownRoomShape(robot *ValetudoMap, robotT, refT AffineMatrix, pos *LivePosition, ignore ...string) *alertRoom {
	seg := roomUnder(robot, robotT, pos, ignore...)
	if seg == "" {
		return nil
	}
	layer := segmentLayer(robot, seg)
	if layer == nil || len(layer.Pixels) < 2 {
		return nil
	}

	px := layer.Pixels
	minX, minY, maxX, maxY := px[0], px[1], px[0], px[1]
	for i := 0; i+1 < len(px); i += 2 {
		minX, maxX = min(minX, px[i]), max(maxX, px[i])
		minY, maxY = min(minY, px[i+1]), max(maxY, px[i+1])
	}
	w, h := maxX-minX+1, maxY-minY+1
	in := make([]bool, w*h)
	for i := 0; i+1 < len(px); i += 2 {
		in[(px[i+1]-minY)*w+px[i]-minX] = true
	}

	toRef := MultiplyMatrices(InvertMatrix(refT), robotT)
	toRobot := InvertMatrix(toRef)
	bx0, by0, bx1, by1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range []Point{
		{X: float64(minX), Y: float64(minY)}, {X: float64(maxX + 1), Y: float64(minY)},
		{X: float64(minX), Y: float64(maxY + 1)}, {X: float64(maxX + 1), Y: float64(maxY + 1)},
	} {
		p := TransformPoint(c, toRef)
		bx0, by0 = math.Min(bx0, p.X), math.Min(by0, p.Y)
		bx1, by1 = math.Max(bx1, p.X), math.Max(by1, p.Y)
	}

	var out []int
	for qy := int(math.Floor(by0)) - 1; qy <= int(math.Ceil(by1)); qy++ {
		for qx := int(math.Floor(bx0)) - 1; qx <= int(math.Ceil(bx1)); qx++ {
			p := TransformPoint(Point{X: float64(qx) + 0.5, Y: float64(qy) + 0.5}, toRobot)
			x, y := int(math.Floor(p.X))-minX, int(math.Floor(p.Y))-minY
			if x >= 0 && x < w && y >= 0 && y < h && in[y*w+x] {
				out = append(out, qx, qy)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return &alertRoom{SegmentID: seg, Name: layer.MetaData.Name, Pixels: out}
}
