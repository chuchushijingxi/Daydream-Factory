import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import { createAdministrationFixture, creditStats } from './administration-fixture';
const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/#/operations/usage',
  pretendToBeVisual: true,
});
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'HTMLTextAreaElement',
  'Element',
  'Node',
  'NodeFilter',
  'DocumentFragment',
  'SVGElement',
  'HTMLButtonElement',
  'MutationObserver',
  'Event',
  'MouseEvent',
  'CustomEvent',
  'localStorage',
  'sessionStorage',
  'KeyboardEvent',
])
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
globalThis.getComputedStyle = dom.window.getComputedStyle;
globalThis.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
// Match JSDOM's browser capabilities instead of Node's worker Event implementation.
Object.defineProperty(globalThis, 'BroadcastChannel', {
  value: dom.window.BroadcastChannel,
  configurable: true,
});
const { createRoot } = await import('react-dom/client'),
  { HashRouter } = await import('react-router-dom'),
  { LanguageProvider } = await import('../src/contexts/LanguageContext'),
  { SessionProvider, useSession } = await import('../src/state/SessionContext'),
  { Application } = await import('../src/Application'),
  { sessionBoundary } = await import('../src/api/session-boundary');
const { ApiError } = await import('../src/api/http-error');
let fixture = createAdministrationFixture(),
  root: ReturnType<typeof createRoot> | undefined,
  session: ReturnType<typeof useSession>,
  httpRequests: string[] = [];
function Probe() {
  session = useSession();
  return null;
}
const flush = async () => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5));
  });
};
const button = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent?.trim() === label || b.getAttribute('aria-label') === label,
  )!;
