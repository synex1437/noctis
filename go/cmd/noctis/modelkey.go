package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Claude Code 2.1.289 files a model's own settings in modelSettings under one name for the model, the one its
// mV function gives: /autocompact saves a model's window there, and a session's model finds its entry by the
// same name. modelNaming.key transcribes mV with what it reads: the aliases and the models the environment
// gives them, modelOverrides, and the models Claude Code's catalog knows.

// claudeCatalogModels are the models Claude Code's catalog knows, and the three Claude 3 models it still names.
var claudeCatalogModels = []string{
	"claude-3-5-haiku", "claude-haiku-4-5", "claude-3-5-sonnet", "claude-3-7-sonnet", "claude-sonnet-4-0",
	"claude-sonnet-4-5", "claude-sonnet-4-6", "claude-sonnet-5", "claude-sonnet-5-5", "claude-opus-4-0",
	"claude-opus-4-1", "claude-opus-4-5", "claude-opus-4-6", "claude-opus-4-7", "claude-opus-4-8", "claude-opus-5",
	"claude-opus-5-5", "claude-fable-5", "claude-fable-5-1", "claude-mythos-5", "claude-mythos-5-1",
	"claude-3-opus", "claude-3-sonnet", "claude-3-haiku",
}

// claudeModelLadder are the models Claude Code looks for inside a name it cannot parse, in its order.
// claude-opus-4 and claude-sonnet-4 count only where no minor version follows, for claude-opus-4-0 and
// claude-sonnet-4-0.
var claudeModelLadder = []string{
	"claude-fable-5-1", "claude-fable-5", "claude-mythos-5-1", "claude-mythos-5", "claude-opus-5-5", "claude-opus-5",
	"claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6", "claude-opus-4-5", "claude-opus-4-1", "claude-opus-4",
	"claude-sonnet-5-5", "claude-sonnet-5", "claude-sonnet-4-6", "claude-sonnet-4-5", "claude-sonnet-4",
	"claude-haiku-4-5", "claude-3-7-sonnet", "claude-3-5-sonnet", "claude-3-5-haiku", "claude-3-opus",
	"claude-3-sonnet", "claude-3-haiku",
}

// claudeAliasModels are the models Claude Code's catalog gives its aliases, by provider ("" for any other), where
// no ANTHROPIC_DEFAULT_*_MODEL names one. opusplan is sonnet outside plan mode. best stands for what opus does,
// as it does for an account that may not use Fable; for one that may, Claude Code takes fable, which noctis
// cannot tell.
var claudeAliasModels = map[string]map[string]string{
	"opus":   {"": "claude-opus-5-5", "foundry": "claude-opus-4-6"},
	"sonnet": {"": "claude-sonnet-5-5", "bedrock": "claude-sonnet-4-5", "vertex": "claude-sonnet-4-5", "foundry": "claude-sonnet-4-5", "mantle": "claude-sonnet-4-5", "anthropicAws": "claude-sonnet-4-6"},
	"haiku":  {"": "claude-haiku-4-5"},
	"fable":  {"": "claude-fable-5-1"},
}

