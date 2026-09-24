package main

import (
	"os"
	"strings"
	"testing"
)

func doctorRun(t *testing.T, host string, cfg object) []string {
	t.Helper()
	previousHost, previousLocale := activeHost, locale
	t.Cleanup(func() { activeHost, locale = previousHost, previousLocale })
	activeHost, locale = host, "en"
	doctorIssues = 0
	return doctorLines(cfg)
}

func remedyFor(t *testing.T, lines []string, marker string) (string, string) {
	t.Helper()
	for index, line := range lines {
		if strings.HasPrefix(line, "!!") && strings.Contains(line, marker) {
			if index+1 < len(lines) && strings.Contains(lines[index+1], "fix:") {
				return line, lines[index+1]
			}
			t.Errorf("%q says what is wrong but not what to run:\n%s", line, strings.Join(lines, "\n"))
			return line, ""
		}
	}
	t.Errorf("no failure line mentions %q:\n%s", marker, strings.Join(lines, "\n"))
	return "", ""
}

func doctorRemedySandbox(t *testing.T) {
	t.Helper()
	sandboxFiles(t)
	previousRoot := files.pluginRoot
	t.Cleanup(func() { files.pluginRoot = previousRoot })
	files.pluginRoot = t.TempDir()
	t.Setenv("PATH", t.TempDir())
	previousLookup := claudeLookup
	t.Cleanup(func() { claudeLookup = previousLookup })
	claudeLookup = func() string { return "" }
}

func TestEveryDoctorFailureSaysWhatToRun(t *testing.T) {
	t.Run("claude", func(t *testing.T) {
		doctorRemedySandbox(t)
		until := float64(nowSec() + 3600)
		updateState(func(state object) { state["disabledUntil"] = until })
		cliWrite(t, files.settings, []byte(`{"model": "opus",`))

		lines := doctorRun(t, "claude", object{"models": object{"effort": "max"}})

		if _, fix := remedyFor(t, lines, formatTime(until)); fix != "" && !strings.Contains(fix, "noctis on") {
			t.Errorf("the paused guard's remedy does not name noctis on: %q", fix)
		}
		if _, fix := remedyFor(t, lines, "claude:"); fix != "" && !strings.Contains(fix, "PATH") {
			t.Errorf("a missing claude's remedy does not mention PATH: %q", fix)
		}
		if _, fix := remedyFor(t, lines, "settings.json:"); fix != "" && !strings.Contains(fix, files.settings) {
			t.Errorf("a broken settings.json's remedy does not name the file: %q", fix)
		}
		remedyFor(t, lines, "plugin location:")
		remedyFor(t, lines, "agents/lite.md")
	})
	t.Run("settings.json that cannot be opened", func(t *testing.T) {
		doctorRemedySandbox(t)
		if err := os.MkdirAll(files.settings, 0o755); err != nil {
			t.Fatal(err)
		}

		lines := doctorRun(t, "claude", object{})

		if _, fix := remedyFor(t, lines, "settings.json:"); fix != "" && (!strings.Contains(fix, files.settings) || strings.Contains(fix, "JSON")) {
			t.Errorf("the remedy for a settings.json that cannot be opened asks to fix a JSON error, or does not name the file: %q", fix)
		}
	})
	t.Run("effort", func(t *testing.T) {
		doctorRemedySandbox(t)
		cliWrite(t, files.settings, []byte(`{"model": "opus", "env": {"CLAUDE_CODE_EFFORT_LEVEL": "medium"}}`))

		lines := doctorRun(t, "claude", object{"models": object{"effort": "max"}})

		line, fix := remedyFor(t, lines, "CLAUDE_CODE_EFFORT_LEVEL")
		if !strings.Contains(line, "medium") || !strings.Contains(line, "max") {
			t.Errorf("the effort line hides what config.json expects: %q", line)
		}
		if fix != "" && (!strings.Contains(fix, "max") || strings.Contains(fix, "missing")) {
			t.Errorf("the effort remedy calls a mismatch missing settings or leaves out the level: %q", fix)
		}
	})
	t.Run("codex", func(t *testing.T) {
		doctorRemedySandbox(t)

		lines := doctorRun(t, "codex", object{})

		if _, fix := remedyFor(t, lines, "Codex"); fix != "" && !strings.Contains(fix, "PATH") {
			t.Errorf("a missing codex's remedy does not mention PATH: %q", fix)
		}
		for _, marker := range []string{"hooks wired:", "config:"} {
			if _, fix := remedyFor(t, lines, marker); fix != "" && !strings.Contains(fix, "noctis setup --host codex") {
				t.Errorf("%s: the remedy does not name the setup that wires it: %q", marker, fix)
			}
		}
	})
}

