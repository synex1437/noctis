package main

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTheAutoUpdateHintsPointToThePluginMenuInEveryLanguage(t *testing.T) {
	sandboxFiles(t)
	previousLocale, previousArgs, previousRoot := locale, args, files.pluginRoot
	t.Cleanup(func() { locale, args, files.pluginRoot = previousLocale, previousArgs, previousRoot })
	args = parseArgs([]string{"setup"})
	root := filepath.Join(t.TempDir(), "plugins", "cache", "test-mkt", pluginName, "1.0.0")
	files.pluginRoot = root
	t.Setenv("NOCTIS_UPDATE_URL", "https://example.invalid/plugin.json")
	now := nowSec()
	mustWriteJSON(files.release, object{"checkedAt": float64(now), "latest": "99.0.0"})
	menu := "/plugin → Marketplaces → test-mkt → Enable auto-update"

	for _, code := range append([]string{"en", "tr"}, slices.Sorted(maps.Keys(extraCatalogBuilders))...) {
		locale = code
		failed := enableMarketplaceAutoUpdate(root)
		notice := checkForUpdate(loadConfig(), object{}, now)

		for name, text := range map[string]string{"the line setup prints when it cannot switch auto-update on": failed, "the update notice": notice} {
			if !strings.Contains(text, menu) || strings.Contains(text, "--auto-update") {
				t.Errorf("%s: %s should send the person to %s, not to a command Claude Code refuses: %q", code, name, menu, text)
			}
		}
	}
}
