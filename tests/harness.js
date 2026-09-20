'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const crypto = require('crypto');
const { spawn, spawnSync, fork } = require('child_process');

const PLUGIN_NAME = 'noctis';
const SOURCE_ROOT = path.resolve(__dirname, '..');

function sourceBinary() {
  const platform = `${process.platform === 'win32' ? 'windows' : process.platform}-${process.arch === 'x64' ? 'amd64' : process.arch}`;
  return path.join(SOURCE_ROOT, 'bin', platform, process.platform === 'win32' ? 'noctis.exe' : 'noctis');
}
const IS_WINDOWS = process.platform === 'win32';

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function nowSec() {
  return Math.floor(Date.now() / 1000);
}

function readJson(file) {
  try {
    return JSON.parse(fs.readFileSync(file, 'utf8'));
  } catch {
    return null;
  }
}

function sleepSync(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

function writeJson(file, data) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const staging = `${file}.${process.pid}.labtmp`;
  fs.writeFileSync(staging, JSON.stringify(data, null, 2));
  for (let attempt = 0; ; attempt++) {
    try {
      fs.renameSync(staging, file);
      return;
    } catch (error) {
      if (attempt === 11 || !['EPERM', 'EACCES', 'EBUSY'].includes(error.code)) throw error;
      sleepSync(10 + attempt * 10);
    }
  }
}

function isAlive(pid) {
  if (!pid) return false;
  if (process.platform === 'linux') {
    try {
      return !/\sZ\s/.test(fs.readFileSync(`/proc/${pid}/stat`, 'utf8'));
    } catch {
      return false;
    }
  }
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function refreshChecksums() {
  const binDir = path.join(SOURCE_ROOT, 'bin');
  const lines = [];
  const walk = (dir, prefix) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (entry.isDirectory()) walk(path.join(dir, entry.name), relative);
      else if (entry.name.startsWith('noctis')) lines.push(`${crypto.createHash('sha256').update(fs.readFileSync(path.join(dir, entry.name))).digest('hex')}  ${relative}`);
    }
  };
  walk(binDir, '');
  fs.writeFileSync(path.join(binDir, 'SHA256SUMS'), `${lines.join('\n')}\n`);
}

function canonicalTmpdir() {
  try {
    return fs.realpathSync.native(os.tmpdir());
  } catch {
    return os.tmpdir();
  }
}

class Lab {
  constructor(name) {
    this.root = path.join(canonicalTmpdir(), `${name}-${process.pid}`);
    this.binDir = path.join(this.root, 'bin');
    this.projectDir = path.join(this.root, 'project');
    this.callsFile = path.join(this.root, 'claude-calls.log');
    this.transcript = path.join(this.projectDir, 'session.jsonl');
    this.mockDir = path.join(this.root, 'mock');
    this.mockPort = 0;
    this.mockProcess = null;
    fs.rmSync(this.root, { recursive: true, force: true });
    fs.mkdirSync(this.root, { recursive: true });
    this.snapshotSource();
    this.writeFakeClaude();
    this.writeFakeGh();
    this.writeFakeHosts();
    this.writeTranscript();
  }

  snapshotSource() {
    this.sourceRoot = path.join(this.root, 'source');
    const platform = `${process.platform === 'win32' ? 'windows' : process.platform}-${process.arch === 'x64' ? 'amd64' : process.arch}`;
    const binary = process.platform === 'win32' ? 'noctis.exe' : 'noctis';
    const wanted = [
      path.join('bin', platform, binary),
      path.join('bin', 'SHA256SUMS'),
      path.join('bin', 'noctis'),
      path.join('hooks', 'hooks.json'),
      path.join('scripts', 'notify.ps1'),
      path.join('scripts', 'launch.ps1'),
      'config.default.json',
      path.join('.claude-plugin', 'plugin.json'),
      'LICENSE',
      'README.md',
    ];
    for (const relative of wanted) {
      const from = path.join(SOURCE_ROOT, relative);
      if (!fs.existsSync(from)) continue;
      const to = path.join(this.sourceRoot, relative);
      fs.mkdirSync(path.dirname(to), { recursive: true });
      fs.copyFileSync(from, to);
      fs.chmodSync(to, fs.statSync(from).mode);
    }
    for (const dir of ['agents', 'skills']) {
      const from = path.join(SOURCE_ROOT, dir);
      if (fs.existsSync(from)) fs.cpSync(from, path.join(this.sourceRoot, dir), { recursive: true });
    }
    this.snapshotBinary = path.join(this.sourceRoot, 'bin', platform, binary);
  }

