import test from 'node:test';
import assert from 'node:assert/strict';
import { createApi } from '../src/api/client.ts';

const fixture = (fetch) => createApi(fetch);
test('image association routes use parent IDs, revision and file ID with abort signal and verified receipt', async () => {
  const fileId = 'file_' + 'b'.repeat(32),
    image = {
      fileId,
      originalUrl: '/api/files/' + fileId + '/original',
      thumbnailUrl: '/api/files/' + fileId + '/thumbnail',
    };
  for (const target of [{ assetId: 'asset /' }, { assetId: 'asset /', lookId: 'look /' }]) {
    let call;
    const controller = new AbortController();
    const result = {
      id: 'project /',
      revision: 8,
      assets: [{ id: target.assetId, image, looks: [{ id: target.lookId || 'unused', image }] }],
    };
    const api = fixture(async (url, options) => {
      call = { url, options };
      return Response.json(result);
    });
    assert.deepEqual(
      await api.saveAssetImage('project /', target, { revision: 7, fileId }, controller.signal),
      result,
    );
    assert.equal(
      call.url,
      '/api/projects/project%20%2F/assets/asset%20%2F/' +
        (target.lookId ? 'looks/look%20%2F/' : '') +
        'image',
    );
    assert.ok(call.options.signal instanceof AbortSignal);
    assert.deepEqual(JSON.parse(call.options.body), { revision: 7, fileId });
  }
  let calls = 0;
  const api = fixture(async () => {
    calls++;
    return Response.json({ id: 'p', revision: 8, assets: [] });
  });
  await assert.rejects(
    api.saveAssetImage('p', { assetId: 'a' }, { revision: 7, fileId }),
    (e) => e.code === 'PROGRAM_FAILED',
  );
  assert.equal(calls, 1);
  for (const assets of [12, {}, [null], [{ id: 'a', looks: {} }]]) {
    const bad = fixture(async () => Response.json({ id: 'p', revision: 8, assets }));
    await assert.rejects(
      bad.saveAssetImage('p', { assetId: 'a', lookId: 'l' }, { revision: 7, fileId }),
      (e) => e.code === 'PROGRAM_FAILED',
    );
  }
});
test('explicit task requests use exact routes and revision; HTTP 200 failed is preserved', async () => {
  const requests = [],
    failed = {
      id: 'p',
      runs: [{ contract: 'visual-prompt-agent-v1', status: 'failed', callCount: 0 }],
    };
  const api = fixture(async (url, options) => {
    requests.push({ url, options });
    return { ok: true, status: 200, json: async () => failed };
  });
  for (const [method, suffix] of [
    ['startExtraction', 'runs'],
    ['startVisualPrompts', 'visual-prompt-runs'],
    ['cancelTask', 'cancel'],
  ]) {
    assert.equal(await api[method]('project /', { revision: 8 }), failed);
    const r = requests.at(-1);
    assert.equal(r.url, '/api/projects/project%20%2F/' + suffix);
    assert.equal(r.options.method, 'POST');
    assert.deepEqual(JSON.parse(r.options.body), { revision: 8 });
  }
  assert.equal(requests.length, 3);
  assert.equal(api.startOutline, undefined);
});
test('write network ambiguity, conflict and malformed success never auto-resend or masquerade as saved', async () => {
  for (const [fetch, code, status] of [
    [
      async () => {
        throw new TypeError('offline');
      },
      'NETWORK',
      0,
    ],
    [
      async () => ({
        ok: false,
        status: 409,
        json: async () => ({ code: 'CONFLICT', message: '项目已更新' }),
      }),
      'CONFLICT',
      409,
    ],
    [
      async () => ({
        ok: true,
        status: 200,
        json: async () => {
          throw new Error('invalid json');
        },
      }),
      'PROGRAM_FAILED',
      200,
    ],
  ]) {
    let count = 0;
    const api = fixture(async (...args) => {
      count++;
      return fetch(...args);
    });
    await assert.rejects(
      () => api.createLook('p', 'c', { revision: 9, look: { name: '晚宴', description: '' } }),
      (error) => error.code === code && error.status === status,
    );
    assert.equal(count, 1);
  }
});
test('project deletion uses its canonical command and requires a complete saved receipt without retries', async () => {
  let calls = 0;
  const received = [];
  const api = fixture(async (url, options) => {
    calls++;
    received.push({ url, body: JSON.parse(options.body) });
    return {
      ok: true,
      status: 200,
      json: async () => ({ id: 'project /', revision: 9, deletedAt: '2026-10-14T01:00:00Z' }),
    };
  });
  await api.deleteProject('project /', { revision: 8 });
  assert.deepEqual(received, [
    { url: '/api/projects/project%20%2F/delete', body: { revision: 8 } },
  ]);
  assert.equal(calls, 1);
  for (const receipt of [
    { id: 'other', revision: 9, deletedAt: 'date' },
    { id: 'p', revision: 8, deletedAt: 'date' },
    { id: 'p', revision: 9, deletedAt: null },
  ]) {
    let writes = 0;
    const malformed = fixture(async () => {
      writes++;
      return { ok: true, status: 200, json: async () => receipt };
    });
    await assert.rejects(
      () => malformed.deleteProject('p', { revision: 8 }),
      (e) => e.code === 'PROGRAM_FAILED',
    );
    assert.equal(writes, 1);
  }
});
test('single asset and look use exact encoded canonical routes and revision-only body', async () => {
  const requests = [];
  const api = fixture(async (url, options) => {
    requests.push({ url, body: JSON.parse(options.body) });
    return { ok: true, status: 202, json: async () => ({ id: 'p' }) };
  });
  await api.startSingleVisualPrompts('p /', { assetId: 'c /' }, { revision: 9 });
  await api.startSingleVisualPrompts('p /', { assetId: 'c /', lookId: 'l /' }, { revision: 10 });
  assert.deepEqual(requests, [
    { url: '/api/projects/p%20%2F/assets/c%20%2F/visual-prompt-runs', body: { revision: 9 } },
    {
      url: '/api/projects/p%20%2F/assets/c%20%2F/looks/l%20%2F/visual-prompt-runs',
      body: { revision: 10 },
    },
  ]);
});
test('general upload sends optional thumbnail bounds and project creation sends only the stored file ID', async () => {
  const id = 'file_' + 'b'.repeat(32),
    file = new File(['complete binary bytes'], 'reference.bin', {
      type: 'application/octet-stream',
    }),
    requests = [];
  const api = createApi(async (url, options) => {
    requests.push({ url, options });
    if (url === '/api/upload')
      return Response.json(
        {
          fileId: id,
          filename: file.name,
          sizeBytes: file.size,
          contentType: 'application/octet-stream',
          originalUrl: '/api/files/' + id + '/original',
          thumbnailUrl: '/api/files/' + id + '/thumbnail',
        },
        { status: 201 },
      );
    return Response.json({ id: 'project-new', revision: 1 }, { status: 201 });
  });
  const receipt = await api.uploadFile(file, {
    thumbnail: true,
    thumbnailMaxWidth: 320,
    thumbnailMaxHeight: 180,
  });
  assert.equal(requests[0].url, '/api/upload');
  assert.equal(requests[0].options.body.get('file').name, file.name);
  assert.equal(requests[0].options.body.get('thumbnail'), 'true');
  assert.equal(requests[0].options.body.get('thumbnailMaxWidth'), '320');
  assert.equal(requests[0].options.body.get('thumbnailMaxHeight'), '180');
  await api.createProject({ fileId: receipt.fileId, title: 'Stored script' });
  assert.equal(requests[1].url, '/api/projects');
  assert.equal(new Headers(requests[1].options.headers).get('Content-Type'), 'application/json');
  assert.deepEqual(JSON.parse(requests[1].options.body), { fileId: id, title: 'Stored script' });
});

