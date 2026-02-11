package mesh

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NewAutoCalibrator
// ---------------------------------------------------------------------------

func TestNewAutoCalibrator_NilCache(t *testing.T) {
	cfg := &Config{Vacuums: []VacuumConfig{{ID: "vac-a"}}}
	st := NewStateTracker()

	ac := NewAutoCalibrator(cfg, nil, "/tmp/test-cal.json", "", st)
	if ac == nil {
		t.Fatal("expected non-nil AutoCalibrator")
		return
	}
	if ac.cache == nil {
		t.Fatal("expected cache to be initialized when nil is passed")
	}
	if ac.cache.Vacuums == nil {
		t.Fatal("expected cache.Vacuums to be initialized")
	}
}

func TestNewAutoCalibrator_WithCache(t *testing.T) {
	cfg := &Config{Vacuums: []VacuumConfig{{ID: "vac-a"}}}
	st := NewStateTracker()
	cache := &CalibrationData{
		ReferenceVacuum: "vac-a",
		Vacuums: map[string]VacuumCalibration{
			"vac-a": {Transform: Identity(), LastUpdated: time.Now().Unix()},
		},
	}

	ac := NewAutoCalibrator(cfg, cache, "/tmp/test-cal.json", "", st)
	if ac.cache != cache {
		t.Fatal("expected cache to be the same pointer passed in")
	}
}

// ---------------------------------------------------------------------------
// OnDockingEvent – debounce
// ---------------------------------------------------------------------------

func TestOnDockingEvent_DebounceSkips(t *testing.T) {
	apiURL := "http://fake:1234/api/v2/robot/state/map"
	cfg := &Config{
		Vacuums: []VacuumConfig{{ID: "vac-a", ApiURL: &apiURL}},
	}
	st := NewStateTracker()
	cache := &CalibrationData{
		ReferenceVacuum: "vac-a",
		Vacuums: map[string]VacuumCalibration{
			"vac-a": {
				Transform:            Identity(),
				LastUpdated:          time.Now().Unix(),
				MapAreaAtCalibration: 5000,
			},
		},
	}

	ac := NewAutoCalibrator(cfg, cache, filepath.Join(t.TempDir(), "cal.json"), "", st)
	// Simulate a recent calibration
	ac.lastCalibrated["vac-a"] = time.Now()

	// This should be debounced (no panic, no HTTP call)
	ac.OnDockingEvent("vac-a")

	// The lastCalibrated time should remain the same (was not updated by this call)
	// since we skipped. We can't easily assert the skip without a counter,
	// but at minimum we verify no crash.
}

// ---------------------------------------------------------------------------
// OnDockingEvent – missing vacuum config
// ---------------------------------------------------------------------------

func TestOnDockingEvent_UnknownVacuum(t *testing.T) {
	cfg := &Config{Vacuums: []VacuumConfig{{ID: "vac-a"}}}
	st := NewStateTracker()

	ac := NewAutoCalibrator(cfg, nil, filepath.Join(t.TempDir(), "cal.json"), "", st)

	// Should log warning and return without panic
	ac.OnDockingEvent("unknown-vacuum")
}

// ---------------------------------------------------------------------------
// OnDockingEvent – no API URL configured
// ---------------------------------------------------------------------------

func TestOnDockingEvent_NoApiURL(t *testing.T) {
	cfg := &Config{Vacuums: []VacuumConfig{{ID: "vac-a"}}}
	st := NewStateTracker()

	ac := NewAutoCalibrator(cfg, nil, filepath.Join(t.TempDir(), "cal.json"), "", st)

	// Should log warning about missing apiUrl and return
	ac.OnDockingEvent("vac-a")
}

// ---------------------------------------------------------------------------
// resolveReference
// ---------------------------------------------------------------------------

