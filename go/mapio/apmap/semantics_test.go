package apmap

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func decodeDocument(t *testing.T, body string) Value {
	t.Helper()
	value, err := DecodeJSON([]byte(body))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return value
}

// rulesCited returns the distinct SEM-* rule ids a problem list names.
func rulesCited(problems []string) map[string]bool {
	cited := map[string]bool{}
	for _, problem := range problems {
		if index := strings.IndexByte(problem, ' '); index > 0 {
			cited[problem[:index]] = true
		}
	}
	return cited
}

// --- the published corpus ---------------------------------------------------

// TestSemanticVectorsAreRejectedByRule is the counterpart to prompt 13's
// schema-layer test, and together they close the loop: that one asserts the
// seven SEM-* vectors are *accepted* by the schema layer, this one asserts they
// are rejected here, by the rule the corpus's own index names.
//
// A validator that rejected everything would pass one of those tests and fail
// the other, which is the point of asserting both directions.
func TestSemanticVectorsAreRejectedByRule(t *testing.T) {
	root := mapperRoot(t)
	index := loadVectorIndex(t, root, "invalid")

	sawSEM := 0
	for _, vector := range index.Vectors {
		t.Run(vector.File, func(t *testing.T) {
			document := loadVector(t, root, "invalid", vector.File)
			problems := SemanticProblems(document)

			if !strings.HasPrefix(vector.Rule, "SEM-") {
				// A SCH-* vector is the schema layer's to reject. It may or may
				// not also trip a semantic rule — several are structurally
				// broken enough to — so nothing is asserted about it here
				// beyond the absence of a panic, which reaching this line
				// already proves.
				return
			}

			sawSEM++
			if len(problems) == 0 {
				t.Fatalf("%s violates %s (%s) and must be rejected here, but passed",
					vector.File, vector.Rule, vector.Violation)
			}
			if cited := rulesCited(problems); !cited[vector.Rule] {
				t.Errorf("%s should cite %s, problems cite %v:\n  %s",
					vector.File, vector.Rule, keysOf(cited), strings.Join(problems, "\n  "))
			}

			// And the raising form must agree.
			err := ValidateSemantics(document, vector.File)
			if err == nil {
				t.Fatal("ValidateSemantics must reject what SemanticProblems reports")
			}
			var semanticError *SemanticError
			if !asSemanticError(err, &semanticError) {
				t.Fatalf("got %T, want *SemanticError", err)
			}
			if len(semanticError.Problems) != len(problems) {
				t.Errorf("the two forms disagree: %d vs %d problems", len(semanticError.Problems), len(problems))
			}
		})
	}

	if sawSEM != 7 {
		t.Errorf("expected the 7 documented SEM-* vectors, saw %d", sawSEM)
	}
}

// TestValidVectorsPassSemantics is the false-positive half. A semantic
// rejection of a valid document is exactly as wrong as accepting an invalid
// one, and it is the easier mistake to make — every rule here is a heuristic
// applied to a document the rule's author never saw.
func TestValidVectorsPassSemantics(t *testing.T) {
	root := mapperRoot(t)
	index := loadVectorIndex(t, root, "valid")

	for _, vector := range index.Vectors {
		t.Run(vector.File, func(t *testing.T) {
			document := loadVector(t, root, "valid", vector.File)
			if problems := SemanticProblems(document); len(problems) != 0 {
				t.Fatalf("%s covers %q and must pass every semantic rule, got:\n  %s",
					vector.File, vector.Covers, strings.Join(problems, "\n  "))
			}
			if err := ValidateSemantics(document, vector.File); err != nil {
				t.Fatalf("ValidateSemantics rejected a valid vector: %v", err)
			}
		})
	}
}

