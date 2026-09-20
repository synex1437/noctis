#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const { Lab, sleep, readJson, writeJson, refreshChecksums } = require('./harness');

const options = { days: 7, seed: 1, sessionsPerDay: 4, turns: 6, hard: 0 };
for (let i = 2; i < process.argv.length; i += 2) {
  const key = process.argv[i].replace(/^--/, '');
  if (key in options) options[key] = Number(process.argv[i + 1]);
}

const HOUR = 3600;
const DAY = 86400;
const THRESHOLDS = { five: 92, week: 89, fable: 95 };
const CODING_PROMPTS = ['auth.js dosyasındaki hatayı düzelt', 'Refactor the payment module and add unit tests', 'npm test çalıştır ve kırmızıları düzelt', 'Implement caching for the api layer', 'Bu fonksiyonu optimize et', 'Add a migration for the orders table'];
const RESEARCH_PROMPTS = ['En iyi mekanik klavye 2026 araştır', 'Compare pricing of Claude Max and ChatGPT Pro plans', 'Anthropic güncel haberleri neler', 'Latest research on intermittent fasting', 'Şu yazıyı özetle https://example.com/article'];
const OTHER_PROMPTS = ['Bu konuşmayı özetle', 'devam et', 'Write a short poem about autumn', 'JWT nasıl çalışır kısaca anlat'];

