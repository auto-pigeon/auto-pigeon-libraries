package apmap

import (
	"fmt"
	"sort"
	"strings"
)

// Summary is what `apmap-inspect` reports for one document.
//
// # Field order
//
// Declaration order is JSON output order, and the order is part of the output
// contract: unlike modelfmt.Record and entitygraph's wire types, this one is
// declared in reading order rather than alphabetically. Reordering these fields
// changes the output.
//
// # Nullability and type fidelity
//
// Every scalar is reported *as the document has it*, so these are `any` rather
// than `*string`. Two things depend on that:
//
//   - `revision` is a JSON *number* in a real document, not a string. A
//     string-only accessor reported `null` for every document that had one.
//   - a key the document omits reports `null`, which `any` gives for free, and
//     is still distinguishable from a key declared as `""`.
//
// Nothing here interprets these values; an inspection reports what the document
// says.
type Summary struct {
	Document       string       `json:"document"`
	APMapVersion   any          `json:"apmap_version"`
	DocumentID     any          `json:"document_id"`
	Revision       any          `json:"revision"`
	Role           any          `json:"role"`
	Game           any          `json:"game"`
	IDPolicy       any          `json:"id_policy"`
	Frame          SummaryFrame `json:"frame"`
	Entities       int          `json:"entities"`
	Brushes        int          `json:"brushes"`
	Faces          int          `json:"faces"`
	SyntheticFaces int          `json:"synthetic_faces"`
	Relationships  int          `json:"relationships"`
	// Extensions is the sorted list of extension names — the keys, not the
	// values, because an inspection is a shape report and an extension body is
	// arbitrary.
	Extensions []string `json:"extensions"`
	// Validation is present only when the caller asked for it (the inspection names a
	// validation summary among the facts an inspection should carry). It is a
	// separate, omitted-when-absent field so that an inspection without
	// validation keeps exactly the shape above.
	Validation *ValidationSummary `json:"validation,omitempty"`
}

// ValidationSummary is the compact form of a schema+semantic validation pass.
// The problems themselves are not repeated here — `apmap-validate` is the
// command that prints them, and duplicating its output would make two commands
// responsible for one contract.
type ValidationSummary struct {
	Valid            bool `json:"valid"`
	SchemaProblems   int  `json:"schema_problems"`
	SemanticProblems int  `json:"semantic_problems"`
}

// SummaryFrame is the summary's nested frame object: exactly two of the
// frame's keys.
//
// ToSource is a plain Go value produced by Plain, never an *Object. Object's
// members are unexported, so encoding/json writes it as `{}` — a real
// document's `{"type": "identity"}` would be reported as an empty object by the
// CLI's --json path.
type SummaryFrame struct {
	Space    any `json:"space"`
	ToSource any `json:"to_source"`
}

// InspectOptions configures Inspect.
type InspectOptions struct {
	// Name is what the summary reports as `document`. Empty means the caller's own name for it.
	Name string
	// Schema, when non-nil, adds the Validation field. Nil omits it entirely.
	Schema *Object
	// MatchDocumentVersion resolves the deployment's contract FOR THIS DOCUMENT when Schema is nil.
	//
	// It exists because the obvious spelling of "validate with the deployment's schema" —
	// `LoadSchema("")` once, then hand the result to every document — silently converts
	// version-matched validation into current-only validation. SchemaForDocument's own comment
	// records six call sites that made exactly that mistake; `apmap-inspect --validate` was still
	// one of them, and it reported the published, valid APMap 1.0 and 1.1 vectors as invalid for
	// `groups`, a member neither contract has a word for.
	//
	// A version this build cannot read does not fail the inspection. Inspecting is how somebody
	// finds out WHY a document is unusable, so the refusal is reported as the validation problem
	// and the summary is still produced.
	MatchDocumentVersion bool
}

