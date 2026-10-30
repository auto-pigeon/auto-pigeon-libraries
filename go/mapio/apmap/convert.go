package apmap

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
)

// Conversion between APMap and the `.map` geometry model.
//
// mapfile's model has nowhere to keep an entity_id: an Entity is a
// sequence of KeyValue | Brush and nothing else. Converting a document through
// it would therefore lose every APMap identity, extension and piece of
// provenance — unless the conversion is arranged so identity is *re-attached*
// rather than carried.
//
// That is what Rebuild does. ToMapDocument never reorders anything, so an
// object's structural path (e0.b1.f3) is the same before and after the trip.
// Rebuild keys the original document's per-object metadata by that path and
// re-attaches it to the geometry coming back. With unmodified geometry the
// output is byte-identical to the input; when a brush moved, its id, its
// derived_from and its extensions travel with it, because its *path* did not
// change even though its coordinates did.
//
// # The frame is never applied to coordinates
//
// Not anywhere in this file, deliberately. A prefab_local
// document stays in prefab-local numbers and only *declares* its transform back
// to source world, which is what prefab.map already does today. Adding the
// transform here would silently move geometry that every downstream consumer
// expects to find where the document put it.
//
// # This is the first file in apmap with an outside dependency
//
// identity.go and serialize.go were deliberately kept dependency-free within
// this package, and they still are. That constraint was always scoped to those
// two files — a converter between two formats cannot avoid depending on both of
// them — and nothing here reimplements `.map` parsing or writing: Parse, Write
// and FormatNumber are called, never re-derived.

// SchemaURLTemplate is the $schema URL a produced document declares.
const SchemaURLTemplate = "https://auto-pigeon.org/schemas/apmap/%s/apmap.schema.json"

// The four document roles, matching DocumentRoles in identity.go.
const (
	RoleFullMap          = "full_map"
	RolePrefabCore       = "prefab_core"
	RoleReferenceContext = "reference_context"
	RoleCroppedContext   = "cropped_context"
)

// The defaults a document declares when the caller names none.
const (
	DefaultGame    = "quake1"
	DefaultDialect = "quake_standard"
)

// The two coordinate spaces a frame may declare.
const (
	SpaceSourceWorld = "source_world"
	SpacePrefabLocal = "prefab_local"
)

// The two identity policies.
const (
	PolicyDerived = "derived"
	PolicyMinted  = "minted"
)

// Identity is the union of the three identity sources from identity.go.
//
// Go has no sum type, and the three cannot share an interface with an IDFor
// method: AuthoredIdentity's takes an int ordinal where the other two take a
// string path. That difference is the whole point of the ordinal branch below,
// so it is preserved rather than smoothed over with a common signature that
// would make the two cases look interchangeable.
type Identity interface {
	isIdentity()
}

func (PackageIdentity) isIdentity()   {}
func (SourceMapIdentity) isIdentity() {}
func (AuthoredIdentity) isIdentity()  {}

// normalizeIdentity accepts a pointer to any of the three, since a value
// receiver marker method makes *T satisfy Identity as readily as T does.
func normalizeIdentity(identity Identity) Identity {
	switch typed := identity.(type) {
	case *PackageIdentity:
		if typed != nil {
			return *typed
		}
	case *SourceMapIdentity:
		if typed != nil {
			return *typed
		}
	case *AuthoredIdentity:
		if typed != nil {
			return *typed
		}
	}
	return identity
}

// --------------------------------------------------------------------- frames

// Frame is a document's coordinate space and its transform back to source
// world.
//
// The zero value is the source-world frame, which is what makes it usable as an
// omitted field in ConvertOptions the way Python's `frame: Frame = SOURCE_FRAME`
// default argument is.
type Frame struct {
	Space string
	// Translation maps this document's coordinates *into* source world. Nil is
	// the identity transform. It is a pointer rather than a slice so that
	// "absent" and "three zeros" stay distinguishable and a two-element
	// translation is unrepresentable.
	Translation *[3]float64
}

// SourceFrame is the frame of a document already in source-world coordinates.
var SourceFrame = Frame{Space: SpaceSourceWorld}

// PrefabLocalFrame is the frame for a canonicalized prefab.
//
// translation maps prefab-local coordinates into source world, which is the
// inverse of the canonicalize_rigid translation a recipe records. A nil
// translation becomes an explicit (0, 0, 0) rather than the identity transform:
// a prefab_local document declares a translation even when it happens to be
// zero.
func PrefabLocalFrame(translation *[3]float64) Frame {
	if translation == nil {
		return Frame{Space: SpacePrefabLocal, Translation: &[3]float64{0, 0, 0}}
	}
	value := *translation
	return Frame{Space: SpacePrefabLocal, Translation: &value}
}

// Validate checks the frame; Go has no constructor to run it on
// construction. An empty Space is the zero value and means source world.
func (f Frame) Validate() error {
	space := f.space()
	if space != SpaceSourceWorld && space != SpacePrefabLocal {
		return newError("unknown frame space: %q", f.Space)
	}
	if space == SpaceSourceWorld && f.Translation != nil {
		// SEM-4: a document already in source coordinates cannot also declare a
		// translation back to them.
		return newError("a source_world frame cannot carry a translation")
	}
	return nil
}

func (f Frame) space() string {
	if f.Space == "" {
		return SpaceSourceWorld
	}
	return f.Space
}

// ToJSON is the frame's document representation.
func (f Frame) ToJSON() (*Object, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}

	transform := NewObject()
	if f.Translation == nil {
		transform.Set("type", "identity")
	} else {
		values := make([]Value, 0, 3)
		for _, component := range *f.Translation {
			number, err := encodeNumber(component)
			if err != nil {
				return nil, err
			}
			values = append(values, number)
		}
		transform.Set("type", "translation")
		transform.Set("translation", values)
	}

	system := NewObject()
	system.Set("handedness", "right")
	system.Set("up_axis", "z")

	frame := NewObject()
	frame.Set("space", f.space())
	frame.Set("units", "quake_unit")
	frame.Set("coordinate_system", system)
	frame.Set("to_source", transform)
	return frame, nil
}

