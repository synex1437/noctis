package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestALockCanBeReleasedWhileAnotherProcessReadsIt(t *testing.T) {
	sandboxFiles(t)
	ensureDir(files.guardDir)
	owner := []byte("4321")
	if err := os.WriteFile(files.stateLock, owner, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	waiting, err := openShared(files.stateLock)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer waiting.Close()

	if err := os.Remove(files.stateLock); err != nil {
		t.Fatalf("a process reading state.lock stopped its owner from releasing it: %v", err)
	}
	held, err := io.ReadAll(waiting)
	if err != nil {
		t.Fatalf("the open handle stopped reading once the lock was released: %v", err)
	}
	if !bytes.Equal(held, owner) {
		t.Fatalf("the open handle read %q; it opened the lock when it held %q", held, owner)
	}
	if _, err := readFileShared(files.stateLock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a fresh read of the released lock returned %v; withFileLock branches on os.ErrNotExist", err)
	}
}

func TestARenameReplacesTheFileAReaderIsHolding(t *testing.T) {
	sandboxFiles(t)
	ensureDir(files.guardDir)
	original := []byte(`{"waits":{"before":{}}}`)
	if err := os.WriteFile(files.state, original, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	reader, err := openShared(files.state)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()

	replacement := []byte(`{"waits":{"after":{}}}`)
	staging := files.state + ".staging"
	if err := os.WriteFile(staging, replacement, 0o600); err != nil {
		t.Fatalf("write staging: %v", err)
	}
	if err := renameAtomic(staging, files.state); err != nil {
		t.Fatalf("a reader holding state.json open blocked the rename that replaces it: %v", err)
	}

	held, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("the open handle stopped reading once its file was replaced: %v", err)
	}
	if !bytes.Equal(held, original) {
		t.Fatalf("the open handle read %q; it opened the file when it held %q, so the replacement "+
			"overwrote the file the reader was holding instead of swapping a new one in", held, original)
	}
	if landed, readErr := readFileShared(files.state); readErr != nil || !bytes.Equal(landed, replacement) {
		t.Fatalf("a fresh read got %q (%v), want the replacement %q", landed, readErr, replacement)
	}
}

func TestAWriteNeverRewritesTheFileUnderAReader(t *testing.T) {
	sandboxFiles(t)
	if err := writeJSONAtomic(files.state, object{"hookCapSeconds": float64(11)}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	original, err := readFileShared(files.state)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	reader, err := openShared(files.state)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()

	if err := writeJSONAtomic(files.state, object{"hookCapSeconds": float64(22)}); err != nil {
		t.Fatalf("a reader holding state.json open made the write fail outright: %v", err)
	}
	if numberOr(readState(), "hookCapSeconds", 0) != 22 {
		t.Fatalf("the write did not land while a reader held the file open")
	}

	held, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("the open handle stopped reading once the file was written: %v", err)
	}
	if !bytes.Equal(held, original) {
		t.Fatalf("the open handle read %q instead of the %q it opened: writeJSONAtomic gave up on the "+
			"rename and rewrote the file in place, which is how a reader mid-read gets half a file", held, original)
	}
}

func TestReadingAndWritingTheStateFileDoNotRefuseEachOther(t *testing.T) {
	sandboxFiles(t)
	document := object{"waits": object{}, "notified": object{}}
	for index := 0; index < 400; index++ {
		stateMap(document, "notified")[itoa(index)] = float64(index)
	}
	if err := writeJSONAtomic(files.state, document); err != nil {
		t.Fatalf("first write: %v", err)
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	var reads, readRefusals atomic.Int64
	for worker := 0; worker < 3; worker++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := readFileShared(files.state); err != nil {
					readRefusals.Add(1)
					return
				}
				reads.Add(1)
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	writes := 0
	var refused error
	for ; writes < 20; writes++ {
		if err := writeJSONAtomic(files.state, document); err != nil {
			refused = err
			break
		}
	}
	close(stop)
	readers.Wait()

	if refused != nil {
		t.Fatalf("write %d of 20 failed while the file was being read: %v", writes+1, refused)
	}
	if got := readRefusals.Load(); got != 0 {
		t.Fatalf("%d reads were refused while the file was being written", got)
	}
	if reads.Load() < 5 {
		t.Fatalf("only %d reads ran against %d writes: the two never overlapped, so nothing was proven", reads.Load(), writes)
	}
}
