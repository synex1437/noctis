package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type marketplaceBox struct {
	owner, root, known, config string
}

func newMarketplaceBox(t *testing.T) marketplaceBox {
	t.Helper()
	sandboxFiles(t)
	previousLocale, previousArgs := locale, args
	t.Cleanup(func() { locale, args = previousLocale, previousArgs })
	locale, args = "en", parseArgs([]string{"setup"})
	t.Setenv("CLAUDE_CODE_PLUGIN_SEED_DIR", "")
	owner := t.TempDir()
	box := marketplaceBox{
		owner:  owner,
		root:   filepath.Join(owner, "plugins", "cache", "test-mkt", pluginName, "1.0.0"),
		known:  knownMarketplacesFile(owner),
		config: filepath.Join(owner, pluginName, "config.json"),
	}
	mustWriteJSON(box.config, object{})
	return box
}

func (box marketplaceBox) entries(source, field string) string {
	location := filepath.ToSlash(filepath.Join(box.owner, "plugins", "marketplaces"))
	return `{
  "other-mkt": {
    "source": {
      "source": "github",
      "repo": "someone/other"
    },
    "installLocation": "` + location + `/other-mkt",
    "lastUpdated": "2026-09-01T00:00:00.000Z"
  },
  "test-mkt": {
    "source": {
      "source": "` + source + `",
      "repo": "synex1437/noctis"
    },
    "installLocation": "` + location + `/test-mkt",
    "lastUpdated": "2026-09-01T00:00:00.000Z"` + field + `
  }
}`
}

func (box marketplaceBox) write(t *testing.T, field string) []byte {
	t.Helper()
	content := []byte(box.entries("github", field))
	cliWrite(t, box.known, content)
	return content
}

func TestSetupSwitchesMarketplaceAutoUpdateOnInTheFileClaudeCodeReads(t *testing.T) {
	for _, c := range []struct {
		name, field string
		record      object
	}{
		{"no value yet", "", object{"marketplace": "test-mkt"}},
		{"switched off", `,
    "autoUpdate": false`, object{"marketplace": "test-mkt", "previous": false}},
	} {
		t.Run(c.name, func(t *testing.T) {
			box := newMarketplaceBox(t)
			box.write(t, c.field)

			said := enableMarketplaceAutoUpdate(box.root)

			if !strings.Contains(said, "marketplace auto-update on for test-mkt") {
				t.Fatalf("setup said %q, want it to say auto-update is on", said)
			}
			cliUnchanged(t, box.known, []byte(box.entries("github", `,
    "autoUpdate": true`)))
			if statSafe(box.known+".lock") != nil {
				t.Fatalf("setup left %s.lock behind, which would hold Claude Code off the file", box.known)
			}
			if got := getMap(readJSON(box.config), "managedAutoUpdate"); !sameJSON(got, c.record) {
				t.Fatalf("setup recorded %v, want %v, so that an uninstall sets back what it changed", got, c.record)
			}
		})
	}
}

func TestSetupLeavesAMarketplaceAutoUpdateThatIsAlreadyOnAsItIs(t *testing.T) {
	box := newMarketplaceBox(t)
	before := box.write(t, `,
    "autoUpdate": true`)

	if said := enableMarketplaceAutoUpdate(box.root); !strings.Contains(said, "marketplace auto-update on for test-mkt") {
		t.Fatalf("setup said %q, want it to say auto-update is on", said)
	}
	cliUnchanged(t, box.known, before)
	if record := readJSON(box.config)["managedAutoUpdate"]; record != nil {
		t.Fatalf("setup recorded %v for an auto-update it did not switch on, so an uninstall would switch it off", record)
	}
}

func TestSetupWaitsForTheLockClaudeCodeHoldsOnTheMarketplaces(t *testing.T) {
	t.Run("released while setup waits", func(t *testing.T) {
		box := newMarketplaceBox(t)
		box.write(t, "")
		lock := box.known + ".lock"
		if err := os.Mkdir(lock, 0o700); err != nil {
			t.Fatal(err)
		}
		released := make(chan struct{})
		go func() {
			defer close(released)
			time.Sleep(300 * time.Millisecond)
			_ = os.Remove(lock)
		}()

		said := enableMarketplaceAutoUpdate(box.root)
		<-released

		if !strings.Contains(said, "marketplace auto-update on for test-mkt") || getMap(readJSON(box.known), "test-mkt")["autoUpdate"] != true {
			t.Fatalf("setup did not wait for the lock Claude Code let go of: %q", said)
		}
	})

	t.Run("left behind by a process that ended", func(t *testing.T) {
		box := newMarketplaceBox(t)
		box.write(t, "")
		lock := box.known + ".lock"
		if err := os.Mkdir(lock, 0o700); err != nil {
			t.Fatal(err)
		}
		stale := time.Now().Add(-marketplacesLockStaleAfter - time.Second)
		if err := os.Chtimes(lock, stale, stale); err != nil {
			t.Fatal(err)
		}

		said := enableMarketplaceAutoUpdate(box.root)

		if !strings.Contains(said, "marketplace auto-update on for test-mkt") || getMap(readJSON(box.known), "test-mkt")["autoUpdate"] != true {
			t.Fatalf("setup did not take over a lock not touched for longer than Claude Code counts one live: %q", said)
		}
		if statSafe(lock) != nil {
			t.Fatalf("setup left the lock behind")
		}
	})

	t.Run("held all along", func(t *testing.T) {
		box := newMarketplaceBox(t)
		before := box.write(t, "")
		lock := box.known + ".lock"
		if err := os.Mkdir(lock, 0o700); err != nil {
			t.Fatal(err)
		}

		said := enableMarketplaceAutoUpdate(box.root)

		if !strings.Contains(said, "could not enable marketplace auto-update for test-mkt — turn it on in /plugin → Marketplaces → test-mkt") {
			t.Fatalf("setup said %q, want it to send the person to /plugin", said)
		}
		cliUnchanged(t, box.known, before)
		if statSafe(lock) == nil {
			t.Fatalf("setup took away a lock Claude Code still holds")
		}
		if record := readJSON(box.config)["managedAutoUpdate"]; record != nil {
			t.Fatalf("setup recorded %v for an auto-update it did not switch on", record)
		}
	})
}

