package mesh

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// DefaultMinCalibrationInterval is the minimum time between calibrations
	// for the same vacuum (debounce).
	DefaultMinCalibrationInterval = 30 * time.Minute
)

// AutoCalibrator orchestrates automatic calibration when a vacuum docks.
// It debounces frequent docking events, fetches a fresh map from the robot's
// HTTP API, validates the map, aligns it to the reference, and persists the result.
type AutoCalibrator struct {
	config       *Config
	cache        *CalibrationData
	cachePath    string
	dataDir      string
	stateTracker *StateTracker

	// mu guards lastCalibrated, inFlight and cache-file writes. It is never
	// held during network fetches or alignment, so one slow robot does not
	// delay the others. cache has its own lock.
	mu             sync.Mutex
	lastCalibrated map[string]time.Time
	inFlight       map[string]bool
}

// NewAutoCalibrator creates an AutoCalibrator ready to handle docking events.
func NewAutoCalibrator(config *Config, cache *CalibrationData, cachePath string, dataDir string, st *StateTracker) *AutoCalibrator {
	if cache == nil {
		cache = &CalibrationData{
			Vacuums: make(map[string]VacuumCalibration),
		}
	}
	return &AutoCalibrator{
		config:         config,
		cache:          cache,
		cachePath:      cachePath,
		dataDir:        dataDir,
		stateTracker:   st,
		lastCalibrated: make(map[string]time.Time),
		inFlight:       make(map[string]bool),
	}
}

// OnDockingEvent is the DockingHandler callback registered with the MQTT client.
// It is safe to call from any goroutine.
func (ac *AutoCalibrator) OnDockingEvent(vacuumID string) {
	log.Printf("[AUTO-CAL] Docking event received for %s", vacuumID)
	vc, ok := ac.begin(vacuumID)
	if !ok {
		return
	}
	defer ac.finish(vacuumID)

	// --- Fetch fresh map from the robot's HTTP API ---
	log.Printf("[AUTO-CAL] %s: fetching map from %s", vacuumID, *vc.ApiURL)
	freshMap, err := FetchMapFromAPI(*vc.ApiURL)
	if err != nil {
		log.Printf("[AUTO-CAL] %s: failed to fetch map: %v (preserving existing calibration)", vacuumID, err)
		return
	}

	// Save fetched map to data-dir for persistence (same convention as MQTT handler).
	if ac.dataDir != "" {
		savePath := filepath.Join(ac.dataDir, fmt.Sprintf("ValetudoMapExport-%s.json", vacuumID))
		jsonBytes, err := json.MarshalIndent(freshMap, "", "  ")
		if err != nil {
			log.Printf("[AUTO-CAL] %s: failed to marshal map for saving: %v", vacuumID, err)
		} else if err := os.WriteFile(savePath, jsonBytes, 0644); err != nil {
			log.Printf("[AUTO-CAL] %s: failed to save map to %s: %v", vacuumID, savePath, err)
		} else {
			log.Printf("[AUTO-CAL] %s: saved HTTP-fetched map to %s", vacuumID, savePath)
		}
	}

	// --- Validate map completeness ---
	if err := ValidateMapForCalibration(freshMap); err != nil {
		log.Printf("[AUTO-CAL] %s: map validation failed: %v (preserving existing calibration)", vacuumID, err)
		return
	}
	log.Printf("[AUTO-CAL] %s: map validated (area=%d, layers=%d, entities=%d)",
		vacuumID, freshMap.MetaData.TotalLayerArea, len(freshMap.Layers), len(freshMap.Entities))

	// Update the state tracker with the fresh map so it is available for rendering.
	ac.stateTracker.UpdateMap(vacuumID, freshMap)

	// --- Determine reference vacuum ---
	referenceID := ac.resolveReference()
	if referenceID == "" {
		log.Printf("[AUTO-CAL] %s: no reference vacuum available, skipping", vacuumID)
		return
	}

	// If the docked vacuum IS the reference, we just need to update its entry.
	if vacuumID == referenceID {
		log.Printf("[AUTO-CAL] %s: is the reference vacuum, updating identity entry", vacuumID)
		ac.cache.UpdateVacuumCalibration(vacuumID, VacuumCalibration{
			Transform:            Identity(),
			LastUpdated:          time.Now().Unix(),
			MapAreaAtCalibration: freshMap.MetaData.TotalLayerArea,
		})
		ac.persistAndRecord(vacuumID)
		return
	}

	// --- Get reference map ---
	allMaps := ac.stateTracker.GetMaps()
	refMap, ok := allMaps[referenceID]
	if !ok {
		log.Printf("[AUTO-CAL] %s: reference vacuum %s has no map data, skipping", vacuumID, referenceID)
		return
	}

	// --- Align against the reference ---
	var result Alignment
	var aligned bool
	if vc.Rotation != nil {
		result, aligned = AlignMapsWithRotationHint(freshMap, refMap, *vc.Rotation)
	} else {
		result, aligned = AlignMaps(freshMap, refMap)
	}
	if !aligned {
		log.Printf("[AUTO-CAL] %s: map has no floor geometry; keeping previous calibration", vacuumID)
		return
	}
	log.Printf("[AUTO-CAL] %s: aligned against %s: rotation=%.1f° method=%s confidence=%.2f",
		vacuumID, referenceID, result.RotationDeg, result.Method, result.Confidence)

	transform := result.Transform

	// Apply manual translation if configured.
	if vc.Translation != nil && (vc.Translation.X != 0 || vc.Translation.Y != 0) {
		transform = MultiplyMatrices(Translation(vc.Translation.X, vc.Translation.Y), transform)
		log.Printf("[AUTO-CAL] %s: applied manual translation (%.0f, %.0f)",
			vacuumID, vc.Translation.X, vc.Translation.Y)
	}

	// --- Update cache ---
	ac.cache.SetReference(referenceID)
	ac.cache.UpdateVacuumCalibration(vacuumID, VacuumCalibration{
		Transform:            transform,
		LastUpdated:          time.Now().Unix(),
		MapAreaAtCalibration: freshMap.MetaData.TotalLayerArea,
	})

	ac.persistAndRecord(vacuumID)
}

