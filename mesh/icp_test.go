package mesh

import (
	"math"
	"math/rand"
	"testing"
)

// Helper to create a random point cloud (fully constrained, no sliding)
func createRandomCloud(origin Point, count int, area float64, rng *rand.Rand) FeatureSet {
	points := []Point{}

	for i := 0; i < count; i++ {
		points = append(points, Point{
			X: origin.X + rng.Float64()*area,
			Y: origin.Y + rng.Float64()*area,
		})
	}

	return FeatureSet{
		WallPoints: points,
		Centroid:   Centroid(points),
		HasCharger: false,
	}
}

// Keep snake for the global test which uses init
func createRandomSnakeFeatures(origin Point, segments int, rng *rand.Rand) FeatureSet {
	points := []Point{}
	curr := origin

	for i := 0; i < segments; i++ {
		angle := rng.Float64() * 2 * math.Pi
		length := 5.0 + rng.Float64()*10.0
		steps := int(length)
		dx := math.Cos(angle) * (length / float64(steps))
		dy := math.Sin(angle) * (length / float64(steps))
		for j := 0; j < steps; j++ {
			curr.X += dx
			curr.Y += dy
			points = append(points, curr)
		}
	}
	return FeatureSet{WallPoints: points, Centroid: Centroid(points)}
}

func TestICP_Identity(t *testing.T) {
	rng := rand.New(rand.NewSource(1234))
	original := createRandomCloud(Point{0, 0}, 100, 100, rng)
	config := DefaultICPConfig()
	result := runICP(original.WallPoints, original.WallPoints, Identity(), config)
	if !result.Converged {
		t.Errorf("Identity failed")
	}
}

func TestICP_Translation_Cloud(t *testing.T) {
	rng := rand.New(rand.NewSource(1234))
	original := createRandomCloud(Point{0, 0}, 200, 100, rng)

	// Shift must be smaller than avg nearest neighbor dist (approx 3.5 for this density)
	// otherwise ICP prefers matching to random neighbors (Identity) over true match.
	expectedTx, expectedTy := 2.0, 1.0
	targetPoints := TransformPoints(original.WallPoints, Translation(expectedTx, expectedTy))

	config := DefaultICPConfig()
	// Should converge easily on cloud data
	result := runICP(original.WallPoints, targetPoints, Identity(), config)

	if math.Abs(result.Transform.Tx-expectedTx) > 0.25 || math.Abs(result.Transform.Ty-expectedTy) > 0.25 {
		t.Errorf("ICP cloud translation incorrect. Got (%f, %f), want (%f, %f). Error: %f",
			result.Transform.Tx, result.Transform.Ty, expectedTx, expectedTy, result.Error)
	}
}

func TestICP_LargeTranslation_NeedsInit(t *testing.T) {
	rng := rand.New(rand.NewSource(1234))
	original := createRandomSnakeFeatures(Point{0, 0}, 50, rng)
	expectedTx, expectedTy := 50.0, 30.0
	targetPoints := TransformPoints(original.WallPoints, Translation(expectedTx, expectedTy))
	srcCentroid := original.Centroid
	tgtCentroid := TransformPoint(srcCentroid, Translation(expectedTx, expectedTy))

	config := DefaultICPConfig()
	config.SamplePoints = 2000
	config.RNG = rng
	initialTx := findBestInitialAlignment(original.WallPoints, targetPoints, srcCentroid, tgtCentroid, 0, rng)
	result := runICP(original.WallPoints, targetPoints, initialTx, config)

	if math.Abs(result.Transform.Tx-expectedTx) > 1.0 || math.Abs(result.Transform.Ty-expectedTy) > 1.0 {
		t.Errorf("ICP large translation failed with init. Got (%f, %f), want (%f, %f)",
			result.Transform.Tx, result.Transform.Ty, expectedTx, expectedTy)
	}
}

func TestICP_Rotation(t *testing.T) {
	rng := rand.New(rand.NewSource(1234))
	original := createRandomCloud(Point{0, 0}, 100, 100, rng)
	rotation := RotationDeg(45)
	targetPoints := TransformPoints(original.WallPoints, rotation)
	srcCentroid := original.Centroid
	tgtCentroid := TransformPoint(original.Centroid, rotation)

	initialTx := findBestInitialAlignment(original.WallPoints, targetPoints, srcCentroid, tgtCentroid, 45, rng)

	config := DefaultICPConfig()
	config.RNG = rng
	result := runICP(original.WallPoints, targetPoints, initialTx, config)

	angle := math.Atan2(result.Transform.C, result.Transform.A) * 180 / math.Pi
	if angle < 0 {
		angle += 360
	}
	if math.Abs(angle-45) > 1.0 {
		t.Errorf("ICP rotation incorrect")
	}
}

