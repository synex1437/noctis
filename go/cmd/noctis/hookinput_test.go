package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
)

// hookBatch is a PostToolBatch input whose first tool's output is output, as written.
func hookBatch(output string) []byte {
	return []byte(`{"session_id":"s1","hook_event_name":"PostToolBatch","tool_calls":[{"tool_name":"Read","tool_input":{"file_path":"/p/a.go"},"tool_use_id":"t1","tool_response":` + output + `},{"tool_name":"Bash","tool_input":{"command":"go test ./..."},"tool_use_id":"t2","tool_response":{"stdout":"ok","interrupted":false}}]}`)
}

// hookInputWant is what readHookInput must give back for data: what reading all of it and
// decoding it gave before, without the outputs of a batch's tools.
func hookInputWant(data []byte) (any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	dropToolOutputs(value)
	return value, nil
}

// checkHookInputBy fails t unless readHookInput, reading data a window of size bytes at a time
// from readers that give it out whole, a byte at a time, by halves and with the end of input,
// gives back what hookInputWant gives: the same value, or a refusal, with encoding/json's error
// when the input is shorter than the window.
func checkHookInputBy(t testing.TB, data []byte, size int) {
	t.Helper()
	want, wantErr := hookInputWant(data)
	readers := map[string]func(io.Reader) io.Reader{
		"whole":         func(r io.Reader) io.Reader { return r },
		"byte by byte":  iotest.OneByteReader,
		"by halves":     iotest.HalfReader,
		"with its end":  iotest.DataErrReader,
		"by two halves": func(r io.Reader) io.Reader { return iotest.HalfReader(iotest.DataErrReader(r)) },
	}
	for name, reader := range readers {
		input := bytes.NewReader(data)
		got, readErr, err := readHookInputBy(reader(input), make([]byte, size))
		switch {
		case input.Len() != 0:
			t.Fatalf("reading %s %s through %d bytes left %d bytes of it unread; the host's write of them may fail", shown(string(data)), name, size, input.Len())
		case readErr != nil:
			t.Fatalf("reading %s %s through %d bytes: read error %v", shown(string(data)), name, size, readErr)
		case (err == nil) != (wantErr == nil):
			t.Fatalf("reading %s %s through %d bytes: error %v, before it was %v", shown(string(data)), name, size, err, wantErr)
		case err != nil && len(data) < size && err.Error() != wantErr.Error():
			t.Fatalf("reading %s %s through %d bytes: error %q, before it was %q", shown(string(data)), name, size, err, wantErr)
		case !sameJSON(got, want):
			t.Fatalf("reading %s %s through %d bytes gave %s, want %s", shown(string(data)), name, size, shown(got), shown(want))
		}
	}
}

// checkHookInput is checkHookInputBy with windows of every size that matters for data: a few
// bytes, a word and a bit, its length and a byte either side, and the real one.
func checkHookInput(t testing.TB, data []byte) {
	t.Helper()
	for _, size := range []int{1, 2, 3, 7, 8, 9, 64, len(data) - 1, len(data), len(data) + 1, hookInputWindow} {
		if size > 0 {
			checkHookInputBy(t, data, size)
		}
	}
}

