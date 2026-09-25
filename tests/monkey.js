#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const os = require('os');
const { spawnSync } = require('child_process');
const { PLUGIN_NAME, SOURCE_ROOT, IS_WINDOWS, Lab, sleep, nowSec, readJson, writeJson, waitKey } = require('./harness');

const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const index = args.indexOf(`--${name}`);
  return index >= 0 && args[index + 1] ? args[index + 1] : fallback;
};
const SEED = Number(flag('seed', '7'));
const ROUNDS = Number(flag('rounds', '400'));
const VERBOSE = args.includes('--verbose');

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
const EVENTS = ['UserPromptSubmit', 'PostToolBatch', 'Stop', 'SessionStart', 'SessionEnd', 'PreToolUse', 'StopFailure', 'Notification', 'PostModelSwitch', 'TaskCreated', 'TaskCompleted', 'NotAnEvent'];
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

function runEngine(argv, input, extraEnv = {}) {
  const [binary, prefix] = acc.engine();
  const result = spawnSync(binary, [...prefix, ...argv], {
    encoding: 'utf8',
    input: input === undefined ? undefined : typeof input === 'string' ? input : JSON.stringify(input),
    env: acc.env(extraEnv),
    timeout: 25000,
  });
  return result;
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
    if (event.startsWith('Task')) { input.task_id = `t${between(1, 5)}`; input.task_subject = 'thing'; }
    const since = nowSec();
    checkResult(round, `hook ${event}`, runEngine(['hook'], input), { since, sid });
    note(`hook:${event}`);
  }],
  ['statusline', (round) => {
    const sid = pick(SESSIONS);
    const five = chance(0.15) ? between(90, 100) : between(0, 89);
    const week = chance(0.15) ? between(85, 100) : between(0, 84);
    const input = acc.statuslineInput(sid || 'anon', pick(MODELS), five, nowSec() + between(-600, 18000), week, nowSec() + between(-600, 5 * 86400), between(0, 99));
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
    const allowExit = gates[command[0]] || (alwaysOk.includes(command[0]) ? [0] : [0, 1]);
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
    const now = nowSec();
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
    const since = nowSec();
    const children = inputs.map((input) => runEngine(['hook'], input));
    children.forEach((result, index) => checkResult(round, `parallel hook ${index}`, result, { since, sid: inputs[index].session_id }));
    note('parallel-hooks');
  }],
  ['resume', (round) => {
    const sid = pick(SESSIONS);
    checkResult(round, 'resume', runEngine(['resume', '--sid', sid, '--account', acc.dir]), { allowExit: sid === '' ? [2] : [0, 1] });
    note('resume');
  }],
];

function invariants(round) {
  runEngine(['on']); 
  const recovery = runEngine(['hook'], { hook_event_name: 'PostToolBatch', session_id: 'invariant', cwd: lab.projectDir, transcript_path: lab.transcript });
  checkResult(round, 'recovery hook', recovery);
  runEngine(['statusline'], acc.statuslineInput('invariant', 'claude-fable-5-1', 20, nowSec() + 7200, 20, nowSec() + 3 * 86400));
  const state = currentState();
  if (state === null) {
    problem(round, 'state', 'state.json is still unreadable after a recovery hook');
    return;
  }
  for (const [sid, wait] of Object.entries(state.waits || {})) {
    if (typeof wait !== 'object' || wait === null) { problem(round, 'state', `wait ${sid} is not an object`); continue; }
    if (!wait.resumeAt || Number.isNaN(Number(wait.resumeAt))) problem(round, 'state', `wait ${sid} has no usable resumeAt`);
    if (!wait.scheduled && !wait.inHook) problem(round, 'state', `wait ${sid} has no resume path (no scheduler, not in a hook)`);
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
  lab.setLimits([{ kind: 'session', percent: 30, resets_at: new Date((nowSec() + 7200) * 1000).toISOString() }]);
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  const recovery = runEngine(['doctor']);
  if (![0, 1].includes(recovery.status)) problems.push(`recovery: doctor exits ${recovery.status}`);
  if (!/OK {2}engine:/.test(recovery.stdout || '')) problems.push('recovery: doctor prints no report after the monkey');
  const statusline = runEngine(['statusline'], acc.statuslineInput('recover', 'claude-fable-5-1', 30, nowSec() + 7200, 20, nowSec() + 3 * 86400));
  if (!/∞|NOCTIS/.test(statusline.stdout || '')) problems.push('recovery: status line does not render after the monkey');
  const errors = fs.existsSync(path.join(acc.guardDir, 'errors.log')) ? fs.readFileSync(path.join(acc.guardDir, 'errors.log'), 'utf8') : '';
  const panics = errors.split('\n').filter((line) => /panic|goroutine/.test(line));
  if (panics.length) problems.push(`errors.log records ${panics.length} panic line(s): ${panics[0].slice(0, 160)}`);
  const collected = acc.stopRunners();
  lab.stopMock();
  const seconds = ((Date.now() - started) / 1000).toFixed(0);
  process.stdout.write(`\naction mix: ${Object.entries(counts).sort((a, b) => b[1] - a[1]).map(([k, v]) => `${k}=${v}`).join(' ')}\n`);
  process.stdout.write(`${ROUNDS} rounds in ${seconds}s, ${collected} background process(es) collected\n`);
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