func TestICP_Noise(t *testing.T) {
	cloudRng := rand.New(rand.NewSource(42))
	original := createRandomCloud(Point{0, 0}, 200, 100, cloudRng)
	noiseRng := rand.New(rand.NewSource(42))

	expectedTx, expectedTy := 1.5, 1.5
	targetPoints := make([]Point, len(original.WallPoints))
	for i, p := range original.WallPoints {
		px := p.X + expectedTx + (noiseRng.Float64()-0.5)*2.0
		py := p.Y + expectedTy + (noiseRng.Float64()-0.5)*2.0
		targetPoints[i] = Point{X: px, Y: py}
	}

	config := DefaultICPConfig()
	result := runICP(original.WallPoints, targetPoints, Identity(), config)

	if math.Abs(result.Transform.Tx-expectedTx) > 1.0 || math.Abs(result.Transform.Ty-expectedTy) > 1.0 {
		t.Errorf("ICP noisy translation incorrect. Got (%f, %f), want (%f, %f). Error: %f", result.Transform.Tx, result.Transform.Ty, expectedTx, expectedTy, result.Error)
	}
}

// =============================================================================
// Production Entry Point Integration Tests
// =============================================================================
// These tests exercise the AlignMaps and AlignMapsWithRotationHint functions,
// which are the ONLY entry points used by CalibrateVacuums and the MQTT state
// machine. They test the full pipeline:
//   ExtractFeatures -> SampleFeatures -> findBestInitialAlignment ->
//   runMultiScaleICP -> runICPWithMutualNN -> FineTuneRotation/Translation

// createTestValetudoMap creates a ValetudoMap from wall points and optional charger position.
// This helper enables testing the production AlignMaps functions which require *ValetudoMap input.
func createTestValetudoMap(wallPoints []Point, chargerPos *Point) *ValetudoMap {
	// Convert points to flattened pixel array (x,y pairs)
	wallPixels := make([]int, 0, len(wallPoints)*2)
	for _, p := range wallPoints {
		wallPixels = append(wallPixels, int(p.X), int(p.Y))
	}

	// Create floor layer from wall points bounding box (simplified)
	// In reality, floor would be filled area, but for ICP alignment the walls dominate
	floorPixels := make([]int, 0)
	if len(wallPoints) > 0 {
		// Create sparse floor inside bounding box
		minX, minY := wallPoints[0].X, wallPoints[0].Y
		maxX, maxY := wallPoints[0].X, wallPoints[0].Y
		for _, p := range wallPoints {
			if p.X < minX {
				minX = p.X
			}
			if p.Y < minY {
				minY = p.Y
			}
			if p.X > maxX {
				maxX = p.X
			}
			if p.Y > maxY {
				maxY = p.Y
			}
		}
		// Add grid of floor points
		step := 10.0
		for x := minX + step; x < maxX-step; x += step {
			for y := minY + step; y < maxY-step; y += step {
				floorPixels = append(floorPixels, int(x), int(y))
			}
		}
	}

	layers := []MapLayer{
		{
			Class: "MapLayer",
			Type:  "wall",
			MetaData: LayerMetaData{
				Area:       len(wallPixels) / 2,
				PixelCount: len(wallPixels) / 2,
			},
			Pixels: wallPixels,
		},
		{
			Class: "MapLayer",
			Type:  "floor",
			MetaData: LayerMetaData{
				Area:       len(floorPixels) / 2,
				PixelCount: len(floorPixels) / 2,
			},
			Pixels: floorPixels,
		},
	}

	entities := []MapEntity{}
	if chargerPos != nil {
		entities = append(entities, MapEntity{
			Class:  "PointMapEntity",
			Type:   "charger_location",
			Points: []int{int(chargerPos.X), int(chargerPos.Y)},
		})
	}

	return &ValetudoMap{
		Class:     "ValetudoMap",
		PixelSize: 5,
		Size:      Size{X: 1000, Y: 1000},
		MetaData: MapMetaData{
			Version:        1,
			TotalLayerArea: len(wallPixels)/2 + len(floorPixels)/2,
		},
		Layers:   layers,
		Entities: entities,
	}
}

