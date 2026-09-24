package main

import (
	"os"
	"strings"
	"testing"
)

func shellWords(line string) []string {
	words := []string{}
	var word strings.Builder
	inWord, quoted := false, false
	for index := 0; index < len(line); index++ {
		char := line[index]
		switch {
		case quoted && char == '\'':
			quoted = false
		case quoted:
			word.WriteByte(char)
		case char == '\'':
			quoted, inWord = true, true
		case char == '\\' && index+1 < len(line):
			index++
			word.WriteByte(line[index])
			inWord = true
		case char == ' ' || char == '\t' || char == '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteByte(char)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}

func powershellWords(line string) []string {
	words := []string{}
	for _, field := range strings.Split(strings.TrimPrefix(line, "& "), "' '") {
		words = append(words, strings.ReplaceAll(strings.Trim(field, "'"), "''", "'"))
	}
	return words
}

func previewOutput(t *testing.T, argv ...string) string {
	t.Helper()
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs(append([]string{"schedule-preview"}, argv...))
	return capturedStdout(t, runSchedulePreview)
}

func TestTheSleeperPreviewIsTheCommandTheSchedulerRuns(t *testing.T) {
	sandboxFiles(t)
	output := strings.TrimSpace(previewOutput(t, "--backend", "sleeper", "--sid", "s1", "--at", "1790300000"))

	if strings.Contains(output, "--watch") {
		t.Fatalf("the sleeper preview shows the reset watcher, which returns at the deadline without resuming: %s", output)
	}
	executable, _ := os.Executable()
	want := append([]string{executable}, runnerArgs("sleeper", "s1", files.configDir, "--at", "1790300000")...)
	got := shellWords(output)
	if isWindows {
		got = powershellWords(output)
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("the sleeper preview does not paste as the command the scheduler starts:\n got %q\nwant %q", got, want)
	}
}

func TestTheSystemdPreviewPastesAsOneCommand(t *testing.T) {
	sandboxFiles(t)
	output := strings.TrimSpace(previewOutput(t, "--backend", "systemd", "--sid", "s1", "--at", "1790300000"))

	words := shellWords(output)
	calendar := ""
	for _, word := range words {
		if strings.HasPrefix(word, "--on-calendar=") {
			calendar = word
		}
	}
	if len(words) == 0 || words[0] != "systemd-run" || !strings.Contains(calendar, " ") {
		t.Fatalf("pasted into a shell, the systemd preview splits its calendar time (%q):\n%s", calendar, output)
	}
}

func TestPreviewingTheTaskBackendWritesNothing(t *testing.T) {
	sandboxFiles(t)
	files.runnerLauncher = files.guardDir + string(os.PathSeparator) + "runner.cmd"

	output := previewOutput(t, "--backend", "task", "--sid", "s1", "--at", "1790300000")

	if !strings.Contains(output, "Register-ScheduledTask") || !strings.Contains(output, "runner.cmd") {
		t.Fatalf("the task preview does not show the script it would run:\n%s", output)
	}
	if statSafe(files.runnerLauncher) != nil {
		t.Fatal("previewing the task backend wrote runner.cmd, which pending tasks execute")
	}
	if statSafe(files.errors) != nil {
		content, _ := os.ReadFile(files.errors)
		t.Fatalf("previewing the task backend logged an error:\n%s", content)
	}
}

func TestThePreviewRefusesABackendOrTimeItDoesNotKnow(t *testing.T) {
	for _, argv := range [][]string{
		{"--backend", "sleepr"},
		{"--backend", "cron"},
		{"--at", "NaN"},
		{"--at", "inf"},
		{"--at", "-5"},
		{"--at", "tomorrow"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			run := runNoctisCLI(t, nil, append([]string{"schedule-preview", "--sid", "s1"}, argv...)...)

			if run.code != 2 || !strings.Contains(run.stderr, "schedule-preview") {
				t.Fatalf("want the usage on stderr and exit 2, nothing previewed:\n%s", run)
			}
		})
	}
}
