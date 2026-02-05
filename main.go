package main

import (
	"flag"
	"fmt"
)

// Version is set at build time via -ldflags
var Version = "dev"

var (
	configFile         = flag.String("config", "config.yaml", "Path to configuration file")
	parseOnly          = flag.Bool("parse-only", false, "Parse JSON exports and exit (test mode)")
	calibrateOnly      = flag.Bool("calibrate", false, "Run calibration on JSON exports and exit (test mode)")
	renderOnly         = flag.Bool("render", false, "Render composite map PNG and exit")
	renderIndividual   = flag.Bool("render-individual", false, "Render each vacuum map as separate PNG")
	individualRotation = flag.String("individual-rotation", "", "Rotation for individual renders: VACUUM_ID=DEGREES")
	compareRotation    = flag.String("compare-rotation", "", "Render 4 rotation options for specified vacuum ID")
	forceRotation      = flag.String("force-rotation", "", "Force rotation for vacuum: VACUUM_ID=DEGREES (e.g., FrugalLameLion=180)")
	referenceVacuum    = flag.String("reference", "", "Override reference vacuum (default: from config or largest area)")
	rotateAll          = flag.Float64("rotate-all", 0, "Rotate entire composite by degrees (0, 90, 180, 270)")
	outputFile         = flag.String("output", "composite-map.png", "Output file for --render mode")
	dataDir            = flag.String("data-dir", ".", "Directory containing JSON exports for parse-only mode")
	detectRotation     = flag.Bool("detect-rotation", false, "Analyze wall angles to detect rotation differences")
	calibrationCache   = flag.String("calibration-cache", ".calibration-cache.json", "Path to calibration cache file")
	mqttMode           = flag.Bool("mqtt", false, "Run MQTT service mode for real-time position tracking")
	httpMode           = flag.Bool("http", false, "Enable HTTP server for serving map images")
	httpPort           = flag.Int("http-port", 8080, "HTTP server port (default 8080)")
	
	// Vector rendering flags
	renderFormat = flag.String("format", "raster", "Render format: raster, vector, or both")
	vectorFormat = flag.String("vector-format", "svg", "Vector output format: svg or png")
	gridSpacing  = flag.Float64("grid-spacing", 1000.0, "Grid line spacing in millimeters (default 1000mm = 1m)")
)

func main() {
	flag.Parse()
	fmt.Printf("tudomesh version: %s\n", Version)

	app := NewApp()
	app.DataDir = *dataDir
	app.ConfigFile = *configFile
	app.CalibrationCache = *calibrationCache
	app.RotateAll = *rotateAll
	app.ForceRotation = *forceRotation
	app.ReferenceVacuum = *referenceVacuum
	app.OutputFile = *outputFile
	app.RenderFormat = *renderFormat
	app.VectorFormat = *vectorFormat
	app.GridSpacing = *gridSpacing
	app.HttpPort = *httpPort
	app.MqttMode = *mqttMode
	app.HttpMode = *httpMode

	if *parseOnly {
		app.RunParseOnly()
		return
	}

	if *calibrateOnly {
		app.RunCalibration()
		return
	}

	if *renderOnly {
		app.RunRender()
		return
	}

	if *renderIndividual {
		app.RunRenderIndividual(*individualRotation)
		return
	}

	if *compareRotation != "" {
		app.RunCompareRotation(*compareRotation)
		return
	}

	if *detectRotation {
		app.RunDetectRotation()
		return
	}

	if *mqttMode || *httpMode {
		app.RunService()
		return
	}

	// Normal service mode - to be implemented
	fmt.Println("tudomesh service starting...")
	fmt.Println("Use --parse-only to test JSON parsing")
	fmt.Println("Use --calibrate to test ICP calibration")
	fmt.Println("Use --render to output composite map PNG")
	fmt.Println("Use --compare-rotation=VACUUM_ID to compare rotation options")
	fmt.Println("Use --detect-rotation to analyze wall angles")
	fmt.Println("Use --mqtt to run MQTT service mode")
	fmt.Println("Use --http to run HTTP server mode")
	fmt.Println("Use --mqtt --http to run both MQTT and HTTP together")
	fmt.Println("\nConfiguration:")
	fmt.Println("  config.yaml - MQTT settings and calibration overrides")
	fmt.Println("  .calibration-cache.json - Auto-computed ICP transforms (cached)")
	fmt.Println("\nFull service mode not yet implemented")
}
