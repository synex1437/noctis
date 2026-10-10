package main

import (
	"strings"
	"testing"
)

func TestACloudSessionPausedPastWhatItsHookHoldsIsToldToResumeItItself(t *testing.T) {
	previous := locale
	t.Cleanup(func() { locale = previous })
	locale = "en"
	cfg, project := cloudSandbox(t, t.TempDir(), nil)
	writeUsage(40, 98, 0)
	sid := "cloud-weekly"
	batch := agentHookInput("PostToolBatch", sid, project, nil)
	output := hookOutput(t, onPostToolBatch, batch, cfg)
	stop := getString(output, "stopReason")
	if !stoppedBy(output) || strings.Contains(stop, "auto-resume") || !strings.Contains(stop, "resume it yourself") {
		t.Fatalf("a cloud session paused for a weekly reset days away was promised a resume that nothing there keeps: %v", output)
	}
	if scheduled := getMap(pendingWait(sid), "scheduled"); getString(scheduled, "method") != "cloud" {
		t.Fatalf("the cloud pause was not stored with the method cloud: %v", scheduled)
	}
	if reason := journaledReason(sid, "cloud-no-wake"); !strings.Contains(reason, "starts no runner") {
		t.Errorf("noctis why does not say that nothing resumes the paused cloud session: %v", journaledFor(sid))
	}

	joined := hookOutput(t, onPostToolBatch, batch, cfg)
	if stop := getString(joined, "stopReason"); !stoppedBy(joined) || strings.Contains(stop, "auto-resume") || !strings.Contains(stop, "resume it yourself") {
		t.Errorf("a second batch that joins the cloud pause was promised a resume: %v", joined)
	}

	now := nowSec()
	burst := &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 88, threshold: 92, until: float64(now + 3600), hit: "burst"}
	updateState(func(state object) {
		stateMap(state, "waits")["cloud-replaced"] = object{"window": "seven_day", "label": windowLabel("seven_day"), "used": float64(96), "hit": "threshold",
			"threshold": float64(95), "until": float64(now + 3*86400), "resumeAt": float64(now + 3*86400), "inHook": false, "startedAt": float64(now), "holder": "other"}
	})
	replaced := holdWait("batch", "cloud-replaced", cfg, burst, burst.until, object{"startedAt": float64(now - 60), "holder": "gone", "resumeAt": burst.until}, false)
	if strings.Contains(replaced.stop, "auto-resume") || !strings.Contains(replaced.stop, "resume it yourself") {
		t.Errorf("a cloud wait held in the hook and replaced by a pause no hook holds promises a resume: %q", replaced.stop)
	}
}
