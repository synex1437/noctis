# noctis 7.4.0

7.4.0 keeps the file every hook reads small. A checkpoint that was used, because its session went
on or its note went to a new session, moves out of `state.json` into `used-checkpoints.json` beside
it, which hooks do not read; `state.json` is written without indentation; and a `Read` that asks
for permission looks at the checkpoint records only when the file is in `checkpoints/`. In the
264 KB state the 7.3.2 hooks were measured with, 392 of the 448 checkpoints were used: with 7.4.0
that `state.json` shrinks to 85 KB, and a `UserPromptSubmit` hook takes about 8 ms where 7.3.2 took
10. 7.4.0 carries everything in 7.3.2 ([RELEASE_NOTES_7.3.2.md](RELEASE_NOTES_7.3.2.md)).

## If you are upgrading

From 7.3.0, the upgrade notes of 7.3.1 and 7.3.2 apply too
([RELEASE_NOTES_7.3.1.md](RELEASE_NOTES_7.3.1.md#if-you-are-upgrading),
[RELEASE_NOTES_7.3.2.md](RELEASE_NOTES_7.3.2.md#if-you-are-upgrading)). 7.4.0 adds no settings.

- The move needs nothing from you. The first time noctis writes `state.json` after the upgrade, it
  writes it without indentation and moves the used checkpoints in it to `used-checkpoints.json`;
  until then a `state.json` from 7.3 is read as it is.
- Going back to 7.3 works, but 7.3 does not know `used-checkpoints.json`. The used checkpoints kept
  there are forgotten: `noctis checkpoint` no longer counts them among the ones used up, a session
  that was handed a note is asked before it reads that note again, and the notes stay in
  `checkpoints/` until you delete them, since 7.3 deletes a note only with its record in
  `state.json`.

## A smaller state

**Used checkpoints have a file of their own.** A checkpoint is kept after it was used, for 7 days
from when it was made: `noctis checkpoint` counts it among the ones used up, and the session it was
handed to may read its note without being asked. In 7.3 all of those records sat in `state.json`,
which every hook reads, and in a busy week they can be more than half of it. Now a checkpoint moves
to `used-checkpoints.json` in the same state write that uses it up, under the same lock. Besides the
writes that move a checkpoint there or drop one that expired, only `noctis checkpoint` and a `Read`
of a note in `checkpoints/` look at that file. While it cannot be read or written the checkpoint
stays in `state.json`, as in 7.3, and `errors.log` says why. A record there goes when it turns 7
days old, with its note unless a checkpoint still to be used names the same note; `state.json` keeps
when the oldest one expires, so no hook reads `used-checkpoints.json` to find out.

**`state.json` without indentation.** noctis reads it on almost every hook and people seldom do, so
it is now written on one line, a fifth to a third smaller, and so is `used-checkpoints.json`. On its
own that saves little time, about 0.2 ms a hook with a 100 KB state, but every write is smaller.
`jq . state.json` shows it indented.

**A `Read` outside `checkpoints/` looks at no checkpoint.** When Claude Code asks whether Claude
may read a file, noctis answers for the note it handed that session. Every note it writes is in
`checkpoints/`, so for any other file it now answers nothing without looking at the records.

Measured on Linux, noctis run directly, medians of 120 calls in each of two runs (the status line:
80 refreshes with nothing new), with the 264 KB state of the 7.3.2 measurements after one
`UserPromptSubmit` has written it: 7.3.2 keeps it at 264 KB, 7.4.0 at 85 KB, with 126 KB in
`used-checkpoints.json`. 7.3.2 was measured again in the same runs, so its times differ a little
from its own notes.

| | 7.3.2 | 7.4.0 |
| --- | --- | --- |
| `UserPromptSubmit` | 10.1–10.2 ms | 7.8–8.2 ms |
| `Stop` | 8.2–8.3 ms | 6.2–6.5 ms |
| `SessionStart` (resume) | 9.3–9.7 ms | 6.9–7.3 ms |
| `PermissionRequest` Read | 7.4–7.6 ms | 5.9 ms |
| status line | 8.7–9.0 ms | 6.8–7.0 ms |

`PreToolUse` Edit, Write and Bash and `PostToolBatch`, which do not read the state, take about 4 ms
in both. With a state that holds no used checkpoints (100 KB, 67 KB without indentation) a hook that
reads it gains about 0.2 ms, and with a small state (805 bytes) the two are within 0.1 ms, both
measured over 200 calls alternating between the two binaries.

## Known limits

- A downgrade to 7.3 forgets the used checkpoints (see above).
- `used-checkpoints.json` has no backup. If it is found broken, noctis starts it over and the used
  checkpoints in it are forgotten, as after a downgrade; `errors.log` says so.
- The limits listed for 7.3.2 and 7.3.1 still apply
  ([RELEASE_NOTES_7.3.2.md](RELEASE_NOTES_7.3.2.md#known-limits)).

## Tests

Eight new Go tests hold the move: a used checkpoint leaves `state.json` for `used-checkpoints.json`
while the state a write returns still has it, a note handed on stays the receiver's after the move
but not once its session writes a newer checkpoint to the same file, a 7.3 `state.json` is read as
it is and moved on the first write, which leaves the backup as it was, the used records expire only
once due and take their notes but not one a checkpoint still to be used names nor a file outside
`checkpoints/`, the newer record of a session wins, the checkpoints stay in `state.json` while their
file cannot be opened, hooks and a `Read` of another file leave `used-checkpoints.json` unread while
a `Read` of the note looks there, and `noctis checkpoint --sid` finds a used checkpoint in its new
file. The test that `state.json` is what a state write encoded now expects it without indentation.
The Go tests and the lab look a checkpoint up wherever it is kept, and one new lab check sees a used
checkpoint leave `state.json`.
