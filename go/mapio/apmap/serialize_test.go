package apmap

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
)

func normalized(t *testing.T, literal string) string {
	t.Helper()
	got, err := NormalizeNumber(json.Number(literal))
	if err != nil {
		t.Fatalf("NormalizeNumber(%s): %v", literal, err)
	}
	return string(got)
}

// TestNormalizeNumber is SER-7 in a table.
func TestNormalizeNumber(t *testing.T) {
	cases := map[string]string{
		// An integer literal is already canonical.
		"0":    "0",
		"1":    "1",
		"-64":  "-64",
		"-0":   "0", // a signed zero must not survive
		"1000": "1000",
		// Integral floats become integers — the rule with teeth.
		"192.0":  "192",
		"-0.0":   "0",
		"1e3":    "1000",
		"2.5e1":  "25",
		"1.0e-0": "1",
		// Non-integral values keep six places.
		"64.75":       "64.75",
		"-1.25":       "-1.25",
		"0.1":         "0.1",
		"1.2345678":   "1.234568", // rounded to six
		"1.23456749":  "1.234567",
		"0.0000004":   "0", // below the tolerance, collapses
		"-0.0000004":  "0", // and does so without a sign
		"1234567.5":   "1234567.5",
		"123456789.5": "123456789.5",
	}

	for literal, want := range cases {
		if got := normalized(t, literal); got != want {
			t.Errorf("NormalizeNumber(%s) = %s, want %s", literal, got, want)
		}
	}
}

// TestNormalizeNumberRejectsNonFinite is SEM-11 at the serialization boundary.
//
// json.go decodes 1e400 successfully on purpose — the schema layer must accept
// the document so that the *semantic* layer can reject it. This is where the
// rejection actually happens, and it must happen rather than emitting
// `Infinity`, which is not JSON at all.
func TestNormalizeNumberRejectsNonFinite(t *testing.T) {
	for _, literal := range []string{"1e400", "-1e400", "1e999"} {
		if _, err := NormalizeNumber(json.Number(literal)); err == nil {
			t.Errorf("NormalizeNumber(%s) must be rejected under SEM-11", literal)
		} else if !strings.Contains(err.Error(), "SEM-11") {
			t.Errorf("NormalizeNumber(%s) error should cite SEM-11, got %v", literal, err)
		}
	}

	// And through the encoder, which is how a real document would hit it.
	document, err := DecodeJSON([]byte(`{"x": 1e400}`))
	if err != nil {
		t.Fatalf("the document must decode; SEM-11 is a rule, not a parse error: %v", err)
	}
	if _, err := Encode(document); err == nil {
		t.Error("Encode must refuse a non-finite number rather than emitting one")
	}
}

// TestNormalizeNumberPreservesLargeIntegers guards a hazard specific to this
// port: routing an integer literal through float64 would silently lose digits
// beyond 2^53. The document said what it meant, so it is passed through.
func TestNormalizeNumberPreservesLargeIntegers(t *testing.T) {
	for _, literal := range []string{"9007199254740993", "123456789012345678901234567890"} {
		if got := normalized(t, literal); got != literal {
			t.Errorf("NormalizeNumber(%s) = %s; a large integer literal must survive intact", literal, got)
		}
	}
}

// TestNumberFormattingAgreesWithMapfile is the prompt's required direct
// comparison, and it is the test that keeps the two implementations in step.
//
// apmap may not import mapfile (this package's identity and
// serialization are deliberately dependency-free), so SER-7's formatter is a
// deliberate mirror of mapfile.FormatNumber rather than a call to it. A mirror
// that is never compared is just a second implementation waiting to drift, so
// this compares them over the whole range SER-7 can produce.
//
// Writing this comparison is what found the bug in mapfile.FormatNumber: it
// used Go's 'g' verb, whose shortest-form exponent threshold is 1e6 where
// Python's repr switches at 1e16, so every non-integral coordinate at or above
// 1e6 was written in scientific notation that neither parser accepts. Fixed
// there; this test is what would catch a reintroduction.
func TestNumberFormattingAgreesWithMapfile(t *testing.T) {
	var values []float64

	// Systematic: every decimal magnitude SER-7 can produce, integral and not.
	for exponent := -8; exponent <= 18; exponent++ {
		for _, mantissa := range []float64{1, 1.5, 2.5, 3.25, 7.125, 9.999999, 1.000001} {
			values = append(values, mantissa*math.Pow10(exponent), -mantissa*math.Pow10(exponent))
		}
	}
	// Randomised, seeded so a failure is reproducible.
	generator := rand.New(rand.NewSource(20260801))
	for index := 0; index < 20000; index++ {
		values = append(values,
			generator.Float64()*2-1,
			(generator.Float64()*2-1)*1e6,
			(generator.Float64()*2-1)*1e9,
			(generator.Float64()*2-1)*1e15,
		)
	}
	// The boundaries themselves, where a threshold bug lives.
	values = append(values,
		0, math.Copysign(0, -1), 1e-7, 1e-6, 1e-5, 1e-4, 0.0001, 0.00001,
		999999.5, 1e6, 1000000.5, 1e15, 1e16, 1e17, 1234567.5, 12345678.125,
	)

	compared, mismatches := 0, 0
	for _, value := range values {
		// SER-7 rounds first; the comparison is on what it produces.
		rounded := roundHalfEven(value, DecimalPlaces)
		if math.IsInf(rounded, 0) || math.IsNaN(rounded) {
			continue
		}

		got, err := NormalizeNumber(json.Number(strconv.FormatFloat(value, 'g', -1, 64)))
		if err != nil {
			t.Fatalf("NormalizeNumber(%v): %v", value, err)
		}
		want := mapfile.FormatNumber(mapfile.Number(rounded))

		compared++
		if string(got) != want {
			mismatches++
			if mismatches <= 10 {
				t.Errorf("%v: apmap = %s, mapfile.FormatNumber = %s", value, got, want)
			}
		}
	}

	if compared < 1000 {
		t.Fatalf("only %d values compared; the sample is too small to mean anything", compared)
	}
	t.Logf("compared %d values against mapfile.FormatNumber, %d mismatches", compared, mismatches)
}

