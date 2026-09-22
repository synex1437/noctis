package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func shippedDefaults(t *testing.T) object {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "config.default.json"))
	if err != nil {
		t.Fatalf("config.default.json unreadable: %v", err)
	}
	var parsed object
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("config.default.json unparseable: %v", err)
	}
	return parsed
}

func TestTheBuiltinThresholdsMatchTheShippedConfig(t *testing.T) {
	shipped := getMap(shippedDefaults(t), "thresholds")
	for key, want := range builtinThresholds {
		got, ok := getNumber(shipped, key)
		if !ok || got != want {
			t.Fatalf("builtinThresholds[%q]=%v but config.default.json says %v", key, want, shipped[key])
		}
	}
}

func TestABadThresholdFallsBackToTheDefaultInsteadOfGoingQuiet(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	for _, bad := range []any{float64(899), float64(-5), float64(101), "ninetytwo", true} {
		mustWriteJSON(files.config, object{"thresholds": object{"session5h": bad}})
		cfg := loadConfig()
		if got := thresholdOf(cfg, "session5h"); got != 92 {
			t.Fatalf("session5h=%v left the threshold at %v, want the shipped 92", bad, got)
		}
		if len(repairedThresholds(cfg)) == 0 {
			t.Fatalf("session5h=%v was repaired without telling anyone", bad)
		}
		if len(unguardedWindows(cfg)) != 0 {
			t.Fatalf("session5h=%v still counts as unguarded after the repair", bad)
		}
		usage := usageView{hasAny: true, fiveHour: &window{used: 96, resetsAt: float64(nowSec() + 3600)}}
		if result := evaluate(cfg, usage, "claude-opus-5", 0, false); result.wait == nil {
			t.Fatalf("session5h=%v: 96%% of the five-hour window did not stop the run", bad)
		}
	}
}

func TestAThresholdSwitchedOffStaysOff(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	for _, off := range []any{nil, float64(0), false} {
		mustWriteJSON(files.config, object{"thresholds": object{"session5h": off}})
		cfg := loadConfig()
		if len(repairedThresholds(cfg)) != 0 {
			t.Fatalf("session5h=%v was overruled: turning a window off must stay off", off)
		}
		usage := usageView{hasAny: true, fiveHour: &window{used: 99, resetsAt: float64(nowSec() + 3600)}}
		if result := evaluate(cfg, usage, "claude-opus-5", 0, false); result.wait != nil {
			t.Fatalf("session5h=%v: a window the user switched off still stopped the run", off)
		}
		if len(unguardedWindows(cfg)) == 0 {
			t.Fatalf("session5h=%v: an unguarded window was not named", off)
		}
	}
}

func TestAGoodThresholdIsLeftAloneAndReportsNothing(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	mustWriteJSON(files.config, object{"thresholds": object{"session5h": float64(70)}})
	cfg := loadConfig()
	if got := thresholdOf(cfg, "session5h"); got != 70 {
		t.Fatalf("threshold %v, want the configured 70", got)
	}
	if names := repairedThresholds(cfg); len(names) != 0 {
		t.Fatalf("a valid threshold was reported as repaired: %v", names)
	}
}

func taskEvent(t *testing.T, event, id, subject string) {
	t.Helper()
	input := object{"session_id": "wf", "hook_event_name": event, "task_id": id}
	if subject != "" {
		input["task_subject"] = subject
	}
	onTaskEvent(input, object{})
}

func storedTaskItems(t *testing.T) object {
	t.Helper()
	return getMap(getMap(getMap(readState(), "tasks"), "wf"), "items")
}

func TestAFinishedTaskLeavesNothingBehind(t *testing.T) {
	sandboxFiles(t)
	for i := 0; i < 40; i++ {
		taskEvent(t, "TaskCreated", itoa(i), "work item that carries a reasonably long subject line")
	}
	if got := len(storedTaskItems(t)); got != 40 {
		t.Fatalf("%d open tasks stored, want 40", got)
	}
	for i := 0; i < 40; i++ {
		taskEvent(t, "TaskCompleted", itoa(i), "")
	}
	if entry := getMap(getMap(readState(), "tasks"), "wf"); entry != nil {
		t.Fatalf("the session still holds %v after every task finished", entry)
	}
	if size := statSafe(files.state).Size(); size > 2048 {
		t.Fatalf("state.json is %d bytes after 40 tasks came and went", size)
	}
}

func TestOpenTasksStopPilingUp(t *testing.T) {
	sandboxFiles(t)
	for i := 0; i < openTaskLimit*3; i++ {
		taskEvent(t, "TaskCreated", itoa(i), "work item")
	}
	if got := len(storedTaskItems(t)); got != openTaskLimit {
		t.Fatalf("%d open tasks stored, want the cap of %d", got, openTaskLimit)
	}
}

func TestAnOldStateFileLosesItsFinishedTasks(t *testing.T) {
	sandboxFiles(t)
	items := object{}
	for i := 0; i < 300; i++ {
		items[itoa(i)] = object{"subject": "done long ago", "status": "completed"}
	}
	updateState(func(state object) {
		stateMap(state, "tasks")["wf"] = object{"items": items, "at": float64(nowSec())}
	})
	updateState(func(object) {})
	if entry := getMap(getMap(readState(), "tasks"), "wf"); entry != nil {
		t.Fatalf("a 5.5.3 state file kept %v", entry)
	}
}

