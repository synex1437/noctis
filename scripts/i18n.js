#!/usr/bin/env node
'use strict';

const fs = require('fs');
const path = require('path');

const ROOT = path.dirname(__dirname);
const I18N = path.join(ROOT, 'i18n');
const MESSAGES = path.join(ROOT, 'go', 'cmd', 'noctis', 'messages.go');
const LANG = path.join(ROOT, 'go', 'cmd', 'noctis', 'lang.go');

const REFERENCE = ['en', 'tr'];
const LANGUAGES = ['de', 'fr', 'es', 'pt', 'it', 'nl', 'pl', 'ru', 'ja', 'zh', 'ko', 'ar'];

function blockBody(source, language, from = 0) {
  const marker = new RegExp(`"${language}":\\s*\\{`, 'g');
  marker.lastIndex = from;
  const match = marker.exec(source);
  if (!match) return null;
  let index = match.index + match[0].length;
  let depth = 1;
  while (depth > 0 && index < source.length) {
    if (source[index] === '{') depth += 1;
    else if (source[index] === '}') depth -= 1;
    index += 1;
  }
  return source.slice(match.index + match[0].length, index - 1);
}

function parseEntries(body) {
  const entries = {};
  const pattern = /"([a-zA-Z0-9_.]+)":\s*("(?:[^"\\]|\\.)*")/g;
  let match;
  while ((match = pattern.exec(body)) !== null) entries[match[1]] = JSON.parse(match[2]);
  return entries;
}

function readCatalog(code) {
  return JSON.parse(fs.readFileSync(path.join(I18N, `${code}.json`), 'utf8'));
}

function extract() {
  const source = fs.readFileSync(MESSAGES, 'utf8');
  const base = source.slice(source.indexOf('func baseCatalog()'));
  for (const code of REFERENCE) {
    const entries = parseEntries(blockBody(base, code));
    if (Object.keys(entries).length === 0) throw new Error(`no ${code} entries found in messages.go`);
    fs.writeFileSync(path.join(I18N, `${code}.json`), `${JSON.stringify(entries, null, 2)}\n`);
    process.stdout.write(`i18n/${code}.json: ${Object.keys(entries).length} keys\n`);
  }
}

function renderCatalogFunction() {
  const english = readCatalog('en');
  const order = Object.keys(english);
  const lines = [
    'func extraCatalogTable() map[string]map[string]string {',
    '\treturn map[string]map[string]string{',
  ];
  for (const code of LANGUAGES) {
    const table = readCatalog(code);
    const extraKeys = Object.keys(table).filter((key) => !order.includes(key));
    if (extraKeys.length > 0) {
      throw new Error(`i18n/${code}.json has ${extraKeys.length} key(s) English does not: ${extraKeys.join(', ')}`);
    }
    lines.push(`\t\t"${code}": {`);
    for (const key of order) {
      if (!(key in table)) continue;
      lines.push(`\t\t\t${JSON.stringify(key)}: ${JSON.stringify(table[key])},`);
    }
    lines.push('\t\t},');
  }
  lines.push('\t}', '}');
  return lines.join('\n');
}

function spliceInto(source, rendered) {
  const startMarker = source.indexOf('func extraCatalogTable()');
  if (startMarker < 0) throw new Error('extraCatalogTable not found in lang.go');
  let start = startMarker;
  const before = source.slice(0, startMarker).split('\n');
  let keep = before.length - 1;
  while (keep > 0 && before[keep - 1].startsWith('//')) keep -= 1;
  start = before.slice(0, keep).join('\n').length + (keep > 0 ? 1 : 0);

  let index = source.indexOf('{', startMarker);
  let depth = 1;
  index += 1;
  while (depth > 0 && index < source.length) {
    if (source[index] === '{') depth += 1;
    else if (source[index] === '}') depth -= 1;
    index += 1;
  }
  return source.slice(0, start) + rendered + source.slice(index);
}

function renderGo() {
  return spliceInto(fs.readFileSync(LANG, 'utf8'), renderCatalogFunction());
}

function build() {
  const rendered = renderGo();
  fs.writeFileSync(LANG, rendered);
  const gofmt = require('child_process').spawnSync('gofmt', ['-w', LANG], { encoding: 'utf8' });
  if (gofmt.status !== 0) throw new Error(`gofmt failed: ${gofmt.stderr || gofmt.error}`);
  const counts = LANGUAGES.map((code) => `${code} ${Object.keys(readCatalog(code)).length}`).join('  ');
  process.stdout.write(`lang.go written: ${counts}\n`);
}

