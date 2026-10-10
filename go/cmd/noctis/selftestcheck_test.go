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

func TestTheSelftestTimesTheSubagentHooksAndLeavesNoTraceOfThem(t *testing.T) {
	previousFiles, previousArgs, previousLocale := files, args, locale
	t.Cleanup(func() { files, args, locale = previousFiles, previousArgs, previousLocale })
	account, scratch := t.TempDir(), t.TempDir()
	for key, value := range map[string]string{testAsNoctis: "1", "TMPDIR": scratch, "TMP": scratch, "TEMP": scratch, "NOCTIS_PLUGIN_ROOT": "", "NOCTIS_USAGE_URL": "http://127.0.0.1:9/usage",
		"NOCTIS_UPDATE_URL": "off", "NOCTIS_NO_WATCHER": "1", "NOCTIS_NO_TASKS": "1", "NOCTIS_NO_SCHEDULE": "1", "NOCTIS_LANG": "en"} {
		t.Setenv(key, value)
	}
	args, locale = parseArgs([]string{"selftest", "--account", account}), "en"
	initPaths()
	ensureDir(files.guardDir)
	mustWriteJSON(files.config, object{"alarm": object{"enabled": false}, "update": object{"check": false}})
	writeUsage(10, 30, 0)
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}

	ok, line := probeSubagentHooks(executable)

	if !ok || !strings.HasPrefix(line, "subagent hooks: Agent gate ") || !strings.HasSuffix(line, "transcript of 500 tool calls") {
		t.Fatalf("the subagent hook probe reported %q (ok %v)", line, ok)
	}
	state := readState()
	spawns := getMap(state, spawnStateKey)
	if getMap(getMap(spawns, "sessions"), selftestSession) != nil || len(getList(spawns, "recent")) > 0 || getMap(getMap(spawns, "stopped"), selftestSession) != nil {
		t.Errorf("the probe left subagent counts behind: %v", spawns)
	}
	for mark := range getMap(state, "notified") {
		t.Errorf("the probe left the notice mark %q", mark)
	}
	if spent := getMap(getMap(getMap(readJSON(files.usage), spendKey), "sessions"), selftestSession); spent != nil {
		t.Errorf("the probe left its spend in usage.json: %v", spent)
	}
	if records, _ := os.ReadDir(filepath.Join(files.guardDir, "subagents")); len(records) > 0 {
		t.Errorf("the probe left the subagent record %s", records[0].Name())
	}
	if journal, _ := os.ReadFile(files.decisions); len(journal) > 0 {
		t.Errorf("the probe wrote to the decision journal:\n%s", journal)
	}
	if left, _ := os.ReadDir(scratch); len(left) > 0 {
		t.Errorf("the probe left %s in the temp directory", left[0].Name())
	}
}

func TestForgettingTheSubagentProbeTakesBackOnlyWhatItsHooksWrote(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 10, 30)
	dir, agent := t.TempDir(), "selftestprobe1"
	transcript := filepath.Join(dir, selftestSession, "subagents", "agent-"+agent+".jsonl")
	if _, err := writeProbeTranscript(transcript, 20); err != nil {
		t.Fatal(err)
	}
	probe := func(event, sid string) object {
		return agentHookInput(event, sid, project, object{"agent_id": agent, "agent_type": "general-purpose", "transcript_path": filepath.Join(dir, selftestSession+".jsonl"), "agent_transcript_path": transcript})
	}
	for _, sid := range []string{selftestSession, "real"} {
		hookOutput(t, onSubagentStart, probe("SubagentStart", sid), cfg)
		hookOutput(t, onSubagentStop, probe("SubagentStop", sid), cfg)
	}
	sessions := func() object { return getMap(getMap(readJSON(files.usage), spendKey), "sessions") }
	if numberOr(getMap(sessions(), selftestSession), "opened", 0) != 1 || numberOr(getMap(sessions(), selftestSession), "sub", 0) <= 0 || statSafe(growthRecordPath(probe("SubagentStop", selftestSession))) == nil {
		t.Fatalf("the probe's hooks recorded nothing to take back: %v", sessions())
	}

	forgetSubagentProbe(dir, agent)

	if getMap(sessions(), selftestSession) != nil || statSafe(growthRecordPath(probe("SubagentStop", selftestSession))) != nil || statSafe(dir) != nil {
		t.Fatalf("the probe's spend %v, its subagent record or its transcript stayed behind", getMap(sessions(), selftestSession))
	}
	if real := getMap(sessions(), "real"); numberOr(real, "opened", 0) != 1 || numberOr(real, "sub", 0) <= 0 || statSafe(growthRecordPath(probe("SubagentStop", "real"))) == nil {
		t.Fatalf("forgetting the probe took a real session's subagent spend with it: %v", real)
	}
}

func TestTheSelftestProbeLeavesTheBurnNoticeToARealSession(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 10, 30)
	if err := os.Remove(files.usage); err != nil {
		t.Fatal(err)
	}
	now := nowSec()
	weekReadings(t, now, float64(now+5*86400), 3, 30, 2, 33, 1, 36, 0, 39)

	selftestProbeLeavesNoTrace(t, cfg, project, "with the week burning too fast")

	for mark := range getMap(readState(), "notified") {
		t.Fatalf("the selftest's own probe took the notice mark %q", mark)
	}
	if message := getString(hookOutput(t, onUserPromptSubmit, promptInput("burn", project, "go on with the parser"), cfg), "systemMessage"); !strings.Contains(message, "refuses new subagents") {
		t.Fatalf("after the selftest's probe a real prompt in a week burning too fast showed %q", message)
	}
}

func TestTheSelftestTranscriptReadsAsTheSubagentItStandsFor(t *testing.T) {
	path := filepath.Join(t.TempDir(), selftestSession, "subagents", "agent-probe.jsonl")

	size, err := writeProbeTranscript(path, selftestProbeCalls)

	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != int64(size) {
		t.Fatalf("the probe transcript is %v on disk, %d bytes reported", info, size)
	}
	spend := measureAgent(object{}, path)
	if spend.calls != selftestProbeCalls || spend.tools != selftestProbeCalls || spend.start != 51605 || spend.peak != 101505 {
		t.Fatalf("the probe transcript measures %d calls, %d tool calls, start %v and peak %v; want 500, 500, 51605 and 101505", spend.calls, spend.tools, spend.start, spend.peak)
	}
	if context := lastContext(path); context != spend.peak {
		t.Fatalf("the batch probe reads %v tokens of context from the transcript's tail; want %v", context, spend.peak)
	}
	if limits, _ := agentGrowthLimits(object{"agent_type": "general-purpose"}, object{}); reached(spend.peak, limits.context) || reached(1, limits.calls) {
		t.Fatalf("the probe's subagent would be told to wrap up under the default budget %+v", limits)
	}
}
