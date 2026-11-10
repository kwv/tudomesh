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
func (m *mockApp) RunCalibration()              { m.called["RunCalibration"] = true }
func (m *mockApp) RunCompareRotation(s string)  { m.called["RunCompareRotation"] = true; m.sArg = s }
func (m *mockApp) RunService()                  { m.called["RunService"] = true }
func (m *mockApp) RunPlanRooms()                { m.called["RunPlanRooms"] = true }

func TestRun_Flags(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedCalled string
		verifyOpts     func(*testing.T, AppOptions)
	}{

		{
			name:           "Calibrate",
			args:           []string{"--calibrate", "--calibration-cache", "test.json", "--data-dir", "/tmp/data", "--rotate-all", "90"},
			expectedCalled: "RunCalibration",
			verifyOpts: func(t *testing.T, opts AppOptions) {
				if opts.CalibrationCache != "test.json" {
					t.Errorf("expected CalibrationCache test.json, got %s", opts.CalibrationCache)
				}
				if !opts.CalibrateOnly {
					t.Error("expected CalibrateOnly true")
				}
				if opts.DataDir != "/tmp/data" || opts.RotateAll != 90 {
					t.Errorf("DataDir, RotateAll = %s, %g", opts.DataDir, opts.RotateAll)
				}
			},
		},

		{
			name:           "PlanRooms",
			args:           []string{"--plan-rooms"},
			expectedCalled: "RunPlanRooms",
			verifyOpts: func(t *testing.T, opts AppOptions) {
				if !opts.PlanRooms {
					t.Error("expected PlanRooms true")
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

	if !strings.Contains(out.String(), "--mqtt --http") {
		t.Errorf("expected usage hints, got: %s", out.String())
	}
}

func TestMain_Execute(t *testing.T) {
	// Smoke test to ensure version is set
	if Version == "" {
		t.Error("expected Version to be set")
	}
}
