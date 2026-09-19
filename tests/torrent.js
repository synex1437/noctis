#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const { Lab, readJson, writeJson, sleep } = require('./harness.js');

const options = {
  jobs: 2000,
  accounts: 4,
  seed: 20260919,
  quiet: false,
};
for (let i = 2; i < process.argv.length; i += 1) {
  const flag = process.argv[i];
  if (flag === '--jobs') options.jobs = Number(process.argv[++i]);
  else if (flag === '--accounts') options.accounts = Number(process.argv[++i]);
  else if (flag === '--seed') options.seed = Number(process.argv[++i]);
  else if (flag === '--quiet') options.quiet = true;
}

function makeRandom(seed) {
  let state = seed >>> 0;
  return () => {
    state = (state * 1664525 + 1013904223) >>> 0;
    return state / 4294967296;
  };
}

const problems = [];
const stats = {
  jobs: 0,
  hooks: 0,
  blocked: 0,
  allowed: 0,
  allowedOverThreshold: 0,
  parked: 0,
  parkRefused: 0,
  routed: 0,
  switches: 0,
  checkpoints: 0,
  hookMs: [],
};

function fail(what, detail) {
  problems.push(detail ? `${what}\n      ${detail}` : what);
}

const JOB_SHAPES = [
  { name: 'quick-fix', turns: [2, 5], burn: [0.1, 0.6], context: [10, 40] },
  { name: 'feature', turns: [8, 20], burn: [0.4, 1.8], context: [20, 75] },
  { name: 'refactor', turns: [20, 45], burn: [0.8, 2.5], context: [30, 92] },
  { name: 'all-nighter', turns: [40, 90], burn: [1.0, 3.0], context: [40, 96] },
  { name: 'research', turns: [10, 25], burn: [0.2, 1.0], context: [15, 60] },
];

const PROMPTS = [
  'refactor the auth middleware so the token check is one function',
  'write tests for the queue importer, including the GitHub failure path',
  'find every place we parse a date by hand and list them',
  'the webhook retry loop double-sends on a 500 — fix it',
  'summarise what changed in the scheduler since last week',
  'kimlik doğrulama akışını sadeleştir ve testlerini yaz',
  'add pagination to the issues endpoint and update the docs',
  'investigate why the status line flickers on windows terminal',
];

const TOOLS = ['Write', 'Edit', 'MultiEdit', 'Read', 'Bash', 'Grep', 'Task', 'WebFetch', 'WebSearch'];

function pick(random, list) {
  return list[Math.floor(random() * list.length) % list.length];
}

function between(random, [low, high]) {
  return low + random() * (high - low);
}

