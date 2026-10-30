// Command schemasync writes go/mapio/apmap/embedded/schema: the generated snapshot of the canonical
// APMap contract in ts/apmap-schema/schema, because go:embed cannot reach a parent directory.
//
// The canonical bundle is the only one anybody edits. This program rebuilds the snapshot from
// scratch (an incremental copy would leave a retired schema behind as a second "current") and writes
// a provenance manifest of source paths and digests. Its output is a pure function of the canonical
// files: it names no repository commit, because a file cannot carry the hash of the commit holding it.
//
//	go run ./go/mapio/internal/schemasync          # from the repository root
//	go run ./go/mapio/internal/schemasync --check  # fail, changing nothing, if the snapshot has drifted
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
)

const (
	packageDir  = "ts/apmap-schema"
	sourceDir   = packageDir + "/schema"
	snapshotDir = "go/mapio/apmap/embedded/schema"
	marker      = "GENERATED from " + sourceDir + " by go/mapio/internal/schemasync; edit the source, never this copy"
)

var schemaName = regexp.MustCompile(`^apmap-(\d+\.\d+)\.schema\.json$`)

func main() {
	check := flag.Bool("check", false, "fail if the snapshot differs from the canonical schemas, changing nothing")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintf(os.Stderr, "schemasync: %v\n", err)
		os.Exit(1)
	}
}

func run(check bool) error {
	files, err := snapshot()
	if err != nil {
		return err
	}
	if check {
		return compare(files)
	}
	if err = os.RemoveAll(snapshotDir); err != nil {
		return err
	}
	for name, data := range files {
		target := filepath.Join(snapshotDir, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err = os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %d files to %s\n", len(files), snapshotDir)
	return nil
}

// snapshot is the exact set of files the embedded directory must hold.
func snapshot() (map[string][]byte, error) {
	// The same loader every consumer uses: a bundle it would refuse is refused here.
	contract, err := apmap.ParseContract(os.DirFS(sourceDir), sourceDir)
	if err != nil {
		return nil, fmt.Errorf("the canonical bundle does not load: %w", err)
	}
	version, err := packageVersion()
	if err != nil {
		return nil, err
	}

	files := map[string][]byte{}
	entry := func(version, file string) (apmap.ProvenanceEntry, error) {
		data, readErr := os.ReadFile(filepath.Join(sourceDir, filepath.FromSlash(file)))
		if readErr != nil {
			return apmap.ProvenanceEntry{}, readErr
		}
		files[file] = data
		sum := sha256.Sum256(data)
		return apmap.ProvenanceEntry{Version: version, File: file, SHA256: hex.EncodeToString(sum[:])}, nil
	}

	current, err := entry(contract.Version, "apmap-"+contract.Version+".schema.json")
	if err != nil {
		return nil, err
	}
	names, err := os.ReadDir(filepath.Join(sourceDir, "deprecated"))
	if err != nil {
		return nil, err
	}
	legacy := []apmap.ProvenanceEntry{}
	for _, name := range names {
		if match := schemaName.FindStringSubmatch(name.Name()); match != nil && !name.IsDir() {
			loaded, entryErr := entry(match[1], "deprecated/"+name.Name())
			if entryErr != nil {
				return nil, entryErr
			}
			legacy = append(legacy, loaded)
		}
	}
	sort.Slice(legacy, func(i, j int) bool { return legacy[i].File < legacy[j].File })

	encoded, err := json.MarshalIndent(apmap.Provenance{
		Generated: marker, SourcePackage: packageDir, SourceVersion: version, Current: current, Legacy: legacy,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	files[apmap.ProvenanceFile] = append(encoded, '\n')
	return files, nil
}

func packageVersion() (string, error) {
	data, err := os.ReadFile(filepath.Join(packageDir, "package.json"))
	if err != nil {
		return "", err
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return "", err
	}
	return manifest.Version, nil
}

func compare(want map[string][]byte) error {
	seen := map[string]bool{}
	err := filepath.WalkDir(snapshotDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, relErr := filepath.Rel(snapshotDir, path)
		if relErr != nil {
			return relErr
		}
		name := filepath.ToSlash(relative)
		seen[name] = true
		expected, ok := want[name]
		if !ok {
			return fmt.Errorf("%s is in the snapshot and not in %s", name, sourceDir)
		}
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(got, expected) {
			return fmt.Errorf("%s differs from %s; run go run ./go/mapio/internal/schemasync", name, sourceDir)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for name := range want {
		if !seen[name] {
			return fmt.Errorf("%s is in %s and missing from the snapshot", name, sourceDir)
		}
	}
	fmt.Printf("%s matches %s\n", snapshotDir, sourceDir)
	return nil
}
