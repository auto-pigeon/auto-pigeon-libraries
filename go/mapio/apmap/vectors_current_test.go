package apmap_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/internal/apmaptest"
)

// The CURRENT conformance vectors — AULIBS ts/apmap-schema/test-vectors — through both of AUE's
// validation layers, read from the corpus's own index.json so a vector added upstream becomes a
// case here without an edit. The index states three claims and each is a test:
//
//	valid             passes the schema AND every semantic rule, and is a canonical fixed point
//	invalid           is refused by the schema (or, for an unknown version, by the version gate)
//	semantic_invalid  PASSES the schema and is refused by the semantic layer for exactly its rule
//
// Nothing here names a version. The index says which one it was written for, and it must be the
// one this build writes: a promotion that forgot to re-sync the embedded bundle fails here first.

type currentIndex struct {
	APMapVersion string `json:"apmap_version"`
	Valid        []struct {
		File string `json:"file"`
	} `json:"valid"`
	Invalid []struct {
		File           string `json:"file"`
		Rule           string `json:"rule"`
		ExpectFragment *struct {
			Pointer string `json:"pointer"`
		} `json:"expect_fragment"`
		ExpectDocument *struct {
			Keyword string `json:"keyword"`
		} `json:"expect_document"`
	} `json:"invalid"`
	SemanticInvalid []struct {
		File string `json:"file"`
		Rule string `json:"rule"`
	} `json:"semantic_invalid"`
}

func loadCurrentIndex(t *testing.T) currentIndex {
	t.Helper()
	if apmaptest.PackageRoot() == "" {
		t.Skip("the auto-pigeon-libraries contract package is not beside this checkout")
	}
	data, err := os.ReadFile(filepath.Join(apmaptest.Vectors(), "index.json"))
	if err != nil {
		t.Fatalf("reading the current vector index: %v", err)
	}
	var index currentIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatalf("parsing the current vector index: %v", err)
	}
	if index.APMapVersion != apmap.CurrentVersion() {
		t.Fatalf("the vectors are for APMap %s and this build writes %s; re-sync the embedded bundle "+
			"(scripts/sync-aulibs.sh)", index.APMapVersion, apmap.CurrentVersion())
	}
	if len(index.Valid) == 0 || len(index.Invalid) == 0 || len(index.SemanticInvalid) == 0 {
		t.Fatal("the current vector index has an empty section")
	}
	return index
}

func readVector(t *testing.T, kind, file string) (string, *apmap.Object) {
	t.Helper()
	path := filepath.Join(apmaptest.Vectors(), kind, file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	document, err := apmap.Decode(string(data), file)
	if err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return string(data), document
}

func schemaProblems(t *testing.T, document apmap.Value) []string {
	t.Helper()
	schema, err := apmap.SchemaForDocument(document)
	if err != nil {
		t.Fatalf("resolving the contract: %v", err)
	}
	problems, err := apmap.ValidateAgainstSchema(document, schema)
	if err != nil {
		t.Fatalf("the validator could not read the contract: %v", err)
	}
	return problems
}

// ruleSet is the distinct SEM-* rule ids a problem list names, sorted.
func ruleSet(problems []string) []string {
	seen := map[string]bool{}
	for _, problem := range problems {
		seen[strings.Fields(problem)[0]] = true
	}
	var rules []string
	for rule := range seen {
		rules = append(rules, rule)
	}
	sort.Strings(rules)
	return rules
}

func TestCurrentValidVectorsPassBothLayersAndAreFixedPoints(t *testing.T) {
	index := loadCurrentIndex(t)
	for _, entry := range index.Valid {
		t.Run(entry.File, func(t *testing.T) {
			text, document := readVector(t, "valid", entry.File)
			if problems := schemaProblems(t, document); len(problems) > 0 {
				t.Fatalf("schema problems: %s", strings.Join(problems, "; "))
			}
			if problems := apmap.SemanticProblems(document); len(problems) > 0 {
				t.Fatalf("semantic problems: %s", strings.Join(problems, "; "))
			}
			if err := apmap.ValidateDocument(document, apmap.ValidateOptions{Source: entry.File}); err != nil {
				t.Fatalf("ValidateDocument: %v", err)
			}
			// Read -> write is byte-exact: authorship text, nested group members and one-member
			// groups all survive, member order included (SER-5).
			encoded, err := apmap.Encode(document)
			if err != nil {
				t.Fatal(err)
			}
			if encoded != text {
				t.Fatalf("decode -> encode changed the bytes")
			}
		})
	}
}

// problemPath is the location a problem blames: everything before its first ": ".
func problemPath(problem string) string {
	if at := strings.Index(problem, ": "); at >= 0 {
		return problem[:at]
	}
	return problem
}

// pointerPath spells a JSON pointer the way this validator spells a location: /groups/0 is
// $.groups[0].
func pointerPath(pointer string) string {
	out := "$"
	for _, token := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		if token == "" {
			continue
		}
		if _, err := fmt.Sscanf(token, "%d", new(int)); err == nil && strings.Trim(token, "0123456789") == "" {
			out += "[" + token + "]"
		} else {
			out += "." + token
		}
	}
	return out
}

