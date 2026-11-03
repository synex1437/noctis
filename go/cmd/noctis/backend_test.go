package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func batchOf(sid, cwd string) object {
	return object{"hook_event_name": "PostToolBatch", "session_id": sid, "cwd": cwd, "tool_calls": []any{}}
}

func TestASessionOnAnotherBackendSpendsNoneOfThePlansLimits(t *testing.T) {
	for _, backend := range []struct{ name, variable, value string }{
		{"Ollama", "ANTHROPIC_BASE_URL", "http://localhost:11434"},
		{"LM Studio", "ANTHROPIC_BASE_URL", "http://127.0.0.1:1234"},
		{"llama.cpp", "ANTHROPIC_BASE_URL", "http://localhost:8080"},
		{"a gateway", "ANTHROPIC_BASE_URL", "https://llm.example.com/anthropic"},
		{"Bedrock", "CLAUDE_CODE_USE_BEDROCK", "1"},
	} {
		t.Run(backend.name, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, nil, 95, 40)
			t.Setenv("CLAUDECODE", "1")
			if held := decide(cfg, readState(), object{"session_id": "lb0", "cwd": project}, nowSec(), decideOptions{}); held.wait == nil {
				t.Fatal("the plan's 5h window at 95% does not hold a session on Anthropic's API, so this test proves nothing")
			}
			t.Setenv(backend.variable, backend.value)
			result := decide(cfg, readState(), object{"session_id": "lb1", "cwd": project}, nowSec(), decideOptions{})
			if result.wait != nil || result.warnWindow != nil || result.usage.hasAny {
				t.Fatalf("a session on %s was held to the plan's 5h window at 95%%: wait %+v, warn %+v", backend.name, result.wait, result.warnWindow)
			}
			if batch := hookOutput(t, onPostToolBatch, batchOf("lb1", project), cfg); len(batch) > 0 {
				t.Fatalf("a session on %s was stopped or told about the plan's limits after a tool batch: %v", backend.name, batch)
			}
			if journaledEntry("lb1", "pause") != nil {
				t.Fatalf("a session on %s journaled a pause", backend.name)
			}
		})
	}
}

func TestAnAnthropicBaseURLKeepsThePlansLimits(t *testing.T) {
	for _, env := range [][2]string{
		{"ANTHROPIC_BASE_URL", "https://api.anthropic.com"},
		{"ANTHROPIC_BASE_URL", "https://api.anthropic.com:443/"},
		{"_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL", "1"},
	} {
		cfg, project, _ := limitSandbox(t, nil, 95, 40)
		t.Setenv("CLAUDECODE", "1")
		t.Setenv("ANTHROPIC_BASE_URL", "http://localhost:11434")
		t.Setenv(env[0], env[1])
		if result := decide(cfg, readState(), object{"session_id": "lb2", "cwd": project}, nowSec(), decideOptions{}); result.wait == nil {
			t.Fatalf("with %s=%s the session is on Anthropic's API, yet the 5h window at 95%% does not hold it", env[0], env[1])
		}
	}
}

func TestOutsideASessionTheBackendSettingsJSONNamesCounts(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 95, 40)
	t.Setenv("CLAUDECODE", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	mustWriteJSON(files.settings, object{"env": object{"ANTHROPIC_BASE_URL": "http://localhost:11434"}})
	if backend := foreignBackend(); backend != "http://localhost:11434" {
		t.Fatalf("a terminal command reads the backend as %q, want settings.json's env", backend)
	}
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
	if backend := foreignBackend(); backend != "http://localhost:11434" {
		t.Fatalf("the shell's ANTHROPIC_BASE_URL won over settings.json's, which Claude Code applies over it: %q", backend)
	}
	t.Setenv("CLAUDECODE", "1")
	if backend := foreignBackend(); backend != "" {
		t.Fatalf("inside a session the process environment is what Claude Code applied, yet %q was read", backend)
	}
	if result := decide(cfg, readState(), object{"session_id": "lb3", "cwd": project}, nowSec(), decideOptions{}); result.wait == nil {
		t.Fatal("a session whose process environment names Anthropic's API was not held at 95%")
	}
}

