package main

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// settingsLayer is one of the settings Claude Code lays over each other, as it loads it.
type settingsLayer struct {
	data object
	// content is the file's text, which gives the order of the names in its objects; the managed settings,
	// laid together from several files, keep that order in orders instead.
	content []byte
	orders  map[string][]string
	// file names the file in a message: "" for the person's settings.json.
	file string
	// from names the file of a setting ("autoCompactWindow", "autoCompactEnabled", "env.<name>" or
	// "modelSettings.<name>") that comes from another file than file, as one of managed-settings.d's.
	from map[string]string
}

func (layer settingsLayer) fileOf(setting string) string {
	if file, found := layer.from[setting]; found {
		return file
	}
	return layer.file
}

// order is the order of the names in the layer's object under field, as Claude Code goes through them.
func (layer settingsLayer) order(field string) []string {
	if layer.content == nil {
		return layer.orders[field]
	}
	return objectOrder(bytes.TrimPrefix(layer.content, utf8BOM), field)
}

// managedSettingsDir is where Claude Code reads the managed settings of this system from. It is a variable
// so that the command-line tests' children read none of the machine's.
var managedSettingsDir = func() string {
	switch {
	case isWindows:
		return `C:\Program Files\ClaudeCode`
	case isDarwin:
		return "/Library/Application Support/ClaudeCode"
	}
	return "/etc/claude-code"
}

// claudeSettingsLayers are the settings Claude Code 2.1.289 lays over each other for a session started in
// dir, in its order, each over the ones before: the person's settings.json, the project's
// .claude/settings.json in dir, its .claude/settings.local.json (localSettingsLayer), then the managed
// settings. A file that cannot be read, or holds no JSON object, counts for nothing, as Claude Code sets it
// aside, and a file of the project that is the person's settings.json, as in the home directory, is read
// once. The settings Claude Code is given on its command line (--settings), and managed settings it takes
// from elsewhere (a device profile, the Windows registry, the server), are not seen. Another host than Claude
// Code has only its settings.json read.
func claudeSettingsLayers(dir string) []settingsLayer {
	layers := []settingsLayer{}
	if read := readJSONShared(files.settings); read.ok && read.data != nil {
		layers = append(layers, settingsLayer{data: read.data, content: read.raw})
	}
	if currentHost().id != "claude" {
		return layers
	}
	if dir != "" {
		if file := filepath.Join(dir, ".claude", "settings.json"); !samePath(file, files.settings) {
			if data, content := readSettingsFile(file); data != nil {
				layers = append(layers, settingsLayer{data: data, content: content, file: ".claude/settings.json"})
			}
		}
		if local, found := localSettingsLayer(dir); found {
			layers = append(layers, local)
		}
	}
	if managed, found := managedSettingsLayer(files.managedSettings); found {
		layers = append(layers, managed)
	}
	return layers
}

// localSettingsLayer is the project's .claude/settings.local.json as Claude Code 2.1.289 loads it for a
// session started in dir: the one in localSettingsDir(dir), laid over the one in dir where that is
// elsewhere.
func localSettingsLayer(dir string) (settingsLayer, bool) {
	if absolute, err := filepath.Abs(dir); err == nil {
		dir = absolute
	}
	file := filepath.Join(dir, ".claude", "settings.local.json")
	at := localSettingsDir(dir)
	if at == dir {
		data, content := readSettingsFile(file)
		return settingsLayer{data: data, content: content, file: ".claude/settings.local.json"}, data != nil
	}
	return mergedSettingsLayer([]string{file, filepath.Join(at, ".claude", "settings.local.json")}, func(source string) string {
		if source == file {
			return ".claude/settings.local.json"
		}
		return forwardSlashes(source)
	})
}

