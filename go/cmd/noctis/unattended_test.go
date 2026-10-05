package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func askUser(sid, cwd, question string) object {
	return agentHookInput("PreToolUse", sid, cwd, object{"tool_name": "AskUserQuestion", "tool_input": object{"questions": []any{
		object{"question": question, "header": "Database", "multiSelect": false, "options": []any{
			object{"label": "Postgres", "description": "keep the current database"},
			object{"label": "SQLite", "description": "a file next to the app"},
		}},
	}}})
}

func refusalOf(output object) string {
	return getString(getMap(output, "hookSpecificOutput"), "permissionDecisionReason")
}

func waitingFor(sid, cwd, kind, message string) object {
	return agentHookInput("Notification", sid, cwd, object{"notification_type": kind, "message": message, "title": "Claude Code"})
}

func waitingNotices(sid, cwd, message string) int {
	return loggedTimes("notify: " + pluginName + " — " + T("notification.waiting", filepath.Base(cwd), shortSid(sid), message))
}

func TestTheHookStartsForQuestionsAndForEveryWaitItTellsTheUserOf(t *testing.T) {
	if !preToolUseMatches(t)("AskUserQuestion") {
		t.Fatal("hooks/hooks.json does not start the PreToolUse hook for AskUserQuestion")
	}
	wired := map[string]bool{}
	for _, group := range getList(getMap(readJSON(filepath.Join(repoRoot(), "hooks", "hooks.json")), "hooks"), "Notification") {
		for _, kind := range strings.Split(getString(toObject(group), "matcher"), "|") {
			wired[kind] = true
		}
	}
	for kind := range waitingNoticeKinds {
		if !wired[kind] {
			t.Errorf("hooks/hooks.json does not start the Notification hook for %s", kind)
		}
	}
	if wired["idle_prompt"] {
		t.Error("the Notification hook starts for idle_prompt, which Claude Code sends whenever a session has sat idle for a minute after a turn")
	}
}

func TestAQuestionWhileAQueueDrivesTheSessionIsLeftToClaude(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "")
	queuePath := filepath.Join(project, "TASKS.md")
	output := hookOutput(t, onPreToolUse, askUser("uq1", frontend, "Which database should\nthe users table move to?"), cfg)
	reason := refusalOf(output)
	if !deniedTool(output) || !strings.HasPrefix(reason, "[noctis] A queue drives this session (TASKS.md: 2 open)") || !strings.Contains(reason, "Do not ask: decide yourself") {
		t.Fatalf("a question in a session the queue drives was put to a user who is away: %v", output)
	}
	for _, want := range []string{
		pluginName + ` queue note "<the decision, and why>" --file ` + shellQuote(queuePath),
		pluginName + ` queue defer <a unique part of the item's text> --reason "<what it waits on>" --file ` + shellQuote(queuePath) + " and go on with the next item",
	} {
		if !strings.Contains(reason, want) {
			t.Fatalf("the refusal does not give Claude %q: %s", want, reason)
		}
	}
	if entry := journaledEntry("uq1", "deny-question"); getString(entry, "reason") != "Which database should the users table move to?" || getString(entry, "file") != "TASKS.md" || numberOr(entry, "open", 0) != 2 {
		t.Fatalf("the refused question was not journaled on one line with its queue: %v", entry)
	}
	blank := agentHookInput("PreToolUse", "uq1", frontend, object{"tool_name": "AskUserQuestion", "tool_input": object{}})
	if output := hookOutput(t, onPreToolUse, blank, cfg); !deniedTool(output) || getString(journaledEntry("uq1", "deny-question"), "reason") != "TASKS.md" {
		t.Fatalf("a question with no text was not refused under the queue's name: %v", output)
	}
}

