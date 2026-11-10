package mesh

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ValetudoMap represents the root map structure from Valetudo JSON export
type ValetudoMap struct {
	Class     string      `json:"__class"`
	MetaData  MapMetaData `json:"metaData"`
	Size      Size        `json:"size"`
	PixelSize int         `json:"pixelSize"`
	Layers    []MapLayer  `json:"layers"`
	Entities  []MapEntity `json:"entities"`
}

// MapMetaData contains map metadata
type MapMetaData struct {
	VendorMapID    int    `json:"vendorMapId,omitempty"`
	Version        int    `json:"version"`
	Nonce          string `json:"nonce"`
	TotalLayerArea int    `json:"totalLayerArea"`
}

// Size represents map dimensions in pixels
type Size struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// MapLayer represents a floor/segment/wall layer
type MapLayer struct {
	Class    string        `json:"__class"`
	MetaData LayerMetaData `json:"metaData"`
	Type     string        `json:"type"` // "floor", "segment", "wall"
	// Pixels are grid indices [x1,y1,x2,y2,...]; multiply by PixelSize for
	// entity units. Rebuilt from CompressedPixels on parse when present.
	Pixels []int `json:"pixels"`
	// CompressedPixels is Valetudo's run-length form: [gridX, gridY, runLength, ...].
	CompressedPixels []int `json:"compressedPixels,omitempty"`
}

// LayerMetaData contains layer metadata
type LayerMetaData struct {
	SegmentID  string `json:"segmentId,omitempty"`
	Name       string `json:"name,omitempty"`
	Area       int    `json:"area"`
	Active     bool   `json:"active,omitempty"`
	Source     string `json:"source,omitempty"`
	PixelCount int    `json:"pixelCount,omitempty"`
}

// MapEntity represents a map entity (robot position, charger, path)
type MapEntity struct {
	Class    string                 `json:"__class"`
	MetaData map[string]interface{} `json:"metaData"`
	Points   []int                  `json:"points"`
	Type     string                 `json:"type"` // "robot_position", "charger_location", "path"
}

// Point represents a 2D coordinate
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// AffineMatrix for 2D transforms: x' = ax + by + tx, y' = cx + dy + ty
type AffineMatrix struct {
	A  float64 `json:"a"`
	B  float64 `json:"b"`
	Tx float64 `json:"tx"`
	C  float64 `json:"c"`
	D  float64 `json:"d"`
	Ty float64 `json:"ty"`
}

// Identity returns an identity matrix (no transformation)
func Identity() AffineMatrix {
	return AffineMatrix{A: 1, B: 0, Tx: 0, C: 0, D: 1, Ty: 0}
}

