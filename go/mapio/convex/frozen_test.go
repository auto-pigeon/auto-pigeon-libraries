package convex

import (
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// Frozen vectors for the convex solver, frozen from AUE output.
//
// This package is numerical, and its behaviour on degenerate geometry is
// decided entirely by five epsilon constants and by two semantics Go does not
// give for free — round-half-to-even snapping, and axis ties resolving to the
// lowest index. A hand-written assertion table checks what its author thought
// of; these vectors pin every observable of a set of scenarios chosen for the
// hard cases (duplicate faces, an oblique wedge on the SAT path, a sheared
// solid, a tetrahedron, a thin slab, negative coordinates, and four ways to
// fail to bound), plus the primitives underneath them.
//
// The vectors live in testdata/ and are regenerated from the current code with
//
//	go test ./internal/geometry/convex -run FrozenVectors -update
//
// which is a deliberate act: a regenerated vector is a behaviour change to
// review, not a test to make green.

var updateFrozen = flag.Bool("update", false, "rewrite the frozen convex vectors from the current implementation")

const (
	polyhedraGolden  = "frozen_polyhedra.json"
	primitivesGolden = "frozen_primitives.json"
)

// frozenVectors decodes testdata/<name> into target, first rewriting it from
// compute() when -update is set.
func frozenVectors(t *testing.T, name string, compute func() any, target any) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateFrozen {
		encoded, err := json.MarshalIndent(compute(), "", " ")
		if err != nil {
			t.Fatalf("encoding %s: %v", name, err)
		}
		if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the frozen vectors: %v", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
}

func vec(v Vec3f) []float64 { return []float64{v[0], v[1], v[2]} }

func vecs(points []Vec3f) [][]float64 {
	out := make([][]float64, 0, len(points))
	for _, point := range points {
		out = append(out, vec(point))
	}
	return out
}

// computeScenarioResults records every observable of each scenario.
func computeScenarioResults(t *testing.T, scenarios []scenario) []scenarioResult {
	t.Helper()
	out := make([]scenarioResult, 0, len(scenarios))
	for _, s := range scenarios {
		entry := scenarioResult{Name: s.Name}
		planes := make([]Plane, 0, len(s.Faces))
		var failure error
		for _, face := range s.Faces {
			plane, err := PlaneFromPoints(face[0], face[1], face[2])
			if err != nil {
				failure = err
				break
			}
			planes = append(planes, plane)
		}
		if failure == nil {
			for _, plane := range planes {
				entry.Planes = append(entry.Planes, []float64{plane.Normal[0], plane.Normal[1], plane.Normal[2], plane.Distance})
				key := plane.Key()
				entry.PlaneKeys = append(entry.PlaneKeys, []int64{key[0], key[1], key[2], key[3]})
			}
		}
		var solid *ConvexPolyhedron
		var mapping []int
		if failure == nil {
			solid, mapping, failure = BuildPolyhedron(planes)
		}
		if failure != nil {
			message := failure.Error()
			entry.Error = &message
			out = append(out, entry)
			continue
		}
		for _, polygon := range solid.Polygons {
			entry.Polygons = append(entry.Polygons, vecs(polygon))
			entry.Areas = append(entry.Areas, WindingArea(polygon))
			entry.Normals = append(entry.Normals, vec(WindingNormal(polygon)))
			entry.Centroids = append(entry.Centroids, vec(WindingCentroid(polygon)))
		}
		entry.Min, entry.Max = vec(solid.MinXYZ), vec(solid.MaxXYZ)
		entry.Volume, entry.Centroid = solid.Volume, vec(solid.Centroid)
		entry.PlaneToPolygon = append([]int{}, mapping...)
		entry.Extents = vec(solid.Extents())
		entry.IsBox = PolyhedronIsAxisAlignedBox(solid, AxisAlignedBoxEpsilon)
		entry.EdgeCount = len(solid.Edges())
		for planeIndex, polygonIndex := range mapping {
			if polygonIndex < 0 {
				continue
			}
			polygon := solid.Polygons[polygonIndex]
			entry.Orientations = append(entry.Orientations,
				[]float64{float64(planeIndex), WindingOrientation(polygon, planes[planeIndex].Normal)})
			entry.Offsets = append(entry.Offsets,
				[]float64{float64(planeIndex), PolygonPlaneOffset(polygon, planes[planeIndex])})
		}
		for _, ray := range s.Rays {
			if distance, hit := RayHitsPolyhedron(ray.Origin, ray.Direction, solid, ray.Max); hit {
				entry.Rays = append(entry.Rays, &distance)
			} else {
				entry.Rays = append(entry.Rays, nil)
			}
		}
		for _, box := range s.Boxes {
			entry.BoxHits = append(entry.BoxHits, BoxIntersectsPolyhedron(box.Min, box.Max, solid, box.Epsilon))
		}
		out = append(out, entry)
	}
	return out
}

// computePrimitiveResults records the primitives' answers for payload.
func computePrimitiveResults(t *testing.T, payload primitivePayload) primitiveResult {
	t.Helper()
	var out primitiveResult
	for index, point := range payload.SnapPoints {
		out.Snapped = append(out.Snapped, vec(SnapPoint(point, payload.SnapGrids[index])))
	}
	for _, plane := range payload.BaseWindings {
		out.Windings = append(out.Windings, vecs(BaseWinding(Plane{Normal: plane.Normal, Distance: plane.Distance}, BaseWindingExtent)))
	}
	for _, c := range payload.ClipCases {
		out.Clipped = append(out.Clipped, vecs(ClipWinding(c.Points, Plane{Normal: c.Plane.Normal, Distance: c.Plane.Distance}, c.Epsilon)))
	}
	for _, c := range payload.DuplicateCases {
		out.Duplicates = append(out.Duplicates, vecs(RemoveDuplicatePoints(c, PointOnPlaneEpsilon)))
	}
	for _, c := range payload.AreaCases {
		out.Areas = append(out.Areas, WindingArea(c))
		out.Normals = append(out.Normals, vec(WindingNormal(c)))
		out.Centroids = append(out.Centroids, vec(WindingCentroid(c)))
	}
	for _, c := range payload.PlaneCases {
		plane, err := PlaneFromPoints(c[0], c[1], c[2])
		if err != nil {
			out.Planes = append(out.Planes, nil)
			continue
		}
		values := []float64{plane.Normal[0], plane.Normal[1], plane.Normal[2], plane.Distance}
		out.Planes = append(out.Planes, &values)
	}
	return out
}

// nearlyEqual reports whether two floats agree to within a tolerance appropriate to
// their magnitude. Agreement with the frozen vectors is normally exact; the
// tolerance covers only the last bit or two of accumulated rounding in volume
// and centroid sums, which may differ between architectures (FMA).
func nearlyEqual(a, b float64) bool {
	if a == b {
		return true
	}
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	scale := math.Max(math.Abs(a), math.Abs(b))
	return math.Abs(a-b) <= math.Max(1e-9, scale*1e-12)
}

func closeVec(a Vec3f, b []float64) bool {
	if len(b) != 3 {
		return false
	}
	return nearlyEqual(a[0], b[0]) && nearlyEqual(a[1], b[1]) && nearlyEqual(a[2], b[2])
}

// ── scenarios ───────────────────────────────────────────────────────────────

type rayQuery struct {
	Origin    Vec3f   `json:"origin"`
	Direction Vec3f   `json:"direction"`
	Max       float64 `json:"max"`
}

type boxQuery struct {
	Min     Vec3f   `json:"min"`
	Max     Vec3f   `json:"max"`
	Epsilon float64 `json:"epsilon"`
}

type scenario struct {
	Name  string     `json:"name"`
	Faces [][3]Vec3f `json:"faces"`
	Rays  []rayQuery `json:"rays"`
	Boxes []boxQuery `json:"boxes"`
}

type scenarioResult struct {
	Name           string        `json:"name"`
	Error          *string       `json:"error"`
	Planes         [][]float64   `json:"planes"`
	PlaneKeys      [][]int64     `json:"plane_keys"`
	Polygons       [][][]float64 `json:"polygons"`
	Min            []float64     `json:"min"`
	Max            []float64     `json:"max"`
	Volume         float64       `json:"volume"`
	Centroid       []float64     `json:"centroid"`
	PlaneToPolygon []int         `json:"plane_to_polygon"`
	Extents        []float64     `json:"extents"`
	IsBox          bool          `json:"is_box"`
	EdgeCount      int           `json:"edge_count"`
	Areas          []float64     `json:"areas"`
	Normals        [][]float64   `json:"normals"`
	Centroids      [][]float64   `json:"centroids"`
	Orientations   [][]float64   `json:"orientations"`
	Offsets        [][]float64   `json:"offsets"`
	Rays           []*float64    `json:"rays"`
	BoxHits        []bool        `json:"boxes"`
}

// boxFaces reproduces the canonical axis-aligned box face order and winding
// (z=max, y=max, x=max, z=min, y=min, x=min) without importing mapfile
// — this package deliberately depends on nothing else in the repository, and a
// test-only import would still be an import.
func boxFaces(min, max Vec3f) [][3]Vec3f {
	x0, y0, z0 := min[0], min[1], min[2]
	x1, y1, z1 := max[0], max[1], max[2]
	return [][3]Vec3f{
		{{x1, y1, z1}, {x1, y0, z1}, {x0, y1, z1}},
		{{x1, y1, z1}, {x0, y1, z1}, {x1, y1, z0}},
		{{x1, y1, z1}, {x1, y1, z0}, {x1, y0, z1}},
		{{x0, y0, z0}, {x1, y0, z0}, {x0, y1, z0}},
		{{x0, y0, z0}, {x0, y0, z1}, {x1, y0, z0}},
		{{x0, y0, z0}, {x0, y1, z0}, {x0, y0, z1}},
	}
}

func frozenScenarios() []scenario {
	box := boxFaces(Vec3f{0, 0, 0}, Vec3f{64, 64, 64})

	// A box with one face duplicated: the de-duplication path must fold the
	// repeat into a single polygon rather than counting the face twice.
	duplicated := append(append([][3]Vec3f{}, box...), box[0])

	// A wedge: the top face is replaced by an oblique plane cutting the box
	// diagonally, so the solid is bounded but is not an axis-aligned box. This
	// is the shape the SAT path exists for.
	wedge := [][3]Vec3f{
		box[3],                                 // z = min
		box[4],                                 // y = min
		box[5],                                 // x = min
		{{0, 64, 0}, {64, 64, 0}, {0, 64, 64}}, // y = max
		// The oblique cut, wound so its normal faces +x+z and the interior is
		// x + z <= 64. Reversing these two points faces the plane outward and
		// leaves the solid unbounded instead.
		{{64, 0, 0}, {0, 0, 64}, {64, 64, 0}},
	}

	// A sheared parallelepiped: every face is oblique, so nothing takes the
	// axis-aligned fast path and the plane de-duplication has no easy targets.
	shear := func(p Vec3f) Vec3f { return Vec3f{p[0] + 0.5*p[2], p[1] + 0.25*p[2], p[2]} }
	sheared := make([][3]Vec3f, 0, 6)
	for _, face := range box {
		sheared = append(sheared, [3]Vec3f{shear(face[0]), shear(face[1]), shear(face[2])})
	}

	// A tetrahedron: the minimum plane count that can bound a solid.
	tetra := [][3]Vec3f{
		{{0, 0, 0}, {64, 0, 0}, {0, 64, 0}},
		{{0, 0, 0}, {0, 0, 64}, {64, 0, 0}},
		{{0, 0, 0}, {0, 64, 0}, {0, 0, 64}},
		{{64, 0, 0}, {0, 0, 64}, {0, 64, 0}},
	}

	// Four planes all facing outward from one side cannot close a solid. This
	// is the "should legitimately fail to bound" case the prompt asks for.
	unbounded := [][3]Vec3f{
		box[0], box[1], box[2],
		{{0, 0, 0}, {64, 0, 0}, {0, 64, 0}},
	}
	// Only the three max-side planes plus one more: open toward -x/-y/-z.
	openSided := [][3]Vec3f{box[0], box[1], box[2], box[3]}

	rays := []rayQuery{
		{Origin: Vec3f{32, 32, 200}, Direction: Vec3f{0, 0, -1}, Max: 1000},
		{Origin: Vec3f{32, 32, 32}, Direction: Vec3f{0, 0, -1}, Max: 1000}, // inside
		{Origin: Vec3f{-200, 32, 32}, Direction: Vec3f{1, 0, 0}, Max: 1000},
		{Origin: Vec3f{-200, -200, -200}, Direction: Vec3f{1, 0, 0}, Max: 1000}, // miss
		{Origin: Vec3f{32, 32, 200}, Direction: Vec3f{0, 0, -1}, Max: 10},       // out of range
		{Origin: Vec3f{32, 32, 200}, Direction: Vec3f{0, 0, 1}, Max: 1000},      // away
		{Origin: Vec3f{0, 0, 200}, Direction: Vec3f{0, 0, -1}, Max: 1000},       // corner grazing
	}
	boxes := []boxQuery{
		{Min: Vec3f{10, 10, 10}, Max: Vec3f{20, 20, 20}},       // inside
		{Min: Vec3f{-10, -10, -10}, Max: Vec3f{10, 10, 10}},    // overlapping corner
		{Min: Vec3f{100, 100, 100}, Max: Vec3f{110, 110, 110}}, // far away
		{Min: Vec3f{-10, -10, -10}, Max: Vec3f{0, 0, 0}},       // touching corner
		{Min: Vec3f{-10, -10, -10}, Max: Vec3f{0, 0, 0}, Epsilon: 0.5},
		{Min: Vec3f{-5, -5, 30}, Max: Vec3f{5, 5, 40}},   // near the oblique cut
		{Min: Vec3f{60, 60, 60}, Max: Vec3f{70, 70, 70}}, // opposite corner
	}

	return []scenario{
		{Name: "axis_aligned_box", Faces: box, Rays: rays, Boxes: boxes},
		{Name: "duplicated_face_box", Faces: duplicated, Rays: rays, Boxes: boxes},
		{Name: "wedge", Faces: wedge, Rays: rays, Boxes: boxes},
		{Name: "sheared_parallelepiped", Faces: sheared, Rays: rays, Boxes: boxes},
		{Name: "tetrahedron", Faces: tetra, Rays: rays, Boxes: boxes},
		{Name: "thin_slab", Faces: boxFaces(Vec3f{0, 0, 0}, Vec3f{64, 64, 1}), Rays: rays, Boxes: boxes},
		{Name: "negative_coordinates", Faces: boxFaces(Vec3f{-96, -32, -16}, Vec3f{-32, 32, 48}), Rays: rays, Boxes: boxes},
		{Name: "unbounded_four_planes", Faces: unbounded},
		{Name: "open_sided", Faces: openSided},
		{Name: "too_few_planes", Faces: box[:3]},
		{Name: "degenerate_plane", Faces: [][3]Vec3f{
			{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}}, // collinear
			box[1], box[2], box[3],
		}},
	}
}

