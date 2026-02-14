package mesh

import "math"

// TransformPoint applies an affine transform to a point
// x' = a*x + b*y + tx
// y' = c*x + d*y + ty
func TransformPoint(p Point, m AffineMatrix) Point {
	return Point{
		X: m.A*p.X + m.B*p.Y + m.Tx,
		Y: m.C*p.X + m.D*p.Y + m.Ty,
	}
}

// TransformPoints applies an affine transform to multiple points
func TransformPoints(points []Point, m AffineMatrix) []Point {
	result := make([]Point, len(points))
	for i, p := range points {
		result[i] = TransformPoint(p, m)
	}
	return result
}

// NormalizeAngle normalizes an angle in degrees to the range [0, 360).
func NormalizeAngle(degrees float64) float64 {
	degrees = math.Mod(degrees, 360)
	if degrees < 0 {
		degrees += 360
	}
	return degrees
}

// TransformAngle applies the rotation component of an affine transform to a local angle (in degrees).
// The rotation is extracted from the transform matrix via atan2(C, A).
// Returns the transformed angle normalized to [0, 360).
func TransformAngle(localAngle float64, transform AffineMatrix) float64 {
	transformRotation := math.Atan2(transform.C, transform.A) * 180 / math.Pi
	return NormalizeAngle(localAngle + transformRotation)
}

// MultiplyMatrices composes two affine transforms: result = m1 * m2
// Applying result is equivalent to applying m2 first, then m1
func MultiplyMatrices(m1, m2 AffineMatrix) AffineMatrix {
	return AffineMatrix{
		A:  m1.A*m2.A + m1.B*m2.C,
		B:  m1.A*m2.B + m1.B*m2.D,
		Tx: m1.A*m2.Tx + m1.B*m2.Ty + m1.Tx,
		C:  m1.C*m2.A + m1.D*m2.C,
		D:  m1.C*m2.B + m1.D*m2.D,
		Ty: m1.C*m2.Tx + m1.D*m2.Ty + m1.Ty,
	}
}

// InvertMatrix computes the inverse of an affine transform
// Returns identity if matrix is singular (determinant ~= 0)
func InvertMatrix(m AffineMatrix) AffineMatrix {
	det := m.A*m.D - m.B*m.C
	if math.Abs(det) < 1e-10 {
		return Identity()
	}

	invDet := 1.0 / det
	return AffineMatrix{
		A:  m.D * invDet,
		B:  -m.B * invDet,
		Tx: (m.B*m.Ty - m.D*m.Tx) * invDet,
		C:  -m.C * invDet,
		D:  m.A * invDet,
		Ty: (m.C*m.Tx - m.A*m.Ty) * invDet,
	}
}

// Translation creates a translation-only transform
func Translation(tx, ty float64) AffineMatrix {
	return AffineMatrix{A: 1, B: 0, Tx: tx, C: 0, D: 1, Ty: ty}
}

// Rotation creates a rotation transform (angle in radians, around origin)
func Rotation(angle float64) AffineMatrix {
	cos := math.Cos(angle)
	sin := math.Sin(angle)
	return AffineMatrix{A: cos, B: -sin, Tx: 0, C: sin, D: cos, Ty: 0}
}

// RotationDeg creates a rotation transform (angle in degrees, around origin)
func RotationDeg(degrees float64) AffineMatrix {
	return Rotation(degrees * math.Pi / 180.0)
}

// CreateRotationTranslation creates a combined rotation + translation transform
// Rotation is applied first (around origin), then translation
func CreateRotationTranslation(degrees, tx, ty float64) AffineMatrix {
	rot := RotationDeg(degrees)
	return AffineMatrix{
		A:  rot.A,
		B:  rot.B,
		Tx: tx,
		C:  rot.C,
		D:  rot.D,
		Ty: ty,
	}
}

// Scale creates a scaling transform
func Scale(sx, sy float64) AffineMatrix {
	return AffineMatrix{A: sx, B: 0, Tx: 0, C: 0, D: sy, Ty: 0}
}

// CalculateFromPointPairs computes the best-fit affine transform using least squares
// Maps source points to target points: target ≈ transform(source)
// Requires at least 3 non-collinear point pairs for a full affine transform
// With 2 points, returns translation + rotation + uniform scale only
func CalculateFromPointPairs(source, target []Point) AffineMatrix {
	n := len(source)
	if n == 0 || n != len(target) {
		return Identity()
	}

	if n == 1 {
		// Single point: translation only
		return Translation(target[0].X-source[0].X, target[0].Y-source[0].Y)
	}

	if n == 2 {
		// Two points: similarity transform (translation + rotation + uniform scale)
		return calculateSimilarityTransform(source, target)
	}

	// 3+ points: full affine transform using least squares
	return calculateAffineTransform(source, target)
}

