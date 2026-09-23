package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPowerShellQuotingSurvivesQuotes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`plain`, `'plain'`},
		{`C:\Users\Ada\AppData`, `'C:\Users\Ada\AppData'`},
		{`it's`, `'it''s'`},
		{`''`, `''''''`},
		{`'; Remove-Item C:\ -Recurse; '`, `'''; Remove-Item C:\ -Recurse; '''`},
		{``, `''`},
	}
	for _, tc := range cases {
		if got := psQuote(tc.in); got != tc.want {
			t.Errorf("psQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	for _, hostile := range []string{`a'b`, `'`, `'''`, `$(whoami)`, "a\nb", `"; ls; "`} {
		quoted := psQuote(hostile)
		if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
			t.Errorf("psQuote(%q) = %q: not a quoted literal", hostile, quoted)
		}
		inner := quoted[1 : len(quoted)-1]

		for i := 0; i < len(inner); i++ {
			if inner[i] != '\'' {
				continue
			}
			if i+1 >= len(inner) || inner[i+1] != '\'' {
				t.Errorf("psQuote(%q) = %q: a lone quote at %d ends the literal early", hostile, quoted, i)
				break
			}
			i++
		}
	}
}

func TestWindowsQuotingRoundTrips(t *testing.T) {
	cases := []string{
		`plain`,
		`has space`,
		`C:\Program Files\claude\claude.cmd`,
		`trailing\`,
		`trailing\\`,
		`"quoted"`,
		`a"b`,
		`C:\path with space\`,
		``,
	}
	for _, arg := range cases {
		quoted := windowsQuote(arg)
		if got := parseWindowsArgv(quoted); got != arg {
			t.Errorf("windowsQuote(%q) = %q, which parses back as %q", arg, quoted, got)
		}
	}
}

func parseWindowsArgv(quoted string) string {
	var out strings.Builder
	inQuotes := false
	for i := 0; i < len(quoted); i++ {
		switch quoted[i] {
		case '\\':
			slashes := 0
			for i < len(quoted) && quoted[i] == '\\' {
				slashes++
				i++
			}
			if i < len(quoted) && quoted[i] == '"' {
				out.WriteString(strings.Repeat(`\`, slashes/2))
				if slashes%2 == 1 {
					out.WriteByte('"')
				} else {
					inQuotes = !inQuotes
				}
			} else {
				out.WriteString(strings.Repeat(`\`, slashes))
				i--
			}
		case '"':
			inQuotes = !inQuotes
		default:
			out.WriteByte(quoted[i])
		}
	}
	_ = inQuotes
	return out.String()
}

func TestScheduledTaskNameIsStableAndScoped(t *testing.T) {
	previous := files
	t.Cleanup(func() { files = previous })

	files.configDir = "/home/ada/.claude"
	first := taskName("S1")
	if first != taskName("S1") {
		t.Fatal("the same session produced two different task names; a cancel would miss")
	}
	if first == taskName("S2") {
		t.Error("two sessions share a task name")
	}
	if !strings.HasPrefix(first, "Noctis-") {
		t.Errorf("task name %q is not recognisable as ours", first)
	}
	files.configDir = "/home/bob/.claude"
	if first == taskName("S1") {
		t.Error("two accounts share a task name: one would cancel the other's relaunch")
	}

	files.configDir = "/home/ada/.claude"
	for _, sid := range []string{`a b`, `a/b\c`, `"; schtasks /delete /tn *`, strings.Repeat("x", 500), ""} {
		name := taskName(sid)
		if strings.ContainsAny(name, ` /\:*?"<>|`) || len(name) > 200 {
			t.Errorf("taskName(%q) = %q is not a usable task name", sid, name)
		}
	}
}

func TestCompareVersionsOrdersReleases(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1.251", "2.1.251", 0},
		{"2.1.250", "2.1.251", -1},
		{"2.1.252", "2.1.251", 1},
		{"2.2.0", "2.1.999", 1},
		{"3.0.0", "2.9.9", 1},
		{"2.1", "2.1.0", 0},
		{"2.1.251", "2.1", 1},
		{"", "0.0.0", 0},
		{"2.1.251-beta.1", "2.1.251", 0},
	}
	for _, tc := range cases {
		got := compareVersions(tc.a, tc.b)
		sign := 0
		if got > 0 {
			sign = 1
		} else if got < 0 {
			sign = -1
		}
		if sign != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}

	if compareVersions(testedClaudeMin, testedClaudeMax) >= 0 {
		t.Errorf("tested range is inverted: min %s is not below max %s", testedClaudeMin, testedClaudeMax)
	}
}