func TestPolyhedraMatchFrozenVectors(t *testing.T) {
	scenarios := frozenScenarios()

	var expected []scenarioResult
	frozenVectors(t, polyhedraGolden, func() any { return computeScenarioResults(t, scenarios) }, &expected)
	if len(expected) != len(scenarios) {
		t.Fatalf("the frozen vectors hold %d scenarios, want %d", len(expected), len(scenarios))
	}

	// A scenario whose geometry stops bounding still "agrees" with the frozen
	// vector — both raise — so it silently stops testing the solver. Count what actually
	// built, and require the non-box SAT path among it. This caught a wedge whose
	// oblique plane was wound outward and had been proving nothing.
	built, nonBox := 0, 0
	for _, want := range expected {
		if want.Error != nil {
			continue
		}
		built++
		if !want.IsBox {
			nonBox++
		}
	}
	if built < 6 {
		t.Fatalf("only %d scenarios reconstructed a solid; the solver is barely covered", built)
	}
	if nonBox < 2 {
		t.Fatalf("only %d non-box solids reconstructed; the SAT path is not covered", nonBox)
	}

	for index, want := range expected {
		got := scenarios[index]
		t.Run(got.Name, func(t *testing.T) {
			if want.Name != got.Name {
				t.Fatalf("scenario order drifted: frozen %q, Go says %q", want.Name, got.Name)
			}

			planes := make([]Plane, 0, len(got.Faces))
			var planeErr error
			for _, face := range got.Faces {
				plane, err := PlaneFromPoints(face[0], face[1], face[2])
				if err != nil {
					planeErr = err
					break
				}
				planes = append(planes, plane)
			}

			if planeErr != nil {
				if want.Error == nil {
					t.Fatalf("PlaneFromPoints failed (%v) but the frozen vector succeeded", planeErr)
				}
				if planeErr.Error() != *want.Error {
					t.Fatalf("plane error = %q, frozen %q", planeErr, *want.Error)
				}
				return
			}

			for planeIndex, plane := range planes {
				w := want.Planes[planeIndex]
				if !nearlyEqual(plane.Normal[0], w[0]) || !nearlyEqual(plane.Normal[1], w[1]) ||
					!nearlyEqual(plane.Normal[2], w[2]) || !nearlyEqual(plane.Distance, w[3]) {
					t.Errorf("plane %d = %v/%v, frozen %v", planeIndex, plane.Normal, plane.Distance, w)
				}
				key := plane.Key()
				wk := want.PlaneKeys[planeIndex]
				for component := 0; component < 4; component++ {
					if key[component] != wk[component] {
						t.Errorf("plane %d key = %v, frozen %v", planeIndex, key, wk)
						break
					}
				}
			}

			solid, mapping, err := BuildPolyhedron(planes)
			if err != nil {
				if want.Error == nil {
					t.Fatalf("BuildPolyhedron failed (%v) but the frozen vector succeeded", err)
				}
				if err.Error() != *want.Error {
					t.Fatalf("build error = %q, frozen %q", err, *want.Error)
				}
				return
			}
			if want.Error != nil {
				t.Fatalf("BuildPolyhedron succeeded but the frozen vector raised %q", *want.Error)
			}

			if len(solid.Polygons) != len(want.Polygons) {
				t.Fatalf("polygon count = %d, frozen %d", len(solid.Polygons), len(want.Polygons))
			}
			for polygonIndex, polygon := range solid.Polygons {
				wp := want.Polygons[polygonIndex]
				if len(polygon) != len(wp) {
					t.Errorf("polygon %d has %d points, frozen %d", polygonIndex, len(polygon), len(wp))
					continue
				}
				for pointIndex, point := range polygon {
					if !closeVec(point, wp[pointIndex]) {
						t.Errorf("polygon %d point %d = %v, frozen %v", polygonIndex, pointIndex, point, wp[pointIndex])
					}
				}
			}

			if !closeVec(solid.MinXYZ, want.Min) {
				t.Errorf("min = %v, frozen %v", solid.MinXYZ, want.Min)
			}
			if !closeVec(solid.MaxXYZ, want.Max) {
				t.Errorf("max = %v, frozen %v", solid.MaxXYZ, want.Max)
			}
			if !closeVec(solid.Extents(), want.Extents) {
				t.Errorf("extents = %v, frozen %v", solid.Extents(), want.Extents)
			}
			if !nearlyEqual(solid.Volume, want.Volume) {
				t.Errorf("volume = %v, frozen %v", solid.Volume, want.Volume)
			}
			if !closeVec(solid.Centroid, want.Centroid) {
				t.Errorf("centroid = %v, frozen %v", solid.Centroid, want.Centroid)
			}
			if len(mapping) != len(want.PlaneToPolygon) {
				t.Fatalf("mapping length = %d, frozen %d", len(mapping), len(want.PlaneToPolygon))
			}
			for i, value := range mapping {
				if value != want.PlaneToPolygon[i] {
					t.Errorf("plane_to_polygon = %v, frozen %v", mapping, want.PlaneToPolygon)
					break
				}
			}
			if isBox := PolyhedronIsAxisAlignedBox(solid, AxisAlignedBoxEpsilon); isBox != want.IsBox {
				t.Errorf("is_axis_aligned_box = %v, frozen %v", isBox, want.IsBox)
			}
			if count := len(solid.Edges()); count != want.EdgeCount {
				t.Errorf("edge count = %d, frozen %d", count, want.EdgeCount)
			}

			for polygonIndex, polygon := range solid.Polygons {
				if area := WindingArea(polygon); !nearlyEqual(area, want.Areas[polygonIndex]) {
					t.Errorf("polygon %d area = %v, frozen %v", polygonIndex, area, want.Areas[polygonIndex])
				}
				if normal := WindingNormal(polygon); !closeVec(normal, want.Normals[polygonIndex]) {
					t.Errorf("polygon %d normal = %v, frozen %v", polygonIndex, normal, want.Normals[polygonIndex])
				}
				if centroid := WindingCentroid(polygon); !closeVec(centroid, want.Centroids[polygonIndex]) {
					t.Errorf("polygon %d centroid = %v, frozen %v", polygonIndex, centroid, want.Centroids[polygonIndex])
				}
			}

			for _, pair := range want.Orientations {
				planeIndex := int(pair[0])
				polygon := solid.Polygons[mapping[planeIndex]]
				if orientation := WindingOrientation(polygon, planes[planeIndex].Normal); !nearlyEqual(orientation, pair[1]) {
					t.Errorf("plane %d orientation = %v, frozen %v", planeIndex, orientation, pair[1])
				}
			}
			for _, pair := range want.Offsets {
				planeIndex := int(pair[0])
				polygon := solid.Polygons[mapping[planeIndex]]
				if offset := PolygonPlaneOffset(polygon, planes[planeIndex]); !nearlyEqual(offset, pair[1]) {
					t.Errorf("plane %d plane offset = %v, frozen %v", planeIndex, offset, pair[1])
				}
			}

			for rayIndex, ray := range got.Rays {
				distance, hit := RayHitsPolyhedron(ray.Origin, ray.Direction, solid, ray.Max)
				w := want.Rays[rayIndex]
				if w == nil {
					if hit {
						t.Errorf("ray %d hit at %v, frozen miss", rayIndex, distance)
					}
					continue
				}
				if !hit {
					t.Errorf("ray %d missed, frozen hit at %v", rayIndex, *w)
					continue
				}
				if !nearlyEqual(distance, *w) {
					t.Errorf("ray %d distance = %v, frozen %v", rayIndex, distance, *w)
				}
			}

			for boxIndex, box := range got.Boxes {
				if hit := BoxIntersectsPolyhedron(box.Min, box.Max, solid, box.Epsilon); hit != want.BoxHits[boxIndex] {
					t.Errorf("box %d intersects = %v, frozen %v", boxIndex, hit, want.BoxHits[boxIndex])
				}
			}
		})
	}
}

