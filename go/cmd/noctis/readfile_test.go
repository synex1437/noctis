package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAReaderDoesNotBlockTheWriteThatReplacesTheFile(t *testing.T) {
	dir := t.TempDir()
	guard := filepath.Join(dir, "state.json")
	original := []byte(`{"waits":{"a":{"resumeAt":1}}}`)
	if err := os.WriteFile(guard, original, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	reader, err := openShared(guard)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()

	staging := filepath.Join(dir, "state.json.tmp")
	if err := os.WriteFile(staging, []byte(`{"waits":{}}`), 0o600); err != nil {
		t.Fatalf("write staging: %v", err)
	}
	if err := os.Rename(staging, guard); err != nil {
		t.Fatalf("a writer could not replace state.json while a reader held it open: %v", err)
	}

	held, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("the open handle stopped reading once its file was replaced: %v", err)
	}
	if !bytes.Equal(held, original) {
		t.Fatalf("the open handle read %q; it opened the file when it held %q", held, original)
	}
}

func TestAReaderDoesNotBlockTheDeleteThatRetiresTheFile(t *testing.T) {
	dir := t.TempDir()
	guard := filepath.Join(dir, "state.json")
	original := []byte(`{"waits":{}}`)
	if err := os.WriteFile(guard, original, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	reader, err := openShared(guard)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()

	if err := os.Remove(guard); err != nil {
		t.Fatalf("a writer could not delete state.json while a reader held it open: %v", err)
	}
	held, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("the open handle stopped reading once its file was deleted: %v", err)
	}
	if !bytes.Equal(held, original) {
		t.Fatalf("the open handle read %q; it opened the file when it held %q", held, original)
	}
	if _, err := readFileShared(guard); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a fresh read of the deleted file returned %v; callers branch on os.ErrNotExist", err)
	}
}

func TestReplacingTheStateFileNeverWaitsForAReader(t *testing.T) {
	dir := t.TempDir()
	guard := filepath.Join(dir, "state.json")
	payload := bytes.Repeat([]byte(`{"session":"x"},`), 15000)
	if err := os.WriteFile(guard, payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	var reads, readRefusals atomic.Int64
	for worker := 0; worker < 4; worker++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := readFileShared(guard); err != nil {
					readRefusals.Add(1)
					return
				}
				reads.Add(1)
			}
		}()
	}

	staging := filepath.Join(dir, "state.json.staging")
	var refused error
	replacements := 0
	for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline); {
		if err := os.WriteFile(staging, payload, 0o600); err != nil {
			refused = err
			break
		}
		if err := os.Rename(staging, guard); err != nil {
			refused = err
			break
		}
		replacements++
	}
	close(stop)
	readers.Wait()

	if refused != nil {
		t.Fatalf("a reader blocked the %dth replacement of state.json: %v", replacements+1, refused)
	}
	if got := readRefusals.Load(); got != 0 {
		t.Fatalf("%d reads were refused while the file was being replaced", got)
	}
	if replacements < 5 || reads.Load() < 5 {
		t.Fatalf("%d replacements against %d reads: the two never overlapped, so nothing was proven", replacements, reads.Load())
	}
}
