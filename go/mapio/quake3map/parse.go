// Package quake3map reads Quake III `.map` text into an ordered syntax tree: the Go reader behind
// `q3map-to-apmap`.
//
// It is a transliteration of the TypeScript reference reader (`ts/mapio/src/map/quake3.ts`), and the
// two must agree on every accepted map, every refused one, and the code, line and column of every
// fault; the shared conformance fixtures under `fixtures/mapio-conformance/` hold them to it. The
// question both answer is "what will Q3Map2 read?", so the tokenizer is Q3Map2's own rule.
//
// | block | here | why |
// | --- | --- | --- |
// | classic face, with or without the three-number tail | read | Q3Map2's BPRIMIT_OLDBRUSHES |
// | `brushDef { … }` face with a 2x3 texture matrix | read, matrix verbatim | BPRIMIT_NEWBRUSHES |
// | `patchDef2 { … }` | read, every number verbatim | APMap 1.5 §9c |
// | `brushDef3`, `patchDef3`, `patchDef2WS`, `patchDef3WS` | refused | Doom 3 grammars Q3Map2 does not read |
// | a Valve 220 `[ … ]` face | read | NetRadiant-custom's Q3Map2 compiles it |
// | a brush mixing face syntaxes | refused | Q3Map2 fixes one syntax per file |
// | editor key/value pairs inside `brushDef` | refused | APMap has no member for them |
// | any other word where a brush was expected | refused | nothing is guessed |
//
// A refusal is an error on the list, never a panic and never a skip: a caller refuses the whole map
// when the list is not empty. No message quotes the file — a line and a column are enough to find a
// fault in a file the reader has, and a report copied into a bug tracker must not carry a piece of
// somebody's unreleased map.
//
// The package is bytes in, values out: it opens no file and starts nothing. Parse honours its
// context between entities and between brush blocks, and a cancelled parse returns no tree.
package quake3map

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// The refusal codes, one per grammar the reader declines. Stable, and the reference reader's.
const (
	CodeUnterminatedString   = "Q3_UNTERMINATED_STRING"
	CodeUnterminatedComment  = "Q3_UNTERMINATED_COMMENT"
	CodeContentOutsideEntity = "Q3_CONTENT_OUTSIDE_ENTITY"
	CodeInvalidProperty      = "Q3_INVALID_PROPERTY"
	CodeUnexpectedToken      = "Q3_UNEXPECTED_TOKEN"
	CodeUnexpectedEnd        = "Q3_UNEXPECTED_END"
	CodeInvalidNumber        = "Q3_INVALID_NUMBER"
	CodeMixedSyntax          = "Q3_MIXED_BRUSH_SYNTAX"
	CodeDoom3Grammar         = "Q3_DOOM3_GRAMMAR_UNSUPPORTED"
	CodeUnknownBlock         = "Q3_UNKNOWN_BLOCK"
	CodeBrushEpairs          = "Q3_BRUSH_EPAIRS_UNSUPPORTED"
	CodePatchSize            = "Q3_PATCH_SIZE_UNSUPPORTED"
	CodeTooFewFaces          = "Q3_BRUSH_TOO_FEW_FACES"
)

// Syntax is the face syntax a brush is written in.
type Syntax string

const (
	SyntaxClassic         Syntax = "classic"
	SyntaxValve220        Syntax = "valve220"
	SyntaxBrushPrimitives Syntax = "brush_primitives"
)

// Vec3 is a plane point.
type Vec3 = [3]float64

// ClassicTexture is a classic face's five texture numbers.
type ClassicTexture struct {
	Shift    [2]float64
	Rotation float64
	Scale    [2]float64
}

// Valve220Texture is a Valve 220 face's `[x y z shift]` axes, its decorative rotation and its scale.
type Valve220Texture struct {
	U, V     [4]float64
	Rotation float64
	Scale    [2]float64
}

// Face is one brush face. Exactly one of Classic, Valve and Matrix is set.
type Face struct {
	Points [3]Vec3
	// Texture is the shader name as written; Q3Map2 prefixes `textures/` itself.
	Texture string
	Classic *ClassicTexture
	Valve   *Valve220Texture
	// Matrix is a brushDef face's 2x3 texture matrix, verbatim.
	Matrix *[2][3]float64
	// Tail is the content flags, surface flags and value — only the numbers the file wrote.
	Tail   []float64
	Line   int
	Column int
}

