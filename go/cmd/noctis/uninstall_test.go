package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func uninstallAccount(t *testing.T) string {
	t.Helper()
	account := t.TempDir()
	marker := filepath.Join(account, "skills", pluginName, "bin", binaryFileName())
	cliWrite(t, marker, []byte("engine"))
	cliWrite(t, filepath.Join(account, "settings.json"), []byte(`{"statusLine": {"type": "command", "command": "\"`+forwardSlashes(marker)+`\" statusline"}}`))
	cliWrite(t, filepath.Join(account, pluginName, "config.json"), []byte(`{"statusline": {"chainCommand": "my-bar"}}`))
	return account
}

func TestUninstallCancelsEveryRelaunchScheduledForTheAccount(t *testing.T) {
	sandboxFiles(t)
	recorded := withFakeScheduler(t, nil)
	previousArgs, previousLocale := args, locale
	t.Cleanup(func() { args, locale = previousArgs, previousLocale })
	args, locale = parseArgs([]string{"install", "--uninstall"}), "en"
	account := uninstallAccount(t)
	sandbox := files
	files = pathsFor(account, files.pluginRoot)
	at := float64(nowSec() + 3600)
	timers := []string{}
	for _, sid := range []string{"first-session", "second-session"} {
		unit := systemdJobUnit(sid, at)
		timers = append(timers, unit+".timer")
		wait := liveWait(3600)
		wait["scheduled"] = object{"method": "systemd", "unit": unit, "at": at}
		updateState(func(state object) { stateMap(state, "waits")[sid] = wait })
	}
	files = sandbox

	var err error
	output := capturedStdout(t, func() { err = uninstallFrom(account) })

	if err != nil {
		t.Fatalf("uninstall failed: %v\n%s", err, output)
	}
	stopped := systemctlStops(*recorded)
	for _, timer := range timers {
		if !slices.Contains(stopped, timer) {
			t.Fatalf("uninstall left %s to relaunch a session after it; stopped: %v\n%s", timer, stopped, output)
		}
	}
	if waits := getMap(readJSON(filepath.Join(account, pluginName, "state.json")), "waits"); len(waits) != 0 {
		t.Fatalf("uninstall left the waits in state.json: %v", waits)
	}
	if !strings.Contains(output, "pending relaunches cancelled: 2") {
		t.Fatalf("uninstall did not say how many relaunches it cancelled:\n%s", output)
	}
}

func TestAHostUninstallCancelsTheRelaunchesOfItsAccountToo(t *testing.T) {
	root := cliPluginTree(t)
	codexHome := t.TempDir()
	marker := filepath.Join(codexHome, pluginName, "plugin", "bin", binaryFileName())
	cliWrite(t, marker, []byte("engine"))
	cliWrite(t, filepath.Join(codexHome, "hooks.json"), []byte(`{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "\"`+forwardSlashes(marker)+`\" hook --host codex", "statusMessage": "noctis"}]}]}}`))
	state, err := json.Marshal(object{"waits": object{cliLongSid: cliWait()}})
	if err != nil {
		t.Fatal(err)
	}
	cliWrite(t, filepath.Join(codexHome, pluginName, "state.json"), state)

	run := runNoctisCLI(t, map[string]string{"NOCTIS_PLUGIN_ROOT": root, "CODEX_HOME": codexHome}, "install", "--uninstall", "--config-dir", codexHome, "--host", "codex")

	if run.code != 0 || !strings.Contains(run.stdout, "pending relaunches cancelled: 1") {
		t.Fatalf("a codex uninstall did not cancel the pending relaunch and say so:\n%s", run)
	}
	if waits := getMap(cliState(t, codexHome), "waits"); len(waits) != 0 {
		t.Fatalf("a codex uninstall left the wait whose relaunch still fires: %v", waits)
	}
}

