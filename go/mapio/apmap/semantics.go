package apmap

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Semantic validation — rules SEM-1 … SEM-11, SEM-Q-1 … SEM-Q-5 (semantics_quake3.go), and
// (semantics_groups.go) SEM-G-1 … SEM-G-9 and SEM-A-1.
//
// These are the rules JSON Schema provably cannot express, which is why this is
// a separate pass rather than a few extra schema keywords: id uniqueness is
// cross-document, reference integrity needs the whole object graph, convexity
// needs half-space arithmetic, and 1e400 is valid JSON that decodes to infinity.
// Seven of the twenty-two published invalid vectors are rejected here and
// nowhere else.
//
// # Every check is defensive
//
// This runs against documents that may already have failed schema validation, so
// every lookup is guarded: a missing or wrong-typed member is the schema layer's
// finding to report, not a problem to raise here and never a panic. In Python
// that discipline is a series of isinstance checks; in Go it is the comma-ok
// form on every type assertion, and there is no unchecked assertion anywhere in
// this file. TestSemanticProblemsOnASchemaInvalidDocument is the proof.
//
// # Where SEM-11 lives, checked rather than assumed
//
// The prompt asks not to assume this layer and serialize.NormalizeNumber are
// redundant. They are not, and the split is deliberate:
//
//   - serialize.NormalizeNumber rejects a non-finite value at *encode* time,
//     because JSON has no spelling for infinity and Python's json.dumps would
//     write a bare `Infinity`. It fails the whole encode.
//   - This layer reports a non-finite value at *validate* time, with the JSON
//     path that carries it, alongside every other problem in the document.
//
// A document can be validated without ever being encoded, and encoded without
// being validated. Neither call subsumes the other, and dropping either would
// leave a real path uncovered.
//
// One subtlety the value model forces. Python's _finite tests
// `isinstance(node, float)`, so a Python *int* is never checked — correct there,
// since Python ints are arbitrary precision and always finite. This package
// keeps numbers as literals, so the equivalent question is whether the literal
// is an integer one: `1e400` decodes to infinity and is flagged, while a
// four-hundred-digit integer literal is finite in exactly the way Python's int
// is, and is not. isIntegerLiteral draws that line, the same one json.go and
// serialize.go already use.

// NormalEpsilon is the threshold below which a triangle's normal is treated as
// degenerate.
const NormalEpsilon = 1e-9

// ValidateSemantics returns a *SemanticError if any SEM-* rule is violated.
//
// The dual pure/raising pair matches what schema validation established in part
// 1, deliberately: a caller aggregating its own errors wants the list, and a
// caller that just needs to reject a document wants the one-liner.
func ValidateSemantics(document Value, source string) error {
	problems := SemanticProblems(document)
	if len(problems) > 0 {
		return NewSemanticError(problems, source)
	}
	return nil
}

// SemanticProblems returns one message per violation, each naming its rule.
//
// The order is fixed: finiteness, frame, then per-entity ids and brushes, then
// references, worldspawn and identity policy, then groups and authorship. Order is not arbitrary — the
// frozen corpus test compares problem lists element by element, so a reordering
// shows up as a diff rather than passing silently.
func SemanticProblems(document Value) []string {
	contract, _ := CurrentContract()
	// A background context cannot cancel, so the error is always nil.
	problems, _ := semanticProblems(context.Background(), contract, document)
	return problems
}

// SemanticReport is the semantic layer against an explicit contract: the fatal problems, the
// non-fatal warnings, and the SEM-8 / SEM-10 subset of the problems. Cancellation stops it between
// entities with ctx's Err.
func SemanticReport(ctx context.Context, contract *Contract, document Value) (problems, warnings, identity []string, err error) {
	problems, err = semanticProblems(ctx, contract, document)
	if err != nil {
		return nil, nil, nil, err
	}
	return problems, semanticWarnings(contract, document), IdentityProblems(document), nil
}

