#!/usr/bin/env node
'use strict';

// Builds bin/ from go/ exactly as CI checks it, so `git diff -- bin/` afterwards shows only what the
// source changed: Linux and Windows get one binary per CPU, macOS one universal binary holding its
// x86_64 and arm64 builds, bin/noctis.exe is the windows-amd64 build, and bin/SHA256SUMS is
// regenerated with the code the suites and CI use. --host builds only this machine's binary (on
// macOS a single-CPU bin/darwin/noctis); --go <path> picks the Go toolchain.

const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawn, spawnSync } = require('child_process');
const { refreshChecksums } = require('../tests/harness.js');

const ROOT = path.dirname(__dirname);
const BIN = path.join(ROOT, 'bin');
const UNIVERSAL = path.join(BIN, 'darwin', 'noctis');
const TARGETS = ['windows/amd64', 'windows/arm64', 'darwin/amd64', 'darwin/arm64', 'linux/amd64', 'linux/arm64'];

// A universal (fat) Mach-O file is a big-endian header listing each CPU's slice, followed by the
// slices: each CPU's own Mach-O image, byte for byte, at an offset aligned to 2^ALIGN (16 KB, the
// page size of Apple silicon) with zeros in between. x86_64 comes first and arm64 second, as lipo
// writes them. Leaving the images untouched keeps the ad hoc signature Go's linker gives the arm64
// build valid.
const FAT_MAGIC = 0xcafebabe;
const MH_MAGIC_64 = 0xfeedfacf;
const MH_EXECUTE = 2;
const ALIGN = 14;
const SLICES = [
  { goarch: 'amd64', name: 'x86_64', cputype: 0x01000007, subtype: 3 },
  { goarch: 'arm64', name: 'arm64', cputype: 0x0100000c, subtype: 0 },
];

function parseArgs(argv) {
  const options = { host: false, go: 'go' };
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] === '--host') options.host = true;
    else if (argv[i] === '--go' && argv[i + 1]) options.go = argv[(i += 1)];
    else {
      process.stderr.write('usage: node scripts/build.js [--host] [--go <path to go>]\n');
      process.exit(2);
    }
  }
  return options;
}

function goVersion(go) {
  const result = spawnSync(go, ['env', 'GOVERSION'], { cwd: path.join(ROOT, 'go'), encoding: 'utf8' });
  if (result.error || result.status !== 0) {
    throw new Error(`${go} env GOVERSION failed (${result.error ? result.error.message : (result.stderr || '').trim()}); install Go or pass --go <path>`);
  }
  return result.stdout.trim();
}

// Settings that change the code Go writes stay at Go's defaults, as they are on CI.
const CODEGEN_SETTINGS = ['GOFLAGS', 'GOAMD64', 'GOARM64', 'GOEXPERIMENT'];

function build(go, target, output) {
  const [goos, goarch] = target.split('/');
  fs.mkdirSync(path.dirname(output), { recursive: true });
  const env = { ...process.env, CGO_ENABLED: '0', GOOS: goos, GOARCH: goarch };
  for (const name of CODEGEN_SETTINGS) delete env[name];
  const started = Date.now();
  return new Promise((resolve, reject) => {
    const child = spawn(go, ['build', '-trimpath', '-buildvcs=false', '-ldflags=-s -w', '-o', output, './cmd/noctis'], {
      cwd: path.join(ROOT, 'go'),
      env,
      stdio: ['ignore', 'ignore', 'pipe'],
    });
    let stderr = '';
    child.stderr.on('data', (chunk) => {
      stderr += chunk;
    });
    child.on('error', (error) => reject(new Error(`${target}: ${error.message}`)));
    child.on('close', (code) => {
      if (code !== 0) reject(new Error(`${target}: go build exited with ${code}\n${stderr.trim()}`));
      else resolve(Date.now() - started);
    });
  });
}

function binaryOf(target) {
  const [goos, goarch] = target.split('/');
  if (goos === 'darwin') return UNIVERSAL;
  return path.join(BIN, `${goos}-${goarch}`, goos === 'windows' ? 'noctis.exe' : 'noctis');
}

function hostTarget() {
  const goos = { darwin: 'darwin', linux: 'linux', win32: 'windows' }[process.platform];
  const goarch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
  if (!goos || !goarch) throw new Error(`noctis is not built for ${process.platform}/${process.arch}`);
  return `${goos}/${goarch}`;
}

