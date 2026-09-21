package main

import "testing"

func TestTickPaceStaysTightNearTheResume(t *testing.T) {
	watch := &waitWatch{pollEvery: 300}
	if pace := watch.tickPace(sleepFarSeconds); pace != sleepTickSeconds {
		t.Fatalf("inside the near window the tick must not relax: got %v", pace)
	}
	if pace := watch.tickPace(60); pace != sleepTickSeconds {
		t.Fatalf("a minute from the resume the tick must not relax: got %v", pace)
	}
}

func TestTickPaceRelaxesWhenTheResumeIsFarAway(t *testing.T) {
	watch := &waitWatch{pollEvery: 300}
	if pace := watch.tickPace(4 * 3600); pace != sleepFarTickSeconds {
		t.Fatalf("hours out, the tick should relax to %v: got %v", float64(sleepFarTickSeconds), pace)
	}
}

func TestTickPaceIsNeverSlowerThanTheConfiguredPoll(t *testing.T) {
	for _, pollEvery := range []float64{3, 10, 30, 59} {
		watch := &waitWatch{pollEvery: pollEvery}
		pace := watch.tickPace(7 * 86400)
		if pace > pollEvery {
			t.Fatalf("a poll of %vs must not be outpaced by a tick of %vs", pollEvery, pace)
		}
	}
}

func TestTickPaceWithEarlyResetOff(t *testing.T) {
	watch := &waitWatch{pollEvery: 0}
	if pace := watch.tickPace(4 * 3600); pace != sleepFarTickSeconds {
		t.Fatalf("with nothing to poll for, a distant resume should relax: got %v", pace)
	}
	if pace := watch.tickPace(120); pace != sleepTickSeconds {
		t.Fatalf("close to the resume the tick stays at the base rate: got %v", pace)
	}
}

func TestSleepUntilPacedAsksThePaceEachRound(t *testing.T) {
	seen := []float64{}
	ticks := 0
	done := sleepUntilPaced(float64(nowSec())+2, func(remaining float64) float64 {
		seen = append(seen, remaining)
		return 1
	}, func() bool {
		ticks++
		return ticks >= 2
	})
	if done {
		t.Fatalf("a tick that returns true should stop the wait")
	}
	if len(seen) < 1 {
		t.Fatalf("the pace should have been consulted at least once: %v", seen)
	}
}
