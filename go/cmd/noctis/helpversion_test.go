package main

import (
	"strings"
	"testing"
)

var plumbingForTest = []string{"hook", "statusline", "resume", "sleeper", "release-check", "selftest-mark", "state-write"}

func listedCommands(t *testing.T, stderr string) map[string]bool {
	t.Helper()
	_, list, found := strings.Cut(strings.TrimSpace(stderr), "Commands: ")
	if !found {
		t.Fatalf("the unknown-command error lists no commands:\n%s", stderr)
	}
	listed := map[string]bool{}
	for _, name := range strings.Split(list, ",") {
		listed[strings.TrimSpace(name)] = true
	}
	return listed
}

func TestVersionFlagsPrintTheVersionAndNothingElse(t *testing.T) {
	for _, argv := range [][]string{{"version"}, {"--version"}, {"-v"}, {"-V"}} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			run := runNoctisCLI(t, nil, argv...)

			if run.code != 0 || cliFirstLine(run.stdout) != pluginVersion || strings.Contains(run.stdout, "Account dir") || run.stderr != "" {
				t.Fatalf("want only %q and exit 0:\n%s", pluginVersion, run)
			}
		})
	}
	t.Run("--version without a home folder", func(t *testing.T) {
		run := runNoctisCLI(t, map[string]string{"HOME": "", "USERPROFILE": ""}, "--version")

		if run.code != 0 || cliFirstLine(run.stdout) != pluginVersion {
			t.Fatalf("want %q and exit 0 even with no account to read:\n%s", pluginVersion, run)
		}
	})
}

func TestHelpListsEveryCommandPeopleRunAndNoPlumbing(t *testing.T) {
	run := runNoctisCLI(t, nil, "help")
	if run.code != 0 {
		t.Fatalf("help failed:\n%s", run)
	}
	shown := map[string]bool{}
	for _, line := range strings.Split(run.stdout, "\n") {
		if fields := strings.Fields(line); strings.HasPrefix(line, "  ") && len(fields) > 1 {
			shown[fields[0]] = true
		}
	}
	for _, name := range userCommands() {
		if !shown[name] {
			t.Errorf("help does not list %s:\n%s", name, run.stdout)
		}
	}
	for _, name := range []string{"classify", "schedule-preview"} {
		if !shown[name] {
			t.Errorf("help hides %s, which the reference documents:\n%s", name, run.stdout)
		}
	}
	for _, name := range plumbingForTest {
		if shown[name] {
			t.Errorf("help lists %s, which only the plugin itself runs:\n%s", name, run.stdout)
		}
	}
}

func TestEveryCommandPeopleRunHasAHelpLineInEveryLanguage(t *testing.T) {
	for _, name := range userCommands() {
		key := helpKey(name)
		for lang, table := range catalogTable() {
			if strings.TrimSpace(table[key]) == "" {
				t.Errorf("%s: %s has no help line (%s)", lang, name, key)
			}
		}
	}
	for _, name := range plumbingForTest {
		if _, isCommand := ported[name]; !isCommand {
			t.Errorf("%s is no longer a command; the plumbing list here is stale", name)
		}
		for _, user := range userCommands() {
			if user == name {
				t.Errorf("%s is plumbing but counted as a command people run", name)
			}
		}
	}
}

func TestAnUnknownCommandListsOnlyTheCommandsPeopleRun(t *testing.T) {
	run := runNoctisCLI(t, nil, "statuss")

	if run.code != 1 || !strings.Contains(run.stderr, "statuss") {
		t.Fatalf("want exit 1 naming the word it did not know:\n%s", run)
	}
	listed := listedCommands(t, run.stderr)
	for _, name := range plumbingForTest {
		if listed[name] {
			t.Errorf("the error offers %s, which only the plugin itself runs:\n%s", name, run.stderr)
		}
	}
	for _, name := range []string{"status", "doctor", "classify", "schedule-preview", "help"} {
		if !listed[name] {
			t.Errorf("the error does not offer %s:\n%s", name, run.stderr)
		}
	}
}

func TestTheDoctorsFirstLineCarriesTheVersion(t *testing.T) {
	for _, host := range []string{"claude", "codex", "copilot"} {
		t.Run(host, func(t *testing.T) {
			sandboxFiles(t)
			previousHost, previousLocale := activeHost, locale
			t.Cleanup(func() { activeHost, locale = previousHost, previousLocale })
			activeHost, locale = host, "en"
			doctorIssues = 0

			first := doctorLines(object{})[0]

			if !strings.HasPrefix(first, "OK") || !strings.Contains(first, pluginName+" "+pluginVersion) || !strings.Contains(first, platformName()) {
				t.Fatalf("a pasted doctor report does not say which noctis wrote it: %q", first)
			}
		})
	}
}
