package main

import (
	"os"
	"path/filepath"
	"strings"
)

// copiedSkillCommands names each command in a message, such as /noctis:pause,
// the way it is typed where it exists. Where Claude Code runs without plugins,
// as on the web, noctis's skills are copied into the project's .claude/skills
// (or the account's skills folder) as noctis-pause, noctis-start and so on, and
// are typed /noctis-pause; a notice that named /noctis:pause there would point
// at a command that does not exist. A command with no such copy keeps the
// plugin's name.
func copiedSkillCommands(text string) string {
	prefix := "/" + pluginName + ":"
	if activeHost != "claude" || !strings.Contains(text, prefix) {
		return text
	}
	project := claudeProjectDir()
	if project == "" {
		project, _ = os.Getwd()
	}
	folders := []string{}
	if project != "" {
		folders = append(folders, filepath.Join(project, ".claude", "skills"))
	}
	if files.configDir != "" {
		folders = append(folders, filepath.Join(files.configDir, "skills"))
	}
	pairs := []string{}
	for _, name := range sortedKeys(controlSkills) {
		for _, folder := range folders {
			if statSafe(filepath.Join(folder, pluginName+"-"+name, "SKILL.md")) != nil {
				pairs = append(pairs, prefix+name, "/"+pluginName+"-"+name)
				break
			}
		}
	}
	if len(pairs) == 0 {
		return text
	}
	return strings.NewReplacer(pairs...).Replace(text)
}
