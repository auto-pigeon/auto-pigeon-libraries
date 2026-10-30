package apmap

import (
	"math"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/convex"
)

// Classic texture fidelity through SER-7 — Q2_002A.
//
// A Classic face stores five numbers and no axes: the compiler re-derives the
// base plane from the face's three points (qbsp texture_axis_t). SER-7 rounds the
// points, and the numbers, to six decimal places, and two things go wrong that no
// single number's rounding error explains:
//
//  1. On an exact 45° plane the base-axis choice is a tie decided by the last bit
//     of the normal. Six-place points can decide it the other way, and the same
//     five numbers then project the texture through another world plane:
//     hundreds of texels, measured against qbsp's own texinfo.
//  2. A small scale loses digits (0.0625004768 becomes 0.0625), and multiplied by
//     a world coordinate of thousands of units that is a tenth of a texel.
//
// So the conversion asks, face by face, which five numbers mean what the source
// meant ON THE SURFACE THE DOCUMENT NOW HOLDS. This is the same rule as AUP's
// packages/quake-geometry/src/apmap/classicFidelity.ts, transcribed operation for
// operation so that the two producers write the same document:
//
//   - literal: the SER-7 spelling of the source's numbers, kept whenever it
//     reproduces the source within ClassicFidelityBoundTexels at every corner of
//     the emitted face — nearly always;
//   - rebased: otherwise, the source's mapping restricted to the emitted plane,
//     re-expressed on the base plane the emitted points select — only when that
//     restriction is a rotation and two scales there (its gradients orthogonal)
//     and the rounded result is within the bound;
//   - unrepresentable: neither (a shear between the two base planes); the literal
//     spelling is kept and the face is reported.
//
// Geometry is never moved and Valve 220 is never produced.
//
// Products are written as float64(a*b): the Go specification lets a compiler fuse
// x*y+z into one rounding on some architectures, and a tie decided in the last
// bit must be decided as qbsp's baseline x86-64 build decides it, everywhere.

// ClassicFidelityBoundTexels is the fidelity a Classic face keeps through import.
const ClassicFidelityBoundTexels = 0.01

// ClassicFidelityOutcome names what happened to one Classic face.
type ClassicFidelityOutcome string

const (
	ClassicExact           ClassicFidelityOutcome = "exact"
	ClassicLiteral         ClassicFidelityOutcome = "literal"
	ClassicRebased         ClassicFidelityOutcome = "rebased"
	ClassicUnrepresentable ClassicFidelityOutcome = "unrepresentable"
	ClassicNoSurface       ClassicFidelityOutcome = "no_surface"
)

// ClassicNumbers are a Classic face's five numbers.
type ClassicNumbers struct {
	Shift    [2]float64
	Rotation float64
	Scale    [2]float64
}

// ClassicFidelity is the decision for one face and its measurement.
type ClassicFidelity struct {
	Outcome            ClassicFidelityOutcome
	Numbers            ClassicNumbers
	ErrorTexels        float64
	LiteralErrorTexels float64
	SourceAxis         int
	EmittedAxis        int
	Shear              float64
}

type vec3 = [3]float64

// ser7 is SER-7 for one number, exactly as serialize.go writes it.
func ser7(value float64) float64 {
	rounded := roundHalfEven(value, DecimalPlaces)
	if rounded == 0 {
		return 0
	}
	return rounded
}

func dot3(a, b vec3) float64 { return float64(a[0]*b[0]) + float64(a[1]*b[1]) + float64(a[2]*b[2]) }

