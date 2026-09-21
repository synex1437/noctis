# noctis 5.5.1

5.5.0 shipped with a suite that had never run anywhere but Linux. Its own notes said "CI runs the
suite on all three platforms" — that was the intention, not an observation. This release is what
happened when the suite was put on real Windows and macOS runners for the first time and told to go
green without loosening a single assertion.

Twelve product bugs came out of it, five of them in the file lock and the reads around it, and two
of them the same failure the release leads with wearing a different coat: a session that should
have come back and did not. Everything else was the tests carrying Linux assumptions they had never
been asked to justify.

## The one that matters: on Windows, a parked session was never resumed

**Product bug.** `enforceWait` spawned the resume runner and *then* wrote the wait record.
`sleepUntilEvery` runs its first tick immediately, with no initial sleep. So the sleeper's first act
was to read the `waits` map — and it usually got there first. It found nothing, concluded the wait
had been cancelled, and exited at birth, seconds after it was started.

The session stayed parked. Nothing ever brought it back.

And nothing said so. The give-up path logs at INFO, so `errors.log` stayed clean. `noctis status`
showed a wait with a resume time and a scheduled runner, because the record it reads was written
immediately afterwards — by then the process it names is already gone. `noctis doctor` reported
nothing, and the repair pass skipped the session because its record *has* a scheduled entry.

This is the twin of the bug 5.5.0 led with. That one was "schedule the relaunch, then record the
wait", and the recorder killed the runner that had just been started. The same ordering was still
there one layer down, where it killed the *watcher* instead. The wait is now registered before
anything is spawned, so there is no window left to lose.

What it cost the person: on Windows, an overnight park did not come back — the plugin's entire
reason to exist. Nearly deterministic there; on Linux it needed load, which is why thousands of
torrent sessions had never shaken it out and a single Windows CI run did it on the first try.

Two rejected fixes are worth recording, because both were worse than the bug's shape suggested.
Letting the sleeper linger while it waited for the record kept dead sleepers alive holding their
working directory open, and Windows could not then remove that directory. Having the sleeper poll
state for two seconds put the decision back in the racing process. Registering first removes the
race instead of tolerating it.

## A lock could be taken from a process that was still holding it

**Product bug**, and the one that quietly lost data. Reclaiming an abandoned lock read the holder's
pid, asked whether that process was still alive, and then deleted the file. Between the read and the
delete the holder can finish, exit and be replaced. The pid now reads dead, so the waiter deletes a
lock a *different*, live process is holding, and both run their critical section at once. One of
them writes the whole state file over the other's copy. Nothing fails, so nothing is logged: the
losing process exits 0 having silently thrown its own update away.

Every holder of this lock is a short-lived hook process, so reading a pid that dies a moment later
is the ordinary case rather than a corner. Windows made it routine. `processAlive` there is
`OpenProcess` plus `GetExitCodeProcess`, which reports a process dead the instant it exits, while
`kill(pid, 0)` on Unix keeps answering for a zombie until its parent reaps it. The lab check that
fires twelve concurrent writes and counts the survivors lost **six of twelve on Windows, one of
twelve on macOS, and none on Linux** — the platform's process semantics, read straight off the
failure rate.

There are two barriers now. A dead owner's lock must also be stale — two seconds, far longer than
any real hold and well inside the ten-second give-up — so a lock a live process created
milliseconds ago can never be a candidate, whoever's pid was read. And the owner is read once more
immediately before the removal, so a lock that changed hands while the decision was being made is
left alone.

## Every read on Windows blocked the write it was racing

**Product bug**, and the root of most of what this release spent its time on. Windows will not let
a file be renamed over or deleted while somebody has it open, unless that somebody asked for
`FILE_SHARE_DELETE` when they opened it. Go's `os.ReadFile` does not ask — `syscall.Open` requests
share-read and share-write and nothing else — so on Windows every read the plugin performed stood
in the way of the next write to that file.

It surfaced in two places that look nothing alike and are the same thing underneath.

The lock file first. A waiter reads the lock to see who owns it; while it does, the owner cannot
remove it. Both halves are in the log, one after the other:

```
lock release failed: ... being used by another process
lock open failed: ... Access is denied.; state.lock not written
```

