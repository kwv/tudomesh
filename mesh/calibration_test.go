package mesh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// LoadCalibration
// ---------------------------------------------------------------------------

func TestLoadCalibration_NotExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-file.json")

	cal, err := LoadCalibration(path)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if cal != nil {
		t.Fatal("expected nil CalibrationData for missing file")
	}
}

func TestLoadCalibration_ValidFile(t *testing.T) {
	want := &CalibrationData{
		ReferenceVacuum: "vac-a",
		Vacuums: map[string]VacuumCalibration{
			"vac-a": {Transform: Identity(), LastUpdated: 1700000000},
			"vac-b": {Transform: AffineMatrix{A: 0, B: -1, Tx: 100, C: 1, D: 0, Ty: 200}, LastUpdated: 1700000000},
		},
		LastUpdated: 1700000000,
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	path := filepath.Join(t.TempDir(), "cal.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := LoadCalibration(path)
	if err != nil {
		t.Fatalf("LoadCalibration: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil CalibrationData")
	}
	if got.ReferenceVacuum != want.ReferenceVacuum {
		t.Errorf("ReferenceVacuum = %q, want %q", got.ReferenceVacuum, want.ReferenceVacuum)
	}
	if len(got.Vacuums) != 2 {
		t.Errorf("len(Vacuums) = %d, want 2", len(got.Vacuums))
	}
	if got.Vacuums["vac-b"].Transform.Tx != 100 {
		t.Errorf("vac-b.Transform.Tx = %g, want 100", got.Vacuums["vac-b"].Transform.Tx)
	}
}

func TestLoadCalibration_LegacyFormat(t *testing.T) {
	// Simulate old cache file where Vacuums was map[string]AffineMatrix
	legacy := `{
		"referenceVacuum": "vac-a",
		"vacuums": {
			"vac-a": {"a":1,"b":0,"tx":0,"c":0,"d":1,"ty":0},
			"vac-b": {"a":0,"b":-1,"tx":100,"c":1,"d":0,"ty":200}
		},
		"lastUpdated": 1700000000
	}`

	path := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	got, err := LoadCalibration(path)
	if err != nil {
		t.Fatalf("LoadCalibration (legacy): %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil CalibrationData from legacy format")
	}
	if got.ReferenceVacuum != "vac-a" {
		t.Errorf("ReferenceVacuum = %q, want %q", got.ReferenceVacuum, "vac-a")
	}
	if len(got.Vacuums) != 2 {
		t.Errorf("len(Vacuums) = %d, want 2", len(got.Vacuums))
	}
	// Verify the transform was migrated correctly
	vacB := got.Vacuums["vac-b"]
	if vacB.Transform.Tx != 100 {
		t.Errorf("vac-b.Transform.Tx = %g, want 100", vacB.Transform.Tx)
	}
	// Legacy entries inherit the global LastUpdated
	if vacB.LastUpdated != 1700000000 {
		t.Errorf("vac-b.LastUpdated = %d, want 1700000000", vacB.LastUpdated)
	}
}

func TestLoadCalibration_CorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{not valid json!!!"), 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	_, err := LoadCalibration(path)
	if err == nil {
		t.Fatal("expected error for corrupt JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// SaveCalibration
// ---------------------------------------------------------------------------

func TestSaveCalibration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "dir") // nested -- MkdirAll must fire
	path := filepath.Join(dir, "cal.json")

	before := time.Now().Unix()
	cal := &CalibrationData{
		ReferenceVacuum: "vac-a",
		Vacuums: map[string]VacuumCalibration{
			"vac-a": {Transform: Identity(), LastUpdated: before},
		},
		LastUpdated: 0, // should be overwritten
	}

	if err := SaveCalibration(path, cal); err != nil {
		t.Fatalf("SaveCalibration: %v", err)
	}
	after := time.Now().Unix()

	// Timestamp must have been updated by SaveCalibration
	if cal.LastUpdated < before || cal.LastUpdated > after {
		t.Errorf("LastUpdated = %d, want between %d and %d", cal.LastUpdated, before, after)
	}

	// Round-trip: load back and verify
	loaded, err := LoadCalibration(path)
	if err != nil {
		t.Fatalf("LoadCalibration after save: %v", err)
	}
	if loaded.ReferenceVacuum != "vac-a" {
		t.Errorf("ReferenceVacuum = %q, want %q", loaded.ReferenceVacuum, "vac-a")
	}
	if _, ok := loaded.Vacuums["vac-a"]; !ok {
		t.Error("vac-a missing from loaded Vacuums")
	}
}

// ---------------------------------------------------------------------------
// CalibrationData.GetTransform
// ---------------------------------------------------------------------------

func TestCalibrationData_GetTransform(t *testing.T) {
	cal := &CalibrationData{
		Vacuums: map[string]VacuumCalibration{
			"vac-a": {Transform: Identity()},
			"vac-b": {Transform: AffineMatrix{A: 2, B: 0, Tx: 50, C: 0, D: 2, Ty: 75}},
		},
	}

	t.Run("nil receiver", func(t *testing.T) {
		var nilCal *CalibrationData
		got := nilCal.GetTransform("anything")
		if got != Identity() {
			t.Errorf("nil receiver: got %+v, want identity", got)
		}
	})

	t.Run("missing vacuum ID", func(t *testing.T) {
		got := cal.GetTransform("does-not-exist")
		if got != Identity() {
			t.Errorf("missing ID: got %+v, want identity", got)
		}
	})

	t.Run("present vacuum ID", func(t *testing.T) {
		got := cal.GetTransform("vac-b")
		want := AffineMatrix{A: 2, B: 0, Tx: 50, C: 0, D: 2, Ty: 75}
		if got != want {
			t.Errorf("vac-b: got %+v, want %+v", got, want)
		}
	})

	t.Run("nil Vacuums map", func(t *testing.T) {
		nilMap := &CalibrationData{Vacuums: nil}
		got := nilMap.GetTransform("vac-a")
		if got != Identity() {
			t.Errorf("nil Vacuums map: got %+v, want identity", got)
		}
	})
}

// ---------------------------------------------------------------------------
// CalibrationData.NeedsRecalibration (global)
// ---------------------------------------------------------------------------

func TestCalibrationData_NeedsRecalibration(t *testing.T) {
	maxAge := 24 * time.Hour

	t.Run("nil receiver", func(t *testing.T) {
		var nilCal *CalibrationData
		if !nilCal.NeedsRecalibration(maxAge) {
			t.Error("nil receiver should need recalibration")
		}
	})

	t.Run("zero timestamp", func(t *testing.T) {
		cal := &CalibrationData{LastUpdated: 0}
		if !cal.NeedsRecalibration(maxAge) {
			t.Error("zero timestamp should need recalibration")
		}
	})

	t.Run("recent update", func(t *testing.T) {
		cal := &CalibrationData{LastUpdated: time.Now().Unix()}
		if cal.NeedsRecalibration(maxAge) {
			t.Error("recent timestamp should NOT need recalibration")
		}
	})

	t.Run("stale update", func(t *testing.T) {
		stale := time.Now().Add(-48 * time.Hour).Unix()
		cal := &CalibrationData{LastUpdated: stale}
		if !cal.NeedsRecalibration(maxAge) {
			t.Error("48h-old timestamp should need recalibration with 24h maxAge")
		}
	})
}

// ---------------------------------------------------------------------------
// CalibrationData.ShouldRecalibrate (per-vacuum)
// ---------------------------------------------------------------------------

func TestCalibrationData_ShouldRecalibrate(t *testing.T) {
	debounce := 30 * time.Minute

	t.Run("nil receiver", func(t *testing.T) {
		var nilCal *CalibrationData
		if !nilCal.ShouldRecalibrate("vac-a", 5000, debounce) {
			t.Error("nil receiver should need recalibration")
		}
	})

	t.Run("never calibrated", func(t *testing.T) {
		cal := &CalibrationData{
			Vacuums: map[string]VacuumCalibration{
				"vac-a": {Transform: Identity(), LastUpdated: time.Now().Unix()},
			},
		}
		if !cal.ShouldRecalibrate("vac-b", 5000, debounce) {
			t.Error("uncalibrated vacuum should need recalibration")
		}
	})

	t.Run("map area changed", func(t *testing.T) {
		cal := &CalibrationData{
			Vacuums: map[string]VacuumCalibration{
				"vac-a": {
					Transform:            Identity(),
					LastUpdated:          time.Now().Unix(),
					MapAreaAtCalibration: 5000,
				},
			},
		}
		if !cal.ShouldRecalibrate("vac-a", 6000, debounce) {
			t.Error("changed map area should trigger recalibration")
		}
	})

	t.Run("within debounce window", func(t *testing.T) {
		cal := &CalibrationData{
			Vacuums: map[string]VacuumCalibration{
				"vac-a": {
					Transform:            Identity(),
					LastUpdated:          time.Now().Unix(),
					MapAreaAtCalibration: 5000,
				},
			},
		}
		if cal.ShouldRecalibrate("vac-a", 5000, debounce) {
			t.Error("recent calibration with same area should NOT need recalibration")
		}
	})

	t.Run("outside debounce window", func(t *testing.T) {
		stale := time.Now().Add(-1 * time.Hour).Unix()
		cal := &CalibrationData{
			Vacuums: map[string]VacuumCalibration{
				"vac-a": {
					Transform:            Identity(),
					LastUpdated:          stale,
					MapAreaAtCalibration: 5000,
				},
			},
		}
		if !cal.ShouldRecalibrate("vac-a", 5000, debounce) {
			t.Error("stale calibration should need recalibration")
		}
	})

	t.Run("zero LastUpdated", func(t *testing.T) {
		cal := &CalibrationData{
			Vacuums: map[string]VacuumCalibration{
				"vac-a": {Transform: Identity(), LastUpdated: 0},
			},
		}
		if !cal.ShouldRecalibrate("vac-a", 5000, debounce) {
			t.Error("zero LastUpdated should need recalibration")
		}
	})
}

// ---------------------------------------------------------------------------
// CalibrationData.UpdateVacuumCalibration
// ---------------------------------------------------------------------------

func TestCalibrationData_UpdateVacuumCalibration(t *testing.T) {
	t.Run("nil map initializes", func(t *testing.T) {
		cal := &CalibrationData{}
		now := time.Now().Unix()
		cal.UpdateVacuumCalibration("vac-a", VacuumCalibration{
			Transform:            Identity(),
			LastUpdated:          now,
			MapAreaAtCalibration: 5000,
		})
		if len(cal.Vacuums) != 1 {
			t.Fatalf("expected 1 vacuum, got %d", len(cal.Vacuums))
		}
		vc := cal.Vacuums["vac-a"]
		if vc.Transform != Identity() {
			t.Errorf("transform = %+v, want identity", vc.Transform)
		}
		if vc.MapAreaAtCalibration != 5000 {
			t.Errorf("MapAreaAtCalibration = %d, want 5000", vc.MapAreaAtCalibration)
		}
	})

	t.Run("updates global LastUpdated", func(t *testing.T) {
		cal := &CalibrationData{
			LastUpdated: 100,
			Vacuums:     map[string]VacuumCalibration{},
		}
		cal.UpdateVacuumCalibration("vac-a", VacuumCalibration{
			Transform:   Identity(),
			LastUpdated: 200,
		})
		if cal.LastUpdated != 200 {
			t.Errorf("global LastUpdated = %d, want 200", cal.LastUpdated)
		}
	})

	t.Run("does not regress global LastUpdated", func(t *testing.T) {
		cal := &CalibrationData{
			LastUpdated: 300,
			Vacuums:     map[string]VacuumCalibration{},
		}
		cal.UpdateVacuumCalibration("vac-a", VacuumCalibration{
			Transform:   Identity(),
			LastUpdated: 200,
		})
		if cal.LastUpdated != 300 {
			t.Errorf("global LastUpdated = %d, want 300 (should not regress)", cal.LastUpdated)
		}
	})

	t.Run("replaces existing", func(t *testing.T) {
		cal := &CalibrationData{
			Vacuums: map[string]VacuumCalibration{
				"vac-a": {Transform: Identity(), LastUpdated: 100},
			},
		}
		newTransform := AffineMatrix{A: 2, D: 2}
		cal.UpdateVacuumCalibration("vac-a", VacuumCalibration{
			Transform:            newTransform,
			LastUpdated:          200,
			MapAreaAtCalibration: 9000,
		})
		vc := cal.Vacuums["vac-a"]
		if vc.Transform != newTransform {
			t.Errorf("transform = %+v, want %+v", vc.Transform, newTransform)
		}
		if vc.MapAreaAtCalibration != 9000 {
			t.Errorf("MapAreaAtCalibration = %d, want 9000", vc.MapAreaAtCalibration)
		}
	})
}

// ---------------------------------------------------------------------------
// CalibrationData.GetVacuumCalibration
// ---------------------------------------------------------------------------

func TestCalibrationData_GetVacuumCalibration(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var nilCal *CalibrationData
		if nilCal.GetVacuumCalibration("vac-a") != nil {
			t.Error("nil receiver should return nil")
		}
	})

	t.Run("missing vacuum", func(t *testing.T) {
		cal := &CalibrationData{
			Vacuums: map[string]VacuumCalibration{},
		}
		if cal.GetVacuumCalibration("vac-a") != nil {
			t.Error("missing vacuum should return nil")
		}
	})

	t.Run("present vacuum", func(t *testing.T) {
		cal := &CalibrationData{
			Vacuums: map[string]VacuumCalibration{
				"vac-a": {Transform: Identity(), LastUpdated: 100, MapAreaAtCalibration: 5000},
			},
		}
		vc := cal.GetVacuumCalibration("vac-a")
		if vc == nil {
			t.Fatal("expected non-nil VacuumCalibration")
		}
		if vc.MapAreaAtCalibration != 5000 {
			t.Errorf("MapAreaAtCalibration = %d, want 5000", vc.MapAreaAtCalibration)
		}
	})
}

// ---------------------------------------------------------------------------
// SelectReferenceVacuum
// ---------------------------------------------------------------------------

func TestSelectReferenceVacuum(t *testing.T) {
	t.Run("empty maps", func(t *testing.T) {
		got := SelectReferenceVacuum(map[string]*ValetudoMap{}, nil)
		if got != "" {
			t.Errorf("empty maps: got %q, want empty string", got)
		}
	})

	t.Run("single vacuum", func(t *testing.T) {
		maps := map[string]*ValetudoMap{
			"only": {MetaData: MapMetaData{TotalLayerArea: 500}},
		}
		got := SelectReferenceVacuum(maps, nil)
		if got != "only" {
			t.Errorf("single vacuum: got %q, want %q", got, "only")
		}
	})

	t.Run("largest area wins", func(t *testing.T) {
		maps := map[string]*ValetudoMap{
			"small":  {MetaData: MapMetaData{TotalLayerArea: 100}},
			"medium": {MetaData: MapMetaData{TotalLayerArea: 500}},
			"big":    {MetaData: MapMetaData{TotalLayerArea: 9000}},
		}
		got := SelectReferenceVacuum(maps, nil)
		if got != "big" {
			t.Errorf("largest area: got %q, want %q", got, "big")
		}
	})

	t.Run("configs param is ignored", func(t *testing.T) {
		// Passing configs with different IDs must not affect selection
		maps := map[string]*ValetudoMap{
			"alpha": {MetaData: MapMetaData{TotalLayerArea: 200}},
			"beta":  {MetaData: MapMetaData{TotalLayerArea: 800}},
		}
		configs := []VacuumConfig{
			{ID: "alpha", Topic: "t/alpha"},
			{ID: "beta", Topic: "t/beta"},
		}
		got := SelectReferenceVacuum(maps, configs)
		if got != "beta" {
			t.Errorf("configs ignored: got %q, want %q", got, "beta")
		}
	})
}
