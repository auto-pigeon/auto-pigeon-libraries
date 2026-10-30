package rmf

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/convex"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
)

// Writing RMF v1.8.
//
// # What a written file carries, and what it cannot
//
// This encoder is the inverse of the reader in rmf.go, and only of that. It
// writes what the reader reads: world geometry, entities, textures, face
// projections and Quake 2 surface properties. It does not write visgroups,
// groups, paths, per-object editor colours or camera data, because the reader
// does not produce them and APMap 1.0 has no field to carry them. A file that
// makes the round trip therefore comes back byte-different from the original
// even when nothing was edited — the model is the thing that round-trips, not
// the bytes, and TestOracleRMFSurvivesReEncoding asserts exactly that.
//
// Stating it the other way, because it is the part that can surprise: opening a
// written file in Worldcraft loses the visgroups and groups the author had in
// the original. That is a real limitation of going through APMap, not an
// oversight here, and it is why nothing in this repository rewrites an author's
// `.rmf` in place.
//
// # Precision
//
// RMF stores every coordinate as float32. A document that came from an `.rmf`
// re-encodes exactly, because its numbers were float32 to begin with. A
// document that came from a `.map` may not: a coordinate `.map` text can hold
// exactly, such as 0.1, is not a float32. Coordinates are written through the
// same rounding the reader applies on the way in, so the value that comes back
// is the nearest 6-decimal value to the float32, which is stable under further
// round trips.
//
// # The unknown fields
//
// Several fixed-width regions have no documented meaning. Rather than write
// zeros everywhere and hope, each is written with the value that dominates the
// four oracle fixtures, and the note at each site says what was observed. The
// two that are not simply "all zero in every fixture" are the object colour and
// the entity's two-byte field; both are called out below.

// writeVersion is the version stamp written into every file: the float32
// nearest 1.8, which is bit-identical to what all four fixtures hold.
const writeVersion float32 = SupportedVersion

// docInfo is the twenty-byte trailer, byte-identical in all four fixtures:
// "DOCINFO\0", float32 0.2 (the camera-block version), int32 -1 (no active
// camera) and int32 0 (no cameras).
//
// Written verbatim rather than assembled from its parts, because the parts are
// only an interpretation; the bytes are the observation. The reader ignores the
// trailer entirely, so this exists for Worldcraft's benefit, not ours.
var docInfo = []byte{
	'D', 'O', 'C', 'I', 'N', 'F', 'O', 0,
	0xcd, 0xcc, 0x4c, 0x3e,
	0xff, 0xff, 0xff, 0xff,
	0x00, 0x00, 0x00, 0x00,
}

// defaultColor is the per-object editor colour. White is the most common value
// in the fixtures (2072 objects) and is what Worldcraft assigns an object with
// no colour of its own. The reader discards colours, so nothing better is
// available to write.
var defaultColor = [3]byte{0xff, 0xff, 0xff}

// entityMarker is the two-byte field between an entity's key block and its
// origin. 0x0002 in 2008 of the 2307 fixture entities and 0x0000 in the rest;
// no correlation with entity kind, brush count or classname was found. The
// dominant value is written.
var entityMarker = [2]byte{0x02, 0x00}

// EncodeStats records what an encode produced. It is the writing counterpart of
// Stats and is reported rather than logged, so a caller can surface it.
type EncodeStats struct {
	Solids   int
	Entities int
	Faces    int
	// CachedWindings is the number of faces written with Worldcraft's polygon
	// cache filled in. See windings for when that is not possible.
	CachedWindings int
	// UncachedBrushes is the number of brushes whose planes did not bound a
	// solid, so their faces were written with an empty vertex cache.
	UncachedBrushes int
}

// EncodeError is a structured RMF write failure. It is separate from Error,
// which carries a byte offset that means nothing on the way out.
type EncodeError struct {
	Message string
}

func (e *EncodeError) Error() string { return "rmf: " + e.Message }

func encodeFail(format string, args ...any) error {
	return &EncodeError{Message: fmt.Sprintf(format, args...)}
}

// writer accumulates the file.
type writer struct {
	buf []byte
	ctx context.Context
}

func (w *writer) bytes(raw []byte) { w.buf = append(w.buf, raw...) }

func (w *writer) uint8(value byte) { w.buf = append(w.buf, value) }

