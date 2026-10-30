package rmf

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
)

// RMF v1.6, and the Worldcraft-synthesized `angle`.
//
// Two fixtures, deliberately of different kinds. The one below is assembled byte by byte in this
// file — minimal, independently created, and readable as a specification of the layout being
// claimed. The one at the bottom is the real 1.6 document in the corpus, read when it is reachable,
// because a synthetic file only ever proves that the reader agrees with the test's own idea of the
// format. Neither is committed as a binary blob.

// legacyBuilder assembles an RMF file of a chosen version.
//
// It is a specification written as code: every call below names the field it writes, so the layout
// this package claims for 1.6 can be checked by reading rather than by hex-dumping.
type legacyBuilder struct {
	data   []byte
	layout faceLayout
}

func newLegacyBuilder(version float64) *legacyBuilder {
	b := &legacyBuilder{layout: layoutFor(version)}
	b.data = binary.LittleEndian.AppendUint32(b.data, math.Float32bits(float32(version)))
	b.data = append(b.data, Magic...)
	b.i32(0) // visgroup count

	return b
}

func (b *legacyBuilder) i32(value int32) {
	b.data = binary.LittleEndian.AppendUint32(b.data, uint32(value))
}

func (b *legacyBuilder) f32(value float32) {
	b.data = binary.LittleEndian.AppendUint32(b.data, math.Float32bits(value))
}

func (b *legacyBuilder) pchar(value string) {
	b.data = append(b.data, byte(len(value)+1))
	b.data = append(b.data, value...)
	b.data = append(b.data, 0)
}

func (b *legacyBuilder) zeros(n int) { b.data = append(b.data, make([]byte, n)...) }

// object header: type, visgroup id, editor colour, child count.
func (b *legacyBuilder) header(kind string, children int32) {
	b.pchar(kind)
	b.i32(0)
	b.data = append(b.data, 0xdc, 0xdc, 0xdc)
	b.i32(children)
}

// entityData: classname, ANGLE, spawnflags, key/value pairs, twelve zero bytes.
func (b *legacyBuilder) entityData(classname string, angle, spawnflags int32, keys ...[2]string) {
	b.pchar(classname)
	b.i32(angle)
	b.i32(spawnflags)
	b.i32(int32(len(keys)))
	for _, pair := range keys {
		b.pchar(pair[0])
		b.pchar(pair[1])
	}
	b.zeros(12)
}

// face: texture block, five projection floats, the version's trailing int32s, vertex count and
// cached polygon, then the three plane points.
func (b *legacyBuilder) face(texture string, plane [3]mapfile.Vec3) {
	block := make([]byte, b.layout.TextureBlock)
	copy(block, texture)
	b.data = append(b.data, block...)
	for _, value := range []float32{0, -32, -32, 1, 1} { // rotation, shifts, scales
		b.f32(value)
	}
	for i := 0; i < b.layout.SurfaceInts; i++ {
		b.i32(0)
	}
	b.i32(0) // cached polygon vertex count
	for _, point := range plane {
		b.f32(float32(point.X()))
		b.f32(float32(point.Y()))
		b.f32(float32(point.Z()))
	}
}

func (b *legacyBuilder) cube(texture string) {
	b.header(TypeSolid, 0)
	b.i32(6)
	at := mapfile.NewVec3
	faces := [6][3]mapfile.Vec3{
		{at(0, 0, 0), at(64, 0, 0), at(64, 64, 0)},
		{at(0, 0, 16), at(64, 64, 16), at(64, 0, 16)},
		{at(0, 0, 0), at(0, 64, 0), at(0, 64, 16)},
		{at(64, 0, 0), at(64, 0, 16), at(64, 64, 16)},
		{at(0, 0, 0), at(0, 0, 16), at(64, 0, 16)},
		{at(0, 64, 0), at(64, 64, 16), at(0, 64, 16)},
	}
	for _, plane := range faces {
		b.face(texture, plane)
	}
}

// oneRoomAndOneAngledEntity is the whole minimal document: a world holding one cube, and one point
// entity whose angle is 90 and whose spawnflags are 3.
func oneRoomAndOneAngledEntity(version float64) []byte {
	b := newLegacyBuilder(version)
	b.header(TypeWorld, 2)
	b.cube("e1u1/metal14_1")

	b.header(TypeEntity, 0)
	b.entityData("info_player_start", 90, 3, [2]string{"targetname", "spawn"})
	b.zeros(2)
	b.f32(16)
	b.f32(32)
	b.f32(48)
	b.zeros(4)

	// The world's own trailing data comes after its children.
	b.entityData("worldspawn", 0, 0)
	b.i32(0) // paths

	return b.data
}

