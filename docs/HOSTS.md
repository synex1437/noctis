# Running inside other AI coding tools

Noctis is one engine (`noctis`) with a thin adapter per host. This page is the operator's view: what each adapter wires, how to check it on a real machine in five minutes, and what to send in a bug report. The contracts below come from each tool's documentation as of September 2026; the adapters are verified against fakes of those tools in `tests/lab.js` (`hosts` scenario). Once a week a CI job (`.github/workflows/real-tools.yml`) installs the real Claude Code, Codex and Copilot CLIs and checks that the main flags the plugin passes are still there and that `codex app-server` answers the handshake; it never signs in or runs a model. **No adapter has run a real session end to end yet; Antigravity and Droid are not checked at all** — treat the first run on your machine as the smoke test and report what you see. Lean compaction is Claude Code's alone: it is a Claude Code hooks module (`hooks/lean.js`), and the adapters for the other tools write command hooks only.

## Install and remove

```
./scripts/install.sh --host codex            # macOS / Linux   (or no --host: a numbered question)
.\scripts\install.ps1 -Tool codex            # Windows
./scripts/install.sh --host codex --uninstall
```

Hosts: `claude` (default), `codex`, `antigravity`, `droid`, `copilot`. The plugin tree and binary land in `<tool home>/noctis/plugin/`; config, usage snapshots, checkpoints and logs in `<tool home>/noctis/`. Tool homes: `~/.codex` (`CODEX_HOME`), `~/.gemini/antigravity-cli`, `~/.factory`, `~/.copilot` (`COPILOT_HOME`). Every `noctis` command takes `--host <id>`; `noctis doctor --host codex` shows the wiring.

Each smoke test below starts by trusting the test folder's `TASKS.md`: in that folder, run `<tool home>/noctis/plugin/bin/noctis queue trust --host <id>` (`bin\noctis.exe` on Windows). `queue.requireTrust` is on, so until the file is trusted neither the session-start hook nor the Stop hook drives it. Antigravity has no session-start hook; there the Stop hook alone drives the queue, and only a trusted file.

## OpenAI Codex CLI

- **Hooks written** to `~/.codex/hooks.json` (existing entries kept): `SessionStart` (`startup|resume|clear|compact|fork`), `SessionEnd` (3 s), `UserPromptSubmit` (may wait in place, timeout 21600 s), `PreToolUse` (matcher `Agent|spawn_agent`), `PostToolUse` (`*`, the per-call gate, 21600 s), `Stop` (60 s). Each handler carries `"statusMessage": "noctis"` — that is the marker uninstall looks for. `commandWindows` carries the Windows form of the same command. Only `UserPromptSubmit` and `PostToolUse` wait in place; a pause caught in `PreToolUse` (20 s) or `Stop` (60 s) is answered at once — a deny, or the stop with the pause message — and the scheduled runner resumes the session after the reset. Per-call `PostToolUse` hooks that pause for the same limit share one wait.
- **Trust:** Codex runs non-managed hooks only after you review them once: open Codex, run `/hooks`, trust the plugin's entries. Automation can pass `--dangerously-bypass-hook-trust` to `codex exec`, but `resume.extraArgs` drops it: a relaunch after a pause runs only the hooks you trusted.
- **Usage:** `fable.source: "codex"` — the engine spawns `codex app-server`, completes the `initialize` handshake, then sends `account/rateLimits/read`. A window of 4–6 hours becomes the 5-hour window, one of 6 days or more the weekly window; other durations are ignored. Both `rateLimits.primary|secondary` and `rateLimitsByLimitId.codex.primary|secondary` are read; the higher value per window wins. The 5-hour window has been switched off and on by OpenAI during 2026 — when it is absent, only the weekly rules fire.
- **Resume:** `codex exec resume <SESSION_ID> --skip-git-repo-check -- "<prompt>"` (headless), scheduled the same way as for Claude Code.
- **Not available:** custom status line (Codex only offers built-in items — its `/statusline` has `five-hour-limit` and `weekly-limit`), model fallback, research routing, same-session wake.
- **No error hook:** a limit the guard did not see coming is not retried.

Smoke test (5 min): install → in a folder with a 2-item `TASKS.md`, run `~/.codex/noctis/plugin/bin/noctis queue trust --host codex` → open `codex` there → `/hooks` → trust → ask for the first item → when Codex stops, it should immediately get a "[noctis] Queue continues: 1 open …" follow-up. Then `noctis status --host codex` should show usage percentages after the first hook ran (or `errors.log` says why the app-server call failed).

## Antigravity CLI (`agy`)

