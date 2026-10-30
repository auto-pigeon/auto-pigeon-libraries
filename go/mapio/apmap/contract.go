package apmap

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// THE APMAP CONTRACT BUNDLE.
//
//	READ      current + every supported legacy format
//	WRITE     current only
//	VALIDATE  with the schema matching the document being read
//
// Readers keep working across versions on purpose: generated corpora written years ago stay readable
// without regenerating anything to change a format header.
//
// One schema file directly under the configured directory is the CURRENT contract and the only
// format anything here writes; each apmap-*.schema.json under deprecated/ is a supported LEGACY
// READ contract. Nothing in this repository writes an APMap version down: every value is derived at
// load from a FILENAME, so promoting a version is a rename plus a move in ts/apmap-schema and a
// regenerated embedded/ snapshot, and no code change here.
//
// Depth is the mechanism. Current discovery reads DIRECT children only, so a deprecated schema can
// never become the writer's contract; a reader enumerates deprecated/ deliberately. "Deprecated"
// means not current and never written — not unreadable.
//
// A contract is a value. ParseContract builds one from any fs.FS and EmbeddedContract returns the one
// compiled into this package, so a standalone program needs no checkout, no environment variable and
// no data root. Where a contract comes from is the caller's business: this package reads no
// environment, opens no directory and searches for no sibling checkout.
//
// The process-wide default below (SetContract, CurrentContract) exists for package-level functions
// a long-running service calls many times. It is unset until a caller sets it and nothing here sets
// it. The root mapio package never reads it: every conversion names its contract.

// schemaPattern matches a schema file in either half of the bundle. WHICH half it sits in decides
// whether it is the writer's contract or a legacy read contract.
var schemaPattern = regexp.MustCompile(`^apmap-(\d+\.\d+)\.schema\.json$`)

// currentSchemaPattern is the same expression, named for the call site that scans direct children.
var currentSchemaPattern = schemaPattern

// deprecatedSchemaDir is the subdirectory holding supported legacy READ contracts.
const deprecatedSchemaDir = "deprecated"

// LoadedSchema is one contract: its version, where it came from, and the parsed schema.
type LoadedSchema struct {
	Version string
	Path    string
	Schema  *Object
}

// Contract is the loaded bundle.
//
// Version/Path/Schema are the CURRENT contract — the writer's, and the only one this build
// produces. Readable is every contract a document may be validated against, keyed by version, and
// always includes the current one: a build that could write a document it could not read back would
// be a contract nobody could use.
//
// Source says where the bundle came from, in the words of whoever loaded it. The embedded bundle
// says "embedded (apmap-schema <version>)".
type Contract struct {
	Version  string
	Path     string
	Schema   *Object
	Readable map[string]*LoadedSchema
	Source   string
}

var currentContract struct {
	sync.RWMutex
	value *Contract
}

// ParseContract discovers and parses a bundle rooted at fsys WITHOUT making it the process's default.
//
// label names the root in every message and in each schema's Path. The caller sets Contract.Source:
// it alone knows where fsys came from.
func ParseContract(fsys fs.FS, label string) (*Contract, error) {
	return loadBundle(fsys, label)
}

// SetContract records the process's default contract, which the package-level functions below read.
// Nil clears it. It is a default for callers that want one and never a side effect of a load.
func SetContract(contract *Contract) {
	currentContract.Lock()
	currentContract.value = contract
	currentContract.Unlock()
}

