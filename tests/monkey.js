#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const os = require('os');
const { spawnSync } = require('child_process');
const {
  PLUGIN_NAME, SOURCE_ROOT, IS_WINDOWS, Lab, sleep, nowSec, readJson, writeJson, waitKey, isAlive, MODEL_WINDOWS, THRASHING, OTHER_REQUEST_ERRORS, QUIET_FAILURES, WINDOW_LIMIT, claudeCompactionPoint,
} = require('./harness');
const { checkContinuations, pauseEndedWithoutReason } = require('./continuations');

const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const index = args.indexOf(`--${name}`);
  return index >= 0 && args[index + 1] ? args[index + 1] : fallback;
};
const SEED = Number(flag('seed', '7'));
const ROUNDS = Number(flag('rounds', '400'));
const VERBOSE = args.includes('--verbose');
const HANDOFF_GRACE_SECONDS = 60;
const LAUNCH_OVERLAP_MS = 5000;

let rngState = SEED >>> 0;
function random() {
  rngState |= 0;
  rngState = (rngState + 0x6d2b79f5) | 0;
  let t = Math.imul(rngState ^ (rngState >>> 15), 1 | rngState);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
}
const pick = (list) => list[Math.floor(random() * list.length)];
const chance = (probability) => random() < probability;
const between = (low, high) => Math.floor(low + random() * (high - low + 1));

const problems = [];
const counts = {};
function note(kind) {
  counts[kind] = (counts[kind] || 0) + 1;
}
function problem(round, action, detail) {
  problems.push(`round ${round} [${action}] ${detail}`);
}

const lab = new Lab('noctis-monkey');
const acc = lab.account('accountA');

const SESSIONS = ['m1', 'm2', 'm3', 'weird id with spaces', '../escape', 'çok-uzun-türkçe-oturum-kimliği', ''];
const MODELS = ['claude-fable-5-1', 'claude-opus-5', 'claude-haiku-4-5-20251001', 'claude-sonnet-5', '', 'something-unknown'];
const EVENTS = ['UserPromptSubmit', 'PostToolBatch', 'Stop', 'SessionStart', 'SessionEnd', 'PreToolUse', 'StopFailure', 'Notification', 'PostModelSwitch', 'TaskCreated', 'TaskCompleted', 'PreCompact', 'NotAnEvent'];
const PROMPTS = [
  'fix the typo',
  'ne yapıyorsun',
  'Audit every route handler under src/routes and fix what you find',
  '- [ ] one\n- [ ] two\n- [ ] three',
  'Add input validation to the signup form and reject empty emails. Then write unit tests for the payments module covering refunds. After that update the README for the new CLI flags. Finally deploy the service to staging and check the health endpoint.',
  'Could you explain how the retry logic works, why it backs off, and whether the webhook shares the transaction?',
  '🙂🙂🙂',
  'a'.repeat(9000),
  '"; rm -rf /; echo "',
  '{"not":"a prompt"}',
  '\u0000\u0001 control chars',
  'ultracode: migrate every component to TypeScript',
];
const QUEUE_CONTENTS = [
  '# tasks\n- [ ] first item to do\n- [ ] second item to do\n',
  '- [x] done\n- [x] also done\n',
  '',
  '- [ ] (P0) urgent #tag\n- [ ] (P9) later (after #tag)\n- [ ] (after 99) impossible\n',
  'not a list at all, just prose about the project\n',
  `- [ ] ${'x'.repeat(5000)}\n`,
  Array.from({ length: 300 }, (_, i) => `- [ ] item ${i}`).join('\n'),
  '\u0000binary\u0001garbage\u0002',
];

// Sessions whose context the monkey counts in tokens, as Claude Code hands it to the status line, in the
// window of a model whose window it knows: the context grows with each request, starts over when Claude
// Code compacts it at the point settings.json sets, and fills up where Claude Code does not.
const CONTEXT_SESSIONS = ['m1', 'm2', 'm3'];
const contexts = {};

function runEngine(argv, input, extraEnv = {}) {
  const [binary, prefix] = acc.engine();
  const result = spawnSync(binary, [...prefix, ...argv], {
    encoding: 'utf8',
    input: input === undefined ? undefined : typeof input === 'string' ? input : JSON.stringify(input),
    env: acc.env(extraEnv),
    timeout: 25000,
  });
  acc.settleRefresh(argv);
  return result;
}

function clock() {
  return nowSec() + acc.timeOffset;
}

function currentState() {
  try {
    return acc.state();
  } catch (err) {
    return null;
  }
}

