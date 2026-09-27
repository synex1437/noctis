# noctis 7.3.1

7.3.1 fixes six bugs that a black-box test of noctis in a Claude Code on the web session turned up.
Three are in the checklist noctis makes from a long prompt: a prompt that said not to do the work
was driven through it, list items were dropped by their first word, and steps past the 40th were
lost without a word. A Claude Code on the web session now comes back from a usage limit by itself, a
queue that stopped making progress is no longer pushed again after every give-up, and an empty
checklist line is no longer handed to Claude as an item. Each fix came with a test that fails on
7.3.0. Hooks also cost less: the launcher of a marketplace install starts no other program before
noctis on Linux and one on macOS, and a write of noctis's state parses and encodes it once.

## If you are upgrading

There are no new settings, and an existing `config.json` works as it is. Three things behave
differently:

- A long prompt that tells Claude not to do the listed work yet, or asks only for a plan, an
  estimate or a review, no longer becomes a checklist; `noctis why` shows `no-auto-queue` with the
  words that held it back. A rule on how to do the work ("don't commit") keeps the checklist.
- After noctis gives up on a queue that stopped making progress, it stays out of that session until
  an item in the queue file is ticked, added or edited, or you type a prompt.
- In a Claude Code on the web session noctis no longer asks for the status line. It says once that
  usage limits cannot be tracked there.

## The fixes

