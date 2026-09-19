package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	pluginName              = "noctis"
	handoffEnv              = "NOCTIS_HANDOFF"
	tailLineWindowBytes     = 64 * 1024
	logMaxBytes             = 512 * 1024
	transcriptTailBytes     = 256 * 1024
	sessionTTLSeconds       = 36 * 3600
	stateEntryTTLSeconds    = 3 * 86400
	checkpointTTLSeconds    = 7 * 86400
	routeTTLSeconds         = 3600
	heartbeatFreshSeconds   = 120
	responseBodyLimit       = 1024 * 1024
	lockWaitMs              = 3000
	lockStaleMs             = 15000
	stopFailureMaxAttempts  = 5
	burstHistoryLimit       = 6
	burstWindowSeconds      = 1800
	burstMinUsed            = 60.0
	burstSafety             = 1.2
	projectionMinStaleness  = 60.0
	projectionMaxStaleness  = 3600.0
	clockSkewMinSeconds     = 30
	clockSkewMaxSeconds     = 86400
	selfHealIntervalSeconds = 600
	selfCheckIntervalSec    = 86400
	culpritFloor            = 90.0
	warnBand                = 6.0
	warnBandMax             = 15.0
	nearEdgeBand            = 8.0
	nearEdgePollFast        = 30
	nearEdgePollNormal      = 60
	nearEdgePollSlow        = 120
	nearEdgePollClose       = 15
	blindAfterSeconds       = 60
	fetchTimeout            = 5 * time.Second
	pluginVersion           = "5.5.0"
	codingActivityWindow    = 45 * 60
	codingTailBytes         = 64 * 1024
	longTextSummaryChars    = 1200
	compactionContextPct    = 85.0
	compactionGuardGap      = 5.0
	compactionBand          = 6.0
	clearedSessionWindow    = 600
	handoffGraceSeconds     = 60
	waitStaleSeconds        = 2 * 86400
	learnedBlockMisroutes   = 2
	gitStatusLines          = 30
	hookPulseInterval       = 600
	maxResetHorizon         = 45 * 86400
	launchRecordTTLSeconds  = 8 * 86400
	hooksDeadAfterSeconds   = 1800
	hooksDeadMinStatuslines = 4
	statuslineRecentSeconds = 300
	interruptedSamples      = 3
	etaMinSpanSeconds       = 1800
	queueMaxBytes           = 1024 * 1024
	queueMaxItems           = 15
	defaultUsageURL         = "https://api.anthropic.com/api/oauth/usage"
	aliveHookMaxChecks      = 6
	fallbackClaudeVersion   = "2.1.267"
)

var (
	reservedKeys   = map[string]bool{"__proto__": true, "constructor": true, "prototype": true}
	fileTools      = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true}
	agentTools     = map[string]bool{"Agent": true, "Task": true}
	textExtensions = map[string]bool{".md": true, ".markdown": true, ".txt": true, ".rst": true, ".adoc": true, ".csv": true, ".tsv": true, ".srt": true, ".vtt": true}

	knownPermModes   = map[string]bool{"default": true, "acceptEdits": true, "plan": true, "auto": true}
	refusedPermModes = map[string]bool{"bypassPermissions": true, "dontAsk": true}
	localHosts       = map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true, "[::1]": true}
	safeNamePattern  = lazyRegexp(`[^A-Za-z0-9_.-]`)
	compactPrefixPat = lazyRegexp(`(?i)^this session is being continued`)
	testedClaudeMin  = "2.1.251"
	testedClaudeMax  = "2.1.999"
)

type object = map[string]any

type paths struct {
	pluginRoot     string
	notifyScript   string
	launchScript   string
	configDir      string
	guardDir       string
	config         string
	usage          string
	usageBackup    string
	fable          string
	state          string
	stateBackup    string
	stateLock      string
	usageLock      string
	fableLock      string
	scheduleLock   string
	settingsLock   string
	decisions      string
	hooks          string
	log            string
	errors         string
	resumeLog      string
	settings       string
	credentials    string
	checkpoints    string
	launches       string
	runnerLauncher string
}

