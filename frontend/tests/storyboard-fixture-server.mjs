import http from 'node:http';
import { readFile } from 'node:fs/promises';
import { resolve, extname } from 'node:path';
import { createStoryboardFixture } from './storyboard-fixture.mjs';
const root = resolve(import.meta.dirname, '../..'),
  fixture = createStoryboardFixture();
const server = http.createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://127.0.0.1');
    if (url.pathname.startsWith('/__fixture/')) {
      const action = url.pathname.slice(11);
      if (action === 'complete') fixture.complete();
      else fixture.setScenario(action);
      res.writeHead(200, { 'Content-Type': 'text/html;charset=utf-8' });
      res.end(
        '<p>免费隔离夹具控制已应用。<a href="/demo/app/#/playlet/p5_free/production">返回工作区</a></p>',
      );
      return;
    }
    if (url.pathname === '/__requests') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify(fixture.requests, null, 2));
      return;
    }
    if (url.pathname.startsWith('/api/')) {
      const chunks = [];
      for await (const chunk of req) chunks.push(chunk);
      const response = await fixture.fetch(url.pathname, {
        method: req.method,
        body: chunks.length ? Buffer.concat(chunks).toString() : undefined,
      });
      res.writeHead(response.status, {
        'Content-Type': 'application/json',
        'X-Test-Only': 'isolated-free-fixture',
      });
      res.end(await response.text());
      return;
    }
    const path = resolve(
      root,
      '.' + (url.pathname.endsWith('/') ? url.pathname + 'index.html' : url.pathname),
    );
    if (!path.startsWith(root + '/demo/')) {
      res.writeHead(404);
      res.end();
      return;
    }
    const data = await readFile(path);
    res.writeHead(200, {
      'Content-Type':
        {
          '.html': 'text/html;charset=utf-8',
          '.js': 'text/javascript',
          '.mjs': 'text/javascript',
          '.css': 'text/css',
          '.svg': 'image/svg+xml',
          '.png': 'image/png',
          '.jpg': 'image/jpeg',
          '.ttf': 'font/ttf',
        }[extname(path)] || 'application/octet-stream',
      'Cache-Control': 'no-store',
    });
    res.end(data);
  } catch (e) {
    res.writeHead(500, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ code: 'FIXTURE_FAILURE', message: e.message }));
  }
});
server.listen(0, '127.0.0.1', () =>
  console.log(
    JSON.stringify({
      url: `http://127.0.0.1:${server.address().port}/demo/app/#/playlet/p5_free/production`,
      isolation: 'free fixture only; no model, credentials or PG',
    }),
  ),
);