// ------------------------------------------------------------- model -> APMap

// Reused is per-path metadata recovered from a previous encoding of a document.
type Reused struct {
	Entities map[string]*Object
	Brushes  map[string]*Object
	Faces    map[string]*Object
}

// NewReused builds an empty reuse index.
func NewReused() *Reused {
	return &Reused{
		Entities: map[string]*Object{},
		Brushes:  map[string]*Object{},
		Faces:    map[string]*Object{},
	}
}

// ConvertOptions carries the conversion's optional arguments. Every zero value
// is the ordinary default, so the common call sets four fields and leaves the
// rest alone.
type ConvertOptions struct {
	// Identity is required; there is no sensible default for it.
	Identity   Identity
	Role       string
	DocumentID string
	// Frame's zero value is SourceFrame.
	Frame      Frame
	Revision   int
	Provenance Value
	// Authorship is the document's APMap 1.4 `authorship` object, written after provenance
	// (SER-5), or nil for none. AUE never invents one — a `.map` has no word for who made it, and
	// §9b forbids filling it from anything but the source — so only an APMap -> APMap operation
	// that carries the source's authorship forward sets it, and it is written back verbatim:
	// absent stays absent, "" stays "", and the text is never trimmed or normalized.
	Authorship *Object
	// Groups is the document's group list. AUE does not create groups — a `.map`
	// has no word for one — so a fresh import writes the empty array the current
	// contract requires. An APMap -> APMap operation that preserves the rest of
	// the document's editor structure passes the source's groups through here.
	Groups        []Value
	Relationships []Value
	Extensions    *Object
	// Game defaults to DefaultGame, MapDialect to DefaultDialect, Version to
	// APMapVersion and IDPolicy to PolicyDerived.
	Game       string
	MapDialect string
	Version    string
	IDPolicy   string
	// Reused is the identity re-attachment index; nil means none, which is the
	// fresh-import case.
	Reused *Reused
	// SyntheticFaces maps an object path to a synthetic_reason. A face named
	// there is marked synthetic: true, which is how a cropped-context cap face
	// becomes self-describing without a sidecar lookup.
	SyntheticFaces map[string]string
	// Source names the document in parse errors from ImportMapText.
	Source string
	// Contract is the contract this conversion writes. Nil means the process's default.
	Contract *Contract
	// Context cancels the conversion between entities, returning its Err. Nil means never cancelled.
	Context context.Context
	// OnClassicFidelity, when set, is told about every Classic face whose SER-7
	// spelling would have moved its texture by more than ClassicFidelityBoundTexels:
	// rebased (stored as other numbers that draw the same texels) or unrepresentable
	// (kept literal, and the caller must say so). Q2_002A.
	OnClassicFidelity func(path string, fidelity ClassicFidelity)
}

func (o ConvertOptions) withDefaults() ConvertOptions {
	if o.Version == "" {
		// WRITE current only: derived from the loaded contract, never a constant. A producer that
		// named its own version could emit a format the validator does not speak.
		if o.Contract != nil {
			o.Version = o.Contract.Version
		} else {
			o.Version = CurrentVersion()
		}
	}
	if o.Game == "" {
		o.Game = DefaultGame
	}
	if o.MapDialect == "" {
		o.MapDialect = DefaultDialect
	}
	if o.IDPolicy == "" {
		o.IDPolicy = PolicyDerived
	}
	if o.Frame.Space == "" && o.Frame.Translation == nil {
		o.Frame = SourceFrame
	}
	return o
}

