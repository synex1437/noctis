package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLines(t *testing.T, file string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStatusCountsTheCompactionsTheLeanModuleTrimmed(t *testing.T) {
	leanSandbox(t)
	t.Setenv("NOCTIS_LANG", "en")
	cfg := loadConfig()
	status := describeState(cfg, readState(), usageView{}, nowSec())
	if !strings.Contains(status, "Compaction   : lean compaction on, compacts early at 70% of the context · no compaction trimmed yet") {
		t.Fatalf("status does not say that nothing was trimmed yet:\n%s", status)
	}
	writeLines(t, filepath.Join(files.guardDir, "compact.log"),
		`{"at":100,"sid":"s1","trigger":"auto","rows":40,"changed":3,"trimmed":1000,"tokensBefore":150000,"tokensAfter":9000}`,
		`{"at":200,"sid":"s1","trigger":"plugin","rows":60,"changed":12,"trimmed":128000,"ctx":75}`,
		`not json`,
		`{"at":300,"sid":"s2","trigger":"manual","rows":5,"changed":1,"trimmed":5}`)
	status = describeState(cfg, readState(), usageView{}, nowSec())
	if !strings.Contains(status, "Compaction   : lean compaction on, compacts early at 70% of the context · compactions trimmed: 3, ~129k characters") {
		t.Fatalf("status does not count the trimmed compactions:\n%s", status)
	}
}

func TestWhyListsTheLeanCompactionsAmongTheDecisions(t *testing.T) {
	leanSandbox(t)
	t.Setenv("NOCTIS_LANG", "en")
	writeLines(t, files.decisions,
		`{"at":100,"sid":"s1","event":"UserPromptSubmit","action":"warn","reason":"5h 90%"}`,
		`{"at":300,"sid":"s1","event":"Stop","action":"allow-stop","reason":"queue finished"}`)
	writeLines(t, filepath.Join(files.guardDir, "compact.log"),
		`{"at":200,"sid":"s1","trigger":"plugin","rows":60,"changed":12,"trimmed":128000,"ctx":75,"tokensBefore":150014,"tokensAfter":8496}`)
	defer func(previous parsedArgs) { args = previous }(args)
	args = parseArgs([]string{"why", "--last", "3"})
	out := strings.Split(strings.TrimSpace(capturedStdout(t, runWhy)), "\n")
	if len(out) != 3 || !strings.Contains(out[0], "warn") || !strings.Contains(out[2], "allow-stop") {
		t.Fatalf("why did not keep the decisions in time order around the compaction:\n%s", strings.Join(out, "\n"))
	}
	for _, want := range []string{"session.compact", "compact", "plugin: 60 rows, 12 changed, 128000 characters trimmed, 150014 → 8496 tokens", "[ctx=75%]"} {
		if !strings.Contains(out[1], want) {
			t.Fatalf("why's compaction line lacks %q:\n%s", want, out[1])
		}
	}
	args = parseArgs([]string{"why", "--last", "2", "--json"})
	raw := strings.Split(strings.TrimSpace(capturedStdout(t, runWhy)), "\n")
	if len(raw) != 2 || !strings.Contains(raw[0], `"action":"compact"`) || !strings.Contains(raw[0], `"trimmed":128000`) || !strings.Contains(raw[1], `"allow-stop"`) {
		t.Fatalf("why --json did not merge the compaction as a journal line:\n%s", strings.Join(raw, "\n"))
	}
}

func TestWhyShowsCompactionsWhenNoDecisionWasMadeYet(t *testing.T) {
	leanSandbox(t)
	t.Setenv("NOCTIS_LANG", "en")
	writeLines(t, filepath.Join(files.guardDir, "compact.log"),
		`{"at":200,"sid":"s1","trigger":"auto","rows":10,"changed":2,"trimmed":4000}`)
	defer func(previous parsedArgs) { args = previous }(args)
	args = parseArgs([]string{"why"})
	out := capturedStdout(t, runWhy)
	if !strings.Contains(out, "auto: 10 rows, 2 changed, 4000 characters trimmed") {
		t.Fatalf("why said nothing about the compaction:\n%s", out)
	}
}
