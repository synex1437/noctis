#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const { Lab, sleep, readJson } = require('./harness.js');

let checks = 0;
const failures = [];

function check(label, condition, detail) {
  checks += 1;
  if (!condition) failures.push(detail ? `${label}\n      ${detail}` : label);
}

const nowSec = () => Math.floor(Date.now() / 1000);

function limitsAt(sessionPercent, resetIn) {
  const now = nowSec();
  return [
    { kind: 'session', percent: sessionPercent, resets_at: new Date((now + resetIn) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 10, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
}

function installStubs(lab, logFile) {
  const stubDir = path.join(__dirname, 'stubs');
  fs.copyFileSync(path.join(stubDir, 'fire.js'), path.join(lab.binDir, 'fire.js'));
  for (const name of ['systemd-run', 'systemctl']) {
    const file = path.join(lab.binDir, name);
    fs.copyFileSync(path.join(stubDir, `${name}.js`), file);
    fs.chmodSync(file, 0o755);
  }
  fs.writeFileSync(logFile, '');
}

const readLog = (logFile) =>
  fs.readFileSync(logFile, 'utf8').split('\n').filter(Boolean).map((line) => JSON.parse(line));

async function scenarioSystemdFiresAndResumes(lab) {
  const account = lab.account('systemd');
  account.install((config) => { config.wait.maxInHookMinutes = 1; });
  const logFile = path.join(lab.root, 'scheduler-systemd.jsonl');
  installStubs(lab, logFile);
  const extraEnv = { NOCTIS_SCHEDULER_LOG: logFile, NOCTIS_NO_TASKS: '' };

  account.useOwnToken();
  const resetIn = 66; 
  account.setOwnLimits(limitsAt(96, resetIn));
  account.statusline('sysA', 'claude-fable-5-1', 96, nowSec() + resetIn, 10, nowSec() + 3 * 86400);

  const out = account.hook({
    hook_event_name: 'PostToolBatch', session_id: 'sysA',
    cwd: lab.projectDir, transcript_path: account.transcript,
  }, extraEnv);
  check('systemd: the session is stopped at the wall', out.includes('"continue":false'), out.slice(0, 200));

  const wait = (account.state().waits || {}).sysA;
  check('systemd: a wait was recorded', wait !== undefined);
  check('systemd: the plugin chose the systemd backend on its own',
    wait && wait.scheduled && wait.scheduled.method === 'systemd',
    wait && JSON.stringify(wait.scheduled));

  const scheduled = readLog(logFile).filter((e) => e.kind === 'schedule');
  const rejected = readLog(logFile).filter((e) => e.kind === 'reject');
  check('systemd: no argument was rejected by the stub', rejected.length === 0,
    rejected.map((r) => r.why).join('; '));
  check('systemd: exactly one timer was registered', scheduled.length === 1,
    `registered ${scheduled.length}`);

  if (scheduled.length === 1) {
    const entry = scheduled[0];
    check('systemd: the timer is a calendar timer, not a monotonic one',
      /^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$/.test(entry.calendar), entry.calendar);
    check('systemd: accuracy is tightened to a second',
      entry.properties.includes('AccuracySec=1s'), entry.properties.join(','));
    check('systemd: the command is this binary resuming this session',
      entry.command.join(' ').includes('resume') && entry.command.join(' ').includes('sysA'),
      entry.command.join(' '));
    const drift = Math.abs(entry.fireAt - (wait.resumeAt || 0));
    check('systemd: the timer fires when the wait says it should (±2s)', drift <= 2, `drift ${drift}s`);
  }

  const deadline = Date.now() + 150000;
  let fired = false;
  let resumed = false;
  let closed = false;
  while (Date.now() < deadline && !(resumed && closed)) {
    await sleep(500);
    const entries = readLog(logFile);
    fired = fired || entries.some((e) => e.kind === 'fire');
    closed = closed || (account.state().waits || {}).sysA === undefined;
    resumed = resumed || lab.calls().some((line) => /--resume|-p\b|headless/.test(line));
  }
  check('systemd: the timer fired', fired);
  check('systemd: the session was relaunched by the timer, with nobody watching', resumed,
    `calls: ${JSON.stringify(lab.calls().slice(-3))}`);
  check('systemd: the wait was closed once the session came back', closed,
    JSON.stringify((account.state().waits || {}).sysA));

  return account;
}

async function scenarioCancelStopsTheTimer(lab) {
  const account = lab.account('systemd-cancel');
  account.install((config) => { config.wait.maxInHookMinutes = 1; });
  const logFile = path.join(lab.root, 'scheduler-cancel.jsonl');
  installStubs(lab, logFile);
  const extraEnv = { NOCTIS_SCHEDULER_LOG: logFile, NOCTIS_NO_TASKS: '' };

  account.useOwnToken();
  const resetIn = 600; 
  account.setOwnLimits(limitsAt(96, resetIn));
  account.statusline('sysB', 'claude-fable-5-1', 96, nowSec() + resetIn, 10, nowSec() + 3 * 86400);
  account.hook({
    hook_event_name: 'PostToolBatch', session_id: 'sysB',
    cwd: lab.projectDir, transcript_path: account.transcript,
  }, extraEnv);

  const wait = (account.state().waits || {}).sysB;
  check('cancel: a systemd wait exists to cancel',
    wait && wait.scheduled && wait.scheduled.method === 'systemd');
  const unit = wait && wait.scheduled && wait.scheduled.unit;

  account.run(['cancel', '--sid', 'sysB'], undefined, extraEnv);

  const entries = readLog(logFile);
  check('cancel: the unit was stopped by name',
    entries.some((e) => e.kind === 'stop' && e.unit === unit),
    entries.filter((e) => e.kind === 'stop').map((e) => e.unit).join(',') || 'nothing was stopped');
  check('cancel: the wait is gone from state', (account.state().waits || {}).sysB === undefined);

  await sleep(2000);
  check('cancel: the cancelled timer did not fire',
    !readLog(logFile).some((e) => e.kind === 'fire'));
}

function scenarioLaunchdPlistIsValid(lab) {
  const account = lab.account('launchd');
  account.install();
  const at = nowSec() + 3600;
  const out = account.run(['schedule-preview', '--backend', 'launchd', '--sid', 'macA', '--at', String(at)]);
  if (!out || out.includes('unknown command')) {
    check('launchd: the binary can print a plist for inspection', false,
      'schedule-preview is missing; the plist cannot be validated from here');
    return;
  }
  const plistFile = path.join(lab.root, 'preview.plist');
  fs.writeFileSync(plistFile, out);

  const validator = `
import plistlib, sys, json, time
with open(sys.argv[1], 'rb') as fh:
    data = plistlib.load(fh)
print(json.dumps({
  'label': data.get('Label'),
  'args': data.get('ProgramArguments'),
  'calendar': data.get('StartCalendarInterval'),
  'runAtLoad': data.get('RunAtLoad'),
  'workingDirectory': data.get('WorkingDirectory'),
}))
`;
  const parsed = spawnSync('python3', ['-c', validator, plistFile], { encoding: 'utf8' });
  check('launchd: the plist parses with Apple\u2019s own property-list parser',
    parsed.status === 0, (parsed.stderr || '').trim().split('\n').pop());
  if (parsed.status !== 0) return;

  const plist = JSON.parse(parsed.stdout);
  const moment = new Date(at * 1000);
  const expected = new Date(Math.ceil(at / 60) * 60 * 1000);
  const otherLabel = JSON.parse(spawnSync('python3', ['-c', validator, (() => {
    const second = path.join(lab.root, 'preview-2.plist');
    fs.writeFileSync(second, account.run(['schedule-preview', '--backend', 'launchd', '--sid', 'macB', '--at', String(at)]));
    return second;
  })()], { encoding: 'utf8' }).stdout).label;
  check('launchd: the label is a real launchd label', /^com\.[A-Za-z0-9.\-]+$/.test(plist.label || ''), plist.label);
  check('launchd: two sessions get different labels', plist.label !== otherLabel, `${plist.label} vs ${otherLabel}`);
  check('launchd: the job resumes this session',
    (plist.args || []).join(' ').includes('resume'), (plist.args || []).join(' '));
  check('launchd: the calendar names the minute, rounded up, never down',
    plist.calendar && plist.calendar.Minute === expected.getMinutes() && plist.calendar.Hour === expected.getHours(),
    `plist ${JSON.stringify(plist.calendar)} vs requested ${moment.toString()}`);
  check('launchd: the job does not fire the moment it is loaded', plist.runAtLoad === false,
    String(plist.runAtLoad));
  check('launchd: it has a working directory', Boolean(plist.workingDirectory), plist.workingDirectory);
}

async function main() {
  const lab = new Lab('scheduler');
  try {
    await scenarioSystemdFiresAndResumes(lab);
    await scenarioCancelStopsTheTimer(lab);
    scenarioLaunchdPlistIsValid(lab);
  } finally {
    lab.stopMock();
  }

  if (failures.length) {
    console.log(`\nscheduler: ${checks - failures.length}/${checks} checks passed — FAILED:`);
    for (const failure of failures) console.log('  ' + failure);
    console.log(`lab dir: ${lab.root}`);
    process.exit(1);
  }
  console.log(`scheduler: ${checks}/${checks} checks passed (systemd fired for real; launchd plist validated by plistlib)`);
}

main().catch((err) => { console.error(err); process.exit(1); });
