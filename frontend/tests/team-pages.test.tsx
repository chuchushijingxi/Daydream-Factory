import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import type {
  SpaceRole,
  SpaceSummary,
  TeamView,
  Overview,
  TeamMember,
} from '../src/api/spaces-types';
import { createAdministrationFixture } from './administration-fixture';
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
const { MemoryRouter, Routes, Route } = await import('react-router-dom');
const { LanguageProvider } = await import('../src/contexts/LanguageContext');
const { SessionOptionalContext } = await import('../src/state/SessionContext');
const { SpaceProvider, useSpace } = await import('../src/state/SpaceContext');
const { spaceStorageKey } = await import('../src/api/space-selection');
const { sessionBoundary } = await import('../src/api/session-boundary');
const { teamClient } = await import('../src/api/team-client');
const { teamCreditsApi } = await import('../src/api/team-credits');
const { TeamSettingsPage } = await import('../src/pages/team/TeamSettingsPage');
const { MembersPanel } = await import('../src/pages/team/MembersPanel');
const { TeamResourceTransferPanel } = await import('../src/pages/team/TeamResourceTransferPanel');
const { AuditTable } = await import('../src/pages/team/OverviewPanel');
const { SettingsPanel } = await import('../src/pages/team/SettingsPanel');
const { InvitesPanel } = await import('../src/pages/team/InvitesPanel');
const { PendingInvitesPage } = await import('../src/pages/invite/PendingInvitesPage');
const { TeamsPage } = await import('../src/pages/operations/teams/TeamsPage');
const { ApiError } = await import('../src/api/http-error');
let root: ReturnType<typeof createRoot> | undefined;
const at = '2026-10-20T00:00:00Z';
const view: TeamView = {
  id: 'team',
  name: '真实团队',
  ownerId: 'owner',
  ownerName: '所有者甲',
  memberCount: 4,
  memberLimit: 6,
  expiresAt: '2027-01-01T00:00:00Z',
  disabled: false,
  grantAmount: 400,
  policy: 'read_all',
  openedAt: at,
  nextGrantAt: '2026-11-19T00:00:00Z',
  revision: 1,
  status: 'active',
  balance: 500,
  modelDiscounts: [],
};
let overviewCalls = 0;
let overviewRanges: string[] = [];
const summary = (role: SpaceRole): SpaceSummary => ({
  id: 'team',
  name: view.name,
  kind: 'team',
  role,
  memberCount: 4,
  memberLimit: 6,
  status: 'active',
  readOnly: false,
  expiresAt: view.expiresAt,
});
let members: TeamMember[];
const overview = (): Overview => ({
  team: view,
  range: 'contract',
  start: at,
  end: at,
  allocated: 40,
  unallocated: 460,
  daily: [],
  recent: [],
  members: members.map((member) => ({
    userId: member.userId,
    name: member.name,
    holderBalance: member.role === 'member' ? 17 : 0,
    credits: 2,
    images: 1,
    videos: 0,
    projects: 1,
  })),
});
const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
};
const buttons = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].filter(
    (item) =>
      !item.closest('[hidden]') &&
      (item.textContent?.trim() === name || item.getAttribute('aria-label') === name),
  );
