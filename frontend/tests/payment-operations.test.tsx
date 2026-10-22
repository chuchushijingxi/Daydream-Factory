import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import type {
  PaymentAPI,
  PaymentOrder,
  PaymentRefund,
  OperationsPaymentPackage,
  PaymentOrderDetails,
  PaymentAnomaly,
} from '../src/api/payment-types';
const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/#/operations/payments/orders',
  pretendToBeVisual: true,
});
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'HTMLTextAreaElement',
  'HTMLSelectElement',
  'HTMLButtonElement',
  'Element',
  'Node',
  'NodeFilter',
  'DocumentFragment',
  'SVGElement',
  'MutationObserver',
  'Event',
  'MouseEvent',
  'CustomEvent',
  'localStorage',
  'sessionStorage',
  'KeyboardEvent',
])
  Object.defineProperty(globalThis, key, {
    value: dom.window[key as keyof Window],
    configurable: true,
  });
Object.assign(globalThis, {
  getComputedStyle: dom.window.getComputedStyle,
  matchMedia: () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }),
  ResizeObserver: class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
  requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window),
  cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window),
  IS_REACT_ACT_ENVIRONMENT: true,
});
Object.defineProperty(globalThis, 'BroadcastChannel', {
  value: undefined,
  configurable: true,
});
const { createRoot } = await import('react-dom/client');
const { MemoryRouter, useLocation, useNavigate } = await import('react-router-dom');
const { LanguageProvider } = await import('../src/contexts/LanguageContext');
const { OperationsPaymentOrdersPage } = await import(
  '../src/pages/operations/OperationsPaymentOrdersPage'
);
const { OperationsPaymentAnomaliesPage } = await import(
  '../src/pages/operations/OperationsPaymentAnomaliesPage'
);
const { OperationsPaymentPackagesPage } = await import(
  '../src/pages/operations/OperationsPaymentPackagesPage'
);
const { OperationsLayout } = await import('../src/pages/operations/OperationsLayout');
const { SessionOptionalContext } = await import('../src/state/SessionContext');
const { paymentApi } = await import('../src/api/payment-client');
const { sessionBoundary } = await import('../src/api/session-boundary');
const { ApiError } = await import('../src/api/http-error');
const at = '2026-10-20T01:00:00Z';
const order: PaymentOrder = {
  orderNo: 'PO20261020090000AAAAAAAAAA',
  buyerUserId: 'user-fixture',
  clientRequestId: 'fixture-request',
  target: { kind: 'personal', id: 'user-fixture', name: 'fixture' },
  packageId: 'fixture-package',
  amountFen: 999,
  originalAmountFen: 999,
  rechargeCredits: 100,
  giftCredits: 20,
  channel: 'demo',
  codeUrl: 'demo://fixture',
  channelTradeNo: 'demo-trade',
  status: 'credited',
  agreementVersion: '2026-10-20',
  expiresAt: at,
  createdAt: at,
  creditedAt: at,
  paidAt: at,
  closedAt: null,
  refundedAt: null,
};
const pack: OperationsPaymentPackage = {
  name: '日常档位',
  id: 'fixture-package',
  revision: 3,
  amountFen: 999,
  originalAmountFen: 999,
  rechargeCredits: 100,
  giftCredits: 20,
  enabled: true,
  sort: 2,
  testOnly: false,
  createdAt: at,
  updatedAt: at,
};
const anomaly: PaymentAnomaly = {
  id: 'anomaly-fixture',
  orderNo: order.orderNo,
  channel: 'demo',
  kind: 'refund_failed',
  detail: { code: 'PAYMENT_REFUND_RESTORE_PENDING' },
  status: 'open',
  createdAt: at,
  resolvedAt: null,
};
function refund(id: string, credits = 40): PaymentRefund {
  return {
    id,
    refundNo: 'PR-fixture',
    orderNo: order.orderNo,
    kind: 'operator',
    amountFen: 399,
    rechargeCredits: credits,
    giftCredits: 8,
    status: 'pending',
    reason: 'test reason',
    operatorId: 'operator-fixture',
    channelRefundId: '',
    attempts: 0,
    createdAt: at,
    finishedAt: null,
    billAt: null,
  };
}
function details(o = order): PaymentOrderDetails {
  return {
    order: o,
    events: [
      {
        id: 'event-fixture',
        orderNo: o.orderNo,
        source: 'refund',
        outcome: 'restore_pending',
        facts: { code: 'PAYMENT_REFUND_RESTORE_PENDING' },
        createdAt: at,
      },
    ],
    refunds: [],
    credits: [],
  };
}
function fixture(): PaymentAPI {
  return {
    options: async () => ({
      channels: [],
      packages: [],
      targets: [],
      agreement: { version: '2026-10-20', agreed: false },
    }),
    createOrder: async () => order,
    getOrder: async () => order,
    closeOrder: async () => order,
    orders: async () => ({ items: [order], total: 1, page: 1, pageSize: 20 }),
    simulate: async () => order,
    operationsOrders: async (_filters, page = 1, pageSize = 20) => ({
      items: [order],
      total: 1,
      page,
      pageSize,
    }),
    orderDetails: async () => details(),
    queryOrder: async () => order,
    refundQuote: async (_no, credits = 0) => ({
      maxCredits: 100,
      rechargeCredits: credits || 100,
      giftCredits: credits === 40 ? 8 : 20,
      amountFen: credits === 40 ? 399 : 999,
    }),
    createRefund: async (_no, input) => refund(input.refundId, input.credits),
    anomalies: async (_filter, page = 1, pageSize = 20) => ({
      items: [anomaly],
      total: 1,
      page,
      pageSize,
    }),
    resolveAnomaly: async (_id, note) => ({
      ...anomaly,
      status: 'resolved',
      note,
      resolvedBy: 'operator-fixture',
      resolvedAt: at,
    }),
    packages: async () => [pack],
    savePackage: async (input) => ({
      ...input,
      revision: input.revision + 1,
      createdAt: at,
      updatedAt: at,
    }),
    settings: async () => ({ revision: 4, dailyLimitFen: 500000 }),
    saveSettings: async (input) => ({ ...input, revision: input.revision + 1 }),
  };
}
let root: ReturnType<typeof createRoot> | undefined;
const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 5));
  });
};
async function mount(node: React.ReactNode) {
  if (root) await act(async () => root!.unmount());
  sessionBoundary.reset();
  root = createRoot(document.getElementById('root')!);
  await act(async () => root!.render(<LanguageProvider>{node}</LanguageProvider>));
  await flush();
}
const buttons = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].filter(
    (v) => v.textContent?.trim() === name || v.getAttribute('aria-label') === name,
  );
