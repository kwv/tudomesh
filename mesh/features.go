package mesh

import (
	"fmt"
	"math"
	"sort"
)

// FeatureSet contains extracted alignment features from a map
type FeatureSet struct {
	// Boundary points from all segments/floor layers
	BoundaryPoints []Point

	// Corner points (significant angle changes in boundary)
	Corners []Point

	// Wall points (strong structural features)
	WallPoints []Point

	// Grid-sampled floor points (for rotation matching)
	GridPoints []Point

	// Charger position (strong anchor point)
	ChargerPosition Point
	HasCharger      bool

	// Centroid of all floor area
	Centroid Point

	// Bounding box
	MinX, MinY, MaxX, MaxY float64
}

// ExtractFeatures extracts alignment features from a Valetudo map
func ExtractFeatures(m *ValetudoMap) FeatureSet {
	fs := FeatureSet{
		MinX: math.MaxFloat64,
		MinY: math.MaxFloat64,
		MaxX: -math.MaxFloat64,
		MaxY: -math.MaxFloat64,
	}

	// Collect all floor/segment pixels
	var allPixels []Point
	for _, layer := range m.Layers {
		if layer.Type == "floor" || layer.Type == "segment" {
			points := PixelsToPoints(layer.Pixels)
			allPixels = append(allPixels, points...)

			// Update bounding box
			for _, p := range points {
				if p.X < fs.MinX {
					fs.MinX = p.X
				}
				if p.Y < fs.MinY {
					fs.MinY = p.Y
				}
				if p.X > fs.MaxX {
					fs.MaxX = p.X
				}
				if p.Y > fs.MaxY {
					fs.MaxY = p.Y
				}
			}
		}
	}

	// Extract wall points (strong structural features)
	for _, layer := range m.Layers {
		if layer.Type == "wall" {
			wallPts := PixelsToPoints(layer.Pixels)
			// Sample walls at regular intervals
			step := 1
			if len(wallPts) > 500 {
				step = len(wallPts) / 500
			}
			for i := 0; i < len(wallPts); i += step {
				fs.WallPoints = append(fs.WallPoints, wallPts[i])
			}
		}
	}

	// Calculate centroid
	if len(allPixels) > 0 {
		fs.Centroid = Centroid(allPixels)
	}

	// Extract boundary points (edge detection)
	fs.BoundaryPoints = extractBoundary(allPixels, m.PixelSize)

	// Extract corners from boundary
	fs.Corners = extractCorners(fs.BoundaryPoints, 60.0) // 60 degree threshold

	// Extract grid-sampled floor points for robust rotation matching
	fs.GridPoints = extractGridPoints(allPixels, m.PixelSize, 50) // 50 pixel grid

	// Get charger position as strong anchor
	if pos, ok := ExtractChargerPosition(m); ok {
		fs.ChargerPosition = pos
		fs.HasCharger = true
	}

	return fs
}

// extractGridPoints samples floor points on a regular grid
// This provides consistent features for rotation matching
func extractGridPoints(pixels []Point, pixelSize int, gridSpacing int) []Point {
	if len(pixels) == 0 {
		return nil
	}

	// Build occupancy grid
	occupied := make(map[Point]bool)
	for _, p := range pixels {
		gx := math.Round(p.X / float64(gridSpacing))
		gy := math.Round(p.Y / float64(gridSpacing))
		occupied[Point{X: gx, Y: gy}] = true
	}

	// Return grid centers that are occupied
	var gridPts []Point
	gs := float64(gridSpacing)
	for key := range occupied {
		gridPts = append(gridPts, Point{X: key.X * gs, Y: key.Y * gs})
	}

	return gridPts
}

