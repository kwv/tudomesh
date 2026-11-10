package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kwv/tudomesh/mesh"
)

// Per-vacuum endpoints: each robot's room, status and position, and a map
// image centered on it:
//
//	GET /vacuums           -> []vacuumInfo for every known vacuum
//	GET /vacuum/{key}      -> vacuumInfo
//	GET /vacuum/{key}.png  -> live map with that vacuum circled and a caption;
//	                          ?scale=1..8 sets image pixels per map pixel (default 3)
//	GET /vacuum/{key}.svg  -> live SVG with that vacuum and its room highlighted
//
// Both images take ?crop=room (the vacuum's room plus 1 m) or ?crop=4m /
// ?crop=250cm (a square of that radius around the vacuum); see mesh.ParseCrop.
// Without it the whole floor is shown. ?theme=light|dark|auto overrides the
// configured theme (render.theme).
//
// {key} is the vacuum id from config (case-insensitive) or the Valetudo
// identifier from its MQTT topic, e.g. "robo7" for
// valetudo/robo7/MapData/map-data.

type vacuumPosition struct {
	X         float64   `json:"x"` // shared (composite) map coordinates
	Y         float64   `json:"y"`
	Angle     float64   `json:"angle"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type vacuumInfo struct {
	ID         string             `json:"id"`
	Name       string             `json:"name,omitempty"` // topic identifier, e.g. "robo7"
	State      string             `json:"state,omitempty"`
	StateSince *time.Time         `json:"stateSince,omitempty"`
	Room       *mesh.RoomLocation `json:"room,omitempty"`
	Position   *vacuumPosition    `json:"position,omitempty"`
	MapURL     string             `json:"mapUrl"`
}

// topicIdentifier returns the Valetudo identifier from a map-data topic
// (<prefix>/<identifier>/MapData/map-data), or "" if the topic is too short.
func topicIdentifier(topic string) string {
	parts := strings.Split(strings.Trim(topic, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[len(parts)-3]
}

// vacuumIDs returns all known vacuum ids: configured ones plus any with a map.
func vacuumIDs(config *mesh.Config, maps map[string]*mesh.ValetudoMap) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		id = strings.TrimSuffix(id, ".json")
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if config != nil {
		for _, vc := range config.Vacuums {
			add(vc.ID)
		}
	}
	for k := range maps {
		add(k)
	}
	sort.Strings(ids)
	return ids
}

// resolveVacuum maps a request key to a vacuum id and its topic identifier.
func resolveVacuum(key string, config *mesh.Config, maps map[string]*mesh.ValetudoMap) (id, name string, ok bool) {
	if config != nil {
		for _, vc := range config.Vacuums {
			ident := topicIdentifier(vc.Topic)
			if strings.EqualFold(vc.ID, key) || (ident != "" && strings.EqualFold(ident, key)) {
				return vc.ID, ident, true
			}
		}
	}
	for _, id := range vacuumIDs(nil, maps) {
		if strings.EqualFold(id, key) {
			return id, "", true
		}
	}
	return "", "", false
}

// mapFor finds a vacuum's map; file-loaded maps may be keyed "<id>.json".
func mapFor(maps map[string]*mesh.ValetudoMap, id string) (string, *mesh.ValetudoMap) {
	if m, ok := maps[id]; ok {
		return id, m
	}
	if m, ok := maps[id+".json"]; ok {
		return id + ".json", m
	}
	return "", nil
}

func buildVacuumInfo(id, name string, st *mesh.StateTracker) vacuumInfo {
	key := name
	if key == "" {
		key = id
	}
	info := vacuumInfo{ID: id, Name: name, MapURL: "/vacuum/" + key + ".png"}

	if s, ok := st.GetStatus(id); ok {
		info.State = s.State
		since := s.Since
		info.StateSince = &since
	}

	mapKey, m := mapFor(st.GetMaps(), id)
	if m != nil {
		if p, _, ok := mesh.ExtractRobotPosition(m); ok {
			if room, ok := mesh.RoomAt(m, p, 0); ok {
				info.Room = &room
			}
		}
	}

	positions := st.GetPositions()
	pos, ok := positions[id]
	if !ok && mapKey != "" {
		pos, ok = positions[mapKey]
	}
	if ok {
		info.Position = &vacuumPosition{X: pos.X, Y: pos.Y, Angle: pos.Angle, UpdatedAt: pos.Timestamp}
	}
	return info
}

func vacuumCaption(info vacuumInfo) string {
	label := info.ID
	if info.Name != "" {
		label = info.Name
	}
	parts := []string{label}
	if info.Room != nil {
		parts = append(parts, info.Room.Name)
	} else {
		parts = append(parts, "room unknown")
	}
	if info.State != "" {
		parts = append(parts, info.State)
	}
	return strings.Join(parts, " | ")
}

// writeLiveSVG renders the structured live SVG (see mesh.RenderLiveToSVG),
// labelling every vacuum with its name, room and state. highlight is a
// vacuum id or "".
func writeLiveSVG(w http.ResponseWriter, st *mesh.StateTracker, cache *mesh.CalibrationData, config *mesh.Config, refID string, rotateAll float64, highlight string, crop mesh.Crop, theme mesh.Theme) {
	vr, ok := newSVGRenderer(w, st, cache, config, refID, rotateAll)
	if !ok {
		return
	}
	vr.Crop, vr.Theme = crop, theme
	maps := vr.Maps

	positions := st.GetPositions()
	vr.Meta = make(map[string]mesh.VacuumMeta, len(positions))
	for key := range positions {
		id, name, ok := resolveVacuum(strings.TrimSuffix(key, ".json"), config, maps)
		if !ok {
			continue
		}
		info := buildVacuumInfo(id, name, st)
		meta := mesh.VacuumMeta{Name: name, State: info.State}
		if info.Room != nil {
			meta.Room = info.Room.Name
		}
		vr.Meta[key] = meta
	}
	vr.Highlight = highlight
	vr.Calibrated = calibratedVacuums(maps, cache)

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache")
	if err := vr.RenderLiveToSVG(w, positions); err != nil {
		log.Printf("Error encoding live SVG: %v", err)
	}
}

// calibratedVacuums marks the vacuums in maps that have a stored calibration,
// so the alert map only trusts their transforms. Never nil.
func calibratedVacuums(maps map[string]*mesh.ValetudoMap, cache *mesh.CalibrationData) map[string]bool {
	out := make(map[string]bool, len(maps))
	for id := range maps {
		out[id] = cache.GetVacuumCalibration(id) != nil
	}
	return out
}

// alertImageScale parses ?scale=, defaulting to 3 and clamping to 1..8.
func alertImageScale(q string) float64 {
	v, err := strconv.ParseFloat(q, 64)
	if err != nil || v <= 0 {
		return mesh.DefaultAlertScale
	}
	return math.Min(8, math.Max(1, v))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

func registerVacuumHandlers(mux *http.ServeMux, st *mesh.StateTracker, cache *mesh.CalibrationData, config *mesh.Config, refID string, rotateAll float64) {
	mux.HandleFunc("GET /vacuums", func(w http.ResponseWriter, r *http.Request) {
		maps := st.GetMaps()
		out := []vacuumInfo{}
		for _, id := range vacuumIDs(config, maps) {
			_, name, _ := resolveVacuum(id, config, maps)
			out = append(out, buildVacuumInfo(id, name, st))
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("GET /vacuum/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		format := ""
		for _, ext := range []string{".png", ".svg"} {
			if strings.HasSuffix(key, ext) {
				format = ext
				key = strings.TrimSuffix(key, ext)
			}
		}

		maps := st.GetMaps()
		id, name, ok := resolveVacuum(key, config, maps)
		if !ok {
			http.Error(w, fmt.Sprintf("unknown vacuum %q", key), http.StatusNotFound)
			return
		}
		info := buildVacuumInfo(id, name, st)
		if format == "" {
			writeJSON(w, info)
			return
		}
		crop, err := mesh.ParseCrop(r.URL.Query().Get("crop"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		theme, ok := requestTheme(w, r, config)
		if !ok {
			return
		}

		if format == ".svg" {
			highlight := id
			if _, ok := st.GetPositions()[highlight]; !ok {
				if k, _ := mapFor(maps, id); k != "" {
					highlight = k
				}
			}
			writeLiveSVG(w, st, cache, config, refID, rotateAll, highlight, crop, theme)
			return
		}

		if len(maps) == 0 {
			http.Error(w, "No maps available", http.StatusServiceUnavailable)
			return
		}
		effectiveRef := refID
		if effectiveRef == "" {
			effectiveRef = mesh.SelectReferenceVacuum(maps, nil)
		}
		renderer := mesh.NewCompositeRenderer(maps, buildTransforms(maps, cache), effectiveRef)
		renderer.GlobalRotation = rotateAll
		renderer.Scale = alertImageScale(r.URL.Query().Get("scale"))
		applyConfigColors(renderer, config)
		renderer.Calibrated = calibratedVacuums(maps, cache)
		renderer.Crop, renderer.Theme = crop, theme

		positions := st.GetPositions()
		applyLiveOptions(renderer, config, maps, positions)
		highlight := id
		if _, ok := positions[highlight]; !ok {
			if k, _ := mapFor(maps, id); k != "" {
				highlight = k
			}
		}
		img := renderer.RenderLiveHighlight(positions, highlight, vacuumCaption(info))
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-cache")
		if err := png.Encode(w, img); err != nil {
			log.Printf("Error encoding vacuum PNG: %v", err)
		}
	})
}
