package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type commandFunc func()

var ported = map[string]commandFunc{
	"statusline":       runStatusline,
	"hook":             runHook,
	"resume":           runResume,
	"sleeper":          runSleeper,
	"status":           runStatus,
	"check":            runCheck,
	"queue":            runQueue,
	"cancel":           runCancel,
	"off":              runOff,
	"on":               runOn,
	"model":            runModel,
	"classify":         runClassify,
	"checkpoint":       runCheckpointCommand,
	"doctor":           runDoctor,
	"selftest":         runSelftest,
	"selftest-mark":    runSelftestMark,
	"state-write":      runStateWrite,
	"report":           runReport,
	"install":          runInstall,
	"setup":            runSetup,
	"ensure":           runEnsure,
	"why":              runWhy,
	"webhook":          runWebhookCommand,
	"schedule-preview": runSchedulePreview,
	"release-check":    runReleaseCheck,
	"version":          runVersion,
}

func runVersion() {
	fmt.Println(pluginVersion)
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func dispatchCommand() string {
	if name := positional(0); name != "" {
		return name
	}
	payload := peekStdinJSON()
	if payload == nil {
		return "status"
	}
	miswired = true
	if getString(payload, "hook_event_name") != "" {
		return "hook"
	}
	if getMap(payload, "rate_limits") != nil || getMap(payload, "context_window") != nil {
		return "statusline"
	}
	return "hook"
}

var plumbingCommands = map[string]bool{"hook": true, "statusline": true, "resume": true, "sleeper": true, "release-check": true, "selftest-mark": true, "state-write": true}

func userCommands() []string {
	names := []string{}
	for _, name := range sortedKeys(ported) {
		if !plumbingCommands[name] {
			names = append(names, name)
		}
	}
	return names
}

func helpKey(command string) string {
	return "help.cmd." + strings.ReplaceAll(command, "-", "")
}

func runHelp() {
	fmt.Println(T("help.usage", pluginName, pluginVersion))
	for _, name := range userCommands() {
		fmt.Printf("  %-17s %s\n", name, T(helpKey(name)))
	}
	fmt.Println(T("help.more"))
}

func offeredCommands() string {
	names := append(userCommands(), "help")
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func main() {
	defer func() {
		if recovered := recover(); recovered != nil {
			os.Exit(crashed(recovered))
		}
	}()
	args = parseArgs(os.Args[1:])
	initPaths()
	if files.configDir == "" {
		setLocale(nil)
		switch {
		case helpAsked():
			runHelp()
			return
		case versionAsked():
			runVersion()
			return
		case startedByHost[dispatchCommand()]:
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, T("home.missing", pluginName))
		os.Exit(1)
	}
	if helpAsked() {
		setLocale(loadConfig())
		runHelp()
		return
	}
	if versionAsked() {
		runVersion()
		return
	}
	cfg := loadConfig()
	resolveHost(cfg)
	setLocale(cfg)
	setMode(cfg)
	command = dispatchCommand()
	run, ok := ported[command]
	if !ok {
		fmt.Fprintln(os.Stderr, T("unknownCommand", command, offeredCommands()))
		os.Exit(1)
	}
	if host := unknownHost(); host != "" && !startedByHost[command] {
		fmt.Fprintln(os.Stderr, T("host.unknown", host))
		fmt.Fprintln(os.Stderr, T("host.choices"))
		fmt.Fprintln(os.Stderr, describeHostIDs())
		os.Exit(2)
	}
	run()
}

func crashed(recovered any) int {
	fail("fatal: %v", recovered)
	name := command
	if name == "" {
		name = positional(0)
	}
	if name == "statusline" {
		fmt.Println(T("statusline.fatal", pluginName))
	}
	if startedByHost[name] {
		return 0
	}
	fmt.Fprintln(os.Stderr, T("fatal.command", strings.TrimSpace(pluginName+" "+name), recovered, files.errors))
	if name == "" {
		return 0
	}
	return 1
}

var startedByHost = map[string]bool{"hook": true, "statusline": true, "resume": true, "sleeper": true, "ensure": true, "release-check": true, "webhook": true, "selftest-mark": true, "state-write": true}

func versionAsked() bool {
	switch positional(0) {
	case "":
		return args.present["version"]
	case "-v", "-V":
		return true
	}
	return false
}

func helpAsked() bool {
	if args.present["help"] || args.present["h"] || positional(0) == "help" {
		return true
	}
	for _, word := range args.positional {
		if word == "-h" || word == "-help" {
			return true
		}
	}
	return false
}
