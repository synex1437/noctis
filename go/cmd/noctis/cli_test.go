package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const cliChildArgs = "NOCTIS_CLI_TEST_ARGS"

func TestCLIChildRunsTheCommandItWasHanded(t *testing.T) {
	raw := os.Getenv(cliChildArgs)
	if raw == "" {
		t.Skip("the child side of the command-line tests")
	}
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		t.Fatal(err)
	}
	os.Args = append([]string{os.Args[0]}, argv...)
	main()
}

type cliRun struct {
	code           int
	stdout, stderr string
}

func runNoctisCLI(t *testing.T, env map[string]string, argv ...string) cliRun {
	t.Helper()
	return startNoctisCLI(t, env, argv...)()
}

func startNoctisCLI(t *testing.T, env map[string]string, argv ...string) func() cliRun {
	t.Helper()
	return startNoctisCLIAt(t, t.TempDir(), "", env, argv...)
}

func startNoctisCLIAt(t *testing.T, home, input string, env map[string]string, argv ...string) func() cliRun {
	t.Helper()
	encoded, err := json.Marshal(argv)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestCLIChildRunsTheCommandItWasHanded$")
	child.Dir = home
	if input != "" {
		child.Stdin = strings.NewReader(input)
	}
	child.Env = append(os.Environ(), cliChildArgs+"="+string(encoded), "NOCTIS_LANG=en", "NOCTIS_HOST=", "NOCTIS_NO_WATCHER=1", "NOCTIS_NO_TASKS=1",
		"CLAUDE_CONFIG_DIR=", "CLAUDE_PLUGIN_ROOT=", "NOCTIS_PLUGIN_ROOT=",
		"CLAUDE_CODE_OAUTH_TOKEN=test", "HOME="+home, "USERPROFILE="+home, "CODEX_HOME="+filepath.Join(home, ".codex"), "COPILOT_HOME="+filepath.Join(home, ".copilot"))
	for key, value := range env {
		child.Env = append(child.Env, key+"="+value)
	}
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	if err := child.Start(); err != nil {
		t.Fatalf("the command-line child did not start: %v", err)
	}
	return func() cliRun {
		t.Helper()
		err := child.Wait()
		var exit *exec.ExitError
		switch {
		case err == nil:
			return cliRun{0, stdout.String(), stderr.String()}
		case errors.As(err, &exit):
			return cliRun{exit.ExitCode(), stdout.String(), stderr.String()}
		}
		t.Fatalf("the command-line child did not finish: %v", err)
		return cliRun{}
	}
}

func (run cliRun) String() string {
	return fmt.Sprintf("exit %d\nstdout:\n%s\nstderr:\n%s", run.code, run.stdout, run.stderr)
}

