package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func routerAgentToolRules(t *testing.T, cfg object) map[string]agentToolRules {
	t.Helper()
	all := map[string]agentToolRules{}
	for _, agent := range []string{liteAgentType(cfg), digestAgentType(cfg)} {
		file := "agents/" + strings.TrimPrefix(agent, pluginName+":") + ".md"
		rules, ok := readAgentToolRules(filepath.Join(repoRoot(), filepath.FromSlash(file)))
		if !ok {
			t.Fatalf("%s is missing or has no frontmatter to read its tools from", file)
		}
		all[file] = rules
	}
	return all
}

func preToolUseMatches(t *testing.T) func(string) bool {
	t.Helper()
	manifest := readJSON(filepath.Join(repoRoot(), "hooks", "hooks.json"))
	groups := getList(getMap(manifest, "hooks"), "PreToolUse")
	if len(groups) == 0 {
		t.Fatal("hooks/hooks.json registers no PreToolUse hook")
	}
	exactList := regexp.MustCompile(`^[A-Za-z0-9_|]+$`)
	names, everything := map[string]bool{}, false
	for _, group := range groups {
		matcher := getString(toObject(group), "matcher")
		switch {
		case matcher == "" || matcher == "*":
			everything = true
		case exactList.MatchString(matcher):
			for _, name := range strings.Split(matcher, "|") {
				names[name] = true
			}
		default:
			t.Fatalf("the PreToolUse matcher %q is a regex; this check reads it as the exact list of tool names the host compares", matcher)
		}
	}
	return func(tool string) bool { return everything || names[tool] }
}

func capturedStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	captured := make(chan string, 1)
	go func() {
		output, _ := io.ReadAll(reader)
		reader.Close()
		captured <- string(output)
	}()
	previous := os.Stdout
	os.Stdout = writer
	func() {
		defer func() {
			os.Stdout = previous
			writer.Close()
		}()
		run()
	}()
	return <-captured
}

func mainThreadAnswers(t *testing.T, cfg object) map[string]bool {
	t.Helper()
	defer func(previous bool) { emitted = previous }(emitted)
	call := func(tool, filePath string) string {
		updateState(func(state object) {
			stateMap(state, "routes")["routed"] = object{"at": float64(nowSec()), "denies": float64(0)}
		})
		return capturedStdout(t, func() {
			emitted = false
			onPreToolUse(object{"session_id": "routed", "cwd": t.TempDir(), "tool_name": tool, "tool_input": object{"file_path": filePath, "content": "x", "old_string": "a", "new_string": "b"}}, cfg)
		})
	}
	if out := call("WebSearch", "main.go"); !strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("a routed prompt let a main-thread WebSearch through (%q), so a quiet file tool below would prove nothing", out)
	}
	answers := map[string]bool{}
	for tool := range fileTools {
		answers[tool] = call(tool, "main.go") != "" || call(tool, files.config) != "" || call(tool, files.state) != ""
	}
	return answers
}

func liteAgentGuards(t *testing.T, cfg object) map[string]bool {
	t.Helper()
	defer func(previousEmitted bool, previousHost string) { emitted, activeHost = previousEmitted, previousHost }(emitted, activeHost)
	activeHost = "claude"
	guarded := map[string]bool{}
	for tool := range fileTools {
		out := capturedStdout(t, func() {
			emitted = false
			onPreToolUse(object{"session_id": "lite", "agent_type": liteAgentType(cfg), "cwd": t.TempDir(), "tool_name": tool, "tool_input": object{"file_path": "scratch.bin", "content": "x", "old_string": "a", "new_string": "b"}}, cfg)
		})
		guarded[tool] = strings.Contains(out, `"permissionDecision":"deny"`)
	}
	return guarded
}

