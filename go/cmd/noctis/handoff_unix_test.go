//go:build !windows

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestAClaudePrintModeStopFailureOutlivesTheTeardownThatEndsItsHook(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pluginRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil || !looksLikePluginRoot(pluginRoot) {
		t.Fatalf("the plugin root is not three levels above the package (%s): %v", pluginRoot, err)
	}
	root := t.TempDir()
	account := filepath.Join(root, "claude home")
	project := filepath.Join(root, "project")
	ensureDir(project)
	previous, previousArgs := files, args
	t.Cleanup(func() { files, args = previous, previousArgs })
	args = parseArgs(nil)
	files = pathsFor(account, pluginRoot)
	mustWriteJSON(files.config, object{"alarm": object{"enabled": false}, "update": object{"check": false}})
	sid := "sf-teardown"
	recordWorkflowLaunch(sid, object{"tool_input": object{"name": "audit-routes"}}, nowSec())
	held := make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-held
		now := time.Now().UTC()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(marshalCompact(object{
			"five_hour": object{"utilization": 20, "resets_at": now.Add(time.Hour).Format(time.RFC3339)},
			"seven_day": object{"utilization": 40, "resets_at": now.Add(72 * time.Hour).Format(time.RFC3339)},
		}))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(release)
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "NOCTIS_") || strings.HasPrefix(key, "CLAUDE") || key == "HOME" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "HOME="+root, "CLAUDE_CONFIG_DIR="+account, "CLAUDE_CODE_ENTRYPOINT=sdk-cli", "CLAUDE_CODE_OAUTH_TOKEN=lab-token",
		"NOCTIS_PLUGIN_ROOT="+pluginRoot, "NOCTIS_USAGE_URL="+server.URL+"/api/oauth/usage", "NOCTIS_NO_TASKS=1", "NOCTIS_NO_SCHEDULE=1",
		"NOCTIS_NO_WATCHER=1", "NOCTIS_UPDATE_URL=off", "NOCTIS_LANG=en", testAsNoctis+"=1")
	hook := func(payload object) *exec.Cmd {
		command := exec.Command(executable, "hook")
		command.Env, command.Dir = env, project
		command.Stdin = bytes.NewReader(marshalCompact(payload))
		isolateTree(command)
		return command
	}
	transcript := filepath.Join(project, sid+".jsonl")
	failure := hook(object{"session_id": sid, "transcript_path": transcript, "cwd": project, "hook_event_name": "StopFailure", "error": "rate_limit",
		"last_assistant_message": "API Error: Request rejected (429) · Number of request tokens has exceeded your per-minute rate limit"})
	if err := failure.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = failure.Wait()
		close(exited)
	}()
	pending := filepath.Join(files.guardDir, "pending")
	handedOff := func() bool {
		entries, _ := os.ReadDir(pending)
		return len(entries) > 0
	}
	teardown := time.After(3 * time.Second)
	for watching := true; watching; {
		select {
		case <-exited:
			watching = false
		case <-teardown:
			watching = false
		case <-time.After(2 * time.Millisecond):
			watching = !handedOff()
		}
	}
	_ = failure.Process.Signal(syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
	}
	_ = syscall.Kill(-failure.Process.Pid, syscall.SIGKILL)
	<-exited
	if output, err := hook(object{"session_id": sid, "transcript_path": transcript, "cwd": project, "hook_event_name": "SessionEnd", "reason": "other"}).CombinedOutput(); err != nil {
		t.Fatalf("the SessionEnd hook claude -p runs at teardown failed: %v\n%s", err, output)
	}
	release()
	var wait object
	for until := time.Now().Add(20 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		if wait = pendingWait(sid); wait != nil && getMap(wait, "scheduled") != nil {
			break
		}
	}
	if wait == nil || getMap(wait, "scheduled") == nil {
		logged, _ := os.ReadFile(files.log)
		t.Fatalf("a 429 in a claude -p run whose StopFailure hook got the teardown's SIGTERM left no retry with a runner: wait %v\n%s", wait, logged)
	}
	logged, _ := os.ReadFile(files.log)
	if match := regexp.MustCompile(`\[INFO (\d+) hook\] StopFailure for ` + sid + ` taken over`).FindSubmatch(logged); match != nil {
		copyPid, _ := strconv.Atoi(string(match[1]))
		until := time.Now().Add(20 * time.Second)
		for processAlive(copyPid) && time.Now().Before(until) {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if entries, _ := os.ReadDir(pending); len(entries) != 0 {
		t.Fatalf("the hand-off was left in pending/ after the retry was stored: %d file(s)", len(entries))
	}
	if launches := workflowLaunches(readState(), sid); len(launches) != 1 {
		t.Fatalf("the SessionEnd that ran while the retry was being stored dropped the workflow the relaunch must resume: %v", launches)
	}
}
