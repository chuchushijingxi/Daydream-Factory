import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import type { CheckoutAPI } from '../src/features/payments/checkout';
import type {
  PaymentCreateOrderInput,
  PaymentOptions,
  PaymentOrder,
} from '../src/api/payment-types';
import { ApiError } from '../src/api/http-error';
import { Checkout } from '../src/features/payments/checkout';
import { qrPath, PaymentQRCode } from '../src/features/payments/PaymentQRCode';
import { renderToStaticMarkup } from 'react-dom/server';
const options: PaymentOptions = {
  channels: [
    { id: 'demo', demo: true },
    { id: 'wechatpay', demo: false },
  ],
  targets: [{ kind: 'personal', id: 'person', name: '' }],
  agreement: { version: '2026-10-20', agreed: false },
  packages: [
    {
      id: 'pkg',
      amountFen: 100,
      originalAmountFen: 100,
      rechargeCredits: 200,
      giftCredits: 10,
      revision: 1,
      sort: 1,
      enabled: true,
      testOnly: false,
      createdAt: '2026-10-20T00:00:00Z',
      updatedAt: '2026-10-20T00:00:00Z',
    },
    {
      id: 'pkg2',
      amountFen: 200,
      originalAmountFen: 200,
      rechargeCredits: 400,
      giftCredits: 0,
      revision: 1,
      sort: 2,
      enabled: true,
      testOnly: false,
      createdAt: '2026-10-20T00:00:00Z',
      updatedAt: '2026-10-20T00:00:00Z',
    },
  ],
};
const order = (patch: Partial<PaymentOrder> = {}): PaymentOrder => ({
  orderNo: 'PO20261020100000AAAAAAAAAA',
  buyerUserId: 'person',
  clientRequestId: 'request',
  target: options.targets[0],
  packageId: 'pkg',
  amountFen: 100,
  originalAmountFen: 100,
  rechargeCredits: 200,
  giftCredits: 10,
  channel: 'demo',
  status: 'pending',
  codeUrl: 'demo://order',
  channelTradeNo: '',
  agreementVersion: '2026-10-20',
  expiresAt: new Date(Date.now() + 900000).toISOString(),
  createdAt: '2026-10-20T00:00:00Z',
  creditedAt: null,
  paidAt: null,
  closedAt: null,
  refundedAt: null,
  ...patch,
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { resolve, promise };
}
function fake() {
  const creates: PaymentCreateOrderInput[] = [],
    closes: string[] = [],
    reads: string[] = [],
    events: string[] = [];
  const api: CheckoutAPI = {
    options: async () => structuredClone(options),
    createOrder: async (input) => {
      creates.push(input);
      events.push('create:' + input.packageId + ':' + input.channel);
      return order({
        clientRequestId: input.clientRequestId,
        channel: input.channel,
        packageId: input.packageId,
      });
    },
    closeOrder: async (no) => {
      closes.push(no);
      events.push('close:' + no);
      return order({ status: 'closed' });
    },
    getOrder: async (no) => {
      reads.push(no);
      return order();
    },
    simulate: async (_no, result) => order({ status: result === 'paid' ? 'credited' : 'pending' }),
  };
  return { api, creates, closes, reads, events };
}
async function ready(api: CheckoutAPI, valid = () => true) {
  const state = new Checkout(api, valid);
  await state.loadOptions();
  state.open();
  state.agree(true);
  return state;
}
const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://localhost/demo/app/#/credits',
  pretendToBeVisual: true,
});
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
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
  'KeyboardEvent',
])
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
globalThis.getComputedStyle = dom.window.getComputedStyle;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const { createRoot } = await import('react-dom/client');
const { MemoryRouter } = await import('react-router-dom');
const { SessionOptionalContext } = await import('../src/state/SessionContext');
const { RechargeProvider, RechargeButton } = await import('../src/features/payments/Recharge');
const { RechargeOrdersPage } = await import('../src/pages/payments/RechargeOrdersPage');
const { LegalDocumentPage, legalDocumentAt } = await import('../src/pages/legal/LegalDocumentPage');
let root: ReturnType<typeof createRoot> | undefined;
const session = {
  state: 'authenticated',
  user: { identity: 'person' },
  service: {},
  epoch: 0,
} as any;
after(async () => {
  if (root) await act(async () => root!.unmount());
  dom.window.close();
});
async function mount(children: React.ReactNode) {
  if (root) await act(async () => root!.unmount());
  document.getElementById('root')!.innerHTML = '';
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <MemoryRouter>
        <SessionOptionalContext.Provider value={session}>
          {children}
        </SessionOptionalContext.Provider>
      </MemoryRouter>,
    ),
  );
}
async function clickLabel(label: string) {
  const button = document.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`);
  assert.ok(button, label);
  await act(async () => button.click());
}
async function buyFirst() {
  const button = [...document.querySelectorAll<HTMLButtonElement>('.payment-package-buy')][0];
  assert.ok(button, '购买');
  await act(async () => button.click());
}
async function click(text: string) {
  const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === text,
  );
  assert.ok(button, text);
  await act(async () => button.click());
}
test('close while creating preserves request then closes late order, even if reopened', async () => {
  const f = fake(),
    pending = deferred<PaymentOrder>();
  f.api.createOrder = async (input) => {
    f.creates.push(input);
    return pending.promise;
  };
  const state = await ready(f.api),
    running = state.create();
  state.close();
  state.open();
  await state.change({ channel: 'wechatpay' });
  assert.equal(state.snapshot().selection.channel, 'demo');
  pending.resolve(order());
  await running;
  assert.deepEqual(f.closes, [order().orderNo]);
  assert.equal(state.snapshot().order?.status, 'closed');
  assert.equal(f.creates.length, 1);
});
test('unknown write freezes request and explicit retry uses same id after close and reopen', async () => {
  const f = fake();
  let calls = 0;
  const original = f.api.createOrder;
  f.api.createOrder = async (input) => {
    if (++calls === 1) {
      f.creates.push(input);
      throw new ApiError(0, 'NETWORK', 'unknown');
    }
    return original(input);
  };
  const state = await ready(f.api);
  await state.create();
  assert.equal(state.snapshot().uncertain, true);
  await state.change({ packageId: 'pkg2', channel: 'wechatpay' });
  assert.equal(state.snapshot().selection.packageId, 'pkg');
  state.close();
  state.open();
  await state.create();
  assert.deepEqual(f.creates[0], f.creates[1]);
  assert.equal(state.snapshot().uncertain, false);
});
test('definite rejection allows edit; absent agreement cannot create', async () => {
  const f = fake();
  let first = true;
  const original = f.api.createOrder;
  f.api.createOrder = async (input) => {
    if (first) {
      first = false;
      f.creates.push(input);
      throw new ApiError(409, 'PAYMENT_DAILY_LIMIT', 'limit');
    }
    return original(input);
  };
  const state = await ready(f.api);
  state.agree(false);
  await state.create();
  assert.equal(f.creates.length, 0);
  state.agree(true);
  await state.create();
  assert.equal(state.snapshot().uncertain, false);
  await state.change({ packageId: 'pkg2' });
  await state.create();
  assert.notEqual(f.creates[0].clientRequestId, f.creates[1].clientRequestId);
  assert.equal(f.creates[1].packageId, 'pkg2');
});
test('account fence prevents old create cleanup with new Cookie', async () => {
  const f = fake(),
    pending = deferred<PaymentOrder>();
  let active = true;
  f.api.createOrder = async () => pending.promise;
  const state = await ready(f.api, () => active),
    running = state.create();
  state.close();
  active = false;
  state.invalidate();
  pending.resolve(order());
  await running;
  assert.deepEqual(f.closes, []);
  assert.equal(state.snapshot().open, false);
  assert.equal(state.snapshot().order, null);
});
test('switch closes old order before recreating another channel/package', async () => {
  const f = fake(),
    state = await ready(f.api);
  await state.create();
  await state.change({ channel: 'wechatpay', packageId: 'pkg2' });
  assert.deepEqual(
    f.events.map((x) => x.split(':')[0]),
    ['create', 'close', 'create'],
  );
  assert.equal(f.creates[1].channel, 'wechatpay');
  assert.equal(f.creates[1].packageId, 'pkg2');
  assert.notEqual(f.creates[0].clientRequestId, f.creates[1].clientRequestId);
});
test('failed close keeps old order and blocks creation until explicit retry', async () => {
  const f = fake(),
    original = f.api.closeOrder;
  let fail = true;
  f.api.closeOrder = async (no) => {
    if (fail) throw new ApiError(503, 'PAYMENT_CHANNEL_FAILED', 'unavailable');
    return original(no);
  };
  const state = await ready(f.api);
  await state.create();
  await state.change({ packageId: 'pkg2' });
  assert.equal(state.snapshot().needsClose, true);
  assert.equal(state.snapshot().selection.packageId, 'pkg');
  await state.create();
  assert.equal(f.creates.length, 1);
  fail = false;
  await state.closeKnownOrder();
  await state.create();
  assert.equal(f.creates.length, 2);
});
test('read poll cannot regress credited state and terminal/closed dialogs stop polling', async () => {
  const f = fake(),
    pending = deferred<PaymentOrder>();
  f.api.getOrder = async (no) => {
    f.reads.push(no);
    return pending.promise;
  };
  const state = await ready(f.api);
  await state.create();
  const polling = state.poll();
  await state.simulate('paid');
  pending.resolve(order());
  await polling;
  assert.equal(state.snapshot().order?.status, 'credited');
  await state.poll();
  assert.equal(f.reads.length, 1);
  state.close();
  await state.poll();
  assert.equal(f.closes.length, 0);
  assert.equal(f.reads.length, 1);
});
test('demo failure stays pending and permits retry, reopening permits new recharge', async () => {
  const f = fake(),
    state = await ready(f.api);
  await state.create();
  await state.simulate('failed');
  assert.equal(state.snapshot().order?.status, 'pending');
  assert.match(state.snapshot().notice, /模拟支付失败/);
  await state.simulate('paid');
  assert.equal(state.snapshot().order?.status, 'credited');
  state.close();
  state.open();
  assert.equal(state.snapshot().order, null);
});
test('updated agreement requires consent and empty channels remain empty', async () => {
  const f = fake(),
    state = await ready(f.api);
  f.api.options = async () => ({
    ...options,
    channels: [],
    agreement: { version: 'next', agreed: false },
  });
  await state.loadOptions();
  assert.equal(state.snapshot().selection.agreed, false);
  assert.equal(state.snapshot().options?.channels.length, 0);
});
test('local QR uses four-module quiet zone, black on white, no remote image, explicit invalid error', () => {
  const qr = qrPath('https://offline.example/test'),
    modules = qr.size - 8;
  for (const match of qr.path.matchAll(/M(\d+) (\d+)h1v1h-1z/g)) {
    assert.ok(Number(match[1]) >= 4 && Number(match[1]) < modules + 4);
    assert.ok(Number(match[2]) >= 4 && Number(match[2]) < modules + 4);
  }
  const html = renderToStaticMarkup(<PaymentQRCode value="weixin://offline-test" />);
  assert.match(html, /fill="#fff"/);
  assert.match(html, /fill="#000"/);
  assert.doesNotMatch(html, /<img/);
  assert.match(renderToStaticMarkup(<PaymentQRCode value="" />), /role="alert"/);
});
test('recharge sheet buys without a checkbox under the purchase terms and supports demo failure/success', async () => {
  const f = fake();
  await mount(
    <RechargeProvider api={f.api}>
      <RechargeButton />
    </RechargeProvider>,
  );
  await click('充值');
  assert.ok(document.querySelector('[role="dialog"].payment-sheet'));
  assert.equal(document.querySelector('input[type="checkbox"]'), null);
  assert.match(document.querySelector('.payment-sheet-terms')!.textContent!, /购买即代表同意《积分充值协议》/);
  const agreement = document.querySelector<HTMLAnchorElement>('.payment-sheet-terms a')!;
  assert.match(agreement.getAttribute('href')!, /\/legal\/recharge$/);
  assert.equal(agreement.getAttribute('target'), '_blank', 'the agreement opens in a new tab');
  assert.equal(document.querySelector('a[href="#/payments/orders"]'), null, 'records live in the account menu');
  await buyFirst();
  assert.equal(f.creates[0].agreementVersion, '2026-10-20');
  assert.ok(document.querySelector('.payment-checkout-card'));
  assert.doesNotMatch(document.body.textContent!, /演示支付，不会真实扣款/);
  assert.equal(document.querySelector('svg.payment-qr'), null);
  await click('模拟失败');
  assert.match(document.body.textContent!, /模拟支付失败/);
  await click('模拟支付成功');
  assert.match(document.body.textContent!, /已到账 200 积分到个人空间/);
  await clickLabel('关闭');
  await act(async () => new Promise((resolve) => setTimeout(resolve, 10)));
  assert.equal(document.activeElement?.textContent, '充值');
  assert.equal(f.closes.length, 0);
});

test('empty channels hide entry; user records and placeholder protocol show required facts and server version', async () => {
  const f = fake();
  f.api.options = async () => ({ ...options, channels: [] });
  await mount(
    <RechargeProvider api={f.api}>
      <RechargeButton />
    </RechargeProvider>,
  );
  assert.equal(
    [...document.querySelectorAll('button')].some((b) => b.textContent === '充值'),
    false,
  );
  const api = {
    orders: async () => ({
      items: [order({ status: 'credited', channelTradeNo: 'fixture-trade' })],
      total: 1,
      page: 1,
      pageSize: 10,
    }),
  };
  await mount(<RechargeOrdersPage api={api} />);
  for (const text of [
    order().orderNo,
    '个人空间',
    '¥1.00',
    '+210',
    '含赠送 10',
    '已到账',
    '需要发票请联系客服并提供订单号',
  ])
    assert.ok(document.body.textContent?.includes(text), text);
  // 用户列表只保留关键字段：渠道交易号、协议版本等审计信息不在用户侧展示。
  for (const text of ['fixture-trade', '协议版本', '渠道交易号'])
    assert.ok(!document.body.textContent?.includes(text), text);
  assert.equal(document.querySelectorAll('.payment-records-row').length, 1);
  assert.equal(legalDocumentAt('/payments/agreement'), 'recharge', 'old agreement links still work');
  await mount(<LegalDocumentPage document="recharge" api={f.api} />);
  for (const text of [
    '研发占位稿',
    '2026-10-20',
    '不可提现',
    '不能转回个人空间',
    '监护人同意',
    '原则上不退款',
  ])
    assert.ok(document.body.textContent?.includes(text), text);
  // 独立文档页不挂任何平台外壳或入口。
  assert.equal(document.querySelector('.legal-page a, .legal-page button, .legal-page nav'), null);
  assert.equal(document.title, '积分充值协议');
  for (const [kind, title] of [
    ['terms', '用户服务协议'],
    ['privacy', '隐私政策'],
  ] as const) {
    await mount(<LegalDocumentPage document={kind} api={f.api} />);
    assert.equal(document.querySelector('.legal-document h1')?.textContent, title);
    assert.ok(document.querySelectorAll('.legal-document section').length >= 5);
    assert.equal(document.querySelector('.legal-document-version'), null);
  }
});

test('unknown command can retry unchanged after packages disappear and agreement version changes', async () => {
  const f = fake();
  let first = true;
  f.api.createOrder = async (input) => {
    f.creates.push(input);
    if (first) {
      first = false;
      throw new ApiError(503, 'PAYMENT_COMMIT_UNKNOWN', 'unknown');
    }
    return order();
  };
  const state = await ready(f.api);
  await state.create();
  f.api.options = async () => ({
    ...options,
    targets: [],
    packages: [],
    agreement: { version: 'next', agreed: false },
  });
  await state.loadOptions();
  await state.create();
  assert.equal(f.creates.length, 2);
  assert.deepEqual(f.creates[0], f.creates[1]);
});
test('closing during failed simulate still checks and closes the pending order', async () => {
  const f = fake(),
    pending = deferred<PaymentOrder>();
  f.api.simulate = async () => pending.promise;
  const state = await ready(f.api);
  await state.create();
  const simulation = state.simulate('paid');
  state.close();
  pending.resolve(order());
  await simulation;
  assert.equal(f.closes.length, 1);
  assert.equal(state.snapshot().order?.status, 'closed');
  const second = await ready(f.api);
  await second.create();
  let reject!: (error: unknown) => void;
  f.api.simulate = async () =>
    new Promise((_resolve, r) => {
      reject = r;
    });
  const failure = second.simulate('paid');
  second.close();
  reject(new ApiError(0, 'NETWORK', 'unknown'));
  await failure;
  assert.equal(f.closes.length, 2);
  assert.equal(second.snapshot().order?.status, 'closed');
});
test('actual sheet polls only the local GET after two seconds and stops on close', async () => {
  const f = fake();
  await mount(
    <RechargeProvider api={f.api}>
      <RechargeButton />
    </RechargeProvider>,
  );
  await click('充值');
  await buyFirst();
  assert.equal(f.reads.length, 0);
  await act(async () => new Promise((resolve) => setTimeout(resolve, 2100)));
  assert.equal(f.reads.length, 1);
  await clickLabel('关闭');
  assert.equal(f.closes.length, 1);
  await act(async () => new Promise((resolve) => setTimeout(resolve, 2100)));
  assert.equal(f.reads.length, 1);
});

test('late failed simulation cannot replace an order settled by same-dialog timeout close', async () => {
  for (const status of ['closed', 'credited', 'refunded'] as const) {
    const f = fake(),
      pending = deferred<PaymentOrder>();
    f.api.simulate = async () => pending.promise;
    f.api.closeOrder = async () => order({ status });
    const state = await ready(f.api);
    await state.create();
    const simulation = state.simulate('failed');
    await state.closeKnownOrder();
    assert.equal(state.snapshot().open, true);
    assert.equal(state.snapshot().order?.status, status);
    pending.resolve(order({ status: 'pending' }));
    await simulation;
    assert.equal(state.snapshot().order?.status, status);
    assert.equal(state.snapshot().notice, '');
  }
});

test('late simulation cannot replace a new order created after timeout close in same dialog', async () => {
  const f = fake(),
    pending = deferred<PaymentOrder>();
  const first = order(),
    next = order({ orderNo: 'PO20261020100000BBBBBBBBBB', clientRequestId: 'next' });
  f.api.simulate = async () => pending.promise;
  const state = await ready(f.api);
  await state.create();
  const simulation = state.simulate('failed');
  await state.closeKnownOrder();
  f.api.createOrder = async () => next;
  await state.create();
  pending.resolve(first);
  await simulation;
  assert.equal(state.snapshot().order?.orderNo, next.orderNo);
  assert.equal(state.snapshot().notice, '');
});

test('old simulate success or rejection cannot unlock or overwrite a newer pending create', async () => {
  for (const rejectOld of [false, true]) {
    const f = fake();
    let settleOld!: () => void;
    f.api.simulate = async () =>
      new Promise((resolve, reject) => {
        settleOld = () =>
          rejectOld
            ? reject(new ApiError(503, 'PAYMENT_CHANNEL_FAILED', 'old simulation error'))
            : resolve(order());
      });
    const state = await ready(f.api);
    await state.create();
    const simulation = state.simulate('failed');
    await state.closeKnownOrder();
    const pendingCreate = deferred<PaymentOrder>();
    f.api.createOrder = async () => pendingCreate.promise;
    const creating = state.create();
    assert.equal(state.snapshot().busy, true);
    const originalError = state.snapshot().error;
    settleOld();
    await simulation;
    assert.equal(state.snapshot().busy, true);
    assert.equal(state.snapshot().error, originalError);
    const next = order({ orderNo: 'PO20261020100000BBBBBBBBBB' });
    pendingCreate.resolve(next);
    await creating;
    assert.equal(state.snapshot().busy, false);
    assert.equal(state.snapshot().order?.orderNo, next.orderNo);
  }
});

test('old simulate rejection preserves a newer create error after the new write settles', async () => {
  const f = fake();
  let rejectOld!: (error: unknown) => void;
  f.api.simulate = async () =>
    new Promise((_resolve, reject) => {
      rejectOld = reject;
    });
  const state = await ready(f.api);
  await state.create();
  const simulation = state.simulate('failed');
  await state.closeKnownOrder();
  f.api.createOrder = async () => {
    throw new ApiError(409, 'PAYMENT_DAILY_LIMIT', 'new create error');
  };
  await state.create();
  assert.equal(state.snapshot().error, 'new create error');
  rejectOld(new ApiError(503, 'PAYMENT_CHANNEL_FAILED', 'old simulation error'));
  await simulation;
  assert.equal(state.snapshot().error, 'new create error');
});

test('cashier target switch sends the selected space, locks while paying and Escape returns before closing', async () => {
  const f = fake(),
    pending = deferred<PaymentOrder>();
  f.api.createOrder = async (input) => {
    f.creates.push(input);
    return pending.promise;
  };
  f.api.options = async () => ({
    ...options,
    targets: [...options.targets, { kind: 'team', id: 'team-fixture', name: '测试团队目标' }],
  });
  await mount(
    <RechargeProvider api={f.api}>
      <RechargeButton />
    </RechargeProvider>,
  );
  await click('充值');
  const radios = () => [...document.querySelectorAll<HTMLButtonElement>('[role="radiogroup"][aria-label="充值目标"] [role="radio"]')];
  assert.equal(document.querySelector('select'), null);
  assert.deepEqual(radios().map((r) => r.textContent), ['个人空间', '测试团队目标']);
  assert.equal(radios()[0].getAttribute('aria-checked'), 'true');
  await act(async () => radios()[1].click());
  assert.equal(radios()[1].getAttribute('aria-checked'), 'true');
  assert.match(document.querySelector('.payment-sheet-head p')!.textContent!, /到账至测试团队目标/);
  await buyFirst();
  assert.deepEqual(f.creates[0].target, { kind: 'team', id: 'team-fixture' });
  assert.equal(radios()[0].disabled, true);
  await act(async () => pending.resolve(order()));
  const escape = async () =>
    act(async () =>
      document.activeElement!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
      ),
    );
  await escape();
  assert.ok(document.querySelector('[role="dialog"]'), 'Escape first returns from payment to packages');
  assert.equal(document.querySelector('.payment-checkout-card'), null);
  assert.equal(f.closes.length, 1, 'leaving the payment layer closes the pending order');
  await escape();
  assert.equal(
    document.querySelector('[role="dialog"]'),
    null,
    'Escape subsequently closes cashier',
  );
  assert.equal(f.closes.length, 1);
});

test('recharge record shared page-size menu keeps server pagination and returns to first page', async () => {
  const calls: number[][] = [];
  await mount(
    <RechargeOrdersPage
      api={{
        orders: async (page = 1, size = 10) => {
          calls.push([page, size]);
          return { items: [], total: 100, page, pageSize: size };
        },
      }}
    />,
  );
  await click('下一页');
  assert.deepEqual(calls.at(-1), [2, 10]);
  const trigger = document.querySelector<HTMLButtonElement>(
    'button[aria-label="充值记录每页条数"]',
  )!;
  await act(async () =>
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })),
  );
  await act(async () => new Promise((resolve) => setTimeout(resolve, 10)));
  const size = [...document.querySelectorAll<HTMLElement>('[role="menuitemradio"]')].find(
    (node) => node.textContent === '50',
  )!;
  await act(async () => size.click());
  assert.deepEqual(calls.at(-1), [1, 50]);
  assert.equal(document.querySelector('select'), null);
});


test('checkout cards show the package name with discount and bonus badges; pending and history use frozen prices', async () => {
  const f = fake();
  f.api.options = async () => ({
    ...structuredClone(options),
    packages: [{ ...options.packages[0], amountFen: 8000, originalAmountFen: 10000, name: '内部运营专用名称' }],
  });
  f.api.createOrder = async (input) => {
    f.creates.push(input);
    return order({ clientRequestId: input.clientRequestId, amountFen: 7000, originalAmountFen: 9000 });
  };
  await mount(<RechargeProvider api={f.api}><RechargeButton /></RechargeProvider>);
  await click('充值');
  const card = () => document.querySelector('.payment-package-card')!;
  assert.equal(card().querySelector('.payment-price strong')?.textContent, '¥80.00');
  assert.equal(card().querySelector('del')?.textContent, '¥100.00');
  assert.equal(card().querySelector('.payment-package-total strong')?.textContent, '210');
  assert.match(card().textContent || '', /充值 200 \+ 赠送 10/);
  assert.match(card().textContent || '', /加赠5%/);
  assert.match(card().textContent || '', /立省¥20\.00/);
  assert.equal(card().querySelector('.payment-package-head h3')?.textContent, '内部运营专用名称');
  await buyFirst();
  assert.equal(document.querySelector('.payment-payable strong')?.textContent, '¥70.00');
  assert.equal(document.querySelector('.payment-payable del')?.textContent, '¥90.00');
  assert.equal(document.querySelector('.payment-payable strong')?.getAttribute('aria-label'), '实付金额 ¥70.00');
  assert.equal(document.querySelector('.payment-payable del')?.getAttribute('aria-label'), '原价 ¥90.00');
  await clickLabel('关闭');
  await mount(<RechargeOrdersPage api={{ orders: async () => ({ items: [order({ amountFen: 7000, originalAmountFen: 9000 })], total: 1, page: 1, pageSize: 10 }) }} />);
  assert.equal(document.querySelector('.payment-records-row .payment-price strong')?.textContent, '¥70.00');
  assert.equal(document.querySelector('.payment-records-row del')?.textContent, '¥90.00');
  await mount(<RechargeOrdersPage api={{ orders: async () => ({ items: [order()], total: 1, page: 1, pageSize: 10 }) }} />);
  assert.equal(document.querySelector('.payment-records-row del'), null, 'equal prices must not fabricate a discount');
});
test('opening from a team preselects that owned team, re-reads unknown targets and never retargets a pending order', async () => {
  const f = fake();
  let targets = [
    { kind: 'personal' as const, id: 'person', name: '' },
    { kind: 'team' as const, id: 'team_a', name: '团队A' },
  ];
  f.api.options = async () => ({ ...structuredClone(options), targets: structuredClone(targets) });
  const state = new Checkout(f.api, () => true);
  await state.loadOptions();
  assert.equal(state.snapshot().selection.target, 'personal:person');
  state.open('team:team_a');
  assert.equal(state.snapshot().selection.target, 'team:team_a');
  state.close();
  // 刚成为所有者的团队不在旧目标里：重新读取后再选中。
  targets = [...targets, { kind: 'team', id: 'team_b', name: '团队B' }];
  state.open('team:team_b');
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(state.snapshot().selection.target, 'team:team_b');
  state.agree(true);
  await state.create();
  assert.deepEqual(f.creates.at(-1)?.target, { kind: 'team', id: 'team_b' });
  state.open('personal:person');
  assert.equal(state.snapshot().selection.target, 'team:team_b');
  state.close();
  await new Promise((resolve) => setTimeout(resolve, 0));
  state.open('personal:person');
  assert.equal(state.snapshot().selection.target, 'personal:person');
});
