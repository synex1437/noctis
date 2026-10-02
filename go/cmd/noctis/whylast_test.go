package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWhyTakesAHugeOrInfiniteLastWithoutPanicking(t *testing.T) {
	home := t.TempDir()
	account := filepath.Join(home, ".claude", pluginName)
	lines := []string{}
	for index := 1; index <= 25; index++ {
		lines = append(lines, fmt.Sprintf(`{"at":%d,"sid":"s1","event":"Stop","action":"allow-stop","reason":"x3 entry %d"}`, 1000+index, index))
	}
	cliWrite(t, filepath.Join(account, "decisions.jsonl"), []byte(strings.Join(lines, "\n")+"\n"))
	for _, last := range []struct {
		value string
		want  int
	}{{"1e19", 25}, {"9.3e18", 25}, {"1e6", 25}, {"inf", 20}, {"+Infinity", 20}, {"NaN", 20}, {"1e400", 20}, {"3", 3}} {
		run := startNoctisCLIAt(t, home, "", nil, "why", "--last", last.value, "--json")()
		got := []string{}
		for _, line := range strings.Split(run.stdout, "\n") {
			if strings.Contains(line, "x3 entry") {
				got = append(got, line)
			}
		}
		if run.code != 0 || len(got) != last.want || !strings.Contains(got[len(got)-1], "x3 entry 25") {
			t.Errorf("noctis why --last %s must list the last %d entries:\n%s", last.value, last.want, run)
		}
	}
	if logged, _ := os.ReadFile(filepath.Join(account, "errors.log")); len(logged) > 0 {
		t.Errorf("noctis why --last left an error behind for the doctor:\n%s", logged)
	}
}

func TestWhyJSONWithNoDecisionsPrintsNoLine(t *testing.T) {
	home := t.TempDir()
	if run := startNoctisCLIAt(t, home, "", nil, "why", "--json")(); run.code != 0 || strings.TrimSpace(strings.TrimSuffix(run.stdout, "PASS\n")) != "" {
		t.Fatalf("noctis why --json with no decisions recorded printed what a JSON reader cannot parse:\n%s", run)
	}
	if run := startNoctisCLIAt(t, home, "", nil, "why")(); run.code != 0 || !strings.Contains(run.stdout, "No decisions recorded yet.") {
		t.Fatalf("noctis why with no decisions recorded no longer says so:\n%s", run)
	}
}

func whyOutput(t *testing.T, argv ...string) string {
	t.Helper()
	defer func(previous parsedArgs) { args = previous }(args)
	args = parseArgs(append([]string{"why"}, argv...))
	return capturedStdout(t, runWhy)
}

func TestAJournalEntryKeepsItsOwnFieldsWhateverItsExtraFieldsSay(t *testing.T) {
	sandboxFiles(t)
	before := float64(nowSec())
	journal("s1", "Stop", "arm-queue-wake", "TASKS.md", object{"at": before + 7200, "sid": "s2", "event": "cli", "action": "note", "reason": "kept the old parser", "method": "sleeper"})
	lines := tailFileLines(files.decisions, 10)
	entry := object{}
	if len(lines) != 1 || jsonUnmarshalObject([]byte(lines[0]), &entry) != nil {
		t.Fatalf("the journal holds %q, want one entry", lines)
	}
	if at := numberOr(entry, "at", 0); at < before || at > float64(nowSec()) || getString(entry, "sid") != "s1" || getString(entry, "event") != "Stop" || getString(entry, "action") != "arm-queue-wake" || getString(entry, "reason") != "TASKS.md" || getString(entry, "method") != "sleeper" {
		t.Fatalf("an extra field replaced one the journal entry sets itself, or an extra field of its own was lost: %s", lines[0])
	}
}

func TestTailFileLinesTakesANegativeCountAsNoLimit(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x3.log")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("tailFileLines panicked on a negative count: %v", recovered)
		}
	}()
	if got := tailFileLines(file, math.MinInt); len(got) != 3 {
		t.Errorf("a count that overflowed to a negative number must not cut the lines: %q", got)
	}
	if got := tailFileLines(file, 2); strings.Join(got, ",") != "two,three" {
		t.Errorf("a count of 2 must keep the last two lines: %q", got)
	}
}

func whyJSONLines(t *testing.T, home, mark string, argv ...string) []string {
	t.Helper()
	run := startNoctisCLIAt(t, home, "", nil, append([]string{"why", "--json"}, argv...)...)()
	if run.code != 0 {
		t.Fatalf("noctis why failed:\n%s", run)
	}
	got := []string{}
	for _, line := range strings.Split(run.stdout, "\n") {
		if strings.Contains(line, mark) {
			got = append(got, line)
		}
	}
	return got
}

func TestWhyLastListsAsManyDecisionsAsAskedFromALongJournal(t *testing.T) {
	home := t.TempDir()
	lines := []string{}
	for index := 1; index <= 1000; index++ {
		lines = append(lines, fmt.Sprintf(`{"at":%d,"sid":"0123456789abcdef","event":"PostToolBatch","action":"pause","reason":"5h 93%% (threshold 92%%) x4 entry %d.","window":"five_hour","five":93}`, 1000+index, index))
	}
	cliWrite(t, filepath.Join(home, ".claude", pluginName, "decisions.jsonl"), []byte(strings.Join(lines, "\n")+"\n"))
	got := whyJSONLines(t, home, "x4 entry", "--last", "500")
	if len(got) != 500 || !strings.Contains(got[0], "x4 entry 501.") || !strings.Contains(got[499], "x4 entry 1000.") {
		t.Fatalf("noctis why --last 500 listed %d decisions of the 1000 the journal holds, not the last 500", len(got))
	}
}

func TestRightAfterTheJournalIsMovedAsideWhyStillListsTheDecisionsBeforeIt(t *testing.T) {
	home := t.TempDir()
	account := filepath.Join(home, ".claude", pluginName)
	older := []string{}
	for index := 1; index <= 30; index++ {
		older = append(older, fmt.Sprintf(`{"at":%d,"sid":"s1","event":"Stop","action":"allow-stop","reason":"x4 older %d."}`, 1000+index, index))
	}
	cliWrite(t, filepath.Join(account, "decisions.jsonl.1"), []byte(strings.Join(older, "\n")+"\n"))
	cliWrite(t, filepath.Join(account, "decisions.jsonl"), []byte(`{"at":2000,"sid":"s1","event":"Stop","action":"allow-stop","reason":"x4 newer 1."}`+"\n"+`{"at":2001,"sid":"s1","event":"Stop","action":"allow-stop","reason":"x4 newer 2."}`+"\n"))
	got := whyJSONLines(t, home, "x4 ")
	if len(got) != 20 || !strings.Contains(got[0], "x4 older 13.") || !strings.Contains(got[19], "x4 newer 2.") {
		t.Fatalf("right after decisions.jsonl was moved aside to decisions.jsonl.1, noctis why listed %d decisions, not the last 20:\n%s", len(got), strings.Join(got, "\n"))
	}
}
