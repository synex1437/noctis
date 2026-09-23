package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type hostSpec struct {
	id, display, exe string
	limits           bool
	statusline       bool
	modelSwitch      bool
	wake             bool
	agents           bool
	stopFailure      bool
	homeEnv          string
	homeDefault      []string
}

var hostSpecs = map[string]hostSpec{
	"claude":      {id: "claude", display: "Claude Code", exe: "claude", limits: true, statusline: true, modelSwitch: true, wake: true, agents: true, stopFailure: true, homeEnv: "CLAUDE_CONFIG_DIR", homeDefault: []string{".claude"}},
	"codex":       {id: "codex", display: "OpenAI Codex CLI", exe: "codex", limits: true, homeEnv: "CODEX_HOME", homeDefault: []string{".codex"}},
	"antigravity": {id: "antigravity", display: "Antigravity CLI (Google)", exe: "agy", limits: true, statusline: true, stopFailure: true, homeDefault: []string{".gemini", "antigravity-cli"}},
	"droid":       {id: "droid", display: "Factory Droid", exe: "droid", homeDefault: []string{".factory"}},
	"copilot":     {id: "copilot", display: "GitHub Copilot CLI", exe: "copilot", stopFailure: true, homeEnv: "COPILOT_HOME", homeDefault: []string{".copilot"}},
}

var hostOrder = []string{"claude", "codex", "antigravity", "droid", "copilot"}

var (
	activeHost  = "claude"
	activeEvent = ""
)

const hookMarker = pluginName

func hostOf(id string) hostSpec {
	if spec, ok := hostSpecs[strings.ToLower(strings.TrimSpace(id))]; ok {
		return spec
	}
	return hostSpecs["claude"]
}

func hostFromArgs() string {
	if flag := strings.ToLower(strings.TrimSpace(flagString("host"))); flag != "" {
		if _, ok := hostSpecs[flag]; ok {
			return flag
		}
	}
	if env := strings.ToLower(strings.TrimSpace(os.Getenv("NOCTIS_HOST"))); env != "" {
		if _, ok := hostSpecs[env]; ok {
			return env
		}
	}
	return ""
}

func resolveHost(cfg object) {
	if fromArgs := hostFromArgs(); fromArgs != "" {
		activeHost = fromArgs
		return
	}
	if configured := strings.ToLower(strings.TrimSpace(getString(cfg, "host"))); configured != "" {
		if _, ok := hostSpecs[configured]; ok {
			activeHost = configured
			return
		}
	}
	activeHost = "claude"
}

func currentHost() hostSpec {
	return hostOf(activeHost)
}

func hostHome(id string) string {
	spec := hostOf(id)
	if spec.homeEnv != "" {
		if env := os.Getenv(spec.homeEnv); env != "" {
			resolved, _ := filepath.Abs(env)
			return resolved
		}
	}
	parts := append([]string{homeDir()}, spec.homeDefault...)
	return filepath.Join(parts...)
}

func hostExecutable(id string) string {
	spec := hostOf(id)
	if id == "claude" {
		return claudeExecutable()
	}
	return locateExecutable(spec.exe)
}

type hostHookFile struct {
	path    string
	summary string
}

func quotedCommand(binary string, extra ...string) string {
	parts := append([]string{`"` + forwardSlashes(binary) + `"`, "hook"}, extra...)
	return strings.Join(parts, " ")
}

