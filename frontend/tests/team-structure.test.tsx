import { after, test } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import { createAdministrationFixture } from './administration-fixture';
import type { JoinRequest, SpaceRole, SpaceSummary, TeamView } from '../src/api/spaces-types';

const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/#/team/team',
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
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
globalThis.getComputedStyle = dom.window.getComputedStyle;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
Object.defineProperty(globalThis, 'BroadcastChannel', { value: undefined, configurable: true });

const { createRoot } = await import('react-dom/client');
const { HashRouter, MemoryRouter, useLocation, useNavigate } = await import('react-router-dom');
const { LanguageProvider } = await import('../src/contexts/LanguageContext');
const { SessionOptionalContext, SessionProvider } = await import('../src/state/SessionContext');
const { SpaceProvider, useSpace } = await import('../src/state/SpaceContext');
const { WorkspaceProvider } = await import('../src/state/WorkspaceContext');
const { MainWorkspace } = await import('../src/MainWorkspace');
const { Application } = await import('../src/Application');
const { spacesApi } = await import('../src/api/spaces');
const { teamClient } = await import('../src/api/team-client');
const { teamCreditsApi } = await import('../src/api/team-credits');
const { notificationsApi } = await import('../src/api/notifications-client');
const { sessionBoundary } = await import('../src/api/session-boundary');
const { spaceStorageKey } = await import('../src/api/space-selection');
const { ApiError } = await import('../src/api/http-error');
const { notificationTarget } = await import('../src/api/notifications-client');
const i18n = (await import('../src/i18n')).default;

const at = '2026-10-20T00:00:00Z';
const team: TeamView = {
  id: 'team',
  name: '真实团队',
  ownerId: 'owner',
  ownerName: '原所有者',
  memberCount: 3,
  memberLimit: 6,
  expiresAt: '2099-01-01T00:00:00Z',
  disabled: false,
  grantAmount: 400,
  policy: 'read_all',
  openedAt: at,
  nextGrantAt: '2098-11-19T00:00:00Z',
  revision: 1,
  status: 'active',
  balance: 500,
  modelDiscounts: [{ modelInstanceId: 'contract-model', percent: 80 }],
};
let root: ReturnType<typeof createRoot> | undefined;
let role: SpaceRole = 'owner';
let currentSpace: ReturnType<typeof useSpace>;
let go: ReturnType<typeof useNavigate>;
let pathname = '';
let overviewCalls = 0,
  auditCalls = 0;
let pendingRequests: JoinRequest[] = [],
  teamChanged: (() => void) | null = null;
const flush = async () =>
  act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25));
  });
const visible = <T extends Element>(selector: string) =>
  [...document.querySelectorAll<T>(selector)].filter((node) => !node.closest('[hidden]'));
