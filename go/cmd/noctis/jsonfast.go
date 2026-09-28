package main

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
)

// The JSON noctis reads and writes on every hook and status line (state.json, usage.json, the
// config, the hook's stdin and what it prints) is plain maps, lists, text, numbers and literals.
// decodeJSON and encodeJSON handle those in one pass without reflection, and give back exactly
// what encoding/json gives back: the same values, the same bytes. Whatever they cannot vouch for
// (a syntax error, a number out of range, a type they do not know) goes to encoding/json whole,
// so errors and odd values come out as they always did.

// jsonMaxDepth is how deeply encoding/json lets a document nest before it refuses it.
const jsonMaxDepth = 10000

// jsonShortObject is how many members a map holds in its first group of slots.
const jsonShortObject = 8

// jsonTextChunk is how much of a document one conversion to text covers. The strings decoded
// from it are pieces of that text, so one string kept after the rest is dropped holds at most
// this much more of the document in memory.
const jsonTextChunk = 4096

// jsonPow10 holds the powers of ten a float64 represents exactly.
var jsonPow10 = [...]float64{1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22}

type jsonMember struct {
	key   string
	value any
}

type jsonDecoder struct {
	data    []byte
	pos     int
	depth   int
	text    string
	textAt  int
	members []jsonMember
	items   []any
	scratch []byte
}

// decodeJSON is json.Unmarshal(data, &value) for a value of type any.
func decodeJSON(data []byte) (any, error) {
	decoder := jsonDecoder{data: data}
	if value, ok := decoder.document(); ok {
		return value, nil
	}
	var value any
	err := json.Unmarshal(data, &value)
	return value, err
}

func (d *jsonDecoder) document() (any, bool) {
	d.skipSpace()
	value, ok := d.value()
	if !ok {
		return nil, false
	}
	d.skipSpace()
	return value, d.pos == len(d.data)
}

func (d *jsonDecoder) skipSpace() {
	data, at := d.data, d.pos
	for at < len(data) {
		if c := data[at]; c > ' ' || (c != ' ' && c != '\n' && c != '\t' && c != '\r') {
			break
		}
		at++
	}
	d.pos = at
}

func (d *jsonDecoder) value() (any, bool) {
	if d.pos >= len(d.data) {
		return nil, false
	}
	switch c := d.data[d.pos]; c {
	case '{':
		return d.object()
	case '[':
		return d.array()
	case '"':
		text, ok := d.string()
		return text, ok
	case 't':
		return true, d.literal("true")
	case 'f':
		return false, d.literal("false")
	case 'n':
		return nil, d.literal("null")
	default:
		number, ok := d.number()
		return number, ok
	}
}

func (d *jsonDecoder) literal(word string) bool {
	if len(d.data)-d.pos < len(word) || string(d.data[d.pos:d.pos+len(word)]) != word {
		return false
	}
	d.pos += len(word)
	return true
}

// object reads a map. The members of a short one go straight into it; once one has more than
// fit in a map's first group they are gathered first, so the map is made once at its full size.
func (d *jsonDecoder) object() (any, bool) {
	d.pos++
	if d.depth++; d.depth > jsonMaxDepth {
		return nil, false
	}
	d.skipSpace()
	decoded := map[string]any{}
	if d.pos < len(d.data) && d.data[d.pos] == '}' {
		d.pos++
		d.depth--
		return decoded, true
	}
	base := -1
	for {
		if d.pos >= len(d.data) || d.data[d.pos] != '"' {
			return nil, false
		}
		key, ok := d.string()
		if !ok {
			return nil, false
		}
		d.skipSpace()
		if d.pos >= len(d.data) || d.data[d.pos] != ':' {
			return nil, false
		}
		d.pos++
		d.skipSpace()
		value, ok := d.value()
		if !ok {
			return nil, false
		}
		switch {
		case base >= 0:
			d.members = append(d.members, jsonMember{key, value})
		case len(decoded) < jsonShortObject:
			decoded[key] = value
		default:
			if _, seen := decoded[key]; seen {
				decoded[key] = value
				break
			}
			base = len(d.members)
			for earlier, held := range decoded {
				d.members = append(d.members, jsonMember{earlier, held})
			}
			d.members = append(d.members, jsonMember{key, value})
		}
		d.skipSpace()
		if d.pos >= len(d.data) {
			return nil, false
		}
		next := d.data[d.pos]
		d.pos++
		if next == '}' {
			break
		}
		if next != ',' {
			return nil, false
		}
		d.skipSpace()
	}
	if base >= 0 {
		members := d.members[base:]
		decoded = make(map[string]any, len(members))
		for _, member := range members {
			decoded[member.key] = member.value
		}
		d.members = d.members[:base]
	}
	d.depth--
	return decoded, true
}

