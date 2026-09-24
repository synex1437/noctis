---
name: status
description: Show noctis status — current usage windows, thresholds, model, pending waits, recent errors — and the last decisions it made. Use when the user asks what noctis is doing, why it paused, or for its status.
allowed-tools:
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/noctis" status)
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/noctis" why *)
---

Print the status and the last decisions, verbatim:

```bash
"${CLAUDE_PLUGIN_ROOT}/bin/noctis" status
"${CLAUDE_PLUGIN_ROOT}/bin/noctis" why --last 10
```

(PowerShell: prefix each command with `& `.)

If the user asked *why* something happened (a pause, a model switch, a routed prompt), explain it from the `why` lines: each line is `time  session  hook-event  action  reason [usage facts]`.
