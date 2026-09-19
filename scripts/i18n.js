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
  const fromSource = parseEntries(blockBody(fs.readFileSync(MESSAGES, 'utf8').slice(fs.readFileSync(MESSAGES, 'utf8').indexOf('func baseCatalog()')), 'en'));
  if (JSON.stringify(english) !== JSON.stringify(fromSource)) {
    throw new Error('i18n/en.json does not match messages.go — run `node scripts/i18n.js extract`');
  }
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
