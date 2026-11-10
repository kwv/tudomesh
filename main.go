package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// Version is set at build time via -ldflags
var Version = "dev"

// AppOptions holds the configuration parsed from CLI flags
type AppOptions struct {
	ConfigFile       string
	CalibrateOnly    bool
	CompareRotation  string
	ReferenceVacuum  string
	RotateAll        float64
	DataDir          string
	CalibrationCache string
	MqttMode         bool
	HttpMode         bool
	HttpPort         int
	PlanRooms        bool
}

// MainApp defines the interface for the application logic
type MainApp interface {
	ApplyOptions(opts AppOptions)
	RunCalibration()
	RunCompareRotation(string)
	RunService()
	RunPlanRooms()
}

func main() {
	app := NewApp()
	if err := run(os.Args[1:], os.Stdout, app); err != nil {
		if err != flag.ErrHelp {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

func run(args []string, out io.Writer, app MainApp) error {
	fs := flag.NewFlagSet("tudomesh", flag.ContinueOnError)
	fs.SetOutput(out)

	opts := AppOptions{}
	fs.StringVar(&opts.ConfigFile, "config", "config.yaml", "Path to configuration file")
	fs.BoolVar(&opts.CalibrateOnly, "calibrate", false, "Run calibration on JSON exports and exit (test mode)")
	fs.StringVar(&opts.CompareRotation, "compare-rotation", "", "Render 4 rotation options for specified vacuum ID")
	fs.StringVar(&opts.ReferenceVacuum, "reference", "", "Override reference vacuum (default: from config or largest area)")
	fs.Float64Var(&opts.RotateAll, "rotate-all", 0, "Rotate entire composite by degrees (0, 90, 180, 270)")
	fs.StringVar(&opts.DataDir, "data-dir", ".", "Directory for config.yaml, map exports and caches")
	fs.StringVar(&opts.CalibrationCache, "calibration-cache", ".calibration-cache.json", "Path to calibration cache file")
	fs.BoolVar(&opts.MqttMode, "mqtt", false, "Run MQTT service mode for real-time position tracking")
	fs.BoolVar(&opts.HttpMode, "http", false, "Enable HTTP server for serving map images")
	fs.IntVar(&opts.HttpPort, "http-port", 8080, "HTTP server port (default 8080)")
	fs.BoolVar(&opts.PlanRooms, "plan-rooms", false, "Plan room edits that match each robot's rooms to the reference robot's; writes previews, changes nothing")

	if err := fs.Parse(args); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "tudomesh version: %s\n", Version)

	app.ApplyOptions(opts)

	if opts.CalibrateOnly {
		app.RunCalibration()
		return nil
	}

	if opts.PlanRooms {
		app.RunPlanRooms()
		return nil
	}

	if opts.CompareRotation != "" {
		app.RunCompareRotation(opts.CompareRotation)
		return nil
	}

	if opts.MqttMode || opts.HttpMode {
		app.RunService()
		return nil
	}

	_, _ = fmt.Fprintln(out, "Usage:")
	_, _ = fmt.Fprintln(out, "  --mqtt --http          run the service (live positions and HTTP maps)")
	_, _ = fmt.Fprintln(out, "  --calibrate            align the map exports in --data-dir and write the calibration cache")
	_, _ = fmt.Fprintln(out, "  --compare-rotation=ID  render the 4 rotations of one robot, to pick a rotation hint")
	_, _ = fmt.Fprintln(out, "  --plan-rooms           preview room edits that match each robot's rooms to the reference's")
	_, _ = fmt.Fprintln(out, "Run with -h for every flag.")

	return nil
}