func (d *jsonDecoder) array() (any, bool) {
	d.pos++
	if d.depth++; d.depth > jsonMaxDepth {
		return nil, false
	}
	d.skipSpace()
	if d.pos < len(d.data) && d.data[d.pos] == ']' {
		d.pos++
		d.depth--
		return []any{}, true
	}
	base := len(d.items)
	for {
		value, ok := d.value()
		if !ok {
			return nil, false
		}
		d.items = append(d.items, value)
		d.skipSpace()
		if d.pos >= len(d.data) {
			return nil, false
		}
		next := d.data[d.pos]
		d.pos++
		if next == ']' {
			break
		}
		if next != ',' {
			return nil, false
		}
		d.skipSpace()
	}
	decoded := make([]any, len(d.items)-base)
	copy(decoded, d.items[base:])
	d.items = d.items[:base]
	d.depth--
	return decoded, true
}

// number reads a number as strconv.ParseFloat does. Up to 15 digits scaled by at most 10^22 are
// exact in a float64, and one multiplication or division of two exact values is rounded once, as
// ParseFloat rounds; anything longer or larger goes to ParseFloat itself.
func (d *jsonDecoder) number() (float64, bool) {
	data, start := d.data, d.pos
	at := start
	negative := data[at] == '-'
	if negative {
		at++
	}
	if at >= len(data) {
		return 0, false
	}
	var mantissa uint64
	digits := 0
	switch c := data[at]; {
	case c == '0':
		at++
		digits++
	case '1' <= c && c <= '9':
		for at < len(data) && '0' <= data[at] && data[at] <= '9' {
			mantissa = mantissa*10 + uint64(data[at]-'0')
			digits++
			at++
		}
	default:
		return 0, false
	}
	exponent := 0
	if at < len(data) && data[at] == '.' {
		at++
		fraction := at
		for at < len(data) && '0' <= data[at] && data[at] <= '9' {
			mantissa = mantissa*10 + uint64(data[at]-'0')
			digits++
			at++
		}
		if at == fraction {
			return 0, false
		}
		exponent = fraction - at
	}
	if at < len(data) && (data[at] == 'e' || data[at] == 'E') {
		at++
		sign := 1
		if at < len(data) && (data[at] == '+' || data[at] == '-') {
			if data[at] == '-' {
				sign = -1
			}
			at++
		}
		first, power := at, 0
		for at < len(data) && '0' <= data[at] && data[at] <= '9' {
			if power < 100000 {
				power = power*10 + int(data[at]-'0')
			}
			at++
		}
		if at == first {
			return 0, false
		}
		exponent += sign * power
	}
	d.pos = at
	if digits <= 15 && -22 <= exponent && exponent <= 22 {
		value := float64(mantissa)
		if exponent > 0 {
			value *= jsonPow10[exponent]
		} else if exponent < 0 {
			value /= jsonPow10[-exponent]
		}
		if negative {
			value = -value
		}
		return value, true
	}
	value, err := strconv.ParseFloat(string(data[start:at]), 64)
	return value, err == nil
}