// FromMapDocument encodes a `.map` geometry model as an APMap document.
func FromMapDocument(geometry *mapfile.MapFile, opts ConvertOptions) (*Object, error) {
	opts = opts.withDefaults()

	if opts.Contract != nil {
		if !opts.Contract.IsCurrent(opts.Version) {
			return nil, &VersionError{Message: opts.Contract.UnsupportedVersionMessage(opts.Version)}
		}
	} else if !IsCurrentVersion(opts.Version) {
		return nil, &VersionError{Message: DeprecatedVersionMessage(opts.Version)}
	}
	if opts.IDPolicy != PolicyDerived && opts.IDPolicy != PolicyMinted {
		return nil, newError("unknown id_policy: %q", opts.IDPolicy)
	}
	if opts.Identity == nil {
		return nil, newError("a conversion requires an identity")
	}
	if geometry == nil {
		return nil, newError("a conversion requires a geometry document")
	}
	identity := normalizeIdentity(opts.Identity)

	// SEM-8: only a "derived" document promises a derivation record on every
	// object. Writing one into a "minted" document would be a false claim, and
	// would break the round trip for documents that never had one.
	recordDerivation := opts.IDPolicy == PolicyDerived

	frame, err := opts.Frame.ToJSON()
	if err != nil {
		return nil, err
	}

	encoded := NewObject()
	encoded.Set("$schema", fmt.Sprintf(SchemaURLTemplate, opts.Version))
	encoded.Set("apmap_version", opts.Version)
	encoded.Set("document_id", opts.DocumentID)
	encoded.Set("revision", json.Number(strconv.Itoa(opts.Revision)))
	encoded.Set("role", opts.Role)
	encoded.Set("game", opts.Game)
	encoded.Set("map_dialect", opts.MapDialect)
	encoded.Set("id_policy", opts.IDPolicy)
	encoded.Set("frame", frame)
	if present(opts.Provenance) {
		encoded.Set("provenance", opts.Provenance)
	}
	if opts.Authorship != nil {
		encoded.Set("authorship", opts.Authorship)
	}

	ord := &ordinal{}
	entities := []Value{}
	for entityIndex := 0; entityIndex < geometry.EntityCount(); entityIndex++ {
		if opts.Context != nil {
			if err := opts.Context.Err(); err != nil {
				return nil, err
			}
		}
		entity := geometry.Entity(entityIndex)
		path, err := ObjectPath(entityIndex, -1, -1)
		if err != nil {
			return nil, err
		}

		node := NewObject()
		id, err := idFor(identity, KindEntity, path, ord)
		if err != nil {
			return nil, err
		}
		node.Set("entity_id", id)
		if recordDerivation {
			derivation, err := recordFor(identity, path, ord)
			if err != nil {
				return nil, err
			}
			node.Set("derived_from", derivation)
		}

		content := []Value{}
		brushIndex := 0
		for _, item := range entity.Items() {
			switch typed := item.(type) {
			case mapfile.KeyValue:
				property := NewObject()
				property.Set("kind", "property")
				property.Set("key", typed.Key())
				property.Set("value", typed.Value())
				content = append(content, property)
			case mapfile.Brush:
				brushPath, err := ObjectPath(entityIndex, brushIndex, -1)
				if err != nil {
					return nil, err
				}
				brushNode, err := encodeBrush(typed, entityIndex, brushIndex, brushPath, identity, ord, opts, recordDerivation)
				if err != nil {
					return nil, err
				}
				content = append(content, reattach(brushNode, reusedBrushes(opts.Reused), brushPath, brushMembers))
				brushIndex++
			default:
				return nil, newError("unsupported entity item: %T", item)
			}
		}
		node.Set("content", content)
		entities = append(entities, reattach(node, reusedEntities(opts.Reused), path, entityMembers))
	}
	encoded.Set("entities", entities)

	// Always emitted, empty or not: `groups` is required of a current writer, and an absent one
	// would be indistinguishable from a producer that has never heard of groups. It sits between
	// entities and relationships, which is where the contract's canonical order puts it.
	encoded.Set("groups", append([]Value(nil), opts.Groups...))

	if len(opts.Relationships) > 0 {
		encoded.Set("relationships", append([]Value(nil), opts.Relationships...))
	}
	if opts.Extensions.Len() > 0 {
		encoded.Set("extensions", shallowCopy(opts.Extensions))
	}
	return encoded, nil
}

func encodeBrush(
	brush mapfile.Brush,
	entityIndex, brushIndex int,
	brushPath string,
	identity Identity,
	ord *ordinal,
	opts ConvertOptions,
	recordDerivation bool,
) (*Object, error) {
	node := NewObject()
	node.Set("kind", "brush")

	id, err := idFor(identity, KindBrush, brushPath, ord)
	if err != nil {
		return nil, err
	}
	node.Set("brush_id", id)
	if recordDerivation {
		derivation, err := recordFor(identity, brushPath, ord)
		if err != nil {
			return nil, err
		}
		node.Set("derived_from", derivation)
	}

	// Q2_002A: the points every reader will see are the SER-7 ones, and a Classic
	// face's base plane is re-derived from them. Under qbsp's reader (every game but
	// Quake III, whose Q3Map2 rule this does not touch) each Classic face's numbers
	// are chosen for those points; the brush's emitted polygons are built once, and
	// only if a face needs them.
	emitted := make([][3]vec3, brush.FaceCount())
	for faceIndex := range emitted {
		for p, point := range brush.Face(faceIndex).Points() {
			emitted[faceIndex][p] = vec3{ser7(point.X()), ser7(point.Y()), ser7(point.Z())}
		}
	}
	var polygons [][]vec3
	corners := func(index int) func() []vec3 {
		return func() []vec3 {
			if polygons == nil {
				polygons = emittedPolygons(emitted)
			}
			return polygons[index]
		}
	}

	faces := []Value{}
	for faceIndex := 0; faceIndex < brush.FaceCount(); faceIndex++ {
		facePath, err := ObjectPath(entityIndex, brushIndex, faceIndex)
		if err != nil {
			return nil, err
		}
		source := brush.Face(faceIndex)
		var fidelity *ClassicFidelity
		if opts.Game != "quake3" && source.TextureMode() != mapfile.ModeValve220 {
			var sourcePoints [3]vec3
			for p, point := range source.Points() {
				sourcePoints[p] = vec3{point.X(), point.Y(), point.Z()}
			}
			for _, value := range []float64{source.XOffset(), source.YOffset(), source.Rotation(), source.XScale(), source.YScale()} {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					return nil, newError("SEM-11 non-finite number: %v", value)
				}
			}
			decided := FaithfulClassicNumbers(sourcePoints,
				ClassicNumbers{Shift: [2]float64{source.XOffset(), source.YOffset()}, Rotation: source.Rotation(), Scale: [2]float64{source.XScale(), source.YScale()}},
				emitted[faceIndex], corners(faceIndex))
			if decided.Outcome == ClassicRebased || decided.Outcome == ClassicUnrepresentable {
				fidelity = &decided
				if opts.OnClassicFidelity != nil {
					opts.OnClassicFidelity(facePath, decided)
				}
			}
		}
		face, err := encodeFace(source, facePath, identity, ord, opts, recordDerivation)
		if err != nil {
			return nil, err
		}
		if fidelity != nil && fidelity.Outcome == ClassicRebased {
			face, err = rebasedFace(face, emitted[faceIndex], fidelity.Numbers)
			if err != nil {
				return nil, err
			}
		}
		faces = append(faces, face)
	}
	node.Set("faces", faces)
	return node, nil
}

// ordinal is the monotonic counter authored identities derive from.
//
// The asymmetry between peek and take is load-bearing: an object's
// id and its derivation record must name the *same* ordinal, so the id reads the
// counter without advancing it and the record advances it. Getting this
// backwards — or advancing twice — would give one object two ordinals and the
// next object none.
type ordinal struct{ value int }

