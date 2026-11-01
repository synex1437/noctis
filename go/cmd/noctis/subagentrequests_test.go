package main

import (
	"strings"
	"testing"
)

func TestTheShippedConfigAsksForNoSubagentOfNoctisItself(t *testing.T) {
	leanSandbox(t)
	shipped := shippedDefaults(t)
	if getBool(getMap(shipped, "router"), "digest", true) {
		t.Error("config.default.json sends long test runs to the digest agent")
	}
	if got := getString(getMap(shipped, "queue"), "escalate"); got != "off" {
		t.Errorf("config.default.json hands a stuck queue item to a stronger model's subagent (queue.escalate %q)", got)
	}
	if digestEnabled(object{}) {
		t.Error("a config without router.digest sends long test runs to the digest agent")
	}
	if model, agent := escalationTarget(object{}, object{}, "sr1", ""); model != "" || agent != "" {
		t.Errorf("a config without queue.escalate hands a stuck item to %q through %q", model, agent)
	}
	if comment := getString(getMap(getMap(shipped, "queue"), "github"), "comment"); comment != issueClosingComment || strings.Contains(strings.ToLower(comment), "claude") {
		t.Errorf("config.default.json closes an issue with %q", comment)
	}
}

func TestAConfigNoctisWroteBeforeStopsAskingForSubagentsOnce(t *testing.T) {
	leanSandbox(t)
	mustWriteJSON(files.config, object{
		configVersionKey: float64(3),
		"router":         object{"enabled": true, "digest": true},
		"queue":          object{"escalate": "auto", "maxEscalationsPerDay": float64(5), "github": object{"closeOnDone": true, "comment": formerIssueComment}},
	})
	cfg := loadConfig()
	if digestEnabled(cfg) || getString(section(cfg, "queue"), "escalate") != "off" {
		t.Fatalf("a config.json noctis wrote before 8.7.2 still asks for subagents: router %v, queue %v", section(cfg, "router"), section(cfg, "queue"))
	}
	if !getBool(section(cfg, "router"), "enabled", false) {
		t.Fatal("the move turned off a router the user turned on")
	}
	if _, _, err := mergeConfig(files.config, shippedDefaults(t)); err != nil {
		t.Fatal(err)
	}
	stored := readJSON(files.config)
	router, queue := getMap(stored, "router"), getMap(stored, "queue")
	if getBool(router, "digest", true) || getString(queue, "escalate") != "off" || numberOr(stored, configVersionKey, 0) != configVersion {
		t.Fatalf("the merge left router %v, queue %v and %s %v in config.json", router, queue, configVersionKey, stored[configVersionKey])
	}
	if github := getMap(queue, "github"); getString(github, "comment") != issueClosingComment || !getBool(github, "closeOnDone", false) {
		t.Fatalf("the merge left queue.github at %v", github)
	}
	if numberOr(queue, "maxEscalationsPerDay", -1) != 5 {
		t.Fatalf("the move changed maxEscalationsPerDay: %v", queue)
	}

	router["digest"], queue["escalate"] = true, "auto"
	mustWriteJSON(files.config, stored)
	if _, _, err := mergeConfig(files.config, shippedDefaults(t)); err != nil {
		t.Fatal(err)
	}
	cfg = loadConfig()
	if !digestEnabled(cfg) || getString(section(cfg, "queue"), "escalate") != "auto" {
		t.Fatalf("a choice made again after the move did not stay: router %v, queue %v", section(cfg, "router"), section(cfg, "queue"))
	}
}

func TestOwnSettingsStayThroughTheMove(t *testing.T) {
	leanSandbox(t)
	mustWriteJSON(files.config, object{
		configVersionKey: float64(3),
		"router":         object{"digest": false},
		"queue":          object{"escalate": "opus", "github": object{"comment": "Closed from the checklist."}},
	})
	cfg := loadConfig()
	queue := section(cfg, "queue")
	if digestEnabled(cfg) || getString(queue, "escalate") != "opus" || getString(getMap(queue, "github"), "comment") != "Closed from the checklist." {
		t.Fatalf("the move changed settings noctis never wrote: router %v, queue %v", section(cfg, "router"), queue)
	}
}
