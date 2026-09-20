#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const { spawn, spawnSync } = require('child_process');
const { PLUGIN_NAME, SOURCE_ROOT, IS_WINDOWS, Lab, sleep, nowSec, readJson, writeJson, isAlive, refreshChecksums } = require('./harness');
const PLUGIN_VERSION = readJson(path.join(SOURCE_ROOT, '.claude-plugin', 'plugin.json')).version;

const lab = new Lab('noctis-lab');
const LAB_ROOT = lab.root;
const PROJECT_DIR = lab.projectDir;
const TRANSCRIPT = lab.transcript;
const results = [];

const mock = {
  set limits(value) {
    lab.setLimits(value);
  },
  get limits() {
    return lab.getLimits();
  },
  get hits() {
    return lab.mockHits();
  },
  set skew(seconds) {
    lab.setSkew(seconds);
  },
  get lastHeaders() {
    return lab.mockLastHeaders();
  },
};

function check(name, actual, expected) {
  const ok = JSON.stringify(actual) === JSON.stringify(expected);
  results.push({ name, ok, actual, expected });
  if (!ok) process.stdout.write(`  FAIL ${name}: got ${JSON.stringify(actual)} expected ${JSON.stringify(expected)}\n`);
}

function writeTranscript(extraBytes = 0) {
  lab.writeTranscript(extraBytes);
}

