package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var keepModelPattern = lazyRegexp(`(?i)fable|opus`)

var thresholdPresets = map[string]object{
	"conservative": {"session5h": float64(85), "weeklyAll": float64(82), "weeklyFable": float64(90)},
	"balanced":     {"session5h": float64(92), "weeklyAll": float64(89), "weeklyFable": float64(95)},
	"aggressive":   {"session5h": float64(96), "weeklyAll": float64(94), "weeklyFable": float64(98)},
}

func binaryFileName() string {
	if isWindows {
		return "noctis.exe"
	}
	return "noctis"
}

func healTargetBinary() string {
	if files.pluginRoot != "" {
		candidate := filepath.Join(files.pluginRoot, "bin", binaryFileName())
		if statSafe(candidate) != nil {
			return candidate
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return executable
}

func platformBinary(sourceRoot string) string {
	return filepath.Join(sourceRoot, "bin", runtime.GOOS+"-"+runtime.GOARCH, binaryFileName())
}

func installedPaths() (files []string, dirs []string) {
	platform := filepath.Join("bin", runtime.GOOS+"-"+runtime.GOARCH, binaryFileName())
	return []string{
			platform,
			filepath.Join("bin", "SHA256SUMS"),
			filepath.Join("bin", "noctis"),
			filepath.Join("hooks", "hooks.json"),
			filepath.Join("scripts", "notify.ps1"),
			filepath.Join("scripts", "launch.ps1"),
			"config.default.json",
			filepath.Join(".claude-plugin", "plugin.json"),
			"LICENSE",
			"README.md",
		}, []string{
			"agents",
			"skills",
		}
}

func copyPluginTree(from, to string) error {
	wanted, dirs := installedPaths()
	for _, relative := range wanted {
		source := filepath.Join(from, relative)
		if statSafe(source) == nil {
			continue
		}
		if err := copyOneFile(source, filepath.Join(to, relative)); err != nil {
			return err
		}
	}
	for _, dir := range dirs {
		source := filepath.Join(from, dir)
		if statSafe(source) == nil {
			continue
		}
		if err := copyTree(source, filepath.Join(to, dir)); err != nil {
			return err
		}
	}
	return nil
}

func copyOneFile(source, target string) error {
	info, statErr := os.Stat(source)
	if statErr != nil {
		return statErr
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	in, openErr := os.Open(source)
	if openErr != nil {
		return openErr
	}
	defer in.Close()
	out, createErr := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm()|0o600)
	if createErr != nil {
		return createErr
	}
	defer out.Close()
	_, copyErr := io.Copy(out, in)
	return copyErr
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(source string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(from, source)
		target := filepath.Join(to, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		in, openErr := os.Open(source)
		if openErr != nil {
			return openErr
		}
		defer in.Close()
		out, createErr := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm()|0o600)
		if createErr != nil {
			return createErr
		}
		defer out.Close()
		_, copyErr := io.Copy(out, in)
		return copyErr
	})
}

func isInside(child, parent string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func backupFile(file string) string {
	if statSafe(file) == nil {
		return ""
	}
	backup := fmt.Sprintf("%s.bak-%s", file, strings.NewReplacer(":", "-", ".", "-").Replace(time.Now().UTC().Format("2006-01-02T15:04:05.000Z")))
	content, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	if err := os.WriteFile(backup, content, 0o600); err != nil {
		return ""
	}
	return backup
}

func patchHooks(installRoot, binary string) error {
	hooksFile := filepath.Join(installRoot, "hooks", "hooks.json")
	hooks := readJSONStrict(hooksFile)
	if !hooks.ok || hooks.data == nil {
		return errors.New(T("install.hooksBroken", orDefault(hooks.err, T("install.invalid"))))
	}
	for _, raw := range getMap(hooks.data, "hooks") {
		groups, _ := raw.([]any)
		for _, rawGroup := range groups {
			for _, rawHook := range getList(toObject(rawGroup), "hooks") {
				hook := toObject(rawHook)
				if getString(hook, "type") == "command" && getList(hook, "args") != nil {
					hook["command"] = forwardSlashes(binary)
				}
			}
		}
	}
	return writeJSONAtomic(hooksFile, hooks.data)
}

func mergeConfig(configFile string, defaults object) (object, bool, map[string]bool) {
	existing := readJSONStrict(configFile)
	if !existing.ok {
		fmt.Println(T("install.configSkipped", configFile, existing.err))
		return nil, false, nil
	}
	current := existing.data
	if current == nil {
		current = object{}
	}
	added := map[string]bool{}
	merged := object{}
	for name, base := range defaults {
		own := current[name]
		if own == nil {
			added[name] = true
		}
		if baseMap, ok := base.(object); ok {
			combined := cloneObject(baseMap)
			if ownMap, ok := own.(object); ok {
				mergeInto(combined, ownMap)
			}
			merged[name] = combined
			continue
		}
		if own != nil {
			merged[name] = own
		} else {
			merged[name] = base
		}
	}
	for name, own := range current {
		if _, exists := merged[name]; !exists {
			merged[name] = own
		}
	}
	mustWriteJSON(configFile, merged)
	return merged, true, added
}

func placeBinary(pluginRoot string) (string, error) {
	binary := filepath.Join(pluginRoot, "bin", binaryFileName())
	os.Remove(binary + ".old")
	self, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	source := platformBinary(pluginRoot)
	if statSafe(source) == nil {
		source = self
	}
	if same, _ := filepath.Abs(source); same == binary {
		return binary, nil
	}

	if statSafe(filepath.Join(pluginRoot, "go", "go.mod")) != nil {
		return source, nil
	}
	content, err := os.ReadFile(source)
	if err != nil {
		return "", errors.New(T("install.binaryMissing", source))
	}

	if err := verifyShippedBinary(pluginRoot, content); err != nil {
		return "", errors.New(T("install.checksumFailed", err.Error()))
	}
	if existing, readErr := os.ReadFile(binary); readErr == nil && bytes.Equal(existing, content) {
		return binary, nil
	}
	ensureDir(filepath.Dir(binary))
	staging := fmt.Sprintf("%s.%d.tmp", binary, os.Getpid())

	linked := os.Link(source, staging) == nil
	if !linked {
		if err := os.WriteFile(staging, content, 0o755); err != nil {
			return "", fmt.Errorf(T("install.binaryFailed"), err)
		}
	} else if err := os.Chmod(staging, 0o755); err != nil {

		os.Remove(staging)
		if err := os.WriteFile(staging, content, 0o755); err != nil {
			return "", fmt.Errorf(T("install.binaryFailed"), err)
		}
	}
	err = os.Rename(staging, binary)
	if err != nil && isWindows && os.Rename(binary, binary+".old") == nil {
		err = os.Rename(staging, binary)
	}
	if err != nil {
		os.Remove(staging)
		return "", fmt.Errorf(T("install.binaryFailed"), err)
	}
	return binary, nil
}

func firstRunSetup(defaults object) {
	if len(defaults) == 0 {
		return
	}
	fresh := statSafe(files.config) == nil
	if fresh {
		ensureDir(files.guardDir)
		if _, ok, _ := mergeConfig(files.config, defaults); !ok {
			return
		}
	}
	wired := false
	withSettings(func(data object) bool {
		current := getString(getMap(data, "statusLine"), "command")
		if strings.Contains(current, pluginName) || strings.Contains(current, "guard.js") {
			return false
		}
		binary := healTargetBinary()
		if binary == "" {
			return false
		}
		if current != "" {

			config := readJSON(files.config)
			if config == nil {
				config = object{}
			}
			statusline := getMap(config, "statusline")
			if statusline == nil {
				statusline = object{}
				config["statusline"] = statusline
			}
			if getString(statusline, "chainCommand") == "" {
				statusline["chainCommand"] = current
				mustWriteJSON(files.config, config)
			}
		}
		data["statusLine"] = object{"type": "command", "command": fmt.Sprintf(`"%s" statusline`, forwardSlashes(binary))}
		wired = true
		return true
	})
	if !wired && !fresh {
		return
	}

	if numberOr(getMap(readState(), "notified"), "firstRun", 0) == 0 {
		updateState(func(next object) { stateMap(next, "notified")["firstRun"] = float64(nowSec()) })
		logInfo("first run: config written and the status line wired; /noctis:setup configures the rest")
	}
}

func runEnsure() {
	if _, err := placeBinary(files.pluginRoot); err != nil {
		warn("ensure: %v", err)
	}
	firstRunSetup(readJSON(filepath.Join(files.pluginRoot, "config.default.json")))

	if statSafe(files.config) != nil {
		if defaults := readJSON(filepath.Join(files.pluginRoot, "config.default.json")); len(defaults) > 0 {
			if _, ok, added := mergeConfig(files.config, defaults); ok && len(added) > 0 {
				logInfo("ensure: %d new config section(s) added: %s", len(added), strings.Join(sortedKeys(added), ", "))
			}
		}
	}
	if roles := section(loadConfig(), "roles"); len(roles) > 0 {
		if changed := syncAgentFiles(files.pluginRoot, roles); changed > 0 {
			logInfo("ensure: %d agent file(s) synced with the roles profile", changed)
		}
	}
	healStatusLine()
}

func healStatusLine() {
	from, to := "", ""

	if withSettings(func(data object) bool {
		line := getMap(data, "statusLine")
		command := getString(line, "command")
		if command == "" || !strings.Contains(command, pluginName) {
			return false
		}
		current := command
		if strings.HasPrefix(command, `"`) {
			if end := strings.Index(command[1:], `"`); end >= 0 {
				current = command[1 : end+1]
			}
		} else if space := strings.Index(command, " "); space >= 0 {
			current = command[:space]
		}
		binary := healTargetBinary()
		if binary == "" || forwardSlashes(current) == forwardSlashes(binary) {
			return false
		}
		if statSafe(current) != nil && !strings.Contains(forwardSlashes(current), "/plugins/cache/") {

			return false
		}
		line["command"] = strings.Replace(command, current, forwardSlashes(binary), 1)
		data["statusLine"] = line
		from, to = current, forwardSlashes(binary)
		return true
	}) {
		logInfo("ensure: statusLine re-pointed from %s to %s", from, to)
	}
}

func configureRoles(configFile string, config object, pluginRoot string, rolesAreNew bool) error {
	current := section(config, "roles")
	if rolesAreNew {

		current = derivedRoles(config, current)
	}
	roles, given, err := rolesFromArgs(current)
	if err != nil {
		return err
	}
	if !given && stdinIsTerminal() && !args.present["no-ask"] {
		roles = askRoles(current)
	}
	applyRoles(configFile, config, roles)
	syncAgentFiles(pluginRoot, roles)
	fmt.Println(T("roles.applied", describeRoles(roles)))
	return nil
}

func applyPreset(configFile string, config object, preset string) error {
	if preset == "" {
		return nil
	}
	values, ok := thresholdPresets[strings.ToLower(preset)]
	if !ok {
		return errors.New(T("setup.unknownPreset", preset))
	}
	thresholds := section(config, "thresholds")
	mergeInto(thresholds, values)
	config["thresholds"] = thresholds
	mustWriteJSON(configFile, config)
	fmt.Println(T("setup.preset", strings.ToLower(preset), formatNumber(numberOr(values, "session5h", 0)), formatNumber(numberOr(values, "weeklyAll", 0)), formatNumber(numberOr(values, "weeklyFable", 0))))
	return nil
}

func wireSettings(configDir, binary string, config object, configOk bool, configFile string, defaults object, noModel bool) error {
	settingsFile := filepath.Join(configDir, "settings.json")
	settings := readJSONStrict(settingsFile)
	if !settings.ok {
		return errors.New(T("install.settingsBroken", settings.err))
	}
	data := settings.data
	if data == nil {
		data = object{}
	}
	backup := backupFile(settingsFile)
	previous := getString(getMap(data, "statusLine"), "command")
	if previous != "" && !strings.Contains(previous, "guard.js") && !strings.Contains(previous, "noctis") && configOk && getString(section(config, "statusline"), "chainCommand") == "" {
		section(config, "statusline")["chainCommand"] = previous
		mustWriteJSON(configFile, config)
		fmt.Println(T("install.chained", previous))
	}
	data["statusLine"] = object{"type": "command", "command": fmt.Sprintf(`"%s" statusline`, forwardSlashes(binary))}
	effort := getString(section(defaults, "models"), "effort")
	if configOk {
		effort = orDefault(getString(section(config, "models"), "effort"), effort)
	}
	env := getMap(data, "env")
	if env == nil {
		env = object{}
	}
	if configOk && getString(env, "CLAUDE_CODE_EFFORT_LEVEL") != effort {
		config["managedEffort"] = object{"previous": env["CLAUDE_CODE_EFFORT_LEVEL"], "set": effort}
		mustWriteJSON(configFile, config)
	}
	env["CLAUDE_CODE_EFFORT_LEVEL"] = effort
	data["env"] = env
	if !noModel && !keepModelPattern.MatchString(getString(data, "model")) {
		primary := getString(section(defaults, "models"), "primary")
		if configOk {
			primary = orDefault(getString(section(config, "models"), "primary"), primary)
		}
		if configOk && getString(data, "model") != primary {

			previous, hadModel := data["model"]
			if !hadModel {
				previous = nil
			}
			config["managedModel"] = object{"previous": previous, "set": primary}
			mustWriteJSON(configFile, config)
		}
		data["model"] = primary
	}
	permissionNote := ""
	if mode := managedPermissionMode(config, configOk, configFile); mode != "" {
		permissions := getMap(data, "permissions")
		if permissions == nil {
			permissions = object{}
		}
		if configOk && getString(permissions, "defaultMode") != mode {
			config["managedPermissionPrevious"] = permissions["defaultMode"]
			mustWriteJSON(configFile, config)
		}
		permissions["defaultMode"] = mode
		data["permissions"] = permissions
		permissionNote = T("install.permissions", mode)
	}
	mustWriteJSON(settingsFile, data)
	if permissionNote != "" {
		fmt.Println(permissionNote)
	}
	backupText := ""
	if backup != "" {
		backupText = T("install.backup", filepath.Base(backup))
	}
	fmt.Println(T("install.settings", backupText, effort, getString(data, "model")))
	return nil
}

func managedPermissionMode(config object, configOk bool, configFile string) string {
	choice := strings.ToLower(flagString("permissions"))
	if choice == "keep" {
		return ""
	}
	if choice == "" {
		choice = "auto"
	}
	probe := object{"resume": object{"permissionMode": choice}}
	mode := supportedPermissionMode(probe, claudeExecutable(), "")
	if configOk {
		config["managedPermissionMode"] = mode
		mustWriteJSON(configFile, config)
	}
	return mode
}

func installInto(configDir, sourceRoot string, noModel bool, defaults object) error {
	fmt.Println(T("install.header", configDir))
	installRoot := filepath.Join(configDir, "skills", pluginName)
	if absInstall, _ := filepath.Abs(installRoot); absInstall != sourceRoot {
		if isInside(sourceRoot, installRoot) || isInside(installRoot, sourceRoot) {
			return errors.New(T("install.nested", sourceRoot, installRoot))
		}
		if err := copyPluginTree(sourceRoot, installRoot); err != nil {
			return fmt.Errorf(T("install.copyFailed"), err)
		}
		fmt.Println(T("install.copied", installRoot))
	}
	binary, err := placeBinary(installRoot)
	if err != nil {
		return err
	}
	if err := patchHooks(installRoot, binary); err != nil {
		return err
	}
	fmt.Println(T("install.enginePath", forwardSlashes(binary)))
	guardDir := filepath.Join(configDir, pluginName)
	ensureDir(guardDir)
	configFile := filepath.Join(guardDir, "config.json")
	config, configOk, added := mergeConfig(configFile, defaults)
	if configOk {
		if err := applyPreset(configFile, config, flagString("preset")); err != nil {
			return err
		}
		if err := configureRoles(configFile, config, installRoot, added["roles"]); err != nil {
			return err
		}
	}
	if err := wireSettings(configDir, binary, config, configOk, configFile, defaults, noModel); err != nil {
		return err
	}
	previousConfig := files.configDir
	files = pathsFor(configDir, installRoot)
	for _, line := range doctorLines(loadConfig()) {
		fmt.Printf("   %s\n", line)
	}
	files = pathsFor(previousConfig, files.pluginRoot)
	return nil
}

func uninstallFrom(configDir string) {
	fmt.Println(T("install.uninstallHeader", configDir))
	installRoot := filepath.Join(configDir, "skills", pluginName)
	if statSafe(installRoot) != nil {
		if err := os.RemoveAll(installRoot); err == nil {
			fmt.Println(T("install.removed", installRoot))
		}
	}
	settingsFile := filepath.Join(configDir, "settings.json")
	settings := readJSONStrict(settingsFile)
	if !settings.ok || settings.data == nil {
		return
	}
	data := settings.data
	chain := getString(section(readJSON(filepath.Join(configDir, pluginName, "config.json")), "statusline"), "chainCommand")
	statusLine := getString(getMap(data, "statusLine"), "command")
	if strings.Contains(statusLine, "guard.js") || strings.Contains(statusLine, "noctis") {
		if chain != "" {
			data["statusLine"] = object{"type": "command", "command": chain}
		} else {
			delete(data, "statusLine")
		}
	}
	guardConfig := readJSON(filepath.Join(configDir, pluginName, "config.json"))
	if env := getMap(data, "env"); env != nil {
		managedEffort := getMap(guardConfig, "managedEffort")
		switch {
		case managedEffort != nil && getString(env, "CLAUDE_CODE_EFFORT_LEVEL") != getString(managedEffort, "set"):

		case managedEffort != nil && getString(managedEffort, "previous") != "":
			env["CLAUDE_CODE_EFFORT_LEVEL"] = getString(managedEffort, "previous")
		default:
			delete(env, "CLAUDE_CODE_EFFORT_LEVEL")
		}
	}
	managed := getString(guardConfig, "managedPermissionMode")
	if permissions := getMap(data, "permissions"); permissions != nil && managed != "" && getString(permissions, "defaultMode") == managed {
		if previous := getString(guardConfig, "managedPermissionPrevious"); previous != "" && previous != managed {
			permissions["defaultMode"] = previous
		} else {
			delete(permissions, "defaultMode")
		}
	}
	modelNote := T("install.modelKept")
	if managedModel := getMap(guardConfig, "managedModel"); managedModel != nil && getString(data, "model") == getString(managedModel, "set") {
		if previous := getString(managedModel, "previous"); previous != "" {
			data["model"] = previous
			modelNote = T("install.modelRestored", previous)
		} else {
			delete(data, "model")
			modelNote = T("install.modelRemoved")
		}
	}
	mustWriteJSON(settingsFile, data)
	fmt.Println(T("install.restored", modelNote))
}

func configTargets() []string {
	targets := []string{}
	for i := 0; i+1 < len(os.Args); i++ {
		if os.Args[i] == "--config-dir" {
			resolved, _ := filepath.Abs(os.Args[i+1])
			targets = append(targets, resolved)
		}
	}
	if len(targets) == 0 {
		if env := os.Getenv("CLAUDE_CONFIG_DIR"); env != "" {
			resolved, _ := filepath.Abs(env)
			targets = append(targets, resolved)
		} else {
			targets = append(targets, filepath.Join(homeDir(), ".claude"))
		}
	}
	return targets
}

func runInstall() {
	sourceRoot := files.pluginRoot
	if flagString("source") != "" {
		sourceRoot, _ = filepath.Abs(flagString("source"))
	}
	defaults := readJSON(filepath.Join(sourceRoot, "config.default.json"))
	if defaults == nil {
		fmt.Fprintln(os.Stderr, T("install.defaultsMissing", sourceRoot))
		os.Exit(1)
	}
	host := chooseHost()
	if host != "claude" {
		for _, configDir := range hostTargets(host) {
			if args.present["uninstall"] {
				uninstallHost(host, configDir)
				continue
			}
			if err := installHost(host, configDir, sourceRoot, defaults); err != nil {
				fmt.Fprintf(os.Stderr, "!! %s\n", err)
				os.Exit(1)
			}
		}
		return
	}
	targets := configTargets()
	for _, configDir := range targets {
		if args.present["uninstall"] {
			uninstallFrom(configDir)
			continue
		}
		if err := installInto(configDir, sourceRoot, args.present["no-model"], defaults); err != nil {
			fmt.Fprintf(os.Stderr, "!! %s\n", err)
			os.Exit(1)
		}
	}
	if writeFailures > 0 {
		fmt.Fprintf(os.Stderr, "!! %s\n", T("install.incomplete", writeFailures, files.errors))
		os.Exit(1)
	}
	if !args.present["uninstall"] {
		fmt.Println(T("install.next"))
		fmt.Println(T("install.next1"))
		fmt.Println(T("install.next2", pluginName, binaryFileName()))
	}
}

func chooseHost() string {
	if host := hostFromArgs(); host != "" {
		return host
	}
	if !stdinIsTerminal() {
		return "claude"
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println(T("host.question"))
	fmt.Println(describeHosts())
	for {
		fmt.Printf("%s [1]: ", T("host.prompt"))
		line, _ := reader.ReadString('\n')
		if host := parseHostChoice(line); host != "" {
			return host
		}
		fmt.Println("  " + T("host.badChoice", strings.TrimSpace(line)))
	}
}

func hostTargets(host string) []string {
	targets := []string{}
	for i := 0; i+1 < len(os.Args); i++ {
		if os.Args[i] == "--config-dir" {
			resolved, _ := filepath.Abs(os.Args[i+1])
			targets = append(targets, resolved)
		}
	}
	if len(targets) == 0 {
		targets = append(targets, hostHome(host))
	}
	return targets
}

func installHost(host, configDir, sourceRoot string, defaults object) error {
	spec := hostOf(host)
	fmt.Println(T("host.header", spec.display, configDir))
	guardDir := filepath.Join(configDir, pluginName)
	installRoot := filepath.Join(guardDir, "plugin")
	if absInstall, _ := filepath.Abs(installRoot); absInstall != sourceRoot {
		if isInside(sourceRoot, installRoot) || isInside(installRoot, sourceRoot) {
			return errors.New(T("install.nested", sourceRoot, installRoot))
		}
		if err := copyPluginTree(sourceRoot, installRoot); err != nil {
			return fmt.Errorf(T("install.copyFailed"), err)
		}
		fmt.Println(T("install.copied", installRoot))
	}
	binary, err := placeBinary(installRoot)
	if err != nil {
		return err
	}
	fmt.Println(T("install.enginePath", forwardSlashes(binary)))
	ensureDir(guardDir)
	configFile := filepath.Join(guardDir, "config.json")
	config, configOk, _ := mergeConfig(configFile, defaults)
	if configOk {
		config["host"] = host
		fable := section(config, "fable")
		switch host {
		case "codex":
			fable["source"] = "codex"
		default:
			fable["source"] = "off"
		}
		config["fable"] = fable
		if err := applyPreset(configFile, config, flagString("preset")); err != nil {
			return err
		}
		mustWriteJSON(configFile, config)
	}
	written, err := wireHostHooks(host, binary, configDir)
	if err != nil {
		return err
	}
	for _, file := range written {
		fmt.Println(T("host.wired", forwardSlashes(file.path), file.summary))
	}
	previousFiles, previousHost := files, activeHost
	files = pathsFor(configDir, installRoot)
	activeHost = host
	for _, line := range doctorLines(loadConfig()) {
		fmt.Printf("   %s\n", line)
	}
	files, activeHost = previousFiles, previousHost
	if exe := hostExecutable(host); exe == "" {
		fmt.Println(T("host.exeMissing", spec.exe, spec.display))
	}
	if writeFailures > 0 {
		return errors.New(T("install.incomplete", writeFailures, files.errors))
	}
	fmt.Println(T("host.done", spec.display, forwardSlashes(binary)))
	fmt.Println(T("host.next." + host))
	if !spec.limits {
		fmt.Println(T("host.noLimits", spec.display))
	}
	return nil
}

func uninstallHost(host, configDir string) {
	spec := hostOf(host)
	fmt.Println(T("install.uninstallHeader", configDir))
	for _, file := range unwireHostHooks(host, configDir) {
		fmt.Println(T("host.unwired", forwardSlashes(file)))
	}
	installRoot := filepath.Join(configDir, pluginName, "plugin")
	if statSafe(installRoot) != nil {
		if err := os.RemoveAll(installRoot); err == nil {
			fmt.Println(T("install.removed", installRoot))
		}
	}
	fmt.Println(T("host.uninstalled", spec.display))
}

func runSetup() {
	pluginRoot := files.pluginRoot
	defaults := readJSON(filepath.Join(pluginRoot, "config.default.json"))
	if defaults == nil {
		fmt.Fprintln(os.Stderr, T("install.defaultsMissing", pluginRoot))
		os.Exit(1)
	}
	if host := chooseHost(); host != "claude" {
		for _, configDir := range hostTargets(host) {
			if err := installHost(host, configDir, pluginRoot, defaults); err != nil {
				fmt.Fprintf(os.Stderr, "!! %s\n", err)
				os.Exit(1)
			}
		}
		return
	}
	for _, configDir := range configTargets() {
		fmt.Println(T("install.header", configDir))
		binary, err := placeBinary(pluginRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "!! %s\n", err)
			os.Exit(1)
		}
		guardDir := filepath.Join(configDir, pluginName)
		ensureDir(guardDir)
		configFile := filepath.Join(guardDir, "config.json")
		config, configOk, added := mergeConfig(configFile, defaults)
		if configOk {
			if err := applyPreset(configFile, config, flagString("preset")); err != nil {
				fmt.Fprintf(os.Stderr, "!! %s\n", err)
				os.Exit(1)
			}
			if err := configureRoles(configFile, config, pluginRoot, added["roles"]); err != nil {
				fmt.Fprintf(os.Stderr, "!! %s\n", err)
				os.Exit(1)
			}
		}
		if err := wireSettings(configDir, binary, config, configOk, configFile, defaults, args.present["no-model"]); err != nil {
			fmt.Fprintf(os.Stderr, "!! %s\n", err)
			os.Exit(1)
		}
		previousConfig := files.configDir
		files = pathsFor(configDir, pluginRoot)
		for _, line := range doctorLines(loadConfig()) {
			fmt.Printf("   %s\n", line)
		}
		files = pathsFor(previousConfig, pluginRoot)
		if writeFailures > 0 {
			fmt.Fprintf(os.Stderr, "!! %s\n", T("install.incomplete", writeFailures, files.errors))
			os.Exit(1)
		}
		fmt.Println(T("setup.done", forwardSlashes(binary), orDefault(getString(readJSON(filepath.Join(configDir, "settings.json")), "model"), "-")))
	}
	if line := enableMarketplaceAutoUpdate(pluginRoot); line != "" {
		fmt.Println(line)
	}
	fmt.Println(T("install.next"))
	fmt.Println(T("install.next1"))
}

func marketplaceNameFor(pluginRoot string) string {
	normalized := filepath.ToSlash(pluginRoot)
	index := strings.Index(normalized, "/plugins/cache/")
	if index < 0 {
		return ""
	}
	rest := normalized[index+len("/plugins/cache/"):]
	return strings.SplitN(rest, "/", 2)[0]
}

func enableMarketplaceAutoUpdate(pluginRoot string) string {
	if strings.ToLower(flagString("updates")) == "keep" {
		return ""
	}
	market := marketplaceNameFor(pluginRoot)
	if market == "" {
		return ""
	}
	claudePath := claudeExecutable()
	if claudePath == "" {
		return T("update.autoFailed", market)
	}
	if _, err := runWithTimeout(claudeCommand(claudePath, []string{"plugin", "marketplace", "update", market, "--auto-update"}), 45*time.Second); err != nil {
		warn("marketplace auto-update could not be enabled for %s: %v", market, err)
		return T("update.autoFailed", market)
	}
	logInfo("marketplace auto-update enabled for %s", market)
	return T("update.autoEnabled", market)
}

func pathsFor(configDir, pluginRoot string) paths {
	saved := os.Getenv("CLAUDE_CONFIG_DIR")
	savedRoot := os.Getenv("NOCTIS_PLUGIN_ROOT")
	os.Setenv("CLAUDE_CONFIG_DIR", configDir)
	os.Setenv("NOCTIS_PLUGIN_ROOT", pluginRoot)

	previousFlags, hadFlag := args.flags["account"]
	args.flags["account"] = configDir
	initPaths()
	if hadFlag {
		args.flags["account"] = previousFlags
	} else {
		delete(args.flags, "account")
	}
	result := files
	os.Setenv("CLAUDE_CONFIG_DIR", saved)
	os.Setenv("NOCTIS_PLUGIN_ROOT", savedRoot)
	return result
}
