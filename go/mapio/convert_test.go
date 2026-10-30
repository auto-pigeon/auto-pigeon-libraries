package mapio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
)

// The fixtures are deliberately tiny and hand-written: this package's job is
// routing and validation, and the formats themselves are pinned against the
// real corpus in the mapfile, apmap and rmf packages.

const q1MapText = `{
"classname" "worldspawn"
"wad" "quake101.wad"
{
( 0 0 0 ) ( 0 1 0 ) ( 0 0 1 ) city2_1 0 0 0 1 1
( 64 64 64 ) ( 64 64 65 ) ( 64 65 64 ) city2_1 0 0 0 1 1
( 0 0 0 ) ( 0 0 1 ) ( 1 0 0 ) city2_1 0 0 0 1 1
( 64 64 64 ) ( 65 64 64 ) ( 64 64 65 ) city2_1 0 0 0 1 1
( 0 0 0 ) ( 1 0 0 ) ( 0 1 0 ) city2_1 0 0 0 1 1
( 64 64 64 ) ( 64 65 64 ) ( 65 64 64 ) city2_1 0 0 0 1 1
}
}
{
"classname" "info_player_start"
"origin" "32 32 24"
}
`

// The Quake 2 fixture is the Quake 1 one plus the three trailing surface
// fields, which is exactly what distinguishes the two dialects.
const q2MapText = `{
"classname" "worldspawn"
{
( 0 0 0 ) ( 0 1 0 ) ( 0 0 1 ) e1u1/baselt_4 0 0 0 1 1 0 0 0
( 64 64 64 ) ( 64 64 65 ) ( 64 65 64 ) e1u1/baselt_4 0 0 0 1 1 0 0 0
( 0 0 0 ) ( 0 0 1 ) ( 1 0 0 ) e1u1/baselt_4 0 0 0 1 1 0 0 0
( 64 64 64 ) ( 65 64 64 ) ( 64 64 65 ) e1u1/baselt_4 0 0 0 1 1 0 0 0
( 0 0 0 ) ( 1 0 0 ) ( 0 1 0 ) e1u1/baselt_4 0 0 0 1 1 0 0 0
( 64 64 64 ) ( 64 65 64 ) ( 65 64 64 ) e1u1/sky1 0 0 0 1 1 0 132 0
}
}
`

// schemaOrSkip loads the real APMap schema, which lives outside the repository.
func schemaOrSkip(t *testing.T) *apmap.Object {
	t.Helper()
	schema, err := apmap.CurrentSchema()
	if err != nil {
		t.Skipf("APMap schema unavailable: %v", err)
	}
	return schema
}

func convertOrFail(t *testing.T, name string, data []byte, direction string, opts Options) ([]byte, Result) {
	t.Helper()
	resolved, ok := LookupDirection(direction)
	if !ok {
		t.Fatalf("unknown direction %q", direction)
	}
	output, result, err := Convert(context.Background(), resolved, name, data, opts)
	if err != nil {
		t.Fatalf("%s: %v", direction, err)
	}
	return output, result
}

func TestDirectionsAreComplete(t *testing.T) {
	want := []string{
		"apmap-to-q1map", "apmap-to-q2map", "apmap-to-q2rmf", "apmap-to-q3map",
		"q1map-to-apmap", "q2map-to-apmap", "q2rmf-to-apmap",
	}
	got := DirectionNames()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("directions = %v, want %v", got, want)
	}
	for _, name := range want {
		direction, ok := LookupDirection(name)
		if !ok {
			t.Fatalf("LookupDirection(%q) failed", name)
		}
		if direction.Summary == "" {
			t.Errorf("%s has no summary, so it cannot appear in usage text", name)
		}
	}
	if _, ok := LookupDirection("q1map-to-q2rmf"); ok {
		t.Error("RMF must not convert directly to or from .map")
	}
}

func TestMapConversionReportsSkippedEmptyBrushBlocks(t *testing.T) {
	input := strings.Replace(q2MapText, "{\n( 0 0 0 )", "{\n}\n{\n( 0 0 0 )", 1)
	_, result := convertOrFail(t, "worldcraft.map", []byte(input), "q2map-to-apmap", Options{Validate: false})
	if got, want := strings.Join(warningTexts(result.Warnings), "\n"), "empty_brush_blocks_skipped: 1"; got != want {
		t.Errorf("Warnings = %q, want %q", got, want)
	}
}