function sleepingInsideAHook(since, sid) {
  const state = currentState();
  const waits = Object.entries((state && state.waits) || {});
  const key = sid ? waitKey(sid) : '';
  const relevant = key ? waits.filter(([name]) => name === key) : waits;
  if (relevant.some(([, wait]) => wait && (wait.inHook || wait.waking))) return true;
  const journal = path.join(acc.guardDir, 'decisions.jsonl');
  if (!fs.existsSync(journal)) return false;
  const lines = fs.readFileSync(journal, 'utf8').split('\n').filter(Boolean).slice(-80);
  return lines.some((line) => {
    try {
      const entry = JSON.parse(line);
      return entry.inHook === true && Number(entry.at || 0) >= since - 5 && (!key || entry.sid === key);
    } catch {
      return false;
    }
  });
}

function checkResult(round, action, result, { allowExit = [0], since = 0, sid = '' } = {}) {
  if (result.error && result.error.code === 'ETIMEDOUT') {
    if (sleepingInsideAHook(since, sid)) { note('hook-waiting'); return; }
    problem(round, action, 'timed out with no in-hook wait to explain it');
    return;
  }
  if (result.error) {
    problem(round, action, `spawn error: ${result.error.message}`);
    return;
  }
  if (!allowExit.includes(result.status)) {
    problem(round, action, `exit ${result.status}: ${(result.stderr || '').trim().slice(0, 200)}`);
  }
  const out = (result.stdout || '').trim();
  if (out.startsWith('{') || out.startsWith('[')) {
    try {
      JSON.parse(out);
    } catch (err) {
      problem(round, action, `stdout is not valid JSON: ${out.slice(0, 200)}`);
    }
  }
  if (/panic:|goroutine \d+ \[running\]/.test(`${result.stdout}${result.stderr}`)) {
    problem(round, action, 'engine panicked');
  }
  if (out.includes('"decision":"block"')) {
    try {
      const parsed = JSON.parse(out);
      const reason = parsed.reason || (parsed.hookSpecificOutput || {}).additionalContext || '';
      if (!reason || reason.length < 10) problem(round, action, 'blocked without a readable reason');
    } catch {
    }
  }
}

function readText(file) {
  try {
    return fs.readFileSync(file, 'utf8');
  } catch {
    return '';
  }
}

function sizeOf(file) {
  try {
    return fs.statSync(file).size;
  } catch {
    return 0;
  }
}

// What was written to file from a byte offset on: all of it once it was rotated and started over.
function textSince(file, offset) {
  try {
    const whole = fs.readFileSync(file);
    return whole.subarray(whole.length >= offset ? offset : 0).toString('utf8');
  } catch {
    return '';
  }
}

function hookOutput(result) {
  try {
    return JSON.parse((result.stdout || '').trim() || '{}') || {};
  } catch {
    return {};
  }
}

const claudeSettingsFile = () => path.join(acc.dir, 'settings.json');
const guardLog = () => path.join(acc.guardDir, 'guard.log');

function contextOf(sid) {
  if (!contexts[sid]) {
    const model = pick(Object.keys(MODEL_WINDOWS));
    contexts[sid] = { model, window: MODEL_WINDOWS[model], tokens: between(15000, 45000) };
  }
  return contexts[sid];
}

// The status line shows how far a session's context is to where Claude Code compacts it, by the
// settings.json the status line ran with, or how much of the window it fills while Claude Code compacts
// nothing by itself.
function checkContextShare(round, sid, session, line, settingsText) {
  let settings;
  try {
    settings = JSON.parse(settingsText || '{}') || {};
  } catch {
    return;
  }
  const point = claudeCompactionPoint(settings, session.model, session.window);
  const expected = point === null ? Math.min(100, Math.round((100 * session.tokens) / session.window)) : Math.round((100 * session.tokens) / point);
  const shown = /ctx %(\d+)/.exec(line || '');
  note('context-check');
  if (!shown || Number(shown[1]) !== expected) {
    problem(round, 'context', `the status line shows ${shown ? `ctx %${shown[1]}` : 'no context'} for ${session.tokens} tokens of ${sid}'s ${session.window} window where Claude Code compacts at ${point === null ? 'no point' : point}: ctx %${expected} expected`);
  }
}

// Claude Code compacts a session whose context reached the point: PreCompact, then SessionStart with
// source compact, and the context starts over from the summary. Neither hook holds the compaction up or
// stops the session.
function compactSession(round, sid, session) {
  const since = clock();
  const pre = runEngine(['hook'], { hook_event_name: 'PreCompact', session_id: sid, cwd: lab.projectDir, transcript_path: lab.transcript, trigger: 'auto', custom_instructions: '' });
  checkResult(round, 'hook PreCompact', pre, { since, sid });
  const held = hookOutput(pre);
  if (held.decision === 'block' || held.continue === false) problem(round, 'hook PreCompact', `held up the compaction of ${sid}: ${(pre.stdout || '').trim().slice(0, 160)}`);
  const start = runEngine(['hook'], { hook_event_name: 'SessionStart', source: 'compact', session_id: sid, cwd: lab.projectDir, transcript_path: lab.transcript, model: session.model });
  checkResult(round, 'hook SessionStart compact', start, { since, sid });
  if (hookOutput(start).continue === false) problem(round, 'hook SessionStart compact', `stopped ${sid} after its compaction`);
  session.tokens = between(15000, 45000);
  note('compaction');
}

