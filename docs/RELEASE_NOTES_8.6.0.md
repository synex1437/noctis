# noctis 8.6.0

8.6.0 keeps a long session in a small context. On its own, Claude Code compacts a 1M window near
967k tokens of context, so every request until then can carry that much; 8.6.0 has it compact by
about 280k tokens, in a 1M window too. Setup writes `"autoCompactWindow": 313000` into
`settings.json`, the setting Claude Code compacts by, from the new `compaction.compactAt` (280000),
and on an account set up before, the first session start of 8.6.0 writes it once, with a notice.
Claude Code reads that setting as it starts, so setup now asks for a restart rather than
`/reload-plugins`. Wherever the point is set — by noctis, by you in any settings file Claude Code
reads, or with Claude Code's own `/autocompact`: 600k, 140k, 60 % of each model's window, or Claude
Code's own point — noctis reckons it as Claude Code does, for each session's model and window, and
finds a model's own window under every name Claude Code gives the model, on every provider. The
status line's `ctx`, the early compaction of lean compaction and the guard before the 5-hour limit
all count toward it, and after a compaction noctis drops the context fill it had. When Claude Code
cannot fit the context even after compacting, noctis now starts the session afresh from its resume
note; in 8.5.4 the StopFailure hook was never started for that error. The hooks hand Claude the same
text as in 8.5.4 and take at most about 0.2 ms longer.
8.6.0 carries everything in 8.5.4 ([RELEASE_NOTES_8.5.4.md](RELEASE_NOTES_8.5.4.md)).

## If you are upgrading

8.6.0 reads every file 8.5.4 wrote. It adds `compaction.compactAt` (280000), and
`compaction.earlyAtPercent` (90) replaces `compactAtPercent`. What changes from the first session:

- The first session start writes `"autoCompactWindow": 313000` into `settings.json` once, records it
  in `config.json` as `managedAutoCompactWindow`, and says so in the session. Claude Code compacts
  there from its next start. Nothing is written beside an `autoCompactWindow` or
  `CLAUDE_CODE_AUTO_COMPACT_WINDOW` of yours. Beside a `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` of yours,
  in `settings.json` or in the environment Claude Code runs in, the window is 1000000 instead: your
  percent decides, taken of each model's whole window, for every model. A window you take out by
  hand is not written again at a session start, though a later `/noctis:setup` writes it;
  `/noctis:setup --compact-at off` takes it back and keeps it out.
- `compaction.compactAtPercent` moves to `earlyAtPercent`. A value that switched the early
  compaction off becomes 0, and any other value gives way to 90, which now counts the way to where
  Claude Code compacts rather than the whole window.
- The status line's `ctx` is the share of the way to where Claude Code compacts, not of the window:
  140k tokens in a 1M window that compacts by 280k show `ctx 50%`, where 8.5.4 showed 14 %.
- The guard pauses at 95 % of the point, where 8.5.4 held the context's share of the window against
  85 %: at 266k tokens in a 1M window, about 159k in a 200k one. A `compaction.contextPercent`
  changed from 85 keeps the share.
- `noctis status` and `noctis doctor` say where Claude Code compacts and which setting decides it,
  with one line for each model whose own window `/autocompact` saved. The doctor fails while
  `compactAt` asks for a setting that `settings.json` lacks.
- The StopFailure matcher in `hooks/hooks.json` takes `invalid_request`, so the hook now runs when a
  turn ends with the context full.
- Setup's next step is a restart (`/exit`, then `claude --continue`), since `/reload-plugins` does
  not make Claude Code read where it compacts again.

## Where Claude Code compacts

Claude Code 2.1.289 compacts once the context is within 33000 tokens of the window it works with:
20000 kept for the summary and 13000 as a buffer. Where `CLAUDE_CODE_MAX_OUTPUT_TOKENS` lets a reply
have fewer than 20000 tokens, only that many are kept for the summary. That window is the model's
own, or a smaller one taken from the first of these that is set:

1. `CLAUDE_CODE_AUTO_COMPACT_WINDOW`, held between 100000 and 1000000.
2. The model's own `modelSettings.<model>.autoCompactWindow`, as `/autocompact` saves it.
3. `autoCompactWindow`.

Claude Code sets aside an `autoCompactWindow` that is not a whole number from 100000 to 1000000, as
if it were not there, and so does noctis: a model's own entry set aside that way leaves
`autoCompactWindow` to decide.

`CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` puts the point at that percent of the window once the tokens kept
for the summary are set aside, never later than the 33000 short of it: 60 % of a 1M window is 588k.
In a 1M window with none of these set, Claude Code compacts near 967k tokens.

`compaction.compactAt` takes one of three kinds of value:

- A token count from 67000 to 967000, also written `280k` or `0.28m`. Setup writes
  `autoCompactWindow` = that count + 33000.
- A percent of each model's window from 10 to 100, written `60%`. Setup writes
  `env.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`, with `autoCompactWindow` 1000000 beside it.
- `off`, which leaves the point to Claude Code.

`/noctis:setup --compact-at` sets it, and later setups keep it. A value out of range exits 1 before
anything is written. Where each choice has Claude Code compact:

| Choice | In `settings.json` | 1M window | 200k window |
| --- | --- | --- | --- |
| shipped, `280k` | `autoCompactWindow` 313000 | 280k | 167k |
| `--compact-at 600k` | `autoCompactWindow` 633000 | 600k | 167k |
| `--compact-at 140k` | `autoCompactWindow` 173000 | 140k | 140k |
| `--compact-at 60%` | `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` 60, `autoCompactWindow` 1000000 | 588k | 108k |
| `--compact-at off` | nothing | 967k | 167k |
| `/autocompact 140k`, for that model | `modelSettings.<model>.autoCompactWindow` 140000 | 107k | 107k |
| `/autocompact auto`, for that model | `modelSettings.<model>.autoCompactWindow` `"auto"` | 967k | 167k |
| your own `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` 60 | `autoCompactWindow` 1000000 beside it | 588k | 108k |

Since the smaller window counts, a 200k window keeps Claude Code's own point, near 167k, for every
token count above that. Setup records what it wrote as `managedAutoCompactWindow` and
`managedAutoCompactPercent`. `--compact-at off` and uninstall take a setting back only while it
still holds that value, and a percent taken back is kept as `takenAutoCompactPercent`, so a stale
copy in Claude Code's environment is not taken for yours. Claude Code checks the point before each
model request, so a long turn compacts on the way too, such as a queue the Stop hook keeps going from
item to item. The setting is Claude Code's own, so the point holds without function hooks.

Claude Code compacts at a percent only for a model it has a window for, from the settings or its
own. It keeps one of its own for the Claude 5 models, but none for Haiku 4.5, the other Claude 4
models and older ones, or a Bedrock inference profile: those it compacts only once the context is
full, whatever the percent. The `autoCompactWindow` 1000000 beside a percent gives every model a
window from the settings, and since Claude Code takes the smaller of that and the model's own, it is
each model's whole window. A window for a token count would narrow it instead: 60 % of 313000 is
about 176k.

Claude Code reads the window only as it starts, and again when `/autocompact` changes it. A window
that setup or a session start writes, moves or takes back therefore applies from Claude Code's next
start. Setup's output, the session notice, the setup skill, the README and the guide now say so.

## noctis follows the point

noctis works out the point for each session as Claude Code does, from the session's model and window
as the status line reports them and from the settings above:

- **The status line's `ctx`** is the share of the way to that point, so `ctx 100%` is where Claude
  Code compacts, wherever that is. When lean compaction is not running, `▲` marks a context past
  `earlyAtPercent` and the next prompt gets one notice to type `/compact`.
- **The early compaction** of lean compaction asks Claude Code to compact at the end of a turn once
  the context is 90 % of the way to the point (`compaction.earlyAtPercent`): at 252k tokens in a 1M
  window with the shipped point, at about 150k in a 200k one, and at about 529k with 60 % in a 1M
  window. The lean module reads the settings as they are, so it follows a moved point at once.
- **The guard** pauses at 95 % of the point while the 5-hour window is within 6 points of its pause
  point, so the summary request does not carry that window past it.
- **After a compaction**, noctis forgets the context fill it had until the status line reports the
  new one, and arms the `▲` notice again, so nothing weighs the context from before the compaction.
- **With Claude Code's compaction switched off** (`DISABLE_AUTO_COMPACT`, `DISABLE_COMPACT`, or
  `autoCompactEnabled` false in the settings files or in Claude Code's global config,
  `~/.claude.json`) there is no point to count toward: `ctx` is the share of the window, no early
  compaction is asked for, and the guard does not pause for a compaction. The global config can run
  to megabytes, so noctis reads it again only once its size or modification time changes, or while
  it is less than two seconds old, as a second write in that instant can leave both as they were,
  and keeps the answer in `compaction-switch.json`.

