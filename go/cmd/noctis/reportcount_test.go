package main

import (
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
