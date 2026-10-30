package apmap

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// writeFileForTest is a one-line helper kept local so the fixture writes below
// read as one statement each.
func writeFileForTest(path, text string) error {
	return os.WriteFile(path, []byte(text), 0o644)
}

// inspectFixture is a hand-built document rather than a converted one, so each
// count below has a value a reader can check by eye against the literal: three
// entities, two brushes, five faces, one of them synthetic.
const inspectFixture = `{
  "apmap_version": "1.0",
  "document_id": "fixture:full_map",
  "revision": "3",
  "role": "full_map",
  "game": "quake1",
  "id_policy": "stable",
  "frame": {"space": "source", "to_source": {"type": "translate", "translation": [8, 0, -16]}},
  "entities": [
    {"content": [
      {"kind": "brush", "faces": [{"texture": "A"}, {"texture": "B"}, {"texture": "C", "synthetic": true}]},
      {"kind": "brush", "faces": [{"texture": "D"}, {"texture": "E"}]},
      {"kind": "patch", "faces": [{"texture": "IGNORED"}]}
    ]},
    {"content": []},
    {}
  ],
  "relationships": [{"from": "a", "to": "b"}],
  "extensions": {"zeta": {}, "alpha": {}}
}`

func inspectDocument(t *testing.T, text string) *Object {
	t.Helper()
	document, err := Decode(text, "fixture.apmap")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return document
}

func TestInspectCounts(t *testing.T) {
	summary, err := Inspect(inspectDocument(t, inspectFixture), "fixture.apmap", nil)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	tests := []struct {
		name string
		got  int
		want int
	}{
		{"entities", summary.Entities, 3},
		// The `patch` item is not a brush and its face is not counted; only
		// `kind == "brush"` counts, and this is the case that proves it.
		{"brushes", summary.Brushes, 2},
		{"faces", summary.Faces, 5},
		{"synthetic_faces", summary.SyntheticFaces, 1},
		{"relationships", summary.Relationships, 1},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s = %d, want %d", test.name, test.got, test.want)
		}
	}

	// Extension names are sorted, not in document order — the fixture declares
	// zeta before alpha precisely to catch a missing sort.
	if strings.Join(summary.Extensions, ",") != "alpha,zeta" {
		t.Errorf("extensions = %v, want [alpha zeta]", summary.Extensions)
	}
}

func TestInspectIdentityFields(t *testing.T) {
	summary, err := Inspect(inspectDocument(t, inspectFixture), "named.apmap", nil)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if summary.Document != "named.apmap" {
		t.Errorf("document = %q, want the name passed in", summary.Document)
	}
	pairs := []struct {
		name string
		got  any
		want string
	}{
		{"apmap_version", summary.APMapVersion, "1.0"},
		{"document_id", summary.DocumentID, "fixture:full_map"},
		{"revision", summary.Revision, "3"},
		{"role", summary.Role, "full_map"},
		{"game", summary.Game, "quake1"},
		{"id_policy", summary.IDPolicy, "stable"},
	}
	for _, pair := range pairs {
		if pair.got != pair.want {
			t.Errorf("%s = %v, want %q", pair.name, pair.got, pair.want)
		}
	}
	if summary.Frame.Space != "source" {
		t.Errorf("frame.space = %v, want source", summary.Frame.Space)
	}
	// to_source must be plain Go values, not an *Object — an *Object marshals
	// as `{}` and would silently drop the transform.
	transform, ok := summary.Frame.ToSource.(map[string]any)
	if !ok {
		t.Fatalf("frame.to_source = %T, want map[string]any", summary.Frame.ToSource)
	}
	if transform["type"] != "translate" {
		t.Errorf("to_source.type = %v, want translate", transform["type"])
	}
}

// TestInspectAbsentScalarsAreNullNotEmpty is the distinction the pointer fields
// exist for: a document that omits `role` is a different fact from one that
// declares it empty, and an inspection command exists to tell them apart.
func TestInspectAbsentScalarsAreNullNotEmpty(t *testing.T) {
	summary, err := Inspect(inspectDocument(t, `{"apmap_version": "1.0", "role": ""}`), "bare.apmap", nil)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if summary.Role != "" {
		t.Errorf("a declared empty role = %v, want \"\"", summary.Role)
	}
	if summary.Game != nil {
		t.Errorf("an absent game = %v, want nil", summary.Game)
	}

	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if !strings.Contains(string(encoded), `"game":null`) {
		t.Errorf("an absent game did not marshal as null:\n%s", encoded)
	}
	if !strings.Contains(string(encoded), `"role":""`) {
		t.Errorf("a declared empty role did not marshal as \"\":\n%s", encoded)
	}
}

// TestInspectEmptyDocument pins the degenerate case: nothing declared, nothing
// counted, and arrays that are arrays rather than nulls.
func TestInspectEmptyDocument(t *testing.T) {
	summary, err := Inspect(inspectDocument(t, `{}`), "empty.apmap", nil)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if summary.Entities != 0 || summary.Brushes != 0 || summary.Faces != 0 {
		t.Errorf("counts = %d/%d/%d, want zeroes", summary.Entities, summary.Brushes, summary.Faces)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(encoded), `"extensions":null`) {
		t.Errorf("extensions marshalled as null:\n%s", encoded)
	}
}