// extractBoundary finds boundary points from a set of floor pixels
// Uses a simple edge detection: points with fewer neighbors are on the boundary
func extractBoundary(pixels []Point, pixelSize int) []Point {
	if len(pixels) == 0 {
		return nil
	}

	// Build a set of occupied pixels for fast lookup
	occupied := make(map[Point]bool)
	for _, p := range pixels {
		// Normalize to grid coordinates
		gx := math.Round(p.X / float64(pixelSize))
		gy := math.Round(p.Y / float64(pixelSize))
		occupied[Point{X: gx, Y: gy}] = true
	}

	// Find boundary points (those missing at least one neighbor)
	var boundary []Point
	ps := float64(pixelSize)
	neighbors := []Point{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}

	seen := make(map[Point]bool)
	for _, p := range pixels {
		gx := math.Round(p.X / ps)
		gy := math.Round(p.Y / ps)
		key := Point{X: gx, Y: gy}

		if seen[key] {
			continue
		}
		seen[key] = true

		// Check if this is an edge pixel
		isBoundary := false
		for _, n := range neighbors {
			neighbor := Point{X: gx + n.X, Y: gy + n.Y}
			if !occupied[neighbor] {
				isBoundary = true
				break
			}
		}

		if isBoundary {
			boundary = append(boundary, Point{X: gx * ps, Y: gy * ps})
		}
	}

	return boundary
}

// extractCorners finds corner points where the boundary has significant angle changes
func extractCorners(boundary []Point, angleThresholdDeg float64) []Point {
	if len(boundary) < 3 {
		return nil
	}

	// Sort boundary points to form a rough contour
	// Use a simple approach: sort by angle from centroid
	centroid := Centroid(boundary)
	sorted := make([]Point, len(boundary))
	copy(sorted, boundary)

	sort.Slice(sorted, func(i, j int) bool {
		ai := math.Atan2(sorted[i].Y-centroid.Y, sorted[i].X-centroid.X)
		aj := math.Atan2(sorted[j].Y-centroid.Y, sorted[j].X-centroid.X)
		return ai < aj
	})

	// Find points with significant angle changes
	threshold := angleThresholdDeg * math.Pi / 180.0
	var corners []Point

	n := len(sorted)
	for i := 0; i < n; i++ {
		prev := sorted[(i-1+n)%n]
		curr := sorted[i]
		next := sorted[(i+1)%n]

		// Calculate angle at this point
		v1x := prev.X - curr.X
		v1y := prev.Y - curr.Y
		v2x := next.X - curr.X
		v2y := next.Y - curr.Y

		// Normalize vectors
		len1 := math.Sqrt(v1x*v1x + v1y*v1y)
		len2 := math.Sqrt(v2x*v2x + v2y*v2y)
		if len1 < 1e-10 || len2 < 1e-10 {
			continue
		}

		v1x /= len1
		v1y /= len1
		v2x /= len2
		v2y /= len2

		// Angle between vectors (using dot product)
		dot := v1x*v2x + v1y*v2y
		if dot > 1 {
			dot = 1
		}
		if dot < -1 {
			dot = -1
		}
		angle := math.Acos(dot)

		// Sharp corner if angle is less than (180 - threshold)
		if angle < math.Pi-threshold {
			corners = append(corners, curr)
		}
	}

	return corners
}

