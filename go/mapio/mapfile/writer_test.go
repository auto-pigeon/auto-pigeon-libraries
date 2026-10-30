package mapfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// roundTripFixtures is every fixture the round-trip tests run over.
var roundTripFixtures = []string{
	filepath.Join("testdata", "known_good", "one_cube.map"),
	filepath.Join("testdata", "one_room.map"),
	filepath.Join("testdata", "door_trigger.map"),
	filepath.Join("testdata", "train_path.map"),
	filepath.Join("testdata", "q2", "basic_q2.map"),
	filepath.Join("testdata", "q2", "valve220_door.map"),
	filepath.Join("testdata", "q2", "classic_areaportal.map"),
}

func mustWrite(t *testing.T, document *MapFile) string {
	t.Helper()
	text, err := Write(document)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	return text
}

func TestWriteIsDeterministicAndParseable(t *testing.T) {
	document := mustParse(t, sampleMap)
	first := mustWrite(t, document)
	second := mustWrite(t, mustParse(t, first))

	if first != second {
		t.Errorf("write→parse→write is not stable:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	for _, want := range []string{`"_unknown" "preserved"`, `"_path" "textures\base"`, "__TB_empty"} {
		if !strings.Contains(first, want) {
			t.Errorf("output does not contain %q:\n%s", want, first)
		}
	}
}

// Determinism means the same model produces the same bytes every time, not only
// that a second parse→write agrees. Go's map iteration order makes it worth
// pinning explicitly.
func TestWriteIsByteIdenticalAcrossRepeatedCalls(t *testing.T) {
	for _, fixture := range roundTripFixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			document := mustParseFile(t, fixture)
			want := mustWrite(t, document)
			for i := 0; i < 20; i++ {
				if got := mustWrite(t, document); got != want {
					t.Fatalf("Write() output changed on call %d", i+2)
				}
			}
		})
	}
}

func TestParseWriteParsePreservesEquivalentModel(t *testing.T) {
	for _, fixture := range roundTripFixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			original := mustParseFile(t, fixture)
			roundTripped, err := ParseNamed(mustWrite(t, original), fixture)
			if err != nil {
				t.Fatalf("reparse error = %v", err)
			}
			if !roundTripped.Equal(original) {
				t.Errorf("round trip changed the model for %s", fixture)
			}
		})
	}
}

// An unknown key must survive parse→write→parse *unchanged in place*, not merely
// survive parsing. This checks the key, its exact value, and its position among
// its siblings.
func TestUnknownKeysSurviveARoundTripInSourceOrder(t *testing.T) {
	source := `{
"classname" "worldspawn"
"_unknown_first" "value one"
"wad" "example.wad"
"_unknown_second" "verbatim // not a comment"
"_path" "textures\base"
{
( 0 0 0 ) ( 64 0 0 ) ( 64 64 0 ) dev/dev_gray 0 0 0 1 1
}
"_unknown_after_brush" "still here"
}
`
	original := mustParse(t, source)
	text := mustWrite(t, original)
	reparsed := mustParse(t, text)

	wantKeys := []string{
		"classname", "_unknown_first", "wad", "_unknown_second", "_path", "_unknown_after_brush",
	}
	var gotKeys []string
	for _, kv := range reparsed.Entity(0).Properties() {
		gotKeys = append(gotKeys, kv.Key())
	}
	if strings.Join(gotKeys, ",") != strings.Join(wantKeys, ",") {
		t.Errorf("property order = %v, want %v", gotKeys, wantKeys)
	}

	wantValues := map[string]string{
		"_unknown_first":       "value one",
		"_unknown_second":      "verbatim // not a comment",
		"_path":                `textures\base`,
		"_unknown_after_brush": "still here",
	}
	for key, want := range wantValues {
		if got := mustGet(t, reparsed.Entity(0), key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	// The brush stayed between "_path" and "_unknown_after_brush": items keep
	// source order, not properties-then-brushes order.
	items := reparsed.Entity(0).Items()
	if _, ok := items[5].(Brush); !ok {
		t.Errorf("item 5 is %T, want the brush to stay in source position", items[5])
	}
	if !reparsed.Equal(original) {
		t.Error("round trip changed the model")
	}
}

func TestWriteFileRoundTripOneRoom(t *testing.T) {
	original := mustParseFile(t, filepath.Join("testdata", "one_room.map"))
	out := filepath.Join(t.TempDir(), "one_room_rt.map")

	if err := WriteFile(original, out); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	roundTripped := mustParseFile(t, out)

	if !roundTripped.Equal(original) {
		t.Error("round trip through a file changed the model")
	}
}

func TestWriteFilePreservesQuake2FixtureModel(t *testing.T) {
	original := mustParseFile(t, filepath.Join("testdata", "q2", "basic_q2.map"))
	out := filepath.Join(t.TempDir(), "basic_q2_rt.map")

	if err := WriteFile(original, out); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	roundTripped := mustParseFile(t, out)

	if !roundTripped.Equal(original) {
		t.Error("round trip through a file changed the model")
	}
	if got := mustGet(t, roundTripped.Entity(1), "_user_1998_key"); got != "preserve this" {
		t.Errorf("_user_1998_key = %q, want %q", got, "preserve this")
	}
	assertNumbers(t, "face 0 extra values",
		roundTripped.Entity(0).Brushes()[0].Face(0).ExtraValues(), []Number{0, 0, 0})
}

// WriteTo must agree with Write byte for byte; it exists so the CLI can stream.
func TestWriteToMatchesWrite(t *testing.T) {
	for _, fixture := range roundTripFixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			document := mustParseFile(t, fixture)
			var buffer bytes.Buffer
			if err := WriteTo(&buffer, document); err != nil {
				t.Fatalf("WriteTo() error = %v", err)
			}
			if buffer.String() != mustWrite(t, document) {
				t.Error("WriteTo output differs from Write output")
			}
		})
	}
}