// TestInspectJSONKeyOrderIsFixed pins the one thing a struct-field reorder would
// silently break: the key order is part of the output contract, and
// encoding/json writes struct fields in declaration order.
func TestInspectJSONKeyOrderIsFixed(t *testing.T) {
	summary, err := Inspect(inspectDocument(t, inspectFixture), "fixture.apmap", nil)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}

	want := []string{
		"document", "apmap_version", "document_id", "revision", "role", "game",
		"id_policy", "frame", "entities", "brushes", "faces", "synthetic_faces",
		"relationships", "extensions",
	}
	position := 0
	for _, key := range want {
		index := strings.Index(string(encoded)[position:], `"`+key+`":`)
		if index < 0 {
			t.Fatalf("key %q is missing or out of order:\n%s", key, encoded)
		}
		position += index
	}
}

// TestInspectValidationIsOptAndOmitted proves the field is absent rather than
// null when no schema is supplied, so the CLI's default output keeps its shape.
func TestInspectValidationIsOptAndOmitted(t *testing.T) {
	summary, err := Inspect(inspectDocument(t, inspectFixture), "fixture.apmap", nil)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if summary.Validation != nil {
		t.Errorf("validation = %v with no schema, want nil", summary.Validation)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(encoded), "validation") {
		t.Errorf("the validation key appeared with no schema:\n%s", encoded)
	}
}

func TestInspectValidationCounts(t *testing.T) {
	schema, err := LoadSchema("")
	if err != nil {
		t.Skipf("the published APMap schema is not reachable: %v", err)
	}

	summary, err := Inspect(inspectDocument(t, inspectFixture), "fixture.apmap", schema)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if summary.Validation == nil {
		t.Fatal("a schema was supplied but no validation summary was produced")
	}
	// The fixture is deliberately not a real APMap document, so it must not
	// pass — a summary reporting `valid: true` here would mean the schema was
	// never actually applied.
	if summary.Validation.Valid {
		t.Error("the hand-built fixture validated, which means nothing was checked")
	}
	if summary.Validation.SchemaProblems == 0 && summary.Validation.SemanticProblems == 0 {
		t.Error("validation reported invalid with no problems counted")
	}
}

// TestInspectTextLinesRenderPythonNulls pins the text form's stand-in for an
// absent key: Python's spelling, None.
func TestInspectTextLinesRenderPythonNulls(t *testing.T) {
	summary, err := Inspect(inspectDocument(t, `{}`), "empty.apmap", nil)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	lines := strings.Join(summary.TextLines(), "\n")
	for _, want := range []string{"apmap None", "role=None", "to_source=None"} {
		if !strings.Contains(lines, want) {
			t.Errorf("the text form is missing %q:\n%s", want, lines)
		}
	}
	// Neither optional line appears when its count is zero.
	if strings.Contains(lines, "relationships=") || strings.Contains(lines, "extensions=") {
		t.Errorf("an empty document printed an optional line:\n%s", lines)
	}
}

func TestInspectTextLinesRenderTheTransform(t *testing.T) {
	summary, err := Inspect(inspectDocument(t, inspectFixture), "fixture.apmap", nil)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	lines := strings.Join(summary.TextLines(), "\n")
	// The translation is appended directly to the transform type with no
	// separator, rendered as a Python list repr.
	if !strings.Contains(lines, "to_source=translate[8, 0, -16]") {
		t.Errorf("the transform line does not have its fixed shape:\n%s", lines)
	}
	if !strings.Contains(lines, "relationships=1") || !strings.Contains(lines, "extensions=alpha, zeta") {
		t.Errorf("the optional lines are missing:\n%s", lines)
	}
}

// TestInspectFileReadsFromDisk covers the path-taking entry point, including
// the Name override the HTTP transport depends on to keep its temp path out of
// the response.
func TestInspectFileReadsFromDisk(t *testing.T) {
	path := t.TempDir() + "/doc.apmap"
	if err := writeFileForTest(path, inspectFixture); err != nil {
		t.Fatal(err)
	}

	summary, err := InspectFile(path, InspectOptions{})
	if err != nil {
		t.Fatalf("InspectFile: %v", err)
	}
	if summary.Document != path {
		t.Errorf("document = %q, want the path", summary.Document)
	}

	named, err := InspectFile(path, InspectOptions{Name: "client.apmap"})
	if err != nil {
		t.Fatalf("InspectFile: %v", err)
	}
	if named.Document != "client.apmap" {
		t.Errorf("document = %q, want the overriding name", named.Document)
	}
}

func TestInspectFileReportsAMalformedDocument(t *testing.T) {
	path := t.TempDir() + "/broken.apmap"
	if err := writeFileForTest(path, "{not json"); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectFile(path, InspectOptions{}); err == nil {
		t.Fatal("a malformed document must be an error")
	}
}
