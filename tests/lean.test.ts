import { test, expect, describe, mock } from "claude-code/testing"
import { prune, measure, policyOf, register, SHIPPED } from "../hooks/lean.js"

const POLICY = { lean: true, earlyAtPercent: 90, keepTurns: 1, maxToolResultChars: 100, instructions: "" }

function prompt(text: string, handle = "") {
  return { role: "user", text, toolUses: [], ...(handle ? { handle } : {}) }
}

function call(id: string, tool: string, input: Record<string, unknown>, handle = "", text = "") {
  return { role: "assistant", text, toolUses: [{ tool_use_id: id, tool, input }], ...(handle ? { handle } : {}) }
}

function answer(id: string, text: string, handle = "", isError = false) {
  return { role: "user", text: "", toolUses: [], toolResults: [{ tool_use_id: id, text, isError }], ...(handle ? { handle } : {}) }
}

function reply(text: string, handle = "") {
  return { role: "assistant", text, toolUses: [], ...(handle ? { handle } : {}) }
}

describe("prune", () => {
  test("a long tool result in an older turn keeps its head and tail around a marker", () => {
    const long = "a".repeat(50) + "b".repeat(4900) + "c".repeat(50)
    const rows = [prompt("run the tests", "h1"), call("t1", "Bash", { command: "npm test" }, "h2"), answer("t1", long, "h3"), reply("done", "h4")]
    const out = prune(rows, POLICY)
    expect(out[2].toolResults[0].text).toBe("a".repeat(50) + "\n[noctis: 4900 chars trimmed]\n" + "c".repeat(50))
    expect(out[2].toolResults[0].tool_use_id).toBe("t1")
    expect(out[2].handle).toBeUndefined()
    expect(out[3]).toBe(rows[3])
  })

  test("a result at or under the cap is handed down as it is", () => {
    const rows = [prompt("x", "h1"), call("t1", "Bash", { command: "ls" }, "h2"), answer("t1", "z".repeat(100), "h3"), reply("done", "h4")]
    const out = prune(rows, POLICY)
    expect(out[2]).toBe(rows[2])
    expect(out[2].handle).toBe("h3")
  })

  test("a read-only call that ran again later with the same input loses its first result", () => {
    const rows = [
      prompt("look", "h1"),
      call("t1", "Read", { file_path: "/p/a.js" }, "h2"),
      answer("t1", "old a", "h3"),
      call("t2", "Read", { file_path: "/p/b.js" }, "h4"),
      answer("t2", "only b", "h5"),
      call("t3", "Read", { file_path: "/p/a.js", offset: 10 }, "h6"),
      answer("t3", "a from line 10", "h7"),
      call("t4", "Read", { file_path: "/p/a.js" }, "h8"),
      answer("t4", "new a", "h9"),
      reply("read them", "h10"),
    ]
    const out = prune(rows, POLICY)
    expect(out[2].toolResults[0].text).toBe("[noctis: dropped, the same Read ran again later]")
    expect(out[2].handle).toBeUndefined()
    expect(out[4]).toBe(rows[4])
    expect(out[6]).toBe(rows[6])
    expect(out[8]).toBe(rows[8])
  })

  test("a later Read answered only with the file-unchanged note does not replace the earlier result", () => {
    const unchanged = "File unchanged since last read. The content from the earlier Read tool_result in this conversation is still current — refer to that instead of re-reading."
    const note = { role: "user", text: "", toolUses: [], toolResults: [{ tool_use_id: "t2", text: unchanged, isError: false, result: { type: "file_unchanged", file: { filePath: "/p/config.ts" } } }], handle: "h5" }
    const rows = [
      prompt("why port 3000?", "h1"),
      call("t1", "Read", { file_path: "/p/config.ts" }, "h2"),
      answer("t1", "export const PORT = 3000", "h3"),
      call("t2", "Read", { file_path: "/p/config.ts" }, "h4"),
      note,
      reply("PORT is 3000", "h6"),
    ]
    expect(prune(rows, POLICY)[2]).toBe(rows[2])
    const textOnly = [...rows.slice(0, 4), answer("t2", unchanged, "h5"), rows[5]]
    expect(prune(textOnly, POLICY)[2]).toBe(textOnly[2])
  })

  test("a later call that failed does not replace an earlier result", () => {
    const rows = [
      prompt("look", "h1"),
      call("t1", "Grep", { pattern: "x", path: "/p" }, "h2"),
      answer("t1", "hits", "h3"),
      call("t2", "Grep", { path: "/p", pattern: "x" }, "h4"),
      answer("t2", "grep failed", "h5", true),
      reply("done", "h6"),
    ]
    const out = prune(rows, POLICY)
    expect(out[2]).toBe(rows[2])
  })

  test("a repeated call of a tool that changes things is never taken for a repeat read", () => {
    const rows = [
      prompt("go", "h1"),
      call("t1", "Bash", { command: "git status" }, "h2"),
      answer("t1", "clean", "h3"),
      call("t2", "Bash", { command: "git status" }, "h4"),
      answer("t2", "dirty", "h5"),
      reply("done", "h6"),
    ]
    const out = prune(rows, POLICY)
    expect(out[2]).toBe(rows[2])
  })

  test("the last keepTurns assistant messages and everything after the first of them stay the engine's own rows", () => {
    const long = "q".repeat(1000)
    const rows = [
      prompt("one", "h1"),
      call("t1", "Bash", { command: "a" }, "h2"),
      answer("t1", long, "h3"),
      call("t2", "Bash", { command: "b" }, "h4"),
      answer("t2", long, "h5"),
      call("t3", "Bash", { command: "c" }, "h6"),
      answer("t3", long, "h7"),
      reply("done", "h8"),
    ]
    const out = prune(rows, { ...POLICY, keepTurns: 2 })
    expect(out[2].handle).toBeUndefined()
    expect(out[4].handle).toBeUndefined()
    for (const index of [5, 6, 7]) expect(out[index]).toBe(rows[index])
  })

  test("keepTurns larger than the conversation changes nothing", () => {
    const rows = [prompt("one", "h1"), call("t1", "Bash", { command: "a" }, "h2"), answer("t1", "w".repeat(5000), "h3"), reply("done", "h4")]
    const out = prune(rows, { ...POLICY, keepTurns: 6 })
    rows.forEach((row, index) => expect(out[index]).toBe(row))
  })

  test("keepTurns 0 lets the newest rows be pruned too", () => {
    const rows = [prompt("one", "h1"), call("t1", "Bash", { command: "a" }, "h2"), answer("t1", "w".repeat(5000), "h3")]
    const out = prune(rows, { ...POLICY, keepTurns: 0 })
    expect(out[2].toolResults[0].text).toContain("[noctis: 4900 chars trimmed]")
  })

  test("system reminders leave an older user text, the prompt itself stays", () => {
    const rows = [prompt("fix it <system-reminder>\nnote\n</system-reminder>\nplease", "h1"), reply("ok", "h2"), prompt("next", "h3"), reply("done", "h4")]
    const out = prune(rows, POLICY)
    expect(out[0].text).toBe("fix it please")
    expect(out[0].handle).toBeUndefined()
    expect(out[2]).toBe(rows[2])
  })

  test("user prompts and assistant texts are never cut, however long", () => {
    const rows = [prompt("p".repeat(5000), "h1"), reply("r".repeat(5000), "h2"), prompt("next", "h3"), reply("done", "h4")]
    const out = prune(rows, POLICY)
    expect(out[0]).toBe(rows[0])
    expect(out[1]).toBe(rows[1])
  })

  test("a long string in a tool input is cut the same way, the other fields stay", () => {
    const rows = [prompt("write", "h1"), call("t1", "Write", { file_path: "/p/a.txt", content: "k".repeat(5000) }, "h2"), answer("t1", "written", "h3"), reply("done", "h4")]
    const out = prune(rows, POLICY)
    const use = out[1].toolUses[0]
    expect(use.input.file_path).toBe("/p/a.txt")
    expect(use.input.content).toContain("[noctis: 4900 chars trimmed]")
    expect(use.tool_use_id).toBe("t1")
    expect(use.tool).toBe("Write")
    expect(out[1].handle).toBeUndefined()
    expect(out[2]).toBe(rows[2])
  })

  test("strings nested in a tool input are cut too", () => {
    const rows = [prompt("edit", "h1"), call("t1", "MultiEdit", { edits: [{ old_string: "o".repeat(500), new_string: "n" }] }, "h2"), answer("t1", "ok", "h3"), reply("done", "h4")]
    const out = prune(rows, POLICY)
    expect(out[1].toolUses[0].input.edits[0].old_string).toContain("[noctis: 400 chars trimmed]")
    expect(out[1].toolUses[0].input.edits[0].new_string).toBe("n")
  })

  test("a changed tool result drops the stored record, an unchanged one keeps it", () => {
    const rows = [
      prompt("x", "h1"),
      { role: "assistant", text: "", toolUses: [{ tool_use_id: "t1", tool: "Bash", input: { command: "a" } }, { tool_use_id: "t2", tool: "Bash", input: { command: "b" } }], handle: "h2" },
      { role: "user", text: "", toolUses: [], toolResults: [{ tool_use_id: "t1", text: "y".repeat(500), isError: false, result: { stdout: "y".repeat(500) } }, { tool_use_id: "t2", text: "short", isError: false, result: { stdout: "short" } }], handle: "h3" },
      reply("done", "h4"),
    ]
    const out = prune(rows, POLICY)
    expect(out[2].toolResults[0].result).toBeUndefined()
    expect(out[2].toolResults[1]).toBe(rows[2].toolResults[1])
  })

  test("what is not a list comes back as it is", () => {
    expect(prune(undefined, POLICY)).toBeUndefined()
    expect(prune(null, POLICY)).toBe(null)
  })

  test("the rows handed in are never modified", () => {
    const rows = [prompt("x", "h1"), call("t1", "Bash", { command: "a" }, "h2"), answer("t1", "y".repeat(500), "h3"), reply("done", "h4")]
    const copy = JSON.parse(JSON.stringify(rows))
    prune(rows, POLICY)
    expect(rows).toEqual(copy)
  })
})