// InspectWith is Inspect with the option set, so the version-matched schema resolution lives in one
// place rather than at each transport.
func InspectWith(document *Object, name string, options InspectOptions) (Summary, error) {
	schema := options.Schema
	if schema == nil && options.MatchDocumentVersion {
		resolved, err := SchemaForDocument(document)
		if err != nil {
			summary, inspectErr := Inspect(document, name, nil)
			if inspectErr != nil {
				return Summary{}, inspectErr
			}
			// One schema problem: the document names a contract this build does not hold, which
			// is a failure to be READABLE and is counted as such. ValidationSummary carries counts
			// only — the inspection's shape, deliberately not widened — so the reason reaches a human
			// through the caller's own error reporting rather than through a new field.
			summary.Validation = &ValidationSummary{
				Valid: false, SchemaProblems: 1, SemanticProblems: 0,
			}

			return summary, nil
		}
		schema = resolved
	}

	return Inspect(document, name, schema)
}

// Inspect summarises an already-decoded document.
func Inspect(document *Object, name string, schema *Object) (Summary, error) {
	summary := Summary{
		Document:     name,
		APMapVersion: plainAt(document, "apmap_version"),
		DocumentID:   plainAt(document, "document_id"),
		Revision:     plainAt(document, "revision"),
		Role:         plainAt(document, "role"),
		Game:         plainAt(document, "game"),
		IDPolicy:     plainAt(document, "id_policy"),
		Extensions:   []string{},
	}

	if frame, ok := objectAt(document, "frame"); ok {
		summary.Frame.Space = plainAt(frame, "space")
		summary.Frame.ToSource = plainAt(frame, "to_source")
	}

	entities, _ := arrayAt(document, "entities")
	summary.Entities = len(entities)
	for _, entry := range entities {
		entity, ok := entry.(*Object)
		if !ok {
			continue
		}
		content, _ := arrayAt(entity, "content")
		for _, item := range content {
			brush, ok := item.(*Object)
			if !ok {
				continue
			}
			// Only a member whose kind is "brush" is a brush.
			if kind := optionalString(brush, "kind"); kind == nil || *kind != "brush" {
				continue
			}
			summary.Brushes++
			faces, _ := arrayAt(brush, "faces")
			for _, faceValue := range faces {
				summary.Faces++
				face, ok := faceValue.(*Object)
				if !ok {
					continue
				}
				// Truthiness, so `false`, `0`, `""` and absence all count as
				// not synthetic.
				if value, present := face.Get("synthetic"); present && truthy(value) {
					summary.SyntheticFaces++
				}
			}
		}
	}

	relationships, _ := arrayAt(document, "relationships")
	summary.Relationships = len(relationships)

	if extensions, ok := objectAt(document, "extensions"); ok {
		names := extensions.Keys()
		sort.Strings(names)
		summary.Extensions = names
	}

	// DELIBERATELY NOT GATED on the declared version.
	//
	// Inspecting is not reading-for-use. Looking at a deprecated document to find out what it is —
	// its version, its counts, its entities — is precisely what somebody does before deciding to
	// migrate it, and a gate here would make the one tool that could answer "what have I got?"
	// refuse to say. The schema findings below are reported against whatever schema the caller
	// supplied, and Summary carries the declared version so the reader can see the mismatch.
	if schema != nil {
		schemaProblems, err := ValidateAgainstSchema(document, schema)
		if err != nil {
			return Summary{}, err
		}
		semanticProblems := SemanticProblems(document)
		summary.Validation = &ValidationSummary{
			Valid:            len(schemaProblems) == 0 && len(semanticProblems) == 0,
			SchemaProblems:   len(schemaProblems),
			SemanticProblems: len(semanticProblems),
		}
	}

	return summary, nil
}

