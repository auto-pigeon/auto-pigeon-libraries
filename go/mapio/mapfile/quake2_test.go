package mapfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Multi-dialect coverage: the classic and Valve 220 face dialects and the
// Quake 2 face tail.
//
// Every dialect difference the parser knows about lives in the parser itself:
// the classic vs Valve 220 branch on a leading '[', and the Quake 2 face tail.
// Game profiles (hull sizes, step height, vocabularies) are an analyser
// concern and play no part in parsing or writing.
//
// The diagnostics side of Quake 2 support (inspect lines and compatibility
// statistics) lives in internal/diagnostics/quake2_test.go, because
// internal/diagnostics imports mapfile and not the other way round.

// quake2Fixture resolves one of the AUE-authored Quake 2 fixtures under
// testdata/q2/: valve220_door.map exercises Valve 220 axes with the tail, and
// classic_areaportal.map the classic dialect with the tail.
func quake2Fixture(name string) string {
	return filepath.Join("testdata", "q2", name)
}

func TestParseQuake2ClassicFixture(t *testing.T) {
	document := mustParseFile(t, quake2Fixture("classic_areaportal.map"))

	if got := mustGet(t, document.Entity(0), "classname"); got != "worldspawn" {
		t.Errorf("classname = %q, want worldspawn", got)
	}
	if document.BrushCount() == 0 {
		t.Error("BrushCount() = 0, want a positive count")
	}
	faces := allFaces(document)
	if len(faces) == 0 {
		t.Fatal("fixture has no faces")
	}
	for i, face := range faces {
		if face.TextureMode() != ModeClassic {
			t.Errorf("face %d dialect = %q, want classic", i, face.TextureMode())
		}
	}
}

func TestParseQuake2Valve220Fixture(t *testing.T) {
	document := mustParseFile(t, quake2Fixture("valve220_door.map"))

	if got := mustGet(t, document.Entity(0), "classname"); got != "worldspawn" {
		t.Errorf("classname = %q, want worldspawn", got)
	}
	if document.BrushCount() == 0 {
		t.Error("BrushCount() = 0, want a positive count")
	}

	var valveFaces []Face
	for _, face := range allFaces(document) {
		if face.TextureMode() == ModeValve220 {
			valveFaces = append(valveFaces, face)
		}
	}
	if len(valveFaces) == 0 {
		t.Fatal("fixture has no Valve 220 faces")
	}

	face := valveFaces[0]
	if got := face.Texture(); got != "e1u1/c_met11_2" {
		t.Errorf("texture = %q, want e1u1/c_met11_2", got)
	}
	uAxis, ok := face.UAxis()
	if !ok {
		t.Fatal("Valve 220 face has no U axis")
	}
	vAxis, ok := face.VAxis()
	if !ok {
		t.Fatal("Valve 220 face has no V axis")
	}
	if uAxis != NewTextureAxis(0, -1, 0, 0) {
		t.Errorf("u axis = %v, want (0 -1 0 0)", uAxis)
	}
	if vAxis != NewTextureAxis(0, 0, -1, 0) {
		t.Errorf("v axis = %v, want (0 0 -1 0)", vAxis)
	}
	if face.Rotation() != 0 || face.XScale() != 1 || face.YScale() != 1 {
		t.Errorf("rotation/scale = %v/%v/%v, want 0/1/1",
			face.Rotation(), face.XScale(), face.YScale())
	}
}

func TestValve220MapversionIsPreserved(t *testing.T) {
	document := mustParseFile(t, quake2Fixture("valve220_door.map"))

	if got := mustGet(t, document.Entity(0), "mapversion"); got != "220" {
		t.Errorf("mapversion = %q, want 220", got)
	}
}

func TestWriteValve220RoundTripPreservesAxes(t *testing.T) {
	fixture := quake2Fixture("valve220_door.map")
	original := mustParseFile(t, fixture)
	out := filepath.Join(t.TempDir(), "valve220_door_rt.map")

	if err := WriteFile(original, out); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	parsed := mustParseFile(t, out)

	if parsed.EntityCount() != original.EntityCount() {
		t.Errorf("entity count = %d, want %d", parsed.EntityCount(), original.EntityCount())
	}
	if parsed.BrushCount() != original.BrushCount() {
		t.Errorf("brush count = %d, want %d", parsed.BrushCount(), original.BrushCount())
	}
	if len(allFaces(parsed)) != len(allFaces(original)) {
		t.Errorf("face count = %d, want %d", len(allFaces(parsed)), len(allFaces(original)))
	}

	hasValveAxis := false
	for _, face := range allFaces(parsed) {
		if _, ok := face.UAxis(); ok && face.TextureMode() == ModeValve220 {
			hasValveAxis = true
			break
		}
	}
	if !hasValveAxis {
		t.Error("round trip lost every Valve 220 texture axis")
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "[ 0 -1 0 0 ] [ 0 0 -1 0 ]") {
		t.Error("written file does not contain the expected axis spelling")
	}

	// The whole document must be equivalent, not just the counts.
	if !parsed.Equal(original) {
		t.Error("round trip changed the model")
	}
}