func TestUninstallKeepsThePluginWhenTheCancelledRelaunchesCannotBeSaved(t *testing.T) {
	sandboxFiles(t)
	recorded := withFakeScheduler(t, nil)
	previousArgs, previousLocale, previousFailures := args, locale, writeFailures
	t.Cleanup(func() { args, locale, writeFailures = previousArgs, previousLocale, previousFailures })
	args, locale = parseArgs([]string{"install", "--uninstall"}), "en"
	account := uninstallAccount(t)
	marker := filepath.Join(account, "skills", pluginName, "bin", binaryFileName())
	settingsFile := filepath.Join(account, "settings.json")
	settings, err := os.ReadFile(settingsFile)
	if err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(account, pluginName, "state.json")
	wait := liveWait(3600)
	wait["scheduled"] = object{"method": "systemd", "unit": "noctis-unit", "at": wait["resumeAt"]}
	state, err := json.Marshal(object{"waits": object{cliLongSid: wait}})
	if err != nil {
		t.Fatal(err)
	}
	cliWrite(t, stateFile, state)
	if err := os.MkdirAll(fmt.Sprintf("%s.%d.tmp", stateFile, os.Getpid()), 0o755); err != nil {
		t.Fatal(err)
	}

	var uninstallErr error
	output := capturedStdout(t, func() { uninstallErr = uninstallFrom(account) })

	if uninstallErr == nil || !strings.Contains(uninstallErr.Error(), "left in place") {
		t.Fatalf("uninstall did not stop and say so though the cancelled relaunches could not be saved: %v\n%s", uninstallErr, output)
	}
	if statSafe(marker) == nil {
		t.Fatal("uninstall removed the plugin though the relaunch it could not cancel is still pending")
	}
	cliUnchanged(t, settingsFile, settings)
	if stopped := systemctlStops(*recorded); len(stopped) != 0 {
		t.Fatalf("uninstall stopped %v though state.json still lists the wait", stopped)
	}
	if waits := getMap(readJSON(stateFile), "waits"); len(waits) != 1 {
		t.Fatalf("the wait in state.json changed though it could not be saved: %v", waits)
	}
}

func TestUninstallKeepsTheStateFolderAndSaysWhatItHoldsAndHowToRemoveIt(t *testing.T) {
	root := cliPluginTree(t)
	account := uninstallAccount(t)
	guardDir := filepath.Join(account, pluginName)
	cliWrite(t, filepath.Join(guardDir, "guard.log"), []byte("a log line\n"))

	run := runNoctisCLI(t, cliAccountEnv(root, account), "install", "--uninstall", "--config-dir", account, "--host", "claude")

	if run.code != 0 {
		t.Fatalf("uninstall failed:\n%s", run)
	}
	for _, kept := range []string{"config.json", "guard.log"} {
		if statSafe(filepath.Join(guardDir, kept)) == nil {
			t.Fatalf("uninstall without --purge removed %s from the state folder:\n%s", kept, run)
		}
	}
	for _, said := range []string{"state folder kept: " + guardDir, "logs", "--purge"} {
		if !strings.Contains(run.stdout, said) {
			t.Fatalf("uninstall left the state folder without saying %q:\n%s", said, run)
		}
	}
}

func TestPurgeRemovesTheStateFolderButNothingALinkInsideItLeadsTo(t *testing.T) {
	root := cliPluginTree(t)
	account := uninstallAccount(t)
	guardDir := filepath.Join(account, pluginName)
	cliWrite(t, filepath.Join(guardDir, "state.json"), []byte(`{}`))
	cliWrite(t, filepath.Join(guardDir, "queues", "prompt.md"), []byte("- [ ] an item\n"))
	outside := t.TempDir()
	mine := filepath.Join(outside, "mine.txt")
	cliWrite(t, mine, []byte("not noctis's"))
	if err := os.Symlink(outside, filepath.Join(guardDir, "linked")); err != nil {
		t.Skipf("no symlink on this system: %v", err)
	}

	run := runNoctisCLI(t, cliAccountEnv(root, account), "install", "--uninstall", "--purge", "--config-dir", account, "--host", "claude")

	if run.code != 0 || !strings.Contains(run.stdout, "state folder removed: "+guardDir) {
		t.Fatalf("uninstall --purge did not remove the state folder and say so:\n%s", run)
	}
	if _, err := os.Lstat(guardDir); !os.IsNotExist(err) {
		t.Fatalf("uninstall --purge left %s (%v):\n%s", guardDir, err, run)
	}
	cliUnchanged(t, mine, []byte("not noctis's"))
	if got := getString(getMap(readJSON(filepath.Join(account, "settings.json")), "statusLine"), "command"); got != "my-bar" {
		t.Fatalf("uninstall --purge did not put the status line back first: %q", got)
	}
}