- **Hooks written** to `~/.gemini/config/hooks.json` under the key `noctis`: `PreToolUse` (matcher `invoke_subagent|manage_subagents`), `PreInvocation`, `PostInvocation`, `Stop`. Status line: `~/.gemini/antigravity-cli/settings.json` → `statusLine: {type: "command", command: "… statusline --host antigravity", stack_with_default: true}` — a status line you already had in that file is replaced, and uninstall removes the key.
- **Usage:** the status-line payload's `quota` map (`remaining_fraction`, `reset_time`, `reset_in_seconds` per bucket). A bucket resetting within ~5 h becomes the 5-hour window, others the weekly window; the bucket that names the current model wins. Used % = (1 − remaining) × 100.
- **Gate:** `PreInvocation` runs before every model call and can wait in place for short resets, as can `PostInvocation`; `PreToolUse` (20 s) and `Stop` (60 s) never wait in place but answer at once. `PostInvocation` answers `{"terminationBehavior": "terminate"}` when a long wait is needed, so the loop ends and the scheduled relaunch takes over. `Stop` answers `{"decision": "continue", "reason": …}` in queue mode and `{"decision": "stop"}` otherwise. A `Stop` with `terminationReason: "error"` is handled like a failed turn in Claude Code: text that mentions a quota or rate limit like a 429, an overload with the overload backoff, sign-in and billing errors not at all, anything else with a retry on the `wait.retryMinutes` steps.
- **Resume:** `agy --conversation <ID> --output-format text -p "<prompt>"`.

Smoke test: install → in a folder with `TASKS.md`, run `~/.gemini/antigravity-cli/noctis/plugin/bin/noctis queue trust --host antigravity` → `agy` there → `/hooks` lists the plugin, `/statusline` shows the plugin's line under the built-in one → after a turn, `noctis status --host antigravity` shows the windows from the last status-line payload.

## Factory Droid

- **Hooks written** to `~/.factory/hooks.json` (event-keyed, existing groups kept): `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse` (matcher `Task`), `PostToolUse`, `Stop`.
- **Usage:** none readable by scripts (`/limits` is interactive), so the limit guard is off; queue mode works; Droid has no error hook, so nothing is retried and no checkpoint is written.
- **Resume:** `droid exec --session-id <id> --auto high --output-format text "<prompt>"` (`resume.droidAuto` sets the level) — the command a relaunch would use; nothing on Droid schedules one yet.

Smoke test: install → in a folder with `TASKS.md`, run `~/.factory/noctis/plugin/bin/noctis queue trust --host droid` → `droid` there → `/hooks` → finish the first item → the Stop hook should hand over the next one.

## GitHub Copilot CLI

- **Hooks written** to `~/.copilot/hooks/noctis.json` (exec form, a file of its own): `sessionStart`, `sessionEnd`, `userPromptSubmitted`, `preToolUse`, `agentStop`, `errorOccurred`. Copilot's camelCase payloads carry no `hook_event_name`, so the engine names the event from the payload's shape (`stopReason` → `agentStop`, `errorContext` → `errorOccurred`, `toolName` → `preToolUse`, `prompt` → `userPromptSubmitted`, `reason` → `sessionEnd`, `source` or `initialPrompt` → `sessionStart`) before handling it; `sessionStart` hands over the queue directive and the checkpoint.
- **Usage:** none (monthly AI credits); limit guard off. An `errorOccurred` that Copilot marks `recoverable: false` writes a checkpoint and schedules a retry on the `wait.retryMinutes` steps (10 min for the first failure by default) through `copilot --resume=<id> -p "<prompt>"`; overload errors use the overload backoff, sign-in and billing errors are not retried. An error Copilot recovers from itself (a model call it retries, a tool that threw) is left alone. `resume.copilotAllowAllTools: true` adds `--allow-all-tools` (off by default).
- **Queue mode:** `agentStop` → `{"decision": "block", "reason": …}` for a trusted queue file. Copilot ends the turn after 8 consecutive blocks. The plugin's own stop after 4 continues without progress counts from the last prompt, so it may not trigger here: if Copilot feeds a block reason back as a new prompt, that prompt starts the count again.

Smoke test: install → in a folder with `TASKS.md`, run `~/.copilot/noctis/plugin/bin/noctis queue trust --host copilot` → `copilot` there → after the first answer the queue directive should appear as session context (`/hooks` lists the file).

## What to report

`noctis doctor --host <id>`, the tool's version (`codex --version`, `agy --version`, `droid --version`, `copilot --version`), the relevant lines of `<tool home>/noctis/errors.log` and `noctis why --host <id> --last 20`. Instead of the doctor output and the log lines you can attach `noctis report --bundle --host <id>`: it zips the doctor output, the log tails and the state files (tokens and webhook URLs are redacted; prompts and file paths are not, so read it before you attach it). Hook payload shapes change between releases; a copy of the raw hook JSON (with `NOCTIS_DEBUG_HOOKS=1` the engine writes it to `hooks-debug.log`) pins the problem down fastest.