function waitOf(sid) {
  return ((currentState() || {}).waits || {})[waitKey(sid)];
}

function contextFullStop(round, sid, session, kind, cloud) {
  const action = `context-full ${kind}${cloud ? ' cloud' : ''}`;
  const input = { hook_event_name: 'StopFailure', session_id: sid, cwd: lab.projectDir, transcript_path: lab.transcript, error: 'invalid_request' };
  if (kind === 'thrashing') input.last_assistant_message = THRASHING;
  else if (kind === 'windowLimit') Object.assign(input, { error: 'max_output_tokens', last_assistant_message: WINDOW_LIMIT });
  else Object.assign(input, { last_assistant_message: 'Prompt is too long', error_details: `prompt is too long: ${session.tokens} tokens > ${session.window} maximum` });
  const mark = acc.journalMark();
  const logFrom = sizeOf(guardLog());
  const since = clock();
  const result = runEngine(['hook'], input, cloud ? { CLAUDE_CODE_REMOTE: 'true' } : {});
  checkResult(round, action, result, { since, sid });
  note(`context-full:${kind}`);
  if (result.error || result.status !== 0) return;
  const after = waitOf(sid);
  const waits = Boolean(after && after.contextFull === true && Number(after.storedBy) === result.pid);
  const rows = acc.journalSince(mark).filter((row) => row.sid === waitKey(sid) && row.event === 'StopFailure');
  const notified = /notify: /.test(textSince(guardLog(), logFrom));
  if (cloud) {
    if (waits) problem(round, action, `a cloud session ${sid} that stopped with its context full was set to wait`);
    if (!rows.some((row) => row.action === 'context-full' && row.cloud) || !notified) problem(round, action, `a cloud session ${sid} that stopped with its context full was left without a notice: ${JSON.stringify(rows.map((row) => row.action))}`);
  } else if (rows.some((row) => row.action === 'retry-giveup' && row.contextFull)) {
    if (waits) problem(round, action, `${sid} was given up on after its context kept filling up, yet set to wait`);
    if (!notified) problem(round, action, `${sid} was given up on after its context kept filling up without a notification`);
    if (rows.some((row) => row.action === 'retry-giveup' && row.observe)) problem(round, action, `observe mode, which starts no fresh session, gave ${sid} up after its fresh starts`);
    note('context-full:giveup');
  } else if (rows.some((row) => row.action === 'would-schedule-resume')) {
    if (waits) problem(round, action, `observe mode set ${sid} to wait after its context filled up`);
  } else if (rows.some((row) => row.action === 'schedule-resume')) {
    if (!waits) problem(round, action, `${sid} stopped with its context full and was journaled to start afresh, yet no wait holds it: ${JSON.stringify(after || null).slice(0, 200)}`);
    else if (!after.scheduled) problem(round, action, `${sid} stopped with its context full and waits with nothing scheduled`);
    if (!notified) problem(round, action, `${sid} stopped with its context full and was set to start afresh without a notice`);
    note('context-full:stopped');
  } else if (!hookOutput(result).systemMessage) {
    problem(round, action, `${sid} stopped with its context full and was neither set to start afresh, given up on nor left with a notice: ${JSON.stringify(rows.map((row) => row.action))}`);
  }
}

const ACCOUNT_FAILURES = ['verification_required', 'oauth_org_not_allowed'];

const READY_TAIL = '; iş devam ettiriliyor.';

function notifiedBy(pid, logFrom, command = 'hook', text = '') {
  return textSince(guardLog(), logFrom).split('\n').some((line) => line.includes(`[INFO ${pid} ${command}] notify: `) && line.includes(text));
}

function stopFailureActions(mark, sid) {
  return acc.journalSince(mark).filter((row) => row.sid === waitKey(sid) && row.event === 'StopFailure').map((row) => row.action);
}