func cliWrite(t *testing.T, file string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func cliRead(t *testing.T, file string) []byte {
	t.Helper()
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func cliUnchanged(t *testing.T, file string, want []byte) {
	t.Helper()
	if got := cliRead(t, file); !bytes.Equal(got, want) {
		t.Fatalf("%s was changed:\nbefore: %s\nafter:  %s", file, want, got)
	}
}

func cliPluginTree(t *testing.T) string {
	t.Helper()
	engine := []byte("engine")
	fakeInstallTree(t, engine)
	writeShippedSum(t, sha256Of(engine))
	root := files.pluginRoot
	encoded, err := json.Marshal(shippedDefaults(t))
	if err != nil {
		t.Fatal(err)
	}
	cliWrite(t, filepath.Join(root, "config.default.json"), encoded)
	cliWrite(t, filepath.Join(root, "hooks", "hooks.json"), []byte(`{"hooks": {"SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/bin/noctis", "args": ["ensure"], "timeout": 15}]}]}}`))
	cliWrite(t, filepath.Join(root, "agents", "lite.md"), []byte("---\nname: lite\nmodel: sonnet\neffort: high\n---\n\nResearch on the model the economy profile picked.\n"))
	return root
}

func cliAccountEnv(root, account string) map[string]string {
	return map[string]string{"NOCTIS_PLUGIN_ROOT": root, "CLAUDE_CONFIG_DIR": account}
}

var cliForeignSettings = []byte(`{
  "model": "sonnet",
  "env": {"CLAUDE_CODE_EFFORT_LEVEL": "medium"},
  "permissions": {"defaultMode": "default"},
  "statusLine": {"type": "command", "command": "npx -y ccstatusline@latest"}
}
`)

var cliUnreadableConfigs = []struct {
	name  string
	plant func(t *testing.T, guardDir string) []byte
}{
	{"a trailing comma", func(t *testing.T, guardDir string) []byte {
		content := []byte(`{"thresholds": {"session5h": 90,}}`)
		cliWrite(t, filepath.Join(guardDir, "config.json"), content)
		return content
	}},
	{"a file where its folder belongs", func(t *testing.T, guardDir string) []byte {
		cliWrite(t, guardDir, []byte("not a folder"))
		return nil
	}},
}

func TestEnsureLeavesAConfigItCannotReadAsItIs(t *testing.T) {
	root := cliPluginTree(t)
	broken := []byte("{\n  \"thresholds\": {\"session5h\": 80, \"weeklyAll\": 75,},\n  \"report\": {\"webhook\": \"https://example.invalid/hook\"},\n}\n")
	cliWrite(t, files.config, broken)
	cliWrite(t, files.settings, cliForeignSettings)
	lite := filepath.Join(root, "agents", "lite.md")
	agent := cliRead(t, lite)

	capturedStdout(t, runEnsure)

	cliUnchanged(t, files.config, broken)
	cliUnchanged(t, files.settings, cliForeignSettings)
	cliUnchanged(t, lite, agent)

	cliWrite(t, files.config, []byte(`{"thresholds": {"session5h": 80, "weeklyAll": 75}}`))
	capturedStdout(t, runEnsure)

	config := readJSON(files.config)
	if chained := getString(getMap(config, "statusline"), "chainCommand"); chained != "npx -y ccstatusline@latest" {
		t.Fatalf("once the config reads again, ensure should chain the status line it replaces, got %q", chained)
	}
	if got := numberOr(getMap(config, "thresholds"), "session5h", 0); got != 80 {
		t.Fatalf("ensure replaced the person's threshold with %v", got)
	}
	if command := getString(getMap(readJSON(files.settings), "statusLine"), "command"); !strings.Contains(command, pluginName) {
		t.Fatalf("once the config reads again, ensure should wire the status line, got %q", command)
	}
}

func TestEnsureLeavesTheAgentsAloneWhileTheConfigCannotBeRead(t *testing.T) {
	root := cliPluginTree(t)
	broken := []byte(`{"roles": {"profile": "economy", "research": {"model": "sonnet", "effort": "high"},},}`)
	cliWrite(t, files.config, broken)
	settings := []byte(`{"statusLine": {"type": "command", "command": "\"` + forwardSlashes(filepath.Join(root, "bin", binaryFileName())) + `\" statusline"}}`)
	cliWrite(t, files.settings, settings)
	lite := filepath.Join(root, "agents", "lite.md")
	agent := cliRead(t, lite)

	capturedStdout(t, runEnsure)

	cliUnchanged(t, lite, agent)
	cliUnchanged(t, files.config, broken)
	cliUnchanged(t, files.settings, settings)
}

func TestEnsureKeepsAStatusLineWhoseChainItCannotSave(t *testing.T) {
	cliPluginTree(t)
	cliWrite(t, filepath.Join(files.pluginRoot, "bin", binaryFileName()), []byte("engine"))
	cliWrite(t, files.config, []byte(`{"thresholds": {"session5h": 80}}`))
	cliWrite(t, files.settings, cliForeignSettings)
	if err := os.MkdirAll(fmt.Sprintf("%s.%d.tmp", files.config, os.Getpid()), 0o755); err != nil {
		t.Fatal(err)
	}

	capturedStdout(t, func() { firstRunSetup(shippedDefaults(t)) })

	cliUnchanged(t, files.settings, cliForeignSettings)
}

func TestSetupRefusesAConfigItCannotRead(t *testing.T) {
	root := cliPluginTree(t)
	for _, unreadable := range cliUnreadableConfigs {
		t.Run(unreadable.name, func(t *testing.T) {
			account := t.TempDir()
			cliWrite(t, filepath.Join(account, "settings.json"), cliForeignSettings)
			config := unreadable.plant(t, filepath.Join(account, pluginName))

			run := runNoctisCLI(t, cliAccountEnv(root, account), "setup", "--config-dir", account, "--profile", "economy", "--no-ask")

			if run.code != 1 || !strings.Contains(run.stderr, "config.json") || strings.Contains(run.stdout, "setup complete") {
				t.Fatalf("setup went on past a config.json it cannot read:\n%s", run)
			}
			cliUnchanged(t, filepath.Join(account, "settings.json"), cliForeignSettings)
			if config != nil {
				cliUnchanged(t, filepath.Join(account, pluginName, "config.json"), config)
			}
		})
	}
}

func TestInstallRefusesAConfigItCannotRead(t *testing.T) {
	root := cliPluginTree(t)
	for _, unreadable := range cliUnreadableConfigs {
		t.Run(unreadable.name, func(t *testing.T) {
			account := t.TempDir()
			cliWrite(t, filepath.Join(account, "settings.json"), cliForeignSettings)
			config := unreadable.plant(t, filepath.Join(account, pluginName))

			run := runNoctisCLI(t, cliAccountEnv(root, account), "install", "--source", root, "--config-dir", account, "--host", "claude", "--profile", "economy", "--no-ask")

			if run.code != 1 || !strings.Contains(run.stderr, "config.json") {
				t.Fatalf("install went on past a config.json it cannot read:\n%s", run)
			}
			cliUnchanged(t, filepath.Join(account, "settings.json"), cliForeignSettings)
			if config != nil {
				cliUnchanged(t, filepath.Join(account, pluginName, "config.json"), config)
				if statSafe(filepath.Join(account, "skills", pluginName)) != nil {
					t.Fatalf("install copied the plugin before it found config.json unreadable:\n%s", run)
				}
			}
		})
	}
}

func TestAHostInstallRefusesAConfigItCannotRead(t *testing.T) {
	root := cliPluginTree(t)
	codexHome := t.TempDir()
	hooks := []byte(`{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "python3 other.py"}]}]}}`)
	cliWrite(t, filepath.Join(codexHome, "hooks.json"), hooks)
	config := cliUnreadableConfigs[0].plant(t, filepath.Join(codexHome, pluginName))

	run := runNoctisCLI(t, map[string]string{"NOCTIS_PLUGIN_ROOT": root, "CODEX_HOME": codexHome}, "install", "--source", root, "--config-dir", codexHome, "--host", "codex")

	if run.code != 1 || !strings.Contains(run.stderr, "config.json") {
		t.Fatalf("a codex install went on past a config.json it cannot read:\n%s", run)
	}
	cliUnchanged(t, filepath.Join(codexHome, "hooks.json"), hooks)
	cliUnchanged(t, filepath.Join(codexHome, pluginName, "config.json"), config)
}

func TestUninstallKeepsThePluginWhenSettingsCannotBeRead(t *testing.T) {
	root := cliPluginTree(t)
	account := t.TempDir()
	marker := filepath.Join(account, "skills", pluginName, "bin", binaryFileName())
	cliWrite(t, marker, []byte("engine"))
	settings := []byte(`{"statusLine": {"type": "command", "command": "\"` + forwardSlashes(marker) + `\" statusline"}, "permissions": {"defaultMode": "auto"},}`)
	cliWrite(t, filepath.Join(account, "settings.json"), settings)
	cliWrite(t, filepath.Join(account, pluginName, "config.json"), []byte(`{"managedPermissionMode": "auto", "managedPermissionPrevious": "plan"}`))

	run := runNoctisCLI(t, cliAccountEnv(root, account), "install", "--uninstall", "--config-dir", account, "--host", "claude")

	if run.code != 1 || !strings.Contains(run.stderr, "settings.json") {
		t.Fatalf("uninstall did not stop at a settings.json it cannot read:\n%s", run)
	}
	if statSafe(marker) == nil {
		t.Fatalf("uninstall removed the plugin the status line still runs:\n%s", run)
	}
	cliUnchanged(t, filepath.Join(account, "settings.json"), settings)
}

func TestUninstallChangesNothingWhenTheConfigCannotBeRead(t *testing.T) {
	root := cliPluginTree(t)
	account := t.TempDir()
	marker := filepath.Join(account, "skills", pluginName, "bin", binaryFileName())
	cliWrite(t, marker, []byte("engine"))
	settings := []byte(`{"statusLine": {"type": "command", "command": "\"` + forwardSlashes(marker) + `\" statusline"}, "model": "opus", "env": {"CLAUDE_CODE_EFFORT_LEVEL": "max"}, "permissions": {"defaultMode": "auto"}}`)
	cliWrite(t, filepath.Join(account, "settings.json"), settings)
	config := []byte(`{"managedPermissionMode": "auto", "managedPermissionPrevious": "plan", "managedModel": {"previous": "sonnet", "set": "opus"},}`)
	cliWrite(t, filepath.Join(account, pluginName, "config.json"), config)

	run := runNoctisCLI(t, cliAccountEnv(root, account), "install", "--uninstall", "--config-dir", account, "--host", "claude")

	if run.code != 1 || !strings.Contains(run.stderr, "config.json") {
		t.Fatalf("uninstall did not stop at the config.json that records what setup changed:\n%s", run)
	}
	if statSafe(marker) == nil {
		t.Fatalf("uninstall removed the plugin:\n%s", run)
	}
	cliUnchanged(t, filepath.Join(account, "settings.json"), settings)
	cliUnchanged(t, filepath.Join(account, pluginName, "config.json"), config)
}

func TestUninstallPutsBackWhatSetupRecordedAndThenRemovesThePlugin(t *testing.T) {
	root := cliPluginTree(t)
	account := t.TempDir()
	marker := filepath.Join(account, "skills", pluginName, "bin", binaryFileName())
	cliWrite(t, marker, []byte("engine"))
	cliWrite(t, filepath.Join(account, "settings.json"), []byte(`{"statusLine": {"type": "command", "command": "\"`+forwardSlashes(marker)+`\" statusline"}, "model": "opus", "env": {"CLAUDE_CODE_EFFORT_LEVEL": "max"}, "permissions": {"defaultMode": "auto", "allow": ["Bash(npm test)"]}}`))
	cliWrite(t, filepath.Join(account, pluginName, "config.json"), []byte(`{"statusline": {"chainCommand": "my-bar"}, "managedEffort": {"previous": "medium", "set": "max"}, "managedPermissionMode": "auto", "managedPermissionPrevious": "plan", "managedModel": {"previous": "sonnet", "set": "opus"}}`))

	run := runNoctisCLI(t, cliAccountEnv(root, account), "install", "--uninstall", "--config-dir", account, "--host", "claude")

	if run.code != 0 {
		t.Fatalf("uninstall failed:\n%s", run)
	}
	if statSafe(filepath.Join(account, "skills", pluginName)) != nil {
		t.Fatalf("uninstall left the plugin behind:\n%s", run)
	}
	settings := readJSON(filepath.Join(account, "settings.json"))
	if got := getString(getMap(settings, "statusLine"), "command"); got != "my-bar" {
		t.Fatalf("the chained status line was not put back: %q", got)
	}
	if got := getString(settings, "model"); got != "sonnet" {
		t.Fatalf("the model was not put back: %q", got)
	}
	if got := getString(getMap(settings, "env"), "CLAUDE_CODE_EFFORT_LEVEL"); got != "medium" {
		t.Fatalf("the effort was not put back: %q", got)
	}
	if permissions := getMap(settings, "permissions"); getString(permissions, "defaultMode") != "plan" || len(getList(permissions, "allow")) != 1 {
		t.Fatalf("the permission mode was not put back: %v", permissions)
	}
}

func TestAHostUninstallKeepsThePluginWhenItsHookFileCannotBeRead(t *testing.T) {
	root := cliPluginTree(t)
	codexHome := t.TempDir()
	marker := filepath.Join(codexHome, pluginName, "plugin", "bin", binaryFileName())
	cliWrite(t, marker, []byte("engine"))
	hooks := []byte(`{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "\"` + forwardSlashes(marker) + `\" hook --host codex", "statusMessage": "noctis"}]}],}}`)
	cliWrite(t, filepath.Join(codexHome, "hooks.json"), hooks)

	run := runNoctisCLI(t, map[string]string{"NOCTIS_PLUGIN_ROOT": root, "CODEX_HOME": codexHome}, "install", "--uninstall", "--config-dir", codexHome, "--host", "codex")

	if run.code != 1 || !strings.Contains(run.stderr, "hooks.json") {
		t.Fatalf("a codex uninstall did not stop at a hook file it cannot read:\n%s", run)
	}
	if statSafe(marker) == nil {
		t.Fatalf("a codex uninstall removed the plugin its hooks still run:\n%s", run)
	}
	cliUnchanged(t, filepath.Join(codexHome, "hooks.json"), hooks)
}

func TestAnAntigravityUninstallReadsBothFilesBeforeItChangesEither(t *testing.T) {
	root := cliPluginTree(t)
	home := t.TempDir()
	marker := filepath.Join(home, pluginName, "plugin", "bin", binaryFileName())
	cliWrite(t, marker, []byte("engine"))
	hooksFile := filepath.Join(t.TempDir(), "hooks.json")
	hooks := []byte(`{"noctis": {"Stop": [{"type": "command", "command": "\"` + forwardSlashes(marker) + `\" hook --host antigravity"}]}, "my-linter": {}}`)
	cliWrite(t, hooksFile, hooks)
	settings := []byte(`{"statusLine": {"command": "\"` + forwardSlashes(marker) + `\" statusline --host antigravity"},}`)
	cliWrite(t, filepath.Join(home, "settings.json"), settings)

	run := runNoctisCLI(t, map[string]string{"NOCTIS_PLUGIN_ROOT": root, "NOCTIS_ANTIGRAVITY_HOOKS": hooksFile}, "install", "--uninstall", "--config-dir", home, "--host", "antigravity")

	if run.code != 1 || !strings.Contains(run.stderr, "settings.json") {
		t.Fatalf("an antigravity uninstall did not stop at a settings.json it cannot read:\n%s", run)
	}
	cliUnchanged(t, hooksFile, hooks)
	cliUnchanged(t, filepath.Join(home, "settings.json"), settings)
	if statSafe(marker) == nil {
		t.Fatalf("an antigravity uninstall removed the plugin its status line still runs:\n%s", run)
	}
}

func TestUninstallKeepsThePluginWhenSettingsCannotBeWritten(t *testing.T) {
	sandboxFiles(t)
	account := t.TempDir()
	marker := filepath.Join(account, "skills", pluginName, "bin", binaryFileName())
	cliWrite(t, marker, []byte("engine"))
	settingsFile := filepath.Join(account, "settings.json")
	settings := []byte(`{"statusLine": {"type": "command", "command": "\"` + forwardSlashes(marker) + `\" statusline"}}`)
	cliWrite(t, settingsFile, settings)
	if err := os.MkdirAll(fmt.Sprintf("%s.%d.tmp", settingsFile, os.Getpid()), 0o755); err != nil {
		t.Fatal(err)
	}

	var err error
	capturedStdout(t, func() { err = uninstallFrom(account) })

	if err == nil {
		t.Fatal("uninstall reported success though settings.json could not be written")
	}
	if statSafe(marker) == nil {
		t.Fatal("uninstall removed the plugin the status line still runs")
	}
	cliUnchanged(t, settingsFile, settings)
}

func TestSettingsStayAsTheyWereWhenWhatSetupChangesCannotBeRecorded(t *testing.T) {
	sandboxFiles(t)
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs([]string{"setup", "--permissions", "keep"})
	account := t.TempDir()
	settingsFile := filepath.Join(account, "settings.json")
	cliWrite(t, settingsFile, cliForeignSettings)
	blocked := filepath.Join(account, pluginName)
	cliWrite(t, blocked, []byte("a file, so nothing can be written under it"))
	defaults := shippedDefaults(t)

	var err error
	capturedStdout(t, func() {
		err = wireSettings(account, filepath.Join(account, "bin", binaryFileName()), cloneObject(defaults), filepath.Join(blocked, "config.json"), defaults, false)
	})

	if err == nil || !strings.Contains(err.Error(), "config.json") {
		t.Fatalf("setup went on to settings.json though config.json, which records what it changes there, could not be written: %v", err)
	}
	cliUnchanged(t, settingsFile, cliForeignSettings)
}

func TestTheDoctorSendsABrokenConfigBackToTheFileNotToSetup(t *testing.T) {
	sandboxFiles(t)
	previous := locale
	t.Cleanup(func() { locale = previous })
	locale = "en"
	cliWrite(t, files.config, []byte(`{"thresholds": {"session5h": 90,}}`))

	lines := doctorConfigLines(object{"configError": "invalid character '}' looking for beginning of object key string"})

	if len(lines) != 2 || !strings.HasPrefix(lines[0], "!!") || !strings.Contains(lines[0], "invalid character") {
		t.Fatalf("a broken config.json is not reported with its error: %q", lines)
	}
	if strings.Contains(lines[1], "setup") || !strings.Contains(lines[1], "config.json") {
		t.Fatalf("the fix for a broken config.json should point at the file, not at setup, which refuses to run on it: %q", lines[1])
	}

	if err := os.Remove(files.config); err != nil {
		t.Fatal(err)
	}
	lines = doctorConfigLines(object{})
	if len(lines) != 2 || !strings.Contains(lines[1], "setup") {
		t.Fatalf("a missing config.json should still send the person to setup: %q", lines)
	}
}

const cliLongSid = "1a2b3c4d-5e6f-7a8b-9c0d-111122223333"

func cliWait() object {
	wait := liveWait(3600)
	wait["label"] = "5h"
	wait["scheduled"] = object{"method": "manual", "at": wait["resumeAt"]}
	return wait
}

func cliStateAccount(t *testing.T, state object) (string, map[string]string) {
	t.Helper()
	account := t.TempDir()
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	cliWrite(t, filepath.Join(account, pluginName, "state.json"), encoded)
	return account, map[string]string{"CLAUDE_CONFIG_DIR": account, "NOCTIS_TIME_OFFSET": ""}
}

func cliState(t *testing.T, account string) object {
	t.Helper()
	var state object
	if err := json.Unmarshal(cliRead(t, filepath.Join(account, pluginName, "state.json")), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func cliFirstLine(text string) string {
	return strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
}

func TestCancelTakesTheShortIdThatStatusShows(t *testing.T) {
	account, env := cliStateAccount(t, object{
		"waits":       object{cliLongSid: cliWait()},
		"checkpoints": object{cliLongSid: object{"at": float64(nowSec()), "path": filepath.Join(t.TempDir(), "checkpoint.md")}},
	})
	if status := runNoctisCLI(t, env, "status"); !strings.Contains(status.stdout, "  - 1a2b3c4d ") || strings.Contains(status.stdout, cliLongSid) {
		t.Fatalf("status no longer lists the wait under the eight-character id this test hands to cancel:\n%s", status)
	}

	run := runNoctisCLI(t, env, "cancel", "1a2b3c4d")

	if run.code != 0 || !strings.Contains(run.stdout, "Cancelled: 1a2b3c4d") {
		t.Fatalf("cancel did not take the id status shows:\n%s", run)
	}
	if waits := getMap(cliState(t, account), "waits"); len(waits) != 0 {
		t.Fatalf("cancel reported the wait cancelled, but it is still there and its relaunch still fires: %v", waits)
	}
	if status := runNoctisCLI(t, env, "status"); strings.Contains(status.stdout, "1a2b3c4d") {
		t.Fatalf("status still lists the cancelled wait:\n%s", status)
	}
}

func TestCancelFindsTheSessionItIsGivenOrSaysItDidNot(t *testing.T) {
	cases := []struct {
		name      string
		waits     []string
		arg       string
		code      int
		cancelled []string
		say       []string
	}{
		{"an exact id wins over a longer one it starts", []string{"abcd", "abcd1"}, "abcd", 0, []string{"abcd"}, []string{"Cancelled: abcd"}},
		{"an id with spaces reaches the name it is stored under", []string{"weird_id"}, "weird id", 0, []string{"weird_id"}, []string{"Cancelled: weird_id"}},
		{"the start of one id", []string{cliLongSid, "9f8e7d6c-0000"}, "1a2b3c4d", 0, []string{cliLongSid}, []string{"Cancelled: 1a2b3c4d"}},
		{"a start two ids share cancels neither", []string{"abcd1-first", "abcd2-second"}, "abcd", 2, nil, []string{"abcd1-first", "abcd2-second"}},
		{"under four characters only an exact id counts", []string{"abc123"}, "abc", 0, nil, []string{"Nothing pending for abc"}},
		{"an id nothing is pending for", []string{"abcd1"}, "does-not-exist", 0, nil, []string{"Nothing pending for does-not-exist"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			waits := object{}
			for _, sid := range c.waits {
				waits[sid] = cliWait()
			}
			account, env := cliStateAccount(t, object{"waits": waits})

			run := runNoctisCLI(t, env, "cancel", c.arg)

			if run.code != c.code {
				t.Fatalf("cancel %q: want exit %d:\n%s", c.arg, c.code, run)
			}
			for _, text := range c.say {
				if !strings.Contains(run.stdout+run.stderr, text) {
					t.Fatalf("cancel %q does not say %q:\n%s", c.arg, text, run)
				}
			}
			if len(c.cancelled) == 0 && strings.Contains(run.stdout, "Cancelled") {
				t.Fatalf("cancel %q reported a cancellation that did not happen:\n%s", c.arg, run)
			}
			left := getMap(cliState(t, account), "waits")
			for _, sid := range c.waits {
				_, kept := left[sid]
				if gone := indexOf(c.cancelled, sid) >= 0; gone == kept {
					t.Fatalf("cancel %q: wait %s kept=%v, want kept=%v; waits now %v", c.arg, sid, kept, !gone, left)
				}
			}
		})
	}
}

func TestCancelClearsTheFailedRelaunchStatusReports(t *testing.T) {
	for _, argv := range [][]string{{"cancel", "x"}, {"cancel"}} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			_, env := cliStateAccount(t, object{"launchFailures": object{"x": object{"at": float64(nowSec()), "model": "opus"}}})
			if status := runNoctisCLI(t, env, "status"); !strings.Contains(status.stdout, "automatic relaunch of x failed") {
				t.Fatalf("status does not report the failed relaunch this test clears:\n%s", status)
			}

			run := runNoctisCLI(t, env, argv...)

			if run.code != 0 || !strings.Contains(run.stdout, "Cancelled: x") {
				t.Fatalf("%s did not clear the failed relaunch:\n%s", strings.Join(argv, " "), run)
			}
			if status := runNoctisCLI(t, env, "status"); strings.Contains(status.stdout, "automatic relaunch") {
				t.Fatalf("%s left the failed relaunch in status:\n%s", strings.Join(argv, " "), status)
			}
		})
	}
}

func TestCheckpointAndModelTakeTheShortIdToo(t *testing.T) {
	checkpoint := filepath.Join(t.TempDir(), "checkpoint.md")
	cliWrite(t, checkpoint, []byte("# checkpoint of the long session\n"))
	account, env := cliStateAccount(t, object{
		"checkpoints": object{
			cliLongSid:     object{"at": float64(nowSec()), "path": checkpoint},
			"abcd1-first":  object{"at": float64(nowSec()), "path": checkpoint},
			"abcd2-second": object{"at": float64(nowSec()), "path": checkpoint},
		},
		"modelOverrides": object{
			cliLongSid:     object{"model": "claude-sonnet-5", "at": float64(nowSec())},
			"abcd1-first":  object{"model": "claude-opus-5", "at": float64(nowSec())},
			"abcd2-second": object{"model": "claude-opus-5", "at": float64(nowSec())},
		},
	})
	cliWrite(t, filepath.Join(account, pluginName, "usage.json"), []byte(`{"sessions": {"only-in-usage": {"model": "claude-haiku-4-5", "updatedAt": 1}}}`))

	if run := runNoctisCLI(t, env, "checkpoint", "--sid", "1a2b3c4d"); run.code != 0 || !strings.Contains(run.stdout, "checkpoint of the long session") {
		t.Fatalf("checkpoint --sid did not find the session by the id status shows:\n%s", run)
	}
	if run := runNoctisCLI(t, env, "model", "--sid", "1a2b3c4d"); run.code != 0 || cliFirstLine(run.stdout) != "claude-sonnet-5" {
		t.Fatalf("model --sid did not find the session by the id status shows:\n%s", run)
	}
	if run := runNoctisCLI(t, env, "model", "--sid", "only-in-usage"); run.code != 0 || cliFirstLine(run.stdout) != "claude-haiku-4-5" {
		t.Fatalf("model --sid lost a session the status line reported:\n%s", run)
	}
	for _, argv := range [][]string{{"checkpoint", "--sid", "abcd"}, {"model", "--sid", "abcd"}} {
		if run := runNoctisCLI(t, env, argv...); run.code != 2 || !strings.Contains(run.stderr, "abcd1-first") || !strings.Contains(run.stderr, "abcd2-second") || strings.Contains(run.stdout, "checkpoint of") || strings.Contains(run.stdout, "claude-") {
			t.Fatalf("%s with an id two sessions share should name both and answer for neither:\n%s", strings.Join(argv, " "), run)
		}
	}
}

func TestOffReadsTheDurationItIsGivenOrChangesNothing(t *testing.T) {
	accepted := []struct {
		request []string
		minutes float64
	}{
		{nil, 60},
		{[]string{"30"}, 30},
		{[]string{"2h"}, 120},
		{[]string{"90m"}, 90},
		{[]string{"1h30m"}, 90},
		{[]string{"2", "hours"}, 120},
		{[]string{"1", "day"}, 1440},
		{[]string{"0.5"}, 1},
		{[]string{"7d"}, 7 * 1440},
	}
	for _, c := range accepted {
		argv := append([]string{"off"}, c.request...)
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			account, env := cliStateAccount(t, object{})
			start := float64(time.Now().Unix())
			run := runNoctisCLI(t, env, argv...)
			end := float64(time.Now().Unix())

			until := numberOr(cliState(t, account), "disabledUntil", 0)
			if run.code != 0 || !strings.Contains(run.stdout, "disabled until") || until-end > c.minutes*60 || until-start < c.minutes*60 {
				t.Fatalf("%s: want a pause of %v minutes, got one of %.1f to %.1f:\n%s", strings.Join(argv, " "), c.minutes, (until-end)/60, (until-start)/60, run)
			}
		})
	}
	for _, request := range []string{"abc", "nan", "inf", "-5", "0", "30d", "1e300"} {
		t.Run("off "+request, func(t *testing.T) {
			account, env := cliStateAccount(t, object{"disabledUntil": float64(0)})

			run := runNoctisCLI(t, env, "off", request)

			if run.code != 2 || !strings.Contains(run.stderr, "usage: noctis off") || strings.Contains(run.stdout, "disabled until") {
				t.Fatalf("off %s should be refused with the usage line and exit 2:\n%s", request, run)
			}
			if until := numberOr(cliState(t, account), "disabledUntil", 0); until != 0 {
				t.Fatalf("off %s was refused, yet the guard is paused until %v", request, until)
			}
		})
	}
}

