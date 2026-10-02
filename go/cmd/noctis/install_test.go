package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeInstallTree(t *testing.T, engine []byte) (platform, binary string) {
	t.Helper()
	files.pluginRoot = filepath.Join(sandboxFiles(t), "plugin")
	platform = platformBinary(files.pluginRoot)
	binary = filepath.Join(files.pluginRoot, "bin", binaryFileName())
	if err := os.MkdirAll(filepath.Dir(platform), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(platform, engine, 0o755); err != nil {
		t.Fatal(err)
	}
	return platform, binary
}

// shippedName is the name bin/SHA256SUMS gives the binary at path: its path under bin/, the way
// tests/harness.js writes the file.
func shippedName(t *testing.T, root, path string) string {
	t.Helper()
	relative, err := filepath.Rel(filepath.Join(root, "bin"), path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(relative)
}

func writeShippedSum(t *testing.T, sum string) {
	t.Helper()
	line := sum + "  " + shippedName(t, files.pluginRoot, platformBinary(files.pluginRoot)) + "\n"
	if err := os.WriteFile(filepath.Join(files.pluginRoot, "bin", "SHA256SUMS"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loggedErrors() string {
	content, _ := os.ReadFile(files.errors)
	return string(content)
}

func TestEnsureDoesNotReadTheEngineItAlreadyLinked(t *testing.T) {
	platform, binary := fakeInstallTree(t, []byte("engine"))
	if err := os.Link(platform, binary); err != nil {
		t.Skipf("no hard links on this filesystem: %v", err)
	}
	writeShippedSum(t, strings.Repeat("0", 64))
	if err := os.WriteFile(binary+".old", []byte("swapped out"), 0o755); err != nil {
		t.Fatal(err)
	}

	runEnsure()

	if logged := loggedErrors(); strings.Contains(logged, "SHA256SUMS") {
		t.Fatalf("ensure read and hashed the engine bin/%s already links to: %s", binaryFileName(), logged)
	}
	if !os.SameFile(statSafe(platform), statSafe(binary)) {
		t.Fatal("the linked engine was replaced")
	}
	if statSafe(binary+".old") != nil {
		t.Fatal("the engine a Windows swap left behind was not removed")
	}
}

func TestEnsureStillChecksAnEngineThatIsNotLinkedBeforePlacingIt(t *testing.T) {
	engine := []byte("engine")
	_, binary := fakeInstallTree(t, engine)
	launcher := []byte("#!/bin/sh\nexec \"$(dirname \"$0\")/engine\" \"$@\"\n")
	if err := os.WriteFile(binary, launcher, 0o755); err != nil {
		t.Fatal(err)
	}
	writeShippedSum(t, strings.Repeat("0", 64))

	runEnsure()

	if logged := loggedErrors(); !strings.Contains(logged, "SHA256SUMS") {
		t.Fatalf("an engine that fails bin/SHA256SUMS was not refused; errors.log: %q", logged)
	}
	if placed, _ := os.ReadFile(binary); !bytes.Equal(placed, launcher) {
		t.Fatalf("an engine that fails bin/SHA256SUMS replaced the launcher: %q", placed)
	}

	writeShippedSum(t, sha256Of(engine))
	runEnsure()

	if placed, _ := os.ReadFile(binary); !bytes.Equal(placed, engine) {
		t.Fatalf("a verified engine did not replace the launcher: %q", placed)
	}
}

func TestEnsureAddsADefaultSectionTheConfigLacks(t *testing.T) {
	engine := []byte("engine")
	fakeInstallTree(t, engine)
	writeShippedSum(t, sha256Of(engine))
	mustWriteJSON(filepath.Join(files.pluginRoot, "config.default.json"), object{
		"thresholds": object{"session5h": float64(92)},
		"digest":     object{"enabled": true},
	})
	mustWriteJSON(files.config, object{"thresholds": object{"session5h": float64(70)}})

	runEnsure()

	config := readJSON(files.config)
	if got := numberOr(getMap(config, "thresholds"), "session5h", 0); got != 70 {
		t.Fatalf("ensure overwrote the person's threshold with %v", got)
	}
	if !getBool(getMap(config, "digest"), "enabled", false) {
		t.Fatalf("the section config.default.json gained was not added: %v", config)
	}
	logged, _ := os.ReadFile(files.log)
	if !strings.Contains(string(logged), "1 new config section(s) added: digest") {
		t.Fatalf("the added section was not logged: %s", logged)
	}
}

func TestTheRepositoryShipsThisPlatformsBinaryWhereInstallsLookForIt(t *testing.T) {
	root := repoRoot()
	binary := platformBinary(root)
	if statSafe(binary) == nil {
		t.Fatalf("bin/ has no binary for this platform at %s, where an install looks for it", binary)
	}
	if name := shippedName(t, root, binary); shippedChecksum(root, name) == "" {
		t.Fatalf("bin/SHA256SUMS has no line for %s, so an install from this tree would refuse its binary", name)
	}
}

func TestACopyUpdatedOverOneMadeBefore750KeepsNoPerCPUMacOSBinary(t *testing.T) {
	sandboxFiles(t)
	from, to := t.TempDir(), t.TempDir()
	engine := platformBinary(from)
	if err := os.MkdirAll(filepath.Dir(engine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, []byte("7.5 engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, retired := range retiredPlatformFolders {
		old := filepath.Join(to, "bin", retired, "noctis")
		if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(old, []byte("7.4 engine"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyPluginTree(from, to); err != nil {
		t.Fatal(err)
	}
	for _, retired := range retiredPlatformFolders {
		if statSafe(filepath.Join(to, "bin", retired)) != nil {
			t.Fatalf("the copy still has bin/%s after an install over it", retired)
		}
	}
	if copied, err := os.ReadFile(platformBinary(to)); err != nil || string(copied) != "7.5 engine" {
		t.Fatalf("the copy's binary is %q (%v), not the one installed", copied, err)
	}
}

func TestEnsureLeavesAStatusLineThatOnlyMentionsNoctisAsItIs(t *testing.T) {
	cliPluginTree(t)
	binary := filepath.Join(files.pluginRoot, "bin", binaryFileName())
	cliWrite(t, binary, []byte("engine"))
	statusCommand := func() string { return getString(getMap(readJSON(files.settings), "statusLine"), "command") }
	ownScript := filepath.Join(t.TempDir(), "noctis-line.sh")
	cliWrite(t, ownScript, []byte("#!/bin/sh\necho line\n"))
	ownScriptNotSyncedYet := filepath.Join(t.TempDir(), "dotfiles", "noctis-line.sh")
	for _, command := range []string{
		"bash /home/me/.claude/noctis-line.sh",
		`NOCTIS_LANG=tr "` + forwardSlashes(binary) + `" statusline`,
		forwardSlashes(ownScript),
		`"` + forwardSlashes(ownScriptNotSyncedYet) + `" --short`,
	} {
		mustWriteJSON(files.config, object{"statusline": object{"chainCommand": ""}})
		mustWriteJSON(files.settings, object{"statusLine": object{"type": "command", "command": command}})

		capturedStdout(t, func() { firstRunSetup(shippedDefaults(t)) })
		healStatusLine()

		if got := statusCommand(); got != command {
			t.Errorf("a session start rewrote the status line %q to %q", command, got)
		}
	}
	gone := forwardSlashes(filepath.Join(t.TempDir(), "plugins", "cache", pluginName, pluginName, "8.4.0", "bin", binaryFileName()))
	mustWriteJSON(files.settings, object{"statusLine": object{"type": "command", "command": `"` + gone + `" statusline`}})

	healStatusLine()

	if got, want := statusCommand(), `"`+forwardSlashes(binary)+`" statusline`; got != want {
		t.Errorf("a status line on a noctis binary that is gone became %q, want %q", got, want)
	}
}

func TestEnsureKeepsAConfigSectionOfTheWrongTypeForStatusAndDoctorToName(t *testing.T) {
	cliPluginTree(t)
	for _, own := range []any{float64(90), "92", []any{float64(92), float64(95)}, true} {
		mustWriteJSON(files.config, object{"thresholds": own})

		capturedStdout(t, runEnsure)

		if repaired := repairedThresholds(loadConfig()); strings.Join(repaired, ", ") != "thresholds" {
			t.Errorf("after a session start a thresholds value of %v is no longer named (%v); config.json holds %v", own, repaired, readJSON(files.config)["thresholds"])
		}
	}
}

func TestAConfigThatIsNotAnObjectIsNamedAndLeftAsItIs(t *testing.T) {
	cliPluginTree(t)
	for _, mine := range []string{`null`, `[{"mode": "observe", "queue": {"verifyCommand": "make check"}}]`, `"{\"mode\": \"observe\"}"`} {
		cliWrite(t, files.config, []byte(mine))
		if getString(loadConfig(), "configError") == "" {
			t.Errorf("config.json %s is read as no settings at all, and nothing names it", mine)
		}
		capturedStdout(t, runEnsure)
		if after, _ := os.ReadFile(files.config); string(after) != mine {
			t.Errorf("a session start wrote over config.json %s:\n%.200s", mine, after)
		}
	}
}
