package apmap

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Semantic validation of `groups` (SEM-G-1 … SEM-G-9) and of `authorship` (SEM-A-1).
//
// The rules are AULIBS' (ts/apmap-schema/SEMANTICS.md §11), and test/helpers.mjs `semanticFaults`
// is their reference implementation; this is a port of it, rule for rule, and the AULIBS
// `semantic_invalid` vectors are what vectors_1_4_test.go measures it against. None of them can be
// said in JSON Schema, because every one is a statement about the rest of the document.
//
// They apply to every document that has groups, whatever version it declares. SEM-G-1 … SEM-G-5
// have bound since 1.2; what 1.4 changed is the reach of SEM-G-4 (a GROUP has one parent too) and
// SEM-G-5 (containment across a whole tree, not one member list), and it added SEM-G-8/9. A 1.2 or
// 1.3 document cannot express a `group` member at all — its frozen schema has no such arm — so for
// one of those every group is its own root, and the tree rules reduce exactly to the per-group
// rules its own version stated. One implementation is therefore right for every version.
//
// SEM-G-6 (the minimum member count) is the schema's `minItems`, per version, and is not repeated
// here. SEM-G-7 says `source.prefab_id` is an external identity that SEM-G-1 does not resolve,
// which is to say it is a rule about what this code must NOT do.
//
// # Iterative and linear, never recursive
//
// A hostile document may nest thousands of groups or close a cycle through all of them. Nothing
// here recurses: the group graph is walked upward along the one-parent map with a memo, so each
// group is visited a bounded number of times and the whole pass is linear in the members. A chain
// deeper than GroupMaxDepth is reported as SEM-G-9, and a cycle as SEM-G-8 — never as a stack
// overflow or a hang.

// # SEM-G-5 on a legacy document is a warning
//
// The frozen 1.2 and 1.3 corpora publish group-two-groups.apmap as VALID, and it holds a brush and
// that brush's own face in one group — a SEM-G-5 fault no schema could see and no reader of those
// versions checked. Legacy documents must stay readable: a map saved under a previous contract is
// not made unreadable by a rule its producers were never measured against. So SEM-G-5 is FATAL
// only for a document declaring the current version, and a NON-FATAL warning for one declaring a
// readable older version (SemanticWarnings), exactly as the AUP backend reports it. Every other
// SEM-G rule and SEM-A-1 stays fatal for every version, and a document declaring no version, or
// one this build does not read, gets no leniency: the version gate refuses it anyway.

// isLegacyDeclared reports whether the document declares a readable version older than current —
// one this build reads but does not write.
func isLegacyDeclared(contract *Contract, document *Object) bool {
	declared := declaredVersion(document)
	return declared != "" && contract != nil && contract.IsReadable(declared) && !contract.IsCurrent(declared)
}

// splitLegacyContainment separates the SEM-G-5 findings of a legacy-declared document, which are
// warnings, from everything else, which is fatal. For a current document nothing is a warning.
func splitLegacyContainment(contract *Contract, document *Object, problems []string) (fatal, warnings []string) {
	if !isLegacyDeclared(contract, document) {
		return problems, nil
	}
	for _, problem := range problems {
		if strings.HasPrefix(problem, "SEM-G-5 ") {
			warnings = append(warnings, problem)
		} else {
			fatal = append(fatal, problem)
		}
	}
	return fatal, warnings
}

// SemanticWarnings returns the non-fatal semantic findings: today exactly the SEM-G-5 containment
// faults of a document declaring a readable version older than the current one. They never make a
// document invalid; a caller with a place for advice (a conversion's warnings) reports them there.
func SemanticWarnings(document Value) []string {
	contract, _ := CurrentContract()
	return semanticWarnings(contract, document)
}

func semanticWarnings(contract *Contract, document Value) []string {
	object, ok := document.(*Object)
	if !ok {
		return nil
	}
	_, warnings := splitLegacyContainment(contract, object, groupProblems(object))
	return warnings
}

// GroupMaxDepth is the deepest a document may nest groups, counted in groups from a root to its
// deepest descendant (SEM-G-9). A root alone is depth 1.
const GroupMaxDepth = 32

// groupObject is one object a group member can name: its kind, and the object that owns it (an
// entity for a brush or a patch, a brush for a face, "" for an entity).
type groupObject struct {
	kind  string
	owner string
}

