package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// refreshLab runs real `noctis hook` processes (this test binary as noctis), so the refresher a
// hook starts is a real detached process. The usage endpoint holds every request until the test
// opens its gate.
type refreshLab struct {
	t       *testing.T
	env     []string
	project string
	hits    atomic.Int64
	gate    chan struct{}
	open    sync.Once
}

func newRefreshLab(t *testing.T, held bool) *refreshLab {
	t.Helper()
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
	lab := &refreshLab{t: t, project: project, gate: make(chan struct{})}
	if !held {
		lab.release()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		lab.hits.Add(1)
		select {
		case <-lab.gate:
		case <-request.Context().Done():
			return
		}
		now := time.Now().UTC()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(marshalCompact(object{
			"five_hour": object{"utilization": 37, "resets_at": now.Add(time.Hour).Format(time.RFC3339)},
			"seven_day": object{"utilization": 41, "resets_at": now.Add(72 * time.Hour).Format(time.RFC3339)},
		}))
	}))
	t.Cleanup(server.Close)
	// A refresher still waiting on the endpoint when the test ends would write into a folder that
	// is being removed.
	t.Cleanup(func() {
		lab.release()
		lab.settle(10 * time.Second)
	})
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "NOCTIS_") || strings.HasPrefix(key, "CLAUDE") || key == "HOME" || key == "USERPROFILE" {
			continue
		}
		lab.env = append(lab.env, entry)
	}
	lab.env = append(lab.env, "HOME="+root, "USERPROFILE="+root, "CLAUDE_CONFIG_DIR="+account, "CLAUDE_CODE_OAUTH_TOKEN=lab-token",
		"NOCTIS_PLUGIN_ROOT="+pluginRoot, "NOCTIS_USAGE_URL="+server.URL+"/api/oauth/usage", "NOCTIS_NO_TASKS=1", "NOCTIS_NO_SCHEDULE=1",
		"NOCTIS_NO_WATCHER=1", "NOCTIS_UPDATE_URL=off", "NOCTIS_LANG=en", testAsNoctis+"=1")
	return lab
}

func (lab *refreshLab) release() { lab.open.Do(func() { close(lab.gate) }) }

// prompt runs one UserPromptSubmit hook and returns how long the host waited on it.
func (lab *refreshLab) prompt(sid string) time.Duration {
	lab.t.Helper()
	executable, err := os.Executable()
	if err != nil {
		lab.t.Fatal(err)
	}
	command := exec.Command(executable, "hook")
	command.Env, command.Dir = lab.env, lab.project
	command.Stdin = bytes.NewReader(marshalCompact(object{"hook_event_name": "UserPromptSubmit", "session_id": sid, "cwd": lab.project, "prompt": "go on with the refactor"}))
	started := time.Now()
	if output, err := command.CombinedOutput(); err != nil {
		lab.t.Fatalf("the hook failed: %v\n%s", err, output)
	}
	return time.Since(started)
}

// settle waits for the refresher in flight, if any, to let fable.lock go.
func (lab *refreshLab) settle(limit time.Duration) bool {
	for until := time.Now().Add(limit); time.Now().Before(until); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(files.fableLock); os.IsNotExist(err) {
			return true
		}
	}
	return false
}

// reading leaves a reading of used % in both windows, fetched at fetchedAt.
func (lab *refreshLab) reading(fetchedAt int64, used float64) {
	reset := float64(nowSec() + 3600)
	mustWriteJSON(files.fable, object{"fetchedAt": float64(fetchedAt), "five_hour": object{"used": used, "resetsAt": reset}, "seven_day": object{"used": used, "resetsAt": reset + 86400}})
}

// handedOffBound is well under refreshWait, so a hook that waited for the fetch fails it, and far
// above what starting the hook and its refresher costs on a slow CI runner.
const handedOffBound = refreshWait - 300*time.Millisecond

func TestAHookWhoseReadingIsDueHandsTheFetchOffAndDoesNotWaitForTheEndpoint(t *testing.T) {
	lab := newRefreshLab(t, true)
	fetchedAt := nowSec() - 11*60
	lab.reading(fetchedAt, 30)

	took := lab.prompt("due")

	if took > handedOffBound {
		t.Errorf("the reading is 11 minutes old and far from every edge, yet the hook took %s: it waited for the endpoint", took)
	}
	if _, err := os.Stat(files.fableLock); err != nil {
		t.Errorf("right after the hook, no refresher holds fable.lock (%v): the fetch was dropped, or its lock is free while it runs", err)
	}
	lab.release()
	if !lab.settle(8 * time.Second) {
		t.Fatal("the refresher never let fable.lock go")
	}
	fable := readJSON(files.fable)
	if got := numberOr(fable, "fetchedAt", 0); got <= float64(fetchedAt) {
		t.Errorf("fable.json still holds the reading fetched at %d after the refresher finished (fetchedAt %v)", fetchedAt, got)
	}
	if used := numberOr(getMap(fable, "five_hour"), "used", 0); used != 37 {
		t.Errorf("fable.json holds a 5-hour reading of %v %%, want the endpoint's 37", used)
	}
	if got := lab.hits.Load(); got != 1 {
		t.Errorf("the endpoint was asked %d time(s), want once", got)
	}
}

func TestAHookWithNoReadingWaitsForTheFetchOnlyUpToItsBound(t *testing.T) {
	lab := newRefreshLab(t, true)

	took := lab.prompt("blind")

	if took < refreshWait-100*time.Millisecond || took > refreshWait+time.Second {
		t.Errorf("with no reading at all and an endpoint that does not answer, the hook took %s, want about %s", took, refreshWait)
	}
	lab.release()
	if !lab.settle(8 * time.Second) {
		t.Fatal("the refresher never let fable.lock go")
	}
	if numberOr(readJSON(files.fable), "fetchedAt", 0) == 0 {
		t.Error("the answer the hook stopped waiting for never reached fable.json")
	}
}