// Item is one entity content item: a Property, a *Brush or a *Patch, in file order.
type Item interface{ item() }

// Property is one key/value pair.
type Property struct {
	Key, Value   string
	Line, Column int
}

// Brush is one brush block.
type Brush struct {
	Syntax       Syntax
	Faces        []Face
	Line, Column int
}

// Patch is one patchDef2 block.
type Patch struct {
	Texture       string
	Width, Height int
	// Tail is the three header numbers after width and height, verbatim.
	Tail []float64
	// ControlPoints is Width columns of Height `[x, y, z, s, t]` — patchDef2 text order.
	ControlPoints [][][5]float64
	Line, Column  int
}

func (Property) item() {}
func (*Brush) item()   {}
func (*Patch) item()   {}

// Entity is one entity, its content in the order the file wrote it.
type Entity struct {
	Index        int
	Content      []Item
	Line, Column int
}

// ParseError is one fault, by code and position. EntityIndex and BrushIndex are -1 when the fault
// is not inside one.
type ParseError struct {
	Code        string
	Message     string
	Line        int
	Column      int
	EntityIndex int
	BrushIndex  int
}

func (e ParseError) String() string {
	return fmt.Sprintf("line %d, column %d (%s): %s", e.Line, e.Column, e.Code, e.Message)
}

// Stats counts what was read. Bytes is the input's length in bytes.
type Stats struct {
	Bytes, Entities, Properties, Brushes             int
	ClassicBrushes, Valve220Brushes, BrushPrimitives int
	Faces, Patches                                   int
}

// Map is a parsed Quake III map: every entity, every fault, and the counts.
type Map struct {
	Entities []*Entity
	Errors   []ParseError
	Stats    Stats
}

func newError(code, message string, line, column, entity, brush int) ParseError {
	return ParseError{Code: code, Message: message, Line: line, Column: column, EntityIndex: entity, BrushIndex: brush}
}

var doom3Blocks = map[string]bool{"brushDef3": true, "patchDef3": true, "patchDef2WS": true, "patchDef3WS": true}

// HasQuake3OnlySyntax reports whether text holds a Quake III-only brush keyword standing alone as a
// word — what a Quake 1 or Quake 2 read asks before it fails generically, so it can say the file is a
// Quake III map instead.
func HasQuake3OnlySyntax(text string) bool {
	for _, field := range strings.Fields(text) {
		switch field {
		case "brushDef", "patchDef2", "brushDef3", "patchDef3":
			return true
		}
	}
	return false
}

// isNumber is the reference reader's number rule, `^[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?$`:
// stricter than Q3Map2's `atof`, so a token that is not wholly a number is refused.
func isNumber(text string) bool {
	index := 0
	if index < len(text) && (text[index] == '+' || text[index] == '-') {
		index++
	}
	digits := func() int {
		start := index
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			index++
		}
		return index - start
	}
	if digits() > 0 {
		if index < len(text) && text[index] == '.' {
			index++
			digits()
		}
	} else {
		if index >= len(text) || text[index] != '.' {
			return false
		}
		index++
		if digits() == 0 {
			return false
		}
	}
	if index < len(text) && (text[index] == 'e' || text[index] == 'E') {
		index++
		if index < len(text) && (text[index] == '+' || text[index] == '-') {
			index++
		}
		if digits() == 0 {
			return false
		}
	}
	return index == len(text)
}

