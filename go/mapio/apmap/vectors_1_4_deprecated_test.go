package apmap_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/internal/apmaptest"
)

// THE DEPRECATED 1.4 CORPUS — AULIBS ts/apmap-schema/deprecated/1.4 — read the way every legacy
// document is: validated against the frozen 1.4 contract it declares, then promoted by its header
// alone. 1.4 stopped being current when 1.5 added the Quake III surfaces (Q3_001); it did not stop
// being readable, and its own answers — what it accepted, what it refused and for which rule — are
// still the ones a 1.4 document gets.
//
// SEM-G-5 is a warning on a legacy-declared document (semantics_groups.go), so the one
// semantic_invalid vector naming it is checked as a warning, not a refusal.

const deprecated14 = "1.4"

type legacyIndex struct {
	APMapVersion string `json:"apmap_version"`
	Valid        []struct {
		File string `json:"file"`
	} `json:"valid"`
	Invalid []struct {
		File string `json:"file"`
		Rule string `json:"rule"`
	} `json:"invalid"`
	SemanticInvalid []struct {
		File string `json:"file"`
		Rule string `json:"rule"`
	} `json:"semantic_invalid"`
}

func deprecatedCorpus(t *testing.T, version string) (string, legacyIndex) {
	t.Helper()
	loadCurrentIndex(t) // skips without the contract package; fails on an unsynced bundle
	if !apmap.IsReadableVersion(version) || apmap.IsCurrentVersion(version) {
		t.Skipf("APMap %s is not a readable legacy version of this build", version)
	}
	root := filepath.Join(apmaptest.PackageRoot(), "deprecated", version)
	data, err := os.ReadFile(filepath.Join(root, "test-vectors", "index.json"))
	if err != nil {
		t.Fatalf("reading the deprecated %s index: %v", version, err)
	}
	var index legacyIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	if index.APMapVersion != version {
		t.Fatalf("the deprecated %s corpus declares %s", version, index.APMapVersion)
	}
	if len(index.Valid) == 0 || len(index.Invalid) == 0 || len(index.SemanticInvalid) == 0 {
		t.Fatalf("the deprecated %s index has an empty section", version)
	}
	return root, index
}

func readLegacy(t *testing.T, root, kind, file string) (string, *apmap.Object) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "test-vectors", kind, file))
	if err != nil {
		t.Fatal(err)
	}
	return string(data), mustDecodeText(t, string(data))
}

func TestTheDeprecated14ValidVectorsAreValidUnderTheirOwnContractAndPromote(t *testing.T) {
	root, index := deprecatedCorpus(t, deprecated14)
	for _, entry := range index.Valid {
		t.Run(entry.File, func(t *testing.T) {
			text, document := readLegacy(t, root, "valid", entry.File)
			if version := apmap.DeclaredVersion(document); version != deprecated14 {
				t.Fatalf("declares %s", version)
			}
			if problems := schemaProblems(t, document); len(problems) > 0 {
				t.Fatalf("1.4 schema problems: %s", strings.Join(problems, "; "))
			}
			if problems := apmap.SemanticProblems(document); len(problems) > 0 {
				t.Fatalf("semantic problems: %s", strings.Join(problems, "; "))
			}
			if err := apmap.ValidateDocument(document, apmap.ValidateOptions{Source: entry.File}); err != nil {
				t.Fatalf("ValidateDocument: %v", err)
			}
			// A 1.4 document read and written again is still byte-exact at its own version.
			if encoded, err := apmap.Encode(document); err != nil || encoded != text {
				t.Fatalf("decode -> encode changed the bytes (err %v)", err)
			}
			promoted := mustDecodeText(t, apmaptest.PromoteToCurrent(text))
			if err := apmap.ValidateDocument(promoted, apmap.ValidateOptions{Source: "promoted"}); err != nil {
				t.Fatalf("promoted by its header, it is not a valid current document: %v", err)
			}
			if surfaces := apmap.FindQuake3Surfaces(promoted); len(surfaces) > 0 {
				t.Fatalf("promotion invented a Quake III surface: %v", surfaces)
			}
		})
	}
}

func TestTheDeprecated14InvalidVectorsAreRefusedByTheirOwnContract(t *testing.T) {
	root, index := deprecatedCorpus(t, deprecated14)
	for _, entry := range index.Invalid {
		t.Run(entry.File, func(t *testing.T) {
			_, document := readLegacy(t, root, "invalid", entry.File)
			if entry.Rule == "VER-1" {
				var versionError *apmap.VersionError
				if err := apmap.GateDeclaredVersion(document); !errors.As(err, &versionError) {
					t.Fatalf("GateDeclaredVersion = %v, want a *VersionError", err)
				}
				return
			}
			if problems := schemaProblems(t, document); len(problems) == 0 {
				t.Fatalf("%s (%s) passed the 1.4 schema", entry.File, entry.Rule)
			}
		})
	}
}

func TestTheDeprecated14SemanticInvalidVectorsAreRefusedForExactlyTheirRule(t *testing.T) {
	root, index := deprecatedCorpus(t, deprecated14)
	for _, entry := range index.SemanticInvalid {
		t.Run(entry.File, func(t *testing.T) {
			_, document := readLegacy(t, root, "semantic-invalid", entry.File)
			if problems := schemaProblems(t, document); len(problems) > 0 {
				t.Fatalf("the 1.4 schema refused a semantic_invalid vector: %s", strings.Join(problems, "; "))
			}
			problems := apmap.SemanticProblems(document)
			warnings := apmap.SemanticWarnings(document)
			if entry.Rule == "SEM-G-5" {
				// Legacy containment is advice, never a refusal.
				if len(problems) > 0 || strings.Join(ruleSet(warnings), ",") != "SEM-G-5" {
					t.Fatalf("problems %v, warnings %v; want exactly one SEM-G-5 warning", problems, warnings)
				}
				return
			}
			if got := ruleSet(problems); strings.Join(got, ",") != entry.Rule {
				t.Fatalf("refused for %v, want exactly %s:\n  %s", got, entry.Rule, strings.Join(problems, "\n  "))
			}
		})
	}
}

// A document that declares 1.4 is held to 1.4: its frozen contract has no patch, no
// brush_primitives projection and no e<n>.p<n> path, so the same Quake III document that is valid
// as 1.5 is refused as 1.4 — promotion never runs backwards, and nothing launders a surface into a
// version that cannot say it.
func TestA14DocumentCannotCarryAQuake3Surface(t *testing.T) {
	deprecatedCorpus(t, deprecated14)
	for _, file := range []string{"q3-patch-minimal.apmap", "q3-brush-primitives.apmap", "q3-patch-derived-ids.apmap"} {
		t.Run(file, func(t *testing.T) {
			text, current := readVector(t, "valid", file)
			if err := apmap.ValidateDocument(current, apmap.ValidateOptions{Source: file}); err != nil {
				t.Fatalf("the 1.5 vector is not valid: %v", err)
			}
			relabelled := strings.Replace(text, `"apmap_version": "`+apmap.CurrentVersion()+`"`,
				`"apmap_version": "`+deprecated14+`"`, 1)
			relabelled = strings.Replace(relabelled, "/apmap/"+apmap.CurrentVersion()+"/", "/apmap/"+deprecated14+"/", 1)
			legacy := mustDecodeText(t, relabelled)
			if apmap.DeclaredVersion(legacy) != deprecated14 {
				t.Fatal("the relabel did not take")
			}
			if problems := schemaProblems(t, legacy); len(problems) == 0 {
				t.Fatal("a Quake III surface declared as 1.4 passed the frozen 1.4 schema")
			}
		})
	}
}