func TestCurrentInvalidVectorsAreRefusedByTheSchemaWhereTheyBreakIt(t *testing.T) {
	index := loadCurrentIndex(t)
	for _, entry := range index.Invalid {
		t.Run(entry.File, func(t *testing.T) {
			_, document := readVector(t, "invalid", entry.File)
			if entry.Rule == "VER-1" {
				// An unknown future version is refused at the gate, by name, before any contract
				// is chosen to suit it.
				var versionError *apmap.VersionError
				err := apmap.GateDeclaredVersion(document)
				if !errors.As(err, &versionError) {
					t.Fatalf("GateDeclaredVersion = %v, want a *VersionError", err)
				}
				if !strings.Contains(err.Error(), `APMap 999.999 is not a supported APMap version`) ||
					!strings.Contains(err.Error(), "writes "+apmap.CurrentVersion()) {
					t.Fatalf("the refusal does not name the version and the capability: %v", err)
				}
				return
			}
			problems := schemaProblems(t, document)
			if len(problems) == 0 {
				t.Fatalf("%s (%s) passed the schema", entry.File, entry.Rule)
			}
			// Blamed on the path to where the vector says the fault is, not incidentally somewhere
			// else. "On the path" rather than "at": where the fault sits inside a oneOf branch —
			// a derived_from inside a content item, which is itself a oneOf of property and brush —
			// this validator blames the outermost oneOf it could not satisfy (the location
			// vectors_test.go already freezes for the 1.0 corpus), which is an ancestor of Ajv's.
			want := "$"
			if entry.ExpectFragment != nil {
				want = pointerPath(entry.ExpectFragment.Pointer)
			}
			blamed := false
			for _, problem := range problems {
				path := problemPath(problem)
				if strings.HasPrefix(path, want) || (path != "$" && strings.HasPrefix(want, path)) {
					blamed = true
				}
			}
			if !blamed {
				t.Fatalf("%s (%s): no problem is blamed at %s:\n  %s",
					entry.File, entry.Rule, want, strings.Join(problems, "\n  "))
			}
			var schemaError *apmap.SchemaError
			if err := apmap.ValidateDocument(document, apmap.ValidateOptions{Source: entry.File}); !errors.As(err, &schemaError) {
				t.Fatalf("ValidateDocument = %v, want a *SchemaError", err)
			}
		})
	}
}

func TestCurrentSemanticInvalidVectorsAreRefusedForExactlyTheirRule(t *testing.T) {
	index := loadCurrentIndex(t)
	for _, entry := range index.SemanticInvalid {
		t.Run(entry.File, func(t *testing.T) {
			_, document := readVector(t, "semantic-invalid", entry.File)
			// The schema must ACCEPT it; otherwise a missing semantic checker would look conformant.
			if problems := schemaProblems(t, document); len(problems) > 0 {
				t.Fatalf("the schema refused a semantic_invalid vector: %s", strings.Join(problems, "; "))
			}
			problems := apmap.SemanticProblems(document)
			if got := ruleSet(problems); strings.Join(got, ",") != entry.Rule {
				t.Fatalf("refused for %v, want exactly %s:\n  %s", got, entry.Rule, strings.Join(problems, "\n  "))
			}
			var semanticError *apmap.SemanticError
			if err := apmap.ValidateDocument(document, apmap.ValidateOptions{Source: entry.File}); !errors.As(err, &semanticError) {
				t.Fatalf("ValidateDocument = %v, want a *SemanticError", err)
			}
		})
	}
}

