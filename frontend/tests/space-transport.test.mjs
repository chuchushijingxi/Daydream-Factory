import test, { afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { createHttpClient, protectedResourceUrl } from '../src/api/client.ts';
import { SessionBoundary } from '../src/api/session-boundary.ts';
import {
  spaceSelection,
  safeReturnTo,
  SpacePreferenceOrder,
  parseSpacePreference,
  persistSpacePreference,
} from '../src/api/space-selection.ts';
import { notificationTarget } from '../src/api/notifications-client.ts';
const deferred = () => {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
};
afterEach(() => spaceSelection.select(''));
test('space selection fences late JSON and unauthorized bodies without ending the login', async () => {
  for (const status of [200, 401]) {
    spaceSelection.select('team-a');
    const boundary = new SessionBoundary(),
      body = deferred();
    let unauthorized = 0,
      headers,
      signal;
    boundary.onUnauthorized = () => unauthorized++;
    const transport = createHttpClient(async (_url, options) => {
      headers = options.headers;
      signal = options.signal;
      return { ok: status === 200, status, json: () => body.promise };
    }, boundary);
    const pending = transport.json('/api/projects');
    await Promise.resolve();
    assert.equal(headers.get('X-Drama-Space'), 'team-a');
    spaceSelection.select('team-b');
    body.resolve({ code: 'AUTH_REQUIRED', items: ['private-a'] });
    await assert.rejects(pending, (e) => e.code === 'SPACE_CHANGED');
    assert.equal(signal.aborted, true);
    assert.equal(unauthorized, 0);
    assert.equal(boundary.epoch, 0);
  }
});
test('space generation separates identical GETs and all binary/stream late adoption', async () => {
  spaceSelection.select('a');
  const delayed = deferred();
  let calls = 0;
  const client = createHttpClient(async (_url, options) => {
    calls++;
    return options.headers.get('X-Drama-Space') === 'a'
      ? delayed.promise
      : Response.json({ space: 'b' });
  }, new SessionBoundary());
  const old = client.json('/api/projects');
  const duplicate = client.json('/api/projects');
  assert.equal(calls, 1);
  spaceSelection.select('b');
  assert.deepEqual(await client.json('/api/projects'), { space: 'b' });
  assert.equal(calls, 2);
  delayed.resolve(Response.json({ space: 'a' }));
  assert.deepEqual(
    (await Promise.allSettled([old, duplicate])).map((v) => v.status),
    ['rejected', 'rejected'],
  );
  const raw = deferred();
  const binary = createHttpClient(
    async () => new Response('original'),
    new SessionBoundary(),
  ).binary('/api/files/file/original', undefined, () => raw.promise);
  await Promise.resolve();
  spaceSelection.select('c');
  raw.resolve(new Blob(['old']));
  await assert.rejects(binary, (e) => e.code === 'SPACE_CHANGED');
  let streamController;
  const chunks = [];
  const streaming = createHttpClient(
    async () =>
      new Response(
        new ReadableStream({
          start(c) {
            streamController = c;
          },
        }),
      ),
    new SessionBoundary(),
  );
  const consume = streaming.stream('/api/credits/events', undefined, (c) =>
    chunks.push(new TextDecoder().decode(c)),
  );
  await new Promise((r) => setTimeout(r, 0));
  streamController.enqueue(new TextEncoder().encode('current'));
  await new Promise((r) => setTimeout(r, 0));
  spaceSelection.select('d');
  streamController.enqueue(new TextEncoder().encode('late'));
  streamController.close();
  await assert.rejects(consume, (e) => e.code === 'SPACE_CHANGED');
  assert.deepEqual(chunks, ['current']);
});
test('profile reads survive selection and keepalive release is sent but its late result is ignored', async () => {
  spaceSelection.select('a');
  const body = deferred();
  let signal;
  const client = createHttpClient(async (_url, options) => {
    signal = options.signal;
    return body.promise;
  }, new SessionBoundary());
  const profile = client.json('/api/auth/me', { authentication: 'public' });
  spaceSelection.select('b');
  assert.equal(signal.aborted, false);
  body.resolve(Response.json({ user: 'same-account' }));
  assert.deepEqual(await profile, { user: 'same-account' });
  const release = deferred();
  const leaving = createHttpClient(async (_url, options) => {
    signal = options.signal;
    return release.promise;
  }, new SessionBoundary()).json('/api/uploads/u', {
    method: 'DELETE',
    keepalive: true,
  });
  spaceSelection.select('c');
  assert.equal(signal.aborted, false);
  release.resolve(Response.json({ released: true }));
  await assert.rejects(leaving, (e) => e.code === 'SPACE_CHANGED');
});
test('native protected links carry selection while internal auth return and notification targets stay safe', () => {
  spaceSelection.select('team/a');
  assert.equal(
    protectedResourceUrl('/api/files/f/original?download=1'),
    '/api/files/f/original?download=1&audience=workspace&space=team%2Fa',
  );
  assert.equal(
    protectedResourceUrl('/api/files/f/original', 'operations'),
    '/api/files/f/original?audience=operations',
  );
  for (const value of [
    'https://outside.invalid',
    '//outside.invalid',
    '/\\outside.invalid',
    '/bad\npath',
  ])
    assert.equal(safeReturnTo(value), '');
  assert.equal(safeReturnTo('/invite/valid-token?next=1'), '/invite/valid-token?next=1');
  assert.deepEqual(
    notificationTarget({
      type: 'invite',
      spaceId: 'not-yet-joined',
      targetId: 'invite',
    }),
    { path: '/invites?inviteId=invite', switchSpace: false },
  );
  assert.deepEqual(
    notificationTarget({
      type: 'ownership_transfer',
      spaceId: 'team',
      targetId: 'transfer',
    }),
    { path: '/team/team/ownership/transfer', switchSpace: false },
  );
  assert.deepEqual(notificationTarget({ type: 'project', spaceId: 'team', targetId: 'p' }), {
    path: '/playlet/p/outline',
    switchSpace: true,
  });
  assert.equal(
    notificationTarget({
      type: 'https://outside.invalid',
      spaceId: 'team',
      targetId: 'x',
    }),
    null,
  );
});

test('space shell projects only declared wire facts and rejects invented authority', async () => {
  const { createSpacesApi } = await import('../src/api/spaces.ts');
  const row = {
    id: 'user',
    kind: 'personal',
    name: '个人空间',
    role: 'owner',
    memberCount: 1,
    memberLimit: 1,
    status: 'active',
    readOnly: false,
  };
  const client = createSpacesApi(
    createHttpClient(
      async () =>
        Response.json([
          { ...row, passwordHash: 'fixture-only-not-a-real-secret', team: { privateExtra: true } },
        ]),
      new SessionBoundary(),
    ),
  );
  assert.deepEqual(await client.list(), [row]);
  for (const patch of [
    { role: 'operator' },
    { readOnly: 'false' },
    { memberLimit: -1 },
    { expiresAt: 0 },
  ]) {
    const bad = createSpacesApi(
      createHttpClient(async () => Response.json([{ ...row, ...patch }]), new SessionBoundary()),
    );
    await assert.rejects(bad.list(), (e) => e.code === 'PROGRAM_FAILED');
  }
});

test('simultaneous tabs converge under crossed broadcasts and reversed storage writes', () => {
  for (const delivery of [
    ['left', 'right'],
    ['right', 'left'],
  ]) {
    const key = 'selection',
      values = new Map();
    const storage = {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => values.set(key, value),
    };
    const initial = { spaceId: 'personal', counter: 1, tabId: 'initial' };
    const left = new SpacePreferenceOrder('tab-a'),
      right = new SpacePreferenceOrder('tab-b');
    left.receive(initial);
    right.receive(initial);
    const leftChoice = left.select('team-a'),
      rightChoice = right.select('team-b');
    assert.equal(leftChoice.counter, rightChoice.counter);
    // Simulate both processes reading old storage before either writes: the
    // losing record physically lands last, despite having the lower full order.
    storage.setItem(key, JSON.stringify(rightChoice));
    storage.setItem(key, JSON.stringify(leftChoice));
    const receive = (order, value) => {
      order.receive(value);
      order.receive(storage.getItem(key));
      persistSpacePreference(storage, key, order.current);
    };
    for (const destination of delivery) {
      if (destination === 'left') receive(left, rightChoice);
      else receive(right, leftChoice);
    }
    // Include duplicate/delayed events, as produced by the storage repair.
    receive(left, leftChoice);
    receive(right, rightChoice);
    assert.deepEqual(left.current, rightChoice);
    assert.deepEqual(right.current, rightChoice);
    assert.deepEqual(parseSpacePreference(storage.getItem(key)), rightChoice);
    const later = left.select('personal');
    assert.equal(later.counter, rightChoice.counter + 1);
    receive(right, later);
    assert.deepEqual(right.current, later);
    assert.equal(right.receive(rightChoice), null);
  }
});
