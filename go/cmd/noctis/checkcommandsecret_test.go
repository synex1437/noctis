package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestABundleRedactsTheQueueCheckCommandAndPasswordsInAnyURL(t *testing.T) {
	home := t.TempDir()
	account := filepath.Join(home, ".claude", pluginName)
	command := `DATABASE_URL="postgres://app:x2Hunter2pass@db.internal/app" go test ./... && echo "x2Tail done"`
	cliWrite(t, filepath.Join(account, "config.json"), marshalPretty(object{
		"queue":  object{"verifyCommand": command},
		"resume": object{"terminal": "x2term --hold sh {script}"},
	}))
	at := time.Now().Add(-48 * time.Hour)
	logged := errorsLogEntry(at, fmt.Sprintf("queue check %q failed 2 time(s) in a row for s1 (exited with status 1); TASKS.md held until it passes, stop allowed", command)) +
		errorsLogEntry(at, "cache refused redis://:x2RedisPass@cache:6379/0 and amqp://guest:x2AmqpPass@mq/ too")
	cliWrite(t, filepath.Join(account, "guard.log"), []byte(logged))
	cliWrite(t, filepath.Join(account, "errors.log"), []byte(logged))
	cliWrite(t, filepath.Join(account, "decisions.jsonl"), append(marshalCompact(object{"at": float64(nowSec()), "sid": "s1", "event": "Stop", "action": "hold-queue", "reason": "exited with status 1", "command": command}), '\n'))
	target := filepath.Join(home, "bundle.zip")
	run := startNoctisCLIAt(t, home, "", nil, "report", "--bundle", target)()
	if run.code != 0 {
		t.Fatalf("noctis report --bundle failed:\n%s", run)
	}
	entries := bundleEntries(t, target)
	for name, content := range entries {
		for _, secret := range []string{"x2Hunter2pass", "x2Tail", "x2RedisPass", "x2AmqpPass", "x2term"} {
			if strings.Contains(content, secret) {
				t.Errorf("%s in the bundle carries %s", name, secret)
			}
		}
	}
	for _, want := range []string{`"verifyCommand": "<redacted>"`, `"terminal": "<redacted>"`} {
		if !strings.Contains(entries["config.json"], want) {
			t.Errorf("config.json in the bundle must hold %s:\n%s", want, entries["config.json"])
		}
	}
	if !strings.Contains(entries["decisions.jsonl"], `"action":"hold-queue"`) {
		t.Errorf("decisions.jsonl in the bundle lost the entry itself:\n%s", entries["decisions.jsonl"])
	}
}

func TestAHeldQueueKeepsTheCheckCommandOutOfTheNotification(t *testing.T) {
	command := shellFor("DATABASE_URL=postgres://app:x2Hunter2pass@db.internal/app sh -c 'exit 3'", "set DATABASE_URL=postgres://app:x2Hunter2pass@db.internal/app& exit 3")
	cfg, project, frontend := queueCheckSandbox(t, command)
	section(cfg, "queue")["escalate"] = "off"
	tickFirstQueueItem(t, project)
	stopHookOutput(t, stopInput("x2held", frontend), cfg)
	held := stopHookOutput(t, stopAgain("x2held", frontend), cfg)
	if !strings.Contains(getString(held, "systemMessage"), command) {
		t.Fatalf("the session must still be told which command holds the queue: %v", held)
	}
	notices := 0
	for _, line := range tailFileLines(files.log, 1000) {
		if !strings.Contains(line, "notify: "+pluginName+" — ") {
			continue
		}
		notices++
		if strings.Contains(line, "x2Hunter2pass") || strings.Contains(line, "DATABASE_URL") {
			t.Errorf("the notification, which a webhook sends on as it is, carries the check command: %s", line)
		}
	}
	if want := "notify: " + pluginName + " — " + T("queue.heldNotify", 2, "TASKS.md", "queue.verifyCommand"); notices != 1 || loggedTimes(want) != 1 {
		t.Errorf("the hold must go through the notify path once as %q (%d notification(s))", want, notices)
	}
}

func TestABundleRedactsEachQueueCheckCommandAsItIsLogged(t *testing.T) {
	home := t.TempDir()
	cases := []struct {
		name     string
		queue    object
		logged   string
		password string
	}{
		{"the per-item check", object{"verifyEachCommand": "curl -fsS -u ci:x8EachPw77 https://ci.internal/ping && go test ./internal/queue/..."}, "curl -fsS -u ci:x8EachPw77 https://ci.internal/ping && go test ./internal/queue/...", "x8EachPw77"},
		{"a check padded with spaces", object{"verifyCommand": " mysql -uci -px8PadPw77 -e 'select 1' && make check "}, "mysql -uci -px8PadPw77 -e 'select 1' && make check", "x8PadPw77"},
		{"a check under the home folder", object{"verifyCommand": filepath.Join(home, "ci", "check.sh") + " -u ci:x8HomePw77"}, filepath.Join(home, "ci", "check.sh") + " -u ci:x8HomePw77", "x8HomePw77"},
		{"a per-item check that starts with the full check", object{"verifyCommand": "make check", "verifyEachCommand": "make check && curl -fsS -u ci:x8LongPw77 https://ci.internal/ping"}, "make check && curl -fsS -u ci:x8LongPw77 https://ci.internal/ping", "x8LongPw77"},
	}
	for index, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			account := filepath.Join(home, ".claude", pluginName)
			cliWrite(t, filepath.Join(account, "config.json"), marshalPretty(object{"queue": c.queue}))
			logged := errorsLogEntry(time.Now().Add(-time.Hour), fmt.Sprintf("queue check %q failed 2 time(s) in a row for s1 (exited with status 1); TASKS.md held until it passes, stop allowed", c.logged))
			cliWrite(t, filepath.Join(account, "guard.log"), []byte(logged))
			cliWrite(t, filepath.Join(account, "errors.log"), []byte(logged))
			cliWrite(t, filepath.Join(account, "decisions.jsonl"), append(marshalCompact(object{"at": float64(nowSec()), "sid": "s1", "event": "Stop", "action": "hold-queue", "reason": "exited with status 1", "command": c.logged}), '\n'))
			target := filepath.Join(home, fmt.Sprintf("bundle-%d.zip", index))
			if run := startNoctisCLIAt(t, home, "", nil, "report", "--bundle", target)(); run.code != 0 {
				t.Fatalf("noctis report --bundle failed:\n%s", run)
			}
			for name, content := range bundleEntries(t, target) {
				if at := strings.Index(content, c.password); at >= 0 {
					start := strings.LastIndexByte(content[:at], '\n') + 1
					t.Errorf("%s in the bundle carries the password of the check command: %.200s", name, content[start:])
				}
			}
		})
	}
}
