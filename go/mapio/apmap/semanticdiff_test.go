package apmap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decodeOrFail(t *testing.T, text string) Value {
	t.Helper()
	document, err := DecodeJSON([]byte(text))
	if err != nil {
		t.Fatalf("decoding %s: %v", text, err)
	}
	return document
}

// The whole point: a spelling is not a geometry difference.
func TestASpellingIsNotADifference(t *testing.T) {
	for _, pair := range []struct{ name, left, right string }{
		{"exponent and fixed", `{"v": 1e-5}`, `{"v": 0.00001}`},
		{"trailing zero", `{"v": 2.50}`, `{"v": 2.5}`},
		{"negative zero", `{"v": -0.0}`, `{"v": 0}`},
		{"integral float", `{"v": 3.0}`, `{"v": 3}`},
		{"member order", `{"a": 1, "b": 2}`, `{"b": 2, "a": 1}`},
		{"nested member order",
			`{"e": [{"x": 1, "y": 2}]}`, `{"e": [{"y": 2, "x": 1}]}`},
	} {
		t.Run(pair.name, func(t *testing.T) {
			differences := SemanticDiff(decodeOrFail(t, pair.left), decodeOrFail(t, pair.right),
				CompareOptions{})
			if len(differences) != 0 {
				t.Errorf("reported %v", differences)
			}
		})
	}
}

// And the other half, which is what makes the first half worth anything.
func TestARealDifferenceIsReportedWithItsPath(t *testing.T) {
	for _, testCase := range []struct {
		name, left, right, path, reason string
	}{
		{"a coordinate moved", `{"p": [0, 0, 0]}`, `{"p": [0, 1, 0]}`, "$.p[1]", DifferentValue},
		{"a face vanished", `{"f": [1, 2]}`, `{"f": [1]}`, "$.f", DifferentLength},
		{"a member vanished", `{"a": 1, "b": 2}`, `{"a": 1}`, "$.b", MissingMember},
		{"a member appeared", `{"a": 1}`, `{"a": 1, "b": 2}`, "$.b", MissingMember},
		{"a texture changed", `{"t": "wall"}`, `{"t": "floor"}`, "$.t", DifferentValue},
		{"a number became a string", `{"v": 1}`, `{"v": "1"}`, "$.v", DifferentType},
		{"an object became an array", `{"v": {}}`, `{"v": []}`, "$.v", DifferentType},
		{"the last decimal place", `{"v": 1.000001}`, `{"v": 1.000002}`, "$.v", DifferentValue},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			differences := SemanticDiff(decodeOrFail(t, testCase.left),
				decodeOrFail(t, testCase.right), CompareOptions{})
			if len(differences) != 1 {
				t.Fatalf("differences = %v, want exactly one", differences)
			}
			if differences[0].Path != testCase.path {
				t.Errorf("path = %q, want %q", differences[0].Path, testCase.path)
			}
			if differences[0].Reason != testCase.reason {
				t.Errorf("reason = %q, want %q", differences[0].Reason, testCase.reason)
			}
		})
	}
}

func TestAToleranceIsOffByDefaultAndAppliesWhenAsked(t *testing.T) {
	left, right := decodeOrFail(t, `{"v": 196.139862}`), decodeOrFail(t, `{"v": 196.139871}`)

	if SemanticallyEqual(left, right, CompareOptions{}) {
		t.Error("two different values compared equal with no tolerance")
	}
	if !SemanticallyEqual(left, right, CompareOptions{NumberTolerance: 1e-4}) {
		t.Error("a float32 rounding difference failed a comparison with a tolerance for it")
	}
	if SemanticallyEqual(left, decodeOrFail(t, `{"v": 197}`),
		CompareOptions{NumberTolerance: 1e-4}) {
		t.Error("the tolerance swallowed a whole unit")
	}
}

func TestIgnoredMembersAreSkippedAtEveryDepth(t *testing.T) {
	left := decodeOrFail(t, `{"document_id": "a", "e": [{"document_id": "x", "v": 1}]}`)
	right := decodeOrFail(t, `{"document_id": "b", "e": [{"document_id": "y", "v": 1}]}`)

	if differences := SemanticDiff(left, right, CompareOptions{}); len(differences) != 2 {
		t.Fatalf("without the option: %v, want two differences", differences)
	}
	if !SemanticallyEqual(left, right, CompareOptions{IgnoreMembers: []string{"document_id"}}) {
		t.Error("the ignored member still produced a difference")
	}
}

