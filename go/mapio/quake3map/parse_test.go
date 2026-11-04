package quake3map

import (
	"context"
	"reflect"
	"testing"
)

func parse(t *testing.T, text string) *Map {
	t.Helper()
	parsed, err := Parse(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func codes(parsed *Map) [][3]any {
	out := [][3]any{}
	for _, fault := range parsed.Errors {
		out = append(out, [3]any{fault.Code, fault.Line, fault.Column})
	}
	return out
}

const cube = `( 0 64 16 ) ( 64 64 16 ) ( 64 0 16 ) a 0 0 0 1 1
( 0 0 0 ) ( 64 0 0 ) ( 64 64 0 ) a 0 0 0 1 1
( 64 0 16 ) ( 64 64 16 ) ( 64 64 0 ) a 0 0 0 1 1
( 0 64 16 ) ( 0 0 16 ) ( 0 0 0 ) a 0 0 0 1 1
`

func TestTheTokenizerIsQ3Map2s(t *testing.T) {
	parsed := parse(t, "// a comment\r\n{ ; another\n\"k\" \"a\\b ;c\" # third\n/* a\nspan */ \"x\" \"y\"\n{\n"+cube+"}\n}\n")
	if len(parsed.Errors) != 0 {
		t.Fatalf("faults: %v", parsed.Errors)
	}
	content := parsed.Entities[0].Content
	if got := content[0].(Property); got.Key != "k" || got.Value != `a\b ;c` || got.Line != 3 {
		t.Errorf("a backslash is an ordinary byte and a quoted ; is not a comment: %+v", got)
	}
	if got := content[1].(Property); got.Key != "x" || got.Line != 5 {
		t.Errorf("after a block comment: %+v", got)
	}
	if brush := content[2].(*Brush); brush.Syntax != SyntaxClassic || len(brush.Faces) != 4 || len(brush.Faces[0].Tail) != 0 {
		t.Errorf("the brush: %+v", brush)
	}
}

func TestATailIsReadOnlyOnItsOwnLine(t *testing.T) {
	text := "{\n{\n( 0 64 16 ) ( 64 64 16 ) ( 64 0 16 ) a 0 0 0 1 1 1 2\n3\n}\n}\n"
	if got := codes(parse(t, text)); !reflect.DeepEqual(got, [][3]any{{CodeUnexpectedToken, 4, 1}}) {
		t.Errorf("a tail number on the next line is the next face's start: %v", got)
	}
}

func TestFaultsArePositionedInUTF16Columns(t *testing.T) {
	// "é" is one UTF-16 unit, "😀" two; the fault after them is at the column the reference reports.
	parsed := parse(t, "{\n\"é😀\" x\n}\n")
	if got := codes(parsed); !reflect.DeepEqual(got, [][3]any{{CodeInvalidProperty, 2, 1}}) {
		t.Errorf("faults %v", got)
	}
	// Measured from the reference reader: column 13, where a byte count would say 15.
	parsed = parse(t, "{ \"é😀\" \"v\" x }\n")
	if got := codes(parsed); !reflect.DeepEqual(got, [][3]any{{CodeInvalidProperty, 1, 13}}) {
		t.Errorf("faults %v", got)
	}
}

func TestRefusalsByName(t *testing.T) {
	for text, want := range map[string]string{
		"{\n{\nbrushDef3\n{\n}\n}\n}\n":  CodeDoom3Grammar,
		"{\n{\nterrainDef\n{\n}\n}\n}\n": CodeUnknownBlock,
		"{\n{\n}\n}\n":                   CodeTooFewFaces,
		"{\n\"a\" \"b\n}\n":              CodeUnterminatedString,
		"{\n/* open\n}\n":                CodeUnterminatedComment,
		"\"stray\"\n":                    CodeContentOutsideEntity,
		"{\n{\n( 0 0 0 ) ( 1 0 0 ) ( 0 1 0 ) a [ 1 0 0 0 ] [ 0 1 0 0 ] 0 1 1\n( 0 0 0 ) ( 1 0 0 ) ( 0 1 0 ) a 0 0 0 1 1\n}\n}\n": CodeMixedSyntax,
		"{\n{\n( 0 0 0 ) ( 1 0 0 ) ( 0 1 0 ) a 0 0 0 1 1e999\n}\n}\n":                                                            CodeInvalidNumber,
		"{\n{\npatchDef2\n{\na\n( 3 4 0 0 0 )\n}\n}\n}\n":                                                                        CodePatchSize,
		"{\n": CodeUnexpectedEnd,
	} {
		parsed := parse(t, text)
		if len(parsed.Errors) == 0 || parsed.Errors[0].Code != want {
			t.Errorf("%q: %v, want %s first", text, parsed.Errors, want)
		}
	}
}

func TestNumbersAreTheReferencesRule(t *testing.T) {
	for text, want := range map[string]bool{
		"1": true, "-1": true, "+1.5": true, "1.": true, ".5": true, "1e5": true, "1E-5": true, "-.5e+3": true,
		"": false, ".": false, "e5": false, "1e": false, "0x10": false, "1_000": false, "inf": false, "NaN": false, "1.2.3": false,
	} {
		if got := isNumber(text); got != want {
			t.Errorf("isNumber(%q) = %v", text, got)
		}
	}
	if jsNumber(1.5e-7) != "1.5e-7" || jsNumber(4) != "4" || jsNumber(1e21) != "1e+21" || jsNumber(-0.5) != "-0.5" {
		t.Errorf("jsNumber: %s %s %s %s", jsNumber(1.5e-7), jsNumber(4), jsNumber(1e21), jsNumber(-0.5))
	}
}
