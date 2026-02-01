package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image/color"
	"image/png"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kwv/tudomesh/mesh"
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

	if *parseOnly {
		runParseOnly()
		return
	}

	if *calibrateOnly {
		runCalibration()
		return
	}

	if *renderOnly {
		runRender()
		return
	}

	if *renderIndividual {
		runRenderIndividual()
		return
	}

	if *compareRotation != "" {
		runCompareRotation(*compareRotation)
		return
	}

	if *detectRotation {
		runDetectRotation()
		return
	}

	if *mqttMode || *httpMode {
		runService()
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

// runParseOnly finds and parses all Valetudo JSON exports
func runParseOnly() {
	pattern := filepath.Join(*dataDir, "ValetudoMapExport-*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		log.Fatalf("Error finding JSON files: %v", err)
	}

	if len(files) == 0 {
		// Try current directory
		files, _ = filepath.Glob("ValetudoMapExport-*.json")
	}

	if len(files) == 0 {
		log.Fatal("No ValetudoMapExport-*.json files found")
	}

	fmt.Printf("Found %d map export(s)\n\n", len(files))

	for _, file := range files {
		parseAndPrint(file)
	}
}

func parseAndPrint(path string) {
	// Extract vacuum name from filename
	base := filepath.Base(path)
	name := strings.TrimPrefix(base, "ValetudoMapExport-")
	name = strings.Split(name, "-2")[0] // Remove timestamp

	fmt.Printf("=== %s ===\n", name)
	fmt.Printf("File: %s\n", path)

	m, err := mesh.ParseMapFile(path)
	if err != nil {
		fmt.Printf("ERROR: %v\n\n", err)
		return
	}

	summary := mesh.Summarize(m)

	fmt.Printf("Map Size: %dx%d (pixel size: %d)\n", summary.Size.X, summary.Size.Y, summary.PixelSize)
	fmt.Printf("Total Layer Area: %d\n", summary.TotalLayerArea)
	fmt.Printf("Robot Position: (%.0f, %.0f) angle: %.0f°\n",
		summary.RobotPosition.X, summary.RobotPosition.Y, summary.RobotAngle)
	fmt.Printf("Charger Position: (%.0f, %.0f)\n",
		summary.ChargerPosition.X, summary.ChargerPosition.Y)
	fmt.Printf("Segments: %d", summary.SegmentCount)
	if len(summary.SegmentNames) > 0 {
		fmt.Printf(" [%s]", strings.Join(summary.SegmentNames, ", "))
	}
	fmt.Println()
	fmt.Printf("Has Floor: %v, Has Wall: %v\n", summary.HasFloor, summary.HasWall)
	fmt.Println()
}

// runCompareRotation renders 4 images with different rotation options for a vacuum
func runCompareRotation(vacuumID string) {
	pattern := filepath.Join(*dataDir, "ValetudoMapExport-*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		log.Fatalf("Error finding JSON files: %v", err)
	}

	if len(files) == 0 {
		files, _ = filepath.Glob("ValetudoMapExport-*.json")
	}

	if len(files) == 0 {
		log.Fatal("No ValetudoMapExport-*.json files found")
	}

	// Load all maps
	maps := make(map[string]*mesh.ValetudoMap)
	for _, file := range files {
		base := filepath.Base(file)
		name := strings.TrimPrefix(base, "ValetudoMapExport-")
		name = strings.Split(name, "-2")[0]

		m, err := mesh.ParseMapFile(file)
		if err != nil {
			fmt.Printf("Error loading %s: %v\n", name, err)
			continue
		}
		maps[name] = m
	}

	// Check vacuum exists
	if _, ok := maps[vacuumID]; !ok {
		fmt.Printf("Vacuum '%s' not found. Available:\n", vacuumID)
		for id := range maps {
			fmt.Printf("  - %s\n", id)
		}
		return
	}

	fmt.Printf("Rendering rotation comparison for %s...\n", vacuumID)
	outputPrefix := fmt.Sprintf("rotation_%s", vacuumID)

	if err := mesh.RenderRotationComparison(maps, vacuumID, outputPrefix, *referenceVacuum, *rotateAll); err != nil {
		log.Fatalf("Error rendering: %v", err)
	}

	fmt.Printf("Created: %s_0.png, %s_90.png, %s_180.png, %s_270.png\n",
		outputPrefix, outputPrefix, outputPrefix, outputPrefix)
}

// runRender loads maps, aligns them, and outputs a composite PNG
func runRender() {
	pattern := filepath.Join(*dataDir, "ValetudoMapExport-*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		log.Fatalf("Error finding JSON files: %v", err)
	}

	if len(files) == 0 {
		files, _ = filepath.Glob("ValetudoMapExport-*.json")
	}

	if len(files) == 0 {
		log.Fatal("No ValetudoMapExport-*.json files found")
	}

	fmt.Printf("Found %d map export(s)\n", len(files))

	// Load all maps
	maps := make(map[string]*mesh.ValetudoMap)
	for _, file := range files {
		base := filepath.Base(file)
		name := strings.TrimPrefix(base, "ValetudoMapExport-")
		name = strings.Split(name, "-2")[0]

		m, err := mesh.ParseMapFile(file)
		if err != nil {
			fmt.Printf("Error loading %s: %v\n", name, err)
			continue
		}
		maps[name] = m
		fmt.Printf("Loaded: %s\n", name)
	}

	if len(maps) < 2 {
		log.Fatal("Need at least 2 maps for composite render")
	}

	// Load unified config (optional - provides rotation hints and manual overrides)
	var config *mesh.Config
	if _, err := os.Stat(*configFile); err == nil {
		config, err = mesh.LoadConfig(*configFile)
		if err != nil {
			log.Printf("Warning: Failed to load config file %s: %v", *configFile, err)
		} else {
			log.Printf("Loaded config from %s", *configFile)
		}
	}

	// Load calibration cache (auto-computed ICP transforms)
	var cache *mesh.CalibrationData
	cache, err = mesh.LoadCalibration(*calibrationCache)
	if err != nil {
		log.Printf("Warning: Failed to load calibration cache %s: %v", *calibrationCache, err)
	} else if cache != nil {
		log.Printf("Loaded calibration cache from %s", *calibrationCache)
	}

	// Determine effective reference
	effectiveRef := *referenceVacuum
	if effectiveRef == "" && config != nil {
		effectiveRef = mesh.GetEffectiveReference(config, cache, maps)
	}
	if effectiveRef == "" {
		effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
	}
	fmt.Printf("Reference vacuum: %s\n", effectiveRef)

	// Build transforms from cache, config, and CLI (priority: CLI > config > cache > ICP)
	transforms := make(map[string]mesh.AffineMatrix)
	transforms[effectiveRef] = mesh.Identity()
	needsRecalibration := false

	for id := range maps {
		if id == effectiveRef {
			continue
		}

		var transform mesh.AffineMatrix
		source := "ICP (auto-computed)"

		// Priority 1: Check cache
		if cache != nil && cache.ReferenceVacuum == effectiveRef {
			if cachedTransform, ok := cache.Vacuums[id]; ok {
				transform = cachedTransform
				source = "cache"
			}
		}

		// Priority 2: Check config rotation hints (run ICP with hint as starting point)
		if config != nil {
			vc := config.GetVacuumByID(id)
			if vc != nil && vc.Rotation != nil {
				rotHint := *vc.Rotation
				fmt.Printf("  %s: re-running ICP with rotation hint %.0f° from config\n", id, rotHint)
				icpConfig := mesh.DefaultICPConfig()
				result := mesh.AlignMapsWithRotationHint(maps[id], maps[effectiveRef], icpConfig, rotHint)
				transform = result.Transform
				source = fmt.Sprintf("ICP+hint(%.0f°)", rotHint)
				needsRecalibration = true

				// Apply manual translation if provided
				if vc.Translation != nil && (vc.Translation.X != 0 || vc.Translation.Y != 0) {
					transform = mesh.MultiplyMatrices(mesh.Translation(vc.Translation.X, vc.Translation.Y), transform)
					source += fmt.Sprintf("+translation(%.0f,%.0f)", vc.Translation.X, vc.Translation.Y)
				}
			}
		}

		// Priority 3: CLI --force-rotation override (highest priority)
		if *forceRotation != "" {
			cliRotations := mesh.BuildForceRotationMap(*forceRotation)
			if rotDeg, ok := cliRotations[id]; ok {
				fmt.Printf("  %s: CLI override rotation %.0f° (running ICP with hint)\n", id, rotDeg)
				icpConfig := mesh.DefaultICPConfig()
				result := mesh.AlignMapsWithRotationHint(maps[id], maps[effectiveRef], icpConfig, rotDeg)
				transform = result.Transform
				source = fmt.Sprintf("CLI+ICP(%.0f°)", rotDeg)
				needsRecalibration = true
			}
		}

		// If no transform found, run full ICP
		if transform.A == 0 && transform.D == 0 {
			fmt.Printf("  %s: running full ICP alignment (not in cache)\n", id)
			icpConfig := mesh.DefaultICPConfig()
			result := mesh.AlignMaps(maps[id], maps[effectiveRef], icpConfig)
			transform = result.Transform
			source = "ICP (auto-computed)"
			needsRecalibration = true
		}

		transforms[id] = transform
		if source == "cache" {
			totalRotation := math.Atan2(transform.C, transform.A) * 180 / math.Pi
			if totalRotation < 0 {
				totalRotation += 360
			}
			fmt.Printf("  %s: using cached transform (rotation %.1f°)\n", id, totalRotation)
		}
	}

	// Update cache if any transforms were recomputed
	if needsRecalibration {
		fmt.Printf("\nUpdating calibration cache with new transforms...\n")
		newCache := mesh.CalibrationData{
			ReferenceVacuum: effectiveRef,
			Vacuums:         transforms,
		}
		if err := mesh.SaveCalibration(*calibrationCache, &newCache); err != nil {
			log.Printf("Warning: Failed to save calibration cache: %v", err)
		} else {
			fmt.Printf("Calibration cache updated: %s\n", *calibrationCache)
		}
	}

	// Render with computed transforms
	fmt.Printf("\nRendering composite map to %s...\n", *outputFile)

	// Determine render format
	format := *renderFormat
	if format != "raster" && format != "vector" && format != "both" {
		log.Fatalf("Invalid format: %s (must be raster, vector, or both)", format)
	}

	// Raster rendering (existing behavior)
	if format == "raster" || format == "both" {
		renderer := mesh.NewCompositeRenderer(maps, transforms, effectiveRef)
		renderer.GlobalRotation = *rotateAll
		applyConfigColors(renderer, config)

		outputPath := *outputFile
		if format == "both" && !strings.HasSuffix(outputPath, ".png") {
			outputPath = strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".png"
		}

		if err := renderer.SavePNG(outputPath); err != nil {
			log.Fatalf("Error rendering raster: %v", err)
		}
		fmt.Printf("Created raster: %s\n", outputPath)
	}

	// Vector rendering
	if format == "vector" || format == "both" {
		vectorRenderer := mesh.NewVectorRenderer(maps, transforms, effectiveRef)
		vectorRenderer.GlobalRotation = *rotateAll

		// Apply grid spacing from config or flag
		if config != nil && config.GridSpacing > 0 {
			vectorRenderer.Padding = config.GridSpacing
		} else if *gridSpacing > 0 {
			vectorRenderer.Padding = *gridSpacing / 2 // Padding is half the grid spacing
		}

		outputPath := *outputFile
		if format == "both" {
			// When rendering both, change extension to .svg for vector
			outputPath = strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".svg"
		}

		// Create output file
		outFile, err := os.Create(outputPath)
		if err != nil {
			log.Fatalf("Error creating output file %s: %v", outputPath, err)
		}
		defer func() {
			if err := outFile.Close(); err != nil {
				log.Printf("Warning: error closing output file %s: %v", outputPath, err)
			}
		}()

		// Render based on vector format
		if *vectorFormat == "svg" {
			if err := vectorRenderer.RenderToSVG(outFile); err != nil {
				log.Fatalf("Error rendering vector SVG: %v", err)
			}
			fmt.Printf("Created vector SVG: %s\n", outputPath)
		} else {
			log.Fatalf("PNG vector format not yet implemented (use --vector-format=svg)")
		}
	}

	fmt.Println("Done!")
}

// runRenderIndividual renders each vacuum map as a separate PNG
func runRenderIndividual() {
	pattern := filepath.Join(*dataDir, "ValetudoMapExport-*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		log.Fatalf("Error finding JSON files: %v", err)
	}

	if len(files) == 0 {
		files, _ = filepath.Glob("ValetudoMapExport-*.json")
	}

	if len(files) == 0 {
		log.Fatal("No ValetudoMapExport-*.json files found")
	}

	fmt.Printf("Found %d map export(s)\n", len(files))

	// Parse individual rotations
	individualRotations := make(map[string]float64)
	if *individualRotation != "" {
		for _, spec := range strings.Split(*individualRotation, ",") {
			parts := strings.Split(strings.TrimSpace(spec), "=")
			if len(parts) == 2 {
				var rotDeg float64
				if _, err := fmt.Sscanf(parts[1], "%f", &rotDeg); err == nil {
					individualRotations[parts[0]] = rotDeg
				}
			}
		}
	}

	// Define colors for each vacuum
	colors := []struct {
		name  string
		floor color.RGBA
		wall  color.RGBA
	}{
		{"blue", color.RGBA{100, 149, 237, 255}, color.RGBA{0, 0, 139, 255}},
		{"red", color.RGBA{255, 99, 71, 255}, color.RGBA{139, 0, 0, 255}},
		{"green", color.RGBA{144, 238, 144, 255}, color.RGBA{0, 100, 0, 255}},
	}

	for i, file := range files {
		base := filepath.Base(file)
		name := strings.TrimPrefix(base, "ValetudoMapExport-")
		name = strings.Split(name, "-2")[0]

		m, err := mesh.ParseMapFile(file)
		if err != nil {
			fmt.Printf("Error loading %s: %v\n", name, err)
			continue
		}

		colorIdx := i % len(colors)
		rotDeg := individualRotations[name] // defaults to 0 if not specified
		outputPath := fmt.Sprintf("individual_%s_%s_rot%d.png", name, colors[colorIdx].name, int(rotDeg))

		fmt.Printf("Rendering %s as %s (rot %.0f°) -> %s\n", name, colors[colorIdx].name, rotDeg, outputPath)

		if err := mesh.RenderSingleMapWithRotation(m, outputPath, colors[colorIdx].floor, colors[colorIdx].wall, rotDeg); err != nil {
			fmt.Printf("Error rendering %s: %v\n", name, err)
		}
	}

	fmt.Println("Done!")
}

// runCalibration loads all JSON exports and runs ICP calibration
func runCalibration() {
	pattern := filepath.Join(*dataDir, "ValetudoMapExport-*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		log.Fatalf("Error finding JSON files: %v", err)
	}

	if len(files) == 0 {
		files, _ = filepath.Glob("ValetudoMapExport-*.json")
	}

	if len(files) == 0 {
		log.Fatal("No ValetudoMapExport-*.json files found")
	}

	fmt.Printf("Found %d map export(s)\n\n", len(files))

	// Load all maps
	maps := make(map[string]*mesh.ValetudoMap)
	for _, file := range files {
		base := filepath.Base(file)
		name := strings.TrimPrefix(base, "ValetudoMapExport-")
		name = strings.Split(name, "-2")[0] // Remove timestamp

		m, err := mesh.ParseMapFile(file)
		if err != nil {
			fmt.Printf("Error loading %s: %v\n", name, err)
			continue
		}
		maps[name] = m
		fmt.Printf("Loaded: %s (area: %d)\n", name, m.MetaData.TotalLayerArea)
	}

	if len(maps) < 2 {
		log.Fatal("Need at least 2 maps for calibration")
	}

	// Select reference vacuum (largest area)
	refID := mesh.SelectReferenceVacuum(maps, nil)
	fmt.Printf("\nReference vacuum: %s (auto-selected by largest area)\n\n", refID)

	refMap := maps[refID]

	// Run ICP alignment for each non-reference vacuum
	fmt.Println("Running ICP alignment...")
	fmt.Println(strings.Repeat("-", 60))

	for id, m := range maps {
		if id == refID {
			fmt.Printf("%-25s: [REFERENCE - identity transform]\n", id)
			continue
		}

		// Extract features for comparison
		srcFeatures := mesh.ExtractFeatures(m)
		tgtFeatures := mesh.ExtractFeatures(refMap)

		fmt.Printf("%-25s:\n", id)
		fmt.Printf("  Source: %d walls, %d grid, %d boundary, %d corners, charger=%v\n",
			len(srcFeatures.WallPoints), len(srcFeatures.GridPoints),
			len(srcFeatures.BoundaryPoints), len(srcFeatures.Corners), srcFeatures.HasCharger)
		fmt.Printf("  Target: %d walls, %d grid, %d boundary, %d corners, charger=%v\n",
			len(tgtFeatures.WallPoints), len(tgtFeatures.GridPoints),
			len(tgtFeatures.BoundaryPoints), len(tgtFeatures.Corners), tgtFeatures.HasCharger)

		// Run ICP
		config := mesh.DefaultICPConfig()
		result := mesh.AlignMaps(m, refMap, config)

		valid := mesh.ValidateAlignment(result.Transform)

		// Calculate total rotation angle from transform matrix
		totalRotation := math.Atan2(result.Transform.C, result.Transform.A) * 180 / math.Pi
		if totalRotation < 0 {
			totalRotation += 360
		}

		fmt.Printf("  ICP result: %d iterations, error=%.2f, score=%.4f, inliers=%.1f%%, converged=%v, valid=%v\n",
			result.Iterations, result.Error, result.Score, result.InlierFraction*100, result.Converged, valid)
		fmt.Printf("  Rotation errors: 0°=%.1f, 90°=%.1f, 180°=%.1f, 270°=%.1f\n",
			mesh.RotationErrors[0], mesh.RotationErrors[90],
			mesh.RotationErrors[180], mesh.RotationErrors[270])
		fmt.Printf("  Initial rotation: %.0f°, Final rotation: %.1f°\n", result.InitialRotation, totalRotation)
		fmt.Printf("  Translation: (%.1f, %.1f)\n", result.Transform.Tx, result.Transform.Ty)

		// Show transformed positions
		srcPos, srcAngle, _ := mesh.ExtractRobotPosition(m)
		srcCharger, _ := mesh.ExtractChargerPosition(m)

		worldPos := mesh.TransformPoint(srcPos, result.Transform)
		worldCharger := mesh.TransformPoint(srcCharger, result.Transform)

		// Adjust robot angle by the transform rotation
		worldAngle := srcAngle + totalRotation
		for worldAngle >= 360 {
			worldAngle -= 360
		}
		for worldAngle < 0 {
			worldAngle += 360
		}

		fmt.Printf("  Robot: local(%.0f,%.0f) -> world(%.0f,%.0f)\n",
			srcPos.X, srcPos.Y, worldPos.X, worldPos.Y)
		fmt.Printf("  Robot angle: local=%.0f° -> world=%.0f°\n", srcAngle, worldAngle)
		fmt.Printf("  Charger: local(%.0f,%.0f) -> world(%.0f,%.0f)\n",
			srcCharger.X, srcCharger.Y, worldCharger.X, worldCharger.Y)
		fmt.Println()
	}

	// Show reference vacuum positions
	refPos, refAngle, _ := mesh.ExtractRobotPosition(refMap)
	refCharger, _ := mesh.ExtractChargerPosition(refMap)
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("Reference (%s) positions (world coordinates):\n", refID)
	fmt.Printf("  Robot: (%.0f, %.0f) angle=%.0f°\n", refPos.X, refPos.Y, refAngle)
	fmt.Printf("  Charger: (%.0f, %.0f)\n", refCharger.X, refCharger.Y)

	// Save calibration cache
	cache := mesh.CalibrationData{
		ReferenceVacuum: refID,
		Vacuums:         make(map[string]mesh.AffineMatrix),
	}
	cache.Vacuums[refID] = mesh.Identity()

	// Re-run alignment to get transforms for cache
	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("Building calibration cache...")
	for id, m := range maps {
		if id == refID {
			continue
		}
		config := mesh.DefaultICPConfig()
		result := mesh.AlignMaps(m, refMap, config)
		cache.Vacuums[id] = result.Transform
		fmt.Printf("  %s: cached transform (rotation %.1f°)\n", id, math.Atan2(result.Transform.C, result.Transform.A)*180/math.Pi)
	}

	// Save to cache file
	fmt.Printf("\nSaving calibration cache to %s\n", *calibrationCache)
	if err := mesh.SaveCalibration(*calibrationCache, &cache); err != nil {
		log.Printf("Warning: Failed to save calibration cache: %v", err)
	} else {
		fmt.Println("Calibration cache saved successfully")
	}
}

// runDetectRotation analyzes wall angles to detect rotation differences between maps
func runDetectRotation() {
	pattern := filepath.Join(*dataDir, "ValetudoMapExport-*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		log.Fatalf("Error finding JSON files: %v", err)
	}

	if len(files) == 0 {
		files, _ = filepath.Glob("ValetudoMapExport-*.json")
	}

	if len(files) == 0 {
		log.Fatal("No ValetudoMapExport-*.json files found")
	}

	fmt.Printf("Found %d map export(s)\n\n", len(files))

	// Load all maps
	maps := make(map[string]*mesh.ValetudoMap)
	for _, file := range files {
		base := filepath.Base(file)
		name := strings.TrimPrefix(base, "ValetudoMapExport-")
		name = strings.Split(name, "-2")[0]

		m, err := mesh.ParseMapFile(file)
		if err != nil {
			fmt.Printf("Error loading %s: %v\n", name, err)
			continue
		}
		maps[name] = m
	}

	if len(maps) < 2 {
		log.Fatal("Need at least 2 maps for rotation detection")
	}

	// Select reference vacuum
	refID := *referenceVacuum
	if refID == "" {
		refID = mesh.SelectReferenceVacuum(maps, nil)
	}
	refMap := maps[refID]

	fmt.Printf("Reference vacuum: %s\n", refID)
	fmt.Println(strings.Repeat("=", 70))

	// Analyze wall angles for reference
	refHist := mesh.ExtractWallAngles(refMap)
	refDominant := refHist.DominantAngles(4)
	fmt.Printf("\n%s (reference) dominant wall angles: ", refID)
	for i, a := range refDominant {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Printf("%.0f°", a)
	}
	fmt.Printf(" (%d edges analyzed)\n", refHist.TotalEdges)

	// Analyze each other vacuum
	fmt.Println(strings.Repeat("-", 70))
	for id, m := range maps {
		if id == refID {
			continue
		}

		fmt.Printf("\n%s:\n", id)

		// Get wall angle histogram
		hist := mesh.ExtractWallAngles(m)
		dominant := hist.DominantAngles(4)
		fmt.Printf("  Dominant wall angles: ")
		for i, a := range dominant {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%.0f°", a)
		}
		fmt.Printf(" (%d edges)\n", hist.TotalEdges)

		// Run rotation detection with detailed output
		analysis := mesh.DetectRotationWithFeaturesDebug(m, refMap)

		fmt.Printf("  Rotation scores:\n")
		for _, rot := range []float64{0, 90, 180, 270} {
			marker := ""
			if rot == analysis.BestRotation {
				marker = " <-- BEST"
			}
			fmt.Printf("    %3.0f°: %.4f%s\n", rot, analysis.Scores[rot], marker)
		}
		fmt.Printf("  Detected rotation: %.0f° (confidence: %.1f%%)\n",
			analysis.BestRotation, analysis.Confidence*100)
	}

	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("\nTo apply detected rotations, use:")
	fmt.Print("  --force-rotation=\"")
	first := true
	for id, m := range maps {
		if id == refID {
			continue
		}
		analysis := mesh.DetectRotationWithFeatures(m, refMap)
		if !first {
			fmt.Print(",")
		}
		fmt.Printf("%s=%.0f", id, analysis.BestRotation)
		first = false
	}
	fmt.Println("\"")
}

// runService starts the combined MQTT and/or HTTP service
func runService() {
	fmt.Println("Starting tudomesh service...")

	// 1. Resolve configuration paths relative to data-dir if provided
	resolvedConfig := *configFile
	resolvedCache := *calibrationCache

	// If data-dir is specified and files are still pointing to defaults,
	// resolve them relative to the data-dir.
	if *dataDir != "." {
		if resolvedConfig == "config.yaml" {
			resolvedConfig = filepath.Join(*dataDir, "config.yaml")
		}
		if resolvedCache == ".calibration-cache.json" {
			resolvedCache = filepath.Join(*dataDir, ".calibration-cache.json")
		}
	}

	// 2. Load config.yaml (required)
	config, err := mesh.LoadConfig(resolvedConfig)
	if err != nil {
		log.Fatalf("Failed to load config: %v (looked at %s)", err, resolvedConfig)
	}
	log.Printf("Loaded config from %s", resolvedConfig)

	// 3. Load calibration cache (optional but recommended)
	var cache *mesh.CalibrationData
	cache, err = mesh.LoadCalibration(resolvedCache)
	if err != nil {
		log.Printf("Warning: Failed to load calibration cache %s: %v", resolvedCache, err)
	} else if cache != nil {
		log.Printf("Loaded calibration cache from %s", resolvedCache)
	} else {
		log.Printf("Warning: No calibration cache found at %s. Positions will not be transformed.", resolvedCache)
		log.Printf("Run './tudomesh --calibrate' to generate it.")
	}

	// 3. Determine reference vacuum
	refID := ""
	if config.Reference != "" {
		refID = config.Reference
	} else if cache != nil && cache.ReferenceVacuum != "" {
		refID = cache.ReferenceVacuum
	}
	if refID != "" {
		log.Printf("Reference vacuum: %s", refID)
	} else {
		log.Println("Reference vacuum: (will auto-select on first map data)")
	}

	// 4. Create state tracker for HTTP endpoints
	stateTracker := mesh.NewStateTracker()

	// Set colors from config
	for _, vc := range config.Vacuums {
		if vc.Color != "" {
			stateTracker.SetColor(vc.ID, vc.Color)
		}
	}

	// 5. Load initial maps from JSON exports if available
	initialMaps := loadInitialMaps(*dataDir)
	for id, m := range initialMaps {
		stateTracker.UpdateMap(id, m)
		// Also extract initial position
		if robotPos, robotAngle, ok := mesh.ExtractRobotPosition(m); ok {
			var gridX, gridY, worldAngle float64
			// Convert robot position from mm to grid coordinates
			// (Valetudo entity.Points are in mm, layer.Pixels are in grid units)
			pixelSize := float64(m.PixelSize)
			if pixelSize == 0 {
				pixelSize = 5 // default
			}
			gridPos := mesh.Point{X: robotPos.X / pixelSize, Y: robotPos.Y / pixelSize}

			if cache != nil {
				transform := cache.GetTransform(id)
				// Transform works in grid coordinates
				transformedPos := mesh.TransformPoint(gridPos, transform)
				gridX = transformedPos.X
				gridY = transformedPos.Y
				transformRotation := math.Atan2(transform.C, transform.A) * 180 / math.Pi
				worldAngle = robotAngle + transformRotation
				for worldAngle >= 360 {
					worldAngle -= 360
				}
				for worldAngle < 0 {
					worldAngle += 360
				}
			} else {
				// No calibration - use grid coords directly
				gridX = gridPos.X
				gridY = gridPos.Y
				worldAngle = robotAngle
			}
			stateTracker.UpdatePosition(id, gridX, gridY, worldAngle)
		}
	}
	if len(initialMaps) > 0 {
		fmt.Printf("Loaded %d initial maps from JSON exports\n", len(initialMaps))
	}

	// 6. Create position publisher (initialized after MQTT client)
	var publisher *mesh.Publisher
	var mqttClient *mesh.MQTTClient

	// 7. Start MQTT if enabled
	if *mqttMode {
		// Create message handler that updates state tracker
		messageHandler := func(vacuumID string, mapData *mesh.ValetudoMap, err error) {
			if err != nil {
				log.Printf("Error receiving map data for %s: %v", vacuumID, err)
				return
			}

			// Update state tracker with new map only if it contains drawable content
			// This prevents lightweight MQTT updates from overwriting the rich floorplan loaded from disk
			if mesh.HasDrawablePixels(mapData) {
				stateTracker.UpdateMap(vacuumID, mapData)
			}

			// Debug: log map data stats
			log.Printf("[DEBUG] %s: received map data - pixelSize=%d, layers=%d, entities=%d",
				vacuumID, mapData.PixelSize, len(mapData.Layers), len(mapData.Entities))

			// Extract robot position and angle from map data
			robotPos, robotAngle, ok := mesh.ExtractRobotPosition(mapData)
			if !ok {
				log.Printf("[DEBUG] %s: robot_position entity not found in %d entities", vacuumID, len(mapData.Entities))
				for i, e := range mapData.Entities {
					log.Printf("[DEBUG]   entity[%d]: type=%s, points=%d", i, e.Type, len(e.Points))
				}
				return
			}

			// Convert robot position from mm to grid coordinates
			// (Valetudo entity.Points are in mm, layer.Pixels are in grid units)
			pixelSize := float64(mapData.PixelSize)
			if pixelSize == 0 {
				pixelSize = 5 // default
			}
			gridPos := mesh.Point{X: robotPos.X / pixelSize, Y: robotPos.Y / pixelSize}

			// Auto-cache map to disk if it contains drawable data
			if mesh.HasDrawablePixels(mapData) {
				cachePath := filepath.Join(*dataDir, fmt.Sprintf("ValetudoMapExport-%s.json", vacuumID))
				// Save map data to disk for persistent floorplan (async)
				go func(p string, d *mesh.ValetudoMap) {
					// Encode back to JSON
					jsonBytes, err := json.MarshalIndent(d, "", "  ")
					if err == nil {
						if err := os.WriteFile(p, jsonBytes, 0644); err == nil {
							log.Printf("[DEBUG] Cached map for %s to %s", vacuumID, p)
						}
					}
				}(cachePath, mapData)
			}

			// Transform position if calibration available
			var gridX, gridY, worldAngle float64

			if cache != nil {
				transform := cache.GetTransform(vacuumID)
				// Transform works in grid coordinates
				transformedPos := mesh.TransformPoint(gridPos, transform)
				gridX = transformedPos.X
				gridY = transformedPos.Y

				// Calculate rotation from transform matrix
				transformRotation := math.Atan2(transform.C, transform.A) * 180 / math.Pi
				worldAngle = robotAngle + transformRotation

				// Normalize angle to [0, 360)
				for worldAngle >= 360 {
					worldAngle -= 360
				}
				for worldAngle < 0 {
					worldAngle += 360
				}
			} else {
				// No calibration - use grid coordinates directly
				gridX = gridPos.X
				gridY = gridPos.Y
				worldAngle = robotAngle
			}

			// Update state tracker with position (in grid coords)
			stateTracker.UpdatePosition(vacuumID, gridX, gridY, worldAngle)

			// Always log the position update for debugging
			log.Printf("%s: pos(%.0f,%.0f) / pixelSize=%d -> grid(%.1f,%.1f) -> world(%.1f,%.1f,%.0f°)",
				vacuumID, robotPos.X, robotPos.Y, mapData.PixelSize,
				gridPos.X, gridPos.Y, gridX, gridY, worldAngle)

			// Publish transformed position
			if publisher != nil {
				if err := publisher.PublishPosition(vacuumID, gridX, gridY, worldAngle); err != nil {
					log.Printf("Error publishing position for %s: %v", vacuumID, err)
				}
			}
		}

		// Initialize MQTT client
		mqttClient, err = mesh.InitMQTT(config, messageHandler)
		if err != nil {
			log.Fatalf("Failed to initialize MQTT: %v", err)
		}

		if mqttClient == nil {
			log.Fatal("MQTT broker not configured in config.yaml")
		}

		// Initialize publisher now that we have MQTT client
		publisher = mesh.NewPublisher(mqttClient.GetClient())
		fmt.Println("MQTT position publisher initialized")
	}

	// 8. Start HTTP server if enabled
	if *httpMode {
		// Create HTTP handlers
		httpServer := newHTTPServer(stateTracker, cache, config, refID)
		go func() {
			addr := fmt.Sprintf(":%d", *httpPort)
			fmt.Printf("HTTP server starting on %s\n", addr)
			if err := http.ListenAndServe(addr, httpServer); err != nil {
				log.Fatalf("HTTP server error: %v", err)
			}
		}()
	}

	// 9. Print service info
	fmt.Println("\nService Running")
	fmt.Println("===============")

	if *mqttMode {
		fmt.Println("\nMQTT:")
		fmt.Println("  Subscribed topics:")
		for _, vc := range config.Vacuums {
			fmt.Printf("    - %s (%s)\n", vc.Topic, vc.ID)
		}
		publishPrefix := config.MQTT.PublishPrefix
		if publishPrefix == "" {
			publishPrefix = "tudomesh"
		}
		fmt.Printf("  Publishing to: %s/{vacuumID}\n", publishPrefix)
		fmt.Printf("  Combined positions: %s/positions\n", publishPrefix)
	}

	if *httpMode {
		fmt.Printf("\nHTTP endpoints (port %d):\n", *httpPort)
		fmt.Println("  GET /health          - Health check")
		fmt.Println("  GET /composite-map.png - Color-coded composite map")
		fmt.Println("  GET /floorplan.png   - Greyscale floor plan")
		fmt.Println("  GET /live.png        - Greyscale floor plan with live positions")
	}

	fmt.Println("\nPress Ctrl+C to stop")

	// 10. Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan

	fmt.Println("\nShutting down service...")
	if mqttClient != nil {
		mqttClient.Disconnect()
	}
	fmt.Println("Service stopped")
}

// loadInitialMaps loads map JSON exports from the data directory
func loadInitialMaps(dataDir string) map[string]*mesh.ValetudoMap {
	maps := make(map[string]*mesh.ValetudoMap)

	pattern := filepath.Join(dataDir, "ValetudoMapExport-*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return maps
	}

	if len(files) == 0 {
		// Try current directory
		files, _ = filepath.Glob("ValetudoMapExport-*.json")
	}

	for _, file := range files {
		base := filepath.Base(file)
		name := strings.TrimPrefix(base, "ValetudoMapExport-")
		name = strings.Split(name, "-2")[0] // Remove timestamp

		m, err := mesh.ParseMapFile(file)
		if err != nil {
			log.Printf("Warning: Failed to load %s: %v", name, err)
			continue
		}
		maps[name] = m
	}

	return maps
}

// newHTTPServer creates an HTTP server with all endpoints
func newHTTPServer(stateTracker *mesh.StateTracker, cache *mesh.CalibrationData, config *mesh.Config, refID string) http.Handler {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := struct {
			Status    string    `json:"status"`
			Timestamp time.Time `json:"timestamp"`
			HasMaps   bool      `json:"hasMaps"`
		}{
			Status:    "ok",
			Timestamp: time.Now(),
			HasMaps:   stateTracker.HasMaps(),
		}
		if err := json.NewEncoder(w).Encode(status); err != nil {
			log.Printf("Error encoding health status: %v", err)
		}
	})

	// Composite map endpoint (color-coded)
	mux.HandleFunc("/composite-map.png", func(w http.ResponseWriter, r *http.Request) {
		maps := stateTracker.GetMaps()
		if len(maps) == 0 {
			http.Error(w, "No maps available", http.StatusServiceUnavailable)
			return
		}

		// Build transforms from cache
		transforms := buildTransforms(maps, cache, refID)

		// Determine effective reference
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}

		// Create renderer with colors from config
		renderer := mesh.NewCompositeRenderer(maps, transforms, effectiveRef)
		renderer.GlobalRotation = *rotateAll

		// Apply colors from config
		applyConfigColors(renderer, config)

		// If no drawable content exists, return service unavailable to avoid generating invalid images
		if !renderer.HasDrawableContent() {
			log.Printf("Warning: maps present but no drawable content; endpoint=/composite-map.png")
			http.Error(w, "No drawable map content", http.StatusServiceUnavailable)
			return
		}

		// Render and send
		img := renderer.Render()
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-cache")
		if err := png.Encode(w, img); err != nil {
			log.Printf("Error encoding composite map PNG: %v", err)
		}
	})

	// Greyscale floorplan endpoint
	mux.HandleFunc("/floorplan.png", func(w http.ResponseWriter, r *http.Request) {
		maps := stateTracker.GetMaps()
		if len(maps) == 0 {
			http.Error(w, "No maps available", http.StatusServiceUnavailable)
			return
		}

		// Build transforms from cache
		transforms := buildTransforms(maps, cache, refID)

		// Determine effective reference
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}

		// Create renderer
		renderer := mesh.NewCompositeRenderer(maps, transforms, effectiveRef)
		renderer.GlobalRotation = *rotateAll

		// If no drawable content exists, return service unavailable
		if !renderer.HasDrawableContent() {
			log.Printf("Warning: maps present but no drawable content; endpoint=/floorplan.png")
			http.Error(w, "No drawable map content", http.StatusServiceUnavailable)
			return
		}

		// Render greyscale and send
		img := renderer.RenderGreyscale()
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-cache")
		if err := png.Encode(w, img); err != nil {
			log.Printf("Error encoding floorplan PNG: %v", err)
		}
	})

	// Live positions endpoint
	mux.HandleFunc("/live.png", func(w http.ResponseWriter, r *http.Request) {
		maps := stateTracker.GetMaps()
		if len(maps) == 0 {
			http.Error(w, "No maps available", http.StatusServiceUnavailable)
			return
		}

		// Build transforms from cache
		transforms := buildTransforms(maps, cache, refID)

		// Determine effective reference
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}

		// Create renderer
		renderer := mesh.NewCompositeRenderer(maps, transforms, effectiveRef)
		renderer.GlobalRotation = *rotateAll

		// If no drawable content exists, we can still show positions on a blank map
		if !renderer.HasDrawableContent() {
			// Add more debug logging for layer keys if no drawable content
			for id, m := range maps {
				layerSummary := ""
				if len(m.Layers) > 0 {
					layerTypes := make([]string, len(m.Layers))
					for i, layer := range m.Layers {
						layerTypes[i] = layer.Type
					}
					layerSummary = strings.Join(layerTypes, ", ")
				}
				log.Printf("[DEBUG] renderer: map %s has no drawable pixels. Layers found: [%s]", id, layerSummary)
			}
			log.Printf("[DEBUG] renderer: no drawable content for /live.png, rendering positions only")
		}

		// Get live positions
		positions := stateTracker.GetPositions()

		// Render with positions and send
		img := renderer.RenderLive(positions)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-cache")
		if err := png.Encode(w, img); err != nil {
			log.Printf("Error encoding live PNG: %v", err)
		}
	})

	// Vector SVG endpoints
	// Composite map SVG endpoint
	mux.HandleFunc("/composite-map.svg", func(w http.ResponseWriter, r *http.Request) {
		maps := stateTracker.GetMaps()
		if len(maps) == 0 {
			http.Error(w, "No maps available", http.StatusServiceUnavailable)
			return
		}

		// Build transforms from cache
		transforms := buildTransforms(maps, cache, refID)

		// Determine effective reference
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}

		// Create vector renderer
		vectorRenderer := mesh.NewVectorRenderer(maps, transforms, effectiveRef)
		vectorRenderer.GlobalRotation = *rotateAll

		// Apply grid spacing from config if available
		if config != nil && config.GridSpacing > 0 {
			vectorRenderer.Padding = config.GridSpacing / 2
		}

		// Render SVG
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "no-cache")
		if err := vectorRenderer.RenderToSVG(w); err != nil {
			log.Printf("Error encoding composite map SVG: %v", err)
		}
	})

	// Floorplan SVG endpoint
	mux.HandleFunc("/floorplan.svg", func(w http.ResponseWriter, r *http.Request) {
		maps := stateTracker.GetMaps()
		if len(maps) == 0 {
			http.Error(w, "No maps available", http.StatusServiceUnavailable)
			return
		}

		// Build transforms from cache
		transforms := buildTransforms(maps, cache, refID)

		// Determine effective reference
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}

		// Create vector renderer
		vectorRenderer := mesh.NewVectorRenderer(maps, transforms, effectiveRef)
		vectorRenderer.GlobalRotation = *rotateAll

		// Apply grid spacing from config if available
		if config != nil && config.GridSpacing > 0 {
			vectorRenderer.Padding = config.GridSpacing / 2
		}

		// Render SVG
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "no-cache")
		if err := vectorRenderer.RenderToSVG(w); err != nil {
			log.Printf("Error encoding floorplan SVG: %v", err)
		}
	})

	return mux
}