// chain builds n groups nested one inside the next, the innermost holding one brush.
func chain(n int, brushID string) []apmap.Value {
	groups := make([]apmap.Value, 0, n)
	for i := 0; i < n; i++ {
		member := apmap.NewObject()
		if i < n-1 {
			member.Set("kind", "group")
			member.Set("group_id", fmt.Sprintf("grp_chain%08d", i+1))
		} else {
			member.Set("kind", "brush")
			member.Set("brush_id", brushID)
		}
		group := apmap.NewObject()
		group.Set("group_id", fmt.Sprintf("grp_chain%08d", i))
		group.Set("name", fmt.Sprintf("G%d", i))
		group.Set("members", []apmap.Value{member})
		groups = append(groups, group)
	}
	return groups
}

func TestTheDepthBoundIs32AndAHostileChainIsRefusedNotOverflowed(t *testing.T) {
	loadCurrentIndex(t)
	_, document := readVector(t, "valid", "group-one-member.apmap")

	document.Set("groups", chain(apmap.GroupMaxDepth, "brs_cube00000000a1"))
	if problems := apmap.SemanticProblems(document); len(problems) > 0 {
		t.Fatalf("a chain of exactly %d groups was refused: %v", apmap.GroupMaxDepth, problems)
	}
	document.Set("groups", chain(apmap.GroupMaxDepth+1, "brs_cube00000000a1"))
	if got := ruleSet(apmap.SemanticProblems(document)); strings.Join(got, ",") != "SEM-G-9" {
		t.Fatalf("a chain of %d groups: %v", apmap.GroupMaxDepth+1, got)
	}

	// Twenty thousand deep: iterative and linear, so this is a problem report, not a stack overflow
	// and not a quadratic stall.
	document.Set("groups", chain(20000, "brs_cube00000000a1"))
	if got := ruleSet(apmap.SemanticProblems(document)); strings.Join(got, ",") != "SEM-G-9" {
		t.Fatalf("a hostile chain: %v", got)
	}
	if problems := schemaProblems(t, document); len(problems) > 0 {
		t.Fatalf("the hostile chain should be schema-valid: %v", problems[:1])
	}
}

// SEM-G-5 reaches across a tree, and only a tree: two unrelated roots may hold a brush and its face.
func TestContainmentIsPerTree(t *testing.T) {
	loadCurrentIndex(t)
	_, document := readVector(t, "valid", "group-nested-parent.apmap")
	faceID := firstFaceID(t, document, "brs_cube00000000c3")

	unrelated := apmap.NewObject()
	unrelated.Set("group_id", "grp_elsewhere00001")
	unrelated.Set("name", "Elsewhere")
	unrelated.Set("members", []apmap.Value{memberOf("face", faceID)})
	groups, _ := document.Get("groups")
	document.Set("groups", append(append([]apmap.Value{}, groups.([]apmap.Value)...), unrelated))
	if problems := apmap.SemanticProblems(document); len(problems) > 0 {
		t.Fatalf("an unrelated tree holding a face of a grouped brush was refused: %v", problems)
	}

	// The same face moved under the Columns child of the tree that holds its brush: refused.
	columns := groupByID(t, document, "grp_columns0000001")
	members, _ := columns.Get("members")
	columns.Set("members", append(append([]apmap.Value{}, members.([]apmap.Value)...), memberOf("face", faceID)))
	unrelated.Set("members", []apmap.Value{memberOf("brush", "brs_cube00000000a1")}) // keep it non-empty
	got := apmap.SemanticProblems(document)
	rules := ruleSet(got)
	if !contains(rules, "SEM-G-5") {
		t.Fatalf("containment across the tree was not refused: %v", got)
	}
}

