// Package convex reconstructs exact convex brush geometry: planes, windings,
// polyhedra and collision queries.
//
// Quake .map brushes are stored as an intersection of half-spaces, not as
// bounded solids. Everything downstream (exposure classification, contact
// relations, walkability) needs the actual bounded polyhedron, so this package
// reconstructs it with the classic base-winding/clip algorithm and then offers
// exact narrow-phase queries against the convex planes.
//
// AABBs are produced too, but only as broad-phase accelerators and recorded
// approximate metrics. They are never the collision model.
//
// It depends only on pysum — it takes planes and points and returns planes and
// points — so any consumer of map geometry can share it.
package convex

import (
	"math"
	"sync"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/pysum"
)

// Vec3f is a three-component vector. The array form keeps it comparable and usable as a map key component, as the quantized
// identity helpers below need.
type Vec3f [3]float64

// Geometry tolerances. These are load-bearing for
// degenerate and near-degenerate geometry, not decorative.
const (
	// BaseWindingExtent is half the Quake world span; base windings start
	// larger than any real map.
	BaseWindingExtent = 65536.0

	// Plane-coincidence tolerances. Quake map coordinates are integers in
	// practice, so these are generous relative to real geometry yet far below
	// one unit.
	PlaneDistanceEpsilon = 0.01
	NormalEpsilon        = 1e-5
	PointOnPlaneEpsilon  = 0.01
	AreaEpsilon          = 1e-6

	// SnapGrid is snap_point's default grid.
	SnapGrid = 0.01

	// AxisAlignedBoxEpsilon is PolyhedronIsAxisAlignedBox's default tolerance.
	// It is named here because Go has no default arguments.
	AxisAlignedBoxEpsilon = 1e-4
)

// pyRound reproduces Python's built-in round() for floats, which is
// round-half-to-even, not round-half-away-from-zero. Go's math.Round is the
// latter, so using it here would change the answer on every exact .5 boundary —
// and these helpers quantize coordinates precisely to land on such boundaries.
func pyRound(value float64) float64 { return math.RoundToEven(value) }

// GeometryError reports brush planes that cannot form a bounded convex solid.
type GeometryError struct{ Message string }

func (e *GeometryError) Error() string { return e.Message }

func geometryError(message string) error { return &GeometryError{Message: message} }

