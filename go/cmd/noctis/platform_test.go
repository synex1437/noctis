package main

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"testing"
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

	for _, nonsense := range []string{"99", "emacs", "!!"} {
		if got := parseHostChoice(nonsense); got != "" && got != "claude" {
			t.Errorf("parseHostChoice(%q) = %q; unrecognised input must not pick a host", nonsense, got)
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
