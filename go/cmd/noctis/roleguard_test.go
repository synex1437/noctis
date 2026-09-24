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
		syncAgentFiles(root, object{"research": spec})
		if content, _ := os.ReadFile(lite); string(content) != original {
			t.Fatalf("research role %q was written into the agent's frontmatter:\n%s", spec, content)
		}
		if problems := badRoleValues(object{"research": spec}); len(problems) != 1 {
			t.Fatalf("the doctor does not name the research role %q: %v", spec, problems)
		}
	}
	if changed := syncAgentFiles(root, object{"research": object{"model": "claude-sonnet-5[1m]", "effort": "high"}}); changed != 1 {
		t.Fatal("a real model name with a context suffix was not written")
	}
	if content, _ := os.ReadFile(lite); !strings.Contains(string(content), "model: claude-sonnet-5[1m]\neffort: high\n---") {
		t.Fatalf("the agent file does not carry the new model and effort:\n%s", content)
	}
	if _, err := parseRoleFlag("research", "haiku tools"); err == nil {
		t.Fatal("setup accepted a --research model name with a space in it")
	}
	for _, value := range []string{"opus", "claude-opus-5-5[1m]:max", "claude-haiku-4-5@20251001"} {
		if _, err := parseRoleFlag("code", value); err != nil {
			t.Fatalf("setup refused the model value %q: %v", value, err)
		}
	}
}