**A prompt that said not to do the work was driven through it.** "Do NOT implement any of the
following right now and do not touch any files; just estimate how long each would take", followed by
a list, became a checklist, and the Stop hook then drove Claude through the work the prompt had
forbidden. The Turkish form did the same. The words around the list are now read first: when they
forbid the listed work (do not, don't, never, hold off; yapma, dokunma, uygulama, …) or ask only for
a plan, an estimate, a review or an explanation, no checklist is made. The phrase lists cover all 14
languages noctis speaks. A rule on how the work is done ("don't commit", "don't touch the migrations
folder", "başka bir şeye dokunma") keeps the checklist.

**List items were dropped by their first word.** A listed line was left out when its first word was
a "descriptive" starter in any language, and some of those are ordinary first words in another:
Turkish items that started with En, Bu, Şu or Mevcut and English ones that started with To, Ten, A
or Die could leave the checklist without a word. The starters now come from the prompt's own
language only, and a line that ends in a work verb (Turkish, German and Dutch put the verb last) or
whose main clause after the first comma leads with one is a step. A listed line that is still not
read as a step is no longer lost: the notice names it, Claude is told about it, and the checklist
file keeps it under `## Not on the checklist`, where it does not drive the session. A closing "Can
you do these?" no longer makes the prompt a question, and a comma-chained paragraph ("first …, then
…, after that …, and finally …") whose every part reads as a step is split into its steps.

**Steps past the 40th were lost without a word.** A 45-step prompt became a 40-item checklist, and
steps 41 to 45 were gone. Now you and Claude are told how many steps the prompt lists and how many
were left out, the checklist file keeps them at its end under `## Not on the checklist: steps over
the 40-item limit`, `noctis why` counts them, and the notice that the checklist is done names them
again.

**A Claude Code on the web session never came back from a usage limit.** In a cloud session
(`CLAUDE_CODE_REMOTE=true`) no status line reports usage, so every limit stop had an unknown reset.
The wake in place needed usage data and never ran, and the resume was left to a runner that
relaunches `claude` in the container, which is not the session you watch. Every such session was
also asked whether the status line was installed, which it cannot be there. noctis now starts no
runner, reset watcher or relaunch there: the StopFailure hook waits and wakes the same session in
place, for an unknown reset too, on the `wait.retryMinutes` steps for as long as `wake.maxMinutes`
allows from the first failure, then gives up and says to type a prompt. `noctis why` shows
`cloud-wait`, or `cloud-no-wake` with the reason, and the first prompt of the session is told once
that usage limits cannot be tracked in a cloud session. A session woken after a retry for an unknown
reset, locally too, is now told `Retry N after the session stopped on <error> (waited <time>)`
instead of an unknown limit reset at 0 %.

**A stuck queue was pushed again after every give-up.** When a queue stopped making progress, noctis
let the session stop but also reset its count, so the next stop of that session, after a wake in
place for example, drove the same stuck queue through `queue.maxIdleContinues` more blocks. The
give-up now holds: later stops on the same file are let go (`queue gave up earlier and is unchanged`
in `noctis why`) until an open item is ticked, added or edited, or a prompt arrives, typed or sent
by a relaunch. The notice says what drives the queue again.

**An empty checklist line was handed to Claude as an item.** A `- [ ] ` or `TODO: ` with nothing
after it counted as an open item: the Stop hook handed Claude an empty item and held back any item
that waited on it with `(after N)`, and `/noctis:start` counted it as a job and started a queue from
a file that held nothing else. An empty line is now skipped and named once by its line number, in
the notice and in Claude's directive; `noctis queue status` and `trust` name it too, and
`/noctis:start` leaves it out of the job count and names it by its line in the file.

## Faster hooks

**The launcher starts no other program on Linux.** A marketplace install runs noctis through the
`bin/noctis` sh launcher on macOS and Linux, which forked four times and started three programs
(`dirname` once and `uname` twice) before noctis itself: about 6 ms of the 10 to 12 ms a hook took
on Linux. It now reads the OS and CPU from the kernel (Linux 6.1 or later) or from bash's `OSTYPE`
and `HOSTTYPE` (an older Linux kernel, Git Bash, Cygwin), and asks `uname` once only when neither
answers. macOS always asks it once: its `/bin/bash` is one build for both CPUs, so its `HOSTTYPE`
may name the other one. On Linux the launcher now adds about 1.5 ms where it added about 6: through
it, a `UserPromptSubmit` hook takes about 7 ms (11.5 ms with 7.3.0's launcher, 5.5 ms run directly)
and a `Stop` hook about 5.5 ms (10.5 and 4 ms).

**A state write parses and encodes the state once.** Each write of `state.json` parsed the file it
replaced a second time to decide whether to back it up, and encoded the new state twice. With a
264 KB state, a `UserPromptSubmit` hook that changes it now takes about 23 ms where it took 30
(Linux, noctis run directly, medians of 60 calls); a hook that leaves the state as it was, and a
small state, gain little. A `state.json` that an editor saved with a byte order mark is now backed
up like any other.

## Known limits

- In a Claude Code on the web session no status line reports usage, so noctis cannot pause before a
  limit there; it only brings the session back after a limit stops it. A reset later than
  `wake.maxMinutes` (about 5½ hours) is not waited for: type a prompt once the limit has reset.
- Whether a prompt holds its listed work back is read from phrase lists. A prohibition worded in a
  way the lists do not know still becomes a checklist; `/noctis:stop` ends it.
- The limits listed for 7.3.0 still apply ([RELEASE_NOTES_7.3.0.md](RELEASE_NOTES_7.3.0.md)).

## Tests

Every fix came with Go tests that fail on 7.3.0: twenty of them, beside two that hold what must not
change (a list without a hold-back, and a job prompt with a rule on how to do it, still get their
checklist) and two that check the new phrase lists are written as they are matched and that a huge
prompt is still read quickly. The lab drives the built binary through each fix, and the auto-queue
fuzz target's seeds now cover held-back prompts, lines left out and comma chains. The contract
drives the launcher with the kernel's, bash's and `uname`'s answers set by the test and counts the
`uname` calls, and a Go test holds what a state write backs up and writes. Local round on the
release tree, on Linux with claude 2.1.283, Go 1.24.7 and node 22: go test with Windows and macOS
builds and vets, go test -race once, 30 seconds of each of the five fuzz targets, hygiene, i18n,
contract, chaos, scheduler, lab, a two-day hard soak, the torrent and two monkey seeds.