The owner cannot delete its own lock, Windows leaves the name delete-pending — still there, no
longer openable — and the next `O_CREATE|O_EXCL` gets `ERROR_ACCESS_DENIED` instead of
`ERROR_FILE_EXISTS`. That is not the "someone else has it" the loop was written for, so it was
treated as fatal and the write was refused and thrown away.

Then the state file. `state.json` is replaced by renaming a staging file over it, and that happens
while the lock is held. A reader arriving at that moment makes the rename fail, so the writer
retries — about a third of a second, still holding the lock — and meets the next reader. Every
process queued behind it watches ten seconds go by without the lock changing hands and gives up at
the same instant: `state.lock is held by another process; not written`. One Windows torrent lost
thirty-nine writes that way, in bursts of three.

The fix is one primitive. Reads go through `readFileShared`, which on Windows opens with all three
share flags and on every other platform is `os.ReadFile` unchanged, and every reader of a file
another process replaces underneath it now uses it: the strict JSON reader behind `state.json`,
`usage.json` and `fable.json`, the lock-holder probe, the quiet-marker fingerprints, the pid files.

The fingerprints are where the volume was, and they are why this took as long as it did. Deciding
whether a hook may stay silent fingerprints `config.json`, `state.json` and `fable.json`, and the
hook fingerprints `state.json` once more before that — four unshared opens of the three most
contended files, on every single hook. The reader under investigation was never the one doing the
damage.

Three earlier mitigations stay, because each is right on its own terms: `ERROR_ACCESS_DENIED` on
the create means busy rather than fatal, on Windows only, so a real permission error on Unix still
fails as loudly as it did; each poll reads the lock once rather than two or three times; and the
poll interval is spread by pid so waiters stop opening the file in lockstep.

## A file Windows would not open yet was read as a file gone bad

**Product bug**, and the most damaging single one. A reader that arrives while a rename is
replacing its target gets `ERROR_SHARING_VIOLATION` — the file is fine, it just cannot be opened
this instant. The reader could not tell that from unparseable content, so it took the other branch:
`state.json` declared unusable and restored from the backup. One Windows torrent did that
fifty-eight times on a single account.

Restoring the backup rolls state back to the write before last. Nothing failed, nothing was locked,
no error was raised about the records that disappeared — they were quietly replaced by older ones.
Every lock bug in these notes can lose one update; this one gives back several at a time.

Three changes, all in the read path. A transient open failure is retried, eight times across about
a third of a second, far longer than a rename holds the name, and because the retry lives in the
primitive they share it closes the window for `state.json`, `fable.json` and `usage.json` alike.
The backup is restored only when the file was read and its contents would not parse, which is the
corruption it was written for. And a state file that cannot be opened at all no longer yields an
empty state to write over the top of: the update refuses, because continuing from nothing would
erase every other session's record.

One honest note on the order these landed in. This retry made the blocking above worse before that
was fixed — a reader that holds on through a failing rename is a reader the rename is waiting for.
It is the right behaviour now that readers no longer stand in the way of writers at all.

## State updates were dropped under load

**Product bug.** `withFileLock` gave up a fixed number of seconds after it *started queueing*, and
by design that fails closed: the write is refused and logged rather than performed unlocked.
Correct when a holder is stuck, wrong when the lock is merely busy. Under the torrent's load on
macOS hundreds of sleepers resume at once, the queue is longer than the deadline, and state updates
were thrown away — `state.lock is held by another process; not written`, with the worst hook
spending ten and a half seconds doing nothing but losing its turn.

The two constants disagreed about this already: the reclaim path respects a live holder for 120
seconds while the wait gave up after ten. The deadline now measures the absence of progress rather
than the length of the queue — every time the lock changes hands the clock starts again, capped at
the same 120 seconds, which both now share as one constant. A lock nobody releases still fails
closed on schedule, because its holder never changes.

Alongside it, every resume path now clears the wait and consumes the checkpoint in a **single**
locked write instead of two. They
were always meant to happen together — a consumed wait with an unconsumed checkpoint behind it is a
state no code expects — and doing them separately meant the second one could be the one that was
refused. That halves the locked writes on the path where the contention actually was.

Releasing a lock that another process had already reclaimed as abandoned also stopped warning about
it. The file being gone is the outcome the release wanted.

