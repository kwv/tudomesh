package mesh

import (
	"log"
	"math"
)

// Alignment is the transform that maps one vacuum's map into another's
// coordinates, and how it was found.
type Alignment struct {
	Transform   AffineMatrix
	RotationDeg float64
	// Confidence is the grid search's margin over the best different
	// orientation (see GridAlignment.Confidence). +Inf when a rotation hint
	// was given; 0 when room names decided.
	Confidence float64
	// Method is "grid", "rooms" (grid was ambiguous, room names decided),
	// "grid-ambiguous" (best guess; set a rotation hint), or "fixed-rotation".
	Method string
}

// AlignMaps aligns source to target:
//  1. AlignByGrid over all rotations; used when Confidence >= MinGridConfidence.
//  2. Otherwise AlignByRooms, if the maps share enough named rooms.
//  3. Otherwise the ambiguous grid result, logged with a warning.
//
// ok is false only when the maps have no floor geometry.
func AlignMaps(source, target *ValetudoMap) (Alignment, bool) {
	ga, ok := AlignByGrid(source, target, GridAlignOptions{})
	if ok && ga.Confidence >= MinGridConfidence {
		log.Printf("Grid alignment: rotation=%.1f° confidence=%.2f", ga.RotationDeg, ga.Confidence)
		return fromGrid(ga, "grid"), true
	}

	if ra, roomOK := AlignByRooms(source, target); roomOK {
		log.Printf("Grid alignment ambiguous (confidence %.2f); using room names: rotation=%.1f° from %v, mean error %.1f",
			ga.Confidence, ra.RotationDeg, ra.SharedRooms, ra.MeanResidual)
		return Alignment{Transform: ra.Transform, RotationDeg: ra.RotationDeg, Method: "rooms"}, true
	}

	if ok {
		log.Printf("WARNING: grid alignment ambiguous (rotation=%.1f°, confidence %.2f); set a rotation hint in config to resolve",
			ga.RotationDeg, ga.Confidence)
		return fromGrid(ga, "grid-ambiguous"), true
	}
	return Alignment{Transform: Identity()}, false
}

// AlignMapsWithRotationHint aligns source to target searching within ±20° of
// hintDeg. ok is false when the maps have no floor geometry.
func AlignMapsWithRotationHint(source, target *ValetudoMap, hintDeg float64) (Alignment, bool) {
	ga, ok := AlignByGrid(source, target, GridAlignOptions{RotationHint: &hintDeg})
	if !ok {
		return Alignment{Transform: Identity()}, false
	}
	log.Printf("Grid alignment near hint %.0f°: rotation=%.1f°", hintDeg, ga.RotationDeg)
	return fromGrid(ga, "grid"), true
}

// AlignMapsAtRotation finds the best translation with the rotation fixed at
// rotationDeg (for forced rotations and rotation comparisons).
func AlignMapsAtRotation(source, target *ValetudoMap, rotationDeg float64) (Alignment, bool) {
	ga, ok := AlignByGrid(source, target, GridAlignOptions{RotationHint: &rotationDeg, FixRotation: true})
	if !ok {
		return Alignment{Transform: Identity()}, false
	}
	return fromGrid(ga, "fixed-rotation"), true
}

func fromGrid(ga GridAlignment, method string) Alignment {
	return Alignment{Transform: ga.Transform, RotationDeg: ga.RotationDeg, Confidence: ga.Confidence, Method: method}
}

// TransformRotationDeg returns the rotation of a transform in [0, 360).
func TransformRotationDeg(t AffineMatrix) float64 {
	return NormalizeAngle(math.Atan2(t.C, t.A) * 180 / math.Pi)
}
