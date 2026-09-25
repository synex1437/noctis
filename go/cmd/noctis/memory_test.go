package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func heapAfterGC() uint64 {
	for i := 0; i < 3; i++ {
		runtime.GC()
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

func growth(t *testing.T, name string, warmup, rounds int, work func()) {
	t.Helper()
	for i := 0; i < warmup; i++ {
		work()
	}
	before := heapAfterGC()
	beforeRoutines := runtime.NumGoroutine()
	beforeFDs, countable := openDescriptors()
	for i := 0; i < rounds; i++ {
		work()
	}
	after := heapAfterGC()
	afterRoutines := runtime.NumGoroutine()
	perRound := (float64(after) - float64(before)) / float64(rounds)
	t.Logf("%s: heap %d -> %d B over %d rounds (%.1f B/round), goroutines %d -> %d",
		name, before, after, rounds, perRound, beforeRoutines, afterRoutines)
	kept := float64(after) - float64(before)
	if perRound > 256 && kept > 64*1024 {
		t.Errorf("%s keeps %.1f B per round (%.0f kB over %d rounds) after a full GC; a wait runs this for hours", name, perRound, kept/1024, rounds)
	}
	if afterRoutines > beforeRoutines {
		t.Errorf("%s leaked %d goroutine(s)", name, afterRoutines-beforeRoutines)
	}
	if !countable {
		return
	}
	if afterFDs, _ := openDescriptors(); afterFDs > beforeFDs {
		t.Errorf("%s left %d descriptor(s) open over %d rounds (%d -> %d); a wait runs this for hours", name, afterFDs-beforeFDs, rounds, beforeFDs, afterFDs)
	}
}

func waitingSandbox(t *testing.T) object {
	t.Helper()
	sandboxFiles(t)
	now := float64(nowSec())
	cfg := object{
		"thresholds": object{"session5h": float64(92), "weeklyAll": float64(89), "weeklyFable": float64(95)},
		"credits":    object{},
		"budget":     object{},
		"wait":       object{"earlyResetPollMinutes": float64(0)},
		"usage":      object{},
		"models":     object{"primary": "fable", "fallback": "opus", "scopedPattern": "fable"},
		"router":     object{"enabled": true},
	}
	updateState(func(state object) {
		stateMap(state, "waits")["mem"] = object{
			"startedAt": now, "resumeAt": now + 7200, "until": now + 7200,
			"window": "five_hour", "label": "5h", "used": float64(93),
			"cwd": files.guardDir, "kind": "threshold",
		}
	})
	mustWriteJSON(files.usage, object{
		"updatedAt": now,
		"five_hour": object{"used": float64(40), "resetsAt": now + 3600},
		"seven_day": object{"used": float64(30), "resetsAt": now + 86400},
	})
	return cfg
}

func TestTheWaitLoopDoesNotGrowWhileItWaits(t *testing.T) {
	cfg := waitingSandbox(t)
	watch := newWaitWatch(cfg, "mem", true)
	watch.pollEvery = 0
	growth(t, "waitWatch.tick", 50, 3000, func() { watch.tick() })
}

func TestReadingStateDoesNotGrow(t *testing.T) {
	waitingSandbox(t)
	growth(t, "readState", 50, 3000, func() { _ = readState() })
}

func TestTheDecisionPathDoesNotGrow(t *testing.T) {
	cfg := waitingSandbox(t)
	input := object{"session_id": "mem", "cwd": files.guardDir}
	growth(t, "decide", 20, 600, func() {
		state := readState()
		_ = decide(cfg, state, input, nowSec(), decideOptions{noProbe: true})
	})
}

func TestClassifyingPromptsDoesNotGrow(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	prompts := []string{
		"fix the auth.js token refresh loop and keep the tests green",
		"what are the latest trends in european ai regulation",
		"write the copy for our landing page hero section",
		"en iyi vektör veritabanı hangisi karşılaştırma yapar mısın",
	}
	round := 0
	growth(t, "classifyPrompt", 100, 4000, func() {
		_ = classifyPrompt(cfg, nil, prompts[round%len(prompts)], "", nowSec())
		round++
	})
}

func TestPollingTheUsageEndpointDoesNotRetain(t *testing.T) {
	sandboxFiles(t)
	body := `{"limits":[{"kind":"session","utilization":40,"resets_at":"2030-01-01T00:00:00Z"},{"kind":"weekly_all","utilization":30,"resets_at":"2030-01-02T00:00:00Z"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
	growth(t, "fetchOauthUsage", 20, 400, func() {
		result := fetchOauthUsage("token")
		if result.status != 200 {
			t.Fatalf("the probe server answered %d (%s)", result.status, result.err)
		}
	})
}

func TestARefreshCycleDoesNotRetain(t *testing.T) {
	cfg := waitingSandbox(t)
	body := `{"limits":[{"kind":"session","utilization":40,"resets_at":"2030-01-01T00:00:00Z"},{"kind":"weekly_all","utilization":30,"resets_at":"2030-01-02T00:00:00Z"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
	mustWriteJSON(files.credentials, object{"claudeAiOauth": object{"accessToken": "t", "expiresAt": float64(nowSec()+86400) * 1000}})
	growth(t, "refreshFable", 10, 200, func() { refreshFable(cfg, nowSec(), "probe", 0, true) })
}
