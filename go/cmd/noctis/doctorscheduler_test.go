package main

import (
	"strings"
	"testing"
)

func doctorSchedulerLines(t *testing.T, host string) []string {
	t.Helper()
	previousHost, previousLocale := activeHost, locale
	t.Cleanup(func() { activeHost, locale = previousHost, previousLocale })
	activeHost, locale = host, "en"
	doctorIssues = 0
	lines := doctorLines(object{})
	for index, line := range lines {
		if strings.Contains(line, "scheduler backend:") {
			found := []string{line}
			if index+1 < len(lines) && strings.HasPrefix(lines[index+1], "    ") {
				found = append(found, lines[index+1])
			}
			return found
		}
	}
	t.Fatalf("the %s doctor has no scheduler line:\n%s", host, strings.Join(lines, "\n"))
	return nil
}

func TestEveryDoctorCallsTheSleeperSchedulerNormal(t *testing.T) {
	sandboxFiles(t)
	scheduleBackendOverride = "sleeper"
	t.Cleanup(func() { scheduleBackendOverride = "" })

	claude := doctorSchedulerLines(t, "claude")
	if !strings.HasPrefix(claude[0], "OK") || len(claude) != 2 {
		t.Fatalf("the Claude doctor no longer calls the sleeper normal: %q", claude)
	}
	for _, host := range []string{"codex", "antigravity", "droid", "copilot"} {
		lines := doctorSchedulerLines(t, host)
		if strings.Join(lines, "\n") != strings.Join(claude, "\n") {
			t.Errorf("the %s doctor reports the sleeper as %q, the Claude doctor as %q", host, lines, claude)
		}
	}
}
