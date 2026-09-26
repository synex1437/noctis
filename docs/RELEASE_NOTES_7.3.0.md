# noctis 7.3.0

7.3.0 is about long jobs. You start a queue from a file you name with `/noctis:start <file>` and end
it with `/noctis:stop`, a `TASKS.md` you never trusted stays out of the way, and a prompt that asks
for several jobs in sentences is queued in your own words. In a long queue Claude's context stays
small: once it holds 100k tokens the next item goes to a fresh subagent, and a relaunch after a
pause of an hour or more starts a new session from the handoff note instead of reading the whole
conversation again with a cold cache. A pause now goes on in one place only, so the work never runs
twice. The rest is fixes to relaunches, usage readings, the queue and the guards, setup profiles
named for the work they suit, and releases that no longer wait for a tag. Each fix to noctis's code
came with a test.

## If you are upgrading

An existing `config.json` keeps the values it has and gets the new keys with their defaults.

- **A `TASKS.md` nobody trusted is left alone.** It is neither followed nor brought up at session
  start, and it no longer hides a trusted queue file further down the lookup or keeps a list prompt
  from becoming a checklist. `noctis queue status` still shows it with the way to trust it. To run
  a file's jobs without trusting the file, use `/noctis:start <file>`.
- **The fresh context is on.** `queue.subagentAboveTokens` (100000), `resume.freshAfterMinutes`
  (60) and `resume.freshAboveTokens` (100000) reach your `config.json` with those values, and `0`
  turns each off. See "Long jobs: a fresh context" below.
- **The setup profiles are Code, Search, Balanced and SYNEX.** SYNEX is the default and the only one
  that runs code and its fallback on Opus 5.5 at max; Code and Search run the main session at xhigh
  (Search researches at xhigh, Code at high), and Balanced is unchanged. `economy` is gone: a
  configuration that stored it keeps its roles and is shown under that name, and `--profile
  economy` is refused. `noctis`, the name 7.2 stored for SYNEX's assignment, still means SYNEX.
- **The first relaunch after the upgrade may leave one window open.** `resume.closePrevious` now
  closes a window only while its process ID still has the start time noctis recorded. A launch
  record written by 7.2.0 or earlier has none, so that window is left for you to close.

## The ones that matter

**A pause could go on twice.** Several paths act on a stored pause: the hook holding it in place,
hooks that joined it, the runner, a same-session wake, a manual resume in another window, Claude
Code's own auto-continue and noctis's early resume. Most read the pause without the lock. A hook
holding a pause went on after the runner had relaunched the session or after you resumed it by hand
in another window, so the work ran twice; a wake woke a session the runner had already relaunched;
a runner relaunched a session a wake had just woken in place; and a late step could delete,
reschedule or mark a newer pause that had taken the old one's place. Now every step changes a pause
only while it is still the one it read, under the state lock, and whatever takes a pause over for
another place marks it. A hook still holding that pause stops with "While this window waited, the
session went on in another window, so this one stops here and the work does not run twice." (in
all 14 languages): its prompt is blocked, an Agent call is denied and ends the turn, a tool batch
ends the turn and the Stop hook lets the stop through. A runner that comes due within
`wake.graceSeconds` of a wake checks back once that grace is over instead of relaunching, and
`noctis why` logs `continued-elsewhere`.

**The queue file could change between two reads.** The Stop hook checked the trust on one read of
the queue file and took the next item, the issues to close and the check's ticked items from later
reads; a checkpoint and the relaunch prompt did the same. A process that rewrote `TASKS.md` in
between got its text into Claude's continuation, the checkpoint or the relaunch prompt, and
`queue.github.closeOnDone` could close an issue that only the rewritten file had ticked. Each of
them now reads the file once and acts only on the text its trust check read.

**`resume.closePrevious` could close a window you opened.** The launch record kept only the process
ID of the window noctis opened. A program started after that window ended could get the same ID
(Windows hands a freed ID out again at once), and the next relaunch killed it if it was named
`claude`, `node` or `cmd.exe`. The record now keeps the process's start time too, and the window is
closed only while its ID still has it.

**With a check set, an issue closed before the check ran.** With `queue.verifyCommand` set, the Stop
hook closed the GitHub issue of an item Claude had just ticked before it ran the check, and the stop
that found every item ticked called the queue finished without running the check at all, so the
last item was never checked. Now an issue closes only once each of its ticked items passed the
check, and the last stop runs the check first: a failure sends Claude back to fix it, and only a
pass finishes the queue.

