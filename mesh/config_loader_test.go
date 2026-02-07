package mesh

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func floatPtr(v float64) *float64 { return &v }

func validConfigYAML() string {
	return `mqtt:
  broker: tcp://localhost:1883
  publishPrefix: tudomesh
  clientId: tudomesh-test
vacuums:
  - id: vac-a
    topic: valetudo/vac-a
    color: "#FF0000"
  - id: vac-b
    topic: valetudo/vac-b
    color: "#00FF00"
`
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// LoadConfig
// ---------------------------------------------------------------------------

func TestLoadConfig_NotExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing config file, got nil")
	}
}

func TestLoadConfig_ValidYAML(t *testing.T) {
	path := writeConfig(t, validConfigYAML())

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MQTT.Broker != "tcp://localhost:1883" {
		t.Errorf("Broker = %q, want %q", cfg.MQTT.Broker, "tcp://localhost:1883")
	}
	if len(cfg.Vacuums) != 2 {
		t.Fatalf("len(Vacuums) = %d, want 2", len(cfg.Vacuums))
	}
	if cfg.Vacuums[0].ID != "vac-a" {
		t.Errorf("Vacuums[0].ID = %q, want %q", cfg.Vacuums[0].ID, "vac-a")
	}
	if cfg.Vacuums[1].Topic != "valetudo/vac-b" {
		t.Errorf("Vacuums[1].Topic = %q, want %q", cfg.Vacuums[1].Topic, "valetudo/vac-b")
	}
}

