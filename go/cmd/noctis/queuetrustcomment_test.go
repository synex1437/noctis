package main

import (
	"testing"
	"time"
)

// A shell comment is never run, so words after an unquoted "#" that starts a
// word (and a PowerShell <# ... #> block) must not be read as a call to the
// noctis binary.
func TestATopLevelCommentDoesNotTriggerTheQueueTrustGuard(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n")
	for _, call := range []struct{ tool, command string }{
		{"Bash", `git status # the user later types noctis queue trust`},
		{"Bash", `git status # noctis queue trust`},
		{"Bash", `ls # it's the user who runs noctis queue trust`},
		{"Bash", `echo done  #noctis queue trust`},
		{"PowerShell", `git status # noctis queue trust`},
		{"PowerShell", `Get-ChildItem <# noctis queue trust #>`},
	} {
		if run := runHostHook(t, "claude", shellToolCall("q5a", project, call.tool, call.command), "q5a", 10*time.Second); run.answer != nil {
			t.Errorf("the %s call %q only mentions noctis queue trust in a comment, and noctis answered it: %v", call.tool, call.command, run.answer)
		}
	}
}

// Dropping the comment must not lose a real call that sits beside one: a call
// before the "#" on the same line, or on another line after the comment ends.
func TestARealQueueTrustCallBesideACommentIsStillDenied(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n")
	for _, call := range []struct{ tool, command string }{
		{"Bash", "git status # c\nnoctis queue trust"},
		{"Bash", `git status; noctis queue trust # the user asked`},
		{"Bash", "ls # comment\nnoctis queue trust # and another"},
		{"PowerShell", "git status # c\nnoctis queue trust"},
		{"PowerShell", `git status; noctis queue trust # the user asked`},
	} {
		run := runHostHook(t, "claude", shellToolCall("q5b", project, call.tool, call.command), "q5b", 10*time.Second)
		if permissionOf(run.answer) != "deny" || getString(run.answer, "systemMessage") != T("queue.trustByModel", pluginName) {
			t.Errorf("Claude's %s call %q runs noctis queue trust beside a comment, and noctis did not deny it: %v", call.tool, call.command, run.answer)
		}
	}
}
