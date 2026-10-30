package apmap

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The published APMap 1.0 test vectors are the primary correctness proof for
// the schema layer — more load-bearing than any hand-written unit test in this
// package, because they are the specification's own ground truth rather than
// this package's reading of it.
//
// Every vector is used, and the expectations are read from the corpus's own
// index.json rather than restated here, so a vector added upstream becomes a
// test case without an edit to this file. What the index says is the contract:
// `valid/index.json` claims every listed document passes schema validation and
// every semantic rule; `invalid/index.json` claims every listed document is
// rejected, and names the rule that rejects it.
//
// # The SCH/SEM split, which is the whole subtlety here
//
// "Rejected" in invalid/index.json does not mean "rejected by this layer". Six
// of the twenty-one invalid vectors cite SEM-* rules, and JSON Schema cannot
// express any of them — cross-document id uniqueness (SEM-1), reference
// integrity (SEM-2), co-occurrence of synthetic and synthetic_reason (SEM-3),
// the identity-transform requirement on a source_world frame (SEM-4), plane
// degeneracy (SEM-5), brush boundedness (SEM-6), and finiteness (SEM-11).
// Those documents are schema-valid and *must* be, and the semantics port is
// what will reject them.
//
// Asserting that split, rather than asserting "everything under invalid/
// fails", is what makes this suite meaningful. A validator that rejected the
// SEM-* vectors would be over-rejecting, and the loose assertion would call
// that a pass.

// The DEPRECATED 1.0 conformance corpus, now inside the contract package rather than under the
// data root. Its documents declare 1.0, so the ordinary runtime path refuses them at the version
// gate; this suite goes around that gate deliberately and measures each vector against the
// contract it declares, which is what the SCH-*/SEM-* rules it cites were written for. The files
// are never modified.
const vectorRoot = "test-vectors"

// vectorIndex is the shape of the corpus's index.json.
type vectorIndex struct {
	APMapVersion string `json:"apmap_version"`
	Expectation  string `json:"expectation"`
	Vectors      []struct {
		File      string `json:"file"`
		Covers    string `json:"covers"`
		Rule      string `json:"rule"`
		Violation string `json:"violation"`
	} `json:"vectors"`
}

// mapperRoot returns $MAPPER_ROOT, skipping when it is unset or does not carry
// the formats tree. Same convention as every other corpus-dependent test in
// this repository: the corpus is never vendored, so its absence is a skip with
// a message naming what was looked for, not a failure.
func mapperRoot(t *testing.T) string {
	t.Helper()
	root := deprecatedCorpusRoot()
	if root == "" {
		t.Skip("APMap vector corpus skipped: the auto-pigeon-libraries contract package is not beside this checkout")
	}
	if _, err := os.Stat(filepath.Join(root, vectorRoot)); err != nil {
		t.Skip("APMap vector corpus skipped: " + filepath.Join(root, vectorRoot) + ": " + err.Error())
	}
	return root
}