// The writer refuses a key/value it cannot represent. Such a
// value is reachable from parsed input, because the tokenizer keeps backslash
// escapes verbatim: `"a\"b"` parses to a value containing a raw quote.
func TestWriteRejectsUnrepresentableValues(t *testing.T) {
	document := mustParse(t, "{\n\"key\" \"a\\\"b\"\n}\n")
	if got := mustGet(t, document.Entity(0), "key"); got != `a\"b` {
		t.Fatalf("parsed value = %q, want %q", got, `a\"b`)
	}

	if _, err := Write(document); err == nil {
		t.Error("Write() accepted a value containing a raw quote")
	} else {
		var writeErr *WriteError
		if !errors.As(err, &writeErr) {
			t.Errorf("error is %T, want *WriteError", err)
		}
	}

	newline := NewMapFile([]Entity{NewEntity([]EntityItem{NewKeyValue("key", "line\nbreak")})})
	if _, err := Write(newline); err == nil {
		t.Error("Write() accepted a value containing a newline")
	}
}

// The writer emits the empty string, not a lone newline, for a document with no
// entities — so an empty map round-trips.
func TestWriteEmptyDocument(t *testing.T) {
	document := mustParse(t, "")
	text := mustWrite(t, document)
	if text != "" {
		t.Errorf("Write() = %q, want the empty string", text)
	}
	reparsed := mustParse(t, text)
	if reparsed.EntityCount() != 0 {
		t.Errorf("reparsed EntityCount() = %d, want 0", reparsed.EntityCount())
	}
}

func TestWriteNeverEmitsEmptyBrushBlocks(t *testing.T) {
	parsed := mustParse(t, "{\n\"classname\" \"worldspawn\"\n{\n}\n}\n")
	text := mustWrite(t, parsed)
	if strings.Contains(text, "{\n}\n") {
		t.Errorf("Write() emitted an empty brush block:\n%s", text)
	}

	handBuilt := NewMapFile([]Entity{NewEntity([]EntityItem{NewBrush(nil)})})
	if _, err := Write(handBuilt); err == nil {
		t.Error("Write() accepted a hand-built brush with no faces")
	} else if !strings.Contains(err.Error(), "brush with no faces") {
		t.Errorf("Write() error = %q, want empty-brush explanation", err)
	}
}