## Under load, the relaunch could not find the tool it was meant to start

**Product bug**, and the other way a parked session failed to come back. Working out which `claude`
to launch ran `where.exe claude` on Windows and `which claude` elsewhere — a whole child process,
on every launch, with a five-second timeout. That timeout is reachable. In the first Windows torrent
run the suite has ever done, hundreds of sleepers resumed at once on a runner already slow enough to
make a single hook take thirty-three seconds, each starting its own finder. The finder returned
nothing, the path came back empty, and the sleeper gave up:

```
launch aborted: claude executable not found
runner acct1-job165: automatic relaunch failed; resume manually with claude --resume acct1-job165
```

Most of the 311 errors that run logged were this. Unlike the pause bug at the top of these notes it
does say so, in `errors.log` and in a desktop notification — but the session still sat there.

`exec.LookPath` answers the same question from PATH and PATHEXT inside the process, with no child at
all. The preference the old code applied turns out to be the same one: its regular expression
matched `.exe` and `.cmd`, which is what PATHEXT resolution gives you anyway. The finder stays as a
fallback for the case where `LookPath` comes up empty, so nothing that used to resolve stops
resolving.

## Windows paid for a PowerShell process on every pause, to delete nothing

**Product bug**, and the reason Windows is slow rather than merely slower. Cancelling a scheduled
runner ran `Unregister-ScheduledTask` unconditionally on Windows — a whole PowerShell process, half
a second to a second and a half of it, on every pause and again on every resume, whatever actually
scheduled the runner. When the backend is the sleeper, which it is whenever scheduled tasks are
unavailable or switched off, that task never existed. The command was there to delete nothing.

Worse, it is paid inside the *global* schedule lock, so it does not only cost the session paying
it. In the first Windows torrent this suite has ever run, 473 parked sessions queued their
PowerShell starts one behind another on that single lock. The run took 1537 seconds against 70 on
Linux, the worst hook took 51 seconds, and sleepers began giving up with `schedule.lock is held by
another process; not written`. On Linux and macOS `removeScheduledTask` returns at its first line,
so none of this was visible on either of them.

The wait record says which backend scheduled the runner. Where it says sleeper or manual, no task
was registered and the unregister is skipped. Where it says task — or where there is no record at
all, and something may be registered that nothing remembers — it still runs.

## And it listed every process on the machine to check one pid

**Product bug**, the same shape as the one above and found by following it.
Before killing a runner it recorded, the plugin checks that the pid still
belongs to one of its own helpers. That check is worth having — it exists
because a stored pid was once killed two days later with no identity check at
all. On Windows it ran `tasklist`, which enumerates every process on the
machine, with a five-second timeout, from inside the same global schedule lock,
every time a session with a live runner was parked again.

The check was right; its cost was wrong. `CreateToolhelp32Snapshot` and
`Process32Next` answer the same question in the process that is asking, and both
are in Go's standard library, so the build stays what the README promises. Unix
keeps `ps`, which costs about five milliseconds there.

Three of this round's findings share a shape worth naming. A cost that is
invisible on Linux is invisible in review: `removeScheduledTask` returns at its
first line, `notify-send` does not exist on the runner, `ps` is cheap where
`tasklist` is not. Each of the three had quietly settled inside a lock that
every session has to pass through, and only a Windows machine running the whole
suite could show it.

## What else a pause stopped paying for

Not bugs, but the same measurement pointed at what was left. Each of these costs the person on
Windows, where starting a process is the expensive part, and costs nothing anywhere else.

Registering a scheduled task was preceded by unregistering it. The registration already carries
`-Force`, which replaces a task of the same name, so the unregister was a whole PowerShell process
spent deleting something about to be overwritten — and spent inside the global schedule lock. It
is now skipped when a task is about to take its place, and the old task is removed there and then
if that registration fails, so a stale task still cannot outlive a failed replacement.

`git status --short` ran twice per pause: once for the checkpoint's list of changed files, once
for the fingerprint the wait records. Git starts slowly on Windows. The raw status is remembered
for two seconds per directory, long enough for one pause to read it once. It cannot hide a changed
worktree, because the fingerprint that matters is compared across two processes — at the pause and
again at the resume — and a memo inside one process never spans them.