func loadVectorIndex(t *testing.T, root, kind string) vectorIndex {
	t.Helper()
	path := filepath.Join(root, vectorRoot, kind, "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var index vectorIndex
	if err := decodeInto(data, &index); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	// The index declares 1.0: this is the deprecated corpus, kept as frozen evidence of what the
	// SCH-*/SEM-* rules said. What it must not be is empty or mis-shaped.
	if index.APMapVersion == "" {
		t.Fatalf("%s declares no apmap_version", path)
	}
	if len(index.Vectors) == 0 {
		t.Fatalf("%s lists no vectors", path)
	}
	return index
}

// corpusSchema is the schema MATCHING the version these vectors declare — not the current one.
//
// The corpus is the deprecated 1.0 conformance set, and a 1.0 document is measured against 1.0's
// frozen contract. That is the rule everywhere else in this package; this helper used to return
// the current schema and got away with it for as long as each new version was purely optional
// over the last. APMap 1.2 ended that: it requires `groups`, which no 1.0 document can carry, so
// every vector in the corpus started failing for a member 1.0 never had a word for.
//
// The failure was loud, but the bug it exposed was quiet and older: validating a document against
// a contract it does not claim to meet reports faults the document was never asked to avoid, and —
// worse in the other direction — would silently accept a document mislabelled as 1.0 that used a
// current-only member.
func corpusSchema(t *testing.T, root string) *Object {
	t.Helper()
	// Derived from the corpus's own index.json, never written down here: the corpus says which
	// contract it was written against, and the bundle is asked for that one.
	version := loadVectorIndex(t, root, "valid").APMapVersion
	schema, err := SchemaForVersion(version)
	if err != nil {
		t.Fatalf("the APMap %s contract is not loaded, so the %s corpus cannot be measured "+
			"against the contract it declares: %v", version, version, err)
	}
	return schema
}

func loadVector(t *testing.T, root, kind, file string) Value {
	t.Helper()
	path := filepath.Join(root, vectorRoot, kind, file)
	document, err := DecodeJSONFile(path)
	if err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return document
}

func TestValidVectorsPass(t *testing.T) {
	root := mapperRoot(t)
	schema := corpusSchema(t, root)
	index := loadVectorIndex(t, root, "valid")

	for _, vector := range index.Vectors {
		t.Run(vector.File, func(t *testing.T) {
			document := loadVector(t, root, "valid", vector.File)
			problems, err := ValidateAgainstSchema(document, schema)
			if err != nil {
				t.Fatalf("schema defect while validating a valid vector: %v", err)
			}
			if len(problems) != 0 {
				t.Fatalf("%s covers %q and must be schema-valid, got %d problem(s):\n  %s",
					vector.File, vector.Covers, len(problems), strings.Join(problems, "\n  "))
			}

			// The raising form must agree with the pure one.
			if err := Validate(document, ValidateOptions{Schema: schema}); err != nil {
				t.Fatalf("Validate rejected a valid vector: %v", err)
			}
		})
	}
}

// schemaDetectableLocation is where the validator reports each SCH-* vector's
// violation — frozen from AUE output. A keyword implemented at the wrong level
// of the tree tends to produce the right number of problems at the wrong place,
// so the location, not just the count, is what is pinned.
//
// Problem messages are positional and carry no SCH-* prefix; inventing one would
// be inventing a convention (AGENTS.md §2). What is preserved instead is the
// traceability itself: this table binds each rule id from the corpus index to
// the exact location the validator blames.
var schemaDetectableLocation = map[string]string{
	"SCH-1":  "$.apmap_version",
	"SCH-2":  "$",
	"SCH-3":  "$.revision",
	"SCH-4":  "$.role",
	"SCH-5":  "$",
	"SCH-6":  "$.entities[0].content[1]",
	"SCH-7":  "$.entities[0].content[1]",
	"SCH-8":  "$.entities[0].content[1]",
	"SCH-9":  "$.entities[0].content[1]",
	"SCH-10": "$.entities[0].entity_id",
	"SCH-11": `$.extensions.<key "review">`,
	"SCH-12": "$.entities[0].content",
	"SCH-13": "$.frame.to_source",
	"SCH-14": "$.entities[0].content[1]",
}

func TestInvalidVectorsAreRejectedByTheRightLayer(t *testing.T) {
	root := mapperRoot(t)
	schema := corpusSchema(t, root)
	index := loadVectorIndex(t, root, "invalid")

	sawSCH, sawSEM := 0, 0
	for _, vector := range index.Vectors {
		t.Run(vector.File, func(t *testing.T) {
			document := loadVector(t, root, "invalid", vector.File)
			problems, err := ValidateAgainstSchema(document, schema)
			if err != nil {
				t.Fatalf("schema defect while validating %s: %v", vector.File, err)
			}

			// `apmap-version-wrong.apmap`'s entire fault was declaring a version that is not 1.0 —
			// and the version it declares is the one this build now speaks, so the CURRENT schema
			// accepts it. Named rather than silently excluded: a silent exception list is how a
			// real regression hides. It is still refused in the ordinary path, by the version gate.
			if vector.File == "apmap-version-wrong.apmap" {
				t.Skip("declares the current version; refused by the version gate, not by the schema")
			}

			switch {
			case strings.HasPrefix(vector.Rule, "SCH-"):
				sawSCH++
				if len(problems) == 0 {
					t.Fatalf("%s violates %s (%s) and must fail schema validation, but passed",
						vector.File, vector.Rule, vector.Violation)
				}
				expected, known := schemaDetectableLocation[vector.Rule]
				if !known {
					t.Fatalf("%s cites %s, which this test has no frozen location for; "+
						"record the location the validator reports", vector.File, vector.Rule)
				}
				if location := problemLocation(problems[0]); location != expected {
					t.Fatalf("%s (%s): reported at %q, frozen at %q\n  full: %s",
						vector.File, vector.Rule, location, expected, problems[0])
				}
				var schemaError *SchemaError
				err = Validate(document, ValidateOptions{Schema: schema, Source: vector.File})
				if !errors.As(err, &schemaError) {
					t.Fatalf("Validate returned %v, want a *SchemaError", err)
				}
				if len(schemaError.Problems) != len(problems) {
					t.Fatalf("Validate reported %d problem(s), ValidateAgainstSchema %d",
						len(schemaError.Problems), len(problems))
				}
				if !errors.Is(err, ErrAPMap) {
					t.Fatal("a *SchemaError must satisfy errors.Is(err, ErrAPMap)")
				}

			case strings.HasPrefix(vector.Rule, "SEM-"):
				sawSEM++
				if len(problems) != 0 {
					t.Fatalf("%s violates %s, which JSON Schema cannot express — the schema layer "+
						"must accept it and leave the rejection to the semantics port. Got %d "+
						"problem(s), so this validator is over-rejecting:\n  %s",
						vector.File, vector.Rule, len(problems), strings.Join(problems, "\n  "))
				}

			default:
				t.Fatalf("%s cites rule %q, which is neither SCH-* nor SEM-*", vector.File, vector.Rule)
			}
		})
	}

	// Guard against the corpus silently losing its SEM-* half, which would
	// turn the interesting half of this test into a no-op.
	if sawSCH == 0 || sawSEM == 0 {
		t.Fatalf("the invalid corpus should exercise both layers, saw %d SCH-* and %d SEM-*", sawSCH, sawSEM)
	}
}

// TestNonFiniteVectorDecodes pins the reason apmap decodes numbers as
// literals rather than float64. non-finite-number.apmap writes a coordinate as
// 1e400, which SEM-11 rejects as non-finite — but only the semantics layer may
// reject it, so this layer has to get the document through the decoder first.
// A float64 decode fails outright here, and the failure would look like a
// malformed document rather than the deliberate out-of-range value it is.
func TestNonFiniteVectorDecodes(t *testing.T) {
	root := mapperRoot(t)
	path := filepath.Join(root, vectorRoot, "invalid", "non-finite-number.apmap")
	if _, err := os.Stat(path); err != nil {
		t.Skip("non-finite-number.apmap is not in this corpus: " + err.Error())
	}

	document, err := DecodeJSONFile(path)
	if err != nil {
		t.Fatalf("the non-finite vector must decode; SEM-11 is a semantic rule, not a parse error: %v", err)
	}
	if _, ok := document.(*Object); !ok {
		t.Fatalf("decoded to %T, want *Object", document)
	}

	// And the out-of-range literal survives to where a semantics port can see
	// it, rather than having been rounded, clamped, or dropped.
	if !strings.Contains(renderDocument(document), "1e400") {
		t.Fatal("the 1e400 literal did not survive decoding")
	}
}

func renderDocument(document Value) string { return render(document) }

// problemLocation splits a problem message at its first ": ", which every
// message in this validator prefixes with the JSON path it blames.
//
// Quotes are normalised to double, because one location shape — propertyNames'
// `<key ...>` — embeds a member name, and a frozen location should not depend on
// which quote character spells it. The structural part — that the blame lands
// on the key `review` of `$.extensions` — is compared exactly.
func problemLocation(problem string) string {
	index := strings.Index(problem, ": ")
	if index >= 0 {
		problem = problem[:index]
	}
	return strings.ReplaceAll(problem, "'", `"`)
}

// decodeInto is encoding/json's ordinary struct decode, used only for the
// corpus index files — they are test metadata, not APMap documents, so they
// need none of the ordering or literal-preservation this package's own decoder
// exists for.
func decodeInto(data []byte, target any) error { return json.Unmarshal(data, target) }
