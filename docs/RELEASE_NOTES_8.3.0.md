# noctis 8.3.0

8.3.0 gives noctis a second job. Besides keeping long Claude Code work going through the usage
limits, it now keeps Claude at its best while it does that work: a small, clean context for each
step, a check before a step counts as done, a stronger model for the step Claude gets stuck on, and
limits spent on the work rather than on rereading a long conversation. So the effort a session runs
at now follows the session instead of an environment variable that overrode every other level; new
installs start on the Code profile; a queue names the project's own check and counts the ticks no
check has passed; a test is not weakened to make a check pass without a word; a compaction keeps
what the work needs, and the session after it is told where its item stands; a stuck item goes to a
fresh worker, and a queue gives up sooner; and `noctis queue status` shows what each item took. It
carries everything in 8.2.0 ([RELEASE_NOTES_8.2.0.md](RELEASE_NOTES_8.2.0.md)).

## If you are upgrading

8.3.0 adds one setting, `queue.guardTests` (true), one agent, `noctis:worker`, and one hook:
`hooks.json` now wires `PreCompact`, which runs asynchronously. It changes three shipped values:
`queue.maxIdleContinues` (4 → 3), `compaction.instructions` ("" → a list of what a summary is to
keep) and the profile (SYNEX → Code).

- **Run `/noctis:setup` once.** Setup used to write the code role's effort to
  `env.CLAUDE_CODE_EFFORT_LEVEL` in `settings.json`. That variable overrides every other level: a
  typed `/effort`, `--effort`, and the effort of noctis's own agents, `noctis:deep` at max among
  them. Setup now saves a level below max as `modelSettings.<model>.effortLevel`, which Claude Code
  2.1.251 and later read when a session starts, and takes the variable away, keeping its value for
  uninstall. `max` stays in the variable, since no setting holds it, and so does any level on an
  older Claude Code. Until you run setup, `noctis doctor` fails the effort line of a profile below
  max and names the fix.
