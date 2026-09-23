//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func echoingCmd(t *testing.T, folder, name string) (script, echoed string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), folder)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script, echoed = filepath.Join(dir, name), filepath.Join(dir, "echoed.txt")
	if err := os.WriteFile(script, []byte("@>\"%NOCTIS_TEST_ECHOED%\" echo %*\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOCTIS_TEST_ECHOED", echoed)
	return script, echoed
}

func echoedArguments(t *testing.T, script, echoed string) string {
	t.Helper()
	content, err := os.ReadFile(echoed)
	if err != nil {
		t.Fatalf("cmd.exe never ran %s: %v", script, err)
	}
	return strings.TrimRight(string(content), "\r\n")
}

func TestAClaudeCmdInAFolderWithASpaceIsHandedItsArgumentsAsQuoted(t *testing.T) {
	claude, echoed := echoingCmd(t, "npm global", "claude.cmd")
	arguments := []string{"-p", "--resume", "0b7e41c2", "--effort", "", "carry on with the parser's tests"}
	if err := claudeCommand(claude, arguments).Run(); err != nil {
		t.Fatalf("claude.cmd in a folder with a space did not run: %v", err)
	}
	quoted := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		quoted = append(quoted, windowsQuote(argument))
	}
	if got, want := echoedArguments(t, claude, echoed), strings.Join(quoted, " "); got != want {
		t.Fatalf("claude.cmd was handed %s, want %s", got, want)
	}
}

func TestAStatusLineChainThatQuotesItsProgramRuns(t *testing.T) {
	script, echoed := echoingCmd(t, "status line", "line.cmd")
	runChain(`"`+script+`" first "second arg"`, nil)
	if got := echoedArguments(t, script, echoed); got != `first "second arg"` {
		t.Fatalf("the chained status line was handed %s, want first \"second arg\"", got)
	}
}
