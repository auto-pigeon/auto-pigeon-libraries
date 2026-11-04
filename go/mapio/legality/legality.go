// Package legality answers whether a brush is one a Quake compiler will build: the Go port of the
// TypeScript legality authority (`ts/mapio/src/geometry/brushLegality.ts`, with `convexity.ts`,
// `reconstructBrush.ts` and `quakeWinding.ts`) that the Quake III importer crosses every brush with.
//
// It is a port for one reason: the Quake III import admits or refuses a map, and leaves idle planes
// out, on what this check answers, so the Go and TypeScript importers agree only if the two checks
// agree on every code, every convexity reason and every face index they name. The arithmetic
// therefore follows the reference operation for operation (see arith.go); the conformance fixtures
// and a randomized differential against the reference hold it there. Finding DETAIL sentences are
// not part of that contract and are not reproduced.
//
// Pure: no IO, no logging, no goroutine. Check honours its context between reconstructed faces.
package legality

import (
	"context"
	"math"
)

// The finding codes. A code is `illegal` or a `risk`; see Finding.Illegal.
const (
	CodeNonFinite              = "non_finite"
	CodeUnserializable         = "unserializable"
	CodeNotConvex              = "not_convex"
	CodeWindingInward          = "winding_inward"
	CodeWindingMixed           = "winding_mixed"
	CodeWindingUnreadable      = "winding_unreadable"
	CodeDuplicatePlane         = "duplicate_plane"
	CodeUncheckedFaceCount     = "unchecked_face_count"
	CodeThin                   = "thin"
	CodeTextureUnnamed         = "texture_unnamed"
	CodeTextureScaleDegenerate = "texture_scale_degenerate"
	CodeTextureBasisDegenerate = "texture_basis_degenerate"
)

// The convexity reasons.
const (
	ReasonTooFewFaces     = "too_few_faces"
	ReasonDegeneratePlane = "degenerate_plane"
	ReasonTooManyFaces    = "too_many_faces"
	ReasonUnbounded       = "unbounded"
	ReasonEmpty           = "empty"
	ReasonDegenerate      = "degenerate"
	ReasonNotConvex       = "not_convex"
)

// The reference's tolerances, by the reference's names.
const (
	ConvexityTolerance     = 1e-6
	MaxConvexityFaces      = 64
	singular               = 1e-9
	CoincidentNormal       = 1 - 1e-9
	CoincidentOffset       = 1e-6
	MinFaceTextureScale    = 1e-4
	MaxFaceTextureScale    = 1e6
	MinTextureAxisLength   = 1e-9
	ThinBrushSpan          = 1
	windingInteriorEpsilon = 1e-6
)

// The projection modes.
const (
	ModeClassic         = "classic"
	ModeValve220        = "valve220"
	ModeBrushPrimitives = "brush_primitives"
)

// Projection is a face's texture projection, as the APMap document stores it.
type Projection struct {
	Mode     string
	Shift    [2]float64 // classic
	U, V     [4]float64 // valve220
	Rotation float64    // classic, valve220
	Scale    [2]float64 // classic, valve220
	Matrix   [2][3]float64
}

// Face is one face: its three plane points, texture name and projection.
type Face struct {
	Points     [3]vec3
	Texture    string
	Projection Projection
}

// Convexity is the reference's `checkBrushConvexity` answer.
type Convexity struct {
	OK              bool
	Reason          string
	Faces           []int
	VertexCount     int
	Volume          float64
	OrientationSign int
}

// Finding is one thing found. Illegal findings make the brush illegal; risks do not.
type Finding struct {
	Code    string
	Illegal bool
	Faces   []int
}

// Result is the whole answer.
type Result struct {
	// OK is true exactly when no finding is illegal.
	OK       bool
	Findings []Finding
	// Codes are the illegal codes, in the order found.
	Codes     []string
	Convexity Convexity
	// Winding is "outward", "inward", "mixed" or "unreadable".
	Winding      string
	ThinnestSpan float64
}

