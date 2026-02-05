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
	if !result.Converged { t.Errorf("Identity failed") }
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
	
	if math.Abs(result.Transform.Tx - expectedTx) > 0.25 || math.Abs(result.Transform.Ty - expectedTy) > 0.25 {
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
	
	initialTx := findBestInitialAlignment(original.WallPoints, targetPoints, srcCentroid, tgtCentroid, 0)
	
	config := DefaultICPConfig()
	config.SamplePoints = 2000
	result := runICP(original.WallPoints, targetPoints, initialTx, config)
	
	if math.Abs(result.Transform.Tx - expectedTx) > 1.0 || math.Abs(result.Transform.Ty - expectedTy) > 1.0 {
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
	
	initialTx := findBestInitialAlignment(original.WallPoints, targetPoints, srcCentroid, tgtCentroid, 45)
	
	config := DefaultICPConfig()
	result := runICP(original.WallPoints, targetPoints, initialTx, config)
	
	angle := math.Atan2(result.Transform.C, result.Transform.A) * 180 / math.Pi
	if angle < 0 { angle += 360 }
	if math.Abs(angle - 45) > 1.0 { t.Errorf("ICP rotation incorrect") }
}

func TestICP_Noise(t *testing.T) {
	cloudRng := rand.New(rand.NewSource(42))
	original := createRandomCloud(Point{0, 0}, 200, 100, cloudRng)
	noiseRng := rand.New(rand.NewSource(42))
	
	expectedTx, expectedTy := 1.5, 1.5
	targetPoints := make([]Point, len(original.WallPoints))
	for i, p := range original.WallPoints {
		px := p.X + expectedTx + (noiseRng.Float64() - 0.5) * 2.0
		py := p.Y + expectedTy + (noiseRng.Float64() - 0.5) * 2.0
		targetPoints[i] = Point{X: px, Y: py}
	}
	
	config := DefaultICPConfig()
	result := runICP(original.WallPoints, targetPoints, Identity(), config)
	
	if math.Abs(result.Transform.Tx - expectedTx) > 1.0 || math.Abs(result.Transform.Ty - expectedTy) > 1.0 {
		t.Errorf("ICP noisy translation incorrect. Got (%f, %f), want (%f, %f). Error: %f", result.Transform.Tx, result.Transform.Ty, expectedTx, expectedTy, result.Error)
	}
}
