package main

import (
	"path/filepath"
	"testing"
)

func TestTheQueueReasonSentBackAsAPromptDoesNotResetTheIdleCount(t *testing.T) {
	cfg, project := copilotSandbox(t)
	sid := "cp-idle"
	maxIdle := int(numberOr(section(cfg, "queue"), "maxIdleContinues", 4))
	blocked := 0
	for round := 0; round < 3*maxIdle; round++ {
		stop := hostHook(t, "copilot", copilotPayload(sid, project, object{"transcriptPath": filepath.Join(project, "events.jsonl"), "stopReason": "end_turn", "stop_hook_active": round > 0}))
		if getString(stop, "decision") != "block" {
			break
		}
		blocked++
		hostHook(t, "copilot", copilotPayload(sid, project, object{"prompt": getString(stop, "reason")}))
	}
	if blocked > maxIdle {
		t.Fatalf("Copilot sends the Stop hook's reason back as the next prompt, and each one reset the idle count: the queue that made no progress was continued %d times, the cap is %d", blocked, maxIdle)
	}

	updateState(func(state object) {
		stateMap(state, "stopGuard")[sid] = object{"forced": float64(3), "idle": float64(3), "lastOpen": float64(2), "at": float64(nowSec())}
	})
	hostHook(t, "copilot", copilotPayload(sid, project, object{"prompt": "also cover the flag parser"}))
	if idle := numberOr(getMap(getMap(readState(), "stopGuard"), sid), "idle", -1); idle != 0 {
		t.Fatalf("a prompt the user typed must still reset the idle count, it is %v", idle)
	}
}