**The bug-report bundle carried what workflows were launched with.** `noctis report --bundle`, which
is meant to be attached to a public bug report, copied the name, script path, description,
arguments and script of each session's last three workflow launches. Each of those fields now reads
`<redacted workflow <field>, length N>`, and only the time of the launch is kept.

**A resume that Task Scheduler started lacked the session's environment.** On Windows a runner that
Task Scheduler starts gets the account's stored environment, not the PATH, certificate and proxy
variables of the session that paused, so a relaunch could fail to find `claude` or to reach the
API. Those variables now go to the file only you can read that already carries them to a launchd or
systemd runner (`proxies/<session>.json`); the runner restores them and removes the file. Nothing
new reaches the task's command line.

**Two readings in the same second made a burst.** Two readings of a window with the same reset in
the same second, from two sessions' status lines or an older and a newer one written together,
counted as a rise that took no time: 20 % and then 86 % made a 66-point burst, and a window at 88 %
paused four points under its 92 % pause point. Only the last reading of each second now counts, for
the burst, the projection of stale data and the weekly ETA alike.

**A window that stopped updating looked fresh.** Each window's data is as old as its own last
reading, but `noctis check` and the session-start notice that a window is over its pause point
looked only at the usage file's age. `noctis check` now answers 20 (stale data) when either window
was last read longer ago than `usage.staleMinutes`, and the session start fetches such a window
before it says anything about the limit.

## Starting and stopping a queue

`/noctis:start deneme.md` reads the jobs in the file you name (its queue items, or one job per line
in a file that has none), says how many it found ("5 job(s) detected in deneme.md") and copies them
to a checklist in noctis's own folder. That checklist drives the session the way a queue file does,
before a trusted `TASKS.md`, through the prompts you type while it runs and again after a compaction
or a relaunch; your file is never ticked. A file that cannot be read as text, holds no open job, or
is over 1 MB or 500 jobs starts nothing, and neither does a start while noctis is paused or queues
are off; each says why.

`/noctis:stop` ends the session's queue, whether it came from a file or from a prompt, and says how
many of its jobs were done. It runs no turn, so it also works while noctis is paused, at the usage
ceiling and in a window the session moved out of. Claude can start neither command itself.

## Several jobs in one prompt

A prompt that asked for several jobs in sentences, not as a list, became a checklist only when it
held four or more of them, each on its own line or tied by words such as "then" or "finally". Such
a prompt (240 to 6000 characters, with at least three clauses that read as instructions, not a
question, a bug report or pasted log lines) now gets an empty checklist, and Claude is told to write
the jobs into it first, one line each, in the order to do them and in your own words, or to leave it
empty when the prompt is one job. The first jobs it writes bring the usual "3-step job detected"
notice, the Stop hook then drives the checklist, `/noctis:stop` ends it and a compaction keeps it. A
checklist left empty is removed at the next stop without a word.

noctis keeps a digest of each word of the prompt with the checklist, for checklists taken from list
prompts too, and denies a Write or Edit of Claude's that would put a line in it with a word the
prompt does not have. It names those words to Claude and shows you the lines it kept out, so no job
you did not ask for is queued. Ticking a box and lines already in the file pass.

## Long jobs: a fresh context

A queue that runs for hours keeps one session going, and every turn of every item carries the whole
conversation; after a pause longer than the prompt cache lives, `--resume` also reads all of it
again with a cold cache. The status line now keeps how many tokens the context holds
(`contextTokens` in `usage.json`: the input of the last request, cached parts included, else the
used share of the window), and a pause keeps that count with it.

- Once the context holds `queue.subagentAboveTokens` (100000), the Stop hook's continuation of a
  trusted queue or checklist, and a relaunch prompt that resumes the session with one, tell Claude
  to hand the next item, unless it is a quick edit, to a fresh general-purpose subagent with a brief
  that stands on its own (the goal, the files and decisions it needs, what done means), then to
  check its work and tick the item itself. The subagent reads only what the item needs.
- When the runner relaunches a Claude Code session that was idle for `resume.freshAfterMinutes`
  (60) with `resume.freshAboveTokens` (100000) of context, it starts a new session with
  `--session-id` instead of `--resume`. The new session is told which session it takes over from,
  reads the handoff note first (noctis answers the permission prompt for that Read) and searches
  the old transcript for a detail instead of reading it whole. The checklist and its continue counts
  move to it. A session that launched a dynamic workflow is always resumed, since a workflow can be
  resumed only from its own session. If the new session never answers, everything goes back and the
  retry uses `--resume`.

