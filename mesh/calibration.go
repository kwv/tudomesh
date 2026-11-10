package mesh

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultCalibrationCachePath is the default path for auto-computed calibration cache
const DefaultCalibrationCachePath = ".calibration-cache.json"

// LoadCalibration loads auto-computed calibration data from a JSON cache file
// This cache stores auto-computed AffineMatrix transforms
func LoadCalibration(path string) (*CalibrationData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No calibration file yet
		}
		return nil, fmt.Errorf("reading calibration file: %w", err)
	}

	var cal CalibrationData
	if err := json.Unmarshal(data, &cal); err != nil {
		return nil, fmt.Errorf("parsing calibration file: %w", err)
	}

	return &cal, nil
}

// SaveCalibration saves auto-computed calibration data to a JSON cache file
func SaveCalibration(path string, cal *CalibrationData) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating calibration directory: %w", err)
	}

	cal.mu.Lock()
	cal.LastUpdated = time.Now().Unix()
	data, err := json.MarshalIndent(cal, "", "  ")
	cal.mu.Unlock()
	if err != nil {
		return fmt.Errorf("marshaling calibration data: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("writing calibration file to %s: %w (check directory permissions and Docker user UID)", path, err)
		}
		return fmt.Errorf("writing calibration file: %w", err)
	}

	return nil
}

// SelectReferenceVacuum auto-selects the best reference vacuum
// based on total layer area (largest map coverage)
// configs parameter is deprecated and ignored (kept for compatibility)
func SelectReferenceVacuum(maps map[string]*ValetudoMap, configs []VacuumConfig) string {
	// Auto-select by largest totalLayerArea; ties (including maps with no
	// area metadata) go to the lexicographically smallest id so the choice is
	// stable. Returns "" only when maps is empty.
	bestID := ""
	maxArea := -1
	for id, m := range maps {
		area := 0
		if m != nil {
			area = m.MetaData.TotalLayerArea
		}
		if area > maxArea || (area == maxArea && id < bestID) {
			maxArea = area
			bestID = id
		}
	}
	return bestID
}

// GetTransform retrieves the transformation matrix for a vacuum.
// Returns identity if not found.
func (c *CalibrationData) GetTransform(vacuumID string) AffineMatrix {
	if c == nil {
		return Identity()
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if vc, ok := c.Vacuums[vacuumID]; ok {
		return vc.Transform
	}
	return Identity()
}

// GetVacuumCalibration retrieves the full per-vacuum calibration metadata.
// Returns nil if the vacuum is not calibrated.
func (c *CalibrationData) GetVacuumCalibration(vacuumID string) *VacuumCalibration {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if vc, ok := c.Vacuums[vacuumID]; ok {
		return &vc
	}
	return nil
}

// UpdateVacuumCalibration stores or replaces calibration metadata for a single vacuum.
func (c *CalibrationData) UpdateVacuumCalibration(vacuumID string, cal VacuumCalibration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Vacuums == nil {
		c.Vacuums = make(map[string]VacuumCalibration)
	}
	c.Vacuums[vacuumID] = cal
	// Keep the global LastUpdated in sync for backward-compatible readers.
	if cal.LastUpdated > c.LastUpdated {
		c.LastUpdated = cal.LastUpdated
	}
}

// ShouldRecalibrate returns true when the given vacuum should be recalibrated.
// It triggers recalibration when:
//   - the vacuum has never been calibrated,
//   - the map area has changed (indicating a map reset or significant change), or
//   - more than minInterval has elapsed since the last calibration.
//
// The minInterval acts as a debounce to avoid recalibrating too frequently.
func (c *CalibrationData) ShouldRecalibrate(vacuumID string, newMapArea int, minInterval time.Duration) bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	vc, ok := c.Vacuums[vacuumID]
	c.mu.RUnlock()
	if !ok || vc.LastUpdated == 0 {
		return true // never calibrated
	}
	if vc.MapAreaAtCalibration != newMapArea {
		return true // map changed
	}
	return time.Since(time.Unix(vc.LastUpdated, 0)) > minInterval
}

// Reference returns the reference vacuum id.
func (c *CalibrationData) Reference() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ReferenceVacuum
}

// SetReference sets the reference vacuum id.
func (c *CalibrationData) SetReference(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ReferenceVacuum = id
}

// Transforms returns a copy of every stored transform, keyed by vacuum id.
func (c *CalibrationData) Transforms() map[string]AffineMatrix {
	out := make(map[string]AffineMatrix)
	if c == nil {
		return out
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for id, vc := range c.Vacuums {
		out[id] = vc.Transform
	}
	return out
}

// TransformPosition transforms a vacuum's local position to world coordinates
func (c *CalibrationData) TransformPosition(vacuumID string, pos Point) Point {
	return TransformPoint(pos, c.GetTransform(vacuumID))
}

// CalibrationStatus provides status information about calibration
type CalibrationStatus struct {
	ReferenceVacuum   string            `json:"referenceVacuum"`
	CalibratedVacuums []string          `json:"calibratedVacuums"`
	MissingVacuums    []string          `json:"missingVacuums"`
	LastUpdated       time.Time         `json:"lastUpdated"`
	Errors            map[string]string `json:"errors,omitempty"`
}

// GetStatus returns the current calibration status
func (c *CalibrationData) GetStatus(expectedVacuums []string) CalibrationStatus {
	status := CalibrationStatus{
		Errors: make(map[string]string),
	}

	if c == nil {
		status.MissingVacuums = expectedVacuums
		return status
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	status.ReferenceVacuum = c.ReferenceVacuum
	status.LastUpdated = time.Unix(c.LastUpdated, 0)

	calibrated := make(map[string]bool)
	for id := range c.Vacuums {
		status.CalibratedVacuums = append(status.CalibratedVacuums, id)
		calibrated[id] = true
	}

	for _, id := range expectedVacuums {
		if !calibrated[id] {
			status.MissingVacuums = append(status.MissingVacuums, id)
		}
	}

	return status
}

// NeedsRecalibration checks if calibration should be refreshed
func (c *CalibrationData) NeedsRecalibration(maxAge time.Duration) bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.LastUpdated == 0 {
		return true
	}
	return time.Since(time.Unix(c.LastUpdated, 0)) > maxAge
}