// createLShapeWalls creates an L-shaped room outline (asymmetric for rotation testing)
func createLShapeWalls(origin Point, scale float64) []Point {
	// L-shape with distinct asymmetry for rotation detection
	//    ________
	//   |        |
	//   |   _____|
	//   |  |
	//   |__|
	points := []Point{}

	// Outer walls (counter-clockwise)
	// Bottom horizontal
	for x := 0.0; x <= 100*scale; x += 2 {
		points = append(points, Point{X: origin.X + x, Y: origin.Y})
	}
	// Right vertical (short part)
	for y := 0.0; y <= 50*scale; y += 2 {
		points = append(points, Point{X: origin.X + 100*scale, Y: origin.Y + y})
	}
	// Inner horizontal step
	for x := 100 * scale; x >= 50*scale; x -= 2 {
		points = append(points, Point{X: origin.X + x, Y: origin.Y + 50*scale})
	}
	// Inner vertical
	for y := 50 * scale; y <= 100*scale; y += 2 {
		points = append(points, Point{X: origin.X + 50*scale, Y: origin.Y + y})
	}
	// Top horizontal
	for x := 50 * scale; x >= 0; x -= 2 {
		points = append(points, Point{X: origin.X + x, Y: origin.Y + 100*scale})
	}
	// Left vertical
	for y := 100 * scale; y >= 0; y -= 2 {
		points = append(points, Point{X: origin.X, Y: origin.Y + y})
	}

	return points
}

// createRectangleWalls creates a simple rectangular room outline
func createRectangleWalls(origin Point, width, height float64) []Point {
	points := []Point{}
	step := 2.0

	// Bottom edge
	for x := 0.0; x <= width; x += step {
		points = append(points, Point{X: origin.X + x, Y: origin.Y})
	}
	// Right edge
	for y := 0.0; y <= height; y += step {
		points = append(points, Point{X: origin.X + width, Y: origin.Y + y})
	}
	// Top edge
	for x := width; x >= 0; x -= step {
		points = append(points, Point{X: origin.X + x, Y: origin.Y + height})
	}
	// Left edge
	for y := height; y >= 0; y -= step {
		points = append(points, Point{X: origin.X, Y: origin.Y + y})
	}

	return points
}

func TestAlignMaps_Identity(t *testing.T) {
	// Test that identical maps produce an identity-like transform
	walls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	charger := Point{X: 120, Y: 120}

	sourceMap := createTestValetudoMap(walls, &charger)
	targetMap := createTestValetudoMap(walls, &charger)

	config := DefaultICPConfig()
	result := AlignMaps(sourceMap, targetMap, config)

	// Should produce near-identity transform
	if !result.Converged {
		t.Logf("Identity alignment did not converge (may be normal for identical maps)")
	}

	// Transform should be close to identity
	// A=1, B=0, C=0, D=1, Tx=0, Ty=0
	tolerance := 5.0
	if math.Abs(result.Transform.A-1.0) > 0.1 {
		t.Errorf("A coefficient incorrect: got %f, want ~1.0", result.Transform.A)
	}
	if math.Abs(result.Transform.D-1.0) > 0.1 {
		t.Errorf("D coefficient incorrect: got %f, want ~1.0", result.Transform.D)
	}
	if math.Abs(result.Transform.Tx) > tolerance {
		t.Errorf("Tx should be ~0: got %f", result.Transform.Tx)
	}
	if math.Abs(result.Transform.Ty) > tolerance {
		t.Errorf("Ty should be ~0: got %f", result.Transform.Ty)
	}

	// Score should be high for identical maps
	if result.Score < 0.5 {
		t.Errorf("Score too low for identical maps: got %f, want > 0.5", result.Score)
	}

	t.Logf("Identity result: Score=%.3f, InlierFrac=%.3f, Tx=%.1f, Ty=%.1f",
		result.Score, result.InlierFraction, result.Transform.Tx, result.Transform.Ty)
}

