package apmap

import (
	"strings"
	"testing"
)

// TestUnpairedSurrogateEscapesAreSeenInTheRawText pins the scanner SEM-A-1 depends on. The decoded
// string cannot answer the question — encoding/json has already put U+FFFD where the surrogate was
// — so the answer comes from the member's own JSON text.
func TestUnpairedSurrogateEscapesAreSeenInTheRawText(t *testing.T) {
	for _, testCase := range []struct {
		raw      string
		unpaired bool
	}{
		{`: "Zo\ud83d"`, true},          // a high surrogate at the end
		{`: "\ude00 alone"`, true},      // a low surrogate with no high one
		{`: "😀 paired"`, false},         // a real pair: U+1F600
		{`: "😀 upper-case hex"`, false}, // hex is case-insensitive
		{`: "\ud83dx"`, true},           // high followed by a plain character
		{`: "\ud83d\n"`, true},          // high followed by another escape
		{`: "\ud83d😀"`, true},           // two highs: the first is unpaired
		{`: "\\ud83d is text, not an escape"`, false},
		{`: "literal U+FFFD � is legal text"`, false},
		{`: "é and —"`, false},
		{` : "Zo\ud83d" `, true}, // surrounding separators and whitespace
	} {
		if got := hasUnpairedSurrogateEscape([]byte(testCase.raw)); got != testCase.unpaired {
			t.Errorf("%s: unpaired = %v, want %v", testCase.raw, got, testCase.unpaired)
		}
	}
}

func TestDecodeRecordsWhichMemberLostASurrogate(t *testing.T) {
	document := mustDecode(t, `{"authorship": {"author": "Zo\ud83d", "license": "ok 😀", "copyright_notice": "a literal � is fine"}}`)
	authorship, _ := document.(*Object).Get("authorship")
	object := authorship.(*Object)
	if !object.HadUnpairedSurrogate("author") {
		t.Fatal("the unpaired surrogate in author was not recorded")
	}
	for _, name := range []string{"license", "copyright_notice"} {
		if object.HadUnpairedSurrogate(name) {
			t.Fatalf("%s was flagged, but its text is well-formed", name)
		}
	}
	// A value a caller supplies is not the one the file held.
	object.Set("author", "Zoë")
	if object.HadUnpairedSurrogate("author") {
		t.Fatal("Set did not clear the record")
	}
}

func TestSEMA1RefusesTextThatIsNotUnicode(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		document  func(t *testing.T) *Object
		wantFault bool
	}{
		{"an unpaired surrogate escape", func(t *testing.T) *Object {
			return mustDecode(t, `{"authorship": {"author": "Zo\ud83d"}}`).(*Object)
		}, true},
		{"a literal replacement character", func(t *testing.T) *Object {
			return mustDecode(t, `{"authorship": {"author": "Zo�"}}`).(*Object)
		}, false},
		{"a surrogate pair", func(t *testing.T) *Object {
			return mustDecode(t, `{"authorship": {"license": "😀"}}`).(*Object)
		}, false},
		{"bytes that are not UTF-8, built in memory", func(t *testing.T) *Object {
			authorship := NewObject()
			authorship.Set("copyright_notice", "\xed\xa0\xbd") // a WTF-8 lone surrogate
			document := NewObject()
			document.Set("authorship", authorship)
			return document
		}, true},
		{"no authorship at all", func(t *testing.T) *Object { return NewObject() }, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			problems := authorshipProblems(testCase.document(t))
			if got := len(problems) > 0; got != testCase.wantFault {
				t.Fatalf("problems = %v, want a fault: %v", problems, testCase.wantFault)
			}
			for _, problem := range problems {
				if !strings.HasPrefix(problem, "SEM-A-1 authorship.") {
					t.Fatalf("mis-labelled: %s", problem)
				}
			}
		})
	}
}

// groupsDocument is one entity owning two brushes (each with one face), plus the given groups.
func groupsDocument(t *testing.T, groups string) *Object {
	t.Helper()
	return mustDecode(t, `{
		"entities": [{"entity_id": "ent_world0000001", "content": [
			{"kind": "brush", "brush_id": "brs_a000000001", "faces": [{"face_id": "fac_a000000001"}]},
			{"kind": "brush", "brush_id": "brs_b000000001", "faces": [{"face_id": "fac_b000000001"}]}
		]}],
		"groups": `+groups+`}`).(*Object)
}

