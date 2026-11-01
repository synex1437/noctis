# noctis 8.7.2

noctis no longer asks for subagents of its own. To keep the session's context small, 8.7.1 sent work
away from it: a queue's instructions handed research, copy and docs items to `noctis:lite` and long
test runs, big diffs and noisy logs to `noctis:digest`; an item the session kept stopping on went to
a stronger model's subagent (`noctis:worker` on Opus, or `noctis:deep`, Opus at max effort), and so
did the fix of a queue check that kept failing; and the descriptions of `noctis:lite` and
`noctis:digest` invited the model to use them in any session. Each subagent starts with a context of
its own, about 41k to 53k tokens before it does anything in the transcripts 8.7.0 measured, and reads
all of it again on every call, so the weekly limit went on building contexts rather than on the
work. 8.7.2 keeps the work in the session. Each of these can be turned back on. 8.7.2 carries
everything in 8.7.1 ([RELEASE_NOTES_8.7.1.md](RELEASE_NOTES_8.7.1.md)).

## If you are upgrading

- `config.json` gains `"configVersion": 4` at the next setup or session start, and three values the
  earlier defaults wrote there change then: `router.digest` `true` becomes `false`, `queue.escalate`
  `auto` becomes `off`, and the former default of `queue.github.comment` becomes the new one (below).
  Until then noctis reads `config.json` as if that were done. Any other value stays, and so does one
  set after that.
- `agents/lite.md` and `agents/digest.md` have new descriptions. A session reads the agent files as
  it starts, so restart running sessions once after the update.
- No new settings. `hooks/hooks.json` is unchanged.
- 8.7.1 reads the new `config.json` as it is: it keeps `configVersion` 4 and the values stored there.

## The work stays in the session

| | 8.7.1 | 8.7.2 |
| --- | --- | --- |
| A queue's research, copy and docs items | `noctis:lite`, one subagent each | the session |
| A queue's long test runs, big diffs and noisy logs | `noctis:digest` | the session; `router.digest: true` brings the digest agent back |
| An item the session keeps stopping on | once to `noctis:worker` on Opus or `noctis:deep`, then set aside | set aside; `queue.escalate: auto` brings the hand-off back |
| A queue check that keeps failing | its fix once to the same agents, then the queue is held | the queue is held; `queue.escalate: auto` brings the hand-off back |
| `noctis:lite` and `noctis:digest` in any session | opened whenever a task looked like their description | opened only when you or noctis's instructions name them |

A queue's instructions now say to do the work in the session, reading only what each item needs, and
to open a subagent only where they name one. With `queue.escalate` off, the continuation that would
have handed a stuck item up asks for it to be set aside with its reason, as when no subagent could be
opened, and the stop after that ends the queue's continuations: by default the second stop in a row
without progress asks for it and the third gives up (`queue.maxIdleContinues`, 3), each one stop
sooner than with a hand-off. A check that keeps failing holds the queue until it passes, as before,
without the hand-off first.

What stays:

- The router stays off until you turn it on, and once on it still hands research to `noctis:lite`.
- A queue item tagged `(opus)`, `(sonnet)`, `(haiku)` or `(fable)` still goes to `noctis:worker` on
  that model while the session runs on another one: the tag asks for it.
- `queue.subagentAboveTokens` stays 0, off, as since 8.7.0.
- The spawn gate, the budgets and the growth limit of 8.7.0 still hold the subagents the model opens
  on its own.

## The comment on a closed GitHub issue

`queue.github.closeOnDone`, off by default, closes a GitHub issue once its queue items are done and
leaves `queue.github.comment` on it. Its default now reads `Completed by the noctis queue.`, and a
`config.json` that holds the former default gets the new text once.

## Known limits

- A `router.digest: true` or `queue.escalate: auto` set by hand before 8.7.2 looks the same as the
  former default and changes too. Set it again after the update and it stays.
- A description only asks: the model may still pick `noctis:lite` or `noctis:digest` now and then.
  The spawn gate counts `noctis:lite` as any other subagent; `noctis:digest` is still exempt
  (`subagents.exempt`).
- A session started before the update keeps the former descriptions until it restarts.

## Tests

8.7.2 adds 3 Go tests, in `subagentrequests_test.go`:

- The shipped config asks for no subagent of noctis's own: `router.digest` is `false` and
  `queue.escalate` `off`, a config without either key names no digest agent and sends no item up,
  and the issue comment is the new text.
- A `config.json` an earlier noctis wrote (`configVersion` 3 with `router.digest` `true`,
  `queue.escalate` `auto` and the former issue comment) changes once, through a load and a merge,
  and records `configVersion` 4. A router turned on stays on, `maxEscalationsPerDay` stays, and the
  values set again after the move stay.
- Settings noctis never wrote (`router.digest` `false`, `queue.escalate` `opus`, a comment of your
  own) stay.

Changed tests:

- `TestTheQueueDirectiveAsksForNoSubagentOfItsOwn` in `handoffroom_test.go` replaces the test of
  the directive that sent research to `noctis:lite`: the directive keeps the work in the session,
  names `noctis:digest` only with `router.digest` on, and keeps every item in the session while no
  subagent can be opened.
- The tests of the hand-off itself (`escalate_test.go`, `checkescalate_test.go`, `progress_test.go`
  and `stopcap_test.go`) turn `queue.escalate` to `auto`.
- As shipped, a queue that makes no progress asks for the item to be set aside at the third stop and
  gives up at the fourth; with `queue.escalate` on, each comes one stop later.
- The lab checks that the queue's instructions name no subagent of noctis's own, that they name
  `noctis:digest` once `router.digest` is on, and turns `queue.escalate` to `auto` for its check
  escalation scenario.

The results of the local rounds on this release are in its commit message.