// isWord is `^[A-Za-z_]\w*$`.
func isWord(text string) bool {
	if text == "" {
		return false
	}
	for index := 0; index < len(text); index++ {
		c := text[index]
		letter := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if !letter && (index == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// jsNumber spells a number in a message as JavaScript's String(number) does, so the two readers'
// sentences match.
func jsNumber(value float64) string {
	if value == 0 {
		return "0"
	}
	magnitude := value
	if magnitude < 0 {
		magnitude = -magnitude
	}
	if magnitude >= 1e-6 && magnitude < 1e21 {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	text := strconv.FormatFloat(value, 'e', -1, 64)
	mantissa, exponent, _ := strings.Cut(text, "e")
	sign := exponent[0]
	exponent = strings.TrimLeft(exponent[1:], "0")
	if exponent == "" {
		exponent = "0"
	}
	return mantissa + "e" + string(sign) + exponent
}

type position struct{ entity, brush int }

type parser struct {
	ctx        context.Context
	lex        *lexer
	out        *Map
	patchDepth int
}

func (p *parser) fault(code, message string, line, column int, at position) {
	p.out.Errors = append(p.out.Errors, newError(code, message, line, column, at.entity, at.brush))
}

// expect consumes one token and demands it is expected. A mismatched brace is left for recover.
func (p *parser) expect(expected string, at position, what string) bool {
	t := p.lex.peek()
	if t == nil {
		p.fault(CodeUnexpectedEnd, fmt.Sprintf("the file ends where %s expected %q", what, expected), p.lex.line, p.lex.column, at)
		return false
	}
	if t.quoted || t.text != expected {
		p.fault(CodeUnexpectedToken, fmt.Sprintf("%s expected %q", what, expected), t.line, t.column, at)
		if t.quoted || (t.text != "{" && t.text != "}") {
			p.lex.next(false)
		}
		return false
	}
	p.lex.next(false)
	return true
}

func (p *parser) number(at position, what string, sameLine bool) (float64, bool) {
	t := p.lex.next(sameLine)
	if t == nil {
		p.fault(CodeUnexpectedEnd, fmt.Sprintf("the file ends where %s expected a number", what), p.lex.line, p.lex.column, at)
		return 0, false
	}
	if t.quoted || !isNumber(t.text) {
		p.fault(CodeInvalidNumber, fmt.Sprintf("%s is not a finite number", what), t.line, t.column, at)
		return 0, false
	}
	value, err := strconv.ParseFloat(t.text, 64)
	if err != nil { // out of range: JavaScript's Number() is ±Infinity there, which is not finite
		p.fault(CodeInvalidNumber, fmt.Sprintf("%s is not a finite number", what), t.line, t.column, at)
		return 0, false
	}
	return value, true
}

// vector reads `( n n … )` with exactly count numbers.
func (p *parser) vector(count int, at position, what string) ([]float64, bool) {
	if !p.expect("(", at, what) {
		return nil, false
	}
	values := make([]float64, 0, count)
	for index := 0; index < count; index++ {
		value, ok := p.number(at, what, false)
		if !ok {
			return nil, false
		}
		values = append(values, value)
	}
	if !p.expect(")", at, what) {
		return nil, false
	}
	return values, true
}

// recover skips to the brace that closes the block depth levels up, so one fault does not cascade.
func (p *parser) recover(depth int) {
	open := depth
	for t := p.lex.next(false); t != nil; t = p.lex.next(false) {
		if t.quoted {
			continue
		}
		if t.text == "{" {
			open++
		} else if t.text == "}" {
			open--
			if open <= 0 {
				return
			}
		}
	}
}

// tail reads up to three numbers while they stay on the line the face ended on.
func (p *parser) tail(at position, face string) ([]float64, bool) {
	values := []float64{}
	for t := p.lex.peekOnLine(); t != nil; t = p.lex.peekOnLine() {
		if len(values) == 3 {
			p.fault(CodeUnexpectedToken, face+" carries more than three trailing values", t.line, t.column, at)
			return nil, false
		}
		value, ok := p.number(at, face+"'s trailing value", true)
		if !ok {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

// shaderName reads a face's shader from the face's own line, or a patch's from wherever it starts.
func (p *parser) shaderName(at position, what string, crossLine bool) (string, bool) {
	var t *token
	if crossLine {
		t = p.lex.peek()
	} else {
		t = p.lex.peekOnLine()
	}
	if t == nil || (!t.quoted && (t.text == "(" || t.text == ")" || t.text == "{" || t.text == "}" || t.text == "[")) {
		line, column := p.lex.line, p.lex.column
		if t != nil {
			line, column = t.line, t.column
		}
		p.fault(CodeUnexpectedToken, what+" expected a shader name", line, column, at)
		return "", false
	}
	p.lex.next(false)
	return t.text, true
}

func (p *parser) points(at position, what string) ([3]Vec3, bool) {
	var result [3]Vec3
	for index := 0; index < 3; index++ {
		point, ok := p.vector(3, at, what)
		if !ok {
			return result, false
		}
		copy(result[index][:], point)
	}
	return result, true
}

func (p *parser) classicFace(at position, faceIndex int, start *token) (Face, bool) {
	what := fmt.Sprintf("face %d", faceIndex)
	plane, ok := p.points(at, what)
	if !ok {
		return Face{}, false
	}
	texture, ok := p.shaderName(at, what, false)
	if !ok {
		return Face{}, false
	}
	if bracket := p.lex.peekOnLine(); bracket != nil && !bracket.quoted && bracket.text == "[" {
		axisWhat := what + "'s Valve 220 axis"
		axis := func() ([4]float64, bool) {
			var values [4]float64
			if !p.expect("[", at, axisWhat) {
				return values, false
			}
			for index := 0; index < 4; index++ {
				value, ok := p.number(at, axisWhat, true)
				if !ok {
					return values, false
				}
				values[index] = value
			}
			return values, p.expect("]", at, axisWhat)
		}
		u, ok := axis()
		if !ok {
			return Face{}, false
		}
		v, ok := axis()
		if !ok {
			return Face{}, false
		}
		var rest [3]float64
		for index := 0; index < 3; index++ {
			value, ok := p.number(at, what+"'s rotation and scale", true)
			if !ok {
				return Face{}, false
			}
			rest[index] = value
		}
		trailing, ok := p.tail(at, what)
		if !ok {
			return Face{}, false
		}
		return Face{Points: plane, Texture: texture,
			Valve: &Valve220Texture{U: u, V: v, Rotation: rest[0], Scale: [2]float64{rest[1], rest[2]}},
			Tail:  trailing, Line: start.line, Column: start.column}, true
	}
	var values [5]float64
	for index := 0; index < 5; index++ {
		value, ok := p.number(at, what+"'s texture shift, rotation and scale", true)
		if !ok {
			return Face{}, false
		}
		values[index] = value
	}
	trailing, ok := p.tail(at, what)
	if !ok {
		return Face{}, false
	}
	return Face{Points: plane, Texture: texture,
		Classic: &ClassicTexture{Shift: [2]float64{values[0], values[1]}, Rotation: values[2], Scale: [2]float64{values[3], values[4]}},
		Tail:    trailing, Line: start.line, Column: start.column}, true
}

func (p *parser) primitiveFace(at position, faceIndex int, start *token) (Face, bool) {
	what := fmt.Sprintf("brushDef face %d", faceIndex)
	plane, ok := p.points(at, what)
	if !ok {
		return Face{}, false
	}
	matrixWhat := what + "'s texture matrix"
	if !p.expect("(", at, matrixWhat) {
		return Face{}, false
	}
	row0, ok := p.vector(3, at, matrixWhat)
	if !ok {
		return Face{}, false
	}
	row1, ok := p.vector(3, at, matrixWhat)
	if !ok {
		return Face{}, false
	}
	if !p.expect(")", at, matrixWhat) {
		return Face{}, false
	}
	texture, ok := p.shaderName(at, what, false)
	if !ok {
		return Face{}, false
	}
	trailing, ok := p.tail(at, what)
	if !ok {
		return Face{}, false
	}
	matrix := [2][3]float64{{row0[0], row0[1], row0[2]}, {row1[0], row1[1], row1[2]}}
	return Face{Points: plane, Texture: texture, Matrix: &matrix, Tail: trailing, Line: start.line, Column: start.column}, true
}

// brushBlock reads after a brush's own `{`, through its closing `}`.
func (p *parser) brushBlock(entity *Entity, at position, open *token) {
	first := p.lex.peek()
	if first == nil {
		p.fault(CodeUnexpectedEnd, "the file ends inside a brush", open.line, open.column, at)
		return
	}
	if !first.quoted && doom3Blocks[first.text] {
		p.fault(CodeDoom3Grammar, first.text+" is a Doom 3 / Quake 4 grammar that Q3Map2 does not read", first.line, first.column, at)
		p.recover(1)
		return
	}
	if !first.quoted && first.text == "patchDef2" {
		p.lex.next(false)
		patch := p.patchBlock(at, first)
		if patch == nil {
			p.recover(p.patchDepth)
			p.patchDepth = 1
			return
		}
		if !p.expect("}", at, "the patch block") {
			p.recover(1)
			return
		}
		entity.Content = append(entity.Content, patch)
		p.out.Stats.Patches++
		return
	}
	if !first.quoted && first.text == "brushDef" {
		p.lex.next(false)
		if !p.expect("{", at, "brushDef") {
			p.recover(1)
			return
		}
		brush := &Brush{Syntax: SyntaxBrushPrimitives, Line: open.line, Column: open.column}
		for {
			t := p.lex.peek()
			if t == nil {
				p.fault(CodeUnexpectedEnd, "the file ends inside a brushDef", open.line, open.column, at)
				return
			}
			if t.quoted {
				p.fault(CodeBrushEpairs, "a brushDef carries editor key/value pairs, which APMap has no place for", t.line, t.column, at)
				p.recover(2)
				return
			}
			if t.text == "}" {
				p.lex.next(false)
				break
			}
			if t.text != "(" {
				p.fault(CodeUnexpectedToken, `a brushDef face must start with "("`, t.line, t.column, at)
				p.recover(2)
				return
			}
			face, ok := p.primitiveFace(at, len(brush.Faces), t)
			if !ok {
				p.recover(2)
				return
			}
			brush.Faces = append(brush.Faces, face)
		}
		if !p.expect("}", at, "the brush holding a brushDef") {
			p.recover(1)
			return
		}
		p.pushBrush(entity, at, brush)
		return
	}
	if first.quoted || first.text != "(" {
		switch {
		case !first.quoted && isWord(first.text):
			p.fault(CodeUnknownBlock, fmt.Sprintf("%q is not a brush block this editor reads", utf16Prefix(first.text, 32)), first.line, first.column, at)
		case first.text != "}":
			p.fault(CodeUnexpectedToken, `a brush must start with "(", brushDef or patchDef2`, first.line, first.column, at)
		default:
			p.fault(CodeTooFewFaces, "a brush has no faces", first.line, first.column, at)
		}
		p.recover(1)
		return
	}
	brush := &Brush{Syntax: SyntaxClassic, Line: open.line, Column: open.column}
	mixed := false
	for {
		t := p.lex.peek()
		if t == nil {
			p.fault(CodeUnexpectedEnd, "the file ends inside a brush", open.line, open.column, at)
			return
		}
		if !t.quoted && t.text == "}" {
			p.lex.next(false)
			break
		}
		if t.quoted || t.text != "(" {
			message := `a face must start with "("`
			if t.text == "{" && !t.quoted {
				message = "a brace opens inside a brush"
			}
			p.fault(CodeUnexpectedToken, message, t.line, t.column, at)
			p.recover(1)
			return
		}
		face, ok := p.classicFace(at, len(brush.Faces), t)
		if !ok {
			p.recover(1)
			return
		}
		// The first face decides the brush's syntax; a later face in the other one is a brush Q3Map2
		// cannot read at all, so it is an error, not a choice.
		syntax := SyntaxClassic
		if face.Valve != nil {
			syntax = SyntaxValve220
		}
		if len(brush.Faces) == 0 {
			brush.Syntax = syntax
		} else if syntax != brush.Syntax && !mixed {
			mixed = true
			label := func(s Syntax) string {
				if s == SyntaxValve220 {
					return "Valve 220"
				}
				return "classic"
			}
			p.fault(CodeMixedSyntax, fmt.Sprintf("face %d is %s in a %s brush", len(brush.Faces), label(syntax), label(brush.Syntax)),
				face.Line, face.Column, at)
		}
		brush.Faces = append(brush.Faces, face)
	}
	if mixed {
		return
	}
	p.pushBrush(entity, at, brush)
}

func (p *parser) patchBlock(at position, start *token) *Patch {
	p.patchDepth = 1
	if !p.expect("{", at, "patchDef2") {
		return nil
	}
	p.patchDepth = 2
	name, ok := p.shaderName(at, "patchDef2", true)
	if !ok {
		return nil
	}
	header, ok := p.vector(5, at, "the patchDef2 header")
	if !ok {
		return nil
	}
	width, height := header[0], header[1]
	if width != float64(int64(width)) || height != float64(int64(height)) || width < 3 || height < 3 || width > 31 || height > 31 ||
		int64(width)%2 == 0 || int64(height)%2 == 0 {
		p.fault(CodePatchSize, fmt.Sprintf("a %sx%s patch is outside APMap 1.5's grid: each dimension must be odd, 3 to 31",
			jsNumber(width), jsNumber(height)), start.line, start.column, at)
		return nil
	}
	if !p.expect("(", at, "the patchDef2 control grid") {
		return nil
	}
	columns := make([][][5]float64, 0, int(width))
	for c := 0; c < int(width); c++ {
		columnWhat := fmt.Sprintf("patch column %d", c)
		if !p.expect("(", at, columnWhat) {
			return nil
		}
		rows := make([][5]float64, 0, int(height))
		for r := 0; r < int(height); r++ {
			point, ok := p.vector(5, at, fmt.Sprintf("patch point %d,%d", c, r))
			if !ok {
				return nil
			}
			rows = append(rows, [5]float64{point[0], point[1], point[2], point[3], point[4]})
		}
		if !p.expect(")", at, columnWhat) {
			return nil
		}
		columns = append(columns, rows)
	}
	if !p.expect(")", at, "the patchDef2 control grid") {
		return nil
	}
	if !p.expect("}", at, "patchDef2") {
		return nil
	}
	p.patchDepth = 1
	return &Patch{Texture: name, Width: int(width), Height: int(height), Tail: append([]float64{}, header[2:]...),
		ControlPoints: columns, Line: start.line, Column: start.column}
}

func (p *parser) pushBrush(entity *Entity, at position, brush *Brush) {
	if len(brush.Faces) < 4 {
		p.fault(CodeTooFewFaces, fmt.Sprintf("a brush needs at least four faces, this one has %d", len(brush.Faces)), brush.Line, brush.Column, at)
		return
	}
	entity.Content = append(entity.Content, brush)
	stats := &p.out.Stats
	stats.Brushes++
	stats.Faces += len(brush.Faces)
	switch brush.Syntax {
	case SyntaxClassic:
		stats.ClassicBrushes++
	case SyntaxValve220:
		stats.Valve220Brushes++
	default:
		stats.BrushPrimitives++
	}
}

// Parse reads a whole Quake III map. text is what DecodeText returned. Faults are on the returned
// map's Errors; the error is only ever the context's, and then no map is returned.
func Parse(ctx context.Context, text string) (*Map, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	out := &Map{Stats: Stats{Bytes: len(text)}}
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	p := &parser{ctx: ctx, lex: newLexer(normalized, &out.Errors), out: out, patchDepth: 1}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for {
		t := p.lex.next(false)
		if t == nil {
			break
		}
		if t.quoted || t.text != "{" {
			p.fault(CodeContentOutsideEntity, "content appears outside an entity", t.line, t.column, position{-1, -1})
			// Resynchronise on the next top-level brace.
			for peek := p.lex.peek(); peek != nil && !(peek.text == "{" && !peek.quoted); peek = p.lex.peek() {
				p.lex.next(false)
			}
			continue
		}
		entity := &Entity{Index: len(out.Entities), Line: t.line, Column: t.column}
		out.Entities = append(out.Entities, entity)
		out.Stats.Entities++
		brushIndex := 0
		for {
			inner := p.lex.next(false)
			if inner == nil {
				p.fault(CodeUnexpectedEnd, "an entity is not closed", entity.Line, entity.Column, position{entity.Index, -1})
				break
			}
			if !inner.quoted && inner.text == "}" {
				break
			}
			if !inner.quoted && inner.text == "{" {
				// The cancellation point inside an entity: a Quake III worldspawn holds nearly every
				// brush of the map, so between entities alone would be too coarse.
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				p.brushBlock(entity, position{entity.Index, brushIndex}, inner)
				brushIndex++
				continue
			}
			if !inner.quoted {
				p.fault(CodeInvalidProperty, "expected a quoted entity key or a brush brace", inner.line, inner.column, position{entity.Index, -1})
				continue
			}
			value := p.lex.next(true)
			if value == nil || !value.quoted {
				p.fault(CodeInvalidProperty, "an entity key must be followed by a quoted value on the same line", inner.line, inner.column, position{entity.Index, -1})
				if value != nil && !value.quoted && value.text == "}" {
					break
				}
				continue
			}
			entity.Content = append(entity.Content, Property{Key: inner.text, Value: value.text, Line: inner.line, Column: inner.column})
			out.Stats.Properties++
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