func TestGroupRulesOneByOne(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		groups string
		want   string // the one rule, or "" for none
	}{
		{"no groups", `[]`, ""},
		{"a group of one", `[{"group_id": "grp_one0000001", "members": [{"kind": "brush", "brush_id": "brs_a000000001"}]}]`, ""},
		{"a child declared after its parent", `[
			{"group_id": "grp_parent00001", "members": [{"kind": "group", "group_id": "grp_child000001"}, {"kind": "brush", "brush_id": "brs_b000000001"}]},
			{"group_id": "grp_child000001", "members": [{"kind": "brush", "brush_id": "brs_a000000001"}]}]`, ""},
		{"SEM-G-1 a dangling object", `[{"group_id": "grp_one0000001", "members": [{"kind": "brush", "brush_id": "brs_nowhere0001"}]}]`, "SEM-G-1"},
		{"SEM-G-1 a dangling group", `[{"group_id": "grp_one0000001", "members": [{"kind": "group", "group_id": "grp_nowhere0001"}]}]`, "SEM-G-1"},
		{"SEM-G-2 a kind mismatch", `[{"group_id": "grp_one0000001", "members": [{"kind": "entity", "entity_id": "brs_a000000001"}]}]`, "SEM-G-2"},
		{"SEM-G-3 a duplicate group_id", `[
			{"group_id": "grp_one0000001", "members": [{"kind": "brush", "brush_id": "brs_a000000001"}]},
			{"group_id": "grp_one0000001", "members": [{"kind": "brush", "brush_id": "brs_b000000001"}]}]`, "SEM-G-3"},
		{"SEM-G-4 an object in two groups", `[
			{"group_id": "grp_one0000001", "members": [{"kind": "brush", "brush_id": "brs_a000000001"}]},
			{"group_id": "grp_two0000001", "members": [{"kind": "brush", "brush_id": "brs_a000000001"}]}]`, "SEM-G-4"},
		{"SEM-G-4 a group with two parents", `[
			{"group_id": "grp_child000001", "members": [{"kind": "brush", "brush_id": "brs_a000000001"}]},
			{"group_id": "grp_one0000001", "members": [{"kind": "group", "group_id": "grp_child000001"}]},
			{"group_id": "grp_two0000001", "members": [{"kind": "group", "group_id": "grp_child000001"}]}]`, "SEM-G-4"},
		{"SEM-G-5 a brush and its face in one list", `[{"group_id": "grp_one0000001", "members": [
			{"kind": "brush", "brush_id": "brs_a000000001"}, {"kind": "face", "face_id": "fac_a000000001"}]}]`, "SEM-G-5"},
		{"SEM-G-5 an entity above, its face two levels below", `[
			{"group_id": "grp_top00000001", "members": [{"kind": "entity", "entity_id": "ent_world0000001"}, {"kind": "group", "group_id": "grp_mid00000001"}]},
			{"group_id": "grp_mid00000001", "members": [{"kind": "group", "group_id": "grp_low00000001"}]},
			{"group_id": "grp_low00000001", "members": [{"kind": "face", "face_id": "fac_b000000001"}]}]`, "SEM-G-5"},
		{"unrelated trees may split a brush from its face", `[
			{"group_id": "grp_one0000001", "members": [{"kind": "brush", "brush_id": "brs_a000000001"}]},
			{"group_id": "grp_two0000001", "members": [{"kind": "face", "face_id": "fac_a000000001"}]}]`, ""},
		{"SEM-G-8 a self-link", `[{"group_id": "grp_one0000001", "members": [{"kind": "group", "group_id": "grp_one0000001"}]}]`, "SEM-G-8"},
		{"SEM-G-8 a cycle of three", `[
			{"group_id": "grp_aaa00000001", "members": [{"kind": "group", "group_id": "grp_bbb00000001"}]},
			{"group_id": "grp_bbb00000001", "members": [{"kind": "group", "group_id": "grp_ccc00000001"}]},
			{"group_id": "grp_ccc00000001", "members": [{"kind": "group", "group_id": "grp_aaa00000001"}]}]`, "SEM-G-8"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			problems := groupProblems(groupsDocument(t, testCase.groups))
			rules := map[string]bool{}
			for _, problem := range problems {
				rules[strings.Fields(problem)[0]] = true
			}
			if testCase.want == "" {
				if len(problems) > 0 {
					t.Fatalf("a legal document was refused: %v", problems)
				}
				return
			}
			if len(rules) != 1 || !rules[testCase.want] {
				t.Fatalf("got %v, want exactly %s", problems, testCase.want)
			}
		})
	}
}

// A cycle is reported once, however many of its groups the pass starts from.
func TestACycleIsReportedOnce(t *testing.T) {
	problems := groupProblems(groupsDocument(t, `[
		{"group_id": "grp_aaa00000001", "members": [{"kind": "group", "group_id": "grp_bbb00000001"}]},
		{"group_id": "grp_bbb00000001", "members": [{"kind": "group", "group_id": "grp_aaa00000001"}]}]`))
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want one: %v", len(problems), problems)
	}
}

// The problem list is deterministic: Go's map order is random, and the list is compared element by
// element elsewhere.
func TestGroupProblemsAreDeterministic(t *testing.T) {
	groups := `[
		{"group_id": "grp_one0000001", "members": [{"kind": "brush", "brush_id": "brs_x000000001"}, {"kind": "brush", "brush_id": "brs_a000000001"}, {"kind": "face", "face_id": "fac_a000000001"}]},
		{"group_id": "grp_two0000001", "members": [{"kind": "brush", "brush_id": "brs_a000000001"}, {"kind": "group", "group_id": "grp_two0000001"}]}]`
	first := strings.Join(groupProblems(groupsDocument(t, groups)), "\n")
	for i := 0; i < 20; i++ {
		if again := strings.Join(groupProblems(groupsDocument(t, groups)), "\n"); again != first {
			t.Fatalf("run %d differs:\n%s\nvs\n%s", i, again, first)
		}
	}
}
