#!/usr/bin/env node
'use strict';

// The Go unit tests, run as parallel processes. `go test ./...` runs cmd/noctis's ~900 tests one
// after another in a single process, and nearly all of that time is spent waiting on sleeps,
// timeouts and child processes; the tests cannot use t.Parallel because hundreds of them call
// t.Setenv. So this builds the test binary once, splits its top-level tests into shards by the
// seconds recorded in tests/gotest-durations.json (longest first, each onto the least-loaded
// shard) and runs the shards at once, each in the package folder with a temporary folder of its
// own. Quiet when everything passes; prints the whole output of a shard that fails.
//
//   node tests/gotest.js [--shards N] [--run REGEXP] [--race] [--cover] [--coverprofile FILE] [-v]
//   node tests/gotest.js --record    # run the tests and rewrite tests/gotest-durations.json
//
// Other Go packages, if any appear next to cmd/noctis, run with a plain `go test` beside the shards.

const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawn, spawnSync } = require('child_process');

const ROOT = path.resolve(__dirname, '..');
const GO_DIR = path.join(ROOT, 'go');
const PACKAGE = './cmd/noctis';
const PACKAGE_DIR = path.join(GO_DIR, 'cmd', 'noctis');
const DURATIONS = path.join(__dirname, 'gotest-durations.json');
const IS_WINDOWS = process.platform === 'win32';

const UNKNOWN_SECONDS = 0.1; // a test the durations file does not list yet
const MIN_SECONDS = 0.01; // recorded floor, so a 0.00 s test still weighs something when shards are filled
const MAX_PATTERN = 8000; // characters of -test.run per process: a Windows command line holds 32 767
const TEST_TIMEOUT = '10m0s'; // go test's default -timeout
const KILL_AFTER_MS = 11 * 60 * 1000; // go test's backstop: that timeout plus a minute
// The tests mostly wait, so more processes than CPUs pay off: on 4 CPUs, 4/6/8/12/16 shards took
// 65/44/33/24/19 s. Past 16 the slowest single test (~16 s) sets the wall time anyway.
const SHARDS_PER_CPU = 4;
const MAX_SHARDS = 16;

const USAGE = `usage: node tests/gotest.js [--shards N] [--run REGEXP] [--race] [--cover] [--coverprofile FILE] [-v] [--record]

  --shards N           processes to spread the tests over (default ${SHARDS_PER_CPU} per CPU, at most ${MAX_SHARDS})
  --run REGEXP         only the tests go test -run REGEXP would run
  --race, --cover      build the test binary with -race or -cover
  --coverprofile FILE  also write the merged coverage profile to FILE (implies --cover)
  -v                   print every shard's output, as go test -v does
  --record             after a clean run, rewrite tests/gotest-durations.json with the measured seconds`;

function parseArgs(argv) {
  const options = { shards: 0, run: '', race: false, cover: false, coverprofile: '', verbose: false, record: false };
  const valued = new Set(['shards', 'run', 'coverprofile']);
  for (let index = 0; index < argv.length; index += 1) {
    const found = /^--?([a-z]+)(?:=([\s\S]*))?$/.exec(argv[index]);
    const name = found && found[1];
    let value = found && found[2];
    if (name && valued.has(name) && value === undefined) {
      index += 1;
      if (index >= argv.length) usage(`--${name} needs a value`);
      value = argv[index];
    }
    switch (name) {
      case 'shards':
        options.shards = Number(value);
        if (!Number.isInteger(options.shards) || options.shards < 1) usage(`--shards takes a whole number above 0, not ${value}`);
        break;
      case 'run': options.run = value; break;
      case 'coverprofile': options.coverprofile = path.resolve(value); options.cover = true; break;
      case 'race': options.race = true; break;
      case 'cover': options.cover = true; break;
      case 'v': case 'verbose': options.verbose = true; break;
      case 'record': options.record = true; break;
      case 'h': case 'help': process.stdout.write(`${USAGE}\n`); process.exit(0); break;
      default: usage(`unknown argument ${argv[index]}`);
    }
    if (valued.has(name) || value === undefined) continue;
    usage(`--${name} takes no value`);
  }
  if (options.record && (options.race || options.cover)) usage('--record measures a plain build; leave out --race and --cover');
  return options;
}

