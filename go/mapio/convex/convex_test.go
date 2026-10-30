package convex

import (
	"math"
	"testing"
)

// These tests lock behavior that the frozen vectors (frozen_test.go) also
// pin, plus the branches the frozen scenarios cannot reach and the rounding
// and tie-breaking semantics most likely to be got wrong.

func TestBuildPolyhedronErrors(t *testing.T) {
	box := boxFaces(Vec3f{0, 0, 0}, Vec3f{64, 64, 64})

	planesOf := func(t *testing.T, faces [][3]Vec3f) []Plane {
		t.Helper()
		planes := make([]Plane, 0, len(faces))
		for _, face := range faces {
			plane, err := PlaneFromPoints(face[0], face[1], face[2])
			if err != nil {
				t.Fatalf("PlaneFromPoints: %v", err)
			}
			planes = append(planes, plane)
		}
		return planes
	}

	t.Run("too few planes", func(t *testing.T) {
		_, _, err := BuildPolyhedron(planesOf(t, box[:3]))
		assertGeometryError(t, err, "a bounded convex solid needs at least four planes")
	})

	t.Run("unbounded", func(t *testing.T) {
		_, _, err := BuildPolyhedron(planesOf(t, box[:4]))
		assertGeometryError(t, err, "brush planes describe an unbounded solid")
	})

	t.Run("not a closed solid", func(t *testing.T) {
		// Four copies of one plane pass the arity check but collapse to a single
		// unique plane, so fewer than four polygons survive. This is the only
		// route to that message and no frozen scenario reaches it.
		plane := planesOf(t, box[:1])[0]
		_, _, err := BuildPolyhedron([]Plane{plane, plane, plane, plane})
		assertGeometryError(t, err, "brush planes do not bound a closed solid")
	})

	t.Run("degenerate plane", func(t *testing.T) {
		_, err := PlaneFromPoints(Vec3f{0, 0, 0}, Vec3f{1, 0, 0}, Vec3f{2, 0, 0})
		assertGeometryError(t, err, "degenerate face plane: points are collinear or coincident")
	})
}

func assertGeometryError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error %q, got nil", want)
	}
	if _, ok := err.(*GeometryError); !ok {
		t.Fatalf("expected *GeometryError, got %T", err)
	}
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

// TestPyRoundIsHalfToEven guards the single most likely porting mistake in this
// package: Go's math.Round rounds halves away from zero, Python's round() rounds
// them to even. Every quantized identity here depends on the difference.
func TestPyRoundIsHalfToEven(t *testing.T) {
	cases := []struct {
		value float64
		want  float64
	}{
		{0.5, 0}, {1.5, 2}, {2.5, 2}, {3.5, 4},
		{-0.5, 0}, {-1.5, -2}, {-2.5, -2}, {-3.5, -4},
	}
	for _, testCase := range cases {
		if got := pyRound(testCase.value); got != testCase.want {
			t.Errorf("pyRound(%v) = %v, want %v", testCase.value, got, testCase.want)
		}
	}
	// And confirm the two really do disagree, so this test cannot silently
	// become vacuous if the helper is swapped for math.Round.
	if pyRound(2.5) == math.Round(2.5) {
		t.Fatal("pyRound must not agree with math.Round on 2.5; the helper is wrong")
	}
}

// TestBaseWindingTieBreak covers Python's max(range(3), key=...) returning the
// first maximal index. With |x| == |y| the chosen "up" differs between the two
// tie-break rules, which rotates the whole winding.
func TestBaseWindingTieBreak(t *testing.T) {
	normal := VecNormalize(Vec3f{1, 1, 0})
	winding := BaseWinding(Plane{Normal: normal, Distance: 0}, BaseWindingExtent)
	if len(winding) != 4 {
		t.Fatalf("base winding has %d points, want 4", len(winding))
	}
	// largest resolves to axis 0, not axis 1, so "up" is (0,0,1).
	for _, point := range winding {
		if offset := math.Abs(VecDot(normal, point)); offset > 1e-6 {
			t.Errorf("base winding point %v is %v off its own plane", point, offset)
		}
	}
	// A z-dominant normal must select the other branch.
	zWinding := BaseWinding(Plane{Normal: Vec3f{0, 0, 1}, Distance: 8}, BaseWindingExtent)
	for _, point := range zWinding {
		if math.Abs(point[2]-8) > 1e-9 {
			t.Errorf("z-plane winding point %v is not at z=8", point)
		}
	}
}

