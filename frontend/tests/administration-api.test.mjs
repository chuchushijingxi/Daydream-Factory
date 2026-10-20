import test from 'node:test';
import assert from 'node:assert/strict';
import { createHttpClient } from '../src/api/client.ts';
import { createAdministrationApi, creditInput, safeInteger } from '../src/api/administration.ts';
import { SessionBoundary } from '../src/api/session-boundary.ts';
const user = {
  id: 'u1',
  email: 'fiction@example.invalid',
  username: 'fiction',
  role: 'user',
  status: 'enabled',
  createdAt: '2026-10-16T00:00:00Z',
};
const form = {
  username: '虚构创作者🌙',
  email: user.email,
  password: 'fiction-only-password',
  emailCode: '111111',
  registrationGrant: 'fiction-grant',
};
function service(reply) {
  const requests = [];
  const api = createAdministrationApi(
    createHttpClient(async (url, options) => {
      requests.push({
        url,
        options,
        body: typeof options.body === 'string' ? JSON.parse(options.body) : (options.body ?? null),
      });
      return reply(url, options);
    }, new SessionBoundary()),
  );
  return { api, requests };
}
test('recent credit preview reads only the bounded receipt list without page statistics', async () => {
  const receipt = {
    id: 'recent', operationId: 'recent-op', accountId: user.id, kind: 'add',
    amount: 12, balance: 12, category: 'gift', rechargeAmount: 0, giftAmount: 12,
    legacyAmount: 0, source: 'daily-gift', subject: 'system', reason: 'daily-gift',
    createdAt: user.createdAt,
  };
  const { api, requests } = service(() => Response.json({ items: [receipt], future: 'ignored' }));
  const items = await api.myRecentLedger();
  assert.equal(items.length, 1);
  assert.equal(items[0].amount, '12');
  assert.equal(items[0].identity, 'recent');
  assert.equal(requests[0].url, '/api/credits/ledger/recent');
  assert.equal(requests[0].options.credentials, 'same-origin');
  for (const invalid of [{}, { items: null }, { items: Array(6).fill(receipt) }, { items: [{ ...receipt, amount: 12.5 }] }]) {
    await assert.rejects(service(() => Response.json(invalid)).api.myRecentLedger(), /回执/);
  }
  assert.deepEqual(await service(() => Response.json({ items: [] })).api.myRecentLedger(), []);
});
test('auth methods match the sole backend DTO and Cookie transport, and never send client roles or identities', async () => {
  const { api, requests } = service((url) =>
    Response.json(
      url.endsWith('/register')
        ? user
        : url.endsWith('/options')
          ? { registrationRequiresCode: true, passwordLogin: true, codeLogin: true }
          : url.endsWith('/email-code')
            ? { submitted: true }
            : url.endsWith('/logout')
              ? { loggedOut: true }
              : url.endsWith('/reset-password')
                ? { updated: true }
                : { user, avatarUrl: null },
    ),
  );
  const current = await api.current();
  assert.equal(current.operationsAllowed, false);
  assert.equal(current.username, user.username);
  assert.equal((await api.authOptions()).registrationGrantRequired, true);
  await api.login('password', form, false);
  await api.login('email', form, false);
  await api.register(form);
  await api.sendCode(form.email, 'reset');
  await api.resetPassword(form);
  await api.logout();
  assert.deepEqual(requests.find((x) => x.url.endsWith('/login')).body, {
    email: form.email,
    password: form.password,
    audience: 'workspace',
  });
  assert.deepEqual(requests.filter((x) => x.url.endsWith('/login'))[1].body, {
    email: form.email,
    code: form.emailCode,
    audience: 'workspace',
  });
  assert.deepEqual(requests.find((x) => x.url.endsWith('/register')).body, {
    email: form.email,
    username: form.username,
    password: form.password,
    code: form.emailCode,
    authorizationCode: form.registrationGrant,
  });
  assert.ok(
    requests.every(
      (x) => x.options.credentials === 'same-origin' && x.options.cache === 'no-store',
    ),
  );
  assert.equal(
    requests.some((x) => 'role' in (x.body || {}) || 'userId' in (x.body || {})),
    false,
  );
  const notLogged = service(() => Response.json({ code: 'AUTH_REQUIRED' }, { status: 401 }));
  assert.equal(await notLogged.api.current(), null);
});
test('personal balance and ledger routes cannot be retargeted; amounts enforce the safe-integer contract before POST', async () => {
  const { api, requests } = service((url) =>
    Response.json(
      url.includes('/ledger')
        ? {
            items: [],
            total: 0,
            page: 1,
            pageSize: 20,
            summary: {
              acquired: 0,
              consumed: 0,
              refunded: 0,
              netConsumed: 0,
              settledConsumed: 0,
              pending: 0,
            },
            analysis: [],
          }
        : {
            balance: 9007199254740991,
            rechargeBalance: 9007199254740991,
            giftBalance: 0,
            legacyBalance: 0,
          },
    ),
  );
  assert.equal((await api.myCreditBalance()).balance, '9007199254740991');
  assert.equal((await api.myLedger()).items.length, 0);
  assert.equal(
    requests.some((x) => x.url.includes('userId')),
    false,
  );
  for (const invalid of ['1.1', '9007199254740992', '0', '01', '-1'])
    assert.throws(() => creditInput(invalid));
  for (const invalid of [9007199254740992, NaN, 1.1, -1]) assert.throws(() => safeInteger(invalid));
  assert.equal(creditInput('9007199254740991'), Number.MAX_SAFE_INTEGER);
  const invalid = service(() => {
    throw new Error('should not send');
  });
  await assert.rejects(
    invalid.api.adjust({
      account: 'u1',
      command: 'op',
      direction: 'add',
      amount: '9007199254740992',
      reason: 'fixture',
    }),
    (e) => e.code === 'CREDIT_INVALID_AMOUNT',
  );
  assert.equal(invalid.requests.length, 0);
});
test('credit write uses original operation ID and reason; 503 never auto-resubmits and 404 lookup stays distinct from failure', async () => {
  const receipt = {
    id: 'entry',
    operationId: 'same-op',
    accountId: 'u1',
    kind: 'add',
    category: 'gift',
    rechargeAmount: 0,
    giftAmount: 5,
    legacyAmount: 0,
    amount: 5,
    balance: 5,
    source: '运营调整',
    subject: 'operator1',
    reason: 'fixture',
    createdAt: user.createdAt,
  };
  let lookup = false;
  const { api, requests } = service((url) =>
    url.includes('credit-operations')
      ? Response.json(receipt)
      : Response.json({ code: 'CREDIT_COMMIT_UNKNOWN', message: '提交未确认' }, { status: 503 }),
  );
  await assert.rejects(
    api.adjust({
      account: 'u1',
      command: 'same-op',
      direction: 'add',
      category: 'gift',
      amount: '5',
      reason: 'fixture',
    }),
    (e) => e.code === 'CREDIT_COMMIT_UNKNOWN',
  );
  assert.equal(requests.length, 1);
  const result = await api.adjustment('same-op');
  assert.equal(result.state, 'committed');
  assert.equal(requests.length, 2);
  assert.deepEqual(requests[0].body, {
    kind: 'add',
    amount: 5,
    operationId: 'same-op',
    reason: 'fixture',
    category: 'gift',
  });
  assert.equal(lookup, false);
  assert.equal(
    (
      await service(() => Response.json({ code: 'NOT_FOUND' }, { status: 404 })).api.adjustment(
        'op',
      )
    ).state,
    'absent',
  );
  await assert.rejects(
    service(() => Response.json({ code: 'PROGRAM_FAILED' }, { status: 503 })).api.adjustment('op'),
    (e) => e.status === 503,
  );
});
test('server pagination and account/grant projection preserve semantics without returning grant plaintext history', async () => {
  const { api, requests } = service((url) =>
    Response.json(
      url.includes('authorization-codes')
        ? {
            items: [
              {
                id: 'code1',
                createdAt: user.createdAt,
                expiresAt: '2027-01-01T00:00:00Z',
                disabled: false,
                usedBy: null,
                usedAt: null,
              },
            ],
            total: 21,
            page: 2,
            pageSize: 20,
          }
        : { items: [user], total: 21, page: 2, pageSize: 20 },
    ),
  );
  const users = await api.users('fiction', '2');
  assert.equal(users.items[0].username, user.username);
  assert.equal(users.previous, '1');
  assert.equal(users.next, null);
  assert.ok(requests[0].url.includes('q=fiction'));
  assert.equal((await api.grants('2')).items[0].identity, 'code1');
  await assert.rejects(
    service(() =>
      Response.json({ items: [{ code: 'secret' }], total: 1, page: 1, pageSize: 20 }),
    ).api.grants(),
    (e) => e.code === 'PROGRAM_FAILED',
  );
});
test('profile, avatar and password consume the sole protected transport without client identities or external media URLs', async () => {
  const avatarUrl = '/api/auth/avatar?v=' + 'a'.repeat(64),
    profile = { user: { ...user, username: '真实回执名' }, avatarUrl };
  const { api, requests } = service((url, options) =>
    Response.json(
      url.endsWith('/password')
        ? { updated: true }
        : url.endsWith('/avatar') && options.method === 'DELETE'
          ? { ...profile, avatarUrl: null }
          : profile,
    ),
  );
  assert.equal((await api.saveProfile('真实回执名')).username, '真实回执名');
  assert.deepEqual(requests[0].body, { username: '真实回执名' });
  const file = new File(['fixture-only'], 'avatar.png', { type: 'image/png' });
  assert.equal((await api.uploadAvatar(file)).avatarUrl, avatarUrl);
  assert.ok(requests[1].body instanceof FormData);
  assert.deepEqual([...requests[1].body.keys()], ['file']);
  assert.equal(new Headers(requests[1].options.headers).has('Content-Type'), false);
  assert.equal(new Headers(requests[1].options.headers).get('X-Drama-Audience'), 'workspace');
  assert.equal((await api.removeAvatar()).avatarUrl, null);
  assert.equal(requests[2].options.method, 'DELETE');
  await api.changePassword('fiction-old', 'fiction-new-password');
  assert.deepEqual(requests[3].body, {
    currentPassword: 'fiction-old',
    password: 'fiction-new-password',
  });
  assert.ok(
    requests.every(
      (request) =>
        request.options.credentials === 'same-origin' &&
        request.options.cache === 'no-store' &&
        !request.url.includes('userId'),
    ),
  );
  for (const invalid of [
    'https://example.invalid/avatar.png',
    '/api/auth/avatar?token=secret',
    undefined,
  ])
    await assert.rejects(
      service(() => Response.json({ user, avatarUrl: invalid })).api.current(),
      (e) => e.code === 'PROGRAM_FAILED',
    );
  const boundary = new SessionBoundary();
  let resolve;
  const late = createAdministrationApi(
    createHttpClient(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
      boundary,
    ),
  );
  const pending = late.saveProfile('late');
  boundary.reset();
  resolve(Response.json(profile));
  await assert.rejects(pending, (e) => e.code === 'SESSION_CHANGED');
  let expired = false;
  const deniedBoundary = new SessionBoundary();
  deniedBoundary.onUnauthorized = () => {
    expired = true;
  };
  const denied = createAdministrationApi(
    createHttpClient(
      () => Promise.resolve(Response.json({ code: 'SESSION_REPLACED' }, { status: 401 })),
      deniedBoundary,
    ),
  );
  await assert.rejects(denied.saveProfile('name'), (e) => e.code === 'SESSION_REPLACED');
  assert.equal(expired, true);
});
test('SMTP uses protected CAS writes, preserves empty secrets, clears explicitly and discards extra metadata', async () => {
  let value = {
    revision: 0,
    enabled: false,
    address: '',
    username: '',
    from: '',
    tls: 'starttls',
    passwordConfigured: false,
  };
  const { api, requests } = service((_url, options) => {
    if (options.method === 'POST') {
      const input = JSON.parse(options.body);
      value = {
        revision: value.revision + 1,
        enabled: input.enabled,
        address: input.address,
        username: input.username,
        from: input.from,
        tls: input.tls,
        passwordConfigured: input.clearPassword
          ? false
          : !!input.password || value.passwordConfigured,
      };
    }
    return Response.json(value);
  });
  assert.deepEqual(await api.smtp(), value);
  const connection = {
    enabled: false,
    address: 'smtp.example.invalid:587',
    username: 'sender@example.invalid',
    from: 'sender@example.invalid',
    tls: 'starttls',
  };
  await api.saveSmtp({
    ...connection,
    revision: 0,
    password: 'fixture-only-secret',
    role: 'operator',
    userId: 'fake',
  });
  assert.equal(requests[1].body.password, 'fixture-only-secret');
  assert.equal('role' in requests[1].body, false);
  assert.equal('userId' in requests[1].body, false);
  assert.equal('passwordConfigured' in requests[1].body, false);
  await api.saveSmtp({ ...connection, revision: 1, password: '' });
  assert.equal('password' in requests[2].body, false);
  assert.equal(requests[2].body.clearPassword, false);
  assert.equal(value.passwordConfigured, true);
  await api.saveSmtp({ ...connection, revision: 2, clearPassword: true });
  assert.equal(value.passwordConfigured, false);
  assert.equal(requests[3].body.clearPassword, true);
  assert.ok(
    requests.every(
      (request) =>
        request.url === '/api/operations/smtp' &&
        request.options.credentials === 'same-origin' &&
        request.options.cache === 'no-store',
    ),
  );
  for (const invalid of [
    { ...value, revision: 9007199254740992 },
  ])
    await assert.rejects(service(() => Response.json(invalid)).api.smtp(), (error) =>
      ['CONTENT_BOUNDARY_VIOLATION', 'PROGRAM_FAILED'].includes(error.code),
    );
  assert.deepEqual(
    await service(() => Response.json({ ...value, providerStatus: 'ready', password: 'leak', passwordCipher: 'leak' })).api.smtp(),
    value,
  );
  const blocked = service(() => {
    throw new Error('should not send');
  });
  await assert.rejects(
    blocked.api.saveSmtp({ ...connection, revision: 9007199254740992 }),
    (error) => error.code === 'MAIL_CONFIGURATION_INVALID',
  );
  assert.equal(blocked.requests.length, 0);
});
test('SMTP unknown/conflict never resubmits and its read client rejects late account responses', async () => {
  const value = {
    revision: 1,
    enabled: false,
    address: '',
    username: '',
    from: '',
    tls: 'starttls',
    passwordConfigured: false,
  };
  const input = { ...value, revision: 0 };
  for (const [status, code] of [
    [503, 'MAIL_CONFIG_SAVE_UNKNOWN'],
    [409, 'MAIL_CONFIGURATION_CONFLICT'],
  ]) {
    const { api, requests } = service((_url, options) =>
      options.method === 'POST' ? Response.json({ code }, { status }) : Response.json(value),
    );
    await assert.rejects(api.saveSmtp(input), (error) => error.code === code);
    assert.equal(requests.length, 1);
    assert.equal((await api.smtp()).revision, 1);
    assert.equal(requests.filter((request) => request.options.method === 'POST').length, 1);
  }
  let resolve;
  const boundary = new SessionBoundary(),
    api = createAdministrationApi(
      createHttpClient(
        () =>
          new Promise((done) => {
            resolve = done;
          }),
        boundary,
      ),
    );
  const pending = api.smtp();
  boundary.reset();
  resolve(Response.json(value));
  await assert.rejects(pending, (error) => error.code === 'SESSION_CHANGED');
});
test('credit allocations and filtered statistics retain exact amounts, discard extra fields and reject inconsistent receipts', async () => {
  const row = {
    id: 'deduction',
    operationId: 'deduct-op',
    accountId: 'u1',
    kind: 'deduct',
    amount: 12,
    balance: 100,
    category: 'mixed',
    rechargeAmount: 2,
    giftAmount: 10,
    legacyAmount: 0,
    source: 'language-generation:opaque',
    subject: 'u1',
    reason: '整剧解析',
    createdAt: user.createdAt,
  };
  const history = {
    items: [row],
    total: 1,
    page: 1,
    pageSize: 20,
    summary: {
      acquired: 0,
      consumed: 10,
      refunded: 0,
      netConsumed: 10,
      settledConsumed: 7,
      pending: 3,
    },
    analysis: [{ source: 'language-generation', amount: 7 }],
  };
  const wallet = { balance: 100, rechargeBalance: 40, giftBalance: 60, legacyBalance: 0 };
  const { api, requests } = service((url) =>
    Response.json(url.includes('/ledger') ? history : wallet),
  );
  assert.deepEqual(await api.myCreditBalance(), {
    balance: '100',
    rechargeBalance: '40',
    giftBalance: '60',
    legacyBalance: '0',
  });
  const result = await api.myLedger('1', {
    direction: 'consumed',
    category: 'gift',
    start: '2026-10-01T00:00:00Z',
    end: '2026-11-01T00:00:00Z',
  });
  assert.equal(result.summary.consumed, '10');
  assert.equal(result.summary.settledConsumed, '7');
  assert.equal(result.summary.pending, '3');
  assert.equal(result.items[0].amount, '12');
  assert.equal(result.items[0].giftAmount, '10');
  assert.equal(result.analysis[0].amount, '7');
  const query = new URL('http://fixture.invalid' + requests[1].url).searchParams;
  assert.equal(query.get('pageSize'), '10');
  await api.myLedger('2', undefined, '50');
  assert.equal(new URL('http://fixture.invalid' + requests.at(-1).url).searchParams.get('pageSize'), '50');
  await api.userWallet('u1', '1', '20');
  assert.equal(new URL('http://fixture.invalid' + requests.at(-1).url).searchParams.get('pageSize'), '20');
  const beforeInvalid = requests.length;
  await assert.rejects(api.myLedger('1', undefined, '51'));
  assert.equal(requests.length, beforeInvalid);
  assert.equal(query.get('direction'), 'consumed');
  assert.equal(query.get('category'), 'gift');
  assert.equal(query.get('start'), '2026-10-01T00:00:00Z');
  assert.equal(query.has('userId'), false);
  for (const invalid of [
    { ...wallet, giftBalance: 61 },
    { ...wallet, balance: 1.1 },
  ])
    await assert.rejects(
      service(() => Response.json(invalid)).api.myCreditBalance(),
      (error) => error.code === 'PROGRAM_FAILED',
    );
  assert.deepEqual(
    await service(() => Response.json({ ...wallet, lastUpdated: '2026-10-18', privateTitle: 'discarded' })).api.myCreditBalance(),
    { balance: String(wallet.balance), rechargeBalance: String(wallet.rechargeBalance), giftBalance: String(wallet.giftBalance), legacyBalance: String(wallet.legacyBalance) },
  );
  for (const invalid of [
    { ...history, items: [{ ...row, giftAmount: 9 }] },
    { ...history, items: [{ ...row, category: 'gift' }] },
    { ...history, analysis: [{ source: 'private-project-id', amount: 7 }] },
    { ...history, analysis: [{ source: 'language-generation', amount: 6 }] },
  ])
    await assert.rejects(
      service(() => Response.json(invalid)).api.myLedger(),
      (error) => error.code === 'PROGRAM_FAILED',
    );
});
test('operations issuance requires an explicit credit category and deductions cannot redirect the allocation order', async () => {
  const receipt = {
    id: 'add',
    operationId: 'gift-op',
    accountId: 'u1',
    kind: 'add',
    amount: 5,
    balance: 5,
    category: 'gift',
    rechargeAmount: 0,
    giftAmount: 5,
    legacyAmount: 0,
    source: 'operations-adjustment',
    subject: 'operator1',
    reason: 'fixture',
    createdAt: user.createdAt,
  };
  const { api, requests } = service(() => Response.json(receipt));
  await api.adjust({
    account: 'u1',
    command: 'gift-op',
    direction: 'add',
    category: 'gift',
    amount: '5',
    reason: 'fixture',
  });
  assert.deepEqual(requests[0].body, {
    kind: 'add',
    amount: 5,
    operationId: 'gift-op',
    reason: 'fixture',
    category: 'gift',
  });
  const blocked = service(() => {
    throw new Error('must not send');
  });
  await assert.rejects(
    blocked.api.adjust({
      account: 'u1',
      command: 'o',
      direction: 'add',
      amount: '5',
      reason: 'fixture',
    }),
    (error) => error.code === 'CREDIT_INVALID_CATEGORY',
  );
  await assert.rejects(
    blocked.api.adjust({
      account: 'u1',
      command: 'o',
      direction: 'deduct',
      category: 'recharge',
      amount: '5',
      reason: 'fixture',
    }),
    (error) => error.code === 'CREDIT_INVALID_CATEGORY',
  );
  assert.equal(blocked.requests.length, 0);
});
test('daily gift distinguishes zero eligibility from actual funding and rejects inconsistent receipts', async () => {
  const base = { date: '2026-10-19', amount: 0, claimed: false, alreadyClaimed: false };
  const zero = service(() => Response.json(base));
  assert.deepEqual(await zero.api.dailyGift(), { ...base, amount: '0' });
  for (const patch of [
    { claimed: true },
    { alreadyClaimed: true },
    { amount: 60 },
    { claimed: 'false' },
  ]) {
    const invalid = service(() => Response.json({ ...base, ...patch }));
    await assert.rejects(invalid.api.dailyGift());
    assert.equal(invalid.requests.length, 1);
  }
});

