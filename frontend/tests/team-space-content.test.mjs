import test from 'node:test';
import assert from 'node:assert/strict';
import { teamBalanceView, teamLedgerView } from '../src/api/team-credits.ts';
import { sharedResourceApi } from '../src/api/shared-resource.ts';
import { projectMediaApi } from '../src/api/project-media.ts';
import { mediaLibraryApi } from '../src/api/media-library.ts';

test('team wallet and member ledger preserve holder privacy without personal balance synthesis', () => {
  assert.deepEqual(teamBalanceView({ spaceId: 'team', role: 'member', balance: 12 }), {
    spaceId: 'team',
    role: 'member',
    balance: '12',
  });
  for (const raw of [
    { spaceId: 'team', role: 'member', balance: 12, totalBalance: 500 },
    {
      spaceId: 'team',
      role: 'owner',
      balance: 50,
      totalBalance: 50,
      allocated: 20,
      unallocated: 29,
    },
    { spaceId: 'team', role: 'member', balance: Number.MAX_SAFE_INTEGER + 1 },
    // 管理员的 balance 是本人名下分配额，不能超过已分配总额。
    { spaceId: 'team', role: 'admin', balance: 21, totalBalance: 50, allocated: 20, unallocated: 30 },
  ])
    assert.throws(() => teamBalanceView(raw));
  assert.deepEqual(
    teamBalanceView({
      spaceId: 'team',
      role: 'admin',
      balance: 15,
      totalBalance: 50,
      allocated: 20,
      unallocated: 30,
    }),
    {
      spaceId: 'team',
      role: 'admin',
      balance: '15',
      totalBalance: '50',
      allocated: '20',
      unallocated: '30',
    },
  );
  const item = {
    id: 'entry',
    kind: 'deduct',
    amount: 3,
    reason: 'generation',
    createdAt: '2026-10-20T00:00:00Z',
  };
  const ledger = teamLedgerView(
    {
      items: [item],
      page: 1,
      pageSize: 20,
      total: 1,
      allocations: {
        items: [
          { id: 'allocated', kind: 'allocate', amount: 12, balance: 12, createdAt: item.createdAt },
        ],
        page: 1,
        pageSize: 20,
        total: 1,
      },
    },
    true,
  );
  assert.equal('balance' in ledger.items[0], false);
  assert.equal(ledger.allocations.items[0].balance, '12');
  assert.throws(() =>
    teamLedgerView({ items: [{ ...item, balance: 500 }], page: 1, pageSize: 20, total: 1 }, true),
  );
});
test('removing sharing verifies the command receipt then rereads current server sharing', async () => {
  const previous = globalThis.fetch;
  const calls = [];
  const sharing = {
    spaceId: 'team',
    resourceKind: 'project',
    resourceId: 'project',
    shares: [],
    canManage: true,
  };
  try {
    globalThis.fetch = async (url, options) => {
      calls.push({ url, options });
      return Response.json(options.method === 'DELETE' ? { removed: true } : sharing);
    };
    assert.deepEqual(await sharedResourceApi.deleteShare('team', 'project', 'member'), sharing);
    assert.equal(calls.length, 2);
    assert.equal(calls[0].options.method, 'DELETE');
    assert.equal(calls[1].options.method, undefined);
    calls.length = 0;
    globalThis.fetch = async (url, options) => {
      calls.push({ url, options });
      return Response.json({ removed: false });
    };
    await assert.rejects(
      sharedResourceApi.deleteShare('team', 'project', 'member'),
      (e) => e.code === 'PROGRAM_FAILED',
    );
    assert.equal(calls.length, 1);
    calls.length = 0;
    globalThis.fetch = async (url, options) => {
      calls.push({ url, options });
      if (options.method === 'DELETE') return Response.json({ removed: true });
      return Response.json({ code: 'UNAVAILABLE', message: 'offline' }, { status: 503 });
    };
    await assert.rejects(
      sharedResourceApi.deleteShare('team', 'project', 'member'),
      (e) => e.status === 503,
    );
    assert.equal(calls.length, 2);
  } finally {
    globalThis.fetch = previous;
  }
});
test('independent creation assets require their own protected route and real list metadata', async () => {
  const previous = globalThis.fetch;
  let seen = '';
  const item = {
    spaceId: 'team',
    ownerId: 'assignee',
    creatorId: 'creator',
    creatorName: 'Creator',
    sharedWithMe: false,
    effectiveAccess: 1,
    taskId: 'task',
    outputId: 'output',
    projectId: 'hidden-project',
    projectTitle: 'Title',
    kind: 'image',
    createdAt: '2026-10-20T00:00:00Z',
    modelId: 'model',
    requestModelId: 'model',
    aspectRatio: '1:1',
    resolution: '1K',
    width: 10,
    height: 10,
    durationSeconds: 0,
    contentType: 'image/png',
    sizeBytes: 100,
    promptPreview: 'Prompt',
    contentUrl: '/api/media-library/task/outputs/output/file',
    thumbnailUrl: '/api/media-library/task/outputs/output/thumbnail',
  };
  try {
    globalThis.fetch = async (url) => {
      seen = url;
      return Response.json({ items: [item], nextCursor: null });
    };
    const list = await mediaLibraryApi.list('image', '', undefined, 'mine');
    assert.equal(new URL(seen, 'http://test').searchParams.get('filter'), 'mine');
    assert.equal(list.items[0].creatorName, 'Creator');
    globalThis.fetch = async () =>
      Response.json({
        items: [{ ...item, thumbnailUrl: '/api/files/private/thumbnail' }],
        nextCursor: null,
      });
    await assert.rejects(mediaLibraryApi.list(), (e) => e.code === 'CONTENT_BOUNDARY_VIOLATION');
  } finally {
    globalThis.fetch = previous;
  }
});

test('project image and video thumbnails stay inside the authenticated task output route', async () => {
  const previous = globalThis.fetch;
  const prefix = '/api/projects/project/media-tasks/task/outputs/output';
  const task = {
    projectId: 'project',
    id: 'task',
    kind: 'image',
    target: { assetId: 'asset' },
    status: 'succeeded',
    revision: 1,
    credit: null,
    result: {
      files: [{ id: 'output', contentUrl: prefix + '/file', thumbnailUrl: prefix + '/thumbnail' }],
    },
  };
  try {
    for (const kind of ['image', 'video']) {
      globalThis.fetch = async () => Response.json({ ...task, kind });
      assert.equal(
        (await projectMediaApi.get('project', 'task')).result.files[0].thumbnailUrl,
        prefix + '/thumbnail',
      );
    }
    for (const thumbnailUrl of [
      '/api/files/private/thumbnail',
      prefix.replace(/\/output$/, '/other') + '/thumbnail',
      prefix + '/thumbnail?fileId=private',
    ]) {
      globalThis.fetch = async () =>
        Response.json({ ...task, result: { files: [{ ...task.result.files[0], thumbnailUrl }] } });
      await assert.rejects(
        projectMediaApi.get('project', 'task'),
        (error) => error.code === 'CONTENT_BOUNDARY_VIOLATION',
      );
    }
  } finally {
    globalThis.fetch = previous;
  }
});
