// Package apmap is the Go APMap codec: the contract (schema) layer, the SEM-*
// semantic rules, canonical serialization, object identity, and conversion to
// and from `.map`. The APMap contract itself is ts/apmap-schema; this package
// reads it (see contract.go and embedded.go).
//
// The two validation layers are separate on purpose: a document that passes
// Validate may still be rejected by SEM-* rules a JSON Schema cannot express.
// The published test vectors make that concrete — several `invalid/` vectors are
// SEM-* cases that pass schema validation, and are expected to.
//
// # Package boundary
//
// Flat `apmap`, not `apmap/schema`. Schema, convert,
// semantics, serialize and identity use each other freely;
// splitting them into sub-packages would invent an internal boundary with no
// second module to justify where the line falls. If the package becomes
// unwieldy, splitting it is a mechanical change.
package apmap

import (
	"errors"
	"fmt"
	"strings"
)

// ErrAPMap is the root of the error hierarchy. Every error this package defines
// wraps it, so `errors.Is(err, apmap.ErrAPMap)` matches any APMap failure.
//
// Go has no exception subclassing, so the hierarchy is expressed with wrapping
// plus concrete types reachable through errors.As.
var ErrAPMap = errors.New("apmap")

// VersionError: the document declares a version this build does not
// implement, or none at all.
type VersionError struct {
	Message string
}

func (e *VersionError) Error() string { return e.Message }
func (e *VersionError) Unwrap() error { return ErrAPMap }

// SchemaError: the document failed JSON Schema
// validation. Problems holds one message per violation.
type SchemaError struct {
	Problems []string
	Source   string
}

// NewSchemaError builds the error from a problem list. Source names the
// document for the message; empty means DefaultSource.
func NewSchemaError(problems []string, source string) *SchemaError {
	if source == "" {
		source = DefaultSource
	}
	return &SchemaError{Problems: append([]string(nil), problems...), Source: source}
}

func (e *SchemaError) Error() string {
	return violationMessage(e.Source, e.Problems, "schema")
}

func (e *SchemaError) Unwrap() error { return ErrAPMap }

// SemanticError: the document is schema-valid but violates a SEM-* rule.
type SemanticError struct {
	Problems []string
	Source   string
}

// NewSemanticError builds the error from a problem list.
func NewSemanticError(problems []string, source string) *SemanticError {
	if source == "" {
		source = DefaultSource
	}
	return &SemanticError{Problems: append([]string(nil), problems...), Source: source}
}

func (e *SemanticError) Error() string {
	return violationMessage(e.Source, e.Problems, "semantic")
}

func (e *SemanticError) Unwrap() error { return ErrAPMap }

// SchemaNotFoundError covers rather more than its name suggests: the schema
// could not be
// located, could not be read, is not JSON, is not an object, contains a $ref
// that does not resolve, or uses a keyword or type name the validator does not
// implement. All of those are defects in the *schema* or its deployment, never
// in the document being validated, which is why they share one type and are
// reported separately from the document's problem list.
type SchemaNotFoundError struct {
	Message string
	Cause   error
}

func (e *SchemaNotFoundError) Error() string { return e.Message }

func (e *SchemaNotFoundError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return ErrAPMap
}

// Is lets errors.Is(err, ErrAPMap) succeed even when a Cause is attached and
// occupies the single Unwrap slot.
func (e *SchemaNotFoundError) Is(target error) bool { return target == ErrAPMap }

func notFound(format string, args ...any) *SchemaNotFoundError {
	return &SchemaNotFoundError{Message: fmt.Sprintf(format, args...)}
}

// DefaultSource names a document whose caller gave it no source.
const DefaultSource = "<apmap>"

// violationMessage is the shared error text for both layers, including its cap
// of ten shown problems with a count of the rest.
func violationMessage(source string, problems []string, kind string) string {
	shown := problems
	more := ""
	if len(problems) > 10 {
		shown = problems[:10]
		more = fmt.Sprintf("\n  ... and %d more", len(problems)-10)
	}
	return fmt.Sprintf("%s: %d %s violation(s):\n  %s%s",
		source, len(problems), kind, strings.Join(shown, "\n  "), more)
}
