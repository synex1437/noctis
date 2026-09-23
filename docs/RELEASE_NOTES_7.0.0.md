# noctis 7.0.0

7.0 started as a re-tuning of the role profiles for Opus 5.5 and turned into the widest pass the
plugin has had. The tree was then read eight times over, each time for one thing — correctness,
robustness, security, speed, footprint, the limit logic, the user-facing surfaces and the docs — and
every finding was fixed, put in front of a second reader who had not written the fix, and kept only
when that reader could not break it. What that reader broke twice was reverted and is listed at the
end. The high-severity findings those passes deferred were then swept in the same way.

Thirty-six product bugs came out of it. The worst ones share a shape the earlier notes will
recognise: a surface that looked wired and did nothing.

## The ones that matter

**On GitHub Copilot CLI, every hook was a no-op.** Copilot's camelCase payloads carry no
`hook_event_name`, and the engine named its events by that field. So on Copilot nothing ever
happened: no queue hand-over, no checkpoint, no retry after a rate-limit error. The engine now names
the event from the payload's shape before handling it. While tracing that against Copilot CLI
1.0.88, a second gap showed: `errorOccurred` fires for errors Copilot recovers from on its own (a
model call it retries, a tool that threw), and each one used to schedule a relaunch ten minutes
later. Only an error Copilot marks `recoverable: false` counts now.

**On Claude Code, every failed turn took the unknown path.** The `StopFailure` handler read
`error_type` and `error_message`; Claude Code sends `error`, `error_details` and
`last_assistant_message`. A 429 was not seen as a rate limit, a `model_not_found` never healed the
model, and every failure ran the generic retry. Both spellings are read now.

**On Windows, the relaunch after a Fable switch or a 429 retry was registered and then
unregistered.** Those two paths stored their wait after registering the Task Scheduler task, and a
wait with no recorded schedule unregistered "whatever might be there" — the task they had just
made. So the relaunch on the fallback model, and the retry after a 429/529 when the in-place wake
did not take, never happened. A wait with no recorded schedule now unregisters nothing, which also
removes the PowerShell process every session's first pause started to delete a task that did not
exist.

**On Windows under Git Bash, the plugin's own commands ran the Linux binary.** Claude Code's Bash
tool on Windows is Git Bash; `bin/noctis` mapped every `uname` other than Darwin to Linux, so
`/noctis:setup`, `:status`, `:pause` and `:resume` exec'd an ELF and died with "Exec format error".
The launcher now sends MINGW, MSYS and Cygwin to the Windows binary, refuses an unsupported CPU by
name instead of guessing amd64, and finds the plugin root with `CDPATH` exported. `install.sh` run
from Git Bash stops and points to `install.ps1`. The contract suite drives the launcher with a fake
`uname` for eight platforms.

**A `TASKS.md` you never trusted still drove your turns.** 5.4 introduced queue trust, but the check
covered the session start and the checkpoint only: the Stop hook continued an untrusted list,
`queue.github.closeOnDone` closed its issues, and the unattended relaunch prompt carried its items.
Until you trust a file it now drives nothing.

**On macOS, every launchd-fired runner booted out its own job before resuming**, so launchd
SIGTERMed it before it resumed anything. Every schedule now has its own label
(`com.synex.noctis.<hash>.<epoch>`), and a runner can reschedule itself.

**Parallel pauses of one session cancelled each other.** The check that joins a second pause to the
stored wait was not atomic, and a hook that woke deleted whatever wait was stored — another hook's.
Several Agent calls fanned out at once, or the per-call `PostToolUse` hooks on Codex and Droid,
all ran on past the limit. Pauses for the same window and reset now share one wait, and a hook
clears only the wait it stored itself.

**A wait whose runner vanished was never re-armed.** A reboot, a logout, an OOM kill, a container
restart or a launchd date that passed while the Mac was off left the session parked with nothing to
resume it. Such a wait is now re-armed at the next hook or status-line tick (a sleeper pid taken
over by another program, and an overdue Windows task, at the next session start), at most three
times in a row, never within 30 s of the pause and never for a manual pause.

