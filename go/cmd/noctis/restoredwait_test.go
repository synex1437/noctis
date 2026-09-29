package main

import (
	"os"
	"os/exec"
	"testing"
)

// storeWaitEnv hands the sandbox folder a test prepared to the child that stores a wait in it.
const storeWaitEnv = "NOCTIS_TEST_STORE_WAIT_DIR"

func TestAYoungWaitWithoutARunnerIsLeftOnlyToAProcessThatCanStillRecordOne(t *testing.T) {
	dir := t.TempDir()
	now := nowSec()
	young := func(storedBy any) object {
		record := parkedWait(dir, float64(now+600), nil)
		record["startedAt"] = float64(now)
		if storedBy != nil {
			record["storedBy"] = storedBy
		}
		return record
	}
	if reason := strandedBecause("gone", young(float64(deadPid(t))), now, false); reason != "no runner was scheduled" {
		t.Errorf("a young wait with no runner whose process is gone is not stranded (%q), so it waits out the grace for nobody", reason)
	}
	if reason := strandedBecause("storing", young(float64(os.Getpid())), now, false); reason != "" {
		t.Errorf("a young wait whose process may still record its runner was called stranded: %s", reason)
	}
	if reason := strandedBecause("older", young(nil), now, false); reason != "" {
		t.Errorf("a young wait stored before noctis noted its process was called stranded: %s", reason)
	}
}

func TestAWaitRestoredFromTheBackupWithoutItsRunnerGetsOneAtTheNextRepair(t *testing.T) {
	if dir := os.Getenv(storeWaitEnv); dir != "" {
		// The child stores the wait and then records its runner, in two writes as a hook does, and exits.
		sandboxFilesIn(t, dir)
		cfg := loadConfig()
		now := float64(nowSec())
		record := parkedWait(dir, now+600, nil)
		record["kind"], record["startedAt"] = "stopfailure", now
		if !registerWait("restored", record, cfg) {
			t.Fatal("the wait was not stored")
		}
		scheduleRunner(cfg, "restored", now+600)
		return
	}
	dir, _ := strandedSandbox(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), storeWaitEnv+"="+dir)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("the child storing the wait failed: %v\n%s", err, output)
	}
	if getMap(getMap(getMap(readState(), "waits"), "restored"), "scheduled") == nil {
		t.Fatal("the child did not record the wait's runner")
	}

	// state.json breaks, and the backup holds it as it was before the write that recorded the runner.
	if err := os.WriteFile(files.state, []byte(`{"waits": `), 0o600); err != nil {
		t.Fatal(err)
	}
	restored := getMap(getMap(readState(), "waits"), "restored")
	if restored == nil || getMap(restored, "scheduled") != nil {
		t.Fatalf("the restore this test is about did not bring the wait back without its runner: %v", restored)
	}

	repairOrphanWaits()
	scheduled := getMap(getMap(getMap(readState(), "waits"), "restored"), "scheduled")
	if getString(scheduled, "method") != "manual" || numberOr(scheduled, "at", 0) != numberOr(restored, "resumeAt", -1) {
		t.Fatalf("the wait restored without its runner, whose hook is gone, was left with %v; want a runner re-armed at its resumeAt now, not after the %d-second grace", scheduled, schedulingGraceSeconds)
	}
	if entries := journaledCount("restored", "reschedule"); entries != 1 {
		t.Fatalf("the repair was journaled %d time(s) for noctis why, want once", entries)
	}
}
