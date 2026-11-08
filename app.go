package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/kwv/tudomesh/mesh"
)

// App encapsulates the application state and dependencies
type App struct {
	Config         *mesh.Config
	Calibration    *mesh.CalibrationData
	StateTracker   *mesh.StateTracker
	MQTTClient     *mesh.MQTTClient
	AutoCalibrator *mesh.AutoCalibrator

	// CLI Flags (effectively dependencies)
	DataDir          string
	ConfigFile       string
	CalibrationCache string
	RotateAll        float64
	ForceRotation    string
	ReferenceVacuum  string
	OutputFile       string
	RenderFormat     string
	VectorFormat     string
	GridSpacing      float64
	Floorplan        bool
	HttpPort         int
	MqttMode         bool
	HttpMode         bool
}

// NewApp creates a new App instance
func NewApp() *App {
	return &App{
		StateTracker: mesh.NewStateTracker(),
	}
}

// ApplyOptions applies CLI options to the App instance
func (a *App) ApplyOptions(opts AppOptions) {
	a.DataDir = opts.DataDir
	a.ConfigFile = opts.ConfigFile
	a.CalibrationCache = opts.CalibrationCache
	a.RotateAll = opts.RotateAll
	a.ForceRotation = opts.ForceRotation
	a.ReferenceVacuum = opts.ReferenceVacuum
	a.OutputFile = opts.OutputFile
	a.RenderFormat = opts.RenderFormat
	a.VectorFormat = opts.VectorFormat
	a.GridSpacing = opts.GridSpacing
	a.Floorplan = opts.Floorplan
	a.HttpPort = opts.HttpPort
	a.MqttMode = opts.MqttMode
	a.HttpMode = opts.HttpMode
}

// RunParseOnly finds and parses all Valetudo JSON exports
func (a *App) RunParseOnly() {
	pattern := filepath.Join(a.DataDir, "ValetudoMapExport-*.json")
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
		a.parseAndPrint(file)
	}
}