func (o *ordinal) peek() int { return o.value }

func (o *ordinal) take() int {
	current := o.value
	o.value++
	return current
}

// idFor is the branch the prompt singles out: an authored identity derives an id
// from the conversion-wide ordinal, the other two from the object's structural
// path. Getting it backwards makes every authored object collide on ordinal 0.
func idFor(identity Identity, kind Kind, path string, ord *ordinal) (string, error) {
	switch typed := identity.(type) {
	case AuthoredIdentity:
		return typed.IDFor(kind, ord.peek())
	case PackageIdentity:
		return typed.IDFor(kind, path)
	case SourceMapIdentity:
		return typed.IDFor(kind, path)
	default:
		return "", newError("unsupported identity: %T", identity)
	}
}

func recordFor(identity Identity, path string, ord *ordinal) (*Object, error) {
	switch typed := identity.(type) {
	case AuthoredIdentity:
		return typed.Record(ord.take())
	case PackageIdentity:
		return typed.Record(path)
	case SourceMapIdentity:
		return typed.Record(path)
	default:
		return nil, newError("unsupported identity: %T", identity)
	}
}

// SER-5. The specification declares a member order for each object. Setting a
// member after construction appends it, so the order is *imposed* here rather
// than trusted to the order the builders above happen to write members in — a
// document built in the wrong order is schema-valid but not canonical, and would
// fail IsCanonical.
var (
	entityMembers = []string{"entity_id", "derived_from", "content", "extensions"}
	brushMembers  = []string{"kind", "brush_id", "derived_from", "faces", "extensions"}
	faceMembers   = []string{
		"face_id",
		"derived_from",
		"plane",
		"texture",
		"projection",
		"tail",
		"synthetic",
		"synthetic_reason",
		"extensions",
	}
)

func ordered(node *Object, members []string) *Object {
	out := NewObject()
	for _, key := range members {
		if value, ok := node.Get(key); ok {
			out.Set(key, value)
		}
	}
	// Anything undeclared would fail schema validation anyway; keeping it rather
	// than dropping it means the validator gets to say so.
	declared := make(map[string]bool, len(members))
	for _, key := range members {
		declared[key] = true
	}
	for _, key := range node.Keys() {
		if !declared[key] {
			value, _ := node.Get(key)
			out.Set(key, value)
		}
	}
	return out
}

// reattach restores the identity and metadata a previous encoding recorded for
// path.
//
// The lookup is by structural path and nothing else. Re-attaching by array index
// would misattribute every piece of metadata the moment two documents' entity
// order differed, and would do so silently — the output would still be a
// well-formed document, just one asserting that a brush is an object it is not.
func reattach(node *Object, store map[string]*Object, path string, members []string) *Object {
	if previous, ok := store[path]; ok && previous != nil {
		for _, key := range previous.Keys() {
			value, _ := previous.Get(key)
			node.Set(key, value)
		}
	}
	return ordered(node, members)
}

func reusedEntities(reused *Reused) map[string]*Object {
	if reused == nil {
		return nil
	}
	return reused.Entities
}

func reusedBrushes(reused *Reused) map[string]*Object {
	if reused == nil {
		return nil
	}
	return reused.Brushes
}

func reusedFaces(reused *Reused) map[string]*Object {
	if reused == nil {
		return nil
	}
	return reused.Faces
}

func encodeFace(
	face mapfile.Face,
	path string,
	identity Identity,
	ord *ordinal,
	opts ConvertOptions,
	recordDerivation bool,
) (*Object, error) {
	node := NewObject()

	id, err := idFor(identity, KindFace, path, ord)
	if err != nil {
		return nil, err
	}
	node.Set("face_id", id)
	if recordDerivation {
		derivation, err := recordFor(identity, path, ord)
		if err != nil {
			return nil, err
		}
		node.Set("derived_from", derivation)
	}

	points := []Value{}
	for _, point := range face.Points() {
		encodedPoint, err := encodeVec3(point)
		if err != nil {
			return nil, err
		}
		points = append(points, encodedPoint)
	}
	plane := NewObject()
	plane.Set("points", points)
	node.Set("plane", plane)
	node.Set("texture", face.Texture())

	projection, err := encodeProjection(face)
	if err != nil {
		return nil, err
	}
	node.Set("projection", projection)

	if tail := face.ExtraValues(); len(tail) > 0 {
		values := make([]Value, 0, len(tail))
		for _, value := range tail {
			number, err := encodeNumber(value)
			if err != nil {
				return nil, err
			}
			values = append(values, number)
		}
		node.Set("tail", values)
	}

	if reason := opts.SyntheticFaces[path]; reason != "" {
		node.Set("synthetic", true)
		node.Set("synthetic_reason", reason)
	}
	return reattach(node, reusedFaces(opts.Reused), path, faceMembers), nil
}

// rebasedFace stores a rebased Classic face's numbers WITH the rounded points they
// were chosen for. Serialization would round the points anyway; storing them now
// keeps the in-memory document from pairing full-precision points with numbers
// meant for the rounded ones (Q2_002A).
func rebasedFace(face *Object, points [3]vec3, numbers ClassicNumbers) (*Object, error) {
	encodedPoints := []Value{}
	for _, point := range points {
		values := make([]Value, 0, 3)
		for _, component := range point {
			number, err := encodeNumber(component)
			if err != nil {
				return nil, err
			}
			values = append(values, number)
		}
		encodedPoints = append(encodedPoints, values)
	}
	plane := NewObject()
	plane.Set("points", encodedPoints)
	number := func(value float64) Value { n, _ := encodeNumber(value); return n }
	projection := NewObject()
	projection.Set("mode", "classic")
	projection.Set("shift", []Value{number(numbers.Shift[0]), number(numbers.Shift[1])})
	projection.Set("rotation", number(numbers.Rotation))
	projection.Set("scale", []Value{number(numbers.Scale[0]), number(numbers.Scale[1])})
	face.Set("plane", plane)
	face.Set("projection", projection)
	return face, nil
}

