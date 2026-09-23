package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
)

const (
	quietMaxSeconds     = 120
	quietHeadroomPoints = 10
	quietUsedTolerance  = 2
	quietContextDrift   = 3
)

func quietMarkerPath(sid string) string {
	return filepath.Join(files.guardDir, "quiet", safeName(sid)+".json")
}

func fileStamp(path string) string {
	content, err := readFileShared(path)
	if err != nil {
		return "absent"
	}
	sum := sha256.Sum256(content)
	return strconv.Itoa(len(content)) + ":" + hex.EncodeToString(sum[:8])
}

func quietEligible(cfg, state object, sid string, result decision, usageStale bool) bool {
	if observing || result.wait != nil || result.fableHit || result.warnWindow != nil || result.notice != "" || usageStale || !result.usage.hasAny {
		return false
	}
	if getMap(getMap(state, "waits"), sid) != nil || getMap(getMap(state, "handedOff"), sid) != nil || getMap(getMap(state, "overload"), sid) != nil || getMap(state, "modelSwitched") != nil {
		return false
	}

	if len(getList(state, "interruptedWaits")) > 0 || numberOr(state, "hookCapSeconds", 0) > 0 || numberOr(state, "disabledUntil", 0) > float64(nowSec()) {
		return false
	}
	thresholds := section(cfg, "thresholds")
	for _, spec := range []struct {
		win *window
		key string
	}{{result.usage.fiveHour, "session5h"}, {result.usage.sevenDay, "weeklyAll"}} {
		if spec.win == nil || !validThreshold(thresholds[spec.key]) {
			continue
		}
		limit := thresholdOf(cfg, spec.key)
		if spec.win.used >= limit-warnBandMax-quietHeadroomPoints || spec.win.projected >= limit-warnBandMax {
			return false
		}
	}
	if result.usage.fable != nil && validThreshold(scopedThresholdValue(cfg)) && result.usage.fable.used >= scopedThreshold(cfg)-warnBandMax-quietHeadroomPoints {
		return false
	}
	if cap := numberOr(section(cfg, "budget"), "dailyWeeklyPercent", 0); cap > 0 {
		if used, limit, _ := dailyBudgetStatus(cfg, state, result.usage, nowSec()); used >= limit-quietHeadroomPoints {
			return false
		}
	}
	return true
}

func writeQuietMarker(sid string, stateStamp string, contextPercent float64, hasContext bool, model string) {
	usage := readJSON(files.usage)
	marker := object{
		"at": float64(nowSec()), "model": model,
		"config": fileStamp(files.config), "state": stateStamp,

		"fableFile": fileStamp(files.fable),
	}
	for _, spec := range []struct{ key, used, resets string }{{"five_hour", "five", "fiveResetsAt"}, {"seven_day", "seven", "sevenResetsAt"}} {
		win := getMap(usage, spec.key)
		if win == nil {
			marker[spec.used], marker[spec.resets] = float64(-1), float64(0)
			continue
		}
		used, resetsAt, ok := storedWindow(win)
		if !ok {
			marker[spec.used], marker[spec.resets] = float64(-1), float64(0)
			continue
		}
		marker[spec.used], marker[spec.resets] = used, resetsAt
	}
	if hasContext {
		marker["context"] = contextPercent
	}

	if fileStamp(files.state) != stateStamp {
		clearQuietMarker(sid)
		return
	}
	if err := writeJSONAtomic(quietMarkerPath(sid), marker); err != nil {
		logInfo("quiet marker not written: %v", err)
	}
}

func clearQuietMarker(sid string) {
	_ = os.Remove(quietMarkerPath(sid))
}

func sweepQuietMarkers(now int64) {
	entries, err := os.ReadDir(filepath.Join(files.guardDir, "quiet"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && float64(now)-float64(info.ModTime().Unix()) > 86400 {
			_ = os.Remove(filepath.Join(files.guardDir, "quiet", entry.Name()))
		}
	}
}

func quietFastPath(input object) bool {
	if os.Getenv("NOCTIS_NO_QUIET") != "" {
		return false
	}
	sid := sessionKey(input)
	if sid == "" || insideSubagent(input) {
		return false
	}
	marker := readJSON(quietMarkerPath(sid))
	if marker == nil {
		return false
	}

	age := float64(nowSec()) - numberOr(marker, "at", 0)
	if age < 0 || age > quietMaxSeconds {
		return false
	}
	if getString(marker, "config") != fileStamp(files.config) || getString(marker, "state") != fileStamp(files.state) || getString(marker, "fableFile") != fileStamp(files.fable) {
		return false
	}
	usage := readJSON(files.usage)
	if usage == nil {
		return false
	}
	for _, spec := range []struct{ key, used, resets string }{{"five_hour", "five", "fiveResetsAt"}, {"seven_day", "seven", "sevenResetsAt"}} {
		win := getMap(usage, spec.key)
		recorded := numberOr(marker, spec.used, -1)
		if win == nil {
			if recorded >= 0 {
				return false
			}
			continue
		}
		used, resetsAt, ok := storedWindow(win)
		if !ok || recorded < 0 || used > recorded+quietUsedTolerance || resetsAt != numberOr(marker, spec.resets, -1) {
			return false
		}
	}
	session := getMap(getMap(usage, "sessions"), sid)
	if session != nil {
		if model := getString(session, "model"); model != "" && model != getString(marker, "model") {
			return false
		}
		if context, ok := getNumber(session, "context"); ok {
			recorded, had := getNumber(marker, "context")
			if !had || context > recorded+quietContextDrift {
				return false
			}
		}
	}
	return true
}