func memberOf(kind, id string) *apmap.Object {
	member := apmap.NewObject()
	member.Set("kind", kind)
	member.Set(kind+"_id", id)
	return member
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func groupByID(t *testing.T, document *apmap.Object, id string) *apmap.Object {
	t.Helper()
	groups, _ := document.Get("groups")
	for _, entry := range groups.([]apmap.Value) {
		group := entry.(*apmap.Object)
		if got, _ := group.Get("group_id"); got == id {
			return group
		}
	}
	t.Fatalf("no group %s", id)
	return nil
}

func firstFaceID(t *testing.T, document *apmap.Object, brushID string) string {
	t.Helper()
	entities, _ := document.Get("entities")
	for _, entry := range entities.([]apmap.Value) {
		content, _ := entry.(*apmap.Object).Get("content")
		for _, item := range content.([]apmap.Value) {
			brush := item.(*apmap.Object)
			if id, _ := brush.Get("brush_id"); id == brushID {
				faces, _ := brush.Get("faces")
				faceID, _ := faces.([]apmap.Value)[0].(*apmap.Object).Get("face_id")
				return faceID.(string)
			}
		}
	}
	t.Fatalf("no brush %s", brushID)
	return ""
}

// TestAuthorshipAndGroupsSurviveEveryReadWritePath: decode -> encode is covered above for every
// vector; this is the model round trip, APMap -> .map geometry -> APMap (Rebuild), which used to
// rebuild the document without its groups, and never knew authorship existed.
func TestAuthorshipAndGroupsSurviveEveryReadWritePath(t *testing.T) {
	loadCurrentIndex(t)
	for _, file := range []string{
		"authorship-full.apmap", "authorship-deliberately-blank.apmap", "group-nested-parent.apmap",
		"group-nested-three-levels.apmap", "group-one-member.apmap", "group-cut-two-sides.apmap",
		"group-from-prefab.apmap",
	} {
		t.Run(file, func(t *testing.T) {
			text, document := readVector(t, "valid", file)
			geometry, err := apmap.ToMapDocument(document)
			if err != nil {
				t.Fatalf("ToMapDocument: %v", err)
			}
			rebuilt, err := apmap.Rebuild(document, geometry)
			if err != nil {
				t.Fatalf("Rebuild: %v", err)
			}
			encoded, err := apmap.Encode(rebuilt)
			if err != nil {
				t.Fatal(err)
			}
			if encoded != text {
				t.Fatalf("Rebuild is not the identity on %s:\n%s", file, firstDifference(encoded, text))
			}
		})
	}

	// The three states stay three: absent is not "", and "" is not absent.
	_, blank := readVector(t, "valid", "authorship-deliberately-blank.apmap")
	authorship, _ := blank.Get("authorship")
	object := authorship.(*apmap.Object)
	if author, present := object.Get("author"); !present || author != "" {
		t.Fatalf("author should be present and empty, got %q (present %v)", author, present)
	}
	if object.Has("copyright_notice") {
		t.Fatal("an absent copyright_notice was filled in")
	}
}

func firstDifference(got, want string) string {
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
		if gotLines[i] != wantLines[i] {
			return fmt.Sprintf("line %d:\n  got  %q\n  want %q", i+1, gotLines[i], wantLines[i])
		}
	}
	return fmt.Sprintf("lengths differ: %d vs %d lines", len(gotLines), len(wantLines))
}

