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
