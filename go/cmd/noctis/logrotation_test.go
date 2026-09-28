package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// s5LogWriters stands in for the processes that log at the same moment: each appends a line to a
// log that has just grown past logMaxBytes, so each of them finds it due for rotation.
const s5LogWriters = 32

// s5Rounds is how many times the writers meet such a log.
const s5Rounds = 100

// s5LogPastTheLimit writes a log just past logMaxBytes that the round can be told apart by.
func s5LogPastTheLimit(t *testing.T, file string, round int) []byte {
	t.Helper()
	history := []byte(fmt.Sprintf("s5 history of round %d\n", round))
	history = append(history, bytes.Repeat([]byte("."), logMaxBytes)...)
	history = append(history, '\n')
	cliWrite(t, file, history)
	return history
}

// s5AppendAllAtOnce has every writer append its line to file at the same moment, and returns the
// lines.
func s5AppendAllAtOnce(t *testing.T, file string, round int) []string {
	t.Helper()
	lines := make([]string, s5LogWriters)
	failures := make([]error, s5LogWriters)
	start := make(chan struct{})
	var done sync.WaitGroup
	for writer := range lines {
		lines[writer] = fmt.Sprintf("s5 round %d writer %d\n", round, writer)
		done.Add(1)
		go func(writer int) {
			defer done.Done()
			<-start
			failures[writer] = appendRotating(file, lines[writer])
		}(writer)
	}
	close(start)
	done.Wait()
	for writer, err := range failures {
		if err != nil {
			t.Fatalf("round %d: writer %d could not append: %v", round, writer, err)
		}
	}
	return lines
}

func TestLogsRotatedByManyWritersAtOnceKeepTheirHistory(t *testing.T) {
	base := sandboxFiles(t)
	for round := 0; round < s5Rounds; round++ {
		dir := filepath.Join(base, fmt.Sprintf("round-%d", round))
		file := filepath.Join(dir, "guard.log")
		history := s5LogPastTheLimit(t, file, round)

		lines := s5AppendAllAtOnce(t, file, round)

		rotated, _ := os.ReadFile(file + ".1")
		current, _ := os.ReadFile(file)
		var appended []byte
		switch {
		case bytes.HasPrefix(rotated, history):
			appended = append(append(appended, rotated[len(history):]...), current...)
		case bytes.HasPrefix(current, history):
			appended = append(append(appended, rotated...), current[len(history):]...)
		default:
			t.Fatalf("round %d: the log's %d bytes of history were lost when %d writers rotated it at once (guard.log.1 now holds %d bytes: %.120q)",
				round, len(history), s5LogWriters, len(rotated), rotated)
		}
		for _, line := range lines {
			if count := bytes.Count(appended, []byte(line)); count != 1 {
				t.Fatalf("round %d: %q is in the log %d times, not once", round, line, count)
			}
		}
		if left, _ := filepath.Glob(filepath.Join(dir, "*.lock")); len(left) != 0 {
			t.Fatalf("round %d: the rotation left %v behind", round, left)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
}

func TestARotateLockLeftByAProcessThatDiedIsSwept(t *testing.T) {
	dir := sandboxFiles(t)
	lock := filepath.Join(dir, rotateLockName)
	plantDeadOwnersLock(t, lock, time.Minute)

	sweepStaleLocks()

	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("the housekeeping sweep left a rotate.lock whose holder died: %v", err)
	}
}