async function click(name: string, index = 0) {
  assert.ok(buttons(name)[index], 'missing button: ' + name);
  await act(async () => buttons(name)[index].click());
  await flush();
}
async function openMenu(name: string, index = 0) {
  const trigger = buttons(name)[index];
  assert.ok(trigger, 'missing menu trigger: ' + name);
  await act(async () => {
    trigger.focus();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  });
  await flush();
  return trigger;
}
async function selectMenuItem(name: string) {
  const item = [
    ...document.querySelectorAll<HTMLElement>('[role=menuitem], [role=menuitemradio]'),
  ].find((node) => node.textContent?.trim() === name);
  assert.ok(item, 'missing menu item: ' + name);
  await act(async () => item.click());
  await flush();
}
async function fill(element: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value);
    element.dispatchEvent(new Event('input', { bubbles: true }));
  });
}
function Ready({ children }: { children: React.ReactNode }) {
  return useSpace().ready ? children : null;
}
async function mount(
  role: SpaceRole,
  direct: boolean | 'invites' | 'pending' | 'settings' = false,
) {
  if (root) await act(async () => root!.unmount());
  sessionBoundary.reset();
  localStorage.clear();
  overviewCalls = 0;
  teamCreditsApi.ledger = async () => ({ items: [], total: 0, page: 1, pageSize: 20 });
  overviewRanges = [];
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
  localStorage.setItem(spaceStorageKey(user.identity), 'team');
  members = [
    {
      userId: user.identity,
      name: '当前成员',
      role,
      email: 'private-current@example.invalid',
      joinedAt: at,
    },
    {
      userId: 'owner',
      name: '所有者甲',
      role: 'owner',
      email: 'private-owner@example.invalid',
      joinedAt: at,
    },
    {
      userId: 'alice',
      name: '成员乙',
      role: 'member',
      email: 'private-alice@example.invalid',
      joinedAt: at,
    },
    {
      userId: 'admin',
      name: '管理员丙',
      role: 'admin',
      email: 'private-admin@example.invalid',
      joinedAt: at,
    },
  ];
  if (role === 'owner') members = members.filter((member) => member.userId !== 'owner');
  teamClient.detail = async () => ({
    ...summary(role),
    ...(role === 'member' ? {} : { team: view }),
  });
  teamClient.members = async () => members;
  teamClient.overview = async (_id, range) => {
    overviewCalls++;
    overviewRanges.push(range);
    return overview();
  };
  teamClient.ownBalance = async () => ({ spaceId: 'team', role: 'member', balance: 17 });
  teamClient.audit = async () => ({ items: [] });
  if (!direct) {
    // 完整团队页的概览与成员分区会读取加入申请和邀请记录。
    teamClient.requests = async () => [];
    teamClient.invites = async () => [];
  }
  const service = {
    list: async () => [personal, summary(role)],
    pendingInvites: async () => [],
    detail: async (id: string) => (id === 'team' ? summary(role) : personal),
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
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <MemoryRouter initialEntries={['/team/team']}>
          <SessionOptionalContext.Provider value={session}>
            <SpaceProvider service={service}>
              {direct ? (
                <Ready>
                  {direct === 'invites' ? (
                    <InvitesPanel id="team" blocked={false} />
                  ) : direct === 'pending' ? (
                    <PendingInvitesPage />
                  ) : direct === 'settings' ? (
                    <SettingsPanel team={view} role={role} onSaved={() => {}} />
                  ) : (
                    <MembersPanel id="team" role={role} onChanged={() => {}} />
                  )}
                </Ready>
              ) : (
                <Routes>
                  <Route path="/team/:spaceId" element={<TeamSettingsPage />} />
                </Routes>
              )}
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
test('ordinary member requests only names, roles and own balance, never overview or audit and never renders manager metadata', async () => {
  await mount('member');
  assert.equal(overviewCalls, 0);
  const text = document.body.textContent!;
  assert.match(text, /成员乙/);
  assert.match(text, /我的可用积分/);
  assert.match(text, /17/);
  for (const hidden of [
    'private-alice@example.invalid',
    '团队总积分',
    '未分配积分',
    '加入时间',
    '分配积分',
    '团队操作记录',
  ])
    assert.equal(text.includes(hidden), false, hidden);
  assert.equal(document.querySelectorAll('[role=tab]').length, 0);
  assert.equal(buttons('退出团队').length, 1);
});
test('owner roster shows credit adjustment in the row and keeps role, ownership and removal in one compact menu', async () => {
  await mount('owner', true);
  assert.equal(buttons('分配积分').length, 0);
  assert.equal(buttons('调整积分：成员乙').length, 1);
  // 只有所有者能给管理员分配积分。
  assert.equal(buttons('调整积分：管理员丙').length, 1);
  const trigger = await openMenu('管理成员：成员乙');
  const items = [...document.querySelectorAll('[role=menuitem]')].map((item) => item.textContent);
  assert.deepEqual(items, ['设为管理员', '转让所有权', '移出成员']);
  await act(async () =>
    document.activeElement?.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    ),
  );
  await flush();
  assert.equal(document.querySelector('[role=menu]'), null);
  assert.equal(document.activeElement, trigger);
  await openMenu('管理成员：管理员丙');
  assert.deepEqual(
    [...document.querySelectorAll('[role=menuitem]')].map((item) => item.textContent),
    ['设为成员', '转让所有权'],
  );
  const roles = [...document.querySelectorAll('.team-role-badge')].map((badge) =>
    badge.getAttribute('data-role'),
  );
  assert.deepEqual(roles, ['owner', 'member', 'admin']);
});
test('operations team list displays the authoritative total balance alongside contract facts', async () => {
  if (root) await act(async () => root!.unmount());
  const original = teamClient.teams;
  teamClient.teams = async () => ({
    items: [{ ...view, balance: 1234567 }],
    total: 1,
    page: 1,
    pageSize: 20,
  });
  root = createRoot(document.getElementById('root')!);
  try {
    await act(async () =>
      root!.render(
        <LanguageProvider>
          <MemoryRouter initialEntries={['/operations/teams']}>
            <TeamsPage />
          </MemoryRouter>
        </LanguageProvider>,
      ),
    );
    await flush();
    const headers = [...document.querySelectorAll('thead th')].map((cell) => cell.textContent);
    const balanceColumn = headers.indexOf('团队总余额');
    assert.ok(balanceColumn >= 0);
    const cells = document.querySelectorAll('tbody tr td');
    assert.equal(cells[balanceColumn]?.textContent, new Intl.NumberFormat().format(1234567));
    assert.match(document.body.textContent || '', /所有者甲/);
    // 正常团队的“人数 / 状态”列也要给出人数与状态，不能因为没有提醒而留空。
    const stateCell = cells[headers.indexOf('人数 / 状态')];
    assert.match(stateCell?.textContent || '', /\d+ \/ \d+ 人/);
    assert.ok(stateCell?.querySelector('.op-status-badge'), 'status badge');
  } finally {
    await act(async () => root!.unmount());
    root = undefined;
    teamClient.teams = original;
  }
});
test('admin roster only offers ordinary-member credit and removal actions and no admin or ownership actions', async () => {
  await mount('admin', true);
  assert.equal(buttons('设为管理员').length, 0);
  assert.equal(buttons('设为成员').length, 0);
  assert.equal(buttons('转让所有权').length, 0);
  assert.equal(buttons('管理成员：管理员丙').length, 0);
  assert.equal(buttons('调整积分：成员乙').length, 1);
  await openMenu('管理成员：成员乙');
  assert.deepEqual(
    [...document.querySelectorAll('[role=menuitem]')].map((item) => item.textContent),
    ['移出成员'],
  );
  assert.match(document.body.textContent!, /加入时间/);
  assert.match(document.body.textContent!, /2026-10-20/);
});
test('allocation unknown write retains draft and command, makes no automatic retry, and only explicit same-command retry can finish', async () => {
  await mount('owner', true);
  const commands: unknown[] = [];
  teamClient.allocate = async (_id, _member, _kind, input) => {
    commands.push({ ...input });
    if (commands.length === 1) throw new ApiError(0, 'NETWORK', '提交未确认');
    return {
      id: 'event',
      operationId: 'op',
      accountId: 'team',
      holderId: 'alice',
      kind: 'allocate',
      amount: 12,
      balance: 29,
      actorId: 'owner',
      createdAt: at,
    };
  };
  await click('调整积分：成员乙');
  const field = document.querySelector<HTMLInputElement>('[role=dialog] input[type=number]')!;
  await fill(field, '12');
  await click('确认调整');
  await flush();
  assert.equal(commands.length, 1);
  assert.equal(field.value, '12');
  assert.equal(buttons('确认调整')[0].disabled, true);
  const dialog = document.querySelector('[role=dialog], [role=alertdialog]')!;
  const reread = [...dialog.querySelectorAll<HTMLButtonElement>('button')].find(
    (button) => button.textContent?.trim() === '读取最新状态',
  )!;
  await act(async () => reread.click());
  await flush();
  assert.equal(
    document.querySelector<HTMLInputElement>('[role=dialog] input[type=number]')!.value,
    '12',
  );
  await click('确认调整');
  assert.equal(commands.length, 2);
  assert.deepEqual(commands[0], commands[1]);
  assert.equal(document.querySelector('[role=dialog], [role=alertdialog]'), null);
});
test('actual audit event codes render translated actions and reuse known names without requesting identities', async () => {
  await mount('owner', true);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <AuditTable
          names={{ alice: '成员乙' }}
          items={[
            'email_invited',
            'credits_allocate',
            'visibility_changed',
            'invite_link_created',
            'model_discounts_changed',
            'owner_changed_by_operator',
          ].map((action, index) => ({
            id: index + 1,
            spaceId: 'team',
            actorId: index ? 'unknown-actor' : 'alice',
            actorKind: 'user',
            action,
            target: 'alice',
            detail: null,
            createdAt: at,
          }))}
        />
      </LanguageProvider>,
    ),
  );
  const text = document.body.textContent!;
  for (const action of [
    '邮箱邀请',
    '分配积分',
    '变更可见性',
    '生成邀请链接',
    '修改模型折扣',
    '运营更换所有者',
  ])
    assert.ok(text.includes(action), action);
  assert.ok(text.includes('成员乙'));
  assert.ok(text.includes('unknown-actor'));
  assert.equal(text.includes('email_invited'), false);
});

