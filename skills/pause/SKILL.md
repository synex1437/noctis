---
name: pause
description: Temporarily disable noctis enforcement (limit pauses, routing, queue continuation) for N minutes. Use when the user says pause noctis, disable the guard, stop the plugin for a while.
allowed-tools: Bash
---

Disable the guard for the requested number of minutes (default 60):

```bash
"${CLAUDE_PLUGIN_ROOT}/bin/noctis" off $ARGUMENTS
```

(PowerShell: prefix the command with `& `.)

Report the line it prints. Enforcement resumes automatically when the period ends, or earlier with `/noctis:resume`.