func indices(count int) []int {
	out := make([]int, count)
	for index := range out {
		out[index] = index
	}
	return out
}

// faceNumbers is every number a face writes into a `.map`.
func faceNumbers(face Face) []float64 {
	numbers := []float64{}
	for _, point := range face.Points {
		numbers = append(numbers, point[:]...)
	}
	projection := face.Projection
	if projection.Mode == ModeBrushPrimitives {
		return append(numbers, projection.Matrix[0][0], projection.Matrix[0][1], projection.Matrix[0][2],
			projection.Matrix[1][0], projection.Matrix[1][1], projection.Matrix[1][2])
	}
	if projection.Mode == ModeValve220 {
		numbers = append(numbers, projection.U[:]...)
		numbers = append(numbers, projection.V[:]...)
	} else {
		numbers = append(numbers, projection.Shift[:]...)
	}
	return append(numbers, projection.Rotation, projection.Scale[0], projection.Scale[1])
}

// IsJSSpace is JavaScript's regular-expression `\s`: the ASCII whitespace, U+00A0, U+1680,
// U+2000–U+200A, U+2028, U+2029, U+202F, U+205F, U+3000 and U+FEFF.
func IsJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// Nameable reports `^[^\s"]+$` in JavaScript's sense: a texture name a `.map` can write as one token.
func Nameable(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r == '"' || IsJSSpace(r) {
			return false
		}
	}
	return true
}

// faceProjectionCodes is `faceProjectionLegality`'s codes, in its order.
func faceProjectionCodes(face Face) []string {
	codes := []string{}
	if !Nameable(face.Texture) {
		codes = append(codes, CodeTextureUnnamed)
	}
	projection := face.Projection
	if projection.Mode == ModeBrushPrimitives {
		matrix := projection.Matrix
		determinant := float64(mul(matrix[0][0], matrix[1][1]) - mul(matrix[0][1], matrix[1][0]))
		if !(math.Abs(determinant) > 0) || !finite(determinant) {
			codes = append(codes, CodeTextureBasisDegenerate)
		}
		return codes
	}
	for _, value := range projection.Scale {
		size := math.Abs(value)
		if !finite(size) || size < MinFaceTextureScale || size > MaxFaceTextureScale {
			codes = append(codes, CodeTextureScaleDegenerate)
		}
	}
	if projection.Mode == ModeValve220 {
		u := vec3{projection.U[0], projection.U[1], projection.U[2]}
		v := vec3{projection.V[0], projection.V[1], projection.V[2]}
		lengthU, lengthV := hypot3(u), hypot3(v)
		area := hypot3(cross(u, v))
		if !(lengthU > MinTextureAxisLength) || !(lengthV > MinTextureAxisLength) || !(area > MinTextureAxisLength) {
			codes = append(codes, CodeTextureBasisDegenerate)
		}
	}
	return codes
}

// parseable is what the reference's `parsedBrushFromApmap` accepts of a brush whose numbers are all
// finite: at least four faces, and a brushDef face whose plane points give a normal to place its
// texture with.
func parseable(faces []Face) bool {
	if len(faces) < 4 {
		return false
	}
	for _, face := range faces {
		if face.Projection.Mode != ModeBrushPrimitives {
			continue
		}
		raw := cross(sub(face.Points[0], face.Points[1]), sub(face.Points[2], face.Points[1]))
		length := hypot3(raw)
		if !(length > 0) || !finite(length) {
			return false
		}
	}
	return true
}

// lazyReconstruction runs reconstruct once, however many parts of the check need it: the
// reference runs it up to three times over the same planes, and it is deterministic.
type lazyReconstruction struct {
	ctx   context.Context
	faces []Face
	done  bool
	built reconstruction
	err   error
}

func (l *lazyReconstruction) get() (reconstruction, error) {
	if !l.done {
		l.built, l.err = reconstruct(l.ctx, l.faces)
		l.done = true
	}
	return l.built, l.err
}

