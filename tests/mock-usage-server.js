#!/usr/bin/env node
'use strict';

const fs = require('fs');
const http = require('http');
const path = require('path');

const dir = process.argv[2];
const limitsFile = path.join(dir, 'limits.json');
const hitsFile = path.join(dir, 'hits.log');
const portFile = path.join(dir, 'port');
const skewFile = path.join(dir, 'skew.json');

function outage() {
  try {
    return JSON.parse(fs.readFileSync(path.join(dir, 'outage.json'), 'utf8')).mode || '';
  } catch {
    return '';
  }
}

const server = http.createServer((request, response) => {
  if (String(request.url).startsWith('/webhook')) {
    let body = '';
    request.on('data', (chunk) => {
      body += chunk;
    });
    request.on('end', () => {
      fs.appendFileSync(path.join(dir, 'webhooks.log'), `${JSON.stringify({ url: request.url, title: request.headers.title || '', type: request.headers['content-type'] || '', body })}\n`);
      response.writeHead(outage() === 'webhook-error' ? 500 : 200);
      response.end('ok');
    });
    return;
  }
  if (String(request.url).startsWith('/plugin.json')) {
    fs.appendFileSync(path.join(dir, 'version-checks.log'), `${Date.now()}\n`);
    try {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(fs.readFileSync(path.join(dir, 'plugin-version.json'), 'utf8'));
    } catch {
      response.writeHead(404);
      response.end('{}');
    }
    return;
  }
  fs.appendFileSync(hitsFile, `${JSON.stringify(request.headers)}\n`);
  const mode = outage();
  if (mode === 'error') {
    response.writeHead(500);
    response.end('{}');
    return;
  }
  if (mode === 'garbage') {
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end('{not json');
    return;
  }
  if (mode === 'timeout') {
    setTimeout(() => {
      response.writeHead(200);
      response.end('{}');
    }, 9500);
    return;
  }
  if (mode === 'rate-limited') {
    response.writeHead(429, { 'Content-Type': 'application/json', 'Retry-After': '120' });
    response.end('{"error":{"type":"rate_limit_error"}}');
    return;
  }
  if (mode === 'forbidden') {
    response.writeHead(403, { 'Content-Type': 'application/json' });
    response.end('{"error":{"type":"permission_error"}}');
    return;
  }
  if (mode === 'truncated') {
    response.writeHead(200, { 'Content-Type': 'application/json', 'Content-Length': '400' });
    response.write('{"five_hour":null,"seven_day":null,"limits":[{"kind":"session","percent":1');
    response.socket.destroy();
    return;
  }
  if (mode === 'truncated-but-valid') {
    response.writeHead(200, { 'Content-Type': 'application/json', 'Content-Length': '900' });
    response.write('{"five_hour":null,"seven_day":null,"limits":[]}');
    response.socket.destroy();
    return;
  }
  if (mode === 'chunked-cut') {
    const body = '{"five_hour":null,"seven_day":null,"limits":[]}';
    const socket = request.socket;
    socket.write(
      'HTTP/1.1 200 OK\r\n'
      + 'Content-Type: application/json\r\n'
      + 'Transfer-Encoding: chunked\r\n'
      + '\r\n'
      + `${body.length.toString(16)}\r\n${body}\r\n`,
    );
    setTimeout(() => socket.destroy(), 30);
    return;
  }
  if (mode === 'reset') {
    response.socket.destroy();
    return;
  }
  if (mode === 'slow-drip') {
    response.writeHead(200, { 'Content-Type': 'application/json' });
    let sent = 0;
    const timer = setInterval(() => {
      sent += 1;
      try {
        response.write(' ');
      } catch {
        clearInterval(timer);
      }
      if (sent > 600) clearInterval(timer);
    }, 250);
    request.on('close', () => clearInterval(timer));
    return;
  }
  if (mode === 'huge') {
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.write('{"limits":[');
    for (let i = 0; i < 200000; i += 1) response.write('{"kind":"session","percent":1,"resets_at":"2026-01-01T00:00:00Z"},');
    response.end('{"kind":"session","percent":1,"resets_at":"2026-01-01T00:00:00Z"}]}');
    return;
  }
  const token = String(request.headers.authorization || '').replace(/^Bearer /, '');
  if (!token.startsWith('lab-token')) {
    response.writeHead(401);
    response.end('{}');
    return;
  }
  let limits = [];
  const perToken = path.join(dir, `limits-${token.replace(/[^A-Za-z0-9-]/g, '_')}.json`);
  try {
    limits = JSON.parse(fs.readFileSync(fs.existsSync(perToken) ? perToken : limitsFile, 'utf8'));
  } catch {
    limits = [];
  }
  let skew = 0;
  try {
    skew = Number(JSON.parse(fs.readFileSync(skewFile, 'utf8')).seconds) || 0;
  } catch {
    skew = 0;
  }
  response.writeHead(200, { 'Content-Type': 'application/json', Date: new Date(Date.now() + skew * 1000).toUTCString() });
  response.end(JSON.stringify({ five_hour: null, seven_day: null, limits }));
});

process.on('disconnect', () => process.exit(0));

server.listen(0, '127.0.0.1', () => {
  fs.writeFileSync(portFile, String(server.address().port));
});
