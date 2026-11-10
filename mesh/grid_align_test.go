package mesh

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// lHouse builds an unnamed L-shaped floor plan (grid units, pixelSize 5)
// with outer walls and one interior wall.
func lHouse() *ValetudoMap {
	inL := func(x, y int) bool {
		return (x >= 0 && x < 160 && y >= 0 && y < 80) || (x >= 0 && x < 60 && y >= 80 && y < 180)
	}
	var floor, wall []int
	for y := -1; y <= 181; y++ {
		for x := -1; x <= 161; x++ {
			if inL(x, y) {
				if x == 100 && y < 60 { // interior wall with a doorway
					wall = append(wall, x, y)
				} else {
					floor = append(floor, x, y)
				}
				continue
			}
			// Wall cell if a 4-neighbour is inside.
			if inL(x+1, y) || inL(x-1, y) || inL(x, y+1) || inL(x, y-1) {
				wall = append(wall, x, y)
			}
		}
	}
	return &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "floor", Pixels: floor},
		{Type: "wall", Pixels: wall},
	}}
}

func transformMap(m *ValetudoMap, t AffineMatrix) *ValetudoMap {
	out := &ValetudoMap{PixelSize: m.PixelSize}
	for _, l := range m.Layers {
		nl := l
		nl.Pixels = transformPixels(l.Pixels, t)
		out.Layers = append(out.Layers, nl)
	}
	return out
}

func TestAlignByGridRecoversTransform(t *testing.T) {
	target := lHouse()
	for _, deg := range []float64{0, 90, 180, 270, 37} {
		truth := CreateRotationTranslation(deg, 600, -240)
		source := transformMap(target, InvertMatrix(truth))

		ga, ok := AlignByGrid(source, target, GridAlignOptions{})
		if !ok {
			t.Fatalf("%v°: no result", deg)
		}
		if d := angleDiff(ga.RotationDeg, deg); d > 1.5 {
			t.Errorf("%v°: rotation %.1f", deg, ga.RotationDeg)
		}
		if ga.Confidence < MinGridConfidence {
			t.Errorf("%v°: confidence %.2f, want >= %.2f", deg, ga.Confidence, MinGridConfidence)
		}
		// Map corners of the source back and check they land within 3 cells.
		for _, p := range []Point{{0, 0}, {160, 0}, {0, 180}} {
			src := TransformPoint(p, InvertMatrix(truth))
			if d := Distance(TransformPoint(src, ga.Transform), p); d > 3 {
				t.Errorf("%v°: corner %v off by %.0f", deg, p, d)
			}
		}
	}
}

func TestAlignByGridRotationHintLimitsSearch(t *testing.T) {
	target := lHouse()
	truth := CreateRotationTranslation(90, 100, 100)
	source := transformMap(target, InvertMatrix(truth))

	hint := 85.0
	ga, ok := AlignByGrid(source, target, GridAlignOptions{RotationHint: &hint})
	if !ok || angleDiff(ga.RotationDeg, 90) > 1.5 {
		t.Fatalf("with hint: rotation %.1f ok=%v", ga.RotationDeg, ok)
	}
}

func TestAlignByGridSymmetricRoomIsAmbiguous(t *testing.T) {
	var floor, wall []int
	for y := -1; y <= 80; y++ {
		for x := -1; x <= 80; x++ {
			if x < 0 || y < 0 || x >= 80 || y >= 80 {
				wall = append(wall, x, y)
			} else {
				floor = append(floor, x, y)
			}
		}
	}
	square := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		{Type: "floor", Pixels: floor}, {Type: "wall", Pixels: wall},
	}}
	ga, ok := AlignByGrid(transformMap(square, RotationDeg(90)), square, GridAlignOptions{})
	if !ok {
		t.Fatal("no result")
	}
	if ga.Confidence >= MinGridConfidence {
		t.Errorf("square room: confidence %.2f, want < %.2f", ga.Confidence, MinGridConfidence)
	}
}

func TestAlignByGridEmptyMap(t *testing.T) {
	if _, ok := AlignByGrid(&ValetudoMap{}, lHouse(), GridAlignOptions{}); ok {
		t.Error("expected ok=false for empty source")
	}
}

// TestAlignByGridRealMaps runs on local exports in ../tudomesh-data, with room
// names removed, and checks against the orientations in its calibration cache.
func TestAlignByGridRealMaps(t *testing.T) {
	ref, want := localRealMaps(t)
	for id, want := range want {
		named := loadLocalMap(t, id)
		unnamed := loadLocalMap(t, id)
		for i := range unnamed.Layers {
			unnamed.Layers[i].MetaData.Name = ""
		}
		ga, ok := AlignByGrid(unnamed, ref, GridAlignOptions{})
		if !ok {
			t.Fatalf("%s: no result", id)
		}
		if d := angleDiff(ga.RotationDeg, want); d > 5 {
			t.Errorf("%s: rotation %.1f, want ~%.0f", id, ga.RotationDeg, want)
		}
		if ga.Confidence < MinGridConfidence {
			t.Errorf("%s: confidence %.2f", id, ga.Confidence)
		}
		// Cross-check placement against the room-name fit.
		if ra, ok := AlignByRooms(named, ref); ok {
			var worst float64
			for _, c := range RoomCentroids(named) {
				worst = math.Max(worst, Distance(TransformPoint(c, ga.Transform), TransformPoint(c, ra.Transform)))
			}
			if worst > 12 { // grid cells (60cm at pixelSize 5)
				t.Errorf("%s: grid and room fits disagree by %.0f", id, worst)
			}
			t.Logf("%s: rotation %.1f° confidence %.2f, max disagreement with room fit %.0f", id, ga.RotationDeg, ga.Confidence, worst)
		}
	}
}

// localRealMaps reads the calibration cache in ../tudomesh-data and returns
// the reference map and, for every other vacuum with an export there, its
// cached rotation in degrees. It skips the test without local data.
func localRealMaps(t *testing.T) (*ValetudoMap, map[string]float64) {
	t.Helper()
	cache, err := LoadCalibration(filepath.Join("..", "tudomesh-data", ".calibration-cache.json"))
	if err != nil || cache == nil || cache.ReferenceVacuum == "" {
		t.Skip("local calibration cache not present")
	}
	ref := loadLocalMap(t, cache.ReferenceVacuum)
	want := map[string]float64{}
	for id, vc := range cache.Vacuums {
		if id == cache.ReferenceVacuum {
			continue
		}
		if _, err := os.Stat(localMapPath(id)); err == nil {
			want[id] = NormalizeAngle(math.Atan2(vc.Transform.C, vc.Transform.A) * 180 / math.Pi)
		}
	}
	if len(want) == 0 {
		t.Skip("no local map exports besides the reference")
	}
	return ref, want
}

func localMapPath(id string) string {
	return filepath.Join("..", "tudomesh-data", "ValetudoMapExport-"+id+".json")
}

func loadLocalMap(t *testing.T, id string) *ValetudoMap {
	t.Helper()
	m, err := ParseMapFile(localMapPath(id))
	if err != nil {
		t.Skip("local map exports not present")
	}
	return m
}