// claudeModelIDs are the ids of the models of claudeAliasModels: on Anthropic's API (firstParty), which
// modelOverrides names a model by, and on each provider, which an alias stands for there. Where a provider has
// none, as Mantle for claude-sonnet-4-5, Claude Code takes the first model of its catalog that it has one for:
// claude-haiku-4-5. A Bedrock id starts with the region's prefix in place of us.
var claudeModelIDs = map[string]map[string]string{
	"claude-opus-5-5": {"firstParty": "claude-opus-5-5", "bedrock": "us.anthropic.claude-opus-5-5", "vertex": "claude-opus-5-5", "foundry": "claude-opus-5-5",
		"anthropicAws": "claude-opus-5-5", "anthropicGoogleCloud": "claude-opus-5-5", "mantle": "anthropic.claude-opus-5-5"},
	"claude-opus-4-6": {"firstParty": "claude-opus-4-6", "bedrock": "us.anthropic.claude-opus-4-6-v1", "vertex": "claude-opus-4-6", "foundry": "claude-opus-4-6",
		"anthropicAws": "claude-opus-4-6", "anthropicGoogleCloud": "claude-opus-4-6"},
	"claude-sonnet-5-5": {"firstParty": "claude-sonnet-5-5", "bedrock": "us.anthropic.claude-sonnet-5-5", "vertex": "claude-sonnet-5-5", "foundry": "claude-sonnet-5-5",
		"anthropicAws": "claude-sonnet-5-5", "anthropicGoogleCloud": "claude-sonnet-5-5", "mantle": "anthropic.claude-sonnet-5-5"},
	"claude-sonnet-4-6": {"firstParty": "claude-sonnet-4-6", "bedrock": "us.anthropic.claude-sonnet-4-6", "vertex": "claude-sonnet-4-6", "foundry": "claude-sonnet-4-6",
		"anthropicAws": "claude-sonnet-4-6", "anthropicGoogleCloud": "claude-sonnet-4-6"},
	"claude-sonnet-4-5": {"firstParty": "claude-sonnet-4-5-20250929", "bedrock": "us.anthropic.claude-sonnet-4-5-20250929-v1:0", "vertex": "claude-sonnet-4-5@20250929",
		"foundry": "claude-sonnet-4-5", "anthropicAws": "claude-sonnet-4-5-20250929", "anthropicGoogleCloud": "claude-sonnet-4-5-20250929"},
	"claude-haiku-4-5": {"firstParty": "claude-haiku-4-5-20251001", "bedrock": "us.anthropic.claude-haiku-4-5-20251001-v1:0", "vertex": "claude-haiku-4-5@20251001",
		"foundry": "claude-haiku-4-5", "anthropicAws": "claude-haiku-4-5-20251001", "anthropicGoogleCloud": "claude-haiku-4-5-20251001", "mantle": "anthropic.claude-haiku-4-5"},
	"claude-fable-5-1": {"firstParty": "claude-fable-5-1", "bedrock": "us.anthropic.claude-fable-5-1", "vertex": "claude-fable-5-1", "foundry": "claude-fable-5-1",
		"anthropicAws": "claude-fable-5-1", "anthropicGoogleCloud": "claude-fable-5-1", "mantle": "anthropic.claude-fable-5-1"},
}

// bedrockPrefixes are the region prefixes ANTHROPIC_BEDROCK_REGION_PREFIX can name.
var bedrockPrefixes = []string{"us", "eu", "apac", "jp", "au", "global"}

// claudeAliasVariables are the variables that name the model of an alias.
var claudeAliasVariables = map[string]string{"opus": "ANTHROPIC_DEFAULT_OPUS_MODEL", "sonnet": "ANTHROPIC_DEFAULT_SONNET_MODEL", "haiku": "ANTHROPIC_DEFAULT_HAIKU_MODEL", "fable": "ANTHROPIC_DEFAULT_FABLE_MODEL"}

// claudeProviders are the variables that send Claude Code to another provider than Anthropic's API, in the
// order it reads them.
var claudeProviders = [][2]string{
	{"CLAUDE_CODE_USE_BEDROCK", "bedrock"}, {"CLAUDE_CODE_USE_FOUNDRY", "foundry"}, {"CLAUDE_CODE_USE_ANTHROPIC_AWS", "anthropicAws"},
	{"CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD", "anthropicGoogleCloud"}, {"CLAUDE_CODE_USE_MANTLE", "mantle"}, {"CLAUDE_CODE_USE_VERTEX", "vertex"},
}

// legacyOpusModels are the Opus 4 and 4.1 ids that Claude Code takes for opus on Anthropic's own API.
var legacyOpusModels = []string{"claude-opus-4-20250514", "claude-opus-4-1-20250805", "claude-opus-4-0", "claude-opus-4-1"}