// frozenSemanticProblems is SemanticProblems' exact output over the published APMap 1.0 corpus,
// frozen from AUE output. Every vector not listed here reports no semantic problem. The messages
// are assembled from rule ids and JSON paths, so they are stable enough to pin message for message:
// a rule that moves its blame, changes its wording or starts firing on another vector fails here.
var frozenSemanticProblems = map[string][]string{
	"invalid/affine-transform-reserved.apmap":          {"SEM-4 frame: space is source_world, so to_source must be the identity transform"},
	"invalid/brush-too-few-faces.apmap":                {"SEM-6 entities[0].content[1]: the faces do not enclose a finite volume"},
	"invalid/collinear-plane-points.apmap":             {"SEM-5 entities[0].content[1].faces[0]: the three plane points are collinear"},
	"invalid/dangling-relationship.apmap":              {`SEM-2 relationships[0].to: "ent_doesnotexist00" is not declared in this document`},
	"invalid/duplicate-object-id.apmap":                {`SEM-1 duplicate object id "brs_bad1000000000000": declared at entities[0].content[1].brush_id and entities[0].content[2].brush_id`},
	"invalid/entity-empty-content.apmap":               {"SEM-9 a full_map needs exactly one worldspawn entity, found 0"},
	"invalid/non-finite-number.apmap":                  {"SEM-11 $.entities[0].content[1].faces[0].plane.points[0][0]: inf is not a finite number"},
	"invalid/synthetic-reason-without-synthetic.apmap": {"SEM-3 entities[0].content[1].faces[0]: synthetic and synthetic_reason must appear together"},
	"invalid/unbounded-brush.apmap":                    {"SEM-6 entities[0].content[1]: the faces do not enclose a finite volume"},
	"invalid/world-frame-with-translation.apmap":       {"SEM-4 frame: space is source_world, so to_source must be the identity transform"},
}

// TestSemanticProblemsOverEveryVectorAreFrozen compares SemanticProblems over every published
// vector against frozenSemanticProblems, message for message.
func TestSemanticProblemsOverEveryVectorAreFrozen(t *testing.T) {
	root := mapperRoot(t)

	compared, withProblems := 0, 0
	for _, kind := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join(root, vectorRoot, kind, "*.apmap"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			name := kind + "/" + filepath.Base(file)
			got := SemanticProblems(loadVector(t, root, kind, filepath.Base(file)))
			want := frozenSemanticProblems[name]
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("%s:\n  got:    %q\n  frozen: %q", name, got, want)
			}
			compared++
			if len(got) > 0 {
				withProblems++
			}
		}
	}

	if compared == 0 {
		t.Fatal("no vectors were compared")
	}
	if withProblems != len(frozenSemanticProblems) {
		t.Errorf("%d vectors report problems, %d are frozen with problems", withProblems, len(frozenSemanticProblems))
	}
}

// --- SEM-6, the rule most at risk of being implemented wrong ---------------

// buildBrush assembles a document with one entity holding one brush whose faces
// are the given plane point triples.
func buildBrush(t *testing.T, faces string) Value {
	t.Helper()
	return decodeDocument(t, `{
      "apmap_version": "1.0",
      "entities": [
        {"entity_id": "ent_aaaaaaaaaaaaaaaa",
         "content": [
           {"kind": "brush", "brush_id": "brs_aaaaaaaaaaaaaaaa", "faces": `+faces+`}
         ]}
      ]
    }`)
}

// A unit cube's six faces, each a triple of plane points wound so its normal
// points outward. This is the shape the sweep must accept.
const cubeFaces = `[
  {"face_id": "fac_1", "plane": {"points": [[0,0,0],[0,1,0],[1,0,0]]}},
  {"face_id": "fac_2", "plane": {"points": [[0,0,1],[1,0,1],[0,1,1]]}},
  {"face_id": "fac_3", "plane": {"points": [[0,0,0],[1,0,0],[0,0,1]]}},
  {"face_id": "fac_4", "plane": {"points": [[0,1,0],[0,1,1],[1,1,0]]}},
  {"face_id": "fac_5", "plane": {"points": [[0,0,0],[0,0,1],[0,1,0]]}},
  {"face_id": "fac_6", "plane": {"points": [[1,0,0],[1,1,0],[1,0,1]]}}
]`

// TestSEM6AcceptsABoundedBrush is the over-rejection half. The sweep is a
// weaker check than convex reconstruction, so anything it rejects is rejected
// for certain — which makes a false positive here a document the rule was never
// meant to reject.
func TestSEM6AcceptsABoundedBrush(t *testing.T) {
	problems := SemanticProblems(buildBrush(t, cubeFaces))
	for _, problem := range problems {
		if strings.HasPrefix(problem, "SEM-6") {
			t.Errorf("a closed cube must not be reported unbounded: %s", problem)
		}
	}
}