### A model's own window

`/autocompact` saves a model's window in `modelSettings` under the one name Claude Code files the
model under, whether the session names it by its alias, its id, a dated id, `[1m]` or a provider's
id. noctis finds that entry for a session as Claude Code 2.1.289 does, from what decides the name:

- the provider the `CLAUDE_CODE_USE_*` variables choose, and on Bedrock the region prefix that
  `AWS_REGION`, `AWS_DEFAULT_REGION` or `ANTHROPIC_BEDROCK_REGION_PREFIX` gives the model's id;
- the models the aliases stand for: the `ANTHROPIC_DEFAULT_*_MODEL` variables, else the provider's
  own model for the alias;
- `modelOverrides` in the settings files, the first in their order where two give one name;
- `ANTHROPIC_BASE_URL`, `CLAUDE_CODE_DISABLE_1M_CONTEXT` and
  `CLAUDE_CODE_DISABLE_LEGACY_MODEL_REMAP`.

Where several entries stand for the same model, the one under the very name Claude Code files it
under wins, else the first in the file, as in Claude Code. The engine and the lean module give the
answers Claude Code's own code gives for 1547 model names and 870 windows across these settings;
they are kept in `go/cmd/noctis/testdata/claudecode-models.json`, and `go test` checks both against
every one.

### Every settings file Claude Code reads

Claude Code lays several settings files over each other, and the point can come from any of them.
The status line, the guard, the `▲` notice, `noctis status` and `noctis doctor` now lay them as
Claude Code 2.1.289 does:

- the account's `settings.json`, the project's `.claude/settings.json` in the directory the session
  started in, its `.claude/settings.local.json`, then the managed settings: `managed-settings.json`
  with the files of `managed-settings.d` laid over it in the order of their names;
- each `env` variable, `modelOverrides` entry and `autoCompactEnabled` is the last file's that sets
  it; a file that sets `autoCompactWindow` sets aside the model windows of the files before it, and
  of the model windows left, the last file's counts;
- a project's `.claude/settings.local.json` is read where Claude Code reads it: at the root of the
  git work tree the session is in, or of a linked work tree at the main work tree, laid over the one
  in the session's directory, where that root, its `.git` and its `.claude` belong to the user and
  it is not the home directory; on Windows, only the session directory's;
- a file that cannot be read, or holds no JSON object, counts for nothing, and a project file that
  is the account's `settings.json`, as in the home directory, is read once.

`noctis status` and `noctis doctor` name the file a setting comes from. Setup and the session start
still write only the account's `settings.json`. The lean module asks Claude Code for each file and
lays them the same way, so the early compaction follows them too; the engine and the lean module
give the same point for 3000 random sets of one to four files.

## When the context stays full

A file or a command output too large for the window can fill the context again right after each
compaction. Claude Code then ends the turn with `invalid_request`: `Autocompact is thrashing`, or
the API finds the prompt too long. Woken in place, the session would send the same full context
again, and in 8.5.4 the StopFailure hook was not even started for that error.

noctis now takes an `invalid_request` whose text names a full context for one:

- It starts a fresh session from the resume note 30 seconds later, never in place, with the same
  checklist. The fresh session is told to read large files and command output in parts.
- The fresh start needs no usage data, since a full context is no limit, and the context it leaves
  behind holds nothing back: that context is never compacted, so the guard does not weigh it.
- If that context fills up as well, the next fresh start waits the `wait.retryMinutes` steps: 10,
  20, 30, then 45 minutes.
- After five fresh starts that fill up again, noctis stops, journals `retry-giveup`, sends a
  notification and leaves the resume note for the session you start next.
- Observe mode starts no fresh session, so it counts none: it journals each full context as the
  first, never says it gave up, and leaves all five fresh starts to enforcing.
- A turn that ends normally starts the count again, also while noctis is off. Tool calls do not,
  and a fresh session carries the count on.
- A session with no handoff note is resumed with a note that says why it stopped.
- A Claude Code on the web session cannot be started afresh, so its notice says to run `/clear`, or
  `/compact` once what filled the context is gone. The session `/clear` starts is handed the resume
  note.