// encodeProjection maps a `.map` face's texture representation onto APMap's
// projection shape. The two are not 1:1: `.map` writes a classic face's offsets
// as two bare numbers and a Valve 220 face's axes as two bracketed four-tuples,
// where APMap tags the mode explicitly and carries rotation and scale in both
// modes.
//
// A valve220 face with no axes, or a classic face with no offsets, is an
// error. Neither state is representable in the model: NewValve220Face and
// NewClassicFace are the only ways to build a Face, and each supplies its own
// dialect's fields. The check is kept anyway for a Face that arrived as a zero
// value, which has neither mode nor axes.
func encodeProjection(face mapfile.Face) (*Object, error) {
	projection := NewObject()

	if face.TextureMode() == mapfile.ModeValve220 {
		uAxis, hasU := face.UAxis()
		vAxis, hasV := face.VAxis()
		if !hasU || !hasV {
			return nil, newError("a valve220 face requires u_axis and v_axis")
		}
		u, err := encodeAxis(uAxis)
		if err != nil {
			return nil, err
		}
		v, err := encodeAxis(vAxis)
		if err != nil {
			return nil, err
		}
		rotation, scale, err := encodeRotationAndScale(face)
		if err != nil {
			return nil, err
		}
		projection.Set("mode", "valve220")
		projection.Set("u_axis", u)
		projection.Set("v_axis", v)
		projection.Set("rotation", rotation)
		projection.Set("scale", scale)
		return projection, nil
	}

	shiftX, err := encodeNumber(face.XOffset())
	if err != nil {
		return nil, err
	}
	shiftY, err := encodeNumber(face.YOffset())
	if err != nil {
		return nil, err
	}
	rotation, scale, err := encodeRotationAndScale(face)
	if err != nil {
		return nil, err
	}
	projection.Set("mode", "classic")
	projection.Set("shift", []Value{shiftX, shiftY})
	projection.Set("rotation", rotation)
	projection.Set("scale", scale)
	return projection, nil
}

func encodeRotationAndScale(face mapfile.Face) (Value, []Value, error) {
	rotation, err := encodeNumber(face.Rotation())
	if err != nil {
		return nil, nil, err
	}
	xScale, err := encodeNumber(face.XScale())
	if err != nil {
		return nil, nil, err
	}
	yScale, err := encodeNumber(face.YScale())
	if err != nil {
		return nil, nil, err
	}
	return rotation, []Value{xScale, yScale}, nil
}

func encodeAxis(axis mapfile.TextureAxis) ([]Value, error) {
	values := make([]Value, 0, 4)
	for _, component := range [4]float64{axis.X(), axis.Y(), axis.Z(), axis.Offset()} {
		number, err := encodeNumber(component)
		if err != nil {
			return nil, err
		}
		values = append(values, number)
	}
	return values, nil
}

func encodeVec3(point mapfile.Vec3) ([]Value, error) {
	values := make([]Value, 0, 3)
	for _, component := range [3]float64{point.X(), point.Y(), point.Z()} {
		number, err := encodeNumber(component)
		if err != nil {
			return nil, err
		}
		values = append(values, number)
	}
	return values, nil
}

// encodeNumber is the model's float64 as an APMap number literal.
//
// It mirrors NormalizeNumber's tail — integral values as integers, everything
// else as CPython's repr — but deliberately does *not* apply SER-7's rounding.
// The document holds the model's full-precision float and Encode rounds on the
// way out. Rounding here would round twice for anything
// that gets encoded, and would silently lower precision for a caller that only
// wants the document in memory.
func encodeNumber(value float64) (json.Number, error) {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return "", newError("SEM-11 non-finite number: %v", value)
	}
	if value == math.Trunc(value) {
		if math.Abs(value) < 1e18 {
			return json.Number(strconv.FormatInt(int64(value), 10)), nil
		}
		return json.Number(exactInteger(value)), nil
	}
	return json.Number(formatFloat(value)), nil
}

// ------------------------------------------------------------- APMap -> model

// ToMapDocument decodes an APMap document into the `.map` geometry model.
//
// Order is preserved exactly — entities, content items and faces all keep their
// positions — which is what lets Rebuild find them again by path.
//
// A document holding a Quake III patch or a brush_primitives face is REFUSED, whole and before
// anything is lowered, with a *Quake3UnsupportedError naming the surface: the `.map` model has no
// patch and no texture matrix, and a map written without them — or with the matrix flattened into a
// classic projection — would be a different map that looks like the right one. Every `.map`, RMF,
// entity-catalogue and prefab-resolve path lowers through here, so each refuses the same way.
func ToMapDocument(document Value) (*mapfile.MapFile, error) {
	return ToMapDocumentContext(context.Background(), document)
}