func TestOffAndCancelSayWhenNothingWasSaved(t *testing.T) {
	t.Run("state.json cannot be read", func(t *testing.T) {
		account := t.TempDir()
		if err := os.MkdirAll(filepath.Join(account, pluginName, "state.json"), 0o755); err != nil {
			t.Fatal(err)
		}

		run := runNoctisCLI(t, map[string]string{"CLAUDE_CONFIG_DIR": account}, "off", "30")

		if run.code != 1 || strings.Contains(run.stdout, "disabled until") || !strings.Contains(run.stderr, "still on") {
			t.Fatalf("off reported a pause it could not save:\n%s", run)
		}
	})
	t.Run("state.lock is held by a live process", func(t *testing.T) {
		account, env := cliStateAccount(t, object{"waits": object{cliLongSid: cliWait()}})
		stateFile := filepath.Join(account, pluginName, "state.json")
		before := cliRead(t, stateFile)
		cliWrite(t, filepath.Join(account, pluginName, "state.lock"), []byte(strconv.Itoa(os.Getpid())))

		off, cancel := startNoctisCLI(t, env, "off", "30"), startNoctisCLI(t, env, "cancel", "1a2b3c4d")
		offRun, cancelRun := off(), cancel()

		if offRun.code != 1 || strings.Contains(offRun.stdout, "disabled until") || !strings.Contains(offRun.stderr, "still on") {
			t.Fatalf("off reported a pause it could not save:\n%s", offRun)
		}
		if cancelRun.code != 1 || strings.Contains(cancelRun.stdout, "Cancelled") || !strings.Contains(cancelRun.stderr, "Nothing was cancelled") {
			t.Fatalf("cancel reported a cancellation it could not save:\n%s", cancelRun)
		}
		cliUnchanged(t, stateFile, before)
	})
}

