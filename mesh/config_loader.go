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

	if len(config.Vacuums) == 0 {
		return nil, fmt.Errorf("at least one vacuum must be defined")
	}
	for i, vc := range config.Vacuums {
		if vc.BaseURL() == "" && (vc.ID == "" || vc.Topic == "") {
			return nil, fmt.Errorf("vacuum[%d] needs a url (or id and topic)", i)
		}
	}
	if _, err := config.ResolveTheme(""); err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}

	return &config, nil
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
	if ref := cache.Reference(); ref != "" {
		if _, ok := maps[ref]; ok {
			return ref
		}
	}

	// Priority 3: Auto-select by largest area
	return SelectReferenceVacuum(maps, config.Vacuums)
}
