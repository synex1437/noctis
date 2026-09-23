package main

import (
	"fmt"
	"os"
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

func runHelp() {
	fmt.Println(T("help.usage", pluginName, pluginVersion))
	for _, name := range sortedKeys(ported) {
		if key := "help.cmd." + name; T(key) != key {
			fmt.Printf("  %-14s %s\n", name, T(key))
		}
	}
	fmt.Println(T("help.more"))
}

func main() {
	defer func() {
		if recovered := recover(); recovered != nil {
			fail("fatal: %v", recovered)

			if command == "statusline" {
				fmt.Println(T("statusline.fatal", pluginName))
			}
			os.Exit(0)
		}
	}()
	args = parseArgs(os.Args[1:])
	if helpAsked() {
		initPaths()
		setLocale(loadConfig())
		runHelp()
		return
	}
	initPaths()
	cfg := loadConfig()
	resolveHost(cfg)
	setLocale(cfg)
	setMode(cfg)
	command = dispatchCommand()
	run, ok := ported[command]
	if !ok {
		fmt.Fprintln(os.Stderr, T("unknownCommand", command, strings.Join(sortedKeys(ported), ", ")))
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

var startedByHost = map[string]bool{"hook": true, "statusline": true, "resume": true, "sleeper": true, "ensure": true, "release-check": true, "webhook": true, "selftest-mark": true, "state-write": true}

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
