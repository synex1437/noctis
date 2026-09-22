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

You queued forty tasks, went to bed, and woke up to a session that died on a usage limit at 01:40 — or stopped at task three with *"moving on to the next thing"*.

**Noctis** keeps long Claude Code jobs running unattended. It pauses just *before* the 5-hour or weekly limit, waits for the reset, and continues the same session. It works down a `TASKS.md` checklist without asking, and sends research to a cheaper model so the expensive one's quota lasts. It never calls a model and never spends a token deciding. Nothing to type day to day.

For **Pro or Max** subscribers, and — since v5.2 — inside OpenAI Codex CLI, Antigravity CLI, Factory Droid and GitHub Copilot CLI.

**Install** — four lines inside Claude Code, about a minute. The third asks one question: which model does which kind of work.

```
/plugin marketplace add synex1437/noctis
/plugin install noctis
/noctis:setup
/reload-plugins
```

**Requirements:** Claude Code 2.1.251 or newer, a Pro or Max account, Windows, macOS or Linux. No Node, no Git Bash, no compiler.

Setup edits `~/.claude/settings.json`: status line, default model, and **`permissions.defaultMode` → `auto`, so Claude edits files and runs commands without asking**. That is what lets it work overnight; `--permissions keep` opts out. Every key and its undo is [listed below](#what-it-changes-on-your-machine--and-how-to-undo-it).

Not sure your plan includes Fable? Pick `balanced` or `economy` at setup — `noctis` assumes Max. On an API key there are no usage windows, so the limit guard stays idle after one notice; queue mode and the router still work. Another AI coding tool? See [Other AI coding tools](#other-ai-coding-tools).

<p align="center"><img src="docs/demo.svg" alt="A night with the plugin: pauses at the 5-hour limit, resumes after the reset, routes research to a cheaper model, switches model when the scoped quota is out, relaunches an interrupted workflow, finishes the queue by morning" width="100%"></p>

## What it changes on your machine — and how to undo it

<details>
<summary>Every settings key setup writes, what it talks to on the network, and the undo for each.</summary>

Setup writes a backup `settings.json.bak-<time>` first, then touches exactly this:

| Where | What | Undo |
|---|---|---|
| `settings.json` → `statusLine` | Points it at the plugin's binary — that is how Claude Code reports your usage. A status line you already had keeps running behind it. | `noctis install --uninstall` |
| `settings.json` → `permissions.defaultMode` | Set to `auto` (older Claude Code: `acceptEdits`). **Claude will edit files and run commands without asking you first** — that is what lets it work while you sleep. Never `bypassPermissions`: an unattended relaunch refuses that mode even if a config asks for it. | `--permissions keep` at setup; uninstall removes exactly what setup wrote |
| `settings.json` → `model`, `env.CLAUDE_CODE_EFFORT_LEVEL` | Default model = the *code* model of your profile (**Fable 5.1 · max** by default). Skipped if your default is already Fable or Opus. | `--no-model` at setup; uninstall restores the old value |
| `~/.claude/noctis/` | Its own `config.json`, usage snapshots, checkpoints, logs | Delete the folder |
| A scheduled task, only while a resume is pending | Task Scheduler `Noctis-…` (can wake the PC), launchd `com.synex.noctis.…`, systemd `noctis-…` | `noctis cancel`; `"alarm": {"wakePc": false}` stops the wake |
| Marketplace auto-update | Setup runs `claude plugin marketplace update noctis --auto-update`, so new versions arrive on their own. | `--updates keep` at setup, or `/plugin` → Marketplaces |

**Network.** Two servers. `api.anthropic.com`, the usage endpoint the Claude app itself uses, with the login token Claude Code already keeps — on macOS that lives in the Keychain, so expect one "allow access" prompt. Every 10 minutes, down to every 15 seconds in the last two points before a pause. And once a day, the raw `plugin.json` on GitHub, to see whether a newer version exists — fetched by a detached process, so a session start never waits for it (`update.check: false` turns that off). Plus your webhook URL, if you set one. No telemetry.

**It never spends paid usage credits.** Work stops at 100 % of a window even if your thresholds are misconfigured, and even while `/noctis:pause` is running, because past that point the account pays for the overflow. A dynamic workflow fans out many agents at once and can burn the last points between two checks, so one is refused unless a window has 25 points of room. `"credits": {"allowPaid": true}` if you *want* the overflow; `ceiling` and `fanOutHeadroom` tune the rest. What noctis cannot do is switch off auto-reload on the account — that is in your Anthropic billing settings.

**Queue mode runs a checklist for you — once you say it may.** When the project folder holds a `TASKS.md` (or `tasks.md`, `.claude/TASKS.md`, `docs/TASKS.md`) with open `- [ ]` items, the first session there says so, and says it is **not** driving anything yet:

```text
☰ TASKS.md in this folder holds 12 open item(s). It is not driving this session: a checklist can
  arrive with a repository, and driving means working through it without stopping to ask.
  To let it drive: noctis queue trust
```

`noctis queue trust` turns it on for that file, `untrust` off, `status` says where it stands. A checklist noctis wrote from your own prompt needs no permission — you already asked for it. The gate exists because a `TASKS.md` can arrive with a `git clone`, and "work through this list without asking" is not something a downloaded file should be able to say. Old always-on behaviour: `"queue": {"requireTrust": false}`. Never want it: `"queue": {"files": []}`.

**Turn it off.** For a while: `/noctis:pause 120` (minutes — Claude keeps working, the guard sleeps), `:resume` switches it back on. Watch only: `"mode": "observe"` in `config.json`. Completely:

```
noctis cancel                 # drop pending resumes and their scheduled tasks
noctis install --uninstall    # restore settings.json — run this FIRST, because removing the
                              # plugin deletes the binary that knows how to undo the edits
/plugin uninstall noctis      # inside Claude Code
```

Clone install: `scripts/install.sh --uninstall` or `scripts\install.ps1 -Uninstall` instead of the middle line.

If the plugin is already gone and the settings remain, undo it by hand: remove `permissions.defaultMode`, `model`, `env.CLAUDE_CODE_EFFORT_LEVEL` and the `statusLine` block from `~/.claude/settings.json`, or restore the `settings.json.bak-<time>` copy. `permissions.defaultMode` is the one that matters: left behind, every future session keeps editing files and running commands without asking.

</details>

## The first five minutes

1. After `/reload-plugins`, look at the bottom of the window: `∞ 5h %41→14:35 · Wk %23▲→Mon 21.09 · Fable 5.1/max · ctx %37` — both usage windows, when each resets, the current model. If it says *waiting for limit data*, send one message; it fills in after the first reply.
2. Type `/noctis:status`: usage, pause points, which model does which job, and the last decisions the plugin made.
3. Write the three-line `TASKS.md` below and tell Claude "work through TASKS.md". Watch it tick items off on its own.
4. At a limit there is nothing to do. A short reset is waited out inside the turn, context intact. A long one is saved and resumed as `claude --resume` of the same session — a new terminal window on Windows, in the background elsewhere with output in `resume-output.log`. Windows and Linux ask to wake the machine (Linux needs `CAP_WAKE_ALARM`); launchd cannot wake a Mac, so keep it awake for the night.
5. Not ready to trust it? `"mode": "observe"` in `~/.claude/noctis/config.json` logs every decision and enforces nothing.

## Queue file

<details>
<summary>The `TASKS.md` format, plus priorities, tags, dependencies and GitHub issues.</summary>

One task per line, in the folder where you start `claude`:

```markdown
- [ ] add input validation to the signup form
- [ ] write tests for the payments module
- [ ] update README for the new CLI flags
```

That is the whole format. Claude takes the first open item, ticks it `- [x]` when done, and moves on without asking; at the end it says `✔ Queue finished` and stops. Sloppy lists are accepted (`-[ ]`, `* [ ]`, `1. [ ]`, `[]`, `TODO:`), an item wrapped over several lines is one item, and `~~struck~~`, `(done)` and `✓` count as finished.

Priorities and dependencies:

```markdown
- [ ] (P0) fix the login redirect #auth
- [ ] (P1) migrate users (after #auth, #db)
- [ ] deploy (after 2)
```

`(P0)`–`(P9)` orders the work (default P5, lower first) and `#name` tags an item. `(after #tag)` or `(after 3)` waits until the referenced items are checked; `(after 2)` means the 2nd item in the file. The Stop hook hands Claude the best eligible item, says how many are still waiting, and stops cleanly when all are blocked or done. A reference that matches nothing is ignored, so a typo never deadlocks a night.

`noctis queue import` appends open GitHub issues as `- [ ] (P1) #123 Title` through the `gh` CLI — priority from `P0`–`P9` or `priority: high` labels, idempotent — and `queue.github.closeOnDone` closes an issue when its item is ticked.

</details>

## What it does while you're away

<details>
<summary>Situation by situation: limits, early resets, queue stops, quota switches and the failures it handles alone.</summary>

| Situation | What happens |
|---|---|
| Usage nears the limit (92 % / 89 %) | The turn pauses **before** the wall, after a checkpoint of the last request, touched files, `git status`, todos and next queue items. Short resets are waited out in place; long ones are saved and auto-resumed by a scheduled task — Task Scheduler (can wake the PC), launchd or systemd — plus a notification and, if set, a webhook. |
| The reset comes early | The wait notices within minutes: it re-checks every 5 minutes, counts fresh data from any window, and without a usage API retries on a 10/20/30/45/60-minute ladder. `⚡ 5h limit reset ahead of schedule`. |
| Claude stops with "moving on to the next thing" | In queue mode the Stop hook hands it the next unchecked item, no confirmations, and stops cleanly when the queue is empty. |
| You paste a long prompt with several tasks | noctis writes the checklist itself, in its own folder, and runs it. `☰ 5-step job detected`. Short prompts and single tasks are left alone (`queue.auto: false`). |
| The expensive model's weekly quota runs out | The default model and effort switch to the fallback, the session is checkpointed and relaunched on it, and both revert at the reset. |
| A prompt is research or writing | It goes to a cheaper subagent. Noisy test output is digested by the cheapest model, and file search never touches the primary model's quota. |
| A 429 kills the turn anyway | The session is woken **in place** at the reset, with a scheduled relaunch as the net. Claude Code's own error text is trusted over the percentages. |
| The API is overloaded (529 / 5xx) | Growing pauses instead of dying: 30 s → 5 min, two-hour budget, one notice, one more if the outage is real. |
| Usage numbers stop arriving near the limit | It stops rather than guess: burn-rate projection, burst prediction, a blind-spot probe, 15-second refreshes inside two points of the wall. |
| A window is already at 100 % | Work stops there whatever the thresholds say, and a pause does not lift it: past that point the account pays for the overflow. |
| The task is a fan-out ("migrate every endpoint") | Claude is told to run a **dynamic workflow** with your roles profile's models. It is refused in the warn band and unless a window has 25 points of room, every launch is recorded, and after a pause Claude relaunches the same run so finished agents return saved results. |
| You install at 99 % of the weekly quota | Setup works; the first session says when work resumes, the first prompt is parked until the reset (`/noctis:pause 120` to keep going), and `noctis check` reports it to scripts. |
| You write in German, Japanese, Turkish… | Notices, status line and toasts follow the language you type, detected without a model call. All fourteen are complete — a Go test fails the build if one lacks a message, takes different arguments from the English, or breaks the `noctis status` columns. The `[noctis]` instructions handed to Claude stay English everywhere. |
| Files changed while the session waited | The checkpoint fingerprints `git status`; if the tree differs at resume, Claude re-reads what it was editing first (`wait.workspaceGuard`). `checkpoint.gitSnapshot` can pin uncommitted changes as a hidden git ref. |
| Two things try to resume one session | Only one relaunch happens; the session is claimed under a lock. |
| A long wait ends | The session returns where you can see it — a Windows Terminal tab, a macOS Terminal window, your Linux desktop terminal, headless if none is reachable. The previous window for that session is closed first and says `↪ continues in another window`. |
| A terminal is closed, a process killed, a file half-written | The engine repairs itself: corrupt `state.json`/`usage.json` restored from backup, a dead hand-off unblocked, a runnerless wait re-scheduled, stray temp files and locks swept. |
| A threshold is edited into nonsense | The shipped default guards that window instead, and `noctis status`, `noctis doctor` and the next session all name it. Set a threshold to `0` or `null` when you really want a window left unguarded. |
| A new version is published | Auto-update downloads it after a session start and the next session says `⬆ noctis 5.5.3 was downloaded (this session still runs 5.5.2): run /reload-plugins` once. With auto-update off, a one-line notice names the version and the command. |

<p align="center"><img src="docs/flow.svg" alt="One tool turn through the guard: signals feed the hooks, deterministic rules pick an outcome" width="100%"></p>

</details>

## Works alongside other plugins

Noctis adds hooks and a status line; it never removes or rewrites anyone else's. Claude Code runs every hook for an event, so a loop plugin and noctis's queue can both push the same turn — harmless, but if you see double continues, pause one. A status line you already had (ccstatusline, claude-powerline) keeps running behind noctis's, and usage dashboards (ccusage, Claude-Code-Usage-Monitor) read the same files and are unaffected. Two tools that both auto-resume (unsnooze, claude-auto-resume) would race — keep one. `noctis doctor` lists the neighbours it can see.

## In your language

<p align="center"><img src="docs/languages.svg" alt="The same pause and queue-finished notices in English, Turkish, German, Spanish, Japanese and Russian" width="100%"></p>

## Other AI coding tools

Since 5.2 the same engine runs inside **OpenAI Codex CLI**, **Antigravity CLI** (Google), **Factory Droid** and **GitHub Copilot CLI**. From the zip or clone, `./scripts/install.sh` or `.\scripts\install.ps1` asks which tool — or pass `--host codex` / `-Tool codex` — then wires that tool's hook file and resumes with its own command. Codex and Antigravity expose usage windows to scripts, so the full guard works there; Droid and Copilot get queue mode, checkpoints and error retries. Details: [docs/REFERENCE.md](docs/REFERENCE.md#other-ai-coding-tools) and [docs/HOSTS.md](docs/HOSTS.md).

## Friday night → Monday morning

<details>
<summary>A weekend that needs nobody: checkpoint, wait out the reset, relaunch on Monday.</summary>

<p align="center"><img src="docs/timeline.svg" alt="Timeline: checkpoint at 92 percent, wait, resume after the reset, save at the weekly limit, relaunch on Monday" width="100%"></p>

Nothing in that weekend needs you. The checkpoint holds the last request, touched files, `git status`, todos and the next queue items. A short reset is waited out inside the hook, so the turn simply continues; a long one is relaunched at the reset time — on Windows the task can wake the PC, elsewhere it runs as soon as the machine is awake.

<p align="center"><img src="docs/before-after.svg" alt="The same night with and without the plugin: 14 of 48 tasks versus 48 of 48" width="100%"></p>

</details>

## How it compares

| Capability | noctis | usage dashboards / status lines | loop plugins ("keep going") | auto-resume scripts |
|---|:---:|:---:|:---:|:---:|
| Stops **before** the wall (thresholds + burst + burn-rate projection) | ✅ | show only | — | react after the 429 |
| Resumes on its own after the reset (same session, or scheduled relaunch) | ✅ | — | — | partly |
| Never spends paid usage credits, and gates fan-out by measured headroom | ✅ | — | — | — |
| Keeps a task queue moving across stops, no confirmations — priorities, dependencies, GitHub issues | ✅ | — | ✅ (flat) | — |
| Cheaper models for research, file search and noisy output | ✅ | — | — | — |
| Scoped-model fallback (e.g. Fable → Opus) with automatic revert | ✅ | — | — | — |
| Deterministic, zero tokens per decision, journaled (`noctis why`) | ✅ | ✅ | prompt-driven | varies |
| Overload (529/5xx) backoff with jitter, separate from limit handling | ✅ | — | — | some |
| ccusage-compatible cost report, exit-code gate for crons/CI | ✅ | ✅ / — | — | — |
| No runtime to install (single static binary, ~7 ms per hook) | ✅ | varies | varies | varies |
| Observe mode to watch decisions before enforcing anything | ✅ | — | — | — |
| Dynamic workflows: suggested for fan-out, gated near the limit, rescued after a pause | ✅ | — | — | — |
| Roles profile: which model does code, research, planning, digests, search, fallback | ✅ | — | — | — |
| Follows the language you type in (14 languages, all complete) | ✅ | some | — | — |
| Works inside Claude Code, Codex CLI, Antigravity CLI, Droid and Copilot CLI | ✅ | some | Claude only | some |

## Install (details)

<details>
<summary>Marketplace and clone installs, the roles profile, the flags, and what to do if the binary will not run.</summary>

<p align="center"><img src="docs/install.svg" alt="Install in sixty seconds: add the marketplace, install, run setup (which asks which model does which work), reload, write TASKS.md" width="100%"></p>

**From a marketplace** — the four lines at the top. `setup` asks one thing, **which model should do which kind of work**, and remembers it as your *roles profile*:

| Profile | code | research & writing | planning | digests & search | fallback |
|---|---|---|---|---|---|
| `noctis` (Max plans) | Fable 5.1 · max | Opus 5 · xhigh | Fable 5.1 | Haiku 4.5 · high | Opus 5 · max |
| `balanced` | Opus 5 · high | Sonnet 5 · high | Opus 5 | Haiku 4.5 | Sonnet 5 |
| `economy` | Sonnet 5 · high | Haiku 4.5 · high | Opus 5 | Haiku 4.5 | Haiku 4.5 |
| `custom` | a model per role, with an effort where one applies | | | | |

Run the skill again to change it, or skip the question with `--profile noctis|balanced|economy` or `--code opus:high --research sonnet:high …`; `/noctis:status` shows the current assignment. Effort reaches the main session, the research subagent and the digest subagent. `Plan` and `Explore` take a model only, because the Agent tool has no effort to give them, and setup says so instead of pretending otherwise.

Setup also makes three edits to `settings.json` — status line, permission mode, default model and effort — listed with their undo in [What it changes on your machine](#what-it-changes-on-your-machine--and-how-to-undo-it). Flags: `--permissions keep`, `--no-model`, `--updates keep`, `--config-dir <dir>` for a second account, `--preset conservative|balanced|aggressive` (pause at 85/82/90, 92/89/95 or 96/94/98 %).

**From a clone or zip**, a per-account copy lands under `~/.claude/skills/`: run `.\scripts\install.ps1` or `./scripts/install.sh` — both first ask which AI coding tool this is for — then `/reload-plugins`. That copy is 16 files and about 7.5 MB: your platform's binary, the hooks, agents, skills and default config. Nothing is downloaded or compiled; the binary is already in the repo (`bin/<os>-<arch>/`), and one that does not match `bin/SHA256SUMS` is refused rather than installed. Hooks never go through a shell, and Git Bash is not needed on Windows.

**If nothing happens at all** — no status line, no notices, and `noctis doctor` will not run either — the binary is not being allowed to execute. The binaries are not code-signed or notarized, so:

- **macOS**: a browser download gets a quarantine flag and is killed on sight. `xattr -dr com.apple.quarantine <plugin folder>`, or install with `git clone`, which never sets the flag.
- **Windows**: SmartScreen may block a downloaded `.exe` — Properties → Unblock, or clone instead.
- **From a zip**: zips do not always carry the executable bit. `chmod +x bin/noctis bin/*/noctis`.
- **Anything else**: run `bin/<os>-<arch>/noctis version` in a terminal — the error it prints is the real one. Outside Claude Code, use the full path setup prints, or add that folder to your PATH. Build from source: `cd go && go build -trimpath -ldflags="-s -w" ./cmd/noctis` (standard library only).

In the VS Code and Cursor extensions everything works, except that a relaunch after a long wait runs outside the editor — a separate terminal window on Windows, a background `claude --resume` elsewhere — not in an editor tab.

</details>

## Commands inside Claude Code

`/noctis:setup` (run again to change models) · `:status` (usage, pause points, roles, pending waits, last decisions) · `:pause [minutes]` (switches the **guard** off for a while — Claude keeps working) · `:resume` (guard back on).

From a terminal: `noctis setup`, `noctis status` and `noctis why`, `noctis off [minutes]`, `noctis on`.

## Status line

<p align="center"><img src="docs/statusline.svg" alt="The status line: 5-hour window, weekly window with pace marker, scoped bucket, ETA, model, context" width="100%"></p>

```
∞ 5h %41→14:35 · Wk %23▲→Mon 21.09 · Fable %60 · ⌛ Fable ~1d 3h · Fable 5.1/max · ctx %37
```

`▲ / ● / ▼` shows whether you are ahead of, on, or behind an even weekly pace. `⌛` shows when a pause point will be reached at the current burn rate. `⏸` shows a pending resume time, `⚠ hooks inactive` means the status line updates but no hook has run for 30 minutes, `👁` means observe mode. None of it spends a token. `statusline.mode: silent` keeps the data capture but prints nothing, or only your chained status line.

## Reference and limits

Every command, the full configuration table, the host adapters and the file layout are in [docs/REFERENCE.md](docs/REFERENCE.md). Design notes and the bug log (Turkish): [docs/PLAN.md](docs/PLAN.md). The ten test suites, and what the latest run measured: [docs/TESTING.md](docs/TESTING.md).

Three limits worth knowing. Usage data comes from Claude Code's status-line payload (`rate_limits`, Claude Code ≥ 2.1.251) and, for scoped buckets, from the undocumented OAuth usage endpoint — so watch `errors.log` after Claude Code updates. Same-session wake relies on `asyncRewake`, documented as observational, with the scheduled relaunch as the fallback. And hooks cannot type `/clear`, so compaction stays with Claude Code.

## Contributing

Bug reports with `noctis doctor` output and the relevant `errors.log` / `noctis why --last 20` lines are the most useful thing you can send. See [CONTRIBUTING.md](CONTRIBUTING.md) for the build and test loop.

## License

MIT — © 2026 synex