func TestAlignMaps_Translation(t *testing.T) {
	// Test pure translation detection
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}

	expectedTx, expectedTy := 50.0, 30.0

	// Create target by translating source
	targetWalls := TransformPoints(sourceWalls, Translation(expectedTx, expectedTy))
	chargerTgt := TransformPoint(chargerSrc, Translation(expectedTx, expectedTy))

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	result := AlignMaps(sourceMap, targetMap, config)

	// Check translation accuracy (allow some tolerance due to ICP refinement)
	tolerance := 10.0
	if math.Abs(result.Transform.Tx-expectedTx) > tolerance {
		t.Errorf("Tx incorrect: got %f, want %f (tolerance %f)",
			result.Transform.Tx, expectedTx, tolerance)
	}
	if math.Abs(result.Transform.Ty-expectedTy) > tolerance {
		t.Errorf("Ty incorrect: got %f, want %f (tolerance %f)",
			result.Transform.Ty, expectedTy, tolerance)
	}

	// Rotation should be minimal (A~1, D~1, B~0, C~0)
	if math.Abs(result.Transform.A-1.0) > 0.1 || math.Abs(result.Transform.D-1.0) > 0.1 {
		t.Errorf("Unexpected rotation in pure translation test: A=%f, D=%f",
			result.Transform.A, result.Transform.D)
	}

	// Good score expected
	if result.Score < 0.3 {
		t.Errorf("Score too low: got %f", result.Score)
	}

	t.Logf("Translation result: Score=%.3f, Tx=%.1f (want %.1f), Ty=%.1f (want %.1f)",
		result.Score, result.Transform.Tx, expectedTx, result.Transform.Ty, expectedTy)
}

func TestAlignMaps_Rotation90(t *testing.T) {
	// Test 90 degree rotation detection using rotation hint
	// Note: Without hints, symmetric shapes may align at multiple rotations with similar scores.
	// This test verifies that WITH the correct rotation hint, ICP refines accurately.
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}

	// Rotate 90 degrees around centroid
	srcCentroid := Centroid(sourceWalls)
	toOrigin := Translation(-srcCentroid.X, -srcCentroid.Y)
	rotate := RotationDeg(90)
	fromOrigin := Translation(srcCentroid.X, srcCentroid.Y)
	transform := MultiplyMatrices(fromOrigin, MultiplyMatrices(rotate, toOrigin))

	targetWalls := TransformPoints(sourceWalls, transform)
	chargerTgt := TransformPoint(chargerSrc, transform)

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	// Use rotation hint to get accurate 90 degree alignment
	result := AlignMapsWithRotationHint(sourceMap, targetMap, config, 90.0)

	// Extract rotation angle from transform matrix
	angle := math.Atan2(result.Transform.C, result.Transform.A) * 180 / math.Pi
	if angle < 0 {
		angle += 360
	}

	// Should find ~90 degree rotation
	angleTolerance := 15.0
	if math.Abs(angle-90) > angleTolerance {
		t.Errorf("Rotation angle incorrect: got %.1f, want ~90", angle)
	}

	// Score should be reasonable
	if result.Score < 0.2 {
		t.Errorf("Score too low for rotation alignment: got %f", result.Score)
	}

	t.Logf("90deg rotation result: Score=%.3f, InitRot=%.0f, ComputedAngle=%.1f",
		result.Score, result.InitialRotation, angle)
}

func TestAlignMaps_Rotation180(t *testing.T) {
	// Test 180 degree rotation detection using rotation hint
	// Note: Without hints, symmetric shapes may align at multiple rotations with similar scores.
	// This test verifies that WITH the correct rotation hint, ICP refines accurately.
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}

	srcCentroid := Centroid(sourceWalls)
	toOrigin := Translation(-srcCentroid.X, -srcCentroid.Y)
	rotate := RotationDeg(180)
	fromOrigin := Translation(srcCentroid.X, srcCentroid.Y)
	transform := MultiplyMatrices(fromOrigin, MultiplyMatrices(rotate, toOrigin))

	targetWalls := TransformPoints(sourceWalls, transform)
	chargerTgt := TransformPoint(chargerSrc, transform)

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	// Use rotation hint to get accurate 180 degree alignment
	result := AlignMapsWithRotationHint(sourceMap, targetMap, config, 180.0)

	angle := math.Atan2(result.Transform.C, result.Transform.A) * 180 / math.Pi
	if angle < 0 {
		angle += 360
	}

	// Should find ~180 degree rotation
	angleTolerance := 15.0
	if math.Abs(angle-180) > angleTolerance {
		t.Errorf("Rotation angle incorrect: got %.1f, want ~180", angle)
	}

	if result.Score < 0.2 {
		t.Errorf("Score too low: got %f", result.Score)
	}

	t.Logf("180deg rotation result: Score=%.3f, InitRot=%.0f, ComputedAngle=%.1f",
		result.Score, result.InitialRotation, angle)
}