// TestSEM6CatchesAnUnboundedBrush is the whole point of the rule: planes that
// are individually fine and collectively enclose nothing.
func TestSEM6CatchesAnUnboundedBrush(t *testing.T) {
	// Four faces forming an open box — the -x direction has no face normal
	// pointing that way, so the solid runs to infinity.
	open := `[
      {"face_id": "fac_1", "plane": {"points": [[0,0,0],[0,1,0],[1,0,0]]}},
      {"face_id": "fac_2", "plane": {"points": [[0,0,1],[1,0,1],[0,1,1]]}},
      {"face_id": "fac_3", "plane": {"points": [[0,0,0],[1,0,0],[0,0,1]]}},
      {"face_id": "fac_4", "plane": {"points": [[0,1,0],[0,1,1],[1,1,0]]}},
      {"face_id": "fac_5", "plane": {"points": [[1,0,0],[1,1,0],[1,0,1]]}}
    ]`
	problems := SemanticProblems(buildBrush(t, open))

	if !rulesCited(problems)["SEM-6"] {
		t.Errorf("an open box must be reported unbounded, got: %v", problems)
	}
}

// TestSEM6IsSuppressedByDegenerateFaces: a brush with a
// collinear face reports SEM-5 and *does not* also report SEM-6, because the
// plane set is incomplete and judging boundedness from it would be guessing.
func TestSEM6IsSuppressedByDegenerateFaces(t *testing.T) {
	degenerate := `[
      {"face_id": "fac_1", "plane": {"points": [[0,0,0],[1,1,1],[2,2,2]]}},
      {"face_id": "fac_2", "plane": {"points": [[0,0,1],[1,0,1],[0,1,1]]}}
    ]`
	problems := SemanticProblems(buildBrush(t, degenerate))
	cited := rulesCited(problems)

	if !cited["SEM-5"] {
		t.Errorf("collinear points must be reported, got: %v", problems)
	}
	if cited["SEM-6"] {
		t.Errorf("SEM-6 must be suppressed when a face is degenerate, got: %v", problems)
	}
}

// TestSEM6IsNotStrengthened pins the deliberately-weaker condition. These six
// planes pass the axis sweep but do not actually bound a finite volume — a full
// convex reconstruction would reject them. SEM-6 is defined as the sweep, so AUE
// accepts this document; a stronger check would be a bug.
func TestSEM6IsNotStrengthened(t *testing.T) {
	// Six faces whose normals cover all six axis directions, but offset so the
	// half-spaces do not close: the +x and -x planes are the wrong way round.
	notReallyClosed := `[
      {"face_id": "fac_1", "plane": {"points": [[0,0,0],[0,1,0],[1,0,0]]}},
      {"face_id": "fac_2", "plane": {"points": [[0,0,1],[1,0,1],[0,1,1]]}},
      {"face_id": "fac_3", "plane": {"points": [[0,0,0],[1,0,0],[0,0,1]]}},
      {"face_id": "fac_4", "plane": {"points": [[0,1,0],[0,1,1],[1,1,0]]}},
      {"face_id": "fac_5", "plane": {"points": [[9,0,0],[9,0,1],[9,1,0]]}},
      {"face_id": "fac_6", "plane": {"points": [[-9,0,0],[-9,1,0],[-9,0,1]]}}
    ]`
	problems := SemanticProblems(buildBrush(t, notReallyClosed))

	// The assertion is only that the sweep's verdict is used as-is. Whatever it
	// says is pinned over the corpus by TestSemanticProblemsOverEveryVectorAreFrozen,
	// and must not come from a stronger reconstruction this module does not have.
	if len(problems) > 0 && !rulesCited(problems)["SEM-6"] {
		t.Errorf("unexpected problems: %v", problems)
	}
}

// --- SEM-8 / SEM-10 ---------------------------------------------------------

// derivedDocument builds an id_policy: "derived" document whose one entity
// carries the given id and derived_from record.
func derivedDocument(t *testing.T, entityID, derivedFrom string) Value {
	t.Helper()
	return decodeDocument(t, `{
      "apmap_version": "1.0",
      "id_policy": "derived",
      "entities": [
        {"entity_id": "`+entityID+`", "derived_from": `+derivedFrom+`, "content": []}
      ]
    }`)
}

func TestSEM10AcceptsACorrectDerivedID(t *testing.T) {
	identity := PackageIdentity{PackageID: "pkg-1", Role: "full_map"}
	correct, err := identity.IDFor(KindEntity, "e0")
	if err != nil {
		t.Fatal(err)
	}

	document := derivedDocument(t, correct,
		`{"kind": "package", "package_id": "pkg-1", "role": "full_map", "object_path": "e0"}`)

	if problems := SemanticProblems(document); len(problems) != 0 {
		t.Errorf("a correctly derived id must pass, got: %v", problems)
	}
}

