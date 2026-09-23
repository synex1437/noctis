//go:build windows

package main

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func pidsNamed(t *testing.T, name string) []int {
	t.Helper()
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(snapshot)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	pids := []int{}
	for err = syscall.Process32First(snapshot, &entry); err == nil; err = syscall.Process32Next(snapshot, &entry) {
		if strings.EqualFold(syscall.UTF16ToString(entry.ExeFile[:]), name) {
			pids = append(pids, int(entry.ProcessID))
		}
	}
	return pids
}

func TestAProtectedSystemProcessCountsAsAlive(t *testing.T) {
	pids := pidsNamed(t, "csrss.exe")
	if len(pids) == 0 {
		t.Skip("no csrss.exe on this system")
	}
	for _, pid := range pids {
		if !processAlive(pid) {
			t.Fatalf("processAlive(%d) is false for the running csrss.exe, a protected process", pid)
		}
	}
}

func TestAPidBeyondTheDwordRangeIsNeverTreatedAsAProcess(t *testing.T) {
	wrapped := 1<<32 + os.Getpid()
	if processAlive(wrapped) {
		t.Fatalf("processAlive(%d) is true: OpenProcess read it as pid %d", wrapped, os.Getpid())
	}
	if name := processName(wrapped); name != "" {
		t.Fatalf("processName(%d) = %q, want no name for an impossible pid", wrapped, name)
	}
	if err := terminateProcess(wrapped); err == nil {
		t.Fatalf("terminateProcess(%d) reported success for an impossible pid", wrapped)
	}
}