function mulberry32(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const rng = mulberry32(options.seed);
const pick = (list) => list[Math.floor(rng() * list.length)];
const between = (min, max) => min + rng() * (max - min);

const realStart = Math.floor(Date.now() / 1000);
let T = realStart;
const lab = new Lab('noctis-soak');
const stats = {
  hooks: 0,
  statuslines: 0,
  calls: 0,
  stops: 0,
  fableSwitches: 0,
  reverts: 0,
  routes: 0,
  denies: 0,
  clears: 0,
  compactions: 0,
  transient429: 0,
  relaunches: 0,
  chaos: 0,
  recoveries: 0,
  corruptions: 0,
  queueContinues: 0,
  stuckStops: 0,
  subagentGates: 0,
  overloadStorms: 0,
  overloadRetries: 0,
  overloadGiveups: 0,
  workspaceFlags: 0,
  workflowLaunches: 0,
  workflowNotes: 0,
  languageSwitches: 0,
  freshAccounts: 0,
  doubleResumes: 0,
  priorityChecks: 0,
  workCallsToday: 0,
  startNotices: 0,
  queueNotices: 0,
  startNoticesSeen: new Set(),
  breaches: [],
  anomalies: [],
  maxAllowedFive: 0,
  maxAllowedWeek: 0,
  latencies: [],
  timings: [],
};
const pendingResumes = [];

function account(name) {
  const acc = lab.account(name);
  acc.install((config) => {
    config.fable.pollMinutes = 1;
    config.wait.maxInHookMinutes = 0;
  });
  acc.manualSchedule = true;
  acc.fastClaude = true;
  acc.useOwnToken();
  acc.truth = {
    five: { used: 0, resetsAt: T + 5 * HOUR },
    week: { used: 0, resetsAt: T + 7 * DAY },
    fable: { used: 0, resetsAt: T + 7 * DAY },
  };
  acc.prefix = name.toLowerCase();
  return acc;
}

function setClock(accounts, t) {
  T = t;
  const offset = T - Math.floor(Date.now() / 1000);
  for (const acc of accounts) acc.timeOffset = offset;
  lab.setSkew(offset);
}

function rollWindows(acc) {
  const truth = acc.truth;
  while (T >= truth.five.resetsAt) {
    truth.five.used = 0;
    truth.five.resetsAt += 5 * HOUR;
  }
  while (T >= truth.week.resetsAt) {
    truth.week.used = 0;
    truth.week.resetsAt += 7 * DAY;
    truth.fable.used = 0;
    truth.fable.resetsAt = truth.week.resetsAt;
  }
}

function publishTruth(acc) {
  const truth = acc.truth;
  const iso = (epoch) => new Date(epoch * 1000).toISOString();
  acc.setOwnLimits([
    { kind: 'session', percent: Number(truth.five.used.toFixed(1)), resets_at: iso(truth.five.resetsAt) },
    { kind: 'weekly_all', percent: Number(truth.week.used.toFixed(1)), resets_at: iso(truth.week.resetsAt) },
    { kind: 'weekly_scoped', percent: Number(truth.fable.used.toFixed(1)), resets_at: iso(truth.fable.resetsAt), scope: { group: 'model', model: { display_name: 'Fable' } } },
  ]);
}

let ACCOUNTS = [];

function timedHook(acc, input, extraEnv = {}) {
  const started = Date.now();
  const output = acc.hook(input, extraEnv);
  const elapsed = Date.now() - started;
  stats.latencies.push(elapsed);
  stats.timings.push({ ms: elapsed, event: input.hook_event_name || '?', sid: input.session_id || '?' });
  stats.hooks += 1;
  if (elapsed >= 2000) {
    T += Math.round(elapsed / 1000);
    for (const account of ACCOUNTS) rollWindows(account);
  }
  return output;
}

function parseOutput(text) {
  if (!text) return {};
  try {
    return JSON.parse(text);
  } catch {
    return { raw: text };
  }
}

const MAX_OVERSHOOT = 4;

function applyCall(acc, session, cost) {
  const truth = acc.truth;
  const before = { five: truth.five.used, week: truth.week.used };
  if (truth.five.used + cost > 100 || truth.week.used + cost * 0.11 > 100) {
    stats.breaches.push({ day: Math.floor((T - realStart) / DAY) + 1, account: acc.name, sid: session.sid, five: truth.five.used, week: truth.week.used, cost });
  }
  if (before.five > THRESHOLDS.five + MAX_OVERSHOOT) {
    stats.anomalies.push(`call allowed at five=${before.five.toFixed(1)} % (threshold ${THRESHOLDS.five}) ${acc.name}/${session.sid}`);
  }
  if (before.week > THRESHOLDS.week + MAX_OVERSHOOT) {
    stats.anomalies.push(`call allowed at week=${before.week.toFixed(1)} % (threshold ${THRESHOLDS.week}) ${acc.name}/${session.sid}`);
  }
  stats.maxAllowedFive = Math.max(stats.maxAllowedFive, before.five);
  stats.maxAllowedWeek = Math.max(stats.maxAllowedWeek, before.week);
  truth.five.used = Math.min(100, truth.five.used + cost);
  truth.week.used = Math.min(100, truth.week.used + cost * 0.11);
  if (/fable/i.test(session.model)) truth.fable.used = Math.min(100, truth.fable.used + cost * 0.25);
  stats.calls += 1;
  publishTruth(acc);
}

function callCost() {
  return rng() < 0.05 ? between(4, 7) : between(0.5, 3);
}

function emitStatusline(acc, session) {
  if (rng() < 0.1) return;
  const truth = acc.truth;
  acc.statusline(session.sid, session.model, Number(truth.five.used.toFixed(1)), truth.five.resetsAt, Number(truth.week.used.toFixed(1)), truth.week.resetsAt, Math.round(session.context));
  stats.statuslines += 1;
}

function queueResume(acc, session, expect = {}, reason = '') {
  const state = acc.state();
  const wait = state.waits[session.sid];
  if (!wait) {
    const journalFile = path.join(acc.guardDir, 'decisions.jsonl');
    const journal = fs.existsSync(journalFile) ? fs.readFileSync(journalFile, 'utf8') : '';
    const traced = journal.split('\n').filter(Boolean).some((line) => {
      try {
        const row = JSON.parse(line);
        return row.sid === session.sid && ['pause', 'early-reset', 'wait-cancelled', 'resume', 'launch'].includes(row.action);
      } catch {
        return false;
      }
    });
    if (!traced) stats.anomalies.push(`stop without wait record or journal trace ${acc.name}/${session.sid}: ${String(reason).slice(0, 160)}`);
    return;
  }
  const entry = { acc, sid: session.sid, resumeAt: Number(wait.resumeAt), kind: wait.kind, ...expect };
  if (session.workflow) {
    entry.expectWorkflow = session.workflow;
    const checkpoint = state.checkpoints && state.checkpoints[session.sid];
    let text = '';
    try {
      text = fs.readFileSync(checkpoint.path, 'utf8');
    } catch {
      text = '';
    }
    if (!text.includes(session.workflow) || !/workflow/i.test(text)) stats.anomalies.push(`checkpoint lost the running workflow ${acc.name}/${session.sid}`);
  }
  pendingResumes.push(entry);
  stats.stops += 1;
}

function checkRelaunchPrompt(entry, line) {
  if (entry.expectWorkspace && !/çalışma ağacı|working tree/.test(line)) stats.anomalies.push(`relaunch prompt lacks the workspace warning ${entry.acc.name}/${entry.sid}`);
  if (entry.expectWorkspace) stats.workspaceFlags += 1;
  if (entry.expectWorkflow && !(line.includes(entry.expectWorkflow) && /relaunch|never start it over/i.test(line))) stats.anomalies.push(`relaunch prompt lacks the workflow resume note ${entry.acc.name}/${entry.sid}`);
  if (entry.expectWorkflow) stats.workflowNotes += 1;
  if (/TASKS\.md/.test(line) && !/do not redo items already marked done/.test(line)) stats.anomalies.push(`relaunch prompt lost the queue instruction ${entry.acc.name}/${entry.sid}`);
}

function processResumes(accounts) {
  const due = pendingResumes.filter((entry) => entry.resumeAt <= T);
  for (const entry of due) {
    pendingResumes.splice(pendingResumes.indexOf(entry), 1);
    rollWindows(entry.acc);
    publishTruth(entry.acc);
    const callsBefore = lab.calls().length;
    entry.acc.run(['resume', '--sid', entry.sid, '--account', entry.acc.dir]);
    const state = entry.acc.state();
    const launched = lab.calls().length - callsBefore;
    if (launched > 1) stats.anomalies.push(`double launch ${entry.acc.name}/${entry.sid}`);
    if (launched === 1) {
      stats.relaunches += 1;
      const call = lab.calls()[callsBefore];
      if (!call.includes(`CONFIG=${entry.acc.dir}`)) stats.anomalies.push(`relaunch on wrong account ${entry.acc.name}/${entry.sid}`);
      if (Object.keys(state.handedOff || {}).length) stats.anomalies.push(`handoff not released ${entry.acc.name}/${entry.sid}`);
      checkRelaunchPrompt(entry, call);
    } else if (state.waits && state.waits[entry.sid]) {
      pendingResumes.push({ acc: entry.acc, sid: entry.sid, resumeAt: Number(state.waits[entry.sid].resumeAt) });
    } else {
      stats.anomalies.push(`resume neither launched nor rescheduled ${entry.acc.name}/${entry.sid}`);
    }
  }
}

function transcriptFor(acc, sid) {
  const file = path.join(acc.dir, 'projects', '-lab-project', `${sid}.jsonl`);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.copyFileSync(lab.transcript, file);
  return file;
}

function newSession(acc) {
  const sid = `${acc.prefix}-${Math.floor(rng() * 1e9).toString(16)}`;
  const model = acc.settingsModel() === 'opus' ? 'claude-opus-5' : 'claude-fable-5-1';
  const session = { sid, model, context: between(15, 45), transcript: transcriptFor(acc, sid) };
  const out = parseOutput(timedHook(acc, { hook_event_name: 'SessionStart', source: 'startup', session_id: sid, cwd: lab.projectDir }));
  if (out.systemMessage && /^☰ /.test(out.systemMessage)) stats.queueNotices += 1;
  else if (out.systemMessage && /yeniden fable/.test(out.systemMessage)) stats.reverts += 1;
  else if (out.systemMessage && /^⏸ .*zaten/.test(out.systemMessage)) {
    const key = out.systemMessage.replace(/%[\d.]+/, '');
    if (stats.startNoticesSeen.has(key)) stats.anomalies.push(`already-over notice repeated: ${out.systemMessage}`);
    stats.startNoticesSeen.add(key);
    stats.startNotices += 1;
  } else if (out.systemMessage) stats.anomalies.push(`unexpected self-check: ${out.systemMessage}`);
  const directive = /Queue mode \(TASKS\.md: (\d+) open\)/.exec((out.hookSpecificOutput || {}).additionalContext || '');
  if (!directive) stats.anomalies.push(`queue directive missing for ${acc.name}/${sid}`);
  else if (options.hard && Number(directive[1]) !== openQueueItems()) stats.anomalies.push(`queue count ${directive[1]} but the file has ${openQueueItems()} open boxes ${acc.name}/${sid}`);
  return session;
}

async function runTurn(acc, session) {
  rollWindows(acc);
  publishTruth(acc);
  const roll = rng();
  const prompt = roll < 0.7 ? pick(CODING_PROMPTS) : roll < 0.9 ? pick(RESEARCH_PROMPTS) : pick(OTHER_PROMPTS);
  let out = parseOutput(timedHook(acc, { hook_event_name: 'UserPromptSubmit', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, prompt }));
  if (out.decision === 'block' && /\/model/.test(out.reason)) {
    stats.fableSwitches += 1;
    timedHook(acc, { hook_event_name: 'PostModelSwitch', session_id: session.sid, from_model: session.model, to_model: 'claude-opus-5' });
    session.model = 'claude-opus-5';
    out = parseOutput(timedHook(acc, { hook_event_name: 'UserPromptSubmit', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, prompt }));
  }
  if (out.decision === 'block') {
    if (/⏸|↪/.test(out.reason)) {
      queueResume(acc, session, {}, out.reason);
      return 'stopped';
    }
    stats.anomalies.push(`unexpected block ${acc.name}/${session.sid}: ${out.reason}`);
    return 'stopped';
  }
  if (out.systemMessage && /yeniden fable/.test(out.systemMessage)) stats.reverts += 1;
  const routed = Boolean(out.hookSpecificOutput && /Non-code research/.test(out.hookSpecificOutput.additionalContext || ''));
  if (routed) {
    stats.routes += 1;
    if (rng() < 0.5) {
      const deny = parseOutput(timedHook(acc, { hook_event_name: 'PreToolUse', session_id: session.sid, tool_name: 'WebSearch', tool_input: { query: prompt } }));
      if (deny.hookSpecificOutput && deny.hookSpecificOutput.permissionDecision === 'deny') stats.denies += 1;
      else stats.anomalies.push(`routed prompt but WebSearch allowed ${acc.name}/${session.sid}`);
    }
    const subagent = parseOutput(timedHook(acc, { hook_event_name: 'PreToolUse', session_id: session.sid, agent_id: 'lite-1', agent_type: 'noctis:lite', tool_name: 'WebSearch', tool_input: {} }));
    if (subagent.hookSpecificOutput) stats.anomalies.push(`subagent search denied ${acc.name}/${session.sid}`);
  }
  applyCall(acc, session, callCost());
  emitStatusline(acc, session);
  const batches = 1 + Math.floor(rng() * 3);
  for (let i = 0; i < batches; i += 1) {
    setClock([acc], T + Math.floor(between(20, 120)));
    rollWindows(acc);
    const batch = parseOutput(timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript }));
    if (batch.continue === false) {
      const stopReason = batch.stopReason || '';
      if (/🔁/.test(stopReason)) {
        stats.fableSwitches += 1;
        timedHook(acc, { hook_event_name: 'PostModelSwitch', session_id: session.sid, from_model: session.model, to_model: 'claude-opus-5' });
        session.model = 'claude-opus-5';
        return 'worked';
      }
      queueResume(acc, session, {}, stopReason);
      return 'stopped';
    }
    if (batch.systemMessage && /yeniden fable/.test(batch.systemMessage)) stats.reverts += 1;
    let cost = callCost();
    if (session.context >= 85) {
      cost += 6;
      session.context = 25;
      stats.compactions += 1;
    }
    applyCall(acc, session, cost);
    session.context = Math.min(95, session.context + between(1, 9));
    emitStatusline(acc, session);
  }
  if (rng() < 0.05) {
    const fresh = `${acc.prefix}-${Math.floor(rng() * 1e9).toString(16)}`;
    timedHook(acc, { hook_event_name: 'SessionStart', source: 'clear', session_id: fresh, cwd: lab.projectDir });
    stats.clears += 1;
    session.sid = fresh;
    session.context = 10;
    session.transcript = transcriptFor(acc, fresh);
  }
  if (rng() < 0.03 && acc.truth.five.used < 85 && acc.truth.week.used < 85 && acc.truth.fable.used < 85) {
    timedHook(acc, { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error_type: 'rate_limit', error_message: 'Rate limit exceeded' });
    const wait = acc.state().waits[session.sid];
    if (!wait || wait.window !== 'unknown') stats.anomalies.push(`transient 429 misclassified as ${wait && wait.window} ${acc.name}/${session.sid}`);
    if (acc.settingsModel() === 'opus' && !acc.state().modelSwitched) stats.anomalies.push(`transient 429 switched model ${acc.name}`);
    stats.transient429 += 1;
    queueResume(acc, session);
    return 'stopped';
  }
  return 'ok';
}