var (
	args       parsedArgs
	command    string
	files      paths
	timeOffset int64
	isWindows  = os.PathSeparator == '\\'
	isDarwin   = runtime.GOOS == "darwin"
)

type parsedArgs struct {
	positional []string
	flags      map[string]string
	present    map[string]bool
}

func parseArgs(argv []string) parsedArgs {
	out := parsedArgs{flags: map[string]string{}, present: map[string]bool{}}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "--") {
			out.positional = append(out.positional, arg)
			continue
		}
		name := arg[2:]
		out.present[name] = true
		if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "--") {
			out.flags[name] = argv[i+1]
			i++
		}
	}
	return out
}

func flagString(name string) string {
	return args.flags[name]
}

func positional(index int) string {
	if index < len(args.positional) {
		return args.positional[index]
	}
	return ""
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "."
}

func looksLikePluginRoot(dir string) bool {
	return dir != "" && statSafe(filepath.Join(dir, "config.default.json")) != nil && statSafe(filepath.Join(dir, "hooks", "hooks.json")) != nil
}

func resolvePluginRoot(executable string) string {
	for _, candidate := range []string{os.Getenv("NOCTIS_PLUGIN_ROOT"), os.Getenv("CLAUDE_PLUGIN_ROOT")} {
		if candidate == "" {
			continue
		}

		resolved, _ := filepath.Abs(candidate)
		if looksLikePluginRoot(resolved) {
			return resolved
		}
		warn("%s does not look like a plugin root; ignored", candidate)
	}
	if executable == "" {
		return ""
	}
	dir := filepath.Dir(executable)
	for depth := 0; depth < 4; depth++ {
		if looksLikePluginRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(executable)))
}

func initPaths() {
	executable, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	pluginRoot := resolvePluginRoot(executable)
	configDir := flagString("account")
	if configDir == "" {
		if host := hostFromArgs(); host != "" && host != "claude" {
			configDir = hostHome(host)
		}
	}
	if configDir == "" {
		configDir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if configDir == "" {
		configDir = filepath.Join(homeDir(), ".claude")
	}
	configDir, _ = filepath.Abs(configDir)
	guardDir := filepath.Join(configDir, pluginName)
	files = paths{
		pluginRoot:     pluginRoot,
		notifyScript:   filepath.Join(pluginRoot, "scripts", "notify.ps1"),
		launchScript:   filepath.Join(pluginRoot, "scripts", "launch.ps1"),
		configDir:      configDir,
		guardDir:       guardDir,
		config:         filepath.Join(guardDir, "config.json"),
		usage:          filepath.Join(guardDir, "usage.json"),
		usageBackup:    filepath.Join(guardDir, "usage.json.bak"),
		fable:          filepath.Join(guardDir, "fable.json"),
		state:          filepath.Join(guardDir, "state.json"),
		stateBackup:    filepath.Join(guardDir, "state.json.bak"),
		stateLock:      filepath.Join(guardDir, "state.lock"),
		usageLock:      filepath.Join(guardDir, "usage.lock"),
		fableLock:      filepath.Join(guardDir, "fable.lock"),
		scheduleLock:   filepath.Join(guardDir, "schedule.lock"),
		settingsLock:   filepath.Join(guardDir, "settings.lock"),
		decisions:      filepath.Join(guardDir, "decisions.jsonl"),
		hooks:          filepath.Join(pluginRoot, "hooks", "hooks.json"),
		log:            filepath.Join(guardDir, "guard.log"),
		errors:         filepath.Join(guardDir, "errors.log"),
		resumeLog:      filepath.Join(guardDir, "resume-output.log"),
		settings:       filepath.Join(configDir, "settings.json"),
		credentials:    filepath.Join(configDir, ".credentials.json"),
		checkpoints:    filepath.Join(guardDir, "checkpoints"),
		launches:       filepath.Join(guardDir, "launches"),
		runnerLauncher: filepath.Join(guardDir, "runner.cmd"),
	}
	if offset, err := strconv.ParseInt(os.Getenv("NOCTIS_TIME_OFFSET"), 10, 64); err == nil {
		timeOffset = offset
	}
}

func nowSec() int64 {
	return time.Now().Unix() + timeOffset
}

func ensureDir(dir string) {
	_ = os.MkdirAll(dir, 0o700)
}

func appendRotating(file, line string) error {
	if info, err := os.Stat(file); err == nil && info.Size() > logMaxBytes {
		_ = os.Rename(file, file+".1")
	}
	handle, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer handle.Close()
	_, err = handle.WriteString(line)
	return err
}

func logLevel(level, message string) {
	ensureDir(files.guardDir)
	line := fmt.Sprintf("%s [%s %d %s] %s\n", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), strings.ToUpper(level), os.Getpid(), command, message)
	if err := appendRotating(files.log, line); err != nil {
		if command != "hook" && command != "statusline" {
			fmt.Fprintf(os.Stderr, "%s log failed: %v\n", pluginName, err)
		}
		return
	}
	if level != "info" {
		_ = appendRotating(files.errors, line)
	}
}

