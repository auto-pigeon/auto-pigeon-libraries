package apmap

import (
	"strings"
	"testing"
)

// The editor (AUP) records how far it has rotated a brush as editor metadata in a namespaced brush
// extension, `auto-pigeon.orientation`. The brush's plane points are ALREADY rotated when the
// extension is written: the quaternion is a record of what was done, never an instruction.
//
// AUE does not know the namespace, and these tests pin the two things an unknowing consumer owes
// it anyway:
//
//  1. the extension — and any other unknown one beside it — survives every APMap read/write path
//     with its integers spelled exactly as they arrived;
//  2. an APMap -> `.map` export writes the stored planes and nothing else. Applying the quaternion
//     there would rotate the brush a second time.
//
// The fixture is the generated 64-unit cube the neighbouring codec tests use, with the extension
// added to its one brush. No user map is involved.

// orientationExtensions is the brush `extensions` member exactly as AUP writes it: a unit
// quaternion (x, y, z, w) scaled by 10^9 — 45 degrees about Z here — beside an unrelated
// vendor extension that must be carried just as faithfully.
const orientationExtensions = `{
  "auto-pigeon.orientation": {
    "version": 1,
    "quaternion_e9": [0, 0, 382683432, 923879533]
  },
  "vendor.note": {"keep": true}
}`

// orientationExtensionsCanonical is that member as the canonical serializer must spell it inside a
// brush (four levels of two-space indentation deep).
const orientationExtensionsCanonical = `          "extensions": {
            "auto-pigeon.orientation": {
              "version": 1,
              "quaternion_e9": [
                0,
                0,
                382683432,
                923879533
              ]
            },
            "vendor.note": {
              "keep": true
            }
          }`

// fullMapDocument is a valid current-version full-map document holding one cube.
func fullMapDocument(t *testing.T) *Object {
	t.Helper()
	return build(t, classicMap(t), func(opts *ConvertOptions) {
		opts.Role = RoleFullMap
		opts.Identity = PackageIdentity{PackageID: "test-package", Role: RoleFullMap}
	})
}

// withOrientation is fullMapDocument whose brush carries the two extensions.
func withOrientation(t *testing.T) *Object {
	t.Helper()
	document := fullMapDocument(t)
	extensions, err := DecodeJSON([]byte(orientationExtensions))
	if err != nil {
		t.Fatalf("DecodeJSON(extensions): %v", err)
	}
	brushOf(t, document, 0).Set("extensions", extensions)
	return document
}

func TestABrushOrientationExtensionIsAValidDocument(t *testing.T) {
	schema, err := LoadSchema("")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	document := withOrientation(t)

	problems, err := ValidateAgainstSchema(document, schema)
	if err != nil {
		t.Fatalf("ValidateAgainstSchema: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("schema problems: %v", problems)
	}
	if semantic := SemanticProblems(document); len(semantic) != 0 {
		t.Fatalf("semantic problems: %v", semantic)
	}
	if !IsCanonical(encodeOrFail(t, document)) {
		t.Fatalf("a document carrying the orientation extension is not canonical")
	}
}

func TestABrushOrientationExtensionSurvivesTheCanonicalRoundTrip(t *testing.T) {
	written := encodeOrFail(t, withOrientation(t))
	if !strings.Contains(written, orientationExtensionsCanonical) {
		t.Fatalf("the brush extensions were not written verbatim:\n%s", written)
	}

	// parse -> serialize: the text a consumer reads is the text it writes back.
	parsed, err := Decode(written, "orientation")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	rewritten := encodeOrFail(t, parsed)
	if rewritten != written {
		t.Fatalf("parse -> serialize changed the bytes: %s", firstDifference(rewritten, written))
	}

	// The integers are integers. A float64 pass would be free to spell 382683432 as
	// 3.82683432e+08, and SER-7's six-decimal rounding must have nothing to round.
	for _, forbidden := range []string{"e+", "E+", "382683432.", "923879533."} {
		if strings.Contains(rewritten, forbidden) {
			t.Fatalf("the rewritten document spells a number with %q:\n%s", forbidden, rewritten)
		}
	}

	extensions, ok := brushOf(t, parsed, 0).Get("extensions")
	if !ok {
		t.Fatalf("the brush extensions were dropped by the round trip")
	}
	names := extensions.(*Object).Keys()
	if len(names) != 2 || names[0] != "auto-pigeon.orientation" || names[1] != "vendor.note" {
		t.Fatalf("the brush extensions are %v, want both namespaces in their written order", names)
	}
}

// TestABrushOrientationExtensionSurvivesTheModelRoundTrip is the APMap -> model -> APMap path: the
// `.map` model has no place for an extension, so it is re-attached by structural path.
func TestABrushOrientationExtensionSurvivesTheModelRoundTrip(t *testing.T) {
	document := withOrientation(t)
	written := encodeOrFail(t, document)

	geometry, err := ToMapDocument(document)
	if err != nil {
		t.Fatalf("ToMapDocument: %v", err)
	}
	restored, err := Rebuild(document, geometry)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if got := encodeOrFail(t, restored); got != written {
		t.Fatalf("the model round trip changed the bytes: %s", firstDifference(got, written))
	}
}

// TestTheOrientationExtensionIsNeverAppliedOnExport is the no-double-transform guarantee. The
// planes are stored already rotated; the `.map` written from the document with the extension is the
// `.map` written from the same document without it.
func TestTheOrientationExtensionIsNeverAppliedOnExport(t *testing.T) {
	plain, err := ExportMapText(fullMapDocument(t))
	if err != nil {
		t.Fatalf("ExportMapText(plain): %v", err)
	}
	oriented, err := ExportMapText(withOrientation(t))
	if err != nil {
		t.Fatalf("ExportMapText(oriented): %v", err)
	}
	if oriented != plain {
		t.Fatalf("the orientation extension changed the exported .map: %s", firstDifference(oriented, plain))
	}

	// And the planes that were exported are the stored ones: the export of a document is the
	// `.map` it was imported from, the cube at its authored coordinates.
	if source := classicMap(t); oriented != source {
		t.Fatalf("the export is not the stored geometry: %s", firstDifference(oriented, source))
	}
	for _, leaked := range []string{"orientation", "quaternion", "382683432", "vendor"} {
		if strings.Contains(oriented, leaked) {
			t.Fatalf("the exported .map mentions %q:\n%s", leaked, oriented)
		}
	}
}