async function runSession(acc) {
  const session = newSession(acc);
  for (let turn = 0; turn < options.turns; turn += 1) {
    const result = await runTurn(acc, session);
    if (result === 'stopped') break;
    setClock([acc], T + Math.floor(between(60, 240)));
  }
  timedHook(acc, { hook_event_name: 'SessionEnd', session_id: session.sid, reason: 'exit' });
}

function dirSize(dir, pattern) {
  return fs.readdirSync(dir).filter((name) => pattern.test(name)).length;
}

const LEFTOVER = /\.(lock|tmp)$/;
const SWEEP_TTL_MS = 5 * 60 * 1000;

function leftovers(dir) {
  return fs.readdirSync(dir).filter((name) => LEFTOVER.test(name));
}

function staleLeftovers(dir) {
  return leftovers(dir).filter((name) => Date.now() - fs.statSync(path.join(dir, name)).mtimeMs > SWEEP_TTL_MS).length;
}

function sweepWorks(accounts) {
  for (const acc of accounts) {
    const planted = path.join(acc.guardDir, 'state.json.999999.tmp');
    if (!fs.existsSync(planted)) fs.writeFileSync(planted, '');
    const old = new Date(Date.now() - SWEEP_TTL_MS - 60000);
    for (const name of leftovers(acc.guardDir)) fs.utimesSync(path.join(acc.guardDir, name), old, old);
    acc.statusline(`${acc.prefix}-sweep`, 'claude-fable-5-1', Number(acc.truth.five.used.toFixed(1)), acc.truth.five.resetsAt, Number(acc.truth.week.used.toFixed(1)), acc.truth.week.resetsAt);
    const left = leftovers(acc.guardDir).filter((name) => Date.now() - fs.statSync(path.join(acc.guardDir, name)).mtimeMs > SWEEP_TTL_MS);
    if (left.length) stats.anomalies.push(`${acc.name} stale leftovers survived a sweep: ${left.join(', ')}`);
  }
}

