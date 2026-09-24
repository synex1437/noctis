package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupRefusesAModelNameClaudeCodeDoesNotKnow(t *testing.T) {
	for _, value := range []string{"opsu:max", "sonet", "hiaku:high", "gpt-5", "latest"} {
		model := strings.SplitN(value, ":", 2)[0]
		if _, err := parseRoleFlag("code", value, false); err == nil || !strings.Contains(err.Error(), model) {
			t.Errorf("setup accepted --code %s, whose model %q is not one Claude Code knows (err %v)", value, model, err)
		}
		if problems := badRoleValues(object{"code": object{"model": model}}, false); len(problems) != 1 {
			t.Errorf("the doctor does not name the code role's model %q: %v", model, problems)
		}
	}
	for _, value := range []string{"opus", "sonnet", "haiku", "fable", "best", "opusplan", "default", "inherit", "opus[1m]", "sonnet[1m]:high", "claude-opus-5-5:max", "claude-mythos-5", "claude-haiku-4-5@20251001", "claude-future-9"} {
		if _, err := parseRoleFlag("code", value, false); err != nil {
			t.Errorf("setup refused --code %s: %v", value, err)
		}
	}
}

func TestATypedModelNameLeavesTheAccountUntouched(t *testing.T) {
	box := newCLIBox(t)

	run := box.run(t, "setup", "--config-dir", box.account, "--code", "opsu:max")

	if run.code != 1 || !strings.Contains(run.stderr, "opsu") {
		t.Fatalf("want exit 1 naming the unknown model:\n%s", run)
	}
	box.untouched(t, run)
}

var claudeProviderEnv = []string{"ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_USE_ANTHROPIC_AWS", "CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD", "CLAUDE_CODE_USE_MANTLE", "CLAUDE_CODE_USE_GATEWAY"}

const bedrockProfileARN = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc123"

func TestAProviderModelIDReachesTheAgentFileOnlyWhereClaudeCodeUsesThatProvider(t *testing.T) {
	cases := []struct {
		name, settings string
		env            map[string]string
		taken          bool
	}{
		{"bedrock in settings.json", `{"env": {"CLAUDE_CODE_USE_BEDROCK": "1"}}`, nil, true},
		{"foundry in the environment", `{}`, map[string]string{"CLAUDE_CODE_USE_FOUNDRY": "true"}, true},
		{"a gateway in the environment", `{}`, map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:4000"}, true},
		{"a gateway in settings.json", `{"env": {"ANTHROPIC_BASE_URL": "https://llm.example.com/anthropic"}}`, nil, true},
		{"vertex in settings.json", `{"env": {"CLAUDE_CODE_USE_VERTEX": "yes"}}`, nil, true},
		{"a gateway switch in the environment", `{}`, map[string]string{"CLAUDE_CODE_USE_GATEWAY": "On"}, true},
		{"anthropic's own API", `{"env": {"ANTHROPIC_BASE_URL": "https://api.anthropic.com", "CLAUDE_CODE_USE_BEDROCK": "0"}}`, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, name := range claudeProviderEnv {
				t.Setenv(name, "")
			}
			for name, value := range c.env {
				t.Setenv(name, value)
			}
			root := cliPluginTree(t)
			lite := filepath.Join(root, "agents", "lite.md")
			before := cliRead(t, lite)
			cliWrite(t, files.settings, []byte(c.settings))
			mustWriteJSON(files.config, object{"roles": object{"research": object{"model": bedrockProfileARN}}})

			capturedStdout(t, runEnsure)

			written := strings.Contains(string(cliRead(t, lite)), "model: "+bedrockProfileARN+"\n")
			refused := strings.Contains(loggedErrors(), "left unchanged")
			named := strings.Contains(strings.Join(doctorRun(t, "claude", loadConfig()), "\n"), "not a model Claude Code knows")
			if written != c.taken || refused == c.taken || named == c.taken {
				t.Fatalf("research model %s: written into lite.md %v, refused in errors.log %v, named by the doctor %v; want it taken %v\nerrors.log: %s", bedrockProfileARN, written, refused, named, c.taken, loggedErrors())
			}
			if !c.taken {
				cliUnchanged(t, lite, before)
			}
		})
	}
}