describe("measure", () => {
  test("counts the rows that changed and the characters that went", () => {
    const rows = [prompt("x", "h1"), call("t1", "Bash", { command: "a" }, "h2"), answer("t1", "y".repeat(500), "h3"), reply("done", "h4")]
    const out = prune(rows, POLICY)
    const counted = measure(rows, out)
    expect(counted.changed).toBe(1)
    expect(counted.trimmed).toBe(500 - out[2].toolResults[0].text.length)
  })
})

describe("policyOf", () => {
  test("the shipped values when the account has none", () => {
    expect(policyOf(undefined, undefined)).toEqual(SHIPPED)
    expect(policyOf({ lean: true, earlyAtPercent: 90, keepTurns: 6, maxToolResultChars: 2000, instructions: SHIPPED.instructions }, {})).toEqual(SHIPPED)
  })

  test("a wrong type or range falls back to the shipped value", () => {
    const shipped = { lean: true, earlyAtPercent: 90, keepTurns: 6, maxToolResultChars: 2000, instructions: "" }
    expect(policyOf(shipped, { lean: "yes" }).lean).toBe(true)
    expect(policyOf(shipped, { earlyAtPercent: 150 }).earlyAtPercent).toBe(90)
    expect(policyOf(shipped, { earlyAtPercent: -5 }).earlyAtPercent).toBe(90)
    expect(policyOf(shipped, { earlyAtPercent: "abc" }).earlyAtPercent).toBe(90)
    expect(policyOf(shipped, { earlyAtPercent: [80] }).earlyAtPercent).toBe(90)
    expect(policyOf(shipped, { keepTurns: -1 }).keepTurns).toBe(6)
    expect(policyOf(shipped, { keepTurns: 2.5 }).keepTurns).toBe(6)
    expect(policyOf(shipped, { maxToolResultChars: 50 }).maxToolResultChars).toBe(2000)
    expect(policyOf(shipped, { instructions: 5 }).instructions).toBe("")
  })

  test("numbers written as text count, as they do for the thresholds", () => {
    const shipped = { lean: true, earlyAtPercent: 90, keepTurns: 6, maxToolResultChars: 2000, instructions: "" }
    expect(policyOf(shipped, { earlyAtPercent: " 75 " }).earlyAtPercent).toBe(75)
    expect(policyOf(shipped, { keepTurns: "3" }).keepTurns).toBe(3)
    expect(policyOf(shipped, { earlyAtPercent: "0x10" }).earlyAtPercent).toBe(90)
  })

  test("0, false and null switch the early compaction off", () => {
    const shipped = { lean: true, earlyAtPercent: 90, keepTurns: 6, maxToolResultChars: 2000, instructions: "" }
    for (const off of [0, false, null, "0"]) expect(policyOf(shipped, { earlyAtPercent: off }).earlyAtPercent).toBe(0)
  })

  test("compactAtPercent from before 8.6.0 keeps the early compaction off when it switched it off, and is dropped otherwise", () => {
    const shipped = { lean: true, earlyAtPercent: 90, keepTurns: 6, maxToolResultChars: 2000, instructions: "" }
    for (const off of [0, false, null, "0"]) expect(policyOf(shipped, { compactAtPercent: off }).earlyAtPercent).toBe(0)
    expect(policyOf(shipped, { compactAtPercent: 70 }).earlyAtPercent).toBe(90)
    expect(policyOf(shipped, { compactAtPercent: 0, earlyAtPercent: 80 }).earlyAtPercent).toBe(80)
    expect("compactAtPercent" in policyOf(shipped, { compactAtPercent: 0 })).toBe(false)
  })

  test("a section that is not an object is set aside", () => {
    const shipped = { lean: true, earlyAtPercent: 80, keepTurns: 4, maxToolResultChars: 3000, instructions: "keep the plan" }
    for (const bad of [true, 5, "on", [1]]) expect(policyOf(shipped, bad)).toEqual(shipped)
    expect(policyOf({ lean: "x", earlyAtPercent: "y" }, {})).toEqual(SHIPPED)
  })

  test("the account's own values win", () => {
    const shipped = { lean: true, earlyAtPercent: 90, keepTurns: 6, maxToolResultChars: 2000, instructions: "" }
    expect(policyOf(shipped, { lean: false, earlyAtPercent: 60, keepTurns: 2, maxToolResultChars: 800, instructions: "the plan" })).toEqual({ lean: false, earlyAtPercent: 60, keepTurns: 2, maxToolResultChars: 800, instructions: "the plan" })
  })
})

type World = {
  files: Record<string, string>
  env: Record<string, string>
  settings: Record<string, unknown>
  sources?: Record<string, Record<string, unknown>>
  model?: string
  usage: { context: { percent?: number; tokens?: number; window: number }; rateLimits?: unknown }
  compacts: unknown[]
  compactAnswer: () => Promise<unknown>
  logs: unknown[][]
  now: number
  sid: string
  root: string
  throwOn: string
}

