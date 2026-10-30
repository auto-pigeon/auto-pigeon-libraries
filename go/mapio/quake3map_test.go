package mapio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
)

// apmap-to-q3map (Q3_004).
//
// testdata/q3map holds the proof, and PINS.json pins every byte of it:
//
//   - q3004-*.map        self-authored synthetic Quake III sources (a sealed room in classic,
//                        brushDef and Valve 220 syntax, and the classic room with a 5x3 arch patch);
//   - q3004-*.apmap      those sources imported by the EDITOR's (AUP's) Quake III importer;
//   - *.golden.map       the EDITOR's `mapTextFromApmap` output for each — the reference this
//                        writer must reproduce byte for byte;
//   - aulibs/            AULIBS' own valid 1.5 vectors, copied at the pinned commit, and the editor's
//                        output for each;
//   - number-spellings.json  the editor's two number spellings over every class of number;
//   - *.q3map-manifest.json  this writer's manifests (the one kind of file here AUE produces).
//
// scripts/q3map-parity.sh regenerates the editor-side files from an AUP checkout and fails if any
// differs from what is pinned, so the goldens are the editor's bytes, never this writer's.

var updateQuake3Manifests = flag.Bool("update-q3map-manifests", false,
	"rewrite testdata/q3map/*.q3map-manifest.json from this writer (the .map goldens are never rewritten here)")

const q3mapTestdata = "testdata/q3map"

func q3Direction(t *testing.T) Direction {
	t.Helper()
	direction, ok := LookupDirection("apmap-to-q3map")
	if !ok {
		t.Fatal("apmap-to-q3map is not registered")
	}
	return direction
}