function button(name: string) {
  const result = visible<HTMLButtonElement>('button').find(
    (node) => node.textContent?.trim() === name,
  );
  assert.ok(result, 'missing visible button: ' + name);
  return result;
}
async function click(name: string) {
  await act(async () => button(name).click());
  await flush();
}
async function fill(input: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}
function Probe() {
  currentSpace = useSpace();
  go = useNavigate();
  pathname = useLocation().pathname;
  return currentSpace.ready ? (
    <WorkspaceProvider load="none">
      <MainWorkspace />
    </WorkspaceProvider>
  ) : null;
}
function RouteProbe() {
  go = useNavigate();
  pathname = useLocation().pathname;
  return null;
}
async function mount(initialRole: SpaceRole = 'owner', route = '/team/team', outer = false) {
  if (root) await act(async () => root!.unmount());
  sessionBoundary.reset();
  localStorage.clear();
  await i18n.changeLanguage('zh');
  role = initialRole;
  overviewCalls = auditCalls = 0;
  const fixture = createAdministrationFixture();
  const user = fixture.user!;
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
  const summary = (): SpaceSummary => ({
    id: team.id,
    name: team.name,
    kind: 'team',
    role,
    memberCount: 3,
    memberLimit: 6,
    expiresAt: team.expiresAt,
    status: 'active',
    readOnly: false,
  });
  localStorage.setItem(spaceStorageKey(user.identity), 'team');
  teamClient.detail = async () => ({ ...summary(), ...(role === 'member' ? {} : { team }) });
  teamClient.overview = async () => {
    overviewCalls++;
    return {
      team,
      range: 'contract',
      start: at,
      end: at,
      allocated: 40,
      unallocated: 460,
      daily: [],
      members: [],
      recent: [
        {
          id: 999,
          action: 'team_opened',
          actorId: 'owner',
          actorKind: 'user',
          target: '',
          details: {},
          createdAt: at,
        },
      ],
    };
  };
  teamClient.audit = async () => {
    auditCalls++;
    return { items: [] };
  };
  teamClient.members = async () => [
    { userId: 'owner', name: '原所有者', role: 'owner', joinedAt: at },
    { userId: 'member', name: '成员乙', role: 'member', joinedAt: at },
  ];
  teamClient.invites = async () => [];
  pendingRequests = [];
  teamChanged = null;
  teamClient.requests = async () => pendingRequests;
  teamClient.watch = async (_id, onChange, signal) => {
    // 不模拟 ready 帧，避免干扰按次数断言的读取；实时用例显式触发。
    teamChanged = onChange;
    await new Promise<void>((resolve) =>
      signal.addEventListener('abort', () => resolve(), { once: true }),
    );
  };
  teamClient.ownBalance = async () => ({ spaceId: 'team', role: 'member', balance: 40 });
  teamCreditsApi.balance = async () =>
    role === 'member'
      ? { spaceId: 'team', role, balance: '40' }
      : {
          spaceId: 'team',
          role,
          balance: '500',
          totalBalance: '500',
          allocated: '40',
          unallocated: '460',
        };
  teamCreditsApi.ledger = async () => ({ items: [], total: 0, page: 1, pageSize: 20 });
  notificationsApi.unread = async () => 0;
  notificationsApi.watch = async (_onChange, signal) =>
    new Promise<void>((resolve) =>
      signal.addEventListener('abort', () => resolve(), { once: true }),
    );
  const session = {
    state: 'authenticated' as const,
    user,
    service: fixture.service,
    epoch: sessionBoundary.epoch,
    message: '',
    refresh: async () => {},
    authenticate: async () => true,
    updateProfile: async () => user,
    changePassword: async () => {},
    logout: async () => {},
  };
  if (outer) {
    spacesApi.list = async () => [personal, summary()];
    spacesApi.pendingInvites = async () => [];
    spacesApi.detail = async (id) => (id === team.id ? summary() : personal);
    window.location.hash = '#' + route;
    await flush();
    root = createRoot(document.getElementById('root')!);
    await act(async () =>
      root!.render(
        <LanguageProvider>
          <HashRouter>
            <SessionProvider service={fixture.service}>
              <RouteProbe />
              <Application />
            </SessionProvider>
          </HashRouter>
        </LanguageProvider>,
      ),
    );
    await flush();
    await flush();
    return;
  }
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <MemoryRouter initialEntries={[route]}>
          <SessionOptionalContext.Provider value={session}>
            <SpaceProvider
              service={{
                list: async () => [personal, summary()],
                pendingInvites: async () => [],
                detail: async (id) => {
                  if (id === 'outsider') throw new ApiError(404, 'NOT_FOUND', '');
                  return id === team.id ? summary() : personal;
                },
              }}
            >
              <Probe />
            </SpaceProvider>
          </SessionOptionalContext.Provider>
        </MemoryRouter>
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

test('actual workspace separates overview, credits and activity while plan details remain a distinct manager route', async () => {
  await mount();
  assert.equal(visible('.team-tiles > .team-tile').length, 2);
  // 主数字是管理者可花的未分配（与右上角一致），总余额只作为构成条的合计。
  assert.equal(visible('.team-balance-value')[0].textContent, '460');
  assert.match(visible('.team-balance-legend')[0].textContent!, /团队总积分500/);
  assert.equal(visible('.team-activity-panel').length, 0);
  assert.equal(overviewCalls, 1);
  assert.equal(auditCalls, 0);
  await click('动态');
  assert.equal(visible('.team-activity-panel').length, 1);
  assert.equal(auditCalls, 1);
  await click('概览');
  await click('详情');
  assert.equal(pathname, '/team/team/contract');
  // 套餐卡片：名额、周期赠送、有效期三块数据，模型折扣按“折”展示（目录未加载时显示模型 ID）。
  assert.equal(visible('.team-contract-panel .team-plan-stat').length, 3);
  const plan = visible('.team-contract-panel')[0].textContent!;
  assert.match(plan, /3\/ 6 人/);
  assert.match(plan, /contract-model/);
  assert.match(plan, /8 折省 20%/);
  assert.equal(visible('.team-tiles').length, 0);
  await click('积分');
  assert.equal(pathname, '/team/team');
  assert.equal(visible('.team-tiles > .team-tile').length, 2);
  assert.match(visible('.team-tiles')[0].textContent!, /下次发放\+400/);
  assert.equal(overviewCalls, 1, 'credits do not rerequest the usage statistics');
});

test('direct plan URL returns through external workspace navigation to a filled overview', async () => {
  await mount('owner', '/team/team/contract');
  assert.equal(overviewCalls, 0);
  assert.equal(auditCalls, 0);
  assert.equal(visible('.team-contract-panel').length, 1);
  await act(async () => go('/team/team'));
  await flush();
  assert.equal(pathname, '/team/team');
  assert.equal(visible('.team-dashboard').length, 1);
  assert.equal(overviewCalls, 1);
  assert.equal(auditCalls, 0);
});

test('actual workspace keeps CAS/unknown settings and unsaved form through tabs and plan navigation', async () => {
  for (const error of [
    new ApiError(0, 'SPACE_COMMIT_UNKNOWN', '写入结果未确认'),
    new ApiError(409, 'CONFLICT', '版本已变化'),
  ]) {
    await mount();
    const writes: unknown[] = [];
    teamClient.settings = async (_id, input) => {
      writes.push(input);
      throw error;
    };
    await click('设置');
    const form = document.querySelector<HTMLFormElement>('form.team-settings-card')!;
    const name = form.querySelector<HTMLInputElement>('.team-basic-settings input')!;
    await fill(name, '尚未确认的草稿');
    await act(async () =>
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    );
    await flush();
    assert.equal(writes.length, 1);
    assert.equal(name.disabled, true);
    await click('概览');
    assert.equal(visible('.team-basic-settings').length, 0);
    await click('详情');
    assert.equal(pathname, '/team/team/contract');
    await act(async () => go(-1));
    await flush();
    assert.equal(pathname, '/team/team');
    await click('设置');
    assert.equal(
      document.querySelector('form.team-settings-card'),
      form,
      'real nested workspace route must retain the controller',
    );
    assert.equal(name.value, '尚未确认的草稿');
    assert.equal(name.disabled, true);
    assert.equal(button('保存设置').disabled, true);
    assert.equal(writes.length, 1, 'tab/route transitions must not resubmit');
    await click('读取最新状态');
    assert.equal(name.disabled, false);
    assert.equal(
      name.value,
      '尚未确认的草稿',
      'readback leaves the draft for an explicit decision',
    );
    assert.equal(writes.length, 1);
  }
});

test('a member direct contract URL cannot mount manager metadata, overview or audit; ownership URL still selects its confirmation route', async () => {
  await mount('member', '/team/team/contract');
  assert.equal(overviewCalls, 0);
  assert.equal(auditCalls, 0);
  assert.equal(document.querySelector('.team-contract-panel'), null);
  assert.equal(document.body.textContent!.includes('contract-model'), false);
  assert.equal(document.body.textContent!.includes('每 30 天发放积分'), false);
  assert.match(document.body.textContent!, /无权限访问/);
  await click('返回成员列表');
  assert.equal(pathname, '/team/team');
  await mount('member', '/team/team/ownership/latest-transfer');
  assert.match(document.body.textContent!, /确认接任团队所有者/);
  assert.equal(document.querySelector('.team-workspace-page'), null);
  assert.equal(document.querySelector('[role=tablist]'), null);
});

test('a team URL without access shows one localized toast and returns home instead of a retry page', async () => {
  const { toast } = await import('../src/components/ui/sonner');
  const errors: unknown[][] = [];
  const originalError = toast.error;
  toast.error = ((...args: unknown[]) => {
    errors.push(args);
    return '';
  }) as unknown as typeof toast.error;
  try {
    await mount('owner', '/team/outsider');
    await flush();
    assert.equal(pathname, '/projects');
    assert.equal(document.body.textContent!.includes('NOT_FOUND'), false);
    assert.equal(document.body.textContent!.includes('重新读取'), false);
    assert.ok(errors.length >= 1);
    for (const [message, options] of errors) {
      assert.equal(message, '无权限访问该团队，已返回首页。');
      assert.deepEqual(options, { id: 'team-access-denied' });
    }
  } finally {
    toast.error = originalError;
  }
});

test('real role and session scopes discard visited settings controller rather than preserving another identity draft', async () => {
  await mount();
  await click('设置');
  const original = document.querySelector<HTMLInputElement>('.team-basic-settings input')!;
  await fill(original, '旧角色草稿');
  role = 'admin';
  await act(async () => currentSpace.refresh());
  await flush();
  const changedRole = document.querySelector<HTMLInputElement>('.team-basic-settings input')!;
  assert.notEqual(changedRole, original);
  assert.equal(changedRole.value, team.name);
  await fill(changedRole, '旧会话草稿');
  await act(async () => {
    sessionBoundary.reset();
    await currentSpace.refresh();
  });
  await flush();
  const changedSession = document.querySelector<HTMLInputElement>('.team-basic-settings input')!;
  assert.notEqual(changedSession, changedRole);
  assert.equal(changedSession.value, team.name);
});

test('both actual workspace navigation levels reveal keyboard selection horizontally without moving the page', async () => {
  await mount();
  await click('成员');
  const primary = document.querySelector<HTMLElement>('.team-workflow-navigation [role=tablist]')!;
  const people = document.querySelector<HTMLElement>('.team-people > [role=tablist]')!;
  assert.equal(visible('.team-workspace-page [role=tablist]').length, 2);
  let pageScrolls = 0;
  window.scrollTo = () => {
    pageScrolls++;
  };
  // 先验证成员分区，再验证主导航；主导航按 End 会切到“设置”并隐藏成员分区。
  for (const [index, navigation] of [people, primary].entries()) {
    if (index === 1) await act(async () => i18n.changeLanguage('en'));
    Object.defineProperty(navigation, 'clientWidth', { value: 200, configurable: true });
    navigation.getBoundingClientRect = () => ({ left: 0, right: 200, width: 200 }) as DOMRect;
    const tabs = [...navigation.querySelectorAll<HTMLElement>('[role=tab]')];
    tabs.forEach((tab, index) => {
      tab.getBoundingClientRect = () =>
        ({
          left: index * 120 - navigation.scrollLeft,
          right: index * 120 + 100 - navigation.scrollLeft,
          width: 100,
        }) as DOMRect;
    });
    await act(async () => {
      tabs[0].focus({ preventScroll: true });
      tabs[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true }));
    });
    await flush();
    assert.equal(document.activeElement, tabs.at(-1));
    assert.ok(navigation.scrollLeft > 0);
    await act(async () => {
      tabs.at(-1)!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }));
    });
    await flush();
    assert.equal(navigation.scrollLeft, 0);
  }
  assert.equal(pageScrolls, 0);
});

