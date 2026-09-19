# Noctis 5.4.0 — what a full review found

5.3.0 shipped after a hardening round that fixed everything two review agents could find. This
release is what a **full review of the finished product** turned up afterwards: four independent
passes over the engine, the test suite, the install path and the security surface, with every claim
then verified against the source or a running binary. Thirty-one fixes. The important ones are here;
the complete table is `docs/PLAN.md` §5k, rows #179–#209.

Two of them were found by the review of the review — checking the findings rather than trusting
them. One reported problem turned out not to exist; two real bugs turned up in its place.

## The guard could be off while everything looked fine

If the host ever invoked the binary without its `hook` argument — a plugin manifest that stopped
honouring an args array, a hand-edited wiring — the call landed on `status`, printed a table and
exited 0. Nothing was blocked, nothing complained, and the status line kept reporting usage from its
own separate wiring, so the one visible signal said the plugin was working. Every test passed too,
because the tests pipe their payloads straight into `noctis hook`.

Now a payload on stdin is proof this was not someone typing `noctis` at a prompt: the call is served
as the hook it is, so the session stays guarded, and the output carries a notice once a day that the
wiring needs looking at. Typed at a terminal with no input it is still the status command.

Two related blind spots closed with it: `noctis --help` printed the status table instead of help, and
`hooksLookDead` — the "your hooks are not firing" warning — could not fire at all on an install whose
hooks had never worked once, because it waited for a pulse that was never coming.

## A checklist in a repository no longer drives your session unasked

`queue.auto` is on by default, and any `TASKS.md`, `tasks.md`, `.claude/TASKS.md` or `docs/TASKS.md`
in the folder you opened became the session's marching orders — with a directive that says to work
through the list without stopping to ask, and with `permissions.defaultMode: auto` behind it. Cloning
a repository or unpacking an archive was enough to place one.

A queue file now drives nothing until you say so: `noctis queue trust` (also `untrust` and `status`).
Until then the plugin says, every session, that the file is there and not driving. Checklists the
plugin wrote from your own prompt are yours already and need no agreement. `queue.requireTrust: false`
restores the old behaviour.

The notice that was supposed to prevent exactly this had a one-word bug: the self-check message
overwrote it instead of joining it, and the "already told them" stamp was written before the message
was assembled — so on the machines that had something to fix, which is where the warning mattered,
it was generated, discarded and suppressed for a week. The code's own comment forbade that.

## Lock failures are no longer silent data loss

`withFileLock` waited three seconds and then ran the work **unlocked**. Every caller reads a whole
file, changes it and writes it back, so two unlocked processes do not interleave a field — the loser
silently loses its entire change, including a wait it had just registered. It was reachable: pruning
ran inside that lock and could call `git update-ref` with a five-second timeout, which is longer than
anyone else's patience, so one expired checkpoint on a slow repository pushed every other process
past its deadline at once.

The lock now fails closed: the write is refused, an error is logged, and the git call happens outside
the lock. `registerWait` verifies the wait really reached disk, and if it did not, the session is
**not** paused — it keeps working and says why, rather than parking against a record nothing can find.

## Permissions, tokens and files

- `bypassPermissions` and `dontAsk` are refused for unattended relaunches and fall back to
  `acceptEdits`. Both READMEs said this was already true; only now it is.
- `resume.copilotAllowAllTools` defaulted to **true** in code while being absent from
  `config.default.json` — every unattended Copilot resume pre-approved every tool. Now false, and visible.
- `NOCTIS_USAGE_URL` accepted any https host while the request carried your OAuth bearer token. Only
  the real endpoint's host or localhost is accepted now.
- `NOCTIS_PLUGIN_ROOT` was never validated (a condition that was always true on the first iteration),
  and that root supplies the scripts the binary executes and the config defaults it merges.
- Everything holding prompts and replies — state, checkpoints, queues, the resume log, backups — is
  written 0600 in 0700 directories. There had been exactly one 0600 in the whole codebase.
- `killDetached` killed a stored pid with no identity check, two days after it was recorded. It now
  applies the same check the window-closing path already had.
- A binary missing from `bin/SHA256SUMS` used to be installed unchecked; it is now refused. CI
  compares the committed checksums instead of regenerating them, so a swapped binary with a matching
  manifest can no longer pass.

## Things that were invisible are now visible

- `noctis status` shows a relaunch that failed. It used to print `Waiting: none` — the same thing it
  prints when everything went perfectly.
- `noctis doctor` prints the remedy under each problem, a summary at the end, and exits 1 when
  something needs attention, so a script can gate on it. `scheduler backend: sleeper` is reported as
  the normal fallback it is, not as a fault.
- The install path now documents the failure that has no diagnostic at all: a binary macOS quarantine
  or SmartScreen will not let run, where you cannot even reach `noctis doctor` to ask why.
- Config sections merge at every depth, so setting one nested key no longer drops its siblings, and
  `noctis ensure` adds config keys that arrived with a new version instead of leaving a file that
  silently stops matching the documented defaults.

## The test suite

Six new Go unit tests, thirteen new lab checks, and three existing checks that could not fail turned
into real ones — including a test that had been asserting a copy of the production logic rather than
calling it. The hygiene test now holds every state-key access against the schema in `emptyState()`:
that check, with a negative control, catches both of this round's untyped-state bugs.

The soak now asserts that the run actually exercised what it claims to cover. It was printing those
counters and gating on none of them, so a feature could break completely and the summary would read
exactly as it does when it works.

It also had an invariant that was simply wrong: a subagent gate or a batch that ends with "switch
model and carry on" is not a pause — nothing is scheduled and no wait is written — but the soak
counted it as a stop and then reported the missing wait record as an anomaly. Reordering this
round's work shifted the simulation onto that path and it failed three runs in a row, in the same
session, which is what made it findable. A failure like that now carries the reason text and looks
for a trace in the decision journal, so the next one explains itself instead of naming a session id.

Latest run: lab 501/501, soak 7-day hard, 14-day hard and 14-day normal all at 0 breaches and
0 anomalies, monkey clean on six seeds, and the whole sweep repeated under deliberate load.

## Upgrading

Nothing to do beyond the usual update. Two behaviour changes worth knowing: a `TASKS.md` that used to
drive your sessions needs `noctis queue trust` once, and `noctis doctor` now exits 1 when it has
something to report.
