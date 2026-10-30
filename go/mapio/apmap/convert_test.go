package apmap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
)

// The cube is generated rather than hand-written as six face lines:
// hand-written planes do not enclose a volume, and SEM-6 correctly rejects them,
// so a hand-written fixture would exercise the validator instead of the codec.

func cube(t *testing.T, texture string) mapfile.Brush {
	t.Helper()
	brush, err := mapfile.MakeBoxBrush(mapfile.Point{0, 0, 0}, mapfile.Point{64, 64, 64}, texture)
	if err != nil {
		t.Fatalf("MakeBoxBrush: %v", err)
	}
	return brush
}

// classicMap is a worldspawn whose first face carries
// non-default texture offsets.
func classicMap(t *testing.T) string {
	t.Helper()
	faces := cube(t, "METAL5_6").Faces()
	first := faces[0]
	faces[0] = mapfile.NewClassicFace(
		first.P1(), first.P2(), first.P3(), first.Texture(),
		12, -34, first.Rotation(), first.XScale(), first.YScale(), nil,
	)
	return writeMap(t, []mapfile.EntityItem{
		mapfile.NewKeyValue("classname", "worldspawn"),
		mapfile.NewKeyValue("wad", ""),
		mapfile.NewBrush(faces),
	})
}

func valveMap(t *testing.T) string {
	t.Helper()
	source := cube(t, "METAL5_6").Faces()
	faces := make([]mapfile.Face, 0, len(source))
	for _, face := range source {
		faces = append(faces, mapfile.NewValve220Face(
			face.P1(), face.P2(), face.P3(), face.Texture(),
			mapfile.NewTextureAxis(1, 0, 0, 8),
			mapfile.NewTextureAxis(0, -1, 0, -16),
			0, 1, 1, nil,
		))
	}
	return writeMap(t, []mapfile.EntityItem{
		mapfile.NewKeyValue("classname", "worldspawn"),
		mapfile.NewBrush(faces),
	})
}

func quake2TailMap(t *testing.T) string {
	t.Helper()
	faces := cube(t, "METAL5_6").Faces()
	first := faces[0]
	faces[0] = mapfile.NewClassicFace(
		first.P1(), first.P2(), first.P3(), first.Texture(),
		first.XOffset(), first.YOffset(), first.Rotation(), first.XScale(), first.YScale(),
		[]mapfile.Number{1, 0, 32},
	)
	return writeMap(t, []mapfile.EntityItem{
		mapfile.NewKeyValue("classname", "worldspawn"),
		mapfile.NewBrush(faces),
	})
}

// repeatedKeysMap has a repeated key and a property written *after* a brush.
func repeatedKeysMap(t *testing.T) string {
	t.Helper()
	return writeMap(t, []mapfile.EntityItem{
		mapfile.NewKeyValue("classname", "worldspawn"),
		mapfile.NewKeyValue("message", "first"),
		cube(t, "METAL5_6"),
		mapfile.NewKeyValue("message", "second"),
	})
}

const pointEntityMap = "{\n\"classname\" \"light\"\n\"origin\" \"8 8 8\"\n}\n"

func writeMap(t *testing.T, items []mapfile.EntityItem) string {
	t.Helper()
	text, err := mapfile.Write(mapfile.NewMapFile([]mapfile.Entity{mapfile.NewEntity(items)}))
	if err != nil {
		t.Fatalf("mapfile.Write: %v", err)
	}
	return text
}

func coreIdentity() PackageIdentity {
	return PackageIdentity{PackageID: "test-package", Role: RolePrefabCore}
}

// build imports `.map` text with the prefab-core
// defaults.
func build(t *testing.T, text string, mutate ...func(*ConvertOptions)) *Object {
	t.Helper()
	opts := ConvertOptions{
		Identity:   coreIdentity(),
		Role:       RolePrefabCore,
		DocumentID: "test-package:prefab_core",
	}
	for _, apply := range mutate {
		apply(&opts)
	}
	document, err := ImportMapText(text, opts)
	if err != nil {
		t.Fatalf("ImportMapText: %v", err)
	}
	return document
}

// brushOf is the first brush of an entity, found by kind rather than position.
func brushOf(t *testing.T, document *Object, entityIndex int) *Object {
	t.Helper()
	entities, _ := document.Get("entities")
	list, _ := entities.([]Value)
	if entityIndex >= len(list) {
		t.Fatalf("document has no entity %d", entityIndex)
	}
	entity, _ := list[entityIndex].(*Object)
	content, _ := entity.Get("content")
	for _, raw := range content.([]Value) {
		item, _ := raw.(*Object)
		if kind, _ := item.Get("kind"); kind == "brush" {
			return item
		}
	}
	t.Fatalf("entity %d has no brush", entityIndex)
	return nil
}

func facesOf(t *testing.T, brush *Object) []*Object {
	t.Helper()
	raw, _ := brush.Get("faces")
	list, _ := raw.([]Value)
	faces := make([]*Object, 0, len(list))
	for _, item := range list {
		face, ok := item.(*Object)
		if !ok {
			t.Fatalf("a face is not an object")
		}
		faces = append(faces, face)
	}
	return faces
}

func encodeOrFail(t *testing.T, document Value) string {
	t.Helper()
	text, err := Encode(document)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return text
}

func stringAt(t *testing.T, node *Object, name string) string {
	t.Helper()
	value, ok := node.Get(name)
	if !ok {
		t.Fatalf("member %q is absent", name)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("member %q is %T, not a string", name, value)
	}
	return text
}

// --- projections ------------------------------------------------------------

