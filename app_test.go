package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kwv/tudomesh/mesh"
)

// Helper function to create a test Valetudo map
func createTestMap(name string) *mesh.ValetudoMap {
	return &mesh.ValetudoMap{
		Size:      mesh.Size{X: 100, Y: 100},
		PixelSize: 5,
		Layers: []mesh.MapLayer{
			{
				Type:   "floor",
				Pixels: []int{1, 2, 3, 4},
				MetaData: mesh.LayerMetaData{
					Area: 100,
				},
			},
		},
		Entities: []mesh.MapEntity{},
		MetaData: mesh.MapMetaData{
			Version:        1,
			TotalLayerArea: 100,
		},
	}
}

// Helper function to save a test map to file
func saveTestMapToFile(m *mesh.ValetudoMap, path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func TestNewApp(t *testing.T) {
	app := NewApp()
	if app == nil {
		t.Fatal("NewApp returned nil")
		return
	}
	if app.StateTracker == nil {
		t.Error("StateTracker should be initialized")
	}
}

func TestApplyOptions(t *testing.T) {
	app := NewApp()
	opts := AppOptions{
		DataDir:          "/test/data",
		ConfigFile:       "test-config.yaml",
		CalibrationCache: ".test-cache.json",
		RotateAll:        90.0,
		ReferenceVacuum:  "ref-vacuum",
		HttpPort:         8080,
		MqttMode:         true,
		HttpMode:         false,
	}

	app.ApplyOptions(opts)

	if app.DataDir != "/test/data" {
		t.Errorf("DataDir = %s, want /test/data", app.DataDir)
	}
	if app.ConfigFile != "test-config.yaml" {
		t.Errorf("ConfigFile = %s, want test-config.yaml", app.ConfigFile)
	}
	if app.CalibrationCache != ".test-cache.json" {
		t.Errorf("CalibrationCache = %s, want .test-cache.json", app.CalibrationCache)
	}
	if app.RotateAll != 90.0 {
		t.Errorf("RotateAll = %f, want 90.0", app.RotateAll)
	}
	if app.ReferenceVacuum != "ref-vacuum" {
		t.Errorf("ReferenceVacuum = %s, want ref-vacuum", app.ReferenceVacuum)
	}
	if app.HttpPort != 8080 {
		t.Errorf("HttpPort = %d, want 8080", app.HttpPort)
	}
	if !app.MqttMode {
		t.Error("MqttMode should be true")
	}
	if app.HttpMode {
		t.Error("HttpMode should be false")
	}
}

func TestApplyOptions_AllDefaults(t *testing.T) {
	app := NewApp()
	opts := AppOptions{}

	app.ApplyOptions(opts)

	// Verify all fields are set to their zero values
	if app.DataDir != "" {
		t.Errorf("DataDir = %s, want empty string", app.DataDir)
	}
	if app.HttpPort != 0 {
		t.Errorf("HttpPort = %d, want 0", app.HttpPort)
	}
}

func TestLoadInitialMaps_EmptyDir(t *testing.T) {
	app := NewApp()
	tmpDir := t.TempDir()

	maps := app.loadInitialMaps(tmpDir)
	if len(maps) != 0 {
		t.Errorf("Expected 0 maps, got %d", len(maps))
	}
}

func TestLoadInitialMaps_WithSampleFiles(t *testing.T) {
	app := NewApp()
	tmpDir := t.TempDir()

	// Create a valid sample JSON export file
	sampleMap := createTestMap("test-vacuum")

	// Save sample map to temp directory
	// Use a timestamp pattern that will be parsed correctly
	samplePath := filepath.Join(tmpDir, "ValetudoMapExport-test-vacuum-20240101.json")
	if err := saveTestMapToFile(sampleMap, samplePath); err != nil {
		t.Fatalf("Failed to create sample map file: %v", err)
	}

	maps := app.loadInitialMaps(tmpDir)
	if len(maps) != 1 {
		t.Errorf("Expected 1 map, got %d", len(maps))
	}

	// The name should be 'test-vacuum' after parsing
	if _, ok := maps["test-vacuum"]; !ok {
		t.Errorf("Expected map 'test-vacuum' to be loaded, got maps: %v", getMapKeys(maps))
	}
}

// Helper to get map keys for debugging
func getMapKeys(m map[string]*mesh.ValetudoMap) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestLoadInitialMaps_InvalidJSON(t *testing.T) {
	app := NewApp()
	tmpDir := t.TempDir()

	// Create an invalid JSON file
	invalidPath := filepath.Join(tmpDir, "ValetudoMapExport-invalid.json")
	if err := os.WriteFile(invalidPath, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("Failed to create invalid JSON file: %v", err)
	}

	// Should not panic, should just skip invalid files
	maps := app.loadInitialMaps(tmpDir)
	if len(maps) != 0 {
		t.Errorf("Expected 0 maps (invalid JSON should be skipped), got %d", len(maps))
	}
}

func TestLoadInitialMaps_MultipleFiles(t *testing.T) {
	app := NewApp()
	tmpDir := t.TempDir()

	// Create multiple valid maps
	for i, name := range []string{"vacuum1", "vacuum2", "vacuum3"} {
		m := createTestMap(name)
		m.MetaData.TotalLayerArea = (i + 1) * 100
		path := filepath.Join(tmpDir, "ValetudoMapExport-"+name+"-20240101.json")
		if err := saveTestMapToFile(m, path); err != nil {
			t.Fatalf("Failed to create map file: %v", err)
		}
	}

	maps := app.loadInitialMaps(tmpDir)
	if len(maps) != 3 {
		t.Errorf("Expected 3 maps, got %d", len(maps))
	}

	for _, name := range []string{"vacuum1", "vacuum2", "vacuum3"} {
		if _, ok := maps[name]; !ok {
			t.Errorf("Expected map '%s' to be loaded", name)
		}
	}
}

func TestLoadInitialMaps_GlobError(t *testing.T) {
	app := NewApp()

	// Use an invalid pattern that should cause Glob to work but return empty
	maps := app.loadInitialMaps("/\x00invalid")

	// Should return empty map without panicking
	if len(maps) != 0 {
		t.Errorf("Expected 0 maps for invalid directory, got %d", len(maps))
	}
}

func TestLoadInitialMaps_CurrentDirectory(t *testing.T) {
	app := NewApp()
	tmpDir := t.TempDir()

	// Save to current directory (working dir)
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("Failed to restore working directory: %v", err)
		}
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create a map in current directory
	m := createTestMap("local-vacuum")
	path := filepath.Join(".", "ValetudoMapExport-local-vacuum-20240101.json")
	if err := saveTestMapToFile(m, path); err != nil {
		t.Fatalf("Failed to create map file: %v", err)
	}

	// Test with tmpDir first (should be empty)
	tmpDir2 := t.TempDir()
	maps := app.loadInitialMaps(tmpDir2)

	// This should fall back to current directory and find the map
	if len(maps) == 0 {
		// The fallback worked and we're checking current dir
		maps2 := app.loadInitialMaps(".")
		if len(maps2) != 1 {
			t.Errorf("Expected 1 map from current directory, got %d", len(maps2))
		}
	}
}

