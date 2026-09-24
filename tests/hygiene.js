#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');

const ROOT = path.join(__dirname, '..');
const problems = [];

function walk(dir, visit) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (['node_modules', '.git', 'bin'].includes(entry.name)) continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, visit);
    else visit(full);
  }
}

const TEXT = /\.(go|js|ts|json|md|ya?ml|sh|ps1|svg|txt)$/;
walk(ROOT, (file) => {
  if (!TEXT.test(file)) return;
  const data = fs.readFileSync(file);
  const relative = path.relative(ROOT, file);
  for (let i = 0; i < data.length; i += 1) {
    const byte = data[i];
    const control = byte < 0x09 || (byte >= 0x0b && byte <= 0x1f && byte !== 0x0d) || byte === 0x7f;
    if (control) {
      const line = data.subarray(0, i).toString('utf8').split('\n').length;
      problems.push(`${relative}:${line} has a control byte 0x${byte.toString(16)} — a \\b or \\t written into the source by mistake?`);
      break;
    }
  }
  const text = data.toString('utf8');
  const rtlCatalog = relative === path.join('i18n', 'ar.json') || relative === path.join('go', 'cmd', 'noctis', 'lang.go');
  const codes = rtlCatalog ? ['00a0', '2007', '202f', '200b'] : ['00a0', '2007', '202f', '200b', '200e', '200f'];
  const invisible = new RegExp('[' + codes.map((code) => '\\u' + code).join('') + ']');
  if (invisible.test(text) && !file.endsWith('.svg') && !file.endsWith('.md')) {
    problems.push(`${relative} contains an invisible space character`);
  }
  if (file.endsWith('.go') && /\t \t/.test(text)) problems.push(`${relative} mixes tabs and spaces in indentation`);
});

function isExecutable(relative, full) {
  const listed = require('child_process').spawnSync('git', ['ls-files', '-s', '--', relative], { cwd: ROOT, encoding: 'utf8' });
  if (listed.status === 0 && listed.stdout.trim()) return listed.stdout.trim().split(/\s+/)[0] === '100755';
  return Boolean(fs.statSync(full).mode & 0o111);
}

for (const relative of ['bin/noctis', 'scripts/install.sh']) {
  const full = path.join(ROOT, relative);
  if (!fs.existsSync(full)) { problems.push(`${relative} is missing`); continue; }
  if (!isExecutable(relative, full)) problems.push(`${relative} is not executable`);
}
const launcher = fs.readFileSync(path.join(ROOT, 'bin', 'noctis'));
if (!launcher.subarray(0, 2).equals(Buffer.from('#!'))) {
  problems.push('bin/noctis is not the shell launcher any more (a build overwrote it?)');
}
if (fs.existsSync(path.join(ROOT, 'package.json'))) {
  const lockfiles = ['package-lock.json', 'npm-shrinkwrap.json', 'bun.lock', 'bun.lockb'].filter((name) => fs.existsSync(path.join(ROOT, name)));
  if (lockfiles.length) {
    problems.push(`package.json and ${lockfiles.join(', ')} at the plugin root: Claude Code would run an install in every cached copy of the plugin`);
  }
}

{
  const hooksFile = path.join(ROOT, 'hooks', 'hooks.json');
  const manifest = JSON.parse(fs.readFileSync(hooksFile, 'utf8'));
  if ('modules' in manifest) {
    const modules = manifest.modules;
    if (!Array.isArray(modules) || modules.length !== 1 || typeof modules[0] !== 'string' || !modules[0].trim()) {
      problems.push(`hooks/hooks.json: "modules" must be a list naming exactly one hooks module (Claude Code refuses a second entry), not ${JSON.stringify(modules)}`);
    } else {
      const module = path.resolve(path.dirname(hooksFile), modules[0]);
      const relative = path.relative(ROOT, module);
      if (relative.startsWith('..') || path.isAbsolute(relative)) problems.push(`hooks/hooks.json names the hooks module ${modules[0]}, which is outside the plugin`);
      else if (!fs.existsSync(module) || !fs.statSync(module).isFile()) problems.push(`hooks/hooks.json names the hooks module ${modules[0]}, which does not exist`);
      if (!/\.(ts|tsx|jsx|js|mjs|cjs|mts|cts)$/.test(module)) problems.push(`hooks/hooks.json names the hooks module ${modules[0]}, which is not named like code, so Claude Code would not load it`);
    }
  }
}

