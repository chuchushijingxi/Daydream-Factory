import { after, test } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import type { SpaceRole, SpaceSummary } from '../src/api/spaces-types';
import { createAdministrationFixture } from './administration-fixture';

const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/#/projects',
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
  'localStorage',
  'sessionStorage',
]) {
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
}
globalThis.getComputedStyle = dom.window.getComputedStyle;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
Object.defineProperty(globalThis, 'BroadcastChannel', { value: undefined, configurable: true });
const { createRoot } = await import('react-dom/client');
const { MemoryRouter, Route, Routes, useLocation } = await import('react-router-dom');
const { LanguageProvider } = await import('../src/contexts/LanguageContext');
const { SessionOptionalContext } = await import('../src/state/SessionContext');
const { SpaceProvider, useSpace } = await import('../src/state/SpaceContext');
const { spaceStorageKey } = await import('../src/api/space-selection');
const { sessionBoundary } = await import('../src/api/session-boundary');
const { PersonalCreditsProvider } = await import('../src/features/credits/PersonalCredits');
const { CreditBalanceMenu } = await import('../src/components/workspace/CreditBalanceMenu');
const { CreditsPage } = await import('../src/pages/credits/CreditsPage');
const { RechargeProvider } = await import('../src/features/payments/Recharge');
// 充值入口只看角色与团队状态；选项读取失败时入口仍可用（打开后显示错误），测试不触达支付接口。
const rechargeApi = {
  options: async () => {
    throw new Error('options unavailable in menu tests');
  },
} as unknown as import('../src/features/payments/checkout').CheckoutAPI;
let teamStatus: SpaceSummary['status'] = 'active';
const originalFetch = globalThis.fetch;
let root: ReturnType<typeof createRoot> | undefined;
let changed = () => {};
let wallet: Record<string, unknown>;
let balanceFails = false;
let historyFails = false;
let empty = false;
let many = false;
let calls: { path: string; method: string; search?: string; body?: string }[];
const tick = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
};
const trigger = () => document.querySelector<HTMLButtonElement>('.credit-balance-trigger')!;
const textButton = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (button) => button.textContent === text,
  )!;
