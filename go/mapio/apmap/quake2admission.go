package apmap

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// QUAKE II BUILD ADMISSION — the AULIBS contract's ADM-Q2-1..4 (SEMANTICS.md §6.3a, Q2_001).
//
// These are not document-validity rules. APMap 1.5's face `tail` is any number of finite numbers,
// because 1.0–1.4 documents were written that way and are frozen, so a document that trips one of
// these is a VALID document every reader keeps byte for byte. What it is not is a `.map` a Quake II
// compiler reads the way its author meant, measured on the pinned EricW builds:
//
//	ADM-Q2-1  a tail of other than 3 numbers  4 numbers: "Invalid brush plane format"; 1 or 2: EricW
//	                                          fills zeros, id's qbsp3 reads the next line's tokens
//	ADM-Q2-2  a tail number that is not an    `1.5` is truncated to 1 by std::stoi, silently
//	          integer
//	ADM-Q2-3  outside signed 32-bit           `2147483648` crashes fork 1.4518 (stoi) and is wrapped
//	                                          to 0x80000000 by alpha7: one map, two results
//	ADM-Q2-4  a texture or resource key that  the compiler opens what it is given
//	          leaves the game root
//
// So apmap-to-q2map refuses the whole document before writing a byte, naming the face or the entity
// key, and never rewrites a tail into something a compiler would take. An absent tail is not a fault:
// it means "use the WAL header", which the compiler does.

// Stable refusal codes, one per rule.
const (
	Q2MapCodeTailArity       = "q2map_tail_arity"
	Q2MapCodeTailNotInteger  = "q2map_tail_not_integer"
	Q2MapCodeTailRange       = "q2map_tail_out_of_int32"
	Q2MapCodeReferenceUnsafe = "q2map_reference_unsafe"
)

// Quake2AdmissionError is one refused document: the first fault in document order.
type Quake2AdmissionError struct {
	Rule     string
	Code     string
	ObjectID string
	Key      string
	Message  string
}

func (e *Quake2AdmissionError) Error() string {
	where := e.ObjectID
	if e.Key != "" {
		where += " key " + e.Key
	}
	return fmt.Sprintf("apmap-to-q2map refused [%s, %s]: %s: %s; nothing was written", e.Code, e.Rule, where, e.Message)
}

func (e *Quake2AdmissionError) Unwrap() error { return ErrAPMap }

const (
	int32Min = -2147483648
	int32Max = 2147483647
)

// quake2ResourceKeys are the entity keys a Quake II build resolves to a file under the game root.
var quake2ResourceKeys = map[string]bool{"model": true, "noise": true, "sky": true}

// unsafeQuake2Reference reports a name that would resolve outside the game root: an absolute path,
// a drive, or an empty, `.` or `..` segment.
func unsafeQuake2Reference(name string) bool {
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) ||
		(len(name) >= 2 && name[1] == ':' && ((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z'))) {
		return true
	}
	for _, segment := range strings.Split(strings.ReplaceAll(name, `\`, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

// Quake2Admission returns a *Quake2AdmissionError for the first ADM-Q2 fault in document order, or
// nil. It reads the decoded document and changes nothing.
func Quake2Admission(document Value) error {
	root, ok := document.(*Object)
	if !ok {
		return nil
	}
	entities, _ := root.Get("entities")
	list, _ := entities.([]Value)
	for _, rawEntity := range list {
		entity, ok := rawEntity.(*Object)
		if !ok {
			continue
		}
		entityID := textMember(entity, "entity_id", "")
		content, _ := entity.Get("content")
		items, _ := content.([]Value)
		for _, rawItem := range items {
			item, ok := rawItem.(*Object)
			if !ok {
				continue
			}
			switch textMember(item, "kind", "") {
			case "property":
				key, value := textMember(item, "key", ""), textMember(item, "value", "")
				if quake2ResourceKeys[key] && value != "" && !strings.HasPrefix(value, "*") && unsafeQuake2Reference(value) {
					return &Quake2AdmissionError{Rule: "ADM-Q2-4", Code: Q2MapCodeReferenceUnsafe, ObjectID: entityID, Key: key,
						Message: fmt.Sprintf("%q would resolve outside the game root", value)}
				}
			case "brush":
				if err := quake2BrushAdmission(item); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func quake2BrushAdmission(brush *Object) error {
	faces, _ := brush.Get("faces")
	list, _ := faces.([]Value)
	for _, rawFace := range list {
		face, ok := rawFace.(*Object)
		if !ok {
			continue
		}
		faceID := textMember(face, "face_id", "")
		if texture := textMember(face, "texture", ""); unsafeQuake2Reference(texture) {
			return &Quake2AdmissionError{Rule: "ADM-Q2-4", Code: Q2MapCodeReferenceUnsafe, ObjectID: faceID,
				Message: fmt.Sprintf("texture %q would resolve outside the game root", texture)}
		}
		raw, present := face.Get("tail")
		if !present || raw == nil {
			continue
		}
		values, _ := raw.([]Value)
		if len(values) != 3 {
			return &Quake2AdmissionError{Rule: "ADM-Q2-1", Code: Q2MapCodeTailArity, ObjectID: faceID,
				Message: fmt.Sprintf("a Quake II face tail is CONTENTS SURFACE_FLAGS VALUE, 3 integers; this one has %d", len(values))}
		}
		for index, value := range values {
			literal, ok := value.(json.Number)
			if !ok {
				return &Quake2AdmissionError{Rule: "ADM-Q2-2", Code: Q2MapCodeTailNotInteger, ObjectID: faceID,
					Message: fmt.Sprintf("tail[%d] is not a number", index)}
			}
			exact, ok := new(big.Rat).SetString(literal.String())
			if !ok || !exact.IsInt() {
				return &Quake2AdmissionError{Rule: "ADM-Q2-2", Code: Q2MapCodeTailNotInteger, ObjectID: faceID,
					Message: fmt.Sprintf("tail[%d] = %s is not an integer, and a Quake II compiler would truncate it silently", index, literal)}
			}
			integer := exact.Num()
			if !integer.IsInt64() || integer.Int64() < int32Min || integer.Int64() > int32Max {
				hint := ""
				if integer.IsInt64() && integer.Int64() > int32Max && integer.Int64() <= 4294967295 {
					hint = fmt.Sprintf(" (its signed 32-bit spelling is %d)", int32(uint32(integer.Int64())))
				}
				return &Quake2AdmissionError{Rule: "ADM-Q2-3", Code: Q2MapCodeTailRange, ObjectID: faceID,
					Message: fmt.Sprintf("tail[%d] = %s does not fit a signed 32-bit int%s", index, literal, hint)}
			}
		}
	}
	return nil
}