Any other `invalid_request` is left alone, since a retry would be turned down for the same reason.

## Settings stay in step

What noctis writes to `settings.json` and what it records of it in `config.json` stay in step:

- A `settings.json` that is not a JSON object is left as it is. Setup exits 1 and names it, a session
  start writes nothing, and the doctor and the session-start check name it. An `env` in it that is
  not an object gets no percent from a session start, nor the window beside it.
- When `settings.json` cannot be written, a session start records and says nothing, and setup leaves
  the records in `config.json` as they were. The next start then neither takes a value of yours for
  noctis's nor a value of noctis's for yours.
- Sessions that start together merge `config.json` under the lock the compaction record is written
  under, so the window noctis wrote never passes for yours.
- Setup and uninstall keep a window that `/autocompact` saves for a model while they run.
- A relaunch leaves out a percent that setup took back, so the relaunched session compacts where
  `settings.json` now says.
- Messages give a window of a million tokens in digits (1000000, not 1e+06), and the steps to undo
  setup by hand name both compaction settings.

## Fixed since 8.5.4

- **A wait that starts just after a reset made the reset's first check late.** After a reset, noctis
  reads the usage 10, 30 and 60 seconds past it. A wait made 15 seconds past the reset skipped the
  check at 10 seconds and first read the usage at 30. A check that is due is now made at once.
- **A turn you sent in the session's own window after a pause ended left the queue stopped.** This
  happened between the end of a pause and its runner: a retry 10 minutes after a timeout, a runner
  late after a reset, or a pause brought back with the state backup. The turn ended without the
  queue going on, and the runner then found that the session had gone on and resumed nothing. The
  Stop hook of that turn now ends the pause, as the runner would, and the queue goes on.
- **Observe mode said it gave up on retries it never made.** After an overload that lasted past
  `overload.maxTotalMinutes`, or a failure noctis cannot place that came back six times in a row,
  observe mode sent the give-up notification, and `noctis status` said the automatic relaunch had
  failed. It now only journals `would-overload-giveup` or `would-retry-giveup`.
- **A hook stopped while it held a lock made the next hook wait 2 seconds.** A hook process that
  is stopped while it writes noctis's state, a hook timeout for example, leaves its lock behind. The
  next hook waited until that lock was 2 seconds old before it took the lock over, so a prompt, a
  tool call or a stop took 2 seconds longer. A lock whose process is gone is now taken over at
  once: on macOS and Linux only when no process holds its `flock`, and on Windows a lock that is
  held cannot be deleted. A lock that names no process yet, which another hook may have just
  created, still waits 2 seconds.
- **A session noctis gave up on was started again at the next reset.** When the retry of an
  overload, of a failure noctis cannot place or of a full context came while a limit was in force,
  its runner put it off to the reset. If the session failed again before then, in a turn you sent,
  and noctis gave up, the notification said the session was left stopped, yet the runner still
  started it at the reset. Giving up now takes that retry back.
- **A move off Fable at the end of a turn was journaled under the wrong hook.** When the Stop hook
  moved a session off Fable, as its queue went on past Fable's threshold, the journal row named a
  tool batch, where the pauses of a Stop name the Stop hook. It now names the Stop hook.

## Measured

On linux-amd64, 240 calls of each kind went to the 8.5.4 and 8.6.0 release binaries, taking turns,
in a sandbox account set up by `noctis install`. The table gives the median and, in brackets, the
90th percentile, in ms:

| Call | 8.5.4 | 8.6.0 |
| --- | --- | --- |
| `ensure` | 4.91 (6.25) | 4.99 (6.33) |
| status line, 41 % of the window, 120k tokens | 5.00 (6.27) | 5.10 (6.19) |
| status line, 88 % of the window, 200k tokens | 5.00 (6.39) | 5.06 (6.57) |
| UserPromptSubmit | 5.04 (6.42) | 5.14 (6.42) |
| UserPromptSubmit, 5-hour window near its pause point | 4.97 (6.22) | 5.07 (6.42) |
| PreToolUse, Edit | 5.05 (6.29) | 5.16 (6.47) |
| PreToolUse, Bash | 4.17 (5.46) | 4.28 (5.26) |
| Stop | 4.43 (5.70) | 4.38 (5.43) |
| Stop, 5-hour window near its pause point | 4.40 (5.44) | 4.34 (5.28) |

