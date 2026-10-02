package main

import (
	"path/filepath"
	"testing"
)

func TestHostInstallAndUninstallKeepTheUsersOwnHooksThatMentionNoctis(t *testing.T) {
	userCommands := []string{"~/bin/noctis-notify.sh --sound", "noctis status --json >> ~/limits.log"}
	for _, host := range []string{"codex", "droid"} {
		t.Run(host, func(t *testing.T) {
			sandboxFiles(t)
			dir := t.TempDir()
			file := filepath.Join(dir, "hooks.json")
			stopGroups := func() []any {
				root := readJSON(file)
				if host == "codex" {
					root = getMap(root, "hooks")
				}
				return getList(root, "Stop")
			}
			commandCounts := func() map[string]int {
				counts := map[string]int{}
				for _, group := range stopGroups() {
					for _, handler := range getList(toObject(group), "hooks") {
						counts[getString(toObject(handler), "command")]++
					}
				}
				return counts
			}
			userGroups := []any{}
			for _, command := range userCommands {
				userGroups = append(userGroups, object{"hooks": []any{object{"type": "command", "command": command}}})
			}
			if host == "codex" {
				mustWriteJSON(file, object{"hooks": object{"Stop": userGroups}})
			} else {
				mustWriteJSON(file, object{"Stop": userGroups})
			}
			binary := filepath.Join(dir, "Program Files", pluginName, "bin", binaryFileName())

			for round := 1; round <= 2; round++ {
				if _, err := wireHostHooks(host, binary, dir); err != nil {
					t.Fatal(err)
				}
				counts := commandCounts()
				for _, command := range userCommands {
					if counts[command] != 1 {
						t.Fatalf("install %d dropped the user's own Stop hook %q: Stop is %v", round, command, stopGroups())
					}
				}
				if len(stopGroups()) != len(userCommands)+1 {
					t.Fatalf("install %d left %d Stop groups, want the user's %d and one of noctis's: %v", round, len(stopGroups()), len(userCommands), stopGroups())
				}
			}
			if _, err := unwireHostHooks(host, dir); err != nil {
				t.Fatal(err)
			}

			counts := commandCounts()
			if len(stopGroups()) != len(userCommands) || counts[userCommands[0]] != 1 || counts[userCommands[1]] != 1 {
				t.Fatalf("uninstall should leave exactly the user's own Stop hooks, Stop is %v", stopGroups())
			}
		})
	}
}

func TestDoctorSeesThePluginsHooksOnlyWhileTheyAreWired(t *testing.T) {
	userCommand := "noctis status --json >> ~/limits.log"
	for _, host := range []string{"codex", "droid", "antigravity"} {
		t.Run(host, func(t *testing.T) {
			sandboxFiles(t)
			dir := t.TempDir()
			file := filepath.Join(dir, "hooks.json")
			switch host {
			case "codex":
				mustWriteJSON(file, object{"hooks": object{"Stop": []any{object{"hooks": []any{object{"type": "command", "command": userCommand}}}}}})
			case "droid":
				mustWriteJSON(file, object{"Stop": []any{object{"hooks": []any{object{"type": "command", "command": userCommand}}}}})
			default:
				t.Setenv("NOCTIS_ANTIGRAVITY_HOOKS", file)
				mustWriteJSON(file, object{"alerts": object{"Stop": []any{object{"type": "command", "command": userCommand}}}})
			}
			binary := filepath.Join(dir, "Program Files", pluginName, "bin", binaryFileName())
			for _, step := range []struct {
				name  string
				wired bool
				run   func() error
			}{
				{"before install", false, func() error { return nil }},
				{"after install", true, func() error { _, err := wireHostHooks(host, binary, dir); return err }},
				{"after uninstall", false, func() error { _, err := unwireHostHooks(host, dir); return err }},
			} {
				if err := step.run(); err != nil {
					t.Fatal(err)
				}
				if wired, _ := hostHooksWired(host, dir); wired != step.wired {
					t.Errorf("%s, doctor takes the hooks for wired: %v, want %v; %s holds %v", step.name, wired, step.wired, file, readJSON(file))
				}
			}
		})
	}
}

func TestAntigravityUninstallKeepsAStatusLineOfTheUsersThatMentionsNoctis(t *testing.T) {
	sandboxFiles(t)
	dir := t.TempDir()
	t.Setenv("NOCTIS_ANTIGRAVITY_HOOKS", filepath.Join(dir, "hooks.json"))
	settingsFile := filepath.Join(dir, "settings.json")
	binary := filepath.Join(dir, "Program Files", pluginName, "bin", binaryFileName())
	if _, err := wireHostHooks("antigravity", binary, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := unwireHostHooks("antigravity", dir); err != nil {
		t.Fatal(err)
	}
	if line := readJSON(settingsFile)["statusLine"]; line != nil {
		t.Fatalf("uninstall left the plugin's own status line: %v", line)
	}
	own := "bash ~/.gemini/noctis-line.sh"
	mustWriteJSON(settingsFile, object{"statusLine": object{"type": "command", "command": own}})
	if _, err := unwireHostHooks("antigravity", dir); err != nil {
		t.Fatal(err)
	}
	if command := getString(getMap(readJSON(settingsFile), "statusLine"), "command"); command != own {
		t.Errorf("uninstall removed the user's own status line %q: settings.json holds %v", own, readJSON(settingsFile))
	}
}