function usage(problem) {
  process.stderr.write(`gotest: ${problem}\n${USAGE}\n`);
  process.exit(2);
}

function defaultShards(cpus) {
  return Math.max(2, Math.min(MAX_SHARDS, SHARDS_PER_CPU * cpus));
}

// Go's testing.splitRegexp: the pattern's top-level alternatives, each a list of '/'-separated
// levels. The first level picks the top-level tests; the rest go to the test binary unchanged.
function splitPattern(pattern) {
  const alternatives = [];
  let levels = [];
  let rest = pattern;
  let brackets = 0;
  let parens = 0;
  for (let index = 0; index < rest.length;) {
    const char = rest[index];
    if (char === '[') brackets += 1;
    else if (char === ']') brackets = Math.max(0, brackets - 1);
    else if (char === '(' && brackets === 0) parens += 1;
    else if (char === ')' && brackets === 0) parens -= 1;
    else if (char === '\\') index += 1;
    else if ((char === '/' || char === '|') && brackets === 0 && parens === 0) {
      levels.push(rest.slice(0, index));
      rest = rest.slice(index + 1);
      index = 0;
      if (char === '|') {
        alternatives.push(levels);
        levels = [];
      }
      continue;
    }
    index += 1;
  }
  levels.push(rest);
  alternatives.push(levels);
  return alternatives.map(([top, ...below]) => ({ top, below: below.join('/') }));
}

function escapeRegexp(name) {
  return name.replace(/[\\^$.*+?()[\]{}|/]/g, '\\$&');
}

// Splits one shard's tests into -test.run patterns short enough for any command line.
function patterns(tests, selections) {
  const groups = selections.map((selection) => ({ below: selection.below, names: selection.names }));
  const plain = groups.filter((group) => !group.below);
  const nested = groups.filter((group) => group.below);
  const render = (chosen) => {
    const parts = [];
    const topOnly = chosen.filter((name) => plain.some((group) => group.names.has(name)));
    if (topOnly.length) parts.push(`^(${topOnly.map(escapeRegexp).join('|')})$`);
    for (const group of nested) {
      const mine = chosen.filter((name) => group.names.has(name));
      if (mine.length) parts.push(`^(${mine.map(escapeRegexp).join('|')})$/${group.below}`);
    }
    return parts.join('|');
  };
  const chunks = [];
  let current = [];
  let length = 0;
  for (const name of tests) {
    const cost = (escapeRegexp(name).length + 1) * (1 + nested.filter((group) => group.names.has(name)).length);
    if (current.length && length + cost > MAX_PATTERN) {
      chunks.push(current);
      current = [];
      length = nested.reduce((sum, group) => sum + group.below.length + 8, 8);
    }
    current.push(name);
    length += cost;
  }
  if (current.length) chunks.push(current);
  return chunks.map((chosen) => ({ tests: chosen, pattern: render(chosen) }));
}