func capturedStderr(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	captured := make(chan string, 1)
	go func() {
		output, _ := io.ReadAll(reader)
		reader.Close()
		captured <- string(output)
	}()
	previous := os.Stderr
	os.Stderr = writer
	func() {
		defer func() {
			os.Stderr = previous
			writer.Close()
		}()
		run()
	}()
	return <-captured
}

func TestCancelAndOffDoNotReportAStateWriteThatFailed(t *testing.T) {
	sandboxFiles(t)
	previousLocale, previousFailures := locale, writeFailures
	t.Cleanup(func() { locale, writeFailures = previousLocale, previousFailures })
	locale = "en"
	updateState(func(state object) { stateMap(state, "waits")[cliLongSid] = cliWait() })
	before := cliRead(t, files.state)
	if err := os.MkdirAll(fmt.Sprintf("%s.%d.tmp", files.state, os.Getpid()), 0o755); err != nil {
		t.Fatal(err)
	}

	code := 0
	var stderr string
	stdout := capturedStdout(t, func() { stderr = capturedStderr(t, func() { code = cancelPending("1a2b3c4d") }) })
	if code != 1 || strings.Contains(stdout, "Cancelled") || !strings.Contains(stderr, "Nothing was cancelled") {
		t.Fatalf("cancel reported a cancellation state.json never received: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	cliUnchanged(t, files.state, before)

	stdout = capturedStdout(t, func() { stderr = capturedStderr(t, func() { code = pauseGuard("30") }) })
	if code != 1 || strings.Contains(stdout, "disabled until") || !strings.Contains(stderr, "still on") {
		t.Fatalf("off reported a pause state.json never received: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	cliUnchanged(t, files.state, before)
}

func cliFakeClaude(t *testing.T) (string, string) {
	t.Helper()
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls.log")
	name, script := "claude", "#!/bin/sh\necho \"$*\" >> \""+calls+"\"\n"
	if isWindows {
		name, script = "claude.cmd", "@echo off\r\necho %*>>\""+calls+"\"\r\n"
	}
	cliWrite(t, filepath.Join(bin, name), []byte(script))
	if err := os.Chmod(filepath.Join(bin, name), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin + string(os.PathListSeparator) + os.Getenv("PATH"), calls
}

type cliBox struct {
	root, home, account, calls string
	env                        map[string]string
}

func newCLIBox(t *testing.T) cliBox {
	t.Helper()
	root := cliPluginTree(t)
	path, calls := cliFakeClaude(t)
	box := cliBox{root: root, home: t.TempDir(), account: t.TempDir(), calls: calls, env: map[string]string{"NOCTIS_PLUGIN_ROOT": root, "PATH": path, "NOCTIS_USAGE_URL": "http://127.0.0.1:9/usage", "NOCTIS_UPDATE_URL": "off"}}
	cliWrite(t, filepath.Join(box.account, "settings.json"), cliForeignSettings)
	return box
}

func (box cliBox) run(t *testing.T, argv ...string) cliRun {
	t.Helper()
	return startNoctisCLIAt(t, box.home, "", box.env, argv...)()
}

func (box cliBox) expand(argv []string) []string {
	replacer := strings.NewReplacer("ACCOUNT", box.account, "ROOT", box.root)
	expanded := make([]string, len(argv))
	for index, arg := range argv {
		expanded[index] = replacer.Replace(arg)
	}
	return expanded
}

func (box cliBox) untouched(t *testing.T, run cliRun) {
	t.Helper()
	cliUnchanged(t, filepath.Join(box.account, "settings.json"), cliForeignSettings)
	for _, written := range []string{filepath.Join(box.account, pluginName), filepath.Join(box.account, "skills"), filepath.Join(box.account, "hooks.json"),
		filepath.Join(box.home, ".claude"), filepath.Join(box.home, ".codex"), filepath.Join(box.home, "~"), filepath.Join(box.home, "--profile")} {
		if statSafe(written) != nil {
			t.Fatalf("%s was written by a command that should have changed nothing:\n%s", written, run)
		}
	}
}

func (box cliBox) configured(t *testing.T, run cliRun, account, profile string) {
	t.Helper()
	if run.code != 0 {
		t.Fatalf("the command failed:\n%s", run)
	}
	config := readJSON(filepath.Join(account, pluginName, "config.json"))
	if got := getString(section(config, "roles"), "profile"); got != profile {
		t.Fatalf("%s got the %q profile, want %q:\n%s", account, got, profile, run)
	}
	if command := getString(getMap(readJSON(filepath.Join(account, "settings.json")), "statusLine"), "command"); !strings.Contains(command, pluginName) {
		t.Fatalf("%s did not get the status line: %q\n%s", account, command, run)
	}
	if account != filepath.Join(box.home, ".claude") && statSafe(filepath.Join(box.home, ".claude")) != nil {
		t.Fatalf("the default account was written although %s was named:\n%s", account, run)
	}
}

func TestSetupAndInstallChangeNothingWhenTheyDoNotUnderstandTheirArguments(t *testing.T) {
	cases := []struct {
		argv []string
		env  map[string]string
		say  []string
	}{
		{[]string{"setup", "--config-dir", "ACCOUNT", "--profle", "economy"}, nil, []string{"--profle", "did you mean --profile?"}},
		{[]string{"setup", "--config-dir", "ACCOUNT", "--permisions", "keep"}, nil, []string{"--permisions", "did you mean --permissions?"}},
		{[]string{"setup", "--config-dir", "ACCOUNT", "--permissions", "kep"}, nil, []string{"--permissions kep", "acceptEdits"}},
		{[]string{"setup", "--config-dir", "ACCOUNT", "--permissions", "bypassPermissions"}, nil, []string{"--permissions bypassPermissions"}},
		{[]string{"setup", "--config-dir", "ACCOUNT", "--updates", "no"}, nil, []string{"--updates no"}},
		{[]string{"setup", "--config-dir", "ACCOUNT", "--profile", "noctis", "economy"}, nil, []string{"unexpected word economy", "did you mean --profile economy?"}},
		{[]string{"setup", "--config-dir", "ACCOUNT", "--no-ask", "economy"}, nil, []string{"unexpected word economy"}},
		{[]string{"setup", "--config-dir", "ACCOUNT", "--no-model=yes"}, nil, []string{"--no-model"}},
		{[]string{"setup", "--config-dir", "--profile", "economy"}, nil, []string{"--config-dir"}},
		{[]string{"setup", "--config-dir=", "--profile", "economy"}, nil, []string{"--config-dir"}},
		{[]string{"setup", "--account", "ACCOUNT", "--profile", "economy", "--config-dir"}, nil, []string{"--config-dir"}},
		{[]string{"install", "--source", "ROOT", "--config-dir", "ACCOUNT", "--host", "claude", "--uninstal"}, nil, []string{"--uninstal", "did you mean --uninstall?"}},
		{[]string{"install", "--source", "ROOT", "--config-dir", "ACCOUNT", "--host", "codx"}, nil, []string{"codx", "codex"}},
		{[]string{"install", "--source", "ROOT", "--config-dir", "ACCOUNT", "--host", "Codex CLI"}, nil, []string{"Codex CLI", "codex"}},
		{[]string{"setup", "--config-dir", "ACCOUNT", "--profile", "economy"}, map[string]string{"NOCTIS_HOST": "codx"}, []string{"NOCTIS_HOST", "codx", "codex"}},
		{[]string{"setup", "--config-dir", "ACCOUNT", "--profile", "economy", "--host", "codx"}, map[string]string{"NOCTIS_HOST": "codex"}, []string{"codx"}},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.argv, " "), func(t *testing.T) {
			box := newCLIBox(t)
			for key, value := range c.env {
				box.env[key] = value
			}

			run := box.run(t, box.expand(c.argv)...)

			if run.code != 2 || strings.Contains(run.stdout, "complete") {
				t.Fatalf("want exit 2 with nothing done:\n%s", run)
			}
			for _, text := range c.say {
				if !strings.Contains(run.stderr, text) {
					t.Fatalf("stderr does not say %q:\n%s", text, run)
				}
			}
			box.untouched(t, run)
		})
	}
}

func TestSetupChecksItsValuesBeforeItWritesAnything(t *testing.T) {
	for _, bad := range [][]string{{"--preset", "weird", "weird"}, {"--profile", "bogus", "bogus"}, {"--code", "opus:ultra", "ultra"}, {"--research", ":high", ":high"}} {
		t.Run(strings.Join(bad[:2], " "), func(t *testing.T) {
			box := newCLIBox(t)

			run := box.run(t, "setup", "--config-dir", box.account, bad[0], bad[1])

			if run.code != 1 || !strings.Contains(run.stderr, bad[2]) {
				t.Fatalf("want exit 1 naming %q:\n%s", bad[2], run)
			}
			box.untouched(t, run)
		})
	}
}

func TestDashHShowsTheHelpInsteadOfRunningTheCommand(t *testing.T) {
	for _, argv := range [][]string{{"setup", "-h"}, {"install", "-h"}} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			box := newCLIBox(t)

			run := box.run(t, argv...)

			if run.code != 0 || !strings.Contains(run.stdout, "Usage: noctis") {
				t.Fatalf("-h did not print the help:\n%s", run)
			}
			box.untouched(t, run)
		})
	}
}

