import test from 'node:test';
import assert from 'node:assert/strict';
import { createPaymentApi } from '../src/api/payment-client.ts';
import { createHttpClient } from '../src/api/client.ts';
import { SessionBoundary } from '../src/api/session-boundary.ts';
import { createAdministrationApi, creditLedgerView } from '../src/api/administration.ts';
import {
  formatPaymentAmount,
  paymentAmountInput,
  paymentCreditsInput,
  paymentChannelLabel,
  paymentStatusLabel,
  paymentTargetLabel
} from '../src/domain/payment-format.ts';
import { creditSignedAmount, creditRecordTitle, creditSourceLabel, creditActorLabel } from '../src/pages/credits/personal-credit-format.ts';
const no = 'PO20261020120000ABCDEFGHIJ', now = '2026-10-20T04:00:00Z';
const pack = {
  id: 'starter',
  name: '入门档位',
  revision: 1,
  amountFen: 100,
  originalAmountFen: 100,
  rechargeCredits: 1000,
  giftCredits: 100,
  enabled: true,
  sort: 0,
  testOnly: false,
  createdAt: now,
  updatedAt: now
};
const order = {
  orderNo: no,
  buyerUserId: 'buyer',
  clientRequestId: 'request-one',
  target: { kind: 'personal', id: 'buyer', name: '个人空间' },
  packageId: pack.id,
  amountFen: 100,
  originalAmountFen: 100,
  rechargeCredits: 1000,
  giftCredits: 100,
  channel: 'demo',
  codeUrl: 'demo:' + no,
  channelTradeNo: '',
  status: 'pending',
  agreementVersion: '2026-10-20',
  expiresAt: '2026-10-20T04:15:00Z',
  createdAt: now,
  creditedAt: null,
  paidAt: null,
  closedAt: null,
  refundedAt: null
};
const command = {
  clientRequestId: order.clientRequestId,
  target: { kind: 'personal', id: 'buyer' },
  packageId: pack.id,
  channel: 'demo',
  agreementVersion: order.agreementVersion
};
const options = {
  channels: [{ id: 'demo', demo: true }],
  packages: [pack],
  targets: [order.target],
  agreement: { version: order.agreementVersion, agreed: false }
};
const refund = {
  id: 'refund-request',
  refundNo: 'RFsame',
  orderNo: no,
  kind: 'operator',
  amountFen: 50,
  rechargeCredits: 500,
  giftCredits: 50,
  status: 'pending',
  operatorId: 'operator',
  reason: '保留人工原因',
  channelRefundId: '',
  attempts: 0,
  billAt: null,
  createdAt: now,
  finishedAt: null
};
const anomaly = {
  id: 'alert-one',
  orderNo: no,
  channel: 'demo',
  kind: 'refund_failed',
  detail: { stage: 'restore_pending' },
  status: 'open',
  resolvedAt: null,
  createdAt: now
};
const receipt = {
  id: 'reclaim-one',
  operationId: 'recharge:reclaim:RFsame',
  accountId: 'buyer',
  kind: 'reclaim',
  amount: 500,
  balance: 600,
  category: 'recharge',
  rechargeAmount: 500,
  giftAmount: 0,
  legacyAmount: 0,
  source: 'payment-order:' + no,
  subject: 'operator',
  reason: '充值退款扣回',
  originalAdditionId: 'original-recharge',
  createdAt: now
};
const page = items => ({
  items,
  total: items.length,
  page: 1,
  pageSize: 20
});
function fixture(reply, boundary = new SessionBoundary()) {
  const requests = [];
  const api = createPaymentApi(createHttpClient(async (url, init) => {
    requests.push({ url, init, body: typeof init.body === 'string' ? JSON.parse(init.body) : null });
    return reply(url, init);
  }, boundary));
  return { api, requests, boundary };
}
const json = value => () => Response.json(value);
test('payment client consumes every HTTP route with Cookie transport and projects only declared facts', async () => {
  const { api, requests } = fixture(url => {
    if (url.endsWith('/options')) return Response.json({ ...options, ignored: true });
    if (url.endsWith('/refunds')) return Response.json(refund, { status: 201 });
    if (url.includes('/refund-quote?')) return Response.json({
      maxCredits: 1000,
      rechargeCredits: 500,
      giftCredits: 50,
      amountFen: 50
    });
    if (url.endsWith('/resolve')) return Response.json({
      ...anomaly,
      status: 'resolved',
      note: '已核对原文',
      resolvedBy: 'operator',
      resolvedAt: now
    });
    if (url.includes('/anomalies?')) return Response.json(page([anomaly]));
    if (url.endsWith('/packages')) return Response.json({ items: [pack] });
    if (url.endsWith('/settings')) return Response.json({ revision: 1, dailyLimitFen: 500000 });
    if (url.includes('/orders?')) return Response.json(page([order]));
    if (url === '/api/operations/payments/orders/' + no) return Response.json({
      order,
      events: [{
        id: 'event',
        source: 'query',
        outcome: 'pending',
        facts: {},
        createdAt: now
      }],
      refunds: [refund],
      credits: [receipt]
    });
    return Response.json({ ...order, ignored: true });
  });
  assert.equal((await api.options()).agreement.agreed, false);
  await api.createOrder({ ...command, ignored: 'must not send' });
  await api.getOrder(no);
  await api.closeOrder(no);
  await api.simulate(no, 'paid');
  assert.equal((await api.orders()).total, 1);
  await api.operationsOrders({ channelTradeNo: 'real channel id', buyerUserId: 'buyer', start: now });
  const details = await api.orderDetails(no);
  assert.equal(details.credits[0].direction, 'reclaim');
  assert.equal(details.credits[0].originalAddition, 'original-recharge');
  assert.equal(details.credits[0].originalDebit, null);
  await api.queryOrder(no, '保留人工原因');
  assert.equal((await api.refundQuote(no)).maxCredits, 1000);
  assert.equal((await api.refundQuote(no, 500)).amountFen, 50);
  await api.createRefund(no, { refundId: refund.id, credits: 500, reason: refund.reason });
  assert.equal((await api.anomalies({ status: 'open', kind: 'refund_failed', channel: 'demo' })).items[0].detail.stage, 'restore_pending');
  await api.resolveAnomaly(anomaly.id, '已核对原文');
  assert.equal((await api.packages())[0].amountFen, 100);
  assert.equal((await api.settings()).dailyLimitFen, 500000);
  assert.ok(requests.every(r => r.init.credentials === 'same-origin' && r.init.headers.get('X-Drama-Audience') === 'workspace'));
  assert.deepEqual(requests.find(r => r.url === '/api/payments/orders' && r.init.method === 'POST').body, command);
  assert.deepEqual(requests.find(r => r.url.endsWith('/close')).body, {});
  assert.equal(requests.find(r => r.url.endsWith('/query')).body.reason, '保留人工原因');
  assert.equal(new URL(requests.find(r => r.url.startsWith('/api/operations/payments/orders?')).url, 'https://fixture.invalid').searchParams.get('channelTradeNo'), 'real channel id');
  assert.equal('ignored' in await api.getOrder(no), false);
});
test('package and settings writes validate original CAS, all saved facts and zero unlimited setting', async () => {
  const { api, requests } = fixture((url, init) => {
    const body = JSON.parse(init.body);
    return Response.json(url.endsWith('/packages') ? { ...pack, ...body, revision: body.revision + 1 } : { revision: body.revision + 1, dailyLimitFen: body.dailyLimitFen });
  });
  assert.equal((await api.savePackage({ ...pack, amountFen: Number.MAX_SAFE_INTEGER, originalAmountFen: Number.MAX_SAFE_INTEGER })).revision, 2);
  assert.equal((await api.saveSettings({ revision: 1, dailyLimitFen: 0 })).dailyLimitFen, 0);
  assert.equal(requests[0].body.amountFen, Number.MAX_SAFE_INTEGER);
  assert.equal('createdAt' in requests[0].body, false);
  assert.equal('updatedAt' in requests[0].body, false);
  for (const changed of [{ revision: 1 }, { name: 'wrong' }, { originalAmountFen: 200 }, { amountFen: 2 }, { enabled: false }, { sort: 1 }, { giftCredits: 2 }, { id: 'wrong' }]) await assert.rejects(fixture(json({ ...pack, revision: 2, ...changed })).api.savePackage({ ...pack }), e => e.code === 'PROGRAM_FAILED');
  await assert.rejects(fixture(json({ revision: 2, dailyLimitFen: 1 })).api.saveSettings({ revision: 1, dailyLimitFen: 0 }), e => e.code === 'PROGRAM_FAILED');
});
test('unsafe integers, malformed nested facts and other order credit associations never reach UI', async () => {
  for (const bad of [Number.MAX_SAFE_INTEGER + 1, -1, 0.1, null, '100']) {
    for (const key of ['amountFen', 'originalAmountFen', 'rechargeCredits', 'giftCredits']) await assert.rejects(fixture(json({ ...order, [key]: bad })).api.getOrder(no), e => e.code === 'PROGRAM_FAILED');
    await assert.rejects(fixture(json({ revision: bad, dailyLimitFen: 0 })).api.settings(), e => e.code === 'PROGRAM_FAILED');
  }
  for (const changed of [{ target: null }, { status: 'paid' }, { channel: 'unknown' }, { paidAt: undefined }, { createdAt: 'bad' }, { giftCredits: Number.MAX_SAFE_INTEGER }]) await assert.rejects(fixture(json({ ...order, ...changed })).api.getOrder(no));
  for (const bad of [null, {}, { ...options, channels: [{ id: 'demo', demo: false }] }, { ...options, channels: options.channels.concat(options.channels) }, { ...options, packages: null }]) await assert.rejects(fixture(json(bad)).api.options());
  for (const changed of [{ total: Number.MAX_SAFE_INTEGER + 1 }, { page: 2 }, { pageSize: 100 }, { total: 0 }, { items: null }]) await assert.rejects(fixture(json({ ...page([order]), ...changed })).api.orders());
  const detail = {
    order,
    events: [],
    refunds: [],
    credits: [receipt]  
};
  await assert.rejects(fixture(json({ ...detail, credits: [{ ...receipt, source: 'payment-order:someone-else' }] })).api.orderDetails(no));
  await assert.rejects(fixture(json({ ...detail, refunds: [{ ...refund, orderNo: 'another-order' }] })).api.orderDetails(no));
});
test('mutation receipts retain original command and order; uncertain writes are sent once', async () => {
  for (const changed of [{ clientRequestId: 'other' }, { packageId: 'other' }, { target: { ...order.target, id: 'other' } }, { channel: 'alipay' }, { agreementVersion: 'old' }]) await assert.rejects(fixture(json({ ...order, ...changed })).api.createOrder(command));
  for (const call of [api => api.getOrder(no), api => api.closeOrder(no), api => api.simulate(no, 'failed'), api => api.queryOrder(no, '原因')]) await assert.rejects(call(fixture(json({ ...order, orderNo: 'other' })).api));
  for (const changed of [{ id: 'other' }, { orderNo: 'other' }, { rechargeCredits: 499 }, { reason: 'other' }, { kind: 'late_payment' }]) await assert.rejects(fixture(json({ ...refund, ...changed })).api.createRefund(no, { refundId: refund.id, credits: 500, reason: refund.reason }));
  const failed = fixture(() => Response.json({ code: 'PAYMENT_PENDING', message: '服务端原文保留' }, { status: 503 }));
  await assert.rejects(failed.api.createOrder(command), e => e.status === 503 && e.message === '服务端原文保留');
  assert.equal(failed.requests.length, 1);
});
test('invalid payment input makes no request and query filters keep only server field names', async () => {
  const { api, requests } = fixture(() => { throw Error('must not send'); });
  for (const input of [{ ...command, clientRequestId: '' }, { ...command, target: { kind: 'other', id: 'buyer' } }, { ...command, channel: 'other' }]) await assert.rejects(api.createOrder(input), e => e.status === 400);
  for (const input of [{ ...pack, revision: Number.MAX_SAFE_INTEGER }, { ...pack, sort: 2147483648 }, { ...pack, giftCredits: Number.MAX_SAFE_INTEGER }]) await assert.rejects(api.savePackage({ ...input }), e => e.status === 400);
  await assert.rejects(api.saveSettings({ revision: 1, dailyLimitFen: Number.MAX_SAFE_INTEGER + 1 }));
  await assert.rejects(api.createRefund(no, { refundId: 'refund', credits: 0, reason: '原文' }));
  await assert.rejects(api.queryOrder(no, ' '));
  await assert.rejects(api.operationsOrders({ tradeNo: 'wrong-field' }));
  await assert.rejects(api.operationsOrders({ status: 'all' }));
  await assert.rejects(api.operationsOrders({ start: now, end: now }));
  await assert.rejects(api.anomalies({ channel: 'all' }));
  await assert.rejects(api.orders(0));
  await assert.rejects(api.orders(1, 101));
  assert.equal(requests.length, 0);
});
test('payment reads retain AbortSignal and shared session fence against late responses', async () => {
  let resolve;
  const boundary = new SessionBoundary();
  const { api } = fixture(() => new Promise(r => { resolve = r; }), boundary);
  const late = api.options();
  boundary.reset();
  resolve(Response.json(options));
  await assert.rejects(late, e => e.code === 'SESSION_CHANGED');
  const controller = new AbortController();
  const next = fixture(() => new Promise(r => { resolve = r; }));
  const aborted = next.api.getOrder(no, controller.signal);
  controller.abort();
  resolve(Response.json(order));
  await assert.rejects(aborted, e => e.code === 'ABORTED');
});
test('money inputs preserve every fen at safe integer bound and localize only owned names', () => {
  for (const [input, fen] of [['0', 0], ['0.01', 1], ['0.1', 10], ['1.23', 123], ['90071992547409.91', Number.MAX_SAFE_INTEGER]]) {
    assert.equal(paymentAmountInput(input), fen);
    assert.equal(paymentAmountInput(formatPaymentAmount(fen).slice(1)), fen);  
}
  for (const value of ['-1', '1e2', '1.001', '.50', '00.01', '90071992547409.92', 'Infinity']) assert.throws(() => paymentAmountInput(value));
  for (const value of [NaN, -1, 0.1, Number.MAX_SAFE_INTEGER + 1]) assert.throws(() => formatPaymentAmount(value));
  assert.equal(paymentCreditsInput(String(Number.MAX_SAFE_INTEGER)), Number.MAX_SAFE_INTEGER);
  assert.equal(paymentCreditsInput('0', true), 0);
  for (const value of ['0', '01', '1.0', '-1', '1e3', '9007199254740992']) assert.throws(() => paymentCreditsInput(value));
  const c = key => 'translated:' + key;
  assert.equal(paymentTargetLabel(order.target, c), 'translated:个人空间');
  assert.equal(paymentTargetLabel({ kind: 'team', id: 'team', name: '用户原文团队' }, c), '用户原文团队');
  assert.equal(paymentChannelLabel('demo', c), 'translated:演示支付');
  assert.equal(paymentStatusLabel('credited', c), 'translated:已到账');
});
test('reclaim flows through all, recent and operations credit reads, independent from consumption', async () => {
  const summary = {
    acquired: 0,
    consumed: 0,
    refunded: 0,
    netConsumed: 0,
    settledConsumed: 0,
    pending: 0
  }, requests = [];
  const api = createAdministrationApi(createHttpClient(async url => {
    requests.push(url);
    return Response.json(url.endsWith('/recent') ? { items: [receipt] } : url.includes('/balance') ? {
      balance: 600,
      rechargeBalance: 600,
      giftBalance: 0,
      legacyBalance: 0
    } : { ...page([receipt]), summary, analysis: [] });
  }, new SessionBoundary()));
  const filtered = await api.myLedger('', {
    direction: 'reclaimed',
    category: 'all',
    start: '',
    end: ''
  });
  assert.match(requests[0], /direction=reclaimed/);
  assert.equal(filtered.items[0].direction, 'reclaim');
  assert.equal(filtered.summary.consumed, '0');
  assert.equal((await api.myLedger('', {
    direction: 'all',
    category: 'all',
    start: '',
    end: ''
  })).items[0].originalAddition, receipt.originalAdditionId);
  assert.equal((await api.myRecentLedger())[0].originalAddition, receipt.originalAdditionId);
  assert.equal((await api.userWallet('buyer')).ledger.items[0].direction, 'reclaim');
  for (const changed of [{ originalAdditionId: undefined }, { originalAdditionId: '' }, { originalDeductionId: 'deduct' }, { category: 'legacy', rechargeAmount: 0, legacyAmount: 500 }, { category: 'mixed', rechargeAmount: 250, giftAmount: 250 }, { kind: 'add' }]) assert.throws(() => creditLedgerView({ ...receipt, ...changed }));
  const view = creditLedgerView(structuredClone(receipt)), c = key => 'translated:' + key;
  assert.equal(creditSignedAmount(view), '−500');
  assert.equal(creditRecordTitle(view, c), 'translated:充值退款扣回');
  assert.equal(creditSourceLabel(view.source, c), 'translated:积分充值');
  assert.equal(creditActorLabel({ ...view, actorLabel: 'payment:demo' }, c), 'translated:演示支付');
  assert.equal(creditRecordTitle({ ...view, reason: '运营自填原文' }, c), '运营自填原文');
  assert.equal(creditActorLabel(view, c), 'operator');
});