function button(name: string) {
  const found = buttons(name).at(-1);
  assert.ok(found, 'button ' + name);
  return found;
}
function field(name: string) {
  const labels = [...document.querySelectorAll('label')];
  const label = labels.find(
    (v) => v.firstElementChild?.textContent === name || v.textContent?.trim() === name,
  );
  assert.ok(label, 'field ' + name);
  const input = label.querySelector('input,textarea,select');
  assert.ok(input);
  return input as HTMLInputElement;
}
async function fill(name: string, value: string) {
  await act(async () => {
    const input = field(name),
      proto =
        input.tagName === 'TEXTAREA'
          ? HTMLTextAreaElement.prototype
          : input.tagName === 'SELECT'
            ? HTMLSelectElement.prototype
            : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(input, value);
    input.dispatchEvent(
      new Event(input.tagName === 'SELECT' ? 'change' : 'input', {
        bubbles: true,
      }),
    );
  });
}
async function choose(name: string, value: string) {
  await act(async () =>
    button(name).dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })),
  );
  await flush();
  const item = [...document.querySelectorAll<HTMLElement>('[role="menuitemradio"]')].find(
    (node) => node.getAttribute('data-state') !== null && node.textContent === value,
  );
  assert.ok(item, 'menu option ' + value);
  await act(async () => item.click());
  await flush();
}
async function editDate(name: string, value: string, confirm = true) {
  await click(name);
  const [day, time] = value.split('T'),
    [year, month, date] = day.split('-');
  // Set the shown month through the real calendar controls, independent of the test run date.
  const target = Number(year) * 12 + Number(month);
  let displayed = document
    .querySelector('.jm-calendar-head > span')!
    .textContent!.match(/(\d+) 年 (\d+) 月/)!;
  let current = Number(displayed[1]) * 12 + Number(displayed[2]);
  while (current !== target) {
    await click(current < target ? '下个月' : '上个月');
    current += current < target ? 1 : -1;
  }
  await click(`${year}年${Number(month)}月${Number(date)}日`);
  for (const [part, raw] of [
    ['小时', time.slice(0, 2)],
    ['分钟', time.slice(3, 5)],
  ]) {
    const input = document.querySelector<HTMLInputElement>(`input[aria-label="${name}${part}"]`)!;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, raw);
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
  }
  if (confirm) await click('确定');
  else {
    await act(async () =>
      document.activeElement!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      ),
    );
    await flush();
  }
}
async function click(name: string) {
  await act(async () => button(name).click());
  await flush();
}
after(async () => {
  if (root) await act(async () => root!.unmount());
  dom.window.close();
});

test('order filters send all server keys and Beijing times as RFC3339; detail shows audited restore pending without claiming restored', async () => {
  const api = fixture(),
    calls: unknown[] = [];
  api.operationsOrders = async (filter, page = 1, pageSize = 20) => {
    calls.push(filter);
    return { items: [order], total: 1, page, pageSize };
  };
  await mount(<OperationsPaymentOrdersPage api={api} />);
  await fill('订单号', order.orderNo);
  await fill('渠道交易号', 'channel-id');
  await fill('下单人 ID', 'buyer');
  await fill('目标空间 ID', 'target');
  await choose('目标空间类型', '团队空间');
  await choose('支付渠道', '微信支付');
  await choose('订单状态', '已到账');
  await editDate('开始时间（北京时间）', '2026-10-19T09:00');
  await editDate('结束时间（北京时间）', '2026-10-20T09:00');
  await click('查询订单');
  assert.deepEqual(calls.at(-1), {
    orderNo: order.orderNo,
    channelTradeNo: 'channel-id',
    buyerUserId: 'buyer',
    targetKind: 'team',
    targetId: 'target',
    channel: 'wechatpay',
    status: 'credited',
    start: '2026-10-19T01:00:00.000Z',
    end: '2026-10-20T01:00:00.000Z',
  });
  await click('查看详情');
  assert.match(document.body.textContent || '', /渠道已拒绝退款，积分恢复待处理/);
  assert.match(document.body.textContent || '', /PAYMENT_REFUND_RESTORE_PENDING/);
  await click('重新查单');
  assert.equal(button('重新查单').disabled, true);
  await fill('重新查单原因', 'offline investigation');
  let queried = '';
  api.queryOrder = async (_no, reason) => {
    queried = reason;
    return order;
  };
  await click('重新查单');
  assert.equal(queried, 'offline investigation');
});

test('refund uses server quote, requires second confirmation and retains one original ID after unknown result and dialog reopening', async () => {
  const api = fixture(),
    sent: { refundId: string; credits: number; reason: string }[] = [];
  api.createRefund = async (_no, input) => {
    sent.push({ ...input });
    if (sent.length === 1) throw new ApiError(503, 'PAYMENT_COMMIT_UNKNOWN', 'fixture unknown');
    return refund(input.refundId, input.credits);
  };
  await mount(<OperationsPaymentOrdersPage api={api} />);
  await click('查看详情');
  await click('发起原路退款');
  await fill('退款积分数', '40');
  await fill('退款原因', 'test reason');
  await click('计算并核对退款');
  assert.equal(sent.length, 0);
  assert.ok(document.querySelector('[role=alertdialog]'));
  assert.match(document.body.textContent || '', /退回 ¥3.99/);
  await click('确认退款');
  assert.equal(sent.length, 1);
  const id = sent[0].refundId;
  assert.ok(id);
  assert.equal(field('退款积分数').disabled, true);
  assert.equal(field('退款原因').value, 'test reason');
  assert.match(document.body.textContent || '', /退款提交结果未确认/);
  await click('关闭');
  await click('发起原路退款');
  assert.equal(field('退款积分数').value, '40');
  assert.equal(field('退款原因').value, 'test reason');
  assert.match(document.body.textContent || '', new RegExp(id));
  await click('回读退款结果');
  await click('按原编号重试');
  assert.equal(sent.length, 2);
  assert.deepEqual(sent[0], sent[1]);
});

