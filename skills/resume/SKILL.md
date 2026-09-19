---
name: resume
description: Re-enable noctis enforcement after a pause. Use when the user says resume noctis, enable the guard again, turn the plugin back on.
allowed-tools: Bash
---

Re-enable the guard:

```bash
"${CLAUDE_PLUGIN_ROOT}/bin/noctis" on
```

(PowerShell: prefix the command with `& `.)

Report the line it prints.