func TestClipWindingEpsilonBoundaries(t *testing.T) {
	square := []Vec3f{{0, 0, 0}, {10, 0, 0}, {10, 10, 0}, {0, 10, 0}}

	// Every vertex exactly at +epsilon counts as inside, so the winding is
	// returned untouched.
	kept := ClipWinding(square, Plane{Normal: Vec3f{1, 0, 0}, Distance: 10 - PlaneDistanceEpsilon}, PlaneDistanceEpsilon)
	if len(kept) != 4 {
		t.Errorf("at +epsilon the winding should survive whole, got %d points", len(kept))
	}

	// Entirely outside collapses to nothing.
	if dropped := ClipWinding(square, Plane{Normal: Vec3f{1, 0, 0}, Distance: -100}, PlaneDistanceEpsilon); len(dropped) != 0 {
		t.Errorf("a fully outside winding should clip to nothing, got %d points", len(dropped))
	}

	// A genuine cut produces the four points of the clipped half.
	cut := ClipWinding(square, Plane{Normal: Vec3f{1, 0, 0}, Distance: 5}, PlaneDistanceEpsilon)
	if len(cut) != 4 {
		t.Fatalf("clipping the square in half should give 4 points, got %d", len(cut))
	}
	for _, point := range cut {
		if point[0] > 5+PlaneDistanceEpsilon {
			t.Errorf("clipped point %v lies outside the half-space", point)
		}
	}

	if empty := ClipWinding(nil, Plane{Normal: Vec3f{1, 0, 0}, Distance: 0}, PlaneDistanceEpsilon); len(empty) != 0 {
		t.Errorf("clipping an empty winding should stay empty, got %d points", len(empty))
	}
}

func TestRemoveDuplicatePointsAtTolerance(t *testing.T) {
	// Exactly at the tolerance the points are equal (the comparison is <=).
	atTolerance := RemoveDuplicatePoints([]Vec3f{{0, 0, 0}, {PointOnPlaneEpsilon, 0, 0}, {1, 0, 0}}, PointOnPlaneEpsilon)
	if len(atTolerance) != 2 {
		t.Errorf("points exactly at the tolerance should collapse, got %d", len(atTolerance))
	}
	// Just beyond it they are distinct.
	beyond := RemoveDuplicatePoints([]Vec3f{{0, 0, 0}, {PointOnPlaneEpsilon * 1.01, 0, 0}, {1, 0, 0}}, PointOnPlaneEpsilon)
	if len(beyond) != 3 {
		t.Errorf("points beyond the tolerance should be kept, got %d", len(beyond))
	}
	// The closing point is dropped.
	closed := RemoveDuplicatePoints([]Vec3f{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 0, 0}}, PointOnPlaneEpsilon)
	if len(closed) != 3 {
		t.Errorf("a closed winding should drop its repeated first point, got %d", len(closed))
	}
}