// Longest processing time first: each test, slowest first, onto the shard with the least work.
function assign(tests, count, durations) {
  const shards = Array.from({ length: count }, (_, index) => ({ index, tests: [], estimate: 0 }));
  const weighted = tests
    .map((name) => ({ name, seconds: typeof durations[name] === 'number' ? durations[name] : UNKNOWN_SECONDS }))
    .sort((a, b) => b.seconds - a.seconds || (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  for (const test of weighted) {
    let lightest = shards[0];
    for (const shard of shards) if (shard.estimate < lightest.estimate) lightest = shard;
    lightest.tests.push(test.name);
    lightest.estimate += test.seconds;
  }
  return shards.filter((shard) => shard.tests.length);
}

function readDurations() {
  try {
    const parsed = JSON.parse(fs.readFileSync(DURATIONS, 'utf8'));
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed;
    throw new Error('not an object of test names');
  } catch (error) {
    if (error.code !== 'ENOENT') process.stderr.write(`gotest: ignoring ${path.relative(ROOT, DURATIONS)} (${error.message}); every test weighs ${UNKNOWN_SECONDS} s\n`);
    return {};
  }
}

function writeDurations(measured, listed) {
  const previous = readDurations();
  const next = {};
  for (const name of [...listed].sort()) {
    if (measured.has(name)) next[name] = Math.max(MIN_SECONDS, Math.round(measured.get(name) * 100) / 100);
    else if (typeof previous[name] === 'number') next[name] = previous[name];
  }
  const staging = `${DURATIONS}.${process.pid}.tmp`;
  fs.writeFileSync(staging, `${JSON.stringify(next, null, 2)}\n`);
  fs.renameSync(staging, DURATIONS);
  const added = Object.keys(next).filter((name) => !(name in previous)).length;
  const dropped = Object.keys(previous).filter((name) => !(name in next)).length;
  const total = Object.values(next).reduce((sum, seconds) => sum + seconds, 0);
  console.log(`gotest: recorded ${plural(measured.size, 'test')} in ${path.relative(ROOT, DURATIONS)} (${Object.keys(next).length} listed, ${added} new, ${dropped} gone, ${total.toFixed(1)} s in all)`);
}

function envKey(env, name) {
  return Object.keys(env).find((key) => (IS_WINDOWS ? key.toUpperCase() === name.toUpperCase() : key === name));
}

function deleteEnv(env, name) {
  for (let key = envKey(env, name); key !== undefined; key = envKey(env, name)) delete env[key];
}

function setEnv(env, name, value) {
  deleteEnv(env, name);
  env[name] = value;
}

// What go test gives a test binary: the package folder as its working directory (and PWD), the
// toolchain's bin first on PATH, stdin from the null device, stdout and stderr on one stream.
// Each shard also gets its own TMPDIR, TEMP and TMP, so no two shards share a temporary folder.
// The caller's proxy settings stay out, as on a CI runner: with one set, the tests that schedule
// a runner without a sandbox write proxies/<session>.json into the package folder, which every
// shard shares. The tests about proxies set their own.
function testEnv(goroot, tmp) {
  const env = { ...process.env };
  if (goroot) {
    const key = envKey(env, 'PATH') || 'PATH';
    const value = env[key];
    setEnv(env, key, value ? `${path.join(goroot, 'bin')}${path.delimiter}${value}` : path.join(goroot, 'bin'));
  }
  setEnv(env, 'PWD', PACKAGE_DIR);
  for (const name of ['TMPDIR', 'TEMP', 'TMP']) setEnv(env, name, tmp);
  for (const name of ['HTTPS_PROXY', 'HTTP_PROXY', 'NO_PROXY', 'https_proxy', 'http_proxy', 'no_proxy']) deleteEnv(env, name);
  return env;
}

const running = new Set();
let interrupted = false;

function stop(child, signal) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  if (IS_WINDOWS) {
    spawnSync('taskkill', ['/pid', String(child.pid), '/t', '/f'], { stdio: 'ignore', windowsHide: true });
    return;
  }
  try {
    if (child.grouped) process.kill(-child.pid, signal);
    else child.kill(signal);
  } catch {
    try { child.kill(signal); } catch { /* already gone */ }
  }
}

function start(command, args, options) {
  const grouped = Boolean(options.detached) && !IS_WINDOWS;
  const child = spawn(command, args, { windowsHide: true, ...options, detached: grouped });
  child.grouped = grouped;
  running.add(child);
  const done = new Promise((resolve) => {
    let settled = false;
    const settle = (code, signal, error) => {
      if (settled) return;
      settled = true;
      running.delete(child);
      resolve({ code, signal, error });
    };
    child.on('error', (error) => settle(null, null, error));
    child.on('exit', (code, signal) => settle(code, signal, null));
  });
  return { child, done };
}

function goOutput(args) {
  const result = spawnSync('go', args, { cwd: GO_DIR, encoding: 'utf8', windowsHide: true });
  if (result.error || result.status !== 0) {
    throw new Error(`go ${args.join(' ')} failed: ${(result.error && result.error.message) || (result.stderr || '').trim()}`);
  }
  return result.stdout.trim();
}

function removeQuietly(dir) {
  try {
    fs.rmSync(dir, { recursive: true, force: true, maxRetries: 3, retryDelay: 200 });
  } catch (error) {
    process.stderr.write(`gotest: could not remove ${dir}: ${error.message}\n`);
  }
}

function plural(count, noun) {
  return `${count} ${noun}${count === 1 ? '' : 's'}`;
}

function seconds(ms) {
  return `${(ms / 1000).toFixed(1)} s`;
}

function clean(text) {
  return text.endsWith('\n') || !text ? text : `${text}\n`;
}

// One process of the test binary. Its output goes to a file rather than a pipe: a helper a test
// leaves running cannot hold the shard open by keeping the pipe, and stdout and stderr land in
// one stream in the order they were written.
async function runTests(binary, work, shard, chunk, options) {
  const tag = `${shard.index + 1}.${chunk.index + 1}`;
  const logFile = path.join(work, `s${tag}.log`);
  const args = ['-test.paniconexit0', `-test.timeout=${TEST_TIMEOUT}`, '-test.count=1', '-test.v=true', `-test.run=${chunk.pattern}`];
  if (options.cover) {
    chunk.profile = path.join(work, `s${tag}.cover`);
    args.push(`-test.coverprofile=${chunk.profile}`);
  }
  const fd = fs.openSync(logFile, 'w');
  let started;
  try {
    started = start(binary, args, { cwd: PACKAGE_DIR, env: shard.env, stdio: ['ignore', fd, fd], detached: true });
  } finally {
    fs.closeSync(fd);
  }
  let timedOut = false;
  const alarm = setTimeout(() => {
    timedOut = true;
    stop(started.child, IS_WINDOWS ? 'SIGKILL' : 'SIGQUIT');
    setTimeout(() => stop(started.child, 'SIGKILL'), 60 * 1000).unref();
  }, KILL_AFTER_MS);
  const { code, signal, error } = await started.done;
  clearTimeout(alarm);
  let output = fs.readFileSync(logFile, 'utf8');
  if (error) output += `\ngotest: could not start ${binary}: ${error.message}\n`;
  if (timedOut) output += `\n*** Test killed: ran too long (${KILL_AFTER_MS / 60000}m0s).\n`;
  return { code, signal, error, output };
}

// The result lines -test.v prints for top-level tests. A line a test indents (t.Log output, a
// child's output logged back) is not one; output a test wrote without a newline may come before one.
const RESULT = /(?<![ \t])--- (PASS|FAIL|SKIP): ([^\s/]+) \((\d+(?:\.\d+)?)s\)\r?$/gm;
const STARTED = /(?<![ \t])=== RUN {3}([^\s/]+)\r?$/gm;

function readResults(output, expected) {
  const results = new Map();
  const started = new Set();
  for (const [, name] of output.matchAll(STARTED)) if (expected.has(name)) started.add(name);
  for (const [, status, name, time] of output.matchAll(RESULT)) {
    if (expected.has(name)) results.set(name, { status, seconds: Number(time) });
  }
  return { results, started };
}

function mergeProfiles(files) {
  let mode = '';
  const blocks = new Map();
  for (const file of files) {
    let text;
    try {
      text = fs.readFileSync(file, 'utf8');
    } catch {
      continue;
    }
    for (const line of text.split(/\r?\n/)) {
      if (!line) continue;
      if (line.startsWith('mode: ')) {
        mode = mode || line.slice(6).trim();
        continue;
      }
      const cut = line.lastIndexOf(' ');
      const key = line.slice(0, cut);
      const count = Number(line.slice(cut + 1));
      const before = blocks.get(key) || 0;
      blocks.set(key, mode === 'set' ? Math.max(before, count) : before + count);
    }
  }
  let statements = 0;
  let covered = 0;
  for (const [key, count] of blocks) {
    const size = Number(key.slice(key.lastIndexOf(' ') + 1));
    statements += size;
    if (count > 0) covered += size;
  }
  return { mode, blocks, percent: statements ? (100 * covered) / statements : 0 };
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const began = Date.now();
  const work = fs.mkdtempSync(path.join(os.tmpdir(), 'gotest-'));
  const onSignal = (signal) => {
    if (interrupted) return;
    interrupted = true;
    process.stderr.write(`\ngotest: ${signal}: stopping ${running.size} process(es)\n`);
    for (const child of running) stop(child, 'SIGKILL');
    removeQuietly(work);
    process.exit(signal === 'SIGINT' ? 130 : 143);
  };
  for (const signal of ['SIGINT', 'SIGTERM', 'SIGHUP']) process.on(signal, onSignal);

  try {
    const goroot = goOutput(['env', 'GOROOT']);
    const here = path.resolve(PACKAGE_DIR).toLowerCase();
    let others = [];
    try {
      others = goOutput(['list', '-f', '{{if or .TestGoFiles .XTestGoFiles}}{{.Dir}}\t{{.ImportPath}}{{end}}', './...'])
        .split(/\r?\n/).filter(Boolean).map((line) => line.split('\t'))
        .filter(([dir]) => path.resolve(dir).toLowerCase() !== here).map(([, importPath]) => importPath);
    } catch (error) {
      process.stderr.write(`gotest: ${error.message}\n`); // the build below names the problem
    }

    const binary = path.join(work, IS_WINDOWS ? 'noctis.test.exe' : 'noctis.test');
    const buildArgs = ['test', '-c', '-o', binary, ...(options.race ? ['-race'] : []), ...(options.cover ? ['-cover'] : []), PACKAGE];
    const buildEnv = { ...process.env };
    setEnv(buildEnv, 'GOTMPDIR', work); // go's own work folder goes with ours, even when a Ctrl-C kills go
    const build = await start('go', buildArgs, { cwd: GO_DIR, env: buildEnv, stdio: ['ignore', 'inherit', 'inherit'] }).done;
    if (build.error || build.code !== 0 || !fs.existsSync(binary)) {
      console.log(`gotest: go ${buildArgs.join(' ')} failed${build.error ? `: ${build.error.message}` : ` (exit ${build.code})`}`);
      return 1;
    }
    const built = Date.now();

    const listEnv = testEnv(goroot, work);
    const list = (pattern) => {
      const listed = spawnSync(binary, [`-test.list=${pattern || '.'}`], { cwd: PACKAGE_DIR, env: listEnv, encoding: 'utf8', windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
      if (listed.error || listed.status !== 0) {
        throw new Error(`listing the tests with -test.list=${pattern || '.'} failed (${listed.error ? listed.error.message : `exit ${listed.status}`})\n${listed.stdout || ''}${listed.stderr || ''}`);
      }
      return new Set(listed.stdout.split(/\r?\n/).filter((name) => name && !name.startsWith('Benchmark')));
    };
    const everything = list('.');
    const selections = splitPattern(options.run).map(({ top, below }) => ({ below, names: top ? list(top) : everything }));
    const tests = [...everything].filter((name) => selections.some((selection) => selection.names.has(name)));
    if (!tests.length && !others.length) {
      console.log(options.run ? `gotest: no test matches --run ${options.run}` : 'gotest: no tests to run');
      return 0;
    }

    const durations = readDurations();
    const cpus = typeof os.availableParallelism === 'function' ? os.availableParallelism() : os.cpus().length;
    const count = Math.min(options.shards || defaultShards(cpus), Math.max(1, tests.length));
    const shards = assign(tests, count, durations);
    for (const shard of shards) {
      shard.tmp = path.join(work, `s${shard.index + 1}`);
      fs.mkdirSync(shard.tmp);
      shard.env = testEnv(goroot, shard.tmp);
      shard.chunks = patterns(shard.tests, selections).map((chunk, index) => ({ ...chunk, index }));
    }
    const unknown = tests.filter((name) => typeof durations[name] !== 'number').length;
    console.log(`gotest: built the test binary in ${seconds(built - began)}; ${plural(tests.length, 'test')} in ${plural(shards.length, 'shard')}${options.shards ? '' : ` (${cpus} CPUs)`}`
      + `${unknown ? ` (${unknown} not in ${path.basename(DURATIONS)}, counted as ${UNKNOWN_SECONDS} s each)` : ''}`
      + `${others.length ? `, and go test for ${others.join(' ')}` : ''}`);

    const failures = [];
    const measured = new Map();
    const profiles = [];
    let passed = 0;
    let skipped = 0;
    let failed = 0;
    let notRun = 0;
    const width = String(shards.length).length;

    const runShard = async (shard) => {
      const shardBegan = Date.now();
      const problems = [];
      const outputs = [];
      for (const chunk of shard.chunks) {
        const result = await runTests(binary, work, shard, chunk, options);
        if (interrupted) return;
        if (chunk.profile) profiles.push(chunk.profile);
        const expected = new Set(chunk.tests);
        const { results, started } = readResults(result.output, expected);
        const found = [];
        for (const name of chunk.tests) {
          const outcome = results.get(name);
          if (outcome) measured.set(name, outcome.seconds);
          if (outcome && outcome.status === 'PASS') passed += 1;
          else if (outcome && outcome.status === 'SKIP') skipped += 1;
          else if (outcome || started.has(name)) {
            failed += 1;
            found.push(outcome ? `${name} failed` : `${name} never finished (a panic, os.Exit or the timeout)`);
          } else {
            notRun += 1;
            found.push(`${name} did not run${result.code === 0 ? '' : ': the process ended before it'}`);
          }
        }
        const ok = result.code === 0 && !found.length;
        if (!ok && !found.length) found.push(`the test binary exited with ${result.code === null ? `signal ${result.signal}` : `code ${result.code}`}`);
        if (!ok || options.verbose) outputs.push({ chunk, output: result.output, ok });
        problems.push(...found);
      }
      const took = Date.now() - shardBegan;
      const status = problems.length ? `FAIL (${problems.length})` : 'ok';
      console.log(`  shard ${String(shard.index + 1).padStart(width)}/${shards.length}  ${plural(shard.tests.length, 'test').padStart(9)}  ${seconds(took).padStart(7)}  (estimated ${seconds(shard.estimate * 1000)})  ${status}`);
      if (problems.length || options.verbose) failures.push({ shard, problems, outputs });
    };

    const runOthers = async () => {
      if (!others.length) return;
      const logFile = path.join(work, 'others.log');
      const args = ['test', '-count=1', ...(options.race ? ['-race'] : []), ...(options.verbose ? ['-v'] : []), ...(options.run ? ['-run', options.run] : [])];
      if (options.cover) {
        const profile = path.join(work, 'others.cover');
        profiles.push(profile);
        args.push(`-coverprofile=${profile}`);
      }
      args.push(...others);
      const tmp = path.join(work, 'others');
      fs.mkdirSync(tmp);
      const env = { ...process.env };
      for (const name of ['TMPDIR', 'TEMP', 'TMP']) setEnv(env, name, tmp);
      setEnv(env, 'GOTMPDIR', work);
      const fd = fs.openSync(logFile, 'w');
      let started;
      try {
        started = start('go', args, { cwd: GO_DIR, env, stdio: ['ignore', fd, fd], detached: true });
      } finally {
        fs.closeSync(fd);
      }
      const result = await started.done;
      if (interrupted) return;
      const output = fs.readFileSync(logFile, 'utf8');
      if (result.code === 0 && !result.error) {
        process.stdout.write(options.verbose ? clean(output) : output.split(/\r?\n/).filter((line) => /^(ok|\?)\s/.test(line)).map((line) => `  ${line}\n`).join(''));
      } else {
        console.log(`  go ${args.join(' ')}  FAIL`);
        failures.push({ shard: null, problems: [`go test ${others.join(' ')} failed`], outputs: [{ chunk: null, output: output + (result.error ? `\n${result.error.message}\n` : ''), ok: false }] });
      }
    };

    await Promise.all([...shards.map(runShard), runOthers()]);
    if (interrupted) return 130;

    const problems = failures.flatMap((failure) => failure.problems.map((problem) => ({ shard: failure.shard, problem })));
    for (const failure of failures.sort((a, b) => (a.shard ? a.shard.index : Infinity) - (b.shard ? b.shard.index : Infinity))) {
      for (const { chunk, output, ok } of failure.outputs) {
        const name = failure.shard ? `shard ${failure.shard.index + 1}${failure.shard.chunks.length > 1 ? ` part ${chunk.index + 1}/${failure.shard.chunks.length}` : ''}` : 'go test for the other packages';
        process.stdout.write(`\n======== ${name}${ok ? '' : ' FAILED'} ========\n${clean(output)}`);
        if (chunk && !ok) process.stdout.write(`(run in ${PACKAGE_DIR} with -test.run=${chunk.pattern.length > 300 ? `${chunk.pattern.slice(0, 300)}...` : chunk.pattern})\n`);
      }
    }

    const slowest = [...measured].sort((a, b) => b[1] - a[1]).slice(0, 5);
    const testTime = [...measured.values()].reduce((sum, time) => sum + time, 0);
    if (slowest.length) {
      console.log(`slowest: ${slowest.map(([name, time]) => `${time.toFixed(2)} s ${name}`).join('\n         ')}`);
    }
    if (profiles.length) {
      const merged = mergeProfiles(profiles);
      console.log(`coverage: ${merged.percent.toFixed(1)}% of statements (all shards together)`);
      if (options.coverprofile) {
        const lines = [...merged.blocks].map(([key, value]) => `${key} ${value}`);
        fs.writeFileSync(options.coverprofile, `mode: ${merged.mode || 'set'}\n${lines.join('\n')}\n`);
      }
    }
    const summary = `${passed} passed, ${skipped} skipped, ${failed} failed${notRun ? `, ${notRun} not run` : ''} in ${seconds(Date.now() - began)}`
      + ` (build ${seconds(built - began)}, ${testTime.toFixed(1)} s of test time)`;
    if (problems.length) {
      console.log(`\nFAIL`);
      for (const { shard, problem } of problems) console.log(`  ${shard ? `shard ${shard.index + 1}: ` : ''}${problem}`);
      console.log(`gotest: ${summary}`);
      if (options.record) console.log(`gotest: not recording ${path.basename(DURATIONS)} after a failed run`);
      return 1;
    }
    console.log(`gotest: ${summary}`);
    if (options.record) writeDurations(measured, everything);
    return 0;
  } finally {
    if (!interrupted) removeQuietly(work);
  }
}

main().then((code) => {
  process.exitCode = code;
}, (error) => {
  process.stderr.write(`gotest: ${error.message}\n`);
  process.exitCode = 1;
});