func readQ3Fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(q3mapTestdata, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// q3Pins is PINS.json: provenance and the digest of every fixture.
type q3Pins struct {
	Comment    string            `json:"$comment"`
	Provenance map[string]string `json:"provenance"`
	Files      map[string]string `json:"files"`
}

func TestQuake3FixturesMatchTheirPins(t *testing.T) {
	var pins q3Pins
	if err := json.Unmarshal(readQ3Fixture(t, "PINS.json"), &pins); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(q3mapTestdata, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() == "PINS.json" {
			return err
		}
		relative, _ := filepath.Rel(q3mapTestdata, path)
		relative = filepath.ToSlash(relative)
		seen[relative] = true
		want, ok := pins.Files[relative]
		if !ok {
			t.Errorf("%s is not pinned in PINS.json", relative)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		if got := hex.EncodeToString(digest[:]); got != want {
			t.Errorf("%s: sha256 %s, pinned %s — a golden changed; regenerate it with scripts/q3map-parity.sh, never by hand", relative, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name := range pins.Files {
		if !seen[name] {
			t.Errorf("PINS.json pins %s, which is not in testdata/q3map", name)
		}
	}
}

// The heart of the parity claim: for every fixture both writers accept, the bytes are identical.
func TestQuake3MapIsByteIdenticalToTheEditorsWriter(t *testing.T) {
	cases := []string{
		"q3004-room", "q3004-room-patch", "q3004-room-brushdef", "q3004-room-valve220",
		"aulibs/q3-patch-minimal", "aulibs/q3-brush-primitives", "aulibs/q3-patch-max-grid",
		"aulibs/q3-patch-derived-ids", "aulibs/q3-patch-in-nested-group",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			input := readQ3Fixture(t, name+".apmap")
			golden := readQ3Fixture(t, name+".golden.map")
			for _, validate := range []bool{true, false} {
				output, result, err := Convert(context.Background(), q3Direction(t), filepath.Base(name)+".apmap", input, Options{Validate: validate})
				if err != nil {
					t.Fatalf("validate=%v: %v", validate, err)
				}
				if !bytes.Equal(output, golden) {
					t.Fatalf("validate=%v: the output differs from the editor's writer\n--- got ---\n%s\n--- want ---\n%s", validate, output, golden)
				}
				if result.Manifest == nil {
					t.Fatal("no manifest")
				}
			}
		})
	}
}

func TestQuake3MapManifestsAreDeterministicAndPinned(t *testing.T) {
	for _, name := range []string{"q3004-room", "q3004-room-patch", "q3004-room-brushdef", "q3004-room-valve220"} {
		t.Run(name, func(t *testing.T) {
			input := readQ3Fixture(t, name+".apmap")
			var first []byte
			for run := 0; run < 2; run++ {
				_, result, err := Convert(context.Background(), q3Direction(t), name+".apmap", input, Options{Validate: true})
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := EncodeManifest(result.Manifest)
				if err != nil {
					t.Fatal(err)
				}
				if run == 0 {
					first = encoded
				} else if !bytes.Equal(first, encoded) {
					t.Fatal("two conversions of one document produced different manifests")
				}
			}
			path := filepath.Join(q3mapTestdata, name+".q3map-manifest.json")
			if *updateQuake3Manifests {
				if err := os.WriteFile(path, first, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if want := readQ3Fixture(t, name+".q3map-manifest.json"); !bytes.Equal(first, want) {
				t.Fatalf("manifest differs from %s\n--- got ---\n%s", path, first)
			}
		})
	}
}

func TestQuake3MapManifestContent(t *testing.T) {
	output, result, err := Convert(context.Background(), q3Direction(t), "q3004-room-patch.apmap", readQ3Fixture(t, "q3004-room-patch.apmap"), Options{Validate: true})
	if err != nil {
		t.Fatal(err)
	}
	manifest := result.Manifest
	if manifest.Document.Game != "quake3" || manifest.Document.MapDialect != "quake3_extended" ||
		manifest.Document.APMapVersion != "1.5" || manifest.Document.DocumentID != "q3004-room-patch:full_map" {
		t.Errorf("document identity = %+v", manifest.Document)
	}
	if manifest.BrushSyntax != "classic" {
		t.Errorf("brush_syntax = %q", manifest.BrushSyntax)
	}
	if got := manifest.Counts; got.Entities != 4 || got.Brushes != 6 || got.Faces != 36 || got.Patches != 1 || got.PatchControlPoints != 15 {
		t.Errorf("counts = %+v", got)
	}
	names := []string{}
	for _, shader := range manifest.Shaders {
		names = append(names, shader.Name+"="+shader.Written)
	}
	if got, want := strings.Join(names, ","), "textures/q3004/arch=q3004/arch,textures/q3004/ceiling=q3004/ceiling,textures/q3004/floor=q3004/floor,textures/q3004/wall=q3004/wall"; got != want {
		t.Errorf("shaders = %s, want %s", got, want)
	}
	if len(manifest.Sounds) != 1 || manifest.Sounds[0].Name != "sound/q3004/hum.wav" || manifest.Sounds[0].Keys[0] != "noise" {
		t.Errorf("sounds = %+v", manifest.Sounds)
	}
	if len(manifest.Models) != 0 || len(manifest.Warnings) != 0 {
		t.Errorf("models = %+v warnings = %+v", manifest.Models, manifest.Warnings)
	}
	sourceDigest := sha256.Sum256(readQ3Fixture(t, "q3004-room-patch.apmap"))
	outputDigest := sha256.Sum256(output)
	if manifest.Source.SHA256 != hex.EncodeToString(sourceDigest[:]) || manifest.Output.SHA256 != hex.EncodeToString(outputDigest[:]) ||
		manifest.Output.Name != "q3004-room-patch.map" || manifest.Output.Bytes != len(output) {
		t.Errorf("source/output identity = %+v / %+v", manifest.Source, manifest.Output)
	}
	if got := ManifestName(q3Direction(t), "dir/q3004-room-patch.apmap"); got != "q3004-room-patch.q3map-manifest.json" {
		t.Errorf("ManifestName = %q", got)
	}
}

// The source bytes are never written to.
func TestQuake3MapLeavesTheSourceUntouched(t *testing.T) {
	input := readQ3Fixture(t, "q3004-room-patch.apmap")
	before := append([]byte(nil), input...)
	if _, _, err := Convert(context.Background(), q3Direction(t), "x.apmap", input, Options{Validate: true}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, before) {
		t.Fatal("the conversion modified its input")
	}
}

// mutate returns the room fixture with one textual substitution — the fixture's canonical text is
// stable, so a replacement is the most legible way to build each negative case.
func mutate(t *testing.T, name, old, replacement string, count int) []byte {
	t.Helper()
	text := string(readQ3Fixture(t, name))
	if !strings.Contains(text, old) {
		t.Fatalf("%s does not contain %q", name, old)
	}
	return []byte(strings.Replace(text, old, replacement, count))
}

// Every refusal is whole: no bytes, no manifest, a stable code naming the object. Validation is
// switched off where the schema or SEM rules would answer first, so the WRITER's own gate is what
// is proved; each case also states what the default (validated) path says.
func TestQuake3MapRefusals(t *testing.T) {
	type refusal struct {
		name     string
		input    []byte
		validate bool
		// code is the Quake3MapError code, "validation" for a ValidationError, or "" for a plain error.
		code string
		// contains is a substring of the message: the object id, a path, a number.
		contains string
	}
	room := "q3004-room.apmap"
	patch := "q3004-room-patch.apmap"
	cases := []refusal{
		{"quake1 document to the Quake III writer", readQ3Fixture(t, "aulibs/no-1-5-features.apmap"), true,
			apmap.Q3MapCodeGameUnsupported, "unsupported game \"quake1\""},
		{"game missing", mutate(t, room, `"game": "quake3",`, ``, 1), false, apmap.Q3MapCodeGameUnsupported, "unsupported game"},
		{"future apmap version", mutate(t, room, `"apmap_version": "1.5"`, `"apmap_version": "1.6"`, 1), false,
			apmap.Q3MapCodeVersionUnsupported, "1.6"},
		{"future apmap version, validated", mutate(t, room, `"apmap_version": "1.5"`, `"apmap_version": "9.0"`, 1), true,
			apmap.Q3MapCodeVersionUnsupported, "9.0"},
		{"quake3 before 1.5", mutate(t, room, `"apmap_version": "1.5"`, `"apmap_version": "1.4"`, 1), false,
			apmap.Q3MapCodeVersionUnsupported, "gives \"quake3\" no meaning"},
		{"a Quake 1 dialect", mutate(t, room, `"map_dialect": "quake3_extended"`, `"map_dialect": "quake_standard"`, 1), false,
			apmap.Q3MapCodeDialectUnsupported, "quake_standard"},
		{"non-finite plane point", mutate(t, room, `-272,`, `-1e400,`, 1), false, apmap.Q3MapCodeNumber, "non-finite"},
		{"non-finite plane point, validated", mutate(t, room, `-272,`, `-1e400,`, 1), true, "validation", "SEM-11"},
		{"non-finite patch point", mutate(t, patch, `0.666666666667`, `1e999`, 1), false, apmap.Q3MapCodePatchInvalid, "non-finite"},
		{"missing shader", mutate(t, room, `"texture": "q3004/wall"`, `"texture": ""`, 1), false, apmap.Q3MapCodeShaderMissing, "names no shader"},
		{"missing shader, validated", mutate(t, room, `"texture": "q3004/wall"`, `"texture": ""`, 1), true, "validation", ""},
		{"path traversal", mutate(t, room, `"texture": "q3004/wall"`, `"texture": "../../scripts/evil"`, 1), true, apmap.Q3MapCodeShaderUnsafe, "fac_"},
		{"absolute path", mutate(t, room, `"texture": "q3004/wall"`, `"texture": "/etc/passwd"`, 1), true, apmap.Q3MapCodeShaderUnsafe, "begins or ends"},
		{"backslash", mutate(t, room, `"texture": "q3004/wall"`, `"texture": "q3004\\\\wall"`, 1), true, apmap.Q3MapCodeShaderUnsafe, "holds"},
		{"comment opener", mutate(t, room, `"texture": "q3004/wall"`, `"texture": "q3004//wall"`, 1), true, apmap.Q3MapCodeShaderUnsafe, "comment"},
		{"grammar token", mutate(t, room, `"texture": "q3004/wall"`, `"texture": "q3004/wall)"`, 1), true, apmap.Q3MapCodeShaderUnsafe, "')'"},
		{"non-ASCII", mutate(t, room, `"texture": "q3004/wall"`, `"texture": "q3004/wäll"`, 1), true, apmap.Q3MapCodeShaderUnsafe, "non-ASCII"},
		{"over MAX_QPATH", mutate(t, room, `"texture": "q3004/wall"`, `"texture": "q3004/`+strings.Repeat("w", 50)+`"`, 1), true,
			apmap.Q3MapCodeShaderUnsafe, "64"},
		{"unsafe patch shader", mutate(t, patch, `"texture": "q3004/arch"`, `"texture": "../arch"`, 1), true, apmap.Q3MapCodeShaderUnsafe, "pat_"},
		{"patch even width", mutate(t, patch, `"width": 5`, `"width": 4`, 1), false, apmap.Q3MapCodePatchInvalid, "must be odd"},
		{"patch grid mismatch", mutate(t, patch, `"height": 3`, `"height": 5`, 1), false, apmap.Q3MapCodePatchInvalid, "SEM-Q-2"},
		{"face tail of two numbers", mutate(t, room, "\"tail\": [\n                0,\n                0,\n                0\n              ]",
			"\"tail\": [\n                0,\n                0\n              ]", 1), false, apmap.Q3MapCodeTail, "not 2"},
		{"unknown projection mode", mutate(t, room, `"mode": "classic"`, `"mode": "planar"`, 1), false, apmap.Q3MapCodeProjection, "planar"},
		{"mixed file syntax", mixedSyntax(t), false, apmap.Q3MapCodeMixedSyntax, "1 brushDef"},
		{"future member, validated", mutate(t, room, `"role": "full_map",`, `"role": "full_map", "q3_future": true,`, 1), true, "validation", ""},
		{"broken group reference, validated", brokenGroup(t), true, "validation", "SEM-G"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			output, result, err := Convert(context.Background(), q3Direction(t), "case.apmap", c.input, Options{Validate: c.validate})
			if err == nil {
				t.Fatalf("converted; want a refusal (%d bytes)", len(output))
			}
			if len(output) != 0 || result.Manifest != nil {
				t.Fatalf("a refusal produced %d bytes / a manifest", len(output))
			}
			var q3 *apmap.Quake3MapError
			var validation *ValidationError
			switch c.code {
			case "validation":
				if !errors.As(err, &validation) {
					t.Fatalf("err = %v, want a ValidationError", err)
				}
			default:
				if !errors.As(err, &q3) || q3.Code != c.code {
					t.Fatalf("err = %v, want code %s", err, c.code)
				}
			}
			if c.contains != "" && !strings.Contains(err.Error(), c.contains) {
				t.Fatalf("err = %v, want it to say %q", err, c.contains)
			}
		})
	}
}

// The prompt's malicious/missing-material fixture: one face names a path-traversal shader and
// another names none. Validated, the schema refuses the empty name; unvalidated, the writer
// refuses the traversal first, naming the face id. Nothing is written either way.
func TestQuake3MapMaliciousMissingMaterialFixture(t *testing.T) {
	input := readQ3Fixture(t, "q3004-malicious-missing-material.apmap")
	_, _, err := Convert(context.Background(), q3Direction(t), "m.apmap", input, Options{Validate: true})
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("validated: err = %v, want the schema refusal of the empty shader", err)
	}
	output, _, err := Convert(context.Background(), q3Direction(t), "m.apmap", input, Options{Validate: false})
	var q3 *apmap.Quake3MapError
	if !errors.As(err, &q3) || q3.Code != apmap.Q3MapCodeShaderUnsafe || q3.ObjectID == "" ||
		q3.Path != "entities[0].content[2].faces[0]" || len(output) != 0 {
		t.Fatalf("unvalidated: err = %v (%+v), want q3map_shader_unsafe at entities[0].content[2].faces[0]", err, q3)
	}
}

func mixedSyntax(t *testing.T) []byte {
	t.Helper()
	// One brushDef brush (all six faces) among classic ones: Q3Map2 reads one syntax per file.
	var room map[string]any
	decoder := json.NewDecoder(bytes.NewReader(readQ3Fixture(t, "q3004-room.apmap")))
	decoder.UseNumber()
	if err := decoder.Decode(&room); err != nil {
		t.Fatal(err)
	}
	content := room["entities"].([]any)[0].(map[string]any)["content"].([]any)
	for _, raw := range content {
		item := raw.(map[string]any)
		if item["kind"] != "brush" {
			continue
		}
		for _, face := range item["faces"].([]any) {
			face.(map[string]any)["projection"] = map[string]any{"mode": "brush_primitives",
				"matrix": []any{[]any{0.0078125, 0, 0}, []any{0, 0.0078125, 0}}}
			delete(face.(map[string]any), "tail")
		}
		break
	}
	encoded, err := json.Marshal(room)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func brokenGroup(t *testing.T) []byte {
	t.Helper()
	text := string(readQ3Fixture(t, "q3004-room-patch.apmap"))
	index := strings.Index(text, `"groups": []`)
	if index < 0 {
		t.Fatal("the fixture has no empty groups member")
	}
	group := `"groups": [{"group_id": "grp_0000000000000001", "name": "arch", "members": [{"kind": "patch", "patch_id": "pat_ffffffffffffffff"}]}]`
	return []byte(text[:index] + group + text[index+len(`"groups": []`):])
}

// A Quake III document sent to a Quake 1 / Quake 2 direction is refused by game — the other half
// of "a Q1 APMap accidentally routed to the Q3 converter".
func TestQuake3DocumentToAQuake1DirectionIsRefused(t *testing.T) {
	for _, name := range []string{"apmap-to-q1map", "apmap-to-q2map", "apmap-to-q2rmf"} {
		direction, _ := LookupDirection(name)
		output, _, err := Convert(context.Background(), direction, "room.apmap", readQ3Fixture(t, "q3004-room.apmap"), Options{Validate: true})
		var game *GameError
		if !errors.As(err, &game) || len(output) != 0 {
			t.Fatalf("%s: err = %v (%d bytes), want the game refusal", name, err, len(output))
		}
	}
}

// countingContext reports Canceled from its Nth Err() call on: a cancellation that arrives while
// the writer is part-way through the document.
type countingContext struct {
	context.Context
	after int64
	calls atomic.Int64
}

func (c *countingContext) Err() error {
	if c.calls.Add(1) > c.after {
		return context.Canceled
	}
	return nil
}

func (c *countingContext) Done() <-chan struct{} { return nil }

func (c *countingContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func TestQuake3MapCancellation(t *testing.T) {
	// A document with enough content items for the writer to check its context part-way.
	large := largeQuake3Document(t, 2000)
	if _, _, err := Convert(context.Background(), q3Direction(t), "large.apmap", large, Options{Validate: false}); err != nil {
		t.Fatalf("the large document does not convert uncancelled: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, ctx := range map[string]context.Context{
		"before":     cancelled,
		"part-way":   &countingContext{Context: context.Background(), after: 3},
		"at-the-end": &countingContext{Context: context.Background(), after: 7},
	} {
		output, result, err := Convert(ctx, q3Direction(t), "large.apmap", large, Options{Validate: false})
		if !apmap.IsQuake3MapCancelled(err) || len(output) != 0 || result.Manifest != nil {
			t.Fatalf("%s: err = %v, %d bytes; want a cancelled conversion with no output", name, err, len(output))
		}
	}
}

// largeQuake3Document is the room with its worldspawn's first brush repeated n times.
func largeQuake3Document(t *testing.T, n int) []byte {
	t.Helper()
	var room map[string]any
	decoder := json.NewDecoder(bytes.NewReader(readQ3Fixture(t, "q3004-room.apmap")))
	decoder.UseNumber()
	if err := decoder.Decode(&room); err != nil {
		t.Fatal(err)
	}
	world := room["entities"].([]any)[0].(map[string]any)
	content := world["content"].([]any)
	var brush any
	for _, item := range content {
		if item.(map[string]any)["kind"] == "brush" {
			brush = item
			break
		}
	}
	for i := 0; i < n; i++ {
		content = append(content, brush)
	}
	world["content"] = content
	encoded, err := json.Marshal(room)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestQuake3MapInputIsBounded(t *testing.T) {
	huge := make([]byte, MaxQuake3MapInputBytes+1)
	_, _, err := Convert(context.Background(), q3Direction(t), "huge.apmap", huge, Options{Validate: false})
	if err == nil || !strings.Contains(err.Error(), "converts documents up to") {
		t.Fatalf("err = %v, want the size bound", err)
	}
}

// Quake 1 tool names and WAD-style names are warned about, never renamed; model keys are listed.
func TestQuake3MapWarnsAndListsAssetsWithoutSubstituting(t *testing.T) {
	input := mutate(t, "q3004-room.apmap", `"texture": "q3004/wall"`, `"texture": "clip"`, 1)
	input = []byte(strings.Replace(string(input), `"texture": "q3004/wall"`, `"texture": "*lava1"`, 1))
	input = []byte(strings.Replace(string(input), `"value": "target_speaker"`, `"value": "misc_model"
        },
        {
          "kind": "property",
          "key": "model",
          "value": "models/q3004/crate.md3"`, 1))
	output, result, err := Convert(context.Background(), q3Direction(t), "w.apmap", input, Options{Validate: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), " clip ") || !strings.Contains(string(output), " *lava1 ") {
		t.Fatal("a name was substituted")
	}
	codes := []string{}
	for _, warning := range result.Manifest.Warnings {
		codes = append(codes, warning.Code+"@"+warning.Path)
	}
	sort.Strings(codes)
	if got, want := strings.Join(codes, ","), "q3map_quake1_tool_name@textures/clip,q3map_shader_outside_directory@textures/*lava1"; got != want {
		t.Errorf("warnings = %s, want %s", got, want)
	}
	if len(result.Warnings) != 2 || !strings.HasPrefix(result.Warnings[0].Text, "q3map_warning: ") {
		t.Errorf("Result.Warnings = %v", result.Warnings)
	}
	if len(result.Manifest.Models) != 1 || result.Manifest.Models[0].Name != "models/q3004/crate.md3" {
		t.Errorf("models = %+v", result.Manifest.Models)
	}
}
