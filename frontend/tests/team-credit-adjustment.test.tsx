import { after, test } from 'node:test';
import assert from 'node:assert/strict';
import React, { act, useState } from 'react';
import { JSDOM } from 'jsdom';
import type { SpaceRole, SpaceSummary } from '../src/api/spaces-types';
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
Object.defineProperty(globalThis, 'BroadcastChannel', { value: undefined, configurable: true });
const { createRoot } = await import('react-dom/client');
const { MemoryRouter } = await import('react-router-dom');
const { LanguageProvider } = await import('../src/contexts/LanguageContext');
const { SessionOptionalContext } = await import('../src/state/SessionContext');
const { SpaceProvider, useSpace } = await import('../src/state/SpaceContext');
const { spaceStorageKey } = await import('../src/api/space-selection');
const { sessionBoundary } = await import('../src/api/session-boundary');
const { MembersPanel } = await import('../src/pages/team/MembersPanel');
const originalFetch = globalThis.fetch;
let root: ReturnType<typeof createRoot> | undefined;
let writes: {
  member: string;
  body: { kind: string; commandId: string; amount?: number; all?: boolean };
}[];
let reads: string[];
let failWrite = false;
let readback: 'ok' | 'pending' | 'failed' = 'ok';
let finishReadback: ((reply: Response) => void) | undefined;
let setPaneVisible: (visible: boolean) => void;
const at = '2026-10-20T00:00:00Z';
const overview = {
  team: { id: 'team' },
  members: [
    { userId: 'alice', holderBalance: 17, credits: 2 },
    { userId: 'bob', holderBalance: 5, credits: 0 },
  ],
};
const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
};
const buttons = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].filter(
    (button) =>
      !button.closest('[hidden]') &&
      (button.textContent?.trim() === label || button.getAttribute('aria-label') === label),
  );
async function click(label: string) {
  assert.ok(buttons(label)[0], label);
  await act(() => buttons(label)[0].click());
  await flush();
}
async function openMenu(label: string) {
  const trigger = buttons(label)[0];
  assert.ok(trigger, label);
  await act(() => {
    trigger.focus();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  });
  await flush();
}
async function choose(label: string) {
  const item = [
    ...document.querySelectorAll<HTMLElement>('[role=menuitem],[role=menuitemradio]'),
  ].find((item) => item.textContent?.trim() === label);
  assert.ok(item, label);
  await act(() => item.click());
  await flush();
}
const amount = () => document.querySelector<HTMLInputElement>('[role=dialog] input[type=number]')!;
const direction = () =>
  document.querySelector<HTMLButtonElement>('[role=dialog] [aria-label="调整方向"]')!;
