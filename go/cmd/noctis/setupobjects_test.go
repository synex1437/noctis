package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestUninstallRemovesTheEnvAndPermissionsSetupAddedOnceTheyAreEmpty(t *testing.T) {
	sandboxFiles(t)
	recordClaudeWithAuto(t)
	account := recordAccount(t, object{"theme": "dark"})
	settingsFile := filepath.Join(account, "settings.json")

	recordSetup(t, account, "max")
	recordSetup(t, account, "low")
	if settings := readJSON(settingsFile); getMap(settings, "env") == nil || getMap(settings, "permissions") == nil {
		t.Fatalf("setup did not write env and permissions: %v", settings)
	}
	after, _ := recordUninstall(t, account)

	if want := (object{"theme": "dark"}); !reflect.DeepEqual(after, want) {
		t.Fatalf("settings.json held only the theme before setup; after setup, setup again and uninstall it is %v", after)
	}
}

func TestUninstallKeepsTheEnvAndPermissionsTheUserHadOrFilledSinceSetup(t *testing.T) {
	sandboxFiles(t)
	recordClaudeWithAuto(t)
	t.Run("empty before setup", func(t *testing.T) {
		account := recordAccount(t, object{"env": object{}, "permissions": object{}})

		recordSetup(t, account, "max")
		after, _ := recordUninstall(t, account)

		if want := (object{"env": object{}, "permissions": object{}}); !reflect.DeepEqual(after, want) {
			t.Fatalf("the user's own empty env and permissions should stay through setup and uninstall: %v", after)
		}
	})
	t.Run("filled after setup", func(t *testing.T) {
		account := recordAccount(t, object{"theme": "dark"})
		settingsFile := filepath.Join(account, "settings.json")
		recordSetup(t, account, "max")
		settings := readJSON(settingsFile)
		getMap(settings, "env")["MY_TOOL_HOME"] = "/opt/tool"
		getMap(settings, "permissions")["allow"] = []any{"Bash(npm test)"}
		mustWriteJSON(settingsFile, settings)

		after, _ := recordUninstall(t, account)

		want := object{"theme": "dark", "env": object{"MY_TOOL_HOME": "/opt/tool"}, "permissions": object{"allow": []any{"Bash(npm test)"}}}
		if !reflect.DeepEqual(after, want) {
			t.Fatalf("what the user put into env and permissions after setup should stay: %v", after)
		}
	})
}
