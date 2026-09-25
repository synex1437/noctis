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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	pluginName              = "noctis"
	handoffEnv              = "NOCTIS_HANDOFF"
	claudeConfigEnv         = "CLAUDE_CONFIG_DIR"
	systemdUnitEnv          = "NOCTIS_UNIT"
	queueContinuesPrefix    = "[noctis] Queue continues"
	tailLineWindowBytes     = 64 * 1024
	logMaxBytes             = 512 * 1024
	transcriptTailBytes     = 256 * 1024
	sessionTTLSeconds       = 36 * 3600
	stateEntryTTLSeconds    = 3 * 86400
	checkpointTTLSeconds    = 7 * 86400
	routeTTLSeconds         = 3600
	heartbeatFreshSeconds   = 120
	responseBodyLimit       = 1024 * 1024
	lockWaitForegroundMs    = 10000
	lockStaleMs             = 15000
	lockLiveHolderMs        = 120000
	lockDeadOwnerMs         = 2000
	schedulingGraceSeconds  = 30
	lockQueueBackgroundMs   = 900000
	stopFailureMaxAttempts  = 5
	failureEpisodeSlack     = 3600
	burstHistoryLimit       = 6
	burstWindowSeconds      = 1800
	burstMinUsed            = 60.0
	burstSafety             = 1.2
	projectionMinStaleness  = 60.0
	projectionMaxStaleness  = 3600.0
	clockSkewMinSeconds     = 30
	gitStatusCacheTTL       = 2 * time.Second
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
	appServerExitGrace      = time.Second
	pluginVersion           = "7.1.0"
	codingActivityWindow    = 45 * 60
	codingTailBytes         = 64 * 1024
	longTextSummaryChars    = 1200
	compactionContextPct    = 85.0
	compactionGuardGap      = 5.0
	compactionBand          = 6.0
	creditCeilingDefault    = 100.0
	fanOutHeadroomDefault   = 25.0
	clearedSessionWindow    = 600
	handoffGraceSeconds     = 60
	waitStaleSeconds        = 2 * 86400
	learnedBlockMisroutes   = 2
	gitStatusLines          = 30
	treeStatLimit           = 2000
	sameWindowSeconds       = 120
	workflowScriptMaxBytes  = 1024 * 1024
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
	queueUnmatchedKept      = 50
	queueUnmatchedNamed     = 5
	queueReferenceChars     = 40
	openTaskLimit           = 50
	defaultUsageURL         = "https://api.anthropic.com/api/oauth/usage"
	aliveHookMaxChecks      = 6
	fallbackClaudeVersion   = "2.1.267"
	queueTrustTTLSeconds    = 30 * 86400
)

var (
	reservedKeys   = map[string]bool{"__proto__": true, "constructor": true, "prototype": true}
	fileTools      = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true}
	agentTools     = map[string]bool{"Agent": true, "Task": true}
	textExtensions = map[string]bool{".md": true, ".markdown": true, ".txt": true, ".rst": true, ".adoc": true, ".csv": true, ".tsv": true, ".srt": true, ".vtt": true}

	knownPermModes   = map[string]bool{"default": true, "manual": true, "acceptEdits": true, "plan": true, "auto": true}
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
	release        string
	state          string
	stateBackup    string
	stateLock      string
	usageLock      string
	fableLock      string
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
	values     map[string][]string
	present    map[string]bool
}

var switchFlags = map[string]bool{"help": true, "h": true, "json": true, "skip-task": true, "watch": true, "uninstall": true, "no-model": true, "no-ask": true, "no-lean": true}

func parseArgs(argv []string) parsedArgs {
	out := parsedArgs{flags: map[string]string{}, values: map[string][]string{}, present: map[string]bool{}}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "--") {
			out.positional = append(out.positional, arg)
			continue
		}
		name, value, given := strings.Cut(arg[2:], "=")
		out.present[name] = true
		if !given && !switchFlags[name] && i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "--") {
			value, given = argv[i+1], true
			i++
		}
		if given {
			out.flags[name] = value
		}
		if given || !switchFlags[name] {
			out.values[name] = append(out.values[name], value)
		}
	}
	return out
}