The medians of 8.6.0 lie within 0.2 ms of 8.5.4's, less than the spread of the calls; a second run
of 240 gave the same. The status line and each prompt now read the settings files for the point and
look for the root of the project's git work tree, about 20 more system calls, which cost them about
0.1 ms of CPU. Where `/autocompact` saved a window of a model's own, they take about 0.2 ms more, to
work out the name Claude Code files the session's model under; noctis skips that where no model has
a window of its own, as with the effort entries setup writes. A global config of 5 MB adds nothing,
since it is read again only once it changes; in the two seconds after Claude Code writes it, which
it does seldom and not on each turn, each call reads it, about 6 ms for 5 MB. The binary is about
225 KB larger (10.6 MB).

What the hooks hand Claude is the same in both, character for character:

- nothing for an ordinary prompt, a tool call or a plain stop;
- 342 characters (about 86 tokens) after a prompt with a list in it;
- 759 and 1173 characters (about 190 and 293 tokens) at the Stop hook's first and second
  continuation of the list;
- 1577 characters (about 394 tokens) after a compaction.

What 8.6.0 takes off is the context itself: in a 1M window a request carries at most about 280k
tokens of it, instead of up to about 967k.

## Known limits

- **Claude Code reads where it compacts only as it starts,** and when `/autocompact` changes it. A
  window written, moved or taken back while it runs applies from its next start; `/reload-plugins`
  does not change that.
  - Until the restart, noctis already counts toward the new point: the status line, the guard and,
    where lean compaction runs, the early compaction at the end of a turn.
  - A percent in `settings.json` reaches a running Claude Code at once, but it is taken of the
    window that Claude Code started with.
  - A percent noctis took back stays in a running Claude Code until it restarts.
- **noctis reckons the point as Claude Code 2.1.289 does:** 33000 tokens below the window it works
  with, with the settings in the order above. A version that reckons it differently compacts at its
  own point, while noctis counts toward this one.
- **Left to Claude Code, with `--compact-at off` or `/autocompact auto`, the window may come from
  Claude Code's own per-model defaults or server-side settings.** noctis cannot read these, so it
  reckons with the model's whole window. For a model Claude Code keeps no window for — Haiku 4.5,
  the other Claude 4 models and older ones, a Bedrock inference profile — Claude Code then compacts
  only once the context is full, and a `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` alone does not move that,
  while noctis counts toward the point 33000 tokens short of the window, or at the percent: the
  status line's `ctx` can pass 100 %, and the early compaction comes before Claude Code's.
- **The status line, the guard, `noctis status` and `noctis doctor` do not see every settings
  source.** A `--settings` file, `--setting-sources` or `--project-config-root` given to Claude
  Code is not read, nor are managed settings Claude Code takes from a device profile, the Windows
  registry or the server, which set the managed files aside. A settings file Claude Code sets aside
  whole, because one of its settings has a form Claude Code does not take, such as
  `cleanupPeriodDays` 0, still counts for them. The early compaction, which asks Claude Code for
  each settings file, follows all of these but `--setting-sources`. Setup and `/autocompact` write
  to the account's `settings.json`.
- **A model's own window is found only under a name noctis can work out.** A name that comes from
  what noctis cannot read is not matched: the models an organization, a plan or a gateway sets for an
  alias, a Bedrock inference profile, the region in `~/.aws/config`, or what Claude Code learns of a
  model from its server. noctis then reckons with `autoCompactWindow`, or with the model's whole
  window.
- **A `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` found only in Claude Code's environment, equal to a percent
  noctis took back, is taken for noctis's leftover,** not for a value of yours.
- **A session compacts about three to four times as often** as one that ran to near 967k tokens.
  Each compaction is a summary request over up to about 280k tokens, and what the summary leaves out
  is gone from the context. `compaction.instructions` lists what it keeps, and `--compact-at` moves
  the point.
- **The early compaction needs Claude Code's function hooks.** Without them, Claude Code compacts at
  the point by itself, and the `▲` notice asks for `/compact` before that.

The limits in [RELEASE_NOTES_8.5.4.md](RELEASE_NOTES_8.5.4.md) still apply.

## Tests

8.6.0 adds 87 Go tests, 86 on Windows.

