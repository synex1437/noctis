package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// q4ShellForm is a shell call that hands the noctis binary queue trust (or
// state-write) through quoting, escapes, expansions, comments or
// here-documents the way the shell reads them. inBash marks the ones the bash
// check below runs for real.
type q4ShellForm struct {
	tool, command, message string
	inBash                 bool
}

var q4ShellForms = []q4ShellForm{
	{"Bash", `$'noctis' queue trust`, "queue.trustByModel", true},
	{"Bash", `noctis $'queue' $'trust'`, "queue.trustByModel", true},
	{"Bash", `$"noctis" queue trust`, "queue.trustByModel", true},
	{"Bash", `noctis queue $"trust"`, "queue.trustByModel", true},
	{"Bash", `$'\x6eoctis' queue trust`, "queue.trustByModel", true},
	{"Bash", `noctis queue $'\x74rust'`, "queue.trustByModel", true},
	{"Bash", `noctis queue $'\164rust'`, "queue.trustByModel", true},
	{"Bash", `echo $'\'' ; noctis queue trust ; echo $'\''`, "queue.trustByModel", true},
	{"Bash", "noc\\\ntis queue trust", "queue.trustByModel", true},
	{"Bash", "noctis \\\nqueue trust", "queue.trustByModel", true},
	{"Bash", "noctis queue tr\\\nust", "queue.trustByModel", true},
	{"Bash", `{noctis,queue,trust}`, "queue.trustByModel", true},
	{"Bash", `noctis {queue,trust}`, "queue.trustByModel", true},
	{"Bash", `noctis queue {trust,}`, "queue.trustByModel", true},
	{"Bash", `noctis queue tru{s,}t`, "queue.trustByModel", true},
	{"Bash", `noctis queue tru{s..s}t`, "queue.trustByModel", true},
	{"Bash", `{noct,--x=}is queue trust`, "queue.trustByModel", true},
	{"Bash", `"$CLAUDE_PLUGIN_ROOT"/bin/noct?s queue trust`, "queue.trustByModel", true},
	{"Bash", `./bin/n*s queue trust`, "queue.trustByModel", true},
	{"Bash", `./bin/[n]octis queue trust`, "queue.trustByModel", true},
	{"Bash", `noctis queue t[r]ust`, "queue.trustByModel", true},
	{"Bash", "cat /dev/null # <<'EOF'\nnoctis queue trust\nEOF\n", "queue.trustByModel", true},
	{"Bash", `echo \' ; noctis queue trust ; echo \'`, "queue.trustByModel", true},
	{"Bash", `echo \" ; noctis queue trust ; echo \"`, "queue.trustByModel", true},
	{"Bash", "cat \"<<EOF\"\" \"\nnoctis queue trust\nEOF\n", "queue.trustByModel", true},
	{"Bash", "cat <<$'EOF' /dev/null\nx\nEOF\nnoctis queue trust\n$EOF\n", "queue.trustByModel", true},
	{"Bash", "cat /dev/null $[1<<EOF]\nnoctis queue trust\nEOF]\n", "queue.trustByModel", true},
	{"Bash", "wc -l <<EOF\nit's\nEOF\nnoctis queue trust\n", "queue.trustByModel", true},
	{"Bash", "git commit --dry-run -m \"$(cat <<'EOF'\nx\nEOF)\"\nnoctis queue trust\nEOF\n", "queue.trustByModel", false},
	{"Bash", `noctis queue trust>/dev/null`, "queue.trustByModel", true},
	{"Bash", `noctis>/dev/null queue trust`, "queue.trustByModel", true},
	{"Bash", `noctis queue 2>/dev/null trust`, "queue.trustByModel", true},
	{"Bash", `$'noctis' state-write </dev/null`, "queue.stateWriteByModel", true},
	{"Bash", `noctis state-wr{i,}te </dev/null`, "queue.stateWriteByModel", true},
	{"Bash", "noctis state-\\\nwrite </dev/null", "queue.stateWriteByModel", true},
	{"PowerShell", `noctis --% queue trust`, "queue.trustByModel", false},
	{"PowerShell", `& noctis.exe --% queue trust`, "queue.trustByModel", false},
	{"PowerShell", `noctis queue,trust`, "queue.trustByModel", false},
	{"PowerShell", `noctis 'queue','trust'`, "queue.trustByModel", false},
	{"PowerShell", `noctis @('queue','trust')`, "queue.trustByModel", false},
	{"PowerShell", `noctis ('queue','trust')`, "queue.trustByModel", false},
	{"PowerShell", `noctis "queue"trust`, "queue.trustByModel", false},
	{"PowerShell", `echo "C:\" ; noctis queue trust ; echo "x"`, "queue.trustByModel", false},
	{"PowerShell", "$x = @'\nit's\n'@\nnoctis queue trust\n", "queue.trustByModel", false},
	{"PowerShell", "& \u2018noctis\u2019 queue trust", "queue.trustByModel", false},
	{"PowerShell", "noctis \u201cqueue\u201d trust", "queue.trustByModel", false},
	{"PowerShell", "noctis\u00a0queue\u00a0trust", "queue.trustByModel", false},
	{"PowerShell", `noctis queue trust>$null`, "queue.trustByModel", false},
	{"PowerShell", `<# it's #> noctis queue trust`, "queue.trustByModel", false},
	{"PowerShell", `Get-Content doc.json | noctis --% state-write`, "queue.stateWriteByModel", false},
	{"PowerShell", `noctis @('state-write')`, "queue.stateWriteByModel", false},
}

