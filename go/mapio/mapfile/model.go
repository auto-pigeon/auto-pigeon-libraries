// Package mapfile is the Go `.map` kernel: the AST, the parser, the
// deterministic writer, and the geometry primitives the generators build boxes
// from.
//
// # Immutability
//
// The AST is immutable, and later subsystems (canonicalization, the patch
// engine) depend on that: a parsed document is never mutated in place, only
// rebuilt. Go has no frozen-struct equivalent and no way to enforce this at
// compile time, so the package compensates in three ways:
//
//   - every field is unexported and reachable only through a constructor and
//     value-returning accessors, so no caller outside this package can assign
//     to one;
//   - the composite types are used as values, not pointers, so assigning or
//     passing an Entity copies it (the sole exception is *MapFile, the document
//     handle threaded through Parse/Write, which is never mutated after
//     construction);
//   - every slice crossing the package boundary is defensively copied, in the
//     constructor on the way in and in the accessor on the way out, so no
//     caller can reach through a shared backing array into a document's
//     interior. This is the one thing Python's frozen dataclass gave for free
//     (its fields are tuples) that Go has to pay for explicitly.
//
// What Go still cannot prevent: a caller inside this package assigning to an
// unexported field. That is a code-review obligation here, not a compiler
// guarantee.
package mapfile

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Number is the numeric type of every coordinate, offset, scale, and face-tail
// value in the AST.
//
// An integer and an integral float are not distinguished: FormatNumber writes
// an integral value in integer spelling ("1.0" is written as "1"), so the
// distinction is invisible to output and to every round-trip test. One float64
// is used throughout.
//
// float64 is exact only to 2^53, so a `.map` coordinate beyond ~9.0e15 rounds.
// Real map geometry is bounded by ±32768 (Quake's world limits), so this is
// documented rather than defended against.
type Number = float64

// TextureMode selects the face dialect: a classic Quake face carries scalar
// texture offsets, a Valve 220 face carries bracketed texture axes.
type TextureMode string

const (
	ModeClassic  TextureMode = "classic"
	ModeValve220 TextureMode = "valve220"
)

// KeyValue is one quoted entity property. Unknown keys are preserved verbatim
// (AGENTS.md "Entity Doctrine"), so nothing here interprets or normalizes key.
type KeyValue struct {
	key   string
	value string
}

func NewKeyValue(key, value string) KeyValue { return KeyValue{key: key, value: value} }

func (kv KeyValue) Key() string   { return kv.key }
func (kv KeyValue) Value() string { return kv.value }

func (kv KeyValue) isEntityItem() {}

// Vec3 is one face point.
type Vec3 struct {
	x, y, z Number
}

func NewVec3(x, y, z Number) Vec3 { return Vec3{x: x, y: y, z: z} }

func (v Vec3) X() Number { return v.x }
func (v Vec3) Y() Number { return v.y }
func (v Vec3) Z() Number { return v.z }

// Line renders the point as `( x y z )`.
func (v Vec3) Line() string {
	return "( " + FormatNumber(v.x) + " " + FormatNumber(v.y) + " " + FormatNumber(v.z) + " )"
}

// TextureAxis is one bracketed Valve 220 texture axis.
type TextureAxis struct {
	x, y, z, offset Number
}

func NewTextureAxis(x, y, z, offset Number) TextureAxis {
	return TextureAxis{x: x, y: y, z: z, offset: offset}
}

func (a TextureAxis) X() Number      { return a.x }
func (a TextureAxis) Y() Number      { return a.y }
func (a TextureAxis) Z() Number      { return a.z }
func (a TextureAxis) Offset() Number { return a.offset }

// Line renders the axis as `[ x y z offset ]`.
func (a TextureAxis) Line() string {
	return "[ " + FormatNumber(a.x) + " " + FormatNumber(a.y) + " " +
		FormatNumber(a.z) + " " + FormatNumber(a.offset) + " ]"
}