func (w *writer) zeros(n int) {
	for i := 0; i < n; i++ {
		w.buf = append(w.buf, 0)
	}
}

func (w *writer) int32(value int32) {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], uint32(value))
	w.bytes(raw[:])
}

func (w *writer) float32(value float64) {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], math.Float32bits(float32(value)))
	w.bytes(raw[:])
}

func (w *writer) vector(v mapfile.Vec3) {
	w.float32(v.X())
	w.float32(v.Y())
	w.float32(v.Z())
}

// pchar writes a length-prefixed string.
//
// The length byte counts the trailing NUL: "CMapSolid" is nine characters and
// is written with length 10 in all four fixtures, and the reader's comment
// records that real files are inconsistent about this. Writing the counted form
// is the safe choice — a reader that ignores the NUL sees the same string
// either way, and one that expects it would be broken by the shorter form.
func (w *writer) pchar(value string) error {
	if len(value)+1 > 255 {
		return encodeFail("string is too long for RMF's one-byte length prefix (%d bytes): %q",
			len(value), value)
	}
	if strings.IndexByte(value, 0) >= 0 {
		return encodeFail("string contains a NUL byte, which RMF cannot represent: %q", value)
	}
	w.uint8(byte(len(value) + 1))
	w.bytes([]byte(value))
	w.uint8(0)
	return nil
}

// objectHeader writes the header every object shares. childCount is the number
// of child objects that will follow immediately.
func (w *writer) objectHeader(kind string, childCount int) error {
	if err := w.pchar(kind); err != nil {
		return err
	}
	w.int32(0) // visgroup id: 0 means "no visgroup", which is what is written
	w.bytes(defaultColor[:])
	w.int32(int32(childCount))
	return nil
}

// Encode writes a `.map` geometry model as an RMF v1.8 file.
//
// The document's first entity must be worldspawn, which is the shape every
// producer in this repository emits and the shape the reader itself returns.
func Encode(document *mapfile.MapFile) ([]byte, *EncodeStats, error) {
	return EncodeContext(context.Background(), document)
}

// EncodeContext is Encode, cancelled between solids with ctx's Err.
func EncodeContext(ctx context.Context, document *mapfile.MapFile) ([]byte, *EncodeStats, error) {
	if document == nil {
		return nil, nil, encodeFail("an RMF file needs a geometry document")
	}
	entities := document.Entities()
	if len(entities) == 0 {
		return nil, nil, encodeFail("an RMF file needs a worldspawn entity, but the document has none")
	}
	if classname := entityValue(entities[0], "classname"); classname != "worldspawn" {
		return nil, nil, encodeFail(
			"the first entity must be worldspawn, but its classname is %q; RMF has no way to "+
				"represent a document without one", classname)
	}

	stats := &EncodeStats{}
	w := &writer{ctx: ctx}

	w.float32(float64(writeVersion))
	w.bytes([]byte(Magic))
	// No visgroups. The reader does not produce them, so there are none to
	// write; a count of zero is what makes the world object start immediately.
	w.int32(0)

	worldBrushes := entities[0].Brushes()
	// Worldspawn's own solids come first, then one CMapEntity per remaining
	// entity. Both are direct children of the world: this encoder writes no
	// CMapGroup, so the tree is exactly two levels deep.
	if err := w.objectHeader(TypeWorld, len(worldBrushes)+len(entities)-1); err != nil {
		return nil, nil, err
	}
	for _, brush := range worldBrushes {
		if err := w.solid(brush, stats); err != nil {
			return nil, nil, err
		}
	}
	for index, entity := range entities[1:] {
		if err := w.entity(entity, stats); err != nil {
			return nil, nil, fmt.Errorf("entity %d: %w", index+1, err)
		}
	}
	if err := w.entityData(entities[0]); err != nil {
		return nil, nil, fmt.Errorf("worldspawn: %w", err)
	}
	w.int32(0) // path count; the reader refuses files that declare any

	w.bytes(docInfo)
	return w.buf, stats, nil
}

// EncodeFromAPMap converts an APMap document to RMF, going through the MAP
// model rather than building a second, parallel conversion — the same shape as
// DecodeToAPMap, in the other direction.
func EncodeFromAPMap(document apmap.Value) ([]byte, *EncodeStats, error) {
	return EncodeFromAPMapContext(context.Background(), document)
}