func windowsCommand(binary string, extra ...string) string {
	parts := append([]string{`"` + strings.ReplaceAll(binary, "/", `\`) + `"`, "hook"}, extra...)
	return strings.Join(parts, " ")
}

func hookArgs(host, accountDir string) []string {
	extra := []string{"--host", host}
	if accountDir != "" && filepath.Clean(accountDir) != filepath.Clean(hostHome(host)) {
		extra = append(extra, "--account", `"`+forwardSlashes(accountDir)+`"`)
	}
	return extra
}

func wireHostHooks(host, binary, accountDir string) ([]hostHookFile, error) {
	extra := hookArgs(host, accountDir)
	command := quotedCommand(binary, extra...)
	windows := windowsCommand(binary, extra...)
	switch host {
	case "codex":
		file := filepath.Join(accountDir, "hooks.json")
		data := readJSONStrict(file)
		if !data.ok {
			return nil, errors.New(T("host.hooksBroken", file, data.err))
		}
		root := data.data
		if root == nil {
			root = object{}
		}
		hooks := getMap(root, "hooks")
		if hooks == nil {
			hooks = object{}
		}
		handler := func(timeout int) object {
			return object{"type": "command", "command": command, "commandWindows": windows, "timeout": float64(timeout), "statusMessage": hookMarker}
		}
		events := []struct {
			name, matcher string
			timeout       int
		}{
			{"SessionStart", "startup|resume|clear|compact|fork", 20},
			{"SessionEnd", "", 3},
			{"UserPromptSubmit", "", 21600},
			{"PreToolUse", "Agent|spawn_agent", 20},
			{"PostToolUse", "*", 21600},
			{"Stop", "", 60},
		}
		for _, event := range events {
			groups := withoutOurGroups(getList(hooks, event.name))
			group := object{"hooks": []any{handler(event.timeout)}}
			if event.matcher != "" {
				group["matcher"] = event.matcher
			}
			hooks[event.name] = append(groups, group)
		}
		root["hooks"] = hooks
		if err := writeJSONAtomic(file, root); err != nil {
			return nil, err
		}
		return []hostHookFile{{file, "SessionStart, SessionEnd, UserPromptSubmit, PreToolUse, PostToolUse, Stop"}}, nil
	case "droid":
		file := filepath.Join(accountDir, "hooks.json")
		data := readJSONStrict(file)
		if !data.ok {
			return nil, errors.New(T("host.hooksBroken", file, data.err))
		}
		root := data.data
		if root == nil {
			root = object{}
		}
		handler := func(timeout int) object {
			return object{"type": "command", "command": command, "timeout": float64(timeout)}
		}
		events := []struct {
			name, matcher string
			timeout       int
		}{
			{"SessionStart", "", 20},
			{"SessionEnd", "", 10},
			{"UserPromptSubmit", "", 21600},
			{"PreToolUse", "Task", 20},
			{"PostToolUse", "", 21600},
			{"Stop", "", 60},
		}
		for _, event := range events {
			groups := withoutOurGroups(getList(root, event.name))
			group := object{"hooks": []any{handler(event.timeout)}}
			if event.matcher != "" {
				group["matcher"] = event.matcher
			}
			root[event.name] = append(groups, group)
		}
		if err := writeJSONAtomic(file, root); err != nil {
			return nil, err
		}
		return []hostHookFile{{file, "SessionStart, SessionEnd, UserPromptSubmit, PreToolUse, PostToolUse, Stop"}}, nil
	case "antigravity":
		file := filepath.Join(homeDir(), ".gemini", "config", "hooks.json")
		if custom := os.Getenv("NOCTIS_ANTIGRAVITY_HOOKS"); custom != "" {
			file = custom
		}
		data := readJSONStrict(file)
		if !data.ok {
			return nil, errors.New(T("host.hooksBroken", file, data.err))
		}
		root := data.data
		if root == nil {
			root = object{}
		}
		handler := func(timeout int) object {
			return object{"type": "command", "command": command, "timeout": float64(timeout)}
		}
		root[hookMarker] = object{
			"PreToolUse":     []any{object{"matcher": "invoke_subagent|manage_subagents", "hooks": []any{handler(20)}}},
			"PreInvocation":  []any{handler(21600)},
			"PostInvocation": []any{handler(21600)},
			"Stop":           []any{handler(60)},
		}
		if err := writeJSONAtomic(file, root); err != nil {
			return nil, err
		}
		written := []hostHookFile{{file, "PreToolUse, PreInvocation, PostInvocation, Stop"}}
		settingsFile := filepath.Join(accountDir, "settings.json")
		settings := readJSONStrict(settingsFile)
		if !settings.ok {
			written = append(written, hostHookFile{settingsFile, T("host.statuslineSkipped", settings.err)})
			return written, nil
		}
		root = settings.data
		if root == nil {
			root = object{}
		}
		root["statusLine"] = object{"type": "command", "command": `"` + forwardSlashes(binary) + `" statusline ` + strings.Join(extra, " "), "stack_with_default": true}
		if err := writeJSONAtomic(settingsFile, root); err != nil {
			written = append(written, hostHookFile{settingsFile, T("host.statuslineSkipped", err.Error())})
			return written, nil
		}
		written = append(written, hostHookFile{settingsFile, "statusLine"})
		return written, nil
	case "copilot":
		dir := filepath.Join(accountDir, "hooks")
		ensureDir(dir)
		file := filepath.Join(dir, pluginName+".json")
		handler := func(timeout int) object {
			return object{"type": "command", "exec": binary, "args": append([]any{"hook"}, toAnyList(hookArgsPlain(host, accountDir))...), "timeoutSec": float64(timeout)}
		}
		root := object{"version": float64(1), "hooks": object{
			"sessionStart":        []any{handler(20)},
			"sessionEnd":          []any{handler(10)},
			"userPromptSubmitted": []any{handler(21600)},
			"preToolUse":          []any{handler(20)},
			"agentStop":           []any{handler(60)},
			"errorOccurred":       []any{handler(20)},
		}}
		if err := writeJSONAtomic(file, root); err != nil {
			return nil, err
		}
		return []hostHookFile{{file, "sessionStart, sessionEnd, userPromptSubmitted, preToolUse, agentStop, errorOccurred"}}, nil
	}
	return nil, errors.New(T("host.unknown", host))
}

func hookArgsPlain(host, accountDir string) []string {
	extra := []string{"--host", host}
	if accountDir != "" && filepath.Clean(accountDir) != filepath.Clean(hostHome(host)) {
		extra = append(extra, "--account", accountDir)
	}
	return extra
}

func toAnyList(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func withoutOurGroups(groups []any) []any {
	kept := []any{}
	for _, raw := range groups {
		group := toObject(raw)
		if group == nil {
			kept = append(kept, raw)
			continue
		}
		ours := false
		for _, rawHandler := range getList(group, "hooks") {
			handler := toObject(rawHandler)
			if getString(handler, "statusMessage") == hookMarker || strings.Contains(getString(handler, "command"), pluginName) || strings.Contains(getString(handler, "command"), "/noctis") || strings.Contains(getString(handler, "command"), `\noctis`) {
				ours = true
				break
			}
		}
		if !ours {
			kept = append(kept, raw)
		}
	}
	return kept
}

func unwireHostHooks(host, accountDir string) ([]string, error) {
	removed := []string{}
	switch host {
	case "codex", "droid":
		file := filepath.Join(accountDir, "hooks.json")
		data := readJSONStrict(file)
		if !data.ok {
			return removed, errors.New(T("install.uninstallBroken", file, data.err))
		}
		if data.data == nil {
			return removed, nil
		}
		root := data.data
		table := root
		if host == "codex" {
			table = getMap(root, "hooks")
			if table == nil {
				return removed, nil
			}
		}
		for event, raw := range table {
			groups, ok := raw.([]any)
			if !ok {
				continue
			}
			kept := withoutOurGroups(groups)
			if len(kept) == 0 {
				delete(table, event)
			} else {
				table[event] = kept
			}
		}
		if err := writeJSONAtomic(file, root); err != nil {
			return removed, err
		}
		removed = append(removed, file)
	case "antigravity":
		file := filepath.Join(homeDir(), ".gemini", "config", "hooks.json")
		if custom := os.Getenv("NOCTIS_ANTIGRAVITY_HOOKS"); custom != "" {
			file = custom
		}
		settingsFile := filepath.Join(accountDir, "settings.json")
		data := readJSONStrict(file)
		if !data.ok {
			return removed, errors.New(T("install.uninstallBroken", file, data.err))
		}
		settings := readJSONStrict(settingsFile)
		if !settings.ok {
			return removed, errors.New(T("install.uninstallBroken", settingsFile, settings.err))
		}
		if data.data != nil && data.data[hookMarker] != nil {
			delete(data.data, hookMarker)
			if err := writeJSONAtomic(file, data.data); err != nil {
				return removed, err
			}
			removed = append(removed, file)
		}
		if settings.data != nil && strings.Contains(getString(getMap(settings.data, "statusLine"), "command"), "noctis") {
			delete(settings.data, "statusLine")
			if err := writeJSONAtomic(settingsFile, settings.data); err != nil {
				return removed, err
			}
			removed = append(removed, settingsFile)
		}
	case "copilot":
		file := filepath.Join(accountDir, "hooks", pluginName+".json")
		if os.Remove(file) == nil {
			removed = append(removed, file)
		}
	}
	return removed, nil
}

var rateLimitText = lazyRegexp(`(?i)rate.?limit|usage limit|quota|too many requests|\b429\b`)
var authErrorText = lazyRegexp(`(?i)auth|unauthori|login|credential`)
var billingErrorText = lazyRegexp(`(?i)billing|payment|credit`)
var overloadText = lazyRegexp(`(?i)overload|\b529\b|\b5\d\d\b|server error|unavailable|internal error`)

func classifyErrorText(text string) string {
	switch {
	case text == "":
		return "unknown"
	case rateLimitText.MatchString(text):
		return "rate_limit"
	case overloadText.MatchString(text):
		return "overloaded"
	case authErrorText.MatchString(text):
		return "authentication_failed"
	case billingErrorText.MatchString(text):
		return "billing_error"
	}
	return "unknown"
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}

func normalizeHookInput(host string, raw object) (string, object) {
	event := getString(raw, "hook_event_name")
	switch host {
	case "claude":
		return event, raw
	case "codex":
		if getString(raw, "source") == "fork" {
			raw["source"] = "startup"
		}
		switch event {
		case "PostToolUse":
			return "PostToolBatch", raw
		case "SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse", "Stop":
			if event == "PreToolUse" && getString(raw, "tool_name") == "spawn_agent" {
				raw["tool_name"] = "Agent"
			}
			return event, raw
		}
		return "", raw
	case "droid":
		switch event {
		case "PostToolUse":
			return "PostToolBatch", raw
		case "SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse", "Stop", "Notification":
			return event, raw
		}
		return "", raw
	case "antigravity":
		input := object{
			"session_id":      getString(raw, "conversationId"),
			"transcript_path": expandHome(getString(raw, "transcriptPath")),
			"permission_mode": "",
		}
		if roots := getList(raw, "workspacePaths"); len(roots) > 0 {
			input["cwd"] = fmt.Sprint(roots[0])
		} else if cwd, err := os.Getwd(); err == nil {
			input["cwd"] = cwd
		}
		input["model"] = getString(raw, "modelName")
		switch event {
		case "PreToolUse":
			call := getMap(raw, "toolCall")
			name := getString(call, "name")
			if name == "invoke_subagent" || name == "manage_subagents" {
				name = "Agent"
			}
			input["hook_event_name"], input["tool_name"], input["tool_input"] = "PreToolUse", name, getMap(call, "args")
			return "PreToolUse", input
		case "PreInvocation", "PostInvocation":
			input["hook_event_name"] = "PostToolBatch"
			return "PostToolBatch", input
		case "Stop":
			if getString(raw, "terminationReason") == "error" {
				input["hook_event_name"] = "StopFailure"
				input["error_type"] = classifyErrorText(getString(raw, "error"))
				input["error_message"] = getString(raw, "error")
				return "StopFailure", input
			}
			input["hook_event_name"] = "Stop"
			input["stop_hook_active"] = numberOr(raw, "executionNum", 1) > 1
			return "Stop", input
		}
		return "", input
	case "copilot":
		input := object{
			"session_id":      getString(raw, "sessionId"),
			"cwd":             getString(raw, "cwd"),
			"transcript_path": getString(raw, "transcriptPath"),
			"permission_mode": "",
		}
		if input["session_id"] == "" {
			input["session_id"] = getString(raw, "session_id")
		}
		if input["transcript_path"] == "" {
			input["transcript_path"] = getString(raw, "transcript_path")
		}
		switch strings.ToLower(event) {
		case "sessionstart":
			source := getString(raw, "source")
			if source == "new" || source == "" {
				source = "startup"
			}
			input["hook_event_name"], input["source"] = "SessionStart", source
			return "SessionStart", input
		case "sessionend":
			input["hook_event_name"], input["reason"] = "SessionEnd", getString(raw, "reason")
			return "SessionEnd", input
		case "userpromptsubmitted", "userpromptsubmit":
			input["hook_event_name"], input["prompt"] = "UserPromptSubmit", getString(raw, "prompt")
			return "UserPromptSubmit", input
		case "pretooluse":
			name := orDefault(getString(raw, "toolName"), getString(raw, "tool_name"))
			if strings.EqualFold(name, "task") {
				name = "Agent"
			}
			input["hook_event_name"], input["tool_name"] = "PreToolUse", name
			if raw["toolArgs"] != nil {
				input["tool_input"] = raw["toolArgs"]
			} else {
				input["tool_input"] = raw["tool_input"]
			}
			return "PreToolUse", input
		case "agentstop", "stop":
			input["hook_event_name"], input["stop_hook_active"] = "Stop", getBool(raw, "stop_hook_active", false)
			return "Stop", input
		case "erroroccurred":
			errorInfo := getMap(raw, "error")
			message := strings.TrimSpace(getString(errorInfo, "name") + " " + getString(errorInfo, "message"))
			input["hook_event_name"], input["error_type"], input["error_message"] = "StopFailure", classifyErrorText(message), message
			return "StopFailure", input
		}
		return "", input
	}
	return event, raw
}

func translateOutput(host, event string, out object) object {
	switch host {
	case "claude":
		return out
	case "codex", "droid":
		if specific := getMap(out, "hookSpecificOutput"); getString(specific, "hookEventName") == "PostToolBatch" {
			specific["hookEventName"] = event
		}
		return out
	case "antigravity":
		specific := getMap(out, "hookSpecificOutput")
		switch event {
		case "PreToolUse":
			if decision := getString(specific, "permissionDecision"); decision != "" {
				return object{"decision": decision, "reason": getString(specific, "permissionDecisionReason")}
			}
			return object{"decision": "allow"}
		case "PreInvocation":
			if cont, ok := out["continue"].(bool); ok && !cont {
				return object{"injectSteps": []any{object{"ephemeralMessage": getString(out, "stopReason") + " Stop this turn now with a one-line status; the plugin resumes the session after the reset."}}}
			}
			if message := joinNotices(getString(out, "systemMessage"), getString(specific, "additionalContext")); message != "" {
				return object{"injectSteps": []any{object{"ephemeralMessage": message}}}
			}
			return object{}
		case "PostInvocation":
			if cont, ok := out["continue"].(bool); ok && !cont {
				return object{"terminationBehavior": "terminate", "injectSteps": []any{object{"ephemeralMessage": getString(out, "stopReason")}}}
			}
			if message := joinNotices(getString(out, "systemMessage"), getString(specific, "additionalContext")); message != "" {
				return object{"injectSteps": []any{object{"ephemeralMessage": message}}, "terminationBehavior": ""}
			}
			return object{}
		case "Stop":
			if getString(out, "decision") == "block" {
				return object{"decision": "continue", "reason": getString(out, "reason")}
			}
			return object{"decision": "stop"}
		}
		return object{}
	case "copilot":
		specific := getMap(out, "hookSpecificOutput")
		switch strings.ToLower(event) {
		case "sessionstart":
			if context := getString(specific, "additionalContext"); context != "" {
				return object{"additionalContext": context}
			}
			return nil
		case "pretooluse":
			if decision := getString(specific, "permissionDecision"); decision != "" {
				return object{"permissionDecision": decision, "permissionDecisionReason": getString(specific, "permissionDecisionReason")}
			}
			return nil
		case "agentstop", "stop":
			if getString(out, "decision") == "block" {
				return object{"decision": "block", "reason": getString(out, "reason")}
			}
			return nil
		}
		return nil
	}
	return out
}

func hostDefaultOutput(host, event string) object {
	if host != "antigravity" {
		return nil
	}
	switch event {
	case "PreToolUse":
		return object{"decision": "allow"}
	case "Stop":
		return object{"decision": "stop"}
	case "PreInvocation", "PostInvocation":
		return object{}
	}
	return nil
}

func bypassFlag(host, arg string) (bypass, takesValue bool) {
	name, _, hasValue := strings.Cut(arg, "=")
	switch host + " " + name {
	case "claude --dangerously-skip-permissions", "claude --allow-dangerously-skip-permissions",
		"codex --dangerously-bypass-approvals-and-sandbox", "copilot --allow-all-tools", "droid --skip-permissions-unsafe":
		return true, false
	case "claude --permission-mode", "droid --auto":
		return true, !hasValue
	}
	return false, false
}

func relaunchExtraArgs(host string, resume object, sid string) []string {
	configured := getList(resume, "extraArgs")
	extra, dropped := []string{}, []string{}
	for index := 0; index < len(configured); index++ {
		arg := fmt.Sprint(configured[index])
		bypass, takesValue := bypassFlag(host, arg)
		if !bypass {
			extra = append(extra, arg)
			continue
		}
		dropped = append(dropped, arg)
		if takesValue && index+1 < len(configured) {
			index++
			dropped = append(dropped, fmt.Sprint(configured[index]))
		}
	}
	if len(dropped) > 0 {
		warn("resume.extraArgs: %s dropped from the relaunch; unattended permissions come from the resume settings, never from extra arguments", strings.Join(dropped, " "))
		journal(sid, "resume", "extra-arg-dropped", strings.Join(dropped, " "), nil)
	}
	return extra
}

func hostLaunchArgs(host string, cfg object, launch launchSpec, effort, permissionMode string) []string {
	resume := section(cfg, "resume")
	extra := relaunchExtraArgs(host, resume, launch.sid)
	switch host {
	case "codex":
		return append(append([]string{"exec", "resume", launch.sid, "--skip-git-repo-check"}, extra...), launch.prompt)
	case "antigravity":
		return append(append([]string{"--conversation", launch.sid, "--output-format", "text"}, extra...), "-p", launch.prompt)
	case "droid":
		auto := orDefault(getString(resume, "droidAuto"), "high")
		return append(append([]string{"exec", "--session-id", launch.sid, "--auto", auto, "--output-format", "text"}, extra...), launch.prompt)
	case "copilot":
		base := []string{"--resume=" + launch.sid, "-p", launch.prompt}

		if getBool(resume, "copilotAllowAllTools", false) {
			base = append(base, "--allow-all-tools")
		}
		return append(base, extra...)
	}
	claudeArgs := append([]string{"--resume", launch.sid, "--model", launch.model, "--effort", effort, "--permission-mode", permissionMode}, extra...)
	if getBool(resume, "remoteControl", false) {
		claudeArgs = append(claudeArgs, "--remote-control")
	}
	return append(claudeArgs, launch.prompt)
}

func fetchCodexRateLimits(exe string, timeout time.Duration) (object, error) {
	if exe == "" {
		return nil, errors.New("codex executable not found")
	}
	command := inGuardDir(exec.Command(exe, "app-server"))
	isolateTree(command)
	command.WaitDelay = time.Second
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		exited := make(chan struct{})
		go func() {
			_ = command.Wait()
			close(exited)
		}()
		select {
		case <-exited:
		case <-time.After(appServerExitGrace):
			killTree(command.Process)
			<-exited
		}
	}()
	write := func(message object) error {
		_, err := stdin.Write(append(marshalCompact(message), '\n'))
		return err
	}
	lines := make(chan string, 64)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-stop:
				return
			}
		}
		close(lines)
	}()
	deadline := time.After(timeout)

	waitFor := func(id float64) (object, error) {
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					return nil, errors.New("app-server closed before answering")
				}
				var message object
				if err := json.Unmarshal([]byte(line), &message); err != nil || message == nil {
					continue
				}
				if got, present := getNumber(message, "id"); !present || got != id {
					continue
				}
				if errorInfo := getMap(message, "error"); errorInfo != nil {
					return nil, fmt.Errorf("app-server error: %s", orDefault(getString(errorInfo, "message"), "unknown"))
				}
				return getMap(message, "result"), nil
			case <-deadline:
				return nil, errors.New("timeout")
			}
		}
	}
	if err := write(object{"method": "initialize", "id": 0, "params": object{"clientInfo": object{"name": pluginName, "title": pluginName, "version": pluginVersion}}}); err != nil {
		return nil, err
	}
	if _, err := waitFor(0); err != nil {
		return nil, err
	}
	if err := write(object{"method": "initialized", "params": object{}}); err != nil {
		return nil, err
	}
	if err := write(object{"method": "account/rateLimits/read", "id": 1, "params": object{}}); err != nil {
		return nil, err
	}
	result, err := waitFor(1)
	if err != nil {
		return nil, err
	}
	windows := codexWindows(result)
	if len(windows) == 0 {
		return nil, errors.New("app-server returned no rate-limit windows")
	}
	return windows, nil
}

