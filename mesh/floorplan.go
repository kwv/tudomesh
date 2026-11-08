package mesh

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// Floorplan returns the consensus floorplan (BuildConsensusMap) for the
// current maps and calibration, in the calibration reference's grid: the
// frame live positions use. Vacuums without a calibration transform are left
// out. refID is the reference to use when cal has none ("" picks the largest
// map).
//
// The result is cached and rebuilt only when the calibration changes
// (cal.Version), the reference changes, or the set of calibrated vacuums with
// a map changes; ordinary map updates keep the cached floorplan. Returns nil
// when there are no maps. The returned map must not be modified.
func (st *StateTracker) Floorplan(cal *CalibrationData, refID string) *ValetudoMap {
	maps := st.GetMaps()
	if len(maps) == 0 {
		return nil
	}
	// Transforms map into the calibration's reference, so keep it even when
	// its own map is missing.
	ref := cal.Reference()
	if ref == "" {
		ref = refID
		if _, ok := maps[ref]; !ok {
			ref = SelectReferenceVacuum(maps, nil)
		}
	}
	transforms := cal.Transforms()
	used := map[string]AffineMatrix{}
	if _, ok := maps[ref]; ok {
		used[ref] = Identity()
	}
	for id := range maps {
		if t, ok := transforms[id]; ok && id != ref {
			used[id] = t
		}
	}
	key := fmt.Sprintf("%p/%d/%s/%s", cal, cal.Version(), ref, strings.Join(sortedKeys(used), ","))

	st.fpMu.Lock()
	defer st.fpMu.Unlock()
	if st.floorplan != nil && st.fpKey == key {
		return st.floorplan
	}
	start := time.Now()
	fp := BuildConsensusMap(maps, used, ref)
	if fp == nil {
		return nil
	}
	st.floorplan, st.fpKey = fp, key
	log.Printf("Floorplan rebuilt from %d vacuum(s) in %s (reference %s)", len(used), time.Since(start).Round(time.Millisecond), ref)
	return fp
}