// ToMapDocumentContext is ToMapDocument, cancelled between entities with ctx's Err.
func ToMapDocumentContext(ctx context.Context, document Value) (*mapfile.MapFile, error) {
	root, ok := document.(*Object)
	if !ok {
		return nil, newError("an APMap document must be a JSON object")
	}
	if err := Quake3SurfaceRefusal(root, "APMap -> .map conversion"); err != nil {
		return nil, err
	}

	nodes, err := requiredArray(root, "entities")
	if err != nil {
		return nil, err
	}

	entities := make([]mapfile.Entity, 0, len(nodes))
	for entityIndex, node := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entity, ok := node.(*Object)
		if !ok {
			return nil, newError("entity %d is not an object", entityIndex)
		}
		content, err := requiredArray(entity, "content")
		if err != nil {
			return nil, err
		}

		items := make([]mapfile.EntityItem, 0, len(content))
		for _, raw := range content {
			item, ok := raw.(*Object)
			if !ok {
				return nil, newError("entity %d has a content item that is not an object", entityIndex)
			}
			kind, _ := item.Get("kind")
			switch name, _ := kind.(string); name {
			case "property":
				key, _ := item.Get("key")
				value, _ := item.Get("value")
				items = append(items, mapfile.NewKeyValue(plainString(key), plainString(value)))
			case "brush":
				faceNodes, err := requiredArray(item, "faces")
				if err != nil {
					return nil, err
				}
				faces := make([]mapfile.Face, 0, len(faceNodes))
				for _, faceNode := range faceNodes {
					face, err := decodeFace(faceNode)
					if err != nil {
						return nil, err
					}
					faces = append(faces, face)
				}
				items = append(items, mapfile.NewBrush(faces))
			default:
				return nil, newError("unknown entity content kind: %s", render(kind))
			}
		}
		entities = append(entities, mapfile.NewEntity(items))
	}
	return mapfile.NewMapFile(entities), nil
}

func decodeFace(node Value) (mapfile.Face, error) {
	face, ok := node.(*Object)
	if !ok {
		return mapfile.Face{}, newError("a brush face must be an object")
	}

	plane, _ := face.Get("plane")
	planeObject, _ := plane.(*Object)
	points, _ := planeObject.Get("points")
	pointList, _ := points.([]Value)
	if len(pointList) != 3 {
		return mapfile.Face{}, newError("a face plane needs exactly three points")
	}
	var corners [3]mapfile.Vec3
	for index, raw := range pointList {
		vector, err := decodeVec3(raw)
		if err != nil {
			return mapfile.Face{}, err
		}
		corners[index] = vector
	}

	texture := ""
	if value, ok := face.Get("texture"); ok {
		texture = plainString(value)
	}

	tail, err := decodeTail(face)
	if err != nil {
		return mapfile.Face{}, err
	}

	projection, _ := face.Get("projection")
	projectionObject, _ := projection.(*Object)
	mode, _ := projectionObject.Get("mode")
	modeName, _ := mode.(string)

	// A brushDef texture matrix is never read as a classic projection: its shift, rotation and
	// scale are absent, so falling through would silently write the defaults in its place.
	// ToMapDocument refuses such a document before reaching here; this is the same refusal for any
	// other caller, so no path can lose the matrix.
	if modeName == "brush_primitives" {
		id, _ := stringMember(face, "face_id")
		return mapfile.Face{}, &Quake3UnsupportedError{
			Operation: "APMap -> .map face conversion",
			First:     Quake3Surface{Kind: quake3SurfaceBrushPrimitive, ID: id, Where: "this face"},
			Count:     1,
		}
	}

	rotation, err := decodeNumberOr(projectionObject, "rotation", 0)
	if err != nil {
		return mapfile.Face{}, err
	}
	xScale, yScale, err := decodePair(projectionObject, "scale", 1, 1)
	if err != nil {
		return mapfile.Face{}, err
	}

	if modeName == "valve220" {
		uAxis, err := decodeAxis(projectionObject, "u_axis")
		if err != nil {
			return mapfile.Face{}, err
		}
		vAxis, err := decodeAxis(projectionObject, "v_axis")
		if err != nil {
			return mapfile.Face{}, err
		}
		return mapfile.NewValve220Face(
			corners[0], corners[1], corners[2],
			texture,
			uAxis, vAxis,
			rotation, xScale, yScale,
			tail,
		), nil
	}

	xOffset, yOffset, err := decodePair(projectionObject, "shift", 0, 0)
	if err != nil {
		return mapfile.Face{}, err
	}
	return mapfile.NewClassicFace(
		corners[0], corners[1], corners[2],
		texture,
		xOffset, yOffset, rotation, xScale, yScale,
		tail,
	), nil
}

func decodeTail(face *Object) ([]mapfile.Number, error) {
	raw, ok := face.Get("tail")
	if !ok || raw == nil {
		return nil, nil
	}
	values, ok := raw.([]Value)
	if !ok {
		return nil, newError("a face tail must be an array of numbers")
	}
	tail := make([]mapfile.Number, 0, len(values))
	for _, value := range values {
		number, err := decodeNumber(value)
		if err != nil {
			return nil, err
		}
		tail = append(tail, number)
	}
	return tail, nil
}

func decodeVec3(value Value) (mapfile.Vec3, error) {
	components, ok := value.([]Value)
	if !ok || len(components) != 3 {
		return mapfile.Vec3{}, newError("a plane point needs exactly three components")
	}
	var numbers [3]float64
	for index, component := range components {
		number, err := decodeNumber(component)
		if err != nil {
			return mapfile.Vec3{}, err
		}
		numbers[index] = number
	}
	return mapfile.NewVec3(numbers[0], numbers[1], numbers[2]), nil
}

func decodeAxis(projection *Object, name string) (mapfile.TextureAxis, error) {
	raw, _ := projection.Get(name)
	components, ok := raw.([]Value)
	if !ok || len(components) != 4 {
		return mapfile.TextureAxis{}, newError("a valve220 projection needs four-component u_axis and v_axis")
	}
	var numbers [4]float64
	for index, component := range components {
		number, err := decodeNumber(component)
		if err != nil {
			return mapfile.TextureAxis{}, err
		}
		numbers[index] = number
	}
	return mapfile.NewTextureAxis(numbers[0], numbers[1], numbers[2], numbers[3]), nil
}

