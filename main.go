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
	ConfigFile         string
	ParseOnly          bool
	CalibrateOnly      bool
	RenderOnly         bool
	RenderIndividual   bool
	IndividualRotation string
	CompareRotation    string
	ForceRotation      string
	ReferenceVacuum    string
	RotateAll          float64
	OutputFile         string
	DataDir            string
	DetectRotation     bool
	CalibrationCache   string
	MqttMode           bool
	HttpMode           bool
	HttpPort           int
	RenderFormat       string
	VectorFormat       string
	GridSpacing        float64
}

// MainApp defines the interface for the application logic
type MainApp interface {
	ApplyOptions(opts AppOptions)
	RunParseOnly()
	RunCalibration()
	RunRender()
	RunRenderIndividual(string)
	RunCompareRotation(string)
	RunDetectRotation()
	RunService()
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
	fs.BoolVar(&opts.ParseOnly, "parse-only", false, "Parse JSON exports and exit (test mode)")
	fs.BoolVar(&opts.CalibrateOnly, "calibrate", false, "Run calibration on JSON exports and exit (test mode)")
	fs.BoolVar(&opts.RenderOnly, "render", false, "Render composite map PNG and exit")
	fs.BoolVar(&opts.RenderIndividual, "render-individual", false, "Render each vacuum map as separate PNG")
	fs.StringVar(&opts.IndividualRotation, "individual-rotation", "", "Rotation for individual renders: VACUUM_ID=DEGREES")
	fs.StringVar(&opts.CompareRotation, "compare-rotation", "", "Render 4 rotation options for specified vacuum ID")
	fs.StringVar(&opts.ForceRotation, "force-rotation", "", "Force rotation for vacuum: VACUUM_ID=DEGREES (e.g., FrugalLameLion=180)")
	fs.StringVar(&opts.ReferenceVacuum, "reference", "", "Override reference vacuum (default: from config or largest area)")
	fs.Float64Var(&opts.RotateAll, "rotate-all", 0, "Rotate entire composite by degrees (0, 90, 180, 270)")
	fs.StringVar(&opts.OutputFile, "output", "composite-map.png", "Output file for --render mode")
	fs.StringVar(&opts.DataDir, "data-dir", ".", "Directory containing JSON exports for parse-only mode")
	fs.BoolVar(&opts.DetectRotation, "detect-rotation", false, "Analyze wall angles to detect rotation differences")
	fs.StringVar(&opts.CalibrationCache, "calibration-cache", ".calibration-cache.json", "Path to calibration cache file")
	fs.BoolVar(&opts.MqttMode, "mqtt", false, "Run MQTT service mode for real-time position tracking")
	fs.BoolVar(&opts.HttpMode, "http", false, "Enable HTTP server for serving map images")
	fs.IntVar(&opts.HttpPort, "http-port", 8080, "HTTP server port (default 8080)")
	fs.StringVar(&opts.RenderFormat, "format", "raster", "Render format: raster, vector, or both")
	fs.StringVar(&opts.VectorFormat, "vector-format", "svg", "Vector output format: svg or png")
	fs.Float64Var(&opts.GridSpacing, "grid-spacing", 1000.0, "Grid line spacing in millimeters (default 1000mm = 1m)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "tudomesh version: %s\n", Version)

	app.ApplyOptions(opts)

	if opts.ParseOnly {
		app.RunParseOnly()
		return nil
	}

	if opts.CalibrateOnly {
		app.RunCalibration()
		return nil
	}

	if opts.RenderOnly {
		app.RunRender()
		return nil
	}

	if opts.RenderIndividual {
		app.RunRenderIndividual(opts.IndividualRotation)
		return nil
	}

	if opts.CompareRotation != "" {
		app.RunCompareRotation(opts.CompareRotation)
		return nil
	}

	if opts.DetectRotation {
		app.RunDetectRotation()
		return nil
	}

	if opts.MqttMode || opts.HttpMode {
		app.RunService()
		return nil
	}

	// Normal service mode - to be implemented
	_, _ = fmt.Fprintln(out, "tudomesh service starting...")
	_, _ = fmt.Fprintln(out, "Use --parse-only to test JSON parsing")
	_, _ = fmt.Fprintln(out, "Use --calibrate to test ICP calibration")
	_, _ = fmt.Fprintln(out, "Use --render to output composite map PNG")
	_, _ = fmt.Fprintln(out, "Use --compare-rotation=VACUUM_ID to compare rotation options")
	_, _ = fmt.Fprintln(out, "Use --detect-rotation to analyze wall angles")
	_, _ = fmt.Fprintln(out, "Use --mqtt to run MQTT service mode")
	_, _ = fmt.Fprintln(out, "Use --http to run HTTP server mode")
	_, _ = fmt.Fprintln(out, "Use --mqtt --http to run both MQTT and HTTP together")
	_, _ = fmt.Fprintln(out, "\nConfiguration:")
	_, _ = fmt.Fprintln(out, "  config.yaml - MQTT settings and calibration overrides")
	_, _ = fmt.Fprintln(out, "  .calibration-cache.json - Auto-computed ICP transforms (cached)")
	_, _ = fmt.Fprintln(out, "\nFull service mode not yet implemented")

	return nil
}