**A relaunch always exported `CLAUDE_CONFIG_DIR`.** A default install's resumed session therefore
read the wrong global config (onboarding, project trust, MCP servers) and, on macOS, the wrong
keychain item. It is set only when the paused session had it.

**The Codex usage probe hung** hooks, the sleeper and the runner whenever `codex` was the npm, pnpm
or bun wrapper: the orphaned `app-server` deadlocked `Cmd.Wait`. It now gets stdin EOF and one
second to exit before its process tree is killed.

## Limits, waits and wakes

- **Early release depends on why the session paused.** Burst and projection pauses ended at the first
  fresh reading, and a blind ("no data") pause never ended before the announced reset. A pause at a
  threshold, burst, compaction, ceiling or budget now ends early only when the window really resets
  (the paused window at least 10 points below both its pause point and its level at the pause). A
  blind or stale-projection pause ends as soon as a fresh reading of that window shows room; a
  relaunch for that reason happens only if the session has been quiet since it paused. New journal
  action `data-back`; every window in `usage.json` carries `at`, the time it was last reported.
- **Subagents and workflow agents meet the limits.** Their tool calls were never checked against the
  pause threshold or the paid-credit ceiling. Past the pause point a subagent's `WebSearch`,
  `WebFetch`, `Agent`, `Task`, `Workflow` or `Write` is denied with an instruction to report what it
  has; its first tool batch past that point warns, the next ends it; at the ceiling it is ended at
  once. Every agent cut short is named in the checkpoint and on resume so the session redoes that
  part. Journal actions `deny-subagent-tool`, `warn-subagent`, `stop-subagent`.
- **While the Fable quota is out, a subagent pinned to Fable, or spawned with an explicit Fable model,
  runs on `models.fallback`** (`subagent-fallback` in `noctis why`), and the fan-out advice names the
  fallback. Before, only the main session switched.
- **A second session hitting the scoped cap overwrote the switch record**, so the revert forgot the
  effort it had replaced.
- **`claude --agent <name>` was treated as a subagent**, which switched off the mid-turn pause, the
  credit ceiling, the Agent and Workflow gates and routing for that whole session. Only `agent_id`
  marks a subagent now.
- **The plugin's own `/noctis:pause`, `:status`, `:resume` and `:setup` were blocked or held at the
  very threshold they are meant to override**, then replayed as the relaunch prompt. They pass the
  pause point again (not the 100 % ceiling); `noctis why` shows `control-prompt`, `control-batch` and
  `block-control-prompt`.
- **In-hook waits ignored the hook timeouts noctis itself wires** for Codex, Droid, Antigravity and
  Copilot. A wait in place is now capped at that timeout less 30 s, and a hook wired with under 90 s
  (the 20-second `PreToolUse` and the 60-second `Stop`) answers at once and leaves the wait to its
  runner.
- **The same-session wake after an overload backoff re-checks the limits**, and a session at or past
  a pause point is left to the scheduled runner instead of being woken into the wall.
- **After an in-hook wait, `PostToolBatch`, Agent spawns and `Stop` dropped the "working tree
  changed" note for Claude**, and `Stop` dropped the "waited …, continuing" line for you. All paths
  show them; the note states as a fact that files read or edited before the pause may differ from
  disk, instead of ordering a re-read.
- **`killDetached` SIGKILLed a process it could not identify** — no `ps`, busybox `ps`, a `ps`
  timeout — which after a container restart is an unrelated process. Linux now reads
  `/proc/<pid>/comm` (works without procps, cannot time out), and on every platform a recorded pid
  that cannot be named is left alone with a warning.
- **A window-relaunched session that paused again was never resumed**: its first runner kept the
  hand-off for up to seven days while it watched the old window. The next runner takes the session
  over from a runner that only watches a window opened before this pause.
- **Relaunches inherited Claude Code's session markers** (`CLAUDECODE`, `CLAUDE_CODE_SESSION_ID`,
  `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_ENTRYPOINT` and the rest), so a relaunched interactive
  session stopped saving its transcript. Every relaunch path drops them.
- **Windows: `runner.cmd` embedded the UTF-8 path, and `cmd.exe` reads batch files in the OEM code
  page**, so a scheduled resume under a profile such as Çağrı or Müller died with "the system cannot
  find the path specified". The launcher switches its console to UTF-8 first.
