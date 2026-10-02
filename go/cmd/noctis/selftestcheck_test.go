package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelftestEndsWithAVerdictAndFailsWhenACheckFailed(t *testing.T) {
	box := newCLIBox(t)

	run := box.run(t, "selftest")

	if !strings.Contains(run.stdout, "!!") {
		t.Fatalf("this account is not set up, yet selftest found nothing to report:\n%s", run)
	}
	if run.code != 1 || !strings.Contains(run.stdout, "need attention above") {
		t.Fatalf("selftest printed failures but gave no verdict or exited %d:\n%s", run.code, run)
	}
}

func TestSelftestSaysWhenNoDesktopNotificationCouldBeShown(t *testing.T) {
	box := newCLIBox(t)
	box.env["PATH"] = strings.Split(box.env["PATH"], string(os.PathListSeparator))[0]

	run := box.run(t, "selftest")

	if strings.Contains(run.stdout, "notification sent") {
		t.Fatalf("selftest reports a notification although no notifier could start:\n%s", run)
	}
	if !strings.Contains(run.stdout, "no desktop notification") {
		t.Fatalf("selftest does not say that no notification could be shown:\n%s", run)
	}
}

func TestTheSelftestProbeDoesNotCountAsAHookPulse(t *testing.T) {
	sandboxFiles(t)
	cfg := object{"thresholds": object{"session5h": float64(92), "weeklyAll": float64(89)}, "usage": object{}, "models": object{"primary": "opus"}}

	decide(cfg, readState(), object{"session_id": selftestSession, "cwd": files.guardDir}, nowSec(), decideOptions{noProbe: true})

	if at := numberOr(readState(), "lastHookAt", 0); at != 0 {
		t.Fatalf("the selftest's own probe was recorded as a hook pulse (lastHookAt %v), so the next selftest reports hooks that never ran", at)
	}

	decide(cfg, readState(), object{"session_id": "real", "cwd": files.guardDir}, nowSec(), decideOptions{noProbe: true})

	if at := numberOr(readState(), "lastHookAt", 0); at == 0 {
		t.Fatal("a real hook call no longer records its pulse")
	}
}

func selftestProbeLeavesNoTrace(t *testing.T, cfg object, cwd, scene string) {
	t.Helper()
	output := hookOutput(t, onPostToolBatch, object{"hook_event_name": "PostToolBatch", "session_id": selftestSession, "cwd": cwd}, cfg)
	if stoppedBy(output) {
		t.Errorf("%s the selftest's own probe was stopped: %v", scene, output)
	}
	if wait := pendingWait(selftestSession); wait != nil {
		t.Errorf("%s the selftest's own probe was paused: %v", scene, wait)
	}
	if _, err := os.Stat(filepath.Join(files.checkpoints, selftestSession+".md")); err == nil || getMap(getMap(readState(), "checkpoints"), selftestSession) != nil {
		t.Errorf("%s the selftest's own probe left a checkpoint behind (%v)", scene, getMap(getMap(readState(), "checkpoints"), selftestSession))
	}
	if journal, _ := os.ReadFile(files.decisions); strings.Contains(string(journal), `"sid":"`+selftestSession+`"`) {
		t.Errorf("%s the selftest's own probe left an entry in the decision journal:\n%s", scene, journal)
	}
}

func TestTheSelftestProbeIsNotPausedByAWindowPastItsPausePoint(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"wait": object{"maxInHookMinutes": float64(1)}}, 95, 40)

	selftestProbeLeavesNoTrace(t, cfg, project, "with the 5-hour window at 95%, past its pause point,")

	if output := hookOutput(t, onPostToolBatch, object{"hook_event_name": "PostToolBatch", "session_id": "real", "cwd": project}, cfg); pendingWait("real") == nil {
		t.Fatalf("a real session's batch at 95%% was not paused, so the window was not past its pause point: %v", output)
	}
}

func TestTheSelftestProbeDoesNotSwitchTheModelPastTheScopedThreshold(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"wait": object{"maxInHookMinutes": float64(1)}}, 20, 10)
	mustWriteJSON(files.settings, object{"model": "claude-fable-5-1"})
	now := float64(nowSec())
	writeFableBucket(builtinThresholds["weeklyFable"]+1, now, now+86400)

	selftestProbeLeavesNoTrace(t, cfg, project, "with the Fable bucket past its threshold")

	if model := settingsModel(); model != "claude-fable-5-1" {
		t.Fatalf("the selftest's own probe switched the default model to %q", model)
	}
	hookOutput(t, onPostToolBatch, object{"hook_event_name": "PostToolBatch", "session_id": "real", "cwd": project}, cfg)
	if model := settingsModel(); model == "claude-fable-5-1" {
		t.Fatal("a real Fable session's batch past the threshold did not switch the model, so the bucket was not past its threshold")
	}
}

func TestTheSelftestProbeIsNotPausedBlindNearTheCeilingWithoutData(t *testing.T) {
	recordingUsageEndpoint(t, "", "")
	now := nowSec()
	cfg, input, _ := blindAtTheCeiling(t, now, now+600, 3)

	if plan := decide(cfg, readState(), object{"session_id": selftestSession, "cwd": getString(input, "cwd")}, now, decideOptions{}).wait; plan != nil {
		t.Fatalf("3 points from the paid-credit ceiling without data the selftest's own probe was paused: %+v", plan)
	}
	if plan := decide(cfg, readState(), input, now, decideOptions{}).wait; plan == nil || plan.hit != "blind" {
		t.Fatalf("a real session 3 points from the paid-credit ceiling without data was not paused blind: %+v", plan)
	}
}

func TestNotifyGivesTheReasonWhenWindowsHasNoTrustedToastScript(t *testing.T) {
	sandboxFiles(t)
	previous := isWindows
	t.Cleanup(func() { isWindows = previous })
	isWindows = true
	files.pluginRoot, files.notifyScript = t.TempDir(), ""

	reason := notify(object{}, pluginName, "body")

	if !strings.Contains(reason, files.pluginRoot) || !strings.Contains(reason, "notify.ps1 is not run") {
		t.Fatalf("on Windows with no trusted notify.ps1 notify returned %q, so selftest reports a notification that was never shown", reason)
	}
}