// SampleFeatures reduces the number of features for faster ICP matching
// Prioritizes: charger, walls, grid points, corners, then boundary points
func SampleFeatures(fs FeatureSet, maxPoints int) []Point {
	var result []Point

	// Always include charger if present (strongest anchor)
	if fs.HasCharger {
		result = append(result, fs.ChargerPosition)
	}

	// Include wall points (strong structural features)
	wallAlloc := maxPoints / 3
	if len(fs.WallPoints) > 0 {
		step := 1
		if len(fs.WallPoints) > wallAlloc {
			step = len(fs.WallPoints) / wallAlloc
		}
		for i := 0; i < len(fs.WallPoints) && len(result) < maxPoints/3; i += step {
			result = append(result, fs.WallPoints[i])
		}
	}

	// Include grid points for rotation matching
	gridAlloc := maxPoints / 3
	if len(fs.GridPoints) > 0 {
		step := 1
		if len(fs.GridPoints) > gridAlloc {
			step = len(fs.GridPoints) / gridAlloc
		}
		for i := 0; i < len(fs.GridPoints) && len(result) < 2*maxPoints/3; i += step {
			result = append(result, fs.GridPoints[i])
		}
	}

	// Include some corners (important geometric features)
	if len(fs.Corners) > 0 {
		remaining := maxPoints - len(result)
		cornerAlloc := min(len(fs.Corners), min(50, remaining))
		for i := 0; i < cornerAlloc; i++ {
			result = append(result, fs.Corners[i])
		}
	}

	// Fill remaining with boundary points
	remaining := maxPoints - len(result)
	if remaining > 0 && len(fs.BoundaryPoints) > 0 {
		step := 1
		if len(fs.BoundaryPoints) > remaining {
			step = len(fs.BoundaryPoints) / remaining
		}
		for i := 0; i < len(fs.BoundaryPoints) && len(result) < maxPoints; i += step {
			result = append(result, fs.BoundaryPoints[i])
		}
	}

	return result
}

// FeatureDistance calculates the average nearest-neighbor distance between two feature sets
// Lower values indicate better alignment
func FeatureDistance(source, target []Point) float64 {
	if len(source) == 0 || len(target) == 0 {
		return math.MaxFloat64
	}

	var totalDist float64
	for _, sp := range source {
		minDist := math.MaxFloat64
		for _, tp := range target {
			d := Distance(sp, tp)
			if d < minDist {
				minDist = d
			}
		}
		totalDist += minDist
	}

	return totalDist / float64(len(source))
}

// WallAngleHistogram represents the distribution of wall angles in a map
// Angles are binned into 1-degree buckets from 0-179 (180° symmetry)
type WallAngleHistogram struct {
	Bins       [180]float64 // Normalized histogram bins
	RawCounts  [180]int     // Raw counts per bin
	TotalEdges int          // Total number of edges analyzed
}

// ExtractWallAngles builds a histogram of wall segment angles from a map
// Uses 180° symmetry since walls have no inherent direction
func ExtractWallAngles(m *ValetudoMap) WallAngleHistogram {
	var hist WallAngleHistogram

	// Collect wall points
	var wallPoints []Point
	for _, layer := range m.Layers {
		if layer.Type == "wall" {
			wallPoints = append(wallPoints, PixelsToPoints(layer.Pixels)...)
		}
	}

	if len(wallPoints) < 2 {
		return hist
	}

	// Build a grid of wall pixels for edge detection
	pixelSize := float64(m.PixelSize)
	if pixelSize == 0 {
		pixelSize = 5
	}

	// Map points to grid cells
	grid := make(map[Point]bool)
	for _, p := range wallPoints {
		gx := math.Round(p.X / pixelSize)
		gy := math.Round(p.Y / pixelSize)
		grid[Point{X: gx, Y: gy}] = true
	}

	// For each wall pixel, find neighbors and compute edge angles
	// This detects local wall orientations
	neighbors := []Point{
		{1, 0}, {1, 1}, {0, 1}, {-1, 1},
		{-1, 0}, {-1, -1}, {0, -1}, {1, -1},
	}

	for _, p := range wallPoints {
		gx := math.Round(p.X / pixelSize)
		gy := math.Round(p.Y / pixelSize)

		// Find connected neighbors
		for _, n := range neighbors {
			nx, ny := gx+n.X, gy+n.Y
			if grid[Point{X: nx, Y: ny}] {
				// Calculate angle of this edge
				dx := n.X
				dy := n.Y
				angle := math.Atan2(dy, dx) * 180 / math.Pi

				// Normalize to 0-179 (symmetric - a wall at 0° is same as 180°)
				for angle < 0 {
					angle += 180
				}
				for angle >= 180 {
					angle -= 180
				}

				bin := int(math.Round(angle)) % 180
				hist.RawCounts[bin]++
				hist.TotalEdges++
			}
		}
	}

	// Normalize histogram
	if hist.TotalEdges > 0 {
		for i := 0; i < 180; i++ {
			hist.Bins[i] = float64(hist.RawCounts[i]) / float64(hist.TotalEdges)
		}
	}

	return hist
}

