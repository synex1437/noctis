# noctis 5.0.0 — Claude Code that works through the night

First public release. The engine is a single static Go binary (standard library only) shipped for Windows, macOS and Linux on amd64 and arm64; nothing is downloaded or compiled at install time and no Node, Bun or Git Bash is needed.

## What it does

- **Stops before the usage-limit wall.** Thresholds on the 5-hour and weekly windows (presets 85/82/90 · 92/89/95 · 96/94/98), burst projection from the largest recent jump, burn-rate projection when data is stale, a blind-spot probe when there is no data near the edge. A checkpoint (last request, touched files, `git status`, todos, next queue items) is written every time.
- **Resumes on its own.** Short resets are waited out inside the hook, so the same turn simply continues with its context intact. Long waits become a scheduled task (Windows Task Scheduler with wake-from-sleep, launchd, systemd-run, or a detached sleeper) that relaunches `claude --resume` at the reset time. A 429 that kills the turn wakes the same session in place (async hook, exit 2) with the scheduled relaunch as the safety net.
- **Keeps a queue moving.** With a `TASKS.md` of `- [ ]` items the Stop hook continues to the next item without confirmation prompts, stops safely when the queue is empty, when a completion promise is seen (`<promise>DONE</promise>`) or when nothing progresses.
- **Spends quota where it matters.** Research/writing prompts are routed to a cheaper subagent (with WebSearch/WebFetch denied on the main thread); noisy commands go through a Haiku digest subagent; built-in subagents spawned without a model are pinned (`Explore → haiku`); a scoped weekly bucket that runs out (e.g. Fable) switches the default model to the fallback and reverts on reset.
- **Tells you.** One short line per event in the session (never sent to the model), a notice when the queue is finished, beep + toast + webhook (Telegram, Discord, Slack, ntfy presets, retries, circuit breaker); a status line with pace marker (`▲ ● ▼`) and threshold ETA; `noctis why` shows every decision with the usage facts behind it; `noctis report --json` is ccusage-compatible and adds `keptOffPrimary`.

## Guarantees

- Every decision is deterministic: no LLM calls, zero tokens on the hot path, ~7 ms per hook (p50).
- Concurrency-safe: PID-aware file locks, atomic writes, `.bak` recovery, single-flight API polling, multi-session maximum per window.
- Tested black-box against the real binary: 248 lab checks (fake `claude`, fake usage API with clock skew and outages, compressed time) and multi-day chaos soaks (corrupted files, API outages, status-line blackouts, stale locks, SIGKILLed hooks, clock jumps, giant transcripts, parallel subagents) with the invariant *no model call above 100 %, no leaked locks, no session without a resume path*.

## Install

```
/plugin marketplace add synex1437/noctis
/plugin install noctis
/noctis:setup
/reload-plugins
```

Or from the zip / a clone: `scripts\install.ps1 -ConfigDir "C:\Users\<you>\.claude"` (Windows) · `./scripts/install.sh --config-dir ~/.claude` (macOS/Linux). Every hook is exec form and the repo ships `bin/noctis.exe` and a `bin/noctis` launcher, so no shell is needed at any point; `noctis ensure` on `SessionStart` keeps the exact platform binary in place across marketplace updates.

## Checksums

`bin/SHA256SUMS` lists every shipped binary.

## Known limits

Usage data comes from Claude Code's `statusLine` payload (`rate_limits`, Claude Code ≥ 2.1.251) and, for scoped buckets, from the undocumented OAuth usage endpoint; `noctis selftest` reports the tested version range. Same-session wake relies on `asyncRewake`, documented as observational — the scheduled relaunch covers it if it stops working. Hooks cannot type `/clear`; compaction stays with Claude Code.
