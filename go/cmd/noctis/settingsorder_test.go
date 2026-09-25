package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const handOrderedSettings = `{
  "permissions": {
    "defaultMode": "acceptEdits",
    "allow": [
      "Bash(git status)"
    ]
  },
  "model": "claude-fable-5-1",
  "env": {
    "MAX_THINKING_TOKENS": "8000",
    "CLAUDE_CODE_EFFORT_LEVEL": "high"
  },
  "alwaysThinkingEnabled": true,
  "hooks": {
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "say done"
          }
        ]
      }
    ]
  }
}
`

func settingsText(t *testing.T, file string) string {
	t.Helper()
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestAModelChangeLeavesTheRestOfSettingsJSONAsThePersonWroteIt(t *testing.T) {
	sandboxFiles(t)
	previousHost := activeHost
	t.Cleanup(func() { activeHost = previousHost })
	activeHost = "claude"
	if err := os.WriteFile(files.settings, []byte(handOrderedSettings), 0o600); err != nil {
		t.Fatal(err)
	}

	setSettingsModel("sonnet")

	if got, want := settingsText(t, files.settings), strings.Replace(handOrderedSettings, `"claude-fable-5-1"`, `"sonnet"`, 1); got != want {
		t.Fatalf("changing only the model rewrote the rest of settings.json:\n got %s\nwant %s", got, want)
	}

	removeSettingsModel()
	setSettingsEffort("low")
	setSettingsModel("opus")

	want := strings.Replace(strings.Replace(handOrderedSettings, "  \"model\": \"claude-fable-5-1\",\n", "", 1), `"CLAUDE_CODE_EFFORT_LEVEL": "high"`, `"CLAUDE_CODE_EFFORT_LEVEL": "low"`, 1)
	want = strings.Replace(want, "    ]\n  }\n}\n", "    ]\n  },\n  \"model\": \"opus\"\n}\n", 1)
	if got := settingsText(t, files.settings); got != want {
		t.Fatalf("a removed model, a changed effort and a model added back did not keep the other keys where they were, with the new key last:\n got %s\nwant %s", got, want)
	}
}

func TestSetupAndUninstallKeepTheOrderOfTheKeysInSettingsJSON(t *testing.T) {
	sandboxFiles(t)
	account := t.TempDir()
	settingsFile := filepath.Join(account, "settings.json")
	if err := os.WriteFile(settingsFile, []byte(handOrderedSettings), 0o600); err != nil {
		t.Fatal(err)
	}

	recordSetup(t, account, "max", "--permissions", "keep")
	recordUninstall(t, account)

	if got := settingsText(t, settingsFile); got != handOrderedSettings {
		t.Fatalf("setup and uninstall did not leave settings.json as the person wrote it:\n got %s\nwant %s", got, handOrderedSettings)
	}
}
