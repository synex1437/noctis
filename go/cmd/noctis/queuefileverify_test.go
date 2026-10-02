package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fileVerifyItems = "- [ ] migrate the users table\n- [ ] write the release notes\n"

// fileVerifySandbox is a project whose TASKS.md names its own check command, trusted as written,
// with its first item ticked since.
func fileVerifySandbox(t *testing.T, check string) (object, string, string) {
	t.Helper()
	cfg, project, frontend := queueCheckSandbox(t, "")
	path := writeQueueFile(t, project, "# q\nnoctis-verify: `"+check+"`\n\n"+fileVerifyItems)
	trustQueueFile(path, true)
	writeQueueFile(t, project, "# q\nnoctis-verify: `"+check+"`\n\n"+strings.Replace(fileVerifyItems, "- [ ]", "- [x]", 1))
	return cfg, project, frontend
}

func TestATrustedQueueFileRunsTheCheckItNames(t *testing.T) {
	cfg, project, frontend := fileVerifySandbox(t, "echo checked> file-check.txt")
	output := stopHookOutput(t, stopInput("fv1", frontend), cfg)
	if reason := getString(output, "reason"); getString(output, "decision") != "block" || !strings.Contains(reason, "write the release notes") {
		t.Fatalf("after its check passed, the queue did not go on to its next item: %v", output)
	}
	if _, err := os.Stat(filepath.Join(project, "file-check.txt")); err != nil {
		t.Fatalf("the check command TASKS.md names did not run in the project folder: %v", err)
	}
}

func TestAFailingFileCheckSendsClaudeBackWithItsCommand(t *testing.T) {
	command := shellFor("echo broken; exit 4", "echo broken & exit 4")
	cfg, _, frontend := fileVerifySandbox(t, command)
	section(cfg, "queue")["escalate"] = "off"
	output := stopHookOutput(t, stopInput("fv2", frontend), cfg)
	reason := getString(output, "reason")
	if getString(output, "decision") != "block" || !strings.Contains(reason, "`"+command+"`") || !strings.Contains(reason, "exited with status 4") || strings.Contains(reason, "write the release notes") {
		t.Fatalf("a failing check from TASKS.md did not send Claude back to fix it: %v", output)
	}
	second := stopHookOutput(t, stopAgain("fv2", frontend), cfg)
	if getString(second, "decision") == "block" {
		t.Fatalf("the second failure in a row did not hold the queue: %v", second)
	}
	if told := loggedTimes("notify: " + pluginName + " — " + T("queue.heldNotifyFile", 2, "TASKS.md")); told != 1 {
		t.Fatalf("the hold of a file's own check was notified %d times with the file's wording, want once", told)
	}
}

func TestTheFilesCheckWinsOverQueueVerifyCommand(t *testing.T) {
	cfg, project, frontend := fileVerifySandbox(t, "echo file> file-check.txt")
	section(cfg, "queue")["verifyCommand"] = "echo config> config-check.txt"
	stopHookOutput(t, stopInput("fv3", frontend), cfg)
	if _, err := os.Stat(filepath.Join(project, "file-check.txt")); err != nil {
		t.Fatalf("the file's own check did not run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "config-check.txt")); err == nil {
		t.Fatal("queue.verifyCommand ran although the trusted queue file names its own check")
	}
}

func TestACheckLineAddedAfterTheTrustNeverRuns(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "")
	path := writeQueueFile(t, project, "# q\n"+fileVerifyItems)
	trustQueueFile(path, true)
	writeQueueFile(t, project, "# q\nnoctis-verify: `echo ran> added-check.txt`\n"+strings.Replace(fileVerifyItems, "- [ ]", "- [x]", 1))
	if output := stopHookOutput(t, stopInput("fv4", frontend), cfg); getString(output, "decision") == "block" {
		t.Fatalf("a queue file with a line added after its trust still drove the session: %v", output)
	}
	if _, err := os.Stat(filepath.Join(project, "added-check.txt")); err == nil {
		t.Fatal("a check command added to TASKS.md after the user trusted it was run")
	}
	if command := queueCheckCommand(cfg, path); command != "" {
		t.Fatalf("the check of a file whose trust no longer holds is %q, want none", command)
	}
}

func TestAFileCheckNeedsTheUsersTrustEvenWithRequireTrustOff(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "")
	section(cfg, "queue")["requireTrust"] = false
	writeQueueFile(t, project, "# q\nnoctis-verify: `echo ran> untrusted-check.txt`\n"+strings.Replace(fileVerifyItems, "- [ ]", "- [x]", 1))
	if err := os.Remove(filepath.Join(project, "untrusted-check.txt")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	trustQueueFile(filepath.Join(project, "TASKS.md"), false)
	output := stopHookOutput(t, stopInput("fv5", frontend), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "write the release notes") {
		t.Fatalf("with queue.requireTrust off the file no longer drove the session: %v", output)
	}
	if _, err := os.Stat(filepath.Join(project, "untrusted-check.txt")); err == nil {
		t.Fatal("with queue.requireTrust off, a queue file the user never trusted had its check command run")
	}
}