func checkConvexity(faces []Face, built *lazyReconstruction) (Convexity, error) {
	fail := func(reason string, faces []int, sign, vertices int, volume float64) Convexity {
		if faces == nil {
			faces = []int{}
		}
		return Convexity{Reason: reason, Faces: faces, OrientationSign: sign, VertexCount: vertices, Volume: volume}
	}
	if len(faces) < 4 {
		return fail(ReasonTooFewFaces, nil, 0, 0, 0), nil
	}
	if len(faces) > MaxConvexityFaces {
		return fail(ReasonTooManyFaces, nil, 0, 0, 0), nil
	}
	signed := make([]plane, 0, len(faces))
	degenerate := []int{}
	for index, face := range faces {
		raw := cross(sub(face.Points[1], face.Points[0]), sub(face.Points[2], face.Points[0]))
		length := hypot3(raw)
		if !finite(length) || length <= singular {
			degenerate = append(degenerate, index)
			continue
		}
		n := vec3{raw[0] / length, raw[1] / length, raw[2] / length}
		signed = append(signed, plane{n: n, d: dot(n, face.Points[0])})
	}
	if len(degenerate) > 0 {
		return fail(ReasonDegeneratePlane, degenerate, 0, 0, 0), nil
	}

	reconstructed, err := built.get()
	if err != nil {
		return Convexity{}, err
	}
	sign := reconstructed.orientationSign
	flip := float64(-sign)
	planes := make([]plane, len(signed))
	for index, p := range signed {
		planes[index] = plane{n: vec3{flip * p.n[0], flip * p.n[1], flip * p.n[2]}, d: flip * p.d}
	}
	outside := func(point vec3) float64 {
		worst := math.Inf(-1)
		for _, p := range planes {
			worst = math.Max(worst, float64(dot(p.n, point)-p.d))
		}
		return worst
	}

	vertices := []vec3{}
	for i := 0; i < len(planes); i++ {
		for j := i + 1; j < len(planes); j++ {
			for k := j + 1; k < len(planes); k++ {
				a, b, c := planes[i], planes[j], planes[k]
				determinant := dot(a.n, cross(b.n, c.n))
				if math.Abs(determinant) < singular {
					continue
				}
				bc, ca, ab := cross(b.n, c.n), cross(c.n, a.n), cross(a.n, b.n)
				var point vec3
				for axis := 0; axis < 3; axis++ {
					point[axis] = float64(float64(mul(bc[axis], a.d)+mul(ca[axis], b.d))+mul(ab[axis], c.d)) / determinant
				}
				if finite(point[0]) && finite(point[1]) && finite(point[2]) && outside(point) <= ConvexityTolerance {
					vertices = append(vertices, point)
				}
			}
		}
	}

	escapes := []int{}
	for _, face := range reconstructed.faces {
		for _, point := range face.polygon {
			if math.Abs(point[0]) >= BaseWindingSize-1 || math.Abs(point[1]) >= BaseWindingSize-1 || math.Abs(point[2]) >= BaseWindingSize-1 {
				escapes = append(escapes, face.index)
				break
			}
		}
	}
	if len(escapes) > 0 {
		return fail(ReasonUnbounded, escapes, sign, len(vertices), 0), nil
	}
	if len(vertices) == 0 {
		return fail(ReasonEmpty, nil, sign, 0, 0), nil
	}
	volume := enclosedVolume(reconstructed.faces)
	if volume <= ConvexityTolerance {
		return fail(ReasonDegenerate, nil, sign, len(vertices), volume), nil
	}
	bare := []int{}
	for index, p := range planes {
		on := 0
		for _, point := range vertices {
			if math.Abs(float64(dot(p.n, point)-p.d)) <= ConvexityTolerance {
				on++
			}
		}
		if on < 3 {
			bare = append(bare, index)
		}
	}
	if len(bare) > 0 {
		return fail(ReasonNotConvex, bare, sign, len(vertices), volume), nil
	}
	return Convexity{OK: true, Faces: []int{}, VertexCount: len(vertices), Volume: volume, OrientationSign: sign}, nil
}

