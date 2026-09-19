package main

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"testing"
)

func FuzzNormalizeHookInput(f *testing.F) {
	f.Add("claude", `{"hook_event_name":"Stop","session_id":"s1"}`)
	f.Add("codex", `{"hook_event_name":"PostToolUse","session_id":"s1","tool_name":"Bash"}`)
	f.Add("antigravity", `{"hookEventName":"Stop","conversationId":"c1","terminationReason":"error","error":"rate limit"}`)
	f.Add("copilot", `{"hook_event_name":"agentStop","sessionId":"s1","toolName":"bash"}`)
	f.Add("droid", `{"hook_event_name":"Stop","session_id":"","nested":{"a":[1,2,{"b":null}]}}`)
	f.Add("claude", `{"hook_event_name":123,"session_id":{"x":1}}`)
	f.Fuzz(func(t *testing.T, host, payload string) {
		var raw object
		if err := json.Unmarshal([]byte(payload), &raw); err != nil || raw == nil {
			return
		}
		event, input := normalizeHookInput(host, raw)
		if input == nil {
			t.Fatalf("normalizeHookInput(%q) returned a nil input", host)
		}
		if strings.ContainsAny(event, "\x00") {
			t.Fatalf("event name carries a NUL: %q", event)
		}
		translated := translateOutput(host, event, object{"decision": "block", "reason": "stop right there"})
		if translated != nil {
			if encoded := marshalCompact(translated); len(encoded) == 0 || !json.Valid(encoded) {
				t.Fatalf("translateOutput(%q, %q) produced invalid JSON: %s", host, event, encoded)
			}
		} else if knownHookEvents[event] {

			t.Fatalf("translateOutput(%q, %q) dropped a block", host, event)
		}
	})
}

var knownHookEvents = map[string]bool{
	"SessionStart": true, "SessionEnd": true, "UserPromptSubmit": true, "PreToolUse": true,
	"PostToolBatch": true, "Stop": true, "StopFailure": true, "Notification": true,
	"PostModelSwitch": true, "PreCompact": true,
}

func FuzzAutoQueueItems(f *testing.F) {
	f.Add("- one thing to do here\n- another thing to do\n- a third thing to do now")
	f.Add(strings.Repeat("Add the thing and then fix the other thing. ", 20))
	f.Add("```\ncode fence\n```\nAdd a test for it and then deploy it and finally write the notes down.")
	f.Add("[x] done\n[ ] not done\n[] weird\nTODO: something")
	f.Fuzz(func(t *testing.T, prompt string) {
		items := autoQueueItems(prompt)
		if len(items) > autoQueueMaxItems {
			t.Fatalf("returned %d items, cap is %d", len(items), autoQueueMaxItems)
		}
		seen := map[string]bool{}
		for _, item := range items {
			if strings.TrimSpace(item) == "" {
				t.Fatalf("empty item from %q", prompt)
			}
			if strings.Contains(item, "\n") {
				t.Fatalf("item spans lines: %q", item)
			}
			if len([]rune(item)) > 201 {
				t.Fatalf("item longer than the cap: %d runes", len([]rune(item)))
			}
			if seen[item] {
				t.Fatalf("duplicate item %q", item)
			}
			seen[item] = true

			if words := strings.Fields(item); len(words) > 0 && !strings.Contains(prompt, words[0]) {
				t.Fatalf("item %q is not from the prompt", item)
			}
		}
	})
}

func FuzzDetectLanguage(f *testing.F) {
	f.Add("merhaba dünya nasılsın")
	f.Add("please fix the bug in the parser")
	f.Add("\x00\x01\x02")
	f.Fuzz(func(t *testing.T, text string) {
		lang := detectLanguage(text)
		if lang == "" {
			return
		}
		if len(lang) != 2 {
			t.Fatalf("language code is not two letters: %q", lang)
		}
		if again := detectLanguage(text); again != lang {
			t.Fatalf("detection is not deterministic: %q then %q", lang, again)
		}
		if catalogTable()[lang] == nil {
			t.Fatalf("detected %q, which has no message catalog", lang)
		}
	})
}

