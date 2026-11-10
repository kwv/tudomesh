package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kwv/tudomesh/mesh"
)

// cellsRect returns grid cells [x,y,...] of a w x h rectangle at (x0,y0).
func cellsRect(x0, y0, w, h int) []int {
	var px []int
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			px = append(px, x, y)
		}
	}
	return px
}

func segLayer(id, name string, px []int) mesh.MapLayer {
	return mesh.MapLayer{Type: "segment", MetaData: mesh.LayerMetaData{SegmentID: id, Name: name}, Pixels: px}
}

// planHouse is an L of three rooms, so alignment has one answer.
func planHouse(rooms ...mesh.MapLayer) *mesh.ValetudoMap {
	return &mesh.ValetudoMap{PixelSize: 5, Layers: rooms}
}

func planTestApp(t *testing.T) *App {
	t.Helper()
	return &App{DataDir: t.TempDir()}
}

func planTestConfig() *mesh.Config {
	zero := 0.0
	return &mesh.Config{Vacuums: []mesh.VacuumConfig{
		{ID: "ref", URL: "http://ref.local"},
		{ID: "robot", URL: "http://robot.local", Rotation: &zero},
	}}
}

// The robot has one room where the reference has two, and generic names in
// another language: plan a split and renames, with names kept as written.
func TestPlanRooms_SplitAndRenameAnyNames(t *testing.T) {
	ref := planHouse(
		segLayer("1", "Küche", cellsRect(0, 0, 40, 40)),
		segLayer("2", "Wohn & Esszimmer", cellsRect(40, 0, 40, 40)),
		segLayer("3", "厨房", cellsRect(0, 40, 40, 30)),
	)
	robot := planHouse(
		segLayer("10", "Raum 1", cellsRect(0, 0, 80, 40)),
		segLayer("11", "", cellsRect(0, 40, 40, 30)),
	)
	a := planTestApp(t)
	var out bytes.Buffer
	if err := a.planRooms(&out, planTestConfig(), map[string]*mesh.ValetudoMap{"ref": ref, "robot": robot}, "ref"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"== robot",
		"1. Split segment 10 along the line",
		"PUT http://robot.local/api/v2/robot/capabilities/MapSegmentEditCapability",
		`Rename segment 11 to "厨房"`,
		"PUT http://robot.local/api/v2/robot/capabilities/MapSegmentRenameCapability",
		`"name":"Wohn & Esszimmer"`,
		"To apply a plan",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	data, err := os.ReadFile(filepath.Join(a.DataDir, "rooms-robot-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"Wohn & Esszimmer"`) || !strings.Contains(string(data), `"Küche"`) {
		t.Errorf("plan file escapes names:\n%s", data)
	}
	for _, f := range []string{"rooms-ref.svg", "rooms-robot-before.svg", "rooms-robot-after.svg"} {
		if _, err := os.Stat(filepath.Join(a.DataDir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

func TestPlanRooms_SingleRobot(t *testing.T) {
	ref := planHouse(segLayer("1", "", cellsRect(0, 0, 40, 40)), segLayer("2", "", cellsRect(0, 40, 60, 30)))
	a := planTestApp(t)
	var out bytes.Buffer
	if err := a.planRooms(&out, planTestConfig(), map[string]*mesh.ValetudoMap{"ref": ref}, "ref"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Only ref has a map, so there is nothing to plan", "have no names, so no renames"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(a.DataDir, "rooms-ref.svg")); err != nil {
		t.Error("no reference preview")
	}
}

func TestPlanRooms_Errors(t *testing.T) {
	a := planTestApp(t)
	if err := a.planRooms(&bytes.Buffer{}, planTestConfig(), nil, "ref"); err == nil || !strings.Contains(err.Error(), "no robot maps") {
		t.Errorf("no maps: err = %v", err)
	}
	maps := map[string]*mesh.ValetudoMap{"robot": planHouse(segLayer("1", "A", cellsRect(0, 0, 10, 10)))}
	if err := a.planRooms(&bytes.Buffer{}, planTestConfig(), maps, "ref"); err == nil || !strings.Contains(err.Error(), "[robot]") {
		t.Errorf("missing reference: err = %v", err)
	}
}

func TestRoomOpDescribe(t *testing.T) {
	cases := map[string]mesh.RoomOp{
		"Split segment 4 along the line (10,20)-(30,40)": {Action: "split_segment", SegmentID: "4", PA: &mesh.Point{X: 10, Y: 20}, PB: &mesh.Point{X: 30, Y: 40}},
		"Join segments 1 and 2":                          {Action: "join_segments", SegmentAID: "1", SegmentBID: "2"},
		`Rename segment 3 to "Salle à manger"`:           {Action: "rename_segment", SegmentID: "3", Name: "Salle à manger"},
	}
	for want, op := range cases {
		if got := op.Describe(); got != want {
			t.Errorf("Describe = %q, want %q", got, want)
		}
	}
}

func TestDataDirPaths(t *testing.T) {
	for _, c := range []struct {
		app           App
		config, cache string
	}{
		{App{DataDir: "/data", ConfigFile: "config.yaml", CalibrationCache: ".calibration-cache.json"}, "/data/config.yaml", "/data/.calibration-cache.json"},
		{App{DataDir: ".", ConfigFile: "config.yaml", CalibrationCache: ".calibration-cache.json"}, "config.yaml", ".calibration-cache.json"},
		{App{DataDir: "/data", ConfigFile: "/etc/tm.yaml", CalibrationCache: "/tmp/c.json"}, "/etc/tm.yaml", "/tmp/c.json"},
	} {
		if cfg, cache := c.app.dataDirPaths(); cfg != c.config || cache != c.cache {
			t.Errorf("%+v: got %s, %s; want %s, %s", c.app, cfg, cache, c.config, c.cache)
		}
	}
}
