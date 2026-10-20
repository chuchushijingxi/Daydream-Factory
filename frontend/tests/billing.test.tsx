import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act, useState } from 'react';
import { JSDOM } from 'jsdom';
import type { CreditPriceDTO } from '../src/api/administration-types';
import { billingApi } from '../src/api/billing-client';
import { createAdministrationApi } from '../src/api/administration';
import { createHttpClient } from '../src/api/client';
import { chargeView } from '../src/api/model-usage-client';
import { chargeFormula } from '../src/pages/credits/credit-format';
const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/',
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
const { ModelCreditPricingFields } = await import(
  '../src/pages/model-config/ModelCreditPricingFields'
);
let root: ReturnType<typeof createRoot> | undefined;
after(async () => {
  if (root) await act(async () => root!.unmount());
  dom.window.close();
});
test('model integer price, free window and enable flag stay one configuration draft; invalid decimals cannot submit', async () => {
  let latest: CreditPriceDTO | null = null;
  function Form() {
    const [value, setValue] = useState<CreditPriceDTO | null>({
      unitPrice: 2,
      freeEnabled: false,
      freeStart: null,
      freeEnd: null,
    });
    return (
      <form>
        <ModelCreditPricingFields
          category="analysis"
          value={value}
          disabled={false}
          onChange={(v) => {
            latest = v;
            setValue(v);
          }}
        />
      </form>
    );
  }
  root = createRoot(document.getElementById('root')!);
  await act(async () => root!.render(<Form />));
  const field = document.querySelector<HTMLInputElement>('input[inputmode="numeric"]')!;
  const fill = async (v: string) =>
    act(() => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, v);
      field.dispatchEvent(new Event('input', { bubbles: true }));
    });
  await fill('1.5');
  assert.equal(document.querySelector('form')!.checkValidity(), false);
  assert.equal(latest, null);
  await fill('9007199254740992');
  assert.equal(document.querySelector('form')!.checkValidity(), false);
  await fill('3');
  assert.equal(latest!.unitPrice, 3);
  assert.equal(document.querySelector('form')!.checkValidity(), true);
  await act(async () => document.querySelector<HTMLButtonElement>('[role=switch]')!.click());
  assert.equal(latest!.freeEnabled, true);
  let valid = true;
  await act(async () => {
    valid = document.querySelector('form')!.checkValidity();
  });
  assert.equal(valid, false);
});
test('file and project quotes call read-only pricing endpoints and never start generation or compute authoritative amounts', async () => {
  const requests: any[] = [];
  const q = {
    basis: 'server_basis',
    modelId: 'm',
    connectionId: 'c',
    scope: 'source',
    sourceVersion: 1,
    inputRevision: 0,
    quantity: 12345,
    unit: 'characters',
    priceVersion: 1,
    rule: { unitPrice: 2, freeEnabled: false, freeStart: null, freeEnd: null },
    originalAmount: 247,
    amount: 247,
    free: false,
  };
  globalThis.fetch = async (url, options) => {
    requests.push({ url, body: JSON.parse(String(options?.body)) });
    return Response.json(q);
  };
  const settings = { llmModel: 'm', imageModel: '', videoModel: '', aspectRatio: '9:16' };
  assert.deepEqual(await billingApi.file('file_fiction', settings), q);
  assert.deepEqual(
    await billingApi.project('project /', {
      revision: 4,
      operation: 'single-visual',
      assetId: 'a',
    }),
    q,
  );
  assert.deepEqual(
    requests.map((r) => r.url),
    ['/api/projects/quote', '/api/projects/project%20%2F/quote'],
  );
  assert.deepEqual(requests[1].body, { revision: 4, operation: 'single-visual', assetId: 'a' });
  assert.equal(
    requests.some((r) => 'amount' in r.body || 'quantity' in r.body),
    false,
  );
});
test('media quotes keep server integer quantities and amounts, map image/second units and reject unsafe precision', async () => {
  let response: any;
  const bodies: any[] = [];
  globalThis.fetch = async (_url, options) => {
    bodies.push(JSON.parse(String(options?.body)));
    return Response.json(response);
  };
  const common = {
    basis: 'server_basis',
    modelId: 'mock',
    connectionId: 'c',
    scope: 'media-video',
    sourceVersion: 1,
    inputRevision: 4,
    priceVersion: 1,
    rule: { unitPrice: 10, freeEnabled: false, freeStart: null, freeEnd: null },
    free: false,
  };
  response = { ...common, quantity: 11, unit: 'second', originalAmount: 110, amount: 110 };
  const video = await billingApi.project('p', {
    revision: 4,
    operation: 'media',
    modelId: 'mock',
    target: { episodeId: 'e', shotId: 's' },
  });
  assert.equal(
    chargeFormula(chargeView({ ...video, id: 'c', scopeDigest: 'd', acceptedAt: '' })),
    '11 秒 × 10 积分/秒＝110 积分',
  );
  assert.equal('quantity' in bodies[0], false);
  response = {
    ...common,
    scope: 'media-image',
    quantity: 1,
    unit: 'image',
    originalAmount: 5,
    amount: 5,
    rule: { ...common.rule, unitPrice: 5 },
  };
  const image = await billingApi.project('p', {
    revision: 4,
    operation: 'media',
    modelId: 'mock',
    target: { assetId: 'a' },
  });
  assert.equal(chargeView({ ...image, id: 'c', scopeDigest: 'd', acceptedAt: '' }).unit, 'images');
  response = { ...response, amount: 9007199254740992 };
  await assert.rejects(
    billingApi.project('p', {
      revision: 4,
      operation: 'media',
      modelId: 'mock',
      target: { assetId: 'a' },
    }),
    (e) => (e as any).code === 'PROGRAM_FAILED',
  );
  response = { ...response, amount: 5, unit: 'second' };
  await assert.rejects(
    billingApi.project('p', {
      revision: 4,
      operation: 'media',
      modelId: 'mock',
      target: { assetId: 'a' },
    }),
    (e) => (e as any).code === 'PROGRAM_FAILED',
  );
});
test('usage filters and statistics follow server scope, calls omit task price, and extra response content is discarded', async () => {
  const record = {
    id: 't',
    taskId: 't',
    userId: 'u',
    username: '测试用户名',
    email: 'fiction@example.invalid',
    operation: 'asset-preparation-v1',
    status: 'running',
    modelId: 'm',
    connectionId: 'c',
    requestId: null,
    createdAt: '2026-10-16T00:00:00Z',
    durationMs: null,
    inputTokens: null,
    outputTokens: null,
    cacheHitTokens: null,
    mediaUsage: null,
    knownCostMicroCny: 123,
    unknownCost: true,
    callCount: 2,
    basis: null,
    prepaid: 50,
    refunded: 0,
    charged: 0,
    settlement: 'pending',
    deductionId: 'd',
    refundId: null,
    errorCode: null,
  };
  const result = {
    items: [record],
    total: 1,
    page: 1,
    pageSize: 20,
    stats: {
      count: 1,
      callCount: 2,
      charged: 0,
      refunded: 0,
      knownCostMicroCny: 123,
      unknownCost: true,
    },
    readAt: record.createdAt,
  };
  let url = '';
  const api = createAdministrationApi(
    createHttpClient(async (u) => {
      url = String(u);
      return Response.json(result);
    }),
  );
  const page = await api.usage('tasks', {
    start: '2026-10-16T00:00:00Z',
    end: '',
    search: 'u',
    searchBy: 'userId',
    model: 'm',
    connection: 'c',
    business: 'asset-preparation-v1',
    result: 'running',
  });
  assert.ok(url.includes('userId=u'));
  assert.ok(url.includes('modelId=m'));
  assert.equal(page.items[0].deducted, null);
  assert.equal(page.items[0].reserved, '50');
  assert.equal(page.items[0].userLabel, record.username);
  assert.equal(page.items[0].userEmail, record.email);
  assert.equal(page.items[0].userIdentity, record.userId);
  assert.equal(page.summary.knownCostCny, '0.000123');
  assert.equal(page.summary.hasUnknownCost, true);
  for (const search of ['u', 't', 'provider-request']) {
    await api.usage('tasks', {
      start: '',
      end: '',
      search: ' ' + search + ' ',
      model: '',
      connection: '',
      business: '',
      result: '',
    });
    const params = new URL(url, 'http://fixture.invalid').searchParams;
    assert.equal(params.get('q'), search);
    assert.equal(params.has('userId') || params.has('taskId') || params.has('requestId'), false);
  }
  const historical = createAdministrationApi(
    createHttpClient(async () =>
      Response.json({ ...result, items: [{ ...record, username: null, email: null }] }),
    ),
  );
  const old = await historical.usage('calls', {
    start: '',
    end: '',
    search: '',
    model: '',
    connection: '',
    business: '',
    result: '',
  });
  assert.equal(old.items[0].userLabel, '未记录用户名');
  assert.equal(old.items[0].userEmail, null);
  assert.equal(old.items[0].userIdentity, 'u');
  const bad = createAdministrationApi(
    createHttpClient(async () =>
      Response.json({ ...result, items: [{ ...record, rawResponse: 'private-body' }] }),
    ),
  );
  const projected = await bad.usage('tasks', {
    start: '', end: '', search: '', model: '', connection: '', business: '', result: '',
  });
  assert.deepEqual(projected, page);
  assert.equal(JSON.stringify(projected).includes('private-body'), false);
  const mediaBasis = {
    id: 'charge',
    sourceVersion: 1,
    inputRevision: 1,
    modelId: 'm',
    connectionId: 'c',
    scope: 'media-video',
    scopeDigest: 'd',
    quantity: 11,
    unit: 'second',
    priceVersion: 1,
    rule: { unitPrice: 10, freeEnabled: false, freeStart: null, freeEnd: null },
    acceptedAt: record.createdAt,
    originalAmount: 110,
    amount: 110,
    free: false,
  };
  const refundPending = createAdministrationApi(
    createHttpClient(async () =>
      Response.json({
        ...result,
        items: [
          {
            ...record,
            operation: 'video.generate',
            status: 'failed',
            basis: mediaBasis,
            settlement: 'refund-pending',
            prepaid: 110,
          },
        ],
      }),
    ),
  );
  const pending = await refundPending.usage('tasks', {
    start: '',
    end: '',
    search: '',
    model: '',
    connection: '',
    business: '',
    result: '',
  });
  assert.equal(pending.items[0].deducted, null);
  assert.equal(pending.items[0].reserved, '110');
  assert.equal(pending.items[0].refunded, null);
  assert.equal(pending.items[0].charge?.unit, 'seconds');
});

