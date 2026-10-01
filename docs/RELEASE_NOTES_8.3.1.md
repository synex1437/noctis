# noctis 8.3.1

8.3.1 makes the hook that runs after each batch of tool calls quicker when the tools return a lot,
and draws the pictures again on one black theme with red accents. Claude Code hands that hook,
`PostToolBatch`, the whole output of every tool in the batch, and waits for it before Claude goes
on. noctis needs only which tools ran and the commands they ran, and it now reads past the outputs
instead of building them: a batch that returned 1 MB is done in 9–11 ms where it took 16–18, one
that returned 23 MB in 82–84 ms where it took 173–182. A batch of a few kilobytes, as most are,
takes about what it took. 8.3.1 carries everything in 8.3.0
([RELEASE_NOTES_8.3.0.md](RELEASE_NOTES_8.3.0.md)).

## If you are upgrading

Nothing to do. 8.3.1 adds no settings, and the files noctis keeps are read and written as 8.3.0
reads and writes them.

## A batch's outputs are read past, not built

The input of `PostToolBatch` lists the batch's tools under `tool_calls`, and each entry holds the
tool's output under `tool_response`: every file Claude read, every command's output, every
subagent's report. noctis decoded all of it, then looked at each entry's tool name and, for a shell
tool, its command. 8.3.1 reads the input 64 KB at a time, checks each part as the whole input was
checked before, and keeps only the bytes noctis decodes: it never builds an output, and past the
first 64 KB it does not hold one either. An input shorter than that is decoded whole, as before, and
loses its outputs after. On a batch of about 1 MB, reading the input takes 1.4–1.5 ms and 68 KB of
memory, where decoding it whole took 10.0–10.8 ms and 11.4 MB.

What stays as it was:

- With `NOCTIS_DEBUG_HOOKS` set, the hook reads its input whole, so `hooks-debug.log` keeps every
  output.
- Only the entries of a batch lose their outputs. The `PostToolUse` hook that follows a subagent
  (`Agent` or `Task`) still reads the subagent's report, which is that input's own `tool_response`.
- An input that is not JSON is refused, as before, with a warning in noctis's log. For an input of
  64 KB or more the warning gives the byte where it stops being JSON rather than `encoding/json`'s
  words. An input that is only space is taken as empty, as before.

## The pictures and the README

Every picture under `docs/` is drawn again on one black theme with red accents: the banner, the
social preview, the demo, before and after, the flow, the install, the languages, the status line
and the timeline share one background, one red, one card style and a red rule along the bottom.
Colour emoji give way to drawn icons, so the pictures look the same wherever they are shown. What
they say changed only where it had fallen behind 8.3.0: the flow names the hooks noctis runs and
what each is for, and reads the project's own check as a signal; the install shows the Code profile
new installs start on and the settings noctis writes for it; and the demo's legend tells the
notices shown to you apart from the instruction the Stop hook hands Claude.

The README, in English and in Turkish, gets flat dark badges, a row of links to its sections, a
strip of four facts under the demo, the before-and-after picture under "Why not just let Claude
Code continue?" and a footer with the star line and a link back to the top.

The picture for the repository's social preview is now `docs/social-preview.jpg`, 1280×640 and
82 KB where the PNG was 416 KB, so link previews that skip large images, WhatsApp's among them,
show it. GitHub takes it under Settings → General → Social preview → Edit → Upload an image.

## The other channels, checked

The other ways noctis and Claude talk were checked for load the same way, and are left as they are.
Every hook Claude waits for (`SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`,
`PostToolBatch` and `Stop`) and four of the asynchronous ones were timed 30 times each with 8.3.0 on
Linux, in a session without a queue: the medians run from 5.0 to 6.8 ms, about what starting the
program costs there. The text noctis hands Claude is none on most events; about 1,700 to 2,200
characters for the directive of a queue session as it starts, resumes or comes back from a
compaction; and 2,701 at most, on a Stop whose check failed with 240 KB of output: well under the
10,000 characters Claude Code adds to its context from a hook.

## Measured

On Linux with 4 CPUs, a `PostToolBatch` of four `Read` results whose text is code (tabs, quotes and
line breaks), against the lab's stand-in for the usage endpoint, the input written to the hook
through a pipe as Claude Code writes it; the medians of two runs of 25 calls, alternating call by
call between the two versions:

| Batch input | 8.3.0 | 8.3.1 |
| --- | --- | --- |
| 2 KB | 5.78–6.34 ms | 5.44–5.98 ms |
| 115 KB | 7.17–8.51 ms | 6.14–6.41 ms |
| 1.1 MB | 15.68–17.72 ms | 9.41–10.77 ms |
| 5.7 MB | 49.58–57.92 ms | 29.91–31.85 ms |
| 22.9 MB | 173.42–182.10 ms | 81.71–84.28 ms |

Part of what is left is the pipe: `cat` took 3.6 ms on the 1.1 MB input and 16.6 ms on the 22.9 MB
one, against 3.1 ms on 2 KB. With the input read from a file instead, 8.3.0 took 14.50, 39.84 and
141.19 ms on 1.1, 5.7 and 22.9 MB, and 8.3.1 7.94, 16.70 and 49.69 ms: about 6 ms per megabyte of
output before, about 2 now.

## Known limits

- **Claude Code still writes the outputs to the hook.** noctis reads the input to its end, to check
  it as before, so a big batch still costs the pipe and about 2 ms per megabyte; no hook setting
  asks Claude Code for a batch without its outputs.
- The limits in [RELEASE_NOTES_8.3.0.md](RELEASE_NOTES_8.3.0.md#known-limits) still apply.

## Tests

New Go tests read hook inputs a window at a time and compare what comes back with what decoding the
whole input gave before, the outputs left out: every case of the JSON codec's own tests, alone and
in each place of a batch; names written with escapes, members named twice, outputs at other depths
and outside `tool_calls`; and inputs that stop being JSON inside an output. Each is read whole, a
byte at a time, by halves and with the end of input arriving with the last bytes, through windows
from 1 byte to 64 KB. Other tests check the byte a refusal names, the depth limit, noctis's own
state as an output, input that is only space, a string's end found wherever it falls in a word,
that a batch with 7 MB of output is read in under 1 MB of memory, and that `NOCTIS_DEBUG_HOOKS`
keeps the outputs. A new fuzz target, `FuzzJSONFastHookInput`, compares the reader with
`encoding/json` on any input and window, and CI runs it with the other fuzz targets.

Of 24 changes made to the reader by hand to break it, the tests fail on 22; the other two change
nothing a reader of the input can see.