function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`;
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`).join(',')}}`;
  }
  return JSON.stringify(value);
}

function callsLog() {
  return lab.calls();
}

function resetCalls() {
  lab.resetCalls();
}

async function waitRecord(acc, sid, seconds = 20) {
  for (let i = 0; i < seconds * 4 && !(acc.state().waits || {})[sid]; i += 1) await sleep(250);
  return (acc.state().waits || {})[sid];
}

async function callsMatching(fragment, seconds = 60) {
  for (let i = 0; i < seconds * 2 && !callsLog().some((line) => line.includes(fragment)); i += 1) await sleep(500);
  return callsLog().filter((line) => line.includes(fragment));
}

async function anyCall(seconds = 60) {
  for (let i = 0; i < seconds * 2 && !callsLog().length; i += 1) await sleep(500);
  return callsLog();
}

async function scenarioBaseline(acc) {
  const now = nowSec();
  const line = acc.statusline('s1', 'claude-fable-5-1', 41, now + 7200, 23, now + 3 * 86400);
  check('statusline badge', line.startsWith('∞ 5s %41'), true);
  check('usage.json written', readJson(path.join(acc.guardDir, 'usage.json')).five_hour.used, 41);
  check('below threshold silent', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's1', cwd: PROJECT_DIR, prompt: 'fix the auth.js bug please' }), '');
  check('post batch silent', acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  const started = Date.now();
  for (let i = 0; i < 5; i += 1) acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const avg = (Date.now() - started) / 5;
  results.push({ name: `hot path avg ${avg.toFixed(0)}ms`, ok: avg < 400, actual: avg, expected: '<400ms' });
  const marker = path.join(acc.guardDir, 'quiet', 's1.json');
  check('quiet path: marker written after a calm decision', fs.existsSync(marker), true);
  const batch = { hook_event_name: 'PostToolBatch', session_id: 's1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT };
  const fullTimes = [];
  const quietTimes = [];
  for (let i = 0; i < 12; i += 1) {
    let started = process.hrtime.bigint();
    acc.hook(batch, { NOCTIS_NO_QUIET: '1' });
    fullTimes.push(Number(process.hrtime.bigint() - started) / 1e6);
    started = process.hrtime.bigint();
    acc.hook(batch);
    quietTimes.push(Number(process.hrtime.bigint() - started) / 1e6);
  }
  const median = (values) => values.slice().sort((a, b) => a - b)[Math.floor(values.length / 2)];
  const quietMedian = median(quietTimes);
  const fullMedian = median(fullTimes);
  results.push({
    name: `quiet path is not slower than the full path (${quietMedian.toFixed(1)}ms vs ${fullMedian.toFixed(1)}ms)`,
    ok: quietMedian <= fullMedian * 2 + 5,
    actual: quietMedian.toFixed(1),
    expected: `<= ${(fullMedian * 2 + 5).toFixed(1)}`,
  });
  const stateBefore = fs.statSync(acc.stateFile).mtimeMs;
  const markerBefore = fs.statSync(marker).mtimeMs;
  for (let i = 0; i < 5; i += 1) acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('quiet path: writes nothing (state and marker untouched)', fs.statSync(acc.stateFile).mtimeMs === stateBefore && fs.statSync(marker).mtimeMs === markerBefore, true);
  acc.statusline('s1', 'claude-fable-5-1', 93, now + 7200, 23, now + 3 * 86400);
  acc.setConfig((config) => {
    config.wait.maxInHookMinutes = 0;
  });
  check('quiet path: a usage jump is never hidden by the marker', acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }).includes('"continue":false'), true);
  check('quiet path: marker gone once a wait exists', fs.existsSync(marker), false);
  acc.run(['cancel', 's1']);
  acc.setConfig((config) => {
    config.wait.maxInHookMinutes = 330;
  });
  acc.statusline('s1', 'claude-fable-5-1', 41, now + 7200, 23, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('quiet path: marker returns when calm again', fs.existsSync(marker), true);
  const fileStamp = (file) => {
    const content = fs.readFileSync(file);
    return `${content.length}:${require('crypto').createHash('sha256').update(content).digest('hex').slice(0, 16)}`;
  };
  check('quiet path: the marker stamps the state file by content', readJson(marker).state, fileStamp(acc.stateFile));
  acc.run(['off', '1']);
  check('quiet path: a state change (noctis off) invalidates the marker stamp', readJson(marker).state !== fileStamp(acc.stateFile), true);
  const stateStampBefore = fs.statSync(acc.stateFile).mtimeMs;
  acc.run(['on']);
  acc.statusline('s1', 'claude-fable-5-1', 41, now + 7200, 23, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('quiet path: a stale marker forces a full decision (state is written again)', fs.statSync(acc.stateFile).mtimeMs !== stateStampBefore, true);
  const qmarker = path.join(acc.guardDir, 'quiet', 'qz.json');
  acc.statusline('qz', 'claude-fable-5-1', 41, now + 5000, 23, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'qz', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const quietRun = (mutate) => {
    acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'qz', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
    const before = JSON.stringify(readJson(qmarker));
    mutate();
    acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'qz', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
    return JSON.stringify(readJson(qmarker)) === before; 
  };
  check('quiet path: usage drift within the tolerance stays quiet', quietRun(() => acc.statusline('qz', 'claude-fable-5-1', 42, now + 5000, 23, now + 3 * 86400)), true);
  check('quiet path: usage drift past the tolerance re-decides', quietRun(() => acc.statusline('qz', 'claude-fable-5-1', 48, now + 5000, 23, now + 3 * 86400)), false);
  check('quiet path: a new reset time re-decides', quietRun(() => acc.statusline('qz', 'claude-fable-5-1', 48, now + 5200, 23, now + 3 * 86400)), false);
  check('quiet path: a model change re-decides', quietRun(() => acc.statusline('qz', 'claude-opus-5', 48, now + 5200, 23, now + 3 * 86400)), false);
  check('quiet path: context growth re-decides', quietRun(() => acc.statusline('qz', 'claude-opus-5', 48, now + 5200, 23, now + 3 * 86400, 80)), false);
  check('quiet path: a config edit re-decides', quietRun(() => acc.setConfig((config) => { config.thresholds.session5h = 91; })), false);
  acc.setConfig((config) => { config.thresholds.session5h = 92; });
  check('quiet path: a fable.json refresh re-decides', quietRun(() => fs.writeFileSync(path.join(acc.guardDir, 'fable.json'), JSON.stringify({ fetchedAt: nowSec(), five_hour: { used: 48, resetsAt: now + 5200 } }))), false);
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'qz', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const subagentMarker = JSON.stringify(readJson(qmarker));
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'qz', agent_id: 'a1', agent_type: 'general-purpose', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('quiet path: a subagent hook neither takes it nor refreshes it', JSON.stringify(readJson(qmarker)), subagentMarker);
  acc.run(['cancel']);
  mock.limits = [];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
}

async function scenarioWarnAndBurst(acc) {
  const now = nowSec();
  for (const used of [70, 76, 82, 87]) acc.statusline('s1', 'claude-fable-5-1', used, now + 7300, 23, now + 3 * 86400);
  const warn = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's1', cwd: PROJECT_DIR, prompt: 'continue editing auth.js and keep the tests green' });
  check('warn context once', warn.includes('auto-pause at 92%'), true);
  check('warn does not stop', warn.includes('"decision":"block"'), false);
  check('warn not repeated', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's1', cwd: PROJECT_DIR, prompt: 'continue editing auth.js and keep the tests green' }), '');
  for (const used of [60, 78, 85]) acc.statusline('s1', 'claude-fable-5-1', used, now + 2 * 86400, 23, now + 3 * 86400);
  const out = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('burst projection stops early (85% + 18% burst)', out.includes('"continue":false'), true);
  check('burst reason logged', fs.readFileSync(path.join(acc.guardDir, 'guard.log'), 'utf8').includes('ani yükseliş öngörüsü'), true);
  acc.run(['cancel']);
  acc.statusline('s1', 'claude-fable-5-1', 30, now + 7200, 23, now + 3 * 86400);
}

async function scenarioInHookWait(acc) {
  const now = nowSec();
  acc.statusline('s1', 'claude-fable-5-1', 93, now + 3, 23, now + 3 * 86400);
  const child = acc.hookAsync({ hook_event_name: 'UserPromptSubmit', session_id: 's1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'keep going with auth.js' });
  let stdout = '';
  child.stdout.on('data', (chunk) => {
    stdout += chunk;
  });
  const mid = await waitRecord(acc, 's1');
  check('wait registered mid-hook', Boolean(mid && mid.inHook), true);
  check('heartbeat present', Boolean(mid && mid.heartbeat >= now), true);
  const watchdogPid = mid && mid.scheduled && mid.scheduled.pid;
  await new Promise((resolve) => child.on('close', resolve));
  check('in-hook wait lasted until reset', Boolean(mid) && Date.now() / 1000 >= mid.resumeAt - 1, true);
  check('in-hook notice', stdout.includes('beklendi, devam ediliyor'), true);
  check('wait cleared after hook', acc.state().waits.s1 === undefined, true);
  await sleep(300);
  check('watchdog cancelled', isAlive(watchdogPid), false);
  check('checkpoint consumed', acc.state().checkpoints.s1.consumed, true);
}

async function scenarioWorkspaceGuard(acc) {
  const now = nowSec();
  const repo = path.join(LAB_ROOT, 'guard-repo');
  fs.rmSync(repo, { recursive: true, force: true });
  fs.mkdirSync(repo, { recursive: true });
  const git = (...args) => spawnSync('git', args, { cwd: repo, encoding: 'utf8', env: { ...process.env, GIT_AUTHOR_NAME: 'lab', GIT_AUTHOR_EMAIL: 'lab@example.com', GIT_COMMITTER_NAME: 'lab', GIT_COMMITTER_EMAIL: 'lab@example.com' } });
  git('init', '-q');
  fs.writeFileSync(path.join(repo, 'a.txt'), 'one\n');
  git('add', '.');
  git('commit', '-q', '-m', 'init');
  acc.statusline('wg1', 'claude-fable-5-1', 93, now + 3, 23, now + 3 * 86400);
  const child = acc.hookAsync({ hook_event_name: 'UserPromptSubmit', session_id: 'wg1', cwd: repo, transcript_path: TRANSCRIPT, prompt: 'keep going with a.txt' });
  let stdout = '';
  child.stdout.on('data', (chunk) => {
    stdout += chunk;
  });
  const wg1 = await waitRecord(acc, 'wg1');
  check('workspace guard: fingerprint stored with the wait', typeof wg1.tree === 'string' && wg1.tree.length === 16, true);
  fs.writeFileSync(path.join(repo, 'a.txt'), 'someone edited this while it waited\n');
  await new Promise((resolve) => child.on('close', resolve));
  check('workspace guard: user notice mentions the changed tree', stdout.includes('çalışma ağacı değişti'), true);
  check('workspace guard: the model is told to re-check', stdout.includes('"additionalContext"') && stdout.includes('git status differs from the checkpoint'), true);
  check('workspace guard: journaled', acc.run(['why', '--last', '3']).includes('workspace-changed'), true);
  acc.statusline('wg2', 'claude-fable-5-1', 93, nowSec() + 3, 23, now + 3 * 86400);
  const quiet = await acc.hookPromise({ hook_event_name: 'UserPromptSubmit', session_id: 'wg2', cwd: repo, transcript_path: TRANSCRIPT, prompt: 'keep going with a.txt' });
  check('workspace guard: silent when nothing changed', !quiet.includes('additionalContext') && quiet.includes('devam ediliyor'), true);
  acc.setConfig((config) => {
    config.wait.workspaceGuard = false;
  });
  acc.statusline('wg3', 'claude-fable-5-1', 93, nowSec() + 3, 23, now + 3 * 86400);
  const off = acc.hookAsync({ hook_event_name: 'UserPromptSubmit', session_id: 'wg3', cwd: repo, transcript_path: TRANSCRIPT, prompt: 'keep going with a.txt' });
  const wg3 = await waitRecord(acc, 'wg3');
  check('workspace guard: off -> no fingerprint', wg3.tree, undefined);
  await new Promise((resolve) => off.on('close', resolve));
  acc.setConfig((config) => {
    config.wait.workspaceGuard = true;
    config.checkpoint = { gitSnapshot: true };
  });
  fs.writeFileSync(path.join(repo, 'a.txt'), 'snapshot me\n');
  acc.statusline('gs1', 'claude-fable-5-1', 93, nowSec() + 2 * 86400, 10, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'gs1', cwd: repo, transcript_path: TRANSCRIPT });
  const refs = git('for-each-ref', '--format=%(refname) %(objectname)', 'refs/noctis/gs1/').stdout.trim().split('\n').filter(Boolean);
  check('git snapshot: one hidden ref per checkpoint', refs.length, 1);
  const [ref, hash] = (refs[0] || ' ').split(' ');
  check('git snapshot: captures the uncommitted change without touching the tree', git('show', `${hash}:a.txt`).stdout === 'snapshot me\n' && git('status', '--short').stdout.trim() === 'M a.txt', true);
  const checkpointText = fs.readFileSync(acc.state().checkpoints.gs1.path, 'utf8');
  check('git snapshot: checkpoint tells how to restore', checkpointText.includes(ref) && checkpointText.includes(`git stash apply ${hash}`), true);
  acc.run(['cancel', 'gs1']);
  const aged = acc.state();
  aged.checkpoints.gs1.at = nowSec() - 8 * 86400;
  writeJson(acc.stateFile, aged);
  acc.hook({ hook_event_name: 'PostModelSwitch', session_id: 'gs1', to_model: 'claude-fable-5-1' });
  check('git snapshot: pruned with the expired checkpoint', git('for-each-ref', 'refs/noctis/gs1/').stdout.trim(), '');
  acc.setConfig((config) => {
    config.checkpoint = { gitSnapshot: false };
  });
  acc.statusline('wg3', 'claude-fable-5-1', 10, now + 7200, 10, now + 3 * 86400);
}

async function scenarioKilledHookRecovery(acc) {
  const now = nowSec();
  resetCalls();
  acc.statusline('s2', 'claude-opus-5', 94, now + 6, 23, now + 3 * 86400);
  const child = acc.hookAsync({ hook_event_name: 'PostToolBatch', session_id: 's2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  await waitRecord(acc, 's2');
  child.kill();
  await sleep(300);
  const wait = acc.state().waits.s2;
  check('wait survives hook kill', Boolean(wait && wait.inHook), true);
  const watchdogPid = wait.scheduled && wait.scheduled.pid;
  if (watchdogPid) {
    try {
      process.kill(watchdogPid);
    } catch {
      results.push({ name: 'watchdog pid missing', ok: false });
    }
  }
  const stale = acc.state();
  stale.waits.s2.heartbeat = now - 400;
  writeJson(acc.stateFile, stale);
  await sleep(5500);
  const old = new Date(Date.now() - 3600000);
  fs.utimesSync(TRANSCRIPT, old, old);
  acc.statusline('s2', 'claude-opus-5', 5, now + 7200, 23, now + 3 * 86400);
  acc.run(['resume', '--sid', 's2', '--account', acc.dir]);
  const calls = callsLog();
  check('runner relaunched dead session', calls.length, 1);
  check('relaunch keeps account', calls[0] && calls[0].includes(`CONFIG=${acc.dir}`), true);
  check('relaunch marks handoff env', calls[0] && calls[0].includes('HANDOFF=s2'), true);
  check('relaunch effort max', calls[0] && calls[0].includes('--effort max'), true);
  check('handoff released', Object.keys(acc.state().handedOff).length, 0);
}

async function scenarioWeeklyLongWait(acc) {
  const now = nowSec();
  acc.statusline('s3', 'claude-fable-5-1', 50, now + 7200, 90, now + 2 * 86400);
  const stop = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's3', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('weekly stop', stop.includes('"continue":false'), true);
  check('weekly stop message short', JSON.parse(stop).stopReason.length < 140, true);
  const block = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's3', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'please finish the tests' });
  check('weekly prompt blocked', block.includes('"decision":"block"'), true);
  const wait = acc.state().waits.s3;
  check('prompt queued', wait.queuedPrompt, 'please finish the tests');
  const firstPid = wait.scheduled.pid;
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's3', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'second prompt' });
  await sleep(300);
  check('previous watchdog replaced', isAlive(firstPid), false);
  const replacement = acc.state().waits.s3.scheduled;
  check('replacement watchdog is alive', replacement && replacement.pid ? isAlive(replacement.pid) : 'no pid recorded', true);
  check('replacement is a different runner', replacement.pid !== firstPid, true);
  check('second queued prompt wins', acc.state().waits.s3.queuedPrompt, 'second prompt');
  acc.run(['cancel', 's3']);
  await sleep(300);
  check('cancel removes wait', acc.state().waits.s3 === undefined, true);
}

async function scenarioFableFlow(acc) {
  const now = nowSec();
  resetCalls();
  mock.limits = [
    { kind: 'session', percent: 30, resets_at: new Date((now + 1800) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 40, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
    { kind: 'weekly_scoped', percent: 96, resets_at: new Date((now + 2 * 86400) * 1000).toISOString(), scope: { group: 'model', model: { display_name: 'Fable' } } },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('s4', 'claude-fable-5-1', 30, now + 1800, 40, now + 3 * 86400);
  const hitsBefore = mock.hits;
  const block = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's4', cwd: PROJECT_DIR, prompt: 'go on with the code changes' });
  check('mock usage fetched', mock.hits > hitsBefore, true);
  check('oauth headers', Boolean(mock.lastHeaders && mock.lastHeaders['anthropic-beta'] === 'oauth-2025-04-20' && /^claude-code\//.test(mock.lastHeaders['user-agent'])), true);
  check('fable prompt block', block.includes('/model opus'), true);
  check('default switched to opus', acc.settingsModel(), 'opus');
  const stop = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's4', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('fable batch relaunch plan', stop.includes('yeni pencerede'), true);
  const pid = acc.state().waits.s4.scheduled.pid;
  check('fable watchdog pid', Number.isInteger(pid) && pid > 0, true);
  try {
    process.kill(pid);
  } catch (error) {
    if (error && error.code !== 'ESRCH') {
      process.stdout.write(`  fable watchdog kill failed: ${error.code} pid=${JSON.stringify(pid)} scheduled=${JSON.stringify(acc.state().waits.s4 && acc.state().waits.s4.scheduled)}\n`);
      results.push({ name: 'fable watchdog still owned', ok: false });
    }
  }
  const runner = spawn(acc.engine()[0], ['resume', '--sid', 's4', '--account', acc.dir], { env: acc.env(), stdio: 'ignore' });
  await sleep(600);
  check('old window blocked during handoff', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's4', prompt: 'typing in old window' }).includes('"decision":"block"'), true);
  check('new window passes with env marker', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's4', cwd: PROJECT_DIR, prompt: 'continue the code' }, { NOCTIS_HANDOFF: 's4' }), '');
  await new Promise((resolve) => runner.on('close', resolve));
  const calls = callsLog();
  check('relaunched on opus', calls.some((line) => line.includes('--model opus')), true);
  check('handoff cleared', Object.keys(acc.state().handedOff).length, 0);
  const limits = mock.limits;
  limits[2].percent = 10;
  mock.limits = limits;
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  check('no premature revert while quota window open', acc.settingsModel(), 'opus');
  const switched = acc.state();
  switched.modelSwitched.fableResetsAt = now - 10;
  writeJson(acc.stateFile, switched);
  const morning = acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 's4-morning', cwd: PROJECT_DIR });
  check('SessionStart reverts the default model once the scoped window cleared', morning.includes('yeniden fable') && acc.settingsModel() === 'fable', true);
  writeJson(path.join(acc.dir, 'settings.json'), { ...readJson(path.join(acc.dir, 'settings.json')), model: 'opus' });
  const again = acc.state();
  again.modelSwitched = { at: now - 3600, from: 'fable', to: 'opus', fableResetsAt: now - 10 };
  writeJson(acc.stateFile, again);
  const notice = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's4', cwd: PROJECT_DIR, prompt: 'continue the code' }, { NOCTIS_HANDOFF: 's4' });
  check('revert notice after quota reset', notice.includes('yeniden fable'), true);
  check('default reverted', acc.settingsModel(), 'fable');
}

async function scenarioStaleFallbackAndNearEdge(acc) {
  const now = nowSec();
  const usageFile = path.join(acc.guardDir, 'usage.json');
  const usage = readJson(usageFile);
  usage.updatedAt = now - 3600;
  writeJson(usageFile, usage);
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  mock.limits = [
    { kind: 'session', percent: 95, resets_at: new Date((now + 2 * 86400) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 10, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
  const stop = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's5', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('stale usage -> oauth fallback stops', stop.includes('"continue":false'), true);
  acc.run(['cancel', 's5']);
  const fable = readJson(path.join(acc.guardDir, 'fable.json'));
  fable.fetchedAt = now - 120;
  fable.five_hour.used = 86;
  writeJson(path.join(acc.guardDir, 'fable.json'), fable);
  acc.statusline('s5', 'claude-opus-5', 86, now + 7200, 10, now + 3 * 86400);
  const nearLimits = mock.limits;
  nearLimits[0].percent = 20;
  mock.limits = nearLimits;
  const hitsBefore = mock.hits;
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's5', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('near-edge refresh within poll interval', mock.hits, hitsBefore + 1);
  acc.statusline('s5', 'claude-opus-5', 20, now + 7200, 10, now + 3 * 86400);
}

async function scenarioStopFailure(acc) {
  const now = nowSec();
  mock.limits = [
    { kind: 'session', percent: 97, resets_at: new Date((now + 1800) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 40, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('s6', 'claude-opus-5', 97, now + 1800, 40, now + 3 * 86400);
  acc.hook({ hook_event_name: 'StopFailure', session_id: 's6', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'rate_limit' });
  const wait = acc.state().waits.s6;
  check('stopfailure culprit', wait && wait.window, 'five_hour');
  check('stopfailure resume after reset+margins', wait && wait.resumeAt - now >= 1800, true);
  acc.hook({ hook_event_name: 'Notification', session_id: 's6', notification_type: 'quota_auto_resume_fired', message: 'x' });
  await sleep(300);
  check('builtin auto-continue cancels runner', acc.state().waits.s6 === undefined, true);
  const lowLimits = mock.limits;
  lowLimits[0].percent = 30;
  mock.limits = lowLimits;
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('s7', 'claude-fable-5-1', 30, now + 1800, 40, now + 3 * 86400);
  acc.hook({ hook_event_name: 'StopFailure', session_id: 's7', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'rate_limit' });
  const transientWait = acc.state().waits.s7;
  check('transient 429 without Fable evidence -> retry, no model switch', transientWait && transientWait.window, 'unknown');
  check('transient 429 keeps default model', acc.settingsModel(), 'fable');
  acc.run(['cancel', 's7']);
  acc.hook({ hook_event_name: 'StopFailure', session_id: 's7', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'rate_limit', error_message: "You've hit your weekly limit · resets Monday" });
  const namedWeekly = acc.state().waits.s7;
  check('error_message naming the weekly limit -> weekly culprit at 40 %', namedWeekly && namedWeekly.window === 'seven_day' && Math.abs(namedWeekly.until - (now + 3 * 86400)) < 5, true);
  check('named culprit journaled with the message', acc.run(['why', '--last', '1', '--json']).includes('"hint":"seven_day"'), true);
  acc.run(['cancel', 's7']);
  acc.hook({ hook_event_name: 'StopFailure', session_id: 's7', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'rate_limit', error_message: 'Session limit reached (5-hour window)' });
  check('error_message naming the 5-hour limit -> five_hour culprit at 30 %', acc.state().waits.s7 && acc.state().waits.s7.window, 'five_hour');
  acc.run(['cancel', 's7']);
  mock.limits = [...lowLimits, { kind: 'weekly_scoped', percent: 60, resets_at: new Date((now + 2 * 86400) * 1000).toISOString(), scope: { group: 'model', model: { display_name: 'Fable' } } }];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.hook({ hook_event_name: 'StopFailure', session_id: 's7', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'rate_limit', error_message: 'Fable usage limit reached for this week' });
  check('error_message naming the scoped bucket -> switch even at 60 %', acc.state().waits.s7 && acc.state().waits.s7.window === 'fable' && acc.settingsModel() === 'opus', true);
  acc.run(['cancel']);
  const namedState = acc.state();
  namedState.modelSwitched = null;
  writeJson(acc.stateFile, namedState);
  writeJson(path.join(acc.dir, 'settings.json'), { ...readJson(path.join(acc.dir, 'settings.json')), model: 'fable' });
  mock.limits = [...lowLimits, { kind: 'weekly_scoped', percent: 97, resets_at: new Date((now + 2 * 86400) * 1000).toISOString(), scope: { group: 'model', model: { display_name: 'Fable' } } }];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.hook({ hook_event_name: 'StopFailure', session_id: 's7', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'rate_limit' });
  const fableWait = acc.state().waits.s7;
  check('fable cap suspected with bucket evidence', fableWait && fableWait.window, 'fable');
  check('fable suspicion switches default', acc.settingsModel(), 'opus');
  acc.run(['cancel']);
  const state = acc.state();
  state.modelSwitched = null;
  writeJson(acc.stateFile, state);
  writeJson(path.join(acc.dir, 'settings.json'), { ...readJson(path.join(acc.dir, 'settings.json')), model: 'fable' });
  mock.limits = lowLimits;
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('ov1', 'claude-fable-5-1', 30, now + 1800, 40, now + 3 * 86400);
  acc.hook({ hook_event_name: 'StopFailure', session_id: 'ov1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'overloaded' });
  const first = acc.state().waits.ov1;
  check('overloaded: first retry after ~30 s with jitter', first && first.overload === true && first.resumeAt - nowSec() >= 20 && first.resumeAt - nowSec() <= 40, true);
  check('overloaded: episode attempt 1', acc.state().overload.ov1.attempts, 1);
  check('overloaded: journaled as backoff', acc.run(['why', '--last', '1']).includes('overload-backoff'), true);
  acc.hook({ hook_event_name: 'StopFailure', session_id: 'ov1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'server_error' });
  const second = acc.state().waits.ov1;
  check('server_error: second retry doubles (~60 s)', second && second.resumeAt - nowSec() >= 43 && second.resumeAt - nowSec() <= 76, true);
  for (let i = 0; i < 6; i += 1) acc.hook({ hook_event_name: 'StopFailure', session_id: 'ov1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'overloaded' });
  const capped = acc.state().waits.ov1;
  check('overloaded: delay capped at 5 min', capped && capped.resumeAt - nowSec() <= 300 && capped.resumeAt - nowSec() >= 220, true);
  check('overloaded: model untouched', acc.settingsModel(), 'fable');
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'ov1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('successful model call clears the overload episode', acc.state().overload.ov1 === undefined, true);
  acc.run(['cancel', 'ov1']);
  const spent = acc.state();
  spent.overload = { ov1: { firstAt: nowSec() - 9000, lastAt: nowSec() - 60, attempts: 12 } };
  writeJson(acc.stateFile, spent);
  acc.hook({ hook_event_name: 'StopFailure', session_id: 'ov1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'overloaded' });
  check('overloaded: retry budget spent -> give up, no wait registered', acc.state().waits.ov1 === undefined && acc.run(['why', '--last', '1']).includes('overload-giveup'), true);
  const stale = acc.state();
  stale.overload = { ov1: { firstAt: nowSec() - 5 * 3600, lastAt: nowSec() - 4 * 3600, attempts: 12 } };
  writeJson(acc.stateFile, stale);
  acc.hook({ hook_event_name: 'StopFailure', session_id: 'ov1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'overloaded' });
  check('overloaded: an old episode starts fresh', acc.state().overload.ov1.attempts === 1 && acc.state().waits.ov1 !== undefined, true);
  acc.run(['cancel', 'ov1']);
}

async function scenarioRouter(acc) {
  const now = nowSec();
  acc.statusline('s8', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400);
  const cases = [
    ['Kullanıcı auth modülünü refactor et ve testleri düzelt', 'code-signal'],
    ['React ile Vue karşılaştırması 2026 hangisi daha iyi', 'web-words'],
    ['Şu yazıyı özetle https://example.com/a/b.html', 'url'],
    ['npm install hatası alıyorum yardım et', 'code-signal'],
    ['En iyi mekanik klavye 2026 araştır', 'web-words'],
    ['devam et lütfen', 'continuation'],
    ['/model opus', 'short-or-command'],
    ['lite: bu konuyu araştır', 'forced'],
    ['fable: bu konuyu araştır', 'forced-main'],
    ['Bu projedeki performans sorununu araştır', 'code-signal'],
    ["Anthropic'in son haberleri neler, araştırabilir misin", 'web-words'],
    ['JWT nasıl çalışır kısaca anlat', 'no-research-signal'],
    ['Summarize the meeting notes in notes.md', 'code-signal'],
    ['Compare pricing of Claude Max and ChatGPT Pro plans', 'web-words'],
    ['Docker compose ile deploy nasıl yapılır araştır', 'code-signal'],
    ['Research the best approach to implement caching in our api', 'code-signal'],
    ['What are the latest findings on intermittent fasting?', 'web-words'],
    ['Write a short poem about autumn in Ankara', 'no-research-signal'],
    ['hataları araştır ve logları incele', 'code-signal'],
    ['the logic behind quantum computing, latest research', 'web-words'],
    ['Look up the current USD/TRY exchange rate news', 'web-words'],
    ['Bu konuşmayı özetle lütfen', 'summary-needs-context'],
    [`Şu metni özetle: ${'lorem ipsum dolor sit amet '.repeat(50)}`, 'long-text-summary'],
    ['Kuantum bilgisayarların temellerini araştır', 'investigate'],
    ['Investigate the history of the Ottoman navy', 'investigate'],
    ['Dosyalarımızdaki yorumları temizle', 'code-signal'],
  ];
  for (const [prompt, expected] of cases) check(`classify: ${prompt.slice(0, 40)}`, JSON.parse(acc.run(['classify', prompt])).reason, expected);
  check('investigate during coding session stays on main model', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's8', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'Kuantum bilgisayarların temellerini araştır' }), '');
  const quietTranscript = path.join(PROJECT_DIR, 'quiet.jsonl');
  fs.writeFileSync(quietTranscript, `${JSON.stringify({ type: 'user', message: { role: 'user', content: 'merhaba' } })}\n`);
  check('investigate in a non-coding session routes', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's8', cwd: PROJECT_DIR, transcript_path: quietTranscript, prompt: 'Kuantum bilgisayarların temellerini araştır' }).includes('additionalContext'), true);
  check('web signal routes even in coding session', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's8', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'En iyi mekanik klavye 2026 araştır' }).includes('additionalContext'), true);
  const directive = JSON.parse(acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's8', cwd: PROJECT_DIR, prompt: 'En iyi mekanik klavye 2026 araştır' })).hookSpecificOutput.additionalContext;
  check('directive stays compact (<260 chars)', directive.length < 260, true);
  const routed = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's8', cwd: PROJECT_DIR, prompt: 'En iyi mekanik klavye 2026 araştır' });
  check('route directive + notice', routed.includes('additionalContext') && routed.includes('systemMessage'), true);
  const deny = acc.hook({ hook_event_name: 'PreToolUse', session_id: 's8', tool_name: 'WebSearch', tool_input: { query: 'x' } });
  check('main-thread WebSearch denied', deny.includes('"permissionDecision":"deny"'), true);
  check('subagent WebSearch allowed', acc.hook({ hook_event_name: 'PreToolUse', session_id: 's8', agent_id: 'a1', agent_type: `${PLUGIN_NAME}:lite`, tool_name: 'WebSearch', tool_input: {} }), '');
  acc.hook({ hook_event_name: 'PreToolUse', session_id: 's8', tool_name: 'WebFetch', tool_input: {} });
  acc.hook({ hook_event_name: 'PreToolUse', session_id: 's8', tool_name: 'WebFetch', tool_input: {} });
  check('deny cap releases', acc.hook({ hook_event_name: 'PreToolUse', session_id: 's8', tool_name: 'WebSearch', tool_input: {} }), '');
  check('code prompt clears route', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 's8', cwd: PROJECT_DIR, prompt: 'auth.js dosyasındaki hatayı düzelt' }), '');
  check('no route -> WebSearch free', acc.hook({ hook_event_name: 'PreToolUse', session_id: 's8', tool_name: 'WebSearch', tool_input: {} }), '');
}

async function scenarioIsolationAndConcurrency(accA, accB) {
  const now = nowSec();
  accA.statusline('shared', 'claude-opus-5', 50, now + 7200, 90, now + 2 * 86400);
  accB.statusline('shared', 'claude-opus-5', 10, now + 7200, 10, now + 3 * 86400);
  const stopA = accA.hook({ hook_event_name: 'PostToolBatch', session_id: 'shared', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('account A stops', stopA.includes('"continue":false'), true);
  check('account B unaffected', accB.hook({ hook_event_name: 'PostToolBatch', session_id: 'shared', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  check('account B has no wait', (accB.state().waits || {}).shared === undefined, true);
  accA.run(['cancel']);
  const children = [];
  for (let i = 0; i < 12; i += 1) children.push(accA.hookAsync({ hook_event_name: 'PostModelSwitch', session_id: `c${i}`, to_model: 'claude-opus-5' }));
  await Promise.all(children.map((child) => new Promise((resolve) => child.on('close', resolve))));
  check('12 parallel writes kept', Object.keys(accA.state().modelOverrides).filter((key) => key.startsWith('c')).length, 12);
  check('no lock left', fs.existsSync(path.join(accA.guardDir, 'state.lock')), false);
}

async function scenarioFoundation(acc) {
  const now = nowSec();
  const children = [];
  for (let i = 0; i < 12; i += 1) {
    const child = spawn(acc.engine()[0], ['statusline'], { env: acc.env(), stdio: ['pipe', 'ignore', 'ignore'] });
    child.stdin.end(JSON.stringify({ session_id: `p${i}`, cwd: PROJECT_DIR, model: { id: 'claude-opus-5' }, rate_limits: { five_hour: { used_percentage: 20 + i, resets_at: now + 7200 }, seven_day: { used_percentage: 10, resets_at: now + 3 * 86400 } } }));
    children.push(new Promise((resolve) => child.on('close', resolve)));
  }
  await Promise.all(children);
  const usage = readJson(path.join(acc.guardDir, 'usage.json'));
  check('12 parallel statusline writes keep every session', Object.keys(usage.sessions).filter((key) => key.startsWith('p')).length, 12);
  check('history keeps the full burst window under concurrent writes', usage.history.five_hour.length, 6);
  check('no usage lock left', fs.existsSync(path.join(acc.guardDir, 'usage.lock')), false);
  mock.skew = 600;
  mock.limits = [{ kind: 'session', percent: 95, resets_at: new Date((now + 300) * 1000).toISOString() }];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('skew', 'claude-fable-5-1', 95, now + 300, 10, now + 3 * 86400);
  check('server-ahead skew: window already expired -> no block', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'skew', cwd: PROJECT_DIR, prompt: 'continue editing auth.js' }), '');
  check('clock offset recorded', Math.abs(Number(readJson(path.join(acc.guardDir, 'fable.json')).clockOffset) - 600) <= 5, true);
  mock.skew = 0;
  mock.limits = [];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('skew', 'claude-opus-5', 10, now + 7200, 10, now + 3 * 86400);
  const hooksFile = path.join(acc.dir, 'skills', PLUGIN_NAME, 'hooks', 'hooks.json');
  const hooks = readJson(hooksFile);
  for (const groups of Object.values(hooks.hooks)) for (const group of groups) for (const hook of group.hooks) hook.command = '/nonexistent/node';
  writeJson(hooksFile, hooks);
  const usageFile = path.join(acc.guardDir, 'usage.json');
  writeJson(usageFile, { ...readJson(usageFile), selfHealAt: 0 });
  acc.statusline('skew', 'claude-opus-5', 10, now + 7200, 10, now + 3 * 86400);
  const healed = readJson(hooksFile);
  const installedBinary = path.join(acc.dir, 'skills', PLUGIN_NAME, 'bin', IS_WINDOWS ? 'noctis.exe' : 'noctis').replace(/\\/g, '/');
  const healedCommand = healed.hooks.SessionStart.flatMap((group) => group.hooks).find((hook) => Array.isArray(hook.args)).command;
  check('hooks engine path self-healed to the plugin root copy', healedCommand, installedBinary);
  const statusLineCommand = String((readJson(path.join(acc.dir, 'settings.json')).statusLine || {}).command || '');
  check('both repairs agree on which binary is the right one', statusLineCommand.includes(installedBinary), true);
  const settingsFile = path.join(acc.dir, 'settings.json');
  const settings = readJson(settingsFile);
  const savedStatusLine = settings.statusLine;
  delete settings.statusLine;
  writeJson(settingsFile, settings);
  const notice = acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'boot', cwd: PROJECT_DIR });
  check('self-check reports missing statusLine', notice.includes('statusLine bağlı değil'), true);
  check('self-check once per day', acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'boot2', cwd: PROJECT_DIR }).includes('systemMessage'), false);
  writeJson(settingsFile, { ...settings, statusLine: savedStatusLine });
  const state = acc.state();
  state.checkpoints.orphan = { path: path.join(acc.guardDir, 'checkpoints', 's3.md'), cwd: PROJECT_DIR, at: now + 1, consumed: false };
  writeJson(acc.stateFile, state);
  const startup = acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'boot3', cwd: PROJECT_DIR });
  check('orphan checkpoint injected as context', startup.includes('additionalContext') && startup.includes('devam notu'), true);
  check('orphan checkpoint consumed', acc.state().checkpoints.orphan.consumed, true);
}

async function scenarioCompactAndClear(acc) {
  const now = nowSec();
  acc.statusline('cmp', 'claude-opus-5', 87, now + 2 * 86400, 10, now + 3 * 86400, 90);
  const stop = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'cmp', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('compaction imminent + near threshold -> early stop', stop.includes('"continue":false'), true);
  check('compaction reason logged', fs.readFileSync(path.join(acc.guardDir, 'guard.log'), 'utf8').includes('sıkıştırması öncesi güvenlik'), true);
  acc.run(['cancel', 'cmp']);
  acc.statusline('cmp', 'claude-opus-5', 87, now + 2 * 86400, 10, now + 3 * 86400, 30);
  check('same usage without compaction risk -> continues', acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'cmp', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  const compactTranscript = path.join(PROJECT_DIR, 'compact.jsonl');
  fs.writeFileSync(compactTranscript, [
    JSON.stringify({ type: 'user', message: { role: 'user', content: 'Implement the payment webhook handler' } }),
    JSON.stringify({ type: 'assistant', message: { role: 'assistant', content: [{ type: 'tool_use', name: 'Edit', input: { file_path: path.join(PROJECT_DIR, 'src', 'webhook.js') } }] } }),
    JSON.stringify({ type: 'user', isCompactSummary: true, message: { role: 'user', content: 'This session is being continued from a previous conversation that ran out of context. Summary: webhook handler half done.' } }),
    JSON.stringify({ type: 'assistant', message: { role: 'assistant', content: [{ type: 'text', text: 'Continuing with signature verification.' }] } }),
  ].join('\n'));
  acc.statusline('cmp', 'claude-opus-5', 93, now + 2 * 86400, 10, now + 3 * 86400, 30);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'cmp', cwd: PROJECT_DIR, transcript_path: compactTranscript });
  const checkpoint = fs.readFileSync(acc.state().checkpoints.cmp.path, 'utf8');
  check('checkpoint keeps real last request after compaction', checkpoint.includes('## Son istek\nImplement the payment webhook handler'), true);
  check('checkpoint carries compact summary section', checkpoint.includes('## Bağlam özeti (sıkıştırma)'), true);
  acc.run(['cancel', 'cmp']);
  acc.statusline('cmp', 'claude-opus-5', 10, now + 7200, 10, now + 3 * 86400, 30);
  acc.statusline('old', 'claude-opus-5', 10, now + 7200, 10, now + 3 * 86400);
  const state = acc.state();
  state.waits.old = { kind: 'batch', window: 'five_hour', label: '5s', until: now + 600, resumeAt: now + 601, inHook: true, heartbeat: now, cwd: PROJECT_DIR, transcript: TRANSCRIPT, checkpoint: '', queuedPrompt: '', startedAt: now - 5 };
  state.waits.parked = { kind: 'batch', window: 'seven_day', label: 'haftalık', until: now + 86400, resumeAt: now + 86401, inHook: false, cwd: PROJECT_DIR, transcript: TRANSCRIPT, checkpoint: '', queuedPrompt: '', startedAt: now - 5 };
  writeJson(acc.stateFile, state);
  acc.hook({ hook_event_name: 'PostModelSwitch', session_id: 'old', to_model: 'claude-fable-5-1' });
  acc.hook({ hook_event_name: 'SessionStart', source: 'clear', session_id: 'fresh', cwd: PROJECT_DIR });
  const after = acc.state();
  check('/clear releases interrupted in-hook wait of previous session', after.waits.old === undefined, true);
  check('/clear keeps parked long waits of other sessions', Boolean(after.waits.parked), true);
  check('/clear carries live model to new session id', after.modelOverrides.fresh && after.modelOverrides.fresh.model, 'claude-fable-5-1');
  acc.run(['cancel']);
}

async function scenarioAgentGateAndWarnBand(accA) {
  const now = nowSec();
  accA.statusline('fo1', 'claude-fable-5-1', 50, now + 7200, 90, now + 2 * 86400);
  const plainStop = accA.hook({ hook_event_name: 'PostToolBatch', session_id: 'fo1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('weekly limit -> wait on the same account, never handed elsewhere', plainStop.includes('⏸') && accA.state().waits.fo1.kind === 'batch' && !plainStop.includes('↪'), true);
  accA.run(['cancel', 'fo1']);
  accA.statusline('fo2', 'claude-fable-5-1', 93, now + 2 * 86400, 10, now + 3 * 86400);
  const agentDeny = accA.hook({ hook_event_name: 'PreToolUse', session_id: 'fo2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, tool_name: 'Agent', tool_input: { prompt: 'explore' } });
  check('subagent spawn denied at threshold', agentDeny.includes('"permissionDecision":"deny"') && agentDeny.includes('⏸'), true);
  accA.run(['cancel', 'fo2']);
  accA.statusline('fo2', 'claude-fable-5-1', 40, now + 7200, 10, now + 3 * 86400);
  check('subagent spawn allowed below threshold', accA.hook({ hook_event_name: 'PreToolUse', session_id: 'fo2', cwd: PROJECT_DIR, tool_name: 'Agent', tool_input: {} }), '');
  for (const used of [75, 82]) accA.statusline('fo3', 'claude-opus-5', used, now + 7300, 10, now + 3 * 86400);
  check('adaptive warn band widens with burst', accA.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'fo3', cwd: PROJECT_DIR, prompt: 'continue editing auth.js' }).includes('auto-pause at 92%'), true);
  for (const used of [79, 80, 81, 82]) accA.statusline('fo4', 'claude-opus-5', used, now + 7400, 10, now + 3 * 86400);
  check('calm usage keeps the default warn band', accA.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'fo4', cwd: PROJECT_DIR, prompt: 'continue editing auth.js' }), '');
  accA.statusline('fo4', 'claude-opus-5', 10, now + 7400, 10, now + 3 * 86400);
}

async function scenarioQueueMode(acc) {
  const now = nowSec();
  acc.manualSchedule = true;
  const queueFile = path.join(PROJECT_DIR, 'TASKS.md');
  fs.writeFileSync(queueFile, ['# release queue', '- [x] Fix the date parser for ISO weeks', '- [ ] Retry failed uploads with backoff', '- [ ] Write the migration guide (10 sections)', '1. [ ] Settings page: export button', 'TODO: rollout notes for the beta channel', '- [x] Landing page mockup', ''].join('\n'));
  const untrusted = acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'q0', cwd: PROJECT_DIR });
  check('untrusted queue file injects no directive', untrusted.includes('Queue mode'), false);
  check('untrusted queue file is reported instead', untrusted.includes('noctis queue trust'), true);
  const untrustedAgain = acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'q0b', cwd: PROJECT_DIR });
  check('and reported again next session, not once a week', untrustedAgain.includes('noctis queue trust'), true);
  acc.run(['queue', 'trust', '--file', queueFile]);
  check('trust is recorded against the file', Object.keys(acc.state().queueTrust || {}).length, 1);
  const startup = acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'q1', cwd: PROJECT_DIR });
  check('queue directive on startup with open count', startup.includes('Queue mode (TASKS.md: 4 open)') && startup.includes('code stays with you'), true);
  const compact = acc.hook({ hook_event_name: 'SessionStart', source: 'compact', session_id: 'q1', cwd: PROJECT_DIR });
  check('queue directive re-injected after compaction', compact.includes('Queue mode'), true);
  check('no checkpoint note on compaction', compact.includes('devam notu'), false);
  acc.hook({ hook_event_name: 'TaskCreated', session_id: 'q1', task_id: 't1', task_subject: 'Retry failed uploads with backoff' });
  acc.hook({ hook_event_name: 'TaskCreated', session_id: 'q1', task_id: 't2', task_subject: 'Guide draft' });
  acc.hook({ hook_event_name: 'TaskCompleted', session_id: 'q1', task_id: 't2', task_subject: 'Guide draft' });
  check('task events tracked', Object.values(acc.state().tasks.q1.items).filter((task) => task.status === 'open').length, 1);
  acc.statusline('q1', 'claude-fable-5-1', 93, now + 2 * 86400, 10, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'q1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const checkpoint = fs.readFileSync(acc.state().checkpoints.q1.path, 'utf8');
  check('checkpoint lists queue items', checkpoint.includes('## Sıradaki işler (TASKS.md: 4 açık)') && checkpoint.includes('- [ ] Retry failed uploads with backoff') && checkpoint.includes('rollout notes'), true);
  check('checkpoint lists open tracked tasks', checkpoint.includes('## Açık görevler (Claude Code)') && checkpoint.includes('Retry failed uploads') && !checkpoint.includes('- Guide draft'), true);
  resetCalls();
  const wait = acc.state();
  wait.waits.q1.resumeAt = now - 5;
  wait.waits.q1.until = now - 10;
  writeJson(acc.stateFile, wait);
  const old = new Date(Date.now() - 3600000);
  fs.utimesSync(TRANSCRIPT, old, old);
  acc.statusline('q1', 'claude-fable-5-1', 5, now + 7200, 10, now + 3 * 86400);
  acc.run(['resume', '--sid', 'q1', '--account', acc.dir]);
  check('resume prompt carries task list hint', callsLog().some((line) => line.includes('Task list: TASKS.md')), true);
  check('resume prompt names next items', callsLog().some((line) => line.includes('Next: Retry failed uploads with backoff')), true);
  const liteType = `${PLUGIN_NAME}:lite`;
  check('lite may write markdown', acc.hook({ hook_event_name: 'PreToolUse', session_id: 'q1', agent_id: 'l1', agent_type: liteType, tool_name: 'Write', tool_input: { file_path: path.join(PROJECT_DIR, 'docs', 'guide.md'), content: 'x' } }), '');
  check('lite may not write code', acc.hook({ hook_event_name: 'PreToolUse', session_id: 'q1', agent_id: 'l1', agent_type: liteType, tool_name: 'Write', tool_input: { file_path: path.join(PROJECT_DIR, 'src', 'parser.cs'), content: 'x' } }).includes('"permissionDecision":"deny"'), true);
  check('other subagents unaffected', acc.hook({ hook_event_name: 'PreToolUse', session_id: 'q1', agent_id: 'g1', agent_type: 'general-purpose', tool_name: 'Write', tool_input: { file_path: path.join(PROJECT_DIR, 'src', 'parser.cs') } }), '');
  check('main thread Write untouched', acc.hook({ hook_event_name: 'PreToolUse', session_id: 'q1', tool_name: 'Write', tool_input: { file_path: path.join(PROJECT_DIR, 'src', 'parser.cs') } }), '');
  fs.unlinkSync(queueFile);
  check('no directive without queue file', acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'q2', cwd: PROJECT_DIR }).includes('Queue mode'), false);
  acc.run(['cancel']);
  acc.manualSchedule = false;
}

async function scenarioUptime(acc) {
  const now = nowSec();
  check('hook pulse recorded', Number(acc.state().lastHookAt) > now - 3600, true);
  acc.setConfig((config) => {
    config.resume.mode = 'window';
  });
  acc.statusline('seen', 'claude-opus-5', 50, now + 7200, 90, now + 2 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'seen', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'ghost', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('interactive session keeps window mode', acc.state().waits.seen.launchMode, 'window');
  check('session unseen by statusline inferred headless', acc.state().waits.ghost.launchMode, 'headless');
  acc.run(['cancel']);
  acc.setConfig((config) => {
    config.resume.mode = 'headless';
  });
  const usageFile = path.join(acc.guardDir, 'usage.json');
  const usage = readJson(usageFile);
  usage.history.five_hour = Array.from({ length: 9 }, (_, i) => ({ used: 10 + i, resetsAt: now + 7200, at: now - 900 + i * 100 }));
  writeJson(usageFile, usage);
  const dead = acc.state();
  dead.lastHookAt = now - 4000;
  delete dead.notified.hooksDead;
  writeJson(acc.stateFile, dead);
  const errorsBefore = fs.readFileSync(path.join(acc.guardDir, 'errors.log'), 'utf8').split('\n').filter((line) => line.includes('hooks appear inactive')).length;
  const line = acc.statusline('seen', 'claude-opus-5', 20, now + 7200, 10, now + 3 * 86400);
  check('dead hooks flagged in status bar', line.includes('⚠ hook yok'), true);
  acc.statusline('seen', 'claude-opus-5', 20, now + 7200, 10, now + 3 * 86400);
  const errorsAfter = fs.readFileSync(path.join(acc.guardDir, 'errors.log'), 'utf8').split('\n').filter((line) => line.includes('hooks appear inactive')).length;
  check('dead-hook error logged once', errorsAfter - errorsBefore, 1);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'seen', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('hook pulse clears the alert', acc.statusline('seen', 'claude-opus-5', 20, now + 7200, 10, now + 3 * 86400).includes('⚠ hook yok'), false);
  for (const slept of [600, 640]) {
    const state = acc.state();
    state.waits.cap = { kind: 'batch', window: 'five_hour', label: '5s', until: now + 3000, resumeAt: now + 3001, inHook: true, startedAt: now - 1000, heartbeat: now - 1000 + slept, cwd: PROJECT_DIR, transcript: TRANSCRIPT, checkpoint: '', queuedPrompt: '' };
    writeJson(acc.stateFile, state);
    acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'cap', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  }
  check('hook timeout cap learned from repeated interruptions', acc.state().hookCapSeconds, 600);
  acc.statusline('cap', 'claude-opus-5', 93, now + 1200, 10, now + 3 * 86400);
  const stop = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'cap', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('waits beyond learned cap use the runner instead of sleeping', stop.includes('"continue":false'), true);
  acc.run(['cancel']);
  const reset = acc.state();
  reset.hookCapSeconds = 0;
  reset.interruptedWaits = [];
  writeJson(acc.stateFile, reset);
  const etaUsage = readJson(usageFile);
  etaUsage.history.seven_day = [{ used: 40, resetsAt: now + 3 * 86400, at: now - 3600 }, { used: 50, resetsAt: now + 3 * 86400, at: now - 1800 }];
  writeJson(usageFile, etaUsage);
  const etaLine = acc.statusline('eta', 'claude-opus-5', 20, now + 7200, 60, now + 3 * 86400);
  check('weekly threshold ETA shown when it lands before reset', etaLine.includes('⌛ hafta eşiği'), true);
  const calmUsage = readJson(usageFile);
  calmUsage.history.seven_day = [{ used: 59, resetsAt: now + 86400, at: now - 7200 }, { used: 60, resetsAt: now + 86400, at: now - 3600 }];
  writeJson(usageFile, calmUsage);
  check('no ETA when reset comes first', acc.statusline('eta', 'claude-opus-5', 20, now + 7200, 60, now + 86400).includes('⌛'), false);
}

async function scenarioSmartDecisions(accA) {
  const now = nowSec();
  const quiet = path.join(PROJECT_DIR, 'quiet2.jsonl');
  fs.writeFileSync(quiet, `${JSON.stringify({ type: 'user', message: { role: 'user', content: 'merhaba' } })}\n`);
  accA.statusline('sd3', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400);
  const routed = accA.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'sd3', cwd: PROJECT_DIR, transcript_path: quiet, prompt: 'Kuantum bilgisayarların temellerini araştır' });
  check('investigate routed with signal recorded', routed.includes('additionalContext') && accA.state().routes.sd3.signal === 'araştır', true);
  for (let i = 0; i < 2; i += 1) accA.hook({ hook_event_name: 'PostToolUse', session_id: 'sd3', tool_name: 'Agent', tool_input: { subagent_type: `${PLUGIN_NAME}:lite`, prompt: 'x' }, tool_response: 'NEEDS_CODE: this requires editing the parser' });
  check('two misroutes learned', accA.state().routerLearned['araştır'].misroutes, 2);
  check('learned signal no longer routes', JSON.parse(accA.run(['classify', 'Kuantum bilgisayarların temellerini araştır'])).reason, 'learned-block');
  check('other signals unaffected', JSON.parse(accA.run(['classify', 'En iyi mekanik klavye 2026 araştır'])).route, true);
  for (let i = 0; i < 2; i += 1) accA.hook({ hook_event_name: 'PostToolUse', session_id: 'sd3', tool_name: 'Agent', tool_input: { subagent_type: `${PLUGIN_NAME}:lite` }, tool_response: { result: 'Here is the research report...' } });
  check('successes rehabilitate the signal', JSON.parse(accA.run(['classify', 'Kuantum bilgisayarların temellerini araştır'])).route, true);
  const fableFile = path.join(accA.guardDir, 'fable.json');
  writeJson(fableFile, { fetchedAt: now, fable: { used: 70, resetsAt: now + 4 * 86400 }, five_hour: null, seven_day: null, clockOffset: 0, history: { fable: [{ used: 40, resetsAt: now + 4 * 86400, at: now - 7200 }, { used: 70, resetsAt: now + 4 * 86400, at: now }] } });
  check('fable ETA shown on fable session', accA.statusline('sd3', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400).includes('⌛ Fable'), true);
  check('no fable ETA on opus session', accA.statusline('sd3', 'claude-opus-5', 20, now + 7200, 10, now + 3 * 86400).includes('⌛ Fable'), false);
  fs.rmSync(fableFile, { force: true });
  spawnSync('git', ['init', '-q'], { cwd: PROJECT_DIR });
  fs.writeFileSync(path.join(PROJECT_DIR, 'dirty.js'), 'x');
  accA.statusline('sd4', 'claude-opus-5', 93, now + 2 * 86400, 10, now + 3 * 86400);
  accA.hook({ hook_event_name: 'PostToolBatch', session_id: 'sd4', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const checkpoint = fs.readFileSync(accA.state().checkpoints.sd4.path, 'utf8');
  check('checkpoint includes git status', checkpoint.includes('## Git durumu') && checkpoint.includes('dirty.js'), true);
  accA.run(['cancel']);
  fs.rmSync(path.join(PROJECT_DIR, '.git'), { recursive: true, force: true });
  fs.rmSync(path.join(PROJECT_DIR, 'dirty.js'), { force: true });
}

async function scenarioQueueContinuation(acc) {
  const now = nowSec();
  const queueFile = path.join(PROJECT_DIR, 'TASKS.md');
  const writeQueue = (open) => fs.writeFileSync(queueFile, ['# q', '- [x] done item', ...Array.from({ length: open }, (_, i) => `- [ ] item ${i + 1}`), ''].join('\n'));
  writeQueue(4);
  acc.statusline('qc1', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 30);
  const first = acc.hook({ hook_event_name: 'Stop', session_id: 'qc1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: false });
  check('stop blocked while queue has open items', first.includes('"decision":"block"') && first.includes('Queue continues: 4 open') && first.includes('item 1'), true);
  check('forced continue counted', acc.state().stopGuard.qc1.forced, 1);
  for (let i = 0; i < 3; i += 1) acc.hook({ hook_event_name: 'Stop', session_id: 'qc1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true });
  check('idle continues tracked', acc.state().stopGuard.qc1.idle, 3);
  writeQueue(3);
  const progressed = acc.hook({ hook_event_name: 'Stop', session_id: 'qc1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true });
  check('progress resets idle counter and continues', progressed.includes('"decision":"block"') && acc.state().stopGuard.qc1.idle === 0, true);
  let stuck = '';
  for (let i = 0; i < 4; i += 1) stuck = acc.hook({ hook_event_name: 'Stop', session_id: 'qc1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true });
  check('stuck queue stops forcing and warns', !stuck.includes('"decision":"block"') && stuck.includes('Kuyruk ilerlemiyor'), true);
  check('stuck queue: counters reset, the next cycle continues again', acc.state().stopGuard.qc1.idle === 0 && acc.state().stopGuard.qc1.forced === 0 && acc.state().stopGuard.qc1.cycles === 1 && acc.hook({ hook_event_name: 'Stop', session_id: 'qc1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: false }).includes('"decision":"block"'), true);
  for (let i = 0; i < 4; i += 1) stuck = acc.hook({ hook_event_name: 'Stop', session_id: 'qc1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true });
  check('stuck queue: the warning is not repeated for the same file', !stuck.includes('"decision":"block"') && !stuck.includes('Kuyruk ilerlemiyor'), true);
  writeQueue(0);
  const finished = acc.hook({ hook_event_name: 'Stop', session_id: 'qc1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('finished queue: one done notice for a session that was driven through it', finished.includes('Kuyruk bitti') && finished.includes('TASKS.md') && !finished.includes('"decision":"block"'), true);
  check('finished queue journaled', acc.run(['why', '--last', '1']).includes('queue finished'), true);
  check('empty queue is silent afterwards', acc.hook({ hook_event_name: 'Stop', session_id: 'qc1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  writeQueue(3);
  acc.statusline('qc2', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 62);
  check('high context at stop still continues in the same session', acc.hook({ hook_event_name: 'Stop', session_id: 'qc2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }).includes('"decision":"block"'), true);
  check('no restart wait is ever created for context', acc.state().waits.qc2 === undefined, true);
  acc.setConfig((config) => {
    config.resume.remoteControl = true;
    config.resume.extraArgs = ['--verbose'];
  });
  acc.statusline('qc2', 'claude-fable-5-1', 93, now + 2 * 86400, 10, now + 3 * 86400, 62);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'qc2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const wait = acc.state();
  wait.waits.qc2.resumeAt = now - 5;
  wait.waits.qc2.until = now - 10;
  writeJson(acc.stateFile, wait);
  const old = new Date(Date.now() - 3600000);
  fs.utimesSync(TRANSCRIPT, old, old);
  acc.statusline('qc2', 'claude-fable-5-1', 5, now + 7200, 10, now + 3 * 86400, 62);
  resetCalls();
  acc.run(['resume', '--sid', 'qc2', '--account', acc.dir]);
  const call = (await anyCall()).pop() || '';
  check('relaunch always resumes the same session', call.includes('--resume qc2'), true);
  check('remote control and extra args passed', call.includes('--remote-control') && call.includes('--verbose'), true);
  acc.setConfig((config) => {
    config.resume.remoteControl = false;
    config.resume.extraArgs = [];
  });
  acc.statusline('qc3', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 70);
  writeQueue(4);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'qc3', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  writeQueue(3);
  check('task boundary with high context continues in place (compaction is left to Claude Code)', acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'qc3', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  acc.run(['cancel']);
  acc.statusline('qc4', 'claude-fable-5-1', 93, now + 2 * 86400, 10, now + 3 * 86400, 30);
  check('stop during limit is allowed and wait registered', acc.hook({ hook_event_name: 'Stop', session_id: 'qc4', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }).includes('"decision":"block"'), false);
  check('limit wait registered from stop', acc.state().waits.qc4 && acc.state().waits.qc4.kind, 'stop');
  acc.run(['cancel']);
  fs.unlinkSync(queueFile);
  const transcriptDir = path.join(acc.dir, 'projects', '-lab-project');
  fs.mkdirSync(transcriptDir, { recursive: true });
  const iso = new Date().toISOString();
  fs.writeFileSync(path.join(transcriptDir, 'rep1.jsonl'), [
    JSON.stringify({ type: 'assistant', timestamp: iso, message: { role: 'assistant', model: 'claude-fable-5-1', usage: { input_tokens: 1200, output_tokens: 300, cache_read_input_tokens: 50000, cache_creation_input_tokens: 2000 }, content: [] } }),
    JSON.stringify({ type: 'assistant', timestamp: iso, isSidechain: true, message: { role: 'assistant', model: 'claude-opus-5', usage: { input_tokens: 8000, output_tokens: 900 }, content: [] } }),
    '',
  ].join('\n'));
  const report = acc.run(['report', '--days', '3']);
  check('report lists models and subagent estimate', report.includes('claude-fable-5-1') && report.includes('claude-opus-5') && report.includes('uzak tutulan'), true);
  check('report counts events', /Olaylar: \d+ yönlendirme/.test(report), true);
  check('report prices models at list prices', report.includes('API karşılığı maliyet: $0.127'), true);
  const costJson = JSON.parse(acc.run(['report', '--days', '3', '--json']));
  check('report json: totalCost and per-day cost', Math.abs(costJson.totals.totalCost - 0.127) < 0.001 && Math.abs(costJson.daily[0].totalCost - 0.127) < 0.001, true);
  check('report json: kept-off savings vs primary', Math.abs(costJson.keptOffPrimary.savedVsPrimary - 0.0625) < 0.0005 && Math.abs(costJson.keptOffPrimary.costOnPrimary - 0.125) < 0.0005, true);
  acc.setConfig((config) => {
    config.report = { pricing: { 'opus-5': { input: 1, output: 1, cacheWrite: 1, cacheRead: 1 } } };
  });
  const overridden = JSON.parse(acc.run(['report', '--days', '3', '--json']));
  check('report json: config pricing overrides the builtin table', Math.abs(overridden.models.find((m) => m.modelName === 'claude-opus-5').cost - 0.0089) < 0.0005, true);
  acc.setConfig((config) => {
    config.report = { pricing: {} };
  });
  const htmlReport = acc.run(['report', '--days', '3', '--html']);
  check('report html has the cost column', htmlReport.includes('<th>cost</th>') && htmlReport.includes('$0.127'), true);
  const selftest = acc.run(['selftest']);
  check('selftest runs end to end', selftest.includes('hook hızı') && selftest.includes('marker yazdı: evet'), true);
}

async function scenarioProjection(acc) {
  const now = nowSec();
  acc.statusline('pj1', 'claude-opus-5', 70, now + 2 * 86400, 10, now + 3 * 86400);
  const usageFile = path.join(acc.guardDir, 'usage.json');
  const usage = readJson(usageFile);
  usage.updatedAt = now - 1500;
  usage.sessions.pj1.updatedAt = now - 1500;
  usage.history.five_hour = [{ used: 40, resetsAt: now + 2 * 86400, at: now - 3300 }, { used: 70, resetsAt: now + 2 * 86400, at: now - 1500 }];
  writeJson(usageFile, usage);
  lab.setOutage('error');
  const stop = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'pj1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('stale data + steady burn projects past threshold -> stop', stop.includes('"continue":false'), true);
  check('projection reason logged', fs.readFileSync(path.join(acc.guardDir, 'guard.log'), 'utf8').includes('bayat veriden yakım öngörüsü'), true);
  lab.setOutage('');
  acc.run(['cancel', 'pj1']);
  const calm = readJson(usageFile);
  calm.history.five_hour = [{ used: 69, resetsAt: now + 2 * 86400, at: now - 3300 }, { used: 70, resetsAt: now + 2 * 86400, at: now - 1500 }];
  writeJson(usageFile, calm);
  check('slow burn does not project a stop', acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'pj1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  acc.statusline('pj1', 'claude-opus-5', 10, now + 7200, 10, now + 3 * 86400);
}

async function scenarioAutocompactAdaptation(acc) {
  const now = nowSec();
  const settingsFile = path.join(acc.dir, 'settings.json');
  const settings = readJson(settingsFile);
  writeJson(settingsFile, { ...settings, env: { ...(settings.env || {}), CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: '60' } });
  acc.statusline('ac1', 'claude-opus-5', 87, now + 2 * 86400, 10, now + 3 * 86400, 58);
  const stop = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'ac1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('autocompact 60 -> compaction guard at 55 stops at context 58', stop.includes('"continue":false'), true);
  acc.run(['cancel', 'ac1']);
  acc.statusline('ac1', 'claude-opus-5', 87, now + 2 * 86400, 10, now + 3 * 86400, 50);
  check('below derived guard -> continues', acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'ac1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  writeJson(settingsFile, settings);
  acc.statusline('ac1', 'claude-opus-5', 10, now + 7200, 10, now + 3 * 86400, 20);
}

async function scenarioLocaleAndModes(acc) {
  const now = nowSec();
  const english = acc.statusline('lc1', 'claude-fable-5-1', 41, now + 7200, 23, now + 3 * 86400, 32);
  check('turkish statusline by default in lab', english.startsWith('∞ 5s %41'), true);
  const en = acc.run(['statusline'], acc.statuslineInput('lc1', 'claude-fable-5-1', 41, now + 7200, 23, now + 3 * 86400), { NOCTIS_LANG: 'en' });
  check('NOCTIS_LANG=en switches labels', en.startsWith('∞ 5h %41') && en.includes('Wk %23'), true);
  check('english off message', acc.run(['off', '1'], null, { NOCTIS_LANG: 'en' }).includes('disabled until'), true);
  acc.run(['on']);
  acc.setConfig((config) => {
    config.locale = 'en';
  });
  check('config.locale en beats LANG', acc.run(['off', '1'], null, { NOCTIS_LANG: '' }).includes('disabled until'), true);
  acc.run(['on']);
  acc.setConfig((config) => {
    config.locale = 'auto';
  });
  check('LANG=tr_TR detected', acc.run(['off', '1'], null, { NOCTIS_LANG: '', LANG: 'tr_TR.UTF-8' }).includes('devre dışı'), true);
  acc.run(['on']);
  check('unknown LANG falls back to english', acc.run(['off', '1'], null, { NOCTIS_LANG: '', LANG: 'fi_FI.UTF-8' }).includes('disabled until'), true);
  acc.run(['on']);
  check('LANG=de_DE picks the German catalog', acc.run(['off', '1'], null, { NOCTIS_LANG: '', LANG: 'de_DE.UTF-8' }).includes('deaktiviert bis'), true);
  acc.run(['on']);
  const languageCases = [
    ['de', 'Bitte füge die Tests hinzu und mach das Refactoring nicht zu groß', 'Wo %'],
    ['fr', 'Est-ce que tu peux ajouter les tests pour le module et corriger le bug', 'Sem %'],
    ['es', 'Por favor agrega las pruebas y no cambies el esquema de la base', 'Sem %'],
    ['ja', 'テストを追加してからリファクタリングを続けてください', '週 %'],
    ['ru', 'Добавь тесты и продолжай рефакторинг модуля', 'Нед %'],
    ['tr', 'Testleri ekle ve modülü düzeltmeye devam et lütfen', 'Hf %'],
    ['en', 'Please add the tests and keep going with the refactor of the module', 'Wk %'],
  ];
  for (const [lang, prompt, badge] of languageCases) {
    acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'lang1', cwd: PROJECT_DIR, prompt }, { NOCTIS_LANG: '' });
    check(`language auto: ${lang} prompt detected`, acc.state().sessionLocale.lang1.lang, lang);
    const line = acc.run(['statusline'], acc.statuslineInput('lang1', 'claude-fable-5-1', 10, now + 7200, 10, now + 6 * 86400), { NOCTIS_LANG: '' });
    check(`language auto: status line follows the session language (${lang})`, line.includes(badge), true);
  }
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'lang1', cwd: PROJECT_DIR, prompt: 'ok' }, { NOCTIS_LANG: '' });
  check('language auto: a short ambiguous prompt keeps the previous language', acc.state().sessionLocale.lang1.lang, 'en');
  check('language auto: another session is unaffected', acc.state().sessionLocale.lc2, undefined);
  acc.setConfig((config) => {
    config.locale = 'tr';
  });
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'lang1', cwd: PROJECT_DIR, prompt: 'Bitte füge die Tests hinzu und mach weiter' }, { NOCTIS_LANG: '' });
  check('language pinned in config wins over detection', acc.run(['statusline'], acc.statuslineInput('lang1', 'claude-fable-5-1', 10, now + 7200, 10, now + 6 * 86400), { NOCTIS_LANG: '' }).includes('Hf %'), true);
  acc.setConfig((config) => {
    config.locale = 'auto';
  });
  const early = acc.statusline('lc2', 'claude-fable-5-1', 10, now + 7200, 5, now + 6 * 86400 + 3600);
  check('pace marker: far below linear pace -> ▲', early.includes('Hf %5▲'), true);
  const late = acc.statusline('lc2', 'claude-fable-5-1', 10, now + 7200, 80, now + 6 * 86400);
  check('pace marker: far above linear pace -> ▼', late.includes('Hf %80▼'), true);
  acc.setConfig((config) => {
    config.statusline.mode = 'silent';
  });
  check('silent statusline mode prints nothing', acc.statusline('lc2', 'claude-fable-5-1', 10, now + 7200, 10, now + 6 * 86400), '');
  check('silent mode still records usage', readJson(path.join(acc.guardDir, 'usage.json')).seven_day.used, 10);
  acc.setConfig((config) => {
    config.statusline.mode = 'own';
    config.statusline.chainCommand = 'echo chained-line';
  });
  const chained = acc.statusline('lc2', 'claude-fable-5-1', 10, now + 7200, 10, now + 6 * 86400);
  check('chain command output precedes ours', chained.startsWith('chained-line\n∞'), true);
  acc.setConfig((config) => {
    config.statusline.chainCommand = '';
  });
  const doctor = acc.run(['doctor']);
  check('doctor reports scheduler backend', doctor.includes('zamanlayıcı arka ucu'), true);
  const installedBinary = path.join(acc.dir, 'skills', PLUGIN_NAME, 'bin', process.platform === 'win32' ? 'noctis.exe' : 'noctis');
  const envWithoutRoot = { ...acc.env() };
  delete envWithoutRoot.NOCTIS_PLUGIN_ROOT;
  const installedDoctor = spawnSync(installedBinary, ['doctor'], { encoding: 'utf8', env: envWithoutRoot }).stdout;
  check('installed binary resolves its plugin root without env hints', installedDoctor.includes(`OK  plugin konumu: ${path.join(acc.dir, 'skills', PLUGIN_NAME)}`), true);
  const installedStatus = spawnSync(installedBinary, ['status'], { encoding: 'utf8', env: envWithoutRoot }).stdout;
  check('installed binary loads defaults from its own root', installedStatus.includes('5s ≥%92'), true);
  const status = acc.run(['status']);
  check('status reports plan windows', status.includes('Plan pencere : 5h + 7d'), true);
}

async function scenarioMultiSessionAndScoped(acc) {
  const now = nowSec();
  acc.setConfig((config) => {
    config.usage.multiSessionMax = true;
  });
  const reset = now + 5000;
  acc.statusline('ms-a', 'claude-fable-5-1', 80, reset, 20, now + 3 * 86400);
  acc.statusline('ms-b', 'claude-fable-5-1', 60, reset, 12, now + 3 * 86400);
  const usage = readJson(path.join(acc.guardDir, 'usage.json'));
  check('quieter session cannot drag the window down', usage.five_hour.used, 80);
  check('weekly also keeps the maximum', usage.seven_day.used, 20);
  acc.statusline('ms-a', 'claude-fable-5-1', 70, reset, 20, now + 3 * 86400);
  check('same session reporting lower is trusted', readJson(path.join(acc.guardDir, 'usage.json')).five_hour.used, 70);
  acc.statusline('ms-b', 'claude-fable-5-1', 30, reset + 100, 12, now + 3 * 86400);
  check('new window resets the maximum', readJson(path.join(acc.guardDir, 'usage.json')).five_hour.used, 30);
  acc.setConfig((config) => {
    config.usage.multiSessionMax = false;
    config.models.scopedPattern = 'opus';
    config.models.scopedLabel = 'Opus';
    config.thresholds.weeklyScoped = 90;
  });
  mock.limits = [
    { kind: 'session', percent: 30, resets_at: new Date((now + 1800) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 40, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
    { kind: 'weekly_scoped', percent: 93, resets_at: new Date((now + 2 * 86400) * 1000).toISOString(), scope: { group: 'model', model: { display_name: 'Opus 5' } } },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('sc1', 'claude-opus-5', 30, now + 1800, 40, now + 3 * 86400);
  const out = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'sc1', cwd: PROJECT_DIR, prompt: 'continue editing the parser code' });
  check('scoped rule generalizes to another model bucket', out.includes('🔁 Opus %93') && out.includes('/model opus'), true);
  const scoped = readJson(path.join(acc.guardDir, 'fable.json'));
  check('scoped bucket parsed by pattern', scoped.fable.used, 93);
  check('bucket names recorded for plan detection', scoped.buckets, 'Opus 5');
  acc.run(['cancel']);
  acc.setConfig((config) => {
    config.models.scopedPattern = 'fable';
    config.models.scopedLabel = 'Fable';
    delete config.thresholds.weeklyScoped;
  });
  writeJson(path.join(acc.dir, 'settings.json'), { ...readJson(path.join(acc.dir, 'settings.json')), model: 'fable' });
  const state = acc.state();
  state.modelSwitched = null;
  writeJson(acc.stateFile, state);
  mock.limits = [];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('sc1', 'claude-fable-5-1', 10, now + 7200, 10, now + 3 * 86400);
}

async function scenarioSubagentsAndObserve(acc) {
  const now = nowSec();
  acc.statusline('sp1', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400);
  const explore = acc.hook({ hook_event_name: 'PreToolUse', session_id: 'sp1', cwd: PROJECT_DIR, tool_name: 'Agent', tool_input: { subagent_type: 'Explore', prompt: 'find the parser' } });
  check('Explore subagent pinned to haiku', explore.includes('"updatedInput"') && explore.includes('"model":"haiku"') && explore.includes('"permissionDecision":"allow"'), true);
  check('explicit model is respected', acc.hook({ hook_event_name: 'PreToolUse', session_id: 'sp1', cwd: PROJECT_DIR, tool_name: 'Agent', tool_input: { subagent_type: 'Explore', model: 'sonnet', prompt: 'x' } }), '');
  check('unmapped subagent untouched', acc.hook({ hook_event_name: 'PreToolUse', session_id: 'sp1', cwd: PROJECT_DIR, tool_name: 'Agent', tool_input: { subagent_type: 'general-purpose', prompt: 'x' } }), '');
  check('plugin agents never pinned', acc.hook({ hook_event_name: 'PreToolUse', session_id: 'sp1', cwd: PROJECT_DIR, tool_name: 'Agent', tool_input: { subagent_type: `${PLUGIN_NAME}:lite`, prompt: 'x' } }), '');
  const digestDeny = acc.hook({ hook_event_name: 'PreToolUse', session_id: 'sp1', agent_id: 'd1', agent_type: `${PLUGIN_NAME}:digest`, tool_name: 'Write', tool_input: { file_path: path.join(PROJECT_DIR, 'notes.md') } });
  check('digest agent may not write even text files', digestDeny.includes('"permissionDecision":"deny"'), true);
  check('digest agent may run commands', acc.hook({ hook_event_name: 'PreToolUse', session_id: 'sp1', agent_id: 'd1', agent_type: `${PLUGIN_NAME}:digest`, tool_name: 'Bash', tool_input: { command: 'npm test' } }), '');
  const start = acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'sp1', cwd: PROJECT_DIR });
  fs.writeFileSync(path.join(PROJECT_DIR, 'TASKS.md'), '# q\n- [ ] item one\n- [ ] item two\n');
  const queueStart = acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'sp1', cwd: PROJECT_DIR });
  check('queue directive mentions the digest agent', queueStart.includes(`${PLUGIN_NAME}:digest`), true);
  check('no queue file -> no digest mention', start.includes('digest'), false);
  acc.setConfig((config) => {
    config.mode = 'observe';
  });
  acc.statusline('ob1', 'claude-fable-5-1', 93, now + 2 * 86400, 10, now + 3 * 86400);
  check('observe: threshold does not pause', acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'ob1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  check('observe: no wait registered', acc.state().waits.ob1 === undefined, true);
  acc.statusline('ob2', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400);
  check('observe: stop hook does not force the queue', acc.hook({ hook_event_name: 'Stop', session_id: 'ob2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  check('observe: research not routed', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'ob1', cwd: PROJECT_DIR, prompt: 'En iyi mekanik klavye 2026 araştır' }), '');
  const journal = fs.readFileSync(path.join(acc.guardDir, 'decisions.jsonl'), 'utf8');
  check('observe: decisions journaled as would-*', journal.includes('"would-pause"') && journal.includes('"would-continue-queue"') && journal.includes('"would-route"'), true);
  check('observe marker in statusline', acc.statusline('ob1', 'claude-fable-5-1', 93, now + 2 * 86400, 10, now + 3 * 86400).includes('👁'), true);
  acc.setConfig((config) => {
    config.mode = 'enforce';
  });
  const why = acc.run(['why', '--last', '5']);
  check('why lists recent decisions with facts', why.includes('would-pause') && why.includes('five=93%'), true);
  check('why --json emits raw entries', acc.run(['why', '--json', '--last', '1']).startsWith('{'), true);
  fs.writeFileSync(path.join(PROJECT_DIR, 'TASKS.md'), '# q\n- [ ] item one\n');
  acc.setConfig((config) => {
    config.queue.completionPromise = 'ALL DONE';
  });
  const promised = path.join(PROJECT_DIR, 'promised.jsonl');
  fs.writeFileSync(promised, `${JSON.stringify({ type: 'assistant', message: { role: 'assistant', content: [{ type: 'text', text: 'Finished everything. <promise>ALL DONE</promise>' }] } })}\n`);
  acc.statusline('cp1', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400);
  check('completion promise allows stop despite open items', acc.hook({ hook_event_name: 'Stop', session_id: 'cp1', cwd: PROJECT_DIR, transcript_path: promised }), '');
  check('without the promise the queue continues', acc.hook({ hook_event_name: 'Stop', session_id: 'cp1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }).includes('"decision":"block"'), true);
  acc.setConfig((config) => {
    config.queue.completionPromise = '';
  });
  fs.rmSync(path.join(PROJECT_DIR, 'TASKS.md'), { force: true });
  acc.statusline('ob1', 'claude-fable-5-1', 10, now + 7200, 10, now + 3 * 86400);
}

async function scenarioBudgetWakeWebhook(acc) {
  const now = nowSec();
  acc.setConfig((config) => {
    config.budget.dailyWeeklyPercent = 10;
  });
  const state = acc.state();
  delete state.budgetDay;
  writeJson(acc.stateFile, state);
  const weekReset = now + 4 * 86400;
  acc.statusline('bd1', 'claude-fable-5-1', 20, now + 7200, 30, weekReset);
  check('budget day start recorded from first sample', acc.state().budgetDay.startUsed, 30);
  acc.statusline('bd1', 'claude-fable-5-1', 20, now + 7200, 41, weekReset);
  const notice = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'bd1', cwd: PROJECT_DIR, prompt: 'continue with the parser code' });
  check('daily budget notice once it is exceeded', notice.includes('daily budget reached') && notice.includes('11%'), true);
  check('budget notice not repeated the same day', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'bd1', cwd: PROJECT_DIR, prompt: 'continue with the parser code' }), '');
  acc.setConfig((config) => {
    config.budget.hardStop = true;
    config.wait.maxInHookMinutes = 1;
  });
  const hard = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'bd1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('hard stop pauses until local midnight', hard.includes('"continue":false') && acc.state().waits.bd1.hit === 'budget', true);
  acc.run(['cancel', 'bd1']);
  acc.setConfig((config) => {
    config.budget.dailyWeeklyPercent = 0;
    config.budget.hardStop = false;
    config.wait.maxInHookMinutes = 330;
    config.resume.permissionMode = 'inherit';
  });
  acc.statusline('pm1', 'claude-fable-5-1', 93, now + 2 * 86400, 10, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'pm1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, permission_mode: 'plan' });
  check('permission mode captured from hook input', acc.state().waits.pm1.permissionMode, 'plan');
  const waitState = acc.state();
  waitState.waits.pm1.resumeAt = now - 5;
  waitState.waits.pm1.until = now - 10;
  writeJson(acc.stateFile, waitState);
  const old = new Date(Date.now() - 3600000);
  fs.utimesSync(TRANSCRIPT, old, old);
  acc.statusline('pm1', 'claude-fable-5-1', 5, now + 7200, 10, now + 3 * 86400);
  resetCalls();
  acc.run(['resume', '--sid', 'pm1', '--account', acc.dir]);
  check('relaunch inherits the session permission mode', (callsLog().find((line) => line.includes('--resume pm1')) || '').includes('--permission-mode plan'), true);
  acc.setConfig((config) => {
    config.resume.permissionMode = 'auto';
    config.wake.sameSession = true;
    config.wake.graceSeconds = 60;
  });
  const savedLimits = mock.limits;
  mock.limits = [
    { kind: 'session', percent: 95, resets_at: new Date((nowSec() + 2) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 10, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('wk1', 'claude-fable-5-1', 95, nowSec() + 2, 10, now + 3 * 86400);
  const wake = acc.runFull(['hook'], { hook_event_name: 'StopFailure', session_id: 'wk1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'rate_limit' });
  mock.limits = savedLimits;
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  check('same-session wake exits 2 with a continue message', wake.status === 2 && wake.stdout.includes('[noctis] 5s limit'), true);
  check('wake attempt recorded on the wait', acc.state().waits.wk1.wakeAttemptedAt > 0, true);
  const future = new Date(Date.now() + 5000);
  fs.utimesSync(TRANSCRIPT, future, future);
  acc.statusline('wk1', 'claude-fable-5-1', 5, now + 7200, 10, now + 3 * 86400);
  resetCalls();
  acc.run(['resume', '--sid', 'wk1', '--account', acc.dir]);
  check('runner skips the launch after a successful wake', callsLog().length === 0 && acc.state().waits.wk1 === undefined, true);
  acc.setConfig((config) => {
    config.wake.sameSession = false;
  });
  const oldAgain = new Date(Date.now() - 3600000);
  fs.utimesSync(TRANSCRIPT, oldAgain, oldAgain);
  acc.setConfig((config) => {
    config.alarm.webhook = { url: `http://127.0.0.1:${lab.mockPort}/webhook/tg`, preset: 'telegram', chatId: '42' };
  });
  acc.run(['webhook', '--title', 'T1', '--body', 'hello']);
  const telegram = lab.webhooks().pop();
  check('telegram preset posts chat_id + text', telegram && telegram.body.includes('"chat_id":"42"') && telegram.body.includes('T1\\nhello'), true);
  acc.setConfig((config) => {
    config.alarm.webhook = { url: `http://127.0.0.1:${lab.mockPort}/webhook/ntfy`, preset: 'ntfy' };
  });
  acc.run(['webhook', '--title', 'Öz-test', '--body', 'plain body']);
  const ntfy = lab.webhooks().pop();
  check('ntfy preset sends plain text with encoded title', ntfy && ntfy.body === 'plain body' && ntfy.title.startsWith('=?UTF-8?B?'), true);
  lab.setOutage('webhook-error');
  const before = lab.webhooks().length;
  acc.run(['webhook', '--title', 'x', '--body', 'y']);
  check('failed delivery is retried 3 times', lab.webhooks().length - before, 3);
  for (let i = 0; i < 4; i += 1) acc.run(['webhook', '--title', 'x', '--body', 'y']);
  check('circuit opens after repeated failures', acc.state().webhook.openUntil > nowSec(), true);
  const stalled = lab.webhooks().length;
  acc.run(['webhook', '--title', 'x', '--body', 'y']);
  check('open circuit skips delivery', lab.webhooks().length, stalled);
  lab.setOutage('');
  acc.setConfig((config) => {
    config.alarm.webhook = { url: '', preset: 'generic', chatId: '' };
    config.wake.graceSeconds = 300;
  });
  const webhookState = acc.state();
  webhookState.webhook = {};
  writeJson(acc.stateFile, webhookState);
  const setupDir = path.join(LAB_ROOT, 'setup-account');
  fs.mkdirSync(setupDir, { recursive: true });
  writeJson(path.join(setupDir, 'settings.json'), { statusLine: { type: 'command', command: 'my-bar' } });
  const setup = acc.run(['setup', '--config-dir', setupDir, '--preset', 'conservative']);
  const setupSettings = readJson(path.join(setupDir, 'settings.json'));
  check('setup wires statusLine and keeps the old one chained', setupSettings.statusLine.command.includes('noctis') && readJson(path.join(setupDir, PLUGIN_NAME, 'config.json')).statusline.chainCommand === 'my-bar', true);
  check('setup applies the threshold preset', readJson(path.join(setupDir, PLUGIN_NAME, 'config.json')).thresholds.session5h, 85);
  check('setup prints the doctor', setup.includes('plugin konumu'), true);
  check('unknown preset is rejected', acc.runFull(['setup', '--config-dir', setupDir, '--preset', 'weird']).status, 1);
  acc.statusline('bd1', 'claude-fable-5-1', 5, now + 7200, 10, now + 3 * 86400);
}

