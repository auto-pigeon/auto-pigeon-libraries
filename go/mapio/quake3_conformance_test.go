package mapio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
)

// The Quake III `.map` -> APMap conformance set this module shares with the TypeScript reference:
// the q3-*.map fixtures and `quake3-conformance/` under ts/mapio/test/fixtures. facts.json holds
// hand-derived facts both implementations must meet; reference/ holds the TypeScript importer's
// own outputs, which this one must reproduce byte for byte but for the two provenance lines that
// name the producer. See ts/mapio/test/quake3Conformance.test.ts for the other half.

func quake3FixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "ts", "mapio", "test", "fixtures")
	if _, err := os.Stat(filepath.Join(dir, "quake3-conformance", "facts.json")); err != nil {
		t.Skip("the shared conformance fixtures (ts/mapio/test/fixtures) are not beside this checkout")
	}
	return dir
}

type quake3Facts struct {
	Imports map[string]struct {
		Game          string                     `json:"game"`
		MapDialect    string                     `json:"map_dialect"`
		Summary       map[string]json.RawMessage `json:"summary"`
		Kinds         map[string][]string        `json:"kinds"`
		Facts         [][2]json.RawMessage       `json:"facts"`
		ExportRefused bool                       `json:"export_refused"`
	} `json:"imports"`
	Refusals map[string]struct {
		Code     string            `json:"code"`
		First    []json.RawMessage `json:"first"`
		Rejected json.RawMessage   `json:"rejected"`
	} `json:"refusals"`
}

func loadQuake3Facts(t *testing.T, dir string) quake3Facts {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "quake3-conformance", "facts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var facts quake3Facts
	if err := json.Unmarshal(data, &facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

// plainJSON decodes JSON into comparable plain values: numbers as float64, so 0.5 and 0.50 agree.
func plainJSON(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	return value
}

// jsonPointer is RFC 6901 over decoded JSON; nil when the path is absent.
func jsonPointer(value any, path string) any {
	for _, token := range strings.Split(path, "/")[1:] {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch typed := value.(type) {
		case map[string]any:
			value = typed[token]
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(typed) {
				return nil
			}
			value = typed[index]
		default:
			return nil
		}
	}
	return value
}

func convertQuake3(t *testing.T, direction, name string, data []byte) ([]byte, Result, error) {
	t.Helper()
	resolved, ok := LookupDirection(direction)
	if !ok {
		t.Fatalf("no %s direction", direction)
	}
	return Convert(context.Background(), resolved, name, data, Options{Validate: true})
}

// withoutProducer drops the two lines that name the implementation, and says whether they were the
// Go ones — so a document whose producer is wrong fails rather than being compared away.
func withoutProducer(t *testing.T, text string, wantGo bool) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	seen := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, `"producer":`):
			seen++
			if wantGo && trimmed != `"producer": "`+apmap.Quake3ImportProducer+`",` {
				t.Fatalf("producer line %q does not name this implementation", trimmed)
			}
		case strings.HasPrefix(trimmed, `"producer_version":`):
			seen++
		default:
			kept = append(kept, line)
		}
	}
	if seen != 2 {
		t.Fatalf("expected exactly the producer and producer_version lines, found %d", seen)
	}
	return strings.Join(kept, "\n")
}