// TestGroupingNeverChangesTheExportedMap: `.map` has no word for a group, so exporting loses the
// whole hierarchy (§14) — and, the part that could go wrong, writes each brush and entity exactly
// once however many groups reach it. A nested-group document exports to the same bytes as the same
// document with `groups: []`, and authorship is dropped the same way.
func TestGroupingNeverChangesTheExportedMap(t *testing.T) {
	loadCurrentIndex(t)
	for _, file := range []string{
		"group-nested-parent.apmap", "group-nested-three-levels.apmap", "authorship-full.apmap",
	} {
		t.Run(file, func(t *testing.T) {
			_, document := readVector(t, "valid", file)
			grouped, err := apmap.ExportMapText(document)
			if err != nil {
				t.Fatal(err)
			}
			document.Set("groups", []apmap.Value{})
			document.Delete("authorship")
			plain, err := apmap.ExportMapText(document)
			if err != nil {
				t.Fatal(err)
			}
			if grouped != plain {
				t.Fatalf("groups or authorship changed the exported .map:\n%s", firstDifference(grouped, plain))
			}
			brushes := 0
			entities, _ := document.Get("entities")
			for _, entity := range entities.([]apmap.Value) {
				content, _ := entity.(*apmap.Object).Get("content")
				for _, item := range content.([]apmap.Value) {
					if kind, _ := item.(*apmap.Object).Get("kind"); kind == "brush" {
						brushes++
					}
				}
			}
			// One `(` … `)` face line per face, and one `{` per entity plus one per brush.
			if opens := strings.Count(grouped, "{\n"); opens != len(entities.([]apmap.Value))+brushes {
				t.Fatalf("the .map opens %d blocks for %d entities and %d brushes",
					opens, len(entities.([]apmap.Value)), brushes)
			}
		})
	}
}

