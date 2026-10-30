package apmap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Deterministic APMap object identity, specified by
// auto-pigeon-libraries' apmap-schema SEMANTICS.md, and §3.5 of the published 1.0
// specification archived there as deprecated/1.0/PUBLISHED-README.md:
//
//	payload = "apmap/1.0" US kind US c1 US ... US cN      (US = U+001F)
//	id      = prefix(kind) + "_" + sha256(payload).hexdigest()[:16]
//
// Two properties make this worth its own file, and both are requirements
// rather than niceties.
//
// **Identity survives editing.** Every digest input is edit-stable: a package
// id, a document role, an object's structural path. Coordinates, bounds and
// source brush positions are recorded on the object as provenance and are
// deliberately never digested, so moving a brush 32 units left leaves its id
// alone. An id derived from geometry would change on every move, and every
// journal entry referencing it would dangle.
//
// **Identity survives regeneration.** SHA-256 rather than a process-seeded
// hash, so re-running an extraction on the same inputs reproduces the same
// bytes. Go's maps.Hash equivalents and Python's hash() are both randomized per
// process; neither would do.

// IdentityNamespace is the payload namespace. Bumping the format version
// changes every derived id, which is correct: a 2.0 document is not the same
// document.
const IdentityNamespace = "apmap/1.0"

// ComponentSeparator is U+001F. It cannot occur in any component the format
// admits, so ("ab", "c") and ("a", "bc") can never digest alike.
const ComponentSeparator = "\x1f"

// DigestLength is how many hex characters of the SHA-256 an id carries.
const DigestLength = 16

// Kind is the object kind an id addresses.
type Kind string

// The object kinds APMap gives identity to: the three 1.0 kinds, and from 1.5 a Quake III patch
// (SEMANTICS.md §3.3, §3.5 — the payload's kind component is "patch").
const (
	KindEntity Kind = "entity"
	KindBrush  Kind = "brush"
	KindFace   Kind = "face"
	KindPatch  Kind = "patch"
)

var kindPrefix = map[Kind]string{
	KindEntity: "ent",
	KindBrush:  "brs",
	KindFace:   "fac",
	KindPatch:  "pat",
}

// DocumentRoles are the roles a document may declare.
var DocumentRoles = []string{"full_map", "prefab_core", "reference_context", "cropped_context"}

// ObjectPathPattern matches e30, e30.b0, e30.b0.f0 and, from 1.5, e30.p0 — an entity's first
// patch. A patch has no sub-objects, so there is no e30.p0.f0.
var ObjectPathPattern = regexp.MustCompile(`^e[0-9]+(\.b[0-9]+(\.f[0-9]+)?|\.p[0-9]+)?$`)

// ObjectPath builds the canonical path addressing an object inside a document.
//
// brush and face are negative when absent — Go has no Optional[int], and a
// pointer-to-int parameter would make every call site allocate to say "no
// brush". The sentinel is checked rather than trusted: a negative index that
// was meant as a real index is rejected by the same branch.
func ObjectPath(entity, brush, face int) (string, error) {
	if entity < 0 {
		return "", &Error{Message: "object path indices must be non-negative"}
	}
	if face >= 0 && brush < 0 {
		return "", &Error{Message: "a face path requires a brush index"}
	}

	path := "e" + strconv.Itoa(entity)
	if brush >= 0 {
		path += ".b" + strconv.Itoa(brush)
	}
	if face >= 0 {
		path += ".f" + strconv.Itoa(face)
	}
	return path, nil
}

// PatchObjectPath is the canonical path of an entity's patch-th patch, e30.p0. Patches are counted
// separately from brushes, each from 0 in content order, so adding a patch never renumbers a brush.
func PatchObjectPath(entity, patch int) (string, error) {
	if entity < 0 || patch < 0 {
		return "", &Error{Message: "object path indices must be non-negative"}
	}
	return "e" + strconv.Itoa(entity) + ".p" + strconv.Itoa(patch), nil
}

// Error is a plain APMap failure, with no problem list and no schema involved.
type Error struct {
	Message string
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return ErrAPMap }

// NewError is a codec error with a formatted message, for a consumer's adapter around this package
// that fails the way the package does.
func NewError(format string, args ...any) error { return newError(format, args...) }

