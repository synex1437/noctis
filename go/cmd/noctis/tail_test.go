package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundleTailStartsAtALineBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")

	var whole strings.Builder
	var recordLen int
	for i := 0; i < 200; i++ {
		record, _ := json.Marshal(object{"n": fmt.Sprintf("%03d", i), "note": strings.Repeat("x", 40)})
		if recordLen == 0 {
			recordLen = len(record) + 1
		} else if len(record)+1 != recordLen {
			t.Fatalf("records must be a fixed width for this test; got %d and %d", recordLen, len(record)+1)
		}
		whole.Write(record)
		whole.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(whole.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	tail := tailOfFile(path, int64(10*recordLen+recordLen/2))
	if len(tail) == 0 {
		t.Fatal("tail was empty")
	}
	first := strings.SplitN(string(tail), "\n", 2)[0]
	if err := json.Unmarshal([]byte(first), &object{}); err != nil {
		t.Errorf("the bundle's first line is not a whole record: %q (%v)", first, err)
	}
}

func TestShortReadIsNotPaddedWithNULs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guard.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info := statSafe(path)
	if info == nil {
		t.Fatal("stat failed")
	}

	content, _, err := readTailBytes(path, info.Size()+4096, info.Size()+4096)
	if err != nil {
		t.Fatalf("readTailBytes: %v", err)
	}
	if strings.ContainsRune(string(content), 0) {
		t.Errorf("read was padded with NUL bytes: %q", content)
	}
	if string(content) != "one\ntwo\nthree\n" {
		t.Errorf("content = %q, want the file verbatim", content)
	}
}

func TestWholeFileReadIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, ok := tailLines(path, transcriptTailBytes)
	if !ok {
		t.Fatal("a readable file was reported unreadable")
	}
	if len(lines) < 2 || lines[0] != "a" || lines[1] != "b" {
		t.Errorf("lines = %q, want the file's own lines", lines)
	}

	if _, ok := tailLines(filepath.Join(dir, "absent.jsonl"), 1024); ok {
		t.Error("a missing file reported itself readable")
	}
}

func TestTailFileLinesCountsRealLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "errors.log")
	if err := os.WriteFile(path, []byte("\n\nfirst\n\nsecond\n\n\nthird\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := tailFileLines(path, 3)
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q — blank lines must not fill the count", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}
