//go:build !windows

package main

import (
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func holdForTest(t *testing.T, handle *os.File) {
	t.Helper()
	if err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("the test could not hold %s: %v", handle.Name(), err)
	}
}

func letGoForTest(handle *os.File) {
	_ = os.Remove(handle.Name())
	_ = handle.Close()
}

func TestALockWhoseRunningHolderHungForTwoMinutesIsStillTakenOver(t *testing.T) {
	sandboxFiles(t)
	if err := os.WriteFile(files.stateLock, []byte(strconv.Itoa(os.Getppid())), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-3 * time.Minute)
	if err := os.Chtimes(files.stateLock, old, old); err != nil {
		t.Fatal(err)
	}
	handle, err := os.OpenFile(files.stateLock, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	holdForTest(t, handle)

	ran := false
	began := time.Now()
	if !withFileLock(files.stateLock, func() { ran = true }) || !ran {
		t.Fatal("a lock whose running holder has held it for three minutes was not taken over")
	}
	if waited := time.Since(began); waited > 2*time.Second {
		t.Fatalf("taking over the hung holder's lock took %s", waited)
	}
}
