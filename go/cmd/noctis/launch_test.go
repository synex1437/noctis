package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func relaunchSandbox(t *testing.T) string {
	t.Helper()
	dir := sandboxFiles(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(claudeConfigEnv, "")
	if err := os.Unsetenv(claudeConfigEnv); err != nil {
		t.Fatal(err)
	}
	files.configDir = filepath.Join(home, ".claude")
	files.resumeLog = filepath.Join(dir, "resume-output.log")
	return home
}

func launcherConfigLines(t *testing.T, launch launchSpec) []string {
	t.Helper()
	script, _ := unixLaunchScript(launch, "/opt/claude/bin/claude", []string{"--resume", launch.sid}, "max")
	if script == "" {
		t.Fatal("the launcher script was not written")
	}
	content, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	found := []string{}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, claudeConfigEnv) {
			found = append(found, line)
		}
	}
	return found
}

func configEntries(env []string) []string {
	found := []string{}
	for _, entry := range env {
		if key, _, _ := strings.Cut(entry, "="); strings.EqualFold(key, claudeConfigEnv) {
			found = append(found, entry)
		}
	}
	return found
}

func TestDefaultInstallRelaunchScriptUnsetsTheConfigDir(t *testing.T) {
	home := relaunchSandbox(t)
	got := launcherConfigLines(t, launchSpec{sid: "s1", cwd: home})
	if strings.Join(got, "|") != "unset CLAUDE_CONFIG_DIR" {
		t.Fatalf("a session that never set CLAUDE_CONFIG_DIR must be relaunched without it (Claude would read %s/.claude.json and another keychain item), got %q", files.configDir, got)
	}
}

func TestRelaunchCarriesTheConfigDirOnlyWhenTheSessionHadIt(t *testing.T) {
	home := relaunchSandbox(t)
	account := filepath.Join(t.TempDir(), "it's an account") + string(os.PathSeparator)
	cases := []struct {
		name    string
		session string
		env     []string
		script  []string
	}{
		{"default install", "", nil, []string{"unset CLAUDE_CONFIG_DIR"}},
		{"explicit account", account, []string{"CLAUDE_CONFIG_DIR=" + account}, []string{"export CLAUDE_CONFIG_DIR=" + shellQuote(account)}},
	}
	for _, tc := range cases {
		if tc.session == "" {
			os.Unsetenv(claudeConfigEnv)
		} else {
			os.Setenv(claudeConfigEnv, tc.session)
		}
		updateState(func(state object) {
			stateMap(state, "waits")["s1"] = object{"scheduled": object{"method": "manual"}}
		})
		if !registerWait("s1", object{"kind": "batch", "cwd": home, "scheduled": object{"method": "manual"}}, nil) {
			t.Fatalf("%s: the wait was not stored", tc.name)
		}
		wait := getMap(getMap(readState(), "waits"), "s1")
		if recorded, present := wait["configDirEnv"].(string); !present || recorded != tc.session {
			t.Fatalf("%s: the wait recorded CLAUDE_CONFIG_DIR as %#v, want %q", tc.name, wait["configDirEnv"], tc.session)
		}
		os.Setenv(claudeConfigEnv, filepath.Join(home, "runner-leftover"))
		launch := launchSpec{sid: "s1", cwd: home, configDir: relaunchConfigDir(wait)}
		env := relaunchEnv(launch, "max")
		if got := configEntries(env); strings.Join(got, "|") != strings.Join(tc.env, "|") {
			t.Errorf("%s: the relaunch environment carries %q, want %q", tc.name, got, tc.env)
		}
		joined := "|" + strings.Join(env, "|") + "|"
		if !strings.Contains(joined, "|CLAUDE_CODE_EFFORT_LEVEL=max|") || !strings.Contains(joined, "|"+handoffEnv+"=s1|") {
			t.Errorf("%s: the relaunch environment lost its effort or hand-off marker", tc.name)
		}
		if got := launcherConfigLines(t, launch); strings.Join(got, "|") != strings.Join(tc.script, "|") {
			t.Errorf("%s: the window launcher says %q, want %q", tc.name, got, tc.script)
		}
	}
}

func TestRelaunchConfigDirFollowsWhatTheSessionHad(t *testing.T) {
	home := relaunchSandbox(t)
	defaultDir := filepath.Join(home, ".claude")
	account := filepath.Join(t.TempDir(), "acct")
	cases := []struct {
		name    string
		wait    object
		account string
		want    string
	}{
		{"default install, wait written before the field existed", object{}, defaultDir, ""},
		{"default install", object{"configDirEnv": ""}, defaultDir, ""},
		{"default install, account passed with a trailing separator", object{"configDirEnv": ""}, defaultDir + string(os.PathSeparator), ""},
		{"explicit account", object{"configDirEnv": account}, account, account},
		{"explicit account keeps its exact spelling", object{"configDirEnv": account + string(os.PathSeparator)}, account, account + string(os.PathSeparator)},
		{"explicitly set to the default directory", object{"configDirEnv": defaultDir}, defaultDir, defaultDir},
		{"explicit account, wait written before the field existed", object{}, account, account},
		{"unset in a session whose home the runner does not share", object{"configDirEnv": ""}, account, account},
		{"a malformed record falls back to the account", object{"configDirEnv": 42.0}, account, account},
	}
	for _, tc := range cases {
		files.configDir = tc.account
		if got := relaunchConfigDir(tc.wait); got != tc.want {
			t.Errorf("%s: relaunch CLAUDE_CONFIG_DIR = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRelaunchEnvDropsTheConfigDirInAnyCaseOnWindows(t *testing.T) {
	previous := isWindows
	t.Cleanup(func() { isWindows = previous })
	isWindows = true
	got := withoutEnv([]string{`Claude_Config_Dir=C:\stray`, `=C:=C:\work`, `PATH=C:\bin`, "CLAUDE_CONFIG_DIRECTORY=keep"}, claudeConfigEnv)
	if want := []string{`=C:=C:\work`, `PATH=C:\bin`, "CLAUDE_CONFIG_DIRECTORY=keep"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("withoutEnv = %q, want %q", got, want)
	}
	isWindows = false
	if got := withoutEnv([]string{"Claude_Config_Dir=/kept", "CLAUDE_CONFIG_DIR=/dropped"}, claudeConfigEnv); strings.Join(got, "|") != "Claude_Config_Dir=/kept" {
		t.Fatalf("on unix only the exact name is the variable Claude reads, got %q", got)
	}
}

func TestOnlyAWindowsRelaunchGetsThePromptAsOneScrubbedLine(t *testing.T) {
	previous := isWindows
	t.Cleanup(func() { isWindows = previous })
	typed := "keep \"100%\" of the rows\n^ then run the tests"
	isWindows = true
	if got, want := relaunchPrompt(typed), "keep '100'' of the rows ' then run the tests"; got != want {
		t.Fatalf("a Windows relaunch prompt can pass cmd.exe, so quotes, %% and ^ are replaced and the lines joined: got %q, want %q", got, want)
	}
	isWindows = false
	if got := relaunchPrompt(typed); got != typed {
		t.Fatalf("elsewhere the prompt reaches the tool as an argument no shell reads, so it stays as typed: got %q", got)
	}
	if got := relaunchPrompt("- add tests"); got != "Continue. - add tests" {
		t.Fatalf("a prompt that starts with a dash must not be read as an option: got %q", got)
	}
}
