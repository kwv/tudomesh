package mesh

import (
	"sync"
	"time"
)

// LivePosition represents a vacuum's current position in world coordinates
type LivePosition struct {
	VacuumID  string    `json:"vacuumId"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	Angle     float64   `json:"angle"` // degrees, 0 = East, CCW
	Timestamp time.Time `json:"timestamp"`
	Color     string    `json:"color"` // hex color for this vacuum
}

// StateTracker tracks live vacuum positions for HTTP endpoints
type StateTracker struct {
	mu        sync.RWMutex
	positions map[string]*LivePosition
	maps      map[string]*ValetudoMap
	colors    map[string]string // vacuum ID -> hex color
	status    map[string]VacuumStatus
}

// NewStateTracker creates a new state tracker
func NewStateTracker() *StateTracker {
	return &StateTracker{
		positions: make(map[string]*LivePosition),
		maps:      make(map[string]*ValetudoMap),
		colors:    make(map[string]string),
		status:    make(map[string]VacuumStatus),
	}
}

// SetColor sets the color for a vacuum
func (st *StateTracker) SetColor(vacuumID, hexColor string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.colors[vacuumID] = hexColor
}

// UpdatePosition updates a vacuum's position
func (st *StateTracker) UpdatePosition(vacuumID string, x, y, angle float64) {
	st.mu.Lock()
	defer st.mu.Unlock()

	color := st.colors[vacuumID]
	if color == "" {
		color = "#FF0000" // default red
	}

	st.positions[vacuumID] = &LivePosition{
		VacuumID:  vacuumID,
		X:         x,
		Y:         y,
		Angle:     angle,
		Timestamp: time.Now(),
		Color:     color,
	}
}

// VacuumStatus is the last Valetudo status value seen for a vacuum
// (e.g. "docked", "cleaning", "error") and when it last changed.
type VacuumStatus struct {
	State string    `json:"state"`
	Since time.Time `json:"since"`
}

// UpdateStatus records a vacuum's status. Since only moves when the value changes.
func (st *StateTracker) UpdateStatus(vacuumID, state string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if cur, ok := st.status[vacuumID]; ok && cur.State == state {
		return
	}
	st.status[vacuumID] = VacuumStatus{State: state, Since: time.Now()}
}

// GetStatus returns the last recorded status for a vacuum.
func (st *StateTracker) GetStatus(vacuumID string) (VacuumStatus, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	s, ok := st.status[vacuumID]
	return s, ok
}

// UpdateMap stores the latest map data for a vacuum
func (st *StateTracker) UpdateMap(vacuumID string, m *ValetudoMap) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.maps[vacuumID] = m
}

// GetPositions returns all current positions
func (st *StateTracker) GetPositions() map[string]*LivePosition {
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make(map[string]*LivePosition)
	for k, v := range st.positions {
		copy := *v
		result[k] = &copy
	}
	return result
}

// GetMaps returns all current maps
func (st *StateTracker) GetMaps() map[string]*ValetudoMap {
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make(map[string]*ValetudoMap)
	for k, v := range st.maps {
		result[k] = v
	}
	return result
}

// HasMaps returns true if we have at least one map
func (st *StateTracker) HasMaps() bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.maps) > 0
}
