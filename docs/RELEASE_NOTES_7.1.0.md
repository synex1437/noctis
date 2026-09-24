# noctis 7.1.0

7.1.0 has three parts. The first is a second read of what noctis does around a session: what setup
and uninstall record and give back, how a prompt's language and route are read, the queue, the
waits and relaunches, and the usage readings the guard decides on. The second is the commands
people type: help, doctor, selftest, status, webhook, the queue commands, and the exit code a crash
leaves. Fifty-four product bugs came out of the two, and each fix came with a test that fails
without it. The third is new: lean compaction, which asks Claude Code to compact between turns
before the context is full and, while prompt caching is off, trims the old tool output the summary
is written from.

## The ones that matter

**Paid credits could be spent past the ceiling.** With paid credits refused, the ceiling was a
plain "used ≥ ceiling" test on stored data, while a pause point also stops on a burst and on a stale
reading's projection. With the thresholds switched off, a 5-hour window at 97 % whose last reading
had jumped 6 points, or a ten-minute-old 97 % reading projecting to 107 %, got no wait; nothing was
polled near the ceiling, and a paused guard (`noctis off`, `/noctis:pause`) checked it on stored
data alone. The next turns could run past 100 %, and the account paid for the overflow. When usage
data stopped arriving near the ceiling in a window whose threshold was off, noctis probed but never
paused; with only the weekly threshold on, it paused on the weekly window at 20 % until its reset
three days away. The ceiling now goes through the same checks as a pause point and is polled within
8 points of it, also while the guard is paused; a stop before it reads "usage limit about to be
reached (paid credits refused)".

**A newer, lower reading hid a higher one.** When the status line and the usage endpoint both held a
window, the newer file won even when it read lower, and the status line rewrites its file at every
render. With the endpoint at 93 % of the 5-hour window and a render at 85 % for the same reset, every
hook decided on 85 % and the session worked on past its 92 % pause point. Two readings whose reset
times are at most 120 s apart are now one window, and the higher reading is used.

**A 429 in `claude -p` could leave no retry.** In print mode, the Agent SDK and the GitHub Action,
Claude Code 2.1.281 starts the StopFailure hook while it is already shutting down and ends it at
once unless it registered in time. Measured against a local server that answers 429, with no
sign-in and no git repository, the 7.0.1 hook stored the wait and a live runner in 100 of 120 runs;
the others kept a wait without its runner, or nothing. The hook now hands the payload to a detached
copy of noctis about 8 ms after it is started, and the copy stores the checkpoint, the retry and its
runner: 58 of 60 runs, and in both losses the hook wrote nothing. With a usage endpoint that never
answers, `claude -p` also exits after about a second instead of 6 to 8.

**Unattended queue work stopped at a permission prompt.** The checklist noctis writes from a long
prompt lives under `~/.claude`, which Claude Code protects, so Claude's Read of it was refused in
`claude -p` and stopped at "Do you want to proceed?" in an interactive session, acceptEdits mode
included: an unattended job stopped at its first tick. The same held for a trusted
`.claude/TASKS.md` and for the resume note noctis hands a new session. noctis now answers Claude
Code's permission request with allow for exactly those files: the session's own queue file or
checklist, and a Read of the resume note that session was handed. Never for a link and never in plan
mode; every other request is left to you.

**After a `cd`, a session lost its queue.** Claude Code moves the hooks' working folder with the
session's `cd`, and the queue file was looked up only there. After `cd frontend && npm test` the Stop
hook no longer found `TASKS.md` and let the session stop with items open, a list prompt typed there
started a second checklist, a compaction there lost the queue, and a pause there relaunched in the
subfolder with no task list in its prompt. The queue is now looked up first in the folder the
session started in, and a wait records that folder for the relaunch.

**Setup kept putting the permission mode back to auto, and uninstall gave back the wrong values.**
Every setup run without `--permissions` set `permissions.defaultMode` to auto, so the next
`/noctis:setup` you ran to switch profile undid a mode you had chosen since, and every session after
that edited files and ran commands without asking. Each run also recorded what the run before it had
written as the value to give back, a run while the fallback role stood in recorded noctis's own
switch, and uninstall deleted an effort or a mode setup had never changed. Setup now changes the mode
only on an account's first setup or with `--permissions`, records the values from before the first
setup once, and uninstall gives exactly those back, leaves what setup holds no record of, and
forgets its records, so the next setup starts over.

