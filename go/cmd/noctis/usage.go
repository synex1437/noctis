package main

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type window struct {
	used       float64
	resetsAt   float64
	burst      float64
	staleness  float64
	projected  float64
	reportedAt float64
}

type usageView struct {
	fiveHour    *window
	sevenDay    *window
	fable       *window
	hasAny      bool
	updatedAt   float64
	clockOffset float64
}

func (view usageView) byKey(key string) *window {
	switch key {
	case "five_hour":
		return view.fiveHour
	case "seven_day":
		return view.sevenDay
	}
	return nil
}

func storedWindow(raw object) (float64, float64, bool) {
	used, okUsed := getNumber(raw, "used")
	resetsAt, okReset := getNumber(raw, "resetsAt")
	if !okUsed || !okReset {
		return 0, 0, false
	}
	return used, resetsAt, true
}

func activeWindow(used, resetsAt float64, ok bool, now int64) *window {
	if !ok || resetsAt <= float64(now) {
		return nil
	}
	return &window{used: used, resetsAt: resetsAt}
}

func validThreshold(value any) bool {
	number, ok := toNumber(value)
	return ok && number > 0 && number <= 100
}

func usageStaleSeconds(cfg object) float64 {
	return math.Max(60, numberOr(section(cfg, "usage"), "staleMinutes", 20)*60)
}

func thresholdOf(cfg object, key string) float64 {
	return numberOr(section(cfg, "thresholds"), key, 0)
}

var (
	scopedPatternCache     = map[string]*regexp.Regexp{}
	scopedPatternCacheLock sync.Mutex
)

func scopedModelPattern(cfg object) *regexp.Regexp {
	source := getString(section(cfg, "models"), "scopedPattern")
	if source == "" {
		source = "fable"
	}
	scopedPatternCacheLock.Lock()
	compiled, ok := scopedPatternCache[source]
	scopedPatternCacheLock.Unlock()
	if ok {
		return compiled
	}
	compiled, err := regexp.Compile("(?i)" + source)
	if err != nil {
		warn("models.scopedPattern invalid (%s); using 'fable'", err)
		compiled = regexp.MustCompile("(?i)fable")
	}
	scopedPatternCacheLock.Lock()
	scopedPatternCache[source] = compiled
	scopedPatternCacheLock.Unlock()
	return compiled
}

func scopedLabel(cfg object) string {
	if label := getString(section(cfg, "models"), "scopedLabel"); label != "" {
		return label
	}
	return "Fable"
}

func scopedThresholdValue(cfg object) any {
	thresholds := section(cfg, "thresholds")
	if value, present := thresholds["weeklyScoped"]; present && validThreshold(value) {
		return value
	}
	return thresholds["weeklyFable"]
}

func scopedThreshold(cfg object) float64 {
	value, _ := toNumber(scopedThresholdValue(cfg))
	return value
}

func thresholdEnabled(cfg object, key string) (float64, bool) {
	value := section(cfg, "thresholds")[key]
	number, _ := toNumber(value)
	return number, validThreshold(value)
}

func scopedThresholdEnabled(cfg object) (float64, bool) {
	value := scopedThresholdValue(cfg)
	number, _ := toNumber(value)
	return number, validThreshold(value)
}

func pruneSessions(sessions object, now int64) object {
	kept := object{}
	for sid, raw := range sessions {
		info, _ := raw.(object)
		if info != nil && numberOr(info, "updatedAt", 0) > float64(now-sessionTTLSeconds) {
			kept[sid] = info
		}
	}
	return kept
}

func historyList(history object, key string) []any {
	if history == nil {
		return nil
	}
	return getList(history, key)
}

func burstFor(history []any, resetsAt float64, now int64) float64 {
	samples := historySamples(history, resetsAt, func(sample object) bool {
		return float64(now)-numberOr(sample, "at", 0) <= burstWindowSeconds
	})
	burst := 0.0
	for i := 1; i < len(samples); i++ {
		delta := numberOr(samples[i], "used", 0) - numberOr(samples[i-1], "used", 0)
		if delta > burst {
			burst = delta
		}
	}
	return burst
}

func historySamples(history []any, resetsAt float64, keep func(object) bool) []object {
	samples := []object{}
	for _, raw := range history {
		sample, _ := raw.(object)
		if sample == nil {
			continue
		}
		if reset, ok := getNumber(sample, "resetsAt"); !ok || reset != resetsAt {
			continue
		}
		if keep != nil && !keep(sample) {
			continue
		}
		samples = append(samples, sample)
	}
	return samples
}

func slopeFor(history []any, resetsAt float64, now int64, idleAfter float64) (float64, bool) {
	samples := historySamples(history, resetsAt, nil)
	if len(samples) < 2 {
		return 0, false
	}
	first, last := samples[0], samples[len(samples)-1]
	if idleAfter > 0 && float64(now)-numberOr(last, "at", 0) > idleAfter {
		return 0, true
	}
	span := numberOr(last, "at", 0) - numberOr(first, "at", 0)
	rise := numberOr(last, "used", 0) - numberOr(first, "used", 0)
	if span <= 0 || rise <= 0 {
		return 0, true
	}
	return rise / span, true
}