test('gift policy preserves integer precision and CAS conflicts, while daily gift is a separate idempotent command', async () => {
  const policy = {
      revision: 4,
      registrationGift: 9007199254740991,
      dailyGift: 60,
      timezone: 'Asia/Shanghai',
    },
    balance = { balance: 60, rechargeBalance: 0, giftBalance: 60, legacyBalance: 0 };
  const { api, requests } = service((url, options) =>
    Response.json(
      url.includes('daily-gift')
        ? { date: '2026-10-17', amount: 60, claimed: true, alreadyClaimed: true, balance }
        : options.method === 'POST'
          ? { ...policy, revision: 5 }
          : policy,
    ),
  );
  assert.equal((await api.creditPolicy()).registrationGift, '9007199254740991');
  await api.saveCreditPolicy({ ...policy, role: 'operator', userId: 'fake' });
  assert.deepEqual(requests[1].body, {
    revision: 4,
    registrationGift: 9007199254740991,
    dailyGift: 60,
  });
  const claim = await api.dailyGift();
  assert.equal(claim.alreadyClaimed, true);
  assert.equal(claim.amount, '60');
  assert.equal(Object.hasOwn(claim, 'balance'), false);
  assert.equal(requests[2].url, '/api/credits/daily-gift');
  assert.deepEqual(requests[2].body, {});
  assert.equal(requests[2].options.method, 'POST');
  for (const [status, code] of [
    [409, 'CREDIT_POLICY_CONFLICT'],
    [503, 'CREDIT_POLICY_SAVE_UNKNOWN'],
  ]) {
    const failed = service(() => Response.json({ code }, { status }));
    await assert.rejects(failed.api.saveCreditPolicy(policy), (error) => error.code === code);
    assert.equal(failed.requests.length, 1);
  }
  const invalid = service(() => {
    throw new Error('must not send');
  });
  for (const dailyGift of [-1, 1.1, 9007199254740992])
    await assert.rejects(
      invalid.api.saveCreditPolicy({ ...policy, dailyGift }),
      (error) => error.code === 'CREDIT_POLICY_INVALID',
    );
  assert.equal(invalid.requests.length, 0);
  let resolve;
  const boundary = new SessionBoundary(),
    late = createAdministrationApi(
      createHttpClient(
        () =>
          new Promise((done) => {
            resolve = done;
          }),
        boundary,
      ),
    );
  const pending = late.dailyGift();
  boundary.reset();
  resolve(
    Response.json({
      date: '2026-10-17',
      amount: 60,
      claimed: true,
      alreadyClaimed: false,
      balance,
    }),
  );
  await assert.rejects(pending, (error) => error.code === 'SESSION_CHANGED');
});
