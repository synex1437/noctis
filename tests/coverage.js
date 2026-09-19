#!/usr/bin/env node
'use strict';

const { execFileSync, spawnSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const GO_DIR = path.join(ROOT, 'go');
const PLATFORM_BINARY = path.join(ROOT, 'bin', 'linux-amd64', 'noctis');

const args = process.argv.slice(2);
const flag = (name) => args.includes(name);
const valueOf = (name) => {
  const at = args.indexOf(name);
  return at >= 0 ? args[at + 1] : null;
};

const work = fs.mkdtempSync(path.join(os.tmpdir(), 'noctis-cov-'));
const covdata = path.join(work, 'covdata');
const instrumented = path.join(work, 'noctis-cov');
fs.mkdirSync(covdata);

const savedBinary = path.join(work, 'noctis-real');
let binarySwapped = false;
const restore = () => {
  if (!binarySwapped) return;
  fs.copyFileSync(savedBinary, PLATFORM_BINARY);
  fs.chmodSync(PLATFORM_BINARY, 0o755);
  refreshChecksums();
  binarySwapped = false;
};
const refreshChecksums = () => {
  require(path.join(ROOT, 'tests', 'harness.js')).refreshChecksums();
};
for (const signal of ['SIGINT', 'SIGTERM', 'uncaughtException']) {
  process.on(signal, (err) => {
    restore();
    if (err instanceof Error) throw err;
    process.exit(1);
  });
}
process.on('exit', restore);

const run = (cmd, argv, options = {}) => {
  const result = spawnSync(cmd, argv, { stdio: 'pipe', encoding: 'utf8', ...options });
  if (result.status !== 0 && !options.allowFailure) {
    process.stderr.write(result.stdout || '');
    process.stderr.write(result.stderr || '');
    throw new Error(`${cmd} ${argv.join(' ')} exited ${result.status}`);
  }
  return result;
};

const parseProfile = (text) => {
  const blocks = new Map();
  for (const line of text.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('mode:')) continue;
    const at = trimmed.lastIndexOf(' ');
    const countAt = trimmed.lastIndexOf(' ', at - 1);
    const block = trimmed.slice(0, countAt);
    const statements = Number(trimmed.slice(countAt + 1, at));
    const count = Number(trimmed.slice(at + 1));
    const previous = blocks.get(block);
    blocks.set(block, { statements, count: Math.max(count, previous ? previous.count : 0) });
  }
  return blocks;
};

const percent = (blocks) => {
  let total = 0;
  let covered = 0;
  for (const { statements, count } of blocks.values()) {
    total += statements;
    if (count > 0) covered += statements;
  }
  return { total, covered, pct: total === 0 ? 0 : (covered / total) * 100 };
};

console.log('building an instrumented binary…');
run('go', ['build', '-cover', '-covermode=set', '-coverpkg=./cmd/noctis', '-o', instrumented, './cmd/noctis'],
  { cwd: GO_DIR, env: { ...process.env, CGO_ENABLED: '0' } });

console.log('running the unit tests…');
const unitProfile = path.join(work, 'unit.cov');
run('go', ['test', './cmd/noctis/', '-covermode=set', `-coverprofile=${unitProfile}`], { cwd: GO_DIR });

console.log('running the lab against it…');
fs.copyFileSync(PLATFORM_BINARY, savedBinary);
binarySwapped = true;
fs.copyFileSync(instrumented, PLATFORM_BINARY);
fs.chmodSync(PLATFORM_BINARY, 0o755);
refreshChecksums();

const lab = run('node', [path.join(ROOT, 'tests', 'lab.js')], {
  cwd: ROOT,
  env: { ...process.env, GOCOVERDIR: covdata },
  allowFailure: true,
});
const labSummary = (lab.stdout || '').split('\n').filter((line) => /checks passed/.test(line)).pop() || '';
restore();

if (lab.status !== 0) {
  process.stderr.write(lab.stdout || '');
  throw new Error('the lab failed under instrumentation; coverage would be misleading');
}

const labProfile = path.join(work, 'lab.cov');
run('go', ['tool', 'covdata', 'textfmt', `-i=${covdata}`, `-o=${labProfile}`], { cwd: GO_DIR });

const unit = parseProfile(fs.readFileSync(unitProfile, 'utf8'));
const labBlocks = parseProfile(fs.readFileSync(labProfile, 'utf8'));
const merged = new Map(labBlocks);
for (const [block, entry] of unit) {
  const existing = merged.get(block);
  merged.set(block, {
    statements: entry.statements,
    count: Math.max(entry.count, existing ? existing.count : 0),
  });
}

const mergedProfile = path.join(work, 'merged.cov');
fs.writeFileSync(mergedProfile,
  'mode: set\n' + [...merged].map(([b, e]) => `${b} ${e.statements} ${e.count}`).join('\n') + '\n');

const unitPct = percent(unit);
const labPct = percent(labBlocks);
const total = percent(merged);

console.log('');
console.log(`  ${labSummary.trim()}`);
console.log('');
console.log(`  go test alone        ${unitPct.pct.toFixed(1)}%`);
console.log(`  lab alone            ${labPct.pct.toFixed(1)}%`);
console.log(`  combined             ${total.pct.toFixed(1)}%  (${total.covered}/${total.total} statements)`);

if (flag('--uncovered')) {
  const byFunc = run('go', ['tool', 'cover', `-func=${mergedProfile}`], { cwd: GO_DIR }).stdout;
  const dead = byFunc.split('\n').filter((line) => /\s0\.0%$/.test(line));
  console.log('');
  console.log(`  ${dead.length} function(s) never reached by any test:`);
  for (const line of dead) console.log('    ' + line.replace(/.*cmd\/noctis\//, '').trim());
}

const html = valueOf('--html');
if (html) {
  run('go', ['tool', 'cover', `-html=${mergedProfile}`, `-o=${path.resolve(html)}`], { cwd: GO_DIR });
  console.log(`\n  annotated source: ${path.resolve(html)}`);
}

fs.rmSync(work, { recursive: true, force: true });