// ── primitive-level vectors ─────────────────────────────────────────────────

type primitivePayload struct {
	SnapPoints     []Vec3f    `json:"snap_points"`
	SnapGrids      []float64  `json:"snap_grids"`
	BaseWindings   []Plane2   `json:"base_windings"`
	ClipCases      []clipCase `json:"clip_cases"`
	DuplicateCases [][]Vec3f  `json:"duplicate_cases"`
	AreaCases      [][]Vec3f  `json:"area_cases"`
	PlaneCases     [][3]Vec3f `json:"plane_cases"`
}

// Plane2 is the wire form of a plane, kept separate from Plane so the JSON
// field names are stable whatever the Go type's are.
type Plane2 struct {
	Normal   Vec3f   `json:"normal"`
	Distance float64 `json:"distance"`
}

type clipCase struct {
	Points  []Vec3f `json:"points"`
	Plane   Plane2  `json:"plane"`
	Epsilon float64 `json:"epsilon"`
}

type primitiveResult struct {
	Snapped    [][]float64   `json:"snapped"`
	Windings   [][][]float64 `json:"windings"`
	Clipped    [][][]float64 `json:"clipped"`
	Duplicates [][][]float64 `json:"duplicates"`
	Areas      []float64     `json:"areas"`
	Normals    [][]float64   `json:"normals"`
	Centroids  [][]float64   `json:"centroids"`
	Planes     []*[]float64  `json:"planes"`
}

