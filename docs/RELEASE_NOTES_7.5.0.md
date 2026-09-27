# noctis 7.5.0

7.5.0 takes the waits out of hooks. A hook that found its usage reading due asked the usage endpoint
itself, and Claude Code waited for the answer: 150 to 400 ms against the real endpoint, and the full
5 s request timeout when it hangs. Now a detached process asks, the hook decides on the reading it
has, and it waits only close to a limit, for at most 1.5 s. Hooks also read and write their JSON
with noctis's own codec, a status line refresh reads `state.json` without copying it, hooks start
without threads they do not need, and on macOS the launcher no longer starts `uname` for every
hook. With a 77 KB `state.json`, through the launcher, a `UserPromptSubmit` hook takes 7.65 ms
instead of 8.63, a `Stop` hook 6.84 instead of 7.83 and a status line refresh 6.49 instead of 8.85.
7.5.0 carries everything in 7.4.0 ([RELEASE_NOTES_7.4.0.md](RELEASE_NOTES_7.4.0.md)).

## If you are upgrading

From 7.3, the upgrade notes of 7.4.0 apply too
([RELEASE_NOTES_7.4.0.md](RELEASE_NOTES_7.4.0.md#if-you-are-upgrading)). 7.5.0 adds no settings,
and the files noctis keeps are read and written as 7.4.0 reads and writes them.

- **macOS has one binary.** `bin/darwin/noctis` holds the builds for Apple silicon and Intel Macs,
  and macOS runs the one for its CPU; `bin/darwin-amd64` and `bin/darwin-arm64` are gone. A
  marketplace install needs nothing: its launcher runs the new file, and the status line is
  re-pointed as after every update. A copy made by `scripts/install.sh` keeps running its old binary
  until you run the installer again, as after every update; that run copies `bin/darwin/noctis`,
  points the hooks at it and removes the old folder from the copy.
- **Release downloads.** The macOS binary is published as `noctis-darwin-universal`, and, so that
  links made before 7.5.0 keep working, also as `noctis-darwin-amd64` and `noctis-darwin-arm64`: the
  same universal file under all three names.
- **`GOMAXPROCS`.** The launcher sets it to 1 for a hook or a status line refresh when it is not
  set, and noctis takes it out again before it starts anything else. A value you set yourself is
  left as it is.

## A hook does not wait for the usage endpoint

A hook or status line refresh that finds a usage fetch due takes `fable.lock`, starts a detached
`noctis refresh`, hands it the lock and decides on the reading it has. The refresher asks the
endpoint, writes `fable.json` and lets the lock go, so the answer is there for the next hook, and
while it asks, `fable.lock` still tells every other process that a fetch is in flight. A hook that
finds one in flight does not start another.

A hook waits for the answer, up to 1.5 s, only where deciding on the reading it has could be wrong:
when it has no reading, or one older than `usage.staleMinutes`; within 8 points of a pause point or
of the 100 % stop; before a check noctis makes once, such as a workflow launch or the check after an
API error; at session start near a pause point; and when a subagent starts near the Fable switch
point. It stops waiting at once on a lock that no fetch stands behind, one whose holder is gone or
that names no process. The background processes (the runner, the sleeper, the blind probe) still ask
the endpoint themselves, and so does a hook that cannot start the refresher.

In the lab, against an endpoint that takes 4 s to answer, a hook whose reading is due returns in
9 ms, and one with no reading at all in 1.5 s; 7.4.0 took more than 4 s in both.

**A lock that names no process goes after 2 s.** Every noctis process writes its pid into a lock as
it takes it, so a lock whose content is not a pid was left by nothing that still runs. It was kept
for 15 s, and a hook or status line refresh waiting on it gave up only after 10 s; it now goes after
the 2 s a lock whose holder died gets. An empty lock keeps the 15 s, since that is also what a lock
looks like between being made and getting its pid.

## Faster hooks

**A JSON codec of its own.** Every hook parses what Claude Code hands it and prints its answer as
JSON, and most read `state.json`, `usage.json`, `fable.json` and the config. `encoding/json` did all
of it through reflection, and with a busy state, parsing and copying `state.json` was the
largest part of a `Stop` hook or a status line refresh. noctis now reads and writes all of these
with a codec of its own, and reads transcript lines, `decisions.jsonl` and the `state.json` a write
is about to back up with it too: one pass over the bytes, no reflection, and exactly the values and
the bytes `encoding/json` gives, key order and escapes included, so no file changes by a byte. What
it cannot vouch for, a syntax error, a number out of range or a type it does not know, goes to
`encoding/json` whole, and errors and odd values come out as they always did. Measured with the Go
benchmarks on an 85 KB state, with `GOMAXPROCS=1` and the garbage collector set as hooks run it
(`GOGC=400`), medians of five runs:

| | `encoding/json` | 7.5.0 |
| --- | --- | --- |
| parse | 1.03 ms, 10 205 allocations | 0.40 ms, 4 424 allocations |
| compact write (`state.json`, a hook's answer) | 0.95 ms, 7 836 allocations | 0.27 ms, 1 allocation |
| indented write (`fable.json`, the config) | 1.63 ms, 7 860 allocations | 0.32 ms, 1 allocation |

**A status line refresh copies no state.** noctis keeps what it parsed of a file for the rest of the
process, and a reader that may change the state gets a copy of its own. A status line refresh read
the state four times: the line itself looked at the kept parse, and the early-resume check, the
repair of stranded waits and the day's budget record each took a copy, though only the repair
changes what it reads, and only to clear a hand-off whose relaunch process is gone. They now look at
the kept parse too, and the repair takes a copy only when it finds such a hand-off. Measured over
300 calls against the commit before, the binary run with the launcher's `GOMAXPROCS=1`: with the
77 KB state a refresh takes 5.18 ms instead of 6.58, and a `SessionStart` resume, which runs the
same repair, 5.99 instead of 6.47.

**No threads for idle CPUs.** The Go runtime starts a program with one scheduler per CPU and wakes
threads for them as it boots, though a hook is over in milliseconds. The `bin/noctis` launcher now
starts hooks and status line refreshes with `GOMAXPROCS=1`, so no such thread is started, and marks
that the value came from it: noctis takes both out of its environment first thing, so Claude Code,
git, a chained status line or a detached noctis started from a hook run with the defaults. The
value has to be there when the process starts: set from inside noctis it comes after the threads
and measured slower. It saves 0.15 to 0.26 ms a hook (medians of 300 calls with the 77 KB state of
the measurements below).

**Less work at start.** Every noctis process built the router's word-list patterns as it started, a
third of the start-up work of the main package, though most hooks never use them; they are now built
on first use, and the package's start takes 0.13 ms instead of 0.22. Loading the config no longer
builds the number pattern for the compaction keys that hold text, and a prompt without a code fence
no longer builds the fence pattern. Of the hook events and the status line, only a
`UserPromptSubmit` whose prompt holds a fence, or a hook that goes on to other work that needs a
pattern, now builds one.

**macOS: no `uname` for every hook.** `/bin/bash` on macOS is one build for two CPUs, so the
launcher could not trust bash's CPU and started `uname` on every call to choose between the two
macOS binaries. With one binary for both it needs no CPU: when the shell's `OSTYPE` says `darwin`,
as the `/bin/sh` of a Mac sets it, it runs `bin/darwin/noctis` without starting another program.
Without `OSTYPE` (a Mac whose `sh` is dash) `uname -s` answers once. Measured on Linux with the
launcher acting out macOS and `/bin/true` in place of the binary, a call took 3.05 to 3.42 ms where
7.4.0's launcher took 4.49 to 4.81.

## Measured

On Linux with 4 CPUs, 300 calls of each event alternating between 7.4.0 and 7.5.0, each with its own
account and a local stand-in for the usage endpoint, the early-resume check on; medians. The 77 KB
`state.json` holds 56 checkpoints and the records of some 200 sessions, written without indentation
as 7.4.0 writes it; the small one holds 648 bytes. Through the launcher, as a marketplace install
runs hooks on Linux and macOS:

| | 7.4.0, 77 KB | 7.5.0, 77 KB | 7.4.0, 648 B | 7.5.0, 648 B |
| --- | --- | --- | --- | --- |
| `UserPromptSubmit` | 8.63 ms | 7.65 ms | 5.83 ms | 5.47 ms |
| `PreToolUse` Bash | 5.49 ms | 5.13 ms | 5.58 ms | 5.15 ms |
| `PreToolUse` Edit | 5.80 ms | 5.39 ms | 5.68 ms | 5.26 ms |
| `PostToolBatch` | 5.86 ms | 5.43 ms | 5.69 ms | 5.25 ms |
| `Stop` | 7.83 ms | 6.84 ms | 5.62 ms | 5.39 ms |
| `SessionStart` (resume) | 8.38 ms | 7.00 ms | 5.75 ms | 5.35 ms |
| status line | 8.85 ms | 6.49 ms | 6.04 ms | 5.61 ms |
| `noctis version` | 5.47 ms | 5.25 ms | 5.79 ms | 5.53 ms |

Run directly, as a clone install runs them, without `GOMAXPROCS=1`:

| | 7.4.0, 77 KB | 7.5.0, 77 KB | 7.4.0, 648 B | 7.5.0, 648 B |
| --- | --- | --- | --- | --- |
| `UserPromptSubmit` | 7.75 ms | 6.89 ms | 4.93 ms | 4.67 ms |
| `PreToolUse` Bash | 4.45 ms | 4.26 ms | 4.51 ms | 4.25 ms |
| `PreToolUse` Edit | 4.90 ms | 4.60 ms | 4.85 ms | 4.59 ms |
| `PostToolBatch` | 5.12 ms | 4.74 ms | 4.74 ms | 4.47 ms |
| `Stop` | 6.92 ms | 6.09 ms | 4.93 ms | 4.59 ms |
| `SessionStart` (resume) | 7.29 ms | 6.04 ms | 5.00 ms | 4.67 ms |
| status line | 7.76 ms | 5.54 ms | 5.44 ms | 5.10 ms |
| `noctis version` | 4.54 ms | 4.28 ms | 4.80 ms | 4.60 ms |

`noctis version` does no more than load the config, so it shows about what starting a process costs
here: 4.3 ms run directly and 5.3 ms through the launcher, which does not set `GOMAXPROCS=1` for it.
A `PreToolUse` or `PostToolBatch` hook, which does not read the state, is within 0.5 ms of that;
with the 77 KB state a status line refresh takes 1.2 to 1.3 ms more, a `Stop` hook 1.6 to 1.8 and a
`UserPromptSubmit` hook 2.4 to 2.6.

## Also in 7.5.0

- **`noctis selftest` takes 50 ms instead of 3 s** on Linux and macOS: it looks for the mark of its
  background run every 20 ms instead of sleeping 3 s first, and still gives up after 3 s. On
  Windows it looks every 250 ms over the same 120 s, and reports as soon as the scheduled task ran.
- **An install over an older copy removes the old macOS folders** from that copy
  (`bin/darwin-amd64`, `bin/darwin-arm64`), which nothing runs once the hooks point at
  `bin/darwin/noctis`.

## For contributors

- `node scripts/build.js` builds every platform the way CI does, all at once, joins the two macOS
  builds into `bin/darwin/noctis` and regenerates `bin/SHA256SUMS`; `--host` builds only this
  machine's binary. The universal file is the two builds byte for byte behind a header, the same
  file on any machine from the same Go, and the ad hoc signature Go gives the arm64 build stays
  valid inside it.
- `node tests/gotest.js` runs the Go tests in parallel processes of one test binary, balanced by
  `tests/gotest-durations.json`: about 20 s on 4 CPUs where `go test ./...` takes about 4 minutes.
  `--run`, `--race`, `--cover` and `-v` work as in `go test`, and `--record` rewrites the durations
  after a run in which every test passed.
- CI runs every job at once: the binary check, the Go checks on three systems, two fuzz parts that
  find their targets with `go test -list` instead of naming them, and two lab groups per system
  that run the binaries committed in `bin/`, as an install does.

## Not in 7.5.0: a cached status line

A status line refresh that answers from a cache when nothing changed was profiled and left out.
Composing the line is a small part of a refresh; the rest has to run every time, since a refresh is
where noctis records what Claude Code hands it in `usage.json`, resumes a wait whose window reset
early, and checks the waits and the locks. On the 77 KB state about half of a refresh went to
reading `state.json`, which 7.5.0 parses with the codec above and no longer copies: run directly, a
refresh takes 5.54 ms instead of 7.76, of which 4.3 ms are starting the process.

## Known limits

- Windows runs `bin/noctis.exe` without the launcher, so its hooks start with the runtime's default
  number of threads.
- A status line that runs the platform binary directly, as `noctis setup` wires it, does not get
  `GOMAXPROCS=1` either; one that `ensure` re-pointed to `bin/noctis` after an update does.
- On a Linux kernel older than 6.1, whose `/proc/sys/kernel/arch` the launcher cannot read, a shell
  that inherits an exported `OSTYPE` starting with `darwin` makes the launcher pick the macOS
  binary, which Linux cannot run. Shells set `OSTYPE` without exporting it.
- CI runs `bin/darwin/noctis` on Apple silicon only: its x86_64 build is run there with
  `arch -x86_64` when the runner has Rosetta, and checked everywhere by hygiene's look at the file.
  No Intel Mac ran 7.5.0.
- The limits listed for 7.4.0 still apply
  ([RELEASE_NOTES_7.4.0.md](RELEASE_NOTES_7.4.0.md#known-limits)).

## Tests

Seven new Go tests hold the fetch aside: a hook whose reading is due hands the fetch off and does
not wait for the endpoint, a hook with no reading waits for the fetch only up to its bound and
decides on the answer when the endpoint is quick, a hook near an edge decides on the answer it
waited for, hooks that find a fetch in flight start no second one, a refresher fetches only with the
lock handed to it, and a wait for a fetch in flight ends at once when no fetch stands behind the
lock. Another sees a lock that names no process taken over as soon as one whose holder died.

Fifteen hold the codec to `encoding/json`: the values, numbers, depth and long documents it reads,
text it decodes that shares no memory with the input, the numbers, text, values, key order and depth
it writes, the JSON helpers and the hook input as they behaved before, the copies noctis keeps,
every JSON file in the repository and an 85 KB state shaped like a busy one. Two fuzz targets, which
CI runs for 30 s each, do the same for any input: `FuzzJSONFastDecode` wants the same value or the
same error, and `FuzzJSONFastEncode` the same bytes, compact, indented and with HTML escaped, for
values built from its input.

Four more: noctis hands on no `GOMAXPROCS` the launcher set but keeps a user's, the repository ships
this platform's binary where installs look for it, an update over a copy made before 7.5.0 leaves no
per-CPU macOS binary in it, and a status line repair clears a dead hand-off from a copy of its own,
leaving the parse other readers look at as it was. The test that reads and writes the state at once
now writes until the readers have read 30 times: with the codec, its twenty writes could be over
before a reader woke.

The contract sees the launcher run `bin/darwin/noctis` without `uname` for each `OSTYPE` and
`HOSTTYPE` a Mac's shells set and leave one that bash never sets to `uname`, give hooks and status
line refreshes `GOMAXPROCS=1` and its marker but no other command, and pass on a `GOMAXPROCS`
already set; on a Mac it runs `bin/darwin/noctis`, each build in it the Mac can run, and `lipo` on
it. The lab checks the fetch aside eight ways against an endpoint that answers after 4 s, and that
the clone installer asks `uname` only for the system on macOS. Hygiene checks, on every system, the
universal binary's layout and the code signature its arm64 build needs to run, and that CI fuzzes
every target the Go tests declare.