test('loading a valid second-precision promotion preserves its original saved window', async () => {
  if (root) await act(async () => root!.unmount());
  let edits = 0;
  const value = {
    unitPrice: 2,
    freeEnabled: true,
    freeStart: '2028-02-29T02:30:20.000Z',
    freeEnd: '2028-02-29T02:30:40.000Z',
  };
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <form>
        <ModelCreditPricingFields
          category="analysis"
          value={value}
          disabled={false}
          onChange={() => {
            edits++;
          }}
        />
      </form>,
    ),
  );
  assert.equal(document.querySelector('form')!.checkValidity(), true);
  assert.equal(edits, 0);
  assert.equal(
    document.querySelector<HTMLInputElement>('.jm-date-time-validation')?.value,
    '2028-02-29T10:30:20.000',
  );
  assert.doesNotMatch(document.body.textContent || '', /结束时间须晚于开始时间/);
});

test('custom promotion calendar keeps Shanghai minutes, leap-day navigation, explicit commit and strict end boundary', async () => {
  if (root) await act(async () => root!.unmount());
  let latest: CreditPriceDTO | null = null;
  function Form() {
    const [value, setValue] = useState<CreditPriceDTO>({
      unitPrice: 2,
      freeEnabled: true,
      freeStart: '2028-02-28T02:30:00.000Z',
      freeEnd: '2028-03-01T02:30:00.000Z',
    });
    latest = value;
    return (
      <form>
        <ModelCreditPricingFields
          category="analysis"
          value={value}
          disabled={false}
          onChange={(next) => next && setValue(next)}
        />
      </form>
    );
  }
  root = createRoot(document.getElementById('root')!);
  await act(async () => root!.render(<Form />));
  const button = (label: string) =>
    [...document.querySelectorAll<HTMLButtonElement>('button')].find(
      (node) => node.getAttribute('aria-label') === label || node.textContent === label,
    )!;
  const flush = async () =>
    act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5));
    });
  assert.equal(document.querySelector('input[type=datetime-local]'), null);
  assert.match(button('限免开始').textContent || '', /2028-02-28 10:30/);
  await act(async () => button('限免开始').click());
  await flush();
  assert.equal(document.activeElement?.getAttribute('aria-label'), '2028年2月28日');
  assert.equal(document.querySelectorAll('[role=grid] tbody button[tabindex="0"]').length, 1);
  await act(async () =>
    document.activeElement!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }),
    ),
  );
  assert.equal(document.activeElement?.getAttribute('aria-label'), '2028年2月29日');
  await act(async () => button('2028年2月29日').click());
  assert.equal(
    latest!.freeStart,
    '2028-02-28T02:30:00.000Z',
    'selection must remain an uncommitted calendar draft',
  );
  await act(async () => button('确定').click());
  await flush();
  assert.equal(latest!.freeStart, '2028-02-29T02:30:00.000Z');
  assert.equal(document.activeElement?.getAttribute('aria-label'), '限免开始');
  await act(async () => button('限免结束').click());
  await flush();
  await act(async () => button('上个月').click());
  assert.equal(button('2028年2月28日').disabled, true);
  await act(async () => button('2028年2月29日').click());
  assert.equal(button('确定').disabled, true);
  assert.match(document.body.textContent || '', /结束时间须晚于开始时间/);
  const minute = document.querySelector<HTMLInputElement>('input[aria-label="限免结束分钟"]')!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(minute, '31');
    minute.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(async () => button('确定').click());
  await flush();
  assert.equal(latest!.freeEnd, '2028-02-29T02:31:00.000Z');
  assert.equal(document.querySelector('form')!.checkValidity(), true);
  await act(async () => button('限免开始').click());
  await flush();
  await act(async () => button('下个月').click());
  assert.match(document.querySelector('.jm-calendar-head')?.textContent || '', /2028 年 3 月/);
  await act(async () => button('2028年3月1日').click());
  await act(async () =>
    document.activeElement!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    ),
  );
  await flush();
  assert.equal(latest!.freeStart, '2028-02-29T02:30:00.000Z');
  assert.equal(document.querySelector('.jm-date-time-popover'), null);
  await act(async () => button('限免结束').click());
  await flush();
  await act(async () => button('清除').click());
  await flush();
  assert.equal(latest!.freeEnd, null);
  let valid = true;
  await act(async () => {
    valid = document.querySelector('form')!.checkValidity();
  });
  assert.equal(valid, false);
});

test('expiry receipts use their own negative ledger kind and filter without counting consumption', async () => {
  let url='';
  const result={items:[{id:'expiry',operationId:'gift:expire:u:day',accountId:'u',kind:'expire',amount:20,balance:50,category:'gift',rechargeAmount:0,giftAmount:20,legacyAmount:0,source:'daily-gift-expiration',subject:'system',reason:'每日赠送到期回收',createdAt:'2026-10-18T16:00:00Z'}],total:1,page:1,pageSize:20,summary:{acquired:0,consumed:0,refunded:0,netConsumed:0,settledConsumed:0,pending:0},analysis:[]};
  const api=createAdministrationApi(createHttpClient(async u=>{url=String(u);return Response.json(result);}));
  const page=await api.myLedger('',{direction:'expired',category:'gift',start:'',end:''});
  assert.equal(new URL(url,'http://fixture.invalid').searchParams.get('direction'),'expired');
  assert.equal(page.items[0].direction,'expire');
  assert.equal(page.summary.consumed,'0');
  result.items[0].kind='unexpected';
  await assert.rejects(()=>api.myLedger(''),/回执/);
});
