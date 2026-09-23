#!/usr/bin/env node
'use strict';

const fs = require('fs');

const log = process.env.NOCTIS_SCHEDULER_LOG;
const argv = process.argv.slice(2).filter((arg) => arg !== '--user');
const record = (entry) => fs.appendFileSync(log, JSON.stringify(entry) + '\n');
const history = () => fs.readFileSync(log, 'utf8').split('\n').filter(Boolean).map((line) => JSON.parse(line));

if (argv[0] === 'is-system-running') {
  process.stdout.write('running\n');
  process.exit(0);
}

const escapeRegExp = (text) => text.replace(/[.+^${}()|[\]\\]/g, '\\$&');
const globToRegExp = (glob) => new RegExp('^' + glob.split('*').map((part) => part.split('?').map(escapeRegExp).join('.')).join('.*') + '$');

function expand(name) {
  if (!/[*?]/.test(name)) return [name];
  const pattern = globToRegExp(name);
  const known = history().filter((entry) => entry.kind === 'schedule').map((entry) => entry.unit);
  return [...new Set(known)].filter((unit) => pattern.test(unit));
}

if (argv[0] === 'stop') {
  const services = [];
  for (const target of argv.slice(1)) {
    const isTimer = target.endsWith('.timer');
    for (const unit of expand(target.replace(/\.(timer|service)$/, ''))) {
      if (isTimer) record({ kind: 'stop', unit });
      else services.push(unit);
    }
  }
  const fired = history().filter((entry) => entry.kind === 'fire' && entry.pid);
  const victims = [];
  for (const unit of services) {
    record({ kind: 'stop-service', unit });
    for (const entry of fired.filter((candidate) => candidate.unit === unit)) {
      record({ kind: 'terminate', unit, pid: entry.pid });
      victims.push(entry);
    }
  }
  for (const entry of victims) {
    try {
      process.kill(-entry.pid, 'SIGTERM');
    } catch (error) {
      record({ kind: 'terminate-failed', unit: entry.unit, pid: entry.pid, why: error.code });
    }
  }
}
process.exit(0);