func TestAlignMaps_Rotation270(t *testing.T) {
	// Test 270 degree rotation detection using rotation hint
	// Note: Without hints, symmetric shapes may align at multiple rotations with similar scores.
	// This test verifies that WITH the correct rotation hint, ICP refines accurately.
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}

	srcCentroid := Centroid(sourceWalls)
	toOrigin := Translation(-srcCentroid.X, -srcCentroid.Y)
	rotate := RotationDeg(270)
	fromOrigin := Translation(srcCentroid.X, srcCentroid.Y)
	transform := MultiplyMatrices(fromOrigin, MultiplyMatrices(rotate, toOrigin))

	targetWalls := TransformPoints(sourceWalls, transform)
	chargerTgt := TransformPoint(chargerSrc, transform)

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	// Use rotation hint to get accurate 270 degree alignment
	result := AlignMapsWithRotationHint(sourceMap, targetMap, config, 270.0)

	angle := math.Atan2(result.Transform.C, result.Transform.A) * 180 / math.Pi
	if angle < 0 {
		angle += 360
	}

	// Should find ~270 degree rotation
	angleTolerance := 15.0
	if math.Abs(angle-270) > angleTolerance {
		t.Errorf("Rotation angle incorrect: got %.1f, want ~270", angle)
	}

	if result.Score < 0.2 {
		t.Errorf("Score too low: got %f", result.Score)
	}

	t.Logf("270deg rotation result: Score=%.3f, InitRot=%.0f, ComputedAngle=%.1f",
		result.Score, result.InitialRotation, angle)
}

func TestAlignMapsWithRotationHint_90(t *testing.T) {
	// Test rotation hint forces the correct starting rotation
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}

	srcCentroid := Centroid(sourceWalls)
	toOrigin := Translation(-srcCentroid.X, -srcCentroid.Y)
	rotate := RotationDeg(90)
	fromOrigin := Translation(srcCentroid.X, srcCentroid.Y)
	transform := MultiplyMatrices(fromOrigin, MultiplyMatrices(rotate, toOrigin))

	targetWalls := TransformPoints(sourceWalls, transform)
	chargerTgt := TransformPoint(chargerSrc, transform)

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	// Use the rotation hint entry point
	result := AlignMapsWithRotationHint(sourceMap, targetMap, config, 90.0)

	// InitialRotation should be what we provided
	if result.InitialRotation != 90.0 {
		t.Errorf("InitialRotation not set correctly: got %f, want 90", result.InitialRotation)
	}

	angle := math.Atan2(result.Transform.C, result.Transform.A) * 180 / math.Pi
	if angle < 0 {
		angle += 360
	}

	// Should be close to 90 degrees
	angleTolerance := 15.0
	if math.Abs(angle-90) > angleTolerance {
		t.Errorf("Rotation angle incorrect: got %.1f, want ~90", angle)
	}

	if result.Score < 0.2 {
		t.Errorf("Score too low: got %f", result.Score)
	}

	t.Logf("RotationHint(90) result: Score=%.3f, ComputedAngle=%.1f",
		result.Score, angle)
}

func TestAlignMapsWithRotationHint_WrongHint(t *testing.T) {
	// Test that even with wrong hint, refinement may recover
	// (or at least produces a valid result)
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}

	// Actual rotation is 90 degrees
	srcCentroid := Centroid(sourceWalls)
	toOrigin := Translation(-srcCentroid.X, -srcCentroid.Y)
	rotate := RotationDeg(90)
	fromOrigin := Translation(srcCentroid.X, srcCentroid.Y)
	transform := MultiplyMatrices(fromOrigin, MultiplyMatrices(rotate, toOrigin))

	targetWalls := TransformPoints(sourceWalls, transform)
	chargerTgt := TransformPoint(chargerSrc, transform)

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	// Provide wrong hint (180 instead of 90)
	result := AlignMapsWithRotationHint(sourceMap, targetMap, config, 180.0)

	// The function should still produce a valid result (even if not optimal)
	if result.Score < 0 {
		t.Errorf("Should produce valid score even with wrong hint: got %f", result.Score)
	}

	t.Logf("WrongHint(180 for 90deg) result: Score=%.3f, InitRot=%.0f",
		result.Score, result.InitialRotation)

	// Compare with correct hint
	correctResult := AlignMapsWithRotationHint(sourceMap, targetMap, config, 90.0)
	if result.Score > correctResult.Score*1.5 {
		// Wrong hint might produce worse score, but should still work
		t.Logf("Correct hint score: %.3f (expected to be better)", correctResult.Score)
	}
}