func etaSeconds(history []any, win *window, threshold float64, now int64, offset float64) (float64, bool) {
	if win == nil {
		return 0, false
	}
	slope, ok := slopeFor(history, win.resetsAt+offset, now, 0)
	if !ok || slope <= 0 {
		return 0, false
	}
	remaining := threshold - win.used
	if remaining <= 0 {
		return 0, false
	}
	eta := remaining / slope
	if eta < win.resetsAt-float64(now) {
		return eta, true
	}
	return 0, false
}

func appendHistory(history object, key string, used, resetsAt float64, now int64) {
	list := getList(history, key)
	if len(list) > 0 {
		last, _ := list[len(list)-1].(object)
		if last != nil && numberOr(last, "used", math.NaN()) == used && numberOr(last, "resetsAt", math.NaN()) == resetsAt {
			history[key] = trimHistory(list)
			return
		}
	}
	list = append(list, object{"used": used, "resetsAt": resetsAt, "at": float64(now)})
	history[key] = trimHistory(list)
}

func trimHistory(list []any) []any {
	if len(list) > burstHistoryLimit {
		return list[len(list)-burstHistoryLimit:]
	}
	return list
}

func readUsageRepaired(guardDir string) object {
	file := filepath.Join(guardDir, "usage.json")
	primary := readJSONStrict(file)
	if primary.ok && (primary.data != nil || !primary.exists) {
		return primary.data
	}
	backupFile := file + ".bak"
	backup := readJSONStrict(backupFile)
	if backup.ok && backup.exists && backup.data != nil {
		if err := writeJSONAtomic(file, backup.data); err == nil {
			warn("usage.json was corrupt (%s); restored from the backup", primary.err)
			return backup.data
		}
	}

	if err := os.Remove(file); err == nil {
		warn("usage.json was corrupt (%s) and no backup was usable; it was cleared", primary.err)
	}
	return nil
}

func currentUsageIn(now int64, guardDir string) usageView {
	usage := readUsageRepaired(guardDir)
	fable := readJSON(filepath.Join(guardDir, "fable.json"))
	usageAt := numberOr(usage, "updatedAt", 0)
	fetchedAt := numberOr(fable, "fetchedAt", 0)
	offset := numberOr(fable, "clockOffset", 0)
	oauthHistory := getMap(fable, "history")
	statusHistory := getMap(usage, "history")
	build := func(key string) *window {
		fromStatus := getMap(usage, key)
		fromOauth := getMap(fable, key)
		var raw object
		var at float64
		useOauth := false
		switch {
		case fromStatus != nil && fromOauth != nil:
			if oauthReadingWins(fromStatus, fromOauth, usageAt, fetchedAt) {
				raw, at, useOauth = fromOauth, fetchedAt, true
			} else {
				raw, at = fromStatus, usageAt
			}
		case fromStatus != nil:
			raw, at = fromStatus, usageAt
		case fromOauth != nil:
			raw, at, useOauth = fromOauth, fetchedAt, true
		default:
			return nil
		}
		used, resetsAt, ok := storedWindow(raw)
		win := activeWindow(used, resetsAt-offset, ok, now)
		if win == nil {
			return nil
		}
		var history []any
		if useOauth {
			history = historyList(oauthHistory, key)
			win.reportedAt = at
		} else {
			history = historyList(statusHistory, key)
			win.reportedAt = math.Min(at, numberOr(raw, "at", 0))
		}
		win.burst = burstFor(history, resetsAt, now)
		win.staleness = math.Max(0, float64(now)-at)
		slope, _ := slopeFor(history, resetsAt, now, 0)
		win.projected = win.used + math.Max(0, slope)*math.Min(win.staleness, projectionMaxStaleness)
		return win
	}
	var fableWindow *window
	if raw := getMap(fable, "fable"); raw != nil {
		used, resetsAt, ok := storedWindow(raw)
		fableWindow = activeWindow(used, resetsAt-offset, ok, now)
	}
	return usageView{
		fiveHour:    build("five_hour"),
		sevenDay:    build("seven_day"),
		fable:       fableWindow,
		hasAny:      usageAt > 0 || fetchedAt > 0,
		updatedAt:   math.Max(usageAt, fetchedAt),
		clockOffset: offset,
	}
}

func oauthReadingWins(fromStatus, fromOauth object, usageAt, fetchedAt float64) bool {
	statusUsed, statusReset, statusOk := storedWindow(fromStatus)
	oauthUsed, oauthReset, oauthOk := storedWindow(fromOauth)
	if statusOk && oauthOk && math.Abs(statusReset-oauthReset) <= sameWindowSeconds && statusUsed != oauthUsed {
		return oauthUsed > statusUsed
	}
	return fetchedAt > usageAt
}

