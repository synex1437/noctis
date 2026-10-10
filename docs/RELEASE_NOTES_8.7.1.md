# noctis 8.7.1

8.7.1 moves the weekly pause point from 95 % to 97 %: a session now pauses when the weekly window
(all models) reaches 97 %, or earlier when a burst or the burn rate says the next turns would cross
it. The 5-hour window still pauses at 92 % and the Fable bucket at 97 %. The 100 % stop is
unchanged, so no window spends paid usage credits. 8.7.1 carries everything in 8.7.0
([RELEASE_NOTES_8.7.0.md](RELEASE_NOTES_8.7.0.md)).

## If you are upgrading

- `config.json` gains `"configVersion": 3` at the next setup or session start, and a
  `thresholds.weeklyAll` of 95, the earlier default setup wrote there, becomes 97 then. Until then
  noctis reads `config.json` as if that were done. Any other value stays, and so does a 95 set after
  that.
- A `config.json` that `setup --preset aggressive` wrote before 8.7.1 keeps that preset's former
  weekly 97, where `balanced` now pauses too. Run `setup --preset aggressive` again for the new 98.
- No new settings. `hooks/hooks.json` and the agent files are unchanged.
- 8.7.0 reads the new `config.json` as it is: it keeps `configVersion` 3 and pauses at the 97
  stored there.

## The weekly window pauses at 97 %

What noctis measures from the weekly pause point moves with it, 2 points later:

| Weekly window | 8.7.0 | 8.7.1 |
| --- | --- | --- |
| The session pauses | 95 % | 97 % |
| The warning before the pause starts (6 points before it, up to 15 while usage climbs fast) | 89 % | 91 % |
| The usage endpoint is asked more often (8 points before it) | 87 % | 89 % |
| A new subagent opens (`subagents.weeklyRoom` 15) | up to 80 % | up to 82 % |
| A new workflow starts (`credits.fanOutHeadroom` 15) | up to 80 % | up to 82 % |

While no subagent can be opened, a queue keeps its items in the session and research stays there
too, now past 82 %. The burn alarm projects when the week reaches 97 % instead of 95 %, and the
queue's pace counts the room left before 97 %. The 100 % stop and the burst and stale-data
projections are unchanged.

`setup --preset` follows: `balanced` pauses at 92 / 97 / 97 (5-hour / weekly / Fable), and
`aggressive` moves from 96 / 97 / 98 to 96 / 98 / 98, so it still pauses later than `balanced` on
every window. `conservative` stays at 85 / 82 / 90.

Setup writes the shipped defaults into `config.json`, so nearly every `config.json` holds the former
95. noctis moves a 95 there to 97 once and records `configVersion` 3 beside it.

## Known limits

- A 95 set by hand before 8.7.1 looks the same as the former default and moves to 97 too. Set it
  again after the update and it stays.

## Tests

8.7.1 adds 2 Go tests, in `thresholdpresets_test.go`:

- A `config.json` on the former 95 moves to 97 once, through a load and a merge, and records
  `configVersion` 3. Its 5-hour and Fable pause points stay, and so does a `subagentAboveTokens` of
  100000 under `configVersion` 2. A 95 set after that stays.
- Any other weekly pause point (82, 90, 94.5, 96, 98, `0`, `false`) stays.

Changed tests:

- The shipped thresholds and `--preset balanced` pause at 92 / 97 / 97, and each preset still pauses
  later than the one before it.
- The tests that put weekly usage at a set distance from the pause point read 2 points higher, so
  what they check is unchanged: the spawn gate and the hand-off room (87 %, 10 points before it), the
  workflow gate (82 % starts, 82.5 % is refused), the queue's pace (79 points of room at 18 %), a
  prompt and control commands at the pause point (97 %), a cloud session past it (98 %), the warning
  before it (95 %), and the status lines with a threshold switched off.
- The lab's weekly room check reads the weekly window 10 points under the shipped pause point
  instead of at 85 %.

The results of the local rounds on this release are in its commit message.