func newError(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// DeriveID is the APMap id for components, per README §3.5.
func DeriveID(kind Kind, components ...string) (string, error) {
	prefix, known := kindPrefix[kind]
	if !known {
		return "", newError("unknown object kind: %q", string(kind))
	}
	if len(components) == 0 {
		return "", newError("a derived id needs at least one identity component")
	}
	for _, component := range components {
		if strings.Contains(component, ComponentSeparator) {
			return "", newError("an identity component cannot contain U+001F")
		}
	}

	parts := make([]string, 0, len(components)+2)
	parts = append(parts, IdentityNamespace, string(kind))
	parts = append(parts, components...)

	digest := sha256.Sum256([]byte(strings.Join(parts, ComponentSeparator)))
	return prefix + "_" + hex.EncodeToString(digest[:])[:DigestLength], nil
}

// PackageIdentity is identity scoped to a published package and a document role.
//
// role is a digest component on purpose. prefab.apmap and context.apmap both
// address an object at e0.b0, but they are not the same object; giving them one
// id would invite a consumer to correlate two unrelated brushes.
type PackageIdentity struct {
	PackageID string
	Role      string
}

// Validate checks the identity. Go has no constructor to run it on, so
// every method calls it and it is exported for a caller that wants to check
// early.
func (p PackageIdentity) Validate() error {
	if p.PackageID == "" {
		return newError("package identity requires a package id")
	}
	for _, role := range DocumentRoles {
		if p.Role == role {
			return nil
		}
	}
	return newError("unknown document role: %q", p.Role)
}

// IDFor is the id for the object at path.
func (p PackageIdentity) IDFor(kind Kind, path string) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return DeriveID(kind, p.PackageID, p.Role, path)
}

// Record is the derived_from provenance record for the object at path.
//
// provenance is recorded verbatim and never reaches the digest. Nil values are
// dropped rather than written as null.
// The result is an *Object rather than a map because member order is meaning
// here — this record is serialized into a document, and SER-5/6 forbid sorting.
func (p PackageIdentity) Record(path string, provenance ...Member) (*Object, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	record := NewObject()
	record.Set("kind", "package")
	record.Set("package_id", p.PackageID)
	record.Set("role", p.Role)
	record.Set("object_path", path)
	addProvenance(record, provenance)
	return record, nil
}

// SourceMapIdentity is identity scoped to a source `.map` file and its content
// hash.
//
// Preferred when the producer knows the exact source object. The hash is a
// digest input, so re-importing a modified source yields different objects —
// which is correct: silently reusing the old ids would assert an equivalence
// nobody checked.
type SourceMapIdentity struct {
	SourceMap    string
	SourceSHA256 string
}

var lowercaseSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate checks the identity's members.
func (s SourceMapIdentity) Validate() error {
	if s.SourceMap == "" {
		return newError("source identity requires a source map name")
	}
	if !lowercaseSHA256.MatchString(s.SourceSHA256) {
		return newError("source identity requires a lowercase hex SHA-256")
	}
	return nil
}

// IDFor is the id for the object at path.
func (s SourceMapIdentity) IDFor(kind Kind, path string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	return DeriveID(kind, s.SourceMap, s.SourceSHA256, path)
}

// Record is the derived_from provenance record for the object at path.
func (s SourceMapIdentity) Record(path string, provenance ...Member) (*Object, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	record := NewObject()
	record.Set("kind", "source_map")
	record.Set("source_map", s.SourceMap)
	record.Set("source_sha256", s.SourceSHA256)
	record.Set("source_path", path)
	addProvenance(record, provenance)
	return record, nil
}

// AuthoredIdentity is identity for objects created by one operation in an edit
// journal.
//
// Replaying the same journal against the same document reproduces every id, so
// a recovered checkpoint and a replayed session agree.
type AuthoredIdentity struct {
	DocumentID  string
	OperationID string
}

// Validate checks the identity's members.
func (a AuthoredIdentity) Validate() error {
	if a.DocumentID == "" || a.OperationID == "" {
		return newError("authored identity requires a document id and an operation id")
	}
	return nil
}

// IDFor is the id for the ordinal-th object the operation created.
func (a AuthoredIdentity) IDFor(kind Kind, ordinal int) (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	if ordinal < 0 {
		return "", newError("an authored ordinal must be non-negative")
	}
	return DeriveID(kind, a.DocumentID, a.OperationID, strconv.Itoa(ordinal))
}

