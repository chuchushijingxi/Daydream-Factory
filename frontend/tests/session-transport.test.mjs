import test from 'node:test';
import assert from 'node:assert/strict';
import { createApi, createHttpClient } from '../src/api/client.ts';
import { createStoryboardApi } from '../src/api/storyboard-client.ts';
import {
  SessionBoundary,
  sessionBoundary,
  clearAccountDrafts,
} from '../src/api/session-boundary.ts';
import {
  formatInteger,
  formatCost,
  chargeFormula,
  integer,
  shanghaiInput,
} from '../src/pages/credits/credit-format.ts';
const deferred = () => {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
};
test('shared JSON transport sends protected Cookie, preserves 403, and invalidates only the current 401', async () => {
  const boundary = new SessionBoundary();
  let invalidated = 0,
    options;
  boundary.onUnauthorized = () => invalidated++;
  const denied = createHttpClient(async (_u, o) => {
    options = o;
    return Response.json({ code: 'SERVER_DENIAL', message: '无权访问' }, { status: 403 });
  }, boundary);
  await assert.rejects(
    denied.json('/api/private'),
    (e) => e.status === 403 && e.code === 'SERVER_DENIAL',
  );
  assert.equal(invalidated, 0);
  assert.equal(options.credentials, 'same-origin');
  const missing = createHttpClient(
    async () =>
      Response.json({ code: 'BACKEND_SESSION_CODE', message: '原会话已替换' }, { status: 401 }),
    boundary,
  );
  await assert.rejects(missing.json('/api/private'), (e) => e.status === 401);
  assert.equal(invalidated, 1);
  await assert.rejects(
    missing.json('/login', { authentication: 'public' }),
    (e) => e.status === 401,
  );
  assert.equal(invalidated, 1);
});
test('A late JSON result, body and old unauthorized response cannot populate or invalidate B', async () => {
  for (const status of [200, 401]) {
    const body = deferred(),
      boundary = new SessionBoundary();
    let invalidated = 0;
    boundary.onUnauthorized = () => invalidated++;
    const client = createHttpClient(
      async () => ({ ok: status === 200, status, json: () => body.promise }),
      boundary,
    );
    const pending = client.json('/private');
    await Promise.resolve();
    boundary.reset();
    body.resolve({ owner: 'A' });
    await assert.rejects(pending, (e) => e.code === 'SESSION_CHANGED');
    assert.equal(invalidated, 0);
  }
});
test('binary parsing, uploads and streams use the same session boundary, abort and late-chunk checks', async () => {
  const boundary = new SessionBoundary(),
    body = deferred();
  let signal;
  const client = createHttpClient(async (_u, o) => {
    signal = o.signal;
    return new Response('binary');
  }, boundary);
  const pending = client.binary('/binary', undefined, () => body.promise);
  await Promise.resolve();
  boundary.reset();
  body.resolve(new Blob(['old']));
  await assert.rejects(pending, (e) => e.code === 'SESSION_CHANGED');
  assert.equal(signal.aborted, true);
  const streamBoundary = new SessionBoundary();
  let controller,
    started = deferred();
  const chunks = [];
  const streaming = createHttpClient(
    async () =>
      new Response(
        new ReadableStream({
          start(c) {
            controller = c;
            started.resolve();
          },
        }),
      ),
    streamBoundary,
  );
  const consume = streaming.stream('/stream', undefined, (c) =>
    chunks.push(new TextDecoder().decode(c)),
  );
  await started.promise;
  controller.enqueue(new TextEncoder().encode('first'));
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(chunks, ['first']);
  streamBoundary.reset();
  controller.enqueue(new TextEncoder().encode('old'));
  controller.close();
  await assert.rejects(consume, (e) => e.code === 'SESSION_CHANGED');
  assert.deepEqual(chunks, ['first']);
  const unauthorized = createHttpClient(
    async () => Response.json({ code: 'EXPIRED' }, { status: 401 }),
    new SessionBoundary(),
  );
  await assert.rejects(
    unauthorized.binary('/download', undefined, (r) => r.blob()),
    (e) => e.status === 401,
  );
  await assert.rejects(
    unauthorized.stream('/events', undefined, () => {}),
    (e) => e.status === 401,
  );
});
test('business clients use shared errors, and no transport retry resends an ambiguous write', async () => {
  for (const create of [createApi, createStoryboardApi]) {
    let count = 0;
    const client = create(async () => {
      count++;
      return Response.json({ code: 'ROLE_DENIED' }, { status: 403 });
    });
    await assert.rejects(
      create === createApi ? client.getProject('p') : client.getIndex('p'),
      (e) => e.status === 403 && e.code === 'ROLE_DENIED',
    );
    assert.equal(count, 1);
  }
  let count = 0;
  const api = createApi(async () => {
    count++;
    throw new Error('lost');
  });
  await assert.rejects(api.createProject({}), (e) => e.code === 'NETWORK');
  assert.equal(count, 1);
  const upload = createApi(async () => Response.json({ code: 'SESSION_EXPIRED' }, { status: 401 }));
  await assert.rejects(upload.uploadFile(new File(['x'], 'fiction.txt')), (e) => e.status === 401);
  sessionBoundary.reset();
});
test('account cleanup removes only owned drafts, and integer/currency display is lossless', () => {
  const values = new Map([
    ['theme', 'dark'],
    ['drama:session-owner', 'old'],
    ['other-app', 'keep'],
    ['media-command:project', 'private'],
    ['image-batch:project', 'private batch'],
    ['drama:mock-command:m', 'private'],
    ['drama:credit-command:u', 'private'],
  ]);
  const storage = {
    get length() {
      return values.size;
    },
    key: (i) => [...values.keys()][i],
    removeItem: (key) => values.delete(key),
  };
  clearAccountDrafts(storage);
  assert.deepEqual([...values.keys()], ['theme', 'other-app']);
  assert.equal(formatInteger('9223372036854775807'), '9,223,372,036,854,775,807');
  assert.equal(formatCost('0.0000012300'), '¥0.0000012300');
  assert.equal(formatCost(null), '待确认');
  for (const bad of ['1.2', '-1', '1e3', '01', '9007199254740993.0', ''])
    assert.throws(() => integer(bad, true));
  assert.equal(
    chargeFormula({ quantity: '12345', unit: 'characters', unitPrice: '2', original: '247' }),
    '12,345 字 × 2 积分/100 字＝247 积分',
  );
  assert.equal(shanghaiInput('2026-10-16T10:00'), '2026-10-16T02:00:00.000Z');
});
test('project media client shares authentication errors and rejects cross-project or secret-bearing media links', async () => {
  const { projectMediaApi } = await import('../src/api/project-media.ts');
  const previous = globalThis.fetch;
  let requests = 0;
  try {
    globalThis.fetch = async () => {
      requests++;
      return Response.json({ code: 'ROLE_FORBIDDEN' }, { status: 403 });
    };
    await assert.rejects(projectMediaApi.get('p', 't'), (e) => e.status === 403);
    assert.equal(requests, 1);
    globalThis.fetch = async () =>
      Response.json({
        projectId: 'p',
        id: 't',
        target: { assetId: 'a' },
        status: 'succeeded',
        revision: 1,
        credit: null,
        result: {
          files: [{ id: 'f', contentUrl: '/api/projects/other/media-tasks/t/outputs/f/file' }],
        },
      });
    await assert.rejects(
      projectMediaApi.get('p', 't'),
      (e) => e.code === 'CONTENT_BOUNDARY_VIOLATION',
    );
    globalThis.fetch = async () =>
      Response.json({
        projectId: 'p',
        id: 't',
        target: { assetId: 'a' },
        status: 'succeeded',
        revision: 1,
        credit: null,
        result: {
          files: [
            {
              id: 'f',
              contentUrl: '/api/projects/p/media-tasks/t/outputs/f/file',
              thumbnailUrl: '/api/files/f/thumbnail?secret=fixture',
            },
          ],
        },
      });
    await assert.rejects(
      projectMediaApi.get('p', 't'),
      (e) => e.code === 'CONTENT_BOUNDARY_VIOLATION',
    );
  } finally {
    globalThis.fetch = previous;
  }
});
test('a rejected stale quote can be refreshed without trapping a never-created project in unknown readback', async () => {
  const { needsProjectReadback, projectRequestMessage, authenticationMessage } = await import(
    '../src/api/errors.ts'
  );
  const error = { status: 409, code: 'BILLING_BASIS_CHANGED' };
  assert.equal(needsProjectReadback(error), false);
  assert.match(
    projectRequestMessage(error, (x) => x),
    /报价/,
  );
  assert.match(authenticationMessage({ code: 'MAIL_UNAVAILABLE' }), /暂时无法发送/);
  assert.equal(needsProjectReadback({ status: 409, code: 'CONFLICT' }), true);
});
