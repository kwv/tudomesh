package mesh

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// square returns pixels for a filled w x h block at (x0,y0), step 5.
func square(x0, y0, w, h int) []int {
	var px []int
	for y := y0; y < y0+h; y += 5 {
		for x := x0; x < x0+w; x += 5 {
			px = append(px, x, y)
		}
	}
	return px
}

func roomMap(rooms map[string][]int) *ValetudoMap {
	m := &ValetudoMap{PixelSize: 5}
	for name, px := range rooms {
		m.Layers = append(m.Layers, MapLayer{
			Type:     "segment",
			MetaData: LayerMetaData{Name: name},
			Pixels:   px,
		})
	}
	return m
}

func transformPixels(px []int, t AffineMatrix) []int {
	out := make([]int, 0, len(px))
	for _, p := range PixelsToPoints(px) {
		q := TransformPoint(p, t)
		out = append(out, int(math.Round(q.X)), int(math.Round(q.Y)))
	}
	return out
}

func testHouse() map[string][]int {
	return map[string][]int{
		"Kitchen":     square(0, 0, 400, 300),
		"Living Room": square(400, 0, 500, 400),
		"diningRoom":  square(0, 300, 300, 300),
		"EntryWay":    square(300, 400, 200, 200),
	}
}

func TestAlignByRoomsRecoversRotations(t *testing.T) {
	target := roomMap(testHouse())
	for _, deg := range []float64{0, 90, 180, 270, 37} {
		truth := CreateRotationTranslation(deg, 1234, -567)
		inv := InvertMatrix(truth)

		// Source is the house as seen by a robot whose frame is rotated;
		// room names differ only in case/spacing.
		srcRooms := map[string][]int{}
		for name, px := range testHouse() {
			srcRooms[NormalizeRoomName(name)+""] = transformPixels(px, inv)
		}
		source := roomMap(srcRooms)

		ra, ok := AlignByRooms(source, target)
		if !ok {
			t.Fatalf("%v°: expected alignment, got rejected (%+v)", deg, ra)
		}
		if len(ra.SharedRooms) != 4 {
			t.Errorf("%v°: shared rooms = %v, want 4", deg, ra.SharedRooms)
		}
		got := ra.RotationDeg
		diff := math.Abs(NormalizeAngle(got - deg))
		if diff > 180 {
			diff = 360 - diff
		}
		if diff > 0.5 {
			t.Errorf("%v°: rotation = %.2f", deg, got)
		}
		p := TransformPoint(Point{X: 100, Y: 100}, inv)
		back := TransformPoint(p, ra.Transform)
		if Distance(back, Point{X: 100, Y: 100}) > 5 {
			t.Errorf("%v°: round trip landed at %+v", deg, back)
		}
	}
}

func TestAlignByRoomsNeedsEnoughRooms(t *testing.T) {
	house := testHouse()
	target := roomMap(house)
	source := roomMap(map[string][]int{"kitchen": house["Kitchen"], "livingroom": house["Living Room"]})
	if _, ok := AlignByRooms(source, target); ok {
		t.Fatal("expected rejection with only 2 shared rooms")
	}
}

func TestAlignByRoomsRejectsMislabelledRooms(t *testing.T) {
	house := testHouse()
	target := roomMap(house)
	// Same shapes, names shuffled so centers don't correspond.
	source := roomMap(map[string][]int{
		"kitchen":    house["EntryWay"],
		"livingroom": house["diningRoom"],
		"diningroom": house["Living Room"],
		"entryway":   house["Kitchen"],
	})
	if ra, ok := AlignByRooms(source, target); ok {
		t.Fatalf("expected rejection for shuffled names, got %+v", ra)
	}
}

func TestRoomCentroidsSkipsUnnamedAndDuplicates(t *testing.T) {
	m := roomMap(nil)
	m.Layers = []MapLayer{
		{Type: "segment", MetaData: LayerMetaData{Name: ""}, Pixels: square(0, 0, 50, 50)},
		{Type: "segment", MetaData: LayerMetaData{Name: "Hall"}, Pixels: square(0, 0, 50, 50)},
		{Type: "segment", MetaData: LayerMetaData{Name: "hall"}, Pixels: square(100, 0, 50, 50)},
		{Type: "segment", MetaData: LayerMetaData{Name: "Bath"}, Pixels: square(0, 0, 10, 10)},
		{Type: "floor", MetaData: LayerMetaData{Name: "Floor"}, Pixels: square(0, 0, 10, 10)},
	}
	got := RoomCentroids(m)
	if len(got) != 1 {
		t.Fatalf("got %v, want only bath", got)
	}
	if _, ok := got["bath"]; !ok {
		t.Fatalf("missing bath: %v", got)
	}
}

func TestDecodeCompressedPixels(t *testing.T) {
	got := DecodeCompressedPixels([]int{10, 20, 3, 5, 6, 1, 99})
	want := []int{10, 20, 11, 20, 12, 20, 5, 6}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestParseMapJSONExpandsCompressedPixels(t *testing.T) {
	data := []byte(`{"pixelSize":5,"layers":[
		{"type":"segment","metaData":{"name":"a"},"compressedPixels":[2,4,2]},
		{"type":"wall","pixels":[1,2]},
		{"type":"floor","pixels":[10,20,15,20],"compressedPixels":[2,4,2]}]}`)
	m, err := ParseMapJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Layers[0].Pixels; len(got) != 4 || got[0] != 2 || got[1] != 4 || got[2] != 3 {
		t.Errorf("segment pixels = %v, want [2 4 3 4]", got)
	}
	if got := m.Layers[1].Pixels; len(got) != 2 || got[0] != 1 {
		t.Errorf("pixels without compressed form should be kept, got %v", got)
	}
	if got := m.Layers[2].Pixels; len(got) != 4 || got[0] != 2 {
		t.Errorf("compressed form should win over pre-scaled pixels, got %v", got)
	}
}

// TestAlignByRoomsRealMaps runs against local exports in ../tudomesh-data when present.
func TestAlignByRoomsRealMaps(t *testing.T) {
	dir := filepath.Join("..", "tudomesh-data")
	load := func(id string) *ValetudoMap {
		path := filepath.Join(dir, "ValetudoMapExport-"+id+".json")
		if _, err := os.Stat(path); err != nil {
			t.Skip("local map exports not present")
		}
		m, err := ParseMapFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	ref := load("FrugalLameLion")
	for id, want := range map[string]float64{"TrickyZanyPartridge": 180, "AppropriateMinorAntelope": 270} {
		ra, ok := AlignByRooms(load(id), ref)
		if !ok {
			t.Fatalf("%s: rejected %+v", id, ra)
		}
		diff := math.Abs(NormalizeAngle(ra.RotationDeg - want))
		if diff > 180 {
			diff = 360 - diff
		}
		if diff > 5 {
			t.Errorf("%s: rotation %.1f, want ~%.0f", id, ra.RotationDeg, want)
		}
		t.Logf("%s: rotation %.1f°, mean error %.0f, spread %.0f", id, ra.RotationDeg, ra.MeanResidual, ra.Spread)
	}
}
