package apmap

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Canonical APMap encoding — rules SER-1 … SER-9, and APMap 1.5's SER-7Q.
//
// Two producers given the same document must emit the same bytes, and decoding
// a canonical document then re-encoding it must reproduce those bytes exactly.
// This file exists to make that a property rather than a convention.
//
// # Naming: Encode/Decode, not Marshal/Unmarshal
//
// Go convention would suggest Marshal/Unmarshal, and that is exactly why they
// are not used. Those names carry an expectation from encoding/json: struct
// tags, arbitrary Go values, `json.Marshal(v any)`. Nothing here does that —
// Encode takes a decoded APMap Value and produces the one canonical text for
// it, and Decode is its inverse. Borrowing the stdlib's names for a function
// with a narrower contract would mislead more than the convention helps.
//
// # SER-7 and why the number formatter is duplicated
//
// The number rule is the one with teeth: a value that is mathematically an
// integer is emitted as a JSON integer, so 192.0 and 192 produce the same
// text. That is not cosmetic — it matches mapfile.FormatNumber, which already
// writes 192 for both, so nothing shifts when a document crosses between the
// two formats.
//
// This file therefore needs mapfile.FormatNumber's exact behaviour, and it
// **mirrors** it rather than importing it. The prompt allows either; mirroring
// is forced by this package's own constraint that identity and serialization
// stay dependency-free within apmap, and importing mapfile
// would also invert the dependency the eventual convert.go wants. The mirror is
// not left to trust: TestNumberFormattingAgreesWithMapfile compares the two
// implementations directly over the whole range SER-7 can produce, and it is
// the test that fails if either drifts.
//
// Writing this file is what surfaced a real bug in mapfile.FormatNumber — it
// used Go's 'g' verb, whose shortest-form exponent threshold is 1e6, where
// Python's repr switches at 1e16. Any non-integral coordinate at or above 1e6
// was written as `1.2345675e+06`, which AUE's `.map` parser does not
// accept. Fixed there; see that function's comment and the prompt 16 handoff.

// DecimalPlaces is SER-7's rounding tolerance: six decimal places.
const DecimalPlaces = 6

// Indent is SER-3's two spaces.
const Indent = "  "

// NormalizeNumber applies SER-7 to one number.
//
// The returned json.Number is the canonical spelling: an integer when the value
// is mathematically an integer, the shortest round-tripping decimal otherwise.
//
// -0.0 collapses to 0 because a signed zero would otherwise make two
// geometrically identical documents differ in their bytes. A non-finite value
// is rejected under SEM-11 rather than emitted — JSON has no spelling for it,
// and Python's json.dumps would write bare `Infinity`, which is not JSON.
func NormalizeNumber(number json.Number) (json.Number, error) {
	text := string(number)

	// An integer literal is already canonical and must not go through float64:
	// a value beyond 2^53 would lose digits it arrived with, and the document
	// said what it meant.
	if isIntegerLiteral(number) {
		if text == "-0" {
			return json.Number("0"), nil
		}
		return number, nil
	}

	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		// Out of float64 range — 1e400 and friends, which decode fine (see
		// json.go) precisely so this layer can reject them by rule.
		return "", newError("SEM-11 non-finite number: %s", text)
	}
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return "", newError("SEM-11 non-finite number: %s", text)
	}

	rounded := roundHalfEven(value, DecimalPlaces)
	if rounded == math.Trunc(rounded) {
		// Python's `int(rounded)`: an integral value is emitted as an integer,
		// and through an integer so -0.0 prints as "0" rather than "-0".
		if math.Abs(rounded) < 1e18 {
			return json.Number(strconv.FormatInt(int64(rounded), 10)), nil
		}
		return json.Number(exactInteger(rounded)), nil
	}
	return json.Number(formatFloat(rounded)), nil
}

// roundHalfEven is Python's round(value, places): round-half-to-even, applied
// to the decimal representation rather than the binary one.
//
// Go's math.Round is round-half-away-from-zero, which disagrees with Python on
// exact .5 cases — round(2.5) is 2 in Python and 3 in Go. At six decimal places
// an exactly-representable tie is rare but not impossible, and "rare" is not a
// property a canonical form can rest on: two implementations disagreeing on one
// value in a million still produce two different canonical documents.
//
// The scale-round-unscale is done via strconv rather than arithmetic because
// value*1e6 introduces its own representation error; formatting to six places
// and reparsing is what Python's round effectively does.
func roundHalfEven(value float64, places int) float64 {
	// strconv.FormatFloat rounds to nearest, ties to even — the same rule
	// Python's round uses.
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(value, 'f', places, 64), 64)
	if err != nil {
		return value
	}
	return rounded
}

