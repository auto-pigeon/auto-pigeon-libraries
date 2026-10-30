package apmap

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// The validator is a small interpreter for the JSON Schema 2020-12 subset the
// APMap schema actually uses — not a general JSON Schema implementation.
//
// This module depends on the standard library alone, and taking a JSON Schema
// dependency to read one file would be a poor trade. The shortcut is
// verified against the published test vector corpus, which lives in
// auto-pigeon-libraries' apmap-schema package (`deprecated/1.0/test-vectors/`,
// reached by `mapperRoot` in the tests): every valid vector passes, and every
// SCH-* vector is blamed at the location vectors_test.go freezes.
//
// # The supported keyword set, and why it is what it is
//
// Implemented: type, const, enum, required,
// properties, additionalProperties, propertyNames, items, minItems, maxItems,
// uniqueItems, minProperties, maxProperties, minLength, maxLength, minimum,
// maximum, pattern, $ref, oneOf, anyOf, allOf. Ignored
// without checking: $schema, $id, title, description,
// $defs, examples, default, deprecated.
//
// Anything else is a hard error, not a silent pass. That is the safe
// direction: a schema keyword the validator does not understand
// would otherwise let an invalid document through quietly. It also means the
// question "is `minimum` supported?" has an observable answer rather than a
// convention — it is, and the APMap 1.0 schema uses it six times, for
// `revision` among others. (The invalid vector negative-revision.apmap is
// caught by exactly that keyword, so dropping it would silently accept a
// document the specification rejects under SCH-3.)
//
// anyOf, allOf and maximum are implemented but unexercised by the APMap 1.0
// schema. They are kept because they are small, general, and a later contract
// may use them.
//
// Not implemented, because no APMap contract uses them: format, if/then/else,
// prefixItems, contains, dependentSchemas, not, multipleOf,
// exclusiveMinimum/Maximum. (minProperties arrived with APMap 1.4's
// `authorship`, and maxProperties with it as its mirror.) A contract
// that starts using one fails loudly (see above) rather than validating
// silently.

var ignoredKeywords = map[string]bool{
	"$schema":     true,
	"$id":         true,
	"title":       true,
	"description": true,
	"$defs":       true,
	"examples":    true,
	"default":     true,
	"deprecated":  true,
}

// ValidateAgainstSchema returns one message per violation; an empty list means
// the document is valid against this schema.
//
// The error return is separate from the problem list on purpose: it reports a
// defect in the schema (an unresolvable $ref, an unsupported keyword or type
// name, an uncompilable pattern), which is never something the document did
// and must never be reported as if it were.
func ValidateAgainstSchema(document Value, schema *Object) ([]string, error) {
	return ValidateAgainstSchemaContext(context.Background(), document, schema)
}

// checkEvery is how many schema nodes pass between two looks at the context: often enough to stop a
// large document promptly, rarely enough that the look costs nothing.
const checkEvery = 4096

// ValidateAgainstSchemaContext is ValidateAgainstSchema, stopped part-way with ctx's Err.
func ValidateAgainstSchemaContext(ctx context.Context, document Value, schema *Object) ([]string, error) {
	if schema == nil {
		return nil, notFound("no APMap schema was supplied")
	}
	state := &checker{root: schema, ctx: ctx}
	if _, err := state.check(document, schema, "$"); err != nil {
		return nil, err
	}
	return state.problems, nil
}

type checker struct {
	root     *Object
	problems []string
	ctx      context.Context
	visited  int
}

func (c *checker) report(format string, args ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, args...))
}