// TestFloatRenderingMatchesPython is the risk the prompt names explicitly:
// Python's json.dumps and Go's float formatting need not agree once past
// simple cases, and SER-7's rounding is not the whole story. The notation
// threshold is where they actually diverge, so this compares against the real
// Python rather than reasoning about it.
func TestFloatRenderingMatchesPython(t *testing.T) {
	python := pythonBinary()
	if _, err := exec.LookPath(python); err != nil {
		t.Skip(python + " is not on PATH: " + err.Error())
	}

	literals := []string{
		"0.1", "64.75", "-1.25", "1e-05", "0.0001", "1.5e-05",
		"1234567.5", "12345678.125", "999999.5", "1000000.5",
		"999999900000.0001", "123456789012345.6", "1.7976931348623157e+308",
		"5e-324", "0.30000000000000004", "2.220446049250313e-16",
	}

	script := `import json, sys
out = {}
for text in sys.argv[1:]:
    value = float(text)
    rounded = round(value, 6)
    if rounded == int(rounded):
        out[text] = str(int(rounded))
    else:
        # Q2_002 (F18): SER-7 forbids an exponent, so the expected spelling is repr's digits in
        # positional notation — what json.dumps writes, with its exponent (below 1e-4) expanded.
        from decimal import Decimal
        out[text] = format(Decimal(json.dumps(rounded)), 'f')
sys.stdout.write(json.dumps(out))`

	command := exec.Command(python, append([]string{"-c", script}, literals...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Skipf("python invocation failed: %v\n%s", err, output)
	}
	var expected map[string]string
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatalf("python returned output that is not JSON: %v\n%s", err, output)
	}

	for _, literal := range literals {
		got, err := NormalizeNumber(json.Number(literal))
		if err != nil {
			t.Errorf("NormalizeNumber(%s): %v", literal, err)
			continue
		}
		if string(got) != expected[literal] {
			t.Errorf("%s: AUE = %s, Python = %s", literal, got, expected[literal])
		}
	}
	t.Logf("compared %d edge-case numbers against Python's own rendering", len(literals))
}

// TestRoundHalfEvenMatchesPython covers the tie-breaking rule. Go's math.Round
// is half-away-from-zero and Python's round is half-to-even; a canonical form
// cannot rest on the two ties being rare.
func TestRoundHalfEvenMatchesPython(t *testing.T) {
	python := pythonBinary()
	if _, err := exec.LookPath(python); err != nil {
		t.Skip(python + " is not on PATH")
	}

	literals := []string{
		"0.0000005", "0.0000015", "0.0000025", "0.0000035",
		"2.5", "3.5", "-2.5", "-0.0000005", "1.0000005", "1.0000015",
	}
	script := `import json, sys
sys.stdout.write(json.dumps({t: repr(round(float(t), 6)) for t in sys.argv[1:]}))`

	command := exec.Command(python, append([]string{"-c", script}, literals...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Skipf("python invocation failed: %v\n%s", err, output)
	}
	var expected map[string]string
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, output)
	}

	for _, literal := range literals {
		value, _ := strconv.ParseFloat(literal, 64)
		got := roundHalfEven(value, DecimalPlaces)
		want, _ := strconv.ParseFloat(expected[literal], 64)
		if got != want {
			t.Errorf("roundHalfEven(%s) = %v, Python's round gives %v", literal, got, want)
		}
	}
}