- `compactwindow_test.go` (32 tests):
  - `--compact-at` values: token counts, percents, `off`, and the ends of their ranges;
  - the compaction point for every choice Claude Code offers: a window by itself,
    `autoCompactWindow`, a model's own window by its id or alias, `/autocompact` values and `auto`,
    the window variable with Claude Code's floor, and the percent override; the effort entries
    setup writes, and a model's own window Claude Code does not take, leave `autoCompactWindow`;
  - the three ways compaction is switched off;
  - Claude Code's global config is read again, in this process and the next, only once its size or
    modification time changes or while that time is not yet two seconds past, and a legacy
    `.config.json` comes before it;
  - the window variable, read as Claude Code reads it;
  - `ctx` as the share of the way to the point;
  - the guard at 95 % of the point, and a 5-hour pause before a token compaction, with the percent
    kept for an own `contextPercent`;
  - the move from `compactAtPercent` to `earlyAtPercent`;
  - `ensure`: it writes the window or the percent once and says so, and leaves a value the person
    set or removed and a window the variable sets. Beside the person's own window it adds the
    percent. Beside a percent, its own or the person's, in `settings.json` or in Claude Code's
    environment, it puts the window 1000000, which follows a move between a percent and a token
    count and goes with `compactAt` off. It records and says nothing while `settings.json` cannot
    be written, keeps its record while it cannot take its window back, and leaves an `env` that is
    not an object;
  - a percent noctis took back is not taken for the person's own;
  - setup writes, moves and takes back its own value, says it applies from Claude Code's next start
    and asks for a restart, and leaves a value it did not write; it puts the window 1000000 beside
    a percent, the person's own too, and beside the window variable writes the percent alone and
    says where Claude Code then compacts; a setup that cannot write
    `settings.json` leaves the records as they were;
  - uninstall takes back the window and the percent;
  - a `config.json` merge with nothing to add leaves the file alone, and a session start merges it
    only under the lock the compaction record is written under;
  - the status and doctor lines, the docs, the steps to undo setup by hand, and a window of a
    million tokens named in digits.
- `claudesettings_test.go` (14 tests):
  - a project's `.claude/settings.json` and `.claude/settings.local.json` and the managed settings,
    with their drop-ins, are laid over the account's as Claude Code lays them, and status names the
    file of each setting;
  - a later file's `autoCompactWindow` sets aside the model windows of the files before it, and in
    one file a model's own name is taken before its other names, then the first;
  - `modelOverrides` of several files are gone through in their merged order;
  - a project that turns compaction off or on is followed, the guard weighs the context by the
    project's point, and the status line and the `▲` notice follow the project the session started
    in;
  - a file that cannot be read is set aside at once, a session in the home directory reads the
    account's settings once, and another host reads only its own;
  - a session in a folder of a work tree, or in a linked work tree, reads the local settings at the
    work tree's root, or the main work tree's, laid over its own; a work tree at the home directory,
    or one whose git directory does not point back at it, keeps them where the session started, as
    Windows always does.
- `settingsowner_unix_test.go` (1 test, not on Windows): the local settings of a work tree someone
  else owns, or under a `.claude` someone else owns, stay where the session started.
- `compactpoint_test.go` (3 tests): a model's own window applies under every spelling Claude Code
  gives the model, a reply limit below 20000 moves the point as it moves Claude Code's, and a window
  Claude Code sets aside does not count.
- `modelkey_test.go` (11 tests):
  - the engine, and the lean module run with Node, file each model under the name Claude Code
    2.1.289 files it under and take the window it takes, for the 1547 names and 870 windows in
    `testdata/claudecode-models.json`;
  - the lean module reckons the compaction point the engine reckons, for 3000 random settings, and
    lays the settings files over each other as the engine does, for 3000 random sets of one to four
    files;
  - Bedrock's region prefixes, the base URLs that keep Claude Code on its own API, switches read
    with the white space JavaScript trims, a `modelOverrides` value that is not text, and the first
    of several names in `settings.json`;
  - the lean module names each variable it reads for the naming of models where it reads it, as
    Claude Code loads a module only then.
- `contextfull_test.go` (8 tests):
  - a turn that ends with the context full is started afresh after 30 s, not woken in place;
  - a context that fills up in every fresh start is given up on after five. The count is kept
    across tool calls and into the fresh session, and a turn that ends starts it again, also while
    noctis is off;
  - observe mode counts no fresh starts and gives none up, and the first full context after it is
    started afresh as the first;
  - a cloud session is left to the person;
  - a context given up on, and the session `/clear` starts after a cloud session filled up, are
    handed the resume note;
  - only an `invalid_request` that names a full context, and only on Claude Code, is taken for one.
