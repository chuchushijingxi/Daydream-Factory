import { after, test } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import { createAdministrationFixture } from './administration-fixture';
import type { SpaceRole, SpaceSummary, TeamView } from '../src/api/spaces-types';

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
const ledgerCalls: { member: boolean; page: number; direction: string; range: string }[] = [];
let overviewCalls = 0,
  auditCalls = 0;
const flush = async () =>
  act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25));
  });
const visible = <T extends Element>(selector: string) =>
  [...document.querySelectorAll<T>(selector)].filter((node) => !node.closest('[hidden]'));
function button(name: string) {
  const result = visible<HTMLButtonElement>('button').find(
    (node) => node.textContent?.trim() === name || node.getAttribute('aria-label') === name,
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
  ledgerCalls.length = 0;
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
  teamClient.requests = async () => [];
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
  teamCreditsApi.ledger = async (member, page = 1, direction = 'all', range = 'all') => {
    ledgerCalls.push({ member, page, direction, range });
    return {
      items: [
        {
          id: direction + '-' + page,
          kind: direction === 'consumed' ? 'deduct' : 'add',
          amount: '50',
          balance: '500',
          reason: direction + '-' + page,
          createdAt: at,
        },
      ],
      total: 21,
      page,
      pageSize: 20,
    };
  };
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
                detail: async (id) => (id === team.id ? summary() : personal),
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

test('five work areas put team facts first and load each area only when opened', async () => {
  await mount();
  assert.deepEqual(
    [...document.querySelectorAll('#team-panel')].map((node) =>
      node.getAttribute('aria-labelledby'),
    ),
    ['team-tab-overview'],
  );
  assert.deepEqual(
    [...document.querySelectorAll('.team-workflow-navigation > [role=tablist] > button')].map(
      (node) => node.textContent,
    ),
    ['概览', '成员', '积分', '动态', '设置'],
  );
  assert.equal(document.querySelector('.team-hero h1')!.textContent, team.name);
  assert.equal(visible('.team-tiles > .team-tile').length, 2);
  // 主数字是管理者可花的未分配（与右上角一致），总余额只作为构成条的合计。
  assert.equal(visible('.team-balance-value')[0].textContent, '460');
  assert.match(visible('.team-balance-legend')[0].textContent!, /团队总积分500/);
  assert.deepEqual(
    visible('.team-balance-legend > div').map((item) => item.textContent),
    ['团队总积分500', '未分配460', '已分配给成员40'],
  );
  assert.equal(overviewCalls, 1);
  assert.equal(auditCalls, 0);
  assert.equal(ledgerCalls.length, 0);
  assert.match(visible('.team-recent')[0].textContent!, /开通团队/);
  assert.equal(document.body.textContent!.includes('合同'), false);
  assert.equal(document.querySelector('.team-workspace-page a[href="/credits"]'), null);
  await click('积分');
  assert.equal(visible('.team-tiles > .team-tile').length, 2);
  assert.match(visible('.team-tiles')[0].textContent!, /下次发放\+400/);
  assert.deepEqual(
    ledgerCalls.map(({ member, direction }) => ({ member, direction })),
    [{ member: false, direction: 'all' }],
  );
  await click('下一页');
  assert.deepEqual(ledgerCalls.at(-1), {
    member: false,
    page: 2,
    direction: 'all',
    range: 'all',
  });
  assert.equal(visible('.team-credit-records tbody')[0].textContent!.includes('all-2'), true);
  await click('动态');
  assert.equal(auditCalls, 1);
  await click('概览');
  assert.equal(overviewCalls, 1);
});

test('plan details keep a real nested URL inside settings while unknown and CAS settings retain their controller', async () => {
  for (const error of [
    new ApiError(0, 'SPACE_COMMIT_UNKNOWN', '写入结果未确认'),
    new ApiError(409, 'CONFLICT', '版本已变化'),
  ]) {
    await mount('owner', '/team/team', true);
    await click('设置');
    const form = document.querySelector<HTMLFormElement>('form.team-settings-card')!;
    const name = form.querySelector<HTMLInputElement>('.team-basic-settings input')!;
    await fill(name, '保留的团队草稿');
    let writes = 0;
    teamClient.settings = async () => {
      writes++;
      throw error;
    };
    await act(async () =>
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    );
    await flush();
    assert.equal(name.disabled, true);
    await click('概览');
    await click('详情');
    assert.equal(window.location.hash, '#/team/team/contract');
    assert.equal(
      document.querySelector('#team-tab-settings')!.getAttribute('aria-selected'),
      'true',
    );
    assert.equal(visible('.team-contract-panel .team-plan-stat').length, 3);
    assert.equal(document.querySelector('form.team-settings-card'), form);
    await click('成员');
    assert.equal(window.location.hash, '#/team/team');
    await click('设置');
    assert.equal(document.querySelector('form.team-settings-card'), form);
    assert.equal(name.value, '保留的团队草稿');
    assert.equal(button('保存设置').disabled, true);
    assert.equal(writes, 1);
    await click('读取最新状态');
    assert.equal(name.disabled, false);
    assert.equal(writes, 1);
  }
});

test('header invitation opens the members controller dialog before that area is visited; unknown invitation survives closing and area changes', async () => {
  await mount('owner', '/team/team', true);
  let writes = 0;
  teamClient.inviteEmail = async () => {
    writes++;
    throw new ApiError(0, 'SPACE_COMMIT_UNKNOWN', '邀请结果未确认');
  };
  await click('邀请成员');
  const dialog = document.querySelector<HTMLElement>('[role=dialog]')!;
  assert.ok(dialog, 'Dialog portal must open even while the members area is hidden');
  const email = dialog.querySelector<HTMLInputElement>('input[type=email]')!;
  await fill(email, 'invitee@example.invalid');
  await click('发送邀请');
  assert.equal(writes, 1);
  await click('关闭');
  assert.equal(document.querySelector('[role=dialog]'), null);
  await click('动态');
  assert.equal(auditCalls, 1);
  await click('设置');
  await click('成员');
  await click('邀请记录');
  await click('邀请成员');
  const reopened = document.querySelector<HTMLElement>('[role=dialog]')!;
  assert.equal(
    reopened.querySelector<HTMLInputElement>('input[type=email]')!.value,
    'invitee@example.invalid',
  );
  assert.equal(reopened.querySelector<HTMLInputElement>('input[type=email]')!.disabled, true);
  assert.equal(button('发送邀请').disabled, true);
  assert.equal(writes, 1);
  await click('读取最新状态');
  assert.equal(reopened.querySelector<HTMLInputElement>('input[type=email]')!.disabled, false);
  assert.equal(writes, 1);
});

test('legacy invitation query lands in member invite records; direct plan and ownership routes preserve their narrower roles', async () => {
  await mount('owner', '/team/team?tab=invites');
  assert.equal(document.querySelector('#team-tab-members')!.getAttribute('aria-selected'), 'true');
  assert.equal(
    document.querySelector('#team-people-invites')!.getAttribute('aria-selected'),
    'true',
  );
  assert.equal(visible('.team-invitation-records').length, 1);
  await mount('member', '/team/team/contract');
  assert.equal(document.querySelector('.team-contract-panel'), null);
  assert.equal(document.querySelector('[role=tablist]'), null);
  assert.equal(overviewCalls, 0);
  assert.equal(auditCalls, 0);
  assert.equal(ledgerCalls.length, 0);
  await click('返回成员列表');
  assert.equal(visible('.team-own-balance').length, 1);
  await mount('member', '/team/team/ownership/latest');
  assert.match(document.body.textContent!, /确认接任团队所有者/);
  assert.equal(document.querySelector('.team-workspace-page'), null);
});

test('primary navigation keeps keyboard reveal local and real role changes discard previous drafts', async () => {
  await mount();
  await click('设置');
  const original = document.querySelector<HTMLInputElement>('.team-basic-settings input')!;
  await fill(original, '旧角色草稿');
  role = 'admin';
  await act(async () => currentSpace.refresh());
  await flush();
  const current = document.querySelector<HTMLInputElement>('.team-basic-settings input')!;
  assert.notEqual(current, original);
  assert.equal(current.value, team.name);
  const navigation = document.querySelector<HTMLElement>(
    '.team-workflow-navigation [role=tablist]',
  )!;
  Object.defineProperty(navigation, 'clientWidth', { value: 200, configurable: true });
  navigation.getBoundingClientRect = () => ({ left: 0, right: 200, width: 200 }) as DOMRect;
  const tabs = [...navigation.querySelectorAll<HTMLElement>('[role=tab]')];
  tabs.forEach((tab, index) => {
    tab.getBoundingClientRect = () =>
      ({
        left: index * 110 - navigation.scrollLeft,
        right: index * 110 + 100 - navigation.scrollLeft,
      }) as DOMRect;
  });
  await act(async () => {
    tabs[0].focus({ preventScroll: true });
    tabs[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true }));
  });
  await flush();
  assert.equal(pathname, '/team/team');
  assert.equal(document.activeElement, tabs.at(-1));
  assert.ok(navigation.scrollLeft > 0);
});
