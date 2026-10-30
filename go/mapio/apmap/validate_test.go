package apmap

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mustDecode parses a JSON literal used as a test input.
func mustDecode(t *testing.T, body string) Value {
	t.Helper()
	value, err := DecodeJSON([]byte(body))
	if err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return value
}

func mustDecodeSchema(t *testing.T, body string) *Object {
	t.Helper()
	schema, ok := mustDecode(t, body).(*Object)
	if !ok {
		t.Fatalf("%s is not a JSON object", body)
	}
	return schema
}

// validateProblems runs the validator and fails on a schema defect, for the
// cases where only the document's problems are under test.
func validateProblems(t *testing.T, document Value, schema *Object) []string {
	t.Helper()
	problems, err := ValidateAgainstSchema(document, schema)
	if err != nil {
		t.Fatalf("unexpected schema defect: %v", err)
	}
	return problems
}

// TestKeywordSubset covers every keyword the validator implements, in both
// directions. It is the table that says what the validator is: a schema using
// anything outside this set is a hard error, and a document violating anything
// inside it is a problem.
func TestKeywordSubset(t *testing.T) {
	cases := []struct {
		name     string
		schema   string
		document string
		problems int
		contains string
	}{
		{
			name: "type accepts", schema: `{"type": "object"}`,
			document: `{}`, problems: 0,
		},
		{
			name: "type rejects", schema: `{"type": "object"}`,
			document: `[]`, problems: 1, contains: "expected type object, got array",
		},
		{
			name: "type accepts a union", schema: `{"type": ["string", "null"]}`,
			document: `null`, problems: 0,
		},
		{
			name: "type rejects a union", schema: `{"type": ["string", "null"]}`,
			document: `7`, problems: 1, contains: "expected type string/null, got integer",
		},
		{
			name: "const accepts", schema: `{"const": "1.0"}`,
			document: `"1.0"`, problems: 0,
		},
		{
			name: "const rejects", schema: `{"const": "1.0"}`,
			document: `"1.1"`, problems: 1, contains: `expected the constant "1.0", got "1.1"`,
		},
		{
			name: "enum accepts", schema: `{"enum": ["a", "b"]}`,
			document: `"b"`, problems: 0,
		},
		{
			name: "enum rejects", schema: `{"enum": ["a", "b"]}`,
			document: `"c"`, problems: 1, contains: `"c" is not one of ["a", "b"]`,
		},
		{
			name: "required accepts", schema: `{"required": ["id"]}`,
			document: `{"id": "x"}`, problems: 0,
		},
		{
			name: "required rejects", schema: `{"required": ["id", "kind"]}`,
			document: `{"id": "x"}`, problems: 1, contains: `required member "kind" is missing`,
		},
		{
			name: "required ignores a non-object", schema: `{"required": ["id"]}`,
			document: `"not an object"`, problems: 0,
		},
		{
			name: "properties descends", schema: `{"properties": {"n": {"type": "integer"}}}`,
			document: `{"n": "no"}`, problems: 1, contains: "$.n: expected type integer",
		},
		{
			name: "properties skips absent members", schema: `{"properties": {"n": {"type": "integer"}}}`,
			document: `{}`, problems: 0,
		},
		{
			name:     "additionalProperties false rejects",
			schema:   `{"properties": {"a": {}}, "additionalProperties": false}`,
			document: `{"a": 1, "b": 2}`, problems: 1, contains: `member "b" is not allowed here`,
		},
		{
			name:     "additionalProperties as a schema applies to the extras only",
			schema:   `{"properties": {"a": {}}, "additionalProperties": {"type": "string"}}`,
			document: `{"a": 1, "b": 2}`, problems: 1, contains: "$.b: expected type string",
		},
		{
			name:     "propertyNames checks keys",
			schema:   `{"propertyNames": {"pattern": "^[a-z]+$"}}`,
			document: `{"ok": 1, "Bad": 2}`, problems: 1, contains: `$.<key "Bad">`,
		},
		{
			name: "items descends", schema: `{"items": {"type": "integer"}}`,
			document: `[1, "two", 3]`, problems: 1, contains: "$[1]: expected type integer",
		},
		{
			name: "minItems rejects", schema: `{"minItems": 4}`,
			document: `[1, 2, 3]`, problems: 1, contains: "needs at least 4 item(s), has 3",
		},
		{
			name: "maxItems rejects", schema: `{"maxItems": 2}`,
			document: `[1, 2, 3]`, problems: 1, contains: "allows at most 2 item(s), has 3",
		},
		{
			name: "minProperties rejects an empty object", schema: `{"minProperties": 1}`,
			document: `{}`, problems: 1, contains: "needs at least 1 member(s), has 0",
		},
		{
			name: "minProperties accepts the boundary", schema: `{"minProperties": 1}`,
			document: `{"author": ""}`, problems: 0,
		},
		{
			name: "minProperties ignores a non-object", schema: `{"minProperties": 1}`,
			document: `[]`, problems: 0,
		},
		{
			name: "maxProperties rejects", schema: `{"maxProperties": 1}`,
			document: `{"a": 1, "b": 2}`, problems: 1, contains: "allows at most 1 member(s), has 2",
		},
		{
			name: "maxProperties accepts the boundary", schema: `{"maxProperties": 2}`,
			document: `{"a": 1, "b": 2}`, problems: 0,
		},
		{
			name: "minLength rejects", schema: `{"minLength": 1}`,
			document: `""`, problems: 1, contains: "needs at least 1 character(s)",
		},
		{
			name: "minLength counts characters not bytes", schema: `{"minLength": 3}`,
			document: `"héé"`, problems: 0,
		},
		{
			name: "minimum rejects", schema: `{"minimum": 0}`,
			document: `-1`, problems: 1, contains: "-1 is less than the minimum 0",
		},
		{
			name: "minimum accepts the boundary", schema: `{"minimum": 0}`,
			document: `0`, problems: 0,
		},
		{
			name: "maximum rejects", schema: `{"maximum": 10}`,
			document: `11`, problems: 1, contains: "11 is more than the maximum 10",
		},
		{
			name: "pattern rejects", schema: `{"pattern": "^ent_[0-9A-Za-z]{8,64}$"}`,
			document: `"brs_0123456789abcdef"`, problems: 1, contains: "does not match",
		},
		{
			name: "pattern accepts", schema: `{"pattern": "^ent_[0-9A-Za-z]{8,64}$"}`,
			document: `"ent_0123456789abcdef"`, problems: 0,
		},
		{
			name:     "oneOf accepts exactly one branch",
			schema:   `{"oneOf": [{"type": "string"}, {"type": "integer"}]}`,
			document: `"x"`, problems: 0,
		},
		{
			name:     "oneOf rejects no branch",
			schema:   `{"oneOf": [{"type": "string"}, {"type": "integer"}]}`,
			document: `[]`, problems: 1, contains: "matches no branch of oneOf",
		},
		{
			name:     "oneOf rejects two branches",
			schema:   `{"oneOf": [{"type": "integer"}, {"minimum": 0}]}`,
			document: `1`, problems: 1, contains: "matches 2 branches of oneOf",
		},
		{
			name:     "anyOf accepts more than one branch",
			schema:   `{"anyOf": [{"type": "integer"}, {"minimum": 0}]}`,
			document: `1`, problems: 0,
		},
		{
			name:     "anyOf rejects no branch",
			schema:   `{"anyOf": [{"type": "string"}, {"type": "boolean"}]}`,
			document: `1`, problems: 1, contains: "matches no branch of anyOf",
		},
		{
			name:     "allOf applies every branch",
			schema:   `{"allOf": [{"type": "integer"}, {"minimum": 5}]}`,
			document: `1`, problems: 1, contains: "less than the minimum 5",
		},
		{
			name:     "$ref resolves locally",
			schema:   `{"$defs": {"id": {"type": "string"}}, "properties": {"a": {"$ref": "#/$defs/id"}}}`,
			document: `{"a": 1}`, problems: 1, contains: "$.a: expected type string",
		},
		{
			name:     "a sibling of $ref is ignored",
			schema:   `{"$defs": {"id": {"type": "string"}}, "properties": {"a": {"$ref": "#/$defs/id", "description": "an id"}}}`,
			document: `{"a": "x"}`, problems: 0,
		},
		{
			name:     "every ignored annotation keyword is inert",
			schema:   `{"$schema": "x", "$id": "y", "title": "t", "description": "d", "examples": [1], "default": 2, "deprecated": true, "type": "string"}`,
			document: `"ok"`, problems: 0,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			schema := mustDecodeSchema(t, testCase.schema)
			document := mustDecode(t, testCase.document)
			problems := validateProblems(t, document, schema)

			if len(problems) != testCase.problems {
				t.Fatalf("got %d problem(s), want %d:\n  %s",
					len(problems), testCase.problems, strings.Join(problems, "\n  "))
			}
			if testCase.contains != "" && !strings.Contains(problems[0], testCase.contains) {
				t.Fatalf("problem %q should contain %q", problems[0], testCase.contains)
			}
		})
	}
}

