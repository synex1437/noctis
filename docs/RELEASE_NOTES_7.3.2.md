# noctis 7.3.2

7.3.1 was not released. The CI run for its merge to main failed in the two-day soak, and the release
workflow publishes only from a passing run. The failure was in the soak's own bookkeeping, not in
noctis: since 7.2.0 a prompt you type at or past a pause point goes ahead with a warning and its
turn runs on until the 100 % ceiling, but the soak still counted every call as one the guard should
have held back. 7.3.2 carries all of 7.3.1's fixes ([RELEASE_NOTES_7.3.1.md](RELEASE_NOTES_7.3.1.md))
and makes hooks faster when `state.json` is large: with a 264 KB state an `Edit` or `Write` hook
takes about 4 ms where it took 8, a `UserPromptSubmit` hook about 9.5 where it took 15, and a status
line refresh that brings nothing new no longer rewrites `usage.json`.

## If you are upgrading

From 7.3.0, everything in 7.3.1's upgrade notes applies
([RELEASE_NOTES_7.3.1.md](RELEASE_NOTES_7.3.1.md#if-you-are-upgrading)). 7.3.2 adds no settings,
and an existing `config.json` works as it is. One thing behaves differently:

- Turning the router off (`router.enabled`) now also ends a route it made earlier in the session.
  Before, a route made while it was on kept sending `WebSearch` and `WebFetch` in the main thread to
  the research subagent for up to an hour.

## Why 7.3.1 was not released

The two-day hard soak (`soak.js --days 2 --seed 11 --hard 1`) failed on Ubuntu with breaches: calls
that took an account past 100 % of its 5-hour window. Since 7.2.0 a prompt the user types at or past
the pause point, including one the guard projects a burst or a compaction to reach, goes ahead with
a warning, and the turn it starts runs on until the credits ceiling (`credits.ceiling`, 100 %): the
user chose to go on. The soak still held every call to the pause point, so the last call of such a
turn, started just below 100 % and large enough to end past it, counted as a breach. Seeds 5 and 8
show those breaches on every run of the old soak here, all from such turns.

The soak now keeps those calls apart. A call in a turn that began with a typed prompt breaches only
if it starts at or past 100 %, and the summary counts them (`typedTurnCalls`) with the highest
5-hour reading one started at. Calls in any other turn, a queue continuation or a prompt noctis
composed, are held to the pause point as before. Every breach also lists the guard's last decisions
for its session, so a failing CI log says how the call got through.

## Faster hooks

**A hook reads `state.json` once, and not at all where nothing needs it.** A hook that only looks at
the state now reads the parse it already holds instead of a deep copy of it, and a state write
keeps what it wrote, so a read later in the same hook needs no parse. Hooks that do not need the
state no longer read it: an `Edit` or `Write` hook reads it only when the file is the session's
checklist or one of noctis's own files, `WebSearch` and `WebFetch` read it only while the router is
on, and the session's language, which picks the language of what noctis says, is looked up only the
first time a hook needs some text.

**A status line refresh with nothing new writes nothing.** Every refresh rewrote `usage.json` and
its backup, even when nothing had changed. A refresh whose `usage.json` would be byte for byte the
one on disk (the same readings within the same second) now writes neither. In 80 refreshes with
the same input, 7.3.1 rewrote `usage.json` 80 times and 7.3.2 once at most.

**Less work at start.** Hooks and the status line run the Go garbage collector less often (GOGC 400,
unless `GOGC` is set): they live for milliseconds with a heap of a few megabytes, and a hook that
settles in to wait puts the default back. The config loaded at start is handed to the hook instead
of being read and merged again, checking a language no longer builds the English and Turkish
catalogs, and file names are made safe without a regular expression.

Measured on Linux, noctis run directly, medians of 120 calls in each of two runs (the status line:
80 refreshes with nothing new), with a 264 KB `state.json`:

| | 7.3.1 | 7.3.2 |
| --- | --- | --- |
| `PreToolUse` Edit | 8.0–8.6 ms | 3.9–4.9 ms |
| `PreToolUse` Write | 8.1 ms | 4.0–4.3 ms |
| `UserPromptSubmit` | 14.5–15.4 ms | 9.5–9.9 ms |
| `Stop` | 11.0–11.1 ms | 8.0 ms |
| `SessionStart` (resume) | 11.0–11.8 ms | 8.9–9.1 ms |
| `PermissionRequest` Read | 10.5–11.1 ms | 7.0–7.5 ms |
| status line | 10.9–11.2 ms | 9.0–9.4 ms |

With a small state (805 bytes) `UserPromptSubmit` gains about 1 ms (5.1–5.3 to 4.3–4.4 ms) and the
other hooks stay within noise of 4 ms. `PreToolUse` Bash and `PostToolBatch` take about 4 ms with
either state. The `bin/noctis` launcher of a macOS or Linux marketplace install adds 1 to 1.5 ms.

## Known limits

- The CI run that failed could not be replayed here: seed 11 passes locally, before the change and
  after it. The breaches it reported were most likely the ones seeds 5 and 8 show; if one was not,
  the next failing log names the decisions that let it through.
- The limits listed for 7.3.1 still apply ([RELEASE_NOTES_7.3.1.md](RELEASE_NOTES_7.3.1.md#known-limits)).

## Tests

Fourteen new Go tests hold what the speedups must not change: a look at the state lends the parse
and a read copies it, a missing or broken `state.json` still reads as the empty state, a write keeps
what a parse of its bytes gives back (numbers above 2^53, a `[]string`, text that is not UTF-8, a
nil map) and is read back without a parse, a value that could not be encoded keeps nothing, hooks
with nothing to say leave the state unread, an edit of the session's checklist still reads it, an
old route holds nothing once the router is off, a status line with nothing new writes nothing, a
wait puts the default collector back, the session's language is settled by the first text a hook
needs and only when neither `config.json` nor `NOCTIS_LANG` names one, the languages noctis knows
are its catalogs, and safe file names are made as before. Under `go test` every JSON read also
checks that the parses kept for later reads still match their bytes, so a test whose code changes a
map it was only lent fails. An existing test and a test helper now turn the router on, since routes
are kept only while it is on.
