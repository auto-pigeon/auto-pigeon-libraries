package apmap

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Locating the canonical contract package from inside `package apmap`. apmaptest does this
// for every other package, but these tests are IN this package, so importing it would be a cycle.

// contractPackageRoot is the canonical APMap contract package, three directories up from this one.
func contractPackageRoot() string {
	return filepath.Join("..", "..", "..", "ts", "apmap-schema")
}

// deprecatedCorpusRoot is the quarantined 1.0 material: frozen evidence and rejection fixtures.
func deprecatedCorpusRoot() string {
	return filepath.Join(contractPackageRoot(), "deprecated", "1.0")
}

// currentSchemaDir is the directory holding the one current schema.
func currentSchemaDir() string { return filepath.Join(contractPackageRoot(), "schema") }

// TestMain records the canonical contract as the process default, which nothing in the library does.
func TestMain(m *testing.M) {
	contract, err := LoadContract(currentSchemaDir())
	if err != nil {
		panic("apmap tests: " + err.Error())
	}
	SetContract(contract)
	os.Exit(m.Run())
}

// migrateHeader promotes a published document to the current contract, so every byte the promotion
// does not touch survives to be compared.
//
// The published corpora were written when 1.0 was current, and this build reads, writes and
// validates the current version only. Promoting in memory is exactly what a migration tool does,
// and it is the whole of the difference — which is what makes the byte-for-byte round-trip
// comparisons still meaningful. The files on disk are never modified.
//
// It stopped being only a header when APMap 1.2 arrived. 1.2 requires `groups` of every writer, so
// a promoted document also grows the empty array the encoder now always emits. That is a
// STRUCTURAL change, not a textual one, so the promotion is done on the decoded document and
// re-encoded canonically: `groups` has a defined position in the canonical order, and a regex that
// guessed where to splice it would be asserting the encoder's formatting rather than its content.
func migrateHeader(text string) string {
	current := CurrentVersion()
	text = versionField.ReplaceAllString(text, `${1}"`+current+`"`)
	text = schemaURL.ReplaceAllString(text, `${1}`+current+`${2}`)

	document, err := Decode(text, "migrateHeader")
	if err != nil {
		panic("migrateHeader: the published document does not decode: " + err.Error())
	}
	promoted, err := Encode(withEmptyGroups(document))
	if err != nil {
		panic("migrateHeader: the promoted document does not encode: " + err.Error())
	}
	return promoted
}

// withEmptyGroups returns document with `groups: []` in its canonical position — directly after
// `entities` — or unchanged if it already declares groups.
//
// Encode emits members in insertion order (SER-5), so position is not a formatting detail here: it
// is the contract's canonical order, and the only way to place a member into it is to rebuild the
// object around it.
func withEmptyGroups(document *Object) *Object {
	if document.Has("groups") {
		return document
	}
	rebuilt := NewObject()
	for _, key := range document.Keys() {
		value, _ := document.Get(key)
		rebuilt.Set(key, value)
		if key == "entities" {
			rebuilt.Set("groups", []Value{})
		}
	}
	return rebuilt
}

var (
	versionField = regexp.MustCompile(`("apmap_version"\s*:\s*)"\d+\.\d+"`)
	schemaURL    = regexp.MustCompile(`(schemas/apmap/)\d+\.\d+(/)`)
)
