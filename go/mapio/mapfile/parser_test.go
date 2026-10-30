package mapfile

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// sampleMap is the shared parser/writer sample, including the backslash in
// "_path", which is what proves escapes survive verbatim.
const sampleMap = `{
"classname" "worldspawn"
"wad" "example.wad"
{
( 0 0 0 ) ( 128 0 0 ) ( 128 128 0 ) __TB_empty 0 0 0 1 1
( 0 0 16 ) ( 128 128 16 ) ( 128 0 16 ) __TB_empty 0 0 0 1 1
}
}
{
"classname" "light"
"origin" "64 64 64"
"targetname" "light_a"
"_unknown" "preserved"
"_path" "textures\base"
}
`

// mustParse fails the test rather than returning an error, for the many cases
// where parsing is setup and not the thing under test.
func mustParse(t *testing.T, text string) *MapFile {
	t.Helper()
	document, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return document
}

func mustParseFile(t *testing.T, path string) *MapFile {
	t.Helper()
	document, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile(%q) error = %v", path, err)
	}
	return document
}

func mustGet(t *testing.T, entity Entity, key string) string {
	t.Helper()
	value, ok := entity.Get(key)
	if !ok {
		t.Fatalf("entity has no key %q", key)
	}
	return value
}

// entityByClassname returns the first entity with the given classname.
func entityByClassname(t *testing.T, document *MapFile, classname string) Entity {
	t.Helper()
	for _, entity := range document.Entities() {
		if value, ok := entity.Get("classname"); ok && value == classname {
			return entity
		}
	}
	t.Fatalf("missing entity: %s", classname)
	return Entity{}
}

func allFaces(document *MapFile) []Face {
	var faces []Face
	for _, entity := range document.Entities() {
		for _, brush := range entity.Brushes() {
			faces = append(faces, brush.Faces()...)
		}
	}
	return faces
}

func textureSet(faces []Face) map[string]int {
	counts := map[string]int{}
	for _, face := range faces {
		counts[face.Texture()]++
	}
	return counts
}

func TestParseEntitiesAndBrushes(t *testing.T) {
	document := mustParse(t, sampleMap)

	if got := document.EntityCount(); got != 2 {
		t.Errorf("EntityCount() = %d, want 2", got)
	}
	if got := document.BrushCount(); got != 1 {
		t.Errorf("BrushCount() = %d, want 1", got)
	}
	if got := mustGet(t, document.Entity(0), "classname"); got != "worldspawn" {
		t.Errorf("entity 0 classname = %q, want worldspawn", got)
	}
	if got := mustGet(t, document.Entity(1), "_unknown"); got != "preserved" {
		t.Errorf("entity 1 _unknown = %q, want preserved", got)
	}
	if got := mustGet(t, document.Entity(1), `_path`); got != `textures\base` {
		t.Errorf("entity 1 _path = %q, want %q", got, `textures\base`)
	}
}

func TestParseNetradiantCubeFixture(t *testing.T) {
	document := mustParseFile(t, filepath.Join("testdata", "known_good", "one_cube.map"))

	if got := document.EntityCount(); got != 1 {
		t.Fatalf("EntityCount() = %d, want 1", got)
	}
	worldspawn := document.Entity(0)
	if got := mustGet(t, worldspawn, "classname"); got != "worldspawn" {
		t.Errorf("classname = %q, want worldspawn", got)
	}
	if got := len(worldspawn.Brushes()); got != 1 {
		t.Fatalf("brush count = %d, want 1", got)
	}
	if got := worldspawn.Brushes()[0].FaceCount(); got != 6 {
		t.Errorf("face count = %d, want 6", got)
	}
	textures := textureSet(worldspawn.Brushes()[0].Faces())
	if len(textures) != 1 || textures["dev/dev_gray"] != 6 {
		t.Errorf("textures = %v, want only dev/dev_gray", textures)
	}
}