// exactInteger is Python's str(int(value)) for an integral float64 too large
// for an int64. Mirrors mapfile.exactInteger; see this file's header.
//
// FormatFloat with 'f' and precision -1 would not do: it emits the shortest
// decimal that round-trips to the same float64, where Python prints the float's
// exact mathematical value. They agree up to 2^53 and diverge above —
// str(int(1e30)) is 1000000000000000019884624838656 against a shortest form of
// 1000000000000000000000000000000. Both name the same float64; the canonical
// form is the exact one.
func exactInteger(value float64) string {
	integer, _ := new(big.Float).SetFloat64(value).Int(nil)
	return integer.String()
}

// formatFloat renders a non-integral float64 exactly as CPython's repr does,
// which is what Python's json.dumps emits for a float.
//
// Mirrors mapfile.pythonRepr; see this file's header for why it is duplicated
// rather than imported, and TestNumberFormattingAgreesWithMapfile for the test
// that keeps them identical.
//
// Q2_002: CPython's repr switches to an exponent below 1e-4, and SER-7 forbids exponent notation in an
// APMap document — `3.4e-05` was written into canonical documents and into `.map` exports that this
// repository's own `.map` parser then refused (finding F18, 4 of 9 Quake II corpus maps). The digits
// are still the shortest that round-trip; only the notation is positional now, always. A non-integral
// float64 at or above 1e16 does not exist, so the other branch of repr's rule was unreachable.
func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// NormalizeExactNumber applies SER-7Q to one number: the shortest decimal that parses back to the
// same IEEE-754 double, in positional notation (never an exponent), with an integral value as an
// integer and -0 as 0. It is SER-7 without the six-decimal rounding, and it governs exactly the five
// numbers of a Quake III patch control point and the six coefficients of a brush_primitives
// texture matrix (APMap 1.5, SEMANTICS.md §12): six decimal places would change a texture
// coordinate of 0.00260417, and a 1/128 matrix coefficient rounded that way moves the texture by
// more than a quarter of a texel at x' = 4096. A non-finite value is refused exactly as SER-7 does.
func NormalizeExactNumber(number json.Number) (json.Number, error) {
	text := string(number)
	if isIntegerLiteral(number) {
		if text == "-0" {
			return json.Number("0"), nil
		}
		return number, nil
	}

	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return "", newError("SEM-11 non-finite number: %s", text)
	}
	if value == math.Trunc(value) {
		if math.Abs(value) < 1e18 {
			return json.Number(strconv.FormatInt(int64(value), 10)), nil
		}
		return json.Number(exactInteger(value)), nil
	}
	// 'f' with precision -1 is the shortest round-tripping digits, written positionally.
	return json.Number(strconv.FormatFloat(value, 'f', -1, 64)), nil
}

// numberContext is where a node sits in an APMap document, as far as the number rule cares.
//
// SER-7Q is PATH-aware, not shape-aware: it governs a patch's control_points only where a patch
// can be — an entity content item — and a matrix only inside a face's projection. An `extensions`
// value that happens to spell {"kind": "patch", "control_points": …} is not a patch and keeps
// SER-7, so the walk tracks the document's structure from its root rather than recognising shapes
// wherever they occur.
type numberContext int

const (
	contextDocument    numberContext = iota // the document root
	contextEntities                         // the `entities` array
	contextEntity                           // one entity
	contextContent                          // an entity's `content` array
	contextContentItem                      // one content item
	contextFaces                            // a brush's `faces` array
	contextFace                             // one face
	contextProjection                       // a face's `projection`
	contextOther                            // anywhere else: SER-7
	contextExact                            // inside control_points or a matrix: SER-7Q
)

// memberContext is the context of an object's member.
func memberContext(object *Object, context numberContext, key string) numberContext {
	switch context {
	case contextExact:
		return contextExact
	case contextDocument:
		if key == "entities" {
			return contextEntities
		}
	case contextEntity:
		if key == "content" {
			return contextContent
		}
	case contextContentItem:
		kind, _ := object.Get("kind")
		if kind == "patch" && key == "control_points" {
			return contextExact
		}
		if kind == "brush" && key == "faces" {
			return contextFaces
		}
	case contextFace:
		if key == "projection" {
			return contextProjection
		}
	case contextProjection:
		if mode, _ := object.Get("mode"); mode == "brush_primitives" && key == "matrix" {
			return contextExact
		}
	}
	return contextOther
}

// itemContext is the context of an array's element.
func itemContext(context numberContext) numberContext {
	switch context {
	case contextExact:
		return contextExact
	case contextEntities:
		return contextEntity
	case contextContent:
		return contextContentItem
	case contextFaces:
		return contextFace
	}
	return contextOther
}

// Normalize recursively applies SER-7 — and SER-7Q to a patch's control points and a
// brush_primitives matrix — preserving every key and every array order (SER-5/6).
//
// node is read as a document root: SER-7Q is located by the document's structure (see
// numberContext), so a fragment passed on its own is normalized by SER-7 throughout.
func Normalize(node Value) (Value, error) {
	return normalizeIn(node, contextDocument)
}