func codexWindows(result object) object {
	parsed := object{}
	consider := func(win object) {
		if win == nil {
			return
		}
		rawUsed, okUsed := getNumber(win, "usedPercent")
		resetsAt, okReset := getNumber(win, "resetsAt")
		minutes := numberOr(win, "windowDurationMins", 0)
		if !okUsed || !okReset || minutes <= 0 {
			return
		}
		used, sane := sanePercent(rawUsed)
		if !sane || !saneResetTime(resetsAt, nowSec()) {
			warn("codex reported an unusable window (usedPercent=%v resetsAt=%v); ignored", rawUsed, resetsAt)
			return
		}
		key := ""
		switch {
		case minutes >= 6*24*60:
			key = "seven_day"
		case minutes >= 4*60 && minutes <= 6*60:
			key = "five_hour"
		default:
			return
		}
		current := getMap(parsed, key)
		if current == nil || used > numberOr(current, "used", 0) {
			parsed[key] = object{"used": used, "resetsAt": resetsAt}
		}
	}
	limits := getMap(result, "rateLimits")
	consider(getMap(limits, "primary"))
	consider(getMap(limits, "secondary"))
	if byID := getMap(result, "rateLimitsByLimitId"); byID != nil {
		if main := getMap(byID, "codex"); main != nil {
			consider(getMap(main, "primary"))
			consider(getMap(main, "secondary"))
		}
	}
	return parsed
}

