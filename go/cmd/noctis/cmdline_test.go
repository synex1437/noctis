package main

import (
	"slices"
	"strings"
	"testing"
)

func parseWindowsArguments(line string) []string {
	arguments := []string{}
	var current strings.Builder
	quoted, started := false, false
	for i := 0; i < len(line); {
		switch c := line[i]; {
		case c == '\\':
			run := 0
			for i < len(line) && line[i] == '\\' {
				run++
				i++
			}
			if i < len(line) && line[i] == '"' {
				current.WriteString(strings.Repeat(`\`, run/2))
				if run%2 == 1 {
					current.WriteByte('"')
					i++
				}
			} else {
				current.WriteString(strings.Repeat(`\`, run))
			}
			started = true
		case c == '"':
			if quoted && i+1 < len(line) && line[i+1] == '"' {
				current.WriteByte('"')
				i += 2
			} else {
				quoted = !quoted
				i++
			}
			started = true
		case (c == ' ' || c == '\t') && !quoted:
			if started {
				arguments = append(arguments, current.String())
				current.Reset()
				started = false
			}
			i++
		default:
			current.WriteByte(c)
			started = true
			i++
		}
	}
	if started {
		arguments = append(arguments, current.String())
	}
	return arguments
}

func cmdSlashSRuns(commandLine string) (program, rest string, ok bool) {
	_, after, found := strings.Cut(commandLine, " /d /s /c ")
	if !found || len(after) < 2 || after[0] != '"' || after[len(after)-1] != '"' {
		return "", "", false
	}
	after = after[1 : len(after)-1]
	if !strings.HasPrefix(after, `"`) {
		program, rest, _ = strings.Cut(after, " ")
		return program, rest, true
	}
	end := strings.IndexByte(after[1:], '"')
	if end < 0 {
		return "", "", false
	}
	return after[1 : end+1], strings.TrimLeft(after[end+2:], " "), true
}

func TestAClaudeCmdCommandLineReachesClaudeArgumentForArgument(t *testing.T) {
	program := `C:\Program Files\nodejs\claude.cmd`
	arguments := []string{"-p", "--resume", "0b7e41c2", "--model", "claude-opus-5-5", "--effort", "", "--permission-mode", "acceptEdits",
		"Continue. Next: fix the parser (a&b | c > d)", `C:\work dir\notes\`, `ends with\`, "x=1,y;2", `a\b`}
	line := "cmd.exe " + windowsShellArgument(windowsCommandLine(program, arguments))
	ran, rest, ok := cmdSlashSRuns(line)
	if !ok || ran != program {
		t.Fatalf("cmd.exe /s /c would run %q from %s, want %q", ran, line, program)
	}
	if got := parseWindowsArguments(rest); !slices.Equal(got, arguments) {
		t.Fatalf("claude.cmd would hand claude %q, want %q (command line %s)", got, arguments, line)
	}
}

func TestAWindowsCommandLineParserReadsTheRulesClaudeCmdIsHandedBy(t *testing.T) {
	cases := map[string][]string{
		`a "b c" d`:          {"a", "b c", "d"},
		`"a\"b" c`:           {`a"b`, "c"},
		`"a\\" b`:            {`a\`, "b"},
		`a\\\"b`:             {`a\"b`},
		`"" x`:               {"", "x"},
		`"say ""hi""" there`: {`say "hi"`, "there"},
		`  spaced   out  `:   {"spaced", "out"},
	}
	for line, want := range cases {
		if got := parseWindowsArguments(line); !slices.Equal(got, want) {
			t.Errorf("%s parsed as %q, want %q", line, got, want)
		}
	}
}