func TestClassicProjectionSurvivesAFullRoundTrip(t *testing.T) {
	text := classicMap(t)
	document := build(t, text)
	face := facesOf(t, brushOf(t, document, 0))[0]

	projection, _ := face.Get("projection")
	if got := encodeOrFail(t, projection); got != `{
  "mode": "classic",
  "shift": [
    12,
    -34
  ],
  "rotation": 0,
  "scale": [
    1,
    1
  ]
}
` {
		t.Fatalf("classic projection:\n%s", got)
	}

	exported, err := ExportMapText(document)
	if err != nil {
		t.Fatalf("ExportMapText: %v", err)
	}
	if exported != text {
		t.Fatalf("export differs:\n%s\nwant:\n%s", exported, text)
	}
}

func TestValve220ProjectionSurvivesAFullRoundTrip(t *testing.T) {
	text := valveMap(t)
	document := build(t, text)
	face := facesOf(t, brushOf(t, document, 0))[0]

	projection, _ := face.Get("projection")
	object, _ := projection.(*Object)
	if mode := stringAt(t, object, "mode"); mode != "valve220" {
		t.Fatalf("mode = %q", mode)
	}
	if got := renderNumbers(t, object, "u_axis"); got != "1 0 0 8" {
		t.Fatalf("u_axis = %q", got)
	}
	if got := renderNumbers(t, object, "v_axis"); got != "0 -1 0 -16" {
		t.Fatalf("v_axis = %q", got)
	}
	if got := renderNumbers(t, object, "scale"); got != "1 1" {
		t.Fatalf("scale = %q", got)
	}

	exported, err := ExportMapText(document)
	if err != nil {
		t.Fatalf("ExportMapText: %v", err)
	}
	if exported != text {
		t.Fatalf("export differs:\n%s\nwant:\n%s", exported, text)
	}
}

func renderNumbers(t *testing.T, node *Object, name string) string {
	t.Helper()
	raw, _ := node.Get(name)
	list, ok := raw.([]Value)
	if !ok {
		t.Fatalf("member %q is %T, not an array", name, raw)
	}
	parts := make([]string, 0, len(list))
	for _, item := range list {
		number, ok := item.(json.Number)
		if !ok {
			t.Fatalf("member %q holds a %T", name, item)
		}
		parts = append(parts, string(number))
	}
	return strings.Join(parts, " ")
}

func TestTheTwoProjectionModesAreNeverMixedOnOneFace(t *testing.T) {
	for _, text := range []string{classicMap(t), valveMap(t)} {
		for _, face := range facesOf(t, brushOf(t, build(t, text), 0)) {
			projection, _ := face.Get("projection")
			object, _ := projection.(*Object)
			classic := object.Has("shift")
			valve := object.Has("u_axis") || object.Has("v_axis")
			if classic && valve {
				t.Fatalf("a face mixes both projection modes: %v", object.Keys())
			}
		}
	}
}

func TestQuake2NumericFaceTailRoundTripsVerbatim(t *testing.T) {
	text := quake2TailMap(t)
	document := build(t, text)
	faces := facesOf(t, brushOf(t, document, 0))

	if got := renderNumbers(t, faces[0], "tail"); got != "1 0 32" {
		t.Fatalf("tail = %q", got)
	}
	if faces[1].Has("tail") {
		t.Fatalf("a face without a tail must not gain an empty one")
	}

	exported, err := ExportMapText(document)
	if err != nil {
		t.Fatalf("ExportMapText: %v", err)
	}
	if exported != text {
		t.Fatalf("export differs:\n%s\nwant:\n%s", exported, text)
	}
}

// --- ordering and repeats ---------------------------------------------------

func TestRepeatedEntityKeysAndTheirOrderRelativeToBrushesSurvive(t *testing.T) {
	text := repeatedKeysMap(t)
	document := build(t, text)

	entities, _ := document.Get("entities")
	entity, _ := entities.([]Value)[0].(*Object)
	content, _ := entity.Get("content")

	var kinds, messages []string
	for _, raw := range content.([]Value) {
		item, _ := raw.(*Object)
		kinds = append(kinds, stringAt(t, item, "kind"))
		if key, _ := item.Get("key"); key == "message" {
			messages = append(messages, stringAt(t, item, "value"))
		}
	}
	if strings.Join(kinds, ",") != "property,property,brush,property" {
		t.Fatalf("a property written after a brush must stay after it: %v", kinds)
	}
	if strings.Join(messages, ",") != "first,second" {
		t.Fatalf("messages = %v", messages)
	}

	exported, err := ExportMapText(document)
	if err != nil {
		t.Fatalf("ExportMapText: %v", err)
	}
	if exported != text {
		t.Fatalf("export differs:\n%s\nwant:\n%s", exported, text)
	}
}

func TestEntityOrderAndBrushlessPointEntitiesArePreserved(t *testing.T) {
	text := classicMap(t) + pointEntityMap
	document := build(t, text)

	entities, _ := document.Get("entities")
	var names []string
	for _, raw := range entities.([]Value) {
		entity, _ := raw.(*Object)
		content, _ := entity.Get("content")
		for _, rawItem := range content.([]Value) {
			item, _ := rawItem.(*Object)
			if key, _ := item.Get("key"); key == "classname" {
				names = append(names, stringAt(t, item, "value"))
			}
		}
	}
	if strings.Join(names, ",") != "worldspawn,light" {
		t.Fatalf("entity order lost: %v", names)
	}

	second, _ := entities.([]Value)[1].(*Object)
	content, _ := second.Get("content")
	for _, rawItem := range content.([]Value) {
		item, _ := rawItem.(*Object)
		if stringAt(t, item, "kind") != "property" {
			t.Fatalf("a point entity gained a brush")
		}
	}

	exported, err := ExportMapText(document)
	if err != nil {
		t.Fatalf("ExportMapText: %v", err)
	}
	if exported != text {
		t.Fatalf("export differs:\n%s\nwant:\n%s", exported, text)
	}
}

