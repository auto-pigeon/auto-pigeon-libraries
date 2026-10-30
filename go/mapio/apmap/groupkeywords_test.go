package apmap_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/internal/apmaptest"
)

// PREPROD-03C. The built-in validator has to be able to READ the contract, which is a different
// question from whether a document satisfies it — and it is the question no unit test was asking.
//
// APMap 1.2's `groups` introduced two JSON Schema keywords no earlier contract had used,
// `maxLength` and `uniqueItems`. This validator answers an unknown keyword by refusing the SCHEMA,
// which is right — silently ignoring a constraint would report an invalid document as valid — but
// it meant every document with a named group was refused with
//
//	the APMap schema uses an unsupported keyword "maxLength" at $.groups[0].name;
//	the built-in validator must be extended before this schema can be used
//
// A whole-corpus test missed it because an empty `groups: []` never reaches `groups[0].name`. Only a
// real extraction of a map that actually had a group did.

// TestEveryKeywordInTheBundleIsImplemented is the general form, and the one that matters for the
// next promotion: it fails when a contract in the bundle uses anything this validator cannot read,
// whether or not any test document happens to reach it.
func TestEveryKeywordInTheBundleIsImplemented(t *testing.T) {
	root := apmaptest.PackageRoot()
	if root == "" {
		t.Skip("the auto-pigeon-libraries contract package is not beside this checkout")
	}
	schemaDir := apmaptest.SchemaDir()

	var files []string
	for _, pattern := range []string{"apmap-*.schema.json", "deprecated/apmap-*.schema.json"} {
		matched, err := filepath.Glob(filepath.Join(schemaDir, pattern))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matched...)
	}
	if len(files) == 0 {
		t.Fatalf("no schemas found under %s", schemaDir)
	}

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s is not JSON: %v", filepath.Base(file), err)
		}
		for _, keyword := range keywordsIn(decoded) {
			if !implementedKeywords[keyword] {
				t.Errorf("%s uses %q, which the built-in validator does not implement; "+
					"extend ValidateAgainstSchema and add it to implementedKeywords",
					filepath.Base(file), keyword)
			}
		}
	}
}

// implementedKeywords is what ValidateAgainstSchema's switch handles, plus the annotations that
// constrain nothing. Kept beside the test rather than exported from the validator: the switch is
// the implementation, and a separate list is what makes a drift between the two visible.
var implementedKeywords = map[string]bool{
	"type": true, "const": true, "enum": true, "required": true, "properties": true,
	"additionalProperties": true, "propertyNames": true, "items": true, "minItems": true,
	"maxItems": true, "minLength": true, "maxLength": true, "uniqueItems": true,
	"minProperties": true, "maxProperties": true,
	"minimum": true, "maximum": true, "pattern": true, "oneOf": true, "anyOf": true, "allOf": true,
	// Annotations and identifiers: read past, never enforced.
	"title": true, "description": true, "examples": true, "default": true, "deprecated": true,
}

// keywordsIn collects every schema keyword used anywhere in a schema document.
//
// It descends only where a SCHEMA lives, so a `properties` member literally named "type" is not
// mistaken for the keyword.
func keywordsIn(node any) []string {
	var out []string
	var walk func(any)
	walk = func(current any) {
		switch value := current.(type) {
		case map[string]any:
			for key, child := range value {
				if strings.HasPrefix(key, "$") {
					if key == "$defs" || key == "definitions" {
						if nested, ok := child.(map[string]any); ok {
							for _, sub := range nested {
								walk(sub)
							}
						}
					}
					continue
				}
				out = append(out, key)
				switch key {
				case "properties", "patternProperties":
					if nested, ok := child.(map[string]any); ok {
						for _, sub := range nested {
							walk(sub)
						}
					}
				case "items", "additionalProperties", "propertyNames", "not", "if", "then", "else",
					"contains":
					walk(child)
				case "oneOf", "anyOf", "allOf", "prefixItems":
					if nested, ok := child.([]any); ok {
						for _, sub := range nested {
							walk(sub)
						}
					}
				}
			}
		case []any:
			for _, sub := range value {
				walk(sub)
			}
		}
	}
	walk(node)

	return out
}