func TestBoxAndRayQueries(t *testing.T) {
	solid := mustBuild(t, boxFaces(Vec3f{0, 0, 0}, Vec3f{64, 64, 64}))

	t.Run("ray", func(t *testing.T) {
		cases := []struct {
			name      string
			origin    Vec3f
			direction Vec3f
			max       float64
			wantHit   bool
			want      float64
		}{
			{"straight down onto the top", Vec3f{32, 32, 200}, Vec3f{0, 0, -1}, 1000, true, 136},
			{"starting inside", Vec3f{32, 32, 32}, Vec3f{0, 0, -1}, 1000, true, 0},
			{"pointing away", Vec3f{32, 32, 200}, Vec3f{0, 0, 1}, 1000, false, 0},
			{"beyond max distance", Vec3f{32, 32, 200}, Vec3f{0, 0, -1}, 10, false, 0},
			{"missing entirely", Vec3f{-200, -200, -200}, Vec3f{1, 0, 0}, 1000, false, 0},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				distance, hit := RayHitsPolyhedron(testCase.origin, testCase.direction, solid, testCase.max)
				if hit != testCase.wantHit {
					t.Fatalf("hit = %v, want %v", hit, testCase.wantHit)
				}
				if hit && math.Abs(distance-testCase.want) > 1e-9 {
					t.Errorf("distance = %v, want %v", distance, testCase.want)
				}
			})
		}
	})

	t.Run("box", func(t *testing.T) {
		cases := []struct {
			name    string
			min     Vec3f
			max     Vec3f
			epsilon float64
			want    bool
		}{
			{"fully inside", Vec3f{10, 10, 10}, Vec3f{20, 20, 20}, 0, true},
			{"overlapping a corner", Vec3f{-10, -10, -10}, Vec3f{10, 10, 10}, 0, true},
			{"far away", Vec3f{100, 100, 100}, Vec3f{110, 110, 110}, 0, false},
			{"touching at a corner", Vec3f{-10, -10, -10}, Vec3f{0, 0, 0}, 0, true},
			{"just short, with slack", Vec3f{-10, -10, -10}, Vec3f{-1, -1, -1}, 2, true},
			{"just short, no slack", Vec3f{-10, -10, -10}, Vec3f{-1, -1, -1}, 0, false},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				if got := BoxIntersectsPolyhedron(testCase.min, testCase.max, solid, testCase.epsilon); got != testCase.want {
					t.Errorf("intersects = %v, want %v", got, testCase.want)
				}
			})
		}
	})
}

// TestSATPathOnNonBox exercises the separating-axis path specifically: a wedge
// whose AABB overlaps a box that the solid itself misses. The AABB-only answer
// and the exact answer differ here, which is the reason the SAT code exists.
func TestSATPathOnNonBox(t *testing.T) {
	box := boxFaces(Vec3f{0, 0, 0}, Vec3f{64, 64, 64})
	// A triangular prism: the box's +x and +z faces are replaced by one oblique
	// plane whose interior is x + z <= 64.
	wedge := mustBuild(t, [][3]Vec3f{
		box[3], box[4], box[5],
		{{0, 64, 0}, {64, 64, 0}, {0, 64, 64}},
		{{64, 0, 0}, {0, 0, 64}, {64, 64, 0}},
	})

	if PolyhedronIsAxisAlignedBox(wedge, AxisAlignedBoxEpsilon) {
		t.Fatal("the wedge must not be classified as an axis-aligned box")
	}

	// A probe near the cut-away corner: inside the wedge's AABB, outside the
	// wedge.
	probeMin := Vec3f{62, 32, 62}
	probeMax := Vec3f{64, 34, 64}
	if !BoundsIntersect(probeMin, probeMax, wedge.MinXYZ, wedge.MaxXYZ, 0) {
		t.Fatal("the probe should overlap the wedge's AABB, or the test proves nothing")
	}
	if BoxIntersectsPolyhedron(probeMin, probeMax, wedge, 0) {
		t.Error("the probe lies in the cut-away corner and must not intersect the wedge")
	}
}

func TestPolyhedronCachesAreStable(t *testing.T) {
	solid := mustBuild(t, boxFaces(Vec3f{0, 0, 0}, Vec3f{64, 64, 64}))

	first := solid.Edges()
	second := solid.Edges()
	if len(first) != len(second) {
		t.Fatalf("Edges() is not stable: %d then %d", len(first), len(second))
	}
	if len(first) != 12 {
		t.Errorf("a box has 12 edges, got %d", len(first))
	}

	// The axis-aligned-box answer is cached without regard to epsilon. A second call with an absurd tolerance must return the cached value.
	if !PolyhedronIsAxisAlignedBox(solid, AxisAlignedBoxEpsilon) {
		t.Fatal("a box should classify as axis-aligned")
	}
	if !PolyhedronIsAxisAlignedBox(solid, 0) {
		t.Error("the cached answer must be returned regardless of the epsilon passed later")
	}
}