const sums = fs.readFileSync(path.join(ROOT, 'bin', 'SHA256SUMS'), 'utf8').split('\n').filter(Boolean);
if (sums.length < 6) problems.push(`bin/SHA256SUMS lists only ${sums.length} binaries`);
for (const line of sums) {
  const [sum, name] = line.split(/\s+/);
  if (!/^[0-9a-f]{64}$/.test(sum)) { problems.push(`bin/SHA256SUMS has a malformed checksum for ${name}`); continue; }
  const target = path.join(ROOT, 'bin', name.replace(/^\*/, ''));
  if (!fs.existsSync(target)) { problems.push(`bin/SHA256SUMS names a missing file: ${name}`); continue; }
  const actual = require('crypto').createHash('sha256').update(fs.readFileSync(target)).digest('hex');
  if (actual !== sum) problems.push(`bin/SHA256SUMS is stale for ${name}`);
}

walk(ROOT, (file) => {
  const relative = path.relative(ROOT, file);
  if (relative.startsWith('bin' + path.sep) || relative === 'bin') return;
  const info = fs.statSync(file);
  if (info.size > 1024 * 1024 && (info.mode & 0o111) && !TEXT.test(file)) {
    problems.push(`${relative} looks like a stray build output (${Math.round(info.size / 1048576)} MB, executable, outside bin/)`);
  }
});

{
  const result = require('child_process').spawnSync(process.execPath, [path.join(ROOT, 'scripts', 'i18n.js'), 'check'], { encoding: 'utf8' });
  if (result.status !== 0) problems.push((result.stderr || result.stdout || 'i18n check failed').trim());
}

for (const name of fs.readdirSync(path.join(ROOT, 'tests')).filter((file) => file.endsWith('.js'))) {
  const text = fs.readFileSync(path.join(ROOT, 'tests', name), 'utf8');
  for (const [call] of text.matchAll(/writeJson\([^;]*stateFile[^;]*\)/g)) {
    problems.push(`tests/${name} replaces state.json behind the plugin's back: ${call.slice(0, 60)} — use editState or putState, which write under state.lock`);
  }
}

walk(ROOT, (file) => {
  const relative = path.relative(ROOT, file);
  if (/\.tmp$/.test(relative) || /^\.\d+\.tmp$/.test(path.basename(relative))) {
    problems.push(`${relative} is a leftover temp file — an atomic write died before its rename`);
  }
});

const pluginVersion = JSON.parse(fs.readFileSync(path.join(ROOT, '.claude-plugin', 'plugin.json'), 'utf8')).version;
const marketplace = JSON.parse(fs.readFileSync(path.join(ROOT, '.claude-plugin', 'marketplace.json'), 'utf8'));
if (marketplace.plugins[0].version !== pluginVersion) problems.push(`marketplace.json says ${marketplace.plugins[0].version}, plugin.json says ${pluginVersion}`);
const storeGo = fs.readFileSync(path.join(ROOT, 'go', 'cmd', 'noctis', 'store.go'), 'utf8');
const inGo = /pluginVersion\s+=\s+"([^"]+)"/.exec(storeGo);
if (!inGo || inGo[1] !== pluginVersion) problems.push(`store.go says ${inGo && inGo[1]}, plugin.json says ${pluginVersion}`);