// EncodeFromAPMapContext is EncodeFromAPMap, cancelled between entities and solids with ctx's Err.
func EncodeFromAPMapContext(ctx context.Context, document apmap.Value) ([]byte, *EncodeStats, error) {
	geometry, err := apmap.ToMapDocumentContext(ctx, document)
	if err != nil {
		return nil, nil, err
	}
	return EncodeContext(ctx, geometry)
}

// entityValue returns an entity's first value for key, or "".
func entityValue(entity mapfile.Entity, key string) string {
	for _, property := range entity.Properties() {
		if property.Key() == key {
			return property.Value()
		}
	}
	return ""
}

// entityData writes the block CMapWorld and CMapEntity both carry.
//
// classname, angle, spawnflags and origin are lifted out of the key list into
// the dedicated fields RMF has for them, which is the exact inverse of what
// entityItems does on the way in. Leaving them in the list as well would make
// them appear twice after a round trip.
func (w *writer) entityData(entity mapfile.Entity) error {
	classname := entityValue(entity, "classname")
	if classname == "" {
		classname = "worldspawn"
	}
	if err := w.pchar(classname); err != nil {
		return err
	}
	// The angle field, four bytes, immediately after the classname.
	//
	// It was written as four zeros here and described as "unknown purpose,
	// non-zero in 315 of the 2311 fixture entities with no discernible pattern".
	// The pattern is that they are angles: entityData.Angle on the reading side
	// records how that was established, and 315 is close to the 315 non-zero
	// values the four fixtures hold between them.
	//
	// Writing it back is what makes the round trip lossless. Before this an
	// `angle` key survived read-write-read only because the reader did not
	// produce one either, so the loss cancelled out and no test could see it.
	angle, err := entityAngle(entity)
	if err != nil {
		return err
	}
	w.int32(angle)

	spawnflags, err := entitySpawnflags(entity)
	if err != nil {
		return err
	}

	w.int32(spawnflags)

	keys := writableKeys(entity)
	w.int32(int32(len(keys)))
	for _, property := range keys {
		if err := w.pchar(property.Key()); err != nil {
			return err
		}
		if err := w.pchar(property.Value()); err != nil {
			return err
		}
	}
	// Twelve bytes, zero in all 2311 fixture entities without exception.
	w.zeros(12)
	return nil
}

// entityAngle reads the angle key back into RMF's dedicated int32.
//
// Refused rather than truncated or rounded, for spawnflags' reason: a `.map`
// may carry `angle "90.5"` or `angle "up"`, and RMF has one signed 32-bit
// integer for it. Writing 90 for 90.5, or 0 for "up", would produce a file that
// looks like it round-tripped and points the entity somewhere else.
func entityAngle(entity mapfile.Entity) (int32, error) {
	raw := strings.TrimSpace(entityValue(entity, "angle"))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, encodeFail("angle %q cannot be written to RMF's 32-bit field: %v", raw, err)
	}

	return int32(value), nil
}

// entitySpawnflags reads the spawnflags key back into RMF's dedicated int32.
//
// A value `.map` holds as text but RMF's int32 cannot is an error rather than a
// silent truncation: spawnflags is a bit field, and a truncated one is a
// different entity, not an approximate one.
func entitySpawnflags(entity mapfile.Entity) (int32, error) {
	raw := entityValue(entity, "spawnflags")
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0, encodeFail("spawnflags %q cannot be written to RMF's 32-bit field: %v", raw, err)
	}
	return int32(value), nil
}

// writableKeys is an entity's properties minus the four RMF stores elsewhere.
//
// origin is dropped only for a point entity, mirroring the reader: a brush
// entity that genuinely carries an "origin" key — a rotating door's hinge, for
// instance — keeps it, because RMF's origin field for such an entity holds
// (0,0,0) and dropping the key would move the door.
func writableKeys(entity mapfile.Entity) []mapfile.KeyValue {
	point := len(entity.Brushes()) == 0
	properties := entity.Properties()
	keys := make([]mapfile.KeyValue, 0, len(properties))
	for _, property := range properties {
		switch property.Key() {
		case "classname", "spawnflags", "angle":
			continue
		case "origin":
			if point {
				continue
			}
		}
		keys = append(keys, property)
	}
	return keys
}

