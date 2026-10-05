# noctis 8.6.2

8.6.2 keeps a queue going while nobody watches it. Two things held a session that a queue drove
until you came back: a question Claude put to you with its `AskUserQuestion` tool, and a permission
prompt or a question from an MCP server. noctis now hands such a question back to Claude, to decide
and note, and tells you once, on the desktop and, with a webhook set, on your phone, when a session
nobody watches waits for you. It approves nothing for you. 8.6.2 carries everything in 8.6.1
([RELEASE_NOTES_8.6.1.md](RELEASE_NOTES_8.6.1.md)).

## If you are upgrading

8.6.2 adds no settings and reads every file 8.6.1 wrote. In `hooks/hooks.json`, the PreToolUse
matcher gains `AskUserQuestion`, and the Notification matcher gains `permission_prompt`,
`worker_permission_prompt`, `elicitation_dialog` and `elicitation_url_dialog`. `state.json` keeps
the time of the last notice that a session waits in `notified`, as `waiting:<session>`.
`noctis why` has a `deny-question` line with the question and a `waiting-notice` line with what the
session waits for; observe mode journals `would-deny-question` and `would-waiting-notice`.

## A question while a queue runs

A queue runs while you are away, and a question from Claude held its session, and the queue with
it, until you came back to answer. The PreToolUse hook now refuses `AskUserQuestion` while both
hold:

- a trusted queue that is not held, or a checklist from a prompt (`queue.auto`), drives the session;
- it has an item Claude can take.

The question is not shown. The refusal names the queue and its open items and tells Claude to
decide, take the reading that fits the item and the project best, and go on:

- It notes the decision in one line with `noctis queue note "<the decision, and why>" --file <the
  queue>`, which `noctis queue status` and the daily digest list. For a checklist from a prompt,
  whose notes no digest reads, it says in its reply what it decided and why.
- When only you can settle it (an account, a payment, a key, a choice that is yours alone), it sets
  the item aside with `noctis queue defer <a unique part of the item's text> --reason "<what it
  waits on>"` and goes on with the next item.

`noctis why` has a `deny-question` line with the question on one line, the queue file and its open
items. Observe mode journals `would-deny-question` and refuses nothing.

The question still goes to you:

- where no queue drives the session, or its queue is not trusted;
- while noctis is paused, or queues are off for the run (`NOCTIS_QUEUE=off`);
- while the queue's check holds it;
- when the queue has no item Claude can take: every item ticked, or only your `(human)` items and
  deferred ones left.

## A session that waits for you

A permission prompt halts a session until someone answers it. Claude Code tells hooks about one
with a notification:

- `permission_prompt` once a prompt has been open for 6 seconds, and `worker_permission_prompt`;
- `elicitation_dialog` and `elicitation_url_dialog` when an MCP server asks for input.

noctis now sends one notification for these when nobody watches the session: one a queue drives,
one noctis relaunched, or one it woke in place.

- It names the folder, the session and what the session waits for, for example *myapp: session
  1a2b3c4d is waiting for you (Claude needs your permission to use Bash); the work stands still
  until you respond in the session.* It goes to the desktop and, with `alarm.webhook` set, to the
  webhook.
- It comes at most once in 10 minutes per session, and never while noctis is paused.
- A session you run yourself, with no queue driving it, gets none: Claude Code shows you the prompt
  there.
- It approves nothing and answers nothing.
- `noctis why` has a `waiting-notice` line with the kind of wait. Observe mode journals
  `would-waiting-notice` and sends none.

`idle_prompt` is not among them: Claude Code sends it whenever a session has sat idle for a minute
after a turn, so it would come after every finished run too.

To keep prompts from halting the work, allow the commands the plan runs in the project's
`.claude/settings.json` before you leave. Claude Code's
`CLAUDE_CODE_DISABLE_PERMISSION_PROMPT_NOTIFY_HOOKS` keeps its permission prompts from the hook,
and with them this notice.

## Known limits

- **A question comes back to Claude in every turn of a session a queue drives, the one you typed
  included.** noctis cannot tell a turn you sit at from one you left. To be asked again, end a
  queue you started with `/noctis:stop`, or pause noctis with `/noctis:pause`.
- **Both work on Claude Code only.** On the other hosts noctis follows, it refuses no question, and
  a session that waits for you sends noctis nothing.

The limits in [RELEASE_NOTES_8.6.1.md](RELEASE_NOTES_8.6.1.md) still apply.

## Tests

8.6.2 adds 9 Go tests, all of them on Windows too, in `unattended_test.go`:

- `hooks/hooks.json` starts the PreToolUse hook for `AskUserQuestion` and the Notification hook for
  the four waits, and not for `idle_prompt`;
- a question while a queue drives the session is refused with the queue's name and open items, the
  note and the deferral, and journaled on one line; a question with no text is journaled under the
  queue's name;
- a question in a checklist from a prompt is decided in the reply;
- a question reaches the user with no queue, an untrusted queue, a finished queue, only the user's
  own items open, a queue its check holds, noctis paused, and queues off for the run;
- observe mode journals `would-deny-question` and `would-waiting-notice` and does nothing else;
- a session a queue drives tells the user once in 10 minutes, and keeps the record of Claude Code's
  own auto-continue;
- each of the four waits notifies, on one line;
- a session noctis relaunched, or woke in place, notifies;
- a session the user runs, or one noctis relaunched while it is paused, stays quiet.

Outside the Go tests:

- `tests/contract.js` checks that the PreToolUse matcher starts the hook for `AskUserQuestion`, and
  that the Notification matcher starts it for the seven notifications noctis handles and not for
  `idle_prompt`.
- `tests/lab.js` has 3 more checks, through the binary: a question while the queue drives the
  session is refused with the note and the deferral, two permission prompts make one notification,
  and both are journaled.

The results of the local rounds on this release are in its commit message.
