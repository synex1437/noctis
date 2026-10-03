package main

import (
	"strings"
	"testing"
)

func TestAWorkflowPastThePausePointIsNotToldOfNegativeRoom(t *testing.T) {
	cfg := creditsConfig()
	cfg["workflow"] = object{"gate": true}
	reset := float64(nowSec() + 3600)
	past := gateWorkflowLaunch(cfg, decision{usage: usageFrom(92.8, 30, reset)}, nil)
	if !strings.Contains(past, "the 5h window is already past its pause point") || strings.Contains(past, "-0.8") || strings.Contains(past, "only") {
		t.Errorf("a workflow refused with the 5-hour window 0.8 points past its pause point was told of negative room: %q", past)
	}
	at := gateWorkflowLaunch(cfg, decision{usage: usageFrom(92, 30, reset)}, nil)
	if !strings.Contains(at, "the 5h window is at its pause point") || strings.Contains(at, "only 0 points") {
		t.Errorf("a workflow refused with the 5-hour window at its pause point was told of 0 points of room: %q", at)
	}
	scoped := usageFrom(20, 30, reset)
	scoped.fable = &window{used: 97, resetsAt: reset}
	if fable := gateWorkflowLaunch(cfg, decision{usage: scoped}, nil); !strings.Contains(fable, "the Fable window is already past its pause point") || strings.Contains(fable, "-2") {
		t.Errorf("a workflow that may run on Fable, refused with Fable's window 2 points past its pause point, was told of negative room: %q", fable)
	}
	short := gateWorkflowLaunch(cfg, decision{usage: usageFrom(80, 30, reset)}, nil)
	if !strings.Contains(short, "only 12 points of the 5h window are left before the pause point") || !strings.Contains(short, "A workflow needs 25 points of room") {
		t.Errorf("a workflow refused with 12 points of room lost its wording: %q", short)
	}
}
