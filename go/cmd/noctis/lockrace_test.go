package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func init() {
	if dir := os.Getenv("NOCTIS_TEST_LOCK_DIR"); dir != "" {
		os.Exit(lockRaceChild(dir, os.Getenv("NOCTIS_TEST_LOCK_ROLE"), os.Getenv("NOCTIS_TEST_LOCK_KEY")))
	}
}

func lockRaceChild(dir, role, key string) int {
	files.guardDir, files.configDir = dir, dir
	files.state, files.stateBackup, files.stateLock = filepath.Join(dir, "state.json"), filepath.Join(dir, "state.json.bak"), filepath.Join(dir, "state.lock")
	files.usageLock, files.fableLock = filepath.Join(dir, "usage.lock"), filepath.Join(dir, "fable.lock")
	files.log, files.errors = filepath.Join(dir, "guard.log"), filepath.Join(dir, "errors.log")
	files.checkpoints, files.launches = filepath.Join(dir, "checkpoints"), filepath.Join(dir, "launches")
	_, _ = io.Copy(io.Discard, os.Stdin)
	if role == "sweep" {
		sweepStaleLocks()
		return 0
	}
	updateState(func(state object) {
		stateMap(state, "modelOverrides")[key] = object{"model": "opus", "at": float64(nowSec())}
	})
	return 0
}

func plantDeadOwnersLock(t *testing.T, lock string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(lock, []byte("2147483646"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-age)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestReleasingALockLeavesASuccessorsLockInPlace(t *testing.T) {
	if isWindows {
		t.Skip("Windows refuses to delete a lock file while its holder keeps it open, so no successor can take its place")
	}
	sandboxFiles(t)
	successor := func(lock string) {
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lock, []byte("2147483645"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	withFileLock(files.stateLock, func() { successor(files.stateLock) })
	release, acquired := tryFileLock(files.fableLock)
	if !acquired {
		t.Fatal("the fable lock was not acquired")
	}
	successor(files.fableLock)
	release()

	for _, lock := range []string{files.stateLock, files.fableLock} {
		if content, err := os.ReadFile(lock); err != nil || string(content) != "2147483645" {
			t.Errorf("releasing %s removed the lock another process had taken in the meantime: %q %v", filepath.Base(lock), content, err)
		}
	}
}

func TestAStaleLookingLockThatIsStillHeldIsNeitherSweptNorTakenOver(t *testing.T) {
	for _, age := range []time.Duration{time.Minute, 3 * time.Minute} {
		t.Run(age.String()+" old", func(t *testing.T) {
			sandboxFiles(t)
			plantDeadOwnersLock(t, files.stateLock, age)
			handle, err := os.OpenFile(files.stateLock, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			holdForTest(t, handle)

			sweepStaleLocks()
			if statSafe(files.stateLock) == nil {
				handle.Close()
				t.Fatal("the sweep removed a lock whose holder still holds it")
			}
			ran := make(chan struct{})
			done := make(chan bool, 1)
			go func() { done <- withFileLock(files.stateLock, func() { close(ran) }) }()
			select {
			case <-ran:
				handle.Close()
				<-done
				t.Fatal("a writer took over a lock whose holder still holds it")
			case <-time.After(time.Second):
			}
			letGoForTest(handle)

			select {
			case <-ran:
			case <-time.After(5 * time.Second):
				t.Fatal("the writer did not get the lock after its holder let it go")
			}
			if !<-done {
				t.Fatal("the writer reported that it could not take the lock")
			}
		})
	}
}

func TestAStaleLockIsTakenOverByOneWriterAtATime(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const writers, sweepers = 8, 4
	for _, age := range []time.Duration{time.Minute, 3 * time.Minute} {
		for round := 0; round < 20; round++ {
			dir := t.TempDir()
			plantDeadOwnersLock(t, filepath.Join(dir, "state.lock"), age)
			var children []*exec.Cmd
			var gates []io.WriteCloser
			for index := 0; index < writers+sweepers; index++ {
				role := "write"
				if index >= writers {
					role = "sweep"
				}
				child := exec.Command(self)
				child.Env = append(os.Environ(), "NOCTIS_TEST_LOCK_DIR="+dir, "NOCTIS_TEST_LOCK_ROLE="+role, "NOCTIS_TEST_LOCK_KEY=w"+strconv.Itoa(index), "NOCTIS_NO_WATCHER=1")
				gate, err := child.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := child.Start(); err != nil {
					t.Fatal(err)
				}
				children, gates = append(children, child), append(gates, gate)
			}
			time.Sleep(300 * time.Millisecond)
			for _, gate := range gates {
				gate.Close()
			}
			for _, child := range children {
				if err := child.Wait(); err != nil {
					t.Fatalf("%s old lock, round %d: a child failed: %v", age, round, err)
				}
			}
			kept := sortedKeys(getMap(readJSON(filepath.Join(dir, "state.json")), "modelOverrides"))
			if len(kept) != writers {
				t.Fatalf("%s old lock, round %d: %d of %d writes kept (%v): two writers held state.lock at once", age, round, len(kept), writers, kept)
			}
		}
	}
}