function dailyInvariants(accounts, day) {
  for (const acc of accounts) {
    timedHook(acc, { hook_event_name: 'SessionStart', source: 'startup', session_id: `${acc.prefix}-morning-${day}`, cwd: lab.projectDir });
    const stateSize = fs.statSync(acc.stateFile).size;
    if (stateSize > 60000) stats.anomalies.push(`day ${day} ${acc.name} state.json ${stateSize} bytes`);
    const usageSize = fs.statSync(path.join(acc.guardDir, 'usage.json')).size;
    if (usageSize > 30000) stats.anomalies.push(`day ${day} ${acc.name} usage.json ${usageSize} bytes`);
    for (const logName of ['guard.log', 'errors.log']) {
      const file = path.join(acc.guardDir, logName);
      if (fs.existsSync(file) && fs.statSync(file).size > 600000) stats.anomalies.push(`day ${day} ${acc.name} ${logName} not rotated`);
    }
    if (staleLeftovers(acc.guardDir)) stats.anomalies.push(`day ${day} ${acc.name} leftover lock/tmp`);
    const state = acc.state();
    const foreign = Object.keys(state.modelOverrides || {}).concat(Object.keys(state.waits || {})).filter((sid) => !sid.startsWith(acc.prefix) && sid !== 'unknown');
    if (foreign.length) stats.anomalies.push(`day ${day} ${acc.name} foreign session ids ${foreign.join(',')}`);
    const errors = fs.existsSync(path.join(acc.guardDir, 'errors.log')) ? fs.readFileSync(path.join(acc.guardDir, 'errors.log'), 'utf8') : '';
    if (/fatal/.test(errors)) stats.anomalies.push(`day ${day} ${acc.name} fatal in errors.log`);
    stats.recoveries += (errors.match(/recovered from backup|restored from the backup/g) || []).length;
    if (acc.truth.fable.used < THRESHOLDS.fable && state.modelSwitched && T > Number(state.modelSwitched.fableResetsAt)) stats.anomalies.push(`day ${day} ${acc.name} default model not reverted after fable reset`);
  }
}

function percentile(values, p) {
  if (!values.length) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * p))];
}

function corruptFile(file) {
  stats.corruptions += 1;
  try {
    const text = fs.readFileSync(file, 'utf8');
    fs.writeFileSync(file, text.slice(0, Math.max(2, Math.floor(text.length / 2))));
  } catch {
    fs.writeFileSync(file, '{ broken');
  }
}

const CHAOS_KINDS = ['state-corrupt', 'usage-corrupt', 'fable-corrupt', 'api-error', 'api-garbage', 'api-timeout', 'statusline-blackout', 'stale-lock', 'hook-kill', 'clock-back', 'big-transcript', 'parallel-subagents', 'overload-storm', 'workspace-edit', 'queue-priorities', 'language-switch', 'workflow-launch', 'fresh-account-99', 'double-resume', 'overload-giveup'];
const LANGUAGE_SAMPLES = [
  ['de', 'Bitte behebe den Fehler in der Datei und führe die Tests danach erneut aus'],
  ['fr', "Corrige l'erreur dans le fichier et relance les tests s'il te plaît"],
  ['es', 'Por favor corrige el error en el archivo y vuelve a ejecutar las pruebas'],
  ['ja', 'ファイルのエラーを修正してテストをもう一度実行してください'],
  ['ru', 'Пожалуйста, исправь ошибку в файле и запусти тесты ещё раз'],
  ['en', 'Please fix the failing test in the auth module and then run the whole suite again'],
  ['tr', 'Dosyadaki hatayı düzelt ve sonra testleri yeniden çalıştır lütfen'],
];
let chaosSerial = 0;

function anomaly(text) {
  stats.anomalies.push(text);
}

function forceWall(acc, session) {
  acc.truth.five.used = Math.max(acc.truth.five.used, THRESHOLDS.five + 2);
  publishTruth(acc);
  acc.statusline(session.sid, session.model, Number(acc.truth.five.used.toFixed(1)), acc.truth.five.resetsAt, Number(acc.truth.week.used.toFixed(1)), acc.truth.week.resetsAt, Math.round(session.context));
  const batch = parseOutput(timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript }));
  if (batch.continue !== false) {
    anomaly(`forced wall not honoured ${acc.name}/${session.sid}: ${JSON.stringify(batch)}`);
    return false;
  }
  return Boolean(acc.state().waits[session.sid]);
}

