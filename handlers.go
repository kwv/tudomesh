package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/kwv/tudomesh/mesh"
)

// newHTTPServer creates an HTTP server with all endpoints
func newHTTPServer(stateTracker *mesh.StateTracker, cache *mesh.CalibrationData, config *mesh.Config, refID string, rotateAll float64) http.Handler {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[HTTP] /health request from %s", r.RemoteAddr)
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
		transforms := buildTransforms(maps, cache)

		// Determine effective reference
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}

		// Create renderer with colors from config
		renderer := mesh.NewCompositeRenderer(maps, transforms, effectiveRef)
		renderer.GlobalRotation = rotateAll

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
		transforms := buildTransforms(maps, cache)

		// Determine effective reference
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}

		// Create renderer
		renderer := mesh.NewCompositeRenderer(maps, transforms, effectiveRef)
		renderer.GlobalRotation = rotateAll

		// If no drawable content exists, return service unavailable
		if !renderer.HasDrawableContent() {
			log.Printf("Warning: maps present but no drawable content; endpoint=/floorplan.png")
			http.Error(w, "No drawable_map content", http.StatusServiceUnavailable)
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
		transforms := buildTransforms(maps, cache)

		// Determine effective reference
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}

		// Create renderer
		renderer := mesh.NewCompositeRenderer(maps, transforms, effectiveRef)
		renderer.GlobalRotation = rotateAll

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
		transforms := buildTransforms(maps, cache)

		// Determine effective reference
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}

		// Create vector renderer
		vectorRenderer := mesh.NewVectorRenderer(maps, transforms, effectiveRef)
		vectorRenderer.GlobalRotation = rotateAll

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
		transforms := buildTransforms(maps, cache)

		// Determine effective reference
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}

		// Create vector renderer
		vectorRenderer := mesh.NewVectorRenderer(maps, transforms, effectiveRef)
		vectorRenderer.GlobalRotation = rotateAll

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

	// Default route serves HTML page embedding the SVG map
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tudomesh</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
html,body{width:100%;height:100%;overflow:hidden;background:#1a1a1a}
img{display:block;width:100vw;height:100vh;object-fit:contain}
</style>
</head>
<body>
<img src="/composite-map.svg" alt="Composite Map">
</body>
</html>`)
	})

	// Wrap mux with logging middleware
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[HTTP] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		mux.ServeHTTP(w, r)
	})
}

// buildTransforms creates transform map from cache or identity
func buildTransforms(maps map[string]*mesh.ValetudoMap, cache *mesh.CalibrationData) map[string]mesh.AffineMatrix {
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
		baseColor := color.NRGBA{r, g, b, 255}
		renderer.Colors[vc.ID] = mesh.VacuumColor{
			Floor: color.NRGBA{r, g, b, 150}, // Semi-transparent for floor
			Wall:  darkenColor(baseColor),    // Darker version for walls
			Robot: baseColor,                 // Full color for robot
		}
	}
}

// darkenColor creates a darker version of a color for walls
func darkenColor(c color.NRGBA) color.NRGBA {
	factor := 0.5
	return color.NRGBA{
		R: uint8(float64(c.R) * factor),
		G: uint8(float64(c.G) * factor),
		B: uint8(float64(c.B) * factor),
		A: 255,
	}
}