test('unknown refund readback adopts an existing original refund and never sends another request', async () => {
  const api = fixture();
  let sent = 0,
    id = '';
  api.createRefund = async (_no, input) => {
    sent++;
    id = input.refundId;
    throw new ApiError(503, 'PAYMENT_COMMIT_UNKNOWN', 'unknown');
  };
  await mount(<OperationsPaymentOrdersPage api={api} />);
  await click('查看详情');
  await click('发起原路退款');
  await fill('退款积分数', '40');
  await fill('退款原因', 'test reason');
  await click('计算并核对退款');
  await click('确认退款');
  api.orderDetails = async () => ({ ...details(), refunds: [refund(id)] });
  await click('回读退款结果');
  assert.equal(sent, 1);
  assert.match(document.body.textContent || '', /PR-fixture/);
  assert.equal(buttons('发起原路退款').length, 0);
});

test('package switches retain checked drafts on reopen, submit booleans and stay disabled during an unknown write', async () => {
  const api = fixture();
  const sent: Parameters<PaymentAPI['savePackage']>[0][] = [];
  let reject!: (error: Error) => void;
  api.savePackage = (input) => {
    sent.push(input);
    return new Promise((_resolve, fail) => { reject = fail; });
  };
  await mount(<OperationsPaymentPackagesPage api={api} />);
  await click('编辑档位');
  assert.equal(buttons('读取最新档位').length, 0);
  assert.equal(document.querySelectorAll('[role=dialog] .op-payment-buttons button').length, 1);
  const enabled = () => button('上架档位');
  const testOnly = () => button('仅非正式环境运营可见');
  assert.equal(enabled().getAttribute('role'), 'switch');
  assert.equal(testOnly().getAttribute('role'), 'switch');
  assert.equal(enabled().type, 'button');
  assert.equal(testOnly().type, 'button');
  assert.equal(enabled().getAttribute('aria-checked'), 'true');
  assert.equal(testOnly().getAttribute('aria-checked'), 'false');
  await click('上架档位');
  await click('仅非正式环境运营可见');
  assert.equal(sent.length, 0, 'switches must not submit the form');
  await click('关闭');
  await click('编辑档位');
  assert.equal(enabled().getAttribute('aria-checked'), 'false');
  assert.equal(testOnly().getAttribute('aria-checked'), 'true');
  await click('保存充值档位');
  assert.equal(sent.length, 1);
  assert.equal(sent[0].enabled, false);
  assert.equal(sent[0].testOnly, true);
  assert.equal(enabled().disabled, true);
  assert.equal(testOnly().disabled, true);
  await act(async () => reject(new ApiError(503, 'PAYMENT_COMMIT_UNKNOWN', 'fixture unknown switches')));
  await flush();
  await click('关闭');
  await click('编辑档位');
  assert.equal(enabled().disabled, true);
  assert.equal(testOnly().disabled, true);
  assert.equal(enabled().getAttribute('aria-checked'), 'false');
  assert.equal(testOnly().getAttribute('aria-checked'), 'true');
  await click('上架档位');
  assert.equal(enabled().getAttribute('aria-checked'), 'false');
  assert.equal(sent.length, 1);
});

test('package conflict keeps its draft locked without reload controls; page remount reads the actual revision', async () => {
  const api = fixture();
  let saves = 0,
    reads = 0,
    revision = 0;
  api.packages = async () => {
    reads++;
    return [pack];
  };
  api.savePackage = async (input) => {
    saves++;
    revision = input.revision;
    if (saves === 1) throw new ApiError(409, 'PAYMENT_CONFLICT', 'fixture conflict');
    return {
      ...input,
      revision: input.revision + 1,
      createdAt: at,
      updatedAt: at,
    };
  };
  await mount(<OperationsPaymentPackagesPage api={api} />);
  await click('编辑档位');
  await fill('原价（元）', '12.34');
  await click('保存充值档位');
  assert.equal(field('原价（元）').value, '12.34');
  assert.equal(button('保存充值档位').disabled, true);
  assert.equal(saves, 1);
  assert.equal(revision, 3);
  assert.equal(button('上架档位').disabled, true);
  assert.equal(button('仅非正式环境运营可见').disabled, true);
  await click('关闭');
  await click('编辑档位');
  assert.equal(field('原价（元）').value, '12.34');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
  assert.equal(button('保存充值档位').disabled, true);
  assert.equal(saves, 1);
  assert.equal(reads, 1, 'closing/reopening must not silently replace the draft');
  assert.match(document.querySelector('[role=alert]')?.textContent || '', /保存结果暂未确认，请刷新页面后核对。/);
  api.packages = async () => {
    reads++;
    return [{ ...pack, revision: 7, amountFen: 1500, originalAmountFen: 1500 }];
  };
  assert.equal(buttons('读取最新档位').length, 0);
  assert.equal(saves, 1);
  await mount(<OperationsPaymentPackagesPage api={api} />);
  assert.equal(saves, 1, 'page reload must only read, never resend');
  assert.equal(reads, 2);
  await click('编辑档位');
  assert.equal(field('原价（元）').value, '15.00');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
  await click('保存充值档位');
  assert.equal(saves, 2);
  assert.equal(revision, 7);
});

test('daily limit zero is explicit unlimited; unknown save stays locked until page remount reads saved facts', async () => {
  const api = fixture();
  let saves = 0,
    reads = 0,
    amount = -1;
  api.settings = async () => {
    reads++;
    return { revision: 4, dailyLimitFen: 500000 };
  };
  api.saveSettings = async (input) => {
    saves++;
    amount = input.dailyLimitFen;
    throw new ApiError(503, 'PAYMENT_COMMIT_UNKNOWN', 'unknown settings');
  };
  await mount(<OperationsPaymentPackagesPage api={api} />);
  assert.equal(document.querySelector('[role=dialog]'), null);
  await click('设置单日限额');
  assert.equal(buttons('读取最新限额').length, 0);
  assert.equal(document.querySelectorAll('[role=dialog] .op-payment-buttons button').length, 1);
  await fill('单日限额（元）', '0');
  await click('保存单日限额');
  assert.equal(saves, 1);
  assert.equal(amount, 0);
  assert.equal(field('单日限额（元）').value, '0');
  assert.equal(button('保存单日限额').disabled, true);
  await click('关闭');
  await click('设置单日限额');
  assert.equal(field('单日限额（元）').value, '0');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
  assert.equal(button('保存单日限额').disabled, true);
  assert.equal(field('单日限额（元）').disabled, true);
  assert.match(document.body.textContent || '', /保存结果暂未确认，请刷新页面后核对。/);
  assert.equal(saves, 1);
  assert.equal(reads, 1, 'unknown writes must not trigger automatic reads or reset drafts');
  api.settings = async () => {
    reads++;
    return { revision: 9, dailyLimitFen: 2500 };
  };
  assert.equal(buttons('读取最新限额').length, 0);
  await mount(<OperationsPaymentPackagesPage api={api} />);
  assert.equal(saves, 1, 'page reload must not repeat the uncertain save');
  assert.equal(reads, 2);
  await click('设置单日限额');
  assert.equal(field('单日限额（元）').value, '25.00');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
});