func TestAnEntryWithNoTimestampStillExpires(t *testing.T) {
	sandboxFiles(t)
	state := emptyState()
	stateMap(state, "routes")["ghost"] = object{"route": true}
	now := nowSec()
	pruneState(state, now)
	if getMap(stateMap(state, "routes"), "ghost") == nil {
		t.Fatalf("the first prune dropped an entry it could not date")
	}
	pruneState(state, now+routeTTLSeconds+1)
	if getMap(stateMap(state, "routes"), "ghost") != nil {
		t.Fatalf("the entry never expired")
	}
}

func TestTheStatusLineAcceptsAResetTimeInEitherShape(t *testing.T) {
	now := nowSec()
	epoch := float64(now + 3600)
	for name, value := range map[string]any{
		"epoch":          epoch,
		"rfc3339":        time.Unix(now+3600, 0).UTC().Format(time.RFC3339),
		"rfc3339nano":    time.Unix(now+3600, 0).UTC().Format(time.RFC3339Nano),
		"numeric string": formatNumber(epoch),
	} {
		got, ok := resetEpoch(value)
		if !ok || got != epoch {
			t.Fatalf("%s reset time read as (%v, %v), want (%v, true)", name, got, ok, epoch)
		}
	}
	if _, ok := resetEpoch("not a time"); ok {
		t.Fatalf("nonsense was accepted as a reset time")
	}
}

func TestTheStatusLineStoresAnRfc3339Window(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	recordStatusline(object{
		"session_id": "s1",
		"rate_limits": object{
			"five_hour": object{"used_percentage": float64(61), "resets_at": time.Unix(now+3600, 0).UTC().Format(time.RFC3339)},
			"seven_day": object{"used_percentage": float64(40), "resets_at": time.Unix(now+300000, 0).UTC().Format(time.RFC3339)},
		},
	}, now, false)
	stored := readJSON(files.usage)
	if used := numberOr(getMap(stored, "five_hour"), "used", -1); used != 61 {
		t.Fatalf("five_hour used=%v, want 61 — an RFC3339 reset time was dropped", used)
	}
	if used := numberOr(getMap(stored, "seven_day"), "used", -1); used != 40 {
		t.Fatalf("seven_day used=%v, want 40", used)
	}
}

func TestSessionStartNeverWaitsOnTheUpdateEndpoint(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	stalled := make(chan struct{})
	t.Cleanup(func() { close(stalled) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { <-stalled }))
	t.Cleanup(server.Close)
	t.Setenv("NOCTIS_UPDATE_URL", server.URL)
	t.Setenv("NOCTIS_NO_WATCHER", "1")
	mustWriteJSON(files.config, object{"update": object{"check": true}})

	done := make(chan string, 1)
	go func() { done <- checkForUpdate(loadConfig(), readState(), nowSec()) }()
	select {
	case notice := <-done:
		if notice != "" {
			t.Fatalf("an empty cache produced a notice: %q", notice)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("session start blocked on the update endpoint")
	}
}

func TestACachedReleaseStillRaisesTheNotice(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	t.Setenv("NOCTIS_UPDATE_URL", "https://example.invalid/plugin.json")
	now := nowSec()
	mustWriteJSON(files.release, object{"checkedAt": float64(now), "latest": "99.0.0"})
	if notice := checkForUpdate(loadConfig(), readState(), now); notice == "" {
		t.Fatalf("a cached newer version raised no notice")
	}
}

func TestReadingAFileTwiceGivesTwoPrivateCopies(t *testing.T) {
	dir := sandboxFiles(t)
	path := filepath.Join(dir, "shared.json")
	mustWriteJSON(path, object{"nested": object{"value": float64(1)}})
	first := readJSON(path)
	getMap(first, "nested")["value"] = float64(99)
	second := readJSON(path)
	if got := numberOr(getMap(second, "nested"), "value", -1); got != 1 {
		t.Fatalf("a second read saw %v, so the two callers share one tree", got)
	}
}

func TestAChangedFileIsNeverServedFromTheCache(t *testing.T) {
	dir := sandboxFiles(t)
	path := filepath.Join(dir, "moving.json")
	mustWriteJSON(path, object{"value": float64(1)})
	if got := numberOr(readJSON(path), "value", -1); got != 1 {
		t.Fatalf("first read gave %v", got)
	}
	mustWriteJSON(path, object{"value": float64(2)})
	if got := numberOr(readJSON(path), "value", -1); got != 2 {
		t.Fatalf("second read gave %v after the file changed", got)
	}
}

func TestWordSetsSplitOnEveryKindOfSpace(t *testing.T) {
	set := wordSet("  add   build\n\ncreate\twrite  lütfen пожалуйста  ")
	for _, word := range []string{"add", "build", "create", "write", "lütfen", "пожалуйста"} {
		if !set[word] {
			t.Fatalf("%q missing from the word set", word)
		}
	}
	if len(set) != 6 {
		t.Fatalf("word set has %d entries, want 6: %v", len(set), set)
	}
}

func TestOnlyTheLocaleInUseIsBuilt(t *testing.T) {
	catalogCache = map[string]map[string]string{}
	t.Cleanup(func() { catalogCache = map[string]map[string]string{} })
	if catalogFor("de")["win.five"] == "" {
		t.Fatalf("German catalog did not build")
	}
	if len(catalogCache) != 1 {
		t.Fatalf("asking for one locale built %d: %v", len(catalogCache), sortedKeys(toObjectKeys(catalogCache)))
	}
	if !knownLocale("ja") {
		t.Fatalf("knownLocale says Japanese is unknown")
	}
	if len(catalogCache) != 1 {
		t.Fatalf("knownLocale built a catalog it only had to name")
	}
	if knownLocale("xx") {
		t.Fatalf("knownLocale accepted a language that does not exist")
	}
}

func toObjectKeys(table map[string]map[string]string) object {
	out := object{}
	for key := range table {
		out[key] = true
	}
	return out
}
