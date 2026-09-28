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
const serviceProperties = [];
const environment = {};
let at = 0;
for (; at < argv.length; at++) {
  const arg = argv[at];
  if (!arg.startsWith('--')) break;
  if (arg === '--user' || arg === '--quiet' || arg === '--collect') continue;
  if (arg.startsWith('--unit=')) { unit = arg.slice('--unit='.length); continue; }
  if (arg.startsWith('--on-calendar=')) { calendar = arg.slice('--on-calendar='.length); continue; }
  if (arg.startsWith('--timer-property=')) { properties.push(arg.slice('--timer-property='.length)); continue; }
  if (arg.startsWith('--property=')) {
    const property = arg.slice('--property='.length);
    if (!/^[A-Za-z]+=\S+$/.test(property)) die('systemd-run: not a Key=Value property: ' + property);
    serviceProperties.push(property);
    continue;
  }
  if (arg.startsWith('--setenv=')) {
    const assignment = arg.slice('--setenv='.length);
    const cut = assignment.indexOf('=');
    if (cut <= 0) die('systemd-run: --setenv needs NAME=VALUE: ' + assignment);
    environment[assignment.slice(0, cut)] = assignment.slice(cut + 1);
    continue;
  }
  die('systemd-run: unrecognised option ' + arg);
}
const command = argv.slice(at);
if (!unit) die('systemd-run: no --unit');
if (!calendar) die('systemd-run: no --on-calendar');
if (command.length === 0) die('systemd-run: no command to run');
if (!/^[A-Za-z0-9:_.\\-]+$/.test(unit)) die('systemd-run: not a valid unit name: ' + unit);

if (!/^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d( UTC)?$/.test(calendar)) {
  die('systemd-run: OnCalendar is not in a form systemd accepts: ' + calendar);
}
// The user manager reads a calendar time without a zone in the system's zone, not in the TZ of
// the process that asked for the timer.
const systemEnv = { ...process.env };
delete systemEnv.TZ;
const resolved = spawnSync('date', ['-d', calendar, '+%s'], { encoding: 'utf8', env: systemEnv });
if (resolved.status !== 0) die('systemd-run: date could not resolve ' + calendar);
const fireAt = Number(resolved.stdout.trim());
if (!Number.isFinite(fireAt) || fireAt <= 0) die('systemd-run: unusable fire time from ' + calendar);

// The manager expands the arguments, not the executable, before it starts the command: an
// argument that is exactly $NAME becomes the variable's value split at whitespace, ${NAME} its
// value and $$ one $.
const managerEnv = { ...process.env, ...environment };
const started = [command[0]];
for (const arg of command.slice(1)) {
  const whole = /^\$([A-Za-z_][A-Za-z0-9_]*)$/.exec(arg);
  if (whole) {
    started.push(...(managerEnv[whole[1]] || '').split(/\s+/).filter(Boolean));
    continue;
  }
  started.push(arg.replace(/\$(\$|\{([^}]+)\})/g, (all, what, name) => (what === '$' ? '$' : managerEnv[name] || '')));
}

const priorEntries = fs.readFileSync(log, 'utf8').split('\n').filter(Boolean).length;
record({
  kind: 'schedule', unit, calendar, fireAt, properties, serviceProperties, environment, command,
  wake: properties.includes('WakeSystem=true'),
});

const timer = spawn(process.execPath, [__dirname + '/fire.js'], {
  detached: true,
  stdio: 'ignore',
  env: {
    ...process.env,
    ...environment,
    NOCTIS_STUB_JOB_ENV: JSON.stringify(environment),
    NOCTIS_STUB_UNIT: unit,
    NOCTIS_STUB_FIRE_AT: String(fireAt),
    NOCTIS_STUB_COMMAND: JSON.stringify(started),
    NOCTIS_STUB_AFTER: String(priorEntries),
  },
});
timer.unref();