test('page reload after unknown config saves keeps failed GETs unavailable until explicit initial-read retry succeeds', async () => {
  const api = fixture();
  let phase: 'initial' | 'unavailable' | 'recovered' = 'initial';
  let packageReads = 0,
    settingsReads = 0,
    writes = 0;
  api.packages = async () => {
    packageReads++;
    if (phase === 'unavailable') throw new Error('fixture packages unavailable');
    return [phase === 'recovered' ? { ...pack, revision: 7, amountFen: 1500, originalAmountFen: 1500 } : pack];
  };
  api.settings = async () => {
    settingsReads++;
    if (phase === 'unavailable') throw new Error('fixture settings unavailable');
    return phase === 'recovered'
      ? { revision: 9, dailyLimitFen: 2500 }
      : { revision: 4, dailyLimitFen: 500000 };
  };
  api.savePackage = async () => {
    writes++;
    throw new ApiError(503, 'PAYMENT_COMMIT_UNKNOWN', 'fixture unknown package');
  };
  api.saveSettings = async () => {
    writes++;
    throw new ApiError(503, 'PAYMENT_COMMIT_UNKNOWN', 'fixture unknown settings');
  };
  await mount(<OperationsPaymentPackagesPage api={api} />);
  await click('编辑档位');
  await fill('原价（元）', '12.34');
  await click('保存充值档位');
  assert.equal(button('保存充值档位').disabled, true);
  await click('关闭');
  await click('设置单日限额');
  await fill('单日限额（元）', '20.00');
  await click('保存单日限额');
  assert.equal(button('保存单日限额').disabled, true);
  assert.equal(writes, 2);
  assert.equal(packageReads, 1);
  assert.equal(settingsReads, 1);

  phase = 'unavailable';
  await mount(<OperationsPaymentPackagesPage api={api} />);
  assert.equal(packageReads, 2);
  assert.equal(settingsReads, 2);
  assert.equal(writes, 2, 'reload must not resend either uncertain command');
  assert.equal(document.querySelector('[role=dialog]'), null);
  assert.equal(buttons('编辑档位').length, 0, 'no saved package is invented after a failed GET');
  assert.equal(button('新增充值档位').disabled, true);
  assert.equal(button('设置单日限额').disabled, true);
  assert.equal(buttons('保存充值档位').length, 0);
  assert.equal(buttons('保存单日限额').length, 0);
  assert.equal(document.querySelectorAll('[role=alert]').length, 2);
  assert.doesNotMatch(document.body.textContent || '', /当前条件下暂无记录/);
  await flush();
  assert.equal(writes, 2);
  assert.equal(packageReads, 2, 'initial-read failures must not automatically retry');
  assert.equal(settingsReads, 2);

  phase = 'recovered';
  for (const error of ['fixture settings unavailable', 'fixture packages unavailable']) {
    const feedback = [...document.querySelectorAll<HTMLElement>('[role=alert]')].find((node) =>
      node.textContent?.includes(error),
    );
    assert.ok(feedback);
    const retry = feedback.querySelector<HTMLButtonElement>('button');
    assert.equal(retry?.textContent, '重新读取');
    await act(async () => retry!.click());
    await flush();
  }
  assert.equal(packageReads, 3);
  assert.equal(settingsReads, 3);
  assert.equal(writes, 2, 'successful read retries still cannot resend writes');
  assert.equal(document.querySelector('[role=alert]'), null);
  assert.equal(button('新增充值档位').disabled, false);
  await click('编辑档位');
  assert.equal(field('原价（元）').value, '15.00');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
  assert.equal(button('保存充值档位').disabled, false, 'configuration saves do not require a reason');
  assert.equal(buttons('读取最新档位').length, 0);
  await click('关闭');
  await click('设置单日限额');
  assert.equal(field('单日限额（元）').value, '25.00');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
  assert.equal(button('保存单日限额').disabled, false);
  assert.equal(buttons('读取最新限额').length, 0);
  assert.equal(writes, 2);
});

test('anomaly refresh has one concise accessible name and rereads the current filters', async () => {
  const api = fixture();
  const requests: Parameters<PaymentAPI['anomalies']>[] = [];
  const read = api.anomalies;
  api.anomalies = (...input) => {
    requests.push(input);
    return read(...input);
  };
  await mount(<OperationsPaymentAnomaliesPage api={api} />);
  await choose('异常状态', '已核查');
  const before = requests.length;
  const current = requests.at(-1)!;
  assert.equal(button('刷新').textContent, '刷新');
  assert.equal(buttons('刷新异常列表').length, 0);
  await click('刷新');
  await flush();
  assert.equal(requests.length, before + 1);
  assert.deepEqual(requests.at(-1)?.slice(0, 3), current.slice(0, 3));
});

test('anomalies require explanation, retain runtime details and expose only permitted actions', async () => {
  const api = fixture();
  let note = '',
    query = '';
  api.resolveAnomaly = async (_id, value) => {
    note = value;
    return { ...anomaly, status: 'resolved', note: value, resolvedAt: at };
  };
  api.queryOrder = async (_no, value) => {
    query = value;
    return order;
  };
  await mount(<OperationsPaymentAnomaliesPage api={api} />);
  await click('核查详情');
  assert.equal(button('标记已核查').disabled, true);
  assert.equal(button('重新查单').disabled, true);
  assert.match(document.body.textContent || '', /PAYMENT_REFUND_RESTORE_PENDING/);
  assert.equal(buttons('手工加积分').length, 0);
  await fill('重新查单原因', 'investigate channel');
  await click('重新查单');
  assert.equal(query, 'investigate channel');
  await fill('核查说明', 'fixture explanation');
  await click('标记已核查');
  assert.equal(note, 'fixture explanation');
  assert.equal(buttons('标记已核查').length, 0);
  assert.match(document.body.textContent || '', /fixture explanation/);
});