const fuzzSource = fs.readFileSync(path.join(ROOT, 'go', 'cmd', 'noctis', 'fuzz_test.go'), 'utf8');
const ci = fs.readFileSync(path.join(ROOT, '.github', 'workflows', 'ci.yml'), 'utf8');
const targets = [...fuzzSource.matchAll(/^func (Fuzz\w+)\(/gm)].map((found) => found[1]);
if (!targets.length) problems.push('fuzz_test.go declares no fuzz targets');
for (const target of targets) {
  if (!new RegExp(`\\b${target}\\b`).test(ci)) problems.push(`ci.yml never runs the fuzz target ${target}`);
}

const workflowDir = path.join(ROOT, '.github', 'workflows');
for (const name of fs.readdirSync(workflowDir).filter((file) => /\.ya?ml$/.test(file))) {
  const workflow = fs.readFileSync(path.join(workflowDir, name), 'utf8');
  if (!/\bnode tests\//.test(workflow)) continue;
  const at = workflow.search(/^jobs:/m);
  const jobs = at < 0 ? [] : workflow.slice(at).split(/^ {2}(?=[\w-]+:\s*$)/m).slice(1);
  if (!jobs.length) problems.push(`.github/workflows/${name} runs the suites, but its jobs could not be read`);
  for (const job of jobs) {
    if (!/^ {4}timeout-minutes: *\d+/m.test(job)) {
      problems.push(`.github/workflows/${name}: job ${job.slice(0, job.indexOf(':'))} sets no timeout-minutes, so a suite that hangs holds the runner for six hours`);
    }
  }
}

const engineSources = fs.readdirSync(path.join(ROOT, 'go', 'cmd', 'noctis'))
  .filter((name) => name.endsWith('.go') && !name.endsWith('_test.go'))
  .map((name) => ({ name, text: fs.readFileSync(path.join(ROOT, 'go', 'cmd', 'noctis', name), 'utf8') }));
const template = /func emptyState\(\) object \{\s*return object\{([\s\S]*?)\n\t\}/.exec(storeGo);
if (!template) {
  problems.push('emptyState() could not be read, so the state schema cannot be checked');
} else {
  const shapes = new Map();
  for (const [, key, value] of template[1].matchAll(/"([A-Za-z0-9_]+)":\s*([^,\n]+),/g)) {
    const text = value.trim();
    shapes.set(key, text.startsWith('object{') ? 'map' : text.startsWith('[]any') ? 'list' : text.startsWith('float64') ? 'number' : 'any');
  }
  const expect = (file, key, wanted, how) => {
    if (shapes.get(key) !== wanted && shapes.get(key) !== 'any') {
      problems.push(`${file}: ${how} state key "${key}" as a ${wanted}, but emptyState() declares it a ${shapes.get(key)}`);
    }
  };
  for (const { name, text } of engineSources) {
    for (const [, key] of text.matchAll(/\bgetMap\([A-Za-z_][A-Za-z0-9_]*,\s*"([A-Za-z0-9_]+)"\)/g)) {
      if (shapes.has(key)) expect(name, key, 'map', 'reads');
    }
    for (const [, key] of text.matchAll(/\bgetList\([A-Za-z_][A-Za-z0-9_]*,\s*"([A-Za-z0-9_]+)"\)/g)) {
      if (shapes.has(key)) expect(name, key, 'list', 'reads');
    }
    for (const [, key] of text.matchAll(/\bnumberOr\([A-Za-z_][A-Za-z0-9_]*,\s*"([A-Za-z0-9_]+)",/g)) {
      if (shapes.has(key)) expect(name, key, 'number', 'reads');
    }
  }
  for (const { name, text } of engineSources) {
    for (const found of text.matchAll(/updateState\(func\((\w+) object\) \{/g)) {
      const variable = found[1];
      let depth = 0;
      let index = text.indexOf('{', found.index + found[0].length - 1);
      const start = index;
      for (; index < text.length; index += 1) {
        if (text[index] === '{') depth += 1;
        else if (text[index] === '}') { depth -= 1; if (depth === 0) break; }
      }
      const body = text.slice(start, index);
      const written = new Set();
      for (const [, key] of body.matchAll(new RegExp(`\\b(?:stateMap\\(${variable},\\s*|${variable}\\[)"([A-Za-z0-9_]+)"`, 'g'))) written.add(key);
      for (const key of written) {
        if (!shapes.has(key)) problems.push(`${name}: updateState writes state key "${key}", which emptyState() does not declare, so readState() drops it`);
      }
    }
  }
}

if (problems.length) {
  process.stdout.write(`HYGIENE FAILED\n${problems.map((line) => `  ${line}`).join('\n')}\n`);
  process.exitCode = 1;
} else {
  process.stdout.write('hygiene: source, launcher, checksums, versions and the state schema all agree\n');
}