// loadBundle discovers and parses a bundle rooted at fsys. label names the root in every message
// and in each schema's Path. It never touches the process's contract.
func loadBundle(fsys fs.FS, label string) (*Contract, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, &SchemaNotFoundError{
			Message: fmt.Sprintf("the APMap schema directory %s cannot be read: %v", label, err),
			Cause:   err,
		}
	}

	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() && currentSchemaPattern.MatchString(entry.Name()) {
			matches = append(matches, entry.Name())
		}
	}
	sort.Strings(matches)
	if len(matches) != 1 {
		// Both are named when there are two: "there are two" without saying which is a puzzle.
		return nil, notFound(
			"expected exactly one current APMap schema in %s, found %d%s;\n"+
				"  exactly one file is the contract, and a second is an ambiguity nothing can resolve",
			label, len(matches), joined(matches))
	}

	current, err := readSchemaFile(fsys, label, matches[0])
	if err != nil {
		return nil, err
	}
	readable := map[string]*LoadedSchema{current.Version: current}

	// The legacy half. A fault here is fatal exactly as a fault in the current half is: this
	// directory's contents are a capability claim — this build says it can read 1.0 — and a claim
	// discovered to be false when a job reaches a 1.0 prefab is worse than one never made.
	legacyEntries, err := fs.ReadDir(fsys, deprecatedSchemaDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, &SchemaNotFoundError{
			Message: fmt.Sprintf("the deprecated APMap schema directory in %s cannot be read: %v", label, err),
			Cause:   err,
		}
	}
	var legacyNames []string
	for _, entry := range legacyEntries {
		if !entry.IsDir() && schemaPattern.MatchString(entry.Name()) {
			legacyNames = append(legacyNames, entry.Name())
		}
	}
	sort.Strings(legacyNames)
	for _, name := range legacyNames {
		legacy, err := readSchemaFile(fsys, label, path.Join(deprecatedSchemaDir, name))
		if err != nil {
			return nil, err
		}
		if existing, clash := readable[legacy.Version]; clash {
			return nil, notFound(
				"APMap %s is declared twice in %s (%s and %s/%s);\n"+
					"  a version that resolves to two contracts resolves to neither",
				legacy.Version, label, filepath.Base(existing.Path), deprecatedSchemaDir, name)
		}
		readable[legacy.Version] = legacy
	}

	return &Contract{Version: current.Version, Path: current.Path, Schema: current.Schema, Readable: readable}, nil
}

// readSchemaFile parses one schema file and checks it can express the version its filename carries.
func readSchemaFile(fsys fs.FS, label, name string) (*LoadedSchema, error) {
	full := filepath.Join(label, filepath.FromSlash(name))
	version := schemaPattern.FindStringSubmatch(path.Base(name))[1]

	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, &SchemaNotFoundError{
			Message: fmt.Sprintf("cannot read the APMap schema at %s: %v", full, err),
			Cause:   err,
		}
	}
	schema, err := ParseSchema(full, data)
	if err != nil {
		return nil, err
	}

	// The filename says one thing; the document must be able to say it too. A file named apmap-2.0
	// whose contract cannot express 2.0 is a packaging error, and shipping it would make every
	// document fail for a reason nobody would look for in a filename.
	if !schemaAccepts(schema, version) {
		return nil, notFound(
			"%s is named for APMap %s, but its apmap_version contract cannot express it", path.Base(name), version)
	}
	return &LoadedSchema{Version: version, Path: full, Schema: schema}, nil
}

// CurrentContract is the process's default contract, or an error saying none was set.
//
// Never loads lazily: reaching here without a contract means the process started without one, and
// "discover the schema when somebody first validates something" is precisely the behaviour that
// let a deployment look healthy until the first job ran.
func CurrentContract() (*Contract, error) {
	currentContract.RLock()
	defer currentContract.RUnlock()
	if currentContract.value == nil {
		return nil, notFound(
			"the APMap contract was not loaded at startup; this is a startup fault, not a missing feature")
	}
	return currentContract.value, nil
}

// CurrentVersion is the APMap version this build reads, writes and validates.
//
// Panics when no contract is loaded, deliberately: the alternative is returning "" and writing it
// into a document's apmap_version, which would produce a file nothing can read.
func CurrentVersion() string {
	contract, err := CurrentContract()
	if err != nil {
		panic("apmap: " + err.Error())
	}
	return contract.Version
}

// CurrentVersionOrUnknown is CurrentVersion for message-building paths, including the ones that
// run precisely BECAUSE no contract is loaded. Panicking while composing "there is no contract"
// would replace a clear diagnostic with a stack trace.
func CurrentVersionOrUnknown() string {
	contract, err := CurrentContract()
	if err != nil {
		return "(unknown: no contract loaded)"
	}
	return contract.Version
}

// IsCurrentVersion reports whether version is the one this build WRITES.
//
// Never use it to decide whether a document may be READ — IsReadableVersion answers that, and the
// two stopped being the same question when backward reads landed.
func IsCurrentVersion(version string) bool {
	contract, err := CurrentContract()
	return err == nil && contract.IsCurrent(version)
}