test('credit-first and payment-first modules initialize independently without an API runtime cycle', async () => {
  const { spawnSync } = await import('node:child_process');
  for (const first of ['credit-client', 'payment-client']) {
    const script = `
    const first = await import('./src/api/${first}.ts');
    const { createHttpClient } = await import('./src/api/client.ts');
    const { SessionBoundary } = await import('./src/api/session-boundary.ts');
    const credit = await import('./src/api/credit-client.ts');
    const transport = createHttpClient(async () => Response.json({items:[${JSON.stringify(receipt)}]}), new SessionBoundary());
    const rows = await credit.createCreditApi(transport).myRecentLedger();
    if(rows[0].originalAddition !== 'original-recharge') throw Error('original Add missing');
    if('${first}' === 'payment-client' && typeof first.createPaymentApi !== 'function') throw Error('payment API missing');
  `;
    const result = spawnSync(process.execPath, ['--import', 'tsx', '--input-type=module', '-e', script], { encoding: 'utf8', cwd: process.cwd() });
    assert.equal(result.status, 0, first + ': ' + result.stderr);
  }
});

test('payment terminal statuses require persisted completion evidence before being shown as confirmed', async () => {
  for (const changed of [{status:'credited'}, {status:'refunded'}, {status:'closed'}])
    await assert.rejects(fixture(json({...order,...changed})).api.getOrder(no));
  const credited = {...order,status:'credited',creditedAt:now,paidAt:now,channelTradeNo:'real-trade'};
  assert.equal((await fixture(json(credited)).api.getOrder(no)).status,'credited');
  for (const changed of [{status:'succeeded'}, {status:'failed'}])
    await assert.rejects(fixture(json({...refund,...changed})).api.createRefund(no,{refundId:refund.id,credits:500,reason:refund.reason}));
});
test('payment and reclaim names follow current English while operator text stays original', async () => {
  const { default:i18n } = await import('../src/i18n.ts');
  const { translate } = await import('../src/contexts/translate.ts');
  const previous = i18n.language;
  try {
    await i18n.changeLanguage('en');
    for (const value of [paymentTargetLabel(order.target,translate),paymentChannelLabel('demo',translate),paymentStatusLabel('credited',translate),creditRecordTitle(creditLedgerView(receipt),translate),creditSourceLabel(receipt.source,translate)])
      assert.doesNotMatch(value,/[\u3400-\u9fff]/u);
    assert.equal(creditRecordTitle({...creditLedgerView(receipt),reason:'运营填写的原文'},translate),'运营填写的原文');
  } finally { await i18n.changeLanguage(previous); }
});

