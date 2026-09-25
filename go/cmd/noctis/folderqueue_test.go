package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sessionStartInput(sid, source, cwd string) object {
	return object{"hook_event_name": "SessionStart", "source": source, "session_id": sid, "cwd": cwd}
}

func TestAFolderQueueFileNobodyTrustedIsNeitherFollowedNorMentioned(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "# notes\n- [ ] call the accountant about the invoice\n- [ ] migrate the database\n")
	for _, source := range []string{"startup", "resume", "compact"} {
		output := hookOutput(t, onSessionStart, sessionStartInput("fq1", source, project), cfg)
		if text := fmt.Sprint(output); strings.Contains(text, "TASKS.md") || strings.Contains(text, "queue trust") {
			t.Errorf("session start (%s) brought up the TASKS.md nobody trusted: %v", source, output)
		}
	}
	if output := stopHookOutput(t, stopInput("fq1", project), cfg); output != nil {
		t.Errorf("the Stop hook acted on the TASKS.md nobody trusted: %v", output)
	}
	if file := sessionQueueFile(cfg, "fq1", project); file != "" {
		t.Errorf("noctis follows %s although nobody trusted it", file)
	}
}

func TestAListPromptIsQueuedInAFolderWhoseQueueFileNobodyTrusted(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "- [ ] migrate the database\n")
	output := startQueue(t, cfg, "fq2", project, listPromptForAutoQueue())
	if checklist := sessionQueueFile(cfg, "fq2", project); !isAutoQueue(checklist) {
		t.Fatalf("a TASKS.md nobody trusted kept the jobs of the prompt from being queued (queue file %q): %v", checklist, output)
	}
}

func TestTheFolderFileTheUserTrustedIsFollowedWhenAnotherComesFirst(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "- [ ] call the accountant about the invoice\n")
	docs := filepath.Join(project, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	trustQueueFile(writeJobFile(t, docs, "TASKS.md", "- [ ] migrate the database\n"), true)
	output := stopHookOutput(t, stopInput("fq3", project), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "migrate the database") {
		t.Fatalf("docs/TASKS.md, which the user trusted, does not drive the session because TASKS.md nobody trusted comes first: %v", output)
	}
}

func TestStopLeavesOutAFolderFileNobodyTrusted(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "- [ ] migrate the database\n")
	if output := startQueue(t, cfg, "fq4", project, "/noctis:stop"); getString(output, "reason") != T("queue.stopNone") {
		t.Errorf("/noctis:stop named a TASKS.md that drives nothing: %v", output)
	}
}

func TestStopSaysWhyAFolderFileDrivesTheSessionWhenTrustIsOff(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	section(cfg, "queue")["requireTrust"] = false
	writeQueueFile(t, project, "- [ ] migrate the database\n")
	if output := startQueue(t, cfg, "fq5", project, "/noctis:stop"); getString(output, "reason") != T("queue.stopProjectFileOpen", "TASKS.md") {
		t.Errorf("/noctis:stop with queue.requireTrust off does not say why TASKS.md drives the session: %v", output)
	}
}

func TestQueueStatusStillShowsAFolderFileNobodyTrusted(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "- [ ] migrate the database\n- [ ] write the release notes\n")
	printed := queueImportOutput(t, "queue", "status", "--cwd", project)
	if want := T("queue.trustAsk", "TASKS.md", 2, pluginName); !strings.Contains(printed, want) {
		t.Errorf("noctis queue status no longer shows the TASKS.md waiting for a trust:\n%s", printed)
	}
}

func TestQueueTrustForASessionThatStartedAQueueStillTakesTheFolderFile(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "- [ ] migrate the database\n- [ ] write the release notes\n")
	writeJobFile(t, project, "deneme.md", "- add a login page\n")
	startedChecklist(t, cfg, "fq7", project, "deneme.md")
	printed := queueImportOutput(t, "queue", "status", "--cwd", project, "--sid", "fq7")
	if want := T("queue.trustAsk", "TASKS.md", 2, pluginName); !strings.Contains(printed, want) {
		t.Errorf("noctis queue status --sid shows the queue the session started instead of the TASKS.md waiting for a trust:\n%s", printed)
	}
}

func TestWithQueuesTurnedOffStartSaysSoAndStartsNothing(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	section(cfg, "queue")["enabled"] = false
	writeJobFile(t, project, "deneme.md", "- add a login page\n- add a logout button\n")
	output := startQueue(t, cfg, "fq6", project, "/noctis:start deneme.md")
	if getString(output, "decision") != "block" || getString(output, "reason") != T("queue.startOff", pluginName) {
		t.Errorf("/noctis:start with queue.enabled off did not say queues are off: %v", output)
	}
	if record := getMap(getMap(readState(), "autoQueues"), "fq6"); record != nil {
		t.Fatalf("/noctis:start started a queue with queue.enabled off: %v", record)
	}
}