// --- encoding --------------------------------------------------------------

func TestEncodeShape(t *testing.T) {
	document, err := DecodeJSON([]byte(`{"b":1,"a":[1,2],"c":{"d":true,"e":null},"f":"x","g":[],"h":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	text, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	want := `{
  "b": 1,
  "a": [
    1,
    2
  ],
  "c": {
    "d": true,
    "e": null
  },
  "f": "x",
  "g": [],
  "h": {}
}
`
	if text != want {
		t.Errorf("Encode produced:\n%s\nwant:\n%s", text, want)
	}
}

// TestEncodeNeverSorts is SER-5/6: insertion order is the meaning. A port that
// reached for encoding/json.Marshal would fail this, since Marshal sorts map
// keys — which is one of the three reasons this file writes JSON itself.
func TestEncodeNeverSorts(t *testing.T) {
	document, err := DecodeJSON([]byte(`{"z":1,"m":2,"a":3}`))
	if err != nil {
		t.Fatal(err)
	}
	text, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(text, `"z"`) > strings.Index(text, `"a"`) {
		t.Errorf("member order was not preserved:\n%s", text)
	}
}

// TestEncodeDoesNotEscapeUTF8 is SER-9. encoding/json.Marshal escapes <, > and
// & by default, which is visible elsewhere in this repository — internal/api's
// JSON shows `\u003e` where the CLI shows `>`. A canonical form cannot have
// that, since the escaped and unescaped texts decode alike but are different
// bytes.
func TestEncodeDoesNotEscapeUTF8(t *testing.T) {
	document, err := DecodeJSON([]byte(`{"k":"a > b & c < d — pakkét ✓"}`))
	if err != nil {
		t.Fatal(err)
	}
	text, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{">", "&", "<", "—", "pakkét", "✓"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q was escaped away:\n%s", want, text)
		}
	}
	if strings.Contains(text, `\u`) {
		t.Errorf("no printable character should be \\u-escaped:\n%s", text)
	}

	// Compare against Python's ensure_ascii=False, which is the actual rule.
	python := pythonBinary()
	if _, err := exec.LookPath(python); err != nil {
		return
	}
	script := `import json,sys
sys.stdout.write(json.dumps(json.loads(sys.argv[1]), indent=2, ensure_ascii=False,
    separators=(",", ": "), allow_nan=False, sort_keys=False) + "\n")`
	output, err := exec.Command(python, "-c", script, `{"k":"a > b & c < d — pakkét ✓"}`).Output()
	if err == nil && string(output) != text {
		t.Errorf("Encode disagrees with Python's json.dumps\n AUE: %q\n  py: %q", text, output)
	}
}

// TestEncodeEscapesControlCharacters covers the other half of SER-9: what must
// be escaped still is.
func TestEncodeEscapesControlCharacters(t *testing.T) {
	object := NewObject()
	object.Set("k", "tab\there\nnewline\x01one")
	text, err := Encode(object)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`\t`, `\n`, `\u0001`} {
		if !strings.Contains(text, want) {
			t.Errorf("%s should have been escaped:\n%s", want, text)
		}
	}
}

// TestEncodeEndsWithExactlyOneNewline is SER-4.
func TestEncodeEndsWithExactlyOneNewline(t *testing.T) {
	object := NewObject()
	object.Set("a", json.Number("1"))
	text, err := Encode(object)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(text, "}\n") || strings.HasSuffix(text, "\n\n") {
		t.Errorf("output must end with exactly one newline, got %q", text)
	}
}

// --- decoding --------------------------------------------------------------

func TestDecodeRejectsNonObjects(t *testing.T) {
	for _, text := range []string{`[1,2]`, `"a string"`, `42`, `null`, `true`} {
		if _, err := Decode(text, "doc.apmap"); err == nil {
			t.Errorf("Decode(%s) must reject a non-object top level", text)
		} else if !strings.Contains(err.Error(), "must be a JSON object") {
			t.Errorf("Decode(%s): unexpected message %v", text, err)
		}
	}
	if _, err := Decode(`{ broken`, "doc.apmap"); err == nil {
		t.Error("malformed JSON must be rejected")
	} else if !strings.Contains(err.Error(), "doc.apmap") {
		t.Errorf("the error should name the source, got %v", err)
	}
}

func TestWriteAndReadText(t *testing.T) {
	object := NewObject()
	object.Set("apmap_version", "1.0")
	object.Set("revision", json.Number("2.0"))

	path := filepath.Join(t.TempDir(), "doc.apmap")
	text, err := WriteText(object, path)
	if err != nil {
		t.Fatal(err)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != text {
		t.Error("WriteText must return exactly what it wrote")
	}
	// SER-7 applied on the way out: 2.0 became 2.
	if !strings.Contains(text, `"revision": 2`) {
		t.Errorf("SER-7 was not applied on write:\n%s", text)
	}

	read, err := ReadText(path)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := read.Get("apmap_version"); value != "1.0" {
		t.Errorf("round trip lost a member: %v", value)
	}
}

// --- the fixed-point property ----------------------------------------------

// TestIsCanonicalOverEveryVector is the property the whole file exists for, run
// over the real corpus rather than over hand-written documents.
//
// It is also the test that would actually catch a runtime-level disagreement
// about how a float renders: the published vectors are canonical text written
// independently of this encoder, so if Go's rendering differed anywhere in that
// corpus, re-encoding would not reproduce the bytes.
func TestIsCanonicalOverEveryVector(t *testing.T) {
	root := mapperRoot(t)

	checked, canonical := 0, 0
	for _, kind := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join(root, vectorRoot, kind, "*.apmap"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			checked++

			document, err := Decode(text, file)
			if err != nil {
				t.Errorf("%s: %v", filepath.Base(file), err)
				continue
			}
			encoded, err := Encode(document)
			if err != nil {
				// non-finite-number.apmap legitimately fails here: SEM-11
				// forbids the value, and the encoder is where that is caught.
				if strings.Contains(err.Error(), "SEM-11") {
					continue
				}
				t.Errorf("%s: %v", filepath.Base(file), err)
				continue
			}
			if encoded != text {
				t.Errorf("%s is not a fixed point of decode+encode\nfirst difference at %s",
					filepath.Base(file), firstDifference(text, encoded))
				continue
			}
			canonical++
			if !IsCanonical(text) {
				t.Errorf("%s: IsCanonical disagrees with an equal re-encode", filepath.Base(file))
			}
		}
	}

	if checked == 0 {
		t.Fatal("no vectors were checked")
	}
	t.Logf("decoded and re-encoded %d published vectors; %d reproduced their bytes exactly", checked, canonical)
}

// TestIsCanonicalRejectsNonCanonicalText covers the negative side, so the test
// above cannot pass by IsCanonical always returning true.
func TestIsCanonicalRejectsNonCanonicalText(t *testing.T) {
	for _, text := range []string{
		`{"a":1}` + "\n",       // no indentation
		"{\n  \"a\": 1\n}",     // no trailing newline
		"{\n  \"a\": 1.0\n}\n", // SER-7 not applied
		"{\n    \"a\": 1\n}\n", // four-space indent
		"{\n  \"a\": 1\n}\n\n", // two trailing newlines
		`{ broken`,             // not JSON at all
	} {
		if IsCanonical(text) {
			t.Errorf("IsCanonical(%q) = true, want false", text)
		}
	}

	if !IsCanonical("{\n  \"a\": 1\n}\n") {
		t.Error("a genuinely canonical document must be recognised")
	}
}

// TestEncodeIsIdempotent is the fixed-point property stated directly: encoding
// an already-encoded document changes nothing.
func TestEncodeIsIdempotent(t *testing.T) {
	root := mapperRoot(t)
	files, _ := filepath.Glob(filepath.Join(root, vectorRoot, "valid", "*.apmap"))
	if len(files) == 0 {
		t.Skip("no valid vectors present")
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		document, err := Decode(string(data), file)
		if err != nil {
			t.Fatal(err)
		}
		once, err := Encode(document)
		if err != nil {
			t.Fatal(err)
		}
		reparsed, err := Decode(once, file)
		if err != nil {
			t.Fatal(err)
		}
		twice, err := Encode(reparsed)
		if err != nil {
			t.Fatal(err)
		}
		if once != twice {
			t.Errorf("%s: encoding is not idempotent", filepath.Base(file))
		}
	}
}

// firstDifference reports where two texts diverge, with context. A whole-file
// diff of a 400-line document is unreadable in a test failure.
func firstDifference(left, right string) string {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for index := 0; index < limit; index++ {
		if left[index] != right[index] {
			start := index - 40
			if start < 0 {
				start = 0
			}
			return "byte " + strconv.Itoa(index) +
				"\n  want: ..." + strconv.Quote(left[start:min(index+40, len(left))]) +
				"\n   got: ..." + strconv.Quote(right[start:min(index+40, len(right))])
		}
	}
	if len(left) != len(right) {
		return "length: want " + strconv.Itoa(len(left)) + ", got " + strconv.Itoa(len(right))
	}
	return "(identical)"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// pythonBinary is the interpreter the Python-stdlib differential tests run: $PYTHON when set,
// otherwise python3. Those tests compare against the language's own json/round behaviour, which
// is the rule SER-7 and SER-9 are written in; they need nothing but the interpreter.
func pythonBinary() string {
	if python := os.Getenv("PYTHON"); python != "" {
		return python
	}
	return "python3"
}