// nativeOneMillionModels are the models with a 1M window of their own, needing no [1m]: the catalog's, and
// the preview model Claude Code counts with them.
var nativeOneMillionModels = []string{"claude-sonnet-5", "claude-sonnet-5-5", "claude-opus-4-7", "claude-opus-4-8", "claude-opus-5",
	"claude-opus-5-5", "claude-fable-5", "claude-fable-5-1", "claude-mythos-5", "claude-mythos-5-1", "claude-mythos-preview"}

// modelNameVariables are the variables modelNaming reads; a test leaves them unset.
var modelNameVariables = []string{"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL",
	"ANTHROPIC_DEFAULT_FABLE_MODEL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_USE_ANTHROPIC_AWS",
	"CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD", "CLAUDE_CODE_USE_MANTLE", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_DISABLE_LEGACY_MODEL_REMAP",
	"CLAUDE_CODE_DISABLE_1M_CONTEXT", "ANTHROPIC_BASE_URL", "_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL", "AWS_REGION", "AWS_DEFAULT_REGION",
	"ANTHROPIC_BEDROCK_REGION_PREFIX"}

var (
	oneMillionMark  = lazyRegexp(`(?i)\[1m\]`)
	oneMillionTail  = lazyRegexp(`(?i)(?:\[1m\])+$`)
	contextSuffix   = lazyRegexp(`\[[12]m\]$`)
	providerModelID = lazyRegexp(`^(?:([a-z-]+)\.)?anthropic\.(claude-.*)$`)
	awsRegion       = lazyRegexp(`(?i)^[a-z]{2,}(?:-[a-z0-9]+){0,4}$`)
	claudeModelTail = lazyRegexp(`^(?:-fast|-latest)?(?:-v\d{1,3}@\d{8}|[-@]\d{8})?(?:-v\d{1,3}(?::\d{1,3})?)?$`)
)

// bedrockRegions are the regions a Bedrock model id can start with, as us in us.anthropic.claude-opus-5-5.
var bedrockRegions = []string{"us", "eu", "apac", "jp", "au", "us-gov", "global"}

// javaScriptSpace is the white space JavaScript trims: Unicode's, less U+0085, and the byte order mark.
func javaScriptSpace(char rune) bool {
	return char == '\ufeff' || char != '\u0085' && unicode.IsSpace(char)
}

// modelNaming is what Claude Code names a model by besides its id, as settings and the environment set it.
type modelNaming struct {
	aliases       map[string]string
	provider      string
	bedrockPrefix string
	remap         bool
	oneMillion    bool
	firstParty    bool
	overrides     object
	// overridesOrder is the order Claude Code goes through overrides in, where several settings files give
	// them; nil where settings.json alone does, whose order is read from it.
	overridesOrder []string
}

// modelNamingOf reads the naming settings give and the environment, settings.json's env first.
func modelNamingOf(settings object) modelNaming {
	env := getMap(settings, "env")
	naming := modelNaming{aliases: map[string]string{}, provider: "firstParty", bedrockPrefix: bedrockPrefixOf(env), overrides: modelOverridesOf(settings)}
	if facts := layeredFactsOf(settings); facts != nil {
		naming.overridesOrder = facts.overridesOrder
	}
	for _, provider := range claudeProviders {
		if switchIsOn(envVariable(env, provider[0])) {
			naming.provider = provider[1]
			break
		}
	}
	for alias, models := range claudeAliasModels {
		model, set := models[naming.provider]
		if !set {
			model = models[""]
		}
		naming.aliases[alias] = naming.aliasModel(model)
		if own := strings.TrimFunc(envVariable(env, claudeAliasVariables[alias]), javaScriptSpace); own != "" {
			naming.aliases[alias] = own
		}
	}
	naming.aliases["opusplan"], naming.aliases["best"] = naming.aliases["sonnet"], naming.aliases["opus"]
	naming.remap = !slices.Contains([]string{"bedrock", "foundry", "mantle", "vertex"}, naming.provider) && !switchIsOn(envVariable(env, "CLAUDE_CODE_DISABLE_LEGACY_MODEL_REMAP"))
	naming.oneMillion = !switchIsOn(envVariable(env, "CLAUDE_CODE_DISABLE_1M_CONTEXT"))
	naming.firstParty = naming.provider == "firstParty" && (switchIsOn(envVariable(env, "_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL")) || anthropicURL(envVariable(env, "ANTHROPIC_BASE_URL")))
	naming.aliases["fable"] = naming.fableModel(naming.aliases["fable"])
	return naming
}