func antigravityRateLimits(input object, now int64) object {
	quota := getMap(input, "quota")
	if quota == nil {
		return nil
	}
	model := strings.ToLower(getString(getMap(input, "model"), "id"))
	limits := object{}
	names := sortedKeys(quota)
	for _, name := range names {
		bucket := toObject(quota[name])
		if bucket == nil {
			continue
		}
		remaining, okRemaining := getNumber(bucket, "remaining_fraction")
		if !okRemaining {
			continue
		}
		var resetsAt float64
		if seconds, ok := getNumber(bucket, "reset_in_seconds"); ok {
			resetsAt = float64(now) + seconds
		} else if win, ok := toWindow(0, bucket["reset_time"]); ok {
			resetsAt = numberOr(win, "resetsAt", 0)
		} else {
			continue
		}
		key := "seven_day"
		lower := strings.ToLower(name)
		if resetsAt-float64(now) <= 5*3600+15*60 || strings.Contains(lower, "5h") || strings.Contains(lower, "five") || strings.Contains(lower, "hour") {
			key = "five_hour"
		}
		used, sane := sanePercent((1 - remaining) * 100)
		if !sane || !saneResetTime(resetsAt, now) {
			warn("the status line quota for %q is unusable (remaining_fraction=%v); ignored", name, remaining)
			continue
		}
		entry := object{"used": used, "resets_at": resetsAt}
		current := getMap(limits, key)
		modelWords := strings.Fields(strings.ToLower(model))
		prefers := len(modelWords) > 0 && strings.Contains(lower, modelWords[0])
		if current == nil || prefers || (!getBool(current, "preferred", false) && used > numberOr(current, "used", 0)) {
			entry["preferred"] = prefers
			limits[key] = entry
		}
	}
	for _, key := range []string{"five_hour", "seven_day"} {
		if entry := getMap(limits, key); entry != nil {
			limits[key] = object{"used_percentage": entry["used"], "resets_at": entry["resets_at"]}
		}
	}
	return limits
}

