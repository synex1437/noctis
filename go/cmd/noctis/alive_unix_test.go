//go:build !windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func startSleeper(t *testing.T) (*exec.Cmd, chan struct{}) {
	t.Helper()
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = child.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = child.Process.Kill()
		<-done
	})
	return child, done
}

func TestAPidBeyondTheKernelRangeIsNeverTreatedAsAProcess(t *testing.T) {
	child, done := startSleeper(t)
	wrapped := 1<<32 + child.Process.Pid
	if processAlive(wrapped) {
		t.Fatalf("processAlive(%d) is true: the kernel read it as pid %d", wrapped, child.Process.Pid)
	}
	if processAlive(1<<32 - 1) {
		t.Fatal("processAlive(4294967295) is true: the kernel read it as kill(-1, 0)")
	}
	if name := processName(wrapped); name != "" {
		t.Fatalf("processName(%d) = %q, want no name for an impossible pid", wrapped, name)
	}
	if err := terminateProcess(wrapped); err == nil {
		t.Fatalf("terminateProcess(%d) reported success for an impossible pid", wrapped)
	}
	select {
	case <-done:
		t.Fatalf("terminateProcess(%d) killed pid %d", wrapped, child.Process.Pid)
	case <-time.After(300 * time.Millisecond):
	}
	if !processAlive(child.Process.Pid) {
		t.Fatalf("processAlive(%d) is false for a running child", child.Process.Pid)
	}
}

func TestAnExitedChildNobodyReapedIsNotAlive(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("no /proc on this system")
	}
	child := exec.Command("sh", "-c", "exit 0")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Wait() }()
	pid := child.Process.Pid
	statFile := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	deadline := time.Now().Add(5 * time.Second)
	for {
		stat, err := os.ReadFile(statFile)
		if err != nil {
			t.Fatalf("reading %s: %v", statFile, err)
		}
		if end := bytes.LastIndexByte(stat, ')'); end >= 0 && end+2 < len(stat) && stat[end+2] == 'Z' {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d never became a zombie: %s", pid, stat)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("processAlive(%d) is true for a zombie", pid)
	}
}

func TestAProcessReapedWhileItIsCheckedCountsAsExited(t *testing.T) {
	child := exec.Command("sh", "-c", "exit 0")
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
	if pid := child.Process.Pid; !processExited(pid) {
		t.Fatalf("processExited(%d) is false for a process that exited and was reaped, so processAlive, which asks it after kill(pid, 0) found the process, calls a zombie reaped between the two looks alive", pid)
	}
}
