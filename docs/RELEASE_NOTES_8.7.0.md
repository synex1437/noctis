# noctis 8.7.0

On October 9, 2026, a user measured where their weekly limit had gone, from the transcripts of
October 7 to 9 on their machine, each API response counted once by its message id and weighted by
API list prices. About 70 % of the weekly limit went in two days: 25,573 API calls, 96 % of them in
one session. Subagents spent 73 % of it, 62 % even with cache reads left out, and the 121 subagents
opened on one day spent about 61 % of the weekly limit. Each started with about 53k tokens of
context. The heavy ones grew in 250 to 520 tool calls to the 300k compaction point and read their
whole context again on every call, to write 2k to 18k tokens. Cache reads were 73 % of the spend and
output, thinking at max effort included, 5.8 %: what a subagent costs is its calls times the size of
its context. noctis's pause points look at how much of a window is used, not how fast it goes, so
nothing showed or stopped the burn, and the user noticed it at 70 %.

8.7.0 shows where the limit goes, watches how fast the weekly window burns, and limits subagents
where they cost: how many are opened, how far each grows, and the ones noctis asked for itself. Every
limit is on by default and can be loosened or turned off. 8.7.0 carries everything in 8.6.4
([RELEASE_NOTES_8.6.4.md](RELEASE_NOTES_8.6.4.md)).

## If you are upgrading

