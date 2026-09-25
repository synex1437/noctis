package main

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var roleNames = []string{"code", "research", "planning", "digest", "explore", "fallback"}

var validEfforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}

var effortlessRoles = map[string]bool{"planning": true, "explore": true}

var roleModelPattern = lazyRegexp(`^[A-Za-z0-9._:@/\[\]-]+$`)

var frontmatterLine = lazyRegexp(`(?m)^(model|effort):[^\n]*\n`)

var familyFreeModels = map[string]bool{"best": true, "default": true, "inherit": true}

func knownModelName(model string) bool {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(model)), "[1m]")
	return modelFamily(name) != "" || familyFreeModels[name] || strings.HasPrefix(name, "claude-")
}

var providerSwitches = []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_USE_ANTHROPIC_AWS", "CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD", "CLAUDE_CODE_USE_MANTLE", "CLAUDE_CODE_USE_GATEWAY"}

func providerModels(settings object) bool {
	env := getMap(settings, "env")
	for _, lookup := range []func(string) string{os.Getenv, func(name string) string { return getString(env, name) }} {
		for _, name := range providerSwitches {
			switch strings.ToLower(strings.TrimSpace(lookup(name))) {
			case "1", "true", "yes", "on":
				return true
			}
		}
		if base := strings.TrimSpace(lookup("ANTHROPIC_BASE_URL")); base != "" && !anthropicHost(base) {
			return true
		}
	}
	return false
}

func anthropicHost(address string) bool {
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "anthropic.com" || strings.HasSuffix(host, ".anthropic.com")
}

var roleProfiles = map[string]object{
	"code": {
		"code":     object{"model": "opus", "effort": "xhigh"},
		"research": object{"model": "opus", "effort": "high"},
		"planning": object{"model": "opus"},
		"digest":   object{"model": "haiku"},
		"explore":  object{"model": "haiku"},
		"fallback": object{"model": "opus", "effort": "xhigh"},
	},
	"search": {
		"code":     object{"model": "opus", "effort": "xhigh"},
		"research": object{"model": "opus", "effort": "xhigh"},
		"planning": object{"model": "opus"},
		"digest":   object{"model": "haiku"},
		"explore":  object{"model": "haiku"},
		"fallback": object{"model": "opus", "effort": "xhigh"},
	},
	"balanced": {
		"code":     object{"model": "opus", "effort": "high"},
		"research": object{"model": "opus", "effort": "medium"},
		"planning": object{"model": "opus"},
		"digest":   object{"model": "haiku"},
		"explore":  object{"model": "haiku"},
		"fallback": object{"model": "opus", "effort": "high"},
	},
	"synex": {
		"code":     object{"model": "opus", "effort": "max"},
		"research": object{"model": "opus", "effort": "xhigh"},
		"planning": object{"model": "opus"},
		"digest":   object{"model": "haiku"},
		"explore":  object{"model": "haiku"},
		"fallback": object{"model": "opus", "effort": "max"},
	},
}

var profileTitles = map[string]string{"code": "Code", "search": "Search", "balanced": "Balanced", "synex": "SYNEX"}

var retiredProfiles = map[string]object{
	"synex": {
		"code":     object{"model": "fable", "effort": "max"},
		"research": object{"model": "opus", "effort": "xhigh"},
		"planning": object{"model": "fable"},
		"digest":   object{"model": "haiku"},
		"explore":  object{"model": "haiku"},
		"fallback": object{"model": "opus", "effort": "max"},
	},
	"balanced": {
		"code":     object{"model": "opus", "effort": "high"},
		"research": object{"model": "sonnet", "effort": "high"},
		"planning": object{"model": "opus"},
		"digest":   object{"model": "haiku"},
		"explore":  object{"model": "haiku"},
		"fallback": object{"model": "sonnet"},
	},
}

func modelTakesEffort(model string) bool {
	return modelFamily(model) != "haiku"
}

func appliedEffort(role string, spec object) string {
	if effortlessRoles[role] || !modelTakesEffort(getString(spec, "model")) {
		return ""
	}
	return getString(spec, "effort")
}

func roleFlagError(role, value string, anyModel bool) error {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	if strings.TrimSpace(parts[0]) == "" {
		return errors.New(T("roles.badValue", role, value))
	}
	if model := strings.TrimSpace(parts[0]); !roleModelPattern.MatchString(model) {
		return errors.New(T("roles.badModel", role, strconv.Quote(model)))
	} else if !anyModel && !knownModelName(model) {
		return errors.New(T("roles.unknownModel", role, strconv.Quote(model)))
	}
	if len(parts) == 2 && !validEfforts[strings.ToLower(strings.TrimSpace(parts[1]))] {
		return errors.New(T("roles.badEffort", role, parts[1]))
	}
	return nil
}

func parseRoleFlag(role, value string, anyModel bool) (object, error) {
	if err := roleFlagError(role, value, anyModel); err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	model := strings.TrimSpace(parts[0])
	spec := object{"model": model}
	if len(parts) == 2 {
		effort := strings.ToLower(strings.TrimSpace(parts[1]))
		switch {
		case effortlessRoles[role]:
			fmt.Println("  " + T("roles.effortIgnored", T("roles."+role), effort))
		case !modelTakesEffort(model):
			fmt.Println("  " + T("roles.modelNoEffort", T("roles."+role), model, effort))
		default:
			spec["effort"] = effort
		}
	}
	return spec, nil
}

func rolesFromArgs(current object, anyModel bool) (object, bool, error) {
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
		spec, err := parseRoleFlag(role, value, anyModel)
		if err != nil {
			return nil, false, err
		}
		roles[role] = spec
		roles["profile"] = "custom"
		given = true
	}
	return roles, given, nil
}