const text = () => document.body.textContent || '';
const input = async (element: HTMLInputElement, value: string) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value);
    element.dispatchEvent(new Event('input', { bubbles: true }));
  });
};
async function mount(
  route = '/operations/usage',
  setup?: (f: ReturnType<typeof createAdministrationFixture>) => void,
) {
  if (root) await act(async () => root!.unmount());
  sessionBoundary.reset();
  fixture = createAdministrationFixture();
  setup?.(fixture);
  window.location.hash = '#' + route;
  httpRequests = [];
  globalThis.fetch = async (url) => {
    httpRequests.push(String(url));
    const target = String(url).split('?')[0];
    const personal = {id: fixture.user?.identity || 'fixture_operator', kind: 'personal', name: '个人空间', role: 'owner', memberCount: 1, memberLimit: 1, status: 'active', readOnly: false};
    if (target === '/api/spaces') return Response.json([personal]);
    if (target === '/api/invites/pending') return Response.json([]);
    if (target === '/api/spaces/' + personal.id) return Response.json(personal);
    return Response.json(
      target === '/api/projects'
        ? []
        : url === '/api/models'
          ? { analysis: [], image: [], video: [] }
          : url === '/api/visual-styles'
            ? { styles: [] }
            : { readOnly: true, capabilities: {} },
    );
  };
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <HashRouter>
          <SessionProvider service={fixture.service}>
            <Probe />
            <Application />
          </SessionProvider>
        </HashRouter>
      </LanguageProvider>,
    ),
  );
  await flush();
  await flush();
}
after(async () => {
  if (root) await act(async () => root!.unmount());
  dom.window.close();
});
test('operations sidebar starts expanded at every width and keeps an explicit collapse across navigation', async () => {
  const originalMedia = globalThis.matchMedia;
  try {
    for (const narrow of [false, true]) {
      globalThis.matchMedia = () => ({
        matches: narrow,
        addEventListener() {},
        removeEventListener() {},
      });
      await mount('/operations/users');
      const sidebar = document.querySelector('.op-sidebar')!;
      assert.equal(sidebar.getAttribute('data-collapsed'), 'false');
      assert.equal(button('收起运营导航').getAttribute('aria-expanded'), 'true');
      await act(async () => button('收起运营导航').click());
      await flush();
      await act(async () => {
        sidebar.dispatchEvent(new MouseEvent('pointerover', { bubbles: true }));
        button('展开运营导航').focus();
        window.dispatchEvent(new Event('resize'));
      });
      await flush();
      await act(async () =>
        document
          .querySelector<HTMLAnchorElement>(
            'nav[aria-label="运营导航"] a[href="#/operations/usage"]',
          )!
          .click(),
      );
      await flush();
      assert.equal(sidebar.getAttribute('data-collapsed'), 'true');
      assert.equal(button('展开运营导航').getAttribute('aria-expanded'), 'false');
      await act(async () => button('展开运营导航').click());
      await flush();
      assert.equal(sidebar.getAttribute('data-collapsed'), 'false');
      await mount('/operations/users');
      assert.equal(document.querySelector('.op-sidebar')!.getAttribute('data-collapsed'), 'false');
    }
  } finally {
    globalThis.matchMedia = originalMedia;
  }
});
test('operations default task view, detail, shared filters and opaque paging request the selected server scope', async () => {
  await mount();
  assert.equal(document.querySelector('h1')?.textContent, '模型使用记录');
  assert.equal(document.querySelectorAll('nav[aria-label="运营导航"] a').length, 12);
  assert.equal(document.querySelector('nav[aria-label="运营导航"] a[href="#/operations/teams"]')?.textContent, '团队管理');
  assert.equal(document.querySelectorAll('nav[aria-label="运营导航"] a[aria-label="支付管理"]').length, 1);
  assert.equal(document.querySelectorAll('nav[aria-label="运营导航"] a[aria-label="支付异常"]').length, 0);
  assert.match(text(), /筛选范围 · 任务/);
  assert.match(text(), /¥1.320000123/);
  assert.match(text(), /另有待确认成本/);
  const userCell = document.querySelector('.op-usage-user')!;
  assert.equal(userCell.querySelector('span')?.textContent, '虚构用户 A');
  assert.equal(userCell.querySelector('small')?.textContent, 'fiction-a@example.invalid');
  assert.equal(userCell.getAttribute('title'), null);
  assert.equal(
    document.querySelector('.op-usage-time time')?.getAttribute('datetime'),
    fixture.rows[0].at,
  );
  await act(async () => button('详情 整剧解析 虚构用户 A').click());
  await flush();
  assert.match(text(), /18,450 字 × 2 积分\/100 字＝369 积分/);
  assert.equal(fixture.requests.filter((r) => r.method === 'usageDetail').length, 1);
  assert.doesNotMatch(document.querySelector('.op-usage-detail')!.textContent!, /源版本|输入修订|原扣款流水|查看此任务|fixture_task_01/);
  await act(async () => button('调用明细').click());
  await flush();
  assert.match(text(), /筛选范围 · 调用/);
  assert.equal(document.querySelectorAll('.op-usage-table tbody tr').length, 2);
  assert.equal(document.querySelector('.op-usage-table')?.textContent?.includes('369 积分'), false);
  await act(async () => button('任务汇总').click());
  await flush();
  await act(async () => button('下一页').click());
  await flush();
  assert.match(text(), /预扣 50 积分/);
  assert.match(text(), /待确认/);
  assert.match(text(), /已返还 160 积分/);
  const search = document.querySelector<HTMLInputElement>(
    'input[aria-label="搜索用户、任务或请求 ID"]',
  )!;
  await input(search, 'fixture_user_b');
  await act(async () =>
    document
      .querySelector('.op-usage-filters')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const request = fixture.requests.filter((r) => r.method === 'usage').at(-1)!.input as any;
  assert.equal(request.filters.search, 'fixture_user_b');
  assert.equal(request.filters.searchBy, undefined);
  assert.equal(button('搜索 ID 类型'), undefined);
  assert.equal(request.key, '');
});
test('personal wallet never accepts URL account override; normal users cannot mount or query operations', async () => {
  await mount('/credits?account=another', (f) => {
    f.user = {
      identity: 'ordinary',
      username: '虚构用户',
      email: 'ordinary@example.invalid',
      roleLabel: '普通用户',
      operationsAllowed: false,
    };
  });
  assert.match(text(), /9,007,199,254,740,990/);
  assert.equal(
    fixture.requests.some((r) => r.method === 'userWallet'),
    false,
  );
  assert.equal(fixture.requests.filter((r) => r.method === 'myCreditBalance').length, 1);
  assert.equal(fixture.requests.filter((r) => r.method === 'myLedger').length, 2);
  await mount('/operations/usage', (f) => {
    f.user = {
      identity: 'ordinary',
      username: '虚构用户',
      email: 'ordinary@example.invalid',
      roleLabel: '普通用户',
      operationsAllowed: false,
    };
  });
  assert.equal(document.querySelector('h1')?.textContent, '登录运营后台');
  assert.equal(window.location.hash, '#/operations-login');
  assert.equal(
    fixture.requests.some((r) => ['users', 'usage', 'userWallet'].includes(r.method)),
    false,
  );
});
test('workspace credit capsule loads recent records only when opened and preserves exact signed amounts', async () => {
  await mount('/projects', (f) => {
    f.service.myCreditBalance = async () => {
      f.requests.push({ method: 'myCreditBalance' });
      return { balance: '64', rechargeBalance: '4', giftBalance: '60', legacyBalance: '0' };
    };
    f.service.myRecentLedger = async (key) => {
      f.requests.push({ method: 'myRecentLedger', input: key });
      return Array.from({ length: 5 }, (_, i) => ({
          identity: 'entry-' + i,
          at: '2026-10-17T01:00:00Z',
          direction: i === 1 ? 'deduct' : i === 2 ? 'refund' : 'add',
          amount: '12',
          balance: '64',
          reason: '虚构变动 ' + i,
          source: 'fixture',
          actorLabel: 'fixture_operator',
          originalDebit: i === 2 ? 'original-debit' : null,
        }));
    };
  });
  assert.doesNotMatch(text(), /本地工作台/);
  assert.ok(button('积分余额 64'));
  assert.equal(fixture.requests.filter((r) => r.method === 'myCreditBalance').length, 1);
  assert.equal(
    fixture.requests.some((r) => r.method === 'myRecentLedger'),
    false,
  );
  await act(async () => button('积分余额 64').click());
  await flush();
  const panel = document.querySelector('.personal-credit-panel')!;
  assert.ok(panel);
  assert.equal(panel.querySelectorAll('li').length, 5);
  assert.match(panel.textContent || '', /\+12/);
  assert.match(panel.textContent || '', /−12/);
  assert.equal(fixture.requests.filter((r) => r.method === 'myRecentLedger').length, 1);
  assert.equal(
    fixture.requests.some((r) => r.method === 'userWallet'),
    false,
  );
  assert.ok(panel.querySelector('a[href="#/credits"]'));
  assert.equal(panel.querySelector('a[href="#/payments/orders"]'), null);
  assert.equal(panel.querySelector('.credit-panel-categories'), null);
  // 逐文本节点断言：避免“充值”按钮与“积分记录”标题在 textContent 中拼接出“充值积分”。
  const panelTextNodes = [...panel.querySelectorAll('*')]
    .flatMap((el) =>
      [...el.childNodes]
        .filter((n) => n.nodeType === 3)
        .map((n) => n.textContent ?? ''),
    )
    .join('\n');
  assert.doesNotMatch(panelTextNodes, /充值积分|赠送及免费积分|开会员|开发票/);
  assert.equal(panel.querySelector('a svg'), null);
  assert.equal(panel.querySelector('li strong')?.textContent, '虚构变动 0');
  assert.match(panel.querySelector('time')?.textContent || '', /10\/17 09:00/);
  await act(async () =>
    document.activeElement!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    ),
  );
  await flush();
  assert.equal(document.querySelector('.personal-credit-panel'), null);
  assert.equal(document.activeElement?.getAttribute('aria-label'), '积分余额 64');
  const trigger = button('积分余额 64');
  await act(async () => {
    const pointer = new MouseEvent('pointerover', { bubbles: true });
    Object.defineProperty(pointer, 'pointerType', { value: 'mouse' });
    trigger.dispatchEvent(pointer);
  });
  await flush();
  assert.equal(
    document.querySelector('.personal-credit-panel'),
    null,
    'hover must not open the credit panel',
  );
  await act(async () => trigger.click());
  await flush();
  assert.ok(document.querySelector('.personal-credit-panel'));
  assert.equal(fixture.requests.filter(r => r.method === 'myRecentLedger').length, 1);
  assert.doesNotMatch(document.querySelector('.personal-credit-panel')!.textContent!, /读取积分记录/);
  await act(async () => trigger.click());
  await flush();
  assert.equal(document.querySelector('.personal-credit-panel'), null);
});
test('credit popover distinguishes loading, failed and genuinely empty records without inventing a sample balance', async () => {
  let resolve!: (value: any) => void;
  let changed = () => {};
  await mount('/projects', (f) => {
    f.service.watchCredits = async (_target, onChange, signal) => {
      changed = onChange;
      await new Promise<void>(done => signal.addEventListener('abort', () => done(), { once: true }));
    };
    f.wallet = { balance: '0', rechargeBalance: '0', giftBalance: '0', legacyBalance: '0' };
    f.service.myRecentLedger = () =>
      new Promise((done) => {
        resolve = done;
      });
  });
  await act(async () =>
    button('积分余额 0').dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }),
    ),
  );
  await flush();
  assert.match(document.querySelector('.personal-credit-panel')?.textContent || '', /读取积分记录/);
  assert.equal(document.querySelector('.credit-panel-empty'), null);
  await act(async () =>
    resolve([]),
  );
  await flush();
  const panel = document.querySelector('.personal-credit-panel')!;
  assert.equal(panel.querySelector('.credit-panel-balance strong')?.textContent, '0');
  assert.equal(panel.querySelector('.credit-panel-empty')?.textContent, '暂无积分记录');
  assert.equal(panel.querySelector('.credit-panel-empty svg'), null);
  assert.doesNotMatch(panel.textContent || '', /积分变动后会显示在这里/);
  await act(async () =>
    document.activeElement!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    ),
  );
  await flush();
  fixture.service.myRecentLedger = async () => {
    throw new Error('真实读取失败');
  };
  await act(async () =>
    button('积分余额 0').dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }),
    ),
  );
  await flush();
  assert.equal(document.querySelector('.credit-panel-error'), null, 'reopening retains the confirmed empty result');
  await act(async () => { changed(); await new Promise(done => setTimeout(done, 130)); });
  await flush();
  assert.match(document.querySelector('.credit-panel-error')?.textContent || '', /真实读取失败/);
  assert.equal(document.querySelector('.credit-panel-empty'), null);
  fixture.service.myRecentLedger = async () => [];
  await act(async () => button('重新读取记录').click());
  await flush();
  assert.equal(document.querySelector('.credit-panel-empty')?.textContent, '暂无积分记录');
  assert.equal(
    fixture.requests.some((request) => ['adjust', 'userWallet'].includes(request.method)),
    false,
  );
});
test('recent credit records remain visible during an SSE refresh and do not reload when reopened', async () => {
  let changed = () => {}, resolve!: (items: any[]) => void;
  let calls = 0;
  const entry = { identity: 'confirmed', at: '2026-10-19T05:40:00Z', direction: 'deduct' as const,
    amount: '5', balance: '60', reason: 'generation', source: 'fixture', actorLabel: 'fixture', originalDebit: null };
  await mount('/projects', f => {
    f.wallet = { balance: '60', rechargeBalance: '0', giftBalance: '60', legacyBalance: '0' };
    f.service.myRecentLedger = async () => {
      calls++;
      if (calls === 1) return [entry];
      return new Promise(done => { resolve = done; });
    };
    f.service.watchCredits = async (_target, onChange, signal) => {
      changed = onChange;
      await new Promise<void>(done => signal.addEventListener('abort', () => done(), { once: true }));
    };
  });
  await act(() => button('积分余额 60').click());
  await flush();
  assert.equal(document.querySelectorAll('.credit-panel-records li').length, 1);
  await act(async () => { changed(); await new Promise(done => setTimeout(done, 130)); });
  await flush();
  assert.equal(calls, 2);
  assert.equal(document.querySelectorAll('.credit-panel-records li').length, 1);
  assert.doesNotMatch(document.querySelector('.credit-panel-records')!.textContent!, /读取积分记录/);
  await act(() => button('积分余额 60').click());
  await flush();
  await act(() => button('积分余额 60').click());
  await flush();
  assert.equal(calls, 2);
  assert.equal(document.querySelectorAll('.credit-panel-records li').length, 1);
  await act(() => resolve([{ ...entry, identity: 'newest', amount: '6' }, entry]));
  await flush();
  assert.equal(document.querySelectorAll('.credit-panel-records li').length, 2);
});
test('personal credit records retain navigation and server pagination while audit fields stay in operations', async () => {
  await mount('/credits?account=foreign', (f) => {
    f.service.myLedger = async (key) => {
      f.requests.push({ method: 'myLedger', input: key });
      return {
        ...creditStats,
        items: [
          {
            identity: 'refund-receipt',
            at: '2026-10-17T01:00:00Z',
            direction: 'refund',
            amount: '12',
            balance: '64',
            reason: '取消返还',
            source: 'fixture-generation',
            actorLabel: 'fixture_worker',
            originalDebit: 'original-deduction',
          },
        ],
        previous: key ? '' : null,
        next: key ? null : '2',
        caption: key ? '第2页' : '第1页',
      };
    };
  });
  assert.equal(document.querySelector('h1')?.textContent, '积分明细');
  assert.ok(document.querySelector('.jm-sidebar'));
  assert.match(text(), /取消返还.*\+12/s);
  assert.equal(fixture.requests.filter((r) => r.method === 'myCreditBalance').length, 1);
  assert.equal(
    fixture.requests.some((r) => r.method === 'userWallet'),
    false,
  );
  assert.equal(document.querySelector('.personal-credit-ledger details'), null);
  assert.equal(document.querySelector('.personal-credit-entry-detail'), null);
  assert.doesNotMatch(document.querySelector('.personal-credit-ledger')?.textContent || '', /original-deduction|refund-receipt|fixture_worker|fixture-generation|流水编号|积分分配|操作人/);
  await act(async () => button('下一页').click());
  await flush();
  assert.equal(fixture.requests.filter((r) => r.method === 'myLedger').at(-1)?.input, '2');
  assert.equal(document.querySelectorAll('.personal-credit-ledger li').length, 1);
});
test('personal balance and records failures remain explicit instead of displaying zero or an empty wallet', async () => {
  await mount('/credits', (f) => {
    f.service.myCreditBalance = async () => {
      throw new Error('余额服务暂时不可用');
    };
    f.service.myLedger = async () => {
      throw new Error('流水读取失败');
    };
  });
  assert.equal(document.querySelector('.personal-credit-balance-card strong')?.textContent, '—');
  assert.match(text(), /余额服务暂时不可用/);
  assert.match(text(), /流水读取失败/);
  assert.equal(document.querySelector('.personal-credit-empty'), null);
  assert.equal(document.querySelector('.personal-credit-ledger'), null);
});
test('ambiguous credit adjustment is submitted once and reconciled with its original command', async () => {
  await mount('/operations/credits?account=fixture_a', (f) => f.setUncertain(true));
  assert.equal(
    fixture.requests.some((r) => r.method === 'userWallet'),
    false,
  );
  const route = window.location.hash;
  await act(async () => button('调整积分 fiction@example.invalid').click());
  await flush();
  assert.ok(document.querySelector('[role=dialog]'));
  assert.equal(window.location.hash, route);
  await input(document.querySelector<HTMLInputElement>('input[inputmode="numeric"]')!, '1');
  const fields = document.querySelectorAll<HTMLInputElement>('.op-adjustment input');
  await input(fields[1], '虚构测试调整原因');
  await act(async () =>
    document
      .querySelector('.op-adjustment')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.match(text(), /提交结果待确认/);
  assert.equal(fixture.requests.filter((r) => r.method === 'adjust').length, 1);
  assert.equal(button('提交调整').disabled, true);
  await act(async () => button('关闭').click());
  await flush();
  assert.ok(document.querySelector('[role=dialog]') === null);
  assert.match(text(), /提交结果待确认/);
  assert.equal(button('调整积分 fiction@example.invalid').disabled, true);
  await act(async () => button('查看待核对操作').click());
  await flush();
  await act(async () => button('按原编号核对').click());
  await flush();
  assert.equal(fixture.requests.filter((r) => r.method === 'adjust').length, 1);
  assert.equal(fixture.requests.filter((r) => r.method === 'adjustment').length, 1);
  assert.match(text(), /已回读确认记账/);
  await act(async () => button('关闭').click());
  await flush();
});
test('only the credit page exposes credit actions and its paged user list reads wallets on demand', async () => {
  await mount('/operations/users');
  assert.match(
    document.querySelector('.op-table')?.textContent || '',
    /用户名.*虚构用户 A.*fiction@example.invalid/s,
  );
  assert.equal(document.querySelector('.op-row-actions')?.textContent, '禁用');
  assert.ok(document.querySelector('.op-row-actions .jm-button--danger'));
  assert.doesNotMatch(document.querySelector('.op-table')?.textContent || '', /fixture_a/);
  assert.equal(document.querySelector('a[href*="credits?account="]'), null);
  await mount('/operations/credits', (f) => {
    f.service.users = async (search, key) => {
      f.requests.push({ method: 'users', input: { search, key } });
      return {
        items: [
          {
            identity: 'u1',
            username: '虚构运营一',
            email: 'one@example.invalid',
            roleLabel: '运营人员',
            enabled: true,
            createdAt: '2026-10-16T00:00:00Z',
          },
          {
            identity: 'u2',
            username: '虚构用户二',
            email: 'two@example.invalid',
            roleLabel: '普通用户',
            enabled: false,
            createdAt: '2026-10-16T00:00:00Z',
          },
        ],
        previous: key ? '' : null,
        next: key ? null : '2',
        caption: key ? '第 2 页' : '第 1 页',
      };
    };
  });
  assert.equal(document.querySelectorAll('.op-table tbody tr').length, 2);
  assert.doesNotMatch(document.querySelector('.op-table')?.textContent || '', /用户 ID|u1|u2/);
  assert.ok(document.querySelector('.op-status-badge[data-tone=info]'));
  assert.ok(document.querySelector('.op-status-badge[data-tone=danger]'));
  assert.equal(document.querySelectorAll('.op-credit-actions .jm-button').length, 4);
  assert.equal(
    fixture.requests.some((r) => r.method === 'userWallet'),
    false,
  );
  await act(async () => button('下一页').click());
  await flush();
  assert.equal(
    (fixture.requests.filter((r) => r.method === 'users').at(-1)?.input as any).key,
    '2',
  );
  await act(async () => button('余额与流水 two@example.invalid').click());
  await flush();
  assert.equal(
    (fixture.requests.find((r) => r.method === 'userWallet')?.input as any).identity,
    'u2',
  );
  assert.match(document.querySelector('[role=dialog]')?.textContent || '', /9,007,199,254,740,990/);
  assert.equal(document.querySelector('.op-adjustment'), null);
  assert.equal(window.location.hash, '#/operations/credits');
  await act(async () => button('关闭').click());
  await flush();
});
test('registration requirement uses a server-confirmed switch and failed changes are read back', async () => {
  let resolveWrite: () => void = () => {};
  await mount('/operations/grants', (f) => {
    const original = f.service.setRegistration;
    f.service.setRegistration = (required) =>
      new Promise<void>((resolve) => {
        resolveWrite = () => {
          void original(required).then(resolve);
        };
      });
  });
  const setting = document.querySelector<HTMLButtonElement>(
    '[role=switch][aria-label="注册需要授权码"]',
  )!;
  assert.equal(setting.getAttribute('aria-checked'), 'true');
  await act(async () => setting.click());
  await flush();
  assert.equal(setting.disabled, true);
  assert.equal(setting.getAttribute('aria-checked'), 'true');
  await act(async () => resolveWrite());
  await flush();
  assert.equal(document.querySelector('[role=switch]')?.getAttribute('aria-checked'), 'false');
  assert.equal(fixture.requests.filter((r) => r.method === 'setRegistration').length, 1);
  fixture.service.setRegistration = async () => {
    throw new Error('设置提交失败');
  };
  await act(async () => document.querySelector<HTMLButtonElement>('[role=switch]')!.click());
  await flush();
  assert.match(text(), /设置提交失败/);
  assert.equal(document.querySelector('[role=switch]')?.getAttribute('aria-checked'), 'false');
  assert.ok(fixture.requests.filter((r) => r.method === 'registration').length >= 3);
});
test('register server option controls the distinct authorization code and send failure never reports sent', async () => {
  await mount('/register', (f) => {
    f.user = null;
    f.service.sendCode = async () => {
      throw new Error('邮件投递不可用');
    };
  });
  assert.match(text(), /注册授权码/);
  assert.match(text(), /与邮箱验证码不同/);
  await input(
    document.querySelector<HTMLInputElement>('input[autocomplete=username]')!,
    'fiction@example.invalid',
  );
  await act(async () => button('发送验证码').click());
  await flush();
  assert.match(text(), /邮件投递不可用/);
  assert.doesNotMatch(text(), /验证码已发送/);
});
test('focus rechecks preserve authentication inputs for anonymous and already revoked sessions', async () => {
  for (const expired of [false, true])
    for (const route of [
      '/login',
      '/operations-login',
      '/register',
      '/forgot-password',
      '/operations/forgot-password',
    ]) {
      await mount(route, (f) => {
        f.user = null;
        if (expired)
          f.service.current = async () => {
            throw new ApiError(401, 'SESSION_REVOKED', '会话已撤销');
          };
      });
      const form = document.querySelector('form')!,
        fields = [...form.querySelectorAll<HTMLInputElement>('input')];
      for (const [index, field] of fields.entries())
        await input(
          field,
          field.type === 'email' ? 'draft@example.invalid' : 'fixture-draft-' + index,
        );
      const values = fields.map((field) => field.value),
        epoch = sessionBoundary.epoch;
      for (let recheck = 0; recheck < 3; recheck++) {
        let resolve!: () => void;
        fixture.service.current = () =>
          new Promise((done, reject) => {
            resolve = () =>
              expired ? reject(new ApiError(401, 'SESSION_REVOKED', '会话已撤销')) : done(null);
          });
        await act(async () => {
          window.dispatchEvent(new Event('blur'));
          window.dispatchEvent(new Event('focus'));
        });
        assert.ok(document.querySelector('form') === form, 'pending recheck unmounted the form');
        await act(async () => resolve());
        await flush();
        assert.ok(document.querySelector('form') === form, 'anonymous recheck remounted the form');
        assert.deepEqual(
          fields.map((field) => field.value),
          values,
        );
        assert.equal(
          sessionBoundary.epoch,
          epoch,
          'unchanged anonymous identity advanced the account boundary',
        );
      }
    }
});
test('real revocation still clears authenticated content and drafts once; further focus keeps the login form', async () => {
  await mount('/operations/users');
  sessionStorage.setItem('drama:credit-command:private', 'private');
  sessionStorage.setItem('media-command:workspace', 'keep');
  fixture.service.current = async () => {
    throw new ApiError(401, 'SESSION_REVOKED', '会话已撤销');
  };
  const epoch = sessionBoundary.epoch;
  await act(async () => {
    await session.refresh();
  });
  await flush();
  assert.equal(sessionBoundary.epoch, epoch + 1);
  assert.equal(sessionStorage.getItem('drama:credit-command:private'), null);
  assert.equal(sessionStorage.getItem('media-command:workspace'), 'keep');
  assert.equal(document.querySelector('nav[aria-label="运营导航"]'), null);
  assert.equal(window.location.hash, '#/operations-login');
  const email = document.querySelector<HTMLInputElement>('input[autocomplete=username]')!;
  await input(email, 'draft@example.invalid');
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.ok(document.querySelector('input[autocomplete=username]') === email);
  assert.equal(email.value, 'draft@example.invalid');
  assert.equal(sessionBoundary.epoch, epoch + 1);
});
test('account switch clears original drafts, private tree and pending results; late old logout cannot clear B', async () => {
  let resolveLogout: () => void = () => {};
  await mount('/credits', (f) => {
    f.service.logout = () =>
      new Promise<void>((r) => {
        resolveLogout = r;
      });
  });
  sessionStorage.setItem('media-command:old', 'private');
  let outgoing: Promise<void>;
  await act(async () => {
    outgoing = session.logout();
  });
  assert.doesNotMatch(text(), /9,007,199/);
  assert.equal(sessionStorage.getItem('media-command:old'), null);
  fixture.user = {
    identity: 'B',
    username: '虚构用户B',
    email: 'b@example.invalid',
    operationsAllowed: false,
    roleLabel: '普通用户',
  };
  await act(async () => {
    sessionBoundary.reset();
  });
  await act(async () => {
    await session.refresh();
  });
  await flush();
  assert.equal(session.user?.identity, 'B');
  await act(async () => {
    resolveLogout();
    await outgoing!;
  });
  assert.equal(session.user?.identity, 'B');
});
test('workspace authentication routes expose no operations entry or cross-surface links', async () => {
  for (const route of ['/login', '/register', '/forgot-password']) {
    await mount(route, (f) => {
      f.user = null;
    });
    assert.doesNotMatch(text(), /运营|后台/);
    assert.equal(
      [...document.querySelectorAll('a')].some((a) =>
        a.getAttribute('href')?.includes('operations'),
      ),
      false,
    );
    assert.deepEqual(httpRequests, []);
  }
});
test('operations login and password recovery stay within their own routes, including an existing ordinary session', async () => {
  await mount('/operations-login', (f) => {
    f.user = {
      identity: 'ordinary',
      username: '虚构用户',
      email: 'ordinary@example.invalid',
      roleLabel: '普通用户',
      operationsAllowed: false,
    };
  });
  assert.match(text(), /登录运营后台/);
  assert.doesNotMatch(text(), /注册账号|返回创作/);
  assert.deepEqual(
    [...document.querySelectorAll('a')].map((a) => a.getAttribute('href')),
    ['#/operations-login', '#/operations/forgot-password'],
  );
  fixture.service.login = async () => {
    throw new Error('当前账号没有运营权限。');
  };
  await input(
    document.querySelector<HTMLInputElement>('input[autocomplete=username]')!,
    'ordinary@example.invalid',
  );
  await input(
    document.querySelector<HTMLInputElement>('input[type=password]')!,
    'fixture-only-password',
  );
  await act(async () =>
    document
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(session.user?.identity, 'ordinary');
  assert.equal(window.location.hash, '#/operations-login');
  assert.match(text(), /当前账号没有运营权限/);
  await act(async () =>
    document.querySelector<HTMLAnchorElement>('a[href="#/operations/forgot-password"]')!.click(),
  );
  await flush();
  assert.match(text(), /新密码/);
  assert.equal(window.location.hash, '#/operations/forgot-password');
  assert.equal(
    document.querySelector('.account-links a')?.getAttribute('href'),
    '#/operations-login',
  );
  assert.equal(session.user?.identity, 'ordinary');
  assert.equal(
    fixture.requests.some((r) => ['users', 'usage', 'userWallet'].includes(r.method)),
    false,
  );
  assert.deepEqual(httpRequests, []);
});
test('direct operations routes require an operator session before mounting business readers', async () => {
  for (const ordinary of [false, true])
    for (const route of [
      '/operations',
      '/operations/users',
      '/operations/models',
      '/operations/usage',
      '/operations/mail',
      '/operations/credit-policy',
      '/operations/notifications',
    ]) {
      await mount(route, (f) => {
        f.user = ordinary
          ? {
              identity: 'ordinary',
              username: '普通用户',
              email: 'ordinary@example.invalid',
              operationsAllowed: false,
              roleLabel: '普通用户',
            }
          : null;
      });
      assert.equal(window.location.hash, '#/operations-login', route);
      assert.equal(document.querySelector('h1')?.textContent, '登录运营后台');
      assert.doesNotMatch(text(), /无法访问|页面不存在/);
      assert.equal(document.querySelector('.op-shell'), null);
      assert.equal(
        fixture.requests.every((request) => request.method === 'current'),
        true,
      );
      assert.deepEqual(httpRequests, []);
      assert.equal(session.user?.identity, ordinary ? 'ordinary' : undefined);
    }
});
test('ordinary anonymous checks are quiet; replacement uses its popup and actionable account or service errors remain', async () => {
  for (const [status, code] of [
    [401, 'AUTH_REQUIRED'],
    [401, 'SESSION_EXPIRED'],
    [401, 'SESSION_REPLACED'],
    [401, 'SESSION_REVOKED'],
    [401, 'ACCOUNT_DISABLED'],
    [0, 'NETWORK'],
  ] as const) {
    await mount('/operations/users', (f) => {
      f.service.current = async () => {
        throw new ApiError(status, code, '测试会话原因 ' + code);
      };
    });
    assert.equal(window.location.hash, '#/operations-login');
    assert.equal(document.querySelector('h1')?.textContent, '登录运营后台');
    if (code === 'ACCOUNT_DISABLED' || code === 'NETWORK')
      assert.match(document.querySelector('[role=alert]')?.textContent || '', new RegExp(code));
    else assert.equal(document.querySelector('.account-form [role=alert]'), null);
    assert.equal(!!document.querySelector('[role=alertdialog]'), code === 'SESSION_REPLACED');
    assert.equal(session.state, status === 401 ? 'anonymous' : 'unavailable');
    assert.equal(
      fixture.requests.some((request) => ['users', 'usage', 'logout'].includes(request.method)),
      false,
    );
  }
});
test('opening operations confirms its independent session and preserves the workspace draft without logging it out', async () => {
  await mount('/credits', (f) => {
    f.user = {
      identity: 'ordinary',
      username: '普通用户',
      email: 'ordinary@example.invalid',
      operationsAllowed: false,
      roleLabel: '普通用户',
    };
  });
  const epoch = sessionBoundary.epoch;
  sessionStorage.setItem('media-command:ordinary-draft', 'keep');
  fixture.requests.length = 0;
  httpRequests = [];
  fixture.service.current = async () => {
    fixture.requests.push({ method: 'current', input: null });
    return null;
  };
  await act(async () => {
    window.location.hash = '#/operations/users';
  });
  await flush();
  await flush();
  assert.equal(window.location.hash, '#/operations-login');
  assert.equal(session.user, null);
  assert.equal(sessionBoundary.epoch, epoch + 1);
  assert.equal(sessionStorage.getItem('media-command:ordinary-draft'), 'keep');
  assert.equal(fixture.requests.filter((request) => request.method === 'current').length, 1);
  assert.equal(
    fixture.requests.some((request) => request.method === 'logout'),
    false,
  );
  assert.deepEqual(httpRequests, []);
});
test('bootstrap account and email login routes submit their audience and operations logout returns to its own login', async () => {
  for (const operations of [false, true])
    for (const method of ['password', 'email']) {
      await mount(operations ? '/operations-login' : '/login', (f) => {
        f.user = null;
      });
      if (method === 'email') await act(async () => button('验证码登录').click());
      const identity = document.querySelector<HTMLInputElement>('input[autocomplete=username]')!;
      assert.equal(identity.type, method === 'email' ? 'email' : 'text');
      await input(identity, method === 'email' ? 'fixture@example.invalid' : 'Daydream');
      await input(
        document.querySelector<HTMLInputElement>(
          method === 'email' ? 'input[autocomplete="one-time-code"]' : 'input[type=password]',
        )!,
        method === 'email' ? 'fixture-code' : 'fixture-only-password',
      );
      await act(async () =>
        document
          .querySelector('form')!
          .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
      );
      await flush();
      await flush();
      assert.deepEqual(fixture.requests.find((r) => r.method === 'login')?.input, {
        email: method === 'email' ? 'fixture@example.invalid' : 'Daydream',
        operations,
      });
      assert.equal(window.location.hash, operations ? '#/operations/users' : '#/projects');
      if (operations) {
        assert.equal(document.querySelector('a[href="#/projects"]'), null);
        assert.equal(document.querySelectorAll('.op-sidebar-footer button').length, 1);
        assert.equal(button('退出登录'), undefined);
        await act(async () =>
          button('账号菜单：虚构登录账号').dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
          ),
        );
        await flush();
        assert.match(
          document.querySelector('[role=menu]')?.textContent || '',
          method === 'email' ? /虚构登录账号.*fixture@example.invalid/ : /虚构登录账号.*Daydream/,
        );
        const wasDark = document.documentElement.classList.contains('dark');
        await act(async () =>
          document
            .querySelector<HTMLElement>('[role=menuitemcheckbox][data-preference="theme"]')!
            .click(),
        );
        await flush();
        assert.equal(document.documentElement.classList.contains('dark'), !wasDark);
        assert.ok(document.querySelector('[role=menu]'));
        assert.equal(fixture.requests.filter((r) => r.method === 'logout').length, 0);
        await act(async () =>
          document
            .querySelector<HTMLElement>('[role=menu]')!
            .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
        );
        await flush();
        assert.equal(fixture.requests.filter((r) => r.method === 'logout').length, 0);
        assert.equal(document.querySelector('[role=menu]'), null);
        await act(async () =>
          button('账号菜单：虚构登录账号').dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
          ),
        );
        await flush();
        await act(async () =>
          [...document.querySelectorAll<HTMLElement>('[role=menuitem]')]
            .find((item) => item.textContent === '退出登录')!
            .click(),
        );
        await flush();
        assert.equal(fixture.requests.filter((r) => r.method === 'logout').length, 1);
        assert.equal(window.location.hash, '#/operations-login');
        assert.equal(document.querySelector('nav[aria-label="运营导航"]'), null);
      }
    }
});
test('workspace menus and the removed model management route never link to operations, even for an operator', async () => {
  await mount('/model-config', (f) => {
    f.user!.username = '虚构创作者';
  });
  assert.equal(window.location.hash, '#/projects');
  assert.equal(document.querySelector('aside')?.getAttribute('data-collapsed'), 'true');
  await act(async () => button('展开侧栏').click());
  await flush();
  assert.equal(document.querySelector('.xy-sidebar-menu-name')?.textContent, '虚构创作者');
  await act(async () =>
    button('工作台菜单').dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
    ),
  );
  await flush();
  const menu = document.querySelector('[role=menu]')!;
  assert.match(menu.textContent || '', /个人资料/);
  assert.doesNotMatch(menu.textContent || '', /我的积分/);
  assert.ok(menu.querySelector('[role=separator]'));
  assert.equal(menu.querySelector('[role=menuitem]:last-child')?.textContent, '退出登录');
  assert.equal(
    document.querySelector('.xy-preferences-identity strong')?.textContent,
    '虚构创作者',
  );
  assert.match(
    document.querySelector('.xy-preferences-identity')?.textContent || '',
    /operator@example.invalid/,
  );
  assert.doesNotMatch(text(), /运营|后台/);
  assert.equal(document.querySelector('a[href*="operations"]'), null);
  assert.equal(
    httpRequests.some((url) => url.startsWith('/api/operations') || url === '/api/model-config'),
    false,
  );
});

