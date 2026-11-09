package mesh

// VectorRenderer renders vacuum maps as structured SVG (see svg.go).
//
// World units are Valetudo entity units: grid cell × pixelSize. Padding and
// GridSpacing are in world units.
type VectorRenderer struct {
	Maps           map[string]*ValetudoMap
	Transforms     map[string]AffineMatrix // grid -> reference grid, per vacuum
	Colors         map[string]VacuumColor  // per-vacuum colors for the composite view
	Reference      string
	Padding        float64
	GlobalRotation float64               // degrees, applied to the whole view around its center
	GridSpacing    float64               // 0 disables the grid
	Meta           map[string]VacuumMeta // optional per-vacuum labels/state for live SVG
	Highlight      string                // vacuum id to highlight in live SVG
	// IgnoreRooms lists base-map room names drawn as plain floor in the live
	// SVG (see IsIgnoredRoom).
	IgnoreRooms []string
}

// NewVectorRenderer creates a vector renderer with default settings.
func NewVectorRenderer(maps map[string]*ValetudoMap, transforms map[string]AffineMatrix, reference string) *VectorRenderer {
	colors := DefaultColors()
	colorMap := make(map[string]VacuumColor)

	i := 0
	for _, id := range sortedKeys(maps) {
		if id == reference {
			colorMap[id] = colors[0]
		} else {
			colorMap[id] = colors[(i%3)+1]
			i++
		}
	}

	return &VectorRenderer{
		Maps:        maps,
		Transforms:  transforms,
		Colors:      colorMap,
		Reference:   reference,
		Padding:     500.0,
		GridSpacing: 1000.0,
	}
}