function quietFailureStop(round, sid, error, message, cloud) {
  const action = `quiet failure ${error}${cloud ? ' cloud' : ''}`;
  const mark = acc.journalMark();
  const logFrom = sizeOf(guardLog());
  const since = clock();
  const result = runEngine(['hook'], { hook_event_name: 'StopFailure', session_id: sid, cwd: lab.projectDir, transcript_path: lab.transcript, error, last_assistant_message: message }, cloud ? { CLAUDE_CODE_REMOTE: 'true' } : {});
  checkResult(round, action, result, { since, sid });
  note(`quiet-failure:${error}`);
  if (result.error || result.status !== 0) return;
  const after = waitOf(sid);
  const waits = Boolean(after && Number(after.storedBy) === result.pid);
  const actions = stopFailureActions(mark, sid);
  const notified = notifiedBy(result.pid, logFrom);
  if (actions.includes('retry-giveup')) {
    if (waits) problem(round, action, `${sid} was given up on after ${error}, yet set to wait`);
    if (!notified) problem(round, action, `${sid} was given up on after ${error} without a notification`);
    note('quiet-failure:giveup');
  } else if (actions.includes('schedule-resume')) {
    if (!waits || !after.scheduled) problem(round, action, `${sid} was set to retry after ${error} with nothing scheduled: ${JSON.stringify(after || null).slice(0, 200)}`);
    if (notified) problem(round, action, `${sid} was set to retry after ${error} with a notification before any give-up`);
    note('quiet-failure:retry');
  } else if (actions.some((name) => name.startsWith('would-'))) {
    if (waits) problem(round, action, `observe mode set ${sid} to wait after ${error}`);
  } else if (!hookOutput(result).systemMessage) {
    problem(round, action, `${sid} stopped on ${error} and was neither set to retry, given up on nor left with a notice: ${JSON.stringify(actions)}`);
  }
}

function accountFailureStop(round, sid, error) {
  const action = `account failure ${error}`;
  const mark = acc.journalMark();
  const logFrom = sizeOf(guardLog());
  const since = clock();
  const result = runEngine(['hook'], { hook_event_name: 'StopFailure', session_id: sid, cwd: lab.projectDir, transcript_path: lab.transcript, error });
  checkResult(round, action, result, { since, sid });
  note(`account-failure:${error}`);
  if (result.error || result.status !== 0) return;
  const after = waitOf(sid);
  if (after && Number(after.storedBy) === result.pid) problem(round, action, `${sid} was set to wait after ${error}, which no retry fixes`);
  const told = stopFailureActions(mark, sid).includes('account-error');
  if (told !== notifiedBy(result.pid, logFrom)) problem(round, action, `${error} for ${sid} was ${told ? 'journaled without a notification' : 'notified without a journal row'}`);
}

// A session that stopped with its context full starts afresh from its handoff note when it can: no
// compaction is near in the fresh session, so none holds it back.
function freshStartHeld(state, sid, before, after) {
  if (!before || before.contextFull !== true || before.freshFailed || !after || after.hit !== 'compaction') return false;
  const workflows = (state.workflows || {})[sid] || [];
  const checkpoint = (state.checkpoints || {})[sid] || {};
  return !(Array.isArray(workflows) && workflows.some((run) => run && !run.agent)) && Boolean(checkpoint.path) && !checkpoint.consumed && fs.existsSync(String(checkpoint.path));
}