// quakePlaneNormal is qbsp's normal, (p0-p1) x (p2-p1), normalised with a plain square root.
func quakePlaneNormal(points [3]vec3) vec3 {
	n := cross(sub(points[0], points[1]), sub(points[2], points[1]))
	length := math.Sqrt(float64(float64(mul(n[0], n[0])+mul(n[1], n[1])) + mul(n[2], n[2])))
	if length == 0 {
		return vec3{}
	}
	return vec3{n[0] / length, n[1] / length, n[2] / length}
}

// windingReport is the reference's `brushWindingReport`, reduced to its verdict and the faces that
// are not outward: "outward", "inward", "mixed" or "unreconstructable".
func windingReport(faces []Face, lazy *lazyReconstruction) (string, []int, error) {
	if len(faces) == 0 {
		return "unreconstructable", []int{}, nil
	}
	built, err := lazy.get()
	if err != nil {
		return "", nil, err
	}
	corners := []vec3{}
	for _, face := range built.faces {
		corners = append(corners, face.polygon...)
	}
	if len(corners) < 4 {
		return "unreconstructable", []int{}, nil
	}
	for _, corner := range corners {
		for _, value := range corner {
			if math.Abs(value) >= BaseWindingSize-1 {
				return "unreconstructable", []int{}, nil
			}
		}
	}
	var interior vec3
	count := float64(len(corners))
	for _, corner := range corners {
		interior[0] = float64(interior[0] + corner[0]/count)
		interior[1] = float64(interior[1] + corner[1]/count)
		interior[2] = float64(interior[2] + corner[2]/count)
	}
	unreadable := false
	notOutward := []int{}
	outward := 0
	for index, face := range faces {
		normal := quakePlaneNormal(face.Points)
		if normal[0] == 0 && normal[1] == 0 && normal[2] == 0 {
			unreadable = true
			notOutward = append(notOutward, index)
			continue
		}
		signedDistance := float64(dot(normal, interior) - dot(normal, face.Points[1]))
		if math.Abs(signedDistance) <= windingInteriorEpsilon {
			unreadable = true
			notOutward = append(notOutward, index)
			continue
		}
		if signedDistance < 0 {
			outward++
		} else {
			notOutward = append(notOutward, index)
		}
	}
	switch {
	case unreadable:
		return "unreconstructable", notOutward, nil
	case outward == len(faces):
		return "outward", notOutward, nil
	case outward == 0:
		return "inward", notOutward, nil
	default:
		return "mixed", notOutward, nil
	}
}

func thinnestSpan(faces []Face, sign int, built reconstruction) float64 {
	corners := []vec3{}
	for _, face := range built.faces {
		corners = append(corners, face.polygon...)
	}
	if len(corners) < 4 {
		return math.Inf(1)
	}
	thinnest := math.Inf(1)
	flip := float64(-sign)
	for _, face := range faces {
		raw := cross(sub(face.Points[1], face.Points[0]), sub(face.Points[2], face.Points[0]))
		length := hypot3(raw)
		if length == 0 || math.IsNaN(length) {
			continue
		}
		n := vec3{mul(flip, raw[0]) / length, mul(flip, raw[1]) / length, mul(flip, raw[2]) / length}
		d := dot(n, face.Points[0])
		deepest := 0.0
		for _, corner := range corners {
			deepest = math.Max(deepest, float64(d-dot(n, corner)))
		}
		thinnest = math.Min(thinnest, deepest)
	}
	return thinnest
}

