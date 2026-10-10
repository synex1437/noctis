#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const { Lab, sleep, readJson, writeJson, MODEL_WINDOWS, COMPACT_REPLY_TOKENS, THRASHING, OTHER_REQUEST_ERRORS, QUIET_FAILURES, WINDOW_LIMIT, claudeCompactionPoint } = require('./harness');
const { checkContinuations, pauseEndedWithoutReason } = require('./continuations');

const options = { days: 7, seed: 1, sessionsPerDay: 4, turns: 6, hard: 0 };
for (let i = 2; i < process.argv.length; i += 2) {
  const key = process.argv[i].replace(/^--/, '');
  if (key in options) options[key] = Number(process.argv[i + 1]);
}

const HOUR = 3600;
const DAY = 86400;
const SHIPPED = readJson(path.join(__dirname, '..', 'config.default.json'));
const THRESHOLDS = { five: SHIPPED.thresholds.session5h, week: SHIPPED.thresholds.weeklyAll, fable: SHIPPED.thresholds.weeklyFable };
const FAN_OUT_HEADROOM = SHIPPED.credits.fanOutHeadroom;
const BURN_LOOKBACK = SHIPPED.burn.lookbackHours * HOUR;
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
const PLAY = { seed: options.seed };
const CONTINUING_MODES = ['answer', 'slow', 'silent', 'answer-fail'];
const pick = (list) => list[Math.floor(rng() * list.length)];
const between = (min, max) => min + rng() * (max - min);

// The context of a marathon session is counted in tokens, as Claude Code counts it, in the window of the
// session's model; its draws come from a stream of their own.
const contextRng = mulberry32(options.seed * 7919 + 104729);
const contextBetween = (min, max) => min + contextRng() * (max - min);
const contextPick = (list) => list[Math.floor(contextRng() * list.length)];
// Where a person may have Claude Code compact, in settings.json: at the window setup writes for
// compaction.compactAt 280000, at a percent of their own, at a window of one model's own as /autocompact
// writes it, or not by itself at all.
const COMPACTION_CHOICES = ['managed', 'percent', 'model', 'off'];
const MANAGED_COMPACT_WINDOW = 313000;

const realStart = Math.floor(Date.now() / 1000);
let T = realStart;
const lab = new Lab('noctis-soak');
const stats = {
  hooks: 0,
  statuslines: 0,
  calls: 0,
  stops: 0,
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
  spawnRefusals: 0,
  burnRefusals: 0,
  subagentStops: 0,
  overloadStorms: 0,
  overloadRetries: 0,
  overloadHolds: 0,
  overloadGiveups: 0,
  inHookHolds: 0,
  workspaceFlags: 0,
  workflowLaunches: 0,
  workflowNotes: 0,
  languageSwitches: 0,
  freshAccounts: 0,
  doubleResumes: 0,
  ownWindowSkips: 0,
  ownWindowStops: 0,
  ownWindowRepauses: 0,
  daysWaitedOut: 0,
  restoredPauseSkips: 0,
  priorityChecks: 0,
  workCallsToday: 0,
  startNotices: 0,
  queueNotices: 0,
  startNoticesSeen: new Set(),
  breaches: [],
  anomalies: [],
  maxAllowedFive: 0,
  maxAllowedWeek: 0,
  typedCalls: 0,
  maxTypedFive: 0,
  latencies: [],
  timings: [],
  labWaitMs: 0,
  compactionChoices: {},
  contextChecks: 0,
  contextFulls: { thrashing: 0, tooLong: 0, windowLimit: 0, cloud: 0, giveups: 0, inPlace: 0 },
  freshStarts: 0,
  otherRequestErrors: 0,
  quietFailures: { max_output_tokens: 0, unknown: 0, cloud_credential_error: 0, giveups: 0 },
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
  acc.weekTrail = [];
  acc.prefix = name.toLowerCase();
  return acc;
}

function setClock(accounts, t) {
  lab.settleLateAnswers();
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
  recordWeek(acc);
  const iso = (epoch) => new Date(epoch * 1000).toISOString();
  acc.setOwnLimits([
    { kind: 'session', percent: Number(truth.five.used.toFixed(1)), resets_at: iso(truth.five.resetsAt) },
    { kind: 'weekly_all', percent: Number(truth.week.used.toFixed(1)), resets_at: iso(truth.week.resetsAt) },
    { kind: 'weekly_scoped', percent: Number(truth.fable.used.toFixed(1)), resets_at: iso(truth.fable.resetsAt), scope: { group: 'model', model: { display_name: 'Fable' } } },
  ]);
}

function recordWeek(acc) {
  const { used, resetsAt } = acc.truth.week;
  const trail = acc.weekTrail;
  const last = trail[trail.length - 1];
  if (last && last.used === used && last.resetsAt === resetsAt) return;
  trail.push({ at: T, used, resetsAt });
  const settled = T - BURN_LOOKBACK - HOUR;
  while (trail.length > 1 && trail[1].at <= settled) trail.shift();
}

function burnsThroughWeek(acc) {
  const { used, resetsAt } = acc.truth.week;
  const start = T - BURN_LOOKBACK;
  const seen = acc.weekTrail.filter((entry) => entry.resetsAt === resetsAt && entry.at <= T).sort((a, b) => a.at - b.at);
  const last = seen.filter((entry) => entry.at <= start).pop();
  const base = last ? { used: last.used, at: start } : seen[0];
  if (!base || base.at >= T) return false;
  const perSecond = (used - base.used) / (T - base.at);
  return perSecond > 0 && used + perSecond * (resetsAt - T) >= THRESHOLDS.week;
}

let ACCOUNTS = [];

// A hook that holds the host this long waited out a usage fetch timeout (5 s). Only the blind
// probe may: every other hook waits for a fetch at most refreshWait and leaves the rest to a
// refresher.
const FETCH_TIMEOUT_HOLD_MS = 4500;
// A reset closer than wait.maxInHookMinutes is waited out in the hook itself. The soak sets 0, which
// noctis reads as its minimum of one minute, so an in-hook wait may hold the host that long (and a
// little more for the hook's own work), never longer. inHookHolds counts the in-hook waits that held
// the host at least FETCH_TIMEOUT_HOLD_MS.
const IN_HOOK_HOLD_MS = 70 * 1000;
const HUNG_ENDPOINT_HOLD_MS = 1500;

function logSince(file, offset) {
  try {
    const text = fs.readFileSync(file);
    return text.subarray(text.length >= offset ? offset : 0).toString('utf8');
  } catch {
    return '';
  }
}