// string reads the string that starts at the quote under d.pos. Text with no escapes and no
// broken UTF-8 is a piece of the document's text; the rest is unquoted as encoding/json unquotes
// it.
func (d *jsonDecoder) string() (string, bool) {
	data := d.data
	start := d.pos + 1
	at, high := start, uint64(0)
	for {
		for at+8 <= len(data) {
			word := binary.LittleEndian.Uint64(data[at:])
			if jsonStops(word) {
				break
			}
			high |= word
			at += 8
		}
		for at < len(data) && jsonPlainText[data[at]] {
			at++
		}
		if at >= len(data) {
			return "", false
		}
		switch c := data[at]; {
		case c == '"':
			if high&jsonHighBits != 0 && !utf8.Valid(data[start:at]) {
				return d.unquote(start)
			}
			d.pos = at + 1
			return d.textOf(start, at), true
		case c == '\\':
			return d.unquote(start)
		case c < ' ':
			return "", false
		default:
			high |= jsonHighBits
			at++
		}
	}
}

func (d *jsonDecoder) textOf(start, end int) string {
	if start == end {
		return ""
	}
	if start < d.textAt || end > d.textAt+len(d.text) {
		stop := min(max(end, start+jsonTextChunk), len(d.data))
		d.text, d.textAt = string(d.data[start:stop]), start
	}
	return d.text[start-d.textAt : end-d.textAt]
}

// unquote is encoding/json's unquote: escapes resolved, a \u surrogate that is not half of a
// pair and every byte that is not UTF-8 made U+FFFD.
func (d *jsonDecoder) unquote(start int) (string, bool) {
	data, out := d.data, d.scratch[:0]
	for at := start; at < len(data); {
		switch c := data[at]; {
		case c == '"':
			d.pos, d.scratch = at+1, out
			return string(out), true
		case c == '\\':
			if at+1 >= len(data) {
				return "", false
			}
			switch escaped := data[at+1]; escaped {
			case '"', '\\', '/':
				out = append(out, escaped)
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'u':
				char := jsonHex4(data, at+2)
				if char < 0 {
					return "", false
				}
				at += 6
				if utf16.IsSurrogate(char) {
					low := rune(-1)
					if at+1 < len(data) && data[at] == '\\' && data[at+1] == 'u' {
						low = jsonHex4(data, at+2)
					}
					if pair := utf16.DecodeRune(char, low); pair != utf8.RuneError {
						out = utf8.AppendRune(out, pair)
						at += 6
						continue
					}
					char = utf8.RuneError
				}
				out = utf8.AppendRune(out, char)
				continue
			default:
				return "", false
			}
			at += 2
		case c < ' ':
			return "", false
		case c < utf8.RuneSelf:
			out = append(out, c)
			at++
		default:
			char, size := utf8.DecodeRune(data[at:])
			out = utf8.AppendRune(out, char)
			at += size
		}
	}
	return "", false
}

func jsonHex4(data []byte, at int) rune {
	if at+4 > len(data) {
		return -1
	}
	var char rune
	for _, c := range data[at : at+4] {
		switch {
		case '0' <= c && c <= '9':
			c -= '0'
		case 'a' <= c && c <= 'f':
			c -= 'a' - 10
		case 'A' <= c && c <= 'F':
			c -= 'A' - 10
		default:
			return -1
		}
		char = char*16 + rune(c)
	}
	return char
}

// jsonPlainText marks the bytes a JSON string holds as they are: printable ASCII but the quote
// and the backslash.
var jsonPlainText = func() (plain [256]bool) {
	for c := ' '; c < utf8.RuneSelf; c++ {
		plain[c] = c != '"' && c != '\\'
	}
	return plain
}()

// jsonHTMLPlainText is jsonPlainText without <, > and &, which json.Marshal escapes.
var jsonHTMLPlainText = func() [256]bool {
	plain := jsonPlainText
	plain['<'], plain['>'], plain['&'] = false, false, false
	return plain
}()

// A string is scanned eight bytes at a time while none of them needs a closer look, with the
// usual tests on the bytes of a word: (x - 0x0101..01) &^ x & 0x8080..80 is not zero when some
// byte of x is zero, so it finds a byte equal to c in x ^ c*0x0101..01, and a byte below n (up to
// 0x80) with n*0x0101..01 subtracted in place of 0x0101..01.
const (
	jsonLowBits  = 0x0101010101010101
	jsonHighBits = 0x8080808080808080
)

