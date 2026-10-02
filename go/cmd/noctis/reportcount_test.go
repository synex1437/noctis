package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheReportCountsEachResponseOnceWhateverItsBlocks(t *testing.T) {
	sandboxFiles(t)
	project := filepath.Join(files.configDir, "projects", "-work-app")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	usage := object{"input_tokens": float64(100), "output_tokens": float64(50), "cache_read_input_tokens": float64(1000), "cache_creation_input_tokens": float64(10)}
	line := func(id, request string, block object) string {
		return string(marshalCompact(object{"type": "assistant", "timestamp": stamp, "requestId": request,
			"message": object{"id": id, "model": "claude-opus-5-5", "role": "assistant", "content": []any{block}, "usage": usage}}))
	}
	rows := []string{
		line("msg_1", "req_1", object{"type": "thinking", "thinking": "…"}),
		line("msg_1", "req_1", object{"type": "text", "text": "Reading the parser."}),
		line("msg_1", "req_1", object{"type": "tool_use", "name": "Read", "input": object{}}),
		line("msg_2", "req_2", object{"type": "text", "text": "Done."}),
	}
	if err := os.WriteFile(filepath.Join(project, "s1.jsonl"), []byte(strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "s1-fork.jsonl"), []byte(rows[3]+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := collectReport(object{"models": object{"primary": "fable"}}, 1)
	bucket := data.byModel["claude-opus-5-5"]
	if bucket == nil || bucket.calls != 2 || bucket.input != 200 || bucket.output != 100 || bucket.cacheRead != 2000 {
		t.Fatalf("two responses, one written as three content blocks and one copied into a second transcript, were counted as %+v; want 2 calls, 200 input, 100 output, 2000 cache reads", bucket)
	}
}

func TestAReportAskedForMoreDaysThanATimeSpanHoldsStillCountsEveryTranscript(t *testing.T) {
	sandboxFiles(t)
	project := filepath.Join(files.configDir, "projects", "-work-app")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	call := object{"type": "assistant", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "requestId": "req_1",
		"message": object{"id": "msg_1", "model": "claude-opus-5-5", "role": "assistant", "content": []any{}, "usage": object{"input_tokens": float64(100), "output_tokens": float64(50)}}}
	if err := os.WriteFile(filepath.Join(project, "s1.jsonl"), append(marshalCompact(call), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	defer func(previous parsedArgs) { args = previous }(args)
	for _, days := range []string{"7", "106753", "999999", "1e300"} {
		args = parseArgs([]string{"report", "--days", days, "--json"})
		report := object{}
		if output := capturedStdout(t, runReport); jsonUnmarshalObject([]byte(output), &report) != nil || numberOr(report, "transcripts", 0) != 1 || numberOr(report, "days", 0) > lookbackMaxDays {
			t.Fatalf("report --days %s counted %v transcripts over %v days; want today's one transcript over at most %d days:\n%s", days, report["transcripts"], report["days"], lookbackMaxDays, output)
		}
	}
}

func TestTheReportFilesEachCallUnderTheLocalDayItWasMadeOn(t *testing.T) {
	sandboxFiles(t)
	defer func(previous *time.Location) { time.Local = previous }(time.Local)
	time.Local = time.FixedZone("UTC+3", 3*3600)
	project := filepath.Join(files.configDir, "projects", "-work-tz")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	yesterday := time.Now().In(time.Local).AddDate(0, 0, -1)
	early := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 1, 30, 0, 0, time.Local)
	late := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 12, 0, 0, 0, time.Local)
	rows := []string{}
	for index, at := range []time.Time{early, late} {
		rows = append(rows, string(marshalCompact(object{"type": "assistant", "timestamp": at.UTC().Format(time.RFC3339Nano), "requestId": fmt.Sprintf("req_%d", index),
			"message": object{"id": fmt.Sprintf("msg_%d", index), "model": "claude-opus-5-5", "role": "assistant", "content": []any{}, "usage": object{"input_tokens": float64(100), "output_tokens": float64(50)}}})))
	}
	if err := os.WriteFile(filepath.Join(project, "s1.jsonl"), []byte(strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := collectReport(object{"models": object{"primary": "fable"}}, 7)
	day := early.Format("2006-01-02")
	if bucket := data.byDay[day]; len(data.byDay) != 1 || bucket == nil || bucket.calls != 2 {
		t.Fatalf("two calls made at 01:30 and 12:00 local time on %s were filed under %v; want both under that day", day, sortedKeys(data.byDay))
	}
}

func TestTheReportCountsTheCallsOfATranscriptLargerThan64MiB(t *testing.T) {
	sandboxFiles(t)
	project := filepath.Join(files.configDir, "projects", "-work-app")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	toolResult := append(marshalCompact(object{"type": "user", "timestamp": stamp,
		"message": object{"role": "user", "content": []any{object{"type": "tool_result", "content": strings.Repeat("x", 1<<20)}}}}), '\n')
	transcript, err := os.Create(filepath.Join(project, "long.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for index := range 3 {
		call := marshalCompact(object{"type": "assistant", "timestamp": stamp, "requestId": fmt.Sprintf("req_%d", index),
			"message": object{"id": fmt.Sprintf("msg_%d", index), "model": "claude-opus-5-5", "role": "assistant", "content": []any{}, "usage": object{"input_tokens": float64(100000), "output_tokens": float64(5000)}}})
		if _, err := transcript.Write(append(call, '\n')); err != nil {
			t.Fatal(err)
		}
		for range 22 {
			if _, err := transcript.Write(toolResult); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := transcript.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(transcript.Name())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 64<<20 {
		t.Fatalf("the transcript is %d bytes; it should be larger than 64 MiB", info.Size())
	}

	data := collectReport(object{"models": object{"primary": "opus"}}, 1)

	if bucket := data.byModel["claude-opus-5-5"]; data.transcripts != 1 || bucket == nil || bucket.calls != 3 || bucket.input != 300000 || bucket.output != 15000 {
		t.Fatalf("a long session's transcript of three calls among large tool results was counted as %+v in %d transcripts; want 3 calls, 300000 input and 15000 output tokens", bucket, data.transcripts)
	}
}