function timedHook(acc, input, extraEnv = {}) {
  const log = path.join(acc.guardDir, 'guard.log');
  let logFrom = 0;
  try {
    logFrom = fs.statSync(log).size;
  } catch {}
  const started = Date.now();
  const output = acc.hook(input, extraEnv);
  const elapsed = Date.now() - started;
  // The host waits on the hook process alone. The lab then waits for any refresher the hook
  // started, which is its own time, not the host's: that is counted apart.
  const hookMs = Math.round(acc.lastRunMs);
  const event = input.hook_event_name || '?';
  stats.latencies.push(hookMs);
  stats.timings.push({ ms: hookMs, event, sid: input.session_id || '?', endpointHangs: outage === 'timeout' });
  stats.labWaitMs = Math.max(stats.labWaitMs, elapsed - hookMs);
  const hookLog = hookMs >= FETCH_TIMEOUT_HOLD_MS ? logSince(log, logFrom) : '';
  const inHookWait = hookLog.split('\n').some((line) => line.includes(` for ${input.session_id}: `) && line.includes('; inHook=true;'));
  if (inHookWait) {
    stats.inHookHolds += 1;
    if (hookMs > IN_HOOK_HOLD_MS) stats.anomalies.push(`${event} (${input.session_id}) held the host ${hookMs} ms in an in-hook wait, past the minute the soak allows`);
  } else if (hookMs >= FETCH_TIMEOUT_HOLD_MS && event !== 'StopFailure' && !/probing before allowing more work/.test(hookLog)) {
    stats.anomalies.push(`${event} (${input.session_id}) held the host ${hookMs} ms, as long as a usage fetch that times out`);
  }
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

const NEAR_EDGE = 8;

const BURST_MIN_USED = 60;

function pastLimit(acc) {
  const { five, week } = acc.truth;
  return five.used >= THRESHOLDS.five || week.used >= THRESHOLDS.week || five.used >= 100 || week.used >= 100;
}

function nearLimit(acc) {
  const { five, week } = acc.truth;
  return five.used >= THRESHOLDS.five - NEAR_EDGE || week.used >= THRESHOLDS.week - NEAR_EDGE;
}

function subagentLimited(out) {
  const specific = out.hookSpecificOutput || {};
  return specific.permissionDecision === 'deny' && /^\[noctis\] .* usage is /.test(specific.permissionDecisionReason || '');
}

function citedUsage(out) {
  const cited = /usage is ([\d.]+)%/.exec(out.hookSpecificOutput.permissionDecisionReason);
  return cited ? Number(cited[1]) : NaN;
}

function withinBurstReach(acc, out) {
  const used = citedUsage(out);
  const { five, week } = acc.truth;
  return used >= BURST_MIN_USED && used <= Math.ceil(Math.max(five.used, week.used));
}

function expectedSubagentLimit(acc, out, mainPaused) {
  if (!subagentLimited(out)) return false;
  if (pastLimit(acc) || mainPaused) return true;
  return (nearLimit(acc) || withinBurstReach(acc, out)) && /reached early at the current burn rate/.test(out.hookSpecificOutput.permissionDecisionReason);
}

// The last few guard decisions for a session, so a breach in a CI log says how the call got through.
function recentDecisions(acc, sid, count = 8) {
  let rows = [];
  try {
    rows = fs.readFileSync(path.join(acc.guardDir, 'decisions.jsonl'), 'utf8').split('\n').filter(Boolean).map((line) => {
      try {
        return JSON.parse(line);
      } catch {
        return null;
      }
    });
  } catch {
    return [];
  }
  return rows.filter((row) => row && row.sid === sid).slice(-count).map((row) => [row.event, row.action, row.hit, row.five !== undefined ? `five=${row.five}` : row.used !== undefined ? `used=${row.used}` : '', row.reason].filter(Boolean).join(' '));
}

function journaledFableSwitches(acc) {
  const lines = ['decisions.jsonl.1', 'decisions.jsonl'].flatMap((name) => {
    try {
      return fs.readFileSync(path.join(acc.guardDir, name), 'utf8').split('\n');
    } catch {
      return [];
    }
  });
  return lines.filter((line) => {
    try {
      const row = JSON.parse(line);
      return row.action === 'switch-model';
    } catch {
      return false;
    }
  }).length;
}

// A prompt the user typed goes ahead past the pause point with a warning, and its turn runs on
// until the ceiling: those calls are the user's choice, so they answer to the ceiling alone.
function typedTurnFor(acc, sid) {
  return Boolean((acc.state().typedTurns || {})[sid]);
}

function guardReading(acc) {
  const usage = readJson(path.join(acc.guardDir, 'usage.json')) || {};
  const fable = readJson(path.join(acc.guardDir, 'fable.json')) || {};
  const ofThisWindow = (reading) => reading && Math.abs((reading.resetsAt || 0) - acc.truth.five.resetsAt) < 300;
  const readings = {};
  if (ofThisWindow(usage.five_hour)) readings.statusLine = { used: usage.five_hour.used, age: T - Math.min(usage.five_hour.at || usage.updatedAt || 0, usage.updatedAt || 0) };
  if (ofThisWindow(fable.five_hour)) readings.endpoint = { used: fable.five_hour.used, age: T - (fable.fetchedAt || 0) };
  return readings;
}

function recordBreach(acc, session, cost, typed) {
  const truth = acc.truth;
  stats.breaches.push({ day: Math.floor((T - realStart) / DAY) + 1, account: acc.name, sid: session.sid, five: truth.five.used, week: truth.week.used, cost, typed, reading: guardReading(acc), trail: recentDecisions(acc, session.sid) });
}

function applyCall(acc, session, cost) {
  const truth = acc.truth;
  const before = { five: truth.five.used, week: truth.week.used };
  if (session.typedTurn) {
    if (before.five >= 100 || before.week >= 100) recordBreach(acc, session, cost, true);
    stats.typedCalls += 1;
    stats.maxTypedFive = Math.max(stats.maxTypedFive, before.five);
  } else {
    if (truth.five.used + cost > 100 || truth.week.used + cost * 0.11 > 100) recordBreach(acc, session, cost, false);
    if (before.five > THRESHOLDS.five + MAX_OVERSHOOT) {
      stats.anomalies.push(`call allowed at five=${before.five.toFixed(1)} % (threshold ${THRESHOLDS.five}) ${acc.name}/${session.sid}`);
    }
    if (before.week > THRESHOLDS.week + MAX_OVERSHOOT) {
      stats.anomalies.push(`call allowed at week=${before.week.toFixed(1)} % (threshold ${THRESHOLDS.week}) ${acc.name}/${session.sid}`);
    }
    stats.maxAllowedFive = Math.max(stats.maxAllowedFive, before.five);
    stats.maxAllowedWeek = Math.max(stats.maxAllowedWeek, before.week);
  }
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
  const line = acc.statusline(session.sid, session.model, Number(truth.five.used.toFixed(1)), truth.five.resetsAt, Number(truth.week.used.toFixed(1)), truth.week.resetsAt, contextOf(session));
  stats.statuslines += 1;
  checkContextShare(acc, session, line);
}

// What the status line hands noctis of a session's context: tokens in the model's window for a marathon
// session, a share of the window for any other.
function contextOf(session) {
  return session.tokens === undefined ? Math.round(session.context) : { tokens: Math.round(session.tokens), window: session.window };
}

function settingsOf(acc) {
  return readJson(path.join(acc.dir, 'settings.json')) || {};
}

// Where Claude Code compacts a session, in tokens of context, read from settings.json.
function compactionPointOf(acc, session) {
  return claudeCompactionPoint(settingsOf(acc), session.model, session.window);
}

// The status line shows how far a session's context is to where Claude Code compacts it, or how much of
// the window it fills while Claude Code compacts nothing by itself.
function checkContextShare(acc, session, line) {
  if (session.tokens === undefined || !line) return;
  const shown = /ctx %(\d+)/.exec(line);
  const point = compactionPointOf(acc, session);
  const tokens = Math.round(session.tokens);
  const expected = point === null ? Math.min(100, Math.round((100 * tokens) / session.window)) : Math.round((100 * tokens) / point);
  stats.contextChecks += 1;
  if (!shown || Number(shown[1]) !== expected) {
    anomaly(`status line shows ${shown ? `ctx %${shown[1]}` : 'no context'} for ${tokens} tokens of a ${session.window} window where Claude Code compacts at ${point === null ? 'no point' : point} (${acc.compaction}): ctx %${expected} expected ${acc.name}/${session.sid}`);
  }
}

// The person sets where Claude Code compacts between days, while no session of the account runs: a
// Claude Code started later takes the env of settings.json along, and reads the rest as it changes.
function chooseCompaction(acc, choice) {
  const settings = settingsOf(acc);
  const env = { ...(settings.env || {}) };
  delete env.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE;
  delete env.DISABLE_AUTO_COMPACT;
  for (const entry of Object.values(settings.modelSettings || {})) {
    if (entry && typeof entry === 'object') delete entry.autoCompactWindow;
  }
  if (choice === 'managed') settings.autoCompactWindow = MANAGED_COMPACT_WINDOW;
  if (choice === 'percent') env.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE = String(contextPick([55, 70, 85]));
  if (choice === 'model') setModelCompactWindow(acc, settings, contextPick(Object.keys(MODEL_WINDOWS)));
  if (choice === 'off') env.DISABLE_AUTO_COMPACT = '1';
  settings.env = env;
  writeJson(path.join(acc.dir, 'settings.json'), settings);
  acc.compaction = choice;
  stats.compactionChoices[choice] = (stats.compactionChoices[choice] || 0) + 1;
}

// /autocompact in a session saves a window for the session's model in modelSettings.
function setModelCompactWindow(acc, settings, model) {
  const window = contextPick([150000, 250000, 400000, 600000]);
  settings.modelSettings = { ...(settings.modelSettings || {}) };
  settings.modelSettings[model] = { ...(settings.modelSettings[model] || {}), autoCompactWindow: window };
  return window;
}

function journalSize(acc) {
  try {
    return fs.statSync(path.join(acc.guardDir, 'decisions.jsonl')).size;
  } catch {
    return 0;
  }
}

function journaledSince(acc, offset, sid, action, reason) {
  let content;
  try {
    content = fs.readFileSync(path.join(acc.guardDir, 'decisions.jsonl'));
  } catch {
    return false;
  }
  return content.subarray(content.length >= offset ? offset : 0).toString('utf8').split('\n').some((line) => {
    try {
      const row = JSON.parse(line);
      return row.sid === sid && row.action === action && (!reason || reason.test(row.reason || ''));
    } catch {
      return false;
    }
  });
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
  const entry = { acc, sid: session.sid, resumeAt: Number(wait.resumeAt), kind: wait.kind, contextFull: wait.contextFull === true, outputCap: wait.outputCap === true, ...expect };
  if (session.workflow) {
    entry.expectWorkflow = session.workflow;
    const checkpoint = acc.checkpoint(session.sid);
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
  if (entry.contextFull) {
    const fresh = Boolean(freshSessionOf(line));
    if (!fresh) stats.contextFulls.inPlace += 1;
    if (!(fresh ? /which stopped with its context full/ : /The session stopped with its context full/).test(line)) anomaly(`relaunch of a session that stopped with its context full lacks its note ${entry.acc.name}/${entry.sid}: ${line.slice(0, 200)}`);
  }
  if (entry.outputCap && !/running into the output token maximum\. Go on where it stopped, in smaller steps/.test(line)) anomaly(`relaunch of a session cut at the output token maximum lacks its note ${entry.acc.name}/${entry.sid}: ${line.slice(0, 200)}`);
}

// The fresh session a launch starts with --session-id in place of the one it took over from.
function freshSessionOf(line) {
  const match = /--session-id (\S+)/.exec(line);
  return match ? match[1] : '';
}

// The launches that go on with sid: its resume, or a fresh session that takes over from it.
function launchesOf(calls, sid) {
  return calls.filter((line) => line.includes(`--resume ${sid} `) || (line.includes(`HANDOFF=${sid} `) && Boolean(freshSessionOf(line))));
}

// The sessions fresh ones took over from, and which took over; the ids of fresh sessions are noctis's own.
const takenOver = new Map();
const takenOverBy = new Set();

// A launch that goes on with a session the soak drives in a window drives it twice at once, and so does
// one that goes on with a session a fresh one took over from.
function noteLaunch(entry, line, driving = new Set()) {
  stats.relaunches += 1;
  checkRelaunchPrompt(entry, line);
  const fresh = freshSessionOf(line);
  if (fresh) {
    stats.freshStarts += 1;
    takenOverBy.add(fresh);
  }
  if (takenOver.has(entry.sid)) anomaly(`${entry.acc.name}/${entry.sid} relaunched after the fresh session ${takenOver.get(entry.sid)} took over from it`);
  for (const sid of [entry.sid, fresh].filter(Boolean)) {
    if (driving.has(sid)) anomaly(`${entry.acc.name}/${sid} launched while it still works in its window`);
  }
  return fresh;
}

function resumeOnce(acc, sid) {
  const before = (acc.state().waits || {})[sid];
  const mark = acc.journalMark();
  const callsBefore = lab.calls().length;
  acc.run(['resume', '--sid', sid, '--account', acc.dir]);
  const calls = lab.calls().slice(callsBefore);
  const state = acc.state();
  const after = (state.waits || {})[sid];
  const journal = acc.journalSince(mark);
  const silent = pauseEndedWithoutReason({ key: sid, before, after, launched: calls.length > 0, journal });
  // A session that stopped with its context full starts afresh from its handoff note: no compaction is
  // near in the fresh session, so none holds it back.
  const workflow = ((state.workflows || {})[sid] || []).some((run) => run && !run.agent);
  if (before && before.contextFull && !before.freshFailed && !workflow && after && after.hit === 'compaction') {
    anomaly(`${acc.name}/${sid} stopped with its context full and its fresh start was held for a compaction until ${new Date(Number(after.resumeAt) * 1000).toISOString()}`);
  }
  return { before, after, calls, state, journal, silent: silent && `${silent} (${acc.name})` };
}

function requeue(entry, wait) {
  pendingResumes.push({ acc: entry.acc, sid: entry.sid, resumeAt: Number(wait.resumeAt), kind: wait.kind, contextFull: wait.contextFull === true, outputCap: wait.outputCap === true });
}

function processResumes(accounts) {
  const due = pendingResumes.filter((entry) => entry.resumeAt <= T);
  for (const entry of due) {
    pendingResumes.splice(pendingResumes.indexOf(entry), 1);
    rollWindows(entry.acc);
    publishTruth(entry.acc);
    const outcome = resumeOnce(entry.acc, entry.sid);
    const launched = outcome.calls.length;
    let fresh = '';
    if (launched > 1) stats.anomalies.push(`double launch ${entry.acc.name}/${entry.sid}`);
    if (launched === 1) {
      const call = outcome.calls[0];
      if (!call.includes(`CONFIG=${entry.acc.dir}`)) stats.anomalies.push(`relaunch on wrong account ${entry.acc.name}/${entry.sid}`);
      if (Object.keys(outcome.state.handedOff || {}).length) stats.anomalies.push(`handoff not released ${entry.acc.name}/${entry.sid}`);
      fresh = noteLaunch(entry, call);
    }
    if (fresh && !outcome.after) takenOver.set(entry.sid, fresh);
    if (outcome.after) requeue(entry, outcome.after);
    else if (!launched && !outcome.before) stats.anomalies.push(`resume neither launched nor rescheduled ${entry.acc.name}/${entry.sid}`);
    else if (outcome.silent) stats.anomalies.push(outcome.silent);
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
  if (options.hard) Object.assign(session, { window: MODEL_WINDOWS[model], tokens: Math.round(contextBetween(15000, 45000)), refills: 0 });
  return startSession(acc, session);
}

// Claude Code starts a session: in a marathon, noctis ensure first, as hooks.json has it, then the
// SessionStart hook.
function startSession(acc, session, source = 'startup') {
  const sid = session.sid;
  if (options.hard && source === 'startup') acc.run(['ensure']);
  const out = parseOutput(timedHook(acc, { hook_event_name: 'SessionStart', source, session_id: sid, cwd: lab.projectDir, transcript_path: session.transcript }));
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
  session.typedTurn = typedTurnFor(acc, session.sid);
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
    if (subagent.hookSpecificOutput) {
      if (subagentLimited(subagent) && pastLimit(acc)) stats.subagentStops += 1;
      else stats.anomalies.push(`subagent search denied ${acc.name}/${session.sid} at five=${acc.truth.five.used.toFixed(1)} week=${acc.truth.week.used.toFixed(1)}: ${subagent.hookSpecificOutput.permissionDecisionReason || ''}`);
    }
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
    timedHook(acc, { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error: 'rate_limit', error_details: '429 Too Many Requests', last_assistant_message: 'API Error: Rate limit reached' });
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
    const foreign = Object.keys(state.modelOverrides || {}).concat(Object.keys(state.waits || {})).filter((sid) => !sid.startsWith(acc.prefix) && sid !== 'unknown' && !takenOverBy.has(sid));
    if (foreign.length) stats.anomalies.push(`day ${day} ${acc.name} foreign session ids ${foreign.join(',')}`);
    for (const [sid, wait] of Object.entries(state.waits || {})) {
      if (wait && wait.contextFull === true && !wait.scheduled) stats.anomalies.push(`day ${day} ${acc.name} ${sid} stopped with its context full and waits with nothing scheduled`);
    }
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

function corruptFile(file, backedUp = true) {
  if (backedUp) stats.corruptions += 1;
  try {
    const text = fs.readFileSync(file, 'utf8');
    fs.writeFileSync(file, text.slice(0, Math.max(2, Math.floor(text.length / 2))));
  } catch {
    fs.writeFileSync(file, '{ broken');
  }
}

const CHAOS_KINDS = ['state-corrupt', 'usage-corrupt', 'fable-corrupt', 'api-error', 'api-garbage', 'api-timeout', 'statusline-blackout', 'stale-lock', 'hook-kill', 'clock-back', 'big-transcript', 'parallel-subagents', 'overload-storm', 'workspace-edit', 'queue-priorities', 'language-switch', 'workflow-launch', 'fresh-account-99', 'double-resume', 'overload-giveup', 'own-window', 'restored-pause'];
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
let outage = '';

function anomaly(text) {
  stats.anomalies.push(text);
}

function setOutage(mode) {
  outage = mode;
  lab.setOutage(mode);
}

function forceWall(acc, session) {
  rollWindows(acc);
  if (acc.truth.five.resetsAt - T <= IN_HOOK_HOLD_MS / 1000 || typedTurnFor(acc, session.sid)) return false;
  acc.truth.five.used = Math.max(acc.truth.five.used, THRESHOLDS.five + 2);
  publishTruth(acc);
  acc.statusline(session.sid, session.model, Number(acc.truth.five.used.toFixed(1)), acc.truth.five.resetsAt, Number(acc.truth.week.used.toFixed(1)), acc.truth.week.resetsAt, contextOf(session));
  const batch = parseOutput(timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript }));
  if (batch.continue !== false) {
    anomaly(`forced wall not honoured ${acc.name}/${session.sid}: ${JSON.stringify(batch)}`);
    return false;
  }
  return Boolean(acc.state().waits[session.sid]);
}

// One pause seen twice: a pause noctis makes later for the same session starts at another time or for
// another reason.
function samePause(a, b) {
  return Number(a.startedAt) === Number(b.startedAt) && a.kind === b.kind && Number(a.until) === Number(b.until);
}

async function injectChaos(acc, session, kindIndex, turn, accounts) {
  const guardDir = acc.guardDir;
  const kind = CHAOS_KINDS[kindIndex % CHAOS_KINDS.length];
  stats.chaos += 1;
  chaosSerial += 1;
  const serial = chaosSerial;
  switch (kind) {
    case 'overload-storm': {
      lab.playClaude({ ...PLAY, force: CONTINUING_MODES });
      stats.overloadStorms += 1;
      const rounds = 3 + Math.floor(rng() * 3);
      let previousDelay = 0;
      for (let i = 0; i < rounds; i += 1) {
        const type = rng() < 0.5 ? 'overloaded' : 'server_error';
        // The failed request was just written to the transcript, so the session is not idle.
        fs.utimesSync(session.transcript, T, T);
        timedHook(acc, { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error: type });
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
          const held = acc.state().waits[session.sid];
          if (!launched.length && held && Number(held.resumeAt) > T && nearLimit(acc)) {
            // The account reached its limit, or the band before a compaction, during the storm: the
            // retry waits for the reset like any other pause.
            stats.overloadHolds += 1;
            continue;
          }
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
      lab.playClaude({ ...PLAY, force: CONTINUING_MODES });
      stats.overloadStorms += 1;
      let gaveUp = false;
      for (let i = 0; i < 40 && !gaveUp; i += 1) {
        fs.utimesSync(session.transcript, T, T);
        timedHook(acc, { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error: 'overloaded' });
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
        // A prompt is no progress, so the episode outlives it; the first tools of the turn it starts end it.
        timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript });
        if ((acc.state().overload || {})[session.sid]) anomaly(`overload episode not cleared by the next batch of tools ${acc.name}/${session.sid}`);
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
      for (const other of ACCOUNTS) other.run(['queue', 'trust', '--file', file]);
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
      const line = acc.run(['statusline'], acc.statuslineInput(session.sid, session.model, Number(acc.truth.five.used.toFixed(1)), acc.truth.five.resetsAt, Number(acc.truth.week.used.toFixed(1)), acc.truth.week.resetsAt, contextOf(session)), { NOCTIS_LANG: '' });
      if (!line) anomaly(`statusline empty in ${lang} ${acc.name}/${session.sid}`);
      break;
    }
    case 'workflow-launch': {
      const name = `audit-${serial}`;
      const journaled = journalSize(acc);
      const gate = parseOutput(timedHook(acc, { hook_event_name: 'PreToolUse', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, tool_name: 'Workflow', tool_input: { script_path: `.claude/workflows/${name}.ts`, name } }));
      const denied = Boolean(gate.hookSpecificOutput && gate.hookSpecificOutput.permissionDecision === 'deny');
      const low = Object.keys(THRESHOLDS).every((key) => THRESHOLDS[key] - acc.truth[key].used >= FAN_OUT_HEADROOM + MAX_OVERSHOOT);
      if (denied && low) {
        const reason = gate.hookSpecificOutput.permissionDecisionReason || '';
        if (/The weekly limit is burning too fast/.test(reason) && journaledSince(acc, journaled, session.sid, 'deny-workflow', /^weekly burn /) && burnsThroughWeek(acc)) stats.burnRefusals += 1;
        else anomaly(`workflow denied at low usage ${acc.name}/${session.sid}: ${reason}`);
      }
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
      // A fresh account learns its usage from the endpoint alone: an outage another session's chaos left on
      // would give it no data to warn from, so the endpoint answers while the fresh account is checked.
      const held = outage;
      if (held) setOutage('');
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
      const typed = parseOutput(timedHook(fresh, { hook_event_name: 'UserPromptSubmit', session_id: sid, cwd: lab.projectDir, transcript_path: transcriptFor(fresh, sid), prompt: 'auth.js dosyasındaki hatayı düzelt' }));
      if (typed.decision === 'block' || fresh.state().waits[sid] || !/yine de devam ediyor|goes ahead anyway/.test(typed.systemMessage || '')) anomaly(`fresh account at 99%: the prompt you typed did not go ahead with a warning (${JSON.stringify(typed).slice(0, 160)})`);
      const check = fresh.runFull(['check']);
      if (check.status !== 11) anomaly(`fresh account: noctis check exit ${check.status} expected 11`);
      fresh.truth.week.used = 100;
      publishTruth(fresh);
      fs.rmSync(path.join(fresh.guardDir, 'fable.json'), { force: true });
      const prompt = parseOutput(timedHook(fresh, { hook_event_name: 'UserPromptSubmit', session_id: sid, cwd: lab.projectDir, transcript_path: transcriptFor(fresh, sid), prompt: 'auth.js dosyasındaki hatayı düzelt' }));
      const wait = fresh.state().waits[sid];
      if (prompt.decision !== 'block' || !wait || wait.window !== 'seven_day') anomaly(`fresh account at 100%: the prompt was not parked until the weekly reset (${JSON.stringify(prompt).slice(0, 160)})`);
      else if (Math.abs(Number(wait.until) - fresh.truth.week.resetsAt) > 5) anomaly('fresh account: wait not aligned with the weekly reset');
      if (!fresh.run(['status'])) anomaly('fresh account: status printed nothing');
      fresh.run(['cancel', sid]);
      if (fresh.state().waits[sid]) anomaly('fresh account: cancel left the wait');
      if (held) setOutage(held);
      break;
    }
    case 'own-window': {
      if (forceWall(acc, session)) {
        const wait = acc.state().waits[session.sid];
        setClock(accounts, Math.ceil(Number(wait.resumeAt)) + 2);
        for (const account of accounts) rollWindows(account);
        publishTruth(acc);
        lab.writeAnswer(session.transcript, acc.timeOffset);
        if (rng() < 0.5) {
          // The turn that went on in the window ends before the runner comes: the queue goes on from it,
          // as from any other turn, and the pause is over.
          const mark = acc.journalMark();
          const stop = parseOutput(timedHook(acc, { hook_event_name: 'Stop', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, stop_hook_active: (session.forced || 0) > 0 }));
          stats.ownWindowStops += 1;
          const after = acc.state().waits[session.sid];
          if (after && !samePause(after, wait)) {
            // The Stop retired the pause the turn went past, and the queue it went on with met a limit
            // that still holds (Fable over its threshold moves the session to the fallback model): that
            // is a pause of its own, and the runner resumes it like any other.
            if (!acc.journalSince(mark).some((row) => row.sid === session.sid && row.event === 'Stop' && row.action === 'skip-launch')) {
              anomaly(`a Stop made a new pause without retiring the one its session went past ${acc.name}/${session.sid}`);
            }
            stats.ownWindowRepauses += 1;
            queueResume(acc, session);
            session.stoppedByChaos = true;
            break;
          }
          if (after) anomaly(`the pause of a session that went on in its own window outlived the end of that turn ${acc.name}/${session.sid}`);
          if (stop.decision === 'block') {
            stats.queueContinues += 1;
            session.forced = (session.forced || 0) + 1;
          } else if (openQueueItems() > 0 && !stop.systemMessage && !acc.journalSince(mark).some((row) => row.sid === session.sid && row.event === 'Stop' && row.action !== 'skip-launch')) {
            anomaly(`a Stop with ${openQueueItems()} open items in a window that went on after the reset neither continued the queue nor said why ${acc.name}/${session.sid}`);
          }
        }
        const outcome = resumeOnce(acc, session.sid);
        if (outcome.calls.length) anomaly(`relaunched a session that had gone on in its own window ${acc.name}/${session.sid}`);
        else if (outcome.after) anomaly(`kept the pause of a session that had gone on in its own window ${acc.name}/${session.sid}`);
        else if (outcome.silent) anomaly(outcome.silent);
        else stats.ownWindowSkips += 1;
      }
      break;
    }
    case 'restored-pause': {
      lab.playClaude({ ...PLAY, force: ['answer'] });
      if (forceWall(acc, session)) {
        const wait = acc.state().waits[session.sid];
        setClock(accounts, Math.ceil(Number(wait.resumeAt)) + 2);
        for (const account of accounts) rollWindows(account);
        publishTruth(acc);
        const first = resumeOnce(acc, session.sid);
        if (first.calls.length !== 1 || first.after) {
          anomaly(`restored pause: the relaunch did not take the pause ${acc.name}/${session.sid}`);
          break;
        }
        const backup = readJson(`${acc.stateFile}.bak`);
        if (backup && backup.waits && backup.waits[session.sid]) {
          corruptFile(acc.stateFile);
          const second = resumeOnce(acc, session.sid);
          const reason = second.journal.find((row) => row.sid === session.sid && row.action === 'skip-launch');
          if (second.calls.length) anomaly(`relaunched a pause that came back with the state backup after its relaunch answered ${acc.name}/${session.sid}`);
          else if (second.after) anomaly(`kept a pause that came back with the state backup after its relaunch answered ${acc.name}/${session.sid}`);
          else if (!reason) anomaly(`a pause that came back with the state backup ended with no reason in the journal ${acc.name}/${session.sid}`);
          else stats.restoredPauseSkips += 1;
        }
        followFreshStart(acc, session, first.calls);
      }
      break;
    }
    case 'double-resume': {
      lab.playClaude({ ...PLAY, force: ['answer', 'slow'] });
      if (forceWall(acc, session)) {
        const wait = acc.state().waits[session.sid];
        setClock(accounts, Math.ceil(Number(wait.resumeAt)) + 2);
        for (const account of accounts) rollWindows(account);
        publishTruth(acc);
        const before = lab.calls().length;
        await Promise.all([acc.runPromise(['resume', '--sid', session.sid, '--account', acc.dir]), acc.runPromise(['resume', '--sid', session.sid, '--account', acc.dir])]);
        const calls = lab.calls().slice(before);
        const launched = launchesOf(calls, session.sid);
        if (launched.length !== 1) anomaly(`double resume launched ${launched.length} times ${acc.name}/${session.sid}`);
        else stats.doubleResumes += 1;
        if (acc.state().waits[session.sid]) anomaly(`double resume left the wait record ${acc.name}/${session.sid}`);
        if (Object.keys(acc.state().handedOff || {}).length) anomaly(`double resume left a handoff ${acc.name}/${session.sid}`);
        followFreshStart(acc, session, calls);
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
      corruptFile(path.join(guardDir, 'fable.json'), false);
      timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript });
      break;
    case 'api-error':
      setOutage('error');
      session.outageUntilTurn = turn + 3;
      break;
    case 'api-garbage':
      setOutage('garbage');
      session.outageUntilTurn = turn + 2;
      break;
    case 'api-timeout':
      setOutage('timeout');
      session.outageUntilTurn = turn + 1;
      break;
    case 'statusline-blackout':
      session.blackoutUntilTurn = turn + 6;
      break;
    case 'stale-lock': {
      const lock = path.join(guardDir, 'state.lock');
      const owner = rng() < 0.5 ? '' : '999999';
      const age = owner ? 60000 : 3000;
      fs.writeFileSync(lock, owner);
      const old = new Date(Date.now() - age);
      fs.utimesSync(lock, old, old);
      timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript }, { NOCTIS_NO_QUIET: '1' });
      if (acc.lastRunMs > 3000) anomaly(`a stale lock cost the hook ${Math.round(acc.lastRunMs)} ms ${acc.name}`);
      acc.run(['on']);
      if (fs.existsSync(lock) && Date.now() - fs.statSync(lock).mtimeMs > age / 2) anomaly(`stale lock survived a state write ${acc.name}`);
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
      timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript });
      if (acc.lastRunMs > 8000) anomaly(`a hook took ${Math.round(acc.lastRunMs)} ms on a big transcript ${acc.name}`);
      break;
    case 'parallel-subagents': {
      const spawns = [];
      for (let i = 0; i < 4; i += 1) spawns.push(acc.hookPromise({ hook_event_name: 'PreToolUse', session_id: session.sid, agent_id: `p${i}`, agent_type: 'general-purpose', tool_name: 'Write', tool_input: { file_path: path.join(lab.projectDir, `out${i}.md`) } }));
      spawns.push(acc.hookPromise({ hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript }));
      const outputs = await Promise.all(spawns);
      const mainBatch = parseOutput(outputs[4]);
      const mainPaused = mainBatch.continue === false && /⏸/.test(mainBatch.stopReason || '');
      const blocked = outputs.slice(0, 4).filter(Boolean);
      const unexpected = blocked.filter((out) => !expectedSubagentLimit(acc, parseOutput(out), mainPaused));
      stats.subagentStops += blocked.length - unexpected.length;
      if (unexpected.length) stats.anomalies.push(`general-purpose subagent write blocked ${acc.name}/${session.sid} at five=${acc.truth.five.used.toFixed(1)} week=${acc.truth.week.used.toFixed(1)}: ${unexpected[0].slice(0, 200)}`);
      break;
    }
    default:
      break;
  }
  lab.playClaude(PLAY);
  return kind;
}