const ACTIONS = [
  ['hook', (round) => {
    const event = pick(EVENTS);
    const sid = pick(SESSIONS);
    const input = { hook_event_name: event, session_id: sid, cwd: chance(0.85) ? lab.projectDir : pick(['', '/nope', lab.root]), transcript_path: chance(0.8) ? lab.transcript : '/missing.jsonl' };
    if (event === 'UserPromptSubmit') input.prompt = pick(PROMPTS);
    if (event === 'PreToolUse') {
      input.tool_name = pick(['Agent', 'Task', 'Workflow', 'WebSearch', 'Write', 'Bash']);
      input.tool_input = { name: 'run', script_path: '/tmp/x.js', command: 'ls' };
    }
    if (event === 'Stop') input.stop_hook_active = chance(0.5);
    if (event === 'StopFailure') input.error_message = pick(['rate limit exceeded', 'overloaded_error 529', 'model_not_found', 'weekly limit reached', '']);
    if (event === 'Notification') input.message = pick(['quota_auto_resume_fired', 'quota_auto_resume_stale', 'hello']);
    if (event === 'PostModelSwitch') input.to_model = pick(MODELS);
    if (event === 'PreCompact') input.trigger = pick(['auto', 'manual']);
    if (event.startsWith('Task')) { input.task_id = `t${between(1, 5)}`; input.task_subject = 'thing'; }
    const since = clock();
    checkResult(round, `hook ${event}`, runEngine(['hook'], input), { since, sid });
    note(`hook:${event}`);
  }],
  ['statusline', (round) => {
    const sid = pick(SESSIONS);
    const five = chance(0.15) ? between(90, 100) : between(0, 89);
    const week = chance(0.15) ? between(85, 100) : between(0, 84);
    const input = acc.statuslineInput(sid || 'anon', pick(MODELS), five, clock() + between(-600, 18000), week, clock() + between(-600, 5 * 86400), between(0, 99));
    if (chance(0.1)) delete input.rate_limits;
    if (chance(0.1)) input.rate_limits = { five_hour: { used_percentage: 'lots' } };
    checkResult(round, 'statusline', runEngine(['statusline'], input));
    note('statusline');
  }],
  ['command', (round) => {
    const command = pick([
      ['status'], ['doctor'], ['why', '--last', String(between(1, 20))], ['check'], ['report'], ['report', '--json'],
      ['version'], ['cancel'], ['cancel', pick(SESSIONS)], ['off', String(between(1, 5))], ['on'], ['model'],
      ['checkpoint', '--sid', pick(SESSIONS)], ['queue', 'import'], ['ensure'], ['selftest'], ['nonsense-command'],
    ]);
    const alwaysOk = ['status', 'why', 'report', 'version', 'cancel', 'on', 'off', 'ensure', 'checkpoint'];
    const gates = { check: [0, 10, 11, 20], doctor: [0, 1] };
    const emptySid = command[0] === 'cancel' && command[1] === '';
    const allowExit = emptySid ? [2] : gates[command[0]] || (alwaysOk.includes(command[0]) ? [0] : [0, 1]);
    checkResult(round, `cmd ${command.join(' ')}`, runEngine(command), { allowExit });
    note(`cmd:${command[0]}`);
  }],
  ['queue-file', (round) => {
    const file = path.join(lab.projectDir, pick(['TASKS.md', 'tasks.md', 'docs/TASKS.md']));
    fs.mkdirSync(path.dirname(file), { recursive: true });
    if (chance(0.2)) {
      fs.rmSync(file, { force: true });
    } else {
      fs.writeFileSync(file, pick(QUEUE_CONTENTS));
    }
    note('queue-file');
  }],
  ['config-edit', (round) => {
    const config = readJson(acc.configFile) || {};
    const mutation = pick([
      () => { config.thresholds = { session5h: between(-10, 150), weeklyAll: pick([89, 'nope', null]) }; },
      () => { config.mode = pick(['enforce', 'observe', 'bananas']); },
      () => { config.queue = { ...(config.queue || {}), enabled: chance(0.5), auto: chance(0.5), files: pick([['TASKS.md'], [], 'not-an-array']) }; },
      () => { config.wait = { ...(config.wait || {}), maxInHookMinutes: pick([0, 1, 330, -5]), earlyResetPollMinutes: pick([0, 0.05, 5]) }; },
      () => { config.locale = pick(['auto', 'tr', 'en', 'xx', 42]); },
      () => { config.resume = { ...(config.resume || {}), mode: pick(['window', 'headless', 'none', 'nonsense']), terminal: pick(['auto', 'none']) }; },
      () => { config.roles = pick([{ profile: 'noctis' }, { profile: 'economy' }, 'broken']); },
    ]);
    mutation();
    if (chance(0.08)) {
      fs.writeFileSync(acc.configFile, '{ this is not json');
    } else {
      writeJson(acc.configFile, config);
    }
    note('config-edit');
  }],
  ['state-damage', (round) => {
    const what = pick(['truncate-state', 'corrupt-state', 'delete-state', 'corrupt-usage', 'delete-usage', 'stale-lock', 'delete-guard-dir-file']);
    const state = path.join(acc.guardDir, 'state.json');
    const usage = path.join(acc.guardDir, 'usage.json');
    try {
      if (what === 'truncate-state' && fs.existsSync(state)) fs.writeFileSync(state, fs.readFileSync(state, 'utf8').slice(0, between(1, 80)));
      if (what === 'corrupt-state') fs.writeFileSync(state, '{"waits": ');
      if (what === 'delete-state') fs.rmSync(state, { force: true });
      if (what === 'corrupt-usage') fs.writeFileSync(usage, '<<<not json>>>');
      if (what === 'delete-usage') fs.rmSync(usage, { force: true });
      if (what === 'stale-lock') fs.writeFileSync(path.join(acc.guardDir, pick(['state.lock', 'usage.lock', 'fable.lock'])), pick(['999999', JSON.stringify({ pid: 999999 }), 'garbage']));
      if (what === 'delete-guard-dir-file') fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
    } catch {  }
    note(`damage:${what}`);
  }],
  ['limits', (round) => {
    const now = clock();
    const kind = pick(['calm', 'near', 'over', 'weekly-over', 'scoped-over', 'empty', 'garbage', 'reset-early']);
    const shapes = {
      calm: [{ kind: 'session', percent: between(0, 60), resets_at: new Date((now + 7200) * 1000).toISOString() }],
      near: [{ kind: 'session', percent: between(86, 91), resets_at: new Date((now + 3600) * 1000).toISOString() }],
      over: [{ kind: 'session', percent: between(93, 100), resets_at: new Date((now + 1800) * 1000).toISOString() }],
      'weekly-over': [{ kind: 'weekly_all', percent: between(90, 100), resets_at: new Date((now + 2 * 86400) * 1000).toISOString() }],
      'scoped-over': [{ kind: 'weekly_scoped', percent: between(96, 100), resets_at: new Date((now + 86400) * 1000).toISOString(), scope: { group: 'model', model: { display_name: 'Fable' } } }],
      empty: [],
      garbage: [{ kind: 'session', percent: 'lots', resets_at: 'soon' }],
      'reset-early': [{ kind: 'session', percent: 2, resets_at: new Date((now + 18000) * 1000).toISOString() }],
    };
    lab.setLimits(shapes[kind]);
    fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
    note(`limits:${kind}`);
  }],
  ['parallel-hooks', (round) => {
    const inputs = Array.from({ length: between(2, 6) }, () => ({ hook_event_name: pick(['PostToolBatch', 'UserPromptSubmit', 'Stop']), session_id: pick(SESSIONS), cwd: lab.projectDir, transcript_path: lab.transcript, prompt: pick(PROMPTS) }));
    const since = clock();
    const children = inputs.map((input) => runEngine(['hook'], input));
    children.forEach((result, index) => checkResult(round, `parallel hook ${index}`, result, { since, sid: inputs[index].session_id }));
    note('parallel-hooks');
  }],
  ['resume', (round) => {
    const waiting = Object.keys((currentState() || {}).waits || {});
    const sid = waiting.length && chance(0.6) ? pick(waiting) : pick(SESSIONS);
    const state = currentState() || {};
    const before = sid ? (state.waits || {})[sid] : undefined;
    const mark = acc.journalMark();
    const logFrom = sizeOf(guardLog());
    const startedReal = Date.now();
    const result = runEngine(['resume', '--sid', sid, '--account', acc.dir]);
    checkResult(round, 'resume', result, { allowExit: sid === '' ? [2] : [0, 1] });
    if (before && !result.error) {
      const after = ((currentState() || {}).waits || {})[sid];
      const launched = lab.playRecords().launches.some((launch) => launch.key === sid && (!launch.done || launch.done.real >= startedReal - LAUNCH_OVERLAP_MS));
      const silent = pauseEndedWithoutReason({ key: sid, before, after, launched, journal: acc.journalSince(mark) });
      if (silent) problem(round, 'resume', silent);
      if (freshStartHeld(state, sid, before, after)) problem(round, 'resume', `${sid} stopped with its context full and its fresh start was held for a compaction until ${new Date(Number(after.resumeAt) * 1000).toISOString()}`);
      if (before.quietRetry && notifiedBy(result.pid, logFrom, 'resume', READY_TAIL)) problem(round, 'resume', `${sid} waited for a retry noctis sets quietly, and its runner sent a notification that the limit reset`);
    }
    note('resume');
  }],
  ['context', (round) => {
    const sid = pick(CONTEXT_SESSIONS);
    const session = contextOf(sid);
    const point = claudeCompactionPoint(readJson(claudeSettingsFile()) || {}, session.model, session.window);
    if (point !== null && session.tokens >= point) {
      compactSession(round, sid, session);
    } else if (session.tokens >= session.window - 20000) {
      // Claude Code compacts nothing by itself and the request is too long for the window: the session
      // starts over, afresh or cleared by the person.
      contextFullStop(round, sid, session, 'tooLong', chance(0.1));
      session.tokens = between(15000, 45000);
      return;
    }
    session.tokens = Math.min(session.window, session.tokens + Math.round((between(2, 14) * session.window) / 100));
    const settings = readText(claudeSettingsFile());
    // Now and then the five-hour window is in the band before the shipped pause point, where a compaction
    // that comes near holds a session back.
    const five = chance(0.2) ? between(86, 91) : between(0, 85);
    const input = acc.statuslineInput(sid, session.model, five, clock() + between(600, 18000), between(0, 84), clock() + between(3600, 5 * 86400), { tokens: session.tokens, window: session.window });
    const result = runEngine(['statusline'], input);
    checkResult(round, 'context statusline', result);
    // A status line that changed settings.json may have read where Claude Code compacts before or after.
    if (!result.error && result.status === 0 && readText(claudeSettingsFile()) === settings) checkContextShare(round, sid, session, result.stdout, settings);
    note('context');
  }],
  ['compaction-settings', () => {
    // The person sets where Claude Code compacts, in settings.json: at the window setup writes, at a
    // percent, at a window for one model as /autocompact writes it, or not by itself at all.
    const settings = readJson(claudeSettingsFile());
    if (!settings || typeof settings !== 'object') return;
    const env = { ...(settings.env || {}) };
    pick([
      () => { settings.autoCompactWindow = 313000; },
      () => { delete settings.autoCompactWindow; },
      () => { env.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE = String(pick([55, 70, 85])); },
      () => { delete env.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE; },
      () => {
        const model = pick(Object.keys(MODEL_WINDOWS));
        settings.modelSettings = { ...(settings.modelSettings || {}), [model]: { ...((settings.modelSettings || {})[model] || {}), autoCompactWindow: pick([150000, 250000, 400000, 600000]) } };
      },
      () => { delete settings.modelSettings; },
      () => { env.DISABLE_AUTO_COMPACT = '1'; },
      () => { delete env.DISABLE_AUTO_COMPACT; },
    ])();
    settings.env = env;
    writeJson(claudeSettingsFile(), settings);
    note('compaction-settings');
  }],
  ['context-full', (round) => {
    const sid = pick(CONTEXT_SESSIONS);
    contextFullStop(round, sid, contextOf(sid), pick(['thrashing', 'tooLong', 'windowLimit']), chance(0.15));
  }],
  ['other-request-error', (round) => {
    // The API turned a request down for another reason: that is the session's to deal with, and noctis
    // neither pauses it nor acts on it.
    const sid = pick(CONTEXT_SESSIONS);
    const message = pick(OTHER_REQUEST_ERRORS);
    const mark = acc.journalMark();
    const since = clock();
    const input = { hook_event_name: 'StopFailure', session_id: sid, cwd: lab.projectDir, transcript_path: lab.transcript, error: 'invalid_request', error_details: message, last_assistant_message: `API Error: 400 ${JSON.stringify({ type: 'error', error: { type: 'invalid_request_error', message } })}` };
    const result = runEngine(['hook'], input, chance(0.15) ? { CLAUDE_CODE_REMOTE: 'true' } : {});
    checkResult(round, 'other request error', result, { since, sid });
    const after = waitOf(sid);
    const acted = acc.journalSince(mark).filter((row) => row.sid === sid && row.event === 'StopFailure');
    if (after && Number(after.storedBy) === result.pid) problem(round, 'other request error', `${sid} was set to wait after a request the API turned down: ${message}`);
    if (acted.length) problem(round, 'other request error', `a request the API turned down was acted on (${acted.map((row) => row.action).join(', ')}): ${message}`);
    note('other-request-error');
  }],
  ['quiet-failure', (round) => {
    const [error, message] = pick(QUIET_FAILURES);
    quietFailureStop(round, pick(CONTEXT_SESSIONS), error, message, chance(0.15));
  }],
  ['account-failure', (round) => {
    accountFailureStop(round, pick(CONTEXT_SESSIONS), pick(ACCOUNT_FAILURES));
  }],
  ['time-passes', () => {
    lab.settleLateAnswers();
    acc.timeOffset += pick([between(60, 600), between(600, 3 * 3600), between(3 * 3600, 6 * 3600)]);
    note('time-passes');
  }],
  ['session-answers', () => {
    if (fs.existsSync(lab.transcript)) lab.writeAnswer(lab.transcript, acc.timeOffset);
    note('session-answers');
  }],
];