test('settings conflict readback preserves the user draft and submits only against the freshly read revision', async () => {
  if (root) await act(async () => root!.unmount());
  sessionBoundary.reset();
  let saves = 0,
    parentRefresh = 0;
  const commands: unknown[] = [];
  teamClient.settings = async (_id, input) => {
    commands.push(input);
    saves++;
    if (saves === 1) throw new ApiError(409, 'SPACE_CONFLICT', '版本冲突');
    return { ...view, ...input, revision: input.revision + 1 };
  };
  teamClient.detail = async () => ({
    ...summary('owner'),
    team: { ...view, name: '他人改名', revision: 8 },
  });
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <SettingsPanel
          team={view}
          onSaved={() => {
            parentRefresh++;
          }}
        />
      </LanguageProvider>,
    ),
  );
  const field = [...document.querySelectorAll('label')]
    .find((label) => label.textContent?.includes('团队名称'))!
    .querySelector('input')!;
  await fill(field, '我的保留草稿');
  await click('保存设置');
  assert.equal(field.value, '我的保留草稿');
  assert.equal(buttons('保存设置')[0].disabled, true);
  await click('读取最新状态');
  assert.equal(field.value, '我的保留草稿');
  assert.equal(parentRefresh, 0);
  assert.ok(document.body.textContent!.includes('他人改名'));
  await click('保存设置');
  assert.deepEqual(commands[1], { revision: 8, name: '我的保留草稿', policy: view.policy });
  assert.equal(parentRefresh, 1);
});

