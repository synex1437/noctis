package main

import (
	"bufio"
	"path/filepath"
	"strings"
	"testing"
)

const routedResearchPrompt = "En iyi mekanik klavye 2026 araştır"

func TestTheShippedConfigLeavesTheRouterOff(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	cfg := loadConfig()
	if got := classifyPrompt(cfg, nil, routedResearchPrompt, "", nowSec()); got.route || got.reason != "router-off" {
		t.Fatalf("with the shipped config the research prompt %q got %+v; the router stays off until setup turns it on", routedResearchPrompt, got)
	}
	if got := classifyPrompt(object{}, nil, routedResearchPrompt, "", nowSec()); got.route {
		t.Fatalf("a config without a router section routed %q: %+v", routedResearchPrompt, got)
	}
	if status := describeState(cfg, object{}, usageView{}, nowSec()); !strings.Contains(status, T("status.router", T("status.routerOff"))) {
		t.Fatalf("status does not say that the router is off:\n%s", status)
	}
	on := object{"router": object{"enabled": true}}
	if got := classifyPrompt(on, nil, routedResearchPrompt, "", nowSec()); !got.route {
		t.Fatalf("with router.enabled true the research prompt %q was not routed: %+v", routedResearchPrompt, got)
	}
}

func TestSetupRouterOnOrOffSwitchesTheRouter(t *testing.T) {
	sandboxFiles(t)
	previous := args
	t.Cleanup(func() { args = previous })
	for _, tc := range []struct {
		flag string
		want bool
	}{{"on", true}, {"off", false}, {"ON", true}} {
		configFile := filepath.Join(t.TempDir(), "config.json")
		config := cloneObject(shippedDefaults(t))
		section(config, "router")["enabled"] = !tc.want
		args = parseArgs([]string{"setup", "--router", tc.flag})
		if problems := argProblems(setupFlags); len(problems) > 0 {
			t.Fatalf("setup --router %s was refused: %q", tc.flag, problems)
		}
		output := capturedStdout(t, func() { configureRouter(configFile, config) })
		if got, ok := section(readJSON(configFile), "router")["enabled"].(bool); !ok || got != tc.want {
			t.Errorf("setup --router %s left router.enabled at %v in config.json, want %v", tc.flag, section(readJSON(configFile), "router")["enabled"], tc.want)
		}
		key := "setup.routerOff"
		if tc.want {
			key = "setup.routerOn"
		}
		if want := T(key, liteAgentType(config)); !strings.Contains(output, want) {
			t.Errorf("setup --router %s printed %q, want the line %q", tc.flag, output, want)
		}
	}
	args = parseArgs([]string{"setup", "--router", "maybe"})
	if problems := strings.Join(argProblems(setupFlags), "\n"); !strings.Contains(problems, T("args.badValue", "--router", "maybe", "on, off")) {
		t.Fatalf("setup --router maybe was not refused as a value outside on and off: %q", problems)
	}
}

func TestSetupWithoutRouterFlagLeavesTheRouterAsItIsWhenItCannotAsk(t *testing.T) {
	sandboxFiles(t)
	previous := args
	t.Cleanup(func() { args = previous })
	for _, argv := range [][]string{{"setup"}, {"setup", "--no-ask"}} {
		for _, current := range []bool{true, false} {
			configFile := filepath.Join(t.TempDir(), "config.json")
			config := cloneObject(shippedDefaults(t))
			section(config, "router")["enabled"] = current
			args = parseArgs(argv)
			capturedStdout(t, func() { configureRouter(configFile, config) })
			if got := getBool(section(readJSON(configFile), "router"), "enabled", !current); got != current {
				t.Errorf("%s with no terminal to ask turned the router from %v to %v", strings.Join(argv, " "), current, got)
			}
		}
	}
}

func TestTheRouterQuestionTakesOnOrOffAndEnterKeepsTheCurrentSetting(t *testing.T) {
	for _, tc := range []struct {
		input         string
		current, want bool
	}{
		{"\n", false, false},
		{"\n", true, true},
		{"on\n", false, true},
		{" OFF \n", true, false},
		{"maybe\non\n", false, true},
		{"", true, true},
		{"on", false, true},
	} {
		var got bool
		output := capturedStdout(t, func() { got = askRouter(bufio.NewReader(strings.NewReader(tc.input)), tc.current, "noctis:lite") })
		if got != tc.want {
			t.Errorf("answer %q with the router %v: got %v, want %v", tc.input, tc.current, got, tc.want)
		}
		if !strings.Contains(output, "noctis:lite") {
			t.Errorf("the question does not name the agent the prompts would go to: %q", output)
		}
		if strings.HasPrefix(tc.input, "maybe") && !strings.Contains(output, T("setup.routerBadAnswer", "maybe")) {
			t.Errorf("the answer maybe was not named before asking again: %q", output)
		}
	}
}
