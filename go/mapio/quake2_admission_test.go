package mapio

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/internal/apmaptest"
)

// Q2_001. The AULIBS contract's Quake II vectors, read from the contract's own index so a vector
// added upstream is exercised here without editing this file.
type quake2Sheet struct {
	Valid []struct {
		File string `json:"file"`
	} `json:"valid"`
	Admission []struct {
		File     string `json:"file"`
		Rule     string `json:"rule"`
		FaceID   string `json:"face_id"`
		EntityID string `json:"entity_id"`
		Key      string `json:"key"`
	} `json:"quake2_admission"`
}

func loadQuake2Sheet(t *testing.T) quake2Sheet {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(apmaptest.Vectors(), "index.json"))
	if err != nil {
		t.Skipf("contract vectors unavailable: %v", err)
	}
	var sheet quake2Sheet
	if err := json.Unmarshal(data, &sheet); err != nil {
		t.Fatal(err)
	}
	if len(sheet.Admission) == 0 {
		t.Skip("this contract has no quake2_admission vectors")
	}
	return sheet
}

func convertVector(t *testing.T, path, direction string, opts Options) ([]byte, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, ok := LookupDirection(direction)
	if !ok {
		t.Fatalf("unknown direction %q", direction)
	}
	output, _, err := Convert(context.Background(), resolved, filepath.Base(path), data, opts)
	return output, err
}

// TestQuake2AdmissionRefusesExactlyTheContractRule: every quake2_admission vector is a VALID
// document (validation on), refused by apmap-to-q2map for exactly the rule and object the contract
// names, with nothing written — and converted unchanged by apmap-to-q1map, whose output a Quake II
// compiler never reads.
func TestQuake2AdmissionRefusesExactlyTheContractRule(t *testing.T) {
	sheet := loadQuake2Sheet(t)
	for _, entry := range sheet.Admission {
		t.Run(entry.File, func(t *testing.T) {
			path := filepath.Join(apmaptest.Vectors(), "quake2-admission", entry.File)
			output, err := convertVector(t, path, "apmap-to-q2map", Options{Validate: true})
			var refusal *apmap.Quake2AdmissionError
			if !errors.As(err, &refusal) {
				t.Fatalf("apmap-to-q2map = %v, want a Quake2AdmissionError", err)
			}
			if output != nil {
				t.Error("a refused conversion wrote output")
			}
			if refusal.Rule != entry.Rule {
				t.Errorf("rule = %s, want %s (%v)", refusal.Rule, entry.Rule, err)
			}
			want := entry.FaceID
			if want == "" {
				want = entry.EntityID
			}
			if refusal.ObjectID != want || refusal.Key != entry.Key {
				t.Errorf("refused %s key %q, want %s key %q", refusal.ObjectID, refusal.Key, want, entry.Key)
			}
			if !strings.Contains(err.Error(), "nothing was written") {
				t.Errorf("message %q does not say nothing was written", err)
			}
			if _, err := convertVector(t, path, "apmap-to-q1map", Options{Validate: true}); err != nil {
				t.Errorf("apmap-to-q1map changed behaviour: %v", err)
			}
		})
	}
}

// TestQuake2VectorsConvertAndKeepTheirTails: every valid q2-* vector passes admission, and the
// written `.map` carries each tail as the contract spells it — an absent tail stays absent, the
// sign bit stays signed.
func TestQuake2VectorsConvertAndKeepTheirTails(t *testing.T) {
	sheet := loadQuake2Sheet(t)
	seen := 0
	for _, entry := range sheet.Valid {
		if !strings.HasPrefix(entry.File, "q2-") {
			continue
		}
		seen++
		output, err := convertVector(t, filepath.Join(apmaptest.Vectors(), "valid", entry.File), "apmap-to-q2map", Options{Validate: true})
		if err != nil {
			t.Fatalf("%s: %v", entry.File, err)
		}
		if entry.File == "q2-tail-bits.apmap" {
			for _, want := range []string{" -2147483648 0 0\n", " 679673856 0 0\n", " 1 3072 0\n", " 1 2147483647 2147483647\n"} {
				if !strings.Contains(string(output), want) {
					t.Errorf("%s: written map lacks tail %q", entry.File, want)
				}
			}
		}
		if entry.File == "q2-classic-sealed-room.apmap" && !strings.Contains(string(output), "( 144 144 0 ) ( 144 145 0 ) ( 145 144 0 ) aut_q2/floor 0 0 0 1 1\n") {
			t.Errorf("the absent tail was not written as absent:\n%s", output)
		}
	}
	if seen < 6 {
		t.Fatalf("only %d q2-* vectors in the contract", seen)
	}
}

// TestValidateOffStillRefusesAnUnknownVersion: --validate-off skips the schema and the SEM rules,
// never the version gate. Before Q2_001 a future APMap converted through it as if it were current.
func TestValidateOffStillRefusesAnUnknownVersion(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(apmaptest.Vectors(), "valid", "q2-classic-sealed-room.apmap"))
	if err != nil {
		t.Skip(err)
	}
	future := strings.Replace(string(data), `"apmap_version": "1.5"`, `"apmap_version": "9.9"`, 1)
	for _, direction := range []string{"apmap-to-q2map", "apmap-to-q1map", "apmap-to-q2rmf"} {
		resolved, _ := LookupDirection(direction)
		output, _, err := Convert(context.Background(), resolved, "future.apmap", []byte(future), Options{Validate: false})
		var version *apmap.VersionError
		if !errors.As(err, &version) {
			t.Errorf("%s with validation off = %v, want a VersionError", direction, err)
		}
		if output != nil {
			t.Errorf("%s wrote output for a future version", direction)
		}
		if err != nil && !strings.Contains(err.Error(), "APMap 9.9 is not a supported APMap version") {
			t.Errorf("%s: %v", direction, err)
		}
	}
	// The current version still converts with validation off.
	resolved, _ := LookupDirection("apmap-to-q2map")
	if _, _, err := Convert(context.Background(), resolved, "current.apmap", data, Options{Validate: false}); err != nil {
		t.Errorf("a current document with validation off: %v", err)
	}
}
