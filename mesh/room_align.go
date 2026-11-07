package mesh

import (
	"log"
	"math"
	"sort"
	"strings"
	"unicode"
)

// MinSharedRooms is the number of same-named rooms two maps need before
// room-based alignment is trusted.
const MinSharedRooms = 3

// maxRoomResidualRatio caps the mean room-center error as a fraction of how
// spread out the rooms are. Above this, the room names probably don't refer
// to the same places and alignment falls back to ICP.
const maxRoomResidualRatio = 0.15

// RoomAlignment describes the result of AlignByRooms.
type RoomAlignment struct {
	Transform    AffineMatrix
	RotationDeg  float64
	SharedRooms  []string // normalized room names used for the fit
	MeanResidual float64  // mean distance between matched room centers after transform, map units
	Spread       float64  // RMS distance of target room centers from their centroid, map units
}

// NormalizeRoomName lowercases and strips non-alphanumerics so "Living Room",
// "livingRoom" and "living_room" all match.
func NormalizeRoomName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// RoomCentroids returns the center of each named segment, keyed by normalized name.
// Unnamed segments and names that appear more than once are skipped.
func RoomCentroids(m *ValetudoMap) map[string]Point {
	out := make(map[string]Point)
	dup := make(map[string]bool)
	for _, layer := range m.Layers {
		if layer.Type != "segment" {
			continue
		}
		key := NormalizeRoomName(layer.MetaData.Name)
		if key == "" || len(layer.Pixels) < 2 {
			continue
		}
		if _, seen := out[key]; seen {
			dup[key] = true
			continue
		}
		out[key] = Centroid(PixelsToPoints(layer.Pixels))
	}
	for k := range dup {
		delete(out, k)
	}
	return out
}

// AlignByRooms computes a rigid transform (rotation + translation) mapping
// source into target by matching the centers of rooms with the same name.
// ok is false when fewer than MinSharedRooms match or the fit is poor.
func AlignByRooms(source, target *ValetudoMap) (RoomAlignment, bool) {
	src := RoomCentroids(source)
	tgt := RoomCentroids(target)

	var names []string
	for k := range src {
		if _, ok := tgt[k]; ok {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	ra := RoomAlignment{SharedRooms: names}
	if len(names) < MinSharedRooms {
		return ra, false
	}

	srcPts := make([]Point, len(names))
	tgtPts := make([]Point, len(names))
	for i, k := range names {
		srcPts[i] = src[k]
		tgtPts[i] = tgt[k]
	}

	ra.Transform = CalculateRigidTransform(srcPts, tgtPts)
	ra.RotationDeg = NormalizeAngle(math.Atan2(ra.Transform.C, ra.Transform.A) * 180 / math.Pi)

	center := Centroid(tgtPts)
	var resid, spread float64
	for i := range names {
		resid += Distance(TransformPoint(srcPts[i], ra.Transform), tgtPts[i])
		d := Distance(tgtPts[i], center)
		spread += d * d
	}
	ra.MeanResidual = resid / float64(len(names))
	ra.Spread = math.Sqrt(spread / float64(len(names)))

	if ra.Spread == 0 || ra.MeanResidual > maxRoomResidualRatio*ra.Spread {
		return ra, false
	}
	return ra, true
}

// alignGeometric picks a transform for AlignMaps / AlignMapsWithRotationHint:
//  1. AlignByGrid (map geometry only). Used if its Confidence >= MinGridConfidence.
//  2. Otherwise AlignByRooms, if the maps share enough named rooms.
//  3. Otherwise the ambiguous grid result, logged as low confidence.
//
// ok is false only when neither method can run (no geometry, no rooms).
// Score is 1 for a confident result and 0.5 for an ambiguous one.
func alignGeometric(source, target *ValetudoMap, rotationHint *float64) (ICPResult, bool) {
	ga, gridOK := AlignByGrid(source, target, GridAlignOptions{RotationHint: rotationHint})
	if gridOK && ga.Confidence >= MinGridConfidence {
		log.Printf("Grid alignment: rotation=%.1f° confidence=%.2f", ga.RotationDeg, ga.Confidence)
		return gridResult(ga, 1), true
	}

	ra, roomOK := AlignByRooms(source, target)
	if roomOK {
		log.Printf("Grid alignment ambiguous (confidence %.2f); using room names: rotation=%.1f° from %v, mean error %.0f",
			ga.Confidence, ra.RotationDeg, ra.SharedRooms, ra.MeanResidual)
		return ICPResult{
			Transform:       ra.Transform,
			Error:           ra.MeanResidual,
			Score:           1,
			InlierFraction:  1,
			Converged:       true,
			InitialRotation: ra.RotationDeg,
		}, true
	}

	if gridOK {
		log.Printf("WARNING: grid alignment ambiguous (rotation=%.1f°, confidence %.2f); set a rotation hint in config to resolve",
			ga.RotationDeg, ga.Confidence)
		return gridResult(ga, 0.5), true
	}
	return ICPResult{}, false
}

func gridResult(ga GridAlignment, score float64) ICPResult {
	return ICPResult{
		Transform:       ga.Transform,
		Error:           1, // non-zero: callers treat 0 as "no result"
		Score:           score,
		InlierFraction:  score,
		Converged:       true,
		InitialRotation: ga.RotationDeg,
	}
}
