package mesh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// TestDockingAutoCalibrateFlow
//
// Integration test that exercises the full docking -> auto-calibration chain:
//   1. Mock MQTT client receives a "docked" state message
//   2. DockingHandler fires for the correct vacuum
//   3. Handler fetches fresh map data via HTTP API (httptest server)
//   4. ShouldRecalibrate decides whether to proceed
//   5. CalibrateVacuums computes transforms
//   6. CalibrationData is updated and persisted to disk
// ---------------------------------------------------------------------------

func TestDockingAutoCalibrateFlow(t *testing.T) {
	// -- arrange: map data served by a fake Valetudo REST API --
	referenceMap := validMap(2000)
	dockedMap := validMap(1500)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("expected Accept: application/json, got %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		data, _ := json.Marshal(dockedMap)
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	// -- arrange: MQTT mock with docking detection --
	mock := NewMockClient()
	mock.SetConnected(true)

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "valetudo/vacuum1/MapData/map-data"},
			{ID: "vacuum2", Topic: "valetudo/vacuum2/MapData/map-data"},
		},
	}

	client := newMQTTClientWithMock(mock, config, func(string, []byte, *ValetudoMap, error) {})

	// -- arrange: calibration cache that allows recalibration --
	calData := &CalibrationData{
		ReferenceVacuum: "vacuum1",
		Vacuums: map[string]VacuumCalibration{
			"vacuum1": {
				Transform:            Identity(),
				LastUpdated:          time.Now().Add(-2 * time.Hour).Unix(), // stale
				MapAreaAtCalibration: 2000,
			},
		},
		LastUpdated: time.Now().Add(-2 * time.Hour).Unix(),
	}

	// Prepare a temp dir for cache persistence
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "calibration-cache.json")

	// -- arrange: docking handler simulates the auto-calibration logic --
	var mu sync.Mutex
	var calibratedVacuumID string
	var calibrationUpdated bool

	dockingHandler := func(vacuumID string) {
		mu.Lock()
		defer mu.Unlock()

		// Step 1: Check if recalibration is needed (30-min debounce)
		if !calData.ShouldRecalibrate(vacuumID, 1500, 30*time.Minute) {
			return
		}

		// Step 2: Fetch fresh map from API
		fetchedMap, err := FetchMapFromAPI(srv.URL,
			WithHTTPClient(srv.Client()),
			WithMaxRetries(1),
		)
		if err != nil {
			t.Errorf("FetchMapFromAPI failed: %v", err)
			return
		}

		// Step 3: Build maps collection and calibrate
		maps := map[string]*ValetudoMap{
			"vacuum1": referenceMap,
			vacuumID:  fetchedMap,
		}

		newCal, err := CalibrateVacuums(maps, "vacuum1")
		if err != nil {
			t.Errorf("CalibrateVacuums failed: %v", err)
			return
		}

		// Step 4: Update the existing calibration data
		if vc, ok := newCal.Vacuums[vacuumID]; ok {
			calData.UpdateVacuumCalibration(vacuumID, vc)
		}

		// Step 5: Persist to disk
		if err := SaveCalibration(cachePath, calData); err != nil {
			t.Errorf("SaveCalibration failed: %v", err)
			return
		}

		calibratedVacuumID = vacuumID
		calibrationUpdated = true
	}

	client.SetDockingHandler(dockingHandler)

	// -- act: subscribe and simulate docking event --
	client.onConnect(mock)

	// Send docked state for vacuum2
	mock.SimulateMessage(
		"valetudo/vacuum2/StatusStateAttribute/status",
		[]byte(`{"value":"docked"}`),
	)

	// -- assert: calibration was triggered for the correct vacuum --
	mu.Lock()
	gotID := calibratedVacuumID
	gotUpdated := calibrationUpdated
	mu.Unlock()

	assert.Equal(t, "vacuum2", gotID, "calibratedVacuumID should be vacuum2")
	assert.True(t, gotUpdated, "calibration should have been updated")

	// -- assert: calibration data was updated in memory --
	vc := calData.GetVacuumCalibration("vacuum2")
	if vc.LastUpdated == 0 {
		t.Error("vacuum2 LastUpdated should be non-zero")
	}
	if vc.MapAreaAtCalibration != 1500 {
		t.Errorf("vacuum2 MapAreaAtCalibration = %d, want 1500", vc.MapAreaAtCalibration)
	}

	// -- assert: calibration was persisted to disk --
	loaded, err := LoadCalibration(cachePath)
	if err != nil {
		t.Fatalf("LoadCalibration from disk: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil calibration from disk")
		return
	}
	if _, ok := loaded.Vacuums["vacuum2"]; !ok {
		t.Error("vacuum2 missing from persisted calibration")
	}
}