function thinImage(file, slice) {
  const data = fs.readFileSync(file);
  const word = (at) => (data.length >= at + 4 ? data.readUInt32LE(at) : -1);
  const found = [
    [word(0) === 0xfeedface, 'is a 32-bit Mach-O image'],
    [word(0) === 0xbebafeca, 'is a universal binary already'],
    [word(0) !== MH_MAGIC_64 || data.length < 32, 'is not a 64-bit Mach-O image'],
    [word(4) !== slice.cputype, `is built for CPU type 0x${word(4).toString(16)}, not ${slice.name}`],
    [(word(8) & 0x00ffffff) !== slice.subtype, `has CPU subtype 0x${word(8).toString(16)}, not the one every ${slice.name} runs`],
    [word(12) !== MH_EXECUTE, 'is not an executable'],
    [32 + word(20) > data.length, 'has load commands that run past its end'],
  ].find(([wrong]) => wrong);
  if (found) throw new Error(`${file} ${found[1]}`);
  return { ...slice, cpusubtype: word(8), data };
}

function universalBinary(images) {
  const header = Buffer.alloc(8 + 20 * images.length);
  header.writeUInt32BE(FAT_MAGIC, 0);
  header.writeUInt32BE(images.length, 4);
  const parts = [header];
  let end = header.length;
  images.forEach((image, index) => {
    const offset = Math.ceil(end / 2 ** ALIGN) * 2 ** ALIGN;
    const entry = 8 + 20 * index;
    header.writeUInt32BE(image.cputype, entry);
    header.writeUInt32BE(image.cpusubtype, entry + 4);
    header.writeUInt32BE(offset, entry + 8);
    header.writeUInt32BE(image.data.length, entry + 12);
    header.writeUInt32BE(ALIGN, entry + 16);
    parts.push(Buffer.alloc(offset - end), image.data);
    end = offset + image.data.length;
  });
  return Buffer.concat(parts, end);
}

function checkUniversal(file, images) {
  const data = fs.readFileSync(file);
  const fail = (problem) => {
    throw new Error(`${file} was written wrong: it ${problem}`);
  };
  if (data.length < 8 || data.readUInt32BE(0) !== FAT_MAGIC) fail('does not start with the universal binary magic');
  if (data.readUInt32BE(4) !== images.length) fail(`lists ${data.readUInt32BE(4)} slices`);
  let end = 8 + 20 * images.length;
  const layout = images.map((image, index) => {
    const [cputype, cpusubtype, offset, size, align] = [0, 4, 8, 12, 16].map((field) => data.readUInt32BE(8 + 20 * index + field));
    if (cputype !== image.cputype || cpusubtype !== image.cpusubtype) fail(`lists CPU 0x${cputype.toString(16)}/0x${cpusubtype.toString(16)} where ${image.name} belongs`);
    if (align !== ALIGN || offset % 2 ** ALIGN !== 0 || offset < end) fail(`puts ${image.name} at offset ${offset} aligned to 2^${align}`);
    if (data.subarray(end, offset).some((byte) => byte !== 0)) fail(`pads ${image.name} with bytes that are not zero`);
    if (size !== image.data.length || !data.subarray(offset, offset + size).equals(image.data)) fail(`does not hold the ${image.name} build byte for byte`);
    end = offset + size;
    return `${image.name} at ${offset}`;
  });
  if (end !== data.length) fail(`has ${data.length - end} bytes after its last slice`);
  return layout;
}

// Other programs that read universal binaries, where this machine has them. On macOS lipo is part
// of the developer tools, and without them /usr/bin/lipo only offers to install them.
function crossCheck(file) {
  const readers = [];
  const lipos = process.platform === 'darwin' && spawnSync('xcode-select', ['-p']).status !== 0 ? ['llvm-lipo'] : ['lipo', 'llvm-lipo'];
  for (const lipo of lipos) {
    const result = spawnSync(lipo, ['-archs', file], { encoding: 'utf8' });
    if (result.error) continue;
    const archs = (result.stdout || '').trim().split(/\s+/).sort().join(' ');
    if (result.status !== 0 || archs !== 'arm64 x86_64') {
      throw new Error(`${lipo} -archs ${file} answers "${`${result.stdout}${result.stderr}`.trim()}", not x86_64 and arm64`);
    }
    readers.push(lipo);
  }
  const described = spawnSync('file', ['-b', file], { encoding: 'utf8' });
  if (!described.error) {
    const text = (described.stdout || '').trim();
    if (described.status !== 0 || !/universal binary with 2 architectures/.test(text) || !/x86_64/.test(text) || !/arm64/.test(text)) {
      throw new Error(`file ${file} answers "${text}", not a universal binary for x86_64 and arm64`);
    }
    readers.push('file');
  }
  return readers;
}