func TestResolveReference_FromConfig(t *testing.T) {
	cfg := &Config{
		Reference: "vac-a",
		Vacuums:   []VacuumConfig{{ID: "vac-a"}, {ID: "vac-b"}},
	}
	st := NewStateTracker()

	ac := NewAutoCalibrator(cfg, nil, "/tmp/test.json", "", st)
	ref := ac.resolveReference()
	if ref != "vac-a" {
		t.Fatalf("expected vac-a, got %s", ref)
	}
}

func TestResolveReference_FromCache(t *testing.T) {
	cfg := &Config{Vacuums: []VacuumConfig{{ID: "vac-a"}, {ID: "vac-b"}}}
	cache := &CalibrationData{ReferenceVacuum: "vac-b", Vacuums: make(map[string]VacuumCalibration)}
	st := NewStateTracker()

	ac := NewAutoCalibrator(cfg, cache, "/tmp/test.json", "", st)
	ref := ac.resolveReference()
	if ref != "vac-b" {
		t.Fatalf("expected vac-b, got %s", ref)
	}
}

func TestResolveReference_AutoSelect(t *testing.T) {
	cfg := &Config{Vacuums: []VacuumConfig{{ID: "vac-a"}, {ID: "vac-b"}}}
	st := NewStateTracker()

	// Add maps with different areas so auto-select picks the larger one
	st.UpdateMap("vac-a", &ValetudoMap{MetaData: MapMetaData{TotalLayerArea: 1000}})
	st.UpdateMap("vac-b", &ValetudoMap{MetaData: MapMetaData{TotalLayerArea: 5000}})

	ac := NewAutoCalibrator(cfg, nil, "/tmp/test.json", "", st)
	ref := ac.resolveReference()
	if ref != "vac-b" {
		t.Fatalf("expected vac-b (larger area), got %s", ref)
	}
}

func TestResolveReference_NoMaps(t *testing.T) {
	cfg := &Config{Vacuums: []VacuumConfig{{ID: "vac-a"}}}
	st := NewStateTracker()

	ac := NewAutoCalibrator(cfg, nil, "/tmp/test.json", "", st)
	ref := ac.resolveReference()
	if ref != "" {
		t.Fatalf("expected empty string, got %s", ref)
	}
}

// ---------------------------------------------------------------------------
// GetCache
// ---------------------------------------------------------------------------

func TestGetCache(t *testing.T) {
	cache := &CalibrationData{
		ReferenceVacuum: "vac-a",
		Vacuums:         map[string]VacuumCalibration{"vac-a": {Transform: Identity()}},
	}
	ac := NewAutoCalibrator(&Config{}, cache, "/tmp/test.json", "", NewStateTracker())

	got := ac.GetCache()
	if got != cache {
		t.Fatal("GetCache should return the same cache pointer")
	}
}

// ---------------------------------------------------------------------------
// persistAndRecord – saves to disk
// ---------------------------------------------------------------------------

func TestPersistAndRecord_SavesFile(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "calibration.json")
	cache := &CalibrationData{
		ReferenceVacuum: "vac-a",
		Vacuums: map[string]VacuumCalibration{
			"vac-a": {Transform: Identity(), LastUpdated: time.Now().Unix()},
		},
	}

	ac := NewAutoCalibrator(&Config{}, cache, cachePath, "", NewStateTracker())
	ac.persistAndRecord("vac-a")

	// Verify file was written
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatal("expected calibration file to be created")
	}

	// Verify lastCalibrated was updated
	if _, ok := ac.lastCalibrated["vac-a"]; !ok {
		t.Fatal("expected lastCalibrated to be set for vac-a")
	}
}

// ---------------------------------------------------------------------------
// String
// ---------------------------------------------------------------------------

func TestAutoCalibrator_String(t *testing.T) {
	cache := &CalibrationData{
		ReferenceVacuum: "vac-a",
		Vacuums: map[string]VacuumCalibration{
			"vac-a": {Transform: Identity()},
		},
	}
	ac := NewAutoCalibrator(&Config{}, cache, "/tmp/test.json", "", NewStateTracker())

	s := ac.String()
	if s == "" {
		t.Fatal("expected non-empty string")
	}
}
