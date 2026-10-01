# noctis — Go engine

`noctis` is the plugin's whole runtime: every hook, the status line, the resume runner, the scheduler, the installer and the CLI commands live in this single static binary (standard library only, `CGO_ENABLED=0`).

Build for the current platform, from the repository root (into `bin/<os>-<arch>/`, or `bin/darwin/` on macOS, where the launcher `bin/noctis` looks):

```sh
node scripts/build.js --host
```

All six shipped targets, the way CI builds them (`CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" ./cmd/noctis` in this folder for each target; the two macOS builds become one universal binary, `bin/darwin/noctis`; `bin/noctis.exe`, the shipped fallback so exec-form hooks work before `noctis ensure` runs, is the windows-amd64 build; `bin/SHA256SUMS` is regenerated):

```sh
node scripts/build.js
```

Layout of `cmd/noctis`:

| File | Responsibility |
|---|---|
| `main.go` | command table, argument parsing, panic-to-errors.log |
| `store.go` | paths, config loading, state/usage files, PID-aware file locks, atomic JSON writes, logging |
| `usage.go` | usage sources (statusLine capture, OAuth usage endpoint), burst/slope/projection, threshold evaluation, clock-skew correction, stale-lock sweeping |
| `engine.go` | decisions: checkpoints, waits, in-hook waiting, scoped-model switch, daily budget, notifications |
| `hooks.go` | hook handlers (SessionStart/End, UserPromptSubmit, PreToolUse, PostToolUse, PostToolBatch, Stop, StopFailure, Notification, PostModelSwitch, Task*, PreCompact) |
| `router.go` | deterministic research router (RE2-safe Unicode word boundaries), learned misroute signals |
| `runtime.go` | status line, pace marker, hooks self-heal, resume runner, `claude` launching, sleeper |
| `host.go` | the other AI coding tools: capability table, hook-file wiring, input/output translation, Codex rate limits, Antigravity quota, update check, coexistence notes |
| `autoqueue.go` | long prompts turned into checklists (`<account>/noctis/queues/`) |
| `roles.go` | roles profiles (Code / Search / Balanced / SYNEX / custom) projected onto models, router and agent files |
| `lang.go` | deterministic language detection, per-session locale, the thirteen-language catalog subset |
| `workflow.go` | dynamic-workflow detection, suggestion and relaunch notes |
| `github.go` | `queue import` from GitHub issues, closing issues when items are ticked |
| `pricing.go` | price table for `report --cost` |
| `scheduler.go` | scheduled relaunch: Task Scheduler / launchd / systemd-run / detached sleeper |
| `journal.go` | decision journal (`decisions.jsonl`), observe mode, `noctis why` |
| `webhook.go` | webhook delivery with presets, retries and a circuit breaker |
| `commands.go` | `status`, `doctor`, `selftest`, `report` (text / ccusage-compatible JSON / HTML), `cancel`, `off`/`on`, `model`, `checkpoint` |
| `install.go` | `install` (clone copy under `<account>/skills/`), `setup` (marketplace), `ensure` (SessionStart bootstrap), presets |
| `messages.go` | en/tr message catalog (`T()`); directives sent to Claude stay English; the other languages live in `lang.go` |
| `alive_*.go`, `detach_*.go` | per-OS process liveness and detached process spawning |

Tests are black-box and live in `../tests` (Node drives the binary): `node tests/lab.js`, `node tests/soak.js --days 7`, `node tests/soak.js --days 3 --hard 1`.
