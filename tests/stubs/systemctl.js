#!/usr/bin/env node
'use strict';

const fs = require('fs');

const log = process.env.NOCTIS_SCHEDULER_LOG;
const argv = process.argv.slice(2).filter((arg) => arg !== '--user');

if (argv[0] === 'is-system-running') {
  process.stdout.write('running\n');
  process.exit(0);
}
if (argv[0] === 'stop') {
  for (const unit of argv.slice(1)) {
    fs.appendFileSync(log, JSON.stringify({ kind: 'stop', unit: unit.replace(/\.(timer|service)$/, '') }) + '\n');
  }
}
process.exit(0);
