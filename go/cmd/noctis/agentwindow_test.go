package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pluginAgentsCopy(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range pluginAgentFiles {
		content, err := os.ReadFile(filepath.Join(repoRoot(), "agents", file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "agents", file), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func agentFileText(t *testing.T, root, file string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "agents", file))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestNoctisAgentsCompactAtSubagentsCompactWindow(t *testing.T) {
	sandboxFiles(t)
	root := pluginAgentsCopy(t)
	if changed := syncAgentWindows(root, object{}); changed != 0 {
		t.Fatalf("the shipped agent files do not carry the default subagents.compactWindow: %d rewritten", changed)
	}
	for _, file := range pluginAgentFiles {
		if text := agentFileText(t, root, file); !strings.Contains(text, "\nautoCompactWindow: 200000\nmodel: ") {
			t.Fatalf("agents/%s does not compact at the default window:\n%s", file, text)
		}
	}
	loosened := object{"subagents": object{"compactWindow": float64(400000)}}
	if changed := syncAgentWindows(root, loosened); changed != len(pluginAgentFiles) {
		t.Fatalf("subagents.compactWindow 400000 rewrote %d agent files, want %d", changed, len(pluginAgentFiles))
	}
	roles := object{"research": object{"model": "opus", "effort": "xhigh"}, "digest": object{"model": "haiku"}}
	syncAgentFiles(root, roles, false)
	if changed := syncAgentWindows(root, loosened) + syncAgentFiles(root, roles, false); changed != 0 {
		t.Fatalf("the roles sync and the window sync undo each other: %d rewrites", changed)
	}
	for _, file := range pluginAgentFiles {
		if text := agentFileText(t, root, file); strings.Count(text, "autoCompactWindow:") != 1 || !strings.Contains(text, "\nautoCompactWindow: 400000\nmodel: ") {
			t.Fatalf("agents/%s does not compact at 400000:\n%s", file, text)
		}
	}
	if text := agentFileText(t, root, "lite.md"); !strings.Contains(text, "\nautoCompactWindow: 400000\nmodel: opus\neffort: xhigh\n---\n") {
		t.Fatalf("agents/lite.md lost its research role or its window:\n%s", text)
	}
	if changed := syncAgentWindows(root, object{"subagents": object{"compactWindow": float64(0)}}); changed != len(pluginAgentFiles) {
		t.Fatalf("subagents.compactWindow 0 rewrote %d agent files, want %d", changed, len(pluginAgentFiles))
	}
	for _, file := range pluginAgentFiles {
		if text := agentFileText(t, root, file); strings.Contains(text, "autoCompactWindow") {
			t.Fatalf("with subagents.compactWindow 0 agents/%s still sets a window of its own:\n%s", file, text)
		}
	}
	before := agentFileText(t, root, "worker.md")
	for _, bad := range []float64{50000, 2000000, 150000.5} {
		if changed := syncAgentWindows(root, object{"subagents": object{"compactWindow": bad}}); changed != 0 || agentFileText(t, root, "worker.md") != before {
			t.Fatalf("subagents.compactWindow %v, which Claude Code would not take, rewrote %d agent files", bad, changed)
		}
	}
	if changed := syncAgentWindows("", loosened); changed != 0 {
		t.Fatalf("with no plugin folder the window sync rewrote %d files", changed)
	}
}

func TestEnsureKeepsTheAgentFilesAtSubagentsCompactWindow(t *testing.T) {
	root := cliPluginTree(t)
	lite := filepath.Join(root, "agents", "lite.md")
	mustWriteJSON(files.config, object{"subagents": object{"compactWindow": float64(300000)}})

	capturedStdout(t, runEnsure)

	if text := string(cliRead(t, lite)); !strings.Contains(text, "\nautoCompactWindow: 300000\nmodel: ") || strings.Count(text, "autoCompactWindow:") != 1 {
		t.Fatalf("ensure did not move the lite agent's compaction point to subagents.compactWindow 300000:\n%s", text)
	}
	before := cliRead(t, lite)
	cliWrite(t, files.config, []byte(`{"subagents": {"compactWindow": 400000,}}`))

	capturedStdout(t, runEnsure)

	cliUnchanged(t, lite, before)
}
