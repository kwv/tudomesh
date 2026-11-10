package main

import (
	"encoding/json"
	"fmt"
	"html"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"net/url"
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

	// Live positions endpoint
	mux.HandleFunc("/live.png", func(w http.ResponseWriter, r *http.Request) {
		theme, ok := requestTheme(w, r, config)
		if !ok {
			return
		}
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

		// Get live positions
		positions := stateTracker.GetPositions()

		// Create renderer
		renderer := mesh.NewCompositeRenderer(maps, transforms, effectiveRef)
		renderer.GlobalRotation = rotateAll
		renderer.Theme = theme
		applyLiveOptions(renderer, config, maps, positions)

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

		// Render with positions and send
		img := renderer.RenderLive(positions)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-cache")
		if err := png.Encode(w, img); err != nil {
			log.Printf("Error encoding live PNG: %v", err)
		}
	})

	// Composite map SVG: every vacuum's map overlaid in its color (alignment check)
	mux.HandleFunc("/composite-map.svg", func(w http.ResponseWriter, r *http.Request) {
		vr, ok := newSVGRenderer(w, stateTracker, cache, config, refID, rotateAll)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "no-cache")
		if err := vr.RenderCompositeToSVG(w); err != nil {
			log.Printf("Error encoding composite map SVG: %v", err)
		}
	})

	// Floorplan SVG: the live view without vacuums
	mux.HandleFunc("/floorplan.svg", func(w http.ResponseWriter, r *http.Request) {
		theme, ok := requestTheme(w, r, config)
		if !ok {
			return
		}
		vr, ok := newSVGRenderer(w, stateTracker, cache, config, refID, rotateAll)
		if !ok {
			return
		}
		vr.Theme = theme
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "no-cache")
		if err := vr.RenderLiveToSVG(w, nil); err != nil {
			log.Printf("Error encoding floorplan SVG: %v", err)
		}
	})

	// Live SVG endpoint
	mux.HandleFunc("/live.svg", func(w http.ResponseWriter, r *http.Request) {
		theme, ok := requestTheme(w, r, config)
		if !ok {
			return
		}
		writeLiveSVG(w, stateTracker, cache, config, refID, rotateAll, "", mesh.Crop{}, theme)
	})

	// Default route serves HTML page embedding the SVG map
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		src := "/live.svg"
		if t := r.URL.Query().Get("theme"); t != "" {
			if _, err := mesh.ThemeByName(t); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			src += "?theme=" + url.QueryEscape(t)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tudomesh</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
html,body{width:100%%;height:100%%;overflow:hidden;background:#1a1a1a}
img{display:block;width:100vw;height:100vh;object-fit:contain}
</style>
</head>
<body>
<img src="%s" alt="Live Map">
</body>
</html>`, html.EscapeString(src))
	})

	registerVacuumHandlers(mux, stateTracker, cache, config, refID, rotateAll)

	// Wrap mux with logging middleware
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[HTTP] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		mux.ServeHTTP(w, r)
	})
}

// requestTheme resolves ?theme= (light, dark or auto; default from config)
// with the configured colors. It writes a 400 and returns false on a bad name.
func requestTheme(w http.ResponseWriter, r *http.Request, config *mesh.Config) (mesh.Theme, bool) {
	t, err := config.ResolveTheme(r.URL.Query().Get("theme"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return mesh.Theme{}, false
	}
	return t, true
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
	for id, c := range configColors(config) {
		renderer.Colors[id] = c
	}
}

// configColors returns the colors set in config (hex "#rrggbb"), by vacuum id.
func configColors(config *mesh.Config) map[string]mesh.VacuumColor {
	colors := map[string]mesh.VacuumColor{}
	if config == nil {
		return colors
	}
	for _, vc := range config.Vacuums {
		hexColor := strings.TrimPrefix(vc.Color, "#")
		if len(hexColor) != 6 {
			continue
		}
		var r, g, b uint8
		if _, err := fmt.Sscanf(hexColor, "%02x%02x%02x", &r, &g, &b); err != nil {
			continue
		}
		baseColor := color.NRGBA{r, g, b, 255}
		colors[vc.ID] = mesh.VacuumColor{
			Floor: color.NRGBA{r, g, b, 150}, // semi-transparent so overlaps show
			Wall:  darkenColor(baseColor),
			Robot: baseColor,
		}
	}
	return colors
}

// newSVGRenderer builds a vector renderer over the current maps with
// calibration, colors, grid spacing and rotation applied. It writes a 503
// and returns false when no maps are available.
func newSVGRenderer(w http.ResponseWriter, st *mesh.StateTracker, cache *mesh.CalibrationData, config *mesh.Config, refID string, rotateAll float64) (*mesh.VectorRenderer, bool) {
	maps := st.GetMaps()
	if len(maps) == 0 {
		http.Error(w, "No maps available", http.StatusServiceUnavailable)
		return nil, false
	}
	effectiveRef := refID
	if effectiveRef == "" {
		effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
	}
	vr := mesh.NewVectorRenderer(maps, buildTransforms(maps, cache), effectiveRef)
	vr.GlobalRotation = rotateAll
	for id, c := range configColors(config) {
		vr.Colors[id] = c
	}
	if config != nil && config.GridSpacing > 0 {
		vr.GridSpacing = config.GridSpacing
	}
	if config != nil {
		vr.IgnoreRooms = config.IgnoreRooms
	}
	return vr, true
}

// applyLiveOptions sets the live-view options of a PNG renderer: ignored
// rooms from config and legend names for positions (vacuumNames).
func applyLiveOptions(renderer *mesh.CompositeRenderer, config *mesh.Config, maps map[string]*mesh.ValetudoMap, positions map[string]*mesh.LivePosition) {
	if config != nil {
		renderer.IgnoreRooms = config.IgnoreRooms
	}
	renderer.Names = vacuumNames(config, maps, positions)
}

// vacuumNames maps each position key to its vacuum's MQTT identifier (from
// the configured or discovered topic, see resolveVacuum). Keys without one
// are left out, so renderers fall back to the id.
func vacuumNames(config *mesh.Config, maps map[string]*mesh.ValetudoMap, positions map[string]*mesh.LivePosition) map[string]string {
	names := make(map[string]string, len(positions))
	for key := range positions {
		if _, name, ok := resolveVacuum(strings.TrimSuffix(key, ".json"), config, maps); ok && name != "" {
			names[key] = name
		}
	}
	return names
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
