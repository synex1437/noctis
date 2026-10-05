export const SHIPPED = Object.freeze({ lean: true, earlyAtPercent: 90, keepTurns: 6, maxToolResultChars: 2000, instructions: "Write the summary in short sections: the user's requests and intent, quoting any requirement they set; the task or queue item in hand, its acceptance criteria, what is done and what is left; each file changed or created and why; each build, test or check command run and its exit status, marking any that timed out, were killed or exited non-zero as UNVERIFIED, to be run again; errors met and how each was fixed, quoting any not fixed yet; decisions and approaches ruled out, each with its reason; open questions; the next step. Leave out file contents, tool output and search results that can be read again." })

const READ_ONLY = new Set(["Read", "Grep", "Glob", "LS", "WebFetch", "WebSearch", "NotebookRead"])
const REMINDER = /<system-reminder>[\s\S]*?<\/system-reminder>\s*/g
const UNCHANGED_NOTE = "File unchanged since last read"
const DECIMAL = /^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/
const LOG_LINES = 200
const SESSIONS_KEPT = 50
const PRECOMPUTE_SKIP = "noctis lean compaction prunes the conversation when it compacts, so a summary computed ahead would be thrown away"
const COMPACTION_BAND = 6
const BUILTIN_THRESHOLDS = Object.freeze({ session5h: 92 })
const WINDOW_THRESHOLDS = new Map([["five_hour", "session5h"]])
const WINDOW_MIN = 100000
const WINDOW_MAX = 1000000
const REPLY_TOKENS = 20000
const BUFFER_TOKENS = 13000
const EXPONENT = /^[+-]?(\d+(\.\d*)?|\.\d+)[eE][+-]?\d+$/
const GROUPED = /^[+-]?\d{1,3}([_,\u00A0\u202F ])\d{3}(?:\1\d{3})*$/
const GROUP_MARKS = /[_,\u00A0\u202F ]/g
// The models Claude Code 2.1.289's catalog knows, and the three Claude 3 models it still names.
const CATALOG_MODELS = ["claude-3-5-haiku", "claude-haiku-4-5", "claude-3-5-sonnet", "claude-3-7-sonnet", "claude-sonnet-4-0", "claude-sonnet-4-5", "claude-sonnet-4-6", "claude-sonnet-5", "claude-sonnet-5-5", "claude-opus-4-0", "claude-opus-4-1", "claude-opus-4-5", "claude-opus-4-6", "claude-opus-4-7", "claude-opus-4-8", "claude-opus-5", "claude-opus-5-5", "claude-fable-5", "claude-fable-5-1", "claude-mythos-5", "claude-mythos-5-1", "claude-3-opus", "claude-3-sonnet", "claude-3-haiku"]
// The models Claude Code looks for inside a name it cannot parse, in its order; claude-opus-4 and
// claude-sonnet-4 count where no minor version follows.
const MODEL_LADDER = ["claude-fable-5-1", "claude-fable-5", "claude-mythos-5-1", "claude-mythos-5", "claude-opus-5-5", "claude-opus-5", "claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6", "claude-opus-4-5", "claude-opus-4-1", "claude-opus-4", "claude-sonnet-5-5", "claude-sonnet-5", "claude-sonnet-4-6", "claude-sonnet-4-5", "claude-sonnet-4", "claude-haiku-4-5", "claude-3-7-sonnet", "claude-3-5-sonnet", "claude-3-5-haiku", "claude-3-opus", "claude-3-sonnet", "claude-3-haiku"]
// The models Claude Code's catalog gives its aliases, by provider ("" for any other); opusplan is sonnet and
// best is opus (Claude Code takes fable for an account that may use Fable, which noctis cannot tell).
const ALIAS_MODELS = { opus: { "": "claude-opus-5-5", foundry: "claude-opus-4-6" }, sonnet: { "": "claude-sonnet-5-5", bedrock: "claude-sonnet-4-5", vertex: "claude-sonnet-4-5", foundry: "claude-sonnet-4-5", mantle: "claude-sonnet-4-5", anthropicAws: "claude-sonnet-4-6" }, haiku: { "": "claude-haiku-4-5" }, fable: { "": "claude-fable-5-1" } }
// The ids of the models ALIAS_MODELS names: on Anthropic's API (firstParty), which modelOverrides names a model
// by, and on each provider, which an alias stands for there. Where a provider has none, as Mantle for
// claude-sonnet-4-5, Claude Code takes claude-haiku-4-5's, the first model of its catalog it has one for. A
// Bedrock id starts with the region's prefix in place of us.
const MODEL_IDS = {
  "claude-opus-5-5": { firstParty: "claude-opus-5-5", bedrock: "us.anthropic.claude-opus-5-5", vertex: "claude-opus-5-5", foundry: "claude-opus-5-5", anthropicAws: "claude-opus-5-5", anthropicGoogleCloud: "claude-opus-5-5", mantle: "anthropic.claude-opus-5-5" },
  "claude-opus-4-6": { firstParty: "claude-opus-4-6", bedrock: "us.anthropic.claude-opus-4-6-v1", vertex: "claude-opus-4-6", foundry: "claude-opus-4-6", anthropicAws: "claude-opus-4-6", anthropicGoogleCloud: "claude-opus-4-6" },
  "claude-sonnet-5-5": { firstParty: "claude-sonnet-5-5", bedrock: "us.anthropic.claude-sonnet-5-5", vertex: "claude-sonnet-5-5", foundry: "claude-sonnet-5-5", anthropicAws: "claude-sonnet-5-5", anthropicGoogleCloud: "claude-sonnet-5-5", mantle: "anthropic.claude-sonnet-5-5" },
  "claude-sonnet-4-6": { firstParty: "claude-sonnet-4-6", bedrock: "us.anthropic.claude-sonnet-4-6", vertex: "claude-sonnet-4-6", foundry: "claude-sonnet-4-6", anthropicAws: "claude-sonnet-4-6", anthropicGoogleCloud: "claude-sonnet-4-6" },
  "claude-sonnet-4-5": { firstParty: "claude-sonnet-4-5-20250929", bedrock: "us.anthropic.claude-sonnet-4-5-20250929-v1:0", vertex: "claude-sonnet-4-5@20250929", foundry: "claude-sonnet-4-5", anthropicAws: "claude-sonnet-4-5-20250929", anthropicGoogleCloud: "claude-sonnet-4-5-20250929" },
  "claude-haiku-4-5": { firstParty: "claude-haiku-4-5-20251001", bedrock: "us.anthropic.claude-haiku-4-5-20251001-v1:0", vertex: "claude-haiku-4-5@20251001", foundry: "claude-haiku-4-5", anthropicAws: "claude-haiku-4-5-20251001", anthropicGoogleCloud: "claude-haiku-4-5-20251001", mantle: "anthropic.claude-haiku-4-5" },
  "claude-fable-5-1": { firstParty: "claude-fable-5-1", bedrock: "us.anthropic.claude-fable-5-1", vertex: "claude-fable-5-1", foundry: "claude-fable-5-1", anthropicAws: "claude-fable-5-1", anthropicGoogleCloud: "claude-fable-5-1", mantle: "anthropic.claude-fable-5-1" },
}
const BEDROCK_PREFIXES = ["us", "eu", "apac", "jp", "au", "global"]
const ALIAS_VARIABLES = { opus: "ANTHROPIC_DEFAULT_OPUS_MODEL", sonnet: "ANTHROPIC_DEFAULT_SONNET_MODEL", haiku: "ANTHROPIC_DEFAULT_HAIKU_MODEL", fable: "ANTHROPIC_DEFAULT_FABLE_MODEL" }
const PROVIDERS = [["CLAUDE_CODE_USE_BEDROCK", "bedrock"], ["CLAUDE_CODE_USE_FOUNDRY", "foundry"], ["CLAUDE_CODE_USE_ANTHROPIC_AWS", "anthropicAws"], ["CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD", "anthropicGoogleCloud"], ["CLAUDE_CODE_USE_MANTLE", "mantle"], ["CLAUDE_CODE_USE_VERTEX", "vertex"]]
const LEGACY_OPUS = ["claude-opus-4-20250514", "claude-opus-4-1-20250805", "claude-opus-4-0", "claude-opus-4-1"]
const NATIVE_1M = ["claude-sonnet-5", "claude-sonnet-5-5", "claude-opus-4-7", "claude-opus-4-8", "claude-opus-5", "claude-opus-5-5", "claude-fable-5", "claude-fable-5-1", "claude-mythos-5", "claude-mythos-5-1", "claude-mythos-preview"]
const BEDROCK_REGIONS = ["us", "eu", "apac", "jp", "au", "us-gov", "global"]
const DEFAULT_PORTS = { http: "80", https: "443", ws: "80", wss: "443", ftp: "21", file: "" }
const ONE_MILLION = /\[1m\]/i
const ONE_MILLION_TAIL = /(?:\[1m\])+$/i
const PROVIDER_MODEL = /^(?:([a-z-]+)\.)?anthropic\.(claude-.*)$/
const MODEL_TAIL = /^(?:-fast|-latest)?(?:-v\d{1,3}@\d{8}|[-@]\d{8})?(?:-v\d{1,3}(?::\d{1,3})?)?$/
const DATE_SUFFIX = /-\d{8}$/