async function injectChaos(acc, session, kindIndex, turn, accounts) {
  const guardDir = acc.guardDir;
  const kind = CHAOS_KINDS[kindIndex % CHAOS_KINDS.length];
  stats.chaos += 1;
  chaosSerial += 1;
  const serial = chaosSerial;
  switch (kind) {
    case 'overload-storm': {
      stats.overloadStorms += 1;
      const rounds = 3 + Math.floor(rng() * 3);
      let previousDelay = 0;
      for (let i = 0; i < rounds; i += 1) {
        const type = rng() < 0.5 ? 'overloaded' : 'server_error';
        timedHook(acc, { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error_type: type });
        const wait = acc.state().waits[session.sid];
        if (!wait || wait.overload !== true || wait.window !== 'unknown') {
          anomaly(`overload not recorded as backoff ${acc.name}/${session.sid}: ${JSON.stringify(wait)}`);
          break;
        }
        const delay = Number(wait.resumeAt) - T;
        if (delay < 1 || delay > 330) anomaly(`overload delay out of range (${delay}s) ${acc.name}/${session.sid}`);
        if (i > 0 && delay < Math.min(previousDelay, 200)) anomaly(`overload backoff shrank ${previousDelay}s -> ${delay}s ${acc.name}/${session.sid}`);
        if (Number(wait.attempt) !== i + 1) anomaly(`overload attempt ${wait.attempt} expected ${i + 1} ${acc.name}/${session.sid}`);
        previousDelay = delay;
        if (i < rounds - 1) {
          setClock(accounts, Math.ceil(Number(wait.resumeAt)) + 1);
          for (const account of accounts) rollWindows(account);
          publishTruth(acc);
          const before = lab.calls().length;
          acc.run(['resume', '--sid', session.sid, '--account', acc.dir]);
          const launched = lab.calls().slice(before).filter((line) => line.includes(`--resume ${session.sid} `));
          if (launched.length !== 1) anomaly(`overload retry launched ${launched.length} times ${acc.name}/${session.sid}`);
          else if (!/API (overloaded|server_error) durumundaydı|The API was (overloaded|server_error)/.test(launched[0])) anomaly(`overload retry prompt lacks the retry note ${acc.name}/${session.sid}: ${launched[0].slice(0, 200)}`);
          stats.overloadRetries += 1;
          if (acc.state().waits[session.sid]) anomaly(`overload wait not cleared after retry ${acc.name}/${session.sid}`);
        }
      }
      if (acc.state().waits[session.sid]) {
        queueResume(acc, session);
        session.stoppedByChaos = true;
      }
      break;
    }
    case 'overload-giveup': {
      stats.overloadStorms += 1;
      let gaveUp = false;
      for (let i = 0; i < 40 && !gaveUp; i += 1) {
        timedHook(acc, { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error_type: 'overloaded' });
        const wait = acc.state().waits[session.sid];
        if (!wait) {
          gaveUp = true;
          break;
        }
        setClock(accounts, Math.ceil(Number(wait.resumeAt)) + 1);
        for (const account of accounts) rollWindows(account);
        publishTruth(acc);
        acc.run(['resume', '--sid', session.sid, '--account', acc.dir]);
        stats.overloadRetries += 1;
      }
      if (!gaveUp) anomaly(`overload retry budget never ran out ${acc.name}/${session.sid}`);
      else {
        stats.overloadGiveups += 1;
        const errors = fs.existsSync(path.join(guardDir, 'errors.log')) ? fs.readFileSync(path.join(guardDir, 'errors.log'), 'utf8') : '';
        if (!/retry budget spent/.test(errors)) anomaly(`overload give-up not logged ${acc.name}/${session.sid}`);
        const out = parseOutput(timedHook(acc, { hook_event_name: 'UserPromptSubmit', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, prompt: 'devam et' }));
        if (out.decision === 'block' && !/⏸|↪|\/model/.test(out.reason)) anomaly(`prompt blocked after overload give-up ${acc.name}/${session.sid}: ${out.reason}`);
        if ((acc.state().overload || {})[session.sid]) anomaly(`overload episode not cleared by the next prompt ${acc.name}/${session.sid}`);
      }
      break;
    }
    case 'workspace-edit': {
      if (forceWall(acc, session)) {
        fs.writeFileSync(path.join(lab.projectDir, `chaos-${serial}.txt`), 'edited while the session was waiting\n');
        queueResume(acc, session, { expectWorkspace: true });
        session.stoppedByChaos = true;
      }
      break;
    }
    case 'queue-priorities': {
      const file = path.join(lab.projectDir, 'TASKS.md');
      const text = fs.readFileSync(file, 'utf8');
      fs.writeFileSync(file, `${text}- [ ] (P0) urgent chaos item ${serial}\n- [ ] #never${serial} slow chaos item ${serial}\n- [ ] (after #never${serial}) blocked chaos item ${serial}\n`);
      const stop = parseOutput(timedHook(acc, { hook_event_name: 'Stop', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, stop_hook_active: false }));
      if (stop.decision === 'block') {
        stats.priorityChecks += 1;
        session.forced = (session.forced || 0) + 1;
        if (!new RegExp(`\\("\\(P0\\) urgent chaos item ${serial}"\\)`).test(stop.reason)) anomaly(`P0 item not first ${acc.name}/${session.sid}: ${stop.reason.slice(0, 160)}`);
        if (!/\d+ item\(s\) wait on unfinished dependencies/.test(stop.reason)) anomaly(`blocked item not reported ${acc.name}/${session.sid}`);
      }
      fs.writeFileSync(file, fs.readFileSync(file, 'utf8').replace(`- [ ] (P0) urgent chaos item ${serial}`, `- [x] (P0) urgent chaos item ${serial}`));
      break;
    }
    case 'language-switch': {
      const [lang, prompt] = LANGUAGE_SAMPLES[serial % LANGUAGE_SAMPLES.length];
      const out = parseOutput(timedHook(acc, { hook_event_name: 'UserPromptSubmit', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, prompt }, { NOCTIS_LANG: '' }));
      const recorded = ((acc.state().sessionLocale || {})[session.sid] || {}).lang;
      if (recorded !== lang) anomaly(`language ${lang} detected as ${recorded} ${acc.name}/${session.sid}`);
      else stats.languageSwitches += 1;
      if (out.decision === 'block') {
        if (/⏸|↪/.test(out.reason)) {
          queueResume(acc, session);
          session.stoppedByChaos = true;
        }
      }
      const line = acc.run(['statusline'], acc.statuslineInput(session.sid, session.model, Number(acc.truth.five.used.toFixed(1)), acc.truth.five.resetsAt, Number(acc.truth.week.used.toFixed(1)), acc.truth.week.resetsAt), { NOCTIS_LANG: '' });
      if (!line) anomaly(`statusline empty in ${lang} ${acc.name}/${session.sid}`);
      break;
    }
    case 'workflow-launch': {
      const name = `audit-${serial}`;
      const gate = parseOutput(timedHook(acc, { hook_event_name: 'PreToolUse', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, tool_name: 'Workflow', tool_input: { script_path: `.claude/workflows/${name}.ts`, name } }));
      const denied = Boolean(gate.hookSpecificOutput && gate.hookSpecificOutput.permissionDecision === 'deny');
      const low = acc.truth.five.used < 70 && acc.truth.week.used < 70 && acc.truth.fable.used < 70;
      if (denied && low) anomaly(`workflow denied at low usage ${acc.name}/${session.sid}: ${gate.hookSpecificOutput.permissionDecisionReason}`);
      if (!denied) {
        stats.workflowLaunches += 1;
        const runs = (acc.state().workflows || {})[session.sid] || [];
        if (!runs.some((run) => run.name === name)) anomaly(`workflow launch not recorded ${acc.name}/${session.sid}`);
        session.workflow = name;
      }
      break;
    }
    case 'fresh-account-99': {
      stats.freshAccounts += 1;
      const fresh = account(`C${serial}`);
      fresh.timeOffset = acc.timeOffset;
      fresh.truth.week.used = 99;
      fresh.truth.week.resetsAt = acc.truth.week.resetsAt;
      fresh.truth.five.used = 40;
      publishTruth(fresh);
      const sid = `${fresh.prefix}-first`;
      const start = parseOutput(timedHook(fresh, { hook_event_name: 'SessionStart', source: 'startup', session_id: sid, cwd: lab.projectDir }));
      if (!/zaten %99|already 99%/.test(start.systemMessage || '')) anomaly(`fresh account at 99% not warned at start: ${JSON.stringify(start).slice(0, 200)}`);
      const again = parseOutput(timedHook(fresh, { hook_event_name: 'SessionStart', source: 'startup', session_id: `${sid}-b`, cwd: lab.projectDir }));
      if (/zaten %99|already 99%/.test(again.systemMessage || '')) anomaly('fresh account: the already-over notice repeated for the same window');
      const prompt = parseOutput(timedHook(fresh, { hook_event_name: 'UserPromptSubmit', session_id: sid, cwd: lab.projectDir, transcript_path: transcriptFor(fresh, sid), prompt: 'auth.js dosyasındaki hatayı düzelt' }));
      const wait = fresh.state().waits[sid];
      if (prompt.decision !== 'block' || !wait || wait.window !== 'seven_day') anomaly(`fresh account at 99%: first prompt not parked until the weekly reset (${JSON.stringify(prompt).slice(0, 160)})`);
      else if (Math.abs(Number(wait.until) - fresh.truth.week.resetsAt) > 5) anomaly('fresh account: wait not aligned with the weekly reset');
      const check = fresh.runFull(['check']);
      if (check.status !== 11) anomaly(`fresh account: noctis check exit ${check.status} expected 11`);
      if (!fresh.run(['status'])) anomaly('fresh account: status printed nothing');
      fresh.run(['cancel', sid]);
      if (fresh.state().waits[sid]) anomaly('fresh account: cancel left the wait');
      break;
    }
    case 'double-resume': {
      if (forceWall(acc, session)) {
        const wait = acc.state().waits[session.sid];
        setClock(accounts, Math.ceil(Number(wait.resumeAt)) + 2);
        for (const account of accounts) rollWindows(account);
        publishTruth(acc);
        const before = lab.calls().length;
        await Promise.all([acc.runPromise(['resume', '--sid', session.sid, '--account', acc.dir]), acc.runPromise(['resume', '--sid', session.sid, '--account', acc.dir])]);
        const launched = lab.calls().slice(before).filter((line) => line.includes(`--resume ${session.sid} `));
        if (launched.length !== 1) anomaly(`double resume launched ${launched.length} times ${acc.name}/${session.sid}`);
        else stats.doubleResumes += 1;
        if (acc.state().waits[session.sid]) anomaly(`double resume left the wait record ${acc.name}/${session.sid}`);
        if (Object.keys(acc.state().handedOff || {}).length) anomaly(`double resume left a handoff ${acc.name}/${session.sid}`);
      }
      break;
    }
    case 'state-corrupt': {
      const waitsBefore = Object.keys(acc.state().waits || {}).length;
      corruptFile(acc.stateFile);
      timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript });
      const after = acc.state();
      if (!after || typeof after !== 'object') anomaly(`state still unreadable after a hook ${acc.name}`);
      else if (Object.keys(after.waits || {}).length < waitsBefore) anomaly(`corrupt state lost ${waitsBefore - Object.keys(after.waits || {}).length} wait(s) ${acc.name}`);
      break;
    }
    case 'usage-corrupt': {
      corruptFile(path.join(guardDir, 'usage.json'));
      timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript });
      const usageFile = path.join(guardDir, 'usage.json');
      if (fs.existsSync(usageFile)) {
        try { JSON.parse(fs.readFileSync(usageFile, 'utf8')); } catch { anomaly(`usage.json still unreadable after a hook ${acc.name}`); }
      }
      break;
    }
    case 'fable-corrupt':
      corruptFile(path.join(guardDir, 'fable.json'));
      timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript });
      break;
    case 'api-error':
      lab.setOutage('error');
      session.outageUntilTurn = turn + 3;
      break;
    case 'api-garbage':
      lab.setOutage('garbage');
      session.outageUntilTurn = turn + 2;
      break;
    case 'api-timeout':
      lab.setOutage('timeout');
      session.outageUntilTurn = turn + 1;
      break;
    case 'statusline-blackout':
      session.blackoutUntilTurn = turn + 6;
      break;
    case 'stale-lock': {
      const lock = path.join(guardDir, 'state.lock');
      fs.writeFileSync(lock, rng() < 0.5 ? '' : '999999');
      const old = new Date(Date.now() - 60000);
      fs.utimesSync(lock, old, old);
      const started = Date.now();
      timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript }, { NOCTIS_NO_QUIET: '1' });
      if (Date.now() - started > 3000) anomaly(`a stale lock cost the hook ${Date.now() - started} ms ${acc.name}`);
      acc.run(['on']);
      if (fs.existsSync(lock) && Date.now() - fs.statSync(lock).mtimeMs > 30000) anomaly(`stale lock survived a state write ${acc.name}`);
      break;
    }
    case 'hook-kill': {
      const child = acc.hookAsync({ hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript });
      const closed = new Promise((resolve) => child.on('close', resolve));
      await sleep(4);
      child.kill('SIGKILL');
      await closed;
      break;
    }
    case 'clock-back': {
      const before = Object.entries(acc.state().waits || {}).map(([sid, wait]) => [sid, Number(wait.resumeAt)]);
      setClock(accounts, T - 600);
      timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript });
      for (const [sid, at] of before) {
        const now = Number((acc.state().waits[sid] || {}).resumeAt);
        if (now && Math.abs(now - at) > 5) anomaly(`a backwards clock moved resumeAt for ${sid}: ${at} -> ${now}`);
      }
      break;
    }
    case 'big-transcript':
      fs.appendFileSync(session.transcript, `${JSON.stringify({ type: 'assistant', message: { role: 'assistant', content: [{ type: 'text', text: 'y'.repeat(4000) }] } })}\n`.repeat(250));
      {
        const started = Date.now();
        timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript });
        if (Date.now() - started > 8000) anomaly(`a hook took ${Date.now() - started} ms on a big transcript ${acc.name}`);
      }
      break;
    case 'parallel-subagents': {
      const spawns = [];
      for (let i = 0; i < 4; i += 1) spawns.push(acc.hookPromise({ hook_event_name: 'PreToolUse', session_id: session.sid, agent_id: `p${i}`, agent_type: 'general-purpose', tool_name: 'Write', tool_input: { file_path: path.join(lab.projectDir, `out${i}.md`) } }));
      spawns.push(acc.hookPromise({ hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript }));
      const outputs = await Promise.all(spawns);
      if (outputs.slice(0, 4).some((out) => out)) stats.anomalies.push(`general-purpose subagent write blocked ${acc.name}/${session.sid}`);
      break;
    }
    default:
      break;
  }
  return kind;
}