test('permission failures are errors, never empty records or success', async () => {
  const api = fixture();
  api.operationsOrders = async () => {
    throw new ApiError(403, 'ROLE_FORBIDDEN', 'fixture forbidden');
  };
  await mount(<OperationsPaymentOrdersPage api={api} />);
  assert.match(document.querySelector('[role=alert]')?.textContent || '', /fixture forbidden/);
  assert.equal(buttons('查看详情').length, 0);
  assert.doesNotMatch(document.body.textContent || '', /当前条件下暂无记录/);
});

test('late order details cannot reappear after close or across session epoch', async () => {
  const api = fixture();
  let resolve!: (v: PaymentOrderDetails) => void;
  api.orderDetails = () =>
    new Promise((done) => {
      resolve = done;
    });
  await mount(<OperationsPaymentOrdersPage api={api} />);
  await click('查看详情');
  await click('关闭');
  await act(async () => resolve(details({ ...order, buyerUserId: 'old-private-buyer' })));
  await flush();
  assert.doesNotMatch(document.body.textContent || '', /old-private-buyer/);
  await click('查看详情');
  await act(async () => sessionBoundary.reset());
  await flush();
  await act(async () => resolve(details({ ...order, buyerUserId: 'old-session-private' })));
  await flush();
  assert.doesNotMatch(document.body.textContent || '', /old-session-private/);
  assert.equal(document.querySelector('[role=dialog]'), null);
});

test('unknown refund keeps its original ID across a page remount in the same account and uses existing account draft cleanup', async () => {
  const { SessionOptionalContext } = await import('../src/state/SessionContext');
  const { clearAccountDrafts } = await import('../src/api/session-boundary');
  const api = fixture();
  let id = '',
    sends = 0;
  api.createRefund = async (_no, input) => {
    id = input.refundId;
    sends++;
    throw new ApiError(503, 'PAYMENT_COMMIT_UNKNOWN', 'fixture unknown');
  };
  const scoped = () => (
    <SessionOptionalContext.Provider
      value={
        { user: { identity: 'persist-operator-fixture' } } as React.ContextType<
          typeof SessionOptionalContext
        >
      }
    >
      <OperationsPaymentOrdersPage api={api} />
    </SessionOptionalContext.Provider>
  );
  await mount(scoped());
  await click('查看详情');
  await click('发起原路退款');
  await fill('退款积分数', '40');
  await fill('退款原因', 'test reason');
  await click('计算并核对退款');
  await click('确认退款');
  const key = 'drama:credit-command:payment-refunds:persist-operator-fixture';
  assert.match(sessionStorage.getItem(key) || '', new RegExp(id));
  await mount(scoped());
  await click('查看详情');
  await click('发起原路退款');
  assert.equal(field('退款积分数').value, '40');
  assert.equal(field('退款原因').value, 'test reason');
  assert.match(document.body.textContent || '', new RegExp(id));
  await click('回读退款结果');
  assert.equal(sends, 1);
  assert.ok(button('按原编号重试'));
  clearAccountDrafts(sessionStorage, 'operations');
  assert.equal(sessionStorage.getItem(key), null);
});

test('non-refund failed audit events use generic failure copy and created/processing remain generic', async () => {
  const { paymentEventLabel } = await import('../src/pages/operations/payment-controls');
  const english = (key: string) =>
    ({ 操作失败: 'Failed', 已创建: 'Created', 处理中: 'Processing' })[key] || key;
  assert.equal(paymentEventLabel('failed', english), 'Failed');
  assert.equal(paymentEventLabel('created', english), 'Created');
  assert.equal(paymentEventLabel('processing', english), 'Processing');
  assert.equal(paymentEventLabel('pending', english), 'Processing');
  assert.equal(paymentEventLabel('future_channel_state', english), 'future_channel_state');
  assert.equal(paymentEventLabel('succeeded', english), '退款成功');
  const api = fixture();
  api.orderDetails = async () => ({
    ...details(),
    events: [
      {
        id: 'simulate-failed',
        source: 'simulate',
        outcome: 'failed',
        facts: { state: 'failed' },
        createdAt: at,
      },
      {
        id: 'query-failed',
        source: 'query',
        outcome: 'failed',
        facts: { state: 'failed' },
        createdAt: at,
      },
      { id: 'order-created', source: 'create', outcome: 'created', facts: {}, createdAt: at },
    ],
  });
  await mount(<OperationsPaymentOrdersPage api={api} />);
  await click('查看详情');
  const audit =
    [...document.querySelectorAll('.op-payment-card')].find((card) =>
      card.querySelector('h3')?.textContent?.includes('回调、查单与审计记录'),
    )?.textContent || '';
  assert.match(audit, /演示模拟 · 操作失败/);
  assert.match(audit, /渠道查单 · 操作失败/);
  assert.match(audit, /创建订单 · 已创建/);
  assert.doesNotMatch(audit, /退款失败/);
});

test('empty payment collections render one spanning cell inside tbody; loading and errors stay distinct', async () => {
  const api = fixture();
  api.operationsOrders = async () => ({ items: [], total: 0, page: 1, pageSize: 20 });
  api.anomalies = async () => ({ items: [], total: 0, page: 1, pageSize: 20 });
  api.packages = async () => [];
  for (const [node, columns] of [
    [<OperationsPaymentOrdersPage api={api} />, 7],
    [<OperationsPaymentAnomaliesPage api={api} />, 6],
    [<OperationsPaymentPackagesPage api={api} />, 9],
  ] as const) {
    await mount(node);
    const empty = document.querySelector('tbody td[role=status]');
    assert.ok(empty);
    assert.equal(empty.textContent?.trim(), '当前条件下暂无记录');
    assert.equal(empty.getAttribute('colspan'), String(columns));
    assert.equal(document.querySelectorAll('thead th').length, columns, 'empty cell spans every column');
    assert.equal(document.querySelectorAll('tbody tr').length, 1);
    assert.equal(document.querySelector('.op-feedback'), null);
  }
  let resolve!: (value: Awaited<ReturnType<PaymentAPI['anomalies']>>) => void;
  api.anomalies = () =>
    new Promise((done) => {
      resolve = done;
    });
  await mount(<OperationsPaymentAnomaliesPage api={api} />);
  assert.match(document.querySelector('[role=status]')?.textContent || '', /正在读取/);
  assert.doesNotMatch(document.body.textContent || '', /当前条件下暂无记录/);
  await act(async () => resolve({ items: [], total: 0, page: 1, pageSize: 20 }));
  await flush();
  assert.ok(document.querySelector('tbody td[role=status]'));
  api.anomalies = async () => {
    throw new Error('fixture unavailable');
  };
  await mount(<OperationsPaymentAnomaliesPage api={api} />);
  assert.match(document.querySelector('[role=alert]')?.textContent || '', /fixture unavailable/);
  assert.doesNotMatch(document.body.textContent || '', /当前条件下暂无记录/);
});

