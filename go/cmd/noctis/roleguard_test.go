package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestARoleValueThatIsNotAModelNameNeverReachesAnAgentFile(t *testing.T) {
	sandboxFiles(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "---\nname: lite\ntools: WebSearch, Read\ndisallowedTools: Bash\nmodel: opus\n---\nDo research.\n"
	lite := filepath.Join(root, "agents", "lite.md")
	if err := os.WriteFile(lite, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []object{
		{"model": "haiku\ntools: Bash, Write"},
		{"model": "sonnet", "effort": "high\ndisallowedTools: none"},
		{"model": "sonnet # the fast one"},
	} {
		for _, anyModel := range []bool{false, true} {
			syncAgentFiles(root, object{"research": spec}, anyModel)
			if content, _ := os.ReadFile(lite); string(content) != original {
				t.Fatalf("research role %q was written into the agent's frontmatter (provider ids taken: %v):\n%s", spec, anyModel, content)
			}
			if problems := badRoleValues(object{"research": spec}, anyModel); len(problems) != 1 {
				t.Fatalf("the doctor does not name the research role %q (provider ids taken: %v): %v", spec, anyModel, problems)
			}
		}
	}
	if changed := syncAgentFiles(root, object{"research": object{"model": "claude-sonnet-5[1m]", "effort": "high"}}, false); changed != 1 {
		t.Fatal("a real model name with a context suffix was not written")
	}
	if content, _ := os.ReadFile(lite); !strings.Contains(string(content), "model: claude-sonnet-5[1m]\neffort: high\n---") {
		t.Fatalf("the agent file does not carry the new model and effort:\n%s", content)
	}
	for _, anyModel := range []bool{false, true} {
		if _, err := parseRoleFlag("research", "haiku tools", anyModel); err == nil {
			t.Fatalf("setup accepted a --research model name with a space in it (provider ids taken: %v)", anyModel)
		}
	}
	for _, value := range []string{"opus", "claude-opus-5-5[1m]:max", "claude-haiku-4-5@20251001"} {
		if _, err := parseRoleFlag("code", value, false); err != nil {
			t.Fatalf("setup refused the model value %q: %v", value, err)
		}
	}
}
