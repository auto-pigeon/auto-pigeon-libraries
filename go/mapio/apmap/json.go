package apmap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Value is one decoded JSON value. The concrete types are:
//
//	nil          JSON null
//	bool         JSON true / false
//	string       JSON string
//	json.Number  JSON number, kept as its literal text
//	[]Value      JSON array
//	*Object      JSON object, in document order
//
// This is deliberately not encoding/json's default `any` shape, for two
// reasons that both come straight from what the validator needs.
//
// # Numbers stay literals
//
// encoding/json decodes every number into a float64 unless told otherwise,
// which loses two distinctions the validator depends on. First, JSON Schema's
// "integer" versus "number": a literal is an integer exactly when it has no
// fraction and no exponent, and json.Number preserves the literal, so the
// question is answerable. Second, range: the APMap
// 1.0 test vector `non-finite-number.apmap` contains 1e400, which the
// specification's SEM-11 rule rejects as non-finite — but that is a *semantic*
// rule, so the schema layer must decode the document successfully and report
// no problem. A float64 decode of 1e400 fails outright; UseNumber does not.
//
// # Objects keep their order
//
// The validator visits schema keywords and document members in document
// order, and that order is observable: `type` short-circuits the rest of a
// schema node, and every other keyword appends problems in the order it visits
// members. A Go map iterates randomly, so a plain map[string]any would emit the
// same problems in a different, unstable order on every run — the opposite of
// the determinism AGENTS.md §3 requires. Object keeps the order.
type Value any

// Object is a JSON object that remembers the order its members appeared in.
type Object struct {
	keys   []string
	values map[string]Value
	// replaced names the string members whose JSON text held an UNPAIRED SURROGATE escape
	// (`"\ud83d"` with no low surrogate after it, or a lone low one). encoding/json cannot represent
	// one in a Go string and silently substitutes U+FFFD, so by the time a Value exists the
	// author's text has already been changed; this is the only record that it was. Set by the
	// decoder alone, and cleared by Set and Delete — a value a caller supplies is not the one the
	// file held. SEM-A-1 reads it (HadUnpairedSurrogate).
	replaced map[string]bool
}

// NewObject builds an empty object. Exported for tests that construct schema
// fragments directly rather than parsing them.
func NewObject() *Object {
	return &Object{values: map[string]Value{}}
}

// Set adds or replaces a member. A replaced member keeps its original
// position, matching Python's dict — which is what a duplicate key in a JSON
// document does there.
func (o *Object) Set(name string, value Value) {
	if o.values == nil {
		o.values = map[string]Value{}
	}
	delete(o.replaced, name)
	if _, exists := o.values[name]; !exists {
		o.keys = append(o.keys, name)
	}
	o.values[name] = value
}

// Get returns the member and whether it is present.
func (o *Object) Get(name string) (Value, bool) {
	if o == nil {
		return nil, false
	}
	value, ok := o.values[name]
	return value, ok
}

// Has reports whether the member is present, including when its value is null.
func (o *Object) Has(name string) bool {
	_, ok := o.Get(name)
	return ok
}

// Delete removes a member, keeping the order of the rest. Removing a member
// that is not present is not an error, matching dict.pop(name, None) — which is
// how every caller in this repository uses it.
//
// The missing counterpart to Set on an insertion-ordered object. Added for
// the consumer's fault-injection catalogue, whose schema_missing_version_field and
// schema_missing_required_field exist precisely to drop a required member.
func (o *Object) Delete(name string) {
	if o == nil {
		return
	}
	if _, exists := o.values[name]; !exists {
		return
	}
	delete(o.values, name)
	delete(o.replaced, name)
	for index, key := range o.keys {
		if key == name {
			o.keys = append(o.keys[:index], o.keys[index+1:]...)
			break
		}
	}
}

// HadUnpairedSurrogate reports whether the named string member was decoded from JSON text holding
// an unpaired UTF-16 surrogate escape, which the decoder could only replace with U+FFFD.
//
// APMap 1.4's SEM-A-1 is the reason it exists: an `authorship` string that is not Unicode text must
// be REFUSED, because re-emitting the replacement character would silently change the author's
// words. A reader cannot see that from the decoded string — U+FFFD is also a perfectly legal
// character a file may hold literally — so the decoder records it from the raw bytes.
func (o *Object) HadUnpairedSurrogate(name string) bool {
	if o == nil {
		return false
	}
	return o.replaced[name]
}

// Keys returns the member names in document order. The slice is a copy, so a
// caller cannot reorder the object by sorting it.
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	return append([]string(nil), o.keys...)
}

// Len is the member count.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// DecodeJSON parses one JSON document into the Value shape above. Trailing
// content after the document is an error, matching json.loads.
func DecodeJSON(data []byte) (Value, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	state := &jsonDecoder{Decoder: decoder, data: data}

	value, err := state.decodeValue()
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing content after the JSON document")
	}
	return value, nil
}