// TestMapToAPMapAndBack is the headline round trip for both `.map` games: the
// text that comes back must be the text that went in.
func TestMapToAPMapAndBack(t *testing.T) {
	schema := schemaOrSkip(t)
	for _, testCase := range []struct {
		game    string
		text    string
		forward string
		back    string
	}{
		{"quake1", q1MapText, "q1map-to-apmap", "apmap-to-q1map"},
		{"quake2", q2MapText, "q2map-to-apmap", "apmap-to-q2map"},
	} {
		t.Run(testCase.game, func(t *testing.T) {
			opts := Options{Validate: true, Schema: schema}
			document, forward := convertOrFail(t, "fixture.map", []byte(testCase.text), testCase.forward, opts)
			if !forward.Validated {
				t.Error("validation did not run despite Validate: true")
			}
			if forward.OutputName != "fixture.apmap" {
				t.Errorf("output name = %q, want fixture.apmap", forward.OutputName)
			}

			// The produced document must declare the game the direction names.
			decoded, err := apmap.DecodeJSON(document)
			if err != nil {
				t.Fatalf("the produced document is not JSON: %v", err)
			}
			object, ok := decoded.(*apmap.Object)
			if !ok {
				t.Fatal("the produced document is not a JSON object")
			}
			if game, _ := object.Get("game"); game != testCase.game {
				t.Errorf("game = %v, want %s", game, testCase.game)
			}

			text, back := convertOrFail(t, "fixture.apmap", document, testCase.back, opts)
			if back.OutputName != "fixture.map" {
				t.Errorf("output name = %q, want fixture.map", back.OutputName)
			}
			if string(text) != testCase.text {
				t.Errorf("round trip changed the map\n got:\n%s\nwant:\n%s", text, testCase.text)
			}
		})
	}
}

// TestRMFRoundTripThroughAPMap covers the pair the human specified: `.rmf` and
// APMap convert to each other, and `.rmf` never touches `.map` directly.
func TestRMFRoundTripThroughAPMap(t *testing.T) {
	schema := schemaOrSkip(t)
	opts := Options{Validate: true, Schema: schema}

	// Build an RMF by converting a Quake 2 map to APMap and out again, which
	// is the only way to obtain one without a binary fixture in the repo.
	document, _ := convertOrFail(t, "fixture.map", []byte(q2MapText), "q2map-to-apmap", opts)
	binary, result := convertOrFail(t, "fixture.apmap", document, "apmap-to-q2rmf", opts)
	if result.OutputName != "fixture.rmf" {
		t.Errorf("output name = %q, want fixture.rmf", result.OutputName)
	}
	if len(binary) == 0 {
		t.Fatal("apmap-to-q2rmf produced no bytes")
	}

	// And back.
	//
	// The expected text is not the input verbatim, and the difference is the
	// one lossy step in this pair: RMF stores a face's contents, flags and
	// value as three int32s that are always present, so it cannot distinguish
	// an explicit `0 0 0` tail from no tail at all. Worldcraft resolves that
	// the same way on export — it omits the three fields when all are zero —
	// and the reader reproduces its rule, so an explicit all-zero tail comes
	// back absent. A face carrying real surface data is unaffected, which is
	// what the sky face below pins down.
	wantText := strings.ReplaceAll(q2MapText, "e1u1/baselt_4 0 0 0 1 1 0 0 0", "e1u1/baselt_4 0 0 0 1 1")
	if wantText == q2MapText {
		t.Fatal("fixture no longer contains the all-zero tails this test is about")
	}

	roundTripped, _ := convertOrFail(t, "fixture.rmf", binary, "q2rmf-to-apmap", opts)
	text, _ := convertOrFail(t, "fixture.apmap", roundTripped, "apmap-to-q2map", opts)
	if string(text) != wantText {
		t.Errorf("rmf round trip changed the map\n got:\n%s\nwant:\n%s", text, wantText)
	}
	if !strings.Contains(string(text), "e1u1/sky1 0 0 0 1 1 0 132 0") {
		t.Error("the sky face's surface flags did not survive the rmf round trip")
	}

	// Converting again must now be a fixed point: the second trip through RMF
	// changes nothing, which is what makes the pair usable in a pipeline.
	stable, _ := convertOrFail(t, "fixture.map", text, "q2map-to-apmap", opts)
	stableBinary, _ := convertOrFail(t, "fixture.apmap", stable, "apmap-to-q2rmf", opts)
	stableDocument, _ := convertOrFail(t, "fixture.rmf", stableBinary, "q2rmf-to-apmap", opts)
	stableText, _ := convertOrFail(t, "fixture.apmap", stableDocument, "apmap-to-q2map", opts)
	if string(stableText) != wantText {
		t.Errorf("a second rmf round trip was not a fixed point\n got:\n%s\nwant:\n%s",
			stableText, wantText)
	}
}

