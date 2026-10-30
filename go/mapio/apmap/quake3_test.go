package apmap

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// APMap 1.5's Quake III surfaces: SER-7Q, and the explicit refusals every `.map` path owes a
// document holding a patch or a brush_primitives face (the Quake III writer, Q3_004, is separate:
// quake3map.go, and it never lowers through the Quake 1 model these refusals guard).
// The documents are built here from a Quake 1 import rather than read from AULIBS, so none of these
// tests skips on a machine without the contract package beside this checkout.

// quake3Point is one control point, [x, y, z, s, t], as number literals.
func quake3Point(x, y, z, s, tc string) []Value {
	return []Value{json.Number(x), json.Number(y), json.Number(z), json.Number(s), json.Number(tc)}
}

// withPatch turns a Quake 1 import into a Quake III document holding one 3x3 patch — derived
// identity included, at e0.p0 — appended to entity 0's content. It is schema- and semantically
// valid, so a refusal of it is about the patch and nothing else.
func withPatch(t *testing.T, document *Object) *Object {
	t.Helper()
	document.Set("game", "quake3")
	id, err := coreIdentity().IDFor(KindPatch, "e0.p0")
	if err != nil {
		t.Fatal(err)
	}
	record, err := coreIdentity().Record("e0.p0")
	if err != nil {
		t.Fatal(err)
	}
	patch := NewObject()
	patch.Set("kind", "patch")
	patch.Set("patch_id", id)
	patch.Set("derived_from", record)
	patch.Set("texture", "base_trim/arch")
	patch.Set("width", json.Number("3"))
	patch.Set("height", json.Number("3"))
	var columns []Value
	for c, x := range []string{"0", "16", "32"} {
		column := []Value{}
		for r, z := range []string{"64", "80", "96"} {
			s := []string{"0", "0.5", "1"}[c]
			tc := []string{"0.0078125", "0.00260417", "1"}[r]
			column = append(column, quake3Point(x, "8", z, s, tc))
		}
		columns = append(columns, column)
	}
	patch.Set("control_points", columns)
	patch.Set("tail", []Value{json.Number("0"), json.Number("0"), json.Number("0")})
	entity := firstEntity(t, document)
	content, _ := entity.Get("content")
	entity.Set("content", append(content.([]Value), patch))
	return document
}

// withBrushPrimitives turns every face of entity 0's first brush into a brushDef face, keeping
// SEM-Q-5 (one syntax per brush), and declares the document Quake III.
func withBrushPrimitives(t *testing.T, document *Object) *Object {
	t.Helper()
	document.Set("game", "quake3")
	for _, face := range facesOf(t, brushOf(t, document, 0)) {
		projection := NewObject()
		projection.Set("mode", "brush_primitives")
		projection.Set("matrix", []Value{
			[]Value{json.Number("0.0078125"), json.Number("0"), json.Number("0.00260417")},
			[]Value{json.Number("0"), json.Number("0.0078125"), json.Number("0")},
		})
		face.Set("projection", projection)
	}
	return document
}

func firstEntity(t *testing.T, document *Object) *Object {
	t.Helper()
	entities, _ := document.Get("entities")
	entity, ok := entities.([]Value)[0].(*Object)
	if !ok {
		t.Fatal("entity 0 is not an object")
	}
	return entity
}

func mustBeValid(t *testing.T, document *Object) {
	t.Helper()
	if err := ValidateDocument(document, ValidateOptions{Source: "quake3"}); err != nil {
		t.Fatalf("the constructed Quake III document is not valid: %v", err)
	}
}

// requireQuake3Refusal checks err is the explicit Quake III refusal and names the object.
func requireQuake3Refusal(t *testing.T, err error, objectID string) {
	t.Helper()
	var refusal *Quake3UnsupportedError
	if !errors.As(err, &refusal) {
		t.Fatalf("err = %v, want a *Quake3UnsupportedError", err)
	}
	message := err.Error()
	for _, want := range []string{objectID, "Quake III", "no place for", "apmap-to-q3map", "whole document is"} {
		if !strings.Contains(message, want) {
			t.Fatalf("the refusal does not say %q: %s", want, message)
		}
	}
	if !errors.Is(err, ErrAPMap) {
		t.Fatal("the refusal does not unwrap to ErrAPMap")
	}
}

// --- SER-7Q -----------------------------------------------------------------

