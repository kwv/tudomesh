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
// HTTP API, validates the map, runs ICP alignment, and persists the result.
type AutoCalibrator struct {
	config       *Config
	cache        *CalibrationData
	cachePath    string
	dataDir      string
	stateTracker *StateTracker

	mu             sync.Mutex
	lastCalibrated map[string]time.Time
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
	}
}

// OnDockingEvent is the DockingHandler callback registered with the MQTT client.
// It is safe to call from any goroutine.
func (ac *AutoCalibrator) OnDockingEvent(vacuumID string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	log.Printf("[AUTO-CAL] Docking event received for %s", vacuumID)

	// --- Step 1: Debounce ---
	if last, ok := ac.lastCalibrated[vacuumID]; ok {
		if time.Since(last) < DefaultMinCalibrationInterval {
			log.Printf("[AUTO-CAL] %s: skipping, last calibrated %s ago (min interval %s)",
				vacuumID, time.Since(last).Round(time.Second), DefaultMinCalibrationInterval)
			return
		}
	}

	// Also check the cache-level debounce (covers restarts).
	// We pass 0 for newMapArea here because we haven't fetched the map yet;
	// ShouldRecalibrate will still fire on time-based expiry or missing entry.
	if !ac.cache.ShouldRecalibrate(vacuumID, ac.cachedMapArea(vacuumID), DefaultMinCalibrationInterval) {
		log.Printf("[AUTO-CAL] %s: skipping, cache says recalibration not needed", vacuumID)
		return
	}

	// --- Step 2: Look up vacuum config for API URL ---
	vc := ac.config.GetVacuumByID(vacuumID)
	if vc == nil {
		log.Printf("[AUTO-CAL] %s: vacuum not found in config, skipping", vacuumID)
		return
	}
	if vc.ApiURL == nil || *vc.ApiURL == "" {
		log.Printf("[AUTO-CAL] %s: no apiUrl configured, skipping auto-calibration", vacuumID)
		return
	}

	// --- Step 3: Fetch fresh map from the robot's HTTP API ---
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

	// --- Step 4: Validate map completeness ---
	if err := ValidateMapForCalibration(freshMap); err != nil {
		log.Printf("[AUTO-CAL] %s: map validation failed: %v (preserving existing calibration)", vacuumID, err)
		return
	}
	log.Printf("[AUTO-CAL] %s: map validated (area=%d, layers=%d, entities=%d)",
		vacuumID, freshMap.MetaData.TotalLayerArea, len(freshMap.Layers), len(freshMap.Entities))

	// Update the state tracker with the fresh map so it is available for rendering.
	ac.stateTracker.UpdateMap(vacuumID, freshMap)

	// --- Step 5: Determine reference vacuum ---
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

	// --- Step 6: Get reference map ---
	allMaps := ac.stateTracker.GetMaps()
	refMap, ok := allMaps[referenceID]
	if !ok {
		log.Printf("[AUTO-CAL] %s: reference vacuum %s has no map data, skipping", vacuumID, referenceID)
		return
	}

	// --- Step 7: Run ICP calibration ---
	log.Printf("[AUTO-CAL] %s: running ICP alignment against reference %s", vacuumID, referenceID)

	// Use rotation hint from config if available.
	var result ICPResult
	icpCfg := DefaultICPConfig()
	if vc.Rotation != nil {
		result = AlignMapsWithRotationHint(freshMap, refMap, icpCfg, *vc.Rotation)
		log.Printf("[AUTO-CAL] %s: ICP with rotation hint %.0f: error=%.2f, iterations=%d, converged=%v",
			vacuumID, *vc.Rotation, result.Error, result.Iterations, result.Converged)
	} else {
		result = AlignMaps(freshMap, refMap, icpCfg)
		log.Printf("[AUTO-CAL] %s: ICP full: error=%.2f, iterations=%d, converged=%v",
			vacuumID, result.Error, result.Iterations, result.Converged)
	}

	transform := result.Transform

	// Apply manual translation if configured.
	if vc.Translation != nil && (vc.Translation.X != 0 || vc.Translation.Y != 0) {
		transform = MultiplyMatrices(Translation(vc.Translation.X, vc.Translation.Y), transform)
		log.Printf("[AUTO-CAL] %s: applied manual translation (%.0f, %.0f)",
			vacuumID, vc.Translation.X, vc.Translation.Y)
	}

	// --- Step 8: Update cache ---
	ac.cache.ReferenceVacuum = referenceID
	ac.cache.UpdateVacuumCalibration(vacuumID, VacuumCalibration{
		Transform:            transform,
		LastUpdated:          time.Now().Unix(),
		MapAreaAtCalibration: freshMap.MetaData.TotalLayerArea,
	})

	ac.persistAndRecord(vacuumID)
}

// GetCache returns the current calibration data (for use by the app layer).
func (ac *AutoCalibrator) GetCache() *CalibrationData {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.cache
}

// resolveReference determines the reference vacuum ID from config, cache, or auto-selection.
func (ac *AutoCalibrator) resolveReference() string {
	// Priority 1: explicit config
	if ref := ac.config.GetReference(); ref != "" {
		return ref
	}
	// Priority 2: cache
	if ac.cache != nil && ac.cache.ReferenceVacuum != "" {
		return ac.cache.ReferenceVacuum
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
		ac.cachePath, len(ac.cache.Vacuums), len(ac.lastCalibrated))
}
