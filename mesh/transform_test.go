package mesh

import (
	"math"
	"testing"
)

const epsilon = 1e-10

// almostEqual checks if two floats are equal within epsilon tolerance
func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

// matricesEqual checks if two affine matrices are equal within epsilon tolerance
func matricesEqual(m1, m2 AffineMatrix) bool {
	return almostEqual(m1.A, m2.A) &&
		almostEqual(m1.B, m2.B) &&
		almostEqual(m1.Tx, m2.Tx) &&
		almostEqual(m1.C, m2.C) &&
		almostEqual(m1.D, m2.D) &&
		almostEqual(m1.Ty, m2.Ty)
}

// pointsEqual checks if two points are equal within epsilon tolerance
func pointsEqual(p1, p2 Point) bool {
	return almostEqual(p1.X, p2.X) && almostEqual(p1.Y, p2.Y)
}

func TestTransformPoint(t *testing.T) {
	tests := []struct {
		name   string
		point  Point
		matrix AffineMatrix
		want   Point
	}{
		{
			name:   "identity transform",
			point:  Point{X: 10, Y: 20},
			matrix: Identity(),
			want:   Point{X: 10, Y: 20},
		},
		{
			name:   "translation only",
			point:  Point{X: 5, Y: 5},
			matrix: Translation(10, 15),
			want:   Point{X: 15, Y: 20},
		},
		{
			name:   "scale 2x",
			point:  Point{X: 3, Y: 4},
			matrix: Scale(2, 2),
			want:   Point{X: 6, Y: 8},
		},
		{
			name:   "90 degree rotation",
			point:  Point{X: 1, Y: 0},
			matrix: RotationDeg(90),
			want:   Point{X: 0, Y: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TransformPoint(tt.point, tt.matrix)
			if !pointsEqual(got, tt.want) {
				t.Errorf("TransformPoint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransformPoints(t *testing.T) {
	tests := []struct {
		name   string
		points []Point
		matrix AffineMatrix
		want   []Point
	}{
		{
			name:   "empty slice",
			points: []Point{},
			matrix: Identity(),
			want:   []Point{},
		},
		{
			name:   "translate multiple points",
			points: []Point{{X: 0, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 2}},
			matrix: Translation(5, 10),
			want:   []Point{{X: 5, Y: 10}, {X: 6, Y: 11}, {X: 7, Y: 12}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TransformPoints(tt.points, tt.matrix)
			if len(got) != len(tt.want) {
				t.Fatalf("TransformPoints() length = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if !pointsEqual(got[i], tt.want[i]) {
					t.Errorf("TransformPoints()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMultiplyMatrices(t *testing.T) {
	tests := []struct {
		name string
		m1   AffineMatrix
		m2   AffineMatrix
		want AffineMatrix
	}{
		{
			name: "identity * identity",
			m1:   Identity(),
			m2:   Identity(),
			want: Identity(),
		},
		{
			name: "identity * translation",
			m1:   Identity(),
			m2:   Translation(5, 10),
			want: Translation(5, 10),
		},
		{
			name: "translation * identity",
			m1:   Translation(5, 10),
			m2:   Identity(),
			want: Translation(5, 10),
		},
		{
			name: "two translations",
			m1:   Translation(5, 10),
			m2:   Translation(3, 7),
			want: Translation(8, 17),
		},
		{
			name: "rotation * scale",
			m1:   RotationDeg(90),
			m2:   Scale(2, 2),
			want: AffineMatrix{A: 0, B: -2, Tx: 0, C: 2, D: 0, Ty: 0},
		},
		{
			name: "composition associativity check",
			m1:   Translation(10, 20),
			m2:   RotationDeg(45),
			want: AffineMatrix{
				A:  math.Cos(45 * math.Pi / 180),
				B:  -math.Sin(45 * math.Pi / 180),
				Tx: 10,
				C:  math.Sin(45 * math.Pi / 180),
				D:  math.Cos(45 * math.Pi / 180),
				Ty: 20,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MultiplyMatrices(tt.m1, tt.m2)
			if !matricesEqual(got, tt.want) {
				t.Errorf("MultiplyMatrices() = %+v, want %+v", got, tt.want)
			}
		})
	}

	// Test associativity property: (A * B) * C = A * (B * C)
	t.Run("associativity property", func(t *testing.T) {
		m1 := Translation(5, 10)
		m2 := RotationDeg(30)
		m3 := Scale(2, 3)

		left := MultiplyMatrices(MultiplyMatrices(m1, m2), m3)
		right := MultiplyMatrices(m1, MultiplyMatrices(m2, m3))

		if !matricesEqual(left, right) {
			t.Errorf("Associativity failed: (m1*m2)*m3 != m1*(m2*m3)")
		}
	})
}

func TestInvertMatrix(t *testing.T) {
	tests := []struct {
		name string
		m    AffineMatrix
		want AffineMatrix
	}{
		{
			name: "identity inverse",
			m:    Identity(),
			want: Identity(),
		},
		{
			name: "translation inverse",
			m:    Translation(5, 10),
			want: Translation(-5, -10),
		},
		{
			name: "scale inverse",
			m:    Scale(2, 3),
			want: Scale(0.5, 1.0/3.0),
		},
		{
			name: "rotation inverse",
			m:    RotationDeg(45),
			want: RotationDeg(-45),
		},
		{
			name: "singular matrix (zero determinant)",
			m:    AffineMatrix{A: 1, B: 2, Tx: 0, C: 2, D: 4, Ty: 0}, // det = 1*4 - 2*2 = 0
			want: Identity(),                                         // Returns identity for singular matrices
		},
		{
			name: "near-singular matrix",
			m:    AffineMatrix{A: 1, B: 2, Tx: 5, C: 2, D: 4.000000000001, Ty: 10}, // det = 1e-12 < 1e-10
			want: Identity(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InvertMatrix(tt.m)
			if !matricesEqual(got, tt.want) {
				t.Errorf("InvertMatrix() = %+v, want %+v", got, tt.want)
			}
		})
	}

	// Test property: M * M^-1 = I (for non-singular matrices)
	t.Run("inversion property M * M^-1 = I", func(t *testing.T) {
		matrices := []AffineMatrix{
			Translation(10, 20),
			Scale(2, 3),
			RotationDeg(60),
			CreateRotationTranslation(30, 5, 10),
		}

		for i, m := range matrices {
			inv := InvertMatrix(m)
			product := MultiplyMatrices(m, inv)
			if !matricesEqual(product, Identity()) {
				t.Errorf("Test %d: M * M^-1 != I, got %+v", i, product)
			}
		}
	})

	// Test property: (M^-1)^-1 = M
	t.Run("double inversion property", func(t *testing.T) {
		m := CreateRotationTranslation(45, 10, 20)
		inv := InvertMatrix(m)
		invInv := InvertMatrix(inv)
		if !matricesEqual(invInv, m) {
			t.Errorf("(M^-1)^-1 != M")
		}
	})
}

func TestTranslation(t *testing.T) {
	tests := []struct {
		name string
		tx   float64
		ty   float64
		want AffineMatrix
	}{
		{
			name: "zero translation",
			tx:   0,
			ty:   0,
			want: Identity(),
		},
		{
			name: "positive translation",
			tx:   10,
			ty:   20,
			want: AffineMatrix{A: 1, B: 0, Tx: 10, C: 0, D: 1, Ty: 20},
		},
		{
			name: "negative translation",
			tx:   -5,
			ty:   -15,
			want: AffineMatrix{A: 1, B: 0, Tx: -5, C: 0, D: 1, Ty: -15},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Translation(tt.tx, tt.ty)
			if !matricesEqual(got, tt.want) {
				t.Errorf("Translation() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRotation(t *testing.T) {
	tests := []struct {
		name  string
		angle float64
		point Point
		want  Point
	}{
		{
			name:  "0 degrees",
			angle: 0,
			point: Point{X: 1, Y: 0},
			want:  Point{X: 1, Y: 0},
		},
		{
			name:  "90 degrees",
			angle: math.Pi / 2,
			point: Point{X: 1, Y: 0},
			want:  Point{X: 0, Y: 1},
		},
		{
			name:  "180 degrees",
			angle: math.Pi,
			point: Point{X: 1, Y: 0},
			want:  Point{X: -1, Y: 0},
		},
		{
			name:  "270 degrees",
			angle: 3 * math.Pi / 2,
			point: Point{X: 1, Y: 0},
			want:  Point{X: 0, Y: -1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Rotation(tt.angle)
			got := TransformPoint(tt.point, m)
			if !pointsEqual(got, tt.want) {
				t.Errorf("Rotation(%f) applied to %v = %v, want %v", tt.angle, tt.point, got, tt.want)
			}
		})
	}
}

func TestRotationDeg(t *testing.T) {
	tests := []struct {
		name    string
		degrees float64
		point   Point
		want    Point
	}{
		{
			name:    "0 degrees",
			degrees: 0,
			point:   Point{X: 1, Y: 0},
			want:    Point{X: 1, Y: 0},
		},
		{
			name:    "90 degrees",
			degrees: 90,
			point:   Point{X: 1, Y: 0},
			want:    Point{X: 0, Y: 1},
		},
		{
			name:    "180 degrees",
			degrees: 180,
			point:   Point{X: 1, Y: 0},
			want:    Point{X: -1, Y: 0},
		},
		{
			name:    "270 degrees",
			degrees: 270,
			point:   Point{X: 1, Y: 0},
			want:    Point{X: 0, Y: -1},
		},
		{
			name:    "45 degrees",
			degrees: 45,
			point:   Point{X: 1, Y: 0},
			want:    Point{X: math.Sqrt(2) / 2, Y: math.Sqrt(2) / 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := RotationDeg(tt.degrees)
			got := TransformPoint(tt.point, m)
			if !pointsEqual(got, tt.want) {
				t.Errorf("RotationDeg(%f) applied to %v = %v, want %v", tt.degrees, tt.point, got, tt.want)
			}
		})
	}
}

func TestCreateRotationTranslation(t *testing.T) {
	tests := []struct {
		name    string
		degrees float64
		tx      float64
		ty      float64
		point   Point
		want    Point
	}{
		{
			name:    "0 degree rotation with translation",
			degrees: 0,
			tx:      10,
			ty:      20,
			point:   Point{X: 5, Y: 5},
			want:    Point{X: 15, Y: 25},
		},
		{
			name:    "90 degree rotation with translation",
			degrees: 90,
			tx:      10,
			ty:      20,
			point:   Point{X: 1, Y: 0},
			want:    Point{X: 10, Y: 21},
		},
		{
			name:    "180 degree rotation with translation",
			degrees: 180,
			tx:      5,
			ty:      5,
			point:   Point{X: 1, Y: 0},
			want:    Point{X: 4, Y: 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := CreateRotationTranslation(tt.degrees, tt.tx, tt.ty)
			got := TransformPoint(tt.point, m)
			if !pointsEqual(got, tt.want) {
				t.Errorf("CreateRotationTranslation(%f, %f, %f) applied to %v = %v, want %v",
					tt.degrees, tt.tx, tt.ty, tt.point, got, tt.want)
			}
		})
	}
}

func TestScale(t *testing.T) {
	tests := []struct {
		name  string
		sx    float64
		sy    float64
		point Point
		want  Point
	}{
		{
			name:  "identity scale",
			sx:    1,
			sy:    1,
			point: Point{X: 5, Y: 10},
			want:  Point{X: 5, Y: 10},
		},
		{
			name:  "uniform scale 2x",
			sx:    2,
			sy:    2,
			point: Point{X: 3, Y: 4},
			want:  Point{X: 6, Y: 8},
		},
		{
			name:  "non-uniform scale",
			sx:    2,
			sy:    3,
			point: Point{X: 5, Y: 10},
			want:  Point{X: 10, Y: 30},
		},
		{
			name:  "scale down",
			sx:    0.5,
			sy:    0.25,
			point: Point{X: 10, Y: 20},
			want:  Point{X: 5, Y: 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Scale(tt.sx, tt.sy)
			got := TransformPoint(tt.point, m)
			if !pointsEqual(got, tt.want) {
				t.Errorf("Scale(%f, %f) applied to %v = %v, want %v", tt.sx, tt.sy, tt.point, got, tt.want)
			}
		})
	}
}

func TestCalculateFromPointPairs(t *testing.T) {
	tests := []struct {
		name   string
		source []Point
		target []Point
		verify []struct {
			src  Point
			want Point
		}
	}{
		{
			name:   "empty points",
			source: []Point{},
			target: []Point{},
			verify: []struct {
				src  Point
				want Point
			}{
				{src: Point{X: 5, Y: 10}, want: Point{X: 5, Y: 10}}, // Identity transform
			},
		},
		{
			name:   "mismatched lengths",
			source: []Point{{X: 0, Y: 0}},
			target: []Point{{X: 1, Y: 1}, {X: 2, Y: 2}},
			verify: []struct {
				src  Point
				want Point
			}{
				{src: Point{X: 5, Y: 10}, want: Point{X: 5, Y: 10}}, // Identity transform
			},
		},
		{
			name:   "single point - translation only",
			source: []Point{{X: 0, Y: 0}},
			target: []Point{{X: 10, Y: 20}},
			verify: []struct {
				src  Point
				want Point
			}{
				{src: Point{X: 0, Y: 0}, want: Point{X: 10, Y: 20}},
				{src: Point{X: 5, Y: 5}, want: Point{X: 15, Y: 25}},
			},
		},
		{
			name:   "two points - similarity transform (translation + rotation)",
			source: []Point{{X: 0, Y: 0}, {X: 1, Y: 0}},
			target: []Point{{X: 0, Y: 0}, {X: 0, Y: 1}}, // 90 degree rotation
			verify: []struct {
				src  Point
				want Point
			}{
				{src: Point{X: 0, Y: 0}, want: Point{X: 0, Y: 0}},
				{src: Point{X: 1, Y: 0}, want: Point{X: 0, Y: 1}},
			},
		},
		{
			name:   "two points - with scale",
			source: []Point{{X: 0, Y: 0}, {X: 1, Y: 0}},
			target: []Point{{X: 0, Y: 0}, {X: 2, Y: 0}}, // 2x scale
			verify: []struct {
				src  Point
				want Point
			}{
				{src: Point{X: 0, Y: 0}, want: Point{X: 0, Y: 0}},
				{src: Point{X: 1, Y: 0}, want: Point{X: 2, Y: 0}},
				{src: Point{X: 2, Y: 0}, want: Point{X: 4, Y: 0}},
			},
		},
		{
			name: "three points - affine transform",
			source: []Point{
				{X: 0, Y: 0},
				{X: 1, Y: 0},
				{X: 0, Y: 1},
			},
			target: []Point{
				{X: 10, Y: 20},
				{X: 12, Y: 20},
				{X: 10, Y: 23},
			},
			verify: []struct {
				src  Point
				want Point
			}{
				{src: Point{X: 0, Y: 0}, want: Point{X: 10, Y: 20}},
				{src: Point{X: 1, Y: 0}, want: Point{X: 12, Y: 20}},
				{src: Point{X: 0, Y: 1}, want: Point{X: 10, Y: 23}},
			},
		},
		{
			name: "four points - overdetermined system",
			source: []Point{
				{X: 0, Y: 0},
				{X: 10, Y: 0},
				{X: 10, Y: 10},
				{X: 0, Y: 10},
			},
			target: []Point{
				{X: 0, Y: 0},
				{X: 20, Y: 0},
				{X: 20, Y: 20},
				{X: 0, Y: 20},
			},
			verify: []struct {
				src  Point
				want Point
			}{
				{src: Point{X: 5, Y: 5}, want: Point{X: 10, Y: 10}}, // 2x scale
			},
		},
		{
			name:   "degenerate two points (same point)",
			source: []Point{{X: 5, Y: 5}, {X: 5, Y: 5}},
			target: []Point{{X: 10, Y: 10}, {X: 10, Y: 10}},
			verify: []struct {
				src  Point
				want Point
			}{
				{src: Point{X: 5, Y: 5}, want: Point{X: 5, Y: 5}}, // Returns identity
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := CalculateFromPointPairs(tt.source, tt.target)

			// Verify the transform works correctly on test points
			for i, v := range tt.verify {
				got := TransformPoint(v.src, m)
				if !pointsEqual(got, v.want) {
					t.Errorf("verify[%d]: TransformPoint(%v) = %v, want %v (matrix: %+v)",
						i, v.src, got, v.want, m)
				}
			}
		})
	}
}

func TestDistance(t *testing.T) {
	tests := []struct {
		name string
		p1   Point
		p2   Point
		want float64
	}{
		{
			name: "same point",
			p1:   Point{X: 5, Y: 10},
			p2:   Point{X: 5, Y: 10},
			want: 0,
		},
		{
			name: "horizontal distance",
			p1:   Point{X: 0, Y: 0},
			p2:   Point{X: 5, Y: 0},
			want: 5,
		},
		{
			name: "vertical distance",
			p1:   Point{X: 0, Y: 0},
			p2:   Point{X: 0, Y: 5},
			want: 5,
		},
		{
			name: "diagonal distance (3-4-5 triangle)",
			p1:   Point{X: 0, Y: 0},
			p2:   Point{X: 3, Y: 4},
			want: 5,
		},
		{
			name: "negative coordinates",
			p1:   Point{X: -3, Y: -4},
			p2:   Point{X: 0, Y: 0},
			want: 5,
		},
		{
			name: "unit circle point",
			p1:   Point{X: 0, Y: 0},
			p2:   Point{X: 1, Y: 1},
			want: math.Sqrt(2),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Distance(tt.p1, tt.p2)
			if !almostEqual(got, tt.want) {
				t.Errorf("Distance(%v, %v) = %v, want %v", tt.p1, tt.p2, got, tt.want)
			}
		})
	}
}

func TestCentroid(t *testing.T) {
	tests := []struct {
		name   string
		points []Point
		want   Point
	}{
		{
			name:   "empty points",
			points: []Point{},
			want:   Point{X: 0, Y: 0},
		},
		{
			name:   "single point",
			points: []Point{{X: 5, Y: 10}},
			want:   Point{X: 5, Y: 10},
		},
		{
			name:   "two points",
			points: []Point{{X: 0, Y: 0}, {X: 10, Y: 20}},
			want:   Point{X: 5, Y: 10},
		},
		{
			name:   "square corners",
			points: []Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
			want:   Point{X: 5, Y: 5},
		},
		{
			name:   "triangle",
			points: []Point{{X: 0, Y: 0}, {X: 6, Y: 0}, {X: 3, Y: 6}},
			want:   Point{X: 3, Y: 2},
		},
		{
			name:   "negative coordinates",
			points: []Point{{X: -5, Y: -10}, {X: 5, Y: 10}},
			want:   Point{X: 0, Y: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Centroid(tt.points)
			if !pointsEqual(got, tt.want) {
				t.Errorf("Centroid(%v) = %v, want %v", tt.points, got, tt.want)
			}
		})
	}
}

func TestCalculateRigidTransform(t *testing.T) {
	tests := []struct {
		name          string
		source        []Point
		target        []Point
		shouldBeExact bool // Whether we expect exact mapping (for well-formed rigid transforms)
	}{
		{
			name:   "less than 2 points",
			source: []Point{{X: 0, Y: 0}},
			target: []Point{{X: 10, Y: 10}},
		},
		{
			name:   "mismatched lengths",
			source: []Point{{X: 0, Y: 0}, {X: 1, Y: 0}},
			target: []Point{{X: 0, Y: 0}},
		},
		{
			name: "pure translation",
			source: []Point{
				{X: 0, Y: 0},
				{X: 1, Y: 0},
				{X: 0, Y: 1},
			},
			target: []Point{
				{X: 10, Y: 20},
				{X: 11, Y: 20},
				{X: 10, Y: 21},
			},
			shouldBeExact: true,
		},
		{
			name: "rotation + translation - square",
			source: []Point{
				{X: 0, Y: 0},
				{X: 10, Y: 0},
				{X: 10, Y: 10},
				{X: 0, Y: 10},
			},
			target: []Point{
				{X: 100, Y: 95},
				{X: 110, Y: 95},
				{X: 110, Y: 105},
				{X: 100, Y: 105},
			},
			shouldBeExact: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := CalculateRigidTransform(tt.source, tt.target)

			// For invalid inputs, should return identity
			if len(tt.source) < 2 || len(tt.source) != len(tt.target) {
				if !matricesEqual(m, Identity()) {
					t.Errorf("Expected identity matrix for invalid input, got %+v", m)
				}
				return
			}

			// Verify it's a rigid transform (rotation + translation, no scale)
			// Check that the determinant is 1 (no scale)
			det := m.A*m.D - m.B*m.C
			if !almostEqual(math.Abs(det), 1.0) {
				t.Errorf("Not a rigid transform: det = %v, want ±1", det)
			}

			// Verify distances are preserved (key property of rigid transforms)
			for i := 0; i < len(tt.source)-1; i++ {
				for j := i + 1; j < len(tt.source); j++ {
					srcDist := Distance(tt.source[i], tt.source[j])
					transformed_i := TransformPoint(tt.source[i], m)
					transformed_j := TransformPoint(tt.source[j], m)
					transformedDist := Distance(transformed_i, transformed_j)
					if !almostEqual(srcDist, transformedDist) {
						t.Errorf("Does not preserve distance between points %d and %d: src=%v, transformed=%v",
							i, j, srcDist, transformedDist)
					}
				}
			}

			// For well-formed rigid transforms (pure translation or rotation+translation of symmetric shapes),
			// verify the mapping is exact
			if tt.shouldBeExact {
				var totalErrorSq float64
				for i := range tt.source {
					transformed := TransformPoint(tt.source[i], m)
					dx := transformed.X - tt.target[i].X
					dy := transformed.Y - tt.target[i].Y
					totalErrorSq += dx*dx + dy*dy
				}
				rmsError := math.Sqrt(totalErrorSq / float64(len(tt.source)))

				if rmsError > 0.01 {
					t.Errorf("RMS error too high: %v (should be < 0.01 for exact rigid transform)", rmsError)
					for i := range tt.source {
						transformed := TransformPoint(tt.source[i], m)
						t.Logf("  source[%d]=%v -> %v (target=%v, error=%v)",
							i, tt.source[i], transformed, tt.target[i],
							Distance(transformed, tt.target[i]))
					}
				}
			}
		})
	}
}

// Benchmarks for critical paths

func BenchmarkMultiplyMatrices(b *testing.B) {
	m1 := CreateRotationTranslation(45, 100, 200)
	m2 := Scale(2, 3)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = MultiplyMatrices(m1, m2)
	}
}

func BenchmarkInvertMatrix(b *testing.B) {
	m := CreateRotationTranslation(30, 50, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = InvertMatrix(m)
	}
}

func BenchmarkTransformPoint(b *testing.B) {
	m := CreateRotationTranslation(45, 100, 200)
	p := Point{X: 50, Y: 75}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = TransformPoint(p, m)
	}
}

func BenchmarkCalculateFromPointPairs_2Points(b *testing.B) {
	source := []Point{{X: 0, Y: 0}, {X: 10, Y: 0}}
	target := []Point{{X: 5, Y: 5}, {X: 15, Y: 5}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateFromPointPairs(source, target)
	}
}

func BenchmarkCalculateFromPointPairs_4Points(b *testing.B) {
	source := []Point{
		{X: 0, Y: 0},
		{X: 10, Y: 0},
		{X: 10, Y: 10},
		{X: 0, Y: 10},
	}
	target := []Point{
		{X: 0, Y: 0},
		{X: 20, Y: 0},
		{X: 20, Y: 20},
		{X: 0, Y: 20},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateFromPointPairs(source, target)
	}
}

func BenchmarkCalculateRigidTransform(b *testing.B) {
	source := []Point{
		{X: 0, Y: 0},
		{X: 10, Y: 0},
		{X: 10, Y: 10},
		{X: 0, Y: 10},
	}
	target := []Point{
		{X: 5, Y: 5},
		{X: 15, Y: 5},
		{X: 15, Y: 15},
		{X: 5, Y: 15},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateRigidTransform(source, target)
	}
}

func BenchmarkDistance(b *testing.B) {
	p1 := Point{X: 10, Y: 20}
	p2 := Point{X: 50, Y: 80}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Distance(p1, p2)
	}
}

func BenchmarkCentroid(b *testing.B) {
	points := []Point{
		{X: 0, Y: 0},
		{X: 10, Y: 0},
		{X: 10, Y: 10},
		{X: 0, Y: 10},
		{X: 5, Y: 5},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Centroid(points)
	}
}