func TestNormalizeExactNumberIsShortestRoundTripPositional(t *testing.T) {
	cases := map[string]string{
		"0.0078125":       "0.0078125",
		"0.00260417":      "0.00260417",
		"1e-7":            "0.0000001",
		"1.5E2":           "150",
		"2.0":             "2",
		"-0":              "0",
		"-0.0":            "0",
		"-0.125":          "-0.125",
		"131072":          "131072",
		"0.1":             "0.1",
		"1234.5678901234": "1234.5678901234",
	}
	for literal, want := range cases {
		got, err := NormalizeExactNumber(json.Number(literal))
		if err != nil {
			t.Fatalf("%s: %v", literal, err)
		}
		if string(got) != want {
			t.Errorf("SER-7Q %s = %s, want %s", literal, got, want)
		}
	}
	for _, literal := range []string{"1e400", "-1e999"} {
		if _, err := NormalizeExactNumber(json.Number(literal)); err == nil {
			t.Errorf("SER-7Q accepted the non-finite %s", literal)
		}
	}
	// What SER-7 would have done to the same two values: the reason SER-7Q exists.
	if got, _ := NormalizeNumber("0.00260417"); got != "0.002604" {
		t.Fatalf("SER-7 0.00260417 = %s", got)
	}
	if got, _ := NormalizeNumber("0.0078125"); got != "0.007812" {
		t.Fatalf("SER-7 0.0078125 = %s", got)
	}
}

// SER-7Q governs exactly a patch's control points and a brush_primitives matrix; every other
// number — the patch's own tail, a face tail, the frame, an `extensions` value shaped like a
// patch — keeps SER-7's six-place rounding.
func TestSER7QIsPathAwareAndSER7GovernsEverythingElse(t *testing.T) {
	document := withBrushPrimitives(t, withPatch(t, build(t, classicMap(t))))
	entity := firstEntity(t, document)
	content, _ := entity.Get("content")
	items := content.([]Value)
	patch := items[len(items)-1].(*Object)
	patch.Set("tail", []Value{json.Number("0.00260417"), json.Number("0"), json.Number("0")})

	decoy := NewObject()
	decoy.Set("kind", "patch")
	decoy.Set("control_points", []Value{[]Value{quake3Point("0", "0", "0", "0.00260417", "0")}})
	extensions := NewObject()
	extensions.Set("org.example.decoy", decoy)
	document.Set("extensions", extensions)

	normalizedValue, err := Normalize(document)
	if err != nil {
		t.Fatal(err)
	}
	normalized := normalizedValue.(*Object)
	normalizedItems, _ := firstEntity(t, normalized).Get("content")
	list := normalizedItems.([]Value)
	normalizedPatch := list[len(list)-1].(*Object)
	grid, _ := normalizedPatch.Get("control_points")
	if got := grid.([]Value)[0].([]Value)[1].([]Value)[4]; got != json.Number("0.00260417") {
		t.Errorf("a control point's t = %v, want 0.00260417 kept exactly", got)
	}
	tail, _ := normalizedPatch.Get("tail")
	if got := tail.([]Value)[0]; got != json.Number("0.002604") {
		t.Errorf("a patch tail = %v, want SER-7's 0.002604", got)
	}
	projection, _ := facesOf(t, brushOf(t, normalized, 0))[0].Get("projection")
	matrix, _ := projection.(*Object).Get("matrix")
	row := matrix.([]Value)[0].([]Value)
	if row[0] != json.Number("0.0078125") || row[2] != json.Number("0.00260417") {
		t.Errorf("matrix row 0 = %v, want the coefficients kept exactly", row)
	}
	normalizedExtensions, _ := normalized.Get("extensions")
	shaped, _ := normalizedExtensions.(*Object).Get("org.example.decoy")
	decoyGrid, _ := shaped.(*Object).Get("control_points")
	if got := decoyGrid.([]Value)[0].([]Value)[0].([]Value)[3]; got != json.Number("0.002604") {
		t.Errorf("an extensions value shaped like a patch = %v, want SER-7's 0.002604", got)
	}

	// And the canonical text is a fixed point.
	text := encodeOrFail(t, document)
	if !IsCanonical(text) {
		t.Fatal("the SER-7Q encoding is not a fixed point")
	}
}

// A Quake 1 document is untouched by SER-7Q: its canonical text is what SER-7 alone produced.
func TestSER7QLeavesADocumentWithoutQuake3SurfacesAlone(t *testing.T) {
	document := build(t, classicMap(t))
	if len(FindQuake3Surfaces(document)) != 0 {
		t.Fatal("a Quake 1 import reports a Quake III surface")
	}
	text := encodeOrFail(t, document)
	decoded, err := Decode(text, "q1")
	if err != nil {
		t.Fatal(err)
	}
	again := encodeOrFail(t, decoded)
	if again != text {
		t.Fatal("a Quake 1 document is not a fixed point")
	}
}

// --- identity -------------------------------------------------------------------------------

