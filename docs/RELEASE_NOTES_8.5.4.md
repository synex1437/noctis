# noctis 8.5.4

8.5.4 widens the word lists noctis reads a long prompt with before it makes a checklist of it, and
lifts the checklist limits 8.5.3 listed, some of them in part: a prohibition on no list, an unknown
word after the files, a comma before a qualifier, German *bis auf*, statements and English lead-ins.
In the thirteen languages read by phrase lists, the prohibitions grow from 427 phrases to 4610 forms
and the requests for only an estimate, a plan or a review from 258 to 331, with the infinitive and
rule forms written instructions use. They are read with or without accents and Arabic marks, with up
to three filler words between their words, and with an ending on the last word. 8.5.4 also fixes the
defects found on the way: an Arabic rule on one step held the list back when its *أيضا* (also) began
with *أي* (any); *No projeto não mexa em nada*, *Ändere nichts anderes als zu schätzen*, *Jangan
ubah file di proyek ini* and a time after the files kept a checklist the prompt forbade; an
exception such as *außer der README* or Turkish *diğer* (other) held back a list it only limited;
and *let's not* and Turkish compound verbs such as *değişiklik yapma* held nothing back. Each change
in what a prompt holds back comes with a test that fails without it. 8.5.4 adds no feature, and
carries everything in 8.5.3 ([RELEASE_NOTES_8.5.3.md](RELEASE_NOTES_8.5.3.md)).

## If you are upgrading

8.5.4 adds no settings and reads every file 8.5.3 wrote. Only checklists from a prompt change:

- More long prompts get no checklist: a prohibition or a request for only an estimate in words the
  8.5.3 lists did not have, in an infinitive or a rule form (*Merci de ne rien modifier*, *No tocar
  nada*, *禁止修改任何文件*), without its accents, or with a filler word inside (*Ändere bitte nichts*).
- More long prompts keep their checklist: a prohibition with an exception (*No toques nada excepto
  el README*) or with a qualifier after a comma, a sentence that tells what something does or can
  do, and a prohibition in a clause that gives a purpose (*Lavora su una copia per non toccare il
  codice*).
- `noctis why` and the log quote the words that held a list back as the prompt writes them, *NO
  TOQUES NADA* where 8.5.3 wrote *no toques nada*; in Arabic, Chinese, Japanese and Korean they
  quote the listed form.

## Checklists from a prompt

**The phrase lists missed many ways of saying a prohibition.** In the thirteen languages read by
phrase lists, 8.5.3 knew 427 prohibitions and 258 requests for only an estimate, a plan or a review,
each matched with its accents and with nothing between its words. So *No modifiques ninguna línea
del código*, *لا تعدل ملفات المشروع*, *Nao mexa em nada ainda*, *Ändere bitte nichts*,
*これらを実装しないでください* and *Só quero uma estimativa destes itens* got a checklist the Stop hook then
drove. The lists now hold 4610 prohibitions and 331 requests, written per language as patterns with
alternatives. They are read with or without accents, with the forms of Arabic alef and the Arabic
marks folded, with up to three filler words such as *bitte*, *todavía* or *пока* between their
words, and with an ending on a last word of more than three letters (*Jangan ubah filenya dulu*).

**Infinitives and rules held nothing back.** Written instructions often give an infinitive or a rule
instead of an imperative: *Merci de ne rien modifier*, *No tocar nada*, *Ничего не трогать*, *Proszę
nic nie zmieniać*, *Gelieve niets te wijzigen*, *Bitte keine Änderungen vornehmen*, *Tidak usah
mengubah apa pun*, *يرجى عدم تعديل الملفات*, *これらは実装しないこと*, *禁止修改任何文件*, *코드 수정 금지*. 8.5.3 held back
none of the 39 such prompts in the new test, and 8.5.4 holds back all of them. As with an
imperative, a rule on one part of the work keeps the checklist, and so does a statement: an
infinitive after *can*, *should*, *tends to* or *risks* whose subject is not *I*, *we*, *you* or
Claude (*Le correctif devrait ne rien changer*, *El botón puede no hacer nada*, *Скрипт может ничего
не менять*, *Ça risque de ne rien changer*). *Vous devez ne rien modifier*, *Il faut ne rien
modifier* and *Ты должен ничего не трогать* hold the list back.

