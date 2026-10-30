package apmap

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// THE CONTRACT BUNDLE — §6 backward reads, §8 failure injection.
//
//	READ      current + every supported legacy format
//	WRITE     current only
//	VALIDATE  with the schema matching the document being read
//
// AUE is the stack's most important backward reader: the generated prefab corpora on disk are 1.0
// and there are hundreds of them. PREPROD-01B's current-only runtime refused every one, and the
// remedy on offer was regenerating a corpus to change a format header.
//
// Every test here restores the real contract afterwards, because LoadContract writes process-global
// state that TestMain established for the whole package.

// withBundle builds a bundle directory from an explicit file list and loads it, restoring the real
// contract when the test ends. An empty string as content means "copy the real file".
func withBundle(t *testing.T, files map[string]string) error {
	t.Helper()
	previous, _ := CurrentContract()
	t.Cleanup(func() {
		if previous != nil {
			SetContract(previous)
		}
	})

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "deprecated"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := LoadContract(root)
	return err
}

func realSchema(t *testing.T, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(currentSchemaDir(), relative))
	if err != nil {
		t.Skipf("the contract package is not beside this checkout: %v", err)
	}
	return string(data)
}

// currentSchemaName is the current contract's file name, read off the loaded bundle.
//
// It used to be typed as "apmap-1.1.schema.json". `AUP 90` promoted 1.3 and every test built on
// that literal stopped finding a file — and because the lookup skips rather than fails, the whole
// of §8 below reported PASS with every subtest skipped for a year's worth of promotions. Nothing
// in this file names a version any more: the names come from the bundle, so a promotion moves them
// with it.
func currentSchemaName(t *testing.T) string {
	t.Helper()
	contract, err := CurrentContract()
	if err != nil {
		t.Fatal(err)
	}
	return "apmap-" + contract.Version + ".schema.json"
}

// legacySchemaName is one deprecated READ contract, bundle-relative, chosen from the bundle.
//
// The lowest readable legacy version, so the choice is deterministic rather than map-iteration
// order. A bundle with no legacy contract at all is legal (see the no-deprecated-directory test),
// and the tests that need one say so by skipping with that reason.
func legacySchemaName(t *testing.T) (relative, version string) {
	t.Helper()
	contract, err := CurrentContract()
	if err != nil {
		t.Fatal(err)
	}
	versions := make([]string, 0, len(contract.Readable))
	for readable := range contract.Readable {
		if readable != contract.Version {
			versions = append(versions, readable)
		}
	}
	if len(versions) == 0 {
		t.Skip("the loaded bundle carries no legacy contract, so there is no legacy half to fault")
	}
	sort.Strings(versions)
	name := "apmap-" + versions[0] + ".schema.json"
	return deprecatedSchemaDir + "/" + name, versions[0]
}

// healthy is the control bundle: the real current schema plus every real legacy one.
func healthy(t *testing.T) map[string]string {
	t.Helper()
	contract, err := CurrentContract()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for version := range contract.Readable {
		name := "apmap-" + version + ".schema.json"
		if version == contract.Version {
			files[name] = realSchema(t, name)
			continue
		}
		files[deprecatedSchemaDir+"/"+name] = realSchema(t, filepath.Join(deprecatedSchemaDir, name))
	}
	return files
}

// ---- what the real deployment speaks ---------------------------------------------------

func TestTheRealBundleReadsEveryLegacyVersionAndWritesOne(t *testing.T) {
	contract, err := CurrentContract()
	if err != nil {
		t.Fatal(err)
	}
	// Derived from the bundle on disk rather than written down. This block DID name "1.0,1.1,1.2"
	// and "1.2" — the one place in this repository a version was typed — on the argument that it
	// was a useful migration assertion. `AUP 90` promoted 1.3 and it was also the only thing in
	// this repository that failed: not one line of runtime code needed touching. A test that has
	// to be edited for a promotion is the same drift the architecture exists to remove, so it now
	// asserts the SHAPE — 1.0 is still readable, the current version is readable and is the one
	// written, and everything came from a filename.
	readable := ReadableVersions()
	if len(readable) < 2 {
		t.Fatalf("readable versions = %v, want the current one and at least one legacy", readable)
	}
	if !slices.Contains(readable, "1.0") {
		t.Errorf("readable versions = %v, want 1.0 among them — hundreds of packages on disk are 1.0", readable)
	}
	if !slices.Contains(readable, contract.Version) {
		t.Errorf("the current version %q is not readable, so this build could not read back what it writes",
			contract.Version)
	}
	// Derived from filenames, both halves. No version is written down anywhere in this repository.
	for version, loaded := range contract.Readable {
		if !strings.Contains(filepath.Base(loaded.Path), "apmap-"+version+".schema.json") {
			t.Errorf("APMap %s was loaded from %s, whose name does not carry that version", version, loaded.Path)
		}
	}
	// And the writer's contract is the direct child — a deprecated schema never competes for it.
	if filepath.Base(filepath.Dir(contract.Path)) == deprecatedSchemaDir {
		t.Errorf("the current contract was taken from %s", deprecatedSchemaDir)
	}
}

