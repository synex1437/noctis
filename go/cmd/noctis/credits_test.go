package main

import (
	"os"
	"path/filepath"
	"testing"
)

func creditsConfig() object {
	return object{
		"thresholds": object{"session5h": float64(92), "weeklyAll": float64(89), "weeklyFable": float64(95)},
		"credits":    object{"allowPaid": false, "ceiling": float64(100), "fanOutHeadroom": float64(25)},
		"budget":     object{},
	}
}

func TestWorkAtTheCeilingStopsEvenWhenTheThresholdsAreNonsense(t *testing.T) {
	cfg := creditsConfig()
	cfg["thresholds"] = object{"session5h": float64(-40), "weeklyAll": float64(899), "weeklyFable": float64(0)}
	result := evaluate(cfg, usageFrom(100, 100, float64(nowSec()+3600)), "claude-fable-5-1", 0, false)
	if result.wait == nil {
		t.Fatal("a config whose thresholds are all out of range left the session running at 100%: past that point the account pays for every token")
	}
	if result.wait.hit != "ceiling" {
		t.Fatalf("the stop was attributed to %q, not the credit ceiling", result.wait.hit)
	}
}

func TestBelowTheCeilingNothingChanges(t *testing.T) {
	if result := evaluate(creditsConfig(), usageFrom(50, 40, float64(nowSec()+3600)), "claude-fable-5-1", 0, false); result.wait != nil {
		t.Fatalf("an ordinary session was stopped: %+v", *result.wait)
	}
}

func TestTheCeilingCanBeLiftedOnPurpose(t *testing.T) {
	cfg := creditsConfig()
	cfg["credits"] = object{"allowPaid": true}
	cfg["thresholds"] = object{"session5h": float64(899), "weeklyAll": float64(899), "weeklyFable": float64(899)}
	if result := evaluate(cfg, usageFrom(100, 100, float64(nowSec()+3600)), "claude-fable-5-1", 0, false); result.wait != nil {
		t.Fatal("credits.allowPaid was set, so the user has said the overflow is theirs to pay; the ceiling should step aside")
	}
}

func TestPausingTheGuardDoesNotOpenTheDoorToPaidCredits(t *testing.T) {
	sandboxFiles(t)
	cfg := creditsConfig()
	now := nowSec()
	state := object{"disabledUntil": float64(now + 3600)}
	if !guardPaused(cfg, state, now) {
		t.Fatal("a plain pause with no usage data should hold")
	}
	mustWriteJSON(files.usage, object{
		"updatedAt": float64(now),
		"five_hour": object{"used": float64(100), "resetsAt": float64(now + 3600)},
		"seven_day": object{"used": float64(100), "resetsAt": float64(now + 86400)},
	})
	if guardPaused(cfg, state, now) {
		t.Fatal("/noctis:pause kept the guard asleep at 100%: that is exactly the overnight bill the ceiling exists to prevent")
	}
}

func TestAWorkflowNeedsRoomBeforeItFansOut(t *testing.T) {
	cfg := creditsConfig()
	cfg["workflow"] = object{"gate": true}
	crowded := decision{usage: usageFrom(20, 70, float64(nowSec()+3600))}
	if reason := gateWorkflowLaunch(cfg, crowded, nil); reason == "" {
		t.Fatal("a workflow was allowed with 19 points left on the weekly window; hundreds of agents can burn that between two hook calls")
	}
	roomy := decision{usage: usageFrom(20, 30, float64(nowSec()+3600))}
	if reason := gateWorkflowLaunch(cfg, roomy, nil); reason != "" {
		t.Fatalf("a workflow with 59 points of room was refused: %s", reason)
	}
}

func TestHeadroomIsMeasuredAgainstTheNearestWall(t *testing.T) {
	cfg := creditsConfig()
	room, label := headroomLeft(cfg, usageFrom(88, 30, float64(nowSec()+3600)))
	if int(room) != 4 {
		t.Fatalf("headroom came out as %v; 92 - 88 is 4", room)
	}
	if label != windowLabel("five_hour") {
		t.Fatalf("the nearest wall was reported as %q", label)
	}
}