**A prohibition in a clause that gives a purpose held the list back.** *Lavora su una copia per non
toccare il codice* (work on a copy so as not to touch the code) says how to do the work, but 8.5.3
held that list back, and the ones with *in modo da*, *de forma que* and *para que*. A prohibition
after *para*, *pra*, *pour*, *per*, *afin de*, *in modo da*, *de forma que*, *para que*, *pour que*,
*чтобы*, *żeby*, *aby* and the like is now a rule on the work and keeps the checklist, unless the
words before ask for it or call it important: *Peço para não mexer em nada*, *Proszę, żeby nic nie
zmieniać* and *Важно, чтобы ничего не менять* hold the list back. A prohibition after *semble*,
*parece* or *sembra* (seems) is a statement (*Le bouton Enregistrer semble ne rien faire*).

**A word after the files was read as narrowing them.** A project with a name or a word such as
*this* after it, and a time, now keep the hold: *No toques los archivos del proyecto Atlas*, *Jangan
ubah file di proyek ini*, *No toques los archivos esta semana*, *Nie ruszaj plików do jutra* and
*Ändere keine Dateien bis auf weiteres* (until further notice) got a checklist and now hold the list
back. So do *No projeto não mexa em nada*, whose *No* (in the) is no subject, and *Ändere nichts
anderes als zu schätzen* (change nothing but estimate), which asks for only an estimate. A qualifier
after a comma now narrows the rule (*Не трогай файлы, связанные с миграциями*), and German *bis auf*
is read as *except* (*Ändere keine Dateien bis auf die README*): both keep the checklist.

**A prohibition with an exception held back the whole list.** 8.5.3 had no words for an exception,
so *Ändere nichts außer der README*, *No hagas nada salvo la documentación*, *Ne touche à rien sauf
le README*, *Non toccare niente tranne il README*, *Не трогай ничего, кроме README*, *Nie zmieniaj
niczego oprócz README*, *Raak geen bestanden aan behalve de README* and *Jangan ubah apa pun kecuali
README* got no checklist. An exception now makes the prohibition a limit on the work, as *anything
else* does, and the list keeps its checklist, unless a condition or a word such as *estimate*
follows it: *Ne touche à rien sauf si je te le dis* and *No hagas nada excepto estimar cada punto*
still hold the list back.

**Statements held the list back.** A sentence that tells what something does keeps the checklist
when its subject is a name or a noun with a complement (*O botão de salvar não faz nada*), when it
is German with *sie* (they) (*Der Login-Knopf geht nicht, sie können noch nicht anfangen*), and in
an Arabic feminine or a Japanese plain form (*الصفحة الجديدة لا تغير شيئا*, *このスクリプトは何も変更しない*). A
time is no subject, so *今日は何も変更しない* still holds the list back, and so does the request
*このスクリプトは何も変更しないでください*.