func TestTheDoctorAndTheSelfCheckLeaveTheTokenAndThresholdsOutOnAnotherBackend(t *testing.T) {
	cfg, _, _ := limitSandbox(t, nil, 10, 10)
	t.Setenv("CLAUDECODE", "1")
	files.credentials = filepath.Join(t.TempDir(), ".credentials.json")
	if issues := strings.Join(selfCheckIssues(cfg), "\n"); !strings.Contains(issues, T("selfcheck.token", scopedLabel(cfg))) {
		t.Fatalf("on Anthropic's API without a token the self-check says nothing about it:\n%s", issues)
	}
	t.Setenv("ANTHROPIC_BASE_URL", "http://localhost:11434")
	if issues := strings.Join(selfCheckIssues(cfg), "\n"); strings.Contains(issues, T("selfcheck.token", scopedLabel(cfg))) {
		t.Fatalf("on a local backend the self-check asks for a Claude sign-in:\n%s", issues)
	}
	doctor := strings.Join(doctorLines(cfg), "\n")
	if !strings.Contains(doctor, T("doctor.planLimitsOff", "localhost:11434")) {
		t.Fatalf("the doctor does not say the plan's limits are off on a local backend:\n%s", doctor)
	}
	if strings.Contains(doctor, "OAuth") || strings.Contains(doctor, T("doctor.thresholdsOk", "92", "97", "97")) {
		t.Fatalf("the doctor still checks the sign-in or the thresholds on a local backend:\n%s", doctor)
	}
}

var localBackend = map[string]string{
	"ANTHROPIC_BASE_URL":             "http://localhost:11434",
	"ANTHROPIC_AUTH_TOKEN":           "ollama-7f3a-secret",
	"ANTHROPIC_MODEL":                "qwen3-coder:30b",
	"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "32768",
}

func scheduleBackendWait(t *testing.T, backend, sid string) (*[]recordedCommand, object, string) {
	t.Helper()
	config := object{"resume": object{"mode": "none"}, "alarm": object{"enabled": false}, "wait": object{"heartbeatGraceSeconds": float64(0)}}
	agents := ""
	var recorded *[]recordedCommand
	if backend == "launchd" {
		agents, recorded = sandboxLaunchd(t, config)
	} else {
		recorded = sandboxSystemd(t, config)
	}
	for name, value := range localBackend {
		t.Setenv(name, value)
	}
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "fable", "window": "fable", "until": now, "resumeAt": now + 60, "startedAt": now}
	})
	scheduled := scheduleRunner(loadConfig(), sid, now+60)
	if getString(scheduled, "method") != backend {
		t.Fatalf("the wait was not scheduled on %s: %v", backend, scheduled)
	}
	return recorded, scheduled, agents
}

func TestARunnerTheSchedulerStartsResumesOnTheSessionsOwnBackend(t *testing.T) {
	for _, backend := range []string{"systemd", "launchd"} {
		t.Run(backend, func(t *testing.T) {
			sid := "local-runner-" + backend
			recorded, scheduled, agents := scheduleBackendWait(t, backend, sid)
			for _, entry := range *recorded {
				if strings.Contains(strings.Join(entry.args, " "), localBackend["ANTHROPIC_AUTH_TOKEN"]) {
					t.Fatalf("the backend's token is on the command line of %s: %q", filepath.Base(entry.args[0]), entry.args)
				}
			}
			if backend == "launchd" {
				body, _ := os.ReadFile(filepath.Join(agents, getString(scheduled, "label")+".plist"))
				if strings.Contains(string(body), localBackend["ANTHROPIC_AUTH_TOKEN"]) {
					t.Fatalf("the backend's token is written into the launchd plist:\n%s", body)
				}
			}
			if info, err := os.Stat(runnerProxiesFile(sid)); err != nil || !isWindows && info.Mode().Perm() != 0o600 {
				t.Fatalf("the runner's copy of the backend is missing or not 0600: %v %v", info, err)
			}
			started := jobEnvironment(t, backend, *recorded, scheduled, agents)
			for name := range localBackend {
				t.Setenv(name, started[name])
			}
			if backend == "launchd" {
				firedByLaunchd(t, sid, getString(scheduled, "label"))
			} else {
				firedBySystemd(t, sid, getString(scheduled, "unit"))
			}
			runResume()
			for name, want := range localBackend {
				if got := os.Getenv(name); got != want {
					t.Errorf("the runner %s started resumed the session with %s=%q, want the paused session's %q", backend, name, got, want)
				}
			}
			if statSafe(runnerProxiesFile(sid)) != nil {
				t.Error("the runner left its copy of the backend's token behind")
			}
		})
	}
}

