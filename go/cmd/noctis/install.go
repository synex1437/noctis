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
	"balanced":     {"session5h": float64(92), "weeklyAll": float64(95), "weeklyFable": float64(97)},
	"aggressive":   {"session5h": float64(96), "weeklyAll": float64(97), "weeklyFable": float64(98)},
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

func hooksModules(root string) []string {
	modules := []string{}
	for _, raw := range getList(readJSON(filepath.Join(root, "hooks", "hooks.json")), "modules") {
		name, _ := raw.(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		relative := filepath.Join("hooks", filepath.FromSlash(name))
		if isInside(filepath.Join(root, relative), root) {
			modules = append(modules, relative)
		}
	}
	return modules
}

func copyPluginTree(from, to string) error {
	wanted, dirs := installedPaths()
	for _, relative := range append(wanted, hooksModules(from)...) {
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

func readInstallConfig(configFile string) (object, error) {
	stored := readJSONStrict(configFile)
	if !stored.ok {
		return nil, errors.New(T("install.configBroken", configFile, stored.err))
	}
	return stored.data, nil
}

func writeInstallConfig(configFile string, config object) error {
	if err := writeJSONAtomic(configFile, config); err != nil {
		fail("write %s failed: %v", filepath.Base(configFile), err)
		return errors.New(T("install.configUnwritable", configFile, err))
	}
	return nil
}

func mergeConfig(configFile string, defaults object) (object, map[string]bool, error) {
	current, err := readInstallConfig(configFile)
	if err != nil {
		return nil, nil, err
	}
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
	if err := writeInstallConfig(configFile, merged); err != nil {
		return nil, nil, err
	}
	return merged, added, nil
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
	defer func() {
		if err := os.Remove(staging); err != nil && !errors.Is(err, os.ErrNotExist) {
			warn("temp file left behind: %s", staging)
		}
	}()

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
		if _, _, err := mergeConfig(files.config, defaults); err != nil {
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
			stored := readJSONStrict(files.config)
			if !stored.ok {
				warn("ensure: statusLine left as it is: config.json, where its chain is kept, cannot be read (%s)", stored.err)
				return false
			}
			config := stored.data
			if config == nil {
				config = object{}
			}
			statusline := getMap(config, "statusline")
			if statusline == nil {
				statusline = object{}
				config["statusline"] = statusline
			}
			if getString(statusline, "chainCommand") != current {
				statusline["chainCommand"] = current
				if err := writeJSONAtomic(files.config, config); err != nil {
					warn("ensure: statusLine left as it is: its chain could not be saved in config.json (%v)", err)
					return false
				}
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
	binary := filepath.Join(files.pluginRoot, "bin", binaryFileName())
	if os.SameFile(statSafe(platformBinary(files.pluginRoot)), statSafe(binary)) {
		os.Remove(binary + ".old")
	} else if _, err := placeBinary(files.pluginRoot); err != nil {
		warn("ensure: %v", err)
	}
	defaults := readJSON(filepath.Join(files.pluginRoot, "config.default.json"))
	firstRunSetup(defaults)

	if len(defaults) > 0 && statSafe(files.config) != nil {
		if _, added, err := mergeConfig(files.config, defaults); err == nil && len(added) > 0 {
			logInfo("ensure: %d new config section(s) added: %s", len(added), strings.Join(sortedKeys(added), ", "))
		}
	}
	if cfg := loadConfig(); getString(cfg, "configError") == "" {
		if roles := section(cfg, "roles"); len(roles) > 0 {
			if changed := syncAgentFiles(files.pluginRoot, roles, providerModels(readJSONStrict(files.settings).data)); changed > 0 {
				logInfo("ensure: %d agent file(s) synced with the roles profile", changed)
			}
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
		current := statusLineBinary(command)
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

func statusLineBinary(command string) string {
	if strings.HasPrefix(command, `"`) {
		if end := strings.Index(command[1:], `"`); end >= 0 {
			return command[1 : end+1]
		}
	} else if space := strings.Index(command, " "); space >= 0 {
		return command[:space]
	}
	return command
}

func ownStatusLine(command string) bool {
	return strings.Contains(command, "guard.js") || strings.Contains(command, pluginName)
}

func goneBinary(binary string) string {
	if strings.Contains(binary, pluginName) && filepath.IsAbs(filepath.FromSlash(binary)) && statSafe(binary) == nil {
		return binary
	}
	return ""
}

func statusLineGone(command string) string {
	if !ownStatusLine(command) {
		return ""
	}
	return goneBinary(statusLineBinary(command))
}

func configureRoles(configFile string, config object, pluginRoot string, rolesAreNew, anyModel bool) error {
	current := section(config, "roles")
	if rolesAreNew {

		current = derivedRoles(config, current)
	}
	roles, given, err := rolesFromArgs(current, anyModel)
	if err != nil {
		return err
	}
	if !given && stdinIsTerminal() && !args.present["no-ask"] {
		roles = askRoles(current, anyModel)
	}
	applyRoles(configFile, config, roles)
	syncAgentFiles(pluginRoot, roles, anyModel)
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

func wireSettings(configDir, binary string, config object, configFile string, defaults object, noModel bool) error {
	settingsFile := filepath.Join(configDir, "settings.json")
	settings := readJSONStrict(settingsFile)
	if !settings.ok {
		return errors.New(T("install.settingsBroken", settings.err))
	}
	data := settings.data
	if data == nil {
		data = object{}
	}
	chained := ""
	previous := getString(getMap(data, "statusLine"), "command")
	if previous != "" && !strings.Contains(previous, "guard.js") && !strings.Contains(previous, "noctis") && getString(section(config, "statusline"), "chainCommand") != previous {
		statusline := section(config, "statusline")
		statusline["chainCommand"] = previous
		config["statusline"] = statusline
		chained = previous
	}
	data["statusLine"] = object{"type": "command", "command": fmt.Sprintf(`"%s" statusline`, forwardSlashes(binary))}
	effort := orDefault(getString(section(config, "models"), "effort"), getString(section(defaults, "models"), "effort"))
	env := getMap(data, "env")
	if env == nil {
		env = object{}
	}
	effortRecord := getMap(config, "managedEffort")
	config["managedEffort"] = object{"previous": valueSetupFound(env, "CLAUDE_CODE_EFFORT_LEVEL", effortRecord["previous"], effortRecord != nil), "set": effort}
	env["CLAUDE_CODE_EFFORT_LEVEL"] = effort
	leanNote := wireLeanSwitch(config, env)
	data["env"] = env
	current := getString(data, "model")
	managed := getMap(config, "managedModel")
	ours := managed != nil && current != "" && current == getString(managed, "set")
	primary := orDefault(getString(section(config, "models"), "primary"), getString(section(defaults, "models"), "primary"))
	wroteModel := !noModel && (ours || !keepModelPattern.MatchString(current))
	if wroteModel {
		if current != primary {

			previous, hadModel := data["model"]
			if !hadModel {
				previous = nil
			}
			if managed != nil {
				previous = managed["previous"]
			}
			config["managedModel"] = object{"previous": previous, "set": primary}
		}
		data["model"] = primary
	}
	permissionNote := ""
	modeFound, modeFoundRecorded := config["managedPermissionPrevious"]
	if mode := managedPermissionMode(config); mode != "" {
		permissions := getMap(data, "permissions")
		if permissions == nil {
			permissions = object{}
		}
		before := valueSetupFound(permissions, "defaultMode", modeFound, modeFoundRecorded)
		config["managedPermissionPrevious"] = before
		permissions["defaultMode"] = mode
		data["permissions"] = permissions
		permissionNote = permissionChangeNote(mode, before)
	} else if choiceOf(permissionChoices, flagString("permissions")) == "" {
		if current := getString(getMap(data, "permissions"), "defaultMode"); current != "" {
			permissionNote = T("install.permissionsKept", current)
		} else {
			permissionNote = T("install.permissionsNone")
		}
	} else if resume := section(config, "resume"); getString(resume, "permissionMode") == "auto" {
		resume["permissionMode"] = "inherit"
		config["resume"] = resume
		permissionNote = T("install.relaunchInherit")
	}
	if err := writeInstallConfig(configFile, config); err != nil {
		return err
	}
	backup := backupFile(settingsFile)
	if err := writeJSONKeepingOrder(settingsFile, data); err != nil {
		writeFailures++
		fail("write %s failed: %v", filepath.Base(settingsFile), err)
	}
	model := getString(data, "model")
	settleModelSwitch(configDir, noModel, func(switched object) string {
		before := getString(switched, "from")
		if getBool(switched, "modelAbsent", false) {
			before = ""
		}
		target := before
		if (before != "" && before == getString(managed, "set")) || !keepModelPattern.MatchString(before) {
			target = primary
		}
		if wroteModel || model == primary || target == model {
			return ""
		}
		return target
	})
	if chained != "" {
		fmt.Println(T("install.chained", chained))
	}
	if permissionNote != "" {
		fmt.Println(permissionNote)
	}
	backupText := ""
	if backup != "" {
		backupText = T("install.backup", filepath.Base(backup))
	}
	fmt.Println(T("install.settings", backupText, effort, getString(data, "model")))
	fmt.Println(leanNote)
	return nil
}

func permissionChangeNote(mode string, before any) string {
	previous, _ := before.(string)
	switch {
	case previous == "":
		return T("install.permissionsUnset", mode)
	case previous == mode:
		return T("install.permissionsSame", mode)
	case previous != "keep" && choiceOf(permissionChoices, previous) == previous && (previous != "auto" || supportedPermissionMode(object{"resume": object{"permissionMode": "auto"}}, claudeExecutable(), "") == "auto"):
		return T("install.permissions", mode, previous, previous)
	}
	return T("install.permissionsOther", mode, previous)
}

func valueSetupFound(holder object, key string, found any, recorded bool) any {
	if recorded {
		return found
	}
	return holder[key]
}

func settleModelSwitch(configDir string, noModel bool, giveBackAfterSetup func(switched object) string) {
	previous := files
	defer func() { files = previous }()
	files = pathsFor(configDir, files.pluginRoot)
	if getMap(readState(), "modelSwitched") == nil {
		return
	}
	outcome, giveBack := "", ""
	updateState(func(state object) {
		switched := getMap(state, "modelSwitched")
		if switched == nil {
			return
		}
		outcome = "effort"
		if !noModel {
			if giveBack = giveBackAfterSetup(switched); giveBack == "" {
				state["modelSwitched"], outcome = nil, "dropped"
				return
			}
			switched["from"], outcome = giveBack, "kept"
			delete(switched, "modelAbsent")
		}
		for _, key := range switchedEffortKeys {
			delete(switched, key)
		}
	})
	switch outcome {
	case "effort":
		logInfo("setup set the effort: the scoped-model switch gives back only the model at its reset")
	case "kept":
		logInfo("setup set the effort and kept the model the scoped-model switch wrote: the switch gives back %s at its reset", giveBack)
	case "dropped":
		logInfo("setup set the default model and effort: the scoped-model switch will not undo them at its reset")
	}
}

func managedPermissionMode(config object) string {
	choice := choiceOf(permissionChoices, flagString("permissions"))
	if choice == "keep" {
		config["managedPermissionKeep"] = true
		return ""
	}
	if choice == "" && (getString(config, "managedPermissionMode") != "" || getBool(config, "managedPermissionKeep", false)) {
		return ""
	}
	if choice == "" {
		choice = "auto"
	}
	probe := object{"resume": object{"permissionMode": choice}}
	mode := supportedPermissionMode(probe, claudeExecutable(), "")
	config["managedPermissionMode"] = mode
	return mode
}

func installInto(configDir, sourceRoot string, noModel bool, defaults object) error {
	fmt.Println(T("install.header", configDir))
	guardDir := filepath.Join(configDir, pluginName)
	configFile := filepath.Join(guardDir, "config.json")
	if _, err := readInstallConfig(configFile); err != nil {
		return err
	}
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
	ensureDir(guardDir)
	config, added, err := mergeConfig(configFile, defaults)
	if err != nil {
		return err
	}
	if err := applyPreset(configFile, config, flagString("preset")); err != nil {
		return err
	}
	if err := configureRoles(configFile, config, installRoot, added["roles"], providerModels(readJSONStrict(filepath.Join(configDir, "settings.json")).data)); err != nil {
		return err
	}
	if err := wireSettings(configDir, binary, config, configFile, defaults, noModel); err != nil {
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

func uninstallFrom(configDir string) error {
	fmt.Println(T("install.uninstallHeader", configDir))
	settingsFile := filepath.Join(configDir, "settings.json")
	settings := readJSONStrict(settingsFile)
	if !settings.ok {
		return errors.New(T("install.uninstallBroken", settingsFile, settings.err) + "\n" + T("install.undoByHand"))
	}
	configFile := filepath.Join(configDir, pluginName, "config.json")
	stored := readJSONStrict(configFile)
	if !stored.ok {
		return errors.New(T("install.uninstallBroken", configFile, stored.err))
	}
	cancelled, err := cancelAccountRelaunches(configDir)
	if err != nil {
		return err
	}
	fmt.Println(T("install.cancelled", cancelled))
	if settings.data != nil {
		if err := undoSetupSettings(settingsFile, settings.data, stored.data); err != nil {
			return err
		}
	}
	if line := undoMarketplaceAutoUpdate(configDir, stored.data); line != "" {
		fmt.Println(line)
	}
	if settings.data != nil {
		forgetSetupRecords(configFile, stored.data)
	}
	installRoot := filepath.Join(configDir, "skills", pluginName)
	if statSafe(installRoot) != nil {
		if err := os.RemoveAll(installRoot); err == nil {
			fmt.Println(T("install.removed", installRoot))
		}
	}
	settleStateDir(configDir)
	return nil
}

func cancelAccountRelaunches(configDir string) (int, error) {
	previous := files
	defer func() { files = previous }()
	files = pathsFor(configDir, files.pluginRoot)
	state := readState()
	_, found := pendingOf(state)
	sids := sortedKeys(found)
	if len(sids) > 0 && !cancelSessions(sids, state) {
		return 0, errors.New(T("install.cancelFailed", files.errors))
	}
	return len(sids), nil
}

func settleStateDir(configDir string) {
	guardDir := filepath.Join(configDir, pluginName)
	if !args.present["purge"] {
		if statSafe(guardDir) != nil {
			fmt.Println(T("install.stateKept", guardDir))
		}
		return
	}
	info, err := os.Lstat(guardDir)
	if err != nil {
		return
	}
	if !info.IsDir() {
		fmt.Println(T("install.purgeLink", guardDir))
		return
	}
	if err := os.RemoveAll(guardDir); err != nil {
		fmt.Println(T("install.purgeFailed", guardDir, err))
		return
	}
	fmt.Println(T("install.purged", guardDir))
}

var setupRecords = []string{"managedModel", "managedEffort", "managedPermissionMode", "managedPermissionPrevious", "managedPermissionKeep", "managedFunctionHooks", "managedAutoUpdate"}

func forgetSetupRecords(configFile string, config object) {
	forgotten := false
	for _, key := range setupRecords {
		if _, recorded := config[key]; recorded {
			delete(config, key)
			forgotten = true
		}
	}
	if statusline := getMap(config, "statusline"); getString(statusline, "chainCommand") != "" {
		statusline["chainCommand"] = ""
		forgotten = true
	}
	if !forgotten {
		return
	}
	if err := writeJSONAtomic(configFile, config); err != nil {
		warn("uninstall: %s still holds what setup recorded about settings.json: %v", filepath.Base(configFile), err)
	}
}

func undoSetupSettings(settingsFile string, data, guardConfig object) error {
	chain := getString(section(guardConfig, "statusline"), "chainCommand")
	statusLine := getString(getMap(data, "statusLine"), "command")
	if strings.Contains(statusLine, "guard.js") || strings.Contains(statusLine, "noctis") {
		if chain != "" {
			data["statusLine"] = object{"type": "command", "command": chain}
		} else {
			delete(data, "statusLine")
		}
	}
	if env := getMap(data, "env"); env != nil {
		managedEffort := getMap(guardConfig, "managedEffort")
		switch {
		case managedEffort == nil, getString(env, "CLAUDE_CODE_EFFORT_LEVEL") != getString(managedEffort, "set"):

		case getString(managedEffort, "previous") != "":
			env["CLAUDE_CODE_EFFORT_LEVEL"] = getString(managedEffort, "previous")
		default:
			delete(env, "CLAUDE_CODE_EFFORT_LEVEL")
		}
	}
	if env := getMap(data, "env"); env != nil {
		takeBackLeanSwitch(guardConfig, env)
	}
	managed := getString(guardConfig, "managedPermissionMode")
	found, foundRecorded := guardConfig["managedPermissionPrevious"]
	if permissions := getMap(data, "permissions"); permissions != nil && managed != "" && foundRecorded && getString(permissions, "defaultMode") == managed {
		if previous, _ := found.(string); previous != "" {
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
	if err := writeJSONKeepingOrder(settingsFile, data); err != nil {
		return err
	}
	fmt.Println(T("install.restored", modelNote))
	return nil
}

var setupFlags = []string{"profile", "preset", "permissions", "updates", "no-model", "no-lean", "no-ask", "config-dir", "account", "host", "code", "research", "planning", "digest", "explore", "fallback"}

var installFlags = append([]string{"source", "uninstall", "purge"}, setupFlags...)

var permissionChoices = []string{"auto", "acceptEdits", "plan", "default", "keep"}

var updateChoices = []string{"on", "off", "keep"}

var flagChoices = map[string][]string{"permissions": permissionChoices, "updates": updateChoices}

func choiceOf(choices []string, value string) string {
	for _, choice := range choices {
		if strings.EqualFold(strings.TrimSpace(value), choice) {
			return choice
		}
	}
	return ""
}

func accountTargets(host string) []string {
	targets, seen := []string{}, map[string]bool{}
	for _, name := range []string{"config-dir", "account"} {
		for _, value := range args.values[name] {
			if strings.TrimSpace(value) == "" {
				continue
			}
			resolved, _ := filepath.Abs(expandHome(value))
			if !seen[resolved] {
				seen[resolved] = true
				targets = append(targets, resolved)
			}
		}
	}
	if len(targets) == 0 {
		targets = append(targets, hostHome(host))
	}
	return targets
}

func checkSetupArgs(name string, allowed []string) int {
	if problems := argProblems(allowed); len(problems) > 0 {
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, problem)
		}
		fmt.Fprintln(os.Stderr, T("args.usage", name, "--"+strings.Join(allowed, " --")))
		return 2
	}
	if err := setupValueError(); err != nil {
		fmt.Fprintf(os.Stderr, "!! %s\n", err)
		return 1
	}
	return 0
}

func argProblems(allowed []string) []string {
	known := map[string]bool{}
	for _, name := range allowed {
		known[name] = true
	}
	problems := []string{}
	for _, name := range sortedKeys(args.present) {
		values := args.values[name]
		switch {
		case !known[name]:
			if guess := closestFlag(name, allowed); guess != "" {
				problems = append(problems, T("args.unknownGuess", "--"+name, "--"+guess))
			} else {
				problems = append(problems, T("args.unknown", "--"+name))
			}
		case switchFlags[name]:
			if len(values) > 0 {
				problems = append(problems, T("args.noValue", "--"+name))
			}
		default:
			for _, value := range values {
				if problem := valueProblem(name, value); problem != "" {
					problems = append(problems, problem)
					break
				}
			}
		}
	}
	for index, word := range args.positional {
		if index == 0 {
			continue
		}
		if guess := wordGuess(word, known); guess != "" {
			problems = append(problems, T("args.strayGuess", word, guess))
		} else {
			problems = append(problems, T("args.stray", word))
		}
	}
	return problems
}

func valueProblem(name, value string) string {
	if strings.TrimSpace(value) == "" {
		return T("args.needsValue", "--"+name)
	}
	if choices := flagChoices[name]; choices != nil && choiceOf(choices, value) == "" {
		return T("args.badValue", "--"+name, value, strings.Join(choices, ", "))
	}
	return ""
}

func wordGuess(word string, known map[string]bool) string {
	lower := strings.ToLower(strings.TrimSpace(word))
	if roleProfiles[lower] != nil {
		return "--profile " + lower
	}
	if thresholdPresets[lower] != nil {
		return "--preset " + lower
	}
	if mode := choiceOf(permissionChoices, lower); mode != "" {
		return "--permissions " + mode
	}
	if choice := choiceOf(updateChoices, lower); choice != "" {
		return "--updates " + choice
	}
	if _, ok := hostSpecs[lower]; ok {
		return "--host " + lower
	}
	if name := strings.TrimLeft(lower, "-"); known[name] {
		return "--" + name
	}
	return ""
}

func closestFlag(name string, allowed []string) string {
	lower := strings.ToLower(name)
	started := []string{}
	for _, candidate := range allowed {
		if len(lower) >= 3 && strings.HasPrefix(candidate, lower) {
			started = append(started, candidate)
		}
	}
	if len(started) == 1 {
		return started[0]
	}
	best, bestDistance := "", 3
	for _, candidate := range allowed {
		if distance := editDistance(lower, candidate); distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

func editDistance(from, to string) int {
	previous := make([]int, len(to)+1)
	for index := range previous {
		previous[index] = index
	}
	for i := 1; i <= len(from); i++ {
		current := make([]int, len(to)+1)
		current[0] = i
		for j := 1; j <= len(to); j++ {
			cost := 1
			if from[i-1] == to[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(to)]
}

func setupValueError() error {
	for _, preset := range args.values["preset"] {
		if thresholdPresets[strings.ToLower(preset)] == nil {
			return errors.New(T("setup.unknownPreset", preset))
		}
	}
	for _, profile := range args.values["profile"] {
		if name := profileAlias(strings.ToLower(profile)); roleProfiles[name] == nil {
			return errors.New(T("roles.unknownProfile", name))
		}
	}
	anyModel := true
	for _, configDir := range accountTargets("claude") {
		anyModel = anyModel && providerModels(readJSONStrict(filepath.Join(configDir, "settings.json")).data)
	}
	for _, role := range roleNames {
		for _, value := range args.values[role] {
			if err := roleFlagError(role, value, anyModel); err != nil {
				return err
			}
		}
	}
	return nil
}

func runInstall() {
	if code := checkSetupArgs("install", installFlags); code != 0 {
		os.Exit(code)
	}
	if args.present["purge"] && !args.present["uninstall"] {
		fmt.Fprintln(os.Stderr, T("install.purgeAlone"))
		os.Exit(2)
	}
	sourceRoot := files.pluginRoot
	if source := flagString("source"); source != "" {
		sourceRoot, _ = filepath.Abs(expandHome(source))
	}
	defaults := readJSON(filepath.Join(sourceRoot, "config.default.json"))
	if defaults == nil {
		fmt.Fprintln(os.Stderr, T("install.defaultsMissing", sourceRoot))
		os.Exit(1)
	}
	host := chooseHost()
	if host != "claude" {
		for _, configDir := range accountTargets(host) {
			var err error
			if args.present["uninstall"] {
				err = uninstallHost(host, configDir)
			} else {
				err = installHost(host, configDir, sourceRoot, defaults)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "!! %s\n", err)
				os.Exit(1)
			}
		}
		return
	}
	for _, configDir := range accountTargets("claude") {
		var err error
		if args.present["uninstall"] {
			err = uninstallFrom(configDir)
		} else {
			err = installInto(configDir, sourceRoot, args.present["no-model"], defaults)
		}
		if err != nil {
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
		line, err := reader.ReadString('\n')
		if host := parseHostChoice(line); host != "" {
			return host
		}
		if err != nil {
			return "claude"
		}
		fmt.Println("  " + T("host.badChoice", strings.TrimSpace(line)))
	}
}

func installHost(host, configDir, sourceRoot string, defaults object) error {
	spec := hostOf(host)
	fmt.Println(T("host.header", spec.display, configDir))
	guardDir := filepath.Join(configDir, pluginName)
	configFile := filepath.Join(guardDir, "config.json")
	if _, err := readInstallConfig(configFile); err != nil {
		return err
	}
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
	config, _, err := mergeConfig(configFile, defaults)
	if err != nil {
		return err
	}
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
	if err := writeInstallConfig(configFile, config); err != nil {
		return err
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

func uninstallHost(host, configDir string) error {
	spec := hostOf(host)
	fmt.Println(T("install.uninstallHeader", configDir))
	removed, err := unwireHostHooks(host, configDir)
	for _, file := range removed {
		fmt.Println(T("host.unwired", forwardSlashes(file)))
	}
	if err != nil {
		return err
	}
	cancelled, err := cancelAccountRelaunches(configDir)
	if err != nil {
		return err
	}
	fmt.Println(T("install.cancelled", cancelled))
	installRoot := filepath.Join(configDir, pluginName, "plugin")
	if statSafe(installRoot) != nil {
		if err := os.RemoveAll(installRoot); err == nil {
			fmt.Println(T("install.removed", installRoot))
		}
	}
	settleStateDir(configDir)
	fmt.Println(T("host.uninstalled", spec.display))
	return nil
}

func runSetup() {
	if code := checkSetupArgs("setup", setupFlags); code != 0 {
		os.Exit(code)
	}
	pluginRoot := files.pluginRoot
	defaults := readJSON(filepath.Join(pluginRoot, "config.default.json"))
	if defaults == nil {
		fmt.Fprintln(os.Stderr, T("install.defaultsMissing", pluginRoot))
		os.Exit(1)
	}
	if host := chooseHost(); host != "claude" {
		for _, configDir := range accountTargets(host) {
			if err := installHost(host, configDir, pluginRoot, defaults); err != nil {
				fmt.Fprintf(os.Stderr, "!! %s\n", err)
				os.Exit(1)
			}
		}
		return
	}
	for _, configDir := range accountTargets("claude") {
		if err := setupInto(configDir, pluginRoot, defaults); err != nil {
			fmt.Fprintf(os.Stderr, "!! %s\n", err)
			os.Exit(1)
		}
	}
	if line := enableMarketplaceAutoUpdate(pluginRoot); line != "" {
		fmt.Println(line)
	}
	fmt.Println(T("install.next"))
	fmt.Println(T("install.next1"))
}

func setupInto(configDir, pluginRoot string, defaults object) error {
	fmt.Println(T("install.header", configDir))
	guardDir := filepath.Join(configDir, pluginName)
	configFile := filepath.Join(guardDir, "config.json")
	if _, err := readInstallConfig(configFile); err != nil {
		return err
	}
	binary, err := placeBinary(pluginRoot)
	if err != nil {
		return err
	}
	ensureDir(guardDir)
	config, added, err := mergeConfig(configFile, defaults)
	if err != nil {
		return err
	}
	if err := applyPreset(configFile, config, flagString("preset")); err != nil {
		return err
	}
	if err := configureRoles(configFile, config, pluginRoot, added["roles"], providerModels(readJSONStrict(filepath.Join(configDir, "settings.json")).data)); err != nil {
		return err
	}
	if err := wireSettings(configDir, binary, config, configFile, defaults, args.present["no-model"]); err != nil {
		return err
	}
	previousConfig := files.configDir
	files = pathsFor(configDir, pluginRoot)
	for _, line := range doctorLines(loadConfig()) {
		fmt.Printf("   %s\n", line)
	}
	files = pathsFor(previousConfig, pluginRoot)
	if writeFailures > 0 {
		return errors.New(T("install.incomplete", writeFailures, files.errors))
	}
	fmt.Println(T("setup.done", forwardSlashes(binary), orDefault(getString(readJSON(filepath.Join(configDir, "settings.json")), "model"), "-")))
	return nil
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

func marketplaceOwnerFor(pluginRoot string) string {
	normalized := filepath.ToSlash(pluginRoot)
	index := strings.Index(normalized, "/plugins/cache/")
	if index <= 0 {
		return ""
	}
	return filepath.FromSlash(normalized[:index])
}

func knownMarketplacesFile(configDir string) string {
	return filepath.Join(configDir, "plugins", "known_marketplaces.json")
}

func marketplaceAutoUpdate(configDir, market string) (any, bool) {
	if configDir == "" {
		return nil, false
	}
	value, had := getMap(readJSON(knownMarketplacesFile(configDir)), market)["autoUpdate"]
	return value, had
}

func recordMarketplaceAutoUpdate(owner, market string, previous any, hadPrevious bool) string {
	configFile := filepath.Join(owner, pluginName, "config.json")
	stored := readJSONStrict(configFile)
	if !stored.ok || !stored.exists || stored.data == nil {
		return T("update.autoNotRecorded", market)
	}
	if _, recorded := stored.data["managedAutoUpdate"]; recorded {
		return ""
	}
	record := object{"marketplace": market}
	if hadPrevious {
		record["previous"] = previous
	}
	stored.data["managedAutoUpdate"] = record
	if err := writeJSONAtomic(configFile, stored.data); err != nil {
		warn("setup: the marketplace auto-update it switched on is not recorded in %s: %v", configFile, err)
		return T("update.autoNotRecorded", market)
	}
	return ""
}

func undoMarketplaceAutoUpdate(configDir string, config object) string {
	record := getMap(config, "managedAutoUpdate")
	market := getString(record, "marketplace")
	if market == "" {
		return ""
	}
	knownFile := knownMarketplacesFile(configDir)
	known := readJSONStrict(knownFile)
	if !known.ok {
		return T("install.autoUpdateLeft", market, knownFile)
	}
	marketplace := getMap(known.data, market)
	if marketplace == nil || marketplace["autoUpdate"] != true {
		return ""
	}
	if previous, had := record["previous"]; had {
		marketplace["autoUpdate"] = previous
	} else {
		delete(marketplace, "autoUpdate")
	}
	if err := writeJSONAtomic(knownFile, known.data); err != nil {
		warn("uninstall: marketplace auto-update for %s not set back: %v", market, err)
		return T("install.autoUpdateLeft", market, knownFile)
	}
	return T("install.autoUpdateBack", market)
}

func enableMarketplaceAutoUpdate(pluginRoot string) string {
	if choice := choiceOf(updateChoices, flagString("updates")); choice == "keep" || choice == "off" {
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
	owner := marketplaceOwnerFor(pluginRoot)
	before, hadBefore := marketplaceAutoUpdate(owner, market)
	if _, err := runWithTimeout(inGuardDir(claudeCommand(claudePath, []string{"plugin", "marketplace", "update", market, "--auto-update"})), 45*time.Second); err != nil {
		warn("marketplace auto-update could not be enabled for %s: %v", market, err)
		return T("update.autoFailed", market)
	}
	logInfo("marketplace auto-update enabled for %s", market)
	enabled := T("update.autoEnabled", market)
	if after, _ := marketplaceAutoUpdate(owner, market); owner != "" && before != true && after == true {
		if note := recordMarketplaceAutoUpdate(owner, market, before, hadBefore); note != "" {
			enabled += "\n" + note
		}
	}
	return enabled
}

func pathsFor(configDir, pluginRoot string) paths {
	saved, hadSaved := os.LookupEnv("CLAUDE_CONFIG_DIR")
	savedRoot, hadRoot := os.LookupEnv("NOCTIS_PLUGIN_ROOT")
	os.Setenv("CLAUDE_CONFIG_DIR", configDir)
	os.Setenv("NOCTIS_PLUGIN_ROOT", pluginRoot)

	previousFlags, hadFlag := args.flags["account"]
	if args.flags == nil {
		args.flags = map[string]string{}
	}
	args.flags["account"] = configDir
	initPaths()
	if hadFlag {
		args.flags["account"] = previousFlags
	} else {
		delete(args.flags, "account")
	}
	result := files
	restoreEnv("CLAUDE_CONFIG_DIR", saved, hadSaved)
	restoreEnv("NOCTIS_PLUGIN_ROOT", savedRoot, hadRoot)
	return result
}

func restoreEnv(name, value string, had bool) {
	if had {
		os.Setenv(name, value)
		return
	}
	os.Unsetenv(name)
}
