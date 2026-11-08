package mesh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestStateTrackerFloorplanCachesUntilCalibrationChanges(t *testing.T) {
	st := NewStateTracker()
	st.UpdateMap("a", boxRoom("1", "Room", 10, 6))
	cal := &CalibrationData{ReferenceVacuum: "a", Vacuums: map[string]VacuumCalibration{}}
	cal.UpdateVacuumCalibration("a", VacuumCalibration{Transform: Identity()})

	first := st.Floorplan(cal, "")
	if first == nil {
		t.Fatal("no floorplan")
	}

	// A map update alone keeps the cached floorplan.
	st.UpdateMap("a", boxRoom("1", "Room", 12, 6))
	if st.Floorplan(cal, "") != first {
		t.Error("rebuilt on a map update")
	}

	// An uncalibrated vacuum is left out and does not trigger a rebuild.
	st.UpdateMap("b", boxRoom("1", "Room", 10, 6))
	if st.Floorplan(cal, "") != first {
		t.Error("rebuilt for an uncalibrated vacuum")
	}

	// Calibrating it does.
	cal.UpdateVacuumCalibration("b", VacuumCalibration{Transform: Identity()})
	second := st.Floorplan(cal, "")
	if second == first {
		t.Fatal("not rebuilt after calibration changed")
	}
	if got := cellKinds(second)[[2]int{11, 2}]; got != "Room" {
		t.Errorf("rebuild did not use the current map of a: cell (11,2) = %q", got)
	}
}

func TestStateTrackerFloorplanWithoutCalibration(t *testing.T) {
	st := NewStateTracker()
	if st.Floorplan(nil, "") != nil {
		t.Error("floorplan with no maps")
	}
	st.UpdateMap("small", boxRoom("1", "Room", 4, 4))
	st.UpdateMap("big", boxRoom("1", "Room", 10, 6))
	// Without calibration only the reference is drawn: refID when given.
	fp := st.Floorplan(nil, "small")
	if _, ok := cellKinds(fp)[[2]int{8, 2}]; ok {
		t.Error("uncalibrated map was merged in")
	}
}

func TestAutoCalibratorCallsOnCalibrated(t *testing.T) {
	m := boxRoom("1", "Room", 10, 6)
	m.MetaData.TotalLayerArea = 1000
	m.Entities = []MapEntity{
		{Type: "robot_position", Points: []int{10, 10}, MetaData: map[string]interface{}{"angle": 0.0}},
		{Type: "charger_location", Points: []int{5, 5}},
	}
	body, _ := json.Marshal(m)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()

	url := srv.URL
	cfg := &Config{Reference: "vac-a", Vacuums: []VacuumConfig{{ID: "vac-a", ApiURL: &url}}}
	st := NewStateTracker()
	ac := NewAutoCalibrator(cfg, nil, filepath.Join(t.TempDir(), "cal.json"), "", st)
	var got string
	ac.OnCalibrated = func(ref string) { got = ref }

	ac.OnDockingEvent("vac-a")
	if got != "vac-a" {
		t.Errorf("OnCalibrated got %q, want vac-a", got)
	}
	if st.Floorplan(ac.GetCache(), "") == nil {
		t.Error("no floorplan after calibration")
	}
}
