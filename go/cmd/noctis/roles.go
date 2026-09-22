package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var roleNames = []string{"code", "research", "planning", "digest", "explore", "fallback"}

var validEfforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}

var effortlessRoles = map[string]bool{"planning": true, "explore": true}

var frontmatterLine = lazyRegexp(`(?m)^(model|effort):[^\n]*\n`)

var roleProfiles = map[string]object{
	"noctis": {
		"code":     object{"model": "fable", "effort": "max"},
		"research": object{"model": "opus", "effort": "xhigh"},
		"planning": object{"model": "fable"},
		"digest":   object{"model": "haiku", "effort": "high"},
		"explore":  object{"model": "haiku"},
		"fallback": object{"model": "opus", "effort": "max"},
	},
	"balanced": {
		"code":     object{"model": "opus", "effort": "high"},
		"research": object{"model": "sonnet", "effort": "high"},
		"planning": object{"model": "opus"},
		"digest":   object{"model": "haiku", "effort": "medium"},
		"explore":  object{"model": "haiku"},
		"fallback": object{"model": "sonnet"},
	},
	"economy": {
		"code":     object{"model": "sonnet", "effort": "high"},
		"research": object{"model": "haiku", "effort": "high"},
		"planning": object{"model": "opus"},
		"digest":   object{"model": "haiku", "effort": "low"},
		"explore":  object{"model": "haiku"},
		"fallback": object{"model": "haiku"},
	},
}

func parseRoleFlag(role, value string) (object, error) {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	model := strings.TrimSpace(parts[0])
	if model == "" {
		return nil, errors.New(T("roles.badValue", role, value))
	}
	spec := object{"model": model}
	if len(parts) == 2 {
		effort := strings.ToLower(strings.TrimSpace(parts[1]))
		if !validEfforts[effort] {
			return nil, errors.New(T("roles.badEffort", role, parts[1]))
		}
		if effortlessRoles[role] {
			fmt.Println("  " + T("roles.effortIgnored", T("roles."+role), effort))
		} else {
			spec["effort"] = effort
		}
	}
	return spec, nil
}

func rolesFromArgs(current object) (object, bool, error) {
	roles := cloneObject(current)
	given := false
	if profile := profileAlias(strings.ToLower(flagString("profile"))); profile != "" {
		base, known := roleProfiles[profile]
		if !known {
			return nil, false, errors.New(T("roles.unknownProfile", profile))
		}
		roles = cloneObject(base)
		roles["profile"] = profile
		given = true
	}
	for _, role := range roleNames {
		value := flagString(role)
		if value == "" {
			continue
		}
		spec, err := parseRoleFlag(role, value)
		if err != nil {
			return nil, false, err
		}
		roles[role] = spec
		roles["profile"] = "custom"
		given = true
	}
	return roles, given, nil
}

func askRoles(current object) object {
	reader := bufio.NewReader(os.Stdin)
	ask := func(question, fallback string) string {
		fmt.Printf("%s [%s]: ", question, fallback)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return fallback
		}
		return line
	}
	fmt.Println(T("roles.intro"))
	choice := profileAlias(strings.ToLower(ask(T("roles.profileQuestion"), "noctis")))
	if base, known := roleProfiles[choice]; known {
		roles := cloneObject(base)
		roles["profile"] = choice
		return roles
	}
	roles := cloneObject(current)
	roles["profile"] = "custom"
	for _, role := range roleNames {
		spec := getMap(roles, role)
		fallback := getString(spec, "model")
		if effort := getString(spec, "effort"); effort != "" {
			fallback += ":" + effort
		}
		for {
			answer := ask(T("roles.roleQuestion", T("roles."+role)), fallback)
			parsed, err := parseRoleFlag(role, answer)
			if err == nil {
				roles[role] = parsed
				break
			}
			fmt.Println("  " + err.Error())
		}
	}
	return roles
}

