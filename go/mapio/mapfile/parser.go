package mapfile

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ParseError is a `.map` syntax error. Its message is
// "<source>:<line>:<column>: <message>" and the tests assert on that position
// prefix, so the position is kept as structured fields rather than being
// flattened into a string: a caller that wants the line number should not have
// to parse it back out of an error message.
type ParseError struct {
	Source  string
	Line    int
	Column  int
	Message string
	// Err is the inner failure when one error wraps another, as the
	// malformed-Valve-220-axis handler does.
	Err error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.Source, e.Line, e.Column, e.Message)
}

func (e *ParseError) Unwrap() error { return e.Err }

func newParseError(source string, line, column int, format string, args ...any) *ParseError {
	return &ParseError{Source: source, Line: line, Column: column, Message: fmt.Sprintf(format, args...)}
}

// DefaultSourceName is the placeholder used when parsing a string with no
// filename attached.
const DefaultSourceName = "<string>"

// Parse parses `.map` source text. Positions in errors are reported against
// DefaultSourceName; use ParseNamed to attach a filename.
func Parse(source string) (*MapFile, error) {
	return ParseNamed(source, DefaultSourceName)
}

// ParseNamed is Parse with an explicit source name for error messages.
func ParseNamed(text, sourceName string) (*MapFile, error) {
	tokens, err := tokenize(text, sourceName)
	if err != nil {
		return nil, err
	}
	return (&parser{tokens: tokens, source: sourceName}).parse()
}

type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenLBrace
	tokenRBrace
	tokenLParen
	tokenRParen
	tokenLBracket
	tokenRBracket
	tokenString
	tokenAtom
)

func (k tokenKind) String() string {
	switch k {
	case tokenLBrace:
		return "LBRACE"
	case tokenRBrace:
		return "RBRACE"
	case tokenLParen:
		return "LPAREN"
	case tokenRParen:
		return "RPAREN"
	case tokenLBracket:
		return "LBRACKET"
	case tokenRBracket:
		return "RBRACKET"
	case tokenString:
		return "STRING"
	case tokenAtom:
		return "ATOM"
	default:
		return "EOF"
	}
}

type token struct {
	kind   tokenKind
	value  string
	line   int
	column int
}

// numberPattern is the numeric token grammar. Go's \d is ASCII-only, so a
// non-ASCII digit is rejected. No `.map` file in the corpus contains one.
var numberPattern = regexp.MustCompile(`^[-+]?(?:\d+(?:\.\d*)?|\.\d+)$`)

var delimiters = map[rune]tokenKind{
	'{': tokenLBrace,
	'}': tokenRBrace,
	'(': tokenLParen,
	')': tokenRParen,
	'[': tokenLBracket,
	']': tokenRBracket,
}

// tokenize is line-oriented: a token never spans a line, and a `//` comment runs
// to end of line. Columns are 1-based counts of characters (runes), not bytes.
func tokenize(text, source string) ([]token, error) {
	var tokens []token
	lines := splitLines(text)

	for lineNumber, line := range lines {
		runes := []rune(line)
		index := 0
		for index < len(runes) {
			char := runes[index]
			column := index + 1

			if unicode.IsSpace(char) {
				index++
				continue
			}

			if char == '/' && index+1 < len(runes) && runes[index+1] == '/' {
				break
			}

			if char == '"' {
				value, next, err := readQuotedString(runes, index, source, lineNumber+1, column)
				if err != nil {
					// Tokenization fails before any structural parsing, so an unterminated string anywhere in the file
					// wins over any structural error earlier in it.
					return nil, err
				}
				tokens = append(tokens, token{kind: tokenString, value: value, line: lineNumber + 1, column: column})
				index = next
				continue
			}

			if kind, ok := delimiters[char]; ok {
				tokens = append(tokens, token{kind: kind, value: string(char), line: lineNumber + 1, column: column})
				index++
				continue
			}

			start := index
			for index < len(runes) {
				char = runes[index]
				if unicode.IsSpace(char) || char == '"' {
					break
				}
				if _, isDelimiter := delimiters[char]; isDelimiter {
					break
				}
				if char == '/' && index+1 < len(runes) && runes[index+1] == '/' {
					break
				}
				index++
			}
			tokens = append(tokens, token{
				kind:   tokenAtom,
				value:  string(runes[start:index]),
				line:   lineNumber + 1,
				column: start + 1,
			})
		}
	}

	eofLine, eofColumn := 1, 1
	if len(lines) > 0 {
		eofLine = len(lines)
		eofColumn = len([]rune(lines[len(lines)-1])) + 1
	}
	return append(tokens, token{kind: tokenEOF, line: eofLine, column: eofColumn}), nil
}