- **Windows: `install.ps1` and `noctis setup` never asked which tool or which roles in a real
  console**, because the terminal check compared stdin with `NUL`. It asks Windows whether stdin is a
  console now.

## Queue and GitHub

- Trust now gates the Stop hook, the checkpoint's queue items, the relaunch prompt and
  close-on-done (above).
- **`closeOnDone` closed every `#N` mentioned in a ticked item**, so `#12 Follow-up to #45` closed
  #45, and **issues imported with `--repo` were closed in the checkout's own repository.** An issue
  is closed once every item that *starts with* its reference is ticked, and only after it was seen
  open. `queue import --repo owner/name` writes `owner/name#123`, and such items are closed with
  `--repo`; a reference naming another host is contacted only when it is github.com or `GH_HOST`.

## Setup, install and the command line

- **An unreadable `config.json` or `settings.json` never stopped a write.** `ensure` overwrote a
  `config.json` it could not read with the defaults; `setup` and `install` rewrote `settings.json`
  with the shipped defaults and said "setup complete"; `--uninstall` deleted the plugin before it
  found `settings.json` unreadable. Each now stops with exit 1 and nothing changed; uninstall
  restores the settings first and removes the copy afterwards.
- **`setup` and `install` acted on input they did not understand.** `--flag=value`, a misspelled
  flag, a stray word, an invalid `--permissions` or `--updates` value and an unknown `--host` or
  `NOCTIS_HOST` all fell back to the defaults and rewrote the default account. They exit 2 with
  nothing written (with a did-you-mean); `--flag=value` is read; `--account` and every `--config-dir`
  name the accounts configured; `-h` prints the help.
- **`cancel` and `off` reported success for what they did not do.** The 8-character id `status`
  shows cancelled nothing; `2h` became 60 minutes, `2 hours` 2 minutes, `nan` and `inf` whatever
  they parsed to. `cancel` takes a unique start of the id (4 characters or more) and refuses an
  ambiguous one; `off` takes `90m`, `2h`, `1h30m`, `2 hours`, `1 day`, up to 7 days, and refuses `0`,
  negatives and words with exit 2.
- **Re-running setup with another profile kept a default model setup itself had written** when it
  was Fable or Opus, so `noctis` to `economy` stayed on Fable.
- **Profiles promised efforts on Haiku 4.5**, which has no effort levels; setup now says so.
- **`claude-opus-5-5` was priced as Opus 5** in the cost reports ($5/$25, cache reads at 2.5×).
- **A `claude` or `codex` planted in the working directory, or reachable only through a relative
  `PATH` entry such as `.` or `node_modules/.bin`, was run.** The lookup now searches only absolute
  `PATH` directories outside the working directory plus Claude's fixed install locations, and the
  version, marketplace-update and `codex app-server` probes run from the guard directory so an npm
  shim cannot pick `node` up from the project.

## Router

- **Links to code were routed as research.** A pull request, issue, commit, compare view, file or CI
  run on GitHub, GitLab or Bitbucket, and any local server (`localhost`, `127.x`, `0.0.0.0`, `[::1]`,
  any host with a port), now count as code and stay on the main model; `lite:` still forces the
  agent.
- **The coding-session check read only the last 64 KB of the transcript**, so one large tool result
  made an active coding session look cold and sent its next prompt to the lite agent. It scans back
  until it meets an assistant entry older than 45 minutes, retrying with a 1 MiB tail, and counts an
  undecided or unreadable transcript as coding.

## Hosts

- **macOS: the usage and Fable fetch read the default account's keychain item for every
  `CLAUDE_CONFIG_DIR` account.** Each account's own item is read now (`Claude Code-credentials-` plus
  the first 8 hex digits of the SHA-256 of `CLAUDE_CONFIG_DIR` as set, honouring
  `CLAUDE_SECURESTORAGE_CONFIG_DIR`), never falling back to the default; expect one "allow access"
  prompt per account.
- Copilot and Codex: above. Antigravity: `PreToolUse` (20 s) and `Stop` (60 s) answer at once and
  leave a long wait to the runner.

## Profiles for Opus 5.5

Rebuilt from Anthropic's published Opus 5.5 numbers (per-effort charts, cost per task):

