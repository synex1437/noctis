package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// i1Link makes link a symbolic link to target, or skips the test on a system that gives none.
func i1Link(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symbolic links on this system: %v", err)
	}
}

func TestAnInstallIntoASkillsFolderLinkedToTheCloneLeavesTheCloneWhole(t *testing.T) {
	box := newCLIBox(t)
	i1Link(t, box.root, filepath.Join(box.account, "skills", pluginName))
	engine := cliRead(t, platformBinary(box.root))

	run := box.run(t, "install", "--source", box.root, "--config-dir", box.account, "--host", "claude", "--profile", "balanced", "--permissions", "keep")

	if placed, err := os.ReadFile(platformBinary(box.root)); err != nil || string(placed) != string(engine) {
		t.Fatalf("an install into skills/%s, a link to the clone, destroyed the clone's own binary (%q, %v):\n%s", pluginName, placed, err, run)
	}
	box.configured(t, run, box.account, "balanced")
	if agent := cliRead(t, filepath.Join(box.root, "agents", "lite.md")); !strings.Contains(string(agent), "name: lite") {
		t.Fatalf("the clone's agent file lost its content: %q", agent)
	}
}

func TestAnInstallRunFromTheInstalledCopyThroughALinkedHomeLeavesTheCopyWhole(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		t.Run(host, func(t *testing.T) {
			box := newCLIBox(t)
			home, flags, installed := box.account, []string{"--host", "claude", "--profile", "balanced", "--permissions", "keep"}, filepath.Join(box.account, "skills", pluginName)
			if host != "claude" {
				home = t.TempDir()
				flags, installed = []string{"--host", host}, filepath.Join(home, pluginName, "plugin")
				box.env["CODEX_HOME"] = home
			}
			first := box.run(t, append([]string{"install", "--source", box.root, "--config-dir", home}, flags...)...)
			if first.code != 0 {
				t.Fatalf("the first install failed:\n%s", first)
			}
			engine := cliRead(t, platformBinary(installed))
			linked := filepath.Join(t.TempDir(), "linked-home")
			i1Link(t, home, linked)
			box.env["NOCTIS_PLUGIN_ROOT"] = installed

			run := box.run(t, append([]string{"install", "--config-dir", linked}, flags...)...)

			if placed, err := os.ReadFile(platformBinary(installed)); err != nil || string(placed) != string(engine) {
				t.Fatalf("an install run from the installed copy into its own home, reached through a link, destroyed the copy's binary (%q, %v):\n%s", placed, err, run)
			}
			if run.code != 0 || strings.Contains(run.stdout, "plugin copied") {
				t.Fatalf("the install should find the copy already in place and go on without copying it onto itself:\n%s", run)
			}
		})
	}
}

func TestCopyingThePluginOntoFoldersLinkedBackToItDeletesNothing(t *testing.T) {
	sandboxFiles(t)
	from, to := t.TempDir(), t.TempDir()
	engine, agent := platformBinary(from), filepath.Join(from, "agents", "lite.md")
	cliWrite(t, engine, []byte("engine"))
	cliWrite(t, agent, []byte("---\nname: lite\n---\n"))
	i1Link(t, filepath.Join(from, "bin"), filepath.Join(to, "bin"))
	i1Link(t, filepath.Join(from, "agents"), filepath.Join(to, "agents"))

	if err := copyPluginTree(from, to); err != nil {
		t.Fatalf("copying the plugin onto folders linked back to it failed: %v", err)
	}

	cliUnchanged(t, engine, []byte("engine"))
	cliUnchanged(t, agent, []byte("---\nname: lite\n---\n"))
}
