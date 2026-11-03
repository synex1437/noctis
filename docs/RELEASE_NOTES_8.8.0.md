# noctis 8.8.0

noctis now installs and works when Claude Code runs on a local model (Ollama, LM Studio, llama.cpp,
vLLM) or on any other endpoint that `ANTHROPIC_BASE_URL` names. Up to 8.7.2 it took every session for
one on Anthropic's API: it held a session on a local model to the plan's 5-hour and weekly limits,
which that session does not spend, so with the plan near a limit it paused it, and otherwise the
status line waited for limit data that never came. The doctor asked for a sign-in, a relaunch opened
Claude Code without the session's endpoint, a subagent pinned to `sonnet` or `haiku` asked the local
server for a model it does not have, and Claude Code counted 200k tokens of window for a 32k model.
8.8.0 carries everything in 8.7.2 ([RELEASE_NOTES_8.7.2.md](RELEASE_NOTES_8.7.2.md)).

## If you are upgrading

- No new configuration keys, and `config.json` keeps `configVersion` 4. `hooks/hooks.json` is
  unchanged.
- For a local model, run `/noctis:setup` once more, with `--context-window` and the window you load
  the model with (such as `--context-window 64k`). Setup records what it writes, and uninstall takes
  it back.
- Nothing changes for a session on Anthropic's API.

## On another endpoint

| | 8.7.2 | 8.8.0 |
| --- | --- | --- |
| The plan's 5-hour and weekly limits | applied: a pause near a limit the session does not spend | not applied; the status line says *no plan limits* |
| The doctor, `noctis status` and the session-start check | asked for a sign-in and checked thresholds, paid credits and usage | say the limit guard is off there; the doctor names the model's context window |
| A relaunch, a queue wake, a runner after a wait | Claude Code without the session's endpoint, token and model | Claude Code on the session's endpoint, with its token, model and model mappings |
| A subagent noctis pins to `sonnet` or `haiku` | asked the server for that Claude model | pinned only to a model the endpoint serves; otherwise the session's own |
| `noctis:lite` and `noctis:digest` | the role's Claude model | `model: inherit` where `settings.json` names an endpoint that does not serve it |
| Setup | wrote the profile's Claude model and effort | writes neither, puts your model back where it had written its own, and sets `CLAUDE_CODE_ATTRIBUTION_HEADER=0` |
| A model Claude Code does not know | counted with a 200k window | `setup --context-window <tokens>` gives it its own |

The queue, compaction (lean compaction too), the 529/5xx retry, relaunches and the scheduler work as
on Anthropic's API.

## Which sessions count

A session runs on another endpoint when its `ANTHROPIC_BASE_URL` points anywhere but
`api.anthropic.com`, unless `_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL` is set, or when a provider
switch such as `CLAUDE_CODE_USE_BEDROCK` is on: Bedrock, Vertex AI and Microsoft Foundry bill by the
token, not from a plan. noctis reads the variables as Claude Code does: in a session from its
environment, in a terminal from `settings.json` → `env` and then the shell.

Setup and the agent files change what every session of the account reads, so they go by the
endpoint the account's `settings.json` names. An endpoint only one shell or launcher names leaves
them as they are for the sessions that still run on Anthropic's API; the hooks and the status line
still go by each session's own environment.

## Subagents

Claude Code asks the endpoint for a subagent's model by name. noctis pins a subagent, and gives its
research and digest agents a model, only where the endpoint serves it: a Claude name once
`ANTHROPIC_DEFAULT_OPUS_MODEL`, `ANTHROPIC_DEFAULT_SONNET_MODEL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL` or
`ANTHROPIC_DEFAULT_FABLE_MODEL` maps it, any other name as it is. The rest run on the session's own
model. On Bedrock, Vertex AI and Foundry the pins stay, since Claude Code maps the Claude names there
itself.

## The context window

Claude Code knows the window of Claude models only and counts 200,000 tokens for any other, so with
a 32k or 64k local model it compacts too late. `setup --context-window` writes
`CLAUDE_CODE_MAX_CONTEXT_TOKENS`, which Claude Code takes as the window of a model it does not know:
a whole number of tokens from 1000 to 10,000,000, also written as `64k` or `0.128m`. A Claude
model's window does not change. `--context-window off` and uninstall take back what setup wrote,
and a value you had before comes back. The doctor names the window, or says that a model Claude Code
does not know needs one: the model in `ANTHROPIC_MODEL` or `settings.json`, through an alias mapping,
or else the model the latest session ran on.