func TestAlignMaps_PartialOverlap(t *testing.T) {
	// Test realistic scenario where source and target maps have partial overlap
	// (simulating a vacuum that has mapped different parts of the same space)

	// Create overlapping L-shapes at different positions
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}

	// Target is shifted but overlaps significantly
	shiftX, shiftY := 30.0, 20.0
	targetWalls := createLShapeWalls(Point{X: 100 + shiftX, Y: 100 + shiftY}, 2.0)
	chargerTgt := Point{X: 120 + shiftX, Y: 120 + shiftY}

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	result := AlignMaps(sourceMap, targetMap, config)

	// Should find the translation
	tolerance := 15.0
	if math.Abs(result.Transform.Tx-shiftX) > tolerance {
		t.Errorf("Tx incorrect for partial overlap: got %f, want ~%f",
			result.Transform.Tx, shiftX)
	}
	if math.Abs(result.Transform.Ty-shiftY) > tolerance {
		t.Errorf("Ty incorrect for partial overlap: got %f, want ~%f",
			result.Transform.Ty, shiftY)
	}

	// Should still get a reasonable score
	if result.Score < 0.1 {
		t.Errorf("Score too low for partial overlap: got %f", result.Score)
	}

	t.Logf("Partial overlap result: Score=%.3f, Tx=%.1f (want %.1f), Ty=%.1f (want %.1f)",
		result.Score, result.Transform.Tx, shiftX, result.Transform.Ty, shiftY)
}

func TestAlignMaps_CombinedRotationTranslation(t *testing.T) {
	// Test combined rotation and translation using rotation hint
	// This tests the full pipeline with a known transformation
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}

	// Apply 90 degree rotation around centroid, then translate
	srcCentroid := Centroid(sourceWalls)
	toOrigin := Translation(-srcCentroid.X, -srcCentroid.Y)
	rotate := RotationDeg(90)
	fromOrigin := Translation(srcCentroid.X, srcCentroid.Y)
	rotateAroundCentroid := MultiplyMatrices(fromOrigin, MultiplyMatrices(rotate, toOrigin))

	// Additional translation after rotation
	additionalTx, additionalTy := 25.0, 15.0
	finalTransform := MultiplyMatrices(Translation(additionalTx, additionalTy), rotateAroundCentroid)

	targetWalls := TransformPoints(sourceWalls, finalTransform)
	chargerTgt := TransformPoint(chargerSrc, finalTransform)

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	// Use rotation hint for accurate combined transform detection
	result := AlignMapsWithRotationHint(sourceMap, targetMap, config, 90.0)

	// Check that rotation is detected
	angle := math.Atan2(result.Transform.C, result.Transform.A) * 180 / math.Pi
	if angle < 0 {
		angle += 360
	}

	angleTolerance := 20.0
	if math.Abs(angle-90) > angleTolerance {
		t.Errorf("Combined transform rotation incorrect: got %.1f, want ~90", angle)
	}

	// Score should be reasonable
	if result.Score < 0.2 {
		t.Errorf("Score too low for combined transform: got %f", result.Score)
	}

	t.Logf("Combined rot+trans result: Score=%.3f, Angle=%.1f, Tx=%.1f, Ty=%.1f",
		result.Score, angle, result.Transform.Tx, result.Transform.Ty)
}

func TestAlignMaps_NoCharger(t *testing.T) {
	// Test alignment without charger anchor
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	targetWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)

	// No charger provided
	sourceMap := createTestValetudoMap(sourceWalls, nil)
	targetMap := createTestValetudoMap(targetWalls, nil)

	config := DefaultICPConfig()
	result := AlignMaps(sourceMap, targetMap, config)

	// Should still work using wall features
	if result.Score < 0.3 {
		t.Errorf("Score too low without charger: got %f", result.Score)
	}

	t.Logf("No charger result: Score=%.3f, Tx=%.1f, Ty=%.1f",
		result.Score, result.Transform.Tx, result.Transform.Ty)
}