func logInfo(format string, values ...any) { logLevel("info", fmt.Sprintf(format, values...)) }
func warn(format string, values ...any)    { logLevel("warn", fmt.Sprintf(format, values...)) }
func fail(format string, values ...any)    { logLevel("error", fmt.Sprintf(format, values...)) }

type strictRead struct {
	exists bool
	ok     bool
	data   object
	err    string
}

func readJSONStrict(file string) strictRead {
	content, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return strictRead{exists: false, ok: true, data: object{}}
		}
		return strictRead{exists: true, ok: false, err: err.Error()}
	}
	var raw any
	if err := json.Unmarshal(content, &raw); err != nil {
		return strictRead{exists: true, ok: false, err: err.Error()}
	}
	data, _ := raw.(object)
	return strictRead{exists: true, ok: true, data: data}
}

func readJSON(file string) object {
	read := readJSONStrict(file)
	if !read.ok {
		warn("readJson %s: %s", filepath.Base(file), read.err)
		return nil
	}
	if !read.exists {
		return nil
	}
	return read.data
}

func marshalPretty(value any) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fail("json encode failed: %v", err)
		return nil
	}
	return bytes.TrimRight(buffer.Bytes(), "\n")
}

func marshalCompact(value any) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return bytes.TrimRight(buffer.Bytes(), "\n")
}

func writeJSONAtomic(file string, value any) error {
	ensureDir(filepath.Dir(file))
	encoded := marshalPretty(value)
	if encoded == nil {
		return errors.New("value cannot be encoded as JSON; file left unchanged")
	}
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return err
	}

	var err error
	for attempt := 0; attempt < 8; attempt++ {
		if err = os.Rename(tmp, file); err == nil {
			return nil
		}
		time.Sleep(time.Duration(10+attempt*10) * time.Millisecond)
	}
	if writeErr := os.WriteFile(file, encoded, 0o600); writeErr != nil {
		return writeErr
	}
	if removeErr := os.Remove(tmp); removeErr != nil {
		warn("temp file left behind: %s", tmp)
	}
	warn("atomic rename of %s failed after retries (%v); wrote in place", filepath.Base(file), err)
	return nil
}

var writeFailures = 0

func mustWriteJSON(file string, value any) {
	if err := writeJSONAtomic(file, value); err != nil {
		writeFailures++
		fail("write %s failed: %v", filepath.Base(file), err)
	}
}

var (
	stdinCache  object
	stdinLoaded bool
)

func stdinIsPipe() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

func peekStdinJSON() object {
	if !stdinLoaded {
		if !stdinIsPipe() {
			stdinLoaded, stdinCache = true, object{}
		} else {
			readStdinJSON()
		}
	}
	if len(stdinCache) == 0 {
		return nil
	}
	return stdinCache
}

