import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import { createAdministrationFixture } from './administration-fixture';
import type { SpaceState } from '../src/state/SpaceContext';
import type { Invite, SpaceSummary } from '../src/api/spaces-types';

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
let narrow = false;
const mediaListeners = new Set<() => void>();
globalThis.matchMedia = () => ({
  get matches() {
    return narrow;
  },
  addEventListener(_name: string, listener: () => void) {
    mediaListeners.add(listener);
  },
  removeEventListener(_name: string, listener: () => void) {
    mediaListeners.delete(listener);
  },
});
class Channel {
  static sent: unknown[] = [];
  onmessage = null;
  postMessage(value: unknown) {
    Channel.sent.push(value);
  }
  close() {}
}
Object.defineProperty(globalThis, 'BroadcastChannel', { value: Channel, configurable: true });
const { createRoot } = await import('react-dom/client');
const { HashRouter } = await import('react-router-dom');
const { LanguageProvider } = await import('../src/contexts/LanguageContext');
const { SessionOptionalContext } = await import('../src/state/SessionContext');
const { WorkspaceProvider } = await import('../src/state/WorkspaceContext');
const { SpaceProvider, useSpace } = await import('../src/state/SpaceContext');
const { SidebarAccountMenu } = await import('../src/components/workspace/SidebarAccountMenu');
const { spaceStorageKey } = await import('../src/api/space-selection');
let root: ReturnType<typeof createRoot> | undefined, current: SpaceState;
const personal: SpaceSummary = {
  id: 'user',
  kind: 'personal',
  name: '个人空间',
  role: 'owner',
  memberCount: 1,
  memberLimit: 1,
  status: 'active',
  readOnly: false,
};
const name = '山海之间的长篇影像与跨地域原创故事联合创作工作室';
const team: SpaceSummary = {
  id: 'team',
  kind: 'team',
  name,
  role: 'owner',
  memberCount: 23,
  memberLimit: 30,
  status: 'expired',
  readOnly: false,
};
const other: SpaceSummary = {
  ...team,
  id: 'other',
  name: '远山故事室',
  role: 'admin',
  status: 'active',
};
const flush = async () =>
  act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
function Probe() {
  current = useSpace();
  return <output data-selection>{current.spaceId}</output>;
}
async function mount(isNarrow: boolean) {
  if (root) await act(async () => root!.unmount());
  narrow = isNarrow;
  localStorage.clear();
  Channel.sent = [];
  localStorage.setItem(spaceStorageKey('user'), 'team');
  window.location.hash = '#/projects';
  await flush();
  const accounts = createAdministrationFixture();
  const user = await accounts.service.current();
  const session = {
    state: 'authenticated',
    user: { ...user!, identity: 'user' },
    epoch: 0,
    service: accounts.service,
    logout: () => accounts.service.logout(),
  } as NonNullable<React.ContextType<typeof SessionOptionalContext>>;
  const selected: string[] = [];
  const rows = [personal, team, other];
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <HashRouter>
          <SessionOptionalContext.Provider value={session}>
            <SpaceProvider
              service={{
                async list() {
                  return rows;
                },
                async pendingInvites(): Promise<Invite[]> {
                  return [
                    {
                      id: 'pending',
                      spaceId: 'inviting-team',
                      teamName: '受邀团队',
                      kind: 'email',
                      inviteeUserId: 'user',
                      createdBy: 'inviter',
                      status: 'pending',
                      createdAt: '2099-10-20T00:00:00Z',
                      expiresAt: '2099-10-27T00:00:00Z',
                    },
                  ];
                },
                async detail(id) {
                  selected.push(id);
                  return rows.find((item) => item.id === id)!;
                },
              }}
            >
              <WorkspaceProvider load="none">
                <Probe />
                <SidebarAccountMenu label="测试空间账号" showLanguage />
              </WorkspaceProvider>
            </SpaceProvider>
          </SessionOptionalContext.Provider>
        </HashRouter>
      </LanguageProvider>,
    ),
  );
  await flush();
  return { selected, accounts };
}
const trigger = () => document.querySelector<HTMLButtonElement>('[aria-label="测试空间账号"]')!;
const parent = () =>
  document.querySelector<HTMLElement>('.xy-preferences-menu:not(.space-submenu)')!;
const key = async (element: Element, value: string) => {
  await act(async () =>
    element.dispatchEvent(
      new dom.window.KeyboardEvent('keydown', { key: value, bubbles: true, cancelable: true }),
    ),
  );
  await flush();
};
async function open() {
  await key(trigger(), 'ArrowDown');
  assert.ok(parent());
}
async function openSpaces() {
  const entry = document.querySelector<HTMLElement>('.space-menu-trigger')!;
  assert.ok(entry);
  await act(async () => entry.focus());
  await key(entry, narrow ? 'Enter' : 'ArrowRight');
}
const click = async (element: Element) => {
  await act(async () =>
    element.dispatchEvent(new dom.window.MouseEvent('click', { bubbles: true, cancelable: true })),
  );
  await flush();
};
after(async () => {
  if (root) await act(async () => root!.unmount());
  dom.window.close();
});