// check applies one schema node to one value. The bool is the early return
// after a failed `type`: every other keyword in the node assumes the
// type held, so once it has not, the node is done.
func (c *checker) check(value Value, schema *Object, where string) (bool, error) {
	if c.visited++; c.visited%checkEvery == 0 {
		if err := c.ctx.Err(); err != nil {
			return false, err
		}
	}
	if ref, ok := schema.Get("$ref"); ok {
		name, ok := ref.(string)
		if !ok {
			return true, notFound("%s: $ref is not a string", where)
		}
		target, err := c.resolve(name)
		if err != nil {
			return true, err
		}
		// A sibling keyword next to $ref is legal in 2020-12 and the APMap
		// schema uses it for "description"; nothing else needs applying.
		return c.check(value, target, where)
	}

	for _, keyword := range schema.Keys() {
		if ignoredKeywords[keyword] {
			continue
		}
		expected, _ := schema.Get(keyword)

		switch keyword {
		case "type":
			names, err := typeNames(expected, where)
			if err != nil {
				return true, err
			}
			matched := false
			for _, name := range names {
				ok, err := typeMatches(value, name)
				if err != nil {
					return true, err
				}
				if ok {
					matched = true
					break
				}
			}
			if !matched {
				c.report("%s: expected type %s, got %s", where, strings.Join(names, "/"), typeName(value))
				return false, nil
			}

		case "const":
			if !jsonEqual(value, expected) {
				c.report("%s: expected the constant %s, got %s", where, render(expected), render(value))
			}

		case "enum":
			options, ok := expected.([]Value)
			if !ok {
				return true, notFound("%s: enum is not an array", where)
			}
			found := false
			for _, option := range options {
				if jsonEqual(value, option) {
					found = true
					break
				}
			}
			if !found {
				c.report("%s: %s is not one of %s", where, render(value), render(expected))
			}

		case "required":
			object, ok := value.(*Object)
			if !ok {
				continue
			}
			names, ok := expected.([]Value)
			if !ok {
				return true, notFound("%s: required is not an array", where)
			}
			for _, entry := range names {
				name, ok := entry.(string)
				if !ok {
					return true, notFound("%s: required lists a non-string member name", where)
				}
				if !object.Has(name) {
					c.report("%s: required member %q is missing", where, name)
				}
			}

		case "properties":
			object, ok := value.(*Object)
			if !ok {
				continue
			}
			declared, ok := expected.(*Object)
			if !ok {
				return true, notFound("%s: properties is not an object", where)
			}
			for _, name := range declared.Keys() {
				member, present := object.Get(name)
				if !present {
					continue
				}
				sub, err := subSchema(declared, name, where)
				if err != nil {
					return true, err
				}
				if _, err := c.check(member, sub, where+"."+name); err != nil {
					return true, err
				}
			}

		case "additionalProperties":
			object, ok := value.(*Object)
			if !ok {
				continue
			}
			extra := c.undeclared(object, schema)
			switch typed := expected.(type) {
			case bool:
				if !typed {
					for _, name := range extra {
						c.report("%s: member %q is not allowed here", where, name)
					}
				}
			case *Object:
				for _, name := range extra {
					member, _ := object.Get(name)
					if _, err := c.check(member, typed, where+"."+name); err != nil {
						return true, err
					}
				}
			default:
				return true, notFound("%s: additionalProperties is neither a boolean nor a schema", where)
			}

		case "propertyNames":
			object, ok := value.(*Object)
			if !ok {
				continue
			}
			sub, ok := expected.(*Object)
			if !ok {
				return true, notFound("%s: propertyNames is not a schema", where)
			}
			for _, name := range object.Keys() {
				if _, err := c.check(name, sub, fmt.Sprintf("%s.<key %q>", where, name)); err != nil {
					return true, err
				}
			}

		case "items":
			items, ok := value.([]Value)
			if !ok {
				continue
			}
			sub, ok := expected.(*Object)
			if !ok {
				return true, notFound("%s: items is not a schema", where)
			}
			for index, item := range items {
				if _, err := c.check(item, sub, fmt.Sprintf("%s[%d]", where, index)); err != nil {
					return true, err
				}
			}

		case "minItems":
			items, ok := value.([]Value)
			if !ok {
				continue
			}
			bound, err := integerBound(expected, keyword, where)
			if err != nil {
				return true, err
			}
			if len(items) < bound {
				c.report("%s: needs at least %d item(s), has %d", where, bound, len(items))
			}

		case "maxItems":
			items, ok := value.([]Value)
			if !ok {
				continue
			}
			bound, err := integerBound(expected, keyword, where)
			if err != nil {
				return true, err
			}
			if len(items) > bound {
				c.report("%s: allows at most %d item(s), has %d", where, bound, len(items))
			}

		case "minProperties", "maxProperties":
			// Added for APMap 1.4, whose `authorship` is `minProperties: 1` so that "nothing
			// recorded" has one spelling (omit the member) rather than two. maxProperties is its
			// mirror, implemented because it is the same three lines and a later contract meeting
			// it should not be a hard error for want of them. Both count members, not keys the
			// schema declares: a closed object's undeclared members are additionalProperties'
			// finding, reported separately.
			object, ok := value.(*Object)
			if !ok {
				continue
			}
			bound, err := integerBound(expected, keyword, where)
			if err != nil {
				return true, err
			}
			if keyword == "minProperties" && object.Len() < bound {
				c.report("%s: needs at least %d member(s), has %d", where, bound, object.Len())
			}
			if keyword == "maxProperties" && object.Len() > bound {
				c.report("%s: allows at most %d member(s), has %d", where, bound, object.Len())
			}

		case "minLength":
			text, ok := value.(string)
			if !ok {
				continue
			}
			bound, err := integerBound(expected, keyword, where)
			if err != nil {
				return true, err
			}
			// Runes, not bytes: Python's len() on a str counts code points,
			// and minLength in JSON Schema is defined in characters.
			if utf8.RuneCountInString(text) < bound {
				c.report("%s: needs at least %d character(s)", where, bound)
			}

		case "maxLength":
			text, ok := value.(string)
			if !ok {
				continue
			}
			bound, err := integerBound(expected, keyword, where)
			if err != nil {
				return true, err
			}
			// Runes, not bytes, for the same reason minLength counts them: JSON Schema defines
			// length in characters, and a group named in Cyrillic must get the same cap as one
			// named in ASCII rather than a quarter of it.
			if utf8.RuneCountInString(text) > bound {
				c.report("%s: allows at most %d character(s), has %d",
					where, bound, utf8.RuneCountInString(text))
			}

		case "uniqueItems":
			// Only `true` constrains anything; `false` is the default and is a no-op rather than an
			// instruction to permit duplicates twice over.
			required, ok := expected.(bool)
			if !ok {
				return true, notFound("%s: uniqueItems is not a boolean", where)
			}
			if !required {
				continue
			}
			items, ok := value.([]Value)
			if !ok {
				continue
			}
			// Pairwise through jsonEqual, which is the same equality `const` and `enum` already
			// use, so "the same member twice" means one thing across this validator. Quadratic, and
			// deliberately: the arrays this applies to are a group's members, where n is a handful,
			// and a canonical-encoding index would be a second definition of equality to keep in
			// step with the first.
			for i := 0; i < len(items); i++ {
				for j := i + 1; j < len(items); j++ {
					if jsonEqual(items[i], items[j]) {
						c.report("%s: items %d and %d are duplicates, and this collection requires "+
							"every item to be unique", where, i, j)
					}
				}
			}

		case "minimum", "maximum":
			number, ok := numberValue(value)
			if !ok {
				continue
			}
			bound, ok := numberValue(expected)
			if !ok {
				return true, notFound("%s: %s is not a number", where, keyword)
			}
			if keyword == "minimum" && number < bound {
				c.report("%s: %s is less than the minimum %s", where, render(value), render(expected))
			}
			if keyword == "maximum" && number > bound {
				c.report("%s: %s is more than the maximum %s", where, render(value), render(expected))
			}

		case "pattern":
			text, ok := value.(string)
			if !ok {
				continue
			}
			source, ok := expected.(string)
			if !ok {
				return true, notFound("%s: pattern is not a string", where)
			}
			expression, err := compilePattern(source)
			if err != nil {
				return true, notFound("%s: pattern %s does not compile: %v", where, render(expected), err)
			}
			// Unanchored, like Python's re.search — the APMap schema anchors
			// its own patterns with ^ and $ where it means to.
			if !expression.MatchString(text) {
				c.report("%s: %s does not match %s", where, render(value), render(expected))
			}

		case "oneOf":
			branches, err := branchSchemas(expected, keyword, where)
			if err != nil {
				return true, err
			}
			matched := 0
			for _, branch := range branches {
				found, err := c.collect(value, branch, where)
				if err != nil {
					return true, err
				}
				if len(found) == 0 {
					matched++
				}
			}
			if matched != 1 {
				detail := "matches no branch"
				if matched > 0 {
					detail = fmt.Sprintf("matches %d branches", matched)
				}
				c.report("%s: %s of oneOf", where, detail)
			}

		case "anyOf":
			branches, err := branchSchemas(expected, keyword, where)
			if err != nil {
				return true, err
			}
			matched := false
			for _, branch := range branches {
				found, err := c.collect(value, branch, where)
				if err != nil {
					return true, err
				}
				if len(found) == 0 {
					matched = true
					break
				}
			}
			if !matched {
				c.report("%s: matches no branch of anyOf", where)
			}

		case "allOf":
			branches, err := branchSchemas(expected, keyword, where)
			if err != nil {
				return true, err
			}
			for _, branch := range branches {
				if _, err := c.check(value, branch, where); err != nil {
					return true, err
				}
			}

		default:
			return true, notFound(
				"the APMap schema uses an unsupported keyword %q at %s; "+
					"the built-in validator must be extended before this schema can be used",
				keyword, where)
		}
	}
	return true, nil
}