func TestLoadConfig_Validation(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "missing broker",
			yaml: `mqtt:
  broker: ""
vacuums:
  - id: v1
    topic: t/v1
`,
		},
		{
			name: "empty vacuums list",
			yaml: `mqtt:
  broker: tcp://localhost:1883
vacuums: []
`,
		},
		{
			name: "vacuum missing id",
			yaml: `mqtt:
  broker: tcp://localhost:1883
vacuums:
  - id: ""
    topic: t/v1
`,
		},
		{
			name: "vacuum missing topic",
			yaml: `mqtt:
  broker: tcp://localhost:1883
vacuums:
  - id: v1
    topic: ""
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.yaml)
			_, err := LoadConfig(path)
			if err == nil {
				t.Errorf("expected validation error for %q, got nil", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SaveConfig
// ---------------------------------------------------------------------------

func TestSaveConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.yaml")

	original := &Config{
		MQTT: MQTTConfig{
			Broker:        "tcp://localhost:1883",
			PublishPrefix: "tudomesh",
			ClientID:      "test-client",
		},
		Vacuums: []VacuumConfig{
			{ID: "vac-a", Topic: "valetudo/vac-a", Color: "#FF0000"},
		},
		GridSpacing: 500,
	}

	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Round-trip: LoadConfig must succeed and reproduce the data
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after save: %v", err)
	}
	if loaded.MQTT.Broker != original.MQTT.Broker {
		t.Errorf("Broker = %q, want %q", loaded.MQTT.Broker, original.MQTT.Broker)
	}
	if loaded.GridSpacing != 500 {
		t.Errorf("GridSpacing = %g, want 500", loaded.GridSpacing)
	}
	if len(loaded.Vacuums) != 1 || loaded.Vacuums[0].ID != "vac-a" {
		t.Errorf("Vacuums round-trip mismatch: %+v", loaded.Vacuums)
	}
}

// ---------------------------------------------------------------------------
// BuildForceRotationMap
// ---------------------------------------------------------------------------

func TestBuildForceRotationMap(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		got := BuildForceRotationMap("")
		if len(got) != 0 {
			t.Errorf("empty input: got %v, want empty map", got)
		}
	})

	t.Run("single entry", func(t *testing.T) {
		got := BuildForceRotationMap("vac-a=90")
		if len(got) != 1 {
			t.Fatalf("single entry: len = %d, want 1", len(got))
		}
		if got["vac-a"] != 90 {
			t.Errorf("vac-a = %g, want 90", got["vac-a"])
		}
	})

	t.Run("multiple entries", func(t *testing.T) {
		got := BuildForceRotationMap("vac-a=90,vac-b=180,vac-c=270")
		if len(got) != 3 {
			t.Fatalf("multiple entries: len = %d, want 3", len(got))
		}
		if got["vac-a"] != 90 {
			t.Errorf("vac-a = %g, want 90", got["vac-a"])
		}
		if got["vac-b"] != 180 {
			t.Errorf("vac-b = %g, want 180", got["vac-b"])
		}
		if got["vac-c"] != 270 {
			t.Errorf("vac-c = %g, want 270", got["vac-c"])
		}
	})

	t.Run("malformed entries skipped", func(t *testing.T) {
		// "bad" has no =, "vac-x=abc" has non-numeric value
		got := BuildForceRotationMap("bad,vac-x=abc,vac-ok=45")
		if len(got) != 1 {
			t.Fatalf("malformed: len = %d, want 1 (only vac-ok)", len(got))
		}
		if got["vac-ok"] != 45 {
			t.Errorf("vac-ok = %g, want 45", got["vac-ok"])
		}
	})

	t.Run("fractional degrees", func(t *testing.T) {
		got := BuildForceRotationMap("vac-f=33.5")
		if got["vac-f"] != 33.5 {
			t.Errorf("vac-f = %g, want 33.5", got["vac-f"])
		}
	})
}

// ---------------------------------------------------------------------------
// MergeCalibrationIntoConfig
// ---------------------------------------------------------------------------

func TestMergeCalibrationIntoConfig(t *testing.T) {
	t.Run("nil cache yields manual-only transforms", func(t *testing.T) {
		rot := floatPtr(90)
		cfg := &Config{
			Vacuums: []VacuumConfig{
				{ID: "vac-a", Topic: "t/a", Rotation: rot},
			},
		}
		result := MergeCalibrationIntoConfig(cfg, nil)
		if _, ok := result["vac-a"]; !ok {
			t.Fatal("vac-a missing from result")
		}
		// 90-degree rotation: A ~= 0, C ~= 1
		if result["vac-a"].A > 0.01 || result["vac-a"].A < -0.01 {
			t.Errorf("90-deg rotation A = %g, want ~0", result["vac-a"].A)
		}
	})

	t.Run("cache only, no manual overrides", func(t *testing.T) {
		cfg := &Config{
			Vacuums: []VacuumConfig{
				{ID: "vac-a", Topic: "t/a"}, // no Rotation, no Translation
				{ID: "vac-b", Topic: "t/b"},
			},
		}
		cache := &CalibrationData{
			Vacuums: map[string]VacuumCalibration{
				"vac-a": {Transform: AffineMatrix{A: 1, B: 0, Tx: 10, C: 0, D: 1, Ty: 20}},
				"vac-b": {Transform: Identity()},
			},
		}
		result := MergeCalibrationIntoConfig(cfg, cache)
		if result["vac-a"].Tx != 10 {
			t.Errorf("vac-a.Tx = %g, want 10 (from cache)", result["vac-a"].Tx)
		}
		if result["vac-b"] != Identity() {
			t.Errorf("vac-b = %+v, want identity (from cache)", result["vac-b"])
		}
	})

	t.Run("manual override wins over cache", func(t *testing.T) {
		cfg := &Config{
			Vacuums: []VacuumConfig{
				{ID: "vac-a", Topic: "t/a", Rotation: floatPtr(0), Translation: &TranslationOffset{X: 99, Y: 88}},
			},
		}
		cache := &CalibrationData{
			Vacuums: map[string]VacuumCalibration{
				"vac-a": {Transform: AffineMatrix{A: 1, B: 0, Tx: 10, C: 0, D: 1, Ty: 20}}, // should be overridden
			},
		}
		result := MergeCalibrationIntoConfig(cfg, cache)
		// 0-degree rotation + translation (99, 88) => Tx=99, Ty=88
		if result["vac-a"].Tx != 99 {
			t.Errorf("vac-a.Tx = %g, want 99 (manual override)", result["vac-a"].Tx)
		}
		if result["vac-a"].Ty != 88 {
			t.Errorf("vac-a.Ty = %g, want 88 (manual override)", result["vac-a"].Ty)
		}
	})
}

// ---------------------------------------------------------------------------
// GetEffectiveReference
// ---------------------------------------------------------------------------

func TestGetEffectiveReference(t *testing.T) {
	maps := map[string]*ValetudoMap{
		"vac-a": {MetaData: MapMetaData{TotalLayerArea: 100}},
		"vac-b": {MetaData: MapMetaData{TotalLayerArea: 900}},
	}

	t.Run("config reference present in maps", func(t *testing.T) {
		cfg := &Config{Reference: "vac-a"}
		got := GetEffectiveReference(cfg, nil, maps)
		if got != "vac-a" {
			t.Errorf("config ref: got %q, want %q", got, "vac-a")
		}
	})

	t.Run("config reference not in maps falls to cache", func(t *testing.T) {
		cfg := &Config{Reference: "ghost"}
		cache := &CalibrationData{ReferenceVacuum: "vac-b"}
		got := GetEffectiveReference(cfg, cache, maps)
		if got != "vac-b" {
			t.Errorf("cache fallback: got %q, want %q", got, "vac-b")
		}
	})

	t.Run("cache reference not in maps falls to auto-select", func(t *testing.T) {
		cfg := &Config{Reference: "ghost"}
		cache := &CalibrationData{ReferenceVacuum: "also-ghost"}
		got := GetEffectiveReference(cfg, cache, maps)
		if got != "vac-b" {
			t.Errorf("auto-select fallback: got %q, want %q (largest area)", got, "vac-b")
		}
	})

	t.Run("nil cache falls to auto-select", func(t *testing.T) {
		cfg := &Config{} // empty Reference
		got := GetEffectiveReference(cfg, nil, maps)
		if got != "vac-b" {
			t.Errorf("nil cache auto-select: got %q, want %q", got, "vac-b")
		}
	})

	t.Run("empty config and nil cache with empty maps", func(t *testing.T) {
		cfg := &Config{}
		got := GetEffectiveReference(cfg, nil, map[string]*ValetudoMap{})
		if got != "" {
			t.Errorf("all empty: got %q, want empty string", got)
		}
	})
}
