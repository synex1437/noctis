package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const humanQueue = "# q\n- [ ] migrate the users table\n- [ ] (human) open the payment account #pay\n- [ ] charge the first customer (after #pay)\n- [ ] write the release notes\n"

// humanSandbox is a project whose trusted TASKS.md holds an item marked (human) and one that waits
// for it.
func humanSandbox(t *testing.T, check string) (object, string, string) {
	t.Helper()
	cfg, project, frontend := queueCheckSandbox(t, check)
	trustQueueFile(writeQueueFile(t, project, humanQueue), true)
	return cfg, project, frontend
}

// humanTicked is humanQueue with the items named ticked.
func humanTicked(items ...string) string {
	content := humanQueue
	for _, item := range items {
		content = strings.Replace(content, "- [ ] "+item, "- [x] "+item, 1)
	}
	return content
}

func queueEdit(sid, cwd, project, old, next string) object {
	return agentHookInput("PreToolUse", sid, cwd, object{"tool_name": "Edit", "tool_input": object{"file_path": filepath.Join(project, "TASKS.md"), "old_string": old, "new_string": next}})
}

func deniedTool(output object) bool {
	return getString(getMap(output, "hookSpecificOutput"), "permissionDecision") == "deny"
}

func TestOnlyTheHumanMarkerMakesAnItemTheUsers(t *testing.T) {
	cases := map[string]bool{
		"(human) open the payment account": true,
		"open the payment account (HUMAN)": true,
		"ödeme hesabını aç (insan)":        true,
		"(humans) review the copy":         false,
		"human review of the copy":         false,
		"write the (human-readable) docs":  false,
	}
	for text, want := range cases {
		if got := newQueueEntry(1, text, false).human; got != want {
			t.Errorf("newQueueEntry(%q).human = %t, want %t", text, got, want)
		}
	}
}

func TestAHumanItemIsNeverClaudesNextItem(t *testing.T) {
	cfg, _, frontend := humanSandbox(t, "")
	view := queueSnapshotOf("TASKS.md", humanQueue)
	if view.total != 3 || view.human != 1 || view.blocked != 1 || len(view.items) != 2 {
		t.Fatalf("the snapshot counts %d open, %d human, %d blocked, eligible %q; want 3, 1, 1 and the two items Claude can take", view.total, view.human, view.blocked, view.items)
	}
	output := stopHookOutput(t, stopInput("hu1", frontend), cfg)
	reason := getString(output, "reason")
	if getString(output, "decision") != "block" || !strings.Contains(reason, `("migrate the users table")`) {
		t.Fatalf("the queue did not go on with the first item Claude can take: %v", output)
	}
	if !strings.Contains(reason, "1 item(s) marked (human) are the user's own work") || !strings.Contains(reason, "1 item(s) wait on unfinished dependencies") {
		t.Fatalf("the continuation did not tell Claude to leave the user's item and what waits for it: %q", reason)
	}
}

func TestTheUsersItemsAloneLetTheSessionStopAndAreNamedOnce(t *testing.T) {
	cfg, project, frontend := humanSandbox(t, "")
	writeQueueFile(t, project, humanTicked("migrate the users table", "write the release notes"))
	view := queueSnapshotOf("TASKS.md", humanTicked("migrate the users table", "write the release notes"))
	_, names := humanItemNames(view)
	output := stopHookOutput(t, stopInput("hu2", frontend), cfg)
	if getString(output, "decision") == "block" {
		t.Fatalf("with only the user's item and what waits for it left, the session was kept going: %v", output)
	}
	if got, want := getString(output, "systemMessage"), T("queue.humanBlockedMessage", 1, 1, names); got != want {
		t.Fatalf("the stop said %q, want %q", got, want)
	}
	if again := stopHookOutput(t, stopAgain("hu2", frontend), cfg); again != nil {
		t.Fatalf("the user was told about the same items a second time: %v", again)
	}
	if told := loggedTimes("notify: " + pluginName + " — " + T("queue.humanBlockedNotify", 1, "TASKS.md", 1, names)); told != 1 {
		t.Fatalf("the user was notified %d times of the items that wait for them, want once", told)
	}
	if done := loggedTimes(T("queue.doneNotify", "TASKS.md")); done != 0 {
		t.Fatal("a queue with the user's items still open was announced as finished")
	}
}

func TestALastHumanItemIsNotAFinishedQueue(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "")
	path := writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n- [ ] (human) sign the hosting contract\n")
	trustQueueFile(path, true)
	writeQueueFile(t, project, "# q\n- [x] migrate the users table\n- [ ] (human) sign the hosting contract\n")
	output := stopHookOutput(t, stopInput("hu3", frontend), cfg)
	if got, want := getString(output, "systemMessage"), T("queue.humanMessage", 1, "TASKS.md", `"(human) sign the hosting contract"`); got != want {
		t.Fatalf("the stop said %q, want %q", got, want)
	}
	if done := loggedTimes(T("queue.doneNotify", "TASKS.md")); done != 0 {
		t.Fatal("a queue whose last open item is the user's was announced as finished")
	}
}