func semanticProblems(ctx context.Context, contract *Contract, document Value) ([]string, error) {
	object, ok := document.(*Object)
	if !ok {
		return []string{"SEM-0 an APMap document must be a JSON object"}, nil
	}

	problems := []string{}
	problems = append(problems, finiteProblems(object, "$")...)
	problems = append(problems, frameProblems(object)...)

	// declared maps an object id to where it was first seen, which is what
	// makes the SEM-1 message able to name both sites.
	declared := map[string]string{}
	order := []string{}

	for entityIndex, entry := range arrayMember(object, "entities") {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entity, ok := entry.(*Object)
		if !ok {
			continue
		}
		where := fmt.Sprintf("entities[%d]", entityIndex)
		claim(entity, "entity_id", where+".entity_id", declared, &order, &problems)

		for itemIndex, contentEntry := range arrayMember(entity, "content") {
			item, ok := contentEntry.(*Object)
			if !ok {
				continue
			}
			if kind, _ := item.Get("kind"); kind != "brush" {
				continue
			}
			brushWhere := fmt.Sprintf("%s.content[%d]", where, itemIndex)
			claim(item, "brush_id", brushWhere+".brush_id", declared, &order, &problems)
			problems = append(problems, brushProblems(item, brushWhere, declared, &order)...)
		}
	}

	problems = append(problems, referenceProblems(object, declared)...)
	problems = append(problems, worldspawnProblems(object)...)
	problems = append(problems, identityProblems(object)...)
	problems = append(problems, quake3Problems(object)...)
	// Last, so the problem list of a document without groups or authorship is exactly what it was
	// before these rules existed (semantics_groups.go).
	fatal, _ := splitLegacyContainment(contract, object, groupProblems(object))
	problems = append(problems, fatal...)
	problems = append(problems, authorshipProblems(object)...)
	return problems, nil
}

// --- SEM-1: unique object ids ----------------------------------------------

// claim records an id and reports a duplicate. A missing or non-string id is
// the schema layer's finding, so it is skipped silently here.
func claim(holder *Object, member, where string, declared map[string]string, order *[]string, problems *[]string) {
	value, present := holder.Get(member)
	if !present {
		return
	}
	objectID, ok := value.(string)
	if !ok || objectID == "" {
		return
	}
	if first, seen := declared[objectID]; seen {
		*problems = append(*problems,
			fmt.Sprintf("SEM-1 duplicate object id %s: declared at %s and %s",
				strconv.Quote(objectID), first, where))
		return
	}
	declared[objectID] = where
	*order = append(*order, objectID)
}

// --- SEM-3, SEM-5, SEM-6: faces and brush geometry -------------------------

func brushProblems(brush *Object, where string, declared map[string]string, order *[]string) []string {
	problems := []string{}
	var planes []plane
	degenerate := false

	for faceIndex, entry := range arrayMember(brush, "faces") {
		face, ok := entry.(*Object)
		if !ok {
			continue
		}
		faceWhere := fmt.Sprintf("%s.faces[%d]", where, faceIndex)
		claim(face, "face_id", faceWhere+".face_id", declared, order, &problems)

		// SEM-3: synthetic and synthetic_reason travel together, and synthetic
		// must be exactly true — not merely present, and not merely truthy.
		hasFlag := face.Has("synthetic")
		hasReason := face.Has("synthetic_reason")
		if hasFlag || hasReason {
			if !(hasFlag && hasReason) {
				problems = append(problems,
					fmt.Sprintf("SEM-3 %s: synthetic and synthetic_reason must appear together", faceWhere))
			} else if value, _ := face.Get("synthetic"); value != true {
				problems = append(problems,
					fmt.Sprintf("SEM-3 %s: synthetic must be true when present", faceWhere))
			}
		}

		points, ok := planePoints(face)
		if !ok {
			// Unreadable geometry is the schema layer's finding, but it also
			// means this brush's boundedness cannot be judged — so the SEM-6
			// check below is suppressed rather than run on partial planes.
			degenerate = true
			continue
		}
		normal := cross(sub(points[1], points[0]), sub(points[2], points[0]))
		if norm(normal) < NormalEpsilon {
			problems = append(problems,
				fmt.Sprintf("SEM-5 %s: the three plane points are collinear", faceWhere))
			degenerate = true
			continue
		}
		planes = append(planes, plane{normal: normal, offset: dot(normal, points[0])})
	}

	if len(planes) > 0 && !degenerate && !bounded(planes) {
		problems = append(problems,
			fmt.Sprintf("SEM-6 %s: the faces do not enclose a finite volume", where))
	}
	return problems
}

type vector [3]float64