test('unknown invitation creation remains blocked while either readback is pending or failed', async () => {
  const original = {
    invites: teamClient.invites,
    requests: teamClient.requests,
    createLink: teamClient.createLink,
  };
  let writes = 0;
  let recheck: Promise<never[]> | undefined;
  let reject!: (error: Error) => void;
  teamClient.invites = async () => [];
  teamClient.requests = async () => recheck ?? [];
  teamClient.createLink = async () => {
    writes++;
    throw new ApiError(0, 'NETWORK', '提交未确认');
  };
  try {
    await mount('owner', 'invites');
    assert.equal(buttons('读取最新状态').length, 0);
    await click('邀请成员');
    await openMenu('邀请方式');
    await selectMenuItem('邀请链接');
    await click('生成邀请链接');
    assert.equal(writes, 1);
    assert.equal(buttons('生成邀请链接')[0].disabled, true);
    recheck = new Promise<never[]>((_resolve, fail) => {
      reject = fail;
    });
    await click('读取最新状态');
    assert.equal(buttons('生成邀请链接')[0].disabled, true);
    assert.equal(buttons('读取最新状态')[0].disabled, true);
    await act(async () => reject(new ApiError(0, 'NETWORK', '回读失败')));
    await flush();
    assert.match(document.body.textContent || '', /回读失败/);
    assert.equal(buttons('生成邀请链接')[0].disabled, true);
    assert.equal(buttons('读取最新状态')[0].disabled, false);
    assert.equal(writes, 1);
    recheck = undefined;
    await click('读取最新状态');
    assert.equal(buttons('生成邀请链接')[0].disabled, false);
    assert.equal(buttons('读取最新状态').length, 0);
    assert.equal(writes, 1);
  } finally {
    if (root) await act(async () => root!.unmount());
    root = undefined;
    Object.assign(teamClient, original);
  }
});