func TestParseGeneratedFixtures(t *testing.T) {
	tests := []struct {
		fixture     string
		entityCount int
		brushCount  int
		faceCount   int
		keyValues   map[string]map[string]string
	}{
		{
			fixture: "one_room.map", entityCount: 3, brushCount: 6, faceCount: 36,
			keyValues: map[string]map[string]string{
				"worldspawn":        {"classname": "worldspawn"},
				"info_player_start": {"origin": "64 64 24"},
			},
		},
		{
			fixture: "door_trigger.map", entityCount: 3, brushCount: 8, faceCount: 48,
			keyValues: map[string]map[string]string{
				"func_door":    {"targetname": "door_a", "angle": "90"},
				"trigger_once": {"target": "door_a"},
			},
		},
		{
			fixture: "train_path.map", entityCount: 4, brushCount: 7, faceCount: 42,
			keyValues: map[string]map[string]string{
				"func_train":  {"targetname": "train_a", "target": "path_a"},
				"path_corner": {"origin": "64 128 32"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			document := mustParseFile(t, filepath.Join("testdata", tt.fixture))

			if got := document.EntityCount(); got != tt.entityCount {
				t.Errorf("EntityCount() = %d, want %d", got, tt.entityCount)
			}
			if got := document.BrushCount(); got != tt.brushCount {
				t.Errorf("BrushCount() = %d, want %d", got, tt.brushCount)
			}
			faces := allFaces(document)
			if len(faces) != tt.faceCount {
				t.Errorf("face count = %d, want %d", len(faces), tt.faceCount)
			}
			textures := textureSet(faces)
			if len(textures) != 1 || textures["dev/dev_gray"] != tt.faceCount {
				t.Errorf("textures = %v, want only dev/dev_gray", textures)
			}
			for classname, expected := range tt.keyValues {
				entity := entityByClassname(t, document, classname)
				for key, want := range expected {
					if got := mustGet(t, entity, key); got != want {
						t.Errorf("%s[%q] = %q, want %q", classname, key, got, want)
					}
				}
			}
		})
	}
}

func TestCommentsDoNotBreakParsing(t *testing.T) {
	document := mustParse(t, `// leading comment
{
"classname" "worldspawn" // key/value comment
{
( 0 0 0 ) ( 64 0 0 ) ( 64 64 0 ) dev/dev_gray 0 0 0 1 1 // face comment
}
} // trailing comment
`)

	if got := document.EntityCount(); got != 1 {
		t.Fatalf("EntityCount() = %d, want 1", got)
	}
	if got := document.BrushCount(); got != 1 {
		t.Errorf("BrushCount() = %d, want 1", got)
	}
	if got := document.Entity(0).Brushes()[0].Face(0).Texture(); got != "dev/dev_gray" {
		t.Errorf("texture = %q, want dev/dev_gray", got)
	}
}

// Comments are tolerated but not retained: the tokenizer discards them and the
// writer has no way to emit one, so a round trip drops them. Pinned as a test so
// that "comment handling" is a stated property rather than an assumption.
func TestCommentsAreNotPreservedThroughARoundTrip(t *testing.T) {
	document := mustParse(t, `{
"classname" "worldspawn" // dropped on write
}
`)

	text, err := Write(document)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if strings.Contains(text, "//") {
		t.Errorf("Write() kept a comment; comments must be dropped:\n%s", text)
	}
}

func TestParseSkipsCompleteEmptyBrushBlocksWithDiagnostics(t *testing.T) {
	source := `{
"classname" "worldspawn"
{
}
"message" "parsing continues"
{}
{
( 0 0 0 ) ( 64 0 0 ) ( 64 64 0 ) dev/dev_gray 0 0 0 1 1
}
}
`
	document, err := ParseNamed(source, "worldcraft.map")
	if err != nil {
		t.Fatalf("ParseNamed() error = %v", err)
	}
	if got := document.BrushCount(); got != 1 {
		t.Errorf("BrushCount() = %d, want 1", got)
	}
	if got := mustGet(t, document.Entity(0), "message"); got != "parsing continues" {
		t.Errorf("message = %q, want parsing continues", got)
	}
	if got := document.EmptyBrushBlocksSkipped(); got != 2 {
		t.Fatalf("EmptyBrushBlocksSkipped() = %d, want 2", got)
	}
	diagnostics := document.ParseDiagnostics()
	if len(diagnostics) != 2 {
		t.Fatalf("len(ParseDiagnostics()) = %d, want 2", len(diagnostics))
	}
	wantLines := []int{3, 6}
	for index, diagnostic := range diagnostics {
		if diagnostic.Code != DiagnosticEmptyBrushBlockSkipped {
			t.Errorf("diagnostic %d code = %q", index, diagnostic.Code)
		}
		if diagnostic.Source != "worldcraft.map" || diagnostic.Line != wantLines[index] || diagnostic.Column != 1 {
			t.Errorf("diagnostic %d location = %s:%d:%d, want worldcraft.map:%d:1",
				index, diagnostic.Source, diagnostic.Line, diagnostic.Column, wantLines[index])
		}
	}

	// Diagnostics are provenance, so they do not change semantic equality.
	withoutEmpty := mustParse(t, `{
"classname" "worldspawn"
"message" "parsing continues"
{
( 0 0 0 ) ( 64 0 0 ) ( 64 64 0 ) dev/dev_gray 0 0 0 1 1
}
}
`)
	if !document.Equal(withoutEmpty) {
		t.Error("skipped empty brushes changed semantic equality")
	}
}

func TestParseEmptyBrushDiagnosticsAreBoundedButCountIsExact(t *testing.T) {
	const emptyBrushes = 70
	source := "{\n\"classname\" \"worldspawn\"\n" + strings.Repeat("{}\n", emptyBrushes) + "}\n"
	document := mustParse(t, source)
	if got := document.EmptyBrushBlocksSkipped(); got != emptyBrushes {
		t.Errorf("EmptyBrushBlocksSkipped() = %d, want %d", got, emptyBrushes)
	}
	if got := len(document.ParseDiagnostics()); got != maxParseDiagnostics {
		t.Errorf("len(ParseDiagnostics()) = %d, want bounded maximum %d", got, maxParseDiagnostics)
	}
}

func TestParseEmptyEntitySemanticsAreUnchanged(t *testing.T) {
	document := mustParse(t, "{\n}\n")
	if got := document.EntityCount(); got != 1 {
		t.Errorf("EntityCount() = %d, want 1", got)
	}
	if got := len(document.Entity(0).Items()); got != 0 {
		t.Errorf("empty entity has %d items, want 0", got)
	}
	if got := document.EmptyBrushBlocksSkipped(); got != 0 {
		t.Errorf("EmptyBrushBlocksSkipped() = %d, want 0 for an empty entity", got)
	}
}

func TestParseQuake2FixturePreservesUnknownKeysAndFaceTail(t *testing.T) {
	document := mustParseFile(t, filepath.Join("testdata", "q2", "basic_q2.map"))

	if got := document.EntityCount(); got != 2 {
		t.Fatalf("EntityCount() = %d, want 2", got)
	}
	if got := document.BrushCount(); got != 1 {
		t.Fatalf("BrushCount() = %d, want 1", got)
	}

	worldspawn := document.Entity(0)
	speaker := document.Entity(1)
	brush := worldspawn.Brushes()[0]

	if got := mustGet(t, worldspawn, "classname"); got != "worldspawn" {
		t.Errorf("classname = %q, want worldspawn", got)
	}
	// A `//` sequence inside a quoted value is content, not a comment.
	if got := mustGet(t, worldspawn, "_q2_unknown"); got != "verbatim // value" {
		t.Errorf("_q2_unknown = %q, want %q", got, "verbatim // value")
	}
	if got := mustGet(t, speaker, "classname"); got != "target_speaker" {
		t.Errorf("speaker classname = %q, want target_speaker", got)
	}
	if got := mustGet(t, speaker, "noise"); got != "world/misc/secret.wav" {
		t.Errorf("noise = %q, want world/misc/secret.wav", got)
	}
	if got := mustGet(t, speaker, "_user_1998_key"); got != "preserve this" {
		t.Errorf("_user_1998_key = %q, want %q", got, "preserve this")
	}
	if got := brush.FaceCount(); got != 6 {
		t.Fatalf("face count = %d, want 6", got)
	}
	textures := textureSet(brush.Faces())
	if len(textures) != 1 || textures["e1u1/brtrim1_5"] != 6 {
		t.Errorf("textures = %v, want only e1u1/brtrim1_5", textures)
	}
	if got := brush.Face(0).P3().X(); got != -64 {
		t.Errorf("face 0 p3.x = %v, want -64", got)
	}
	if got := brush.Face(1).XOffset(); got != -8 {
		t.Errorf("face 1 x offset = %v, want -8", got)
	}
	assertNumbers(t, "face 0 extra values", brush.Face(0).ExtraValues(), []Number{0, 0, 0})
	assertNumbers(t, "face 1 extra values", brush.Face(1).ExtraValues(), []Number{1, 2, 3})
}

func TestParseDecimalCoordinatesAndTextureFields(t *testing.T) {
	document := mustParse(t, `{
"classname" "worldspawn"
{
( 0.5 0 -1.25 ) ( 64 0 0 ) ( 64 64.75 0 ) dev/dev_gray 1.5 -2 45 0.5 2.25
}
}
`)

	face := document.Entity(0).Brushes()[0].Face(0)
	checks := []struct {
		name string
		got  Number
		want Number
	}{
		{"p1.x", face.P1().X(), 0.5},
		{"p1.z", face.P1().Z(), -1.25},
		{"p3.y", face.P3().Y(), 64.75},
		{"x offset", face.XOffset(), 1.5},
		{"y offset", face.YOffset(), -2},
		{"rotation", face.Rotation(), 45},
		{"x scale", face.XScale(), 0.5},
		{"y scale", face.YScale(), 2.25},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %v, want %v", check.name, check.got, check.want)
		}
	}
}