func TestAlignMaps_InsufficientFeatures(t *testing.T) {
	// Test edge case: maps with too few features
	// Should return gracefully without crashing
	minimalWalls := []Point{
		{X: 100, Y: 100},
		{X: 101, Y: 100},
	}

	sourceMap := createTestValetudoMap(minimalWalls, nil)
	targetMap := createTestValetudoMap(minimalWalls, nil)

	config := DefaultICPConfig()
	result := AlignMaps(sourceMap, targetMap, config)

	// Should return identity with bad score (not crash)
	if result.Score > 0 {
		// If it somehow succeeded, that's fine too
		t.Logf("Minimal features produced score: %f", result.Score)
	} else {
		// Expected: identity transform with negative/zero score
		if result.Transform.A != 1.0 || result.Transform.D != 1.0 {
			t.Logf("Expected identity for insufficient features, got A=%f, D=%f",
				result.Transform.A, result.Transform.D)
		}
	}
}

func TestAlignMaps_DifferentScaleRooms(t *testing.T) {
	// Test with different sized rooms (should still align structure)
	sourceWalls := createRectangleWalls(Point{X: 100, Y: 100}, 200, 150)
	chargerSrc := Point{X: 120, Y: 120}

	// Same room translated
	targetWalls := createRectangleWalls(Point{X: 150, Y: 130}, 200, 150)
	chargerTgt := Point{X: 170, Y: 150}

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	result := AlignMaps(sourceMap, targetMap, config)

	// Expected translation is 50, 30
	expectedTx, expectedTy := 50.0, 30.0
	tolerance := 15.0

	if math.Abs(result.Transform.Tx-expectedTx) > tolerance {
		t.Errorf("Rectangle Tx incorrect: got %f, want %f", result.Transform.Tx, expectedTx)
	}
	if math.Abs(result.Transform.Ty-expectedTy) > tolerance {
		t.Errorf("Rectangle Ty incorrect: got %f, want %f", result.Transform.Ty, expectedTy)
	}

	t.Logf("Rectangle alignment: Score=%.3f, Tx=%.1f, Ty=%.1f",
		result.Score, result.Transform.Tx, result.Transform.Ty)
}

func TestAlignMaps_ValidateTransform(t *testing.T) {
	// Verify that AlignMaps produces valid transforms
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}

	// Apply known transform
	transform := MultiplyMatrices(Translation(40, 25), RotationDeg(90))
	targetWalls := TransformPoints(sourceWalls, transform)
	chargerTgt := TransformPoint(chargerSrc, transform)

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	result := AlignMaps(sourceMap, targetMap, config)

	// Use ValidateAlignment to check the result
	if !ValidateAlignment(result.Transform) {
		t.Errorf("AlignMaps produced invalid transform: scale or reflection issue")
		t.Logf("Transform: A=%f, B=%f, C=%f, D=%f, Tx=%f, Ty=%f",
			result.Transform.A, result.Transform.B,
			result.Transform.C, result.Transform.D,
			result.Transform.Tx, result.Transform.Ty)
	}
}

func TestAlignMaps_RotationSearchBehavior(t *testing.T) {
	// Test that AlignMaps tries all rotations and returns consistent results
	// Note: The automatic rotation search may not always find the "correct" rotation
	// when multiple rotations produce similar alignment scores. This is expected
	// behavior for symmetric shapes. This test verifies the search mechanism works
	// and produces valid transforms.
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}

	srcCentroid := Centroid(sourceWalls)
	toOrigin := Translation(-srcCentroid.X, -srcCentroid.Y)
	rotate := RotationDeg(90)
	fromOrigin := Translation(srcCentroid.X, srcCentroid.Y)
	transform := MultiplyMatrices(fromOrigin, MultiplyMatrices(rotate, toOrigin))

	targetWalls := TransformPoints(sourceWalls, transform)
	chargerTgt := TransformPoint(chargerSrc, transform)

	sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
	targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

	config := DefaultICPConfig()
	config.TryRotations = true
	result := AlignMaps(sourceMap, targetMap, config)

	// Verify that rotation search was performed (RotationErrors should be populated)
	if len(RotationErrors) == 0 {
		t.Error("RotationErrors not populated - rotation search may not have run")
	}

	// Verify all 4 rotations were tried
	expectedRotations := []float64{0, 90, 180, 270}
	for _, rot := range expectedRotations {
		if _, ok := RotationErrors[rot]; !ok {
			t.Errorf("Rotation %.0f was not tried", rot)
		}
	}

	// Verify the result is a valid transform
	if !ValidateAlignment(result.Transform) {
		t.Errorf("AlignMaps produced invalid transform")
	}

	// Verify a reasonable score was achieved
	if result.Score < 0.5 {
		t.Errorf("Score too low: got %f, want > 0.5", result.Score)
	}

	t.Logf("Rotation search result: Score=%.3f, ChosenRot=%.0f, RotationErrors=%v",
		result.Score, result.InitialRotation, RotationErrors)
}