func TestSetupAndInstallTakeFlagsHoweverTheyAreWritten(t *testing.T) {
	t.Run("--flag=value", func(t *testing.T) {
		box := newCLIBox(t)

		run := box.run(t, "setup", "--config-dir="+box.account, "--profile=economy", "--permissions=keep")

		box.configured(t, run, box.account, "economy")
		if mode := getString(getMap(readJSON(filepath.Join(box.account, "settings.json")), "permissions"), "defaultMode"); mode != "default" {
			t.Fatalf("--permissions=keep did not keep the permission mode: %q\n%s", mode, run)
		}
	})
	t.Run("--account", func(t *testing.T) {
		box := newCLIBox(t)

		run := box.run(t, "setup", "--account", box.account, "--profile", "economy", "--permissions", "keep")

		box.configured(t, run, box.account, "economy")
	})
	t.Run("--config-dir ~/work", func(t *testing.T) {
		box := newCLIBox(t)

		run := box.run(t, "setup", "--config-dir", "~/work", "--profile", "economy", "--permissions", "keep")

		box.configured(t, run, filepath.Join(box.home, "work"), "economy")
		if statSafe(filepath.Join(box.home, "~")) != nil {
			t.Fatalf("~ was taken as a folder name:\n%s", run)
		}
	})
	t.Run("the profile the setup skill puts first gives way to the one the person asked for", func(t *testing.T) {
		box := newCLIBox(t)

		run := box.run(t, "setup", "--profile", "noctis", "--config-dir", box.account, "--profile=economy", "--permissions", "keep")

		box.configured(t, run, box.account, "economy")
	})
	t.Run("--permissions in any case", func(t *testing.T) {
		for flag, want := range map[string]string{"ACCEPTEDITS": "acceptEdits", "Plan": "plan", "Default": "default"} {
			box := newCLIBox(t)
			cliWrite(t, filepath.Join(box.account, "settings.json"), []byte(`{"permissions": {"defaultMode": "auto"}}`))

			run := box.run(t, "setup", "--config-dir", box.account, "--profile", "economy", "--permissions", flag)

			box.configured(t, run, box.account, "economy")
			if mode := getString(getMap(readJSON(filepath.Join(box.account, "settings.json")), "permissions"), "defaultMode"); mode != want {
				t.Fatalf("--permissions %s set %q, want %q:\n%s", flag, mode, want, run)
			}
			if mode := getString(readJSON(filepath.Join(box.account, pluginName, "config.json")), "managedPermissionMode"); mode != want {
				t.Fatalf("--permissions %s was recorded as %q, want %q:\n%s", flag, mode, want, run)
			}
		}
	})
	t.Run("install --config-dir=", func(t *testing.T) {
		box := newCLIBox(t)

		run := box.run(t, "install", "--source", box.root, "--config-dir="+box.account, "--host", "claude", "--profile", "economy", "--permissions", "keep")

		box.configured(t, run, box.account, "economy")
		if statSafe(filepath.Join(box.account, "skills", pluginName, "hooks", "hooks.json")) == nil {
			t.Fatalf("the plugin was not copied into the account named with --config-dir=:\n%s", run)
		}
	})
}