function invariants(round) {
  const checkStartedAt = clock();
  runEngine(['on']); 
  const recovery = runEngine(['hook'], { hook_event_name: 'PostToolBatch', session_id: 'invariant', cwd: lab.projectDir, transcript_path: lab.transcript });
  checkResult(round, 'recovery hook', recovery);
  runEngine(['statusline'], acc.statuslineInput('invariant', 'claude-fable-5-1', 20, clock() + 7200, 20, clock() + 3 * 86400));
  const state = currentState();
  if (state === null) {
    problem(round, 'state', 'state.json is still unreadable after a recovery hook');
    return;
  }
  for (const [sid, wait] of Object.entries(state.waits || {})) {
    if (typeof wait !== 'object' || wait === null) { problem(round, 'state', `wait ${sid} is not an object`); continue; }
    if (!wait.resumeAt || Number.isNaN(Number(wait.resumeAt))) problem(round, 'state', `wait ${sid} has no usable resumeAt`);
    const handoff = (state.handedOff || {})[sid];
    const heldByHandOff = Boolean(handoff && typeof handoff === 'object'
      && (isAlive(Number(handoff.pid)) || checkStartedAt - Number(handoff.at) < HANDOFF_GRACE_SECONDS));
    if (!wait.scheduled && !wait.inHook && !heldByHandOff) problem(round, 'state', `wait ${sid} has no resume path (no scheduler, not in a hook, no hand-off holding it)`);
  }
  for (const file of ['state.json', 'usage.json']) {
    const full = path.join(acc.guardDir, file);
    if (fs.existsSync(full)) {
      try { JSON.parse(fs.readFileSync(full, 'utf8')); } catch { problem(round, 'files', `${file} is still not valid JSON after a recovery hook`); }
    }
  }
  const leftovers = fs.existsSync(acc.guardDir) ? fs.readdirSync(acc.guardDir).filter((name) => name.endsWith('.tmp')) : [];
  if (leftovers.length) problem(round, 'files', `temp files left behind: ${leftovers.join(', ')}`);
  const usage = readJson(path.join(acc.guardDir, 'usage.json')) || {};
  const fable = readJson(path.join(acc.guardDir, 'fable.json')) || {};
  for (const [file, data] of [['usage.json', usage], ['fable.json', fable]]) {
    for (const key of ['five_hour', 'seven_day', 'fable']) {
      const used = (data[key] || {}).used;
      if (used === undefined || used === null) continue;
      if (typeof used !== 'number' || Number.isNaN(used) || used < 0 || used > 100) {
        problem(round, 'usage', `${file} ${key}.used is ${JSON.stringify(used)}`);
      }
      const resetsAt = (data[key] || {}).resetsAt;
      if (resetsAt !== undefined && (typeof resetsAt !== 'number' || Number.isNaN(resetsAt))) {
        problem(round, 'usage', `${file} ${key}.resetsAt is ${JSON.stringify(resetsAt)}`);
      }
    }
  }
  for (const [sid, record] of Object.entries(state.launched || {})) {
    if (!record || typeof record !== 'object') { problem(round, 'state', `launched ${sid} is not an object`); continue; }
    if (Number(record.pid) === process.pid) problem(round, 'state', `launched ${sid} points at the test runner`);
  }
}