// decodePair reads a two-element numeric member, falling back to the given
// defaults when it is absent or empty. A one-element array is reported as an
// error rather than read past.
func decodePair(projection *Object, name string, firstDefault, secondDefault float64) (float64, float64, error) {
	raw, ok := projection.Get(name)
	if !ok || raw == nil {
		return firstDefault, secondDefault, nil
	}
	values, ok := raw.([]Value)
	if !ok {
		return 0, 0, newError("a projection %s must be an array of two numbers", name)
	}
	if len(values) == 0 {
		// An empty array takes the default, like an absent one.
		return firstDefault, secondDefault, nil
	}
	if len(values) < 2 {
		return 0, 0, newError("a projection %s needs two components", name)
	}
	first, err := decodeNumber(values[0])
	if err != nil {
		return 0, 0, err
	}
	second, err := decodeNumber(values[1])
	if err != nil {
		return 0, 0, err
	}
	return first, second, nil
}

func decodeNumberOr(projection *Object, name string, fallback float64) (float64, error) {
	raw, ok := projection.Get(name)
	if !ok {
		return fallback, nil
	}
	return decodeNumber(raw)
}

// decodeNumber refuses anything that is not a JSON number, booleans included,
// and anything non-finite.
//
// A literal like 1e400 decodes successfully by design (see json.go) so that the
// schema layer can accept it and SEM-11 can reject it. Let through into the
// geometry model, the `.map` writer would spell it `inf` and produce a file no
// parser accepts. Refusing it here keeps the lowering step from manufacturing an
// unparseable file.
func decodeNumber(value Value) (float64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, newError("expected a number, got %s", render(value))
	}
	parsed, err := strconv.ParseFloat(string(number), 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return 0, newError("SEM-11 non-finite number: %s", string(number))
	}
	return parsed, nil
}

// ExportMapText is the `.map` representation of an APMap document.
//
// Identity, relationships and extensions have nowhere to live in `.map` and are
// dropped — that is the documented export loss boundary, and it is why `.map` is
// an interchange representation while APMap is canonical.
func ExportMapText(document Value) (string, error) {
	return ExportMapTextContext(context.Background(), document)
}

// ExportMapTextContext is ExportMapText, cancelled between entities with ctx's Err.
func ExportMapTextContext(ctx context.Context, document Value) (string, error) {
	geometry, err := ToMapDocumentContext(ctx, document)
	if err != nil {
		return "", err
	}
	return mapfile.Write(geometry)
}

// ------------------------------------------------------------- round tripping

// CollectReusable indexes a document's per-object metadata by structural path.
//
// Everything that is *not* geometry is captured: ids, derivation records,
// extensions, synthetic markers, and any member a later version adds. Geometry
// members are excluded precisely because they are what the round trip is allowed
// to change.
//
// Defensive throughout: a malformed node is skipped rather than reported, since
// this runs on documents that may not have passed validation.
//
// A document holding a Quake III patch or a brush_primitives face is refused with a
// *Quake3UnsupportedError rather than indexed: the index is keyed by brush and face paths and keeps
// no geometry, so a patch — and a matrix, which is geometry here — would silently not come back
// from a rebuild.
func CollectReusable(document Value) (*Reused, error) {
	reused := NewReused()

	root, ok := document.(*Object)
	if !ok {
		return reused, nil
	}
	if err := Quake3SurfaceRefusal(root, "APMap identity re-attachment"); err != nil {
		return nil, err
	}
	entities, _ := root.Get("entities")
	entityList, _ := entities.([]Value)

	for entityIndex, node := range entityList {
		entity, ok := node.(*Object)
		if !ok {
			continue
		}
		path, err := ObjectPath(entityIndex, -1, -1)
		if err != nil {
			continue
		}
		reused.Entities[path] = without(entity, "content")

		content, _ := entity.Get("content")
		contentList, _ := content.([]Value)

		brushIndex := 0
		for _, raw := range contentList {
			item, ok := raw.(*Object)
			if !ok {
				continue
			}
			kind, _ := item.Get("kind")
			if name, _ := kind.(string); name != "brush" {
				continue
			}
			brushPath, err := ObjectPath(entityIndex, brushIndex, -1)
			if err != nil {
				continue
			}
			reused.Brushes[brushPath] = without(item, "kind", "faces")

			faces, _ := item.Get("faces")
			faceList, _ := faces.([]Value)
			for faceIndex, rawFace := range faceList {
				face, ok := rawFace.(*Object)
				if !ok {
					continue
				}
				facePath, err := ObjectPath(entityIndex, brushIndex, faceIndex)
				if err != nil {
					continue
				}
				reused.Faces[facePath] = without(face, "plane", "texture", "projection", "tail")
			}
			brushIndex++
		}
	}
	return reused, nil
}

// Rebuild re-encodes document around new geometry, keeping every identity.
//
// With unmodified geometry this is the identity function on the document's
// canonical bytes, which is the APMap -> model -> APMap guarantee the whole
// format rests on.
//
// A document holding a Quake III patch or a brush_primitives face is refused (a
// *Quake3UnsupportedError) before anything is built: the `.map` model the geometry arrives in has
// no place for either, so a rebuild would return the document without them.
func Rebuild(document Value, geometry *mapfile.MapFile) (*Object, error) {
	root, ok := document.(*Object)
	if !ok {
		return nil, newError("an APMap document must be a JSON object")
	}
	reused, err := CollectReusable(root)
	if err != nil {
		return nil, err
	}

	frame, err := frameFrom(root)
	if err != nil {
		return nil, err
	}
	identity, err := identityFrom(root)
	if err != nil {
		return nil, err
	}

	relationships, _ := root.Get("relationships")
	relationshipList, _ := relationships.([]Value)
	extensions, _ := root.Get("extensions")
	extensionObject, _ := extensions.(*Object)
	provenance, _ := root.Get("provenance")
	// Groups and authorship are editor structure and attribution, not geometry, so they are
	// carried across exactly as relationships are: a rebuild that kept every identity and dropped
	// the hierarchy (nested groups included) or who made the map would not be the identity
	// function it promises to be. An absent `groups` stays the empty array the writer requires.
	groups, _ := root.Get("groups")
	groupList, _ := groups.([]Value)
	authorship, _ := root.Get("authorship")
	authorshipObject, _ := authorship.(*Object)

	return FromMapDocument(geometry, ConvertOptions{
		Identity:      identity,
		Role:          textMember(root, "role", RoleFullMap),
		DocumentID:    textMember(root, "document_id", ""),
		Frame:         frame,
		Revision:      intMember(root, "revision"),
		Provenance:    provenance,
		Authorship:    authorshipObject,
		Groups:        groupList,
		Relationships: relationshipList,
		Extensions:    extensionObject,
		Game:          textMember(root, "game", DefaultGame),
		MapDialect:    textMember(root, "map_dialect", DefaultDialect),
		IDPolicy:      textMember(root, "id_policy", PolicyDerived),
		Reused:        reused,
	})
}

