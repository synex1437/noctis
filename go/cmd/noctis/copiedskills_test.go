package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func x11CopySkills(t *testing.T, folder string, names ...string) {
	t.Helper()
	for _, name := range names {
		dir := filepath.Join(folder, pluginName+"-"+name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+pluginName+"-"+name+"\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNoticesNameTheCommandOfASkillCopiedIntoTheProject(t *testing.T) {
	sandboxFiles(t)
	project := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	if got := T("session.pauseHint"); !strings.Contains(got, "/noctis:pause 120") {
		t.Fatalf("with no copy of the skills a notice must name the plugin's command: %q", got)
	}
	x11CopySkills(t, filepath.Join(project, ".claude", "skills"), "pause", "start", "stop")
	x11CopySkills(t, filepath.Join(files.configDir, "skills"), "resume")
	for _, notice := range []struct{ text, want, not string }{
		{T("session.pauseHint"), "/noctis-pause 120", "/noctis:pause"},
		{T("wait.savedHint"), "/noctis-pause 120", "/noctis:pause"},
		{T("queue.startUsage", pluginName), "/noctis-start", "/noctis:start"},
		{T("queue.autoNotice", 3, pluginName), "/noctis-stop", "/noctis:stop"},
		{T("queue.startPaused", "12:00", pluginName), "/noctis-resume", "/noctis:resume"},
		{T("status.notSetUp", pluginName), "/noctis:setup", "/noctis-setup"},
	} {
		if !strings.Contains(notice.text, notice.want) || strings.Contains(notice.text, notice.not) {
			t.Errorf("with the skills copied as noctis-pause, noctis-start, noctis-stop (project) and noctis-resume (account), a notice said %q; want %s in it, not %s", notice.text, notice.want, notice.not)
		}
	}
}

func TestACopiedSkillsCommandIsTakenAsAControlPrompt(t *testing.T) {
	cfg, project := controlSandbox(t, 95, 40)
	for i, prompt := range []string{"/noctis-pause 120", "/noctis-resume", "/noctis-status"} {
		sid := "x11-" + strconv.Itoa(i)
		if output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, prompt), cfg); output != nil {
			t.Errorf("%q, typed to a copy of the skill in .claude/skills, did not pass the pause point as /noctis:%s does: %v", prompt, strings.TrimPrefix(strings.Fields(prompt)[0], "/noctis-"), output)
		}
	}
	writeUsage(20, 10, float64(nowSec()+7200))
	writeJobFile(t, project, "x11.md", "- add a login page\n- add a logout button\n")
	hookOutput(t, onUserPromptSubmit, promptInput("x11-queue", project, "/noctis-start x11.md"), cfg)
	if sessionQueueFile(cfg, "x11-queue", project) == "" {
		t.Fatal("/noctis-start x11.md started no queue")
	}
	output := hookOutput(t, onUserPromptSubmit, promptInput("x11-queue", project, "/noctis-stop"), cfg)
	if getString(output, "reason") != T("queue.stopDone", 0, 2, "x11.md") || sessionQueueFile(cfg, "x11-queue", project) != "" {
		t.Errorf("/noctis-stop did not stop the queue /noctis-start began: %v", output)
	}
}
