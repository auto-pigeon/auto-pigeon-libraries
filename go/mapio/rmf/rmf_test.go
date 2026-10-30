package rmf

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
)

// The Worldcraft export comparison over real human-authored `.rmf` files is corpus material and stays
// with the corpus owner; everything here is hermetic.

// ── malformed input ─────────────────────────────────────────────────────────

// minimalRMF is a complete empty-world RMF assembled in memory. Parser unit
// tests mutate this small hermetic input.
func minimalRMF() []byte {
	data := make([]byte, 0, 64)
	data = binary.LittleEndian.AppendUint32(data, math.Float32bits(SupportedVersion))
	data = append(data, Magic...)
	data = binary.LittleEndian.AppendUint32(data, 0) // visgroups
	data = appendPChar(data, TypeWorld)
	data = binary.LittleEndian.AppendUint32(data, 0) // visgroup id
	data = append(data, 0, 0, 0)                     // editor colour
	data = binary.LittleEndian.AppendUint32(data, 0) // children
	data = appendPChar(data, "worldspawn")
	data = binary.LittleEndian.AppendUint32(data, 0) // entity-data padding
	data = binary.LittleEndian.AppendUint32(data, 0) // spawnflags
	data = binary.LittleEndian.AppendUint32(data, 0) // key/value pairs
	data = append(data, make([]byte, 12)...)
	data = binary.LittleEndian.AppendUint32(data, 0) // paths
	return data
}

func appendPChar(data []byte, value string) []byte {
	data = append(data, byte(len(value)))
	return append(data, value...)
}

func TestRejectsBadMagic(t *testing.T) {
	data := minimalRMF()
	copy(data[4:7], []byte("XXX"))
	_, _, err := Decode(data)
	if err == nil {
		t.Fatal("expected an error for bad magic")
	}
	if !strings.Contains(err.Error(), "not an RMF file") {
		t.Errorf("error = %v, want the magic message", err)
	}
}

func TestRejectsUnsupportedVersion(t *testing.T) {
	data := minimalRMF()
	// 2.2 is a real RMF version whose Face layout differs; it must be refused
	// rather than parsed with the 1.8 layout.
	copy(data[0:4], []byte{0x9a, 0x99, 0x0c, 0x40}) // float32 2.2
	_, _, err := Decode(data)
	if err == nil {
		t.Fatal("expected an error for an unsupported version")
	}
	if !strings.Contains(err.Error(), "unsupported RMF version") {
		t.Errorf("error = %v, want the version message", err)
	}
}

func TestRejectsTruncatedFile(t *testing.T) {
	full := minimalRMF()
	// Several truncation points: inside the header, inside the visgroup block,
	// and partway through the object tree.
	for _, size := range []int{0, 3, 6, 10, len(full) / 2, len(full) - 1} {
		data := append([]byte(nil), full[:size]...)
		_, _, err := Decode(data)
		if err == nil {
			t.Errorf("truncating to %d bytes should fail", size)
			continue
		}
		structured, ok := err.(*Error)
		if !ok {
			t.Errorf("truncating to %d bytes gave %T, want *rmf.Error", size, err)
			continue
		}
		if structured.Offset < 0 || structured.Offset > size {
			t.Errorf("truncating to %d bytes reported offset %d", size, structured.Offset)
		}
	}
}

// TestTruncationNeverPanics runs the reader over every prefix of a minimal RMF.
// A binary reader that indexes without bounds checks fails here.
func TestTruncationNeverPanics(t *testing.T) {
	full := minimalRMF()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("Decode panicked on a truncated file: %v", recovered)
		}
	}()
	for size := 0; size < len(full); size++ {
		if _, _, err := Decode(full[:size]); err == nil && size < len(full) {
			t.Fatalf("prefix of %d bytes decoded without error", size)
		}
	}
}

func TestRejectsNonzeroPathCount(t *testing.T) {
	data := minimalRMF()
	if _, _, err := Decode(data); err != nil {
		t.Fatalf("the unpatched fixture must decode: %v", err)
	}

	patched := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(patched[len(patched)-4:], 3)

	_, _, err := Decode(patched)
	if err == nil {
		t.Fatal("expected an error for a nonzero path count")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Errorf("error = %v, want the path message", err)
	}
}

func TestNormalizeTextureName(t *testing.T) {
	cases := map[string]string{
		`e1u1\baselt_4`: "e1u1/baselt_4",
		`E1U3\BFLOR3_2`: "e1u3/bflor3_2",
		"e1u1/box1_1":   "e1u1/box1_1",
		"":              "",
		`mapper\37'2`:   "mapper/37'2",
	}
	for input, want := range cases {
		if got := NormalizeTextureName(input); got != want {
			t.Errorf("NormalizeTextureName(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestEmptyEntityRuleIsNarrow guards the drop rule against over-reach: it must
// remove only entities with neither brushes nor a position, never a point
// entity.
func TestEmptyEntityRuleIsNarrow(t *testing.T) {
	origin := mapfile.NewVec3(0, 0, 0)
	placed := mapfile.NewVec3(0, 0, 8)
	brush, err := mapfile.MakeBoxBrush(mapfile.Point{0, 0, 0}, mapfile.Point{8, 8, 8}, "t")
	if err != nil {
		t.Fatalf("MakeBoxBrush: %v", err)
	}

	if !dropEmptyEntity(nil, origin) {
		t.Error("an entity with no brushes and no origin should be dropped")
	}
	if dropEmptyEntity(nil, placed) {
		t.Error("a point entity must never be dropped")
	}
	if dropEmptyEntity([]mapfile.Brush{brush}, origin) {
		t.Error("a brush entity must never be dropped")
	}
	if dropEmptyEntity([]mapfile.Brush{brush}, placed) {
		t.Error("an entity with brushes and an origin must never be dropped")
	}
}
