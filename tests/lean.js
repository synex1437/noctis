#!/usr/bin/env node
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawnSync } = require('child_process');

const ROOT = path.join(__dirname, '..');
const MINIMUM = [2, 1, 281];
const IS_WINDOWS = process.platform === 'win32';

function findClaude(searchPath = process.env.PATH || '') {
  const names = IS_WINDOWS ? ['claude.exe', 'claude.cmd', 'claude.bat'] : ['claude'];
  for (const dir of searchPath.split(path.delimiter).filter(Boolean)) {
    for (const name of names) {
      const candidate = path.join(dir, name);
      try {
        if (fs.statSync(candidate).isFile()) return candidate;
      } catch {
        continue;
      }
    }
  }
  return '';
}

function run(claude, args, env, cwd = ROOT) {
  const shell = IS_WINDOWS && /\.(cmd|bat)$/i.test(claude);
  const result = spawnSync(shell ? `"${claude}"` : claude, args, { cwd, env, encoding: 'utf8', timeout: 180000, shell });
  return { code: result.status, output: `${result.stdout || ''}${result.stderr || ''}`, error: result.error };
}

function parseVersion(text) {
  const found = /(\d+)\.(\d+)\.(\d+)/.exec(text || '');
  return found ? found.slice(1, 4).map(Number) : null;
}

function atLeast(version, minimum) {
  for (let index = 0; index < minimum.length; index += 1) {
    if (version[index] !== minimum[index]) return version[index] > minimum[index];
  }
  return true;
}

function isolatedEnv(configDir, extra = {}) {
  return {
    ...process.env,
    CLAUDE_CONFIG_DIR: configDir,
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
    DISABLE_TELEMETRY: '1',
    DISABLE_AUTOUPDATER: '1',
    DISABLE_ERROR_REPORTING: '1',
    ...extra,
  };
}

function leanChecks({ root = ROOT, searchPath } = {}) {
  const claude = findClaude(searchPath);
  if (!claude) return { skipped: 'no claude on PATH' };
  const configDir = fs.mkdtempSync(path.join(os.tmpdir(), 'noctis-lean-'));
  try {
    const versionRun = run(claude, ['--version'], isolatedEnv(configDir), root);
    const version = parseVersion(versionRun.output);
    if (!version) return { skipped: `${claude} --version printed no version: ${versionRun.output.trim().slice(0, 120)}` };
    if (!atLeast(version, MINIMUM)) return { skipped: `claude ${version.join('.')} is older than ${MINIMUM.join('.')}` };
    const checks = [];
    const plugin = run(claude, ['plugin', 'validate', path.join('.claude-plugin', 'plugin.json'), '--strict', '--json'], isolatedEnv(configDir), root);
    let report = null;
    try {
      report = JSON.parse(plugin.output.slice(plugin.output.indexOf('{')));
    } catch {
      report = null;
    }
    const hooksReport = (report && Array.isArray(report.contents) ? report.contents : []).find((entry) => /hooks\.json$/.test(entry.file || ''));
    const notes = hooksReport && Array.isArray(hooksReport.notes) ? hooksReport.notes.join('\n') : '';
    checks.push({ name: 'claude plugin validate --strict passes the plugin', ok: plugin.code === 0 && Boolean(report && report.success), detail: plugin.output.trim().slice(-600) });
    checks.push({ name: 'the validator reads the lean module and the three events it hooks', ok: /lean\.js hooks: session\.start, session\.compact, turn\.complete/.test(notes), detail: notes || plugin.output.trim().slice(-600) });
    const marketplace = run(claude, ['plugin', 'validate', '.', '--strict'], isolatedEnv(configDir), root);
    checks.push({ name: 'claude plugin validate --strict passes the marketplace', ok: marketplace.code === 0, detail: marketplace.output.trim().slice(-600) });
    const kit = run(claude, ['plugin', 'test', '.'], isolatedEnv(configDir, { CLAUDE_CODE_ENABLE_FUNCTION_HOOKS: '1' }), root);
    const passed = Number((/(\d+) pass/.exec(kit.output) || [])[1] || 0);
    const failed = Number((/(\d+) fail/.exec(kit.output) || [])[1] || 0);
    checks.push({ name: `claude plugin test passes the lean kit tests (${passed} passed, ${failed} failed)`, ok: kit.code === 0 && passed > 0 && failed === 0, detail: kit.output.trim().slice(-1500) });
    return { claude, version: version.join('.'), checks };
  } finally {
    fs.rmSync(configDir, { recursive: true, force: true });
  }
}

module.exports = { leanChecks, findClaude, parseVersion, atLeast, MINIMUM };

if (require.main === module) {
  const outcome = leanChecks();
  if (outcome.skipped) {
    process.stdout.write(`lean: skipped (${outcome.skipped})\n`);
  } else {
    for (const check of outcome.checks) {
      process.stdout.write(`  ${check.ok ? 'ok  ' : 'FAIL'} ${check.name}\n`);
      if (!check.ok) process.stdout.write(`${check.detail}\n`);
    }
    const failed = outcome.checks.filter((check) => !check.ok).length;
    process.stdout.write(`lean: ${outcome.checks.length - failed}/${outcome.checks.length} checks passed with claude ${outcome.version} (${outcome.claude})\n`);
    process.exitCode = failed ? 1 : 0;
  }
}
