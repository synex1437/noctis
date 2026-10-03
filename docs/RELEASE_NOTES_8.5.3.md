# noctis 8.5.3

8.5.3 lifts the known limit 8.5.2 added and those 8.5.1 listed, some of them in part, and fixes
eight defects found on the way: a quiet status line that let work go on past a pause point, an empty
lock a killed hook left that held the next hook for 15 s, a passing 429 that moved a session off
Fable for the rest of its week, a retry that continued a session a second time after the clock went
back, a long prompt the prompt hook took seconds to read, a move off Fable the decision journal left
out, text for Claude that named a window in your language, and a workflow refusal that spoke of
negative room. Each fix comes with a test that fails without it. 8.5.3 adds no feature, and carries
everything in 8.5.2 ([RELEASE_NOTES_8.5.2.md](RELEASE_NOTES_8.5.2.md)).

## If you are upgrading

8.5.3 adds no settings and reads every file 8.5.2 wrote. A few answers change:

- `noctis why` has a `switch-model` line when a turn fails on Fable's limit, with the fallback model
  and Fable's reading, as it has for the other moves off Fable, and a `skip-launch` line from
  `UserPromptSubmit` when a prompt in the session's own window takes over a relaunch on the fallback
  model.
- A rate-limit error whose text names no limit, in a session on Fable whose bucket reads 90 % or
  more but is below its switch point, is retried on the `wait.retryMinutes` steps, and moves the
  session to the fallback model only when it comes again before the session finishes a turn or a
  batch of tools. 8.5.2 moved the session at once, for the rest of Fable's week.
- While the status line is quiet, the usage endpoint is asked more often near a pause point: a pause
  decision or a subagent's check asks it, at most once a minute, once the burn-rate projection of a
  reading at least a minute old comes within 8 points of a pause point or of `credits.ceiling`.
- What noctis tells Claude names the 5-hour and weekly windows `5h` and `weekly` in every language,
  and the workflow gate's refusal for a window at or past its pause point says so instead of giving
  the room left as 0 or below. The notices to you keep your language.
- `noctis queue status` and the queue instructions count every decision noted on a file. A record
  8.5.2 wrote counts on from the decisions it keeps.
- `noctis queue verify` typed inside `docs/` or `.claude/` runs the check in the folder the Stop
  hook ran it in, once the Stop hook has run it for that file.
- `noctis report --html` writes its title and column names in noctis's language.
- On a provider account, `noctis setup --code opus:hgih` stops with the unknown-effort error; 8.5.2
  saved the model `opus:hgih`.
- Setup keeps a status line whose command only mentions noctis, such as `bash ~/bin/noctis-line.sh`,
  as `statusline.chainCommand`, and uninstall leaves it in place.
- `noctis job list` shows a job whose wrapper alone was killed as running while its command runs,
  the Stop hook waits for it, and `noctis job stop` ends that command.
- A checklist from a prompt: in the thirteen languages read by phrase lists, a rule on one part of
  the work keeps the checklist, and an English prohibition after a lead-in such as *After our call,*
  holds the list back; *In case the build breaks, don't touch any of these* gets its checklist.

## Usage limits

**A quiet status line let work go on past a pause point.** When the status line went quiet, noctis
went on with its last reading and a projection of its burn rate, and asked the usage endpoint only
once that projection reached the pause point. A reading of 80 % eight minutes old whose burn
projected 91 % was taken as room while the window could already be past 92 %: a seven-day soak with
seed 5 let work go on at 93.1 % on a reading of 82.4 %. A pause decision and a subagent's check now
ask the endpoint, at most once a minute and waiting up to 1.5 s for the answer, as soon as the
projection of a reading at least a minute old comes within 8 points of a pause point or of
`credits.ceiling`. Near the edge, a window is also polled every 15 s, as within two points of its
pause point, once one more jump like its last one (with a 20 % margin) could cross it; a window that
had jumped 4.5 points to 87 % was polled every 60 s.

**A lower reading waited for a status line that no longer rendered.** When the status line and the
usage endpoint read the same window, the higher reading was used, since usage does not go down
within a window. A fall the endpoint reported, after a plan upgrade say, then waited for the status
line to render again, which a paused session's status line does not do, so the session could stay
paused on a reading that was no longer true. A lower endpoint reading of the same window is now used
when it was fetched more than 5 minutes after the status line last sent that window; within those 5
minutes the higher reading still wins, so an endpoint reading that lags a status line still
rendering does not undercut it.

**A passing 429 moved a session off Fable for the rest of its week.** A rate-limit error whose text
names no limit was put on Fable's bucket whenever the session ran on Fable and the bucket read 90 %
or more, so a passing 429 at 91 % moved the session to the fallback model until Fable's reset,
though noctis would have kept it on Fable up to the switch point (97 % by default). Such an error
now moves the session off Fable only when the bucket reads at or past its switch point (100 % when
it has none), or when the session fails with the same error again before it finishes a turn or a
batch of tools; until then it is retried on the `wait.retryMinutes` steps like any error the limits
do not explain.