  writeFakeGh() {
    this.ghLog = path.join(this.root, 'gh-calls.log');
    if (IS_WINDOWS) {
      fs.writeFileSync(path.join(this.binDir, 'gh.cmd'), [
        '@echo off',
        'if "%~1"=="issue" if "%~2"=="list" (type "%NOCTIS_LAB_GH_ISSUES%" & exit /b 0)',
        'echo GH args=[%*]>> "%NOCTIS_LAB_GH_LOG%"',
        '',
      ].join('\r\n'));
      return;
    }
    const script = path.join(this.binDir, 'gh');
    fs.writeFileSync(script, [
      '#!/usr/bin/env sh',
      'if [ "$1" = "issue" ] && [ "$2" = "list" ]; then cat "$NOCTIS_LAB_GH_ISSUES"; exit 0; fi',
      'echo "GH args=[$*]" >> "$NOCTIS_LAB_GH_LOG"',
      'exit 0',
      '',
    ].join('\n'));
    fs.chmodSync(script, 0o755);
  }

  writeFakeHosts() {
    if (IS_WINDOWS) {
      for (const name of ['codex', 'agy', 'droid', 'copilot']) {
        fs.writeFileSync(path.join(this.binDir, `${name}.cmd`), [
          '@echo off',
          `echo FAKE_${name.toUpperCase()} args=[%*] HANDOFF=%NOCTIS_HANDOFF% HOST=%NOCTIS_HOST%>> "%NOCTIS_LAB_CALLS%"`,
          '',
        ].join('\r\n'));
      }
      return;
    }
    for (const name of ['codex', 'agy', 'droid', 'copilot']) {
      const script = path.join(this.binDir, name);
      const lines = ['#!/usr/bin/env sh'];
      if (name === 'codex') {
        lines.push(
          'if [ "$1" = "app-server" ]; then',
          '  while IFS= read -r line; do',
          '    case "$line" in',
          "      *'\"method\":\"initialize\"'*) echo '{\"id\":0,\"result\":{\"userAgent\":\"fake\",\"platformFamily\":\"unix\",\"platformOs\":\"linux\"}}' ;;",
          "      *'account/rateLimits/read'*) cat \"$NOCTIS_LAB_CODEX_LIMITS\"; echo; exit 0 ;;",
          '    esac',
          '  done',
          '  exit 0',
          'fi',
        );
      }
      lines.push(`echo "FAKE_${name.toUpperCase()} args=[$*] HANDOFF=$NOCTIS_HANDOFF HOST=$NOCTIS_HOST" >> "$NOCTIS_LAB_CALLS"`, 'exit 0', '');
      fs.writeFileSync(script, lines.join('\n'));
      fs.chmodSync(script, 0o755);
    }
  }

  ghCalls() {
    return fs.existsSync(this.ghLog) ? fs.readFileSync(this.ghLog, 'utf8').split('\n').filter(Boolean) : [];
  }

