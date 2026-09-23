package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func claudeKeychainItem(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
}

func keychainSandbox(t *testing.T) string {
	t.Helper()
	sandboxFiles(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, name := range []string{claudeConfigEnv, claudeSecureStorageEnv, "CLAUDE_CODE_OAUTH_TOKEN"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs(nil)
	files.configDir = filepath.Join(home, ".claude")
	files.credentials = filepath.Join(files.configDir, ".credentials.json")
	return home
}

func fakeKeychain(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	store := filepath.Join(dir, "items")
	if err := os.Mkdir(store, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOCTIS_TEST_KEYCHAIN", store)
	name, script := "security", "#!/bin/sh\n[ \"$1\" = find-generic-password ] && [ \"$2\" = -s ] && [ -f \"$NOCTIS_TEST_KEYCHAIN/$3.json\" ] || exit 44\ncat \"$NOCTIS_TEST_KEYCHAIN/$3.json\"\n"
	if isWindows {
		name, script = "security.cmd", "@echo off\r\nif not \"%~1\"==\"find-generic-password\" exit /b 1\r\nif not \"%~2\"==\"-s\" exit /b 1\r\nif not exist \"%NOCTIS_TEST_KEYCHAIN%\\%~3.json\" exit /b 44\r\ntype \"%NOCTIS_TEST_KEYCHAIN%\\%~3.json\"\r\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return store
}

func stockKeychain(t *testing.T, store string, items map[string]string) {
	t.Helper()
	entries, err := os.ReadDir(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(store, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	expires := float64(time.Now().Add(time.Hour).UnixMilli())
	for service, token := range items {
		item := marshalCompact(object{"claudeAiOauth": object{"accessToken": token, "expiresAt": expires}})
		if err := os.WriteFile(filepath.Join(store, service+".json"), item, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestKeychainServiceNameIsTheItemClaudeCodeKeepsForTheAccount(t *testing.T) {
	home := keychainSandbox(t)
	defaultDir := filepath.Join(home, ".claude")
	work := "/Users/x/.claude-work"
	workDir, _ := filepath.Abs(work)
	shared := "/Users/x/.claude-shared"
	cases := []struct {
		name    string
		env     map[string]string
		account string
		wait    object
		want    string
	}{
		{"the default account", nil, defaultDir, nil, "Claude Code-credentials"},
		{"CLAUDE_CONFIG_DIR names the account", map[string]string{claudeConfigEnv: work}, workDir, nil, "Claude Code-credentials-74ed04d5"},
		{"the spelling Claude Code was given is what it hashes", map[string]string{claudeConfigEnv: work + "/"}, workDir, nil, "Claude Code-credentials-e7620868"},
		{"CLAUDE_CONFIG_DIR set to the default directory", map[string]string{claudeConfigEnv: defaultDir}, defaultDir, nil, claudeKeychainItem(defaultDir)},
		{"an empty CLAUDE_SECURESTORAGE_CONFIG_DIR drops the suffix", map[string]string{claudeConfigEnv: work, claudeSecureStorageEnv: ""}, workDir, nil, "Claude Code-credentials"},
		{"CLAUDE_SECURESTORAGE_CONFIG_DIR is hashed instead of the account", map[string]string{claudeConfigEnv: work, claudeSecureStorageEnv: shared}, workDir, nil, claudeKeychainItem(shared)},
		{"the default account honours CLAUDE_SECURESTORAGE_CONFIG_DIR", map[string]string{claudeSecureStorageEnv: shared}, defaultDir, nil, claudeKeychainItem(shared)},
		{"--account of the default directory from another account's shell", map[string]string{claudeConfigEnv: work, claudeSecureStorageEnv: shared}, defaultDir, nil, "Claude Code-credentials"},
		{"--account of another directory from a shell without CLAUDE_CONFIG_DIR", map[string]string{claudeSecureStorageEnv: ""}, workDir, nil, claudeKeychainItem(workDir)},
		{"a runner uses the spelling its session had", nil, workDir, object{"configDirEnv": work + "/"}, "Claude Code-credentials-e7620868"},
		{"a runner for a session that named the default directory", nil, defaultDir, object{"configDirEnv": defaultDir}, claudeKeychainItem(defaultDir)},
		{"a runner for a default session", nil, defaultDir, object{"configDirEnv": ""}, "Claude Code-credentials"},
		{"a runner whose wait predates the recorded spelling", nil, workDir, object{}, claudeKeychainItem(workDir)},
	}
	for _, tc := range cases {
		for _, name := range []string{claudeConfigEnv, claudeSecureStorageEnv} {
			if value, set := tc.env[name]; set {
				os.Setenv(name, value)
			} else {
				os.Unsetenv(name)
			}
		}
		files.configDir = tc.account
		args = parseArgs(nil)
		if tc.wait != nil {
			args = parseArgs([]string{"resume", "--sid", "s1", "--account", tc.account})
			updateState(func(state object) {
				stateMap(state, "waits")["s1"] = tc.wait
			})
		}
		if got := keychainServiceName(); got != tc.want {
			t.Errorf("%s: keychain item %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMacOSReadsTheOAuthTokenFromTheAccountsOwnKeychainItem(t *testing.T) {
	home := keychainSandbox(t)
	previous := isDarwin
	t.Cleanup(func() { isDarwin = previous })
	isDarwin = true
	store := fakeKeychain(t)
	personal := filepath.Join(home, ".claude")
	work, _ := filepath.Abs(filepath.Join(home, ".claude-work"))
	personalItem, workItem := "Claude Code-credentials", claudeKeychainItem(work)
	token := func() string {
		return getString(getMap(keychainCredentials(), "claudeAiOauth"), "accessToken")
	}
	os.Setenv(claudeConfigEnv, work)
	files.configDir = work
	files.credentials = filepath.Join(work, ".credentials.json")

	stockKeychain(t, store, map[string]string{personalItem: "PERSONAL-token", workItem: "WORK-token"})
	if got := token(); got != "WORK-token" {
		t.Fatalf("with CLAUDE_CONFIG_DIR=%s the keychain gave %q, want the token Claude Code keeps under %q", work, got, workItem)
	}
	if got := oauthToken(); got != "WORK-token" {
		t.Fatalf("oauthToken() = %q for the work account, want WORK-token", got)
	}
	stockKeychain(t, store, map[string]string{personalItem: "PERSONAL-token"})
	if got := token(); got != "" {
		t.Fatalf("the work account is logged out, yet the keychain gave %q: it must never fall back to the default account's item", got)
	}
	stockKeychain(t, store, map[string]string{workItem: "WORK-token"})
	if got := oauthToken(); got != "WORK-token" {
		t.Fatalf("with the default account logged out the work account read %q, want WORK-token", got)
	}

	os.Unsetenv(claudeConfigEnv)
	files.configDir = personal
	files.credentials = filepath.Join(personal, ".credentials.json")
	stockKeychain(t, store, map[string]string{personalItem: "PERSONAL-token", workItem: "WORK-token"})
	if got := oauthToken(); got != "PERSONAL-token" {
		t.Fatalf("the default account read %q, want PERSONAL-token", got)
	}
}
