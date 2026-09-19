# Noctis — reference

The README says what the plugin does and how to install it; this page lists every command, configuration key, host adapter and file.

## Commands

`noctis status`, `noctis check [--json]`, `noctis report --bundle [file.zip]`, `noctis why [--last 20] [--json]`, `noctis doctor`, `noctis selftest`, `noctis report [--days 7] [--json | --html [file]]`, `noctis queue import [--repo owner/name] [--label name]`, `noctis cancel [sid]`, `noctis off [minutes]`, `noctis on`, `noctis checkpoint`, `noctis model [--sid <id>]`, `noctis schedule-preview [--backend launchd|systemd|task] [--sid <id>] [--at <epoch>]`, `noctis classify "<prompt>"`, `noctis setup [--profile …] [--permissions auto|acceptEdits|keep] [--updates keep] [--host …]`, `noctis ensure`, `noctis install [--host …] [--uninstall]`, `noctis version`.

`noctis check` is the exit-code gate for crons, CI and other agents: `0` under every threshold, `10` inside the warn band, `11` a threshold is reached (the hooks would pause right now), `20` no or stale usage data. It records nothing and never touches the network.

`report --json` is ccusage-compatible (`daily[]` with `inputTokens/outputTokens/cacheCreationTokens/cacheReadTokens/totalTokens/totalCost/modelsUsed/modelBreakdowns`) and adds `keptOffPrimary` — the tokens subagents spent on other models instead of the primary one, with the API-equivalent cost they avoided. Costs use the published list prices (informational on a subscription); `report.pricing` in `config.json` overrides or extends the table. `report --html` writes a single-file page.

## Configuration — `<account>/noctis/config.json`

