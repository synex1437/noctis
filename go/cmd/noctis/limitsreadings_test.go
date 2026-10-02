package main

import (
	"os"
	"testing"
	"time"
)

func TestAProjectionIsNotDrawnFromReadingsSecondsApart(t *testing.T) {
	cases := []struct {
		name           string
		first, second  float64
		apart, silence int64
	}{
		{"a fresh window: 1% then 2% two seconds apart, then a 3-minute tool call", 1, 2, 2, 200},
		{"mid-window: 70% then 71% five seconds apart, then a 2-minute tool call", 70, 71, 5, 125},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sandboxFiles(t)
			t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
			now := nowSec()
			reset, weekReset := float64(now+4*3600), float64(now+3*86400)
			statusReadingFrom("a", now-c.silence-c.apart, c.first, reset, 20, weekReset)
			statusReadingFrom("a", now-c.silence, c.second, reset, 20, weekReset)

			result := decide(releaseConfig(), readState(), object{"session_id": "a", "cwd": t.TempDir()}, now, decideOptions{})

			if result.wait != nil {
				t.Fatalf("at %v%% of the 5-hour window (pause point 92%%) the session pauses on a projection from two readings %d s apart: hit %q, projected %.0f%%",
					c.second, c.apart, result.wait.hit, currentUsage(now).fiveHour.projected)
			}
		})
	}
}

func TestATypedPromptIsNotHeldByAProjectionFromReadingsSecondsApart(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"wait": object{"maxInHookMinutes": float64(0)}}, 20, 20)
	if err := os.Remove(files.usage); err != nil {
		t.Fatal(err)
	}
	now := nowSec()
	fiveReset, weekReset := float64(now+4*3600), float64(now+3*86400)
	statusReadingFrom("tp", now-202, 1, fiveReset, 20, weekReset)
	statusReadingFrom("tp", now-200, 2, fiveReset, 20, weekReset)
	prompt := promptInput("tp", project, "start with the parser refactor")

	if answer := hookOutput(t, onUserPromptSubmit, prompt, cfg); getString(answer, "decision") == "block" {
		t.Fatalf("at 2%% of the 5-hour window the prompt you typed was held: %v", answer["reason"])
	}
	updateState(func(state object) { state["disabledUntil"] = float64(now + 3600); delete(state, "waits") })
	if answer := hookOutput(t, onUserPromptSubmit, prompt, cfg); getString(answer, "decision") == "block" {
		t.Fatalf("with noctis off, at 2%% of the 5-hour window, the prompt you typed was held: %v", answer["reason"])
	}
}

func TestAProjectionThatWouldPauseAsksTheUsageEndpointFirst(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	reset, weekReset := float64(now+4*3600), float64(now+3*86400)
	served := limitsServer(t, limitsBody(74, reset, 20, weekReset))
	cfg := releaseConfig()
	cfg["fable"] = object{"source": "oauth"}
	statusReadingFrom("a", now-720, 70, reset, 20, weekReset)
	statusReadingFrom("a", now-600, 74, reset, 20, weekReset)
	if usage := currentUsage(now); windowHit(usage.fiveHour, float64(92)) != "projection" {
		t.Fatalf("setup: four points in two minutes, then ten minutes without a reading, should project past the 92%% pause point: projected %.0f%%", usage.fiveHour.projected)
	}

	result := decide(cfg, readState(), object{"session_id": "a", "cwd": t.TempDir()}, now, decideOptions{})

	if served.Load() != 1 || result.wait != nil {
		t.Fatalf("the usage endpoint, asked %d time(s), says 74%% now; the session should go on, but it pauses: %+v", served.Load(), result.wait)
	}
}

