package legality

import (
	"context"
	"math"
	"reflect"
	"testing"
)

// Values measured from V8 (Node 24): Math.hypot(...v). The last two differ from the plain
// sqrt(x²+y²+z²), which is exactly why Hypot is not that.
func TestHypotIsV8s(t *testing.T) {
	for _, test := range []struct {
		in   []float64
		want float64
	}{
		{[]float64{3, 4, 0}, 5},
		{[]float64{1e-300, 1e-300, 0}, 1.4142135623730952e-300},
		{[]float64{0.1, 0.2, 0.3}, 0.37416573867739417},
		{[]float64{5e-324, 0, 0}, 5e-324},
		{[]float64{0.7071067811865476, 0.7071067811865476, 0}, 1.0000000000000002},
		{[]float64{123456.789, 9.87654321, 0.001}, 123456.78939506173},
		{[]float64{}, 0},
		{[]float64{math.Inf(-1), math.NaN()}, math.Inf(1)},
	} {
		if got := Hypot(test.in...); got != test.want {
			t.Errorf("Hypot(%v) = %v, want %v", test.in, got, test.want)
		}
	}
	if !math.IsNaN(Hypot(1, math.NaN())) {
		t.Error("NaN without an infinity is NaN")
	}
}

func box(x0, y0, z0, x1, y1, z1 float64) []Face {
	quad := func(a, b, c vec3) Face {
		return Face{Points: [3]vec3{a, b, c}, Texture: "t",
			Projection: Projection{Mode: ModeClassic, Scale: [2]float64{1, 1}}}
	}
	return []Face{
		quad(vec3{x0, y1, z1}, vec3{x1, y1, z1}, vec3{x1, y0, z1}),
		quad(vec3{x0, y0, z0}, vec3{x1, y0, z0}, vec3{x1, y1, z0}),
		quad(vec3{x1, y0, z1}, vec3{x1, y1, z1}, vec3{x1, y1, z0}),
		quad(vec3{x0, y1, z1}, vec3{x0, y0, z1}, vec3{x0, y0, z0}),
		quad(vec3{x1, y1, z1}, vec3{x0, y1, z1}, vec3{x0, y1, z0}),
		quad(vec3{x0, y0, z1}, vec3{x1, y0, z1}, vec3{x1, y0, z0}),
	}
}

func check(t *testing.T, faces []Face) Result {
	t.Helper()
	result, err := Check(context.Background(), faces)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCheckNamesWhatItFinds(t *testing.T) {
	sound := check(t, box(0, 0, 0, 64, 64, 16))
	if !sound.OK || sound.Winding != "outward" || !sound.Convexity.OK || sound.Convexity.OrientationSign != 1 {
		t.Fatalf("a box: %+v", sound)
	}

	inward := box(0, 0, 0, 64, 64, 16)
	for index := range inward {
		inward[index].Points[0], inward[index].Points[2] = inward[index].Points[2], inward[index].Points[0]
	}
	if got := check(t, inward).Codes; !reflect.DeepEqual(got, []string{CodeWindingInward}) {
		t.Errorf("an inside-out box: %v", got)
	}

	idle := append(box(0, 0, 0, 64, 64, 64), Face{Points: [3]vec3{{0, 64, 96}, {64, 64, 96}, {64, 0, 96}}, Texture: "t",
		Projection: Projection{Mode: ModeClassic, Scale: [2]float64{1, 1}}})
	result := check(t, idle)
	if result.Convexity.Reason != ReasonNotConvex || !reflect.DeepEqual(result.Convexity.Faces, []int{6}) {
		t.Errorf("a box with a plane above it: %+v", result.Convexity)
	}

	thin := check(t, box(0, 0, 0, 64, 64, 0.5))
	if !thin.OK || len(thin.Findings) != 1 || thin.Findings[0].Code != CodeThin || thin.Findings[0].Illegal {
		t.Errorf("a half-unit slab is a thin RISK: %+v", thin.Findings)
	}

	textures := box(0, 0, 0, 64, 64, 16)
	textures[0].Texture = "two words"
	textures[1].Projection.Scale = [2]float64{0, 1}
	textures[2].Projection = Projection{Mode: ModeValve220, U: [4]float64{1, 0, 0, 0}, V: [4]float64{1, 0, 0, 0}, Scale: [2]float64{1, 1}}
	textures[3].Projection = Projection{Mode: ModeBrushPrimitives, Matrix: [2][3]float64{{1, 2, 0}, {2, 4, 0}}}
	codes := []string{}
	for _, finding := range check(t, textures).Findings {
		codes = append(codes, finding.Code)
	}
	if want := []string{CodeTextureUnnamed, CodeTextureScaleDegenerate, CodeTextureBasisDegenerate}; !reflect.DeepEqual(codes, want) {
		t.Errorf("texture findings %v, want %v", codes, want)
	}

	if got := check(t, box(0, 0, 0, 64, 64, 16)[:3]).Codes; !reflect.DeepEqual(got, []string{CodeUnserializable}) {
		t.Errorf("three faces cannot be read as a brush: %v", got)
	}
	nonFinite := box(0, 0, 0, 64, 64, 16)
	nonFinite[0].Points[0][0] = math.Inf(1)
	if got := check(t, nonFinite).Codes; len(got) == 0 || got[0] != CodeNonFinite {
		t.Errorf("an infinite coordinate: %v", got)
	}
}

func TestCheckStopsOnItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Check(ctx, box(0, 0, 0, 64, 64, 16)); err == nil {
		t.Error("a cancelled check returned an answer")
	}
}