// A 1.3 document is held to 1.3: its frozen contract has no `group` arm and a two-member minimum,
// so a one-member group declared as 1.3 is refused before any promotion could launder it. A valid
// 1.3 document promotes by its header alone to a valid current document.
func TestALegacyDocumentIsHeldToItsOwnContractThenPromoted(t *testing.T) {
	loadCurrentIndex(t)
	legacy := filepath.Join(apmaptest.PackageRoot(), "deprecated", "1.3")

	data, err := os.ReadFile(filepath.Join(legacy, "test-vectors", "invalid", "group-one-member.apmap"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := apmap.Decode(string(data), "group-one-member.apmap")
	if err != nil {
		t.Fatal(err)
	}
	if version := apmap.DeclaredVersion(document); version != "1.3" {
		t.Fatalf("the frozen 1.3 vector declares %s", version)
	}
	problems := schemaProblems(t, document)
	if len(problems) == 0 || !strings.Contains(strings.Join(problems, "; "), "$.groups[0].members: needs at least 2 item(s), has 1") {
		t.Fatalf("a one-member group declared as 1.3 was not refused by 1.3's minItems: %v", problems)
	}
	// The same bytes promoted would be valid — which is exactly why promotion must never come first.
	if promoted := apmaptest.PromoteToCurrent(string(data)); len(schemaProblems(t, mustDecodeText(t, promoted))) > 0 {
		t.Fatal("the promoted one-member group should be valid under the current contract")
	}

	data, err = os.ReadFile(filepath.Join(legacy, "one-three-document.apmap"))
	if err != nil {
		t.Fatal(err)
	}
	document = mustDecodeText(t, string(data))
	if err := apmap.ValidateDocument(document, apmap.ValidateOptions{Source: "one-three-document.apmap"}); err != nil {
		t.Fatalf("the 1.3 fixture is not valid under its own contract: %v", err)
	}
	promoted := mustDecodeText(t, apmaptest.PromoteToCurrent(string(data)))
	if apmap.DeclaredVersion(promoted) != apmap.CurrentVersion() {
		t.Fatal("promotion did not rewrite the header")
	}
	if err := apmap.ValidateDocument(promoted, apmap.ValidateOptions{Source: "promoted"}); err != nil {
		t.Fatalf("the promoted 1.3 fixture is not a valid current document: %v", err)
	}
	if promoted.Has("authorship") {
		t.Fatal("promotion invented authorship; absent means unknown and must stay absent")
	}
}

func mustDecodeText(t *testing.T, text string) *apmap.Object {
	t.Helper()
	document, err := apmap.Decode(text, "test")
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// The frozen 1.2 and 1.3 corpora publish group-two-groups.apmap as VALID, and it carries one known
// containment fault — brush a1 and a1's own face in one group — which no schema could see and which
// the AULIBS reference checker reports as SEM-G-5 (test/promotion.test.mjs). Legacy documents must
// stay readable, so on a document declaring an older, readable version SEM-G-5 is a WARNING: the
// document validates and converts, and the finding is reported through SemanticWarnings. The same
// bytes relabelled as the current version are refused for SEM-G-5 and nothing else.
func TestTheFrozenLegacyContainmentFaultIsAWarningAndFatalOnlyWhenCurrent(t *testing.T) {
	loadCurrentIndex(t)
	for _, version := range []string{"1.2", "1.3"} {
		t.Run(version, func(t *testing.T) {
			path := filepath.Join(apmaptest.PackageRoot(), "deprecated", version, "test-vectors", "valid", "group-two-groups.apmap")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			legacy := mustDecodeText(t, string(data))
			if apmap.DeclaredVersion(legacy) != version {
				t.Fatalf("the frozen vector declares %s", apmap.DeclaredVersion(legacy))
			}
			if err := apmap.ValidateDocument(legacy, apmap.ValidateOptions{Source: path}); err != nil {
				t.Fatalf("the published-valid legacy vector was refused: %v", err)
			}
			if got := ruleSet(apmap.SemanticWarnings(legacy)); strings.Join(got, ",") != "SEM-G-5" {
				t.Fatalf("legacy warnings %v, want exactly SEM-G-5", got)
			}
			if _, err := apmap.ToMapDocument(legacy); err != nil {
				t.Fatalf("the legacy vector does not convert: %v", err)
			}

			// Relabelled current, $schema included: the same fault is fatal.
			current := apmaptest.PromoteToCurrent(string(data))
			if !strings.Contains(current, `"$schema": "https://auto-pigeon.org/schemas/apmap/`+apmap.CurrentVersion()+`/apmap.schema.json"`) {
				t.Fatal("the relabelled document does not carry the current $schema")
			}
			relabelled := mustDecodeText(t, current)
			if problems := schemaProblems(t, relabelled); len(problems) > 0 {
				t.Fatalf("the relabelled document is schema-invalid: %v", problems)
			}
			if got := ruleSet(apmap.SemanticProblems(relabelled)); strings.Join(got, ",") != "SEM-G-5" {
				t.Fatalf("relabelled %s: semantic rules %v, want exactly SEM-G-5", apmap.CurrentVersion(), got)
			}
			if warnings := apmap.SemanticWarnings(relabelled); len(warnings) > 0 {
				t.Fatalf("a current document has no warnings, only errors: %v", warnings)
			}
			var semanticError *apmap.SemanticError
			if err := apmap.ValidateDocument(relabelled, apmap.ValidateOptions{Source: "relabelled"}); !errors.As(err, &semanticError) {
				t.Fatalf("ValidateDocument = %v, want a *SemanticError", err)
			}
		})
	}
}

// Only SEM-G-5 is softened for a legacy document. Every other group rule stays fatal whatever the
// declared version.
func TestOnlyContainmentIsLenientForLegacy(t *testing.T) {
	loadCurrentIndex(t)
	path := filepath.Join(apmaptest.PackageRoot(), "deprecated", "1.3", "test-vectors", "valid", "group-two-brushes.apmap")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document := mustDecodeText(t, string(data))
	groups, _ := document.Get("groups")
	group := groups.([]apmap.Value)[0].(*apmap.Object)
	twin := apmap.NewObject()
	twin.Set("group_id", "grp_twin00000001")
	twin.Set("name", "Twin")
	members, _ := group.Get("members")
	twin.Set("members", members) // the same two brushes: each now in two groups
	document.Set("groups", []apmap.Value{group, twin})
	if got := ruleSet(apmap.SemanticProblems(document)); strings.Join(got, ",") != "SEM-G-4" {
		t.Fatalf("legacy SEM-G-4: %v, want it fatal", got)
	}
	if len(apmap.SemanticWarnings(document)) > 0 {
		t.Fatal("SEM-G-4 was softened to a warning")
	}
}