// FormatNumber is the single rule every written number goes through: an
// integer or integral float prints as an integer, anything else as the
// shortest round-trip decimal, switching to an exponent only below 1e-4.
func TestFormatNumberRule(t *testing.T) {
	tests := []struct {
		value Number
		want  string
	}{
		{0, "0"},
		{-0, "0"},
		{1, "1"},
		{-64, "-64"},
		{1.0, "1"}, // an integral float prints as an integer
		{64.75, "64.75"},
		{-1.25, "-1.25"},
		{0.5, "0.5"},
		{2.25, "2.25"},
		{0.0001, "0.0001"},
		// Q2_002 (F18): positional always. Python's repr writes 1e-05 here, which this package's own
		// parser refuses; the digits are still the shortest round-trip ones.
		{0.00001, "0.00001"},
		{0.000034, "0.000034"},
		{1e16, "10000000000000000"},

		// Regression, prompt 16. FormatNumber used Go's 'g' verb, whose
		// shortest-form exponent threshold is 1e6 where Python's repr switches
		// at 1e16. Every non-integral coordinate at or above 1e6 was written in
		// scientific notation — `1.2345675e+06` — which the parser does not
		// accept, so `roundtrip` produced an unparseable `.map` rather than
		// merely a differently-formatted one.
		{1234567.5, "1234567.5"},
		{12345678.125, "12345678.125"},
		{999999.5, "999999.5"},   // just below the old threshold
		{1000000.5, "1000000.5"}, // just above it: the first value that broke
		{-1234567.5, "-1234567.5"},
		{999999900000.0001, "999999900000.0001"},
		{123456789012345.6, "123456789012345.6"},

		// The other half of the same fix: an integral float too large for an
		// int64 is Python's exact `int(value)`, not the shortest round-trip
		// spelling. str(int(1e30)) is 1000000000000000019884624838656.
		{1e18, "1000000000000000000"},
		{1e30, "1000000000000000019884624838656"},
	}

	for _, tt := range tests {
		if got := FormatNumber(tt.value); got != tt.want {
			t.Errorf("FormatNumber(%v) = %q, want %q", tt.value, got, tt.want)
		}
	}

	// A value written by FormatNumber must be readable again — the property
	// the scientific-notation bug actually broke, stated directly rather than
	// only as a string comparison.
	for _, value := range []Number{1234567.5, 1000000.5, -98765432.25, 1e18} {
		text := FormatNumber(value)
		if strings.ContainsAny(text, "eE") {
			t.Errorf("FormatNumber(%v) = %q; a coordinate with an exponent cannot be parsed back", value, text)
		}
	}

	// Negative zero must not print as "-0": Python's str(int(-0.0)) is "0".
	if got := FormatNumber(negativeZero()); got != "0" {
		t.Errorf("FormatNumber(-0.0) = %q, want %q", got, "0")
	}
}

func negativeZero() Number {
	zero := Number(0)
	return -zero
}

// Writing to a file must produce exactly what Write produced.
func TestWriteFileContentMatchesWrite(t *testing.T) {
	document := mustParseFile(t, filepath.Join("testdata", "one_room.map"))
	out := filepath.Join(t.TempDir(), "one_room.map")
	if err := WriteFile(document, out); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != mustWrite(t, document) {
		t.Error("WriteFile content differs from Write output")
	}
}

// TestWriteMatchesFrozenOutput pins the writer's exact bytes for every
// round-trip fixture. The expected text is frozen from AUE output: the four
// Quake 1 fixtures are reproduced byte for byte, and the three Quake 2 fixtures
// lose only their leading comment lines, frozen under testdata/golden/q2/.
// Structural round-trip tests would pass a spacing or number-formatting
// change; this one does not.
func TestWriteMatchesFrozenOutput(t *testing.T) {
	for _, fixture := range roundTripFixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			golden := fixture
			if strings.Contains(fixture, string(filepath.Separator)+"q2"+string(filepath.Separator)) {
				golden = filepath.Join("testdata", "golden", "q2", filepath.Base(fixture))
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("ReadFile(%s) error = %v", golden, err)
			}
			got := mustWrite(t, mustParseFile(t, fixture))
			if got != string(want) {
				t.Errorf("Write(%s) differs from %s\ngot:\n%s\nwant:\n%s", fixture, golden, got, want)
			}
			// The frozen text is a fixed point of parse→write.
			if again := mustWrite(t, mustParse(t, got)); again != got {
				t.Errorf("rewriting the frozen output of %s changed it", fixture)
			}
		})
	}
}