func TestPreToolUseStartsNoctisOnlyForFileToolsItCanAnswer(t *testing.T) {
	sandboxFiles(t)
	cfg := readJSON(filepath.Join(repoRoot(), "config.default.json"))
	matches := preToolUseMatches(t)
	agents := routerAgentToolRules(t, cfg)
	mainThread := mainThreadAnswers(t, cfg)
	liteGuarded := liteAgentGuards(t, cfg)
	unhooked := map[string]bool{}
	for _, tool := range unhookedFileTools {
		if !fileTools[tool] {
			t.Errorf("unhookedFileTools lists %s, which is not a file tool", tool)
		}
		unhooked[tool] = true
	}
	for _, tool := range sortedKeys(fileTools) {
		callable := false
		for _, agent := range agents {
			callable = callable || agent.mayCall(tool)
		}
		hooked := matches(tool) && liteGuarded[tool]
		if hooked && unhooked[tool] {
			t.Errorf("the PreToolUse hook stops a stray %s (the matcher sends it and the write policy denies it), so unhookedFileTools should not list it", tool)
		}
		if !hooked && !unhooked[tool] {
			t.Errorf("the PreToolUse hook does not apply the write policy to %s, so unhookedFileTools must list it for the self-check to name a router agent that allows it", tool)
		}
		if matches(tool) {
			if !callable && !mainThread[tool] {
				t.Errorf("PreToolUse starts noctis on every %s call, but the main thread lets %s through and no router agent may call it, so the process can never answer", tool, tool)
			}
			continue
		}
		if mainThread[tool] {
			t.Errorf("noctis answers a main-thread %s call, but the PreToolUse matcher never sends it one", tool)
		}
		for _, file := range sortedKeys(agents) {
			if agent := agents[file]; !agent.disallowed[tool] || agent.tools[tool] {
				t.Errorf("%s leaves %s open, but the PreToolUse matcher never sends %s to noctis, so the write policy cannot stop it", file, tool, tool)
			}
		}
	}
}

func TestARouterAgentThatMayCallAnUnhookedFileToolIsNamed(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = repoRoot()
	if issues := unguardedAgentTools(readJSON(filepath.Join(repoRoot(), "config.default.json"))); len(issues) > 0 {
		t.Fatalf("the shipped router agents were reported: %q", issues)
	}
	files.pluginRoot = dir
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, frontmatter, open string }{
		{"inherits", "name: inherits\ndescription: no tool list\n", "Edit, MultiEdit, NotebookEdit"},
		{"allowlist", "tools: Read, Write, Glob\n", ""},
		{"flow", "tools: [Read, \"Edit\", Bash(git:*)]\n", "Edit"},
		{"block", "tools:\n  - Read\n  - MultiEdit\n  - Write\ndisallowedTools: Agent\n", "MultiEdit"},
		{"denylist", "disallowedTools:\n- Edit\n- 'MultiEdit'\nmodel: haiku\n", "NotebookEdit"},
		{"commented", "disallowedTools: Edit, MultiEdit # NotebookEdit stays\n", "NotebookEdit"},
		{"crlf", "disallowedTools: Edit, NotebookEdit\n", "MultiEdit"},
		{"star", "tools: \"*\"\ndisallowedTools: MultiEdit, NotebookEdit\n", "Edit"},
		{"nested", "hooks:\n  tools: Read\ndisallowedTools: NotebookEdit\n", "Edit, MultiEdit"},
	}
	for _, c := range cases {
		content := "---\n" + c.frontmatter + "---\nWork the task.\n"
		if c.name == "crlf" {
			content = "\uFEFF" + strings.ReplaceAll(content, "\n", "\r\n")
		}
		if err := os.WriteFile(filepath.Join(dir, "agents", c.name+".md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		want := []string{}
		if c.open != "" {
			want = append(want, T("selfcheck.agentTools", "agents/"+c.name+".md", c.open))
		}
		got := unguardedAgentTools(object{"router": object{"agent": c.name, "digestAgent": "absent"}})
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: got %q, want %q", c.name, got, want)
		}
	}
	both := object{"router": object{"agent": "allowlist", "digestAgent": "inherits"}}
	issue := T("selfcheck.agentTools", "agents/inherits.md", "Edit, MultiEdit, NotebookEdit")
	if got := unguardedAgentTools(both); len(got) != 1 || got[0] != issue {
		t.Errorf("a hand-made digest agent was not named on its own: %q", got)
	}
	if got := unguardedAgentTools(object{"router": object{"agent": "inherits", "digestAgent": "inherits"}}); len(got) != 1 {
		t.Errorf("one file named for both router agents was reported %d times", len(got))
	}
	if !strings.Contains(strings.Join(selfCheckIssues(both), "; "), issue) {
		t.Errorf("the session self-check left out %q", issue)
	}
	back, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(back) })
	files.pluginRoot = ""
	if got := unguardedAgentTools(both); len(got) != 0 {
		t.Errorf("with no plugin root the check read agents/ from the working directory: %q", got)
	}
}