func TestAPatchHasDerivedIdentityAndAPath(t *testing.T) {
	path, err := PatchObjectPath(30, 0)
	if err != nil || path != "e30.p0" {
		t.Fatalf("PatchObjectPath = %q, %v", path, err)
	}
	for _, accepted := range []string{"e30.p0", "e0.p12"} {
		if !ObjectPathPattern.MatchString(accepted) {
			t.Errorf("%s is refused by the object-path grammar", accepted)
		}
	}
	for _, refused := range []string{"e30.p0.f0", "e30.b0.p0", "e30.p"} {
		if ObjectPathPattern.MatchString(refused) {
			t.Errorf("%s is accepted by the object-path grammar", refused)
		}
	}
	id, err := DeriveID(KindPatch, "map", strings.Repeat("a", 64), "e0.p0")
	if err != nil || !strings.HasPrefix(id, "pat_") || len(id) != 4+DigestLength {
		t.Fatalf("DeriveID(patch) = %q, %v", id, err)
	}

	// SEM-10 checks a patch id like any other.
	document := withPatch(t, build(t, classicMap(t)))
	mustBeValid(t, document)
	content, _ := firstEntity(t, document).Get("content")
	items := content.([]Value)
	items[len(items)-1].(*Object).Set("patch_id", "pat_0000000000000000")
	problems := strings.Join(IdentityProblems(document), "\n")
	if !strings.Contains(problems, "SEM-10") || !strings.Contains(problems, "pat_0000000000000000") {
		t.Fatalf("a wrong derived patch id was not reported: %q", problems)
	}
}

// --- refusals -------------------------------------------------------------------------------

func patchID(t *testing.T, document *Object) string {
	t.Helper()
	content, _ := firstEntity(t, document).Get("content")
	items := content.([]Value)
	id, _ := items[len(items)-1].(*Object).Get("patch_id")
	return id.(string)
}

func TestAPatchIsRefusedByEveryMapPathNeverDropped(t *testing.T) {
	document := withPatch(t, build(t, classicMap(t)))
	mustBeValid(t, document)
	id := patchID(t, document)

	geometry, err := ToMapDocument(document)
	requireQuake3Refusal(t, err, id)
	if geometry != nil {
		t.Fatal("ToMapDocument returned geometry alongside the refusal")
	}
	text, err := ExportMapText(document)
	requireQuake3Refusal(t, err, id)
	if text != "" {
		t.Fatal("ExportMapText returned a (truncated) map alongside the refusal")
	}
	if !strings.Contains(err.Error(), "Quake III patch "+id) {
		t.Fatalf("the refusal does not name the patch as a patch: %v", err)
	}
}

func TestABrushPrimitivesFaceIsRefusedByEveryMapPathNeverFlattened(t *testing.T) {
	document := withBrushPrimitives(t, build(t, classicMap(t)))
	mustBeValid(t, document)
	face := facesOf(t, brushOf(t, document, 0))[0]
	faceID, _ := face.Get("face_id")

	_, err := ToMapDocument(document)
	requireQuake3Refusal(t, err, faceID.(string))
	if !strings.Contains(err.Error(), "brush-primitives face") {
		t.Fatalf("the refusal does not name a brush-primitives face: %v", err)
	}
	if _, err := ExportMapText(document); err == nil {
		t.Fatal("ExportMapText exported a brush_primitives face")
	}

	// decodeFace on its own — the path any other caller would reach — never reads the matrix as a
	// classic projection with default shift, rotation and scale.
	decoded, err := decodeFace(face)
	requireQuake3Refusal(t, err, faceID.(string))
	if decoded.Texture() != "" {
		t.Fatalf("decodeFace returned a face alongside the refusal: %+v", decoded)
	}
}

func TestRebuildAndCollectReusableRefuseQuake3SurfacesRatherThanDropThem(t *testing.T) {
	for name, document := range map[string]*Object{
		"patch":            withPatch(t, build(t, classicMap(t))),
		"brush_primitives": withBrushPrimitives(t, build(t, classicMap(t))),
	} {
		t.Run(name, func(t *testing.T) {
			// The geometry a caller hands Rebuild comes from the Quake 1 source; the refusal is
			// about what the document holds, which the rebuild could not carry back.
			geometry, err := ToMapDocument(build(t, classicMap(t)))
			if err != nil {
				t.Fatal(err)
			}
			rebuilt, err := Rebuild(document, geometry)
			if rebuilt != nil {
				t.Fatal("Rebuild returned a document alongside the refusal")
			}
			var refusal *Quake3UnsupportedError
			if !errors.As(err, &refusal) {
				t.Fatalf("Rebuild err = %v, want a *Quake3UnsupportedError", err)
			}
			reused, err := CollectReusable(document)
			if reused != nil || !errors.As(err, &refusal) {
				t.Fatalf("CollectReusable = (%v, %v), want a refusal", reused, err)
			}
		})
	}
}

func TestTheRefusalCountsEverySurfaceAndNamesTheFirst(t *testing.T) {
	document := withBrushPrimitives(t, withPatch(t, build(t, classicMap(t))))
	mustBeValid(t, document)
	surfaces := FindQuake3Surfaces(document)
	faces := len(facesOf(t, brushOf(t, document, 0)))
	if len(surfaces) != faces+1 {
		t.Fatalf("found %d surfaces, want %d faces and one patch", len(surfaces), faces)
	}
	err := Quake3SurfaceRefusal(document, "test")
	if err == nil || !strings.Contains(err.Error(), "and "+strconv.Itoa(faces)+" more Quake III surface(s)") {
		t.Fatalf("refusal = %v", err)
	}
}
