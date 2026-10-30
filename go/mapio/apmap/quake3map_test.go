package apmap

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// The two number spellings are the editor's (AUP writer.ts `formatMapNumber` and
// `formatExactMapNumber`), row for row over every class of number they treat differently —
// integers, -0, shortest round-trip decimals, negatives, either side of 1e-6 where JavaScript
// switches to an exponent, long mantissas. The table is the editor's own output, produced by
// scripts/q3map-parity.sh and pinned in go/mapio/testdata/q3map/PINS.json.
func TestQuake3NumberSpellingsMatchTheEditor(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "q3map", "number-spellings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Value string `json:"value"`
		Map   string `json:"map"`
		Exact string `json:"exact"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) < 50 {
		t.Fatalf("only %d rows", len(rows))
	}
	for _, row := range rows {
		value := math.Copysign(0, -1)
		if row.Value != "-0" {
			value, err = strconv.ParseFloat(row.Value, 64)
			if err != nil {
				t.Fatalf("%s: %v", row.Value, err)
			}
		}
		if got := formatQuake3MapNumber(value); got != row.Map {
			t.Errorf("map spelling of %s = %q, the editor writes %q", row.Value, got, row.Map)
		}
		if got := formatQuake3ExactNumber(value); got != row.Exact {
			t.Errorf("exact spelling of %s = %q, the editor writes %q", row.Value, got, row.Exact)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	for _, c := range []struct {
		version string
		want    bool
	}{{"1.5", true}, {"1.6", true}, {"1.10", true}, {"2.0", true}, {"1.4", false}, {"0.9", false}, {"", false}, {"1", false}, {"1.x", false}} {
		if got := versionAtLeast(c.version, "1.5"); got != c.want {
			t.Errorf("versionAtLeast(%q) = %v", c.version, got)
		}
	}
}