// VecSub returns a - b.
func VecSub(a, b Vec3f) Vec3f { return Vec3f{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }

// VecAdd returns a + b.
func VecAdd(a, b Vec3f) Vec3f { return Vec3f{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }

// VecScale returns a * scalar.
func VecScale(a Vec3f, scalar float64) Vec3f {
	return Vec3f{a[0] * scalar, a[1] * scalar, a[2] * scalar}
}

// VecDot returns the dot product of a and b.
func VecDot(a, b Vec3f) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

// VecCross returns the cross product of a and b.
func VecCross(a, b Vec3f) Vec3f {
	return Vec3f{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

// VecLength returns the Euclidean length of a.
func VecLength(a Vec3f) float64 { return math.Sqrt(a[0]*a[0] + a[1]*a[1] + a[2]*a[2]) }

// VecNormalize scales a to unit length, returning the zero vector when a has no
// length. The length is tested against exact zero here, not against NormalEpsilon.
func VecNormalize(a Vec3f) Vec3f {
	length := VecLength(a)
	if length == 0.0 {
		return Vec3f{0.0, 0.0, 0.0}
	}
	return Vec3f{a[0] / length, a[1] / length, a[2] / length}
}

// PlaneKey is a plane's deterministic quantized identity.
type PlaneKey [4]int64

// Plane is an outward-facing half-space dot(normal, x) <= distance.
type Plane struct {
	Normal   Vec3f
	Distance float64
}

// Evaluate returns the signed distance of point from the plane: negative inside
// the half-space, positive outside.
func (p Plane) Evaluate(point Vec3f) float64 { return VecDot(p.Normal, point) - p.Distance }

// Key returns a deterministic quantized identity for de-duplicating planes.
func (p Plane) Key() PlaneKey {
	return PlaneKey{
		int64(pyRound(p.Normal[0] / NormalEpsilon)),
		int64(pyRound(p.Normal[1] / NormalEpsilon)),
		int64(pyRound(p.Normal[2] / NormalEpsilon)),
		int64(pyRound(p.Distance / PlaneDistanceEpsilon)),
	}
}

// PlaneFromPoints builds the outward Quake plane from a .map face's three
// points.
//
// Quake's own qbsp computes cross(p1 - p2, p3 - p2) and the resulting normal
// points out of the solid, so the brush interior satisfies
// dot(normal, x) <= distance.
func PlaneFromPoints(p1, p2, p3 Vec3f) (Plane, error) {
	normal := VecCross(VecSub(p1, p2), VecSub(p3, p2))
	length := VecLength(normal)
	if length < NormalEpsilon {
		return Plane{}, geometryError("degenerate face plane: points are collinear or coincident")
	}
	normal = Vec3f{normal[0] / length, normal[1] / length, normal[2] / length}
	return Plane{Normal: normal, Distance: VecDot(normal, p1)}, nil
}

// BaseWinding returns a large quad lying on plane, wound counter-clockwise.
// Pass BaseWindingExtent for the default extent.
func BaseWinding(plane Plane, extent float64) []Vec3f {
	normal := plane.Normal

	// Ties resolve to the lowest axis index, the *first* maximal element. A
	// ">" test preserves that; ">=" would not.
	largest := 0
	for index := 1; index < 3; index++ {
		if math.Abs(normal[index]) > math.Abs(normal[largest]) {
			largest = index
		}
	}

	var up Vec3f
	if largest == 2 {
		up = Vec3f{1.0, 0.0, 0.0}
	} else {
		up = Vec3f{0.0, 0.0, 1.0}
	}
	up = VecSub(up, VecScale(normal, VecDot(up, normal)))
	up = VecNormalize(up)
	right := VecCross(up, normal)
	origin := VecScale(normal, plane.Distance)
	up = VecScale(up, extent)
	right = VecScale(right, extent)
	return []Vec3f{
		VecAdd(VecSub(origin, right), up),
		VecAdd(VecAdd(origin, right), up),
		VecSub(VecAdd(origin, right), up),
		VecSub(VecSub(origin, right), up),
	}
}

// ClipWinding keeps the part of points inside the half-space of plane. Pass
// PlaneDistanceEpsilon for the default tolerance.
func ClipWinding(points []Vec3f, plane Plane, epsilon float64) []Vec3f {
	if len(points) == 0 {
		return nil
	}
	distances := make([]float64, len(points))
	for index, point := range points {
		distances[index] = plane.Evaluate(point)
	}

	allInside := true
	allOutside := true
	for _, distance := range distances {
		if distance > epsilon {
			allInside = false
		}
		if distance < -epsilon {
			allOutside = false
		}
	}
	if allInside {
		return points
	}
	if allOutside {
		return nil
	}

	result := make([]Vec3f, 0, len(points)+1)
	count := len(points)
	for index := 0; index < count; index++ {
		current := points[index]
		currentDistance := distances[index]
		nextIndex := (index + 1) % count
		nextPoint := points[nextIndex]
		nextDistance := distances[nextIndex]
		if currentDistance <= epsilon {
			result = append(result, current)
		}
		if (currentDistance > epsilon && nextDistance < -epsilon) ||
			(currentDistance < -epsilon && nextDistance > epsilon) {
			fraction := currentDistance / (currentDistance - nextDistance)
			result = append(result, Vec3f{
				current[0] + fraction*(nextPoint[0]-current[0]),
				current[1] + fraction*(nextPoint[1]-current[1]),
				current[2] + fraction*(nextPoint[2]-current[2]),
			})
		}
	}
	return result
}

// WindingArea returns the area of a planar polygon.
func WindingArea(points []Vec3f) float64 {
	if len(points) < 3 {
		return 0.0
	}
	total := Vec3f{0.0, 0.0, 0.0}
	origin := points[0]
	for index := 1; index < len(points)-1; index++ {
		total = VecAdd(total, VecCross(VecSub(points[index], origin), VecSub(points[index+1], origin)))
	}
	return VecLength(total) * 0.5
}

// WindingNormal returns the Newell normal of a polygon, following its actual
// vertex order.
func WindingNormal(points []Vec3f) Vec3f {
	total := Vec3f{0.0, 0.0, 0.0}
	count := len(points)
	for index := 0; index < count; index++ {
		current := points[index]
		following := points[(index+1)%count]
		total = Vec3f{
			total[0] + (current[1]-following[1])*(current[2]+following[2]),
			total[1] + (current[2]-following[2])*(current[0]+following[0]),
			total[2] + (current[0]-following[0])*(current[1]+following[1]),
		}
	}
	return VecNormalize(total)
}

// WindingOrientation returns +1 when the vertex order runs counter-clockwise
// about normal, and -1 otherwise.
func WindingOrientation(points []Vec3f, normal Vec3f) float64 {
	if VecDot(WindingNormal(points), normal) >= 0.0 {
		return 1.0
	}
	return -1.0
}

// WindingCentroid returns the area-weighted centroid of a planar polygon.
func WindingCentroid(points []Vec3f) Vec3f {
	if len(points) == 0 {
		return Vec3f{0.0, 0.0, 0.0}
	}
	if len(points) < 3 {
		return meanPoint(points)
	}
	origin := points[0]
	totalArea := 0.0
	accumulator := Vec3f{0.0, 0.0, 0.0}
	for index := 1; index < len(points)-1; index++ {
		a := points[index]
		b := points[index+1]
		area := VecLength(VecCross(VecSub(a, origin), VecSub(b, origin))) * 0.5
		if area <= 0.0 {
			continue
		}
		centre := Vec3f{
			(origin[0] + a[0] + b[0]) / 3.0,
			(origin[1] + a[1] + b[1]) / 3.0,
			(origin[2] + a[2] + b[2]) / 3.0,
		}
		accumulator = VecAdd(accumulator, VecScale(centre, area))
		totalArea += area
	}
	if totalArea <= 0.0 {
		return meanPoint(points)
	}
	return VecScale(accumulator, 1.0/totalArea)
}

// meanPoint is the per-axis mean of points. Each axis sum is compensated, the
// way CPython's builtin sum() over floats is, rather than naive — see
// pysum for why that matters.
func meanPoint(points []Vec3f) Vec3f {
	count := float64(len(points))
	var axes [3][]float64
	for axis := range axes {
		axes[axis] = make([]float64, len(points))
	}
	for index, point := range points {
		for axis := 0; axis < 3; axis++ {
			axes[axis][index] = point[axis]
		}
	}
	return Vec3f{
		pysum.Sum(axes[0]) / count,
		pysum.Sum(axes[1]) / count,
		pysum.Sum(axes[2]) / count,
	}
}

// RemoveDuplicatePoints drops consecutive near-identical points, and closes the
// winding by dropping a final point equal to the first. Pass
// PointOnPlaneEpsilon for the default tolerance.
func RemoveDuplicatePoints(points []Vec3f, epsilon float64) []Vec3f {
	result := make([]Vec3f, 0, len(points))
	for _, point := range points {
		if len(result) > 0 && pointsEqual(result[len(result)-1], point, epsilon) {
			continue
		}
		result = append(result, point)
	}
	if len(result) > 1 && pointsEqual(result[0], result[len(result)-1], epsilon) {
		result = result[:len(result)-1]
	}
	return result
}

func pointsEqual(a, b Vec3f, epsilon float64) bool {
	return math.Abs(a[0]-b[0]) <= epsilon &&
		math.Abs(a[1]-b[1]) <= epsilon &&
		math.Abs(a[2]-b[2]) <= epsilon
}

// SnapPoint quantizes a reconstructed vertex so identical corners compare
// equal. Pass SnapGrid for the default grid.
func SnapPoint(point Vec3f, grid float64) Vec3f {
	// The "+ 0.0" normalizes -0.0 to 0.0: -0.0 + 0.0 is +0.0, so the addition
	// is kept rather than dropped as a no-op.
	return Vec3f{
		pyRound(point[0]/grid)*grid + 0.0,
		pyRound(point[1]/grid)*grid + 0.0,
		pyRound(point[2]/grid)*grid + 0.0,
	}
}

// ConvexPolyhedron is a bounded convex solid reconstructed from Quake brush
// planes.
//
// Always used through a pointer: the derived-data caches below are populated
// lazily, and are not part of the solid's identity or comparison.
type ConvexPolyhedron struct {
	Planes   []Plane
	Polygons [][]Vec3f
	MinXYZ   Vec3f
	MaxXYZ   Vec3f
	Volume   float64
	Centroid Vec3f

	// Derived data reused across collision queries; never part of identity.
	mu             sync.Mutex
	edges          [][2]Vec3f
	edgesDone      bool
	satVertices    []Vec3f
	satAxes        []Vec3f
	satDone        bool
	axisAlignedBox bool
	axisAlignedSet bool
}

// Extents returns the solid's axis-aligned bounding-box dimensions.
func (s *ConvexPolyhedron) Extents() Vec3f {
	return Vec3f{
		s.MaxXYZ[0] - s.MinXYZ[0],
		s.MaxXYZ[1] - s.MinXYZ[1],
		s.MaxXYZ[2] - s.MinXYZ[2],
	}
}

// ContainsPoint reports whether point lies inside every half-space, within
// epsilon.
func (s *ConvexPolyhedron) ContainsPoint(point Vec3f, epsilon float64) bool {
	for _, plane := range s.Planes {
		if plane.Evaluate(point) > epsilon {
			return false
		}
	}
	return true
}

// Edges returns the solid's unique undirected edges.
func (s *ConvexPolyhedron) Edges() [][2]Vec3f {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.edgesLocked()
}

func (s *ConvexPolyhedron) edgesLocked() [][2]Vec3f {
	if s.edgesDone {
		return s.edges
	}
	type edgeKey [2][3]int64
	seen := make(map[edgeKey]struct{})
	result := make([][2]Vec3f, 0)
	for _, polygon := range s.Polygons {
		count := len(polygon)
		for index := 0; index < count; index++ {
			a := polygon[index]
			b := polygon[(index+1)%count]
			keyA := quantize(a)
			keyB := quantize(b)
			var key edgeKey
			if lessInt3(keyA, keyB) || keyA == keyB {
				key = edgeKey{keyA, keyB}
			} else {
				key = edgeKey{keyB, keyA}
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, [2]Vec3f{a, b})
		}
	}
	s.edges = result
	s.edgesDone = true
	return result
}

func quantize(point Vec3f) [3]int64 {
	return [3]int64{
		int64(pyRound(point[0] * 100.0)),
		int64(pyRound(point[1] * 100.0)),
		int64(pyRound(point[2] * 100.0)),
	}
}

// lessInt3 compares two quantized points lexicographically, matching Python's
// tuple ordering.
func lessInt3(a, b [3]int64) bool {
	for index := 0; index < 3; index++ {
		if a[index] != b[index] {
			return a[index] < b[index]
		}
	}
	return false
}

// BuildPolyhedron reconstructs the bounded solid defined by planes.
//
// It returns the polyhedron and, for each input plane, the index of its polygon
// in Polygons, or -1 when the plane contributes no bounded face (a redundant
// plane, or a brush that is not bounded on that side).
func BuildPolyhedron(planes []Plane) (*ConvexPolyhedron, []int, error) {
	if len(planes) < 4 {
		return nil, nil, geometryError("a bounded convex solid needs at least four planes")
	}

	// Coincident planes must contribute one face, not one each: duplicates occur
	// in hand-built brushes and whenever two solids are intersected, and counting
	// a face twice inflates the volume.
	uniquePlanes := make([]Plane, 0, len(planes))
	representative := make([]int, 0, len(planes))
	seen := make(map[PlaneKey]int, len(planes))
	for _, plane := range planes {
		key := plane.Key()
		index, ok := seen[key]
		if !ok {
			index = len(uniquePlanes)
			seen[key] = index
			uniquePlanes = append(uniquePlanes, plane)
		}
		representative = append(representative, index)
	}

	polygons := make([][]Vec3f, 0, len(uniquePlanes))
	uniqueToPolygon := make([]int, 0, len(uniquePlanes))
	for index, plane := range uniquePlanes {
		points := BaseWinding(plane, BaseWindingExtent)
		for otherIndex, other := range uniquePlanes {
			if otherIndex == index {
				continue
			}
			if planesEqual(plane, other) {
				continue
			}
			points = ClipWinding(points, other, PlaneDistanceEpsilon)
			if len(points) < 3 {
				break
			}
		}
		snapped := make([]Vec3f, len(points))
		for pointIndex, point := range points {
			snapped[pointIndex] = SnapPoint(point, SnapGrid)
		}
		points = RemoveDuplicatePoints(snapped, PointOnPlaneEpsilon)
		if len(points) < 3 || WindingArea(points) <= AreaEpsilon {
			uniqueToPolygon = append(uniqueToPolygon, -1)
			continue
		}
		uniqueToPolygon = append(uniqueToPolygon, len(polygons))
		polygons = append(polygons, points)
	}

	planeToPolygon := make([]int, len(representative))
	for index, uniqueIndex := range representative {
		planeToPolygon[index] = uniqueToPolygon[uniqueIndex]
	}

	if len(polygons) < 4 {
		return nil, nil, geometryError("brush planes do not bound a closed solid")
	}

	first := true
	var minXYZ, maxXYZ Vec3f
	for _, polygon := range polygons {
		for _, point := range polygon {
			if first {
				minXYZ, maxXYZ = point, point
				first = false
				continue
			}
			for axis := 0; axis < 3; axis++ {
				if point[axis] < minXYZ[axis] {
					minXYZ[axis] = point[axis]
				}
				if point[axis] > maxXYZ[axis] {
					maxXYZ[axis] = point[axis]
				}
			}
		}
	}
	for axis := 0; axis < 3; axis++ {
		if maxXYZ[axis]-minXYZ[axis] >= BaseWindingExtent {
			return nil, nil, geometryError("brush planes describe an unbounded solid")
		}
	}

	keptPlanes := make([]Plane, 0, len(uniquePlanes))
	for index, polygonIndex := range uniqueToPolygon {
		if polygonIndex >= 0 {
			keptPlanes = append(keptPlanes, uniquePlanes[index])
		}
	}
	volume, centroid := volumeAndCentroid(keptPlanes, polygons, minXYZ)
	if volume <= AreaEpsilon {
		return nil, nil, geometryError("brush planes enclose no volume")
	}

	return &ConvexPolyhedron{
		Planes:   uniquePlanes,
		Polygons: polygons,
		MinXYZ:   minXYZ,
		MaxXYZ:   maxXYZ,
		Volume:   volume,
		Centroid: centroid,
	}, planeToPolygon, nil
}

func planesEqual(a, b Plane) bool {
	return math.Abs(a.Normal[0]-b.Normal[0]) < NormalEpsilon &&
		math.Abs(a.Normal[1]-b.Normal[1]) < NormalEpsilon &&
		math.Abs(a.Normal[2]-b.Normal[2]) < NormalEpsilon &&
		math.Abs(a.Distance-b.Distance) < PlaneDistanceEpsilon
}

// volumeAndCentroid computes the divergence-theorem volume plus the volumetric
// centroid.
func volumeAndCentroid(planes []Plane, polygons [][]Vec3f, reference Vec3f) (float64, Vec3f) {
	totalVolume := 0.0
	accumulator := Vec3f{0.0, 0.0, 0.0}

	// Planes are zipped with polygons, so the shorter sequence bounds the loop.
	count := len(planes)
	if len(polygons) < count {
		count = len(polygons)
	}
	for polygonIndex := 0; polygonIndex < count; polygonIndex++ {
		points := polygons[polygonIndex]
		origin := points[0]
		for index := 1; index < len(points)-1; index++ {
			a := points[index]
			b := points[index+1]
			tetra := VecDot(
				VecSub(origin, reference),
				VecCross(VecSub(a, reference), VecSub(b, reference)),
			) / 6.0
			centre := Vec3f{
				(reference[0] + origin[0] + a[0] + b[0]) / 4.0,
				(reference[1] + origin[1] + a[1] + b[1]) / 4.0,
				(reference[2] + origin[2] + a[2] + b[2]) / 4.0,
			}
			totalVolume += tetra
			accumulator = VecAdd(accumulator, VecScale(centre, tetra))
		}
	}
	if math.Abs(totalVolume) <= AreaEpsilon {
		return 0.0, reference
	}
	// The volume is returned unsigned but the centroid divides by the *signed*
	// total.
	return math.Abs(totalVolume), VecScale(accumulator, 1.0/totalVolume)
}

// BoundsIntersect is a broad-phase AABB overlap test.
func BoundsIntersect(aMin, aMax, bMin, bMax Vec3f, epsilon float64) bool {
	for axis := 0; axis < 3; axis++ {
		if aMax[axis]+epsilon < bMin[axis] {
			return false
		}
		if bMax[axis]+epsilon < aMin[axis] {
			return false
		}
	}
	return true
}

// BoxIntersectsPolyhedron is an exact axis-aligned-box versus convex-polyhedron
// overlap test (SAT).
//
// The separating-axis set is the box's three axes, the solid's face normals and
// every box-edge x solid-edge cross product. Using the solid's planes alone (or
// the solid's AABB) would report false collisions around wedges and diagonals.
func BoxIntersectsPolyhedron(boxMin, boxMax Vec3f, solid *ConvexPolyhedron, epsilon float64) bool {
	if !BoundsIntersect(boxMin, boxMax, solid.MinXYZ, solid.MaxXYZ, epsilon) {
		return false
	}

	// This uses the default tolerance, not the epsilon passed in.
	if PolyhedronIsAxisAlignedBox(solid, AxisAlignedBoxEpsilon) {
		// For a box the AABB overlap test is already exact.
		return true
	}

	vertices, axes := satData(solid)

	boxCentre := Vec3f{
		(boxMin[0] + boxMax[0]) * 0.5,
		(boxMin[1] + boxMax[1]) * 0.5,
		(boxMin[2] + boxMax[2]) * 0.5,
	}
	boxHalf := Vec3f{
		(boxMax[0] - boxMin[0]) * 0.5,
		(boxMax[1] - boxMin[1]) * 0.5,
		(boxMax[2] - boxMin[2]) * 0.5,
	}
	for _, axis := range axes {
		boxCentreProjection := VecDot(axis, boxCentre)
		boxRadius := math.Abs(axis[0])*boxHalf[0] +
			math.Abs(axis[1])*boxHalf[1] +
			math.Abs(axis[2])*boxHalf[2]
		boxLow := boxCentreProjection - boxRadius
		boxHigh := boxCentreProjection + boxRadius

		solidLow := VecDot(axis, vertices[0])
		solidHigh := solidLow
		for _, vertex := range vertices {
			projection := VecDot(axis, vertex)
			// if/else-if, so a projection below the running low is never also
			// considered for the high. With the seed taken from vertices[0] the
			// two forms agree.
			if projection < solidLow {
				solidLow = projection
			} else if projection > solidHigh {
				solidHigh = projection
			}
		}
		if boxHigh < solidLow-epsilon || solidHigh < boxLow-epsilon {
			return false
		}
	}
	return true
}

// satData returns cached separating-axis candidates for box-versus-solid tests.
func satData(solid *ConvexPolyhedron) ([]Vec3f, []Vec3f) {
	solid.mu.Lock()
	defer solid.mu.Unlock()
	if solid.satDone {
		return solid.satVertices, solid.satAxes
	}

	vertices := make([]Vec3f, 0)
	for _, polygon := range solid.Polygons {
		vertices = append(vertices, polygon...)
	}

	boxAxes := []Vec3f{{1.0, 0.0, 0.0}, {0.0, 1.0, 0.0}, {0.0, 0.0, 1.0}}
	axes := make([]Vec3f, 0, len(boxAxes)+len(solid.Planes))
	axes = append(axes, boxAxes...)
	seen := make(map[[3]int64]struct{}, len(boxAxes))
	for _, axis := range boxAxes {
		seen[axisKey(axis)] = struct{}{}
	}
	for _, plane := range solid.Planes {
		key := axisKey(plane.Normal)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		axes = append(axes, plane.Normal)
	}
	for _, edge := range solid.edgesLocked() {
		direction := VecSub(edge[1], edge[0])
		for _, boxAxis := range boxAxes {
			axis := VecCross(boxAxis, direction)
			length := VecLength(axis)
			if length < NormalEpsilon {
				continue
			}
			axis = Vec3f{axis[0] / length, axis[1] / length, axis[2] / length}
			key := axisKey(axis)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			axes = append(axes, axis)
		}
	}

	solid.satVertices = vertices
	solid.satAxes = axes
	solid.satDone = true
	return vertices, axes
}

// axisKey returns a direction-insensitive quantized axis identity.
func axisKey(axis Vec3f) [3]int64 {
	x, y, z := axis[0], axis[1], axis[2]
	// The tuples (x, y, z) < (-x, -y, -z) are compared lexicographically to pick
	// a canonical direction for the axis.
	if lessFloat3(x, y, z, -x, -y, -z) {
		x, y, z = -x, -y, -z
	}
	return [3]int64{
		int64(pyRound(x * 1e5)),
		int64(pyRound(y * 1e5)),
		int64(pyRound(z * 1e5)),
	}
}

func lessFloat3(ax, ay, az, bx, by, bz float64) bool {
	if ax != bx {
		return ax < bx
	}
	if ay != by {
		return ay < by
	}
	return az < bz
}

// RayHitsPolyhedron returns the entry distance of a ray into solid. The second
// result reports whether the ray hits at all.
func RayHitsPolyhedron(origin, direction Vec3f, solid *ConvexPolyhedron, maxDistance float64) (float64, bool) {
	enter := 0.0
	leave := maxDistance
	for _, plane := range solid.Planes {
		denominator := VecDot(plane.Normal, direction)
		numerator := plane.Distance - VecDot(plane.Normal, origin)
		if math.Abs(denominator) < NormalEpsilon {
			if numerator < 0.0 {
				return 0, false
			}
			continue
		}
		distance := numerator / denominator
		if denominator > 0.0 {
			if distance < leave {
				leave = distance
			}
		} else {
			if distance > enter {
				enter = distance
			}
		}
		if enter > leave {
			return 0, false
		}
	}
	if enter > maxDistance {
		return 0, false
	}
	return enter, true
}

// PolyhedronIsAxisAlignedBox reports whether the solid is an axis-aligned box,
// enabling fast voxel paths. Pass AxisAlignedBoxEpsilon for the default.
//
// The result is cached on first call and the cache is not keyed by epsilon, so
// a later call with a different tolerance returns the first call's answer.
func PolyhedronIsAxisAlignedBox(solid *ConvexPolyhedron, epsilon float64) bool {
	solid.mu.Lock()
	defer solid.mu.Unlock()
	if solid.axisAlignedSet {
		return solid.axisAlignedBox
	}

	result := true
	if len(solid.Polygons) != 6 {
		result = false
	} else {
		for _, plane := range solid.Planes {
			aligned := false
			for axis := 0; axis < 3; axis++ {
				if math.Abs(math.Abs(plane.Normal[axis])-1.0) <= epsilon {
					aligned = true
					break
				}
			}
			if !aligned {
				result = false
				break
			}
		}
	}

	solid.axisAlignedBox = result
	solid.axisAlignedSet = true
	return result
}

// PolygonPlaneOffset returns the largest absolute out-of-plane deviation of a
// reconstructed polygon.
func PolygonPlaneOffset(polygon []Vec3f, plane Plane) float64 {
	largest := 0.0
	for _, point := range polygon {
		if offset := math.Abs(plane.Evaluate(point)); offset > largest {
			largest = offset
		}
	}
	return largest
}

// ConvexPolygonIntersectionArea is the overlap area of two convex polygons that
// share a plane with the given normal.
//
// This is the lower-level seam behind every "how much of these two surfaces sit
// on top of one another" question in the repository. It was extracted from
// textureusage/relations, which still calls it, so that a second consumer never
// grows a second — and inevitably divergent — implementation of the same clip.
//
// B's polygon is clipped by the in-plane half-spaces of A's. The outward
// in-plane direction depends on the vertex order, so it is measured rather than
// assumed.
func ConvexPolygonIntersectionArea(aPolygon, bPolygon []Vec3f, normal Vec3f) float64 {
	if len(aPolygon) < 3 || len(bPolygon) < 3 {
		return 0.0
	}
	points := append([]Vec3f(nil), bPolygon...)
	orientation := WindingOrientation(aPolygon, normal)
	count := len(aPolygon)
	for index := 0; index < count; index++ {
		start := aPolygon[index]
		end := aPolygon[(index+1)%count]
		edge := VecSub(end, start)
		outward := VecScale(VecCross(edge, normal), orientation)
		length := VecLength(outward)
		if length < NormalEpsilon {
			continue
		}
		outward = Vec3f{outward[0] / length, outward[1] / length, outward[2] / length}
		distance := VecDot(outward, start)
		points = ClipWinding(points, Plane{Normal: outward, Distance: distance},
			PlaneDistanceEpsilon)
		if len(points) < 3 {
			return 0.0
		}
	}
	return WindingArea(points)
}

// SharedBoundaryLength is the total length over which two convex polygons in a
// common plane run along one another.
//
// Two treads that merely touch at a corner share a boundary of zero; two that
// form one composite tread share the full length of the edge between them. That
// distinction is what separates a real same-level component from an accidental
// point contact, so it is measured rather than inferred from an overlap test.
func SharedBoundaryLength(aPolygon, bPolygon []Vec3f, epsilon float64) float64 {
	total := 0.0
	for aIndex := 0; aIndex < len(aPolygon); aIndex++ {
		aStart := aPolygon[aIndex]
		aEnd := aPolygon[(aIndex+1)%len(aPolygon)]
		direction := VecSub(aEnd, aStart)
		length := VecLength(direction)
		if length < epsilon {
			continue
		}
		unit := VecScale(direction, 1.0/length)
		for bIndex := 0; bIndex < len(bPolygon); bIndex++ {
			bStart := bPolygon[bIndex]
			bEnd := bPolygon[(bIndex+1)%len(bPolygon)]
			if !pointOnLine(aStart, unit, bStart, epsilon) ||
				!pointOnLine(aStart, unit, bEnd, epsilon) {
				continue
			}
			low := math.Min(VecDot(VecSub(bStart, aStart), unit),
				VecDot(VecSub(bEnd, aStart), unit))
			high := math.Max(VecDot(VecSub(bStart, aStart), unit),
				VecDot(VecSub(bEnd, aStart), unit))
			overlap := math.Min(high, length) - math.Max(low, 0.0)
			if overlap > epsilon {
				total += overlap
			}
		}
	}
	return total
}

// pointOnLine reports whether point lies on the line through origin with the
// given unit direction, within epsilon.
func pointOnLine(origin, unit, point Vec3f, epsilon float64) bool {
	offset := VecSub(point, origin)
	along := VecScale(unit, VecDot(offset, unit))
	return VecLength(VecSub(offset, along)) <= epsilon
}