// VacuumPosition represents a vacuum's position in world coordinates
type VacuumPosition struct {
	VacuumID  string  `json:"vacuumId"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Angle     float64 `json:"angle"`
	Timestamp int64   `json:"timestamp"`
}

// VacuumState tracks full state for a vacuum
type VacuumState struct {
	ID              string       `json:"id"`
	Position        Point        `json:"position"`
	Angle           float64      `json:"angle"`
	ChargerPosition Point        `json:"chargerPosition"`
	LastUpdate      int64        `json:"lastUpdate"`
	IsOnline        bool         `json:"isOnline"`
	TotalLayerArea  int          `json:"totalLayerArea"`
	MapSize         Size         `json:"mapSize"`
	Calibration     AffineMatrix `json:"calibration"`
}

// TranslationOffset represents a 2D translation offset for calibration
type TranslationOffset struct {
	X float64 `yaml:"x" json:"x"`
	Y float64 `yaml:"y" json:"y"`
}

// VacuumConfig defines a vacuum from config file. Usually only URL (and
// optionally Color) is set; ResolveVacuums fills ID, Topic and ApiURL from
// the robot's Valetudo API.
type VacuumConfig struct {
	URL         string             `yaml:"url,omitempty" json:"url,omitempty"` // Valetudo base URL, e.g. http://robo2.home
	ID          string             `yaml:"id,omitempty" json:"id,omitempty"`   // default: Valetudo systemId
	Topic       string             `yaml:"topic,omitempty" json:"topic,omitempty"`
	Color       string             `yaml:"color,omitempty" json:"color,omitempty"`
	Rotation    *float64           `yaml:"rotation,omitempty" json:"rotation,omitempty"`       // Optional rotation hint/override (0, 90, 180, 270)
	Translation *TranslationOffset `yaml:"translation,omitempty" json:"translation,omitempty"` // Optional manual translation override
	ApiURL      *string            `yaml:"apiUrl,omitempty" json:"apiUrl,omitempty"`           // Optional API URL for fetching map data
}

// Config represents the full configuration file
type Config struct {
	MQTT        MQTTConfig     `yaml:"mqtt" json:"mqtt"`
	Reference   string         `yaml:"reference,omitempty" json:"reference,omitempty"` // Optional reference vacuum ID
	Vacuums     []VacuumConfig `yaml:"vacuums" json:"vacuums"`
	GridSpacing float64        `yaml:"gridSpacing,omitempty" json:"gridSpacing,omitempty"` // Grid line spacing in cm (default 100 = 1 m)
	// IgnoreRooms lists room names drawn as outside on the live maps (no fill,
	// label or alert highlight), e.g. a porch. Matched by IsIgnoredRoom.
	IgnoreRooms []string     `yaml:"ignoreRooms,omitempty" json:"ignoreRooms,omitempty"`
	Render      RenderConfig `yaml:"render,omitempty" json:"render,omitempty"`
}

// RenderConfig holds optional image rendering settings.
type RenderConfig struct {
	// FontPath is a .ttf/.otf/.ttc font used for PNG text before the bundled
	// Go Regular (Latin, Greek, Cyrillic), e.g. a CJK font. See SetFontFile.
	FontPath string `yaml:"fontPath,omitempty" json:"fontPath,omitempty"`
	// Theme is the default theme of the live and alert maps: light (default),
	// dark, or auto (SVG follows the viewer; PNG is light). ?theme= overrides it.
	Theme string `yaml:"theme,omitempty" json:"theme,omitempty"`
	// Colors overrides single theme colors, for every theme.
	Colors ThemeColors `yaml:"colors,omitempty" json:"colors,omitempty"`
}

// ResolveTheme returns the theme named name (the configured one if "")
// with the configured color overrides. c may be nil.
func (c *Config) ResolveTheme(name string) (Theme, error) {
	var rc RenderConfig
	if c != nil {
		rc = c.Render
	}
	if name == "" {
		name = rc.Theme
	}
	t, err := ThemeByName(name)
	if err != nil {
		return Theme{}, err
	}
	t, err = t.WithColors(rc.Colors)
	if err != nil {
		return Theme{}, fmt.Errorf("render.colors: %w", err)
	}
	return t, nil
}

// MQTTConfig holds MQTT connection settings
type MQTTConfig struct {
	Broker   string `yaml:"broker,omitempty" json:"broker,omitempty"`     // default: the broker the robots use
	ClientID string `yaml:"clientId,omitempty" json:"clientId,omitempty"` // default: tudomesh-<hostname>
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Password string `yaml:"password,omitempty" json:"password,omitempty"`
}

// GetVacuumByID returns the vacuum config for the given ID
func (c *Config) GetVacuumByID(id string) *VacuumConfig {
	for i := range c.Vacuums {
		if c.Vacuums[i].ID == id {
			return &c.Vacuums[i]
		}
	}
	return nil
}

// GetReference returns the reference vacuum ID from config or empty string
func (c *Config) GetReference() string {
	return c.Reference
}

// HasManualCalibration returns true if the vacuum has manual rotation or translation overrides
func (vc *VacuumConfig) HasManualCalibration() bool {
	return vc.Rotation != nil || vc.Translation != nil
}

// GetRotation returns the rotation value or 0 if not set
func (vc *VacuumConfig) GetRotation() float64 {
	if vc.Rotation != nil {
		return *vc.Rotation
	}
	return 0
}

// GetTranslation returns the translation value or (0,0) if not set
func (vc *VacuumConfig) GetTranslation() TranslationOffset {
	if vc.Translation != nil {
		return *vc.Translation
	}
	return TranslationOffset{X: 0, Y: 0}
}

// VacuumCalibration stores per-vacuum calibration metadata alongside the transform.
type VacuumCalibration struct {
	Transform            AffineMatrix `json:"transform"`
	LastUpdated          int64        `json:"lastUpdated"`
	MapAreaAtCalibration int          `json:"mapAreaAtCalibration"`
}

// CalibrationData stores calibration matrices for all vacuums.
// This is the auto-computed alignment transform cache stored as JSON.
//
// In service mode one instance is shared by the HTTP handlers, the MQTT
// position handler and the AutoCalibrator. Use the methods (which lock) rather
// than the fields once it is shared; direct field access is fine while a
// single goroutine owns it (loading, CLI batch modes).
type CalibrationData struct {
	ReferenceVacuum string                       `json:"referenceVacuum"`
	Vacuums         map[string]VacuumCalibration `json:"vacuums"`
	LastUpdated     int64                        `json:"lastUpdated"`

	mu sync.RWMutex
}

// UnmarshalJSON provides backward compatibility with old cache files where
// Vacuums was map[string]AffineMatrix (no VacuumCalibration wrapper).
// It probes the raw JSON to detect the format and falls back to the legacy
// format when the vacuum entries lack a "transform" key.
func (c *CalibrationData) UnmarshalJSON(data []byte) error {
	// Step 1: Unmarshal the envelope with raw vacuum entries.
	var envelope struct {
		ReferenceVacuum string                     `json:"referenceVacuum"`
		Vacuums         map[string]json.RawMessage `json:"vacuums"`
		LastUpdated     int64                      `json:"lastUpdated"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}

	c.ReferenceVacuum = envelope.ReferenceVacuum
	c.LastUpdated = envelope.LastUpdated

	if len(envelope.Vacuums) == 0 {
		c.Vacuums = make(map[string]VacuumCalibration)
		return nil
	}

	// Step 2: Detect format by probing the first entry for a "transform" key.
	isNewFormat := false
	for _, raw := range envelope.Vacuums {
		var probe struct {
			Transform *json.RawMessage `json:"transform"`
		}
		if err := json.Unmarshal(raw, &probe); err == nil && probe.Transform != nil {
			isNewFormat = true
		}
		break
	}

	c.Vacuums = make(map[string]VacuumCalibration, len(envelope.Vacuums))

	if isNewFormat {
		for id, raw := range envelope.Vacuums {
			var vc VacuumCalibration
			if err := json.Unmarshal(raw, &vc); err != nil {
				return err
			}
			c.Vacuums[id] = vc
		}
	} else {
		// Legacy format: bare AffineMatrix values.
		for id, raw := range envelope.Vacuums {
			var m AffineMatrix
			if err := json.Unmarshal(raw, &m); err != nil {
				return err
			}
			c.Vacuums[id] = VacuumCalibration{
				Transform:   m,
				LastUpdated: envelope.LastUpdated,
			}
		}
	}

	return nil
}