// calculateSimilarityTransform computes translation + rotation + uniform scale from 2 point pairs
func calculateSimilarityTransform(source, target []Point) AffineMatrix {
	// Source vector
	sx := source[1].X - source[0].X
	sy := source[1].Y - source[0].Y
	srcLen := math.Sqrt(sx*sx + sy*sy)

	// Target vector
	tx := target[1].X - target[0].X
	ty := target[1].Y - target[0].Y
	tgtLen := math.Sqrt(tx*tx + ty*ty)

	if srcLen < 1e-10 || tgtLen < 1e-10 {
		return Identity()
	}

	// Scale factor
	scale := tgtLen / srcLen

	// Rotation angle
	srcAngle := math.Atan2(sy, sx)
	tgtAngle := math.Atan2(ty, tx)
	angle := tgtAngle - srcAngle

	cos := math.Cos(angle)
	sin := math.Sin(angle)

	// Transform: first scale+rotate around origin, then translate
	// We want: target[0] = transform(source[0])
	// So: tx = target[0].x - (scale*cos*source[0].x - scale*sin*source[0].y)
	a := scale * cos
	b := -scale * sin
	c := scale * sin
	d := scale * cos

	translateX := target[0].X - (a*source[0].X + b*source[0].Y)
	translateY := target[0].Y - (c*source[0].X + d*source[0].Y)

	return AffineMatrix{
		A: a, B: b, Tx: translateX,
		C: c, D: d, Ty: translateY,
	}
}

// calculateAffineTransform computes full affine transform using least squares
// Solves the system: [x' y'] = [x y 1] * [[a c] [b d] [tx ty]]
func calculateAffineTransform(source, target []Point) AffineMatrix {
	n := float64(len(source))

	// Compute sums for normal equations
	var sumX, sumY, sumXX, sumXY, sumYY float64
	var sumXp, sumYp, sumXXp, sumXYp, sumYXp, sumYYp float64

	for i := range source {
		x, y := source[i].X, source[i].Y
		xp, yp := target[i].X, target[i].Y

		sumX += x
		sumY += y
		sumXX += x * x
		sumXY += x * y
		sumYY += y * y
		sumXp += xp
		sumYp += yp
		sumXXp += x * xp
		sumXYp += x * yp
		sumYXp += y * xp
		sumYYp += y * yp
	}

	// Solve 3x3 linear system for [a, b, tx] and [c, d, ty]
	// Using Cramer's rule for simplicity
	// Matrix: [[sumXX, sumXY, sumX], [sumXY, sumYY, sumY], [sumX, sumY, n]]

	det := sumXX*(sumYY*n-sumY*sumY) - sumXY*(sumXY*n-sumY*sumX) + sumX*(sumXY*sumY-sumYY*sumX)
	if math.Abs(det) < 1e-10 {
		return Identity()
	}

	invDet := 1.0 / det

	// Solve for a, b, tx (maps to x')
	detA := sumXXp*(sumYY*n-sumY*sumY) - sumXY*(sumYXp*n-sumY*sumXp) + sumX*(sumYXp*sumY-sumYY*sumXp)
	detB := sumXX*(sumYXp*n-sumY*sumXp) - sumXXp*(sumXY*n-sumY*sumX) + sumX*(sumXY*sumXp-sumYXp*sumX)
	detTx := sumXX*(sumYY*sumXp-sumYXp*sumY) - sumXY*(sumXY*sumXp-sumYXp*sumX) + sumXXp*(sumXY*sumY-sumYY*sumX)

	a := detA * invDet
	b := detB * invDet
	tx := detTx * invDet

	// Solve for c, d, ty (maps to y')
	detC := sumXYp*(sumYY*n-sumY*sumY) - sumXY*(sumYYp*n-sumY*sumYp) + sumX*(sumYYp*sumY-sumYY*sumYp)
	detD := sumXX*(sumYYp*n-sumY*sumYp) - sumXYp*(sumXY*n-sumY*sumX) + sumX*(sumXY*sumYp-sumYYp*sumX)
	detTy := sumXX*(sumYY*sumYp-sumYYp*sumY) - sumXY*(sumXY*sumYp-sumYYp*sumX) + sumXYp*(sumXY*sumY-sumYY*sumX)

	c := detC * invDet
	d := detD * invDet
	ty := detTy * invDet

	return AffineMatrix{A: a, B: b, Tx: tx, C: c, D: d, Ty: ty}
}