func (a *App) parseAndPrint(path string) {
	// Extract vacuum name from filename
	base := filepath.Base(path)
	name := vacuumIDFromExport(base)

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

// RunCompareRotation renders 4 images with different rotation options for a vacuum
func (a *App) RunCompareRotation(vacuumID string) {
	pattern := filepath.Join(a.DataDir, "ValetudoMapExport-*.json")
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
		name := vacuumIDFromExport(base)

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

	if err := mesh.RenderRotationComparison(maps, vacuumID, outputPrefix, a.ReferenceVacuum, a.RotateAll); err != nil {
		log.Fatalf("Error rendering: %v", err)
	}

	fmt.Printf("Created: %s_0.png, %s_90.png, %s_180.png, %s_270.png\n",
		outputPrefix, outputPrefix, outputPrefix, outputPrefix)
}

// RunRender loads maps, aligns them, and outputs a composite PNG
func (a *App) RunRender() {
	pattern := filepath.Join(a.DataDir, "ValetudoMapExport-*.json")
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
		name := vacuumIDFromExport(base)

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
	if _, err := os.Stat(a.ConfigFile); err == nil {
		config, err = mesh.LoadConfig(a.ConfigFile)
		if err != nil {
			log.Printf("Warning: Failed to load config file %s: %v", a.ConfigFile, err)
		} else {
			log.Printf("Loaded config from %s", a.ConfigFile)
			mesh.ResolveVacuums(config, filepath.Join(a.DataDir, vacuumIdentityCache), discoveryTimeout)
		}
	}

	// Load calibration cache (auto-computed transforms)
	var cache *mesh.CalibrationData
	cache, err = mesh.LoadCalibration(a.CalibrationCache)
	if err != nil {
		log.Printf("Warning: Failed to load calibration cache %s: %v", a.CalibrationCache, err)
	} else if cache != nil {
		log.Printf("Loaded calibration cache from %s", a.CalibrationCache)
	}

	// Determine effective reference
	effectiveRef := a.ReferenceVacuum
	if effectiveRef == "" && config != nil {
		effectiveRef = mesh.GetEffectiveReference(config, cache, maps)
	}
	if effectiveRef == "" {
		effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
	}
	fmt.Printf("Reference vacuum: %s\n", effectiveRef)

	// Build transforms from cache, config, and CLI (priority: CLI > config > cache > auto-alignment)
	transforms := make(map[string]mesh.AffineMatrix)
	transforms[effectiveRef] = mesh.Identity()
	needsRecalibration := false

	for id := range maps {
		if id == effectiveRef {
			continue
		}

		var transform mesh.AffineMatrix
		source := ""

		// Priority 1: Check cache
		if cache != nil && cache.ReferenceVacuum == effectiveRef {
			if vc, ok := cache.Vacuums[id]; ok {
				transform = vc.Transform
				source = "cache"
			}
		}

		// Priority 2: Config rotation hint narrows the search to ±20°
		if config != nil {
			vc := config.GetVacuumByID(id)
			if vc != nil && vc.Rotation != nil {
				rotHint := *vc.Rotation
				fmt.Printf("  %s: aligning near rotation hint %.0f° from config\n", id, rotHint)
				result, _ := mesh.AlignMapsWithRotationHint(maps[id], maps[effectiveRef], rotHint)
				transform = result.Transform
				source = fmt.Sprintf("hint(%.0f°)", rotHint)
				needsRecalibration = true

				// Apply manual translation if provided
				if vc.Translation != nil && (vc.Translation.X != 0 || vc.Translation.Y != 0) {
					transform = mesh.MultiplyMatrices(mesh.Translation(vc.Translation.X, vc.Translation.Y), transform)
					source += fmt.Sprintf("+translation(%.0f,%.0f)", vc.Translation.X, vc.Translation.Y)
				}
			}
		}

		// Priority 3: CLI --force-rotation fixes the rotation exactly (highest priority)
		if a.ForceRotation != "" {
			cliRotations := mesh.BuildForceRotationMap(a.ForceRotation)
			if rotDeg, ok := cliRotations[id]; ok {
				fmt.Printf("  %s: CLI forced rotation %.0f°\n", id, rotDeg)
				result, _ := mesh.AlignMapsAtRotation(maps[id], maps[effectiveRef], rotDeg)
				transform = result.Transform
				source = fmt.Sprintf("forced(%.0f°)", rotDeg)
				needsRecalibration = true
			}
		}

		// Otherwise run full alignment
		if transform.A == 0 && transform.D == 0 {
			fmt.Printf("  %s: aligning (not in cache)\n", id)
			result, _ := mesh.AlignMaps(maps[id], maps[effectiveRef])
			transform = result.Transform
			source = result.Method
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
		nowUnix := time.Now().Unix()
		vacCals := make(map[string]mesh.VacuumCalibration, len(transforms))
		for id, t := range transforms {
			area := 0
			if m, ok := maps[id]; ok {
				area = m.MetaData.TotalLayerArea
			}
			vacCals[id] = mesh.VacuumCalibration{
				Transform:            t,
				LastUpdated:          nowUnix,
				MapAreaAtCalibration: area,
			}
		}
		newCache := mesh.CalibrationData{
			ReferenceVacuum: effectiveRef,
			Vacuums:         vacCals,
		}
		if err := mesh.SaveCalibration(a.CalibrationCache, &newCache); err != nil {
			log.Printf("Warning: Failed to save calibration cache: %v", err)
		} else {
			fmt.Printf("Calibration cache updated: %s\n", a.CalibrationCache)
		}
	}

	// Render with computed transforms
	fmt.Printf("\nRendering composite map to %s...\n", a.OutputFile)

	// Determine render format
	format := a.RenderFormat
	if format != "raster" && format != "vector" && format != "both" {
		log.Fatalf("Invalid format: %s (must be raster, vector, or both)", format)
	}

	// Raster rendering (existing behavior)
	if format == "raster" || format == "both" {
		renderer := mesh.NewCompositeRenderer(maps, transforms, effectiveRef)
		renderer.GlobalRotation = a.RotateAll
		applyConfigColors(renderer, config)

		outputPath := a.OutputFile
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
		if a.VectorFormat != "" && a.VectorFormat != "svg" {
			log.Fatalf("Unsupported --vector-format %q (only svg)", a.VectorFormat)
		}
		vectorRenderer := mesh.NewVectorRenderer(maps, transforms, effectiveRef)
		vectorRenderer.GlobalRotation = a.RotateAll
		for id, c := range configColors(config) {
			vectorRenderer.Colors[id] = c
		}

		// Grid spacing from config, else the flag
		spacing := a.GridSpacing
		if config != nil && config.GridSpacing > 0 {
			spacing = config.GridSpacing
		}
		if spacing > 0 {
			vectorRenderer.GridSpacing = spacing
			vectorRenderer.Padding = spacing / 2
		}

		outputPath := strings.TrimSuffix(a.OutputFile, filepath.Ext(a.OutputFile)) + ".svg"
		outFile, err := os.Create(outputPath)
		if err != nil {
			log.Fatalf("Error creating output file %s: %v", outputPath, err)
		}
		defer func() {
			if err := outFile.Close(); err != nil {
				log.Printf("Warning: error closing output file %s: %v", outputPath, err)
			}
		}()
		if err := vectorRenderer.RenderCompositeToSVG(outFile); err != nil {
			log.Fatalf("Error rendering vector SVG: %v", err)
		}
		fmt.Printf("Created vector SVG: %s\n", outputPath)
	}

	if a.Floorplan {
		a.writeFloorplan(maps, transforms, effectiveRef, config, format)
	}

	fmt.Println("Done!")
}

// RunRenderIndividual renders each vacuum map as a separate PNG
func (a *App) RunRenderIndividual(individualRotationFlag string) {
	pattern := filepath.Join(a.DataDir, "ValetudoMapExport-*.json")
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
	if individualRotationFlag != "" {
		for _, spec := range strings.Split(individualRotationFlag, ",") {
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
		name := vacuumIDFromExport(base)

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

// RunCalibration loads all JSON exports, aligns them to the reference and writes the calibration cache
func (a *App) RunCalibration() {
	pattern := filepath.Join(a.DataDir, "ValetudoMapExport-*.json")
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
		name := vacuumIDFromExport(base)

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

	now := time.Now().Unix()
	cache := mesh.CalibrationData{
		ReferenceVacuum: refID,
		Vacuums:         make(map[string]mesh.VacuumCalibration),
	}
	cache.Vacuums[refID] = mesh.VacuumCalibration{
		Transform:            mesh.Identity(),
		LastUpdated:          now,
		MapAreaAtCalibration: refMap.MetaData.TotalLayerArea,
	}

	fmt.Println("Aligning maps...")
	fmt.Println(strings.Repeat("-", 60))

	ids := make([]string, 0, len(maps))
	for id := range maps {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		m := maps[id]
		if id == refID {
			fmt.Printf("%-25s: [REFERENCE - identity transform]\n", id)
			continue
		}

		result, ok := mesh.AlignMaps(m, refMap)
		if !ok {
			fmt.Printf("%-25s: no floor geometry, skipped\n", id)
			continue
		}
		cache.Vacuums[id] = mesh.VacuumCalibration{
			Transform:            result.Transform,
			LastUpdated:          now,
			MapAreaAtCalibration: m.MetaData.TotalLayerArea,
		}

		fmt.Printf("%-25s:\n", id)
		fmt.Printf("  Method: %s, rotation %.1f°, confidence %.2f (>= %.2f is unambiguous)\n",
			result.Method, result.RotationDeg, result.Confidence, mesh.MinGridConfidence)
		fmt.Printf("  Translation: (%.1f, %.1f)\n", result.Transform.Tx, result.Transform.Ty)

		srcPos, srcAngle, _ := mesh.ExtractRobotPosition(m)
		srcCharger, _ := mesh.ExtractChargerPosition(m)
		ps := float64(m.PixelSize)
		if ps == 0 {
			ps = 5
		}
		// Entity positions are in pixelSize units; transforms work in grid cells.
		toWorld := func(p mesh.Point) mesh.Point {
			g := mesh.TransformPoint(mesh.Point{X: p.X / ps, Y: p.Y / ps}, result.Transform)
			return mesh.Point{X: g.X * ps, Y: g.Y * ps}
		}
		worldPos, worldCharger := toWorld(srcPos), toWorld(srcCharger)
		fmt.Printf("  Robot: local(%.0f,%.0f) -> world(%.0f,%.0f), angle %.0f° -> %.0f°\n",
			srcPos.X, srcPos.Y, worldPos.X, worldPos.Y, srcAngle, mesh.TransformAngle(srcAngle, result.Transform))
		fmt.Printf("  Charger: local(%.0f,%.0f) -> world(%.0f,%.0f)\n",
			srcCharger.X, srcCharger.Y, worldCharger.X, worldCharger.Y)
		fmt.Println()
	}

	refPos, refAngle, _ := mesh.ExtractRobotPosition(refMap)
	refCharger, _ := mesh.ExtractChargerPosition(refMap)
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("Reference (%s) positions (world coordinates):\n", refID)
	fmt.Printf("  Robot: (%.0f, %.0f) angle=%.0f°\n", refPos.X, refPos.Y, refAngle)
	fmt.Printf("  Charger: (%.0f, %.0f)\n", refCharger.X, refCharger.Y)

	printConsensus(maps, cache.Transforms(), refID)

	// Save to cache file
	fmt.Printf("\nSaving calibration cache to %s\n", a.CalibrationCache)
	if err := mesh.SaveCalibration(a.CalibrationCache, &cache); err != nil {
		log.Printf("Warning: Failed to save calibration cache: %v", err)
	} else {
		fmt.Println("Calibration cache saved successfully")
	}
}

// RunService starts the combined MQTT and/or HTTP service
func (a *App) RunService() {
	fmt.Println("Starting tudomesh service...")

	// 1. Resolve configuration paths relative to data-dir if provided
	resolvedConfig := a.ConfigFile
	resolvedCache := a.CalibrationCache

	// If data-dir is specified and files are still pointing to defaults,
	// resolve them relative to the data-dir.
	if a.DataDir != "." {
		if resolvedConfig == "config.yaml" {
			resolvedConfig = filepath.Join(a.DataDir, "config.yaml")
		}
		if resolvedCache == ".calibration-cache.json" {
			resolvedCache = filepath.Join(a.DataDir, ".calibration-cache.json")
		}
	}

	// 2. Load config.yaml (required)
	config, err := mesh.LoadConfig(resolvedConfig)
	if err != nil {
		log.Fatalf("Failed to load config: %v (looked at %s)", err, resolvedConfig)
	}
	log.Printf("Loaded config from %s", resolvedConfig)
	mesh.ResolveVacuums(config, filepath.Join(a.DataDir, vacuumIdentityCache), discoveryTimeout)
	a.Config = config
	if a.MqttMode && os.Getenv("MQTT_BROKER") == "" && config.MQTT.Broker == "" {
		log.Fatal(errNoBroker)
	}

	// Check if data directory is writable (for cache and map persistence)
	if err := a.checkWritability(a.DataDir); err != nil {
		log.Printf("WARNING: Data directory %s is not writable: %v", a.DataDir, err)
		log.Printf("Auto-calibration and map caching will fail. Use 'chown 65532' if running in Docker.")
	}

	// 3. Load calibration cache (optional but recommended)
	// One instance shared by HTTP handlers, MQTT position updates and the
	// auto-calibrator, so calibrations made at runtime apply immediately.
	cache := loadServiceCalibration(resolvedCache)
	a.Calibration = cache

	// 3. Determine reference vacuum
	refID := ""
	if config.Reference != "" {
		refID = config.Reference
	} else if ref := cache.Reference(); ref != "" {
		refID = ref
	}
	if refID != "" {
		log.Printf("Reference vacuum: %s", refID)
	} else {
		log.Println("Reference vacuum: (will auto-select on first map data)")
	}

	// Set colors from config
	for _, vc := range config.Vacuums {
		if vc.Color != "" {
			a.StateTracker.SetColor(vc.ID, vc.Color)
		}
	}

	// 5. Load initial maps from JSON exports if available
	initialMaps := a.loadInitialMaps(a.DataDir)
	for id, m := range initialMaps {
		a.StateTracker.UpdateMap(id, m)
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
				worldAngle = mesh.TransformAngle(robotAngle, transform)
			} else {
				// No calibration - use grid coords directly
				gridX = gridPos.X
				gridY = gridPos.Y
				worldAngle = robotAngle
			}
			a.StateTracker.UpdatePosition(id, gridX, gridY, worldAngle)
		}
	}
	if len(initialMaps) > 0 {
		fmt.Printf("Loaded %d initial maps from JSON exports\n", len(initialMaps))
		a.StateTracker.Floorplan(cache, refID)
	}

	// 7. Start MQTT if enabled
	if a.MqttMode {
		// Create message handler that updates state tracker
		messageHandler := func(vacuumID string, rawPayload []byte, mapData *mesh.ValetudoMap, err error) {
			// Handle raw PNG images (no zTXt metadata) by writing directly to disk
			if err != nil && mesh.IsPNG(rawPayload) {
				pngPath := filepath.Join(a.DataDir, fmt.Sprintf("%s.png", vacuumID))
				if writeErr := os.WriteFile(pngPath, rawPayload, 0644); writeErr != nil {
					log.Printf("Error writing raw PNG for %s: %v", vacuumID, writeErr)
				} else {
					log.Printf("%s: saved raw PNG to %s (%d bytes)", vacuumID, pngPath, len(rawPayload))
				}
				// Raw PNG has no position data to extract; skip further processing
				return
			}

			if err != nil {
				log.Printf("Error receiving map data for %s: %v", vacuumID, err)
				return
			}

			// Update state tracker with new map only if it contains drawable content
			// This prevents lightweight MQTT updates from overwriting the rich floorplan loaded from disk
			if mesh.HasDrawablePixels(mapData) {
				a.StateTracker.UpdateMap(vacuumID, mapData)
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
				cachePath := filepath.Join(a.DataDir, fmt.Sprintf("ValetudoMapExport-%s.json", vacuumID))
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

			if a.Calibration != nil {
				transform := a.Calibration.GetTransform(vacuumID)
				// Transform works in grid coordinates
				transformedPos := mesh.TransformPoint(gridPos, transform)
				gridX = transformedPos.X
				gridY = transformedPos.Y

				// Calculate rotation from transform matrix and apply to angle
				worldAngle = mesh.TransformAngle(robotAngle, transform)

				log.Printf("[CALIBRATION] %s: transform(A=%.4f,C=%.4f) rotation=%.1f° localAngle=%.0f° -> worldAngle=%.0f°",
					vacuumID, transform.A, transform.C,
					math.Atan2(transform.C, transform.A)*180/math.Pi,
					robotAngle, worldAngle)
			} else {
				// No calibration - use grid coordinates directly
				gridX = gridPos.X
				gridY = gridPos.Y
				worldAngle = robotAngle
				log.Printf("[CALIBRATION] %s: no calibration loaded, using raw angle=%.0f°", vacuumID, robotAngle)
			}

			// Update state tracker with position (in grid coords)
			a.StateTracker.UpdatePosition(vacuumID, gridX, gridY, worldAngle)

			// Always log the position update for debugging
			log.Printf("%s: pos(%.0f,%.0f) / pixelSize=%d -> grid(%.1f,%.1f) -> world(%.1f,%.1f,%.0f°)",
				vacuumID, robotPos.X, robotPos.Y, mapData.PixelSize,
				gridPos.X, gridPos.Y, gridX, gridY, worldAngle)
		}

		// Initialize MQTT client
		mqttClient, err := mesh.InitMQTT(config, messageHandler)
		if err != nil {
			log.Fatalf("Failed to initialize MQTT: %v", err)
		}
		a.MQTTClient = mqttClient

		if mqttClient == nil {
			log.Fatal(errNoBroker)
		}

		// Initialize auto-calibrator and register docking handler
		a.AutoCalibrator = mesh.NewAutoCalibrator(config, cache, resolvedCache, a.DataDir, a.StateTracker)
		a.AutoCalibrator.OnCalibrated = func(ref string) { a.StateTracker.Floorplan(cache, ref) }
		mqttClient.SetDockingHandler(a.AutoCalibrator.OnDockingEvent)
		mqttClient.SetStateHandler(a.StateTracker.UpdateStatus)
		fmt.Println("Auto-calibrator initialized (triggers on docking events)")
	}

	// 8. Start HTTP server if enabled
	if a.HttpMode {
		// Create HTTP handlers
		httpServer := newHTTPServer(a.StateTracker, a.Calibration, a.Config, refID, a.RotateAll)
		go func() {
			addr := fmt.Sprintf("0.0.0.0:%d", a.HttpPort)
			log.Printf("[HTTP] Starting server on %s", addr)
			if err := http.ListenAndServe(addr, httpServer); err != nil {
				log.Fatalf("[HTTP] Server error: %v", err)
			}
			log.Printf("[HTTP] Server stopped unexpectedly")
		}()
	}

	// 9. Print service info
	fmt.Println("\nService Running")
	fmt.Println("===============")

	if a.MqttMode {
		fmt.Println("\nMQTT:")
		fmt.Println("  Subscribed topics:")
		for _, vc := range config.Vacuums {
			fmt.Printf("    - %s (%s)\n", vc.Topic, vc.ID)
		}
	}

	if a.HttpMode {
		fmt.Printf("\nHTTP endpoints (port %d):\n", a.HttpPort)
		fmt.Println("  GET /                - Homepage (live SVG map)")
		fmt.Println("  GET /health          - Health check")
		fmt.Println("  GET /live.svg        - Live map with vacuum positions (SVG)")
		fmt.Println("  GET /live.png        - Live map with vacuum positions (PNG)")
		fmt.Println("  GET /composite-map.png - Color-coded composite map")
		fmt.Println("  GET /composite-map.svg - Color-coded composite map (SVG)")
		fmt.Println("  GET /floorplan.svg   - Greyscale floor plan (SVG)")
	}

	fmt.Println("\nPress Ctrl+C to stop")

	// 10. Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan

	fmt.Println("\nShutting down service...")
	if a.MQTTClient != nil {
		a.MQTTClient.Disconnect()
	}
	fmt.Println("Service stopped")
}

// vacuumIdentityCache (in the data dir) remembers what each robot's
// Valetudo API reported, for robots that are offline at startup.
const vacuumIdentityCache = ".vacuums.json"

// errNoBroker explains how to fix a missing MQTT broker.
const errNoBroker = `No MQTT broker configured. Set it in config.yaml:

  mqtt:
    broker: mqtt://<host>:1883

(or the MQTT_BROKER environment variable). Without it, tudomesh only learns
the broker from a robot that answers at startup, and none did.`

// discoveryTimeout bounds each robot's Valetudo API lookup at startup.
const discoveryTimeout = 5 * time.Second

// loadInitialMaps loads map JSON exports from the data directory
func (a *App) loadInitialMaps(dataDir string) map[string]*mesh.ValetudoMap {
	maps := make(map[string]*mesh.ValetudoMap)

	pattern := filepath.Join(dataDir, "ValetudoMapExport-*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return maps
	}

	for _, file := range files {
		base := filepath.Base(file)
		name := vacuumIDFromExport(base)

		m, err := mesh.ParseMapFile(file)
		if err != nil {
			log.Printf("Warning: Failed to load %s: %v", name, err)
			continue
		}
		maps[name] = m
	}

	return maps
}

// exportTimestamp matches a trailing date on export filenames: Valetudo's
// "-YYYY-MM-DDTHH-MM-SS" plus shorter forms like "-YYYYMMDD" and "-YYYY".
var exportTimestamp = regexp.MustCompile(`-\d{4}(-?\d{2}){0,2}(T[\d-]+)?$`)

// vacuumIDFromExport turns "ValetudoMapExport-<id>[-<timestamp>].json" into <id>,
// matching the vacuum ids used in config.
func vacuumIDFromExport(base string) string {
	name := strings.TrimSuffix(strings.TrimPrefix(base, "ValetudoMapExport-"), ".json")
	return exportTimestamp.ReplaceAllString(name, "")
}

// loadServiceCalibration loads the calibration cache for service mode. It
// never returns nil: a missing or unreadable cache yields an empty one that
// the auto-calibrator fills in as vacuums dock.
func loadServiceCalibration(path string) *mesh.CalibrationData {
	cache, err := mesh.LoadCalibration(path)
	switch {
	case err != nil:
		log.Printf("Warning: Failed to load calibration cache %s: %v", path, err)
	case cache != nil:
		log.Printf("Loaded calibration cache from %s", path)
		return cache
	default:
		log.Printf("No calibration cache at %s yet; vacuums are aligned as they dock (or run --calibrate).", path)
	}
	return &mesh.CalibrationData{Vacuums: make(map[string]mesh.VacuumCalibration)}
}

// checkWritability verifies that a directory is writable by creating and deleting a temporary file
func (a *App) checkWritability(dir string) error {
	tmpFile := filepath.Join(dir, ".tudomesh-write-test")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		return err
	}
	return os.Remove(tmpFile)
}