func TestAProjectionStillPausesWhenTheUsageEndpointCannotBeAsked(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	now := nowSec()
	reset, weekReset := float64(now+4*3600), float64(now+3*86400)
	cfg := releaseConfig()
	cfg["fable"] = object{"source": "oauth"}
	statusReadingFrom("a", now-720, 70, reset, 20, weekReset)
	statusReadingFrom("a", now-600, 74, reset, 20, weekReset)

	result := decide(cfg, readState(), object{"session_id": "a", "cwd": t.TempDir()}, now, decideOptions{})

	if result.wait == nil || result.wait.hit != "projection" {
		t.Fatalf("with no fresh reading to be had, a burn that projects past the pause point should still pause on the projection: %+v", result.wait)
	}
}

func TestAClockSetBackDoesNotStopTheUsageFetches(t *testing.T) {
	sandboxFiles(t)
	previous := timeOffset
	t.Cleanup(func() { timeOffset = previous })
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	served := limitsServer(t, limitsBody(50, reset, 20, weekReset), limitsBody(95, reset, 20, weekReset))
	cfg := releaseConfig()
	cfg["fable"] = object{"source": "oauth"}
	timeOffset = previous + 7200
	refreshFableNow(cfg, nowSec(), "session-start", -1, false)
	if served.Load() != 1 {
		t.Fatal("setup: the read on the fast clock did not reach the usage endpoint")
	}
	timeOffset = previous

	result := decide(cfg, readState(), object{"session_id": "headless", "cwd": t.TempDir()}, nowSec(), decideOptions{force: true, noProbe: true})
	refreshFableNow(cfg, nowSec(), "blind-probe", 0, true)

	if asked := served.Load() - 1; asked != 2 || result.wait == nil {
		t.Fatalf("after the clock was set back two hours, a forced read and a blind probe asked the usage endpoint %d time(s), want 2; at 95%% (pause point 92%%) the forced check decided %+v", asked, result.wait)
	}
}

func TestABackoffSetOnAFastClockEndsWithTheLongestBackoff(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	cfg := object{"fable": object{"source": "oauth"}}
	mustWriteJSON(files.fable, object{"error": "http-500", "backoffUntil": float64(now + 2*3600 + 120)})
	if _, due := fableRefreshDue(cfg, now, -1, false); !due {
		t.Fatal("a backoff that ends two hours past the longest one noctis sets, left by a clock that ran fast, still holds every fetch back")
	}
	mustWriteJSON(files.fable, object{"error": "http-429", "backoffUntil": float64(now + 3600)})
	if _, due := fableRefreshDue(cfg, now, -1, false); due {
		t.Fatal("an hour's backoff after a 429 no longer holds the fetches back")
	}
}

func antigravityRender(sid string, at int64, used, reset, countdown float64) {
	stamp := time.Unix(int64(reset), 0).UTC().Format(time.RFC3339)
	input := normalizeStatuslineInput("antigravity", object{"conversation_id": sid, "model": object{"id": "U4model 3 (High)"}, "quota": object{
		"u4model-5h": object{"remaining_fraction": 1 - used/100, "reset_in_seconds": countdown, "reset_time": stamp},
	}}, at)
	recordStatusline(input, at, true)
}

func TestAnAntigravityCountdownASecondOffKeepsTheReadingsOfOneWindowTogether(t *testing.T) {
	for _, skew := range []float64{0, 1} {
		t.Run("burst, countdown "+formatNumber(skew)+" s off", func(t *testing.T) {
			sandboxFiles(t)
			now := nowSec()
			reset := float64(now + 3*3600)
			antigravityRender("ag", now-600, 54, reset, reset-float64(now-600))
			antigravityRender("ag", now-120, 75, reset, reset-float64(now-120)-skew)

			usage := currentUsage(now)

			if plan := evaluate(releaseConfig(), usage, "", 0, false).wait; plan == nil || plan.hit != "burst" {
				t.Fatalf("a 21-point jump to 75%% of the 5-hour window does not pause on the burst: burst %v, wait %+v", usage.fiveHour.burst, plan)
			}
		})
		t.Run("multi-session maximum, countdown "+formatNumber(skew)+" s off", func(t *testing.T) {
			sandboxFiles(t)
			now := nowSec()
			reset := float64(now + 3*3600)
			antigravityRender("busy", now-30, 93, reset, reset-float64(now-30))
			antigravityRender("idle", now, 85, reset, reset-float64(now)-skew)

			usage := currentUsage(now)

			if plan := evaluate(releaseConfig(), usage, "", 0, false).wait; plan == nil {
				t.Fatalf("one conversation read 93%% (pause point 92%%) 30 s ago, an idle one repainted 85%% for the same window, and the guard reads %v%%: no pause", usage.fiveHour.used)
			}
		})
	}
}