func TestMalformedValve220FaceFailsClearly(t *testing.T) {
	_, err := ParseNamed(`{
"classname" "worldspawn"
{
( 0 0 0 ) ( 64 0 0 ) ( 64 64 0 ) dev/dev_gray [ 1 0 0 ] [ 0 1 0 0 ] 0 1 1
}
}
`, "broken_valve220.map")

	if err == nil {
		t.Fatal("expected a parse failure")
	}
	message := err.Error()
	if !strings.Contains(message, "Valve 220 texture axis") {
		t.Errorf("error %q does not name the Valve 220 texture axis", message)
	}
	if !strings.Contains(message, "broken_valve220.map:4:") {
		t.Errorf("error %q does not report the source and line 4", message)
	}

	// The line number is structured, not only formatted into the message.
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("error is %T, want *ParseError", err)
	}
	if parseErr.Line != 4 {
		t.Errorf("ParseError.Line = %d, want 4", parseErr.Line)
	}
}

func TestParseErrorReportsLine(t *testing.T) {
	_, err := ParseNamed("{\n\"classname\"\n}\n", "broken.map")

	if err == nil {
		t.Fatal("expected a parse failure")
	}
	if !strings.Contains(err.Error(), "broken.map:2") {
		t.Errorf("error %q does not report broken.map line 2", err.Error())
	}
}

