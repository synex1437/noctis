---
name: start
description: Work through the jobs in a file, one after another without stopping — /noctis:start <file>. noctis says how many jobs it found and keeps the session going until each is done; /noctis:stop ends it.
argument-hint: <file>
disable-model-invocation: true
---

The user asked noctis to work through the jobs in `$ARGUMENTS`.

noctis reads that file itself when the command is typed. If a noctis note in this turn says how many jobs it found and names a checklist, start the first job now and follow the note. If there is no such note, noctis did not start a queue (it may be paused or not running): tell the user so in one line and do not start the jobs on your own.
