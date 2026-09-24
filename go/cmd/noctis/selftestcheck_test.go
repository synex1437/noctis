package main

import (
	"os"
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
