package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func launchdLabel(sid string) string {
	return "com.synex.noctis." + hashKey(files.configDir+"|"+sid)
}

func launchdJobLabel(sid string, at float64) string {
	label := launchdLabel(sid) + "." + strconv.FormatInt(int64(at), 10)
	if ownLaunchdJob(label) {
		label += "." + strconv.Itoa(os.Getpid())
	}
	return label
}

func ownLaunchdJob(label string) bool {
	return label != "" && os.Getenv("XPC_SERVICE_NAME") == label
}

func launchdJobOf(sid, label string) bool {
	suffix, found := strings.CutPrefix(label, launchdLabel(sid))
	return found && strings.Trim(suffix, ".0123456789") == ""
}

func launchAgentsDir() string {
	return filepath.Join(homeDir(), "Library", "LaunchAgents")
}

func launchdPlist(label string) string {
	return filepath.Join(launchAgentsDir(), label+".plist")
}

func launchdJobsOf(sid string) []string {
	entries, err := os.ReadDir(launchAgentsDir())
	if err != nil {
		return nil
	}
	labels := []string{}
	for _, entry := range entries {
		label, isPlist := strings.CutSuffix(entry.Name(), ".plist")
		if isPlist && launchdJobOf(sid, label) {
			labels = append(labels, label)
		}
	}
	return labels
}

func systemdUnit(sid string) string {
	return "noctis-" + hashKey(files.configDir+"|"+sid)
}

func systemdJobUnit(sid string, at float64) string {
	unit := systemdUnit(sid) + "-" + strconv.FormatInt(int64(at), 10)
	if ownSystemdUnit(unit) {
		unit += "-" + strconv.Itoa(os.Getpid())
	}
	return unit
}

func ownSystemdUnit(unit string) bool {
	return unit != "" && os.Getenv(systemdUnitEnv) == unit
}

func systemdJobOf(sid, unit string) bool {
	suffix, found := strings.CutPrefix(unit, systemdUnit(sid))
	return found && strings.Trim(suffix, "-0123456789") == ""
}

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

var scheduleBackendOverride = ""

func schedulerBackend() string {
	if scheduleBackendOverride != "" {
		return scheduleBackendOverride
	}
	switch {
	case isWindows:
		return "task"
	case runtime.GOOS == "darwin":
		return "launchd"
	case commandAvailable("systemd-run") && systemdUserActive():
		return "systemd"
	}
	return "sleeper"
}

func systemdUserActive() bool {
	output, err := runScheduler(exec.Command("systemctl", "--user", "is-system-running"), 5*time.Second)
	state := strings.TrimSpace(string(output))
	return err == nil || state == "running" || state == "degraded"
}

func xmlEscape(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(text)
}

func currentUID() string {
	if account, err := user.Current(); err == nil {
		return account.Uid
	}
	return fmt.Sprint(os.Getuid())
}

var runScheduler = func(command *exec.Cmd, timeout time.Duration) ([]byte, error) {
	return runWithTimeout(command, timeout)
}

func scheduleAtMinute(at float64) time.Time {
	moment := time.Unix(int64(at), 0).Local()
	if moment.Second() > 0 || moment.Nanosecond() > 0 {
		moment = moment.Add(time.Minute).Truncate(time.Minute)
	}
	return moment
}