func TestPurgeLeavesAStateFolderThatIsALink(t *testing.T) {
	root := cliPluginTree(t)
	account := t.TempDir()
	marker := filepath.Join(account, "skills", pluginName, "bin", binaryFileName())
	cliWrite(t, marker, []byte("engine"))
	cliWrite(t, filepath.Join(account, "settings.json"), []byte(`{"statusLine": {"type": "command", "command": "\"`+forwardSlashes(marker)+`\" statusline"}}`))
	outside := t.TempDir()
	cliWrite(t, filepath.Join(outside, "config.json"), []byte(`{}`))
	cliWrite(t, filepath.Join(outside, "state.json"), []byte(`{}`))
	guardDir := filepath.Join(account, pluginName)
	if err := os.Symlink(outside, guardDir); err != nil {
		t.Skipf("no symlink on this system: %v", err)
	}

	run := runNoctisCLI(t, cliAccountEnv(root, account), "install", "--uninstall", "--purge", "--config-dir", account, "--host", "claude")

	if run.code != 0 || !strings.Contains(run.stdout, "left as it is") {
		t.Fatalf("uninstall --purge did not say it left a state folder that is a link:\n%s", run)
	}
	if info, err := os.Lstat(guardDir); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("uninstall --purge removed the link %s (%v)", guardDir, err)
	}
	if statSafe(filepath.Join(outside, "state.json")) == nil {
		t.Fatalf("uninstall --purge followed the link out of the account and removed what it leads to:\n%s", run)
	}
}

func TestPurgeWithoutUninstallChangesNothing(t *testing.T) {
	box := newCLIBox(t)

	run := box.run(t, "install", "--purge", "--source", box.root, "--config-dir", box.account, "--host", "claude")

	if run.code != 2 || !strings.Contains(run.stderr, "--purge only goes with --uninstall") {
		t.Fatalf("install --purge without --uninstall was not refused with the reason:\n%s", run)
	}
	box.untouched(t, run)
}

func fakeMarketplaceClaude(t *testing.T, box cliBox, known, switchedOn string, refused bool) {
	t.Helper()
	bin := filepath.Dir(box.calls)
	if isWindows {
		refusal := ""
		if refused {
			refusal = "exit /b 1\r\n"
		}
		cliWrite(t, filepath.Join(bin, "claude.cmd"), []byte("@echo off\r\necho %*>>\""+box.calls+"\"\r\necho %*| findstr /c:\"--auto-update\" >nul || exit /b 0\r\n"+refusal+"copy /y \""+switchedOn+"\" \""+known+"\" >nul\r\n"))
		return
	}
	refusal := ""
	if refused {
		refusal = "exit 1\n"
	}
	script := filepath.Join(bin, "claude")
	cliWrite(t, script, []byte("#!/bin/sh\necho \"$*\" >> \""+box.calls+"\"\ncase \"$*\" in *--auto-update*) ;; *) exit 0 ;; esac\n"+refusal+"cp \""+switchedOn+"\" \""+known+"\"\n"))
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallGivesBackTheMarketplaceAutoUpdateOnlyWhenSetupSwitchedItOn(t *testing.T) {
	cases := []struct {
		name, before, later, want string
		refused, givenBack        bool
	}{
		{"setup switched it on where it had no value", "", "", "", false, true},
		{"setup switched it on where it was off", "false", "", "false", false, true},
		{"it was on before setup", "true", "", "true", false, false},
		{"the person switched it off after setup", "", "false", "false", false, false},
		{"setup could not switch it on and the person did later", "false", "true", "true", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			box := newCLIBox(t)
			root := filepath.Join(box.account, "plugins", "cache", "test-mkt", pluginName, pluginVersion)
			if err := copyTree(box.root, root); err != nil {
				t.Fatal(err)
			}
			box.env["NOCTIS_PLUGIN_ROOT"] = root
			known := filepath.Join(box.account, "plugins", "known_marketplaces.json")
			entry := func(autoUpdate string) []byte {
				field := ""
				if autoUpdate != "" {
					field = `, "autoUpdate": ` + autoUpdate
				}
				return []byte(`{"test-mkt": {"source": {"source": "github", "repo": "synex1437/noctis"}, "lastUpdated": "2026-09-01T00:00:00.000Z"` + field + `}}`)
			}
			cliWrite(t, known, entry(c.before))
			switchedOn := filepath.Join(t.TempDir(), "on.json")
			cliWrite(t, switchedOn, entry("true"))
			fakeMarketplaceClaude(t, box, known, switchedOn, c.refused)
			setup := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep")
			box.configured(t, setup, box.account, "balanced")
			if c.later != "" {
				cliWrite(t, known, entry(c.later))
			}

			run := box.run(t, "install", "--uninstall", "--config-dir", box.account, "--host", "claude")

			if run.code != 0 {
				t.Fatalf("uninstall failed:\n%s", run)
			}
			marketplace := getMap(readJSON(known), "test-mkt")
			got := ""
			if value, had := marketplace["autoUpdate"]; had {
				got = fmt.Sprint(value)
			}
			if got != c.want {
				t.Fatalf("autoUpdate is %q after the uninstall, want %q\nsetup:\n%s\nuninstall:\n%s", got, c.want, setup, run)
			}
			if getString(marketplace, "lastUpdated") != "2026-09-01T00:00:00.000Z" || getString(getMap(marketplace, "source"), "repo") != "synex1437/noctis" {
				t.Fatalf("the uninstall changed more of the marketplace entry than autoUpdate: %v", marketplace)
			}
			if said := strings.Contains(run.stdout, "auto-update"); said != c.givenBack {
				t.Fatalf("the uninstall mentioned auto-update=%v, want %v:\n%s", said, c.givenBack, run)
			}
		})
	}
}