const WIDE = [[0x1100, 0x115f], [0x2e80, 0x303e], [0x3041, 0x33ff], [0x3400, 0x4dbf], [0x4e00, 0x9fff],
  [0xa000, 0xa4cf], [0xa960, 0xa97f], [0xac00, 0xd7a3], [0xf900, 0xfaff], [0xfe10, 0xfe19],
  [0xfe30, 0xfe6f], [0xff00, 0xff60], [0xffe0, 0xffe6], [0x1f300, 0x1f64f], [0x1f900, 0x1f9ff],
  [0x20000, 0x3fffd]];
const ZERO = /\p{Mn}|\p{Me}|\p{Cf}/u;

function displayWidth(text) {
  let total = 0;
  for (const char of text) {
    if (ZERO.test(char)) continue;
    const code = char.codePointAt(0);
    total += WIDE.some(([from, to]) => code >= from && code <= to) ? 2 : 1;
  }
  return total;
}

const ARGUMENT = /%(?:\[(\d+)\])?[-+# 0]*\d*(?:\.\d+)?([a-zA-Z%])/g;

function argumentTypes(text) {
  const types = {};
  let next = 1;
  for (const [, index, verb] of text.matchAll(ARGUMENT)) {
    if (verb === '%') continue;
    const position = index ? Number(index) : next;
    types[position] = position in types && types[position] !== verb ? 'conflict' : verb;
    next = position + 1;
  }
  return types;
}

const STATUS_ROWS = ['status.accountDir', 'status.usage', 'status.thresholds', 'status.model',
  'status.router', 'status.skew', 'status.configBroken', 'status.disabled', 'status.observe',
  'status.plan', 'status.waits', 'status.handedOff', 'status.errorsClean', 'status.errors',
  'status.credits', 'status.roles', 'status.scopedData'];

function auditCatalog(code, english, table, problems) {
  for (const [key, source] of Object.entries(english)) {
    const target = table[key];
    if (target === undefined) continue;
    const wanted = JSON.stringify(argumentTypes(source));
    const got = JSON.stringify(argumentTypes(target));
    if (wanted !== got) {
      problems.push(`i18n/${code}.json: "${key}" takes ${got} where English takes ${wanted} — Go would print %!verb(MISSING)`);
    }
    if (source.trimStart().startsWith('[noctis]') && target !== source) {
      problems.push(`i18n/${code}.json: "${key}" is an instruction handed to the model; those stay in English`);
    }
  }
  const columns = new Map();
  for (const key of STATUS_ROWS) {
    const label = /^(.*?)[:\uff1a]/.exec(table[key] || '');
    if (label) columns.set(key, displayWidth(label[1]));
  }
  const widths = [...new Set(columns.values())];
  if (widths.length > 1) {
    const listed = [...columns].map(([key, width]) => `${key}=${width}`).join(' ');
    problems.push(`i18n/${code}.json: the status labels do not line up (${listed})`);
  }
}

function check() {
  const onDisk = fs.readFileSync(LANG, 'utf8');
  const temporary = path.join(require('os').tmpdir(), `noctis-i18n-${process.pid}.go`);
  fs.writeFileSync(temporary, renderGo());
  const gofmt = require('child_process').spawnSync('gofmt', [temporary], { encoding: 'utf8' });
  fs.rmSync(temporary, { force: true });
  if (gofmt.status !== 0) throw new Error(`gofmt failed: ${gofmt.stderr || gofmt.error}`);
  if (gofmt.stdout !== onDisk) {
    throw new Error('lang.go does not match i18n/*.json — run `node scripts/i18n.js build`');
  }
  const english = readCatalog('en');
  const base = fs.readFileSync(MESSAGES, 'utf8');
  const baseCatalog = base.slice(base.indexOf('func baseCatalog()'));
  for (const code of REFERENCE) {
    if (JSON.stringify(readCatalog(code)) !== JSON.stringify(parseEntries(blockBody(baseCatalog, code)))) {
      throw new Error(`i18n/${code}.json does not match messages.go — run \`node scripts/i18n.js extract\``);
    }
  }
  const problems = [];
  for (const code of [...REFERENCE, ...LANGUAGES]) auditCatalog(code, english, readCatalog(code), problems);
  if (problems.length) throw new Error(problems.join('\n'));
  return true;
}

const mode = process.argv[2] || 'check';
try {
  if (mode === 'extract') extract();
  else if (mode === 'build') build();
  else if (mode === 'check') {
    check();
    process.stdout.write('i18n: lang.go matches i18n/*.json\n');
  } else {
    process.stderr.write(`unknown mode ${mode}; use extract, build or check\n`);
    process.exit(2);
  }
} catch (error) {
  process.stderr.write(`${error.message}\n`);
  process.exit(1);
}

module.exports = { check, renderGo, readCatalog, LANGUAGES };
