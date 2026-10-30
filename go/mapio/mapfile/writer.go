package mapfile

import (
	"fmt"
	"io"
	"strings"
)

// WriteError is the writer's refusal of a key/value string it cannot represent
// in `.map` syntax.
//
// These are reachable from a *parsed* document, not just a hand-built one: the
// tokenizer preserves a backslash escape verbatim, so the source `"a\"b"` parses
// to the value `a\"b`, which contains a raw quote the writer then rejects.
type WriteError struct {
	Value  string
	Reason string
}

func (e *WriteError) Error() string {
	return fmt.Sprintf("MAP key/value strings cannot contain %s: %q", e.Reason, e.Value)
}

// Write serializes a document to `.map` text.
//
// Determinism: the output is a pure function of the model — entities, items, and
// faces are emitted in stored order with fixed spacing, and every number goes
// through FormatNumber. There is no map iteration, no timestamp, no float
// formatting that varies by platform, so the same *MapFile yields byte-identical
// text in any process.
//
// The prompt suggested `Write(m *MapFile) string`. It returns an error too,
// because Python signals the unrepresentable-value case by raising and Go cannot
// — and dropping it would mean silently emitting a corrupt `.map` file for input
// the writer cannot represent. WriteTo below is the io.Writer form.
func Write(m *MapFile) (string, error) {
	var b strings.Builder
	if err := writeTo(&b, m); err != nil {
		return "", err
	}
	return b.String(), nil
}

// WriteTo streams a document to w. This is the form the CLI uses: it writes to
// stdout or a file without materializing a whole map in memory first.
func WriteTo(w io.Writer, m *MapFile) error {
	return writeTo(w, m)
}

func writeTo(w io.Writer, m *MapFile) error {
	// Lines are joined with "\n" and a trailing newline is appended only when
	// the document is non-empty, so an empty document writes the empty string,
	// not a lone newline. This is what makes an empty map round-trip.
	if m == nil || len(m.entities) == 0 {
		return nil
	}

	for _, entity := range m.entities {
		if err := writeString(w, "{\n"); err != nil {
			return err
		}
		for _, item := range entity.items {
			if err := writeEntityItem(w, item); err != nil {
				return err
			}
		}
		if err := writeString(w, "}\n"); err != nil {
			return err
		}
	}
	return nil
}

func writeEntityItem(w io.Writer, item EntityItem) error {
	switch value := item.(type) {
	case KeyValue:
		key, err := quote(value.key)
		if err != nil {
			return err
		}
		text, err := quote(value.value)
		if err != nil {
			return err
		}
		return writeString(w, key+" "+text+"\n")
	case Brush:
		if len(value.faces) == 0 {
			return fmt.Errorf("cannot write brush with no faces")
		}
		if err := writeString(w, "{\n"); err != nil {
			return err
		}
		for _, face := range value.faces {
			if err := writeString(w, face.Line()+"\n"); err != nil {
				return err
			}
		}
		return writeString(w, "}\n")
	default:
		return fmt.Errorf("unsupported entity item: %T", item)
	}
}

func writeString(w io.Writer, s string) error {
	_, err := io.WriteString(w, s)
	return err
}

func quote(value string) (string, error) {
	if strings.ContainsAny(value, "\n\r") {
		return "", &WriteError{Value: value, Reason: "newlines"}
	}
	if strings.Contains(value, `"`) {
		return "", &WriteError{Value: value, Reason: "raw quotes"}
	}
	return `"` + value + `"`, nil
}
