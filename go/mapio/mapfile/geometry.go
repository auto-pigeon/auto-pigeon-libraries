package mapfile

import (
	"fmt"
	"math"
)

// Deterministic generation defaults.
const (
	DefaultTexture          = "dev/dev_gray"
	DefaultWallThickness    = 16
	DefaultFloorThickness   = 16
	DefaultCeilingThickness = 16
)

// Point is one corner in world space.
type Point [3]Number

// Bounds is an axis-aligned extent.
type Bounds struct {
	Min Point
	Max Point
}

// BoxFacePoints is the public view of the canonical axis-aligned box face order
// and winding.
//
// The order is z=max, y=max, x=max, z=min, y=min, x=min, derived from the
// NetRadiant-authored one_cube.map fixture. Everything in the project that has
// to emit an axis-aligned plane goes through this, so every generated plane
// shares one convention.
func BoxFacePoints(minXYZ, maxXYZ Point) [6][3]Point {
	x0, y0, z0 := minXYZ[0], minXYZ[1], minXYZ[2]
	x1, y1, z1 := maxXYZ[0], maxXYZ[1], maxXYZ[2]
	return [6][3]Point{
		{{x1, y1, z1}, {x1, y0, z1}, {x0, y1, z1}},
		{{x1, y1, z1}, {x0, y1, z1}, {x1, y1, z0}},
		{{x1, y1, z1}, {x1, y1, z0}, {x1, y0, z1}},
		{{x0, y0, z0}, {x1, y0, z0}, {x0, y1, z0}},
		{{x0, y0, z0}, {x0, y0, z1}, {x1, y0, z0}},
		{{x0, y0, z0}, {x0, y1, z0}, {x0, y0, z1}},
	}
}

// MakeBoxBrush creates a validated six-face convex axis-aligned box brush.
func MakeBoxBrush(minXYZ, maxXYZ Point, texture string) (Brush, error) {
	for i := 0; i < 3; i++ {
		if minXYZ[i] >= maxXYZ[i] {
			return Brush{}, fmt.Errorf("invalid box bounds: min=%v max=%v", minXYZ, maxXYZ)
		}
	}

	points := BoxFacePoints(minXYZ, maxXYZ)
	faces := make([]Face, 0, len(points))
	for _, triple := range points {
		faces = append(faces, NewClassicFace(
			NewVec3(triple[0][0], triple[0][1], triple[0][2]),
			NewVec3(triple[1][0], triple[1][1], triple[1][2]),
			NewVec3(triple[2][0], triple[2][1], triple[2][2]),
			texture,
			0, 0, 0, 1, 1,
			nil,
		))
	}

	brush := NewBrush(faces)
	if err := validateBoxBrush(brush, minXYZ, maxXYZ); err != nil {
		return Brush{}, err
	}
	return brush, nil
}

// RoomShell is the six structural brushes of a room.
type RoomShell struct {
	Floor     Brush
	Ceiling   Brush
	WestWall  Brush
	EastWall  Brush
	SouthWall Brush
	NorthWall Brush
}

// Brushes returns the shell in its canonical order: floor, ceiling, west, east,
// south, north.
func (s RoomShell) Brushes() []Brush {
	return []Brush{s.Floor, s.Ceiling, s.WestWall, s.EastWall, s.SouthWall, s.NorthWall}
}

// MakeRoomShell returns the floor, ceiling, west, east, south, and north
// structural room brushes, as a named struct so that callers index the walls by
// name instead of by position.
func MakeRoomShell(
	width, depth, height int,
	texture string,
	wallThickness, floorThickness, ceilingThickness int,
) (RoomShell, error) {
	if width <= 0 || depth <= 0 || height <= 0 {
		return RoomShell{}, fmt.Errorf("room dimensions must be positive")
	}
	if wallThickness <= 0 || floorThickness <= 0 || ceilingThickness <= 0 {
		return RoomShell{}, fmt.Errorf("room shell thickness values must be positive")
	}

	w := Number(width)
	d := Number(depth)
	h := Number(height)
	wall := Number(wallThickness)
	floorT := Number(floorThickness)
	ceilT := Number(ceilingThickness)

	boxes := []struct {
		min Point
		max Point
	}{
		{Point{-wall, -wall, -floorT}, Point{w + wall, d + wall, 0}},
		{Point{-wall, -wall, h}, Point{w + wall, d + wall, h + ceilT}},
		{Point{-wall, -wall, 0}, Point{0, d + wall, h}},
		{Point{w, -wall, 0}, Point{w + wall, d + wall, h}},
		{Point{0, -wall, 0}, Point{w, 0, h}},
		{Point{0, d, 0}, Point{w, d + wall, h}},
	}

	built := make([]Brush, 0, len(boxes))
	for _, box := range boxes {
		brush, err := MakeBoxBrush(box.min, box.max, texture)
		if err != nil {
			return RoomShell{}, err
		}
		built = append(built, brush)
	}

	return RoomShell{
		Floor:     built[0],
		Ceiling:   built[1],
		WestWall:  built[2],
		EastWall:  built[3],
		SouthWall: built[4],
		NorthWall: built[5],
	}, nil
}