func derivedRoles(config object, defaults object) object {
	roles := cloneObject(defaults)
	models := section(config, "models")
	if primary := getString(models, "primary"); primary != "" {
		roles["code"] = object{"model": primary, "effort": orDefault(getString(models, "effort"), getString(getMap(roles, "code"), "effort"))}
		roles["planning"] = object{"model": primary, "effort": orDefault(getString(models, "effort"), getString(getMap(roles, "planning"), "effort"))}
	}
	if fallback := getString(models, "fallback"); fallback != "" {
		roles["fallback"] = object{"model": fallback}
	}
	pinned := getMap(section(config, "router"), "subagentModels")
	if explore := getString(pinned, "Explore"); explore != "" {
		roles["explore"] = object{"model": explore}
	}
	if plan := getString(pinned, "Plan"); plan != "" {
		roles["planning"] = object{"model": plan}
	}
	roles["profile"] = "custom"
	for name, profile := range roleProfiles {
		if sameRoles(profile, roles) {
			roles["profile"] = name
		}
	}
	return roles
}

func sameRoles(a, b object) bool {
	for _, role := range roleNames {
		left, right := getMap(a, role), getMap(b, role)
		if getString(left, "model") != getString(right, "model") || getString(left, "effort") != getString(right, "effort") {
			return false
		}
	}
	return true
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func applyRoles(configFile string, config object, roles object) {
	config["roles"] = roles
	models := section(config, "models")
	if code := getMap(roles, "code"); code != nil {
		models["primary"] = orDefault(getString(code, "model"), getString(models, "primary"))
		models["effort"] = orDefault(getString(code, "effort"), getString(models, "effort"))
	}
	if fallback := getMap(roles, "fallback"); fallback != nil {
		models["fallback"] = orDefault(getString(fallback, "model"), getString(models, "fallback"))
	}
	if family := modelFamily(getString(models, "primary")); family != "" {

		models["scopedPattern"] = family
		models["scopedLabel"] = strings.ToUpper(family[:1]) + family[1:]
	}
	config["models"] = models
	router := section(config, "router")
	pinned := getMap(router, "subagentModels")
	if pinned == nil {
		pinned = object{}
	}
	if explore := getMap(roles, "explore"); explore != nil && getString(explore, "model") != "" {
		pinned["Explore"] = getString(explore, "model")
	}
	if planning := getMap(roles, "planning"); planning != nil && getString(planning, "model") != "" {
		pinned["Plan"] = getString(planning, "model")
	}
	router["subagentModels"] = pinned
	config["router"] = router
	mustWriteJSON(configFile, config)
}

func modelFamily(model string) string {
	lower := strings.ToLower(model)
	for _, family := range []string{"mythos", "fable", "opus", "sonnet", "haiku"} {
		if strings.Contains(lower, family) {
			return family
		}
	}
	return ""
}

func describeRoles(roles object) string {
	parts := []string{}
	for _, role := range roleNames {
		spec := getMap(roles, role)
		if spec == nil {
			continue
		}
		text := getString(spec, "model")
		if effort := getString(spec, "effort"); effort != "" {
			text += "/" + effort
		}
		parts = append(parts, T("roles."+role)+"="+text)
	}
	return orDefault(getString(roles, "profile"), "custom") + ": " + strings.Join(parts, " · ")
}

func syncAgentFiles(pluginRoot string, roles object) int {
	changed := 0
	for role, file := range map[string]string{"research": "lite.md", "digest": "digest.md"} {
		spec := getMap(roles, role)
		if spec == nil || getString(spec, "model") == "" {
			continue
		}
		target := filepath.Join(pluginRoot, "agents", file)
		content, err := os.ReadFile(target)
		if err != nil {
			continue
		}
		text := string(content)
		if !strings.HasPrefix(text, "---\n") {
			continue
		}
		end := strings.Index(text[4:], "\n---")
		if end < 0 {
			continue
		}
		front := text[:4+end+1]
		rest := text[4+end+1:]
		stripped := frontmatterLine.ReplaceAllString(front, "")
		lines := "model: " + getString(spec, "model") + "\n"
		if effort := getString(spec, "effort"); effort != "" {
			lines += "effort: " + effort + "\n"
		}
		next := strings.TrimSuffix(stripped, "\n") + "\n" + lines + rest
		if next == text {
			continue
		}
		if err := os.WriteFile(target, []byte(next), 0o644); err == nil {
			changed++
		}
	}
	return changed
}

func profileAlias(name string) string {
	switch strings.TrimSpace(name) {
	case "synex", "1", "noctis mode", "noctis-mode":
		return "noctis"
	case "2":
		return "balanced"
	case "3":
		return "economy"
	case "4":
		return "custom"
	}
	return strings.TrimSpace(name)
}
