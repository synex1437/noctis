---
name: pause
description: Temporarily disable noctis enforcement (limit pauses, routing, queue continuation) for N minutes. Use when the user says pause noctis, disable the guard, stop the plugin for a while.
allowed-tools: Bash
---

Disable the guard for the time the user asked for (default 60 minutes). The request: $ARGUMENTS

Convert the request to whole minutes first (2 hours → 120, half an hour → 30, nothing → 60) and pass only that number; the longest pause is 10080 minutes (one week):

```bash
"${CLAUDE_PLUGIN_ROOT}/bin/noctis" off <minutes>
```

(PowerShell: prefix the command with `& `.)

Report the line it prints. If it says nothing changed or that noctis is still on, the pause was not set. Enforcement resumes automatically when the period ends, or earlier with `/noctis:resume`.
