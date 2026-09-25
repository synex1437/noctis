---
name: stop
description: Stop the queue running in this session (started with /noctis:start or taken from a prompt); the jobs left are not started.
disable-model-invocation: true
---

noctis ends this session's queue when the command is typed, before Claude sees it, and says how far it got. If you are reading this, noctis did not handle the command (it may not be running): tell the user in one line that the queue may still be running, and do not start any more of its jobs.
