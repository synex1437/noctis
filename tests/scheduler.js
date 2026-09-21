#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const { Lab, sleep, IS_WINDOWS } = require('./harness.js');

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

const started = [];

function newAccount(lab, name) {
  const account = lab.account(name);
  started.push(account);
  return account;
}

function installStubs(lab, logFile, names) {
  const stubDir = path.join(__dirname, 'stubs');
  fs.copyFileSync(path.join(stubDir, 'fire.js'), path.join(lab.binDir, 'fire.js'));
  for (const name of names) {
    const file = path.join(lab.binDir, name);
    fs.copyFileSync(path.join(stubDir, `${name}.js`), file);
    fs.chmodSync(file, 0o755);
  }
  fs.writeFileSync(logFile, '');
}

const readLog = (logFile) =>
  fs.readFileSync(logFile, 'utf8').split('\n').filter(Boolean).map((line) => JSON.parse(line));

function pythonExecutable() {
  for (const candidate of ['python3', 'python']) {
    if (spawnSync(candidate, ['-c', 'import plistlib'], { encoding: 'utf8' }).status === 0) return candidate;
  }
  return null;
}

function parkSession(account, sid, resetIn, extraEnv) {
  account.useOwnToken();
  account.setOwnLimits(limitsAt(96, resetIn));
  account.statusline(sid, 'claude-fable-5-1', 96, nowSec() + resetIn, 10, nowSec() + 3 * 86400);
  return account.hook({
    hook_event_name: 'PostToolBatch', session_id: sid,
    cwd: account.lab.projectDir, transcript_path: account.transcript,
  }, extraEnv);
}

async function scenarioSystemdFiresAndResumes(lab) {
  const account = newAccount(lab, 'systemd');
  account.install((config) => { config.wait.maxInHookMinutes = 1; });
  const logFile = path.join(lab.root, 'scheduler-systemd.jsonl');
  installStubs(lab, logFile, ['systemd-run', 'systemctl']);
  const extraEnv = { NOCTIS_SCHEDULER_LOG: logFile, NOCTIS_NO_TASKS: '' };

  const resetIn = 66;
  const out = parkSession(account, 'sysA', resetIn, extraEnv);
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

  await waitForTheSessionToComeBack(lab, account, 'sysA', logFile, 'systemd');
  return account;
}

async function waitForTheSessionToComeBack(lab, account, sid, logFile, backend) {
  const deadline = Date.now() + 200000;
  let fired = false;
  let resumed = false;
  let closed = false;
  while (Date.now() < deadline && !(resumed && closed)) {
    await sleep(500);
    const entries = readLog(logFile);
    fired = fired || entries.some((e) => e.kind === 'fire');
    closed = closed || (account.state().waits || {})[sid] === undefined;
    resumed = resumed || lab.calls().some((line) => /--resume|-p\b|headless/.test(line));
  }
  check(`${backend}: the timer fired`, fired);
  check(`${backend}: the session was relaunched by the timer, with nobody watching`, resumed,
    `calls: ${JSON.stringify(lab.calls().slice(-3))}`);
  check(`${backend}: the wait was closed once the session came back`, closed,
    JSON.stringify((account.state().waits || {})[sid]));
}

