package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	launchStartTimeout = 20 * time.Second
	launchPidTimeout   = 60 * time.Second
	launchMaxWait      = 7 * 24 * time.Hour
	launchPollInterval = 2 * time.Second
)

func launchFiles(sid string) (spec, started, pidFile string) {
	base := filepath.Join(files.launches, safeName(sid))
	return base + ".json", base + ".started", base + ".pid"
}

func readPidFile(path string) int {
	content, err := readFileShared(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(content)))
	return pid
}

func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if statSafe(path) != nil {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func waitForPid(pid int) {
	deadline := time.Now().Add(launchMaxWait)
	for time.Now().Before(deadline) && processAlive(pid) {
		time.Sleep(launchPollInterval)
	}
}

func recordLaunch(sid string, pid int, how string) {
	updateState(func(state object) {
		stateMap(state, "launched")[sid] = object{"pid": float64(pid), "at": float64(nowSec()), "how": how}
	})
}

func clearLaunch(sid string) {
	updateState(func(state object) { delete(stateMap(state, "launched"), sid) })
}

func closePreviousLaunch(cfg object, sid string, wait object) {
	if !getBool(section(cfg, "resume"), "closePrevious", true) {
		return
	}
	record := getMap(getMap(readState(), "launched"), sid)
	if record == nil {
		return
	}
	pid := int(numberOr(record, "pid", 0))
	defer clearLaunch(sid)
	if pid <= 0 || pid == os.Getpid() || !processAlive(pid) {
		return
	}

	if age := float64(nowSec()) - numberOr(record, "at", 0); age < 0 || age > launchMaxWait.Seconds() {
		logInfo("launch record for %s is %ds old; not closing pid %d", sid, int(age), pid)
		return
	}
	if wait != nil && sessionActiveAfter(wait, float64(nowSec()-300)) {
		logInfo("previous window of %s (pid %d) was active in the last five minutes; leaving it open", sid, pid)
		return
	}
	if !looksLikeSessionProcess(pid) {
		logInfo("pid %d is no longer the session's claude process; leaving it alone", pid)
		return
	}
	if err := terminateProcess(pid); err != nil {
		warn("previous window of %s (pid %d) could not be closed: %v", sid, pid, err)
		return
	}
	journal(sid, "resume", "close-previous", fmt.Sprintf("pid %d", pid), nil)
	logInfo("closed the previous window of %s (pid %d) before relaunching", sid, pid)
}

var sessionProcessNames = map[string]bool{
	"claude": true, "node": true, "node.exe": true, "codex": true, "agy": true, "droid": true,
	"copilot": true, "powershell": true, "powershell.exe": true, "pwsh": true, "pwsh.exe": true,
	"cmd.exe": true, "conhost.exe": true, "windowsterminal.exe": true, "wt.exe": true,
	"sh": true, "bash": true, "zsh": true, "dash": true, "claude.exe": true, "codex.exe": true,
}

func looksLikeSessionName(raw string) bool {
	name := filepath.Base(strings.ToLower(strings.TrimSpace(raw)))
	if name == "" {
		return false
	}
	if sessionProcessNames[name] {
		return true
	}

	if fields := strings.Fields(name); len(fields) > 0 && sessionProcessNames[filepath.Base(fields[0])] {
		return true
	}
	return false
}

func looksLikeSessionProcess(pid int) bool {
	name := strings.TrimSpace(processName(pid))
	if name == "" {

		logInfo("pid %d could not be identified; leaving it alone", pid)
		return false
	}
	return looksLikeSessionName(name)
}

func ownHelperProcess(name string) bool {
	base := strings.ToLower(strings.TrimSpace(name))
	if fields := strings.Fields(base); len(fields) > 0 {
		base = fields[0]
	}
	base = strings.TrimSuffix(filepath.Base(base), ".exe")
	if base == pluginName {
		return true
	}
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	return base == strings.TrimSuffix(strings.ToLower(filepath.Base(executable)), ".exe")
}

func terminateProcess(pid int) error {
	if isWindows {
		_, err := runWithTimeout(exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F"), 10*time.Second)
		return err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func terminalPreference(cfg object) string {
	return strings.TrimSpace(strings.ToLower(getString(section(cfg, "resume"), "terminal")))
}

func launchInWindowsTerminal(cfg object, launch launchSpec, claudePath string, claudeArgs []string, env []string, effort string) bool {
	ensureDir(files.launches)
	spec, started, pidFile := launchFiles(launch.sid)
	_ = os.Remove(started)
	_ = os.Remove(pidFile)
	quoted := []string{}
	for _, arg := range claudeArgs {
		quoted = append(quoted, windowsQuote(arg))
	}
	title := pluginName + " · " + filepath.Base(launch.cwd)
	mustWriteJSON(spec, object{"claude": claudePath, "cwd": launch.cwd, "configDir": files.configDir, "sessionId": launch.sid, "effort": effort, "arguments": strings.Join(quoted, " "), "title": title})
	defer func() {
		for _, file := range []string{spec, started, pidFile} {
			_ = os.Remove(file)
		}
	}()
	scriptArgs := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", files.launchScript, "-Spec", spec}
	preference := terminalPreference(cfg)
	if preference != "console" {
		if wt := locateExecutable("wt"); wt != "" {

			wtEscape := func(value string) string { return strings.ReplaceAll(value, ";", `\;`) }
			tab := exec.Command(wt, append([]string{"-w", "0", "new-tab", "-d", wtEscape(launch.cwd), "--title", wtEscape(title), "powershell.exe"}, append(scriptArgs, "-Attached")...)...)
			tab.Env = env
			if err := tab.Run(); err == nil && waitForFile(started, launchStartTimeout) {
				logInfo("session %s opened in a Windows Terminal tab", launch.sid)
				return waitForLaunchedSession(launch.sid, pidFile, "tab")
			}
			warn("Windows Terminal did not start the launcher; opening a console window instead")
		}
	}
	command := exec.Command("powershell.exe", scriptArgs...)
	command.Env = env
	if logFile, err := os.OpenFile(files.resumeLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		defer logFile.Close()
		command.Stdout, command.Stderr = logFile, logFile
	}
	if err := command.Start(); err != nil {
		fail("window launch failed: %v", err)
		return false
	}
	go func() { _ = command.Wait() }()
	return waitForLaunchedSession(launch.sid, pidFile, "window")
}

func waitForLaunchedSession(sid, pidFile, how string) bool {
	if !waitForFile(pidFile, launchPidTimeout) {
		warn("session %s: the launcher never reported a claude process; assuming it did not start", sid)
		return false
	}
	pid := readPidFile(pidFile)
	if pid <= 0 {
		return false
	}
	recordLaunch(sid, pid, how)
	defer clearLaunch(sid)
	waitForPid(pid)
	logInfo("%s session ended (pid %d)", how, pid)
	return true
}

func unixLaunchScript(launch launchSpec, claudePath string, claudeArgs []string, effort string) (script, pidFile string) {
	ensureDir(files.launches)
	spec, _, pidFile := launchFiles(launch.sid)
	script = strings.TrimSuffix(spec, ".json") + ".sh"
	_ = os.Remove(pidFile)
	lines := []string{
		"#!/bin/sh",
		"export CLAUDE_CONFIG_DIR=" + shellQuote(files.configDir),
		"export CLAUDE_CODE_EFFORT_LEVEL=" + shellQuote(effort),
		"export " + handoffEnv + "=" + shellQuote(launch.sid),
		"cd " + shellQuote(launch.cwd) + " || exit 1",
		"printf '%s' \"$$\" > " + shellQuote(pidFile),

		"rm -f " + shellQuote(script),
		"exec " + shellQuote(claudePath) + " " + shellJoin(claudeArgs),
	}
	if err := os.WriteFile(script, []byte(strings.Join(lines, "\n")+"\n"), 0o755); err != nil {
		fail("launcher script not written (%s): %v", script, err)
		return "", pidFile
	}
	return script, pidFile
}

func appleScriptEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellJoin(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, shellQuote(value))
	}
	return strings.Join(quoted, " ")
}

func launchInDesktopTerminal(cfg object, launch launchSpec, claudePath string, claudeArgs []string, effort string) bool {
	preference := terminalPreference(cfg)
	if preference == "none" || os.Getenv("NOCTIS_NO_TERMINAL") != "" {
		return false
	}
	script, pidFile := unixLaunchScript(launch, claudePath, claudeArgs, effort)
	if script == "" {
		return false
	}
	defer func() {
		_ = os.Remove(script)
		_ = os.Remove(pidFile)
	}()
	var opener *exec.Cmd
	switch {
	case preference != "" && preference != "auto" && strings.Contains(preference, "{script}"):
		opener = exec.Command("sh", "-c", strings.ReplaceAll(getString(section(cfg, "resume"), "terminal"), "{script}", shellQuote(script)))
	case isDarwin:
		opener = exec.Command("osascript", "-e", `tell application "Terminal" to do script "sh `+appleScriptEscape(script)+`"`, "-e", `tell application "Terminal" to activate`)
	case os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "":
		for _, candidate := range [][]string{{"x-terminal-emulator", "-e"}, {"gnome-terminal", "--"}, {"konsole", "-e"}, {"xfce4-terminal", "-e"}, {"kitty"}, {"alacritty", "-e"}, {"wezterm", "start", "--"}} {
			if locateExecutable(candidate[0]) != "" {
				opener = exec.Command(candidate[0], append(candidate[1:], "sh", script)...)
				break
			}
		}
	}
	if opener == nil {
		return false
	}
	if _, err := runWithTimeout(opener, 15*time.Second); err != nil {
		warn("terminal window could not be opened (%v); running headless", err)
		return false
	}
	if !waitForFile(pidFile, launchPidTimeout) {
		warn("terminal opened but the session did not start; running headless")
		return false
	}
	return waitForLaunchedSession(launch.sid, pidFile, "terminal")
}
