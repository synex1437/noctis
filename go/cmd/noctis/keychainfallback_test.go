package main

import (
	"testing"
	"time"
)

// u10CredentialsFile leaves a credentials file holding oauth under claudeAiOauth, or no
// claudeAiOauth at all when oauth is nil.
func u10CredentialsFile(t *testing.T, oauth object) {
	t.Helper()
	content := object{"mcpOAuth": object{}}
	if oauth != nil {
		content["claudeAiOauth"] = oauth
	}
	cliWrite(t, files.credentials, marshalCompact(content))
}

func TestACredentialsFileWithoutAUsableTokenLeavesTheTokenToTheKeychain(t *testing.T) {
	keychainSandbox(t)
	previous := isDarwin
	t.Cleanup(func() { isDarwin = previous })
	isDarwin = true
	store := fakeKeychain(t)
	stockKeychain(t, store, map[string]string{keychainServiceFor(""): "KEYCHAIN-token"})
	expired := object{"accessToken": "FILE-token", "expiresAt": float64(time.Now().Add(-time.Hour).UnixMilli())}

	u10CredentialsFile(t, expired)
	if token, state := oauthTokenState(); token != "KEYCHAIN-token" || state != "present" {
		t.Fatalf("a leftover credentials file with an expired token hid the Keychain's live one: got %q, %q", token, state)
	}
	u10CredentialsFile(t, nil)
	if token, state := oauthTokenState(); token != "KEYCHAIN-token" || state != "present" {
		t.Fatalf("a credentials file with no sign-in in it hid the Keychain's live one: got %q, %q", token, state)
	}
	u10CredentialsFile(t, object{"accessToken": "FILE-token", "expiresAt": float64(time.Now().Add(time.Hour).UnixMilli())})
	if token, state := oauthTokenState(); token != "FILE-token" || state != "present" {
		t.Fatalf("a credentials file with a live token must still come first: got %q, %q", token, state)
	}

	stockKeychain(t, store, nil)
	u10CredentialsFile(t, expired)
	if token, state := oauthTokenState(); token != "" || state != "expired" {
		t.Fatalf("with an expired token in the file and none in the Keychain the sign-in is expired, got %q, %q", token, state)
	}

	isDarwin = false
	stockKeychain(t, store, map[string]string{keychainServiceFor(""): "KEYCHAIN-token"})
	if token, state := oauthTokenState(); token != "" || state != "expired" {
		t.Fatalf("away from the Keychain's platform an expired token in the file must stay expired, got %q, %q", token, state)
	}
}
