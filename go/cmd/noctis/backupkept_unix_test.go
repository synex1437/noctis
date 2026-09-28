//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// s1FullDiskEnv hands the sandbox folder a test prepared to the child s1RunOnAFullDisk starts.
const s1FullDiskEnv = "NOCTIS_TEST_FULL_DISK_DIR"

// s1FullDiskBytes is the largest file the child can write: RLIMIT_FSIZE stands in for a disk that
// fills up while a file is being written, so a longer write stops there with an error.
const s1FullDiskBytes = 4096

// s1FullDiskChild reports whether this process is the child s1RunOnAFullDisk started. In it the
// files point at the parent's sandbox and no file can grow past s1FullDiskBytes until the test ends.
func s1FullDiskChild(t *testing.T) bool {
	t.Helper()
	dir := os.Getenv(s1FullDiskEnv)
	if dir == "" {
		return false
	}
	sandboxFilesIn(t, dir)
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	previous := limit.Cur
	limit.Cur = s1FullDiskBytes
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		limit.Cur = previous
		_ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit)
	})
	return true
}

// s1RunOnAFullDisk runs the calling test again in a child process that works in dir on a "full
// disk" (see s1FullDiskChild), and waits for it.
func s1RunOnAFullDisk(t *testing.T, dir string) {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), s1FullDiskEnv+"="+dir)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("the child writing on a full disk failed: %v\n%s", err, output)
	}
}

func TestAStateBackupCutShortByAFullDiskLeavesTheOlderBackupWhole(t *testing.T) {
	if s1FullDiskChild(t) {
		updateState(func(state object) { stateMap(state, "notified")["on-a-full-disk"] = float64(nowSec()) })
		return
	}
	dir := sandboxFiles(t)
	stamp := float64(nowSec())
	updateState(func(state object) {
		for index := 0; index < 300; index++ {
			stateMap(state, "notified")[fmt.Sprintf("kept-%03d", index)] = stamp
		}
	})
	updateState(func(state object) { stateMap(state, "notified")["later"] = stamp })
	backup := cliRead(t, files.stateBackup)
	if len(backup) <= s1FullDiskBytes {
		t.Fatalf("the backup holds %d bytes, too few for the full disk to cut its next write short", len(backup))
	}

	s1RunOnAFullDisk(t, dir)

	if after, _ := os.ReadFile(files.stateBackup); !bytes.Equal(after, backup) {
		t.Fatalf("a backup write the full disk cut short left %d bytes of a new backup where the %d-byte older one was", len(after), len(backup))
	}
	if err := os.WriteFile(files.state, []byte(`{"notified":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if notified := getMap(readState(), "notified"); notified["kept-299"] != stamp {
		t.Fatal("state.json broke after the full disk, and the state was not restored from the backup")
	}
}

func TestAUsageBackupCutShortByAFullDiskLeavesTheOlderBackupWhole(t *testing.T) {
	now := nowSec()
	reading := func(sid string, used float64) {
		statusReadingFrom(sid, now, used, float64(now+3600), 10, float64(now+3*86400))
	}
	if s1FullDiskChild(t) {
		reading("on-a-full-disk", 30)
		return
	}
	dir := sandboxFiles(t)
	sessions := object{}
	for index := 0; index < 60; index++ {
		sessions[fmt.Sprintf("kept-%02d", index)] = object{"model": "claude-opus-5-5", "cwd": dir,
			"transcript": filepath.Join(dir, "transcript.jsonl"), "updatedAt": float64(now)}
	}
	mustWriteJSON(files.usage, object{"version": "2.1.300", "sessions": sessions, "history": object{}})
	reading("later", 20)
	backup := cliRead(t, files.usageBackup)
	if len(backup) <= s1FullDiskBytes {
		t.Fatalf("the backup holds %d bytes, too few for the full disk to cut its next write short", len(backup))
	}

	s1RunOnAFullDisk(t, dir)

	if after, _ := os.ReadFile(files.usageBackup); !bytes.Equal(after, backup) {
		t.Fatalf("a backup write the full disk cut short left %d bytes of a new backup where the %d-byte older one was", len(after), len(backup))
	}
	if err := os.WriteFile(files.usage, []byte(`{"sessions":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if getMap(getMap(readUsageRepaired(files.guardDir), "sessions"), "kept-59") == nil {
		t.Fatal("usage.json broke after the full disk, and the readings were not restored from the backup")
	}
}
