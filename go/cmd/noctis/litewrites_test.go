package main

import (
	"os"
	"path/filepath"
	"strconv"
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

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this machine cannot create a symlink: %v", err)
	}
}

func TestTheLiteAgentMayNotWriteThroughALinkWhoseTargetIsMissing(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 10)
	if err := os.MkdirAll(filepath.Join(project, ".claude", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ name, target string }{
		{"notes.md", filepath.Join(".claude", "settings.local.json")},
		{"summary.md", filepath.Join(".claude", "agents", "helper.md")},
		{"draft.md", "TASKS.md"},
		{"outline.md", "draft.md"},
		{"brief.md", filepath.Join(project, "CLAUDE.md")},
	} {
		symlinkOrSkip(t, link.target, filepath.Join(project, link.name))
		output := liteWrite(t, cfg, project, filepath.Join(project, link.name))
		if permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "steer the main model") {
			t.Errorf("the lite agent was allowed to write through %s, a link to %s that does not exist yet: %v", link.name, link.target, output)
		}
	}
	symlinkOrSkip(t, filepath.Join("drafts", "later.md"), filepath.Join(project, "later.md"))
	if output := liteWrite(t, cfg, project, filepath.Join(project, "later.md")); output != nil {
		t.Errorf("the lite agent was refused later.md, a link to the document drafts/later.md that does not exist yet: %v", output)
	}
}

func TestTheLiteAgentMayNotWriteCodeThroughADocumentLink(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 10)
	cliWrite(t, filepath.Join(project, "src", "main.go"), []byte("package main\n"))
	cliWrite(t, filepath.Join(project, "docs", "guide.md"), []byte("# guide\n"))
	for _, link := range []struct{ name, target string }{
		{"draft.md", filepath.Join("src", "main.go")},
		{"plan.md", filepath.Join("src", "server.go")},
		{"notes.txt", filepath.Join(project, "Makefile")},
	} {
		symlinkOrSkip(t, link.target, filepath.Join(project, link.name))
		output := liteWrite(t, cfg, project, filepath.Join(project, link.name))
		if permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "only write text documents") {
			t.Errorf("the lite agent was allowed to write %s through the document link %s: %v", link.target, link.name, output)
		}
	}
	symlinkOrSkip(t, filepath.Join("docs", "guide.md"), filepath.Join(project, "guide.md"))
	if output := liteWrite(t, cfg, project, filepath.Join(project, "guide.md")); output != nil {
		t.Errorf("the lite agent was refused guide.md, a link to docs/guide.md: %v", output)
	}
}

func TestTheLiteAgentMayNotReachThoseFilesByStepsBackOutOfALinkedFolder(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 10)
	team := filepath.Join(project, ".claude", "agents", "team")
	research := filepath.Join(project, "docs", "research")
	elsewhere := filepath.Join(t.TempDir(), "deep")
	for _, dir := range []string{team, research, elsewhere} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	symlinkOrSkip(t, team, filepath.Join(project, "shortcut"))
	symlinkOrSkip(t, elsewhere, filepath.Join(project, "away"))
	symlinkOrSkip(t, "TASKS.md", filepath.Join(project, "plan.md"))
	symlinkOrSkip(t, research, filepath.Join(project, "research"))
	back := func(parts ...string) string {
		return project + string(filepath.Separator) + strings.Join(parts, string(filepath.Separator))
	}
	if output := liteWrite(t, cfg, project, back("shortcut", "..", "helper.md")); permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "steer the main model") {
		t.Errorf("the lite agent was allowed to write shortcut/../helper.md, which is .claude/agents/helper.md while shortcut links to .claude/agents/team: %v", output)
	}
	if output := liteWrite(t, cfg, project, back("away", "..", "plan.md")); permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "steer the main model") {
		t.Errorf("the lite agent was allowed to write away/../plan.md, which reads as plan.md, a link to TASKS.md: %v", output)
	}
	if output := liteWrite(t, cfg, project, back("research", "..", "summary.md")); output != nil {
		t.Errorf("the lite agent was refused research/../summary.md, which is docs/summary.md: %v", output)
	}
}

func TestTheLiteAgentMayNotWriteThroughALinkNoctisCannotFollow(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 10)
	symlinkOrSkip(t, "loop.md", filepath.Join(project, "loop.md"))
	symlinkOrSkip(t, "there", filepath.Join(project, "here"))
	symlinkOrSkip(t, "here", filepath.Join(project, "there"))
	chain := filepath.Join(project, "chain")
	cliWrite(t, filepath.Join(chain, "final.md"), []byte("# final\n"))
	target := "final.md"
	for hop := 8; hop >= 0; hop-- {
		name := "hop" + strconv.Itoa(hop) + ".md"
		symlinkOrSkip(t, target, filepath.Join(chain, name))
		target = name
	}
	for _, file := range []string{filepath.Join(project, "loop.md"), filepath.Join(project, "here", "notes.md"), filepath.Join(chain, "hop0.md")} {
		if output := liteWrite(t, cfg, project, file); permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "cannot follow") {
			t.Errorf("the lite agent was allowed to write through %s, a link to itself, a loop of linked folders or a chain of nine links: %v", file, output)
		}
	}
	if output := liteWrite(t, cfg, project, filepath.Join(chain, "hop1.md")); output != nil {
		t.Errorf("the lite agent was refused hop1.md, a chain of eight links to a document: %v", output)
	}
}