func askRoles(current object, anyModel bool) object {
	reader := bufio.NewReader(os.Stdin)
	ended := false
	ask := func(question, fallback string) string {
		if ended {
			return fallback
		}
		fmt.Printf("%s [%s]: ", question, fallback)
		line, err := reader.ReadString('\n')
		if err != nil {
			ended = true
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return fallback
		}
		return line
	}
	fmt.Println(T("roles.intro"))
	choice := profileAlias(strings.ToLower(ask(T("roles.profileQuestion"), profileTitles["synex"])))
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
			parsed, err := parseRoleFlag(role, answer, anyModel)
			if err == nil {
				roles[role] = parsed
				break
			}
			fmt.Println("  " + err.Error())
			if ended {
				break
			}
		}
	}
	return roles
}

func derivedRoles(config object, defaults object) object {
	roles := cloneObject(defaults)
	models := section(config, "models")
	if primary := getString(models, "primary"); primary != "" {
		roles["code"] = object{"model": primary, "effort": orDefault(getString(models, "effort"), getString(getMap(roles, "code"), "effort"))}
		roles["planning"] = object{"model": primary}
	}
	if fallback := getString(models, "fallback"); fallback != "" {
		roles["fallback"] = withEffort(fallback, getString(getMap(roles, "fallback"), "effort"))
	}
	pinned := getMap(section(config, "router"), "subagentModels")
	if explore := getString(pinned, "Explore"); explore != "" {
		roles["explore"] = object{"model": explore}
	}
	if plan := getString(pinned, "Plan"); plan != "" {
		roles["planning"] = object{"model": plan}
	}
	for role := range effortlessRoles {
		if spec := getMap(roles, role); spec != nil {
			delete(spec, "effort")
		}
	}
	roles["profile"] = "custom"
	for name, profile := range roleProfiles {
		if sameRoles(profile, roles) {
			roles["profile"] = name
		}
	}
	return roles
}

func withEffort(model, effort string) object {
	spec := object{"model": model}
	if effort != "" {
		spec["effort"] = effort
	}
	return spec
}

func sameRoles(a, b object) bool {
	for _, role := range roleNames {
		left, right := getMap(a, role), getMap(b, role)
		if getString(left, "model") != getString(right, "model") || appliedEffort(role, left) != appliedEffort(role, right) {
			return false
		}
	}
	return true
}

func retunedProfile(roles object) string {
	name := profileAlias(strings.ToLower(getString(roles, "profile")))
	earlier, known := retiredProfiles[name]
	if !known || !sameRoles(earlier, roles) || sameRoles(roleProfiles[name], roles) {
		return ""
	}
	return name
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
	family := scopedFamily(getString(models, "primary"))
	models["scopedPattern"] = family
	models["scopedLabel"] = strings.ToUpper(family[:1]) + family[1:]
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

func scopedFamily(model string) string {
	if family := modelFamily(model); family == "fable" || family == "mythos" {
		return family
	}
	return "fable"
}

func roleSummary(roles object, label func(string) string) string {
	parts := []string{}
	for _, role := range roleNames {
		spec := getMap(roles, role)
		if spec == nil {
			continue
		}
		text := getString(spec, "model")
		if effort := appliedEffort(role, spec); effort != "" {
			text += "/" + effort
		}
		parts = append(parts, label(role)+"="+text)
	}
	return strings.Join(parts, " · ")
}

func describeRoles(roles object) string {
	return profileTitle(orDefault(getString(roles, "profile"), "custom")) + ": " + roleSummary(roles, func(role string) string { return T("roles." + role) })
}

func retunedNotice(profile string) string {
	return T("roles.retuned", profileTitle(profile), pluginVersion, roleSummary(roleProfiles[profile], func(role string) string { return role }), profile)
}

func badRoleValue(role string, spec object, anyModel bool) string {
	if model := getString(spec, "model"); model != "" && !roleModelPattern.MatchString(model) {
		return T("roles.badModel", role, strconv.Quote(model))
	} else if model != "" && !anyModel && !knownModelName(model) {
		return T("roles.unknownModel", role, strconv.Quote(model))
	}
	if effort := getString(spec, "effort"); effort != "" && !validEfforts[effort] {
		return T("roles.badEffort", role, strings.Trim(strconv.Quote(effort), `"`))
	}
	return ""
}

func badRoleValues(roles object, anyModel bool) []string {
	problems := []string{}
	for _, role := range roleNames {
		if problem := badRoleValue(role, getMap(roles, role), anyModel); problem != "" {
			problems = append(problems, problem)
		}
	}
	return problems
}

func syncAgentFiles(pluginRoot string, roles object, anyModel bool) int {
	changed := 0
	for role, file := range map[string]string{"research": "lite.md", "digest": "digest.md"} {
		spec := getMap(roles, role)
		if spec == nil || getString(spec, "model") == "" {
			continue
		}
		if problem := badRoleValue(role, spec, anyModel); problem != "" {
			warn("agents/%s left unchanged: %s", file, problem)
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
		if effort := appliedEffort(role, spec); effort != "" {
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
	case "1":
		return "code"
	case "2":
		return "search"
	case "3":
		return "balanced"
	case "4", "noctis", "noctis mode", "noctis-mode":
		return "synex"
	case "5":
		return "custom"
	}
	return strings.TrimSpace(name)
}

func profileTitle(name string) string {
	name = profileAlias(strings.ToLower(name))
	return orDefault(profileTitles[name], name)
}
