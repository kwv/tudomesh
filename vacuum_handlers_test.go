package main

import (
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kwv/tudomesh/mesh"
)

// roomMapWithRobot returns a map with a named segment and a robot inside it.
// Layer pixels are grid indices; entity points are grid * pixelSize.
func roomMapWithRobot() *mesh.ValetudoMap {
	var seg, wall []int
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			seg = append(seg, x, y)
		}
		wall = append(wall, 20, y)
	}
	return &mesh.ValetudoMap{
		PixelSize: 5,
		Size:      mesh.Size{X: 200, Y: 200},
		Layers: []mesh.MapLayer{
			{Type: "segment", MetaData: mesh.LayerMetaData{SegmentID: "7", Name: "Kitchen"}, Pixels: seg},
			{Type: "wall", Pixels: wall},
		},
		Entities: []mesh.MapEntity{
			{Type: "robot_position", Points: []int{50, 50}, MetaData: map[string]interface{}{"angle": 90.0}},
		},
	}
}

func vacuumTestServer() (http.Handler, *mesh.StateTracker) {
	st := mesh.NewStateTracker()
	st.UpdateMap("TidyCalmPanda", roomMapWithRobot())
	st.UpdatePosition("TidyCalmPanda", 10, 10, 90)
	st.UpdateStatus("TidyCalmPanda", "error")
	cfg := &mesh.Config{Vacuums: []mesh.VacuumConfig{
		{ID: "TidyCalmPanda", Topic: "valetudo/robo7/MapData/map-data", Color: "#057dcd"},
		{ID: "FairBoldFalcon", Topic: "valetudo/robo1/MapData/map-data"},
	}}
	return newHTTPServer(st, nil, cfg, "TidyCalmPanda", 0), st
}

func TestTopicIdentifier(t *testing.T) {
	cases := map[string]string{
		"valetudo/robo7/MapData/map-data":      "robo7",
		"home/valetudo/robo7/MapData/map-data": "robo7",
		"map-data":                             "",
	}
	for topic, want := range cases {
		if got := topicIdentifier(topic); got != want {
			t.Errorf("topicIdentifier(%q) = %q, want %q", topic, got, want)
		}
	}
}

func TestVacuumEndpoint_JSON(t *testing.T) {
	srv, _ := vacuumTestServer()
	for _, key := range []string{"robo7", "ROBO7", "TidyCalmPanda", "tidycalmpanda"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", "/vacuum/"+key, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", key, rec.Code, rec.Body.String())
		}
		var info vacuumInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
		if info.ID != "TidyCalmPanda" || info.Name != "robo7" {
			t.Errorf("%s: id/name = %q/%q", key, info.ID, info.Name)
		}
		if info.State != "error" || info.StateSince == nil {
			t.Errorf("%s: state = %q since=%v", key, info.State, info.StateSince)
		}
		if info.Room == nil || info.Room.Name != "Kitchen" || info.Room.SegmentID != "7" {
			t.Errorf("%s: room = %+v", key, info.Room)
		}
		if info.Position == nil || info.Position.X != 10 {
			t.Errorf("%s: position = %+v", key, info.Position)
		}
		if info.MapURL != "/vacuum/robo7.png" {
			t.Errorf("%s: mapUrl = %q", key, info.MapURL)
		}
	}
}

func TestVacuumEndpoint_PNG(t *testing.T) {
	srv, _ := vacuumTestServer()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/vacuum/robo7.png", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type %q", ct)
	}
	if _, err := png.Decode(rec.Body); err != nil {
		t.Errorf("invalid PNG: %v", err)
	}
}

func TestVacuumNames(t *testing.T) {
	cfg := &mesh.Config{Vacuums: []mesh.VacuumConfig{
		{ID: "TidyCalmPanda", Topic: "valetudo/robo7/MapData/map-data"},
	}}
	maps := map[string]*mesh.ValetudoMap{"TidyCalmPanda.json": {}, "Other.json": {}}
	positions := map[string]*mesh.LivePosition{"TidyCalmPanda.json": {}, "Other.json": {}}
	got := vacuumNames(cfg, maps, positions)
	if len(got) != 1 || got["TidyCalmPanda.json"] != "robo7" {
		t.Errorf("vacuumNames = %v, want only TidyCalmPanda.json: robo7", got)
	}
}