// entity writes one CMapEntity and its solids.
func (w *writer) entity(entity mapfile.Entity, stats *EncodeStats) error {
	brushes := entity.Brushes()
	if err := w.objectHeader(TypeEntity, len(brushes)); err != nil {
		return err
	}
	for _, brush := range brushes {
		if err := w.solid(brush, stats); err != nil {
			return err
		}
	}
	if err := w.entityData(entity); err != nil {
		return err
	}
	w.bytes(entityMarker[:])

	origin, err := entityOrigin(entity)
	if err != nil {
		return err
	}
	w.vector(origin)
	// Four bytes, zero in all 2307 fixture entities without exception.
	w.zeros(4)

	stats.Entities++
	return nil
}

// entityOrigin recovers a point entity's position from its origin key.
//
// A brush entity's origin field is (0,0,0) in every fixture entity that has
// solids, and the reader relies on that, so it is written as zero here rather
// than derived from the geometry's bounds.
func entityOrigin(entity mapfile.Entity) (mapfile.Vec3, error) {
	if len(entity.Brushes()) > 0 {
		return mapfile.NewVec3(0, 0, 0), nil
	}
	raw := strings.Fields(entityValue(entity, "origin"))
	if len(raw) == 0 {
		// An entity with neither brushes nor an origin is exactly what
		// dropEmptyEntity discards on the way in, so it cannot come back out
		// of a decode. It can still be hand-authored, and writing it at the
		// world origin is better than refusing a document over it.
		return mapfile.NewVec3(0, 0, 0), nil
	}
	if len(raw) != 3 {
		return mapfile.Vec3{}, encodeFail("origin %q does not have three components",
			entityValue(entity, "origin"))
	}
	var parsed [3]float64
	for index, field := range raw {
		value, err := strconv.ParseFloat(field, 64)
		if err != nil {
			return mapfile.Vec3{}, encodeFail("origin component %q is not a number", field)
		}
		parsed[index] = roundCoordinate(value)
	}
	return mapfile.NewVec3(parsed[0], parsed[1], parsed[2]), nil
}

// solid writes one CMapSolid and its faces.
func (w *writer) solid(brush mapfile.Brush, stats *EncodeStats) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	faces := brush.Faces()
	if len(faces) == 0 {
		// mapfile refuses to build one of these, so it should be
		// unreachable; refused rather than written, because Worldcraft's empty
		// `{ }` block is the thing the reader already declines to produce.
		return encodeFail("a solid must have at least one face")
	}
	if err := w.objectHeader(TypeSolid, 0); err != nil {
		return err
	}
	w.int32(int32(len(faces)))

	cached := windings(faces)
	if cached == nil {
		stats.UncachedBrushes++
	}
	for index, face := range faces {
		var polygon []convex.Vec3f
		if cached != nil {
			polygon = cached[index]
		}
		if err := w.face(face, polygon); err != nil {
			return fmt.Errorf("face %d: %w", index, err)
		}
		if len(polygon) > 0 {
			stats.CachedWindings++
		}
		stats.Faces++
	}
	stats.Solids++
	return nil
}

// windings reconstructs Worldcraft's per-face polygon cache from the brush's
// planes, returning one polygon per face in face order.
//
// # Why bother, when the reader skips these
//
// The vertices are derived data — the plane is authoritative, and rmf.go says
// so — but they are not optional decoration in the file. Worldcraft stores them
// for every face of every solid in all four fixtures, and a file whose caches
// were all empty is a file no observed RMF resembles. Since the repository
// already has an exact plane-clipping solid reconstructor, and it is the same
// one that decides whether a face is degenerate on the way in, writing the real
// polygon costs one call and removes a whole class of "does Worldcraft cope
// with this" that could not be tested here.
//
// nil means the brush's planes did not bound a solid. That is not fatal: the
// faces are written with an empty cache, the file still describes the same
// geometry, and the count surfaces in EncodeStats.UncachedBrushes rather than
// failing a conversion over a brush the source file was happy to hold.
func windings(faces []mapfile.Face) [][]convex.Vec3f {
	if len(faces) < 4 {
		return nil
	}
	planes := make([]convex.Plane, 0, len(faces))
	for _, face := range faces {
		points := face.Points()
		plane, err := convex.PlaneFromPoints(
			convex.Vec3f{points[0].X(), points[0].Y(), points[0].Z()},
			convex.Vec3f{points[1].X(), points[1].Y(), points[1].Z()},
			convex.Vec3f{points[2].X(), points[2].Y(), points[2].Z()},
		)
		if err != nil {
			return nil
		}
		planes = append(planes, plane)
	}
	solid, planeToPolygon, err := convex.BuildPolyhedron(planes)
	if err != nil {
		return nil
	}
	out := make([][]convex.Vec3f, len(faces))
	for index, polygon := range planeToPolygon {
		if polygon >= 0 {
			out[index] = solid.Polygons[polygon]
		}
	}
	return out
}