func normalizeStatuslineInput(host string, input object, now int64) object {
	if host != "antigravity" {
		return input
	}
	if getString(input, "session_id") == "" {
		input["session_id"] = getString(input, "conversation_id")
	}
	if limits := antigravityRateLimits(input, now); limits != nil {
		input["rate_limits"] = limits
	}
	return input
}

func describeHosts() string {
	lines := []string{}
	for index, id := range hostOrder {
		lines = append(lines, fmt.Sprintf("  %d) %s", index+1, hostSpecs[id].display))
	}
	return strings.Join(lines, "\n")
}

func describeHostIDs() string {
	lines := []string{}
	for _, id := range hostOrder {
		lines = append(lines, fmt.Sprintf("  %-12s %s", id, hostSpecs[id].display))
	}
	return strings.Join(lines, "\n")
}

func unknownHost() string {
	named := false
	for _, value := range args.values["host"] {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		named = true
		if _, ok := hostSpecs[strings.ToLower(value)]; !ok {
			return value
		}
	}
	if env := strings.TrimSpace(os.Getenv("NOCTIS_HOST")); !named && env != "" {
		if _, ok := hostSpecs[strings.ToLower(env)]; !ok {
			return "NOCTIS_HOST=" + env
		}
	}
	return ""
}

func parseHostChoice(answer string) string {
	trimmed := strings.ToLower(strings.TrimSpace(answer))
	if trimmed == "" {
		return "claude"
	}
	for index, id := range hostOrder {
		if trimmed == fmt.Sprint(index+1) || trimmed == id || trimmed == strings.ToLower(hostSpecs[id].display) {
			return id
		}
	}
	if len([]rune(trimmed)) < 3 {
		return ""
	}
	chosen := ""
	for _, id := range hostOrder {
		if startsAWord(strings.ToLower(hostSpecs[id].display), trimmed) {
			if chosen != "" {
				return ""
			}
			chosen = id
		}
	}
	return chosen
}