| profile | code | research | planning | digest / explore |
| --- | --- | --- | --- | --- |
| `noctis` | opus · max | opus · xhigh | opus | haiku |
| `balanced` | opus · high | opus · medium | opus | haiku |
| `economy` | opus · low | sonnet · high | opus | haiku |

Opus 5.5 at max beats Fable 5.1 at max on Terminal-Bench 4.0 (64.8 vs 55.8), FrontierCode (54.4 vs
50.3) and CursorBench (57.8 vs 51.8) at a lower cost per task; at low it is the cheapest per solved
coding task of every model charted; research at low collapses (WANDR 31.2), so `economy` researches
on Sonnet 5. The fallback role is the code model in every shipped profile. noctis still watches the
one per-model weekly cap, Fable's: move up to Fable by hand and it switches you to the fallback when
that cap runs out and back when it resets. A configuration still holding an earlier shipped profile
is named in the self-check, `noctis status` and `noctis doctor` with the one command that adopts
the new one; nothing is switched silently.

## What a hook costs now

- **Edit, MultiEdit and NotebookEdit no longer start noctis.** The `PreToolUse` matcher listed them,
  and every such call waited on a synchronous noctis process that returned without doing anything
  (neither shipped agent can call those tools; their frontmatter keeps them off). Per call: 1 → 0
  processes — each was 1 `execve`, 323 syscalls and 5 file opens at a p50 of 4.0 ms; a 250-call
  session's hook wait went from 1037 to 104 ms.
- **Session start no longer reads and hashes the 7.9 MB binary that `bin/noctis` already hard-links**:
  15.8 MB → 21 KB read, 39 → 4 ms per session start. A binary with a new inode (marketplace update,
  reinstall, local build) still goes through the checksum.
- Measured on Linux with a Node process starting each call: a `UserPromptSubmit` hook 5.2 ms, `Stop`
  4.0 ms, a status-line refresh 4.7 ms when the binary is called directly; 11.6, 10.6 and 11.4 ms
  through the `bin/noctis` sh launcher that macOS and Linux marketplace installs run. Replacing that
  launcher in marketplace installs (status line 12.6 → 6.9 ms) was tried and reverted after two
  failed reviews; it is on the list for the next pass.

## Tests and CI

- The lab grew from 525 to 582 checks and the contract suite from 109 to 134; the fixes above are pinned
  by tests in the lab and in `go test`. Coverage went from 80.5 % to 86.6 % of statements (`go test` alone
  64.6 %, the lab alone 73.4 %).
- Each lab checksums its own snapshot of the tree, so a local rebuild installs on the first run and
  no suite rewrites the repository's `bin/SHA256SUMS`; a suite that crashes after its mock server
  started exits 1 at once and takes the mock with it; every CI job that runs the suites has a
  timeout, and `tests/hygiene.js` fails a workflow job without one, or a `package.json` next to a
  lockfile at the plugin root (Claude Code would run an install in every cached copy).
- The end-to-end clock-skew test measures a request that starts and ends in the same wall-clock
  second, so the `Date` header cannot land one second later under load.
- This release's run on Linux: lab 582/582 · contract 134/134 · chaos 64/64 · scheduler 24/24 ·
  `go test`, `gofmt` and `go vet` clean for linux, windows and darwin. Torrent: 600 jobs and 3 268
  hooks through three accounts, 499 sessions stopped before the wall, all 499 parked, none let past
  a threshold, median hook 10.4 ms (p95 59.2 ms). Two-day hard soak: 0 breaches, 0 anomalies, hook
  p50 8 ms, p95 18 ms.

## Documentation

README.md and README.tr.md, docs/REFERENCE.md, docs/HOSTS.md, docs/TESTING.md and CONTRIBUTING.md
were rewritten against the code, then checked claim by claim by a reader who had not written them;
the visuals were redrawn from the current defaults and real output. Where a document and the code
disagreed, the code won; where the code was wrong, it is in the list above. The plugin description
and keywords say what the plugin does.

## If you are upgrading

- Run `noctis queue trust` once in any folder where you relied on Stop continuation, issue closing
  or the relaunch prompt from a `TASKS.md` you had not trusted.
