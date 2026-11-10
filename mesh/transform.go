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
	// theta = atan2(sum(sx*ty - sy*tx), sum(sx*tx + sy*ty)) = atan2(h12 - h21, h11 + h22)
	theta := math.Atan2(h12-h21, h11+h22)

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