// hookInputCases are hook inputs that probe where outputFilter drops a member and where it must
// not, how it joins what it keeps, and what it must refuse inside what it drops.
var hookInputCases = []string{
	// Outputs dropped, the rest kept as it was.
	string(hookBatch(`"text of a file"`)), string(hookBatch(`{"stdout":"a","stderr":"","interrupted":false,"n":[1,2.5e3,-0,true,null]}`)),
	string(hookBatch(`[]`)), string(hookBatch(`{}`)), string(hookBatch(`null`)), string(hookBatch(`-12.5e-3`)), string(hookBatch(`0`)),
	string(hookBatch(`true`)), string(hookBatch(`""`)), string(hookBatch(`"\"\\\/\b\f\n\r\t\u00e9\ud83d\ude00"`)),
	string(hookBatch("\"caf\xe9\xff\xfe\"")), string(hookBatch(`[[[[{"a":[{"b":{}}]}]]]]`)),
	`{"tool_calls":[{"tool_response":1}]}`, `{"tool_calls":[{"tool_response":"x","a":1}]}`,
	`{"tool_calls":[{"a":1,"tool_response":"x","b":2}]}`, `{"tool_calls":[{"a":1,"tool_response":"x"}]}`,
	`{"tool_calls":[{"a":1,"tool_response":"x","tool_response":[1],"b":2,"tool_response":{}}]}`,
	`{"tool_calls":[{"tool_response":1,"tool_response":2}]}`, `{"tool_calls":[{"tool_response":12,"a":3}]}`,
	`{"tool_calls":[{},{"tool_response":1},{"a":{"tool_response":2}},{"tool_response":3,"a":4}]}`,
	` { "tool_calls" : [ { "a" : 1 , "tool_response" : [ 1 , 2 ] , "b" : 2 } , { "tool_response" : "x" } ] } `,
	"{\n\t\"tool_calls\":\r\n[\n\t{\"tool_response\"\t:\n\"x\"\r,\n\"a\":1\n}\n]\n}\n",
	"{\"tool_calls\":[{\"a\":1 , \"tool_response\":7 ,\"b\":true , \"tool_response\":null}]}",
	// Names written with escapes.
	`{"tool\u005fcalls":[{"tool\u005fresponse":1,"a":2}]}`,
	`{"\u0074\u006f\u006f\u006c\u005f\u0063\u0061\u006c\u006c\u0073":[{"\u0074\u006f\u006f\u006c\u005f\u0072\u0065\u0073\u0070\u006f\u006e\u0073\u0065":1,"a":2}]}`,
	`{"tool_calls":[{"\u0074\u006f\u006f\u006c\u005f\u0072\u0065\u0073\u0070\u006f\u006e\u0073\u0065\u0020":1,"a":2}]}`,
	`{"tool_calls":[{"tool_respons\u0065":1,"tool_response\u0000":2,"tool_response ":3,"Tool_response":4,"tool_respons":5,"\u0074ool_response":6}]}`,
	`{"tool_calls":[{"tool_response\\":1,"tool_\/response":2,"\/tool_response":3,"tool_resp\u006Fnse":4}]}`,
	// Long entries and names given twice.
	`{"tool_calls":[{"k0":0,"k1":1,"k2":2,"k3":3,"k4":4,"k5":5,"k6":6,"k7":7,"tool_response":"x","k8":8,"k0":"again","tool_response":{"y":1},"k9":9}]}`,
	`{"tool_calls":[{"tool_response":1,"a":1}],"tool_calls":[{"b":2,"tool_response":2}]}`,
	`{"tool_calls":[{"tool_response":1}],"tool_calls":"x"}`, `{"tool_calls":"x","tool_calls":[{"tool_response":1,"c":3}]}`,
	// What is not the output of an entry stays.
	`{"tool_response":{"content":"an agent's report"},"tool_name":"Agent"}`, `{"tool_calls":[{"a":1}],"tool_response":5}`,
	`{"tool_calls":{"tool_response":1}}`, `{"tool_calls":"tool_response"}`, `{"tool_calls":null}`, `{"tool_calls":[]}`,
	`{"tool_calls":[[{"tool_response":1}]]}`, `{"tool_calls":[{"tool_input":{"tool_response":1}}]}`,
	`{"x":{"tool_calls":[{"tool_response":1}]}}`, `[{"tool_calls":[{"tool_response":1}]}]`, `[[{"tool_response":1}]]`,
	`{"tool_callsx":[{"tool_response":1}]}`, `{"Tool_calls":[{"tool_response":1}]}`, `{"tool_calls ":[{"tool_response":1}]}`,
	`{"tool_calls":[1,"tool_response",null,{"tool_response":2},[{"tool_response":3}],true]}`,
	`{"tool_calls":[{"tool_response":1}],"x":[{"tool_response":2}]}`, `{"a":[{"tool_response":1}],"tool_calls":[{"tool_response":2}]}`,
	`{"tool_calls":[{"a":[{"tool_response":1}],"tool_response":{"tool_calls":[{"tool_response":2}]}}]}`,
	`{"tool_calls":{"a":[1],"b":{"tool_response":1}}}`, `{"tool_calls":{"a":[[]],"b":[{"tool_response":1}]},"c":{"d":{"tool_response":2}}}`,
	// What is wrong inside an output is refused, as it was.
	string(hookBatch("\"\x01\"")), string(hookBatch(`"\u12G4"`)), string(hookBatch(`"\q"`)), string(hookBatch(`1e400`)),
	string(hookBatch(`[1,]`)), string(hookBatch(`{"a"}`)), string(hookBatch(`tru`)), string(hookBatch(`"abc`)), string(hookBatch(`01`)),
	string(hookBatch(`-`)), string(hookBatch(`1.`)), string(hookBatch(``)), string(hookBatch(`]`)), string(hookBatch(`{"a":1}}`)),
	string(hookBatch(`"a"x`)), string(hookBatch(`"\u00`)), string(hookBatch(`{"a":-1e400}`)), string(hookBatch(`[1e999,2]`)),
	string(hookBatch("\"a\x1fb\"")), string(hookBatch("\"a\nb\"")), string(hookBatch(`nul`)), string(hookBatch(`falsex`)),
	string(hookBatch(`"\u00zz"`)), string(hookBatch(`"\u00g0"`)), string(hookBatch(`"\u00G0"`)), string(hookBatch(`"\u00e`)),
	`{"tool_calls":[{"tool_response":1}]`, `{"tool_calls":[{"tool_response":1}]}}`, `{"tool_calls":[{"tool_response":1]}`,
	`{"tool_calls":[{"tool_response":1,}]}`, `{"tool_calls":[{,"tool_response":1}]}`, `{"tool_calls":[{"tool_response"}]}`,
	`{"tool_calls":[{"tool_response":}]}`, `{"tool_calls":[{"tool_response" 1}]}`, `{"tool_calls":[{"tool_response":1 "a":2}]}`,
	`{"tool_calls":[{"tool_response":1}] "x":1}`, `{"tool_calls":[{"tool_response":1}]} x`, `{"tool_calls":[{"tool_response":1}]}{}`,
	`{"tool_calls":[{"tool_response":1}]}` + "\x00", "\xef\xbb\xbf" + `{"tool_calls":[]}`,
}