async function marathonTurn(acc, session, turn, accounts) {
  rollWindows(acc);
  publishTruth(acc);
  if (session.outageUntilTurn !== undefined && turn >= session.outageUntilTurn) {
    lab.setOutage('');
    session.outageUntilTurn = undefined;
  }
  const blackout = session.blackoutUntilTurn !== undefined && turn < session.blackoutUntilTurn;
  if (session.blackoutUntilTurn !== undefined && turn >= session.blackoutUntilTurn) session.blackoutUntilTurn = undefined;
  if (turn % 5 === 4) {
    const gate = parseOutput(timedHook(acc, { hook_event_name: 'PreToolUse', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, tool_name: 'Agent', tool_input: { prompt: 'explore' } }));
    stats.subagentGates += 1;
    if (gate.hookSpecificOutput && gate.hookSpecificOutput.permissionDecision === 'deny') {
      const gateReason = gate.hookSpecificOutput.permissionDecisionReason || '';
      if (/🔁/.test(gateReason)) {
        stats.fableSwitches += 1;
        timedHook(acc, { hook_event_name: 'PostModelSwitch', session_id: session.sid, from_model: session.model, to_model: 'claude-opus-5' });
        session.model = 'claude-opus-5';
        return 'worked';
      }
      queueResume(acc, session, {}, gateReason);
      return 'stopped';
    }
  }
  const prompt = rng() < 0.75 ? pick(CODING_PROMPTS) : pick(RESEARCH_PROMPTS);
  let out = parseOutput(timedHook(acc, { hook_event_name: 'UserPromptSubmit', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, prompt }));
  if (out.decision === 'block' && /\/model/.test(out.reason)) {
    stats.fableSwitches += 1;
    timedHook(acc, { hook_event_name: 'PostModelSwitch', session_id: session.sid, from_model: session.model, to_model: 'claude-opus-5' });
    session.model = 'claude-opus-5';
    out = parseOutput(timedHook(acc, { hook_event_name: 'UserPromptSubmit', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, prompt }));
  }
  if (out.decision === 'block') {
    if (/⏸|↪/.test(out.reason)) {
      queueResume(acc, session, {}, out.reason);
      return 'stopped';
    }
    if (/başka pencerede sürüyor/.test(out.reason)) return 'stopped';
    stats.anomalies.push(`unexpected block ${acc.name}/${session.sid}: ${out.reason}`);
    return 'stopped';
  }
  if (out.systemMessage && /yeniden fable/.test(out.systemMessage)) stats.reverts += 1;
  if (out.hookSpecificOutput && /Non-code research/.test(out.hookSpecificOutput.additionalContext || '')) stats.routes += 1;
  applyCall(acc, session, callCost());
  stats.workCallsToday += 1;
  if (!blackout) emitStatusline(acc, session);
  const batches = 1 + Math.floor(rng() * 4);
  for (let i = 0; i < batches; i += 1) {
    setClock(accounts, T + Math.floor(between(20, 180)));
    rollWindows(acc);
    const batch = parseOutput(timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript }));
    if (batch.continue === false) {
      const stopReason = batch.stopReason || '';
      if (/🔁/.test(stopReason)) {
        stats.fableSwitches += 1;
        timedHook(acc, { hook_event_name: 'PostModelSwitch', session_id: session.sid, from_model: session.model, to_model: 'claude-opus-5' });
        session.model = 'claude-opus-5';
        return 'worked';
      }
      queueResume(acc, session, {}, stopReason);
      return 'stopped';
    }
    let cost = callCost();
    if (session.context >= 85) {
      cost += 6;
      session.context = 25;
      stats.compactions += 1;
      timedHook(acc, { hook_event_name: 'SessionStart', source: 'compact', session_id: session.sid, cwd: lab.projectDir });
    }
    applyCall(acc, session, cost);
    stats.workCallsToday += 1;
    session.context = Math.min(95, session.context + between(2, 10));
    if (!blackout) emitStatusline(acc, session);
  }
  if (rng() < 0.03 && acc.truth.five.used < 85 && acc.truth.week.used < 85) {
    timedHook(acc, { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error_type: 'rate_limit' });
    stats.transient429 += 1;
    queueResume(acc, session);
    return 'stopped';
  }
  if (rng() < 0.4) markQueueProgress();
  if (rng() < 0.3) {
    const stop = parseOutput(timedHook(acc, { hook_event_name: 'Stop', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, stop_hook_active: (session.forced || 0) > 0 }));
    if (stop.decision === 'block') {
      stats.queueContinues += 1;
      session.forced = (session.forced || 0) + 1;
    } else if (stop.systemMessage && /ilerlemiyor/.test(stop.systemMessage)) {
      stats.stuckStops += 1;
    }
  }
  return 'ok';
}

