package rmf

import (
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
)

// boxBrush is a unit test brush with a distinguishable texture.
func boxBrush(t *testing.T, texture string) mapfile.Brush {
	t.Helper()
	brush, err := mapfile.MakeBoxBrush(mapfile.Point{0, 0, 0}, mapfile.Point{64, 64, 16}, texture)
	if err != nil {
		t.Fatalf("MakeBoxBrush: %v", err)
	}
	return brush
}

// encodeDecode is the operation every test here is really about.
func encodeDecode(t *testing.T, document *mapfile.MapFile) *mapfile.MapFile {
	t.Helper()
	data, _, err := Encode(document)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, _, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode of encoded output: %v", err)
	}
	return decoded
}

func TestEncodeRoundTripsWorldspawnGeometry(t *testing.T) {
	original := mapfile.NewMapFile([]mapfile.Entity{
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "worldspawn"),
			mapfile.NewKeyValue("wad", "e1u1.wad"),
			boxBrush(t, "e1u1/baselt_4"),
		}),
	})

	decoded := encodeDecode(t, original)
	if !decoded.Equal(original) {
		t.Errorf("round trip changed the document\n got: %s\nwant: %s",
			mustWrite(t, decoded), mustWrite(t, original))
	}
}

func TestEncodeRoundTripsPointAndBrushEntities(t *testing.T) {
	original := mapfile.NewMapFile([]mapfile.Entity{
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "worldspawn"),
			boxBrush(t, "e1u1/baselt_4"),
		}),
		// A point entity: its origin must survive the trip through RMF's
		// dedicated origin field and come back as a key.
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "info_player_start"),
			mapfile.NewKeyValue("angle", "90"),
			mapfile.NewKeyValue("spawnflags", "3"),
			mapfile.NewKeyValue("origin", "32 32 24"),
		}),
		// A brush entity: it has no origin key, and must not acquire one.
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "func_door"),
			mapfile.NewKeyValue("targetname", "d1"),
			boxBrush(t, "e1u1/door01_1"),
		}),
	})

	decoded := encodeDecode(t, original)
	if !decoded.Equal(original) {
		t.Errorf("round trip changed the document\n got: %s\nwant: %s",
			mustWrite(t, decoded), mustWrite(t, original))
	}
}

// TestEncodeRoundTripsQuake2SurfaceProperties is the regression guard for the
// half of the format that was being dropped entirely until the surface block
// was identified. Both directions have to agree on the field order, and a swap
// between contents and flags is exactly what this catches.
func TestEncodeRoundTripsQuake2SurfaceProperties(t *testing.T) {
	brush := boxBrush(t, "e1u1/sky1")
	faces := brush.Faces()
	rebuilt := make([]mapfile.Face, 0, len(faces))
	for index, face := range faces {
		points := face.Points()
		var tail []mapfile.Number
		switch index {
		case 0:
			tail = []mapfile.Number{0, 132, 0} // SURF_SKY|SURF_NODRAW
		case 1:
			tail = []mapfile.Number{134217728, 0, 0} // CONTENTS_DETAIL
		case 2:
			tail = []mapfile.Number{0, 1, 10000} // SURF_LIGHT with a value
		}
		rebuilt = append(rebuilt, mapfile.NewClassicFace(
			points[0], points[1], points[2], face.Texture(),
			face.XOffset(), face.YOffset(), face.Rotation(),
			face.XScale(), face.YScale(), tail,
		))
	}

	original := mapfile.NewMapFile([]mapfile.Entity{
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "worldspawn"),
			mapfile.NewBrush(rebuilt),
		}),
	})

	decoded := encodeDecode(t, original)
	if !decoded.Equal(original) {
		t.Errorf("surface properties did not survive the round trip\n got: %s\nwant: %s",
			mustWrite(t, decoded), mustWrite(t, original))
	}
}

func TestEncodeWritesTheFixtureHeaderAndTrailer(t *testing.T) {
	document := mapfile.NewMapFile([]mapfile.Entity{
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "worldspawn"),
			boxBrush(t, "t"),
		}),
	})
	data, _, err := Encode(document)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got := string(data[4:7]); got != Magic {
		t.Errorf("magic = %q, want %q", got, Magic)
	}
	if got := string(data[len(data)-len(docInfo):]); got != string(docInfo) {
		t.Errorf("trailer = %q, want the fixture DOCINFO block", got)
	}
}

// TestEncodeFillsTheWorldcraftVertexCache checks the polygon cache is real
// rather than empty: a box has six quads, so the encode must report six cached
// windings and no uncached brush.
func TestEncodeFillsTheWorldcraftVertexCache(t *testing.T) {
	document := mapfile.NewMapFile([]mapfile.Entity{
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "worldspawn"),
			boxBrush(t, "t"),
		}),
	})
	_, stats, err := Encode(document)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if stats.CachedWindings != 6 {
		t.Errorf("cached windings = %d, want 6", stats.CachedWindings)
	}
	if stats.UncachedBrushes != 0 {
		t.Errorf("uncached brushes = %d, want 0", stats.UncachedBrushes)
	}
	if stats.Solids != 1 || stats.Faces != 6 {
		t.Errorf("solids/faces = %d/%d, want 1/6", stats.Solids, stats.Faces)
	}
}

