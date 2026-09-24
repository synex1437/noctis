export const SHIPPED = Object.freeze({ lean: true, compactAtPercent: 70, keepTurns: 6, maxToolResultChars: 2000, instructions: "" })

const READ_ONLY = new Set(["Read", "Grep", "Glob", "LS", "WebFetch", "WebSearch", "NotebookRead"])
const REMINDER = /<system-reminder>[\s\S]*?<\/system-reminder>\s*/g
const DECIMAL = /^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/
const LOG_LINES = 200
const SESSIONS_KEPT = 50
const PRECOMPUTE_SKIP = "noctis lean compaction prunes the conversation when it compacts, so a summary computed ahead would be thrown away"
const COMPACTION_BAND = 6
const BUILTIN_THRESHOLDS = Object.freeze({ session5h: 92, weeklyAll: 89 })
const WINDOW_THRESHOLDS = new Map([["five_hour", "session5h"], ["seven_day", "weeklyAll"]])

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
  compactAtPercent: percentOf,
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

export function policyOf(shipped, own) {
  const base = settle(SHIPPED, shipped)
  return own === undefined ? base : settle(base, isPlainObject(own) ? own : undefined)
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

function outcomes(rows) {
  const answered = new Map()
  for (const row of rows) {
    for (const result of Array.isArray(row?.toolResults) ? row.toolResults : []) {
      if (typeof result?.tool_use_id === "string") answered.set(result.tool_use_id, result.isError === true)
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
      const failed = use.isError === true || answered.get(use.tool_use_id) === true
      calls.push({ id: use.tool_use_id, key: `${use.tool} ${canonical(use.input ?? {})}`, tool: use.tool, good: answered.has(use.tool_use_id) && !failed })
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

function truthy(value) {
  return ["1", "true", "yes", "on"].includes(String(value ?? "").trim().toLowerCase())
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
      const mark = context.policy.compactAtPercent
      if (!mark) return answered
      const usage = await $.session.usage()
      const percent = usage?.context?.percent
      if (typeof percent !== "number") return answered
      if (percent < mark) {
        disarmed.delete(context.sid)
        return answered
      }
      if (disarmed.has(context.sid) || (await ownCompactionOff($, context.account))) return answered
      if (nearPausePoint(usage.rateLimits, context.pausePoints, await $.clock.now())) {
        debug($, `early compaction at ${percent}% put off: a usage window is within ${COMPACTION_BAND} points of its pause point`)
        return answered
      }
      disarmed.add(context.sid)
      early = { sid: context.sid, percent }
      try {
        await $.session.compact(context.policy.instructions ? { instructions: context.policy.instructions } : {})
      } catch (err) {
        debug($, `early compaction at ${percent}% not run: ${err}`)
      } finally {
        early = undefined
      }
    } catch (err) {
      debug($, `turn end skipped: ${err}`)
    }
    return answered
  })
}
