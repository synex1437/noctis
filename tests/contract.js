#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const { Lab } = require('./harness.js');

const ROOT = path.dirname(__dirname);

function pluginRootOf(account) {
  return path.dirname(path.dirname(account.guard));
}
const MANIFEST = path.join(ROOT, 'hooks', 'hooks.json');

let checks = 0;
const failures = [];

function check(label, condition, detail) {
  checks += 1;
  if (!condition) failures.push(detail ? `${label}\n      ${detail}` : label);
}

function payloadFor(event, sid, lab, account) {
  const base = {
    session_id: sid,
    transcript_path: path.join(account.dir, 'transcript.jsonl'),
    cwd: lab.projectDir,
    hook_event_name: event,
  };
  switch (event) {
    case 'SessionStart':
      return { ...base, source: 'startup' };
    case 'SessionEnd':
      return { ...base, reason: 'clear' };
    case 'UserPromptSubmit':
      return { ...base, prompt: 'add a test for the parser' };
    case 'PreToolUse':
      return { ...base, tool_name: 'Write', tool_input: { file_path: 'a.txt', content: 'x' } };
    case 'PermissionRequest':
      return { ...base, permission_mode: 'default', tool_name: 'Read', tool_input: { file_path: path.join(lab.projectDir, 'README.md') }, permission_suggestions: [] };
    case 'PostToolUse':
      return { ...base, tool_name: 'Task', tool_input: { description: 'research' }, tool_response: { ok: true } };
    case 'PostToolBatch':
      return { ...base, tools: [{ tool_name: 'Read' }, { tool_name: 'Edit' }] };
    case 'Stop':
      return { ...base, stop_hook_active: false };
    case 'StopFailure':
      return { ...base, reason: 'rate_limit', error: { type: 'rate_limit_error', message: '5-hour limit reached' } };
    case 'Notification':
      return { ...base, notification_type: 'quota_auto_resume_fired', message: 'resumed' };
    case 'PostModelSwitch':
      return { ...base, from_model: 'claude-fable-5-1', to_model: 'claude-opus-5' };
    case 'TaskCreated':
      return { ...base, task: { id: 't1', subject: 'write the parser' } };
    case 'TaskCompleted':
      return { ...base, task: { id: 't1', subject: 'write the parser', status: 'completed' } };
    default:
      return base;
  }
}

function limitsAt(sessionPercent, weeklyPercent) {
  const now = Math.floor(Date.now() / 1000);
  return [
    { kind: 'session', percent: sessionPercent, resets_at: new Date((now + 1800) * 1000).toISOString() },
    { kind: 'weekly_all', percent: weeklyPercent, resets_at: new Date((now + 3 * 86400) * 1000).toISOString() },
  ];
}

function manifestEntries() {
  const manifest = JSON.parse(fs.readFileSync(MANIFEST, 'utf8'));
  const entries = [];
  for (const [event, groups] of Object.entries(manifest.hooks)) {
    for (const group of groups) {
      for (const hook of group.hooks) {
        entries.push({ event, matcher: group.matcher, ...hook });
      }
    }
  }
  return entries;
}

function checkManifestShape(entries, binaryHandlers) {
  for (const entry of entries) {
    const where = `${entry.event} (args ${JSON.stringify(entry.args)})`;
    check(`${where}: type is command`, entry.type === 'command');
    check(`${where}: command uses \${CLAUDE_PLUGIN_ROOT}`, /^\$\{CLAUDE_PLUGIN_ROOT\}\//.test(entry.command),
      `command was ${entry.command}`);
    check(`${where}: args is a non-empty array`, Array.isArray(entry.args) && entry.args.length > 0);
    check(`${where}: timeout is a positive number`, typeof entry.timeout === 'number' && entry.timeout > 0);
    if (entry.matcher) {
      let valid = true;
      try { new RegExp(`^(${entry.matcher})$`); } catch { valid = false; }
      check(`${where}: matcher is a valid regex`, valid, entry.matcher);
    }
    if (entry.args[0] === 'hook') {
      check(`${where}: binary dispatches this event`, binaryHandlers.has(entry.event),
        `${entry.event} is not in the handler table`);
    }
  }
}

