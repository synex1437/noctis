<p align="center">
  <img src="docs/banner.svg" alt="Noctis — AI coding agents that work through the night" width="100%">
</p>

<p align="center">
  <a href="https://github.com/synex1437/noctis/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/synex1437/noctis/actions/workflows/ci.yml/badge.svg"></a>
  <img alt="Windows, macOS, Linux" src="https://img.shields.io/badge/Windows%20%C2%B7%20macOS%20%C2%B7%20Linux-one%20binary-2a78d6">
  <img alt="Claude Code, Codex CLI, Antigravity CLI, Droid, Copilot CLI" src="https://img.shields.io/badge/Claude%20Code%20%C2%B7%20Codex%20%C2%B7%20Antigravity%20%C2%B7%20Droid%20%C2%B7%20Copilot-5%20tools-35b26e">
  <a href="LICENSE"><img alt="MIT license" src="https://img.shields.io/badge/license-MIT-lightgrey"></a>
  <a href="README.tr.md"><img alt="Türkçe README" src="https://img.shields.io/badge/README-T%C3%BCrk%C3%A7e-e30a17"></a>
</p>

You queued forty tasks, went to bed, and woke up to a session that died on a usage limit at 01:40 — or stopped at task three with *"moving on to the next thing"*. Every Claude Code power user knows that morning.

**Noctis** is a plugin for people on a Claude **Pro or Max** subscription (and, from plugin v5.2, inside OpenAI Codex CLI, Antigravity CLI, Factory Droid and GitHub Copilot CLI) who run long jobs and hit the 5-hour or weekly usage limit halfway through. It pauses the agent just *before* the limit, waits for the reset, and continues the same session. It works through a `TASKS.md` checklist item by item with no "shall I continue?" prompts, and on Claude Code it sends research to a cheaper model so the expensive one's quota lasts. It never calls a model and never spends a token on its own decisions, and it writes every decision to a log you can read. It runs silently in the background; there is nothing to type day to day.

**Install** — type these four lines inside Claude Code (about a minute; the third one asks a single question: which model does which kind of work):

```
/plugin marketplace add synex1437/noctis
/plugin install noctis
/noctis:setup
/reload-plugins
```

