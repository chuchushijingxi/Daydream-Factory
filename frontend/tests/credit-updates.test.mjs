import test from 'node:test';
import assert from 'node:assert/strict';
import { createHttpClient } from '../src/api/client.ts';
import { createAdministrationApi } from '../src/api/administration.ts';
import { SessionBoundary } from '../src/api/session-boundary.ts';

test('credit SSE shares Cookie transport and parses split frames without treating keepalive as money', async () => {
  let controller, request;
  const boundary = new SessionBoundary(),
    changes = [];
  const api = createAdministrationApi(
    createHttpClient(async (url, options) => {
      request = { url, options };
      return new Response(
        new ReadableStream({
          start(c) {
            controller = c;
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream; charset=utf-8' } },
      );
    }, boundary),
  );
  const pending = assert.rejects(
    api.watchCredits(null, () => changes.push('reread'), new AbortController().signal),
    (e) => e.code === 'CREDIT_STREAM_CLOSED',
  );
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(request.url, '/api/credits/events');
  assert.equal(request.options.credentials, 'same-origin');
  assert.equal(new Headers(request.options.headers).get('Accept'), 'text/event-stream');
  const encoder = new TextEncoder();
  for (const text of [
    'event: credits.re',
    'ady\r\ndata: {}\r\n',
    '\r\n: keepalive\n\nevent: credits.changed\ndata: {',
    '}\n\n',
  ])
    controller.enqueue(encoder.encode(text));
  controller.close();
  await pending;
  assert.deepEqual(changes, ['reread', 'reread']);
});

test('operator stream selects the existing encoded account scope and rejects content-bearing frames', async () => {
  let url;
  const api = createAdministrationApi(
    createHttpClient(async (value) => {
      url = value;
      return new Response('event: credits.changed\ndata: {"balance":900}\n\n', {
        headers: { 'Content-Type': 'text/event-stream' },
      });
    }, new SessionBoundary()),
  );
  let applied = 0;
  await assert.rejects(
    api.watchCredits('a/b', () => applied++, new AbortController().signal),
    (e) => e.code === 'PROGRAM_FAILED',
  );
  assert.equal(url, '/api/operations/users/a%2Fb/credits/events');
  assert.equal(applied, 0);
  const html = createAdministrationApi(
    createHttpClient(async () => new Response('<html>login</html>'), new SessionBoundary()),
  );
  await assert.rejects(
    html.watchCredits(null, () => applied++, new AbortController().signal),
    (e) => e.code === 'PROGRAM_FAILED',
  );
  assert.equal(applied, 0);
});

test('SSE revocation and late chunks use the same session fence; stale events never refresh another account', async () => {
  const boundary = new SessionBoundary();
  let unauthorized = 0,
    controller,
    applied = 0;
  boundary.onUnauthorized = () => unauthorized++;
  const api = createAdministrationApi(
    createHttpClient(
      async () => Response.json({ code: 'SESSION_REPLACED', message: 'replaced' }, { status: 401 }),
      boundary,
    ),
  );
  await assert.rejects(
    api.watchCredits(null, () => applied++, new AbortController().signal),
    (e) => e.status === 401,
  );
  assert.equal(unauthorized, 1);
  assert.equal(applied, 0);
  const late = createAdministrationApi(
    createHttpClient(
      async () =>
        new Response(
          new ReadableStream({
            start(c) {
              controller = c;
            },
          }),
          { headers: { 'Content-Type': 'text/event-stream' } },
        ),
      boundary,
    ),
  );
  const pending = assert.rejects(
    late.watchCredits(null, () => applied++, new AbortController().signal),
    (e) => e.code === 'SESSION_CHANGED',
  );
  await new Promise((resolve) => setTimeout(resolve, 0));
  boundary.reset();
  controller.enqueue(new TextEncoder().encode('event: credits.changed\ndata: {}\n\n'));
  controller.close();
  await pending;
  assert.equal(applied, 0);
});