func launchdPlistBody(label, executable string, commandArgs []string, at float64, workingDir string) string {
	moment := scheduleAtMinute(at)
	arguments := []string{"<string>" + xmlEscape(executable) + "</string>"}
	for _, arg := range commandArgs {
		arguments = append(arguments, "<string>"+xmlEscape(arg)+"</string>")
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array>%s</array>
<key>StartCalendarInterval</key><dict><key>Month</key><integer>%d</integer><key>Day</key><integer>%d</integer><key>Hour</key><integer>%d</integer><key>Minute</key><integer>%d</integer></dict>
<key>RunAtLoad</key><false/>
<key>WorkingDirectory</key><string>%s</string>
</dict></plist>
`, xmlEscape(label), strings.Join(arguments, ""), int(moment.Month()), moment.Day(), moment.Hour(), moment.Minute(), xmlEscape(workingDir))
}

func scheduleLaunchd(sid string, at float64, commandArgs []string) (object, bool) {
	executable, err := os.Executable()
	if err != nil || homeDir() == "" {
		return nil, false
	}
	cancelLaunchdJobs(sid)
	bootOutStrandedLaunchdJobs(sid)
	label := launchdJobLabel(sid, at)
	plist := launchdPlistBody(label, executable, commandArgs, at, files.guardDir)
	path := launchdPlist(label)
	ensureDir(filepath.Dir(path))
	if err := os.WriteFile(path, []byte(plist), 0o600); err != nil {
		warn("launchd plist write failed: %v", err)
		return nil, false
	}
	domain := "gui/" + currentUID()
	if _, err := runScheduler(exec.Command("launchctl", "bootstrap", domain, path), 15*time.Second); err != nil {
		if _, legacyErr := runScheduler(exec.Command("launchctl", "load", "-w", path), 15*time.Second); legacyErr != nil {
			warn("launchd registration failed: %v", legacyErr)
			_ = os.Remove(path)
			return nil, false
		}
	}
	return object{"method": "launchd", "label": label, "at": at}, true
}

func cancelLaunchd(label string) {
	path := launchdPlist(label)
	if ownLaunchdJob(label) {
		if os.Remove(path) == nil {
			logInfo("launchd job %s is this process: plist removed, job left to finish", label)
		}
		return
	}
	if statSafe(path) == nil {
		return
	}
	domain := "gui/" + currentUID()
	if _, err := runScheduler(exec.Command("launchctl", "bootout", domain+"/"+label), 15*time.Second); err != nil {
		_, _ = runScheduler(exec.Command("launchctl", "unload", path), 15*time.Second)
	}

	_ = os.Remove(path)
}

func cancelLaunchdJobs(sid string) {
	for _, label := range launchdJobsOf(sid) {
		cancelLaunchd(label)
	}
}

func bootOutLaunchdJob(label string) {
	if _, err := runScheduler(exec.Command("launchctl", "bootout", "gui/"+currentUID()+"/"+label), 15*time.Second); err != nil {
		_, _ = runScheduler(exec.Command("launchctl", "remove", label), 15*time.Second)
	}
}

func bootOutStrandedLaunchdJobs(sid string) {
	listing, err := runScheduler(exec.Command("launchctl", "list"), 15*time.Second)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(listing), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "-" || ownLaunchdJob(fields[2]) || !launchdJobOf(sid, fields[2]) || statSafe(launchdPlist(fields[2])) != nil {
			continue
		}
		logInfo("launchd job %s is still loaded without a plist after it ran; booting it out", fields[2])
		bootOutLaunchdJob(fields[2])
	}
}

func bootOutFinishedLaunchdJob(sid string) {
	label := os.Getenv("XPC_SERVICE_NAME")
	if !launchdJobOf(sid, label) {
		return
	}
	_ = os.Remove(launchdPlist(label))
	logInfo("launchd job %s has done its resume; booting it out so it cannot fire again", label)
	bootOutLaunchdJob(label)
}

func systemdRunArgs(unit, executable string, commandArgs []string, at float64, wake bool) []string {
	moment := time.Unix(int64(at), 0).Local()
	arguments := []string{
		"--user", "--quiet", "--collect",
		"--unit=" + unit,
		"--setenv=" + systemdUnitEnv + "=" + unit,
		"--property=KillMode=process",
		"--on-calendar=" + moment.Format("2006-01-02 15:04:05"),
		"--timer-property=AccuracySec=1s",
	}
	if wake {
		arguments = append(arguments, "--timer-property=WakeSystem=true")
	}
	return append(append(arguments, executable), commandArgs...)
}

func scheduleSystemd(sid string, at float64, commandArgs []string, wake bool) (object, bool) {
	executable, err := os.Executable()
	if err != nil {
		return nil, false
	}
	cancelSystemd(systemdUnit(sid) + "*")
	unit := systemdJobUnit(sid, at)
	_, err = runScheduler(exec.Command("systemd-run", systemdRunArgs(unit, executable, commandArgs, at, wake)...), 15*time.Second)
	if err != nil && wake {

		logInfo("systemd wake alarm refused, scheduling without it: %v", err)
		_, err = runScheduler(exec.Command("systemd-run", systemdRunArgs(unit, executable, commandArgs, at, false)...), 15*time.Second)
	}
	if err != nil {
		warn("systemd-run scheduling failed: %v", err)
		return nil, false
	}
	return object{"method": "systemd", "unit": unit, "at": at}, true
}

func cancelSystemd(unit string) {
	_, _ = runScheduler(exec.Command("systemctl", "--user", "stop", unit+".timer"), 10*time.Second)
}

func scheduleNative(backend, sid string, at float64, commandArgs []string, wake bool) (object, bool) {
	switch backend {
	case "launchd":
		return scheduleLaunchd(sid, at, commandArgs)
	case "systemd":
		return scheduleSystemd(sid, at, commandArgs, wake)
	}
	return nil, false
}

func cancelNative(sid string, scheduled object) {
	switch getString(scheduled, "method") {
	case "launchd":
		label := getString(scheduled, "label")
		if !launchdJobOf(sid, label) {
			label = launchdLabel(sid)
		}
		cancelLaunchd(label)
	case "systemd":
		unit := getString(scheduled, "unit")
		if !systemdJobOf(sid, unit) {
			unit = systemdUnit(sid)
		}
		cancelSystemd(unit)
	}
}

func runSchedulePreview() {
	sid := orDefault(flagString("sid"), "preview")
	at := numberOr(object{"at": flagString("at")}, "at", 0)
	if parsed, ok := toNumber(flagString("at")); ok && parsed > 0 {
		at = parsed
	}
	if at <= 0 {
		at = float64(nowSec() + 3600)
	}
	backend := orDefault(flagString("backend"), schedulerBackend())
	executable, err := os.Executable()
	if err != nil {
		fail("schedule-preview: %v", err)
		return
	}
	commandArgs := runnerArgs("resume", sid, files.configDir)
	wake := getBool(section(loadConfig(), "alarm"), "wakePc", true)

	switch backend {
	case "launchd":
		fmt.Print(launchdPlistBody(launchdJobLabel(sid, at), executable, commandArgs, at, files.guardDir))
	case "systemd":
		fmt.Println(strings.Join(append([]string{"systemd-run"},
			systemdRunArgs(systemdJobUnit(sid, at), executable, commandArgs, at, wake)...), " "))
	case "task":
		fmt.Println(windowsTaskScript(taskName(sid), at, runnerArgs("resume", sid, `"`+files.configDir+`"`), wake))
	default:
		fmt.Println(strings.Join(append([]string{executable},
			runnerArgs("sleeper", sid, files.configDir, "--at", formatNumber(at), "--watch")...), " "))
	}
}