// TestUnsupportedKeywordIsRejected answers the prompt's "check, don't assume"
// question about what happens outside the implemented set.
//
// The validator has no fall-through: an unrecognised keyword returns a
// SchemaNotFoundError with a message telling the reader to extend the
// validator. It does *not* silently ignore it. That is the safe direction — a
// silently ignored keyword is a constraint the specification imposes and the
// validator does not enforce, so an invalid document would be accepted with no
// signal anywhere. This test pins the loud behaviour, using keywords a
// "complete" JSON Schema implementation would have and this one deliberately
// does not.
func TestUnsupportedKeywordIsRejected(t *testing.T) {
	// `uniqueItems` and `maxLength` used to be in this list and are now implemented — APMap 1.2's
	// `groups` is the first contract to use either, and refusing them meant refusing every document
	// with a named group. What the list is FOR is unchanged: keywords a "complete" JSON Schema
	// implementation would have and this one deliberately does not, so that meeting one is loud.
	for _, keyword := range []string{"format", "if", "prefixItems", "not", "multipleOf", "exclusiveMinimum", "contains", "dependentSchemas"} {
		t.Run(keyword, func(t *testing.T) {
			schema := NewObject()
			schema.Set(keyword, "whatever")

			problems, err := ValidateAgainstSchema(mustDecode(t, `"x"`), schema)
			if err == nil {
				t.Fatalf("%q must be rejected as unsupported, got %d problem(s) instead", keyword, len(problems))
			}
			var notFoundError *SchemaNotFoundError
			if !errors.As(err, &notFoundError) {
				t.Fatalf("got %T, want *SchemaNotFoundError", err)
			}
			if !strings.Contains(err.Error(), "unsupported keyword") || !strings.Contains(err.Error(), keyword) {
				t.Fatalf("the message should name the unsupported keyword:\n%v", err)
			}
			if problems != nil {
				t.Fatal("a schema defect must not be reported as a document problem")
			}
		})
	}
}