// splitLines reproduces Python's str.splitlines, which the tokenizer's line
// numbering depends on. Go's strings.Split(text, "\n") would number lines
// differently for a file using bare CR endings or any of the Unicode line
// breaks Python honours, and line numbers are part of the tested contract.
func splitLines(text string) []string {
	var lines []string
	var current strings.Builder
	runes := []rune(text)

	for i := 0; i < len(runes); i++ {
		char := runes[i]
		switch char {
		case '\r':
			lines = append(lines, current.String())
			current.Reset()
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
		case '\n', '\v', '\f', '\x1c', '\x1d', '\x1e', '\u0085', '\u2028', '\u2029':
			lines = append(lines, current.String())
			current.Reset()
		default:
			current.WriteRune(char)
		}
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func readQuotedString(runes []rune, index int, source string, lineNumber, column int) (string, int, error) {
	index++
	var chars strings.Builder
	for index < len(runes) {
		char := runes[index]
		if char == '\\' {
			if index+1 >= len(runes) {
				return "", 0, newParseError(source, lineNumber, column, "unterminated quoted string")
			}
			// Both the backslash and the escaped character are kept verbatim:
			// escapes are preserved, never interpreted, so `"textures\base"`
			// survives a round trip byte for byte.
			chars.WriteRune(char)
			chars.WriteRune(runes[index+1])
			index += 2
			continue
		}
		if char == '"' {
			return chars.String(), index + 1, nil
		}
		chars.WriteRune(char)
		index++
	}
	return "", 0, newParseError(source, lineNumber, column, "unterminated quoted string")
}

type parser struct {
	tokens                  []token
	source                  string
	index                   int
	parseDiagnostics        []ParseDiagnostic
	emptyBrushBlocksSkipped int
}

func (p *parser) parse() (*MapFile, error) {
	var entities []Entity
	for !p.check(tokenEOF) {
		if p.check(tokenRBrace) {
			return nil, p.errorHere("entity closed before it was opened")
		}
		entity, err := p.parseEntity()
		if err != nil {
			return nil, err
		}
		entities = append(entities, entity)
	}
	return newParsedMapFile(entities, p.parseDiagnostics, p.emptyBrushBlocksSkipped), nil
}

func (p *parser) parseEntity() (Entity, error) {
	if _, err := p.expect(tokenLBrace, "expected entity start"); err != nil {
		return Entity{}, err
	}

	var items []EntityItem
	for !p.check(tokenRBrace) {
		if p.check(tokenEOF) {
			return Entity{}, p.errorHere("unterminated entity")
		}
		if p.check(tokenLBrace) {
			brush, present, err := p.parseBrush()
			if err != nil {
				return Entity{}, err
			}
			if present {
				items = append(items, brush)
			}
			continue
		}
		if p.check(tokenString) {
			keyValue, err := p.parseKeyValue()
			if err != nil {
				return Entity{}, err
			}
			items = append(items, keyValue)
			continue
		}
		return Entity{}, p.errorHere("expected entity key/value or brush")
	}

	p.advance()
	return NewEntity(items), nil
}

func (p *parser) parseKeyValue() (KeyValue, error) {
	key, err := p.expect(tokenString, "expected quoted entity key")
	if err != nil {
		return KeyValue{}, err
	}
	if !p.check(tokenString) {
		return KeyValue{}, newParseError(
			p.source, key.line, key.column,
			// %s of ReprString, not %q: the key is quoted in Python repr form,
			// which single-quotes. See mapfile.ReprString.
			"expected quoted value for entity key %s", ReprString(key.value),
		)
	}
	value := p.advance()
	return NewKeyValue(key.value, value.value), nil
}

func (p *parser) parseBrush() (Brush, bool, error) {
	opening, err := p.expect(tokenLBrace, "expected brush start")
	if err != nil {
		return Brush{}, false, err
	}

	var faces []Face
	for !p.check(tokenRBrace) {
		if p.check(tokenEOF) {
			return Brush{}, false, p.errorHere("unterminated brush")
		}
		if !p.check(tokenLParen) {
			return Brush{}, false, p.errorHere("expected brush face or brush end")
		}
		face, err := p.parseFace()
		if err != nil {
			return Brush{}, false, err
		}
		faces = append(faces, face)
	}

	p.advance()
	if len(faces) == 0 {
		p.emptyBrushBlocksSkipped++
		if len(p.parseDiagnostics) < maxParseDiagnostics {
			p.parseDiagnostics = append(p.parseDiagnostics, ParseDiagnostic{
				Code:    DiagnosticEmptyBrushBlockSkipped,
				Source:  p.source,
				Line:    opening.line,
				Column:  opening.column,
				Message: "syntactically complete empty brush block omitted",
			})
		}
		return Brush{}, false, nil
	}
	return NewBrush(faces), true, nil
}

func (p *parser) parseFace() (Face, error) {
	p1, err := p.parseVec3()
	if err != nil {
		return Face{}, err
	}
	p2, err := p.parseVec3()
	if err != nil {
		return Face{}, err
	}
	p3, err := p.parseVec3()
	if err != nil {
		return Face{}, err
	}
	texture, err := p.expect(tokenAtom, "expected texture name after face points")
	if err != nil {
		return Face{}, err
	}

	if p.check(tokenLBracket) {
		uAxis, err := p.parseTextureAxis("u")
		if err != nil {
			return Face{}, err
		}
		vAxis, err := p.parseTextureAxis("v")
		if err != nil {
			return Face{}, err
		}
		rotation, err := p.parseNumber("texture rotation")
		if err != nil {
			return Face{}, err
		}
		xScale, err := p.parseNumber("x texture scale")
		if err != nil {
			return Face{}, err
		}
		yScale, err := p.parseNumber("y texture scale")
		if err != nil {
			return Face{}, err
		}
		tail, err := p.parseFaceTail()
		if err != nil {
			return Face{}, err
		}
		return NewValve220Face(p1, p2, p3, texture.value, uAxis, vAxis, rotation, xScale, yScale, tail), nil
	}

	xOffset, err := p.parseNumber("x texture offset")
	if err != nil {
		return Face{}, err
	}
	yOffset, err := p.parseNumber("y texture offset")
	if err != nil {
		return Face{}, err
	}
	rotation, err := p.parseNumber("texture rotation")
	if err != nil {
		return Face{}, err
	}
	xScale, err := p.parseNumber("x texture scale")
	if err != nil {
		return Face{}, err
	}
	yScale, err := p.parseNumber("y texture scale")
	if err != nil {
		return Face{}, err
	}
	tail, err := p.parseFaceTail()
	if err != nil {
		return Face{}, err
	}
	return NewClassicFace(p1, p2, p3, texture.value, xOffset, yOffset, rotation, xScale, yScale, tail), nil
}

// parseTextureAxis reports a malformed axis against the position of the opening
// bracket, wrapping the inner failure, which is what makes
// TestMalformedValve220FaceFailsClearly's "broken.map:4:" hold even though the
// inner error is further along the line.
func (p *parser) parseTextureAxis(name string) (TextureAxis, error) {
	start, err := p.expect(tokenLBracket, fmt.Sprintf("expected Valve 220 texture axis %s start", name))
	if err != nil {
		return TextureAxis{}, err
	}

	malformed := func(inner error) error {
		return &ParseError{
			Source:  p.source,
			Line:    start.line,
			Column:  start.column,
			Message: fmt.Sprintf("malformed Valve 220 texture axis %s: %s", name, inner),
			Err:     inner,
		}
	}

	values := make([]Number, 0, 4)
	for _, component := range []string{"x", "y", "z", "offset"} {
		value, err := p.parseNumber(fmt.Sprintf("Valve 220 texture axis %s %s", name, component))
		if err != nil {
			return TextureAxis{}, malformed(err)
		}
		values = append(values, value)
	}
	if _, err := p.expect(tokenRBracket, fmt.Sprintf("expected Valve 220 texture axis %s end", name)); err != nil {
		return TextureAxis{}, malformed(err)
	}
	return NewTextureAxis(values[0], values[1], values[2], values[3]), nil
}

// parseFaceTail consumes the trailing Quake 2 content/surface/value fields, if
// any: everything up to the next face, the end of the brush, or EOF.
func (p *parser) parseFaceTail() ([]Number, error) {
	var values []Number
	for !p.check(tokenLParen) && !p.check(tokenRBrace) && !p.check(tokenEOF) {
		if p.check(tokenLBracket) {
			return nil, p.errorHere("unexpected Valve 220 texture axis")
		}
		value, err := p.parseNumber("Quake 2 face value")
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (p *parser) parseVec3() (Vec3, error) {
	if _, err := p.expect(tokenLParen, "expected face point start"); err != nil {
		return Vec3{}, err
	}
	x, err := p.parseNumber("x coordinate")
	if err != nil {
		return Vec3{}, err
	}
	y, err := p.parseNumber("y coordinate")
	if err != nil {
		return Vec3{}, err
	}
	z, err := p.parseNumber("z coordinate")
	if err != nil {
		return Vec3{}, err
	}
	if _, err := p.expect(tokenRParen, "expected face point end"); err != nil {
		return Vec3{}, err
	}
	return NewVec3(x, y, z), nil
}

func (p *parser) parseNumber(label string) (Number, error) {
	tok, err := p.expect(tokenAtom, "expected "+label)
	if err != nil {
		return 0, err
	}
	if !numberPattern.MatchString(tok.value) {
		return 0, newParseError(p.source, tok.line, tok.column, "expected numeric %s", label)
	}
	value, convErr := strconv.ParseFloat(tok.value, 64)
	if convErr != nil {
		// Only reachable for a token that matches the numeric pattern but
		// overflows float64, which Python's int/float would have accepted.
		return 0, newParseError(p.source, tok.line, tok.column, "numeric %s out of range: %s", label, tok.value)
	}
	return value, nil
}

func (p *parser) check(kind tokenKind) bool { return p.tokens[p.index].kind == kind }

func (p *parser) advance() token {
	tok := p.tokens[p.index]
	p.index++
	return tok
}

func (p *parser) expect(kind tokenKind, message string) (token, error) {
	if !p.check(kind) {
		return token{}, p.errorHere(message)
	}
	return p.advance(), nil
}

func (p *parser) errorHere(message string) *ParseError {
	tok := p.tokens[p.index]
	return newParseError(p.source, tok.line, tok.column, "%s", message)
}