// Face is one brush face in either dialect.
//
// Face is a single struct carrying both dialects' fields (x/y offsets and u/v
// axes), reachable only through NewClassicFace/NewValve220Face, which makes the
// two inconsistent combinations (a Valve 220 face without axes, a classic face
// without offsets) unrepresentable. No information is lost — both dialects'
// fields are still carried, and a Valve 220 face has 0 offsets — but Line needs
// no error return because an inconsistent face cannot be built.
type Face struct {
	p1, p2, p3  Vec3
	texture     string
	xOffset     Number
	yOffset     Number
	rotation    Number
	xScale      Number
	yScale      Number
	textureMode TextureMode
	uAxis       *TextureAxis
	vAxis       *TextureAxis
	extraValues []Number
}

// NewClassicFace builds a classic Quake face. extraValues carries the trailing
// Quake 2 content/surface/value fields when present; pass nil when absent.
func NewClassicFace(
	p1, p2, p3 Vec3,
	texture string,
	xOffset, yOffset, rotation, xScale, yScale Number,
	extraValues []Number,
) Face {
	return Face{
		p1: p1, p2: p2, p3: p3,
		texture:     texture,
		xOffset:     xOffset,
		yOffset:     yOffset,
		rotation:    rotation,
		xScale:      xScale,
		yScale:      yScale,
		textureMode: ModeClassic,
		extraValues: copyNumbers(extraValues),
	}
}

// NewValve220Face builds a Valve 220 face. Its xOffset/yOffset stay 0.
func NewValve220Face(
	p1, p2, p3 Vec3,
	texture string,
	uAxis, vAxis TextureAxis,
	rotation, xScale, yScale Number,
	extraValues []Number,
) Face {
	return Face{
		p1: p1, p2: p2, p3: p3,
		texture:     texture,
		rotation:    rotation,
		xScale:      xScale,
		yScale:      yScale,
		textureMode: ModeValve220,
		uAxis:       &uAxis,
		vAxis:       &vAxis,
		extraValues: copyNumbers(extraValues),
	}
}

func (f Face) P1() Vec3                 { return f.p1 }
func (f Face) P2() Vec3                 { return f.p2 }
func (f Face) P3() Vec3                 { return f.p3 }
func (f Face) Texture() string          { return f.texture }
func (f Face) XOffset() Number          { return f.xOffset }
func (f Face) YOffset() Number          { return f.yOffset }
func (f Face) Rotation() Number         { return f.rotation }
func (f Face) XScale() Number           { return f.xScale }
func (f Face) YScale() Number           { return f.yScale }
func (f Face) TextureMode() TextureMode { return f.textureMode }

// Points returns the face's three plane points.
func (f Face) Points() [3]Vec3 { return [3]Vec3{f.p1, f.p2, f.p3} }

// UAxis reports the U texture axis and whether the face has one. Only a Valve
// 220 face does.
func (f Face) UAxis() (TextureAxis, bool) {
	if f.uAxis == nil {
		return TextureAxis{}, false
	}
	return *f.uAxis, true
}

// VAxis reports the V texture axis and whether the face has one.
func (f Face) VAxis() (TextureAxis, bool) {
	if f.vAxis == nil {
		return TextureAxis{}, false
	}
	return *f.vAxis, true
}

// ExtraValues returns a copy of the trailing Quake 2 face values.
func (f Face) ExtraValues() []Number { return copyNumbers(f.extraValues) }

// Line renders the face as one `.map` face line in its own dialect.
func (f Face) Line() string {
	var b strings.Builder
	b.WriteString(f.p1.Line())
	b.WriteByte(' ')
	b.WriteString(f.p2.Line())
	b.WriteByte(' ')
	b.WriteString(f.p3.Line())
	b.WriteByte(' ')
	b.WriteString(f.texture)
	b.WriteByte(' ')

	if f.textureMode == ModeValve220 {
		b.WriteString(f.uAxis.Line())
		b.WriteByte(' ')
		b.WriteString(f.vAxis.Line())
	} else {
		b.WriteString(FormatNumber(f.xOffset))
		b.WriteByte(' ')
		b.WriteString(FormatNumber(f.yOffset))
	}

	b.WriteByte(' ')
	b.WriteString(FormatNumber(f.rotation))
	b.WriteByte(' ')
	b.WriteString(FormatNumber(f.xScale))
	b.WriteByte(' ')
	b.WriteString(FormatNumber(f.yScale))

	for _, value := range f.extraValues {
		b.WriteByte(' ')
		b.WriteString(FormatNumber(value))
	}
	return b.String()
}

