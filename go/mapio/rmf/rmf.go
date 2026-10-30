// Package rmf reads Worldcraft/Hammer RMF v1.6 and v1.8, and writes v1.8.
//
// Reading is rmf.go; writing is encode.go, which states what a written file
// does and does not carry. The pair is not symmetric and is not claimed to be:
// this reader keeps what Worldcraft's own "export to .map" keeps — geometry,
// entities, textures and Quake 2 surface properties — and a written file can
// therefore only contain that much. Visgroups and groups are parsed just far
// enough to consume their bytes correctly and are written as absent, because
// APMap 1.0 has no field for either and inventing one is a schema-versioning
// decision rather than something to improvise here.
//
// There is deliberately no direct RMF↔`.map` path in either direction. Both
// go through APMap, which is the format that carries identity and provenance;
// a shortcut would be a second conversion contract to keep in step with this
// one.
//
// # The structure, as validated
//
// Little-endian throughout. Every claim below was checked byte-for-byte against
// four real, human-authored fixtures with paired Worldcraft `.map` exports, not
// taken from the community specification alone. Where the two disagreed the
// fixtures won, and the difference is recorded at the site.
//
//	float32  version        // 1.8
//	[3]byte  magic          // "RMF"
//	int32    visgroup_count
//	[140]byte visgroups[visgroup_count]
//	World    worldspawn
//	         // optional "DOCINFO" trailer, ignored
//
// Every object shares one header, *including its children array*, and the
// children come before any type-specific trailing data:
//
//	p_char  object_type   // "CMapWorld" / "CMapSolid" / "CMapEntity" / "CMapGroup"
//	int32   visgroup_id
//	[3]byte color
//	int32   child_count
//	Object  children[child_count]   // recurse here, then read the trailing data
package rmf

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/convex"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
)

// SupportedVersion is the version this package WRITES, and the one four of the
// five verified fixtures store.
const SupportedVersion = 1.8

// LegacyVersion is the older layout this package also reads.
//
// # How the 1.6 layout was established, and what "verified" means here
//
// Not from the community notes, which this package has never trusted: by
// parsing a real 1.6 file end to end and checking that the parse LANDS. The
// file is a level authored in March 1998, the one 1.6 document in a private
// corpus of thirty-one. There is no paired Worldcraft `.map`
// export for it, so the oracle is the file's own structure, and it is a strong
// one: a wrong field width desynchronises a recursive binary format within a
// few objects.
//
// Four independent checks agree, and all four are asserted by the owner's
// corpus test against the real file when it is reachable:
//
//	the walk ends exactly 20 bytes from EOF — the same DOCINFO trailer the
//	four 1.8 fixtures end with;
//	the object counts it recovers (1 world, 116 groups, 1,189 solids, 200
//	entities) equal the counts of the type-name byte patterns in the file;
//	every one of the 7,035 faces reads a scale of exactly (1.0, 1.0) and a
//	rotation drawn from {0, 45, 75, 90, 270};
//	the single trailing int32 is only ever 0 or 1, which is what "alignment"
//	is in 1.8 as well.
//
// The competing split that also adds up — a 36-byte texture block and two
// trailing int32s — fails the third and fourth: it reads the fifth projection
// float as an integer 0x3f800000. So the layout is measured rather than
// guessed, which is the standard this package's 1.8 notes were held to.
//
// # The two differences, both in the face record
//
//	texture block   40 bytes, not 260
//	trailing int32s 1, not 4
//
// Everything else — header, visgroup record size, object header, entity data,
// solid layout, plane points — is byte-identical to 1.8.
//
// The three int32s that are missing are Quake 2's surface flags, contents and
// value. They are ABSENT in 1.6, not zero: Worldcraft added the Quake 2 surface
// block after this version, so a 1.6 face has no such data to lose, and the
// faces this reader produces from one carry no `.map` tail — which is exactly
// what `hasSurfaceProperties` already reports for a face whose three values are
// all zero. Nothing is invented for them.
const LegacyVersion = 1.6