const PAUSE_POINTS: [unknown, unknown, string, number, boolean][] = [
  [{ session5h: 80 }, { session5h: 92, weeklyAll: 89 }, "five_hour", 74, false],
  [{ session5h: 80 }, { session5h: 92, weeklyAll: 89 }, "five_hour", 73.9, true],
  [{ session5h: " 80 " }, { session5h: 92, weeklyAll: 89 }, "five_hour", 74, false],
  [{ session5h: "abc" }, { session5h: 92, weeklyAll: 89 }, "five_hour", 86, false],
  [{ session5h: "abc" }, { session5h: 92, weeklyAll: 89 }, "five_hour", 85.9, true],
  [{ session5h: "abc" }, undefined, "five_hour", 86, false],
  [{ session5h: 150 }, { session5h: 90, weeklyAll: 85 }, "five_hour", 84, false],
  [{ session5h: 150 }, { session5h: 90, weeklyAll: 85 }, "five_hour", 83.9, true],
  [{}, { session5h: 90, weeklyAll: 85 }, "five_hour", 84, false],
  ["not an object", { session5h: 90, weeklyAll: 85 }, "five_hour", 84, false],
  [undefined, { session5h: 90, weeklyAll: 85 }, "five_hour", 84, false],
  [undefined, { session5h: 150 }, "five_hour", 86, false],
  [{ session5h: 0 }, { session5h: 92, weeklyAll: 89 }, "five_hour", 99, true],
  [{ session5h: false }, { session5h: 92, weeklyAll: 89 }, "five_hour", 99, true],
  [{ session5h: null }, { session5h: 92, weeklyAll: 89 }, "five_hour", 99, true],
  [{ session5h: "0" }, { session5h: 92, weeklyAll: 89 }, "five_hour", 99, true],
  [undefined, undefined, "five_hour", 99, true],
  ["not an object", undefined, "five_hour", 99, true],
  [{ weeklyAll: 70 }, { session5h: 92, weeklyAll: 89 }, "seven_day", 64, true],
  [{ weeklyAll: 70 }, { session5h: 92, weeklyAll: 89 }, "seven_day", 63.9, true],
]

const CACHE_OFF = { HOME: "/home/u", DISABLE_PROMPT_CACHING: "1" }

function world(overrides: Partial<World> = {}): World {
  return {
    files: {
      "/plugin/config.default.json": JSON.stringify({ thresholds: { session5h: 92, weeklyAll: 89, weeklyFable: 95 }, compaction: { contextPercent: 85, lean: true, earlyAtPercent: 90, keepTurns: 1, maxToolResultChars: 100, instructions: "" } }),
    },
    env: { HOME: "/home/u" },
    settings: {},
    usage: { context: { percent: 80, tokens: 160000, window: 200000 } },
    compacts: [],
    compactAnswer: async () => ({ messages: [prompt("summary")] }),
    logs: [],
    now: 1_790_000_000_000,
    sid: "sid-1",
    root: "/plugin",
    throwOn: "",
    ...overrides,
  }
}

function engine(w: World) {
  const guard = (name: string) => {
    if (w.throwOn === name) throw new Error(`${name} broke`)
  }
  return {
    plugin: { name: "noctis", root: w.root },
    env: { get: async (name: string) => (guard("env"), w.env[name]) },
    fs: {
      read: async (path: string) => {
        guard("read")
        if (!(path in w.files)) throw new Error(`ENOENT: ${path}`)
        return w.files[path]
      },
      write: async (path: string, text: string) => {
        guard("write")
        w.files[path] = text
      },
    },
    clock: { now: async () => w.now },
    settings: {
      read: async (arg?: { source?: string }) => {
        if (arg?.source === undefined) return w.settings
        guard("sources")
        return w.sources ? (w.sources[arg.source] ?? {}) : arg.source === "user" ? w.settings : {}
      },
    },
    session: {
      id: async () => w.sid,
      model: async () => w.model,
      usage: async () => w.usage,
      compact: async (args: unknown) => {
        w.compacts.push(args)
        return w.compactAnswer()
      },
    },
    ui: { log: (...args: unknown[]) => void w.logs.push(args) },
  }
}

function hooks() {
  const registered: Record<string, (...args: any[]) => any> = {}
  register((event: string, hook: (...args: any[]) => any) => {
    registered[event] = hook
  }, {})
  return registered
}

const CONVERSATION = () => [prompt("go", "h1"), call("t1", "Bash", { command: "npm test" }, "h2"), answer("t1", "x".repeat(5000), "h3"), reply("done", "h4")]

function lines(w: World, file: string) {
  return (w.files[file] ?? "").split("\n").filter(Boolean).map((line) => JSON.parse(line))
}