// collect runs a schema branch in isolation and returns its problems without
// adding them to the document's list. oneOf and anyOf need to
// know *whether* a branch matched, not to blame the document for the branches
// it did not take.
func (c *checker) collect(value Value, schema *Object, where string) ([]string, error) {
	branch := &checker{root: c.root, ctx: c.ctx}
	if _, err := branch.check(value, schema, where); err != nil {
		return nil, err
	}
	return branch.problems, nil
}

// undeclared lists the object's members that `properties` does not name, in
// document order.
func (c *checker) undeclared(object *Object, schema *Object) []string {
	declared := map[string]bool{}
	if properties, ok := schema.Get("properties"); ok {
		if named, ok := properties.(*Object); ok {
			for _, name := range named.Keys() {
				declared[name] = true
			}
		}
	}
	var extra []string
	for _, name := range object.Keys() {
		if !declared[name] {
			extra = append(extra, name)
		}
	}
	return extra
}

// resolve follows a local JSON pointer. Only "#/..." refs are supported: the
// APMap schema is one self-contained file, and
// following a remote ref would mean fetching a second contract at validation
// time.
func (c *checker) resolve(ref string) (*Object, error) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, notFound("unsupported $ref (only local refs): %s", ref)
	}
	var node Value = c.root
	for _, token := range strings.Split(ref[2:], "/") {
		token = strings.ReplaceAll(token, "~1", "/")
		token = strings.ReplaceAll(token, "~0", "~")
		object, ok := node.(*Object)
		if !ok {
			return nil, notFound("$ref does not resolve: %s", ref)
		}
		next, ok := object.Get(token)
		if !ok {
			return nil, notFound("$ref does not resolve: %s", ref)
		}
		node = next
	}
	target, ok := node.(*Object)
	if !ok {
		return nil, notFound("$ref does not point at a schema: %s", ref)
	}
	return target, nil
}

