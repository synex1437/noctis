# Noctis 5.2.0 — five tools, one engine

5.2.0 takes the engine out of Claude Code: the same binary now wires itself into **OpenAI Codex CLI**, **Antigravity CLI** (Google), **Factory Droid** and **GitHub Copilot CLI**, updates itself through the marketplace, and comes with a README a first-time reader can act on in thirty seconds.

## New

- **Other AI coding tools.** `scripts/install.sh` / `install.ps1` (or `noctis setup` in a terminal) ask which tool with a numbered list (`--host codex|antigravity|droid|copilot` skips it). One adapter per tool writes that tool's hook file, translates hook JSON in and out, reads usage where the tool exposes it and resumes sessions with the tool's own command:
  - *Codex CLI* — `~/.codex/hooks.json` (`SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`, `SessionEnd`); usage through `codex app-server` (`account/rateLimits/read`, windows classified by duration so the on-and-off 5-hour window is handled); resume with `codex exec resume <id> "<prompt>"`. Full pause-before-the-wall guard.
  - *Antigravity CLI* — `~/.gemini/config/hooks.json` (`PreToolUse`, `PreInvocation`, `PostInvocation`, `Stop`) plus a status-line command; usage from the status-line `quota` payload; `PostInvocation` terminates the loop at the threshold, `Stop` answers `continue` in queue mode; resume with `agy --conversation <id> -p`. Full guard.
  - *Factory Droid* and *GitHub Copilot CLI* — queue mode, checkpoints and error retries (no script-readable usage windows on either); resume with `droid exec --session-id` / `copilot --resume=<id> -p`.
  - Each `noctis` command takes `--host`; `noctis doctor --host codex` checks the wiring; uninstall removes only the plugin's own hook entries. Documented contracts, lab-tested against fakes of the tools — see `docs/HOSTS.md` for the five-minute smoke test per tool and what to report.
- **Updates for everyone.** Setup runs `claude plugin marketplace update noctis --auto-update` (third-party marketplaces have auto-update off by default), and once a day at session start the plugin looks up the published version and shows one line when a newer one exists (`update.check: false` turns it off, `--updates keep` leaves the marketplace setting alone).
- **README for first-time readers.** Who it is for and the four install lines on screen one; a *What it changes on your machine — and how to undo it* table (permissions, model, status line, network, queue mode); *The first five minutes*; a three-line `TASKS.md`; the reference material moved to `docs/REFERENCE.md`. Turkish README restructured the same way.
- **Long prompts become queues.** A prompt with several pieces of work (a list of ≥ 3 items, ≥ 4 lines opening with a work verb, or a paragraph of ≥ 4 such sentences tied by "then/finally") is written to a checklist in Noctis's own folder and driven like `TASKS.md`; a one-line `☰ N-step job detected` tells the person, and any manual prompt while the job is open hands control back. Short prompts, questions, bug reports with pasted output, descriptive context and single tasks are untouched (`queue.auto: false` disables it).
- **Restart banner.** When marketplace auto-update has downloaded a newer version, the next session start says so with the reload hint and a desktop toast, once per version.
- **`noctis` profile** (was `synex`; the old name still works): code and planning on Fable 5.1 · max, research on Opus 5 · xhigh, digests and file search on Haiku · high, fallback Opus · max.
- **Coexistence notes.** `noctis doctor` lists other hooks on the same events and neighbouring plugins (loops, auto-resume tools, status lines); nothing is changed.
- **Queue mode says so.** The first session in a folder with an open queue shows `☰ Queue mode: TASKS.md has N open item(s) — Claude works through them without asking…`; `TODO.md` is no longer a queue file by default (`TASKS.md`, `tasks.md`, `.claude/TASKS.md`, `docs/TASKS.md` are).
- **Uninstall restores the model.** Setup remembers the `model` it wrote (`managedModel`); uninstall puts the previous value back or removes the key.
- `noctis version`; `NOCTIS_DEBUG_HOOKS=1` logs raw hook payloads to `hooks-debug.log`.

## Fixed / clarified

- The `[noctis]` instructions given to Claude are English in every language catalog (two Turkish translations of them were removed; user-facing notices stay localized).
- Docs no longer claim launchd/systemd wake a sleeping machine — only the Windows scheduled task does.
- `install.ps1` error text in English; `-Tool` / `-Roles` parameters (PowerShell reserves `$Host` and `$Profile`).

## Tests

`node tests/lab.js` — 426 black-box checks, now with fake `codex` (including a JSONL app-server), `agy`, `droid` and `copilot`. Soaks unchanged: 20 chaos kinds, 0 breaches, 0 anomalies.

## Upgrade

`/plugin update noctis` (auto-update brings it in after a session start once enabled), then `/noctis:setup` once so setup can enable marketplace auto-update, and `/reload-plugins`.
