package mesh

import (
	"errors"
	"testing"
)

// helper to build a minimal valid map with drawable pixels, robot, and charger
func validMap(area int) *ValetudoMap {
	return &ValetudoMap{
		MetaData: MapMetaData{TotalLayerArea: area},
		Layers: []MapLayer{
			{Type: "floor", Pixels: []int{1, 2, 3, 4}},
		},
		Entities: []MapEntity{
			{Type: "robot_position", Points: []int{100, 200}},
			{Type: "charger_location", Points: []int{300, 400}},
		},
	}
}

func TestValidateMapForCalibration(t *testing.T) {
	tests := []struct {
		name    string
		m       *ValetudoMap
		wantErr error
	}{
		{
			name:    "nil map",
			m:       nil,
			wantErr: ErrNilMap,
		},
		{
			name:    "no layers at all",
			m:       &ValetudoMap{},
			wantErr: ErrNoDrawablePixels,
		},
		{
			name: "layers but no pixels",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{}}},
			},
			wantErr: ErrNoDrawablePixels,
		},
		{
			name: "has pixels but no robot",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{1, 2}}},
			},
			wantErr: ErrNoRobotPosition,
		},
		{
			name: "has pixels and robot but no charger",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{1, 2}}},
				Entities: []MapEntity{
					{Type: "robot_position", Points: []int{10, 20}},
				},
			},
			wantErr: ErrNoChargerLocation,
		},
		{
			name: "robot_position with insufficient points",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{1, 2}}},
				Entities: []MapEntity{
					{Type: "robot_position", Points: []int{10}},
				},
			},
			wantErr: ErrNoRobotPosition,
		},
		{
			name: "charger_location with insufficient points",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{1, 2}}},
				Entities: []MapEntity{
					{Type: "robot_position", Points: []int{10, 20}},
					{Type: "charger_location", Points: []int{30}},
				},
			},
			wantErr: ErrNoChargerLocation,
		},
		{
			name:    "fully valid map",
			m:       validMap(1000),
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMapForCalibration(tt.m)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateMapForCalibration() unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateMapForCalibration() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeToMM(t *testing.T) {
	t.Run("converts pixel grid indices to mm", func(t *testing.T) {
		m := &ValetudoMap{
			PixelSize: 5,
			Layers: []MapLayer{
				{Type: "floor", Pixels: []int{10, 20, 30, 40}},
				{Type: "wall", Pixels: []int{5, 15}},
				{Type: "segment", Pixels: []int{100, 200}},
			},
			Entities: []MapEntity{
				{Type: "robot_position", Points: []int{500, 600}},
				{Type: "charger_location", Points: []int{700, 800}},
			},
		}

		NormalizeToMM(m)

		// Floor pixels: 10*5=50, 20*5=100, 30*5=150, 40*5=200
		wantFloor := []int{50, 100, 150, 200}
		if len(m.Layers[0].Pixels) != len(wantFloor) {
			t.Fatalf("floor pixels length = %d, want %d", len(m.Layers[0].Pixels), len(wantFloor))
		}
		for i, v := range m.Layers[0].Pixels {
			if v != wantFloor[i] {
				t.Errorf("floor pixel[%d] = %d, want %d", i, v, wantFloor[i])
			}
		}

		// Wall pixels: 5*5=25, 15*5=75
		wantWall := []int{25, 75}
		for i, v := range m.Layers[1].Pixels {
			if v != wantWall[i] {
				t.Errorf("wall pixel[%d] = %d, want %d", i, v, wantWall[i])
			}
		}

		// Segment pixels: 100*5=500, 200*5=1000
		wantSeg := []int{500, 1000}
		for i, v := range m.Layers[2].Pixels {
			if v != wantSeg[i] {
				t.Errorf("segment pixel[%d] = %d, want %d", i, v, wantSeg[i])
			}
		}

		// Entity points are already mm - must not be modified
		if m.Entities[0].Points[0] != 500 || m.Entities[0].Points[1] != 600 {
			t.Errorf("robot_position changed: got %v, want [500, 600]", m.Entities[0].Points)
		}
		if m.Entities[1].Points[0] != 700 || m.Entities[1].Points[1] != 800 {
			t.Errorf("charger_location changed: got %v, want [700, 800]", m.Entities[1].Points)
		}

		if !m.Normalized {
			t.Error("Normalized flag should be true after NormalizeToMM")
		}
	})

	t.Run("idempotent - second call is no-op", func(t *testing.T) {
		m := &ValetudoMap{
			PixelSize: 5,
			Layers: []MapLayer{
				{Type: "floor", Pixels: []int{10, 20}},
			},
		}

		NormalizeToMM(m)
		// After first call: [50, 100]
		NormalizeToMM(m)
		// Must still be [50, 100], not [250, 500]

		if m.Layers[0].Pixels[0] != 50 || m.Layers[0].Pixels[1] != 100 {
			t.Errorf("double normalization: got %v, want [50, 100]", m.Layers[0].Pixels)
		}
	})

	t.Run("nil map is safe", func(t *testing.T) {
		NormalizeToMM(nil) // must not panic
	})

	t.Run("zero pixelSize is no-op", func(t *testing.T) {
		m := &ValetudoMap{
			PixelSize: 0,
			Layers: []MapLayer{
				{Type: "floor", Pixels: []int{10, 20}},
			},
		}
		NormalizeToMM(m)
		if m.Layers[0].Pixels[0] != 10 || m.Layers[0].Pixels[1] != 20 {
			t.Errorf("zero pixelSize should not modify pixels: got %v", m.Layers[0].Pixels)
		}
	})

	t.Run("empty pixels passes through", func(t *testing.T) {
		m := &ValetudoMap{
			PixelSize: 5,
			Layers: []MapLayer{
				{Type: "floor", Pixels: []int{}},
			},
		}
		NormalizeToMM(m)
		if len(m.Layers[0].Pixels) != 0 {
			t.Errorf("empty pixels should remain empty: got %v", m.Layers[0].Pixels)
		}
	})

	t.Run("compressedPixels decoded to Pixels when Pixels empty", func(t *testing.T) {
		m := &ValetudoMap{
			PixelSize: 5,
			Layers: []MapLayer{
				{Type: "floor", Pixels: nil, CompressedPixels: []int{10, 20, 30, 40}},
			},
		}
		NormalizeToMM(m)

		// CompressedPixels copied to Pixels, then scaled: 10*5=50, 20*5=100, etc.
		want := []int{50, 100, 150, 200}
		if len(m.Layers[0].Pixels) != len(want) {
			t.Fatalf("pixels length = %d, want %d", len(m.Layers[0].Pixels), len(want))
		}
		for i, v := range m.Layers[0].Pixels {
			if v != want[i] {
				t.Errorf("pixel[%d] = %d, want %d", i, v, want[i])
			}
		}
		// CompressedPixels should remain unchanged (original data preserved)
		if m.Layers[0].CompressedPixels[0] != 10 {
			t.Errorf("compressedPixels should not be modified: got %v", m.Layers[0].CompressedPixels)
		}
	})

	t.Run("compressedPixels ignored when Pixels present", func(t *testing.T) {
		m := &ValetudoMap{
			PixelSize: 5,
			Layers: []MapLayer{
				{Type: "floor", Pixels: []int{10, 20}, CompressedPixels: []int{99, 99}},
			},
		}
		NormalizeToMM(m)

		// Pixels should be scaled, compressedPixels ignored
		if m.Layers[0].Pixels[0] != 50 || m.Layers[0].Pixels[1] != 100 {
			t.Errorf("pixels = %v, want [50, 100]", m.Layers[0].Pixels)
		}
	})

	t.Run("metaData area is not modified", func(t *testing.T) {
		m := &ValetudoMap{
			PixelSize: 5,
			MetaData:  MapMetaData{TotalLayerArea: 849175},
			Layers: []MapLayer{
				{Type: "floor", Pixels: []int{10, 20}, MetaData: LayerMetaData{Area: 12345}},
			},
		}
		NormalizeToMM(m)

		if m.MetaData.TotalLayerArea != 849175 {
			t.Errorf("totalLayerArea changed: got %d", m.MetaData.TotalLayerArea)
		}
		if m.Layers[0].MetaData.Area != 12345 {
			t.Errorf("layer area changed: got %d", m.Layers[0].MetaData.Area)
		}
	})
}

// helper to generate a slice of n path points (coordinate values)
func makePathPoints(n int) []int {
	pts := make([]int, n)
	for i := range pts {
		pts[i] = i * 10
	}
	return pts
}

// validMapEntityOnly builds a valid map with no layer pixels but rich entity data.
// This simulates the new Valetudo API format where pixels are empty.
func validMapEntityOnly(area int, pathPoints int) *ValetudoMap {
	entities := []MapEntity{
		{Type: "robot_position", Points: []int{100, 200}},
		{Type: "charger_location", Points: []int{300, 400}},
	}
	if pathPoints > 0 {
		entities = append(entities, MapEntity{Type: "path", Points: makePathPoints(pathPoints)})
	}
	layers := []MapLayer{
		{Type: "floor", MetaData: LayerMetaData{Area: area}},
	}
	return &ValetudoMap{
		MetaData: MapMetaData{TotalLayerArea: area},
		Layers:   layers,
		Entities: entities,
	}
}

func TestHasDrawablePixels(t *testing.T) {
	tests := []struct {
		name string
		m    *ValetudoMap
		want bool
	}{
		{
			name: "nil map",
			m:    nil,
			want: false,
		},
		{
			name: "empty map",
			m:    &ValetudoMap{},
			want: false,
		},
		{
			name: "layers with pixels",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{1, 2}}},
			},
			want: true,
		},
		{
			name: "empty pixels no area no entities",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{}}},
			},
			want: false,
		},
		{
			name: "empty pixels but layer area > 0",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{}, MetaData: LayerMetaData{Area: 169918}}},
			},
			want: true,
		},
		{
			name: "empty pixels but segment area > 0",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "segment", Pixels: []int{}, MetaData: LayerMetaData{Area: 50000}}},
			},
			want: true,
		},
		{
			name: "empty pixels but sufficient path entities",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{}}},
				Entities: []MapEntity{
					{Type: "path", Points: makePathPoints(150)},
				},
			},
			want: true,
		},
		{
			name: "empty pixels and insufficient path entities",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{}}},
				Entities: []MapEntity{
					{Type: "path", Points: makePathPoints(50)},
				},
			},
			want: false,
		},
		{
			name: "empty pixels path at exact threshold",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{}}},
				Entities: []MapEntity{
					{Type: "path", Points: makePathPoints(MinEntityPoints)},
				},
			},
			want: true,
		},
		{
			name: "multiple path entities aggregated",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{}}},
				Entities: []MapEntity{
					{Type: "path", Points: makePathPoints(40)},
					{Type: "path", Points: makePathPoints(40)},
					{Type: "path", Points: makePathPoints(40)},
				},
			},
			want: true, // 40+40+40 = 120 >= MinEntityPoints
		},
		{
			name: "non-path entities do not count",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{}}},
				Entities: []MapEntity{
					{Type: "robot_position", Points: makePathPoints(200)},
				},
			},
			want: false,
		},
		{
			name: "wall layer with area",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "wall", Pixels: []int{}, MetaData: LayerMetaData{Area: 10000}}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasDrawablePixels(tt.m)
			if got != tt.want {
				t.Errorf("HasDrawablePixels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateMapForCalibration_EntityOnly(t *testing.T) {
	tests := []struct {
		name    string
		m       *ValetudoMap
		wantErr error
	}{
		{
			name:    "entity-only map with area and path points",
			m:       validMapEntityOnly(169918, 6970),
			wantErr: nil,
		},
		{
			name:    "entity-only map with area but no path points",
			m:       validMapEntityOnly(169918, 0),
			wantErr: nil, // area > 0 is sufficient
		},
		{
			name:    "entity-only map with path points but no area",
			m:       validMapEntityOnly(0, 6970),
			wantErr: nil, // path points > MinEntityPoints is sufficient
		},
		{
			name: "entity-only map with nothing drawable",
			m: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{}}},
				Entities: []MapEntity{
					{Type: "robot_position", Points: []int{100, 200}},
					{Type: "charger_location", Points: []int{300, 400}},
				},
			},
			wantErr: ErrNoDrawablePixels,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMapForCalibration(tt.m)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateMapForCalibration() unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateMapForCalibration() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsMapComplete(t *testing.T) {
	tests := []struct {
		name          string
		newMap        *ValetudoMap
		lastKnownGood *ValetudoMap
		want          bool
	}{
		{
			name:          "nil new map",
			newMap:        nil,
			lastKnownGood: nil,
			want:          false,
		},
		{
			name:          "valid map no baseline",
			newMap:        validMap(1000),
			lastKnownGood: nil,
			want:          true,
		},
		{
			name:          "valid map with nil baseline",
			newMap:        validMap(500),
			lastKnownGood: nil,
			want:          true,
		},
		{
			name:          "area exactly at threshold",
			newMap:        validMap(800),
			lastKnownGood: validMap(1000),
			want:          true,
		},
		{
			name:          "area above threshold",
			newMap:        validMap(900),
			lastKnownGood: validMap(1000),
			want:          true,
		},
		{
			name:          "area below threshold",
			newMap:        validMap(799),
			lastKnownGood: validMap(1000),
			want:          false,
		},
		{
			name:          "area much larger than baseline",
			newMap:        validMap(2000),
			lastKnownGood: validMap(1000),
			want:          true,
		},
		{
			name:          "baseline has zero area skips ratio check",
			newMap:        validMap(100),
			lastKnownGood: validMap(0),
			want:          true,
		},
		{
			name: "structurally invalid new map with good baseline",
			newMap: &ValetudoMap{
				Layers: []MapLayer{{Type: "floor", Pixels: []int{1, 2}}},
			},
			lastKnownGood: validMap(1000),
			want:          false,
		},
		{
			name:          "entity-only map no baseline",
			newMap:        validMapEntityOnly(169918, 6970),
			lastKnownGood: nil,
			want:          true,
		},
		{
			name:          "entity-only map with baseline area check passes",
			newMap:        validMapEntityOnly(900, 6970),
			lastKnownGood: validMapEntityOnly(1000, 6970),
			want:          true,
		},
		{
			name:          "entity-only map area below threshold",
			newMap:        validMapEntityOnly(799, 6970),
			lastKnownGood: validMapEntityOnly(1000, 6970),
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMapComplete(tt.newMap, tt.lastKnownGood)
			if got != tt.want {
				t.Errorf("IsMapComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}