**An Arabic phrase matched inside a longer word.** 8.5.3 found *لا تنفذ أي* (don't carry out any)
inside *لا تنفذ أيضا* (also don't carry out), so *لا تنفذ أيضا الخطوة الثالثة قبل الاجتماع*, a rule
on one step, held the list back. An Arabic phrase now matches as a whole word or with an ending such
as a pronoun, so that list keeps its checklist, and *لا تفعل شيئا الآن* and *لا تعدل ملفاتي* hold
theirs back.

**English.** *Let's not*, *lets not* and *let us not* before the work are prohibitions (*Let's not
start on any of these yet*). A lead-in of *after* with a break or a meeting is no condition at any
length (*After lunch, …*, *After a short break, …*, *After our call with the whole design team and
the product people yesterday, …*), and neither is *when you read this* or *when you see this*: these
prompts got a checklist and now get none. A condition still keeps it (*If the build breaks, don't
touch any of these*).

**Turkish.** A compound verb or a noun for a change holds the list back: *Koda müdahale etme*,
*Değişiklik yapma* and *Bunlara el sürme* got a checklist and now get none, while a place before it
makes it a rule on one part (*README'de değişiklik yapma*, *Testleri yaz ama kodda değişiklik yapma*
keep the checklist). *Diğer dosyalara dokunma* (don't touch the other files) is a limit on the work,
like *başka bir şeye dokunma*, and keeps the checklist.

**Speed.** 8.5.3 searched a prompt once for each phrase. 8.5.4 indexes the phrases by their first
two words and reads the prompt's words once; Arabic, Chinese, Japanese and Korean, which are
searched by characters, share one search for all phrases that begin with the same two characters.
Each time below is the median of five readings, or for a 2 KB prompt the best of three benchmarks,
and each range covers two or three runs of each version:

- 96 KB of English rules: 47–51 ms to 22–23 ms.
- 152 KB of *and after our call*: 65–71 ms to 27 ms.
- 192 KB of *Ändere nichts anderes.*: 126–148 ms to 50–53 ms.
- 255 KB of a narrowed Spanish rule and a French statement: 153–156 ms to 70–73 ms.
- 240 KB of *no*: 123–137 ms to 89–91 ms.
- 384 KB of Chinese rules on one page or file: 94–102 ms to 96–102 ms.
- 416 KB of an Arabic statement: 71–85 ms to 141–152 ms.
- A 2 KB English or Spanish prompt: 0.86–0.92 ms and 0.99–1.05 ms to 0.47–0.50 ms and 0.56–0.58 ms.
  A 2 KB Chinese, Japanese, Korean or Arabic prompt takes 0.01 to 0.11 ms more, at 0.28 to 0.48 ms,
  with its longer lists.

The Arabic statement takes longer because 8.5.3 held that prompt back at its first sentence and
stopped reading, where 8.5.4 reads each of its 8000 sentences as a statement. 424 KB of Arabic with
no prohibition takes 89–93 ms against 100–111 ms.

## Known limits

The limits in [RELEASE_NOTES_8.5.0.md](RELEASE_NOTES_8.5.0.md) still apply. Of the ones 8.5.3
listed, these remain:

- **Reading a prompt for a checklist still goes by word lists.** In the thirteen languages read by
  phrase lists, a pronoun such as *nada* or *nichts* followed by a place still holds the list back
  (*No toques nada de la carpeta tests*, *Ändere nichts im Ordner tests*), while *No toques los
  archivos de la carpeta tests* keeps it. *Nichts weiter* is read as *nothing else*, so
  *Implementiere nichts weiter, bis das Team zustimmt* keeps the checklist. *Not only … but also* is
  read as a request for only an estimate, so *Bitte nicht nur schätzen, sondern auch umsetzen*, *Ne
  fais pas seulement une estimation, implémente aussi* and *Не только оцени, но и реализуй* hold the
  list back. A rule whose object comes between its words keeps it (*Bitte keine Änderungen an den
  Dateien vornehmen*), and the Korean statement *기능 구현은 아직 진행 중입니다* (the feature is still being
  built) holds it back.
- **An English lead-in of *after* with a meeting or a break is no condition at any length,** so
  *After the meeting notes are merged, don't touch any of these files* holds the list back, and a
  *when* lead-in the openers do not list still reads as a condition: *When you have a free minute
  later today, don't implement any of these yet* gets a checklist the Stop hook drives.
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

Each change in what a prompt holds back comes with a Go test that fails without it, and the eleven
new tests all fail on 8.5.3. They read the wider lists, the infinitive and rule forms, the
statements with *can* or *should* and the clauses that ask or call a prohibition important, the
purpose clauses, prohibitions without their accents or with a word inside, a time after a
prohibition, the Arabic endings, the requests for only an estimate, *let's not*, and the Turkish
compound verbs. The check of the phrase lists now expands each pattern and fails on a form written
with capitals, on a single word in a language read word by word, and on two forms that read the same
once folded. The test that times a huge prompt reads seven prompts now, each in under 2 s: the four
before, 4000 times ten *no*, 2000 times a pair of narrowed Chinese rules, and 2000 times an Arabic
statement. Outside the repository, the shared search for Arabic, Chinese, Japanese and Korean was
checked against a search phrase by phrase on 200 000 random prompts with both lists, with no
difference. The results of the local rounds on this release are in its commit message.