function markQueueProgress() {
  const file = path.join(lab.projectDir, 'TASKS.md');
  const text = fs.readFileSync(file, 'utf8');
  fs.writeFileSync(file, text.replace(/\[\s?\]/, '[x]'));
}

function openQueueItems() {
  const text = fs.readFileSync(path.join(lab.projectDir, 'TASKS.md'), 'utf8');
  return (text.match(/\[\s?\]/g) || []).length;
}

function slotForResume(slots, sid) {
  return slots.find((slot) => slot.session.sid === sid);
}

async function runMarathonDay(accounts, day) {
  const turnsPerDay = 28;
  const slots = accounts.map((acc) => ({ home: acc, acc, session: newSession(acc), stopped: false }));
  for (let turn = 0; turn < turnsPerDay; turn += 1) {
    setClock(accounts, T + Math.floor(between(120, 420)));
    for (const acc of accounts) rollWindows(acc);
    const due = pendingResumes.filter((entry) => entry.resumeAt <= T);
    for (const entry of due) {
      pendingResumes.splice(pendingResumes.indexOf(entry), 1);
      rollWindows(entry.acc);
      publishTruth(entry.acc);
      const callsBefore = lab.calls().length;
      entry.acc.run(['resume', '--sid', entry.sid, '--account', entry.acc.dir]);
      const launched = lab.calls().slice(callsBefore).filter((line) => line.includes(`--resume ${entry.sid} `));
      const owner = slotForResume(slots, entry.sid);
      if (launched.length > 1) stats.anomalies.push(`double launch ${entry.acc.name}/${entry.sid}`);
      if (!launched.length) {
        const state = entry.acc.state();
        if (state.waits && state.waits[entry.sid]) {
          pendingResumes.push({ acc: entry.acc, sid: entry.sid, resumeAt: Number(state.waits[entry.sid].resumeAt), kind: state.waits[entry.sid].kind });
        } else if (owner) {
          stats.anomalies.push(`resume neither launched nor rescheduled ${entry.acc.name}/${entry.sid}`);
          owner.session = newSession(owner.home);
          owner.acc = owner.home;
          owner.stopped = false;
        }
        continue;
      }
      stats.relaunches += 1;
      checkRelaunchPrompt(entry, launched[0]);
      const match = /CONFIG=(\S+)/.exec(launched[0]);
      const targetDir = match ? match[1] : entry.acc.dir;
      const target = accounts.find((acc) => acc.dir === targetDir) || entry.acc;
      if (owner) {
        owner.acc = target;
        owner.stopped = false;
      }
    }
    for (const slot of slots) {
      if (slot.stopped) continue;
      if (turn % 4 === 2) {
        slot.home.chaos = (slot.home.chaos || 0) + 1;
        await injectChaos(slot.acc, slot.session, slot.home.chaos * 7 + (slot.home === accounts[0] ? 0 : 10), turn, accounts);
      }
      if (slot.session.stoppedByChaos) {
        slot.stopped = true;
        continue;
      }
      const result = await marathonTurn(slot.acc, slot.session, turn, accounts);
      if (result === 'stopped') slot.stopped = true;
    }
  }
  for (const slot of slots) timedHook(slot.acc, { hook_event_name: 'SessionEnd', session_id: slot.session.sid, reason: 'exit' });
  lab.setOutage('');
}