// ---------------------------------------------------------------------------
// Prompt 20: verifying and completing the Quake 2 dialect.
//
// The three requirements were already met — the field
// exists, the parser fills it and the writer emits it — so what follows closes
// the coverage gaps that verification exposed rather than adding behaviour.

// TestClassicQuake2FixtureRoundTrips is the classic half of the round-trip
// requirement. valve220_door.map covers Valve 220 above; classic_areaportal.map
// is the classic-dialect Quake 2 fixture.
func TestClassicQuake2FixtureRoundTrips(t *testing.T) {
	fixture := quake2Fixture("classic_areaportal.map")
	original := mustParseFile(t, fixture)
	out := filepath.Join(t.TempDir(), "classic_areaportal_rt.map")

	if err := WriteFile(original, out); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	parsed := mustParseFile(t, out)
	if !parsed.Equal(original) {
		t.Error("round trip changed the model")
	}
	// Writing twice must be a fixed point, which is what makes the writer safe
	// to run over a corpus repeatedly.
	if mustWrite(t, parsed) != mustWrite(t, original) {
		t.Error("a second write produced different bytes")
	}
}

// TestQuake1FacesEmitNoTail is the negative half of the writer requirement: a
// Quake 1 face must not gain a trailing field just because the model can carry
// one. The byte-for-byte round-trip tests already imply this; asserting it
// directly names the property, so a regression says what broke.
func TestQuake1FacesEmitNoTail(t *testing.T) {
	for _, fixture := range roundTripFixtures {
		if strings.Contains(fixture, "q2") {
			continue // the Quake 2 fixtures legitimately carry a tail
		}
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			document := mustParseFile(t, fixture)
			faces := allFaces(document)
			if len(faces) == 0 {
				t.Fatal("fixture has no faces")
			}
			for i, face := range faces {
				if tail := face.ExtraValues(); len(tail) != 0 {
					t.Errorf("face %d parsed a tail from a Quake 1 fixture: %v", i, tail)
				}
			}
			// And the emitted text carries nothing after the scale pair.
			for _, line := range strings.Split(mustWrite(t, document), "\n") {
				trimmed := strings.TrimSpace(line)
				if !strings.HasPrefix(trimmed, "(") {
					continue
				}
				if fields := strings.Fields(trimmed); len(fields) != 21 {
					t.Errorf("Quake 1 face emitted %d tokens, want 21: %s", len(fields), trimmed)
				}
			}
		})
	}
}

// TestFaceTailLengthIsUnconstrained pins how permissive the tail is.
//
// The parser accepts *any* count of trailing numbers after the scale pair — the
// tail loop has no arity check. Real Quake 2 content only ever carries 0 or 3,
// so the difference is unreachable from a real map; it is pinned here so the
// permissiveness is a recorded decision rather than an accident.
func TestFaceTailLengthIsUnconstrained(t *testing.T) {
	for _, testCase := range []struct {
		name string
		tail string
		want []Number
	}{
		{"absent", "", nil},
		{"the three Quake 2 fields", " 1 2 3", []Number{1, 2, 3}},
		{"a single trailing value", " 7", []Number{7}},
		{"more than three", " 1 2 3 4", []Number{1, 2, 3, 4}},
		{"negative and decimal", " -1 2.5 3", []Number{-1, 2.5, 3}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			document := mustParse(t, `{
"classname" "worldspawn"
{
( 0 0 0 ) ( 64 0 0 ) ( 64 64 0 ) e1u1/brtrim1_5 0 0 0 1 1`+testCase.tail+`
}
}
`)
			face := document.Entity(0).Brushes()[0].Face(0)
			assertNumbers(t, "extra values", face.ExtraValues(), testCase.want)
			// Whatever was parsed must come back out unchanged.
			roundTripped := mustParse(t, mustWrite(t, document))
			if !roundTripped.Equal(document) {
				t.Error("the tail did not survive a round trip")
			}
		})
	}
}