test('desktop submenu keeps complete names and role/status facts; a keyboard choice uses the existing validated switch', async () => {
  const f = await mount(false);
  await open();
  await openSpaces();
  const submenu = document.querySelector<HTMLElement>('.space-submenu')!;
  assert.ok(submenu);
  assert.ok(parent());
  assert.equal(
    submenu.querySelector('[data-current="true"]')?.getAttribute('aria-checked'),
    'true',
  );
  assert.match(submenu.textContent || '', new RegExp(name));
  assert.match(submenu.textContent || '', /23人/);
  assert.match(submenu.textContent || '', /所有者/);
  assert.match(submenu.textContent || '', /已到期/);
  const choice = submenu.querySelector<HTMLElement>(
    '[role="menuitemradio"][data-current="false"]',
  )!;
  await act(async () => choice.focus());
  await key(choice, 'Enter');
  assert.deepEqual(f.selected, ['user']);
  assert.equal(current.spaceId, 'user');
  assert.equal(document.activeElement, trigger());
  assert.equal(Channel.sent.length, 1);
  assert.equal((Channel.sent[0] as { spaceId: string }).spaceId, 'user');
});

test('desktop Escape closes only the child before the account menu and restores each trigger', async () => {
  const f = await mount(false);
  await open();
  await openSpaces();
  const choice = document.querySelector<HTMLElement>('.space-submenu [role="menuitemradio"]')!;
  await act(async () => choice.focus());
  await key(choice, 'Escape');
  assert.equal(document.querySelector('.space-submenu'), null);
  assert.ok(parent());
  assert.equal(document.activeElement, document.querySelector('.space-menu-trigger'));
  await key(document.activeElement!, 'Escape');
  assert.equal(parent(), null);
  assert.equal(document.activeElement, trigger());
  assert.equal(f.accounts.requests.filter((request) => request.method === 'logout').length, 0);
  assert.deepEqual(f.selected, []);
});

test('390-style navigation stays in one menu, returns with Escape/ArrowLeft and keeps focus', async () => {
  await mount(true);
  await open();
  const original = parent();
  await openSpaces();
  assert.equal(parent(), original);
  assert.equal(document.querySelectorAll('[role="menu"]').length, 1);
  assert.equal(document.querySelector('.space-submenu'), null);
  assert.equal(document.activeElement?.getAttribute('aria-checked'), 'true');
  await key(document.activeElement!, 'Escape');
  assert.equal(parent(), original);
  assert.equal(document.querySelector('.space-menu-back'), null);
  assert.equal(document.activeElement, document.querySelector('.space-menu-trigger'));
  await openSpaces();
  await key(document.activeElement!, 'ArrowLeft');
  assert.equal(document.activeElement, document.querySelector('.space-menu-trigger'));
  await key(document.activeElement!, 'Escape');
  assert.equal(document.activeElement, trigger());
});

test('narrow space list enters a team from its row and preserves pending invitation and joining routes', async () => {
  const f = await mount(true);
  await open();
  await openSpaces();
  const invite = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((item) =>
    item.textContent?.startsWith('待处理邀请'),
  )!;
  assert.match(invite.textContent || '', /1/);
  await click(invite);
  assert.equal(window.location.hash, '#/invites');
  assert.equal(parent(), null);
  await open();
  assert.equal(
    [...document.querySelectorAll('[role="menuitem"]')].some(
      (item) => item.textContent === '团队设置',
    ),
    false,
  );
  await openSpaces();
  const enter = document.querySelector<HTMLElement>('[role="menuitem"][aria-label^="进入团队："]')!;
  assert.ok(enter, 'each team row carries its own enter action');
  await click(enter);
  assert.equal(window.location.hash, '#/team/team');
  assert.deepEqual(f.selected, [], 'entering lets the team page validate and switch');
  await open();
  await openSpaces();
  const joining = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
    (item) => item.textContent === '加入团队',
  )!;
  await click(joining);
  assert.ok(document.querySelector('[role="dialog"]'));
  assert.deepEqual(f.selected, []);
  assert.equal(f.accounts.requests.filter((request) => request.method === 'logout').length, 0);
});

test('changing the viewport closes the old hierarchy without selecting or logging out', async () => {
  const f = await mount(false);
  await open();
  await openSpaces();
  await act(async () => {
    narrow = true;
    for (const listener of mediaListeners) listener();
  });
  await flush();
  assert.equal(document.querySelector('[role="menu"]'), null);
  assert.equal(document.activeElement, trigger());
  await open();
  await openSpaces();
  assert.equal(document.querySelectorAll('[role="menu"]').length, 1);
  assert.deepEqual(f.selected, []);
  assert.equal(f.accounts.requests.filter((request) => request.method === 'logout').length, 0);
});
test('account menu “更多” holds the only recharge record entry on desktop and narrow layouts', async () => {
  for (const isNarrow of [false, true]) {
    await mount(isNarrow);
    await open();
    const more = [...document.querySelectorAll<HTMLElement>('.space-menu-trigger')].find((item) =>
      item.textContent?.startsWith('更多'),
    )!;
    assert.ok(more, 'more entry');
    await act(async () => more.focus());
    await key(more, isNarrow ? 'Enter' : 'ArrowRight');
    if (!isNarrow) assert.ok(document.querySelector('.space-submenu'));
    const items = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')];
    // 协议与政策只以新标签页打开独立文档，不在当前工作台内导航。
    for (const [text, href] of [
      ['用户服务协议', '#/legal/terms'],
      ['隐私政策', '#/legal/privacy'],
    ]) {
      const link = items.find((item) => item.textContent === text);
      assert.equal(link?.tagName, 'A', text);
      assert.equal(link!.getAttribute('href'), href);
      assert.equal(link!.getAttribute('target'), '_blank');
      assert.match(link!.getAttribute('rel') || '', /noopener/);
    }
    const records = items.find((item) => item.textContent === '充值记录')!;
    assert.ok(records, 'recharge records entry');
    await click(records);
    assert.equal(window.location.hash, '#/payments/orders');
    assert.equal(parent(), null);
  }
});