function Ready({ children }: { children: React.ReactNode }) {
  return useSpace().ready ? children : null;
}
function TeamRouteProbe() {
  const location = useLocation();
  return <output data-team-route={location.pathname + location.search} />;
}
async function mount(
  role: SpaceRole | 'personal',
  teamName = '摄影团队',
  value = 300,
  held: { balance?: Promise<Response>; history?: Promise<Response> } = {},
  entry?: string,
) {
  if (root)
    await act(async () => {
      root!.unmount();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  sessionBoundary.reset();
  localStorage.clear();
  balanceFails = false;
  historyFails = false;
  empty = false;
  many = false;
  calls = [];
  const account = createAdministrationFixture();
  const user = account.user!;
  const personal: SpaceSummary = {
    id: user.identity,
    name: '个人空间',
    kind: 'personal',
    role: 'owner',
    memberCount: 1,
    memberLimit: 1,
    status: 'active',
    readOnly: false,
  };
  const team: SpaceSummary = {
    id: 'team',
    name: teamName,
    kind: 'team',
    role: role === 'personal' ? 'owner' : role,
    memberCount: 3,
    memberLimit: 5,
    status: teamStatus,
    readOnly: false,
  };
  localStorage.setItem(spaceStorageKey(user.identity), role === 'personal' ? personal.id : team.id);
  // 所有者 balance 为团队总额、顶栏显示未分配（value）；管理员与成员的 balance 是本人名下分配额（value），
  // 管理员另带团队三余额用于管理分配。
  wallet =
    role === 'member'
      ? { spaceId: 'team', role, balance: value }
      : role === 'admin'
        ? {
            spaceId: 'team',
            role,
            balance: value,
            totalBalance: 1000,
            allocated: 700,
            unallocated: 300,
          }
        : {
            spaceId: 'team',
            role,
            balance: 1000,
            totalBalance: 1000,
            allocated: 1000 - value,
            unallocated: value,
          };
  account.service.watchCredits = async (_target, callback, signal) => {
    changed = callback;
    await new Promise<void>((resolve) =>
      signal.addEventListener('abort', () => resolve(), { once: true }),
    );
  };
  globalThis.fetch = async (url, options) => {
    const path = new URL(String(url), 'http://fixture.invalid').pathname;
    calls.push({
      path,
      method: options?.method || 'GET',
      search: new URL(String(url), 'http://fixture.invalid').search,
      body: typeof options?.body === 'string' ? options.body : undefined,
    });
    if (path === '/api/credits/balance') {
      if (held.balance) return (await held.balance).clone();
      return balanceFails
        ? Response.json({ code: 'UNAVAILABLE', message: '余额读取失败' }, { status: 503 })
        : Response.json(wallet);
    }
    if (path === '/api/credits/ledger/recent') {
      if (held.history) return (await held.history).clone();
      if (historyFails)
        return Response.json({ code: 'UNAVAILABLE', message: '流水读取失败' }, { status: 503 });
      if (many)
        return Response.json({
          items: [1, 2, 3].map((i) => ({
            id: 'consume' + i,
            kind: 'deduct',
            amount: i,
            reason: 'generation',
            createdAt: '2026-10-20T12:0' + i + ':00Z',
          })),
          allocations: [1, 2, 3].map((i) => ({
            id: 'allocation' + i,
            kind: 'allocate',
            amount: i,
            balance: value,
            createdAt: '2026-10-20T12:0' + i + ':30Z',
          })),
        });
      return Response.json({
        items: empty
          ? []
          : [
              {
                id: 'consume',
                kind: 'deduct',
                amount: 5,
                reason: 'generation',
                createdAt: '2026-10-20T12:00:00Z',
                ...(role === 'owner' ? { balance: 300 } : {}),
              },
            ],
        ...(role === 'member' || role === 'admin'
          ? {
              allocations: empty
                ? []
                : [
                    {
                      id: 'allocation',
                      kind: 'allocate',
                      amount: 20,
                      balance: value,
                      createdAt: '2026-10-20T13:00:00Z',
                    },
                  ],
            }
          : {}),
      });
    }
    if (path === '/api/credits/ledger')
      return Response.json({
        items: [
          { id: 'use1', kind: 'deduct', amount: 5, reason: 'generation', createdAt: '2026-10-20T12:00:00Z' },
          { id: 'use2', kind: 'refund', amount: 2, reason: 'generation-incomplete', createdAt: '2026-10-20T11:00:00Z' },
        ],
        total: 2,
        page: 1,
        pageSize: 20,
        allocations: {
          items: [
            { id: 'alloc1', kind: 'allocate', amount: 20, balance: value, createdAt: '2026-10-20T13:00:00Z' },
            { id: 'alloc2', kind: 'release', amount: 3, balance: 0, createdAt: '2026-10-19T13:00:00Z' },
          ],
          total: 21,
          page: 1,
          pageSize: 20,
        },
      });
    if (path === '/api/spaces/team/credit-requests') return Response.json({ requested: true });
    throw new Error('Unexpected credit menu request ' + path);
  };
  const session = {
    state: 'authenticated' as const,
    user,
    service: account.service,
    epoch: sessionBoundary.epoch,
    message: '',
    refresh: async () => {},
    authenticate: async (action: () => Promise<void>) => {
      await action();
      return true;
    },
    updateProfile: async () => user,
    changePassword: async () => {},
    logout: async () => {},
  };
  const service = {
    list: async () => (role === 'personal' ? [personal] : [personal, team]),
    pendingInvites: async () => [],
    detail: async (id: string) => (id === 'team' ? team : personal),
  };
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <MemoryRouter initialEntries={[entry || '/projects']}>
          <SessionOptionalContext.Provider value={session}>
            <SpaceProvider service={service}>
              <Ready>
                <PersonalCreditsProvider>
                  <RechargeProvider api={rechargeApi}>
                  <CreditBalanceMenu />
                  {entry ? (
                    <Routes>
                      <Route path="/credits" element={<CreditsPage />} />
                      <Route path="/team/:spaceId" element={<TeamRouteProbe />} />
                    </Routes>
                  ) : null}
                  </RechargeProvider>
                </PersonalCreditsProvider>
              </Ready>
            </SpaceProvider>
          </SessionOptionalContext.Provider>
        </MemoryRouter>
      </LanguageProvider>,
    ),
  );
  await tick();
  await tick();
}
async function open() {
  await act(async () => {
    trigger().dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
  await tick();
}

test('team owner menu matches the personal layout with a recharge entry and no contract details', async () => {
  await mount('owner');
  assert.match(trigger().textContent!, /300积分摄影团队/);
  assert.doesNotMatch(trigger().textContent!, /未分配积分|我的可用积分|1,000/);
  await open();
  const panel = document.querySelector('.team-credit-panel')!;
  assert.equal(panel.querySelector('.credit-panel-balance-info')?.textContent, '积分余额300');
  assert.ok(panel.querySelector('.credit-panel-balance .credit-panel-recharge'), 'owner can recharge the team');
  assert.equal(panel.querySelector('dl'), null);
  assert.doesNotMatch(panel.textContent!, /摄影团队|总额|已分配|1,000|请联系运营/);
  assert.equal(
    panel.querySelector('.credit-panel-records-heading a')?.getAttribute('href'),
    '/team/team?tab=credits',
    'manager credit details live in the team page credits tab',
  );
  assert.ok(panel.querySelector('.credit-panel-records li time[datetime="2026-10-20T12:00:00Z"]'));
  assert.equal(panel.querySelector('.credit-record-amount')?.textContent, '−5');
  assert.ok(textButton('充值'));
  assert.equal(panel.querySelector('.team-credit-topup'), null);
  assert.ok(calls.every((call) => call.method === 'GET'));
  await act(() =>
    panel.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  );
  await tick();
  assert.equal(document.querySelector('.team-credit-panel'), null);
  assert.equal(document.activeElement, trigger());
});

test('admin top bar shows own allocated credits, follows owner allocations live and offers no recharge', async () => {
  await mount('admin', '联合制作', 30);
  assert.match(trigger().textContent!, /^30积分联合制作$/, 'admin spends only their own allocation');
  // 所有者再分配 15：未分配减少、管理员名下增加，顶栏不打开也跟随。
  wallet = { ...wallet, balance: 45, allocated: 715, unallocated: 285 };
  await act(async () => {
    changed();
    await new Promise((resolve) => setTimeout(resolve, 130));
  });
  await tick();
  assert.match(trigger().textContent!, /^45积分联合制作$/);
  await open();
  assert.equal(textButton('充值'), undefined);
  const panel = document.querySelector('.team-credit-panel')!;
  assert.equal(panel.querySelector('.credit-panel-balance')?.textContent, '积分余额45');
  assert.doesNotMatch(panel.textContent!, /总额|已分配|请联系运营|285|1,000/);
  assert.deepEqual(
    [...panel.querySelectorAll('.credit-panel-records li strong')].map((item) => item.textContent),
    ['分配', '创作生成'],
    'admin recent records are their own usage and allocations',
  );
  assert.equal(
    panel.querySelector('.credit-panel-records-heading a')?.getAttribute('href'),
    '/credits',
    'admin credit details are their own credits page',
  );
});

test('member sees only holder balance and own chronological allocation history, including true zero', async () => {
  await mount('member', '成员团队', 0);
  assert.match(trigger().textContent!, /^0积分成员团队$/);
  await open();
  const panel = document.querySelector('.team-credit-panel')!;
  assert.equal(panel.querySelector('.credit-panel-balance')?.textContent, '积分余额0');
  assert.equal(panel.querySelector('dl'), null);
  assert.doesNotMatch(panel.textContent!, /总额|已分配|1,000|充值/);
  assert.deepEqual(
    [...panel.querySelectorAll('.credit-panel-records li strong')].map((item) => item.textContent),
    ['分配', '创作生成'],
  );
  assert.deepEqual(
    [...panel.querySelectorAll('.credit-record-amount')].map((item) => item.textContent),
    ['+20', '−5'],
  );
  assert.ok(
    calls.every(
      (call) =>
        ['/api/credits/balance', '/api/credits/ledger/recent'].includes(call.path) &&
        call.method === 'GET',
    ),
  );
});

test('recent ledger and allocation events share a five-row chronological preview', async () => {
  await mount('member');
  many = true;
  await open();
  const rows = [...document.querySelectorAll('.credit-panel-records li')];
  assert.equal(rows.length, 5);
  assert.deepEqual(
    rows.map((row) => row.querySelector('time')?.getAttribute('datetime')),
    [
      '2026-10-20T12:03:30Z',
      '2026-10-20T12:03:00Z',
      '2026-10-20T12:02:30Z',
      '2026-10-20T12:02:00Z',
      '2026-10-20T12:01:30Z',
    ],
  );
});

test('pending balance and records stay unknown until confirmed zero and empty receipts arrive', async () => {
  let balanceDone!: (value: Response) => void;
  let historyDone!: (value: Response) => void;
  const balance = new Promise<Response>((resolve) => {
    balanceDone = resolve;
  });
  const history = new Promise<Response>((resolve) => {
    historyDone = resolve;
  });
  await mount('member', '加载团队', 0, { balance, history });
  assert.match(trigger().textContent!, /^—积分/);
  await open();
  assert.equal(document.querySelector('.credit-panel-balance strong')?.textContent, '—');
  assert.match(document.querySelector('.credit-panel-feedback')!.textContent!, /读取积分记录/);
  assert.equal(document.querySelector('.credit-panel-empty'), null);
  await act(async () => {
    balanceDone(Response.json({ spaceId: 'team', role: 'member', balance: 0 }));
    historyDone(Response.json({ items: [], allocations: [] }));
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  await tick();
  assert.equal(document.querySelector('.credit-panel-balance strong')?.textContent, '0');
  assert.equal(document.querySelector('.credit-panel-empty')?.textContent, '暂无积分记录');
});

test('balance/history failures remain distinct from zero or empty and recover only through the relevant read', async () => {
  await mount('member', '故障团队', 0);
  balanceFails = true;
  historyFails = true;
  await open();
  assert.match(document.querySelector('.team-credit-panel')!.textContent!, /余额读取失败/);
  assert.match(document.querySelector('.team-credit-panel')!.textContent!, /流水读取失败/);
  assert.equal(document.querySelector('.credit-panel-empty'), null, 'failed history is not empty');
  balanceFails = false;
  historyFails = false;
  empty = true;
  await act(() => textButton('重新读取余额').click());
  await act(() => textButton('重新读取记录').click());
  await tick();
  await tick();
  assert.equal(document.querySelector('[role="alert"]'), null);
  assert.equal(document.querySelector('.credit-panel-empty')?.textContent, '暂无积分记录');
  assert.match(trigger().textContent!, /^0积分/);
});

test('long team name remains complete in the accessible trigger while the popup and personal menu stay simple', async () => {
  const name = '这是一个很长的电影制作团队名称'.repeat(3).slice(0, 40);
  await mount('member', name, 12);
  assert.ok(trigger().getAttribute('aria-label')?.includes(name));
  await open();
  assert.equal(document.querySelector('.team-credit-context'), null);
  assert.doesNotMatch(document.querySelector('.team-credit-panel')!.textContent!, new RegExp(name));
  assert.equal(document.querySelector('.credit-panel-balance')?.textContent, '积分余额12');
  assert.match(trigger().textContent!, /^12积分/);
  await mount('personal');
  assert.ok(trigger().getAttribute('aria-label')?.startsWith('积分余额'));
  await open();
  assert.ok(document.querySelector('.credit-panel-balance'));
  assert.equal(document.querySelector('.team-credit-summary'), null);
  assert.ok(document.querySelector('.credit-panel-records-heading a'));
  assert.equal(calls.length, 0, 'personal menu must not read team credit endpoints');
});

test('the owner opening /credits lands on the team page credits tab without a second team ledger page', async () => {
  await mount('owner', '摄影团队', 300, {}, '/credits');
  await tick();
  assert.equal(
    document.querySelector('[data-team-route]')?.getAttribute('data-team-route'),
    '/team/team?tab=credits',
  );
  assert.equal(document.querySelector('.personal-credits-page'), null);
  assert.ok(
    calls.every((call) => call.path !== '/api/credits/ledger'),
    'the redirect must not read the team ledger itself',
  );
});

test('an admin opening /credits sees their own allocated credits and requests from the owner only', async () => {
  await mount('admin', '摄影团队', 30, {}, '/credits');
  await tick();
  assert.equal(document.querySelector('[data-team-route]'), null, 'admins are not redirected');
  const page = document.querySelector('.personal-credits-page')!;
  assert.equal(page.querySelector('.personal-credit-balance-value strong')?.textContent, '30');
  assert.doesNotMatch(page.textContent!, /团队总余额|未分配|充值/);
  const ledger = calls.find((call) => call.path === '/api/credits/ledger')!;
  assert.match(ledger.search!, /scope=holder/, 'admin page reads the holder ledger');
  await act(() => textButton('申请积分').click());
  await tick();
  assert.match(document.querySelector('.team-dialog')!.textContent!, /通知团队所有者为你分配/);
  assert.ok(textButton('通知所有者'), 'admins ask the owner, who alone can allocate to them');
});

test('member credit details use the personal layout with own usage, allocations and a credit request', async () => {
  await mount('member', '成员团队', 18, {}, '/credits');
  await open();
  assert.equal(
    document.querySelector('.credit-panel-records-heading a')?.getAttribute('href'),
    '/credits',
    'members keep their own credits page',
  );
  await act(() =>
    document
      .querySelector('.team-credit-panel')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  );
  await tick();
  const page = document.querySelector('.personal-credits-page')!;
  assert.ok(page, 'member page reuses the personal credits layout');
  assert.match(page.querySelector('.personal-credits-heading')!.textContent!, /「成员团队」/);
  assert.equal(page.querySelector('#personal-credit-balance-title')?.textContent, '我的可用积分');
  assert.equal(page.querySelector('.personal-credit-balance-value strong')?.textContent, '18');
  assert.doesNotMatch(page.textContent!, /团队总余额|未分配|已分配|充值/);
  const titles = () =>
    [...page.querySelectorAll('.personal-credit-ledger .personal-credit-reason strong')].map(
      (item) => item.textContent,
    );
  assert.deepEqual(titles(), ['创作生成', '生成未完成返还']);
  assert.equal(page.querySelector('.personal-credit-after-balance'), null);
  assert.equal(
    page.querySelector('.team-member-credit-records')?.getAttribute('data-tab'),
    'usage',
  );
  await act(() => textButton('分配记录').click());
  await tick();
  assert.deepEqual(titles(), ['分配', '退出收回']);
  assert.deepEqual(
    [...page.querySelectorAll('.personal-credit-ledger .credit-record-amount')].map(
      (item) => item.textContent,
    ),
    ['+20', '−3'],
  );
  assert.equal(page.querySelectorAll('.personal-credit-after-balance').length, 2);
  assert.match(page.querySelector('.op-pagination')!.textContent!, /共 21 条 · 第 1 页/);
  await act(() => textButton('申请积分').click());
  await tick();
  const input = document.querySelector<HTMLInputElement>('.team-dialog input')!;
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  await act(async () => {
    setValue.call(input, '50');
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(async () => {
    textButton('通知所有者和管理员').click();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  await tick();
  const request = calls.find((call) => call.path === '/api/spaces/team/credit-requests');
  assert.equal(request?.method, 'POST');
  assert.deepEqual(JSON.parse(request!.body!), { needed: 50 });
  assert.equal(document.querySelector('.team-dialog'), null, 'dialog closes after the receipt');
});

after(async () => {
  if (root)
    await act(async () => {
      root!.unmount();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  globalThis.fetch = originalFetch;
  dom.window.close();
});

test('an expired team hides the owner recharge entry, matching the recharge target rule', async () => {
  teamStatus = 'expired';
  try {
    await mount('owner');
    await open();
    assert.ok(document.querySelector('.team-credit-panel .credit-panel-balance-info'));
    assert.equal(textButton('充值'), undefined);
  } finally {
    teamStatus = 'active';
  }
});
