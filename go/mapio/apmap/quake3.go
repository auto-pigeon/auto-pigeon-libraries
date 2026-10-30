package apmap

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The Quake III surfaces APMap 1.5 added (AULIBS ts/apmap-schema SEMANTICS.md §6.2a, §9c): a
// `patch` entity content item (a patchDef2 bezier control grid) and a `brush_primitives` face
// projection (a brushDef 2×3 texture matrix, kept verbatim).
//
// This file holds the two things this package does with them today:
//
//   - VALIDATES them — SEM-Q-1 … SEM-Q-5, ported rule for rule from test/helpers.mjs
//     `semanticFaults`, the reference implementation the AULIBS `semantic_invalid` vectors are
//     proved against. SEM-G-1/2/4/5 treat a patch as an object its entity owns
//     (semantics_groups.go), and SEM-8/SEM-10 derive a patch id like any other (semantics.go).
//   - REFUSES them wherever it cannot preserve them. The Quake 1 / Quake 2 `.map` model
//     (mapfile) has no patch and no texture matrix, so lowering a document into it would
//     drop the patch or turn the matrix into a classic projection. SEMANTICS.md §9c's rule for a
//     component that cannot preserve a patch is to refuse the document by name before changing
//     anything — never strip it, flatten it, or write the rest without it — and that is what
//     Quake3SurfaceRefusal does. The Quake III `.map` writer is quake3map.go (Q3_004,
//     `apmap-to-q3map`); it never lowers through the Quake 1 model.

// The two kinds of Quake III surface a refusal can name.
const (
	quake3SurfacePatch          = "patch"
	quake3SurfaceBrushPrimitive = "brush_primitives face"
)

// Quake3Surface is one Quake III surface found in a document.
type Quake3Surface struct {
	// Kind is "patch" or "brush_primitives face".
	Kind string
	// ID is the patch_id or face_id ("" when the document does not carry one).
	ID string
	// Owner is the brush_id a brush_primitives face belongs to ("" for a patch).
	Owner string
	// Where is the object's location, entities[e].content[c] or entities[e].content[c].faces[f].
	Where string
}

func (s Quake3Surface) name() string {
	id := s.ID
	if id == "" {
		id = "(no id)"
	}
	switch s.Kind {
	case quake3SurfacePatch:
		return fmt.Sprintf("Quake III patch %s at %s", id, s.Where)
	default:
		owner := ""
		if s.Owner != "" {
			owner = " of brush " + s.Owner
		}
		return fmt.Sprintf("Quake III brush-primitives face %s%s at %s", id, owner, s.Where)
	}
}

// FindQuake3Surfaces returns every patch and every brush_primitives face in the document, in
// document order. It is defensive: a malformed node is skipped, never a panic.
func FindQuake3Surfaces(document Value) []Quake3Surface {
	root, ok := document.(*Object)
	if !ok {
		return nil
	}
	var found []Quake3Surface
	for entityIndex, entry := range arrayMember(root, "entities") {
		entity, ok := entry.(*Object)
		if !ok {
			continue
		}
		for itemIndex, contentEntry := range arrayMember(entity, "content") {
			item, ok := contentEntry.(*Object)
			if !ok {
				continue
			}
			where := fmt.Sprintf("entities[%d].content[%d]", entityIndex, itemIndex)
			switch kind, _ := item.Get("kind"); kind {
			case "patch":
				id, _ := stringMember(item, "patch_id")
				found = append(found, Quake3Surface{Kind: quake3SurfacePatch, ID: id, Where: where})
			case "brush":
				brushID, _ := stringMember(item, "brush_id")
				for faceIndex, faceEntry := range arrayMember(item, "faces") {
					face, ok := faceEntry.(*Object)
					if !ok || !isBrushPrimitivesFace(face) {
						continue
					}
					id, _ := stringMember(face, "face_id")
					found = append(found, Quake3Surface{
						Kind: quake3SurfaceBrushPrimitive, ID: id, Owner: brushID,
						Where: fmt.Sprintf("%s.faces[%d]", where, faceIndex),
					})
				}
			}
		}
	}
	return found
}