// begin runs the cheap checks under the lock (debounce, already running,
// config) and marks the vacuum in flight. ok is false if it should be skipped.
func (ac *AutoCalibrator) begin(vacuumID string) (*VacuumConfig, bool) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.inFlight[vacuumID] {
		log.Printf("[AUTO-CAL] %s: calibration already in progress, skipping", vacuumID)
		return nil, false
	}
	if last, ok := ac.lastCalibrated[vacuumID]; ok && time.Since(last) < DefaultMinCalibrationInterval {
		log.Printf("[AUTO-CAL] %s: skipping, last calibrated %s ago (min interval %s)",
			vacuumID, time.Since(last).Round(time.Second), DefaultMinCalibrationInterval)
		return nil, false
	}
	// Cache-level debounce covers restarts. The map area is not known until
	// the map is fetched, so this fires on time-based expiry or a missing entry.
	if !ac.cache.ShouldRecalibrate(vacuumID, ac.cachedMapArea(vacuumID), DefaultMinCalibrationInterval) {
		log.Printf("[AUTO-CAL] %s: skipping, cache says recalibration not needed", vacuumID)
		return nil, false
	}
	vc := ac.config.GetVacuumByID(vacuumID)
	if vc == nil {
		log.Printf("[AUTO-CAL] %s: vacuum not found in config, skipping", vacuumID)
		return nil, false
	}
	if vc.ApiURL == nil || *vc.ApiURL == "" {
		log.Printf("[AUTO-CAL] %s: no apiUrl configured, skipping auto-calibration", vacuumID)
		return nil, false
	}
	ac.inFlight[vacuumID] = true
	return vc, true
}

func (ac *AutoCalibrator) finish(vacuumID string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	delete(ac.inFlight, vacuumID)
}

// GetCache returns the shared calibration data.
func (ac *AutoCalibrator) GetCache() *CalibrationData {
	return ac.cache
}

// resolveReference determines the reference vacuum ID from config, cache, or auto-selection.
func (ac *AutoCalibrator) resolveReference() string {
	// Priority 1: explicit config
	if ref := ac.config.GetReference(); ref != "" {
		return ref
	}
	// Priority 2: cache
	if ref := ac.cache.Reference(); ref != "" {
		return ref
	}
	// Priority 3: auto-select from available maps
	maps := ac.stateTracker.GetMaps()
	if len(maps) == 0 {
		return ""
	}
	return SelectReferenceVacuum(maps, ac.config.Vacuums)
}

// cachedMapArea returns the map area stored in the calibration cache for the
// given vacuum, or 0 if not present.
func (ac *AutoCalibrator) cachedMapArea(vacuumID string) int {
	vc := ac.cache.GetVacuumCalibration(vacuumID)
	if vc == nil {
		return 0
	}
	return vc.MapAreaAtCalibration
}

// persistAndRecord saves the calibration cache to disk and updates the in-memory
// debounce timestamp.
func (ac *AutoCalibrator) persistAndRecord(vacuumID string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	if err := SaveCalibration(ac.cachePath, ac.cache); err != nil {
		log.Printf("[AUTO-CAL] %s: failed to save calibration cache: %v", vacuumID, err)
	} else {
		log.Printf("[AUTO-CAL] %s: calibration cache saved to %s", vacuumID, ac.cachePath)
	}
	ac.lastCalibrated[vacuumID] = time.Now()
	log.Printf("[AUTO-CAL] %s: calibration complete", vacuumID)
}

// String implements fmt.Stringer for debug logging.
func (ac *AutoCalibrator) String() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return fmt.Sprintf("AutoCalibrator{cachePath=%s, vacuums=%d, lastCalibrated=%d}",
		ac.cachePath, len(ac.cache.Transforms()), len(ac.lastCalibrated))
}
