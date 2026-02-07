package mesh

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads the unified configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	// Validate required fields
	if config.MQTT.Broker == "" {
		return nil, fmt.Errorf("mqtt.broker is required")
	}
	if len(config.Vacuums) == 0 {
		return nil, fmt.Errorf("at least one vacuum must be defined")
	}

	// Validate vacuum configs
	for i, vc := range config.Vacuums {
		if vc.ID == "" {
			return nil, fmt.Errorf("vacuum[%d].id is required", i)
		}
		if vc.Topic == "" {
			return nil, fmt.Errorf("vacuum[%d].topic is required for %s", i, vc.ID)
		}
	}

	return &config, nil
}

// SaveConfig saves the configuration to a YAML file
func SaveConfig(path string, config *Config) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshaling config YAML: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// MergeCalibrationIntoConfig merges auto-computed calibration data with config
// Config manual overrides take precedence over cached calibration
func MergeCalibrationIntoConfig(config *Config, cache *CalibrationData) map[string]AffineMatrix {
	transforms := make(map[string]AffineMatrix)

	// Start with cached transforms if available
	if cache != nil {
		for id, vc := range cache.Vacuums {
			transforms[id] = vc.Transform
		}
	}

	// Override with manual calibrations from config
	for _, vc := range config.Vacuums {
		if vc.HasManualCalibration() {
			// Build affine matrix from rotation and translation
			rotation := vc.GetRotation()
			translation := vc.GetTranslation()

			// Convert rotation to affine matrix
			transforms[vc.ID] = CreateRotationTranslation(rotation, translation.X, translation.Y)
		}
	}

	return transforms
}

// GetEffectiveReference determines the effective reference vacuum ID
// Priority: config.Reference > cache.ReferenceVacuum > auto-select by area
func GetEffectiveReference(config *Config, cache *CalibrationData, maps map[string]*ValetudoMap) string {
	// Priority 1: Explicit config reference
	if config.Reference != "" {
		if _, ok := maps[config.Reference]; ok {
			return config.Reference
		}
	}

	// Priority 2: Cache reference (if still valid)
	if cache != nil && cache.ReferenceVacuum != "" {
		if _, ok := maps[cache.ReferenceVacuum]; ok {
			return cache.ReferenceVacuum
		}
	}

	// Priority 3: Auto-select by largest area
	return SelectReferenceVacuum(maps, config.Vacuums)
}

// BuildForceRotationMap creates a rotation map from --force-rotation CLI flag format
// Format: "VACUUM_ID=DEGREES,VACUUM_ID2=DEGREES2"
// This is used for CLI overrides
func BuildForceRotationMap(forceRotation string) map[string]float64 {
	rotations := make(map[string]float64)

	if forceRotation == "" {
		return rotations
	}

	// Parse comma-separated specs
	remaining := forceRotation
	for remaining != "" {
		var spec string
		idx := indexOf(remaining, ',')
		if idx == -1 {
			spec = remaining
			remaining = ""
		} else {
			spec = remaining[:idx]
			remaining = remaining[idx+1:]
		}

		// Parse VACUUM_ID=DEGREES
		eqIdx := indexOf(spec, '=')
		if eqIdx == -1 {
			continue
		}

		vacuumID := spec[:eqIdx]
		var degrees float64
		if _, err := fmt.Sscanf(spec[eqIdx+1:], "%f", &degrees); err == nil {
			rotations[vacuumID] = degrees
		}
	}

	return rotations
}

// indexOf returns the index of the first occurrence of sep in s, or -1 if not found
func indexOf(s string, sep byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return i
		}
	}
	return -1
}
