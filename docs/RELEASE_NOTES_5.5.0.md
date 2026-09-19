# noctis 5.5.0

This release came out of three things: running thousands of sessions through the plugin at once,
reading the code for what nothing had ever executed, and then asking the more uncomfortable version
of that question — which of the things the code *claims* are true does anything actually check.

Almost every failure below is the same shape. The plugin looking healthy while doing nothing.

## The one that matters

**A paused session could be parked against a dead runner, and every surface said it was fine.**

Every path that pauses a session schedules the relaunch first and then records the wait. The
scheduler writes the new runner onto the wait record that is still in state; the wait recorder then
cancelled "the previous runner" by reading that same record — and killed the runner that had been
started milliseconds earlier.

What the person saw afterwards: `noctis status` showing a wait with a resume time, a clean
`errors.log`, `noctis doctor` reporting nothing, and the repair pass skipping the session because
its record *has* a scheduled entry. It is simply a corpse. Nothing ever came back.

On Windows it was worse. The cancel removes the scheduled task by its deterministic name
unconditionally, so even a first pause deleted the task it had just registered.

The lab already ran this exact scenario and asserted only that the *old* runner had died. Nobody
had asked whether the new one was alive. That missing half is what hid it.

## The guard reading its own usage data wrongly

Three separate ways the one input the whole plugin depends on could be misread:

- **A usable percentage sitting next to an unusable one was thrown away.** The two shapes of usage
  payload were read by two parsers that disagreed about which key won — one preferred `percent`,
  the other `utilization` — and both stopped at the first key that was *present* rather than the
  first that was usable. A payload carrying `"percent": null, "utilization": 87` has the answer
  right there; both parsers dropped the window entirely, and a window the guard cannot see is one
  it cannot defend. One list of key names, tried in one order, until one of them parses.
- **An error the plugin merely remembered was read as a live failure.** `fable.json` keeps the last
  refresh error forever, and the refresher returns that cached record whenever it declines to
  fetch — inside the poll window, during backoff, when another process holds the lock, or when the
  usage source is switched off. Callers read the old string as "the refresh just failed". Near a
  threshold that sends the session into a blind probe: five sleeps inside a single hook. With the
  source switched off it froze permanently — nothing ever fetched again, so nothing ever cleared
  it. An error now counts as live only while its own backoff is still running.
- **The blind probe could release a session on data that never arrived.** It asked "is there no
  error" when it should have asked "did anything new come back": with another process holding the
  fetch lock it got the cached record, whose error had long since aged out, and treated that as a
  successful probe.

## Two answers to "which model is this session on"

Two functions answered that question and could return different names, because only one of them
knew about the plugin's own record that it had moved the account to the fallback model. The trigger
is `settings.json` having no model of its own — exactly what the plugin leaves behind when it
removes a fallback the host rejected.

The guard then measured the scoped threshold against a model the session was not using: `noctis
check` returned 11 ("a threshold is reached") for an account sitting on a model that does not draw
on that window at all, and the hooks told the person to switch to the model they were already on.

Underneath it was a smaller bug doing real damage: **a session's model override expired after three
days while a weekly park lasts seven**, so a session parked at a weekly wall came back having
forgotten which model it was on — which is what made the two answers diverge in the first place.
There were also two pruning loops over the same state, and the older one contradicted the comment
on the newer one. One resolver now, one pruning loop, and the override outlives the longest wait.

## Capabilities the plugin declared and never checked

Each supported host is described by a set of capability flags. Two of them — "this host has a
switchable default model" and "this host reports API errors to a hook" — were written down, set per
host, and read by nothing.

That is not untidiness. On three of the five hosts the plugin wrote a `model` key into a
`settings.json` those hosts do not read, then **read it straight back and believed it**: it
reported the session as running on a model nothing had switched. It could reach that state without
any of it being true, because a host whose status line reports no model name falls through to the
configured primary, which the scoped pattern matches by default. The fix is not only to consult the
flag but to do something sensible when it is false: where the remedy does not exist, a full scoped
window is not special — it parks the session until it resets, like any other full window.

