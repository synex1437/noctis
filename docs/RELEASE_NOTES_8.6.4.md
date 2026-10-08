# noctis 8.6.4

8.6.4 lets a dynamic workflow start with 15 points of room before a pause point instead of 25, has
setup switch marketplace auto-update on again, and knows Claude Haiku 5.5, which Anthropic released
on October 7, 2026 and Claude Code 2.1.293 made its `haiku`. The README's pictures and the social
preview are drawn again without glows. A hook whose pause another window takes over the moment it
is stored now ends its turn instead of going on as well. 8.6.4 carries everything in 8.6.3
([RELEASE_NOTES_8.6.3.md](RELEASE_NOTES_8.6.3.md)).

## If you are upgrading

- `config.json` gains `"configVersion": 1` at the next setup or session start. A
  `credits.fanOutHeadroom` of 25, the earlier default setup wrote there, becomes 15 then, and an
  `effort` saved with a role on a Haiku model is removed then. Until then noctis reads `config.json`
  as if both were done. Any other headroom stays, and so does a 25 or a Haiku effort set after that.
- Setup writes `autoUpdate` in `<config dir>/plugins/known_marketplaces.json` (not with
  `--updates keep` or `off`) and records it in `config.json` under `managedAutoUpdate`, as it did
  when its command still worked.
- No new settings. `hooks/hooks.json` is unchanged. 8.6.3 reads the new `config.json` and keeps
  `configVersion`, so going back and forth does not move the headroom again.

## A workflow needs 15 points of room

A dynamic workflow fans out many agents at once and can burn the last points of a window between two
checks, so a new one is refused unless every usage window it can draw on has
`credits.fanOutHeadroom` points of room before its pause point. With 25 and the default pause points
(5-hour 92 %, weekly 95 %), no workflow started past 67 % of the 5-hour window or 70 % of the weekly
one: a weekly window with 17 points left refused every workflow, while a single session could go on
for a long time.

The default is now 15, so a workflow may start up to 77 % of the 5-hour window and 80 % of the
weekly one (82 % of the Fable bucket, when it counts). Nothing else changes: agents already running
still meet the pause point and the 100 % stop, the refusal still says how much room each window has,
and `0` still turns the check off.

Setup writes the shipped defaults into `config.json`, so nearly every `config.json` holds the former
25. noctis moves a 25 there to 15 once and records `configVersion` 1 beside it.

## Setup switches marketplace auto-update on again

A third-party marketplace has auto-update off by default. Setup switched it on with
`claude plugin marketplace update <name> --auto-update`, which Claude Code 2.1.281 already refused
and 2.1.293 no longer has, so every setup said it could not enable auto-update and sent you to
`/plugin`. Setup now does what that menu does: it sets `autoUpdate: true` on the noctis
marketplace's entry in `<config dir>/plugins/known_marketplaces.json`. The rest of the file stays as
it was.

- It works under the lock Claude Code takes on that file, `known_marketplaces.json.lock`. It waits
  up to 2.5 seconds for a lock Claude Code holds, and takes over one nobody touched for more than 10
  seconds, which a process that ended left behind. When the lock stays held, setup writes nothing
  and sends you to `/plugin`.
- Where `extraKnownMarketplaces` in your settings or managed settings sets `autoUpdate` for the
  marketplace, that value decides, as in Claude Code, and setup writes nothing. When it is on, setup
  says auto-update is on. When your own settings switch it off, setup sends you to `/plugin`. When
  managed settings switch it off, setup says nothing.
- A marketplace an administrator seeds (`CLAUDE_CODE_PLUGIN_SEED_DIR`) and the `claudeai` and
  `pluginDirectory` marketplaces are left alone: Claude Code keeps them up to date itself.
- Without the file, without an entry for the marketplace or with a file that cannot be read, setup
  writes nothing and sends you to `/plugin`.

As before, setup records in `config.json` what it switched on, and `noctis install --uninstall` sets
it back while it is still on, now under the same lock. An auto-update that was already on is not
recorded, so the uninstall leaves it on.

## Claude Haiku 5.5

