//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func scopedRoleConfig() object {
	config := releaseConfig()
	config["wait"] = object{"earlyResetPollMinutes": float64(5), "heartbeatGraceSeconds": float64(1)}
	config["resume"] = object{"mode": "headless", "prompt": "carry on"}
	config["models"] = object{"primary": "fable", "fallback": "opus", "effort": "max", "scopedPattern": "fable", "scopedLabel": "Fable"}
	config["roles"] = object{"code": object{"model": "fable", "effort": "max"}, "fallback": object{"model": "opus", "effort": "high"}}
	config["fable"] = object{"source": "oauth", "revertOnReset": true}
	return config
}

func scopedWindowAt(used float64) {
	now := float64(nowSec())
	mustWriteJSON(files.fable, object{"fetchedAt": now, "clockOffset": float64(0), "fable": object{"used": used, "resetsAt": now + 2*86400}})
}

func sessionRunsOn(sid, model, cwd string) {
	now := float64(nowSec())
	recordStatusline(object{"session_id": sid, "cwd": cwd, "model": object{"id": model}, "rate_limits": object{
		"five_hour": object{"used_percentage": float64(3), "resets_at": now + 18000},
		"seven_day": object{"used_percentage": float64(20), "resets_at": now + 3*86400},
	}}, nowSec(), true)
}

func relaunchLine(t *testing.T, calls, sid string) string {
	t.Helper()
	content, _ := os.ReadFile(calls)
	for _, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, "--resume "+sid+" ") {
			return line
		}
	}
	t.Fatalf("the runner did not relaunch %s (journal %v):\n%s", sid, journaledFor(sid), content)
	return ""
}

func TestARelaunchOverTheScopedCapRunsOnTheFallbackRole(t *testing.T) {
	calls := takeoverSandbox(t)
	mustWriteJSON(files.config, scopedRoleConfig())
	sid := "scoped-capped"
	parkForRelaunch(t, sid, "batch", "headless")
	sessionRunsOn(sid, "claude-fable-5-1", getString(waitOf(sid), "cwd"))
	scopedWindowAt(97)
	resumeWait(sid, "")
	line := relaunchLine(t, calls, sid)
	if !strings.Contains(line, "--model opus ") || !strings.Contains(line, "--effort high ") {
		t.Fatalf("a session paused on its 5-hour window was relaunched on Fable although Fable's weekly window is at 97%% (pause point 95%%): %s", line)
	}
	if got := settingsModel(); got != "opus" {
		t.Errorf("the relaunch switched the session to the fallback but left the default model on %q", got)
	}
}

func TestARelaunchAfterAScopedSwitchRunsAtTheFallbackRolesEffort(t *testing.T) {
	calls := takeoverSandbox(t)
	mustWriteJSON(files.config, scopedRoleConfig())
	sid := "scoped-switched"
	parkForRelaunch(t, sid, "batch", "headless")
	updateState(func(state object) {
		wait := getMap(getMap(state, "waits"), sid)
		wait["kind"], wait["window"], wait["label"], wait["modelOverride"] = "fable", "fable", "Fable", "opus"
	})
	sessionRunsOn(sid, "claude-fable-5-1", getString(waitOf(sid), "cwd"))
	scopedWindowAt(97)
	resumeWait(sid, "")
	line := relaunchLine(t, calls, sid)
	if !strings.Contains(line, "--model opus ") || !strings.Contains(line, "--effort high ") {
		t.Fatalf("the relaunch after a Fable switch should run the fallback role (opus at effort high): %s", line)
	}
}