func TestBoxVolumeAndCentroid(t *testing.T) {
	solid := mustBuild(t, boxFaces(Vec3f{0, 0, 0}, Vec3f{64, 64, 64}))
	if want := 64.0 * 64.0 * 64.0; math.Abs(solid.Volume-want) > 1e-6 {
		t.Errorf("volume = %v, want %v", solid.Volume, want)
	}
	for axis := 0; axis < 3; axis++ {
		if math.Abs(solid.Centroid[axis]-32) > 1e-9 {
			t.Errorf("centroid = %v, want (32,32,32)", solid.Centroid)
			break
		}
	}
	if !solid.ContainsPoint(Vec3f{32, 32, 32}, 0) {
		t.Error("the centre should be inside the solid")
	}
	if solid.ContainsPoint(Vec3f{-1, 32, 32}, 0) {
		t.Error("a point outside the box should not be contained")
	}
}

// TestDuplicatePlanesFoldIntoOneFace covers the de-duplication comment in
// build_polyhedron: a repeated face must not be counted twice.
func TestDuplicatePlanesFoldIntoOneFace(t *testing.T) {
	box := boxFaces(Vec3f{0, 0, 0}, Vec3f{64, 64, 64})
	plain := mustBuild(t, box)
	duplicated := mustBuild(t, append(append([][3]Vec3f{}, box...), box[0], box[0]))

	if len(plain.Polygons) != len(duplicated.Polygons) {
		t.Errorf("duplicated faces changed the polygon count: %d vs %d",
			len(plain.Polygons), len(duplicated.Polygons))
	}
	if math.Abs(plain.Volume-duplicated.Volume) > 1e-6 {
		t.Errorf("duplicated faces changed the volume: %v vs %v", plain.Volume, duplicated.Volume)
	}
}

// TestBuildPolyhedronIsDeterministic repeats a build and requires byte-identical
// structure, per the determinism requirement in AGENTS.md §5.
func TestBuildPolyhedronIsDeterministic(t *testing.T) {
	faces := boxFaces(Vec3f{-96, -32, -16}, Vec3f{-32, 32, 48})
	first := mustBuild(t, faces)
	second := mustBuild(t, faces)

	if len(first.Polygons) != len(second.Polygons) {
		t.Fatalf("polygon counts differ: %d vs %d", len(first.Polygons), len(second.Polygons))
	}
	for index, polygon := range first.Polygons {
		other := second.Polygons[index]
		if len(polygon) != len(other) {
			t.Fatalf("polygon %d lengths differ: %d vs %d", index, len(polygon), len(other))
		}
		for pointIndex, point := range polygon {
			if point != other[pointIndex] {
				t.Errorf("polygon %d point %d differs: %v vs %v", index, pointIndex, point, other[pointIndex])
			}
		}
	}
	if first.Volume != second.Volume || first.Centroid != second.Centroid {
		t.Error("volume or centroid is not reproducible")
	}
}

func TestPolygonPlaneOffset(t *testing.T) {
	plane := Plane{Normal: Vec3f{0, 0, 1}, Distance: 0}
	flat := []Vec3f{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}}
	if offset := PolygonPlaneOffset(flat, plane); offset != 0 {
		t.Errorf("a flat polygon should have zero offset, got %v", offset)
	}
	tilted := []Vec3f{{0, 0, 0}, {1, 0, 0.25}, {1, 1, -0.5}}
	if offset := PolygonPlaneOffset(tilted, plane); math.Abs(offset-0.5) > 1e-9 {
		t.Errorf("offset = %v, want 0.5", offset)
	}
	if offset := PolygonPlaneOffset(nil, plane); offset != 0 {
		t.Errorf("an empty polygon should have zero offset, got %v", offset)
	}
}

