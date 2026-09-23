#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const http = require('http');
const { spawnSync } = require('child_process');
const { Lab, writeJson, readJson } = require('./harness.js');

let checks = 0;
const failures = [];

function check(label, condition, detail) {
  checks += 1;
  if (!condition) failures.push(detail ? `${label}\n      ${detail}` : label);
}

function setOutage(lab, mode) {
  writeJson(path.join(lab.mockDir, 'outage.json'), { mode });
}

function limitsAt(sessionPercent, weeklyPercent, resetIn = 1800) {
  const now = Math.floor(Date.now() / 1000);
  return [
    { kind: 'session', percent: sessionPercent, resets_at: new Date((now + resetIn) * 1000).toISOString() },
    { kind: 'weekly_all', percent: weeklyPercent, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
}

function hookOnce(account, lab, sid, extraEnv = {}) {
  return account.runFull(['hook'], {
    session_id: sid,
    cwd: lab.projectDir,
    transcript_path: lab.transcript,
    hook_event_name: 'UserPromptSubmit',
    prompt: 'keep going',
  }, extraEnv);
}

function fableFile(account) {
  return readJson(path.join(account.guardDir, 'fable.json')) || {};
}

function dropCache(account) {
  fs.rmSync(path.join(account.guardDir, 'fable.json'), { force: true });
}

function networkFailures(lab, account) {
  const cases = [
    ['rate-limited', 'http-429', 'a 429 from the usage endpoint'],
    ['forbidden', 'http-403', 'a 403'],
    ['error', 'http-500', 'a 500'],
    ['garbage', 'bad-json', 'a 200 with invalid JSON'],
    ['truncated', null, 'a body that stops halfway'],
    ['chunked-cut', 'http-0 truncated-response', 'a chunked body cut before its terminating chunk'],
    ['reset', null, 'a socket closed before any bytes'],
  ];
  for (const [mode, wantPrefix, description] of cases) {
    setOutage(lab, mode);
    dropCache(account);
    const result = hookOnce(account, lab, `chaos-${mode}`);

    check(`${description}: the hook still exits 0`, result.status === 0,
      `exit ${result.status}: ${result.stderr.slice(0, 200)}`);

    const fable = fableFile(account);
    check(`${description}: the failure is recorded`, typeof fable.error === 'string' && fable.error !== '',
      `error field was ${JSON.stringify(fable.error)}`);
    if (wantPrefix) {
      check(`${description}: recorded as ${wantPrefix}`, String(fable.error).startsWith(wantPrefix),
        `got ${JSON.stringify(fable.error)}`);
    }
    check(`${description}: a backoff is set so the next hook does not hammer the endpoint`,
      Number(fable.backoffUntil) > Math.floor(Date.now() / 1000),
      `backoffUntil was ${fable.backoffUntil}`);

    check(`${description}: no invented usage numbers`,
      fable.five_hour == null || typeof fable.five_hour === 'object',
      JSON.stringify(fable.five_hour));
    check(`${description}: fetchedAt is not advanced by a failure`,
      !(fable.error && Number(fable.fetchedAt) > 0 && fable.five_hour == null && fable.seven_day == null && Number(fable.fetchedAt) >= Math.floor(Date.now() / 1000) - 2 && mode !== 'garbage'),
      `fetchedAt ${fable.fetchedAt} with error ${fable.error}`);
  }

  lab.setLimits(limitsAt(96, 40));
  account.setConfig((config) => { config.wait.maxInHookMinutes = 0; });
  setOutage(lab, '');
  dropCache(account);
  const honest = hookOnce(account, lab, 'chaos-cut-control', { NOCTIS_NO_SCHEDULE: '1' });
  check('the control really does block at 96%', /"decision":\s*"block"|"continue":\s*false/.test(honest.stdout),
    `the scenario proves nothing unless the healthy case blocks: ${honest.stdout.slice(0, 200)}`);

  setOutage(lab, 'chunked-cut');
  dropCache(account);
  const cut = hookOnce(account, lab, 'chaos-cut', { NOCTIS_NO_SCHEDULE: '1' });
  const cutFable = fableFile(account);
  check('a cut body is never mistaken for an answer',
    typeof cutFable.error === 'string' && cutFable.error !== '' && cutFable.fetchedAt == null,
    `error ${JSON.stringify(cutFable.error)}, fetchedAt ${cutFable.fetchedAt}`);
  check('a cut body does not silently unblock a session that is over the threshold',
    !/"limits"\s*:\s*\[\]/.test(cut.stdout) && cutFable.five_hour == null,
    `stdout ${cut.stdout.slice(0, 200)}`);
  setOutage(lab, '');
  lab.setLimits(limitsAt(20, 30));
  dropCache(account);

  setOutage(lab, 'error');
  dropCache(account);
  hookOnce(account, lab, 'chaos-backoff-500');
  const after500 = Number(fableFile(account).backoffUntil);
  setOutage(lab, 'rate-limited');
  dropCache(account);
  hookOnce(account, lab, 'chaos-backoff-429');
  const after429 = Number(fableFile(account).backoffUntil);
  check('a 429 backs off for longer than a 500', after429 > after500, `429 → ${after429}, 500 → ${after500}`);

  const before = lab.mockHits().length;
  hookOnce(account, lab, 'chaos-backoff-429b');
  check('no fetch while backed off', lab.mockHits().length === before,
    `${lab.mockHits().length - before} extra request(s)`);

  setOutage(lab, '');
}

function slowAndHuge(lab, account) {
  for (const [mode, description, budgetMs] of [['slow-drip', 'a body that never finishes', 45000], ['huge', 'a body far larger than any real response', 45000]]) {
    setOutage(lab, mode);
    dropCache(account);
    const started = Date.now();
    const result = hookOnce(account, lab, `chaos-${mode}`);
    const elapsed = Date.now() - started;
    check(`${description}: the hook returns`, result.status === 0, `exit ${result.status}`);
    check(`${description}: within ${budgetMs / 1000}s`, elapsed < budgetMs, `took ${elapsed} ms`);
  }
  setOutage(lab, '');
}

async function tlsFailure(lab, account) {
  const plain = http.createServer((_, response) => {
    response.writeHead(200);
    response.end('{}');
  });
  await new Promise((resolve) => plain.listen(0, '127.0.0.1', resolve));
  const port = plain.address().port;
  dropCache(account);
  const result = hookOnce(account, lab, 'chaos-tls', { NOCTIS_USAGE_URL: `https://127.0.0.1:${port}/api/oauth/usage` });
  plain.close();

  check('TLS failure: the hook still exits 0', result.status === 0, `exit ${result.status}`);
  const fable = fableFile(account);
  check('TLS failure: recorded as an error', typeof fable.error === 'string' && fable.error !== '',
    JSON.stringify(fable.error));
  check('TLS failure: backoff set', Number(fable.backoffUntil) > Math.floor(Date.now() / 1000),
    `backoffUntil ${fable.backoffUntil}`);
}

function fullDisk(lab, account, mountPoint) {
  if (!mountPoint) {
    console.log('  (disk-full: no small filesystem was provided, skipped — set NOCTIS_CHAOS_FULL_DISK)');
    return;
  }
  lab.setLimits(limitsAt(97, 40));
  account.setConfig((config) => { config.wait.maxInHookMinutes = 0; });

  const configDir = path.join(mountPoint, 'claude');
  fs.mkdirSync(path.join(configDir, 'noctis'), { recursive: true });
  fs.copyFileSync(account.configFile, path.join(configDir, 'noctis', 'config.json'));
  fs.copyFileSync(path.join(account.dir, '.credentials.json'), path.join(configDir, '.credentials.json'));

  const env = { CLAUDE_CONFIG_DIR: configDir, NOCTIS_NO_SCHEDULE: '1' };
  const payload = {
    session_id: 'chaos-fulldisk',
    cwd: lab.projectDir,
    transcript_path: lab.transcript,
    hook_event_name: 'UserPromptSubmit',
    prompt: 'keep going',
  };
  const warm = account.runFull(['hook'], payload, env);
  check('full disk: the setup works before the disk fills', warm.status === 0, `exit ${warm.status}: ${warm.stderr.slice(0, 200)}`);

  const ballast = path.join(mountPoint, 'ballast');
  try {
    const chunk = Buffer.alloc(64 * 1024, 0);
    const handle = fs.openSync(ballast, 'w');
    try {
      for (;;) fs.writeSync(handle, chunk); 
    } catch {  } finally { fs.closeSync(handle); }

    let spaceLeft = true;
    try { fs.writeFileSync(path.join(mountPoint, 'probe'), 'x'.repeat(4096)); } catch { spaceLeft = false; }
    check('full disk: the filesystem really is full', !spaceLeft, 'a 4 KB file still fit');

    const result = account.runFull(['hook'], { ...payload, session_id: 'chaos-fulldisk2' }, env);
    check('full disk: the hook still exits 0', result.status === 0, `exit ${result.status}`);

    const stateOnDisk = readJson(path.join(configDir, 'noctis', 'state.json'));
    const waits = (stateOnDisk && stateOnDisk.waits) || {};
    const parked = Object.keys(waits).includes('chaos-fulldisk2');
    const promised = /iş kaydedildi|work saved|otomatik devam|auto-resume/i.test(result.stdout);

    check('full disk: does not promise an auto-resume it cannot deliver', !promised || parked,
      `said "${result.stdout.slice(0, 200)}" but state.json holds ${JSON.stringify(Object.keys(waits))}`);
    check('full disk: the user is told, one way or another',
      parked || /noctis|⏸/.test(`${result.stdout}${result.stderr}`),
      `stdout: ${result.stdout.slice(0, 200)} | stderr: ${result.stderr.slice(0, 200)}`);
  } finally {
    fs.rmSync(ballast, { force: true });
    fs.rmSync(path.join(mountPoint, 'probe'), { force: true });
  }

  lab.setLimits(limitsAt(20, 30));
  dropCache(account);
}

function oldStateMigrations(lab, account) {
  const stateFile = account.stateFile;
  const now = Math.floor(Date.now() / 1000);
  const fixtures = {
    'a 4.x state with no schema fields at all': {
      waits: { old1: { window: 'five_hour', resumeAt: now + 600, cwd: '/tmp', kind: 'batch' } },
    },
    'a state whose wait uses the old resume-time key': {
      waits: { old2: { window: 'five_hour', resume_at: now + 600, kind: 'batch' } },
    },
    'a state with keys this version has never heard of': {
      waits: { old3: { window: 'five_hour', resumeAt: now + 600, kind: 'batch' } },
      somethingFromTheFuture: { nested: [1, 2, 3] },
      notified: { 'a-session': now },
    },
    'a state where a map is where a number should be': {
      waits: { old4: { window: 'five_hour', resumeAt: { was: 'a number once' }, kind: 'batch' } },
    },
  };

  for (const [description, fixture] of Object.entries(fixtures)) {
    account.putState(fixture);
    const result = account.runFull(['status'], null);
    check(`${description}: status still runs`, result.status === 0,
      `exit ${result.status}: ${result.stderr.slice(0, 200)}`);

    const after = readJson(stateFile) || {};
    check(`${description}: state.json is still an object`, after && typeof after === 'object' && !Array.isArray(after));

    const waits = after.waits || {};
    const expected = Object.keys(fixture.waits || {})[0];
    if (expected && fixture.waits[expected].resumeAt && typeof fixture.waits[expected].resumeAt === 'number') {
      check(`${description}: the parked session survived`, Object.keys(waits).includes(expected),
        `waits are ${JSON.stringify(Object.keys(waits))}`);
    }
  }

  fs.writeFileSync(stateFile, '{"waits": {"broken": ');
  const result = account.runFull(['status'], null);
  check('a half-written state file: status still runs', result.status === 0, `exit ${result.status}`);
  const repaired = readJson(stateFile);
  check('a half-written state file: repaired into valid JSON on disk', repaired !== null && typeof repaired === 'object');
}

async function main() {
  const lab = new Lab('noctis-chaos');
  await lab.startMock();
  lab.setLimits(limitsAt(20, 30));
  try {
    const account = lab.account('main');
    account.install();
    hookOnce(account, lab, 'chaos-warmup');

    networkFailures(lab, account);
    slowAndHuge(lab, account);
    await tlsFailure(lab, account);
    fullDisk(lab, account, process.env.NOCTIS_CHAOS_FULL_DISK || '');
    oldStateMigrations(lab, account);

    if (failures.length > 0) {
      console.error('CHAOS FAILED');
      for (const failure of failures) console.error(`  ✗ ${failure}`);
      process.exitCode = 1;
      return;
    }
    console.log(`chaos: ${checks}/${checks} checks passed`);
  } finally {
    lab.stopMock();
    fs.rmSync(lab.root, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error('CHAOS CRASHED');
  console.error(error);
  process.exit(1);
});