func TestTheWindowOfARelaunchStartsOnTheSessionsOwnBackend(t *testing.T) {
	sandboxFiles(t)
	for name, value := range localBackend {
		t.Setenv(name, value)
	}
	launch := launchSpec{sid: "s1", cwd: t.TempDir(), model: "qwen3-coder:30b", prompt: "carry on"}
	script, _ := unixLaunchScript(launch, "/opt/claude/bin/claude", []string{"--resume", "s1"}, "")
	content, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range localBackend {
		if !strings.Contains(string(content), "export "+name+"="+shellQuote(value)) {
			t.Errorf("the window launcher does not hand the relaunch %s:\n%s", name, content)
		}
	}
}

func TestSetupOnALocalBackendWritesNoClaudeModelOrEffortAndGivesBackItsOwn(t *testing.T) {
	box := newCLIBox(t)
	box.env["CLAUDECODE"] = ""
	settingsFile := filepath.Join(box.account, "settings.json")
	onAnthropic := box.run(t, "setup", "--config-dir", box.account, "--profile", "code", "--permissions", "keep")
	box.configured(t, onAnthropic, box.account, "code")
	written := readJSON(settingsFile)
	if getString(written, "model") == "sonnet" || getString(getMap(written, "env"), "CLAUDE_CODE_EFFORT_LEVEL") == "medium" {
		t.Fatalf("on Anthropic's API setup wrote neither its model nor its effort, so this test proves nothing:\n%s", onAnthropic)
	}
	env := getMap(written, "env")
	env["ANTHROPIC_BASE_URL"] = "http://localhost:11434"
	written["env"] = env
	mustWriteJSON(settingsFile, written)

	local := box.run(t, "setup", "--config-dir", box.account, "--profile", "code", "--permissions", "keep")

	box.configured(t, local, box.account, "code")
	settings := readJSON(settingsFile)
	if model := getString(settings, "model"); model != "sonnet" {
		t.Fatalf("on a local backend setup left the model at %q, want the person's own sonnet back:\n%s", model, local)
	}
	if effort := getString(getMap(settings, "env"), "CLAUDE_CODE_EFFORT_LEVEL"); effort != "medium" {
		t.Fatalf("on a local backend setup left the effort level at %q, want the person's own medium back:\n%s", effort, local)
	}
	if saved := getMap(settings, "modelSettings"); len(saved) > 0 {
		t.Fatalf("on a local backend setup kept the effort level it had saved for a Claude model: %v", saved)
	}
	config := readJSON(filepath.Join(box.account, pluginName, "config.json"))
	for _, record := range []string{"managedModel", "managedEffort", "managedModelEffort"} {
		if config[record] != nil {
			t.Fatalf("setup kept the undo record %s for a value it no longer holds: %v", record, config[record])
		}
	}
	if !strings.Contains(local.stdout, "Claude Code runs on localhost:11434, so setup writes no Claude model or effort level (model: sonnet)") {
		t.Fatalf("setup did not say why it wrote no model or effort:\n%s", local)
	}
}

