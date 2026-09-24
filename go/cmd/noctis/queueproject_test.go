package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectQueueSandbox(t *testing.T) (object, string, string) {
	t.Helper()
	cfg, project := queueTrustSandbox(t, false)
	previous := activeHost
	t.Cleanup(func() { activeHost = previous })
	activeHost = "claude"
	trustQueueFile(writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n- [ ] write the release notes\n"), true)
	frontend := filepath.Join(project, "frontend")
	if err := os.Mkdir(frontend, 0o755); err != nil {
		t.Fatal(err)
	}
	return cfg, project, frontend
}

func listPromptForAutoQueue() string {
	return "Here is some context about the project so that the prompt is long enough for the detector to consider it: " +
		strings.Repeat("the codebase is a Node service with a Postgres database and a React front end. ", 2) +
		"\n- add input validation to the signup form\n- write tests for the payments module\n- update the README for the new CLI flags\n"
}

func TestTheQueueKeepsDrivingAfterClaudeChangesDirectory(t *testing.T) {
	cfg, project, frontend := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	output := stopHookOutput(t, stopInput("pd1", frontend), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "2 open in TASKS.md") || !strings.Contains(getString(output, "reason"), "migrate the users table") {
		t.Fatalf("after cd frontend the Stop hook let the session stop with the project's TASKS.md still open: %v", output)
	}
}

func TestTheQueueDirectiveComesBackAfterCompactionInASubfolder(t *testing.T) {
	cfg, project, frontend := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	output := hookOutput(t, onSessionStart, object{"hook_event_name": "SessionStart", "source": "compact", "session_id": "pd2", "cwd": frontend}, cfg)
	if context := getString(getMap(output, "hookSpecificOutput"), "additionalContext"); !strings.Contains(context, "Queue mode (TASKS.md: 2 open)") {
		t.Fatalf("the compaction in frontend lost the project's queue directive: %v", output)
	}
}

func TestAListPromptAfterCdDoesNotStartASecondQueue(t *testing.T) {
	cfg, project, frontend := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	hookOutput(t, onUserPromptSubmit, promptInput("pd3", frontend, listPromptForAutoQueue()), cfg)
	if record := getMap(getMap(readState(), "autoQueues"), "pd3"); record != nil {
		t.Fatalf("a list typed after cd frontend became a checklist next to the project's TASKS.md: %v", record)
	}
}

func TestTheSessionsProjectQueueWinsOverASubfoldersOwn(t *testing.T) {
	cfg, project, frontend := projectQueueSandbox(t)
	trustQueueFile(writeQueueFile(t, frontend, "# frontend\n- [ ] restyle the login page\n"), true)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	output := stopHookOutput(t, stopInput("pd4", frontend), cfg)
	if reason := getString(output, "reason"); getString(output, "decision") != "block" || !strings.Contains(reason, "migrate the users table") || strings.Contains(reason, "restyle the login page") {
		t.Fatalf("after cd frontend the Stop hook switched from the project's TASKS.md to frontend's: %v", output)
	}
}

func TestWithoutAProjectFolderTheQueueIsLookedUpWhereTheHookRuns(t *testing.T) {
	cfg, _, frontend := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	if output := stopHookOutput(t, stopInput("pd5", frontend), cfg); output != nil {
		t.Fatalf("with no CLAUDE_PROJECT_DIR the Stop hook looked for a queue outside the folder it runs in: %v", output)
	}
}

func TestOtherHostsIgnoreClaudeCodesProjectFolder(t *testing.T) {
	cfg, project, frontend := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	activeHost = "codex"
	if output := stopHookOutput(t, stopInput("pd6", frontend), cfg); output != nil {
		t.Fatalf("a Codex session in frontend was driven by a queue found through Claude Code's CLAUDE_PROJECT_DIR: %v", output)
	}
}

func relaunchRecorder(t *testing.T) (object, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(claudeConfigEnv, "")
	if err := os.Unsetenv(claudeConfigEnv); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	t.Setenv("NOCTIS_NO_TERMINAL", "1")
	t.Setenv(handoffEnv, "")
	files.resumeLog = filepath.Join(files.guardDir, "resume-output.log")
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls.log")
	t.Setenv("NOCTIS_TEST_CALLS", calls)
	if isWindows {
		writeScript(t, filepath.Join(bin, "claude.cmd"), "@>>\"%NOCTIS_TEST_CALLS%\" cd\r\n@>>\"%NOCTIS_TEST_CALLS%\" echo %*\r\n@exit /b 0\r\n")
	} else {
		writeScript(t, filepath.Join(bin, "claude"), "#!/bin/sh\npwd -P >> \"$NOCTIS_TEST_CALLS\"\nprintf '%s\\n' \"$*\" >> \"$NOCTIS_TEST_CALLS\"\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	mustWriteJSON(files.config, object{
		"resume": object{"mode": "headless", "permissionMode": "acceptEdits", "prompt": "carry on"},
		"alarm":  object{"enabled": false},
		"wait":   object{"workspaceGuard": false},
	})
	return loadConfig(), calls
}

func relaunchedIn(t *testing.T, calls string) (string, string) {
	t.Helper()
	content, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("the relaunch never started claude: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(content), "\r\n", "\n")), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected one relaunch (its folder and its arguments), claude recorded %q", lines)
	}
	return strings.TrimSpace(lines[0]), lines[1]
}