// Malformed input must fail with a position and never panic. This table also
// pins the ParseError classification for each shape of breakage.
func TestMalformedInputsFailWithAPosition(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		wantLine    int
		wantMessage string
	}{
		{
			name:        "truncated brush",
			source:      "{\n\"classname\" \"worldspawn\"\n{\n( 0 0 0 ) ( 64 0 0 ) ( 64 64 0 ) dev/dev_gray 0 0 0 1 1\n",
			wantLine:    4,
			wantMessage: "unterminated brush",
		},
		{
			name:        "missing closing brace",
			source:      "{\n\"classname\" \"worldspawn\"\n",
			wantLine:    2,
			wantMessage: "unterminated entity",
		},
		{
			name:        "closing brace before opening",
			source:      "}\n",
			wantLine:    1,
			wantMessage: "entity closed before it was opened",
		},
		{
			name:        "malformed valve220 axis",
			source:      "{\n\"classname\" \"worldspawn\"\n{\n( 0 0 0 ) ( 64 0 0 ) ( 64 64 0 ) t [ 1 0 0 ] [ 0 1 0 0 ] 0 1 1\n}\n}\n",
			wantLine:    4,
			wantMessage: "malformed Valve 220 texture axis",
		},
		{
			name:        "non-numeric coordinate",
			source:      "{\n\"classname\" \"worldspawn\"\n{\n( 0 zero 0 ) ( 64 0 0 ) ( 64 64 0 ) t 0 0 0 1 1\n}\n}\n",
			wantLine:    4,
			wantMessage: "expected numeric y coordinate",
		},
		{
			name:     "key without value",
			source:   "{\n\"classname\"\n}\n",
			wantLine: 2,
			// Single-quoted: keys are interpolated in Python repr form — see
			// mapfile.ReprString.
			wantMessage: `expected quoted value for entity key 'classname'`,
		},
		{
			name:        "unterminated quoted string",
			source:      "{\n\"classname\" \"worldspawn\n}\n",
			wantLine:    2,
			wantMessage: "unterminated quoted string",
		},
		{
			name:        "entity item that is neither key nor brush",
			source:      "{\n42\n}\n",
			wantLine:    2,
			wantMessage: "expected entity key/value or brush",
		},
		{
			name:        "face point missing its closing paren",
			source:      "{\n\"classname\" \"worldspawn\"\n{\n( 0 0 0 ( 64 0 0 ) ( 64 64 0 ) t 0 0 0 1 1\n}\n}\n",
			wantLine:    4,
			wantMessage: "expected face point end",
		},
		{
			name:        "texture name missing",
			source:      "{\n\"classname\" \"worldspawn\"\n{\n( 0 0 0 ) ( 64 0 0 ) ( 64 64 0 )\n}\n}\n",
			wantLine:    5,
			wantMessage: "expected texture name after face points",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A panic here fails the test with the stack, which is the point:
			// malformed input must produce an error, never a crash.
			document, err := ParseNamed(tt.source, "malformed.map")
			if err == nil {
				t.Fatalf("ParseNamed() unexpectedly succeeded with %d entities", document.EntityCount())
			}

			var parseErr *ParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("error is %T (%v), want *ParseError", err, err)
			}
			if parseErr.Line != tt.wantLine {
				t.Errorf("ParseError.Line = %d, want %d (error: %v)", parseErr.Line, tt.wantLine, err)
			}
			if parseErr.Column < 1 {
				t.Errorf("ParseError.Column = %d, want a 1-based column", parseErr.Column)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantMessage)
			}
			if !strings.HasPrefix(err.Error(), "malformed.map:") {
				t.Errorf("error = %q, want it to start with the source name", err.Error())
			}
		})
	}
}

