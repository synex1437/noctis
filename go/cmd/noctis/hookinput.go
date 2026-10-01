package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
)

// Claude Code hands the PostToolBatch hook the whole output of every tool of the batch, under the
// tool_response of each entry of tool_calls: each file read, each command's output, each
// subagent's report, megabytes when the tools read big files. noctis reads only which tools ran
// and the commands they ran, so readHookInput leaves the outputs out. It never builds them, and
// past the first window of the input it does not hold them either: it reads the rest a window at
// a time, checks it as decodeJSON would and keeps only the bytes noctis decodes. Holding and
// building them cost a batch about 6 ms per megabyte of output; reading past them, about 2.

// hookInputWindow is how much of a hook's stdin is read at a time. An input shorter than one is
// decoded whole, as decodeJSON decodes it, and only then loses its outputs.
const hookInputWindow = 64 << 10

// readHookInput reads a host's JSON from r to its end and gives back what decodeJSON makes of it,
// without the tool_response of any entry of the tool_calls list at its top; nil when the input
// is only space. An input shorter than a window that is not JSON is refused with
// encoding/json's error, as before; a longer one with where it stops being JSON.
func readHookInput(r io.Reader) (value any, readErr, parseErr error) {
	return readHookInputBy(r, make([]byte, hookInputWindow))
}

// readHookInputBy is readHookInput with window as the window.
func readHookInputBy(r io.Reader, window []byte) (value any, readErr, parseErr error) {
	filled, err := io.ReadFull(r, window)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		content := window[:filled]
		if len(bytes.TrimSpace(content)) == 0 {
			return nil, nil, nil
		}
		value, err := decodeJSON(content)
		if err != nil {
			return nil, nil, err
		}
		dropToolOutputs(value)
		return value, nil, nil
	}
	filter := outputFilter{failedAt: -1}
	for filter.feed(window[:filled]) && err == nil {
		filled, err = r.Read(window)
	}
	if filter.failedAt >= 0 {
		// The rest is read all the same, as it always was. When only space came before the byte
		// that is not JSON, the input is still only space if the rest is, as bytes.TrimSpace sees
		// space.
		blank := filter.state == filterValue && len(filter.stack) == 0
		rest := window[filter.failedAt-filter.fed : filled : filled]
		if err == nil && blank {
			var more []byte
			more, err = io.ReadAll(r)
			rest = append(rest, more...)
		} else if err == nil {
			_, err = io.Copy(io.Discard, r)
		}
		if err != nil && err != io.EOF {
			return nil, err, nil
		}
		if blank && len(bytes.TrimSpace(rest)) == 0 {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("not JSON at byte %d", filter.failedAt)
	}
	if err != io.EOF {
		return nil, err, nil
	}
	ok, empty := filter.finish()
	switch {
	case !ok:
		return nil, nil, fmt.Errorf("not JSON at byte %d", filter.failedAt)
	case empty:
		return nil, nil, nil
	}
	value, err = decodeJSON(filter.kept)
	if err != nil {
		return nil, nil, err
	}
	return value, nil, nil
}

// readWholeJSON reads r to its end and decodes all of it, the outputs of a batch's tools included.
func readWholeJSON(r io.Reader) (value any, readErr, parseErr error) {
	content, err := io.ReadAll(r)
	if err != nil {
		return nil, err, nil
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil, nil
	}
	value, err = decodeJSON(content)
	if err != nil {
		return nil, nil, err
	}
	return value, nil, nil
}

// dropToolOutputs leaves out the tool_response of each entry of the tool_calls list at the top of
// value.
func dropToolOutputs(value any) {
	document, _ := value.(object)
	calls, _ := document["tool_calls"].([]any)
	for _, call := range calls {
		if entry, ok := call.(object); ok {
			delete(entry, "tool_response")
		}
	}
}

// What outputFilter expects next.
const (
	filterValue      = iota // a value, after any space
	filterValueOrEnd        // after '[': a value or ']'
	filterKeyOrEnd          // after '{': a member's name or '}'
	filterKey               // after ',' in a map: a member's name
	filterColon             // after a member's name: ':'
	filterNext              // after a value: ',' or the end of its map or list
	filterDone              // after the document: space only
	filterString            // more of a string
	filterEscape            // the rest of an escape, after its backslash
	filterHex               // the digits of a \u escape
	filterNumber            // more of a number
	filterLiteral           // more of true, false or null
)

// maxWatchedName is the longest a member's name may be written (every letter of tool_response a
// \u escape) and still be one outputFilter looks for.
const maxWatchedName = 6 * len("tool_response")

