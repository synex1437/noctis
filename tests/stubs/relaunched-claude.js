#!/usr/bin/env node
'use strict';

const fs = require('fs');
const { spawnSync } = require('child_process');

const argv = process.argv.slice(2);
if (argv[0] === '--help') {
  process.stdout.write('  --permission-mode <mode>  (choices: "acceptEdits", "bypassPermissions", "default", "plan", "auto")\n');
  process.exit(0);
}

const plan = JSON.parse(process.env.NOCTIS_LAB_RELAUNCH);
fs.appendFileSync(process.env.NOCTIS_LAB_CALLS, `FAKE_CLAUDE args=[${argv.join(' ')}] HANDOFF=${process.env.NOCTIS_HANDOFF || ''}\n`);

const now = Math.floor(Date.now() / 1000);
const resetsAt = now + plan.resetIn;
const weekResetsAt = now + 3 * 86400;
const staging = `${plan.limitsFile}.${process.pid}`;
fs.writeFileSync(staging, JSON.stringify([
  { kind: 'session', percent: 96, resets_at: new Date(resetsAt * 1000).toISOString() },
  { kind: 'weekly_all', percent: 10, resets_at: new Date(weekResetsAt * 1000).toISOString() },
]));
fs.renameSync(staging, plan.limitsFile);

const noctis = (args, input) => spawnSync(plan.noctis, args, {
  input: JSON.stringify(input), encoding: 'utf8', env: process.env, timeout: 60000,
});
noctis(['statusline'], {
  session_id: plan.sid, cwd: plan.cwd, transcript_path: plan.transcript, version: '2.1.270',
  model: { id: 'claude-fable-5-1', display_name: 'claude-fable-5-1' },
  context_window: { used_percentage: 32 },
  rate_limits: {
    five_hour: { used_percentage: 96, resets_at: resetsAt },
    seven_day: { used_percentage: 10, resets_at: weekResetsAt },
  },
});
const hook = noctis(['hook'], { hook_event_name: 'PostToolBatch', session_id: plan.sid, cwd: plan.cwd });
fs.writeFileSync(`${plan.out}.${process.pid}`, JSON.stringify({ status: hook.status, signal: hook.signal, stdout: hook.stdout }));
fs.renameSync(`${plan.out}.${process.pid}`, plan.out);
process.exit(0);