func TestAWakeLeavesASessionOverTheScopedCapToItsRunner(t *testing.T) {
	if os.Getenv("NOCTIS_TEST_SCOPED_WAKE") != "" {
		cfg, project, _ := limitSandbox(t, object{"models": object{"primary": "fable", "fallback": "opus", "scopedPattern": "fable", "scopedLabel": "Fable"}}, 20, 40)
		sid := "scoped-wake"
		now := float64(nowSec())
		usage := readJSON(files.usage)
		usage["sessions"] = object{sid: object{"model": "claude-fable-5-1", "cwd": project, "transcript": filepath.Join(project, "transcript.jsonl"), "updatedAt": now, "context": nil}}
		mustWriteJSON(files.usage, usage)
		scopedWindowAt(97)
		record := object{"kind": "stopfailure", "window": "five_hour", "label": "5h", "used": float64(95), "until": now, "resumeAt": now, "startedAt": now, "cwd": project, "transcript": filepath.Join(project, "transcript.jsonl")}
		updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(record) })
		wakeSameSession(cfg, sid, record, now)
		if wait := pendingWait(sid); wait == nil || wait["wakeAttemptedAt"] != nil {
			t.Fatalf("the wait was not left to the runner: %v", wait)
		}
		return
	}
	scratch := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), "NOCTIS_TEST_SCOPED_WAKE=1", "TMPDIR="+scratch, "TMP="+scratch, "TEMP="+scratch)
	output, err := child.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("a session on Fable was woken in place while Fable's weekly window is at 97%%, instead of being left to the runner that relaunches it on the fallback: exit %d\n%s", code, output)
	}
}

func TestARelaunchUnderTheScopedCapKeepsTheCodeRolesEffort(t *testing.T) {
	for _, model := range []string{"claude-fable-5-1", "claude-opus-5-5"} {
		t.Run(model, func(t *testing.T) {
			calls := takeoverSandbox(t)
			mustWriteJSON(files.config, scopedRoleConfig())
			sid := "uncapped-" + model
			parkForRelaunch(t, sid, "batch", "headless")
			sessionRunsOn(sid, model, getString(waitOf(sid), "cwd"))
			scopedWindowAt(20)
			resumeWait(sid, "")
			line := relaunchLine(t, calls, sid)
			if !strings.Contains(line, "--model "+model+" ") || !strings.Contains(line, "--effort max ") {
				t.Fatalf("a relaunch on %s with Fable's weekly window at 20%% (pause point 95%%) is not on the fallback and should run at the code role's effort (max), not the fallback role's (high): %s", model, line)
			}
		})
	}
}

func TestARelaunchInObserveModeOnlyJournalsTheScopedSwitch(t *testing.T) {
	calls := takeoverSandbox(t)
	config := scopedRoleConfig()
	config["mode"] = "observe"
	mustWriteJSON(files.config, config)
	defer func(previous bool) { observing = previous }(observing)
	observing = true
	mustWriteJSON(files.settings, object{"model": "claude-fable-5-1", "env": object{"CLAUDE_CODE_EFFORT_LEVEL": "max"}})
	sid := "scoped-observed"
	parkForRelaunch(t, sid, "batch", "headless")
	sessionRunsOn(sid, "claude-fable-5-1", getString(waitOf(sid), "cwd"))
	scopedWindowAt(97)
	resumeWait(sid, "")
	line := relaunchLine(t, calls, sid)
	settings := readJSON(files.settings)
	if getString(settings, "model") != "claude-fable-5-1" || getString(getMap(settings, "env"), "CLAUDE_CODE_EFFORT_LEVEL") != "max" || getMap(readState(), "modelSwitched") != nil {
		t.Errorf("in observe mode the runner switched the default model and effort at Fable's cap: settings %v, switch record %v", settings, getMap(readState(), "modelSwitched"))
	}
	if journaled := journaledFor(sid); !slices.Contains(journaled, "would-switch-model") || slices.Contains(journaled, "switch-model") {
		t.Errorf("in observe mode the runner should journal would-switch-model and no switch-model: %v", journaled)
	}
	if !strings.Contains(line, "--model claude-fable-5-1 ") || !strings.Contains(line, "--effort max ") {
		t.Errorf("in observe mode the relaunch should stay on the session's own model at the code role's effort: %s", line)
	}
}