**English prompts switched the session to Polish, Portuguese or Spanish.** The English word list
lacked most of the words English prompts are made of, while Polish listed i, do and to, Portuguese
do and as, and Spanish no: "do I need to use a mutex here" was read as Polish, and the session's
notices and status line switched language. On a new corpus of 410 prompts in the fourteen languages,
precision went from 95.0 % to 100.0 % and recall from 73.9 % to 90.0 %.

**Reviewing your own code went to the research agent.** The router's research words included the
singular review and benchmark, and Turkish stems that match verbs. A research word routes a prompt
without a code word in any session, and one with a code word in a session that has touched no file,
so "review my changes in the auth module", "benchmark the parser against the old one" and
"bağımlılıkları güncelle" got the lite agent, which has no Bash and may not edit code; "find the
source of the memory leak" and "why is the price wrong on the checkout page" got it in any session.
Only the plural nouns route now (reviews, benchmarks, sources, prices, trends, articles; price only
in "price of").

**A crash in `noctis check` let the caller through.** Every command exited 0 after a crash, so a
crash in `noctis check`, the documented gate for crons, CI and other agents, went on as if usage were
under every threshold, and a crash in setup was reported as success. A command you run now exits 1
and says where the details are; hooks and the status line still never fail the host.

**The workspace guard missed edits, commits and pulls.** It fingerprinted only the text of
`git status --short`, so another edit to a file that was already changed, a commit or a pull while a
session waited went unnoticed, and the resumed session worked from its old view of those files. The
fingerprint now also covers the commit HEAD points at and the size, time and mode of every path git
lists, the files in an untracked folder included.

**Two processes could hold `state.lock` at once.** On macOS and Linux, when a noctis process died
holding the lock, the waiters that judged it stale each removed it by path, so two of them changed
`state.json` at once and one change was lost (a wait entry, a hand-off claim), and releasing a lock
deleted the next holder's. The holder now keeps an flock on its lock file, a stale lock is taken over
by one process at a time, and a process releases only its own lock.

## Lean compaction

When Claude Code compacts a long conversation, the summary request carries all of it. With Claude
Code's early-access function hooks on, noctis's hooks module (`hooks/lean.js`, named in
`hooks/hooks.json`) does two things. It makes no model call of its own; the summary is still Claude
Code's.

- **Early, between turns.** When a turn of an interactive session ends with the context 70 % full
  (`compaction.compactAtPercent`), noctis asks Claude Code to compact right then instead of at its
  own, later threshold, once per crossing. It is not asked while Claude Code's own automatic
  compaction is off (`autoCompactEnabled: false`, `DISABLE_AUTO_COMPACT`, `DISABLE_COMPACT`), and it
  waits for a later turn while the 5-hour or weekly window is within 6 points of its pause point,
  since the summary request could carry the window past it. Claude Code 2.1.281 refuses a plugin's
  compaction in `claude -p` and SDK sessions.
- **Trimmed, while prompt caching is off** (`DISABLE_PROMPT_CACHING` set to 1, true, yes or on).
  In the older part of the conversation, a tool result or tool input over 2 000 characters keeps its
  first and last 1 000 around a `[noctis: N chars trimmed]` marker; a `Read`, `Grep`, `Glob`, `LS`,
  `WebFetch` or `WebSearch` result goes when the same call ran again later and did not fail; and
  `<system-reminder>` blocks leave older user messages. Your prompts, Claude's replies, the tool
  calls themselves and the last 6 assistant messages with everything after them are never touched
  (`compaction.keepTurns`, `compaction.maxToolResultChars`). The summary Claude Code computes ahead
  of time is then declined, since Claude Code discards it once a hook changed the rows.

