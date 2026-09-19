package main

import (
	"os"
	"testing"
)

func TestCheckpointUsableMatchesWhatTheEngineWouldRestore(t *testing.T) {
	now := nowSec()
	const here, elsewhere = "/p", "/q"

	cases := []struct {
		name  string
		entry object
		want  bool
	}{
		{"fresh and unspent", object{"cwd": here, "at": float64(now - 60)}, true},
		{"already spent by a resume", object{"cwd": here, "at": float64(now - 60), "consumed": true}, false},
		{"older than the engine accepts", object{"cwd": here, "at": float64(now) - checkpointTTLSeconds - 1}, false},
		{"another project's", object{"cwd": elsewhere, "at": float64(now - 60)}, false},
		{"exactly at the horizon", object{"cwd": here, "at": float64(now) - checkpointTTLSeconds}, true},
		{"missing", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkpointUsable(tc.entry, here, now); got != tc.want {
				t.Errorf("checkpointUsable = %v, want %v", got, tc.want)
			}

			state := object{"checkpoints": object{"S": tc.entry}}
			_, best := latestCheckpointFor(state, here, now)
			if (best != nil) != tc.want {
				t.Errorf("latestCheckpointFor disagrees with checkpointUsable (%v vs %v)", best != nil, tc.want)
			}
		})
	}
}

func TestConsumedCheckpointIsWithheldNotPrinted(t *testing.T) {
	now := nowSec()
	state := object{"checkpoints": object{
		"spent": object{"cwd": "/p", "at": float64(now - 60), "consumed": true},
	}}
	if _, best := latestCheckpointFor(state, "/p", now); best != nil {
		t.Fatal("a spent checkpoint was offered as restorable")
	}
	if n := countWithheldCheckpoints(state, "/p", "", now); n != 1 {
		t.Errorf("withheld count = %d, want 1 — NONE alone reads as 'no checkpoint was ever taken'", n)
	}
}

func TestSidSkipsTheProjectCheckButNotTheRetirementChecks(t *testing.T) {
	now := nowSec()
	elsewhere := object{"cwd": "/q", "at": float64(now - 60)}
	if !checkpointUsable(elsewhere, "", now) {
		t.Error("--sid should reach a session's own checkpoint wherever it ran")
	}
	if checkpointUsable(elsewhere, "/p", now) {
		t.Error("without a sid the project must still scope the search")
	}
	spent := object{"cwd": "/q", "at": float64(now - 60), "consumed": true}
	if checkpointUsable(spent, "", now) {
		t.Error("--sid must not resurrect a checkpoint a resume already spent")
	}
	old := object{"cwd": "/q", "at": float64(now) - checkpointTTLSeconds - 1}
	if checkpointUsable(old, "", now) {
		t.Error("--sid must not print a checkpoint the engine considers expired")
	}
}

func TestWaitLiveIsOneRuleForEverySurface(t *testing.T) {
	now := nowSec()
	long := float64(now) - waitStaleSeconds - 3600

	cases := []struct {
		name string
		wait object
		want bool
	}{
		{"resume time still ahead", object{"resumeAt": float64(now + 600)}, true},
		{"resume time long past, no runner", object{"resumeAt": long}, false},
		{"long past but its sleeper is alive", object{"resumeAt": long,
			"scheduled": object{"method": "sleeper", "pid": float64(os.Getpid())}}, true},
		{"long past, sleeper is gone", object{"resumeAt": long,
			"scheduled": object{"method": "sleeper", "pid": float64(deadPidForTest(t))}}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := waitLive(tc.wait, now); got != tc.want {
				t.Errorf("waitLive = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPruneDropsExactlyTheWaitsWaitLiveCallsDead(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	state := object{"waits": object{
		"live": object{"resumeAt": float64(now + 600)},
		"dead": object{"resumeAt": float64(now) - waitStaleSeconds - 3600},
	}}
	pruneState(state, now)
	waits := getMap(state, "waits")
	if getMap(waits, "live") == nil {
		t.Error("a live wait was pruned")
	}
	if getMap(waits, "dead") != nil {
		t.Error("an abandoned wait survived pruning, so status would still promise it a resume")
	}
}

func deadPidForTest(t *testing.T) int {
	t.Helper()
	for candidate := 4194300; candidate > 4194000; candidate-- {
		if !processAlive(candidate) {
			return candidate
		}
	}
	t.Skip("no unused pid available to test a dead runner with")
	return 0
}