test('daily limit late conflict while closed keeps its draft locked on reopen; page remount reads the new revision', async () => {
  const api = fixture();
  let reject!: (reason: Error) => void;
  let saves = 0;
  let revision = 0;
  api.saveSettings = (input) => {
    saves++;
    revision = input.revision;
    return new Promise((_resolve, failed) => {
      reject = failed;
    });
  };
  await mount(<OperationsPaymentPackagesPage api={api} />);
  await click('设置单日限额');
  await fill('单日限额（元）', '12.34');
  await click('保存单日限额');
  assert.equal(saves, 1);
  assert.equal(revision, 4);
  await click('关闭');
  await act(async () => reject(new ApiError(409, 'PAYMENT_CONFLICT', 'fixture late conflict')));
  await flush();
  await click('设置单日限额');
  assert.equal(field('单日限额（元）').value, '12.34');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
  assert.equal(button('保存单日限额').disabled, true);
  assert.equal(saves, 1);
  api.settings = async () => ({ revision: 11, dailyLimitFen: 9900 });
  assert.equal(buttons('读取最新限额').length, 0);
  await mount(<OperationsPaymentPackagesPage api={api} />);
  assert.equal(saves, 1);
  await click('设置单日限额');
  assert.equal(field('单日限额（元）').value, '99.00');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
  assert.equal(field('单日限额（元）').disabled, false);
  api.saveSettings = async (input) => {
    saves++;
    revision = input.revision;
    return { revision: input.revision + 1, dailyLimitFen: input.dailyLimitFen };
  };
  await click('保存单日限额');
  assert.equal(saves, 2);
  assert.equal(revision, 11);
  assert.equal(document.querySelector('[role=dialog]'), null);
});

function HistoryControls() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output data-testid="payment-route">{location.pathname}</output>
      <button onClick={() => void navigate(-1)}>fixture back</button>
      <button onClick={() => void navigate(1)}>fixture forward</button>
    </>
  );
}

test('one payment sidebar entry follows direct links, keyboard tabs, and browser back/forward with a single heading', async () => {
  const saved = { ...paymentApi };
  Object.assign(paymentApi, fixture());
  const scoped = (path: string) => (
    <MemoryRouter initialEntries={[path]}>
      <SessionOptionalContext.Provider
        value={{ user: null } as React.ContextType<typeof SessionOptionalContext>}
      >
        <OperationsLayout />
        <HistoryControls />
      </SessionOptionalContext.Provider>
    </MemoryRouter>
  );
  try {
    await mount(scoped('/operations/payments/anomalies'));
    const sidebar = document.querySelector('nav[aria-label="运营导航"]')!;
    assert.equal(sidebar.querySelectorAll('a[aria-label="支付管理"]').length, 1);
    assert.equal(
      sidebar.querySelectorAll('a[aria-label="支付订单"],a[aria-label="支付异常"]').length,
      0,
    );
    assert.equal(
      sidebar.querySelector('a[aria-current=page]')?.getAttribute('aria-label'),
      '支付管理',
    );
    assert.equal(document.querySelectorAll('h1').length, 1);
    assert.equal(document.querySelector('h1')?.textContent, '支付管理');
    assert.equal(button('支付异常').getAttribute('aria-selected'), 'true');
    assert.equal(
      document.querySelector('[role=tabpanel]')?.getAttribute('aria-labelledby'),
      button('支付异常').id,
    );
    assert.ok(document.querySelector('[aria-label="支付异常列表"]'));
    await act(async () =>
      button('支付异常').dispatchEvent(
        new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }),
      ),
    );
    await flush();
    assert.equal(
      document.querySelector('[data-testid=payment-route]')?.textContent,
      '/operations/payments/orders',
    );
    assert.equal(button('支付订单').getAttribute('aria-selected'), 'true');
    assert.equal(document.activeElement, button('支付订单'));
    assert.ok(document.querySelector('[aria-label="支付订单列表"]'));
    await click('fixture back');
    assert.equal(button('支付异常').getAttribute('aria-selected'), 'true');
    await click('fixture forward');
    assert.equal(button('支付订单').getAttribute('aria-selected'), 'true');
    await act(async () =>
      button('支付订单').dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true })),
    );
    await flush();
    assert.equal(button('支付异常').getAttribute('aria-selected'), 'true');
    await act(async () =>
      button('支付异常').dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Home', bubbles: true }),
      ),
    );
    await flush();
    assert.equal(button('支付订单').getAttribute('aria-selected'), 'true');
    await mount(scoped('/operations/payments/packages'));
    assert.equal(
      document
        .querySelector('nav[aria-label="运营导航"] a[aria-current=page]')
        ?.getAttribute('aria-label'),
      '充值档位与限额',
    );
    assert.equal(document.querySelector('[role=tablist]'), null);
  } finally {
    Object.assign(paymentApi, saved);
  }
});