test('unknown invitation acceptance waits for a successful pending-list readback before unlocking', async () => {
  const original = { pending: teamClient.pending, acceptInvite: teamClient.acceptInvite };
  const invite = {
    id: 'invite',
    spaceId: 'team',
    teamName: '真实团队',
    expiresAt: '2027-01-01T00:00:00Z',
  };
  let rows = [invite];
  let recheck: Promise<typeof rows> | undefined;
  let reject!: (error: Error) => void;
  let writes = 0;
  teamClient.pending = async () => recheck ?? rows;
  teamClient.acceptInvite = async () => {
    writes++;
    throw new ApiError(0, 'NETWORK', '提交未确认');
  };
  try {
    await mount('member', 'pending');
    assert.equal(buttons('读取最新状态').length, 0);
    await click('接受并加入');
    assert.equal(buttons('接受并加入')[0].disabled, true);
    recheck = new Promise<typeof rows>((_resolve, fail) => {
      reject = fail;
    });
    await click('读取最新状态');
    assert.equal(buttons('接受并加入')[0].disabled, true);
    assert.equal(buttons('拒绝')[0].disabled, true);
    assert.equal(buttons('读取最新状态')[0].disabled, true);
    await act(async () => reject(new ApiError(0, 'NETWORK', '回读失败')));
    await flush();
    assert.equal(buttons('接受并加入')[0].disabled, true);
    assert.equal(buttons('拒绝')[0].disabled, true);
    assert.match(document.body.textContent || '', /回读失败/);
    assert.equal(writes, 1);
    rows = [];
    recheck = undefined;
    await click('读取最新状态');
    assert.equal(buttons('接受并加入').length, 0);
    assert.equal(buttons('读取最新状态').length, 0);
    assert.equal(writes, 1);
  } finally {
    if (root) await act(async () => root!.unmount());
    root = undefined;
    Object.assign(teamClient, original);
  }
});

test('overview and member statistics default to the last 30 days and offer since-opening in custom menus', async () => {
  await mount('owner');
  assert.deepEqual(overviewRanges, ['30d']);
  assert.equal(document.querySelectorAll('select').length, 0);
  assert.equal(document.body.textContent!.includes('合同'), false);
  await openMenu('统计范围');
  await selectMenuItem('近 7 天');
  assert.equal(overviewRanges.at(-1), '7d');
  await click('成员');
  assert.equal(overviewRanges.at(-1), '30d');
  await openMenu('成员统计范围');
  await selectMenuItem('开通以来');
  assert.equal(overviewRanges.at(-1), 'contract');
});

