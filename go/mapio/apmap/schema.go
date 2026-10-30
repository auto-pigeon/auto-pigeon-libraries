package apmap

import (
	"fmt"
)

// A schema is resolved from a contract (contract.go), never from a path a document names. The caller
// that holds a schema file elsewhere reads the bytes and calls ParseSchema; this package opens no file.
//
// The contract is AULIBS' immutable, versioned artifact: an edited private copy inside any one
// consumer would silently become a second, divergent contract — a correctness bug rather than
// untidiness.

// CurrentSchema is the schema of the process's default contract.
func CurrentSchema() (*Object, error) {
	contract, err := CurrentContract()
	if err != nil {
		return nil, err
	}
	return contract.Schema, nil
}

// ParseSchema parses schema bytes and checks they form a JSON object. source
// names their origin for the error messages — a path, an upload's filename, or
// anything else meaningful to whoever will read the failure.
//
// It caches nothing: a schema is a value, and the caller owns its lifetime.
func ParseSchema(source string, data []byte) (*Object, error) {
	value, err := DecodeJSON(data)
	if err != nil {
		return nil, &SchemaNotFoundError{
			Message: fmt.Sprintf("the APMap schema at %s is not valid JSON: %v", source, err),
			Cause:   err,
		}
	}
	schema, ok := value.(*Object)
	if !ok {
		return nil, notFound("the APMap schema at %s is not a JSON object", source)
	}
	return schema, nil
}

// ValidateOptions carries Validate's optional arguments.
type ValidateOptions struct {
	// Version is retained for callers that still pass one, and is CHECKED rather than used to
	// choose a schema. There is one contract; a version that is not the current one is refused.
	Version string
	// Schema, when the caller names its own contract. Version is then ignored.
	Schema *Object
	// Source names the document in the error message. Empty means DefaultSource.
	Source string
}

// Validate validates document, returning a *SchemaError when it has any
// violation and nil when it has none.
//
// The pure/returning pair is deliberate, because the two serve different
// callers: ValidateAgainstSchema returns the problem list for a caller
// aggregating errors of its own (semantics and publish both do), and Validate is
// the one-line "reject this document" form.
// Neither is a wrapper worth inlining at the call site.
//
// The returned error is a *SchemaNotFoundError instead when the schema itself
// cannot be located, read, or interpreted — a deployment fault, not a document
// fault, and never mixed into the document's problem list.
func Validate(document Value, opts ValidateOptions) error {
	// THE VERSION GATE, before any validation. A document declaring a version the loaded bundle does
	// not hold is refused here rather than validated against a schema chosen to suit it. What the
	// gate asks is membership of the bundle; what it still refuses is a producer choosing its own
	// contract, which is what a version this deployment never loaded would amount to.
	//
	// Then the schema MATCHING that declaration — 1.0 against 1.0's own frozen contract, current
	// against the current one. Substituting the current schema for a legacy document would be a
	// weaker check than the one that document claimed to satisfy.
	//
	// Both skipped when the caller supplied its own schema, which is the explicit-override case —
	// there the caller has said which contract it means, and this is not the deployment's.
	schema := opts.Schema
	if schema == nil {
		declared := opts.Version
		if declared == "" {
			declared = declaredVersion(document)
		}
		if declared == "" {
			return &VersionError{Message: "the document declares no apmap_version, so it names no contract to be read against"}
		}
		matched, err := SchemaForVersion(declared)
		if err != nil {
			return err
		}
		schema = matched
	}

	problems, err := ValidateAgainstSchema(document, schema)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return NewSchemaError(problems, opts.Source)
	}
	return nil
}

// declaredVersion reads apmap_version from the document.
//
// Returns "" when the document declares none, and the gate then refuses it: a document that names
// no contract cannot be said to satisfy one. It used to fall back to the version this build
// implements, which quietly validated a version-less document as if it had declared the right
// thing.
func declaredVersion(document Value) string {
	object, ok := document.(*Object)
	if !ok {
		return ""
	}
	value, ok := object.Get("apmap_version")
	if !ok {
		return ""
	}
	declared, ok := value.(string)
	if !ok {
		return ""
	}
	return declared
}