func TestSetupTakesAGatewayModelNameOnlyForAnAccountThatUsesAGateway(t *testing.T) {
	t.Run("bedrock in settings.json", func(t *testing.T) {
		box := newCLIBox(t)
		cliWrite(t, filepath.Join(box.account, "settings.json"), []byte(`{"model": "sonnet", "env": {"CLAUDE_CODE_USE_BEDROCK": "1"}}`))

		run := box.run(t, "setup", "--config-dir", box.account, "--no-ask", "--code", "my-gateway-model:max", "--digest", "prod-haiku-deployment")

		roles := section(readJSON(filepath.Join(box.account, pluginName, "config.json")), "roles")
		if run.code != 0 || strings.Contains(run.stdout+run.stderr, "not a model Claude Code knows") || getString(getMap(roles, "code"), "model") != "my-gateway-model" || getString(getMap(roles, "digest"), "model") != "prod-haiku-deployment" {
			t.Fatalf("setup refused a deployment name for an account on Bedrock (roles %v):\n%s", roles, run)
		}
	})
	t.Run("a gateway in the environment", func(t *testing.T) {
		box := newCLIBox(t)
		box.env["ANTHROPIC_BASE_URL"] = "http://127.0.0.1:4000"

		run := box.run(t, "setup", "--config-dir", box.account, "--no-ask", "--code", "my-gateway-model")

		if run.code != 0 || getString(getMap(section(readJSON(filepath.Join(box.account, pluginName, "config.json")), "roles"), "code"), "model") != "my-gateway-model" {
			t.Fatalf("setup refused a gateway's model name while ANTHROPIC_BASE_URL points at the gateway:\n%s", run)
		}
	})
	t.Run("anthropic's own API", func(t *testing.T) {
		box := newCLIBox(t)
		box.env["ANTHROPIC_BASE_URL"] = "https://api.anthropic.com"

		run := box.run(t, "setup", "--config-dir", box.account, "--no-ask", "--code", "my-gateway-model")

		if run.code != 1 || !strings.Contains(run.stderr, "my-gateway-model") {
			t.Fatalf("want exit 1 naming the model Anthropic's API does not serve:\n%s", run)
		}
		box.untouched(t, run)
	})
	t.Run("one account on bedrock and one on anthropic's own API", func(t *testing.T) {
		box := newCLIBox(t)
		bedrock := t.TempDir()
		bedrockSettings := []byte(`{"model": "sonnet", "env": {"CLAUDE_CODE_USE_BEDROCK": "1"}}`)
		cliWrite(t, filepath.Join(bedrock, "settings.json"), bedrockSettings)

		run := box.run(t, "setup", "--config-dir", bedrock, "--config-dir", box.account, "--no-ask", "--code", "my-gateway-model")

		if run.code != 1 || !strings.Contains(run.stderr, "my-gateway-model") {
			t.Fatalf("want exit 1 naming the model the second account's Anthropic API does not serve:\n%s", run)
		}
		cliUnchanged(t, filepath.Join(bedrock, "settings.json"), bedrockSettings)
		if statSafe(filepath.Join(bedrock, pluginName)) != nil {
			t.Fatalf("the Bedrock account was set up although setup stopped:\n%s", run)
		}
		box.untouched(t, run)
	})
}

func TestEverySwitchThatTakesClaudeCodeOffAnthropicsAPICounts(t *testing.T) {
	for _, name := range claudeProviderEnv {
		t.Setenv(name, "")
	}
	for _, name := range claudeProviderEnv[1:] {
		for _, value := range []string{"1", "true", "YES", " on "} {
			if !providerModels(object{"env": object{name: value}}) {
				t.Errorf("%s=%q in settings.json does not count as another provider", name, value)
			}
		}
		for _, value := range []string{"", "0", "false", "no", "off", "bedrock"} {
			if providerModels(object{"env": object{name: value}}) {
				t.Errorf("%s=%q in settings.json counts as another provider", name, value)
			}
		}
		t.Setenv(name, "1")
		if !providerModels(nil) {
			t.Errorf("%s=1 in the environment does not count as another provider", name)
		}
		t.Setenv(name, "")
	}
	for address, other := range map[string]bool{"https://api.anthropic.com": false, "https://anthropic.com/": false, "https://API.Anthropic.com:443/v1": false, "http://127.0.0.1:4000": true, "https://gateway.example.com": true, "https://anthropic.com.example.net": true, "https://notanthropic.com": true} {
		if got := providerModels(object{"env": object{"ANTHROPIC_BASE_URL": address}}); got != other {
			t.Errorf("ANTHROPIC_BASE_URL=%s counts as another provider: %v, want %v", address, got, other)
		}
	}
	if providerModels(object{}) || providerModels(nil) {
		t.Error("an account with no provider settings counts as another provider")
	}
}