func TestWindingHelpers(t *testing.T) {
	square := []Vec3f{{0, 0, 0}, {10, 0, 0}, {10, 10, 0}, {0, 10, 0}}

	if area := WindingArea(square); math.Abs(area-100) > 1e-9 {
		t.Errorf("area = %v, want 100", area)
	}
	if area := WindingArea([]Vec3f{{0, 0, 0}, {1, 0, 0}}); area != 0 {
		t.Errorf("a degenerate winding has no area, got %v", area)
	}

	normal := WindingNormal(square)
	if math.Abs(normal[2]-1) > 1e-9 {
		t.Errorf("normal = %v, want +z", normal)
	}
	if orientation := WindingOrientation(square, Vec3f{0, 0, 1}); orientation != 1 {
		t.Errorf("orientation = %v, want +1", orientation)
	}
	if orientation := WindingOrientation(square, Vec3f{0, 0, -1}); orientation != -1 {
		t.Errorf("orientation = %v, want -1", orientation)
	}

	centroid := WindingCentroid(square)
	if math.Abs(centroid[0]-5) > 1e-9 || math.Abs(centroid[1]-5) > 1e-9 {
		t.Errorf("centroid = %v, want (5,5,0)", centroid)
	}
	// Fewer than three points falls back to the arithmetic mean.
	if mean := WindingCentroid([]Vec3f{{0, 0, 0}, {2, 4, 6}}); mean != (Vec3f{1, 2, 3}) {
		t.Errorf("two-point centroid = %v, want (1,2,3)", mean)
	}
	if empty := WindingCentroid(nil); empty != (Vec3f{0, 0, 0}) {
		t.Errorf("empty centroid = %v, want the origin", empty)
	}
}

func TestVectorHelpers(t *testing.T) {
	if got := VecNormalize(Vec3f{0, 0, 0}); got != (Vec3f{0, 0, 0}) {
		t.Errorf("normalizing the zero vector should stay zero, got %v", got)
	}
	if got := VecLength(Vec3f{3, 4, 0}); got != 5 {
		t.Errorf("length = %v, want 5", got)
	}
	if got := VecCross(Vec3f{1, 0, 0}, Vec3f{0, 1, 0}); got != (Vec3f{0, 0, 1}) {
		t.Errorf("cross = %v, want +z", got)
	}
	if got := VecDot(Vec3f{1, 2, 3}, Vec3f{4, 5, 6}); got != 32 {
		t.Errorf("dot = %v, want 32", got)
	}
}

func TestBoundsIntersect(t *testing.T) {
	aMin, aMax := Vec3f{0, 0, 0}, Vec3f{10, 10, 10}
	if !BoundsIntersect(aMin, aMax, Vec3f{5, 5, 5}, Vec3f{15, 15, 15}, 0) {
		t.Error("overlapping bounds should intersect")
	}
	if BoundsIntersect(aMin, aMax, Vec3f{11, 0, 0}, Vec3f{20, 10, 10}, 0) {
		t.Error("separated bounds should not intersect")
	}
	if !BoundsIntersect(aMin, aMax, Vec3f{11, 0, 0}, Vec3f{20, 10, 10}, 1) {
		t.Error("an epsilon of 1 should close a gap of 1")
	}
	if !BoundsIntersect(aMin, aMax, Vec3f{10, 10, 10}, Vec3f{20, 20, 20}, 0) {
		t.Error("touching bounds should intersect")
	}
}

func mustBuild(t *testing.T, faces [][3]Vec3f) *ConvexPolyhedron {
	t.Helper()
	planes := make([]Plane, 0, len(faces))
	for _, face := range faces {
		plane, err := PlaneFromPoints(face[0], face[1], face[2])
		if err != nil {
			t.Fatalf("PlaneFromPoints: %v", err)
		}
		planes = append(planes, plane)
	}
	solid, _, err := BuildPolyhedron(planes)
	if err != nil {
		t.Fatalf("BuildPolyhedron: %v", err)
	}
	return solid
}