function beforeRequest(acc, session) {
  if (session.tokens === undefined) return '';
  const point = compactionPointOf(acc, session);
  if (session.tokens >= session.window - COMPACT_REPLY_TOKENS && (point === null || session.overflows)) return contextFull(acc, session, contextRng() < 0.25 ? 'windowLimit' : 'tooLong');
  if (point === null || session.tokens < point) return '';
  if (session.bigReads && session.refills >= 3) return contextFull(acc, session, 'thrashing');
  compact(acc, session, point);
  return 'compacted';
}

function compact(acc, session, point) {
  const label = `${acc.name}/${session.sid}`;
  const pre = parseOutput(timedHook(acc, { hook_event_name: 'PreCompact', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, trigger: 'auto', custom_instructions: '' }));
  if (pre.decision === 'block' || pre.continue === false) anomaly(`PreCompact held up a compaction ${label}: ${JSON.stringify(pre).slice(0, 160)}`);
  const out = parseOutput(timedHook(acc, { hook_event_name: 'SessionStart', source: 'compact', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript }));
  const context = (out.hookSpecificOutput || {}).additionalContext || '';
  const directive = /Queue mode \(TASKS\.md: (\d+) open\)/.exec(context);
  const open = openQueueItems();
  if (!directive) anomaly(`queue directive missing after a compaction ${label}`);
  else if (Number(directive[1]) !== open) anomaly(`queue count ${directive[1]} after a compaction but the file has ${open} open boxes ${label}`);
  if (open > 0 && !/After the compaction/.test(context)) anomaly(`no note of the item in hand after a compaction ${label}`);
  session.refills = session.bigReads ? session.refills + 1 : 0;
  session.tokens = session.bigReads ? Math.round(point * contextBetween(0.8, 0.95)) : Math.round(contextBetween(18000, 42000));
  stats.compactions += 1;
}