Haiku 5.5 costs $0.10 / $0.50 per million input / output tokens ($0.125 for cache writes, $0.01 for
cache reads), a tenth of Haiku 4.5's. A request whose prompt is over 100,000 tokens costs $0.50 /
$2.50 for all of it. Its 1M window is its own. From Claude Code 2.1.293, `haiku` is Haiku 5.5 on
Anthropic's API. An older Claude Code, and Bedrock, Vertex AI, Foundry and Claude Code's other
providers, still give Haiku 4.5.

**The profiles stay as they are.** Output digests and file search run on `haiku`, so with Claude
Code 2.1.293 or newer on Anthropic's API they run on Haiku 5.5 with nothing to set up. Nothing moves
to it from Opus 5.5 or Sonnet 5.5. On the benchmarks Anthropic published, Haiku 5.5 is well behind
Sonnet 5.5 on code (SWE-Bench Pro 64.8 vs 81.3, Terminal-Bench 4.0 39.2 vs 70.6), and Anthropic
pitches it as a subagent beside Opus 5.5 and Sonnet 5.5, which is what the digest and file search
roles are.

**Effort.** Haiku 5.5 has effort levels (medium when none is set) and Haiku 4.5 has none, as Claude
Code has it.

- A role on `haiku` or on a Haiku 5.5 id now keeps the effort you give it, as in
  `--digest haiku:high`.
- The effort reaches the role's agent, and for the code role `settings.json`, where setup saves a
  level for `haiku` under `modelSettings.claude-haiku-5-5`.
- A relaunch on such a model passes `--effort`.
- A role on Haiku 4.5 or a Claude 3 Haiku still gets no effort, and setup says so.

Before 8.6.4 noctis gave no Haiku model an effort, and one in `config.json` went nowhere (the
profiles before 7.0.0 saved one with the digest role). So that such a role runs as it did, an effort
saved with a Haiku role is removed once, with the `configVersion` 1 record.

**Prices in `noctis report`.**

- Haiku 5.5 is priced at its list prices.
- A request whose prompt (input, cache reads and cache writes together) is over 100,000 tokens is
  counted wholly at $0.50 / $2.50 / $0.625 / $0.05, output included, as Claude Code reckons it.
  One-hour cache writes still cost twice the input price.
- A model with an entry under `report.pricing` costs that entry's prices for every request, as in
  Claude Code.
- From the same day, Anthropic halved Sonnet 5.5's cache-read price to $0.10 per million tokens, and
  the report prices them so.

**Model names.** noctis files a model's own settings (its effort, its compaction window) under the
name Claude Code 2.1.293 gives it. The catalog gains `claude-haiku-5-5`, `haiku` stands for it on
Anthropic's API and for Haiku 4.5 on the other providers, and its ids on each provider follow Claude
Code's. The tests compare noctis's names with Claude Code 2.1.293's own answers
(`testdata/claudecode-models.json`, rebuilt from it).

## The README's pictures, drawn again

The banner, the before-and-after picture and the terminal demo in the README, and the social
preview, are drawn again. Their blurred strokes, drop shadows and wide radial gradients showed as
bands of color, worst around the infinity mark, and their fine grid and hatch patterns shimmered
when GitHub scaled them down. They are now flat shapes with short linear gradients, and their text
is set in Geist and Geist Mono and turned into outlines, so it looks the same on every system. They
say what they said before, and their alt texts are unchanged. `docs/social-preview.jpg` is made
again from the new `docs/social-preview.svg`; GitHub takes it under Settings → General → Social
preview → Edit → Upload an image.

## A pause taken over as it is stored

When a hook pauses the session, it stores the pause and reads it back. If another window took the
pause over in between, with a `claude --resume` of the session, the hook found no pause, said
`could not save the pause (is another window writing, or the disk full?)` and let its turn go on,
so the session went on in two windows. The hook now sees that the pause was taken and stops as a
hook holding the pause does, with `⏹ While this window waited, the session went on in another
window, so this one stops here and the work does not run twice.` and `continued-elsewhere` in
`noctis why`. No runner is scheduled for a pause that is already taken. 8.6.3 has the same race; CI
found it on macOS.