## Pauses and relaunches

**After Fable's limit, a session that went on in its own window was relaunched as well.** noctis
parks a relaunch of the session on the fallback model, and the runner checks first that the session
did not go on by itself. When the session went on in its own window before its answer reached the
transcript, the runner found nothing new and relaunched it as well, and the window it went on in
then had its next prompt refused as a duplicate: the known limit of 8.5.2. A prompt that goes ahead
in the session's own window, also while noctis is paused, now takes the parked relaunch over under
the state lock: it marks the pause as continued by the session, consumes its checkpoint and cancels
its scheduled runner, so the runner skips it, and `noctis why` has `skip-launch`, *the session went
on in its own window after the model switch*. A prompt that finds the runner took the session over
while the prompt was decided is refused like a prompt in a window the session was handed off from.

**After the clock went back, a retry continued a session a second time.** When a relaunch ended
before the session answered, its retry went by the time stamped on each transcript line to tell what
the session wrote after the relaunch. Once the system clock had gone back, an answer or a prompt
from the session's own window was stamped before the relaunch ended, so the retry missed it and
continued the session a second time. The retry now also counts every line written past the size the
transcript had when the failed relaunch returned, whatever its stamp. After Fable's limit the retry
also counts an answer from the second the relaunch ended, as the other retries do; it counted only
answers stamped two seconds later or after.

**A move off Fable after a failed turn was missing from the journal.** When a turn failed on Fable's
limit, noctis made the fallback model the default and parked a relaunch on it, but the decision
journal had no `switch-model` line for the move, as it has when a prompt, a tool batch, a stop or a
runner moves a session off Fable. `noctis why` now shows it with the fallback and Fable's reading,
as it shows the others.

**An empty lock a killed hook left held the next hook for 15 s.** A hook killed between creating
`state.lock` and writing its pid into it left an empty lock behind. noctis gave a lock that names no
holder 15 seconds, so the next hook waited 10 s on it, gave up that state write (*state.lock is held
by another process; not written*) and waited out the rest of the 15 s at its next write. An empty
lock now goes after 2 s, like one whose holder died; a holder that is still running keeps it, since
on Linux and macOS it holds the lock's `flock` and on Windows the file cannot be removed while it is
open.

## What noctis tells Claude

**Claude read the name of a window in your language.** What noctis adds to Claude's context or
returns as a denial is English, but it named the window with your own label: under a Turkish locale
Claude read *5sa usage 93%* or *haftalık usage is 86%*. The note on a typed prompt past the pause
point, the warn-band note, a subagent's stop at a pause point or at the paid-credit ceiling, and the
workflow gate's refusals at a pause point, in the warn band and for want of room now name the 5-hour
and weekly windows `5h` and `weekly`. The scoped window keeps its configured label, and the notices
to you keep their language.

**The workflow gate spoke of negative room.** The workflow gate measures the room left before each
window's pause point. When a window was already at or past it, as Fable's window is for a session on
another model or the 5-hour window is in a typed turn, the refusal read *only -0.8 points of the 5h
window are left* or *only 0 points*. It now says that the window is at its pause point or already
past it; the wording with room left is unchanged.

## Queues

**An item unticked and ticked again within one turn was not checked again.** The Stop hook learns
that a passed item was unticked only when it sees the item unticked at a stop. When Claude unticked
such an item and ticked it again within one turn, the next stop saw it ticked as before and took it
as checked, so the check did not run for the new tick. Now, when Claude or one of its agents unticks
items with Write, Edit or MultiEdit in the file of the queue that drives the session, the
`PreToolUse` hook drops them from the file's check record, and the next stop checks them again as
items ticked for the first time. While the check fails, the record is left to the Stop hook as
before, so passed items unticked then still start the pace notes over.

