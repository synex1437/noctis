#!/usr/bin/env node
'use strict';

const fs = require('fs');
const { spawn, spawnSync } = require('child_process');

const log = process.env.NOCTIS_SCHEDULER_LOG;
if (!log) {
  process.stderr.write('systemd-run stub: NOCTIS_SCHEDULER_LOG is not set\n');
  process.exit(2);
}
const record = (entry) => fs.appendFileSync(log, JSON.stringify(entry) + '\n');
const die = (why) => {
  record({ kind: 'reject', why, argv: process.argv.slice(2) });
  process.stderr.write(why + '\n');
  process.exit(1);
};

const argv = process.argv.slice(2);
let unit = null;
let calendar = null;
const properties = [];
let at = 0;
for (; at < argv.length; at++) {
  const arg = argv[at];
  if (!arg.startsWith('--')) break;
  if (arg === '--user' || arg === '--quiet' || arg === '--collect') continue;
  if (arg.startsWith('--unit=')) { unit = arg.slice('--unit='.length); continue; }
  if (arg.startsWith('--on-calendar=')) { calendar = arg.slice('--on-calendar='.length); continue; }
  if (arg.startsWith('--timer-property=')) { properties.push(arg.slice('--timer-property='.length)); continue; }
  die('systemd-run: unrecognised option ' + arg);
}
const command = argv.slice(at);
if (!unit) die('systemd-run: no --unit');
if (!calendar) die('systemd-run: no --on-calendar');
if (command.length === 0) die('systemd-run: no command to run');
if (!/^[A-Za-z0-9:_.\\-]+$/.test(unit)) die('systemd-run: not a valid unit name: ' + unit);

if (!/^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$/.test(calendar)) {
  die('systemd-run: OnCalendar is not in a form systemd accepts: ' + calendar);
}
const resolved = spawnSync('date', ['-d', calendar, '+%s'], { encoding: 'utf8' });
if (resolved.status !== 0) die('systemd-run: date could not resolve ' + calendar);
const fireAt = Number(resolved.stdout.trim());
if (!Number.isFinite(fireAt) || fireAt <= 0) die('systemd-run: unusable fire time from ' + calendar);

const priorEntries = fs.readFileSync(log, 'utf8').split('\n').filter(Boolean).length;
record({
  kind: 'schedule', unit, calendar, fireAt, properties, command,
  wake: properties.includes('WakeSystem=true'),
});

const timer = spawn(process.execPath, [__dirname + '/fire.js'], {
  detached: true,
  stdio: 'ignore',
  env: {
    ...process.env,
    NOCTIS_STUB_UNIT: unit,
    NOCTIS_STUB_FIRE_AT: String(fireAt),
    NOCTIS_STUB_COMMAND: JSON.stringify(command),
    NOCTIS_STUB_AFTER: String(priorEntries),
  },
});
timer.unref();