function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value)
}

function toNumber(value) {
  if (typeof value === "number") return Number.isFinite(value) ? value : undefined
  if (typeof value !== "string") return undefined
  const text = value.trim()
  return DECIMAL.test(text) ? Number(text) : undefined
}

function switchedOff(value) {
  return value === null || value === false || toNumber(value) === 0
}

function percentOf(value) {
  if (switchedOff(value)) return 0
  const number = toNumber(value)
  return number !== undefined && number > 0 && number <= 100 ? number : undefined
}

function countOf(value, least) {
  const number = toNumber(value)
  return number !== undefined && Number.isInteger(number) && number >= least ? number : undefined
}

const FIELDS = {
  lean: (value) => (typeof value === "boolean" ? value : undefined),
  earlyAtPercent: percentOf,
  keepTurns: (value) => countOf(value, 0),
  maxToolResultChars: (value) => countOf(value, 100),
  instructions: (value) => (typeof value === "string" ? value : undefined),
}

function settle(base, section) {
  const settled = {}
  for (const [key, read] of Object.entries(FIELDS)) {
    const own = isPlainObject(section) && key in section ? read(section[key]) : undefined
    settled[key] = own === undefined ? base[key] : own
  }
  return settled
}

// carried keeps early compaction off for a config of noctis before 8.6.0 that switched it off as
// compactAtPercent, a share of the whole window then.
function carried(section) {
  if (!isPlainObject(section) || !("compactAtPercent" in section) || "earlyAtPercent" in section) return section
  return switchedOff(section.compactAtPercent) ? { ...section, earlyAtPercent: 0 } : section
}