func currentUsage(now int64) usageView {
	return currentUsageIn(now, files.guardDir)
}

func oauthToken() string {
	token, _ := oauthTokenState()
	return token
}

func oauthTokenState() (string, string) {
	if token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); token != "" {
		return token, "present"
	}
	credentials := readJSON(files.credentials)
	if credentials == nil && isDarwin {
		credentials = keychainCredentials()
	}
	oauth := getMap(credentials, "claudeAiOauth")
	token := getString(oauth, "accessToken")
	if token == "" {
		return "", "missing"
	}
	if expires, ok := getNumber(oauth, "expiresAt"); ok && expires > 0 && expires < float64(time.Now().UnixMilli()) {
		return "", "expired"
	}
	return token, "present"
}

const claudeSecureStorageEnv = "CLAUDE_SECURESTORAGE_CONFIG_DIR"

func keychainServiceFor(dir string) string {
	if dir == "" {
		return "Claude Code-credentials"
	}
	return "Claude Code-credentials-" + sha256Of([]byte(dir))[:8]
}

func keychainServiceName() string {
	raw := os.Getenv(claudeConfigEnv)
	envDir := raw
	if envDir == "" {
		envDir = filepath.Join(homeDir(), ".claude")
	}
	if absolute, err := filepath.Abs(envDir); err == nil && sameDir(absolute, files.configDir) {
		if secure, set := os.LookupEnv(claudeSecureStorageEnv); set {
			return keychainServiceFor(secure)
		}
		if raw != "" {
			return keychainServiceFor(raw)
		}
	}
	var wait object
	if sid := flagString("sid"); sid != "" {
		wait = getMap(getMap(readState(), "waits"), sid)
	}
	return keychainServiceFor(relaunchConfigDir(wait))
}

func keychainCredentials() object {
	output, err := runWithTimeout(exec.Command("security", "find-generic-password", "-s", keychainServiceName(), "-w"), 5*time.Second)
	if err != nil {
		return nil
	}
	var parsed object
	if err := jsonUnmarshalObject([]byte(strings.TrimSpace(string(output))), &parsed); err != nil {
		return nil
	}
	return parsed
}

func usageEndpoint() *url.URL {
	fallback, _ := url.Parse(defaultUsageURL)
	override := os.Getenv("NOCTIS_USAGE_URL")
	if override == "" {
		return fallback
	}
	parsed, err := url.Parse(override)
	if err != nil || parsed.Host == "" {
		warn("invalid NOCTIS_USAGE_URL ignored: %s", override)
		return fallback
	}

	if parsed.Hostname() != fallback.Hostname() && !localHosts[parsed.Hostname()] {
		warn("NOCTIS_USAGE_URL host %q is not %s or localhost; ignored", parsed.Hostname(), fallback.Hostname())
		return fallback
	}
	if parsed.Scheme == "https" || (parsed.Scheme == "http" && localHosts[parsed.Hostname()]) {
		return parsed
	}
	return fallback
}

type fetchResult struct {
	status    int
	body      []byte
	date      string
	sentAt    int64
	roundTrip time.Duration
	err       string
}

func fetchOauthUsage(token, version string) fetchResult {
	endpoint := usageEndpoint()
	request, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fetchResult{err: err.Error()}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("anthropic-beta", "oauth-2025-04-20")
	request.Header.Set("User-Agent", "claude-code/"+version)
	request.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: fetchTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	sentAt := nowSec()
	started := time.Now()
	response, err := client.Do(request)
	roundTrip := time.Since(started)
	if err != nil {
		message := err.Error()
		if strings.Contains(message, "Timeout") || strings.Contains(message, "deadline") {
			message = "timeout"
		}
		return fetchResult{err: message}
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, responseBodyLimit))

	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fetchResult{err: "truncated-response " + readErr.Error()}
	}
	return fetchResult{status: response.StatusCode, body: body, date: response.Header.Get("Date"), sentAt: sentAt, roundTrip: roundTrip}
}

func sanePercent(value float64) (float64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return math.Max(0, math.Min(100, value)), true
}

func saneResetTime(at float64, now int64) bool {
	if math.IsNaN(at) || math.IsInf(at, 0) || at <= 0 {
		return false
	}
	return at < float64(now)+maxResetHorizon
}

func resetEpoch(value any) (float64, bool) {
	if text, isText := value.(string); isText {
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, text)
		}
		if err == nil {
			return float64(parsed.Unix()), true
		}
	}
	return toNumber(value)
}

func toWindow(percent any, resetsAt any) (object, bool) {
	raw, ok := toNumber(percent)
	if !ok {
		return nil, false
	}
	used, ok := sanePercent(raw)
	if !ok {
		return nil, false
	}
	at, ok := resetEpoch(resetsAt)
	if !ok || !saneResetTime(at, nowSec()) {
		return nil, false
	}
	return object{"used": used, "resetsAt": at}, true
}

var percentKeys = []string{"percent", "utilization", "used_percentage"}