const openProfile = async () => {
  const trigger = document.querySelector<HTMLButtonElement>('.xy-sidebar-menu-trigger')!;
  await act(async () =>
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
  );
  await flush();
  await act(async () =>
    [...document.querySelectorAll<HTMLElement>('[role=menuitem]')]
      .find((item) => item.textContent === '个人资料')!
      .click(),
  );
  await flush();
  await flush();
};
const profileSubmit = async () => {
  await act(async () =>
    document
      .querySelector('[role=dialog] form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
};
test('registration keeps username independent from the login email and does not sign in automatically', async () => {
  await mount('/register', (f) => {
    f.user = null;
  });
  const name = document.querySelector<HTMLInputElement>('input[autocomplete=nickname]')!;
  assert.ok(name?.required);
  await input(name, '虚构创作者🌙');
  await input(
    document.querySelector<HTMLInputElement>('input[autocomplete=username]')!,
    'writer@example.invalid',
  );
  await input(
    document.querySelector<HTMLInputElement>('input[autocomplete=one-time-code]')!,
    'fixture-code',
  );
  await input(
    document.querySelector<HTMLInputElement>('input[type=password]')!,
    'fixture-only-password',
  );
  await input(
    document.querySelector<HTMLInputElement>('input[autocomplete=off]')!,
    'fixture-grant',
  );
  await act(async () =>
    document
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.deepEqual(fixture.requests.find((request) => request.method === 'register')?.input, {
    username: '虚构创作者🌙',
    email: 'writer@example.invalid',
  });
  assert.match(text(), /注册成功，请登录/);
  assert.equal(
    fixture.requests.some((request) => request.method === 'login'),
    false,
  );
});
test('profile menus save the server name into the shared session, retain drafts and reload on reopen', async () => {
  await mount('/operations/users');
  sessionStorage.setItem('media-command:profile-draft', 'keep');
  const epoch = sessionBoundary.epoch;
  await openProfile();
  assert.equal(document.querySelectorAll('[role=dialog]').length, 1);
  const name = document.querySelector<HTMLInputElement>('input[autocomplete=nickname]')!;
  assert.equal(name.value, '虚构运营');
  assert.equal(
    document.querySelector('.account-email strong')?.textContent,
    'operator@example.invalid',
  );
  await input(name, '  创作人🌙  ');
  await act(async () => {
    const form = document.querySelector('[role=dialog] form')!;
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
  await flush();
  assert.equal(fixture.requests.filter((request) => request.method === 'saveProfile').length, 1);
  assert.equal(session.user?.username, '创作人🌙');
  assert.equal(name.value, '创作人🌙');
  assert.equal(document.querySelector('.xy-sidebar-menu-name')?.textContent, '创作人🌙');
  assert.equal(sessionBoundary.epoch, epoch);
  assert.equal(sessionStorage.getItem('media-command:profile-draft'), 'keep');
  assert.match(text(), /个人资料已保存/);
  await input(name, '尚未保存的草稿');
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(name.value, '尚未保存的草稿');
  await act(async () => button('关闭').click());
  await flush();
  await openProfile();
  assert.equal(
    document.querySelector<HTMLInputElement>('input[autocomplete=nickname]')?.value,
    '创作人🌙',
  );
  await act(async () => button('关闭').click());
  await flush();
});
test('ambiguous profile writes keep the draft and require an explicit readback, with no automatic resend', async () => {
  await mount('/operations/users', (f) => {
    const save = f.service.saveProfile;
    f.service.saveProfile = async (name) => {
      await save(name);
      throw new ApiError(0, 'NETWORK', '响应丢失');
    };
  });
  await openProfile();
  await input(
    document.querySelector<HTMLInputElement>('input[autocomplete=nickname]')!,
    '回读用户名',
  );
  await profileSubmit();
  assert.match(text(), /响应丢失/);
  assert.doesNotMatch(text(), /个人资料已保存/);
  assert.equal(button('保存资料').disabled, true);
  assert.equal(fixture.requests.filter((request) => request.method === 'saveProfile').length, 1);
  const current = fixture.service.current;
  fixture.service.current = async () => {
    throw new ApiError(503, 'PROGRAM_FAILED', '读取失败');
  };
  await act(async () => button('读取最新资料').click());
  await flush();
  assert.match(text(), /读取失败/);
  assert.equal(
    document.querySelector<HTMLInputElement>('input[autocomplete=nickname]')?.value,
    '回读用户名',
  );
  assert.equal(button('保存资料').disabled, true);
  fixture.service.current = current;
  await act(async () => button('读取最新资料').click());
  await flush();
  assert.equal(session.user?.username, '回读用户名');
  assert.match(text(), /已读取最新资料/);
  assert.equal(fixture.requests.filter((request) => request.method === 'saveProfile').length, 1);
  await act(async () => button('关闭').click());
  await flush();
});
test('late profile or password A cannot replace B and an older refresh cannot revert a confirmed name', async () => {
  let resolveProfile!: (value: any) => void;
  await mount('/operations/users');
  const a = fixture.user!,
    epoch = sessionBoundary.epoch;
  let pending!: Promise<unknown>;
  await act(async () => {
    pending = session
      .updateProfile(
        () =>
          new Promise((resolve) => {
            resolveProfile = resolve;
          }),
      )
      .catch((error) => error);
  });
  fixture.user = {
    identity: 'profile_B',
    username: '用户B',
    email: 'b@example.invalid',
    operationsAllowed: false,
    roleLabel: '普通用户',
  };
  await act(async () => {
    sessionBoundary.reset();
    await session.refresh();
  });
  await flush();
  await act(async () => {
    resolveProfile({ ...a, username: '旧A结果' });
    await pending;
  });
  assert.equal(session.user?.identity, 'profile_B');
  assert.notEqual(sessionBoundary.epoch, epoch);
  await mount('/operations/users');
  let resolveOld!: (value: any) => void;
  fixture.service.current = () =>
    new Promise((resolve) => {
      resolveOld = resolve;
    });
  let old!: Promise<void>;
  await act(async () => {
    old = session.refresh();
  });
  await act(async () => {
    await session.updateProfile(() => fixture.service.saveProfile('最新用户名'));
  });
  await act(async () => {
    resolveOld({ ...fixture.user!, username: '旧用户名' });
    await old;
  });
  assert.equal(session.user?.username, '最新用户名');
  let resolvePassword!: () => void;
  fixture.service.changePassword = () =>
    new Promise((resolve) => {
      resolvePassword = resolve;
    });
  let passwordResult!: Promise<unknown>;
  await act(async () => {
    passwordResult = session
      .changePassword('fixture-old', 'fixture-new-password')
      .catch((error) => error);
  });
  fixture.user = {
    identity: 'password_B',
    username: '改密后的用户B',
    email: 'b@example.invalid',
    operationsAllowed: false,
    roleLabel: '普通用户',
  };
  fixture.service.current = async () => fixture.user;
  await act(async () => {
    sessionBoundary.reset();
    await session.refresh();
  });
  await act(async () => {
    resolvePassword();
    await passwordResult;
  });
  assert.equal(session.user?.identity, 'password_B');
});
test('avatar controls consume protected server URLs; password failures keep the session and success revokes it', async () => {
  await mount('/operations/users');
  await openProfile();
  const choose = document.querySelector<HTMLInputElement>('input[type=file]')!;
  Object.defineProperty(choose, 'files', {
    value: [new dom.window.File(['fixture'], 'avatar.png', { type: 'image/png' })],
    configurable: true,
  });
  await act(async () => choose.dispatchEvent(new Event('change', { bubbles: true })));
  await flush();
  assert.equal(fixture.requests.filter((request) => request.method === 'uploadAvatar').length, 1);
  assert.match(session.user?.avatarUrl || '', /^\/api\/auth\/avatar\?v=/);
  assert.ok(document.querySelector('.account-profile-photo img'));
  await act(async () => button('移除头像').click());
  await flush();
  assert.equal(session.user?.avatarUrl, null);
  await act(async () => button('账号安全').click());
  await flush();
  const fields = [
    ...document.querySelectorAll<HTMLInputElement>('[role=dialog] input[type=password]'),
  ];
  for (const [index, field] of fields.entries())
    await input(
      field,
      index === 0 ? 'fiction-old' : index === 1 ? 'fiction-new-password' : 'mismatch',
    );
  await profileSubmit();
  assert.match(text(), /两次输入的新密码不一致/);
  assert.equal(
    fixture.requests.some((request) => request.method === 'changePassword'),
    false,
  );
  await input(fields[2], 'fiction-new-password');
  const change = fixture.service.changePassword;
  fixture.service.changePassword = async () => {
    throw new ApiError(400, 'CURRENT_PASSWORD_INVALID', '原密码不正确');
  };
  await profileSubmit();
  assert.match(text(), /原密码不正确/);
  assert.ok(session.user);
  assert.equal(fields[1].value, 'fiction-new-password');
  fixture.service.changePassword = change;
  await profileSubmit();
  await flush();
  assert.equal(session.user, null);
  assert.equal(window.location.hash, '#/operations-login');
  assert.match(text(), /密码已修改/);
  assert.equal(fixture.requests.filter((request) => request.method === 'changePassword').length, 1);
  assert.equal(
    fixture.requests.some((request) => request.method === 'logout'),
    false,
  );
});
test('ordinary users edit their own profile from the workspace menu without any operations query', async () => {
  await mount('/credits', (f) => {
    f.user = {
      identity: 'profile_ordinary',
      username: '普通创作者',
      email: 'ordinary@example.invalid',
      operationsAllowed: false,
      roleLabel: '普通用户',
    };
  });
  const epoch = sessionBoundary.epoch;
  await openProfile();
  await input(
    document.querySelector<HTMLInputElement>('input[autocomplete=nickname]')!,
    '本人新用户名',
  );
  await profileSubmit();
  assert.equal(session.user?.identity, 'profile_ordinary');
  assert.equal(session.user?.username, '本人新用户名');
  assert.equal(session.user?.email, 'ordinary@example.invalid');
  assert.equal(session.user?.operationsAllowed, false);
  assert.equal(sessionBoundary.epoch, epoch);
  assert.equal(
    fixture.requests.some((request) =>
      ['users', 'usage', 'userWallet', 'setUserEnabled'].includes(request.method),
    ),
    false,
  );
  assert.equal(
    httpRequests.some((url) => url.startsWith('/api/operations')),
    false,
  );
  assert.equal(document.querySelector('a[href*=operations]'), null);
  await act(async () => button('关闭').click());
  await flush();
});

const mailField = (name: string) =>
  [...document.querySelectorAll<HTMLLabelElement>('.op-mail-fields label')]
    .find((label) => label.querySelector('span')?.textContent === name)!
    .querySelector<HTMLInputElement>('input')!;
const mailSubmit = async () => {
  await act(async () =>
    document
      .querySelector('.op-mail-form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
};
const smtpFixture = {
  revision: 3,
  enabled: false,
  address: 'smtp.fixture.invalid:587',
  username: 'sender@fixture.invalid',
  from: 'sender@fixture.invalid',
  tls: 'starttls' as const,
  passwordConfigured: true,
};
test('SMTP settings save once with a confirmed revision and clear new secret input only on success', async () => {
  let resolve!: () => void;
  await mount('/operations/mail', (f) => {
    const save = f.service.saveSmtp;
    f.service.saveSmtp = (input) =>
      new Promise((done) => {
        resolve = () => {
          void save(input).then(done);
        };
      });
  });
  assert.match(text(), /未配置/);
  assert.equal(button('读取最新配置'), undefined);
  assert.equal(mailField('SMTP 密码 / 授权码').value, '');
  await act(async () => button('启用邮件服务').click());
  await input(mailField('SMTP 地址'), 'smtp.fixture.invalid:587');
  await input(mailField('发送邮箱'), 'sender@fixture.invalid');
  await input(mailField('SMTP 认证用户名'), 'sender@fixture.invalid');
  await input(mailField('SMTP 密码 / 授权码'), 'fixture-only-secret');
  assert.equal((document.querySelector('.op-mail-form') as HTMLFormElement).checkValidity(), true);
  await act(async () => {
    const form = document.querySelector('.op-mail-form')!;
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
  assert.equal(button('保存配置').disabled, true);
  assert.equal(mailField('SMTP 地址').disabled, true);
  assert.match(text(), /未配置/);
  await act(async () => resolve());
  await flush();
  assert.match(text(), /已启用/);
  assert.match(text(), /邮件配置已保存/);
  assert.equal(mailField('SMTP 密码 / 授权码').value, '');
  assert.equal(fixture.requests.filter((request) => request.method === 'saveSmtp').length, 1);
  assert.doesNotMatch(text(), /fixture-only-secret/);
  assert.equal(session.user?.identity, 'fixture_operator');
});
test('SMTP empty password preserves the secret and clearing requires an explicit disabled-service action', async () => {
  await mount('/operations/mail', (f) => {
    f.smtp = smtpFixture;
  });
  assert.equal(mailField('SMTP 密码 / 授权码').value, '');
  await mailSubmit();
  const kept = fixture.requests.find((request) => request.method === 'saveSmtp')!.input as any;
  assert.equal('password' in kept, false);
  assert.equal(kept.clearPassword, false);
  assert.equal(fixture.smtp.passwordConfigured, true);
  await act(async () => document.querySelector<HTMLInputElement>('.op-mail-clear input')!.click());
  assert.equal(mailField('SMTP 密码 / 授权码').disabled, true);
  assert.equal(button('启用邮件服务').disabled, true);
  await mailSubmit();
  const cleared = fixture.requests.filter((request) => request.method === 'saveSmtp').at(-1)!
    .input as any;
  assert.equal(cleared.clearPassword, true);
  assert.equal(cleared.enabled, false);
  assert.equal(fixture.smtp.passwordConfigured, false);
  assert.equal(document.querySelector('.op-mail-clear'), null);
});
test('SMTP conflict preserves the draft and blocks writes until an explicit successful reread', async () => {
  await mount('/operations/mail', (f) => {
    f.smtp = smtpFixture;
  });
  await input(mailField('SMTP 地址'), 'draft.fixture.invalid:465');
  fixture.smtp = { ...smtpFixture, revision: 4, address: 'updated.fixture.invalid:587' };
  await mailSubmit();
  assert.match(text(), /配置已被更新/);
  assert.equal(mailField('SMTP 地址').value, 'draft.fixture.invalid:465');
  assert.equal(button('保存配置').disabled, true);
  assert.equal(fixture.requests.filter((request) => request.method === 'smtp').length, 1);
  const read = fixture.service.smtp;
  fixture.service.smtp = async () => {
    throw new ApiError(503, 'MAIL_CONFIGURATION_UNAVAILABLE', '读取失败');
  };
  await act(async () => button('读取最新配置').click());
  await flush();
  assert.equal(mailField('SMTP 地址').value, 'draft.fixture.invalid:465');
  assert.equal(button('保存配置').disabled, true);
  fixture.service.smtp = read;
  await act(async () => button('读取最新配置').click());
  await flush();
  assert.equal(mailField('SMTP 地址').value, 'updated.fixture.invalid:587');
  assert.doesNotMatch(text(), /配置版本/);
  assert.equal(fixture.smtp.revision, 4);
  assert.equal(button('保存配置').disabled, false);
  assert.equal(fixture.requests.filter((request) => request.method === 'saveSmtp').length, 1);
});
test('SMTP committed but unknown save is read back without an automatic retry or false success', async () => {
  await mount('/operations/mail', (f) => {
    f.smtp = smtpFixture;
    const save = f.service.saveSmtp;
    f.service.saveSmtp = async (input) => {
      await save(input);
      throw new ApiError(503, 'MAIL_CONFIG_SAVE_UNKNOWN', '提交不明');
    };
  });
  await input(mailField('SMTP 密码 / 授权码'), 'fixture-only-new-secret');
  await mailSubmit();
  assert.doesNotMatch(text(), /邮件配置已保存/);
  assert.match(text(), /保存结果未确认/);
  assert.equal(button('保存配置').disabled, true);
  await act(async () => button('读取最新配置').click());
  await flush();
  assert.match(text(), /已读取最新配置/);
  assert.equal(mailField('SMTP 密码 / 授权码').value, '');
  assert.doesNotMatch(text(), /配置版本/);
  assert.equal(fixture.smtp.revision, 4);
  assert.equal(fixture.requests.filter((request) => request.method === 'saveSmtp').length, 1);
});
test('SMTP rejected response boundaries block another write until the saved configuration is reread', async () => {
  await mount('/operations/mail', (f) => {
    const save = f.service.saveSmtp;
    f.service.saveSmtp = async (input) => {
      await save(input);
      throw new ApiError(200, 'CONTENT_BOUNDARY_VIOLATION', '回执超出配置合同');
    };
  });
  await input(mailField('SMTP 地址'), 'smtp.fixture.invalid:587');
  await mailSubmit();
  assert.match(text(), /回执超出配置合同/);
  assert.equal(button('保存配置').disabled, true);
  assert.doesNotMatch(text(), /邮件配置已保存/);
  await act(async () => button('读取最新配置').click());
  await flush();
  assert.equal(mailField('SMTP 地址').value, 'smtp.fixture.invalid:587');
  assert.doesNotMatch(text(), /配置版本/);
  assert.equal(fixture.smtp.revision, 1);
  assert.equal(fixture.requests.filter((request) => request.method === 'saveSmtp').length, 1);
});
test('ordinary users never query SMTP and a late operator settings read cannot populate a new account', async () => {
  await mount('/operations/mail', (f) => {
    f.user = {
      identity: 'ordinary',
      username: '普通用户',
      email: 'ordinary@example.invalid',
      operationsAllowed: false,
      roleLabel: '普通用户',
    };
  });
  assert.equal(document.querySelector('h1')?.textContent, '登录运营后台');
  assert.equal(window.location.hash, '#/operations-login');
  assert.equal(
    fixture.requests.some((request) => request.method === 'smtp'),
    false,
  );
  let resolve!: (value: typeof smtpFixture) => void;
  await mount('/operations/mail', (f) => {
    f.service.smtp = () =>
      new Promise((done) => {
        resolve = done;
      });
  });
  fixture.user = {
    identity: 'new_account',
    username: '用户B',
    email: 'b@example.invalid',
    operationsAllowed: false,
    roleLabel: '普通用户',
  };
  await act(async () => {
    sessionBoundary.reset();
    await session.refresh();
  });
  await act(async () => resolve(smtpFixture));
  await flush();
  assert.equal(session.user?.identity, 'new_account');
  assert.equal(document.querySelector('.op-mail-form'), null);
  assert.doesNotMatch(text(), /smtp.fixture.invalid/);
});
test('usage page size and shared filters persist across independent task and call views', async () => {
  await mount();
  const pagination = document.querySelector('nav[aria-label="分页"]')!;
  assert.equal(button('每页条数').closest('nav'), pagination);
  assert.deepEqual([...pagination.children].map((node) => node.getAttribute('aria-label') || node.textContent), [
    '每页条数', '第 1 页 · 虚构夹具', '上一页', '下一页',
  ]);
  assert.doesNotMatch(text(), /统计范围与当前视角、筛选一致|读取于/);
  await act(async () => button('下一页').click());
  await flush();
  assert.equal((fixture.requests.filter((request) => request.method === 'usage').at(-1)!.input as any).key, 'next');
  await act(async () =>
    button('每页条数').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
  );
  await flush();
  await act(async () =>
    [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')]
      .find((item) => item.textContent === '10 条 / 页')!
      .click(),
  );
  await flush();
  assert.equal(
    (fixture.requests.filter((request) => request.method === 'usage').at(-1)!.input as any)
      .pageSize,
    '10',
  );
  assert.equal((fixture.requests.filter((request) => request.method === 'usage').at(-1)!.input as any).key, '');
  assert.match(text(), /¥1.320000123/);
  await act(async () => button('详情 整剧解析 虚构用户 A').click());
  await flush();
  await act(async () => button('调用明细').click());
  await flush();
  await act(async () => button('下一页').click());
  await flush();
  await act(async () =>
    button('每页条数').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
  );
  await flush();
  await act(async () =>
    [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')]
      .find((item) => item.textContent === '50 条 / 页')!
      .click(),
  );
  await flush();
  await act(async () => button('任务汇总').click());
  await flush();
  assert.equal(button('每页条数').textContent, '50 条 / 页');
  const query = fixture.requests.filter((request) => request.method === 'usage').at(-1)!
    .input as any;
  assert.equal(query.view, 'tasks');
  assert.equal(query.pageSize, '50');
  assert.equal(query.key, '');
  assert.match(text(), /筛选范围 · 任务/);
});
test('usage merged pagination keeps the latest view and page size when an old page resolves late', async () => {
  type Result = Awaited<ReturnType<typeof fixture.service.usage>>;
  let release!: (value: Result) => void;
  let oldPage!: Result;
  await mount('/operations/usage', (f) => {
    const read = f.service.usage.bind(f.service);
    f.service.usage = async (...input) => {
      const value = await read(...input);
      if (input[0] === 'tasks' && input[2] === 'next' && input[3] === '20') {
        oldPage = { ...value, summary: { ...value.summary, knownCostCny: '9999.99' } };
        return new Promise((resolve) => { release = resolve; });
      }
      return value;
    };
  });
  await act(async () => button('下一页').click());
  await flush();
  await act(async () => button('调用明细').click());
  await flush();
  await act(async () => button('每页条数').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })));
  await flush();
  await act(async () => [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')].find((item) => item.textContent === '50 条 / 页')!.click());
  await flush();
  await act(async () => release(oldPage));
  await flush();
  assert.match(text(), /筛选范围 · 调用/);
  assert.doesNotMatch(text(), /9999|9,999/);
  assert.equal(button('每页条数').textContent, '50 条 / 页');
  assert.equal(button('每页条数').closest('nav')?.getAttribute('aria-label'), '分页');
  await act(async () => button('任务汇总').click());
  await flush();
  const query = fixture.requests.filter((request) => request.method === 'usage').at(-1)!.input as any;
  assert.equal(query.view, 'tasks');
  assert.equal(query.pageSize, '50');
  assert.equal(query.key, '');
});
test('gift records localize in the balance popup and personal details without translating operator reasons or writing credits', async () => {
  const { default: i18n } = await import('../src/i18n');
  const records = ['daily-gift', 'registration-gift', 'daily-gift-expiration', 'operations-adjustment', 'toString'].map((source, index) => ({
    identity: 'localized-gift-' + index,
    at: '2026-10-19T02:00:00Z',
    direction: index === 2 ? 'expire' as const : 'add' as const,
    category: 'gift' as const,
    amount: '60', balance: '60', giftAmount: '60', rechargeAmount: '0', legacyAmount: '0',
    source,
    reason: index === 2 ? '每日赠送到期回收' : index === 3 ? 'daily-gift' : source,
    actorLabel: index === 2 ? 'system' : 'fixture-user',
    originalDebit: null,
  }));
  await mount('/credits', (f) => {
    f.service.myLedger = async () => ({ ...creditStats, items: records, previous: null, next: null, caption: '本地化夹具' });
    f.service.myRecentLedger = async () => records;
  });
  const titles = () => [...document.querySelectorAll('.personal-credit-reason strong')].map(el => el.textContent);
  assert.deepEqual(titles(), ['每日赠送', '注册赠送', '每日赠送到期回收', 'daily-gift', 'toString']);
  assert.equal(document.querySelector('.personal-credit-entry-detail'), null);
  const requestsBefore = fixture.requests.map(row => row.method);
  try {
    await act(async () => i18n.changeLanguage('en'));
    await flush();
    assert.deepEqual(titles(), ['Daily bonus', 'Registration bonus', 'Daily bonus expired', 'daily-gift', 'toString']);
    assert.equal(document.querySelector('.personal-credit-entry-detail'), null);
    assert.deepEqual(fixture.requests.map(row => row.method), requestsBefore);
    await act(async () => document.querySelector<HTMLButtonElement>('.credit-balance-trigger')!.click());
    await flush();
    assert.deepEqual([...document.querySelectorAll('.credit-panel-records strong')].map(el => el.textContent), titles());
    assert.equal(document.querySelector('.credit-panel-records-heading a')?.textContent, 'View credit details');
    assert.match(document.querySelector('.credit-panel-records .credit-record-amount')?.getAttribute('aria-label') || '', /Received 60 credits/);
    await act(async () => i18n.changeLanguage('zh'));
    await flush();
    assert.equal(document.querySelector('.credit-panel-records strong')?.textContent, '每日赠送');
  } finally {
    await act(async () => i18n.changeLanguage('zh'));
  }
  assert.equal(fixture.requests.some(row => row.method === 'adjust' || row.method === 'saveCreditPolicy'), false);
});

test('operator ledger uses the same localized system reasons and retains custom audit text', async () => {
  const { default: i18n } = await import('../src/i18n');
  await mount('/operations/credits', (f) => {
    f.service.userWallet = async () => ({ ...f.wallet, ledger: { ...creditStats, items: [
      { identity:'gift-daily', at:'2026-10-19T02:00:00Z', direction:'add', category:'gift', amount:'60', balance:'60', reason:'daily-gift', source:'daily-gift', actorLabel:'fixture-user', originalDebit:null },
      { identity:'custom-adjust', at:'2026-10-19T02:00:00Z', direction:'add', category:'gift', amount:'60', balance:'120', reason:'运营说明 daily-gift 保持原文', source:'operations-adjustment', actorLabel:'fixture-operator', originalDebit:null },
      { identity:'payment-add', at:'2026-10-20T02:00:00Z', direction:'add', category:'recharge', amount:'100', balance:'220', reason:'充值到账', source:'payment-order:PO20261020120000ABCDEFGHIJ', actorLabel:'payment:demo', originalDebit:null, originalAddition:null },
    ], previous:null, next:null, caption:'运营流水夹具' } });
  });
  await act(async () => button('余额与流水').click());
  await flush();
  const reasons = () => [...document.querySelectorAll('.op-reason')].map(el => el.textContent);
  assert.deepEqual(reasons(), ['每日赠送', '运营说明 daily-gift 保持原文', '充值到账']);
  assert.match(document.querySelector('.op-reason')?.closest('table')?.textContent || '', /积分充值演示支付/);
  assert.doesNotMatch(document.querySelector('.op-reason')?.closest('table')?.textContent || '', /payment:demo|创作生成/);
  try {
    await act(async () => i18n.changeLanguage('en'));
    await flush();
    assert.deepEqual(reasons(), ['Daily bonus', '运营说明 daily-gift 保持原文', 'Recharge credited']);
    assert.match(document.querySelector('.op-reason')?.closest('table')?.textContent || '', /Demo payment/);
    assert.match(document.querySelector('.op-reason')?.closest('table')?.textContent || '', /Operator adjustment/);
    assert.equal(fixture.requests.some(row => row.method === 'adjust'), false);
  } finally {
    await act(async () => i18n.changeLanguage('zh'));
  }
});

test('credit record labels filter rows while consumption analysis retains its own category and time scope', async () => {
  await mount('/credits', (f) => {
    f.wallet = { balance: '164', rechargeBalance: '104', giftBalance: '60', legacyBalance: '0' };
    f.service.myLedger = async (key, filters) => {
      f.requests.push({ method: 'creditScope', input: { key, filters } });
      const consumed = filters?.direction === 'consumed';
      return {
        items: [
          {
            identity: consumed ? 'mixed-deduction' : 'recharge-grant',
            at: '2026-10-17T01:00:00Z',
            direction: consumed ? 'deduct' : 'add',
            category: consumed ? 'mixed' : 'recharge',
            amount: consumed ? '12' : '160',
            rechargeAmount: consumed ? '2' : '160',
            giftAmount: consumed ? '10' : '0',
            legacyAmount: '0',
            balance: '164',
            reason: consumed ? '完整任务预扣' : '充值记录',
            source: 'language-generation:fixture',
            actorLabel: 'fixture-user',
            originalDebit: null,
          },
        ],
        previous: key ? '1' : null,
        next: key ? null : '2',
        caption: '服务端筛选结果',
        summary: consumed
          ? {
              acquired: '0',
              consumed: '19',
              refunded: '7',
              netConsumed: '12',
              settledConsumed: '9',
              pending: '3',
            }
          : { ...creditStats.summary, acquired: '160' },
        analysis: consumed ? [{ source: 'language-generation', amount: '9' }] : [],
      };
    };
  });
  assert.equal(document.querySelectorAll('.personal-credit-categories article').length, 2);
  assert.match(
    document.querySelector('.personal-credit-categories')?.textContent || '',
    /充值积分104.*赠送及免费积分60/s,
  );
  assert.doesNotMatch(text(), /订阅积分|升级会员|购买积分/);
  assert.match(
    document.querySelector('.personal-credit-analysis')?.textContent || '',
    /已结算消耗9积分.*待结算预扣 3 积分.*文本生成9 积分/s,
  );
  assert.equal(
    document.querySelector<HTMLElement>('.personal-credit-analysis-track span')?.style.width,
    '100%',
  );
  assert.equal(
    document.querySelectorAll('[role=tablist][aria-label="积分记录方向"] [role=tab]').length,
    4,
  );
  assert.equal(button('已获取').getAttribute('aria-selected'), 'true');
  assert.match(document.querySelector('.personal-credit-ledger')?.textContent || '', /充值记录/);
  await act(async () =>
    button('已获取').dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }),
    ),
  );
  await flush();
  assert.equal(
    (fixture.requests.filter((row) => row.method === 'creditScope').at(-1)?.input as any).filters
      .direction,
    'consumed',
  );
  assert.equal(document.activeElement, button('已消耗'));
  assert.match(
    document.querySelector('.personal-credit-ledger')?.textContent || '',
    /完整任务预扣/,
  );
  assert.match(
    document.querySelector('.personal-credit-analysis')?.textContent || '',
    /已结算消耗9积分/,
  );
  await act(async () =>
    button('积分分类').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
  );
  await flush();
  await act(async () =>
    [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')]
      .find((row) => row.textContent === '赠送及免费积分')!
      .click(),
  );
  await flush();
  assert.equal(
    (fixture.requests.filter((row) => row.method === 'creditScope').at(-1)?.input as any).filters
      .category,
    'gift',
  );
  assert.equal(
    (fixture.requests.filter((row) => row.method === 'creditScope').at(-1)?.input as any).key,
    '',
  );
  await act(async () => button('下一页').click());
  await flush();
  const scope = fixture.requests.filter((row) => row.method === 'creditScope').at(-1)?.input as any;
  assert.equal(scope.key, '2');
  assert.equal(scope.filters.category, 'gift');
  assert.equal(scope.filters.direction, 'consumed');
  assert.equal(document.querySelector('.personal-credit-ledger summary'), null);
  assert.doesNotMatch(document.querySelector('.personal-credit-ledger')?.textContent || '', /充值 2 · 赠送 10|mixed-deduction|fixture-user/);
});
test('legacy credit facts are shown only when they exist, and zero settled consumption never renders a fabricated chart', async () => {
  await mount('/credits', (f) => {
    f.wallet = { balance: '60', rechargeBalance: '0', giftBalance: '0', legacyBalance: '60' };
  });
  assert.equal(document.querySelectorAll('.personal-credit-categories article').length, 3);
  assert.match(text(), /历史未分类积分60/);
  assert.equal(document.querySelector('.personal-credit-analysis-track'), null);
  assert.match(text(), /暂无已结算消耗/);
});
test('credit tabs retain mounted results without an update prompt, reject late filters and expose failed refresh without flashing empty', async () => {
  const result = (reason: string) => ({
    ...creditStats,
    items: [
      {
        identity: reason,
        at: '2026-10-17T01:00:00Z',
        direction: 'add' as const,
        amount: '12',
        balance: '64',
        reason,
        source: 'fixture',
        actorLabel: 'fixture',
      },
    ],
    previous: null,
    next: null,
    caption: '共1条',
  });
  await mount('/credits', (f) => {
    f.wallet = { balance: '64', rechargeBalance: '4', giftBalance: '60', legacyBalance: '0' };
    f.service.myLedger = async () => result('已确认记录');
  });
  const card = document.querySelector('.personal-credit-record-card'),
    list = document.querySelector('.personal-credit-ledger'),
    analysis = document.querySelector('.personal-credit-analysis');
  const pending: { resolve: (v: any) => void; reject: (e: Error) => void }[] = [];
  fixture.service.myLedger = () =>
    new Promise((resolve, reject) => pending.push({ resolve, reject }));
  await act(async () => button('已消耗').click());
  await flush();
  assert.equal(document.querySelector('.personal-credit-ledger'), list);
  assert.equal(document.querySelector('.personal-credit-record-card'), card);
  assert.equal(card?.getAttribute('aria-busy'), 'true');
  assert.match(card?.textContent || '', /已确认记录/);
  assert.doesNotMatch(
    document.querySelector('.personal-credit-record-heading')?.textContent || '',
    /正在更新/,
  );
  assert.equal(card?.querySelector('.op-feedback'), null);
  assert.equal(document.querySelector('.personal-credit-analysis'), analysis);
  assert.equal(button('下一页').disabled, true);
  await act(async () => button('已获取').click());
  await flush();
  await act(async () => pending[0].resolve(result('迟到的消耗结果')));
  await flush();
  assert.doesNotMatch(card?.textContent || '', /迟到的消耗结果/);
  await act(async () => pending[1].resolve(result('最新获取结果')));
  await flush();
  assert.match(card?.textContent || '', /最新获取结果/);
  assert.equal(card?.getAttribute('aria-busy'), 'false');
  assert.equal(document.querySelector('.personal-credit-analysis'), analysis);
  await act(async () => button('已消耗').click());
  await flush();
  await act(async () => pending[2].reject(new Error('本次流水更新失败')));
  await flush();
  assert.match(card?.textContent || '', /本次流水更新失败.*最新获取结果/s);
  assert.match(
    document.querySelector('.personal-credit-record-heading')?.textContent || '',
    /显示上次读取结果/,
  );
  assert.equal(card?.querySelector('.personal-credit-empty'), null);
  fixture.service.myLedger = async () => result('重读成功');
  await act(async () => button('重新读取').click());
  await flush();
  assert.match(card?.textContent || '', /重读成功/);
  assert.doesNotMatch(text(), /显示上次读取结果|本次流水更新失败/);
});
test('credit refresh retains confirmed balance within the session and drops retained A results before B reads finish', async () => {
  await mount('/credits', (f) => {
    f.wallet = { balance: '64', rechargeBalance: '4', giftBalance: '60', legacyBalance: '0' };
  });
  const categories = document.querySelector('.personal-credit-categories');
  let balanceReply!: (v: any) => void;
  fixture.service.myCreditBalance = () =>
    new Promise((resolve) => {
      balanceReply = resolve;
    });
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.ok(button('积分余额 64'));
  assert.equal(document.querySelector('.personal-credit-categories'), categories);
  assert.match(categories?.textContent || '', /充值积分4.*赠送及免费积分60/s);
  await act(async () =>
    balanceReply({ balance: '81', rechargeBalance: '21', giftBalance: '60', legacyBalance: '0' }),
  );
  await flush();
  assert.ok(button('积分余额 81'));
  let ledgerReply!: (v: any) => void;
  fixture.service.myLedger = () =>
    new Promise((resolve) => {
      ledgerReply = resolve;
    });
  await act(async () => button('已消耗').click());
  await flush();
  const oldLedgerReply = ledgerReply;
  fixture.user = {
    identity: 'credit-B',
    username: '积分用户B',
    email: 'b@example.invalid',
    operationsAllowed: false,
    roleLabel: '普通用户',
  };
  await act(async () => {
    sessionBoundary.reset();
    await session.refresh();
  });
  await flush();
  assert.equal(button('积分余额 81'), undefined);
  assert.equal(button('积分余额 64'), undefined);
  assert.equal(document.querySelector('.personal-credit-ledger'), null);
  assert.match(document.querySelector('.personal-credit-balance-value')?.textContent || '', /—/);
  await act(async () =>
    oldLedgerReply({
      ...creditStats,
      items: [
        {
          identity: 'late-A',
          at: '2026-10-17T01:00:00Z',
          direction: 'add',
          amount: '12',
          balance: '64',
          reason: 'A账号迟到私有记录',
          source: 'fixture',
          actorLabel: 'A',
        },
      ],
      previous: null,
      next: null,
      caption: '共1条',
    }),
  );
  await flush();
  assert.equal(document.querySelector('.personal-credit-ledger'), null);
  assert.doesNotMatch(text(), /A账号迟到私有记录/);
  assert.equal(session.user?.identity, 'credit-B');
});
test('operations issuance records an explicit recharge category in its original recoverable command', async () => {
  await mount('/operations/credits');
  await act(async () => button('调整积分 fiction@example.invalid').click());
  await flush();
  await act(async () =>
    button('发放类型').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
  );
  await flush();
  await act(async () =>
    [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')]
      .find((row) => row.textContent === '充值积分')!
      .click(),
  );
  await flush();
  const fields = document.querySelectorAll<HTMLInputElement>('.op-adjustment input');
  await input(fields[0], '1');
  await input(fields[1], '虚构充值登记');
  await act(async () =>
    document
      .querySelector('.op-adjustment')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const command = fixture.requests.find((row) => row.method === 'adjust')?.input as any;
  assert.equal(command.category, 'recharge');
  assert.equal(command.direction, 'add');
  assert.match(text(), /已确认记账/);
  await act(async () => button('关闭').click());
  await flush();
});
test('gift settings save nonnegative integers with CAS and preserve a conflicting draft until explicit readback', async () => {
  await mount('/operations/credit-policy');
  assert.equal(document.querySelector('h1')?.textContent, '积分赠送设置');
  assert.equal(button('读取最新设置'), undefined);
  const registration = document.querySelector<HTMLInputElement>('[aria-label="新用户注册赠送"]')!,
    daily = document.querySelector<HTMLInputElement>('[aria-label="每日赠送"]')!;
  await input(registration, '0');
  await input(daily, '60');
  await act(async () =>
    document
      .querySelector('.op-credit-policy-form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.deepEqual(fixture.requests.find((row) => row.method === 'saveCreditPolicy')?.input, {
    revision: 0,
    registrationGift: 0,
    dailyGift: 60,
  });
  assert.match(text(), /赠送设置已保存/);
  assert.equal(daily.value, '60');
  const original = fixture.service.saveCreditPolicy;
  fixture.service.saveCreditPolicy = async () => {
    throw new ApiError(409, 'CREDIT_POLICY_CONFLICT', '设置已变化');
  };
  await input(daily, '80');
  await act(async () =>
    document
      .querySelector('.op-credit-policy-form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(daily.value, '80');
  assert.equal(button('保存设置').disabled, true);
  assert.match(text(), /先读取最新设置核对/);
  fixture.service.saveCreditPolicy = original;
  await act(async () => button('读取最新设置').click());
  await flush();
  assert.equal(daily.value, '60');
  assert.equal(button('保存设置').disabled, false);
  assert.equal(fixture.requests.filter((row) => row.method === 'saveCreditPolicy').length, 1);
});
test('gift settings reject fractional or overflowing drafts without sending a write', async () => {
  await mount('/operations/credit-policy');
  const daily = document.querySelector<HTMLInputElement>('[aria-label="每日赠送"]')!;
  for (const value of ['1.5', '9007199254740992']) {
    await input(daily, value);
    await act(async () =>
      document
        .querySelector('.op-credit-policy-form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    );
    await flush();
    assert.ok(document.querySelector('[role=alert]'));
  }
  assert.equal(
    fixture.requests.some((row) => row.method === 'saveCreditPolicy'),
    false,
  );
});
test('workspace daily gift is a separate once-per-entry command and a failed claim offers explicit safe reconciliation', async () => {
  await mount('/projects', (f) => {
    f.policy = { revision: 1, registrationGift: '0', dailyGift: '60', timezone: 'Asia/Shanghai' };
  });
  assert.equal(fixture.requests.filter((row) => row.method === 'dailyGift').length, 1);
  for (let index = 0; index < 3; index++) {
    await act(async () => window.dispatchEvent(new Event('focus')));
    await flush();
  }
  assert.equal(fixture.requests.filter((row) => row.method === 'dailyGift').length, 1);
  await mount('/projects', (f) => {
    f.service.dailyGift = async () => {
      f.requests.push({ method: 'failedDailyGift' });
      throw new ApiError(503, 'CREDIT_COMMIT_UNKNOWN', '提交结果未确认');
    };
  });
  assert.match(
    document.querySelector('.daily-credit-feedback')?.textContent || '',
    /每日赠送未确认/,
  );
  assert.equal(fixture.requests.filter((row) => row.method === 'failedDailyGift').length, 1);
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(fixture.requests.filter((row) => row.method === 'failedDailyGift').length, 1);
  fixture.service.dailyGift = async () => {
    fixture.requests.push({ method: 'reconcileDailyGift' });
    return {
      date: '2026-10-17',
      amount: '60',
      claimed: true,
      alreadyClaimed: true,
    };
  };
  await act(async () => button('重试核对').click());
  await flush();
  assert.equal(fixture.requests.filter((row) => row.method === 'reconcileDailyGift').length, 1);
  assert.equal(document.querySelector('.daily-credit-feedback'), null);
});

test('zero daily gift can be claimed on same-day workspace return, while a positive claim is cached', async (t) => {
  const { toast } = await import('../src/components/ui/sonner');
  const success = t.mock.method(toast, 'success');
  await mount('/projects');
  const claims = () => fixture.requests.filter((row) => row.method === 'dailyGift').length;
  assert.equal(claims(), 1);
  assert.equal(success.mock.calls.length, 0);
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(claims(), 2);
  fixture.policy = { revision: 1, registrationGift: '0', dailyGift: '60', timezone: 'Asia/Shanghai' };
  await act(async () => {
    window.dispatchEvent(new Event('focus'));
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await flush();
  assert.equal(claims(), 3);
  assert.equal(success.mock.calls.length, 1);
  assert.equal(success.mock.calls[0].arguments[0], '今日赠送 60 积分已到账');
  fixture.policy = { ...fixture.policy, revision: 2, dailyGift: '90' };
  for (let index = 0; index < 3; index++) {
    await act(async () => window.dispatchEvent(new Event('focus')));
    await flush();
  }
  assert.equal(claims(), 3);
});

test('zero daily gift rechecks never overlap and an unresolved reply waits for explicit reconciliation', async () => {
  await mount('/projects');
  let rejectClaim: (error: Error) => void;
  let rechecks = 0;
  fixture.service.dailyGift = async () => {
    rechecks++;
    return new Promise((_resolve, reject) => { rejectClaim = reject; });
  };
  await act(async () => {
    window.dispatchEvent(new Event('focus'));
    document.dispatchEvent(new Event('visibilitychange'));
    window.dispatchEvent(new Event('focus'));
  });
  await flush();
  assert.equal(rechecks, 1);
  await act(async () => rejectClaim!(new ApiError(503, 'CREDIT_COMMIT_UNKNOWN', '提交结果未确认')));
  await flush();
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(rechecks, 1);
  assert.ok(button('重试核对'));
});

test('SSE invalidation refreshes personal balance, records and analysis together without issuing credit writes', async () => {
  let changed = () => {};
  let streamSignal: AbortSignal | undefined;
  await mount('/credits', (f) => {
    f.wallet = { balance: '60', rechargeBalance: '0', giftBalance: '60', legacyBalance: '0' };
    f.service.watchCredits = async (target, onChange, signal) => {
      assert.equal(target, null);
      changed = onChange;
      streamSignal = signal;
      await new Promise<void>((resolve) =>
        signal.addEventListener('abort', () => resolve(), { once: true }),
      );
    };
  });
  assert.ok(button('积分余额 60'));
  const reads = fixture.requests.filter((x) => x.method === 'myLedger').length;
  fixture.wallet = { balance: '45', rechargeBalance: '0', giftBalance: '45', legacyBalance: '0' };
  await act(async () => {
    changed();
    changed();
    changed();
    await new Promise((r) => setTimeout(r, 130));
  });
  await flush();
  assert.ok(button('积分余额 45'));
  assert.equal(fixture.requests.filter((x) => x.method === 'myLedger').length, reads + 2);
  assert.equal(
    fixture.requests.some((x) => x.method === 'adjust'),
    false,
  );
  await mount('/operations-login', (f) => {
    f.user = null;
  });
  assert.equal(streamSignal?.aborted, true);
  const nextReads = fixture.requests.length;
  await act(async () => {
    changed();
    await new Promise((r) => setTimeout(r, 130));
  });
  await flush();
  assert.equal(fixture.requests.length, nextReads);
});

test('operator wallet subscribes only while its selected account dialog is open', async () => {
  let changed = () => {};
  let stopped: AbortSignal | undefined;
  const scopes: (string | null)[] = [];
  await mount('/operations/credits', (f) => {
    f.service.watchCredits = async (target, onChange, signal) => {
      scopes.push(target);
      changed = onChange;
      stopped = signal;
      await new Promise<void>((resolve) =>
        signal.addEventListener('abort', () => resolve(), { once: true }),
      );
    };
  });
  assert.deepEqual(scopes, []);
  await act(async () => button('余额与流水 fiction@example.invalid').click());
  await flush();
  assert.deepEqual(scopes, ['fixture_a']);
  const reads = fixture.requests.filter((x) => x.method === 'userWallet').length;
  fixture.wallet = { balance: '75', rechargeBalance: '15', giftBalance: '60', legacyBalance: '0' };
  await act(async () => {
    changed();
    await new Promise((r) => setTimeout(r, 130));
  });
  await flush();
  assert.match(document.querySelector('.op-balance')?.textContent || '', /75/);
  assert.equal(fixture.requests.filter((x) => x.method === 'userWallet').length, reads + 1);
  await act(async () => button('关闭').click());
  await flush();
  assert.equal(stopped?.aborted, true);
});

test('a disconnected credit stream exposes retry state and reconnect ready rereads latest balance', async () => {
  let calls = 0;
  await mount('/credits', (f) => {
    f.service.watchCredits = async (_target, changed, signal) => {
      if (++calls === 1) throw new Error('isolated offline');
      changed();
      await new Promise<void>((resolve) =>
        signal.addEventListener('abort', () => resolve(), { once: true }),
      );
    };
  });
  assert.match(text(), /积分实时同步暂不可用，正在重连/);
  fixture.wallet = { balance: '81', rechargeBalance: '21', giftBalance: '60', legacyBalance: '0' };
  await act(async () => {
    await new Promise((r) => setTimeout(r, 1150));
  });
  await flush();
  assert.equal(calls, 2);
  assert.ok(button('积分余额 81'));
  assert.doesNotMatch(text(), /积分实时同步暂不可用/);
});

test('expired daily gifts have their own record filter and never become settled consumption', async () => {
  await mount('/credits', (f) => {
    f.service.myLedger = async (key, filters) => {
      f.requests.push({method:'expiryScope', input:filters});
      return {...creditStats, items:filters?.direction==='expired' ? [{identity:'expired-daily',at:'2026-10-18T16:00:00Z',direction:'expire',category:'gift',amount:'20',giftAmount:'20',rechargeAmount:'0',legacyAmount:'0',balance:'50',reason:'每日赠送到期回收',source:'daily-gift-expiration',originalDebit:null,actorLabel:'system'}] : [],previous:null,next:null,caption:'测试流水'};
    };
  });
  await act(async () => button('到期回收').click());
  await flush();
  assert.equal((fixture.requests.filter(r=>r.method==='expiryScope').at(-1)?.input as any).direction,'expired');
  assert.match(document.querySelector('.personal-credit-ledger')?.textContent||'',/每日赠送到期回收/);
  assert.match(document.querySelector('.credit-record-amount')?.textContent||'',/−20/);
  assert.match(document.querySelector('.personal-credit-analysis')?.textContent||'',/暂无已结算消耗/);
  await act(async () => button('到期回收').dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true})));
  await flush();
  assert.equal(document.activeElement,button('退款扣回'));
  await act(async () => button('退款扣回').dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true})));
  await flush();
  assert.equal(document.activeElement,button('已获取'));
});


test('credit page size defaults to ten, changes to fifty and resets paging without losing filters', async () => {
  const scopes: any[] = [];
  await mount('/credits', f => {
    f.service.myLedger = async (key, filters, pageSize) => {
      scopes.push({ key, filters, pageSize });
      return { ...creditStats, items: [], previous: null, next: '2', caption: '积分分页夹具' };
    };
  });
  assert.equal(button('积分每页条数').textContent, '10 条 / 页');
  assert.ok(scopes.some(scope => scope.pageSize === '10'));
  await act(async () => button('下一页').click());
  await flush();
  assert.equal(scopes.at(-1).key, '2');
  await act(async () => button('积分每页条数').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })));
  await flush();
  const sizes = [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')];
  assert.deepEqual(sizes.map(item => item.textContent), ['10 条 / 页', '20 条 / 页', '50 条 / 页']);
  await act(async () => sizes[2].click());
  await flush();
  assert.equal(scopes.at(-1).pageSize, '50');
  assert.equal(scopes.at(-1).key, '');
  assert.equal(scopes.at(-1).filters.direction, 'acquired');
});
test('program generation reasons localize without replacing operator-entered reasons or showing identifiers', async () => {
  const entry = { at: '2026-10-19T02:00:00Z', amount: '12', balance: '60', category: 'gift' as const, giftAmount: '12', rechargeAmount: '0', legacyAmount: '0', actorLabel: 'internal-user', originalDebit: null };
  await mount('/credits', f => {
    f.service.myLedger = async () => ({ ...creditStats, items: [
      { ...entry, identity: 'internal-deduction', source: 'project_private', reason: 'generation', direction: 'deduct' },
      { ...entry, identity: 'internal-refund', source: 'project_private', reason: 'generation-incomplete', direction: 'refund' },
      { ...entry, identity: 'operator-raw', source: 'operations-adjustment', reason: 'generation', direction: 'add' },
    ], previous: null, next: null, caption: '原因夹具' });
  });
  assert.deepEqual([...document.querySelectorAll('.personal-credit-reason strong')].map(item => item.textContent), ['创作生成', '生成未完成返还', 'generation']);
  assert.doesNotMatch(document.querySelector('.personal-credit-ledger')!.textContent!, /internal-|project_private/);
});


test('refund reclaim is a separate keyboard reachable negative record without consuming credits', async () => {
  const scopes: string[] = [];
  await mount('/credits', f => {
    f.service.myLedger = async (_key, filters) => {
      scopes.push(filters?.direction || 'all');
      return { ...creditStats, items: filters?.direction === 'reclaimed' ? [{
        identity: 'refund-reclaim', at: '2026-10-20T04:00:00Z', direction: 'reclaim', category: 'recharge',
        amount: '500', rechargeAmount: '500', giftAmount: '0', legacyAmount: '0', balance: '600',
        reason: '充值退款扣回', source: 'payment-order:PO20261020120000ABCDEFGHIJ', actorLabel: 'operator',
        originalDebit: null, originalAddition: 'original-add',
      }] : [], previous: null, next: null, caption: '扣回夹具' };
    };
  });
  await act(async () => button('已获取').dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true })));
  await flush();
  assert.equal(document.activeElement, button('退款扣回'));
  assert.equal(button('退款扣回').getAttribute('aria-selected'), 'true');
  assert.equal(scopes.at(-1), 'reclaimed');
  const ledger = document.querySelector('.personal-credit-ledger')!;
  assert.match(ledger.textContent || '', /充值退款扣回/);
  assert.match(ledger.textContent || '', /−500/);
  assert.doesNotMatch(ledger.textContent || '', /创作生成|original-add|payment-order:/);
  assert.equal(document.querySelector('.credit-record-amount')?.getAttribute('data-direction'), 'reclaim');
  assert.match(document.querySelector('.personal-credit-analysis')?.textContent || '', /暂无已结算消耗/);
  await act(async () => button('退款扣回').dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true })));
  await flush();
  assert.equal(document.activeElement, button('已获取'));
});