func TestTheReportIsBoundedAndDeterministic(t *testing.T) {
	var left, right strings.Builder
	left.WriteString(`{"e": [`)
	right.WriteString(`{"e": [`)
	for index := 0; index < 200; index++ {
		if index > 0 {
			left.WriteString(",")
			right.WriteString(",")
		}
		left.WriteString("0")
		right.WriteString("1")
	}
	left.WriteString(`]}`)
	right.WriteString(`]}`)

	first := SemanticDiff(decodeOrFail(t, left.String()), decodeOrFail(t, right.String()),
		CompareOptions{MaxDifferences: 5})
	if len(first) != 5 {
		t.Fatalf("differences = %d, want the requested bound of 5", len(first))
	}
	second := SemanticDiff(decodeOrFail(t, left.String()), decodeOrFail(t, right.String()),
		CompareOptions{MaxDifferences: 5})
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("two runs disagree at %d: %v vs %v", index, first[index], second[index])
		}
	}
}

// A document that cannot be canonicalised is not one this comparison can make a claim about, and
// saying so is better than comparing the literals and calling them equal.
func TestADocumentThatCannotBeCanonicalisedIsReportedRatherThanCompared(t *testing.T) {
	differences := SemanticDiff(decodeOrFail(t, `{"v": 1e400}`), decodeOrFail(t, `{"v": 1e400}`),
		CompareOptions{})
	if len(differences) != 1 || !strings.Contains(differences[0].Reason, "canonicalised") {
		t.Fatalf("differences = %v", differences)
	}
}

// ── the semantic round trip of the .map <-> APMap codec ────────────────────

// **AUE's Go codec is canonical for `.map` <-> APMap.** It is what the service runs, it is what
// writes the current contract, and it is the implementation every other component reads the output
// of.
//
// Its exact bytes are pinned by TestConversionOfCommittedFixturesIsFrozen. This is the other claim,
// and it holds whatever the bytes are: **the codec must not lose or invent anything when its own
// output is fed back through it.** It is independent of the byte comparison — a codec change that
// reordered members would pass here and fail there, and one that dropped a face would fail both.
func TestTheMapCodecIsSemanticallyStableAcrossItsOwnRoundTrip(t *testing.T) {
	fixtures := []string{
		filepath.Join("..", "mapfile", "testdata", "one_room.map"),
		filepath.Join("..", "mapfile", "testdata", "door_trigger.map"),
		filepath.Join("..", "mapfile", "testdata", "train_path.map"),
		filepath.Join("..", "mapfile", "testdata", "known_good", "one_cube.map"),
		filepath.Join("..", "mapfile", "testdata", "q2", "basic_q2.map"),
	}
	ran := 0
	for _, fixture := range fixtures {
		if _, err := os.Stat(fixture); err != nil {
			continue
		}
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			ran++
			name := strings.TrimSuffix(filepath.Base(fixture), filepath.Ext(fixture))
			options := ConvertOptions{
				Identity:   SourceMapIdentity{SourceMap: name, SourceSHA256: strings.Repeat("a", 64)},
				Role:       RoleFullMap,
				DocumentID: name,
			}
			first, err := ImportMapFile(fixture, options)
			if err != nil {
				t.Fatalf("ImportMapFile: %v", err)
			}

			// Back out to `.map` text, and in again through the same importer. The intermediate is
			// the other supported codec, so this is the codec pair being closed, not one function
			// called twice.
			text, err := ExportMapText(first)
			if err != nil {
				t.Fatalf("ExportMapText: %v", err)
			}
			roundTripped := filepath.Join(t.TempDir(), filepath.Base(fixture))
			if err := os.WriteFile(roundTripped, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			second, err := ImportMapFile(roundTripped, options)
			if err != nil {
				t.Fatalf("ImportMapFile after export: %v", err)
			}

			// Exact: one representation on both sides, so there is no float32 to make room for.
			// Identity is compared too — the ids are SHA-256 over edit-stable components, so a
			// codec that changed what it hashes would show up here.
			if differences := SemanticDiff(first, second, CompareOptions{}); len(differences) != 0 {
				t.Errorf("the codec is not stable across its own round trip:\n%v", differences)
			}
		})
	}
	if ran == 0 && len(fixtures) > 0 {
		t.Skip("no .map fixture is reachable")
	}
}

// The comparator has to be able to FAIL on a real document, or the test above proves nothing.
func TestTheStabilityCheckWouldNoticeALostBrush(t *testing.T) {
	fixture := filepath.Join("..", "mapfile", "testdata", "one_room.map")
	if _, err := os.Stat(fixture); err != nil {
		t.Skip("the fixture is not reachable")
	}
	document, err := ImportMapFile(fixture, ConvertOptions{
		Identity:   SourceMapIdentity{SourceMap: "one_room", SourceSHA256: strings.Repeat("a", 64)},
		Role:       RoleFullMap,
		DocumentID: "one_room",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	// Move one coordinate by a unit, the smallest change that is unambiguously geometry.
	damaged := strings.Replace(encoded, "272", "273", 1)
	if damaged == encoded {
		t.Skip("the fixture does not contain the coordinate this test edits")
	}

	differences, err := DiffText(encoded, damaged, CompareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(differences) == 0 {
		t.Fatal("a moved coordinate produced no semantic difference")
	}
}