// outputFilter takes a JSON document a piece at a time and keeps all of it in kept but the
// members named tool_response of the maps in the tool_calls list at its top, which it checks and
// drops. It reads JSON as decodeJSON does: a document it takes, decodeJSON takes, and kept is
// that document without those members; a document it refuses, decodeJSON refuses.
type outputFilter struct {
	kept     []byte
	stack    []byte // the maps ('{') and lists ('[') open around the next byte
	state    int
	failedAt int64 // where the document stopped being JSON; -1 while it is
	fed      int64 // the bytes of the pieces fed before this one
	from     int   // where the bytes of this piece not yet kept start; -1 while a value is dropped

	key     bool   // the string being read names a member
	watched bool   // and the member is one of the top map's or of an entry's, so its name counts
	name    []byte // that name as written, up to one byte past maxWatchedName
	hexLeft int    // the digits of a \u escape still to come
	number  []byte // the number being read
	literal string // the literal being read, and how much of it was matched
	matched int

	topCalls  bool // the top map's member being read is tool_calls
	callsOpen bool // the list at depth 2 is that member's value: its maps are the entries
	dropping  bool // the value of an entry's tool_response is being read
	mark      int  // where in kept the entry's member being read starts
	comma     bool // a ',' between the entry's members waits for the next member that is kept
	entryKept bool // a member of the entry was kept
}

// feed reads piece, the next bytes of the document. It reports whether they are still JSON.
func (f *outputFilter) feed(piece []byte) bool {
	if f.failedAt >= 0 {
		return false
	}
	f.from = 0
	if f.dropping {
		f.from = -1
	}
	for at := 0; at < len(piece); at++ {
		c := piece[at]
		switch f.state {
		case filterString:
			end := jsonStringStop(piece, at)
			if f.watched {
				f.capture(piece[at:end])
			}
			if at = end; at == len(piece) {
				break
			}
			switch c = piece[at]; {
			case c == '"':
				if f.key {
					f.nameEnd()
				} else {
					f.endValue(at + 1)
				}
			case c == '\\':
				if f.watched {
					f.capture(piece[at : at+1])
				}
				f.state = filterEscape
			default:
				return f.fail(at)
			}
		case filterEscape:
			switch c {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				f.state = filterString
			case 'u':
				f.state, f.hexLeft = filterHex, 4
			default:
				return f.fail(at)
			}
			if f.watched {
				f.capture(piece[at : at+1])
			}
		case filterHex:
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
				return f.fail(at)
			}
			if f.watched {
				f.capture(piece[at : at+1])
			}
			if f.hexLeft--; f.hexLeft == 0 {
				f.state = filterString
			}
		case filterNumber:
			if '0' <= c && c <= '9' || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
				f.number = append(f.number, c)
				break
			}
			if !f.numberEnds(at) {
				return f.fail(at)
			}
			at-- // the byte after the number is read again, in the state the number left
		case filterLiteral:
			if c != f.literal[f.matched] {
				return f.fail(at)
			}
			if f.matched++; f.matched == len(f.literal) {
				f.endValue(at + 1)
			}
		case filterValue, filterValueOrEnd:
			switch {
			case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			case c == ']' && f.state == filterValueOrEnd:
				if !f.close(c) {
					return f.fail(at)
				}
				f.endValue(at + 1)
			case c == '{' || c == '[':
				if !f.open(c) {
					return f.fail(at)
				}
			case c == '"':
				f.state, f.key, f.watched = filterString, false, false
			case c == 't':
				f.state, f.literal, f.matched = filterLiteral, "true", 1
			case c == 'f':
				f.state, f.literal, f.matched = filterLiteral, "false", 1
			case c == 'n':
				f.state, f.literal, f.matched = filterLiteral, "null", 1
			case c == '-' || '0' <= c && c <= '9':
				f.state, f.number = filterNumber, append(f.number[:0], c)
			default:
				return f.fail(at)
			}
		case filterKeyOrEnd, filterKey:
			switch {
			case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			case c == '}' && f.state == filterKeyOrEnd:
				if !f.close(c) {
					return f.fail(at)
				}
				f.endValue(at + 1)
			case c == '"':
				f.state, f.key, f.name = filterString, true, f.name[:0]
				f.watched = len(f.stack) == 1 || f.inEntry()
				if f.inEntry() {
					f.kept = append(f.kept, piece[f.from:at]...)
					f.from, f.mark = at, len(f.kept)
					if f.comma && f.entryKept {
						f.kept = append(f.kept, ',')
					}
					f.comma = false
				}
			default:
				return f.fail(at)
			}
		case filterColon:
			switch {
			case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			case c == ':':
				f.state = filterValue
			default:
				return f.fail(at)
			}
		case filterNext:
			switch {
			case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			case c == ',':
				if f.stack[len(f.stack)-1] == '[' {
					f.state = filterValue
					break
				}
				f.state = filterKey
				if f.inEntry() {
					f.kept = append(f.kept, piece[f.from:at]...)
					f.from, f.comma = at+1, true
				}
			case c == '}' || c == ']':
				if !f.close(c) {
					return f.fail(at)
				}
				f.endValue(at + 1)
			default:
				return f.fail(at)
			}
		default: // filterDone
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				return f.fail(at)
			}
		}
	}
	if f.from >= 0 {
		f.kept = append(f.kept, piece[f.from:]...)
	}
	f.fed += int64(len(piece))
	return true
}

