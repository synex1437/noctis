# noctis 8.6.1

8.6.1 has noctis go on after five errors that Claude Code 2.1.289 can end a turn with and that
8.6.0 never started its StopFailure hook for: `max_output_tokens`, `unknown`,
`cloud_credential_error`, `verification_required` and `oauth_org_not_allowed`. A session that
stopped on one of them stayed stopped, with no word from noctis, until you typed in it again, and a
queue it drove waited with it. noctis now retries what a retry can mend, and does it quietly: it
sends a notification only when it gives up, or when the account needs you. 8.6.1 carries everything
in 8.6.0 ([RELEASE_NOTES_8.6.0.md](RELEASE_NOTES_8.6.0.md)).

## If you are upgrading

8.6.1 adds no settings and reads every file 8.6.0 wrote. The StopFailure matcher in
`hooks/hooks.json` names the five errors, and `state.json` gains an `outputCaps` record. By session,
it counts the replies cut at the output token maximum since the session last ended a turn.
`noctis why` has a `retry-giveup` line with `outputCap` when noctis gives up on such a reply, and an
`account-error` line for the two account errors that are new to it.

## The output token maximum

Claude Code ends a turn with `max_output_tokens` when a reply runs past its output token maximum,
*Claude's response exceeded the 32000 output token maximum*, most often while Claude writes a large
file in one go. Woken with the plain retry text, the session would likely write the same reply
again. noctis now:

- Retries once, 30 seconds later. Where noctis wakes sessions in place, it wakes this one; elsewhere
  a runner relaunches it, also when the relaunch starts a fresh session. Either way the session is
  told why it stopped and asked to go on in smaller steps: to write a long file in parts, creating
  it with the first part and adding each further part with Edit, and to keep each reply short.
- Sends no notification for that retry: not when it sets it, and not when it wakes or relaunches
  the session. The retry needs no usage data, as the output token maximum is no usage limit.
- Gives up when a reply is cut at the maximum again before the session ends a turn normally:
  - it journals `retry-giveup` and writes the resume note;
  - one notification says to have the long output written in parts, or to raise
    `CLAUDE_CODE_MAX_OUTPUT_TOKENS`, and how to go on: the command that resumes the session, or, in
    a Claude Code on the web session, a prompt typed there.
- Starts the count again once the session ends a turn normally, also while noctis is off. Tool calls
  do not start it again, and a fresh session that takes over carries it on.
- In observe mode retries nothing, so it counts nothing: each cut reply is journaled as the first,
  and noctis never says it gave up.