// ---------------------------------------------------------------------------
// TestDockingAutoCalibrateFlow_DebounceSkips
//
// Verifies that a recent calibration prevents re-calibration on docking.
// ---------------------------------------------------------------------------

func TestDockingAutoCalibrateFlow_DebounceSkips(t *testing.T) {
	calData := &CalibrationData{
		ReferenceVacuum: "vacuum1",
		Vacuums: map[string]VacuumCalibration{
			"vacuum1": {
				Transform:            Identity(),
				LastUpdated:          time.Now().Unix(), // very recent
				MapAreaAtCalibration: 2000,
			},
			"vacuum2": {
				Transform:            Identity(),
				LastUpdated:          time.Now().Unix(), // very recent
				MapAreaAtCalibration: 1500,
			},
		},
		LastUpdated: time.Now().Unix(),
	}

	mock := NewMockClient()
	mock.SetConnected(true)

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "valetudo/vacuum1/MapData/map-data"},
			{ID: "vacuum2", Topic: "valetudo/vacuum2/MapData/map-data"},
		},
	}

	client := newMQTTClientWithMock(mock, config, func(string, []byte, *ValetudoMap, error) {})

	calibrationTriggered := false

	client.SetDockingHandler(func(vacuumID string) {
		// Same area, recent calibration -> should NOT recalibrate
		if calData.ShouldRecalibrate(vacuumID, 1500, 30*time.Minute) {
			calibrationTriggered = true
		}
	})

	client.onConnect(mock)
	mock.SimulateMessage(
		"valetudo/vacuum2/StatusStateAttribute/status",
		[]byte(`{"value":"docked"}`),
	)

	if calibrationTriggered {
		t.Error("calibration should NOT trigger within debounce window")
	}
}

// ---------------------------------------------------------------------------
// TestDockingAutoCalibrateFlow_MapAreaChange
//
// Verifies that a map area change triggers recalibration even within debounce.
// ---------------------------------------------------------------------------

func TestDockingAutoCalibrateFlow_MapAreaChange(t *testing.T) {
	calData := &CalibrationData{
		ReferenceVacuum: "vacuum1",
		Vacuums: map[string]VacuumCalibration{
			"vacuum2": {
				Transform:            Identity(),
				LastUpdated:          time.Now().Unix(), // recent
				MapAreaAtCalibration: 1500,              // old area
			},
		},
		LastUpdated: time.Now().Unix(),
	}

	mock := NewMockClient()
	mock.SetConnected(true)

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "valetudo/vacuum1/MapData/map-data"},
			{ID: "vacuum2", Topic: "valetudo/vacuum2/MapData/map-data"},
		},
	}

	client := newMQTTClientWithMock(mock, config, func(string, []byte, *ValetudoMap, error) {})

	calibrationTriggered := false

	client.SetDockingHandler(func(vacuumID string) {
		// New area (2000) differs from cached area (1500) -> should recalibrate
		if calData.ShouldRecalibrate(vacuumID, 2000, 30*time.Minute) {
			calibrationTriggered = true
		}
	})

	client.onConnect(mock)
	mock.SimulateMessage(
		"valetudo/vacuum2/StatusStateAttribute/status",
		[]byte(`{"value":"docked"}`),
	)

	if !calibrationTriggered {
		t.Error("calibration should trigger when map area changes")
	}
}

// ---------------------------------------------------------------------------
// TestDockingAutoCalibrateFlow_APIFailure
//
// Verifies graceful handling when the API server is unavailable.
// ---------------------------------------------------------------------------