func readStdinJSON() object {
	if stdinLoaded {
		return stdinCache
	}
	stdinLoaded = true
	stdinCache = object{}
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		warn("stdin read failed: %v", err)
		return stdinCache
	}
	if strings.TrimSpace(string(content)) == "" {
		return stdinCache
	}
	var raw any
	if err := json.Unmarshal(content, &raw); err != nil {
		warn("stdin parse failed: %v", err)
		return stdinCache
	}
	if data, ok := raw.(object); ok {
		stdinCache = data
	}
	return stdinCache
}

func getMap(source object, key string) object {
	if source == nil {
		return nil
	}
	value, _ := source[key].(object)
	return value
}

func getString(source object, key string) string {
	if source == nil {
		return ""
	}
	switch value := source[key].(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		if value {
			return "true"
		}
		return "false"
	}
	return ""
}

func getNumber(source object, key string) (float64, bool) {
	if source == nil {
		return 0, false
	}
	return toNumber(source[key])
}

func toNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

func numberOr(source object, key string, fallback float64) float64 {
	if value, ok := getNumber(source, key); ok {
		return value
	}
	return fallback
}

func getBool(source object, key string, fallback bool) bool {
	if source == nil {
		return fallback
	}
	if value, ok := source[key].(bool); ok {
		return value
	}
	return fallback
}

func getList(source object, key string) []any {
	if source == nil {
		return nil
	}
	value, _ := source[key].([]any)
	return value
}

func loadConfig() object {
	defaults := readJSON(filepath.Join(files.pluginRoot, "config.default.json"))
	if defaults == nil {
		defaults = object{}
	}
	userRead := readJSONStrict(files.config)
	user := object{}
	if userRead.ok && userRead.data != nil {
		user = userRead.data
	}
	merged := mergeDefaults(defaults, user)
	if !userRead.ok {
		merged["configError"] = userRead.err
	}
	return merged
}

func mergeDefaults(defaults, user object) object {
	merged := object{}
	for key, base := range defaults {
		override, hasOverride := user[key]
		baseMap, baseIsMap := base.(object)
		overrideMap, overrideIsMap := override.(object)
		switch {
		case baseIsMap && overrideIsMap:
			merged[key] = mergeDefaults(baseMap, overrideMap)
		case baseIsMap && !hasOverride:
			merged[key] = mergeDefaults(baseMap, object{})
		case hasOverride:
			merged[key] = override
		default:
			merged[key] = base
		}
	}
	for key, override := range user {
		if _, exists := merged[key]; !exists {
			merged[key] = override
		}
	}
	return merged
}

func section(cfg object, name string) object {
	if value := getMap(cfg, name); value != nil {
		return value
	}
	return object{}
}

func emptyState() object {
	return object{
		"waits":            object{},
		"handedOff":        object{},
		"modelOverrides":   object{},
		"checkpoints":      object{},
		"autoResume":       object{},
		"notified":         object{},
		"routes":           object{},
		"tasks":            object{},
		"modelSwitched":    nil,
		"disabledUntil":    float64(0),
		"lastHookAt":       float64(0),
		"interruptedWaits": []any{},
		"hookCapSeconds":   float64(0),
		"routerLearned":    object{},
		"stopGuard":        object{},
		"budgetDay":        object{},
		"webhook":          object{},
		"overload":         object{},
		"githubSeen":       object{},
		"sessionLocale":    object{},
		"workflows":        object{},
		"autoQueues":       object{},
		"resumePrompts":    object{},
		"launched":         object{},
		"queueTrust":       object{},
		"launchFailures":   object{},
	}
}

func usableStateJSON(content []byte) bool {
	var raw any
	if err := json.Unmarshal(content, &raw); err != nil {
		return false
	}
	_, isObject := raw.(map[string]any)
	return isObject
}

func stateProblem(result strictRead) string {
	if result.err != "" {
		return result.err
	}
	return "not a JSON object"
}