// A request adds its tool results to the context; a read too large for it refills the context up to the
// compaction point at once.
function growContext(acc, session) {
  if (session.tokens === undefined) return;
  const step = Math.round(contextBetween(0.01, 0.06) * session.window);
  const point = session.bigReads ? compactionPointOf(acc, session) : null;
  session.tokens += point === null ? step : Math.max(step, point - session.tokens);
  // The transcript grows as the session works, so its age tells how long the session has been idle.
  fs.utimesSync(session.transcript, T, T);
}

function contextFull(acc, session, kind) {
  const label = `${acc.name}/${session.sid}`;
  const cloud = contextRng() < 0.08;
  const input = { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error: 'invalid_request' };
  if (kind === 'thrashing') input.last_assistant_message = THRASHING;
  else if (kind === 'windowLimit') Object.assign(input, { error: 'max_output_tokens', last_assistant_message: WINDOW_LIMIT });
  else Object.assign(input, { last_assistant_message: 'Prompt is too long', error_details: `prompt is too long: ${Math.round(session.tokens)} tokens > ${session.window} maximum` });
  const mark = acc.journalMark();
  const log = path.join(acc.guardDir, 'guard.log');
  let logFrom = 0;
  try {
    logFrom = fs.statSync(log).size;
  } catch {}
  timedHook(acc, input, cloud ? { CLAUDE_CODE_REMOTE: 'true' } : {});
  stats.contextFulls[kind] += 1;
  const wait = (acc.state().waits || {})[session.sid];
  const rows = acc.journalSince(mark).filter((row) => row.sid === session.sid);
  const notified = /notify: /.test(logSince(log, logFrom));
  if (cloud) {
    stats.contextFulls.cloud += 1;
    if (wait) anomaly(`a cloud session that stopped with its context full waits ${label}`);
    if (!rows.some((row) => row.action === 'context-full' && row.cloud) || !notified) anomaly(`a cloud session that stopped with its context full was left without a notice ${label}`);
    return clearSession(acc, session);
  }
  if (rows.some((row) => row.action === 'retry-giveup' && row.contextFull)) {
    stats.contextFulls.giveups += 1;
    if (wait) anomaly(`a session given up on after its context kept filling up still waits ${label}`);
    if (!notified) anomaly(`a session whose context kept filling up was given up on without a notification ${label}`);
    return clearSession(acc, session);
  }
  if (!wait || wait.contextFull !== true) {
    anomaly(`a session that stopped with its context full was neither set to start afresh, given up on nor left with a notice ${label}: ${JSON.stringify(rows.map((row) => row.action))}`);
    return clearSession(acc, session);
  }
  if (!wait.scheduled) anomaly(`a session that stopped with its context full waits with nothing scheduled ${label}`);
  const delay = Number(wait.resumeAt) - T;
  if (!(delay > 0 && delay <= (Number(wait.retry) > 1 ? 3600 : 30) + 60)) anomaly(`a session that stopped with its context full goes on in ${delay} s (stop ${wait.retry}) ${label}`);
  if (session.bigReads && session.stubborn === undefined) session.stubborn = contextRng() < 0.3;
  queueResume(acc, session, {}, kind);
  return 'stopped';
}