// localSettingsDir is the directory whose .claude/settings.local.json Claude Code 2.1.289 reads for a session
// started in dir, an absolute path. It keeps a project's local settings at the root of the git work tree dir
// lies in, of a linked work tree at its main work tree's, where that root is not the home directory and it,
// its .git and its .claude, where it has one, belong to the user noctis runs as; in dir otherwise, and always
// on Windows, where it does not tell who owns a directory.
func localSettingsDir(dir string) string {
	if isWindows {
		return dir
	}
	root, linked := gitWorkTreeRoot(dir)
	if root == "" {
		return dir
	}
	if linked {
		root = mainWorkTreeOf(root)
	}
	if root == dir {
		return dir
	}
	home := homeDir()
	if home == "" {
		return dir
	}
	if real, err := filepath.EvalSymlinks(home); err != nil || root == real {
		return dir
	}
	if !ownsLocalSettingsRoot(root) {
		return dir
	}
	return root
}

// gitWorkTreeRoot is the nearest directory from dir up, to the root of the file system, that holds a .git,
// as Claude Code 2.1.289 finds the work tree a session is in: "" where none does. linked tells whether that
// .git is a file, as a linked work tree's is.
func gitWorkTreeRoot(dir string) (root string, linked bool) {
	for at := dir; ; at = filepath.Dir(at) {
		if found, isFile := gitEntry(at); found {
			return at, isFile
		}
		if filepath.Dir(at) == at {
			return "", false
		}
	}
}

// gitEntry tells whether dir holds a .git, a directory or a file or a link to one, as Claude Code 2.1.289
// looks for one, and whether it is a file.
func gitEntry(dir string) (found, isFile bool) {
	file := filepath.Join(dir, ".git")
	info, err := os.Lstat(file)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(file)
	}
	if err != nil {
		return false, false
	}
	return info.IsDir() || info.Mode().IsRegular(), info.Mode().IsRegular()
}

// mainWorkTreeOf is the work tree Claude Code 2.1.289 keeps the local settings of the linked work tree at root
// in: where its .git file names a git directory in the worktrees of a repository, whose commondir leads back
// to that repository and whose gitdir points back at root, the repository's main work tree, or the repository
// itself where it is bare; root otherwise.
func mainWorkTreeOf(root string) string {
	content, err := os.ReadFile(filepath.Join(root, ".git"))
	if err != nil {
		return root
	}
	text := strings.TrimSpace(string(content))
	if !strings.HasPrefix(text, "gitdir:") {
		return root
	}
	gitDir := resolvedFrom(root, strings.TrimSpace(text[len("gitdir:"):]))
	common, read := plainFileText(filepath.Join(gitDir, "commondir"))
	if !read {
		return root
	}
	commonDir := resolvedFrom(gitDir, common)
	if filepath.Dir(gitDir) != filepath.Join(commonDir, "worktrees") {
		return root
	}
	back, read := plainFileText(filepath.Join(gitDir, "gitdir"))
	if !read {
		return root
	}
	pointer, err := filepath.EvalSymlinks(resolvedFrom(gitDir, back))
	if err != nil {
		return root
	}
	if real, err := filepath.EvalSymlinks(root); err != nil || pointer != filepath.Join(real, ".git") {
		return root
	}
	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir)
	}
	if found, _ := gitEntry(commonDir); found {
		return root
	}
	return commonDir
}

// resolvedFrom is path, absolute or taken from base, cleaned.
func resolvedFrom(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

// plainFileText is the text of file, without the spaces around it, where file is a regular file and not a
// link to one, as Claude Code reads the commondir and gitdir of a linked work tree.
func plainFileText(file string) (string, bool) {
	if info, err := os.Lstat(file); err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(content)), true
}

