# noctis 5.1.0 — works for everyone, in every language

5.0.0 made the engine a single static binary. 5.1.0 makes it safe to hand to anyone: the person who installs it at 99 % of their weekly quota, the person whose `TASKS.md` is three sloppy lines per item, the person on a plan without Fable, the person who writes to Claude in Japanese — and it keeps a dynamic workflow alive across a usage-limit pause instead of restarting it from zero.

## New

- **Roles profile.** `/noctis:setup` first asks which model and effort should do which kind of work: code, research & writing, planning, noisy-output digests, file search, fallback. `synex` (code and planning on Fable 5.1 · max, research on Opus 5 · xhigh, digests and search on Haiku, fallback Opus), `balanced`, `economy` or `custom` per role. The answer lives in `roles.*`, is projected onto `models.*`, `router.subagentModels` and the plugin's own agents, and `noctis ensure` keeps the agents in step after marketplace updates. Flags: `--profile synex|balanced|economy`, `--code opus:high --research sonnet:high …`.
- **Unattended permissions.** Setup sets `permissions.defaultMode` to the widest mode that is not `bypassPermissions` — `auto` where Claude Code offers it, `acceptEdits` otherwise — so a night of work never sits behind a permission prompt. `--permissions keep` leaves your setting alone; uninstall removes exactly what setup wrote (`managedPermissionMode`).
- **14 languages.** Notices, status line, toasts and the `[noctis]` directives follow the language of the current session (en, tr, de, fr, es, pt, it, nl, pl, ru, ja, zh, ko, ar), detected deterministically from the prompt — script plus stop-words, no tokens. `locale` pins one.
- **Dynamic workflows.** Fan-out prompts and queue items ("migrate every endpoint", "review all 40 files") get a one-line suggestion to run as a workflow (ultracode) with the models and efforts from the roles profile; a new `Workflow` launch is denied inside the warn band or over a threshold; every launch is recorded, and the checkpoint, the resume prompt and the same-session wake message all say *relaunch the same script — completed agents return saved results — never start over*.
- **Sloppy queues.** `-[ ]`, `* [ ]`, `1. [ ]`, `[]` and `TODO:` all count; an item that spills over two or three lines is one item; `~~struck~~`, `(done)` and `✓` finish plain bullets; a list with no checkboxes is first rewritten into one. Priorities `(P0)`–`(P9)`, `#tags`, `(after #tag)` / `(after 3)` dependencies; references that match nothing never deadlock. 1 MB cap.
- **Installed at 99 %.** The first session says what will happen and when work resumes (once per window), the first prompt is parked until the reset (or `/noctis:pause 120`), `noctis check` returns 11 to scripts. Plans without a scoped bucket or without weekly limits never see those rules fire.
- **529/5xx backoff** (`overload.*`): exponential with jitter, 30 s → 5 min, 2-hour budget, one notice; the relaunch prompt names the outage and the attempt; a persistent outage ends with one notice and the next prompt works normally.
- **Workspace guard** (`wait.workspaceGuard`), **git snapshots** (`checkpoint.gitSnapshot`), **GitHub issues** (`noctis queue import`, `queue.github.closeOnDone`), **`noctis check`** exit-code gate, **cost estimates** in `noctis report` (`report.pricing`).

## Fixed (bug scan, docs/PLAN.md #99–#119)

- Two runners firing for the same wait launched two `claude --resume`; the session is now claimed under the state lock and only one launches.
- A fresh install at 99 % said nothing at the first `SessionStart` because no local usage data existed yet; one bounded OAuth fetch now backs the notice.
- The queue give-up dropped the stop-guard record, so a queue that later finished never produced the *queue finished* notice; counters reset, the record stays, the warning is not repeated for the same file.
- A second prompt arriving during the same wait did not update the queued prompt or replace the watchdog.
- Overload episodes never reset (a single 529 days later counted as attempt 25); episodes are gap-based and cleared by the next successful turn.
- A 429 whose `error_message` names the limit ("weekly limit", "5-hour", the scoped bucket) is now classified from the message, not from usage percentages that may say 40 %.
- The scoped-model revert (Fable ← Opus) only ran inside hooks, so a session opened the morning after the reset started on the fallback; `SessionStart` now reverts and says so.
- Near the edge, usage data is now refreshed every 15 s within two points of a threshold (30 s within four), whatever the recent burst size; a 14-day soak caught one call allowed at 97 % on 60-second-old data.
- Closing the terminal while a session was parked at a wall dropped its workflow record, so the scheduled relaunch no longer told Claude to relaunch the same run; the record now lives as long as the wait does.
- Lock recovery (PID-aware, no spin on an unremovable stale lock), scheduler serialization (`schedule.lock`), promise detection only in the last assistant message, hook-cap learning (two interruptions, cleared by a completed wait), namespaced agent types in the write policy, CJK/TR verb order, atomic writes with rename retries, no stderr from hook/statusline commands, `SessionStart source=resume` no longer skips the queue directive, `noctis on` resets the learned cap.

## Tests

`node tests/lab.js` — 363 black-box checks (fake `claude`, fake `gh`, fake usage API). `node tests/soak.js --hard 1` — marathon sessions with 20 kinds of chaos on a git-backed project with a deliberately sloppy `TASKS.md`: corrupted files, API outages, blackouts, stale locks, SIGKILLed hooks, clock jumps, giant transcripts, parallel subagents, 529 storms and a full retry-budget outage, files edited while a session waits, P0 and blocked items appearing mid-run, language switches, workflow launches, a fresh install at 99 % weekly, two runners for one wait. Invariants: no model call above 100 %, no leaked locks, no session without a resume path, exactly one relaunch per wait, every relaunch prompt carries the queue, workspace and workflow notes. Results: 7-day hard soak and 14-day normal soak, 0 breaches, 0 anomalies.

## Upgrade

`/plugin update noctis` (or replace the clone), then `/noctis:setup` once to pick a roles profile — an existing configuration is read back as a `custom` profile, nothing is switched silently — and `/reload-plugins`. Checksums in `bin/SHA256SUMS`.