func windowPercent(entry object) (float64, bool) {
	for _, key := range percentKeys {
		if value, ok := getNumber(entry, key); ok {
			return value, true
		}
	}
	return 0, false
}

func payloadWindow(entry object, resetsAt any) (object, bool) {
	for _, key := range percentKeys {
		value, present := entry[key]
		if !present {
			continue
		}
		if win, ok := toWindow(value, resetsAt); ok {
			return win, true
		}
	}
	return nil, false
}

func knownUsageShape(payload object) bool {
	for _, key := range []string{"limits", "five_hour", "seven_day", "seven_day_fable"} {
		if _, found := payload[key]; found {
			return true
		}
	}
	return false
}

func parseUsagePayload(payload object, scoped *regexp.Regexp) object {
	parsed := object{}
	buckets := []string{}
	for _, raw := range getList(payload, "limits") {
		entry, _ := raw.(object)
		if entry == nil {
			continue
		}
		win, ok := payloadWindow(entry, entry["resets_at"])
		if !ok {
			continue
		}
		scopeModel := getMap(getMap(entry, "scope"), "model")
		scopeName := getString(scopeModel, "display_name")
		if scopeName == "" {
			scopeName = getString(scopeModel, "id")
		}
		if scopeName != "" {
			buckets = append(buckets, scopeName)
		}
		switch {
		case getString(entry, "kind") == "session":
			parsed["five_hour"] = win
		case getString(entry, "kind") == "weekly_all":
			parsed["seven_day"] = win
		case scoped.MatchString(scopeName):
			parsed["fable"] = win
		}
	}
	if len(buckets) > 0 {
		parsed["buckets"] = strings.Join(buckets, ", ")
	}
	for _, pair := range [][2]string{{"five_hour", "five_hour"}, {"seven_day", "seven_day"}, {"seven_day_fable", "fable"}} {
		if parsed[pair[1]] != nil {
			continue
		}
		flat := getMap(payload, pair[0])
		if flat == nil {
			continue
		}
		if win, ok := payloadWindow(flat, flat["resets_at"]); ok {
			parsed[pair[1]] = win
		}
	}
	return parsed
}

func liveRefreshError(fable object, now int64) string {
	if numberOr(fable, "backoffUntil", 0) <= float64(now) {
		return ""
	}
	return getString(fable, "error")
}

func refreshProblemText(code string) string {
	switch {
	case code == "no-token":
		return T("refresh.noToken")
	case code == "unexpected-shape":
		return T("refresh.unexpectedShape")
	case strings.HasPrefix(code, "bad-json"):
		return T("refresh.badJSON")
	case strings.HasPrefix(code, "codex "):
		return T("refresh.codex", strings.TrimPrefix(code, "codex "))
	case strings.HasPrefix(code, "http-"):
		status, detail, _ := strings.Cut(strings.TrimPrefix(code, "http-"), " ")
		if _, rest, found := strings.Cut(detail, `": `); found && strings.HasPrefix(detail, `Get "`) {
			detail = rest
		}
		switch status {
		case "0":
			if strings.HasPrefix(detail, "truncated-response") {
				return T("refresh.truncated")
			}
			return T("refresh.network", orDefault(detail, "?"))
		case "401", "403":
			return T("refresh.rejected", status)
		case "429":
			return T("refresh.rateLimited")
		}
		return T("refresh.httpStatus", status)
	}
	return code
}

func refreshNoteText(cfg object, note string) string {
	if note == "no-scoped-bucket-in-response" {
		return T("refresh.noScopedBucket", scopedLabel(cfg))
	}
	return note
}

func clockOffsetFrom(result fetchResult, previous float64) float64 {
	serverTime, err := http.ParseTime(result.date)
	if err != nil {
		return previous
	}
	if result.sentAt == 0 || result.roundTrip < 0 || result.roundTrip > fetchTimeout {
		return previous
	}
	midpoint := time.Unix(result.sentAt, 0).Add(result.roundTrip / 2)
	offset := math.Round(serverTime.Sub(midpoint).Seconds())
	if math.Abs(offset) < clockSkewMinSeconds || math.Abs(offset) > clockSkewMaxSeconds {
		return 0
	}
	if offset != previous {
		direction := "ahead of"
		if offset > 0 {
			direction = "behind"
		}
		warn("clock skew detected: local clock is %s the server by %ds; reset times adjusted", direction, int(math.Abs(offset)))
	}
	return offset
}