func TestEncodeRefusesValve220Faces(t *testing.T) {
	axis := mapfile.NewTextureAxis(1, 0, 0, 0)
	face := mapfile.NewValve220Face(
		mapfile.NewVec3(0, 0, 0), mapfile.NewVec3(1, 0, 0), mapfile.NewVec3(0, 1, 0),
		"e1u1/baselt_4", axis, axis, 0, 1, 1, nil,
	)
	document := mapfile.NewMapFile([]mapfile.Entity{
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "worldspawn"),
			mapfile.NewBrush([]mapfile.Face{face}),
		}),
	})
	_, _, err := Encode(document)
	if err == nil {
		t.Fatal("encoding a Valve 220 face should fail rather than drop the texture axes")
	}
	if !strings.Contains(err.Error(), "Valve 220") {
		t.Errorf("error = %v, want it to name the Valve 220 dialect", err)
	}
}

func TestEncodeRefusesDocumentsWithoutWorldspawn(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		document *mapfile.MapFile
		want     string
	}{
		{"empty", mapfile.NewMapFile(nil), "needs a worldspawn"},
		{
			"first entity is not worldspawn",
			mapfile.NewMapFile([]mapfile.Entity{
				mapfile.NewEntity([]mapfile.EntityItem{
					mapfile.NewKeyValue("classname", "info_player_start"),
					mapfile.NewKeyValue("origin", "0 0 0"),
				}),
			}),
			"must be worldspawn",
		},
		{"nil", nil, "needs a geometry document"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := Encode(testCase.document)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("error = %v, want it to contain %q", err, testCase.want)
			}
		})
	}
}

func TestEncodeRefusesUnrepresentableSurfaceValues(t *testing.T) {
	brush := boxBrush(t, "t")
	faces := brush.Faces()
	points := faces[0].Points()
	faces[0] = mapfile.NewClassicFace(
		points[0], points[1], points[2], "t", 0, 0, 0, 1, 1,
		[]mapfile.Number{0.5, 0, 0},
	)
	document := mapfile.NewMapFile([]mapfile.Entity{
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "worldspawn"),
			mapfile.NewBrush(faces),
		}),
	})
	_, _, err := Encode(document)
	if err == nil || !strings.Contains(err.Error(), "not an integer") {
		t.Fatalf("error = %v, want a refusal naming the non-integer surface value", err)
	}
}

// TestEncodeFromAPMapRoundTrip exercises the direction the CLI and API actually
// expose: APMap in, RMF out, and back to the same geometry.
func TestEncodeFromAPMapRoundTrip(t *testing.T) {
	original := mapfile.NewMapFile([]mapfile.Entity{
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "worldspawn"),
			boxBrush(t, "e1u1/baselt_4"),
		}),
		mapfile.NewEntity([]mapfile.EntityItem{
			mapfile.NewKeyValue("classname", "info_player_start"),
			mapfile.NewKeyValue("origin", "32 32 24"),
		}),
	})

	document, err := apmap.FromMapDocument(original, apmap.ConvertOptions{
		Identity: &apmap.SourceMapIdentity{
			SourceMap:    "round-trip.map",
			SourceSHA256: strings.Repeat("a", 64),
		},
		Game: "quake2",
	})
	if err != nil {
		t.Fatalf("FromMapDocument: %v", err)
	}

	data, _, err := EncodeFromAPMap(document)
	if err != nil {
		t.Fatalf("EncodeFromAPMap: %v", err)
	}
	decoded, _, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !decoded.Equal(original) {
		t.Errorf("APMap round trip changed the document\n got: %s\nwant: %s",
			mustWrite(t, decoded), mustWrite(t, original))
	}
}

// TestCoordinateNoiseNeverRendersAsAnExponent is the regression guard for the
// defect that made a converted ter_1 unparseable: a coordinate small enough to
// be float32 noise was written in scientific notation, which no `.map` parser
// accepts — including this repository's own.
func TestCoordinateNoiseNeverRendersAsAnExponent(t *testing.T) {
	for _, value := range []float64{
		2e-06, -2e-06, 1e-13, -1.1368683772161603e-13, 9.9e-5, -9.9e-5, 0, -0.0,
	} {
		rounded := roundCoordinate(value)
		rendered := mapfile.FormatNumber(rounded)
		if strings.ContainsAny(rendered, "eE") {
			t.Errorf("roundCoordinate(%g) rendered as %q, which no .map parser accepts",
				value, rendered)
		}
		if rendered == "-0" {
			t.Errorf("roundCoordinate(%g) rendered as -0", value)
		}
	}

	// The floor must not swallow anything a mapper could have authored. A
	// Worldcraft grid is whole units; these are far below it and still kept.
	for _, value := range []float64{0.001, -0.001, 0.125, 1, -8192} {
		if got := roundCoordinate(value); got != value {
			t.Errorf("roundCoordinate(%g) = %g, want it unchanged", value, got)
		}
	}
}

func mustWrite(t *testing.T, document *mapfile.MapFile) string {
	t.Helper()
	text, err := mapfile.Write(document)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	return text
}