A test now fails the build if a capability is added and never consulted.

## Commands that showed you things the engine would refuse

Pruning only happens when state is written, so every read-only surface was looking at records the
engine had already discarded. Validity was a predicate each reader had written out for itself.

- **`noctis checkpoint` printed checkpoints that had already been used.** A resume marks a
  checkpoint consumed but deliberately leaves the file on disk for a week, so for that week the
  command would print a complete resume document — `claude --resume <sid>` and all — for a session
  the engine would refuse to restore, with nothing in the output to say so. It also printed
  expired ones. `--sid` replaced the eligibility rules rather than narrowing them.
- **`noctis status` and the status line promised resumes that were never coming**, listing waits
  the engine had already classified as abandoned.
- **`noctis doctor` could not see that the guard was switched off.** It read settings, config,
  usage and the error log, but never state — so after `noctis off 60` it printed "all good" and
  exited 0 for an hour while nothing was protecting the account, contradicting `noctis status`,
  which does report it. **`noctis selftest` was worse: its probe hook exited 0 *because* the guard
  was disabled, and that was scored as a passing check.** A switched-off guard produced a green
  self-test.
- **`noctis queue status` reported no queue** for a session being driven by an auto-queue, because
  it resolved the file differently from the engine.

There is now one shared answer to "is this checkpoint usable" and one to "is this wait still live",
and the commands ask those instead of carrying copies.

**The queue trust gate was leaking through the checkpoint.** A `TASKS.md` that arrives with a
repository needs `noctis queue trust` before it can direct a session — but the checkpoint copied
that untrusted checklist's items into its own markdown, which is a document whose entire purpose is
to be handed back to the model on resume.

## Reading the end of a file

Three implementations, each with its own handling of a short read and of EOF, two of them wrong:

- Every truncated file in a bug-report bundle **started mid-line**, because the trim meant to
  prevent that sat behind a condition the reader itself made impossible. For `decisions.jsonl` that
  is a broken JSON record as the first thing anyone reads.
- A file that shrinks between the stat and the read — `guard.log` rotates at half a megabyte — left
  the unread remainder of the buffer as NUL bytes, which were then split into "lines".
- One reader compared an error's *text* against `"EOF"`, so a wrapped EOF was treated as a hard
  failure and the transcript summary came back silently empty.

## Install

**Installing the plugin now protects you.** Until this release, `/plugin marketplace add` +
`/plugin install` left it inert: the tree was in place and the hooks were wired, but with no config
and no status line the guard had no usage data at all — at 95% it said nothing, and everything
looked installed. The first session now writes the config and wires the status line (chaining any
status line already there, and handing it back on uninstall).

**The install is 7.5 MB instead of 48 MB**, 16 files instead of about 130. It used to copy the whole
repository into your account. Also: running `noctis ensure` from a clone no longer replaces
`bin/noctis` — the launcher script in the repository — with an 8 MB binary in your working tree.

## Silent failures, closed

- **A status-line payload whose shape changes** used to freeze usage at its last reading *with a
  fresh timestamp*, so the staleness rules never fired. The timestamp is no longer advanced when
  nothing could be read, and you are told once a day.
- **A single torn read of `state.json`** restored the backup and discarded every record written
  since, from a read path, with no lock. Reads now retry before concluding the file is broken.
- **`(after #tag)` released after the first tagged item was ticked**, not the last.
- **A plain bullet in a checklist file was glued onto the item above it**, because the rule meant to
  stop that could never fire.
- **`report` output was not reproducible**: `pricing` resolved differently from run to run over a
  Go map, and the per-model ranking used an unstable sort with a comparator that can tie, so two
  models with equal totals swapped places between runs.
- **A version component that failed to parse silently became 0**, which would read `2.1.251-beta`
  as 2.1.0 and report a Claude Code newer than the tested minimum as too old.
- **A machine with no desktop notifier** wrote a line to `errors.log` on every alarm.