func readStoredState() object {
	primary := readJSONStrict(files.state)
	if primary.ok && !primary.exists {
		return object{}
	}
	if primary.ok && primary.data != nil {
		return primary.data
	}

	for attempt := 0; attempt < 4 && !(primary.ok && primary.data != nil); attempt++ {
		time.Sleep(25 * time.Millisecond)
		primary = readJSONStrict(files.state)
	}
	if primary.ok && primary.data != nil {
		return primary.data
	}

	backup := readJSONStrict(files.stateBackup)
	if backup.ok && backup.data != nil && backup.exists {

		if err := writeJSONAtomic(files.state, backup.data); err != nil {
			warn("state.json unusable (%s); recovered from backup but could not rewrite it: %v", stateProblem(primary), err)
		} else {
			warn("state.json unusable (%s); restored from the backup", stateProblem(primary))
		}
		return backup.data
	}
	if err := os.Remove(files.state); err == nil {
		fail("state.json unusable and no usable backup (%s); cleared, starting empty", stateProblem(primary))
	} else {
		fail("state.json unusable and no usable backup (%s); starting empty", stateProblem(primary))
	}
	return object{}
}

func readState() object {
	stored := readStoredState()
	state := emptyState()
	for key, template := range state {
		value, present := stored[key]
		if !present {
			continue
		}
		if _, templateIsMap := template.(object); templateIsMap {
			if valueMap, ok := value.(object); ok {
				state[key] = valueMap
			} else {
				state[key] = object{}
			}
			continue
		}
		state[key] = value
	}
	return state
}

func stateMap(state object, key string) object {
	value := getMap(state, key)
	if value == nil {
		value = object{}
		state[key] = value
	}
	return value
}