func TestPrimitivesMatchFrozenVectors(t *testing.T) {
	// SnapPoint uses round-half-to-even, like Python's round(). Every value below is an
	// exact .5 multiple of the grid, where half-to-even and half-away-from-zero
	// disagree — which is the whole point of testing it here.
	snapPoints := []Vec3f{
		{0.005, 0.015, 0.025},
		{-0.005, -0.015, -0.025},
		{0.0349999, 0.045, 1.005},
		{12.345, -12.345, 0.0},
		{0.5, 1.5, 2.5},
		{-0.5, -1.5, -2.5},
	}
	snapGrids := []float64{SnapGrid, SnapGrid, SnapGrid, SnapGrid, 1.0, 1.0}

	baseWindings := []Plane2{
		{Normal: Vec3f{0, 0, 1}, Distance: 64},
		{Normal: Vec3f{0, 0, -1}, Distance: 0},
		{Normal: Vec3f{1, 0, 0}, Distance: -32},
		// A normal whose largest component ties between two axes: the tie
		// resolves to the lowest index, which selects a different "up".
		{Normal: VecNormalize(Vec3f{1, 1, 0}), Distance: 10},
		{Normal: VecNormalize(Vec3f{1, 1, 1}), Distance: 5},
		{Normal: VecNormalize(Vec3f{0, 1, 1}), Distance: -7},
	}

	square := []Vec3f{{0, 0, 0}, {10, 0, 0}, {10, 10, 0}, {0, 10, 0}}
	cutter := Plane2{Normal: Vec3f{1, 0, 0}, Distance: 5}
	clipCases := []clipCase{
		{Points: square, Plane: cutter, Epsilon: PlaneDistanceEpsilon},
		// Entirely inside and entirely outside.
		{Points: square, Plane: Plane2{Normal: Vec3f{1, 0, 0}, Distance: 100}, Epsilon: PlaneDistanceEpsilon},
		{Points: square, Plane: Plane2{Normal: Vec3f{1, 0, 0}, Distance: -100}, Epsilon: PlaneDistanceEpsilon},
		// Exactly on the epsilon boundary in both directions.
		{Points: square, Plane: Plane2{Normal: Vec3f{1, 0, 0}, Distance: 10 - PlaneDistanceEpsilon}, Epsilon: PlaneDistanceEpsilon},
		{Points: square, Plane: Plane2{Normal: Vec3f{1, 0, 0}, Distance: 10 + PlaneDistanceEpsilon}, Epsilon: PlaneDistanceEpsilon},
		{Points: square, Plane: Plane2{Normal: Vec3f{1, 0, 0}, Distance: 0}, Epsilon: PlaneDistanceEpsilon},
		{Points: square, Plane: Plane2{Normal: VecNormalize(Vec3f{1, 1, 0}), Distance: 7}, Epsilon: PlaneDistanceEpsilon},
		{Points: nil, Plane: cutter, Epsilon: PlaneDistanceEpsilon},
	}

	duplicateCases := [][]Vec3f{
		{{0, 0, 0}, {0, 0, 0}, {1, 0, 0}},
		{{0, 0, 0}, {1, 0, 0}, {0, 0, 0}},
		// Separated by exactly the tolerance, and by just over it.
		{{0, 0, 0}, {PointOnPlaneEpsilon, 0, 0}, {1, 0, 0}},
		{{0, 0, 0}, {PointOnPlaneEpsilon * 1.0001, 0, 0}, {1, 0, 0}},
		{{5, 5, 5}},
		{},
	}

	areaCases := [][]Vec3f{
		square,
		{{0, 0, 0}, {1, 0, 0}},
		{{0, 0, 0}},
		// A sliver whose area lands near AREA_EPSILON.
		{{0, 0, 0}, {1e-3, 0, 0}, {1e-3, 2e-3, 0}},
		{{0, 0, 0}, {1, 1, 1}, {2, 2, 2}}, // collinear: zero area
		{{0, 0, 0}, {10, 0, 0}, {10, 10, 5}, {0, 10, 5}},
	}

	planeCases := [][3]Vec3f{
		{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		{{0, 0, 0}, {2, 0, 0}, {4, 0, 0}},       // collinear -> error
		{{0, 0, 0}, {0, 0, 0}, {0, 0, 0}},       // coincident -> error
		{{0, 0, 0}, {1e-3, 0, 0}, {0, 1e-3, 0}}, // tiny but valid
		// Cross-product length just under and just over NORMAL_EPSILON.
		{{0, 0, 0}, {1e-3, 0, 0}, {0, 3e-3, 0}},
		{{64, 64, 64}, {64, 0, 64}, {0, 64, 64}},
	}

	payload := primitivePayload{
		SnapPoints:     snapPoints,
		SnapGrids:      snapGrids,
		BaseWindings:   baseWindings,
		ClipCases:      clipCases,
		DuplicateCases: duplicateCases,
		AreaCases:      areaCases,
		PlaneCases:     planeCases,
	}

	var want primitiveResult
	frozenVectors(t, primitivesGolden, func() any { return computePrimitiveResults(t, payload) }, &want)

	for index, point := range snapPoints {
		if got := SnapPoint(point, snapGrids[index]); !closeVec(got, want.Snapped[index]) {
			t.Errorf("SnapPoint(%v, %v) = %v, frozen %v", point, snapGrids[index], got, want.Snapped[index])
		}
	}

	for index, plane := range baseWindings {
		got := BaseWinding(Plane{Normal: plane.Normal, Distance: plane.Distance}, BaseWindingExtent)
		w := want.Windings[index]
		if len(got) != len(w) {
			t.Errorf("BaseWinding %d has %d points, frozen %d", index, len(got), len(w))
			continue
		}
		for pointIndex, point := range got {
			if !closeVec(point, w[pointIndex]) {
				t.Errorf("BaseWinding %d point %d = %v, frozen %v", index, pointIndex, point, w[pointIndex])
			}
		}
	}

	for index, testCase := range clipCases {
		got := ClipWinding(testCase.Points, Plane{Normal: testCase.Plane.Normal, Distance: testCase.Plane.Distance}, testCase.Epsilon)
		w := want.Clipped[index]
		if len(got) != len(w) {
			t.Errorf("ClipWinding %d returned %d points, frozen %d", index, len(got), len(w))
			continue
		}
		for pointIndex, point := range got {
			if !closeVec(point, w[pointIndex]) {
				t.Errorf("ClipWinding %d point %d = %v, frozen %v", index, pointIndex, point, w[pointIndex])
			}
		}
	}

	for index, testCase := range duplicateCases {
		got := RemoveDuplicatePoints(testCase, PointOnPlaneEpsilon)
		w := want.Duplicates[index]
		if len(got) != len(w) {
			t.Errorf("RemoveDuplicatePoints %d returned %d points, frozen %d", index, len(got), len(w))
			continue
		}
		for pointIndex, point := range got {
			if !closeVec(point, w[pointIndex]) {
				t.Errorf("RemoveDuplicatePoints %d point %d = %v, frozen %v", index, pointIndex, point, w[pointIndex])
			}
		}
	}

	for index, testCase := range areaCases {
		if area := WindingArea(testCase); !nearlyEqual(area, want.Areas[index]) {
			t.Errorf("WindingArea %d = %v, frozen %v", index, area, want.Areas[index])
		}
		if normal := WindingNormal(testCase); !closeVec(normal, want.Normals[index]) {
			t.Errorf("WindingNormal %d = %v, frozen %v", index, normal, want.Normals[index])
		}
		if centroid := WindingCentroid(testCase); !closeVec(centroid, want.Centroids[index]) {
			t.Errorf("WindingCentroid %d = %v, frozen %v", index, centroid, want.Centroids[index])
		}
	}

	for index, testCase := range planeCases {
		plane, err := PlaneFromPoints(testCase[0], testCase[1], testCase[2])
		w := want.Planes[index]
		if w == nil {
			if err == nil {
				t.Errorf("PlaneFromPoints %d succeeded (%v), the frozen vector raised", index, plane)
			}
			continue
		}
		if err != nil {
			t.Errorf("PlaneFromPoints %d failed (%v), frozen %v", index, err, *w)
			continue
		}
		got := (*w)
		if !nearlyEqual(plane.Normal[0], got[0]) || !nearlyEqual(plane.Normal[1], got[1]) ||
			!nearlyEqual(plane.Normal[2], got[2]) || !nearlyEqual(plane.Distance, got[3]) {
			t.Errorf("PlaneFromPoints %d = %v/%v, frozen %v", index, plane.Normal, plane.Distance, got)
		}
	}
}