// managedSettingsLayer is the managed settings in dir as Claude Code loads them: managed-settings.json, with
// each file of managed-settings.d whose name ends in .json and does not start with a dot laid over it in the
// order of their names.
func managedSettingsLayer(dir string) (settingsLayer, bool) {
	if dir == "" {
		return settingsLayer{}, false
	}
	sources := []string{filepath.Join(dir, "managed-settings.json")}
	if entries, err := os.ReadDir(filepath.Join(dir, "managed-settings.d")); err == nil {
		names := []string{}
		for _, entry := range entries {
			name := entry.Name()
			if (entry.Type().IsRegular() || entry.Type()&os.ModeSymlink != 0) && strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, ".") {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		for _, name := range names {
			sources = append(sources, filepath.Join(dir, "managed-settings.d", name))
		}
	}
	return mergedSettingsLayer(sources, forwardSlashes)
}

// mergedSettingsLayer is the settings files sources laid over each other, each over the ones before it, as
// Claude Code lays together the files of one layer; shown names a file in a message. found tells whether any
// of them was read.
func mergedSettingsLayer(sources []string, shown func(file string) string) (layer settingsLayer, found bool) {
	layer = settingsLayer{data: object{}, orders: map[string][]string{}, from: map[string]string{}}
	for _, file := range sources {
		data, content := readSettingsFile(file)
		if data == nil {
			continue
		}
		label := shown(file)
		if !found {
			layer.file, found = label, true
		}
		mergeSettings(layer.data, data)
		for name := range data {
			switch name {
			case "env":
				for variable := range getMap(data, name) {
					layer.from["env."+variable] = label
				}
			case "modelSettings", "modelOverrides":
				if _, isObject := layer.data[name].(object); !isObject {
					delete(layer.orders, name)
					continue
				}
				for _, key := range objectOrder(bytes.TrimPrefix(content, utf8BOM), name) {
					if !slices.Contains(layer.orders[name], key) {
						layer.orders[name] = append(layer.orders[name], key)
					}
				}
				if name == "modelSettings" {
					for model, entry := range getMap(data, name) {
						if fields, isObject := entry.(object); isObject && fields["autoCompactWindow"] != nil {
							layer.from["modelSettings."+model] = label
						}
					}
				}
			default:
				layer.from[name] = label
			}
		}
	}
	return layer, found
}

// mergeSettings lays from over into, as Claude Code lays the files of managed-settings.d over each other: an
// object over an object name by name, any other value in place of the one before. It changes nothing but
// into: an object of from, or one into holds from a file read before, is copied before a value goes into it.
func mergeSettings(into, from object) {
	for name, value := range from {
		inner, isObject := value.(object)
		if !isObject {
			into[name] = value
			continue
		}
		current, _ := into[name].(object)
		merged := maps.Clone(current)
		if merged == nil {
			merged = object{}
		}
		mergeSettings(merged, inner)
		into[name] = merged
	}
}

// readSettingsFile is a settings file Claude Code lays over the person's, and its content: nil where the file
// cannot be read, as a file of the project often is not there, or holds no JSON object. It is read once:
// nothing is waited for, and a file that is not a regular one, as a pipe that would hold the read, is not
// read.
func readSettingsFile(file string) (object, []byte) {
	if info, err := os.Stat(file); err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	content, err := readFileShared(file)
	if err != nil {
		return nil, nil
	}
	if cached, seen := parsedEntry(file); seen && bytes.Equal(cached.raw, content) {
		return cached.data, content
	}
	raw, err := decodeJSON(bytes.TrimPrefix(content, utf8BOM))
	data, isObject := raw.(object)
	if err != nil || !isObject {
		return nil, nil
	}
	keepParsed(file, parsedFile{raw: content, data: data})
	return data, content
}

// workingDir is the directory noctis runs in: "" where it cannot be told.
func workingDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// samePath tells whether two paths name one file, as Claude Code reads a file of the project that is the
// person's settings.json, in the home directory, once.
func samePath(first, second string) bool {
	first, second = filepath.Clean(first), filepath.Clean(second)
	if isWindows || isDarwin {
		return strings.EqualFold(first, second)
	}
	return first == second
}

// compactionSettings is the settings Claude Code compacts a session started in dir by, its settings files
// laid over each other: where none but the person's settings.json is found, that file's.
func compactionSettings(dir string) object {
	layers := claudeSettingsLayers(dir)
	switch {
	case len(layers) == 0:
		return nil
	case len(layers) == 1 && layers[0].file == "":
		return layers[0].data
	}
	return layeredSettings(layers)
}

// layeredFacts is what the settings layeredSettings gives know of their files: the file each setting that
// does not come from the person's settings.json comes from, and the order of the names in modelOverrides.
type layeredFacts struct {
	from           map[string]string
	overridesOrder []string
}

// settingsFactsKey holds the layeredFacts of the settings layeredSettings gives. No setting Claude Code reads
// has this name.
const settingsFactsKey = ""

func layeredFactsOf(settings object) *layeredFacts {
	facts, _ := settings[settingsFactsKey].(*layeredFacts)
	return facts
}

// layeredSettings lays layers over each other where they decide how Claude Code compacts, as Claude Code
// 2.1.289 does: each env variable, modelOverrides entry and autoCompactEnabled is the last layer's that sets
// it, as the settings are merged; autoCompactWindow is the last layer's that sets it, and of the windows
// modelSettings gives models of their own, those of that layer and the layers after it count, the last
// one's where several give a model one (its gOt). Each model's entry keeps the name it has in its layer.
func layeredSettings(layers []settingsLayer) object {
	settings, env, overrides := object{}, object{}, object{}
	facts := &layeredFacts{from: map[string]string{}}
	for _, layer := range layers {
		for name, value := range getMap(layer.data, "env") {
			env[name], facts.from["env."+name] = value, layer.fileOf("env."+name)
		}
		maps.Copy(overrides, modelOverridesOf(layer.data))
		if value, isSwitch := layer.data["autoCompactEnabled"].(bool); isSwitch {
			settings["autoCompactEnabled"], facts.from["autoCompactEnabled"] = value, layer.fileOf("autoCompactEnabled")
		}
	}
	if len(overrides) > 1 {
		facts.overridesOrder = []string{}
		for _, layer := range layers {
			own := modelOverridesOf(layer.data)
			for _, name := range layer.order("modelOverrides") {
				if _, kept := own[name]; kept && !slices.Contains(facts.overridesOrder, name) {
					facts.overridesOrder = append(facts.overridesOrder, name)
				}
			}
		}
	}
	settings["env"], settings["modelOverrides"], settings[settingsFactsKey] = env, overrides, facts
	type ownWindow struct {
		name, file string
		value      any
	}
	var top any
	topFile, owns := "", map[string]ownWindow{}
	var naming modelNaming
	named := false
	for _, layer := range layers {
		mine := map[string]ownWindow{}
		if models := getMap(layer.data, "modelSettings"); ownWindows(models) {
			if !named {
				naming, named = modelNamingOf(settings), true
			}
			order := layer.order("modelSettings")
			for _, name := range sortedKeys(models) {
				key := naming.key(name)
				if _, chosen := mine[key]; key == "" || chosen {
					continue
				}
				if chosen, value, found := modelCompactWindowIn(models, naming, key, order); found {
					mine[key] = ownWindow{chosen, layer.fileOf("modelSettings." + chosen), value}
				}
			}
		}
		if value := layer.data["autoCompactWindow"]; validWindowSetting(value) {
			top, topFile, owns = value, layer.fileOf("autoCompactWindow"), mine
		} else {
			maps.Copy(owns, mine)
		}
	}
	if top != nil {
		settings["autoCompactWindow"], facts.from["autoCompactWindow"] = top, topFile
	}
	if len(owns) > 0 {
		models := object{}
		for _, own := range owns {
			models[own.name] = object{"autoCompactWindow": own.value}
			facts.from["modelSettings."+own.name] = own.file
		}
		settings["modelSettings"] = models
	}
	return settings
}

func validWindowSetting(value any) bool {
	_, valid := windowSetting(value)
	return valid
}

// settingFrom is text, which names setting of settings, with the file the setting comes from before it where
// that is not the person's settings.json.
func settingFrom(settings object, setting, text string) string {
	if facts := layeredFactsOf(settings); facts != nil && facts.from[setting] != "" {
		return facts.from[setting] + ": " + text
	}
	return text
}

// variableFrom is text, which names the variable name, with the settings file it comes from before it where
// that is not the person's settings.json; the bare text where the variable comes from the process.
func variableFrom(settings object, name, text string) string {
	if getString(getMap(settings, "env"), name) == "" {
		return text
	}
	return settingFrom(settings, "env."+name, text)
}
