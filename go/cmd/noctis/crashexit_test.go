package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const crashChildArgs = "NOCTIS_CRASH_TEST_ARGS"

func TestCLIChildCrashesInTheCommandItWasHanded(t *testing.T) {
	raw := os.Getenv(crashChildArgs)
	if raw == "" {
		t.Skip("the child side of the crash tests")
	}
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		t.Fatal(err)
	}
	ported[argv[0]] = func() { panic("planted crash") }
	os.Args = append([]string{os.Args[0]}, argv...)
	main()
}

func crashedRun(t *testing.T, argv ...string) cliRun {
	t.Helper()
	encoded, err := json.Marshal(argv)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	child := exec.Command(executable, "-test.run=^TestCLIChildCrashesInTheCommandItWasHanded$")
	child.Dir = home
	child.Env = append(os.Environ(), crashChildArgs+"="+string(encoded), "NOCTIS_LANG=en", "NOCTIS_HOST=", "NOCTIS_NO_WATCHER=1", "NOCTIS_NO_TASKS=1",
		"CLAUDE_CONFIG_DIR=", "CLAUDE_PLUGIN_ROOT=", "NOCTIS_PLUGIN_ROOT=", "HOME="+home, "USERPROFILE="+home)
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	err = child.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return cliRun{0, stdout.String(), stderr.String()}
	case errors.As(err, &exit):
		return cliRun{exit.ExitCode(), stdout.String(), stderr.String()}
	}
	t.Fatalf("the crashing child did not run: %v", err)
	return cliRun{}
}

func TestACrashInACommandPeopleRunExitsOneAndSaysWhereTheDetailsAre(t *testing.T) {
	for _, name := range []string{"check", "setup", "install", "doctor", "status", "cancel", "off"} {
		t.Run(name, func(t *testing.T) {
			run := crashedRun(t, name)

			if run.code != 1 {
				t.Fatalf("a crash in %s exited %d; callers that gate on it carry on as if it worked:\n%s", name, run.code, run)
			}
			if !strings.Contains(run.stderr, "noctis "+name) || !strings.Contains(run.stderr, "planted crash") || !strings.Contains(run.stderr, "errors.log") {
				t.Fatalf("the crash of %s is not reported with its reason and where the details are:\n%s", name, run)
			}
		})
	}
}

func TestACrashInWhatAHostRunsNeverFailsTheHost(t *testing.T) {
	for _, name := range []string{"hook", "statusline", "ensure", "resume", "sleeper", "webhook", "selftest-mark", "release-check"} {
		t.Run(name, func(t *testing.T) {
			run := crashedRun(t, name, "--sid", "s1")

			if run.code != 0 {
				t.Fatalf("a crash in %s exited %d; the host would report a failed hook or task:\n%s", name, run.code, run)
			}
		})
	}
	if run := crashedRun(t, "statusline"); !strings.Contains(run.stdout, "noctis error") {
		t.Fatalf("a crashed status line prints no fallback:\n%s", run)
	}
}
