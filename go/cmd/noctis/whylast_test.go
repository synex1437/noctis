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