Claude Code also ends a turn with `max_output_tokens` when the model has reached its context window
limit. noctis takes that for a full context, as it takes the `invalid_request` that 8.6.0 handles: a
fresh session from the resume note 30 seconds later, as
[RELEASE_NOTES_8.6.0.md](RELEASE_NOTES_8.6.0.md#when-the-context-stays-full) describes.

## A connection or credentials that fail

Claude Code ends a turn with `unknown` when it cannot reach the API. The connection may have
dropped or been refused, the server's name or the route to it may have failed, or a proxy may have
turned it away. It also ends a turn with `unknown` for an error it does not name. It ends one with
`cloud_credential_error` when it could not load the AWS or Google Cloud credentials it reaches
Claude with on Bedrock or Vertex AI. A retry mends both once the network or the credentials are
back. 8.6.1 retries them on the `wait.retryMinutes` steps (10, 20, 30, 45, then 60 minutes), in
place or by a runner, like a failure no usage limit explains:

- No notification says a retry is coming, or that the session went on.
- The retries need no usage data. A runner relaunches the session on the steps also where noctis
  has none, as on Bedrock and Vertex AI.
- After five retries that fail again, 165 minutes or more after the first failure with the default
  steps, noctis gives up. It journals `retry-giveup` and sends one notification with the command
  that resumes the session.
- Neither error is put down to a usage limit, however near one the windows are: the retry comes on
  these steps, not at a window's reset, and noctis does not move the session off Fable. A runner
  still waits for the reset of a window that is past its pause point when the retry comes, as it
  does before every relaunch.
- In a Claude Code on the web session, the retries go on past the fifth, an hour apart, until
  `wake.maxMinutes` (330) have passed since the first failure, as for the other failures there. The
  notification at the give-up says to type a prompt in the session.

This is how noctis handles these errors on Claude Code. The other hosts noctis follows name an error
by its text, so there `unknown` is whatever noctis could not name, a usage limit among them. It is
handled as in 8.6.0, with a notification for each retry.

## Account errors

`verification_required` (the organization has to be verified before it can go on) and
`oauth_org_not_allowed` (the organization has turned off Claude subscription access for Claude Code)
join `billing_error`, `account_on_hold` and `authentication_failed`. A retry cannot mend them, so
noctis retries nothing:

- one notification per error every six hours says the account needs you;
- `noctis why` has an `account-error` line.

## Known limits

- **`unknown` covers more than a connection.** Claude Code also ends a turn with it when a gateway
  wants you to sign in again, and for API errors it does not name.
  - noctis retries these quietly too, so the notification comes only when it gives up: 165 minutes
    or more after the first failure, with the default steps.
  - An expired AWS SSO sign-in behind a `cloud_credential_error` goes the same way. Once you sign in
    again, the next retry goes through.
- **The retry after the output token maximum relies on Claude following the note.** A reply that is
  cut again before a turn ends is given up on after that one retry.

The limits in [RELEASE_NOTES_8.6.0.md](RELEASE_NOTES_8.6.0.md) still apply.

## Tests

8.6.1 adds 11 Go tests, 9 on Windows.

- `outputcap_test.go` (9 tests):
  - a reply cut at the output token maximum is retried once, 30 s later, with no notification and
    no fresh start;
  - a reply cut again before a turn ends is given up on, with a journal line, a checkpoint and one
    notification. Tool calls in between keep the count, and a turn that ends starts it again;
  - a fresh session that took over the retry and is cut again is given up on;
  - observe mode counts no retries and gives none up;
  - a cloud session that is given up on is told to go on with a prompt;
  - the session woken in place is asked to go on in smaller steps;
  - `max_output_tokens` is taken for a full context when it names the context window limit, and for
    the output token maximum otherwise;
  - `unknown` and `cloud_credential_error` are retried on the `wait.retryMinutes` steps with the
    5-hour window at 95 %, with no notification until the give-up after five retries; on another
    host, `unknown` keeps its notification;
  - a retry set quietly is relaunched with no notification, also where there is no usage data, while
    a failure the limits do not explain is told of when it is set and when the session is
    relaunched.
- `outputcaplaunch_test.go` (2 tests, not on Windows): a relaunch after the output token maximum,
  and a relaunch that starts a fresh session, hand the session the note.

Two existing Go test files changed:

- `hookguard_test.go`: `verification_required` and `oauth_org_not_allowed`, with the texts Claude
  Code ends a turn with, are not retried and are journaled as account errors.
- `contextfull_test.go`: the stand-ins for the desktop notifiers can be set up on their own.

Outside the Go tests:

- `tests/contract.js` checks that the StopFailure matcher starts the hook for the five errors.
- `tests/lab.js` has 6 more checks:
  - an `unknown` at 97 % of the 5-hour window is retried on the steps;
  - the retry comes 30 s after the output token maximum;
  - neither sends a notification;
  - when the reply is cut again, noctis gives up and sends one notification;
  - `verification_required` is journaled as an account error.
- `tests/monkey.js` has two more moves:
  - a failure retried quietly: the output token maximum, `unknown` or `cloud_credential_error`, now
    and then in a cloud session;
  - an account error.

  Its full contexts include the context window limit. It checks that a hook that stores a retry
  sends no notification and a hook that gives up sends one, that a runner that resumes such a retry
  does not say a limit reset, and that an account error stores no retry and sends a notification
  only with its `account-error` line.
- `tests/soak.js` (with `--hard 1`) has turns that stop at the output token maximum, on `unknown`
  or on `cloud_credential_error`, and full contexts at the context window limit. It checks the
  retries and the notifications as the monkey does, and that the relaunch after the output token
  maximum carries the note.

The results of the local rounds on this release are in its commit message.