No number is quoted for any of this, and that is deliberate. The plan these came from says a claim
of a speed-up has to show the figure before and after on the platform that pays it, and the
platform that pays it is a shared two-core CI runner: the same pausing-hook measurement came back
at 1157 ms and 2057 ms on two runs of identical code. A single comparison across that much noise
would prove nothing. What can be stated is what was removed — one PowerShell process and one git
process per pause — and those are counted, not timed.

A sleeping wait woke every fifteen seconds however far off its resume was, and each wake reads the
whole state file and the usage file. The default early-reset poll is five minutes, so nineteen
wakes in twenty could only notice a cancellation — which is delivered by killing the process
anyway — or usage another window had just refreshed. More than half an hour out, the wake now
relaxes to a minute, and never past `wait.earlyResetPollMinutes`: nothing is ever noticed later
than the person asked for it to be. Inside the last half hour the old rate returns untouched.

## A reset that was not early was announced as one

**Product bug**, small, and the kind this plugin exists to not do. A wait does not end when the
window resets; it ends a configured margin later. The watcher polls throughout, so a poll landing
inside that margin sees the window cleared — correctly — and the hook then told the person
"⚡ limit reset ahead of schedule" and wrote an early-reset entry into the decision journal. Nothing
reset ahead of schedule. The plugin was waiting out its own safety margin.

It is early now only when the window cleared before its own reset time. The shared predicate is
untouched, and that is the point: `windowClearedAt` answers whether the window has cleared, which is
what a sleeper needs in order to resume, and three of its unit tests said so when the first attempt
at this fix moved the comparison into it. The comparison belongs where the wording is chosen.

## The repair pass raced the hook it was meant to repair

**Product bug**, and a window this release opened for itself. Pausing now
registers the wait before it starts the runner — that ordering is what stopped a
sleeper being born into an empty `waits` map and exiting at once, which is the
failure at the top of these notes. The scheduled entry is therefore written a
moment later, in a second update, and in between the record sits on disk naming
no runner.

The repair pass reads exactly that shape and concludes a hook died mid-pause, so
it schedules a runner of its own. On Linux the gap is milliseconds and it
essentially never fires. On Windows, where starting a process is not free, it
does: one torrent logged it forty-three times on a single account. Two runners
for one wait is the family of bug 5.5.0 led with.

A wait younger than thirty seconds is left alone now, because a runner being
created is not a runner that failed to be created. A hook that really did die
between the two writes is still repaired, slightly later, and repair runs on
every statusline and every hook, so the delay is not something anyone sees. The
check was not weakened; its subject was corrected. Repair collects what a dead
hook left behind — it does not race a live one.

## A background process gave up on a lock that was only slow

**Product bug**, and half of a fix already made for the right reason. Waiting for the state lock
has two deadlines: how long the queue may take in total, and how long it may go without the lock
changing hands. The total cap already asked who was waiting — a hook has a host in front of it and
should give up, a sleeper has nobody and losing its write is the worse outcome — so the sleeper's
cap is fifteen minutes and the hook's two. The second deadline, ten seconds without progress, was
the same for everybody. Under a stall it fires first, and the sleeper loses exactly the write the
longer cap was written to protect.

A Windows torrent showed it plainly: twenty-three refusals, every one from a sleeper, all inside a
single 110-millisecond window, with the worst hook of that run at 49.6 seconds. That is not a
queue. It is one machine that stopped scheduling anything for most of a minute, and every process
waiting on a lock timing out together because the holder was not running either. The refused write
is the resume's — it clears the wait and consumes the checkpoint in one locked update — so dropping
it leaves a wait on disk for the repair pass to find later.

The no-progress deadline now asks the same question the cap does. A hook keeps its ten seconds. A
sleeper or a resume gets as long as a live holder is allowed to hold the lock at all, because past
that the lock is reclaimed and the wait ends either way: two seconds for a dead owner, 120 for a
live one. Waiting can never be unbounded.

Extending a wait means extending a poll loop, so the poll changed with it. Waiters checked every
fifteen to thirty-five milliseconds however long they had been there — the right rate for the
common case, and a thundering herd during a stall, on the machine that is already not keeping up.
The interval now grows with the wait: unchanged for the first two seconds, three times longer
after that, eight times longer past ten.

