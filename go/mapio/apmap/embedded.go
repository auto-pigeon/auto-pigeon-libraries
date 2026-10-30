package apmap

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
)

// The APMap contract bundle, compiled into every binary that imports this package.
//
// embedded/schema is a GENERATED snapshot of ts/apmap-schema/schema — the one current schema, every
// deprecated READ contract, and apmap-contract-provenance.json naming each file's source path and
// sha256. ts/apmap-schema stays the only editable contract: go:embed cannot reach a parent
// directory, so the bytes are copied here by go/mapio/internal/schemasync, and
// TestEmbeddedSnapshotMatchesTheCanonicalSchema fails when the two differ.
//
// The snapshot records no repository commit. A file cannot carry the hash of the commit that
// contains it, so identity is the content: the digests here, plus the module version a consumer's
// go.mod pins (mapio.BuildModuleVersion).
//
//go:embed embedded/schema
var embeddedBundle embed.FS

// EmbeddedSchemaDir is the embedded bundle's root inside embeddedBundle.
const EmbeddedSchemaDir = "embedded/schema"

// ProvenanceFile is the manifest every copy of the bundle carries.
const ProvenanceFile = "apmap-contract-provenance.json"

// Provenance is where a bundle came from. Current and Legacy use the field names of
// auto-pigeon-tools' staged bundle (scripts/apmap_bundle.py), so the image's copy and the binary's
// copy are described alike.
type Provenance struct {
	Generated     string            `json:"generated"`
	SourcePackage string            `json:"source_package"`
	SourceVersion string            `json:"source_version"`
	Current       ProvenanceEntry   `json:"current"`
	Legacy        []ProvenanceEntry `json:"readable_legacy"`
}

// ProvenanceEntry is one schema file of a bundle.
type ProvenanceEntry struct {
	Version string `json:"version"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
}

// EmbeddedBundle is the embedded bundle as a filesystem rooted at the schema directory.
func EmbeddedBundle() fs.FS {
	sub, err := fs.Sub(embeddedBundle, EmbeddedSchemaDir)
	if err != nil {
		// A build without the directory cannot compile (go:embed), so this is unreachable.
		panic("apmap: " + err.Error())
	}
	return sub
}

// EmbeddedProvenance reads the embedded bundle's provenance manifest.
func EmbeddedProvenance() (Provenance, error) {
	var provenance Provenance
	data, err := fs.ReadFile(EmbeddedBundle(), ProvenanceFile)
	if err != nil {
		return provenance, notFound("the embedded APMap bundle carries no %s: %v", ProvenanceFile, err)
	}
	if err = json.Unmarshal(data, &provenance); err != nil {
		return provenance, notFound("the embedded APMap bundle's %s is unreadable: %v", ProvenanceFile, err)
	}
	return provenance, nil
}

// EmbeddedContract loads the embedded bundle WITHOUT making it the process's default.
//
// Parsed and verified once: the bytes are compiled in and cannot change under a running process, so
// every caller shares one immutable value.
//
// Every schema is checked against the provenance manifest first: a file the manifest does not name,
// or one whose bytes are not the ones it records, is a bundle nobody can say the origin of.
func EmbeddedContract() (*Contract, error) { return embeddedContract() }

var embeddedContract = sync.OnceValues(parseEmbeddedContract)

func parseEmbeddedContract() (*Contract, error) {
	bundle := EmbeddedBundle()
	provenance, err := EmbeddedProvenance()
	if err != nil {
		return nil, err
	}
	if err = verifyProvenance(bundle, provenance); err != nil {
		return nil, err
	}
	contract, err := loadBundle(bundle, "embedded:apmap-schema")
	if err != nil {
		return nil, err
	}
	if contract.Version != provenance.Current.Version {
		return nil, notFound("the embedded APMap bundle's current schema is %s and its provenance says %s",
			contract.Version, provenance.Current.Version)
	}
	contract.Source = "embedded (apmap-schema " + provenance.SourceVersion + ")"
	return contract, nil
}

// verifyProvenance checks that the manifest names exactly the schema files present, at their bytes.
func verifyProvenance(bundle fs.FS, provenance Provenance) error {
	named := map[string]string{provenance.Current.File: provenance.Current.SHA256}
	for _, entry := range provenance.Legacy {
		named[entry.File] = entry.SHA256
	}
	present := map[string]bool{}
	err := fs.WalkDir(bundle, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || name == ProvenanceFile {
			return walkErr
		}
		present[name] = true
		want, ok := named[name]
		if !ok {
			return fmt.Errorf("%s is in the embedded APMap bundle and not in its provenance", name)
		}
		data, readErr := fs.ReadFile(bundle, name)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			return fmt.Errorf("the embedded %s hashes to %s and its provenance says %s", name, got, want)
		}
		return nil
	})
	if err != nil {
		return notFound("the embedded APMap bundle does not match its provenance: %v", err)
	}
	for name := range named {
		if !present[name] {
			return notFound("the embedded APMap bundle's provenance names %s, which it does not carry", name)
		}
	}
	return nil
}