## The attribution line

Claude Code puts an attribution line at the top of the system prompt, and it changes with every
request. A local server then cannot reuse the prompt it has already read and reads the whole
conversation again on every turn. LM Studio's and vLLM's guides set `CLAUDE_CODE_ATTRIBUTION_HEADER=0`
for that, and setup now does the same in `settings.json` → `env` when the file names such an
endpoint and no value is set (recorded as `managedAttributionHeader`). A setup back on Anthropic's
API and uninstall take it back.

## Relaunches

A relaunch, a queue wake and a runner started by systemd, launchd or Task Scheduler now start Claude
Code with the session's own `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY`,
`ANTHROPIC_MODEL`, model mappings (`ANTHROPIC_DEFAULT_*_MODEL`, `ANTHROPIC_SMALL_FAST_MODEL`,
`ANTHROPIC_CUSTOM_MODEL_OPTION*`, `CLAUDE_CODE_SUBAGENT_MODEL`), `ANTHROPIC_CUSTOM_HEADERS`,
`API_TIMEOUT_MS`, `CLAUDE_CODE_MAX_CONTEXT_TOKENS`, `CLAUDE_CODE_ATTRIBUTION_HEADER` and switches
(`DISABLE_PROMPT_CACHING`, `DISABLE_TELEMETRY`, `DISABLE_COST_WARNINGS`,
`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`, `CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS`,
`_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL`). The token stays off command lines and out of launchd
plists: a runner reads it from `proxies/<session>.json` (mode 0600) and removes the file, the window
launcher on macOS and Linux exports it in a script only you can read and deletes that script as it
starts Claude Code, and on Windows the launcher reads it from its launch file in noctis's folder
under your profile, which is removed once the session it started ends.

## Known limits

- The agent files are shared by every session of the account. With the endpoint only in a shell or
  launcher, `noctis:lite` and `noctis:digest` keep the role's Claude model; map the aliases or put
  the endpoint in `settings.json` → `env`.
- The Agent tool takes only `sonnet`, `opus`, `haiku` and `fable` as a model. When the model asks
  for one of them on a local endpoint without a mapping, Claude Code asks the server for a Claude
  model, and noctis cannot change that request.
- A local model's skill at following the queue's instructions and calling tools is its own; noctis
  gives it the same instructions as Claude.

## Tests

8.8.0 adds 14 Go tests, in `backend_test.go`, and one more on Windows:

- A session on Ollama, LM Studio, llama.cpp, a gateway or Bedrock is neither paused nor warned with
  the plan's 5-hour window at 95%, while the same session on Anthropic's API is paused; an
  `ANTHROPIC_BASE_URL` on `api.anthropic.com` and `_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL` keep the
  limits; outside a session `settings.json` → `env` wins over the shell, as in Claude Code.
- The doctor and the session-start check leave the sign-in and the thresholds out on another
  endpoint, say the limit guard is off, and name the context window of a model Claude Code does not
  know, or ask for one; `noctis status` leaves the plan's rows out there.
- A runner that systemd or launchd starts, and the window launcher of a relaunch, resume the session
  with its endpoint, token, model and window, with the token on no command line and in no plist; on
  Windows the PowerShell launcher hands the endpoint to the `claude` it starts.
- Setup on a local endpoint writes no model or effort and puts the person's own back;
  `--context-window` writes, moves, refuses a value out of range and takes back the window, and
  uninstall restores the one from before; the attribution line is left out on a custom endpoint only,
  a value of the person's own stays, and an endpoint only the shell names changes none of the
  account's settings.
- A subagent is pinned to `haiku` on Anthropic's API, on Bedrock and once `haiku` is mapped, and not
  on an unmapped local endpoint; a local model's own name stays pinned. The research agent's file
  says `model: inherit` where `settings.json` names an endpoint that does not serve its model.

The lab checks that a status line on a local model says *no plan limits* while another session's
plan data is on file. The test main and the lab's harness now drop the endpoint variables of the
session they run in, so running the suites from a session on a local model gives the same results.

The results of the local rounds on this release are in its commit message.