export function policyOf(shipped, own) {
  const base = settle(SHIPPED, carried(shipped))
  return own === undefined ? base : settle(base, isPlainObject(own) ? carried(own) : undefined)
}

// claudeInteger reads CLAUDE_CODE_AUTO_COMPACT_WINDOW as Claude Code reads it: an integer in exponent
// form, digits in groups of three, else the digits it starts with.
function claudeInteger(value) {
  const text = String(value).trim()
  if (text.length <= 32) {
    if (EXPONENT.test(text)) {
      const number = Number(text)
      return Number.isInteger(number) ? number : NaN
    }
    if (GROUPED.test(text)) return parseInt(text.replace(GROUP_MARKS, ""), 10)
  }
  return parseInt(text, 10)
}

function truthy(value) {
  return ["1", "true", "yes", "on"].includes(String(value ?? "").trim().toLowerCase())
}

// anthropicURL tells whether ANTHROPIC_BASE_URL leaves Claude Code on Anthropic's own API: unset, or a URL whose
// host is api.anthropic.com.
function anthropicURL(base) {
  if (!base) return true
  const text = String(base).replace(/[\t\n\r]/g, "").replace(/^[\u0000-\u0020]+|[\u0000-\u0020]+$/g, "")
  const parts = /^([a-zA-Z][a-zA-Z0-9+.-]*):\/\/(?:[^/?#]*@)?(\[[^\]]*\]|[^/?#:]*)(?::([^/?#]*))?(?:[/?#]|$)/.exec(text)
  if (!parts) return false
  const scheme = parts[1].toLowerCase()
  const special = Object.hasOwn(DEFAULT_PORTS, scheme)
  let host = parts[2]
  try {
    host = decodeURIComponent(host)
  } catch {
    return false
  }
  if (special) host = host.toLowerCase()
  if (parts[3]) {
    const port = /^\d+$/.test(parts[3]) ? Number(parts[3]) : NaN
    if (!special || !(port <= 65535) || String(port) !== DEFAULT_PORTS[scheme]) return false
  }
  return host === "api.anthropic.com"
}

// modelNaming is what Claude Code names a model by besides its id, as the environment and modelOverrides set
// it: the models its aliases stand for, the provider, and whether [1m] and the legacy Opus ids count.
export function modelNaming(env = {}, overrides = undefined) {
  const provider = PROVIDERS.find(([variable]) => truthy(env[variable]))?.[1] ?? "firstParty"
  const taken = isPlainObject(overrides) && Object.values(overrides).every((value) => typeof value === "string") ? overrides : {}
  const naming = { provider, bedrockPrefix: bedrockPrefixOf(env), overrides: taken, aliases: {} }
  for (const [alias, models] of Object.entries(ALIAS_MODELS)) {
    naming.aliases[alias] = textOf(env[ALIAS_VARIABLES[alias]]) || aliasModel(naming, models[provider] ?? models[""])
  }
  naming.aliases.opusplan = naming.aliases.sonnet
  naming.aliases.best = naming.aliases.opus
  naming.remap = !["bedrock", "foundry", "mantle", "vertex"].includes(provider) && !truthy(env.CLAUDE_CODE_DISABLE_LEGACY_MODEL_REMAP)
  naming.oneMillion = !truthy(env.CLAUDE_CODE_DISABLE_1M_CONTEXT)
  naming.firstParty = provider === "firstParty" && (truthy(env._CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL) || anthropicURL(env.ANTHROPIC_BASE_URL))
  const bare = naming.aliases.fable.replace(/\[1m\]/gi, "")
  if (bare !== naming.aliases.fable && naming.firstParty && naming.oneMillion && nativeOneMillion(naming, bare)) naming.aliases.fable = bare
  return naming
}

const FIRST_PARTY_NAMING = modelNaming()

function textOf(value) {
  return typeof value === "string" ? value.trim() : ""
}

// aliasModel is the id Claude Code takes for an alias whose catalog gives it model: the one modelOverrides maps
// the model's first-party id to, else the provider's id for it.
function aliasModel(naming, model) {
  const ids = MODEL_IDS[model]
  if (Object.hasOwn(naming.overrides, ids.firstParty) && naming.overrides[ids.firstParty]) return naming.overrides[ids.firstParty]
  const id = ids[naming.provider] ?? MODEL_IDS["claude-haiku-4-5"][naming.provider]
  return naming.provider === "bedrock" ? id.replace("us.", `${naming.bedrockPrefix}.`) : id
}

// bedrockPrefixOf is the region prefix Claude Code gives Bedrock ids: us-gov in a GovCloud region, else the one
// ANTHROPIC_BEDROCK_REGION_PREFIX names, else the region's (us, eu, apac or global). The region is AWS_REGION's,
// else AWS_DEFAULT_REGION's, else us-east-1 (Claude Code reads the AWS config's region before that; noctis
// does not).
function bedrockPrefixOf(env) {
  const region = [textOf(env.AWS_REGION), textOf(env.AWS_DEFAULT_REGION)].find((value) => /^[a-z]{2,}(?:-[a-z0-9]+){0,4}$/i.test(value)) ?? "us-east-1"
  if (region.startsWith("us-gov-")) return "us-gov"
  if (BEDROCK_PREFIXES.includes(textOf(env.ANTHROPIC_BEDROCK_REGION_PREFIX))) return textOf(env.ANTHROPIC_BEDROCK_REGION_PREFIX)
  return region.startsWith("us-") ? "us" : region.startsWith("eu-") ? "eu" : region.startsWith("ap-") ? "apac" : "global"
}

function trimOneMillion(model) {
  return model.replace(/\[1m\]$/i, "")
}

function withOneMillion(model, marked) {
  return marked ? `${model.replace(ONE_MILLION_TAIL, "")}[1m]` : model
}

// parseModel parses a Claude model name as Claude Code's Hu does.
function parseModel(model) {
  let name = model.trim().toLowerCase()
  if (name === "" || /\s/.test(name)) return undefined
  name = name.replace(/\[[12]m\]$/, "")
  name = name.slice(name.lastIndexOf("/") + 1)
  const provider = PROVIDER_MODEL.exec(name)
  if (provider) {
    if (provider[1] !== undefined && !BEDROCK_REGIONS.includes(provider[1])) return undefined
    name = provider[2]
  }
  let parse
  let found = /^claude-([a-z]+)-(\d{1,2})(?!\d)(?:-(\d{1,2})(?!\d))?/.exec(name)
  if (found) parse = { family: found[1], major: Number(found[2]), minor: Number(found[3] ?? 0), legacy: false, base: found[0] }
  else if ((found = /^claude-(\d{1,2})(?!\d)(?:-(\d{1,2})(?!\d))?-([a-z]+)/.exec(name))) parse = { family: found[3], major: Number(found[1]), minor: Number(found[2] ?? 0), legacy: true, base: found[0] }
  else return undefined
  const rest = name.slice(parse.base.length)
  if (rest !== "" && !/^[-@]/.test(rest)) return undefined
  parse.trailer = !MODEL_TAIL.test(rest)
  return parse
}

// canonicalModel is the model Claude Code's RS takes a name in lower case for.
function canonicalModel(name) {
  const parse = parseModel(name)
  if (parse && !parse.trailer) {
    const known = CATALOG_MODELS.find((id) => {
      const own = parseModel(id)
      return own.family === parse.family && own.legacy === parse.legacy && own.major === parse.major && own.minor === parse.minor
    })
    return known ?? parse.base.replace(DATE_SUFFIX, "")
  }
  for (const id of MODEL_LADDER) {
    if (id === "claude-opus-4" || id === "claude-sonnet-4" ? new RegExp(`${id}(?!-\\d(?!\\d))`).test(name) : name.includes(id)) return id === "claude-opus-4" || id === "claude-sonnet-4" ? `${id}-0` : id
  }
  return name.replace(DATE_SUFFIX, "")
}

function knownModel(id) {
  const bare = trimOneMillion(id)
  return CATALOG_MODELS.includes(bare) || bare === "claude-mythos-preview"
}

// identify is the model Claude Code takes a name for: the first model modelOverrides maps back from it, else
// its canonical model.
function identify(naming, model) {
  for (const [name, value] of Object.entries(naming.overrides)) {
    if (value === model && knownModel(canonicalModel(name.toLowerCase()))) return canonicalModel(name.toLowerCase())
  }
  return canonicalModel(model.toLowerCase())
}

function nativeOneMillion(naming, model) {
  const bare = trimOneMillion(model)
  if (NATIVE_1M.includes(trimOneMillion(identify(naming, bare)))) return true
  return bare !== model && NATIVE_1M.includes(trimOneMillion(identify(naming, model)))
}

// resolve is the model a name stands for, as Claude Code's Tt gives it.
function resolve(naming, model) {
  const trimmed = model.trim()
  const lower = trimmed.toLowerCase()
  const marked = naming.oneMillion && ONE_MILLION.test(lower)
  const name = marked ? trimOneMillion(lower).trim() : lower
  if (name === "fable") return marked && !naming.firstParty && !ONE_MILLION.test(naming.aliases.fable) ? `${naming.aliases.fable}[1m]` : naming.aliases.fable
  if (["opus", "opusplan", "sonnet", "haiku"].includes(name)) return withOneMillion(naming.aliases[name], marked)
  if (name === "best") return naming.aliases.best
  if (naming.remap && LEGACY_OPUS.includes(name)) return withOneMillion(naming.aliases.opus, marked)
  if (marked && naming.firstParty && name.includes("fable") && nativeOneMillion(naming, name)) return trimmed.replace(ONE_MILLION_TAIL, "").trim()
  return marked ? `${trimmed.replace(ONE_MILLION_TAIL, "").trim()}[1m]` : trimmed
}

// modelKey is the name Claude Code 2.1.289 files a model's own settings under in modelSettings, its mV, for a
// session's model and an entry's name alike: the id of the model an alias stands for; the catalog id of a
// model's dated, [1m], Bedrock, Vertex and Foundry ids; the model modelOverrides maps a provider's id back to;
// else the name in lower case without [1m] or a date. "" for none.
export function modelKey(model, naming = FIRST_PARTY_NAMING) {
  return typeof model === "string" ? trimOneMillion(identify(naming, resolve(naming, model))) : ""
}

// windowOf is an autoCompactWindow as Claude Code's settings take it: a whole number of tokens from
// WINDOW_MIN to WINDOW_MAX.
function windowOf(value) {
  return Number.isInteger(value) && value >= WINDOW_MIN && value <= WINDOW_MAX ? value : undefined
}

// windowTable is what Claude Code's gOt finds in the settings files it lays over each other, given in its
// order: the autoCompactWindow of the last that sets one, and the models' own windows of that file and the
// files after it, a later file's over an earlier one's. In one file the entry under the name Claude Code files
// a model under comes before the model's other names, and of those the first; entries Claude Code does not
// take ("auto" and a window are taken) are passed over.
function windowTable(layers, naming) {
  let top
  let models = new Map()
  for (const settings of layers) {
    if (!isPlainObject(settings)) continue
    const own = new Map()
    if (isPlainObject(settings.modelSettings)) {
      for (const [name, entry] of Object.entries(settings.modelSettings)) {
        if (Object.hasOwn(Object.prototype, name) || !isPlainObject(entry)) continue
        const window = entry.autoCompactWindow
        if (window !== "auto" && windowOf(window) === undefined) continue
        const key = modelKey(name, naming)
        if (key && (name === key || !own.has(key))) own.set(key, window)
      }
    }
    if (windowOf(settings.autoCompactWindow) !== undefined) {
      top = settings.autoCompactWindow
      models = own
    } else models = new Map([...models, ...own])
  }
  return { top, models }
}

// settingsWindow is the window settings give sessions on model: the model's own entry in modelSettings before
// autoCompactWindow; "auto" there leaves the window to Claude Code. settings is a settings object, or the
// settings files Claude Code lays over each other, in its order.
function settingsWindow(settings, model, naming = FIRST_PARTY_NAMING) {
  const table = windowTable(Array.isArray(settings) ? settings : [settings], naming)
  const key = typeof model === "string" ? modelKey(model, naming) : ""
  return windowOf(key && table.models.has(key) ? table.models.get(key) : table.top)
}

// replyTokens is how much of the window Claude Code keeps for the summary: 20000 tokens, or the fewer that
// CLAUDE_CODE_MAX_OUTPUT_TOKENS lets a reply have.
function replyTokens(maxOutput) {
  const limit = maxOutput ? claudeInteger(maxOutput) : NaN
  return !Number.isNaN(limit) && limit > 0 ? Math.min(limit, REPLY_TOKENS) : REPLY_TOKENS
}

// compactionPoint is how many tokens of context a session on model, in a window of window tokens, holds
// when Claude Code compacts it, reckoned as Claude Code reckons it: CLAUDE_CODE_AUTO_COMPACT_WINDOW, or
// else autoCompactWindow (the model's own entry in modelSettings first), lowers the window; 20000 tokens
// (fewer where CLAUDE_CODE_MAX_OUTPUT_TOKENS is lower) are kept for the summary and 13000 more;
// CLAUDE_AUTOCOMPACT_PCT_OVERRIDE's percent of the rest comes sooner when it is the smaller.
export function compactionPoint(window, { variable, percent, settings, model, maxOutput, naming } = {}) {
  let effective = window
  const configured = variable ? claudeInteger(variable) : NaN
  if (!Number.isNaN(configured) && configured > 0) effective = Math.min(window, Math.max(WINDOW_MIN, Math.min(configured, WINDOW_MAX)))
  else {
    const own = settingsWindow(settings, model, naming)
    if (own !== undefined) effective = Math.min(window, own)
  }
  const usable = effective - replyTokens(maxOutput)
  const share = percent ? parseFloat(percent) : NaN
  if (!Number.isNaN(share) && share > 0 && share <= 100) return Math.min(Math.floor(usable * (share / 100)), usable - BUFFER_TOKENS)
  return usable - BUFFER_TOKENS
}

function pausePointsOf(shipped, own) {
  const base = isPlainObject(shipped) ? shipped : undefined
  const thresholds = isPlainObject(own) ? { ...base, ...own } : base
  const points = new Map()
  for (const [key, builtin] of Object.entries(BUILTIN_THRESHOLDS)) {
    if (!isPlainObject(thresholds) || !(key in thresholds)) continue
    const point = percentOf(thresholds[key])
    if (point === undefined) points.set(key, percentOf(base?.[key]) || builtin)
    else if (point > 0) points.set(key, point)
  }
  return points
}

function nearPausePoint(limits, points, now) {
  for (const limit of Array.isArray(limits) ? limits : []) {
    const point = points.get(WINDOW_THRESHOLDS.get(limit?.kind))
    if (point === undefined || typeof limit.percentUsed !== "number" || !(Date.parse(limit.resetsAt) > now)) continue
    if (limit.percentUsed >= point - COMPACTION_BAND) return true
  }
  return false
}

function cut(text, max) {
  if (typeof text !== "string" || text.length <= max) return text
  let head = Math.floor(max / 2)
  let tail = text.length - (max - head)
  if (/[\uD800-\uDBFF]/.test(text[head - 1] ?? "")) head -= 1
  if (/[\uDC00-\uDFFF]/.test(text[tail] ?? "")) tail += 1
  return `${text.slice(0, head)}\n[noctis: ${tail - head} chars trimmed]\n${text.slice(tail)}`
}

function cutInput(value, max) {
  if (typeof value === "string") return cut(value, max)
  if (Array.isArray(value)) {
    const items = value.map((item) => cutInput(item, max))
    return items.some((item, index) => item !== value[index]) ? items : value
  }
  if (isPlainObject(value)) {
    const entries = Object.entries(value).map(([key, item]) => [key, cutInput(item, max)])
    return entries.some(([key, item]) => item !== value[key]) ? Object.fromEntries(entries) : value
  }
  return value
}

function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`
  if (isPlainObject(value)) return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`).join(",")}}`
  return JSON.stringify(value) ?? "null"
}

function tailStart(rows, keepTurns) {
  if (keepTurns <= 0) return rows.length
  let seen = 0
  for (let index = rows.length - 1; index >= 0; index -= 1) {
    if (rows[index]?.role === "assistant" && ++seen === keepTurns) return index
  }
  return 0
}

function selfContained(answer) {
  return answer?.isError !== true && answer?.result?.type !== "file_unchanged" && !(typeof answer?.text === "string" && answer.text.startsWith(UNCHANGED_NOTE))
}

function outcomes(rows) {
  const answered = new Map()
  for (const row of rows) {
    for (const result of Array.isArray(row?.toolResults) ? row.toolResults : []) {
      if (typeof result?.tool_use_id === "string") answered.set(result.tool_use_id, selfContained(result))
    }
  }
  return answered
}

function supersededReads(rows) {
  const answered = outcomes(rows)
  const calls = []
  for (const row of rows) {
    if (row?.role !== "assistant" || !Array.isArray(row.toolUses)) continue
    for (const use of row.toolUses) {
      if (!READ_ONLY.has(use?.tool) || typeof use.tool_use_id !== "string") continue
      calls.push({ id: use.tool_use_id, key: `${use.tool} ${canonical(use.input ?? {})}`, tool: use.tool, good: answered.get(use.tool_use_id) === true && selfContained(use) })
    }
  }
  const superseded = new Map()
  const laterGood = new Set()
  for (let index = calls.length - 1; index >= 0; index -= 1) {
    const { id, key, tool, good } = calls[index]
    if (laterGood.has(key)) superseded.set(id, tool)
    if (good) laterGood.add(key)
  }
  return superseded
}

function withoutHandle(row, changes) {
  const { handle, ...rest } = row
  return { ...rest, ...changes }
}

function pruneUser(row, policy, superseded) {
  const changes = {}
  if (typeof row.text === "string" && row.text.includes("<system-reminder>")) {
    const stripped = row.text.replace(REMINDER, "").trimEnd()
    const results = Array.isArray(row.toolResults) && row.toolResults.length > 0
    if (stripped !== row.text && (stripped.trim() !== "" || results)) changes.text = stripped
  }
  if (Array.isArray(row.toolResults)) {
    const results = row.toolResults.map((result) => {
      if (typeof result?.text !== "string") return result
      const tool = superseded.get(result.tool_use_id)
      const text = tool ? `[noctis: dropped, the same ${tool} ran again later]` : cut(result.text, policy.maxToolResultChars)
      if (text === result.text) return result
      const { result: stored, ...rest } = result
      return { ...rest, text }
    })
    if (results.some((result, index) => result !== row.toolResults[index])) changes.toolResults = results
  }
  return Object.keys(changes).length ? withoutHandle(row, changes) : row
}

function pruneAssistant(row, policy) {
  if (!Array.isArray(row.toolUses)) return row
  const uses = row.toolUses.map((use) => {
    if (!isPlainObject(use?.input)) return use
    const input = cutInput(use.input, policy.maxToolResultChars)
    return input === use.input ? use : { ...use, input }
  })
  return uses.some((use, index) => use !== row.toolUses[index]) ? withoutHandle(row, { toolUses: uses }) : row
}

export function prune(rows, policy) {
  if (!Array.isArray(rows)) return rows
  const settled = policyOf(policy, undefined)
  const start = tailStart(rows, settled.keepTurns)
  const superseded = supersededReads(rows)
  return rows.map((row, index) => {
    if (index >= start || !isPlainObject(row)) return row
    if (row.role === "user") return pruneUser(row, settled, superseded)
    if (row.role === "assistant") return pruneAssistant(row, settled)
    return row
  })
}

function textSize(value) {
  if (typeof value === "string") return value.length
  if (Array.isArray(value)) return value.reduce((sum, item) => sum + textSize(item), 0)
  if (isPlainObject(value)) return Object.values(value).reduce((sum, item) => sum + textSize(item), 0)
  return 0
}

function rowSize(row) {
  if (!isPlainObject(row)) return 0
  const uses = (Array.isArray(row.toolUses) ? row.toolUses : []).reduce((sum, use) => sum + textSize(use?.input), 0)
  const results = (Array.isArray(row.toolResults) ? row.toolResults : []).reduce((sum, result) => sum + textSize(result?.text), 0)
  return textSize(row.text) + uses + results
}

export function measure(before, after) {
  let changed = 0
  let trimmed = 0
  for (let index = 0; index < before.length; index += 1) {
    if (after[index] === before[index]) continue
    changed += 1
    trimmed += rowSize(before[index]) - rowSize(after[index])
  }
  return { changed, trimmed }
}

function trimSeparators(path) {
  return path.replace(/[\\/]+$/, "")
}

async function homeDir($) {
  const windows = /^([A-Za-z]:[\\/]|\\\\)/.test($.plugin.root ?? "")
  const home = windows ? await $.env.get("USERPROFILE") : await $.env.get("HOME")
  return home ? trimSeparators(home) : undefined
}

async function accountDir($) {
  const configured = await $.env.get("CLAUDE_CONFIG_DIR")
  if (configured) return trimSeparators(configured)
  const home = await homeDir($)
  return home ? `${home}/.claude` : undefined
}

function parseJson(text) {
  try {
    return JSON.parse(text.replace(/^﻿/, ""))
  } catch {
    return undefined
  }
}

async function readJson($, path) {
  try {
    return parseJson(await $.fs.read(path))
  } catch {
    return undefined
  }
}

function compactionOf(config) {
  return isPlainObject(config) ? config.compaction : undefined
}

function thresholdsOf(config) {
  return isPlainObject(config) ? config.thresholds : undefined
}

async function contextOf($) {
  const account = await accountDir($)
  const shipped = await readJson($, `${trimSeparators($.plugin.root ?? ".")}/config.default.json`)
  const own = account ? await readJson($, `${account}/noctis/config.json`) : undefined
  return { account, policy: policyOf(compactionOf(shipped), compactionOf(own)), pausePoints: pausePointsOf(thresholdsOf(shipped), thresholdsOf(own)), sid: await $.session.id() }
}

function debug($, text) {
  try {
    $.ui.log(`noctis lean: ${text}`, { to: "debug" })
  } catch {
    return
  }
}

async function globalConfig($, account) {
  if (account) {
    const legacy = await $.fs.read(`${account}/.config.json`).catch(() => undefined)
    if (typeof legacy === "string") return parseJson(legacy)
  }
  const configured = await $.env.get("CLAUDE_CONFIG_DIR")
  const base = configured ? trimSeparators(configured) : await homeDir($)
  if (!base) return undefined
  const suffix = (await $.env.get("CLAUDE_CODE_CUSTOM_OAUTH_URL")) ? "-custom-oauth" : ""
  return readJson($, `${base}/.claude${suffix}.json`)
}

async function ownCompactionOff($, account) {
  if (truthy(await $.env.get("DISABLE_AUTO_COMPACT")) || truthy(await $.env.get("DISABLE_COMPACT"))) return true
  const settings = await $.settings.read()
  if (isPlainObject(settings) && settings.autoCompactEnabled !== undefined) return settings.autoCompactEnabled === false
  const global = await globalConfig($, account)
  return isPlainObject(global) && global.autoCompactEnabled === false
}

async function attempt(read) {
  try {
    return await read()
  } catch {
    return undefined
  }
}

// settingsLayers are the settings files Claude Code lays over each other, in its order, each as
// $.settings.read gives it; undefined where it does not give them one by one. Each source is named where it is
// read, as each variable is.
async function settingsLayers($) {
  try {
    return await Promise.all([
      $.settings.read({ source: "user" }),
      $.settings.read({ source: "project" }),
      $.settings.read({ source: "local" }),
      $.settings.read({ source: "flag" }),
      $.settings.read({ source: "policy" }),
    ])
  } catch {
    return undefined
  }
}

// ownWindows tells whether settings give a model a window of its own.
function ownWindows(settings) {
  return isPlainObject(settings) && isPlainObject(settings.modelSettings) && Object.values(settings.modelSettings).some((entry) => isPlainObject(entry) && entry.autoCompactWindow !== undefined)
}

// namingOf reads the naming of models only where layers, the settings files, give a model a window of its own;
// modelOverrides are those of merged, the settings laid together. Claude Code loads a module only when each
// variable it reads is named where it is read, so every one is spelled out.
async function namingOf($, layers, merged) {
  if (!Array.isArray(layers) || !layers.some(ownWindows)) return undefined
  const env = {
    ANTHROPIC_DEFAULT_OPUS_MODEL: await attempt(() => $.env.get("ANTHROPIC_DEFAULT_OPUS_MODEL")),
    ANTHROPIC_DEFAULT_SONNET_MODEL: await attempt(() => $.env.get("ANTHROPIC_DEFAULT_SONNET_MODEL")),
    ANTHROPIC_DEFAULT_HAIKU_MODEL: await attempt(() => $.env.get("ANTHROPIC_DEFAULT_HAIKU_MODEL")),
    ANTHROPIC_DEFAULT_FABLE_MODEL: await attempt(() => $.env.get("ANTHROPIC_DEFAULT_FABLE_MODEL")),
    CLAUDE_CODE_USE_BEDROCK: await attempt(() => $.env.get("CLAUDE_CODE_USE_BEDROCK")),
    CLAUDE_CODE_USE_FOUNDRY: await attempt(() => $.env.get("CLAUDE_CODE_USE_FOUNDRY")),
    CLAUDE_CODE_USE_ANTHROPIC_AWS: await attempt(() => $.env.get("CLAUDE_CODE_USE_ANTHROPIC_AWS")),
    CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD: await attempt(() => $.env.get("CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD")),
    CLAUDE_CODE_USE_MANTLE: await attempt(() => $.env.get("CLAUDE_CODE_USE_MANTLE")),
    CLAUDE_CODE_USE_VERTEX: await attempt(() => $.env.get("CLAUDE_CODE_USE_VERTEX")),
    CLAUDE_CODE_DISABLE_LEGACY_MODEL_REMAP: await attempt(() => $.env.get("CLAUDE_CODE_DISABLE_LEGACY_MODEL_REMAP")),
    CLAUDE_CODE_DISABLE_1M_CONTEXT: await attempt(() => $.env.get("CLAUDE_CODE_DISABLE_1M_CONTEXT")),
    ANTHROPIC_BASE_URL: await attempt(() => $.env.get("ANTHROPIC_BASE_URL")),
    _CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL: await attempt(() => $.env.get("_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL")),
    AWS_REGION: await attempt(() => $.env.get("AWS_REGION")),
    AWS_DEFAULT_REGION: await attempt(() => $.env.get("AWS_DEFAULT_REGION")),
    ANTHROPIC_BEDROCK_REGION_PREFIX: await attempt(() => $.env.get("ANTHROPIC_BEDROCK_REGION_PREFIX")),
  }
  return modelNaming(env, isPlainObject(merged) ? merged.modelOverrides : undefined)
}

// shareOf is how full the context is, in percent of where Claude Code compacts it; of the whole window
// when the engine reports no token count or window; undefined when it reports neither.
async function shareOf($, context) {
  const { tokens, window, percent } = isPlainObject(context) ? context : {}
  if (typeof tokens === "number" && typeof window === "number" && window > 0) {
    const merged = await attempt(() => $.settings.read())
    const layers = (await settingsLayers($)) ?? [merged]
    const point = compactionPoint(window, {
      variable: await $.env.get("CLAUDE_CODE_AUTO_COMPACT_WINDOW"),
      percent: await $.env.get("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE"),
      settings: layers,
      model: await attempt(() => $.session.model()),
      maxOutput: await $.env.get("CLAUDE_CODE_MAX_OUTPUT_TOKENS"),
      naming: await namingOf($, layers, merged),
    })
    if (point > 0) return (100 * tokens) / point
  }
  return typeof percent === "number" ? percent : undefined
}

async function remember($, context) {
  if (!context.account || !context.sid) return
  const file = `${context.account}/noctis/lean.json`
  const stored = await readJson($, file)
  const sessions = isPlainObject(stored) && isPlainObject(stored.sessions) ? stored.sessions : {}
  if (typeof sessions[context.sid] === "number") return
  const at = Math.floor((await $.clock.now()) / 1000)
  const latest = Object.entries({ ...sessions, [context.sid]: at })
    .filter(([, when]) => typeof when === "number")
    .sort((a, b) => b[1] - a[1])
    .slice(0, SESSIONS_KEPT)
  await $.fs.write(file, `${JSON.stringify({ sessions: Object.fromEntries(latest) })}\n`)
}

async function record($, context, entry) {
  if (!context.account) return
  const file = `${context.account}/noctis/compact.log`
  const previous = await $.fs.read(file).catch(() => "")
  const kept = previous.split("\n").filter((line) => line.trim() !== "").slice(-(LOG_LINES - 1))
  kept.push(JSON.stringify(entry))
  await $.fs.write(file, `${kept.join("\n")}\n`)
}

export function register(on, options) {
  const disarmed = new Set()
  let early

  on("session.start", async ($, e, next) => {
    const started = await next(e)
    try {
      const context = await contextOf($)
      if (context.policy.lean) await remember($, context)
    } catch (err) {
      debug($, `session record not written: ${err}`)
    }
    return started
  })

  on("session.compact", async ($, e, next) => {
    if (!Array.isArray(e.messages)) return next(e)
    let context
    let trimming
    let pruned
    let counted
    try {
      context = await contextOf($)
      trimming = context.policy.lean && truthy(await $.env.get("DISABLE_PROMPT_CACHING"))
      if (trimming && e.trigger !== "precompute") {
        pruned = prune(e.messages, context.policy)
        counted = measure(e.messages, pruned)
      }
    } catch (err) {
      debug($, `left the compaction as it was: ${err}`)
      return next(e)
    }
    if (!context.policy.lean) return next(e)
    const instructions = e.instructions ?? (context.policy.instructions || undefined)
    if (!trimming) return next(instructions === e.instructions ? e : { ...e, instructions })
    if (e.trigger === "precompute") return { skip: PRECOMPUTE_SKIP }
    const handed = { ...e, messages: pruned }
    if (instructions !== undefined) handed.instructions = instructions
    const result = await next(handed)
    if (!Array.isArray(result?.messages)) return result
    try {
      const entry = { at: Math.floor((await $.clock.now()) / 1000), sid: context.sid, trigger: e.trigger, rows: e.messages.length, changed: counted.changed, trimmed: counted.trimmed }
      if (e.agentId) entry.agent = e.agentId
      if (early && e.trigger === "plugin" && early.sid === context.sid) entry.ctx = early.percent
      if (typeof result.tokensBefore === "number") entry.tokensBefore = result.tokensBefore
      if (typeof result.tokensAfter === "number") entry.tokensAfter = result.tokensAfter
      await record($, context, entry)
    } catch (err) {
      debug($, `compaction not logged: ${err}`)
    }
    return result
  })

  on("turn.complete", async ($, e, next) => {
    const answered = await next(e)
    if (e.agentId || e.reason !== "answer") return answered
    try {
      const context = await contextOf($)
      if (!context.policy.lean) return answered
      await remember($, context)
      const mark = context.policy.earlyAtPercent
      if (!mark) return answered
      const usage = await $.session.usage()
      const share = await shareOf($, usage?.context)
      if (share === undefined) return answered
      const percent = Math.round(share)
      if (share < mark) {
        disarmed.delete(context.sid)
        return answered
      }
      if (disarmed.has(context.sid) || (await ownCompactionOff($, context.account))) return answered
      if (nearPausePoint(usage.rateLimits, context.pausePoints, await $.clock.now())) {
        debug($, `early compaction at ${percent}% of the compaction point put off: the 5-hour window is within ${COMPACTION_BAND} points of its pause point`)
        return answered
      }
      disarmed.add(context.sid)
      early = { sid: context.sid, percent }
      try {
        await $.session.compact(context.policy.instructions ? { instructions: context.policy.instructions } : {})
      } catch (err) {
        debug($, `early compaction at ${percent}% of the compaction point not run: ${err}`)
      } finally {
        early = undefined
      }
    } catch (err) {
      debug($, `turn end skipped: ${err}`)
    }
    return answered
  })
}