With prompt caching on, noctis hands the conversation down as it is: the summary request reads
nearly all of it from the prompt cache, and a trimmed row changes the request from that row on. In a
session run through Claude Code 2.1.281 against a local stand-in for the Messages API, the trimmed
summary request would have cost 2.5 % to 6.1 % more at Opus 5.5 prices. Run the same way in the
interactive REPL, a turn that ended at 75 % compacted at once, and with `DISABLE_PROMPT_CACHING=1` a
tool result of 42 853 characters reached the summary as 2 138.

`/noctis:setup` writes `CLAUDE_CODE_ENABLE_FUNCTION_HOOKS=1` into `settings.json` when it is missing
and records it; uninstall takes it back only while it still holds what setup wrote, and a value you
set yourself is kept. The switch loads the hooks module of every installed plugin, not only
noctis's. `/noctis:setup --no-lean` turns lean compaction off (`compaction.lean: false`, which later
setups keep) and takes back a switch setup wrote. Without the switch, or on a Claude Code older than
2.1.281 (2.1.251 does not load the module even with it), compaction is Claude Code's alone, as in
7.0.1; once the context reaches 70 % the status line shows `ctx 72%▲` and the next prompt gets one
notice to type `/compact`, and `noctis doctor` names an older Claude Code with `claude update` as the
fix.

Each compaction noctis trimmed is a line in `~/.claude/noctis/compact.log`: `noctis status` counts
them ("compactions trimmed: 1, ~29k characters") and `noctis why` lists them among its decisions. A
lean setting of the wrong type or out of range keeps its shipped value, and `noctis status` and
`noctis doctor` name it. A local install (`noctis install --source`) now also copies the hooks
module `hooks.json` names.

Function hooks are early access and may change between Claude Code releases. A message noctis
trims goes to the summary without Claude Code's handle, so Claude Code rebuilds it from its role,
text and tool blocks alone, and anything else in it, such as an image or a thinking block, does not
reach the summary; a message it leaves alone stays whole.

## Safety

- **While a noctis skill ran, every Bash command was pre-approved.** The four skills declared
  `allowed-tools: Bash`, which Claude Code turns into an allow rule for every Bash command while the
  skill is active: measured with Claude Code 2.1.281, a `touch pwned.txt` after `/noctis:status` ran
  without approval. The model could also call setup and pause, which switch the permission mode or
  turn the guard off, through the Skill tool, which Claude Code answered with a permission request
  rather than a refusal. Each skill now pre-approves only its own noctis command, and only you can
  start setup and pause.
- On Windows the toast and relaunch scripts ran from a folder the binary guessed: with neither
  `NOCTIS_PLUGIN_ROOT` nor `CLAUDE_PLUGIN_ROOT` set, a binary copied out of its plugin folder took a
  folder above it as the plugin and ran its `scripts\notify.ps1` and `launch.ps1` with
  `-ExecutionPolicy Bypass`. They run only from the plugin folder that holds the binary now;
  otherwise an alarm shows no toast and a windowed relaunch runs headless.
- A `TASKS.md` committed as a link to a file outside the project drove the session once trusted,
  with Claude told to tick items in that outside file, and `noctis queue import` wrote through the
  link or created the missing target of a dangling one. Such a link is ignored now, and reported
  once; import refuses it.

## Setup and your files

- When setup had chained the status line you had at install, one you set later, with `/statusline`
  say, was overwritten at the next session start and saved nowhere, and uninstall put back the older
  one. The one you saw last is chained and given back now.
- A status line removed by hand after an uninstall was chained again by the next setup and put back
  by the next uninstall.