// Distance calculates Euclidean distance between two points
func Distance(p1, p2 Point) float64 {
	dx := p2.X - p1.X
	dy := p2.Y - p1.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// Centroid calculates the center of mass of a set of points
func Centroid(points []Point) Point {
	if len(points) == 0 {
		return Point{}
	}
	var sumX, sumY float64
	for _, p := range points {
		sumX += p.X
		sumY += p.Y
	}
	n := float64(len(points))
	return Point{X: sumX / n, Y: sumY / n}
}

// CalculateRigidTransform computes the best rigid transform (rotation + translation only, no scale)
// using Procrustes analysis. This is more robust for map alignment where scale is known.
func CalculateRigidTransform(source, target []Point) AffineMatrix {
	n := len(source)
	if n < 2 || n != len(target) {
		return Identity()
	}

	// Compute centroids
	srcCentroid := Centroid(source)
	tgtCentroid := Centroid(target)

	// Center the point sets
	var srcCentered, tgtCentered []Point
	for i := range source {
		srcCentered = append(srcCentered, Point{
			X: source[i].X - srcCentroid.X,
			Y: source[i].Y - srcCentroid.Y,
		})
		tgtCentered = append(tgtCentered, Point{
			X: target[i].X - tgtCentroid.X,
			Y: target[i].Y - tgtCentroid.Y,
		})
	}

	// Compute cross-covariance matrix H = src^T * tgt
	// H = [h11 h12]
	//     [h21 h22]
	var h11, h12, h21, h22 float64
	for i := range srcCentered {
		h11 += srcCentered[i].X * tgtCentered[i].X
		h12 += srcCentered[i].X * tgtCentered[i].Y
		h21 += srcCentered[i].Y * tgtCentered[i].X
		h22 += srcCentered[i].Y * tgtCentered[i].Y
	}

	// For 2D, we can directly compute the rotation angle using atan2
	// The optimal rotation minimizes sum of squared distances
	// theta = atan2(h21 - h12, h11 + h22)
	theta := math.Atan2(h21-h12, h11+h22)

	cos := math.Cos(theta)
	sin := math.Sin(theta)

	// Build rotation matrix
	// R = [cos -sin]
	//     [sin  cos]
	a := cos
	b := -sin
	c := sin
	d := cos

	// Compute translation: t = tgtCentroid - R * srcCentroid
	tx := tgtCentroid.X - (a*srcCentroid.X + b*srcCentroid.Y)
	ty := tgtCentroid.Y - (c*srcCentroid.X + d*srcCentroid.Y)

	return AffineMatrix{A: a, B: b, Tx: tx, C: c, D: d, Ty: ty}
}

// CalculateWeightedRigidTransform computes the best rigid transform using weighted Procrustes analysis.
// weights slice must have the same length as source and target.
func CalculateWeightedRigidTransform(source, target []Point, weights []float64) AffineMatrix {
	n := len(source)
	if n < 2 || n != len(target) || n != len(weights) {
		return Identity()
	}

	// Compute weighted centroids
	totalWeight := 0.0
	var srcSumX, srcSumY, tgtSumX, tgtSumY float64
	for i := range source {
		w := weights[i]
		totalWeight += w
		srcSumX += source[i].X * w
		srcSumY += source[i].Y * w
		tgtSumX += target[i].X * w
		tgtSumY += target[i].Y * w
	}

	if totalWeight <= 0 {
		return CalculateRigidTransform(source, target)
	}

	srcCentroid := Point{X: srcSumX / totalWeight, Y: srcSumY / totalWeight}
	tgtCentroid := Point{X: tgtSumX / totalWeight, Y: tgtSumY / totalWeight}

	// Compute weighted cross-covariance H
	var h11, h12, h21, h22 float64
	for i := range source {
		w := weights[i]
		sx := source[i].X - srcCentroid.X
		sy := source[i].Y - srcCentroid.Y
		tx := target[i].X - tgtCentroid.X
		ty := target[i].Y - tgtCentroid.Y

		h11 += w * sx * tx
		h12 += w * sx * ty
		h21 += w * sy * tx
		h22 += w * sy * ty
	}

	theta := math.Atan2(h21-h12, h11+h22)
	cos := math.Cos(theta)
	sin := math.Sin(theta)

	a, b, c, d := cos, -sin, sin, cos
	tx := tgtCentroid.X - (a*srcCentroid.X + b*srcCentroid.Y)
	ty := tgtCentroid.Y - (c*srcCentroid.X + d*srcCentroid.Y)

	return AffineMatrix{A: a, B: b, Tx: tx, C: c, D: d, Ty: ty}
}