// jsonHasByte reports whether some byte of word is c.
func jsonHasByte(word uint64, c byte) bool {
	match := word ^ jsonLowBits*uint64(c)
	return (match-jsonLowBits)&^match&jsonHighBits != 0
}

// jsonStops reports whether some byte of word ends plain text in a JSON string: a control byte,
// a quote or a backslash. Bytes past ASCII never set it off.
func jsonStops(word uint64) bool {
	return (word-jsonLowBits*' ')&^word&jsonHighBits != 0 || jsonHasByte(word, '"') || jsonHasByte(word, '\\')
}

// jsonHTMLStops reports whether some byte of word is <, > or &, which json.Marshal escapes.
func jsonHTMLStops(word uint64) bool {
	return jsonHasByte(word, '<') || jsonHasByte(word, '>') || jsonHasByte(word, '&')
}

// jsonWord is the eight bytes of text from at on, the first one lowest.
func jsonWord(text string, at int) uint64 {
	_ = text[at+7]
	return uint64(text[at]) | uint64(text[at+1])<<8 | uint64(text[at+2])<<16 | uint64(text[at+3])<<24 |
		uint64(text[at+4])<<32 | uint64(text[at+5])<<40 | uint64(text[at+6])<<48 | uint64(text[at+7])<<56
}

const jsonHexDigits = "0123456789abcdef"

// jsonEncodeDepth is how deeply encodeJSON follows maps and lists. encoding/json starts looking
// for a map or list that holds itself past this depth, so deeper values are left to it.
const jsonEncodeDepth = 1000

type jsonEncoder struct {
	out        []byte
	escapeHTML bool
	indent     bool
	depth      int
	// members holds the members of the maps being written, each map's above its parent's, and
	// peak is how many it has held at once in this encoding; order and sorted are where
	// sortMembers orders a long map's.
	members []jsonMember
	peak    int
	order   []uint64
	spread  []uint64
	sorted  []jsonMember
}

// jsonEncoders keeps the buffers of the last encoding for the next one.
var jsonEncoders = sync.Pool{New: func() any { return new(jsonEncoder) }}

// encodeJSON is value encoded as json.Encoder encodes it (escapeHTML as SetEscapeHTML sets it,
// indent as SetIndent("", "  ")) without the newline it ends with, in a slice of its own. It
// reports false for a value it cannot vouch for: the caller then encodes it with encoding/json.
func encodeJSON(value any, escapeHTML, indent bool) ([]byte, bool) {
	encoder := jsonEncoders.Get().(*jsonEncoder)
	encoder.out, encoder.escapeHTML, encoder.indent, encoder.depth = encoder.out[:0], escapeHTML, indent, 0
	ok := encoder.value(value)
	var out []byte
	if ok {
		out = append(make([]byte, 0, len(encoder.out)), encoder.out...)
	}
	// What the buffers held is dropped, so the pool keeps no value alive.
	clear(encoder.members[:encoder.peak])
	encoder.members, encoder.peak = encoder.members[:0], 0
	if cap(encoder.out) <= 4<<20 && cap(encoder.members) <= 1<<16 {
		jsonEncoders.Put(encoder)
	}
	return out, ok
}