// textureNameBytes is the fixed-width texture-name field: 256 bytes of name
// followed by four the reader treats as part of the same block.
const textureNameBytes = 260

// face writes one face record.
func (w *writer) face(face mapfile.Face, polygon []convex.Vec3f) error {
	if face.TextureMode() == mapfile.ModeValve220 {
		// RMF v1.8's face has five projection floats and nowhere to put a
		// texture axis. Silently collapsing a Valve 220 face to shift/rotate/
		// scale would move every texture on it, so the conversion stops instead
		// and says which face and why.
		return encodeFail(
			"face %q uses Valve 220 texture axes, which RMF v1.8 cannot represent; "+
				"convert the document to the classic texture dialect first", face.Texture())
	}

	name := face.Texture()
	if len(name)+1 > textureNameBytes {
		return encodeFail("texture name is too long for RMF's %d-byte field: %q",
			textureNameBytes, name)
	}
	raw := make([]byte, textureNameBytes)
	copy(raw, name)
	w.bytes(raw)

	// Rotation first, then the two shifts — RMF's order, not `.map`'s. See the
	// note on the reader in rmf.go; writing these in `.map` order produced a
	// file that this package read back identically and that Worldcraft would
	// have read wrong.
	w.float32(face.Rotation())
	w.float32(face.XOffset())
	w.float32(face.YOffset())
	w.float32(face.XScale())
	w.float32(face.YScale())

	surface, err := surfaceBlock(face)
	if err != nil {
		return err
	}
	for _, value := range surface {
		w.int32(value)
	}

	w.int32(int32(len(polygon)))
	for _, point := range polygon {
		w.float32(roundCoordinate(point[0]))
		w.float32(roundCoordinate(point[1]))
		w.float32(roundCoordinate(point[2]))
	}

	for _, point := range face.Points() {
		w.vector(point)
	}
	return nil
}

// surfaceBlock renders a face's Quake 2 properties into RMF's four int32s:
// alignment, surface flags, contents, value.
//
// The `.map` tail is (contents, flags, value) and RMF's order is (flags,
// contents, value); see the note on the face type in rmf.go for how that was
// established. The leading alignment field is written as zero — it is 0 for
// three quarters of the fixture faces, it has no `.map` representation, and so
// nothing that reaches here could have carried a value for it.
func surfaceBlock(face mapfile.Face) ([4]int32, error) {
	var block [4]int32
	tail := face.ExtraValues()
	if len(tail) == 0 {
		return block, nil
	}
	if len(tail) != 3 {
		return block, encodeFail(
			"face %q has %d trailing value(s); RMF's surface block holds exactly the "+
				"three Quake 2 fields (contents, flags, value)", face.Texture(), len(tail))
	}
	contents, err := exactInt32(tail[0])
	if err != nil {
		return block, encodeFail("face %q contents: %v", face.Texture(), err)
	}
	flags, err := exactInt32(tail[1])
	if err != nil {
		return block, encodeFail("face %q surface flags: %v", face.Texture(), err)
	}
	value, err := exactInt32(tail[2])
	if err != nil {
		return block, encodeFail("face %q surface value: %v", face.Texture(), err)
	}

	block[0] = 0 // alignment
	block[1] = flags
	block[2] = contents
	block[3] = value
	return block, nil
}

// exactInt32 converts a `.map` number to the int32 RMF stores, refusing
// anything that would not survive. Contents and flags are bit fields and value
// is an integer parameter; none of the three has a meaningful fractional or
// out-of-range form, and rounding one silently would change what the surface
// does.
func exactInt32(value mapfile.Number) (int32, error) {
	if value != math.Trunc(value) || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("surface value %s is not an integer", mapfile.FormatNumber(value))
	}
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, fmt.Errorf("surface value %s does not fit RMF's 32-bit field",
			mapfile.FormatNumber(value))
	}
	return int32(value), nil
}