// DominantAngles returns the top N most common wall angles
func (h *WallAngleHistogram) DominantAngles(n int) []float64 {
	type angleCount struct {
		angle float64
		count int
	}

	var angles []angleCount
	for i := 0; i < 180; i++ {
		if h.RawCounts[i] > 0 {
			angles = append(angles, angleCount{float64(i), h.RawCounts[i]})
		}
	}

	sort.Slice(angles, func(i, j int) bool {
		return angles[i].count > angles[j].count
	})

	result := make([]float64, 0, n)
	for i := 0; i < len(angles) && i < n; i++ {
		result = append(result, angles[i].angle)
	}
	return result
}

// CompareHistograms calculates similarity between two angle histograms
// Returns a score from 0-1 where 1 is identical
func CompareHistograms(h1, h2 WallAngleHistogram, offsetDeg float64) float64 {
	if h1.TotalEdges == 0 || h2.TotalEdges == 0 {
		return 0
	}

	// Apply rotation offset to h2
	offset := int(math.Round(offsetDeg)) % 180
	if offset < 0 {
		offset += 180
	}

	// Calculate histogram intersection (Bhattacharyya-like similarity)
	var similarity float64
	for i := 0; i < 180; i++ {
		j := (i + offset) % 180
		// Geometric mean gives more weight to matching peaks
		similarity += math.Sqrt(h1.Bins[i] * h2.Bins[j])
	}

	return similarity
}

// RotationAnalysis contains the result of automatic rotation detection
type RotationAnalysis struct {
	BestRotation float64             // Best rotation in degrees (0, 90, 180, 270)
	Scores       map[float64]float64 // Similarity score for each rotation
	Confidence   float64             // Confidence in the result (0-1)
	SourceAngles []float64           // Dominant angles in source
	TargetAngles []float64           // Dominant angles in target
}

// DetectRotation analyzes wall angles to determine rotation between two maps
// Returns the rotation (in degrees) needed to align source to target
func DetectRotation(source, target *ValetudoMap) RotationAnalysis {
	result := RotationAnalysis{
		Scores: make(map[float64]float64),
	}

	sourceHist := ExtractWallAngles(source)
	targetHist := ExtractWallAngles(target)

	result.SourceAngles = sourceHist.DominantAngles(4)
	result.TargetAngles = targetHist.DominantAngles(4)

	// Test rotations at 0°, 90°, 180°, 270°
	// Note: For 180° histogram symmetry, we only need 0° and 90° checks
	// but we test all 4 for clarity and to handle asymmetric features
	rotations := []float64{0, 90, 180, 270}

	bestScore := -1.0
	for _, rot := range rotations {
		// For histogram comparison, 90° rotation shifts bins by 90
		// 180° and 270° are equivalent to 0° and 90° due to symmetry
		histOffset := math.Mod(rot, 180)
		score := CompareHistograms(sourceHist, targetHist, histOffset)
		result.Scores[rot] = score

		if score > bestScore {
			bestScore = score
			result.BestRotation = rot
		}
	}

	// Calculate confidence based on how much better the best score is
	// compared to the second best
	scores := make([]float64, 0, len(result.Scores))
	for _, s := range result.Scores {
		scores = append(scores, s)
	}
	sort.Float64s(scores)

	if len(scores) >= 2 && scores[len(scores)-1] > 0 {
		// Confidence = how much better best is than second best
		secondBest := scores[len(scores)-2]
		result.Confidence = (bestScore - secondBest) / bestScore
	}

	return result
}

