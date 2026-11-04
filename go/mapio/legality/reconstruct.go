package legality

import (
	"context"
	"math"
)

// WindingEpsilon is the clip tolerance and the distance under which two corners are one.
const WindingEpsilon = .01

// BaseWindingSize is the half-width of the square each plane starts as before it is clipped.
const BaseWindingSize = 65536

type plane struct {
	n vec3
	d float64
}

// polygonFace is one face of a reconstruction: its corners, or none for a plane that bounds nothing.
type polygonFace struct {
	index      int
	polygon    []vec3
	renderable bool
}

type reconstruction struct {
	faces           []polygonFace
	orientationSign int
}

func normalise(value vec3) vec3 {
	length := hypot3(value)
	if length == 0 {
		return vec3{}
	}
	return vec3{value[0] / length, value[1] / length, value[2] / length}
}

func clip(polygon []vec3, p plane, sign float64) []vec3 {
	output := make([]vec3, 0, len(polygon)+1)
	for index := range polygon {
		a, b := polygon[index], polygon[(index+1)%len(polygon)]
		da := float64(sign * float64(dot(p.n, a)-p.d))
		db := float64(sign * float64(dot(p.n, b)-p.d))
		insideA, insideB := da >= -WindingEpsilon, db >= -WindingEpsilon
		if insideA {
			output = append(output, a)
		}
		if insideA != insideB {
			ratio := da / float64(da-db)
			output = append(output, vec3{
				float64(a[0] + mul(float64(b[0]-a[0]), ratio)),
				float64(a[1] + mul(float64(b[1]-a[1]), ratio)),
				float64(a[2] + mul(float64(b[2]-a[2]), ratio)),
			})
		}
	}
	return output
}

// unique keeps a corner unless an EARLIER corner of the input — kept or not — is within the epsilon:
// the reference's `points.slice(0, index).some(…)`, exactly.
func unique(points []vec3) []vec3 {
	kept := make([]vec3, 0, len(points))
	for index, point := range points {
		duplicate := false
		for _, other := range points[:index] {
			if hypot3(sub(point, other)) < WindingEpsilon {
				duplicate = true
				break
			}
		}
		if !duplicate {
			kept = append(kept, point)
		}
	}
	return kept
}

type attemptResult struct {
	sign       int
	faces      []polygonFace
	renderable int
	extent     float64
}

func attempt(ctx context.Context, planes []plane, sign int) (attemptResult, error) {
	faces := make([]polygonFace, len(planes))
	for index, p := range planes {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return attemptResult{}, err
			}
		}
		helper := vec3{0, 1, 0}
		if math.Abs(p.n[2]) < .9 {
			helper = vec3{0, 0, 1}
		}
		axisU := normalise(cross(helper, p.n))
		axisV := normalise(cross(p.n, axisU))
		origin := vec3{mul(p.n[0], p.d), mul(p.n[1], p.d), mul(p.n[2], p.d)}
		corners := [4][2]float64{{-BaseWindingSize, -BaseWindingSize}, {BaseWindingSize, -BaseWindingSize},
			{BaseWindingSize, BaseWindingSize}, {-BaseWindingSize, BaseWindingSize}}
		polygon := make([]vec3, 0, 8)
		for _, corner := range corners {
			u, v := corner[0], corner[1]
			var point vec3
			for axis := 0; axis < 3; axis++ {
				point[axis] = float64(float64(origin[axis]+mul(axisU[axis], u)) + mul(axisV[axis], v))
			}
			polygon = append(polygon, point)
		}
		for other := 0; other < len(planes) && len(polygon) > 0; other++ {
			if other != index {
				polygon = clip(polygon, planes[other], float64(sign))
			}
		}
		polygon = unique(polygon)
		if len(polygon) < 3 {
			faces[index] = polygonFace{index: index}
			continue
		}
		faces[index] = polygonFace{index: index, polygon: polygon, renderable: true}
	}
	// `points.length ? Math.max(...points.flat().map(Math.abs)) : Infinity` — NaN wins Math.max.
	result := attemptResult{sign: sign, faces: faces, extent: math.Inf(1)}
	largest, seen, nan := math.Inf(-1), false, false
	for _, face := range faces {
		if face.renderable {
			result.renderable++
		}
		for _, point := range face.polygon {
			seen = true
			for _, value := range point {
				if math.IsNaN(value) {
					nan = true
				} else if math.Abs(value) > largest {
					largest = math.Abs(value)
				}
			}
		}
	}
	if seen {
		result.extent = largest
		if nan {
			result.extent = math.NaN()
		}
	}
	return result, nil
}

// reconstruct is the reference's `reconstructBrush`: both orientations of the declared planes are
// clipped into polygons, and the one with more renderable faces — then the smaller extent, then
// the negative sign — is the brush.
func reconstruct(ctx context.Context, faces []Face) (reconstruction, error) {
	planes := make([]plane, len(faces))
	for index, face := range faces {
		n := normalise(cross(sub(face.Points[1], face.Points[0]), sub(face.Points[2], face.Points[0])))
		planes[index] = plane{n: n, d: dot(n, face.Points[0])}
	}
	negative, err := attempt(ctx, planes, -1)
	if err != nil {
		return reconstruction{}, err
	}
	positive, err := attempt(ctx, planes, 1)
	if err != nil {
		return reconstruction{}, err
	}
	// V8 sorts [negative, positive] with one comparison, comparator(positive, negative); NaN (two
	// infinite extents) falls through to the sign, which keeps negative first.
	chosen := negative
	if positive.renderable > negative.renderable ||
		(positive.renderable == negative.renderable && positive.extent < negative.extent) {
		chosen = positive
	}
	return reconstruction{faces: chosen.faces, orientationSign: chosen.sign}, nil
}

func enclosedVolume(faces []polygonFace) float64 {
	volume := 0.0
	for _, face := range faces {
		for index := 1; index+1 < len(face.polygon); index++ {
			volume = float64(volume + dot(face.polygon[0], cross(face.polygon[index], face.polygon[index+1]))/6)
		}
	}
	return math.Abs(volume)
}