func TestSetupSaysWhenItCannotRecordTheAutoUpdateItSwitchedOn(t *testing.T) {
	box := newCLIBox(t)
	owner := t.TempDir()
	root := filepath.Join(owner, "plugins", "cache", "test-mkt", pluginName, pluginVersion)
	if err := copyTree(box.root, root); err != nil {
		t.Fatal(err)
	}
	box.env["NOCTIS_PLUGIN_ROOT"] = root
	known := filepath.Join(owner, "plugins", "known_marketplaces.json")
	cliWrite(t, known, []byte(`{"test-mkt": {"source": {"source": "github", "repo": "synex1437/noctis"}}}`))
	switchedOn := filepath.Join(t.TempDir(), "on.json")
	cliWrite(t, switchedOn, []byte(`{"test-mkt": {"source": {"source": "github", "repo": "synex1437/noctis"}, "autoUpdate": true}}`))
	fakeMarketplaceClaude(t, box, known, switchedOn, false)

	run := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep")

	box.configured(t, run, box.account, "balanced")
	if !strings.Contains(run.stdout, "could not record that it switched marketplace auto-update on for test-mkt") {
		t.Fatalf("setup switched auto-update on where no noctis config could hold the record, without saying the uninstall will not undo it:\n%s", run)
	}
	if statSafe(filepath.Join(owner, pluginName)) != nil {
		t.Fatalf("setup created a noctis folder in %s, an account it was not given:\n%s", owner, run)
	}
}

func TestUninstallSaysItLeftTheAutoUpdateWhenItCannotReadTheMarketplaces(t *testing.T) {
	box := newCLIBox(t)
	root := filepath.Join(box.account, "plugins", "cache", "test-mkt", pluginName, pluginVersion)
	if err := copyTree(box.root, root); err != nil {
		t.Fatal(err)
	}
	box.env["NOCTIS_PLUGIN_ROOT"] = root
	known := filepath.Join(box.account, "plugins", "known_marketplaces.json")
	cliWrite(t, known, []byte(`{"test-mkt": {"source": {"source": "github", "repo": "synex1437/noctis"}}}`))
	switchedOn := filepath.Join(t.TempDir(), "on.json")
	cliWrite(t, switchedOn, []byte(`{"test-mkt": {"source": {"source": "github", "repo": "synex1437/noctis"}, "autoUpdate": true}}`))
	fakeMarketplaceClaude(t, box, known, switchedOn, false)
	box.configured(t, box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep"), box.account, "balanced")
	broken := []byte(`{"test-mkt": {"autoUpdate": true`)
	cliWrite(t, known, broken)

	run := box.run(t, "install", "--uninstall", "--config-dir", box.account, "--host", "claude")

	if run.code != 0 || !strings.Contains(run.stdout, "marketplace auto-update for test-mkt left as it is: "+known) {
		t.Fatalf("the uninstall did not go on and say it left the auto-update it could not set back:\n%s", run)
	}
	cliUnchanged(t, known, broken)
}