// Equal reports whether two faces are the same value, standing in for Python's
// structural dataclass __eq__.
func (f Face) Equal(other Face) bool {
	if f.p1 != other.p1 || f.p2 != other.p2 || f.p3 != other.p3 {
		return false
	}
	if f.texture != other.texture || f.textureMode != other.textureMode {
		return false
	}
	if f.xOffset != other.xOffset || f.yOffset != other.yOffset {
		return false
	}
	if f.rotation != other.rotation || f.xScale != other.xScale || f.yScale != other.yScale {
		return false
	}
	if !equalAxis(f.uAxis, other.uAxis) || !equalAxis(f.vAxis, other.vAxis) {
		return false
	}
	if len(f.extraValues) != len(other.extraValues) {
		return false
	}
	for i, value := range f.extraValues {
		if value != other.extraValues[i] {
			return false
		}
	}
	return true
}

func equalAxis(a, b *TextureAxis) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// EntityItem is a KeyValue or a Brush. The union
// exists so that an entity preserves the *source order* of its properties and
// brushes, which is doctrine, not incidental.
type EntityItem interface {
	isEntityItem()
}

// Brush is an ordered set of faces.
type Brush struct {
	faces []Face
}

func NewBrush(faces []Face) Brush {
	return Brush{faces: copyFaces(faces)}
}

// Faces returns a copy of the brush's faces in source order.
func (b Brush) Faces() []Face { return copyFaces(b.faces) }

// FaceCount avoids the copy Faces makes when only the count is wanted.
func (b Brush) FaceCount() int { return len(b.faces) }

// Face returns the face at index i in source order.
func (b Brush) Face(i int) Face { return b.faces[i] }

func (b Brush) isEntityItem() {}

func (b Brush) Equal(other Brush) bool {
	if len(b.faces) != len(other.faces) {
		return false
	}
	for i, face := range b.faces {
		if !face.Equal(other.faces[i]) {
			return false
		}
	}
	return true
}

// Entity is one `{ ... }` block: an ordered mix of properties and brushes.
type Entity struct {
	items []EntityItem
}

func NewEntity(items []EntityItem) Entity {
	return Entity{items: copyItems(items)}
}

// Items returns a copy of the entity's contents in source order.
func (e Entity) Items() []EntityItem { return copyItems(e.items) }

// Properties returns the entity's key/values in source order.
func (e Entity) Properties() []KeyValue {
	var out []KeyValue
	for _, item := range e.items {
		if kv, ok := item.(KeyValue); ok {
			out = append(out, kv)
		}
	}
	return out
}

// Brushes returns the entity's brushes in source order.
func (e Entity) Brushes() []Brush {
	var out []Brush
	for _, item := range e.items {
		if brush, ok := item.(Brush); ok {
			out = append(out, brush)
		}
	}
	return out
}

// BrushCount avoids the copy Brushes makes when only the count is wanted.
func (e Entity) BrushCount() int {
	count := 0
	for _, item := range e.items {
		if _, ok := item.(Brush); ok {
			count++
		}
	}
	return count
}

// Get returns the first value for key, in source order, and whether one was
// found.
func (e Entity) Get(key string) (string, bool) {
	for _, item := range e.items {
		if kv, ok := item.(KeyValue); ok && kv.key == key {
			return kv.value, true
		}
	}
	return "", false
}

// GetOr is Get with an explicit default.
func (e Entity) GetOr(key, fallback string) string {
	if value, ok := e.Get(key); ok {
		return value
	}
	return fallback
}