func TestClaudeCannotRunQueueTrustThroughShellQuotingExpansionsOrComments(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n")
	for _, call := range q4ShellForms {
		run := runHostHook(t, "claude", shellToolCall("q4a", project, call.tool, call.command), "q4a", 10*time.Second)
		if permissionOf(run.answer) != "deny" || getString(run.answer, "systemMessage") != T(call.message, pluginName) {
			t.Errorf("Claude's %s call %q hands noctis %s, and noctis did not deny it: %v", call.tool, call.command, call.message, run.answer)
		}
	}
}

func TestShellCallsThatOnlyQuoteBracesGlobsOrTheCommandStillPass(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	for _, call := range []struct{ tool, command string }{
		{"Bash", `echo 'noctis {queue,trust}'`},
		{"Bash", `echo "noctis queue tr{u,}st"`},
		{"Bash", `printf '%s\n' $'noctis queue trust'`},
		{"Bash", `ls *.go; echo trust`},
		{"Bash", `grep -n "queue trust" docs/*.md`},
		{"Bash", `noctis queue status 2>/dev/null`},
		{"Bash", `noctis queue {status,untrust}`},
		{"Bash", "noctis queue \\\nstatus"},
		{"Bash", "cat <<'EOF' > notes.md\nRun noctis queue trust yourself.\nEOF\n"},
		{"Bash", "cat > notes.md <<EOF\nit's the user who runs noctis queue trust\nEOF\n"},
		{"Bash", "git commit -F - <<'EOF'\nguard: noctis queue trust stays with the user\nEOF\n"},
		{"Bash", `for f in docs/*.md; do grep -c "noctis queue trust" "$f"; done`},
		{"PowerShell", `Write-Output 'noctis --% queue trust'`},
		{"PowerShell", `Get-ChildItem *.md | Select-String 'noctis queue trust'`},
		{"PowerShell", `Write-Host "noctis queue trust", 'is typed by the user'`},
		{"PowerShell", "$note = @'\nnoctis queue trust\n'@\nWrite-Output $note"},
		{"PowerShell", `noctis queue status 2>$null`},
	} {
		if run := runHostHook(t, "claude", shellToolCall("q4b", project, call.tool, call.command), "q4b", 10*time.Second); run.answer != nil {
			t.Errorf("the %s call %q does not run noctis queue trust, and noctis answered it: %v", call.tool, call.command, run.answer)
		}
	}
}

// TestBashReallyHandsNoctisQueueTrustInEachListedForm runs the bash forms
// above with a stand-in noctis on PATH, so the table lists only calls that
// bash really hands queue trust or state-write.
func TestBashReallyHandsNoctisQueueTrustInEachListedForm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on this machine")
	}
	version, err := exec.Command(bash, "-c", "echo ${BASH_VERSINFO[0]}").Output()
	if major, _ := strconv.Atoi(strings.TrimSpace(string(version))); err != nil || major < 4 {
		t.Skip("bash older than 4 reads some of these forms differently")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(root, "argv")
	stub := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > \"$Q4_ARGV\"\n"
	if err := os.WriteFile(filepath.Join(root, "bin", "noctis"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "trust"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, form := range q4ShellForms {
		if !form.inBash {
			continue
		}
		os.Remove(record)
		cmd := exec.Command(bash, "-c", form.command)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"), "CLAUDE_PLUGIN_ROOT="+root, "Q4_ARGV="+record)
		cmd.Run()
		argv, err := os.ReadFile(record)
		if err != nil {
			t.Errorf("bash never ran noctis for %q", form.command)
			continue
		}
		want := []string{"queue", "trust"}
		if form.message == "queue.stateWriteByModel" {
			want = []string{"state-write"}
		}
		if got := parseArgs(strings.Fields(string(argv))).positional; len(got) < len(want) || strings.Join(got[:len(want)], " ") != strings.Join(want, " ") {
			t.Errorf("bash handed noctis %q for %q, not %q", got, form.command, want)
		}
	}
}

// FuzzQueueTrustGuardReadsShellCommands checks that the guard only ever reads a
// command and never panics, however malformed the quoting, expansions or
// here-documents are.
func FuzzQueueTrustGuardReadsShellCommands(f *testing.F) {
	for _, form := range q4ShellForms {
		f.Add(form.command, form.tool == "PowerShell")
	}
	for _, seed := range []string{
		"noctis queue trust", "", "$(", "`", "<<", "<<-", "{a,b", "{1..9}", "@'", "$'\\",
		"cat <<EOF\n", "noctis ${", "&(", "((", "$[", "'", "\"", "\\", "noctis {q,",
		"a{b..", "[a-", "<#", "@(", "noctis --%", "$'\\x", "noctis\u00a0queue",
	} {
		f.Add(seed, false)
		f.Add(seed, true)
	}
	f.Fuzz(func(t *testing.T, command string, powershell bool) {
		deniedNoctisCommand(command, powershell)
		readShellCommand(command, cmdDialect, true)
	})
}
