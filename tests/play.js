#!/usr/bin/env node
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const crypto = require('crypto');
const { spawn } = require('child_process');

const TAIL_BYTES = 256 * 1024;
const DEFAULT_WEIGHTS = { answer: 4, slow: 2, silent: 1, fail: 2, 'api-error': 1, 'answer-fail': 1, late: 2 };
const FAILING_MODES = new Set(['fail', 'api-error', 'answer-fail', 'late']);
const ANSWERING_MODES = new Set(['answer', 'slow', 'answer-fail']);
const WAIT_FIELDS = ['startedAt', 'holder', 'kind', 'until', 'resumeAt', 'launchAttempts', 'hit', 'transcript'];
const HANDOFF_FIELDS = ['at', 'waitStartedAt', 'pid', 'fresh'];
const MARK_FIELDS = ['startedAt', 'holder', 'by', 'at'];

function sleepMs(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, Math.max(0, ms));
}

function readJson(file) {
  try {
    return JSON.parse(fs.readFileSync(file, 'utf8'));
  } catch {
    return null;
  }
}

function writeRecord(file, data) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const staging = `${file}.${process.pid}.tmp`;
  fs.writeFileSync(staging, JSON.stringify(data));
  for (let attempt = 0; ; attempt += 1) {
    try {
      fs.renameSync(staging, file);
      return;
    } catch (error) {
      if (attempt === 11 || !['EPERM', 'EACCES', 'EBUSY'].includes(error.code)) throw error;
      sleepMs(10 + attempt * 10);
    }
  }
}

function recordDirs(labRoot) {
  return { launches: path.join(labRoot, 'play', 'launches'), answers: path.join(labRoot, 'play', 'answers') };
}

function insideLab(labRoot, file) {
  if (!file) return false;
  const relative = path.relative(path.resolve(labRoot), path.resolve(file));
  return relative !== '' && !relative.startsWith('..') && !path.isAbsolute(relative);
}

function endsWithNewline(file) {
  try {
    const size = fs.statSync(file).size;
    if (size === 0) return true;
    const fd = fs.openSync(file, 'r');
    try {
      const last = Buffer.alloc(1);
      fs.readSync(fd, last, 0, 1, size - 1);
      return last[0] === 0x0a;
    } finally {
      fs.closeSync(fd);
    }
  } catch {
    return true;
  }
}

function appendAnswer(labRoot, transcript, offset, by, { apiError = false } = {}) {
  const id = `${by}-a${crypto.randomBytes(4).toString('hex')}`;
  const clockMs = Date.now() + offset * 1000;
  const entry = {
    type: 'assistant',
    timestamp: new Date(clockMs).toISOString(),
    uuid: id,
    labAnswer: id,
    ...(apiError ? { isApiErrorMessage: true } : {}),
    message: { role: 'assistant', content: [{ type: 'text', text: apiError ? 'API Error: 529 overloaded' : 'Resumed.' }] },
  };
  fs.appendFileSync(transcript, `${endsWithNewline(transcript) ? '' : '\n'}${JSON.stringify(entry)}\n`);
  const record = { id, by, transcript: path.resolve(transcript), ts: clockMs / 1000, real: Date.now(), apiError };
  writeRecord(path.join(recordDirs(labRoot).answers, `${id}.json`), record);
  return record;
}

function visibleAnswers(transcript) {
  let text = '';
  try {
    const size = fs.statSync(transcript).size;
    const length = Math.min(size, TAIL_BYTES);
    const buffer = Buffer.alloc(length);
    const fd = fs.openSync(transcript, 'r');
    try {
      fs.readSync(fd, buffer, 0, length, size - length);
    } finally {
      fs.closeSync(fd);
    }
    text = buffer.toString('utf8');
    if (length < size) text = text.slice(text.indexOf('\n') + 1);
  } catch {
    return [];
  }
  const ids = [];
  for (const line of text.split('\n')) {
    if (!line.includes('"labAnswer"')) continue;
    try {
      const entry = JSON.parse(line);
      if (entry && entry.labAnswer && entry.message) ids.push(entry.labAnswer);
    } catch {}
  }
  return ids;
}

function pickFields(record, fields) {
  if (!record || typeof record !== 'object') return null;
  const picked = {};
  for (const field of fields) {
    if (record[field] !== undefined) picked[field] = record[field];
  }
  return picked;
}

function readState(configDir) {
  const file = path.join(configDir, 'noctis', 'state.json');
  for (let attempt = 0; attempt < 4; attempt += 1) {
    const state = readJson(file);
    if (state && typeof state === 'object') return state;
    sleepMs(25);
  }
  return null;
}

function clockOffset() {
  const raw = process.env.NOCTIS_TIME_OFFSET || '';
  return /^[+-]?\d+$/.test(raw) ? Number(raw) : 0;
}

function earlierLaunches(dir, tag) {
  try {
    return fs.readdirSync(dir).filter((name) => name.startsWith(`${tag}-`) && /^[^.]+\.json$/.test(name)).length;
  } catch {
    return 0;
  }
}

function chooseMode(settings, identity, count) {
  const forced = Array.isArray(settings.force) && settings.force.length ? settings.force : null;
  const weights = forced ? Object.fromEntries(forced.map((mode) => [mode, 1])) : { ...DEFAULT_WEIGHTS, ...(settings.weights || {}) };
  const entries = Object.entries(weights).filter(([, weight]) => Number(weight) > 0);
  const digest = crypto.createHash('sha256').update(`${settings.seed}|${identity}|${count}`).digest();
  const total = entries.reduce((sum, [, weight]) => sum + Number(weight), 0);
  let roll = (digest.readUInt32BE(0) / 4294967296) * total;
  for (const [mode, weight] of entries) {
    if (roll < Number(weight)) return { mode, digest };
    roll -= Number(weight);
  }
  return { mode: entries.length ? entries[entries.length - 1][0] : 'silent', digest };
}

