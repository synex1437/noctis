# noctis 8.4.0

8.4.0 makes the decisions noctis takes on its own sounder and quicker. A queue item Claude keeps
working on is told from one it is stuck on by the working tree, not only by ticks and commits, and
an edit undone again still counts as stuck. The check between items is not run again on a tree it
passed on, and a quicker per-item check can stand in for the full one between items. A check Claude
cannot get to pass goes once to a stronger model before the queue is held. A wait for a limit ends
at the first reading that shows the reset, not 90 seconds after it. And `noctis why --stats` adds up
what these decisions did over the last week. It carries everything in 8.3.1
([RELEASE_NOTES_8.3.1.md](RELEASE_NOTES_8.3.1.md)).

## If you are upgrading

Nothing to do. 8.4.0 adds three settings, all under `queue`: `verifyEachCommand` (""),
`verifyFullEvery` (5) and `verifySkipUnchanged` (true). As in every upgrade, an existing
`config.json` gets them with these values and keeps the rest. Three things work differently from the
start:

- A queue in a git repository whose tree keeps changing gets more continuations before it is called
  stuck: up to `queue.maxIdleContinues` (3) more between two commits, ticks or prompts.
- A check that passed is not run again on the same working tree (`queue.verifySkipUnchanged: false`
  runs it every time).
- A failing check goes once to a stronger model before the queue is held, where `queue.escalate`
  (auto) finds one; `queue.escalate: off` keeps the hold as it was.

## A stuck item is told from one that moves

Up to 8.3.1 a stop counted as progress only when the number of open items changed or a commit
landed. An item that took many turns without a commit, such as a refactor across a dozen files,
looked like an item Claude was stuck on, and was set aside or given up.

The Stop hook now also looks at the working tree when a stop right after a continuation shows
neither: the commit `HEAD` names, `git status` and the content of the files it lists, the queue file
left out. A tree none of the last 16 such stops saw counts as progress, up to
`queue.maxIdleContinues` times until the open items change, a commit lands or you type a prompt; the
first such stop only notes the tree. A tree that comes back to a state seen before is not progress,
so a session that edits a file and undoes the edit again, stop after stop, is still stuck and goes
the way a stuck item goes: to a stronger model, then set aside. `noctis why --json` marks those
continuations with `treeMoved`.

The content of the listed files is read up to 32 MB in all; past that a file counts by its size and
modification time, and a tree that lists more than 2000 files gives no signal. Outside a git
repository nothing changes.

## Quicker checks between items

A check between items (`noctis-verify` in a trusted queue file, or `queue.verifyCommand`) ran
whenever an item had been ticked since its last pass. Two things now keep it from running when it
cannot tell anything new:

- **No run on a tree it passed on.** With `queue.verifySkipUnchanged` (on), a check whose last pass
  was on the same working tree, read as above, is not run but counts as passed; `noctis why` lists
  it as a skipped `verify-queue`. A last item ticked on a tree the full check passed on, for
  instance, ends the queue without running it again. Outside a git repository every check runs.
- **A per-item check.** A second line in the queue file, `` noctis-verify-each: `command` ``
  (trusted like the first), or `queue.verifyEachCommand` names a quicker check, such as one
  package's tests or a lint. With both, the per-item check runs after each ticked item, and the full
  one once `queue.verifyFullEvery` (5) ticked items wait for it and always before the queue counts
  as finished; 0 keeps the full check for the end. A failed check runs again in its own tier until
  it passes, with attempts, hand-ups and holds counted as before. A per-item check with no full one,
  or the same command on both lines, is the one check, and an issue `queue.github.closeOnDone`
  closes waits for a full pass.

With a 10-minute full check and a 20-second per-item one, a queue of 20 items ticked one at a time
spends about 45 minutes on checks where it spent 200: four full runs and 16 per-item ones. `noctis
queue status` and `queue trust` print both commands and where they come from (`eachCommand`,
`eachFrom` and `fullEvery` in `status --json`), and the log, a hold notice and the note after a
compaction name the command of the tier that ran last.

## A failing check goes up once

When a check is about to hold the queue (after `queue.verifyAttempts` failures in a row, or sooner
when two failures print the same output), the fix first goes once to a stronger model, one step up
from the session's as for a stuck item: `noctis:worker` on Opus for a session on Sonnet or Haiku,
`noctis:deep` for one on Opus below max. The Stop hook asks Claude to hand the fix to that subagent
with a brief that stands on its own (the command, its output verbatim, what Claude changed and tried
and why it did not work, the files involved) and to wait for its result; the check runs again at the
next stop. You see a notice:

```
⇧ `go test ./...` failed 2 time(s) in a row: the fix goes once to a subagent on Opus before the queue is held.
```

If the check still fails, the queue is held as before. A hand-up counts toward
`queue.maxEscalationsPerDay` (5) with the stuck items, is not tried where Claude Code's stop block
cap leaves no room for the run after it (`queue.verifyAttempts` at or above the cap), and `noctis
why --json` shows `escalateTo` and `escalated` on that `verify-queue`.

## Back to work at the reset

A wait for a limit slept until the reset plus `wait.resetMarginSeconds` (90 s), unless a poll every
`wait.earlyResetPollMinutes` (5) caught the window cleared early. A waiting hook and the background
watcher next to a scheduled resume now also read the usage 10, 30 and 60 seconds after the reset,
and the wait ends at the first reading that shows it: up to 80 seconds sooner, and the margin only
runs out when no reading confirms the reset. `noctis why` journals such a resume as
`reset-confirmed`, with the seconds gained as `ahead`; `early-reset` stays for a window that clears
before its reset time. A wait after a limit error (`StopFailure`), which Claude Code resumes from
itself, gets no such readings. These readings come on top of the poll every 5 minutes, and
`earlyResetPollMinutes: 0` turns both off.