// buildTransforms creates transform map from cache or identity
func buildTransforms(maps map[string]*mesh.ValetudoMap, cache *mesh.CalibrationData, refID string) map[string]mesh.AffineMatrix {
	transforms := make(map[string]mesh.AffineMatrix)

	for id := range maps {
		if cache != nil {
			transforms[id] = cache.GetTransform(id)
		} else {
			transforms[id] = mesh.Identity()
		}
	}

	return transforms
}

// applyConfigColors applies vacuum colors from config to the renderer
func applyConfigColors(renderer *mesh.CompositeRenderer, config *mesh.Config) {
	if config == nil {
		return
	}

	for _, vc := range config.Vacuums {
		if vc.Color == "" {
			continue
		}

		// Parse hex color
		hexColor := vc.Color
		if len(hexColor) > 0 && hexColor[0] == '#' {
			hexColor = hexColor[1:]
		}

		if len(hexColor) != 6 {
			continue
		}

		var r, g, b uint8
		if _, err := fmt.Sscanf(hexColor, "%02x%02x%02x", &r, &g, &b); err != nil {
			continue
		}

		// Create VacuumColor from the hex color
		baseColor := color.RGBA{r, g, b, 255}
		renderer.Colors[vc.ID] = mesh.VacuumColor{
			Floor: color.RGBA{r, g, b, 150}, // Semi-transparent for floor
			Wall:  darkenColor(baseColor),   // Darker version for walls
			Robot: baseColor,                // Full color for robot
		}
	}
}

// darkenColor creates a darker version of a color for walls
func darkenColor(c color.RGBA) color.RGBA {
	factor := 0.5
	return color.RGBA{
		R: uint8(float64(c.R) * factor),
		G: uint8(float64(c.G) * factor),
		B: uint8(float64(c.B) * factor),
		A: 255,
	}
}