function delayFrom(range, digest, at) {
  const [low, high] = Array.isArray(range) && range.length === 2 ? range.map(Number) : [200, 1500];
  return Math.round(low + (digest.readUInt32BE(at) / 4294967296) * (high - low));
}

function freshTranscript(state, fresh, original) {
  const recorded = state && state.freshStarts && state.freshStarts[fresh] && state.freshStarts[fresh].transcript;
  if (recorded) return path.resolve(recorded);
  return original ? path.join(path.dirname(original), `${fresh}.jsonl`) : '';
}

function play(labRoot) {
  const settings = readJson(path.join(labRoot, 'play.json'));
  const key = process.env.NOCTIS_HANDOFF || '';
  if (!settings || !key) return 0;
  const configDir = path.resolve(process.env.CLAUDE_CONFIG_DIR || path.join(os.homedir(), '.claude'));
  const offset = clockOffset();
  const state = readState(configDir);
  const wait = pickFields(state && state.waits && state.waits[key], WAIT_FIELDS);
  const handoff = pickFields(state && state.handedOff && state.handedOff[key], HANDOFF_FIELDS);
  const mark = pickFields(state && state.continuedBy && state.continuedBy[key], MARK_FIELDS);
  const original = wait && wait.transcript ? path.resolve(String(wait.transcript)) : '';
  const fresh = handoff && handoff.fresh ? String(handoff.fresh) : '';
  const target = fresh ? freshTranscript(state, fresh, original) : original;
  const seen = [...new Set([original, target].filter(Boolean))].map((file) => ({ transcript: file, ids: visibleAnswers(file) }));
  const dirs = recordDirs(labRoot);
  const tag = crypto.createHash('sha1').update(`${configDir}|${key}`).digest('hex').slice(0, 12);
  const id = `${tag}-${process.pid}${Date.now().toString(36)}${crypto.randomBytes(2).toString('hex')}`;
  const attributed = Boolean(wait && handoff);
  const { mode, digest } = attributed ? chooseMode(settings, `${configDir}|${key}`, earlierLaunches(dirs.launches, tag)) : { mode: 'silent', digest: null };
  const writable = attributed && insideLab(labRoot, target) && fs.existsSync(path.dirname(target));
  writeRecord(path.join(dirs.launches, `${id}.json`), {
    id, key, config: configDir, pid: process.pid, real: Date.now(), offset, mode, writable, wait, handoff, mark, fresh, transcript: original, target, seen,
  });
  const exit = FAILING_MODES.has(mode) ? 1 : 0;
  let answer = null;
  if (mode === 'slow') sleepMs(delayFrom(settings.slowMs, digest, 4));
  if (writable && ANSWERING_MODES.has(mode)) answer = appendAnswer(labRoot, target, offset, id).id;
  if (writable && mode === 'api-error') answer = appendAnswer(labRoot, target, offset, id, { apiError: true }).id;
  if (writable && mode === 'late') {
    const delay = delayFrom(settings.lateMs, digest, 8);
    spawn(process.execPath, [__filename, '--late', labRoot, id, target, String(offset), String(delay)], { detached: true, stdio: 'ignore', windowsHide: true }).unref();
  }
  writeRecord(path.join(dirs.launches, `${id}.done.json`), { id, real: Date.now(), exit, answer, lateExpected: writable && mode === 'late' });
  return exit;
}

function answerLate([labRoot, id, target, offset, delay]) {
  sleepMs(Number(delay));
  let answer = null;
  try {
    answer = appendAnswer(labRoot, target, Number(offset), id).id;
  } catch {}
  writeRecord(path.join(recordDirs(labRoot).launches, `${id}.late.json`), { id, real: Date.now(), answer });
}

function readDir(dir) {
  try {
    return fs.readdirSync(dir).filter((name) => name.endsWith('.json'));
  } catch {
    return [];
  }
}

function readPlay(labRoot) {
  const dirs = recordDirs(labRoot);
  const launches = new Map();
  const extras = [];
  for (const name of readDir(dirs.launches)) {
    const record = readJson(path.join(dirs.launches, name));
    if (!record || !record.id) continue;
    if (name.endsWith('.done.json')) extras.push(['done', record]);
    else if (name.endsWith('.late.json')) extras.push(['late', record]);
    else launches.set(record.id, { ...record, done: null, late: null });
  }
  for (const [kind, record] of extras) {
    const launch = launches.get(record.id);
    if (launch) launch[kind] = record;
  }
  const answers = readDir(dirs.answers).map((name) => readJson(path.join(dirs.answers, name))).filter((record) => record && record.id);
  return {
    launches: [...launches.values()].sort((a, b) => a.real - b.real),
    answers: answers.sort((a, b) => a.real - b.real),
  };
}

function unsettledPlayers(labRoot) {
  return readPlay(labRoot).launches.filter((launch) => !launch.done || (launch.done.lateExpected && !launch.late)).length;
}

if (require.main === module) {
  if (process.argv[2] === '--late') answerLate(process.argv.slice(3));
  else process.exitCode = play(process.argv[2] || '');
}

module.exports = { appendAnswer, readPlay, unsettledPlayers };
