# noctis 8.5.1

8.5.1 fixes the defects a deep review of 8.5.0 found in the usage-limit guard, pauses, the queues,
the runners and digests, the language noctis speaks, the commands, the installers, the files noctis
keeps and the lean compaction module. Each fix comes with a test that fails without it. 8.5.1 adds
no feature, and carries everything in 8.5.0 ([RELEASE_NOTES_8.5.0.md](RELEASE_NOTES_8.5.0.md)).

## If you are upgrading

8.5.1 adds no settings and reads every file 8.5.0 wrote. A queue file trusted on 8.5.0 asks for one
new `noctis queue trust` if it holds an HTML comment over several lines, a check line right under an
item, a line that starts with *Todo* without a colon, or an item marked `(BİTTİ)` in a list without
checkboxes, because 8.5.1 reads other items from it (see [Queues](#queues)). A few answers change:

- A role saved without an effort (`--fallback sonnet`, the retired Balanced profile's fallback) now
  runs with none. 8.5.0 put the shipped role's effort back each time it loaded the config.
- `noctis cancel --sid` with an empty value (`--sid`, `--sid=`, `--sid "$SID"` with `SID` unset, or
  `noctis cancel ""`) cancels nothing and exits 2 with `--sid needs a value`. 8.5.0 cancelled every
  session's wait.
- `noctis check --sid` takes the short id `status`, `why` and `off` print, as `model --sid` does; an
  id that fits several sessions lists them and exits 2.
- `noctis report --days` stops at 3650, as `why --stats --days` does.
- In `usage.json` each session's record carries `windows`, the reading its own status line showed
  last of each window.
- `noctis cancel` with an option it does not know (`--id`, `--session`) cancels nothing and exits 2.
- A deferral with an `--until` more than 90 days ahead holds until then; 8.5.0 dropped it after 90
  days.
- `noctis report --json` leaves `costOnPrimary` and `savedVsPrimary` out of `keptOffPrimary` when
  noctis has no price for the primary model, as the text and HTML reports leave out their estimate.
- The decision journal's `defer` entry names the item: its `reason` is `<item>: <what it waits on>`,
  and `waitsOn` holds what it waits on.
- The decision journal dates the setting of a queue wake, of the digest's runner and of a runner's
  next check when it happens; the time each is set for moves to `wakeAt`, `dueAt` or `checkAt`.
  Lines 8.5.0 wrote keep that time in `at`.
- `noctis queue import` into a list without checkboxes adds nothing and exits 1 (see
  [Queues](#queues)); give the list's items checkboxes first.
- `noctis queue import` into a file outside the git checkout it lists the issues from writes
  `owner/name#12`. A bare `#12` an older import wrote there with the same title is rewritten once to
  name its repository, so that file asks for a new `noctis queue trust` after the import.
- `NOCTIS_LANG` or `locale` written with a region (`pt_BR`, `de_DE.UTF-8`) pins its language. A
  value that names no language noctis has counts as `auto`, so the language of your prompts is
  followed; 8.5.0 spoke English and stopped following it.
- A webhook that answers with a redirect is no longer followed: the delivery fails with its status
  (`http-301` and the like). Put the address it redirects to in `alarm.webhook.url`.

## Usage limits

**A projection paused work at 2 % of a window.** The burn-rate projection took its rate from the
last two readings of a window, and the status line records a reading only when the percentage
changes, so the two were often seconds apart: 1 % and then 2 % in the same minute read as a window
on its way past its pause point. A tool call of a few minutes later, the hook paused on that
projection without asking the usage endpoint, and because the line ran past 100 % as well, it
parked the prompt you typed, also with `noctis off`. The rate now comes from readings at least 60
seconds apart, and before a projection alone pauses work, the hook asks the endpoint (waiting up to
1.5 s, as near a pause point).

**After an early reset, an idle session brought the old window back.** When the provider resets the
limits early, a session that got no answer since still shows the week before it, with its reset
days ahead, and it repaints its status line. That reading replaced the new week's, and the guard
paused on a week that had already ended, until its old reset. Each session's record now keeps what
its own status line showed last, and a session that shows the same reading again, of a window that
ends more than 2 minutes before the stored one, is passed over. A session that shows a new reading
is taken as before.

**On Antigravity a reset time that moves by a second split one window in two.** Antigravity gives
each window a countdown, and noctis adds it to its own clock, so the same reset could land a second
apart from one render to the next. The burst pause and the multi-session maximum compare reset times
exactly, so both stopped working: a 21-point jump to 75 % paused nothing, and an idle conversation's
85 % replaced a busy one's 93 %. A reset worked out from Antigravity's countdown now counts as the
stored one when it lands within 2 minutes of it and the stored one has not passed yet.

**On Codex the blind probe outlived the hook.** Near a pause point with no fresh data, noctis probes
the usage source `usage.blindProbeRounds` times, `usage.blindProbeSeconds` (60) apart, before it
pauses. Codex gives the hook that starts a subagent 20 seconds and the `Stop` hook 60, so the hook
was cut off before the pause it was probing for reached Codex. The probe now runs no more rounds
than fit in the hook's time, and a hook wired with under 90 seconds pauses at once and leaves the
wait to its runner, as an in-hook wait there does.

**After the clock was set back, no usage fetch ran.** A reading fetched while the clock ran fast
carried a fetch time in the future, so it looked fresh to every caller, forced checks and blind
probes included, until the clock caught up: a session without a status line went on deciding on a
frozen reading. A last fetch more than 30 seconds in the future now counts as due, and a wait after
a failed fetch that would end more than an hour from now is not kept.

**The queue pace looked past the credit ceiling.** With `credits.ceiling` below the weekly pause
point, or the weekly threshold off, `noctis queue status` and the digest counted the weekly room up
to the pause point and named a finish time days before the one the guard allows. The room now runs
to the ceiling when that comes first.

**With `usage.multiSessionMax` off, an idle session made a burst.** An idle session's older, lower
reading landed in the burst history between the busy session's readings, and the climb from it to
the next reading read as a jump: 86, 88, 76, 89 paused at 89 % with a pause point of 92. A reading
below the last one of the same window now stays out of the burst history.

**A prompt starting with *Okçuluk*, *Tamamıyla* or *Noël* was a reply.** The router takes a prompt
that starts with *ok*, *tamam*, *no*, *dur* and the like for a short reply and never routes it, and
it found the end of that word with a test that knows only ASCII letters, so *Okçuluk* started with
*Ok*. The word now ends where a letter of any alphabet does not follow, and *Hayır, …* now counts as
the reply it is.

**With `credits.allowPaid` on, the Workflow gate measured the room to the credit ceiling.** A new
workflow needs `credits.fanOutHeadroom` points before the pause point, and with paid credits allowed
that point is the threshold, yet the gate stopped at `credits.ceiling`: with `session5h` off, a
workflow at 80 % was refused with a warning about the paid overflow you had allowed. The room now
runs to each window's pause point, and a window without one is left out.

## Pauses

**A hook that waited with another for the same pause said it had waited when the pause was
cancelled.** When two hooks of one session pause for the same reset a moment apart (two subagents
started together), the second waits with the first. After `noctis cancel` the first said
`▶ 5h wait cancelled — continuing.` and the second `⏸ 5h limit 95%: waited 0m, continuing.` Both now
say the wait was cancelled; a pause an early reset or fresh data ended still reads as waited.

**Breaking off two pauses held in the hook taught noctis a hook time limit.** When two pauses held
in the hook end after about the same time, well before their resume time, noctis takes that for a
time limit of the host's hooks and from then on hands every longer pause to the runner and a
relaunch. A pause you broke off with Esc and took over with a prompt counted as such an end, so two
of them six minutes in sent every pause of more than about five minutes to the runner, and in a
cloud session, which has no runner, to no resume at all, until `noctis on`. A pause your prompt takes
over now teaches nothing; one the session's next tool batch finds ended still does.

**While noctis was off, research routing and the Workflow gate still applied.** A prompt routed to
the research subagent before `noctis off` kept the main thread from `WebFetch` and `WebSearch`
during the pause, also for prompts typed after it, and a `Workflow` launch at or near the pause point
was denied with the promise that the plugin would continue the session at the reset, which it does
not do while off. Both now step aside while noctis is off, as the other checks do; a prompt typed
during the pause drops the route of the one before, and at the 100 % stop a launch is still refused.

## Queues

**The check between items skipped an item whose text was already checked.** The check record keeps
which item texts a check has passed, so a second item with the same text (`update the docs` after two
different jobs) and a checklist used again were taken as checked, and the queue went on or finished
without running the check. Repeated texts are now told apart by their place among their twins, and
an item unticked after a failed check and ticked again at a later stop is checked again. The digests
8.5.0 recorded stay valid.

**Lines inside an HTML comment were items.** Items commented out with `<!--` and `-->` on lines of
their own were still handed to Claude, and the comment's closing `-->` was glued to the first item
after it; `/noctis:start` on a file of one job per line took the comment's lines for jobs, and a
`noctis-verify` line inside a comment named the check between items. Lines inside an HTML comment
are now left out, as lines inside a code fence are, so commenting an item out switches it off, and an
item ticked inside a comment closes no issue (`queue.github.closeOnDone`). A comment after an item's
text on the same line stays part of the item.

**A check line right under an item became part of the item.** A `` noctis-verify-each: `go vet ./...` ``
line straight after an item turned it into `migrate the users table noctis-verify-each: …`. A check
line now ends the item above it, as a blank line does.

**A line that only started with the word *todo* was an item.** The `TODO:` form was matched in any
letter case and without its colon, so a Spanish line such as *Todo cambio debe pasar `make test`…*
became an item, and on `/noctis:start` a job `todo app: add a dark mode toggle` swallowed the jobs
after it. `TODO:` in any letter case and `TODO ` in capitals are still items; other lines that start
with the word are not.

**A model tag at the end of a long item was ignored.** The queue view cuts an item at 160
characters, and the tag was read from the cut text, so an item longer than that ending in `(opus)`
went to no Opus worker, and when it got stuck it went one step up from the session's model, not from
Opus. The tag is now read from the item's whole text.

**The test guard and the queue's workflow suggestion read a long item cut at 160 characters.** An
item that named its tests or the test file only after that point had Claude's change to the test
refused once, and the change made again was listed as a test changed past a refusal. A long item
about every component got no workflow suggestion, and a long one about a single file named at its
end got one. Both now read the item's whole text.

**Markers written with the Turkish capital İ were not read.** `(İNSAN)` and `(İnsan)` did not make
an item yours, `(HAİKU)` sent it to no Haiku worker, and `(BİTTİ)` did not mark an item of a list
without checkboxes done. They now count in any letter case, as `(INSAN)` and `(bitti)` did.

**The compactions on the way to a long item were left out of its tick,** so the summary of what items
took in `noctis queue status` counted none for an item longer than 160 characters.

**After a compaction, the note named the full check when the per-item check had passed last.** It
now names the check that ran.

**An item unticked for a failed check and ticked again was counted twice,** in the queue's pace and
in the items credited to the session's model and effort. An untick that a failed check asked for now
leaves the pace as it was.

**The files git ignores inside a new folder were part of the working tree.** While a folder was
untracked, noctis read every file in it for the tree it compares between stops, so a build into
`web/dist` counted as progress on a stuck item and ran the check between items again, and next to
2000 ignored files (a `node_modules`) the tree told nothing, so a session editing files there was
taken for stuck sooner. noctis now asks git for the files of an untracked folder, leaving out those
it ignores.

**A deferral with an `--until` more than 90 days ahead ended after 90 days.** The 90-day limit is for
deferrals without an end, as the reference says; one with an end now holds until it.

**`noctis queue verify --file docs/TASKS.md` from another folder ran the check in `docs/`.** The Stop
hook runs it in the project folder that `queue.files` places the file in, above `docs/` or
`.claude/`, and `verify --file` now does too.

**`noctis queue trust` looked for the check of a queue in `.claude/` or `docs/` in that folder.**
The check it suggests, also in `queue status` and its `--json`, came from the build files next to
the queue file, so `.claude/TASKS.md` got no suggestion and a `docs/` folder with a `package.json`
of its own got `npm test`. It now comes from the project folder the check runs in, such as
`go test ./...` for a Go project.

**The journal entry of `noctis queue defer` named what the item waits on but not the item.** `noctis
why` now shows `defer  connect the payment provider #pay: waits for the Stripe account`, one line per
item for a `#tag`.

**`noctis queue defer "#123"` found no item `queue import` wrote for issue #123,** and `acme/web#7`
also matched `acme/web#70`. An issue such as `#12` or `owner/name#12` now names the open items of that
issue, as `(after …)` reads them, and defers them together, as a `#tag` does.

**Issues imported into a list outside their checkout lost their repository.** `noctis queue import
--file ~/notes/TASKS.md` run in a checkout wrote bare `#12` items, so `queue.github.closeOnDone` closed
#12 of whatever repository the list's folder belonged to, or failed, and a second checkout's #12 was
skipped as already there. Such items now name their repository, taken from the issues' URLs
(`acme/api#12`, or `host/owner/name#12` off github.com), and a bare item an older import wrote with
the same title is rewritten to name it.

**Importing into a list without checkboxes dropped the list's own items.** The imported issue became
the list's first checkbox item, after which noctis read only checkbox items. The import now adds
nothing to such a list: it writes nothing, says why and exits 1. An import with nothing new still
works there.

**An imported title could mark its item as yours or for a model.** A title with `(human)` or a model
tag such as `(Opus)` made the item yours, which Claude never takes, or sent it to a worker on that
model. These marks are now written in brackets (`[human]`, `[Opus]`), as `(P0)` and `(after …)`
already were, and a re-import still knows an item you marked that way yourself.

**When `gh issue list` failed, `noctis queue import` dropped gh's reason** and asked whether the
GitHub CLI was installed and logged in, even when gh had said what was wrong (a mistyped repository,
no git remote, SAML, the rate limit). It now prints gh's own reason, and asks about installing only
when gh is not found.

**`noctis status` left out that a failing check held a queue,** and with `queue status` and the
digest it said the check of the held queue's ticked items ran at the next stop. A held queue's check
runs again only at the first stop after you type a prompt, or with `noctis queue verify`. `status`
now shows the hold as `queue status` does, since when and how to lift it, and the three leave the
count of unchecked items out while the hold lasts.

## Runners, relaunches and digests

**The daily digest moved to the machine's time zone after its first run.** The runner that sends it
worked out the next `digest.time` without the session's `TZ`: with `TZ=Asia/Kolkata` on a machine
set to UTC, the 08:00 digest came again at 13:30 the same day and at 13:30 every day after, and with
`TZ=America/New_York` at 04:00. The clock times in it were in the machine's zone too. Every runner
noctis schedules (systemd, launchd, Task Scheduler and the relaunch script) now gets the session's
`TZ`, as it gets `PATH`.

**A queue wake whose runner was gone was never set again.** When the runner of the wake a queue
stopped on a deferral sets was lost (a reboot, a logout, a killed sleeper), the wake sat in
`state.json` until it was pruned two days later. Queue wakes now get the repair pauses get: the next
prompt, tool batch, status-line refresh or session start sets the runner again, at most three times
in a row.

**`noctis on` left a queue wake waiting for the end of the pause it ended.** A queue wake that comes
while noctis is paused waits for the pause to end. Ended early with `noctis on` or `/noctis:resume`,
the pause still held the wake until the time it would have ended, a day later after `noctis off
1 day`. The wake now comes right away.

**`kill` on the pid `noctis job run` printed left the command running,** and `job list` called the
job gone. On macOS and Linux the job's wrapper now passes SIGTERM, SIGINT and SIGHUP on to the
command and what it started, and `job list` says the job was ended by a signal.

**`resume.terminal` with `{SCRIPT}` or `{Script}` relaunched without a window.** The placeholder
counted in any letter case but was replaced only as `{script}`, so the window got no script and the
relaunch ran headless. It is now replaced in any letter case.

**The digest gave two projects' `TASKS.md` the same heading.** A queue file whose name another
reported file shares is now named by its path, with `~` for your home folder.

**The digest missed an item done since the last one when another item with its text was done
before.** An item counted as new only when no item with the same text was done at the last digest,
so a second *update the docs* ticked since then got *Nothing done since*. The digest now counts the
done items of each text.

**A webhook that answered with a redirect was followed.** A 301, 302 or 303 turned the message into
an empty `GET` that still counted as delivered, so the alarm or the digest was lost and the next
digest counted from it, and a 307 or 308 sent the whole message on to the address it named, also
over plain http. Redirects are no longer followed: the delivery fails with its status (`http-301`
and the like), which `noctis webhook` shows, and `noctis status` for the digest.

**On Windows, the relaunch of an Antigravity, Droid or Copilot session left the paused session's
window open** when the CLI runs as its own `agy.exe`, `droid.exe` or `copilot.exe`. noctis closes
the earlier window only when its process is a session's, and of the Windows names it knew only
`claude.exe`, `codex.exe`, `node.exe` and `cmd.exe`.

**A long digest in Japanese, Chinese or Korean reached ntfy as a file.** The digest is cut at 1,900
characters, which in these scripts take three bytes each, and ntfy turns a body over its 4,096-byte
limit into an attachment, or refuses it on a server without attachments. The body sent to ntfy is
now cut below that limit.

**`noctis why` dated a queue wake, the digest's runner and a runner's check by the time they were
set for.** Setting a queue wake, setting it again, setting and starting the digest's runner, and a
runner leaving a session woken in place its grace wrote that time over the time of the decision, so
`why` listed a wake set at 02:00 for 06:00 at 06:00. The journal now dates each one when it happens,
and keeps the time it is set for in `wakeAt`, `dueAt` or `checkAt`, which `why` shows after the
reason.

## Languages

**A pasted code block switched the session to English.** A Chinese or Korean prompt with a fenced
code block, or a code block sent alone, set the session's notices, status line and toasts to
English. The language is now read from the words you type, without code blocks.

**Two kana in a prompt switched it to Japanese.** An English or German prompt that quoted a Japanese
word such as ログイン was read as Japanese. Kana now decide Japanese at once only when kana and Han
make up a quarter of the letters, as every other script already does; below that, another language
wins only with at least three of its common words and more than any other, so a Japanese prompt with
file names or an English error in it stays Japanese.

**`NOCTIS_LANG` or `locale` written with a region was not recognised.** `pt_BR`, `pt-BR`,
`de_DE.UTF-8` or `zh-CN` gave English everywhere and also turned off following the language of your
prompts. A region form now reads as its language, and a value that names no language noctis has
counts as `auto`.

**A turn Claude Code writes itself set the session's language.** A background task's notice, a
subagent's report, a message from another session or Stop hook feedback switched a Turkish or German
session to English, again and again in a session that uses subagents; on Copilot, noctis's own queue
continuation, which Copilot sends back as a prompt, did the same. Only prompts you type set it now.

**The router and the workflow suggestion took a turn Claude Code writes itself for your prompt.**
With `router.enabled`, a subagent's report, a message from another session or a system notice that
held a link was routed: Claude was told to hand it word for word to `noctis:lite`, and WebSearch and
WebFetch were denied for the turn. With `workflow.suggest` on, such a turn that named work across
many files brought a workflow suggestion. Both now leave these turns alone, as the checklist does.

**A bug report with numbered steps became a checklist in thirteen languages.** In German, French,
Spanish, Portuguese, Italian, Dutch, Polish, Russian, Japanese, Chinese, Korean, Arabic or
Indonesian, a report with reproduction steps was taken for a job list, and the Stop hook drove Claude
through the steps. The usual *steps to reproduce* and *expected/actual result* headings are now known
in all fifteen languages, as they were in English and Turkish.

**Turkish jobs ending in *et*, in the formal plural or with *lütfen* were not read as jobs** in a
sentence or a plain line (`optimize et`, `kontrol et lütfen`, `ekleyiniz`), although they were in a
bullet list, so such a request got no checklist. They are now read alike, and in a Turkish prompt
*et* is that verb, not a joining word.

**A question about a pasted list asked with the Arabic question mark `؟` was not a question.** The
list became a checklist the Stop hook drove Claude through, and a list of questions ending in `؟`
became a checklist of questions. `؟` now counts as `?` and `？` do.

**An emoji, a smiley or an `!` after the question mark hid the question.** *Welche davon sollten wir
zuerst machen? 🤔*, *¿Cuál primero? :)* or *Laquelle en premier ?!* turned the pasted list into a
checklist the Stop hook drove. They are now questions; *Can you do these? 🙏* still gets its
checklist.

**A prohibition typed with another apostrophe was missed.** *Don´t implement any of these yet*, with
`´`, `` ` ``, `ʼ`, `‘`, `′` or `＇` for the apostrophe, became a checklist the Stop hook drove. All of
these now read as an apostrophe, also in the other languages' phrases.

**A word such as *once*, *after*, *when*, *whether* or *case* anywhere before an English prohibition
cancelled it,** so *Once again, don't implement any of these yet.* became a checklist the Stop hook
drove. Only a condition that opens a clause (*If the build breaks, don't touch …*) now makes the
prohibition a rule on the work.

**_Don't touch anything but the auth service_ held the whole list back.** It is now a rule on how to
do the work, and the list gets its checklist; *Don't do anything but estimate each of these* still
holds it back.

**_başka_ or _dışında_ anywhere in a Turkish sentence cancelled its prohibition,** so *Başka
ekiplerin de onayı gerektiği için bunların hiçbirini henüz yapma.* became a checklist the Stop hook
drove. These words now count only in the part of the sentence that holds the verb; *başka bir şeye
dokunma* still keeps the checklist.

**An *other* word in the next sentence or inside another word undid a prohibition** in Spanish,
Italian, Russian, German, Indonesian, Polish and other languages: *No implementes nada todavía. Más
adelante …* or an *además* after it made the list a checklist the Stop hook drove. Such a word now
limits a prohibition only in its own clause and as a word of its own (*no toques nada más*).

**A description in the words of a prohibition lost its checklist** in Korean, Russian, German,
French and Italian (*아직 구현하지 않은 기능*, *Пока ничего не работает*, *am Verhalten darf sich
nichts ändern*, *ça ne change rien*, *sembra non fare nulla*). These now read as descriptions, and the
imperatives still hold the list back.

**A prompt that asked for a workflow in Turkish or with `/deep-research` got a workflow
suggestion.** With `workflow.suggest` on, noctis leaves alone a prompt that already asks for a
workflow, but it found *iş akışı* only when an ASCII letter followed it (*iş akışıyla*), never as
*İş akışı* at the start of a sentence, and never found `/deep-research`. They now count wherever
they stand.

## Commands

- **`noctis off` offered to cancel a queue wake it had not named.** With a session's queue wake
  pending, `off` listed the paused sessions and closed with "To cancel them all: noctis cancel",
  which removed the queue wake too. That line now comes only when every pending item is listed.
- **`noctis cancel --sid` with an empty value cancelled every session's wait**, scheduled
  relaunches, hand-offs, failed-relaunch notices and queue wakes included. It now exits 2 with
  nothing cancelled; `noctis cancel` with no argument still cancels them all.
- **`noctis cancel` with an option it does not know did the same.** `noctis cancel --id 1a2b3c4d`
  ignored the option and cancelled every wait. It now names the option, as `setup` does, and exits 2
  with nothing cancelled.
- **`noctis selftest` past a pause point paused its own probe.** The probe is a real hook call, so
  with a window past its pause point the hook speed check failed after 30 seconds and left a pause
  entry and a checkpoint for a session `selftest`; on Fable past the scoped threshold the probe
  switched the account's default model. The probe now never pauses or switches models.
- **`noctis checkpoint --cwd` printed `NONE` for the folder written another way** (`app/`, `.`,
  `../app`, `a//app`). The folder is now made absolute and clean before the lookup.
- **The doctor failed an effort level that setup saved.** For a model setting that names no single
  model (`opusplan`, `best`, `default`, a provider's id, or none) setup keeps the level in
  `env.CLAUDE_CODE_EFFORT_LEVEL`, and the doctor looked for it under the code model in
  `modelSettings` and said `Effort saved for …: none`. It now judges by the model `settings.json`
  selects.
- **With `models.primary` written as `opus[1m]`, `noctis report` counted every Opus subagent call as
  kept off the primary.** The `[1m]` suffix is now left out when the report compares models. For a
  primary noctis has no price for (`best`, `opusplan`), `--json` gave a cost on the primary of 0 and a
  negative saving; it now leaves both out.
- **`noctis report` left out every call of a transcript larger than 64 MiB.** It now reads each
  transcript line by line, whatever its size.
- **`noctis status` showed a switched-off threshold as its raw value:** `5h ≥false%`, `5h ≥%` or
  `Fable ≥0%`. It now says `off` there.
- **Uninstall said the model setting was kept because setup had not changed it** when setup had
  changed it and the model was changed again since. It now says the model was changed after setup and
  names the one from before setup, which uninstall forgets once it has run.
- **`noctis check --sid <short id>` judged with the default model.** The short id `status`, `why` and
  `off` print found no session, so a Fable session over its pause point got exit 0. `check` now
  resolves the id as `model --sid` does.
- **`noctis report --days 999999` counted nothing.** The span overflowed and the report said "Last
  999999 days" over 0 transcripts. `--days` now stops at 3650.
- **`noctis report` filed each call under its UTC date,** so "By day" split a local day outside UTC.
  It now uses your local date.
- **A count just under a million showed as `1000k`.** The context sizes in `noctis queue status`,
  the characters lean compaction trimmed in `noctis status` and the notes on a long context took
  the unit before rounding, so 999,600 came out as `1000k`. It now shows as `1.0M`.
- **`noctis why --json` printed a sentence when there was nothing to show.** It now prints nothing,
  as an empty JSON lines stream.
- **`noctis why --last N` showed fewer than N decisions.** It read only the last 64 KB of the
  journal and never its rotated `.1`, so right after the journal was moved aside it showed one or
  two, and a large `--last` came up short. It now reads on into `.1` when it needs to.
- **`noctis report --bundle` kept `queue.verifyEachCommand` as it was.** The per-item check, which can
  carry a credential as the full check does, went into the zip in `config.json`, the logs and the
  journal. It is now redacted with the others, also when the configured text has spaces at either end
  or names a path in your home folder: the home folder became `~` before the redaction looked for
  the configured text, so such a command was not found.

## Install, setup and other tools

- **A status line of yours that mentioned noctis was rewritten at every session start.** `noctis
  ensure` repairs the status line's path after an update, and it took any status line whose command
  mentioned noctis for its own: `bash ~/.claude/noctis-line.sh` became a call of noctis with that
  script as a command, and the line went blank. It now repairs only a status line that runs a noctis
  binary by its full path.
- **A section of the wrong type in `config.json` was overwritten at every session start.**
  `"thresholds": 90` or `"queue": false` was replaced by the shipped section, so `doctor` and `status`
  stopped naming the bad value. `ensure` now leaves such a section as it is; noctis runs on the
  shipped values for it, and `doctor` and `status` go on naming a wrong `thresholds` or `compaction`.
- **A reinstall or uninstall for Codex or Droid removed hooks of yours.** Any hook whose command
  mentioned noctis (`~/bin/noctis-notify.sh`, `noctis status --json >> ~/limits.log`) was taken for
  the plugin's. Only the plugin's marker, or a command that runs a noctis binary with `hook`, counts
  now.
- **`noctis doctor --host codex|droid|antigravity` called the hooks wired when only hooks of yours
  mentioned noctis,** before setup and after an uninstall. It now looks for the plugin's own
  handlers the way install and uninstall do.
- **Uninstall for Antigravity removed a status line of yours that mentioned noctis**
  (`bash ~/.gemini/noctis-line.sh`). It now removes only a status line that runs a noctis binary.
- **Provider model ids with a colon were refused.** Bedrock's ids end in `:0`, and inference-profile
  ARNs hold several colons; `setup --code us.anthropic.claude-opus-4-1-20250805-v1:0` stopped with
  "unknown effort '0'", and the interactive setup refused its own offered default and asked again.
  On a provider account the text after the last colon is now an effort only when it is one.
- **`scripts/install.sh` did not find its binary with `CDPATH` set.** It now finds its own folder
  whatever `CDPATH` holds.
- **The setup skill's own custom example gave efforts setup ignores** for the planning and digest
  roles. The example and the question now ask for a model alone there.
- **The trust guard missed a here-document whose closing line was split by a line continuation,**
  so a `noctis queue trust` after it went through. Such a line is now joined before it is compared.

## Settings and the files noctis keeps

- **A `config.json` that is valid JSON but not an object** (`null`, a list) was read as an empty
  config without a word, and `noctis ensure` wrote the shipped defaults over it at the next session
  start. It now counts as a config that cannot be read: `doctor` and `status` say so, and nothing
  writes over it.
- **A `usage.json` that could not be opened was taken for a corrupt one,** and the status line wrote
  the backup over it. A file that exists but cannot be opened is now left as it is, and the status
  line's reading is not stored until it can be.
- **A lock left by a dead process with a time ahead of the clock was never taken over,** so every
  write waited 10 seconds and failed until the clock caught up. A lock whose holder is gone is now
  stale once its time is more than 2 seconds away from now, before or after.
- **A `wait` setting of the wrong type was read as 0.** `"maxInHookMinutes": "5h"` gave a one-minute
  cap on waits in place and `"resetMarginSeconds": null` no margin, silently. A `wait` key the
  plugin ships as a number now runs on the shipped value when it holds something else.
- **Observe mode used up the startup notices it did not show.** With `"mode": "observe"` the
  self-check, the notice that a window is already over its pause point, the queue-mode notice, the
  line that a newer version is out and the one that asks for a restart after an update were marked
  as shown, so back in enforce mode the next session start showed none of them (the self-check for a
  day, the queue-mode notice for a week, a version's lines never). They are now marked only once
  shown, and observe mode no longer looks for a newer version.

## Lean compaction

**A re-read file could vanish from the summary.** Lean compaction drops an older `Read` result when
the same call ran again later. When Claude Code answered that later read with its note that the file
is unchanged since it was read, the only copy of the content was dropped, and the summary got a note
that points at nothing. A result is now dropped only when the later call brought the content back.

## Known limits

- **An item unticked and ticked again within one turn is not checked again:** the check record sees
  the ticks at each stop, and the item is ticked at both. A queue started with `/noctis:start` is
  fine, since each start drops the record.
- **On a provider account a typo in an effort becomes part of the model id** when the text before it
  is a valid id, so `--code opus:hgih` is taken as the model `opus:hgih`. Setup's report shows it.
- **When the status line and the usage endpoint both report a window with the same reset,** the
  higher reading wins, so an endpoint reading that is lower and newer than the status line's waits
  for the next render.
- **`noctis queue verify` typed inside `docs/` or `.claude/` without `--file` runs the check there,**
  where the Stop hook runs it in the folder above: from the path alone, a project folder named `docs`
  cannot be told apart from a subfolder. Use `--cwd <project>`.
- **`noctis report --html` writes its table headers in English** in every language.
- **In the thirteen languages read by phrase lists, a rule on one part of the work holds back the
  whole list,** as *не трогай файлы миграций* or *不要修改任何迁移文件* do: the list gets no checklist,
  and nothing is driven. English and Turkish tell the two apart. Forms that are both a statement and
  an imperative hold a list back the same way: Portuguese *não faz nada*, Spanish *no cambie nada*,
  French *ne modifie rien* after a noun, German *noch nicht anfangen*.
- **An English prohibition after a lead-in such as *After our call,*, *Unless I say otherwise,* or
  *When you have time,* is still read as a condition,** so the list gets its checklist and the Stop
  hook drives it; *In case the build breaks, don't touch any of these* now gets none.
- **`noctis queue status` and the queue instructions count at most 50 decisions noted on a file,** the
  number the file keeps, so a file with 60 notes says 50 in all.
- **In a repository with `status.showUntrackedFiles no`,** a new file is not part of the working tree
  noctis compares between stops: an item whose only change is a new file skips the check between items
  as unchanged, and a stuck item that only adds files is taken for one making no progress. Set
  `queue.verifySkipUnchanged` to `false` there.
- **A job's wrapper killed with `kill -9`,** or on Windows with `taskkill /F` without `/T`, still
  leaves the command running, and `job list` calls the job gone.
- **On Claude Code, setup and uninstall take a status line whose command mentions noctis for the
  plugin's own:** setup puts noctis's line in place of `bash ~/bin/noctis-line.sh` without keeping
  it as `statusline.chainCommand` to show above its own, and uninstall then removes the line. Name
  such a script without *noctis*, or set it again after an uninstall.
- The limits in [RELEASE_NOTES_8.5.0.md](RELEASE_NOTES_8.5.0.md) still apply.

## Tests

Each fix comes with a test that fails without it: a Go test, or for `scripts/install.sh` two
`CDPATH` checks in the lab's installer scenario and for lean compaction one test of the
unchanged-file note in the lean kit. A few more Go tests hold what the fixes must leave as it was.
The results of the local rounds on this release are in its commit message.