// A robot in an ignored room still reports that room in JSON.
func TestVacuumEndpoint_IgnoredRoomStillReported(t *testing.T) {
	st := mesh.NewStateTracker()
	st.UpdateMap("TidyCalmPanda", roomMapWithRobot())
	cfg := &mesh.Config{
		IgnoreRooms: []string{"kitchen"},
		Vacuums:     []mesh.VacuumConfig{{ID: "TidyCalmPanda", Topic: "valetudo/robo7/MapData/map-data"}},
	}
	srv := newHTTPServer(st, nil, cfg, "TidyCalmPanda", 0)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/vacuum/robo7", nil))
	var info vacuumInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Room == nil || info.Room.Name != "Kitchen" {
		t.Errorf("room = %+v, want Kitchen", info.Room)
	}
}

func TestVacuumEndpoint_Unknown(t *testing.T) {
	srv, _ := vacuumTestServer()
	for _, path := range []string{"/vacuum/nope", "/vacuum/nope.png"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, rec.Code)
		}
	}
}

func TestVacuumsEndpoint_ListsConfiguredAndMapped(t *testing.T) {
	srv, _ := vacuumTestServer()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/vacuums", nil))
	var list []vacuumInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d vacuums, want 2: %s", len(list), rec.Body.String())
	}
	// Sorted by id; the one without data still appears with its name and map URL.
	if list[0].ID != "FairBoldFalcon" || list[0].Name != "robo1" || list[0].State != "" {
		t.Errorf("first entry %+v", list[0])
	}
	if list[1].Room == nil || list[1].Room.Name != "Kitchen" {
		t.Errorf("second entry %+v", list[1])
	}
}

func TestVacuumEndpoint_SVG(t *testing.T) {
	srv, _ := vacuumTestServer()
	for path, wantHighlight := range map[string]bool{"/vacuum/robo7.svg": true, "/live.svg": false} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", path, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
			t.Errorf("%s: content-type %q", path, ct)
		}
		body := rec.Body.String()
		for _, want := range []string{`id="vacuum-tidycalmpanda"`, `data-name="robo7"`, `data-room="Kitchen"`, `data-state="error"`, `class="room`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %s", path, want)
			}
		}
		if got := strings.Contains(body, "vacuum state-error highlight"); got != wantHighlight {
			t.Errorf("%s: highlight=%v, want %v", path, got, wantHighlight)
		}
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/vacuum/nope.svg", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown svg: status %d, want 404", rec.Code)
	}
}

func TestVacuumEndpoint_SVGAlertRoom(t *testing.T) {
	srv, _ := vacuumTestServer()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/vacuum/robo7.svg", nil))
	body := rec.Body.String()
	want := `<path id="alert-room" class="alert-room" data-room="Kitchen" data-segment-id="7" data-vacuum="TidyCalmPanda"`
	if !strings.Contains(body, want) {
		t.Errorf("missing %s", want)
	}
	rooms, alert, walls := strings.Index(body, `id="rooms"`), strings.Index(body, `id="alert-room"`), strings.Index(body, `id="walls"`)
	if rooms >= alert || alert >= walls {
		t.Errorf("order rooms=%d alert-room=%d walls=%d; want rooms < alert-room < walls", rooms, alert, walls)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/live.svg", nil))
	if strings.Contains(rec.Body.String(), `id="alert-room"`) {
		t.Error("/live.svg should have no alert room")
	}
}

func TestCalibratedVacuums(t *testing.T) {
	maps := map[string]*mesh.ValetudoMap{"a": {}, "b": {}}
	cache := &mesh.CalibrationData{}
	cache.UpdateVacuumCalibration("a", mesh.VacuumCalibration{Transform: mesh.Translation(1, 2)})

	got := calibratedVacuums(maps, cache)
	if !got["a"] || got["b"] || len(got) != 2 {
		t.Errorf("calibratedVacuums = %v, want a only", got)
	}
	if got := calibratedVacuums(maps, nil); got == nil || got["a"] || got["b"] {
		t.Errorf("nil cache = %v, want non-nil and nothing calibrated", got)
	}
}

// Crop geometry is tested in mesh; here only the query handling.
func TestVacuumEndpoint_Crop(t *testing.T) {
	srv, _ := vacuumTestServer()
	for _, path := range []string{"/vacuum/robo7.png?crop=room", "/vacuum/robo7.svg?crop=room"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/vacuum/robo7.png?crop=bogus", "/vacuum/robo7.svg?crop=0"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", path, rec.Code)
		}
	}
}