func TestClaudeCommandKeepsEveryArgument(t *testing.T) {
	cmd := claudeCommand("claude", []string{"--model", "opus", "--resume", "S1"})
	if cmd == nil {
		t.Fatal("no command built")
	}
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{"--model", "opus", "--resume", "S1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argument %q did not survive into %v", want, cmd.Args)
		}
	}
	var _ *exec.Cmd = cmd
}

func TestHostChoiceAcceptsWhatPeopleActuallyType(t *testing.T) {
	cases := map[string]string{
		"":                         "claude",
		"   ":                      "claude",
		"1":                        "claude",
		"2":                        "codex",
		"codex":                    "codex",
		"CODEX":                    "codex",
		"  Codex  ":                "codex",
		"OpenAI Codex CLI":         "codex",
		"openai codex cli":         "codex",
		"factory":                  "droid",
		"droid":                    "droid",
		"copilot":                  "copilot",
		"GitHub Copilot CLI":       "copilot",
		"antigravity":              "antigravity",
		"Antigravity CLI (Google)": "antigravity",
		"fac":                      "droid",
		"git":                      "copilot",
		"open":                     "codex",
		"goo":                      "antigravity",
		"anti":                     "antigravity",
		"codex cli":                "codex",
		"github copilot":           "copilot",
		"claude code":              "claude",
	}
	for answer, want := range cases {
		if got := parseHostChoice(answer); got != want {
			t.Errorf("parseHostChoice(%q) = %q, want %q", answer, got, want)
		}
	}

	listed := describeHosts()
	for index, id := range hostOrder {
		number := fmt.Sprint(index + 1)
		if got := parseHostChoice(number); got != id {
			t.Errorf("the list offers %s as %q but that answer selects %q", id, number, got)
		}
		if !strings.Contains(listed, hostSpecs[id].display) {
			t.Errorf("%s is selectable but not printed in the list", id)
		}
	}

	for _, nonsense := range []string{"99", "emacs", "!!", "x", "n", "y", "g", "o", "i", "e", "c", "cli", "code"} {
		if got := parseHostChoice(nonsense); got != "" {
			t.Errorf("parseHostChoice(%q) = %q; unrecognised input must not pick a host", nonsense, got)
		}
	}
}