func TestDockingAutoCalibrateFlow_APIFailure(t *testing.T) {
	// Server that always returns 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	mock := NewMockClient()
	mock.SetConnected(true)

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "valetudo/vacuum1/MapData/map-data"},
			{ID: "vacuum2", Topic: "valetudo/vacuum2/MapData/map-data"},
		},
	}

	client := newMQTTClientWithMock(mock, config, func(string, []byte, *ValetudoMap, error) {})

	calData := &CalibrationData{
		ReferenceVacuum: "vacuum1",
		Vacuums:         map[string]VacuumCalibration{},
	}

	var fetchErr error

	client.SetDockingHandler(func(vacuumID string) {
		if !calData.ShouldRecalibrate(vacuumID, 1500, 30*time.Minute) {
			return
		}
		_, err := FetchMapFromAPI(srv.URL,
			WithHTTPClient(srv.Client()),
			WithMaxRetries(1),
			WithBaseBackoff(1*time.Millisecond),
		)
		fetchErr = err
	})

	client.onConnect(mock)
	mock.SimulateMessage(
		"valetudo/vacuum2/StatusStateAttribute/status",
		[]byte(`{"value":"docked"}`),
	)

	if fetchErr == nil {
		t.Error("expected fetch error when API returns 500")
	}
}

// ---------------------------------------------------------------------------
// TestDockingAutoCalibrateFlow_CachePersistence
//
// Verifies the full round-trip: calibrate -> save -> load -> verify transforms.
// ---------------------------------------------------------------------------

func TestDockingAutoCalibrateFlow_CachePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cal.json")

	// Build calibration data with a non-identity transform
	cal := &CalibrationData{
		ReferenceVacuum: "vacuum1",
		Vacuums: map[string]VacuumCalibration{
			"vacuum1": {
				Transform:            Identity(),
				LastUpdated:          time.Now().Unix(),
				MapAreaAtCalibration: 2000,
			},
		},
	}

	// Simulate auto-calibration adding a new vacuum
	cal.UpdateVacuumCalibration("vacuum2", VacuumCalibration{
		Transform:            AffineMatrix{A: 0.999, B: -0.045, Tx: -150.5, C: 0.045, D: 0.999, Ty: 200.3},
		LastUpdated:          time.Now().Unix(),
		MapAreaAtCalibration: 1500,
	})

	// Save to disk
	if err := SaveCalibration(cachePath, cal); err != nil {
		t.Fatalf("SaveCalibration: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file does not exist: %v", err)
	}

	// Load from disk
	loaded, err := LoadCalibration(cachePath)
	if err != nil {
		t.Fatalf("LoadCalibration: %v", err)
	}

	// Verify vacuum2 transform survived round-trip
	vc := loaded.GetVacuumCalibration("vacuum2")
	if vc == nil {
		t.Fatal("vacuum2 missing after round-trip")
		return
	}

	const tolerance = 0.001
	if diff := vc.Transform.Tx - (-150.5); diff > tolerance || diff < -tolerance {
		t.Errorf("vacuum2 Transform.Tx = %g, want -150.5", vc.Transform.Tx)
	}
	if diff := vc.Transform.Ty - 200.3; diff > tolerance || diff < -tolerance {
		t.Errorf("vacuum2 Transform.Ty = %g, want 200.3", vc.Transform.Ty)
	}
	if vc.MapAreaAtCalibration != 1500 {
		t.Errorf("vacuum2 MapAreaAtCalibration = %d, want 1500", vc.MapAreaAtCalibration)
	}
}

// ---------------------------------------------------------------------------
// TestDockingAutoCalibrateFlow_NonDockedStatesIgnored
//
// Verifies that non-docked states (cleaning, idle, returning) do not trigger
// the calibration handler.
// ---------------------------------------------------------------------------

func TestDockingAutoCalibrateFlow_NonDockedStatesIgnored(t *testing.T) {
	mock := NewMockClient()
	mock.SetConnected(true)

	config := &Config{
		Vacuums: []VacuumConfig{
			{ID: "vacuum1", Topic: "valetudo/vacuum1/MapData/map-data"},
		},
	}

	client := newMQTTClientWithMock(mock, config, func(string, []byte, *ValetudoMap, error) {})

	handlerCalled := false
	client.SetDockingHandler(func(vacuumID string) {
		handlerCalled = true
	})

	client.onConnect(mock)

	nonDockedStates := []string{
		`{"value":"cleaning"}`,
		`{"value":"idle"}`,
		`{"value":"returning"}`,
		`{"value":"paused"}`,
		`{"value":"error"}`,
		`{"value":""}`,
	}

	for _, state := range nonDockedStates {
		handlerCalled = false
		mock.SimulateMessage("valetudo/vacuum1/StatusStateAttribute/status", []byte(state))
		if handlerCalled {
			t.Errorf("docking handler should not fire for state %s", state)
		}
	}
}