- A role's model was checked only for its characters: `setup --code opsu:max` wrote `"model":
  "opsu"` into `settings.json`, every new session started on it, and the doctor called it OK. A name
  Claude Code does not know is refused before anything is written, and the doctor names it; on
  Bedrock, Vertex, Foundry or a gateway, where Claude Code passes any id to the provider, the
  provider's ids are taken.
- A symlinked `settings.json`, `config.json` or hook file (stow, chezmoi, home-manager) was replaced
  by a private regular file at the first write, and the file it pointed to kept the old content; a
  file of mode 0644 became 0600 on every write. The link stays now, and a file keeps its mode.
- Setup's permission line now names the mode setup replaced and how to get it back, or says that
  setup left the mode as it was; uninstall says it set the values back instead of "removed".

## Limits and the Fable window

- The Fable window held back every fan-out, whatever model its agents ran on: an Opus session with
  the 5-hour window at 10 % and Fable at 72 % was refused every Workflow launch. It counts now only
  when Fable could run the agents.
- A Fable session whose Fable weekly window went over its pause point while it waited was relaunched
  on Fable, where noctis's own cap then held its first prompt, so a headless relaunch ended without
  doing the work. It comes back on the fallback role now, and a relaunch on the fallback runs at that
  role's effort instead of the code role's.
- At the Fable reset, an account whose `settings.json` named no model and no effort was left with a
  model it never set and the fallback's effort for good. The reset now takes back only what the
  switch added.
- The Fable reset also overwrote what was chosen during the switch: an effort picked since, and a
  setup run meanwhile, and it said "default model is fable again" when you had picked another. What
  you chose stays, and a setup during the switch is the new baseline.
- A pause that did not come from its pause point (the daily budget, a burst, missing data, an
  imminent compaction, the paid-credit ceiling) was announced like one at a lower level, which looked
  like a fault. Its notices add "Pause reason: …" now.

## Waits and relaunches

- A new session was handed the checkpoint of a session parked until the weekly reset, which its
  runner was going to relaunch on the same task, of a relaunched session that Claude Code's own
  auto-continue resumed, and of a session you had gone on with, so it was told to resume work that
  was done or would be done. Those checkpoints are no longer handed over.
- A relaunch that could not start sent three notifications: "resuming work" before anything was
  checked, "claude command not found" even for Codex and the other tools, and a last one that named
  no folder, although a resume from another folder works in the wrong one. It sends one now, with
  the reason, the folder and the command to run there.

## The queue

- Claude Code ends a turn by itself once a Stop hook has blocked it more than
  `CLAUDE_CODE_STOP_HOOK_BLOCK_CAP` times in a row (8 by default). With `queue.maxIdleContinues` at 9
  or more, or the cap set lower, Claude Code ended the turn first, so the "Queue not progressing"
  notice never came and the unattended session sat idle with items open. noctis gives up at the
  smaller of the two.
- The state cleanup deleted a checklist 7 days after its job started, also while the session waited
  through a weekly pause or Claude still ticked it, and the relaunch lost its task list.
- An issue ticked before the session's first stop, the usual case, was never closed on GitHub.
- A close `gh` refused (no login, no network, an expired token) went unnoticed and was never tried
  again. It is tried at the next stops, and after three failures logged to `errors.log`.
- `noctis queue trust --file TASKS.md --cwd <project>` read the file from the folder the command ran
  in, not `--cwd`, and trusted a file that did not exist, a typo included; `queue import` wrote the
  issues into a `TASKS.md` there. trust, status and import read `--file` from `--cwd` now, and trust
  and status refuse a missing file.

## Git and the status line

- The `git status` noctis runs at every pause and after every wait took `.git/index.lock`, so a
  `git add` or `git commit` run at that moment failed, and a status cut off at its 3-second limit left
  the lock behind; the git snapshot of a checkpoint failed while another git command held it. Both
  leave the index alone now.
- A chained status line that ran past its 4 seconds was stopped without the programs it had started,
  so one that hung on the network left a process behind at every tick. The whole process group
  (process tree on Windows) is stopped now.

## Commands you run

- `noctis --version` printed the status report and `-v` was an unknown command; `-?` was taken for a
  command; help left out `classify` and `schedule-preview`, and an unknown command offered commands
  only noctis runs.
- `noctis help` described seven commands wrongly in all 14 languages (`check` does not refresh
  usage, `off` pauses for 60 minutes, `cancel` without an id ends every wait, …) and sent readers to
  a file an install does not copy.
- `noctis resume` without `--sid` printed its usage in English and exited 0; it prints it in your
  language and exits 2.
- `noctis webhook` printed nothing and exited 0 in every case, and without `--title` and `--body` it
  posted an empty message. It says whether the message arrived, names why not, and sends a test
  message; a crash in it exits 1.
- `noctis selftest` had no verdict and always exited 0, said "notification sent" when no notifier
  could start, and its own probe counted as a hook pulse. It ends like the doctor now.
- `noctis doctor` failed for as long as `errors.log` held any line, routine warnings included, and
  the file only rotates at 512 KB; notes noctis writes for itself (the session-start summary, a
  skipped refresh without a sign-in, a missing desktop notifier) failed it for a day. Only the last
  day counts now, and those notes go to `guard.log`.
- The doctor failed the sign-in check when `fable.source` was off and needed no token, called an
  expired token missing, and on macOS named a credentials file that is not there, since Claude Code
  keeps the token in the Keychain; `noctis status` showed the untracked data as "not fetched yet".
- The doctor passed a status line or host hooks whose binary was gone, after a plugin cache prune or
  a moved folder.
- The Codex, Antigravity, Droid and Copilot doctors failed on the sleeper scheduler, which the
  Claude Code doctor calls normal, so on WSL, in containers and on servers they could never pass.
  The Codex and Antigravity doctors had no line for a bad threshold or allowed paid credits, which
  their self-check pointed at.
- Half of the doctor's failure lines, and the Codex and Antigravity usage lines and a bad role
  value, said what was wrong and not what to do; each has a fix line now. A mismatched effort shows
  what `config.json` expects.
- The doctor's first line names the noctis version, so a pasted report says which build wrote it.
- `noctis status` and the Codex doctor printed internal refresh codes (`http-0 Get "https://…"`,
  `no-token`, `bad-json`) and never said when a refresh that backs off tries again. They say it in
  words now, with "next try after <time>".
- `noctis schedule-preview` showed the reset watcher instead of the sleeper the scheduler starts, a
  systemd line that split when pasted, and a `task` preview that rewrote the launcher pending Windows
  tasks run; it took any backend and `--at NaN`.

## Also in this release

- The macOS relaunch fix named in the 7.0.1 notes, for a launcher whose path has a space or a quote,
  landed after the `v7.0.1` tag; 7.1.0 is the first release that carries it.

## Known limits

- The language detector still reads an English prompt made of words Polish shares plus one Polish
  word (nic, z, u, po, od) as Polish, and leaves short Italian and Portuguese prompts whose other
  words are shared undecided; an undecided prompt keeps the session's language.
- The router still sends 28 own-work prompts of its corpus to the lite agent in a session that has
  touched no file, and 8 in a coding one; `main:` and the misroute learning remain the way out.
- A skill's pre-approval does not match where the plugin's path holds a parenthesis; its commands
  then go through the permission settings like any other.
- A lock more than two minutes old whose dead holder's pid now belongs to another running process
  still counts as hung. Windows keeps its lock steps: a lock that is held cannot be deleted there.

## Tests

Every fix above came with a test that fails on the code just before it. Some run only where they
can: `TestReleasingALockLeavesASuccessorsLockInPlace` skips on Windows, where a held lock cannot be
removed to make room for a successor; the symlink tests of the settings and hook file fix skip where
the machine cannot create a symlink; and the toast and launcher tests of the Windows scripts fix run
only on Windows, in CI's Windows job. The lean module's 65 tests run under Claude Code's own test kit
(`CLAUDE_CODE_ENABLE_FUNCTION_HOOKS=1 claude plugin test .`); the lab runs them, with
`claude plugin validate --strict`, when a claude 2.1.281 or newer is on PATH, which CI's weekly
real-tools job installs. The language detector and the router now have corpora whose numbers
`go test ./cmd/noctis -run LanguageCorpus -v` and `-run RouterCorpus -v` print. `go test -race` is
clean: tests that run two hook flows in one process raced on shared caches, which take a lock now.
Local rounds, three times on Linux with claude 2.1.282, Go 1.24.7 and node 22: go test with Windows
and macOS builds and vets, 30 seconds of each of the five fuzz targets, hygiene, i18n, contract,
chaos, scheduler, lab, a two-day hard soak, the torrent and two monkey seeds; and one
`go test -race` run.