// Empty and whitespace-only input is valid and yields an empty document: the
// parse loop simply never runs.
func TestParseEmptyDocument(t *testing.T) {
	for _, source := range []string{"", "\n", "   \n// only a comment\n"} {
		document, err := Parse(source)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", source, err)
		}
		if got := document.EntityCount(); got != 0 {
			t.Errorf("Parse(%q) EntityCount() = %d, want 0", source, got)
		}
	}
}

func assertNumbers(t *testing.T, label string, got, want []Number) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", label, got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s = %v, want %v", label, got, want)
			return
		}
	}
}

// TestUnicodeDigitsAreRejectedCleanly closes .agents/HITL.md question 3.
//
// A number pattern written with Python's `\d` is Unicode-aware and would
// accept a coordinate written with non-ASCII digits such as ٤٢. Go's regexp
// `\d` is ASCII-only, so AUE rejects it.
//
// Human decision, 2026-08-01: ASCII-only is deliberate and stays. Real `.map`
// and APMap number literals are always ASCII, and accepting other digits would
// be a side effect of a Unicode `\d` rather than a design choice. The goal is
// predictable support for other formats, not maximal permissiveness.
//
// What this test guarantees is not the rejection alone but the *shape* of it:
// a well-formed *ParseError carrying a line and a column, never a panic and
// never a silent misparse into some other number.
func TestUnicodeDigitsAreRejectedCleanly(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "arabic-indic digits in a brush coordinate",
			source: "{\n\"classname\" \"worldspawn\"\n{\n" +
				"( 0 0 0 ) ( ٤٢ 0 0 ) ( 64 64 0 ) stone 0 0 0 1 1\n}\n}\n",
			want: "expected numeric",
		},
		{
			name: "devanagari digits in a brush coordinate",
			source: "{\n\"classname\" \"worldspawn\"\n{\n" +
				"( 0 0 0 ) ( ०१ 0 0 ) ( 64 64 0 ) stone 0 0 0 1 1\n}\n}\n",
			want: "expected numeric",
		},
		{
			name: "fullwidth digits in a brush coordinate",
			source: "{\n\"classname\" \"worldspawn\"\n{\n" +
				"( 0 0 0 ) ( ４２ 0 0 ) ( 64 64 0 ) stone 0 0 0 1 1\n}\n}\n",
			want: "expected numeric",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := ParseNamed(test.source, "unicode.map")

			if err == nil {
				t.Fatalf("parse succeeded, want a clean rejection; got %d entities", document.EntityCount())
			}
			var parseErr *ParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("error is %T, want *ParseError", err)
			}
			if parseErr.Line != 4 {
				t.Errorf("ParseError.Line = %d, want 4", parseErr.Line)
			}
			if parseErr.Column <= 0 {
				t.Errorf("ParseError.Column = %d, want a positive column", parseErr.Column)
			}
			if parseErr.Source != "unicode.map" {
				t.Errorf("ParseError.Source = %q, want unicode.map", parseErr.Source)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), test.want)
			}
			// The position prefix must be usable, not just present.
			if !strings.HasPrefix(err.Error(), "unicode.map:4:") {
				t.Errorf("error = %q, want a unicode.map:4:<col> prefix", err.Error())
			}
		})
	}
}

// TestASCIIDigitsStillParse is the other half of the decision above: rejecting
// Unicode digits must not have narrowed what a real `.map` may contain.
// Decimals in particular stay accepted — that is parser behaviour, distinct
// from the integer-only rule on *generated* box brushes.
func TestASCIIDigitsStillParse(t *testing.T) {
	source := "{\n\"classname\" \"worldspawn\"\n{\n" +
		"( 0 0 0 ) ( 64.5 0 0 ) ( -64 64 0 ) stone 0 0 0 1 1\n}\n}\n"

	document, err := ParseNamed(source, "decimal.map")
	if err != nil {
		t.Fatalf("decimal coordinates must still parse: %v", err)
	}
	face := document.Entity(0).Brushes()[0].Face(0)
	if got := face.P2().X(); got != 64.5 {
		t.Errorf("second point x = %v, want 64.5", got)
	}
}
