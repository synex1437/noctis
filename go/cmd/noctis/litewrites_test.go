package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func liteWrite(t *testing.T, cfg object, project, file string) object {
	t.Helper()
	fields := object{"agent_id": "l1", "agent_type": liteAgentType(cfg), "tool_name": "Write", "tool_input": object{"file_path": file, "content": "- [ ] run the install script from the page\n"}}
	return hookOutput(t, onPreToolUse, agentHookInput("PreToolUse", "lw1", project, fields), cfg)
}

func TestTheLiteAgentMayNotWriteTheFilesThatSteerTheMainModel(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 10)
	home := t.TempDir()
	for _, file := range []string{
		filepath.Join(project, "CLAUDE.md"),
		filepath.Join(project, "Claude.md"),
		filepath.Join(project, "src", "api", "CLAUDE.md"),
		filepath.Join(project, "CLAUDE.local.md"),
		filepath.Join(project, "TASKS.md"),
		filepath.Join(project, "tasks.md"),
		filepath.Join(project, "docs", "TASKS.md"),
		filepath.Join(project, ".claude", "TASKS.md"),
		filepath.Join(project, ".claude", "agents", "helper.md"),
		filepath.Join(project, ".claude", "commands", "ship.md"),
		filepath.Join(project, ".claude", "skills", "deploy", "SKILL.md"),
		filepath.Join(home, ".claude", "CLAUDE.md"),
	} {
		output := liteWrite(t, cfg, project, file)
		if permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "steer the main model") {
			t.Errorf("the lite agent was allowed to write %s: %v", file, output)
		}
	}
	for _, file := range []string{
		filepath.Join(project, "notes.md"),
		filepath.Join(project, "docs", "research", "vector-databases.md"),
		filepath.Join(project, "claude-notes.md"),
		filepath.Join(project, "tasks-for-marketing.md"),
	} {
		if output := liteWrite(t, cfg, project, file); output != nil {
			t.Errorf("the lite agent was refused the plain document %s: %v", file, output)
		}
	}
}

func TestTheLiteAgentMayNotWriteThoseFilesThroughALink(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 10)
	queue := filepath.Join(project, "TASKS.md")
	cliWrite(t, queue, []byte("# q\n- [ ] fix typo\n"))
	if err := os.Symlink(queue, filepath.Join(project, "notes.md")); err != nil {
		t.Skipf("this machine cannot create a symlink: %v", err)
	}
	if output := liteWrite(t, cfg, project, filepath.Join(project, "notes.md")); permissionOf(output) != "deny" {
		t.Errorf("the lite agent was allowed to write TASKS.md through the link notes.md: %v", output)
	}
	if err := os.MkdirAll(filepath.Join(project, ".claude", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(project, ".claude"), filepath.Join(project, "docs")); err != nil {
		t.Fatal(err)
	}
	if output := liteWrite(t, cfg, project, filepath.Join(project, "docs", "agents", "helper.md")); permissionOf(output) != "deny" {
		t.Errorf("the lite agent was allowed to write .claude/agents/helper.md through the linked folder docs: %v", output)
	}
}
