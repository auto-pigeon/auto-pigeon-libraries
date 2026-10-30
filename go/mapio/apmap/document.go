package apmap

// The package-level document API: ValidateDocument.
//
// The rest of the codec exposes its two validation layers separately, because
// callers that aggregate their own errors want the problem lists. This is the
// whole-document form most other callers want.

// ValidateDocument runs full validation: schema first, then the semantic rules.
//
// Schema errors are raised before semantic ones because a semantically odd
// message about a member that should not exist at all is noise.
func ValidateDocument(document Value, opts ValidateOptions) error {
	if err := Validate(document, opts); err != nil {
		return err
	}
	source := opts.Source
	if source == "" {
		source = DefaultSource
	}
	return ValidateSemantics(document, source)
}
