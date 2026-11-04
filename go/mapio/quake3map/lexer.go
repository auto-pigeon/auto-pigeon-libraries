package quake3map

import "strings"

// token is one Q3Map2 token with its position. Line and column count from 1; columns count UTF-16
// code units, as the reference reader's do, so a diagnostic points at the same place in both.
type token struct {
	text   string
	quoted bool
	line   int
	column int
	offset int
}

// lexer is Q3Map2's tokenizer (`scriplib.c` `GetToken`), with positions — a transliteration of the
// reference reader's, call for call, because the ORDER in which it records faults is part of what
// both implementations report.
//
//   - whitespace is every byte <= 32, and a token runs to the next one (or to a `;`);
//   - `//`, `;` and `#` at the start of a token comment out the rest of the line, `/* … */` a span;
//   - a `"` starts a quoted token that ends at the next `"`; there is no escape character, and a
//     line break before the closing quote is refused.
type lexer struct {
	text   string
	offset int
	line   int
	column int
	errors *[]ParseError

	peeked     *token
	peekedLine int
}

func newLexer(text string, errors *[]ParseError) *lexer {
	return &lexer{text: text, line: 1, column: 1, errors: errors}
}

// advance moves count bytes forward, counting a line per `\n` and a column per UTF-16 code unit.
func (l *lexer) advance(count int) {
	for index := 0; index < count && l.offset < len(l.text); index++ {
		b := l.text[l.offset]
		switch {
		case b == '\n':
			l.line++
			l.column = 1
		case b&0xC0 == 0x80:
			// A continuation byte: its rune was counted at its lead byte.
		case b >= 0xF0:
			l.column += 2 // a supplementary rune is a surrogate pair
		default:
			l.column++
		}
		l.offset++
	}
}

// skip passes whitespace and comments, and reports false at the end of the text.
func (l *lexer) skip() bool {
	text := l.text
	for {
		for l.offset < len(text) && text[l.offset] <= 32 {
			l.advance(1)
		}
		if l.offset >= len(text) {
			return false
		}
		c := text[l.offset]
		if c == ';' || c == '#' || (c == '/' && l.offset+1 < len(text) && text[l.offset+1] == '/') {
			for l.offset < len(text) && text[l.offset] != '\n' {
				l.advance(1)
			}
			continue
		}
		if c == '/' && l.offset+1 < len(text) && text[l.offset+1] == '*' {
			line, column := l.line, l.column
			end := strings.Index(text[l.offset+2:], "*/")
			if end < 0 {
				*l.errors = append(*l.errors, newError(CodeUnterminatedComment, "a /* comment is never closed", line, column, -1, -1))
				l.advance(len(text) - l.offset)
				return false
			}
			l.advance(end + 2 + 2)
			continue
		}
		return true
	}
}

func (l *lexer) read() *token {
	if !l.skip() {
		return nil
	}
	text, line, column, offset := l.text, l.line, l.column, l.offset
	if text[l.offset] == '"' {
		rest := text[l.offset+1:]
		closing := strings.IndexByte(rest, '"')
		newline := strings.IndexByte(rest, '\n')
		if closing < 0 || (newline >= 0 && newline < closing) {
			*l.errors = append(*l.errors, newError(CodeUnterminatedString,
				"a quoted string is not closed on the line it starts on", line, column, -1, -1))
			// Resynchronise at the end of the line rather than swallowing the rest of the file.
			stop := len(text)
			if newline >= 0 {
				stop = l.offset + 1 + newline
			}
			l.advance(stop - l.offset)
			return &token{text: "", quoted: true, line: line, column: column, offset: offset}
		}
		value := rest[:closing]
		l.advance(closing + 2)
		return &token{text: value, quoted: true, line: line, column: column, offset: offset}
	}
	end := l.offset
	for end < len(text) && text[end] > 32 && text[end] != ';' {
		end++
	}
	value := text[l.offset:end]
	l.advance(end - l.offset)
	return &token{text: value, quoted: false, line: line, column: column, offset: offset}
}

// next consumes the next token, or returns nil at the end. sameLine refuses to cross a line break.
func (l *lexer) next(sameLine bool) *token {
	var t *token
	if sameLine {
		t = l.peekOnLine()
	} else {
		t = l.peek()
	}
	if t != nil {
		l.peeked = nil
	}
	return t
}

func (l *lexer) peek() *token {
	if l.peeked == nil {
		l.peekedLine = l.line
		l.peeked = l.read()
	}
	return l.peeked
}

// peekOnLine is the next token only when no line break separates it from the last one consumed —
// how Q3Map2's `TokenAvailable()` decides a face's optional tail is there.
func (l *lexer) peekOnLine() *token {
	t := l.peek()
	if t != nil && t.line == l.peekedLine {
		return t
	}
	return nil
}