// finish ends the document. It reports whether all of it was JSON, and whether it was only space.
func (f *outputFilter) finish() (ok, empty bool) {
	if f.failedAt >= 0 {
		return false, false
	}
	if f.state == filterNumber && !f.numberEnds(0) {
		f.failedAt = f.fed
		return false, false
	}
	if f.state == filterValue && len(f.stack) == 0 {
		return true, true
	}
	if f.state != filterDone {
		f.failedAt = f.fed
		return false, false
	}
	return true, false
}

func (f *outputFilter) fail(at int) bool {
	f.failedAt = f.fed + int64(at)
	return false
}

// inEntry reports whether the bytes being read are those of an entry of the tool_calls list, and
// not of a value in it.
func (f *outputFilter) inEntry() bool {
	return f.callsOpen && len(f.stack) == 3 && f.stack[2] == '{'
}

// open starts the map or list c opens, as decodeJSON's object and array start one.
func (f *outputFilter) open(c byte) bool {
	if len(f.stack) == 1 && f.topCalls && c == '[' {
		f.callsOpen = true
	}
	if f.stack = append(f.stack, c); len(f.stack) > jsonMaxDepth {
		return false
	}
	if c == '[' {
		f.state = filterValueOrEnd
		return true
	}
	f.state = filterKeyOrEnd
	if f.inEntry() {
		f.comma, f.entryKept = false, false
	}
	return true
}

// close ends the map or list c closes, if it is the one open.
func (f *outputFilter) close(c byte) bool {
	opened := byte('{')
	if c == ']' {
		opened = '['
	}
	if len(f.stack) == 0 || f.stack[len(f.stack)-1] != opened {
		return false
	}
	if f.stack = f.stack[:len(f.stack)-1]; len(f.stack) == 1 {
		f.callsOpen = false
	}
	return true
}

// endValue follows a value that ends before the byte at next: when it is the dropped output of
// an entry, the bytes from next on are kept again.
func (f *outputFilter) endValue(next int) {
	if f.dropping && len(f.stack) == 3 {
		f.dropping, f.from = false, next
	}
	f.state = filterNext
	if len(f.stack) == 0 {
		f.state = filterDone
	}
}

// numberEnds ends the number read so far, which must be one number as decodeJSON reads it (its
// value in range included), before the byte at next.
func (f *outputFilter) numberEnds(next int) bool {
	decoder := jsonDecoder{data: f.number}
	if _, ok := decoder.number(); !ok || decoder.pos != len(f.number) {
		return false
	}
	f.endValue(next)
	return true
}

// capture adds written to the name of the member being read, up to one byte past
// maxWatchedName.
func (f *outputFilter) capture(written []byte) {
	if room := maxWatchedName + 1 - len(f.name); room > 0 {
		f.name = append(f.name, written[:min(room, len(written))]...)
	}
}

// nameEnd follows the name of a member: tool_calls at the top marks the list whose maps are the
// entries, and an entry's tool_response is dropped, with the ',' before it.
func (f *outputFilter) nameEnd() {
	f.state, f.key = filterColon, false
	if !f.watched {
		return
	}
	if len(f.stack) == 1 {
		f.topCalls = f.nameIs("tool_calls")
		return
	}
	if !f.nameIs("tool_response") {
		f.entryKept = true
		return
	}
	f.kept = f.kept[:f.mark]
	f.dropping, f.from = true, -1
}

// nameIs reports whether the name just read is want, its escapes resolved.
func (f *outputFilter) nameIs(want string) bool {
	if len(f.name) > maxWatchedName {
		return false
	}
	if bytes.IndexByte(f.name, '\\') < 0 {
		return string(f.name) == want
	}
	quoted := append(append([]byte{'"'}, f.name...), '"')
	decoder := jsonDecoder{data: quoted}
	name, ok := decoder.string()
	return ok && name == want
}

// jsonStringStop is where the first quote, backslash or control byte from at on is in piece, or
// len(piece) when there is none.
func jsonStringStop(piece []byte, at int) int {
	for at+8 <= len(piece) {
		if mask := jsonStopMask(binary.LittleEndian.Uint64(piece[at:])); mask != 0 {
			return at + bits.TrailingZeros64(mask)>>3
		}
		at += 8
	}
	for at < len(piece) && piece[at] >= ' ' && piece[at] != '"' && piece[at] != '\\' {
		at++
	}
	return at
}

// jsonStopMask sets the high bit of the first byte of word (the lowest) that is a control byte,
// a quote or a backslash; bits above it may be set too, and bytes past ASCII never set one. The
// usual zero-byte tests give each of the three exactly up to their first match, so the first
// match of all three is exact.
func jsonStopMask(word uint64) uint64 {
	quote, slash := word^(jsonLowBits*'"'), word^(jsonLowBits*'\\')
	return ((word-jsonLowBits*' ')&^word | (quote-jsonLowBits)&^quote | (slash-jsonLowBits)&^slash) & jsonHighBits
}
