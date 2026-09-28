package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sameJSON reports whether a and b are the same decoded value, down to the sign of every zero and
// whether each map and list is nil.
func sameJSON(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && math.Float64bits(x) == math.Float64bits(y)
	case object:
		y, ok := b.(object)
		if !ok || (x == nil) != (y == nil) || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			if other, present := y[key]; !present || !sameJSON(value, other) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || (x == nil) != (y == nil) || len(x) != len(y) {
			return false
		}
		for index := range x {
			if !sameJSON(x[index], y[index]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// shown is value for a failure message, cut short.
func shown(value any) string {
	text := fmt.Sprintf("%#v", value)
	if len(text) > 300 {
		return text[:300] + "..."
	}
	return text
}

// checkDecode fails t unless decodeJSON gives back for data what json.Unmarshal does (the same
// value, or the same error), and unless the decoder itself took every document encoding/json
// accepts, leaving it only the ones it refuses.
func checkDecode(t testing.TB, data []byte) {
	t.Helper()
	var want any
	wantErr := json.Unmarshal(data, &want)
	got, err := decodeJSON(data)
	if (err == nil) != (wantErr == nil) || err != nil && err.Error() != wantErr.Error() {
		t.Fatalf("decoding %s: error %v, encoding/json's is %v", shown(string(data)), err, wantErr)
	}
	if !sameJSON(got, want) {
		t.Fatalf("decoding %s gave %s, encoding/json gives %s", shown(string(data)), shown(got), shown(want))
	}
	decoder := jsonDecoder{data: data}
	fast, handled := decoder.document()
	if handled != (wantErr == nil) {
		t.Fatalf("decoding %s: the decoder took it: %v; encoding/json's error: %v", shown(string(data)), handled, wantErr)
	}
	if handled && !sameJSON(fast, want) {
		t.Fatalf("decoding %s: the decoder gave %s, encoding/json gives %s", shown(string(data)), shown(fast), shown(want))
	}
}

// referenceJSON is what a json.Encoder writes for value, without the newline it ends with: what
// noctis wrote before encodeJSON.
func referenceJSON(value any, escapeHTML, indent bool) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(escapeHTML)
	if indent {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

// jsonHandled reports whether encodeJSON must write value itself: all of it of the types it
// knows, no NaN or infinity, and no more than jsonEncodeDepth maps and lists that are not empty
// inside one another. depth is how many hold value.
func jsonHandled(value any, depth int) bool {
	switch typed := value.(type) {
	case nil, bool, string, int, int64:
		return true
	case float64:
		return !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case []string, map[string]int:
		return reflect.ValueOf(typed).Len() == 0 || depth < jsonEncodeDepth
	case object:
		if len(typed) > 0 && depth >= jsonEncodeDepth {
			return false
		}
		for _, item := range typed {
			if !jsonHandled(item, depth+1) {
				return false
			}
		}
		return true
	case []any:
		if len(typed) > 0 && depth >= jsonEncodeDepth {
			return false
		}
		for _, item := range typed {
			if !jsonHandled(item, depth+1) {
				return false
			}
		}
		return true
	}
	return false
}

// checkEncode fails t unless encodeJSON writes value byte for byte as encoding/json does, in each
// way noctis asks for it (HTML escaped or not, indented or not), and declines exactly the values
// it leaves to encoding/json; and unless marshalCompact still writes what it wrote before.
func checkEncode(t testing.TB, value any) {
	t.Helper()
	handled := jsonHandled(value, 0)
	for _, escapeHTML := range []bool{false, true} {
		for _, indent := range []bool{false, true} {
			want, wantErr := referenceJSON(value, escapeHTML, indent)
			got, ok := encodeJSON(value, escapeHTML, indent)
			if ok != handled {
				t.Fatalf("encoding %s (html %v, indented %v): taken %v, want %v", shown(value), escapeHTML, indent, ok, handled)
			}
			if ok && wantErr != nil {
				t.Fatalf("encoding %s (html %v, indented %v) wrote %q; encoding/json refuses it: %v", shown(value), escapeHTML, indent, got, wantErr)
			}
			if ok && !bytes.Equal(got, want) {
				t.Fatalf("encoding %s (html %v, indented %v):\ngot  %q\nwant %q", shown(value), escapeHTML, indent, got, want)
			}
		}
	}
	if marshaled, err := json.Marshal(value); err == nil {
		if got, ok := encodeJSON(value, true, false); ok && !bytes.Equal(got, marshaled) {
			t.Fatalf("encoding %s escaped for HTML: %q, json.Marshal writes %q", shown(value), got, marshaled)
		}
	}
	want, _ := referenceJSON(value, false, false)
	if got := marshalCompact(value); !bytes.Equal(got, want) || (got == nil) != (want == nil) {
		t.Fatalf("marshalCompact(%s) = %q, it wrote %q before", shown(value), got, want)
	}
}

// jsonDecodeCases are documents that probe every corner of the grammar, of the number and text
// conversions and of the errors.
var jsonDecodeCases = []string{
	// Literals, space and what surrounds a document.
	"null", "true", "false", " \t\r\n true \n", "tru", "nul", "fals", "truex", "nullnull", "[true,false,null]",
	"", " ", "\n\t", "\xef\xbb\xbf{}", "{} x", "{}{}", "1 2", "[1] ]", "{}\x00", "\v1", "\f1", "1\xc2\xa0",
	// Numbers.
	"0", "-0", "-0.0", "0.0", "0e0", "-0e-0", "1", "-1", "7", "10", "1.5", "-2.25", "0.1", "0.30000000000000004", "4.35",
	"123456789012345", "1234567890123456", "12345678901234567890", "123456789012345678901234567890",
	"9007199254740993", "9007199254740992", "1e22", "1e23", "123456789012345e22", "123456789012345e-22", "1e-22", "1e-23",
	"1E5", "1e+5", "1e-5", "2.5E-3", "1e400", "-1e400", "1e-400", "5e-324", "2e-324", "1.7976931348623157e308",
	"1.7976931348623159e308", "1e99999999999", "1e-99999999999", "0.000000000000000000000000000001", "1790456864", "1790456864.25",
	"01", "-01", "00", "-", "-a", "+1", ".5", "1.", "1.e5", "1e", "1e+", "1e-", "--1", "0x10", "1_000", "Infinity", "NaN",
	"[1,2,3.5,-4e2]", "{\"n\":-0}", "[-0,0,-0.0]", "[1e400,2]", "{\"a\":1e400,\"b\":[1]}",
	// Text: escapes, surrogates, control bytes and UTF-8 that is broken.
	"\"\"", "\"a\"", "\"hello world, a longer text\"", "\"\\u00e9\"", "\"\\u00E9t\\u00e9\"", "\"\\/\"",
	"\"\\ud83d\\ude00\"", "\"\\uD83D\\uDE00\"", "\"\\ud83d\"", "\"\\ude00\"", "\"\\ud83d\\u0041\"", "\"\\ud83d\\ud83d\\ude00\"",
	"\"\\ud83dx\"", "\"\\ud83d\\\"\"", "\"\\ud83d\\n\"", "\"\\udbff\\udfff\"", "\"\\ud800\\udc00\"", "\"\\ude00\\ud83d\"",
	"\"\\u0000\"", "\"\\u001f\"", "\"\\u2028\\u2029\"", "\"\\uffff\"", "\"\\ufffd\"", "\"\\u12G4\"", "\"\\u12\"", "\"\\u\"",
	"\"\\ud83d\\u12\"", "\"\\ud83d\\uZZZZ\"", "\"\\\"\\\\\\/\\b\\f\\n\\r\\t\"", "\"\\'\"", "\"\\x41\"", "\"\\a\"", "\"\\", "\"abc",
	"\"a\x01b\"", "\"a\x1fb\"", "\"a\x7fb\"", "\"tab\there\"", "\"new\nline\"", "\"\xff\"", "\"caf\xe9\"", "\"\xe2\x82\"",
	"\"\xe2\x82\xac\"", "\"\xed\xa0\x80\"", "\"\xc0\xaf\"", "\"\xf4\x90\x80\x80\"", "\"\xf0\x9f\x98\x80\"", "\"a\xffb\\nc\"",
	"\"\xe2\x80\xa8\"", "\"\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e\"", "\"12345678\\n12345678\xff12345678\"", "\"1234567\"", "\"12345678\"",
	"\"\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9 and more text after it\"", "\"\x00\"", "\"\x80\x80\x80\x80\x80\x80\x80\x80\x80\"",
	// Maps.
	"{}", "{ }", "{\"a\":1}", "{\"a\":1,\"a\":2}", "{\"a\":{\"x\":1},\"a\":{\"y\":2}}", "{\"a\":[1],\"b\":{\"c\":null}}",
	"{\"\":0}", "{\"a\":1,}", "{\"a\" 1}", "{a:1}", "{\"a\":1 \"b\":2}", "{,}", "{\"a\"}", "{\"a\":}", "{\"a\":1", "{\"a\":1]",
	"{1:2}", "{\"a\\u0000b\":1}", "{\"\xff\":1,\"\xfe\":2}", "{\"\\ud83d\":1,\"\\ude00\":2}", " { \"a\" : [ 1 , 2 ] , \"b\" : { } } ",
	"{\"k0\":0,\"k1\":1,\"k2\":2,\"k3\":3,\"k4\":4,\"k5\":5,\"k6\":6,\"k7\":7}",
	"{\"k0\":0,\"k1\":1,\"k2\":2,\"k3\":3,\"k4\":4,\"k5\":5,\"k6\":6,\"k7\":7,\"k8\":8}",
	"{\"k0\":0,\"k1\":1,\"k2\":2,\"k3\":3,\"k4\":4,\"k5\":5,\"k6\":6,\"k7\":7,\"k0\":\"again\",\"k8\":8,\"k9\":9,\"k3\":\"last\",\"k8\":[]}",
	"{\"k0\":0,\"k0\":1,\"k0\":2,\"k0\":3,\"k0\":4,\"k0\":5,\"k0\":6,\"k0\":7,\"k0\":8,\"k0\":9}",
	"{\"k0\":0,\"k1\":1,\"k2\":2,\"k3\":3,\"k4\":4,\"k5\":5,\"k6\":6,\"k7\":7,\"k8\":8,\"k9\":9,\"bad\"}",
	// Lists.
	"[]", "[ ]", "[1]", "[1,]", "[,1]", "[1 2]", "[[[]]]", "[[],{},\"\",0]", "[", "]", "[1,[2,[3]]", "[\"a\",\"a\",\"a\"]",
}

func TestJSONFastDecodeMatchesEncodingJSON(t *testing.T) {
	for _, input := range jsonDecodeCases {
		checkDecode(t, []byte(input))
	}
}

// Numbers of every length, with and without a fraction and an exponent, each read as ParseFloat
// reads it: the decoder's shortcut for short ones must round as it does.
func TestJSONFastDecodeNumbers(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for range 50000 {
		var number strings.Builder
		if rng.IntN(4) == 0 {
			number.WriteByte('-')
		}
		digits := 1 + rng.IntN(20)
		number.WriteByte(byte('1' + rng.IntN(9)))
		for range digits - 1 {
			number.WriteByte(byte('0' + rng.IntN(10)))
		}
		text := number.String()
		if point := rng.IntN(digits + 2); point < digits {
			text = text[:len(text)-point] + "." + text[len(text)-point:]
			if strings.HasPrefix(text, ".") || strings.HasPrefix(text, "-.") {
				text = strings.Replace(text, ".", "0.", 1)
			}
		}
		if rng.IntN(2) == 0 {
			text += fmt.Sprintf("e%d", rng.IntN(61)-30)
		}
		checkDecode(t, []byte(text))
		checkDecode(t, []byte("["+text+"]"))
	}
}

func TestJSONFastDecodeDepth(t *testing.T) {
	for _, depth := range []int{1, 100, jsonMaxDepth - 1, jsonMaxDepth, jsonMaxDepth + 1, jsonMaxDepth + 50} {
		checkDecode(t, []byte(strings.Repeat("[", depth)+strings.Repeat("]", depth)))
		checkDecode(t, []byte(strings.Repeat("{\"a\":", depth)+"1"+strings.Repeat("}", depth)))
		checkDecode(t, []byte(strings.Repeat("[{\"a\":", depth/2)+"null"+strings.Repeat("}]", depth/2)))
		checkDecode(t, []byte(strings.Repeat("[", depth)+"1,"+strings.Repeat("]", depth)))
	}
}

// Text past the few kilobytes one conversion covers, strings that cross from one conversion into
// the next, and many members in one map.
func TestJSONFastDecodeLongDocuments(t *testing.T) {
	var document strings.Builder
	document.WriteString("{")
	for index := 0; index < 300; index++ {
		if index > 0 {
			document.WriteString(",")
		}
		fmt.Fprintf(&document, "%q:[%q,%d.5,{\"n\":%d}]", fmt.Sprintf("key-%d", index), strings.Repeat("x", index*7%1300), index, -index)
	}
	document.WriteString("}")
	checkDecode(t, []byte(document.String()))
	for _, size := range []int{jsonTextChunk - 3, jsonTextChunk, jsonTextChunk + 1, 3*jsonTextChunk + 5} {
		text := strings.Repeat("abcdefgh", size/8+1)[:size]
		checkDecode(t, []byte("[\""+text+"\",\""+text+"\\n\",\"x\"]"))
	}
	var members strings.Builder
	members.WriteString("{")
	for index := 0; index < 5000; index++ {
		fmt.Fprintf(&members, "\"member-%d\":%d,", index%4000, index)
	}
	members.WriteString("\"end\":true}")
	checkDecode(t, []byte(members.String()))
}

func TestJSONFastDecodedTextDoesNotShareTheBuffer(t *testing.T) {
	data := []byte("{\"key\":\"value\",\"list\":[\"a\",\"b\\n\"],\"" + strings.Repeat("k", 5000) + "\":\"" + strings.Repeat("v", 5000) + "\"}")
	var want any
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	for index := range data {
		data[index] = 'x'
	}
	if !sameJSON(decoded, want) {
		t.Fatal("changing the bytes a document was decoded from changed the text decoded from them")
	}
}

// jsonFloatCases are the numbers at the edges of how encoding/json writes them.
var jsonFloatCases = []float64{
	0, math.Copysign(0, -1), 1, -1, 0.1, 0.5, 1.5, -2.25, 100, 1790456864, 1790456864.25, 1e-6, 9.99e-7, 1e-7, 1.5e-10,
	1.0000000000000002e-6, 9.999999999999999e-7, 1e15 + 0.3, 1e20, 1e21, 1e22, 9.999999999999999e20, 123456789.125,
	1<<53 - 1, 1 << 53, 1<<53 + 2, -(1 << 53), 12345678901234567890, 5e-324, 2.2250738585072014e-308, math.MaxFloat64,
	1.5e300, -2.5e-300, 1.0 / 3, 2.0 / 3, 0.30000000000000004, 3e-7, 4.35, 1e-5, 0.000001, 0.0000011,
	math.Nextafter(1e21, 0), math.Nextafter(1e-6, 0), math.Nextafter(1<<53, 0), math.Nextafter(1<<53, math.Inf(1)),
}

func TestJSONFastEncodeNumbers(t *testing.T) {
	for _, number := range jsonFloatCases {
		checkEncode(t, number)
		checkEncode(t, -number)
		checkEncode(t, []any{number, object{"n": -number}})
	}
	for _, refused := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		checkEncode(t, refused)
		checkEncode(t, object{"fine": 1.5, "refused": []any{refused}})
	}
	for _, whole := range []int64{0, 1, -1, 42, math.MaxInt32, math.MinInt32, math.MaxInt64, math.MinInt64, 1 << 53, -(1<<53 + 1)} {
		checkEncode(t, whole)
		checkEncode(t, int(whole))
	}
	rng := rand.New(rand.NewPCG(75, 7))
	for range 20000 {
		checkEncode(t, math.Float64frombits(rng.Uint64()))
		checkEncode(t, float64(rng.Int64N(1<<62)-1<<61)/math.Pow10(rng.IntN(30)))
		checkEncode(t, rng.Float64()*math.Pow10(rng.IntN(60)-30))
	}
}

// jsonSpecialText is text that each needs a closer look from the encoder: what it escapes always,
// what it escapes for HTML, bytes past ASCII that are UTF-8 and bytes that are not.
var jsonSpecialText = []string{
	"\"", "\\", "\n", "\r", "\t", "\b", "\f", "\x00", "\x01", "\x1f", "\x7f", "<", ">", "&", "\xff", "\xc3", "\xc3\xa9",
	"\xe2\x80\xa8", "\xe2\x80\xa9", "\xe2\x80\xa7", "\xe2\x80\xaa", "\xed\xa0\x80", "\xef\xbf\xbd", "\xf0\x9f\x98\x80", "\xf0\x9f\x98",
	"\xe6\x97\xa5\xe6\x9c\xac",
}

func TestJSONFastEncodeText(t *testing.T) {
	for c := 0; c < 256; c++ {
		checkEncode(t, string(rune(c)))
		checkEncode(t, string([]byte{byte(c)}))
		checkEncode(t, "a<b>"+string([]byte{byte(c)})+"&c")
	}
	for _, special := range jsonSpecialText {
		for at := 0; at <= 24; at++ {
			text := strings.Repeat("abcdefgh", 4)
			checkEncode(t, text[:at]+special+text[at:])
			checkEncode(t, special+text[:at]+special)
		}
		checkEncode(t, strings.Repeat(special, 20))
		checkEncode(t, object{special: special, "key" + special: []any{special}})
	}
	checkEncode(t, "")
	checkEncode(t, strings.Repeat("plain text that runs on ", 400))
	checkEncode(t, strings.Repeat("\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e and <html> & \"quotes\"\n", 300))
}

func TestJSONFastEncodeValues(t *testing.T) {
	values := []any{
		nil, true, false, "text", object(nil), object{}, []any(nil), []any{}, []string(nil), []string{}, []string{"a", "<b>"},
		map[string]int(nil), map[string]int{}, map[string]int{"b": 2, "a": -1, "": 0, "\xff": 3, "<": 4},
		object{"a": object{}, "b": []any{}, "c": []any{object{}}, "d": object{"e": []any{[]any{}}}, "f": object(nil), "g": []any(nil)},
		object{"z": 1.0, "a": "x", "m": []any{true, nil, "y", 2.5, object{"k": false}}},
		[]any{object{}, []any{object{"a": []string{}}}, map[string]int{}, []string(nil)},
		object{"count": 7, "big": int64(1) << 60, "names": []string{"a", "b"}, "events": map[string]int{"stop": 3, "hook": 12}},
	}
	for _, value := range values {
		checkEncode(t, value)
	}
	for _, refused := range []any{
		json.Number("12"), json.RawMessage("{\"a\":1}"), struct{ A int }{1}, float32(1.5), uint(3), int32(4),
		map[string]string{"a": "b"}, []object{{"a": 1.0}}, []map[string]any{}, map[string]any{"odd": json.Number("1")},
		object{"list": []any{1.0, struct{}{}}}, func() {}, make(chan int), &struct{}{},
	} {
		checkEncode(t, refused)
	}
}

// Maps are written with their keys in byte order: short ones sorted in place, long ones by the six
// bytes after the beginning all their keys share, then by whole key where those tie.
func TestJSONFastEncodeSortsKeysAsEncodingJSON(t *testing.T) {
	odd := []string{"", "a", "b", "B", "A", "aa", "a\x00", "\x00", "\xc3\xa9", "\xff", "\x80", "\x7f", "<", "\xe2\x80\xa8", "a b", "ab", "abcdefghij"}
	for size := 1; size <= len(odd); size++ {
		members := object{}
		for _, key := range odd[:size] {
			members[key] = key
		}
		checkEncode(t, members)
	}
	rng := rand.New(rand.NewPCG(1, 2))
	shapes := map[string]func(int) string{
		"random":       func(int) string { return fmt.Sprintf("%016x", rng.Uint64()) },
		"shared start": func(int) string { return fmt.Sprintf("session-%08x", rng.Uint32()) },
		"ties":         func(index int) string { return "prefix-abcdef" + strings.Repeat("\x00", index%3) + fmt.Sprint(index%7) },
		"lengths":      func(index int) string { return "k" + strings.Repeat("\x00", index) },
		"high bytes":   func(index int) string { return "\xc0\x80\xff\x7f"[index%4:] + fmt.Sprint(index*37%101) },
		"long shared":  func(index int) string { return strings.Repeat("x", 40) + fmt.Sprintf("%03d", index%500) + "tail" },
		"some shared":  func(index int) string { return strings.Repeat("a", index%9) + fmt.Sprint(index) },
		"broken":       func(index int) string { return "\xff" + fmt.Sprint(index%20) + "\xfe" },
	}
	for name, shape := range shapes {
		for _, size := range []int{12, 13, 14, 50, 300} {
			members := object{}
			for index := 0; index < size; index++ {
				members[shape(index)] = float64(index)
			}
			t.Run(fmt.Sprintf("%s/%d", name, size), func(t *testing.T) { checkEncode(t, members) })
		}
	}
	// More members than sortMembers numbers apart: they are sorted by whole key.
	huge := make(object, 1<<16+10)
	for index := 0; index < 1<<16+10; index++ {
		huge[fmt.Sprintf("%x-%d", rng.Uint32(), index)] = float64(index)
	}
	checkEncode(t, huge)
}

func TestJSONFastEncodeDepth(t *testing.T) {
	nested := func(depth int, leaf any) any {
		value := leaf
		for level := 0; level < depth; level++ {
			if level%2 == 0 {
				value = object{"a": value}
			} else {
				value = []any{value}
			}
		}
		return value
	}
	for _, depth := range []int{jsonEncodeDepth - 1, jsonEncodeDepth, jsonEncodeDepth + 1, jsonEncodeDepth + 10} {
		checkEncode(t, nested(depth, 1.0))
		checkEncode(t, nested(depth, object{}))
		checkEncode(t, nested(depth, []any{}))
		checkEncode(t, nested(depth, []string{"a"}))
	}
	cycle := object{"name": "loop"}
	cycle["self"] = cycle
	checkEncode(t, cycle)
}

// The helpers store.go writes and reads JSON with still write the bytes and give back the values
// and errors they did when they used encoding/json alone.
func TestJSONHelpersBehaveAsBefore(t *testing.T) {
	sandboxFiles(t)
	pretty := func(value any) []byte {
		encoded, err := referenceJSON(value, false, true)
		if err != nil {
			return nil
		}
		return encoded
	}
	compact := func(value any) []byte {
		encoded, err := referenceJSON(value, false, false)
		if err != nil {
			return nil
		}
		return encoded
	}
	deep := any(1.0)
	for range jsonMaxDepth + 1 {
		deep = []any{deep}
	}
	cycle := object{}
	cycle["self"] = cycle
	values := []any{
		nil, "text <b>&\xe2\x80\xa8\xff", 1.5, math.NaN(), math.Inf(-1), object{}, object(nil), []any{}, []any(nil), realisticState(4096),
		object{"a": object{}, "b": []any{object{}}, "n": math.Copysign(0, -1), "e": 1e21, "s": []string{"x"}, "i": 3},
		object{"bad": math.NaN()}, json.Number("7"), json.Number("x"), map[string]string{"k": "<v>"}, deep, cycle,
	}
	for _, value := range values {
		if got, want := marshalState(value), compact(value); !bytes.Equal(got, want) || (got == nil) != (want == nil) {
			t.Fatalf("marshalState(%s) = %q, it wrote %q before", shown(value), got, want)
		}
		if got, want := marshalCompact(value), compact(value); !bytes.Equal(got, want) || (got == nil) != (want == nil) {
			t.Fatalf("marshalCompact(%s) = %q, it wrote %q before", shown(value), got, want)
		}
		if got, want := marshalPretty(value), pretty(value); !bytes.Equal(got, want) || (got == nil) != (want == nil) {
			t.Fatalf("marshalPretty(%s) = %q, it wrote %q before", shown(value), got, want)
		}
	}
	file := filepath.Join(t.TempDir(), "read.json")
	for _, content := range append(jsonDecodeCases, "\xef\xbb\xbf{\"bom\":true}", "\xef\xbb\xbf", "{\"big\":1e400}") {
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		dropParsed(file)
		read := readJSONShared(file)
		var want any
		err := json.Unmarshal(bytes.TrimPrefix([]byte(content), utf8BOM), &want)
		wantData, _ := want.(object)
		if read.ok != (err == nil) || err != nil && read.err != err.Error() || err == nil && !sameJSON(any(read.data), any(wantData)) {
			t.Fatalf("readJSONShared of %q: ok %v, err %q, data %s; encoding/json: %v, %s", content, read.ok, read.err, shown(read.data), err, shown(want))
		}
	}
}

func TestReadStdinJSONDecodesAsBefore(t *testing.T) {
	sandboxFiles(t)
	saved, savedLoaded, savedCache := os.Stdin, stdinLoaded, stdinCache
	t.Cleanup(func() { os.Stdin, stdinLoaded, stdinCache = saved, savedLoaded, savedCache })
	for _, content := range []string{"", " \n\t", "\xc2\xa0", "{\"a\":[1,\"x\"]}", "[1]", "\"text\"", "{bad", "\xef\xbb\xbf{}", "{\"n\":1e400}", "{\"k\":\"\\ud83d\"} "} {
		file := filepath.Join(t.TempDir(), "stdin.json")
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		opened, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		os.Stdin, stdinLoaded, stdinCache = opened, false, nil
		got := readStdinJSON()
		opened.Close()
		want := object{}
		var raw any
		if strings.TrimSpace(content) != "" && json.Unmarshal([]byte(content), &raw) == nil {
			if parsed, ok := raw.(object); ok {
				want = parsed
			}
		}
		if !sameJSON(any(got), any(want)) {
			t.Fatalf("readStdinJSON of %q gave %s, before it gave %s", content, shown(got), shown(want))
		}
	}
}

// jsonCopy keeps what parsing the bytes written for a value gives back; copyValue copies every map
// and list, an absent one included.
func TestJSONCopiesKeepTheirPromises(t *testing.T) {
	for _, value := range []any{
		object{"count": 7, "big": int64(1) << 60, "names": []string{"a", "b"}, "broken": "caf\xe9", "html": "<b>&</b>",
			"nested": object{"list": []any{1.0, "x", nil, object{}}, "none": nil}, "nilMap": object(nil), "events": map[string]int{"a": 1}},
		object{"plain": object{"a": 1.0, "b": "text", "c": true, "d": []any{nil}}},
		[]any{int64(3), []string(nil), object{"x": []any{2, "\xff"}}},
		"caf\xe9", 5, nil,
	} {
		copied, ok := jsonCopy(value)
		encoded, err := json.Marshal(value)
		if err != nil || !ok {
			t.Fatalf("jsonCopy(%s): ok %v, json.Marshal error %v", shown(value), ok, err)
		}
		var want any
		if err := json.Unmarshal(encoded, &want); err != nil {
			t.Fatal(err)
		}
		if !sameJSON(copied, want) {
			t.Fatalf("jsonCopy(%s) = %s, parsing its bytes gives %s", shown(value), shown(copied), shown(want))
		}
	}
	if _, ok := jsonCopy(object{"\xff": 1.0}); ok {
		t.Fatal("jsonCopy kept a map whose key is not UTF-8; encoding it changes the key")
	}
	source := object{"map": object{"list": []any{object{"x": 1.0}}}, "empty": object(nil), "none": []any(nil)}
	copied := copyValue(source).(object)
	if !reflect.DeepEqual(copied["empty"], object{}) || !reflect.DeepEqual(copied["none"], []any{}) {
		t.Fatalf("copyValue turned an absent map or list into %#v and %#v; it copies them as empty ones", copied["empty"], copied["none"])
	}
	copied["map"].(object)["list"].([]any)[0].(object)["x"] = 2.0
	if source["map"].(object)["list"].([]any)[0].(object)["x"] != 1.0 {
		t.Fatal("changing a copy changed the value it was copied from")
	}
}

func TestJSONFastOnTheRepositoryFiles(t *testing.T) {
	root := repoRoot()
	seen := 0
	for _, pattern := range []string{"config.default.json", "hooks/hooks.json", "i18n/*.json", ".claude-plugin/*.json", ".claude/settings.json"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range matches {
			content, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			checkDecode(t, content)
			var value any
			if err := json.Unmarshal(content, &value); err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			checkEncode(t, value)
			compact, _ := referenceJSON(value, false, false)
			checkDecode(t, compact)
			seen++
		}
	}
	if seen < 15 {
		t.Fatalf("only %d of the repository's JSON files were found", seen)
	}
}

// oddState is the realistic state with the values that need the most care written into it.
func oddState() object {
	state := realisticState(benchStateSize)
	stateMap(state, "sessionLocale")["odd"] = object{
		"at": math.Copysign(0, -1), "html": "<b>&amp;</b>", "separators": "\xe2\x80\xa8\xe2\x80\xa9", "broken": "caf\xe9\xff",
		"control": "\x00\x01\x1f\x7f", "tiny": 1e-7, "huge": 1e21, "count": 7, "big": int64(1) << 60, "names": []string{"a", "<b>"},
		"events": map[string]int{"stop": 1}, "none": object(nil), "empty": []any(nil),
	}
	return state
}

func TestJSONFastOnTheRealisticState(t *testing.T) {
	for _, state := range []object{realisticState(benchStateSize), oddState()} {
		checkEncode(t, state)
		for _, indent := range []bool{false, true} {
			for _, escapeHTML := range []bool{false, true} {
				encoded, err := referenceJSON(state, escapeHTML, indent)
				if err != nil {
					t.Fatal(err)
				}
				checkDecode(t, encoded)
			}
		}
	}
}

// jsonFuzzBuilder builds the value FuzzJSONFastEncode encodes from the fuzzer's bytes: each byte
// picks what comes next (a literal, text, a number, a whole number, a map, a list, a []string, a
// map[string]int, a long map, NaN or an infinity, a type encodeJSON leaves to encoding/json), and
// the bytes after it fill it in.
type jsonFuzzBuilder struct {
	data  []byte
	nodes int
}

func (b *jsonFuzzBuilder) next() byte {
	if len(b.data) == 0 {
		return 0
	}
	c := b.data[0]
	b.data = b.data[1:]
	return c
}

func (b *jsonFuzzBuilder) take(n int) []byte {
	n = min(n, len(b.data))
	taken := b.data[:n]
	b.data = b.data[n:]
	return taken
}

func (b *jsonFuzzBuilder) word() uint64 {
	var word [8]byte
	copy(word[:], b.take(8))
	return binary.LittleEndian.Uint64(word[:])
}

// text is up to 31 bytes of the input as they are, broken UTF-8 and all, some of it repeated.
func (b *jsonFuzzBuilder) text() string {
	c := b.next()
	text := string(b.take(int(c % 32)))
	if c >= 0xc0 {
		text = strings.Repeat(text, 1+int(c%5))
	}
	return text
}

func (b *jsonFuzzBuilder) value(depth int) any {
	b.nodes++
	op := b.next()
	kind, negative := op%16, op >= 0x80
	if depth >= 6 || b.nodes > 200 {
		kind %= 8
	}
	switch kind {
	case 0:
		return nil
	case 1:
		return negative
	case 2, 3:
		return b.text()
	case 4:
		return math.Float64frombits(b.word())
	case 5:
		number := jsonFloatCases[int(b.next())%len(jsonFloatCases)]
		if negative {
			number = -number
		}
		return number
	case 6:
		return float64(int32(b.word())) / float64(1+int(b.next())%1000)
	case 7:
		if negative {
			return int(int64(b.word()))
		}
		return int64(b.word())
	case 8, 9:
		count := int(b.next() % 16)
		if negative && count == 0 {
			return object(nil)
		}
		members := make(object, count)
		for range count {
			members[b.text()] = b.value(depth + 1)
		}
		return members
	case 10:
		count := int(b.next() % 8)
		if negative && count == 0 {
			return []any(nil)
		}
		items := make([]any, count)
		for index := range items {
			items[index] = b.value(depth + 1)
		}
		return items
	case 11:
		count := int(b.next() % 5)
		if negative && count == 0 {
			return []string(nil)
		}
		texts := make([]string, count)
		for index := range texts {
			texts[index] = b.text()
		}
		return texts
	case 12:
		count := int(b.next() % 5)
		if negative && count == 0 {
			return map[string]int(nil)
		}
		counts := make(map[string]int, count)
		for range count {
			counts[b.text()] = int(int16(b.word()))
		}
		return counts
	case 13:
		// A long map whose keys share a beginning, so the bytes after it order its members.
		prefix, count := b.text(), 13+int(b.next())%100
		members := make(object, count)
		for index := range count {
			members[prefix+b.text()] = float64(index)
		}
		return members
	case 14:
		return []float64{math.NaN(), math.Inf(1), math.Inf(-1)}[int(b.next())%3]
	default:
		switch b.next() % 6 {
		case 0:
			return json.Number(b.text())
		case 1:
			return float32(math.Float64frombits(b.word()))
		case 2:
			return uint(b.word())
		case 3:
			return map[string]string{b.text(): b.text()}
		case 4:
			return struct{ Name string }{b.text()}
		default:
			return json.RawMessage(b.text())
		}
	}
}

func FuzzJSONFastDecode(f *testing.F) {
	for _, input := range jsonDecodeCases {
		f.Add([]byte(input))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkDecode(t, data)
	})
}

func FuzzJSONFastEncode(f *testing.F) {
	for _, seed := range []string{
		"", "\x08\x03\x02\x01a\x05\x07\x02b", "\x09\x0f\x0d\x04pre-\x40\x03\x01a\x03\x01b\x03\x01c",
		"\x0a\x05\x02\x05<b>&\x04\x00\x00\x00\x00\x00\x00\xf0\x7f\x0b\x02\x02\xe2\x80",
		"{\"a\":[1,-0,1e21,\"\\u2028<&>\"],\"b\":{}}", "[\"\\ud83d\",0.000001,1e-7,{\"z\":null,\"a\":true}]",
		"\x8b\x8c\x88\x8a\x07\x01\x02\x03\x04\x05\x06\x07\x08", "\x0e\x01\x0f\x03\x0f\x00\x05x",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		builder := jsonFuzzBuilder{data: data}
		checkEncode(t, builder.value(0))
		var parsed any
		if json.Unmarshal(data, &parsed) == nil {
			checkEncode(t, parsed)
		}
	})
}

// realisticState builds a state.json the way weeks of heavy use leave it, until its compact
// encoding reaches size bytes: hundreds of sessions with their locale, model, stop, resume and
// continuation records, checkpoints with long paths, waits with queued prompts that carry escapes
// and text that is not ASCII, and a few lists.
func realisticState(size int) object {
	rng := rand.New(rand.NewPCG(7, 75))
	state := emptyState()
	now := 1790456864.0
	langs := []string{"en", "tr", "de", "ja", "pt"}
	models := []string{"model-large", "model-medium", "model-large-long", "model-small"}
	prompts := []string{
		"fix the typo in README.md and commit it",
		"Şu işleri yap:\n- Bu fonksiyonu yeniden yaz\n- Mevcut testleri güncelle\nBunları yapabilir misin?",
		"Refactor the \"store\" package; keep the <tag> & the \\path\\ as they are.\tThen run the tests.",
		"日本語のテキストを要約して、結果を notes.md に書いてください。",
		"araştır: why does the resume fire twice?",
	}
	project := "/home/dev/projects/noctis-sample/service"
	for batch := 0; ; batch++ {
		for index := batch * 10; index < batch*10+10; index++ {
			sid := fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", rng.Uint32(), rng.IntN(1<<16), rng.IntN(1<<12), rng.IntN(1<<12), rng.Uint64()&(1<<48-1))
			at := now - float64(rng.IntN(3*86400))
			stateMap(state, "sessionLocale")[sid] = object{"at": at, "lang": langs[rng.IntN(len(langs))]}
			stateMap(state, "notified")["queue:"+sid[:8]] = at + float64(rng.IntN(600))
			if index%2 == 0 {
				stateMap(state, "modelOverrides")[sid] = object{"at": at, "model": models[rng.IntN(len(models))]}
				stateMap(state, "resumePrompts")[sid] = object{"at": at + 12, "hash": fmt.Sprintf("%016x", rng.Uint64())}
			}
			if index%3 == 0 {
				stateMap(state, "continuedBy")[sid] = object{"at": at + 645, "by": "runner", "holder": fmt.Sprintf("%d-%x", 20000+rng.IntN(9000), rng.Uint64()&0xffffffffffff), "startedAt": at + 39}
				stateMap(state, "stopDay")[sid] = object{"at": at, "continues": float64(1 + rng.IntN(4)), "day": "2026-09-26"}
			}
			if index%4 == 0 {
				stateMap(state, "stopGuard")[sid] = object{"at": at, "forced": float64(rng.IntN(3)), "idle": float64(0), "lastOpen": float64(rng.IntN(7))}
			}
			if index%9 == 0 {
				stateMap(state, "checkpoints")[sid] = object{
					"at":       at,
					"cwd":      project,
					"path":     "/home/dev/.local/state/noctis/checkpoints/" + sid + ".md",
					"snapshot": "refs/noctis/checkpoints/" + sid[:8],
					"consumed": false,
					"reason":   fmt.Sprintf("weekly limit at %.1f%%", 90+rng.Float64()*9),
				}
			}
			if index%23 == 0 {
				stateMap(state, "waits")[sid] = object{
					"resumeAt":     at + 3*3600,
					"kind":         "batch",
					"queuedPrompt": prompts[rng.IntN(len(prompts))],
					"used":         float64(int(rng.Float64()*10000)) / 100,
					"context":      nil,
					"scheduled":    object{"method": "systemd", "at": at + 3*3600, "unit": "noctis-resume-" + sid[:8]},
				}
				stateMap(state, "tasks")[sid] = object{"at": at, "items": object{
					"t1": object{"at": at, "status": "open", "subject": "Retry failed uploads with \"backoff\""},
					"t2": object{"at": at + 60, "status": "done", "subject": "Ölçümleri kaydet → notes/ölçüm.md"},
				}}
				stateMap(state, "workflows")[sid] = []any{object{"at": at, "name": "big-run"}, object{"at": at + 300, "name": "audit-routes", "steps": []any{"scan", "fix", float64(3)}}}
				state["interruptedWaits"] = append(state["interruptedWaits"].([]any), object{"sid": sid, "at": at, "resumeAt": at + 3600, "why": "the runner was gone"})
			}
			if index%31 == 0 {
				stateMap(state, "routes")[sid] = object{"at": at, "denies": float64(0), "signal": "araştır"}
				stateMap(state, "routerLearned")["signal-"+sid[:4]] = object{"at": at, "misroutes": float64(2), "ok": float64(rng.IntN(9))}
			}
		}
		state["budgetDay"] = object{"day": "2026-09-26", "startUsed": 12.25, "weekResetsAt": now + 4*86400, "spent": []any{0.5, 1.75, float64(3), 0.0625}}
		state["modelSwitched"] = object{"from": "model-large-long", "to": "model-medium", "at": now, "because": "scoped model at 97%"}
		state["lastHookAt"] = now
		state["hookCapSeconds"] = float64(21600)
		state["disabledUntil"] = float64(0)
		encoded, err := json.Marshal(state)
		if err != nil {
			panic(err)
		}
		if len(encoded) >= size {
			return state
		}
	}
}

const benchStateSize = 85 * 1024

func BenchmarkJSONDecodeState(b *testing.B) {
	data, err := referenceJSON(realisticState(benchStateSize), false, false)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("fast", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := decodeJSON(data); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("encoding-json", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		b.ReportAllocs()
		for b.Loop() {
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkJSONEncodeState(b *testing.B) {
	state := realisticState(benchStateSize)
	for _, indent := range []bool{false, true} {
		name := "compact"
		if indent {
			name = "indented"
		}
		b.Run(name+"/fast", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, ok := encodeJSON(state, false, indent); !ok {
					b.Fatal("not handled")
				}
			}
		})
		b.Run(name+"/encoding-json", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := referenceJSON(state, false, indent); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
