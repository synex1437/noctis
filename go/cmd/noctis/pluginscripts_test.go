package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	if marker := os.Getenv("NOCTIS_TEST_POWERSHELL_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
		os.Exit(0)
	}
	if os.Getenv("NOCTIS_TEST_REPORT_SCRIPTS") != "" {
		initPaths()
		fmt.Printf("%s\n%s\n%s\n", files.pluginRoot, files.notifyScript, files.launchScript)
		os.Exit(0)
	}
}

func copyTestBinary(t *testing.T, target string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return target
}

func plantPluginLayout(t *testing.T, root string) {
	t.Helper()
	cliWrite(t, filepath.Join(root, "config.default.json"), []byte(`{}`))
	cliWrite(t, filepath.Join(root, "hooks", "hooks.json"), []byte(`{"hooks": {}}`))
	cliWrite(t, filepath.Join(root, "scripts", "notify.ps1"), []byte("exit 0\n"))
	cliWrite(t, filepath.Join(root, "scripts", "launch.ps1"), []byte("exit 0\n"))
}

func scriptsSeenBy(t *testing.T, binary string) (root, notifyScript, launchScript string) {
	t.Helper()
	child := exec.Command(binary)
	child.Env = append(os.Environ(), "NOCTIS_TEST_REPORT_SCRIPTS=1", "NOCTIS_PLUGIN_ROOT=", "CLAUDE_PLUGIN_ROOT=", "CLAUDE_CONFIG_DIR="+t.TempDir())
	output, err := child.Output()
	if err != nil {
		t.Fatalf("%s did not report its paths: %v", binary, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	if len(lines) != 4 || lines[3] != "" {
		t.Fatalf("%s reported %q", binary, output)
	}
	return lines[0], lines[1], lines[2]
}

func TestTheWindowsScriptsRunOnlyFromThePluginFolderThatHoldsTheBinary(t *testing.T) {
	bare := copyTestBinary(t, filepath.Join(t.TempDir(), "tools", "sub", binaryFileName()))
	root, notifyScript, launchScript := scriptsSeenBy(t, bare)
	if notifyScript != "" || launchScript != "" {
		t.Fatalf("a binary copied to %s fell back to %s, which is no plugin folder, and would run %q and %q with the execution policy bypassed", bare, root, notifyScript, launchScript)
	}

	planted := t.TempDir()
	plantPluginLayout(t, planted)
	copied := copyTestBinary(t, filepath.Join(planted, "tools", "sub", binaryFileName()))
	root, notifyScript, launchScript = scriptsSeenBy(t, copied)
	if notifyScript != "" || launchScript != "" {
		t.Fatalf("a binary copied to %s took %s, a folder that only looks like a plugin, as its root and would run %q and %q with the execution policy bypassed", copied, root, notifyScript, launchScript)
	}

	plugin := filepath.Join(t.TempDir(), "plugin")
	plantPluginLayout(t, plugin)
	for _, binary := range []string{filepath.Join(plugin, "bin", binaryFileName()), filepath.Join(plugin, "bin", filepathPlatform(), binaryFileName())} {
		root, notifyScript, launchScript = scriptsSeenBy(t, copyTestBinary(t, binary))
		if !resolvesTo(root, plugin) || notifyScript != filepath.Join(root, "scripts", "notify.ps1") || launchScript != filepath.Join(root, "scripts", "launch.ps1") {
			t.Fatalf("the binary at %s did not get its own plugin's scripts: root %q, %q, %q", binary, root, notifyScript, launchScript)
		}
	}
}

func filepathPlatform() string {
	return filepath.Base(filepath.Dir(platformBinary("")))
}

func TestTheWindowsLauncherIsNotRunWithoutATrustedScript(t *testing.T) {
	sandboxFiles(t)
	bin := t.TempDir()
	copyTestBinary(t, filepath.Join(bin, "powershell.exe"))
	marker := filepath.Join(t.TempDir(), "ran")
	t.Setenv("NOCTIS_TEST_POWERSHELL_MARKER", marker)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	previousPid := launchPidTimeout
	launchPidTimeout = time.Second
	t.Cleanup(func() { launchPidTimeout = previousPid })
	planted := t.TempDir()
	plantPluginLayout(t, planted)
	files.pluginRoot = planted
	files.launchScript = ""

	started := launchInWindowsTerminal(object{"resume": object{"terminal": "console"}}, launchSpec{sid: "s1", cwd: t.TempDir()}, "claude", nil, os.Environ(), "max")

	if started {
		t.Fatal("a relaunch window was reported without a launcher script")
	}
	if ran, err := os.ReadFile(marker); err == nil {
		t.Fatalf("PowerShell was started without a trusted launcher script: %s", ran)
	}
}