// ReadableVersions is every version this build can read, sorted. Derived, never maintained.
func ReadableVersions() []string {
	contract, err := CurrentContract()
	if err != nil {
		return nil
	}
	return contract.ReadableVersions()
}

// IsReadableVersion reports whether the loaded bundle holds a contract for version.
func IsReadableVersion(version string) bool {
	contract, err := CurrentContract()
	return err == nil && contract.IsReadable(version)
}

// SchemaForVersion is the schema a document declaring version is measured against.
//
// Version-matched, deliberately. Validating a 1.0 document against the current schema would be a
// different check than the one 1.0 promised its producers — the current contract accepts strictly
// more — so a mislabelled document would pass on members 1.0 never had an opinion about.
func SchemaForVersion(version string) (*Object, error) {
	contract, err := CurrentContract()
	if err != nil {
		return nil, err
	}
	return contract.SchemaForVersion(version)
}

// DeclaredVersion is the version a document names as its own contract, or "" when it names none.
//
// Exported for the message-building sites. A refusal that reads "x.apmap is not a valid APMap 1.2
// document" about a document declaring 1.0 sends the reader looking for the wrong problem — that
// exact sentence is how the corpus-pinning bug was first reported — so a message about a document
// names the contract the document was actually measured against.
func DeclaredVersion(document Value) string { return declaredVersion(document) }

// SchemaForDocument is THE DEPLOYMENT'S contract, resolved for THIS document.
//
// The one function every caller that means "measure this against what this deployment speaks"
// should use, and the reason it exists is that the obvious shape is wrong. Resolving the contract
// ONCE for a batch and reusing it reads like an optimization; it silently converts
// version-matched validation into current-only validation, because a pinned schema is by
// definition not the one the next document declares.
//
// That is not hypothetical. Six call sites hoisted `LoadSchema("")` out of their loops, and while
// every version was purely additive-optional over the last it made no observable difference. APMap
// 1.2 requires `groups`, and the whole set began reporting every stored 1.0 and 1.1 document as
// invalid for a member those contracts had no word for — `dm2.apmap is not a valid APMap 1.2
// document: $: required member "groups" is missing`, on a document that is a perfectly valid 1.0.
//
// So the gate and the schema choice are ONE decision and are made together here. A version this
// bundle does not hold is refused (SchemaForVersion's VersionError, which is what the gate
// returned); a version it holds resolves to that version's own frozen contract. Resolution is
// cheap — the contract is already loaded and this is a map lookup — so there is nothing left for a
// caller to optimize by hoisting.
//
// A caller that supplied its own schema or an explicit path never reaches here: there it has said
// which contract it means, and this is not the deployment's question.
func SchemaForDocument(document Value) (*Object, error) {
	declared := declaredVersion(document)
	if declared == "" {
		return nil, &VersionError{
			Message: "the document declares no apmap_version, so it names no contract to be read against"}
	}
	return SchemaForVersion(declared)
}

// GateDeclaredVersion refuses a document whose declared version the loaded bundle does not hold.
//
// THE VERSION GATE, for callers that validate against the DEPLOYMENT'S contract directly rather
// than through Validate. There are several — the CLI, two HTTP handlers, the converter, inspect —
// and each one that forgets it is a path where an unknown format is read against a schema chosen to
// suit it. What the gate asks changed with backward reads: membership of the bundle, not equality
// with the current version. What it protects did not.
//
// Callers supplying their OWN schema skip it: there the caller has said which contract it means.
func GateDeclaredVersion(document Value) error {
	declared := declaredVersion(document)
	if declared == "" {
		return &VersionError{Message: "the document declares no apmap_version, so it names no contract to be read against"}
	}
	if IsReadableVersion(declared) {
		return nil
	}
	return &VersionError{Message: UnsupportedVersionMessage(declared)}
}

