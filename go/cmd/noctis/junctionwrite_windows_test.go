//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func junction(t *testing.T, target, link string) {
	t.Helper()
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %v: %s", link, target, err, output)
	}
}

var junctionReadings = []struct {
	name, godebug string
	goFollows     bool
}{
	{"winsymlink 0 as go.mod sets it", "", true},
	{"winsymlink 1 as Go 1.23 and later read links", "winsymlink=1", false},
}

func TestTheLiteAgentMayNotWriteThoseFilesThroughAJunction(t *testing.T) {
	for _, reading := range junctionReadings {
		t.Run(reading.name, func(t *testing.T) {
			t.Setenv("GODEBUG", reading.godebug)
			cfg, project := agentSessionSandbox(t, 10)
			if err := os.MkdirAll(filepath.Join(project, ".claude", "agents"), 0o755); err != nil {
				t.Fatal(err)
			}
			docs := filepath.Join(project, "docs")
			junction(t, filepath.Join(project, ".claude"), docs)
			info, err := os.Lstat(docs)
			if err != nil {
				t.Fatal(err)
			}
			if symlink := info.Mode()&os.ModeSymlink != 0; symlink != reading.goFollows {
				t.Fatalf("Lstat calls the junction a symbolic link: %v, want %v, so this case would not test what its name says", symlink, reading.goFollows)
			}
			if output := liteWrite(t, cfg, project, filepath.Join(docs, "agents", "helper.md")); permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "steer the main model") {
				t.Errorf("the lite agent was allowed to write .claude\\agents\\helper.md through the junction docs: %v", output)
			}
		})
	}
}

func TestClaudeMayNotWriteNoctisOwnStateFileThroughAJunction(t *testing.T) {
	for _, reading := range junctionReadings {
		t.Run(reading.name, func(t *testing.T) {
			t.Setenv("GODEBUG", reading.godebug)
			queueTrustSandbox(t, false)
			if err := os.WriteFile(files.state, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(t.TempDir(), "guard-link")
			junction(t, filepath.Dir(files.state), link)
			path := filepath.Join(link, filepath.Base(files.state))
			viaLink, _ := filepath.EvalSymlinks(path)
			direct, _ := filepath.EvalSymlinks(files.state)
			if follows := viaLink == direct; follows != reading.goFollows {
				t.Fatalf("filepath.EvalSymlinks follows the junction: %v, want %v, so this case would not test what its name says", follows, reading.goFollows)
			}
			run := runHostHook(t, "claude", fileToolCall("of3", "Write", path), "of3", 10*time.Second)
			if permissionOf(run.answer) != "deny" || getString(run.answer, "systemMessage") != T("queue.stateFileByModel", pluginName) {
				t.Errorf("Claude's Write to noctis's state file through the junction %q was not denied: %v", path, run.answer)
			}
		})
	}
}