| Key | Default | Meaning |
|---|---|---|
| `locale` | `auto` | `auto` follows the language of each session's prompts (en, tr, de, fr, es, pt, it, nl, pl, ru, ja, zh, ko, ar), before that `LC_ALL`/`LANG`; or pin one code. The `NOCTIS_LANG` environment variable overrides both |
| `roles.profile / code / research / planning / digest / explore / fallback` | noctis / fable:max / opus:xhigh / fable:max / haiku:high / haiku:high / opus:max | Which model (and effort) does which work; setup writes it and projects it onto `models.*`, `router.subagentModels` and the plugin's own agents; `noctis ensure` keeps the agents in step after marketplace updates |
| `workflow.suggest / gate / size` | true / true / medium | Suggest a dynamic workflow (ultracode) for fan-out prompts and queue items, with per-role models; deny a new `Workflow` launch inside the warn band or over a threshold; the size hint passed along |
| `mode` | `enforce` | `observe` = journal every decision, enforce nothing (try it for the first days) |
| `thresholds.session5h / weeklyAll / weeklyFable` | 92 / 89 / 95 | Pause thresholds (`weeklyScoped` is an alias for the scoped model) |
| `models.primary / fallback / effort` | fable / opus / max | Default model, fallback when the scoped quota is out, effort level |
| `models.scopedPattern / scopedLabel` | `fable` / Fable | Which OAuth bucket and which model names the scoped rule applies to |
| `router.enabled / agent / minPromptLength / maxDeniesPerPrompt` | true / lite / 15 / 3 | Research router |
| `router.digest / digestAgent` | true / digest | Digest subagent for noisy output |
| `router.subagentModels` | `{"Explore": "haiku", "Plan": "fable"}` | Model pinned to built-in subagents spawned without one (setup projects the roles profile onto it) |
| `fable.source / pollMinutes / revertOnReset / autoRelaunch` | oauth / 10 / true / true | Scoped-bucket polling (OAuth usage endpoint), revert, relaunch |
| `usage.staleMinutes / multiSessionMax / blindProbeSeconds / blindProbeRounds` | 20 / true / 60 / 5 | Stale-data rules; `multiSessionMax` keeps the highest value per window across concurrent sessions |
| `wait.maxInHookMinutes / resetMarginSeconds / builtinGraceSeconds / heartbeatGraceSeconds` | 330 / 90 / 180 / 45 | In-hook wait cap and margins |
| `wait.workspaceGuard` | true | Fingerprint `git status` at every pause; if the tree changed while waiting, tell you and tell Claude to re-check before continuing |
| `overload.baseSeconds / maxSeconds / maxTotalMinutes` | 30 / 300 / 120 | Exponential backoff (±25 % jitter) for `overloaded` / `server_error` turns; the episode ends when a call succeeds or the budget is spent |
| `checkpoint.gitSnapshot` | false | Pin the uncommitted tracked changes as a hidden ref (`refs/noctis/<sid>/<time>`, via `git stash create`) at every checkpoint; restore with `git stash apply <hash>`; pruned with the checkpoint |
| `wake.sameSession / maxMinutes / graceSeconds` | true / 330 / 300 | Wake the same session after a 429 (async hook + exit 2); the runner relaunches if the wake did not take |
| `resume.mode / permissionMode / prompt / remoteControl / extraArgs` | window / auto / … / false / [] | Relaunch mode; `permissionMode: inherit` reuses the session's own mode; `remoteControl` adds `--remote-control` |
| `managedPermissionMode` / `managedModel` | (set by setup) | What setup wrote into `settings.json` (`permissions.defaultMode`, `model`), so uninstall restores exactly that and nothing you set yourself |
| `budget.dailyWeeklyPercent / hardStop` | 0 / false | Soft daily cap on weekly usage (notice), or a hard pause until local midnight |
| `alarm.enabled / wakePc / webhook` | true / true / — | Beep + toast; `webhook: {url, preset: generic|telegram|discord|slack|ntfy, chatId}` with 3 retries and a circuit breaker |
| `statusline.mode / chainCommand / emoji / pace` | own / "" / true / true | Status line rendering |
| `wait.earlyResetPollMinutes` | 5 | How often a sleeping wait re-checks usage so a limit reset that happens earlier than announced is noticed; `0` turns the check off (fresh status-line data from another window still triggers a resume) |
| `wait.retryMinutes` | [10, 20, 30, 45, 60] | Retry ladder when a limit was hit but no usage data says when it ends (Droid, Copilot, unknown errors) |
| `resume.terminal` | `auto` | Where a relaunch appears: `auto` (Windows Terminal tab → console; macOS Terminal; a Linux desktop terminal; headless if none), `none` (always headless), or a command template containing `{script}` |
| `resume.closePrevious` | true | Close the window the plugin itself opened for this session last time before opening a new one; windows you opened are never touched |
| `queue.auto` | true | Turn a long multi-item prompt (≥ 240 characters) into a checklist in `<account>/noctis/queues/<session>.md` and drive it like `TASKS.md`. Three shapes count: a bullet or numbered list of ≥ 3 items; ≥ 4 lines that each open with a work verb (add, fix, write, ekle, düzelt, …); or one paragraph of ≥ 4 such sentences tied by sequence words (then, finally, sonra, …). Questions, bug reports with pasted output or "steps to reproduce", and descriptive context lines are left alone; items already ticked `[x]` are skipped. A project queue file always wins; the checklist is removed when done or when you steer manually |
| `queue.enabled / files / maxIdleContinues / maxContinuesPerSession / completionPromise` | true / `TASKS.md, tasks.md, .claude/TASKS.md, docs/TASKS.md` / 4 / 200 / "" | Queue mode; the file names that switch it on; `completionPromise: "DONE"` lets `<promise>DONE</promise>` end the loop |
| `queue.github.closeOnDone / comment` | false / … | Close a GitHub issue (`gh issue close`) when its `#N` item goes from open to checked |
| `report.pricing` | `{}` | USD per million tokens per model substring (`input/output/cacheWrite/cacheRead`), overriding the built-in list prices |
| `compaction.contextPercent` | 85 (or autocompact override − 5) | Limit safety gate before a context compaction |
| `host` | `claude` | Which AI coding tool the hooks belong to: `claude`, `codex`, `antigravity`, `droid`, `copilot` (set by setup, see [Other AI coding tools](#other-ai-coding-tools)) |
| `update.check / url` | true / GitHub raw `plugin.json` | Once a day at session start, fetch the published version and show one line when a newer one exists (3 s budget, nothing about you is sent) |
| `resume.droidAuto / copilotAllowAllTools` | high / false | Permission level for unattended resumes on Factory Droid (`--auto`) and GitHub Copilot CLI (`--allow-all-tools`, off by default — turning it on pre-approves every tool for a session nobody is watching) |
| `queue.requireTrust` | true | A queue file must be agreed to (`noctis queue trust`) before it drives a session. Set false to let any `TASKS.md` in a folder drive, as versions before 5.4 did |
| `resume.permissionMode` | auto | `default`, `acceptEdits`, `plan`, `auto`, or `inherit` (reuse the session's own). `bypassPermissions` and `dontAsk` are refused for unattended relaunches and fall back to `acceptEdits` |

**Environment variables.** These change behaviour in any build, not only in tests — they exist for the test suite and for escape hatches:

| Variable | Effect |
| --- | --- |
| `NOCTIS_LANG` | Forces the interface language instead of detecting it from your prompts |
| `NOCTIS_NO_TERMINAL` | Never open a terminal window for a relaunch (background resume instead) |
| `NOCTIS_NO_TASKS` / `NOCTIS_NO_SCHEDULE` | Skip the OS scheduler; use an in-process sleeper |
| `NOCTIS_NO_QUIET` | Disable the quiet fast path (every hook takes the full decision) |
| `NOCTIS_NO_WATCHER` / `NOCTIS_NO_EARLY_TRIGGER` | Turn off the early-reset watcher and the status-line-driven early resume |
| `NOCTIS_HOST` | Pick the host adapter instead of detecting it |
| `NOCTIS_TIME_OFFSET` | Shift the engine's clock (seconds) — test use |
| `NOCTIS_USAGE_URL` | Point the usage fetch elsewhere; only the real endpoint's host or localhost is accepted, because the request carries your OAuth token |
| `NOCTIS_UPDATE_URL` | Version-check URL, or `off` |
| `NOCTIS_PLUGIN_ROOT` | Plugin root, if it must be found somewhere other than next to the binary; ignored unless it really is one |
| `NOCTIS_DEBUG_HOOKS` | Append every raw hook payload to `hooks-debug.log` (prompts and tool output land in that file) |
| `CLAUDE_CODE_OAUTH_TOKEN` | Token for the usage API, instead of the credentials file or the macOS Keychain |
| `NOCTIS_ANTIGRAVITY_HOOKS` | Antigravity hook-file path — test use |

## Other AI coding tools

The engine is host-independent; a thin adapter per tool writes that tool's hook file, translates hook JSON in and out, reads usage where the tool exposes it, and resumes sessions with the tool's own command. `scripts/install.sh` / `install.ps1` (or `noctis setup` in a terminal) ask **which tool** with a numbered list; `--host <id>` skips the question.

| Tool | Hooks (file) | Usage windows read from | Pause before the wall | Queue mode, checkpoints | Resume after a wait | Notes |
|---|---|---|---|---|---|---|
| **Claude Code** (`claude`) | plugin `hooks/hooks.json` (exec form) | status line + OAuth usage endpoint | ✅ 5-hour, weekly, scoped bucket | ✅ | `claude --resume <id>` | Reference host; model fallback, routing and same-session wake are Claude-only |
| **OpenAI Codex CLI** (`codex`) | `~/.codex/hooks.json` (`SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`, `SessionEnd`) | `codex app-server` → `account/rateLimits/read` (windows classified by duration, so the on-and-off 5-hour window is handled) | ✅ | ✅ | `codex exec resume <id> "<prompt>"` | Run `/hooks` once in Codex and trust the plugin's hooks; Codex has no custom status line |
| **Antigravity CLI** (`agy`, Google) | `~/.gemini/config/hooks.json` (`PreToolUse`, `PreInvocation`, `PostInvocation`, `Stop`) + `statusLine` in `~/.gemini/antigravity-cli/settings.json` | the status-line payload's `quota` (remaining fraction + reset time per bucket) | ✅ | ✅ (`Stop` → `decision: continue`) | `agy --conversation <id> -p "<prompt>"` | `PostInvocation` terminates the loop at the threshold; an `error` stop with a quota message is treated as a limit |
| **Factory Droid** (`droid`) | `~/.factory/hooks.json` | — (`/limits` is interactive only) | — | ✅ | `droid exec --session-id <id> --auto high "<prompt>"` | Limit guard off; queue, checkpoints and error retries work |
| **GitHub Copilot CLI** (`copilot`) | `~/.copilot/hooks/noctis.json` (exec form) | — (monthly credits, no windows) | — | ✅ (`agentStop`; the 8-block cap is respected via `stop_hook_active`) | `copilot --resume=<id> -p "<prompt>" --allow-all-tools` | `errorOccurred` with a rate-limit message schedules a retry |

Not supported (checked September 2026): Gemini CLI (consumer access ended June 2026 — use Antigravity CLI), Kiro CLI (`AgentStop` cannot block), Cursor / OpenCode / Amp / Qwen Code (hooks exist but no script-readable limits; queue-only support is on the list), Roo Code (archived), Cline / Goose / Crush (no continue hook).

Install for another tool from the zip or clone: `./scripts/install.sh --host codex` (macOS/Linux) or `.\scripts\install.ps1 -Tool codex` (Windows); the plugin lands in `<tool home>/noctis/plugin/` with its config next to it, and `noctis doctor --host codex` checks the wiring. Uninstall: the same command with `--uninstall` / `-Uninstall` — only the plugin's own hook entries are removed.

**Honest status:** the four adapters implement each tool's documented hook, usage and resume contracts and pass the black-box lab against fakes of those tools; they have not yet been exercised against the real binaries by the author. Smoke-test steps per tool are in [docs/HOSTS.md](HOSTS.md); bug reports with the tool's version are very welcome.

## Files (`<account>/noctis/`)

`usage.json` (status-line captures, per-session model/context, burst history — a stable contract other status lines can read), `fable.json` (OAuth buckets), `state.json` (+ `.bak`), `decisions.jsonl` (the journal behind `noctis why`), `checkpoints/<sid>.md`, `guard.log`, `errors.log` (WARN/ERROR only, rotated at 512 KB).

## Tests

`node tests/monkey.js [--seed 7] [--rounds 400]` — the chaos user: random events, commands, config edits, file damage, parallel hooks and killed processes, asserting that nothing panics, output stays valid JSON, no turn is blocked without a readable reason, no session is left without a resume path, and no locks or temp files leak.

`node tests/lab.js` — 503 black-box checks against the binary (fake `claude`, `codex`, `agy`, `droid`, `copilot` and `gh`, fake usage API with clock skew and outages, compressed time). `node tests/soak.js --days 14` — multi-day simulation; `--hard 1` adds 20 kinds of chaos (corrupted files, API outages, status-line blackouts, stale locks, SIGKILLed hooks, clock jumps, giant transcripts, parallel subagents, 529 storms and a full retry-budget outage, files edited while a session waits, P0 and blocked items appearing mid-run, language switches, workflow launches, a fresh install at 99 % weekly, two runners firing for the same wait) on a git-backed project with a deliberately sloppy `TASKS.md`. Invariants: no model call above 100 %, no leaked locks, no session without a resume path, exactly one relaunch per wait, every relaunch prompt carries the queue, workspace and workflow notes it should. `node tests/hygiene.js` — refuses control bytes and invisible spaces in the source, a `bin/noctis` that is no longer the launcher script, stale `bin/SHA256SUMS` and a version that disagrees across `plugin.json`, `.claude-plugin/marketplace.json` and the binary. `go test ./...` covers the decision helpers directly, with five fuzz targets over the usage payload, the queue parser, the auto-queue extractor, the language picker and the hook output. CI runs hygiene, the lab, a hard soak, the monkey on two seeds and the fuzz targets on ubuntu, windows and macos.

## Limits

Usage data comes from Claude Code's official `statusLine` payload (`rate_limits`, Claude Code ≥ 2.1.251) and, for scoped buckets, from the undocumented OAuth usage endpoint — keep an eye on `errors.log` after Claude Code updates; `selftest` reports the tested version range. Same-session wake relies on `asyncRewake`, which is documented as observational; the scheduled relaunch covers the case where it stops working. Hooks cannot type `/clear`; compaction stays with Claude Code.
