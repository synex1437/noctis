---
name: setup
description: One-time setup of noctis after installing it from a marketplace — asks which model and effort should do which kind of work, places the native engine binary, wires settings.json (statusLine, model, effort, and the function hooks switch lean compaction needs) and runs the doctor. Use when the user asks to set up, install, configure or repair noctis, or to change which models it uses.
allowed-tools:
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/noctis" setup *)
disable-model-invocation: true
---

Speak the user's language throughout (the language they are writing in right now).

**Step 1 — ask which model does which work** (skip this step if `$ARGUMENTS` already contains `--profile` or a role flag). Ask one question with these choices and wait for the answer:

1. **noctis mode** (recommended): code and planning on **Opus 5.5 · max**, research and writing on **Opus 5.5 · xhigh**, noisy-output digests and file search on **Haiku 4.5**. Opus 5.5 beats Fable 5.1 on every coding benchmark Anthropic published with it, and every paid plan includes it.
2. **balanced**: code and planning on Opus 5.5 · high, research on Opus 5.5 · medium, digests and search on Haiku 4.5.
3. **economy**: code and planning on Opus 5.5 · low (the cheapest per solved coding task), research on Sonnet 5 · high, digests and search on Haiku 4.5.
4. **custom**: ask for each role separately — code, research & writing, planning, noisy-output digests, file search, fallback — as `model` or `model:effort` (models: fable, opus, sonnet, haiku or a full model id; effort: low, medium, high, xhigh, max).

**Step 2 — say in one line what setup is about to change** — `permissions.defaultMode` → `auto` on the first setup of this account (Claude works without asking; never `bypassPermissions`; a later setup leaves the mode as it is unless `--permissions` is given), the default model → the profile's code model, the status line, `env.CLAUDE_CODE_ENABLE_FUNCTION_HOOKS` → `1` when it is missing (lean compaction; Claude Code's early-access switch, which loads the hooks module of every installed plugin), and marketplace auto-update on — and mention `--permissions keep`, `--no-model`, `--no-lean`, `--updates keep`. Then **run the setup** and show its output verbatim:

```bash
"${CLAUDE_PLUGIN_ROOT}/bin/noctis" setup --profile noctis <flags>
```

(PowerShell: prefix the command with `& `.) Replace `--profile noctis` with `--profile balanced`, `--profile economy`, or for custom: `--code opus:high --research sonnet:high --planning opus:high --digest haiku:medium --explore haiku --fallback sonnet`. `<flags>` is `$ARGUMENTS` written as flags, because setup refuses any word or option it does not know: a plain word becomes its flag (`economy` → `--profile economy`, a folder → `--config-dir <folder>`), and anything with no flag below is left out and mentioned to the user. The flags: `--preset conservative|balanced|aggressive` (pause thresholds), `--no-model` (leave `settings.json > model` alone), `--no-lean` (turn lean compaction off; a `CLAUDE_CODE_ENABLE_FUNCTION_HOOKS` that setup wrote is taken back, one the user set stays), `--config-dir <dir>` (repeat for several accounts), `--permissions keep` (do not touch `permissions.defaultMode`, on this run or on later ones, and relaunch in the session's own mode: a `resume.permissionMode` of `auto` becomes `inherit`; without `--permissions` the first setup on an account sets it to `auto`, or `acceptEdits` where `auto` is unavailable, so unattended work never waits on a permission prompt, and a later run leaves it as it is — never `bypassPermissions`), `--permissions auto|acceptEdits|default|plan` (set that mode on this run), `--updates keep`. The output names the permission mode from before the first setup and how to go back to it, or says that it left the mode as it was. Exit code 2 means setup changed nothing and named what it did not understand: correct that and run it once more, or ask the user.

**Step 3** — tell the user to run `/reload-plugins` (or restart Claude Code); the roles can be changed any time by running this skill again, and `noctis selftest` (the `bin/noctis` binary is on the Bash tool's PATH while the plugin is enabled) verifies scheduling, notifications and hook speed.