func normalizeIn(node Value, context numberContext) (Value, error) {
	switch typed := node.(type) {
	case *Object:
		out := NewObject()
		for _, key := range typed.Keys() {
			member, _ := typed.Get(key)
			normalized, err := normalizeIn(member, memberContext(typed, context, key))
			if err != nil {
				return nil, err
			}
			out.Set(key, normalized)
		}
		return out, nil
	case []Value:
		out := make([]Value, 0, len(typed))
		inner := itemContext(context)
		for _, item := range typed {
			normalized, err := normalizeIn(item, inner)
			if err != nil {
				return nil, err
			}
			out = append(out, normalized)
		}
		return out, nil
	case json.Number:
		if context == contextExact {
			return NormalizeExactNumber(typed)
		}
		return NormalizeNumber(typed)
	default:
		// Strings, booleans and null pass through untouched.
		return node, nil
	}
}

// Encode is the canonical UTF-8 text for document — SER-1 … SER-9.
//
// Member order is the caller's insertion order, which is how SER-5 is honoured:
// the builders in the eventual convert.go insert members in the order the
// specification declares them. Nothing here sorts.
//
// The output is written directly rather than through encoding/json.Marshal.
// Marshal cannot be used for three independent reasons, each sufficient on its
// own: it sorts map keys (SER-5/6 forbid it), it escapes <, > and & as <
// and friends by default (SER-9 requires unescaped UTF-8), and it would
// re-render every number through its own float formatter, undoing SER-7.
func Encode(document Value) (string, error) {
	normalized, err := Normalize(document)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	if err := writeValue(&builder, normalized, ""); err != nil {
		return "", err
	}
	builder.WriteByte('\n') // SER-4
	return builder.String(), nil
}

func writeValue(builder *strings.Builder, value Value, indent string) error {
	switch typed := value.(type) {
	case nil:
		builder.WriteString("null")
	case bool:
		builder.WriteString(strconv.FormatBool(typed))
	case string:
		writeString(builder, typed)
	case json.Number:
		builder.WriteString(string(typed))
	case []Value:
		if len(typed) == 0 {
			builder.WriteString("[]")
			return nil
		}
		inner := indent + Indent
		builder.WriteString("[\n")
		for index, item := range typed {
			builder.WriteString(inner)
			if err := writeValue(builder, item, inner); err != nil {
				return err
			}
			if index < len(typed)-1 {
				builder.WriteByte(',')
			}
			builder.WriteByte('\n')
		}
		builder.WriteString(indent)
		builder.WriteByte(']')
	case *Object:
		keys := typed.Keys()
		if len(keys) == 0 {
			builder.WriteString("{}")
			return nil
		}
		inner := indent + Indent
		builder.WriteString("{\n")
		for index, key := range keys {
			builder.WriteString(inner)
			writeString(builder, key)
			builder.WriteString(": ")
			member, _ := typed.Get(key)
			if err := writeValue(builder, member, inner); err != nil {
				return err
			}
			if index < len(keys)-1 {
				builder.WriteByte(',')
			}
			builder.WriteByte('\n')
		}
		builder.WriteString(indent)
		builder.WriteByte('}')
	default:
		return newError("cannot encode a %T", value)
	}
	return nil
}

// writeString emits a JSON string the way Python's json.dumps with
// ensure_ascii=False does (SER-9): control characters escaped, everything else
// — including every non-ASCII rune — written through as UTF-8.
//
// Python escapes exactly the two mandatory characters plus the C0 range, using
// the short forms where they exist. Notably it does *not* escape the forward
// slash, and does not escape U+007F.
func writeString(builder *strings.Builder, value string) {
	builder.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		case '\b':
			builder.WriteString(`\b`)
		case '\f':
			builder.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(builder, `\u%04x`, r)
				continue
			}
			builder.WriteRune(r)
		}
	}
	builder.WriteByte('"')
}

// Decode parses APMap text, rejecting anything that is not a JSON object.
//
// This is a thin wrapper over json.go's decoder rather than a new
// implementation, which is what the prompt expected and what reading that file
// confirms: its ordered, literal-preserving Object is exactly what a canonical
// serializer needs, and it already exists for those reasons.
func Decode(text string, source string) (*Object, error) {
	if source == "" {
		source = DefaultSource
	}
	value, err := DecodeJSON([]byte(text))
	if err != nil {
		return nil, newError("%s: not valid JSON: %v", source, err)
	}
	document, ok := value.(*Object)
	if !ok {
		return nil, newError("%s: an APMap document must be a JSON object", source)
	}
	return document, nil
}

// IsCanonical reports whether text is already the canonical encoding of what it
// decodes to — the fixed-point property the specification requires.
//
// This is the assertion that would actually catch a runtime-level disagreement
// about how a float renders, which is why the test suite runs it over every
// published vector rather than only over hand-written values.
func IsCanonical(text string) bool {
	document, err := Decode(text, "")
	if err != nil {
		return false
	}
	encoded, err := Encode(document)
	if err != nil {
		return false
	}
	return encoded == text
}
