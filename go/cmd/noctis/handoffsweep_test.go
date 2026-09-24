package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAStopFailureHandOffItsCopyNeverRemovedIsSweptAway(t *testing.T) {
	sandboxFiles(t)
	pending := handOffDir()
	ensureDir(pending)
	now := time.Now()
	aged := func(name string, age time.Duration) string {
		file := filepath.Join(pending, name)
		if err := os.WriteFile(file, []byte(`{"hook_event_name":"StopFailure"}`), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.Chtimes(file, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		return file
	}
	orphaned := aged("0a1b2c3d-4242.json", 2*time.Hour)
	torn := aged("0a1b2c3d-4243.json.4243.tmp", 10*time.Minute)
	inFlight := aged("0a1b2c3d-4244.json", 10*time.Minute)
	writing := aged("0a1b2c3d-4245.json.4245.tmp", time.Minute)

	sweepStaleLocks()

	for _, file := range []string{orphaned, torn} {
		if statSafe(file) != nil {
			t.Errorf("a hand-off its copy never removed was left behind: %s", filepath.Base(file))
		}
	}
	for _, file := range []string{inFlight, writing} {
		if statSafe(file) == nil {
			t.Errorf("a hand-off that may still be in use was swept away: %s", filepath.Base(file))
		}
	}
}