func startsAWord(text, start string) bool {
	for index := range text {
		if index > 0 && isWordByte(text[index-1]) {
			continue
		}
		if strings.HasPrefix(text[index:], start) {
			return true
		}
	}
	return false
}

func isWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func hostHooksWired(host, accountDir string) (bool, string) {
	switch host {
	case "codex":
		file := filepath.Join(accountDir, "hooks.json")
		content, _ := os.ReadFile(file)
		return strings.Contains(string(content), hookMarker), file
	case "droid":
		file := filepath.Join(accountDir, "hooks.json")
		content, _ := os.ReadFile(file)
		return strings.Contains(string(content), "noctis"), file
	case "antigravity":
		file := filepath.Join(homeDir(), ".gemini", "config", "hooks.json")
		if custom := os.Getenv("NOCTIS_ANTIGRAVITY_HOOKS"); custom != "" {
			file = custom
		}
		content, _ := os.ReadFile(file)
		return strings.Contains(string(content), hookMarker), file
	case "copilot":
		file := filepath.Join(accountDir, "hooks", pluginName+".json")
		return statSafe(file) != nil, file
	}
	return false, ""
}

var dottedVersion = lazyRegexp(`^\d+(\.\d+){0,3}$`)

func newerVersion(candidate, current string) bool {
	candidate, current = strings.TrimPrefix(strings.TrimSpace(candidate), "v"), strings.TrimPrefix(strings.TrimSpace(current), "v")
	if !dottedVersion.MatchString(candidate) || !dottedVersion.MatchString(current) {
		return false
	}
	left, right := strings.Split(candidate, "."), strings.Split(current, ".")
	for i := 0; i < len(left) || i < len(right); i++ {
		a, b := 0, 0
		if i < len(left) {
			a, _ = toInt(left[i])
		}
		if i < len(right) {
			b, _ = toInt(right[i])
		}
		if a != b {
			return a > b
		}
	}
	return false
}