// UnsupportedVersionMessage is the one wording for a refused document, matching what AUP, AUB and
// the collaboration service say, so a person meeting it in four places reads one sentence.
//
// It names the whole capability rather than only the current version, because "this build reads
// 1.0, 1.1" is what tells someone holding a 0.9 file whether their problem is age or a typo.
func UnsupportedVersionMessage(version string) string {
	readable := ReadableVersions()
	current := "the current version"
	if contract, err := CurrentContract(); err == nil {
		current = contract.Version
	}
	if len(readable) == 0 {
		return fmt.Sprintf(
			"APMap %s is not a supported APMap version; no APMap contract is loaded. "+
				"This is a startup fault, not a document fault.", version)
	}
	return fmt.Sprintf(
		"APMap %s is not a supported APMap version; this build reads %s and writes %s. "+
			"Migrate the document to a supported format.", version, strings.Join(readable, ", "), current)
}

// DeprecatedVersionMessage is the former name, kept for callers that mean "too old to read".
func DeprecatedVersionMessage(version string) string { return UnsupportedVersionMessage(version) }

// resolveContract is contract, or the process's default when contract is nil. Every operation with
// an explicit contract option funnels through here, so nil has one meaning everywhere.
func resolveContract(contract *Contract) (*Contract, error) {
	if contract != nil {
		return contract, nil
	}
	return CurrentContract()
}

// IsCurrent reports whether version is the one this contract WRITES.
func (c *Contract) IsCurrent(version string) bool { return version == c.Version }

// IsReadable reports whether this contract holds a schema for version.
func (c *Contract) IsReadable(version string) bool {
	_, ok := c.Readable[version]
	return ok
}

// ReadableVersions is every version this contract can read, sorted. Derived, never maintained.
func (c *Contract) ReadableVersions() []string {
	versions := make([]string, 0, len(c.Readable))
	for version := range c.Readable {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	return versions
}

// SchemaForVersion is the schema a document declaring version is measured against.
func (c *Contract) SchemaForVersion(version string) (*Object, error) {
	loaded, ok := c.Readable[version]
	if !ok {
		return nil, &VersionError{Message: c.UnsupportedVersionMessage(version)}
	}
	return loaded.Schema, nil
}

// SchemaForDocument is SchemaForDocument (the package function) against this contract.
func (c *Contract) SchemaForDocument(document Value) (*Object, error) {
	declared := declaredVersion(document)
	if declared == "" {
		return nil, &VersionError{
			Message: "the document declares no apmap_version, so it names no contract to be read against"}
	}
	return c.SchemaForVersion(declared)
}

// GateDeclaredVersion is GateDeclaredVersion (the package function) against this contract.
func (c *Contract) GateDeclaredVersion(document Value) error {
	declared := declaredVersion(document)
	if declared == "" {
		return &VersionError{Message: "the document declares no apmap_version, so it names no contract to be read against"}
	}
	if c.IsReadable(declared) {
		return nil
	}
	return &VersionError{Message: c.UnsupportedVersionMessage(declared)}
}

// UnsupportedVersionMessage is UnsupportedVersionMessage (the package function) against this contract.
func (c *Contract) UnsupportedVersionMessage(version string) string {
	return fmt.Sprintf(
		"APMap %s is not a supported APMap version; this build reads %s and writes %s. "+
			"Migrate the document to a supported format.", version, strings.Join(c.ReadableVersions(), ", "), c.Version)
}

// schemaAccepts reports whether the schema's apmap_version contract can express version.
func schemaAccepts(schema *Object, version string) bool {
	properties, ok := schema.Get("properties")
	if !ok {
		return false
	}
	object, ok := properties.(*Object)
	if !ok {
		return false
	}
	field, ok := object.Get("apmap_version")
	if !ok {
		return false
	}
	contract, ok := field.(*Object)
	if !ok {
		return false
	}
	if constant, ok := contract.Get("const"); ok {
		text, ok := constant.(string)
		return ok && text == version
	}
	enum, ok := contract.Get("enum")
	if !ok {
		return false
	}
	values, ok := enum.([]Value)
	if !ok {
		return false
	}
	for _, value := range values {
		if text, ok := value.(string); ok && text == version {
			return true
		}
	}
	return false
}

func joined(matches []string) string {
	if len(matches) == 0 {
		return ""
	}
	out := ": "
	for index, match := range matches {
		if index > 0 {
			out += ", "
		}
		out += match
	}
	return out
}