// versionTolerance covers float32 representation: 1.8 stores as 1.7999999523.
const versionTolerance = 1e-4

// ReadableVersions is every version Decode accepts, lowest first.
//
// Exported so a caller — and a test — can enumerate the capability rather than
// restate it. v2.2 is still refused: its Face layout differs again and no real
// 2.2 file has been available to check a reading against, which is the whole
// standard this package holds itself to.
func ReadableVersions() []float64 { return []float64{LegacyVersion, SupportedVersion} }

// readableVersion matches a stored float32 against the versions this build
// reads, returning the canonical value.
func readableVersion(stored float32) (float64, bool) {
	for _, candidate := range ReadableVersions() {
		if math.Abs(float64(stored)-candidate) <= versionTolerance {
			return candidate, true
		}
	}

	return 0, false
}

// faceLayout is what one face record looks like in a given version. The two
// fields are the whole difference between 1.6 and 1.8.
type faceLayout struct {
	// TextureBlock is the fixed-size, NUL-terminated texture-name field.
	TextureBlock int
	// SurfaceInts is how many int32s follow the five projection floats:
	// alignment alone in 1.6, alignment plus Quake 2's surface flags, contents
	// and value in 1.8.
	SurfaceInts int
}

func layoutFor(version float64) faceLayout {
	if math.Abs(version-LegacyVersion) <= versionTolerance {
		return faceLayout{TextureBlock: 40, SurfaceInts: 1}
	}

	return faceLayout{TextureBlock: 260, SurfaceInts: 4}
}

// Magic is the three bytes following the version.
const Magic = "RMF"

// visgroupRecordSize is the size of one VisGroup record.
//
// Derived from the fixtures, not from the specification, which does not give a
// layout: with 140 the world object's type string lands exactly at
// 11 + 140*visgroup_count in all four files, and no other candidate size does.
const visgroupRecordSize = 140

// Object type names.
const (
	TypeWorld  = "CMapWorld"
	TypeSolid  = "CMapSolid"
	TypeEntity = "CMapEntity"
	TypeGroup  = "CMapGroup"
)

// Error is a structured RMF read failure.
type Error struct {
	Offset  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("rmf: %s (at byte %d)", e.Message, e.Offset)
}

// Stats records what a read produced, for callers that want to report on the
// conversion rather than only consume it.
type Stats struct {
	// Version is the RMF version the file declared, canonicalised to one of
	// ReadableVersions. A caller that wants to know whether Quake 2 surface
	// properties could have been present at all asks this rather than inferring
	// it from an absence of them.
	Version   float64
	Visgroups int
	Groups    int
	Solids    int
	Entities  int
	Faces     int
	// DegenerateFaces is the number of faces whose three plane points do not
	// define a plane. Worldcraft drops these on export and so does this reader;
	// the count is reported rather than hidden.
	DegenerateFaces int
	// EmptyBrushEntities is the number of entities dropped for carrying neither
	// geometry nor a position. See the note on dropEmptyEntity.
	EmptyBrushEntities int
	// EmptySolids is the number of solids that had no usable face left and so
	// produced no brush. Worldcraft writes an empty `{ }` block for these.
	EmptySolids int
}

// reader is a bounds-checked cursor over the file.
type reader struct {
	data   []byte
	offset int
	// layout is the version-dependent face record shape. Zero-valued until
	// Decode has read the header, which is before any face is reached.
	layout faceLayout
}

func (r *reader) fail(message string) error {
	return &Error{Offset: r.offset, Message: message}
}

func (r *reader) need(n int) error {
	if n < 0 || r.offset+n > len(r.data) {
		return &Error{
			Offset:  r.offset,
			Message: fmt.Sprintf("truncated file: wanted %d more bytes, %d remain", n, len(r.data)-r.offset),
		}
	}
	return nil
}