func subSchema(properties *Object, name, where string) (*Object, error) {
	value, _ := properties.Get(name)
	sub, ok := value.(*Object)
	if !ok {
		return nil, notFound("%s: properties.%s is not a schema", where, name)
	}
	return sub, nil
}

func branchSchemas(expected Value, keyword, where string) ([]*Object, error) {
	items, ok := expected.([]Value)
	if !ok {
		return nil, notFound("%s: %s is not an array", where, keyword)
	}
	branches := make([]*Object, 0, len(items))
	for _, item := range items {
		branch, ok := item.(*Object)
		if !ok {
			return nil, notFound("%s: %s contains a non-schema branch", where, keyword)
		}
		branches = append(branches, branch)
	}
	return branches, nil
}

func typeNames(expected Value, where string) ([]string, error) {
	switch typed := expected.(type) {
	case string:
		return []string{typed}, nil
	case []Value:
		names := make([]string, 0, len(typed))
		for _, item := range typed {
			name, ok := item.(string)
			if !ok {
				return nil, notFound("%s: type lists a non-string name", where)
			}
			names = append(names, name)
		}
		return names, nil
	default:
		return nil, notFound("%s: type is neither a string nor an array of strings", where)
	}
}

// typeMatches answers a `type` keyword, including its two subtleties.
//
// A boolean is never an integer or a number. In Python that is a real hazard —
// bool subclasses int, so the check is explicit; in Go the types are distinct
// and the same outcome falls out of the type switch, but the behaviour is
// identical either way.
//
// An integral float *is* an integer, per the JSON Schema specification: 3.0
// satisfies "integer". Python's json produces an int exactly when the literal
// has neither a fraction nor an exponent, so the equivalent question here is
// asked of the literal first and of its value second.
func typeMatches(value Value, name string) (bool, error) {
	switch name {
	case "object":
		_, ok := value.(*Object)
		return ok, nil
	case "array":
		_, ok := value.([]Value)
		return ok, nil
	case "string":
		_, ok := value.(string)
		return ok, nil
	case "boolean":
		_, ok := value.(bool)
		return ok, nil
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false, nil
		}
		if isIntegerLiteral(number) {
			return true, nil
		}
		parsed, err := strconv.ParseFloat(string(number), 64)
		if err != nil {
			// Out of float64 range, which is how 1e400 arrives. Python gets
			// inf here and inf.is_integer() is false, so this is not an
			// integer either way.
			return false, nil
		}
		return !math.IsInf(parsed, 0) && !math.IsNaN(parsed) && parsed == math.Trunc(parsed), nil
	case "number":
		_, ok := value.(json.Number)
		return ok, nil
	case "null":
		return value == nil, nil
	default:
		return false, notFound("unsupported schema type: %s", name)
	}
}

