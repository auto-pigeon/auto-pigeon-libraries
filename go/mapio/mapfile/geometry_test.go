package mapfile

import (
	"path/filepath"
	"strings"
	"testing"
)

// The canonical box face order is derived from the NetRadiant-authored
// one_cube.map fixture. That claim is checkable: generating a box over
// the cube fixture's own bounds must reproduce the fixture's face lines exactly,
// in order. No CLI command exposes MakeBoxBrush directly, so this fixture is
// the strongest external check on it.
func TestMakeBoxBrushReproducesTheNetradiantCubeFixture(t *testing.T) {
	document := mustParseFile(t, filepath.Join("testdata", "known_good", "one_cube.map"))
	fixture := document.Entity(0).Brushes()[0]

	bounds, err := BrushBounds(fixture)
	if err != nil {
		t.Fatalf("BrushBounds() error = %v", err)
	}

	generated, err := MakeBoxBrush(bounds.Min, bounds.Max, fixture.Face(0).Texture())
	if err != nil {
		t.Fatalf("MakeBoxBrush() error = %v", err)
	}

	if !generated.Equal(fixture) {
		var got, want []string
		for _, face := range generated.Faces() {
			got = append(got, face.Line())
		}
		for _, face := range fixture.Faces() {
			want = append(want, face.Line())
		}
		t.Errorf("generated box does not match the fixture\ngot:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The generator builds its default room with MakeRoomShell(256, 256, 128), and
// one_room.map is that generator's committed output. So the shell must
// reproduce the fixture's six worldspawn brushes exactly.
func TestMakeRoomShellReproducesTheOneRoomFixture(t *testing.T) {
	const (
		width  = 256
		depth  = 256
		height = 128
	)

	document := mustParseFile(t, filepath.Join("testdata", "one_room.map"))
	worldspawn := entityByClassname(t, document, "worldspawn")
	fixtureBrushes := worldspawn.Brushes()

	shell, err := MakeRoomShell(width, depth, height, DefaultTexture,
		DefaultWallThickness, DefaultFloorThickness, DefaultCeilingThickness)
	if err != nil {
		t.Fatalf("MakeRoomShell() error = %v", err)
	}
	generated := shell.Brushes()

	if len(generated) != len(fixtureBrushes) {
		t.Fatalf("generated %d brushes, fixture has %d", len(generated), len(fixtureBrushes))
	}
	names := []string{"floor", "ceiling", "west wall", "east wall", "south wall", "north wall"}
	for i := range generated {
		if !generated[i].Equal(fixtureBrushes[i]) {
			t.Errorf("%s brush differs from the fixture\ngot:\n%s\nwant:\n%s",
				names[i], faceLines(generated[i]), faceLines(fixtureBrushes[i]))
		}
	}
}

func faceLines(brush Brush) string {
	var lines []string
	for _, face := range brush.Faces() {
		lines = append(lines, face.Line())
	}
	return strings.Join(lines, "\n")
}

func TestMakeBoxBrushRejectsInvalidBounds(t *testing.T) {
	tests := []struct {
		name     string
		min, max Point
	}{
		{"zero width", Point{0, 0, 0}, Point{0, 64, 64}},
		{"inverted x", Point{64, 0, 0}, Point{0, 64, 64}},
		{"inverted z", Point{0, 0, 64}, Point{64, 64, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := MakeBoxBrush(tt.min, tt.max, DefaultTexture); err == nil {
				t.Error("MakeBoxBrush() accepted invalid bounds")
			}
		})
	}
}

// The box validator finds a face's points with an integers-only pattern over the
// rendered line, so a non-integral box is rejected rather than generated. The
// decision is recorded in .agents/HITL.md.
func TestMakeBoxBrushRejectsNonIntegralBounds(t *testing.T) {
	_, err := MakeBoxBrush(Point{0, 0, 0}, Point{64, 64, 0.5}, DefaultTexture)
	if err == nil {
		t.Fatal("MakeBoxBrush() accepted non-integral bounds; they must be rejected")
	}
	if !strings.Contains(err.Error(), "box face must contain three points") {
		t.Errorf("error = %q, want the non-integral rejection message", err.Error())
	}
}

func TestMakeRoomShellRejectsInvalidDimensions(t *testing.T) {
	tests := []struct {
		name                           string
		width, depth, height           int
		wall, floorThick, ceilingThick int
	}{
		{"zero width", 0, 256, 128, 16, 16, 16},
		{"negative height", 256, 256, -1, 16, 16, 16},
		{"zero wall thickness", 256, 256, 128, 0, 16, 16},
		{"negative ceiling thickness", 256, 256, 128, 16, 16, -4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MakeRoomShell(tt.width, tt.depth, tt.height, DefaultTexture,
				tt.wall, tt.floorThick, tt.ceilingThick)
			if err == nil {
				t.Error("MakeRoomShell() accepted invalid dimensions")
			}
		})
	}
}

func TestBrushBounds(t *testing.T) {
	brush, err := MakeBoxBrush(Point{-16, -16, -16}, Point{64, 128, 32}, DefaultTexture)
	if err != nil {
		t.Fatalf("MakeBoxBrush() error = %v", err)
	}
	bounds, err := BrushBounds(brush)
	if err != nil {
		t.Fatalf("BrushBounds() error = %v", err)
	}
	if bounds.Min != (Point{-16, -16, -16}) || bounds.Max != (Point{64, 128, 32}) {
		t.Errorf("BrushBounds() = %v..%v, want {-16 -16 -16}..{64 128 32}", bounds.Min, bounds.Max)
	}

	if _, err := BrushBounds(Brush{}); err == nil {
		t.Error("BrushBounds() accepted a brush with no faces")
	}
}
