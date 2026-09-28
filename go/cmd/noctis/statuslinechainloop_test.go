//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// u1ChainLab is a status line of the user's own that shows noctis's line among its own parts:
// a script that calls `noctis statusline` (this test binary as noctis), chained by noctis. Each
// run of the script writes a line to runs and its shell's PID and that of the noctis it starts to
// pids, then runs tail. It starts no noctis once the stop file exists or after 10 runs, so that a
// chain that calls itself ends on its own.
type u1ChainLab struct {
	box              leanBox
	pids, runs, stop string
}

func newU1ChainLab(t *testing.T, tail string) u1ChainLab {
	t.Helper()
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	lab := u1ChainLab{box: newLeanBox(t), pids: filepath.Join(dir, "pids"), runs: filepath.Join(dir, "runs"), stop: filepath.Join(dir, "stop")}
	quote := func(text string) string { return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'" }
	script := filepath.Join(dir, "my status.sh")
	cliWrite(t, script, []byte(strings.Join([]string{
		"[ -e " + quote(lab.stop) + " ] && exit 0",
		"echo $$ >> " + quote(lab.pids),
		"echo run >> " + quote(lab.runs),
		"[ \"$(wc -l < " + quote(lab.runs) + ")\" -gt 10 ] && exit 0",
		"echo 'my own bar'",
		testAsNoctis + "=1 sh -c 'echo $$ >> \"$1\"; exec \"$2\" statusline' noctis " + quote(lab.pids) + " " + quote(executable),
		tail,
		"",
	}, "\n")))
	cliWrite(t, filepath.Join(lab.box.account, pluginName, "config.json"), marshalPretty(object{"statusline": object{"chainCommand": "sh " + quote(script)}}))
	// Whatever a chain that calls itself still runs when the test ends would write into folders
	// that are being removed.
	t.Cleanup(func() {
		cliWrite(t, lab.stop, []byte("stop"))
		for until := time.Now().Add(30 * time.Second); len(lab.alive()) > 0 && time.Now().Before(until); time.Sleep(50 * time.Millisecond) {
		}
	})
	return lab
}

func (lab u1ChainLab) alive() []int {
	content, _ := os.ReadFile(lab.pids)
	alive := []int{}
	for _, field := range strings.Fields(string(content)) {
		if pid, err := strconv.Atoi(field); err == nil && processAlive(pid) {
			alive = append(alive, pid)
		}
	}
	return alive
}

func TestAChainedStatusLineThatCallsNoctisRunsOnceAndLeavesNoProcessBehind(t *testing.T) {
	for _, chain := range []struct {
		name, tail string
		finishes   bool
	}{
		{"a chain that finishes", "", true},
		{"a chain past its time limit", "sleep 6", false},
	} {
		lab := newU1ChainLab(t, chain.tail)
		payload := `{"session_id":"u1","model":{"id":"claude-opus-5-5","display_name":"Opus 5.5"},"cwd":"` + forwardSlashes(lab.box.home) + `","context_window":{"used_percentage":40}}`

		run := lab.box.run(t, payload, "statusline")

		alive := lab.alive()
		if run.code != 0 {
			t.Fatalf("%s: the status line failed:\n%s", chain.name, run)
		}
		content, _ := os.ReadFile(lab.runs)
		if runs := strings.Count(string(content), "\n"); runs != 1 {
			t.Fatalf("%s: one status line refresh ran the chained script %d times: the noctis status line the script calls ran the chain again, which called noctis again (%d of their processes still ran after the refresh returned)", chain.name, runs, len(alive))
		}
		if len(alive) > 0 {
			t.Fatalf("%s: the status line refresh returned while %d processes of its chain still ran: %v", chain.name, len(alive), alive)
		}
		if chain.finishes && (!strings.Contains(run.stdout, "my own bar") || !strings.Contains(run.stdout, "Opus 5.5")) {
			t.Fatalf("%s: the chained status line's output, or noctis's own line, is missing:\n%s", chain.name, run)
		}
	}
}

// A runner the noctis in a chain starts outlives the chain and may relaunch a session, whose own
// status line must still run its chain.
func TestADetachedNoctisStartedInsideAStatusLineChainDoesNotCarryItsMarker(t *testing.T) {
	sandboxFiles(t)
	dump := filepath.Join(t.TempDir(), "environ")
	t.Setenv(statuslineChainEnv, "1")
	t.Setenv("NOCTIS_TEST_MAIN_ARGS", `["--version"]`)
	t.Setenv(environAfterMain, dump)

	child := startDetached([]string{"--version"})
	if child == nil {
		t.Fatal("no detached noctis was started")
	}
	if _, err := child.Wait(); err != nil {
		t.Fatal(err)
	}

	environ, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range strings.Split(string(environ), "\n") {
		if strings.HasPrefix(entry, statuslineChainEnv+"=") {
			t.Fatalf("a detached noctis inherited %s: a session it relaunches would show no chained status line", entry)
		}
	}
}