func TestUninstallUndoesTheAccountItIsGiven(t *testing.T) {
	box := newCLIBox(t)
	installed := func(account string) (string, []byte) {
		marker := filepath.Join(account, "skills", pluginName, "bin", binaryFileName())
		cliWrite(t, marker, []byte("engine"))
		settings := []byte(`{"statusLine": {"type": "command", "command": "\"` + forwardSlashes(marker) + `\" statusline"}}`)
		cliWrite(t, filepath.Join(account, "settings.json"), settings)
		return marker, settings
	}
	defaultMarker, defaultSettings := installed(filepath.Join(box.home, ".claude"))
	otherMarker, _ := installed(box.account)

	run := box.run(t, "install", "--uninstall", "--config-dir="+box.account, "--host", "claude", "--source", box.root)

	if run.code != 0 || statSafe(otherMarker) != nil {
		t.Fatalf("the account named with --config-dir= was not uninstalled:\n%s", run)
	}
	if statSafe(defaultMarker) == nil {
		t.Fatalf("uninstall removed the plugin from the default account, which it was not given:\n%s", run)
	}
	cliUnchanged(t, filepath.Join(box.home, ".claude", "settings.json"), defaultSettings)
}

func TestUpdatesOffLeavesMarketplaceAutoUpdateAlone(t *testing.T) {
	cases := []struct {
		flags []string
		asked bool
	}{
		{[]string{"--updates", "off"}, false},
		{[]string{"--updates=keep"}, false},
		{[]string{"--updates", "KEEP"}, false},
		{[]string{"--updates", "on"}, true},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.flags, " "), func(t *testing.T) {
			box := newCLIBox(t)
			market := filepath.Join(t.TempDir(), "plugins", "cache", "test-mkt", pluginName, pluginVersion)
			if err := copyTree(box.root, market); err != nil {
				t.Fatal(err)
			}
			box.env["NOCTIS_PLUGIN_ROOT"] = market

			run := box.run(t, append([]string{"setup", "--config-dir", box.account, "--profile", "economy", "--permissions", "keep"}, c.flags...)...)

			box.configured(t, run, box.account, "economy")
			if said := strings.Contains(run.stdout, "auto-update"); said != c.asked {
				t.Fatalf("%s: auto-update mentioned=%v, want %v:\n%s", strings.Join(c.flags, " "), said, c.asked, run)
			}
			if calls, _ := os.ReadFile(box.calls); !c.asked && strings.Contains(string(calls), "--auto-update") {
				t.Fatalf("%s still asked claude to switch auto-update on: %s", strings.Join(c.flags, " "), calls)
			}
		})
	}
}