// BrushBounds is the axis-aligned extent of a brush's face points.
func BrushBounds(brush Brush) (Bounds, error) {
	if len(brush.faces) == 0 {
		return Bounds{}, fmt.Errorf("brush has no parseable points")
	}

	var min, max Point
	first := true
	for _, face := range brush.faces {
		for _, point := range face.Points() {
			candidate := Point{point.x, point.y, point.z}
			if first {
				min, max = candidate, candidate
				first = false
				continue
			}
			for i := 0; i < 3; i++ {
				if candidate[i] < min[i] {
					min[i] = candidate[i]
				}
				if candidate[i] > max[i] {
					max[i] = candidate[i]
				}
			}
		}
	}
	return Bounds{Min: min, Max: max}, nil
}

func validateBoxBrush(brush Brush, minXYZ, maxXYZ Point) error {
	if len(brush.faces) != 6 {
		return fmt.Errorf("box brush must have exactly six faces")
	}

	bounds, err := BrushBounds(brush)
	if err != nil {
		return err
	}
	if bounds.Min != minXYZ || bounds.Max != maxXYZ {
		return fmt.Errorf("box brush bounds mismatch: expected %v..%v, got %v..%v",
			minXYZ, maxXYZ, bounds.Min, bounds.Max)
	}

	type plane struct {
		axis  string
		value Number
	}
	expected := map[plane]bool{
		{"x", minXYZ[0]}: true, {"x", maxXYZ[0]}: true,
		{"y", minXYZ[1]}: true, {"y", maxXYZ[1]}: true,
		{"z", minXYZ[2]}: true, {"z", maxXYZ[2]}: true,
	}
	actual := map[plane]bool{}

	for _, face := range brush.faces {
		points := face.Points()
		// Box brushes are integer-only by design, not by accident.
		//
		// A box face's points are found with an integers-only pattern over the
		// rendered face line, so a box with any non-integral coordinate fails
		// with "box face must contain three points". Human decision,
		// 2026-08-01: the constraint is kept because it is *right* for the
		// target.
		// Original Quake 1 tooling accepted only integer brush-plane
		// coordinates, and real shipped id1 content agrees — e3m5.map and
		// e3m6.map contain zero non-integer brush-plane coordinates between
		// them. Modern compiler forks are more permissive, but off-grid brushes
		// still cause real problems (T-junction and degenerate-vertex healing).
		//
		// So this is a deliberate Quake-precision safeguard on *generated*
		// geometry. It is not a parser restriction: the parser accepts decimal
		// coordinates and must keep doing so, because real maps in the wild
		// contain them. See apmap for the APMap boundary — APMap 1.0
		// models coordinates as unconstrained finite floats, and the collapse to
		// integers belongs in the future APMap-to-.map lowering step, not here.
		//
		// The constraint is checked directly against the points rather than by
		// re-parsing rendered text — same rejection, same message, no regex.
		for _, point := range points {
			for _, coordinate := range []Number{point.x, point.y, point.z} {
				if coordinate != math.Trunc(coordinate) {
					return fmt.Errorf("box face must contain three points: %s", face.Line())
				}
			}
		}

		found := false
		for index, axis := range []string{"x", "y", "z"} {
			values := map[Number]bool{}
			for _, point := range points {
				values[[3]Number{point.x, point.y, point.z}[index]] = true
			}
			if len(values) != 1 {
				continue
			}
			var value Number
			for v := range values {
				value = v
			}
			if value == minXYZ[index] || value == maxXYZ[index] {
				actual[plane{axis, value}] = true
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("face is not on an expected axis-aligned box plane: %v", points)
		}
	}

	if len(actual) != len(expected) {
		return fmt.Errorf("box brush planes mismatch: expected %v, got %v", expected, actual)
	}
	for key := range actual {
		if !expected[key] {
			return fmt.Errorf("box brush planes mismatch: expected %v, got %v", expected, actual)
		}
	}
	return nil
}