- **An existing `config.json` keeps its values.** As in every upgrade, the configuration keeps the
  values it has and gets the new keys with their defaults. So an install from before 8.3.0 keeps:
  - `queue.maxIdleContinues` 4: set it to 3 for the new default (see
    [A stuck item, sooner](#a-stuck-item-sooner));
  - `compaction.instructions` "": delete the key to take the shipped list (`""` still sends none);
  - its profile: an install on 8.2.0's shipped profile stays on SYNEX (Opus 5.5 · max).
    `/noctis:setup --profile code` moves it to Code.
- **New installs start on Code.** Opus 5.5 · xhigh writes the code, Sonnet 5.5 · high does research
  and writing, and `agents/lite.md` ships to match. On Opus 5.5's system card max scores no better
  than xhigh on two of the three coding benchmarks (Terminal-Bench 4.0 64.8 at max against 66.4 at
  xhigh, within the error bars; FrontierCode 54.4 at max against 54.6 at medium) while it spends
  more of the limits on each step, and Claude Code's effort table warns that max may show
  diminishing returns and is prone to overthinking. SYNEX stays one flag away
  (`/noctis:setup --profile synex`) for those who measure a gain from it.
- **A copy made by the install scripts gets the worker agent and the hook when you run the script
  again.** The copy is now 21 files (22 on Windows). The plugin itself carries `agents/worker.md`.
- **What you may notice.** `noctis doctor` exits 1 while fast mode is on. In a queue, Claude's first
  change to an existing test or check file on an item not about tests is refused once. A check cut
  short runs once more before it counts. With `queue.verifyAttempts` above 2, the same failure
  twice in a row holds the queue. A stop while a subagent or a workflow runs in the background is
  let go without a continuation.

## Effort follows the session

Claude Code puts the effort a session runs at into the hook input (`effort.level`) and, for hooks,
into `$CLAUDE_EFFORT`. noctis now reads the level from there first, then from
`CLAUDE_CODE_EFFORT_LEVEL`, then from `settings.json` (the variable, the level saved in
`modelSettings` for the model, `effortLevel`), then from `models.effort`. So the step up for a stuck
item goes from the level the session really runs at, a typed `/effort` included.

Setup saves the level per model, as above: `opus`, `sonnet` and `fable` under `claude-opus-5-5`,
`claude-sonnet-5-5` and `claude-fable-5-1`, a `claude-…` id under itself. A relaunch passes a level
below max as `--effort` alone. Uninstall puts back what the `modelSettings` entry held before setup,
and leaves a level you changed since. While a Fable scoped switch holds the session on its fallback,
setup saves the level for the model the reset brings back. The doctor checks the level where setup
saved it and fails a line while the variable overrides it.

## Each step in a fresh context

- **A named worker.** Every hand-off the Stop hook asks for (the next item once the session's
  context passes `queue.subagentAboveTokens`, 100,000 tokens; an item tagged for another model; a
  stuck item going up to Opus) now goes to the new `noctis:worker` agent, named by its
  `subagent_type`. The item gets a context of its own rather than a fork of the session's, and the
  hand-off no longer needs a general-purpose agent, which a session may lack. The worker runs on the
  session's model unless the call names another, reads the code before it changes it, fixes the
  code a failing check checks rather than the test, leaves the queue file to its caller and ends
  with each check it ran and its exit status. An Opus item going up to max still goes to
  `noctis:deep`.
- **One hand-off text.** Every hand-off asks for a brief that stands on its own (the goal and what
  done means, the files and decisions the item needs), to wait for the result and start no other
  item meanwhile, since a subagent may start in the background, and then to check the work and tick
  the item. The brief for an item going up adds what was tried and where and why it failed, with
  the errors verbatim, so the stronger model does not repeat those attempts.
- **Background work is waited for.** A stop while a subagent or a workflow the session started
  still runs in the background (Claude Code 2.1.286 and later list it in the Stop input's
  `background_tasks`) is let go without a continuation, since Claude Code wakes the session with the
  result. No next item is handed out, the queue is not called finished, the stop does not count as
  one without progress, and the check between items waits for that result. `noctis why` shows
  `allow-stop` with `background work in flight`. A background shell or monitor, such as a dev
  server, does not hold the queue.

## A check before a step counts

- **The project's own check, named.** While nothing names a check between items (no `noctis-verify`
  line, no `queue.verifyCommand`) and `queue.fileVerify` is on, `noctis queue trust` and
  `noctis queue status` name the command the project seems to check itself with, going by its build
  files: a `check` rule, else a `test` rule, in a Makefile; a `test` script in `package.json`, run
  with the package manager its lock file names; `go.mod`; `Cargo.toml`; a pytest setup, through `uv`
  or `poetry` when their lock file is there; `deno.json`; Gradle; Maven; `mix.exs`; a .NET solution
  or project. noctis never runs a command it only suggests:

  ```
  No check runs between items. This project seems to check itself with `go test ./...`: add the line noctis-verify: `go test ./...` to TASKS.md and trust the file with noctis queue trust to have it run after each ticked item.
  ```

  `noctis queue status --json` gives it as `check.suggested`.
- **The ticks no check passed are counted.** `noctis queue status`, `noctis status` and the daily
  digest count the ticked items no check has passed, all of them while nothing checks the queue;
  `--json` gives `check.unverified` and `check.ticked`.
- **A run cut short runs again.** A check run past `verifyTimeoutSeconds`, killed by a signal, or
  ended with status 124 (as `timeout` ends one) or 137 (a kill, as when the system runs out of
  memory) says nothing about the code. So the first such run since the last pass runs once more
  before it counts as a failed attempt: at once when the Stop hook has the time for it, else at the
  next stop while Claude goes on with the next item. With no item left to go on with, it counts at
  once. The digest says a rerun is due meanwhile.
- **The same failure holds the queue.** With `queue.verifyAttempts` above 2, a failure whose exit
  status and end of output match the failure right before it (durations, clock times, dates, memory
  addresses and temporary folders left out) holds the queue at once: the fix in between changed
  nothing the check sees. The notice says so, and `noctis why --json` shows `same` on that
  `hold-queue`. A run cut short, or one that printed nothing, is never the same.
- **A test is not weakened in silence.** A failing check between items is to be fixed in the code it
  checks. A run nobody watches can instead make it pass by weakening, skipping or deleting a test, or
  by loosening the linter or the CI configuration, and the failure stays. So in a turn noctis
  started, while a trusted queue drives the session, the PreToolUse hook refuses once per queue item
  a `Write`, `Edit` or `MultiEdit` of Claude's to an existing test or check file inside the queue's
  folder when the item Claude is on is not about tests. The refusal says why, and that the same
  change made again goes through, to be explained in the summary. The changes that go through are
  recorded, and the daily digest lists their files; `noctis why` shows `ask-test-edit` and
  `test-edit`. A test or check file is known by its name (`*_test.go`, `test_*.py`, `*.spec.ts`,
  `*Test.java` and the like), by a test folder on the way to it (`tests`, `__tests__`, `spec`,
  `testdata`, `fixtures` and the like), or as a test runner's, a linter's or a CI service's
  configuration. New tests Claude writes, items about tests or naming the file, turns you typed, a
  pause and observe mode are left alone. `queue.guardTests` false turns it off.

## A compaction keeps what the work needs

After a compaction the summary is all Claude has of the work before it, and a summary can drop or
misstate what matters most to go on: the item in hand, the files it changed, and whether a check ran
to the end.

- **Instructions for the summary.** `compaction.instructions` now ships a list of what the summary is
  to keep, in short sections: the user's requests and intent; the task or queue item in hand, its
  acceptance criteria and what is done and left; each file changed and why; each build, test or
  check command run with its exit status, one that timed out, was killed or failed marked
  UNVERIFIED, to be run again; the errors met and their fixes; the decisions and the approaches
  ruled out, with the reason; open questions and the next step; leaving out what can be read again.
  The lean module hands it to each compaction while `compaction.lean` is on. A `/compact` with its
  own text keeps that text, and `""` sends none.
- **A note after the compaction.** While a trusted queue that is not held drives the session, the
  `SessionStart` that follows a compaction adds a note read from the queue file, `git status` and
  noctis's record of the check rather than from the summary: the item in hand, the files changed
  since the last commit (at most 12 named), how the check stands, and, from the second compaction on
  the same item, how many it has gone through, with a word to hand long test runs, big diffs and wide
  searches to a subagent.
- **The compactions are counted.** The new `PreCompact` hook counts the compactions on the item in
  hand; `noctis why` shows `count-compaction`. The count goes with the item's tick (see
  [What each item took](#what-each-item-took)).

## A stuck item, sooner

`queue.maxIdleContinues` now defaults to 3: the second stop in a row without progress is the last
continuation, and the third gives up, so less of the limits goes to an item the session cannot get
past. Where there is a stronger model, that last continuation hands the item to it, `noctis:worker`
on Opus or `noctis:deep`, with the brief above, and its turn takes one stop more: when it does not
finish the item either, the third stop without progress asks Claude to set the item aside and the
fourth gives up. Where there is none, the last continuation asks Claude to set the item aside.

An item now goes up only when Claude Code's stop block cap (8, or `CLAUDE_CODE_STOP_HOOK_BLOCK_CAP`)
leaves room for that stop more, that is with `maxIdleContinues` below the cap; otherwise it is set
aside at once. 8.2.0 let it go up with `maxIdleContinues` at the cap as well, and then Claude Code
ended the turn itself before the queue could give up with its own notice.

## What each item took

At each stop that finds more items of a trusted queue file ticked than the stop before noted, noctis
notes what they took: the session's context in tokens at the tick (as its status line last reported
it), the compactions and the continues without progress on the way, how long they took, the setup
that did them and the commit the repository was at. Once there are 3 notes, `noctis queue status`
sums them up:

```
  Last 12 item(s) ticked: context at the tick 84k tokens (median; most 162k), 3 compaction(s) and 2 continue(s) without progress on the way
```

`noctis queue status --json` gives each of the last 50 notes as `models.ticks` (`at`, `items`,
`seconds`, `contextTokens`, `compactions`, `idle`, `setup`, `head`). So whether your items finish in
a small context, and how many compactions they go through, is read from your own queue rather than
guessed, and you can size the next items by it. The compactions counted on an item go with its tick
and are not counted again. Nothing leaves the machine.

## Smaller changes

- **Fast mode.** `noctis doctor` fails a line while fast mode is on (`fastMode` in `settings.json`,
  which `/fast` saves): its requests draw from paid usage credits at a higher rate, outside the plan
  limits noctis pauses at. The fix line says to turn it off with `/fast`.
- **The reading Claude took.** The queue instructions ask Claude to note, with `noctis queue note`,
  the reading it took of an item that reads more than one way, beside the approaches it chose and
  what it left out.
- **The goal, written down.** The README, the guide and the plan now state noctis's second goal.

## Known limits

- **The test guard is a speed bump, not a lock.** It sees `Write`, `Edit` and `MultiEdit`; a change
  made through the shell is not refused. It knows a test or check file by its name, its folder or as
  a known configuration, so one named otherwise is not guarded. And the same change made again goes
  through: it is recorded, and the digest lists the file.
- **The shipped compaction instructions need function hooks.** They reach a compaction through the
  lean module, which Claude Code loads only with `CLAUDE_CODE_ENABLE_FUNCTION_HOOKS=1` (setup writes
  it unless `--no-lean`). A command hook such as `PreCompact` cannot change what the summary keeps.
  The note after a compaction needs a trusted queue.
- **Background work is known from Claude Code 2.1.286 on.** An older Claude Code does not list it,
  and the stop is continued as before.
- **The effort per model needs Claude Code 2.1.251.** An older one gets the variable, as before. So
  does max, which therefore still overrides the effort of noctis's agents on SYNEX.
- **A tick note is as exact as the status line.** The context is the one the status line last
  reported, none without a status line; items ticked at one stop share one note; and the time runs
  from the note before, a pause included.
- **The same failure is known by its output.** A check whose output changes for another reason
  (a random seed, the order of its tests) is not seen as the same; one that ends the same way for two
  different faults is.
- **The suggested check is a guess.** noctis reads build files and never runs the command; make sure
  it is the one you want before you add the line and trust the file.
- The limits in [RELEASE_NOTES_8.2.0.md](RELEASE_NOTES_8.2.0.md) still apply.

## Tests

33 new Go tests, and the test of the shipped defaults now expects the Code profile:

- setup saving a level below max for the model and taking the variable away, max going back to the
  variable, an older Claude Code getting the variable, the canonical model ids, uninstall putting
  back what an entry held and leaving a level changed after setup, a relaunch passing the level as a
  flag alone, and the doctor checking the level where setup saved it;
- the project's own check found from its build files and suggested by `queue trust` and
  `queue status`, and the unchecked ticks in the status and the digest;
- a check cut short running again at once or at the next stop, one killed on every run counting once
  it ran twice, the same failure holding the queue, another failure keeping its attempts, a failure
  known again whatever its timings and temporary paths, and the check waiting for background work;
- test and check files known by their name or folder, items about tests known by their words, a
  change refused once and then recorded, a test Claude writes staying open to it, and test changes
  going through where they belong;
- the session after a compaction told where its item stands, the note when nothing checks the
  queue, and no count or note for a queue nobody trusted;
- a queue giving up after three stops without progress, or four after a stronger model's turn, an
  item going up only where Claude Code's stop block cap leaves room for that turn, a stop while a
  subagent runs in the background, and the worker agent's frontmatter;
- each tick noting what its item took, and the summary waiting for three notes and doing without the
  context;
- the doctor's line while fast mode is on.

The contract and monkey suites send `PreCompact` as well, and the lab follows the Code default, the
level saved per model and the three stops. The Go tests and the lab no longer take the effort, the
compaction point, the stop block cap or the id of a Claude Code session that runs them, and the soak
ends a session's turn when its stop pauses it, as it does at the other pauses.
