# noctis 8.1.0

8.1.0 lets a queue that waits on the clock go on by itself, and tells you once a day how it went.
An item deferred with `--until` no longer waits for your next prompt once its time has passed: the
Stop hook waits for a deferral that ends soon, and a session that stopped on a later one is resumed
when it ends, the way noctis resumes a session after a pause. With `alarm.digestAt` set, your phone
gets one message a day through the webhook with what each queue finished, what it takes next and
what waits on you. Once sessions have ticked a few items, noctis also says what an item takes of the
weekly limit and when the items left may be done, an item can name the model it is for, an item
Claude cannot get past is set aside instead of ending the queue, and the decisions Claude takes
without asking reach you. The hook of a failed turn also runs `git status` while it reads the
limits instead of after. It carries everything in 8.0.0
([RELEASE_NOTES_8.0.0.md](RELEASE_NOTES_8.0.0.md)).

## If you are upgrading

8.1.0 adds one setting, `alarm.digestAt`, which is empty (the daily digest is off) until you set it,
and `hooks.json` is unchanged.

- **A deferral with `--until` now wakes the session that stopped on it.** If you gave a deferral an
  end so that the queue would stop there and wait for you, defer the item again without `--until`,
  or run `noctis cancel` once the session has stopped.
- **A Stop hook may now hold a stop until a deferral ends,** within `wait.maxInHookMinutes` (330 by
  default) and the hook's own timeout, as it holds one for a pause. Esc ends that wait; run
  `noctis cancel` as well if the session should not go on by itself when the deferral ends.
- **`(opus)`, `(sonnet)`, `(haiku)` and `(fable)` in an item's text now pick the model it runs on**
  (through a subagent, see below). Reword an item that names a model in parentheses for another
  reason.

## A deferral with an end wakes the queue

In 8.0.0 an item deferred with `--until` became eligible again when its time passed, but a session
that had stopped on it stayed stopped until a prompt, a relaunch or a new session came along. On a
server nobody watches, that was the rest of the night.

Now, when every open item left is yours (`(human)`), deferred, or waits for one of those, and a
deferral with an end holds back an item that waits for nothing else:

- **If it ends soon, the Stop hook waits for it.** When the end is within the time a wait in place
  may take, the hook holds the stop, looks at the queue every 15 seconds, and once the deferral
  ends goes on with the item in the same session. Claude is told which deferral ended and what it
  waited on, and to defer the item again if that is still missing instead of stalling on it; you
  see `⏵ The deferral of "…" ended: the queue goes on with it.` If the queue file changes meanwhile
  so that an item is eligible, it goes on at once. A pause of noctis, a prompt or `noctis cancel`
  ends the wait and lets the session stop.
- **If it ends later, a wake resumes the session then.** The session stops, the notice that names
  the deferred items adds `The session goes on by itself at 14:30, when the first deferral ends.`,
  and a runner (`noctis queue-wake`) is scheduled for that moment the way the runner of a pause is:
  a Windows task, a systemd timer, a launchd job or a sleeper. When it fires it resumes the session
  as noctis resumes one after a pause: it reads the limits first and waits for a window that is
  used up, starts fresh from a checkpoint when the context is large and the session has been idle
  long enough (`resume.freshAboveTokens`, `resume.freshAfterMinutes`), and relaunches it the way it
  last ran. It leaves the session alone if it went on in the meantime, if the queue no longer
  drives it, is not trusted as it is or is held by a failing check, or if no item is eligible then.
  A deferral moved later sets it again for the new end, and while noctis is paused it waits for
  the pause to end. With `resume.mode` `none` it only tells you.
- **A prompt takes the queue up yourself.** A prompt typed in the session, or in another session
  the same queue drives, drops the wake, as do `/clear` and `noctis cancel`. `noctis status` lists
  the wake with the pending waits as a `queue` wait, and `noctis why` shows `wait-deferral`,
  `deferral-ended`, `arm-queue-wake`, `drop-queue-wake` and the wake's `resume` or `skip` with its
  reason.

A cloud session (`CLAUDE_CODE_REMOTE`) starts no runner: there the Stop hook still waits for a
deferral that ends soon, and one that ends later waits for your next prompt, as in 8.0.0.

## A daily digest on your phone

A job left to run for days sends notices when something happens, but nothing says how it stands
when nothing does. Set a time of day next to the webhook:

```json
"alarm": {"digestAt": "21:00", "webhook": {"url": "https://…", "preset": "telegram", "chatId": "…"}}
```

Every day at that time (local time) one message goes out through the webhook, in any preset:

- the limits as the status line shows them, with the time of the reading when it is over an hour
  old, and when a pause of noctis ends;
- the sessions that wait for a reset, or for a deferral of their queue to end, and when they go on;
- for each trusted queue file a session drove or you trusted in the last 7 days (at most 4): how
  many items are done and open, the items ticked since the last digest that went out, a change that
  waits for your trust, the item it takes next, how its check stands (passed, failing, or holding
  the queue), and the items marked `(human)` and the deferred ones with what they wait on.

It stays within 1900 characters, which every preset takes as one message, and costs no model call.
A runner scheduled the way a relaunch is sends it and sets up the next day's; a session that starts
or stops sets that runner up when none is set for the time asked, and sends a digest missed while
the machine was off or asleep. `noctis digest` shows the digest now and when the next one goes out,
`noctis digest --send` sends it now, and `noctis status` has a `Digest` line with the next time, the
reason the last one did not go out, or why the digest is off (the time is not `HH:MM`,
`alarm.enabled` is false or no webhook is set). `noctis why` shows `start-runner`, `arm`, `sent`,
`failed` and `drop`.

## Whether this week's limit covers the rest

The digest says what a queue did, not whether the week will cover what is left. noctis now notes the
pace of each queue file a session drives: when a session stops with another number of items done
than noctis noted last, it notes the time, the items done and how much of the weekly limit is used
then (the last 25 changes of each file, kept 30 days). From the items ticked within 6 hours of the
note before it works out the time an item takes, and from the ones ticked between two readings of
the same weekly window the share of the weekly limit an item takes. Once there are 3 such items,
`noctis queue status` says, for example:

```
  Pace: 1h 10m an item over the last 6; a whole weekly limit covers about 40 items
  At this pace the 18 left need about 45% of the weekly limit (32% is left before its pause point) and are done around Sat 04.10 09:20
```

The finish counts the items left one after the other (the deferred ones too, not the `(human)`
ones), each taking its time and its share, and when the next one would pass the pause point
(`thresholds.weeklyAll`, or 100 % while that is off) it starts after the weekly reset. The same
lines are in the daily digest under each queue and in `noctis status` under the queue's name, and
`noctis queue status --json` gives them as `pace`. Until the pace is known, `queue status` says how
many such items it has. Unticking items starts the notes over, and a checklist noctis wrote from a
prompt is left out. It costs no model call, and a stop that ticked nothing only reads.

## A model for each item

A queue mixes hard items with easy ones, and one session runs them all on one model. Tag an item
with the model it is for:

```markdown
- [ ] design the billing schema and its migrations (opus)
- [ ] write the unit tests for the parser (sonnet)
- [ ] rename the settings keys in the docs (haiku)
```

When the session that takes a tagged item runs on another model (the model its status line
reports, else `models.primary`), the Stop hook tells Claude to hand the item to a general-purpose
subagent with that model and a brief that stands on its own, then to check its work and tick the
item itself; for that item this takes the place of the note that hands items to a subagent once the
context is large. The queue instructions a session starts with say what a tag means. The session
keeps its own model, the PreToolUse hook still puts `models.fallback` in place of a scoped model
whose quota is out, and a host without subagents leaves the tag as text. `noctis why --json` shows
`itemModel` on `continue-queue`.

## A stuck item is set aside, the queue goes on

A session that keeps stopping without ticking an item or committing used to end the queue after
`queue.maxIdleContinues` continues (4), with every item after the stuck one left for the morning.
Now the last of those continues asks Claude to find what holds the item up and, if that cannot be
resolved now, to defer it with the root cause as the reason and go on with the next item; you see a
notice, and `noctis why --json` shows `setAside` on that `continue-queue`. The deferral counts as
progress, so the queue goes on; when Claude does not defer the item, the queue gives up at the next
stop as before. The check between items works the same way: the last send-back before the queue is
held tells Claude, when it cannot fix the failure now, to undo the item's changes (keeping them on a
branch or in a stash), leave it unticked and defer it, so the check passes again without it. The
deferred item and its reason show in `noctis queue status` and in the digest, and `noctis queue
undefer` takes it up again.

## The decisions Claude took without asking

A queue tells Claude to decide rather than ask, so the choices it makes overnight are easy to miss.
The queue instructions a session starts with now ask Claude to note each decision you may want to
revisit, in one line:

```bash
noctis queue note "kept the old API next to the new one: two clients still call it" --file TASKS.md
```