test('owner appointment and ownership proposal are in the member rows, with the correct target and confirmation', async () => {
  await mount('owner');
  const original = { role: teamClient.role, beginOwnership: teamClient.beginOwnership };
  const actions: unknown[] = [];
  teamClient.role = async (id, user, role) => {
    actions.push({ id, user, role });
    return {};
  };
  teamClient.beginOwnership = async (id, userId) => {
    actions.push({ id, userId });
    return {
      id: 'proposal',
      spaceId: id,
      fromOwnerId: view.ownerId,
      toUserId: userId,
      status: 'pending',
      createdAt: at,
    };
  };
  try {
    await click('成员');
    await openMenu('管理成员：管理员丙');
    await selectMenuItem('设为成员');
    assert.match(
      document.querySelector('[role=dialog], [role=alertdialog]')!.textContent!,
      /管理员丙/,
    );
    await click('确认');
    assert.deepEqual(actions[0], { id: 'team', user: 'admin', role: 'member' });
    await openMenu('管理成员：成员乙');
    await selectMenuItem('转让所有权');
    assert.match(
      document.querySelector('[role=dialog], [role=alertdialog]')!.textContent!,
      /新的提议会替换旧提议/,
    );
    assert.match(
      document.querySelector('[role=dialog], [role=alertdialog]')!.textContent!,
      /成员乙/,
    );
    await click('确认');
    assert.deepEqual(actions[1], { id: 'team', userId: 'alice' });
    assert.equal(document.querySelector('[role=dialog], [role=alertdialog]'), null);
  } finally {
    Object.assign(teamClient, original);
  }
});

test('admin settings retain visibility editing while the roster omits owner-only role or ownership actions', async () => {
  await mount('admin');
  await click('设置');
  const policies = [...document.querySelectorAll<HTMLInputElement>('.team-policy-option input')];
  assert.equal(policies.length, 3);
  assert.equal(policies.find((input) => input.checked)?.value, 'read_all');
  assert.equal(policies.some((input) => input.disabled), false);
  await click('成员');
  assert.equal(buttons('管理成员：管理员丙').length, 0);
  await openMenu('管理成员：成员乙');
  assert.deepEqual(
    [...document.querySelectorAll('[role=menuitem]')].map((item) => item.textContent),
    ['移出成员'],
  );
});

test('role mutation keeps unknown state after dismissing confirmation or switching tabs and only successful explicit readback unlocks actions', async () => {
  await mount('owner');
  await click('成员');
  const original = { role: teamClient.role, members: teamClient.members };
  let writes = 0;
  let recheck: Promise<TeamMember[]> | undefined;
  let reject!: (error: Error) => void;
  teamClient.role = async () => {
    writes++;
    throw new ApiError(0, 'NETWORK', '提交未确认');
  };
  teamClient.members = async () => recheck ?? members;
  try {
    await openMenu('管理成员：成员乙');
    await selectMenuItem('设为管理员');
    await click('确认');
    assert.equal(writes, 1);
    assert.equal(buttons('确认')[0].disabled, true);
    await click('取消');
    assert.equal(document.querySelector('[role=dialog], [role=alertdialog]'), null);
    assert.equal(buttons('管理成员：成员乙')[0].disabled, true);
    await click('设置');
    await click('成员');
    assert.equal(buttons('管理成员：成员乙')[0].disabled, true);
    assert.equal(writes, 1);
    recheck = new Promise((_resolve, fail) => {
      reject = fail;
    });
    await click('读取最新状态');
    assert.equal(buttons('管理成员：成员乙')[0].disabled, true);
    await act(async () => reject(new ApiError(0, 'NETWORK', '回读失败')));
    await flush();
    assert.equal(buttons('管理成员：成员乙')[0].disabled, true);
    assert.equal(writes, 1);
    recheck = undefined;
    await click('读取最新状态');
    assert.equal(buttons('管理成员：成员乙')[0].disabled, false);
    assert.equal(writes, 1);
  } finally {
    Object.assign(teamClient, original);
  }
});