func (r *reader) bytes(n int) ([]byte, error) {
	if err := r.need(n); err != nil {
		return nil, err
	}
	out := r.data[r.offset : r.offset+n]
	r.offset += n
	return out, nil
}

func (r *reader) uint8() (byte, error) {
	raw, err := r.bytes(1)
	if err != nil {
		return 0, err
	}
	return raw[0], nil
}

func (r *reader) int32() (int32, error) {
	raw, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(raw)), nil
}

func (r *reader) float32() (float32, error) {
	raw, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(raw)), nil
}

func (r *reader) vector() (mapfile.Vec3, error) {
	var out [3]float64
	for axis := 0; axis < 3; axis++ {
		value, err := r.float32()
		if err != nil {
			return mapfile.Vec3{}, err
		}
		out[axis] = roundCoordinate(float64(value))
	}
	return mapfile.NewVec3(out[0], out[1], out[2]), nil
}

// roundCoordinate removes float32 editor noise so a coordinate can be written
// into a `.map`.
//
// RMF stores coordinates as float32. Widening to float64 preserves the exact
// float32 value, which for a corner the author placed at zero can be something
// like -1.1368683772161603e-13 — accumulated noise from dragging in the editor.
// Written out verbatim that becomes scientific notation, which no `.map` parser
// accepts and which Worldcraft's own export never produces: there is not one
// exponent-form coordinate in any of the four exports.
//
// Six decimals is far finer than float32 carries at map magnitudes, so nothing
// authored is lost.
//
// # Why rounding alone is not enough
//
// Six-decimal rounding does not remove the exponent form, it only makes it
// rarer: 2e-06 rounds to 0.000002, which is still written `2e-06`, because
// mapfile.FormatNumber reproduces CPython's rule of switching to scientific
// notation below 1e-4. ter_1 contains exactly this, and it made the `.map` this
// repository produced from it unparseable by this repository's own parser —
// found by converting the corpus in both directions, not by inspection.
//
// So anything that would render in exponent form is snapped to zero. The
// threshold is not chosen for convenience; it is the same 1e-4 at which
// FormatNumber switches notation, which makes the postcondition exact rather
// than approximate: no coordinate this reader produces can be written in
// exponent form.
//
// Nothing authored is at risk at that magnitude. A Quake unit is the grid, the
// finest snap Worldcraft offers is a small fraction of one, and 1e-4 units is
// four orders of magnitude below that. The oracle agrees: there is not one
// exponent-form coordinate in any of the four Worldcraft exports, and where our
// reader produced 2e-06 the paired export holds 0.
const coordinateNoiseFloor = 1e-4

func roundCoordinate(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(value, 'f', 6, 64), 64)
	if err != nil {
		return value
	}
	if math.Abs(rounded) < coordinateNoiseFloor {
		// Also normalizes -0.0, which would otherwise be written as "-0".
		return 0
	}
	return rounded
}

// pchar reads a length-prefixed string.
//
// The length byte is inconsistent across real files: some count a trailing NUL
// and some do not — "CMapWorld" is written with length 10 in all four fixtures.
// Truncating at the first NUL handles both without having to know which.
func (r *reader) pchar() (string, error) {
	length, err := r.uint8()
	if err != nil {
		return "", err
	}
	raw, err := r.bytes(int(length))
	if err != nil {
		return "", err
	}
	if end := indexByte(raw, 0); end >= 0 {
		raw = raw[:end]
	}
	return string(raw), nil
}

func indexByte(raw []byte, value byte) int {
	for index, item := range raw {
		if item == value {
			return index
		}
	}
	return -1
}

