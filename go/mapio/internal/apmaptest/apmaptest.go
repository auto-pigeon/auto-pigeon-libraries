// Package apmaptest loads the real APMap contract for this module's tests, and locates the canonical
// contract package.
//
// The package-level apmap functions read a process default that nothing in the library sets, so each
// test package that calls them sets it from its TestMain. Explicit rather than a lazy fallback in
// the apmap package: "discover the contract when somebody first needs it" is exactly the behaviour
// the current-only design forbids.
package apmaptest

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
)

// Load records the canonical contract as the process default, or stops the test binary saying why.
func Load(m *testing.M) int {
	contract, err := apmap.ParseContract(os.DirFS(SchemaDir()), SchemaDir())
	if err != nil {
		panic("apmaptest: " + err.Error())
	}
	apmap.SetContract(contract)
	return m.Run()
}

// PackageRoot is the canonical APMap contract package in this repository.
func PackageRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("apmaptest: cannot locate this source file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "ts", "apmap-schema")
}

// SchemaDir is the directory holding the one current schema.
func SchemaDir() string { return filepath.Join(PackageRoot(), "schema") }

// Vectors is the CURRENT conformance vectors directory.
func Vectors() string { return filepath.Join(PackageRoot(), "test-vectors") }

// Deprecated is the quarantined 1.0 material — historical evidence and rejection fixtures only.
func Deprecated(segments ...string) string {
	return filepath.Join(append([]string{PackageRoot(), "deprecated", "1.0"}, segments...)...)
}

// PromoteToCurrent promotes a published APMap document to the current contract: the declared
// version and `$schema` URL are rewritten, and the structural defaults the current contract
// requires are supplied. Today that is exactly one member, `groups`, which APMap 1.2 requires of
// every writer and which no 1.0 or 1.1 document could carry.
//
// It exists here, once, because several test packages compare AUE's current output against
// documents published under an older contract version. Promoting those bytes before the comparison
// keeps it about the GEOMETRY rather than about which version of the format each targets.
// Everything the promotion does not touch survives byte for byte, so a real divergence still
// fails.
//
// Input that is not a JSON object is returned unchanged: this is a comparison helper, never the
// thing under test, and it must not turn an unrelated fault into a confusing one here.
func PromoteToCurrent(text string) string {
	promoted := versionField.ReplaceAllString(text, "${1}\""+apmap.CurrentVersion()+"\"")
	promoted = schemaURL.ReplaceAllString(promoted, "${1}"+apmap.CurrentVersion()+"${2}")

	document, err := apmap.Decode(promoted, "apmaptest.PromoteToCurrent")
	if err != nil {
		return promoted
	}
	if document.Has("groups") {
		return promoted
	}
	// `groups` sits directly after `entities` in the contract's canonical order, and the encoder
	// emits members in insertion order (SER-5), so the object is rebuilt around it rather than
	// having the member appended.
	rebuilt := apmap.NewObject()
	for _, key := range document.Keys() {
		value, _ := document.Get(key)
		rebuilt.Set(key, value)
		if key == "entities" {
			rebuilt.Set("groups", []apmap.Value{})
		}
	}
	encoded, err := apmap.Encode(rebuilt)
	if err != nil {
		return promoted
	}
	return encoded
}

var (
	versionField = regexp.MustCompile(`("apmap_version"\s*:\s*)"\d+\.\d+"`)
	schemaURL    = regexp.MustCompile(`(schemas/apmap/)\d+\.\d+(/)`)
)
