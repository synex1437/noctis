package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// cloudSandbox is a Claude Code cloud session with no usage data: no status
// line reports usage there and no token reaches the usage endpoint.
func cloudSandbox(t *testing.T, dir string, overrides object) (object, string) {
	t.Helper()
	sandboxFilesIn(t, dir)
	files.pluginRoot = dir
	files.credentials = filepath.Join(dir, ".credentials.json")
	mustWriteJSON(files.credentials, object{})
	t.Setenv("CLAUDE_CODE_REMOTE", "true")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	user := object{"alarm": object{"enabled": false}, "update": object{"check": false}}
	for key, value := range overrides {
		user[key] = value
	}
	mustWriteJSON(files.config, user)
	project := filepath.Join(dir, "project")
	ensureDir(project)
	return loadConfig(), project
}

func rateLimitStop(sid, cwd string) object {
	return agentHookInput("StopFailure", sid, cwd, object{"error": "rate_limit", "last_assistant_message": "API Error: Rate limit reached"})
}

func TestACloudSessionWaitsInTheHookAndWakesItselfInPlace(t *testing.T) {
	if dir := os.Getenv("NOCTIS_TEST_CLOUD_WAKE"); dir != "" {
		cfg, project := cloudSandbox(t, dir, object{"wait": object{"retryMinutes": []any{0.02}}})
		onStopFailure(rateLimitStop("cloud-wake", project), cfg)
		return
	}
	dir, scratch := t.TempDir(), t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), "NOCTIS_TEST_CLOUD_WAKE="+dir, "TMPDIR="+scratch, "TMP="+scratch, "TEMP="+scratch)
	output, err := child.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if code != 2 || !strings.Contains(string(output), "[noctis] Retry 1 after the session stopped on rate_limit") {
		t.Fatalf("a rate limit in a cloud session with no usage data did not wake the session in place: exit %d\n%s", code, output)
	}
	sandboxFilesIn(t, dir)
	scheduled := getMap(pendingWait("cloud-wake"), "scheduled")
	if getString(scheduled, "method") != "cloud" || scheduled["pid"] != nil {
		t.Errorf("a cloud session's wait was handed to a runner: %v", scheduled)
	}
	if actions := journaledFor("cloud-wake"); !slices.Contains(actions, "cloud-wait") {
		t.Errorf("the wait in the hook is not journaled: %v", actions)
	}
}