func TestQueueFileVerifyFalseLeavesTheLineAlone(t *testing.T) {
	cfg, project, frontend := fileVerifySandbox(t, "echo ran> off-check.txt")
	section(cfg, "queue")["fileVerify"] = false
	stopHookOutput(t, stopInput("fv6", frontend), cfg)
	if _, err := os.Stat(filepath.Join(project, "off-check.txt")); err == nil {
		t.Fatal("with queue.fileVerify false, the check TASKS.md names still ran")
	}
}

func TestOnlyALineOfItsOwnOutsideFencesNamesTheCheck(t *testing.T) {
	cases := []struct {
		content, want string
	}{
		{"noctis-verify: `make check`\n- [ ] a\n", "make check"},
		{"# q\nNOCTIS-VERIFY:   `bash scripts/verify.sh quick`  \n", "bash scripts/verify.sh quick"},
		{"- [ ] noctis-verify: `make check`\n", ""},
		{"> noctis-verify: `make check`\n", ""},
		{"```\nnoctis-verify: `make check`\n```\n", ""},
		{"see noctis-verify: `make check`\n", ""},
		{"noctis-verify: make check\n", ""},
		{"noctis-verify: `first`\nnoctis-verify: `second`\n", "first"},
	}
	for _, test := range cases {
		if got := queueVerifyLine(test.content); got != test.want {
			t.Errorf("queueVerifyLine(%q) = %q, want %q", test.content, got, test.want)
		}
	}
}

func TestACheckLineInsideAnHTMLCommentNamesNoCheck(t *testing.T) {
	cases := []struct {
		content, want string
	}{
		{"<!--\nnoctis-verify: `make check`\n-->\n- [ ] a\n", ""},
		{"<!-- the old check:\nnoctis-verify: `make old`\n-->\nnoctis-verify: `make check`\n", "make check"},
		{"<!-- a note -->\nnoctis-verify: `make check`\n", "make check"},
	}
	for _, test := range cases {
		if got := queueVerifyLine(test.content); got != test.want {
			t.Errorf("queueVerifyLine(%q) = %q, want %q", test.content, got, test.want)
		}
	}
	if got := queueVerifyEachLine("<!--\nnoctis-verify-each: `go vet ./...`\n-->\n- [ ] a\n"); got != "" {
		t.Errorf("a per-item check line inside an HTML comment named the check %q", got)
	}
}

func TestACheckLineRightUnderAnItemIsNotPartOfIt(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "")
	path := writeQueueFile(t, project, "# q\n- [ ] migrate the users table\nnoctis-verify-each: `go vet ./...`\n- [ ] write the release notes\nnoctis-verify: `go test ./...`\n")
	trustQueueFile(path, true)
	if view := queueSnapshot(path); !slices.Equal(view.items, []string{"migrate the users table", "write the release notes"}) {
		t.Fatalf("with the check lines right under the items, the open items are %q", view.items)
	}
	if reason := getString(stopHookOutput(t, stopInput("fv7", frontend), cfg), "reason"); !strings.Contains(reason, `("migrate the users table")`) {
		t.Fatalf("Claude was not handed the item without the check line under it: %s", reason)
	}
}

func TestQueueTrustAndStatusNameTheFilesCheck(t *testing.T) {
	cfg, project, _ := queueCheckSandbox(t, "")
	path := writeQueueFile(t, project, "# q\nnoctis-verify: `make check`\n"+fileVerifyItems)
	trustQueueFile(path, false)
	status := capturedStdout(t, func() { runQueueTrust(cfg, project, "status") })
	if !strings.Contains(status, T("queue.verifyWaits", "make check")) {
		t.Fatalf("queue status of an untrusted file does not say its check runs only once it is trusted:\n%s", status)
	}
	granted := capturedStdout(t, func() { runQueueTrust(cfg, project, "trust") })
	if !strings.Contains(granted, T("queue.verifyFromFile", "make check")) {
		t.Fatalf("noctis queue trust does not show the check command the file makes it run:\n%s", granted)
	}
	section(cfg, "queue")["verifyCommand"] = "go test ./..."
	writeQueueFile(t, project, "# q\n"+fileVerifyItems)
	trustQueueFile(path, true)
	status = capturedStdout(t, func() { runQueueTrust(cfg, project, "status") })
	if !strings.Contains(status, T("queue.verifyFromConfig", "go test ./...", "queue.verifyCommand")) {
		t.Fatalf("queue status does not name queue.verifyCommand as the check:\n%s", status)
	}
}