func toInt(text string) (int, bool) {
	value := 0
	for _, char := range text {
		if char < '0' || char > '9' {
			return 0, false
		}
		value = value*10 + int(char-'0')
	}
	return value, true
}

func updateEndpoint(cfg object) string {
	url := getString(section(cfg, "update"), "url")
	if env := os.Getenv("NOCTIS_UPDATE_URL"); env != "" {
		url = env
	}
	if url == "off" {
		return ""
	}
	return url
}

func runReleaseCheck() {
	cfg := loadConfig()
	url := updateEndpoint(cfg)
	if url == "" || !getBool(section(cfg, "update"), "check", true) {
		return
	}
	record := object{"checkedAt": float64(nowSec())}
	body, err := httpGetWithTimeout(url, 3*time.Second)
	if err != nil {
		record["error"] = err.Error()
		logInfo("update check failed: %v", err)
	} else {
		var manifest object
		if json.Unmarshal(body, &manifest) == nil && manifest != nil {
			record["latest"] = getString(manifest, "version")
		}
	}
	if err := writeJSONAtomic(files.release, record); err != nil {
		warn("release.json not written: %v", err)
	}
}

func checkForUpdate(cfg object, state object, now int64) string {
	update := section(cfg, "update")
	if !getBool(update, "check", true) {
		return ""
	}
	if updateEndpoint(cfg) == "" {
		return ""
	}
	cached := readJSON(files.release)
	notified := getMap(state, "notified")
	if float64(now)-numberOr(cached, "checkedAt", 0) > 86400 && float64(now)-numberOr(notified, "update:checkAt", 0) > 3600 {
		updateState(func(next object) { stateMap(next, "notified")["update:checkAt"] = float64(now) })
		arguments := []string{"release-check", "--account", files.configDir}
		if host := currentHost().id; host != "claude" {
			arguments = append(arguments, "--host", host)
		}
		detachedSelf(arguments)
	}
	latest := getString(cached, "latest")
	if !newerVersion(latest, pluginVersion) {
		return ""
	}
	key := "update:" + latest
	if notified[key] != nil {
		return ""
	}
	updateState(func(next object) { stateMap(next, "notified")[key] = float64(now) })
	journal("", "SessionStart", "update-available", latest, nil)
	market := marketplaceNameFor(files.pluginRoot)
	command := "/plugin update " + pluginName
	if market != "" {
		command += "@" + market
	}
	if currentHost().id != "claude" {
		command = T("update.hostCommand")
	}
	notice := T("update.available", pluginName, latest, pluginVersion, command)
	if market != "" && currentHost().id == "claude" {
		notice += " " + T("update.autoHint", market)
	}
	return notice
}