func (e *jsonEncoder) value(value any) bool {
	switch typed := value.(type) {
	case string:
		e.out = appendJSONString(e.out, typed, e.escapeHTML)
	case float64:
		return e.float(typed)
	case map[string]any:
		return e.object(typed)
	case nil:
		e.out = append(e.out, "null"...)
	case bool:
		e.out = strconv.AppendBool(e.out, typed)
	case []any:
		if typed == nil {
			e.out = append(e.out, "null"...)
			return true
		}
		if !e.begin('[', len(typed)) {
			return false
		}
		for index, item := range typed {
			e.separate(index)
			if !e.value(item) {
				return false
			}
		}
		e.end(']', len(typed))
	case int:
		e.out = strconv.AppendInt(e.out, int64(typed), 10)
	case int64:
		e.out = strconv.AppendInt(e.out, typed, 10)
	case []string:
		if typed == nil {
			e.out = append(e.out, "null"...)
			return true
		}
		if !e.begin('[', len(typed)) {
			return false
		}
		for index, item := range typed {
			e.separate(index)
			e.out = appendJSONString(e.out, item, e.escapeHTML)
		}
		e.end(']', len(typed))
	case map[string]int:
		if typed == nil {
			e.out = append(e.out, "null"...)
			return true
		}
		if !e.begin('{', len(typed)) {
			return false
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for index, key := range keys {
			e.separate(index)
			e.key(key)
			e.out = strconv.AppendInt(e.out, int64(typed[key]), 10)
		}
		e.end('}', len(typed))
	default:
		return false
	}
	return true
}

func (e *jsonEncoder) object(object map[string]any) bool {
	if object == nil {
		e.out = append(e.out, "null"...)
		return true
	}
	if !e.begin('{', len(object)) {
		return false
	}
	base := len(e.members)
	for key, value := range object {
		e.members = append(e.members, jsonMember{key, value})
	}
	e.peak = max(e.peak, len(e.members))
	members := e.members[base:]
	e.sortMembers(members)
	for index, member := range members {
		e.separate(index)
		e.key(member.key)
		if !e.value(member.value) {
			return false
		}
	}
	e.members = e.members[:base]
	e.end('}', len(members))
	return true
}

// key writes a map key and the colon after it.
func (e *jsonEncoder) key(key string) {
	e.out = appendJSONString(e.out, key, e.escapeHTML)
	e.out = append(e.out, ':')
	if e.indent {
		e.out = append(e.out, ' ')
	}
}

// begin opens a map or a list of length members, and reports false when it is nested too deeply.
func (e *jsonEncoder) begin(bracket byte, length int) bool {
	e.out = append(e.out, bracket)
	if length == 0 {
		return true
	}
	e.depth++
	return e.depth <= jsonEncodeDepth
}

func (e *jsonEncoder) separate(index int) {
	if index > 0 {
		e.out = append(e.out, ',')
	}
	e.newline()
}

// end closes what begin opened: an empty map or list stays on one line, as {} or [].
func (e *jsonEncoder) end(bracket byte, length int) {
	if length > 0 {
		e.depth--
		e.newline()
	}
	e.out = append(e.out, bracket)
}

func (e *jsonEncoder) newline() {
	if e.indent {
		e.out = append(e.out, '\n')
		for level := 0; level < e.depth; level++ {
			e.out = append(e.out, ' ', ' ')
		}
	}
}

// sortMembers orders members by key as encoding/json orders map keys: byte by byte. A long map's
// are ordered first as plain numbers, each the six bytes after what all its keys begin with and
// the member's place below them; the members whose six bytes tie are then ordered by key.
func (e *jsonEncoder) sortMembers(members []jsonMember) {
	if len(members) <= 12 || len(members) > 1<<16 {
		sortJSONMembers(members)
		return
	}
	shared := jsonSharedPrefix(members)
	order := e.order[:0]
	for place, member := range members {
		key, prefix := member.key[shared:], uint64(0)
		for at := range 6 {
			prefix <<= 8
			if at < len(key) {
				prefix |= uint64(key[at])
			}
		}
		order = append(order, prefix<<16|uint64(place))
	}
	// The numbers are spread by their first byte, one pass of a counting sort, and each run that
	// shares it is sorted apart: many short sorts cost less than one long one.
	var ends [256]int
	for _, entry := range order {
		ends[entry>>56]++
	}
	total := 0
	for first, count := range ends {
		ends[first] = total
		total += count
	}
	spread := slices.Grow(e.spread[:0], len(order))[:len(order)]
	for _, entry := range order {
		spread[ends[entry>>56]] = entry
		ends[entry>>56]++
	}
	begin := 0
	for _, end := range ends {
		if end-begin > 1 {
			slices.Sort(spread[begin:end])
		}
		begin = end
	}
	sorted := append(e.sorted[:0], members...)
	for place, entry := range spread {
		members[place] = sorted[entry&0xffff]
	}
	clear(sorted)
	for start := 0; start < len(spread); {
		end := start + 1
		for end < len(spread) && spread[end]>>16 == spread[start]>>16 {
			end++
		}
		sortJSONMembers(members[start:end])
		start = end
	}
	e.order, e.spread, e.sorted = order, spread, sorted
}

// jsonSharedPrefix is how many bytes all the members' keys begin with.
func jsonSharedPrefix(members []jsonMember) int {
	first := members[0].key
	shared := len(first)
	for _, member := range members[1:] {
		key, same := member.key, 0
		for same < shared && same < len(key) && key[same] == first[same] {
			same++
		}
		if shared = same; shared == 0 {
			break
		}
	}
	return shared
}

// sortJSONMembers orders members by key, byte by byte.
func sortJSONMembers(members []jsonMember) {
	if len(members) > 12 {
		slices.SortFunc(members, func(a, b jsonMember) int { return strings.Compare(a.key, b.key) })
		return
	}
	for next := 1; next < len(members); next++ {
		for at := next; at > 0 && jsonKeyBefore(members[at].key, members[at-1].key); at-- {
			members[at], members[at-1] = members[at-1], members[at]
		}
	}
}

func jsonKeyBefore(a, b string) bool {
	if a != "" && b != "" && a[0] != b[0] {
		return a[0] < b[0]
	}
	return a < b
}

// float writes a number as encoding/json does: like JavaScript, with an exponent below 1e-6 and
// from 1e21 on, and none for the whole numbers a float64 holds exactly.
func (e *jsonEncoder) float(value float64) bool {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return false
	}
	if value == math.Trunc(value) && math.Abs(value) < 1<<53 {
		if value == 0 && math.Signbit(value) {
			e.out = append(e.out, '-', '0')
		} else {
			e.out = strconv.AppendInt(e.out, int64(value), 10)
		}
		return true
	}
	format := byte('f')
	if abs := math.Abs(value); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	e.out = strconv.AppendFloat(e.out, value, format, -1, 64)
	if n := len(e.out); format == 'e' && e.out[n-4] == 'e' && e.out[n-3] == '-' && e.out[n-2] == '0' {
		e.out[n-2] = e.out[n-1]
		e.out = e.out[:n-1]
	}
	return true
}

