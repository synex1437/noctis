# noctis 8.5.2

8.5.2 fixes five defects in how a runner ends a pause, which came to light while the soak and the
monkey learned to check that a pause is continued once. Each fix comes with a test that fails
without it. 8.5.2 adds no feature, and carries everything in 8.5.1
([RELEASE_NOTES_8.5.1.md](RELEASE_NOTES_8.5.1.md)).

## If you are upgrading

8.5.2 adds no settings and reads every file 8.5.1 wrote. One answer changes: `noctis why` has a
`skip-launch` line for a pause a runner ended because Claude Code's own auto-continue had resumed
the session, because `resume.mode` is `none`, or because no usage data had come in by its last try,
and for the relaunch after Fable's limit of a session that had gone on in its own window, which
8.5.1 relaunched; `noctis why --stats` counts these lines among its decisions.

## A failed turn is retried, whatever the session wrote just before it failed

When a turn fails on `overloaded` or a server error, on Fable's limit, or on an error the usage
limits do not explain, noctis parks a retry, and the runner that relaunches the session first checks
that the session did not go on by itself in the meantime. 8.5.1 counted what the session wrote from
the start of the second the failure came in, so the last prompt or answer before the failure counted
when it fell in that same second: most often the prompt a limit turned down at once, or Claude's
call of a quick tool just before the request that failed. The runner then dropped the retry as a
session that had gone on (`skip-launch`, *the session went on in its own window after the reset*),
and the session stayed stopped unless a same-session wake brought it back. After Fable's limit,
where only the runner moves the session to the fallback model, nothing did. 8.5.2 counts from the
end of that second.

## A retry does not relaunch a session that answered as its relaunch failed

When a relaunch ends with an error before the session answers, the runner tries again later, and
first checks that the session did not go on in the meantime. 8.5.1 counted only what the session
wrote after the end of the second the relaunch ended in, so an answer in the rest of that second,
written after the runner had looked, from the session's own window or from a process the relaunch
left behind, counted nowhere, and the retry continued the session a second time. 8.5.2 counts it.
The prompt and the API error the failed relaunch wrote still do not count, so a relaunch that did
nothing is still retried.

## A clock that went back does not end a pause without a relaunch

Before it relaunches a session, a runner checks that the session did not go on by itself after the
pause, and a queue wake checks the same from when it was set; after a relaunch that ended with an
error, the runner checks whether the session answered before it retries. 8.5.1 went by the time
stamped on each line of the transcript, so once the system clock had gone back, what the session
wrote before the pause, stamped while the clock was ahead, counted as written after it: the runner
ended the pause (`skip-launch`, *the session went on in its own window after the reset*), the queue
wake left the session alone, or a relaunch that failed passed for one the session had answered and
was not retried, and the session stayed stopped. A clock set back a few minutes was enough when a
pause came within those minutes, most often the retry of a turn that failed. 8.5.2 records how long
the transcript was when the pause or the wake was recorded, and counts only what was written after
that. A pause that 8.5.1 recorded is checked as before.

## After Fable's limit, a session that went on in its own window is not relaunched as well

When Fable's weekly cap runs out mid-task, noctis switches the default model to the fallback role
and parks a relaunch of the session on it, which a runner starts in a new window shortly after.
Before it relaunches a session, a runner checks that the session did not go on by itself in the
meantime, but 8.5.1 left this pause out of that check: a session that answered in its own window
first, after a `/model` switch there for instance, was relaunched all the same, so the work went on
in two windows, and the window it had gone on in had its next prompt refused as a duplicate. 8.5.2
checks it. An answer of the model from after the second of the switch ends the pause with
`skip-launch`, *the session went on in its own window after the model switch*; a prompt alone, a
local command such as `/usage`, an API error and the answer of the turn the switch ended do not
count, so the session is still relaunched when nothing answered. A pause that 8.5.1 recorded is
relaunched as before.

## A runner that ends a pause without a relaunch says why

A runner that ends a pause without relaunching the session leaves its reason in the decision
journal. 8.5.1 wrote nothing in three cases, so `noctis why` did not say what became of the pause:
when Claude Code's own auto-continue had already resumed the session, when `resume.mode` is `none`,
and when the runner gave up because no usage data had come in by its last try. Each now writes
`skip-launch` with its reason.

## Known limits

The limits in [RELEASE_NOTES_8.5.1.md](RELEASE_NOTES_8.5.1.md) still apply. After Fable's limit, a
session that goes on in its own window is still relaunched when its answer has not reached the
transcript by the time the runner relaunches it, and the window it went on in then has its next
prompt refused as a duplicate.

## Tests

Each fix comes with a Go test that fails without it.

The `claude` the soak and the monkey launch for a relaunch now answers in the session's transcript,
says nothing, fails, or answers a moment after it exits, as the seed picks. At the end of a run
`tests/continuations.js` checks every relaunch against the answers it could see: no pause is
continued twice, no session is relaunched after it went on by itself, and every pause a runner ended
without a relaunch has its reason in the journal. Two kinds of chaos join the soak's 20: a session
that goes on in its own window while its pause waits, and a pause that comes back with the backup
of a damaged `state.json` after its relaunch answered. Among the monkey's moves the clock jumps
minutes to hours ahead and the session answers on its own.

The first defect showed in four seven-day soaks with `--hard 1`: once a relaunched `claude` had
answered, the next retry of an overload storm was never relaunched. The second showed in one with
seed 1, where the `claude` that fails and answers a moment later answered within the second its
relaunch ended, the third in one with seed 17, where the soak had set the clock back ten minutes
shortly before an overload storm, and the fourth in one with seed 3, where the soak paused a session
that would go on in its own window just as Fable's weekly bucket passed its pause point. The same
soaks no longer flag seven things that were the soak's own mistakes: a StopFailure that waits, by
design, up to 1.5 s for a hung usage endpoint counted against the budget of one that does not; two
checks timed the lab's wait for noctis's refresher along with the hook; a forced wall fell within a
minute of the window's reset, where noctis waits in the hook instead of pausing; a forced wall fell
on a session in a typed turn, which noctis lets run on; a subagent blocked early at a fast burn rate
counted as unexpected below 84 %; the soak moved its clock on while a `claude` that had failed was
about to answer, so the answer came in stamped before the next relaunch and the retry after it did
not count it; and a seven-day run in which noctis moved off Fable only for a turn that failed on
Fable's limit, in its runner, or in a hook the soak's forced walls or chaos sent failed as one that
exercised no move off Fable, since the soak counted only the moves its own turns handled. It now
counts them in noctis's decision journal, names in its summary what a seven-day run exercised too
little of, and fails a `--hard 1` run with fewer than ten chaos injections, which it never did
before, as it read a count it did not keep. Any soak also counted a workflow the gate refused for
want of room as refused at low usage whenever every window was under 70 %, while since 5.5.3 the
gate has wanted 25 points of room before each window's pause point, a 5-hour window at most 67 %
with the shipped thresholds; a CI run on Windows reached its workflow launch at 68.7 %. A refusal
now counts as wrong only when every window has that room and 4 points more. The Go test of the paced
wait, `TestSleepUntilPacedAsksThePaceEachRound`, failed once in CI on a busy runner: the moment it
waits for was two whole seconds ahead, and when the test began late in a second and its one-second
sleep woke late, that moment came before its second tick. It is now five seconds ahead. The results
of the local rounds on this release are in its commit message.