- New settings, all on: `burn.*` (the burn alarm), `subagents.*` (the spawn gate, the budgets, the
  growth limit and the agents' compaction window) and `router.shortQuestionWords`.
  [REFERENCE.md](REFERENCE.md) lists them. `subagents.guard: false` turns the subagent limits off,
  and `burn.alarm: false` the burn alarm.
- `queue.subagentAboveTokens` is now 0, so a queue no longer hands its items to `noctis:worker` once
  the context grows. `config.json` gains `"configVersion": 2` at the next setup or session start, and
  a 100000 there, the earlier default setup wrote, becomes 0 then. Until then noctis reads
  `config.json` as if that were done. Any other value stays, and so does a 100000 set after that.
- `hooks/hooks.json` gains `SubagentStart` and `SubagentStop`; the latter runs in the background.
- noctis's four agent files gain `autoCompactWindow: 200000`. Claude Code reads agent files as it
  starts, so restart it once after the update.
- `noctis report` now counts each API response at its largest figures, as `noctis cost` does, so its
  output tokens and cost come out a little higher than 8.6.4's (see below).
- 8.6.4 reads the new `config.json` as it is: it keeps `configVersion` 2 and takes a
  `subagentAboveTokens` of 0 as off.

## Where the limit goes: `noctis cost`

`noctis cost` reads the transcripts of the last 7 days under `<account>/projects`, the subagents'
own (`<session>/subagents/agent-<id>.jsonl`) with them, and counts each API response once by its
message id: a response written over several lines at the largest figure of each field, and a
response copied into another transcript once. It prices them at API list prices, as `noctis report`
does; on a subscription the dollars only weigh the parts against each other. It shows:

- input, cache writes, cache reads and output apart, in tokens, cost and share;
- the main sessions against the subagents, with their calls, and the subagents' share with cache
  reads left out;
- the spend by day, model, session and subagent type;
- the 10 costliest subagents, with their tool calls, API calls, context at the start and at the
  peak, compactions, cost and task.

`--days N`, `--top N`, `--session <id or its start>` and `--json` narrow or reshape it. Task names
in Chinese, Japanese or Korean are clipped and lined up by the width they take on screen.

On the machine 8.7.0 was built on, `noctis cost --days 30` read 103 transcripts in 2.8 seconds:

```
noctis cost · last 30 days (since 2026-09-10 00:56) · 103 transcripts · 102 subagents · API list prices, informational on a subscription

                        input  cache write   cache read       output        total
  tokens                92.2K      156.50M     7807.19M       29.28M     7993.07M
  cost                 $0.372        $1047        $1537         $592        $3177
  share                  0.0%          33%          48%          19%

Main sessions $2275 (72%, 25782 calls) · subagents $902 (28%, 18274 calls, 102 agents)
Subagents' share with cache reads left out: 20%
…
By subagent type (agents · calls · cost · share):
  general-purpose            91 ·    18055 ·       $896 ·   28%
  Explore                     1 ·       74 ·      $4.08 ·  0.1%
  claude-code-guide          10 ·      145 ·      $1.55 ·  0.0%

Top 10 subagents (tool calls, API calls · start / peak context · compactions · cost, share · task):
  ad2e19b4 general-purpose      726    669 ·   42.8K  296.2K ·  5 ·    $31.95    1% · Long-session simulation bug hunt
  a7c6425d general-purpose      560    551 ·   41.1K  295.4K ·  4 ·    $25.68  0.8% · Marathon review: CLI area
  ab60689b general-purpose      502    461 ·   40.4K  296.0K ·  3 ·     $24.4  0.8% · Fix PDF P2 items batch A
…
```

The pattern is the one measured on October 9: general-purpose subagents that start near 41k tokens
and grow to the compaction point in hundreds of tool calls.

## The weekly window's pace

noctis now keeps the weekly window's readings of the last day, from the status line and the usage
endpoint, and takes its pace over the last 3 hours (`burn.lookbackHours`, 1 to 24) once the readings
span half an hour and the window rose 2 points. At that pace it reckons when the window reaches its
pause point (`thresholds.weeklyAll`, 95 % by default, or 100 % while that is off) and compares that
with the reset. The window's level at the start of those 3 hours lies between the readings on either
side of it, so the rise of hours without readings, a night with the machine off or use from a phone,
is spread over those hours and does not land in the last 3.

- When the pause point comes before the reset, the status line shows `⌛ weekly threshold ~<time>`,
  and once per weekly window you get a notice with the pace and a pointer to `noctis cost`.
- When it comes in less than half the time left to the reset (`burn.stopSubagents`, 0.5), new
  subagents and workflows are refused until the pace drops, and Claude is told why and to do the
  work in the session. The status line shows `⛔ weekly threshold ~<time>, no new subagents`, and a
  notice, a desktop notification and the webhook say so once per weekly window.
- Claude's refusal and the journal give the pace's times in English, like the rest of what noctis
  tells Claude; the notice and the status line give them in your language.

Two days into the week at 40 %, at 2 points an hour, the pause point is 27.5 hours away: before the
reset in 5 days and within half of it, so new subagents are refused. At half a point an hour it is
110 hours away, so you are warned and subagents still open. At the pace measured on October 9, about
1.5 points an hour, a window rises 2 points in about 80 minutes; from then on, as long as that pace
would reach the pause point before the reset, the status line and the notice say so.

The refusal needs `subagents.guard` on and noctis not paused (`noctis off`); while it is paused, and
in observe mode, the alarm only warns. `burn.stopSubagents: 0` keeps the warning without the
refusal, and `burn.alarm: false` turns both off, the `⌛` with them.

The `⌛` was there before: 8.6.4 took its pace from the first and last of the weekly window's last
six readings, whenever they were taken, and showed it after any rise. It now uses the 3-hour pace
and needs a rise of 2 points, so a single point's tick no longer shows it.

## A session's subagents are counted

Every `Agent` or `Task` call, the main session's and a subagent's own, now meets a gate in
`PreToolUse`, in the turn of a prompt you typed too.

- It is refused while the weekly window has less than 15 points left before its pause point
  (`subagents.weeklyRoom`): with the default pause point at 95 %, no subagent opens past 80 %. It is
  also refused while the burn alarm refuses subagents.
- Otherwise it is counted for its session and, across the account's sessions on this machine, for
  the last hour. Once a session has opened 20 (`perSessionDeny`), or the last hour 12
  (`perHourDeny`), the next one is refused. A refused call is not counted.
- From the 10th in a session (`perSessionWarn`) or the 6th in an hour (`perHourWarn`), it goes
  ahead, and Claude is told to do work that needs few reads itself and to give each subagent a brief
  that carries what it needs.

A refusal tells Claude why and to do the work in the session itself, and lifts the research route
of the prompt, so the main session can search the web itself. You get a notice once per session for
each kind, the warning included. `noctis why` lists `spawn-subagent`, `warn-subagent-spawn` and
`deny-subagent-spawn`, each with its reason, such as `general-purpose: 12 opened in the last hour,
limit subagents.perHourDeny 12`. `noctis:digest`, which runs noisy commands on a cheap model and
returns a digest of their output, is exempt (`subagents.exempt`).

On October 9 one session opened 121 subagents in a day. With these defaults it could have opened
20, at most 12 of them in one hour.

While no subagent can be opened, noctis no longer asks for one either:

- the router leaves a research prompt with the main session (`skip-route`);
- a queue's directive and its continuation tell Claude to do each item in the session, an item
  tagged for another model included (`noSubagent` in `noctis why --json`);
- a stuck item is set aside instead of going up to a stronger model, and a check that keeps failing
  holds the queue without going up (`escalateNoRoom`);
- a relaunch prompt leaves out its hand-off to `noctis:worker`.

## Each subagent has a budget

A subagent is told its budget as it starts (`SubagentStart`): 60 tool calls or 120k tokens of
context (`subagents.maxToolCalls`, `maxContextTokens`), and for `noctis:lite` 20 tool calls and a
report under 450 words, for `noctis:worker` a report under 300 words (`subagents.budgets`). It is
told to work from its brief instead of exploring, since every call re-reads its whole context.

After each of its tool batches noctis counts its tool calls and reads its context from the end of
its transcript. Past either limit it is told to stop exploring and return what it has
(`warn-subagent-growth`); 10 tool calls later (`graceToolCalls`) it is stopped
(`stop-subagent-growth`), and you get a notice. At its next tool batch the main session is told
which subagents were stopped, that what they returned may be incomplete, and not to open another
for the same task. The heavy subagents of October 9 took 250 to 520 tool calls to reach the 300k
compaction point; with these defaults a general-purpose subagent is told to wrap up at 60 tool calls
or 120k tokens of context, and stopped 10 tool calls later.

As each subagent ends, `SubagentStop`, which runs in the background, measures it from its transcript
and journals `subagent-report`: tool calls, API calls, context at the start and at its peak,
compactions, and the cost at API list prices with the shares of cache reads and output.

In observe mode the gate and the growth limit only journal (`would-…`) and no budget is given. While
noctis is paused, the gate, the budget and the growth limit stand down; the count and the report go
on.

## noctis asks for fewer subagents itself

noctis asked for subagents of its own in places where one search or the session would do:

- **The queue hand-off.** Past 100k tokens of context, each next queue item went to `noctis:worker`,
  a subagent that starts with a context of its own and reads it again on every call.
  `queue.subagentAboveTokens` is now 0 (off); set a number to have it back.
- **Short questions.** With the router on, a one-line question of up to 20 words that asks the web
  for a fact (`router.shortQuestionWords`), such as *what is the latest Go release?*, now stays with
  the main session (`short-question` in `noctis why`), unless it asks for a comparison, a ranking,
  sources or a piece of writing. One search answers it, and a subagent would start over with a
  context of its own.
- **The agents' descriptions and briefs.** `noctis:lite`'s description now asks for work that takes
  several searches or reads, and tells Claude to look up a short fact itself; the queue's directive
  says the same of non-code items. `noctis:lite`, `noctis:worker` and `noctis:deep` are told to work
  from their brief and read only what the task needs, and `worker` and `deep` keep their report under
  300 words.
- **The agents' compaction point.** All four agent files carry `autoCompactWindow: 200000`, so
  Claude Code compacts their context near 167k tokens instead of where the session compacts.
  `noctis ensure` keeps the files at `subagents.compactWindow`. Claude Code's own agents
  (general-purpose, Explore, Plan) take no such setting; the growth limit's 120k tokens bounds them.

`worker` and `deep` get no `maxTurns`: when a subagent runs out of turns, the main session gets its
last text, which may be half done. The wrap-up note comes first, then the stop, and the main session
is told when a stop cut a subagent short.

## The status line

- `⌛ weekly threshold ~4h 10m` and `⛔ weekly threshold ~4h 10m, no new subagents` come from the
  burn alarm (above).
- `🤖 3, 12% of the week's spend` counts the subagents this session opened and, once one was
  measured, what they cost as a share of what this machine's sessions cost since the weekly reset,
  at API list prices. The week's total comes from the cost each session's status line reports.

## The selftest times the subagent hooks

`noctis selftest` now times the Agent gate, `SubagentStart`, a subagent's tool batch and
`SubagentStop`, on a transcript of 500 tool calls it writes and removes, and leaves no subagent
count, journal entry or spend record behind. On the Linux machine 8.7.0 was built on, the gate took
3 ms, `SubagentStart` 2 ms, a subagent's batch 2 to 3 ms, and `SubagentStop`, in the background, 10
to 11 ms on a 4.9 MB transcript.

## `noctis report` counts as `noctis cost` does

Claude Code writes a response with several content blocks over several lines, each with the
response's usage, and later lines can carry larger output figures. 8.6.4's report counted the first
line of each response. It now reads the transcripts as `noctis cost` does and takes the largest
figure of each field. On the same 103 transcripts, output tokens rose 5.8 % (27,668,958 to
29,285,785) and the cost 1.0 % ($3,146.10 to $3,177.89); the calls and the cache figures stayed the
same. The report also reads a `projects` folder that is a link to another folder, which 8.6.4 did
not enter.

## Known limits

- A hook cannot end a call in progress, so the growth limit acts between tool batches: a subagent
  can go past its budget by a batch, and past the wrap-up note by `graceToolCalls` and a batch,
  before it stops.
- A subagent's context is read from the last of its replies in the last 4 MB of its transcript.
- `reportWords` is only asked for; nothing enforces it.
- `subagents.compactWindow` reaches only noctis's own agents, from Claude Code's next start.
- `SubagentStart` cannot refuse a subagent, so the gate acts on the `Agent` or `Task` call. A
  workflow's agents are not counted there; the workflow gate decides whether the workflow starts.
- The gate's counts and the spend are kept per account on this machine. The same account's sessions
  on another machine or in a browser are not counted, and the 🤖 share sets the subagents' cost at
  API list prices against what this machine's status lines reported for the week.
- The burn alarm takes its pace from the weekly readings this machine took, so it needs half an hour
  of them first; the window itself counts the whole account's use. After hours without readings it
  cannot tell when in them the window rose and spreads the rise evenly over them, so a rise that came
  at their end counts in full only once the next readings show the pace.
- A `queue.subagentAboveTokens` of 100000 that you set yourself before 8.7.0 is changed to 0 too:
  `config.json` does not tell it apart from what setup wrote. Set it again after the upgrade; from
  then on it stays.
- The limits in [RELEASE_NOTES_8.6.4.md](RELEASE_NOTES_8.6.4.md) still apply.

## Tests

8.7.0 adds 72 Go tests:

- `cost_test.go` (13):
  - Each response counts once at its largest figures; a copy in another transcript is skipped, and
    a sidechain line of a main transcript counts as its agent.
  - Main sessions and subagents apart, and the subagents ranked by what they cost.
  - Only the days asked for, and one session by the start of its id.
  - One-hour cache writes at twice the input price.
  - The table and the JSON, and a word when there is nothing to count.
  - A `projects` folder that is a link, for `cost` and `report`.
  - Columns lined up in every language, and wide text clipped and padded by the cells it takes.
- `burn_test.go` (16):
  - A week burning fast stops new subagents long before its reset; one that runs out just before its
    reset warns without stopping them; a pace that lasts the week gives no verdict.
  - Half an hour of readings and 2 points of rise; the lookback; the rise of hours without readings
    is spread over them; a new week starts its pace afresh; the usage endpoint's readings feed the
    pace; the history keeps a day and the reading before it.
  - The alarm turned off or loosened.
  - No refusal claimed while nothing refuses (guard off, `noctis off`, observe mode), in the notice
    or the status line.
  - The notice once per week and level, also on a typed prompt.
- `spawngate_test.go` (11):
  - The gate warns, then refuses past the session limit; it counts the last hour across sessions and
    keeps the last weekly points for the session.
  - Subagents and workflows are refused while the week burns too fast, and the refusal gives Claude
    and the journal its times in English while the notice keeps the user's language.
  - A subagent's own subagents count for its session.
  - Observe mode only journals; a refusal lifts the research route.
  - The gate turned off or loosened; its defaults match the shipped config; its records are kept
    only as long as they count.
- `growth_test.go` (13):
  - A subagent past its tool-call budget is told to wrap up, then stopped; one whose context passes
    its budget is told to wrap up.
  - Observe mode only journals, and the limit can be turned off.
  - A subagent starts with its budget and is counted; noctis's agents start with their own budget
    and report size; `noctis:lite` is told to wrap up at its smaller budget.
  - A user can loosen an agent's budget, and a budget named without the plugin prefix wins every
    time.
  - A subagent's spend is measured when it stops, and the ledger follows the weekly window.
  - The status line shows the session's subagents and their share of the week.
- `handoffroom_test.go` (8):
  - While no subagent can be opened, an item for another model, the large-context hand-off, a stuck
    item, an item that went up and a failing check stay in the session, and research is not routed.
  - The reason names what leaves no room.
  - The queue directive sends only research that needs several reads to `noctis:lite`.
- `freshlaunch_test.go` (1): while no subagent can be opened, a resumed large session gets no
  hand-off.
- `agentwindow_test.go` (2): noctis's agents compact at `subagents.compactWindow`, and
  `noctis ensure` keeps them there.
- `headroom_test.go` (2): a `config.json` on the former hand-off point turns the hand-off off once,
  and one on `configVersion` 1 moves only that.
- `router_corpus_test.go` (2): a short fact question stays in the session, and broad, long or
  multi-line research still goes to `noctis:lite`.
- `selftestcheck_test.go` (4): the selftest times the subagent hooks and leaves no trace of them;
  forgetting its probe takes back only what its hooks wrote; it leaves the burn notice to a real
  session; its transcript reads as the subagent it stands for.

Changed tests:

- The router corpora label six fact questions that now stay in the session, and the recall floor
  follows them.
- The fresh-context, relaunch and item-model tests of the hand-off set `queue.subagentAboveTokens`
  to 100000 themselves, and check that the shipped default hands nothing off.
- The hook guard tests of other limits turn `subagents.guard` off, so the spawn gate does not count
  their subagents.
- The argument-order test covers the burn and growth notices in Japanese, Chinese and Korean.
- The lab gains a subagent guard scenario of 16 checks: the spawn limits, the weekly room, the
  budgets, the growth stop, the report and the status line. Its router cases, its ETA checks and its
  fresh-context check follow the changes above. The contract test sends `SubagentStart` and
  `SubagentStop`, and the monkey test fires them with random agent types.
- The soak takes a refused subagent that the journal records as `deny-subagent-spawn` for Claude
  doing the work itself, not for a pause, and counts it. It counts a corrupted file only when noctis
  keeps a backup of it: `fable.json` has none, so a run whose one corruption hit it no longer reports
  a corruption without a recovery.
- The torrent takes a subagent refused by the gate the same way: the job goes on, and the refusal
  counts apart from the sessions stopped before the wall. A subagent refused past the wall, where
  the session should have paused, fails the run.
- The soak keeps its own weekly usage over time. A workflow or a subagent refused for a weekly burn
  counts as a burn refusal when the journal gives that reason and the soak's own usage over the last
  3 hours would reach the pause point before the reset; one its usage does not bear out is reported,
  and so is any other workflow refused with room left.

The results of the local rounds on this release are in its commit message.
