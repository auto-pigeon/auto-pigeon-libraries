package mapio

import (
	"context"
	"fmt"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
)

// ValidateOptions configures Validate.
type ValidateOptions struct {
	// Contract is the contract the document is measured against. Nil means the embedded one.
	Contract *apmap.Contract
	// Schema is a schema the caller names, replacing version-matched validation.
	Schema *apmap.Object
}

// Report is the verdict on one APMap document. It is a value, not an error: a document that fails
// validation is an answer to the question the caller asked.
type Report struct {
	Name string
	// DeclaredVersion is the document's own apmap_version, or "" when it declares none.
	DeclaredVersion string
	// Valid is true only when every layer passed.
	Valid bool
	// Unreadable is why the document could not be measured at all: not JSON, or a version the
	// contract does not hold. Empty when validation ran.
	Unreadable string
	// SchemaProblems and SemanticProblems are the two layers, reported together.
	SchemaProblems   []string
	SemanticProblems []string
	// IdentityProblems is the SEM-8 / SEM-10 subset of SemanticProblems.
	IdentityProblems []string
	// Warnings are non-fatal findings; they never make a document invalid.
	Warnings []Warning
}

// Validate measures one APMap document, on request. It is the only way this package validates a
// document outside a conversion that asked to, and it returns findings rather than an error: the
// error is only a cancelled context or a defect in the contract itself.
func Validate(ctx context.Context, name string, data []byte, opts ValidateOptions) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	report := Report{Name: name}
	if opts.Contract == nil {
		contract, err := apmap.EmbeddedContract()
		if err != nil {
			return report, err
		}
		opts.Contract = contract
	}
	document, err := apmap.DecodeJSON(data)
	if err != nil {
		report.Unreadable = fmt.Sprintf("%s is not valid JSON: %v", name, err)
		return report, nil
	}
	report.DeclaredVersion = apmap.DeclaredVersion(document)

	schema := opts.Schema
	if schema == nil {
		schema, err = opts.Contract.SchemaForDocument(document)
		if err != nil {
			report.Unreadable = err.Error()
			return report, nil
		}
	}
	report.SchemaProblems, err = apmap.ValidateAgainstSchemaContext(ctx, document, schema)
	if err != nil {
		return report, err
	}
	var warnings []string
	report.SemanticProblems, warnings, report.IdentityProblems, err = apmap.SemanticReport(ctx, opts.Contract, document)
	if err != nil {
		return report, err
	}
	for _, warning := range warnings {
		report.Warnings = append(report.Warnings, Warning{Code: WarningSemantic, Text: "semantic_warning: " + warning})
	}
	report.Valid = len(report.SchemaProblems)+len(report.SemanticProblems) == 0
	return report, nil
}