func TestReadsBothVersionsAndReportsWhichOne(t *testing.T) {
	for _, version := range ReadableVersions() {
		document, stats, err := Decode(oneRoomAndOneAngledEntity(version))
		if err != nil {
			t.Fatalf("RMF %g: Decode: %v", version, err)
		}
		if stats.Version != version {
			t.Errorf("RMF %g: stats.Version = %g", version, stats.Version)
		}
		if stats.Solids != 1 || stats.Faces != 6 || stats.Entities != 1 {
			t.Errorf("RMF %g: solids=%d faces=%d entities=%d, want 1/6/1",
				version, stats.Solids, stats.Faces, stats.Entities)
		}
		if document.EntityCount() != 2 || document.BrushCount() != 1 {
			t.Errorf("RMF %g: %d entities, %d brushes, want 2/1",
				version, document.EntityCount(), document.BrushCount())
		}
	}
}

// The 1.6 face carries no Quake 2 surface block at all, and the reader must produce no tail for it
// rather than three invented zeros — which is the same thing, and is asserted because the two would
// be indistinguishable if a `.map` tail of `0 0 0` were written.
func TestALegacyFaceCarriesNoQuakeIISurfaceTail(t *testing.T) {
	document, _, err := Decode(oneRoomAndOneAngledEntity(LegacyVersion))
	if err != nil {
		t.Fatal(err)
	}
	text, err := mapfile.Write(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "e1u1/metal14_1") {
			continue
		}
		// A classic Quake face line ends with the five projection numbers. A Quake 2 one has three
		// more.
		fields := strings.Fields(line)
		if len(fields) != 21 {
			t.Errorf("a 1.6 face was written with %d fields, want 21 (no Q2 tail): %s",
				len(fields), line)
		}
	}
}

func TestTheVersionRefusalNamesEveryVersionThisBuildReads(t *testing.T) {
	data := oneRoomAndOneAngledEntity(SupportedVersion)
	copy(data[0:4], []byte{0x9a, 0x99, 0x0c, 0x40}) // float32 2.2

	_, _, err := Decode(data)
	if err == nil {
		t.Fatal("RMF 2.2 was accepted")
	}
	for _, want := range []string{"unsupported RMF version", "1.6", "1.8"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
}

// ── the synthesized angle ───────────────────────────────────────────────────

func TestTheEntityAngleBecomesAnAngleKeyRightAfterClassname(t *testing.T) {
	document, _, err := Decode(oneRoomAndOneAngledEntity(SupportedVersion))
	if err != nil {
		t.Fatal(err)
	}
	entity := document.Entities()[1]
	properties := entity.Properties()
	if len(properties) < 2 {
		t.Fatalf("the entity has %d properties", len(properties))
	}
	if properties[0].Key() != "classname" {
		t.Fatalf("properties[0] = %q, want classname", properties[0].Key())
	}
	if properties[1].Key() != "angle" || properties[1].Value() != "90" {
		t.Errorf("properties[1] = %q %q, want angle 90",
			properties[1].Key(), properties[1].Value())
	}
	// And it is Worldcraft's position: before spawnflags and the authored keys.
	if properties[len(properties)-1].Key() != "origin" {
		t.Errorf("the last property is %q, want origin", properties[len(properties)-1].Key())
	}
}

func TestAZeroAngleIsOmittedRatherThanWrittenAsZero(t *testing.T) {
	b := newLegacyBuilder(SupportedVersion)
	b.header(TypeWorld, 1)
	b.header(TypeEntity, 0)
	b.entityData("light", 0, 0)
	b.zeros(2)
	b.f32(1)
	b.f32(2)
	b.f32(3)
	b.zeros(4)
	b.entityData("worldspawn", 0, 0)
	b.i32(0)

	document, _, err := Decode(b.data)
	if err != nil {
		t.Fatal(err)
	}
	for _, property := range document.Entities()[1].Properties() {
		if property.Key() == "angle" {
			t.Errorf("a zero angle was written as %q; Worldcraft omits it", property.Value())
		}
	}
}

// The round trip is what the recovery costs if it is wrong. Before the angle was read it also was
// not written, so read-write-read cancelled the loss out and no test could see it.
func TestTheAngleSurvivesAReadWriteReadRoundTrip(t *testing.T) {
	original, _, err := Decode(oneRoomAndOneAngledEntity(SupportedVersion))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, _, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode after Encode: %v", err)
	}
	if !original.Equal(back) {
		t.Error("the document changed across a write and a read")
	}
	found := false
	for _, property := range back.Entities()[1].Properties() {
		if property.Key() == "angle" && property.Value() == "90" {
			found = true
		}
	}
	if !found {
		t.Error("the angle did not survive the round trip")
	}
}

func TestAnAngleRMFCannotHoldIsRefusedRatherThanTruncated(t *testing.T) {
	document := mapfile.NewMapFile([]mapfile.Entity{
		mapfile.NewEntity([]mapfile.EntityItem{mapfile.NewKeyValue("classname", "worldspawn")}),
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "light"),
			mapfile.NewKeyValue("angle", "90.5"),
			mapfile.NewKeyValue("origin", "0 0 0"),
		}),
	})

	if _, _, err := Encode(document); err == nil {
		t.Fatal("a fractional angle was silently truncated into RMF's int32")
	} else if !strings.Contains(err.Error(), "angle") {
		t.Errorf("the refusal does not name the field: %v", err)
	}
}