  writeFakeClaude() {
    fs.mkdirSync(this.binDir, { recursive: true });
    if (IS_WINDOWS) {
      fs.writeFileSync(path.join(this.binDir, 'claude.cmd'), [
        '@echo off',
        'if "%~1"=="--help" (echo   --permission-mode ^<mode^>  (choices: "acceptEdits", "bypassPermissions", "default", "plan", "auto")& exit /b 0)',
        'echo FAKE_CLAUDE args=[%*] HANDOFF=%NOCTIS_HANDOFF% CONFIG=%CLAUDE_CONFIG_DIR% EFFORT=%CLAUDE_CODE_EFFORT_LEVEL%>> "%NOCTIS_LAB_CALLS%"',
        'if "%NOCTIS_LAB_FAST%"=="" ping -n 2 127.0.0.1 >nul',
        'exit /b 0',
        '',
      ].join('\r\n'));
      return;
    }
    const script = path.join(this.binDir, 'claude');
    fs.writeFileSync(script, [
      '#!/usr/bin/env sh',
      'if [ "$1" = "--help" ]; then printf \'  --permission-mode <mode>  (choices: "acceptEdits", "bypassPermissions", "default", "plan", "auto")\\n\'; exit 0; fi',
      'echo "FAKE_CLAUDE args=[$*] HANDOFF=$NOCTIS_HANDOFF CONFIG=$CLAUDE_CONFIG_DIR EFFORT=$CLAUDE_CODE_EFFORT_LEVEL" >> "$NOCTIS_LAB_CALLS"',
      '[ -z "$NOCTIS_LAB_FAST" ] && sleep 1',
      'exit 0',
      '',
    ].join('\n'));
    fs.chmodSync(script, 0o755);
  }

  writeTranscript(extraBytes = 0, file = this.transcript) {
    const lines = [
      JSON.stringify({ type: 'user', message: { role: 'user', content: 'Refactor the auth module and add tests' } }),
      JSON.stringify({ type: 'assistant', message: { role: 'assistant', content: [{ type: 'text', text: 'Starting with the token parser.' }, { type: 'tool_use', name: 'Edit', input: { file_path: path.join(this.projectDir, 'src', 'auth.js') } }, { type: 'tool_use', name: 'Bash', input: { command: 'npm test' } }] } }),
      JSON.stringify({ type: 'user', message: { role: 'user', content: [{ type: 'tool_result', content: 'ok' }] } }),
      JSON.stringify({ type: 'assistant', message: { role: 'assistant', content: [{ type: 'tool_use', name: 'TodoWrite', input: { todos: [{ content: 'parser', status: 'completed' }, { content: 'tests', status: 'pending' }] } }] } }),
    ];
    const filler = extraBytes ? `${JSON.stringify({ type: 'assistant', message: { role: 'assistant', content: [{ type: 'text', text: 'x'.repeat(2000) }] } })}\n`.repeat(Math.ceil(extraBytes / 2100)) : '';
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, `${filler}${lines.join('\n')}\n`);
  }

  async startMock() {
    fs.mkdirSync(this.mockDir, { recursive: true });
    this.setLimits([]);
    this.mockProcess = fork(path.join(__dirname, 'mock-usage-server.js'), [this.mockDir], { stdio: 'ignore' });
    const portFile = path.join(this.mockDir, 'port');
    const readPort = () => {
      try {
        return Number(fs.readFileSync(portFile, 'utf8').trim());
      } catch {
        return 0;
      }
    };
    for (let i = 0; i < 600 && !readPort(); i += 1) {
      if (this.mockProcess.exitCode !== null) throw new Error(`mock server exited with ${this.mockProcess.exitCode} before it was listening`);
      await sleep(50);
    }
    this.mockPort = readPort();
    if (!this.mockPort) throw new Error('mock server did not report a port within 30 s');
  }

  stopMock() {
    if (this.mockProcess) this.mockProcess.kill();
  }

  setLimits(limits) {
    writeJson(path.join(this.mockDir, 'limits.json'), limits);
  }

  getLimits() {
    return readJson(path.join(this.mockDir, 'limits.json')) || [];
  }

  webhooks() {
    try {
      return fs.readFileSync(path.join(this.mockDir, 'webhooks.log'), 'utf8').split('\n').filter(Boolean).map((line) => JSON.parse(line));
    } catch {
      return [];
    }
  }

  setOutage(mode) {
    writeJson(path.join(this.mockDir, 'outage.json'), { mode });
  }

  setSkew(seconds) {
    writeJson(path.join(this.mockDir, 'skew.json'), { seconds });
  }

  mockHits() {
    try {
      return fs.readFileSync(path.join(this.mockDir, 'hits.log'), 'utf8').split('\n').filter(Boolean).length;
    } catch {
      return 0;
    }
  }

  mockLastHeaders() {
    try {
      const lines = fs.readFileSync(path.join(this.mockDir, 'hits.log'), 'utf8').split('\n').filter(Boolean);
      return lines.length ? JSON.parse(lines[lines.length - 1]) : null;
    } catch {
      return null;
    }
  }

  calls() {
    try {
      return fs.readFileSync(this.callsFile, 'utf8').split(/\r?\n/).filter(Boolean);
    } catch {
      return [];
    }
  }

  resetCalls() {
    fs.rmSync(this.callsFile, { force: true });
  }

  account(name) {
    return new Account(this, name);
  }
}