// classicPlaneNormal is qbsp's normal of three points, (p0 − p1) × (p2 − p1)
// divided by sqrt of its squared length, negated — AUP's planeNormal, bit for bit.
func classicPlaneNormal(points [3]vec3) vec3 {
	a, b, c := points[0], points[1], points[2]
	u := vec3{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	v := vec3{c[0] - b[0], c[1] - b[1], c[2] - b[2]}
	n := vec3{float64(u[1]*v[2]) - float64(u[2]*v[1]), float64(u[2]*v[0]) - float64(u[0]*v[2]), float64(u[0]*v[1]) - float64(u[1]*v[0])}
	length := math.Sqrt(float64(n[0]*n[0]) + float64(n[1]*n[1]) + float64(n[2]*n[2]))
	if length == 0 {
		return vec3{}
	}
	return vec3{n[0] / length, n[1] / length, n[2] / length}
}

// classicAxisRow is qbsp's base plane for a normal: 0 floor/ceiling, 1 the X
// walls, 2 the Y walls, the earlier row winning a tie.
func classicAxisRow(normal vec3) int {
	x, y, z := math.Abs(normal[0]), math.Abs(normal[1]), math.Abs(normal[2])
	if z >= x && z >= y {
		return 0
	}
	if x >= y {
		return 1
	}
	return 2
}

var classicBase = [3][2]vec3{
	{{1, 0, 0}, {0, -1, 0}},
	{{0, 1, 0}, {0, 0, -1}},
	{{1, 0, 0}, {0, 0, -1}},
}

func rotateAxes(u, v vec3, degrees float64) (vec3, vec3) {
	radians := degrees * math.Pi / 180
	if radians == 0 {
		return u, v
	}
	c, s := math.Cos(radians), math.Sin(radians)
	return vec3{float64(u[0]*c) - float64(v[0]*s), float64(u[1]*c) - float64(v[1]*s), float64(u[2]*c) - float64(v[2]*s)},
		vec3{float64(u[0]*s) + float64(v[0]*c), float64(u[1]*s) + float64(v[1]*c), float64(u[2]*s) + float64(v[2]*c)}
}

type texinfo [2][4]float64

// classicTexinfo is qbsp's set_texinfo for a Classic face; a zero scale reads as 1.
func classicTexinfo(points [3]vec3, numbers ClassicNumbers) texinfo {
	axes := classicBase[classicAxisRow(classicPlaneNormal(points))]
	u, v := rotateAxes(axes[0], axes[1], numbers.Rotation)
	su, sv := numbers.Scale[0], numbers.Scale[1]
	if su == 0 {
		su = 1
	}
	if sv == 0 {
		sv = 1
	}
	return texinfo{{u[0] / su, u[1] / su, u[2] / su, numbers.Shift[0]}, {v[0] / sv, v[1] / sv, v[2] / sv, numbers.Shift[1]}}
}

func (t texinfo) at(point vec3) [2]float64 {
	return [2]float64{dot3(vec3{t[0][0], t[0][1], t[0][2]}, point) + t[0][3], dot3(vec3{t[1][0], t[1][1], t[1][2]}, point) + t[1][3]}
}

func maxError(source, candidate texinfo, corners []vec3) float64 {
	worst := 0.0
	for _, corner := range corners {
		a, b := source.at(corner), candidate.at(corner)
		worst = math.Max(worst, math.Max(math.Abs(a[0]-b[0]), math.Abs(a[1]-b[1])))
	}
	return worst
}

func normaliseDegrees(angle float64) float64 {
	value := math.Mod(angle, 360)
	if value > 180 {
		value -= 360
	}
	if value <= -180 {
		value += 360
	}
	return value
}

func sign(value float64) float64 {
	switch {
	case value > 0:
		return 1
	case value < 0:
		return -1
	}
	return 0
}

// reexpress is the source mapping restricted to the emitted plane, on the
// emitted plane's own base plane: both orientations, the source's scale signs
// first, and the shear that makes it unrepresentable when it is not zero.
func reexpress(source texinfo, emitted [3]vec3, scaleHint [2]float64, rotationHint float64) ([]ClassicNumbers, float64) {
	n := classicPlaneNormal(emitted)
	dist := dot3(n, emitted[0])
	axes := classicBase[classicAxisRow(n)]
	index := func(axis vec3) int {
		if axis[0] != 0 {
			return 0
		}
		if axis[1] != 0 {
			return 1
		}
		return 2
	}
	sv, tv := index(axes[0]), index(axes[1])
	d := 3 - sv - tv
	su, sgv := axes[0][sv], axes[1][tv]
	restrict := func(row [4]float64) ([2]float64, float64) {
		return [2]float64{row[sv] - row[d]*n[sv]/n[d], row[tv] - row[d]*n[tv]/n[d]}, row[3] + row[d]*dist/n[d]
	}
	gs, cs := restrict(source[0])
	gt, ct := restrict(source[1])
	length := math.Hypot(gs[0], gs[1])
	cos, sin := gs[0]/length, gs[1]/length
	tLength := math.Hypot(gt[0], gt[1])
	if tLength == 0 {
		tLength = 1
	}
	shear := (float64(cos*gt[0]) + float64(sin*gt[1])) / tLength
	ks, kt := su*length, sgv*(float64(-sin*gt[0])+float64(cos*gt[1]))
	theta := math.Atan2(sin, cos) * 180 / math.Pi
	candidates := []ClassicNumbers{
		{Shift: [2]float64{cs, ct}, Rotation: normaliseDegrees(theta), Scale: [2]float64{1 / ks, 1 / kt}},
		{Shift: [2]float64{cs, ct}, Rotation: normaliseDegrees(theta + 180), Scale: [2]float64{1 / -ks, 1 / -kt}},
	}
	same := func(c ClassicNumbers) int {
		count := 0
		if sign(c.Scale[0]) == sign(scaleHint[0]) {
			count++
		}
		if sign(c.Scale[1]) == sign(scaleHint[1]) {
			count++
		}
		return count
	}
	// The spelling closest to the source's own, decided by preference and never by a last-bit difference in
	// error: the same scale signs first, then the rotation nearer the source's, then atan2's own angle.
	turn := func(c ClassicNumbers) float64 { return math.Abs(normaliseDegrees(c.Rotation - rotationHint)) }
	if same(candidates[1]) > same(candidates[0]) || (same(candidates[1]) == same(candidates[0]) && turn(candidates[1]) < turn(candidates[0])) {
		candidates[0], candidates[1] = candidates[1], candidates[0]
	}
	return candidates, shear
}

func centred(source texinfo, emitted [3]vec3, numbers ClassicNumbers, corners []vec3) ClassicNumbers {
	candidate := classicTexinfo(emitted, numbers)
	for axis := 0; axis < 2; axis++ {
		low, high := math.Inf(1), math.Inf(-1)
		for _, corner := range corners {
			residual := source.at(corner)[axis] - candidate.at(corner)[axis]
			low, high = math.Min(low, residual), math.Max(high, residual)
		}
		numbers.Shift[axis] = ser7(numbers.Shift[axis] + (low+high)/2)
	}
	return numbers
}

// FaithfulClassicNumbers decides the five numbers to store for one Classic face.
// corners is asked for only when the decision needs the emitted face's polygon.
func FaithfulClassicNumbers(sourcePoints [3]vec3, source ClassicNumbers, emitted [3]vec3, corners func() []vec3) ClassicFidelity {
	literal := ClassicNumbers{Shift: [2]float64{ser7(source.Shift[0]), ser7(source.Shift[1])}, Rotation: ser7(source.Rotation),
		Scale: [2]float64{ser7(source.Scale[0]), ser7(source.Scale[1])}}
	sourceAxis, emittedAxis := classicAxisRow(classicPlaneNormal(sourcePoints)), classicAxisRow(classicPlaneNormal(emitted))
	unchanged := literal.Rotation == source.Rotation && literal.Scale == source.Scale
	if sourceAxis == emittedAxis && unchanged {
		outcome := ClassicLiteral
		if literal.Shift == source.Shift {
			outcome = ClassicExact
		}
		e := math.Max(math.Abs(literal.Shift[0]-source.Shift[0]), math.Abs(literal.Shift[1]-source.Shift[1]))
		return ClassicFidelity{Outcome: outcome, Numbers: literal, ErrorTexels: e, LiteralErrorTexels: e, SourceAxis: sourceAxis, EmittedAxis: emittedAxis}
	}
	polygon := corners()
	if len(polygon) < 3 {
		return ClassicFidelity{Outcome: ClassicNoSurface, Numbers: literal, SourceAxis: sourceAxis, EmittedAxis: emittedAxis}
	}
	sourceTexinfo := classicTexinfo(sourcePoints, source)
	literalError := maxError(sourceTexinfo, classicTexinfo(emitted, literal), polygon)
	if literalError <= ClassicFidelityBoundTexels {
		return ClassicFidelity{Outcome: ClassicLiteral, Numbers: literal, ErrorTexels: literalError, LiteralErrorTexels: literalError,
			SourceAxis: sourceAxis, EmittedAxis: emittedAxis}
	}
	candidates, shear := reexpress(sourceTexinfo, emitted, source.Scale, source.Rotation)
	// The first candidate, in preference order, that is faithful: two spellings of one mapping differ in error only
	// in their last bits, and a producer that chose by that would write different bytes from AUP for the same face.
	bestError, found := math.Inf(1), false
	var best ClassicNumbers
	for _, candidate := range candidates {
		if found && bestError <= ClassicFidelityBoundTexels {
			break
		}
		rounded := ClassicNumbers{Shift: [2]float64{ser7(candidate.Shift[0]), ser7(candidate.Shift[1])}, Rotation: ser7(candidate.Rotation),
			Scale: [2]float64{ser7(candidate.Scale[0]), ser7(candidate.Scale[1])}}
		finite := true
		for _, value := range []float64{rounded.Shift[0], rounded.Shift[1], rounded.Rotation, rounded.Scale[0], rounded.Scale[1]} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				finite = false
			}
		}
		if !finite || rounded.Scale[0] == 0 || rounded.Scale[1] == 0 {
			continue
		}
		numbers := centred(sourceTexinfo, emitted, rounded, polygon)
		if e := maxError(sourceTexinfo, classicTexinfo(emitted, numbers), polygon); !found || e < bestError {
			best, bestError, found = numbers, e, true
		}
	}
	if found && bestError <= ClassicFidelityBoundTexels {
		return ClassicFidelity{Outcome: ClassicRebased, Numbers: best, ErrorTexels: bestError, LiteralErrorTexels: literalError,
			SourceAxis: sourceAxis, EmittedAxis: emittedAxis, Shear: shear}
	}
	return ClassicFidelity{Outcome: ClassicUnrepresentable, Numbers: literal, ErrorTexels: literalError, LiteralErrorTexels: literalError,
		SourceAxis: sourceAxis, EmittedAxis: emittedAxis, Shear: shear}
}

// emittedPolygons clips every face of a brush given its emitted points, with
// AUE's own winding code; nil for a face that bounds nothing.
func emittedPolygons(faces [][3]vec3) [][]vec3 {
	planes := make([]convex.Plane, len(faces))
	valid := make([]bool, len(faces))
	for i, points := range faces {
		plane, err := convex.PlaneFromPoints(convex.Vec3f(points[0]), convex.Vec3f(points[1]), convex.Vec3f(points[2]))
		if err == nil {
			planes[i], valid[i] = plane, true
		}
	}
	out := make([][]vec3, len(faces))
	for i := range faces {
		if !valid[i] {
			continue
		}
		winding := convex.BaseWinding(planes[i], convex.BaseWindingExtent)
		for j := range faces {
			if j == i || !valid[j] {
				continue
			}
			winding = convex.ClipWinding(winding, planes[j], convex.PlaneDistanceEpsilon)
			if len(winding) < 3 {
				break
			}
		}
		if len(winding) >= 3 {
			polygon := make([]vec3, len(winding))
			for k, point := range winding {
				polygon[k] = vec3(point)
			}
			out[i] = polygon
		}
	}
	return out
}
