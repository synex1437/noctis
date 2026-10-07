package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// claudeCodeModels are Claude Code 2.1.293's own answers, worked out by its code, in
// testdata/claudecode-models.json: for a model and a settings.json text, the name modelSettings files the
// model under (keys) and the autoCompactWindow that applies to sessions on it (windows, null for none).
type claudeCodeModels struct {
	Settings []string `json:"settings"`
	Keys     [][]any  `json:"keys"`
	Windows  [][]any  `json:"windows"`
}

func readClaudeCodeModels(t *testing.T) claudeCodeModels {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "claudecode-models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture claudeCodeModels
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Keys) < 1000 || len(fixture.Windows) < 500 {
		t.Fatalf("testdata/claudecode-models.json holds %d keys and %d windows, fewer than it was made with", len(fixture.Keys), len(fixture.Windows))
	}
	return fixture
}

// settingsFrom puts a settings.json text in place and gives what the engine reads of it.
func settingsFrom(t *testing.T, text string) object {
	t.Helper()
	if err := os.WriteFile(files.settings, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	read := readJSONShared(files.settings)
	if !read.ok {
		t.Fatalf("settings %s: %s", text, read.err)
	}
	return read.data
}

func TestTheEngineFilesEveryModelUnderTheNameClaudeCodeFilesItUnder(t *testing.T) {
	sandboxFiles(t)
	fixture := readClaudeCodeModels(t)
	for _, row := range fixture.Keys {
		model, text, want := row[0].(string), fixture.Settings[int(row[1].(float64))], row[2].(string)
		if got := modelNamingOf(settingsFrom(t, text)).key(model); got != want {
			t.Errorf("%q with settings %s: filed under %q, Claude Code files it under %q", model, text, got, want)
		}
	}
}

func TestTheEngineTakesTheWindowClaudeCodeTakesForEveryModel(t *testing.T) {
	sandboxFiles(t)
	clearCompactionVariables(t)
	fixture := readClaudeCodeModels(t)
	for _, row := range fixture.Windows {
		model, text := row[0].(string), fixture.Settings[int(row[1].(float64))]
		want, set := row[2].(float64)
		if got, ok := settingsCompactWindow(settingsFrom(t, text), model); ok != set || ok && got != want {
			t.Errorf("%q with settings %s: window %v (set %v), Claude Code's %v", model, text, got, ok, row[2])
		}
	}
}

// runLean runs script, an ES module, with node, hooks/lean.js's path and input's as its arguments, and gives
// what it prints. No node skips the test.
func runLean(t *testing.T, script string, input any) []byte {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node to run hooks/lean.js with")
	}
	data := filepath.Join(t.TempDir(), "input.json")
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(data, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(node, "--input-type=module", "-e", script, filepath.Join(repoRoot(), "hooks", "lean.js"), data)
	output, err := command.Output()
	if err != nil {
		stderr := ""
		if exit, isExit := err.(*exec.ExitError); isExit {
			stderr = string(exit.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	return output
}

const leanFixtureScript = `
import { readFileSync } from "node:fs"
import { pathToFileURL } from "node:url"
const [lean, input] = process.argv.slice(1)
const { modelKey, modelNaming, compactionPoint } = await import(pathToFileURL(lean))
const fixture = JSON.parse(readFileSync(input, "utf8"))
const naming = (settings) => modelNaming(settings.env ?? {}, settings.modelOverrides)
const wrong = []
for (const [model, index, want] of fixture.keys) {
  const settings = JSON.parse(fixture.settings[index])
  const got = modelKey(model, naming(settings))
  if (got !== want) wrong.push(JSON.stringify(model) + " with settings " + fixture.settings[index] + ": filed under " + JSON.stringify(got) + ", Claude Code files it under " + JSON.stringify(want))
}
// The window settings set shows as the point it moves in a window larger than any it can set.
const big = 2000000, open = compactionPoint(big, {})
for (const [model, index, want] of fixture.windows) {
  const settings = JSON.parse(fixture.settings[index])
  const point = compactionPoint(big, { settings, model, naming: naming(settings) })
  const got = point === open ? null : point - open + big
  if (got !== want) wrong.push(JSON.stringify(model) + " with settings " + fixture.settings[index] + ": window " + got + ", Claude Code's " + want)
}
console.log(JSON.stringify(wrong))
`

func TestTheLeanModuleFilesModelsAndTakesWindowsAsClaudeCodeDoes(t *testing.T) {
	fixture := readClaudeCodeModels(t)
	var wrong []string
	if err := json.Unmarshal(runLean(t, leanFixtureScript, fixture), &wrong); err != nil {
		t.Fatal(err)
	}
	for _, line := range wrong {
		t.Error(line)
	}
}

const leanPointScript = `
import { readFileSync } from "node:fs"
import { pathToFileURL } from "node:url"
const [lean, input] = process.argv.slice(1)
const { modelNaming, compactionPoint } = await import(pathToFileURL(lean))
const points = JSON.parse(readFileSync(input, "utf8")).map(({ settings, model, window }) => {
  const env = settings.env ?? {}
  return compactionPoint(window, { variable: env.CLAUDE_CODE_AUTO_COMPACT_WINDOW, percent: env.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE,
    settings, model, maxOutput: env.CLAUDE_CODE_MAX_OUTPUT_TOKENS, naming: modelNaming(env, settings.modelOverrides) })
})
console.log(JSON.stringify(points))
`

// The lean module asks for a compaction on its way to the point where Claude Code compacts, and the guard
// watches the same point: both reckon it alike for every window variable, percent, reply limit,
// autoCompactWindow and model's own entry.
func TestTheLeanModuleReckonsEveryPointAsTheEngineDoes(t *testing.T) {
	sandboxFiles(t)
	clearCompactionVariables(t)
	t.Setenv(maxOutputTokensVar, "")
	variables := []any{nil, "500000", "5e5", "500,000", "500_000", "50000", "2000000", "abc", "0", "-5", " 300000 ", "300000.7", "1.5e5", "300k", "1e6", "\uff11\uff10\uff10"}
	percents := []any{nil, "60", "60%", "0", "100", "101", " 50.5", "abc", "1e1", "-3", ".5"}
	limits := []any{nil, "4096", "16,000", "64000", "0", "big", "8192", "19999", " 1000 "}
	windows := []any{nil, float64(313000), float64(600000), float64(140000), 313000.5, "auto", float64(50000), float64(1e6)}
	entries := []any{nil, object{"claude-opus-5-5": object{"autoCompactWindow": float64(140000)}}, object{"opus": object{"autoCompactWindow": "auto"}},
		object{"us.anthropic.claude-sonnet-4-5-20250929-v1:0": object{"autoCompactWindow": float64(173000)}, "claude-sonnet-5-5": object{"autoCompactWindow": float64(633000)}}}
	providers := []object{{}, {"CLAUDE_CODE_USE_BEDROCK": "1"}, {"CLAUDE_CODE_USE_MANTLE": "1"}}
	models := []string{"claude-opus-5-5", "opus[1m]", "claude-sonnet-5-5", "sonnet", "haiku", ""}
	random := rand.New(rand.NewSource(7))
	pick := func(values []any) any { return values[random.Intn(len(values))] }
	type point struct {
		Settings object  `json:"settings"`
		Model    string  `json:"model"`
		Window   float64 `json:"window"`
	}
	cases := make([]point, 0, 3000)
	for len(cases) < cap(cases) {
		env := object{}
		for name, value := range providers[random.Intn(len(providers))] {
			env[name] = value
		}
		for name, values := range map[string][]any{autoCompactWindowVar: variables, autoCompactPercentVar: percents, maxOutputTokensVar: limits} {
			if value := pick(values); value != nil {
				env[name] = value
			}
		}
		settings := object{"env": env}
		if value := pick(windows); value != nil {
			settings["autoCompactWindow"] = value
		}
		if value := pick(entries); value != nil {
			settings["modelSettings"] = value
		}
		cases = append(cases, point{settings, models[random.Intn(len(models))], []float64{200000, 1e6, 500000}[random.Intn(3)]})
	}
	var lean []float64
	if err := json.Unmarshal(runLean(t, leanPointScript, cases), &lean); err != nil {
		t.Fatal(err)
	}
	for index, item := range cases {
		text, _ := json.Marshal(item.Settings)
		if got, _ := compactionPoint(settingsFrom(t, string(text)), item.Model, item.Window); got != lean[index] {
			t.Errorf("%q in a %v window with settings %s: the engine reckons %v, the lean module %v", item.Model, item.Window, text, got, lean[index])
		}
	}
}

const leanLayersScript = `
import { readFileSync } from "node:fs"
import { pathToFileURL } from "node:url"
const [lean, input] = process.argv.slice(1)
const { modelNaming, compactionPoint } = await import(pathToFileURL(lean))
const points = JSON.parse(readFileSync(input, "utf8")).map(({ layers, model, window }) => {
  const files = layers.map((text) => JSON.parse(text))
  const env = Object.assign({}, ...files.map((file) => file.env))
  const overrides = Object.assign({}, ...files.map((file) => file.modelOverrides))
  return compactionPoint(window, { variable: env.CLAUDE_CODE_AUTO_COMPACT_WINDOW, percent: env.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE,
    settings: files, model, maxOutput: env.CLAUDE_CODE_MAX_OUTPUT_TOKENS, naming: modelNaming(env, overrides) })
})
console.log(JSON.stringify(points))
`

// The lean module reads each settings file Claude Code reads and lays them over each other itself, as the
// engine does: both reckon the same point for any files, each with its own windows, models' own windows in
// the order the file has them, variables and model overrides.
func TestTheLeanModuleLaysTheSettingsFilesOverEachOtherAsTheEngineDoes(t *testing.T) {
	sandboxFiles(t)
	clearCompactionVariables(t)
	t.Setenv(maxOutputTokensVar, "")
	windows := []string{"313000", "600000", "140000", "233000", "313000.5", `"auto"`, "50000", "1000000", `"300000"`, "null"}
	names := []string{"claude-opus-5-5", "opus", "us.anthropic.claude-opus-5-5", "claude-opus-5-5-20260101", "my-opus", "claude-sonnet-5-5", "sonnet", "claude-haiku-4-5"}
	overrides := []string{`{"claude-opus-5-5":"my-opus"}`, `{"claude-opus-4-6":"my-opus","claude-opus-5-5":"my-opus"}`, `{"claude-sonnet-5-5":"my-opus"}`}
	envs := []string{`{"CLAUDE_CODE_USE_BEDROCK":"1"}`, `{"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":"60"}`, `{"CLAUDE_CODE_MAX_OUTPUT_TOKENS":"4096"}`, `{"CLAUDE_CODE_AUTO_COMPACT_WINDOW":"500000"}`}
	models := []string{"claude-opus-5-5", "opus[1m]", "my-opus", "claude-sonnet-5-5", "sonnet", "haiku", ""}
	random := rand.New(rand.NewSource(11))
	quoted := func(text string) string {
		encoded, _ := json.Marshal(text)
		return string(encoded)
	}
	entry := func() string {
		window := windows[random.Intn(len(windows))]
		switch random.Intn(6) {
		case 0:
			return `{}`
		case 1:
			return `"fast"`
		case 2:
			return `{"effort":"high","autoCompactWindow":` + window + `}`
		}
		return `{"autoCompactWindow":` + window + `}`
	}
	file := func() string {
		fields := []string{}
		if random.Intn(2) == 0 {
			fields = append(fields, `"autoCompactWindow":`+windows[random.Intn(len(windows))])
		}
		if random.Intn(3) > 0 {
			own := []string{}
			for _, at := range random.Perm(len(names))[:1+random.Intn(3)] {
				own = append(own, quoted(names[at])+":"+entry())
			}
			fields = append(fields, `"modelSettings":{`+strings.Join(own, ",")+"}")
		}
		if random.Intn(5) == 0 {
			fields = append(fields, `"modelOverrides":`+overrides[random.Intn(len(overrides))])
		}
		if random.Intn(4) == 0 {
			fields = append(fields, `"env":`+envs[random.Intn(len(envs))])
		}
		random.Shuffle(len(fields), func(i, j int) { fields[i], fields[j] = fields[j], fields[i] })
		return "{" + strings.Join(fields, ",") + "}"
	}
	type point struct {
		Layers []string `json:"layers"`
		Model  string   `json:"model"`
		Window float64  `json:"window"`
	}
	cases := make([]point, 0, 3000)
	for len(cases) < cap(cases) {
		layers := make([]string, 1+random.Intn(4))
		for index := range layers {
			layers[index] = file()
		}
		cases = append(cases, point{layers, models[random.Intn(len(models))], []float64{200000, 1e6, 500000}[random.Intn(3)]})
	}
	var lean []float64
	if err := json.Unmarshal(runLean(t, leanLayersScript, cases), &lean); err != nil {
		t.Fatal(err)
	}
	for index, item := range cases {
		layers := make([]settingsLayer, len(item.Layers))
		for at, text := range item.Layers {
			decoded, err := decodeJSON([]byte(text))
			data, isObject := decoded.(object)
			if err != nil || !isObject {
				t.Fatalf("file %s: %v", text, err)
			}
			layers[at] = settingsLayer{data: data, content: []byte(text), file: fmt.Sprintf("file %d", at+1)}
		}
		if got, _ := compactionPoint(layeredSettings(layers), item.Model, item.Window); got != lean[index] {
			t.Errorf("%q in a %v window with the files %s: the engine reckons %v, the lean module %v", item.Model, item.Window, strings.Join(item.Layers, " "), got, lean[index])
		}
	}
}

func TestBedrockIDsTakeTheRegionPrefixClaudeCodeGivesThem(t *testing.T) {
	for _, row := range []struct {
		env  object
		want string
	}{
		{object{}, "us"},
		{object{"AWS_REGION": "eu-west-1"}, "eu"},
		{object{"AWS_REGION": " ap-southeast-2 "}, "apac"},
		{object{"AWS_REGION": "us-gov-west-1", "ANTHROPIC_BEDROCK_REGION_PREFIX": "eu"}, "us-gov"},
		{object{"AWS_REGION": "ca-central-1"}, "global"},
		{object{"AWS_REGION": "EU-WEST-1"}, "global"},
		{object{"AWS_REGION": "not a region", "AWS_DEFAULT_REGION": "eu-north-1"}, "eu"},
		{object{"AWS_REGION": "eu-west-3", "ANTHROPIC_BEDROCK_REGION_PREFIX": " jp "}, "jp"},
		{object{"ANTHROPIC_BEDROCK_REGION_PREFIX": "EU"}, "us"},
		{object{"ANTHROPIC_BEDROCK_REGION_PREFIX": "us-gov"}, "us"},
	} {
		if got := bedrockPrefixOf(row.env); got != row.want {
			t.Errorf("%v: prefix %q, want %q", row.env, got, row.want)
		}
	}
	naming := modelNamingOf(object{"env": object{"CLAUDE_CODE_USE_BEDROCK": "1", "AWS_REGION": "eu-west-1"},
		"modelOverrides": object{"claude-sonnet-5-5": "eu.anthropic.claude-sonnet-4-5-20250929-v1:0"}})
	if got := naming.key("sonnet"); got != "claude-sonnet-5-5" {
		t.Errorf("sonnet on Bedrock in eu-west-1, whose id modelOverrides gives claude-sonnet-5-5: filed under %q, Claude Code files it under claude-sonnet-5-5", got)
	}
}

func TestOnlyAnAnthropicAPIBaseURLKeepsClaudeCodeOnItsOwnAPI(t *testing.T) {
	for base, want := range map[string]bool{
		"": true, "https://api.anthropic.com": true, "https://API.Anthropic.COM/v1": true, "https://api.anthropic.com:443": true,
		"  https://api.anthropic.com  ": true, "https://user:pw@api.anthropic.com": true, "https://api.anthropic.com\t/x": true,
		"http://api.anthropic.com:80": true, "https://api.anthropic.com:0443": true, "wss://api.anthropic.com:443": true,
		"https://proxy.example.com": false, "https://api.anthropic.com:8443": false, "api.anthropic.com": false,
		"https://api.anthropic.com.": false, "foo://api.anthropic.com:1": false, "https://xn--api.anthropic.com": false,
	} {
		if got := anthropicURL(base); got != want {
			t.Errorf("ANTHROPIC_BASE_URL %q: on Anthropic's API %v, want %v", base, got, want)
		}
	}
}

func TestSwitchesAreReadWithTheWhiteSpaceJavaScriptTrims(t *testing.T) {
	for value, want := range map[string]bool{"1": true, " TRUE ": true, "\ufeff1": true, "on\u3000": true, "1\u0085": false, "0": false, "": false} {
		if got := switchIsOn(value); got != want {
			t.Errorf("switch %q: on %v, want %v", value, got, want)
		}
	}
}

func TestAModelOverrideThatIsNotTextSetsAllOfThemAside(t *testing.T) {
	overrides := object{"claude-opus-5-5": "my-opus", "claude-sonnet-5-5": float64(7)}
	if got := modelNamingOf(object{"modelOverrides": overrides}).key("my-opus"); got != "my-opus" {
		t.Errorf("my-opus beside an override that is not text: filed under %q, want my-opus, as Claude Code sets modelOverrides aside", got)
	}
	delete(overrides, "claude-sonnet-5-5")
	if got := modelNamingOf(object{"modelOverrides": overrides}).key("my-opus"); got != "claude-opus-5-5" {
		t.Errorf("my-opus that modelOverrides maps claude-opus-5-5 to: filed under %q, want claude-opus-5-5", got)
	}
}

// Claude Code goes through modelOverrides and modelSettings in the order settings.json has them; the engine
// reads that order from the file, where JSON.parse would leave it.
func TestTheFirstOfSeveralNamesIsTheOneSettingsJSONHasFirst(t *testing.T) {
	sandboxFiles(t)
	text := `{"modelOverrides":{"claude-opus-4-7":"shared","claude-opus-4-6":"shared","claude-opus-4-7":"shared"},` +
		`"modelSettings":{"us.anthropic.claude-opus-5-5":{"autoCompactWindow":173000},"claude-opus-5-5-20260101":{"autoCompactWindow":233000}}}`
	settings := settingsFrom(t, text)
	if got := modelNamingOf(settings).key("shared"); got != "claude-opus-4-7" {
		t.Errorf("shared, which two overrides map to: filed under %q, want claude-opus-4-7, the first in the file", got)
	}
	if got, _ := settingsCompactWindow(settings, "opus"); got != 173000 {
		t.Errorf("opus with two other spellings of its model: window %v, want 173000 from the first in the file", got)
	}
	if order := objectOrder([]byte(text), "modelOverrides"); strings.Join(order, " ") != "claude-opus-4-7 claude-opus-4-6" {
		t.Errorf("modelOverrides order %v, want each name where it first stands", order)
	}
	for _, broken := range []string{`[]`, `{"modelOverrides":{"a":}}`, `{"modelOverrides":[]}`, `{}`} {
		if order := objectOrder([]byte(broken), "modelOverrides"); order != nil {
			t.Errorf("%s: order %v, want none", broken, order)
		}
	}
	if order := objectOrder([]byte(`{"modelOverrides":{"a":"1"},"modelOverrides":{"b":"1","c":{"d":[1,{"e":2}]}}}`), "modelOverrides"); strings.Join(order, " ") != "b c" {
		t.Errorf("order %v, want the last modelOverrides's names, b c", order)
	}
}

// Claude Code loads a hooks module only when each variable it reads is named where it is read, so hooks/lean.js
// spells out the variables the naming of models takes: each under its own name, and every one the engine reads.
func TestTheLeanModuleReadsEveryVariableTheNamingOfModelsTakes(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(repoRoot(), "hooks", "lean.js"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	start := strings.Index(source, "async function namingOf(")
	if start < 0 {
		t.Fatal("hooks/lean.js has no namingOf")
	}
	body, _, _ := strings.Cut(source[start:], "\n}\n")
	var read []string
	for _, match := range regexp.MustCompile(`(\w+): await attempt\(\(\) => \$\.env\.get\("(\w+)"\)\)`).FindAllStringSubmatch(body, -1) {
		if match[1] != match[2] {
			t.Errorf("namingOf files %s under %s", match[2], match[1])
		}
		read = append(read, match[2])
	}
	want := slices.Clone(modelNameVariables)
	slices.Sort(read)
	slices.Sort(want)
	if !slices.Equal(read, want) {
		t.Errorf("namingOf reads %v, want the variables the engine reads, %v", read, want)
	}
}