// TestValidationRejectsABadDocument is the point of the --validate-off switch
// existing: with validation on the conversion must refuse, with it off the same
// input must get through.
func TestValidationRejectsABadDocument(t *testing.T) {
	schema := schemaOrSkip(t)
	document, _ := convertOrFail(t, "fixture.map", []byte(q1MapText),
		"q1map-to-apmap", Options{Validate: true, Schema: schema})

	// Break the document in a way the schema catches but the converter does
	// not need: an unknown role.
	broken := strings.Replace(string(document), `"role": "full_map"`, `"role": "not_a_role"`, 1)
	if broken == string(document) {
		t.Fatal("fixture did not contain the role field this test rewrites")
	}

	direction, _ := LookupDirection("apmap-to-q1map")
	_, _, err := Convert(context.Background(), direction, "broken.apmap", []byte(broken), Options{Validate: true, Schema: schema})
	if err == nil {
		t.Fatal("a document with an invalid role should not convert with validation on")
	}
	validationErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("error = %T (%v), want *ValidationError", err, err)
	}
	if len(validationErr.Problems) == 0 {
		t.Error("a validation error must carry the problems it found")
	}

	_, result, err := Convert(context.Background(), direction, "broken.apmap", []byte(broken), Options{Validate: false})
	if err != nil {
		t.Fatalf("with validation off the same document should convert: %v", err)
	}
	if result.Validated {
		t.Error("Validated should be false when validation is off")
	}
}

// TestValidationOffStillSkipsValidation: --validate-off skips VALIDATION.
//
// It no longer makes the contract unnecessary, and that is deliberate. The writer has to know which
// version to declare and derives it from the one schema file rather than naming it, so a conversion
// without a contract fails whether validation is on or off. The alternative is emitting a document
// with no version, or with one this build invented — neither of which anything else could read.
func TestValidationOffStillSkipsValidation(t *testing.T) {
	direction, _ := LookupDirection("q1map-to-apmap")
	if _, _, err := Convert(context.Background(), direction, "fixture.map", []byte(q1MapText), Options{Validate: false}); err != nil {
		t.Fatalf("conversion with validation off must still convert: %v", err)
	}
	if _, _, err := Convert(context.Background(), direction, "fixture.map", []byte(q1MapText), Options{Validate: true}); err != nil {
		t.Fatalf("conversion with validation on must succeed against the loaded contract: %v", err)
	}
}

func TestConvertReportsTimingAndSizes(t *testing.T) {
	_, result := convertOrFail(t, "fixture.map", []byte(q1MapText),
		"q1map-to-apmap", Options{Validate: false})
	if result.Duration <= 0 {
		t.Error("duration must be measured")
	}
	if result.InputBytes != len(q1MapText) {
		t.Errorf("input bytes = %d, want %d", result.InputBytes, len(q1MapText))
	}
	if result.OutputBytes <= 0 {
		t.Error("output bytes must be reported")
	}
	if result.Direction != "q1map-to-apmap" {
		t.Errorf("direction = %q", result.Direction)
	}
}

func TestParseErrorsNameTheInput(t *testing.T) {
	direction, _ := LookupDirection("q1map-to-apmap")
	_, _, err := Convert(context.Background(), direction, "broken.map", []byte("{ not a map"), Options{Validate: false})
	if err == nil {
		t.Fatal("expected a parse failure")
	}
	if !strings.Contains(err.Error(), "broken.map") {
		t.Errorf("error = %v, want it to name broken.map", err)
	}
}

func TestOutputName(t *testing.T) {
	direction, _ := LookupDirection("q1map-to-apmap")
	for input, want := range map[string]string{
		"e1m1.map":            "e1m1.apmap",
		"/corpus/dm/e1m1.map": "e1m1.apmap",
		"noextension":         "noextension.apmap",
		"archive.tar.map":     "archive.tar.apmap",
	} {
		if got := OutputName(direction, input); got != want {
			t.Errorf("OutputName(%q) = %q, want %q", input, got, want)
		}
	}

	rmfDirection, _ := LookupDirection("apmap-to-q2rmf")
	if got := OutputName(rmfDirection, "e1m1.apmap"); got != "e1m1.rmf" {
		t.Errorf("rmf output name = %q, want e1m1.rmf", got)
	}
}

// TestConvertedFilesAreWritable is a small guard that the produced bytes are
// what a caller can put straight on disk and read back.
func TestConvertedFilesAreWritable(t *testing.T) {
	output, result := convertOrFail(t, "fixture.map", []byte(q1MapText),
		"q1map-to-apmap", Options{Validate: false})

	path := filepath.Join(t.TempDir(), result.OutputName)
	if err := os.WriteFile(path, output, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apmap.Decode(string(data), path); err != nil {
		t.Fatalf("the written document does not read back: %v", err)
	}
}