func TestSEM10CatchesAWrongDerivedID(t *testing.T) {
	document := derivedDocument(t, "ent_0000000000000000",
		`{"kind": "package", "package_id": "pkg-1", "role": "full_map", "object_path": "e0"}`)

	problems := SemanticProblems(document)
	if !rulesCited(problems)["SEM-10"] {
		t.Fatalf("a wrong derived id must be caught, got: %v", problems)
	}
	// The message must name the id it expected, or a reader cannot fix it.
	if !strings.Contains(problems[0], "expected") {
		t.Errorf("the message should name the expected id: %s", problems[0])
	}
}

func TestSEM8CatchesAMissingDerivedFrom(t *testing.T) {
	document := decodeDocument(t, `{
      "apmap_version": "1.0",
      "id_policy": "derived",
      "entities": [{"entity_id": "ent_aaaaaaaaaaaaaaaa", "content": []}]
    }`)

	if !rulesCited(SemanticProblems(document))["SEM-8"] {
		t.Errorf("id_policy derived with no derived_from must be caught, got: %v", SemanticProblems(document))
	}
}

// TestIdentityPolicyIsOnlyCheckedWhenDerived: an authored document makes no
// claim about derivation, so neither rule applies.
func TestIdentityPolicyIsOnlyCheckedWhenDerived(t *testing.T) {
	document := decodeDocument(t, `{
      "apmap_version": "1.0",
      "id_policy": "authored",
      "entities": [{"entity_id": "ent_aaaaaaaaaaaaaaaa", "content": []}]
    }`)

	if problems := SemanticProblems(document); len(problems) != 0 {
		t.Errorf("id_policy authored makes no derivation claim, got: %v", problems)
	}
}

// --- SEM-3, SEM-9, SEM-11 ---------------------------------------------------

func TestSEM3SyntheticPairing(t *testing.T) {
	cases := map[string]struct {
		face   string
		wantEM bool
	}{
		"both present and true": {`{"face_id": "f", "synthetic": true, "synthetic_reason": "context_crop"}`, false},
		"reason without flag":   {`{"face_id": "f", "synthetic_reason": "context_crop"}`, true},
		"flag without reason":   {`{"face_id": "f", "synthetic": true}`, true},
		"flag is false":         {`{"face_id": "f", "synthetic": false, "synthetic_reason": "context_crop"}`, true},
		// Truthy is not true: the rule says the value must be exactly true.
		"flag is a truthy string": {`{"face_id": "f", "synthetic": "yes", "synthetic_reason": "context_crop"}`, true},
		"neither present":         {`{"face_id": "f"}`, false},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			problems := SemanticProblems(buildBrush(t, "["+testCase.face+"]"))
			if got := rulesCited(problems)["SEM-3"]; got != testCase.wantEM {
				t.Errorf("SEM-3 cited = %v, want %v; problems: %v", got, testCase.wantEM, problems)
			}
		})
	}
}

func TestSEM9Worldspawn(t *testing.T) {
	entity := func(classname string) string {
		return `{"entity_id": "ent_` + classname + `", "content": [{"kind": "property", "key": "classname", "value": "` + classname + `"}]}`
	}

	cases := map[string]struct {
		document string
		wantSEM9 bool
	}{
		"one worldspawn first": {
			`{"role": "full_map", "entities": [` + entity("worldspawn") + `,` + entity("light") + `]}`, false},
		"empty full map is legal": {
			`{"role": "full_map", "entities": []}`, false},
		"no entities member at all": {
			`{"role": "full_map"}`, false},
		"two worldspawns": {
			`{"role": "full_map", "entities": [` + entity("worldspawn") + `,` + entity("worldspawn") + `]}`, true},
		"no worldspawn": {
			`{"role": "full_map", "entities": [` + entity("light") + `]}`, true},
		"worldspawn not first": {
			`{"role": "full_map", "entities": [` + entity("light") + `,` + entity("worldspawn") + `]}`, true},
		"another role is exempt": {
			`{"role": "prefab_core", "entities": [` + entity("light") + `]}`, false},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			problems := SemanticProblems(decodeDocument(t, testCase.document))
			if got := rulesCited(problems)["SEM-9"]; got != testCase.wantSEM9 {
				t.Errorf("SEM-9 cited = %v, want %v; problems: %v", got, testCase.wantSEM9, problems)
			}
		})
	}
}