**Requirements:** Claude Code 2.1.251 or newer, signed in with a Pro or Max account, on Windows, macOS or Linux. Nothing else to install — no Node, no Git Bash, no compiler. API-key sessions have no usage windows, so the limit guard stays idle (one notice, then silence); queue mode and the research router (the cheaper-model hand-off) still work. Not sure your plan includes Fable? Pick `balanced` or `economy` when setup asks — `noctis` assumes Max. Setup edits `~/.claude/settings.json`: status line, default model, and **`permissions.defaultMode` → `auto`, so Claude edits files and runs commands without asking** (that is what lets it work overnight; `--permissions keep` opts out). Every key and its undo is [listed below](#what-it-changes-on-your-machine--and-how-to-undo-it). Using another AI coding tool? See [Other AI coding tools](#other-ai-coding-tools).

<p align="center"><img src="docs/demo.svg" alt="A night with the plugin: pauses at the 5-hour limit, resumes after the reset, routes research to a cheaper model, switches model when the scoped quota is out, relaunches an interrupted workflow, finishes the queue by morning" width="100%"></p>

## What it changes on your machine — and how to undo it

<details>
<summary>Every settings key setup writes, what it talks to on the network, and the undo for each.</summary>

Setup writes a backup `settings.json.bak-<time>` first, then touches exactly this:

| Where | What | Undo |
|---|---|---|
| `~/.claude/settings.json` → `statusLine` | Points the status line at the plugin's binary (that is how Claude Code reports your usage). A status line you already had keeps running behind it. | `noctis install --uninstall` puts it back |
| `settings.json` → `permissions.defaultMode` | Set to `auto` (older Claude Code: `acceptEdits`). **In plain words: Claude will edit files and run commands without asking you first** — that is what lets it work while you sleep. It never uses `bypassPermissions`, and an unattended relaunch refuses that mode even if a config asks for it. | `--permissions keep` at setup; uninstall removes exactly what setup wrote |
| `settings.json` → `model` and `env.CLAUDE_CODE_EFFORT_LEVEL` | Default model = the *code* model of the profile you pick (**Fable 5.1, effort `max`** with the default `noctis` profile). Skipped if your default is already Fable or Opus. | `--no-model` at setup; uninstall restores the previous value |
| `~/.claude/noctis/` | Its own `config.json`, usage snapshots, checkpoints, logs | Delete the folder |
| A scheduled task, only while a resume is pending | Windows Task Scheduler `Noctis-…` (can wake the PC), launchd `com.synex.noctis.…`, systemd `noctis-…` | `noctis cancel` removes all pending ones; `"alarm": {"wakePc": false}` stops the wake |
| Marketplace auto-update | Setup runs `claude plugin marketplace update noctis --auto-update` so new versions arrive on their own (third-party marketplaces have this off by default). | `--updates keep` at setup, or the toggle in `/plugin` → Marketplaces |

**Network:** the only servers it contacts are `api.anthropic.com` (the usage endpoint the Claude app itself uses, with the login token Claude Code already keeps — on macOS in the Keychain, expect one "allow access" prompt — every 10 minutes, down to every 15 seconds in the last two points before a pause) and, once a day, the raw `plugin.json` on GitHub to learn whether a newer version exists (`update.check: false` turns that off). Plus the webhook URL you configure, if any. No telemetry; nothing about you is sent anywhere.

**Queue mode runs a checklist for you — once you say it may.** When the project folder contains `TASKS.md` (or `tasks.md`, `.claude/TASKS.md`, `docs/TASKS.md`) with open `- [ ]` items, the first session in that folder tells you the file is there and that it is **not** driving anything yet:

```text
☰ TASKS.md in this folder holds 12 open item(s). It is not driving this session: a checklist can
  arrive with a repository, and driving means working through it without stopping to ask.
  To let it drive: noctis queue trust
```

`noctis queue trust` turns it on for that file, `noctis queue untrust` turns it back off, `noctis queue status` says where it stands. A checklist Noctis wrote itself from your own prompt needs no permission — you already asked for it. The gate exists because a `TASKS.md` can arrive with a `git clone`, and "work through this list without asking" is not something a downloaded file should be able to say. Want the old always-on behaviour? `"queue": {"requireTrust": false}` in `~/.claude/noctis/config.json`. Never want it? `"queue": {"files": []}`.

**Turn it off.** For a while: `/noctis:pause 120` (minutes; Claude keeps working, the guard sleeps) — `:resume` switches it back on. Watch only, enforce nothing: `"mode": "observe"` in `config.json`. Completely:

```
noctis cancel                              # drop pending resumes and their scheduled tasks
noctis install --uninstall                 # restores settings.json (status line, effort, permission mode, model)
                                           # run this FIRST: removing the plugin first deletes the
                                           # binary, and then nothing is left to undo the edits
/plugin uninstall noctis   # inside Claude Code
```

If the plugin is already gone and the settings are still in place, undo it by hand: open `~/.claude/settings.json` and remove `permissions.defaultMode`, `model`, `env.CLAUDE_CODE_EFFORT_LEVEL` and the `statusLine` block (or restore the `settings.json.bak-<time>` copy setup writes next to it). `permissions.defaultMode` is the one that matters: left behind, every future session keeps editing files and running commands without asking.

```text
```
Clone install: `scripts/install.sh --uninstall` (macOS/Linux) or `scripts\install.ps1 -Uninstall` (Windows) instead of the middle line.

</details>

## The first five minutes

1. After `/reload-plugins`, look at the bottom of the Claude Code window: a line like `∞ 5h %41→14:35 · Wk %23▲→Mon 21.09 · Fable 5.1/max · ctx %37` shows your 5-hour and weekly usage, when each resets, and the current model. If it says *waiting for limit data*, send one message — it fills in after the first reply.
2. Type `/noctis:status`: your usage windows, the pause points (92 % of the 5-hour window, 89 % of the weekly one by default) and the last decisions the plugin made.
3. Create the three-line `TASKS.md` below and tell Claude "work through TASKS.md". Watch it tick items off and continue on its own.
4. At a limit there is nothing for you to do: it pauses *before* the wall, waits (a short reset inside the same turn, context intact; a long one as a scheduled `claude --resume` of the same session — on Windows in a new terminal window, on macOS/Linux in the background with its output in `resume-output.log`, ready to pick up with `claude --resume <id>`), and continues. On Windows and Linux the scheduled relaunch asks to wake the machine (Linux needs `CAP_WAKE_ALARM`; if the user manager refuses, it schedules without waking). On macOS launchd cannot wake the Mac, so keep it awake for the night.
5. Not ready to trust it? Put `"mode": "observe"` in `~/.claude/noctis/config.json` for the first days: it logs every decision (`/noctis:status` shows them) and enforces nothing.

## Queue file

<details>
<summary>The `TASKS.md` format, plus priorities, tags, dependencies and GitHub issues.</summary>

Put a `TASKS.md` in the folder where you start `claude`, one task per line:

```markdown
- [ ] add input validation to the signup form
- [ ] write tests for the payments module
- [ ] update README for the new CLI flags
```

That is the whole format. Claude takes the first open item, ticks it `- [x]` in the file when done, and continues to the next without asking; when every box is ticked it says `✔ Queue finished` and stops. No file is required either: paste a long prompt with several things to do and Noctis writes the checklist itself (kept in its own folder, never in your project) and runs it the same way — `☰ 5-step job detected` is how you know. Short prompts and single tasks are left alone. Sloppy lists are accepted (`-[ ]`, `* [ ]`, `1. [ ]`, `[]`, `TODO:`; an item wrapped over two or three lines is one item; `~~struck~~`, `(done)` and `✓` count as finished).

<details><summary>Priorities, tags, dependencies, GitHub issues</summary>

```markdown
- [ ] (P0) fix the login redirect #auth
- [ ] (P1) migrate users (after #auth, #db)
- [ ] deploy (after 2)
- [ ] (P7) write the API docs
```

`(P0)`–`(P9)` orders the work (default P5, lower first); `#name` tags an item; `(after #tag)` or `(after 3)` makes an item wait until the referenced items are checked (`(after 2)` means the 2nd checklist item in the file). The Stop hook hands Claude the best eligible item next, tells it how many items are still waiting, and stops cleanly (with a notice) when every open item is blocked or when the queue is finished. References that match nothing are ignored, so a typo never deadlocks a night. `noctis queue import` appends open GitHub issues as `- [ ] (P1) #123 Title` items through the `gh` CLI (priority from `P0`–`P9` or `priority: high` labels, idempotent), and `queue.github.closeOnDone` closes an issue when its item is checked off.
</details>

</details>

## What it does while you're away

<details>
<summary>Situation by situation: limits, early resets, queue stops, quota switches and the failures it handles alone.</summary>

| Situation | What happens |
|---|---|
| 5-hour or weekly usage approaches the limit (pause at 92 % / 89 % by default) | The turn is paused **before** the wall: a checkpoint is written (last request, touched files, `git status`, todos, next queue items), the session waits in place for short resets, or is saved and **auto-resumed** at the reset time by a scheduled task (Windows Task Scheduler, which can wake the PC; launchd on macOS and systemd on Linux, which run when the machine is awake) plus a desktop notification and, if configured, a webhook. |
| The provider resets the limit earlier than announced (a model launch, a quota change) | The wait notices within minutes and continues where it stopped: a sleeping wait re-checks usage every 5 minutes, fresh data from any other window counts too, and tools without a usage API retry on a 10/20/30/45/60-minute ladder. You see `⚡ 5h limit reset ahead of schedule`. |
| Claude finishes a task and stops with "moving on to the next thing" | In **queue mode** the Stop hook keeps the session going: next unchecked item, no confirmation prompts. Stops cleanly when the queue is empty (and tells you), or when nothing progresses. |
| You paste a long prompt with several things to do | It is a task list nobody put in a file, so Noctis makes one: the items are written to a checklist in its own folder (nothing is left in your project), you see `☰ 5-step job detected`, and the session runs until every item is ticked. Short prompts, questions, bug reports with pasted output and single tasks are left alone (`queue.auto: false` turns it off). |
| The weekly quota of the expensive model (e.g. Fable) runs out | Default model is switched to the fallback (e.g. Opus), the session is checkpointed and relaunched on it; reverted automatically when the quota resets. |
| A prompt is research/writing, not code | It is handed to a cheaper helper model (a *subagent*) so the expensive model's quota is kept for code; long test output is summarised by the cheapest model; file search never burns the primary model's quota. |
| A rate-limit error (429) kills the turn anyway | The session is woken **in place** when the limit resets, with a scheduled relaunch as the safety net. Claude Code's own error message ("weekly limit", "5-hour") is trusted over the percentages. |
| The API itself is overloaded (529 / 5xx) | It retries with growing pauses (30 s → 5 min, two-hour budget) instead of dying, tells you once, and after a real outage stops with one notice. |
| The usage numbers stop arriving near the limit | It stops instead of guessing: burn-rate projection, burst prediction, a blind-spot probe, and 15-second refreshes within two points of the wall. |

<details><summary>More situations it handles</summary>

| Situation | What happens |
|---|---|
| The task is a fan-out ("migrate every endpoint", "review all 40 files") | Claude is told to run it as a **dynamic workflow** with the models and efforts from your roles profile; a new workflow is refused inside the *warn band* (the last few points before a pause), every launch is recorded, and after a pause Claude is told to **relaunch the same run** (completed agents return saved results) instead of starting from zero. |
| You installed it at 99 % of the weekly quota | Setup still works; the first session explains what will happen and when work resumes, the first prompt is parked until the reset (or `/noctis:pause 120` to keep going), `noctis check` reports it to scripts. Plans without a Fable bucket or without weekly limits simply never see those rules fire. |
| You write to Claude in German, Japanese, Turkish… | Every notice, status line and toast follows the language of the current session (detected from what you type — no model call); the short `[noctis]` instructions given to Claude stay in English. All fourteen languages (en tr de fr es pt it nl pl ru ja zh ko ar) are complete: a Go test fails the build if any of them is missing a single message. Translations live in `i18n/<code>.json`; `node scripts/i18n.js build` regenerates the catalog. |
| Files changed while the session was waiting | The checkpoint fingerprints `git status`; if the tree differs at resume, Claude is told to re-read what it was editing first (`wait.workspaceGuard`). `checkpoint.gitSnapshot` can also pin the uncommitted changes as a hidden git ref. |
| Two things try to resume the same session | Only one relaunch ever happens: the session is claimed under a lock. |
| A long wait ends and the session comes back | It reappears where you can see it: a new tab in Windows Terminal, a Terminal window on macOS, your desktop terminal on Linux (headless if none is reachable). The window the plugin opened last time for that session is closed first, so they never pile up, and the old window's status line says `↪ continues in another window — this one can be closed`. |
| A terminal is closed, a process is killed, a file is half-written | The engine repairs itself: a corrupt `state.json`/`usage.json` is restored from its backup on the next read, a hand-off whose process is gone stops blocking the session, a parked wait that lost its runner gets a new one, and leftover temp files and locks are swept. |
| A new version is published | Marketplace auto-update (enabled by setup) downloads it after a session start; the next session shows `⬆ noctis 5.4.0 was downloaded (this session still runs 5.3.0): run /reload-plugins, or restart the tool, to switch.` (plus a desktop toast), once. If auto-update is off, a one-line notice names the version and the update command instead. |
</details>

<p align="center"><img src="docs/flow.svg" alt="One tool turn through the guard: signals feed the hooks, deterministic rules pick an outcome" width="100%"></p>

</details>

## Works alongside other plugins

Noctis adds hooks and a status line; it never removes or rewrites anyone else's. Claude Code runs every hook registered for an event, so a loop plugin (ralph-loop and friends) and Noctis's queue can both push the same turn — harmless, but if you see double continues, pause one of them. A status line you already had (ccstatusline, claude-powerline, …) keeps running behind Noctis's line. Usage dashboards (ccusage, Claude-Code-Usage-Monitor) read the same files Claude Code writes and are unaffected. Two tools that both auto-resume after a limit (unsnooze, claude-auto-resume) would race each other — keep one. `noctis doctor` lists the neighbouring hooks and plugins it can see on your machine.

## In your language

<p align="center"><img src="docs/languages.svg" alt="The same pause and queue-finished notices in English, Turkish, German, Spanish, Japanese and Russian" width="100%"></p>

## Other AI coding tools

Since 5.2 the same engine runs inside **OpenAI Codex CLI**, **Antigravity CLI** (Google), **Factory Droid** and **GitHub Copilot CLI**. From the zip or clone: `./scripts/install.sh` (macOS/Linux) or `.\scripts\install.ps1` (Windows) asks which tool with a numbered list — or pass `--host codex` / `-Tool codex` — then wires that tool's own hook file and resumes sessions with its own command. Codex and Antigravity expose their usage windows to scripts, so the full pause-before-the-wall guard works there; Droid and Copilot get queue mode, checkpoints and error retries. The per-tool table, what each one can and cannot do, and the smoke-test steps are in [docs/REFERENCE.md](docs/REFERENCE.md#other-ai-coding-tools) and [docs/HOSTS.md](docs/HOSTS.md).

## Friday night → Monday morning

<details>
<summary>A weekend that needs nobody: checkpoint, wait out the reset, relaunch on Monday.</summary>

<p align="center"><img src="docs/timeline.svg" alt="Timeline: checkpoint at 92 percent, wait, resume after the reset, save at the weekly limit, relaunch on Monday" width="100%"></p>

Nothing in that weekend needs you. The checkpoint holds the last request, touched files, `git status`, todos and the next queue items. A short reset is waited out inside the hook, so the turn simply continues afterwards; a long one is saved and relaunched at the reset time — on Windows the scheduled task can wake the PC from sleep, on macOS and Linux it runs as soon as the machine is awake.

<p align="center"><img src="docs/before-after.svg" alt="The same night with and without the plugin: 14 of 48 tasks versus 48 of 48" width="100%"></p>

</details>

## How it compares

| Capability | noctis | usage dashboards / status lines | loop plugins ("keep going") | auto-resume scripts |
|---|:---:|:---:|:---:|:---:|
| Stops **before** the wall (thresholds + burst + burn-rate projection) | ✅ | show only | — | react after the 429 |
| Resumes on its own after the reset (same session, or scheduled relaunch) | ✅ | — | — | partly |
| Keeps a task queue moving across stops, no confirmations — with priorities, dependencies and GitHub issues | ✅ | — | ✅ (flat) | — |
| Cheaper models for research, file search and noisy output | ✅ | — | — | — |
| Scoped-model fallback (e.g. Fable → Opus) with automatic revert | ✅ | — | — | — |
| Deterministic, zero tokens per decision, journaled (`noctis why`) | ✅ | ✅ | prompt-driven | varies |
| Overload (529/5xx) backoff with jitter, separate from limit handling | ✅ | — | — | some |
| ccusage-compatible cost report, exit-code gate for crons/CI | ✅ | ✅ / — | — | — |
| No runtime to install (single static binary, ~7 ms per hook) | ✅ | varies | varies | varies |
| Observe mode to watch decisions before enforcing anything | ✅ | — | — | — |
| Dynamic workflows: suggested for fan-out work, gated near the limit, rescued after a pause | ✅ | — | — | — |
| Roles profile: which model and effort does code, research, planning, digests, search, fallback | ✅ | — | — | — |
| Follows the language you are typing in (14 languages, all complete) | ✅ | some | — | — |
| Works inside Claude Code, Codex CLI, Antigravity CLI, Droid and Copilot CLI | ✅ | some | Claude only | some |

## Install (details)

<details>
<summary>Marketplace and clone installs, the roles profile, the flags, and what to do if the binary will not run.</summary>

<p align="center"><img src="docs/install.svg" alt="Install in sixty seconds: add the marketplace, install, run setup (which asks which model does which work), reload, write TASKS.md" width="100%"></p>

**From a marketplace (recommended)** — the four lines at the top. `setup` asks one question — **which model and effort should do which kind of work** — and remembers the answer as your *roles profile*: `noctis` (code and planning on Fable 5.1 · max, research and writing on Opus 5 · xhigh, digests and file search on Haiku · high, fallback Opus · max — for Max plans), `balanced`, `economy`, or `custom` per role. Run the skill again any time to change it, or skip the question with `--profile noctis|balanced|economy` or `--code opus:high --research sonnet:high …`.

It also makes three edits to `settings.json` (status line, permission mode, default model + effort) — listed key by key, with the undo for each, in [What it changes on your machine](#what-it-changes-on-your-machine--and-how-to-undo-it). Flags: `--permissions keep`, `--no-model`, `--updates keep`, `--preset conservative|balanced|aggressive` (pause at 85/82/90, 92/89/95 or 96/94/98 % of the 5-hour / weekly / Fable windows), `--config-dir <dir>` (only for a second account).

**From a clone / zip** (per-account copy under `~/.claude/skills/`): `.\scripts\install.ps1` on Windows, `./scripts/install.sh` on macOS/Linux — in a terminal both first ask which AI coding tool this is for. Add `--config-dir <dir>` (`-ConfigDir <dir>` in PowerShell) only for a second account. Then `/reload-plugins`. What lands there is 16 files and about 7.5 MB: your platform's binary, the hooks, the agents, the skills and the default config — not the Go source, the test suites, the docs or the other five platforms' binaries.

Nothing is downloaded or compiled: the binary for your OS is in the repo (`bin/<os>-<arch>/`, checksums in `bin/SHA256SUMS` — a binary that does not match its checksum, or is missing from it, is refused rather than installed), hooks never go through a shell, and Git Bash is not needed on Windows.

**If nothing happens at all** — no status line, no notices, and `noctis doctor` will not run either — the binary is not being allowed to execute. The binaries are not code-signed or notarized, so:
- **macOS**: a download through a browser (a release zip, "Download ZIP") gets a quarantine flag and is killed on sight. Clear it with `xattr -dr com.apple.quarantine <plugin folder>`, or install with `git clone`, which never sets the flag.
- **Windows**: SmartScreen may block a downloaded `.exe` — Properties → Unblock, or clone instead.
- **Linux/macOS from a zip**: zips do not always carry the executable bit. `chmod +x bin/noctis bin/*/noctis` fixes it.
- Anything else (an unusual CPU architecture, a locked-down machine): run `bin/<os>-<arch>/noctis version` directly in a terminal — the error it prints is the real one. Outside Claude Code, run `noctis` with the full path setup prints (`setup complete: binary at …/bin/noctis`) or add that folder to your PATH. Build from source: `cd go && go build -trimpath -ldflags="-s -w" ./cmd/noctis` (standard library only).

In the VS Code / Cursor extension everything works, except that an automatic relaunch after a long wait runs outside the editor (a separate terminal window on Windows, a background `claude --resume` elsewhere), not in an editor tab.

</details>

## Commands inside Claude Code

`/noctis:setup` (run again to change models) · `:status` (usage, pause points, pending waits, last decisions) · `:pause [minutes]` (switches the **guard** off for a while — Claude keeps working, even past a limit) · `:resume` (guard back on). From a terminal the same are `noctis setup`, `noctis status` + `noctis why`, `noctis off [minutes]`, `noctis on`.

## Status line

<p align="center"><img src="docs/statusline.svg" alt="The status line: 5-hour window, weekly window with pace marker, scoped bucket, ETA, model, context" width="100%"></p>

```
∞ 5h %41→14:35 · Wk %23▲→Mon 21.09 · Fable %60 · ⌛ Fable ~1d 3h · Fable 5.1/max · ctx %37
```

`▲ / ● / ▼` shows whether you are ahead of, on, or behind an even weekly pace (no tokens spent). `⌛` shows when a pause point will be reached at the current burn rate. `⏸` shows a pending resume time, `⚠ hooks inactive` means the status line updates but no hook has run for 30 minutes, `👁` means observe mode. `statusline.mode: silent` keeps the data capture but prints nothing (or only your chained status line).

## Reference and limits

Every command (`noctis status`, `noctis check`, `noctis why`, `noctis doctor`, `noctis report`, `noctis queue import`, `noctis version`, …), the full configuration table, the host adapters and the file layout are in [docs/REFERENCE.md](docs/REFERENCE.md). Design notes and the bug log (Turkish): [docs/PLAN.md](docs/PLAN.md). Three limits worth knowing: usage data comes from Claude Code's official status-line payload (`rate_limits`, Claude Code ≥ 2.1.251) and, for scoped buckets, from the undocumented OAuth usage endpoint, so keep an eye on `errors.log` after Claude Code updates; same-session wake relies on `asyncRewake`, documented as observational, with the scheduled relaunch as the fallback; hooks cannot type `/clear`, so compaction stays with Claude Code.

Ten suites cover the hook contract, a black-box lab against the binary, thousands of concurrent sessions, injected machine failures, the OS schedulers, multi-week soaks, source hygiene and the Go unit and fuzz tests — what each one asks, and what the latest run measured, are in [docs/TESTING.md](docs/TESTING.md).

## Contributing

Bug reports with `noctis doctor` output and the relevant `errors.log` / `noctis why --last 20` lines are the most useful thing you can send. See [CONTRIBUTING.md](CONTRIBUTING.md) for the build and test loop.

## License

MIT — © 2026 synex