// The person clears a session noctis left to them: /clear starts a new one in the window, which has
// launched no workflow of its own.
function clearSession(acc, session) {
  const sid = `${acc.prefix}-${Math.floor(contextRng() * 1e9).toString(16)}`;
  Object.assign(session, { sid, transcript: transcriptFor(acc, sid), tokens: Math.round(contextBetween(15000, 30000)), refills: 0, bigReads: false, stubborn: undefined, overflows: false, forced: 0, workflow: undefined });
  startSession(acc, session, 'clear');
  stats.clears += 1;
  return 'worked';
}

// The fresh session a runner started in place of from goes on in the window: Claude Code started it
// with the model the launch names, in a transcript beside the one it took over from. Work whose reads
// fill a context at once fills the fresh one too.
function freshSession(acc, from, fresh, line) {
  const model = /--model \S*opus/.test(line) ? 'claude-opus-5' : 'claude-fable-5-1';
  const transcript = path.join(path.dirname(from.transcript), `${fresh}.jsonl`);
  if (!fs.existsSync(transcript)) fs.copyFileSync(lab.transcript, transcript);
  const session = { ...from, sid: fresh, model, context: 25, transcript, window: MODEL_WINDOWS[model], tokens: Math.round(contextBetween(20000, 45000)), refills: 0, forced: 0, typedTurn: false, workflow: undefined, bigReads: false, overflows: false };
  if (from.stubborn) Object.assign(session, { bigReads: true, overflows: true, tokens: session.window });
  return startSession(acc, session);
}

