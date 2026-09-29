# noctis 8.0.0

8.0.0 is for projects that take days: a plan of dozens of steps that Claude works through on a
server while nobody watches, a few of them steps only a person can do and some that wait on the
world outside. A trusted queue file can name its own check between items; items marked `(human)`
stay yours; an item that waits on something outside the session is set aside with a reason instead
of stalling the night; a long build or test suite runs as a `noctis job` the Stop hook waits for; a
new commit counts as progress on a long item; a relaunch opens in the tmux session claude ran in;
and `noctis queue status --json` tells a script where the queue stands. A run nobody watches, such
as `claude -p` from a script, is no longer driven by the project's queue.

8.0.0 also carries 7.6.0, which was never released on its own: the 73 fixes of an audit of 7.5.2,
the Sonnet 5.5 roles of the Code and Balanced profiles and the short README. It carries everything
in 7.5.2 ([RELEASE_NOTES_7.5.2.md](RELEASE_NOTES_7.5.2.md)).

## If you are upgrading

8.0.0 adds two settings, `queue.headless` (false) and `queue.fileVerify` (true), and `hooks.json`
is unchanged.

- **A run nobody watches is no longer driven by queues.** A session whose `CLAUDE_CODE_ENTRYPOINT`
  is `sdk-cli` (`claude -p`), `sdk-ts`, `sdk-py` or `claude-code-github-action` gets no `TASKS.md`,
  no checklist from its prompt and no continuation, unless `queue.headless` is true,
  `NOCTIS_QUEUE=on` is set or its queue was started in it with `/noctis:start`. If you drive such
  runs with a queue on purpose, set one of the first two. noctis's own relaunches are driven as
  before.
- **A `noctis-verify:` line in a trusted queue file now runs.** A line such as
  `` noctis-verify: `bash scripts/verify.sh quick` `` outside code fences becomes the check between
  that queue's items, ahead of `queue.verifyCommand`, once you trust the file with the line in it.
  If a file of yours holds such a line for something else, set `queue.fileVerify` to false.
- **An open item with `(human)` or `(insan)` in its text is yours.** Claude skips it and cannot tick
  it in a turn noctis started, and the items that wait for it stay blocked until you tick it. Take
  the word out of an item Claude should do.
- **A new commit counts as progress.** A session that commits between stops is no longer given up
  after `queue.maxIdleContinues` stops without a ticked item; `queue.maxContinuesPerSession` still
  bounds it.
- **A tag can be written in any alphabet.** `#ödeme` is a tag now and `#café` is read whole, where
  7.5.2 read `#caf`; tags compare with letter case aside. An `(after #…)` that 7.5.2 reported as
  matching nothing may now hold its item back.
- **Code and Balanced were re-tuned.** A configuration that holds the roles 7.5.2 gave either
  profile keeps them, and status, the doctor and the session start say the profile was re-tuned in
  8.0.0; `/noctis:setup --profile code` or `--profile balanced` adopts the new roles. Search and
  SYNEX are unchanged. See "Setup profiles: Sonnet 5.5" below.
- **Sonnet 5.5 needs Claude Code 2.1.284 or later.** Older versions resolve the `sonnet` alias to an
  earlier Sonnet. When a role runs on `sonnet` and the last session noctis recorded ran an older
  Claude Code, status, the doctor and setup say so, with that version and `claude update`.
- **`thresholds.weeklyScoped` set to `0`, `null` or `false` now switches the scoped guard off
  (E6).** 7.5.2 passed it over and fell back to `weeklyFable` (97), so a Fable session still moved
  to the fallback model at 97 %.
- **A code role without an effort level sets none.** 7.5.2 kept writing the old `models.effort`
  (often `max`) into `settings.json` and every relaunch. Run `/noctis:setup` once to take back a
  level that setup wrote and the role no longer has.
- **A queue give-up recorded by 7.5.2 is lifted once**, since the give-up now records every open
  item; the stop after it gives up again if nothing changed.