func TestAHookWithNoReadingDecidesOnTheAnswerWhenTheEndpointIsQuick(t *testing.T) {
	lab := newRefreshLab(t, false)

	lab.prompt("quick")

	if _, err := os.Stat(files.fableLock); !os.IsNotExist(err) {
		t.Errorf("the hook returned while its refresher still held fable.lock (%v): it did not wait for a quick answer it needed", err)
	}
	if used := numberOr(getMap(readJSON(files.fable), "five_hour"), "used", 0); used != 37 {
		t.Errorf("when the hook returned, fable.json held a 5-hour reading of %v %%, want the endpoint's 37", used)
	}
}

func TestAHookNearAnEdgeDecidesOnTheAnswerItWaitedFor(t *testing.T) {
	lab := newRefreshLab(t, true)
	// 86 % is inside the band below the default 92 % pause point, where a reading older than two
	// minutes is due.
	lab.reading(nowSec()-150, 86)
	answerAfter := 400 * time.Millisecond
	go func() {
		time.Sleep(answerAfter)
		lab.release()
	}()

	took := lab.prompt("edge")

	if took < answerAfter {
		t.Errorf("near the edge the hook took %s and did not wait for the answer, which came after %s", took, answerAfter)
	}
	if _, err := os.Stat(files.fableLock); !os.IsNotExist(err) {
		t.Errorf("near the edge the hook returned while its refresher still held fable.lock (%v)", err)
	}
	if used := numberOr(getMap(readJSON(files.fable), "five_hour"), "used", 0); used != 37 {
		t.Errorf("when the hook returned, fable.json held a 5-hour reading of %v %%, want the endpoint's 37", used)
	}
}

func TestHooksThatFindAFetchInFlightStartNoSecondOne(t *testing.T) {
	lab := newRefreshLab(t, true)
	lab.reading(nowSec()-11*60, 30)

	for _, sid := range []string{"one", "two", "three"} {
		if took := lab.prompt(sid); took > handedOffBound {
			t.Errorf("hook %s took %s while a fetch was in flight", sid, took)
		}
	}

	lab.release()
	if !lab.settle(8 * time.Second) {
		t.Fatal("the refresher never let fable.lock go")
	}
	if got := lab.hits.Load(); got != 1 {
		t.Errorf("three hooks with one fetch in flight asked the endpoint %d time(s), want once", got)
	}
}

func TestARefresherFetchesOnlyWithTheLockHandedToIt(t *testing.T) {
	sandboxFiles(t)
	if _, adopted := adoptFileLock(files.fableLock); adopted {
		t.Error("with no fable.lock at all, a refresher adopted one")
	}
	if err := os.WriteFile(files.fableLock, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, adopted := adoptFileLock(files.fableLock); adopted {
		t.Error("a refresher adopted fable.lock held in another process's name")
	}
	if waited := time.Since(started); waited < refresherAdoptWait {
		t.Errorf("a refresher gave up after %s on a lock that was not handed to it yet, want %s", waited, refresherAdoptWait)
	}
	if err := os.Remove(files.fableLock); err != nil {
		t.Fatal(err)
	}

	handle, acquired := openFileLock(files.fableLock)
	if !acquired {
		t.Fatal("fable.lock could not be taken")
	}
	handLockTo(handle, os.Getpid())
	release, adopted := adoptFileLock(files.fableLock)
	if !adopted {
		t.Fatal("a refresher did not adopt fable.lock handed to it")
	}
	if _, again := openFileLock(files.fableLock); again {
		t.Error("fable.lock was free while the refresher it was handed to held it")
	}
	release()
	if _, err := os.Stat(files.fableLock); !os.IsNotExist(err) {
		t.Errorf("the refresher left fable.lock behind (%v)", err)
	}
}

func TestAWaitForAFetchInFlightEndsAtOnceWhenNoFetchStandsBehindTheLock(t *testing.T) {
	sandboxFiles(t)
	for _, owner := range []string{"not a pid", strconv.Itoa(deadPid(t))} {
		if err := os.WriteFile(files.fableLock, []byte(owner), 0o644); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		if awaitLockRelease(files.fableLock, time.Second) {
			t.Errorf("fable.lock holding %q still stands, yet the wait for it reported it let go", owner)
		}
		if waited := time.Since(started); waited > 200*time.Millisecond {
			t.Errorf("a hook waited %s on fable.lock holding %q, which no fetch stands behind", waited, owner)
		}
	}
	if err := os.WriteFile(files.fableLock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(files.fableLock, stale, stale); err != nil {
		t.Fatal(err)
	}
	if started := time.Now(); awaitLockRelease(files.fableLock, time.Second) || time.Since(started) > 200*time.Millisecond {
		t.Error("a hook waited on an empty fable.lock two seconds old, which is no hand-over in progress")
	}

	if err := os.WriteFile(files.fableLock, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.Remove(files.fableLock)
	}()
	started := time.Now()
	if !awaitLockRelease(files.fableLock, 2*time.Second) {
		t.Error("the wait for a fetch in flight did not see its holder let fable.lock go")
	}
	if waited := time.Since(started); waited < 100*time.Millisecond {
		t.Errorf("the wait for a fetch in flight ended after %s, before its holder let fable.lock go", waited)
	}
}
