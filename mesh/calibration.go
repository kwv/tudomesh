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
// This cache stores ICP-computed AffineMatrix transforms
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

	// Update timestamp
	cal.LastUpdated = time.Now().Unix()

	data, err := json.MarshalIndent(cal, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling calibration data: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing calibration file: %w", err)
	}

	return nil
}

// CalibrateVacuums performs auto-calibration for all vacuums against the reference
// Returns the calibration data with transformation matrices
func CalibrateVacuums(maps map[string]*ValetudoMap, referenceID string) (*CalibrationData, error) {
	referenceMap, ok := maps[referenceID]
	if !ok {
		return nil, fmt.Errorf("reference vacuum %q not found", referenceID)
	}

	cal := &CalibrationData{
		ReferenceVacuum: referenceID,
		Vacuums:         make(map[string]AffineMatrix),
		LastUpdated:     time.Now().Unix(),
	}

	// Reference vacuum gets identity transform
	cal.Vacuums[referenceID] = Identity()

	// Align other vacuums to reference
	for id, vacuumMap := range maps {
		if id == referenceID {
			continue
		}

		transform, err := AlignToReference(vacuumMap, referenceMap)
		if err != 0 && err < 100 { // Reasonable error threshold
			cal.Vacuums[id] = transform
		} else {
			// Fallback to quick charger-based alignment
			cal.Vacuums[id] = QuickAlign(vacuumMap, referenceMap)
		}
	}

	return cal, nil
}

// SelectReferenceVacuum auto-selects the best reference vacuum
// based on total layer area (largest map coverage)
// configs parameter is deprecated and ignored (kept for compatibility)
func SelectReferenceVacuum(maps map[string]*ValetudoMap, configs []VacuumConfig) string {
	// Auto-select by largest totalLayerArea
	var bestID string
	var maxArea int

	for id, m := range maps {
		if m.MetaData.TotalLayerArea > maxArea {
			maxArea = m.MetaData.TotalLayerArea
			bestID = id
		}
	}

	return bestID
}

// GetTransform retrieves the transformation matrix for a vacuum
// Returns identity if not found
func (c *CalibrationData) GetTransform(vacuumID string) AffineMatrix {
	if c == nil || c.Vacuums == nil {
		return Identity()
	}
	if m, ok := c.Vacuums[vacuumID]; ok {
		return m
	}
	return Identity()
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
	if c == nil || c.LastUpdated == 0 {
		return true
	}
	return time.Since(time.Unix(c.LastUpdated, 0)) > maxAge
}