func TestReadHookInputLeavesOutTheOutputs(t *testing.T) {
	output := `"` + strings.Repeat(`a line of the file, with \"quotes\", \u00e9 and a tab\t\n`, 3*hookInputWindow/56) + `"`
	for _, data := range [][]byte{hookBatch(`"short"`), hookBatch(output)} {
		value, readErr, err := readHookInput(bytes.NewReader(data))
		if readErr != nil || err != nil {
			t.Fatalf("reading a batch of %d bytes: %v, %v", len(data), readErr, err)
		}
		want := object{"session_id": "s1", "hook_event_name": "PostToolBatch", "tool_calls": []any{
			object{"tool_name": "Read", "tool_input": object{"file_path": "/p/a.go"}, "tool_use_id": "t1"},
			object{"tool_name": "Bash", "tool_input": object{"command": "go test ./..."}, "tool_use_id": "t2"},
		}}
		if !sameJSON(value, any(want)) {
			t.Fatalf("reading a batch of %d bytes gave %s, want %s", len(data), shown(value), shown(want))
		}
	}
	// The output of the tool a PostToolUse hook follows is the hook's own business; it stays.
	report := `{"hook_event_name":"PostToolUse","tool_name":"Agent","tool_response":{"content":[{"type":"text","text":"` + strings.Repeat("done. ", 2*hookInputWindow/6) + `"}]}}`
	value, readErr, err := readHookInput(strings.NewReader(report))
	if readErr != nil || err != nil {
		t.Fatal(readErr, err)
	}
	if want, _ := hookInputWant([]byte(report)); !sameJSON(value, want) || getMap(value.(object), "tool_response") == nil {
		t.Fatalf("a subagent's report lost its tool_response: %s", shown(value))
	}
}

func TestReadHookInputMatchesTheWholeDecode(t *testing.T) {
	for _, input := range hookInputCases {
		checkHookInput(t, []byte(input))
	}
	for _, input := range jsonDecodeCases {
		for _, data := range []string{
			input, string(hookBatch(input)), `{"tool_response":` + input + `}`, `{"tool_calls":` + input + `}`,
			`{"tool_calls":[` + input + `]}`, `{"tool_calls":[[` + input + `]]}`,
			`{"tool_calls":[{"tool_name":"Read","tool_input":` + input + `,"tool_response":1}]}`,
			`{"tool_calls":[{"tool_response":1,"tool_input":` + input + `}]}`,
		} {
			checkHookInput(t, []byte(data))
		}
	}
	// Only space, of JSON's kind and of the kind only bytes.TrimSpace takes, longer than a window.
	for _, space := range []string{" \t\r\n", "\v\f\xc2\xa0\xe2\x80\xa8 ", " \xc2", "\xc2\xa0x"} {
		checkHookInput(t, []byte(strings.Repeat(space, 40)))
		checkHookInputBy(t, []byte(strings.Repeat(space, hookInputWindow)), hookInputWindow)
	}
}