func refreshFable(cfg object, now int64, reason string, maxAge float64, ignoreBackoff bool) object {
	cached := readJSON(files.fable)
	if cached == nil {
		cached = object{}
	}
	fableCfg := section(cfg, "fable")
	source := getString(fableCfg, "source")
	if source != "oauth" && source != "codex" {
		return cached
	}
	pollSeconds := maxAge
	if pollSeconds < 0 {
		pollSeconds = math.Max(5, numberOr(fableCfg, "pollMinutes", 10)*60)
	}
	fetchedAt := numberOr(cached, "fetchedAt", 0)
	if fetchedAt > 0 && float64(now)-fetchedAt < pollSeconds {
		return cached
	}
	if !ignoreBackoff && numberOr(cached, "backoffUntil", 0) > float64(now) {
		return cached
	}
	release, acquired := tryFileLock(files.fableLock)
	if !acquired {
		logInfo("fable refresh skipped (%s): another process is fetching", reason)
		return cached
	}
	defer release()
	if latest := readJSON(files.fable); latest != nil && numberOr(latest, "fetchedAt", 0) > fetchedAt {
		return latest
	}
	if source == "codex" {
		return refreshCodexUsage(cached, now, reason)
	}
	token := oauthToken()
	if token == "" {
		next := cloneObject(cached)
		next["error"] = "no-token"
		next["backoffUntil"] = float64(now + 1800)
		mustWriteJSON(files.fable, next)
		logInfo("fable refresh skipped (%s): no usable OAuth token in %s", reason, files.credentials)
		return next
	}
	version := getString(readJSON(files.usage), "version")
	if version == "" {
		version = fallbackClaudeVersion
	}
	response := fetchOauthUsage(token, version)
	if response.status != 200 {
		backoff := int64(120)
		switch response.status {
		case 401, 403:
			backoff = 1800
		case 429:
			backoff = 600
		}
		message := "http-" + itoa(response.status)
		if response.err != "" {
			message += " " + response.err
		}
		next := cloneObject(cached)
		next["error"] = message
		next["backoffUntil"] = float64(now + backoff)
		mustWriteJSON(files.fable, next)
		warn("fable refresh failed (%s): %s", reason, message)
		return next
	}
	var raw any
	if err := json.Unmarshal(response.body, &raw); err != nil {
		next := cloneObject(cached)
		next["error"] = "bad-json " + err.Error()
		next["backoffUntil"] = float64(now + 120)
		mustWriteJSON(files.fable, next)
		warn("fable refresh returned invalid JSON (%s)", reason)
		return next
	}
	payload, _ := raw.(object)
	if !knownUsageShape(payload) {
		next := cloneObject(cached)
		next["error"] = "unexpected-shape"
		next["backoffUntil"] = float64(now + 600)
		mustWriteJSON(files.fable, next)
		if getString(cached, "error") != "unexpected-shape" {
			warn("fable refresh (%s): the usage endpoint answered in a shape noctis does not know; the last reading is kept and the next try is in 10 minutes", reason)
		}
		return next
	}
	parsed := parseUsagePayload(payload, scopedModelPattern(cfg))
	history := getMap(cached, "history")
	if history == nil {
		history = object{}
	}
	for _, key := range []string{"five_hour", "seven_day", "fable"} {
		if win := getMap(parsed, key); win != nil {
			appendHistory(history, key, numberOr(win, "used", 0), numberOr(win, "resetsAt", 0), now)
		}
	}
	next := object{
		"fetchedAt":   float64(now),
		"five_hour":   parsed["five_hour"],
		"seven_day":   parsed["seven_day"],
		"fable":       parsed["fable"],
		"buckets":     parsed["buckets"],
		"clockOffset": clockOffsetFrom(response, numberOr(cached, "clockOffset", 0)),
		"history":     history,
	}
	if parsed["fable"] == nil {
		next["note"] = "no-scoped-bucket-in-response"
		if getString(cached, "note") == "" {
			logInfo("scoped refresh (%s): response has no %s bucket", reason, scopedLabel(cfg))
		}
	}
	mustWriteJSON(files.fable, next)
	logInfo("fable refresh ok (%s): fable=%s 5h=%s 7d=%s", reason, percentText(parsed, "fable"), percentText(parsed, "five_hour"), percentText(parsed, "seven_day"))
	return next
}

var firstNumber = lazyRegexp(`\d+`)

func sweepStaleLocks() {
	lockFiles := []string{files.fableLock, files.stateLock, files.usageLock, filepath.Join(files.guardDir, "schedule.lock")}
	entries, _ := os.ReadDir(files.guardDir)
	for _, entry := range entries {
		if name := entry.Name(); !entry.IsDir() && strings.HasPrefix(name, "schedule-") && strings.HasSuffix(name, ".lock") {
			lockFiles = append(lockFiles, filepath.Join(files.guardDir, name))
		}
	}
	for _, lockFile := range lockFiles {
		if lockAbandoned(lockFile) && removeStaleLock(lockFile) == nil {
			logInfo("abandoned %s removed", filepath.Base(lockFile))
		}
	}
	sweepTempFiles()
}