// aliasModel is the id Claude Code takes for an alias whose catalog gives it model: the one modelOverrides maps
// the model's first-party id to, else the provider's id for it.
func (naming modelNaming) aliasModel(model string) string {
	if overridden, _ := naming.overrides[claudeModelIDs[model]["firstParty"]].(string); overridden != "" {
		return overridden
	}
	id := claudeModelIDs[model][naming.provider]
	if id == "" {
		id = claudeModelIDs["claude-haiku-4-5"][naming.provider]
	}
	if naming.provider == "bedrock" {
		id = strings.Replace(id, "us.", naming.bedrockPrefix+".", 1)
	}
	return id
}

// bedrockPrefixOf is the region prefix Claude Code gives Bedrock ids: us-gov in a GovCloud region, else the one
// ANTHROPIC_BEDROCK_REGION_PREFIX names, else the region's (us, eu, apac or global). The region is AWS_REGION's,
// else AWS_DEFAULT_REGION's, else us-east-1 (Claude Code reads the AWS config's region before that; noctis
// does not).
func bedrockPrefixOf(env object) string {
	region := "us-east-1"
	for _, variable := range []string{"AWS_REGION", "AWS_DEFAULT_REGION"} {
		if value := strings.TrimFunc(envVariable(env, variable), javaScriptSpace); value != "" && awsRegion.MatchString(value) {
			region = value
			break
		}
	}
	if strings.HasPrefix(region, "us-gov-") {
		return "us-gov"
	}
	if prefix := strings.TrimFunc(envVariable(env, "ANTHROPIC_BEDROCK_REGION_PREFIX"), javaScriptSpace); slices.Contains(bedrockPrefixes, prefix) {
		return prefix
	}
	for _, area := range [][2]string{{"us-", "us"}, {"eu-", "eu"}, {"ap-", "apac"}} {
		if strings.HasPrefix(region, area[0]) {
			return area[1]
		}
	}
	return "global"
}

// modelOverridesOf is settings' modelOverrides as Claude Code takes it: none where a value is not text, for
// its schema then sets the whole field aside.
func modelOverridesOf(settings object) object {
	overrides := getMap(settings, "modelOverrides")
	for _, value := range overrides {
		if _, isText := value.(string); !isText {
			return nil
		}
	}
	return overrides
}

// fableModel is the model fable stands for, as Claude Code's bOe gives it: without [1m] on Anthropic's own
// API where the model has a 1M window of its own.
func (naming modelNaming) fableModel(model string) string {
	bare := oneMillionMark.ReplaceAllString(model, "")
	if bare != model && naming.firstParty && naming.oneMillion && naming.nativeOneMillion(bare) {
		return bare
	}
	return model
}

// anthropicURL tells whether ANTHROPIC_BASE_URL leaves Claude Code on Anthropic's own API: unset, or a URL
// whose host, as a browser's URL gives it, is api.anthropic.com.
func anthropicURL(base string) bool {
	if base == "" {
		return true
	}
	text := strings.Map(func(char rune) rune {
		if char == '\t' || char == '\n' || char == '\r' {
			return -1
		}
		return char
	}, strings.TrimFunc(base, func(char rune) bool { return char <= ' ' }))
	parsed, err := url.Parse(text)
	if err != nil || parsed.Scheme == "" || parsed.Opaque != "" {
		return false
	}
	host := parsed.Hostname()
	defaultPort, special := map[string]string{"http": "80", "https": "443", "ws": "80", "wss": "443", "ftp": "21", "file": ""}[parsed.Scheme]
	if special {
		host = strings.ToLower(host)
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number > 65535 || !special || strconv.Itoa(number) != defaultPort {
			return false
		}
	}
	return host == "api.anthropic.com"
}