func TestAQuestionInAChecklistFromThePromptIsDecidedInTheReply(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	path := checklistFor(t, "uq2", project)
	output := hookOutput(t, onPreToolUse, askUser("uq2", project, "Should the signup form check e-mail addresses?"), cfg)
	reason := refusalOf(output)
	if !deniedTool(output) || !strings.Contains(reason, "("+filepath.Base(path)+": 3 open)") || !strings.Contains(reason, "say in your reply what you decided and why") {
		t.Fatalf("a question in a session its prompt's checklist drives was not left to Claude: %v", output)
	}
	if strings.Contains(reason, "queue note") || !strings.Contains(reason, "--file "+shellQuote(path)+" and go on") {
		t.Fatalf("the refusal names a decision note the digest never reads for a checklist, or no deferral for it: %s", reason)
	}
}

func TestAQuestionReachesTheUserWhenNoQueueDrivesTheSession(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) (object, string)
	}{
		{"no queue", func(t *testing.T) (object, string) {
			return queueTrustSandbox(t, false)
		}},
		{"an untrusted queue", func(t *testing.T) (object, string) {
			cfg, project := queueTrustSandbox(t, false)
			writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n")
			return cfg, project
		}},
		{"a finished queue", func(t *testing.T) (object, string) {
			cfg, project, _ := queueCheckSandbox(t, "")
			trustQueueFile(writeQueueFile(t, project, "# q\n- [x] migrate the users table\n- [x] write the release notes\n"), true)
			return cfg, project
		}},
		{"only the user's own items open", func(t *testing.T) (object, string) {
			cfg, project, _ := queueCheckSandbox(t, "")
			trustQueueFile(writeQueueFile(t, project, "# q\n- [x] migrate the users table\n- [ ] (human) open the payment account\n"), true)
			return cfg, project
		}},
		{"a queue its failing check holds", func(t *testing.T) (object, string) {
			cfg, project, _ := heldQueue(t, "echo checked")
			return cfg, project
		}},
		{"noctis paused", func(t *testing.T) (object, string) {
			cfg, project, _ := queueCheckSandbox(t, "")
			updateState(func(next object) { next["disabledUntil"] = float64(nowSec() + 3600) })
			return cfg, project
		}},
		{"queues off for the run", func(t *testing.T) (object, string) {
			cfg, project, _ := queueCheckSandbox(t, "")
			t.Setenv(queueEnv, "off")
			return cfg, project
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cwd := tc.setup(t)
			if output := hookOutput(t, onPreToolUse, askUser("uq3", cwd, "Which database should the users table move to?"), cfg); output != nil {
				t.Fatalf("with %s the question did not reach the user: %v", tc.name, output)
			}
			if len(journaledFor("uq3")) != 0 {
				t.Fatalf("with %s the question was journaled: %v", tc.name, journaledFor("uq3"))
			}
		})
	}
}

func TestObserveModeOnlyJournalsWhatTheUnattendedChecksWouldDo(t *testing.T) {
	cfg, _, frontend := queueCheckSandbox(t, "")
	defer func(previous bool) { observing = previous }(observing)
	observing = true
	if output := hookOutput(t, onPreToolUse, askUser("uo1", frontend, "Which database?"), cfg); output != nil {
		t.Fatalf("in observe mode the question was refused: %v", output)
	}
	hookOutput(t, onNotification, waitingFor("uo1", frontend, "permission_prompt", "Claude needs your permission to use Bash"), cfg)
	if actions := journaledFor("uo1"); strings.Join(actions, " ") != "would-deny-question would-waiting-notice" {
		t.Fatalf("observe mode journaled %v, want the refusal and the notice it would make", actions)
	}
	if waitingNotices("uo1", frontend, "Claude needs your permission to use Bash") != 0 {
		t.Fatal("in observe mode the user was notified")
	}
}