**`noctis queue verify` typed inside `docs/` or `.claude/` ran the check there.** Without `--file`,
the typed verify ran the check in that folder, while the Stop hook of a session in the project runs
it in the folder above, since from the path alone a project folder named `docs` cannot be told apart
from a subfolder: a check such as `make test` then failed or checked nothing. The Stop hook now
keeps the folder it ran the check in with the file's check record, when the check passes, fails or
is cut short, and `queue verify` runs the check in that folder while it is still there and holds the
file (any folder for a session's checklist). A typed verify that passes keeps the folder the Stop
hook recorded, never the one it picked itself, and one that fails still changes nothing.

**`noctis queue status` counted at most 50 decisions.** A queue file keeps the last 50 decisions
noted on it, and `noctis queue status` and the queue instructions of a session counted the ones
kept, so a file with 60 notes said 50 in all. The record of a file now carries how many decisions
were noted on it; each note counts on from the larger of that number and the decisions kept.

**New files did not count where git hides them.** In a repository with `status.showUntrackedFiles`
set to `no`, `git status` lists no new file, so the working tree noctis compares between stops and
between items left new files out: an item whose only change was a new file skipped the check between
items as unchanged, and a stuck item that only added files was taken for one making no progress.
noctis now asks git for new files itself (`--untracked-files=normal`, then `all` for a new folder as
before), whatever the setting says. The workspace guard at a pause still reads `git status` as the
repository sets it.

## Checklists from a prompt

**An English lead-in before a prohibition was read as a condition on it.** A prohibition after *if*,
*unless*, *when*, *whenever*, *once*, *after* or *except* is read as a rule on how to do the work,
so the list keeps its checklist (*If the build breaks, don't touch any of these*). A lead-in that
only opens the sentence was read the same way, so *After our call, don't implement any of these
yet.*, *Unless I say otherwise, …* and *When you have time, read through these, but don't implement
any of them yet.* got a checklist the Stop hook then drove, and *In case the build breaks, don't
touch any of these* got none, as *in case* opened no condition. *In case* now opens a condition, and
an opener whose clause is a lead-in does not, such as *after our call* or *meeting*, *after
talking*, *after we talked* or *met*; *unless I say* or *tell you otherwise* or *so*; *when* or
*whenever you have time*, *get a chance* or *can*, or *when possible*; *in case it wasn't clear* or
*you missed it*. A clause of more than 8 words is no lead-in.

**In thirteen languages a rule on one part of the work held back the whole list.** In the thirteen
languages read by phrase lists, a prohibition held back the whole list whenever its words were on a
list, so a rule on one part of the work, such as *не трогай файлы миграций* or *不要修改任何迁移文件*, left
the list without a checklist and nothing was driven, where English and Turkish keep it. Forms that
are both a statement and an imperative held a list back too: Portuguese *não faz nada*, Spanish *no
cambie nada*, French *ne modifie rien* after a noun, German *noch nicht anfangen* after a modal.

A phrase on the files or the code now holds the list back only when nothing in its clause narrows
its object. A qualifier after it (*de migración*, *im Migrationsordner*, *миграций*, *في مجلد
الترحيل*, *migrasi*) makes it a rule on one part of the work; a time, a polite word, an emphasis
such as *at all* or *in no case*, a joining or subordinating word, the current code, or the project
or its repository keep the hold. A phrase that ends in a quantifier (*aucun*, *ningún*, *nenhum*,
*nessun*, *أي*) is narrowed by a noun other than a file, the code or a word for a piece of the work
(*aucun test*). Korean and Japanese read the word before *파일* or *ファイル*, and Chinese the word after
*任何*, each with its own words that keep the hold (*아직*, *既存の*, *现有*). A form that is a statement as
well as an imperative is read as a statement when its clause opens with a subject: a pronoun, an
elided *l'*, or a noun after an article with no preposition before it, but not a time such as *ce
soir* or *esta noche*. So *O botão não faz nada* and *Le nouveau cache ne modifie rien* keep the
checklist, while *Pendant la démo ne touche à rien* and *Ce soir ne modifie rien* still hold it
back. A German infinitive after a modal is a statement unless its clause has a personal pronoun such
as *ich*, *du* or *Sie*.

**A long prompt that repeated a limit on the work took seconds to read.** In the thirteen languages
read by phrase lists, a prohibition that goes on to *anything else* (*ändere nichts anderes*, *ne
touche à rien d'autre*) is a limit on the work, which noctis tells by looking at the 16 characters
after the phrase and the 8 before it. To get them it turned the whole rest of the prompt, and the
whole part before the phrase, into characters at every such phrase, so a long prompt that repeats
one took time in the square of its length: 8000 times *Ändere nichts anderes.* in a 192 KB prompt
took 9.7 s of the `UserPromptSubmit` hook, and *Ne touche à rien d'autre.* 12.1 s. It now turns only
the bytes that can hold those characters, and the two prompts take 0.28 s and 0.26 s. Since every
occurrence of a phrase is now read before it holds the list back, a prompt that repeats a narrowed
rule costs more than before: 240 to 580 KB of such repeats take 0.19 s to 0.44 s, against 0.10 s to
0.17 s, still in linear time.

## Commands and setup

**`noctis report --html` wrote its headers in English.** The page title and the column names were
English whatever language noctis speaks. Both now come from the catalogs, in all fifteen languages.

**On a provider account a mistyped effort became part of the model id.** On an account whose Claude
Code uses another provider or a gateway, setup takes any id made of the allowed characters, and the
text after the last colon of a role flag was the effort only when it was one, so `--code opus:hgih`
was saved as the model `opus:hgih`. When the text before that colon is a model name noctis knows
(one with a model family in it, a `claude-` id, `best`, `default` or `inherit`) and the text after
it is close to an effort (within two edits, or the first three or more letters of one, as unknown
flags are matched), setup now stops with the unknown-effort error naming it. An id such as
`us.anthropic.claude-opus-4-1-20250805-v1:0`, an inference-profile ARN or a gateway tag such as
`:latest` or `:free` stays the model id, and so does any text after a model name noctis does not
know.

**Setup and uninstall took any status line that mentions noctis for their own.** Setup put noctis's
line in place of a script such as `bash ~/bin/noctis-line.sh` without keeping it in
`statusline.chainCommand` to show above noctis's line, and uninstall then removed the line, or put
back the one setup had replaced in place of such a script set after it. Only a command that runs
noctis's binary with `statusline`, with variables set before it or not, now counts as noctis's own,
besides the `guard.js` line of older versions: setup chains any other, and uninstall leaves it in
place. A session start still leaves a status line that mentions noctis as it is.

**A job whose wrapper alone was killed was called gone while its command ran.** `noctis job run`
starts `noctis job exec`, which runs the command and records how it ended. When that wrapper alone
was killed, as `kill -9` or `taskkill /F` without `/T` does, the command went on, but `noctis job
list` showed the job as gone with no exit status, the Stop hook reported it as gone instead of
waiting for it, and `noctis job stop` refused it as not running. The wrapper now also records the
start time of the command it runs, and a job whose wrapper is gone counts as running while that
command still runs; a command pid that now names another process counts as gone, as the wrapper's
pid already did. `noctis job stop` ends the command's tree when the command still runs, and its wait
for the end to be recorded also ends when no wrapper is left to record it and the command is gone.

## Known limits

The limits in [RELEASE_NOTES_8.5.0.md](RELEASE_NOTES_8.5.0.md) still apply. Of the ones 8.5.1
listed, these remain in part:

- **Reading a prompt for a checklist still goes by word lists.** In the thirteen languages read by
  phrase lists, a prohibition whose words are on no list holds nothing back, as before (*No
  modifiques ninguna línea del código*, *لا تعدل ملفات المشروع*), and a word the lists do not know
  after the object is read as narrowing it, so *No toques los archivos del proyecto Atlas* keeps the
  checklist. A narrowed rule still holds the list back when a comma comes before its qualifier (*Не
  трогай файлы, связанные с миграциями*), or with German *bis auf* (except), read as *bis* (until).
  A statement still holds it back when its subject is a name or a noun with a complement (*O botão
  de salvar não faz nada*), when it is German with *sie* (*sie können noch nicht anfangen*), and in
  an Arabic feminine or a Japanese plain form (*الصفحة الجديدة لا تغير شيئا*, *このスクリプトは何も変更しない*).
- **An English lead-in the openers do not list, or one of more than 8 words, still reads as a
  condition** on the prohibition after it, so *After lunch, don't implement any of these yet.* gets
  a checklist the Stop hook drives.
- **An item a shell command unticks and ticks again before the next stop is not checked again:**
  only Write, Edit and MultiEdit are seen as they happen. A write the tool then fails to make costs
  at most one more check.
- **A lower reading of the usage endpoint counts only once it was fetched more than 5 minutes after
  the status line last sent its window,** and not while its date is ahead of the clock, as after the
  clock went back.
- **On a provider account a mistyped effort after a model name noctis does not know is still part of
  the model id.** Setup's report shows it.
- **Before the Stop hook has run a file's check, `noctis queue verify` typed inside `docs/` or
  `.claude/` runs it there,** as 8.5.2 did. Use `--cwd <project>`.
- **A queue file whose record 8.5.2 wrote counts its decisions on from the ones it keeps,** so the
  notes it had dropped, past the last 50, are not counted.
- **A job whose wrapper alone was killed has no exit status:** once its command ends, `noctis job
  list` calls the job gone with no exit status, since no wrapper was left to record it.

## Tests

Each fix comes with a Go test that fails without it, and the commit of each fix names the failure.
The seven-day soak's stale-lock chaos now plants an empty lock 3 s old half of the time; its
marathon checks that a passing 429 below Fable's switch point is filed as one; it counts the moves
off Fable by their `switch-model` lines alone, so a move after a failed turn is not counted twice;
and it records with each breach what the status line and the usage endpoint last said of the window.
The test that times a huge prompt reads four prompts now, each in under 2 s: the earlier one, 8000
times *and after our call* in one sentence, 8000 times *Ändere nichts anderes.*, and 3000 times a
narrowed Spanish rule and a French statement. Over four seven-day soaks (seeds 3, 5, 17 and 42) the
usage endpoint was asked 1793 times with the check of a quiet reading and 1776 times without it,
with no breach either way. The results of the local rounds on this release are in its commit
message.