// key is the name Claude Code files the settings of model under in modelSettings, for a session's model and
// an entry's name alike: the id of the model an alias stands for; the catalog id of a model's dated, [1m],
// Bedrock, Vertex and Foundry ids, and of an ARN that ends in one; the model modelOverrides maps a provider's
// id back to; else the name in lower case without [1m] or a date. "" for no name.
func (naming modelNaming) key(model string) string {
	return trimOneMillion(naming.identify(naming.resolve(model)))
}

// identify is the model Claude Code's Be takes a name for: the one modelOverrides maps it back to, else its
// canonical model.
func (naming modelNaming) identify(model string) string {
	if overridden, found := naming.overridden(model); found {
		return overridden
	}
	return canonicalModel(strings.ToLower(model))
}

// nativeOneMillion tells whether a name stands for a model with a 1M window of its own, as Claude Code's qI
// finds it: the name without the [1m] at its end, else the name as it is.
func (naming modelNaming) nativeOneMillion(model string) bool {
	bare := trimOneMillion(model)
	if slices.Contains(nativeOneMillionModels, trimOneMillion(naming.identify(bare))) {
		return true
	}
	return bare != model && slices.Contains(nativeOneMillionModels, trimOneMillion(naming.identify(model)))
}

// resolve is the model a name stands for, as Claude Code's Tt gives it: an alias's model, opus for a legacy
// Opus 4 id, with [1m] moved to the end, or dropped from a Fable model on Anthropic's own API that has a 1M
// window of its own.
func (naming modelNaming) resolve(model string) string {
	trimmed := strings.TrimFunc(model, javaScriptSpace)
	lower := strings.ToLower(trimmed)
	marked := naming.oneMillion && oneMillionMark.MatchString(lower)
	name := lower
	if marked {
		name = strings.TrimFunc(trimOneMillion(lower), javaScriptSpace)
	}
	switch name {
	case "fable":
		target := naming.aliases[name]
		if marked && !naming.firstParty && !oneMillionMark.MatchString(target) {
			target += "[1m]"
		}
		return target
	case "opus", "opusplan", "sonnet", "haiku":
		return withOneMillion(naming.aliases[name], marked)
	case "best":
		return naming.aliases[name]
	}
	if naming.remap && slices.Contains(legacyOpusModels, name) {
		return withOneMillion(naming.aliases["opus"], marked)
	}
	if marked && naming.firstParty && strings.Contains(name, "fable") && naming.nativeOneMillion(name) {
		return strings.TrimFunc(oneMillionTail.ReplaceAllString(trimmed, ""), javaScriptSpace)
	}
	if marked {
		return strings.TrimFunc(oneMillionTail.ReplaceAllString(trimmed, ""), javaScriptSpace) + "[1m]"
	}
	return trimmed
}

func withOneMillion(model string, marked bool) string {
	if !marked {
		return model
	}
	return oneMillionTail.ReplaceAllString(model, "") + "[1m]"
}

func trimOneMillion(model string) string {
	if len(model) >= 4 && strings.EqualFold(model[len(model)-4:], "[1m]") {
		return model[:len(model)-4]
	}
	return model
}

// overridden is the model modelOverrides maps model back to: that of the first entry whose value is model
// and whose name is a model Claude Code knows.
func (naming modelNaming) overridden(model string) (string, bool) {
	names := []string{}
	for _, name := range sortedKeys(naming.overrides) {
		if naming.overrides[name] == model && knownModel(canonicalModel(strings.ToLower(name))) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", false
	}
	chosen := names[0]
	if naming.overridesOrder == nil {
		chosen = firstInFile("modelOverrides", naming.overrides, names)
	} else if index := slices.IndexFunc(naming.overridesOrder, func(name string) bool { return slices.Contains(names, name) }); index >= 0 {
		chosen = naming.overridesOrder[index]
	}
	return canonicalModel(strings.ToLower(chosen)), true
}