async function main() {
  refreshChecksums();
  await lab.startMock();
  const sloppy = (i) => {
    const box = i < 20 ? 'x' : ' ';
    switch (i % 9) {
      case 3: return `-[${box}] task ${i + 1} written without a space`;
      case 5: return `* [${box}] task ${i + 1} as a star bullet`;
      case 7: return `${i + 1}. [${box}] task ${i + 1} numbered\n   and continued on a second line`;
      default: return `- [${box}] task ${i + 1}`;
    }
  };
  fs.writeFileSync(path.join(lab.projectDir, 'TASKS.md'), `# queue\n${Array.from({ length: 120 }, (_, i) => (options.hard ? sloppy(i) : `- [${i < 20 ? 'x' : ' '}] task ${i + 1}`)).join('\n')}\n`);
  if (options.hard) {
    spawnSync('git', ['init', '-q'], { cwd: lab.projectDir });
    spawnSync('git', ['-c', 'user.email=soak@example.com', '-c', 'user.name=soak', 'add', '-A'], { cwd: lab.projectDir });
    spawnSync('git', ['-c', 'user.email=soak@example.com', '-c', 'user.name=soak', 'commit', '-q', '-m', 'soak start'], { cwd: lab.projectDir });
  }
  const accounts = [account('A'), account('B')];
  ACCOUNTS = accounts;
  for (const acc of accounts) acc.run(['queue', 'trust', '--file', path.join(lab.projectDir, 'TASKS.md')]);
  setClock(accounts, T);
  for (const acc of accounts) publishTruth(acc);
  const dayLog = [];
  for (let day = 1; day <= options.days; day += 1) {
    stats.workCallsToday = 0;
    if (options.hard) await runMarathonDay(accounts, day);
    else {
      for (let s = 0; s < options.sessionsPerDay; s += 1) {
        setClock(accounts, T + Math.floor(between(30 * 60, 90 * 60)));
        for (const acc of accounts) rollWindows(acc);
        processResumes(accounts);
        if (rng() < 0.35) await Promise.all(accounts.map((acc) => runSession(acc)));
        else await runSession(accounts[rng() < 0.75 ? 0 : 1]);
      }
    }
    if (options.hard && stats.workCallsToday === 0) stats.anomalies.push(`day ${day}: no work progressed`);
    setClock(accounts, Math.max(T + 60, realStart + day * DAY));
    for (const acc of accounts) rollWindows(acc);
    processResumes(accounts);
    dailyInvariants(accounts, day);
    dayLog.push(`day ${String(day).padStart(2)} | five A/B ${accounts[0].truth.five.used.toFixed(0)}/${accounts[1].truth.five.used.toFixed(0)} | week ${accounts[0].truth.week.used.toFixed(0)}/${accounts[1].truth.week.used.toFixed(0)} | fable ${accounts[0].truth.fable.used.toFixed(0)}/${accounts[1].truth.fable.used.toFixed(0)} | model ${accounts[0].settingsModel()}/${accounts[1].settingsModel()} | stops ${stats.stops} relaunch ${stats.relaunches} pending ${pendingResumes.length}`);
    process.stdout.write(`${dayLog[dayLog.length - 1]}\n`);
  }
  sweepWorks(accounts);
  lab.stopMock();
  const p95 = percentile(stats.latencies, 0.95);
  if (p95 > 400) {
    const worst = stats.timings.slice().sort((a, b) => b.ms - a.ms).slice(0, 8).map((entry) => `${entry.event}/${entry.sid} ${entry.ms}ms`).join(', ');
    stats.anomalies.push(`hook p95 latency ${p95} ms is above the 400 ms budget (slowest: ${worst})`);
  }
  if (stats.corruptions > 0 && stats.recoveries === 0) stats.anomalies.push(`${stats.corruptions} file corruption(s) and not one recovery from a backup`);
  const summary = {
    simulatedDays: options.days,
    hooks: stats.hooks,
    statuslines: stats.statuslines,
    modelCalls: stats.calls,
    stops: stats.stops,
    relaunches: stats.relaunches,
    queueContinues: stats.queueContinues,
    stuckStops: stats.stuckStops,
    chaosInjections: stats.chaos,
    stateRecoveries: stats.recoveries,
    subagentGates: stats.subagentGates,
    overloadStorms: stats.overloadStorms,
    overloadRetries: stats.overloadRetries,
    overloadGiveups: stats.overloadGiveups,
    workspaceFlags: stats.workspaceFlags,
    workflowLaunches: stats.workflowLaunches,
    workflowNotes: stats.workflowNotes,
    languageSwitches: stats.languageSwitches,
    freshAccounts: stats.freshAccounts,
    doubleResumes: stats.doubleResumes,
    priorityChecks: stats.priorityChecks,
    startNotices: stats.startNotices,
    queueNotices: stats.queueNotices,
    fableSwitches: stats.fableSwitches,
    reverts: stats.reverts,
    routes: stats.routes,
    denies: stats.denies,
    clears: stats.clears,
    compactions: stats.compactions,
    transient429: stats.transient429,
    maxUsageWhenAllowed: { five: Number(stats.maxAllowedFive.toFixed(1)), week: Number(stats.maxAllowedWeek.toFixed(1)) },
    hookLatencyMs: { p50: percentile(stats.latencies, 0.5), p95: percentile(stats.latencies, 0.95), max: Math.max(...stats.latencies) },
    breaches: stats.breaches.length,
    breachDetails: stats.breaches.slice(0, 6),
    anomalies: stats.anomalies.slice(0, 20),
  };
  process.stdout.write(`\n${JSON.stringify(summary, null, 2)}\n`);
  if (options.days >= 7) {
    const required = [
      ['stops', stats.stops, 5],
      ['relaunches', stats.relaunches, 5],
      ['scoped-model switches', stats.fableSwitches, 1],
      ['compactions', stats.compactions, 1],
    ];
    if (options.hard) required.push(['chaos injections', stats.chaosInjections, 10]);
    for (const [name, value, least] of required) {
      if (value < least) stats.anomalies.push(`this run exercised no ${name} (${value} < ${least}), so it proves nothing about them`);
    }
  }
  const failed = stats.breaches.length || stats.anomalies.length
    || stats.maxAllowedFive > THRESHOLDS.five + MAX_OVERSHOOT
    || stats.maxAllowedWeek > THRESHOLDS.week + MAX_OVERSHOOT;
  process.stdout.write(failed ? `SOAK FAILED (lab dir ${lab.root})\n` : `SOAK PASSED (lab dir ${lab.root})\n`);
  process.exitCode = failed ? 1 : 0;
}

main().catch((err) => {
  process.stderr.write(`${err.stack}\n`);
  process.exitCode = 1;
});