test('production Application + HashRouter retain settings controller and selection across plan navigation', async () => {
  await mount('owner', '/team/team', true);
  await click('设置');
  const form = document.querySelector<HTMLFormElement>('form.team-settings-card')!;
  const name = form.querySelector<HTMLInputElement>('.team-basic-settings input')!;
  await fill(name, '外层路由未确认草稿');
  let writes = 0;
  teamClient.settings = async () => {
    writes++;
    throw new ApiError(0, 'SPACE_COMMIT_UNKNOWN', '写入结果未确认');
  };
  await act(async () =>
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(name.disabled, true);
  await click('概览');
  await click('详情');
  assert.equal(window.location.hash, '#/team/team/contract');
  assert.equal(visible('.team-contract-panel').length, 1);
  assert.equal(document.querySelector('#team-tab-settings')!.getAttribute('aria-selected'), 'true');
  await flush();
  await flush();
  assert.equal(window.location.hash, '#/team/team/contract');
  await click('成员');
  assert.equal(window.location.hash, '#/team/team');
  assert.equal(document.querySelector('#team-tab-members')!.getAttribute('aria-selected'), 'true');
  await click('设置');
  assert.equal(document.querySelector('form.team-settings-card'), form);
  assert.equal(name.value, '外层路由未确认草稿');
  assert.equal(name.disabled, true);
  assert.equal(writes, 1);
  await click('概览');
  await click('详情');
  await act(async () => go(-1));
  await flush();
  assert.equal(window.location.hash, '#/team/team');
  assert.equal(document.querySelector('#team-tab-overview')!.getAttribute('aria-selected'), 'true');
  await click('设置');
  assert.equal(document.querySelector('form.team-settings-card'), form);
  assert.equal(button('保存设置').disabled, true);
});

test('join-request notifications open the requests view, including when the team page is already open', async () => {
  assert.deepEqual(notificationTarget({ type: 'join_request', spaceId: 'team', targetId: 'r1' }), {
    path: '/team/team?view=requests',
    switchSpace: true,
  });
  const selected = () =>
    visible('[role=tab][aria-selected="true"]').map((node) => node.textContent?.trim());
  await mount('owner', '/team/team?view=requests');
  assert.deepEqual(selected(), ['成员', '加入申请']);
  await mount('owner', '/team/team');
  assert.deepEqual(selected(), ['概览']);
  await act(async () => go('/team/team?view=requests'));
  await flush();
  assert.deepEqual(selected(), ['成员', '加入申请']);
});

test('credit detail entries open the credits tab directly, including when the team page is already open', async () => {
  const selected = () =>
    visible('[role=tab][aria-selected="true"]').map((node) => node.textContent?.trim());
  await mount('owner', '/team/team?tab=credits');
  assert.deepEqual(selected(), ['积分']);
  assert.ok(document.querySelector('.team-credit-panel'));
  // 充值记录只走账号菜单的独立入口，团队积分卡不再放跳转。
  assert.equal(document.querySelector('.team-balance-card a[href$="/payments/orders"]'), null);
  await mount('owner', '/team/team');
  assert.deepEqual(selected(), ['概览']);
  await act(async () => go('/team/team?tab=credits'));
  await flush();
  assert.deepEqual(selected(), ['积分']);
});

test('team stream signals re-read join requests in place without a browser refresh', async () => {
  await mount('owner', '/team/team?view=requests');
  assert.match(document.body.textContent!, /暂无待处理的加入申请/);
  assert.ok(teamChanged, 'team page subscribes to the team stream');
  pendingRequests = [
    {
      id: 'r1',
      spaceId: 'team',
      userId: 'applicant',
      userName: '新申请人',
      message: '想加入',
      status: 'pending',
      createdAt: at,
    },
  ];
  await act(async () => teamChanged!());
  // 失效提示按 100ms 合并后才重读。
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 150));
  });
  await flush();
  assert.match(visible('.team-request-list')[0].textContent!, /新申请人/);
  assert.ok(
    visible('[role=tab]').some((node) => node.textContent?.trim() === '加入申请 1'),
    'request count badge follows the live list',
  );
});

test('member quota meter color follows the occupied share of the limit', async () => {
  const { usageTone } = await import('../src/pages/team/team-ui');
  assert.equal(usageTone(3, 5), 'normal');
  assert.equal(usageTone(7, 10), 'warning');
  assert.equal(usageTone(4, 5), 'warning');
  assert.equal(usageTone(9, 10), 'danger');
  assert.equal(usageTone(5, 5), 'danger');
  assert.equal(usageTone(6, 5), 'danger', 'over the limit stays danger');
  assert.equal(usageTone(0, 0), 'danger', 'no capacity counts as full');
  await mount();
  const meter = visible<HTMLElement>('.team-meter')[0];
  assert.ok(meter, 'overview shows the member quota meter');
  assert.equal(meter.dataset.tone, 'normal', '3 of 6 members');
});