// jsonDecoder is the token stream plus the raw bytes it reads, which is what lets an object member
// be traced back to its own JSON text (see Object.replaced).
type jsonDecoder struct {
	*json.Decoder
	data []byte
}

func (decoder *jsonDecoder) decodeValue() (Value, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	return decoder.decodeFromToken(token)
}

func (decoder *jsonDecoder) decodeFromToken(token json.Token) (Value, error) {
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			object := NewObject()
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("object member name is not a string")
				}
				// The member's raw text runs from the end of its name to the end of its value.
				start := decoder.InputOffset()
				value, err := decoder.decodeValue()
				if err != nil {
					return nil, err
				}
				object.Set(key, value)
				// Only a string that now holds U+FFFD can have had a surrogate replaced, so the
				// raw scan runs on a handful of members in a whole corpus rather than on every one.
				if text, ok := value.(string); ok && strings.ContainsRune(text, utf8.RuneError) &&
					hasUnpairedSurrogateEscape(decoder.data[start:decoder.InputOffset()]) {
					if object.replaced == nil {
						object.replaced = map[string]bool{}
					}
					object.replaced[key] = true
				}
			}
			if _, err := decoder.Token(); err != nil { // closing '}'
				return nil, err
			}
			return object, nil
		case '[':
			// Non-nil empty slice, so an empty array stays distinguishable
			// from a missing one after a type assertion.
			items := []Value{}
			for decoder.More() {
				item, err := decoder.decodeValue()
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			if _, err := decoder.Token(); err != nil { // closing ']'
				return nil, err
			}
			return items, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", typed)
		}
	case nil:
		return nil, nil
	case bool:
		return typed, nil
	case string:
		return typed, nil
	case json.Number:
		return typed, nil
	default:
		return nil, fmt.Errorf("unexpected JSON token %T", token)
	}
}

// hasUnpairedSurrogateEscape reports whether the first JSON string literal in raw contains a
// `\uXXXX` escape naming a UTF-16 surrogate that is not half of a valid pair: a high surrogate
// (D800–DBFF) not immediately followed by a `\u` low one, or a low surrogate (DC00–DFFF) with no
// high one before it. raw is the text between a member name and the end of its value, so it holds
// separators and exactly one string literal. An escaped backslash (`\\u...`) is not an escape of
// `u`, which is why this walks escapes rather than searching for "\ud8".
func hasUnpairedSurrogateEscape(raw []byte) bool {
	index := bytes.IndexByte(raw, '"')
	if index < 0 {
		return false
	}
	pendingHigh := false
	for index++; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			return pendingHigh
		case '\\':
			if index+1 >= len(raw) {
				return false
			}
			if raw[index+1] != 'u' || index+5 >= len(raw) {
				if pendingHigh {
					return true // a high surrogate followed by some other escape
				}
				index++ // the escaped character, whatever it is
				continue
			}
			unit, err := strconv.ParseUint(string(raw[index+2:index+6]), 16, 16)
			if err != nil {
				return false // not a valid escape; the decoder has already refused the document
			}
			index += 5
			switch {
			case unit >= 0xD800 && unit <= 0xDBFF:
				if pendingHigh {
					return true // two highs in a row: the first is unpaired
				}
				pendingHigh = true
				continue
			case unit >= 0xDC00 && unit <= 0xDFFF:
				if !pendingHigh {
					return true // a low surrogate with no high one before it
				}
				pendingHigh = false
				continue
			}
			if pendingHigh {
				return true
			}
		default:
			if pendingHigh {
				return true // a high surrogate followed by anything but a low escape
			}
		}
	}
	return pendingHigh
}

// Plain converts a decoded Value tree into the plain Go values encoding/json
// can write: *Object becomes map[string]any, []Value becomes []any, and every
// scalar passes through unchanged.
//
// # Why this is needed at all
//
// Object deliberately has no MarshalJSON. Its members are unexported, so
// encoding/json writes it as `{}` — and its canonical serialisation is Encode,
// which implements the APMap specification's own formatting rather than
// encoding/json's. Giving it a MarshalJSON would make `json.Marshal(document)`
// silently produce something that is *not* a canonical APMap document, which is
// a worse trap than requiring this call.
//
// So anything that hands part of a document to encoding/json — the inspection
// summary's `to_source`, an HTTP response envelope carrying a manifest — goes
// through here first. Anything that must stay canonical goes through Encode
// instead.
//
// # What is lost
//
// Member order. A Go map has none, and encoding/json sorts map keys. That is
// acceptable wherever this is used and nowhere else: those outputs are read by
// a JSON client, not parsed as APMap documents. It is the reason this is a
// named conversion a caller has to ask for rather than something that happens
// implicitly.
func Plain(value Value) any {
	switch typed := value.(type) {
	case *Object:
		if typed == nil {
			return nil
		}
		out := make(map[string]any, typed.Len())
		for _, key := range typed.Keys() {
			member, _ := typed.Get(key)
			out[key] = Plain(member)
		}
		return out
	case []Value:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, Plain(item))
		}
		return out
	default:
		return value
	}
}
