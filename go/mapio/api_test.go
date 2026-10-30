package mapio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/internal/apmaptest"
)

// withoutEnvironment makes the process look like a user's machine: no contract set, no mapper
// configuration, no schema directory that would resolve to anything.
func withoutEnvironment(t *testing.T) {
	t.Helper()
	previous, err := apmap.CurrentContract()
	if err != nil {
		t.Fatal(err)
	}
	apmap.SetContract(nil)
	t.Cleanup(func() { apmap.SetContract(previous) })
	t.Setenv("MAPPER_ROOT", "")
	t.Setenv("APMAP_SCHEMA_DIR", filepath.Join(t.TempDir(), "absent"))
}

func oneCube(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("mapfile", "testdata", "known_good", "one_cube.map"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestEveryDirectionRunsFromTheEmbeddedContractAlone is the standalone guarantee: a binary that links
// this package converts with nothing configured and no process default set.
func TestEveryDirectionRunsFromTheEmbeddedContractAlone(t *testing.T) {
	withoutEnvironment(t)
	ctx := context.Background()
	cube := oneCube(t)

	toAPMap := func(name string) []byte {
		direction, _ := LookupDirection(name)
		output, result, err := Convert(ctx, direction, "cube.map", cube, Options{Validate: true})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !result.Validated {
			t.Fatalf("%s: validation was asked for and did not run", name)
		}
		return output
	}
	q1 := toAPMap("q1map-to-apmap")
	q2 := toAPMap("q2map-to-apmap")

	for name, input := range map[string][]byte{
		"apmap-to-q1map": q1, "apmap-to-q2map": q2, "apmap-to-q2rmf": q2,
		"apmap-to-q3map": readQ3Fixture(t, "q3004-room.apmap"),
	} {
		direction, _ := LookupDirection(name)
		output, _, err := Convert(ctx, direction, "room.apmap", input, Options{Validate: true})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(output) == 0 {
			t.Fatalf("%s produced nothing", name)
		}
	}

	rmfDirection, _ := LookupDirection("apmap-to-q2rmf")
	rmf, _, err := Convert(ctx, rmfDirection, "cube.apmap", q2, Options{Validate: true})
	if err != nil {
		t.Fatal(err)
	}
	back, _ := LookupDirection("q2rmf-to-apmap")
	if _, _, err = Convert(ctx, back, "cube.rmf", rmf, Options{Validate: true}); err != nil {
		t.Fatalf("q2rmf-to-apmap: %v", err)
	}
}

// TestAnExplicitContractIsTheOneUsed: the option decides, not the process default. The bundle here
// holds only the current schema, so a legacy document it does not hold is refused even though the
// process default reads it.
func TestAnExplicitContractIsTheOneUsed(t *testing.T) {
	embedded, err := apmap.EmbeddedContract()
	if err != nil {
		t.Fatal(err)
	}
	if len(embedded.Readable) < 2 {
		t.Fatal("the embedded contract holds no legacy version to refuse")
	}
	name := "apmap-" + embedded.Version + ".schema.json"
	schema, err := os.ReadFile(filepath.Join(apmaptest.SchemaDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := apmap.ParseContract(fstest.MapFS{name: {Data: schema}}, "narrow")
	if err != nil {
		t.Fatal(err)
	}

	legacyVersion := embedded.ReadableVersions()[0]
	data := []byte(fmt.Sprintf(`{"apmap_version": %q, "entities": []}`, legacyVersion))
	direction, _ := LookupDirection("apmap-to-q1map")
	_, _, err = Convert(context.Background(), direction, "old.apmap", data, Options{Contract: narrow})
	if code := RefusalOf(err).Code; code != RefusalVersion {
		t.Fatalf("a version the named contract does not hold: %v (%s), want %s", err, code, RefusalVersion)
	}
}

// TestDecodingNeverValidates: a document the contract would refuse decodes without complaint, and the
// verdict arrives only from the call that asks for one.
func TestDecodingNeverValidates(t *testing.T) {
	broken := []byte(`{"apmap_version": "0.0", "entities": "not a list"}`)
	if _, err := apmap.DecodeJSON(broken); err != nil {
		t.Fatalf("decoding refused a document: %v", err)
	}
	report, err := Validate(context.Background(), "broken.apmap", broken, ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || report.Unreadable == "" || report.DeclaredVersion != "0.0" {
		t.Fatalf("report = %+v, want an unreadable-version verdict", report)
	}
}

func TestValidateReportsBothLayersAsAValue(t *testing.T) {
	ctx := context.Background()
	direction, _ := LookupDirection("q1map-to-apmap")
	good, _, err := Convert(ctx, direction, "cube.map", oneCube(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	report, err := Validate(ctx, "cube.apmap", good, ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.Unreadable != "" {
		t.Fatalf("a produced document did not validate: %+v", report)
	}

	var document map[string]any
	if err = json.Unmarshal(good, &document); err != nil {
		t.Fatal(err)
	}
	document["surplus_member"] = true
	document["document_id"] = "!"
	mutated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	report, err = Validate(ctx, "mutated.apmap", mutated, ValidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || len(report.SchemaProblems) == 0 {
		t.Fatalf("report = %+v, want schema problems", report)
	}

	report, err = Validate(ctx, "junk.apmap", []byte("{"), ValidateOptions{})
	if err != nil || report.Valid || report.Unreadable == "" {
		t.Fatalf("junk: report %+v, err %v", report, err)
	}
}

// TestCancellationStopsEveryDirectionPartWay asks the context to fail at each successive look, so a
// direction that ignored it after its first stage would finish and fail here.
func TestCancellationStopsEveryDirectionPartWay(t *testing.T) {
	ctx := context.Background()
	// A non-zero origin: RMF drops an entity with neither brushes nor a position.
	big := []byte(strings.Repeat(`{ "classname" "info_null" "origin" "8 8 8" }`+"\n", 300))
	big = append(append([]byte{}, oneCube(t)...), big...)

	direction := func(name string) Direction {
		resolved, ok := LookupDirection(name)
		if !ok {
			t.Fatalf("no direction %s", name)
		}
		return resolved
	}
	apmapText, _, err := Convert(ctx, direction("q2map-to-apmap"), "big.map", big, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rmfBytes, _, err := Convert(ctx, direction("apmap-to-q2rmf"), "big.apmap", apmapText, Options{})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string][]byte{
		"q1map-to-apmap": big, "q2map-to-apmap": big, "q2rmf-to-apmap": rmfBytes,
		"apmap-to-q1map": apmapText, "apmap-to-q2map": apmapText, "apmap-to-q2rmf": apmapText,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			stopped := 0
			for after := 1; after < 400; after += 7 {
				counting := &countingContext{Context: ctx, after: int64(after)}
				output, _, err := Convert(counting, direction(name), "big", input, Options{Validate: true})
				if err == nil {
					break
				}
				stopped++
				if output != nil {
					t.Fatalf("a cancelled conversion returned output (after %d)", after)
				}
				if !errors.Is(err, context.Canceled) || RefusalOf(err).Code != RefusalCancelled {
					t.Fatalf("after %d looks: %v (%s), want a cancellation", after, err, RefusalOf(err).Code)
				}
			}
			if stopped < 3 {
				t.Fatalf("%s stopped at only %d distinct points; the context is not being read", name, stopped)
			}
		})
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for name, input := range cases {
		if _, _, err := Convert(cancelled, direction(name), "big", input, Options{}); RefusalOf(err).Code != RefusalCancelled {
			t.Fatalf("%s with a dead context: %v", name, err)
		}
	}
}

func TestRefusalsAreTyped(t *testing.T) {
	ctx := context.Background()
	lookup := func(name string) Direction { resolved, _ := LookupDirection(name); return resolved }
	room := readQ3Fixture(t, "q3004-room.apmap")

	_, _, game := Convert(ctx, lookup("apmap-to-q1map"), "room.apmap", room, Options{})
	_, _, version := Convert(ctx, lookup("apmap-to-q1map"), "x.apmap", []byte(`{"apmap_version": "0.0"}`), Options{})
	_, _, q3 := Convert(ctx, lookup("apmap-to-q3map"), "x.apmap", []byte(`{"game": "quake1"}`), Options{})
	_, _, syntax := Convert(ctx, lookup("q1map-to-apmap"), "x.map", []byte("{ not a map"), Options{})
	_, _, invalid := Convert(ctx, lookup("apmap-to-q1map"), "x.apmap", []byte(`{`), Options{})
	_, _, tooLarge := Convert(ctx, lookup("apmap-to-q3map"), "x.apmap", make([]byte, MaxQuake3MapInputBytes+1), Options{})

	for _, test := range []struct {
		name string
		err  error
		want RefusalCode
	}{
		{"game", game, RefusalGame}, {"version", version, RefusalVersion}, {"q3", q3, RefusalQuake3Map},
		{"syntax", syntax, RefusalInvalidInput}, {"invalid", invalid, RefusalInvalidInput},
		{"too large", tooLarge, RefusalTooLarge},
	} {
		if test.err == nil {
			t.Errorf("%s: no refusal", test.name)
			continue
		}
		refusal := RefusalOf(test.err)
		if refusal.Code != test.want || refusal.Message != test.err.Error() {
			t.Errorf("%s: %+v, want %s", test.name, refusal, test.want)
		}
	}
	if detail := RefusalOf(q3).Detail; detail == "" {
		t.Error("a Quake III refusal lost the writer's own code")
	}
}

func TestWarningsCarryACodeAndTheirExactText(t *testing.T) {
	direction, _ := LookupDirection("q2map-to-apmap")
	_, result, err := Convert(context.Background(), direction, "w.map", []byte("{\n\"classname\" \"worldspawn\"\n{\n}\n}\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
	warning := result.Warnings[0]
	if warning.Code != WarningEmptyBrushBlocksSkipped || warning.String() != "empty_brush_blocks_skipped: 1" {
		t.Fatalf("warning = %+v", warning)
	}
}

// TestTheManifestIsHandedBackReadyToWrite: the caller writes these bytes under this name and never
// has to know how a manifest is encoded.
func TestTheManifestIsHandedBackReadyToWrite(t *testing.T) {
	direction, _ := LookupDirection("apmap-to-q3map")
	output, result, err := Convert(context.Background(), direction, "room.apmap", readQ3Fixture(t, "q3004-room.apmap"), Options{Validate: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.ManifestName != "room.q3map-manifest.json" {
		t.Errorf("manifest name = %q", result.ManifestName)
	}
	want, err := EncodeManifest(result.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.ManifestBytes) != string(want) {
		t.Error("the manifest bytes are not the encoded manifest")
	}
	if result.Manifest.Output.Bytes != len(output) || result.Manifest.Source.Name != "room.apmap" {
		t.Errorf("manifest identity = %+v / %+v", result.Manifest.Source, result.Manifest.Output)
	}

	other, _ := LookupDirection("q1map-to-apmap")
	_, plain, err := Convert(context.Background(), other, "cube.map", oneCube(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.ManifestName != "" || plain.ManifestBytes != nil {
		t.Errorf("a direction without a manifest returned one: %+v", plain)
	}
}

func TestCapabilitiesAreDerivedFromTheContract(t *testing.T) {
	capabilities, err := CapabilitiesOf(nil)
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := apmap.EmbeddedContract()
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.APMapCurrent != embedded.Version || len(capabilities.APMapReadable) != len(embedded.Readable) {
		t.Fatalf("capabilities = %+v", capabilities)
	}
	if len(capabilities.Directions) != len(Directions()) {
		t.Fatalf("directions = %+v", capabilities.Directions)
	}
	for _, direction := range capabilities.Directions {
		if direction.From == FormatQ3Map {
			t.Fatalf("a Quake III importer is advertised: %+v", direction)
		}
	}
}