func TestSetupContextWindowTellsClaudeCodeTheLocalModelsWindowAndGivesBackTheOneBefore(t *testing.T) {
	box := newCLIBox(t)
	box.env["CLAUDECODE"] = ""
	settingsFile := filepath.Join(box.account, "settings.json")
	cliWrite(t, settingsFile, []byte(`{"model": "qwen3-coder:30b", "env": {"ANTHROPIC_BASE_URL": "http://localhost:11434", "CLAUDE_CODE_MAX_CONTEXT_TOKENS": "16384"}}`))
	window := func() string { return getString(getMap(readJSON(settingsFile), "env"), maxContextTokensVar) }
	setup := func(value string) cliRun {
		return box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep", "--context-window", value)
	}

	set := setup("32k")

	box.configured(t, set, box.account, "balanced")
	if got := window(); got != "32000" {
		t.Fatalf("--context-window 32k left %s at %q, want 32000:\n%s", maxContextTokensVar, got, set)
	}
	if !strings.Contains(set.stdout, "env.CLAUDE_CODE_MAX_CONTEXT_TOKENS=32000: from its next start, Claude Code takes this as the window of a model it does not know") {
		t.Fatalf("setup did not say what the window does:\n%s", set)
	}
	if again := setup("131072"); again.code != 0 || window() != "131072" {
		t.Fatalf("a second --context-window did not move the window: %q\n%s", window(), again)
	}
	off := setup("off")
	if off.code != 0 || window() != "16384" {
		t.Fatalf("--context-window off left %q, want the 16384 the person had before setup:\n%s", window(), off)
	}
	if !strings.Contains(off.stdout, "taken back (now: 16384)") {
		t.Fatalf("--context-window off did not say what it took back:\n%s", off)
	}
	for _, bad := range []string{"60%", "999", "12 tokens", "0.0001m", "off-by-one"} {
		refused := setup(bad)
		if refused.code == 0 || !strings.Contains(refused.stdout+refused.stderr, "--context-window takes the local model's window in tokens") || window() != "16384" {
			t.Fatalf("--context-window %q was not refused with what it takes, or changed the window to %q:\n%s", bad, window(), refused)
		}
	}
	if set := setup("0.128M"); set.code != 0 || window() != "128000" {
		t.Fatalf("--context-window 0.128M left %q, want 128000:\n%s", window(), set)
	}

	uninstall := box.run(t, "install", "--uninstall", "--config-dir", box.account, "--host", "claude")

	if uninstall.code != 0 || window() != "16384" {
		t.Fatalf("uninstall left %s at %q, want the 16384 from before setup:\n%s", maxContextTokensVar, window(), uninstall)
	}
}

func TestASubagentIsPinnedOnlyToAModelTheBackendServes(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"subagents": object{"guard": false}}, 20, 10)
	cfg["router"] = object{"subagentModels": object{"Explore": "haiku", "reviewer": "qwen3-coder:30b"}}
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("ANTHROPIC_DEFAULT_HAIKU_MODEL", "")
	pinned := func(agent string) string {
		return spawnedModel(hookOutput(t, onPreToolUse, agentSpawn("s1", project, object{"subagent_type": agent, "prompt": "find the parser"}), cfg))
	}
	for _, c := range []struct {
		name, variable, value, agent, want string
	}{
		{"Anthropic's API", "ANTHROPIC_BASE_URL", "", "Explore", "haiku"},
		{"Ollama", "ANTHROPIC_BASE_URL", "http://localhost:11434", "Explore", ""},
		{"Ollama with a local model of its own", "ANTHROPIC_BASE_URL", "http://localhost:11434", "reviewer", "qwen3-coder:30b"},
		{"Ollama with haiku mapped", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "qwen3:8b", "Explore", "haiku"},
		{"Bedrock", "CLAUDE_CODE_USE_BEDROCK", "1", "Explore", "haiku"},
	} {
		t.Setenv(c.variable, c.value)
		if got := pinned(c.agent); got != c.want {
			t.Fatalf("on %s the %s subagent was pinned to %q, want %q", c.name, c.agent, got, c.want)
		}
	}
}