## The clock-skew check was measuring the hook, not the clock

**Product bug**, and a false statement to the person, in the same family as the early reset. The
check compared the server's `Date` header against the `now` its caller was holding — and that
`now` is the hook's start time, taken before the config was read and before the state lock was
waited on. So it did not measure a clock difference. It measured a clock difference plus
everything that happened in between.

A Windows torrent reported it once per account, at 33, 34 and 48 seconds. The fetch times out
after five, so no HTTP call can account for 48 of them; the gap came from outside the request. The
direction gives it away as well. Elapsed time is always positive, so this error could only ever
read "local clock is behind the server", never ahead, and a one-sided error is not a measurement.

It does not stop at a wrong warning. The offset is stored and added to every reset time and to the
ETA, so a phantom 48 seconds moves when the plugin believes a window reopens.

The fetch now stamps itself — the plugin's own clock when the request goes out, and the round trip
measured alongside it — and skew is the distance between the server's `Date` and the midpoint of
that interval. A round trip longer than the fetch timeout is treated as telling us nothing: the
offset already known stands, rather than being overwritten by a reading that cannot support it.

The first attempt at this fix was wrong, and the soak caught it before it went anywhere. It
stamped the request with the wall clock, which skips the offset the suite shifts to inject skew on
purpose. The injected skew stopped being seen, reset times stopped being adjusted, and the guard
let fifteen calls through at 100 % of a window whose threshold is 92 — the one outcome this plugin
exists to prevent, in a build that had already passed vet, unit tests, contract, chaos and lab.

## The rest were test assumptions, not product faults

Recorded individually, because "the test was wrong" is the easiest sentence in software to hide
behind:

- **The SHA256SUMS refusal could not pass on Windows, and the product was right all along.** The
  check tampers with the manifest, runs `ensure`, and asserted that the shipped `bin/noctis` and the
  platform binary now *differ* — a stand-in for "the shipped file was not overwritten". On POSIX
  `bin/noctis` is a shell launcher, so they always differ and the assertion is nearly free. On
  Windows `bin/noctis.exe` is a byte-identical copy of `bin/windows-amd64/noctis.exe`, so they never
  differ and the assertion could not pass no matter what the binary did. It now asserts the thing it
  meant: the shipped file's bytes *and* mtime are unchanged and the refusal names SHA256SUMS. That
  is stricter on POSIX too — the old form would have accepted an overwrite with any third content.
  The security check itself was verified correct on all three platforms and is unchanged.
- **macOS resolves `/var` to `/private/var`**, so a lab directory created under `os.tmpdir()` did not
  match the paths the binary reported back. The harness canonicalises its temp root once.
- **`LC_ALL` and `LC_MESSAGES` inherited from the runner** outranked the `LANG` the tests set, which
  is correct POSIX precedence and exactly what the plugin implements. The harness clears them.
- **The Windows fake `claude` ended on `timeout /t 1`**, which refuses to run with redirected stdin
  and leaves a non-zero errorlevel as the batch file's exit code. Every caller that read the shim's
  status saw a failure that had not happened. It sleeps with `ping` and exits 0, as the POSIX shim
  already did.
- **The large-transcript latency budget timed a whole `spawnSync`**, so it was measuring process
  creation as much as checkpointing, and Windows spends most of 1500 ms getting a process off the
  ground. It now times the same hook against a small-transcript baseline on the same machine and
  budgets the difference, which is the cost the check is about.
- **The hook-command assertion assumed POSIX quoting**, and the selftest assertion assumed a marker
  line the Windows path words differently.
- **`ps -o comm=` reported `sleep`, not the stand-in's name**, because `/bin/sh` execs a lone command
  in place of itself. And a reaped child still answers `kill(pid, 0)` on macOS, so the lab read a
  zombie as alive. The stand-in for a previous window is now a process the guard recognises as one.
- **Four checks slept a fixed number of milliseconds** and then asserted on state another process was
  still writing. They poll for the record they need, with a timeout, so a slow runner fails the
  check for being slow rather than for being scheduled differently.

## A test that cannot fail

The torrent scans each account for leftover `.lock` and `.tmp` files the moment its last job
returns, and failed on anything it found. That assumes the account has gone quiet, and it has not:
the sleepers earlier jobs detached are still running and still working, so one of them can be
holding `state.lock` for the millisecond the scan goes past. Under load it failed about one run in
three and said only that a file existed.