func httpGetWithTimeout(url string, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", pluginName+"/"+pluginVersion)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("http %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 64*1024))
}

func newerCachedVersion(pluginRoot string) string {
	normalized := filepath.ToSlash(pluginRoot)
	if !strings.Contains(normalized, "/plugins/cache/") {
		return ""
	}
	parent := filepath.Dir(pluginRoot)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	best := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := entry.Name()
		if newerVersion(candidate, pluginVersion) && (best == "" || newerVersion(candidate, best)) {
			best = candidate
		}
	}
	return best
}

func restartNotice(cfg object, state object, now int64) string {
	latest := newerCachedVersion(files.pluginRoot)
	if latest == "" {
		return ""
	}
	key := "restart:" + latest
	if getMap(state, "notified")[key] != nil {
		return ""
	}
	updateState(func(next object) { stateMap(next, "notified")[key] = float64(now) })
	journal("", "SessionStart", "update-downloaded", latest, nil)
	notify(cfg, pluginName, T("update.downloadedNotify", latest))
	return T("update.downloaded", pluginName, latest, pluginVersion)
}

func coexistenceNotes(settings object) []string {
	notes := []string{}
	hooks := getMap(settings, "hooks")
	for _, event := range []string{"Stop", "UserPromptSubmit", "PreToolUse", "SessionStart"} {
		others := 0
		for _, raw := range getList(hooks, event) {
			for _, rawHandler := range getList(toObject(raw), "hooks") {
				command := getString(toObject(rawHandler), "command")
				if command != "" && !strings.Contains(command, pluginName) {
					others++
				}
			}
		}
		if others > 0 {
			notes = append(notes, T("doctor.coexistHooks", others, event))
		}
	}
	installed := readJSON(filepath.Join(files.configDir, "plugins", "installed_plugins.json"))
	names := []string{}
	for name := range getMap(installed, "plugins") {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, pluginName) {
			continue
		}
		if strings.Contains(lower, "ralph") || strings.Contains(lower, "loop") || strings.Contains(lower, "resume") || strings.Contains(lower, "statusline") || strings.Contains(lower, "usage") || strings.Contains(lower, "snooze") {
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		sort.Strings(names)
		notes = append(notes, T("doctor.coexistPlugins", strings.Join(names, ", ")))
	}
	return notes
}