func FuzzParseUsagePayload(f *testing.F) {
	f.Add(`{"five_hour":{"utilization":10,"resets_at":"2026-01-01T00:00:00Z"}}`)
	f.Add(`{"limits":[{"kind":"session","percent":50,"resets_at":"2026-01-01T00:00:00Z"}]}`)
	f.Add(`{"limits":"nonsense"}`)
	f.Add(`{"limits":[{"kind":"session","percent":150,"resets_at":"2026-01-01T00:00:00Z"}]}`)
	f.Add(`{"limits":[{"kind":"session","percent":-20,"resets_at":"2026-01-01T00:00:00Z"}]}`)
	f.Add(`{"limits":[{"kind":"session","percent":"NaN","resets_at":"2026-01-01T00:00:00Z"}]}`)
	f.Add(`{"five_hour":{"utilization":300,"resets_at":"2026-01-01T00:00:00Z"}}`)
	f.Fuzz(func(t *testing.T, payload string) {
		var raw object
		if err := json.Unmarshal([]byte(payload), &raw); err != nil || raw == nil {
			return
		}
		parsed := parseUsagePayload(raw, scopedPatternForTest())
		for _, key := range []string{"five_hour", "seven_day", "fable"} {
			win := getMap(parsed, key)
			if win == nil {
				continue
			}

			used := numberOr(win, "used", -1)
			if math.IsNaN(used) || math.IsInf(used, 0) || used < 0 || used > 100 {
				t.Fatalf("%s used is %v", key, used)
			}
			resetsAt := numberOr(win, "resetsAt", math.NaN())
			if math.IsNaN(resetsAt) || math.IsInf(resetsAt, 0) {
				t.Fatalf("%s resetsAt is %v", key, resetsAt)
			}
		}

		for key, value := range parsed {
			if key != "five_hour" && key != "seven_day" && key != "fable" {
				t.Fatalf("unexpected key %q in the parsed usage", key)
			}
			win, ok := value.(object)
			if !ok {
				t.Fatalf("%s is not an object: %T", key, value)
			}
			for field := range win {
				if field != "used" && field != "resetsAt" {
					t.Fatalf("%s carries an unexpected field %q", key, field)
				}
			}
		}
	})
}

func FuzzQueueSnapshot(f *testing.F) {
	f.Add("- [ ] first\n- [x] second\n- [ ] (P0) third #tag (after 1)")
	f.Add("1. [ ] one\n2.[ ]two\n[]three\nTODO: four")
	f.Add(strings.Repeat("- [ ] item\n", 200))
	f.Fuzz(func(t *testing.T, content string) {
		path := t.TempDir() + "/TASKS.md"
		if err := writeTestFile(path, content); err != nil {
			t.Skip()
		}
		snapshot := queueSnapshot(path)
		if snapshot.total < 0 || len(snapshot.items) > queueMaxItems {
			t.Fatalf("bad snapshot: total=%d items=%d", snapshot.total, len(snapshot.items))
		}
		if snapshot.blocked < 0 || snapshot.blocked > snapshot.total {
			t.Fatalf("blocked=%d is not within total=%d", snapshot.blocked, snapshot.total)
		}
		for _, item := range snapshot.items {
			if strings.Contains(item, "\n") {
				t.Fatalf("item spans lines: %q", item)
			}
		}

		priorities := make([]int, 0, len(snapshot.items))
		for _, item := range snapshot.items {
			priority := 5
			if found := queuePriority.FindStringSubmatch(item); found != nil {
				priority = int(found[1][0] - '0')
			}
			priorities = append(priorities, priority)
		}
		if !sort.IntsAreSorted(priorities) {
			t.Fatalf("items are not in priority order: %v (%v)", snapshot.items, priorities)
		}

		second := queueSnapshot(path)
		if second.total != snapshot.total || len(second.items) != len(snapshot.items) {
			t.Fatalf("queueSnapshot is not stable: %d/%d then %d/%d", snapshot.total, len(snapshot.items), second.total, len(second.items))
		}
	})
}
