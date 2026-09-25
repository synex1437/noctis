---
name: pause
description: Temporarily disable noctis enforcement (limit pauses, routing, queue continuation) for N minutes. Use when the user says pause noctis, disable the guard, stop the plugin for a while.
allowed-tools:
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/noctis" off *)
disable-model-invocation: true
---

Disable the guard for the time the user asked for (default 60 minutes). The request: $ARGUMENTS

Convert the request to whole minutes first (2 hours → 120, half an hour → 30, nothing → 60) and pass only that number; the longest pause is 10080 minutes (one week):

```bash
"${CLAUDE_PLUGIN_ROOT}/bin/noctis" off <minutes>
```

(PowerShell: prefix the command with `& `.)

Report the lines it prints. If it says nothing changed or that noctis is still on, the pause was not set. If it lists waits that still resume on their own, tell the user and ask whether to cancel any of them; each line names its `noctis cancel --sid <id>`, and nothing is cancelled unless the user runs it. Enforcement resumes automatically when the period ends, or earlier with `/noctis:resume`.