func TestUnsupportedTypeNameIsRejected(t *testing.T) {
	schema := mustDecodeSchema(t, `{"type": "decimal"}`)
	_, err := ValidateAgainstSchema(mustDecode(t, `1`), schema)

	var notFoundError *SchemaNotFoundError
	if !errors.As(err, &notFoundError) {
		t.Fatalf("got %v (%T), want *SchemaNotFoundError", err, err)
	}
	if !strings.Contains(err.Error(), "unsupported schema type: decimal") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestBadRefIsASchemaDefect(t *testing.T) {
	cases := []struct {
		name     string
		schema   string
		contains string
	}{
		{
			name:     "remote ref",
			schema:   `{"$ref": "https://example.invalid/other.json"}`,
			contains: "unsupported $ref (only local refs)",
		},
		{
			name:     "dangling ref",
			schema:   `{"$defs": {}, "$ref": "#/$defs/missing"}`,
			contains: "$ref does not resolve",
		},
		{
			name:     "ref to a non-schema",
			schema:   `{"$defs": {"n": 1}, "$ref": "#/$defs/n"}`,
			contains: "$ref does not point at a schema",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			schema := mustDecodeSchema(t, testCase.schema)
			_, err := ValidateAgainstSchema(mustDecode(t, `{}`), schema)
			var notFoundError *SchemaNotFoundError
			if !errors.As(err, &notFoundError) {
				t.Fatalf("got %v (%T), want *SchemaNotFoundError", err, err)
			}
			if !strings.Contains(err.Error(), testCase.contains) {
				t.Fatalf("message %q should contain %q", err, testCase.contains)
			}
		})
	}
}