// Check is the reference's `brushLegality`, with its findings in its order.
func Check(ctx context.Context, faces []Face) (Result, error) {
	result := Result{Findings: []Finding{}, Codes: []string{}, ThinnestSpan: math.Inf(1)}
	add := func(code string, illegal bool, faces []int) {
		if faces == nil {
			faces = []int{}
		}
		result.Findings = append(result.Findings, Finding{Code: code, Illegal: illegal, Faces: faces})
	}

	nonFinite := []int{}
	for index, face := range faces {
		for _, value := range faceNumbers(face) {
			if !finite(value) {
				nonFinite = append(nonFinite, index)
				break
			}
		}
	}
	if len(nonFinite) > 0 {
		add(CodeNonFinite, true, nonFinite)
	}
	// (The reference's canonical-number pass can only refuse a non-finite number, already named.)

	type held struct{ faces []int }
	order := []string{}
	byCode := map[string]*held{}
	for index, face := range faces {
		for _, code := range faceProjectionCodes(face) {
			if entry := byCode[code]; entry != nil {
				entry.faces = append(entry.faces, index)
				continue
			}
			byCode[code] = &held{faces: []int{index}}
			order = append(order, code)
		}
	}
	for _, code := range order {
		add(code, true, byCode[code].faces)
	}

	parsed := false
	if len(nonFinite) == 0 {
		parsed = parseable(faces)
		if !parsed {
			add(CodeUnserializable, true, nil)
		}
	}

	built := &lazyReconstruction{ctx: ctx, faces: faces}
	convexity := Convexity{Faces: []int{}}
	if parsed {
		var err error
		if convexity, err = checkConvexity(faces, built); err != nil {
			return Result{}, err
		}
		if !convexity.OK {
			if convexity.Reason == ReasonTooManyFaces {
				add(CodeUncheckedFaceCount, false, convexity.Faces)
			} else {
				add(CodeNotConvex, true, convexity.Faces)
			}
		}
	}
	result.Convexity = convexity

	type planeOrNone struct {
		plane
		ok bool
	}
	planes := make([]planeOrNone, len(faces))
	for index, face := range faces {
		raw := cross(sub(face.Points[1], face.Points[0]), sub(face.Points[2], face.Points[0]))
		length := hypot3(raw)
		if length == 0 || math.IsNaN(length) {
			continue
		}
		n := vec3{raw[0] / length, raw[1] / length, raw[2] / length}
		planes[index] = planeOrNone{plane: plane{n: n, d: dot(n, face.Points[0])}, ok: true}
	}
	duplicates := []int{}
	seen := map[int]bool{}
	for i := 0; i < len(planes); i++ {
		for j := i + 1; j < len(planes); j++ {
			a, b := planes[i], planes[j]
			if !a.ok || !b.ok {
				continue
			}
			if dot(a.n, b.n) >= CoincidentNormal && math.Abs(a.d-b.d) <= CoincidentOffset && !seen[j] {
				seen[j] = true
				duplicates = append(duplicates, j)
			}
		}
	}
	if len(duplicates) > 0 {
		add(CodeDuplicatePlane, true, duplicates)
	}

	var winding string
	offending := []int{}
	if convexity.OK && parsed {
		winding = "inward"
		if convexity.OrientationSign == 1 {
			winding = "outward"
		} else {
			offending = indices(len(faces))
		}
	} else {
		report, faces, err := windingReport(faces, built)
		if err != nil {
			return Result{}, err
		}
		winding = report
		if report == "unreconstructable" {
			winding = "unreadable"
		}
		offending = faces
	}
	result.Winding = winding
	switch winding {
	case "inward":
		add(CodeWindingInward, true, offending)
	case "mixed":
		add(CodeWindingMixed, true, offending)
	case "unreadable":
		add(CodeWindingUnreadable, false, nil)
	}

	if convexity.OK && parsed {
		reconstructed, err := built.get()
		if err != nil {
			return Result{}, err
		}
		result.ThinnestSpan = thinnestSpan(faces, convexity.OrientationSign, reconstructed)
		if finite(result.ThinnestSpan) && result.ThinnestSpan < ThinBrushSpan {
			add(CodeThin, false, nil)
		}
	}

	for _, finding := range result.Findings {
		if finding.Illegal {
			result.Codes = append(result.Codes, finding.Code)
		}
	}
	result.OK = len(result.Codes) == 0
	return result, nil
}
