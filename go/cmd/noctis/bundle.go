package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const bundleTailBytes = 512 * 1024

var (
	bundleTokenPattern = lazyRegexp(`(?i)(sk-ant-[A-Za-z0-9_-]+|sk-[a-z]+-[A-Za-z0-9_-]{8,}|gh[pousr]_[A-Za-z0-9]{8,}|github_pat_[A-Za-z0-9_]{8,}|xox[abprs]-[A-Za-z0-9-]{8,}|AKIA[0-9A-Z]{12,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]+|(?:api[_-]?key|access[_-]?token|oauth[_-]?token|secret|password|bearer|token|key)["'=: ]+[A-Za-z0-9._~+/-]{8,}|glpat-[A-Za-z0-9_-]{8,}|npm_[A-Za-z0-9]{8,}|AIza[A-Za-z0-9_-]{8,}|https?://[^\s"'/]+:[^\s"'/]+@)`)

	bundleWebhookPattern = lazyRegexp(`https?://[^\s"']*(hooks\.slack\.com|discord\.com/api/webhooks|api\.telegram\.org|ntfy\.sh|[?&](token|key|secret|auth)=)[^\s"']*`)
	bundleSecretKeys     = map[string]bool{"url": true, "webhook": true, "webhookurl": true, "token": true, "chatid": true, "secret": true, "key": true, "apikey": true, "password": true, "chaincommand": true}
)

func redactBundleText(text string) string {
	text = homeAsTilde(text)
	for _, secret := range configuredSecrets() {
		text = strings.ReplaceAll(text, secret, "<redacted>")
	}
	text = bundleTokenPattern.ReplaceAllString(text, "<redacted-token>")
	text = bundleWebhookPattern.ReplaceAllString(text, "<redacted-webhook>")
	return text
}

func configuredSecrets() []string {
	config := readJSON(files.config)
	if config == nil {
		return nil
	}
	values := []string{}
	collect := func(value any) {
		for _, text := range secretStrings(value) {
			if len(text) >= 6 {
				values = append(values, text)
			}
		}
	}
	collect(getMap(section(config, "alarm"), "webhook"))
	collect(section(config, "alarm")["webhookUrl"])
	collect(section(config, "statusline")["chainCommand"])
	collect(getMap(section(config, "queue"), "github"))
	sort.Strings(values)
	return values
}

func secretStrings(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case object:
		out := []string{}
		for key, inner := range typed {
			if bundleSecretKeys[strings.ToLower(strings.ReplaceAll(key, "_", ""))] {
				out = append(out, secretStrings(inner)...)
			}
		}
		return out
	}
	return nil
}