The first attempt at fixing it asserted that the lock must not *survive* the run. Six seeds that had
been failing passed — and that was the warning, not the result. A negative control threw it out: a
planted lock naming a dead pid also passed, because a passing sleeper reclaims a stale lock within a
second or two, so the check could no longer fail at all. It had stopped testing anything.

The question it meant to ask is whether anyone still holds the file. `state.lock` names its holder,
a staging file carries the writer's pid in its own name, and a stray is a fault when that process is
not running. A lock nobody holds fails at once; a lock a live sleeper holds for a moment does not.
Both directions are checked: planted strays naming dead pids fail, with the reason.

## Source hygiene, and the CI that could not have caught any of this

- Both workflows build with the same Go version the binaries were built with, `-trimpath
  -buildvcs=false` and `CGO_ENABLED=0`, so the committed binaries reproduce byte for byte. CI then
  compares `bin/` itself rather than the manifest describing it: a change to Go source that does not
  ship rebuilt binaries fails the build.
- `.gitattributes` normalises text to LF, so a Windows checkout no longer turns `lang.go` and the
  fourteen `i18n/*.json` catalogs into files the hygiene test reports as damaged — while
  `install.ps1` keeps its CRLF and all eight binaries stay byte-identical.
- The executable-bit check reads the git index instead of the filesystem, because Windows has no
  such bit to read.
- The lab job installs Go. It had been running a suite that needs a Go toolchain without one.
- The release workflow staged six platform binaries under two file names and clobbered four of them.
  Each is now staged under a unique name, with its own checksum manifest.

## Documentation

The two READMEs fold their long sections and hand the test detail to `docs/TESTING.md`. Nothing was
removed and every heading kept its anchor. The comparison table is now a section of its own.

## If you are upgrading

Nothing to do. No configuration changed and no exit code moved. If you run Windows and have ever
found a session still sitting at a wall in the morning, that is the bug at the top of these notes;
if you run several sessions at once on any platform, the lock and read fixes are why a record you
watched the plugin make will still be there later.

## Still open, deliberately

- **launchd and schtasks have still never fired on a real Mac or Windows box.** What changed is that
  the suite now genuinely runs on Windows and macOS runners on every push, which is how every
  product bug in these notes was found. A CI runner is not somebody's laptop waking at 4 a.m., and
  that evidence is still missing.
- **The torrent no longer runs on Windows.** It parks several hundred sessions at once, and on a
  two-core runner that measures the runner rather than the plugin: the last Windows run of it had
  a perfect guard — 494 stopped before the wall, 494 parked, nothing allowed past the threshold —
  and still failed, on two hooks that gave up on the state lock and one rename that fell back to
  writing in place, all of them the designed behaviour under saturation, with the worst hook at 59
  seconds. It earned its keep first: every Windows-only bug above was found by that step and by no
  other. Windows keeps hygiene, the host contract, chaos, lab, soak and monkey; Linux and macOS
  keep the torrent. This is a real loss of coverage and not a tidy-up.
- **Pausing a session still costs seconds on Windows.** The hook that parks a session builds a
  checkpoint, registers the wait, schedules the resume runner and raises a desktop notification.
  Three of those start processes, and starting a process is the expensive part on Windows; on Linux
  the same hook takes 15 ms, partly because the runner has no `notify-send` at all. This release
  removes two of the starts — the unregister before a forced re-register, and the second
  `git status` per pause — and leaves one. An earlier draft of these notes said the notification
  makes the person wait for it; that was wrong, and checking the code rather than repeating the
  claim is what found it: `notify` already starts the process detached and releases it without
  waiting. What the hook pays is one `CreateProcess`, which cannot be removed without removing the
  notification. The soak measures the two costs separately rather than averaging them, and each
  platform is now held to what a pause actually costs there: 400 ms everywhere except
  Windows, which gets 5000 because three process starts on a shared runner do not fit in
  less. The single 3000 ms figure that covered all three was a hundred times too loose on
  Linux and macOS and inside the noise on Windows.
- **The binaries are unsigned.** The procedure and the dormant CI steps exist; the certificates do
  not.
