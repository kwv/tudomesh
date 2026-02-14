package mesh

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"time"
)

// ICPConfig holds configuration for the ICP algorithm.
// All distance thresholds are in the same units as the input point clouds.
// After the mm-normalization refactor (bead .5/.6), inputs are in local-mm,
// so thresholds are in millimeters.
type ICPConfig struct {
	MaxIterations     int        // Maximum number of iterations
	ConvergenceThresh float64    // Stop when error improvement is below this (mm)
	MaxCorrespondDist float64    // Maximum distance for point correspondence (mm)
	SamplePoints      int        // Number of feature points to use
	OutlierPercentile float64    // Reject correspondences above this percentile (0-1)
	TryRotations      bool       // Try multiple initial rotations (0°, 90°, 180°, 270°)
	RNG               *rand.Rand // Random number generator for deterministic behavior
}

// DefaultICPConfig returns sensible defaults for ICP.
// Distance values are in mm (matching local-mm feature coordinates).
func DefaultICPConfig() ICPConfig {
	return ICPConfig{
		MaxIterations:     50,
		ConvergenceThresh: 1.0,    // 1mm improvement threshold
		MaxCorrespondDist: 3500.0, // Max 3500mm (3.5m) for correspondence - empirically optimal
		SamplePoints:      600,    // Use up to 600 feature points (denser sampling for rotation discrimination)
		OutlierPercentile: 0.8,    // Keep 80% closest correspondences
		TryRotations:      true,   // Try all 4 rotations
		RNG:               rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// ICPResult contains the result of ICP alignment
type ICPResult struct {
	Transform       AffineMatrix // The computed transformation
	Error           float64      // Final alignment error (avg distance)
	Score           float64      // Alignment quality score (higher is better)
	InlierFraction  float64      // Fraction of points that matched
	Iterations      int          // Number of iterations performed
	Converged       bool         // Whether the algorithm converged
	InitialRotation float64      // The initial rotation that worked best (degrees)
}

// RotationErrors stores the error for each rotation tried (for debugging)
var RotationErrors map[float64]float64

// AlignMapsWithRotationHint runs ICP alignment with a preferred rotation hint as starting point
// This allows using rotation hints from config or CLI while still running full ICP refinement
func AlignMapsWithRotationHint(source, target *ValetudoMap, config ICPConfig, rotationHint float64) ICPResult {
	srcFeatures := ExtractFeatures(source)
	tgtFeatures := ExtractFeatures(target)

	// Sample features for ICP
	sourcePoints := SampleFeatures(srcFeatures, config.SamplePoints)
	targetPoints := SampleFeatures(tgtFeatures, config.SamplePoints)

	if len(sourcePoints) < 3 || len(targetPoints) < 3 {
		return ICPResult{
			Transform: Identity(),
			Error:     math.MaxFloat64,
			Score:     -1.0,
		}
	}

	// Build initial transform from rotation hint
	initialTransform := buildInitialTransform(srcFeatures, tgtFeatures, rotationHint, config.RNG)

	// Run full multi-scale ICP refinement starting from the hint
	result := runMultiScaleICP(sourcePoints, targetPoints, initialTransform, config)
	result.InitialRotation = rotationHint

	// Calculate robust score
	transformed := TransformPoints(sourcePoints, result.Transform)
	// Use scale-aware score threshold: 50 pixels
	scoreThreshold := 50.0 * float64(target.PixelSize)
	score, frac, _ := CalculateInlierScore(transformed, targetPoints, scoreThreshold)
	result.Score = score
	result.InlierFraction = frac

	// Wall-only refinement pass (same as AlignMaps)
	if result.Score > 0.05 {
		sourceWalls := srcFeatures.WallPoints
		targetWalls := tgtFeatures.WallPoints

		sourceWalls = samplePointSlice(sourceWalls, 1000)
		targetWalls = samplePointSlice(targetWalls, 1000)

		if len(sourceWalls) > 10 && len(targetWalls) > 10 {
			refineConfig := config
			refineConfig.MaxIterations = 50
			refineConfig.ConvergenceThresh = 0.5
			refineConfig.MaxCorrespondDist = 1000.0 // 1000mm wall refinement radius

			refinedResult := runICPWithMutualNN(sourceWalls, targetWalls, result.Transform, refineConfig)
			result.Transform = refinedResult.Transform
			result.Converged = refinedResult.Converged
			result.Iterations += refinedResult.Iterations
			result.Error = refinedResult.Error

			// Micro-rotation and translation adjustments
			// Use scale-aware snap tolerance: 15 pixels
			snapTolerance := 15.0 * float64(target.PixelSize)
			result.Transform = FineTuneRotation(sourceWalls, targetWalls, result.Transform, tgtFeatures.Centroid, 2.0, 0.25, snapTolerance)
			result.Transform = FineTuneTranslation(sourceWalls, targetWalls, result.Transform, 2.0, 0.25, snapTolerance)
			result.Transform = FineTuneRotation(sourceWalls, targetWalls, result.Transform, tgtFeatures.Centroid, 1.0, 0.1, snapTolerance)
			result.Transform = FineTuneTranslation(sourceWalls, targetWalls, result.Transform, 0.5, 0.1, snapTolerance)

			// Recalculate final score
			transformed := TransformPoints(sourcePoints, result.Transform)
			score, frac, _ := CalculateInlierScore(transformed, targetPoints, scoreThreshold)
			result.Score = score
			result.InlierFraction = frac
		}
	}

	// Apply semantic bonus (room name matching)
	if bonus, msg := CalculateSemanticBonus(srcFeatures, tgtFeatures, result.Transform); bonus > 0 {
		result.Score += bonus
		log.Println(msg)
	}

	return result
}

// CalculateSemanticBonus computes a score bonus based on the alignment of named room centroids.
func CalculateSemanticBonus(source, target FeatureSet, transform AffineMatrix) (float64, string) {
	semanticMatches := 0
	semanticDistSum := 0.0
	semanticScoreSum := 0.0

	for name, srcCentroid := range source.SegmentCentroids {
		if tgtCentroid, ok := target.SegmentCentroids[name]; ok {
			// Transform source centroid
			rx := srcCentroid.X*transform.A + srcCentroid.Y*transform.B + transform.Tx
			ry := srcCentroid.X*transform.C + srcCentroid.Y*transform.D + transform.Ty
			transformedCentroid := Point{X: rx, Y: ry}

			dist := Distance(transformedCentroid, tgtCentroid)
			// Use loose tolerance (3000mm = 3m) because segment definitions vary
			// but 180-degree flips will be far larger (>10m typically).
			if dist < 3000.0 {
				semanticMatches++
				semanticDistSum += dist
				// Non-linear scoring: rewards close matches much more than loose ones
				// 0m -> 1.0; 1m -> 0.5; 3m -> 0.25
				semanticScoreSum += 1000.0 / (1000.0 + dist)
			}
		}
	}

	if semanticMatches > 0 {
		// Bonus is proportional to the quality of the matches
		// Weight: 0.4 per match (so 4 perfect matches = +1.6)
		bonus := semanticScoreSum * 0.4
		avgDist := semanticDistSum / float64(semanticMatches)
		msg := fmt.Sprintf("   Semantic bonus: +%.3f (%d rooms matched, avg dist %.0fmm)", bonus, semanticMatches, avgDist)
		return bonus, msg
	}
	return 0.0, ""
}

// AlignMaps computes the affine transform to align source map to target map
// Tries multiple initial rotations and picks the best result
func AlignMaps(source, target *ValetudoMap, config ICPConfig) ICPResult {
	bestResult := ICPResult{
		Transform: Identity(),
		Error:     math.MaxFloat64,
		Score:     -1.0,
	}

	RotationErrors = make(map[float64]float64)

	// Extract features from both maps
	sourceFeatures := ExtractFeatures(source)
	targetFeatures := ExtractFeatures(target)

	// Sample mixed features for final scoring (charger + walls + grid + boundary)
	sourcePoints := SampleFeatures(sourceFeatures, config.SamplePoints)
	targetPoints := SampleFeatures(targetFeatures, config.SamplePoints)

	if len(sourcePoints) < 3 || len(targetPoints) < 3 {
		return bestResult
	}

	// Use wall-only points for the rotation selection loop.
	// Walls are the stable structural signal; floor/segment coverage varies
	// between vacuum sessions and corrupts rotation discrimination.
	sourceWalls := samplePointSlice(sourceFeatures.WallPoints, config.SamplePoints)
	targetWalls := samplePointSlice(targetFeatures.WallPoints, config.SamplePoints)
	wallCentroidSrc := Centroid(sourceWalls)
	wallCentroidTgt := Centroid(targetWalls)

	// Fall back to mixed features if walls are too sparse
	rotSrc, rotTgt := sourceWalls, targetWalls
	rotCentroidSrc, rotCentroidTgt := wallCentroidSrc, wallCentroidTgt
	if len(sourceWalls) < 20 || len(targetWalls) < 20 {
		rotSrc, rotTgt = sourcePoints, targetPoints
		rotCentroidSrc = sourceFeatures.Centroid
		rotCentroidTgt = targetFeatures.Centroid
	}

	// Rotations to try (in degrees)
	rotations := []float64{0}
	if config.TryRotations {
		rotations = []float64{0, 90, 180, 270}
	}

	// Try each initial rotation — using wall-only data for discrimination
	for _, rotDeg := range rotations {
		// Use robust initialization to find best translation for this rotation
		initialTransform := findBestInitialAlignment(rotSrc, rotTgt, rotCentroidSrc, rotCentroidTgt, rotDeg, config.RNG)

		// Use multi-scale ICP for better coarse-to-fine convergence
		result := runMultiScaleICP(rotSrc, rotTgt, initialTransform, config)
		result.InitialRotation = rotDeg

		// Use tighter scale-aware score threshold: 15 pixels (~75mm)
		// This strictness is needed to distinguish 0° vs 180° in symmetric hallways
		transformed := TransformPoints(rotSrc, result.Transform)
		scoreThreshold := 15.0 * float64(target.PixelSize)
		score, frac, _ := CalculateInlierScore(transformed, rotTgt, scoreThreshold)
		result.Score = score
		result.InlierFraction = frac

		// Material consistency bonus (e.g. carpet matching) to break geometric symmetry
		matScoreSum := 0.0
		matMatches := 0
		for mat, srcMatPts := range sourceFeatures.MaterialPoints {
			if tgtMatPts, ok := targetFeatures.MaterialPoints[mat]; ok && len(srcMatPts) > 0 && len(tgtMatPts) > 0 {
				// Downsample for speed
				srcSample := samplePointSlice(srcMatPts, 200)
				tgtSample := samplePointSlice(tgtMatPts, 200)

				transformedMat := TransformPoints(srcSample, result.Transform)
				// Use looser threshold for carpet (300mm) as edges are fuzzy
				matThreshold := 60.0 * float64(target.PixelSize)
				_, matchFrac, _ := CalculateInlierScore(transformedMat, tgtSample, matThreshold)

				matScoreSum += matchFrac
				matMatches++
			}
		}

		if matMatches > 0 {
			avgMatScore := matScoreSum / float64(matMatches)
			bonus := avgMatScore * 0.3 // 30% bonus for material alignment
			result.Score += bonus
			log.Printf("   Material bonus: +%.3f (avg match %.3f on %d materials)", bonus, avgMatScore, matMatches)
		}

		// Semantic consistency bonus (room name matching)
		if bonus, msg := CalculateSemanticBonus(sourceFeatures, targetFeatures, result.Transform); bonus > 0 {
			result.Score += bonus
			log.Println(msg)
		}

		RotationErrors[rotDeg] = result.Error

		log.Printf("AlignMaps: rot=%.0f° score=%.6f frac=%.3f error=%.1f tx=%.1f ty=%.1f (walls=%d/%d)",
			rotDeg, result.Score, result.InlierFraction, result.Error,
			result.Transform.Tx, result.Transform.Ty,
			len(rotSrc), len(rotTgt))

		// Pick best by Score (Inlier-based), not raw Error (Average distance)
		if result.Score > bestResult.Score {
			bestResult = result
		}
	}

	// Refinement step: Wall-only alignment
	// Floor coverage varies (robot path), but walls are static structure.
	// Asymmetric floor coverage can bias the alignment. Refine using only wall points to "snap" the structure.
	if bestResult.Score > 0.05 { // If we have a plausible meaningful overlap
		sourceWalls := sourceFeatures.WallPoints
		targetWalls := targetFeatures.WallPoints

		// Downsample if needed, but keep dense for refinement
		sourceWalls = samplePointSlice(sourceWalls, 1000)
		targetWalls = samplePointSlice(targetWalls, 1000)

		if len(sourceWalls) > 10 && len(targetWalls) > 10 {
			// Tighter convergence for refinement
			refineConfig := config
			refineConfig.MaxIterations = 50
			refineConfig.ConvergenceThresh = 0.5    // Sub-mm precision
			refineConfig.MaxCorrespondDist = 1000.0 // 1000mm wall refinement radius

			// Run ICP with mutual NN for more robust wall matching
			refinedResult := runICPWithMutualNN(sourceWalls, targetWalls, bestResult.Transform, refineConfig)

			// Update the transform
			bestResult.Transform = refinedResult.Transform
			bestResult.Converged = refinedResult.Converged
			bestResult.Iterations += refinedResult.Iterations
			bestResult.Error = refinedResult.Error

			// Micro-rotation adjustment: ICP may not perfectly lock rotation
			// Try small angle adjustments (±2° in 0.25° steps) to find optimal rotation
			// Use target centroid as rotation pivot
			// Use scale-aware snap tolerance: 15 pixels
			snapTolerance := 15.0 * float64(target.PixelSize)
			bestResult.Transform = FineTuneRotation(sourceWalls, targetWalls, bestResult.Transform, targetFeatures.Centroid, 2.0, 0.25, snapTolerance)

			// Final Nudge: Hill-climbing optimization for "snapping"
			// ICP minimizes sum of squared errors, which can settle in local minima (average fits).
			// We want to maximize strict overlap/proximity (snapping).
			// Nudge the translation slightly to find the peak InlierScore.
			// Reduced min step from 0.5 to 0.25 for sub-half-pixel precision
			bestResult.Transform = FineTuneTranslation(sourceWalls, targetWalls, bestResult.Transform, 2.0, 0.25, snapTolerance)

			// Second pass of rotation fine-tuning after translation adjustment
			// Sometimes translation changes make a slight rotation adjustment beneficial
			bestResult.Transform = FineTuneRotation(sourceWalls, targetWalls, bestResult.Transform, targetFeatures.Centroid, 1.0, 0.1, snapTolerance)

			// Final ultra-fine translation nudge
			bestResult.Transform = FineTuneTranslation(sourceWalls, targetWalls, bestResult.Transform, 0.5, 0.1, snapTolerance)

			// Recalculate robust score against standard points to ensure global consistency
			// Use scale-aware score threshold: 50 pixels
			scoreThreshold := 50.0 * float64(target.PixelSize)
			transformed := TransformPoints(sourcePoints, bestResult.Transform)
			score, frac, _ := CalculateInlierScore(transformed, targetPoints, scoreThreshold)
			bestResult.Score = score
			bestResult.InlierFraction = frac

			// Re-apply semantic bonus to final score
			if bonus, msg := CalculateSemanticBonus(sourceFeatures, targetFeatures, bestResult.Transform); bonus > 0 {
				bestResult.Score += bonus
				log.Println("Refinement " + msg)
			}
		}
	}

	return bestResult
}

// FineTuneTranslation performs a hill-climbing search on translation (Tx, Ty)
// to maximize the inlier score. It tests "nudges" in 8 directions (including diagonals).
func FineTuneTranslation(source, target []Point, initial AffineMatrix, initialStep float64, minStep float64, snapTolerance float64) AffineMatrix {
	current := initial

	currentScore, _, _ := CalculateInlierScore(TransformPoints(source, current), target, snapTolerance)
	step := initialStep

	// Max iterations to drive alignment
	for i := 0; i < 30; i++ { // Increased from 20 for finer convergence
		improved := false
		var bestCandidate AffineMatrix
		bestCandidateScore := currentScore

		// Test 8 directions: 4 cardinal + 4 diagonal
		// Diagonal step is scaled by 1/sqrt(2) to maintain consistent movement magnitude
		diagStep := step * 0.7071 // 1/sqrt(2)
		candidates := []struct{ dx, dy float64 }{
			{step, 0}, {-step, 0},
			{0, step}, {0, -step},
			{diagStep, diagStep}, {diagStep, -diagStep},
			{-diagStep, diagStep}, {-diagStep, -diagStep},
		}

		for _, cand := range candidates {
			testMx := current
			testMx.Tx += cand.dx
			testMx.Ty += cand.dy

			score, _, _ := CalculateInlierScore(TransformPoints(source, testMx), target, snapTolerance)

			if score > bestCandidateScore {
				bestCandidateScore = score
				bestCandidate = testMx
				improved = true
			}
		}

		if improved {
			current = bestCandidate
			currentScore = bestCandidateScore
		} else {
			// If no improvement at this scale, refine the step size
			step /= 2.0
			if step < minStep {
				break
			}
		}
	}
	return current
}

// FineTuneRotation performs micro-rotation adjustments to maximize alignment
// Tests small rotation angles around the current transform
func FineTuneRotation(source, target []Point, initial AffineMatrix, centroid Point, maxAngleDeg float64, stepDeg float64, snapTolerance float64) AffineMatrix {
	current := initial

	currentScore, _, _ := CalculateInlierScore(TransformPoints(source, current), target, snapTolerance)

	// Try micro-rotations from -maxAngle to +maxAngle
	for angle := -maxAngleDeg; angle <= maxAngleDeg; angle += stepDeg {
		if angle == 0 {
			continue
		}

		// Create rotation around centroid
		toOrigin := Translation(-centroid.X, -centroid.Y)
		rotate := RotationDeg(angle)
		fromOrigin := Translation(centroid.X, centroid.Y)
		microRotation := MultiplyMatrices(fromOrigin, MultiplyMatrices(rotate, toOrigin))

		// Apply micro-rotation after current transform
		testMx := MultiplyMatrices(microRotation, current)

		score, _, _ := CalculateInlierScore(TransformPoints(source, testMx), target, snapTolerance)

		if score > currentScore {
			currentScore = score
			current = testMx
		}
	}

	return current
}

// samplePointSlice reduces a slice of points to a maximum size
func samplePointSlice(points []Point, max int) []Point {
	if len(points) <= max {
		return points
	}
	result := make([]Point, max)
	// Simple uniform sampling
	step := float64(len(points)-1) / float64(max-1)
	for i := 0; i < max; i++ {
		idx := int(float64(i) * step)
		result[i] = points[idx]
	}
	return result
}

// CalculateInlierScore calculates a robust alignment score
// Higher is better. Based on fraction of inliers and their tightness.
func CalculateInlierScore(source, target []Point, maxDist float64) (float64, float64, float64) {
	inlierCount := 0
	totalDist := 0.0

	for _, sp := range source {
		minDist := math.MaxFloat64
		for _, tp := range target {
			d := Distance(sp, tp)
			if d < minDist {
				minDist = d
			}
		}

		if minDist <= maxDist {
			inlierCount++
			totalDist += minDist
		}
	}

	if inlierCount == 0 {
		return 0, 0, math.MaxFloat64
	}

	inlierFraction := float64(inlierCount) / float64(len(source))
	avgInlierDist := totalDist / float64(inlierCount)

	// Score formulation:
	// We want high fraction, low distance.
	// Score = Fraction / (1 + AvgDist / (2 * maxDist))
	// This scales from 0 to 1 roughly.
	// The denominator normalizes the distance based on the search tolerance.
	score := inlierFraction / (1.0 + avgInlierDist/(2*maxDist))

	return score, inlierFraction, avgInlierDist
}

// findBestInitialAlignment tries multiple translations for a given rotation
// to find the best initial overlap, robust to partial overlaps
func findBestInitialAlignment(sourcePoints, targetPoints []Point, sourceCentroid, targetCentroid Point, rotationDeg float64, rng *rand.Rand) AffineMatrix {
	// 1. Base transform: Rotate around source centroid
	// Translate source centroid to origin -> Rotate
	toOrigin := Translation(-sourceCentroid.X, -sourceCentroid.Y)
	rotate := RotationDeg(rotationDeg)
	baseTransform := MultiplyMatrices(rotate, toOrigin)

	// Pre-calculate rotated source points centered at origin
	rotatedSource := TransformPoints(sourcePoints, baseTransform)

	bestTransform := Identity()
	bestScore := math.MaxFloat64

	// Candidate translations (Tx, Ty)
	// We are looking for T such that: baseTransform + T aligns well
	// Note: MultiplyMatrices(Translation(Tx, Ty), baseTransform) effectively adds (Tx, Ty) translation

	// Always include Centroid-to-Centroid alignment
	// Since baseTransform puts sourceCentroid at (0,0), we just need to translate to targetCentroid
	candidates := []Point{targetCentroid}

	// Add candidates from point matching (RANSAC-like)
	// Pick random pairs of points and align them
	// If rotated_s matches t, then translation = t - rotated_s

	// Sample a subset for candidate generation
	// Increased from 200 to 400 for better coverage of partial overlaps
	numCandidates := 400

	for i := 0; i < numCandidates; i++ {
		// Pick random source and target point
		if len(rotatedSource) == 0 || len(targetPoints) == 0 {
			break
		}
		s := rotatedSource[rng.Intn(len(rotatedSource))]
		t := targetPoints[rng.Intn(len(targetPoints))]

		// Candidate translation
		tx := t.X - s.X
		ty := t.Y - s.Y
		candidates = append(candidates, Point{X: tx, Y: ty})
	}

	// Evaluate candidates
	// Use a coarser subset for evaluation to be fast
	evalSource := rotatedSource
	if len(evalSource) > 100 {
		evalSource = make([]Point, 100)
		perm := rng.Perm(len(rotatedSource))
		for i := 0; i < 100; i++ {
			evalSource[i] = rotatedSource[perm[i]]
		}
	}

	for _, trans := range candidates {
		// Calculate score: average distance of nearest neighbors (trimmed)
		// We only care if *some* points overlap well (partial overlap)

		score := 0.0
		matchCount := 0

		// For each source point, find nearest target point
		for _, sp := range evalSource {
			// Apply translation
			tsp := Point{X: sp.X + trans.X, Y: sp.Y + trans.Y}

			minDist := math.MaxFloat64
			// Optimize: check if inside bounding box? For now just brute force
			for _, tp := range targetPoints {
				d := (tsp.X-tp.X)*(tsp.X-tp.X) + (tsp.Y-tp.Y)*(tsp.Y-tp.Y)
				if d < minDist {
					minDist = d
				}
			}

			// Valid match if distance is reasonable (e.g. 100 pixels)
			limit := 100.0 * 5.0 // Base on standard 5mm/px for initial search robust to resolution
			if minDist < limit*limit { 
				score += math.Sqrt(minDist)
				matchCount++
			}
		}

		// Penalize very few matches
		if matchCount < 10 {
			score += 100000.0
		} else {
			score = score / float64(matchCount)
			// Bonus for more matches
			score -= float64(matchCount) * 0.1
		}

		if score < bestScore {
			bestScore = score
			bestTransform = MultiplyMatrices(Translation(trans.X, trans.Y), baseTransform)
		}
	}

	return bestTransform
}

// buildInitialTransform creates an initial transform using robust point matching
// This ensures that even when forcing a rotation, we find the best translation
func buildInitialTransform(source, target FeatureSet, rotationDeg float64, rng *rand.Rand) AffineMatrix {
	// Sample points for matching
	sourcePoints := SampleFeatures(source, 300)
	targetPoints := SampleFeatures(target, 300)

	if len(sourcePoints) < 3 || len(targetPoints) < 3 {
		// Fallback to centroid alignment if not enough points
		sourcePivot := source.Centroid
		targetPivot := target.Centroid
		toOrigin := Translation(-sourcePivot.X, -sourcePivot.Y)
		rotate := RotationDeg(rotationDeg)
		toTarget := Translation(targetPivot.X, targetPivot.Y)
		return MultiplyMatrices(toTarget, MultiplyMatrices(rotate, toOrigin))
	}

	return findBestInitialAlignment(sourcePoints, targetPoints, source.Centroid, target.Centroid, rotationDeg, rng)
}

// runICP performs ICP iterations starting from an initial transform.
// It uses inlier-based scoring to prevent divergence and ensure physical overlap increases.
func runICP(sourcePoints, targetPoints []Point, initialTransform AffineMatrix, config ICPConfig) ICPResult {
	result := ICPResult{
		Transform:       initialTransform,
		Error:           math.MaxFloat64,
		Score:           -1.0,
		InitialRotation: 0, // Placeholder
	}

	currentTransform := initialTransform

	// Calculate initial robust error and score
	transformed := TransformPoints(sourcePoints, currentTransform)
	// We use the pass's MaxCorrespondDist as the robust scoring threshold.
	prevScore, prevFrac, prevInlierError := CalculateInlierScore(transformed, targetPoints, config.MaxCorrespondDist)

	result.Error = prevInlierError
	result.Score = prevScore
	result.InlierFraction = prevFrac
	result.Transform = currentTransform

	for iter := 0; iter < config.MaxIterations; iter++ {
		result.Iterations = iter + 1

		// Transform source points with current estimate
		transformed = TransformPoints(sourcePoints, currentTransform)

		// Find correspondences with outlier rejection
		srcCorr, tgtCorr, distances := findCorrespondencesWithDistances(transformed, targetPoints, config.MaxCorrespondDist)
		if len(srcCorr) < 3 {
			break
		}

		// Reject outliers based on distance percentile
		srcCorr, tgtCorr = rejectOutliers(srcCorr, tgtCorr, distances, config.OutlierPercentile)
		if len(srcCorr) < 3 {
			break
		}

		// TRIMMED ICP: Further select only the best N% of remaining correspondences
		// to compute the transform. This prevents "pulling" from moderately far points
		// and focuses on the high-confidence "skeleton" alignment.
		// Use best 70% of the already rejected set (OutlierPercentile)
		trimPercentile := 0.7
		trimCount := int(float64(len(srcCorr)) * trimPercentile)
		if trimCount < 3 {
			trimCount = len(srcCorr)
		}

		var incrementalTransform AffineMatrix
		if trimCount < len(srcCorr) {
			// Re-calculate distances for the selected subset (already sorted by rejectOutliers)
			// Actually rejectOutliers doesn't sort. findCorrespondencesWithDistances doesn't sort.
			// Let's implement a quick sort-and-trim or use weights.
			// Since we want robust skeletal mapping, let's use weighted transforms
			// where weight = 1.0 / (1.0 + dist/PixelSize).
			weights := make([]float64, len(srcCorr))
			// We need distances for the *current* srcCorr/tgtCorr
			// rejectOutliers returns new slices. Let's redo findCorrespondences to get sorted ones.
			
			// Actually, let's just use the weights for the rigid transform.
			for i := range srcCorr {
				d := Distance(srcCorr[i], tgtCorr[i])
				weights[i] = 1.0 / (1.0 + d*d/1000.0) // Soft weight based on proximity
			}
			incrementalTransform = CalculateWeightedRigidTransform(srcCorr, tgtCorr, weights)
		} else {
			incrementalTransform = CalculateRigidTransform(srcCorr, tgtCorr)
		}

		// Compose: new = incremental * current
		newTransform := MultiplyMatrices(incrementalTransform, currentTransform)

		// Calculate robust alignment quality with new transform
		testTransformed := TransformPoints(sourcePoints, newTransform)
		newScore, newFrac, newInlierError := CalculateInlierScore(testTransformed, targetPoints, config.MaxCorrespondDist)

		// BACKTRACKING / SCORE VALIDATION
		// ICP minimizes sum of squares, which can decrease the physical inlier count
		// if a few outliers pull hard (e.g. into a parallel hallway).
		// We refuse to move if the physical overlap score decreases significantly.
		if newScore < prevScore*0.98 {
			// Termination: physical alignment is degrading.
			break
		}

		// Check convergence
		improvement := prevInlierError - newInlierError
		if improvement < config.ConvergenceThresh && improvement >= 0 {
			result.Converged = true
			result.Transform = newTransform
			result.Error = newInlierError
			result.Score = newScore
			result.InlierFraction = newFrac
			break
		}

		// Check for severe divergence
		if newInlierError > prevInlierError*1.5 {
			break
		}

		prevInlierError = newInlierError
		prevScore = newScore
		currentTransform = newTransform

		result.Transform = newTransform
		result.Error = newInlierError
		result.Score = newScore
		result.InlierFraction = newFrac
	}

	return result
}

// runMultiScaleICP performs ICP with progressive tightening of correspondence distance
// This helps escape local minima by starting coarse and refining progressively
func runMultiScaleICP(sourcePoints, targetPoints []Point, initialTransform AffineMatrix, config ICPConfig) ICPResult {
	result := ICPResult{
		Transform: initialTransform,
		Error:     math.MaxFloat64,
	}

	currentTransform := initialTransform

	// Multi-scale annealing schedule: start coarse, progressively tighten
	// Each scale runs a mini-Pass
	// More granular steps help ensure "locking" in ambiguous structures.
	scales := []struct {
		maxDist    float64
		iterations int
		threshold  float64
	}{
		{config.MaxCorrespondDist, 15, 2.0},       // Coarse: large search radius (e.g. 3500mm)
		{2000.0, 15, 1.0},                         // Intermediate (2m)
		{1000.0, 15, 0.5},                         // Structure lock (1m)
		{500.0, 20, 0.25},                         // Fine (0.5m)
		{250.0, 20, 0.1},                          // Super-fine (0.25m)
	}

	totalIterations := 0
	for _, scale := range scales {
		scaleConfig := config
		scaleConfig.MaxCorrespondDist = scale.maxDist
		scaleConfig.MaxIterations = scale.iterations
		scaleConfig.ConvergenceThresh = scale.threshold

		scaleResult := runICP(sourcePoints, targetPoints, currentTransform, scaleConfig)
		currentTransform = scaleResult.Transform
		totalIterations += scaleResult.Iterations

		if scaleResult.Error < result.Error {
			result.Transform = scaleResult.Transform
			result.Error = scaleResult.Error
			result.Converged = scaleResult.Converged
		}
	}

	result.Iterations = totalIterations
	return result
}

// runICPWithMutualNN performs ICP using mutual nearest neighbor for more robust correspondences.
// It includes inlier-based score validation to prevent structure "slippage".
func runICPWithMutualNN(sourcePoints, targetPoints []Point, initialTransform AffineMatrix, config ICPConfig) ICPResult {
	result := ICPResult{
		Transform: initialTransform,
		Error:     math.MaxFloat64,
		Score:     -1.0,
	}

	currentTransform := initialTransform

	// Calculate initial robust error and score
	transformed := TransformPoints(sourcePoints, currentTransform)
	prevScore, prevFrac, prevInlierError := CalculateInlierScore(transformed, targetPoints, config.MaxCorrespondDist)

	result.Error = prevInlierError
	result.Score = prevScore
	result.InlierFraction = prevFrac
	result.Transform = currentTransform

	for iter := 0; iter < config.MaxIterations; iter++ {
		result.Iterations = iter + 1

		// Transform source points with current estimate
		transformed = TransformPoints(sourcePoints, currentTransform)

		// Find MUTUAL correspondences (more robust for fine refinement)
		srcCorr, tgtCorr, distances := findMutualCorrespondences(transformed, targetPoints, config.MaxCorrespondDist)

		// Fall back to one-way if mutual gives too few correspondences to prevent stalls
		if len(srcCorr) < 10 {
			srcCorr, tgtCorr, distances = findCorrespondencesWithDistances(transformed, targetPoints, config.MaxCorrespondDist)
		}

		if len(srcCorr) < 3 {
			break
		}

		// Reject outliers based on distance percentile
		srcCorr, tgtCorr = rejectOutliers(srcCorr, tgtCorr, distances, config.OutlierPercentile)
		if len(srcCorr) < 3 {
			break
		}

		// Compute transform
		var incrementalTransform AffineMatrix
		weights := make([]float64, len(srcCorr))
		for i := range srcCorr {
			d := Distance(srcCorr[i], tgtCorr[i])
			weights[i] = 1.0 / (1.0 + d*d/500.0) // Slightly tighter weight for fine refinement
		}
		incrementalTransform = CalculateWeightedRigidTransform(srcCorr, tgtCorr, weights)
		newTransform := MultiplyMatrices(incrementalTransform, currentTransform)

		// Calculate robust alignment quality with new transform
		testTransformed := TransformPoints(sourcePoints, newTransform)
		newScore, newFrac, newInlierError := CalculateInlierScore(testTransformed, targetPoints, config.MaxCorrespondDist)

		// BACKTRACKING / SCORE VALIDATION
		// Refinement must improve (or maintain) the physical alignment quality.
		if newScore < prevScore*0.99 { // Very slight buffer for micro-jitters
			break
		}

		// Check convergence
		improvement := prevInlierError - newInlierError
		if improvement < config.ConvergenceThresh && improvement >= 0 {
			result.Converged = true
			result.Transform = newTransform
			result.Error = newInlierError
			result.Score = newScore
			result.InlierFraction = newFrac
			break
		}

		// Check for divergence
		if newInlierError > prevInlierError*1.5 {
			break
		}

		prevInlierError = newInlierError
		prevScore = newScore
		currentTransform = newTransform

		result.Transform = newTransform
		result.Error = newInlierError
		result.Score = newScore
		result.InlierFraction = newFrac
	}

	return result
}

// findCorrespondencesWithDistances finds nearest neighbor pairs and returns distances
func findCorrespondencesWithDistances(source, target []Point, maxDist float64) (srcCorr, tgtCorr []Point, distances []float64) {
	for _, sp := range source {
		minDist := math.MaxFloat64
		var nearest Point
		for _, tp := range target {
			d := Distance(sp, tp)
			if d < minDist {
				minDist = d
				nearest = tp
			}
		}
		if minDist <= maxDist {
			srcCorr = append(srcCorr, sp)
			tgtCorr = append(tgtCorr, nearest)
			distances = append(distances, minDist)
		}
	}
	return
}

// findMutualCorrespondences finds mutual nearest neighbor pairs (both points agree they're each other's nearest)
// This is more robust than one-way nearest neighbor matching
func findMutualCorrespondences(source, target []Point, maxDist float64) (srcCorr, tgtCorr []Point, distances []float64) {
	// Build source -> target nearest neighbor map
	srcToTgt := make(map[int]int) // source index -> target index
	srcToTgtDist := make(map[int]float64)

	for si, sp := range source {
		minDist := math.MaxFloat64
		minIdx := -1
		for ti, tp := range target {
			d := Distance(sp, tp)
			if d < minDist {
				minDist = d
				minIdx = ti
			}
		}
		if minDist <= maxDist && minIdx >= 0 {
			srcToTgt[si] = minIdx
			srcToTgtDist[si] = minDist
		}
	}

	// Build target -> source nearest neighbor map
	tgtToSrc := make(map[int]int) // target index -> source index

	for ti, tp := range target {
		minDist := math.MaxFloat64
		minIdx := -1
		for si, sp := range source {
			d := Distance(tp, sp)
			if d < minDist {
				minDist = d
				minIdx = si
			}
		}
		if minDist <= maxDist && minIdx >= 0 {
			tgtToSrc[ti] = minIdx
		}
	}

	// Keep only mutual correspondences (where both agree)
	for si, ti := range srcToTgt {
		if reverseSi, ok := tgtToSrc[ti]; ok && reverseSi == si {
			srcCorr = append(srcCorr, source[si])
			tgtCorr = append(tgtCorr, target[ti])
			distances = append(distances, srcToTgtDist[si])
		}
	}

	return
}

// rejectOutliers removes correspondences with distances above the given percentile
func rejectOutliers(srcCorr, tgtCorr []Point, distances []float64, percentile float64) ([]Point, []Point) {
	if len(distances) == 0 || percentile >= 1.0 {
		return srcCorr, tgtCorr
	}

	// Find threshold distance at percentile
	sortedDists := make([]float64, len(distances))
	copy(sortedDists, distances)
	sort.Float64s(sortedDists)

	idx := int(float64(len(sortedDists)) * percentile)
	if idx >= len(sortedDists) {
		idx = len(sortedDists) - 1
	}
	threshold := sortedDists[idx]

	// Filter correspondences
	var filteredSrc, filteredTgt []Point
	for i, d := range distances {
		if d <= threshold {
			filteredSrc = append(filteredSrc, srcCorr[i])
			filteredTgt = append(filteredTgt, tgtCorr[i])
		}
	}

	return filteredSrc, filteredTgt
}

// AlignToReference aligns a vacuum map to a reference vacuum's coordinate system
// This is the main entry point for calibration
func AlignToReference(vacuumMap, referenceMap *ValetudoMap) (AffineMatrix, float64) {
	config := DefaultICPConfig()
	result := AlignMaps(vacuumMap, referenceMap, config)
	return result.Transform, result.Error
}

// QuickAlign performs a fast alignment trying all 4 rotations with charger anchor
// Returns the best rotation without full ICP refinement
func QuickAlign(source, target *ValetudoMap) AffineMatrix {
	sourceFeatures := ExtractFeatures(source)
	targetFeatures := ExtractFeatures(target)

	if !sourceFeatures.HasCharger || !targetFeatures.HasCharger {
		return Identity()
	}

	sourcePoints := SampleFeatures(sourceFeatures, 100)
	targetPoints := SampleFeatures(targetFeatures, 100)

	bestTransform := Identity()
	bestError := math.MaxFloat64

	// Create a default RNG for initialization
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for _, rotDeg := range []float64{0, 90, 180, 270} {
		transform := buildInitialTransform(sourceFeatures, targetFeatures, rotDeg, rng)
		transformed := TransformPoints(sourcePoints, transform)
		err := FeatureDistance(transformed, targetPoints)

		if err < bestError {
			bestError = err
			bestTransform = transform
		}
	}

	return bestTransform
}

// RefineAlignment improves an existing alignment with more iterations
func RefineAlignment(source, target *ValetudoMap, initial AffineMatrix) ICPResult {
	config := DefaultICPConfig()
	config.MaxIterations = 100
	config.ConvergenceThresh = 0.1
	config.TryRotations = false // Already have initial transform

	sourceFeatures := ExtractFeatures(source)
	targetFeatures := ExtractFeatures(target)
	sourcePoints := SampleFeatures(sourceFeatures, config.SamplePoints)
	targetPoints := SampleFeatures(targetFeatures, config.SamplePoints)

	if len(sourcePoints) < 3 || len(targetPoints) < 3 {
		return ICPResult{Transform: initial, Error: math.MaxFloat64}
	}

	return runICP(sourcePoints, targetPoints, initial, config)
}

// ValidateAlignment checks if a transform produces reasonable results
// Returns true if the transform preserves approximate scale and doesn't flip
func ValidateAlignment(transform AffineMatrix) bool {
	// Check scale factors (should be close to 1)
	scaleX := math.Sqrt(transform.A*transform.A + transform.C*transform.C)
	scaleY := math.Sqrt(transform.B*transform.B + transform.D*transform.D)

	if scaleX < 0.8 || scaleX > 1.2 || scaleY < 0.8 || scaleY > 1.2 {
		return false
	}

	// Check for reflection (determinant should be positive)
	det := transform.A*transform.D - transform.B*transform.C
	return det >= 0
}
