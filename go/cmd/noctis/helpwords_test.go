package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const referenceURL = "https://github.com/synex1437/noctis/blob/main/docs/REFERENCE.md"

func TestHelpSaysWhatEachCommandDoes(t *testing.T) {
	if minutes, ok := pauseMinutes(""); !ok || minutes != 60 {
		t.Fatalf("noctis off without a duration pauses for %v minutes; the off help line says 60", minutes)
	}
	facts := map[string][]string{
		"help.cmd.off":        {"60", "90m", "2h", "noctis on"},
		"help.cmd.check":      {"10", "11", "20"},
		"help.cmd.checkpoint": {"NONE"},
		"help.cmd.model":      {"--sid <id>"},
		"help.cmd.queue":      {"queue import", "trust", "untrust", "status"},
		"help.cmd.cancel":     {"noctis cancel <id>"},
		"help.cmd.setup":      {"settings.json"},
		"help.cmd.webhook":    {"alarm.webhook.url"},
	}
	for lang, table := range catalogTable() {
		for key, words := range facts {
			for _, word := range words {
				if !strings.Contains(table[key], word) {
					t.Errorf("%s %s does not say %q: %s", lang, key, word, table[key])
				}
			}
		}
	}
}

func TestHelpPointsAtAReferenceTheReaderCanOpen(t *testing.T) {
	shipsDocs := false
	installed, dirs := installedPaths()
	for _, entry := range append(installed, dirs...) {
		shipsDocs = shipsDocs || strings.HasPrefix(filepath.ToSlash(entry), "docs")
	}
	for lang, table := range catalogTable() {
		if more := table["help.more"]; !shipsDocs && !strings.Contains(more, referenceURL) {
			t.Errorf("%s help.more names a reference that installs do not ship: %q", lang, more)
		}
	}
}
