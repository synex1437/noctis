package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const opusOnlyWorkflow = "export const meta = { name: 'port-routes', description: 'port every route', phases: [{ name: 'port' }] }\nconst done = await parallel(['a', 'b'].map((route) => agent('port the ' + route + ' route', { model: 'opus', phase: 'port' })))\nreturn done\n"

func fanOutSession(t *testing.T, overrides object, model string, fable float64) (object, string) {
	t.Helper()
	cfg, project, _ := limitSandbox(t, overrides, 10, 20)
	now := float64(nowSec())
	usage := readJSON(files.usage)
	usage["sessions"] = object{"fan": object{"model": model, "updatedAt": now}}
	mustWriteJSON(files.usage, usage)
	writeFableBucket(fable, now, now+3*86400)
	return cfg, project
}

func launchVerdict(t *testing.T, cfg object, project string, toolInput object) object {
	t.Helper()
	return hookOutput(t, onPreToolUse, agentHookInput("PreToolUse", "fan", project, object{"tool_name": "Workflow", "tool_input": toolInput}), cfg)
}

func fanOutAdvice(cfg object, model string) string {
	return suggestWorkflow(cfg, "Migrate every route handler under src/routes to the new router", evaluate(cfg, currentUsage(nowSec()), model, 0, false))
}

func TestTheFableWindowDoesNotHoldBackAFanOutThatCannotUseFable(t *testing.T) {
	cfg, project := fanOutSession(t, nil, "claude-opus-5-5", 72)
	if output := launchVerdict(t, cfg, project, object{"script": opusOnlyWorkflow}); permissionOf(output) == "deny" {
		t.Fatalf("an Opus session launching a script whose agents all run on Opus was refused over the Fable window (72%%, 23 points before its 95%% switch point): %s", reasonOf(output))
	}
	script := filepath.Join(project, "port-routes.js")
	if err := os.WriteFile(script, []byte(opusOnlyWorkflow), 0o644); err != nil {
		t.Fatal(err)
	}
	if output := launchVerdict(t, cfg, project, object{"scriptPath": script}); permissionOf(output) == "deny" {
		t.Fatalf("an Opus session relaunching an Opus-only script by its path was refused over the Fable window: %s", reasonOf(output))
	}
	if output := launchVerdict(t, cfg, project, object{"scriptPath": "port-routes.js", "resumeFromRunId": "run-1"}); permissionOf(output) == "deny" {
		t.Fatalf("an Opus-only script named by a path relative to the session's folder was refused over the Fable window: %s", reasonOf(output))
	}
	if advice := fanOutAdvice(cfg, "claude-opus-5-5"); !strings.Contains(advice, "fan-out task") {
		t.Fatal("an Opus session on the shipped roles (Opus and Haiku agents) got no workflow advice because of the Fable window")
	}
}