async function main() {
  await lab.startMock();
  acc.install();
  acc.fastClaude = true;
  lab.playClaude({ seed: SEED });
  process.stdout.write(`monkey: seed ${SEED}, ${ROUNDS} rounds\n`);
  const started = Date.now();
  for (let round = 1; round <= ROUNDS; round += 1) {
    const [name, action] = pick(ACTIONS);
    try {
      action(round);
    } catch (err) {
      problem(round, name, `harness threw: ${err.message}`);
    }
    if (round % 10 === 0) invariants(round);
    if (VERBOSE) process.stdout.write(`  ${round} ${name}\n`);
    if (round % 50 === 0) process.stdout.write(`  ${round}/${ROUNDS} rounds, ${problems.length} problem(s)\n`);
  }
  invariants(ROUNDS);
  const config = readJson(acc.configFile);
  if (!config || typeof config !== 'object') writeJson(acc.configFile, {});
  acc.setConfig((current) => {
    current.mode = 'enforce';
    current.thresholds = { ...readJson(path.join(SOURCE_ROOT, 'config.default.json')).thresholds };
    current.wait = { ...(current.wait || {}), maxInHookMinutes: 330 };
  });
  lab.setLimits([{ kind: 'session', percent: 30, resets_at: new Date((clock() + 7200) * 1000).toISOString() }]);
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  const recovery = runEngine(['doctor']);
  if (![0, 1].includes(recovery.status)) problems.push(`recovery: doctor exits ${recovery.status}`);
  if (!/OK {2}engine:/.test(recovery.stdout || '')) problems.push('recovery: doctor prints no report after the monkey');
  const statusline = runEngine(['statusline'], acc.statuslineInput('recover', 'claude-fable-5-1', 30, clock() + 7200, 20, clock() + 3 * 86400));
  if (!/∞|NOCTIS/.test(statusline.stdout || '')) problems.push('recovery: status line does not render after the monkey');
  const errors = fs.existsSync(path.join(acc.guardDir, 'errors.log')) ? fs.readFileSync(path.join(acc.guardDir, 'errors.log'), 'utf8') : '';
  const panics = errors.split('\n').filter((line) => /panic|goroutine/.test(line));
  if (panics.length) problems.push(`errors.log records ${panics.length} panic line(s): ${panics[0].slice(0, 160)}`);
  const collected = acc.stopRunners();
  lab.stopMock();
  const unsettled = await lab.settlePlayers();
  const continuations = checkContinuations(lab.playRecords());
  for (const line of continuations.problems) problems.push(`continuation: ${line}`);
  const seconds = ((Date.now() - started) / 1000).toFixed(0);
  process.stdout.write(`\naction mix: ${Object.entries(counts).sort((a, b) => b[1] - a[1]).map(([k, v]) => `${k}=${v}`).join(' ')}\n`);
  process.stdout.write(`${ROUNDS} rounds in ${seconds}s, ${collected} background process(es) collected\n`);
  process.stdout.write(`continuations: ${JSON.stringify({ ...continuations.summary, unsettledPlayers: unsettled })}\n`);
  if (problems.length) {
    process.stdout.write(`\nMONKEY FOUND ${problems.length} PROBLEM(S)\n`);
    for (const line of problems.slice(0, 40)) process.stdout.write(`  ${line}\n`);
    process.stdout.write(`lab dir: ${lab.root}\n`);
    process.exit(1);
  }
  process.stdout.write(`MONKEY PASSED (lab dir ${lab.root})\n`);
}

main().catch((err) => {
  process.stdout.write(`monkey crashed: ${err.stack}\n`);
  lab.stopMock();
  process.exit(1);
});
