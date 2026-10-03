//go:build !windows

package main

import (
	"os"
	"slices"
	"testing"
)

func TestWhatTheSessionWritesAfterAFailedRelaunchCountsForTheRetryOnceTheClockWentBack(t *testing.T) {
	answer := func(at float64) string {
		return string(marshalCompact(object{"type": "assistant", "timestamp": transcriptStamp(at),
			"message": object{"role": "assistant", "content": []any{object{"type": "text", "text": "Picking up the parser."}}}}))
	}
	prompt := func(at float64) string { return userPromptLine(at, "now run the tests") }
	cases := []struct {
		name, kind string
		written    func(float64) string
	}{
		{"an answer after a pause at a limit", "batch", answer},
		{"an answer after an overload", "stopfailure", answer},
		{"a prompt typed in its window after a pause at a limit", "batch", prompt},
		{"an answer after Fable's limit", "fable", answer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sid := "clock-back-retry-" + tc.kind
			calls := relaunchSandboxWith(t, fakeClaude(promptLine, apiFailureLine, "exit 1"))
			previousOffset := timeOffset
			t.Cleanup(func() { timeOffset = previousOffset })
			if tc.kind == "fable" {
				cwd, now := t.TempDir(), float64(nowSec())
				transcript := writeTranscriptAt(t, cwd, []string{userPromptLine(now-60, "fix the parser")}, now-60)
				t.Setenv("NOCTIS_TEST_TRANSCRIPT", transcript)
				handleFableHit("batch", object{"session_id": sid, "cwd": cwd, "transcript_path": transcript}, loadConfig(), fableHitDecision())
				timeOffset += int64(numberOr(waitOf(sid), "resumeAt", 0)) - nowSec() + 1
			} else {
				parkForRelaunch(t, sid, tc.kind, "headless")
			}

			resumeWait(sid, "")
			wait := waitOf(sid)
			if wait == nil || getString(wait, "hit") != "relaunch" {
				t.Fatalf("the first relaunch did nothing and its retry was not parked: %v (journal %v)", wait, journaledFor(sid))
			}
			timeOffset -= 3600
			transcript, err := os.OpenFile(getString(wait, "transcript"), os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = transcript.WriteString(tc.written(float64(nowSec())) + "\n")
			transcript.Close()
			if err != nil {
				t.Fatal(err)
			}
			timeOffset += int64(numberOr(wait, "resumeAt", 0)) - nowSec() + 1
			statusReadingFrom(sid, nowSec(), 3, float64(nowSec()+18000), 20, float64(nowSec()+3*86400))

			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("the session went on after its relaunch failed, with the clock set back an hour, and the retry relaunched it again: %d launch(es) in total, want 1 (journal %v)", got, journaledFor(sid))
			}
			if left := waitOf(sid); left != nil {
				t.Fatalf("the retry kept the pause of a session that went on: %v", left)
			}
			if !slices.Contains(journaledFor(sid), "skip-launch") {
				t.Fatalf("the retry ended the pause without saying why in the journal: %v", journaledFor(sid))
			}
		})
	}
}