// entityData is the block CMapWorld and CMapEntity both carry.
type entityData struct {
	Classname string
	// Angle is the entity's rotation, and it is the field Worldcraft
	// SYNTHESISES `angle` from on export.
	//
	// # Why this was believed unrecoverable, and what it actually is
	//
	// The RMF files contain the string "angle" nowhere: it is not among the
	// key/value pairs. The Quake II oracle therefore excluded
	// `angle` from its comparison and recorded that Worldcraft "synthesises the
	// key on export out of a per-object rotation field whose layout this
	// reader's specification does not give", and the backlog carried it as
	// reverse-engineering to do only if a visible defect justified it.
	//
	// It is the four bytes immediately after the classname — the ones this
	// reader discarded as unknown — read as a little-endian int32. Established
	// the way everything else here was: by measurement against all four paired
	// fixtures, not from a specification.
	//
	//	dm_1    28 non-zero values, 28 `angle` keys in the export
	//	m3_1    85                  85
	//	ter_1  103                 103
	//	ware_1  99                  99
	//
	// and per entity, matched on classname plus rounded origin, 2,002 point
	// entities agree with ZERO disagreements. The values are what an angle
	// looks like: 0, 90, 180, 270, and Quake's -1 and -2 for up and down.
	//
	// The rule Worldcraft applies is equally exact: write `angle "<n>"` when the
	// field is non-zero, omit it when it is zero. Both point and brush entities,
	// which is why the totals above match rather than merely bound each other.
	Angle      int32
	Spawnflags int32
	Keys       []mapfile.KeyValue
}

func (r *reader) entityData() (entityData, error) {
	classname, err := r.pchar()
	if err != nil {
		return entityData{}, err
	}
	angle, err := r.int32()
	if err != nil {
		return entityData{}, err
	}
	spawnflags, err := r.int32()
	if err != nil {
		return entityData{}, err
	}
	count, err := r.int32()
	if err != nil {
		return entityData{}, err
	}
	if count < 0 {
		return entityData{}, r.fail(fmt.Sprintf("negative key/value count %d", count))
	}

	keys := make([]mapfile.KeyValue, 0, count)
	for index := int32(0); index < count; index++ {
		key, err := r.pchar()
		if err != nil {
			return entityData{}, err
		}
		value, err := r.pchar()
		if err != nil {
			return entityData{}, err
		}
		keys = append(keys, mapfile.NewKeyValue(key, value))
	}
	if _, err := r.bytes(12); err != nil {
		return entityData{}, err
	}
	return entityData{
		Classname: classname, Angle: angle, Spawnflags: spawnflags, Keys: keys,
	}, nil
}

