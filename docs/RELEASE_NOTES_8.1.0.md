# noctis 8.1.0

8.1.0 lets a queue that waits on the clock go on by itself. An item deferred with `--until` no
longer waits for your next prompt once its time has passed: the Stop hook waits for a deferral that
ends soon, and a session that stopped on a later one is resumed when it ends, the way noctis resumes
a session after a pause. It carries everything in 8.0.0
([RELEASE_NOTES_8.0.0.md](RELEASE_NOTES_8.0.0.md)).

## If you are upgrading

8.1.0 adds no setting, and `hooks.json` is unchanged.

- **A deferral with `--until` now wakes the session that stopped on it.** If you gave a deferral an
  end so that the queue would stop there and wait for you, defer the item again without `--until`,
  or run `noctis cancel` once the session has stopped.
- **A Stop hook may now hold a stop until a deferral ends,** within `wait.maxInHookMinutes` (330 by
  default) and the hook's own timeout, as it holds one for a pause. Esc ends that wait; run
  `noctis cancel` as well if the session should not go on by itself when the deferral ends.

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

## Known limits

- **A cloud session is not woken for a deferral that ends after the Stop hook may wait.** The next
  prompt, relaunch or session start goes on with the item.
- The limits in [RELEASE_NOTES_8.0.0.md](RELEASE_NOTES_8.0.0.md) still apply, but for the first one:
  a deferral whose `--until` passes now wakes the session that stopped on it.

## Tests

9 new Go tests for the deferral's end: the Stop hook that waits and goes on, a pause during that
wait, the wake set for a later end and what the stop says, the wake that resumes the session, a
moved deferral, a wake with no item to take, a typed prompt, `noctis cancel` and the pruning of a
wake long past.