// DetectRotationWithFeatures uses multiple feature types for more robust detection
func DetectRotationWithFeatures(source, target *ValetudoMap) RotationAnalysis {
	result := RotationAnalysis{
		Scores: make(map[float64]float64),
	}

	sourceFeatures := ExtractFeatures(source)
	targetFeatures := ExtractFeatures(target)

	// Get charger positions relative to centroid (asymmetric feature)
	sourceChargerOffset := Point{}
	targetChargerOffset := Point{}
	if sourceFeatures.HasCharger {
		sourceChargerOffset = Point{
			X: sourceFeatures.ChargerPosition.X - sourceFeatures.Centroid.X,
			Y: sourceFeatures.ChargerPosition.Y - sourceFeatures.Centroid.Y,
		}
	}
	if targetFeatures.HasCharger {
		targetChargerOffset = Point{
			X: targetFeatures.ChargerPosition.X - targetFeatures.Centroid.X,
			Y: targetFeatures.ChargerPosition.Y - targetFeatures.Centroid.Y,
		}
	}

	// Sample feature points for shape matching
	sourcePoints := SampleFeatures(sourceFeatures, 300)
	targetPoints := SampleFeatures(targetFeatures, 300)

	// Score each rotation using multiple methods
	for _, rot := range []float64{0, 90, 180, 270} {
		score := 0.0

		// 1. Feature point matching (most important)
		if len(sourcePoints) >= 10 && len(targetPoints) >= 10 {
			transform := buildRotationTransform(sourceFeatures.Centroid, targetFeatures.Centroid, rot)
			transformed := TransformPoints(sourcePoints, transform)
			dist := FeatureDistance(transformed, targetPoints)
			// Lower distance is better - use inverse scaled appropriately
			featureScore := 1.0 / (1.0 + dist/500.0)
			score += featureScore * 0.7 // 70% weight
		}

		// 2. Charger offset matching (strong asymmetric feature)
		if sourceFeatures.HasCharger && targetFeatures.HasCharger {
			// Rotate source charger offset
			rad := rot * math.Pi / 180
			rotatedOffset := Point{
				X: sourceChargerOffset.X*math.Cos(rad) - sourceChargerOffset.Y*math.Sin(rad),
				Y: sourceChargerOffset.X*math.Sin(rad) + sourceChargerOffset.Y*math.Cos(rad),
			}
			// Compare with target charger offset
			chargerDist := Distance(rotatedOffset, targetChargerOffset)
			chargerScore := 1.0 / (1.0 + chargerDist/200.0)
			score += chargerScore * 0.3 // 30% weight
		}

		result.Scores[rot] = score
	}

	// Find best rotation
	bestScore := -1.0
	for rot, score := range result.Scores {
		if score > bestScore {
			bestScore = score
			result.BestRotation = rot
		}
	}

	// Calculate confidence
	scores := make([]float64, 0, 4)
	for _, s := range result.Scores {
		scores = append(scores, s)
	}
	sort.Float64s(scores)
	if len(scores) >= 2 && scores[len(scores)-1] > 0 {
		secondBest := scores[len(scores)-2]
		result.Confidence = (bestScore - secondBest) / bestScore
	}

	// Get dominant angles for info
	sourceHist := ExtractWallAngles(source)
	targetHist := ExtractWallAngles(target)
	result.SourceAngles = sourceHist.DominantAngles(4)
	result.TargetAngles = targetHist.DominantAngles(4)

	return result
}

// buildRotationTransform creates a transform for rotation around centroids
func buildRotationTransform(sourceCentroid, targetCentroid Point, rotationDeg float64) AffineMatrix {
	toOrigin := Translation(-sourceCentroid.X, -sourceCentroid.Y)
	rotate := RotationDeg(rotationDeg)
	toTarget := Translation(targetCentroid.X, targetCentroid.Y)
	return MultiplyMatrices(toTarget, MultiplyMatrices(rotate, toOrigin))
}

