package main

import (
	"encoding/json"
	"image/color"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kwv/tudomesh/mesh"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// minimalMap returns a ValetudoMap with one floor pixel and one wall pixel so
// that HasDrawableContent() returns true without pulling in heavy test fixtures.
// Pixels are encoded as triplets [x, y, count] by the mesh package convention.
func minimalMap() *mesh.ValetudoMap {
	return &mesh.ValetudoMap{
		MetaData: mesh.MapMetaData{TotalLayerArea: 100},
		Size:     mesh.Size{X: 100, Y: 100},
		Layers: []mesh.MapLayer{
			{Type: "floor", Pixels: []int{10, 10, 1}},
			{Type: "wall", Pixels: []int{20, 20, 1}},
		},
	}
}

// populatedTracker returns a StateTracker that already contains one map entry.
func populatedTracker() *mesh.StateTracker {
	st := mesh.NewStateTracker()
	st.UpdateMap("vac1", minimalMap())
	return st
}

// emptyTracker returns a StateTracker with no maps.
func emptyTracker() *mesh.StateTracker {
	return mesh.NewStateTracker()
}

// ---------------------------------------------------------------------------
// darkenColor
// ---------------------------------------------------------------------------

func TestDarkenColor(t *testing.T) {
	tests := []struct {
		name  string
		input color.NRGBA
		want  color.NRGBA
	}{
		{
			name:  "zero values",
			input: color.NRGBA{R: 0, G: 0, B: 0, A: 255},
			want:  color.NRGBA{R: 0, G: 0, B: 0, A: 255},
		},
		{
			name:  "full white",
			input: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			want:  color.NRGBA{R: 127, G: 127, B: 127, A: 255}, // floor(255*0.5)
		},
		{
			name:  "mid values",
			input: color.NRGBA{R: 200, G: 100, B: 50, A: 200},
			want:  color.NRGBA{R: 100, G: 50, B: 25, A: 255}, // alpha always 255
		},
		{
			name:  "single channel",
			input: color.NRGBA{R: 100, G: 0, B: 0, A: 128},
			want:  color.NRGBA{R: 50, G: 0, B: 0, A: 255},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := darkenColor(tt.input)
			if got != tt.want {
				t.Errorf("darkenColor(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// applyConfigColors
// ---------------------------------------------------------------------------

func TestApplyConfigColors_NilConfig(t *testing.T) {
	renderer := mesh.NewCompositeRenderer(
		map[string]*mesh.ValetudoMap{"vac1": minimalMap()},
		map[string]mesh.AffineMatrix{"vac1": mesh.Identity()},
		"vac1",
	)
	// Should not panic; colors map unchanged
	before := len(renderer.Colors)
	applyConfigColors(renderer, nil)
	if len(renderer.Colors) != before {
		t.Errorf("applyConfigColors with nil config mutated Colors: len before=%d after=%d", before, len(renderer.Colors))
	}
}

func TestApplyConfigColors_EmptyColor(t *testing.T) {
	renderer := mesh.NewCompositeRenderer(
		map[string]*mesh.ValetudoMap{"vac1": minimalMap()},
		map[string]mesh.AffineMatrix{"vac1": mesh.Identity()},
		"vac1",
	)
	cfg := &mesh.Config{
		Vacuums: []mesh.VacuumConfig{
			{ID: "vac1", Color: ""},
		},
	}
	before := renderer.Colors["vac1"]
	applyConfigColors(renderer, cfg)
	if renderer.Colors["vac1"] != before {
		t.Error("applyConfigColors with empty Color should not overwrite existing color")
	}
}

func TestApplyConfigColors_InvalidHex(t *testing.T) {
	renderer := mesh.NewCompositeRenderer(
		map[string]*mesh.ValetudoMap{"vac1": minimalMap()},
		map[string]mesh.AffineMatrix{"vac1": mesh.Identity()},
		"vac1",
	)
	before := renderer.Colors["vac1"]

	tests := []struct {
		name  string
		color string
	}{
		{"too short", "#FF"},
		{"too long", "#FF00FF00"},
		{"not hex", "#ZZZZZZ"},
		{"hash only short", "FF"},
		{"five chars", "FF00F"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &mesh.Config{
				Vacuums: []mesh.VacuumConfig{
					{ID: "vac1", Color: tt.color},
				},
			}
			applyConfigColors(renderer, cfg)
			if renderer.Colors["vac1"] != before {
				t.Errorf("applyConfigColors with color=%q should not overwrite, but it did", tt.color)
			}
		})
	}
}

func TestApplyConfigColors_ValidHex(t *testing.T) {
	tests := []struct {
		name      string
		hexColor  string
		wantFloor color.NRGBA
		wantWall  color.NRGBA
		wantRobot color.NRGBA
	}{
		{
			name:      "with hash prefix",
			hexColor:  "#FF8040",
			wantFloor: color.NRGBA{R: 255, G: 128, B: 64, A: 150},
			wantWall:  color.NRGBA{R: 127, G: 64, B: 32, A: 255},
			wantRobot: color.NRGBA{R: 255, G: 128, B: 64, A: 255},
		},
		{
			name:      "without hash prefix",
			hexColor:  "00FF00",
			wantFloor: color.NRGBA{R: 0, G: 255, B: 0, A: 150},
			wantWall:  color.NRGBA{R: 0, G: 127, B: 0, A: 255},
			wantRobot: color.NRGBA{R: 0, G: 255, B: 0, A: 255},
		},
		{
			name:      "black",
			hexColor:  "#000000",
			wantFloor: color.NRGBA{R: 0, G: 0, B: 0, A: 150},
			wantWall:  color.NRGBA{R: 0, G: 0, B: 0, A: 255},
			wantRobot: color.NRGBA{R: 0, G: 0, B: 0, A: 255},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := mesh.NewCompositeRenderer(
				map[string]*mesh.ValetudoMap{"vac1": minimalMap()},
				map[string]mesh.AffineMatrix{"vac1": mesh.Identity()},
				"vac1",
			)
			cfg := &mesh.Config{
				Vacuums: []mesh.VacuumConfig{
					{ID: "vac1", Color: tt.hexColor},
				},
			}
			applyConfigColors(renderer, cfg)

			got := renderer.Colors["vac1"]
			if got.Floor != tt.wantFloor {
				t.Errorf("Floor = %v, want %v", got.Floor, tt.wantFloor)
			}
			if got.Wall != tt.wantWall {
				t.Errorf("Wall = %v, want %v", got.Wall, tt.wantWall)
			}
			if got.Robot != tt.wantRobot {
				t.Errorf("Robot = %v, want %v", got.Robot, tt.wantRobot)
			}
		})
	}
}

func TestApplyConfigColors_UnknownVacuum(t *testing.T) {
	// Color is set for a vacuum ID not present in the renderer's Maps; no crash expected.
	renderer := mesh.NewCompositeRenderer(
		map[string]*mesh.ValetudoMap{"vac1": minimalMap()},
		map[string]mesh.AffineMatrix{"vac1": mesh.Identity()},
		"vac1",
	)
	cfg := &mesh.Config{
		Vacuums: []mesh.VacuumConfig{
			{ID: "unknown", Color: "#AABBCC"},
		},
	}
	applyConfigColors(renderer, cfg)
	// "unknown" is written into Colors -- that is fine, just verify no panic and
	// the original vac1 colour is untouched.
	if _, ok := renderer.Colors["vac1"]; !ok {
		t.Error("vac1 color was unexpectedly removed")
	}
}

// ---------------------------------------------------------------------------
// buildTransforms
// ---------------------------------------------------------------------------

func TestBuildTransforms_NilCache(t *testing.T) {
	maps := map[string]*mesh.ValetudoMap{
		"vac1": minimalMap(),
		"vac2": minimalMap(),
	}
	transforms := buildTransforms(maps, nil)

	if len(transforms) != 2 {
		t.Fatalf("expected 2 transforms, got %d", len(transforms))
	}
	identity := mesh.Identity()
	for id, m := range transforms {
		if m != identity {
			t.Errorf("transforms[%q] = %v, want identity %v", id, m, identity)
		}
	}
}

func TestBuildTransforms_WithCache(t *testing.T) {
	customMatrix := mesh.AffineMatrix{A: 2, B: 0, Tx: 10, C: 0, D: 2, Ty: 20}
	cache := &mesh.CalibrationData{
		Vacuums: map[string]mesh.AffineMatrix{
			"vac1": customMatrix,
			// vac2 deliberately missing -- GetTransform returns identity
		},
	}
	maps := map[string]*mesh.ValetudoMap{
		"vac1": minimalMap(),
		"vac2": minimalMap(),
	}

	transforms := buildTransforms(maps, cache)

	if transforms["vac1"] != customMatrix {
		t.Errorf("transforms[vac1] = %v, want %v", transforms["vac1"], customMatrix)
	}
	if transforms["vac2"] != mesh.Identity() {
		t.Errorf("transforms[vac2] = %v, want identity", transforms["vac2"])
	}
}

func TestBuildTransforms_EmptyMaps(t *testing.T) {
	transforms := buildTransforms(map[string]*mesh.ValetudoMap{}, nil)
	if len(transforms) != 0 {
		t.Errorf("expected 0 transforms for empty maps, got %d", len(transforms))
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- /health
// ---------------------------------------------------------------------------

func TestHealth_NoMaps(t *testing.T) {
	handler := newHTTPServer(emptyTracker(), nil, nil, "", 0)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want %d", w.Code, http.StatusOK)
	}

	var body struct {
		Status  string `json:"status"`
		HasMaps bool   `json:"hasMaps"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode /health response: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want %q", body.Status, "ok")
	}
	if body.HasMaps {
		t.Error("hasMaps = true, want false when no maps loaded")
	}
}

func TestHealth_WithMaps(t *testing.T) {
	handler := newHTTPServer(populatedTracker(), nil, nil, "", 0)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want %d", w.Code, http.StatusOK)
	}

	var body struct {
		HasMaps bool `json:"hasMaps"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode /health response: %v", err)
	}
	if !body.HasMaps {
		t.Error("hasMaps = false, want true when maps are loaded")
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- PNG/SVG endpoints with no maps (503 paths)
// ---------------------------------------------------------------------------

func TestEndpoints_NoMaps_503(t *testing.T) {
	handler := newHTTPServer(emptyTracker(), nil, nil, "", 0)

	endpoints := []string{
		"/composite-map.png",
		"/floorplan.png",
		"/live.png",
		"/composite-map.svg",
		"/floorplan.svg",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s status = %d, want %d", ep, w.Code, http.StatusServiceUnavailable)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- PNG endpoints with maps (200 paths)
// ---------------------------------------------------------------------------

func TestCompositeMapPNG_WithMaps(t *testing.T) {
	handler := newHTTPServer(populatedTracker(), nil, nil, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/composite-map.png", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/composite-map.png status = %d, want %d, body=%q", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
	if w.Body.Len() == 0 {
		t.Error("response body is empty; expected PNG data")
	}
}

func TestFloorplanPNG_WithMaps(t *testing.T) {
	handler := newHTTPServer(populatedTracker(), nil, nil, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/floorplan.png", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/floorplan.png status = %d, want %d, body=%q", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}
	if w.Body.Len() == 0 {
		t.Error("response body is empty; expected PNG data")
	}
}

func TestLivePNG_WithMaps(t *testing.T) {
	st := populatedTracker()
	st.UpdatePosition("vac1", 15, 15, 90)

	handler := newHTTPServer(st, nil, nil, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/live.png", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/live.png status = %d, want %d, body=%q", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}
	if w.Body.Len() == 0 {
		t.Error("response body is empty; expected PNG data")
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- SVG endpoints with maps (200 paths)
// ---------------------------------------------------------------------------

func TestCompositeMapSVG_WithMaps(t *testing.T) {
	handler := newHTTPServer(populatedTracker(), nil, nil, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/composite-map.svg", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/composite-map.svg status = %d, want %d, body=%q", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/svg+xml")
	}
	if w.Body.Len() == 0 {
		t.Error("response body is empty; expected SVG data")
	}
}

func TestFloorplanSVG_WithMaps(t *testing.T) {
	handler := newHTTPServer(populatedTracker(), nil, nil, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/floorplan.svg", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/floorplan.svg status = %d, want %d, body=%q", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/svg+xml")
	}
	if w.Body.Len() == 0 {
		t.Error("response body is empty; expected SVG data")
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- SVG endpoints with GridSpacing in config
// ---------------------------------------------------------------------------

func TestCompositeMapSVG_WithGridSpacing(t *testing.T) {
	cfg := &mesh.Config{
		GridSpacing: 500,
	}
	handler := newHTTPServer(populatedTracker(), nil, cfg, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/composite-map.svg", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/composite-map.svg with GridSpacing status = %d, want %d, body=%q", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestFloorplanSVG_WithGridSpacing(t *testing.T) {
	cfg := &mesh.Config{
		GridSpacing: 800,
	}
	handler := newHTTPServer(populatedTracker(), nil, cfg, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/floorplan.svg", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/floorplan.svg with GridSpacing status = %d, want %d, body=%q", w.Code, http.StatusOK, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- reference selection fallback (empty refID)
// ---------------------------------------------------------------------------

func TestEndpoints_EmptyRefID_AutoSelects(t *testing.T) {
	// refID="" forces SelectReferenceVacuum to pick by area; with one map
	// it picks "vac1" automatically.
	handler := newHTTPServer(populatedTracker(), nil, nil, "", 0)

	endpoints := []string{
		"/composite-map.png",
		"/floorplan.png",
		"/live.png",
		"/composite-map.svg",
		"/floorplan.svg",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("%s with empty refID: status = %d, want %d, body=%q", ep, w.Code, http.StatusOK, w.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- with calibration cache
// ---------------------------------------------------------------------------

func TestEndpoints_WithCache(t *testing.T) {
	cache := &mesh.CalibrationData{
		ReferenceVacuum: "vac1",
		Vacuums: map[string]mesh.AffineMatrix{
			"vac1": mesh.Identity(),
		},
	}
	handler := newHTTPServer(populatedTracker(), cache, nil, "vac1", 0)

	endpoints := []string{
		"/composite-map.png",
		"/floorplan.png",
		"/live.png",
		"/composite-map.svg",
		"/floorplan.svg",
	}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("%s with cache: status = %d, want %d", ep, w.Code, http.StatusOK)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- composite-map.png with colors applied from config
// ---------------------------------------------------------------------------

func TestCompositeMapPNG_WithConfigColors(t *testing.T) {
	cfg := &mesh.Config{
		Vacuums: []mesh.VacuumConfig{
			{ID: "vac1", Color: "#3366CC"},
		},
	}
	handler := newHTTPServer(populatedTracker(), nil, cfg, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/composite-map.png", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/composite-map.png with config colors: status = %d, want %d, body=%q", w.Code, http.StatusOK, w.Body.String())
	}
	if w.Body.Len() == 0 {
		t.Error("response body is empty; expected PNG data")
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- live.png with no drawable content but positions present
// ---------------------------------------------------------------------------

func TestLivePNG_NoDrawableContent_WithPositions(t *testing.T) {
	// Map with layers that have no drawable pixel types; HasDrawableContent() returns false.
	// live.png does NOT gate on HasDrawableContent -- it always renders.
	st := mesh.NewStateTracker()
	st.UpdateMap("vac1", &mesh.ValetudoMap{
		MetaData: mesh.MapMetaData{TotalLayerArea: 50},
		Size:     mesh.Size{X: 100, Y: 100},
		Layers: []mesh.MapLayer{
			{Type: "unknown_type", Pixels: []int{5, 5, 1}},
		},
	})
	st.UpdatePosition("vac1", 10, 10, 0)

	handler := newHTTPServer(st, nil, nil, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/live.png", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// live.png always renders (positions only if no drawable content)
	if w.Code != http.StatusOK {
		t.Fatalf("/live.png no-drawable status = %d, want %d, body=%q", w.Code, http.StatusOK, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- composite/floorplan PNG with no drawable content (503)
// ---------------------------------------------------------------------------

func TestCompositeMapPNG_NoDrawableContent_503(t *testing.T) {
	st := mesh.NewStateTracker()
	st.UpdateMap("vac1", &mesh.ValetudoMap{
		MetaData: mesh.MapMetaData{TotalLayerArea: 50},
		Size:     mesh.Size{X: 100, Y: 100},
		Layers: []mesh.MapLayer{
			{Type: "unknown_type", Pixels: []int{5, 5, 1}},
		},
	})

	handler := newHTTPServer(st, nil, nil, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/composite-map.png", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("/composite-map.png no-drawable status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestFloorplanPNG_NoDrawableContent_503(t *testing.T) {
	st := mesh.NewStateTracker()
	st.UpdateMap("vac1", &mesh.ValetudoMap{
		MetaData: mesh.MapMetaData{TotalLayerArea: 50},
		Size:     mesh.Size{X: 100, Y: 100},
		Layers: []mesh.MapLayer{
			{Type: "unknown_type", Pixels: []int{5, 5, 1}},
		},
	})

	handler := newHTTPServer(st, nil, nil, "vac1", 0)
	req := httptest.NewRequest(http.MethodGet, "/floorplan.png", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("/floorplan.png no-drawable status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer -- GlobalRotation is wired through
// ---------------------------------------------------------------------------

func TestEndpoints_WithGlobalRotation(t *testing.T) {
	handler := newHTTPServer(populatedTracker(), nil, nil, "vac1", 90)

	endpoints := []string{"/composite-map.png", "/floorplan.png", "/live.png"}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("%s with rotation=90: status = %d, want %d", ep, w.Code, http.StatusOK)
			}
		})
	}
}
