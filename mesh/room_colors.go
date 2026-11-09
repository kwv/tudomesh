package mesh

import (
	"image/color"
	"sort"
)

// roomPalette holds light room fills for the live map. They stay pale so
// walls, vacuums and the alert room (AlertRoomColor) stand out.
var roomPalette = []color.NRGBA{
	{207, 224, 240, 255}, // blue
	{214, 234, 210, 255}, // green
	{243, 227, 195, 255}, // sand
	{226, 214, 238, 255}, // lavender
	{210, 236, 234, 255}, // teal
	{239, 232, 200, 255}, // straw
}

// AlertRoomColor fills the room the highlighted vacuum is in.
var AlertRoomColor = color.NRGBA{244, 163, 163, 255}

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

// RoomColors gives each segment of m a palette color. Segments are taken in
// id order (numeric ids sort numerically) so colors stay stable between renders.
// Segments whose name is in ignore (IsIgnoredRoom) get no color and no
// palette slot.
func RoomColors(m *ValetudoMap, ignore ...string) map[string]color.NRGBA {
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
	colors := make(map[string]color.NRGBA, len(ids))
	for i, id := range ids {
		colors[id] = roomPalette[i%len(roomPalette)]
	}
	return colors
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
