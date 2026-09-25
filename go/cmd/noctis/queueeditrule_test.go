package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const onlyTheCheckbox = "When an item is done, change only its checkbox to [x]; do not add, edit or remove any other text in the file, since any other change makes noctis wait until the user trusts the file again."

func TestClaudeIsToldToChangeOnlyTheCheckboxOfAnItemItFinishes(t *testing.T) {
	cfg, project, _ := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	start := hookOutput(t, onSessionStart, object{"hook_event_name": "SessionStart", "session_id": "oc1", "cwd": project, "source": "startup"}, cfg)
	if context := contextOf(start); !strings.Contains(context, onlyTheCheckbox) {
		t.Errorf("the queue directive at session start does not tell Claude to change only the checkbox of an item it finishes:\n%s", context)
	}
	activeHost = "codex"
	if directive := queueDirective(cfg, filepath.Join(project, "TASKS.md"), 2); !strings.Contains(directive, onlyTheCheckbox) {
		t.Errorf("the queue directive of a host without subagents does not tell the model to change only the checkbox of an item it finishes:\n%s", directive)
	}
	activeHost = "claude"
	if reason := getString(stopHookOutput(t, stopInput("oc1", project), cfg), "reason"); !strings.Contains(reason, onlyTheCheckbox) {
		t.Errorf("the Stop hook's continuation does not tell Claude to change only the checkbox of an item it finishes:\n%s", reason)
	}
	relaunch, calls := relaunchRecorder(t)
	_, arguments := parkAndRelaunch(t, calls, "oc2", "a pause at the usage limit", func(sid string) {
		enforceWait("batch", object{"session_id": sid, "cwd": project}, relaunch, decision{wait: fiveHourPlan(float64(nowSec() + 8*3600)), model: "claude-opus-5"})
	})
	if !strings.Contains(arguments, onlyTheCheckbox) {
		t.Errorf("the relaunch prompt does not tell Claude to change only the checkbox of an item it finishes:\n%s", arguments)
	}
}

func TestAPlainListIsRewrittenWithCheckboxesWithoutChangingItsText(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	trustQueueFile(writeQueueFile(t, project, "# q\n- migrate the users table\n- write the release notes\n"), true)
	reason := getString(stopHookOutput(t, stopInput("oc3", project), cfg), "reason")
	if !strings.Contains(reason, `rewrite every open item as "- [ ] …" (finished ones as "- [x] …") without changing their text`) {
		t.Fatalf("the Stop hook asks Claude to rewrite the plain list with checkboxes without saying to keep the items' text, which a trust covers:\n%s", reason)
	}
}

func TestAQueueThatNeedsNoTrustIsNotToldAboutTheTrust(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	section(cfg, "queue")["requireTrust"] = false
	queuePath := writeQueueFile(t, project, "# q\n- [ ] write the release notes\n")
	if directive := queueDirective(cfg, queuePath, 1); strings.Contains(directive, "trusts the file") {
		t.Errorf("with queue.requireTrust off the queue directive speaks of a trust:\n%s", directive)
	}
	if reason := getString(stopHookOutput(t, stopInput("oc4", project), cfg), "reason"); !strings.Contains(reason, "write the release notes") || strings.Contains(reason, "trusts the file") {
		t.Errorf("with queue.requireTrust off the Stop hook's continuation speaks of a trust, or does not drive:\n%s", reason)
	}
	section(cfg, "queue")["requireTrust"] = true
	own := t.TempDir()
	if startAutoQueue("oc5", own, []string{"add input validation to the signup form", "write tests for the payments module"}, nowSec()) == "" {
		t.Fatal("the checklist from the prompt was not written")
	}
	if reason := getString(stopHookOutput(t, stopInput("oc5", own), cfg), "reason"); !strings.Contains(reason, "add input validation to the signup form") || strings.Contains(reason, "trusts the file") {
		t.Errorf("the continuation of the checklist noctis wrote from the prompt, which needs no trust, speaks of a trust, or does not drive:\n%s", reason)
	}
}
