# noctis 8.5.0

8.5.0 adds Indonesian, the fifteenth language. Everything noctis says to you comes in Indonesian
when you write to Claude in Indonesian: the notices, the status line, the daily digest, the output
and help of every command. And the queue reads an Indonesian prompt as it reads an English one: it
finds the jobs in it, tells a job from a sentence that describes, keeps an item it was asked not to
do yet, and knows an item about tests. It carries everything in 8.4.0
([RELEASE_NOTES_8.4.0.md](RELEASE_NOTES_8.4.0.md)).

## If you are upgrading

Nothing to do. 8.5.0 adds no settings and changes no file noctis keeps. With `locale: auto` (the
default) a session whose prompts are in Indonesian gets Indonesian from its first prompt on;
`NOCTIS_LANG=id`, or `locale: id` in `config.json`, pins it for every session. Sessions in the other
fourteen languages read as they did.

## What you see

All 648 messages are translated, in the informal register Indonesian developers use with each
other (`kamu`). The terms are the ones the field uses: *antrean* for the queue, *sesi* for a
session, *batas penggunaan* for a usage limit, *jendela 5 jam* and *mingguan* for the two windows,
*pemadatan ramping* for lean compaction, *pohon kerja* for the working tree, *ditangguhkan* for an
item set aside and *ditahan* for a held queue; `checkpoint`, `effort`, `job`, `commit` and the names
of models, files and commands stay as they are. The five short `[noctis]` instructions handed to
Claude stay in English, as they do in every language. `noctis status` on a machine not yet set up:

```
Penggunaan    : (tidak ada data)
Ambang        : 5 jam ≥92% · mingguan ≥95% · Fable ≥97%
Model         : pengaturan=(tidak ada) · utama=opus · cadangan=opus · effort=xhigh
Menunggu      : tidak ada
Log kesalahan : bersih
```

The status line names the weekly window `Mg` (*minggu*) and the 5-hour one `5 jam`, days are `Min
Sen Sel Rab Kam Jum Sab`, and durations use `hr` (*hari*), `j` (*jam*) and `mnt` (*menit*): a wait
of a day and two hours reads `1hr 2j`.

## How a prompt is read as Indonesian

Indonesian is written in the Latin alphabet, so it is told from the other Latin-script languages as
they are told from each other: by its function words. A list of 119 of them (*yang, dan, untuk,
dengan, tidak, tolong, jangan, sudah, belum, kalau, lalu* and so on, with the chat spellings *yg,
dgn, utk, gak, nggak, aja, udah, kalo*) is scored against the first 1200 bytes of the prompt. The
prompt reads as Indonesian when its Indonesian words outnumber the runner-up language's by at least
one and either count three or more, or two of them belong to no other language's list. *di* (in,
at) is also Italian and *dan* (and) is also Dutch, so a prompt whose only Indonesian words are those
two stays in the session's current language; `kenapa build gagal di CI sekarang` or `tolong
tambahkan logging di sekitar retry loop` decides it. Code identifiers, paths and English technical
terms are in no list and count for nothing, so a prompt that mixes them in reads as Indonesian all
the same.

## The queue in Indonesian

The words the queue goes by have Indonesian entries:

- **Jobs.** 70 imperative verbs (*tambahkan, perbaiki, jalankan, ubah, hapus, pindahkan, perbarui,
  implementasikan, tinjau, …*) and 14 lead-ins (*tolong, mohon, silakan, coba, pertama, lalu,
  kemudian, setelah itu, terakhir, akhirnya, selanjutnya, juga, dan, sekarang*) mark a clause as a
  job; *lalu, kemudian, setelah itu, akhirnya* and *selanjutnya* mark a chain of them. A sentence
  that opens with one of 20 describing words (*saya, kami, kita, ini, itu, tersebut, karena, jika,
  kalau, tapi, namun, jadi, …*) is a note, not a job, unless an imperative follows in the same
  sentence. The join words *dan, lalu, kemudian, terus* and *serta* split a line into
  the clauses whose number tells several jobs from one.
- **Held items.** 58 phrases that forbid the work for now (*jangan implementasikan, jangan ubah apa
  pun, jangan sentuh file, belum perlu diimplementasikan, jangan mulai dulu, …*), 48 that ask for
  something else instead (*hanya perkirakan, rencanakan saja, tanpa mengubah kode, jelaskan saja,
  review saja, …*), and the words *lain, lainnya, selain* and *selebihnya*, which right after such a
  phrase turn it on the other items (*jangan implementasikan yang lain*), so the item itself stays a
  job.
- **Items about tests.** *uji* and its forms (*menguji, pengujian, diuji, teruji*), *ngetes,
  mengetes, pengetesan, tes-nya* and *tes unit* mark an item whose work may well change the tests,
  so the guard that refuses test edits on other items lets them through; `test`, which Indonesian
  developers write as often, counted already.

## Measured

On Linux with 4 CPUs, as Go benchmarks of three runs of 300 passes each:

- **Reading a prompt.** The 25 Indonesian prompts of the language corpus take 0.38–0.40 ms a pass,
  about 15 µs a prompt; the whole corpus of 375 prompts takes 4.0–4.2 ms, about 11 µs a prompt. An
  Indonesian prompt costs what a prompt in any other language costs, and the detection runs once per
  prompt inside the `UserPromptSubmit` hook, whose whole run takes 5–7 ms.
- **The binaries.** The catalog adds 64 KB to each Linux binary, 66 KB to each Windows one and 165 KB
  to the universal macOS binary, which holds two builds.

## Known limits

- **Two shared words are not enough.** A short prompt whose only Indonesian function words are *di*
  and *dan* stays in the session's current language, as said above. One more Indonesian word
  decides it.
- **Malay reads as Indonesian.** Standard Malay shares most of these function words, so a prompt in
  Malay is likely to read as Indonesian and be answered in it. There is no Malay catalog.
- **`tes` on its own is neither an imperative nor a test word**, because *tes* is French for *your*
  and would mark French items wrongly. Write `test`, *tes-nya*, *uji* or *pengujian* to have an item
  counted as about tests, and *uji* or *periksa* to make it a job.
- **Chat spellings beyond the ones listed** (*gw, lu, bgt* and the like) count for nothing, and a
  prompt made of them alone stays in the session's language.
- The limits in [RELEASE_NOTES_8.4.0.md](RELEASE_NOTES_8.4.0.md#known-limits) still apply.

## Tests

- The language corpus gains 25 Indonesian prompts, each read as Indonesian; the 350 prompts of the
  other fourteen languages and the 60 English prompts that carry words other languages use read as
  they did, none as Indonesian. A prompt the detector leaves undecided fails the test unless it is
  listed as left to the session's language, and none is.
- The table of items known to be about tests gains three rows: two Indonesian items that are, and a
  French item with *tes* that is not.
- The tests that run over every catalog language now run over Indonesian too: the same keys and
  format verbs as English, status labels that line up, day names and duration units, the help text
  and the update hint. `node scripts/i18n.js check` audits `i18n/id.json` with the rest, and the
  hygiene suite reads it for control bytes and invisible characters.