// A runner a chaos started may have started a fresh session in place of the window's: the window goes
// on in it.
function followFreshStart(acc, session, calls) {
  const line = launchesOf(calls, session.sid).find((call) => freshSessionOf(call));
  if (!line) return;
  const fresh = freshSessionOf(line);
  stats.freshStarts += 1;
  takenOver.set(session.sid, fresh);
  takenOverBy.add(fresh);
  Object.assign(session, freshSession(acc, session, fresh, line));
}

// Claude Code also ends a turn with invalid_request when the API turns a request down for another
// reason. That is the session's to deal with: noctis neither pauses it nor schedules anything for it.
function otherRequestError(acc, session) {
  const message = contextPick(OTHER_REQUEST_ERRORS);
  const label = `${acc.name}/${session.sid}`;
  const before = (acc.state().waits || {})[session.sid];
  const mark = acc.journalMark();
  timedHook(acc, { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error: 'invalid_request', error_details: message, last_assistant_message: `API Error: 400 ${JSON.stringify({ type: 'error', error: { type: 'invalid_request_error', message } })}` });
  const after = (acc.state().waits || {})[session.sid];
  const acted = acc.journalSince(mark).filter((row) => row.sid === session.sid && row.event === 'StopFailure');
  if (after && (!before || after.startedAt !== before.startedAt)) anomaly(`a request the API turned down for another reason set the session to wait ${label}: ${message}`);
  if (acted.length) anomaly(`a request the API turned down for another reason was acted on (${acted.map((row) => row.action).join(', ')}) ${label}`);
  stats.otherRequestErrors += 1;
}

function quietFailure(acc, session) {
  const [error, message] = contextPick(QUIET_FAILURES);
  const label = `${acc.name}/${session.sid}`;
  const before = (acc.state().waits || {})[session.sid];
  const mark = acc.journalMark();
  const log = path.join(acc.guardDir, 'guard.log');
  let logFrom = 0;
  try {
    logFrom = fs.statSync(log).size;
  } catch {}
  timedHook(acc, { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error, last_assistant_message: message });
  stats.quietFailures[error] += 1;
  const wait = (acc.state().waits || {})[session.sid];
  const rows = acc.journalSince(mark).filter((row) => row.sid === session.sid && row.event === 'StopFailure');
  const logged = logSince(log, logFrom);
  if (rows.some((row) => row.action === 'retry-giveup')) {
    stats.quietFailures.giveups += 1;
    if (wait) anomaly(`a session given up on after ${error} still waits ${label}`);
    if (!/notify: /.test(logged)) anomaly(`a session was given up on after ${error} without a notification ${label}`);
    return clearSession(acc, session);
  }
  if (wait && (!before || wait.startedAt !== before.startedAt)) {
    if (wait.window !== 'unknown') anomaly(`a session that stopped on ${error} was put down to the ${wait.window} window ${label}`);
    if (logged.includes(`[INFO ${wait.storedBy} hook] notify: `)) anomaly(`a session set to retry after ${error} raised a notification before any give-up ${label}`);
  }
  queueResume(acc, session, {}, error);
  return 'stopped';
}