func TestAnUnknownHostStopsTheCommandsPeopleRunButNotTheHooks(t *testing.T) {
	payload := `{"hook_event_name": "SessionEnd", "session_id": "host-check"}`
	cases := []struct {
		name  string
		argv  []string
		env   map[string]string
		input string
		code  int
	}{
		{"status with a mistyped --host", []string{"status", "--host", "codx"}, nil, "", 2},
		{"status under a mistyped NOCTIS_HOST", []string{"status"}, map[string]string{"NOCTIS_HOST": "codx"}, "", 2},
		{"doctor with a mistyped --host", []string{"doctor", "--host", "codx"}, nil, "", 2},
		{"a valid --host wins over a mistyped NOCTIS_HOST", []string{"status", "--host", "claude"}, map[string]string{"NOCTIS_HOST": "codx"}, "", 0},
		{"a hook with a mistyped --host", []string{"hook", "--host", "codx"}, nil, payload, 0},
		{"a hook under a mistyped NOCTIS_HOST", []string{"hook"}, map[string]string{"NOCTIS_HOST": "codx"}, payload, 0},
		{"a hook payload with no command", nil, map[string]string{"NOCTIS_HOST": "codx"}, payload, 0},
		{"the status line with a mistyped --host", []string{"statusline", "--host", "codx"}, nil, "{}", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			box := newCLIBox(t)
			box.env["CLAUDE_CONFIG_DIR"] = box.account
			for key, value := range c.env {
				box.env[key] = value
			}

			run := startNoctisCLIAt(t, box.home, c.input, box.env, c.argv...)()

			if run.code != c.code {
				t.Fatalf("want exit %d:\n%s", c.code, run)
			}
			if c.code == 2 && (!strings.Contains(run.stderr, "codx") || !strings.Contains(run.stderr, "codex")) {
				t.Fatalf("the refusal should name the host it did not know and the ones it does:\n%s", run)
			}
		})
	}
}