func TestTheOtherToolsDoctorsNameWhatTheirSelfCheckNames(t *testing.T) {
	shipped := object{"session5h": float64(92), "weeklyAll": float64(89), "weeklyFable": float64(95)}
	cases := []struct {
		name, marker, remedy string
		cfg                  object
	}{
		{"threshold out of range", "session5h", "between 1 and 100", object{"thresholds": shipped, "thresholdsRepaired": "session5h"}},
		{"threshold switched off", "weeklyAll", "between 1 and 100", object{"thresholds": object{"session5h": float64(92), "weeklyAll": false, "weeklyFable": float64(95)}}},
		{"paid credits allowed", "ALLOWED", "credits.allowPaid", object{"thresholds": shipped, "credits": object{"allowPaid": true}}},
	}
	for _, host := range []string{"codex", "antigravity", "droid", "copilot"} {
		for _, testCase := range cases {
			t.Run(host+"/"+testCase.name, func(t *testing.T) {
				doctorRemedySandbox(t)
				previousHost := activeHost
				activeHost = host
				issues := strings.Join(selfCheckIssues(testCase.cfg), "; ")
				activeHost = previousHost

				lines := doctorRun(t, host, testCase.cfg)

				if !hostOf(host).limits {
					if strings.Contains(issues, testCase.marker) {
						t.Errorf("%s has no limit guard, yet its session self-check names %s: %q", host, testCase.marker, issues)
					}
					for _, line := range lines {
						if strings.HasPrefix(line, "!!") && strings.Contains(line, testCase.marker) {
							t.Errorf("%s has no limit guard, yet its doctor fails on %s: %q", host, testCase.marker, line)
						}
					}
					return
				}
				if !strings.Contains(issues, testCase.marker) {
					t.Fatalf("the session self-check on %s does not name %s: %q", host, testCase.marker, issues)
				}
				if _, fix := remedyFor(t, lines, testCase.marker); fix != "" && !strings.Contains(fix, testCase.remedy) {
					t.Errorf("the remedy does not say what to change: %q", fix)
				}
			})
		}
		t.Run(host+"/shipped values", func(t *testing.T) {
			doctorRemedySandbox(t)
			for _, line := range doctorRun(t, host, object{"thresholds": shipped}) {
				if strings.HasPrefix(line, "!!") && (strings.Contains(line, "threshold") || strings.Contains(line, "credits")) {
					t.Errorf("the shipped thresholds fail the %s doctor: %q", host, line)
				}
			}
		})
	}
}

func TestTheUsageAndRoleLinesSayWhatToRun(t *testing.T) {
	for _, host := range []string{"codex", "antigravity"} {
		t.Run(host, func(t *testing.T) {
			doctorRemedySandbox(t)

			lines := doctorRun(t, host, object{})

			if _, fix := remedyFor(t, lines, "usage.json:"); fix != "" && !strings.Contains(fix, hostOf(host).display) {
				t.Errorf("the usage remedy does not name the tool to start: %q", fix)
			}
		})
	}
	t.Run("roles", func(t *testing.T) {
		doctorRemedySandbox(t)

		lines := doctorRun(t, "claude", object{"roles": object{"code": object{"model": "opsu", "effort": "max"}, "digest": object{"model": "haiku", "effort": "hgih"}}})

		for role, marker := range map[string]string{"code": "opsu", "digest": "hgih"} {
			if _, fix := remedyFor(t, lines, marker); fix != "" && (!strings.Contains(fix, "roles") || !strings.Contains(fix, "config.json") || !strings.Contains(fix, "/noctis:setup")) {
				t.Errorf("the %s role's remedy does not say where to correct it: %q", role, fix)
			}
		}
	})
}
