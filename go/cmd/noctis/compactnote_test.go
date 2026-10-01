package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func compactStart(sid, cwd string) object {
	return object{"hook_event_name": "SessionStart", "source": "compact", "session_id": sid, "cwd": cwd}
}

func sessionContext(t *testing.T, input, cfg object) string {
	t.Helper()
	return getString(getMap(hookOutput(t, onSessionStart, input, cfg), "hookSpecificOutput"), "additionalContext")
}

func TestTheSessionAfterACompactionIsToldWhereItsItemStands(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "make check")
	path := filepath.Join(project, "TASKS.md")
	gitIn(t, project, "init", "-q")
	gitIn(t, project, "add", "-A")
	gitIn(t, project, "commit", "-q", "-m", "start")
	writeRepoFile(t, project, "users.go", "package users\n")
	for range 2 {
		if output := hookOutput(t, onPreCompact, agentHookInput("PreCompact", "cn1", frontend, object{"trigger": "auto"}), cfg); output != nil {
			t.Fatalf("PreCompact answered %v; it never holds a compaction back", output)
		}
	}
	context := sessionContext(t, compactStart("cn1", frontend), cfg)
	for _, want := range []string{
		"Queue mode (TASKS.md: 2 open)",
		"[noctis] After the compaction",
		`The item in hand: "migrate the users table".`,
		"Changed since the last commit: users.go.",
		"noctis runs the queue's check (`make check`) after you tick the item.",
		"This item has now gone through 2 compactions",
	} {
		if !strings.Contains(context, want) {
			t.Fatalf("the note after a compaction lacks %q:\n%s", want, context)
		}
	}
	if !journaledAction("cn1", "count-compaction") {
		t.Fatalf("the compaction count is not in the journal: %v", journaledFor("cn1"))
	}

	updateState(func(next object) {
		stateMap(next, "queueVerify")[queueTrustKey(path)] = object{"failures": float64(1), "at": float64(nowSec() - 60)}
	})
	writeQueueFile(t, project, "# q\n- [x] migrate the users table\n- [ ] write the release notes\n")
	trustQueueFile(path, true)
	context = sessionContext(t, compactStart("cn1", frontend), cfg)
	if !strings.Contains(context, `The item in hand: "write the release notes".`) || !strings.Contains(context, "failed at its last run: fix that first") || strings.Contains(context, "compactions:") {
		t.Fatalf("on the next item, with the check failing, the note says:\n%s", context)
	}

	startup := sessionContext(t, object{"hook_event_name": "SessionStart", "source": "startup", "session_id": "cn2", "cwd": frontend}, cfg)
	if strings.Contains(startup, "After the compaction") {
		t.Fatalf("a session that started fresh got the note meant for one after a compaction:\n%s", startup)
	}
}

func TestTheNoteAfterACompactionSaysWhenNothingChecksTheQueue(t *testing.T) {
	cfg, _, frontend := queueCheckSandbox(t, "")
	context := sessionContext(t, compactStart("cn3", frontend), cfg)
	if !strings.Contains(context, "Nothing checks this queue between items, so run the project's tests yourself before you tick the item.") {
		t.Fatalf("the note after a compaction does not say that nothing checks the queue:\n%s", context)
	}
	if strings.Contains(context, "since the last commit") {
		t.Fatalf("outside a git repository the note names changed files:\n%s", context)
	}
}

func TestNoCompactionIsCountedOrNotedForAQueueNobodyTrusted(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "")
	writeQueueFile(t, project, "# q\n- [ ] rename the users columns\n")
	hookOutput(t, onPreCompact, agentHookInput("PreCompact", "cn4", frontend, object{"trigger": "manual"}), cfg)
	if record := getMap(readState(), "queueCompactions"); len(record) != 0 {
		t.Fatalf("a compaction on a queue nobody trusted was counted: %v", record)
	}
	if context := sessionContext(t, compactStart("cn4", frontend), cfg); strings.Contains(context, "After the compaction") {
		t.Fatalf("a queue nobody trusted got the note after a compaction:\n%s", context)
	}
}

func TestTheNoteAfterACompactionNamesTheCheckThatRanLast(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "make check")
	section(cfg, "queue")["verifyEachCommand"] = "make quick"
	key := queueTrustKey(filepath.Join(project, "TASKS.md"))
	for _, setting := range []struct {
		record object
		want   string
	}{
		{object{"failures": float64(1), "tier": eachQueueCheck}, "The queue's check (`make quick`) failed at its last run"},
		{object{"failures": float64(1)}, "The queue's check (`make check`) failed at its last run"},
		{object{"retried": float64(nowSec()), "rerun": float64(nowSec()), "tier": eachQueueCheck}, "The queue's check (`make quick`) was cut short at its last run"},
	} {
		setting.record["at"] = float64(nowSec() - 60)
		updateState(func(next object) { stateMap(next, "queueVerify")[key] = setting.record })
		if context := sessionContext(t, compactStart("cn5", frontend), cfg); !strings.Contains(context, setting.want) {
			t.Fatalf("with the check record %v the note after a compaction lacks %q:\n%s", setting.record, setting.want, context)
		}
	}
}