func sweepTempFiles() {
	sweepDir := func(dir string, age time.Duration, stale func(name string) bool) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !stale(entry.Name()) {
				continue
			}
			info, err := entry.Info()
			if err != nil || time.Since(info.ModTime()) < age {
				continue
			}
			if err := os.Remove(filepath.Join(dir, entry.Name())); err == nil {
				logInfo("leftover %s removed", entry.Name())
			}
		}
	}
	isTemp := func(name string) bool { return strings.HasSuffix(name, ".tmp") }
	sweepDir(files.guardDir, 5*time.Minute, isTemp)
	sweepDir(filepath.Join(files.guardDir, "checkpoints"), 5*time.Minute, isTemp)
	sweepDir(files.launches, 5*time.Minute, func(name string) bool {
		for _, suffix := range []string{".tmp", ".sh", ".pid", ".started", ".json"} {
			if strings.HasSuffix(name, suffix) {
				return true
			}
		}
		return false
	})
	sweepDir(handOffDir(), 5*time.Minute, isTemp)
	sweepDir(handOffDir(), handOffLifetime, func(name string) bool { return strings.HasSuffix(name, ".json") })
}

func lockHolder(lockFile string) (string, time.Duration, bool) {
	info, err := os.Stat(lockFile)
	if err != nil {
		return "", 0, false
	}
	age := time.Since(info.ModTime())
	content, readErr := readFileShared(lockFile)
	if readErr != nil {
		return "", age, true
	}
	return strings.TrimSpace(string(content)), age, true
}

func lockOwnerPid(owner string) (int, bool) {
	pid, parseErr := strconv.Atoi(owner)
	if parseErr != nil {
		if match := firstNumber.FindString(owner); match != "" {
			pid, parseErr = strconv.Atoi(match)
		}
	}
	return pid, parseErr == nil && pid > 0
}

func holderStale(owner string, age time.Duration) bool {
	if pid, parsed := lockOwnerPid(owner); parsed && pid != os.Getpid() {
		if !processAlive(pid) {
			return age > lockDeadOwnerMs*time.Millisecond
		}
		return age > lockLiveHolderMs*time.Millisecond
	}
	return age > lockStaleMs*time.Millisecond
}

func lockAbandoned(lockFile string) bool {
	owner, age, present := lockHolder(lockFile)
	if !present {
		return false
	}
	return holderStale(owner, age)
}

func tryFileLock(lockFile string) (func(), bool) {
	ensureDir(filepath.Dir(lockFile))
	for attempt := 0; attempt < 2; attempt++ {
		handle, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			holdLock(handle)
			_, _ = handle.WriteString(strconv.Itoa(os.Getpid()))
			return func() { releaseLock(handle, lockFile) }, true
		}
		if !lockAbandoned(lockFile) {
			return func() {}, false
		}
		if err := removeStaleLock(lockFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return func() {}, false
		}
	}
	return func() {}, false
}

func percentText(parsed object, key string) string {
	win := getMap(parsed, key)
	if win == nil {
		return "n/a"
	}
	return formatNumber(numberOr(win, "used", 0)) + "%"
}