// typeName is what the document actually supplied, for the type
// mismatch message.
func typeName(value Value) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		if isIntegerLiteral(typed) {
			return "integer"
		}
		return "number"
	case []Value:
		return "array"
	case *Object:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}

// isIntegerLiteral reports whether Python's json scanner would have produced an
// int rather than a float for this literal. Its number regex splits on the
// presence of a fraction or exponent group and nothing else, so "3" is an int
// while "3.0" and "3e0" are both floats.
func isIntegerLiteral(number json.Number) bool {
	return !strings.ContainsAny(string(number), ".eE")
}

func numberValue(value Value) (float64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(string(number), 64)
	if err != nil {
		// Range errors still yield ±Inf, which is what Python's float() gives
		// for the same literal, so the comparison stays faithful.
		if strings.Contains(string(number), "-") {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	}
	return parsed, true
}

func integerBound(expected Value, keyword, where string) (int, error) {
	number, ok := expected.(json.Number)
	if !ok {
		return 0, notFound("%s: %s is not a number", where, keyword)
	}
	bound, err := strconv.Atoi(string(number))
	if err != nil {
		return 0, notFound("%s: %s is not an integer", where, keyword)
	}
	return bound, nil
}

// jsonEqual is value equality across the decoded shapes, for const and enum.
//
// One deliberate divergence from Python, stated rather than hidden: Python's
// `True == 1` is true, so an enum of [1] would accept `true` there. That is a
// quirk of bool subclassing int, not a JSON Schema rule, and the APMap 1.0
// schema has no enum or const mixing booleans with numbers, so nothing
// observable depends on it. Reproducing it would mean writing a bug on
// purpose.
func jsonEqual(a, b Value) bool {
	switch left := a.(type) {
	case nil:
		return b == nil
	case bool:
		right, ok := b.(bool)
		return ok && left == right
	case string:
		right, ok := b.(string)
		return ok && left == right
	case json.Number:
		right, ok := b.(json.Number)
		if !ok {
			return false
		}
		if left == right {
			return true
		}
		// Different literals can still be the same number (1 and 1.0), which
		// is how Python compares them.
		leftValue, leftOK := numberValue(left)
		rightValue, rightOK := numberValue(right)
		return leftOK && rightOK && leftValue == rightValue
	case []Value:
		right, ok := b.([]Value)
		if !ok || len(left) != len(right) {
			return false
		}
		for index := range left {
			if !jsonEqual(left[index], right[index]) {
				return false
			}
		}
		return true
	case *Object:
		right, ok := b.(*Object)
		if !ok || left.Len() != right.Len() {
			return false
		}
		for _, name := range left.Keys() {
			other, present := right.Get(name)
			if !present {
				return false
			}
			member, _ := left.Get(name)
			if !jsonEqual(member, other) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// render formats a value for a problem message in JSON's own spelling, which is
// unambiguous for every shape a document can hold.
func render(value Value) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(typed)
	case string:
		return strconv.Quote(typed)
	case json.Number:
		return string(typed)
	case []Value:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, render(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Object:
		parts := make([]string, 0, typed.Len())
		for _, name := range typed.Keys() {
			member, _ := typed.Get(name)
			parts = append(parts, strconv.Quote(name)+": "+render(member))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprintf("%v", value)
	}
}

// patternCache keeps compiled regexps for the lifetime of the process. The
// APMap schema has a fixed, small set of patterns and every document
// validation recompiles all of them otherwise.
//
// Go's regexp is RE2, not PCRE. Every pattern the APMap 1.0 schema uses is in
// the common subset, so this is not a divergence today, but a future schema
// using a backreference or a lookaround would fail to compile here and be
// reported as a schema defect rather than silently mis-validating — which is
// the right failure direction.
//
// APMap 1.4's `authorship` patterns are the first to use `\xHH` escapes, as in
// `^[^\x00-\x1F\x7F-\x9F]*$`. RE2 reads `\xHH` as the CODE POINT U+00HH, not
// a byte, exactly as an ECMAScript RegExp does, and Go's regexp matches runes of
// the UTF-8 text — so `\x9F` is U+009F (a C1 control) and never the continuation
// byte 0x9F inside, say, "Zoë". TestAuthorshipPatternsMatchLikeECMAScript pins it.
//
// One subtlety worth naming: Python's `$` also matches immediately before a
// trailing newline, while Go's `$` (without the `m` flag) matches only at end of
// text. A document whose id ended in "\n" is therefore rejected here, which is
// the specification's intent; no vector exercises the case.
var patternCache = struct {
	sync.RWMutex
	entries map[string]*regexp.Regexp
}{entries: map[string]*regexp.Regexp{}}

func compilePattern(source string) (*regexp.Regexp, error) {
	patternCache.RLock()
	cached, ok := patternCache.entries[source]
	patternCache.RUnlock()
	if ok {
		return cached, nil
	}
	expression, err := regexp.Compile(source)
	if err != nil {
		return nil, err
	}
	patternCache.Lock()
	patternCache.entries[source] = expression
	patternCache.Unlock()
	return expression, nil
}
