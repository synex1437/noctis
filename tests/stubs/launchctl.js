#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const { spawn, spawnSync } = require('child_process');

const log = process.env.NOCTIS_SCHEDULER_LOG;
if (!log) {
  process.stderr.write('launchctl stub: NOCTIS_SCHEDULER_LOG is not set\n');
  process.exit(2);
}
const argv = process.argv.slice(2);
const record = (entry) => fs.appendFileSync(log, JSON.stringify(entry) + '\n');
const die = (why) => {
  record({ kind: 'reject', why, argv });
  process.stderr.write(why + '\n');
  process.exit(1);
};

const READER = `
import plistlib, sys, json
with open(sys.argv[1], 'rb') as handle:
    data = plistlib.load(handle)
print(json.dumps({
  'label': data.get('Label'),
  'args': data.get('ProgramArguments'),
  'calendar': data.get('StartCalendarInterval'),
  'runAtLoad': data.get('RunAtLoad'),
  'workingDirectory': data.get('WorkingDirectory'),
}))
`;

function readPlist(file) {
  for (const python of ['python3', 'python']) {
    const parsed = spawnSync(python, ['-c', READER, file], { encoding: 'utf8' });
    if (parsed.status === 0) return JSON.parse(parsed.stdout);
  }
  die(`launchctl: this is not a property list launchd would load: ${file}`);
  return null;
}

function fireTime(calendar) {
  const now = new Date();
  const moment = new Date(now.getFullYear(), Number(calendar.Month) - 1, Number(calendar.Day),
    Number(calendar.Hour), Number(calendar.Minute), 0, 0);
  if (moment.getTime() < now.getTime() - 86400000) moment.setFullYear(now.getFullYear() + 1);
  return Math.floor(moment.getTime() / 1000);
}

if (argv[0] === 'bootstrap' || argv[0] === 'load') {
  if (argv[0] === 'bootstrap' && !/^gui\/\d+$/.test(argv[1] || '')) {
    die(`launchctl: not a per-user domain: ${argv[1]}`);
  }
  const plistPath = argv[argv.length - 1];
  if (!plistPath || !fs.existsSync(plistPath)) die(`launchctl: no such plist: ${plistPath}`);
  const plist = readPlist(plistPath);
  if (!plist.label) die('launchctl: the plist carries no Label');
  if (!Array.isArray(plist.args) || plist.args.length === 0) die('launchctl: the plist carries no ProgramArguments');
  if (!plist.calendar) die('launchctl: the plist carries no StartCalendarInterval');
  if (plist.runAtLoad) die('launchctl: RunAtLoad would fire the job the moment it is loaded');
  const fireAt = fireTime(plist.calendar);
  const priorEntries = fs.readFileSync(log, 'utf8').split('\n').filter(Boolean).length;
  record({
    kind: 'schedule', unit: plist.label, fireAt, command: plist.args,
    calendar: plist.calendar, workingDirectory: plist.workingDirectory,
  });
  const timer = spawn(process.execPath, [path.join(__dirname, 'fire.js')], {
    detached: true,
    stdio: 'ignore',
    env: {
      ...process.env,
      XPC_SERVICE_NAME: plist.label,
      NOCTIS_STUB_UNIT: plist.label,
      NOCTIS_STUB_FIRE_AT: String(fireAt),
      NOCTIS_STUB_COMMAND: JSON.stringify(plist.args),
      NOCTIS_STUB_AFTER: String(priorEntries),
    },
  });
  timer.unref();
  process.exit(0);
}

function stopJob(unit) {
  record({ kind: 'stop', unit });
  const running = fs.readFileSync(log, 'utf8').split('\n').filter(Boolean).map((line) => JSON.parse(line))
    .filter((entry) => entry.kind === 'fire' && entry.unit === unit && entry.pid);
  for (const entry of running) {
    record({ kind: 'terminate', unit, pid: entry.pid });
    try {
      process.kill(-entry.pid, 'SIGTERM');
    } catch (error) {
      record({ kind: 'terminate-failed', unit, pid: entry.pid, why: error.code });
    }
  }
}

function loadedJobs() {
  const jobs = new Map();
  for (const line of fs.readFileSync(log, 'utf8').split('\n').filter(Boolean)) {
    const entry = JSON.parse(line);
    if (entry.kind === 'schedule') jobs.set(entry.unit, null);
    if (entry.kind === 'stop') jobs.delete(entry.unit);
    if (entry.kind === 'fire' && jobs.has(entry.unit)) jobs.set(entry.unit, entry.pid);
  }
  return jobs;
}

const running = (pid) => {
  if (!pid) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
};

if (argv[0] === 'list') {
  const lines = ['PID\tStatus\tLabel'];
  for (const [unit, pid] of loadedJobs()) lines.push(`${running(pid) ? pid : '-'}\t0\t${unit}`);
  process.stdout.write(lines.join('\n') + '\n');
  process.exit(0);
}

if (argv[0] === 'remove') {
  stopJob(String(argv[1] || ''));
  process.exit(0);
}

if (argv[0] === 'bootout') {
  const target = String(argv[1] || '');
  stopJob(target.slice(target.lastIndexOf('/') + 1));
  process.exit(0);
}

if (argv[0] === 'unload') {
  const plistPath = String(argv[argv.length - 1] || '');
  stopJob(path.basename(plistPath, '.plist'));
  process.exit(0);
}

die(`launchctl: unrecognised subcommand ${argv[0] || '(none)'}`);