// DetectRotationWithFeaturesDebug is like DetectRotationWithFeatures but prints debug info
func DetectRotationWithFeaturesDebug(source, target *ValetudoMap) RotationAnalysis {
	result := RotationAnalysis{
		Scores: make(map[float64]float64),
	}

	sourceFeatures := ExtractFeatures(source)
	targetFeatures := ExtractFeatures(target)

	fmt.Printf("  Source centroid: (%.0f, %.0f)\n", sourceFeatures.Centroid.X, sourceFeatures.Centroid.Y)
	fmt.Printf("  Target centroid: (%.0f, %.0f)\n", targetFeatures.Centroid.X, targetFeatures.Centroid.Y)

	// Get charger positions relative to centroid
	sourceChargerOffset := Point{}
	targetChargerOffset := Point{}
	if sourceFeatures.HasCharger {
		sourceChargerOffset = Point{
			X: sourceFeatures.ChargerPosition.X - sourceFeatures.Centroid.X,
			Y: sourceFeatures.ChargerPosition.Y - sourceFeatures.Centroid.Y,
		}
		fmt.Printf("  Source charger offset: (%.0f, %.0f)\n", sourceChargerOffset.X, sourceChargerOffset.Y)
	}
	if targetFeatures.HasCharger {
		targetChargerOffset = Point{
			X: targetFeatures.ChargerPosition.X - targetFeatures.Centroid.X,
			Y: targetFeatures.ChargerPosition.Y - targetFeatures.Centroid.Y,
		}
		fmt.Printf("  Target charger offset: (%.0f, %.0f)\n", targetChargerOffset.X, targetChargerOffset.Y)
	}

	sourcePoints := SampleFeatures(sourceFeatures, 300)
	targetPoints := SampleFeatures(targetFeatures, 300)

	fmt.Printf("  Feature point distances per rotation:\n")
	for _, rot := range []float64{0, 90, 180, 270} {
		score := 0.0
		featureDist := 0.0
		chargerDist := 0.0

		if len(sourcePoints) >= 10 && len(targetPoints) >= 10 {
			transform := buildRotationTransform(sourceFeatures.Centroid, targetFeatures.Centroid, rot)
			transformed := TransformPoints(sourcePoints, transform)
			featureDist = FeatureDistance(transformed, targetPoints)
			featureScore := 1.0 / (1.0 + featureDist/500.0)
			score += featureScore * 0.7
		}

		if sourceFeatures.HasCharger && targetFeatures.HasCharger {
			rad := rot * math.Pi / 180
			rotatedOffset := Point{
				X: sourceChargerOffset.X*math.Cos(rad) - sourceChargerOffset.Y*math.Sin(rad),
				Y: sourceChargerOffset.X*math.Sin(rad) + sourceChargerOffset.Y*math.Cos(rad),
			}
			chargerDist = Distance(rotatedOffset, targetChargerOffset)
			chargerScore := 1.0 / (1.0 + chargerDist/200.0)
			score += chargerScore * 0.3
		}

		fmt.Printf("    %3.0f°: feat_dist=%.0f, charger_dist=%.0f\n", rot, featureDist, chargerDist)
		result.Scores[rot] = score
	}

	// Find best
	bestScore := -1.0
	for rot, score := range result.Scores {
		if score > bestScore {
			bestScore = score
			result.BestRotation = rot
		}
	}

	// Confidence
	scores := make([]float64, 0, 4)
	for _, s := range result.Scores {
		scores = append(scores, s)
	}
	sort.Float64s(scores)
	if len(scores) >= 2 && scores[len(scores)-1] > 0 {
		secondBest := scores[len(scores)-2]
		result.Confidence = (bestScore - secondBest) / bestScore
	}

	return result
}