- `freshlaunch_test.go` (5 tests) starts such a session afresh even after a short pause, with no
  usage data and without waiting for a compaction of the context it leaves behind, in a 200k window
  and at the 280k point of a 1M window, and tells a session that has no handoff note why it
  stopped.
- `settingsrace_test.go` (2 tests): setup and uninstall keep a window that `/autocompact` saves
  while they wait.
- `cli_test.go`, `install_test.go` and `launch_test.go` (1 test each): setup refuses a
  `settings.json` that is not an object, a session start names it and leaves it as it is, and a
  relaunch leaves out the percent setup took back.
- `leanhint_test.go` (1 test) counts the status line's `ctx` to the point.
- `resetcheck_test.go` (1 test): a wait that starts after a reset check makes that check at once.
- `fableswitch_test.go` (1 test): a move off Fable at a Stop is journaled under the Stop hook.
- `wakequeue_test.go` (1 test): a turn that went on in the session's own window after its pause
  ended goes on with the queue at its Stop.
- `overloadbackoff_test.go` (2 tests): observe mode only journals the give-ups of an overload and of
  a failure it never retried, and giving up on an overload, a failure or a full context takes back
  the retry a runner put off to a reset.
- `lockrace_test.go` (2 tests): the lock of a hook killed while it held it is taken over at once,
  and an empty lock made a moment ago, which a writer may have just created, is left alone.

Thirteen existing Go test files changed:

- `cli_test.go`: `--compact-at 10k` exits 1 with nothing written, and a command-line child reads the
  managed settings its test writes to its sandbox.
- `failureretry_test.go` and `gitstatus_test.go`: a timeout now stands in for the `invalid_request`
  they used, which now means a full context.
- `install_test.go`: the `ensure` notice is captured.
- `leanconfig_test.go` and `leanlog_test.go`: they read the new keys and the new status text.
- `leanhint_test.go`: the `▲` tests mark the context at 90 % of the way to the point.
- `lockrace_test.go`: a held lock that names a process that is gone, also one made a moment ago, is
  neither swept nor taken over; the tests of a lock that names no process expect it to go after 2
  seconds, no longer as soon as one whose holder died.
- `main_test.go`: the suite also clears the variables that decide how Claude Code names models, a
  command-line child keeps the environment its test hands it, and no test reads the managed settings
  of the machine it runs on.
- `scheduler_test.go`: the sandbox has a managed settings directory of its own.
- `heldback_test.go`: the bound on reading a huge prompt is ten times longer under the race
  detector, which `racedetector_on_test.go` and `racedetector_off_test.go` tell it of.
- `schedulezone_test.go` and `digest_test.go`: the test of the digest runner in the user's time zone
  has the digests answered in its own goroutine, as the inbox server's goroutines read `time.Local`
  while the test changed it, which the race detector reported.

Outside the Go tests:

- `tests/lean.test.ts` has 74 tests, run with Claude Code 2.1.289.
- `tests/contract.js` checks that the StopFailure matcher starts the hook for every error noctis
  handles.
- `scripts/i18n.js` gives gofmt a larger output buffer, as `lang.go` is now past 1 MiB.
- `tests/hygiene.js` leaves out a checkout inside the repository, such as a worktree Claude Code
  makes for an agent.
- `tests/soak.js` (with `--hard 1`) and `tests/monkey.js` follow long sessions whose context grows
  in tokens of their model's window, under a compaction setting that changes as they run. They
  check the status line's share of the point, the compaction hooks at the point, turns that stop
  with the context full or with another `invalid_request`, and a turn that goes on in the session's
  own window after a pause. `tests/continuations.js` also fails a session driven by two relaunches
  at once.
- `tests/soak.js` no longer takes three things noctis does right for faults: a Stop that ends the
  pause its session went past in its own window, then pauses it again for a limit that still holds
  (Fable past its threshold), which the runner resumes like any other pause; a day spent wholly
  inside the accounts' limits, every session stopped to go on at a reset after it; and a fresh
  account checked while another session's chaos holds the usage endpoint down, which now answers
  for that check.

The results of the local rounds on this release are in its commit message.
