package apmap

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

// Frozen output vectors for conversion and publishing.
//
// The structural tests elsewhere in this package prove properties — member order is imposed, ids
// are edit-stable, the two formats agree by construction. These pin the exact bytes, so a change to
// canonicalisation (SER-5 member order, SER-7 number spelling), to id derivation or to the frame
// block cannot pass unnoticed just because every property still holds.
//
// Provenance: every digest below is frozen from AUE output — the SHA-256 of the canonical text this
// package produced for the named input when the vector was recorded. Nothing external produced
// them. A deliberate change to the output (a new APMap contract version, a new canonicalisation
// rule) updates the digests in the same change and says why; an unexplained mismatch is a
// regression.
//
// Re-frozen by Q3_001 for APMap 1.5. Again the only change in any written byte is `apmap_version`
// and the `$schema` URL: with those two lines of the 1.5 output relabelled "1.4", every canonical
// and published digest reproduced its 1.4-frozen value exactly, and no exported `.map` digest
// moved. SER-7Q (serialize.go) governs only patch control points and brush_primitives matrices,
// which none of these Quake 1/2 inputs has.
//
// Re-frozen by NEW_262 for APMap 1.4. The only change in any written byte is the declared contract
// version — `apmap_version` and the `$schema` URL — and every digest taken over those bytes. Proven
// differentially rather than asserted: built with a (never committed) overlay that labels the 1.4
// contract "1.3", this code reproduced every previous frozen value exactly — as it also did when
// run against the previous (1.3) embedded bundle unchanged.

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// TestConversionOfCommittedFixturesIsFrozen converts every committed `.map` fixture under
// mapfile/testdata and pins both directions: the canonical `.apmap` text and the `.map`
// text exported back from it. The fixtures cover repeated keys, point and brushless entities, a
// func_train path and a Quake II face tail — the shapes an ordering bug shows up in.
func TestConversionOfCommittedFixturesIsFrozen(t *testing.T) {
	vectors := []struct {
		fixture string
		apmap   string
		mapText string
	}{
		{"one_room.map",
			"557b820fdde49cda661d8c0d6a6f61eeec1c735b4b3478040d7f28d9c97f3035",
			"f0e518dbc17eb573e9197c0e3527e3d7cdb41c67e70fd804b9977a9b1954cd57"},
		{"door_trigger.map",
			"b67a1d093ff69eb4d77f149183331afe80cd6ff9817f69461680e8d8455d8a60",
			"fc81258655270ac459366ab9c0d2e5edcedc5c6f07a8ff6c89a245e673759424"},
		{"train_path.map",
			"b82b49f688678448b4cc5f851968178d64be87be6315279e58974f23af84bcec",
			"94d42d0c51b900f7645de1a6d6d1225eaa2ae5b71249c8b29f92780d4e803a32"},
		{"known_good/one_cube.map",
			"1f6b4d99aea1425145e5ba4e7ac78d9749ed3d0775dead329fc3edcb36b58cd2",
			"704a88634c1baf5420ef041037c4f973abf6ec362747c5c47c4ea55d492d0dcd"},
		{"q2/basic_q2.map",
			"10fba161aae2b53f10f26da79afa8e5b2df9391cf8f40cfc29800856e6dc83a1",
			"4cbd6df582cf07295aeb88bcac229d1c26107c8176ecfee618ceeed588dceaf3"},
	}

	for _, vector := range vectors {
		t.Run(vector.fixture, func(t *testing.T) {
			path := filepath.Join("..", "mapfile", "testdata", filepath.FromSlash(vector.fixture))
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			document, err := ImportMapFile(path, ConvertOptions{
				Identity:   SourceMapIdentity{SourceMap: name, SourceSHA256: strings.Repeat("a", 64)},
				Role:       RoleFullMap,
				DocumentID: name,
			})
			if err != nil {
				t.Fatalf("ImportMapFile: %v", err)
			}
			text := encodeOrFail(t, document)
			if !IsCanonical(text) {
				t.Fatal("the converted document is not canonical")
			}
			if got := sha256Hex(text); got != vector.apmap {
				t.Errorf("canonical .apmap sha256 = %s, frozen %s", got, vector.apmap)
			}

			exported, err := ExportMapText(document)
			if err != nil {
				t.Fatalf("ExportMapText: %v", err)
			}
			if got := sha256Hex(exported); got != vector.mapText {
				t.Errorf("exported .map sha256 = %s, frozen %s", got, vector.mapText)
			}

			// And the export must rebuild to the same canonical bytes: the round trip is the
			// guarantee the model layer exists for.
			geometry, err := ToMapDocument(document)
			if err != nil {
				t.Fatalf("ToMapDocument: %v", err)
			}
			rebuilt, err := Rebuild(document, geometry)
			if err != nil {
				t.Fatalf("Rebuild: %v", err)
			}
			if again := encodeOrFail(t, rebuilt); again != text {
				t.Fatalf("rebuild is not stable:\n%s", diffFirstLine(again, text))
			}
		})
	}
}