// groupProblems is the SEM-G pass. It returns nothing for a document without groups.
func groupProblems(document *Object) []string {
	declared := arrayMember(document, "groups")
	if len(declared) == 0 {
		return nil
	}
	problems := []string{}

	// The object index, first declaration winning: a duplicate id is SEM-1's finding, not this one.
	objects := map[string]groupObject{}
	index := func(id string, object groupObject) {
		if _, seen := objects[id]; !seen && id != "" {
			objects[id] = object
		}
	}
	for _, entry := range arrayMember(document, "entities") {
		entity, ok := entry.(*Object)
		if !ok {
			continue
		}
		entityID, _ := stringMember(entity, "entity_id")
		index(entityID, groupObject{kind: "entity"})
		for _, contentEntry := range arrayMember(entity, "content") {
			item, ok := contentEntry.(*Object)
			if !ok {
				continue
			}
			kind, _ := item.Get("kind")
			if kind == "patch" {
				// A patch is an object its entity owns, exactly like a brush (SEMANTICS.md §9c).
				patchID, _ := stringMember(item, "patch_id")
				index(patchID, groupObject{kind: "patch", owner: entityID})
				continue
			}
			if kind != "brush" {
				continue
			}
			brushID, _ := stringMember(item, "brush_id")
			index(brushID, groupObject{kind: "brush", owner: entityID})
			for _, faceEntry := range arrayMember(item, "faces") {
				if face, ok := faceEntry.(*Object); ok {
					faceID, _ := stringMember(face, "face_id")
					index(faceID, groupObject{kind: "face", owner: brushID})
				}
			}
		}
	}

	// SEM-G-3: every group_id once. Declaration order is kept, because Go's map order is random
	// and the problem list is compared element by element.
	type groupEntry struct {
		id      string
		where   string
		members []Value
	}
	var groups []groupEntry
	firstAt := map[string]string{}
	for groupIndex, entry := range declared {
		group, ok := entry.(*Object)
		if !ok {
			continue
		}
		where := fmt.Sprintf("groups[%d]", groupIndex)
		id, ok := stringMember(group, "group_id")
		if !ok || id == "" {
			continue // the schema's finding
		}
		if first, seen := firstAt[id]; seen {
			problems = append(problems, fmt.Sprintf("SEM-G-3 %s.group_id: %s is already declared at %s",
				where, strconv.Quote(id), first))
			continue
		}
		firstAt[id] = where
		groups = append(groups, groupEntry{id: id, where: where, members: arrayMember(group, "members")})
	}

	// SEM-G-1, SEM-G-2, SEM-G-4 and the self-link half of SEM-G-8, member by member. parentOf is
	// every direct member's ONE group — the first that lists it — which is what makes the graph
	// below a function and the walk over it linear.
	parentOf := map[string]string{}
	type directMember struct {
		id    string
		group string
	}
	var direct []directMember
	for _, group := range groups {
		for memberIndex, entry := range group.members {
			member, ok := entry.(*Object)
			if !ok {
				continue
			}
			where := fmt.Sprintf("%s.members[%d]", group.where, memberIndex)
			kind, _ := stringMember(member, "kind")
			field := map[string]string{
				"entity": "entity_id", "brush": "brush_id", "face": "face_id", "patch": "patch_id",
				"group": "group_id",
			}[kind]
			if field == "" {
				continue // the schema's finding
			}
			id, ok := stringMember(member, field)
			if !ok || id == "" {
				continue
			}
			if kind == "group" {
				if _, known := firstAt[id]; !known {
					problems = append(problems, fmt.Sprintf(
						"SEM-G-1 %s: group %s is not declared in this document", where, strconv.Quote(id)))
				}
				if id == group.id {
					problems = append(problems, fmt.Sprintf(
						"SEM-G-8 %s: group %s is a member of itself", where, strconv.Quote(id)))
				}
			} else if object, known := objects[id]; !known {
				problems = append(problems, fmt.Sprintf(
					"SEM-G-1 %s: %s %s is not declared in this document", where, kind, strconv.Quote(id)))
			} else if object.kind != kind {
				problems = append(problems, fmt.Sprintf(
					"SEM-G-2 %s: %s is a %s, not a %s", where, strconv.Quote(id), object.kind, kind))
			}
			if other, seen := parentOf[id]; seen {
				problems = append(problems, fmt.Sprintf(
					"SEM-G-4 %s: %s is a direct member of both %s and %s",
					where, strconv.Quote(id), strconv.Quote(other), strconv.Quote(group.id)))
				continue
			}
			parentOf[id] = group.id
			direct = append(direct, directMember{id: id, group: group.id})
		}
	}

	// SEM-G-8 (cycles) and SEM-G-9 (depth): follow each group's parent chain upward, memoized, so
	// a group is climbed past at most once. Revisiting a group within one climb is a cycle.
	depthOf := map[string]int{}
	rootOf := map[string]string{} // "" for a group on, or hanging from, a cycle: it has no root
	deepest := 0
	for _, group := range groups {
		var chain []string
		seen := map[string]bool{}
		at, climbing := group.id, true
		cyclic := false
		for climbing {
			if _, done := depthOf[at]; done {
				break
			}
			if seen[at] {
				cyclic = true
				// A self-link was reported member by member above; a longer cycle is reported
				// once, by the first group whose climb closes it.
				if at != group.id || len(chain) > 1 {
					problems = append(problems, fmt.Sprintf(
						"SEM-G-8 groups: the group graph has a cycle through %s", strconv.Quote(at)))
				}
				break
			}
			seen[at] = true
			chain = append(chain, at)
			at, climbing = parentOf[at]
		}
		depth, root := 0, ""
		switch {
		case cyclic:
		case climbing: // stopped at a group already measured
			depth, root = depthOf[at], rootOf[at]
		default: // reached a group with no parent: the root
			root = chain[len(chain)-1]
		}
		for position := len(chain) - 1; position >= 0; position-- {
			depth++
			depthOf[chain[position]] = depth
			rootOf[chain[position]] = root
		}
		if depth > deepest {
			deepest = depth
		}
	}
	if deepest > GroupMaxDepth {
		problems = append(problems, fmt.Sprintf(
			"SEM-G-9 groups: groups nest %d deep; at most %d is allowed", deepest, GroupMaxDepth))
	}

	// SEM-G-5 across the nesting. A tree's root reaches every object in the tree, so an object and
	// one of its own ancestor objects (face -> brush -> entity, patch -> entity) must not both be direct members
	// anywhere in ONE tree. Linear: compare the roots of the two direct parents. Two unrelated trees
	// may hold them, exactly as two unrelated 1.3 groups could.
	for _, member := range direct {
		object, isObject := objects[member.id]
		if !isObject {
			continue
		}
		root := rootOf[member.group]
		for up, hops := object.owner, 0; up != "" && hops < 2; hops++ {
			if other, listed := parentOf[up]; listed && root != "" && rootOf[other] == root {
				problems = append(problems, fmt.Sprintf(
					"SEM-G-5 groups: the group tree rooted at %s holds both %s and its descendant %s",
					strconv.Quote(root), strconv.Quote(up), strconv.Quote(member.id)))
			}
			up = objects[up].owner
		}
	}
	return problems
}

