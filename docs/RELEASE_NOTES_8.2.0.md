# noctis 8.2.0

8.2.0 gives an item a queue gets stuck on one try on a stronger model before it is set aside, and
tells you which setup finishes your queue for less. When a session keeps stopping on an item without
progress, the continuation that set the item aside in 8.1.0 now first hands it, once, to a fresh
subagent one step up: Opus for a session on Sonnet or Haiku, noctis's new `deep` agent (Opus at max
effort) for one on Opus below max. The subagent gets a brief of what was tried and where it failed.
The session keeps its model, and so its prompt cache; only that item runs on the stronger model. For
each queue noctis also counts what each setup finished, what went up and what share of the weekly
limit it took, and once the numbers say so, `noctis queue status`, `noctis status` and the daily
digest name the profile that finishes the queue for less. It carries everything in 8.1.0
([RELEASE_NOTES_8.1.0.md](RELEASE_NOTES_8.1.0.md)).

## If you are upgrading

8.2.0 adds two settings, `queue.escalate` (`auto`) and `queue.maxEscalationsPerDay` (5), and one
agent, `noctis:deep`; `hooks.json` is unchanged.

- **A stuck item now goes to a stronger model before it is set aside.** That item takes more of the
  weekly limit than the session would spend on it. Set `queue.escalate` to `off` to set a stuck item
  aside at once, as in 8.1.0, or lower `queue.maxEscalationsPerDay`.
- **A copy made by the install scripts gets the agent when you run the script again.** The copy is
  now 20 files (21 on Windows). The plugin itself carries `agents/deep.md`, and `noctis setup` leaves
  it as it is.

## A stuck item goes up once

In 8.1.0 the last continue before `queue.maxIdleContinues` (4) asked Claude to find what holds the
item up and, if that could not be resolved then, to defer it and go on with the next item. The model
that got stuck was asked to judge whether the item could be done, and an item a stronger model would
have finished waited for the morning.

Now that continue first hands the item to a stronger model, when there is one:

- **One step up.** With `queue.escalate` `auto`, the step starts from the model the item is tagged for
  (`(sonnet)` and the like, see 8.1.0), else from the session's model (the one its status line
  reports, else `models.primary`). Sonnet and Haiku go to Opus through a general-purpose subagent;
  Opus below max effort goes to `noctis:deep`, which runs on Opus at max effort; Opus at max and
  Fable have no step up. The session's effort is `CLAUDE_CODE_EFFORT_LEVEL`, else
  `env.CLAUDE_CODE_EFFORT_LEVEL` or `effortLevel` in `settings.json`, else `models.effort`. So a
  Balanced session (Sonnet · high) hands the item to Opus, a Code or Search session (Opus · xhigh) to
  `noctis:deep`, a SYNEX session (Opus · max) sets it aside as before, and a custom setup follows the
  same rule. `queue.escalate` can also name the model (`opus`, `sonnet`, `haiku` or `fable`; a Fable
  whose quota is out falls back to `auto`) or be `off`.
- **A fresh subagent with a brief.** The Stop hook tells Claude to hand the item, once and in the
  foreground, to that subagent with a brief that stands on its own: the goal and what done means,
  what it tried, where and why it failed (the errors verbatim), and the files and decisions the item
  needs, so the subagent does not repeat those attempts. Then Claude checks the subagent's work and
  ticks the item itself. You see
  `⇧ 3 continuations in a row without a ticked item or a commit: "…" goes once to a subagent on Opus before it is set aside.`
- **The session stays as it is.** Its model does not change, so its prompt cache stays warm, and the
  subagent's reading stays out of its context. The deep agent has the tools and the write rules of
  the main session.
- **The queue file is not touched.** noctis records the item in its own state (the last 50 items of
  each queue file, kept 30 days after the file was last noted), so the file and its trust stay as
  they are. The continuations after, in other sessions too, keep the item with its stronger model
  until it is ticked.
- **If the stronger model gets stuck too, the item is set aside.** After the hand-over the count of
  continues without progress stands one short of the last. The next stop without progress asks
  Claude to set the item aside as in 8.1.0, with the root cause the stronger model found as the
  reason. You see `⚠ Opus did not finish "…" either: …`, and the digest says so. The stop after that
  gives up, as before.
- **A pause point and a daily cap come first.** Nothing goes up while a pause is due, on a host
  without subagents, for an item that went up before, or once `queue.maxEscalationsPerDay` items went
  up on this local day (0 for none). The item is then set aside as in 8.1.0.