async function marathonTurn(acc, session, turn, accounts) {
  rollWindows(acc);
  publishTruth(acc);
  if (session.outageUntilTurn !== undefined && turn >= session.outageUntilTurn) {
    setOutage('');
    session.outageUntilTurn = undefined;
  }
  const blackout = session.blackoutUntilTurn !== undefined && turn < session.blackoutUntilTurn;
  if (session.blackoutUntilTurn !== undefined && turn >= session.blackoutUntilTurn) session.blackoutUntilTurn = undefined;
  if (turn % 5 === 4) {
    const journaled = journalSize(acc);
    const gate = parseOutput(timedHook(acc, { hook_event_name: 'PreToolUse', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, tool_name: 'Agent', tool_input: { prompt: 'explore' } }));
    stats.subagentGates += 1;
    if (gate.hookSpecificOutput && gate.hookSpecificOutput.permissionDecision === 'deny') {
      const gateReason = gate.hookSpecificOutput.permissionDecisionReason || '';
      if (/🔁/.test(gateReason)) {
        timedHook(acc, { hook_event_name: 'PostModelSwitch', session_id: session.sid, from_model: session.model, to_model: 'claude-opus-5' });
        session.model = 'claude-opus-5';
        return 'worked';
      }
      if (!journaledSince(acc, journaled, session.sid, 'deny-subagent-spawn')) {
        queueResume(acc, session, {}, gateReason);
        return 'stopped';
      }
      stats.spawnRefusals += 1;
      if (journaledSince(acc, journaled, session.sid, 'deny-subagent-spawn', /: weekly burn /)) {
        if (burnsThroughWeek(acc)) stats.burnRefusals += 1;
        else anomaly(`subagent refused for a weekly burn the usage does not show ${acc.name}/${session.sid}: ${gateReason}`);
      }
    }
  }
  const prompt = rng() < 0.75 ? pick(CODING_PROMPTS) : pick(RESEARCH_PROMPTS);
  let out = parseOutput(timedHook(acc, { hook_event_name: 'UserPromptSubmit', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, prompt }));
  if (out.decision === 'block' && /\/model/.test(out.reason)) {
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
  session.typedTurn = typedTurnFor(acc, session.sid);
  if (out.systemMessage && /yeniden fable/.test(out.systemMessage)) stats.reverts += 1;
  if (out.hookSpecificOutput && /Non-code research/.test(out.hookSpecificOutput.additionalContext || '')) stats.routes += 1;
  // Now and then the work reads something too large for the context.
  if (!session.bigReads && compactionPointOf(acc, session) !== null && contextRng() < 0.025) session.bigReads = true;
  let opening = callCost();
  const first = beforeRequest(acc, session);
  if (first === 'stopped' || first === 'worked') return first;
  if (first === 'compacted') opening += 6;
  applyCall(acc, session, opening);
  stats.workCallsToday += 1;
  growContext(acc, session);
  if (!blackout) emitStatusline(acc, session);
  const batches = 1 + Math.floor(rng() * 4);
  for (let i = 0; i < batches; i += 1) {
    setClock(accounts, T + Math.floor(between(20, 180)));
    rollWindows(acc);
    const batch = parseOutput(timedHook(acc, { hook_event_name: 'PostToolBatch', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript }));
    if (batch.continue === false) {
      const stopReason = batch.stopReason || '';
      if (/🔁/.test(stopReason)) {
        timedHook(acc, { hook_event_name: 'PostModelSwitch', session_id: session.sid, from_model: session.model, to_model: 'claude-opus-5' });
        session.model = 'claude-opus-5';
        return 'worked';
      }
      queueResume(acc, session, {}, stopReason);
      return 'stopped';
    }
    let cost = callCost();
    const step = beforeRequest(acc, session);
    if (step === 'stopped' || step === 'worked') return step;
    if (step === 'compacted') cost += 6;
    applyCall(acc, session, cost);
    stats.workCallsToday += 1;
    session.context = Math.min(95, session.context + between(2, 10));
    growContext(acc, session);
    if (!blackout) emitStatusline(acc, session);
  }
  if (contextRng() < 0.012) {
    otherRequestError(acc, session);
    return 'ok';
  }
  if (contextRng() < 0.006) return quietFailure(acc, session);
  if (rng() < 0.03 && acc.truth.five.used < 85 && acc.truth.week.used < 85) {
    timedHook(acc, { hook_event_name: 'StopFailure', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, error: 'rate_limit' });
    const wait = acc.state().waits[session.sid];
    if (acc.truth.fable.used < THRESHOLDS.fable && (!wait || wait.window !== 'unknown')) stats.anomalies.push(`transient 429 below the Fable switch point (fable ${acc.truth.fable.used.toFixed(1)}%) filed as ${wait && wait.window} ${acc.name}/${session.sid}`);
    stats.transient429 += 1;
    queueResume(acc, session);
    return 'stopped';
  }
  if (rng() < 0.4) markQueueProgress();
  if (rng() < 0.3) {
    const mark = acc.journalMark();
    const stop = parseOutput(timedHook(acc, { hook_event_name: 'Stop', session_id: session.sid, cwd: lab.projectDir, transcript_path: session.transcript, stop_hook_active: (session.forced || 0) > 0 }));
    const wait = acc.state().waits[session.sid];
    if (stop.decision === 'block') {
      stats.queueContinues += 1;
      session.forced = (session.forced || 0) + 1;
    } else if (wait && (wait.kind === 'stop' || /⏸/.test(stop.systemMessage || ''))) {
      // A stop at a pause point (a limit, or the safety point before compaction) saves the work and ends
      // the turn like the other pauses; the runner resumes the session. The relaunch a 🔁 model switch
      // schedules is a wait record too, but the soak goes on with that session on the new model.
      if (!/⏸/.test(stop.systemMessage || '')) anomaly(`stop paused without the saved notice ${acc.name}/${session.sid}: ${JSON.stringify(stop).slice(0, 160)}`);
      queueResume(acc, session, {}, stop.systemMessage);
      return 'stopped';
    } else if (stop.systemMessage && /ilerlemiyor/.test(stop.systemMessage)) {
      stats.stuckStops += 1;
    } else if (options.hard && openQueueItems() > 0 && !stop.systemMessage && !acc.journalSince(mark).some((row) => row.sid === session.sid && row.event === 'Stop')) {
      // A queue goes on after a compaction, or a fresh start, as after any other turn: when it does
      // not, noctis says why.
      anomaly(`a Stop with ${openQueueItems()} open items neither continued the queue nor said why ${acc.name}/${session.sid}`);
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
  for (const acc of accounts) chooseCompaction(acc, day === 1 ? 'managed' : contextPick(COMPACTION_CHOICES));
  const slots = accounts.map((acc) => ({ home: acc, acc, session: newSession(acc), stopped: false }));
  for (let turn = 0; turn < turnsPerDay; turn += 1) {
    setClock(accounts, T + Math.floor(between(120, 420)));
    for (const acc of accounts) rollWindows(acc);
    if (turn === 14) {
      // Midway through the day the person runs /autocompact in a window, which takes at once.
      const slot = contextPick(slots);
      const settings = settingsOf(slot.acc);
      if (slot.acc.compaction !== 'off') {
        setModelCompactWindow(slot.acc, settings, slot.session.model);
        writeJson(path.join(slot.acc.dir, 'settings.json'), settings);
        stats.compactionChoices.autocompact = (stats.compactionChoices.autocompact || 0) + 1;
      }
    }
    const due = pendingResumes.filter((entry) => entry.resumeAt <= T);
    for (const entry of due) {
      pendingResumes.splice(pendingResumes.indexOf(entry), 1);
      rollWindows(entry.acc);
      publishTruth(entry.acc);
      const driving = new Set(slots.filter((slot) => !slot.stopped).map((slot) => slot.session.sid));
      const outcome = resumeOnce(entry.acc, entry.sid);
      const launched = launchesOf(outcome.calls, entry.sid);
      const owner = slotForResume(slots, entry.sid);
      if (launched.length > 1) stats.anomalies.push(`double launch ${entry.acc.name}/${entry.sid}`);
      const fresh = launched.length ? noteLaunch(entry, launched[0], driving) : '';
      if (outcome.after) {
        requeue(entry, outcome.after);
        continue;
      }
      if (fresh) takenOver.set(entry.sid, fresh);
      if (!launched.length) {
        const vanished = owner && (!outcome.before || outcome.calls.length) ? `resume neither launched nor rescheduled ${entry.acc.name}/${entry.sid}` : '';
        const lost = vanished || outcome.silent;
        if (lost) stats.anomalies.push(lost);
        if (owner && lost) {
          owner.session = newSession(owner.home);
          owner.acc = owner.home;
        }
        if (owner) owner.stopped = false;
        continue;
      }
      const match = /CONFIG=(\S+)/.exec(launched[0]);
      const targetDir = match ? match[1] : entry.acc.dir;
      const target = accounts.find((acc) => acc.dir === targetDir) || entry.acc;
      if (owner) {
        owner.acc = target;
        owner.stopped = false;
        // A relaunched Claude Code counts the compactions that refilled its context from nought.
        if (fresh) owner.session = freshSession(target, owner.session, fresh, launched[0]);
        else owner.session.refills = 0;
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
  // A day can pass wholly inside limits the accounts reached: every session stopped at one, to go on at
  // a reset after the day. A day with no work is then noctis keeping to the limits, not holding work back.
  const waitedOut = slots.every((slot) => slot.stopped && pendingResumes.some((entry) => entry.sid === slot.session.sid && entry.resumeAt > T && nearLimit(entry.acc)));
  for (const slot of slots) timedHook(slot.acc, { hook_event_name: 'SessionEnd', session_id: slot.session.sid, reason: 'exit' });
  setOutage('');
  return waitedOut;
}

async function main() {
  await lab.startMock();
  lab.playClaude(PLAY);
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
    let waitedOut = false;
    if (options.hard) waitedOut = await runMarathonDay(accounts, day);
    else {
      for (let s = 0; s < options.sessionsPerDay; s += 1) {
        setClock(accounts, T + Math.floor(between(30 * 60, 90 * 60)));
        for (const acc of accounts) rollWindows(acc);
        processResumes(accounts);
        if (rng() < 0.35) await Promise.all(accounts.map((acc) => runSession(acc)));
        else await runSession(accounts[rng() < 0.75 ? 0 : 1]);
      }
    }
    if (options.hard && stats.workCallsToday === 0) {
      if (waitedOut) stats.daysWaitedOut += 1;
      else stats.anomalies.push(`day ${day}: no work progressed`);
    }
    setClock(accounts, Math.max(T + 60, realStart + day * DAY));
    for (const acc of accounts) rollWindows(acc);
    processResumes(accounts);
    dailyInvariants(accounts, day);
    dayLog.push(`day ${String(day).padStart(2)} | five A/B ${accounts[0].truth.five.used.toFixed(0)}/${accounts[1].truth.five.used.toFixed(0)} | week ${accounts[0].truth.week.used.toFixed(0)}/${accounts[1].truth.week.used.toFixed(0)} | fable ${accounts[0].truth.fable.used.toFixed(0)}/${accounts[1].truth.fable.used.toFixed(0)} | model ${accounts[0].settingsModel()}/${accounts[1].settingsModel()} | stops ${stats.stops} relaunch ${stats.relaunches} pending ${pendingResumes.length}`);
    process.stdout.write(`${dayLog[dayLog.length - 1]}\n`);
  }
  sweepWorks(accounts);
  lab.stopMock();
  const unsettledPlayers = await lab.settlePlayers();
  const continuations = checkContinuations(lab.playRecords());
  for (const line of continuations.problems) stats.anomalies.push(`continuation: ${line}`);
  const slowest = (list) => list.slice().sort((a, b) => b.ms - a.ms).slice(0, 20).map((entry) => `${entry.event}/${entry.sid} ${entry.ms}ms`).join(', ');
  const guarding = stats.timings.filter((entry) => entry.event !== 'StopFailure');
  const pausing = stats.timings.filter((entry) => entry.event === 'StopFailure' && !entry.endpointHangs);
  const pausingOnAHungEndpoint = stats.timings.filter((entry) => entry.event === 'StopFailure' && entry.endpointHangs);
  const p95 = percentile(guarding.map((entry) => entry.ms), 0.95);
  if (p95 > 400) stats.anomalies.push(`hook p95 latency ${p95} ms is above the 400 ms budget (slowest: ${slowest(guarding)})`);
  const pauseTimes = pausing.map((entry) => entry.ms);
  const pauseP95 = pausing.length ? percentile(pauseTimes, 0.95) : 0;
  const pauseBudget = process.platform === 'win32' ? 5000 : 400;
  if (pauseP95 > pauseBudget) stats.anomalies.push(`StopFailure p95 latency ${pauseP95} ms is above the ${pauseBudget} ms budget (slowest: ${slowest(pausing)})`);
  const hungPauseBudget = HUNG_ENDPOINT_HOLD_MS + pauseBudget;
  const pastHungPauseBudget = pausingOnAHungEndpoint.filter((entry) => entry.ms > hungPauseBudget);
  if (pastHungPauseBudget.length) stats.anomalies.push(`StopFailure held the host past ${hungPauseBudget} ms while the usage endpoint hung (${slowest(pastHungPauseBudget)})`);
  if (stats.corruptions > 0 && stats.recoveries === 0) stats.anomalies.push(`${stats.corruptions} file corruption(s) and not one recovery from a backup`);
  const fableSwitches = accounts.reduce((sum, acc) => sum + journaledFableSwitches(acc), 0);
  if (options.days >= 7) {
    const required = [
      ['stops', stats.stops, 5],
      ['relaunches', stats.relaunches, 5],
      ['moves off Fable', fableSwitches, 1],
      ['compactions', stats.compactions, 1],
    ];
    if (options.hard) required.push(['chaos injections', stats.chaos, 10]);
    for (const [name, value, least] of required) {
      if (value < least) stats.anomalies.push(`this run exercised too few ${name} (${value} < ${least}) to prove anything about them`);
    }
  }
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
    spawnRefusals: stats.spawnRefusals,
    burnRefusals: stats.burnRefusals,
    subagentStops: stats.subagentStops,
    overloadStorms: stats.overloadStorms,
    overloadRetries: stats.overloadRetries,
    overloadHolds: stats.overloadHolds,
    overloadGiveups: stats.overloadGiveups,
    inHookHolds: stats.inHookHolds,
    workspaceFlags: stats.workspaceFlags,
    workflowLaunches: stats.workflowLaunches,
    workflowNotes: stats.workflowNotes,
    languageSwitches: stats.languageSwitches,
    freshAccounts: stats.freshAccounts,
    doubleResumes: stats.doubleResumes,
    ownWindowSkips: stats.ownWindowSkips,
    ownWindowStops: stats.ownWindowStops,
    ownWindowRepauses: stats.ownWindowRepauses,
    daysWaitedOut: stats.daysWaitedOut,
    restoredPauseSkips: stats.restoredPauseSkips,
    continuations: { ...continuations.summary, unsettledPlayers },
    priorityChecks: stats.priorityChecks,
    startNotices: stats.startNotices,
    queueNotices: stats.queueNotices,
    fableSwitches,
    reverts: stats.reverts,
    routes: stats.routes,
    denies: stats.denies,
    clears: stats.clears,
    compactions: stats.compactions,
    compactionChoices: stats.compactionChoices,
    contextChecks: stats.contextChecks,
    contextFulls: stats.contextFulls,
    freshStarts: stats.freshStarts,
    otherRequestErrors: stats.otherRequestErrors,
    quietFailures: stats.quietFailures,
    transient429: stats.transient429,
    maxUsageWhenAllowed: { five: Number(stats.maxAllowedFive.toFixed(1)), week: Number(stats.maxAllowedWeek.toFixed(1)) },
    typedTurnCalls: { calls: stats.typedCalls, maxFive: Number(stats.maxTypedFive.toFixed(1)) },
    hookLatencyMs: { p50: percentile(stats.latencies, 0.5), p95: percentile(stats.latencies, 0.95), max: Math.max(...stats.latencies) },
    labWaitForRefresherMaxMs: stats.labWaitMs,
    guardLatencyMs: { p95, budget: 400 },
    stopFailureLatencyMs: {
      p50: pausing.length ? percentile(pauseTimes, 0.5) : 0,
      p95: pauseP95,
      max: pausing.length ? Math.max(...pauseTimes) : 0,
      calls: pausing.length,
      budget: pauseBudget,
      meetsTheUnsplitBudget: pauseP95 <= 400,
      whileTheEndpointHung: { calls: pausingOnAHungEndpoint.length, max: pausingOnAHungEndpoint.length ? Math.max(...pausingOnAHungEndpoint.map((entry) => entry.ms)) : 0, budget: hungPauseBudget },
    },
    breaches: stats.breaches.length,
    breachDetails: stats.breaches.slice(0, 6),
    anomalies: stats.anomalies.slice(0, 20),
  };
  process.stdout.write(`\n${JSON.stringify(summary, null, 2)}\n`);
  const failed = stats.breaches.length || stats.anomalies.length
    || stats.maxAllowedFive > THRESHOLDS.five + MAX_OVERSHOOT
    || stats.maxAllowedWeek > THRESHOLDS.week + MAX_OVERSHOOT;
  process.stdout.write(failed ? `SOAK FAILED (lab dir ${lab.root})\n` : `SOAK PASSED (lab dir ${lab.root})\n`);
  process.exitCode = failed ? 1 : 0;
}

main().catch((err) => {
  process.stderr.write(`${err.stack}\n`);
  lab.stopMock();
  process.exit(1);
});
