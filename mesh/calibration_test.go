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
		Vacuums: map[string]AffineMatrix{
			"vac-a": Identity(),
			"vac-b": {A: 0, B: -1, Tx: 100, C: 1, D: 0, Ty: 200},
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
	if got.Vacuums["vac-b"].Tx != 100 {
		t.Errorf("vac-b.Tx = %g, want 100", got.Vacuums["vac-b"].Tx)
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
		Vacuums: map[string]AffineMatrix{
			"vac-a": Identity(),
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
		Vacuums: map[string]AffineMatrix{
			"vac-a": Identity(),
			"vac-b": {A: 2, B: 0, Tx: 50, C: 0, D: 2, Ty: 75},
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
// CalibrationData.NeedsRecalibration
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