func TestInputFromAPipeOrAFileIsNeverAskedAQuestion(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	answers := filepath.Join(t.TempDir(), "answers.txt")
	if err := os.WriteFile(answers, []byte("2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(answers)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	saved := os.Stdin
	t.Cleanup(func() { os.Stdin = saved })
	for name, input := range map[string]*os.File{"a pipe": reader, "a file": file} {
		os.Stdin = input
		if stdinIsTerminal() {
			t.Errorf("stdin from %s was taken for a terminal; install and setup ask only someone at a keyboard, and piped or redirected input means no question", name)
		}
	}
}

func TestWebhookErrorNamesTheEndpointNotTheWholeURL(t *testing.T) {

	err := &url.Error{
		Op:  "Post",
		URL: "https://hooks.example.com/services/T000/B000/SECRETTOKEN?key=alsosecret",
		Err: errors.New("connection refused"),
	}
	text := webhookErrorText(err)
	if !strings.Contains(text, "hooks.example.com") {
		t.Errorf("error text %q does not say which endpoint failed", text)
	}
	for _, secret := range []string{"SECRETTOKEN", "alsosecret", "/services/"} {
		if strings.Contains(text, secret) {
			t.Errorf("error text %q leaks %q from the webhook URL", text, secret)
		}
	}
	if !strings.Contains(text, "connection refused") {
		t.Errorf("error text %q lost the actual cause", text)
	}

	if got := webhookErrorText(errors.New("plain")); got != "plain" {
		t.Errorf("webhookErrorText(plain) = %q", got)
	}
	if got := unwrapMessage(nil); got == "" {
		t.Error("unwrapMessage(nil) must still say something")
	}
}

func TestQueueFileNamesIgnoresNonStrings(t *testing.T) {
	cfg := object{"queue": object{"files": []any{"TASKS.md", "", 42.0, nil, "TODO.md", object{}}}}
	got := queueFileNames(cfg)
	want := []string{"TASKS.md", "TODO.md"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("name %d = %q, want %q", i, got[i], want[i])
		}
	}
	if names := queueFileNames(object{}); len(names) != 0 {
		t.Errorf("a config with no queue section produced %q", names)
	}
}

func TestAbsFloat(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{{-3.5, 3.5}, {3.5, 3.5}, {0, 0}} {
		if got := absFloat(tc.in); got != tc.want {
			t.Errorf("absFloat(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestTerminalPreferenceIsNormalised(t *testing.T) {
	for answer, want := range map[string]string{
		"  WindowsTerminal ": "windowsterminal",
		"Auto":               "auto",
		"":                   "",
	} {
		if got := terminalPreference(object{"resume": object{"terminal": answer}}); got != want {
			t.Errorf("terminalPreference(%q) = %q, want %q", answer, got, want)
		}
	}
}

func cmdParsesTheTail(argument string) (program string, rest string, ok bool) {
	tail, found := strings.CutPrefix(argument, "/d /s /c ")
	if !found {
		return "", "", false
	}

	if len(tail) >= 2 && tail[0] == '"' {
		tail = tail[1 : len(tail)-1]
	}
	if len(tail) == 0 || tail[0] != '"' {
		cut := strings.IndexByte(tail, ' ')
		if cut < 0 {
			return tail, "", true
		}
		return tail[:cut], tail[cut+1:], true
	}
	end := strings.IndexByte(tail[1:], '"')
	if end < 0 {
		return "", "", false
	}
	return tail[1 : end+1], strings.TrimPrefix(tail[end+2:], " "), true
}

func TestTheScheduledTaskCommandSurvivesTheQuotesCmdStrips(t *testing.T) {
	cases := []struct {
		name     string
		launcher string
		args     []string
	}{
		{"a space in both paths",
			`C:\Users\Ada Byron\AppData\Roaming\noctis\runner.cmd`,
			[]string{"resume", "--sid", "s1", "--account", `"C:\Users\Ada Byron\.claude"`}},
		{"no space anywhere",
			`C:\noctis\runner.cmd`,
			[]string{"resume", "--sid", "s1", "--account", `"C:\claude"`}},
		{"a host flag as well",
			`C:\Users\Ada\runner.cmd`,
			[]string{"resume", "--sid", "s1", "--account", `"C:\Users\Ada\.codex"`, "--host", "codex"}},
	}
	for _, tc := range cases {
		program, rest, ok := cmdParsesTheTail(windowsTaskArgument(tc.launcher, tc.args))
		if !ok {
			t.Errorf("%s: cmd.exe cannot parse the tail at all", tc.name)
			continue
		}
		if program != tc.launcher {
			t.Errorf("%s: cmd.exe would run %q, not the launcher %q", tc.name, program, tc.launcher)
		}
		if want := strings.Join(tc.args, " "); rest != want {
			t.Errorf("%s: the launcher would be handed %q, want %q", tc.name, rest, want)
		}
	}
}

func chdirUntilCleanup(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func plantedName(name string) string {
	if isWindows {
		return name + ".cmd"
	}
	return name
}

func writeScript(t *testing.T, file, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return file
}

func plantExecutable(t *testing.T, file string) string {
	t.Helper()
	return writeScript(t, file, "exit 0\n")
}

func resolvesTo(first, second string) bool {
	a, aErr := os.Stat(first)
	b, bErr := os.Stat(second)
	return aErr == nil && bErr == nil && os.SameFile(a, b)
}

func TestTheLookupNeverSettlesForACopyInTheWorkingDirectory(t *testing.T) {
	work, trusted := t.TempDir(), t.TempDir()
	chdirUntilCleanup(t, work)
	planted := plantExecutable(t, filepath.Join(work, plantedName("zzfake")))
	nested := plantExecutable(t, filepath.Join(work, "bin", plantedName("zznested")))
	wanted := plantExecutable(t, filepath.Join(trusted, plantedName("zzfake")))
	plain := plantExecutable(t, filepath.Join(trusted, plantedName("zzplain")))
	list, system := string(os.PathListSeparator), os.Getenv("PATH")
	cases := []struct{ name, path string }{
		{"only absolute entries on PATH", trusted + list + system},
		{". and bin ahead of the absolute entries", "." + list + "bin" + list + trusted + list + system},
	}
	for _, tc := range cases {
		t.Setenv("PATH", tc.path)
		if got := locateExecutable("zzfake"); !filepath.IsAbs(got) || !resolvesTo(got, wanted) {
			t.Errorf("%s: zzfake resolved to %q; want the copy PATH names (%q), never the one in the working directory (%q)", tc.name, got, wanted, planted)
		}
		if got := locateExecutable("zzplain"); !filepath.IsAbs(got) || !resolvesTo(got, plain) {
			t.Errorf("%s: zzplain resolved to %q, want the absolute %q", tc.name, got, plain)
		}
	}
	t.Setenv("PATH", "bin"+list+system)
	if got := locateExecutable("zznested"); got != "" {
		t.Errorf("zznested resolved to %q; it exists only under the relative PATH entry bin (%q)", got, nested)
	}
}

func TestTheFinderFallbackSkipsTheWorkingDirectory(t *testing.T) {
	work, trusted := t.TempDir(), t.TempDir()
	chdirUntilCleanup(t, work)
	planted := plantExecutable(t, filepath.Join(work, plantedName("zzfake")))
	wanted := plantExecutable(t, filepath.Join(trusted, plantedName("zzfake")))
	list := string(os.PathListSeparator)
	t.Setenv("PATH", "."+list+trusted+list+os.Getenv("PATH"))
	if got := probeExecutable("zzfake"); got != "" && (!filepath.IsAbs(got) || !resolvesTo(got, wanted)) {
		t.Errorf("the finder fallback answered %q; only %q may come back, never the copy in the working directory (%q)", got, wanted, planted)
	}
	for _, line := range []string{"zzfake", filepath.Join(".", "zzfake"), planted, filepath.Join(strings.ToUpper(work), plantedName("zzfake"))} {
		if outsideWorkingDir(line) {
			t.Errorf("a finder line %q counts as outside the working directory", line)
		}
	}
	alias := filepath.Join(trusted, "alias")
	if os.Symlink(work, alias) == nil && outsideWorkingDir(filepath.Join(alias, plantedName("zzfake"))) {
		t.Errorf("a path through a link to the working directory counts as outside it")
	}
	if !outsideWorkingDir(wanted) {
		t.Errorf("%q lies outside the working directory but was refused", wanted)
	}
}

func TestClaudeIsNeverTakenFromTheWorkingDirectory(t *testing.T) {
	work, home := t.TempDir(), t.TempDir()
	chdirUntilCleanup(t, work)
	planted := []string{plantExecutable(t, filepath.Join(work, plantedName("claude")))}
	for _, relative := range []string{filepath.Join(".local", "bin", "claude"), filepath.Join(".local", "bin", "claude.exe"), filepath.Join("npm", "claude.cmd"), filepath.Join("Programs", "claude", "claude.exe")} {
		planted = append(planted, plantExecutable(t, filepath.Join(work, relative)))
	}
	check := func(when string) {
		t.Helper()
		got := claudeExecutable()
		if got == "" {
			return
		}
		if !filepath.IsAbs(got) {
			t.Errorf("%s: claude resolved to the relative path %q", when, got)
		}
		for _, file := range planted {
			if resolvesTo(got, file) {
				t.Errorf("%s: claude resolved to %q, the copy planted in the working directory", when, got)
			}
		}
	}
	homes := []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA"}
	for _, variable := range homes {
		t.Setenv(variable, home)
	}
	t.Setenv("PATH", "."+string(os.PathListSeparator)+os.Getenv("PATH"))
	check("with . on PATH")
	for _, variable := range homes {
		t.Setenv(variable, "")
	}
	t.Setenv("PATH", home)
	check("with no home directory and nothing on PATH")
}

func TestTheLookupRefusesTheWorkingDirectoryWhereGoWouldAllowIt(t *testing.T) {
	work, trusted := t.TempDir(), t.TempDir()
	chdirUntilCleanup(t, work)
	planted := plantExecutable(t, filepath.Join(work, plantedName("zzfake")))
	plantExecutable(t, filepath.Join(work, "bin", plantedName("zzfake")))
	wanted := plantExecutable(t, filepath.Join(trusted, plantedName("zzfake")))
	list, system := string(os.PathListSeparator), os.Getenv("PATH")
	cases := []struct{ name, godebug, path string }{
		{"execerrdot=0 lets LookPath answer from . with no error", "execerrdot=0", "." + list + trusted + list + system},
		{"the working directory is listed by its absolute path after bin", "", "bin" + list + work + list + trusted + list + system},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GODEBUG", tc.godebug)
			t.Setenv("PATH", tc.path)
			if got := locateExecutable("zzfake"); !filepath.IsAbs(got) || !resolvesTo(got, wanted) {
				t.Errorf("zzfake resolved to %q; want the copy PATH names (%q), never one in the working directory (%q)", got, wanted, planted)
			}
		})
	}
}

func TestProbesNeverRunAnInterpreterPlantedInTheProject(t *testing.T) {
	sandboxFiles(t)
	work, trusted := t.TempDir(), t.TempDir()
	marker := filepath.Join(t.TempDir(), "planted-ran")
	t.Setenv("NOCTIS_TEST_PLANTED", marker)
	chdirUntilCleanup(t, work)
	host := filepath.Join(trusted, plantedName("claude"))
	if isWindows {
		writeScript(t, host, "@zznode %*\r\n")
		writeScript(t, filepath.Join(work, "zznode.cmd"), "@echo planted>\"%NOCTIS_TEST_PLANTED%\"\r\n")
	} else {
		writeScript(t, host, "#!/usr/bin/env zznode\n")
		writeScript(t, filepath.Join(work, "zznode"), "#!/bin/sh\n: > \"$NOCTIS_TEST_PLANTED\"\n")
	}
	list := string(os.PathListSeparator)
	t.Setenv("PATH", "."+list+trusted+list+os.Getenv("PATH"))
	ran := func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}
	control := exec.Command(host, "--version")
	control.Dir = work
	_ = control.Run()
	if !ran() {
		t.Fatalf("%s started in the project folder never reached the zznode planted there, so nothing below would be proven", host)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if got := claudeExecutable(); !resolvesTo(got, host) {
		t.Fatalf("claude resolved to %q, want the trusted %q", got, host)
	}
	probes := []struct {
		name string
		run  func()
	}{
		{"codex app-server (usage refresh from the hooks)", func() { _, _ = fetchCodexRateLimits(host, 5*time.Second) }},
		{"claude --help (permission mode probe in setup)", func() { supportedPermissionMode(object{"resume": object{"permissionMode": "auto"}}, host, "") }},
		{"claude --version (selftest)", func() { claudeVersion(host) }},
		{"claude plugin marketplace update (setup)", func() {
			enableMarketplaceAutoUpdate(filepath.Join(trusted, "plugins", "cache", "zzmarket", pluginName, "1.0.0"))
		}},
	}
	for _, probe := range probes {
		probe.run()
		if ran() {
			t.Errorf("%s ran the zznode planted in the project folder", probe.name)
			_ = os.Remove(marker)
		}
	}
}

func TestTheChainedStatusLineIgnoresACommandPlantedInTheProject(t *testing.T) {
	work, trusted := t.TempDir(), t.TempDir()
	marker := filepath.Join(t.TempDir(), "planted-ran")
	t.Setenv("NOCTIS_TEST_PLANTED", marker)
	chdirUntilCleanup(t, work)
	if isWindows {
		writeScript(t, filepath.Join(work, "zzchain.cmd"), "@echo planted>\"%NOCTIS_TEST_PLANTED%\"\r\n")
		writeScript(t, filepath.Join(trusted, "zzchain.cmd"), "@echo trusted\r\n")
	} else {
		writeScript(t, filepath.Join(work, "zzchain"), "#!/bin/sh\n: > \"$NOCTIS_TEST_PLANTED\"\n")
		writeScript(t, filepath.Join(trusted, "zzchain"), "#!/bin/sh\necho trusted\n")
	}
	t.Setenv("PATH", trusted+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got := runChain("zzchain", []byte("{}")); got != "trusted" {
		t.Errorf("the chained status line printed %q; want the copy on PATH to answer %q", got, "trusted")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("the chained status line ran the zzchain planted in the project folder")
	}
}