// An escape, a quote, a control byte or a byte past ASCII at every place in the words a string is
// read by, in a name and in an output.
func TestReadHookInputFindsWhatEndsTextAnywhere(t *testing.T) {
	for _, mark := range []string{`\n`, `\"`, `\\`, `\u00e9`, `\ud83d\ude00`, "\x01", "\x1f", "\xff", "\xe2\x82\xac", "\x7f", `"`, `\`, `\x`} {
		for before := range 18 {
			text := strings.Repeat("a", before) + mark + strings.Repeat("b", 17-before)
			checkHookInput(t, hookBatch(`"`+text+`"`))
			checkHookInput(t, []byte(`{"tool_calls":[{"`+text+`":1,"tool_response":"`+text+`"}],"`+text+`":2}`))
		}
	}
}

// Past the first window an input that is not JSON is refused with where it stops being JSON.
func TestReadHookInputSaysWhereTheInputStopsBeingJSON(t *testing.T) {
	long := strings.Repeat("x", 2*hookInputWindow)
	inOutput := string(hookBatch(`"` + long + `\q"`))
	cutShort := `{"tool_calls":[{"tool_response":"` + long + `"}]`
	after := `{"a":"` + long + `"} y`
	for _, input := range []struct {
		data string
		at   int
	}{{inOutput, strings.Index(inOutput, `\q`) + 1}, {cutShort, len(cutShort)}, {after, len(after) - 1}} {
		for _, size := range []int{1, 7, hookInputWindow} {
			_, _, err := readHookInputBy(strings.NewReader(input.data), make([]byte, size))
			if want := fmt.Sprintf("not JSON at byte %d", input.at); err == nil || err.Error() != want {
				t.Fatalf("reading %s through %d bytes: error %v, want %q", shown(input.data), size, err, want)
			}
		}
	}
}

func TestReadHookInputDepth(t *testing.T) {
	// The top map, the list of entries and an entry hold the output: three levels.
	for _, levels := range []int{jsonMaxDepth - 4, jsonMaxDepth - 3, jsonMaxDepth - 2, jsonMaxDepth + 5} {
		checkHookInputBy(t, hookBatch(strings.Repeat("[", levels)+strings.Repeat("]", levels)), 7)
		checkHookInputBy(t, hookBatch(strings.Repeat(`{"a":`, levels)+"1"+strings.Repeat("}", levels)), hookInputWindow)
		checkHookInputBy(t, hookBatch(strings.Repeat("[", levels)+strings.Repeat("]", levels)), hookInputWindow)
	}
}

// The state noctis keeps, the values that need the most care in it, as an output and as a value
// that stays, long enough to go past a window.
func TestReadHookInputOnTheRealisticState(t *testing.T) {
	for _, state := range []object{realisticState(benchStateSize), oddState()} {
		encoded, err := referenceJSON(state, false, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, data := range [][]byte{
			hookBatch(string(encoded)),
			[]byte(`{"tool_calls":[{"tool_input":` + string(encoded) + `,"tool_response":` + string(encoded) + `,"x":1}]}`),
		} {
			for _, size := range []int{1, 13, 4096, hookInputWindow} {
				checkHookInputBy(t, data, size)
			}
		}
	}
}

// jsonStringStop finds what a byte-at-a-time scan finds, wherever the stop is in a word.
func TestJSONStringStop(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	alphabet := []byte("abcxyz 09~\"\\\x00\x01\x1f\x20\x7f\x80\xa0\xc2\xe2\xff")
	for range 20000 {
		piece := make([]byte, rng.IntN(40))
		for index := range piece {
			piece[index] = 'a'
			if rng.IntN(6) == 0 {
				piece[index] = alphabet[rng.IntN(len(alphabet))]
			}
		}
		at := rng.IntN(len(piece) + 1)
		want := at
		for want < len(piece) && piece[want] >= ' ' && piece[want] != '"' && piece[want] != '\\' {
			want++
		}
		if got := jsonStringStop(piece, at); got != want {
			t.Fatalf("jsonStringStop(%q, %d) = %d, want %d", piece, at, got, want)
		}
	}
}

func TestReadHookInputDoesNotHoldTheOutputs(t *testing.T) {
	output := `"` + strings.Repeat(`a line of the file, with \"quotes\", \u00e9 and a tab\t\n`, 7<<20/56) + `"`
	data := hookBatch(output)
	allocated := func(read func(io.Reader) (any, error, error)) uint64 {
		least := uint64(math.MaxUint64)
		for range 3 {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			value, readErr, err := read(bytes.NewReader(data))
			runtime.ReadMemStats(&after)
			if readErr != nil || err != nil || value == nil {
				t.Fatalf("reading a batch of %d bytes: %v, %v", len(data), readErr, err)
			}
			least = min(least, after.TotalAlloc-before.TotalAlloc)
		}
		return least
	}
	if streamed := allocated(readHookInput); streamed > 1<<20 {
		t.Fatalf("readHookInput allocated %d bytes for a batch with %d bytes of output; it holds only what it keeps", streamed, len(output))
	}
	if whole := allocated(readWholeJSON); whole < uint64(len(output))/2 {
		t.Fatalf("reading the whole batch allocated only %d bytes for %d bytes of output; the measure no longer sees the output held", whole, len(output))
	}
}

func TestReadStdinJSONLeavesOutTheOutputsUnlessDebugging(t *testing.T) {
	sandboxFiles(t)
	saved, savedLoaded, savedCache := os.Stdin, stdinLoaded, stdinCache
	t.Cleanup(func() { os.Stdin, stdinLoaded, stdinCache = saved, savedLoaded, savedCache })
	for _, output := range []string{`"short"`, `"` + strings.Repeat("x", 3*hookInputWindow) + `"`} {
		for _, debugging := range []bool{false, true} {
			if debugging {
				t.Setenv("NOCTIS_DEBUG_HOOKS", "1")
			} else {
				t.Setenv("NOCTIS_DEBUG_HOOKS", "")
			}
			file := filepath.Join(t.TempDir(), "stdin.json")
			if err := os.WriteFile(file, hookBatch(output), 0o600); err != nil {
				t.Fatal(err)
			}
			opened, err := os.Open(file)
			if err != nil {
				t.Fatal(err)
			}
			os.Stdin, stdinLoaded, stdinCache = opened, false, nil
			got := readStdinJSON()
			opened.Close()
			calls := getList(got, "tool_calls")
			if len(calls) != 2 {
				t.Fatalf("readStdinJSON of a batch gave %s", shown(got))
			}
			first, _ := calls[0].(object)
			second, _ := calls[1].(object)
			_, kept := first["tool_response"]
			if kept != debugging || first["tool_name"] != "Read" || getMap(second, "tool_input")["command"] != "go test ./..." {
				t.Fatalf("readStdinJSON of a batch with NOCTIS_DEBUG_HOOKS %q gave %s", os.Getenv("NOCTIS_DEBUG_HOOKS"), shown(got))
			}
		}
	}
}

func FuzzJSONFastHookInput(f *testing.F) {
	for _, input := range append(append([]string{}, hookInputCases...), jsonDecodeCases...) {
		f.Add([]byte(input), uint16(len(input)/2))
	}
	f.Fuzz(func(t *testing.T, data []byte, window uint16) {
		checkHookInputBy(t, data, 1+int(window)%(len(data)+2))
		batch := hookBatch(string(data))
		checkHookInputBy(t, batch, 1+int(window)%(len(batch)+2))
	})
}

func BenchmarkReadHookInput(b *testing.B) {
	data := hookBatch(`"` + strings.Repeat(`a line of the file, with \"quotes\", \u00e9 and a tab\t\n`, 1<<20/56) + `"`)
	for _, reader := range []struct {
		name string
		read func(io.Reader) (any, error, error)
	}{{"streamed", readHookInput}, {"whole", readWholeJSON}} {
		b.Run(reader.name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				if _, readErr, err := reader.read(bytes.NewReader(data)); readErr != nil || err != nil {
					b.Fatal(readErr, err)
				}
			}
		})
	}
}
