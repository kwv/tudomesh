package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMQTTServiceStartupShutdown tests the full MQTT service lifecycle
func TestMQTTServiceStartupShutdown(t *testing.T) {
	// Skip if not running integration tests
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test (set RUN_INTEGRATION_TESTS=1 to run)")
	}

	// Create temporary directory for test files
	tmpDir := t.TempDir()

	// Create test config
	configYAML := `mqtt:
  broker: "mqtt://localhost:1883"
  publishPrefix: "tudomesh-test"
  clientId: "tudomesh-test"

reference: TestVacuum1

vacuums:
  - id: TestVacuum1
    topic: "test/vacuum1/map"
    color: "#FF0000"
  - id: TestVacuum2
    topic: "test/vacuum2/map"
    color: "#00FF00"
`

	configPath := filepath.Join(tmpDir, "test-config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Build the binary
	binaryPath := filepath.Join(tmpDir, "tudomesh-test")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\n%s", err, output)
	}

	tests := []struct {
		name           string
		args           []string
		expectInOutput []string
		timeout        time.Duration
	}{
		{
			name: "successful startup with config",
			args: []string{"--mqtt", "--config=" + configPath},
			expectInOutput: []string{
				"Starting tudomesh MQTT service",
				"Loaded config from",
				"Reference vacuum:",
				"MQTT Service Running",
				"Subscribed topics:",
				"test/vacuum1/map",
				"test/vacuum2/map",
				"Press Ctrl+C to stop",
			},
			timeout: 5 * time.Second,
		},
		{
			name: "missing config file",
			args: []string{"--mqtt", "--config=nonexistent.yaml"},
			expectInOutput: []string{
				"Starting tudomesh MQTT service",
				"Failed to load config",
			},
			timeout: 2 * time.Second,
		},
		{
			name: "with calibration cache warning",
			args: []string{"--mqtt", "--config=" + configPath, "--calibration-cache=nonexistent-cache.json"},
			expectInOutput: []string{
				"Starting tudomesh MQTT service",
				"Warning: No calibration cache found",
			},
			timeout: 5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			// Start the service
			cmd := exec.CommandContext(ctx, binaryPath, tt.args...)
			output, err := cmd.CombinedOutput()

			// Convert output to string
			outputStr := string(output)

			// Check expected output strings
			for _, expected := range tt.expectInOutput {
				if !strings.Contains(outputStr, expected) {
					t.Errorf("Expected output to contain '%s', but it didn't.\nFull output:\n%s",
						expected, outputStr)
				}
			}

			// For successful startup test, verify graceful shutdown message
			if tt.name == "successful startup with config" {
				if !strings.Contains(outputStr, "Connecting to MQTT broker") {
					t.Errorf("Expected MQTT connection attempt.\nFull output:\n%s", outputStr)
				}
			}

			// For error cases, verify the process exits
			if strings.Contains(tt.name, "missing") || strings.Contains(tt.name, "invalid") {
				if err == nil {
					t.Error("Expected command to fail, but it succeeded")
				}
			}
		})
	}
}

// TestMQTTServiceSignalHandling tests SIGINT/SIGTERM handling
func TestMQTTServiceSignalHandling(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test (set RUN_INTEGRATION_TESTS=1 to run)")
	}

	// Create temporary config
	tmpDir := t.TempDir()
	configYAML := `mqtt:
  broker: "mqtt://localhost:1883"
  publishPrefix: "tudomesh-test"

vacuums:
  - id: TestVacuum
    topic: "test/vacuum/map"
    color: "#FF0000"
`

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}

	// Build binary
	binaryPath := filepath.Join(tmpDir, "tudomesh-test")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\n%s", err, output)
	}

	// Start service
	cmd := exec.Command(binaryPath, "--mqtt", "--config="+configPath)

	// Give it time to start
	time.Sleep(2 * time.Second)

	// Send SIGINT
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Logf("Failed to send SIGINT (process may have already exited): %v", err)
	}

	// Wait for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		t.Log("Service shut down gracefully")
	case <-time.After(5 * time.Second):
		t.Error("Service did not shut down within timeout")
		if err := cmd.Process.Kill(); err != nil {
			t.Logf("Failed to kill process: %v", err)
		}
	}
}

// TestMQTTServiceHelpFlag tests the --help output includes mqtt flag
func TestMQTTServiceHelpFlag(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// --help exits with status 0 or 2, depending on flag package
		if !strings.Contains(err.Error(), "exit status") {
			t.Fatalf("Failed to run --help: %v", err)
		}
	}

	outputStr := string(output)

	// Verify mqtt flag is documented
	if !strings.Contains(outputStr, "-mqtt") {
		t.Error("Expected --help output to contain -mqtt flag")
	}
	if !strings.Contains(outputStr, "MQTT service mode") {
		t.Error("Expected --help output to describe MQTT service mode")
	}
}