func TestTheResearchAgentRunsOnTheSessionsModelWhereSettingsNameAnEndpointThatServesNoClaudeModel(t *testing.T) {
	sandboxFiles(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	lite := filepath.Join(root, "agents", "lite.md")
	if err := os.WriteFile(lite, []byte("---\nname: lite\nmodel: sonnet\n---\nDo research.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	roles := object{"research": object{"model": "sonnet", "effort": "high"}}
	frontmatter := func() string {
		content, _ := os.ReadFile(lite)
		front, _, _ := splitFrontmatter(string(content))
		return front
	}
	ollama := object{"env": object{"ANTHROPIC_BASE_URL": "http://localhost:11434"}}
	syncAgentFiles(root, roles, ollama)
	if front := frontmatter(); !strings.Contains(front, "model: inherit") || strings.Contains(front, "effort:") {
		t.Fatalf("with settings.json on a local endpoint that serves no sonnet the research agent asks for it:\n%s", front)
	}
	mapped := object{"env": object{"ANTHROPIC_BASE_URL": "http://localhost:11434", "ANTHROPIC_DEFAULT_SONNET_MODEL": "qwen3-coder:30b"}}
	syncAgentFiles(root, roles, mapped)
	if front := frontmatter(); !strings.Contains(front, "model: sonnet\neffort: high") {
		t.Fatalf("with sonnet mapped to a local model the research agent lost its role's model:\n%s", front)
	}
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("ANTHROPIC_BASE_URL", "http://localhost:11434")
	syncAgentFiles(root, object{"research": object{"model": "haiku", "effort": "low"}}, object{"env": object{}})
	if front := frontmatter(); !strings.Contains(front, "model: haiku\neffort: low") {
		t.Fatalf("an endpoint only one session's environment names decided the agent file every session reads:\n%s", front)
	}
}

func TestTheDoctorNamesTheWindowOfAModelClaudeCodeDoesNotKnow(t *testing.T) {
	cfg, _, _ := limitSandbox(t, nil, 10, 10)
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("ANTHROPIC_BASE_URL", "http://localhost:11434")
	t.Setenv(maxContextTokensVar, "")
	t.Setenv("ANTHROPIC_DEFAULT_SONNET_MODEL", "")
	doctor := func() string { return strings.Join(doctorLines(cfg), "\n") }
	for _, model := range []string{"qwen3-coder:30b", "claude-sonnet-5-5", "sonnet", ""} {
		t.Setenv("ANTHROPIC_MODEL", model)
		lines := doctor()
		if hinted := strings.Contains(lines, T("doctor.contextWindowUnknown")); hinted != (model == "qwen3-coder:30b") {
			t.Fatalf("with the model %q the doctor's window hint is %v:\n%s", model, hinted, lines)
		}
		if strings.Contains(lines, "paid usage credits") || strings.Contains(lines, T("doctor.usage", T("doctor.usageNone"))) {
			t.Fatalf("on a local backend the doctor still checks the plan's credits or usage:\n%s", lines)
		}
	}
	t.Setenv("ANTHROPIC_DEFAULT_SONNET_MODEL", "glm-4.6")
	t.Setenv("ANTHROPIC_MODEL", "sonnet")
	if lines := doctor(); !strings.Contains(lines, T("doctor.contextWindowUnknown")) {
		t.Fatalf("with sonnet mapped to a local model the doctor gives no window hint:\n%s", lines)
	}
	t.Setenv("ANTHROPIC_DEFAULT_SONNET_MODEL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	now := float64(nowSec())
	mustWriteJSON(files.usage, object{"sessions": object{"old": object{"model": "claude-opus-5-5", "updatedAt": now - 60}, "new": object{"model": "devstral:24b", "updatedAt": now}}})
	if lines := doctor(); !strings.Contains(lines, T("doctor.contextWindowUnknown")) {
		t.Fatalf("with the model only on claude's command line the doctor does not go by the model sessions ran on:\n%s", lines)
	}
	t.Setenv(maxContextTokensVar, "32768")
	if lines := doctor(); !strings.Contains(lines, T("doctor.contextWindowSet", "32768")) || strings.Contains(lines, T("doctor.contextWindowUnknown")) {
		t.Fatalf("the doctor does not name the window set for the local model:\n%s", lines)
	}
}

func TestSetupLeavesOutTheAttributionLineOnACustomEndpointOnly(t *testing.T) {
	box := newCLIBox(t)
	box.env["CLAUDECODE"] = ""
	settingsFile := filepath.Join(box.account, "settings.json")
	header := func() (string, bool) {
		value, present := getMap(readJSON(settingsFile), "env")[attributionHeaderVar]
		text, _ := value.(string)
		return text, present
	}
	setup := func(change object) cliRun {
		settings := readJSON(settingsFile)
		env := getMap(settings, "env")
		for name, value := range change {
			env[name] = value
		}
		settings["env"] = env
		mustWriteJSON(settingsFile, settings)
		return box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep")
	}

	local := setup(object{"ANTHROPIC_BASE_URL": "http://127.0.0.1:1234"})

	box.configured(t, local, box.account, "balanced")
	if value, _ := header(); value != "0" {
		t.Fatalf("on LM Studio setup left %s at %q, want 0:\n%s", attributionHeaderVar, value, local)
	}
	if !strings.Contains(local.stdout, "env.CLAUDE_CODE_ATTRIBUTION_HEADER=0: Claude Code leaves out the attribution line") || !strings.Contains(local.stdout, "so 127.0.0.1:1234 can reuse the prompt") {
		t.Fatalf("setup did not say why it left the attribution line out:\n%s", local)
	}
	if back := setup(object{"ANTHROPIC_BASE_URL": "https://api.anthropic.com"}); back.code != 0 {
		t.Fatalf("setup back on Anthropic's API failed:\n%s", back)
	}
	if value, present := header(); present {
		t.Fatalf("back on Anthropic's API setup kept the %s=%q it wrote for the local backend", attributionHeaderVar, value)
	}
	if bedrock := setup(object{"ANTHROPIC_BASE_URL": "", "CLAUDE_CODE_USE_BEDROCK": "1"}); bedrock.code != 0 {
		t.Fatalf("setup on Bedrock failed:\n%s", bedrock)
	}
	if value, present := header(); present {
		t.Fatalf("on Bedrock, which serves Claude itself, setup wrote %s=%q", attributionHeaderVar, value)
	}
	if own := setup(object{"CLAUDE_CODE_USE_BEDROCK": "", "ANTHROPIC_BASE_URL": "http://localhost:8000", attributionHeaderVar: "1"}); own.code != 0 {
		t.Fatalf("setup on vLLM failed:\n%s", own)
	}
	if value, _ := header(); value != "1" {
		t.Fatalf("setup changed the person's own %s=1 to %q", attributionHeaderVar, value)
	}
	if again := setup(object{attributionHeaderVar: ""}); again.code != 0 {
		t.Fatalf("setup on vLLM failed:\n%s", again)
	}
	if value, _ := header(); value != "0" {
		t.Fatalf("on vLLM with nothing of the person's own setup left %s at %q, want 0", attributionHeaderVar, value)
	}

	uninstall := box.run(t, "install", "--uninstall", "--config-dir", box.account, "--host", "claude")

	if value, _ := header(); uninstall.code != 0 || value != "" {
		t.Fatalf("uninstall left %s at %q, want the empty value the person had before:\n%s", attributionHeaderVar, value, uninstall)
	}
}

func TestSetupKeepsTheAccountsClaudeSettingsForAnEndpointOnlyTheShellNames(t *testing.T) {
	box := newCLIBox(t)
	box.env["CLAUDECODE"] = ""
	box.env["ANTHROPIC_BASE_URL"] = "http://localhost:11434"
	settingsFile := filepath.Join(box.account, "settings.json")

	run := box.run(t, "setup", "--config-dir", box.account, "--profile", "code", "--permissions", "keep")

	box.configured(t, run, box.account, "code")
	settings := readJSON(settingsFile)
	if model := getString(settings, "model"); model != "opus" {
		t.Fatalf("an endpoint only the shell names kept setup from writing the model every other session of the account runs on: %q\n%s", model, run)
	}
	if value, present := getMap(settings, "env")[attributionHeaderVar]; present {
		t.Fatalf("an endpoint only the shell names made setup write %s=%v for every session of the account:\n%s", attributionHeaderVar, value, run)
	}
	if !strings.Contains(run.stdout, T("doctor.planLimitsOff", "localhost:11434")) {
		t.Fatalf("the doctor setup runs does not say this shell's sessions run on the local endpoint:\n%s", run)
	}
}

func TestStatusLeavesThePlansRowsOutOnAnotherEndpoint(t *testing.T) {
	cfg, _, _ := limitSandbox(t, nil, 40, 20)
	t.Setenv("CLAUDECODE", "1")
	onAnthropic := describeState(cfg, readState(), currentUsage(nowSec()), nowSec())
	for _, row := range []string{"status.plan", "status.credits"} {
		label := strings.SplitN(T(row, "%s"), ":", 2)[0]
		if !strings.Contains(onAnthropic, label) {
			t.Fatalf("on Anthropic's API noctis status has no %s row, so this test proves nothing:\n%s", row, onAnthropic)
		}
	}
	t.Setenv("ANTHROPIC_BASE_URL", "http://localhost:1234")
	local := describeState(cfg, readState(), currentUsage(nowSec()), nowSec())
	if !strings.Contains(local, T("doctor.planLimitsOff", "localhost:1234")) {
		t.Fatalf("on LM Studio noctis status does not say the limit guard is off:\n%s", local)
	}
	for _, row := range []string{"status.plan", "status.thresholds", "status.credits", "status.scopedData"} {
		if label := strings.SplitN(T(row, "%s", "%s", "%s", "%s"), ":", 2)[0]; strings.Contains(local, label) {
			t.Fatalf("on LM Studio noctis status still shows the plan's %s row:\n%s", row, local)
		}
	}
}