Neither adds a window: a pause that waits in its own window goes on there as before, and the fresh
start takes the place of a relaunch that would have happened anyway.

## Also in this release

- **Setup names the profiles in all 14 languages.** The setup question, the roles line in `noctis
  status` and the re-tune notice use the new names; the numbers 1 to 5 pick Code, Search, Balanced,
  SYNEX and custom, and `--profile` takes the names in any case.
- **A plain search no longer counts as Claude trusting the queue.** The check that stops Claude from
  running `noctis queue trust` itself took every `xargs` for a program that runs what it is handed,
  so `grep -rl "noctis queue trust" docs | xargs wc -l` was denied and you were told Claude had
  tried to trust the queue. An `xargs` whose program starts no other program (echo, cat, grep, wc,
  rm and the others docs/REFERENCE.md lists) now passes; one that can run a command is still denied.
- **Windows junctions under either reading of links.** The lite agent's write limits and the check
  that Claude does not write noctis's own `state.json` or `config.json` followed a junction only
  because the binaries read links as Go 1.22 did (`winsymlink=0`). They now follow it under either
  reading, so `GODEBUG=winsymlink=1` in the environment no longer opens a way around them. The
  shipped binaries were not affected unless `GODEBUG` said otherwise.
- **A process that had just exited could count as running.** noctis asks whether a process is alive
  and then reads its status to tell a zombie from a running process; a process reaped between the
  two looks was taken for a running one. It now asks again and takes "no such process" for exited.
- **Releases no longer wait for a tag.** When ci passes on a push to main, the release workflow
  releases the version in `plugin.json` if it has no tag yet, tagging the commit ci tested; nobody
  pushes a tag by hand. CONTRIBUTING.md says how to release.

## Known limits

- With the router on, it still sends 20 of the 118 own-work prompts in its corpus to the lite agent
  in a session that has touched no file, and 4 in a coding one. The main model can search the web
  itself, and `lite:` still sends a prompt to the agent.
- With `workflow.suggest` on, "review all the files in this PR", "review all tests I added today"
  and "update the header component so it shows on every page" still bring the fan-out notice,
  though each is one task.
- The check on Claude's shell calls reads the command line, not what finally runs: a name or command
  built at run time, a script file that runs it, or another program that runs a command it is
  handed is not recognized. It stops Claude's direct call and is not a sandbox.
- The lite write limits do not cover a file or folder that `CLAUDE.md`, a queue file or a project's
  `.claude` is a link to, written under its own name (an `AGENTS.md` that `CLAUDE.md` links to), nor
  the files a `CLAUDE.md` pulls in with `@`. The main session's writes and other subagents' are not
  held to these limits.
- On Windows the checks that keep a queue file or an import inside its folder follow a junction only
  under the reading of links the binaries ship with; `GODEBUG=winsymlink=1` in the environment turns
  that off for them.
- On Windows a parked prompt still reaches the relaunch as one line, with double quotes, `%` and `^`
  turned into `'`, since the command line can pass through `cmd.exe`.
- The generic webhook tells accounts apart only by their folder's name: `~/.claude` and
  `/work/.claude` both post `".claude"`.
- The fresh context goes by the context size the status line reports. A session with no reading yet,
  or on a host other than Claude Code, keeps its queue items in the session and is resumed with
  `--resume`. Whether an item is a quick edit is left to Claude.

## Tests

Every fix to noctis's code above came with a test that fails on the code just before it, but for the
Windows junction fix: its tests need Windows, CI runs them on the new code, and that the old code
fails them is read from that code, not run. Beyond those, the lab's readings now come a minute
apart, as status lines do, so its burst checks no longer depend on how fast the machine runs, and
docs/PLAN.md, docs/TESTING.md and docs/REFERENCE.md were brought up to date with the code. Local
round on the release tree, on Linux with claude 2.1.283, Go 1.24.7 and node 22: go test with Windows
and macOS builds and vets, go test -race once, 30 seconds of each of the five fuzz targets, hygiene,
i18n, contract, chaos, scheduler, lab, a two-day hard soak, the torrent and two monkey seeds.
