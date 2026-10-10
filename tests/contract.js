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
    case 'PreCompact':
      return { ...base, trigger: 'auto', custom_instructions: '' };
    case 'SubagentStart':
      return { ...base, agent_id: `${sid}-agent`, agent_type: 'general-purpose' };
    case 'SubagentStop':
      return { ...base, stop_hook_active: false, agent_id: `${sid}-agent`, agent_type: 'general-purpose', agent_transcript_path: path.join(account.dir, `agent-${sid}.jsonl`) };
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
  for (const tool of ['Write', 'Edit', 'MultiEdit', 'Agent', 'Task', 'WebSearch', 'WebFetch', 'Workflow', 'AskUserQuestion']) {
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

function checkStopFailureMatcher(entries) {
  const matchers = entries.filter((entry) => entry.event === 'StopFailure').map((entry) => entry.matcher || '');
  for (const error of ['rate_limit', 'overloaded', 'server_error', 'model_not_found', 'billing_error', 'account_on_hold', 'authentication_failed', 'invalid_request', 'max_output_tokens', 'verification_required', 'oauth_org_not_allowed', 'cloud_credential_error', 'unknown']) {
    check(`StopFailure matcher starts the hook for ${error}`,
      matchers.some((matcher) => !matcher || new RegExp(`^(${matcher})$`).test(error)), matchers.join(' ; ') || '(none)');
  }
}

function checkNotificationMatcher(entries) {
  const matchers = entries.filter((entry) => entry.event === 'Notification').map((entry) => entry.matcher || '');
  const starts = (type) => matchers.some((matcher) => !matcher || new RegExp(`^(${matcher})$`).test(type));
  for (const type of ['quota_auto_resume_fired', 'quota_auto_resume_stale', 'quota_auto_resume_disabled', 'permission_prompt', 'worker_permission_prompt', 'elicitation_dialog', 'elicitation_url_dialog']) {
    check(`Notification matcher starts the hook for ${type}`, starts(type), matchers.join(' ; ') || '(none)');
  }
  check('Notification matcher skips idle_prompt, which comes whenever a session has sat idle for a minute after a turn', !starts('idle_prompt'), matchers.join(' ; ') || '(none)');
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
  account.settleRefresh(entry.args);
  return { command, ...result };
}

function checkLauncher() {
  const root = fs.mkdtempSync(path.join(require('os').tmpdir(), 'noctis-launcher-'));
  try {
    const bin = path.join(root, 'bin');
    fs.mkdirSync(bin, { recursive: true });
    // The copy reads a kernel folder of the test's own, so every source of the answer can be set.
    const shipped = fs.readFileSync(path.join(ROOT, 'bin', 'noctis'), 'utf8');
    const kernelLine = 'kernel=/proc/sys/kernel\n';
    check('launcher: asks the kernel first', shipped.includes(kernelLine), `no line ${JSON.stringify(kernelLine)} in bin/noctis`);
    const kernel = path.join(root, 'kernel');
    fs.writeFileSync(path.join(bin, 'noctis'), shipped.replace(kernelLine, `kernel='${kernel}'\n`), { mode: 0o755 });
    const stub = (relative) => {
      const file = path.join(bin, relative);
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.writeFileSync(file, `#!/bin/sh\necho ${relative} "$@"\n`, { mode: 0o755 });
    };
    for (const relative of ['linux-amd64/noctis', 'linux-arm64/noctis', 'darwin/noctis', 'windows-amd64/noctis.exe', 'noctis.exe']) stub(relative);
    const fake = path.join(root, 'fake');
    fs.mkdirSync(fake);
    const unameLog = path.join(root, 'uname.log');
    fs.writeFileSync(path.join(fake, 'uname'), `#!/bin/sh\necho "$*" >> '${unameLog}'\ncase "$1" in -sm) echo "$FAKE_S $FAKE_M" ;; *) echo "uname $* was not expected" >&2; exit 1 ;; esac\n`, { mode: 0o755 });
    const inherited = { ...process.env };
    delete inherited.OSTYPE;
    delete inherited.HOSTTYPE;
    delete inherited.GOMAXPROCS;
    delete inherited.NOCTIS_OWN_GOMAXPROCS;
    // An empty OSTYPE and HOSTTYPE in the environment keep bash (sh on macOS) from setting its own.
    const run = ({ kernelSays = [], shellSays = ['', ''], unameSays = ['', ''], extra = {}, cwd = root, script = path.join(bin, 'noctis'), command = 'version' } = {}) => {
      fs.rmSync(kernel, { recursive: true, force: true });
      const [ostype, arch] = kernelSays;
      if (ostype !== undefined) {
        fs.mkdirSync(kernel);
        fs.writeFileSync(path.join(kernel, 'ostype'), `${ostype}\n`);
        if (arch !== undefined) fs.writeFileSync(path.join(kernel, 'arch'), `${arch}\n`);
      }
      fs.rmSync(unameLog, { force: true });
      const result = spawnSync('sh', [script, command], {
        cwd,
        encoding: 'utf8',
        env: { ...inherited, PATH: `${fake}${path.delimiter}${process.env.PATH}`, OSTYPE: shellSays[0], HOSTTYPE: shellSays[1], FAKE_S: unameSays[0], FAKE_M: unameSays[1], ...extra },
      });
      const unameCalls = fs.existsSync(unameLog) ? fs.readFileSync(unameLog, 'utf8').trim().split('\n').length : 0;
      return { ...result, unameCalls, shown: `exit ${result.status}, uname ran ${unameCalls}x: ${(result.stdout || '').trim()} ${(result.stderr || '').trim()}` };
    };
    const runs = (result, expected, unameCalls) => result.status === 0 && result.stdout.trim() === `${expected} version` && result.unameCalls === unameCalls;
    for (const [ostype, arch, expected] of [['Linux', 'x86_64', 'linux-amd64/noctis'], ['Linux', 'aarch64', 'linux-arm64/noctis']]) {
      const result = run({ kernelSays: [ostype, arch], shellSays: ['darwin23', 'arm64'] });
      check(`launcher: the kernel's ${ostype} ${arch} runs bin/${expected} without uname, whatever the shell says`, runs(result, expected, 0), result.shown);
    }
    for (const [ostype, hosttype, expected, kernelSays] of [
      ['linux-gnu', 'x86_64', 'linux-amd64/noctis'],
      ['linux-gnu', 'aarch64', 'linux-arm64/noctis', ['Linux']],
      ['msys', 'x86_64', 'windows-amd64/noctis.exe'],
      ['cygwin', 'x86_64', 'windows-amd64/noctis.exe'],
      ['msys', 'aarch64', 'noctis.exe'],
    ]) {
      const result = run({ kernelSays, shellSays: [ostype, hosttype] });
      check(`launcher: bash's ${ostype} ${hosttype} runs bin/${expected} without uname${kernelSays ? ' (a kernel before 6.1 has no arch file)' : ''}`, runs(result, expected, 0), result.shown);
    }
    // macOS has one universal binary and runs the build in it for its CPU, so the shell's OSTYPE is
    // all the launcher asks there: not HOSTTYPE, which macOS's /bin/bash (one build for two CPUs) may
    // get wrong, and not uname, whose answer here (Linux) would show it was asked.
    for (const [ostype, hosttype] of [['darwin23', 'arm64'], ['darwin23', 'x86_64'], ['darwin24.0', ''], ['darwin', 'intel-mac']]) {
      const result = run({ shellSays: [ostype, hosttype], unameSays: ['Linux', 'x86_64'] });
      check(`launcher: on macOS the shell's ${ostype}${hosttype ? ` with HOSTTYPE ${hosttype}` : ''} runs bin/darwin/noctis without uname`, runs(result, 'darwin/noctis', 0), result.shown);
    }
    const expectations = [
      ['Linux', 'x86_64', 'linux-amd64/noctis'],
      ['Linux', 'aarch64', 'linux-arm64/noctis'],
      ['Darwin', 'arm64', 'darwin/noctis'],
      ['Darwin', 'x86_64', 'darwin/noctis'],
      ['MINGW64_NT-10.0-26100', 'x86_64', 'windows-amd64/noctis.exe'],
      ['MSYS_NT-10.0-26100', 'x86_64', 'windows-amd64/noctis.exe'],
      ['CYGWIN_NT-10.0-26100', 'x86_64', 'windows-amd64/noctis.exe'],
      ['MINGW64_NT-10.0-26100', 'aarch64', 'noctis.exe'],
    ];
    for (const [system, machine, expected] of expectations) {
      const result = run({ unameSays: [system, machine] });
      check(`launcher: uname's ${system} ${machine} runs bin/${expected}, asked once`, runs(result, expected, 1), result.shown);
    }
    const foreign = run({ shellSays: ['linux', 'x86_64-linux'], unameSays: ['Darwin', 'arm64'] });
    check('launcher: an OSTYPE and HOSTTYPE bash never sets (tcsh exports these) are left to uname', runs(foreign, 'darwin/noctis', 1), foreign.shown);
    for (const [label, options] of [['the kernel', { kernelSays: ['Linux', 'armv7l'] }], ['uname', { unameSays: ['Linux', 'armv7l'] }]]) {
      const unsupported = run(options);
      check(`launcher: an unsupported CPU named by ${label} stops with its name instead of running an amd64 binary`,
        unsupported.status === 1 && /armv7l/.test(unsupported.stderr) && !unsupported.stdout.trim(), unsupported.shown);
    }
    const cdpath = run({ kernelSays: ['Linux', 'x86_64'], extra: { CDPATH: `.${path.delimiter}${root}` }, script: 'bin/noctis' });
    check('launcher: a relative path with an exported CDPATH still finds the binaries', runs(cdpath, 'linux-amd64/noctis', 0), cdpath.shown);
    const bare = run({ kernelSays: ['Linux', 'x86_64'], cwd: bin, script: 'noctis' });
    check('launcher: run by its bare name from its own folder, it finds the binaries next to it', runs(bare, 'linux-amd64/noctis', 0), bare.shown);
    // A hook and a status line refresh get GOMAXPROCS=1 with the marker that has noctis take both
    // out again; other commands and a GOMAXPROCS already set are left alone.
    fs.writeFileSync(path.join(bin, 'linux-amd64', 'noctis'), '#!/bin/sh\necho "$1 GOMAXPROCS=${GOMAXPROCS-unset} marker=${NOCTIS_OWN_GOMAXPROCS-unset}"\n', { mode: 0o755 });
    const limits = (command, extra) => {
      const result = run({ kernelSays: ['Linux', 'x86_64'], command, extra });
      return `exit ${result.status}: ${(result.stdout || '').trim()}${(result.stderr || '').trim()}`;
    };
    check('launcher: a hook runs with GOMAXPROCS=1 and the marker that has noctis take it out', limits('hook'), 'exit 0: hook GOMAXPROCS=1 marker=1');
    check('launcher: a status line refresh too', limits('statusline'), 'exit 0: statusline GOMAXPROCS=1 marker=1');
    check("launcher: another command keeps the Go runtime's own GOMAXPROCS", limits('status'), 'exit 0: status GOMAXPROCS=unset marker=unset');
    check('launcher: a GOMAXPROCS already set reaches a hook as it is, with no marker', limits('hook', { GOMAXPROCS: '3' }), 'exit 0: hook GOMAXPROCS=3 marker=unset');
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
}

// Only a Mac can run bin/darwin/noctis: as it is, and each build in it that this Mac can run (arm64
// on Apple silicon, x86_64 on an Intel Mac or under Rosetta); lipo, where the developer tools are
// installed, has to find both builds in it. tests/hygiene.js checks the file's layout everywhere.
function checkUniversalBinary(account, pluginVersion) {
  const binary = path.join(ROOT, 'bin', 'darwin', 'noctis');
  const ran = (command, args) => {
    const result = spawnSync(command, args, { encoding: 'utf8', env: account.env(), timeout: 60000 });
    return { ...result, shown: `exit ${result.status}${result.error ? ` (${result.error.message})` : ''}: ${`${result.stdout || ''}${result.stderr || ''}`.trim()}` };
  };
  const native = ran(binary, ['version']);
  check(`universal binary: bin/darwin/noctis runs on this ${process.arch} Mac`, native.status === 0 && native.stdout.trim() === pluginVersion, native.shown);
  for (const cpu of ['x86_64', 'arm64']) {
    // arch -x86_64 fails on a Mac without Rosetta, arch -arm64 on an Intel Mac.
    if (ran('/usr/bin/arch', [`-${cpu}`, '/usr/bin/true']).status !== 0) continue;
    const result = ran('/usr/bin/arch', [`-${cpu}`, binary, 'version']);
    check(`universal binary: its ${cpu} build runs (arch -${cpu})`, result.status === 0 && result.stdout.trim() === pluginVersion, result.shown);
  }
  // Without the developer tools, /usr/bin/lipo only offers to install them.
  if (spawnSync('/usr/bin/xcode-select', ['-p']).status === 0) {
    const lipo = ran('/usr/bin/lipo', ['-archs', binary]);
    check('universal binary: lipo finds the x86_64 and arm64 builds in it', lipo.status === 0 && lipo.stdout.trim().split(/\s+/).sort().join(' ') === 'arm64 x86_64', lipo.shown);
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
    checkStopFailureMatcher(entries);
    checkNotificationMatcher(entries);

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
    lab.setLimits(limitsAt(100, 40));
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
    if (process.platform === 'darwin') checkUniversalBinary(account, pluginVersion);

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