// ---- the version gate -------------------------------------------------------------------

// unknownVersion is a version the loaded bundle does not hold, found rather than named — see the
// comment in TestTheGateStillRefusesAVersionTheBundleDoesNotHold for why naming one goes stale.
func unknownVersion(t *testing.T) string {
	t.Helper()
	readable := ReadableVersions()
	for minor := 999; minor < 1100; minor++ {
		candidate := fmt.Sprintf("999.%d", minor)
		if !slices.Contains(readable, candidate) {
			return candidate
		}
	}
	t.Fatalf("every 999.x version is readable, which cannot be right: %v", readable)
	return ""
}

func documentDeclaring(t *testing.T, version string) Value {
	t.Helper()
	source := `{"apmap_version": "` + version + `", "document_id": "doc_x", "entities": []}`
	document, err := DecodeJSON([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestTheGateAdmitsEveryReadableVersion(t *testing.T) {
	for _, version := range ReadableVersions() {
		if err := GateDeclaredVersion(documentDeclaring(t, version)); err != nil {
			t.Errorf("APMap %s was refused at the gate: %v", version, err)
		}
	}
}

func TestTheGateStillRefusesAVersionTheBundleDoesNotHold(t *testing.T) {
	// The gate did not go away; it changed what it is a gate on. Membership of the loaded bundle,
	// not equality with the current version — so an unknown future format is refused exactly as it
	// was, and a format whose read support is retired would be too.
	//
	// The sentinel is deliberately NOT the next minor. It was "1.2" until 1.2 was promoted and
	// "1.3" until 1.3 was, and each promotion quietly turned this into a test that a real,
	// readable version was refused. `unknownVersion` picks one the loaded bundle really does not
	// hold, so the premise is checked rather than assumed.
	unknown := unknownVersion(t)
	err := GateDeclaredVersion(documentDeclaring(t, unknown))
	if err == nil {
		t.Fatalf("APMap %s was admitted", unknown)
	}
	contract, contractErr := CurrentContract()
	if contractErr != nil {
		t.Fatal(contractErr)
	}
	message := err.Error()
	// Both halves of the sentence come from the contract: what is pinned is that the refusal names
	// the whole capability, because "this build reads X and writes Y" is what tells someone holding
	// an unreadable file whether their problem is age or a typo.
	for _, want := range []string{
		unknown + " is not a supported APMap version",
		"reads " + strings.Join(ReadableVersions(), ", "),
		"writes " + contract.Version,
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal does not say %q: %s", want, message)
		}
	}
}

func TestTheGateRefusesADocumentThatNamesNoContract(t *testing.T) {
	document, err := DecodeJSON([]byte(`{"document_id": "doc_x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := GateDeclaredVersion(document); err == nil {
		t.Fatal("a document declaring no apmap_version was admitted")
	}
}

// ---- version-matched validation ---------------------------------------------------------

func TestValidateMeasuresADocumentAgainstTheContractItDeclares(t *testing.T) {
	// `broken` on a brush is a 1.1 addition; 1.0 never defined it. A document declaring 1.0 while
	// using it is mislabelled, and measuring it against the CURRENT schema — which accepts strictly
	// more — would pass it silently. This is the whole reason validation is version-matched rather
	// than always-current.
	//
	// Asserted on the schema FILES, because that is where the difference is and reading them is how
	// a person would check the claim by hand.
	legacyRelative, legacyVersion := legacySchemaName(t)
	legacyText := realSchema(t, filepath.FromSlash(legacyRelative))
	currentText := realSchema(t, currentSchemaName(t))
	if strings.Contains(legacyText, `"broken"`) {
		t.Error("the frozen 1.0 schema mentions `broken`, so the two contracts are not distinguishable here")
	}
	if !strings.Contains(currentText, `"broken"`) {
		t.Error("the current schema does not mention `broken`, so the two contracts are not distinguishable here")
	}

	// And the loader kept them apart: two versions, two schema objects, neither substituted.
	legacy, err := SchemaForVersion(legacyVersion)
	if err != nil {
		t.Fatal(err)
	}
	current, err := SchemaForVersion(CurrentVersion())
	if err != nil {
		t.Fatal(err)
	}
	if legacy == current {
		t.Fatalf("%s and the current version resolved to the same schema object", legacyVersion)
	}
	// Each says its own name, which is what makes "the schema matching the declaration" meaningful.
	if !schemaAccepts(legacy, legacyVersion) {
		t.Errorf("the schema served for %s cannot express %s", legacyVersion, legacyVersion)
	}
	if schemaAccepts(legacy, CurrentVersion()) {
		t.Errorf("the frozen %s schema accepts the current version, so it is not the frozen %s schema",
			legacyVersion, legacyVersion)
	}
}

func TestSchemaForVersionRefusesAVersionTheBundleDoesNotHold(t *testing.T) {
	if _, err := SchemaForVersion("0.9"); err == nil {
		t.Fatal("a schema was produced for a version nothing loaded")
	}
}

// ---- §8 failure injection ---------------------------------------------------------------

func TestAHealthyBundleIsTheControlAndLoads(t *testing.T) {
	want := strings.Join(ReadableVersions(), ",")
	if err := withBundle(t, healthy(t)); err != nil {
		t.Fatalf("the healthy bundle did not load: %v", err)
	}
	// Rebuilt from the real bundle's own files, so it must read back exactly what the real bundle
	// reads. The expectation is taken BEFORE the reload, because withBundle replaces the process
	// contract and taking it afterwards would compare the copy with itself.
	if got := strings.Join(ReadableVersions(), ","); got != want {
		t.Fatalf("readable = %s, want %s", got, want)
	}
}

func TestBundleFaultsAreFatal(t *testing.T) {
	current := currentSchemaName(t)
	legacyRelative, _ := legacySchemaName(t)
	legacyName := filepath.Base(legacyRelative)
	real := healthy(t)
	currentText := real[current]
	legacyText := real[legacyRelative]

	// Every file name below is derived from the loaded bundle. The two that are not — 9.9 and 0.9 —
	// are deliberately versions no schema can express, which is the fault being injected.
	for _, testCase := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"no current schema",
			map[string]string{legacyRelative: legacyText},
			"exactly one current APMap schema"},
		{"two current schemas",
			map[string]string{current: currentText, legacyName: legacyText},
			"found 2"},
		{"malformed current schema",
			map[string]string{current: "{ not json"},
			""},
		{"malformed legacy schema",
			map[string]string{current: currentText, legacyRelative: "{ not json"},
			""},
		{"a filename disagreeing with its current schema",
			map[string]string{"apmap-9.9.schema.json": currentText},
			"cannot express it"},
		{"a filename disagreeing with its legacy schema",
			map[string]string{current: currentText,
				deprecatedSchemaDir + "/apmap-0.9.schema.json": legacyText},
			"cannot express it"},
		{"one version in both halves",
			map[string]string{current: currentText,
				deprecatedSchemaDir + "/" + current: currentText},
			"declared twice"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := withBundle(t, testCase.files)
			if err == nil {
				t.Fatal("the bundle loaded")
			}
			if testCase.want != "" && !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("the refusal does not say %q: %v", testCase.want, err)
			}
		})
	}
}

func TestABundleWithNoDeprecatedDirectoryReadsTheCurrentVersionOnly(t *testing.T) {
	// Zero legacy contracts is a legal bundle, not a degraded one: it is what retiring read support
	// for every older format looks like.
	current := currentSchemaName(t)
	version := strings.TrimSuffix(strings.TrimPrefix(current, "apmap-"), ".schema.json")
	_, legacy := legacySchemaName(t)

	if err := withBundle(t, map[string]string{current: realSchema(t, current)}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ReadableVersions(), ","); got != version {
		t.Fatalf("readable = %s, want %s alone", got, version)
	}
	// The current schema's enum still listing the legacy version decides nothing: no legacy
	// contract is loaded, so there is no schema such a document could be measured against and it is
	// refused.
	if IsReadableVersion(legacy) {
		t.Errorf("%s is readable with no %s contract loaded", legacy, legacy)
	}
}