// knownModel tells whether Claude Code knows a canonical model: one of its catalog, or the preview model.
func knownModel(id string) bool {
	id = trimOneMillion(id)
	return slices.Contains(claudeCatalogModels, id) || id == "claude-mythos-preview"
}

// firstInFile is the one of names, all in entries, that settings.json has first, where entries are its
// modelOverrides or modelSettings (field) as it holds them: Claude Code goes through them in the file's
// order. Else the first of names.
func firstInFile(field string, entries object, names []string) string {
	if len(names) > 1 {
		if read := readJSONShared(files.settings); read.ok && read.raw != nil && reflect.DeepEqual(getMap(read.data, field), entries) {
			for _, name := range objectOrder(bytes.TrimPrefix(read.raw, utf8BOM), field) {
				if slices.Contains(names, name) {
					return name
				}
			}
		}
	}
	return names[0]
}

// objectOrder is the order of the names in the object under field of the JSON object content, as
// JavaScript's JSON.parse leaves it: the last such field's, each name where it first stands; nil where
// content holds no such object.
func objectOrder(content []byte, field string) []string {
	decoder := json.NewDecoder(bytes.NewReader(content))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	var order []string
	for decoder.More() {
		name, err := decoder.Token()
		var value json.RawMessage
		if err != nil || decoder.Decode(&value) != nil {
			return nil
		}
		if name == field {
			order = objectKeys(value)
		}
	}
	return order
}

func objectKeys(content []byte) []string {
	decoder := json.NewDecoder(bytes.NewReader(content))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	order := []string{}
	for decoder.More() {
		token, err := decoder.Token()
		name, isText := token.(string)
		if err != nil || !isText || decoder.Decode(new(json.RawMessage)) != nil {
			return nil
		}
		if !slices.Contains(order, name) {
			order = append(order, name)
		}
	}
	return order
}

// canonicalModel is the model Claude Code's RS takes a name in lower case for: the catalog model it parses as,
// the version it parses as where the catalog has none, the catalog model found inside it, else the name
// without a date. Each provider id of the catalog parses as its model.
func canonicalModel(name string) string {
	if parsed, ok := parseClaudeModel(name); ok && !parsed.trailer {
		for index, known := range catalogVersions() {
			if known.sameVersion(parsed) {
				return claudeCatalogModels[index]
			}
		}
		return modelDateSuffix.ReplaceAllString(parsed.base, "")
	}
	for _, id := range claudeModelLadder {
		if id != "claude-opus-4" && id != "claude-sonnet-4" {
			if strings.Contains(name, id) {
				return id
			}
		} else if majorOnly(name, id) {
			return id + "-0"
		}
	}
	return modelDateSuffix.ReplaceAllString(name, "")
}

// catalogVersions are the versions claudeCatalogModels parse as, in their order.
var catalogVersions = sync.OnceValue(func() []claudeModelParse {
	versions := make([]claudeModelParse, len(claudeCatalogModels))
	for index, id := range claudeCatalogModels {
		versions[index], _ = parseClaudeModel(id)
	}
	return versions
})

// majorOnly tells whether prefix stands somewhere in name without a one or two digit minor version after it.
func majorOnly(name, prefix string) bool {
	for start := 0; ; {
		index := strings.Index(name[start:], prefix)
		if index < 0 {
			return false
		}
		after := name[start+index+len(prefix):]
		if !(len(after) >= 2 && after[0] == '-' && isDigit(after[1]) && (len(after) == 2 || !isDigit(after[2]))) {
			return true
		}
		start += index + 1
	}
}

func isDigit(char byte) bool {
	return char >= '0' && char <= '9'
}

