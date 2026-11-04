package legality

import "math"

// The arithmetic here must give the reference implementation's IEEE-754 results bit for bit, because
// an admission verdict (is this plane idle, is this brush inside out) is a comparison of a computed
// value with a tolerance, and a value one ulp away can land on the other side of it.
//
// Two things make that hold. First, every operation is written in the reference's order. Second,
// every product is wrapped in an explicit float64 conversion before it is added: the Go
// specification lets a compiler fuse `x*y + z` into one fused multiply-add, and the arm64 compiler
// does, which rounds once where JavaScript rounds twice. An explicit conversion forbids the fusion.

type vec3 = [3]float64

func mul(a, b float64) float64 { return float64(a * b) }

func dot(a, b vec3) float64 {
	return float64(float64(mul(a[0], b[0])+mul(a[1], b[1])) + mul(a[2], b[2]))
}

func sub(a, b vec3) vec3 { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }

func cross(a, b vec3) vec3 {
	return vec3{
		float64(mul(a[1], b[2]) - mul(a[2], b[1])),
		float64(mul(a[2], b[0]) - mul(a[0], b[2])),
		float64(mul(a[0], b[1]) - mul(a[1], b[0])),
	}
}

// Hypot is JavaScript's Math.hypot as V8 computes it: every argument divided by the largest
// magnitude, the squares added with Kahan compensation, and the square root scaled back. Go's
// math.Hypot takes two arguments and rounds differently, and sqrt(x²+y²+z²) differs again, so a
// face normal built either way could differ from the reference's in its last bit.
func Hypot(values ...float64) float64 {
	if len(values) == 0 {
		return 0
	}
	largest := 0.0
	nan := false
	for _, value := range values {
		if math.IsNaN(value) {
			nan = true
			continue
		}
		if magnitude := math.Abs(value); magnitude > largest {
			largest = magnitude
		}
	}
	if math.IsInf(largest, 1) {
		return math.Inf(1)
	}
	if nan {
		return math.NaN()
	}
	if largest == 0 {
		return 0
	}
	sum, compensation := 0.0, 0.0
	for _, value := range values {
		n := math.Abs(value) / largest
		summand := float64(mul(n, n) - compensation)
		preliminary := float64(sum + summand)
		compensation = float64(float64(preliminary-sum) - summand)
		sum = preliminary
	}
	return float64(math.Sqrt(sum) * largest)
}

func hypot3(v vec3) float64 { return Hypot(v[0], v[1], v[2]) }

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
