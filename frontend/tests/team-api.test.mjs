import test from 'node:test';
import assert from 'node:assert/strict';
import { createTeamClient } from '../src/api/team-client.ts';
import { createHttpClient } from '../src/api/client.ts';
import { SessionBoundary } from '../src/api/session-boundary.ts';
import { createUsageApi } from '../src/api/model-usage-client.ts';
import {
  buildTeamPatch,
  shanghaiDraft,
} from '../src/pages/operations/teams/TeamContractFields.tsx';

function client(reply = () => Response.json({ accepted: true })) {
  const calls = [];
  const transport = createHttpClient(async (url, options) => {
    calls.push({ url, options, body: options.body ? JSON.parse(options.body) : undefined });
    return reply(url, options);
  }, new SessionBoundary());
  return { api: createTeamClient(transport), transport, calls };
}
test('team management uses the shared transport and exact path-owned contracts, with collection DELETE and stable allocation command', async () => {
  const { api, calls } = client();
  await api.revokeLink('team/a', 'invite-id');
  await api.allocate('team/a', 'member/b', 'reclaim', { commandId: 'fixed-command', all: true });
  await api.beginOwnership('team/a', 'member/b');
  await api.transfer('team/a', {
    fromUserId: 'former',
    toUserId: 'current',
    all: false,
    resources: [{ kind: 'upload', id: 'file-id' }],
  });
  assert.equal(calls[0].url, '/api/spaces/team%2Fa/invite-links');
  assert.equal(calls[0].options.method, 'DELETE');
  assert.deepEqual(calls[0].body, { inviteId: 'invite-id' });
  assert.equal(calls[1].url, '/api/spaces/team%2Fa/members/member%2Fb/allocation');
  assert.deepEqual(calls[1].body, { kind: 'reclaim', commandId: 'fixed-command', all: true });
  assert.deepEqual(calls[2].body, { userId: 'member/b' });
  assert.equal(calls[3].body.resources[0].kind, 'upload');
  assert.ok(
    calls.every(
      (call) => call.options.credentials === 'same-origin' && call.options.cache === 'no-store',
    ),
  );
  assert.ok(calls.every((call) => !('actor' in (call.body || {}))));
});
test('unknown writes are never retried, unsafe numbers are rejected, and model candidates use actual config metadata', async () => {
  const broken = client(() => {
    throw new Error('offline');
  });
  await assert.rejects(
    broken.api.openTeam({
      name: 'Team',
      ownerId: 'owner',
      memberLimit: 5,
      expiresAt: '2027-01-01T00:00:00Z',
      grantAmount: 0,
      modelDiscounts: [],
    }),
    (error) => error.code === 'NETWORK',
  );
  assert.equal(broken.calls.length, 1);
  await assert.rejects(
    client(() => Response.json({ balance: Number.MAX_SAFE_INTEGER + 1 })).api.detail('team'),
  );
  const models = client(() =>
    Response.json({
      connections: [
        {
          id: 'c',
          name: 'Connection',
          secret: 'never-render',
          models: [{ id: 'm', name: 'Model', pricing: { inputCnyPerMillion: 0.15 } }],
        },
      ],
    }),
  );
  assert.deepEqual(await models.api.models(), [{ id: 'm', name: 'Model · Connection' }]);
  assert.equal(models.calls[0].url, '/api/model-config');
});
test('operations contract patch leaves exact expiry untouched when changing unrelated settings and never calculates grant dates', () => {
  const saved = {
    id: 'team',
    ownerId: 'owner',
    revision: 9,
    memberLimit: 5,
    grantAmount: 400,
    expiresAt: '2027-01-01T02:03:47.123Z',
    disabled: false,
    modelDiscounts: [],
  };
  const draft = {
    memberLimit: '6',
    grantAmount: '400',
    expiresAt: shanghaiDraft(saved.expiresAt),
    modelDiscounts: [],
  };
  assert.deepEqual(buildTeamPatch(saved, draft, 'owner', false), { revision: 9, memberLimit: 6 });
  const renewal = buildTeamPatch(
    saved,
    { ...draft, memberLimit: '5', expiresAt: '2027-02-01T10:00' },
    'new-owner',
    true,
  );
  assert.deepEqual(renewal, {
    revision: 9,
    expiresAt: '2027-02-01T02:00:00.000Z',
    ownerId: 'new-owner',
    disabled: true,
  });
  assert.equal('nextGrantAt' in renewal, false);
  assert.throws(() =>
    buildTeamPatch(saved, { ...draft, memberLimit: '9007199254740992' }, 'owner', false),
  );
});
test('model usage forwards a team filter and preserves only the safe space metadata projection', async () => {
  const record = {
    id: 'task',
    taskId: 'task',
    userId: 'user',
    username: 'Creator',
    email: null,
    spaceId: 'team',
    spaceName: 'Team',
    spaceKind: 'team',
    operation: 'image.generate',
    status: 'succeeded',
    modelId: 'model',
    connectionId: 'connection',
    requestId: null,
    createdAt: '2026-10-20T00:00:00Z',
    durationMs: null,
    inputTokens: null,
    outputTokens: null,
    cacheHitTokens: null,
    mediaUsage: null,
    knownCostMicroCny: 0,
    unknownCost: false,
    callCount: 0,
    basis: null,
    prepaid: 0,
    refunded: 0,
    charged: 0,
    settlement: 'settled',
    deductionId: null,
    refundId: null,
    errorCode: null,
    prompt: 'must-not-leak',
  };
  const fixture = client(() =>
    Response.json({
      items: [record],
      total: 1,
      page: 1,
      pageSize: 20,
      stats: {
        count: 1,
        callCount: 0,
        charged: 0,
        refunded: 0,
        knownCostMicroCny: 0,
        unknownCost: false,
      },
      readAt: record.createdAt,
    }),
  );
  const usage = createUsageApi(fixture.transport);
  const result = await usage.usage('tasks', {
    start: '',
    end: '',
    search: '',
    model: '',
    connection: '',
    business: '',
    result: '',
    spaceId: 'team',
  });
  assert.equal(
    new URL(fixture.calls[0].url, 'http://test.invalid').searchParams.get('spaceId'),
    'team',
  );
  assert.equal(result.items[0].spaceName, 'Team');
  assert.equal(result.items[0].spaceKind, 'team');
  assert.equal(JSON.stringify(result).includes('must-not-leak'), false);
});