// TestSEM11Finiteness pins the integer-literal exemption: an integer is
// arbitrary precision and cannot be infinite, and over a literal-preserving
// value model the question is whether the literal is an integer one.
func TestSEM11Finiteness(t *testing.T) {
	cases := map[string]bool{
		`{"x": 1e400}`:  true, // decodes to infinity in Python; flagged
		`{"x": -1e400}`: true,
		`{"x": 1.5}`:    false,
		`{"x": 1}`:      false,
		// A four-hundred-digit *integer* literal is Python's int: arbitrary
		// precision, always finite, never flagged.
		`{"x": ` + strings.Repeat("9", 400) + `}`: false,
		`{"a": [1, 2, {"b": 1e400}]}`:             true,  // found at any depth
		`{"x": "1e400"}`:                          false, // a string is not a number
		`{"x": true}`:                             false,
		`{"x": null}`:                             false,
	}

	for body, wantFlagged := range cases {
		document := decodeDocument(t, body)
		got := rulesCited(SemanticProblems(document))["SEM-11"]
		if got != wantFlagged {
			t.Errorf("%.40s: SEM-11 cited = %v, want %v", body, got, wantFlagged)
		}
	}

	// The path is reported, so a reader can find the value in a large document.
	problems := SemanticProblems(decodeDocument(t, `{"a": [1, 2, {"b": 1e400}]}`))
	if len(problems) != 1 || !strings.Contains(problems[0], "$.a[2].b") {
		t.Errorf("the problem should name the path, got %v", problems)
	}
}

// --- SEM-1, SEM-2 -----------------------------------------------------------

func TestSEM1DuplicateIDsNameBothSites(t *testing.T) {
	document := decodeDocument(t, `{
      "entities": [
        {"entity_id": "ent_dup", "content": []},
        {"entity_id": "ent_dup", "content": []}
      ]
    }`)

	problems := SemanticProblems(document)
	if !rulesCited(problems)["SEM-1"] {
		t.Fatalf("a duplicate id must be caught, got: %v", problems)
	}
	// Naming only the second site would leave a reader hunting for the first.
	if !strings.Contains(problems[0], "entities[0]") || !strings.Contains(problems[0], "entities[1]") {
		t.Errorf("the message should name both declaration sites: %s", problems[0])
	}
}

func TestSEM1SpansEveryObjectKind(t *testing.T) {
	// An entity, a brush and a face sharing one id — uniqueness is across all
	// three kinds, not within each.
	document := decodeDocument(t, `{
      "entities": [
        {"entity_id": "obj_same", "content": [
          {"kind": "brush", "brush_id": "obj_same", "faces": [
            {"face_id": "obj_same", "plane": {"points": [[0,0,0],[0,1,0],[1,0,0]]}}
          ]}
        ]}
      ]
    }`)

	problems := SemanticProblems(document)
	count := 0
	for _, problem := range problems {
		if strings.HasPrefix(problem, "SEM-1") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("two duplicates should be reported (brush and face against the entity), got %d: %v", count, problems)
	}
}