describe("register: session.compact", () => {
  test("with prompt caching off, hands the pruned rows down, answers with what core answered, and logs the compaction", async () => {
    const w = world({ env: CACHE_OFF })
    const rows = CONVERSATION()
    let handed: any
    const core = { messages: [prompt("summary")], tokensBefore: 150000, tokensAfter: 9000 }
    const result = await hooks()["session.compact"](engine(w), { trigger: "auto", messages: rows }, async (e: any) => ((handed = e), core))
    expect(result).toBe(core)
    expect(handed.trigger).toBe("auto")
    expect(handed.messages[2].toolResults[0].text).toContain("[noctis: 4900 chars trimmed]")
    expect(handed.messages[3]).toBe(rows[3])
    expect(handed.instructions).toBeUndefined()
    const [entry] = lines(w, "/home/u/.claude/noctis/compact.log")
    expect(entry).toEqual({ at: 1_790_000_000, sid: "sid-1", trigger: "auto", rows: 4, changed: 1, trimmed: 5000 - handed.messages[2].toolResults[0].text.length, tokensBefore: 150000, tokensAfter: 9000 })
  })

  test("with prompt caching on, the rows go down as the engine gave them and nothing is logged, since the summary request reads them from the prompt cache", async () => {
    const w = world()
    const e = { trigger: "auto", messages: CONVERSATION() }
    let handed: any
    const core = { messages: [prompt("summary")], tokensBefore: 150000, tokensAfter: 9000 }
    const result = await hooks()["session.compact"](engine(w), e, async (arg: any) => ((handed = arg), core))
    expect(result).toBe(core)
    expect(handed).toBe(e)
    expect(w.files["/home/u/.claude/noctis/compact.log"]).toBeUndefined()
  })

  test("with prompt caching on, the configured instructions still ride along on the untouched rows, and a precompute goes down with them", async () => {
    const w = world()
    w.files["/home/u/.claude/noctis/config.json"] = JSON.stringify({ compaction: { instructions: "keep the plan" } })
    for (const trigger of ["auto", "precompute"]) {
      const e = { trigger, messages: CONVERSATION() }
      let handed: any
      const core = { messages: [prompt("s")] }
      const result = await hooks()["session.compact"](engine(w), e, async (arg: any) => ((handed = arg), core))
      expect(result).toBe(core)
      expect(handed).toEqual({ trigger, messages: e.messages, instructions: "keep the plan" })
      expect(handed.messages).toBe(e.messages)
    }
    expect(w.files["/home/u/.claude/noctis/compact.log"]).toBeUndefined()
  })

  test("DISABLE_PROMPT_CACHING turns the trimming on when it is 1, true, yes or on, as Claude Code reads it; the per-model switches do not", async () => {
    const trims = async (env: Record<string, string>) => {
      const w = world({ env: { HOME: "/home/u", ...env } })
      const e = { trigger: "auto", messages: CONVERSATION() }
      let handed: any
      await hooks()["session.compact"](engine(w), e, async (arg: any) => ((handed = arg), { messages: [prompt("s")] }))
      return handed.messages !== e.messages
    }
    for (const value of ["1", "true", "YES", " on "]) expect({ value, trims: await trims({ DISABLE_PROMPT_CACHING: value }) }).toEqual({ value, trims: true })
    for (const value of ["0", "false", "", "no"]) expect({ value, trims: await trims({ DISABLE_PROMPT_CACHING: value }) }).toEqual({ value, trims: false })
    for (const name of ["DISABLE_PROMPT_CACHING_OPUS", "DISABLE_PROMPT_CACHING_SONNET", "DISABLE_PROMPT_CACHING_HAIKU"]) expect({ name, trims: await trims({ [name]: "1" }) }).toEqual({ name, trims: false })
  })

  test("the configured instructions go to the summary unless the person typed their own", async () => {
    const w = world({ env: CACHE_OFF })
    w.files["/home/u/.claude/noctis/config.json"] = JSON.stringify({ compaction: { instructions: "keep the plan" } })
    let handed: any
    const next = async (e: any) => ((handed = e), { messages: [prompt("s")] })
    await hooks()["session.compact"](engine(w), { trigger: "auto", messages: CONVERSATION() }, next)
    expect(handed.instructions).toBe("keep the plan")
    await hooks()["session.compact"](engine(w), { trigger: "manual", instructions: "mine", messages: CONVERSATION() }, next)
    expect(handed.instructions).toBe("mine")
  })

  test("CLAUDE_CONFIG_DIR names the account, as it does for the engine", async () => {
    const w = world({ env: { ...CACHE_OFF, CLAUDE_CONFIG_DIR: "/acc/two/" } })
    w.files["/acc/two/noctis/config.json"] = JSON.stringify({ compaction: { lean: false } })
    const e = { trigger: "auto", messages: CONVERSATION() }
    let handed: any
    await hooks()["session.compact"](engine(w), e, async (arg: any) => ((handed = arg), { messages: [prompt("s")] }))
    expect(handed).toBe(e)
  })

  test("on Windows the account is under USERPROFILE", async () => {
    const w = world({ root: "C:\\Users\\u\\.claude\\plugins\\noctis", env: { HOME: "/c/Users/git", USERPROFILE: "C:\\Users\\u", DISABLE_PROMPT_CACHING: "1" } })
    w.files["C:\\Users\\u\\.claude\\plugins\\noctis/config.default.json"] = w.files["/plugin/config.default.json"]
    await hooks()["session.compact"](engine(w), { trigger: "auto", messages: CONVERSATION() }, async () => ({ messages: [prompt("s")] }))
    expect(lines(w, "C:\\Users\\u/.claude/noctis/compact.log")).toHaveLength(1)
  })

  test("lean switched off hands the event down untouched and logs nothing", async () => {
    const w = world({ env: CACHE_OFF })
    w.files["/home/u/.claude/noctis/config.json"] = JSON.stringify({ compaction: { lean: false } })
    const e = { trigger: "auto", messages: CONVERSATION() }
    let handed: any
    await hooks()["session.compact"](engine(w), e, async (arg: any) => ((handed = arg), { messages: [prompt("s")] }))
    expect(handed).toBe(e)
    expect(w.files["/home/u/.claude/noctis/compact.log"]).toBeUndefined()
  })

  test("a precomputed summary is refused while lean trims, since the pruned rows would throw it away", async () => {
    const w = world({ env: CACHE_OFF })
    let called = false
    const result = await hooks()["session.compact"](engine(w), { trigger: "precompute", messages: CONVERSATION() }, async () => ((called = true), { messages: [] }))
    expect(called).toBe(false)
    expect(typeof result.skip).toBe("string")
    expect(result.skip.length > 0).toBe(true)
  })

  test("a precompute passes through when lean is off", async () => {
    const w = world({ env: CACHE_OFF })
    w.files["/home/u/.claude/noctis/config.json"] = JSON.stringify({ compaction: { lean: false } })
    const e = { trigger: "precompute", messages: CONVERSATION() }
    let handed: any
    await hooks()["session.compact"](engine(w), e, async (arg: any) => ((handed = arg), { messages: [prompt("s")] }))
    expect(handed).toBe(e)
  })

  test("messages that are not a list are handed down as they came", async () => {
    const w = world()
    const e = { instructions: "early" }
    let handed: any
    await hooks()["session.compact"](engine(w), e, async (arg: any) => ((handed = arg), { messages: [prompt("s")] }))
    expect(handed).toBe(e)
  })

  test("a failure while reading the policy hands the event down untouched", async () => {
    const w = world({ throwOn: "env" })
    const e = { trigger: "auto", messages: CONVERSATION() }
    let handed: any
    const core = { messages: [prompt("s")] }
    const result = await hooks()["session.compact"](engine(w), e, async (arg: any) => ((handed = arg), core))
    expect(handed).toBe(e)
    expect(result).toBe(core)
  })

  test("a log that cannot be written never costs the compaction", async () => {
    const w = world({ env: CACHE_OFF, throwOn: "write" })
    const core = { messages: [prompt("s")] }
    const result = await hooks()["session.compact"](engine(w), { trigger: "auto", messages: CONVERSATION() }, async () => core)
    expect(result).toBe(core)
  })

  test("a skip from beneath is handed up and not logged", async () => {
    const w = world({ env: CACHE_OFF })
    const result = await hooks()["session.compact"](engine(w), { trigger: "auto", messages: CONVERSATION() }, async () => ({ skip: "blocked by a PreCompact hook" }))
    expect(result).toEqual({ skip: "blocked by a PreCompact hook" })
    expect(w.files["/home/u/.claude/noctis/compact.log"]).toBeUndefined()
  })

  test("the log keeps its last 200 compactions", async () => {
    const w = world({ env: CACHE_OFF })
    const old = Array.from({ length: 200 }, (_, index) => JSON.stringify({ at: index, sid: "old", trigger: "auto", rows: 1, changed: 0, trimmed: 0 }))
    w.files["/home/u/.claude/noctis/compact.log"] = old.join("\n") + "\n"
    await hooks()["session.compact"](engine(w), { trigger: "auto", messages: CONVERSATION() }, async () => ({ messages: [prompt("s")] }))
    const kept = lines(w, "/home/u/.claude/noctis/compact.log")
    expect(kept).toHaveLength(200)
    expect(kept[0].at).toBe(1)
    expect(kept[199].sid).toBe("sid-1")
  })

  test("a subagent's own compaction is logged under its agent", async () => {
    const w = world({ env: CACHE_OFF })
    await hooks()["session.compact"](engine(w), { trigger: "auto", agentId: "agent-7", messages: CONVERSATION() }, async () => ({ messages: [prompt("s")] }))
    expect(lines(w, "/home/u/.claude/noctis/compact.log")[0].agent).toBe("agent-7")
  })
})

