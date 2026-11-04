package mapio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/quake3map"
)

func quake3Fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(quake3FixtureDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The game is the direction's, never the file's: a classic Quake III face and a Quake 2 one look
// alike, so the same bytes import as either game, and Quake III-only syntax is refused by name.
func TestQuake3WrongGameControls(t *testing.T) {
	lookalike := quake3Fixture(t, "q3-q2-lookalike.map")
	for direction, game := range map[string]string{"q3map-to-apmap": "quake3", "q2map-to-apmap": "quake2", "q1map-to-apmap": "quake1"} {
		output, _, err := convertQuake3(t, direction, "lookalike.map", lookalike)
		if err != nil {
			t.Fatalf("%s: %v", direction, err)
		}
		if got := jsonPointer(plainJSON(t, output), "/game"); got != game {
			t.Errorf("%s wrote game %v, want %s", direction, got, game)
		}
	}
	// A Quake 1 room read as Quake III is a Quake III document: nothing detects, nothing guesses.
	room := quake3Fixture(t, "q1-classic-room.map")
	if output, _, err := convertQuake3(t, "q3map-to-apmap", "room.map", room); err != nil || jsonPointer(plainJSON(t, output), "/game") != "quake3" {
		t.Errorf("q3map-to-apmap of a Quake 1 room: %v", err)
	}
	for _, fixture := range []string{"q3-classic-patches.map", "q3-brushdef-patches.map"} {
		for _, direction := range []string{"q1map-to-apmap", "q2map-to-apmap"} {
			output, _, err := convertQuake3(t, direction, fixture, quake3Fixture(t, fixture))
			if len(output) != 0 {
				t.Fatalf("%s %s wrote output", direction, fixture)
			}
			refusal := RefusalOf(err)
			if refusal.Code != RefusalGame || !strings.Contains(refusal.Message, "q3map-to-apmap") || !strings.Contains(refusal.Message, direction) {
				t.Errorf("%s %s: %+v, want the actionable unsupported_game refusal", direction, fixture, refusal)
			}
		}
	}
	// A malformed Quake 1 map without Quake III syntax keeps its plain syntax refusal.
	if _, _, err := convertQuake3(t, "q1map-to-apmap", "bad.map", quake3Fixture(t, "q1-malformed.map")); RefusalOf(err).Code != RefusalInvalidInput {
		t.Errorf("a malformed Quake 1 map: %+v", RefusalOf(err))
	}
}

func TestQuake3ImportBounds(t *testing.T) {
	_, _, err := convertQuake3(t, "q3map-to-apmap", "huge.map", make([]byte, MaxQuake3MapImportBytes+1))
	if refusal := RefusalOf(err); refusal.Code != RefusalTooLarge {
		t.Errorf("an oversized input: %+v", refusal)
	}
	var builder strings.Builder
	builder.WriteString("{\n\"classname\" \"worldspawn\"\n{\n")
	for face := 0; face <= apmap.MaxQuake3BrushFaces; face++ {
		fmt.Fprintf(&builder, "( 0 0 %d ) ( 1 0 %d ) ( 0 1 %d ) mapio/x 0 0 0 1 1\n", face, face, face)
	}
	builder.WriteString("}\n}\n")
	output, _, err := convertQuake3(t, "q3map-to-apmap", "faces.map", []byte(builder.String()))
	if refusal := RefusalOf(err); len(output) != 0 || refusal.Code != RefusalTooLarge || refusal.Detail != apmap.Quake3ImportTooLarge {
		t.Errorf("a brush past the face bound: %+v", refusal)
	}
}

// countdownContext is cancelled after its Err has been asked a number of times: a cancellation that
// arrives in the MIDDLE of a parse or an encode, at a deterministic point.
type countdownContext struct {
	context.Context
	left atomic.Int64
}

func (c *countdownContext) Err() error {
	if c.left.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}
func (c *countdownContext) Done() <-chan struct{}       { return nil }
func (c *countdownContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func manyBrushMap(brushes int) []byte {
	var builder strings.Builder
	builder.WriteString("{\n\"classname\" \"worldspawn\"\n")
	for index := 0; index < brushes; index++ {
		x := index * 64
		fmt.Fprintf(&builder, "{\n( %d 64 16 ) ( %d 64 16 ) ( %d 0 16 ) mapio/a 0 0 0 1 1\n( %d 0 0 ) ( %d 0 0 ) ( %d 64 0 ) mapio/a 0 0 0 1 1\n"+
			"( %d 0 16 ) ( %d 64 16 ) ( %d 64 0 ) mapio/a 0 0 0 1 1\n( %d 64 16 ) ( %d 0 16 ) ( %d 0 0 ) mapio/a 0 0 0 1 1\n"+
			"( %d 64 16 ) ( %d 64 16 ) ( %d 64 0 ) mapio/a 0 0 0 1 1\n( %d 0 16 ) ( %d 0 16 ) ( %d 0 0 ) mapio/a 0 0 0 1 1\n}\n",
			x, x+64, x+64, x, x+64, x+64, x+64, x+64, x+64, x, x, x, x+64, x, x, x, x+64, x+64)
	}
	builder.WriteString("}\n")
	return []byte(builder.String())
}

func TestQuake3ImportCancellation(t *testing.T) {
	direction, _ := LookupDirection("q3map-to-apmap")
	data := manyBrushMap(200)
	if _, result, err := Convert(context.Background(), direction, "many.map", data, Options{}); err != nil || result.Quake3Import.Brushes != 200 {
		t.Fatalf("the uncancelled import: %v", err)
	}
	// Every count from 0 (before anything) through a point deep in the brush checks.
	for _, after := range []int64{0, 1, 3, 50, 150, 250, 330} {
		ctx := &countdownContext{Context: context.Background()}
		ctx.left.Store(after)
		output, result, err := Convert(ctx, direction, "many.map", data, Options{})
		if len(output) != 0 || result.Quake3Import != nil || RefusalOf(err).Code != RefusalCancelled {
			t.Errorf("cancelled after %d checks: %d bytes, %+v", after, len(output), RefusalOf(err))
		}
	}
	// The reader alone stops too, and returns no tree.
	ctx := &countdownContext{Context: context.Background()}
	ctx.left.Store(20)
	if parsed, err := quake3map.Parse(ctx, string(data)); parsed != nil || err == nil {
		t.Errorf("a cancelled parse returned a tree")
	}
}

// The editor decodes a picked file with the WHATWG decoder, and so does the importer: a BOM is
// dropped and each maximal invalid subsequence is one U+FFFD.
func TestQuake3DecodeText(t *testing.T) {
	for input, want := range map[string]string{
		"\xEF\xBB\xBFabc":  "abc",
		"caf\xE9":          "caf�",
		"\xE2\x82A":        "�A",
		"\xF0\x9F\x98":     "�",
		"\xC0\xAF":         "��",
		"\xED\xA0\x80":     "���",
		"\xF4\x90\x80\x80": "����",
		"ok \xE2\x82\xAC":  "ok €",
	} {
		if got := quake3map.DecodeText([]byte(input)); got != want {
			t.Errorf("DecodeText(%q) = %q, want %q", input, got, want)
		}
	}
	// Identity is the BYTES' digest, so a BOM changes the ids while the content stays the same.
	room := quake3Fixture(t, "q3-q2-lookalike.map")
	plain, _, err := convertQuake3(t, "q3map-to-apmap", "room.map", room)
	if err != nil {
		t.Fatal(err)
	}
	withBOM, _, err := convertQuake3(t, "q3map-to-apmap", "room.map", append([]byte("\xEF\xBB\xBF"), room...))
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) == string(withBOM) || jsonPointer(plainJSON(t, plain), "/entities/0/content/0/value") != jsonPointer(plainJSON(t, withBOM), "/entities/0/content/0/value") {
		t.Error("a BOM should change the source digest and nothing else")
	}
}