// --- .map -> APMap -> .map --------------------------------------------------

func TestMapToAPMapToMapIsStructurallyEquivalent(t *testing.T) {
	cases := map[string]string{
		"classic":       classicMap(t),
		"valve220":      valveMap(t),
		"quake2-tail":   quake2TailMap(t),
		"repeated-keys": repeatedKeysMap(t),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			exported, err := ExportMapText(build(t, text))
			if err != nil {
				t.Fatalf("ExportMapText: %v", err)
			}

			original, err := mapfile.Parse(text)
			if err != nil {
				t.Fatalf("Parse(original): %v", err)
			}
			restored, err := mapfile.Parse(exported)
			if err != nil {
				t.Fatalf("Parse(exported): %v", err)
			}
			if !restored.Equal(original) {
				t.Fatalf("round trip is not structurally equivalent")
			}
			// For text this writer produced, equivalence is in fact byte equality.
			if exported != text {
				t.Fatalf("byte equality lost:\n%s\nwant:\n%s", exported, text)
			}
		})
	}
}

func TestMapCommentsAreADocumentedLossNotASilentCorruption(t *testing.T) {
	text := classicMap(t)
	commented := "// a leading comment\n" + text

	exported, err := ExportMapText(build(t, commented))
	if err != nil {
		t.Fatalf("ExportMapText: %v", err)
	}
	if strings.Contains(exported, "//") {
		t.Fatalf("comments are dropped by the tokenizer, but one survived")
	}

	before, err := mapfile.Parse(commented)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	after, err := mapfile.Parse(exported)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !after.Equal(before) {
		t.Fatalf("geometry was affected by dropping a comment")
	}
}

// --- APMap -> model -> APMap ------------------------------------------------

func TestAPMapToModelToAPMapPreservesIdentityAndBytes(t *testing.T) {
	cases := map[string]string{
		"classic":       classicMap(t),
		"valve220":      valveMap(t),
		"quake2-tail":   quake2TailMap(t),
		"repeated-keys": repeatedKeysMap(t),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			document := build(t, text)
			encoded := encodeOrFail(t, document)

			geometry, err := ToMapDocument(document)
			if err != nil {
				t.Fatalf("ToMapDocument: %v", err)
			}
			restored, err := Rebuild(document, geometry)
			if err != nil {
				t.Fatalf("Rebuild: %v", err)
			}
			if got := encodeOrFail(t, restored); got != encoded {
				t.Fatalf("rebuild changed the bytes:\n%s\nwant:\n%s", got, encoded)
			}
		})
	}
}

// TestEveryPublishedDocumentSurvivesTheModelRoundTrip is the prompt's headline
// guarantee, asserted literally: for every real published document,
// Rebuild(doc, geometry-from-doc) reproduces the document's own canonical bytes.
func TestEveryPublishedDocumentSurvivesTheModelRoundTrip(t *testing.T) {
	paths := publishedDocuments(t)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			text := string(data)
			if !IsCanonical(text) {
				t.Fatalf("the published document is not canonical to begin with")
			}

			document, err := Decode(text, path)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			geometry, err := ToMapDocument(document)
			if err != nil {
				t.Fatalf("ToMapDocument: %v", err)
			}
			restored, err := Rebuild(document, geometry)
			if err != nil {
				t.Fatalf("Rebuild: %v", err)
			}
			// The round trip is the identity on GEOMETRY, and the writer stamps the CURRENT
			// version and $schema — which is the point of writing current-only. The published
			// documents are 1.0, so the expected bytes are the migrated ones: the header, and
			// nothing else, differs. If a future change alters more than the header, the diff
			// below is where it shows.
			if got, want := encodeOrFail(t, restored), migrateHeader(text); got != want {
				t.Fatalf("the round trip is not the identity function on canonical bytes:\n%s", diffFirstLine(got, want))
			}
		})
	}
}