test('new package unknown result retains its identity and block across editors; page remount reads saved facts', async () => {
  const api = fixture();
  const sent: { id: string; revision: number; amountFen: number }[] = [];
  let reject!: (reason: Error) => void;
  api.savePackage = (input) => {
    sent.push(input);
    return new Promise((_resolve, failed) => {
      reject = failed;
    });
  };
  await mount(<OperationsPaymentPackagesPage api={api} />);
  await click('新增充值档位');
  await fill('档位名称', '新增优惠档位');
  assert.doesNotMatch(document.querySelector('[role=dialog]')?.textContent || '', /档位 ID/);
  await fill('原价（元）', '12.34');
  await fill('充值积分数', '120');
  await fill('赠送积分数', '12');
  await click('保存充值档位');
  assert.equal(sent.length, 1);
  const generatedId = sent[0].id;
  assert.match(generatedId, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  assert.equal(sent[0].revision, 0);
  await click('关闭');
  await click('编辑档位');
  assert.equal(field('档位名称').value, pack.name);
  assert.equal(field('原价（元）').value, '9.99');
  await fill('原价（元）', '20.00');
  await act(async () =>
    reject(new ApiError(503, 'PAYMENT_COMMIT_UNKNOWN', 'fixture new package unknown')),
  );
  await flush();
  assert.doesNotMatch(
    document.querySelector('[role=dialog]')?.textContent || '',
    /保存结果暂未确认，请刷新页面后核对。/,
  );
  await click('关闭');
  await click('新增充值档位');
  assert.equal(field('档位名称').value, '新增优惠档位');
  assert.equal(field('原价（元）').value, '12.34');
  assert.equal(field('充值积分数').value, '120');
  assert.equal(field('赠送积分数').value, '12');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
  assert.equal(button('保存充值档位').disabled, true);
  assert.equal(field('档位名称').disabled, true);
  assert.match(document.body.textContent || '', /保存结果暂未确认，请刷新页面后核对。/);
  assert.equal(sent.length, 1);
  await click('关闭');
  await click('编辑档位');
  assert.equal(field('原价（元）').value, '20.00');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
  await click('关闭');
  await click('新增充值档位');
  api.packages = async () => [{ ...pack, id: generatedId, name: '新增优惠档位', revision: 8, amountFen: 1600, originalAmountFen: 1600 }];
  assert.equal(buttons('读取最新档位').length, 0);
  await mount(<OperationsPaymentPackagesPage api={api} />);
  assert.equal(sent.length, 1, 'reloading only reads the original new package');
  await click('编辑档位');
  assert.equal(field('档位名称').value, '新增优惠档位');
  assert.doesNotMatch(document.querySelector('[role=dialog]')?.textContent || '', /档位 ID/);
  assert.equal(field('原价（元）').value, '16.00');
  assert.equal(document.querySelector('[role=dialog] textarea'), null, 'configuration does not ask for a reason');
  api.savePackage = async (input) => {
    sent.push(input);
    return { ...input, revision: input.revision + 1, createdAt: at, updatedAt: at };
  };
  await click('保存充值档位');
  assert.equal(sent.length, 2);
  assert.equal(sent[1].id, generatedId);
  assert.equal(sent[1].revision, 8);
});

test('shared payment filters never submit on menu selection, dates cancel drafts and clear explicitly, pagination sends selected size', async () => {
  const api = fixture(),
    calls: { filter: unknown; page: number; size: number }[] = [];
  api.operationsOrders = async (filter, page = 1, size = 20) => {
    calls.push({ filter, page, size });
    return { items: [], total: 100, page, pageSize: size };
  };
  await mount(<OperationsPaymentOrdersPage api={api} />);
  assert.equal(document.querySelector('select,input[type="datetime-local"]'), null);
  await choose('支付渠道', '支付宝');
  assert.equal(calls.length, 1, 'menu open/select must not submit its enclosing form');
  await editDate('开始时间（北京时间）', '2026-10-19T09:15', false);
  assert.match(button('开始时间（北京时间）').textContent!, /选择日期和时间/);
  assert.equal(calls.length, 1);
  await editDate('开始时间（北京时间）', '2026-10-19T09:15');
  await click('查询订单');
  assert.deepEqual(calls.at(-1)?.filter, { channel: 'alipay', start: '2026-10-19T01:15:00.000Z' });
  await click('开始时间（北京时间）');
  await click('清除');
  await click('查询订单');
  assert.deepEqual(calls.at(-1)?.filter, { channel: 'alipay' });
  await click('下一页');
  assert.equal(calls.at(-1)?.page, 2);
  await choose('每页条数', '50');
  assert.deepEqual(calls.at(-1), { filter: { channel: 'alipay' }, page: 1, size: 50 });
});

test('shared anomaly menus keep independent status, kind and channel values and reset pagination', async () => {
  const api = fixture(),
    calls: unknown[] = [];
  api.anomalies = async (filter, page = 1, pageSize = 20) => {
    calls.push({ filter, page, pageSize });
    return { items: [], total: 100, page, pageSize };
  };
  await mount(<OperationsPaymentAnomaliesPage api={api} />);
  assert.equal(document.querySelector('select'), null);
  await choose('异常状态', '已核查');
  await choose('异常类型', '支付金额不符');
  await choose('支付渠道', '微信支付');
  await click('下一页');
  await choose('每页条数', '10');
  assert.deepEqual(calls.at(-1), {
    filter: { status: 'resolved', kind: 'amount_mismatch', channel: 'wechatpay' },
    page: 1,
    pageSize: 10,
  });
});


test('package names replace IDs and dual prices coexist with bonus credits without changing saved identity', async () => {
  const api = fixture();
  const sent: Parameters<PaymentAPI['savePackage']>[0][] = [];
  api.savePackage = async (input) => {
    sent.push(input);
    return { ...input, revision: input.revision + 1, createdAt: at, updatedAt: at };
  };
  api.packages = async () => [{ ...pack, name: '', originalAmountFen: 10000, amountFen: 8000 }];
  await mount(<OperationsPaymentPackagesPage api={api} />);
  assert.match(document.querySelector('tbody')?.textContent || '', /未命名/);
  assert.doesNotMatch(document.querySelector('tbody')?.textContent || '', /fixture-package/);
  await click('编辑档位');
  assert.equal(field('档位名称').value, '');
  assert.equal(field('原价（元）').value, '100.00');
  assert.equal(field('优惠价（元）').value, '80.00');
  await fill('档位名称', '  运营看得懂的名字  ');
  await fill('充值积分数', '1000');
  await fill('赠送积分数', '100');
  await click('关闭');
  await click('编辑档位');
  assert.equal(field('档位名称').value, '  运营看得懂的名字  ');
  assert.equal(field('优惠价（元）').value, '80.00');
  await click('保存充值档位');
  assert.equal(sent.length, 1);
  assert.deepEqual([sent[0].id, sent[0].name, sent[0].originalAmountFen, sent[0].amountFen, sent[0].rechargeCredits, sent[0].giftCredits], [pack.id, '运营看得懂的名字', 10000, 8000, 1000, 100]);
  await click('编辑档位');
  await fill('档位名称', '无折扣');
  await fill('优惠价（元）', '');
  await click('保存充值档位');
  assert.equal(sent[1].amountFen, 10000);
  assert.equal(sent[1].originalAmountFen, 10000);
});

test('invalid package prices and names fail locally, preserve editable drafts and never submit', async () => {
  const api = fixture();
  let sends = 0;
  api.savePackage = async (input) => {
    sends++;
    return { ...input, revision: input.revision + 1, createdAt: at, updatedAt: at };
  };
  await mount(<OperationsPaymentPackagesPage api={api} />);
  await click('编辑档位');
  for (const [original, discount] of [['0', ''], ['100', '0'], ['100', '100.01'], ['100', '80.001']]) {
    await fill('原价（元）', original);
    await fill('优惠价（元）', discount);
    await click('保存充值档位');
    assert.equal(sends, 0);
    assert.ok(document.querySelector('[role=alert]'));
    assert.equal(field('优惠价（元）').disabled, false);
  }
  await fill('原价（元）', '100');
  await fill('优惠价（元）', '80');
  for (const name of ['x'.repeat(49), '控制\u0007字符']) {
    await fill('档位名称', name);
    await click('保存充值档位');
    assert.equal(sends, 0);
    assert.match(document.querySelector('[role=alert]')?.textContent || '', /1–48/);
  }
  await fill('档位名称', '😀'.repeat(48));
  await click('保存充值档位');
  assert.equal(sends, 1);
});

test('operations order prices use frozen order facts and hide the technical package ID', async () => {
  const api = fixture();
  api.operationsOrders = async (_filters, page = 1, pageSize = 20) => ({ items: [{ ...order, amountFen: 8000, originalAmountFen: 10000 }], total: 1, page, pageSize });
  api.orderDetails = async () => ({ ...details({ ...order, amountFen: 8000, originalAmountFen: 10000 }), refunds: [refund('fixture-refund')] });
  await mount(<OperationsPaymentOrdersPage api={api} />);
  assert.equal(document.querySelector('tbody del')?.textContent, '¥100.00');
  assert.equal(document.querySelector('tbody .payment-price strong')?.textContent, '¥80.00');
  await click('查看详情');
  assert.equal(document.querySelector('[role=dialog] del')?.textContent, '¥100.00');
  assert.equal(document.querySelectorAll('[role=dialog] .payment-price').length, 1, 'the order payable must appear once, outside timeline and refunds');
  const refundsCard = [...document.querySelectorAll('.op-payment-card')].find((card) =>
    card.querySelector('h3')?.textContent?.includes('退款历史'),
  );
  assert.equal(refundsCard?.querySelector('.payment-price'), null);
  assert.match(refundsCard?.textContent || '', /¥3.99/);
  assert.doesNotMatch(document.querySelector('[role=dialog]')?.textContent || '', /档位 ID|fixture-package/);
});


test('duplicate and unnamed package labels retain separate stable row identities and editors', async () => {
  const api = fixture();
  const sent: Parameters<PaymentAPI['savePackage']>[0][] = [];
  api.savePackage = async (input) => {
    sent.push(input);
    return { ...input, revision: input.revision + 1, createdAt: at, updatedAt: at };
  };
  const warnings: unknown[][] = [];
  const originalError = console.error;
  console.error = (...args: unknown[]) => {
    if (args.some((value) => String(value).includes('same key'))) warnings.push(args);
    else originalError(...args);
  };
  try {
    for (const name of ['相同的运营名称', '']) {
      api.packages = async () => [{ ...pack, id: 'first', name }, { ...pack, id: 'second', name }];
      await mount(<OperationsPaymentPackagesPage api={api} />);
      assert.equal(document.querySelectorAll('tbody tr').length, 2);
      await act(async () => buttons('编辑档位')[1].click());
      await flush();
      await fill('档位名称', name || '补填名称');
      await click('保存充值档位');
      assert.equal(sent.at(-1)?.id, 'second');
      await act(async () => buttons('编辑档位')[0].click());
      await flush();
      assert.equal(field('档位名称').value, name, 'editing another row must not adopt the renamed row draft');
    }
    assert.equal(warnings.length, 0, 'non-unique display labels cannot serve as React row keys');
  } finally {
    console.error = originalError;
  }
});


test('configuration exposes only updated time and saves without a reason while retaining hidden CAS revisions', async () => {
 const api = fixture();
 const packages: Parameters<PaymentAPI['savePackage']>[0][] = [];
 const settings: Parameters<PaymentAPI['saveSettings']>[0][] = [];
 api.savePackage = async (input) => { packages.push(input); return { ...input, revision: input.revision + 1, createdAt: at, updatedAt: at }; };
 api.saveSettings = async (input) => { settings.push(input); return { ...input, revision: input.revision + 1 }; };
 await mount(<OperationsPaymentPackagesPage api={api} />);
 const headings = [...document.querySelectorAll('thead th')].map(node => node.textContent);
 assert.ok(headings.includes('更新时间'));
 assert.ok(headings.every(label => !label?.includes('版本')));
 const timeCell = document.querySelectorAll('tbody tr:first-child td')[7];
 assert.equal(timeCell.childElementCount, 0, 'the timestamp cell must not append an internal revision');
 await click('编辑档位');
 assert.equal(document.querySelector('[role=dialog] textarea'), null);
 assert.doesNotMatch(document.querySelector('[role=dialog]')?.textContent || '', /原因/);
 assert.equal(button('保存充值档位').disabled, false);
 await click('保存充值档位');
 assert.equal(packages.length, 1);
 assert.equal(packages[0].revision, pack.revision);
 assert.equal('reason' in packages[0], false);
 await click('设置单日限额');
 assert.equal(document.querySelector('[role=dialog] textarea'), null);
 assert.doesNotMatch(document.querySelector('[role=dialog]')?.textContent || '', /原因/);
 assert.equal(button('保存单日限额').disabled, false);
 await click('保存单日限额');
 assert.equal(settings.length, 1);
 assert.equal(settings[0].revision, 4);
 assert.equal('reason' in settings[0], false);
});
