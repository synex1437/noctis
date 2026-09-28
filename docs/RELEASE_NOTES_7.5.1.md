# noctis 7.5.1

7.5.1 gives back the few milliseconds 7.5.0 added to the one usage fetch a hook still waits for,
and corrects how the soak and the torrent time a hook. In 7.5.0 a hook that had to wait for the
usage endpoint's answer started a detached `noctis refresh` and waited for it, which cost a process
start even when the endpoint answered at once: with no reading on disk, a `SessionStart` hook took
9.8 ms where 7.4.0 took 6.6. Now such a hook asks the endpoint itself and leaves the question to a
refresher only when no answer came within its 1.5 s: 6.6 ms when the endpoint answers, as in 7.4.0,
and still 1.5 s, not 7.4.0's 5 s, when it hangs. 7.5.1 carries everything in 7.5.0
([RELEASE_NOTES_7.5.0.md](RELEASE_NOTES_7.5.0.md)).

## If you are upgrading

Nothing to do. 7.5.1 adds no settings, and the files noctis keeps are read and written as 7.5.0
reads and writes them.

## A hook that waits for the answer asks the endpoint itself

A hook waits for a usage answer, up to 1.5 s, only where deciding on the reading it has could be
wrong: when it has no reading, or one older than `usage.staleMinutes`; within 8 points of a pause
point or of the 100 % stop; before a check noctis makes once; at session start near a pause point;
and when a subagent starts near the Fable switch point. There the hook now asks the OAuth endpoint
itself, holding `fable.lock` as a refresher would, with those 1.5 s as the request's limit. An answer
in time, or a failure such as a refused sign-in or a rate limit, is written to `fable.json` as
before, and the hook decides on it without starting another process. When the limit runs out first,
nothing is written: an endpoint that is slow has not failed, so no failure and no backoff are
recorded for it. The hook then hands the lock to a detached `noctis refresh`, which asks again with
the full 5 s and leaves its answer for the next hook, and the hook goes on with the reading it has.

A hook whose reading is only due still hands the fetch to a refresher at once and does not wait, as
in 7.5.0, and a hook that finds a fetch in flight still starts none. After taking `fable.lock`, a hook
now first looks whether another process wrote a newer reading since it read its own, and takes that
one without asking, as a fetch made in the hook's own process always did. With the `codex` source
the hook still waits for a refresher, since asking Codex means starting `codex app-server` anyway.

Measured on Linux with 4 CPUs against the lab's local stand-in for the endpoint, the hook process
alone, with any refresher from the call before left to finish first; medians of two runs of 20 calls
(3 with the endpoint hanging), with no usage reading on disk:

| | 7.4.0 | 7.5.0 | 7.5.1 |
| --- | --- | --- | --- |
| `SessionStart` startup, the endpoint answers | 6.59 ms | 9.83 ms | 6.56 ms |
| `SessionStart` resume, the endpoint answers | 5.85 ms | 9.05 ms | 6.14 ms |
| `PreToolUse` `Agent`, the endpoint answers | 5.91 ms | 8.62 ms | 6.04 ms |
| `SessionStart` startup, the endpoint answers 429 | 5.82 ms | 9.65 ms | 5.91 ms |
| `SessionStart` startup, the endpoint hangs | 5 011 ms | 1 506 ms | 1 508 ms |
| `PreToolUse` `Agent`, the endpoint hangs | 5 014 ms | 1 509 ms | 1 508 ms |

An earlier pair of runs, on this code before a rename that changes no behavior, gave 5.96, 5.54,
5.54 and 5.58 ms for the first four rows, where 7.4.0 took 6.47, 5.87, 6.28 and 5.87: 7.5.1 and
7.4.0 are within the noise of each other there, and 7.5.0 is 3 ms slower than either.

Against the real endpoint a fetch takes 150 to 400 ms, so the process start this saves is a small
part of it; what 7.5.1 changes there is that a slow answer is no longer recorded as a failure.

## The soak and the torrent time the hook

After each hook the lab waits for the refresher the hook may have started, so that the next step
sees its answer, and the soak and the torrent timed that wait with the hook. On 7.5.0 that made the
soak's longest hook 5 015 ms: with the endpoint hanging, the refresher ran into its 5 s timeout, while
the hook had returned after 6 to 14 ms. Both now time the hook process alone, as the host waits on
it, and the soak reports the lab's longest wait apart (`labWaitForRefresherMaxMs`). A hook other than
`StopFailure` that holds the host 4.5 s or more without running the blind probe is now an anomaly
that fails the soak: that is what a hook took in 7.4.0 when it waited on an endpoint that hangs.

## Known limits

- An endpoint that does not answer within 1.5 s is asked twice when a hook waits, where 7.5.0
  asked it once: by the hook, which gives up on its own request after those 1.5 s, and by the
  refresher the hook then starts. Only one process asks at a time (`fable.lock`), a refresher that
  gets no answer within 5 s records a failure and its backoff as before, and a hook that waits is
  rare: it needs a missing or stale reading, a pause point or the 100 % stop within 8 points, or a
  check noctis forces.
- The limits listed for 7.5.0 still apply
  ([RELEASE_NOTES_7.5.0.md](RELEASE_NOTES_7.5.0.md#known-limits)).

## Tests

Two new Go tests hold the hook's own ask. With no reading and an endpoint that answers at once, the
hook gets the answer itself, the endpoint is asked once and no refresher runs. With an endpoint that
does not answer within the hook's bound, the hook writes no failure to `fable.json`, a refresher
holds `fable.lock` when the hook returns, and its answer lands once the endpoint answers: the
endpoint is asked twice, once by each. The lab checks both with the shipped binary. Against an
endpoint that answers after 4 s, a hook with no reading still returns after 1.5 s and the answer
still lands, now from the second of two requests, where 7.5.0 made one; against one that answers at
once, the hook returns with the answer, asks once and starts no refresher.

The test of a window that pauses again while its first runner still watches it now waits for the
relaunched session's prompt before it closes the window. It closed the window as soon as the stand-in
for `claude` had recorded its call, before the stand-in wrote its prompt to the transcript; a window
closed that early did nothing, the runner rightly kept the pause for another try, and the test failed
once under the race detector on a loaded machine.

The soak times the hook process alone and fails on a hook other than `StopFailure` that holds the
host 4.5 s or more without running the blind probe. Run against 7.4.0's binary, the same two-day
hard soak reports five such hooks, 5 007 to 5 022 ms on an endpoint that hangs; against 7.5.0's and
7.5.1's, none.
