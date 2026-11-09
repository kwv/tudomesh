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

// RoomColors gives each segment of m a palette color. Segments are taken in
// id order (numeric ids sort numerically) so colors stay stable between renders.
func RoomColors(m *ValetudoMap) map[string]color.NRGBA {
	var ids []string
	for _, layer := range m.Layers {
		if layer.Type == "segment" {
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
// "" if pos is not in or near a room.
func roomUnder(m *ValetudoMap, transform AffineMatrix, pos *LivePosition) string {
	if m == nil || pos == nil {
		return ""
	}
	ps := pixelSizeOf(m)
	local := TransformPoint(Point{X: pos.X, Y: pos.Y}, InvertMatrix(transform))
	room, ok := RoomAt(m, Point{X: local.X * ps, Y: local.Y * ps}, 0)
	if !ok {
		return ""
	}
	return room.SegmentID
}