func TestAWindowThatEndsSecondsAfterAWindowThatHasResetIsReadAsItsOwn(t *testing.T) {
	renders := map[string]func(sid string, at int64, used, reset float64){
		"Claude Code": func(sid string, at int64, used, reset float64) {
			statusReadingFrom(sid, at, used, reset, 23, float64(at+3*86400))
		},
		"Antigravity": func(sid string, at int64, used, reset float64) {
			antigravityRender(sid, at, used, reset, reset-float64(at))
		},
	}
	for host, render := range renders {
		t.Run(host, func(t *testing.T) {
			sandboxFiles(t)
			t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
			now := nowSec()
			render("first", now-10, 100, float64(now-5))
			render("next", now, 100, float64(now+3))

			usage := currentUsage(now)

			if usage.fiveHour == nil || usage.fiveHour.resetsAt != float64(now+3) {
				t.Fatalf("the 5-hour window read at 100%% resets at %s; the one before it reset at %s, and the guard reads %+v", formatTime(float64(now+3)), formatTime(float64(now-5)), usage.fiveHour)
			}
			if plan := evaluate(releaseConfig(), usage, "", 0, false).wait; plan == nil {
				t.Fatal("at 100% of a 5-hour window that resets in 3 s the session does not wait for the reset")
			}
		})
	}
}

func TestAClaudeCodeReadingWithAnotherResetThanTheStoredOneIsAnotherWindow(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	now := nowSec()
	reset, weekReset := float64(now+5000), float64(now+3*86400)
	statusReadingFrom("busy", now-60, 70, reset, 20, weekReset)
	statusReadingFrom("other", now, 30, reset+100, 12, weekReset)

	usage := currentUsage(now)

	if usage.fiveHour == nil || usage.fiveHour.used != 30 || usage.fiveHour.resetsAt != reset+100 {
		t.Fatalf("a session's status line reads 30%% of a 5-hour window resetting at %s, 100 s after the window another session read 70%% of; the guard reads %+v", formatTime(reset+100), usage.fiveHour)
	}
}

