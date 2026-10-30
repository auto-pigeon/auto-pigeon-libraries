package mapio

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Q2_002. A Quake II `.map` goes to APMap and back with nothing this repository cannot read again, and
// the APMap says the dialect its faces are written in.

const q2ValveSource = `{
"classname" "worldspawn"
{
( 0 0 0 ) ( 0 1 0 ) ( 0 0 1 ) e1u1/floor [ 0 1 0 0.000034 ] [ 0 0 -1 0 ] 0 1 1 0 0 0
( 64 0 0 ) ( 64 0 1 ) ( 64 1 0 ) e1u1/floor [ 0 1 0 0 ] [ 0 0 -1 0 ] 15 0.5 2 1 2 300
( 0 0 0 ) ( 0 0 1 ) ( 1 0 0 ) e1u1/floor [ 1 0 0 13 ] [ 0 0 -1 -7 ] 0 1 1
( 0 64 0 ) ( 1 64 0 ) ( 0 64 1 ) e1u1/floor [ 1 0 0 0 ] [ 0 0 -1 0 ] 0 1 1 0 0 0
( 0 0 0 ) ( 1 0 0 ) ( 0 1 0 ) e1u1/floor [ 1 0 0 0 ] [ 0 -1 0 0 ] 0 1 1 0 0 0
( 0 0 64 ) ( 0 1 64 ) ( 1 0 64 ) e1u1/floor [ 1 0 0 0 ] [ 0 -1 0 0 ] 0 1 1 0 0 0
}
}
`

const q2ClassicSource = `{
"classname" "worldspawn"
{
( 0 0 0 ) ( 0 1 0 ) ( 0 0 1 ) e1u1/floor 0.000034 0 0 1 1 0 0 0
( 64 0 0 ) ( 64 0 1 ) ( 64 1 0 ) e1u1/floor 0 0 0 1 1 0 0 0
( 0 0 0 ) ( 0 0 1 ) ( 1 0 0 ) e1u1/floor 0 0 0 1 1
( 0 64 0 ) ( 1 64 0 ) ( 0 64 1 ) e1u1/floor 0 0 0 1 1 0 0 0
( 0 0 0 ) ( 1 0 0 ) ( 0 1 0 ) e1u1/floor 0 0 0 1 1 0 0 0
( 0 0 64 ) ( 0 1 64 ) ( 1 0 64 ) e1u1/floor 0 0 0 1 1 0 0 0
}
}
`

func convertText(t *testing.T, direction, name, text string) string {
	t.Helper()
	resolved, ok := LookupDirection(direction)
	if !ok {
		t.Fatalf("unknown direction %q", direction)
	}
	output, _, err := Convert(context.Background(), resolved, name, []byte(text), Options{})
	if err != nil {
		t.Fatalf("%s %s: %v", direction, name, err)
	}
	return string(output)
}

func TestQuake2DialectIsTheFacesDialect(t *testing.T) {
	for _, tc := range []struct{ source, want string }{{q2ValveSource, "valve_220"}, {q2ClassicSource, "quake_standard"}} {
		var document struct {
			Game       string `json:"game"`
			MapDialect string `json:"map_dialect"`
		}
		if err := json.Unmarshal([]byte(convertText(t, "q2map-to-apmap", "room.map", tc.source)), &document); err != nil {
			t.Fatal(err)
		}
		if document.Game != "quake2" || document.MapDialect != tc.want {
			t.Errorf("game/map_dialect = %s/%s, want quake2/%s", document.Game, document.MapDialect, tc.want)
		}
	}
}

func TestQuake2SmallValuesAreWrittenPositionallyAndReadBack(t *testing.T) {
	for _, source := range []string{q2ValveSource, q2ClassicSource} {
		document := convertText(t, "q2map-to-apmap", "room.map", source)
		if strings.Contains(document, "e-05") || !strings.Contains(document, "0.000034") {
			t.Fatalf("SER-7: the APMap must spell 0.000034 positionally:\n%s", document)
		}
		exported := convertText(t, "apmap-to-q2map", "room.apmap", document)
		if strings.Contains(exported, "e-05") || !strings.Contains(exported, "0.000034") {
			t.Fatalf("the .map export must spell 0.000034 positionally:\n%s", exported)
		}
		// The export is readable by this repository's own parser, and converts to the same document.
		again := convertText(t, "q2map-to-apmap", "room.map", exported)
		var first, second map[string]any
		_ = json.Unmarshal([]byte(document), &first)
		_ = json.Unmarshal([]byte(again), &second)
		if a, b := contentOf(first), contentOf(second); a != b {
			t.Errorf("content changed through apmap-to-q2map → q2map-to-apmap:\n%s\n---\n%s", a, b)
		}
	}
}

// contentOf is a document's entities without ids or derivation records — what a `.map` carries.
func contentOf(document map[string]any) string {
	var strip func(value any) any
	strip = func(value any) any {
		switch typed := value.(type) {
		case map[string]any:
			out := map[string]any{}
			for key, member := range typed {
				if key == "face_id" || key == "brush_id" || key == "entity_id" || key == "derived_from" {
					continue
				}
				out[key] = strip(member)
			}
			return out
		case []any:
			out := make([]any, len(typed))
			for index, member := range typed {
				out[index] = strip(member)
			}
			return out
		}
		return value
	}
	encoded, _ := json.Marshal(strip(document["entities"]))
	return string(encoded)
}