func pruneState(state object, now int64) {
	for sid, raw := range stateMap(state, "handedOff") {
		handoff, _ := raw.(object)
		pid, hasPid := getNumber(handoff, "pid")
		if handoff == nil || !hasPid || pid == 0 || float64(now)-numberOr(handoff, "at", 0) < handoffGraceSeconds || processAlive(int(pid)) {
			continue
		}
		delete(stateMap(state, "handedOff"), sid)
		warn("stale handoff for %s dropped (runner %d is gone)", sid, int(pid))
	}
	for sid, raw := range stateMap(state, "waits") {
		wait, _ := raw.(object)
		if wait == nil {
			delete(stateMap(state, "waits"), sid)
			continue
		}
		if waitLive(wait, now) {
			continue
		}

		if scheduled := getMap(wait, "scheduled"); scheduled != nil {
			prunedRunners = append(prunedRunners, object{"sid": sid, "scheduled": scheduled})
		}
		delete(stateMap(state, "waits"), sid)
		warn("stale wait for %s dropped (resume time passed %dh ago without a live runner)", sid, int((float64(now)-numberOr(wait, "resumeAt", 0))/3600+0.5))
	}
	for signal, raw := range stateMap(state, "routerLearned") {
		record, _ := raw.(object)
		if record == nil || float64(now)-numberOr(record, "at", 0) > 30*86400 {
			delete(stateMap(state, "routerLearned"), signal)
		}
	}

	for key, raw := range stateMap(state, "launched") {
		entry, _ := raw.(object)
		if entry == nil || float64(now)-numberOr(entry, "at", 0) > launchRecordTTLSeconds {
			delete(stateMap(state, "launched"), key)
		}
	}
	for sid, raw := range stateMap(state, "workflows") {
		newest := 0.0
		runs, _ := raw.([]any)
		for _, run := range runs {
			if at := numberOr(toObject(run), "at", 0); at > newest {
				newest = at
			}
		}
		if float64(now)-newest > stateEntryTTLSeconds && getMap(getMap(state, "waits"), sid) == nil {

			delete(stateMap(state, "workflows"), sid)
		}
	}
	for sid, raw := range stateMap(state, "autoQueues") {
		record := toObject(raw)
		if float64(now)-numberOr(record, "at", 0) > checkpointTTLSeconds {
			_ = os.Remove(getString(record, "path"))
			delete(stateMap(state, "autoQueues"), sid)
		}
	}
	for sid, raw := range stateMap(state, "resumePrompts") {
		if float64(now)-numberOr(toObject(raw), "at", 0) > 86400 {
			delete(stateMap(state, "resumePrompts"), sid)
		}
	}
	for key, raw := range stateMap(state, "githubSeen") {
		if float64(now)-numberOr(toObject(raw), "at", 0) > 30*86400 {
			delete(stateMap(state, "githubSeen"), key)
		}
	}
	for sid, raw := range stateMap(state, "sessionLocale") {
		if float64(now)-numberOr(toObject(raw), "at", 0) > 14*86400 {
			delete(stateMap(state, "sessionLocale"), sid)
		}
	}
	for sid, raw := range stateMap(state, "overload") {
		if float64(now)-numberOr(toObject(raw), "lastAt", 0) > 86400 {
			delete(stateMap(state, "overload"), sid)
		}
	}
	for key, raw := range stateMap(state, "notified") {
		at, _ := toNumber(raw)
		ttl := float64(stateEntryTTLSeconds)
		if strings.HasPrefix(key, "queue:") || strings.HasPrefix(key, "update:") || strings.HasPrefix(key, "restart:") {
			ttl = 30 * 86400
		}
		if float64(now)-at > ttl {
			delete(stateMap(state, "notified"), key)
		}
	}
	for key, raw := range stateMap(state, "checkpoints") {
		entry, _ := raw.(object)
		if entry != nil && float64(now)-numberOr(entry, "at", 0) <= checkpointTTLSeconds {
			continue
		}
		if entry != nil {
			if target := getString(entry, "path"); target != "" {
				if err := os.Remove(target); err != nil {
					logInfo("expired checkpoint already gone: %s", target)
				}
			}

			if cwd, ref := getString(entry, "cwd"), getString(entry, "snapshot"); cwd != "" && ref != "" {
				prunedSnapshots = append(prunedSnapshots, object{"cwd": cwd, "ref": ref})
			}
		}
		delete(stateMap(state, "checkpoints"), key)
	}

	for name, ttl := range map[string]float64{
		"routes":         routeTTLSeconds,
		"autoResume":     stateEntryTTLSeconds,
		"stopGuard":      stateEntryTTLSeconds,
		"tasks":          stateEntryTTLSeconds,
		"launchFailures": stateEntryTTLSeconds,
		"modelOverrides": launchRecordTTLSeconds,
	} {
		for key, raw := range stateMap(state, name) {
			entry, _ := raw.(object)
			if entry == nil {
				delete(stateMap(state, name), key)
				continue
			}

			newest := 0.0
			for _, field := range []string{"at", "updatedAt", "startedAt", "resumeAt", "lastAt", "since"} {
				if value, ok := getNumber(entry, field); ok && value > newest {
					newest = value
				}
			}
			if newest > 0 && float64(now)-newest > ttl {
				delete(stateMap(state, name), key)
			}
		}
	}

	if list := getList(state, "interruptedWaits"); len(list) > 200 {
		state["interruptedWaits"] = list[len(list)-200:]
	}
}