describe("register: turn.complete", () => {
  const TURN = { answer: "done", durationMs: 5, isAborted: false, turnId: "t", reason: "answer" }

  test("asks for a compaction between turns once the context is earlyAtPercent of the way to where Claude Code compacts", async () => {
    const w = world()
    const answered = { text: "done" }
    const result = await hooks()["turn.complete"](engine(w), TURN, async () => answered)
    expect(result).toBe(answered)
    expect(w.compacts).toEqual([{}])
  })

  test("the configured instructions ride along", async () => {
    const w = world()
    w.files["/home/u/.claude/noctis/config.json"] = JSON.stringify({ compaction: { instructions: "keep the plan" } })
    await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
    expect(w.compacts).toEqual([{ instructions: "keep the plan" }])
  })

  test("short of earlyAtPercent of the way to the compaction point nothing is asked, whatever share of the window that is", async () => {
    for (const context of [{ percent: 75, tokens: 150000, window: 200000 }, { percent: 89, window: 200000 }]) {
      const w = world({ usage: { context } })
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect({ context, compacts: w.compacts }).toEqual({ context, compacts: [] })
    }
  })

  test("the way to the compaction point is reckoned as Claude Code reckons the point: the window variable, the model's own autoCompactWindow, autoCompactWindow, the percent override", async () => {
    const MILLION = 1_000_000
    const cases: [string, Record<string, string>, Record<string, unknown>, string | undefined, number, number, boolean][] = [
      ["autoCompactWindow 313000 compacts by 280k", {}, { autoCompactWindow: 313000 }, undefined, MILLION, 252000, true],
      ["autoCompactWindow 313000, just short", {}, { autoCompactWindow: 313000 }, undefined, MILLION, 251000, false],
      ["Claude Code's own point in a 1M window", {}, {}, undefined, MILLION, 870300, true],
      ["Claude Code's own point in a 1M window, just short", {}, {}, undefined, MILLION, 870000, false],
      ["autoCompactWindow 313000 in a 200k window", {}, { autoCompactWindow: 313000 }, undefined, 200000, 150300, true],
      ["autoCompactWindow out of range is set aside", {}, { autoCompactWindow: 50000 }, undefined, MILLION, 252000, false],
      ["the window variable wins", { CLAUDE_CODE_AUTO_COMPACT_WINDOW: "600000" }, { autoCompactWindow: 313000 }, undefined, MILLION, 510300, true],
      ["the window variable wins, just short", { CLAUDE_CODE_AUTO_COMPACT_WINDOW: "600000" }, { autoCompactWindow: 313000 }, undefined, MILLION, 510000, false],
      ["the model's own entry", {}, { autoCompactWindow: 313000, modelSettings: { "claude-opus-5-5": { autoCompactWindow: 173000 } } }, "claude-opus-5-5[1m]", MILLION, 126000, true],
      ["another model's entry is not this one's", {}, { autoCompactWindow: 313000, modelSettings: { "claude-opus-5-5": { autoCompactWindow: 173000 } } }, "claude-sonnet-5-5[1m]", MILLION, 126000, false],
      ["60 percent of a 1M window", { CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: "60" }, {}, undefined, MILLION, 529200, true],
      ["60 percent of a 1M window, just short", { CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: "60" }, {}, undefined, MILLION, 529000, false],
      ["60 percent of a 200k window", { CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: "60" }, {}, undefined, 200000, 97200, true],
      ["60 percent of a 200k window, just short", { CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: "60" }, {}, undefined, 200000, 97000, false],
    ]
    for (const [name, env, settings, model, window, tokens, asks] of cases) {
      const w = world({ env: { HOME: "/home/u", ...env }, settings, model, usage: { context: { tokens, window } } })
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect({ name, asked: w.compacts.length }).toEqual({ name, asked: asks ? 1 : 0 })
    }
  })

  test("the point follows Claude Code for a provider's spelling of the model and a reply limit below 20000", async () => {
    const MILLION = 1_000_000
    const OWN = { autoCompactWindow: 313000, modelSettings: { "claude-opus-5-5": { autoCompactWindow: 173000 } } }
    const cases: [string, Record<string, string>, Record<string, unknown>, string | undefined, number, number, boolean][] = [
      ["Bedrock's id for the model", {}, OWN, "us.anthropic.claude-opus-5-5[1m]", MILLION, 126000, true],
      ["Bedrock's id for the model, just short", {}, OWN, "us.anthropic.claude-opus-5-5[1m]", MILLION, 125900, false],
      ["a dated Bedrock id", {}, OWN, "eu.anthropic.claude-opus-5-5-20260101-v1:0", MILLION, 126000, true],
      ["Mantle's id", {}, OWN, "anthropic.claude-opus-5-5", MILLION, 126000, true],
      ["a Vertex id", {}, OWN, "claude-opus-5-5@20260101", MILLION, 126000, true],
      ["an inference profile's ARN", {}, OWN, "arn:aws:bedrock:us-east-1:123456789012:inference-profile/global.anthropic.claude-opus-5-5", MILLION, 126000, true],
      ["an entry under Bedrock's id", {}, { autoCompactWindow: 313000, modelSettings: { "us.anthropic.claude-opus-5-5": { autoCompactWindow: 173000 } } }, "claude-opus-5-5[1m]", MILLION, 126000, true],
      ["another model's Bedrock id keeps autoCompactWindow", {}, OWN, "us.anthropic.claude-sonnet-5-5", MILLION, 126000, false],
      ["CLAUDE_CODE_MAX_OUTPUT_TOKENS 4096 keeps 4096 for the summary", { CLAUDE_CODE_MAX_OUTPUT_TOKENS: "4096" }, { autoCompactWindow: 313000 }, undefined, MILLION, 266400, true],
      ["CLAUDE_CODE_MAX_OUTPUT_TOKENS 4096, just short", { CLAUDE_CODE_MAX_OUTPUT_TOKENS: "4096" }, { autoCompactWindow: 313000 }, undefined, MILLION, 266000, false],
      ["CLAUDE_CODE_MAX_OUTPUT_TOKENS 64000 keeps 20000", { CLAUDE_CODE_MAX_OUTPUT_TOKENS: "64000" }, { autoCompactWindow: 313000 }, undefined, MILLION, 252000, true],
    ]
    for (const [name, env, settings, model, window, tokens, asks] of cases) {
      const w = world({ env: { HOME: "/home/u", ...env }, settings, model, usage: { context: { tokens, window } } })
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect({ name, asked: w.compacts.length }).toEqual({ name, asked: asks ? 1 : 0 })
    }
  })

  test("the settings files are laid over each other as Claude Code lays them: a later file's autoCompactWindow sets aside the model windows of the files before it", async () => {
    const MILLION = 1_000_000
    const USER = { autoCompactWindow: 313000, modelSettings: { "claude-opus-5-5": { autoCompactWindow: 173000 } } }
    const LATER_OPUS = { modelSettings: { "claude-opus-5-5": { autoCompactWindow: 313000 } } }
    const cases: [string, Record<string, Record<string, unknown>>, number, boolean][] = [
      ["the project's window sets aside the person's model window", { user: USER, project: { autoCompactWindow: 533000 } }, 450000, true],
      ["the project's window, just short", { user: USER, project: { autoCompactWindow: 533000 } }, 449000, false],
      ["a later file's model window over an earlier one's", { user: USER, local: LATER_OPUS }, 252000, true],
      ["a later file's model window, just short", { user: USER, local: LATER_OPUS }, 251000, false],
      ["a model window in a later file stands beside an earlier file's window", { user: { autoCompactWindow: 533000 }, project: { modelSettings: { opus: { autoCompactWindow: 173000 } } } }, 126000, true],
      ["a model window in a later file, just short", { user: { autoCompactWindow: 533000 }, project: { modelSettings: { opus: { autoCompactWindow: 173000 } } } }, 125000, false],
      ["the managed settings come last", { user: USER, project: { autoCompactWindow: 533000 }, policy: { autoCompactWindow: 233000 } }, 180000, true],
      ["the managed settings, just short", { user: USER, project: { autoCompactWindow: 533000 }, policy: { autoCompactWindow: 233000 } }, 179000, false],
      ["a window given on the command line", { user: USER, flag: { modelSettings: { opus: { autoCompactWindow: 233000 } } } }, 180000, true],
    ]
    for (const [name, sources, tokens, asks] of cases) {
      const w = world({ sources, model: "claude-opus-5-5[1m]", usage: { context: { tokens, window: MILLION } } })
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect({ name, asked: w.compacts.length }).toEqual({ name, asked: asks ? 1 : 0 })
    }
  })

  test("a Claude Code that does not give the settings files one by one leaves the settings laid together", async () => {
    const sources = { user: { autoCompactWindow: 313000, modelSettings: { "claude-opus-5-5": { autoCompactWindow: 173000 } } }, project: { autoCompactWindow: 533000 } }
    const settings = { autoCompactWindow: 533000, modelSettings: { "claude-opus-5-5": { autoCompactWindow: 173000 } } }
    for (const [throwOn, asks] of [["", false], ["sources", true]] as const) {
      const w = world({ sources, settings, throwOn, model: "claude-opus-5-5[1m]", usage: { context: { tokens: 126000, window: 1_000_000 } } })
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect({ throwOn, asked: w.compacts.length }).toEqual({ throwOn, asked: asks ? 1 : 0 })
    }
  })

  test("no fill figure yet means nothing is asked", async () => {
    const w = world({ usage: { context: { window: 200000 } } })
    await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
    expect(w.compacts).toEqual([])
  })

  test("a subagent's turn, an interrupted turn and a failed turn ask for nothing", async () => {
    const w = world()
    const on = hooks()
    await on["turn.complete"](engine(w), { ...TURN, agentId: "a1" }, async () => ({ text: "" }))
    await on["turn.complete"](engine(w), { ...TURN, reason: "aborted", isAborted: true }, async () => ({ text: "" }))
    await on["turn.complete"](engine(w), { ...TURN, reason: "error" }, async () => ({ text: "" }))
    expect(w.compacts).toEqual([])
  })

  test("lean off or earlyAtPercent 0 asks for nothing, as does compactAtPercent 0 from before 8.6.0", async () => {
    for (const compaction of [{ lean: false }, { earlyAtPercent: 0 }, { compactAtPercent: 0 }]) {
      const w = world()
      w.files["/home/u/.claude/noctis/config.json"] = JSON.stringify({ compaction })
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect(w.compacts).toEqual([])
    }
  })

  test("Claude Code's own automatic compaction switched off keeps noctis from compacting early", async () => {
    for (const w of [world({ settings: { autoCompactEnabled: false } }), world({ env: { HOME: "/home/u", DISABLE_AUTO_COMPACT: "1" } }), world({ env: { HOME: "/home/u", DISABLE_COMPACT: "1" } })]) {
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect(w.compacts).toEqual([])
    }
  })

  test("autoCompactEnabled false in Claude Code's global config keeps noctis from compacting early, as it keeps Claude Code's own", async () => {
    const off = JSON.stringify({ autoCompactEnabled: false })
    const cases = [
      world({ files: { ...world().files, "/home/u/.claude.json": off } }),
      world({ env: { HOME: "/home/u", CLAUDE_CONFIG_DIR: "/acc/two/" }, files: { ...world().files, "/acc/two/.claude.json": off } }),
      world({ files: { ...world().files, "/home/u/.claude/.config.json": off, "/home/u/.claude.json": JSON.stringify({ autoCompactEnabled: true }) } }),
      world({ env: { HOME: "/home/u", CLAUDE_CODE_CUSTOM_OAUTH_URL: "https://login.example" }, files: { ...world().files, "/home/u/.claude-custom-oauth.json": off } }),
      world({ root: "C:\\Users\\u\\.claude\\plugins\\noctis", env: { HOME: "/c/Users/git", USERPROFILE: "C:\\Users\\u" }, files: { "C:\\Users\\u\\.claude\\plugins\\noctis/config.default.json": world().files["/plugin/config.default.json"], "C:\\Users\\u/.claude.json": off } }),
    ]
    for (const w of cases) {
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect(w.compacts).toEqual([])
    }
  })

  test("the global config counts only where Claude Code reads it: a settings value wins, a legacy .config.json replaces ~/.claude.json, CLAUDE_CONFIG_DIR moves it", async () => {
    const off = JSON.stringify({ autoCompactEnabled: false })
    const cases = [
      world({ settings: { autoCompactEnabled: true }, files: { ...world().files, "/home/u/.claude.json": off } }),
      world({ files: { ...world().files, "/home/u/.claude/.config.json": JSON.stringify({ theme: "dark" }), "/home/u/.claude.json": off } }),
      world({ env: { HOME: "/home/u", CLAUDE_CONFIG_DIR: "/acc/two" }, files: { ...world().files, "/home/u/.claude.json": off } }),
      world({ files: { ...world().files, "/home/u/.claude.json": JSON.stringify({ autoCompactEnabled: "false" }) } }),
      world({ files: { ...world().files, "/home/u/.claude.json": "{broken" } }),
    ]
    for (const w of cases) {
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect(w.compacts).toEqual([{}])
    }
  })

  test("the early compaction waits while the 5-hour window is within 6 points of its pause point, and a later turn asks once it is clear; the weekly window near its pause point does not hold it back", async () => {
    const soon = new Date(1_790_000_000_000 + 3_600_000).toISOString()
    const w = world()
    const on = hooks()
    const turn = async (rateLimits: unknown[]) => {
      w.usage = { context: { percent: 95, window: 200000 }, rateLimits }
      await on["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
    }
    await turn([{ kind: "five_hour", percentUsed: 86, resetsAt: soon }])
    await turn([{ kind: "five_hour", percentUsed: 97, resetsAt: soon }, { kind: "seven_day", percentUsed: 10, resetsAt: soon }])
    expect(w.compacts).toEqual([])
    await turn([{ kind: "five_hour", percentUsed: 85.9, resetsAt: soon }, { kind: "seven_day", percentUsed: 82.9, resetsAt: soon }])
    expect(w.compacts).toEqual([{}])
    for (const used of [83, 88]) {
      const weekly = world({ usage: { context: { percent: 95, window: 200000 }, rateLimits: [{ kind: "seven_day", percentUsed: used, resetsAt: soon }, { kind: "five_hour", percentUsed: 10, resetsAt: soon }] } })
      await hooks()["turn.complete"](engine(weekly), TURN, async () => ({ text: "done" }))
      expect({ used, compacts: weekly.compacts }).toEqual({ used, compacts: [{}] })
    }
  })

  test("the pause points are the thresholds the guard reads: the account's own, the shipped one for a wrong value, the built-in one when neither is right, none for a window switched off or without a threshold", async () => {
    const soon = new Date(1_790_000_000_000 + 3_600_000).toISOString()
    for (const [own, shipped, kind, used, asks] of PAUSE_POINTS) {
      const w = world({ usage: { context: { percent: 95, window: 200000 }, rateLimits: [{ kind, percentUsed: used, resetsAt: soon }] } })
      w.files["/plugin/config.default.json"] = JSON.stringify({ thresholds: shipped, compaction: { lean: true, earlyAtPercent: 90 } })
      if (own !== undefined) w.files["/home/u/.claude/noctis/config.json"] = JSON.stringify({ thresholds: own })
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect({ own, shipped, kind, used, asked: w.compacts.length }).toEqual({ own, shipped, kind, used, asked: asks ? 1 : 0 })
    }
  })

  test("a window whose reset has passed or is not given, a spend limit, a window without a reading and a kind it does not know leave the early compaction alone", async () => {
    const soon = new Date(1_790_000_000_000 + 3_600_000).toISOString()
    const past = new Date(1_790_000_000_000 - 1000).toISOString()
    const readings = [
      [{ kind: "five_hour", percentUsed: 99, resetsAt: past }],
      [{ kind: "five_hour", percentUsed: 99 }],
      [{ kind: "spend_limit", percentUsed: 120, resetsAt: soon }],
      [{ kind: "five_hour", resetsAt: soon }],
      [{ kind: "constructor", percentUsed: 99, resetsAt: soon }],
      [null, "five_hour"],
      [],
      "not a list",
    ]
    for (const rateLimits of readings) {
      const w = world({ usage: { context: { percent: 95, window: 200000 }, rateLimits } })
      await hooks()["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
      expect({ rateLimits, compacts: w.compacts }).toEqual({ rateLimits, compacts: [{}] })
    }
  })

  test("one compaction per crossing: the next is asked only after a turn ended below the mark", async () => {
    const w = world()
    const on = hooks()
    const turn = async (percent: number) => {
      w.usage = { context: { percent, window: 200000 } }
      await on["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
    }
    await turn(95)
    await turn(98)
    expect(w.compacts).toHaveLength(1)
    await turn(40)
    await turn(92)
    expect(w.compacts).toHaveLength(2)
  })

  test("a refused compaction (a headless session) is caught and not asked again at the same fill", async () => {
    const w = world({ compactAnswer: async () => { throw new Error("not available in a headless (-p / SDK) session yet") } })
    const on = hooks()
    const answered = { text: "done" }
    expect(await on["turn.complete"](engine(w), TURN, async () => answered)).toBe(answered)
    expect(await on["turn.complete"](engine(w), TURN, async () => answered)).toBe(answered)
    expect(w.compacts).toHaveLength(1)
    expect(w.logs.length > 0).toBe(true)
    expect(w.logs.every((args) => (args[1] as any)?.to === "debug")).toBe(true)
  })

  test("the early compaction is logged with the fill that asked for it, in percent of the way to the compaction point", async () => {
    const w = world({ env: CACHE_OFF })
    const on = hooks()
    w.compactAnswer = async () => on["session.compact"](engine(w), { trigger: "plugin", messages: CONVERSATION() }, async () => ({ messages: [prompt("s")], tokensBefore: 150000, tokensAfter: 8000 }))
    await on["turn.complete"](engine(w), TURN, async () => ({ text: "done" }))
    const [entry] = lines(w, "/home/u/.claude/noctis/compact.log")
    expect(entry.trigger).toBe("plugin")
    expect(entry.ctx).toBe(96)
  })

  test("a failure inside never touches the turn's answer", async () => {
    const w = world({ throwOn: "read" })
    const answered = { text: "done" }
    expect(await hooks()["turn.complete"](engine(w), TURN, async () => answered)).toBe(answered)
  })
})

describe("register: the session record", () => {
  test("session.start records the session the module runs in", async () => {
    const w = world()
    const started = { cwd: "/p" }
    expect(await hooks()["session.start"](engine(w), {}, async () => started)).toBe(started)
    expect(JSON.parse(w.files["/home/u/.claude/noctis/lean.json"]).sessions["sid-1"]).toBe(1_790_000_000)
  })

  test("a turn in a session it has not recorded yet records it", async () => {
    const w = world({ usage: { context: { percent: 10, window: 200000 } } })
    await hooks()["turn.complete"](engine(w), { answer: "", durationMs: 1, isAborted: false, turnId: "t", reason: "answer" }, async () => ({ text: "" }))
    expect(Object.keys(JSON.parse(w.files["/home/u/.claude/noctis/lean.json"]).sessions)).toEqual(["sid-1"])
  })

  test("the record keeps the 50 latest sessions and survives a broken file", async () => {
    const w = world()
    w.files["/home/u/.claude/noctis/lean.json"] = "{broken"
    const on = hooks()
    for (let index = 0; index < 55; index += 1) {
      w.sid = `s${index}`
      w.now += 1000
      await on["session.start"](engine(w), {}, async () => ({ cwd: "/p" }))
    }
    const sessions = JSON.parse(w.files["/home/u/.claude/noctis/lean.json"]).sessions
    expect(Object.keys(sessions)).toHaveLength(50)
    expect(sessions.s4).toBeUndefined()
    expect(typeof sessions.s54).toBe("number")
  })

  test("nothing is recorded while lean is off", async () => {
    const w = world()
    w.files["/home/u/.claude/noctis/config.json"] = JSON.stringify({ compaction: { lean: false } })
    await hooks()["session.start"](engine(w), {}, async () => ({ cwd: "/p" }))
    expect(w.files["/home/u/.claude/noctis/lean.json"]).toBeUndefined()
  })
})

function kit(on: any, files: Record<string, string> = {}, env: Record<string, string> = {}, settings: Record<string, unknown> | ((source?: string) => unknown) = {}) {
  mock.env(on, { HOME: "/home/k", ...env })
  mock.clock(on, { now: 1_790_000_000_000 })
  const written: Record<string, string> = {}
  on("session.id", () => ({ value: "kit-session" }))
  on("session.model", () => ({ value: "claude-opus-5-5[1m]" }))
  on("settings.read", ($: any, e: any) => ({ value: typeof settings === "function" ? settings(e?.source) : settings }))
  on("ui.log", () => ({ value: undefined }))
  on("fs.read", ($: any, e: any) => {
    const path = String(e.path)
    if (path in written) return { value: written[path] }
    if (path in files) return { value: files[path] }
    if (path.endsWith("/config.default.json")) return { value: JSON.stringify({ thresholds: { session5h: 92, weeklyAll: 89, weeklyFable: 95 }, compaction: { lean: true, earlyAtPercent: 90, keepTurns: 1, maxToolResultChars: 100, instructions: "" } }) }
    return { deny: `ENOENT: ${path}` }
  })
  on("fs.write", ($: any, e: any) => {
    written[String(e.path)] = String(e.text)
    return { value: undefined }
  })
  return written
}

const SUMMARY = { role: "user", text: "summary", toolUses: [] }

describe("through the engine", () => {
  test("with prompt caching off, a compaction reaches the module, and core gets the pruned rows", async ($, on) => {
    const written = kit(on, {}, { DISABLE_PROMPT_CACHING: "1" })
    const seen: any[] = []
    on("session.compact", ($: any, e: any) => {
      seen.push(e)
      return { messages: [SUMMARY], tokensBefore: 150000, tokensAfter: 9000 }
    })
    const rows = CONVERSATION()
    const result: any = await $.session.compact({ trigger: "auto", messages: rows } as any)
    expect(seen).toHaveLength(1)
    expect(seen[0].messages[2].toolResults[0].text).toContain("[noctis: 4900 chars trimmed]")
    expect(seen[0].messages[2].handle).toBeUndefined()
    expect(seen[0].messages[3].handle).toBe("h4")
    expect(result.messages[0].text).toBe("summary")
    const entry = JSON.parse(written["/home/k/.claude/noctis/compact.log"].trim())
    expect(entry).toMatchObject({ sid: "kit-session", trigger: "auto", rows: 4, changed: 1, tokensBefore: 150000, tokensAfter: 9000 })
  })

  test("a precompute never reaches core while lean trims", async ($, on) => {
    kit(on, {}, { DISABLE_PROMPT_CACHING: "1" })
    const seen: any[] = []
    on("session.compact", ($: any, e: any) => {
      seen.push(e)
      return { messages: [SUMMARY] }
    })
    const result: any = await $.session.compact({ trigger: "precompute", messages: CONVERSATION() } as any)
    expect(seen).toHaveLength(0)
    expect(typeof result.skip).toBe("string")
  })

  test("with prompt caching on, core gets the engine's rows untouched and a precompute reaches it", async ($, on) => {
    const written = kit(on)
    const seen: any[] = []
    on("session.compact", ($: any, e: any) => {
      seen.push(e)
      return { messages: [SUMMARY] }
    })
    await $.session.compact({ trigger: "auto", messages: CONVERSATION() } as any)
    await $.session.compact({ trigger: "precompute", messages: CONVERSATION() } as any)
    expect(seen.map((e) => e.trigger)).toEqual(["auto", "precompute"])
    expect(seen[0].messages[2].toolResults[0].text).toBe("x".repeat(5000))
    expect(seen[0].messages[2].handle).toBe("h3")
    expect(written["/home/k/.claude/noctis/compact.log"]).toBeUndefined()
  })

  test("a turn that ends earlyAtPercent of the way to the compaction point asks the engine for a compaction", async ($, on) => {
    const written = kit(on)
    on("session.usage", () => ({ value: { startedAt: 0, context: { window: 200000, tokens: 160000, percent: 80 }, rateLimits: [] } }))
    on("turn.complete", ($: any, e: any) => ({ text: e.answer }))
    const asked: any[] = []
    on("session.compact", ($: any, e: any) => {
      asked.push(e)
      return { messages: [SUMMARY] }
    })
    const answered: any = await $.turn.complete({ answer: "done", durationMs: 1, isAborted: false, turnId: "t1", reason: "answer" } as any)
    expect(answered.text).toBe("done")
    expect(asked).toEqual([{}])
    expect(JSON.parse(written["/home/k/.claude/noctis/lean.json"]).sessions["kit-session"]).toBe(1_790_000_000)
  })

  test("autoCompactEnabled false in ~/.claude.json keeps a full turn from asking, as it keeps Claude Code's own compaction", async ($, on) => {
    kit(on, { "/home/k/.claude.json": JSON.stringify({ autoCompactEnabled: false }) })
    on("session.usage", () => ({ value: { startedAt: 0, context: { window: 200000, tokens: 160000, percent: 80 }, rateLimits: [] } }))
    on("turn.complete", ($: any, e: any) => ({ text: e.answer }))
    const asked: any[] = []
    on("session.compact", ($: any, e: any) => {
      asked.push(e)
      return { messages: [SUMMARY] }
    })
    await $.turn.complete({ answer: "done", durationMs: 1, isAborted: false, turnId: "t3", reason: "answer" } as any)
    expect(asked).toEqual([])
  })

  test("a full turn next to the 5-hour limit leaves the compaction for later", async ($, on) => {
    kit(on)
    const soon = new Date(1_790_000_000_000 + 3_600_000).toISOString()
    on("session.usage", () => ({ value: { startedAt: 0, context: { window: 200000, tokens: 160000, percent: 80 }, rateLimits: [{ kind: "five_hour", percentUsed: 91, resetsAt: soon }] } }))
    on("turn.complete", ($: any, e: any) => ({ text: e.answer }))
    const asked: any[] = []
    on("session.compact", ($: any, e: any) => {
      asked.push(e)
      return { messages: [SUMMARY] }
    })
    await $.turn.complete({ answer: "done", durationMs: 1, isAborted: false, turnId: "t4", reason: "answer" } as any)
    expect(asked).toEqual([])
  })

  test("a full turn next to the weekly limit asks for the compaction", async ($, on) => {
    kit(on)
    const soon = new Date(1_790_000_000_000 + 3_600_000).toISOString()
    on("session.usage", () => ({ value: { startedAt: 0, context: { window: 200000, tokens: 160000, percent: 80 }, rateLimits: [{ kind: "seven_day", percentUsed: 88, resetsAt: soon }] } }))
    on("turn.complete", ($: any, e: any) => ({ text: e.answer }))
    const asked: any[] = []
    on("session.compact", ($: any, e: any) => {
      asked.push(e)
      return { messages: [SUMMARY] }
    })
    await $.turn.complete({ answer: "done", durationMs: 1, isAborted: false, turnId: "t4", reason: "answer" } as any)
    expect(asked).toEqual([{}])
  })

  test("the model's own autoCompactWindow in the settings sets where the way to the compaction point ends", async ($, on) => {
    kit(on, {}, {}, { autoCompactWindow: 313000, modelSettings: { "claude-opus-5-5": { autoCompactWindow: 173000 } } })
    on("session.usage", () => ({ value: { startedAt: 0, context: { window: 1000000, tokens: 130000, percent: 13 }, rateLimits: [] } }))
    on("turn.complete", ($: any, e: any) => ({ text: e.answer }))
    const asked: any[] = []
    on("session.compact", ($: any, e: any) => {
      asked.push(e)
      return { messages: [SUMMARY] }
    })
    await $.turn.complete({ answer: "done", durationMs: 1, isAborted: false, turnId: "t5", reason: "answer" } as any)
    expect(asked).toEqual([{}])
  })

  test("each settings file Claude Code reads is laid over the ones before it, as Claude Code lays them", async ($, on) => {
    const sources: Record<string, Record<string, unknown>> = {
      user: { autoCompactWindow: 313000, modelSettings: { "claude-opus-5-5": { autoCompactWindow: 173000 } } },
      project: { autoCompactWindow: 533000 },
    }
    const merged = { autoCompactWindow: 533000, modelSettings: { "claude-opus-5-5": { autoCompactWindow: 173000 } } }
    kit(on, {}, {}, (source) => (source === undefined ? merged : (sources[source] ?? {})))
    let tokens = 300000
    on("session.usage", () => ({ value: { startedAt: 0, context: { window: 1000000, tokens, percent: tokens / 10000 }, rateLimits: [] } }))
    on("turn.complete", ($: any, e: any) => ({ text: e.answer }))
    const asked: any[] = []
    on("session.compact", ($: any, e: any) => {
      asked.push(e)
      return { messages: [SUMMARY] }
    })
    await $.turn.complete({ answer: "done", durationMs: 1, isAborted: false, turnId: "t6", reason: "answer" } as any)
    expect(asked).toEqual([])
    tokens = 450000
    await $.turn.complete({ answer: "done", durationMs: 1, isAborted: false, turnId: "t7", reason: "answer" } as any)
    expect(asked).toEqual([{}])
  })

  test("a turn short of earlyAtPercent asks for nothing", async ($, on) => {
    kit(on)
    on("session.usage", () => ({ value: { startedAt: 0, context: { window: 200000, tokens: 20000, percent: 10 }, rateLimits: [] } }))
    on("turn.complete", ($: any, e: any) => ({ text: e.answer }))
    const asked: any[] = []
    on("session.compact", ($: any, e: any) => {
      asked.push(e)
      return { messages: [SUMMARY] }
    })
    await $.turn.complete({ answer: "done", durationMs: 1, isAborted: false, turnId: "t2", reason: "answer" } as any)
    expect(asked).toEqual([])
  })
})