// NormalizeTextureName converts an RMF texture name into the spelling
// Worldcraft's own `.map` export writes.
//
// Two normalizations, both measured against the fixtures rather than assumed:
//
//   - Backslash to forward slash. RMF stores `e1u1\baselt_4`; every `.map`
//     export writes `e1u1/baselt_4`.
//   - Lower case. No `.map` export contains a single upper-case texture name,
//     while the RMF files genuinely hold both `E1U3\BFLOR3_2` and
//     `e1u3\bflor3_2` as distinct strings — 37 such collisions in m3_1 alone.
//     Without folding them the texture set cannot match the oracle.
func NormalizeTextureName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, `\`, "/"))
}

// face is one parsed RMF v1.8 face.
type face struct {
	Texture string
	ShiftX  float64
	ShiftY  float64
	Angle   float64
	ScaleX  float64
	ScaleY  float64
	// Alignment is the leading int32 of the surface block: 0 or 1 in every
	// fixture face, and absent from Worldcraft's own `.map` export, so it is
	// read to keep the offsets honest and deliberately not carried further.
	Alignment    int32
	SurfaceFlags int32
	Contents     int32
	Value        int32
	VertexCount  int
	PlanePoints  [3]mapfile.Vec3
}

// hasSurfaceProperties reports whether a face carries Quake 2 surface data
// worth writing. Worldcraft omits the trailing three `.map` fields entirely
// when all three are zero, and this is the test that reproduces that.
func (f face) hasSurfaceProperties() bool {
	return f.SurfaceFlags != 0 || f.Contents != 0 || f.Value != 0
}

// tail renders the face's Quake 2 content/surface/value fields in `.map` order,
// or nil when there are none.
//
// The `.map` order is contents, surface flags, value; the RMF order is surface
// flags, contents, value. The swap is not a guess — see the type's note above.
func (f face) tail() []mapfile.Number {
	if !f.hasSurfaceProperties() {
		return nil
	}
	return []mapfile.Number{
		mapfile.Number(f.Contents),
		mapfile.Number(f.SurfaceFlags),
		mapfile.Number(f.Value),
	}
}

func (r *reader) face() (face, error) {
	layout := r.layout
	if layout.TextureBlock == 0 {
		// Decode always sets it before a face is reached; a zero value would
		// mean a caller reached this some other way, and 1.8 is the version this
		// package writes.
		layout = layoutFor(SupportedVersion)
	}
	raw, err := r.bytes(layout.TextureBlock)
	if err != nil {
		return face{}, err
	}
	name := raw
	if end := indexByte(name, 0); end >= 0 {
		name = name[:end]
	}

	values := make([]float64, 5)
	for index := range values {
		value, err := r.float32()
		if err != nil {
			return face{}, err
		}
		values[index] = float64(value)
	}
	// The sixteen bytes after the five projection floats are four int32s, not
	// padding: alignment, surface flags, contents, value. Established by
	// comparing every face in all four fixtures against its paired Worldcraft
	// `.map` export — the flags/contents/value multisets agree exactly once the
	// faces Worldcraft drops as degenerate are accounted for, and the sky
	// (0x84), detail (CONTENTS_DETAIL) and light-value (SURF_LIGHT with a
	// non-zero value) faces line up individually. Reading them as opaque
	// padding, which this did until now, silently discarded every Quake 2
	// surface property on the way to `.map` and APMap.
	//
	// In 1.6 there is ONE: alignment. The other three are Quake 2 surface
	// properties Worldcraft had not added yet, so they are absent rather than
	// zero, and the slice is left at its zero value for them — which makes
	// hasSurfaceProperties report no tail, exactly as it does for a 1.8 face
	// that carries three zeros. Nothing is invented for a field the file does
	// not have.
	surface := make([]int32, 4)
	for index := 0; index < layout.SurfaceInts; index++ {
		value, err := r.int32()
		if err != nil {
			return face{}, err
		}
		surface[index] = value
	}

	vertexCount, err := r.int32()
	if err != nil {
		return face{}, err
	}
	if vertexCount < 0 {
		return face{}, r.fail(fmt.Sprintf("negative vertex count %d", vertexCount))
	}
	// The polygon vertices are Worldcraft's own cached data, not authoritative
	// input: APMap represents a face by its plane, exactly as `.map` does. They
	// are skipped rather than read, but the byte offsets after them depend on
	// the count, so it still has to be honoured exactly.
	if _, err := r.bytes(int(vertexCount) * 12); err != nil {
		return face{}, err
	}

	var plane [3]mapfile.Vec3
	for index := range plane {
		point, err := r.vector()
		if err != nil {
			return face{}, err
		}
		plane[index] = point
	}

	return face{
		Texture: NormalizeTextureName(string(name)),
		// The five projection floats are rotation first, then the two shifts —
		// *not* the shift-shift-rotation order a `.map` face is written in.
		//
		// This was read in `.map` order until the APMap-level oracle
		// compared both sides of the four paired fixtures. The
		// `.map` order agrees with Worldcraft's own export on 4540 of dm_1's
		// 7498 faces; this order agrees on all 7498, and on every face of
		// ter_1 and dm_1, all but one of ware_1's 13816, and m3_1's modulo its
		// documented surplus brush. The residue is float32 rounding, not
		// ordering.
		//
		// The two agree exactly when rotation and shift-x happen to be equal,
		// which is why the majority of a file can look correct. A read-write-
		// read round trip cannot see the error at all: encode.go wrote the same
		// order back, so the mistake cancelled. Only the external oracle finds
		// it, which is the reason AGENTS.md §1 requires one.
		Angle:        values[0],
		ShiftX:       values[1],
		ShiftY:       values[2],
		ScaleX:       values[3],
		ScaleY:       values[4],
		Alignment:    surface[0],
		SurfaceFlags: surface[1],
		Contents:     surface[2],
		Value:        surface[3],
		VertexCount:  int(vertexCount),
		PlanePoints:  plane,
	}, nil
}

// collector accumulates the brushes belonging to one entity.
type collector struct {
	brushes []mapfile.Brush
}

// decoder walks the object tree.
type decoder struct {
	reader   *reader
	stats    *Stats
	entities []mapfile.Entity
}

// Decode reads an RMF v1.6 or v1.8 document into the repository's MAP model.
//
// The result is the intermediate every other consumer goes through: pass it to
// apmap.FromMapDocument for an APMap document, or to mapfile.Write for `.map`
// text. There is deliberately no parallel path.
func Decode(data []byte) (*mapfile.MapFile, *Stats, error) {
	r := &reader{data: data}

	version, err := r.float32()
	if err != nil {
		return nil, nil, err
	}
	magic, err := r.bytes(3)
	if err != nil {
		return nil, nil, err
	}
	if string(magic) != Magic {
		return nil, nil, &Error{
			Offset:  4,
			Message: fmt.Sprintf("not an RMF file: magic is %q, want %q", string(magic), Magic),
		}
	}
	declared, ok := readableVersion(version)
	if !ok {
		return nil, nil, &Error{
			Offset: 0,
			Message: fmt.Sprintf(
				"unsupported RMF version %g: this build reads %v and writes %g, and the "+
					"versions outside that set differ in the Face layout rather than merely "+
					"extending it",
				version, ReadableVersions(), SupportedVersion),
		}
	}
	r.layout = layoutFor(declared)

	visgroupCount, err := r.int32()
	if err != nil {
		return nil, nil, err
	}
	if visgroupCount < 0 {
		return nil, nil, r.fail(fmt.Sprintf("negative visgroup count %d", visgroupCount))
	}
	if _, err := r.bytes(int(visgroupCount) * visgroupRecordSize); err != nil {
		return nil, nil, err
	}

	stats := &Stats{Version: declared, Visgroups: int(visgroupCount)}
	d := &decoder{reader: r, stats: stats}

	world := &collector{}
	kind, worldData, err := d.object(world)
	if err != nil {
		return nil, nil, err
	}
	if kind != TypeWorld {
		return nil, nil, &Error{
			Offset:  0,
			Message: fmt.Sprintf("expected a %s root, found %q", TypeWorld, kind),
		}
	}

	// Worldspawn leads, then every entity in the order the file holds them.
	worldItems := entityItems(worldData, world.brushes)
	entities := make([]mapfile.Entity, 0, len(d.entities)+1)
	entities = append(entities, mapfile.NewEntity(worldItems))
	entities = append(entities, d.entities...)

	return mapfile.NewMapFile(entities), stats, nil
}

// DecodeToAPMap reads an RMF document and converts it to APMap, going through
// the MAP model rather than building a second, parallel conversion.
func DecodeToAPMap(data []byte, opts apmap.ConvertOptions) (*apmap.Object, *Stats, error) {
	document, stats, err := Decode(data)
	if err != nil {
		return nil, nil, err
	}
	converted, err := apmap.FromMapDocument(document, opts)
	if err != nil {
		return nil, nil, err
	}
	return converted, stats, nil
}

// object reads one object and everything below it, appending any brushes to
// parent. It returns the object's type and, for world/entity objects, its
// entity data.
func (d *decoder) object(parent *collector) (string, entityData, error) {
	r := d.reader

	kind, err := r.pchar()
	if err != nil {
		return "", entityData{}, err
	}
	if _, err := r.int32(); err != nil { // visgroup id, phase 2
		return "", entityData{}, err
	}
	if _, err := r.bytes(3); err != nil { // editor colour
		return "", entityData{}, err
	}
	childCount, err := r.int32()
	if err != nil {
		return "", entityData{}, err
	}
	if childCount < 0 {
		return "", entityData{}, r.fail(fmt.Sprintf("negative child count %d", childCount))
	}

	// An entity owns its descendants' brushes; a group is transparent and
	// passes them through to whatever encloses it.
	own := parent
	if kind == TypeEntity {
		own = &collector{}
	}
	for index := int32(0); index < childCount; index++ {
		if _, _, err := d.object(own); err != nil {
			return "", entityData{}, err
		}
	}

	switch kind {
	case TypeWorld:
		data, err := r.entityData()
		if err != nil {
			return "", entityData{}, err
		}
		pathCount, err := r.int32()
		if err != nil {
			return "", entityData{}, err
		}
		if pathCount != 0 {
			// Explicitly deferred rather than silently dropped: no fixture has
			// paths, so the Path record layout is unverified.
			return "", entityData{}, &Error{
				Offset: r.offset,
				Message: fmt.Sprintf(
					"file declares %d path(s); RMF paths are not supported and are "+
						"refused rather than dropped", pathCount),
			}
		}
		return kind, data, nil

	case TypeEntity:
		data, err := r.entityData()
		if err != nil {
			return "", entityData{}, err
		}
		if _, err := r.bytes(2); err != nil {
			return "", entityData{}, err
		}
		origin, err := r.vector()
		if err != nil {
			return "", entityData{}, err
		}
		if _, err := r.bytes(4); err != nil {
			return "", entityData{}, err
		}

		if dropEmptyEntity(own.brushes, origin) {
			d.stats.EmptyBrushEntities++
			return kind, data, nil
		}
		d.stats.Entities++
		point := len(own.brushes) == 0
		items := entityItems(data, own.brushes)
		if point {
			items = append(items, mapfile.NewKeyValue("origin", formatOrigin(origin)))
		}
		d.entities = append(d.entities, mapfile.NewEntity(items))
		return kind, data, nil

	case TypeGroup:
		// Grouping is phase 2. The header above already consumed everything a
		// group carries, and its children have been folded into the enclosing
		// collector, so nothing further is read here.
		d.stats.Groups++
		return kind, entityData{}, nil

	case TypeSolid:
		faceCount, err := r.int32()
		if err != nil {
			return "", entityData{}, err
		}
		if faceCount < 0 {
			return "", entityData{}, r.fail(fmt.Sprintf("negative face count %d", faceCount))
		}
		faces := make([]mapfile.Face, 0, faceCount)
		for index := int32(0); index < faceCount; index++ {
			parsed, err := r.face()
			if err != nil {
				return "", entityData{}, err
			}
			d.stats.Faces++
			if !definesAPlane(parsed.PlanePoints) {
				// Worldcraft drops these on export — the six such faces in
				// ware_1 are exactly the six the paired `.map` omits — and they
				// could not become a plane downstream anyway.
				d.stats.DegenerateFaces++
				continue
			}
			faces = append(faces, mapfile.NewClassicFace(
				parsed.PlanePoints[0], parsed.PlanePoints[1], parsed.PlanePoints[2],
				parsed.Texture,
				parsed.ShiftX, parsed.ShiftY, parsed.Angle, parsed.ScaleX, parsed.ScaleY,
				parsed.tail(),
			))
		}
		d.stats.Solids++
		if len(faces) == 0 {
			// Every face was degenerate, so there is no brush left to write.
			// Worldcraft emits an empty `{ }` block here; this reader emits
			// nothing, because an empty brush is not geometry and
			// mapfile rejects it on the way back in.
			d.stats.EmptySolids++
			return kind, entityData{}, nil
		}
		parent.brushes = append(parent.brushes, mapfile.NewBrush(faces))
		return kind, entityData{}, nil
	}

	return "", entityData{}, &Error{
		Offset:  r.offset,
		Message: fmt.Sprintf("unknown object type %q", kind),
	}
}

// dropEmptyEntity reports an entity that carries neither geometry nor a
// position, which Worldcraft omits from its `.map` export.
//
// The rule is derived from the fixtures, not invented. Across all four files
// every entity falls into exactly one of three groups: brush entities, which
// always have at least one solid and an origin of (0,0,0); point entities,
// which have no solids and a non-zero origin; and entities with neither — one
// in ware_1 and one in dm_1, none in m3_1 or ter_1. That is precisely the
// entity-count difference between each RMF and its `.map` sibling.
//
// An entity like this has nothing to write: no brushes to place and no origin
// to place it at.
func dropEmptyEntity(brushes []mapfile.Brush, origin mapfile.Vec3) bool {
	return len(brushes) == 0 && origin.X() == 0 && origin.Y() == 0 && origin.Z() == 0
}

// definesAPlane reports whether three points actually determine a plane.
//
// It defers to the same test the rest of the pipeline uses, so a face this
// reader accepts is a face the geometry layer can reconstruct.
func definesAPlane(points [3]mapfile.Vec3) bool {
	_, err := convex.PlaneFromPoints(
		convex.Vec3f{points[0].X(), points[0].Y(), points[0].Z()},
		convex.Vec3f{points[1].X(), points[1].Y(), points[1].Z()},
		convex.Vec3f{points[2].X(), points[2].Y(), points[2].Z()},
	)
	return err == nil
}

// entityItems assembles an entity's key/value items and its brushes.
//
// classname leads, then the synthesised `angle`, then the authored keys in file
// order, then spawnflags when it is set.
//
// RMF keeps spawnflags in a dedicated field rather than in the key list — no
// fixture entity carries a "spawnflags" key, while 52 of dm_1's entities have a
// non-zero field and its `.map` export has exactly 52 "spawnflags" keys. `angle`
// is the same kind of field and is reconstructed under the same rule: written
// when non-zero, omitted when zero. See entityData.Angle for the measurement.
//
// Its POSITION is Worldcraft's, immediately after `classname` and before
// everything else — checked against the exports rather than chosen. Nothing in
// this repository compares entity key ORDER, so this costs nothing and makes a
// `.map` produced here diff cleanly against one Worldcraft wrote.
func entityItems(data entityData, brushes []mapfile.Brush) []mapfile.EntityItem {
	items := make([]mapfile.EntityItem, 0, len(data.Keys)+len(brushes)+3)
	classname := data.Classname
	if classname == "" {
		classname = "worldspawn"
	}
	items = append(items, mapfile.NewKeyValue("classname", classname))
	if data.Angle != 0 {
		items = append(items, mapfile.NewKeyValue("angle", strconv.FormatInt(int64(data.Angle), 10)))
	}
	for _, key := range data.Keys {
		if key.Key() == "classname" {
			continue
		}
		// An authored `angle` key would be a second spelling of the field above.
		// No fixture has one — the string does not appear in any of the five
		// files read — but emitting both would produce a duplicate key, and a
		// `.map` with two `angle` values is one no consumer can resolve.
		if key.Key() == "angle" && data.Angle != 0 {
			continue
		}
		items = append(items, key)
	}
	if data.Spawnflags != 0 {
		items = append(items, mapfile.NewKeyValue("spawnflags", fmt.Sprintf("%d", data.Spawnflags)))
	}
	for _, brush := range brushes {
		items = append(items, brush)
	}
	return items
}

// formatOrigin renders a point entity's origin the way a `.map` carries it.
func formatOrigin(origin mapfile.Vec3) string {
	return mapfile.FormatNumber(origin.X()) + " " +
		mapfile.FormatNumber(origin.Y()) + " " +
		mapfile.FormatNumber(origin.Z())
}