// Record is the derived_from provenance record. Unlike the other two it takes
// no extra provenance.
func (a AuthoredIdentity) Record(ordinal int) (*Object, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	record := NewObject()
	record.Set("kind", "authored")
	record.Set("document_id", a.DocumentID)
	record.Set("operation_id", a.OperationID)
	// A json.Number, not an int: everything in this package's value model is
	// decoded JSON, and a record that came from a document and a record built
	// here must compare and serialize alike.
	record.Set("ordinal", json.Number(strconv.Itoa(ordinal)))
	return record, nil
}

// Member is one extra provenance key/value pair for a Record.
type Member struct {
	Key   string
	Value Value
}

func addProvenance(record *Object, provenance []Member) {
	for _, member := range provenance {
		// A nil value is dropped rather than written as null.
		if member.Value == nil {
			continue
		}
		record.Set(member.Key, member.Value)
	}
}

// ExpectedID is the id a derived_from record mandates, or "" when it mandates
// none.
//
// This is what makes `id_policy: "derived"` checkable rather than a claim
// (rule SEM-10), and it is why semantics.go in part 3 can verify identity
// without trusting the producer. `synthetic` records mandate no identity in
// 1.0, and neither does a malformed one.
//
// The error return is separate from the empty-string case on purpose: "this
// record mandates nothing" and "this record is malformed" are different
// answers, and conflating them would treat a document that legitimately has no
// mandated id like one whose record is broken. Returning ("", nil) for the
// former and ("", err) for the latter keeps SEM-10 able to tell them apart. A
// caller that only wants the id ignores the error, which is what MandatedID does.
func ExpectedID(kind Kind, derivedFrom Value) (string, error) {
	record, ok := derivedFrom.(*Object)
	if !ok {
		return "", nil
	}

	kindName, _ := record.Get("kind")
	name, _ := kindName.(string)

	switch name {
	case "package":
		components, err := stringComponents(record, "package_id", "role", "object_path")
		if err != nil {
			return "", err
		}
		return DeriveID(kind, components...)
	case "source_map":
		components, err := stringComponents(record, "source_map", "source_sha256", "source_path")
		if err != nil {
			return "", err
		}
		return DeriveID(kind, components...)
	case "authored":
		components, err := stringComponents(record, "document_id", "operation_id")
		if err != nil {
			return "", err
		}
		ordinal, err := integerMember(record, "ordinal")
		if err != nil {
			return "", err
		}
		return DeriveID(kind, append(components, strconv.Itoa(ordinal))...)
	}

	// "synthetic", and any kind a later version adds, mandate nothing.
	return "", nil
}

// MandatedID is ExpectedID without the error: a malformed record and a record
// that mandates nothing are both "".
func MandatedID(kind Kind, derivedFrom Value) string {
	id, err := ExpectedID(kind, derivedFrom)
	if err != nil {
		return ""
	}
	return id
}

// stringComponents reads the named members as strings.
//
// A member is stringified whatever its type, so a number under "role" becomes
// its decimal text rather than an error. That is deliberate: the digest a
// document mandates has to be computable the same way by every reader, and
// disagreeing about a wrongly-typed member would make two validators reach
// different verdicts on the same file. A *missing* member is still an error.
func stringComponents(record *Object, names ...string) ([]string, error) {
	components := make([]string, 0, len(names))
	for _, name := range names {
		value, present := record.Get(name)
		if !present {
			return nil, newError("derived_from is missing %q", name)
		}
		components = append(components, stringify(value))
	}
	return components, nil
}

// integerMember reads a member that must be an integer.
func integerMember(record *Object, name string) (int, error) {
	value, present := record.Get(name)
	if !present {
		return 0, newError("derived_from is missing %q", name)
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, newError("derived_from %q is not a number", name)
	}
	parsed, err := strconv.ParseFloat(string(number), 64)
	if err != nil {
		return 0, newError("derived_from %q is not a number", name)
	}
	// Python's int() truncates toward zero for a float and accepts an int
	// exactly; both land here identically.
	return int(parsed), nil
}

// stringify is Python's str() for the decoded value model.
func stringify(value Value) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return string(typed)
	case bool:
		// Python's str(True) is "True". Reproduced rather than corrected: see
		// stringComponents on why every implementation must agree here.
		if typed {
			return "True"
		}
		return "False"
	case nil:
		return "None"
	default:
		return fmt.Sprintf("%v", value)
	}
}
