# Running inside other AI coding tools

Noctis is one engine (`noctis`) with a thin adapter per host. This page is the operator's view: what each adapter wires, how to check it on a real machine in five minutes, and what to send in a bug report. The contracts below come from each tool's documentation as of September 2026; the adapters are verified against fakes of those tools in `tests/lab.js` (`hosts` scenario) and have **not** been exercised against the real binaries by the author yet — treat the first run on your machine as the smoke test and report what you see.

## Install and remove

```
./scripts/install.sh --host codex            # macOS / Linux   (or no --host: a numbered question)
.\scripts\install.ps1 -Tool codex            # Windows
./scripts/install.sh --host codex --uninstall
```

Hosts: `claude` (default), `codex`, `antigravity`, `droid`, `copilot`. The plugin tree and binary land in `<tool home>/noctis/plugin/`; config, usage snapshots, checkpoints and logs in `<tool home>/noctis/`. Tool homes: `~/.codex` (`CODEX_HOME`), `~/.gemini/antigravity-cli`, `~/.factory`, `~/.copilot` (`COPILOT_HOME`). Every `noctis` command takes `--host <id>`; `noctis doctor --host codex` shows the wiring.

## OpenAI Codex CLI

- **Hooks written** to `~/.codex/hooks.json` (existing entries kept): `SessionStart` (`startup|resume|clear|compact|fork`), `SessionEnd` (3 s), `UserPromptSubmit` (may wait in place, timeout 21600 s), `PreToolUse` (matcher `Agent|spawn_agent`), `PostToolUse` (`*`, the per-call gate, 21600 s), `Stop` (60 s). Each handler carries `"statusMessage": "noctis"` — that is the marker uninstall looks for. `commandWindows` carries the Windows form of the same command.
- **Trust:** Codex runs non-managed hooks only after you review them once: open Codex, run `/hooks`, trust the plugin's entries. Automation can pass `--dangerously-bypass-hook-trust` to `codex exec` (add it to `resume.extraArgs` only if you know what that means).
- **Usage:** `fable.source: "codex"` — the engine spawns `codex app-server`, completes the `initialize` handshake, then sends `account/rateLimits/read` and reads `rateLimits.primary|secondary`. A window whose `windowDurationMins` is around 300 becomes the 5-hour window, one around 10080 the weekly window; other durations are ignored. The 5-hour window has been switched off and on by OpenAI during 2026 — when it is absent, only the weekly rules fire.
- **Resume:** `codex exec resume <SESSION_ID> --skip-git-repo-check "<prompt>"` (headless), scheduled the same way as for Claude Code.
- **Not available:** custom status line (Codex only offers built-in items — its `/statusline` has `five-hour-limit` and `weekly-limit`), model fallback, research routing, same-session wake.

Smoke test (5 min): install → open `codex` in a folder with a 2-item `TASKS.md` → `/hooks` → trust → ask for the first item → when Codex stops, it should immediately get a "[noctis] Queue continues: 1 open …" follow-up. Then `noctis status --host codex` should show usage percentages after the first hook ran (or `errors.log` says why the app-server call failed).

## Antigravity CLI (`agy`)

- **Hooks written** to `~/.gemini/config/hooks.json` under the key `noctis`: `PreToolUse` (matcher `invoke_subagent|manage_subagents`), `PreInvocation`, `PostInvocation`, `Stop`. Status line: `~/.gemini/antigravity-cli/settings.json` → `statusLine: {type: "command", command: "… statusline --host antigravity", stack_with_default: true}`.
- **Usage:** the status-line payload's `quota` map (`remaining_fraction`, `reset_time`, `reset_in_seconds` per bucket). A bucket resetting within ~5 h becomes the 5-hour window, others the weekly window; the bucket that names the current model wins. Used % = (1 − remaining) × 100.
- **Gate:** `PreInvocation` runs before every model call and can wait in place for short resets; `PostInvocation` answers `{"terminationBehavior": "terminate"}` when a long wait is needed, so the loop ends and the scheduled relaunch takes over. `Stop` answers `{"decision": "continue", "reason": …}` in queue mode and `{"decision": "stop"}` otherwise; a `Stop` with `terminationReason: "error"` whose text mentions a quota or rate limit is handled like a 429.
- **Resume:** `agy --conversation <ID> --output-format text -p "<prompt>"`.

Smoke test: install → `agy` in a folder with `TASKS.md` → `/hooks` lists the plugin, `/statusline` shows the plugin's line under the built-in one → after a turn, `noctis status --host antigravity` shows the windows from the last status-line payload.

## Factory Droid

- **Hooks written** to `~/.factory/hooks.json` (event-keyed, existing groups kept): `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse` (matcher `Task`), `PostToolUse`, `Stop`.
- **Usage:** none readable by scripts (`/limits` is interactive), so the limit guard is off; queue mode, checkpoints and retries work.
- **Resume:** `droid exec --session-id <id> --auto high --output-format text "<prompt>"` (`resume.droidAuto` sets the level).

Smoke test: install → `droid` in a folder with `TASKS.md` → `/hooks` → finish the first item → the Stop hook should hand over the next one.

## GitHub Copilot CLI

- **Hooks written** to `~/.copilot/hooks/noctis.json` (exec form, a file of its own): `sessionStart`, `sessionEnd`, `userPromptSubmitted`, `preToolUse`, `agentStop`, `errorOccurred`.
- **Usage:** none (monthly AI credits); limit guard off. `errorOccurred` with a rate-limit message schedules a retry in 30 min through `copilot --resume=<id> -p "<prompt>" --allow-all-tools` (`resume.copilotAllowAllTools: false` drops the flag).
- **Queue mode:** `agentStop` → `{"decision": "block", "reason": …}`; Copilot ends the turn after 8 consecutive blocks, and the plugin already gives up after 4 continues without progress.

Smoke test: install → `copilot` in a folder with `TASKS.md` → after the first answer the queue directive should appear as session context (`/hooks` lists the file).

## What to report

`noctis doctor --host <id>`, the tool's version (`codex --version`, `agy --version`, `droid --version`, `copilot --version`), the relevant lines of `<tool home>/noctis/errors.log` and `noctis why --host <id> --last 20`. Hook payload shapes change between releases; a copy of the raw hook JSON (with `NOCTIS_DEBUG_HOOKS=1` the engine writes it to `hooks-debug.log`) pins the problem down fastest.
