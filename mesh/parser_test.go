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