func flagString(name string) string {
	return args.flags[name]
}

func firstFlagValue(name string) string {
	for _, value := range args.values[name] {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
	return ""
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

func scriptsTrusted(root, executable string) bool {
	if !looksLikePluginRoot(root) {
		return false
	}
	for _, candidate := range []string{os.Getenv("NOCTIS_PLUGIN_ROOT"), os.Getenv("CLAUDE_PLUGIN_ROOT")} {
		if resolved, err := filepath.Abs(candidate); candidate != "" && err == nil && resolved == root {
			return true
		}
	}
	folder := filepath.Dir(executable)
	return folder == filepath.Join(root, "bin") || folder == filepath.Dir(platformBinary(root))
}

func initPaths() {
	executable, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	pluginRoot := resolvePluginRoot(executable)
	configDir := expandHome(flagString("account"))
	if configDir == "" {
		configDir = expandHome(firstFlagValue("config-dir"))
	}
	if configDir == "" {
		if host := hostFromArgs(); host != "" && host != "claude" {
			configDir = hostHome(host)
		}
	}
	if configDir == "" {
		configDir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if home := homeDir(); configDir == "" && home != "" {
		configDir = filepath.Join(home, ".claude")
	}
	if configDir != "" {
		configDir, _ = filepath.Abs(configDir)
	}
	guardDir := filepath.Join(configDir, pluginName)
	notifyScript, launchScript := "", ""
	if scriptsTrusted(pluginRoot, executable) {
		notifyScript, launchScript = filepath.Join(pluginRoot, "scripts", "notify.ps1"), filepath.Join(pluginRoot, "scripts", "launch.ps1")
	}
	files = paths{
		pluginRoot:     pluginRoot,
		notifyScript:   notifyScript,
		launchScript:   launchScript,
		configDir:      configDir,
		guardDir:       guardDir,
		config:         filepath.Join(guardDir, "config.json"),
		usage:          filepath.Join(guardDir, "usage.json"),
		usageBackup:    filepath.Join(guardDir, "usage.json.bak"),
		fable:          filepath.Join(guardDir, "fable.json"),
		release:        filepath.Join(guardDir, "release.json"),
		state:          filepath.Join(guardDir, "state.json"),
		stateBackup:    filepath.Join(guardDir, "state.json.bak"),
		stateLock:      filepath.Join(guardDir, "state.lock"),
		usageLock:      filepath.Join(guardDir, "usage.lock"),
		fableLock:      filepath.Join(guardDir, "fable.lock"),
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
		_ = renameAtomic(file, file+".1")
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
	exists   bool
	ok       bool
	unopened bool
	data     object
	raw      []byte
	err      string
}

type parsedFile struct {
	raw  []byte
	data object
}

const parseCacheLimit = 12

var (
	parseCache     = map[string]parsedFile{}
	parseCacheLock sync.Mutex
)

func copyValue(value any) any {
	switch typed := value.(type) {
	case object:
		clone := make(object, len(typed))
		for key, item := range typed {
			clone[key] = copyValue(item)
		}
		return clone
	case []any:
		clone := make([]any, len(typed))
		for index, item := range typed {
			clone[index] = copyValue(item)
		}
		return clone
	}
	return value
}

func copyObject(source object) object {
	if source == nil {
		return nil
	}
	clone, _ := copyValue(source).(object)
	return clone
}

var utf8BOM = []byte("\xef\xbb\xbf")

func readJSONStrict(file string) strictRead {
	content, err := readFileShared(file)
	for attempt := 0; err != nil && !errors.Is(err, os.ErrNotExist) && attempt < 8; attempt++ {
		time.Sleep(time.Duration(5+attempt*10) * time.Millisecond)
		content, err = readFileShared(file)
	}
	if err != nil {
		dropParsed(file)
		if errors.Is(err, os.ErrNotExist) {
			return strictRead{exists: false, ok: true, data: object{}}
		}
		return strictRead{exists: true, ok: false, unopened: true, err: err.Error()}
	}
	if cached, seen := parsedEntry(file); seen && bytes.Equal(cached.raw, content) {
		return strictRead{exists: true, ok: true, data: copyObject(cached.data), raw: content}
	}
	var raw any
	if err := json.Unmarshal(bytes.TrimPrefix(content, utf8BOM), &raw); err != nil {
		dropParsed(file)
		return strictRead{exists: true, ok: false, err: err.Error()}
	}
	data, _ := raw.(object)
	keepParsed(file, parsedFile{raw: content, data: copyObject(data)})
	return strictRead{exists: true, ok: true, data: data, raw: content}
}

func parsedEntry(file string) (parsedFile, bool) {
	parseCacheLock.Lock()
	defer parseCacheLock.Unlock()
	cached, seen := parseCache[file]
	return cached, seen
}

func keepParsed(file string, parsed parsedFile) {
	parseCacheLock.Lock()
	defer parseCacheLock.Unlock()
	if len(parseCache) >= parseCacheLimit {
		parseCache = map[string]parsedFile{}
	}
	parseCache[file] = parsed
}

func dropParsed(file string) {
	parseCacheLock.Lock()
	defer parseCacheLock.Unlock()
	delete(parseCache, file)
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

type keyOrder struct {
	keys     []string
	children map[string]*keyOrder
	items    []*keyOrder
}

func (order *keyOrder) child(key string) *keyOrder {
	if order == nil {
		return nil
	}
	return order.children[key]
}

func (order *keyOrder) item(index int) *keyOrder {
	if order == nil || index >= len(order.items) {
		return nil
	}
	return order.items[index]
}

func readKeyOrder(decoder *json.Decoder) (*keyOrder, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, nested := token.(json.Delim)
	if !nested {
		return nil, nil
	}
	order := &keyOrder{children: map[string]*keyOrder{}}
	for decoder.More() {
		if delim == '[' {
			item, err := readKeyOrder(decoder)
			if err != nil {
				return nil, err
			}
			order.items = append(order.items, item)
			continue
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, _ := token.(string)
		child, err := readKeyOrder(decoder)
		if err != nil {
			return nil, err
		}
		if _, seen := order.children[key]; !seen {
			order.keys = append(order.keys, key)
		}
		order.children[key] = child
	}
	_, err = decoder.Token()
	return order, err
}

func appendInOrder(out []byte, value any, order *keyOrder) []byte {
	switch typed := value.(type) {
	case object:
		keys, added, placed := []string{}, []string{}, map[string]bool{}
		if order != nil {
			for _, key := range order.keys {
				if _, present := typed[key]; present {
					keys = append(keys, key)
					placed[key] = true
				}
			}
		}
		for key := range typed {
			if !placed[key] {
				added = append(added, key)
			}
		}
		sort.Strings(added)
		out = append(out, '{')
		for index, key := range append(keys, added...) {
			if index > 0 {
				out = append(out, ',')
			}
			out = append(append(out, marshalCompact(key)...), ':')
			out = appendInOrder(out, typed[key], order.child(key))
		}
		return append(out, '}')
	case []any:
		out = append(out, '[')
		for index, item := range typed {
			if index > 0 {
				out = append(out, ',')
			}
			out = appendInOrder(out, item, order.item(index))
		}
		return append(out, ']')
	}
	return append(out, marshalCompact(value)...)
}

func marshalPrettyAsBefore(value any, previous []byte) []byte {
	order, err := readKeyOrder(json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(previous, utf8BOM))))
	var buffer bytes.Buffer
	if err != nil || json.Indent(&buffer, appendInOrder(nil, value, order), "", "  ") != nil {
		return marshalPretty(value)
	}
	if bytes.HasSuffix(previous, []byte("\n")) {
		buffer.WriteByte('\n')
	}
	return buffer.Bytes()
}

func linkTarget(file string) string {
	target := file
	for hops := 0; hops < 40; hops++ {
		link, err := os.Readlink(target)
		if err != nil {
			return target
		}
		if !filepath.IsAbs(link) {
			dir := filepath.Dir(target)
			if resolved, err := filepath.EvalSymlinks(dir); err == nil {
				dir = resolved
			}
			link = filepath.Join(dir, link)
		}
		target = link
	}
	return file
}

func writeJSONAtomic(file string, value any) error {
	return writeEncodedAtomic(file, marshalPretty(value))
}

func writeJSONKeepingOrder(file string, value any) error {
	previous, _ := readFileShared(linkTarget(file))
	return writeEncodedAtomic(file, marshalPrettyAsBefore(value, previous))
}

func writeEncodedAtomic(file string, encoded []byte) error {
	file = linkTarget(file)
	ensureDir(filepath.Dir(file))
	if encoded == nil {
		return errors.New("value cannot be encoded as JSON; file left unchanged")
	}
	mode := os.FileMode(0o600)
	info, statErr := os.Stat(file)
	if statErr == nil {
		mode = info.Mode().Perm()
	}
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, encoded, mode); err != nil {
		return err
	}
	if statErr == nil && !isWindows {
		_ = os.Chmod(tmp, mode)
	}

	var err error
	for attempt := 0; attempt < 8; attempt++ {
		if err = renameAtomic(tmp, file); err == nil {
			return nil
		}
		time.Sleep(time.Duration(10+attempt*10) * time.Millisecond)
	}
	if writeErr := os.WriteFile(file, encoded, mode); writeErr != nil {
		if removeErr := os.Remove(tmp); removeErr != nil {
			warn("temp file left behind: %s", tmp)
		}
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
	repairThresholds(merged, defaults)
	repairCompaction(merged, defaults)
	return merged
}

var builtinThresholds = map[string]float64{"session5h": 92, "weeklyAll": 89, "weeklyFable": 95}

func thresholdSwitchedOff(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case bool:
		return !typed
	}
	number, ok := toNumber(value)
	return ok && number == 0
}

func repairThresholds(merged, defaults object) {
	shipped := getMap(defaults, "thresholds")
	repaired := []string{}
	if _, present := merged["thresholds"]; present && getMap(merged, "thresholds") == nil && shipped != nil {
		merged["thresholds"] = mergeDefaults(shipped, object{})
		repaired = append(repaired, "thresholds")
	}
	thresholds := getMap(merged, "thresholds")
	if thresholds == nil {
		return
	}
	for _, key := range []string{"session5h", "weeklyAll", "weeklyFable", "weeklyScoped"} {
		value, present := thresholds[key]
		if !present || validThreshold(value) || thresholdSwitchedOff(value) {
			continue
		}
		switch fallback := shipped[key]; {
		case validThreshold(fallback):
			thresholds[key] = fallback
		case builtinThresholds[key] > 0:
			thresholds[key] = builtinThresholds[key]
		default:
			delete(thresholds, key)
		}
		repaired = append(repaired, key)
	}
	if len(repaired) > 0 {
		merged["thresholdsRepaired"] = strings.Join(repaired, ", ")
	}
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
		"failureRetries":   object{},
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

func readStoredState() (object, []byte) {
	primary := readJSONStrict(files.state)
	if primary.ok && !primary.exists {
		return object{}, nil
	}
	if primary.ok && primary.data != nil {
		return primary.data, primary.raw
	}

	for attempt := 0; attempt < 4 && !(primary.ok && primary.data != nil); attempt++ {
		time.Sleep(25 * time.Millisecond)
		primary = readJSONStrict(files.state)
	}
	if primary.ok && primary.data != nil {
		return primary.data, primary.raw
	}

	backup := readJSONStrict(files.stateBackup)
	if !primary.unopened && backup.ok && backup.data != nil && backup.exists {

		if err := writeJSONAtomic(files.state, backup.data); err != nil {
			warn("state.json unusable (%s); recovered from backup but could not rewrite it: %v", stateProblem(primary), err)
		} else {
			warn("state.json unusable (%s); restored from the backup", stateProblem(primary))
		}
		return backup.data, nil
	}
	if primary.unopened {
		fail("state.json could not be opened (%s); leaving it alone", stateProblem(primary))
		return nil, nil
	}
	if err := os.Remove(files.state); err == nil {
		fail("state.json unusable and no usable backup (%s); cleared, starting empty", stateProblem(primary))
	} else {
		fail("state.json unusable and no usable backup (%s); starting empty", stateProblem(primary))
	}
	return object{}, nil
}

func readState() object {
	state, _ := readStateWithBytes()
	if state == nil {
		return emptyState()
	}
	return state
}

func readStateWithBytes() (object, []byte) {
	stored, raw := readStoredState()
	if stored == nil {
		return nil, nil
	}
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
	return state, raw
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
			notePruned(&prunedRunners, object{"sid": sid, "scheduled": scheduled})
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
		if getMap(getMap(state, "waits"), sid) != nil {
			continue
		}
		record := toObject(raw)
		touched := numberOr(record, "at", 0)
		if info := statSafe(getString(record, "path")); info != nil && float64(info.ModTime().Unix()) > touched {
			touched = float64(info.ModTime().Unix())
		}
		if float64(now)-touched > checkpointTTLSeconds {
			if isAutoQueue(getString(record, "path")) {
				_ = os.Remove(getString(record, "path"))
			}
			delete(stateMap(state, "autoQueues"), sid)
		}
	}
	for sid, raw := range stateMap(state, "resumePrompts") {
		if float64(now)-numberOr(toObject(raw), "at", 0) > 86400 {
			delete(stateMap(state, "resumePrompts"), sid)
		}
	}
	for key, raw := range stateMap(state, "queueTrust") {
		record := toObject(raw)
		if last := max(numberOr(record, "at", 0), numberOr(record, "used", 0)); float64(now)-last > queueTrustTTLSeconds {
			delete(stateMap(state, "queueTrust"), key)
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
		if strings.HasPrefix(key, "queue:") || strings.HasPrefix(key, "update:") || strings.HasPrefix(key, "restart:") || key == "signInExpired" {
			ttl = 30 * 86400
		}
		if float64(now)-at > ttl {
			delete(stateMap(state, "notified"), key)
		}
	}
	for sid, raw := range stateMap(state, "tasks") {
		items := getMap(toObject(raw), "items")
		dropFinishedTasks(items)
		if len(items) == 0 {
			delete(stateMap(state, "tasks"), sid)
		}
	}
	for key, raw := range stateMap(state, "checkpoints") {
		entry, _ := raw.(object)
		if entry != nil && float64(now)-numberOr(entry, "at", 0) <= checkpointTTLSeconds {
			continue
		}
		if entry != nil {
			if target := getString(entry, "path"); target != "" && target == filepath.Join(files.checkpoints, filepath.Base(target)) {
				if err := os.Remove(target); err != nil {
					logInfo("expired checkpoint already gone: %s", target)
				}
			}

			if cwd, ref := getString(entry, "cwd"), getString(entry, "snapshot"); cwd != "" && ref != "" {
				notePruned(&prunedSnapshots, object{"cwd": cwd, "ref": ref})
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
		"failureRetries": stateEntryTTLSeconds,
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
			if newest == 0 {
				entry["at"] = float64(now)
				continue
			}
			if float64(now)-newest > ttl {
				delete(stateMap(state, name), key)
			}
		}
	}

	if list := getList(state, "interruptedWaits"); len(list) > 200 {
		state["interruptedWaits"] = list[len(list)-200:]
	}
}

func lockQueueMs() time.Duration {
	switch command {
	case "sleeper", "resume":
		return lockQueueBackgroundMs
	default:
		return lockLiveHolderMs
	}
}

func lockPollDelay(waited time.Duration) time.Duration {
	step := time.Duration(15+os.Getpid()%21) * time.Millisecond
	switch {
	case waited > 10*time.Second:
		return step * 8
	case waited > 2*time.Second:
		return step * 3
	default:
		return step
	}
}

func lockNoProgressMs() time.Duration {
	switch command {
	case "sleeper", "resume":
		return lockLiveHolderMs
	default:
		return lockWaitForegroundMs
	}
}

func withFileLock(lockFile string, work func()) bool {
	ensureDir(files.guardDir)
	begin := time.Now()
	noProgress := lockNoProgressMs() * time.Millisecond
	deadline := time.Now().Add(noProgress)
	queueDeadline := time.Now().Add(lockQueueMs() * time.Millisecond)
	holder := ""
	var lock *os.File
	for lock == nil {
		handle, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			holdLock(handle)
			_, _ = handle.WriteString(strconv.Itoa(os.Getpid()))
			lock = handle
			break
		}
		if !errors.Is(err, os.ErrExist) && !(isWindows && errors.Is(err, os.ErrPermission)) {
			fail("lock open failed: %v; %s not written", err, filepath.Base(lockFile))
			return false
		}
		owner, age, present := lockHolder(lockFile)
		if present && holderStale(owner, age) {
			removeErr := removeStaleLock(lockFile)
			if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				continue
			}
			if !isWindows && !errors.Is(removeErr, errLockLive) {
				fail("stale %s cannot be removed: %v; not written", filepath.Base(lockFile), removeErr)
				return false
			}
		}
		if owner != holder {
			holder = owner
			deadline = time.Now().Add(noProgress)
		}
		if now := time.Now(); now.After(deadline) || now.After(queueDeadline) {
			fail("%s is held by another process; not written", filepath.Base(lockFile))
			return false
		}
		time.Sleep(lockPollDelay(time.Since(begin)))
	}
	defer func() {
		if lock != nil {
			releaseLock(lock, lockFile)
		}
	}()
	work()
	return true
}

var errLockLive = errors.New("the lock is held")

var (
	prunedRunners   []object
	prunedSnapshots []object
	prunedLock      sync.Mutex
)

func notePruned(list *[]object, entry object) {
	prunedLock.Lock()
	defer prunedLock.Unlock()
	*list = append(*list, entry)
}

func takePruned(list *[]object) []object {
	prunedLock.Lock()
	defer prunedLock.Unlock()
	pending := *list
	*list = nil
	return pending
}

func drainPrunedRunners() {
	for _, entry := range takePruned(&prunedRunners) {
		sid, scheduled := getString(entry, "sid"), getMap(entry, "scheduled")
		cancelScheduled(sid, scheduled)
		logInfo("scheduler entry for dropped wait %s cancelled", sid)
	}
}

func updateState(mutator func(state object)) object {
	var result object
	withFileLock(files.stateLock, func() {
		state, before := readStateWithBytes()
		if state == nil {
			fail("state.json could not be read; not written")
			result = emptyState()
			return
		}
		mutator(state)
		pruneState(state, nowSec())
		encoded := marshalPretty(state)
		if encoded == nil {
			fail("state.json could not be encoded; left unchanged")
			result = state
			return
		}
		if before == nil {
			before, _ = readFileShared(files.state)
		}
		if before != nil && bytes.Equal(before, encoded) {
			result = state
			return
		}
		if before != nil && usableStateJSON(before) {
			if err := os.WriteFile(files.stateBackup, before, 0o600); err != nil {
				warn("state.json backup not written: %v", err)
			}
		}
		mustWriteJSON(files.state, state)
		result = state
	})
	drainPrunedRunners()
	for _, entry := range takePruned(&prunedSnapshots) {
		dropGitSnapshot(getString(entry, "cwd"), getString(entry, "ref"))
	}
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
		if err := writeJSONKeepingOrder(files.settings, data); err != nil {
			fail("settings.json could not be written: %v", err)
			return
		}
		changed = true
	})
	return held && changed
}

func hashKey(text string) string {
	var hash uint32 = 5381
	for _, char := range text {
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
