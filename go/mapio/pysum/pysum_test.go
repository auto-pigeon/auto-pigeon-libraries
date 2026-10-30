package pysum

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// TestSumIsNotTheNaiveLoop is the whole reason this package exists: if Sum were
// `total += value` in a loop there would be nothing to port.
func TestSumIsNotTheNaiveLoop(t *testing.T) {
	values := repeat(0.1, 10)
	if got := Sum(values); got != 1.0 {
		t.Errorf("Sum(ten tenths) = %v, want exactly 1 — Python's sum() gets this right", got)
	}
	if naiveSum(values) == 1.0 {
		t.Error("the naive loop is exact here, so this input distinguishes nothing")
	}
}

// TestSumMatchesPythonsBuiltinSum runs CPython over the same bits. Python is the
// specification, so the comparison is against the interpreter rather than
// against numbers copied out of one — and the values cross the wire as
// hexadecimal literals, which round-trip exactly.
func TestSumMatchesPythonsBuiltinSum(t *testing.T) {
	python := pythonBinary()
	if _, err := exec.LookPath(python); err != nil {
		t.Skipf("%s is not on PATH: %v", python, err)
	}

	cases := map[string][]float64{
		"empty":                     {},
		"one value":                 {1.5},
		"negative zero":             {math.Copysign(0, -1)},
		"ten tenths":                repeat(0.1, 10),
		"a thousand thirds":         repeat(1.0/3.0, 1000),
		"cancelling magnitudes":     {1e16, 1.0, -1e16, 1.0},
		"ascending magnitudes":      ramp(1e-8, 1.0000001, 2000),
		"descending magnitudes":     reverse(ramp(1e-8, 1.0000001, 2000)),
		"mixed signs":               alternating(2000),
		"an overflow to infinity":   {math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64},
		"infinity and its negation": {math.Inf(1), math.Inf(-1)},
		// The shape of the e1m3 interior-area sum in the package comment: a few
		// thousand same-signed areas of similar magnitude.
		"a corpus of areas": ramp(7654.321, 1.0003, 1600),
	}

	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			want := pythonSum(t, python, values)
			got := Sum(values)
			if !identicalFloat(got, want) {
				t.Errorf("Sum = %s, Python's sum() = %s",
					format(got), format(want))
			}
			// And where the two summations differ, say so: it records that the
			// case is load-bearing rather than trivially satisfied.
			if naive := naiveSum(values); !identicalFloat(naive, want) {
				t.Logf("the naive loop would have written %s instead", format(naive))
			}
		})
	}
}

func naiveSum(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total
}

func identicalFloat(got, want float64) bool {
	if math.IsNaN(got) && math.IsNaN(want) {
		return true
	}
	return math.Float64bits(got) == math.Float64bits(want)
}

func format(value float64) string { return strconv.FormatFloat(value, 'g', 17, 64) }

// pythonSum sums the same bits with CPython's builtin sum().
//
// float() wraps the result because sum([]) returns the integer 0, which has no
// .hex() — the empty case is part of what is being pinned.
func pythonSum(t *testing.T, python string, values []float64) float64 {
	t.Helper()
	input, err := json.Marshal(hexLiterals(values))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "-c",
		"import json,sys\n"+
			"values=[float.fromhex(item) for item in json.load(sys.stdin)]\n"+
			"print(float(sum(values)).hex())\n")
	command.Stdin = strings.NewReader(string(input))
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("python failed: %v\n%s", err, stderr.String())
	}
	text := strings.TrimSpace(string(output))
	parsed, err := parseHexFloat(text)
	if err != nil {
		t.Fatalf("cannot read python's %q: %v", text, err)
	}
	return parsed
}

func hexLiterals(values []float64) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.FormatFloat(value, 'x', -1, 64))
	}
	return out
}

// parseHexFloat reads Python's hexadecimal float spelling. Python writes "inf",
// "-inf" and "nan" as words rather than as hex.
func parseHexFloat(text string) (float64, error) {
	switch text {
	case "inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	case "nan":
		return math.NaN(), nil
	}
	return strconv.ParseFloat(text, 64)
}

func repeat(value float64, count int) []float64 {
	out := make([]float64, count)
	for index := range out {
		out[index] = value
	}
	return out
}

func ramp(start, factor float64, count int) []float64 {
	out := make([]float64, count)
	value := start
	for index := range out {
		out[index] = value
		value *= factor
	}
	return out
}

func reverse(values []float64) []float64 {
	out := make([]float64, len(values))
	for index, value := range values {
		out[len(values)-1-index] = value
	}
	return out
}

func alternating(count int) []float64 {
	out := make([]float64, count)
	for index := range out {
		out[index] = float64(index+1) * 1.0000001
		if index%2 == 1 {
			out[index] = -out[index] * 0.999999
		}
	}
	return out
}

func pythonBinary() string {
	if binary := os.Getenv("PYTHON"); binary != "" {
		return binary
	}
	return "python3"
}