func TestAnIdleSessionDoesNotBringBackTheWindowBeforeAnEarlyReset(t *testing.T) {
	now := nowSec()
	fiveReset := float64(now + 2*3600)
	oldWeek, newWeek := float64(now+3*86400), float64(now+7*86400-900)
	beforeAndAfterTheReset := func() {
		statusReadingFrom("busy", now-1500, 30, fiveReset, 92, oldWeek)
		statusReadingFrom("idle", now-1490, 30, fiveReset, 92, oldWeek)
		statusReadingFrom("busy", now-120, 31, fiveReset, 1, newWeek)
	}
	describe := func(plan *waitPlan) string {
		return "a " + plan.hit + " pause of the " + plan.label + " window at " + formatNumber(plan.used) + "% until " + formatTime(plan.until)
	}
	t.Run("status line", func(t *testing.T) {
		sandboxFiles(t)
		beforeAndAfterTheReset()
		statusReadingFrom("idle", now-30, 30, fiveReset, 92, oldWeek)
		usage := currentUsage(now)
		if plan := evaluate(releaseConfig(), usage, "", 0, false).wait; plan != nil {
			t.Errorf("the weekly window was reset early and stands at 1%% (new week ends %s); after an idle session repainted, the guard reads %v%% ending %s and decides %s",
				formatTime(newWeek), usage.sevenDay.used, formatTime(usage.sevenDay.resetsAt), describe(plan))
		}
	})
	for _, endpoint := range []struct {
		name      string
		fetchedAt int64
		backoff   bool
	}{
		{"the usage endpoint read the new week 10 s ago", now - 10, false},
		{"the usage endpoint read the new week 6 min ago and is rate-limited since", now - 360, true},
	} {
		t.Run("hook, "+endpoint.name, func(t *testing.T) {
			sandboxFiles(t)
			served := limitsServer(t, limitsBody(31, fiveReset, 1, newWeek))
			cfg := releaseConfig()
			cfg["fable"] = object{"source": "oauth"}
			beforeAndAfterTheReset()
			reading := object{"fetchedAt": float64(endpoint.fetchedAt),
				"five_hour": object{"used": float64(31), "resetsAt": fiveReset}, "seven_day": object{"used": float64(1), "resetsAt": newWeek}}
			if endpoint.backoff {
				reading["error"], reading["backoffUntil"] = "http-429", float64(now+600)
			}
			mustWriteJSON(files.fable, reading)
			statusReadingFrom("idle", now-3, 30, fiveReset, 92, oldWeek)

			result := decide(cfg, readState(), object{"session_id": "busy", "cwd": t.TempDir()}, now, decideOptions{})

			if result.wait != nil {
				t.Errorf("both the busy session's status line and the usage endpoint read the new week at 1%%; after an idle session repainted the old week's 92%%, the hook decides %s (endpoint asked %d time(s))",
					describe(result.wait), served.Load())
			}
		})
	}
	t.Run("a reading a session has not shown before is still taken", func(t *testing.T) {
		sandboxFiles(t)
		beforeAndAfterTheReset()
		statusReadingFrom("idle", now-60, 32, fiveReset, 2, newWeek)
		if week := currentUsage(now).sevenDay; week == nil || week.used != 2 || week.resetsAt != newWeek {
			t.Fatalf("the idle session's first answer of the new week (2%%) was not stored: %+v", week)
		}
		otherAccount := float64(now + 2*86400)
		statusReadingFrom("busy", now-30, 5, fiveReset, 40, otherAccount)
		if week := currentUsage(now).sevenDay; week == nil || week.used != 40 || week.resetsAt != otherAccount {
			t.Fatalf("a session signed in to another account showed a week ending %s at 40%%, and the guard did not take it: %+v", formatTime(otherAccount), week)
		}
	})
}

func TestAnotherSessionsLowerReadingMakesNoBurstWithTheMultiSessionMaximumOff(t *testing.T) {
	render := func(sid string, at int64, five, fiveReset, weekReset float64) {
		recordStatusline(object{"session_id": sid, "rate_limits": object{
			"five_hour": object{"used_percentage": five, "resets_at": fiveReset},
			"seven_day": object{"used_percentage": float64(30), "resets_at": weekReset},
		}}, at, false)
	}
	for _, idle := range []bool{false, true} {
		sandboxFiles(t)
		now := nowSec()
		reset, weekReset := float64(now+2*3600), float64(now+3*86400)
		render("busy", now-1500, 86, reset, weekReset)
		render("busy", now-600, 88, reset, weekReset)
		if idle {
			render("idle", now-300, 76, reset, weekReset)
			render("idle", now-240, 76, reset, weekReset)
			if used := currentUsage(now).fiveHour.used; used != 76 {
				t.Fatalf("with usage.multiSessionMax off the idle session's own reading (76%%) is not the one stored: %v%%", used)
			}
		}
		render("busy", now-60, 89, reset, weekReset)
		usage := currentUsage(now)
		if plan := evaluate(releaseConfig(), usage, "", 0, false).wait; plan != nil {
			t.Errorf("idle session repainting: %v; the busy session's window rose from 86 %% to 89 %% in 24 minutes, and the guard pauses: %s at %v %% (burst %v); history %v",
				idle, plan.hit, plan.used, usage.fiveHour.burst, getMap(readJSON(files.usage), "history")["five_hour"])
		}
	}
}