// appendJSONString is encoding/json's string encoder: the quote, the backslash and control bytes
// escaped, <, > and & too when escapeHTML is set, U+2028 and U+2029 always, and every byte that
// is not UTF-8 written as the escape of U+FFFD.
func appendJSONString(out []byte, text string, escapeHTML bool) []byte {
	plain := &jsonPlainText
	if escapeHTML {
		plain = &jsonHTMLPlainText
	}
	out = append(out, '"')
	start := 0
	for at := 0; at < len(text); {
		for at+8 <= len(text) {
			word := jsonWord(text, at)
			if word&jsonHighBits != 0 || jsonStops(word) || escapeHTML && jsonHTMLStops(word) {
				break
			}
			at += 8
		}
		for at < len(text) && plain[text[at]] {
			at++
		}
		if at >= len(text) {
			break
		}
		if c := text[at]; c < utf8.RuneSelf {
			out = append(out, text[start:at]...)
			switch c {
			case '\\', '"':
				out = append(out, '\\', c)
			case '\b':
				out = append(out, '\\', 'b')
			case '\f':
				out = append(out, '\\', 'f')
			case '\n':
				out = append(out, '\\', 'n')
			case '\r':
				out = append(out, '\\', 'r')
			case '\t':
				out = append(out, '\\', 't')
			default:
				out = append(out, '\\', 'u', '0', '0', jsonHexDigits[c>>4], jsonHexDigits[c&0xF])
			}
			at++
			start = at
			continue
		}
		char, size := utf8.DecodeRuneInString(text[at:])
		switch {
		case char == utf8.RuneError && size == 1:
			out = append(out, text[start:at]...)
			out = append(out, '\\', 'u', 'f', 'f', 'f', 'd')
		case char == 0x2028 || char == 0x2029:
			out = append(out, text[start:at]...)
			out = append(out, '\\', 'u', '2', '0', '2', jsonHexDigits[char&0xF])
		default:
			at += size
			continue
		}
		at += size
		start = at
	}
	out = append(out, text[start:]...)
	return append(out, '"')
}