async function scenarioCancelStopsTheTimer(lab) {
  const account = newAccount(lab, 'systemd-cancel');
  account.install((config) => { config.wait.maxInHookMinutes = 1; });
  const logFile = path.join(lab.root, 'scheduler-cancel.jsonl');
  installStubs(lab, logFile, ['systemd-run', 'systemctl']);
  const extraEnv = { NOCTIS_SCHEDULER_LOG: logFile, NOCTIS_NO_TASKS: '' };

  parkSession(account, 'sysB', 600, extraEnv);

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

async function scenarioLaunchdFiresAndResumes(lab) {
  const account = newAccount(lab, 'launchd-fire');
  account.install((config) => { config.wait.maxInHookMinutes = 1; });
  const logFile = path.join(lab.root, 'scheduler-launchd.jsonl');
  installStubs(lab, logFile, ['launchctl']);
  const agents = path.join(lab.root, 'launchd-home');
  const extraEnv = { NOCTIS_SCHEDULER_LOG: logFile, NOCTIS_NO_TASKS: '', HOME: agents };

  const resetIn = 66;
  const out = parkSession(account, 'macA', resetIn, extraEnv);
  check('launchd: the session is stopped at the wall', out.includes('"continue":false'), out.slice(0, 200));

  const wait = (account.state().waits || {}).macA;
  check('launchd: a wait was recorded', wait !== undefined);
  check('launchd: the plugin chose the launchd backend on its own',
    wait && wait.scheduled && wait.scheduled.method === 'launchd',
    wait && JSON.stringify(wait.scheduled));

  const label = wait && wait.scheduled && wait.scheduled.label;
  check('launchd: the agent was written where launchd looks for it',
    Boolean(label) && fs.existsSync(path.join(agents, 'Library', 'LaunchAgents', `${label}.plist`)),
    path.join(agents, 'Library', 'LaunchAgents', `${label}.plist`));

  const rejected = readLog(logFile).filter((e) => e.kind === 'reject');
  check('launchd: launchctl rejected nothing in the plist', rejected.length === 0,
    rejected.map((r) => r.why).join('; '));
  const scheduled = readLog(logFile).filter((e) => e.kind === 'schedule');
  check('launchd: exactly one agent was bootstrapped', scheduled.length === 1, `bootstrapped ${scheduled.length}`);

  if (scheduled.length === 1) {
    const entry = scheduled[0];
    check('launchd: the job resumes this session', entry.command.join(' ').includes('resume')
      && entry.command.join(' ').includes('macA'), entry.command.join(' '));
    check('launchd: it runs from the guard directory', Boolean(entry.workingDirectory), entry.workingDirectory);
    const drift = entry.fireAt - (wait.resumeAt || 0);
    check('launchd: the calendar names the minute of the wait, rounded up, never down',
      drift >= 0 && drift < 60, `fires ${drift}s after the wait says it should`);
  }

  await waitForTheSessionToComeBack(lab, account, 'macA', logFile, 'launchd');
}

async function scenarioCancelStopsTheAgent(lab) {
  const account = newAccount(lab, 'launchd-cancel');
  account.install((config) => { config.wait.maxInHookMinutes = 1; });
  const logFile = path.join(lab.root, 'scheduler-launchd-cancel.jsonl');
  installStubs(lab, logFile, ['launchctl']);
  const agents = path.join(lab.root, 'launchd-cancel-home');
  const extraEnv = { NOCTIS_SCHEDULER_LOG: logFile, NOCTIS_NO_TASKS: '', HOME: agents };

  parkSession(account, 'macB', 600, extraEnv);

  const wait = (account.state().waits || {}).macB;
  check('cancel: a launchd wait exists to cancel',
    wait && wait.scheduled && wait.scheduled.method === 'launchd',
    wait && JSON.stringify(wait.scheduled));
  const label = wait && wait.scheduled && wait.scheduled.label;

  account.run(['cancel', '--sid', 'macB'], undefined, extraEnv);

  const entries = readLog(logFile);
  check('cancel: the agent was booted out by label',
    entries.some((e) => e.kind === 'stop' && e.unit === label),
    entries.filter((e) => e.kind === 'stop').map((e) => e.unit).join(',') || 'nothing was stopped');
  check('cancel: the plist was removed, so a reboot cannot revive it',
    !fs.existsSync(path.join(agents, 'Library', 'LaunchAgents', `${label}.plist`)));
  check('cancel: the wait is gone from state', (account.state().waits || {}).macB === undefined);

  await sleep(2000);
  check('cancel: the cancelled agent did not fire',
    !readLog(logFile).some((e) => e.kind === 'fire'));
}

function scenarioLaunchdPlistIsValid(lab) {
  const python = pythonExecutable();
  const account = newAccount(lab, 'launchd');
  account.install();
  const at = nowSec() + 3600;
  const out = account.run(['schedule-preview', '--backend', 'launchd', '--sid', 'macP', '--at', String(at)]);
  if (!out || out.includes('unknown command')) {
    check('launchd: the binary can print a plist for inspection', false,
      'schedule-preview is missing; the plist cannot be validated from here');
    return;
  }
  check('launchd: a property-list parser is available to check the plist with', python !== null,
    'neither python3 nor python could import plistlib');
  if (!python) return;
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
  const parsed = spawnSync(python, ['-c', validator, plistFile], { encoding: 'utf8' });
  check('launchd: the plist parses with Apple\u2019s own property-list parser',
    parsed.status === 0, (parsed.stderr || '').trim().split('\n').pop());
  if (parsed.status !== 0) return;

  const plist = JSON.parse(parsed.stdout);
  const moment = new Date(at * 1000);
  const expected = new Date(Math.ceil(at / 60) * 60 * 1000);
  const otherLabel = JSON.parse(spawnSync(python, ['-c', validator, (() => {
    const second = path.join(lab.root, 'preview-2.plist');
    fs.writeFileSync(second, account.run(['schedule-preview', '--backend', 'launchd', '--sid', 'macQ', '--at', String(at)]));
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

function lastErrors(account) {
  try {
    const lines = fs.readFileSync(path.join(account.guardDir, 'errors.log'), 'utf8').split('\n').filter(Boolean);
    return lines.slice(-2).join(' | ') || 'errors.log is empty';
  } catch {
    return 'errors.log is missing';
  }
}

function schtasks(args, encoding = 'utf8') {
  return spawnSync('schtasks.exe', args, { encoding: encoding === 'buffer' ? 'buffer' : 'utf8', timeout: 120000 });
}

function taskXml(name) {
  const result = schtasks(['/query', '/tn', name, '/xml', 'ONE'], 'buffer');
  if (result.status !== 0 || !result.stdout) return null;
  const raw = result.stdout;
  return raw.length > 1 && raw[0] === 0xff && raw[1] === 0xfe ? raw.toString('utf16le') : raw.toString('utf8');
}

const unescapeXml = (text) => String(text === undefined ? '' : text)
  .replace(/&quot;/g, '"').replace(/&apos;/g, "'")
  .replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');

const tagValue = (xml, tag) => unescapeXml((new RegExp(`<${tag}>([^<]*)</${tag}>`).exec(xml) || [])[1]);

function deleteTask(name) {
  if (!name) return;
  schtasks(['/delete', '/tn', name, '/f']);
}

async function scenarioWindowsTaskIsRegisteredAndRuns(lab) {
  const account = newAccount(lab, 'task');
  account.install((config) => { config.wait.maxInHookMinutes = 1; });
  const extraEnv = { NOCTIS_NO_TASKS: '' };
  let name = '';
  try {
    const out = parkSession(account, 'winA', 600, extraEnv);
    check('task: the session is stopped at the wall', out.includes('"continue":false'), out.slice(0, 200));

    const wait = (account.state().waits || {}).winA;
    check('task: a wait was recorded', wait !== undefined);
    check('task: the plugin chose the Task Scheduler backend on its own',
      wait && wait.scheduled && wait.scheduled.method === 'task',
      `${wait && JSON.stringify(wait.scheduled)} — ${lastErrors(account)}`);
    name = (wait && wait.scheduled && wait.scheduled.taskName) || '';
    check('task: the wait records the task name Windows knows it by', Boolean(name));
    if (!name) return;

    const xml = taskXml(name);
    check('task: Windows Task Scheduler really holds a task under that name', xml !== null,
      `schtasks /query could not find ${name}`);
    if (!xml) return;

    const boundary = tagValue(xml, 'StartBoundary');
    const fireAt = Math.floor(new Date(boundary).getTime() / 1000);
    const drift = Math.abs(fireAt - Number(wait.resumeAt || 0));
    check('task: the trigger fires when the wait says it should (±5s)', drift <= 5,
      `StartBoundary ${boundary} is ${drift}s from resumeAt ${wait.resumeAt}`);
    check('task: a machine that was asleep still runs it when it wakes',
      /<StartWhenAvailable>true<\/StartWhenAvailable>/.test(xml));
    check('task: a second copy is never started alongside the first',
      /<MultipleInstancesPolicy>IgnoreNew<\/MultipleInstancesPolicy>/.test(xml),
      (/<MultipleInstancesPolicy>([^<]*)</.exec(xml) || [])[1]);
    check('task: the action resumes this session', /\bresume\b/.test(tagValue(xml, 'Arguments'))
      && tagValue(xml, 'Arguments').includes('winA'), tagValue(xml, 'Arguments'));
    check('task: it runs from the guard directory', tagValue(xml, 'WorkingDirectory').length > 0,
      tagValue(xml, 'WorkingDirectory'));

    parkSession(account, 'winA', 1500, extraEnv);
    const again = (account.state().waits || {}).winA;
    const listed = schtasks(['/query', '/fo', 'csv', '/nh']);
    const rows = (listed.stdout || '').split(/\r?\n/).filter((line) => line.includes(name));
    check('task: pausing again replaces the task instead of leaving two behind',
      rows.length === 1, `${rows.length} tasks named ${name}`);

    const replaced = taskXml(name);
    check('task: the replacement is still registered', replaced !== null);
    if (!replaced) return;
    const movedTo = Math.floor(new Date(tagValue(replaced, 'StartBoundary')).getTime() / 1000);
    check('task: the replacement carries the new deadline, not the one it replaced',
      movedTo !== fireAt && Math.abs(movedTo - Number((again && again.resumeAt) || 0)) <= 5,
      `boundary ${tagValue(replaced, 'StartBoundary')} vs resumeAt ${again && again.resumeAt}, first was ${boundary}`);

    const command = tagValue(replaced, 'Command');
    const argumentsText = tagValue(replaced, 'Arguments');
    const workingDirectory = tagValue(replaced, 'WorkingDirectory');

    lab.resetCalls();
    const ran = spawnSync(command, [argumentsText], {
      cwd: workingDirectory, windowsVerbatimArguments: true, encoding: 'utf8',
      env: account.env(extraEnv), timeout: 180000,
    });
    check('task: the command Windows holds runs without an error',
      ran.status === 0, `${ran.status}: ${(ran.stderr || ran.stdout || '').slice(0, 300)}`);

    const deadline = Date.now() + 120000;
    let resumed = false;
    let closed = false;
    while (Date.now() < deadline && !(resumed && closed)) {
      await sleep(500);
      closed = closed || (account.state().waits || {}).winA === undefined;
      resumed = resumed || lab.calls().some((line) => /--resume|-p\b|headless/.test(line));
    }
    check('task: the session was relaunched by the task action, with nobody watching', resumed,
      `calls: ${JSON.stringify(lab.calls().slice(-3))}`);
    check('task: the wait was closed once the session came back', closed,
      JSON.stringify((account.state().waits || {}).winA));
    check('task: a session that came back leaves no task behind', taskXml(name) === null,
      `${name} is still registered`);
  } finally {
    deleteTask(name);
  }
}

async function scenarioCancelRemovesTheTask(lab) {
  const account = newAccount(lab, 'task-cancel');
  account.install((config) => { config.wait.maxInHookMinutes = 1; });
  const extraEnv = { NOCTIS_NO_TASKS: '' };
  let name = '';
  try {
    parkSession(account, 'winB', 600, extraEnv);
    const wait = (account.state().waits || {}).winB;
    check('cancel: a scheduled task exists to cancel',
      wait && wait.scheduled && wait.scheduled.method === 'task',
      wait && JSON.stringify(wait.scheduled));
    name = (wait && wait.scheduled && wait.scheduled.taskName) || '';
    if (!name) return;
    check('cancel: Windows holds the task before the cancel', taskXml(name) !== null);

    account.run(['cancel', '--sid', 'winB'], undefined, extraEnv);

    check('cancel: the task is gone from Windows Task Scheduler', taskXml(name) === null,
      `${name} survived the cancel`);
    check('cancel: the wait is gone from state', (account.state().waits || {}).winB === undefined);
  } finally {
    deleteTask(name);
  }
}

async function main() {
  const lab = new Lab('scheduler');
  try {
    if (IS_WINDOWS) {
      await scenarioWindowsTaskIsRegisteredAndRuns(lab);
      await scenarioCancelRemovesTheTask(lab);
    } else if (process.platform === 'darwin') {
      await scenarioLaunchdFiresAndResumes(lab);
      await scenarioCancelStopsTheAgent(lab);
      scenarioLaunchdPlistIsValid(lab);
    } else {
      await scenarioSystemdFiresAndResumes(lab);
      await scenarioCancelStopsTheTimer(lab);
      scenarioLaunchdPlistIsValid(lab);
    }
  } finally {
    for (const account of started) account.stopRunners();
    lab.stopMock();
  }

  if (failures.length) {
    console.log(`\nscheduler: ${checks - failures.length}/${checks} checks passed — FAILED:`);
    for (const failure of failures) console.log('  ' + failure);
    console.log(`lab dir: ${lab.root}`);
    process.exit(1);
  }
  console.log(`scheduler: ${checks}/${checks} checks passed on ${process.platform} (the backend a real user here would get, exercised end to end)`);
}

main().catch((err) => { console.error(err); process.exit(1); });