A window missing from a reading also counts as reset under a stricter rule: only when that reading
was taken at or after the reset and no reading since the reset still showed the window at its old
reset time. Before, a fresh reading taken before the reset that happened to lack the window let a
wait go right at the reset time.

## What the decisions add up to

`noctis why --stats` reads the decision journal, `decisions.jsonl` and its rotated `.1`, and sums up
the last 7 days (`--days N`, up to 3650):

```
Decisions from Fri 25.09 11:14 to 14:04: 31
Pauses: 5
   planned wait: 4h 55m · 5h: 2 · weekly: 2 · Fable: 1 · burst projection: 1 · daily budget: 1
Resumed before the planned time: 4
   after a confirmed reset: 2, 2m saved · window cleared before its reset time: 1 · on fresh usage data: 1
Limit hits that stopped Claude Code: 2
Queue checks run: 7
   18m in all · passed: 2 · failed: 4 · cut short: 1 · skipped on an unchanged tree: 1 · per-item command: 1 · handed to a stronger model: 1 · held the queue: 1
Queue continuations: 5
   on a changed working tree: 1 · handed to a stronger model: 1 · items set aside: 1 · stuck queues let go: 1
```

Pauses come by window and cause with the wait they planned; resumes before the planned time by what
ended them; the queue checks with their time in all and how they ended; the continuations by what
they did, with the stops let go for a stuck queue or the daily continue limit. A daily continue
limit counts once per session and day, however many stops it let go. Observe-mode entries
(`would-…`) are counted apart, not as decisions. `--json` prints the same counts with English keys
(`days`, `from`, `to`, `decisions`, `observed`, `pauses`, `resumes`, `limitHits`, `checks`,
`continues`) for a script. Nothing is written. The text is in all 14 languages.

## Measured

On Linux with 4 CPUs:

- **The tree read.** The Stop hook reads it only on a stop right after a continuation that changed
  no item and made no commit, and before a check. In a clone of noctis (469 files), with a clean
  tree and with 5 source files changed, it took 3.9–4.3 ms, as long as `git status` alone (3.7–4.8
  ms): reading the changed files adds next to nothing. With a changed 10 MB binary it took 21.4–21.9
  ms, against 4.0–5.2 ms for `git status`. Medians of 40 runs, three times over.
- **`noctis why --stats`** took 22 ms on a full journal of 1 MB (8205 entries), the median of 40
  runs (90th percentile 26 ms); `noctis why --last 20` takes 6 ms on the same journal.

## Known limits

- **The tree is what git sees.** A change to a file git ignores (a `.env`, build output) or to
  something outside the repository is not progress and does not make a check run again. A check that
  depends on such a file can be skipped on a tree git sees as unchanged; set
  `queue.verifySkipUnchanged: false` for it.
- **Tree progress has no switch of its own.** It is bounded by `queue.maxIdleContinues`, so a queue
  that keeps editing without ticking, committing or being typed to gets at most that many
  continuations more; lower the setting to shorten it.
- **A hand-up costs the stronger model's usage.** It is one subagent turn on that model, bounded by
  `queue.maxEscalationsPerDay` together with the stuck items.
- **A confirmed reset needs the usage endpoint to show it within a minute.** If it does not, the
  wait ends at the margin, as before. The readings add up to three requests to it per wait.
- **`--stats` reaches back as far as the journal.** It keeps about 1 MB, so on a busy machine a week
  may not fit; the first line's start time says how far back it reaches. Entries from before 8.4.0
  have no `treeMoved`, `skipped`, `tier` or `reset-confirmed`, so they count only as what they are.
- The limits in [RELEASE_NOTES_8.3.1.md](RELEASE_NOTES_8.3.1.md#known-limits) still apply.

## Tests

28 new Go tests:

- **The tree:** edits between stops holding the stuck step off for as many stops as the idle limit;
  stops without edits stuck as before; a tree that returns to an earlier state, edits to the queue
  file alone, and edits outside a git repository not counting; a commit and a prompt starting the
  allowance again; the fingerprint following content, not times.
- **The checks:** the commands coming from the file, then the config; the full check due once enough
  ticks wait for it and when the queue ends; the per-item check running between items and the full
  one every few items and at the end; a failing per-item check being the one that runs again; a
  per-item check cut short named until it runs again; a tick that changes nothing else skipping the
  check that passed on the same tree; the skip needing a repository and turning off; a failed check
  running again on a tree it once passed on; the note after a compaction naming the check that ran
  last.
- **The hand-up:** a failing check going once to a stronger model before the hold, a check the
  stronger model fixes starting the count again, and a hold as before when no stronger model takes
  it.
- **The reset:** the tick landing on each check after the reset, a wait reading the usage at the
  first check after it, a reset the usage does not show yet read again at the next check, and an
  in-hook wait ending at the first check that sees it.
- **`why --stats`:** every kind of decision counted in the window, the counts in words in English
  and Turkish, `--days` and its bounds, and an empty journal.

The lab gains four scenarios (queue progress, queue checks, reset check and `why --stats`), and its
three git scenarios share one helper. The lab run with 8.3.1's binary fails the three progress
checks, and with a binary that had only the progress change, the seven queue-check checks. A missing
window counted as reset without the new rule fails the new cases of `TestWindowClearedAt` and the
test of a reset the usage does not show yet; counting every daily-limit stop, reading only
`decisions.jsonl`, or counting `would-…` entries as decisions each fails the `why --stats` tests.