// TestGroupBearingVectorsValidate is the specific form: the real conformance vectors for `groups`,
// through this validator, against the contract that declares them.
//
// A schema-defect error and a document problem are different failures and this separates them —
// the bug was the former, reported through a path that looks like the latter.
func TestGroupBearingVectorsValidate(t *testing.T) {
	root := apmaptest.PackageRoot()
	if root == "" {
		t.Skip("the auto-pigeon-libraries contract package is not beside this checkout")
	}

	for _, name := range []string{
		"group-two-brushes.apmap", "group-two-groups.apmap", "group-mixed-members.apmap",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(apmaptest.Vectors(), "valid", name)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("vector %s is not present: %v", name, err)
			}
			document, err := apmap.DecodeJSON(raw)
			if err != nil {
				t.Fatalf("%s is not valid JSON: %v", name, err)
			}
			schema, err := apmap.SchemaForDocument(document)
			if err != nil {
				t.Fatalf("resolving the contract for %s: %v", name, err)
			}
			problems, err := apmap.ValidateAgainstSchema(document, schema)
			if err != nil {
				t.Fatalf("the validator could not READ the contract this vector declares: %v", err)
			}
			if len(problems) > 0 {
				t.Errorf("a valid vector was reported invalid: %s", strings.Join(problems, "; "))
			}
		})
	}
}

// TestTheNewKeywordsActuallyConstrain proves the two additions do work rather than merely being
// accepted and ignored — which is the failure mode a "just add it to the switch" fix invites.
func TestTheNewKeywordsActuallyConstrain(t *testing.T) {
	root := apmaptest.PackageRoot()
	if root == "" {
		t.Skip("the auto-pigeon-libraries contract package is not beside this checkout")
	}

	for _, testCase := range []struct {
		vector string
		want   string
	}{
		// uniqueItems: two identical members in one group.
		{"group-duplicate-member.apmap", "duplicate"},
		// A blank group name. Not a length rule at all, as it turns out — the contract catches it
		// with `pattern: \S`, so this case is here to record which mechanism actually fires rather
		// than to assert the one that sounds likely.
		{"group-empty-name.apmap", "does not match"},
	} {
		t.Run(testCase.vector, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(apmaptest.Vectors(), "invalid", testCase.vector))
			if err != nil {
				t.Skipf("vector %s is not present: %v", testCase.vector, err)
			}
			document, err := apmap.DecodeJSON(raw)
			if err != nil {
				t.Fatalf("%s is not valid JSON: %v", testCase.vector, err)
			}
			schema, err := apmap.SchemaForDocument(document)
			if err != nil {
				t.Fatalf("resolving the contract: %v", err)
			}
			problems, err := apmap.ValidateAgainstSchema(document, schema)
			if err != nil {
				t.Fatalf("the validator could not read the contract: %v", err)
			}
			if len(problems) == 0 {
				t.Fatalf("%s was reported valid", testCase.vector)
			}
			if !strings.Contains(strings.ToLower(strings.Join(problems, "; ")), testCase.want) {
				t.Errorf("refused for the wrong reason: %s", strings.Join(problems, "; "))
			}
		})
	}
}

// TestALongGroupNameIsRefused exercises maxLength directly, since no shipped vector does.
func TestALongGroupNameIsRefused(t *testing.T) {
	root := apmaptest.PackageRoot()
	if root == "" {
		t.Skip("the auto-pigeon-libraries contract package is not beside this checkout")
	}
	raw, err := os.ReadFile(filepath.Join(apmaptest.Vectors(), "valid", "group-two-brushes.apmap"))
	if err != nil {
		t.Skipf("the group vector is not present: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	groups, ok := decoded["groups"].([]any)
	if !ok || len(groups) == 0 {
		t.Fatal("the vector declares no groups")
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatal("the vector's group is not an object")
	}
	group["name"] = strings.Repeat("x", 4096)
	overlong, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	document, err := apmap.DecodeJSON(overlong)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := apmap.SchemaForDocument(document)
	if err != nil {
		t.Fatalf("resolving the contract: %v", err)
	}
	problems, err := apmap.ValidateAgainstSchema(document, schema)
	if err != nil {
		t.Fatalf("the validator could not read the contract: %v", err)
	}
	if len(problems) == 0 {
		t.Fatal("a 4096-character group name was accepted; maxLength is not enforced")
	}
	if !strings.Contains(strings.Join(problems, "; "), "at most") {
		t.Errorf("refused for the wrong reason: %s", strings.Join(problems, "; "))
	}
}