`noctis queue status` gives the last 5 and how many in all (`decisions` in `--json`), the daily
digest the ones noted since the digest before, and the next session's queue instructions the last 5,
so a fresh context keeps to them instead of deciding again. A file keeps its last 50 decisions in
`queue-notes.json`. It costs no model call.

## A failed turn's hook reads `git status` while it reads the limits

When a turn fails on a limit, an overload or a server error, the StopFailure hook reads the limits,
which can mean a request to the usage endpoint, and then writes a checkpoint that lists `git status`
and fingerprints the working tree with it. The two ran one after the other. Now `git status` starts
first and runs while the hook reads the limits; the checkpoint and the fingerprint use that run's
output while it is under 2 seconds old, and the hook does not end before the run has. Every
checkpoint, a pause's too, also runs `git status` while it reads the transcript. In the lab's small
repository, where `git status` takes about 4 ms, the hook's median went from 28.3 ms to 25.2 ms
(four runs of 40 calls, the two builds taking turns); outside a repository nothing changed. In a
large tree, where `git status` takes longer, the hook saves up to the time it spends reading the
limits.

## Known limits

- **A cloud session is not woken for a deferral that ends after the Stop hook may wait.** The next
  prompt, relaunch or session start goes on with the item.
- **The digest goes out from the machine noctis runs on.** While it is off, none goes out; the one
  missed goes out when a session next starts or stops there.
- **The pace is the average of the last items.** For a queue whose items differ a lot in size it is
  rough; the time between two ticks counts as the item's, whatever else the session did then (a gap
  over 6 hours is left out), and so does the weekly use of other work in the same account.
- **A model tag works through a subagent.** The item's work runs in a subagent of that model and the
  session checks and ticks it, so the session's own turns stay on its model; on a host without
  subagents the tag does nothing.
- **Setting an item aside is Claude's call.** noctis asks at the last continue; when Claude keeps
  trying instead, the queue gives up as before. A check that fails for a reason outside the item (a
  service that is down) is not helped by deferring it, and the queue is held as before.
- **Claude notes the decisions it sees as worth it.** Nothing checks that every choice is noted, and a
  note is as good as its line.
- **A failed turn that writes no checkpoint waits for its `git status` too.** When its retries are
  spent, or it repeats a failure the hook just handled, the run started ahead is still waited for
  before the hook ends: in a repository where `git status` is slow, up to its 3-second limit.
- The limits in [RELEASE_NOTES_8.0.0.md](RELEASE_NOTES_8.0.0.md) still apply, but for the first one:
  a deferral whose `--until` passes now wakes the session that stopped on it.

## Tests

9 new Go tests for the deferral's end: the Stop hook that waits and goes on, a pause during that
wait, the wake set for a later end and what the stop says, the wake that resumes the session, a
moved deferral, a wake with no item to take, a typed prompt, `noctis cancel` and the pruning of a
wake long past.

8 new Go tests for the daily digest: what it says about a queue and what waits on the user, a
digest that goes out and the next one naming what was ticked since, a failed delivery that keeps
the baseline, the runner that sends a due digest once and sets up the next day's, the session that
starts the runner when none is set or it is late, the next time of day, a digest turned off and its
runner dropped, uninstall dropping the runner, and `noctis digest` with and without `--send`.

6 new Go tests for the pace: the notes the Stop hook takes and their restart when items are
unticked, the time and share of an item and the finish of the items left, the wait for the weekly
reset at the pause point (and at 100 % with the threshold off, and without a weekly reading), long
gaps and a weekly reset left out, `queue status` (text and JSON), the digest and `noctis status`
giving the pace, and the notes of a file dropped a month after the last one.

5 new Go tests for model tags: reading a tag, the Stop hook handing a tagged item to a subagent of
its model and not when the session runs on it, the large-context note giving way to it and staying
for an item of the session's own model, and the queue instructions saying what a tag means.

4 new Go tests for a stuck item: the last continue before the idle limit asking to set it aside and
the queue going on with the next item once it is deferred, the queue giving up as before when it is
not, no set-aside at the first continue, and the last send-back of a failing check saying how to set
the item aside.

5 new Go tests for decisions: `queue note` and `queue status` (text and JSON), a note kept without
control characters and within 300 characters, the last 50 kept and a file left for a month dropped,
the queue instructions asking for decisions and giving the last ones, and the digest naming the ones
noted since the digest before.

4 new Go tests for the failed turn's `git status`: the status a hook reads is the run started ahead,
a status asked for while that run is under way waits for it instead of starting another, a fresh
status is not run again ahead and none is started outside a repository, and a failed turn that
gives up leaves no run behind.
