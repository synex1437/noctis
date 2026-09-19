# Noctis 5.3.0

The release that assumes the world is unreliable: providers reset quotas whenever they like,
terminals get closed, processes get killed, files get half-written — and the session still has to
come back and finish the work.

## Early resets — every tool, every kind of wait

Providers sometimes clear a window ahead of its announced reset (a new model launch, a quota
change). Until now a paused session would sit until the old reset time.

- A wait that sleeps inside a hook refreshes usage every 5 minutes (`wait.earlyResetPollMinutes`)
  and continues the moment the window is clear: `⚡ 5h limit reset ahead of schedule after 54 min
  of waiting — continuing where you stopped.`
- A long wait parked for the scheduler gets a light watcher next to it; the scheduler still owns
  the relaunch, the watcher only brings it forward.
- Fresh usage data from **any** window (another Claude Code session's status line) resumes a parked
  session immediately — no polling needed for that path.
- Hosts without a usage API (Droid, Copilot) retry on a shorter ladder — 10, 20, 30, 45, 60 minutes
  (`wait.retryMinutes`) instead of a flat 30 — so an early reset is noticed within minutes.
- `noctis cancel` now ends a wait that is sleeping inside a hook; the session continues at once.

## Relaunches you can see, and no window pile-up

- **Windows Terminal tab** when it is available (`wt -w 0 new-tab`), a console window otherwise.
- **macOS**: a Terminal window (osascript). **Linux**: the desktop terminal when one is reachable
  (`x-terminal-emulator`, GNOME Terminal, Konsole, kitty, Alacritty, WezTerm), otherwise headless as
  before. `resume.terminal` takes `auto`, `none`, or a command template with `{script}`.
- The engine waits for the claude process it started, so the wait is cleared exactly when that
  session ends, and **closes the window it opened last time** before opening a new one
  (`resume.closePrevious`). Windows you opened yourself are never touched.
- The old window's status line says where the session went: `↪ continues in another window — this
  one can be closed`.
- A relaunch that could not start now says so instead of failing quietly.

## Faster hooks

- **Lazy regexes and message catalogs**: the engine compiled ~55 patterns and two large catalogs at
  start-up on every hook. They now build on first use. Process start-up went from ~8 ms to ~0.6 ms.
- **Quiet fast path**: after a decision far from every threshold, the next `PostToolBatch` hooks end
  on two small reads and write nothing at all. The marker expires after 2 minutes and is void the
  moment usage, reset times, the model, context size, the config file or the state file changes —
  so a threshold crossing is never hidden by it.
- **No-op state writes are skipped**, which is most hooks.
- Measured: hook p50 9 ms → **3–4 ms**, and a quiet hook touches no file at all.

## Repair: the engine fixes its own mess

- A **corrupt `state.json` or `usage.json`** is restored from its backup the moment it is read (and
  cleared when there is no usable backup) instead of waiting for the next write.
- A **hand-off whose relaunch process is gone** is released, so the session stops being marked
  "continuing elsewhere" and its prompts are no longer blocked.
- A **parked wait that lost its runner** (a hook killed between registering the wait and scheduling
  the relaunch) is rescheduled; the window is also closed at the source — the runner is scheduled
  before the wait becomes visible.
- **Leftover `.tmp` staging files** and lock files in any format are swept.

## Trust

- `noctis report --bundle` writes one zip for a bug report — logs, decision journal, state, config,
  the doctor's view, the hook payload log when debugging was on — with the home path, tokens and
  webhook URLs redacted.
- The engine **verifies a shipped binary against `bin/SHA256SUMS`** before placing it; a mismatch is
  refused with a clear message.
- Releases are built in CI from source, checked against the committed checksums and published with
  **build provenance attestation**.
- **Fuzz tests** for hook-input normalisation, the queue parser, auto-queue detection, language
  detection and the usage payload run in CI.
- **A weekly workflow installs the real CLIs** (Claude Code, Codex, Copilot) and checks the flags and
  handshakes the adapters depend on, so a contract change is noticed in days, not in issues.
- **A monkey test** (`node tests/monkey.js --seed 7 --rounds 400`) drives the plugin with random
  events, commands, config edits, file damage, parallel hooks and killed processes, and asserts the
  invariants that matter: no panic, valid JSON output, no blocked turn without a readable reason, no
  stranded session, no leaked locks or temp files.

## Tests

`node tests/lab.js` — 479 black-box checks. `node tests/soak.js --days 14 [--hard 1]` — multi-day
simulation with 20 kinds of chaos. `node tests/monkey.js` — the chaos user. `node tests/hygiene.js` —
source, launcher, checksum and version hygiene. `go test ./...` plus five fuzz targets.

The last pass before release was a hardening round: two review agents were pointed at the engine and
at the test suite with the brief of breaking them, and everything they found is fixed and pinned by a
test — a usage percentage of `NaN` or `150` no longer silently disables the guard, a relaunch can no
longer close a process that merely shares a name prefix with the session it started, the quiet fast
path now hashes `fable.json` too so nothing written by another window can hide behind it, a `state.json`
that is valid JSON but not an object is recovered from the backup instead of dropping every waiting
session, an early reset needs real evidence rather than a window missing from an empty payload, and
`report --bundle` no longer leaks a webhook URL through an error string. Six lab checks that could
never fail were turned into real assertions, and `tests/hygiene.js` exists because an invisible control
byte had made it into a regex.

One more came out of the chaos user on fresh seeds, after everything else was green: a wait that a
hook is already sleeping on — the one it is about to wake in place — was recorded as unattended, so
the scheduled relaunch and the early-reset trigger both considered it theirs to resume. Between them
they could bring the same work back twice, once in a new window and once in place. A hook parked on a
wait is now visible as such to everything that decides whether to start a second resume.

## Upgrading

Nothing to do: marketplace auto-update brings it in, and the next session says so. New config keys
(`wait.earlyResetPollMinutes`, `wait.retryMinutes`, `resume.terminal`, `resume.closePrevious`) get
their defaults on first run; `noctis doctor` shows the effective values.
