package mesh

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func validConfigYAML() string {
	return `mqtt:
  broker: tcp://localhost:1883
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

func TestLoadConfig_RenderFontPath(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, validConfigYAML()+"render:\n  fontPath: /fonts/NotoSansCJK.ttc\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Render.FontPath != "/fonts/NotoSansCJK.ttc" {
		t.Errorf("Render.FontPath = %q", cfg.Render.FontPath)
	}
}