## Known limits

- A `fanOutHeadroom` of 25, or an effort with a Haiku role, that you set yourself before 8.6.4 is
  changed too: `config.json` does not tell them apart from what setup wrote. Set them again after
  the upgrade; from then on they stay.
- Where `haiku` is still Haiku 4.5 (Claude Code before 2.1.293, or another provider), an effort you
  give a role on it reaches Claude Code, which drops it.
- Claude Code 2.1.293's own cost display still counts Sonnet 5.5's cache reads at $0.20, so for
  Sonnet 5.5 it shows more than `noctis report`.
- A pause that `noctis cancel` removes between a hook storing it and reading it back is still
  reported as not saved. The turn goes on, as after any cancel.
- The limits in [RELEASE_NOTES_8.6.3.md](RELEASE_NOTES_8.6.3.md) still apply.

## Tests

8.6.4 adds 14 Go tests. Three of them replace the one that checked no Haiku effort is stored:

- `headroom_test.go` (3):
  - The shipped config and the built-in default both ask for 15 points.
  - A workflow launches at 77 % of the 5-hour window and 80 % of the weekly one, and is refused just
    above either, with a refusal that asks for 15 points.
  - A `config.json` on the former 25 moves to 15 once and records `configVersion` 1, and a 25 set
    after that stays through loads and merges.
  - Any other headroom (0, 10, 24, 26, 40) stays, and a `config.json` with none of its own takes 15
    beside its own values.
- `marketplaceautoupdate_test.go` (6):
  - Setup switches auto-update on in `known_marketplaces.json`, changing only that field. It leaves
    no lock and records what was there before, either no value or `false`.
  - It leaves an auto-update that is already on alone and records nothing.
  - It waits for a lock that is let go, takes over one left behind, and gives up on one held all
    along, writing nothing and sending you to `/plugin`.
  - A value under `extraKnownMarketplaces` in settings or managed settings decides, as in Claude
    Code.
  - Seeded, `claudeai` and `pluginDirectory` marketplaces are not touched.
  - Without the file, without an entry or with a broken file, setup writes nothing and sends you to
    `/plugin`.
- `profiles_test.go` (3):
  - Which Haiku names take an effort, in every form Claude Code knows (alias, `[1m]`, dated, Bedrock
    and Vertex AI ids).
  - An effort from a flag is stored for `haiku` and Haiku 5.5, and not for Haiku 4.5 or a Claude 3
    Haiku.
  - An effort saved with a Haiku role before 8.6.4 is dropped once, through a load and a merge, and
    one set after that stays.
- `modelprices_test.go` (1):
  - Haiku 5.5's prices.
  - A 100,000-token prompt at $0.10 / $0.50 and a 100,001-token one wholly at $0.50 / $2.50, its
    one-hour writes at twice that input price.
  - With an entry under `report.pricing`, the entry's flat price.
- `singleowner_test.go` (1):
  - Another window takes the pause between the hook's write and its read-back. The hook ends its
    turn as continued elsewhere and journals no failed pause.

Changed tests:

- The workflow gate tests count 15 points of room.
- The setup, uninstall and lab checks of marketplace auto-update read `known_marketplaces.json`
  instead of a fake `claude`'s calls, and check that no `--auto-update` is asked for. The check that
  setup runs no interpreter planted in a project drops its `claude plugin marketplace update` probe,
  since setup no longer runs it.
- The price tables take Haiku 5.5 and Sonnet 5.5's $0.10 cache reads.
- The model-name tests compare with answers rebuilt from Claude Code 2.1.293.
- The earlier-profile test loads its `config.json` as noctis does.
- The lab's check that an effort for a model that takes none is named and not stored gives that
  effort to Haiku 4.5, since `haiku` now takes one. A new lab check holds that an effort for
  `haiku` is stored and reaches its agent.
- The test of a claim whose wait is dropped as soon as it is stored also checks that such a claim
  is not taken for one continued elsewhere.

The results of the local rounds on this release are in its commit message.