// authorshipProblems is SEM-A-1: every `authorship` string is Unicode text.
//
// Two ways a string can fail it. A document decoded from JSON whose text held an unpaired surrogate
// escape — `"\ud83d"` — reaches here with U+FFFD in its place, because encoding/json has no other
// way to spell it in a Go string; DecodeJSON recorded the fact from the raw bytes, and
// Object.HadUnpairedSurrogate reports it. A document built in memory can instead carry bytes that
// are not UTF-8 at all. Either way the only thing a writer could do is emit a replacement
// character, which silently changes the author's words, so the document is refused instead.
func authorshipProblems(document *Object) []string {
	value, present := document.Get("authorship")
	if !present {
		return nil
	}
	authorship, ok := value.(*Object)
	if !ok {
		return nil // the schema's finding
	}
	var problems []string
	for _, name := range authorship.Keys() {
		member, _ := authorship.Get(name)
		text, ok := member.(string)
		if !ok {
			continue
		}
		if authorship.HadUnpairedSurrogate(name) || !utf8.ValidString(text) {
			problems = append(problems, fmt.Sprintf(
				"SEM-A-1 authorship.%s: the text contains an unpaired surrogate (or is not UTF-8), "+
					"so it is not Unicode text and cannot be kept exactly as written", name))
		}
	}
	return problems
}