func TestQuake3ConformanceImports(t *testing.T) {
	dir := quake3FixtureDir(t)
	facts := loadQuake3Facts(t, dir)
	reference := filepath.Join(dir, "quake3-conformance", "reference")

	var fixtures []string
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "q3-") && strings.HasSuffix(entry.Name(), ".map") {
			fixtures = append(fixtures, entry.Name())
		}
	}
	named := []string{}
	for name := range facts.Imports {
		named = append(named, name)
	}
	for name := range facts.Refusals {
		named = append(named, name)
	}
	sort.Strings(fixtures)
	sort.Strings(named)
	if !reflect.DeepEqual(fixtures, named) {
		t.Fatalf("the conformance facts name %v, the fixtures are %v", named, fixtures)
	}

	for name, expected := range facts.Imports {
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			output, result, err := convertQuake3(t, "q3map-to-apmap", name, source)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !result.Validated {
				t.Fatal("validation was asked for and did not run")
			}
			document := plainJSON(t, output)
			if game := jsonPointer(document, "/game"); game != expected.Game {
				t.Errorf("game %v, want %s", game, expected.Game)
			}
			if expected.MapDialect != "" && jsonPointer(document, "/map_dialect") != expected.MapDialect {
				t.Errorf("map_dialect %v, want %s", jsonPointer(document, "/map_dialect"), expected.MapDialect)
			}
			summaryJSON, _ := json.Marshal(result.Quake3Import)
			summary := plainJSON(t, summaryJSON).(map[string]any)
			for key, raw := range expected.Summary {
				if want := plainJSON(t, raw); !reflect.DeepEqual(summary[key], want) {
					t.Errorf("summary.%s = %v, want %v", key, summary[key], want)
				}
			}
			for entity, kinds := range expected.Kinds {
				content, _ := jsonPointer(document, "/entities/"+entity+"/content").([]any)
				got := []string{}
				for _, item := range content {
					got = append(got, item.(map[string]any)["kind"].(string))
				}
				if !reflect.DeepEqual(got, kinds) {
					t.Errorf("entity %s content kinds %v, want %v", entity, got, kinds)
				}
			}
			for _, fact := range expected.Facts {
				var path string
				_ = json.Unmarshal(fact[0], &path)
				if got, want := jsonPointer(document, path), plainJSON(t, fact[1]); !reflect.DeepEqual(got, want) {
					t.Errorf("%s = %v, want %v", path, got, want)
				}
			}

			// Cross-language: the reference's canonical text, but for the producer.
			want, err := os.ReadFile(filepath.Join(reference, name+".apmap"))
			if err != nil {
				t.Fatal(err)
			}
			if withoutProducer(t, string(output), true) != withoutProducer(t, string(want), false) {
				t.Errorf("the document differs from the TypeScript reference beyond its producer")
			}
			wantSummary, _ := os.ReadFile(filepath.Join(reference, name+".summary.json"))
			if !reflect.DeepEqual(summary, plainJSON(t, wantSummary)) {
				t.Errorf("summary %s, want the reference %s", summaryJSON, wantSummary)
			}

			// MAP -> APMap -> MAP -> APMap.
			written, _, err := convertQuake3(t, "apmap-to-q3map", name, output)
			if expected.ExportRefused {
				if err == nil || RefusalOf(err).Code != RefusalQuake3Map {
					t.Fatalf("export: %v, want the Quake III writer's refusal", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("export refused: %v", err)
			}
			wantWritten, _ := os.ReadFile(filepath.Join(reference, name+".export.map"))
			if !bytes.Equal(written, wantWritten) {
				t.Errorf("the written .map differs from the TypeScript writer's")
			}
			again, _, err := convertQuake3(t, "q3map-to-apmap", name, written)
			if err != nil {
				t.Fatalf("re-import refused: %v", err)
			}
			rewritten, _, err := convertQuake3(t, "apmap-to-q3map", name, again)
			if err != nil || !bytes.Equal(rewritten, written) {
				t.Fatalf("the written .map is not a fixed point of read and write (%v)", err)
			}
			strip := func(value any) any {
				encoded, _ := json.Marshal(value)
				var out any
				_ = json.Unmarshal(encoded, &out)
				var walk func(any) any
				walk = func(node any) any {
					switch typed := node.(type) {
					case map[string]any:
						for _, key := range []string{"entity_id", "brush_id", "face_id", "patch_id", "derived_from", "provenance"} {
							delete(typed, key)
						}
						for key, inner := range typed {
							typed[key] = walk(inner)
						}
					case []any:
						for index, inner := range typed {
							typed[index] = walk(inner)
						}
					}
					return node
				}
				return walk(out)
			}
			if !reflect.DeepEqual(strip(plainJSON(t, again)), strip(document)) {
				t.Errorf("the re-imported document differs from the first beyond identity")
			}
		})
	}
}

func TestQuake3ConformanceRefusals(t *testing.T) {
	dir := quake3FixtureDir(t)
	facts := loadQuake3Facts(t, dir)
	for name, expected := range facts.Refusals {
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			output, result, err := convertQuake3(t, "q3map-to-apmap", name, source)
			if len(output) != 0 || result.Quake3Import != nil {
				t.Fatal("a refused import returned output")
			}
			refusal := RefusalOf(err)
			if refusal.Code != RefusalQuake3Import || refusal.Detail != expected.Code {
				t.Fatalf("refusal %+v, want quake3_import/%s", refusal, expected.Code)
			}
			var imported *apmap.Quake3ImportError
			if !errors.As(err, &imported) {
				t.Fatalf("%T is not a *apmap.Quake3ImportError", err)
			}
			parse := []any{}
			for _, one := range imported.ParseErrors {
				parse = append(parse, []any{one.Code, float64(one.Line), float64(one.Column), float64(one.EntityIndex), float64(one.BrushIndex), one.Message})
			}
			rejected := []any{}
			for _, one := range imported.Rejected {
				codes := []any{}
				for _, code := range one.Codes {
					codes = append(codes, code)
				}
				rejected = append(rejected, map[string]any{"entity": float64(one.Entity), "brush": float64(one.Brush), "line": float64(one.Line), "codes": codes})
			}
			got := map[string]any{"code": imported.Code, "message": imported.Message, "parse": parse, "rejected": rejected}
			want, err := os.ReadFile(filepath.Join(dir, "quake3-conformance", "reference", name+".refusal.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, plainJSON(t, want)) {
				t.Errorf("refusal %v, want the reference %s", got, want)
			}
			if len(expected.First) == 3 && len(parse) > 0 {
				first := parse[0].([]any)
				var code string
				var line, column float64
				_ = json.Unmarshal(expected.First[0], &code)
				_ = json.Unmarshal(expected.First[1], &line)
				_ = json.Unmarshal(expected.First[2], &column)
				if first[0] != code || first[1] != line || first[2] != column {
					t.Errorf("first fault %v, want %s at %v:%v", first[:3], code, line, column)
				}
			}
			if len(expected.Rejected) > 0 && !reflect.DeepEqual(rejected, plainJSON(t, expected.Rejected)) {
				t.Errorf("rejected %v, want %s", rejected, expected.Rejected)
			}
		})
	}
}