// TextLines renders one summary as `apmap-inspect`'s text output.
func (s Summary) TextLines() []string {
	lines := []string{s.Document}

	// `frame=<space>  to_source=<type><translation>`: the transform type with
	// the translation appended directly. Values are spelled the way Python's
	// str() spells them — None as "None", a list as its repr — so a frame with no
	// to_source prints "to_source=None".
	transformType := "None"
	translation := ""
	if transform, ok := s.Frame.ToSource.(map[string]any); ok {
		if value, present := transform["type"]; present {
			transformType = pythonScalar(value)
		}
		if value, present := transform["translation"]; present {
			translation = pythonRepr(value)
		}
	}

	lines = append(lines,
		fmt.Sprintf("  apmap %s  role=%s  id_policy=%s",
			pythonScalar(s.APMapVersion), pythonScalar(s.Role), pythonScalar(s.IDPolicy)),
		fmt.Sprintf("  document_id=%s  revision=%s",
			pythonScalar(s.DocumentID), pythonScalar(s.Revision)),
		fmt.Sprintf("  frame=%s  to_source=%s%s",
			pythonScalar(s.Frame.Space), transformType, translation),
		fmt.Sprintf("  entities=%d  brushes=%d  faces=%d  synthetic=%d",
			s.Entities, s.Brushes, s.Faces, s.SyntheticFaces),
	)
	if s.Relationships != 0 {
		lines = append(lines, fmt.Sprintf("  relationships=%d", s.Relationships))
	}
	if len(s.Extensions) != 0 {
		lines = append(lines, fmt.Sprintf("  extensions=%s", strings.Join(s.Extensions, ", ")))
	}
	// Last, so that everything above keeps the same lines with or without
	// validation.
	if s.Validation != nil {
		status := "invalid"
		if s.Validation.Valid {
			status = "valid"
		}
		lines = append(lines, fmt.Sprintf("  validation=%s  schema_problems=%d  semantic_problems=%d",
			status, s.Validation.SchemaProblems, s.Validation.SemanticProblems))
	}
	return lines
}

// plainAt reads one member and converts it to plain Go values. An absent
// member is nil, which marshals as null.
func plainAt(object *Object, name string) any {
	value, ok := object.Get(name)
	if !ok {
		return nil
	}
	return Plain(value)
}

func optionalString(object *Object, name string) *string {
	value, ok := object.Get(name)
	if !ok {
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return nil
	}
	return &text
}

func objectAt(object *Object, name string) (*Object, bool) {
	value, ok := object.Get(name)
	if !ok {
		return nil, false
	}
	nested, ok := value.(*Object)
	return nested, ok
}

func arrayAt(object *Object, name string) ([]Value, bool) {
	value, ok := object.Get(name)
	if !ok {
		return nil, false
	}
	array, ok := value.([]Value)
	return array, ok
}

// truthy is Python's bool() for the JSON values a document can hold.
func truthy(value Value) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case []Value:
		return len(typed) != 0
	case *Object:
		return typed.Len() != 0
	default:
		// A json.Number: zero is falsy, everything else is truthy.
		text := fmt.Sprint(typed)
		return text != "0" && text != "0.0" && text != "-0" && text != ""
	}
}

// pythonScalar is Python's f-string interpolation of a value that may be None:
// the value bare, or the literal "None". A string interpolates without quotes,
// which is why this is not pythonRepr.
func pythonScalar(value any) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		return typed
	default:
		return pythonRepr(value)
	}
}

// pythonRepr renders a JSON value the way Python's str() would inside an
// f-string. Only the translation vector reaches here in practice, printed as a
// Python list repr — `[0.0, 0.0, 0.0]` — appended directly to the transform type
// with no separator.
func pythonRepr(value Value) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case string:
		return "'" + typed + "'"
	case []Value:
		return pythonList(typed)
	case []any:
		// Distinct from []Value above: `type Value any` is a *defined* type,
		// not an alias, so []Value and []any are different types to a type
		// switch. Plain returns []any and the raw decoder returns []Value, and
		// both reach here.
		values := make([]Value, 0, len(typed))
		for _, item := range typed {
			values = append(values, item)
		}
		return pythonList(values)
	default:
		return fmt.Sprint(typed)
	}
}

// pythonList renders a Python list repr.
func pythonList(values []Value) string {
	parts := make([]string, 0, len(values))
	for _, item := range values {
		parts = append(parts, pythonRepr(item))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