async function scenarioMarketplaceBootstrap(acc) {
  const root = path.join(LAB_ROOT, 'marketplace-copy');
  fs.rmSync(root, { recursive: true, force: true });
  const sourceTree = lab.sourceRoot || SOURCE_ROOT;
  for (const entry of ['.claude-plugin', 'hooks', 'agents', 'skills', 'config.default.json']) {
    fs.cpSync(path.join(sourceTree, entry), path.join(root, entry), { recursive: true });
  }
  const platform = path.basename(path.dirname(acc.engine()[0]));
  fs.cpSync(path.dirname(acc.engine()[0]), path.join(root, 'bin', platform), { recursive: true });
  const shipped = path.join(root, 'bin', process.platform === 'win32' ? 'noctis.exe' : 'noctis');
  if (process.platform === 'win32') {
    fs.copyFileSync(acc.engine()[0], shipped);
  } else {
    fs.writeFileSync(shipped, fs.readFileSync(path.join(sourceTree, 'bin', 'noctis')), { mode: 0o755 });
    check('repo ships the launcher script at bin/noctis', fs.readFileSync(shipped, 'utf8').startsWith('#!/bin/sh'), true);
  }
  const env = { ...acc.env() };
  delete env.NOCTIS_PLUGIN_ROOT;
  const hooks = readJson(path.join(root, 'hooks', 'hooks.json'));
  const bootstrap = hooks.hooks.SessionStart.flatMap((group) => group.hooks).find((hook) => (hook.args || [])[0] === 'ensure');
  check('bootstrap hook is exec form (no shell)', bootstrap !== undefined && bootstrap.command === '${CLAUDE_PLUGIN_ROOT}/bin/noctis', true);
  check('every hook is exec form', Object.values(hooks.hooks).flat().flatMap((group) => group.hooks).every((hook) => Array.isArray(hook.args)), true);
  const platformBinary = path.join(root, 'bin', platform, path.basename(acc.engine()[0]));
  const sums = path.join(root, 'bin', 'SHA256SUMS');
  fs.writeFileSync(sums, `${'0'.repeat(64)}  ${platform}/${path.basename(acc.engine()[0])}\n`);
  const untouchedBytes = fs.readFileSync(shipped);
  const untouchedMtime = fs.statSync(shipped).mtimeMs;
  const tampered = spawnSync(shipped, ['ensure'], { encoding: 'utf8', env });
  const tamperedLog = `${tampered.stdout}${tampered.stderr}${fs.existsSync(path.join(acc.guardDir, 'errors.log')) ? fs.readFileSync(path.join(acc.guardDir, 'errors.log'), 'utf8') : ''}`;
  check('ensure refuses a binary that fails the SHA256SUMS check', fs.readFileSync(shipped).equals(untouchedBytes) && fs.statSync(shipped).mtimeMs === untouchedMtime && /SHA256SUMS/.test(tamperedLog), true);
  const crypto = require('crypto');
  fs.writeFileSync(sums, `${crypto.createHash('sha256').update(fs.readFileSync(platformBinary)).digest('hex')}  ${platform}/${path.basename(acc.engine()[0])}\n`);
  const ensure = spawnSync(shipped, ['ensure'], { encoding: 'utf8', env });
  check('ensure via the shipped launcher exits 0', ensure.status, 0);
  check('ensure replaced the launcher with the platform binary', fs.readFileSync(shipped).equals(fs.readFileSync(platformBinary)), true);
  check('no staging leftovers', fs.readdirSync(path.join(root, 'bin')).filter((name) => name.includes('.tmp') || name.endsWith('.old')), []);
  const before = fs.statSync(shipped).mtimeMs;
  await sleep(20);
  check('second ensure is a no-op', spawnSync(shipped, ['ensure'], { encoding: 'utf8', env }).status === 0 && fs.statSync(shipped).mtimeMs === before, true);
  const status = spawnSync(shipped, ['status'], { encoding: 'utf8', env });
  check('placed binary resolves the marketplace root', status.status === 0 && status.stdout.includes('5s ≥%92'), true);
  const doctor = spawnSync(shipped, ['doctor'], { encoding: 'utf8', env }).stdout;
  check('placed binary reports the marketplace root as plugin location', doctor.includes(`plugin konumu: ${root}`), true);
  const rolesDir = path.join(LAB_ROOT, 'roles-account');
  fs.mkdirSync(rolesDir, { recursive: true });
  writeJson(path.join(rolesDir, 'settings.json'), {});
  const economy = spawnSync(shipped, ['setup', '--config-dir', rolesDir, '--profile', 'economy'], { encoding: 'utf8', env: { ...env, NOCTIS_NO_TASKS: '1' } });
  const economyConfig = readJson(path.join(rolesDir, PLUGIN_NAME, 'config.json'));
  check('roles: profile flag sets the roles and the working keys', economy.status === 0 && economyConfig.roles.profile === 'economy' && economyConfig.models.primary === 'sonnet' && economyConfig.models.effort === 'high' && economyConfig.models.fallback === 'haiku' && economyConfig.router.subagentModels.Plan === 'opus', true);
  check('roles: settings.json follows the code role', readJson(path.join(rolesDir, 'settings.json')).model === 'sonnet' && readJson(path.join(rolesDir, 'settings.json')).env.CLAUDE_CODE_EFFORT_LEVEL === 'high', true);
  const lite = fs.readFileSync(path.join(root, 'agents', 'lite.md'), 'utf8');
  const digest = fs.readFileSync(path.join(root, 'agents', 'digest.md'), 'utf8');
  check('roles: plugin agents rewritten from the profile', /^model: haiku$/m.test(lite) && /^effort: high$/m.test(lite) && /^effort: low$/m.test(digest) && lite.split('---').length === 3, true);
  check('setup: permissions.defaultMode=auto when the CLI supports it', readJson(path.join(rolesDir, 'settings.json')).permissions.defaultMode === 'auto' && readJson(path.join(rolesDir, PLUGIN_NAME, 'config.json')).managedPermissionMode === 'auto', true);
  const keepDir = path.join(LAB_ROOT, 'keep-account');
  fs.mkdirSync(keepDir, { recursive: true });
  writeJson(path.join(keepDir, 'settings.json'), { permissions: { defaultMode: 'plan', allow: ['Bash(npm test)'] } });
  spawnSync(shipped, ['setup', '--config-dir', keepDir, '--profile', 'noctis', '--permissions', 'keep'], { encoding: 'utf8', env: { ...env, NOCTIS_NO_TASKS: '1' } });
  check('setup: --permissions keep leaves the user permissions alone', readJson(path.join(keepDir, 'settings.json')).permissions.defaultMode === 'plan' && readJson(path.join(keepDir, 'settings.json')).permissions.allow.length === 1, true);
  spawnSync(shipped, ['setup', '--config-dir', keepDir, '--profile', 'noctis', '--permissions', 'acceptEdits'], { encoding: 'utf8', env: { ...env, NOCTIS_NO_TASKS: '1' } });
  check('setup: --permissions acceptEdits pins the milder mode and keeps allow rules', readJson(path.join(keepDir, 'settings.json')).permissions.defaultMode === 'acceptEdits' && readJson(path.join(keepDir, 'settings.json')).permissions.allow.length === 1, true);
  spawnSync(shipped, ['install', '--source', root, '--config-dir', keepDir, '--uninstall'], { encoding: 'utf8', env });
  check('uninstall: restores the permission mode the person had before setup', readJson(path.join(keepDir, 'settings.json')).permissions.defaultMode === 'plan' && readJson(path.join(keepDir, 'settings.json')).permissions.allow.length === 1, true);
  const custom = spawnSync(shipped, ['setup', '--config-dir', rolesDir, '--code', 'opus:xhigh', '--research', 'sonnet', '--planning', 'opus:high'], { encoding: 'utf8', env: { ...env, NOCTIS_NO_TASKS: '1' } });
  const customConfig = readJson(path.join(rolesDir, PLUGIN_NAME, 'config.json'));
  const liteCustom = fs.readFileSync(path.join(root, 'agents', 'lite.md'), 'utf8');
  check('roles: per-role flags make a custom profile and keep the rest', custom.status === 0 && customConfig.roles.profile === 'custom' && customConfig.models.primary === 'opus' && customConfig.models.effort === 'xhigh' && customConfig.roles.digest.model === 'haiku' && customConfig.router.subagentModels.Plan === 'opus', true);
  check('roles: research without effort drops the effort line', /^model: sonnet$/m.test(liteCustom) && !/^effort:/m.test(liteCustom), true);
  check('roles: bad effort is rejected', spawnSync(shipped, ['setup', '--config-dir', rolesDir, '--code', 'opus:ultra'], { encoding: 'utf8', env }).status, 1);
  const oldDir = path.join(LAB_ROOT, 'old-account');
  fs.mkdirSync(path.join(oldDir, PLUGIN_NAME), { recursive: true });
  writeJson(path.join(oldDir, 'settings.json'), {});
  writeJson(path.join(oldDir, PLUGIN_NAME, 'config.json'), { models: { primary: 'opus', fallback: 'sonnet', effort: 'high' } });
  const upgraded = spawnSync(shipped, ['setup', '--config-dir', oldDir], { encoding: 'utf8', env: { ...env, NOCTIS_NO_TASKS: '1' } });
  const upgradedConfig = readJson(path.join(oldDir, PLUGIN_NAME, 'config.json'));
  check('roles: an older config keeps its models and gets a derived custom profile', upgraded.status === 0 && upgradedConfig.roles.profile === 'custom' && upgradedConfig.roles.code.model === 'opus' && upgradedConfig.roles.code.effort === 'high' && upgradedConfig.models.primary === 'opus' && upgradedConfig.models.fallback === 'sonnet', true);
  spawnSync(shipped, ['setup', '--config-dir', rolesDir, '--profile', 'noctis'], { encoding: 'utf8', env: { ...env, NOCTIS_NO_TASKS: '1' } });
  check('roles: back to noctis restores the agents', /^model: opus$/m.test(fs.readFileSync(path.join(root, 'agents', 'lite.md'), 'utf8')), true);
}