function agentDisallowedTools(file) {
  const text = fs.readFileSync(path.join(ROOT, 'agents', file), 'utf8');
  const front = text.startsWith('---\n') ? text.slice(4, text.indexOf('\n---', 4)) : '';
  const line = /^disallowedTools:(.*)$/m.exec(front);
  return new Set(line ? line[1].split(',').map((name) => name.trim()).filter(Boolean) : []);
}

function checkPreToolUseMatcher(entries) {
  const pre = entries.filter((entry) => entry.event === 'PreToolUse');
  const matchers = pre.map((entry) => entry.matcher || '(none)').join(' ; ');
  const hits = (entry, tool, anchored) => !entry.matcher || entry.matcher === '*' ||
    (/^[A-Za-z0-9_|]+$/.test(entry.matcher) ? entry.matcher.split('|').includes(tool) :
      new RegExp(anchored ? `^(${entry.matcher})$` : entry.matcher).test(tool));
  for (const tool of ['Write', 'Edit', 'MultiEdit', 'Agent', 'Task', 'WebSearch', 'WebFetch', 'Workflow']) {
    check(`PreToolUse matcher starts the hook for ${tool}`, pre.some((entry) => hits(entry, tool, true)), matchers);
  }
  check('PreToolUse matcher skips NotebookEdit, so a notebook edit never waits for a process',
    !pre.some((entry) => hits(entry, 'NotebookEdit', false)), matchers);
  for (const tool of ['Edit', 'MultiEdit', 'NotebookEdit']) {
    for (const file of ['lite.md', 'digest.md']) {
      const disallowed = agentDisallowedTools(file);
      check(`agents/${file} keeps ${tool} in disallowedTools, since the write policy never sees it`,
        disallowed.has(tool), `disallowedTools: ${[...disallowed].join(', ') || '(none)'}`);
    }
  }
}

function binaryHandlerNames() {
  const source = fs.readFileSync(path.join(ROOT, 'go', 'cmd', 'noctis', 'hooks.go'), 'utf8');
  const table = source.slice(source.indexOf('handlers := map[string]func(object, object){'));
  const body = table.slice(0, table.indexOf('}'));
  return new Set([...body.matchAll(/"([A-Za-z]+)":/g)].map((m) => m[1]));
}

function runManifestEntry(entry, account, lab, input, extraEnv = {}) {
  const command = entry.command.replace('${CLAUDE_PLUGIN_ROOT}', pluginRootOf(account));
  const result = spawnSync(command, entry.args, {
    encoding: 'utf8',
    input: input === null ? '' : JSON.stringify(input),
    env: account.env(extraEnv),
    timeout: 60000,
  });
  return { command, ...result };
}