func TestASessionTheQueueDrivesTellsTheUserOnceThatItWaitsForThem(t *testing.T) {
	cfg, _, frontend := queueCheckSandbox(t, "")
	message := "Claude needs your permission to use Bash"
	updateState(func(next object) {
		stateMap(next, "autoResume")["uw1"] = object{"type": "quota_auto_resume_fired", "at": float64(nowSec())}
	})
	if output := hookOutput(t, onNotification, waitingFor("uw1", frontend, "permission_prompt", message), cfg); output != nil {
		t.Fatalf("the notice printed something to Claude Code: %v", output)
	}
	if waitingNotices("uw1", frontend, message) != 1 || getString(journaledEntry("uw1", "waiting-notice"), "type") != "permission_prompt" {
		t.Fatalf("a session the queue drives waits for an approval and the user was not told: %v", journaledFor("uw1"))
	}
	if kind := getString(getMap(getMap(readState(), "autoResume"), "uw1"), "type"); kind != "quota_auto_resume_fired" {
		t.Fatalf("the permission prompt replaced the record of Claude Code's own auto-continue: %q", kind)
	}
	hookOutput(t, onNotification, waitingFor("uw1", frontend, "permission_prompt", message), cfg)
	if waitingNotices("uw1", frontend, message) != 1 {
		t.Fatal("a second wait within ten minutes notified the user again")
	}
	updateState(func(next object) {
		stateMap(next, "notified")["waiting:uw1"] = float64(nowSec() - waitingNoticeGapSeconds)
	})
	hookOutput(t, onNotification, waitingFor("uw1", frontend, "permission_prompt", message), cfg)
	if waitingNotices("uw1", frontend, message) != 2 {
		t.Fatal("a wait ten minutes after the last notice did not notify the user")
	}
}

func TestEveryKindOfWaitInAnUnattendedSessionNotifies(t *testing.T) {
	cfg, _, frontend := queueCheckSandbox(t, "")
	for kind := range waitingNoticeKinds {
		hookOutput(t, onNotification, waitingFor("uk-"+kind, frontend, kind, "waits\nfor "+kind), cfg)
		if waitingNotices("uk-"+kind, frontend, "waits for "+kind) != 1 {
			t.Errorf("a %s in a session the queue drives did not notify the user on one line", kind)
		}
	}
}

func TestASessionNoctisContinuedTellsTheUserWhenItWaitsForThem(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	standInNotifiers(t)
	t.Setenv(handoffEnv, "ur1")
	hookOutput(t, onNotification, waitingFor("ur1", project, "permission_prompt", "Claude needs your permission to use Bash"), cfg)
	if waitingNotices("ur1", project, "Claude needs your permission to use Bash") != 1 {
		t.Fatal("a session noctis relaunched waits for an approval and the user was not told")
	}
	t.Setenv(handoffEnv, "")
	updateState(func(next object) { stateMap(next, "waits")["ur2"] = object{"wakeAttemptedAt": float64(nowSec())} })
	hookOutput(t, onNotification, waitingFor("ur2", project, "elicitation_dialog", "An MCP server asks for input"), cfg)
	if waitingNotices("ur2", project, "An MCP server asks for input") != 1 {
		t.Fatal("a session noctis woke in place waits for an answer and the user was not told")
	}
}

func TestAWaitTheUserIsThereForStaysQuiet(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	standInNotifiers(t)
	hookOutput(t, onNotification, waitingFor("us1", project, "permission_prompt", "Claude needs your permission to use Bash"), cfg)
	if loggedTimes("notify: ") != 0 || len(journaledFor("us1")) != 0 {
		t.Fatalf("a session the user runs notified them of its own permission prompt: %v", journaledFor("us1"))
	}
	t.Setenv(handoffEnv, "us2")
	updateState(func(next object) { next["disabledUntil"] = float64(nowSec() + 3600) })
	hookOutput(t, onNotification, waitingFor("us2", project, "permission_prompt", "Claude needs your permission to use Bash"), cfg)
	if loggedTimes("notify: ") != 0 || len(journaledFor("us2")) != 0 {
		t.Fatalf("with noctis paused a relaunched session's wait notified the user: %v", journaledFor("us2"))
	}
}
