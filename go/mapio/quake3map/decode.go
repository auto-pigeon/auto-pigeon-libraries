package quake3map

import (
	"strings"
	"unicode/utf8"
)

// DecodeText is the text a Quake III `.map` file's bytes hold, read exactly as the editor reads a
// picked file: the WHATWG "UTF-8 decode" (`new TextDecoder('utf-8')`), which drops one leading byte
// order mark and replaces every MAXIMAL invalid subsequence with one U+FFFD.
//
// Go's own `strings.ToValidUTF8` and range loop replace differently (a whole run, or every byte), so
// a file with a truncated multi-byte sequence would import to different property values in the two
// implementations. Valid UTF-8 — what every Quake III editor writes — is returned unchanged.
func DecodeText(data []byte) string {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	if utf8.Valid(data) {
		return string(data)
	}
	var builder strings.Builder
	builder.Grow(len(data))
	for index := 0; index < len(data); {
		b := data[index]
		if b < 0x80 {
			builder.WriteByte(b)
			index++
			continue
		}
		// The WHATWG decoder's state machine: the lead byte fixes how many continuation bytes follow
		// and the range the FIRST of them may take; the first byte that breaks the pattern ends the
		// maximal subpart, and is then read afresh.
		need, lower, upper := 0, byte(0x80), byte(0xBF)
		switch {
		case b >= 0xC2 && b <= 0xDF:
			need = 1
		case b >= 0xE0 && b <= 0xEF:
			need = 2
			if b == 0xE0 {
				lower = 0xA0
			} else if b == 0xED {
				upper = 0x9F
			}
		case b >= 0xF0 && b <= 0xF4:
			need = 3
			if b == 0xF0 {
				lower = 0x90
			} else if b == 0xF4 {
				upper = 0x8F
			}
		default:
			builder.WriteRune(utf8.RuneError)
			index++
			continue
		}
		end := index + 1
		ok := true
		for seen := 0; seen < need; seen++ {
			if end >= len(data) || data[end] < lower || data[end] > upper {
				ok = false
				break
			}
			lower, upper = 0x80, 0xBF
			end++
		}
		if !ok {
			builder.WriteRune(utf8.RuneError)
			index = end
			continue
		}
		builder.Write(data[index:end])
		index = end
	}
	return builder.String()
}

// utf16Length is a string's length in UTF-16 code units: what a JavaScript `string.length` and the
// reference reader's columns count.
func utf16Length(text string) int {
	length := 0
	for _, r := range text {
		if r >= 0x10000 {
			length += 2
		} else {
			length++
		}
	}
	return length
}

// utf16Prefix is `text.slice(0, units)` in UTF-16 code units, never cutting a rune in two.
func utf16Prefix(text string, units int) string {
	used := 0
	for index, r := range text {
		width := 1
		if r >= 0x10000 {
			width = 2
		}
		if used+width > units {
			return text[:index]
		}
		used += width
	}
	return text
}