// claudeModelParse is a Claude model name as Claude Code's Hu parses it.
type claudeModelParse struct {
	family       string
	major, minor int
	legacy       bool
	base         string
	trailer      bool
}

func (parse claudeModelParse) sameVersion(other claudeModelParse) bool {
	return parse.base != "" && !parse.trailer && parse.family == other.family && parse.legacy == other.legacy && parse.major == other.major && parse.minor == other.minor
}

// parseClaudeModel parses a Claude model name as Claude Code's Hu does: claude-<family>-<major>[-<minor>] or
// claude-<major>[-<minor>]-<family>, after a Bedrock region and anthropic., before a date, -v1:0 and the like
// (anything else after it is a trailer); none for a name with white space in it or no such version.
func parseClaudeModel(model string) (claudeModelParse, bool) {
	name := strings.ToLower(strings.TrimFunc(model, javaScriptSpace))
	if name == "" || strings.IndexFunc(name, javaScriptSpace) >= 0 {
		return claudeModelParse{}, false
	}
	name = contextSuffix.ReplaceAllString(name, "")
	name = name[strings.LastIndex(name, "/")+1:]
	if provider := providerModelID.FindStringSubmatch(name); provider != nil {
		if provider[1] != "" && !slices.Contains(bedrockRegions, provider[1]) {
			return claudeModelParse{}, false
		}
		name = provider[2]
	}
	parse, ok := claudeModelVersion(name)
	if !ok {
		return claudeModelParse{}, false
	}
	rest := name[len(parse.base):]
	if rest != "" && rest[0] != '-' && rest[0] != '@' {
		return claudeModelParse{}, false
	}
	parse.trailer = !claudeModelTail.MatchString(rest)
	return parse, true
}

// claudeModelVersion reads the version a name starts with: claude-<family>-<major>[-<minor>], else
// claude-<major>[-<minor>]-<family>, each number of one or two digits; a longer run of digits is no minor.
func claudeModelVersion(name string) (claudeModelParse, bool) {
	rest, found := strings.CutPrefix(name, "claude-")
	if !found {
		return claudeModelParse{}, false
	}
	letters := leadingRun(rest, isLower)
	if letters != "" && strings.HasPrefix(rest[len(letters):], "-") {
		if major, after, ok := versionNumber(rest[len(letters)+1:]); ok {
			parse := claudeModelParse{family: letters, major: major}
			if minor, last, ok := minorVersion(after); ok {
				parse.minor, after = minor, last
			}
			parse.base = name[:len(name)-len(after)]
			return parse, true
		}
	}
	major, after, ok := versionNumber(rest)
	if !ok {
		return claudeModelParse{}, false
	}
	parse := claudeModelParse{major: major, legacy: true}
	if minor, last, ok := minorVersion(after); ok {
		parse.minor, after = minor, last
	}
	family, found := strings.CutPrefix(after, "-")
	if parse.family = leadingRun(family, isLower); !found || parse.family == "" {
		return claudeModelParse{}, false
	}
	parse.base = name[:len(name)-len(family)+len(parse.family)]
	return parse, true
}

// versionNumber reads the one or two digits text starts with, where no third follows.
func versionNumber(text string) (int, string, bool) {
	digits := leadingRun(text, isDigit)
	if len(digits) == 0 || len(digits) > 2 {
		return 0, text, false
	}
	number := 0
	for index := 0; index < len(digits); index++ {
		number = number*10 + int(digits[index]-'0')
	}
	return number, text[len(digits):], true
}

func minorVersion(text string) (int, string, bool) {
	if !strings.HasPrefix(text, "-") {
		return 0, text, false
	}
	return versionNumber(text[1:])
}

func leadingRun(text string, take func(byte) bool) string {
	end := 0
	for end < len(text) && take(text[end]) {
		end++
	}
	return text[:end]
}

func isLower(char byte) bool {
	return char >= 'a' && char <= 'z'
}