async function fill(value: string) {
  await act(() => {
    const input = amount();
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
}
function Ready({ children }: { children: React.ReactNode }) {
  return useSpace().ready ? children : null;
}
async function mount(role: SpaceRole) {
  if (root)
    await act(async () => {
      root!.unmount();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  sessionBoundary.reset();
  localStorage.clear();
  writes = [];
  reads = [];
  failWrite = false;
  readback = 'ok';
  finishReadback = undefined;
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
    name: '团队',
    kind: 'team',
    role,
    memberCount: 4,
    memberLimit: 6,
    status: 'active',
    readOnly: false,
  };
  localStorage.setItem(spaceStorageKey(user.identity), 'team');
  const members = [
    { userId: user.identity, name: '当前用户', role },
    { userId: 'alice', name: '成员甲', role: 'member' },
    { userId: 'bob', name: '成员乙', role: 'member' },
    { userId: 'admin', name: '管理员', role: 'admin' },
  ].map((member) => ({
    ...member,
    joinedAt: at,
    ...(role === 'member' ? {} : { email: member.userId + '@example.invalid' }),
  }));
  globalThis.fetch = async (url, options) => {
    const path = new URL(String(url), 'http://fixture.invalid').pathname;
    if (path === '/api/spaces/team/members') {
      reads.push(path);
      return Response.json(members);
    }
    if (path === '/api/spaces/team/overview') {
      reads.push(path);
      if (readback === 'failed')
        return Response.json({ code: 'UNAVAILABLE', message: '回读失败' }, { status: 503 });
      if (readback === 'pending')
        return new Promise<Response>((resolve) => {
          finishReadback = resolve;
        });
      return Response.json(overview);
    }
    if (path === '/api/credits/balance') {
      reads.push(path);
      return Response.json({ spaceId: 'team', role: 'member', balance: 17 });
    }
    const target = path.match(/^\/api\/spaces\/team\/members\/([^/]+)\/allocation$/);
    if (target && options?.method === 'POST') {
      const body = JSON.parse(options.body as string);
      writes.push({ member: target[1], body });
      if (failWrite) throw new TypeError('提交结果未知');
      return Response.json({
        id: 'event',
        operationId: body.commandId,
        accountId: 'team',
        holderId: target[1],
        actorId: user.identity,
        kind: body.kind,
        amount: body.amount ?? 17,
        balance: 17,
        createdAt: at,
      });
    }
    throw new Error('Unexpected fixture request ' + path);
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
    list: async () => [personal, team],
    detail: async (id: string) => (id === 'team' ? team : personal),
    pendingInvites: async () => [],
  };
  function Pane() {
    const [visible, setVisible] = useState(true);
    setPaneVisible = setVisible;
    return (
      <div hidden={!visible}>
        <MembersPanel id="team" role={role} onChanged={() => {}} />
      </div>
    );
  }
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <MemoryRouter>
          <SessionOptionalContext.Provider value={session}>
            <SpaceProvider service={service}>
              <Ready>
                <Pane />
              </Ready>
            </SpaceProvider>
          </SessionOptionalContext.Provider>
        </MemoryRouter>
      </LanguageProvider>,
    ),
  );
  await flush();
}

test('one credit adjustment dialog selects direction without submitting and clears reclaim-all before allocation', async () => {
  await mount('owner');
  assert.equal(document.querySelector('.team-toolbar h2'), null);
  assert.ok(buttons('成员统计范围')[0]);
  // 调整积分是行内可见动作，菜单只保留角色、所有权与移出。
  await click('调整积分：成员甲');
  assert.match(
    document.querySelector('[role=dialog]')!.textContent!,
    /进行中任务已预扣的部分不可收回/,
  );
  assert.equal(direction().textContent, '增加积分');
  assert.equal(direction().type, 'button');
  await fill('12');
  await openMenu('调整方向');
  await choose('减少积分');
  const all = document.querySelector<HTMLInputElement>('[role=dialog] input[type=checkbox]')!;
  await act(() => all.click());
  await flush();
  assert.equal(amount().disabled, true);
  await openMenu('调整方向');
  await choose('增加积分');
  assert.equal(document.querySelector('[role=dialog] input[type=checkbox]'), null);
  assert.equal(amount().disabled, false);
  assert.equal(amount().value, '12');
  assert.equal(writes.length, 0, 'selecting direction must never submit the form');
  await click('确认调整');
  assert.equal(writes.length, 1);
  assert.equal(writes[0].body.kind, 'allocate');
  assert.equal(writes[0].body.amount, 12);
  assert.equal('all' in writes[0].body, false);
  await click('调整积分：成员甲');
  assert.equal(direction().textContent, '增加积分');
  assert.equal(amount().value, '');
  await openMenu('调整方向');
  await choose('减少积分');
  await act(() =>
    document.querySelector<HTMLInputElement>('[role=dialog] input[type=checkbox]')!.click(),
  );
  await flush();
  await click('确认调整');
  assert.equal(writes[1].body.kind, 'reclaim');
  assert.equal(writes[1].body.all, true);
  assert.equal('amount' in writes[1].body, false);
  assert.notEqual(writes[0].body.commandId, writes[1].body.commandId);
});

test('unknown adjustment preserves one controller and locks direction, amount, all and every member across closing and pane switches', async () => {
  await mount('owner');
  failWrite = true;
  await click('调整积分：成员甲');
  await openMenu('调整方向');
  await choose('减少积分');
  await fill('12');
  await act(() =>
    document.querySelector<HTMLInputElement>('[role=dialog] input[type=checkbox]')!.click(),
  );
  await flush();
  await click('确认调整');
  const first = structuredClone(writes[0]);
  assert.equal(writes.length, 1);
  assert.equal(direction().disabled, true);
  assert.equal(amount().disabled, true);
  assert.equal(
    document.querySelector<HTMLInputElement>('[role=dialog] input[type=checkbox]')!.disabled,
    true,
  );
  await click('取消');
  assert.equal(document.querySelector('[role=dialog]'), null);
  assert.equal(buttons('管理成员：成员乙')[0].disabled, true);
  await act(() => setPaneVisible(false));
  await act(() => setPaneVisible(true));
  await flush();
  assert.equal(buttons('管理成员：成员甲')[0].disabled, true);
  await click('核对未确认积分操作');
  assert.equal(direction().textContent, '减少积分');
  assert.equal(direction().disabled, true);
  assert.equal(amount().value, '12');
  assert.equal(
    document.querySelector<HTMLInputElement>('[role=dialog] input[type=checkbox]')!.checked,
    true,
  );
  await click('确认调整');
  assert.equal(writes.length, 1);
  readback = 'failed';
  await click('读取最新状态');
  assert.match(document.querySelector('[role=dialog]')!.textContent!, /回读失败/);
  assert.equal(direction().disabled, true);
  readback = 'pending';
  await click('读取最新状态');
  assert.ok(finishReadback);
  assert.equal(direction().disabled, true);
  assert.equal(writes.length, 1);
  readback = 'ok';
  failWrite = false;
  await act(async () => finishReadback!(Response.json(overview)));
  await flush();
  assert.equal(direction().disabled, false);
  await click('确认调整');
  assert.equal(writes.length, 2);
  assert.deepEqual(writes[1], first, 'only explicit retry reuses the exact frozen command');
  assert.equal(document.querySelector('[role=dialog]'), null);
  await click('调整积分：成员甲');
  assert.equal(amount().value, '', 'confirmed old controller cannot relock the next adjustment');
  assert.equal(direction().textContent, '增加积分');
  await fill('12');
  await click('确认调整');
  assert.equal(writes.length, 3);
  assert.notEqual(writes[2].body.commandId, first.body.commandId);
});

test('an editable new direction creates a new command after explicit readback, while ordinary members have no credit adjustment actions', async () => {
  await mount('admin');
  failWrite = true;
  await click('调整积分：成员甲');
  await fill('8');
  await click('确认调整');
  const first = structuredClone(writes[0]);
  await click('读取最新状态');
  await openMenu('调整方向');
  await choose('减少积分');
  failWrite = false;
  await click('确认调整');
  assert.equal(writes.length, 2);
  assert.notEqual(writes[1].body.commandId, first.body.commandId);
  assert.equal(writes[1].body.kind, 'reclaim');
  assert.equal(writes[1].body.amount, 8);
  await mount('member');
  assert.equal(buttons('调整积分').length, 0);
  assert.equal(buttons('管理成员：成员甲').length, 0);
  assert.equal(buttons('成员统计范围').length, 0);
  assert.equal(
    reads.some((path) => path.endsWith('/overview')),
    false,
  );
  assert.ok(document.body.textContent!.includes('我的可用积分'));
  assert.equal(buttons('退出团队').length, 1);
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