func TestAnAutoUpdateDeclaredInSettingsDecidesAsInClaudeCode(t *testing.T) {
	declare := func(t *testing.T, file string, autoUpdate bool) {
		t.Helper()
		mustWriteJSON(file, object{"extraKnownMarketplaces": object{"test-mkt": object{
			"source": object{"source": "github", "repo": "synex1437/noctis"}, "autoUpdate": autoUpdate,
		}}})
	}
	for _, c := range []struct {
		name          string
		user, managed any
		want          string
	}{
		{"off in the person's settings", false, nil, "could not enable marketplace auto-update for test-mkt — turn it on in /plugin → Marketplaces → test-mkt"},
		{"on in the person's settings", true, nil, "marketplace auto-update on for test-mkt"},
		{"off in managed settings", true, false, ""},
		{"on in managed settings", false, true, "marketplace auto-update on for test-mkt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			box := newMarketplaceBox(t)
			before := box.write(t, "")
			if declared, ok := c.user.(bool); ok {
				declare(t, filepath.Join(box.owner, "settings.json"), declared)
			}
			if declared, ok := c.managed.(bool); ok {
				managed := t.TempDir()
				t.Setenv(cliChildManaged, managed)
				declare(t, filepath.Join(managed, "managed-settings.json"), declared)
			}

			said := enableMarketplaceAutoUpdate(box.root)

			if (c.want == "" && said != "") || !strings.Contains(said, c.want) {
				t.Fatalf("setup said %q, want %q", said, c.want)
			}
			cliUnchanged(t, box.known, before)
			if record := readJSON(box.config)["managedAutoUpdate"]; record != nil {
				t.Fatalf("setup recorded %v though the settings decide the auto-update", record)
			}
		})
	}
}

func TestMarketplacesClaudeCodeKeepsUpToDateItselfAreNotTouched(t *testing.T) {
	t.Run("seed directory", func(t *testing.T) {
		box := newMarketplaceBox(t)
		before := box.write(t, "")
		t.Setenv("CLAUDE_CODE_PLUGIN_SEED_DIR", t.TempDir()+string(os.PathListSeparator)+filepath.Join(box.owner, "plugins", "marketplaces"))

		if said := enableMarketplaceAutoUpdate(box.root); said != "" {
			t.Fatalf("setup said %q about a marketplace an administrator seeds", said)
		}
		cliUnchanged(t, box.known, before)
	})

	for _, source := range []string{"claudeai", "pluginDirectory"} {
		t.Run(source, func(t *testing.T) {
			box := newMarketplaceBox(t)
			before := []byte(box.entries(source, ""))
			cliWrite(t, box.known, before)

			if said := enableMarketplaceAutoUpdate(box.root); said != "" {
				t.Fatalf("setup said %q about a %s marketplace", said, source)
			}
			cliUnchanged(t, box.known, before)
		})
	}
}

func TestSetupSendsThePersonToThePluginMenuWhereItCannotReadTheMarketplace(t *testing.T) {
	for _, c := range []struct {
		name    string
		content []byte
	}{
		{"no file", nil},
		{"no entry", []byte(`{"other-mkt": {"source": {"source": "github", "repo": "someone/other"}}}`)},
		{"broken file", []byte(`{"test-mkt": {"source": `)},
	} {
		t.Run(c.name, func(t *testing.T) {
			box := newMarketplaceBox(t)
			if c.content != nil {
				cliWrite(t, box.known, c.content)
			}

			if said := enableMarketplaceAutoUpdate(box.root); !strings.Contains(said, "turn it on in /plugin → Marketplaces → test-mkt") {
				t.Fatalf("setup said %q, want it to send the person to /plugin", said)
			}
			if c.content == nil {
				if statSafe(filepath.Dir(box.known)) != nil {
					t.Fatalf("setup made %s for a marketplace Claude Code does not know", filepath.Dir(box.known))
				}
				return
			}
			cliUnchanged(t, box.known, c.content)
		})
	}
}
