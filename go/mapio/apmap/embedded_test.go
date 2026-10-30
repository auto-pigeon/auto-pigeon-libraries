package apmap

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// A standalone binary — the Companion's download on a user's machine, with no AULIBS checkout and
// no auto-pigeon-tools — must have a contract. Before the bundle was embedded, every Build & Run
// there failed at "APMAP_SCHEMA_DIR is not configured" (AUCOM/AUE/AUB 246I1.1).
func TestTheEmbeddedBundleIsAWholeContract(t *testing.T) {
	contract, err := EmbeddedContract()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := EmbeddedProvenance()
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version != provenance.Current.Version {
		t.Errorf("current %s, provenance %s", contract.Version, provenance.Current.Version)
	}
	if len(contract.Readable) != 1+len(provenance.Legacy) {
		t.Errorf("%d readable versions for %d files", len(contract.Readable), 1+len(provenance.Legacy))
	}
	if provenance.SourcePackage != "ts/apmap-schema" || provenance.SourceVersion == "" {
		t.Errorf("the bundle does not name its source: %+v", provenance)
	}
	if !strings.Contains(contract.Source, provenance.SourceVersion) {
		t.Errorf("the contract does not say where it came from: %q", contract.Source)
	}
}

// TestEmbeddedSnapshotMatchesTheCanonicalSchema is the drift check: the snapshot is derived from
// ts/apmap-schema/schema and must equal it, file for file and byte for byte, in both directions.
func TestEmbeddedSnapshotMatchesTheCanonicalSchema(t *testing.T) {
	canonical := filepath.Join(contractPackageRoot(), "schema")
	seen := map[string]bool{}
	err := fs.WalkDir(EmbeddedBundle(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || name == ProvenanceFile {
			return walkErr
		}
		seen[name] = true
		embedded, readErr := fs.ReadFile(EmbeddedBundle(), name)
		if readErr != nil {
			return readErr
		}
		source, readErr := os.ReadFile(filepath.Join(canonical, filepath.FromSlash(name)))
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(embedded, source) {
			t.Errorf("%s differs from ts/apmap-schema/schema; run go run ./go/mapio/internal/schemasync", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(canonical, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, relErr := filepath.Rel(canonical, path)
		if relErr != nil {
			return relErr
		}
		if name := filepath.ToSlash(relative); !seen[name] {
			t.Errorf("%s is in ts/apmap-schema/schema and not in the snapshot", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := os.ReadFile(filepath.Join(contractPackageRoot(), "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Version string `json:"version"`
	}
	if err = json.Unmarshal(manifest, &source); err != nil {
		t.Fatal(err)
	}
	provenance, err := EmbeddedProvenance()
	if err != nil {
		t.Fatal(err)
	}
	if provenance.SourceVersion != source.Version {
		t.Errorf("the snapshot names apmap-schema %s and the package is %s", provenance.SourceVersion, source.Version)
	}
}

func TestABundleThatDoesNotMatchItsProvenanceIsRefused(t *testing.T) {
	provenance := Provenance{Current: ProvenanceEntry{Version: "1.3", File: "apmap-1.3.schema.json",
		SHA256: "0000"}}
	bundle := fstest.MapFS{"apmap-1.3.schema.json": {Data: []byte(`{}`)}}
	if err := verifyProvenance(bundle, provenance); err == nil || !strings.Contains(err.Error(), "hashes to") {
		t.Fatalf("a changed file was accepted: %v", err)
	}
	stray := fstest.MapFS{
		"apmap-1.3.schema.json":            {Data: []byte(`{}`)},
		"deprecated/apmap-0.9.schema.json": {Data: []byte(`{}`)},
	}
	provenance.Current.SHA256 = "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"
	if err := verifyProvenance(stray, provenance); err == nil || !strings.Contains(err.Error(), "not in its provenance") {
		t.Fatalf("an unaccounted schema was accepted: %v", err)
	}
}
