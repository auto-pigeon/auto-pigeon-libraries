package apmap

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// SEMANTIC COMPARISON, INDEPENDENT OF THE BYTES.
//
// # Why a second comparison, when a byte-for-byte one already exists
//
// Every test in this repository that compares two APMap documents compares their canonical TEXT
// (the frozen-vector tests pin its digest). That is the strongest possible claim and it is the right
// default: the codec canonicalises, so agreement byte for byte proves agreement about everything.
//
// It is also the wrong tool for one specific question, and that question is the one the backlog
// entry behind `AUE 141` asks: a semantic comparison beside the byte one. A byte comparison answers "are these the same document"; it cannot answer "do these
// two documents describe the same geometry", and the difference matters as soon as the two sides
// reached the document by different routes:
//
//   - a `.map` and the `.rmf` it was exported from hold the same brush as decimal text and as
//     float32, so their literals differ in the last place while the geometry does not;
//   - a document promoted from a deprecated APMap version differs from the original by the
//     promotion and by nothing else, and "by nothing else" is the claim worth testing;
//   - a codec change that reorders object members changes every byte and no meaning.
//
// A byte comparison reports all three as failures with a first-differing-line, and a reader then has
// to decide by eye whether they saw a representation difference or a geometry one. That decision is
// what this file makes mechanical.
//
// # What "canonicalize before comparing" means here
//
// Both sides go through Normalize first — SER-7's rounding and integer collapse, the same function
// Encode runs — so `1e-5`, `0.00001` and `0.000010` are one value before anything is compared, and
// `-0` is `0`. A difference this comparator reports is therefore never a spelling.
//
// # What is meaning and what is not
//
//	object members     compared as a SET. Order is SER-5's concern, not the geometry's.
//	array elements     compared IN ORDER. Entity order, face order and point order are meaning.
//	numbers            compared by value, with an optional tolerance for cross-codec work.
//	everything else    compared exactly.
//
// # This is not a replacement for the byte comparison
//
// Nothing here relaxes an existing gate. The frozen-vector tests still demand byte equality; this
// runs beside them and says which KIND of difference was found when one is. A test that swapped one for the
// other would be weakening a check, not making it smarter.

// Difference is one semantic disagreement between two documents.
type Difference struct {
	// Path is JSONPath-ish — `$`, `.member`, `[index]` — the same notation SemanticProblems and
	// pyjson's non-finite paths use. Three notations for one idea is two too many.
	Path string
	// Left and Right are the disagreeing values, rendered for a human.
	Left  string
	Right string
	// Reason says what kind of disagreement it is, so a caller can act on the class rather than
	// parsing the message.
	Reason string
}

func (d Difference) String() string {
	return fmt.Sprintf("%s: %s (left %s, right %s)", d.Path, d.Reason, d.Left, d.Right)
}

// Reasons a comparison reports.
const (
	// DifferentType is a value that is an object on one side and something else on the other.
	DifferentType = "different type"
	// DifferentValue is two scalars that disagree.
	DifferentValue = "different value"
	// MissingMember is a member one side has and the other does not.
	MissingMember = "member absent on one side"
	// DifferentLength is two arrays of different lengths.
	DifferentLength = "different array length"
)

// CompareOptions tunes a semantic comparison.
type CompareOptions struct {
	// NumberTolerance is the largest absolute difference two numbers may have and still be equal.
	//
	// Zero means exact after canonicalisation, which is the right setting for two producers reading
	// ONE representation — a round trip, a version promotion. A non-zero
	// tolerance is for comparing across codecs, where one side stored float32 and the other decimal
	// text, and demanding the last bit would fail the more precise pipeline. Choose it from the
	// representation, not from how much slack makes a test pass.
	NumberTolerance float64

	// IgnoreMembers are member names skipped wherever they appear, by name at any depth.
	//
	// For the fields that are provenance rather than geometry — `document_id`, `generated_at`, a
	// source digest — when the comparison is about what the two documents DESCRIBE. Empty by
	// default, deliberately: a comparison that ignores something has to say so at the call site.
	IgnoreMembers []string

	// MaxDifferences bounds the report. Zero means the default of 50: a document whose every face
	// disagrees produces tens of thousands of differences, and nobody reads past the first few.
	MaxDifferences int
}

const defaultMaxDifferences = 50

// SemanticDiff canonicalises both documents and returns where they disagree in meaning.
//
// An empty result means the two describe the same thing. A normalisation failure — a non-finite
// number, which SER-7 refuses — is itself reported as a difference rather than swallowed, because a
// document that cannot be canonicalised is not one this comparison can make a claim about.
func SemanticDiff(left, right Value, opts CompareOptions) []Difference {
	limit := opts.MaxDifferences
	if limit <= 0 {
		limit = defaultMaxDifferences
	}
	ignored := make(map[string]struct{}, len(opts.IgnoreMembers))
	for _, name := range opts.IgnoreMembers {
		ignored[name] = struct{}{}
	}

	normalizedLeft, leftErr := Normalize(left)
	normalizedRight, rightErr := Normalize(right)
	if leftErr != nil || rightErr != nil {
		return []Difference{{
			Path: "$", Reason: "a document could not be canonicalised",
			Left: errorText(leftErr), Right: errorText(rightErr),
		}}
	}

	state := &diffState{options: opts, ignored: ignored, limit: limit}
	state.walk(normalizedLeft, normalizedRight, "$")

	return state.differences
}

