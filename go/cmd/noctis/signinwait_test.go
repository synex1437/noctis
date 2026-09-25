package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func expiredSignInWait(t *testing.T) (object, object) {
	t.Helper()
	dir := sandboxFiles(t)
	previousHost, previousLocale := activeHost, locale
	t.Cleanup(func() { activeHost, locale = previousHost, previousLocale })
	activeHost, locale = "claude", "en"
	files.credentials = filepath.Join(dir, ".credentials.json")
	mustWriteJSON(files.credentials, object{"claudeAiOauth": object{"accessToken": "old", "expiresAt": float64(nowSec()-3600) * 1000}})
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("NOCTIS_USAGE_URL", "http://127.0.0.1:9/api/oauth/usage")
	if !isWindows {
		bin := t.TempDir()
		for _, notifier := range []string{"notify-send", "osascript"} {
			writeScript(t, filepath.Join(bin, notifier), "#!/bin/sh\nexit 0\n")
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	cfg := releaseConfig()
	cfg["fable"] = object{"source": "oauth"}
	now := float64(nowSec())
	record := object{
		"kind": "batch", "window": "seven_day", "label": "weekly", "used": float64(95), "threshold": float64(89), "hit": "threshold",
		"startedAt": now - 600, "until": now + 6*86400, "resumeAt": now + 6*86400 + 60,
	}
	updateState(func(state object) { stateMap(state, "waits")["w1"] = record })
	return cfg, record
}

func pollWithoutBackoff(t *testing.T, cfg, record object, polls int) {
	t.Helper()
	for poll := 1; poll <= polls; poll++ {
		if got := earlyRelease(cfg, "w1", record, true, true); got != "" {
			t.Fatalf("poll %d ended the wait (%s) without any new usage reading", poll, got)
		}
		fable := readJSON(files.fable)
		fable["backoffUntil"] = float64(0)
		mustWriteJSON(files.fable, fable)
	}
}

func TestAWaitTellsTheUserOnceThatAnExpiredSignInHidesEarlyResets(t *testing.T) {
	cfg, record := expiredSignInWait(t)
	notice := "notify: " + pluginName + " — " + T("wait.signInExpired", "weekly", formatTime(numberOr(record, "resumeAt", 0)))

	pollWithoutBackoff(t, cfg, record, 3)

	if getString(readJSON(files.fable), "error") != "no-token" {
		t.Fatalf("the polls did not find the sign-in unusable: %v", readJSON(files.fable))
	}
	if told := strings.Count(string(readFileOrEmpty(files.log)), notice); told != 1 {
		t.Fatalf("the Claude sign-in expired during a weekly wait, so no early reset can be read any more, and the user was told %d times, want once:\n%s", told, readFileOrEmpty(files.log))
	}

	previousOffset := timeOffset
	t.Cleanup(func() { timeOffset = previousOffset })
	timeOffset += 4 * 86400
	pollWithoutBackoff(t, cfg, record, 2)
	if told := strings.Count(string(readFileOrEmpty(files.log)), notice); told != 1 {
		t.Fatalf("four days on, with still no good reading, the user was told again: %d times in all", told)
	}

	mustWriteJSON(files.fable, object{"fetchedAt": float64(nowSec() - 400), "seven_day": object{"used": float64(95), "resetsAt": numberOr(record, "until", 0)}})
	pollWithoutBackoff(t, cfg, record, 2)

	if told := strings.Count(string(readFileOrEmpty(files.log)), notice); told != 2 {
		t.Fatalf("a sign-in that expired again after a good reading was told %d times in all, want twice:\n%s", told, readFileOrEmpty(files.log))
	}
	if content := string(readFileOrEmpty(files.errors)); strings.Contains(content, "sign-in") {
		t.Fatalf("a condition the user was told about is logged as an error, so the doctor fails on it for a day:\n%s", content)
	}
}

func TestAMissingSignInIsNotReportedAsExpiredDuringAWait(t *testing.T) {
	cfg, record := expiredSignInWait(t)
	mustWriteJSON(files.credentials, object{})

	pollWithoutBackoff(t, cfg, record, 2)

	if logged := string(readFileOrEmpty(files.log)); strings.Contains(logged, "notify: ") {
		t.Fatalf("a machine that never had a Claude sign-in got a notice about an expired one:\n%s", logged)
	}
}

func TestAWaitWhoseSignInExpiredStillEndsOnAnEarlyResetAStatusLineShows(t *testing.T) {
	cfg, record := expiredSignInWait(t)
	now := nowSec()
	pollWithoutBackoff(t, cfg, record, 1)

	statusReading(now, 4, float64(now+5*3600), 3, float64(now+7*86400))

	if got := earlyRelease(cfg, "w1", record, true, true); got != "reset" {
		t.Fatalf("with the sign-in expired, a status line showing the weekly window reset did not end the wait: %q", got)
	}
}
