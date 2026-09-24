package main

import (
	"strings"
	"testing"
)

func doctorTokenLine(t *testing.T, cfg object) (string, []string) {
	t.Helper()
	previousHost, previousLocale := activeHost, locale
	t.Cleanup(func() { activeHost, locale = previousHost, previousLocale })
	activeHost, locale = "claude", "en"
	doctorIssues = 0
	lines := doctorLines(cfg)
	for index, line := range lines {
		if strings.Contains(line, "OAuth token") {
			return line, lines[index+1:]
		}
	}
	t.Fatalf("the doctor has no OAuth token line:\n%s", strings.Join(lines, "\n"))
	return "", nil
}

func TestTheSignInCheckRunsOnlyWhenTheScopedDataComesFromIt(t *testing.T) {
	keychainSandbox(t)
	off := object{"fable": object{"source": "off"}}

	line, _ := doctorTokenLine(t, off)

	if !strings.HasPrefix(line, "OK") || !strings.Contains(line, "fable.source") {
		t.Fatalf("with fable.source off a missing sign-in still fails the doctor: %q", line)
	}
	if status := describeState(off, object{}, usageView{}, nowSec()); strings.Contains(status, "not fetched yet") || !strings.Contains(status, "fable.source") {
		t.Fatalf("status waits for scoped data that is never fetched:\n%s", status)
	}
	if line, _ := doctorTokenLine(t, object{"fable": object{"source": "oauth"}}); !strings.HasPrefix(line, "!!") {
		t.Fatalf("with fable.source oauth a missing sign-in is no longer reported: %q", line)
	}
}

func TestAnExpiredSignInIsCalledExpired(t *testing.T) {
	keychainSandbox(t)
	cliWrite(t, files.credentials, []byte(`{"claudeAiOauth": {"accessToken": "old", "expiresAt": 1000}}`))

	line, rest := doctorTokenLine(t, object{"fable": object{"source": "oauth"}})

	if !strings.HasPrefix(line, "!!") || !strings.Contains(line, "expired") || strings.Contains(line, "missing") {
		t.Fatalf("an expired sign-in is not called expired: %q", line)
	}
	if len(rest) == 0 || !strings.Contains(rest[0], "fix:") {
		t.Fatalf("the expired sign-in has no remedy: %q", rest)
	}
}

func TestOnMacOSTheSignInCheckNamesTheKeychainNotAFileThatIsNotThere(t *testing.T) {
	keychainSandbox(t)
	previous := isDarwin
	t.Cleanup(func() { isDarwin = previous })
	isDarwin = true
	fakeKeychain(t)

	line, _ := doctorTokenLine(t, object{"fable": object{"source": "oauth"}})

	if strings.Contains(line, ".credentials.json") || !strings.Contains(line, "Keychain") {
		t.Fatalf("on macOS the missing sign-in names a file Claude Code never writes there: %q", line)
	}
}
