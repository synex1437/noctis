#!/usr/bin/env node
'use strict';

const fs = require('fs');
const { spawn } = require('child_process');

const log = process.env.NOCTIS_SCHEDULER_LOG;
const unit = process.env.NOCTIS_STUB_UNIT;
const fireAt = Number(process.env.NOCTIS_STUB_FIRE_AT);
const command = JSON.parse(process.env.NOCTIS_STUB_COMMAND);
const after = Number(process.env.NOCTIS_STUB_AFTER);

const jobEnvironment = () => {
  const environment = { PATH: '/usr/bin:/bin' };
  for (const [name, value] of Object.entries(process.env)) {
    if (['HOME', 'USER', 'LOGNAME', 'SHELL', 'TMPDIR', 'LANG', 'XPC_SERVICE_NAME'].includes(name) || name.startsWith('NOCTIS_')) {
      environment[name] = value;
    }
  }
  return { ...environment, ...JSON.parse(process.env.NOCTIS_STUB_JOB_ENV || '{}') };
};

const cancelled = () => {
  try {
    return fs.readFileSync(log, 'utf8').split('\n').filter(Boolean).slice(after).some((line) => {
      const entry = JSON.parse(line);
      return entry.kind === 'stop' && entry.unit === unit;
    });
  } catch {
    return false;
  }
};

const tick = () => {
  if (cancelled()) {
    fs.appendFileSync(log, JSON.stringify({ kind: 'cancelled', unit }) + '\n');
    return;
  }
  if (Date.now() / 1000 >= fireAt) {
    const job = spawn(command[0], command.slice(1), { detached: true, stdio: 'ignore', env: jobEnvironment() });
    fs.appendFileSync(log, JSON.stringify({ kind: 'fire', unit, at: Math.floor(Date.now() / 1000), pid: job.pid }) + '\n');
    job.unref();
    return;
  }
  setTimeout(tick, 200);
};
tick();