class Account {
  constructor(lab, name) {
    this.lab = lab;
    this.name = name;
    this.dir = path.join(lab.root, name);
    fs.mkdirSync(this.dir, { recursive: true });
    this.guardDir = path.join(this.dir, PLUGIN_NAME);
    this.guard = path.join(this.dir, 'skills', PLUGIN_NAME, 'bin', 'noctis');
    this.stateFile = path.join(this.guardDir, 'state.json');
    this.configFile = path.join(this.guardDir, 'config.json');
    this.timeOffset = 0;
    this.manualSchedule = false;
    this.fastClaude = false;
    this.token = 'lab-token';
  }

  useOwnToken() {
    this.token = `lab-token-${this.name}`;
    writeJson(path.join(this.dir, '.credentials.json'), { claudeAiOauth: { accessToken: this.token, expiresAt: Date.now() + 30 * 86400000 } });
  }

  setOwnLimits(limits) {
    writeJson(path.join(this.lab.mockDir, `limits-${this.token.replace(/[^A-Za-z0-9-]/g, '_')}.json`), limits);
  }

  env(extra = {}) {
    const inherited = { ...process.env };
    delete inherited.LC_ALL;
    delete inherited.LC_MESSAGES;
    const env = {
      ...inherited,
      PATH: `${this.lab.binDir}${path.delimiter}${process.env.PATH}`,
      CLAUDE_CONFIG_DIR: this.dir,
      NOCTIS_LAB_GH_LOG: this.lab.ghLog,
      NOCTIS_LAB_GH_ISSUES: path.join(this.lab.root, 'gh-issues.json'),
      NOCTIS_LAB_CODEX_LIMITS: path.join(this.lab.root, 'codex-limits.json'),
      NOCTIS_UPDATE_URL: 'off',
      NOCTIS_ANTIGRAVITY_HOOKS: path.join(this.lab.root, 'agy-hooks.json'),
      NOCTIS_USAGE_URL: `http://127.0.0.1:${this.lab.mockPort}/api/oauth/usage`,
      NOCTIS_NO_TASKS: '1',
      NOCTIS_NO_EARLY_TRIGGER: '1',
      NOCTIS_NO_TERMINAL: '1', 
      NOCTIS_LAB_CALLS: this.lab.callsFile,
      NOCTIS_PLUGIN_ROOT: path.dirname(path.dirname(this.guard)),
      NOCTIS_LANG: 'tr',
      ...extra,
    };
    if (this.timeOffset) env.NOCTIS_TIME_OFFSET = String(this.timeOffset);
    if (this.manualSchedule) env.NOCTIS_NO_SCHEDULE = '1';
    if (this.fastClaude) env.NOCTIS_LAB_FAST = '1';
    return env;
  }

  install(mutateConfig) {
    const result = spawnSync(process.env.NOCTIS_BINARY || this.lab.snapshotBinary || sourceBinary(), ['install', '--source', this.lab.sourceRoot || SOURCE_ROOT, '--config-dir', this.dir], { encoding: 'utf8', env: this.env() });
    if (result.status !== 0) throw new Error(`install failed: ${result.stderr}`);
    writeJson(path.join(this.dir, '.credentials.json'), { claudeAiOauth: { accessToken: this.token, expiresAt: Date.now() + 30 * 86400000 } });
    this.setConfig((config) => {
      config.wait.resetMarginSeconds = 1;
      config.wait.builtinGraceSeconds = 2;
      config.wait.heartbeatGraceSeconds = 1;
      config.usage.blindProbeSeconds = 1;
      config.usage.blindProbeRounds = 2;
      config.resume.mode = 'headless';
      config.wake.sameSession = false;
      config.usage.multiSessionMax = false;
      if (mutateConfig) mutateConfig(config);
    });
  }

