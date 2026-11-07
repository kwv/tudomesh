package mesh

import (
	"testing"
	"time"
)

func locateMap() *ValetudoMap {
	return &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "1", Name: "Kitchen"}, Pixels: square(0, 0, 20, 20)},
		{Type: "segment", MetaData: LayerMetaData{SegmentID: "2"}, Pixels: square(40, 0, 20, 20)},
		{Type: "wall", Pixels: []int{30, 5}},
	}}
}

func TestRoomAt(t *testing.T) {
	m := locateMap()
	tests := []struct {
		name    string
		p       Point // entity units (grid * 5)
		want    string
		wantOK  bool
		minDist float64
	}{
		{"inside kitchen", Point{X: 50, Y: 50}, "Kitchen", true, 0},
		{"inside unnamed segment", Point{X: 250, Y: 50}, "segment 2", true, 0},
		{"on wall between rooms, nearer kitchen", Point{X: 120, Y: 25}, "Kitchen", true, 1},
		{"far away", Point{X: 2000, Y: 2000}, "", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := RoomAt(m, tt.p, 0)
			if ok != tt.wantOK || got.Name != tt.want {
				t.Fatalf("RoomAt = %+v ok=%v, want %q ok=%v", got, ok, tt.want, tt.wantOK)
			}
			if ok && got.Distance < tt.minDist {
				t.Errorf("distance %.1f, want >= %.1f", got.Distance, tt.minDist)
			}
		})
	}
	if _, ok := RoomAt(nil, Point{}, 0); ok {
		t.Error("nil map should return ok=false")
	}
}

func TestStateTrackerStatus(t *testing.T) {
	st := NewStateTracker()
	if _, ok := st.GetStatus("v"); ok {
		t.Fatal("expected no status")
	}
	st.UpdateStatus("v", "cleaning")
	first, _ := st.GetStatus("v")
	time.Sleep(2 * time.Millisecond)
	st.UpdateStatus("v", "cleaning")
	same, _ := st.GetStatus("v")
	if !same.Since.Equal(first.Since) {
		t.Error("Since should not change when state repeats")
	}
	st.UpdateStatus("v", "error")
	changed, _ := st.GetStatus("v")
	if changed.State != "error" || !changed.Since.After(first.Since) {
		t.Errorf("got %+v after change", changed)
	}
}
