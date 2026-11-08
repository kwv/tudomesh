package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kwv/tudomesh/mesh"
)

// One vacuum per instance (#112): docking calibrates it as its own
// reference and every endpoint serves its map.
func TestSingleVacuumInstance(t *testing.T) {
	m := roomMapWithRobot()
	m.Entities = append(m.Entities, mesh.MapEntity{Type: "charger_location", Points: []int{25, 25}})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(m)
	}))
	t.Cleanup(api.Close)

	u := api.URL
	cfg := &mesh.Config{Vacuums: []mesh.VacuumConfig{{ID: "NormalLostHamster", Topic: "valetudo/rocky/MapData/map-data", ApiURL: &u}}}
	dir := t.TempDir()
	cachePath := filepath.Join(dir, ".calibration-cache.json")
	cache := loadServiceCalibration(cachePath)
	st := mesh.NewStateTracker()
	st.UpdateMap("NormalLostHamster", m)
	st.UpdatePosition("NormalLostHamster", 10, 10, 0)
	st.UpdateStatus("NormalLostHamster", "error")

	mesh.NewAutoCalibrator(cfg, cache, cachePath, dir, st).OnDockingEvent("NormalLostHamster")
	if cache.Reference() != "NormalLostHamster" || cache.GetVacuumCalibration("NormalLostHamster") == nil {
		t.Errorf("reference=%q, calibration=%v", cache.Reference(), cache.GetVacuumCalibration("NormalLostHamster"))
	}

	srv := newHTTPServer(st, cache, cfg, "", 0)
	for _, path := range []string{"/vacuums", "/vacuum/rocky", "/vacuum/rocky.png", "/vacuum/rocky.svg",
		"/live.svg", "/live.png", "/composite-map.svg", "/composite-map.png", "/floorplan.svg"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("%s: status %d, %d bytes", path, rec.Code, rec.Body.Len())
		}
	}
}