func TestTheFallbackModelBringsItsOwnEffort(t *testing.T) {
	sandboxFiles(t)
	mustWriteJSON(files.settings, object{"model": "fable", "env": object{"CLAUDE_CODE_EFFORT_LEVEL": "max"}})
	cfg := object{
		"models":     object{"primary": "fable", "fallback": "opus"},
		"roles":      object{"fallback": object{"model": "opus", "effort": "high"}},
		"fable":      object{"revertOnReset": true},
		"thresholds": object{"weeklyFable": float64(95)},
	}
	now := nowSec()
	persistModelSwitch(cfg, float64(now+3600), now)
	settings := readJSON(files.settings)
	if got := getString(getMap(settings, "env"), "CLAUDE_CODE_EFFORT_LEVEL"); got != "high" {
		t.Fatalf("the fallback role asked for effort high, settings.json says %q", got)
	}
	if getMap(readState(), "modelSwitched") == nil {
		t.Fatal("the switch was not recorded in state")
	}
	usage := usageView{hasAny: true, fable: &window{used: 1, resetsAt: float64(now + 10)}}
	if note := maybeRevertDefaultModel(cfg, readState(), usage, now+20); note == "" {
		t.Fatal("the scoped window cleared but the default model was not reverted")
	}
	settings = readJSON(files.settings)
	if got := getString(getMap(settings, "env"), "CLAUDE_CODE_EFFORT_LEVEL"); got != "max" {
		t.Fatalf("the revert should have put effort max back, settings.json says %q", got)
	}
}

func TestAnEffortForTheAgentToolIsNotStored(t *testing.T) {
	for _, role := range []string{"planning", "explore"} {
		spec, err := parseRoleFlag(role, "opus:high")
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if getString(spec, "effort") != "" {
			t.Errorf("%s stored an effort the Agent tool cannot carry: %v", role, spec)
		}
	}
	spec, err := parseRoleFlag("code", "fable:max")
	if err != nil || getString(spec, "effort") != "max" {
		t.Fatalf("the code role must keep its effort: %v %v", spec, err)
	}
}

func TestEveryProfileOnlyPromisesEffortsItCanApply(t *testing.T) {
	for name, profile := range roleProfiles {
		for role := range effortlessRoles {
			if effort := getString(getMap(profile, role), "effort"); effort != "" {
				t.Errorf("profile %q gives %s an effort (%s) that nothing applies", name, role, effort)
			}
		}
	}
	defaults := readJSON(filepath.Join(repoRoot(), "config.default.json"))
	for role := range effortlessRoles {
		if effort := getString(getMap(section(defaults, "roles"), role), "effort"); effort != "" {
			t.Errorf("config.default.json gives %s an effort (%s) that nothing applies", role, effort)
		}
	}
}

func repoRoot() string {
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		if statSafe(filepath.Join(dir, "config.default.json")) != nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return "."
}

func TestAFailedWriteDoesNotLeaveItsTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(target, object{"a": float64(1)}); err == nil {
		t.Fatal("writing a JSON file over a non-empty directory should fail")
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(left) > 0 {
		t.Fatalf("a write that failed left %v behind; those accumulate in the user's guard dir", left)
	}
}

func TestNobodyIsAskedAQuestionDevNullCannotAnswer(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip(err)
	}
	defer devNull.Close()
	saved := os.Stdin
	os.Stdin = devNull
	t.Cleanup(func() { os.Stdin = saved })
	if stdinIsTerminal() {
		t.Fatal("stdin on /dev/null is a character device but nobody is behind it; setup would print a menu into the void")
	}
}

func TestAPlainInstallKeepsItsProfileName(t *testing.T) {
	defaults := readJSON(filepath.Join(repoRoot(), "config.default.json"))
	config := cloneObject(defaults)
	derived := derivedRoles(config, section(defaults, "roles"))
	if got := getString(derived, "profile"); got != "noctis" {
		t.Fatalf("a setup with no flags reported the profile as %q; the shipped defaults are the noctis profile", got)
	}
	if got := getString(getMap(derived, "fallback"), "effort"); got != "max" {
		t.Fatalf("the fallback effort was dropped on the way (%q); it is the one effort the scoped switch applies", got)
	}
	for role := range effortlessRoles {
		if got := getString(getMap(derived, role), "effort"); got != "" {
			t.Errorf("%s came back with effort %q, which nothing applies", role, got)
		}
	}
}