- **A pause 7.5.2 scheduled keeps the time it was given.** Its systemd timer or launchd job may
  still fire hours off when your shell's `TZ` is not the system's zone; pauses scheduled after the
  upgrade resume on time.

## Long projects on a server

**A run nobody watches is left alone.** A project whose scripts call `claude -p` for one answer
each, from a cron job, a bot or a GitHub Action, had those runs driven by its trusted `TASKS.md`
like your own session: the Stop hook held them to the queue, the session start told them to work
on it, and a list in their prompt became a checklist. Queues now drive a run whose
`CLAUDE_CODE_ENTRYPOINT` is `sdk-cli`, `sdk-ts`, `sdk-py` or `claude-code-github-action` only when
`queue.headless` is true, `NOCTIS_QUEUE=on` is set, noctis itself relaunched it to go on with a
paused session, or its queue was started in it with `/noctis:start`. A Claude Code on the web
session is not such a run. `NOCTIS_QUEUE=off` keeps every queue out of a session and the processes
it starts, and `/noctis:start` says so instead of starting one. A pause records whether a queue
drove the session; the relaunch of one that none drove runs with `NOCTIS_QUEUE=off` and names no
queue in its prompt, so a script's run that hit a limit is not handed the plan when noctis resumes
it in a terminal.

**A queue file names its own check.** `queue.verifyCommand` is one command for the whole account,
so a project with a check of its own had to set it for every project and keep it quiet elsewhere
with tricks such as `test ! -f scripts/verify.sh || …`. A queue file may now carry a line of its
own, outside code fences:

    noctis-verify: `bash scripts/verify.sh quick`

The first such line is the check between that queue's items and wins over `queue.verifyCommand`.
The command runs without a permission prompt, so it runs only while your trust covers the file as
it reads now, also with `queue.requireTrust` off: a line added or changed later, by Claude, a pull
or a branch switch, names nothing until you trust the file again. Checklists noctis writes never
name one, and `queue.fileVerify` false ignores the lines. `noctis queue trust` and `status` print
the check and where it comes from, or that the file's line waits for your trust, and a hold caused
by the file's check is notified in words of its own.