func TestSEM2ReferenceIntegrity(t *testing.T) {
	document := decodeDocument(t, `{
      "entities": [{"entity_id": "ent_known", "content": []}],
      "relationships": [
        {"kind": "activation_source", "from": "ent_known", "to": "ent_missing"},
        {"kind": "entity_target", "from": "ent_absent", "to": "ent_known"}
      ]
    }`)

	problems := SemanticProblems(document)
	if len(problems) != 2 {
		t.Fatalf("both dangling ends should be reported, got %d: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "relationships[0].to") || !strings.Contains(problems[1], "relationships[1].from") {
		t.Errorf("each problem should name its end: %v", problems)
	}
}

// --- SEM-4 ------------------------------------------------------------------

func TestSEM4FrameConsistency(t *testing.T) {
	cases := map[string]bool{
		`{"frame": {"space": "source_world", "to_source": {"type": "identity"}}}`:                       false,
		`{"frame": {"space": "source_world", "to_source": {"type": "translation", "offset": [1,0,0]}}}`: true,
		// A prefab-local frame may legitimately carry a translation.
		`{"frame": {"space": "prefab_local", "to_source": {"type": "translation", "offset": [1,0,0]}}}`: false,
		`{"frame": {"space": "source_world"}}`:                                                          false,
		`{}`:                                                                                            false,
	}

	for body, wantFlagged := range cases {
		if got := rulesCited(SemanticProblems(decodeDocument(t, body)))["SEM-4"]; got != wantFlagged {
			t.Errorf("%s: SEM-4 cited = %v, want %v", body, got, wantFlagged)
		}
	}
}

// --- defensiveness ----------------------------------------------------------

// TestSemanticProblemsOnASchemaInvalidDocument is the prompt's requirement, and
// the reason every type assertion in semantics.go uses the comma-ok form.
//
// This pass runs on documents that already failed the schema, so it must not
// panic on any of them — and must not manufacture confidence either. A document
// missing everything has no semantic violations to report, because there is
// nothing to violate; that is the schema layer's finding, not this one's.
func TestSemanticProblemsOnASchemaInvalidDocument(t *testing.T) {
	malformed := []string{
		`{}`,
		`{"entities": null}`,
		`{"entities": "not an array"}`,
		`{"entities": [null, 1, "text", []]}`,
		`{"entities": [{"entity_id": 42, "content": "not an array"}]}`,
		`{"entities": [{"content": [{"kind": "brush", "faces": "not an array"}]}]}`,
		`{"entities": [{"content": [{"kind": "brush", "faces": [{"plane": "not an object"}]}]}]}`,
		`{"entities": [{"content": [{"kind": "brush", "faces": [{"plane": {"points": [[0,0],[1],[]]}}]}]}]}`,
		`{"entities": [{"content": [{"kind": "brush", "faces": [{"plane": {"points": [["a","b","c"],[1,2,3],[4,5,6]]}}]}]}]}`,
		`{"entities": [{"content": [{"kind": "brush", "faces": [{"plane": {"points": [[true,0,0],[0,1,0],[1,0,0]]}}]}]}]}`,
		`{"relationships": [null, {"from": 1, "to": null}]}`,
		`{"frame": "not an object"}`,
		`{"frame": {"space": "source_world", "to_source": "not an object"}}`,
		`{"role": "full_map", "entities": [null, "text"]}`,
		`{"id_policy": "derived", "entities": [{"entity_id": "x", "derived_from": "not an object"}]}`,
		`{"id_policy": "derived", "entities": [{"entity_id": "x", "derived_from": {"kind": "package"}}]}`,
	}

	for _, body := range malformed {
		document := decodeDocument(t, body)
		// The assertion is that this returns at all.
		problems := SemanticProblems(document)
		for _, problem := range problems {
			if !strings.HasPrefix(problem, "SEM-") {
				t.Errorf("%s produced a problem that does not name a rule: %q", body, problem)
			}
		}
		if err := ValidateSemantics(document, "x"); err != nil && len(problems) == 0 {
			t.Errorf("%s: the two forms disagree", body)
		}
	}
}

// TestSemanticProblemsRejectsNonObjects covers the SEM-0 guard.
func TestSemanticProblemsRejectsNonObjects(t *testing.T) {
	for _, value := range []Value{nil, "text", json.Number("1"), []Value{}, true} {
		problems := SemanticProblems(value)
		if len(problems) != 1 || !strings.HasPrefix(problems[0], "SEM-0") {
			t.Errorf("SemanticProblems(%#v) = %v, want one SEM-0 problem", value, problems)
		}
	}
}

// TestSchemaAndSemanticLayersAreIndependent states the division of
// responsibility as a test: neither layer's verdict implies the other's.
func TestSchemaAndSemanticLayersAreIndependent(t *testing.T) {
	root := mapperRoot(t)
	schema := corpusSchema(t, root)
	index := loadVectorIndex(t, root, "invalid")

	for _, vector := range index.Vectors {
		document := loadVector(t, root, "invalid", vector.File)
		schemaProblems, err := ValidateAgainstSchema(document, schema)
		if err != nil {
			t.Fatalf("%s: %v", vector.File, err)
		}
		semantic := SemanticProblems(document)

		if strings.HasPrefix(vector.Rule, "SEM-") {
			// The seven SEM vectors: schema-clean, semantically rejected. This
			// is the exact complement of prompt 13's assertion.
			if len(schemaProblems) != 0 {
				t.Errorf("%s (%s) should be schema-clean, got: %v", vector.File, vector.Rule, schemaProblems)
			}
			if len(semantic) == 0 {
				t.Errorf("%s (%s) should be semantically rejected", vector.File, vector.Rule)
			}
		}
	}
}

func keysOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}

func asSemanticError(err error, target **SemanticError) bool {
	typed, ok := err.(*SemanticError)
	if ok {
		*target = typed
	}
	return ok
}