// isBrushPrimitivesFace reports whether a face's projection is the 1.5 texture-matrix shape.
func isBrushPrimitivesFace(face *Object) bool {
	projection, ok := optionalMember(face, "projection").(*Object)
	if !ok || projection == nil {
		return false
	}
	mode, _ := projection.Get("mode")
	return mode == "brush_primitives"
}

// Quake3UnsupportedError is the explicit refusal of a document holding a Quake III surface that an
// operation cannot yet preserve. It names the first such surface and how many there are, and it is
// returned before the operation produces anything: there is never a truncated output.
type Quake3UnsupportedError struct {
	// Operation is what was refused, e.g. "APMap -> .map conversion".
	Operation string
	// First is the first Quake III surface in document order.
	First Quake3Surface
	// Count is how many Quake III surfaces the document holds.
	Count int
}

func (e *Quake3UnsupportedError) Error() string {
	more := ""
	if e.Count > 1 {
		more = fmt.Sprintf(" (and %d more Quake III surface(s))", e.Count-1)
	}
	return fmt.Sprintf("%s refused: the document holds %s%s; this operation writes a Quake 1/Quake 2 "+
		"form with no place for Quake III patches and brush-primitives faces, so the whole document is "+
		"refused rather than written without them (a Quake III document converts with apmap-to-q3map)",
		e.Operation, e.First.name(), more)
}

func (e *Quake3UnsupportedError) Unwrap() error { return ErrAPMap }

// Quake3SurfaceRefusal returns a *Quake3UnsupportedError when document holds any patch or
// brush_primitives face, and nil otherwise. operation names what is being refused.
func Quake3SurfaceRefusal(document Value, operation string) error {
	found := FindQuake3Surfaces(document)
	if len(found) == 0 {
		return nil
	}
	return &Quake3UnsupportedError{Operation: operation, First: found[0], Count: len(found)}
}

// --- SEM-Q-1 … SEM-Q-5 ------------------------------------------------------

// quake3Problems is the SEM-Q pass. A document with neither surface gets nothing from it, so every
// problem list produced before 1.5 is unchanged.
func quake3Problems(document *Object) []string {
	problems := []string{}
	game, _ := document.Get("game")
	gameName, _ := game.(string)
	quake3 := gameName == "quake3"
	gameText := render(game)

	patchAt := map[string]string{}
	for entityIndex, entry := range arrayMember(document, "entities") {
		entity, ok := entry.(*Object)
		if !ok {
			continue
		}
		for itemIndex, contentEntry := range arrayMember(entity, "content") {
			item, ok := contentEntry.(*Object)
			if !ok {
				continue
			}
			where := fmt.Sprintf("entities[%d].content[%d]", entityIndex, itemIndex)
			switch kind, _ := item.Get("kind"); kind {
			case "patch":
				id, _ := stringMember(item, "patch_id")
				// SEM-Q-1: a patch id is unique across the document. The typed prefix keeps it
				// apart from every other kind, so a collision can only be with another patch.
				if id != "" {
					if first, seen := patchAt[id]; seen {
						problems = append(problems, fmt.Sprintf(
							"SEM-Q-1 duplicate patch id %s: declared at %s and %s",
							strconv.Quote(id), first, where))
					} else {
						patchAt[id] = where
					}
				}
				problems = append(problems, patchGridProblems(item, where)...)
				// SEM-Q-4: a Quake III surface only in a Quake III document.
				if !quake3 {
					problems = append(problems, fmt.Sprintf(
						"SEM-Q-4 %s: a patch appears in a document whose game is %s, not \"quake3\"",
						where, gameText))
				}
			case "brush":
				faces := arrayMember(item, "faces")
				matrices := 0
				for faceIndex, faceEntry := range faces {
					face, ok := faceEntry.(*Object)
					if !ok || !isBrushPrimitivesFace(face) {
						continue
					}
					matrices++
					faceWhere := fmt.Sprintf("%s.faces[%d]", where, faceIndex)
					// SEM-Q-3: the matrix's 2×2 part is invertible.
					if degenerateMatrix(face) {
						problems = append(problems, fmt.Sprintf(
							"SEM-Q-3 %s: the brush_primitives texture matrix is degenerate "+
								"(m00·m11 − m01·m10 = 0)", faceWhere))
					}
					if !quake3 {
						problems = append(problems, fmt.Sprintf(
							"SEM-Q-4 %s: a brush_primitives projection appears in a document whose game is %s, not \"quake3\"",
							faceWhere, gameText))
					}
				}
				// SEM-Q-5: one syntax per brush.
				if matrices > 0 && matrices < len(faces) {
					problems = append(problems, fmt.Sprintf(
						"SEM-Q-5 %s: %d of the brush's %d faces are brush_primitives; a brush's faces are "+
							"all brush_primitives or none of them is", where, matrices, len(faces)))
				}
			}
		}
	}
	return problems
}