func (e Entity) Equal(other Entity) bool {
	if len(e.items) != len(other.items) {
		return false
	}
	for i, item := range e.items {
		switch left := item.(type) {
		case KeyValue:
			right, ok := other.items[i].(KeyValue)
			if !ok || left != right {
				return false
			}
		case Brush:
			right, ok := other.items[i].(Brush)
			if !ok || !left.Equal(right) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ParseDiagnosticCode identifies a non-fatal parser observation.
type ParseDiagnosticCode string

const (
	// DiagnosticEmptyBrushBlockSkipped reports a syntactically complete brush
	// block with no faces. Worldcraft writes these for some degenerate solids.
	DiagnosticEmptyBrushBlockSkipped ParseDiagnosticCode = "empty_brush_block_skipped"
	maxParseDiagnostics                                  = 64
)

// ParseDiagnostic is one retained source location for a non-fatal parser
// observation. Counts remain exact even when the bounded detail list is full.
type ParseDiagnostic struct {
	Code    ParseDiagnosticCode
	Source  string
	Line    int
	Column  int
	Message string
}

// MapFile is a whole document: entities in source order. Parse diagnostics are
// provenance, not semantic content, so Equal intentionally ignores them.
type MapFile struct {
	entities                []Entity
	parseDiagnostics        []ParseDiagnostic
	emptyBrushBlocksSkipped int
}

func NewMapFile(entities []Entity) *MapFile {
	return &MapFile{entities: copyEntities(entities)}
}

func newParsedMapFile(entities []Entity, diagnostics []ParseDiagnostic, emptyBrushBlocksSkipped int) *MapFile {
	return &MapFile{
		entities:                copyEntities(entities),
		parseDiagnostics:        append([]ParseDiagnostic(nil), diagnostics...),
		emptyBrushBlocksSkipped: emptyBrushBlocksSkipped,
	}
}

// Entities returns a copy of the document's entities in source order.
func (m *MapFile) Entities() []Entity { return copyEntities(m.entities) }

// EntityCount avoids the copy Entities makes when only the count is wanted.
func (m *MapFile) EntityCount() int { return len(m.entities) }

// Entity returns the entity at index i in source order.
func (m *MapFile) Entity(i int) Entity { return m.entities[i] }

// ParseDiagnostics returns a bounded copy of retained non-fatal observations.
func (m *MapFile) ParseDiagnostics() []ParseDiagnostic {
	return append([]ParseDiagnostic(nil), m.parseDiagnostics...)
}

// EmptyBrushBlocksSkipped returns the exact number of complete empty brush
// blocks omitted from the semantic document during parsing.
func (m *MapFile) EmptyBrushBlocksSkipped() int { return m.emptyBrushBlocksSkipped }

// BrushCount is the number of brushes across every entity.
func (m *MapFile) BrushCount() int {
	total := 0
	for _, entity := range m.entities {
		total += entity.BrushCount()
	}
	return total
}

func (m *MapFile) Equal(other *MapFile) bool {
	if m == nil || other == nil {
		return m == other
	}
	if len(m.entities) != len(other.entities) {
		return false
	}
	for i, entity := range m.entities {
		if !entity.Equal(other.entities[i]) {
			return false
		}
	}
	return true
}

// FormatNumber is the single rule every number in a written `.map` goes
// through.
//
// An integral value prints as the integer; anything else prints as Python's `str(float)`, i.e. the shortest
// decimal that round-trips.
//
// Go's strconv with precision -1 produces the same shortest round-trip digits
// as Python, but the two disagree about *notation*, which is why this does not
// simply use the 'g' verb.
//
// This comment previously claimed the thresholds only differ above 1e16, where
// every float64 is integral and takes the integer branch anyway. That was
// wrong, and the error was reachable. Go's 'g' with precision -1 fixes its
// exponent threshold at 6, so it switches to scientific notation for any value
// at or above 1e6, while Python's repr stays in fixed notation until 1e16.
// `FormatNumber(1234567.5)` therefore emitted `1.2345675e+06` where the rule
// requires `1234567.5` — and since the parser does not accept an exponent in a
// coordinate, the result was an unparseable `.map`, breaking the round-trip
// guarantee this writer exists to provide. See the prompt 16 handoff.
//
// pythonRepr below keeps CPython's digits — the shortest that round-trip — and,
// since Q2_002, never its exponent notation: below 1e-4 repr writes `1e-05`,
// which this package's parser refuses and APMap's SER-7 forbids (finding F18).
func FormatNumber(value Number) string {
	if value == math.Trunc(value) && !math.IsInf(value, 0) {
		// Python's str(int(-0.0)) is "0", where Go's FormatFloat would emit
		// "-0"; going through an integer reproduces Python exactly.
		if math.Abs(value) < 1e18 {
			return strconv.FormatInt(int64(value), 10)
		}
		return exactInteger(value)
	}
	return pythonRepr(value)
}

// exactInteger is Python's str(int(value)) for an integral float64 too large
// for an int64.
//
// FormatFloat with 'f' and precision -1 is *not* the same thing: it emits the
// shortest decimal that round-trips back to the same float64, where Python
// prints the float's exact mathematical value. They agree up to 2^53 and
// diverge above it — str(int(1e30)) is 1000000000000000019884624838656, while
// the shortest round-trip is 1000000000000000000000000000000. Both name the
// same float64, but only the exact value is the canonical spelling, and a
// canonical form cannot be "either of these".
//
// math/big is standard library, so this costs no dependency.
func exactInteger(value float64) string {
	integer, _ := new(big.Float).SetFloat64(value).Int(nil)
	return integer.String()
}

// pythonRepr renders a non-integral float64 exactly as CPython's repr does
// (`str(value)`, identical to repr for floats on Python 3), which is the
// spelling FormatNumber uses for such a value.
//
// Precision -1, so the digits are the shortest that round-trip — the same set
// Python chooses. The notation is positional, always (Q2_002): Python's
// scientific branch below 1e-4 wrote a `.map` this package could not read back,
// and its branch at >= 1e16 is unreachable for a non-integral float64.
//
// This function is deliberately duplicated in apmap's serializer,
// which may not import this package. The two are kept in step by
// TestFormatNumberAgreesWithAPMap, which compares them directly.
func pythonRepr(value Number) string {
	// Q2_002: positional always. The scientific branch (exponent < -4) wrote `3.4e-05`, which this
	// package's own parser refuses — a writer emitting a spelling no reader here accepts (F18).
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// ReprString is Python's `repr()` of a string: single quotes by default,
// double quotes when the value contains a single quote and no double one, with
// backslashes, the active quote and control characters escaped.
//
// It exists because AUE's error messages interpolate keys and values in Python
// repr form, which the parser tests pin byte for byte. Go's `%q` is not a
// substitute: it always uses double quotes, so a parse error naming a key would
// read `expected quoted value for entity key "classname"` instead of
// `'classname'`.
func ReprString(value string) string {
	quote := byte('\'')
	if strings.Contains(value, "'") && !strings.Contains(value, `"`) {
		quote = '"'
	}
	var builder strings.Builder
	builder.WriteByte(quote)
	for _, char := range value {
		switch {
		case char == '\\':
			builder.WriteString(`\\`)
		case byte(char) == quote && char < 128:
			builder.WriteByte('\\')
			builder.WriteRune(char)
		case char == '\n':
			builder.WriteString(`\n`)
		case char == '\r':
			builder.WriteString(`\r`)
		case char == '\t':
			builder.WriteString(`\t`)
		case char < 0x20 || char == 0x7f:
			builder.WriteString(fmt.Sprintf(`\x%02x`, char))
		default:
			builder.WriteRune(char)
		}
	}
	builder.WriteByte(quote)
	return builder.String()
}

func copyNumbers(values []Number) []Number {
	if len(values) == 0 {
		// Normalized to nil so that an empty tail and an absent tail compare
		// equal.
		return nil
	}
	out := make([]Number, len(values))
	copy(out, values)
	return out
}

func copyFaces(faces []Face) []Face {
	if len(faces) == 0 {
		return nil
	}
	out := make([]Face, len(faces))
	copy(out, faces)
	return out
}

func copyItems(items []EntityItem) []EntityItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]EntityItem, len(items))
	copy(out, items)
	return out
}

func copyEntities(entities []Entity) []Entity {
	if len(entities) == 0 {
		return nil
	}
	out := make([]Entity, len(entities))
	copy(out, entities)
	return out
}