func cloneObject(source object) object {
	clone := object{}
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func settingsModel() string {
	settings := readJSONStrict(files.settings)
	if !settings.ok || settings.data == nil {
		return ""
	}
	if model, ok := settings.data["model"].(string); ok {
		return model
	}
	return ""
}

func setSettingsEffort(effort string) bool {
	if effort == "" || !currentHost().modelSwitch {
		return false
	}
	previous := ""
	changed := withSettings(func(data object) bool {
		env := getMap(data, "env")
		if env == nil {
			env = object{}
			data["env"] = env
		}
		previous = getString(env, "CLAUDE_CODE_EFFORT_LEVEL")
		if previous == effort {
			return false
		}
		env["CLAUDE_CODE_EFFORT_LEVEL"] = effort
		return true
	})
	if changed {
		logInfo("settings env.CLAUDE_CODE_EFFORT_LEVEL %s -> %s", orDefault(previous, "(unset)"), effort)
	}
	return changed
}

func settingsModelAndEffort() (model string, hasModel bool, effort string, hasEffort bool) {
	settings := readJSONStrict(files.settings)
	if !settings.ok || settings.data == nil {
		return "", false, "", false
	}
	model, hasModel = settings.data["model"].(string)
	effort, hasEffort = getMap(settings.data, "env")["CLAUDE_CODE_EFFORT_LEVEL"].(string)
	return model, hasModel, effort, hasEffort
}

func removeSettingsModel() bool {
	if !currentHost().modelSwitch {
		return false
	}
	return withSettings(func(data object) bool {
		if _, present := data["model"]; !present {
			return false
		}
		delete(data, "model")
		return true
	})
}

func restoreSettingsEffort(switched object) {
	set := getString(switched, "effortSet")
	if set == "" {
		setSettingsEffort(getString(switched, "effortWas"))
		return
	}
	if !currentHost().modelSwitch {
		return
	}
	withSettings(func(data object) bool {
		env := getMap(data, "env")
		if getString(env, "CLAUDE_CODE_EFFORT_LEVEL") != set {
			return false
		}
		if !getBool(switched, "effortAbsent", false) {
			env["CLAUDE_CODE_EFFORT_LEVEL"] = getString(switched, "effortWas")
			return true
		}
		delete(env, "CLAUDE_CODE_EFFORT_LEVEL")
		if len(env) == 0 {
			delete(data, "env")
		}
		return true
	})
}

func setSettingsModel(alias string) bool {

	if !currentHost().modelSwitch {
		logInfo("settings.model unchanged: %s has no switchable default model", currentHost().display)
		return false
	}
	already := false
	changed := withSettings(func(data object) bool {
		if getString(data, "model") == alias {
			already = true
			return false
		}
		data["model"] = alias
		return true
	})
	if changed {
		logInfo("settings.model -> %s", alias)
	}
	return changed || already
}

func resolveSessionModel(cfg, state, usageFile object, sid string) string {
	fromSwitch := getMap(getMap(state, "modelOverrides"), sid)
	fromStatus := getMap(getMap(usageFile, "sessions"), sid)
	if fromSwitch != nil && (fromStatus == nil || numberOr(fromSwitch, "at", 0) >= numberOr(fromStatus, "updatedAt", 0)) {
		return getString(fromSwitch, "model")
	}
	if model := getString(fromStatus, "model"); model != "" {
		return model
	}
	if model := settingsModel(); model != "" {
		return model
	}

	if getMap(state, "modelSwitched") != nil {
		if fallback := getString(section(cfg, "models"), "fallback"); fallback != "" {
			return fallback
		}
	}
	return getString(section(cfg, "models"), "primary")
}

func defaultModel(cfg, state object) string {
	return resolveSessionModel(cfg, state, nil, "")
}

func windowHit(win *window, threshold any) string {
	if win == nil || !validThreshold(threshold) {
		return ""
	}
	limit, _ := toNumber(threshold)
	if win.used >= limit {
		return "threshold"
	}
	if win.burst > 0 && win.used >= burstMinUsed && win.used+win.burst*burstSafety >= 100 {
		return "burst"
	}
	if win.staleness >= projectionMinStaleness && win.projected >= limit {
		return "projection"
	}
	return ""
}

func compactionGuardPercent(cfg object) float64 {
	configured, configuredOk := getNumber(section(cfg, "compaction"), "contextPercent")
	if configuredOk && configured > 0 && configured <= 100 && configured != compactionContextPct {
		return configured
	}
	settings := readJSONStrict(files.settings)
	env := object{}
	if settings.ok {
		env = getMap(settings.data, "env")
	}
	overrideText := getString(env, "CLAUDE_AUTOCOMPACT_PCT_OVERRIDE")
	if overrideText == "" {
		overrideText = os.Getenv("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE")
	}
	if override, ok := toNumber(overrideText); ok && override > 10 && override <= 100 {
		return math.Max(10, override-compactionGuardGap)
	}
	if configuredOk && configured > 0 {
		return configured
	}
	return compactionContextPct
}

type waitPlan struct {
	window    string
	label     string
	used      float64
	threshold float64
	until     float64
	hit       string
	cause     string
}

type warnPlan struct {
	window    string
	label     string
	used      float64
	threshold float64
	resetsAt  float64
}

type decision struct {
	wait       *waitPlan
	warnWindow *warnPlan
	fableHit   bool
	model      string
	usage      usageView
	notice     string

	usageStale     bool
	contextPercent float64
	hasContext     bool
}

func windowLabel(key string) string {
	switch key {
	case "five_hour":
		return T("win.five")
	case "seven_day":
		return T("win.week")
	}
	return T("win.unknown")
}

func evaluate(cfg object, usage usageView, model string, contextPercent float64, hasContext bool) decision {
	thresholds := section(cfg, "thresholds")
	compactionImminent := hasContext && contextPercent >= compactionGuardPercent(cfg)
	result := decision{model: model, usage: usage}
	windows := []struct {
		key, label string
		threshold  any
	}{
		{"five_hour", windowLabel("five_hour"), thresholds["session5h"]},
		{"seven_day", windowLabel("seven_day"), thresholds["weeklyAll"]},
	}
	for _, spec := range windows {
		win := usage.byKey(spec.key)
		hit := windowHit(win, spec.threshold)
		limit, _ := toNumber(spec.threshold)
		if hit == "" && compactionImminent && win != nil && validThreshold(spec.threshold) && win.used >= limit-compactionBand {
			hit = "compaction"
		}
		if hit != "" {
			if result.wait == nil || win.resetsAt > result.wait.until {
				result.wait = &waitPlan{window: spec.key, label: spec.label, used: win.used, threshold: limit, until: win.resetsAt, hit: hit}
			}
			continue
		}
		if win != nil && validThreshold(spec.threshold) {
			band := math.Min(warnBandMax, math.Max(warnBand, 2*win.burst))
			nearest := result.warnWindow == nil || limit-win.used < result.warnWindow.threshold-result.warnWindow.used
			if win.used >= limit-band && nearest {
				result.warnWindow = &warnPlan{window: spec.key, label: spec.label, used: win.used, threshold: limit, resetsAt: win.resetsAt}
			}
		}
	}
	if ceiling := ceilingHit(cfg, usage); ceiling != nil && (result.wait == nil || ceiling.until > result.wait.until) {
		result.wait = ceiling
	}
	scopedValue := scopedThresholdValue(cfg)
	result.fableHit = validThreshold(scopedValue) && usage.fable != nil && usage.fable.used >= scopedThreshold(cfg) && scopedModelPattern(cfg).MatchString(model)

	if result.fableHit && !currentHost().modelSwitch {
		result.fableHit = false
		if result.wait == nil || usage.fable.resetsAt > result.wait.until {
			result.wait = &waitPlan{
				window: "fable", label: scopedLabel(cfg), used: usage.fable.used,
				threshold: scopedThreshold(cfg), until: usage.fable.resetsAt, hit: "threshold",
			}
		}
	}
	return result
}

func nearEdge(cfg object, usage usageView) bool {
	thresholds := section(cfg, "thresholds")
	return (usage.fiveHour != nil && validThreshold(thresholds["session5h"]) && usage.fiveHour.used >= thresholdOf(cfg, "session5h")-nearEdgeBand) ||
		(usage.sevenDay != nil && validThreshold(thresholds["weeklyAll"]) && usage.sevenDay.used >= thresholdOf(cfg, "weeklyAll")-nearEdgeBand) ||
		nearCeiling(cfg, usage)
}

func edgePollSeconds(cfg object, usage usageView) float64 {
	burst := 0.0
	gap := math.Inf(1)
	if usage.fiveHour != nil {
		burst = usage.fiveHour.burst
		if validThreshold(section(cfg, "thresholds")["session5h"]) {
			gap = thresholdOf(cfg, "session5h") - usage.fiveHour.used
		}
	}
	if usage.sevenDay != nil {
		burst = math.Max(burst, usage.sevenDay.burst)
		if validThreshold(section(cfg, "thresholds")["weeklyAll"]) {
			gap = math.Min(gap, thresholdOf(cfg, "weeklyAll")-usage.sevenDay.used)
		}
	}
	if !paidCreditsAllowed(cfg) {
		for _, win := range []*window{usage.fiveHour, usage.sevenDay} {
			if win != nil {
				gap = math.Min(gap, creditCeiling(cfg)-win.used)
			}
		}
	}
	seconds := float64(nearEdgePollSlow)
	switch {
	case burst >= 5:
		seconds = nearEdgePollFast
	case burst >= 2:
		seconds = nearEdgePollNormal
	}
	switch {
	case gap <= 2:
		seconds = math.Min(seconds, nearEdgePollClose)
	case gap <= 4:
		seconds = math.Min(seconds, nearEdgePollFast)
	}
	return seconds
}

func sortedKeys[V any](source map[string]V) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func limitHint(message, scoped string, usage usageView) string {
	lower := strings.ToLower(message)
	if lower == "" {
		return ""
	}
	scopedLower := strings.ToLower(strings.TrimSpace(scoped))
	switch {
	case usage.fable != nil && scopedLower != "" && strings.Contains(lower, scopedLower):
		return "fable"
	case usage.sevenDay != nil && (strings.Contains(lower, "week") || strings.Contains(lower, "7-day") || strings.Contains(lower, "7 day") || strings.Contains(lower, "hafta")):
		return "seven_day"
	case usage.fiveHour != nil && (strings.Contains(lower, "5-hour") || strings.Contains(lower, "5 hour") || strings.Contains(lower, "five-hour") || strings.Contains(lower, "five hour") || strings.Contains(lower, "5 saat") || strings.Contains(lower, "session limit")):
		return "five_hour"
	}
	return ""
}

func refreshCodexUsage(cached object, now int64, reason string) object {
	exe := hostExecutable("codex")
	windows, err := fetchCodexRateLimits(exe, fetchTimeout+3*time.Second)
	if err != nil {
		next := cloneObject(cached)
		next["error"] = "codex " + err.Error()
		next["backoffUntil"] = float64(now + 120)
		mustWriteJSON(files.fable, next)
		warn("codex usage refresh failed (%s): %v", reason, err)
		return next
	}
	history := getMap(cached, "history")
	if history == nil {
		history = object{}
	}
	for _, key := range []string{"five_hour", "seven_day"} {
		if win := getMap(windows, key); win != nil {
			appendHistory(history, key, numberOr(win, "used", 0), numberOr(win, "resetsAt", 0), now)
		}
	}
	next := object{
		"fetchedAt":   float64(now),
		"five_hour":   windows["five_hour"],
		"seven_day":   windows["seven_day"],
		"fable":       nil,
		"buckets":     "codex",
		"clockOffset": float64(0),
		"history":     history,
	}
	mustWriteJSON(files.fable, next)
	logInfo("codex usage refresh ok (%s): 5h=%s 7d=%s", reason, percentText(windows, "five_hour"), percentText(windows, "seven_day"))
	return next
}