// TestRefEscapes covers the two JSON Pointer escapes, which a hand-written
// pointer walker is easy to get wrong.
func TestRefEscapes(t *testing.T) {
	schema := mustDecodeSchema(t, `{"$defs": {"a/b": {"type": "string"}, "c~d": {"type": "integer"}}, "properties": {"x": {"$ref": "#/$defs/a~1b"}, "y": {"$ref": "#/$defs/c~0d"}}}`)
	problems := validateProblems(t, mustDecode(t, `{"x": 1, "y": "no"}`), schema)
	if len(problems) != 2 {
		t.Fatalf("got %d problem(s), want 2:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}

// TestIntegerVersusNumber pins the number typing, which is the subtlest part of
// the validator because the int/float distinction is made at the *literal*
// level and Go's encoding/json does not make it.
func TestIntegerVersusNumber(t *testing.T) {
	cases := []struct {
		literal string
		integer bool
		number  bool
		name    string
	}{
		{literal: `3`, integer: true, number: true, name: "integer"},
		{literal: `-1`, integer: true, number: true, name: "integer"},
		{literal: `3.0`, integer: true, number: true, name: "number"},
		{literal: `3.5`, integer: false, number: true, name: "number"},
		{literal: `3e2`, integer: true, number: true, name: "number"},
		{literal: `3.5e1`, integer: true, number: true, name: "number"},
		{literal: `1e400`, integer: false, number: true, name: "number"},
		{literal: `true`, integer: false, number: false, name: "boolean"},
		{literal: `"3"`, integer: false, number: false, name: "string"},
		{literal: `null`, integer: false, number: false, name: "null"},
	}

	integerSchema := mustDecodeSchema(t, `{"type": "integer"}`)
	numberSchema := mustDecodeSchema(t, `{"type": "number"}`)

	for _, testCase := range cases {
		t.Run(testCase.literal, func(t *testing.T) {
			value := mustDecode(t, testCase.literal)

			if got := len(validateProblems(t, value, integerSchema)) == 0; got != testCase.integer {
				t.Errorf("%s as integer = %v, want %v", testCase.literal, got, testCase.integer)
			}
			if got := len(validateProblems(t, value, numberSchema)) == 0; got != testCase.number {
				t.Errorf("%s as number = %v, want %v", testCase.literal, got, testCase.number)
			}
			if got := typeName(value); got != testCase.name {
				t.Errorf("typeName(%s) = %q, want %q", testCase.literal, got, testCase.name)
			}
		})
	}
}

// TestBooleanIsNeverANumber: the rule falls out of the type system in Go,
// which is exactly why it is worth a test: nothing in the code draws
// attention to the rule, so a future refactor that started treating JSON
// booleans as 0/1 would have nothing else to trip over.
func TestBooleanIsNeverANumber(t *testing.T) {
	for _, schemaBody := range []string{`{"type": "integer"}`, `{"type": "number"}`} {
		schema := mustDecodeSchema(t, schemaBody)
		if problems := validateProblems(t, mustDecode(t, `true`), schema); len(problems) != 1 {
			t.Fatalf("true must not satisfy %s, got %d problem(s)", schemaBody, len(problems))
		}
	}
}

// TestOneOfDoesNotLeakBranchProblems pins collect. A branch that fails is
// not the document's fault — only "no branch matched" is — so the branch's own
// problems must never reach the document's list. Without this the failure of
// the taken branch and every untaken one would be reported together, which is
// how a two-branch oneOf turns one violation into three.
func TestOneOfDoesNotLeakBranchProblems(t *testing.T) {
	schema := mustDecodeSchema(t, `{"oneOf": [{"type": "string", "minLength": 5}, {"type": "integer", "minimum": 100}]}`)
	problems := validateProblems(t, mustDecode(t, `7`), schema)

	if len(problems) != 1 {
		t.Fatalf("got %d problem(s), want exactly 1:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	if strings.Contains(problems[0], "minLength") || strings.Contains(problems[0], "minimum") {
		t.Fatalf("a branch's internal problem leaked into the document's list: %q", problems[0])
	}
}

// TestTypeFailureStopsTheNode pins the early return after a type mismatch.
// Reporting "expected an object, and also it is missing three required
// members" for a string would be noise, and every keyword after `type` assumes
// the type held.
func TestTypeFailureStopsTheNode(t *testing.T) {
	schema := mustDecodeSchema(t, `{"type": "object", "required": ["a", "b", "c"], "additionalProperties": false}`)
	problems := validateProblems(t, mustDecode(t, `"a string"`), schema)

	if len(problems) != 1 {
		t.Fatalf("got %d problem(s), want exactly 1:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	if !strings.Contains(problems[0], "expected type object") {
		t.Fatalf("unexpected problem: %q", problems[0])
	}
}

// TestProblemOrderIsDeterministic is why this package decodes objects into an
// ordered type rather than a Go map. Determinism is an AGENTS.md §3 principle,
// and a map-based port would pass every other test in this file while
// shuffling its output on each run.
func TestProblemOrderIsDeterministic(t *testing.T) {
	schema := mustDecodeSchema(t, `{"type": "object", "properties": {"a": {"type": "string"}, "b": {"type": "string"}, "c": {"type": "string"}, "d": {"type": "string"}, "e": {"type": "string"}}, "additionalProperties": false}`)
	document := mustDecode(t, `{"e": 1, "d": 2, "c": 3, "b": 4, "a": 5, "z": 6, "y": 7}`)

	first := validateProblems(t, document, schema)
	if len(first) != 7 {
		t.Fatalf("got %d problem(s), want 7:\n  %s", len(first), strings.Join(first, "\n  "))
	}
	for attempt := 0; attempt < 50; attempt++ {
		again := validateProblems(t, document, schema)
		if strings.Join(again, "\n") != strings.Join(first, "\n") {
			t.Fatalf("problem order changed between runs:\n  %s\nversus\n  %s",
				strings.Join(first, "\n  "), strings.Join(again, "\n  "))
		}
	}

	// Schema order drives `properties`, document order drives the extras.
	want := []string{"$.a", "$.b", "$.c", "$.d", "$.e", "$", "$"}
	for index, problem := range first {
		if location := problemLocation(problem); location != want[index] {
			t.Fatalf("problem %d at %q, want %q\n  %s", index, location, want[index], problem)
		}
	}
	if !strings.Contains(first[5], `"z"`) || !strings.Contains(first[6], `"y"`) {
		t.Fatalf("the undeclared members should be reported in document order, got:\n  %s\n  %s", first[5], first[6])
	}
}

// TestDuplicateKeysKeepTheLastValue matches Python's json and every other
// mainstream JSON reader. The APMap schema forbids what a duplicate key would
// express, but the decoder has to make *some* choice, and keeping the first
// would make AUE read a different document from the one other readers see.
func TestDuplicateKeysKeepTheLastValue(t *testing.T) {
	document := mustDecode(t, `{"a": 1, "b": 2, "a": 3}`).(*Object)

	if got := document.Keys(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("keys = %v, want [a b] — a repeated key keeps its first position", got)
	}
	value, _ := document.Get("a")
	if render(value) != "3" {
		t.Fatalf("a = %s, want the last value 3", render(value))
	}
}

func TestValidateAgainstSchemaRejectsANilSchema(t *testing.T) {
	_, err := ValidateAgainstSchema(mustDecode(t, `{}`), nil)
	var notFoundError *SchemaNotFoundError
	if !errors.As(err, &notFoundError) {
		t.Fatalf("got %v (%T), want *SchemaNotFoundError", err, err)
	}
}

// TestSchemaErrorMessage covers the ten-problem cap, which exists so a
// document with hundreds of violations produces a readable message rather than
// a wall of text.
func TestSchemaErrorMessage(t *testing.T) {
	problems := make([]string, 0, 12)
	for index := 0; index < 12; index++ {
		problems = append(problems, "problem")
	}
	message := NewSchemaError(problems, "doc.apmap").Error()

	if !strings.HasPrefix(message, "doc.apmap: 12 schema violation(s):") {
		t.Fatalf("unexpected prefix:\n%s", message)
	}
	if !strings.HasSuffix(message, "... and 2 more") {
		t.Fatalf("the message should cap at ten and count the rest:\n%s", message)
	}
	if strings.Count(message, "problem") != 10 {
		t.Fatalf("the message should show exactly ten problems:\n%s", message)
	}

	if NewSchemaError(nil, "").Error() != "<apmap>: 0 schema violation(s):\n  " {
		t.Fatalf("an empty problem list should still use the default source, got %q", NewSchemaError(nil, "").Error())
	}
	if !strings.Contains(NewSemanticError([]string{"x"}, "d").Error(), "1 semantic violation(s)") {
		t.Fatal("SemanticError should say semantic, not schema")
	}
}

// TestErrorHierarchy is the Go spelling of Python's `except APMapError`.
func TestErrorHierarchy(t *testing.T) {
	for _, err := range []error{
		&VersionError{Message: "v"},
		NewSchemaError([]string{"p"}, "s"),
		NewSemanticError([]string{"p"}, "s"),
		notFound("n"),
		&SchemaNotFoundError{Message: "n", Cause: errors.New("cause")},
	} {
		if !errors.Is(err, ErrAPMap) {
			t.Fatalf("%T does not satisfy errors.Is(err, ErrAPMap)", err)
		}
	}
}

func TestDecodeJSONRejectsMalformedInput(t *testing.T) {
	for _, body := range []string{`{`, `{"a":}`, `[1,]`, ``, `{} trailing`, `{"a": 1} {"b": 2}`} {
		if _, err := DecodeJSON([]byte(body)); err == nil {
			t.Fatalf("DecodeJSON(%q) should have failed", body)
		}
	}
}

// TestDecodeJSONShapes pins the decoded representation, which the validator's
// every type assertion depends on.
func TestDecodeJSONShapes(t *testing.T) {
	value := mustDecode(t, `{"o": {}, "a": [], "s": "x", "n": 1, "f": 1.5, "b": true, "z": null}`)
	object, ok := value.(*Object)
	if !ok {
		t.Fatalf("got %T, want *Object", value)
	}

	for name, want := range map[string]string{
		"o": "object", "a": "array", "s": "string",
		"n": "integer", "f": "number", "b": "boolean", "z": "null",
	} {
		member, present := object.Get(name)
		if !present {
			t.Fatalf("member %q is missing", name)
		}
		if got := typeName(member); got != want {
			t.Errorf("%q decoded as %s, want %s", name, got, want)
		}
	}

	// An empty array is a non-nil []Value, so it is distinguishable from an
	// absent one after a type assertion — minItems 1 has to see it.
	empty, _ := object.Get("a")
	if items, ok := empty.([]Value); !ok || items == nil {
		t.Fatalf("an empty array should decode to a non-nil []Value, got %#v", empty)
	}
}

// TestObjectKeysIsACopy stops a caller from reordering an object by sorting
// the slice it got back, which would silently break determinism everywhere.
func TestObjectKeysIsACopy(t *testing.T) {
	object := mustDecode(t, `{"b": 1, "a": 2}`).(*Object)
	keys := object.Keys()
	keys[0], keys[1] = keys[1], keys[0]

	if again := object.Keys(); again[0] != "b" || again[1] != "a" {
		t.Fatalf("mutating the returned slice reordered the object: %v", again)
	}
}

// TestMinPropertiesBoundMustBeAnInteger: a malformed bound is a schema defect, never a document
// problem — the same rule every other integer-bounded keyword follows.
func TestMinPropertiesBoundMustBeAnInteger(t *testing.T) {
	for _, keyword := range []string{"minProperties", "maxProperties"} {
		schema := mustDecodeSchema(t, `{"`+keyword+`": "one"}`)
		problems, err := ValidateAgainstSchema(mustDecode(t, `{}`), schema)
		var notFoundError *SchemaNotFoundError
		if err == nil || !errors.As(err, &notFoundError) || problems != nil {
			t.Fatalf("%s with a non-number bound: err=%v problems=%v", keyword, err, problems)
		}
	}
}

// TestAuthorshipPatternsMatchLikeECMAScript pins the three APMap 1.4 `authorship` patterns under
// Go's RE2 against what an ECMAScript RegExp (Ajv, which AULIBS proves its vectors with) does with
// them: `\xHH` is the code point U+00HH — so U+009F is refused while the UTF-8 continuation byte
// 0x9F inside an ordinary letter is not a thing the pattern can see — and `$` is the end of the
// text, not "before a trailing newline" as Python's is.
func TestAuthorshipPatternsMatchLikeECMAScript(t *testing.T) {
	const author = `^[^\x00-\x1F\x7F-\x9F]*$`
	const multiline = `^[^\x00-\x08\x0B-\x1F\x7F-\x9F]*$`
	cases := []struct {
		pattern string
		text    string
		match   bool
	}{
		{author, "Zoë O'Neil & Bob \"the Builder\"", true},
		{author, "\u00df\u00e9\u015f\u9ad8\u6a4b \u2014 ok", true},
		{author, "ends in a newline\n", false}, // Python's $ would accept this; ECMA and RE2 do not
		{author, "tab\tinside", false},
		{author, "del\u007f", false},
		{author, "c1\u0080", false},
		{author, "c1\u009f", false},
		{author, "just past C1\u00a0", true},
		{author, "\u011f contains the byte 0x9F in UTF-8", true},
		{author, "", true},
		{multiline, "© 2026 Zoë.\nAll rights reserved.", true},
		{multiline, "blank\n\nline and\ta tab", true},
		{multiline, "crlf\r\n", false},
		{multiline, "bell\u0007", false},
		{multiline, "vt\u000b", false},
		{multiline, "c1\u0085", false},
	}
	for _, testCase := range cases {
		expression, err := compilePattern(testCase.pattern)
		if err != nil {
			t.Fatalf("%s does not compile under RE2: %v", testCase.pattern, err)
		}
		if got := expression.MatchString(testCase.text); got != testCase.match {
			t.Errorf("%s on %q: matched %v, want %v", testCase.pattern, testCase.text, got, testCase.match)
		}
	}
}

// A sub-schema is checked by its own checker, and a large document reaches the context from inside
// one: a real Quake II map did, and the sub-checker had no context to read.
func TestCancellationReachesEverySubSchema(t *testing.T) {
	inner := make([]Value, 3*checkEvery)
	for index := range inner {
		inner[index] = "a"
	}
	schema := mustDecodeSchema(t, `{"type": "array", "items": {"oneOf": [{"type": "number"}, {"type": "string"}]}}`)
	nested := mustDecodeSchema(t, `{"oneOf": [{"type": "array", "items": {"type": "string"}}, {"type": "number"}]}`)

	if problems, err := ValidateAgainstSchemaContext(context.Background(), inner, nested); err != nil || len(problems) != 0 {
		t.Fatalf("problems %v, err %v", problems, err)
	}
	if problems, err := ValidateAgainstSchemaContext(context.Background(), inner, schema); err != nil || len(problems) != 0 {
		t.Fatalf("problems %v, err %v", problems, err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ValidateAgainstSchemaContext(cancelled, inner, nested); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled validation inside a sub-schema returned %v", err)
	}
}