test('closed payment uses its own order label without changing switch disabled copy', () => {
  const labels = { '\u8ba2\u5355\u5df2\u5173\u95ed': 'Closed', '\u5df2\u5173\u95ed': 'Disabled' };
  const c = key => labels[key] || key;
  assert.equal(paymentStatusLabel('closed', c), 'Closed');
  assert.equal(c('\u5df2\u5173\u95ed'), 'Disabled');
});


test('discount prices remain independent of bonus credits, package names reach the cashier and receipts freeze both prices', async () => {
  const discounted = { ...pack, name: '节日内部名称', amountFen: 8000, originalAmountFen: 10000 };
  const option = await fixture(json({ ...options, packages: [discounted] })).api.options();
  assert.equal(option.packages[0].amountFen, 8000);
  assert.equal(option.packages[0].originalAmountFen, 10000);
  assert.equal(option.packages[0].rechargeCredits, 1000);
  assert.equal(option.packages[0].giftCredits, 100);
  assert.equal(option.packages[0].name, '节日内部名称');
  assert.equal((await fixture(json({ items: [discounted] })).api.packages())[0].name, '节日内部名称');
  assert.equal((await fixture(json({ items: [{ ...discounted, name: '' }] })).api.packages())[0].name, '');
  const f = fixture((_url, init) => {
    const body = JSON.parse(init.body);
    return Response.json({ ...discounted, ...body, revision: body.revision + 1 });
  });
  const saved = await f.api.savePackage({ ...discounted, name: '  节日内部名称  ' });
  assert.equal(saved.name, '节日内部名称');
  assert.equal(f.requests[0].body.name, '节日内部名称');
  assert.equal(f.requests[0].body.originalAmountFen, 10000);
  const frozen = await fixture(json({ ...order, amountFen: 8000, originalAmountFen: 10000 })).api.getOrder(no);
  assert.equal(frozen.originalAmountFen, 10000);
  for (const originalAmountFen of [undefined, null, 0, 99, Number.MAX_SAFE_INTEGER + 1]) {
    await assert.rejects(fixture(json({ ...order, originalAmountFen })).api.getOrder(no), e => e.code === 'PROGRAM_FAILED');
    await assert.rejects(fixture(json({ ...options, packages: [{ ...pack, originalAmountFen }] })).api.options(), e => e.code === 'PROGRAM_FAILED');
  }
  for (const name of [undefined, null, '  ', 'x'.repeat(49), 'a\nb', 'a\u0000b', '\ud800']) {
    const invalid = fixture(() => { throw Error('must not send'); });
    await assert.rejects(invalid.api.savePackage({ ...pack, name }), e => e.code === 'PAYMENT_INPUT_INVALID');
    assert.equal(invalid.requests.length, 0);
  }
  const unicode = '😀'.repeat(48);
  await f.api.savePackage({ ...discounted, name: unicode });
  for (const name of [null, ' ', 'x'.repeat(49), 'a\nb', '\ud800']) {
    await assert.rejects(fixture(json({ items: [{ ...pack, name }] })).api.packages(), e => e.code === 'PROGRAM_FAILED');
  }
  const invalid = fixture(() => { throw Error('must not send'); });
  await assert.rejects(invalid.api.savePackage({ ...discounted, originalAmountFen: 7999 }));
  assert.equal(invalid.requests.length, 0);
});


test('configuration saves omit removed reasons while preserving revisions and mandatory reasons on financial actions', async () => {
 const f = fixture((url, init) => {
  const body = JSON.parse(init.body);
  return Response.json(url.endsWith('/packages') ? { ...pack, ...body, revision: body.revision + 1 } : { ...body, revision: body.revision + 1 });
 });
 await f.api.savePackage({ ...pack, reason: 'obsolete extra input' });
 await f.api.saveSettings({ revision: 7, dailyLimitFen: 0, reason: 'obsolete extra input' });
 assert.equal(f.requests[0].body.revision, pack.revision);
 assert.equal(f.requests[1].body.revision, 7);
 for (const request of f.requests) assert.equal('reason' in request.body, false);
 const invalid = fixture(() => { throw Error('must not send'); });
 await assert.rejects(invalid.api.createRefund(no, { refundId: 'original', credits: 1, reason: '' }));
 await assert.rejects(invalid.api.queryOrder(no, ''));
 await assert.rejects(invalid.api.resolveAnomaly(anomaly.id, ''));
 assert.equal(invalid.requests.length, 0);
});
