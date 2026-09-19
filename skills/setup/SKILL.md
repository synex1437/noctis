---
name: setup
description: One-time setup of noctis after installing it from a marketplace — asks which model and effort should do which kind of work, places the native engine binary, wires settings.json (statusLine, model, effort) and runs the doctor. Use when the user asks to set up, install, configure or repair noctis, or to change which models it uses.
allowed-tools: Bash
---

Speak the user's language throughout (the language they are writing in right now).

**Step 1 — ask which model does which work** (skip this step if `$ARGUMENTS` already contains `--profile` or a role flag). Ask one question with these choices and wait for the answer:

1. **noctis mode** (recommended for Max plans): code and planning on **Fable 5.1 · max**, research and writing on **Opus 5 · xhigh**, noisy-output digests and file search on **Haiku 4.5 · high**, fallback **Opus 5 · max** when the Fable weekly quota runs out.
2. **balanced**: code and planning on Opus 5 · high, research on Sonnet 5 · high, digests and search on Haiku, fallback Sonnet.
3. **economy**: code on Sonnet 5 · high, research on Haiku · high, planning on Opus 5 · medium, digests and search on Haiku, fallback Haiku.
4. **custom**: ask for each role separately — code, research & writing, planning, noisy-output digests, file search, fallback — as `model` or `model:effort` (models: fable, opus, sonnet, haiku or a full model id; effort: low, medium, high, xhigh, max).

**Step 2 — say in one line what setup is about to change** — `permissions.defaultMode` → `auto` (Claude works without asking; never `bypassPermissions`), the default model → the profile's code model, the status line, and marketplace auto-update on — and mention `--permissions keep`, `--no-model`, `--updates keep`. Then **run the setup** and show its output verbatim:

```bash
"${CLAUDE_PLUGIN_ROOT}/bin/noctis" setup --profile noctis $ARGUMENTS
```

(PowerShell: prefix the command with `& `.) Replace `--profile noctis` with `--profile balanced`, `--profile economy`, or for custom: `--code opus:high --research sonnet:high --planning opus:high --digest haiku:medium --explore haiku --fallback sonnet`. Other arguments pass through: `--preset conservative|balanced|aggressive` (pause thresholds), `--no-model` (leave `settings.json > model` alone), `--config-dir <dir>` (repeat for several accounts), `--permissions keep` (do not touch `permissions.defaultMode`; by default setup sets it to `auto`, or `acceptEdits` where `auto` is unavailable, so unattended work never waits on a permission prompt — never `bypassPermissions`).

**Step 3** — tell the user to run `/reload-plugins` (or restart Claude Code); the roles can be changed any time by running this skill again, and `noctis selftest` (the `bin/noctis` binary is on the Bash tool's PATH while the plugin is enabled) verifies scheduling, notifications and hook speed.
