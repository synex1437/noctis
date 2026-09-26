package main

import (
	"crypto/rand"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
)

func contextTokensOf(window object) (float64, bool) {
	if current := getMap(window, "current_usage"); current != nil {
		total := 0.0
		for _, key := range []string{"input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"} {
			if value, ok := getNumber(current, key); ok && value > 0 {
				total += value
			}
		}
		if total > 0 {
			return math.Round(total), true
		}
	}
	percent, hasPercent := getNumber(window, "used_percentage")
	size, hasSize := getNumber(window, "context_window_size")
	if hasPercent && hasSize && percent >= 0 && percent <= 100 && size > 0 {
		return math.Round(percent * size / 100), true
	}
	return 0, false
}

func sessionContextTokens(sid string) (float64, bool) {
	return getNumber(getMap(getMap(readJSON(files.usage), "sessions"), sid), "contextTokens")
}

func subagentNote(cfg object, tokens float64, known bool) string {
	above := numberOr(section(cfg, "queue"), "subagentAboveTokens", 100000)
	if !known || above <= 0 || tokens < above || !currentHost().agents {
		return ""
	}
	return fmt.Sprintf(" This session's context already holds about %s tokens: unless the item is a quick edit, hand it to a fresh general-purpose subagent with a brief that stands on its own (the goal, the files and decisions it needs, what done means), then check its work and tick the item yourself.", approxCount(tokens))
}

type freshPlan struct {
	sid    string
	note   string
	tokens float64
	idle   float64
}

func freshStartFor(cfg, state, wait object, sid string, now int64) freshPlan {
	resume := section(cfg, "resume")
	after, above := numberOr(resume, "freshAfterMinutes", 60), numberOr(resume, "freshAboveTokens", 100000)
	if currentHost().id != "claude" || after <= 0 || above <= 0 || getBool(wait, "freshFailed", false) {
		return freshPlan{}
	}
	tokens, known := getNumber(wait, "contextTokens")
	if !known {
		tokens, known = sessionContextTokens(sid)
	}
	idle := float64(now) - lastActiveAt(wait)
	if !known || tokens < above || idle < after*60 {
		return freshPlan{}
	}
	if len(workflowLaunches(state, sid)) > 0 {
		return freshPlan{}
	}
	checkpoint := getMap(getMap(state, "checkpoints"), sid)
	note := getString(checkpoint, "path")
	if note == "" || getBool(checkpoint, "consumed", false) || statSafe(note) == nil {
		return freshPlan{}
	}
	fresh := newSessionID()
	if fresh == "" {
		return freshPlan{}
	}
	return freshPlan{sid: fresh, note: note, tokens: tokens, idle: idle}
}

func lastActiveAt(wait object) float64 {
	if info := statSafe(getString(wait, "transcript")); info != nil {
		return float64(info.ModTime().Unix())
	}
	return numberOr(wait, "startedAt", float64(nowSec()))
}

func newSessionID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

func pauseLength(seconds float64) string {
	if seconds < 5400 {
		return fmt.Sprintf("%d minutes", int(math.Round(seconds/60)))
	}
	return strconv.FormatFloat(math.Round(seconds/360)/10, 'f', -1, 64) + " hours"
}

func freshPrompt(sid string, plan freshPlan, transcript string) string {
	prompt := fmt.Sprintf("[noctis] This is a fresh session taking over from session %s, which paused for %s with about %s tokens of context: resuming it would have read all of that again with a cold cache, so this session starts clean. First read the handoff note %s.", sid, pauseLength(plan.idle), approxCount(plan.tokens), plan.note)
	if transcript != "" {
		prompt += " The earlier conversation is in " + transcript + "; search it for a detail you need instead of reading it whole."
	}
	return prompt + " "
}

var freshSessionRecords = []string{"autoQueues", "stopGuard", "stopDay"}

func moveSessionRecords(state object, from, to string) {
	for _, name := range freshSessionRecords {
		records := stateMap(state, name)
		if record, ok := records[from]; ok && records[to] == nil {
			records[to] = record
			delete(records, from)
		}
	}
}

func undoFreshStart(sid, fresh string) {
	updateState(func(next object) {
		if getMap(getMap(next, "freshStarts"), fresh) == nil {
			return
		}
		moveSessionRecords(next, fresh, sid)
		delete(stateMap(next, "freshStarts"), fresh)
	})
}

func noteFreshStart(state object, sid, transcript string) bool {
	start := getMap(getMap(state, "freshStarts"), sid)
	if start == nil {
		return false
	}
	if transcript != "" && getString(start, "transcript") != transcript {
		updateState(func(next object) {
			if record := getMap(getMap(next, "freshStarts"), sid); record != nil {
				record["transcript"] = transcript
			}
		})
	}
	return true
}

func earlierFreshStart(state object, sid string, startedAt float64) (string, float64) {
	for fresh, raw := range getMap(state, "freshStarts") {
		entry, _ := raw.(object)
		if getString(entry, "from") == sid && numberOr(entry, "waitStartedAt", -1) == startedAt {
			return fresh, numberOr(entry, "at", 0)
		}
	}
	return "", 0
}

func freshTranscript(wait object, fresh string) string {
	if recorded := getString(getMap(getMap(readState(), "freshStarts"), fresh), "transcript"); recorded != "" {
		return recorded
	}
	old := getString(wait, "transcript")
	if old == "" {
		return ""
	}
	if found, _ := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(old)), "*", fresh+".jsonl")); len(found) > 0 {
		return found[0]
	}
	return filepath.Join(filepath.Dir(old), fresh+".jsonl")
}

func freshProgress(wait object, fresh string) object {
	view := cloneObject(wait)
	view["transcript"] = freshTranscript(wait, fresh)
	return view
}