- `noctis off 0` and negative values are refused with exit 2 instead of pausing for one minute;
  `setup` and `install` exit 2 on an unknown flag, a stray word or an invalid value instead of
  running with defaults; `install.ps1 -Tool 'Codex CLI'` exits 2 and lists the ids to pass.
- The `PreToolUse` matcher is `WebSearch|WebFetch|Agent|Task|Workflow|Write`. A hand-made agent
  named in `router.agent` or `router.digestAgent` must keep `Edit`, `MultiEdit` and `NotebookEdit`
  out of its tools itself; `noctis doctor` and the startup self-check name one that does not.
- The launchd label is `com.synex.noctis.<hash>.<epoch>`, one per scheduled resume. Launch files are
  `launches/<sid>.<runner pid>.*`.
- `usage.json` windows carry `at` (additive). New `noctis why` actions: `data-back`, `join-wait`,
  `wait-replaced`, `deny-subagent-tool`, `warn-subagent`, `stop-subagent`, `subagent-fallback`,
  `control-prompt`, `control-batch`, `block-control-prompt`.
- A configuration on an earlier shipped profile keeps working and is named with the command that
  adopts the Opus 5.5 one.

## Still open, deliberately

- **Linux with systemd.** All schedules of a session share one unit, and scheduling stops that unit's
  service first, so a runner that has to schedule itself again stops itself before the new timer
  exists, and a session it relaunched headless is stopped with it at that session's next pause. The
  wait is kept and re-armed at the next prompt, status-line refresh or session start. The fix (a
  unit per schedule, only timers stopped) was reverted after failing independent review twice.
  `NOCTIS_NO_TASKS=1` avoids systemd meanwhile.
- Nine relaunch-runner findings the sweep did not reach: the runner drops a `StopFailure` wait whose
  reset is the failure second; `closePreviousLaunch` kills whatever now owns a recorded pid;
  terminals that stay in the foreground (xterm, konsole, kitty, alacritty, wezterm) are killed after
  15 s with the session inside; a status-line tick without usable `rate_limits` stamps the old
  windows fresh; Windows quotes an already-quoted `cmd.exe` line twice; a relaunch that dies at once
  is recorded as a successful resume; the systemd self-stop above; a finished launchd job stays
  loaded until logout and fires again a year later; `xfce4-terminal -e` is handed a form it does
  not accept.
- Six fixes reverted after two failed reviews: the `StopFailure` hand-off under `claude -p` (whose
  async hooks Claude Code ends at teardown); setup's ownership of effort and permission mode across
  re-runs; English prompts detected as Polish, Portuguese or Spanish; issue titles that inherit a
  trusted queue's trust on import; an OAuth 200 of unknown shape wiping the cached windows; and the
  router's word signals sending the user's own code work to the lite agent.
- Also known: `resume.extraArgs` can re-enable `bypassPermissions` on an unattended relaunch; a resume
  prompt that starts with `-` is read as an option by `claude`, `codex` and `droid`; on Copilot the
  block reason is re-sent as a prompt, so the plugin's own "4 continues without progress" stop does
  not trigger there and Copilot's 8-block cap ends the loop; the checkpoint message says
  `claude --resume` on every host. None of the four adapters has run a real session end to end.
- Lean compaction (pruning a session's context to what the next turn needs, driven by the context
  fullness Claude Code reports) is planned for 7.1, not this release.

## Repository settings applied by hand

GitHub does not read these from the tree. Description (278 characters):

```
Claude Code plugin for the 5-hour and weekly usage limits: pauses just before a limit, resumes the same session after the reset (or relaunches it days later) and works through a TASKS.md queue overnight. One Go binary; adapters for Codex CLI, Copilot CLI, Droid and Antigravity.
```

Topics (20, GitHub's maximum): `claude-code claude-code-plugin claude-code-hooks
claude-code-statusline claude-code-marketplace usage-limit rate-limit auto-resume auto-continue
5-hour-limit weekly-limit overnight task-queue statusline codex-cli copilot-cli antigravity-cli
factory-droid agentic-coding anthropic`.

Social preview: Settings → General → Social preview → upload `docs/social-preview.png` from this
release (1280 × 640).