function checkLauncher() {
  const root = fs.mkdtempSync(path.join(require('os').tmpdir(), 'noctis-launcher-'));
  try {
    const bin = path.join(root, 'bin');
    fs.mkdirSync(bin, { recursive: true });
    fs.copyFileSync(path.join(ROOT, 'bin', 'noctis'), path.join(bin, 'noctis'));
    fs.chmodSync(path.join(bin, 'noctis'), 0o755);
    const stub = (relative) => {
      const file = path.join(bin, relative);
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.writeFileSync(file, `#!/bin/sh\necho ${relative} "$@"\n`, { mode: 0o755 });
    };
    for (const relative of ['linux-amd64/noctis', 'linux-arm64/noctis', 'darwin-arm64/noctis', 'darwin-amd64/noctis', 'windows-amd64/noctis.exe', 'noctis.exe']) stub(relative);
    const fake = path.join(root, 'fake');
    fs.mkdirSync(fake);
    fs.writeFileSync(path.join(fake, 'uname'), '#!/bin/sh\ncase "$1" in -s) echo "$FAKE_S" ;; -m) echo "$FAKE_M" ;; esac\n', { mode: 0o755 });
    const run = (system, machine, extra = {}, cwd = root, script = path.join(bin, 'noctis')) => spawnSync('sh', [script, 'version'], {
      cwd,
      encoding: 'utf8',
      env: { ...process.env, PATH: `${fake}${path.delimiter}${process.env.PATH}`, FAKE_S: system, FAKE_M: machine, ...extra },
    });
    const expectations = [
      ['Linux', 'x86_64', 'linux-amd64/noctis'],
      ['Linux', 'aarch64', 'linux-arm64/noctis'],
      ['Darwin', 'arm64', 'darwin-arm64/noctis'],
      ['Darwin', 'x86_64', 'darwin-amd64/noctis'],
      ['MINGW64_NT-10.0-26100', 'x86_64', 'windows-amd64/noctis.exe'],
      ['MSYS_NT-10.0-26100', 'x86_64', 'windows-amd64/noctis.exe'],
      ['CYGWIN_NT-10.0-26100', 'x86_64', 'windows-amd64/noctis.exe'],
      ['MINGW64_NT-10.0-26100', 'aarch64', 'noctis.exe'],
    ];
    for (const [system, machine, expected] of expectations) {
      const result = run(system, machine);
      check(`launcher: ${system} ${machine} runs bin/${expected}`, result.status === 0 && result.stdout.trim() === `${expected} version`,
        `exit ${result.status}: ${(result.stdout || '').trim()} ${(result.stderr || '').trim()}`);
    }
    const unsupported = run('Linux', 'armv7l');
    check('launcher: an unsupported CPU stops with its name instead of running an amd64 binary',
      unsupported.status === 1 && /armv7l/.test(unsupported.stderr) && !unsupported.stdout.trim(),
      `exit ${unsupported.status}: ${(unsupported.stdout || '').trim()} ${(unsupported.stderr || '').trim()}`);
    const cdpath = run('Linux', 'x86_64', { CDPATH: `.${path.delimiter}${root}` }, root, 'bin/noctis');
    check('launcher: an exported CDPATH does not break the plugin root', cdpath.status === 0 && cdpath.stdout.trim() === 'linux-amd64/noctis version',
      `exit ${cdpath.status}: ${(cdpath.stdout || '').trim()} ${(cdpath.stderr || '').trim()}`);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
}

async function main() {
  const lab = new Lab('noctis-contract');
  await lab.startMock();
  lab.setLimits(limitsAt(12, 30));
  try {
    const account = lab.account('main');
    account.install();
    const entries = manifestEntries();
    const handlers = binaryHandlerNames();

    check('manifest declares at least one hook', entries.length > 0);
    check('handler table was parsed out of hooks.go', handlers.size >= 10, `found ${handlers.size}`);
    checkManifestShape(entries, handlers);
    checkPreToolUseMatcher(entries);

    const sampleCommand = entries[0].command.replace('${CLAUDE_PLUGIN_ROOT}', pluginRootOf(account));
    let executable = false;
    try {
      fs.accessSync(sampleCommand, fs.constants.X_OK);
      executable = true;
    } catch {  }
    check('installed command exists and is executable', executable, sampleCommand);

    let index = 0;
    for (const entry of entries) {
      index += 1;
      const sid = `contract-${index}`;
      const input = entry.args[0] === 'hook' ? payloadFor(entry.event, sid, lab, account) : null;
      const result = runManifestEntry(entry, account, lab, input);
      const where = `${entry.event}/${entry.args.join(' ')}`;

      check(`${where}: spawned`, !result.error, result.error && String(result.error));
      if (result.error) continue;

      check(`${where}: exit 0`, result.status === 0,
        `exit ${result.status}; stderr: ${(result.stderr || '').slice(0, 300)}`);

      const out = (result.stdout || '').trim();
      if (out !== '') {
        let parsed = null;
        try { parsed = JSON.parse(out); } catch {  }
        check(`${where}: stdout is one JSON object`, parsed !== null && typeof parsed === 'object',
          out.slice(0, 300));
        if (parsed && parsed.hookSpecificOutput) {
          check(`${where}: hookEventName echoes the event`,
            parsed.hookSpecificOutput.hookEventName === entry.event,
            `got ${parsed.hookSpecificOutput.hookEventName}`);
        }
        if (parsed) {
          check(`${where}: does not block on an ordinary payload`,
            parsed.continue !== false &&
            !(parsed.hookSpecificOutput && parsed.hookSpecificOutput.permissionDecision === 'deny'),
            out.slice(0, 300));
        }
      }
    }

    const hookEntry = entries.find((entry) => entry.args[0] === 'hook');
    for (const [label, input] of [['empty stdin', null], ['unknown event', { session_id: 'x', hook_event_name: 'SomethingNew' }]]) {
      const result = runManifestEntry(hookEntry, account, lab, input);
      check(`${label}: exit 0`, result.status === 0, `exit ${result.status}: ${(result.stderr || '').slice(0, 200)}`);
      const out = (result.stdout || '').trim();
      if (out !== '') {
        let ok = true;
        try { JSON.parse(out); } catch { ok = false; }
        check(`${label}: stdout is JSON`, ok, out.slice(0, 200));
      }
    }

    const decisionOf = (stdout) => {
      const text = (stdout || '').trim();
      if (text === '') return '';
      let parsed;
      try { parsed = JSON.parse(text); } catch { return `unparseable:${text.slice(0, 120)}`; }
      delete parsed.systemMessage;
      return Object.keys(parsed).length === 0 ? '' : JSON.stringify(parsed);
    };
    const bothWays = (label, payload, extraEnv = {}) => {
      const command = hookEntry.command.replace('${CLAUDE_PLUGIN_ROOT}', pluginRootOf(account));
      const spawnIt = (args) => spawnSync(command, args, {
        encoding: 'utf8',
        input: JSON.stringify(payload),
        env: account.env(extraEnv),
        timeout: 60000,
      });
      const wired = spawnIt(hookEntry.args);
      const dropped = spawnIt([]);
      check(`${label}: exit 0 when args are dropped`, dropped.status === 0, `exit ${dropped.status}`);
      check(`${label}: decision survives the dropped args`,
        decisionOf(dropped.stdout) === decisionOf(wired.stdout),
        `wired ${decisionOf(wired.stdout) || '(empty)'} vs dropped ${decisionOf(dropped.stdout) || '(empty)'}`);
      return { wired, dropped };
    };

    const calm = bothWays('args dropped, calm', payloadFor('UserPromptSubmit', 'contract-dropped-calm', lab, account));
    check('args dropped: the wiring problem is reported',
      /noctis doctor/.test(calm.dropped.stdout || ''),
      (calm.dropped.stdout || '').slice(0, 200));
    const again = spawnSync(
      hookEntry.command.replace('${CLAUDE_PLUGIN_ROOT}', pluginRootOf(account)), [],
      { encoding: 'utf8', input: JSON.stringify(payloadFor('UserPromptSubmit', 'contract-dropped-calm2', lab, account)), env: account.env(), timeout: 60000 },
    );
    check('args dropped: the notice is not repeated within the day',
      !/noctis doctor/.test(again.stdout || ''), (again.stdout || '').slice(0, 200));

    account.setConfig((config) => {
      config.wait.maxInHookMinutes = 0;
    });
    lab.setLimits(limitsAt(97, 40));
    fs.rmSync(path.join(account.guardDir, 'fable.json'), { force: true });
    const { wired } = bothWays('args dropped, at the limit',
      payloadFor('UserPromptSubmit', 'contract-dropped-limit', lab, account),
      { NOCTIS_NO_SCHEDULE: '1' });
    check('at the limit the wired call really does block',
      /"decision":\s*"block"|"continue":\s*false|"permissionDecision":\s*"deny"/.test(wired.stdout || ''),
      `the scenario did not block at all, so the comparison above proves nothing: ${(wired.stdout || '').slice(0, 200)}`);

    const pluginVersion = JSON.parse(fs.readFileSync(path.join(ROOT, '.claude-plugin', 'plugin.json'), 'utf8')).version;
    const reported = spawnSync(sampleCommand, ['version'], { encoding: 'utf8', env: account.env() });
    check('binary version matches the plugin manifest',
      (reported.stdout || '').trim() === pluginVersion,
      `binary ${(reported.stdout || '').trim()} vs manifest ${pluginVersion}`);

    account.stopRunners();

    if (process.platform !== 'win32') checkLauncher();

    if (failures.length > 0) {
      console.error('CONTRACT FAILED');
      for (const failure of failures) console.error(`  ✗ ${failure}`);
      process.exitCode = 1;
      return;
    }
    console.log(`contract: ${checks}/${checks} checks passed (${entries.length} manifest entries run as the host runs them)`);
  } finally {
    lab.stopMock();
    fs.rmSync(lab.root, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error('CONTRACT CRASHED');
  console.error(error);
  process.exit(1);
});