  engine() {
    return [process.env.NOCTIS_BINARY || this.lab.snapshotBinary || sourceBinary(), []];
  }

  run(args, input, extraEnv = {}) {
    const [binary, prefix] = this.engine();
    const result = spawnSync(binary, [...prefix, ...args], { encoding: 'utf8', input: input ? JSON.stringify(input) : undefined, env: this.env(extraEnv), timeout: 120000 });
    if (result.error) throw result.error;
    return result.stdout.trim();
  }

  hook(input, extraEnv = {}) {
    return this.run(['hook'], input, extraEnv);
  }

  runPromise(args, input, extraEnv = {}) {
    const [binary, prefix] = this.engine();
    return new Promise((resolve, reject) => {
      const child = spawn(binary, [...prefix, ...args], { env: this.env(extraEnv), stdio: ['pipe', 'pipe', 'ignore'] });
      let stdout = '';
      child.stdout.on('data', (chunk) => {
        stdout += chunk;
      });
      child.on('error', reject);
      child.on('close', () => resolve(stdout.trim()));
      child.stdin.end(input ? JSON.stringify(input) : '');
    });
  }

  runFull(args, input, extraEnv = {}) {
    const [binary, prefix] = this.engine();
    const result = spawnSync(binary, [...prefix, ...args], { encoding: 'utf8', input: input ? JSON.stringify(input) : undefined, env: this.env(extraEnv), timeout: 120000 });
    if (result.error) throw result.error;
    return { status: result.status, stdout: result.stdout.trim(), stderr: result.stderr.trim() };
  }

  hookAsync(input, extraEnv = {}) {
    const [binary, prefix] = this.engine();
    const child = spawn(binary, [...prefix, 'hook'], { env: this.env(extraEnv), stdio: ['pipe', 'pipe', 'ignore'] });
    child.stdin.end(JSON.stringify(input));
    return child;
  }

  hookPromise(input, extraEnv = {}) {
    return new Promise((resolve, reject) => {
      const child = this.hookAsync(input, extraEnv);
      let stdout = '';
      child.stdout.on('data', (chunk) => {
        stdout += chunk;
      });
      child.on('error', reject);
      child.on('close', () => resolve(stdout.trim()));
    });
  }

  statuslineInput(sid, model, fiveUsed, fiveReset, weekUsed, weekReset, contextPercent = 32) {
    return {
      session_id: sid,
      cwd: this.lab.projectDir,
      transcript_path: this.lab.transcript,
      version: '2.1.270',
      model: { id: model, display_name: model },
      effort: { level: 'max' },
      context_window: { used_percentage: contextPercent },
      rate_limits: { five_hour: { used_percentage: fiveUsed, resets_at: fiveReset }, seven_day: { used_percentage: weekUsed, resets_at: weekReset } },
    };
  }

  statusline(sid, model, fiveUsed, fiveReset, weekUsed, weekReset, contextPercent = 32) {
    return this.run(['statusline'], this.statuslineInput(sid, model, fiveUsed, fiveReset, weekUsed, weekReset, contextPercent));
  }

  state() {
    return readJson(this.stateFile) || {};
  }

  settingsModel() {
    return (readJson(path.join(this.dir, 'settings.json')) || {}).model;
  }

  setConfig(mutator) {
    const config = readJson(this.configFile);
    mutator(config);
    writeJson(this.configFile, config);
  }
}

const RESERVED_KEYS = new Set(['__proto__', 'constructor', 'prototype']);
function waitKey(sid) {
  const raw = sid === undefined || sid === null || sid === '' ? 'unknown' : String(sid);
  const key = Buffer.from(raw.replace(/[^A-Za-z0-9_.-]/gu, '_')).subarray(0, 80).toString();
  return RESERVED_KEYS.has(key) ? 'unknown' : key;
}

module.exports = {
  refreshChecksums, PLUGIN_NAME, SOURCE_ROOT, IS_WINDOWS, Lab, Account, sleep, nowSec, readJson, writeJson, isAlive, waitKey };
