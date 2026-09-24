package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func runMainWithoutAHome(t *testing.T, work string, argv []string, stdin string) (int, string, string) {
	t.Helper()
	child := exec.Command(os.Args[0])
	child.Dir = work
	child.Env = append(withoutEnv(os.Environ(), "HOME", "USERPROFILE", "home", claudeConfigEnv, "NOCTIS_PLUGIN_ROOT", "CLAUDE_PLUGIN_ROOT", "NOCTIS_LANG"),
		"NOCTIS_TEST_MAIN_ARGS="+string(marshalCompact(argv)), "NOCTIS_LANG=en")
	child.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	err := child.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, stdout.String(), stderr.String()
}

func TestWithoutAHomeFolderNoctisSaysSoInsteadOfUsingTheWorkingFolder(t *testing.T) {
	work := t.TempDir()
	if code, _, stderr := runMainWithoutAHome(t, work, []string{"status"}, ""); code != 1 || !strings.Contains(stderr, "HOME") {
		t.Fatalf("noctis status without a home folder exited %d, stderr %q; want exit 1 naming HOME", code, stderr)
	}
	hook := `{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":"` + strings.ReplaceAll(work, `\`, `\\`) + `","prompt":"fix the parser"}`
	if code, stdout, stderr := runMainWithoutAHome(t, work, []string{"hook"}, hook); code != 0 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("a hook without a home folder must do nothing and exit 0: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if code, stdout, _ := runMainWithoutAHome(t, work, []string{"help"}, ""); code != 0 || stdout == "" {
		t.Fatalf("noctis help without a home folder exited %d with %q", code, stdout)
	}
	if entries, _ := os.ReadDir(work); len(entries) != 0 {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("without a home folder noctis wrote an account into the working folder: %v", names)
	}
}