// Writes bin/darwin/noctis through a staging file, so it is never left half written. go build is not
// pointed at it: where go copies its output (on Windows, into a setgid folder) it refuses to
// overwrite a file it does not take for a build of its own, and a universal binary is not one.
function placeDarwin(data) {
  fs.mkdirSync(path.dirname(UNIVERSAL), { recursive: true });
  const staging = path.join(path.dirname(UNIVERSAL), `.noctis-${process.pid}.tmp`);
  try {
    fs.writeFileSync(staging, data);
    fs.chmodSync(staging, 0o755);
    fs.renameSync(staging, UNIVERSAL);
  } finally {
    fs.rmSync(staging, { force: true });
  }
}

function darwinOutput(work, target) {
  return path.join(work, target.replace('/', '-'), 'noctis');
}

function reportBuilt(target, ms) {
  process.stdout.write(`  ${target.padEnd(14)} ${(ms / 1000).toFixed(1)} s\n`);
}

async function buildAll(go) {
  const work = fs.mkdtempSync(path.join(os.tmpdir(), 'noctis-build-'));
  try {
    const outputs = new Map(TARGETS.map((target) => [target, target.startsWith('darwin/') ? darwinOutput(work, target) : binaryOf(target)]));
    const results = await Promise.allSettled(TARGETS.map((target) => build(go, target, outputs.get(target)).then((ms) => reportBuilt(target, ms))));
    const failed = results.filter((result) => result.status === 'rejected').map((result) => result.reason.message);
    if (failed.length) throw new Error(failed.join('\n'));

    const images = SLICES.map((slice) => thinImage(outputs.get(`darwin/${slice.goarch}`), slice));
    placeDarwin(universalBinary(images));
    const layout = checkUniversal(UNIVERSAL, images);
    const readers = crossCheck(UNIVERSAL);
    process.stdout.write(`bin/darwin/noctis: universal, ${layout.join(', ')}; ${readers.length ? `${readers.join(' and ')} agree` : 'no lipo or file here to cross-check it'}\n`);
  } finally {
    fs.rmSync(work, { recursive: true, force: true });
  }
  // The layout before macOS had one universal binary; left behind, it would enter SHA256SUMS.
  for (const old of ['darwin-amd64', 'darwin-arm64']) fs.rmSync(path.join(BIN, old), { recursive: true, force: true });
  return TARGETS;
}

async function buildHost(go) {
  const target = hostTarget();
  if (!target.startsWith('darwin/')) {
    reportBuilt(target, await build(go, target, binaryOf(target)));
    return [target];
  }
  const work = fs.mkdtempSync(path.join(os.tmpdir(), 'noctis-build-'));
  try {
    reportBuilt(target, await build(go, target, darwinOutput(work, target)));
    placeDarwin(thinImage(darwinOutput(work, target), SLICES.find((slice) => target.endsWith(slice.goarch))).data);
  } finally {
    fs.rmSync(work, { recursive: true, force: true });
  }
  process.stdout.write('bin/darwin/noctis: this Mac\'s build alone, for local runs; node scripts/build.js makes the universal binary to commit\n');
  return [target];
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const started = Date.now();
  const version = goVersion(options.go);
  const built = await (options.host ? buildHost(options.go) : buildAll(options.go));
  if (built.includes('windows/amd64')) fs.copyFileSync(binaryOf('windows/amd64'), path.join(BIN, 'noctis.exe'));
  refreshChecksums(ROOT);
  process.stdout.write(`bin/: ${built.length} target${built.length === 1 ? '' : 's'} built with ${version} in ${((Date.now() - started) / 1000).toFixed(1)} s, SHA256SUMS regenerated\n`);
}

main().catch((error) => {
  process.stderr.write(`build: ${error.message}\n`);
  process.exit(1);
});
