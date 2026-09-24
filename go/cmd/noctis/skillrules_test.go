package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type shippedSkill struct {
	allowed        []string
	modelInvocable bool
	commands       []string
}

func readShippedSkill(t *testing.T, name string) shippedSkill {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "skills", name, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(strings.ReplaceAll(string(content), "\r\n", "\n"), "---\n", 3)
	if len(parts) != 3 || parts[0] != "" {
		t.Fatalf("skills/%s/SKILL.md has no frontmatter", name)
	}
	skill := shippedSkill{modelInvocable: true}
	lines := strings.Split(parts[1], "\n")
	for index := 0; index < len(lines); index++ {
		key, value, found := strings.Cut(lines[index], ":")
		if !found || strings.HasPrefix(lines[index], " ") {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "allowed-tools":
			for _, item := range strings.Split(value, ",") {
				if item = strings.TrimSpace(item); item != "" {
					skill.allowed = append(skill.allowed, item)
				}
			}
			for index+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[index+1]), "- ") {
				index++
				skill.allowed = append(skill.allowed, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[index]), "- ")))
			}
		case "disable-model-invocation":
			skill.modelInvocable = value != "true"
		}
	}
	inBlock := false
	for _, line := range strings.Split(parts[2], "\n") {
		switch {
		case strings.HasPrefix(line, "```bash"):
			inBlock = true
		case strings.HasPrefix(line, "```"):
			inBlock = false
		case inBlock && strings.TrimSpace(line) != "":
			skill.commands = append(skill.commands, strings.TrimSpace(line))
		}
	}
	return skill
}

const pluginBinaryCall = `"${CLAUDE_PLUGIN_ROOT}/bin/noctis" `

func bashRulePattern(rule string) (*regexp.Regexp, bool) {
	if !strings.HasPrefix(rule, "Bash(") || !strings.HasSuffix(rule, ")") {
		return nil, false
	}
	content := strings.TrimSuffix(strings.TrimPrefix(rule, "Bash("), ")")
	return regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(content), `\*`, ".*") + "$"), true
}

func TestTheSkillsPreApproveOnlyTheCommandsTheyRun(t *testing.T) {
	for _, name := range []string{"setup", "status", "pause", "resume"} {
		t.Run(name, func(t *testing.T) {
			skill := readShippedSkill(t, name)
			if len(skill.allowed) == 0 || len(skill.commands) == 0 {
				t.Fatalf("skills/%s names no allowed tools (%q) or runs no command (%q)", name, skill.allowed, skill.commands)
			}
			var patterns []*regexp.Regexp
			for _, rule := range skill.allowed {
				pattern, isBash := bashRulePattern(rule)
				content := strings.TrimSuffix(strings.TrimPrefix(rule, "Bash("), ")")
				rest := strings.TrimPrefix(content, pluginBinaryCall)
				if !isBash || !strings.HasPrefix(content, pluginBinaryCall) || len(strings.Fields(rest)) == 0 || strings.Contains(strings.TrimSuffix(rest, " *"), "*") {
					t.Fatalf("skills/%s pre-approves %q: while the skill runs, Claude may use that for other commands without asking; only the plugin's own binary with the skill's subcommand may be pre-approved", name, rule)
				}
				patterns = append(patterns, pattern)
			}
			for _, command := range skill.commands {
				covered := false
				for _, pattern := range patterns {
					covered = covered || pattern.MatchString(command)
				}
				if !covered {
					t.Fatalf("skills/%s runs %q, which none of its rules %q pre-approves", name, command, skill.allowed)
				}
			}
		})
	}
}

func TestOnlyThePersonCanStartTheSkillsThatLowerProtection(t *testing.T) {
	for _, name := range []string{"setup", "pause"} {
		if readShippedSkill(t, name).modelInvocable {
			t.Errorf("skills/%s can be started by Claude itself (a file in the repository can ask it to); it needs disable-model-invocation: true", name)
		}
	}
}