// patchGridProblems is SEM-Q-2: the grid is exactly `width` columns of exactly `height` points.
// Dimensions the schema would refuse (absent, not an integer) are the schema's finding.
func patchGridProblems(patch *Object, where string) []string {
	width, okWidth := integerValue(optionalMember(patch, "width"))
	height, okHeight := integerValue(optionalMember(patch, "height"))
	columnsValue, present := patch.Get("control_points")
	columns, isArray := columnsValue.([]Value)
	if !okWidth || !okHeight || !present || !isArray {
		return nil
	}
	ragged := len(columns) != width
	lengths := []string{}
	seen := map[int]bool{}
	for _, column := range columns {
		points, ok := column.([]Value)
		length := len(points)
		if !ok {
			length = -1
		}
		if length != height {
			ragged = true
		}
		if !seen[length] {
			seen[length] = true
			lengths = append(lengths, strconv.Itoa(length))
		}
	}
	if !ragged {
		return nil
	}
	return []string{fmt.Sprintf(
		"SEM-Q-2 %s: the patch declares %dx%d but its grid is %d column(s) of %s point(s)",
		where, width, height, len(columns), strings.Join(lengths, "/"))}
}

// integerValue reads a JSON number that is mathematically an integer.
func integerValue(value Value) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(string(number), 64)
	if err != nil || parsed != math.Trunc(parsed) || math.Abs(parsed) > 1<<30 {
		return 0, false
	}
	return int(parsed), true
}

// degenerateMatrix is SEM-Q-3's test, in IEEE-754 double precision on the numbers as written:
// m00·m11 − m01·m10 == 0. Each product is rounded to float64 explicitly, which the Go
// specification guarantees prevents a fused multiply-add, so every architecture computes the same
// determinant the reference implementation (JavaScript doubles) does. An unreadable matrix is the
// schema's finding, not this rule's.
func degenerateMatrix(face *Object) bool {
	projection, ok := optionalMember(face, "projection").(*Object)
	if !ok || projection == nil {
		return false
	}
	rows, ok := optionalMember(projection, "matrix").([]Value)
	if !ok || len(rows) != 2 {
		return false
	}
	var m [2][2]float64
	for r, row := range rows {
		coefficients, ok := row.([]Value)
		if !ok || len(coefficients) != 3 {
			return false
		}
		for c := 0; c < 2; c++ {
			number, ok := coefficients[c].(json.Number)
			if !ok {
				return false
			}
			value, err := strconv.ParseFloat(string(number), 64)
			if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
				return false
			}
			m[r][c] = value
		}
	}
	return float64(m[0][0]*m[1][1])-float64(m[0][1]*m[1][0]) == 0
}