type plane struct {
	normal vector
	offset float64
}

// planePoints reads a face's three plane points, or reports that they cannot be
// read. The plane must be an object, points must
// be a three-element array, each point a three-element array of finite,
// non-boolean numbers.
func planePoints(face *Object) ([3]vector, bool) {
	var out [3]vector

	planeValue, present := face.Get("plane")
	if !present {
		return out, false
	}
	planeObject, ok := planeValue.(*Object)
	if !ok {
		return out, false
	}
	pointsValue, present := planeObject.Get("points")
	if !present {
		return out, false
	}
	points, ok := pointsValue.([]Value)
	if !ok || len(points) != 3 {
		return out, false
	}

	for index, entry := range points {
		components, ok := entry.([]Value)
		if !ok || len(components) != 3 {
			return out, false
		}
		for axis, component := range components {
			// A JSON boolean is not a number, in Python because bool subclasses
			// int and the check is explicit, here because the types differ.
			number, ok := component.(json.Number)
			if !ok {
				return out, false
			}
			value, err := strconv.ParseFloat(string(number), 64)
			if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
				return out, false
			}
			out[index][axis] = value
		}
	}
	return out, true
}

// bounded reports whether the half-space intersection is a finite volume.
//
// A **necessary, not sufficient** condition, checked directly: for each of the
// six axis directions some face normal must have a positive component, or the
// solid runs to infinity that way.
//
// This is deliberately weaker than full convex reconstruction, and the stronger
// check would be a bug rather than an improvement: SEM-6 is defined as this
// sweep, so a stronger check would reject documents the rule accepts, and the
// codec must stay usable on a document that never touched a geometry pipeline
// at all.
func bounded(planes []plane) bool {
	for axis := 0; axis < 3; axis++ {
		for _, sign := range []float64{1, -1} {
			found := false
			for _, p := range planes {
				if sign*p.normal[axis] > NormalEpsilon {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

// --- SEM-2: reference integrity --------------------------------------------

func referenceProblems(document *Object, declared map[string]string) []string {
	problems := []string{}
	for index, entry := range arrayMember(document, "relationships") {
		relationship, ok := entry.(*Object)
		if !ok {
			continue
		}
		for _, end := range []string{"from", "to"} {
			value, present := relationship.Get(end)
			if !present {
				continue
			}
			target, ok := value.(string)
			if !ok {
				continue
			}
			if _, known := declared[target]; !known {
				problems = append(problems,
					fmt.Sprintf("SEM-2 relationships[%d].%s: %s is not declared in this document",
						index, end, strconv.Quote(target)))
			}
		}
	}
	return problems
}

// --- SEM-4: frame consistency ----------------------------------------------

func frameProblems(document *Object) []string {
	frameValue, present := document.Get("frame")
	if !present {
		return nil
	}
	frame, ok := frameValue.(*Object)
	if !ok {
		return nil
	}
	space, _ := frame.Get("space")
	transformValue, present := frame.Get("to_source")
	if !present {
		return nil
	}
	transform, ok := transformValue.(*Object)
	if !ok {
		return nil
	}
	if space == "source_world" {
		if kind, _ := transform.Get("type"); kind != "identity" {
			return []string{"SEM-4 frame: space is source_world, so to_source must be the identity transform"}
		}
	}
	return nil
}

// --- SEM-9: worldspawn ------------------------------------------------------

func worldspawnProblems(document *Object) []string {
	if role, _ := document.Get("role"); role != "full_map" {
		return nil
	}
	entities := arrayMember(document, "entities")
	if len(entities) == 0 {
		// An empty full map is explicitly legal. Tightening this would reject
		// a document the specification admits.
		return nil
	}

	names := make([]string, 0, len(entities))
	for _, entry := range entities {
		entity, ok := entry.(*Object)
		if !ok {
			// Non-objects are filtered out of the list entirely, so they shift
			// the "first entity" position rather than occupying it.
			continue
		}
		names = append(names, classnameOf(entity))
	}

	count := 0
	for _, name := range names {
		if name == "worldspawn" {
			count++
		}
	}
	if count != 1 {
		return []string{fmt.Sprintf("SEM-9 a full_map needs exactly one worldspawn entity, found %d", count)}
	}
	if len(names) == 0 || names[0] != "worldspawn" {
		return []string{"SEM-9 worldspawn must be the first entity of a full_map"}
	}
	return nil
}

// classnameOf returns the entity's classname property, or "" when it has none —
// which is Python's None, and compares unequal to "worldspawn" the same way.
func classnameOf(entity *Object) string {
	for _, entry := range arrayMember(entity, "content") {
		item, ok := entry.(*Object)
		if !ok {
			continue
		}
		if kind, _ := item.Get("kind"); kind != "property" {
			continue
		}
		if key, _ := item.Get("key"); key != "classname" {
			continue
		}
		value, _ := item.Get("value")
		text, ok := value.(string)
		if !ok {
			return ""
		}
		return text
	}
	return ""
}

// --- SEM-8 / SEM-10: identity policy ---------------------------------------

// IdentityProblems is the SEM-8 / SEM-10 pass on its own.
//
// SemanticProblems is a concatenation of several passes, and a caller that wants
// to know WHICH KIND of semantic failure a document has cannot get that by
// reading the messages: a classifier built on message prefixes is a taxonomy
// that reshuffles itself the day somebody rewords one.
//
// The distinction is worth having because the two kinds have different fixes and
// different sentences. A geometry failure — collinear plane points, faces
// bounding no volume — is *"check the source brushes"*. An identity failure is a
// document whose geometry is fine and whose provenance claim is not, which is
// *"this document does not meet the contract it declares"* and sends nobody to
// look at their brushes. `AUP 120G` is the incident: an account map edited by a
// minting operation failed SEM-10, and the reader was told the extractor could
// not read the file.
//
// It runs the same pass SemanticProblems runs, so the two can never disagree.
func IdentityProblems(document Value) []string {
	object, ok := document.(*Object)
	if !ok {
		return nil
	}

	return identityProblems(object)
}

// identityProblems is what makes `id_policy: "derived"` checkable rather than a
// trusted claim. It is the reason ExpectedID's signature was settled in part 2.
func identityProblems(document *Object) []string {
	if policy, _ := document.Get("id_policy"); policy != "derived" {
		return nil
	}

	problems := []string{}
	for _, object := range identifiedObjects(document) {
		if object.derivedFrom == nil {
			problems = append(problems,
				fmt.Sprintf("SEM-8 %s: id_policy is derived but no derived_from is recorded", object.where))
			continue
		}
		// MandatedID rather than ExpectedID: SEM-10 treats "mandates nothing"
		// and "the record is malformed" alike. The extra information
		// ExpectedID carries is for a caller that wants it.
		mandated := MandatedID(object.kind, object.derivedFrom)
		if mandated != "" && mandated != object.id {
			problems = append(problems,
				fmt.Sprintf("SEM-10 %s: %s does not match its derived_from identity (expected %s)",
					object.where, strconv.Quote(object.id), strconv.Quote(mandated)))
		}
	}
	return problems
}

type identifiedObject struct {
	id          string
	kind        Kind
	derivedFrom Value
	where       string
}

// identifiedObjects walks every entity, brush, face and patch carrying a string id, in
// document order.
func identifiedObjects(document *Object) []identifiedObject {
	var out []identifiedObject

	for entityIndex, entry := range arrayMember(document, "entities") {
		entity, ok := entry.(*Object)
		if !ok {
			continue
		}
		where := fmt.Sprintf("entities[%d]", entityIndex)
		if id, ok := stringMember(entity, "entity_id"); ok {
			out = append(out, identifiedObject{
				id: id, kind: KindEntity, derivedFrom: optionalMember(entity, "derived_from"), where: where,
			})
		}

		for itemIndex, contentEntry := range arrayMember(entity, "content") {
			item, ok := contentEntry.(*Object)
			if !ok {
				continue
			}
			kind, _ := item.Get("kind")
			if kind == "patch" {
				// A patch is an identified object like a brush (§3.5, kind "patch", path e<n>.p<n>),
				// and has no sub-objects.
				if id, ok := stringMember(item, "patch_id"); ok {
					out = append(out, identifiedObject{
						id: id, kind: KindPatch, derivedFrom: optionalMember(item, "derived_from"),
						where: fmt.Sprintf("%s.content[%d]", where, itemIndex),
					})
				}
				continue
			}
			if kind != "brush" {
				continue
			}
			brushWhere := fmt.Sprintf("%s.content[%d]", where, itemIndex)
			if id, ok := stringMember(item, "brush_id"); ok {
				out = append(out, identifiedObject{
					id: id, kind: KindBrush, derivedFrom: optionalMember(item, "derived_from"), where: brushWhere,
				})
			}
			for faceIndex, faceEntry := range arrayMember(item, "faces") {
				face, ok := faceEntry.(*Object)
				if !ok {
					continue
				}
				if id, ok := stringMember(face, "face_id"); ok {
					out = append(out, identifiedObject{
						id: id, kind: KindFace, derivedFrom: optionalMember(face, "derived_from"),
						where: fmt.Sprintf("%s.faces[%d]", brushWhere, faceIndex),
					})
				}
			}
		}
	}
	return out
}

// --- SEM-11: finiteness -----------------------------------------------------

// finiteProblems walks the whole document reporting any non-finite number.
//
// See this file's header for why an integer literal is exempt: an integer is
// arbitrary precision and cannot be infinite, so only non-integer literals are
// tested.
func finiteProblems(node Value, where string) []string {
	switch typed := node.(type) {
	case json.Number:
		if isIntegerLiteral(typed) {
			return nil
		}
		value, err := strconv.ParseFloat(string(typed), 64)
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
			// The *decoded* value, not the literal — `inf`, where the document
			// says `1e400`. The literal would be marginally more useful, but the
			// path already names exactly where to look, and the message text is
			// part of the SEM-11 output other tools and the frozen corpus test
			// compare, so it stays as it is.
			return []string{fmt.Sprintf("SEM-11 %s: %s is not a finite number", where, pythonFloatName(value, err))}
		}
		return nil
	case *Object:
		var problems []string
		for _, key := range typed.Keys() {
			member, _ := typed.Get(key)
			problems = append(problems, finiteProblems(member, where+"."+key)...)
		}
		return problems
	case []Value:
		var problems []string
		for index, item := range typed {
			problems = append(problems, finiteProblems(item, fmt.Sprintf("%s[%d]", where, index))...)
		}
		return problems
	default:
		// Strings, booleans and null carry no number.
		return nil
	}
}

// --- defensive accessors ----------------------------------------------------

// arrayMember reads a member expected to be an array, returning nothing when it
// is absent, null, or the wrong type — an explicit null reads as an empty list —
// in one place rather than at every call site.
func arrayMember(object *Object, name string) []Value {
	value, present := object.Get(name)
	if !present {
		return nil
	}
	items, ok := value.([]Value)
	if !ok {
		return nil
	}
	return items
}

func stringMember(object *Object, name string) (string, bool) {
	value, present := object.Get(name)
	if !present {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

// optionalMember returns the member or nil when absent. The distinction matters
// for SEM-8: an absent derived_from is a violation, where a present one that
// mandates nothing is not.
func optionalMember(object *Object, name string) Value {
	value, present := object.Get(name)
	if !present {
		return nil
	}
	return value
}

// --- vector math ------------------------------------------------------------
//
// Ported directly rather than reaching for a geometry package: these four
// functions are the whole of it, and mapfile's geometry is a different
// abstraction serving `.map` brush construction, not plane arithmetic over
// decoded JSON.

func sub(a, b vector) vector {
	return vector{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func cross(a, b vector) vector {
	return vector{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func dot(a, b vector) float64 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

func norm(a vector) float64 {
	return math.Sqrt(dot(a, a))
}

// pythonFloatName spells a non-finite float the way Python's repr does (`inf`,
// `-inf`, `nan`), which is the spelling of the SEM-11 message.
//
// err is the ParseFloat error, which for a literal beyond float64's range is
// strconv.ErrRange with the value already saturated to the correctly-signed
// infinity — so the sign is read off the saturated value rather than the text.
func pythonFloatName(value float64, err error) string {
	if err != nil && !math.IsInf(value, 0) && !math.IsNaN(value) {
		// Unparseable for some other reason; nothing better to say than what
		// the parser saw.
		return "nan"
	}
	switch {
	case math.IsNaN(value):
		return "nan"
	case math.IsInf(value, 1):
		return "inf"
	case math.IsInf(value, -1):
		return "-inf"
	default:
		return strconv.FormatFloat(value, 'g', -1, 64)
	}
}
