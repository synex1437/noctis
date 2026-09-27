#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

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

for (const relative of ['bin/noctis', 'bin/darwin/noctis', 'scripts/install.sh']) {
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

// What the plugin ships, as bin/SHA256SUMS names it: one universal binary for macOS, one binary per
// CPU for Linux and Windows, the launcher, and bin/noctis.exe, the windows-amd64 build.
const SHIPPED = ['darwin/noctis', 'linux-amd64/noctis', 'linux-arm64/noctis', 'noctis', 'noctis.exe', 'windows-amd64/noctis.exe', 'windows-arm64/noctis.exe'];
const sums = fs.readFileSync(path.join(ROOT, 'bin', 'SHA256SUMS'), 'utf8').split('\n').filter(Boolean);
const summed = new Set(sums.map((line) => (line.split(/\s+/)[1] || '').replace(/^\*/, '')));
for (const name of SHIPPED) {
  if (!summed.has(name)) problems.push(`bin/SHA256SUMS does not list ${name}`);
}
for (const line of sums) {
  const [sum, name] = line.split(/\s+/);
  if (!/^[0-9a-f]{64}$/.test(sum)) { problems.push(`bin/SHA256SUMS has a malformed checksum for ${name}`); continue; }
  const target = path.join(ROOT, 'bin', name.replace(/^\*/, ''));
  if (!fs.existsSync(target)) { problems.push(`bin/SHA256SUMS names a missing file: ${name}`); continue; }
  const actual = crypto.createHash('sha256').update(fs.readFileSync(target)).digest('hex');
  if (actual !== sum) problems.push(`bin/SHA256SUMS is stale for ${name}`);
}
for (const old of ['darwin-amd64', 'darwin-arm64']) {
  if (fs.existsSync(path.join(ROOT, 'bin', old))) problems.push(`bin/${old}/ is left from before macOS had one universal binary: delete it, bin/darwin/noctis replaces it`);
}

// bin/darwin/noctis is one universal binary: a big-endian header listing an x86_64 and an arm64
// build, each a whole 64-bit Mach-O executable at a page-aligned offset, zeros between them. The
// arm64 build must carry the ad hoc signature Go's linker gives it, as Apple silicon runs no
// unsigned code, and every page hash in it must still match: the signature stays valid only while
// the build's bytes are exactly the ones the linker wrote, which is what building the universal
// binary must keep. This reads the file on any system; tests/contract.js runs it on a Mac.
function signatureProblem(image, dataoff, datasize) {
  if (dataoff + datasize > image.length) return 'has a code signature that runs past its end';
  const blob = image.subarray(dataoff, dataoff + datasize);
  const word = (at) => (at >= 0 && at + 4 <= blob.length ? blob.readUInt32BE(at) : -1);
  if (word(0) !== 0xfade0cc0) return 'has a code signature that is not a signature blob';
  let directory = -1;
  for (let index = 0; index < word(8) && 16 + 8 * index <= blob.length; index += 1) {
    if (word(12 + 8 * index) === 0) directory = word(16 + 8 * index);
  }
  if (word(directory) !== 0xfade0c02) return 'has a code signature without a code directory';
  const [hashes, slots, limit] = [16, 28, 32].map((field) => word(directory + field));
  const [hashSize, hashType, pageShift] = [36, 37, 39].map((field) => blob[directory + field]);
  if (hashType !== 2 || hashSize !== 32) return `has a code signature with hash type ${hashType}, not SHA-256`;
  const page = 2 ** pageShift;
  if (limit !== dataoff || slots !== Math.ceil(limit / page) || directory + hashes + 32 * slots > blob.length) {
    return 'has a code signature that does not cover exactly the bytes before it';
  }
  for (let slot = 0; slot < slots; slot += 1) {
    const hash = crypto.createHash('sha256').update(image.subarray(slot * page, Math.min(limit, (slot + 1) * page))).digest();
    if (!hash.equals(blob.subarray(directory + hashes + 32 * slot, directory + hashes + 32 * (slot + 1)))) {
      return `has a code signature that no longer matches its bytes (page ${slot} of ${slots}), so macOS would kill it`;
    }
  }
  return '';
}

function imageProblem(image, cpu) {
  const word = (at) => (at + 4 <= image.length ? image.readUInt32LE(at) : -1);
  if (word(0) !== 0xfeedfacf) return 'is not a 64-bit Mach-O image';
  if (word(4) !== cpu.type || word(8) !== cpu.subtype) return `says CPU 0x${word(4).toString(16)}/0x${word(8).toString(16)} in its own header`;
  if (word(12) !== 2) return 'is not an executable';
  const end = 32 + word(20);
  if (end > image.length) return 'has load commands that run past its end';
  let at = 32;
  let signature = null;
  for (let index = 0; index < word(16); index += 1) {
    const [command, size] = [word(at), word(at + 4)];
    if (size < 8 || size % 8 !== 0 || at + size > end) return `has a malformed load command (#${index + 1})`;
    if (command === 0x1d) signature = [word(at + 8), word(at + 12)];
    at += size;
  }
  if (at !== end) return 'has load commands that do not fill the size its header gives them';
  if (signature) return signatureProblem(image, ...signature);
  return cpu.name === 'arm64' ? 'carries no code signature, and Apple silicon runs no unsigned code' : '';
}

function universalProblems(file) {
  const data = fs.readFileSync(file);
  const word = (at) => (at + 4 <= data.length ? data.readUInt32BE(at) : -1);
  if (data.length >= 4 && data.readUInt32LE(0) === 0xfeedfacf) {
    return ['holds one CPU\'s build, not a universal binary (from node scripts/build.js --host?); run node scripts/build.js'];
  }
  if (word(0) !== 0xcafebabe) return ['is not a universal binary'];
  if (word(4) !== 2) return [`lists ${word(4)} builds instead of an x86_64 and an arm64 one`];
  const cpus = [{ name: 'x86_64', type: 0x01000007, subtype: 3 }, { name: 'arm64', type: 0x0100000c, subtype: 0 }];
  const entries = [0, 1].map((index) => {
    const [type, subtype, offset, size, align] = [0, 4, 8, 12, 16].map((field) => word(8 + 20 * index + field));
    return { cpu: cpus.find((cpu) => cpu.type === type && cpu.subtype === subtype), type, subtype, offset, size, align };
  }).sort((a, b) => a.offset - b.offset);
  const found = [];
  let end = 8 + 20 * entries.length;
  for (const { cpu, type, subtype, offset, size, align } of entries) {
    if (!cpu) return [`lists a build for CPU 0x${type.toString(16)}/0x${subtype.toString(16)}, not x86_64 (0x1000007/0x3) or arm64 (0x100000c/0x0)`];
    if (entries.filter((entry) => entry.cpu === cpu).length > 1) return [`lists two ${cpu.name} builds`];
    if (align < 12 || align > 16 || offset % 2 ** align !== 0 || offset < end || offset + size > data.length) {
      return [`puts its ${cpu.name} build at offset ${offset} (${size} bytes, aligned to 2^${align}): not on a page boundary, clear of the rest and inside the file`];
    }
    if (data.subarray(end, offset).some((byte) => byte !== 0)) found.push(`has bytes other than zeros before its ${cpu.name} build`);
    const problem = imageProblem(data.subarray(offset, offset + size), cpu);
    if (problem) found.push(`holds an ${cpu.name} build that ${problem}`);
    end = offset + size;
  }
  if (end !== data.length) found.push(`has ${data.length - end} bytes after its last build`);
  return found;
}
if (fs.existsSync(path.join(ROOT, 'bin', 'darwin', 'noctis'))) {
  for (const problem of universalProblems(path.join(ROOT, 'bin', 'darwin', 'noctis'))) problems.push(`bin/darwin/noctis ${problem}`);
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

const release = fs.readFileSync(path.join(workflowDir, 'release.yml'), 'utf8');
const releaseTrigger = /^on:\n((?: {2}.*\n)+)/m.exec(release);
if (!releaseTrigger || releaseTrigger[1] !== '  workflow_run:\n    workflows: [ci]\n    types: [completed]\n    branches: [main]\n') {
  problems.push('release.yml starts on something other than a finished ci run on main, so a version waits for a tag pushed by hand');
}
for (const [pattern, problem] of [
  [/github\.event\.workflow_run\.conclusion == 'success'/, 'would release a commit ci failed'],
  [/github\.event\.workflow_run\.event == 'push'/, 'would act on the ci run of a pull request, whose head branch a fork can name main'],
  [/git ls-remote --exit-code --tags origin "refs\/tags\/\$tag"/, 'would publish a version again on every push to main'],
  [/target_commitish: \$\{\{ github\.event\.workflow_run\.head_sha \}\}/, 'would tag the newest commit on main instead of the one ci tested'],
]) {
  if (!pattern.test(release)) problems.push(`release.yml ${problem}`);
}
const releaseCheckouts = release.match(/uses: actions\/checkout@v4\n(?: {8}.*\n)*/g) || [];
if (!releaseCheckouts.length || releaseCheckouts.some((step) => !step.includes('ref: ${{ github.event.workflow_run.head_sha }}'))) {
  problems.push('release.yml would build another commit than the one ci tested');
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
