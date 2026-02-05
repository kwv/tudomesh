package main

import (
	"bytes"
	"strings"
	"testing"
)

type mockApp struct {
	opts   AppOptions
	called map[string]bool
	sArg   string
}

func newMockApp() *mockApp {
	return &mockApp{
		called: make(map[string]bool),
	}
}

func (m *mockApp) ApplyOptions(opts AppOptions) { m.opts = opts }
func (m *mockApp) RunParseOnly()                { m.called["RunParseOnly"] = true }
func (m *mockApp) RunCalibration()              { m.called["RunCalibration"] = true }
func (m *mockApp) RunRender()                   { m.called["RunRender"] = true }
func (m *mockApp) RunRenderIndividual(s string) { m.called["RunRenderIndividual"] = true; m.sArg = s }
func (m *mockApp) RunCompareRotation(s string)  { m.called["RunCompareRotation"] = true; m.sArg = s }
func (m *mockApp) RunDetectRotation()           { m.called["RunDetectRotation"] = true }
func (m *mockApp) RunService()                  { m.called["RunService"] = true }

func TestRun_Flags(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedCalled string
		verifyOpts     func(*testing.T, AppOptions)
	}{
		{
			name:           "ParseOnly",
			args:           []string{"--parse-only", "--data-dir", "/tmp/data"},
			expectedCalled: "RunParseOnly",
			verifyOpts: func(t *testing.T, opts AppOptions) {
				if opts.DataDir != "/tmp/data" {
					t.Errorf("expected DataDir /tmp/data, got %s", opts.DataDir)
				}
				if !opts.ParseOnly {
					t.Error("expected ParseOnly true")
				}
			},
		},
		{
			name:           "Calibrate",
			args:           []string{"--calibrate", "--calibration-cache", "test.json"},
			expectedCalled: "RunCalibration",
			verifyOpts: func(t *testing.T, opts AppOptions) {
				if opts.CalibrationCache != "test.json" {
					t.Errorf("expected CalibrationCache test.json, got %s", opts.CalibrationCache)
				}
				if !opts.CalibrateOnly {
					t.Error("expected CalibrateOnly true")
				}
			},
		},
		{
			name:           "Render",
			args:           []string{"--render", "--output", "test.png", "--rotate-all", "90"},
			expectedCalled: "RunRender",
			verifyOpts: func(t *testing.T, opts AppOptions) {
				if opts.OutputFile != "test.png" {
					t.Errorf("expected OutputFile test.png, got %s", opts.OutputFile)
				}
				if opts.RotateAll != 90 {
					t.Errorf("expected RotateAll 90, got %f", opts.RotateAll)
				}
				if !opts.RenderOnly {
					t.Error("expected RenderOnly true")
				}
			},
		},
		{
			name:           "RenderIndividual",
			args:           []string{"--render-individual", "--individual-rotation", "vac1=180"},
			expectedCalled: "RunRenderIndividual",
			verifyOpts: func(t *testing.T, opts AppOptions) {
				if opts.IndividualRotation != "vac1=180" {
					t.Errorf("expected IndividualRotation vac1=180, got %s", opts.IndividualRotation)
				}
				if !opts.RenderIndividual {
					t.Error("expected RenderIndividual true")
				}
			},
		},
		{
			name:           "CompareRotation",
			args:           []string{"--compare-rotation", "vac2"},
			expectedCalled: "RunCompareRotation",
			verifyOpts: func(t *testing.T, opts AppOptions) {
				if opts.CompareRotation != "vac2" {
					t.Errorf("expected CompareRotation vac2, got %s", opts.CompareRotation)
				}
			},
		},
		{
			name:           "DetectRotation",
			args:           []string{"--detect-rotation", "--reference", "refVac"},
			expectedCalled: "RunDetectRotation",
			verifyOpts: func(t *testing.T, opts AppOptions) {
				if opts.ReferenceVacuum != "refVac" {
					t.Errorf("expected ReferenceVacuum refVac, got %s", opts.ReferenceVacuum)
				}
				if !opts.DetectRotation {
					t.Error("expected DetectRotation true")
				}
			},
		},
		{
			name:           "MqttMode",
			args:           []string{"--mqtt", "--http-port", "9090"},
			expectedCalled: "RunService",
			verifyOpts: func(t *testing.T, opts AppOptions) {
				if !opts.MqttMode {
					t.Error("expected MqttMode true")
				}
				if opts.HttpPort != 9090 {
					t.Errorf("expected HttpPort 9090, got %d", opts.HttpPort)
				}
			},
		},
		{
			name:           "VectorRendering",
			args:           []string{"--render", "--format", "vector", "--vector-format", "svg", "--grid-spacing", "500"},
			expectedCalled: "RunRender",
			verifyOpts: func(t *testing.T, opts AppOptions) {
				if opts.RenderFormat != "vector" {
					t.Errorf("expected RenderFormat vector, got %s", opts.RenderFormat)
				}
				if opts.VectorFormat != "svg" {
					t.Errorf("expected VectorFormat svg, got %s", opts.VectorFormat)
				}
				if opts.GridSpacing != 500 {
					t.Errorf("expected GridSpacing 500, got %f", opts.GridSpacing)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newMockApp()
			var out bytes.Buffer
			err := run(tt.args, &out, app)
			if err != nil {
				t.Fatalf("run failed: %v", err)
			}

			if !app.called[tt.expectedCalled] {
				t.Errorf("expected %s to be called", tt.expectedCalled)
			}

			if tt.verifyOpts != nil {
				tt.verifyOpts(t, app.opts)
			}
		})
	}
}

func TestRun_Help(t *testing.T) {
	app := newMockApp()
	var out bytes.Buffer
	err := run([]string{"--help"}, &out, app)
	if err == nil {
		t.Error("expected error from --help, got nil")
	}
	if !strings.Contains(out.String(), "Usage of tudomesh") {
		t.Errorf("expected usage info in output, got: %s", out.String())
	}
}

func TestRun_Default(t *testing.T) {
	app := newMockApp()
	var out bytes.Buffer
	err := run([]string{}, &out, app)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	expectedPrefix := "tudomesh version: " + Version
	if !strings.Contains(out.String(), expectedPrefix) {
		t.Errorf("expected output to contain version, got: %s", out.String())
	}

	if !strings.Contains(out.String(), "tudomesh service starting...") {
		t.Errorf("expected output to contain service starting message, got: %s", out.String())
	}
}

func TestMain_Execute(t *testing.T) {
	// Smoke test to ensure version is set
	if Version == "" {
		t.Error("expected Version to be set")
	}
}