test('malformed upload receipt is not accepted and does not create a project', async () => {
  let calls = 0;
  const api = createApi(async () => {
    calls++;
    return Response.json({ fileId: 'missing-urls' }, { status: 201 });
  });
  await assert.rejects(
    () => api.uploadFile(new File(['x'], 'x.txt')),
    (e) => e.code === 'PROGRAM_FAILED',
  );
  assert.equal(calls, 1);
});

test('manual prompts use exact target routes, preserve complete text and reject false receipts', async () => {
  for (const target of [{ assetId: 'a /' }, { assetId: 'a /', lookId: 'l /' }]) {
    let recorded;
    const text = '完整用户正文\n保留空格 ';
    const project = {
      id: 'p /',
      revision: 8,
      source: { version: 2 },
      visualPrompts: [{ target, text, origin: 'user', sourceVersion: 2, stale: false }],
    };
    const api = createApi(async (url, options) => {
      recorded = { url, options };
      return Response.json(project);
    });
    assert.deepEqual(await api.saveVisualPrompt('p /', target, { revision: 7, text }), project);
    assert.equal(
      recorded.url,
      '/api/projects/p%20%2F/assets/a%20%2F/' +
        (target.lookId ? 'looks/l%20%2F/' : '') +
        'visual-prompt',
    );
    assert.deepEqual(JSON.parse(recorded.options.body), { revision: 7, text });
  }
  for (const visualPrompts of [
    [],
    null,
    {},
    [null],
    [{ target: { assetId: 'a' }, text: 'wrong', origin: 'user', sourceVersion: 1, stale: false }],
  ]) {
    let calls = 0;
    const api = createApi(async () => {
      calls++;
      return Response.json({ id: 'p', revision: 2, source: { version: 1 }, visualPrompts });
    });
    await assert.rejects(
      api.saveVisualPrompt('p', { assetId: 'a' }, { revision: 1, text: 'complete' }),
      (e) => e.code === 'PROGRAM_FAILED',
    );
    assert.equal(calls, 1);
  }
});