**Steps only you can do are marked `(human)`.** A plan for a long run has steps only a person can
do: open an account, pay, sign, review. Kept in the queue file, they became Claude's next item, so
it stalled on them or ticked them itself; kept out of it, the items that need them lost their order.
An open item whose text has `(human)` or `(insan)` is now yours: it is not counted as open work and
is never Claude's next item, and the items that wait for it through `(after #tag)` stay blocked
until you tick it. When only such items and what waits for them are left, the Stop hook runs the
queue's check if one is due, then lets the session stop without calling the queue finished, and you
hear once per set of open items which ones are yours, with a desktop notification and the webhook if
one is set. In a turn noctis started (a queue continuation, a relaunch prompt, a turn Claude Code
wrote, a check's send-back) Claude cannot tick, reword or remove an open `(human)` item of the queue
that drives the session: the PreToolUse hook refuses the Write, Edit or MultiEdit and tells you. In
a turn you typed, and while noctis is paused, Claude may tick one you say is done.

**An item that waits is set aside, not stalled on.** An item can wait on something outside the
session: an API key not issued yet, an app review, a DNS change. Claude stalled on it, and after a
few stops without progress the queue gave up with every other item still open.
`noctis queue defer <item> --reason "<why>" [--until 2d]` now sets it aside: it is not counted as
open work and is never the next item, and the items that wait for it stay blocked. The item is
named by a part of its text that only it has, by its number or by a `#tag` for a group, whose
`(human)` items stay yours. The deferral is kept in noctis's state, so the file and its trust stay
as they are. `--until` ends it by itself (a span such as `90m`, `2h` or `3d`, or an RFC 3339
time), `noctis queue undefer <item>` or `--all` ends it at once, and a deferral is dropped after
90 days. A reason is required. Claude may run both commands: the session start says how, and a
continuation after a stop without progress says it again. When only deferred items, `(human)`
items and what waits for them are left, the session stops, and you hear once which items are
deferred and why.

**A long command runs as a job.** A long build, a full test suite or a migration no longer has to
fit in a tool call's time limit or be polled turn after turn.
`noctis job run --label <name> -- <command>` starts it apart from the session, in a process group of
its own on macOS and Linux, with its output in a log under `noctis/jobs`, and records it for the
Claude Code session that started it; `noctis job list`, `stop <id>` and `forget <id>` (or `--all`)
list, stop and forget jobs. When Claude ends its turn while its job still runs and a trusted queue
drives the session, the Stop hook waits for the job as long as an in-hook wait may, looking every
ten seconds, then hands Claude how it ended with the last 20 lines of its log (terminal colors
removed, a line that redraws itself in its last state). If the wait runs out, Claude hears what the
log shows so far and decides; `noctis off` ends the wait. A session no queue drives is never held:
its jobs are reported at the first stop after they end, and a fresh session hears of the jobs of the
session it took over from. The queue instructions suggest jobs to Claude only where the Stop hook
may wait long enough, which is Claude Code. Everything after a bare `--` is left to the command, so
a job keeps its own flags. The trust guard looks into a job's command line, so a job cannot trust a
queue or write noctis's state, and a `--log` among noctis's own files is refused, also through a
link. A job's record is dropped three days after the job ended or was reported, and 30 days after it
started in any case; a log in `noctis/jobs` that no record names is removed once it is three days
old.

**A commit is progress.** The Stop hook let a trusted queue go after `queue.maxIdleContinues`
stops in a row with no item ticked. On a large project one item can take many turns: Claude
builds, tests and commits a piece at a time and ticks the item only at the end, so the queue gave
up on work that was moving. A stop now also counts as progress when `HEAD` names another commit
than at the previous stop. `HEAD` is read from the repository's files, as the StopFailure hook
does, so a stop never waits for git; the first stop of a session only notes it. A session that
neither ticks nor commits still gives up after the same number of stops.

**A relaunch opens in tmux.** A server has no desktop, so a relaunch there ran headless, with its
output in a log nobody watches. When claude runs inside tmux (`TMUX` is set and `tmux` is on the
`PATH`), the relaunch now opens in a new window of the same tmux session, before macOS Terminal or
a Linux desktop terminal; a `resume.terminal` template still comes first. The scheduled runners
carry `TMUX` with the other variables they restore, so a relaunch after a usage-limit wait finds
the tmux server too. `noctis doctor` names the window a relaunch opens in, or says that it runs
headless where nobody sees it and how to change that.

**Where a queue stands.** `noctis queue status` now counts, for a trusted file, the open and the
done items and names the item that comes next and how many wait on others. For any file it lists
your `(human)` items and the deferred ones with their reasons and names the check between items and
where it comes from; while a failing check holds the queue, it says since when, which command
failed how often and how to lift the hold. `noctis queue status --json` prints the same as one JSON
object, for a script or a chat bot that watches an unattended run. `noctis queue verify` runs the
queue's check now, in the folder the Stop hook runs it in: a pass records the ticked items as
checked and lifts a hold, so the next session to start or stop there is driven again; a failure
prints the end of the output, exits 1 and changes nothing. `noctis queue trust` of a file that
changed since its last trust lists the lines the new trust covers, and the notices about such
lines at session start and stop say that `noctis queue status` lists them.

**Tags in any alphabet.** A tag was an ASCII letter followed by ASCII word characters, while an
`(after …)` reference could be written in any alphabet: `#ödeme` tagged nothing, `#café` was read
as `#caf`, and `(after #ödeme)` matched no item, so the item was not held back and the reference
was reported as a typo. A tag is now a letter of any alphabet followed by letters, digits, `_` or
`-`, and tags compare with letter case aside, Turkish's dotted and dotless i included: `#IŞIK`
answers `(after #ışık)` and `#İşlem` answers `(after #işlem)`. `noctis queue defer #tag` compares
the same way.

The guides have a section on all of this, "Large projects on a server" in
[GUIDE.md](GUIDE.md#large-projects-on-a-server) and "Sunucuda büyük projeler" in
[GUIDE.tr.md](GUIDE.tr.md), and the README answers "Can it carry a large project on a server
for days?". The new notices, help and usage lines are in all 14 languages.

## Fixes from the audit of 7.5.2

The audit of 7.5.2 found 73 defects (M1, U1–U11, R1–R11, H1–H8, Q1–Q9, E1–E8, S1–S5, I1–I9 and
X1–X11). Each is fixed with a test that fails on the code before it.

### The ones that matter

**A subagent's report became your checklist (M1).** Claude Code hands a subagent's final report, a
message from another session and a background task's notice to the session as a user turn, and
noctis read such a turn as a prompt you had typed: the bullets of an audit report, 13 of them in the
test, became a checklist, and the Stop hook drove Claude through another agent's words. A turn that
holds one of the markers Claude Code writes around such a turn (`<task-notification>`,
`[Subagent hand-back]`, "Another Claude session sent a message:" and the others) is never read as a
job now, and a checklist your own prompt started stays as it is. A listed line that only gives a
finding's field ("Where: file.go:12", "Fix direction: …") or reports a passed check ("go test ./...:
14 passed") is no longer a step either, in any prompt: it is kept under "Not on the checklist".

**A status line that calls noctis ran without end (U1).** A status line of yours that runs
`noctis statusline` itself, to show noctis's line among its own parts, is chained like any other.
Every refresh then ran noctis, your script, noctis again, without end, and each noctis started its
chain in a process group the first one's 4-second limit could not stop. The chained command now runs
with `NOCTIS_STATUSLINE_CHAIN=1`, and a noctis that finds it prints its line but chains nothing.

**A pause on Linux or macOS resumed hours off its time (R1).** The systemd timer and the launchd job
got the resume time as a wall-clock time in noctis's zone, which Go takes from `TZ`, while systemd
and launchd read it in the system's zone. With a `TZ` in your profile on a UTC machine the resume
came hours late or early, and when the runner rescheduled for a time already past for the manager,
the timer never fired and the resume was lost. systemd now gets the time in UTC and says so; the
launchd job's time is worked out in the system's zone, read from `/etc/localtime`.

**An overload was retried for the whole outage (H1).** The prompt that carries a retry, and the end
of every headless relaunch, deleted the overload episode, so each failure started over at attempt 1:
the delay stayed near 30 s, the "API overloaded" notification went out on every retry and the retry
budget was never spent. The episode now ends only on progress (a batch of tools or a finished turn),
and a failure more than twice `overload.maxSeconds` after the last one still starts a new one.

**`/clear` in one window cancelled another window's pause (H2).** With two windows in one project,
the session start after `/clear` could take the other window's session for the one it cleared and
release its wait: that window went on with "wait cancelled", or was never woken. A session whose
hook still holds its wait is now taken for another window's, the cleared one is the one seen last,
and the SessionEnd hook of the cleared session releases its own sleeping wait.

**A prompt that held the work off still started it (Q1).** "Please hold off on these until Monday",
"I'm not asking you to do these", "Don't start on this until tomorrow", "Give me a time estimate for
each of the following", the Turkish "Bunları şu an yapmanı istemiyorum" and German, Spanish and
Russian forms became checklists that the Stop hook then drove. They hold the list now. A job prompt
that ends "Give me a short summary at the end" keeps its checklist.

### Queues

- **A rule inside one step, or on the code of a tests or docs job, keeps the checklist (Q2).** "…
  don't change the code here" written in one item, "Write tests for all of these modules; don't
  change the existing code" and "Bunların hepsini sırayla yap, şimdilik deploy yapma" lost their
  checklist. The hold-back scan reads the words around the list; a listed line counts only when it
  holds back the whole list.
- **A fresh session that took over a checklist is checked like its first session (Q3).** After a
  relaunch into a fresh session, Claude could write any line into the checklist it took over, since
  the job-word check looked only at the new session's own file.
- **The trust guard reads a command the way the shell does (Q4, Q5).** A call to
  `noctis queue trust` or `noctis state-write` could slip past it through ANSI-C quoting, a
  backslash-newline, brace expansion, a glob in the name, a here-document behind a `#` comment or
  PowerShell's `--%`. The guard now applies the shell's own steps before it compares; a mention
  inside a comment passes like a quoted one. It still cannot see a name built at run time.
- **A blocked queue is announced once (Q6)** until its blocked items change, instead of a
  notification, a webhook and an error line on every stop.
- **Any edit of an open item lifts a give-up (Q7),** also an item past the first 15, the end of a
  long item and one that waits on another.
- **A Turkish I in the other case is the same word (Q8).** A job that wrote "Işık" where the prompt
  said "ışık" was refused as a word the prompt did not have.
- **More ways of writing a step keep it on the checklist (Q9):** "… kontrol et lütfen", "… yedek
  almayı unutma", "For the login page add rate limiting", "I want you to add a retry with backoff",
  "It needs a dark mode toggle".
- **`noctis queue import`** asks gh for each author's open issues, so yours are not hidden behind
  two hundred newer ones by others, and says what gh listed (X4); writes the file only when it
  changes, in one step, keeping its byte order mark and line endings (X5); turns line breaks and
  invisible characters in a title into spaces (X6); and adds new items under the file's own
  `## GitHub issues` heading (X7).

### Hooks, waits and wakes

- **`model_not_found` changes `settings.json` only when its model is the one that failed (H3).** A
  session on another model lost a valid default.
- **A paused guard no longer lets a prompt through in a window whose session went on elsewhere
  (H4),** so one process drives a session during `/noctis:pause` too.
- **noctis's own files and the checklist are guarded under any name that reaches them (H5):**
  another letter case on macOS and Windows, or a hard link.
- **Observe mode leaves the checkpoint it does not hand over (H6)** for the next session.
- **A subagent's own Agent or Task call gets the pinned model and the fallback (H7).** A nested
  spawn ran on the scoped model at 97 %.
- **A same-session wake sleeps no longer than its hook may run (H8).** With `wake.maxMinutes` above
  about 359, Claude Code killed the hook before it woke the session; in a cloud session nothing else
  resumed it.
- **A cloud session tells you to resume a pause no hook can hold (E3)** instead of promising an
  auto-resume that nothing would keep.
- **The turn a same-session wake woke goes on with the queue (E4).** The stored wait kept the Stop
  hook from driving the checklist until someone typed.
- **A wake you took over teaches no hook timeout cap (E5).** Two such wakes could teach noctis a cap
  that sent every later pause to the runner, and in a cloud session to no resume at all.
- **A hook that returns because noctis is off still records the pulse (U3),** so `noctis off` for
  more than 30 minutes no longer reads as dead hooks.

### Limits and usage

- **The quiet path measures each window against the paid-credit ceiling (E1).** With a threshold
  off, or above `credits.ceiling`, a batch at 100 % could pass on the quiet marker.
- **A weekly reset read seconds apart is the same week (E2).** The daily budget took it for a new
  week and counted the whole week as used today: a false "daily budget reached", and with
  `budget.hardStop` every turn paused until midnight.
- **`NaN` and infinities are not numbers (E7).** `"ceiling": "NaN"` made a ceiling that never
  stopped anything.
- **The daily budget starts its day without a status line (E8).** A session whose usage came from
  the usage endpoint alone never started its day, so `budget.dailyWeeklyPercent` never applied.
- **A window that has reset never hides the live one (U2).** An idle session's status line wrote its
  old 5-hour window over the live one, and a session at 93 % under a 92 % pause point ran on.
- **Antigravity buckets are placed by their names first (U4),** so the weekly bucket no longer
  becomes the 5-hour window once a week.
- **An expired sign-in is tried again after 2 minutes (U5)** and named as expired, not as missing.
- **A chained status line that exits non-zero keeps its line (U6);** only one cut off at its 4 s
  limit still shows nothing.
- **The older usage answer reads the scoped model's own weekly bucket (U7).**
- **Near an edge the subagent and paused-guard checks wait for the fetch (U8),** as the prompt check
  does, for up to 1.5 s.
- **A clock offset stops moving reset times 30 minutes after its fetch (U9).**
- **On macOS a credentials file without a live token no longer hides the Keychain's (U10).**

### Relaunches and the scheduler

- **A hand-off holds the session only while its process ID still names the runner (R2),** and the
  window runner stops watching a process ID that has passed to another program (R3). On Windows,
  which hands a freed ID out again at once, a dead runner could keep a session refused in every
  window.
- **On Windows the runner's helpers start without a console window (R4):** PowerShell for each task
  registration, git, taskkill, headless sessions and the console launcher. The scheduled task itself
  still opens its console window for the relaunch, as in 7.5.2 (see "Known limits").
- **A Windows Terminal tab that starts late no longer opens the session a second time (R5).**
- **An early resume stops the systemd timer its pause waited on (R6),** which otherwise fired days
  later.
- **A profile path with a typographic apostrophe (`C:\Users\O’Neil`) schedules its task (R7);** the
  task script failed to parse and the wait fell back to a sleeper.
- **On macOS a helper under a path with a space is known as noctis's (R8),** and the relaunch window
  hands its shell to the session and carries the session's variables (R9).
- **A `$` in the account folder's path reaches the systemd runner unchanged (R10).**
- **A long-lived noctis reaps the processes it hands off (R11)** instead of leaving zombies.

### Files noctis keeps

- **A backup write that fails leaves the older backup whole (S1).** A full disk could leave a cut
  `state.json.bak`, and a later broken `state.json` then started empty. The backup is written
  through a temporary file now, which costs about 0.1 ms per write that changes something.
- **Only a holder of the lock restores or clears `state.json` and `usage.json` (S2).**
- **`used-checkpoints.json` is guarded as noctis's own state (S3).**
- **A write that fails part way removes its temporary file (S4).**
- **Log rotation keeps the history (S5).** Two processes that found a log due at once could move the
  fresh log over the history the first had just kept. Rotation now takes `rotate.lock`. On Windows a
  writer opens the log so that the rotation can move it aside while it is open, and a writer that
  opens it during the move no longer loses its line.

### Setup, install and uninstall

- **An install run through a symbolic link to its own source no longer deletes the binary (I1).**
- **The status line setup chains keeps its other fields (I2),** such as `padding`, and uninstall
  puts it back whole.
- **A code role without an effort level sets none, and relaunches pass none (I3).**
- **`noctis on` says so and exits 1 when it could not save (I4).**
- **Uninstall removes the `env` and `permissions` objects setup added once they are empty (I5).**
- **Setup and uninstall write `settings.json` under its lock (I6)** and keep what others wrote there
  meanwhile, such as a `/model` change.
- **The install note for a host without limits says whether failures are retried there (I7).**
- **The auto-update hints point to the `/plugin` menu (I8),** not a command Claude Code refuses.
- **The guides give the clone install's file count: 19, 20 on Windows (I9).**

### Reports and the webhook

- **A webhook address that does not parse is never logged (X1),** and the bug-report bundle redacts
  a webhook address in any form.
- **The bundle redacts `queue.verifyCommand`, `resume.terminal` and passwords in any URL (X2),** and
  the held-queue notification names the setting instead of quoting the command.
- **`noctis why --last 1e19` no longer crashes (X3).**
- **The cost report prices `claude-3-5-haiku` and `claude-3-7-sonnet` ids, keeps list prices a
  partial `report.pricing` leaves out, and bills one-hour cache writes at twice the input price
  (X8).**

### Skills copied into a project

Claude Code on the web installs no plugins, so a project that wants noctis there copies its skills
into `.claude/skills`, where they are typed `/noctis-pause` and so on. noctis now names those
commands in its notices and takes them like `/noctis:pause` (X11).

## Setup profiles: Sonnet 5.5

Sonnet 5.5 costs $2 / $10 per million input / output tokens, against $4 / $20 for Opus 5.5, and at
max comes close to it: SWE-Bench Pro 81.3 against 89.9, CursorBench 55.5 against 57.8, GDPval-AA
1844 against 1846, and it leads Terminal-Bench 4.0 with 70.6 against 64.8. At xhigh and above it may
start review rounds of its own, and at low and medium it may stop a long task to check in, so the
profiles run it at high only:

- **Code** researches and writes on Sonnet 5.5 at high, and still codes on Opus 5.5 at xhigh.
- **Balanced** codes, researches and falls back on Sonnet 5.5 at high. Planning stays on Opus 5.5,
  digests and file search on Haiku 4.5.
- **Search** and **SYNEX** are unchanged; SYNEX is still the only profile at max.

The research agent (`agents/lite.md`) now searches before it answers anything that can have changed
since training, and the cost report prices `sonnet-5-5`.

## The README

The README was 58 KB in English and 67 KB in Turkish; each is now 159 lines (14 KB and 15 KB), with
the details in [GUIDE.md](GUIDE.md) and [GUIDE.tr.md](GUIDE.tr.md). The English status line examples
read `41%`, as English prints them (U11).

## For contributors

- **The reference names every command the host or noctis starts, and the shipped roles profile
  (X9);** two tests keep it in step with the code.
- **The weekly real-tools job opens its issue from a job of its own that may write issues (X10),**
  and hygiene fails on a job that writes issues without that permission or installs code with it.
- Lab scenarios run on their own read a missing `errors.log` as empty; the torrent keeps each
  window's reset near the clock; the soak ends an overload episode with a batch of tools and lets an
  in-hook wait hold the host up to its one-minute cap.
- The test of a torn `state.json` read writes the file whole inside the reader's wait instead of
  racing it, so a busy runner no longer fails it.

## Known limits

- **A deferral whose `--until` passes does not wake a session that stopped on it.** The next
  prompt, relaunch or session start goes on with the item.
- **The Stop hook waits for a `noctis job` only while a trusted queue drives the session, and only
  where the hook may run long enough (Claude Code).** Elsewhere Claude hears of the job at its next
  stop after it ends.
- **A relaunch opens in tmux only when the claude that paused ran inside tmux.** On a server without
  it the relaunch runs headless, with its output in `resume-output.log`.
- **`(human)` items are guarded against Claude's Write, Edit and MultiEdit calls,** not against a
  shell command that rewrites the queue file; the queue instructions tell Claude to leave them
  alone.
- **Step reading still misses some shapes.** A need without "to" ("We need a CSV export") and a
  scene after "if" or "when" without a comma are read as notes, not steps, and a symptom written as
  a need ("It needs a restart every few days") now reads as a step.
- **A prompt you type that quotes one of Claude Code's turn markers gets no checklist.**
- **The trust guard cannot see a name built at run time,** such as a variable's value or the output
  of a substitution.
- **A checklist started before the upgrade keeps the word digests it was given,** so a job line with
  a Turkish ı added to it is refused until the next prompt.
- **On Windows the scheduled task still opens a console window for the relaunch,** as in 7.5.2, and
  closing it stops the runner. Running the task in `conhost.exe --headless` would hide it, but that
  form could not be proven under Task Scheduler and security tools flag it, so 8.0.0 keeps the
  `cmd.exe` action.
- The limits in [RELEASE_NOTES_7.5.2.md](RELEASE_NOTES_7.5.2.md) still apply.

## Tests

207 new Go tests since 7.5.2, four of them on Windows only: 140 for the audit fixes and the
profiles, one of them a fuzz test of the trust guard's shell reading, and 67 for the long-project
work. The labs cover the fixes where a lab reaches them. The results of the local rounds on this
release are in its commit message.