test('settings resource transfer always moves all resources and preserves independent receipts', async () => {
  await mount('admin', true);
  assert.equal(document.querySelector('.team-resource-transfer'), null);
  const original = teamClient.transfer;
  const commands: unknown[] = [];
  teamClient.transfer = async (_id, input) => {
    commands.push(input);
    return [
      { kind: 'project', id: 'project-a', transferred: true },
      { kind: 'upload', id: 'upload-b', transferred: false, errorCode: 'NOT_FOUND' },
    ];
  };
  try {
    await act(async () => root!.render(
      <LanguageProvider><TeamResourceTransferPanel id="team" onSaved={() => {}} /></LanguageProvider>,
    ));
    await flush();
    const transfer = document.querySelector('.team-resource-transfer')!;
    await fill(transfer.querySelector<HTMLInputElement>('input')!, 'former-user');
    assert.equal(transfer.querySelector('select'), null);
    assert.equal(transfer.querySelector('input[type=checkbox]'), null);
    assert.equal(transfer.querySelector('[aria-label="资源 ID"]'), null);
    await openMenu('请选择接收成员');
    await selectMenuItem('成员乙');
    await click('核对并移交');
    assert.match(document.querySelector('[role=alertdialog]')!.textContent!, /可能部分成功/);
    await click('确认');
    assert.deepEqual(commands, [{ fromUserId: 'former-user', toUserId: 'alice', all: true, resources: [] }]);
    assert.match(document.body.textContent!, /逐项移交结果/);
    assert.match(document.body.textContent!, /已移交/);
    assert.match(document.body.textContent!, /未移交：NOT_FOUND/);
  } finally { teamClient.transfer = original; }
});

test('allocation unknown controller survives closing and tab switches, then a confirmed operation gets a fresh command next time', async () => {
  await mount('owner');
  await click('成员');
  const original = teamClient.allocate;
  const commands: { commandId: string; amount?: number; all?: boolean }[] = [];
  teamClient.allocate = async (_id, holderId, kind, input) => {
    commands.push({ ...input });
    if (commands.length === 1) throw new ApiError(0, 'NETWORK', '提交未确认');
    return {
      id: 'event',
      operationId: 'op',
      accountId: 'team',
      holderId,
      kind,
      amount: input.amount || 0,
      balance: 29,
      actorId: 'owner',
      createdAt: at,
    };
  };
  try {
    await click('调整积分：成员乙');
    await fill(document.querySelector<HTMLInputElement>('[role=dialog] input[type=number]')!, '12');
    await click('确认调整');
    await click('取消');
    assert.equal(buttons('管理成员：成员乙')[0].disabled, true);
    await click('设置');
    await click('成员');
    assert.equal(buttons('管理成员：成员乙')[0].disabled, true);
    assert.equal(commands.length, 1);
    await click('核对未确认积分操作');
    assert.equal(
      document.querySelector<HTMLInputElement>('[role=dialog] input[type=number]')!.value,
      '12',
    );
    assert.equal(buttons('确认调整')[0].disabled, true);
    const reread = [...document.querySelectorAll<HTMLButtonElement>('[role=dialog] button')].find(
      (button) => button.textContent?.trim() === '读取最新状态',
    )!;
    await act(async () => reread.click());
    await flush();
    await click('确认调整');
    assert.deepEqual(commands[1], commands[0]);
    assert.equal(document.querySelector('[role=dialog]'), null);
    await click('调整积分：成员乙');
    assert.equal(
      document.querySelector<HTMLInputElement>('[role=dialog] input[type=number]')!.value,
      '',
    );
    await fill(document.querySelector<HTMLInputElement>('[role=dialog] input[type=number]')!, '12');
    await click('确认调整');
    assert.equal(commands.length, 3);
    assert.notEqual(commands[2].commandId, commands[1].commandId);
  } finally {
    teamClient.allocate = original;
  }
});