function runJob(lab, account, random, index, world) {
  const shape = pick(random, JOB_SHAPES);
  const sid = `${account.name}-job${index}`;
  const turns = Math.round(between(random, shape.turns));
  const thresholds = world.thresholds;
  let blockedAt = null;

  for (let turn = 0; turn < turns; turn += 1) {
    world.fiveUsed = Math.min(100, world.fiveUsed + between(random, shape.burn));
    world.weekUsed = Math.min(100, world.weekUsed + between(random, shape.burn) / 12);
    const context = Math.round(between(random, shape.context));
    const model = random() < 0.25 ? 'claude-fable-5-1' : 'claude-opus-5';

    account.setOwnLimits([
      { kind: 'session', percent: Math.round(world.fiveUsed), resets_at: new Date(world.fiveReset * 1000).toISOString() },
      { kind: 'weekly_all', percent: Math.round(world.weekUsed), resets_at: new Date(world.weekReset * 1000).toISOString() },
      { kind: 'weekly_scoped', percent: Math.round(world.weekUsed / 2), resets_at: new Date(world.weekReset * 1000).toISOString(), scope: { group: 'model', model: { display_name: 'Fable' } } },
    ]);
    account.statusline(sid, model, Math.round(world.fiveUsed), world.fiveReset, Math.round(world.weekUsed), world.weekReset, context);

    const roll = random();
    let event = 'UserPromptSubmit';
    let payload = { prompt: pick(random, PROMPTS) };
    if (roll >= 0.45 && roll < 0.65) {
      event = 'PostToolBatch';
      payload = { tools: [{ tool_name: pick(random, TOOLS) }, { tool_name: pick(random, TOOLS) }] };
    } else if (roll >= 0.65 && roll < 0.85) {
      event = 'PreToolUse';
      payload = { tool_name: pick(random, ['Write', 'Edit', 'MultiEdit', 'Read']), tool_input: { file_path: `src/${index}.ts` } };
    } else if (roll >= 0.85) {
      event = 'PreToolUse';
      payload = { tool_name: pick(random, ['Task', 'Agent']), tool_input: { description: 'research the api surface' } };
    }
    const isGate = event === 'UserPromptSubmit' || event === 'PostToolBatch'
      || (event === 'PreToolUse' && /Task|Agent/.test(payload.tool_name || ''));

    const started = process.hrtime.bigint();
    const out = account.run(['hook'], {
      session_id: sid,
      cwd: lab.projectDir,
      transcript_path: lab.transcript,
      hook_event_name: event,
      ...payload,
    });
    stats.hookMs.push(Number(process.hrtime.bigint() - started) / 1e6);
    stats.hooks += 1;

    const blocked = /"decision":\s*"block"|"continue":\s*false|"permissionDecision":\s*"deny"/.test(out);
    if (blocked) {
      stats.blocked += 1;
      blockedAt = { turn, five: world.fiveUsed, week: world.weekUsed };
      break;
    }
    stats.allowed += 1;

    const overFive = world.fiveUsed >= thresholds.session5h + 3;
    const overWeek = world.weekUsed >= thresholds.weeklyAll + 3;
    if (isGate && (overFive || overWeek)) {
      stats.allowedOverThreshold += 1;
      fail(`${sid} turn ${turn}: ${event} let the session past the wall`,
        `5h ${world.fiveUsed.toFixed(1)}% (limit ${thresholds.session5h}), weekly ${world.weekUsed.toFixed(1)}% (limit ${thresholds.weeklyAll}); hook said ${out.slice(0, 120) || '(nothing)'}`);
    }
    if (/hookSpecificOutput|additionalContext/.test(out)) stats.routed += 1;
  }

  if (blockedAt) {
    const wait = (account.state().waits || {})[sid];
    if (wait) {
      stats.parked += 1;
      if (!(Number(wait.resumeAt) > 0)) {
        fail(`${sid}: parked with no resume time`, JSON.stringify(wait).slice(0, 200));
      }
    } else {
      stats.parkRefused += 1;
    }
  }
  stats.jobs += 1;
  return { sid, shape: shape.name, turns, blockedAt };
}