func redactSecretKeys(value any) any {
	switch typed := value.(type) {
	case object:
		out := object{}
		for key, inner := range typed {
			if bundleSecretKeys[strings.ToLower(strings.ReplaceAll(key, "_", ""))] {
				if text, isText := inner.(string); isText && text != "" {
					out[key] = "<redacted>"
					continue
				}
				if _, isNumber := inner.(float64); isNumber {
					out[key] = "<redacted>"
					continue
				}
			}
			out[key] = redactSecretKeys(inner)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, inner := range typed {
			out = append(out, redactSecretKeys(inner))
		}
		return out
	}
	return value
}

func bundleState(content []byte) []byte {
	var state object
	if jsonUnmarshalObject(bytes.TrimPrefix(content, utf8BOM), &state) != nil || state == nil {
		return []byte(fmt.Sprintf("state.json is not a JSON object (%d bytes); left out\n", len(content)))
	}
	for _, raw := range getMap(state, "waits") {
		if wait := toObject(raw); wait != nil && getString(wait, "queuedPrompt") != "" {
			wait["queuedPrompt"] = fmt.Sprintf("<redacted prompt, length %d>", utf8.RuneCountInString(getString(wait, "queuedPrompt")))
		}
	}
	for _, raw := range getMap(state, "tasks") {
		for _, item := range getMap(toObject(raw), "items") {
			if task := toObject(item); task != nil && getString(task, "subject") != "" {
				task["subject"] = fmt.Sprintf("<redacted task, length %d>", utf8.RuneCountInString(getString(task, "subject")))
			}
		}
	}
	return marshalPretty(state)
}

func listsAsCounts(value any) any {
	switch typed := value.(type) {
	case object:
		out := object{}
		for key, inner := range typed {
			out[key] = listsAsCounts(inner)
		}
		return out
	case []any:
		return fmt.Sprintf("<redacted list, length %d>", len(typed))
	}
	return value
}

func tailOfFile(path string, limit int64) []byte {
	info := statSafe(path)
	if info == nil {
		return nil
	}

	content, cut, err := readTailBytes(path, info.Size(), limit)
	if err != nil {
		return nil
	}

	return dropPartialFirstLine(content, cut)
}

func writeBundle(cfg object) (string, error) {
	target := flagString("bundle")
	if target == "" || target == "true" {
		target = filepath.Join(files.guardDir, fmt.Sprintf("%s-bundle-%s.zip", pluginName, time.Now().Format("20060102-150405")))
	}
	ensureDir(filepath.Dir(target))
	out, err := os.Create(target)
	if err != nil {
		return "", err
	}
	defer out.Close()
	archive := zip.NewWriter(out)
	add := func(name string, content []byte) {
		if content == nil {
			return
		}
		writer, err := archive.Create(name)
		if err == nil {
			_, _ = writer.Write([]byte(redactBundleText(string(content))))
		}
	}
	var summary strings.Builder
	fmt.Fprintf(&summary, "%s %s (%s)\ncreated: %s\nhost: %s\nplugin root: %s\naccount: %s\n\n", pluginName, pluginVersion, platformName(), time.Now().Format(time.RFC3339), currentHost().display, files.pluginRoot, files.configDir)
	summary.WriteString("doctor:\n")
	for _, line := range doctorLines(cfg) {
		summary.WriteString("  " + line + "\n")
	}
	add("summary.txt", []byte(summary.String()))
	for _, file := range []struct{ name, path string }{
		{"guard.log", files.log}, {"errors.log", files.errors}, {"resume-output.log", files.resumeLog},
		{"decisions.jsonl", files.decisions}, {"hooks-debug.log", filepath.Join(files.guardDir, "hooks-debug.log")},
	} {
		add(file.name, tailOfFile(file.path, bundleTailBytes))
	}
	if content, err := os.ReadFile(files.state); err == nil {
		add("state.json", bundleState(content))
	}
	for _, file := range []struct{ name, path string }{{"usage.json", files.usage}, {"fable.json", files.fable}} {
		if content, err := os.ReadFile(file.path); err == nil {
			add(file.name, content)
		}
	}

	if config := readJSON(files.config); config != nil {
		add("config.json", marshalPretty(redactSecretKeys(config)))
	}
	if settings := readJSONStrict(files.settings); settings.ok && settings.data != nil {

		subset := object{"model": settings.data["model"], "statusLine": redactSecretKeys(getMap(settings.data, "statusLine")), "permissions": listsAsCounts(settings.data["permissions"])}
		if env := getMap(settings.data, "env"); env != nil {
			subset["env"] = object{"CLAUDE_CODE_EFFORT_LEVEL": env["CLAUDE_CODE_EFFORT_LEVEL"]}
		}
		add("settings-subset.json", marshalPretty(subset))
	}
	if hooks, err := os.ReadFile(files.hooks); err == nil {
		add("hooks.json", hooks)
	}
	if err := archive.Close(); err != nil {
		return "", err
	}
	return target, nil
}

func shippedChecksum(pluginRoot, relative string) string {
	content, err := os.ReadFile(filepath.Join(pluginRoot, "bin", "SHA256SUMS"))
	if err != nil {
		return ""
	}
	relative = forwardSlashes(relative)
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != relative {
			continue
		}

		if sum := strings.ToLower(fields[0]); sha256Hex.MatchString(sum) {
			return sum
		}
	}
	return ""
}

var sha256Hex = lazyRegexp(`^[0-9a-f]{64}$`)

func sha256Of(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

var errChecksumMismatch = errors.New("checksum mismatch")

func shortSum(sum string) string {
	if len(sum) <= 12 {
		return sum
	}
	return sum[:12]
}

func verifyShippedBinary(pluginRoot string, content []byte) error {
	relative := runtime.GOOS + "-" + runtime.GOARCH + "/" + binaryFileName()
	expected := shippedChecksum(pluginRoot, relative)
	if expected == "" {

		return fmt.Errorf("%w: bin/SHA256SUMS has no entry for bin/%s", errChecksumMismatch, relative)
	}
	if actual := sha256Of(content); actual != expected {
		return fmt.Errorf("%w for bin/%s: expected %s…, found %s…", errChecksumMismatch, relative, shortSum(expected), shortSum(actual))
	}
	return nil
}

func readTailBytes(path string, size, limit int64) (content []byte, cut bool, err error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer handle.Close()
	start := int64(0)
	if size > limit {
		start = size - limit
	}
	buffer := make([]byte, size-start)
	read, err := handle.ReadAt(buffer, start)

	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	return buffer[:read], start > 0, nil
}

func dropPartialFirstLine(content []byte, cut bool) []byte {
	if !cut {
		return content
	}
	if at := bytes.IndexByte(content, '\n'); at >= 0 {
		return content[at+1:]
	}
	return nil
}