func TestTickingTheUsersItemLetsWhatWaitsForItGoOn(t *testing.T) {
	cfg, project, frontend := humanSandbox(t, "")
	writeQueueFile(t, project, humanTicked("migrate the users table", "write the release notes"))
	stopHookOutput(t, stopInput("hu4", frontend), cfg)
	writeQueueFile(t, project, humanTicked("migrate the users table", "write the release notes", "(human) open the payment account #pay"))
	output := stopHookOutput(t, stopInput("hu4", frontend), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), `("charge the first customer (after #pay)")`) {
		t.Fatalf("once the user ticked their item, the item that waited for it did not go on: %v", output)
	}
}

func TestTheQueueCheckRunsBeforeTheUsersTurn(t *testing.T) {
	cfg, project, frontend := humanSandbox(t, "echo checked> verified.txt")
	writeQueueFile(t, project, humanTicked("migrate the users table", "write the release notes"))
	stopHookOutput(t, stopInput("hu5", frontend), cfg)
	if _, err := os.Stat(filepath.Join(project, "verified.txt")); err != nil {
		t.Fatalf("the queue check did not run before the queue waited for the user: %v", err)
	}
}

func TestClaudeCannotTickTheUsersItemInATurnNoctisStarted(t *testing.T) {
	cfg, project, frontend := humanSandbox(t, "")
	item := "- [ ] (human) open the payment account #pay"
	output := hookOutput(t, onPreToolUse, queueEdit("hu6", frontend, project, item, strings.Replace(item, "[ ]", "[x]", 1)), cfg)
	if !deniedTool(output) || !strings.Contains(getString(output, "systemMessage"), T("queue.humanTickByModel", "TASKS.md", `"(human) open the payment account #pay"`)) {
		t.Fatalf("Claude ticked the user's item in a turn the user did not start: %v", output)
	}
	removed := agentHookInput("PreToolUse", "hu6", frontend, object{"tool_name": "Write", "tool_input": object{"file_path": filepath.Join(project, "TASKS.md"), "content": strings.Replace(humanQueue, item+"\n", "", 1)}})
	if output := hookOutput(t, onPreToolUse, removed, cfg); !deniedTool(output) {
		t.Fatalf("Claude removed the user's item from the queue file: %v", output)
	}
	reworded := agentHookInput("PreToolUse", "hu6", frontend, object{"tool_name": "MultiEdit", "tool_input": object{"file_path": filepath.Join(project, "TASKS.md"), "edits": []any{object{"old_string": "(human) open", "new_string": "open"}}}})
	if output := hookOutput(t, onPreToolUse, reworded, cfg); !deniedTool(output) {
		t.Fatalf("Claude took the (human) mark off the user's item: %v", output)
	}
	own := queueEdit("hu6", frontend, project, "- [ ] migrate the users table", "- [x] migrate the users table")
	if output := hookOutput(t, onPreToolUse, own, cfg); output != nil {
		t.Fatalf("ticking an item of Claude's own was refused: %v", output)
	}
}

func TestClaudeTicksTheUsersItemWhenTheUserSaysItIsDone(t *testing.T) {
	cfg, project, frontend := humanSandbox(t, "")
	item := "- [ ] (human) open the payment account #pay"
	tick := queueEdit("hu7", frontend, project, item, strings.Replace(item, "[ ]", "[x]", 1))
	hookOutput(t, onUserPromptSubmit, promptInput("hu7", frontend, "I opened the payment account, tick it"), cfg)
	if output := hookOutput(t, onPreToolUse, tick, cfg); output != nil {
		t.Fatalf("in a turn the user typed, ticking the item they said is done was refused: %v", output)
	}
	if output := stopHookOutput(t, stopInput("hu7", frontend), cfg); getString(output, "decision") != "block" {
		t.Fatalf("the queue did not go on after the user's turn: %v", output)
	}
	if output := hookOutput(t, onPreToolUse, tick, cfg); !deniedTool(output) {
		t.Fatalf("in the turn the queue continued, Claude could still tick the user's item: %v", output)
	}
	hookOutput(t, onUserPromptSubmit, promptInput("hu7", frontend, queueContinuesPrefix+": 2 open in TASKS.md."), cfg)
	if output := hookOutput(t, onPreToolUse, tick, cfg); !deniedTool(output) {
		t.Fatalf("a queue continuation delivered as a prompt counted as the user's word: %v", output)
	}
}

func TestTheQueueDirectiveTellsClaudeWhichItemsAreTheUsers(t *testing.T) {
	cfg, project, _ := humanSandbox(t, "")
	output := hookOutput(t, onSessionStart, object{"hook_event_name": "SessionStart", "source": "startup", "session_id": "hu8", "cwd": project}, cfg)
	if context := getString(getMap(output, "hookSpecificOutput"), "additionalContext"); !strings.Contains(context, "1 item(s) marked (human) are the user's own work") {
		t.Fatalf("the queue directive did not tell Claude to leave the user's items: %q", context)
	}
}

func TestQueueStatusNamesTheUsersItems(t *testing.T) {
	cfg, project, _ := humanSandbox(t, "")
	status := capturedStdout(t, func() { runQueueTrust(cfg, project, "status") })
	if !strings.Contains(status, T("queue.humanStatus", "TASKS.md", 1, `"(human) open the payment account #pay"`)) {
		t.Fatalf("noctis queue status does not name the items marked (human):\n%s", status)
	}
}