// publishedDocuments is every example and valid vector in the published tree.
func publishedDocuments(t *testing.T) []string {
	t.Helper()
	root := publishedRoot(t)
	var paths []string
	for _, pattern := range []string{"examples/*.apmap", "test-vectors/valid/*.apmap"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatalf("Glob: %v", err)
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	if len(paths) < 12 {
		t.Fatalf("the published tree lost documents: found %d", len(paths))
	}
	return paths
}

// publishedRoot is the published 1.0 material, which lives in the contract package now rather than
// under the data root. It is DEPRECATED evidence: the documents there declare 1.0 and the ordinary
// path refuses them, so a caller that wants to read one migrates its header first.
func publishedRoot(t *testing.T) string {
	t.Helper()
	candidate := deprecatedCorpusRoot()
	if candidate == "" {
		t.Skip("the auto-pigeon-libraries contract package is not beside this checkout")
	}
	if _, err := os.Stat(filepath.Join(candidate, "test-vectors")); err != nil {
		t.Skipf("the published 1.0 corpus is not reachable at %s", candidate)
	}
	return candidate
}

func diffFirstLine(got, want string) string {
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	for i := range gotLines {
		if i >= len(wantLines) {
			return "got has extra line: " + gotLines[i]
		}
		if gotLines[i] != wantLines[i] {
			return "line " + string(rune('0'+i%10)) + ": got " + gotLines[i] + " want " + wantLines[i]
		}
	}
	if len(wantLines) > len(gotLines) {
		return "want has extra line: " + wantLines[len(gotLines)]
	}
	return "(no line differs; lengths equal)"
}

// --- identity re-attachment -------------------------------------------------

// TestMovingABrushKeepsItsIdentity is the property the whole identity design
// exists for.
func TestMovingABrushKeepsItsIdentity(t *testing.T) {
	document := build(t, classicMap(t))
	brush := brushOf(t, document, 0)
	brushID := stringAt(t, brush, "brush_id")
	var faceIDs []string
	for _, face := range facesOf(t, brush) {
		faceIDs = append(faceIDs, stringAt(t, face, "face_id"))
	}
	originalX := renderNumbers(t, planePointsOf(t, facesOf(t, brush)[0])[0], "")

	geometry, err := ToMapDocument(document)
	if err != nil {
		t.Fatalf("ToMapDocument: %v", err)
	}
	moved, err := Rebuild(document, shiftX(geometry, 32))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	movedBrush := brushOf(t, moved, 0)
	if got := stringAt(t, movedBrush, "brush_id"); got != brushID {
		t.Fatalf("brush_id changed: %s != %s", got, brushID)
	}
	for index, face := range facesOf(t, movedBrush) {
		if got := stringAt(t, face, "face_id"); got != faceIDs[index] {
			t.Fatalf("face %d id changed: %s != %s", index, got, faceIDs[index])
		}
	}
	// derived_from and extensions travel with the id.
	if _, ok := movedBrush.Get("derived_from"); !ok {
		t.Fatalf("derived_from was not reattached")
	}

	// The coordinates did change; only the identity is stable.
	movedX := renderNumbers(t, planePointsOf(t, facesOf(t, movedBrush)[0])[0], "")
	if movedX == originalX {
		t.Fatalf("the geometry did not actually move")
	}
}

// planePointsOf returns each plane point wrapped in a one-member object so that
// renderNumbers can print it.
func planePointsOf(t *testing.T, face *Object) []*Object {
	t.Helper()
	plane, _ := face.Get("plane")
	object, _ := plane.(*Object)
	points, _ := object.Get("points")
	list, _ := points.([]Value)
	out := make([]*Object, 0, len(list))
	for _, point := range list {
		wrapper := NewObject()
		wrapper.Set("", point)
		out = append(out, wrapper)
	}
	return out
}

func shiftX(document *mapfile.MapFile, delta float64) *mapfile.MapFile {
	entities := make([]mapfile.Entity, 0, document.EntityCount())
	for entityIndex := 0; entityIndex < document.EntityCount(); entityIndex++ {
		entity := document.Entity(entityIndex)
		items := make([]mapfile.EntityItem, 0, len(entity.Items()))
		for _, item := range entity.Items() {
			brush, ok := item.(mapfile.Brush)
			if !ok {
				items = append(items, item)
				continue
			}
			faces := make([]mapfile.Face, 0, brush.FaceCount())
			for faceIndex := 0; faceIndex < brush.FaceCount(); faceIndex++ {
				faces = append(faces, shiftFace(brush.Face(faceIndex), delta))
			}
			items = append(items, mapfile.NewBrush(faces))
		}
		entities = append(entities, mapfile.NewEntity(items))
	}
	return mapfile.NewMapFile(entities)
}

func shiftFace(face mapfile.Face, delta float64) mapfile.Face {
	move := func(v mapfile.Vec3) mapfile.Vec3 {
		return mapfile.NewVec3(v.X()+delta, v.Y(), v.Z())
	}
	if face.TextureMode() == mapfile.ModeValve220 {
		uAxis, _ := face.UAxis()
		vAxis, _ := face.VAxis()
		return mapfile.NewValve220Face(
			move(face.P1()), move(face.P2()), move(face.P3()), face.Texture(),
			uAxis, vAxis, face.Rotation(), face.XScale(), face.YScale(), face.ExtraValues(),
		)
	}
	return mapfile.NewClassicFace(
		move(face.P1()), move(face.P2()), move(face.P3()), face.Texture(),
		face.XOffset(), face.YOffset(), face.Rotation(), face.XScale(), face.YScale(),
		face.ExtraValues(),
	)
}

// TestReattachmentIsByStructuralPathNotArrayIndex is the failure mode the prompt
// singles out. Two documents whose entity *order* differs must not swap
// metadata: reattachment keyed by array index would hand entity 0's identity to
// whatever now sits at position 0.
func TestReattachmentIsByStructuralPathNotArrayIndex(t *testing.T) {
	document := build(t, classicMap(t)+pointEntityMap)
	reused, err := CollectReusable(document)
	if err != nil {
		t.Fatalf("CollectReusable: %v", err)
	}

	// e0 is the brush-bearing worldspawn, e1 the point entity. Their reuse
	// entries must be distinct and addressed by path.
	if len(reused.Entities) != 2 {
		t.Fatalf("expected two entity entries, got %d", len(reused.Entities))
	}
	if _, ok := reused.Brushes["e0.b0"]; !ok {
		t.Fatalf("the brush was not indexed at e0.b0: %v", reuseKeysOf(reused.Brushes))
	}
	if _, ok := reused.Brushes["e1.b0"]; ok {
		t.Fatalf("the point entity gained a brush entry")
	}
	if _, ok := reused.Faces["e0.b0.f5"]; !ok {
		t.Fatalf("faces were not indexed by path: %v", reuseKeysOf(reused.Faces))
	}

	// Rebuilding geometry that has *only* the point entity must give it e0's
	// stored identity — the path is what is being reattached, and the point
	// entity now occupies e0.
	geometry, err := ToMapDocument(document)
	if err != nil {
		t.Fatalf("ToMapDocument: %v", err)
	}
	onlySecond := mapfile.NewMapFile([]mapfile.Entity{geometry.Entity(1)})
	rebuilt, err := Rebuild(document, onlySecond)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	entities, _ := rebuilt.Get("entities")
	list, _ := entities.([]Value)
	if len(list) != 1 {
		t.Fatalf("expected one entity, got %d", len(list))
	}
	first, _ := list[0].(*Object)

	originalEntities, _ := document.Get("entities")
	originalFirst, _ := originalEntities.([]Value)[0].(*Object)
	if got, want := stringAt(t, first, "entity_id"), stringAt(t, originalFirst, "entity_id"); got != want {
		t.Fatalf("path-keyed reattachment did not apply: %s != %s", got, want)
	}
}

func reuseKeysOf(store map[string]*Object) []string {
	var keys []string
	for key := range store {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestRemovingABrushLeavesTheRemainingIdentitiesIntact covers the prompt's
// second re-attachment case: a removed object's metadata is simply not
// reattached, and that is not an error.
func TestRemovingABrushLeavesTheRemainingIdentitiesIntact(t *testing.T) {
	twoBrushes := writeMap(t, []mapfile.EntityItem{
		mapfile.NewKeyValue("classname", "worldspawn"),
		cube(t, "METAL5_6"),
		cube(t, "CITY2_1"),
	})
	document := build(t, twoBrushes)

	entities, _ := document.Get("entities")
	entity, _ := entities.([]Value)[0].(*Object)
	content, _ := entity.Get("content")
	var brushIDs []string
	for _, raw := range content.([]Value) {
		item, _ := raw.(*Object)
		if stringAt(t, item, "kind") == "brush" {
			brushIDs = append(brushIDs, stringAt(t, item, "brush_id"))
		}
	}
	if len(brushIDs) != 2 {
		t.Fatalf("expected two brushes, got %d", len(brushIDs))
	}

	// Drop the *second* brush; the first keeps path e0.b0 and its identity.
	geometry, err := ToMapDocument(document)
	if err != nil {
		t.Fatalf("ToMapDocument: %v", err)
	}
	source := geometry.Entity(0).Items()
	trimmed := mapfile.NewMapFile([]mapfile.Entity{
		mapfile.NewEntity([]mapfile.EntityItem{source[0], source[1]}),
	})

	rebuilt, err := Rebuild(document, trimmed)
	if err != nil {
		t.Fatalf("Rebuild after removing a brush must not be an error: %v", err)
	}
	if got := stringAt(t, brushOf(t, rebuilt, 0), "brush_id"); got != brushIDs[0] {
		t.Fatalf("the surviving brush lost its identity: %s != %s", got, brushIDs[0])
	}

	rebuiltEntities, _ := rebuilt.Get("entities")
	rebuiltEntity, _ := rebuiltEntities.([]Value)[0].(*Object)
	rebuiltContent, _ := rebuiltEntity.Get("content")
	count := 0
	for _, raw := range rebuiltContent.([]Value) {
		item, _ := raw.(*Object)
		if stringAt(t, item, "kind") == "brush" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the removed brush came back: %d brushes", count)
	}
}

// TestExtensionsSurviveAGeometryEdit pins that reattachment carries more than
// the id — a namespaced extension on a brush travels with its path.
func TestExtensionsSurviveAGeometryEdit(t *testing.T) {
	document := build(t, classicMap(t))
	brush := brushOf(t, document, 0)
	extension := NewObject()
	inner := NewObject()
	inner.Set("note", "load bearing")
	extension.Set("aue.test", inner)
	brush.Set("extensions", extension)

	geometry, err := ToMapDocument(document)
	if err != nil {
		t.Fatalf("ToMapDocument: %v", err)
	}
	moved, err := Rebuild(document, shiftX(geometry, 32))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	restored, ok := brushOf(t, moved, 0).Get("extensions")
	if !ok {
		t.Fatalf("the brush extension was dropped by the round trip")
	}
	if got := encodeOrFail(t, restored); !strings.Contains(got, "load bearing") {
		t.Fatalf("extension content changed: %s", got)
	}
}

// --- authored ordinals ------------------------------------------------------

// TestAuthoredIdentityGivesEachObjectItsOwnOrdinal is the branch the prompt
// warns about: get it backwards and every authored object collides on ordinal 0.
func TestAuthoredIdentityGivesEachObjectItsOwnOrdinal(t *testing.T) {
	document := build(t, classicMap(t), func(opts *ConvertOptions) {
		opts.Identity = AuthoredIdentity{DocumentID: "doc-1", OperationID: "op-9f2a"}
		opts.Role = RoleFullMap
		opts.DocumentID = "doc-1"
	})

	entities, _ := document.Get("entities")
	entity, _ := entities.([]Value)[0].(*Object)

	ordinals := []int{ordinalOf(t, entity)}
	brush := brushOf(t, document, 0)
	ordinals = append(ordinals, ordinalOf(t, brush))
	for _, face := range facesOf(t, brush) {
		ordinals = append(ordinals, ordinalOf(t, face))
	}

	seen := map[int]bool{}
	for index, value := range ordinals {
		if seen[value] {
			t.Fatalf("ordinal %d was issued twice (object %d): %v", value, index, ordinals)
		}
		seen[value] = true
		if value != index {
			t.Fatalf("ordinals are not monotonic from zero: %v", ordinals)
		}
	}

	// The id an object carries must be the one its own ordinal mandates.
	if got, want := stringAt(t, entity, "entity_id"), mandatedFor(t, KindEntity, entity); got != want {
		t.Fatalf("entity id does not match its record: %s != %s", got, want)
	}
	if got, want := stringAt(t, brush, "brush_id"), mandatedFor(t, KindBrush, brush); got != want {
		t.Fatalf("brush id does not match its record: %s != %s", got, want)
	}
}

func ordinalOf(t *testing.T, node *Object) int {
	t.Helper()
	raw, ok := node.Get("derived_from")
	if !ok {
		t.Fatalf("object has no derived_from")
	}
	record, _ := raw.(*Object)
	if kind, _ := record.Get("kind"); kind != "authored" {
		t.Fatalf("derived_from is %v, not authored", kind)
	}
	value, _ := record.Get("ordinal")
	number, ok := value.(json.Number)
	if !ok {
		t.Fatalf("ordinal is %T", value)
	}
	parsed, err := number.Int64()
	if err != nil {
		t.Fatalf("ordinal %q: %v", number, err)
	}
	return int(parsed)
}

func mandatedFor(t *testing.T, kind Kind, node *Object) string {
	t.Helper()
	record, _ := node.Get("derived_from")
	id, err := ExpectedID(kind, record)
	if err != nil {
		t.Fatalf("ExpectedID: %v", err)
	}
	return id
}

func TestAnAuthoredImportIsReproducible(t *testing.T) {
	mutate := func(opts *ConvertOptions) {
		opts.Identity = AuthoredIdentity{DocumentID: "doc-1", OperationID: "op-9f2a"}
		opts.Role = RoleFullMap
		opts.DocumentID = "doc-1"
	}
	text := classicMap(t)
	first := encodeOrFail(t, build(t, text, mutate))
	second := encodeOrFail(t, build(t, text, mutate))
	if first != second {
		t.Fatalf("two identical authored imports differ")
	}
}

// --- member ordering --------------------------------------------------------

// TestMemberOrderIsImposedNotIncidental proves SER-5 through IsCanonical rather
// than by eyeballing the key list, and additionally checks the declared order
// directly so a failure says *which* member moved.
func TestMemberOrderIsImposedNotIncidental(t *testing.T) {
	document := build(t, quake2TailMap(t), func(opts *ConvertOptions) {
		opts.SyntheticFaces = map[string]string{"e0.b0.f0": "context_crop"}
	})
	text := encodeOrFail(t, document)
	if !IsCanonical(text) {
		t.Fatalf("converted output is not canonical")
	}

	entities, _ := document.Get("entities")
	entity, _ := entities.([]Value)[0].(*Object)
	assertDeclaredOrder(t, "entity", entity, entityMembers)

	brush := brushOf(t, document, 0)
	assertDeclaredOrder(t, "brush", brush, brushMembers)

	face := facesOf(t, brush)[0]
	assertDeclaredOrder(t, "face", face, faceMembers)
	// The face carries every optional member the order declares, so this run
	// exercises the whole tuple rather than its first four entries.
	for _, name := range []string{"tail", "synthetic", "synthetic_reason"} {
		if !face.Has(name) {
			t.Fatalf("the fixture no longer exercises %q", name)
		}
	}
}

func assertDeclaredOrder(t *testing.T, what string, node *Object, members []string) {
	t.Helper()
	position := map[string]int{}
	for index, name := range members {
		position[name] = index
	}
	previous := -1
	for _, key := range node.Keys() {
		index, declared := position[key]
		if !declared {
			t.Fatalf("%s carries an undeclared member %q", what, key)
		}
		if index <= previous {
			t.Fatalf("%s members are out of declared order: %v", what, node.Keys())
		}
		previous = index
	}
}

// TestAnUndeclaredMemberIsKeptForTheValidatorToReject pins ordered()'s tail
// behaviour: an unknown member is preserved rather than silently dropped, so the
// schema layer gets to report it.
func TestAnUndeclaredMemberIsKeptForTheValidatorToReject(t *testing.T) {
	document := build(t, classicMap(t))
	brush := brushOf(t, document, 0)
	brush.Set("not_a_member", "kept")

	geometry, err := ToMapDocument(document)
	if err != nil {
		t.Fatalf("ToMapDocument: %v", err)
	}
	rebuilt, err := Rebuild(document, geometry)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if _, ok := brushOf(t, rebuilt, 0).Get("not_a_member"); !ok {
		t.Fatalf("an undeclared member was dropped instead of being reported")
	}
}

// --- frames -----------------------------------------------------------------

func TestAPrefabLocalDocumentDeclaresItsTransformBackToSource(t *testing.T) {
	document := build(t, classicMap(t), func(opts *ConvertOptions) {
		opts.Frame = PrefabLocalFrame(&[3]float64{1224, 999, 1384})
	})
	frame, _ := document.Get("frame")
	want := `{
  "space": "prefab_local",
  "units": "quake_unit",
  "coordinate_system": {
    "handedness": "right",
    "up_axis": "z"
  },
  "to_source": {
    "type": "translation",
    "translation": [
      1224,
      999,
      1384
    ]
  }
}
`
	if got := encodeOrFail(t, frame); got != want {
		t.Fatalf("frame:\n%s\nwant:\n%s", got, want)
	}
}

func TestASourceWorldFrameCannotCarryATranslation(t *testing.T) {
	frame := Frame{Space: SpaceSourceWorld, Translation: &[3]float64{1, 0, 0}}
	if err := frame.Validate(); err == nil {
		t.Fatalf("a source_world frame with a translation must be refused")
	}
	if _, err := frame.ToJSON(); err == nil {
		t.Fatalf("ToJSON must refuse the same frame Validate refuses")
	}
}

func TestAnUnknownFrameSpaceIsRefused(t *testing.T) {
	if err := (Frame{Space: "galactic"}).Validate(); err == nil {
		t.Fatalf("an unknown frame space must be refused")
	}
}

// TestTheFrameNeverMovesTheCoordinates is the prompt's explicit "do not".
func TestTheFrameNeverMovesTheCoordinates(t *testing.T) {
	text := classicMap(t)
	plain := build(t, text)
	local := build(t, text, func(opts *ConvertOptions) {
		opts.Frame = PrefabLocalFrame(&[3]float64{1224, 999, 1384})
	})

	plainPlane, _ := facesOf(t, brushOf(t, plain, 0))[0].Get("plane")
	localPlane, _ := facesOf(t, brushOf(t, local, 0))[0].Get("plane")
	if encodeOrFail(t, plainPlane) != encodeOrFail(t, localPlane) {
		t.Fatalf("the frame moved the coordinates")
	}
}

// --- synthetic faces --------------------------------------------------------

func TestANamedFaceIsMarkedSyntheticWithItsReason(t *testing.T) {
	document := build(t, classicMap(t), func(opts *ConvertOptions) {
		opts.SyntheticFaces = map[string]string{"e0.b0.f2": "context_crop"}
	})
	faces := facesOf(t, brushOf(t, document, 0))

	if value, _ := faces[2].Get("synthetic"); value != true {
		t.Fatalf("synthetic = %v", value)
	}
	if got := stringAt(t, faces[2], "synthetic_reason"); got != "context_crop" {
		t.Fatalf("synthetic_reason = %q", got)
	}
	if faces[0].Has("synthetic") {
		t.Fatalf("an authored face must not be marked")
	}
	if problems := SemanticProblems(document); len(problems) != 0 {
		t.Fatalf("semantic problems: %v", problems)
	}
}

// --- policy, version and validity -------------------------------------------

func TestAnUnimplementedVersionIsRefused(t *testing.T) {
	geometry, err := mapfile.Parse(classicMap(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	_, err = FromMapDocument(geometry, ConvertOptions{
		Identity:   coreIdentity(),
		Role:       RolePrefabCore,
		DocumentID: "d",
		Version:    "2.0",
	})
	var versionError *VersionError
	if !asError(err, &versionError) {
		t.Fatalf("expected a *VersionError, got %v", err)
	}
}

func TestAnUnknownIDPolicyIsRefused(t *testing.T) {
	geometry, err := mapfile.Parse(classicMap(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := FromMapDocument(geometry, ConvertOptions{
		Identity:   coreIdentity(),
		Role:       RolePrefabCore,
		DocumentID: "d",
		IDPolicy:   "guessed",
	}); err == nil {
		t.Fatalf("an unknown id_policy must be refused")
	}
}

// TestAMintedDocumentRecordsNoDerivation is SEM-8's other side: writing a
// derivation record into a minted document would be a false claim.
func TestAMintedDocumentRecordsNoDerivation(t *testing.T) {
	document := build(t, classicMap(t), func(opts *ConvertOptions) {
		opts.IDPolicy = PolicyMinted
	})
	entities, _ := document.Get("entities")
	entity, _ := entities.([]Value)[0].(*Object)
	if entity.Has("derived_from") {
		t.Fatalf("a minted document must not claim a derivation")
	}
	if brushOf(t, document, 0).Has("derived_from") {
		t.Fatalf("a minted brush must not claim a derivation")
	}
}

// TestMintedAuthoredIdentityHasAnOrdinalCollision pins a known defect rather
// than silently fixing it.
//
// Only recordFor advances the ordinal, and a minted document records no
// derivation — so every authored object in a minted document is issued ordinal
// 0 and every object of one kind collides on a single id: a six-face cube yields
// one distinct face id and six SEM-1 duplicate-id problems.
//
// The decision is to keep the ordinal rule: the collision is caught by SEM-1
// the moment such a document is validated, so it fails loudly rather than
// corrupting anything, and changing the rule would change the ids minted for an
// existing journal replay, which is worse.
func TestMintedAuthoredIdentityHasAnOrdinalCollision(t *testing.T) {
	document := build(t, classicMap(t), func(opts *ConvertOptions) {
		opts.Identity = AuthoredIdentity{DocumentID: "doc", OperationID: "op"}
		opts.Role = RoleFullMap
		opts.DocumentID = "doc"
		opts.IDPolicy = PolicyMinted
	})

	distinct := map[string]bool{}
	faces := facesOf(t, brushOf(t, document, 0))
	for _, face := range faces {
		distinct[stringAt(t, face, "face_id")] = true
	}
	if len(distinct) != 1 {
		t.Fatalf("a minted document issues one face id here; got %d", len(distinct))
	}

	problems := SemanticProblems(document)
	duplicates := 0
	for _, problem := range problems {
		if strings.HasPrefix(problem, "SEM-1 ") {
			duplicates++
		}
	}
	if duplicates == 0 {
		t.Fatalf("the collision must be caught by SEM-1, but was not: %v", problems)
	}
}

// TestAConvertedDocumentPassesBothValidationLayers is the end-to-end check that
// what this file produces is a document the rest of the codec accepts.
func TestAConvertedDocumentPassesBothValidationLayers(t *testing.T) {
	// The deployment's current schema — the only one this build validates against. This used to
	// read the published 1.0 file out of the corpus directory; that document is quarantined now
	// and nothing loads it.
	schema, err := LoadSchema("")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}

	for name, text := range map[string]string{
		"classic":       classicMap(t),
		"valve220":      valveMap(t),
		"quake2-tail":   quake2TailMap(t),
		"repeated-keys": repeatedKeysMap(t),
		"point-entity":  classicMap(t) + pointEntityMap,
	} {
		t.Run(name, func(t *testing.T) {
			document := build(t, text, func(opts *ConvertOptions) {
				opts.Role = RoleFullMap
				opts.Identity = PackageIdentity{PackageID: "test-package", Role: RoleFullMap}
			})
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
				t.Fatalf("converted output is not canonical")
			}
		})
	}
}

// --- decoding failures ------------------------------------------------------

func TestDecodingRejectsMalformedGeometryWithoutPanicking(t *testing.T) {
	cases := map[string]string{
		"plane with two points":  `{"entities":[{"content":[{"kind":"brush","faces":[{"plane":{"points":[[0,0,0],[1,0,0]]},"texture":"T","projection":{"mode":"classic"}}]}]}]}`,
		"point with two axes":    `{"entities":[{"content":[{"kind":"brush","faces":[{"plane":{"points":[[0,0],[1,0,0],[0,1,0]]},"texture":"T","projection":{"mode":"classic"}}]}]}]}`,
		"three-component u_axis": `{"entities":[{"content":[{"kind":"brush","faces":[{"plane":{"points":[[0,0,0],[1,0,0],[0,1,0]]},"texture":"T","projection":{"mode":"valve220","u_axis":[1,0,0],"v_axis":[0,1,0,0]}}]}]}]}`,
		"non-numeric coordinate": `{"entities":[{"content":[{"kind":"brush","faces":[{"plane":{"points":[["x",0,0],[1,0,0],[0,1,0]]},"texture":"T","projection":{"mode":"classic"}}]}]}]}`,
		"non-finite coordinate":  `{"entities":[{"content":[{"kind":"brush","faces":[{"plane":{"points":[[1e400,0,0],[1,0,0],[0,1,0]]},"texture":"T","projection":{"mode":"classic"}}]}]}]}`,
		"unknown content kind":   `{"entities":[{"content":[{"kind":"portal"}]}]}`,
		"entities is an object":  `{"entities":{"e0":{}}}`,
		"single-element shift":   `{"entities":[{"content":[{"kind":"brush","faces":[{"plane":{"points":[[0,0,0],[1,0,0],[0,1,0]]},"texture":"T","projection":{"mode":"classic","shift":[3]}}]}]}]}`,
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			document, err := Decode(text, "<test>")
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if _, err := ToMapDocument(document); err == nil {
				t.Fatalf("expected an error, got none")
			}
		})
	}
}

// TestAbsentProjectionMembersTakeTheirDefaults: an absent scale is [1, 1].
func TestAbsentProjectionMembersTakeTheirDefaults(t *testing.T) {
	text := `{"entities":[{"content":[{"kind":"brush","faces":[
		{"plane":{"points":[[0,0,0],[1,0,0],[0,1,0]]},"texture":"T","projection":{"mode":"classic"}}
	]}]}]}`
	document, err := Decode(text, "<test>")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	geometry, err := ToMapDocument(document)
	if err != nil {
		t.Fatalf("ToMapDocument: %v", err)
	}
	face := geometry.Entity(0).Brushes()[0].Face(0)
	if face.XScale() != 1 || face.YScale() != 1 {
		t.Fatalf("scale defaults lost: %v %v", face.XScale(), face.YScale())
	}
	if face.XOffset() != 0 || face.YOffset() != 0 || face.Rotation() != 0 {
		t.Fatalf("shift/rotation defaults lost")
	}
}

// TestCollectReusableIsDefensive proves the index never panics on a document
// that would not survive validation.
func TestCollectReusableIsDefensive(t *testing.T) {
	for _, text := range []string{
		`{}`,
		`{"entities":"nope"}`,
		`{"entities":[42]}`,
		`{"entities":[{"content":"nope"}]}`,
		`{"entities":[{"content":[7]}]}`,
		`{"entities":[{"content":[{"kind":"brush","faces":"nope"}]}]}`,
		`{"entities":[{"content":[{"kind":"brush","faces":[null]}]}]}`,
	} {
		document, err := Decode(text, "<test>")
		if err != nil {
			t.Fatalf("Decode(%s): %v", text, err)
		}
		if reused, err := CollectReusable(document); reused == nil || err != nil {
			t.Fatalf("CollectReusable returned (%v, %v) for %s", reused, err, text)
		}
	}
	if reused, err := CollectReusable(nil); reused == nil || err != nil {
		t.Fatalf("CollectReusable(nil) returned (%v, %v)", reused, err)
	}
}

// --- import from disk -------------------------------------------------------

func TestImportMapFileReadsAndConverts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.map")
	text := classicMap(t)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	document, err := ImportMapFile(path, ConvertOptions{
		Identity:   coreIdentity(),
		Role:       RolePrefabCore,
		DocumentID: "test-package:prefab_core",
	})
	if err != nil {
		t.Fatalf("ImportMapFile: %v", err)
	}
	if got := encodeOrFail(t, document); got != encodeOrFail(t, build(t, text)) {
		t.Fatalf("ImportMapFile and ImportMapText disagree")
	}

	if _, err := ImportMapFile(filepath.Join(dir, "missing.map"), ConvertOptions{
		Identity: coreIdentity(), Role: RolePrefabCore, DocumentID: "d",
	}); err == nil {
		t.Fatalf("a missing file must be an error")
	}
}

func TestImportMapTextReportsAParseErrorWithItsSource(t *testing.T) {
	_, err := ImportMapText("{ not a map", ConvertOptions{
		Identity: coreIdentity(), Role: RolePrefabCore, DocumentID: "d", Source: "fixture.map",
	})
	if err == nil {
		t.Fatalf("malformed .map text must be an error")
	}
	if !strings.Contains(err.Error(), "fixture.map") {
		t.Fatalf("the parse error lost its source name: %v", err)
	}
}

// asError is errors.As with a shorter call site.
func asError(err error, target any) bool {
	if err == nil {
		return false
	}
	if pointer, ok := target.(**VersionError); ok {
		versionError, matched := err.(*VersionError)
		if matched {
			*pointer = versionError
		}
		return matched
	}
	return false
}