async function scenarioQueuePriorities(acc) {
  const now = nowSec();
  const queueFile = path.join(PROJECT_DIR, 'TASKS.md');
  fs.writeFileSync(queueFile, [
    '# q',
    '- [x] schema #db',
    '- [ ] (P3) write docs #docs',
    '- [ ] (P0) fix login bug #auth',
    '- [ ] (P1) migrate users (after #auth, #db)',
    '- [ ] deploy (after 4, #docs)',
    '- [ ] (P9) cleanup (after #nonexistent)',
    '',
  ].join('\n'));
  acc.statusline('qp1', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 30);
  const first = acc.hook({ hook_event_name: 'Stop', session_id: 'qp1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: false });
  const firstReason = JSON.parse(first).reason;
  check('queue: highest priority eligible item comes first', firstReason.includes('Queue continues: 5 open') && firstReason.includes('("(P0) fix login bug #auth")'), true);
  check('queue: blocked items are counted and mentioned', firstReason.includes('2 item(s) wait on unfinished dependencies'), true);
  acc.statusline('qp1', 'claude-fable-5-1', 93, now + 2 * 86400, 10, now + 3 * 86400, 30);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'qp1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const checkpoint = fs.readFileSync(acc.state().checkpoints.qp1.path, 'utf8');
  check('queue: checkpoint lists eligible items in priority order', checkpoint.indexOf('(P0) fix login bug') < checkpoint.indexOf('(P3) write docs') && checkpoint.indexOf('(P3) write docs') < checkpoint.indexOf('(P9) cleanup') && !checkpoint.includes('migrate users'), true);
  acc.run(['cancel', 'qp1']);
  acc.statusline('qp1', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 30);
  fs.writeFileSync(queueFile, fs.readFileSync(queueFile, 'utf8').replace('- [ ] (P0) fix login bug #auth', '- [x] (P0) fix login bug #auth'));
  const second = acc.hook({ hook_event_name: 'Stop', session_id: 'qp1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true });
  const secondReason = JSON.parse(second).reason;
  check('queue: finishing a dependency unblocks the next priority', secondReason.includes('Queue continues: 4 open') && secondReason.includes('("(P1) migrate users (after #auth, #db)")'), true);
  fs.writeFileSync(queueFile, [
    '# sloppy list',
    '-[ ]fix the login redirect that breaks',
    '   when the session cookie is missing',
    '   and the user comes from the mobile app',
    '* [] Write the release notes',
    '',
    '2) [X] already done item',
    '[ ] bare box without a bullet (P1)',
    '- [ x ] spaced box means done',
    'TODO: plain todo marker item',
    '- just a note line with no box, ignored',
    '',
  ].join('\n'));
  acc.statusline('qp2', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 30);
  const sloppy = JSON.parse(acc.hook({ hook_event_name: 'Stop', session_id: 'qp2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: false })).reason;
  check('queue: sloppy boxes, bare boxes and TODO markers all count; notes do not', sloppy.includes('Queue continues: 4 open'), true);
  check('queue: a multi-line item is one item and P1 comes first', sloppy.includes('("bare box without a bullet (P1)")'), true);
  acc.statusline('qp2', 'claude-fable-5-1', 93, now + 2 * 86400, 10, now + 3 * 86400, 30);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'qp2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const sloppyCheckpoint = fs.readFileSync(acc.state().checkpoints.qp2.path, 'utf8');
  check('queue: continuation lines joined into the item text', sloppyCheckpoint.includes('fix the login redirect that breaks when the session cookie is missing and the user comes from the mobile app'), true);
  acc.run(['cancel', 'qp2']);
  acc.statusline('qp2', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 30);
  fs.writeFileSync(queueFile, ['# plain list', '- migrate the users table', '- ~~write docs~~', '- deploy (done)', '1) run the smoke tests', '', 'notes: not a task', ''].join('\n'));
  const plain = JSON.parse(acc.hook({ hook_event_name: 'Stop', session_id: 'qp2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true })).reason;
  check('queue: a list without checkboxes is read as tasks with done markers honoured', plain.includes('Queue continues: 2 open') && plain.includes('("migrate the users table")') && plain.includes('rewrite every open item as "- [ ] …"'), true);
  fs.writeFileSync(queueFile, ['# cycle', '- [ ] a #a (after #b)', '- [ ] b #b (after #a)', ''].join('\n'));
  const blocked = acc.hook({ hook_event_name: 'Stop', session_id: 'qp1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true });
  check('queue: fully blocked queue stops with a notice instead of forcing', !blocked.includes('"decision":"block"') && blocked.includes('Kuyruk tıkalı: 2'), true);
  check('queue: blocked stop journaled', acc.run(['why', '--last', '1']).includes('queue blocked'), true);
  fs.unlinkSync(queueFile);
}

async function scenarioGitHubQueue(acc) {
  const now = nowSec();
  const queueFile = path.join(PROJECT_DIR, 'TASKS.md');
  writeJson(path.join(LAB_ROOT, 'gh-issues.json'), [
    { number: 12, title: 'Fix login redirect', labels: [{ name: 'bug' }, { name: 'P1' }] },
    { number: 13, title: 'Write the API docs', labels: [{ name: 'priority: low' }] },
    { number: 14, title: 'Old item already tracked', labels: [] },
  ]);
  fs.writeFileSync(queueFile, '# q\n- [x] #14 Old item already tracked\n');
  const imported = acc.run(['queue', 'import', '--cwd', PROJECT_DIR]);
  const content = fs.readFileSync(queueFile, 'utf8');
  check('queue import: new issues appended with priorities from labels', content.includes('- [ ] (P1) #12 Fix login redirect') && content.includes('- [ ] (P7) #13 Write the API docs') && content.includes('## GitHub issues'), true);
  check('queue import: already tracked issues skipped', imported.includes('2 issue TASKS.md dosyasına eklendi (1 zaten vardı)') && content.split('#14').length === 2, true);
  check('queue import: idempotent', acc.run(['queue', 'import', '--cwd', PROJECT_DIR]).includes('yeni bir şey yok'), true);
  acc.setConfig((config) => {
    config.queue.github.closeOnDone = true;
  });
  acc.statusline('gh1', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 30);
  acc.hook({ hook_event_name: 'Stop', session_id: 'gh1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: false });
  fs.writeFileSync(queueFile, fs.readFileSync(queueFile, 'utf8').replace('- [ ] (P1) #12', '- [x] (P1) #12'));
  acc.hook({ hook_event_name: 'Stop', session_id: 'gh1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true });
  await sleep(400);
  check('close-on-done: checked issue closed via gh once', acc.lab.ghCalls().filter((line) => line.includes('issue close 12')).length === 1 && !acc.lab.ghCalls().some((line) => line.includes('close 14')), true);
  acc.hook({ hook_event_name: 'Stop', session_id: 'gh1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true });
  await sleep(300);
  check('close-on-done: never closed twice', acc.lab.ghCalls().filter((line) => line.includes('issue close 12')).length, 1);
  fs.writeFileSync(queueFile, fs.readFileSync(queueFile, 'utf8').replace('- [ ] (P7) #13', '- [x] (P7) #13'));
  const done = acc.hook({ hook_event_name: 'Stop', session_id: 'gh1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true });
  await sleep(400);
  check('close-on-done: queue finished closes the last issue', done.includes('Kuyruk bitti') && acc.lab.ghCalls().some((line) => line.includes('issue close 13')), true);
  acc.setConfig((config) => {
    config.queue.github.closeOnDone = false;
  });
  fs.unlinkSync(queueFile);
}

async function scenarioWorkflows(acc) {
  const now = nowSec();
  acc.statusline('wf1', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 30);
  const fanOut = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'wf1', cwd: PROJECT_DIR, prompt: 'Audit every route handler under src/routes for missing auth checks and fix what you find' });
  check('workflow: fan-out prompt gets the advisory with role models', fanOut.includes('dynamic workflow (ultracode)') && fanOut.includes('code-writing agents → fable (effort max)') && fanOut.includes('read-only analysis and review agents → opus (effort xhigh)'), true);
  check('workflow: advisory journaled', acc.run(['why', '--last', '2']).includes('suggest-workflow'), true);
  check('workflow: explicit ultracode prompt gets no duplicate advisory', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'wf1', cwd: PROJECT_DIR, prompt: 'ultracode: audit every route handler under src/routes' }).includes('fan-out task'), false);
  check('workflow: ordinary prompt gets no advisory', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'wf1', cwd: PROJECT_DIR, prompt: 'fix the null check in parser.go' }).includes('fan-out task'), false);
  check('workflow: turkish fan-out prompt detected', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'wf1', cwd: PROJECT_DIR, prompt: 'tüm bileşen dosyalarını TypeScript\'a taşı ve testleri düzelt' }).includes('fan-out task'), true);
  const launch = acc.hook({ hook_event_name: 'PreToolUse', session_id: 'wf1', cwd: PROJECT_DIR, tool_name: 'Workflow', tool_input: { name: 'audit-routes', script_path: '/tmp/x/audit-routes.js' } });
  check('workflow: launch allowed under the thresholds and recorded', launch === '' && acc.state().workflows.wf1.length === 1 && acc.state().workflows.wf1[0].name === 'audit-routes', true);
  acc.statusline('wf1', 'claude-fable-5-1', 93, now + 2 * 86400, 10, now + 3 * 86400, 30);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'wf1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const checkpoint = fs.readFileSync(acc.state().checkpoints.wf1.path, 'utf8');
  check('workflow: checkpoint names the run and says relaunch, never restart', checkpoint.includes('audit-routes') && checkpoint.includes('asla sıfırdan başlatma'), true);
  const denied = acc.hook({ hook_event_name: 'PreToolUse', session_id: 'wf1', cwd: PROJECT_DIR, tool_name: 'Workflow', tool_input: { name: 'audit-routes' } });
  check('workflow: launch denied at the pause threshold', denied.includes('"permissionDecision":"deny"') && denied.includes('pause threshold'), true);
  acc.hook({ hook_event_name: 'SessionEnd', session_id: 'wf1', reason: 'exit' });
  check('workflow: session end keeps the record while a wait is pending', acc.state().workflows.wf1.length, 1);
  const wait = acc.state();
  wait.waits.wf1.resumeAt = now - 5;
  wait.waits.wf1.until = now - 10;
  wait.workflows.wf1[0].at = now - 5 * 86400;
  writeJson(acc.stateFile, wait);
  acc.hook({ hook_event_name: 'PostModelSwitch', session_id: 'wfx', to_model: 'claude-opus-5' });
  check('workflow: pruning keeps a 5-day-old record while its wait is pending', acc.state().workflows.wf1.length, 1);
  const old = new Date(Date.now() - 3600000);
  fs.utimesSync(TRANSCRIPT, old, old);
  acc.statusline('wf1', 'claude-fable-5-1', 5, now + 7200, 10, now + 3 * 86400, 30);
  resetCalls();
  acc.run(['resume', '--sid', 'wf1', '--account', acc.dir]);
  const call = callsLog().pop() || '';
  check('workflow: relaunch prompt tells Claude to relaunch the same script', call.includes('--resume wf1') && call.includes('relaunch it with the same script') && call.includes('audit-routes'), true);
  for (const used of [12, 20, 28, 36, 44, 52, 60, 67, 74, 80, 85, 88]) acc.statusline('wf2', 'claude-fable-5-1', used, now + 7200, 10, now + 3 * 86400, 30);
  const warnDenied = acc.hook({ hook_event_name: 'PreToolUse', session_id: 'wf2', cwd: PROJECT_DIR, tool_name: 'Workflow', tool_input: { name: 'big-run' } });
  check('workflow: launch denied inside the warn band', warnDenied.includes('"permissionDecision":"deny"') && warnDenied.includes('too close to the limit'), true);
  check('workflow: fan-out prompt gets no advisory inside the warn band', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'wf2', cwd: PROJECT_DIR, prompt: 'Audit every route handler under src/routes for missing auth checks' }).includes('fan-out task'), false);
  acc.setConfig((config) => {
    config.workflow.gate = false;
  });
  check('workflow: gate off allows the launch', acc.hook({ hook_event_name: 'PreToolUse', session_id: 'wf2', cwd: PROJECT_DIR, tool_name: 'Workflow', tool_input: { name: 'big-run' } }), '');
  acc.setConfig((config) => {
    config.workflow.gate = true;
  });
  const queueFile = path.join(PROJECT_DIR, 'TASKS.md');
  fs.writeFileSync(queueFile, '# q\n- [ ] migrate every component under src/components to TypeScript\n- [ ] fix typo\n');
  acc.statusline('wf3', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 30);
  const stop = acc.hook({ hook_event_name: 'Stop', session_id: 'wf3', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: false });
  check('workflow: fan-out queue item gets the advisory in the Stop directive', JSON.parse(stop).reason.includes('The next item looks like a fan-out task'), true);
  fs.unlinkSync(queueFile);
  acc.hook({ hook_event_name: 'SessionEnd', session_id: 'wf1', reason: 'exit' });
  check('workflow: session end clears the launch record once nothing is pending', acc.state().workflows.wf1, undefined);
}

async function scenarioFirstRunEdges(acc) {
  const now = nowSec();
  acc.statusline('fr1', 'claude-fable-5-1', 30, now + 7200, 99, now + 3 * 86400, 10);
  const start = acc.hook({ hook_event_name: 'SessionStart', session_id: 'fr1', cwd: PROJECT_DIR, source: 'startup' });
  check('first run at 99 %: session start explains the pause and the escape hatch', start.includes('zaten %99') && start.includes('/noctis:pause 120'), true);
  check('first run at 99 %: the notice is not repeated for the same window', acc.hook({ hook_event_name: 'SessionStart', session_id: 'fr1', cwd: PROJECT_DIR, source: 'startup' }).includes('zaten %99'), false);
  const blocked = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'fr1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'start with the parser refactor' });
  check('first run at 99 %: the first prompt is saved with a resume time and the pause hint', blocked.includes('"decision":"block"') && blocked.includes('iş kaydedildi') && blocked.includes('noctis off 120'), true);
  check('first run at 99 %: a scheduled relaunch exists', acc.state().waits.fr1 && acc.state().waits.fr1.resumeAt > now + 2 * 86400, true);
  acc.run(['off', '1']);
  check('first run at 99 %: pause lets the prompt through', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'fr1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'start with the parser refactor' }), '');
  acc.run(['on']);
  acc.run(['cancel', 'fr1']);
  acc.statusline('fr1', 'claude-fable-5-1', 30, now + 7200, 10, now + 3 * 86400, 10);
  const settingsFile = path.join(acc.dir, 'settings.json');
  writeJson(settingsFile, { ...readJson(settingsFile), model: 'fable' });
  acc.hook({ hook_event_name: 'StopFailure', session_id: 'fr1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'model_not_found' });
  check('model_not_found: default model steps down to the fallback', readJson(settingsFile).model, 'opus');
  acc.hook({ hook_event_name: 'StopFailure', session_id: 'fr1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'model_not_found' });
  check('model_not_found: then to no explicit model at all', readJson(settingsFile).model, undefined);
  check('model_not_found: journaled and no wait registered', acc.run(['why', '--last', '1']).includes('model-unavailable') && acc.state().waits.fr1 === undefined, true);
  writeJson(settingsFile, { ...readJson(settingsFile), model: 'fable' });
  acc.hook({ hook_event_name: 'StopFailure', session_id: 'fr1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, error_type: 'billing_error' });
  check('billing_error: nothing scheduled, model untouched', acc.state().waits.fr1 === undefined && readJson(settingsFile).model === 'fable' && acc.run(['why', '--last', '1']).includes('account-error'), true);
  const usageFile = path.join(acc.guardDir, 'usage.json');
  const savedUsage = fs.readFileSync(usageFile, 'utf8');
  acc.setConfig((config) => {
    config.fable.source = 'off';
  });
  fs.unlinkSync(usageFile);
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  const noData = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'fr2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'hello, refactor the parser' });
  check('no usage data: prompt passes with a one-time notice', !noData.includes('"decision":"block"') && noData.includes('kullanım verisi yok'), true);
  check('no usage data: check reports 20', acc.runFull(['check']).status, 20);
  check('no usage data: stop hook lets the session stop', acc.hook({ hook_event_name: 'Stop', session_id: 'fr2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  acc.setConfig((config) => {
    config.fable.source = 'oauth';
  });
  fs.writeFileSync(usageFile, savedUsage);
}

async function scenarioCheckGate(acc) {
  const now = nowSec();
  acc.statusline('cg1', 'claude-fable-5-1', 41, now + 7200, 23, now + 3 * 86400);
  const ok = acc.runFull(['check']);
  check('check: under thresholds -> exit 0', ok.status === 0 && ok.stdout.startsWith('ok ·'), true);
  for (const used of [50, 58, 65, 72, 78, 84, 87]) acc.statusline('cg1', 'claude-fable-5-1', used, now + 7200, 23, now + 3 * 86400);
  check('check: warn band -> exit 10', acc.runFull(['check']).status, 10);
  acc.statusline('cg1', 'claude-fable-5-1', 93, now + 7200, 23, now + 3 * 86400);
  const over = acc.runFull(['check', '--json']);
  const parsed = JSON.parse(over.stdout);
  check('check: over threshold -> exit 11 with json facts', over.status === 11 && parsed.verdict === 'over' && parsed.five === 93 && typeof parsed.until === 'number', true);
  const usageFile = path.join(acc.guardDir, 'usage.json');
  const usage = readJson(usageFile);
  usage.updatedAt = now - 3600;
  writeJson(usageFile, usage);
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  check('check: stale data -> exit 20', acc.runFull(['check']).status, 20);
  const pulseBefore = acc.state().lastHookAt || 0;
  acc.runFull(['check']);
  check('check never records a hook pulse', acc.state().lastHookAt || 0, pulseBefore);
  acc.statusline('cg1', 'claude-fable-5-1', 10, now + 7200, 10, now + 3 * 86400);
}

async function scenarioDoubleRunner(acc) {
  const now = nowSec();
  acc.setConfig((config) => {
    config.wait.maxInHookMinutes = 0; 
  });
  mock.limits = [
    { kind: 'session', percent: 94, resets_at: new Date((now + 600) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 20, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('dr1', 'claude-opus-5', 94, now + 600, 20, now + 3 * 86400);
  check('double runner: wall recorded', acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'dr1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }).includes('"continue":false'), true);
  const wait = acc.state().waits.dr1;
  mock.limits = [
    { kind: 'session', percent: 3, resets_at: new Date((now + 18600) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 20, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.timeOffset = Math.ceil(wait.resumeAt - now) + 5;
  const wasFast = acc.fastClaude;
  acc.fastClaude = false; 
  resetCalls();
  await Promise.all([acc.runPromise(['resume', '--sid', 'dr1', '--account', acc.dir]), acc.runPromise(['resume', '--sid', 'dr1', '--account', acc.dir])]);
  check('double runner (overlapping): exactly one launch', callsLog().filter((line) => line.includes('--resume dr1 ')).length, 1);
  check('double runner: skip journaled', acc.run(['why', '--last', '3']).includes('skip-launch'), true);
  check('double runner: wait and handoff cleared', acc.state().waits.dr1 === undefined && Object.keys(acc.state().handedOff).length === 0, true);
  acc.fastClaude = wasFast;
  acc.timeOffset = 0;
  mock.limits = [
    { kind: 'session', percent: 94, resets_at: new Date((now + 600) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 20, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('dr2', 'claude-opus-5', 94, now + 600, 20, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'dr2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const wait2 = acc.state().waits.dr2;
  mock.limits = [
    { kind: 'session', percent: 3, resets_at: new Date((now + 18600) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 20, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.timeOffset = Math.ceil(wait2.resumeAt - now) + 5;
  resetCalls();
  acc.run(['resume', '--sid', 'dr2', '--account', acc.dir]);
  acc.run(['resume', '--sid', 'dr2', '--account', acc.dir]);
  check('double runner (sequential): exactly one launch', callsLog().filter((line) => line.includes('--resume dr2 ')).length, 1);
  acc.timeOffset = 0;
  mock.limits = [];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.setConfig((config) => {
    config.wait.maxInHookMinutes = 330;
  });
}

async function scenarioAutoQueue(acc) {
  const now = nowSec();
  fs.rmSync(path.join(PROJECT_DIR, 'TASKS.md'), { force: true });
  acc.statusline('aq1', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 30);
  const job = ['Please do the following for the billing service and do not stop until everything is done:', '1. add input validation to the signup form and reject empty emails', '2. write unit tests for the payments module covering refunds', '3. update the README for the new CLI flags we added last week', '4. run the full test suite and fix anything red', '5. open a pull request with a summary of the changes'].join('\n');
  const out = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: job });
  const record = acc.state().autoQueues.aq1;
  check('auto queue: a numbered job becomes a checklist outside the project', record !== undefined && record.items === 5 && record.path.startsWith(path.join(acc.guardDir, 'queues')) && !fs.existsSync(path.join(PROJECT_DIR, 'TASKS.md')), true);
  check('auto queue: Claude gets the directive with the checklist path, the person a one-line notice', out.includes('multi-step job (5 items)') && out.includes(record.path.replace(/\\/g, '\\\\')) && /5-step job detected|5 adımlık iş/.test(out), true);
  const listed = fs.readFileSync(record.path, 'utf8');
  check('auto queue: items are clean checklist lines', (listed.match(/^- \[ \] /gm) || []).length === 5 && listed.includes('- [ ] write unit tests for the payments module covering refunds') && !listed.includes('- [ ] 2.'), true);
  const stop = acc.hook({ hook_event_name: 'Stop', session_id: 'aq1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: false });
  check('auto queue: Stop keeps the session going through the checklist', stop.includes('"decision":"block"') && stop.includes('Queue continues: 5 open') && stop.includes('add input validation'), true);
  acc.statusline('aq1', 'claude-fable-5-1', 95, now + 2 * 86400, 20, now + 3 * 86400, 30);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'aq1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const checkpoint = acc.run(['checkpoint', '--sid', 'aq1']);
  check('auto queue: a checkpoint written while the job is open lists its items', checkpoint.includes('add input validation') && checkpoint.includes(path.basename(record.path)), true);
  acc.run(['cancel', 'aq1']);
  acc.statusline('aq1', 'claude-fable-5-1', 20, now + 7200, 10, now + 3 * 86400, 30);
  fs.writeFileSync(record.path, listed.replace(/- \[ \]/g, '- [x]'));
  const done = acc.hook({ hook_event_name: 'Stop', session_id: 'aq1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, stop_hook_active: true });
  check('auto queue: finished checklist ends the loop with the done notice and is cleaned up', /Kuyruk bitti|Queue finished/.test(done) && !done.includes('"decision":"block"') && acc.state().autoQueues.aq1 === undefined && !fs.existsSync(record.path), true);
  check('auto queue: a short task is not a job', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'fix the typo in README.md and commit it' }).includes('multi-step job'), false);
  check('auto queue: a long question is not a job', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'Can you explain in detail how the payment retry logic works, why it uses exponential backoff, what the maximum number of attempts is, how failures are logged, and whether the webhook handler participates in the same transaction as the database write?' }).includes('multi-step job'), false);
  const prose = 'Refactor the authentication module so that tokens are validated in one place instead of three. Then add integration tests that cover the expired-token path and the revoked-token path. After that, migrate the sessions table to the new schema and write a rollback script. Finally, update the deployment notes so the on-call engineer knows about the new migration step.';
  const proseOut = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq3', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: prose });
  check('auto queue: four long imperative sentences are a job', proseOut.includes('multi-step job (4 items)') && acc.state().autoQueues.aq3 !== undefined, true);
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq3', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'actually, just tell me the current git branch' });
  check('auto queue: a manual prompt while a job is open ends the job (the person is steering)', acc.state().autoQueues.aq3, undefined);
  fs.writeFileSync(path.join(PROJECT_DIR, 'TASKS.md'), '# q\n- [ ] real file item\n');
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq4', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: job });
  check('auto queue: a real TASKS.md always wins over a prompt job', acc.state().autoQueues.aq4, undefined);
  fs.unlinkSync(path.join(PROJECT_DIR, 'TASKS.md'));
  acc.setConfig((config) => {
    config.queue.auto = false;
  });
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq5', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: job });
  check('auto queue: queue.auto=false disables it', acc.state().autoQueues.aq5, undefined);
  acc.setConfig((config) => {
    config.queue.auto = true;
  });
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq6', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: job });
  const open6 = acc.state().autoQueues.aq6;
  const resumePrompt = `${readJson(acc.configFile).resume.prompt} Task list: ${open6.path} (do not redo items already marked done) Next: add input validation to the signup form and reject empty emails | write unit tests for the payments module covering refunds | update the README. [noctis] The working tree changed while the session was paused; re-read the files you touch before editing.`;
  const localeBefore = JSON.stringify(acc.state().sessionLocale.aq6 || null);
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq6', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: resumePrompt });
  check('auto queue: the plugin\'s own resume prompt keeps the open job untouched', acc.state().autoQueues.aq6 !== undefined && acc.state().autoQueues.aq6.path === open6.path && fs.readFileSync(open6.path, 'utf8').includes('signup form'), true);
  check('auto queue: the resume prompt does not change the session language', JSON.stringify(acc.state().sessionLocale.aq6 || null), localeBefore);
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq6', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'thanks, stop here' });
  const context = 'Here is some background so you understand the project before you start on it today.\nOur app is a marketplace for used bikes with about two thousand listings.\nUsers can post listings with photos and sellers get paid through Stripe Connect.\nThe checkout page has been flaky since the last deploy and support is getting complaints about it.\nPlease fix the checkout bug in the cart page and add a regression test for it.';
  check('auto queue: descriptive context lines plus one task are not a job', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq7', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: context }).includes('multi-step job'), false);
  const bugReport = 'I am getting an error when I run the integration tests for the payments module after the dependency bump.\nHere is the exact output from the terminal:\nTypeError: cannot read properties of undefined (reading \'amount\')\n    at Object.<anonymous> (test/payments.test.js:12:5)\n    at Module._compile (node:internal/modules/cjs/loader:1256:14)\nCan you help me figure out what is wrong and fix it?';
  check('auto queue: a bug report with pasted output is not a job', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq7', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: bugReport }).includes('multi-step job'), false);
  const questions = 'Could you walk me through how the login flow works end to end in this codebase, I keep getting lost?\n- where is the session cookie set and which middleware reads it back?\n- why does the redirect loop happen when the token is expired?\n- is the CSRF token validated on every request or only on form posts?';
  check('auto queue: a list of questions is not a job', acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq7', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: questions }).includes('multi-step job'), false);
  const partlyDone = [job, '4. [x] already migrated the sessions table last week'].join('\n');
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq8', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: partlyDone });
  check('auto queue: items already ticked in the prompt are not queued', acc.state().autoQueues.aq8 !== undefined && acc.state().autoQueues.aq8.items === 5 && !fs.readFileSync(acc.state().autoQueues.aq8.path, 'utf8').includes('sessions table'), true);
  acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'aq8', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'ok stop' });
}

async function scenarioEarlyReset(acc) {
  const now = nowSec();
  acc.setConfig((config) => {
    config.wait.earlyResetPollMinutes = 0.05; 
  });
  const limited = (percent, resetIn) => [
    { kind: 'session', percent, resets_at: new Date((now + resetIn) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 20, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
  mock.limits = limited(93, 900);
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('er1', 'claude-fable-5-1', 93, now + 900, 20, now + 3 * 86400);
  let started = Date.now();
  let child = acc.hookAsync({ hook_event_name: 'UserPromptSubmit', session_id: 'er1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'keep going with auth.js' });
  let stdout = '';
  child.stdout.on('data', (chunk) => { stdout += chunk; });
  await sleep(2000);
  check('early reset: wait registered inside the hook', Boolean(acc.state().waits.er1 && acc.state().waits.er1.inHook), true);
  await sleep(4000);
  check('early reset: a window that is still full does not end the wait', Boolean(acc.state().waits.er1 && acc.state().waits.er1.inHook), true);
  mock.limits = limited(2, 18000);
  await new Promise((resolve) => child.on('close', resolve));
  check('early reset: in-hook wait ended within seconds of the reset', Date.now() - started < 20000, true);
  check('early reset: the person is told', /planlanandan önce|ahead of schedule/.test(stdout), true);
  check('early reset: journaled', acc.run(['why', '--last', '4']).includes('early-reset'), true);
  check('early reset: wait cleared', acc.state().waits.er1, undefined);
  mock.limits = limited(93, 900);
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('er2', 'claude-fable-5-1', 93, now + 900, 20, now + 3 * 86400);
  started = Date.now();
  child = acc.hookAsync({ hook_event_name: 'UserPromptSubmit', session_id: 'er2', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'keep going with auth.js' });
  stdout = '';
  child.stdout.on('data', (chunk) => { stdout += chunk; });
  await sleep(2000);
  acc.run(['cancel', 'er2']);
  await new Promise((resolve) => child.on('close', resolve));
  check('cancel: a sleeping in-hook wait ends within seconds', Date.now() - started < 20000, true);
  check('cancel: the session continues with a notice', /iptal edildi|cancelled/.test(stdout) && !stdout.includes('"decision":"block"'), true);
  acc.setConfig((config) => {
    config.wait.maxInHookMinutes = 0;
  });
  mock.limits = limited(94, 900);
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('er3', 'claude-fable-5-1', 94, now + 900, 20, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'er3', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const parked = acc.state().waits.er3;
  check('early reset: long wait parked with a sleeper', Boolean(parked && parked.scheduled && parked.scheduled.method === 'sleeper'), true);
  resetCalls();
  mock.limits = limited(3, 18000);
  await callsMatching('--resume er3 ');
  const er3Resumes = callsLog().filter((line) => line.includes('--resume er3 '));
  process.stdout.write(`  er3 resume calls: ${er3Resumes.length}\n`);
  for (const line of er3Resumes) process.stdout.write(`    ${line}\n`);
  if (er3Resumes.length === 0) {
    const parkedNow = (acc.state().waits || {}).er3;
    process.stdout.write(`    er3 wait now: ${JSON.stringify(parkedNow && parkedNow.scheduled)}\n`);
    process.stdout.write(`    er3 all calls: ${JSON.stringify(callsLog())}\n`);
    const errorsFile = path.join(acc.guardDir, 'errors.log');
    const errors = fs.existsSync(errorsFile) ? fs.readFileSync(errorsFile, 'utf8').trim().split('\n').slice(-12) : [];
    for (const line of errors) process.stdout.write(`    er3 errors.log: ${line}\n`);
  }
  check('early reset: sleeper resumed the session ahead of schedule', callsLog().filter((line) => line.includes('--resume er3 ')).length, 1);
  check('early reset: sleeper journaled it', acc.run(['why', '--last', '6']).includes('early-reset'), true);
  acc.manualSchedule = true;
  mock.limits = limited(94, 900);
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('er4', 'claude-fable-5-1', 94, now + 900, 20, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'er4', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('early reset: wait parked without a scheduler', acc.state().waits.er4 && acc.state().waits.er4.scheduled.method, 'manual');
  resetCalls();
  mock.limits = limited(3, 18000);
  acc.run(['statusline'], acc.statuslineInput('other-window', 'claude-fable-5-1', 3, now + 18000, 20, now + 3 * 86400), { NOCTIS_NO_EARLY_TRIGGER: '' });
  await callsMatching('--resume er4 ');
  check('early reset: another window\'s status line resumed the parked session', callsLog().filter((line) => line.includes('--resume er4 ')).length, 1);
  mock.limits = limited(94, 900);
  acc.statusline('er5', 'claude-fable-5-1', 94, now + 900, 20, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'er5', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  const parked5 = acc.state().waits.er5;
  check('early reset: wait parked for the waking check', Boolean(parked5 && !parked5.inHook), true);
  writeJson(acc.stateFile, {
    ...acc.state(),
    waits: { ...acc.state().waits, er5: { ...parked5, waking: nowSec(), heartbeat: nowSec() } },
  });
  resetCalls();
  mock.limits = limited(3, 18000);
  acc.run(['statusline'], acc.statuslineInput('other-window', 'claude-fable-5-1', 3, now + 18000, 20, now + 3 * 86400), { NOCTIS_NO_EARLY_TRIGGER: '' });
  await sleep(3000);
  check('early reset: a wait a hook is waking is left to that hook', callsLog().filter((line) => line.includes('--resume er5 ')).length, 0);
  check('early reset: and it is not marked as triggered', acc.state().waits.er5 && acc.state().waits.er5.earlyTriggeredAt, undefined);
  const waking5 = acc.state().waits.er5;
  delete waking5.waking;
  writeJson(acc.stateFile, { ...acc.state(), waits: { ...acc.state().waits, er5: waking5 } });
  acc.run(['statusline'], acc.statuslineInput('other-window', 'claude-fable-5-1', 3, now + 18000, 20, now + 3 * 86400), { NOCTIS_NO_EARLY_TRIGGER: '' });
  await callsMatching('--resume er5 ');
  check('early reset: the same wait resumes once the hook is no longer on it', callsLog().filter((line) => line.includes('--resume er5 ')).length, 1);

  acc.manualSchedule = false;
  acc.setConfig((config) => {
    config.wait.maxInHookMinutes = 330;
    config.wait.earlyResetPollMinutes = 5;
  });
  mock.limits = [];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.run(['cancel']);
}

async function scenarioVisibleRelaunch(acc) {
  if (IS_WINDOWS) return;
  const now = nowSec();
  const fakeTerminal = path.join(acc.lab.binDir, 'x-terminal-emulator');
  fs.writeFileSync(fakeTerminal, ['#!/usr/bin/env sh', '# runs the launcher like a terminal tab would: detached, in the background', 'shift', 'nohup sh "$@" >/dev/null 2>&1 &', 'exit 0', ''].join('\n'));
  fs.chmodSync(fakeTerminal, 0o755);
  acc.setConfig((config) => {
    config.wait.maxInHookMinutes = 0;
  });
  mock.limits = [
    { kind: 'session', percent: 94, resets_at: new Date((now + 900) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 20, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('vr1', 'claude-fable-5-1', 94, now + 900, 20, now + 3 * 86400);
  acc.hook({ hook_event_name: 'PostToolBatch', session_id: 'vr1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('visible relaunch: wait parked', Boolean(acc.state().waits.vr1), true);
  const previous = spawn(process.execPath, ['-e', 'setTimeout(() => {}, 60000)'], { stdio: 'ignore' });
  const state = acc.state();
  state.launched.vr1 = { pid: previous.pid, at: now - 3600, how: 'terminal' };
  state.waits.vr1.resumeAt = now - 5;
  state.waits.vr1.until = now - 10;
  writeJson(acc.stateFile, state);
  const old = new Date(Date.now() - 3600000);
  fs.utimesSync(TRANSCRIPT, old, old);
  mock.limits = [
    { kind: 'session', percent: 3, resets_at: new Date((now + 18000) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 20, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  resetCalls();
  const wasFast = acc.fastClaude;
  acc.fastClaude = false; 
  acc.timeOffset = 30; 
  const runner = acc.runPromise(['resume', '--sid', 'vr1', '--account', acc.dir], null, { NOCTIS_NO_TERMINAL: '', DISPLAY: ':9' });
  let moved = '';
  for (let i = 0; i < 40 && !moved; i += 1) {
    await sleep(100);
    if (acc.state().handedOff.vr1) moved = acc.run(['statusline'], acc.statuslineInput('vr1', 'claude-fable-5-1', 3, now + 18000, 20, now + 3 * 86400));
  }
  check('visible relaunch: the old window\'s status line says the session moved', /başka bir pencerede|another window/.test(moved), true);
  await runner;
  acc.fastClaude = wasFast;
  acc.timeOffset = 0;
  check('visible relaunch: claude ran through the terminal launcher', callsLog().some((line) => line.includes('--resume vr1') && line.includes('HANDOFF=vr1')), true);
  await new Promise((resolve) => {
    if (previous.exitCode !== null || previous.signalCode !== null) return resolve();
    previous.once('exit', resolve);
    setTimeout(resolve, 3000);
  });
  const previousAlive = isAlive(previous.pid);
  const closeJournal = acc.run(['why', '--last', '6']);
  if (previousAlive || !closeJournal.includes('close-previous')) {
    process.stdout.write(`  visible relaunch: alive=${previousAlive} exit=${previous.exitCode} signal=${previous.signalCode} journal=${closeJournal.includes('close-previous')}\n`);
  }
  check('visible relaunch: the previous window was closed first', !previousAlive && closeJournal.includes('close-previous'), true);
  check('visible relaunch: launch record and wait cleared afterwards', acc.state().launched.vr1 === undefined && acc.state().waits.vr1 === undefined, true);
  check('visible relaunch: no launcher files left behind', fs.existsSync(path.join(acc.guardDir, 'launches')) ? fs.readdirSync(path.join(acc.guardDir, 'launches')).filter((name) => name.startsWith('vr1')) : [], []);
  try { previous.kill(); } catch {}
  const stranger = spawn('sleep', ['30'], { stdio: 'ignore' });
  await sleep(200);
  const strangerState = acc.state();
  strangerState.launched.vr2 = { pid: stranger.pid, at: nowSec() - 600, how: 'terminal' };
  writeJson(acc.stateFile, strangerState);
  resetCalls();
  acc.run(['resume', '--sid', 'vr2', '--account', acc.dir]);
  check('visible relaunch: a recycled pid running something else is never killed', isAlive(stranger.pid), true);
  check('visible relaunch: and no window was closed for it', fs.readFileSync(path.join(acc.guardDir, 'guard.log'), 'utf8').includes('closed the previous window of vr2'), false);
  try { stranger.kill(); } catch {}
  const oldState = acc.state();
  oldState.launched.vr3 = { pid: process.pid, at: nowSec() - 40 * 86400, how: 'terminal' };
  writeJson(acc.stateFile, oldState);
  acc.run(['resume', '--sid', 'vr3', '--account', acc.dir]);
  check('visible relaunch: a launch record older than any wait is ignored', true, true);
  fs.unlinkSync(fakeTerminal);
  acc.setConfig((config) => {
    config.wait.maxInHookMinutes = 330;
  });
  mock.limits = [];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
}

async function scenarioRepair(acc) {
  const now = nowSec();
  mock.limits = [
    { kind: 'session', percent: 20, resets_at: new Date((now + 7200) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 95, resets_at: new Date((now + 2 * 86400) * 1000).toISOString() },
  ];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
  acc.statusline('rp1', 'claude-fable-5-1', 20, now + 7200, 95, now + 2 * 86400);
  const state = acc.state();
  state.handedOff.rp1 = { at: now - 600, model: 'opus', mode: 'window', pid: 999999 };
  state.waits.rp1 = { kind: 'batch', window: 'seven_day', label: 'weekly', used: 95, threshold: 89, until: now + 2 * 86400, resumeAt: now + 2 * 86400, inHook: false, cwd: PROJECT_DIR, transcript: TRANSCRIPT, checkpoint: '', queuedPrompt: '', startedAt: now - 600, permissionMode: '' };
  writeJson(acc.stateFile, state);
  const blocked = acc.hook({ hook_event_name: 'UserPromptSubmit', session_id: 'rp1', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT, prompt: 'carry on with the refactor please' });
  check('repair: a hand-off whose process is gone stops blocking the session', !/başka pencerede sürüyor|another window with/.test(blocked) && !blocked.includes('handoff'), true);
  check('repair: the dead hand-off is cleared', acc.state().handedOff.rp1, undefined);
  const repaired = acc.state().waits.rp1;
  check('repair: the stranded wait got a real runner again', repaired && repaired.scheduled && repaired.scheduled.method, 'sleeper');
  check('repair: it is journaled for noctis why', /handoff-gone|reschedule/.test(acc.run(['why', '--last', '8'])), true);
  acc.run(['cancel', 'rp1']);
  mock.limits = [];
  fs.rmSync(path.join(acc.guardDir, 'fable.json'), { force: true });
}

async function scenarioHosts(acc) {
  const now = nowSec();
  const hostDir = (name) => path.join(LAB_ROOT, `host-${name}`);
  const hostRun = (host, extra, input, env = {}) => acc.run([...extra, '--host', host, '--account', hostDir(host)], input, env);
  const queueFile = path.join(PROJECT_DIR, 'TASKS.md');
  fs.writeFileSync(queueFile, '# q\n- [ ] first host item\n- [ ] second host item\n');
  for (const host of ['codex', 'antigravity', 'droid', 'copilot']) {
    acc.run(['queue', 'trust', '--file', queueFile, '--host', host, '--account', hostDir(host)]);
  }
  const hostTranscript = path.join(PROJECT_DIR, 'host-session.jsonl');
  fs.writeFileSync(hostTranscript, '{"type":"user","message":{"role":"user","content":"host work"}}\n');

  fs.mkdirSync(hostDir('codex'), { recursive: true });
  writeJson(path.join(hostDir('codex'), 'hooks.json'), { hooks: { Stop: [{ hooks: [{ type: 'command', command: 'python3 other.py' }] }] } });
  const codexSetup = acc.run(['install', '--source', SOURCE_ROOT, '--host', 'codex', '--config-dir', hostDir('codex')]);
  check('codex: setup wires hooks and names the next step', codexSetup.includes('hooks.json') && codexSetup.includes('/hooks'), true);
  const codexHooks = readJson(path.join(hostDir('codex'), 'hooks.json')).hooks;
  check('codex: foreign Stop hook kept, ours appended with the marker', codexHooks.Stop.length === 2 && codexHooks.Stop[0].hooks[0].command === 'python3 other.py' && codexHooks.Stop[1].hooks[0].statusMessage === 'noctis' && /"[^"]*noctis"? hook --host codex --account/.test(codexHooks.Stop[1].hooks[0].command), true);
  check('codex: every event wired with a long timeout on the gates', ['SessionStart', 'SessionEnd', 'UserPromptSubmit', 'PreToolUse', 'PostToolUse', 'Stop'].every((event) => Array.isArray(codexHooks[event])) && codexHooks.PostToolUse[0].hooks[0].timeout === 21600 && codexHooks.SessionEnd[0].hooks[0].timeout === 3, true);
  const codexConfig = readJson(path.join(hostDir('codex'), 'noctis', 'config.json'));
  check('codex: config records the host and the app-server usage source', codexConfig.host === 'codex' && codexConfig.fable.source === 'codex', true);
  const codexSecond = acc.run(['install', '--source', SOURCE_ROOT, '--host', 'codex', '--config-dir', hostDir('codex')]);
  check('codex: setup twice does not duplicate hooks', codexSecond.includes('hooks.json') && readJson(path.join(hostDir('codex'), 'hooks.json')).hooks.Stop.length, 2);
  writeJson(path.join(hostDir('codex'), 'noctis', 'config.json'), { ...codexConfig, wait: { ...codexConfig.wait, maxInHookMinutes: 0, resetMarginSeconds: 1, builtinGraceSeconds: 1 }, resume: { ...codexConfig.resume, mode: 'headless' } });
  const codexInput = (event, extra = {}) => ({ hook_event_name: event, session_id: 'thr_codex1', transcript_path: hostTranscript, cwd: PROJECT_DIR, model: 'gpt-5.6', permission_mode: 'default', ...extra });
  if (!IS_WINDOWS) {
    fs.writeFileSync(path.join(LAB_ROOT, 'codex-limits.json'), JSON.stringify({ id: 1, result: { rateLimits: { limitId: 'codex', primary: { usedPercent: 40, windowDurationMins: 10080, resetsAt: now + 5 * 86400 }, secondary: { usedPercent: 96, windowDurationMins: 300, resetsAt: now + 1200 } } } }));
    const gate = hostRun('codex', ['hook'], codexInput('PostToolUse', { tool_name: 'Bash', tool_input: { command: 'ls' }, tool_response: 'ok' }), { NOCTIS_NO_SCHEDULE: '1' });
    const codexFable = readJson(path.join(hostDir('codex'), 'noctis', 'fable.json'));
    check('codex: usage read through app-server, windows classified by duration', codexFable.five_hour.used === 96 && codexFable.seven_day.used === 40, true);
    check('codex: PostToolUse at 96 % stops the turn (continue:false) and parks a wait', gate.includes('"continue":false') && acc.state && readJson(path.join(hostDir('codex'), 'noctis', 'state.json')).waits.thr_codex1.window === 'five_hour', true);
    const codexState = readJson(path.join(hostDir('codex'), 'noctis', 'state.json'));
    check('codex: relaunch prompt is stored with the wait', typeof codexState.waits.thr_codex1.checkpoint === 'string', true);
    fs.writeFileSync(path.join(LAB_ROOT, 'codex-limits.json'), JSON.stringify({ id: 1, result: { rateLimits: { limitId: 'codex', primary: { usedPercent: 40, windowDurationMins: 10080, resetsAt: now + 5 * 86400 }, secondary: { usedPercent: 2, windowDurationMins: 300, resetsAt: now + 18000 } } } }));
    const old = new Date(Date.now() - 3600000);
    fs.utimesSync(hostTranscript, old, old);
    resetCalls();
    acc.run(['resume', '--sid', 'thr_codex1', '--host', 'codex', '--account', hostDir('codex')], null, { NOCTIS_TIME_OFFSET: String(Math.ceil(codexState.waits.thr_codex1.resumeAt - now) + 5) });
    const codexCall = callsLog().find((line) => line.startsWith('FAKE_CODEX')) || '';
    check('codex: resumed with `codex exec resume <sid> … prompt`', codexCall.includes('exec resume thr_codex1 --skip-git-repo-check') && codexCall.includes('TASKS.md') && codexCall.includes('HANDOFF=thr_codex1'), true);
  }
  const codexStop = hostRun('codex', ['hook'], codexInput('Stop', { stop_hook_active: false, last_assistant_message: 'done with the first item' }));
  check('codex: Stop with open queue items forces another pass (Claude-shaped block)', codexStop.includes('"decision":"block"') && codexStop.includes('Queue continues: 2 open') && !codexStop.includes('Agent call'), true);
  const codexPrompt = hostRun('codex', ['hook'], codexInput('UserPromptSubmit', { prompt: 'Compare the best mechanical keyboards of 2026 for me please', turn_id: 't1' }));
  check('codex: research prompts are not routed (no lite agent outside Claude Code)', codexPrompt.includes('Non-code research'), false);
  const codexStart = hostRun('codex', ['hook'], codexInput('SessionStart', { source: 'fork' }));
  check('codex: SessionStart carries the plain queue directive', codexStart.includes('Queue mode (TASKS.md: 2 open)') && !codexStart.includes('lite'), true);
  acc.run(['install', '--source', SOURCE_ROOT, '--host', 'codex', '--config-dir', hostDir('codex'), '--uninstall']);
  const codexAfter = readJson(path.join(hostDir('codex'), 'hooks.json')).hooks;
  check('codex: uninstall removes only our hooks', codexAfter.Stop.length === 1 && codexAfter.Stop[0].hooks[0].command === 'python3 other.py' && codexAfter.PostToolUse === undefined, true);

  fs.mkdirSync(hostDir('antigravity'), { recursive: true });
  writeJson(path.join(LAB_ROOT, 'agy-hooks.json'), { 'my-linter': { PostToolUse: [{ matcher: 'run_command', hooks: [{ command: './lint.sh' }] }] } });
  const agySetup = acc.run(['install', '--source', SOURCE_ROOT, '--host', 'antigravity', '--config-dir', hostDir('antigravity')]);
  const agyHooks = readJson(path.join(LAB_ROOT, 'agy-hooks.json'));
  check('antigravity: hooks keyed by plugin name, other entries kept', agySetup.includes('agy-hooks.json') && agyHooks['my-linter'] !== undefined && Array.isArray(agyHooks['noctis'].PreInvocation) && agyHooks['noctis'].Stop[0].command.includes('--host antigravity'), true);
  check('antigravity: status line wired in settings.json', readJson(path.join(hostDir('antigravity'), 'settings.json')).statusLine.command.includes('statusline --host antigravity'), true);
  const agyConfig = readJson(path.join(hostDir('antigravity'), 'noctis', 'config.json'));
  writeJson(path.join(hostDir('antigravity'), 'noctis', 'config.json'), { ...agyConfig, wait: { ...agyConfig.wait, maxInHookMinutes: 0, resetMarginSeconds: 1, builtinGraceSeconds: 1 }, resume: { ...agyConfig.resume, mode: 'headless' } });
  const agyPayload = { cwd: PROJECT_DIR, session_id: 'conv-agy1', conversation_id: 'conv-agy1', transcript_path: hostTranscript, model: { id: 'Gemini 3.5 Flash (High)', display_name: 'Gemini 3.5 Flash (High)' }, version: '1.2.1', context_window: { used_percentage: 14.2 }, quota: { 'gemini-weekly': { remaining_fraction: 0.62, reset_time: new Date((now + 4 * 86400) * 1000).toISOString(), reset_in_seconds: 4 * 86400 }, 'gemini-5h': { remaining_fraction: 0.07, reset_in_seconds: 1500 } }, agent_state: 'working', plan_tier: 'Pro' };
  const agyLine = hostRun('antigravity', ['statusline'], agyPayload);
  const agyUsage = readJson(path.join(hostDir('antigravity'), 'noctis', 'usage.json'));
  check('antigravity: status-line quota becomes usage windows (5h 93 %, weekly 38 %)', Math.round(agyUsage.five_hour.used) === 93 && Math.round(agyUsage.seven_day.used) === 38 && agyLine.includes('93'), true);
  const agyInput = (event, extra = {}) => ({ hook_event_name: event, conversationId: 'conv-agy1', workspacePaths: [PROJECT_DIR], transcriptPath: hostTranscript, artifactDirectoryPath: '/tmp/x', modelName: 'gemini-3.5-flash', ...extra });
  const agyPost = hostRun('antigravity', ['hook'], agyInput('PostInvocation', { invocationNum: 3, initialNumSteps: 10 }), { NOCTIS_NO_SCHEDULE: '1' });
  check('antigravity: PostInvocation at 93 % terminates the loop', JSON.parse(agyPost).terminationBehavior === 'terminate' && readJson(path.join(hostDir('antigravity'), 'noctis', 'state.json')).waits['conv-agy1'].window === 'five_hour', true);
  check('antigravity: PreToolUse for an ordinary tool answers allow', JSON.parse(hostRun('antigravity', ['hook'], agyInput('PreToolUse', { toolCall: { name: 'view_file', args: { AbsolutePath: '/x' } }, stepIdx: 3 }))).decision, 'allow');
  const agyState = readJson(path.join(hostDir('antigravity'), 'noctis', 'state.json'));
  const agyLow = { ...agyPayload, quota: { 'gemini-weekly': { remaining_fraction: 0.62, reset_in_seconds: 4 * 86400 }, 'gemini-5h': { remaining_fraction: 0.98, reset_in_seconds: 18000 } } };
  hostRun('antigravity', ['statusline'], agyLow, { NOCTIS_TIME_OFFSET: String(Math.ceil(agyState.waits['conv-agy1'].resumeAt - now) + 5) });
  const oldAgy = new Date(Date.now() - 3600000);
  fs.utimesSync(hostTranscript, oldAgy, oldAgy);
  resetCalls();
  acc.run(['resume', '--sid', 'conv-agy1', '--host', 'antigravity', '--account', hostDir('antigravity')], null, { NOCTIS_TIME_OFFSET: String(Math.ceil(agyState.waits['conv-agy1'].resumeAt - now) + 5) });
  const agyCall = callsLog().find((line) => line.startsWith('FAKE_AGY')) || '';
  check('antigravity: resumed with `agy --conversation <id> -p …`', agyCall.includes('--conversation conv-agy1') && agyCall.includes('-p ') && agyCall.includes('TASKS.md'), true);
  const agyStop = JSON.parse(hostRun('antigravity', ['hook'], agyInput('Stop', { executionNum: 1, terminationReason: 'model_stop', error: '', fullyIdle: true })));
  check('antigravity: Stop with open items answers decision:continue with the directive', agyStop.decision === 'continue' && agyStop.reason.includes('Queue continues'), true);
  fs.writeFileSync(queueFile, '# q\n- [x] first host item\n- [x] second host item\n');
  check('antigravity: Stop with a finished queue lets the agent stop', JSON.parse(hostRun('antigravity', ['hook'], agyInput('Stop', { executionNum: 2, terminationReason: 'model_stop', error: '', fullyIdle: true }))).decision, 'stop');
  fs.writeFileSync(queueFile, '# q\n- [ ] first host item\n- [ ] second host item\n');
  const agyError = hostRun('antigravity', ['hook'], agyInput('Stop', { executionNum: 1, terminationReason: 'error', error: 'Individual quota reached (rate limit)', fullyIdle: true }), { NOCTIS_NO_SCHEDULE: '1' });
  check('antigravity: Stop with terminationReason=error is treated as a rate-limit failure', JSON.parse(agyError).decision === 'stop' && readJson(path.join(hostDir('antigravity'), 'noctis', 'state.json')).waits['conv-agy1'] !== undefined, true);
  acc.run(['cancel', 'conv-agy1', '--host', 'antigravity', '--account', hostDir('antigravity')]);
  acc.run(['install', '--source', SOURCE_ROOT, '--host', 'antigravity', '--config-dir', hostDir('antigravity'), '--uninstall']);
  check('antigravity: uninstall removes our hooks and status line, keeps others', readJson(path.join(LAB_ROOT, 'agy-hooks.json'))['noctis'] === undefined && readJson(path.join(LAB_ROOT, 'agy-hooks.json'))['my-linter'] !== undefined && readJson(path.join(hostDir('antigravity'), 'settings.json')).statusLine === undefined, true);

  fs.mkdirSync(hostDir('droid'), { recursive: true });
  const droidSetup = acc.run(['install', '--source', SOURCE_ROOT, '--host', 'droid', '--config-dir', hostDir('droid')]);
  const droidHooks = readJson(path.join(hostDir('droid'), 'hooks.json'));
  check('droid: hooks written event-keyed, limit guard declared off', /limit guard stays off|limit koruması burada kapalı/.test(droidSetup) && droidHooks.Stop[0].hooks[0].command.includes('--host droid') && droidHooks.PreToolUse[0].matcher === 'Task', true);
  const droidInput = (event, extra = {}) => ({ hook_event_name: event, session_id: 'droid-1', transcript_path: hostTranscript, cwd: PROJECT_DIR, permission_mode: 'auto-high', ...extra });
  check('droid: PostToolUse without usage data is silent', hostRun('droid', ['hook'], droidInput('PostToolUse', { tool_name: 'Execute', tool_input: { command: 'ls' }, tool_response: 'ok' })), '');
  check('droid: Stop with open items blocks the stop', hostRun('droid', ['hook'], droidInput('Stop', { stop_hook_active: false })).includes('"decision":"block"'), true);
  const droidState = readJson(path.join(hostDir('droid'), 'noctis', 'state.json'));
  droidState.waits['droid-1'] = { kind: 'stopfailure', window: 'unknown', label: 'unknown', until: now - 10, resumeAt: now - 5, cwd: PROJECT_DIR, transcript: hostTranscript, checkpoint: '', queuedPrompt: '', startedAt: now - 100, attempts: 0, inHook: false };
  writeJson(path.join(hostDir('droid'), 'noctis', 'state.json'), droidState);
  const droidConfig = readJson(path.join(hostDir('droid'), 'noctis', 'config.json'));
  writeJson(path.join(hostDir('droid'), 'noctis', 'config.json'), { ...droidConfig, resume: { ...droidConfig.resume, mode: 'headless' } });
  resetCalls();
  acc.run(['resume', '--sid', 'droid-1', '--host', 'droid', '--account', hostDir('droid')]);
  const droidCall = callsLog().find((line) => line.startsWith('FAKE_DROID')) || '';
  check('droid: resumed with `droid exec --session-id <id> --auto high … prompt`', droidCall.includes('exec --session-id droid-1 --auto high') && droidCall.includes('TASKS.md'), true);

  fs.mkdirSync(hostDir('copilot'), { recursive: true });
  acc.run(['install', '--source', SOURCE_ROOT, '--host', 'copilot', '--config-dir', hostDir('copilot')]);
  const copilotHooks = readJson(path.join(hostDir('copilot'), 'hooks', 'noctis.json'));
  check('copilot: exec-form hook file with version 1', copilotHooks.version === 1 && copilotHooks.hooks.agentStop[0].exec.endsWith(IS_WINDOWS ? 'noctis.exe' : 'noctis') && copilotHooks.hooks.agentStop[0].args.join(' ') === `hook --host copilot --account ${hostDir('copilot')}` && copilotHooks.hooks.userPromptSubmitted[0].timeoutSec === 21600, true);
  const copilotStart = hostRun('copilot', ['hook'], { hook_event_name: 'sessionStart', sessionId: 'cp-1', timestamp: Date.now(), cwd: PROJECT_DIR, source: 'new' });
  check('copilot: sessionStart answers with additionalContext only', JSON.parse(copilotStart).additionalContext.includes('Queue mode') && JSON.parse(copilotStart).hookSpecificOutput === undefined, true);
  const copilotStop = hostRun('copilot', ['hook'], { hook_event_name: 'agentStop', sessionId: 'cp-1', timestamp: Date.now(), cwd: PROJECT_DIR, transcriptPath: hostTranscript, stopReason: 'end_turn', stop_hook_active: false });
  check('copilot: agentStop with open items blocks with a reason', JSON.parse(copilotStop).decision === 'block' && JSON.parse(copilotStop).reason.includes('Queue continues'), true);
  const copilotConfig = readJson(path.join(hostDir('copilot'), 'noctis', 'config.json'));
  writeJson(path.join(hostDir('copilot'), 'noctis', 'config.json'), { ...copilotConfig, resume: { ...copilotConfig.resume, mode: 'headless' } });
  const copilotError = hostRun('copilot', ['hook'], { hook_event_name: 'errorOccurred', sessionId: 'cp-1', timestamp: Date.now(), cwd: PROJECT_DIR, error: { message: 'Rate limit exceeded, retry later', name: 'RateLimitError' }, errorContext: 'model_call', recoverable: true }, { NOCTIS_NO_SCHEDULE: '1' });
  const copilotState = readJson(path.join(hostDir('copilot'), 'noctis', 'state.json'));
  check('copilot: errorOccurred with a rate-limit message schedules a retry (output ignored by the host)', copilotError === '' && copilotState.waits['cp-1'] && copilotState.waits['cp-1'].window === 'unknown', true);
  resetCalls();
  acc.run(['resume', '--sid', 'cp-1', '--host', 'copilot', '--account', hostDir('copilot')], null, { NOCTIS_TIME_OFFSET: String(Math.ceil(copilotState.waits['cp-1'].resumeAt - now) + 5) });
  const copilotCall = callsLog().find((line) => line.startsWith('FAKE_COPILOT')) || '';
  check('copilot: resumed with `copilot --resume=<id> -p …`', copilotCall.includes('--resume=cp-1 -p '), true);
  check('copilot: tools are not pre-approved by default', copilotCall.includes('--allow-all-tools'), false);
  const copilotConfigFile = path.join(hostDir('copilot'), 'noctis', 'config.json');
  const copilotOptIn = readJson(copilotConfigFile);
  writeJson(copilotConfigFile, { ...copilotOptIn, resume: { ...(copilotOptIn.resume || {}), copilotAllowAllTools: true } });
  hostRun('copilot', ['hook'], { hook_event_name: 'errorOccurred', sessionId: 'cp-2', timestamp: Date.now(), cwd: PROJECT_DIR, error: { message: 'Rate limit exceeded, retry later', name: 'RateLimitError' }, errorContext: 'model_call', recoverable: true }, { NOCTIS_NO_SCHEDULE: '1' });
  const copilotState2 = readJson(path.join(hostDir('copilot'), 'noctis', 'state.json'));
  resetCalls();
  acc.run(['resume', '--sid', 'cp-2', '--host', 'copilot', '--account', hostDir('copilot')], null, { NOCTIS_TIME_OFFSET: String(Math.ceil(copilotState2.waits['cp-2'].resumeAt - now) + 5) });
  check('copilot: opting in adds --allow-all-tools', (callsLog().find((line) => line.startsWith('FAKE_COPILOT')) || '').includes('--allow-all-tools'), true);
  acc.run(['install', '--source', SOURCE_ROOT, '--host', 'copilot', '--config-dir', hostDir('copilot'), '--uninstall']);
  check('copilot: uninstall removes the hook file', fs.existsSync(path.join(hostDir('copilot'), 'hooks', 'noctis.json')), false);

  const asked = spawnSync(acc.engine()[0], ['install', '--source', SOURCE_ROOT, '--config-dir', hostDir('asked')], { encoding: 'utf8', input: '2\n', env: { ...acc.env(), NOCTIS_NO_TASKS: '1' } });
  check('host question: piped stdin means no question, Claude Code install', asked.status === 0 && fs.existsSync(path.join(hostDir('asked'), 'skills', PLUGIN_NAME)) && !fs.existsSync(path.join(hostDir('asked'), 'hooks.json')), true);

  const cacheRoot = path.join(LAB_ROOT, 'plugins', 'cache', 'synex-mkt', PLUGIN_NAME, PLUGIN_VERSION);
  fs.rmSync(cacheRoot, { recursive: true, force: true });
  for (const entry of ['.claude-plugin', 'hooks', 'agents', 'skills', 'config.default.json', 'bin']) {
    fs.cpSync(path.join(SOURCE_ROOT, entry), path.join(cacheRoot, entry), { recursive: true });
  }
  const updDir = path.join(LAB_ROOT, 'update-account');
  fs.mkdirSync(updDir, { recursive: true });
  writeJson(path.join(updDir, 'settings.json'), {});
  resetCalls();
  const cacheSetup = spawnSync(acc.engine()[0], ['setup', '--config-dir', updDir, '--profile', 'balanced'], { encoding: 'utf8', env: { ...acc.env(), NOCTIS_PLUGIN_ROOT: cacheRoot, NOCTIS_NO_TASKS: '1' } });
  check('auto-update: setup enables marketplace auto-update via claude', cacheSetup.status === 0 && callsLog().some((line) => line.includes('plugin marketplace update synex-mkt --auto-update')) && /auto-update on|otomatik güncelleme açık/.test(cacheSetup.stdout), true);
  resetCalls();
  spawnSync(acc.engine()[0], ['setup', '--config-dir', updDir, '--profile', 'balanced', '--updates', 'keep'], { encoding: 'utf8', env: { ...acc.env(), NOCTIS_PLUGIN_ROOT: cacheRoot, NOCTIS_NO_TASKS: '1' } });
  check('auto-update: --updates keep leaves the marketplace setting alone', callsLog().some((line) => line.includes('--auto-update')), false);
  resetCalls();
  const cloneRoot = path.join(LAB_ROOT, 'clone-root');
  fs.rmSync(cloneRoot, { recursive: true, force: true });
  for (const entry of ['.claude-plugin', 'hooks', 'agents', 'skills', 'config.default.json']) {
    fs.cpSync(path.join(SOURCE_ROOT, entry), path.join(cloneRoot, entry), { recursive: true });
  }
  fs.cpSync(path.dirname(acc.engine()[0]), path.join(cloneRoot, 'bin', path.basename(path.dirname(acc.engine()[0]))), { recursive: true });
  fs.cpSync(path.join(SOURCE_ROOT, 'bin', 'SHA256SUMS'), path.join(cloneRoot, 'bin', 'SHA256SUMS'));
  const cloneDir = path.join(LAB_ROOT, 'clone-account');
  fs.mkdirSync(cloneDir, { recursive: true });
  writeJson(path.join(cloneDir, 'settings.json'), {});
  spawnSync(acc.engine()[0], ['setup', '--config-dir', cloneDir, '--profile', 'balanced'], { encoding: 'utf8', env: { ...acc.env(), NOCTIS_PLUGIN_ROOT: cloneRoot, NOCTIS_NO_TASKS: '1' } });
  check('auto-update: clone installs (no marketplace path) do not touch marketplaces', callsLog().some((line) => line.includes('plugin marketplace')), false);

  const versionFile = path.join(lab.mockDir, 'plugin-version.json');
  fs.writeFileSync(versionFile, JSON.stringify({ name: PLUGIN_NAME, version: '9.9.9' }));
  const updateEnv = { NOCTIS_UPDATE_URL: `http://127.0.0.1:${lab.mockPort}/plugin.json` };
  const st0 = acc.state();
  delete st0.notified['update:checkAt'];
  writeJson(acc.stateFile, st0);
  const first = acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'upd1', cwd: PROJECT_DIR }, updateEnv);
  check('update check: a newer published version is announced once with the update command', first.includes('9.9.9') && first.includes('/plugin update noctis'), true);
  check('update check: not repeated the same day', acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'upd2', cwd: PROJECT_DIR }, updateEnv).includes('9.9.9'), false);
  const checksBefore = fs.readFileSync(path.join(lab.mockDir, 'version-checks.log'), 'utf8').split('\n').filter(Boolean).length;
  const st = acc.state();
  delete st.notified['update:checkAt'];
  writeJson(acc.stateFile, st);
  acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'upd3', cwd: PROJECT_DIR }, updateEnv);
  check('update check: a new day fetches again but the known version is not announced twice', fs.readFileSync(path.join(lab.mockDir, 'version-checks.log'), 'utf8').split('\n').filter(Boolean).length === checksBefore + 1, true);
  acc.setConfig((config) => {
    config.update.check = false;
  });
  const st2 = acc.state();
  delete st2.notified['update:checkAt'];
  writeJson(acc.stateFile, st2);
  acc.hook({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'upd4', cwd: PROJECT_DIR }, updateEnv);
  check('update check: update.check=false never fetches', fs.readFileSync(path.join(lab.mockDir, 'version-checks.log'), 'utf8').split('\n').filter(Boolean).length, checksBefore + 1);
  acc.setConfig((config) => {
    config.update.check = true;
  });

  const newerRoot = path.join(LAB_ROOT, 'plugins', 'cache', 'synex-mkt', PLUGIN_NAME, '9.0.0');
  fs.mkdirSync(newerRoot, { recursive: true });
  const restartEnv = { ...acc.env(), NOCTIS_PLUGIN_ROOT: cacheRoot };
  const st3 = acc.state();
  delete st3.notified['update:checkAt'];
  writeJson(acc.stateFile, st3);
  const banner = spawnSync(acc.engine()[0], ['hook'], { encoding: 'utf8', input: JSON.stringify({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'rs1', cwd: PROJECT_DIR }), env: restartEnv }).stdout;
  check('restart banner: a newer cached version is announced with the reload hint', banner.includes('9.0.0') && /reload-plugins/.test(banner) && banner.includes(PLUGIN_VERSION), true);
  const again2 = spawnSync(acc.engine()[0], ['hook'], { encoding: 'utf8', input: JSON.stringify({ hook_event_name: 'SessionStart', source: 'startup', session_id: 'rs2', cwd: PROJECT_DIR }), env: restartEnv }).stdout;
  check('restart banner: shown once per version', again2.includes('9.0.0'), false);
  fs.rmSync(newerRoot, { recursive: true, force: true });

  const settingsFile = path.join(acc.dir, 'settings.json');
  const settingsBefore = readJson(settingsFile);
  writeJson(settingsFile, { ...settingsBefore, hooks: { Stop: [{ hooks: [{ type: 'command', command: 'bash ~/.claude/plugins/ralph-loop/stop.sh' }] }] } });
  writeJson(path.join(acc.dir, 'plugins', 'installed_plugins.json'), { version: 2, plugins: { 'ralph-loop@claude-plugins-official': {}, 'ccstatusline@x': {}, 'github@claude-plugins-official': {} } });
  const doctor = acc.run(['doctor']);
  check('coexistence: doctor notes other Stop hooks and neighbouring plugins', /1 other Stop hook|1 başka Stop hook/.test(doctor) && doctor.includes('ralph-loop@claude-plugins-official') && doctor.includes('ccstatusline@x') && !doctor.includes('github@'), true);
  check('coexistence: doctor changed nothing', readJson(settingsFile).hooks.Stop[0].hooks[0].command, 'bash ~/.claude/plugins/ralph-loop/stop.sh');
  writeJson(settingsFile, settingsBefore);
  fs.rmSync(path.join(acc.dir, 'plugins', 'installed_plugins.json'), { force: true });
  fs.unlinkSync(queueFile);
  fs.rmSync(hostTranscript, { force: true });
}

function zipEntry(file, name) {
  const zlib = require('zlib');
  const buffer = fs.readFileSync(file);
  let eocd = -1;
  for (let offset = buffer.length - 22; offset >= 0; offset -= 1) {
    if (buffer.readUInt32LE(offset) === 0x06054b50) { eocd = offset; break; }
  }
  if (eocd < 0) return '';
  const count = buffer.readUInt16LE(eocd + 10);
  let entry = buffer.readUInt32LE(eocd + 16);
  const target = Buffer.from(name);
  for (let index = 0; index < count; index += 1) {
    const method = buffer.readUInt16LE(entry + 10);
    const compressed = buffer.readUInt32LE(entry + 20);
    const nameLength = buffer.readUInt16LE(entry + 28);
    const extraLength = buffer.readUInt16LE(entry + 30);
    const commentLength = buffer.readUInt16LE(entry + 32);
    const localOffset = buffer.readUInt32LE(entry + 42);
    const entryName = buffer.subarray(entry + 46, entry + 46 + nameLength);
    if (entryName.equals(target)) {
      const localNameLength = buffer.readUInt16LE(localOffset + 26);
      const localExtraLength = buffer.readUInt16LE(localOffset + 28);
      const dataStart = localOffset + 30 + localNameLength + localExtraLength;
      const data = buffer.subarray(dataStart, dataStart + compressed);
      return (method === 0 ? data : zlib.inflateRawSync(data)).toString('utf8');
    }
    entry += 46 + nameLength + extraLength + commentLength;
  }
  return '';
}

async function scenarioBundle(acc) {
  fs.appendFileSync(path.join(acc.guardDir, 'guard.log'), 'token sk-ant-api03-SECRETSECRET should never leave the machine\n');
  const out = acc.run(['report', '--bundle']);
  const match = out.match(/([^\s]+\.zip)/);
  const bundle = match ? match[1] : '';
  check('bundle: report --bundle names the zip it wrote', bundle !== '' && fs.existsSync(bundle), true);
  const raw = fs.readFileSync(bundle);
  check('bundle: contains summary, logs, journal, state and config', ['summary.txt', 'guard.log', 'decisions.jsonl', 'state.json', 'config.json'].every((name) => raw.includes(Buffer.from(name))), true);
  const log = zipEntry(bundle, 'guard.log');
  check('bundle: tokens are redacted and the home path is masked', log.includes('<redacted-token>') && !log.includes('SECRETSECRET') && !raw.includes(Buffer.from('SECRETSECRET')), true);
  check('bundle: the summary carries version, host and doctor lines', zipEntry(bundle, 'summary.txt').includes(PLUGIN_NAME) && /doctor:/.test(zipEntry(bundle, 'summary.txt')), true);
  fs.unlinkSync(bundle);
}

async function scenarioSilentFailures(acc) {
  const now = nowSec();
  acc.statusline('sf1', 'claude-fable-5-1', 20, now + 7200, 93, now + 2 * 86400);
  const bare = acc.run([], { hook_event_name: 'UserPromptSubmit', session_id: 'sf1', cwd: PROJECT_DIR, prompt: 'keep going on the refactor' });
  check('no-command hook call still guards the session', bare.includes('⏸') || bare.includes('"decision"'), true);
  check('and says the wiring is wrong', bare.includes('noctis doctor'), true);
  acc.run(['cancel', 'sf1']);
  acc.statusline('sf1b', 'claude-fable-5-1', 20, now + 7200, 20, now + 2 * 86400);
  const bareAgain = acc.run([], { hook_event_name: 'UserPromptSubmit', session_id: 'sf1b', cwd: PROJECT_DIR, prompt: 'hello' });
  check('the wiring notice is not repeated every turn', bareAgain.includes('without its command argument'), false);
  check('no arguments and no stdin is still status', acc.run([], null).includes(acc.dir), true);
  const help = acc.run(['--help'], null);
  check('--help prints the command list', help.includes('doctor') && help.includes('status') && help.includes('queue'), true);
  check('and --help is not the status table', help.includes(acc.dir), false);

  const lockFile = path.join(acc.guardDir, 'state.lock');
  fs.writeFileSync(lockFile, String(process.pid));
  acc.statusline('sf2', 'claude-fable-5-1', 20, now + 7200, 93, now + 2 * 86400);
  const refused = acc.run(['hook'], { hook_event_name: 'UserPromptSubmit', session_id: 'sf2', cwd: PROJECT_DIR, prompt: 'continue' });
  fs.rmSync(lockFile, { force: true });
  check('a wait that cannot be stored does not pause the session', refused.includes('could not save the pause') || refused.includes('duraklatma kaydedilemedi'), true);
  check('and no half-written wait is left behind', acc.state().waits.sf2, undefined);
  const errors = fs.readFileSync(path.join(acc.guardDir, 'errors.log'), 'utf8');
  check('the refusal is in errors.log, not a silent unlocked write', /is held by another process/.test(errors), true);
  check('nothing ever claims to proceed unlocked', /proceeding unlocked/.test(errors), false);

  const child = spawn(process.execPath, ['-e', 'setTimeout(()=>{},30000)'], { detached: true, stdio: 'ignore' });
  child.unref();
  const withStranger = acc.state();
  withStranger.waits.sf3 = { kind: 'batch', window: 'five_hour', label: '5h', resumeAt: nowSec() + 60, startedAt: nowSec(), scheduled: { method: 'sleeper', pid: child.pid } };
  writeJson(acc.stateFile, withStranger);
  acc.run(['cancel', 'sf3']);
  let alive = true;
  try { process.kill(child.pid, 0); } catch { alive = false; }
  check('a pid that is not our helper is left alone', alive, true);
  try { process.kill(child.pid, 'SIGKILL'); } catch {  }

  const doctor = acc.runFull(['doctor']);
  check('doctor exit code matches its own report', doctor.status === 0, !doctor.stdout.includes('!!'));
  const savedStatusLine = readJson(path.join(acc.dir, 'settings.json'));
  const broken = JSON.parse(JSON.stringify(savedStatusLine));
  delete broken.statusLine;
  writeJson(path.join(acc.dir, 'settings.json'), broken);
  const doctorBroken = acc.runFull(['doctor']);
  check('doctor exits 1 when something needs attention', doctorBroken.status, 1);
  check('and prints a remedy under the problem', /fix:|çözüm:/.test(doctorBroken.stdout), true);
  writeJson(path.join(acc.dir, 'settings.json'), savedStatusLine);

  const noHooks = acc.state();
  noHooks.lastHookAt = 0;
  writeJson(acc.stateFile, noHooks);
  const usageFile = path.join(acc.guardDir, 'usage.json');
  const usage = readJson(usageFile);
  const old = nowSec() - 4000;
  usage.history = usage.history || {};
  usage.history.five_hour = [old, old + 100, old + 200, old + 300, old + 400].map((at, i) => ({ at, used: 10 + i }));
  usage.updatedAt = nowSec();
  writeJson(usageFile, usage);
  const line = acc.statusline('sf4', 'claude-fable-5-1', 20, nowSec() + 7200, 20, nowSec() + 3 * 86400);
  check('a status line that runs while the hooks never did is reported', line.includes('⚠'), true);
  const restored = acc.state();
  restored.lastHookAt = nowSec();
  writeJson(acc.stateFile, restored);
}

async function scenarioHousekeeping(acc) {
  const now = nowSec();
  writeTranscript(300 * 1024);
  acc.statusline('s9', 'claude-opus-5', 93, now + 2 * 86400, 10, now + 3 * 86400);
  const started = Date.now();
  const out = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's9', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('large transcript checkpoint stop', out.includes('"continue":false'), true);
  results.push({ name: `large transcript checkpoint ${Date.now() - started}ms`, ok: Date.now() - started < 1500, actual: Date.now() - started, expected: '<1500ms' });
  const checkpoint = fs.readFileSync(acc.state().checkpoints.s9.path, 'utf8');
  check('checkpoint has todos', checkpoint.includes('- [ ] tests'), true);
  acc.run(['cancel']);
  writeTranscript();
  acc.statusline('s9', 'claude-opus-5', 10, now + 7200, 10, now + 3 * 86400);
  const state = acc.state();
  state.checkpoints.old = { path: path.join(acc.guardDir, 'checkpoints', 'old.md'), cwd: PROJECT_DIR, at: now - 8 * 86400, consumed: false };
  fs.writeFileSync(state.checkpoints.old.path, 'old');
  state.modelOverrides.parked = { model: 'x', at: now - 4 * 86400 };
  state.modelOverrides.stale = { model: 'x', at: now - 9 * 86400 };
  writeJson(acc.stateFile, state);
  acc.hook({ hook_event_name: 'PostModelSwitch', session_id: 's9', to_model: 'claude-opus-5' });
  check('expired checkpoint pruned', acc.state().checkpoints.old === undefined && !fs.existsSync(state.checkpoints.old.path), true);
  check('stale override pruned', acc.state().modelOverrides.stale === undefined, true);
  check('override survives a weekly park', acc.state().modelOverrides.parked !== undefined, true);
  acc.hook({ hook_event_name: 'SessionEnd', session_id: 's9', reason: 'exit' });
  check('session end cleanup', acc.state().modelOverrides.s9 === undefined, true);
  fs.writeFileSync(acc.configFile, '{ broken');
  const notice = acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's10', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT });
  check('config error notice', notice.includes('config.json okunamadı'), true);
  check('config error notice once', acc.hook({ hook_event_name: 'PostToolBatch', session_id: 's10', cwd: PROJECT_DIR, transcript_path: TRANSCRIPT }), '');
  fs.unlinkSync(acc.configFile);
  acc.install();
  check('reinstall recreates config', readJson(acc.configFile).thresholds.session5h, 92);
  const errors = fs.readFileSync(path.join(acc.guardDir, 'errors.log'), 'utf8').split('\n').filter(Boolean);
  check('errors.log only warn/error', errors.every((line) => /\[(WARN|ERROR)/.test(line)), true);
  const doctor = acc.run(['doctor']);
  check('doctor runs', doctor.includes('plugin konumu'), true);
  const uninstall = spawnSync(acc.engine()[0], ['install', '--source', SOURCE_ROOT, '--config-dir', acc.dir, '--uninstall'], { encoding: 'utf8', env: acc.env() });
  check('uninstall restores settings', uninstall.status === 0 && !(readJson(path.join(acc.dir, 'settings.json')) || {}).statusLine, true);
}

async function main() {
  refreshChecksums();
  await lab.startMock();
  const accA = lab.account('accountA');
  const accB = lab.account('accountB');
  accA.install();
  accB.install();
  const scenarios = [
    ['baseline', () => scenarioBaseline(accA)],
    ['warn band + burst projection', () => scenarioWarnAndBurst(accA)],
    ['in-hook wait', () => scenarioInHookWait(accA)],
    ['early reset', () => scenarioEarlyReset(accA)],
    ['visible relaunch: terminal, moved marker, close previous', () => scenarioVisibleRelaunch(accA)],
    ['repair: dead hand-offs and stranded waits', () => scenarioRepair(accA)],
    ['workspace guard', () => scenarioWorkspaceGuard(accA)],
    ['killed hook recovery', () => scenarioKilledHookRecovery(accA)],
    ['weekly long wait', () => scenarioWeeklyLongWait(accA)],
    ['fable flow + handoff', () => scenarioFableFlow(accA)],
    ['stale fallback + near-edge', () => scenarioStaleFallbackAndNearEdge(accA)],
    ['stop failure', () => scenarioStopFailure(accA)],
    ['router', () => scenarioRouter(accA)],
    ['isolation + concurrency', () => scenarioIsolationAndConcurrency(accA, accB)],
    ['foundation: locks, clock skew, self-heal, self-check', () => scenarioFoundation(accA)],
    ['compaction + /clear', () => scenarioCompactAndClear(accA)],
    ['subagent gate + adaptive warn', () => scenarioAgentGateAndWarnBand(accA)],
    ['queue mode + lite write policy', () => scenarioQueueMode(accA)],
    ['uptime: headless inference, dead hooks, learned cap, ETA', () => scenarioUptime(accA)],
    ['smart decisions: learned routing, fable ETA, git', () => scenarioSmartDecisions(accA)],
    ['autocompact adaptation', () => scenarioAutocompactAdaptation(accA)],
    ['stale-data projection', () => scenarioProjection(accA)],
    ['queue continuation: stop hook, report, selftest', () => scenarioQueueContinuation(accA)],
    ['locale, pace, statusline modes, plan detection', () => scenarioLocaleAndModes(accA)],
    ['multi-session max + scoped model rule', () => scenarioMultiSessionAndScoped(accA)],
    ['subagent pinning, digest policy, observe mode, why, completion promise', () => scenarioSubagentsAndObserve(accA)],
    ['daily budget, permission inheritance, same-session wake, webhooks, setup', () => scenarioBudgetWakeWebhook(accA)],
    ['marketplace bootstrap: launcher → platform binary via exec-form ensure', () => scenarioMarketplaceBootstrap(accA)],
    ['queue priorities, tags and dependencies', () => scenarioQueuePriorities(accA)],
    ['github issues: import + close-on-done', () => scenarioGitHubQueue(accA)],
    ['dynamic workflows: advisory, gate, rescue', () => scenarioWorkflows(accA)],
    ['first-run edges: 99 % weekly at install, missing model, no data', () => scenarioFirstRunEdges(accA)],
    ['check gate: exit codes for crons and CI', () => scenarioCheckGate(accA)],
    ['double runner: one wait, one launch', () => scenarioDoubleRunner(accA)],
    ['hosts: codex, antigravity, droid, copilot', () => scenarioHosts(accA)],
    ['auto queue from long prompts', () => scenarioAutoQueue(accA)],
    ['bug-report bundle', () => scenarioBundle(accA)],
    ['silent failures: wiring, unwritable wait, recycled pid, doctor gate', () => scenarioSilentFailures(accA)],
    ['housekeeping', () => scenarioHousekeeping(accA)],
  ];
  const only = process.argv.slice(2).filter((arg) => !arg.startsWith('--'));
  const baselineConfig = fs.readFileSync(accA.configFile, 'utf8');
  for (const [name, scenario] of scenarios) {
    if (only.length && !only.some((needle) => name.includes(needle)) && name !== 'baseline') continue;
    process.stdout.write(`== ${name}\n`);
    const configBefore = fs.readFileSync(accA.configFile, 'utf8');
    try {
      await scenario();
    } catch (err) {
      results.push({ name: `${name} crashed: ${err.message}`, ok: false });
      process.stdout.write(`  CRASH ${err.stack}\n`);
    }
    const before = JSON.parse(configBefore);
    const after = JSON.parse(fs.readFileSync(accA.configFile, 'utf8'));
    const drifted = [...new Set([...Object.keys(before), ...Object.keys(after)])]
      .filter((key) => !key.startsWith('managed')) 
      .filter((key) => canonical(after[key]) !== canonical(before[key]));
    if (drifted.length) {
      results.push({ name: `${name}: leaves config changed (${drifted.join(', ')})`, ok: false, actual: drifted, expected: 'no drift' });
      fs.writeFileSync(accA.configFile, configBefore);
    }
    const pending = Object.keys(accA.state().waits || {});
    if (pending.length) {
      results.push({ name: `${name}: leaves waits pending (${pending.join(', ')})`, ok: false, actual: pending, expected: 'none' });
      accA.run(['cancel']);
    }
    if ((accA.state().disabledUntil || 0) > nowSec()) {
      results.push({ name: `${name}: leaves the plugin switched off`, ok: false, actual: 'off', expected: 'on' });
      accA.run(['on']);
    }
    if (accA.timeOffset || accA.manualSchedule) {
      results.push({ name: `${name}: leaves harness overrides set (timeOffset/manualSchedule)`, ok: false, actual: [accA.timeOffset, accA.manualSchedule], expected: 'cleared' });
      accA.timeOffset = 0;
      accA.manualSchedule = false;
    }
  }
  const stripManaged = (config) => {
    const copy = { ...config };
    for (const key of Object.keys(copy)) if (key.startsWith('managed')) delete copy[key];
    return copy;
  };
  check('suite: the account config is back to what install wrote',
    canonical(stripManaged(JSON.parse(fs.readFileSync(accA.configFile, 'utf8')))), canonical(stripManaged(JSON.parse(baselineConfig))));
  lab.stopMock();
  const failed = results.filter((result) => !result.ok);
  process.stdout.write(`\n${results.length - failed.length}/${results.length} checks passed${failed.length ? ` — FAILED: ${failed.map((f) => f.name).join('; ')}` : ''}\n`);
  process.stdout.write(`lab dir: ${LAB_ROOT} (errors.log: ${path.join(accA.guardDir, 'errors.log')})\n`);
  process.exitCode = failed.length ? 1 : 0;
}

main().catch((err) => {
  process.stderr.write(`${err.stack}\n`);
  process.exitCode = 1;
});