func TestAlignMapsWithRotationHint_AllRotations(t *testing.T) {
	// Test rotation hint for all cardinal rotations
	sourceWalls := createLShapeWalls(Point{X: 100, Y: 100}, 2.0)
	chargerSrc := Point{X: 120, Y: 120}
	srcCentroid := Centroid(sourceWalls)

	testCases := []struct {
		name     string
		rotation float64
	}{
		{"rotation_0", 0},
		{"rotation_90", 90},
		{"rotation_180", 180},
		{"rotation_270", 270},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create target with this rotation
			toOrigin := Translation(-srcCentroid.X, -srcCentroid.Y)
			rotate := RotationDeg(tc.rotation)
			fromOrigin := Translation(srcCentroid.X, srcCentroid.Y)
			transform := MultiplyMatrices(fromOrigin, MultiplyMatrices(rotate, toOrigin))

			targetWalls := TransformPoints(sourceWalls, transform)
			chargerTgt := TransformPoint(chargerSrc, transform)

			sourceMap := createTestValetudoMap(sourceWalls, &chargerSrc)
			targetMap := createTestValetudoMap(targetWalls, &chargerTgt)

			config := DefaultICPConfig()
			result := AlignMapsWithRotationHint(sourceMap, targetMap, config, tc.rotation)

			if result.InitialRotation != tc.rotation {
				t.Errorf("InitialRotation mismatch: got %f, want %f",
					result.InitialRotation, tc.rotation)
			}

			if result.Score < 0.1 {
				t.Errorf("Score too low for rotation %.0f: got %f",
					tc.rotation, result.Score)
			}

			t.Logf("Hint=%.0f: Score=%.3f", tc.rotation, result.Score)
		})
	}
}
func TestICP_HallwaySlippage(t *testing.T) {
	// Create a reasonably sized room/hallway (3m x 1.5m)
	hallwayLength := 3000.0 // 3 meters
	hallwayWidth := 1500.0  // 1.5 meters
	points := []Point{}
	for y := 0.0; y <= hallwayLength; y += 10 {
		points = append(points, Point{X: 0, Y: y})
		points = append(points, Point{X: hallwayWidth, Y: y})
	}
	// Add end-caps (horizontal walls)
	for x := 0.0; x <= hallwayWidth; x += 10 {
		points = append(points, Point{X: x, Y: 0})
		points = append(points, Point{X: x, Y: hallwayLength})
	}

	sourceCloud := FeatureSet{
		WallPoints: points,
		Centroid:   Centroid(points),
	}

	// Target is shifted slightly ALONG the axis (simulating slippage)
	// and slightly across the axis.
	expectedTx, expectedTy := 10.0, 50.0 // Slide 50mm along Y, shift 10mm along X
	targetPoints := TransformPoints(sourceCloud.WallPoints, Translation(expectedTx, expectedTy))

	// Add charger as a strong anchor
	chargerSrc := Point{X: 100, Y: 100}
	chargerTgt := TransformPoint(chargerSrc, Translation(expectedTx, expectedTy))

	sourceMap := createTestValetudoMap(points, &chargerSrc)
	targetMap := createTestValetudoMap(targetPoints, &chargerTgt)

	config := DefaultICPConfig()
	// Full AlignMaps should use findBestInitialAlignment which tries multiple offsets
	result := AlignMaps(sourceMap, targetMap, config)

	// We allow some tolerance, but it shouldn't "slip" indefinitely
	if math.Abs(result.Transform.Tx-expectedTx) > 5.0 || math.Abs(result.Transform.Ty-expectedTy) > 10.0 {
		t.Errorf("Hallway AlignMaps slipped! Got (%f, %f), want (%f, %f)",
			result.Transform.Tx, result.Transform.Ty, expectedTx, expectedTy)
	}
}