func frameFrom(document *Object) (Frame, error) {
	raw, _ := document.Get("frame")
	frame, _ := raw.(*Object)
	space := textMember(frame, "space", SpaceSourceWorld)

	rawTransform, _ := frame.Get("to_source")
	transform, _ := rawTransform.(*Object)
	kind, _ := transform.Get("type")
	if name, _ := kind.(string); name != "translation" {
		return Frame{Space: space}, nil
	}

	values, _ := transform.Get("translation")
	components, ok := values.([]Value)
	if !ok || len(components) == 0 {
		// An absent or empty translation is the zero translation.
		return Frame{Space: space, Translation: &[3]float64{0, 0, 0}}, nil
	}
	if len(components) != 3 {
		// Carrying a short translation through would re-emit a frame the schema
		// rejects. Reporting it is better than producing a document that cannot
		// be validated.
		return Frame{}, newError("a frame translation needs exactly three components")
	}
	var translation [3]float64
	for index, component := range components {
		number, err := decodeNumber(component)
		if err != nil {
			return Frame{}, err
		}
		translation[index] = number
	}
	return Frame{Space: space, Translation: &translation}, nil
}

// identityFrom recovers the identity source a document was produced with.
//
// Only used to keep Rebuild honest when a *new* object appears: every
// pre-existing object gets its id back from the reuse index, so a document whose
// geometry is unchanged never depends on what this returns.
func identityFrom(document *Object) (Identity, error) {
	entities, _ := document.Get("entities")
	entityList, _ := entities.([]Value)

	for _, node := range entityList {
		entity, ok := node.(*Object)
		if !ok {
			continue
		}
		raw, _ := entity.Get("derived_from")
		record, ok := raw.(*Object)
		if !ok {
			continue
		}
		kind, _ := record.Get("kind")
		switch name, _ := kind.(string); name {
		case "package":
			components, err := stringComponents(record, "package_id", "role")
			if err != nil {
				return nil, err
			}
			return PackageIdentity{PackageID: components[0], Role: components[1]}, nil
		case "source_map":
			components, err := stringComponents(record, "source_map", "source_sha256")
			if err != nil {
				return nil, err
			}
			return SourceMapIdentity{SourceMap: components[0], SourceSHA256: components[1]}, nil
		case "authored":
			components, err := stringComponents(record, "document_id", "operation_id")
			if err != nil {
				return nil, err
			}
			return AuthoredIdentity{DocumentID: components[0], OperationID: components[1]}, nil
		}
	}

	return PackageIdentity{
		PackageID: textMember(document, "document_id", "document"),
		Role:      textMember(document, "role", RoleFullMap),
	}, nil
}

// ------------------------------------------------------------------ .map import

// ImportMapText parses `.map` text and encodes it as an APMap document.
func ImportMapText(text string, opts ConvertOptions) (*Object, error) {
	source := opts.Source
	if source == "" {
		source = "<map>"
	}
	geometry, err := mapfile.ParseNamed(text, source)
	if err != nil {
		return nil, err
	}
	// Reused is not a parameter of the import path: a fresh import
	// has no previous encoding to reattach from, and passing one would be
	// asserting an identity the source text does not carry.
	opts.Reused = nil
	return FromMapDocument(geometry, opts)
}

// ---------------------------------------------------------------- small helpers

func requiredArray(node *Object, name string) ([]Value, error) {
	raw, ok := node.Get(name)
	if !ok || raw == nil {
		return nil, nil
	}
	values, ok := raw.([]Value)
	if !ok {
		return nil, newError("%q must be an array", name)
	}
	return values, nil
}

func textMember(node *Object, name, fallback string) string {
	value, ok := node.Get(name)
	if !ok {
		return fallback
	}
	return stringify(value)
}

func intMember(node *Object, name string) int {
	value, ok := node.Get(name)
	if !ok {
		return 0
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0
	}
	parsed, err := strconv.ParseFloat(string(number), 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return 0
	}
	return int(parsed)
}

// plainString is str() for a member that is coerced rather than checked.
func plainString(value Value) string {
	if value == nil {
		return ""
	}
	return stringify(value)
}

func without(node *Object, excluded ...string) *Object {
	drop := make(map[string]bool, len(excluded))
	for _, name := range excluded {
		drop[name] = true
	}
	out := NewObject()
	for _, key := range node.Keys() {
		if drop[key] {
			continue
		}
		value, _ := node.Get(key)
		out.Set(key, value)
	}
	return out
}

func shallowCopy(node *Object) *Object {
	out := NewObject()
	for _, key := range node.Keys() {
		value, _ := node.Get(key)
		out.Set(key, value)
	}
	return out
}

// present is Python's `is not None` for a Value, with the typed-nil trap closed:
// a (*Object)(nil) stored in an interface is not a nil interface, and writing it
// into a document would emit `null` where the member must be omitted entirely.
func present(value Value) bool {
	if value == nil {
		return false
	}
	if object, ok := value.(*Object); ok {
		return object != nil
	}
	return true
}