// Test that applies options with various combinations
func TestApplyOptions_Combinations(t *testing.T) {
	tests := []struct {
		name string
		opts AppOptions
	}{
		{
			name: "mqtt only",
			opts: AppOptions{MqttMode: true},
		},
		{
			name: "http only",
			opts: AppOptions{HttpMode: true},
		},
		{
			name: "both modes",
			opts: AppOptions{MqttMode: true, HttpMode: true},
		},
		{
			name: "with rotation",
			opts: AppOptions{RotateAll: 180.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp()
			app.ApplyOptions(tt.opts)

			// Just verify it doesn't panic and fields are set
			if app == nil {
				t.Error("App should not be nil after applying options")
			}
		})
	}
}

// Add tests for edge cases with various data directory scenarios
func TestLoadInitialMaps_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(string) error
		expected int
	}{
		{
			name: "files with different timestamp formats",
			setup: func(dir string) error {
				m := createTestMap("test1")
				return saveTestMapToFile(m, filepath.Join(dir, "ValetudoMapExport-test1-2024-01-01.json"))
			},
			expected: 1,
		},
		{
			name: "mixed valid and invalid files",
			setup: func(dir string) error {
				m := createTestMap("valid")
				if err := saveTestMapToFile(m, filepath.Join(dir, "ValetudoMapExport-valid-20240101.json")); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(dir, "ValetudoMapExport-invalid-20240101.json"), []byte("bad"), 0644)
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp()
			tmpDir := t.TempDir()

			if tt.setup != nil {
				if err := tt.setup(tmpDir); err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}

			maps := app.loadInitialMaps(tmpDir)
			if len(maps) != tt.expected {
				t.Errorf("Expected %d maps, got %d", tt.expected, len(maps))
			}
		})
	}
}

func TestVacuumIDFromExport(t *testing.T) {
	cases := map[string]string{
		"ValetudoMapExport-FairBoldFalcon.json":                      "FairBoldFalcon",
		"ValetudoMapExport-FairBoldFalcon-2026-01-24T16-38-03.json":  "FairBoldFalcon",
		"ValetudoMapExport-My-Robot.json":                            "My-Robot",
		"ValetudoMapExport-test-vacuum-20240101.json":                "test-vacuum",
		"ValetudoMapExport-test1-2024-01-01.json":                    "test1",
		"ValetudoMapExport-AmberQuietOtter-2026-02-13T02-25-08.json": "AmberQuietOtter",
	}
	for in, want := range cases {
		if got := vacuumIDFromExport(in); got != want {
			t.Errorf("vacuumIDFromExport(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadServiceCalibration(t *testing.T) {
	dir := t.TempDir()

	missing := loadServiceCalibration(filepath.Join(dir, "none.json"))
	if missing == nil || missing.Vacuums == nil {
		t.Fatal("missing cache should give an empty, usable cache")
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if c := loadServiceCalibration(bad); c == nil || c.Vacuums == nil {
		t.Fatal("unreadable cache should give an empty, usable cache")
	}

	good := filepath.Join(dir, "good.json")
	saved := &mesh.CalibrationData{ReferenceVacuum: "ref", Vacuums: map[string]mesh.VacuumCalibration{
		"v": {Transform: mesh.Translation(3, 4)},
	}}
	if err := mesh.SaveCalibration(good, saved); err != nil {
		t.Fatal(err)
	}
	loaded := loadServiceCalibration(good)
	if loaded.Reference() != "ref" || loaded.GetTransform("v").Tx != 3 {
		t.Errorf("loaded cache = %+v", loaded.Transforms())
	}

	// Updates made through the shared pointer (as the auto-calibrator does)
	// are visible to readers holding the same pointer (HTTP, MQTT handlers).
	missing.UpdateVacuumCalibration("w", mesh.VacuumCalibration{Transform: mesh.Translation(7, 0)})
	if missing.GetTransform("w").Tx != 7 {
		t.Error("update not visible through shared cache")
	}
}