func TestParseArgsReadsEveryWayAFlagIsWritten(t *testing.T) {
	parsed := parseArgs([]string{"setup", "--profile=economy", "--config-dir=a", "--config-dir", "b", "--title=x=y", "--json", "--days", "7"})

	for name, want := range map[string]string{"profile": "economy", "title": "x=y", "days": "7"} {
		if got := parsed.flags[name]; got != want {
			t.Errorf("--%s read as %q, want %q", name, got, want)
		}
	}
	if got := parsed.values["config-dir"]; strings.Join(got, "|") != "a|b" {
		t.Errorf("--config-dir kept %q, want both a and b in order", got)
	}
	if _, swallowed := parsed.flags["json"]; !parsed.present["json"] || swallowed {
		t.Errorf("--json should be present and take no value: present %v, flags %v", parsed.present, parsed.flags)
	}
	if strings.Join(parsed.positional, " ") != "setup" {
		t.Errorf("positional words %q, want only setup", parsed.positional)
	}

	parsed = parseArgs([]string{"setup", "--no-ask", "economy", "--config-dir", "--profile", "noctis", "--profile="})

	if strings.Join(parsed.positional, " ") != "setup economy" {
		t.Errorf("a switch swallowed the word after it: positional %q", parsed.positional)
	}
	if got := parsed.values["config-dir"]; len(got) != 1 || got[0] != "" {
		t.Errorf("a --config-dir with no value should be kept as empty, got %q", got)
	}
	if got := parsed.values["profile"]; strings.Join(got, "|") != "noctis|" || parsed.flags["profile"] != "" {
		t.Errorf("--profile= should be read as an empty value after noctis: values %q, flag %q", got, parsed.flags["profile"])
	}
}

func TestAccountTargetsTakeBothFlagsAndExpandTheHome(t *testing.T) {
	sandboxFiles(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	previous := args
	t.Cleanup(func() { args = previous })
	relative, err := filepath.Abs("x")
	if err != nil {
		t.Fatal(err)
	}

	args = parseArgs([]string{"setup", "--account", "x", "--config-dir=~/x", "--config-dir", "x"})

	if got, want := strings.Join(accountTargets("claude"), "|"), filepath.Join(home, "x")+"|"+relative; got != want {
		t.Errorf("accountTargets = %s, want %s", got, want)
	}
	initPaths()
	if files.configDir != relative {
		t.Errorf("--account should pick the account the command reads, got %s", files.configDir)
	}

	args = parseArgs([]string{"setup", "--config-dir=~/work", "--config-dir", "b"})
	initPaths()
	if want := filepath.Join(home, "work"); files.configDir != want {
		t.Errorf("without --account the first --config-dir is the account, got %s, want %s", files.configDir, want)
	}

	args = parseArgs([]string{"setup"})
	if got := accountTargets("claude"); len(got) != 1 || got[0] != filepath.Join(home, ".claude") {
		t.Errorf("with no account named, setup goes to ~/.claude, got %q", got)
	}
	if got := accountTargets("codex"); len(got) != 1 || got[0] != filepath.Join(home, ".codex") {
		t.Errorf("with no account named, a codex install goes to its home, got %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "env"))
	if got := accountTargets("claude"); len(got) != 1 || got[0] != filepath.Join(home, "env") {
		t.Errorf("with no account named, CLAUDE_CONFIG_DIR is the account, got %q", got)
	}
}

func TestTheFlagCheckSaysWhatItDidNotUnderstand(t *testing.T) {
	previous, previousLocale := args, locale
	t.Cleanup(func() { args, locale = previous, previousLocale })
	locale = "en"

	args = parseArgs([]string{"setup", "--profle", "economy", "--permissions", "Plan", "--config-dir=d", "noctis"})
	problems := argProblems(setupFlags)

	want := []string{"unknown option --profle (did you mean --profile?)", "unexpected word noctis (did you mean --profile noctis?)"}
	if strings.Join(problems, "\n") != strings.Join(want, "\n") {
		t.Fatalf("problems:\n%s\nwant:\n%s", strings.Join(problems, "\n"), strings.Join(want, "\n"))
	}

	guesses := map[string]string{"profle": "profile", "Profile": "profile", "permisions": "permissions", "perm": "permissions", "conf": "config-dir",
		"config_dir": "config-dir", "configdir": "config-dir", "noask": "no-ask", "acount": "account", "hsot": "host", "no": "", "zzz": "", "json": ""}
	for name, guess := range guesses {
		if got := closestFlag(name, setupFlags); got != guess {
			t.Errorf("closestFlag(%q) = %q, want %q", name, got, guess)
		}
	}
	if got := closestFlag("uninstal", installFlags); got != "uninstall" {
		t.Errorf("closestFlag(uninstal) = %q, want uninstall", got)
	}

	for _, argv := range [][]string{
		{"setup", "--profile", "noctis", "--profile=economy", "--permissions", "acceptedits", "--updates", "OFF", "--config-dir", "a", "--account=b", "--no-ask", "--code", "opus:max"},
		{"install", "--source", "s", "--uninstall", "--host", "codex", "--config-dir", "c"},
	} {
		args = parseArgs(argv)
		allowed := setupFlags
		if argv[0] == "install" {
			allowed = installFlags
		}
		if problems := argProblems(allowed); len(problems) > 0 {
			t.Errorf("%s was refused: %q", strings.Join(argv, " "), problems)
		}
	}
}