func sameFolder(t *testing.T, left, right string) bool {
	t.Helper()
	leftInfo, rightInfo := statSafe(left), statSafe(right)
	return leftInfo != nil && rightInfo != nil && os.SameFile(leftInfo, rightInfo)
}

func parkAndRelaunch(t *testing.T, calls, sid, what string, park func(sid string)) (string, string) {
	t.Helper()
	capturedStdout(t, func() { park(sid) })
	now := float64(nowSec())
	updateState(func(state object) {
		if wait := getMap(getMap(state, "waits"), sid); wait != nil {
			wait["until"], wait["resumeAt"] = now-10, now-5
		}
	})
	if pendingWait(sid) == nil {
		t.Fatalf("%s left no wait to relaunch", what)
	}
	statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
	if err := os.Remove(calls); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	resumeWait(sid, "")
	return relaunchedIn(t, calls)
}

func TestARelaunchAfterCdStartsInTheProjectAndNamesItsQueue(t *testing.T) {
	_, project, frontend := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	cfg, calls := relaunchRecorder(t)
	pauses := []struct {
		name string
		park func(sid string)
	}{
		{"a pause at the usage limit", func(sid string) {
			enforceWait("batch", object{"session_id": sid, "cwd": frontend}, cfg, decision{wait: fiveHourPlan(float64(nowSec() + 8*3600)), model: "claude-opus-5"})
		}},
		{"a switch off Fable", func(sid string) {
			handleFableHit("batch", object{"session_id": sid, "cwd": frontend}, cfg, fableHitDecision())
		}},
		{"a retry after an overload", func(sid string) {
			onStopFailure(object{"session_id": sid, "cwd": frontend, "error_type": "overloaded"}, cfg)
		}},
	}
	for index, pause := range pauses {
		folder, arguments := parkAndRelaunch(t, calls, fmt.Sprintf("pd-relaunch%d", index+1), pause.name+" after cd frontend", pause.park)
		if !sameFolder(t, folder, project) {
			t.Errorf("%s after cd frontend: the relaunch started claude in %s, not in the folder the session started in (%s), so the relaunched session's CLAUDE_PROJECT_DIR is that subfolder and its hooks no longer find the project's TASKS.md", pause.name, folder, project)
		}
		if !strings.Contains(arguments, "Task list: TASKS.md") || !strings.Contains(arguments, "migrate the users table") {
			t.Errorf("%s after cd frontend: the relaunch prompt does not name the project's queue and its next item: %s", pause.name, arguments)
		}
	}
}

func TestARelaunchAfterCdStartsWhereTheQueueItNamesIs(t *testing.T) {
	_, project, frontend := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	cfg, calls := relaunchRecorder(t)
	own := writeQueueFile(t, frontend, "# frontend\n- [ ] restyle the login page\n")
	trustQueueFile(own, true)
	park := func(sid string) {
		enforceWait("batch", object{"session_id": sid, "cwd": frontend}, cfg, decision{wait: fiveHourPlan(float64(nowSec() + 8*3600)), model: "claude-opus-5"})
	}
	folder, arguments := parkAndRelaunch(t, calls, "pd-both", "a pause after cd frontend", park)
	if !sameFolder(t, folder, project) || !strings.Contains(arguments, "migrate the users table") || strings.Contains(arguments, "restyle the login page") {
		t.Errorf("with a queue in the project and one in frontend, the relaunch after cd frontend started claude in %s with %q; want the project folder and its queue, the one the hooks drive", folder, arguments)
	}
	if err := os.Remove(filepath.Join(project, "TASKS.md")); err != nil {
		t.Fatal(err)
	}
	folder, arguments = parkAndRelaunch(t, calls, "pd-own", "a pause after cd frontend", park)
	if !sameFolder(t, folder, frontend) || !strings.Contains(arguments, "Task list: TASKS.md") || !strings.Contains(arguments, "restyle the login page") {
		t.Errorf("with a queue in frontend only, the relaunch after cd frontend started claude in %s with %q; want frontend, the only folder where the relaunched session's hooks find that queue", folder, arguments)
	}
	if err := os.Remove(own); err != nil {
		t.Fatal(err)
	}
	folder, arguments = parkAndRelaunch(t, calls, "pd-none", "a pause after cd frontend", park)
	if !sameFolder(t, folder, project) || strings.Contains(arguments, "Task list") {
		t.Errorf("with no queue in either folder, the relaunch after cd frontend started claude in %s with %q; want the folder the session started in", folder, arguments)
	}
}

func TestARelaunchWithoutAProjectFolderStartsWhereTheSessionPaused(t *testing.T) {
	_, _, frontend := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	cfg, calls := relaunchRecorder(t)
	folder, _ := parkAndRelaunch(t, calls, "pd-relaunch-plain", "a pause with no CLAUDE_PROJECT_DIR", func(sid string) {
		enforceWait("batch", object{"session_id": sid, "cwd": frontend}, cfg, decision{wait: fiveHourPlan(float64(nowSec() + 8*3600)), model: "claude-opus-5"})
	})
	if !sameFolder(t, folder, frontend) {
		t.Fatalf("with no CLAUDE_PROJECT_DIR the relaunch started claude in %s, not in the folder the session paused in (%s)", folder, frontend)
	}
}