`noctis why --json` shows `escalateTo` and `escalated` on the Stop hook's `continue-queue`, and
`escalateCapped` when the cap held an item back. `noctis queue status` names the items with a stronger
model now:

```
  ⇧ With a stronger model now: migrate the users table → Opus
```

`noctis queue status --json` lists them under `models.escalated`, with the outcome of each (`done`,
`stuck` or open), and the daily digest counts the items that went up since the last digest, how many
of them were done there and how many got stuck there too.

## The numbers pick the profile

Which profile finishes a queue for less depends on the queue. A cheaper model that gets stuck often
can cost more than a stronger one that does not. noctis now counts, for each queue file, what each
setup (the model and effort a session runs on) did:

- the items it finished: those ticked between two pace notes (see 8.1.0), credited to the setup of
  the session that made the second note;
- the stuck items that went up, and how many of them were done there;
- the stuck items that were set aside without going up;
- the share of the weekly limit its items took, counted as the pace is (two notes of one weekly
  window at most 6 hours apart). The items that went up are included, so the stronger model's work
  counts against the setup that handed the item over.

`noctis queue status`, `noctis status` under the queue's name and the daily digest give a line for
each setup:

```
  Sonnet · high: 24 item(s) done; a whole weekly limit covers about 60 such items; 3 stuck item(s) went to a stronger model, 3 of them done there
```

Once the numbers say so, a hint follows:

- **Two setups measured.** When two setups have 6 items each measured this way, and a weekly limit
  covers at least 15 % more items on one of them, the hint names that one:
  `Hint: on this queue a whole weekly limit covers about 60 items on Sonnet · high against 40 on Opus · xhigh, escalations included: Balanced (Sonnet · high) finishes it for less.`
- **One setup.** Otherwise the setup with the most items is weighed once it has 8. On Sonnet or
  Haiku, when a quarter of them went up, the hint names the profile that starts on the stronger model
  (Code; Balanced for Haiku). On Opus, when none went up or got stuck, it names the profile one step
  down, which hands a stuck item up (Balanced for Opus below max, Code for Opus at max).

`noctis queue status --json` gives the counts as `models.setups` and the hint as `models.hint`.
noctis never switches the profile itself, and none of this costs a model call.

## Known limits

- **A stuck item goes up through a subagent.** On a host without subagents it is set aside as in
  8.1.0.
- **The hand-over is Claude's call.** noctis asks for it and records the item as gone up; when Claude
  works on the item itself instead, the next stop asks it to set the item aside all the same.
- **The subagent starts from a fresh context.** It reads what the item needs again, so an item that
  goes up costs more than another continue of the session would. `queue.maxEscalationsPerDay` bounds
  how many do.
- **noctis reads the effort from the environment, the settings and its config.** An effort changed
  another way during a session is not seen: an Opus session moved to max that way still hands a
  stuck item to `noctis:deep`.
- **noctis knows an item by its text.** An item reworded after it went up is a new item to noctis.
- **The hint is as good as its numbers.** The setups may have run different parts of the queue,
  items differ in size, and other work in the same account in those windows counts toward the weekly
  share. When two sessions on different setups drive one file, an item is credited to the one whose
  stop noticed the tick.
- The limits in [RELEASE_NOTES_8.1.0.md](RELEASE_NOTES_8.1.0.md) still apply, but for the first half
  of one: an item Claude cannot get past now goes to a stronger model before Claude is asked to set
  it aside.

## Tests

13 new Go tests for a stuck item that goes up:

- the last continue handing the item from Sonnet to Opus with a brief, the user's notice and the
  journal, the next stop asking to set it aside with the stronger model's root cause, and the queue
  giving up at the stop after;
- another session keeping the item with its stronger model;
- Opus below max going to the deep agent, and Opus at max setting the item aside as before;
- `queue.escalate` off, the daily cap and a pause that is due each keeping the item from going up;
- the step up for each model, tag and setting, and a host without subagents;
- the effort read from the session, then the settings, then the config;
- an item done on the stronger model credited to the setup with its share of the weekly limit;
- the setup titles and the hints for two setups and for one;
- the digest, `queue status` (text and JSON) and a ticked item no longer shown as with a stronger model;
- the last 50 items kept per file;
- the deep agent's frontmatter.