func TestAFanOutThatCanUseFableStillNeedsRoomInTheFableWindow(t *testing.T) {
	cfg, project := fanOutSession(t, nil, "claude-opus-5-5", 72)
	refused := func(what string, toolInput object) {
		t.Helper()
		output := launchVerdict(t, cfg, project, toolInput)
		if permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), scopedLabel(cfg)+" window") {
			t.Fatalf("%s went through with 23 points of room before the Fable switch point: %v", what, output)
		}
	}
	refused("a script with an agent on Fable", object{"script": strings.Replace(opusOnlyWorkflow, "model: 'opus'", "model: 'fable'", 1)})
	refused("a script that takes its model from args naming Fable", object{"script": strings.Replace(opusOnlyWorkflow, "model: 'opus'", "model: args.model", 1), "args": object{"model": "claude-fable-5-1"}})
	refused("a script that starts another workflow", object{"script": opusOnlyWorkflow + "await workflow('audit-everything')\n"})
	refused("a predefined workflow launched by name", object{"name": "audit-everything"})
	refused("a run resumed by its id alone", object{"runId": "run-1"})
	refused("a script path that cannot be read", object{"scriptPath": filepath.Join(project, "missing.js")})
	fable := filepath.Join(project, "fable-routes.js")
	if err := os.WriteFile(fable, []byte(strings.Replace(opusOnlyWorkflow, "model: 'opus'", "model: 'fable'", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	refused("a script file with an agent on Fable", object{"scriptPath": fable, "script": opusOnlyWorkflow})
	oversized := filepath.Join(project, "huge-routes.js")
	if err := os.WriteFile(oversized, []byte(opusOnlyWorkflow+strings.Repeat("// padding\n", workflowScriptMaxBytes/10)), 0o644); err != nil {
		t.Fatal(err)
	}
	refused("a script file over the size noctis reads", object{"scriptPath": oversized})
	nested := hookOutput(t, onPreToolUse, subagentTool("fan", project, "Workflow", object{"script": opusOnlyWorkflow}), cfg)
	if permissionOf(nested) != "deny" || !strings.Contains(reasonOf(nested), scopedLabel(cfg)+" window") {
		t.Fatalf("a subagent, whose own model noctis cannot see, launched a workflow with 23 points of Fable room: %v", nested)
	}

	cfg, project = fanOutSession(t, nil, "claude-fable-5-1", 72)
	refused("an Opus-only script launched from a Fable session", object{"script": opusOnlyWorkflow})
	if advice := fanOutAdvice(cfg, "claude-fable-5-1"); advice != "" {
		t.Fatalf("a Fable session with 23 points of Fable room was advised to fan out: %q", advice)
	}

	cfg, _ = fanOutSession(t, object{"roles": object{"code": object{"model": "fable", "effort": "max"}}}, "claude-opus-5-5", 72)
	if advice := fanOutAdvice(cfg, "claude-opus-5-5"); advice != "" {
		t.Fatalf("the advice would put the code agents on Fable with 23 points of Fable room, and it was given: %q", advice)
	}
}

func TestAFanOutWhoseAgentTypeRunsOnFableNeedsRoomInTheFableWindow(t *testing.T) {
	cfg, project := fanOutSession(t, nil, "claude-opus-5-5", 72)
	for _, name := range []string{"lite.md", "digest.md"} {
		content, err := os.ReadFile(filepath.Join(repoRoot(), "agents", name))
		if err != nil {
			t.Fatal(err)
		}
		writeRepoFile(t, files.pluginRoot, "agents/"+name, string(content))
	}
	withAgentType := func(agentType string) object {
		return object{"script": strings.Replace(opusOnlyWorkflow, "{ model: 'opus', phase: 'port' }", "{ agentType: "+agentType+", phase: 'port' }", 1)}
	}
	allowed := func(what string, toolInput object) {
		t.Helper()
		if output := launchVerdict(t, cfg, project, toolInput); permissionOf(output) == "deny" {
			t.Fatalf("%s was refused over the Fable window: %s", what, reasonOf(output))
		}
	}
	refused := func(what string, toolInput object) {
		t.Helper()
		output := launchVerdict(t, cfg, project, toolInput)
		if permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), scopedLabel(cfg)+" window") {
			t.Fatalf("%s went through with 23 points of room before the Fable switch point: %v", what, output)
		}
	}
	allowed("a script whose agents are noctis:lite, on Opus in the shipped agent file,", withAgentType("'noctis:lite'"))
	allowed("a script whose agents are noctis:digest, on Haiku,", withAgentType(`"noctis:digest"`))
	if changed := syncAgentFiles(files.pluginRoot, object{"research": object{"model": "fable", "effort": "high"}}, false); changed != 1 {
		t.Fatalf("setup's projection of the research role onto agents/lite.md changed %d files", changed)
	}
	refused("a script whose agents are noctis:lite, with the research role (and so agents/lite.md) on Fable,", withAgentType("'noctis:lite'"))
	allowed("a script whose agents are noctis:digest, still on Haiku,", withAgentType("'noctis:digest'"))
	refused("a script that takes its agent type from a variable", withAgentType("kind"))
	refused("a script naming an agent type whose model noctis cannot read", withAgentType("'code-reviewer'"))
}