func withFileLock(lockFile string, work func()) bool {
	ensureDir(files.guardDir)
	deadline := time.Now().Add(lockWaitMs * time.Millisecond)
	var lock *os.File
	for lock == nil {
		handle, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = handle.WriteString(strconv.Itoa(os.Getpid()))
			lock = handle
			break
		}
		if !errors.Is(err, os.ErrExist) {
			fail("lock open failed: %v; %s not written", err, filepath.Base(lockFile))
			return false
		}
		if lockAbandoned(lockFile) {
			if err := os.Remove(lockFile); err == nil || errors.Is(err, os.ErrNotExist) {
				continue
			} else {
				fail("stale %s cannot be removed: %v; not written", filepath.Base(lockFile), err)
				return false
			}
		}
		if time.Now().After(deadline) {
			fail("%s is held by another process; not written", filepath.Base(lockFile))
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer func() {
		if lock != nil {
			lock.Close()
			if err := os.Remove(lockFile); err != nil {
				warn("lock release failed: %v", err)
			}
		}
	}()
	work()
	return true
}

var (
	prunedRunners   []object
	prunedSnapshots []object
)

func drainPrunedRunners() {
	pending := prunedRunners
	prunedRunners = nil
	for _, entry := range pending {
		sid, scheduled := getString(entry, "sid"), getMap(entry, "scheduled")
		cancelScheduled(sid, scheduled)
		logInfo("scheduler entry for dropped wait %s cancelled", sid)
	}
}

func updateState(mutator func(state object)) object {
	var result object
	withFileLock(files.stateLock, func() {
		state := readState()
		mutator(state)
		pruneState(state, nowSec())
		encoded := marshalPretty(state)
		if encoded == nil {
			fail("state.json could not be encoded; left unchanged")
			result = state
			return
		}
		before, readErr := os.ReadFile(files.state)
		if readErr == nil && bytes.Equal(before, encoded) {
			result = state
			return
		}
		if readErr == nil && usableStateJSON(before) {
			_ = os.WriteFile(files.stateBackup, before, 0o600)
		}
		mustWriteJSON(files.state, state)
		result = state
	})
	drainPrunedRunners()
	for _, entry := range prunedSnapshots {
		dropGitSnapshot(getString(entry, "cwd"), getString(entry, "ref"))
	}
	prunedSnapshots = nil
	return result
}

func withSettings(change func(object) bool) bool {
	changed := false
	held := withFileLock(files.settingsLock, func() {
		settings := readJSONStrict(files.settings)
		if !settings.ok {
			fail("settings.json unreadable, left untouched: %s", settings.err)
			return
		}
		data := settings.data
		if data == nil {
			data = object{}
		}
		if !change(data) {
			return
		}
		if err := writeJSONAtomic(files.settings, data); err != nil {
			fail("settings.json could not be written: %v", err)
			return
		}
		changed = true
	})
	return held && changed
}

func hashKey(text string) string {
	var hash uint32 = 5381
	for _, char := range []rune(text) {
		hash = (hash * 33) ^ uint32(char)
	}
	return fmt.Sprintf("%08x", hash)
}

func safeName(text string) string {
	value := safeNamePattern.ReplaceAllString(text, "_")
	if strings.Trim(value, ".") == "" {
		value = strings.Repeat("_", len(value))
	}
	if len(value) > 80 {
		value = value[:72] + "-" + hashKey(text)
	}
	return value
}

func sessionKey(input object) string {
	sid := getString(input, "session_id")
	if sid == "" {
		sid = "unknown"
	}
	key := safeName(sid)
	if reservedKeys[key] {
		return "unknown"
	}
	return key
}

func truncateText(text string, max int) string {
	value := strings.TrimSpace(text)
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return value
}

func formatTime(epoch float64) string {
	if epoch != epoch || epoch == 0 {
		return "?"
	}
	moment := time.Unix(int64(epoch), 0)
	today := time.Now()
	clock := moment.Format("15:04")
	if moment.Year() == today.Year() && moment.YearDay() == today.YearDay() {
		return clock
	}
	return fmt.Sprintf("%s %02d.%02d %s", dayName(int(moment.Weekday())), moment.Day(), int(moment.Month()), clock)
}

func localISO(epoch float64) string {
	return time.Unix(int64(epoch), 0).Format("2006-01-02T15:04:05")
}

func durationText(seconds float64) string {
	total := int64(seconds + 0.5)
	if total < 0 {
		total = 0
	}
	days := total / 86400
	hours := (total % 86400) / 3600
	minutes := (total % 3600) / 60
	parts := []string{}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d%s", days, durationUnit(0)))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d%s", hours, durationUnit(1)))
	}
	parts = append(parts, fmt.Sprintf("%d%s", minutes, durationUnit(2)))
	return strings.Join(parts, " ")
}

func forwardSlashes(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}

func statSafe(file string) os.FileInfo {
	info, err := os.Stat(file)
	if err != nil {
		return nil
	}
	return info
}

func jsonUnmarshal(content []byte, target any) error {
	return json.Unmarshal(content, target)
}

func jsonUnmarshalObject(content []byte, target *object) error {
	var raw any
	if err := json.Unmarshal(content, &raw); err != nil {
		return err
	}
	data, _ := raw.(object)
	*target = data
	return nil
}