async function main() {
  const lab = new Lab('noctis-torrent');
  await lab.startMock();
  const random = makeRandom(options.seed);
  const startedAt = Date.now();
  const nowSeconds = Math.floor(Date.now() / 1000);
  lab.setLimits([
    { kind: 'session', percent: 5, resets_at: new Date((nowSeconds + 5 * 3600) * 1000).toISOString() },
    { kind: 'weekly_all', percent: 10, resets_at: new Date((nowSeconds + 5 * 86400) * 1000).toISOString() },
    { kind: 'weekly_scoped', percent: 12, resets_at: new Date((nowSeconds + 4 * 86400) * 1000).toISOString(), scope: { group: 'model', model: { display_name: 'Fable' } } },
  ]);

  try {
    const accounts = [];
    for (let i = 0; i < options.accounts; i += 1) {
      const account = lab.account(`acct${i}`);
      account.install();
      account.useOwnToken();
      account.setConfig((config) => {
        config.wait.maxInHookMinutes = 0; 
        config.usage.staleMinutes = 0;
      });
      accounts.push(account);
    }
    const thresholds = readJson(accounts[0].configFile).thresholds;

    const now = Math.floor(Date.now() / 1000);
    const worlds = accounts.map(() => ({
      fiveUsed: 2 + random() * 10,
      weekUsed: 5 + random() * 20,
      fiveReset: now + 5 * 3600,
      weekReset: now + 5 * 86400,
      thresholds,
    }));

    const perAccount = Math.ceil(options.jobs / accounts.length);
    const report = [];
    for (let index = 0; index < perAccount; index += 1) {
      for (let a = 0; a < accounts.length; a += 1) {
        if (stats.jobs >= options.jobs) break;
        const world = worlds[a];
        if (world.fiveUsed >= 99) {
          world.fiveUsed = 1;
          world.fiveReset += 5 * 3600;
        }
        if (world.weekUsed >= 97) {
          world.weekUsed = 2 + random() * 8;
          world.weekReset += 7 * 86400;
        }
        report.push(runJob(lab, accounts[a], random, index, world));
        if (!options.quiet && stats.jobs % 250 === 0) {
          process.stdout.write(`  ${stats.jobs}/${options.jobs} işlem · ${stats.hooks} hook · ${stats.blocked} blok · ${problems.length} sorun\n`);
        }
      }
    }

    for (const account of accounts) {
      const state = account.state();

      for (const [sid, wait] of Object.entries(state.waits || {})) {
        if (!wait.cwd) fail(`${account.name}/${sid}: parked with no working directory`);
        if (!(Number(wait.resumeAt) > 0)) fail(`${account.name}/${sid}: parked with no resume time`);
      }

      const size = fs.statSync(account.stateFile).size;
      if (size > 2 * 1024 * 1024) {
        fail(`${account.name}: state.json grew to ${(size / 1024).toFixed(0)} KB over ${stats.jobs} jobs`);
      }

      for (const leftover of fs.readdirSync(account.guardDir)) {
        if (leftover.endsWith('.lock')) fail(`${account.name}: ${leftover} left behind`);
        if (leftover.endsWith('.tmp')) fail(`${account.name}: staging file ${leftover} left behind`);
      }

      const errorsFile = path.join(account.guardDir, 'errors.log');
      if (fs.existsSync(errorsFile)) {
        const lines = fs.readFileSync(errorsFile, 'utf8').split('\n').filter(Boolean);
        const unexpected = lines.filter((line) => !/no scheduler available|task scheduling failed|fable refresh (failed|skipped)|notify failed|no desktop notifier here/.test(line));
        if (unexpected.length > 0) {
          fail(`${account.name}: ${unexpected.length} unexpected error(s) logged`, unexpected.slice(-3).join('\n      '));
        }
      }
    }

    const seen = new Map();
    for (const account of accounts) {
      for (const sid of Object.keys(account.state().waits || {})) {
        if (seen.has(sid)) fail(`session ${sid} is parked in two accounts: ${seen.get(sid)} and ${account.name}`);
        seen.set(sid, account.name);
      }
    }

    const elapsed = (Date.now() - startedAt) / 1000;
    const sorted = stats.hookMs.slice().sort((a, b) => a - b);
    const at = (q) => sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * q))] || 0;
    const totalState = accounts.reduce((sum, account) => sum + fs.statSync(account.stateFile).size, 0);

    console.log('');
    console.log('── ne yapıldı ────────────────────────────────────────────');
    console.log(`  ${stats.jobs} iş · ${stats.hooks} hook · ${options.accounts} hesap · ${elapsed.toFixed(0)} s`);
    console.log(`  duvara girmeden durdurulan oturum : ${stats.blocked}`);
    console.log(`  bunlardan park edilen             : ${stats.parked}`);
    console.log(`  park edilemeyip söylenen          : ${stats.parkRefused}`);
    console.log(`  eşiği aşmasına izin verilen       : ${stats.allowedOverThreshold}  ${stats.allowedOverThreshold === 0 ? '✓' : '✗ KORUMA SIZDIRDI'}`);
    console.log('');
    console.log('── ne kadara mal oldu ───────────────────────────────────');
    console.log(`  hook süresi  medyan ${at(0.5).toFixed(1)} ms · p95 ${at(0.95).toFixed(1)} ms · p99 ${at(0.99).toFixed(1)} ms · en kötü ${at(1).toFixed(1)} ms`);
    console.log(`  durum dosyası: hesap başına ortalama ${(totalState / accounts.length / 1024).toFixed(1)} KB`);
    console.log('');

    if (problems.length > 0) {
      console.error(`TORRENT FAILED — ${problems.length} sorun:`);
      for (const problem of problems.slice(0, 40)) console.error(`  ✗ ${problem}`);
      if (problems.length > 40) console.error(`  … ve ${problems.length - 40} tane daha`);
      console.error(`lab dir: ${lab.root}`);
      process.exit(1);
    }
    console.log(`TORRENT PASSED — ${stats.jobs} iş, ${stats.hooks} hook, 0 sorun`);
  } finally {
    lab.stopMock();
    if (problems.length === 0) {
      for (let attempt = 0; attempt < 5; attempt += 1) {
        try {
          fs.rmSync(lab.root, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
          break;
        } catch {
          await sleep(500);
        }
      }
    }
  }
}

main().catch((error) => {
  console.error('TORRENT CRASHED');
  console.error(error);
  process.exit(1);
});
