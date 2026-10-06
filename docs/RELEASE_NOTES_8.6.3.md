# noctis 8.6.3

8.6.3 makes the check before each `Edit`, `Write` and `MultiEdit` of Claude's lighter. Before such
a write goes ahead, the PreToolUse hook looks at where it lands, with symbolic links (and Windows
junctions) followed, and each of its checks did that on its own, so one write resolved the same
paths over and over. The hook now resolves each path once per write and every check uses that
answer. It decides exactly as 8.6.2 did. 8.6.3 carries everything in 8.6.2
([RELEASE_NOTES_8.6.2.md](RELEASE_NOTES_8.6.2.md)).

## If you are upgrading

8.6.3 adds no settings, writes nothing new and reads every file 8.6.2 wrote. `hooks/hooks.json` is
unchanged.

## One resolution per write

Before a write of Claude's goes ahead, the PreToolUse hook checks that it does not:

- reach noctis's own `state.json`, its backup, `used-checkpoints.json` or `config.json`;
- tick, change or remove an open `(human)` item of the queue that drives the session;
- add a job the prompt did not ask for to a checklist noctis wrote from a prompt;
- change a test or check file while the queue item in hand is not about tests;

and when the write unticks an item the queue's check passed, it forgets that pass. Each of these
resolved the written path, the files it is compared with and their folders by itself: in the cases
measured, one write resolved 12 to 20 paths, the same path up to 6 times. The hook now resolves
each path once, 7 or 8 in all, and keeps what it resolved for that one write. The next write
resolves its paths afresh, so a link that moves between two writes is judged by where it leads at
the second.

Measured on Linux, in a project with no queue: 1000 runs of each hook with the 8.6.2 binary and
with this one, in turn, and the system calls of one run of each.

| PreToolUse | file status calls | system calls | median | p90 |
| --- | --- | --- | --- | --- |
| `Edit` | 286 → 113 | 699 → 417 | 5.73 → 5.46 ms | 6.92 → 6.76 ms |
| `Write` | 286 → 113 | 656 → 415 | 5.76 → 5.47 ms | 7.14 → 6.79 ms |

macOS and Windows were not measured; the hook skips the same repeated resolutions there.

## Known limits

The limits in [RELEASE_NOTES_8.6.2.md](RELEASE_NOTES_8.6.2.md) still apply.

## Tests

8.6.3 adds 2 Go tests, in `writepaths_test.go`. Each makes symbolic links, and skips on a machine
that may not make them:

- within one check of a write, a path is resolved once, though its link moves on in the meantime,
  and after the check it is resolved afresh;
- a link that moves between two edits is judged by its new target: an edit through a link to a test
  outside the project goes ahead, and the same edit, once the link leads to the project's test, is
  refused.

The results of the local rounds on this release are in its commit message.