func TestACloudSessionStartsNoRunnerWatcherOrHandOff(t *testing.T) {
	cfg, _ := cloudSandbox(t, t.TempDir(), object{"wait": object{"earlyResetPollMinutes": float64(5)}})
	t.Setenv("NOCTIS_NO_SCHEDULE", "")
	t.Setenv("NOCTIS_NO_WATCHER", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli")
	at := float64(nowSec() + 600)
	updateState(func(state object) {
		stateMap(state, "waits")["cloud-runner"] = object{"resumeAt": at, "kind": "stopfailure"}
	})
	if scheduled := scheduleRunner(cfg, "cloud-runner", at); getString(scheduled, "method") != "cloud" || scheduled["pid"] != nil {
		t.Errorf("a cloud session spawned a runner to relaunch it: %v", scheduled)
	}
	if pid := startResetWatcher(cfg, "cloud-runner", at); pid != 0 {
		t.Errorf("a cloud session spawned a reset watcher (pid %d)", pid)
	}
	if handsStopFailuresOff() {
		t.Error("a cloud session hands its StopFailure to a detached copy")
	}
}

func TestACloudRateLimitIsRetriedUntilWakeMaxMinutesHavePassed(t *testing.T) {
	cfg, project := cloudSandbox(t, t.TempDir(), object{"wake": object{"sameSession": false, "maxMinutes": float64(330)}})
	sid := "cloud-budget"
	failAfter := func(retries, minutesAgo float64) object {
		t.Helper()
		now := float64(nowSec())
		updateState(func(state object) {
			delete(stateMap(state, "waits"), sid)
			stateMap(state, "failureRetries")[sid] = object{"retries": retries, "lastAt": now - 5, "firstAt": now - minutesAgo*60}
		})
		onStopFailure(rateLimitStop(sid, project), cfg)
		return pendingWait(sid)
	}
	if wait := failAfter(float64(stopFailureMaxAttempts+1), 200); wait == nil {
		t.Fatalf("a cloud rate limit was given up after %d retries, 200 of 330 minutes in", stopFailureMaxAttempts+1)
	} else if delay := numberOr(wait, "resumeAt", 0) - float64(nowSec()); delay < retryDelaySeconds(cfg, stopFailureMaxAttempts+2)-2 {
		t.Errorf("the retry came after %v s, want the last step of wait.retryMinutes", delay)
	}
	if wait := failAfter(3, 300); wait == nil {
		t.Fatal("a cloud rate limit 300 of 330 minutes in was not retried")
	} else if delay := numberOr(wait, "resumeAt", 0) - float64(nowSec()); delay > 30*60+2 {
		t.Errorf("the last wait runs %v s past wake.maxMinutes", delay-30*60)
	}
	if wait := failAfter(3, 329.5); wait != nil {
		t.Fatalf("a cloud rate limit was retried past wake.maxMinutes: %v", wait)
	}
	if actions := journaledFor(sid); !slices.Contains(actions, "retry-giveup") || !slices.Contains(actions, "cloud-no-wake") {
		t.Errorf("the cloud retries and the give-up are not journaled: %v", actions)
	}
}

func TestACloudSessionDoesNotHoldTheHookForAWallPastWakeMaxMinutes(t *testing.T) {
	cfg, project := cloudSandbox(t, t.TempDir(), object{"wake": object{"maxMinutes": float64(60)}})
	writeUsage(40, 99, 0)
	done := make(chan struct{})
	go func() {
		defer close(done)
		onStopFailure(agentHookInput("StopFailure", "cloud-week", project, object{"error": "rate_limit", "last_assistant_message": "You've hit your weekly limit · resets Mon 9am"}), cfg)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("a cloud session holds the hook for a weekly reset three days away, past wake.maxMinutes (60 min)")
	}
	wait := pendingWait("cloud-week")
	if getString(wait, "window") != "seven_day" {
		t.Fatalf("the weekly wall was not recognised: %v", wait)
	}
	if scheduled := getMap(wait, "scheduled"); getString(scheduled, "method") != "cloud" || scheduled["pid"] != nil {
		t.Errorf("a cloud session's wait was handed to a runner: %v", scheduled)
	}
	if reason := journaledReason("cloud-week", "cloud-no-wake"); !strings.Contains(reason, "wake.maxMinutes") {
		t.Errorf("noctis why does not say that the weekly reset is past wake.maxMinutes: %q", reason)
	}
}

func TestACloudSessionIsToldOnceThatItsLimitsCannotBeTracked(t *testing.T) {
	cfg, project := cloudSandbox(t, t.TempDir(), nil)
	first := getString(hookOutput(t, onUserPromptSubmit, promptInput("cloud-notice", project, "hello there"), cfg), "systemMessage")
	if !strings.Contains(first, "cannot be tracked in a cloud session") || strings.Contains(first, "statusLine") {
		t.Errorf("the first prompt of a cloud session is not told that its limits cannot be tracked: %q", first)
	}
	second := getString(hookOutput(t, onUserPromptSubmit, promptInput("cloud-notice", project, "and again"), cfg), "systemMessage")
	if strings.Contains(second, "cloud session") {
		t.Errorf("the cloud notice came twice in one session: %q", second)
	}
	mustWriteJSON(files.settings, object{})
	if issues := strings.Join(selfCheckIssues(cfg), "\n"); strings.Contains(issues, T("selfcheck.statusline")) {
		t.Errorf("a cloud session is told to wire a status line it never runs: %q", issues)
	}
}