// SemanticallyEqual is SemanticDiff with the answer reduced to a bool.
func SemanticallyEqual(left, right Value, opts CompareOptions) bool {
	if opts.MaxDifferences == 0 {
		opts.MaxDifferences = 1
	}

	return len(SemanticDiff(left, right, opts)) == 0
}

// DiffText is SemanticDiff over two canonical documents, decoding both first.
//
// The convenience a text-holding caller wants: it holds text on both sides, and decoding it at every
// call site is where a subtle difference in decode options would creep in.
func DiffText(left, right string, opts CompareOptions) ([]Difference, error) {
	leftDocument, err := DecodeJSON([]byte(left))
	if err != nil {
		return nil, fmt.Errorf("the left document does not decode: %w", err)
	}
	rightDocument, err := DecodeJSON([]byte(right))
	if err != nil {
		return nil, fmt.Errorf("the right document does not decode: %w", err)
	}

	return SemanticDiff(leftDocument, rightDocument, opts), nil
}

func errorText(err error) string {
	if err == nil {
		return "canonicalised"
	}

	return err.Error()
}

type diffState struct {
	options     CompareOptions
	ignored     map[string]struct{}
	limit       int
	differences []Difference
}

func (s *diffState) full() bool { return len(s.differences) >= s.limit }

func (s *diffState) report(path, reason, left, right string) {
	if s.full() {
		return
	}
	s.differences = append(s.differences,
		Difference{Path: path, Reason: reason, Left: left, Right: right})
}

func (s *diffState) walk(left, right Value, path string) {
	if s.full() {
		return
	}

	switch typedLeft := left.(type) {
	case *Object:
		typedRight, ok := right.(*Object)
		if !ok {
			s.report(path, DifferentType, "object", renderForDiff(right))

			return
		}
		s.walkObjects(typedLeft, typedRight, path)

	case []Value:
		typedRight, ok := right.([]Value)
		if !ok {
			s.report(path, DifferentType, "array", renderForDiff(right))

			return
		}
		if len(typedLeft) != len(typedRight) {
			s.report(path, DifferentLength,
				strconv.Itoa(len(typedLeft)), strconv.Itoa(len(typedRight)))

			return
		}
		for index := range typedLeft {
			s.walk(typedLeft[index], typedRight[index], path+"["+strconv.Itoa(index)+"]")
			if s.full() {
				return
			}
		}

	case json.Number:
		typedRight, ok := right.(json.Number)
		if !ok {
			s.report(path, DifferentType, "number", renderForDiff(right))

			return
		}
		if !s.numbersEqual(typedLeft, typedRight) {
			s.report(path, DifferentValue, string(typedLeft), string(typedRight))
		}

	default:
		if !scalarEqual(left, right) {
			s.report(path, DifferentValue, renderForDiff(left), renderForDiff(right))
		}
	}
}

// walkObjects compares member sets. Order is not meaning; presence and content are.
func (s *diffState) walkObjects(left, right *Object, path string) {
	seen := map[string]struct{}{}
	for _, key := range left.Keys() {
		if _, skip := s.ignored[key]; skip {
			seen[key] = struct{}{}

			continue
		}
		seen[key] = struct{}{}
		leftValue, _ := left.Get(key)
		rightValue, present := right.Get(key)
		if !present {
			s.report(path+"."+key, MissingMember, renderForDiff(leftValue), "<absent>")

			continue
		}
		s.walk(leftValue, rightValue, path+"."+key)
		if s.full() {
			return
		}
	}

	// Members only the right side has. Collected and sorted so two runs report them in one order.
	var extra []string
	for _, key := range right.Keys() {
		if _, skip := s.ignored[key]; skip {
			continue
		}
		if _, already := seen[key]; !already {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	for _, key := range extra {
		value, _ := right.Get(key)
		s.report(path+"."+key, MissingMember, "<absent>", renderForDiff(value))
		if s.full() {
			return
		}
	}
}

func (s *diffState) numbersEqual(left, right json.Number) bool {
	if left == right {
		return true
	}
	// Both sides are canonical by now, so unequal literals normally mean unequal values. They are
	// still parsed rather than trusted: an integer literal beyond float64's exact range keeps its
	// digits through Normalize, and comparing those as text is the correct answer.
	leftValue, leftErr := left.Float64()
	rightValue, rightErr := right.Float64()
	if leftErr != nil || rightErr != nil {
		return false
	}
	if s.options.NumberTolerance <= 0 {
		return leftValue == rightValue
	}

	return math.Abs(leftValue-rightValue) <= s.options.NumberTolerance
}

func scalarEqual(left, right Value) bool {
	switch typedLeft := left.(type) {
	case nil:
		return right == nil
	case bool:
		typedRight, ok := right.(bool)

		return ok && typedLeft == typedRight
	case string:
		typedRight, ok := right.(string)

		return ok && typedLeft == typedRight
	}

	return false
}

func renderForDiff(value Value) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(typed)
	case string:
		return strconv.Quote(typed)
	case json.Number:
		return string(typed)
	case []Value:
		return fmt.Sprintf("array(%d)", len(typed))
	case *Object:
		return fmt.Sprintf("object(%d)", len(typed.Keys()))
	}

	return fmt.Sprintf("%T", value)
}