## Speed and size

Hook latency scales with the size of `state.json`, which nobody had measured: 1 KB of state is a
5 ms hook, 313 KB is a 31 ms hook. Eight per-session collections had no expiry at all and grew for
as long as the plugin was installed. The transient ones are now pruned; the records that are the
plugin's *memory of a decision* are kept, and the one that has to survive a weekly park is kept for
longer than the longest wait.

## Languages

**All fourteen are complete** (2 868 new strings). They were 2 complete and 12 at 18%. The catalogs
moved to `i18n/<code>.json` and `lang.go` is generated from them, so adding a key is one edit rather
than twelve, and a Go test fails the build if any language is missing a single message.

Writing them found two real bugs: Japanese and Chinese reordered a sentence's arguments without
using Go's explicit indexes, so at runtime a token count would have printed where a model name
belonged. Nobody reading those languages would have reported it as a bug.

## Tests

Four new suites, each asking something the others could not:

- **`tests/contract.js`** runs every hook in `hooks/hooks.json` the way Claude Code runs it. The
  rest of the suite pipes payloads straight into `noctis hook`, which is the one way the host never
  calls it.
- **`tests/torrent.js`** drives thousands of long sessions at once and asks the two questions only
  volume answers: does the guard ever let a session past the wall, and what does it cost per hook.
- **`tests/chaos.js`** injects the failures a real machine produces, including a genuinely full
  filesystem — a `chmod`-based "unwritable" test proves nothing when the process is root.
- **`tests/scheduler.js`** runs the OS scheduler path, which nothing had ever run. A strict
  stand-in for `systemd-run` goes on PATH, the plugin's *own* backend detection picks it, and a
  parked session really is relaunched by the timer with nobody watching. The launchd plist is
  validated by Python's `plistlib` — Apple's own parser. Both scheduler bugs fixed this round are
  caught by it when reintroduced.

`noctis schedule-preview` is new and came out of that work: it prints exactly what would be
registered with the OS scheduler for a session, and registers nothing.

**On coverage: the 13.5% quoted in an earlier draft of these notes was wrong, or rather it was
answering a question nobody cares about.** That is what `go test` alone reaches, and almost nothing
in this plugin is decided in a Go unit test — the decisions are made by a compiled binary that the
lab runs as a subprocess, exactly as Claude Code does, and none of that reaches the unit-test
counters. Measured properly, by building the binary with `go build -cover` and running the lab
against it, **the suite reaches 80.5% of statements**. `node tests/coverage.js` reproduces that
number and will list every function no test reaches.

Three structural tests now fail the build on dead code: a capability flag nothing reads, a struct
field set and never read, a parameter passed and discarded. All three were introduced after finding
live bugs of exactly those shapes.

The release workflow's checksum step could not reproduce the committed manifest and would have
failed on every release; it now calls the same generator the repository uses.

## If you are upgrading

Nothing to do. Three behaviour changes worth knowing:

- A `TASKS.md` that arrived with a repository needs `noctis queue trust` once before it drives a
  session (this landed in 5.4.0 and was missing from the README until now).
- `noctis doctor` exits 1 when it has something to report, including when the guard is switched
  off — so a cron or CI job can read it.
- An account currently sitting in the diverged model state will see `noctis check` flip from a
  false 11 to 0. That is the fix, but it changes an exit code an existing CI gate may be reading.

## Still open, deliberately

- **launchd and schtasks have still never fired on a real Mac or Windows box.** The systemd path is
  now proven end to end, the plist is validated by Apple's own parser and the task script by
  parsing it — but a stand-in is not the scheduler, and this is the gap where both of this round's
  scheduler bugs lived. CI runs the suite on all three platforms; a real relaunch on a real Mac is
  still the missing evidence.
- **The binaries are unsigned.** The procedure and the dormant CI steps exist; the certificates do
  not. `docs/PUBLISHING.md` §7 has what to do when they are bought, and both READMEs tell the
  person what they will see in the meantime.
