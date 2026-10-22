import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import type { SpaceState } from '../src/state/SpaceContext';
import type { SpacesApi } from '../src/api/spaces';
import type { SpaceSummary } from '../src/api/spaces-types';
import { createAdministrationFixture } from './administration-fixture';
const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/#/creations',
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
  Object.defineProperty(globalThis, key, {
    value: dom.window[key],
    configurable: true,
  });
globalThis.getComputedStyle = dom.window.getComputedStyle;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.matchMedia = () => ({
  matches: false,
  addEventListener() {},
  removeEventListener() {},
});
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
class Channel {
  static live = new Set<Channel>();
  onmessage: ((event: { data: unknown }) => void) | null = null;
  sent: unknown[] = [];
  constructor(readonly name: string) {
    Channel.live.add(this);
  }
  postMessage(value: unknown) {
    this.sent.push(value);
  }
  close() {
    Channel.live.delete(this);
  }
}
Object.defineProperty(globalThis, 'BroadcastChannel', {
  value: Channel,
  configurable: true,
});
const { createRoot } = await import('react-dom/client');
const { HashRouter, useLocation, Routes, Route } = await import('react-router-dom');
const { LanguageProvider } = await import('../src/contexts/LanguageContext');
const { SessionOptionalContext, SessionProvider } = await import('../src/state/SessionContext');
const { WorkspaceProvider } = await import('../src/state/WorkspaceContext');
const { SpaceProvider, useSpace } = await import('../src/state/SpaceContext');
const { SidebarAccountMenu } = await import('../src/components/workspace/SidebarAccountMenu');
const { TeamSettingsPage } = await import('../src/pages/team/TeamSettingsPage');
const { ApiError } = await import('../src/api/http-error');
const { spaceSelection, spaceStorageKey, parseSpacePreference } = await import(
  '../src/api/space-selection'
);
const { toast } = await import('../src/components/ui/sonner');
const { spaceSwitchNoticeKey } = await import('../src/state/SpaceContext');
let root: ReturnType<typeof createRoot> | undefined, latest: SpaceState;
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
const team: SpaceSummary = {
  ...personal,
  id: 'team',
  kind: 'team',
  name: '虚构团队',
  role: 'member',
  memberCount: 3,
  memberLimit: 5,
};
const flush = async () => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 10));
  });
};
function Probe() {
  latest = useSpace();
  const location = useLocation();
  return (
    <output data-state>{latest.spaceId + ':' + latest.ready + ':' + location.pathname}</output>
  );
}
function FixtureWorkspace({ menu, teamRoute }: { menu: boolean; teamRoute: boolean }) {
  const space = useSpace();
  return (
    <WorkspaceProvider key={space.epoch} load="none">
      <Probe />
      {menu ? <SidebarAccountMenu label="空间测试菜单" /> : null}
      {teamRoute ? (
        <Routes>
          <Route path="/team/:spaceId" element={<TeamSettingsPage />} />
          <Route path="*" element={<p>fixture route</p>} />
        </Routes>
      ) : null}
    </WorkspaceProvider>
  );
}
async function mount(
  rows: SpaceSummary[],
  restore = '',
  menu = false,
  initialList?: Promise<SpaceSummary[]>,
  teamRoute = false,
  storage: Record<string, string> = {},
) {
  if (root) await act(async () => root!.unmount());
  localStorage.clear();
  if (restore) localStorage.setItem(spaceStorageKey('user'), restore);
  for (const [key, value] of Object.entries(storage)) localStorage.setItem(key, value);
  window.location.hash = teamRoute ? '#/team/team' : '#/creations';
  await flush();
  const f = createAdministrationFixture();
  const current = await f.service.current();
  const session = {
    state: 'authenticated',
    user: { ...current!, identity: 'user' },
    epoch: 0,
    service: f.service,
  } as NonNullable<React.ContextType<typeof SessionOptionalContext>>;
  const fixture = {
    rows,
    details: [] as string[],
    service: null as unknown as SpacesApi,
  };
  fixture.service = {
    async list() {
      if (initialList) {
        const pending = initialList;
        initialList = undefined;
        return pending;
      }
      return fixture.rows;
    },
    async pendingInvites() {
      return [];
    },
    async detail(id) {
      fixture.details.push(id);
      const row = fixture.rows.find((v) => v.id === id);
      if (!row || row.status === 'disabled') throw new ApiError(404, 'NOT_FOUND', 'unavailable');
      return row;
    },
  };
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <HashRouter>
          <SessionOptionalContext.Provider value={session}>
            <SpaceProvider service={fixture.service}>
              <FixtureWorkspace menu={menu} teamRoute={teamRoute} />
            </SpaceProvider>
          </SessionOptionalContext.Provider>
        </HashRouter>
      </LanguageProvider>,
    ),
  );
  await flush();
  return fixture;
}
after(async () => {
  if (root) await act(async () => root!.unmount());
  dom.window.close();
});
test('remembered space restores per account; failed selection cannot grant access; removal returns home', async () => {
  const fixture = await mount([personal, team], 'team');
  assert.equal(latest.ready, true);
  assert.equal(latest.spaceId, 'team');
  assert.equal(spaceSelection.spaceId, 'team');
  const epoch = latest.epoch;
  let accepted = true;
  await act(async () => {
    accepted = await latest.switchSpace('outsider');
  });
  assert.equal(accepted, false);
  assert.equal(latest.spaceId, 'team');
  assert.equal(latest.epoch, epoch);
  fixture.rows = [personal];
  await act(async () => latest.refresh());
  assert.equal(latest.spaceId, 'user');
  assert.equal(document.querySelector('[data-state]')?.textContent, 'user:true:/projects');
  assert.equal(
    parseSpacePreference(localStorage.getItem(spaceStorageKey('user')))?.spaceId,
    'user',
  );
});
test('validated tab message switches once without echo and keepRoute retains resource path', async () => {
  const fixture = await mount([personal, team]);
  const channel = [...Channel.live].find((v) => v.name === 'space:user')!;
  await act(async () => {
    channel.onmessage?.({ data: { spaceId: 'team', counter: 2, tabId: 'other-tab' } });
    await new Promise((r) => setTimeout(r, 10));
  });
  assert.equal(latest.spaceId, 'team');
  assert.deepEqual(fixture.details, ['team']);
  assert.equal(channel.sent.length, 0);
  window.location.hash = '#/playlet/p/outline';
  await flush();
  await act(async () => {
    assert.equal(await latest.switchSpace('user', { keepRoute: true }), true);
  });
  assert.equal(window.location.hash, '#/playlet/p/outline');
  assert.equal(latest.spaceId, 'user');
  assert.equal(channel.sent.length, 1);
});
test('storage and broadcast share one version and stale messages cannot undo a validated choice', async () => {
  const fixture = await mount([personal, team]);
  const channel = [...Channel.live].find((v) => v.name === 'space:user')!;
  const remote = { spaceId: 'team', counter: 10, tabId: 'remote' };
  localStorage.setItem(spaceStorageKey('user'), JSON.stringify(remote));
  await act(async () => {
    window.dispatchEvent(
      new dom.window.StorageEvent('storage', {
        key: spaceStorageKey('user'),
        newValue: JSON.stringify(remote),
      }),
    );
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
  assert.equal(latest.spaceId, 'team');
  assert.deepEqual(fixture.details, ['team']);
  // A simultaneous losing writer can overwrite localStorage before its older
  // event arrives. Receiving it repairs the same winner without another GET.
  const old = { spaceId: 'user', counter: 10, tabId: 'older' };
  localStorage.setItem(spaceStorageKey('user'), JSON.stringify(old));
  await act(async () => {
    channel.onmessage?.({ data: old });
    channel.onmessage?.({ data: remote });
  });
  assert.equal(latest.spaceId, 'team');
  assert.deepEqual(fixture.details, ['team']);
  assert.deepEqual(parseSpacePreference(localStorage.getItem(spaceStorageKey('user'))), remote);
  assert.equal(channel.sent.length, 0);
});
test('a late remote validation cannot commit after a newer space version was validated', async () => {
  const fixture = await mount([personal, team]);
  const channel = [...Channel.live].find((v) => v.name === 'space:user')!;
  let finish!: (value: SpaceSummary) => void;
  const original = fixture.service.detail;
  fixture.service.detail = (id) =>
    id === 'team'
      ? new Promise((resolve) => {
          finish = resolve;
        })
      : original(id);
  await act(async () => {
    channel.onmessage?.({ data: { spaceId: 'team', counter: 20, tabId: 'remote' } });
  });
  assert.equal(latest.spaceId, 'user');
  const newest = { spaceId: 'user', counter: 21, tabId: 'remote' };
  await act(async () => {
    channel.onmessage?.({ data: newest });
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
  await act(async () => {
    finish(team);
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
  assert.equal(latest.spaceId, 'user');
  assert.deepEqual(parseSpacePreference(localStorage.getItem(spaceStorageKey('user'))), newest);
  assert.equal(channel.sent.length, 0);
});
test('failed remote selection during the initial load exposes retry instead of waiting for polling', async () => {
  for (const failure of [
    new ApiError(404, 'NOT_FOUND', 'unavailable'),
    new ApiError(0, 'NETWORK', 'offline'),
  ]) {
    let finish!: (rows: SpaceSummary[]) => void;
    const initial = new Promise<SpaceSummary[]>((resolve) => {
      finish = resolve;
    });
    const fixture = await mount([personal, team], '', false, initial);
    assert.equal(latest.ready, false);
    fixture.service.detail = async () => {
      throw failure;
    };
    const channel = [...Channel.live].find((v) => v.name === 'space:user')!;
    await act(async () => {
      channel.onmessage?.({ data: { spaceId: 'unavailable', counter: 30, tabId: 'remote' } });
      await new Promise((resolve) => setTimeout(resolve, 10));
    });
    assert.equal(latest.ready, false);
    assert.equal(latest.error, failure.message);
    // The superseded first response cannot erase the actionable error.
    await act(async () => {
      finish([personal, team]);
    });
    assert.equal(latest.ready, false);
    assert.equal(latest.error, failure.message);
    await act(async () => latest.refresh());
    assert.equal(latest.ready, true);
    assert.equal(latest.error, '');
    assert.equal(latest.spaceId, 'user');
  }
});
test('switching from a real team settings route navigates and changes epoch together', async () => {
  const previousFetch = globalThis.fetch;
  globalThis.fetch = async (target) => {
    const path = String(target).split('?')[0];
    if (path === '/api/spaces/team') return Response.json(team);
    if (path.endsWith('/members')) return Response.json([]);
    if (path === '/api/credits/balance') return Response.json({ accountId: 'team', balance: 0 });
    return Response.json({ readOnly: false, capabilities: {} });
  };
  try {
    const other = { ...team, id: 'other-team', name: '另一个团队' };
    const fixture = await mount([personal, team, other], 'team', false, undefined, true);
    assert.equal(latest.spaceId, 'team');
    assert.equal(window.location.hash, '#/team/team');
    await act(async () => {
      assert.equal(await latest.switchSpace('other-team'), true);
    });
    await flush();
    await flush();
    assert.equal(window.location.hash, '#/projects');
    assert.equal(latest.spaceId, 'other-team');
    assert.deepEqual(fixture.details, ['other-team']);
    assert.equal(
      parseSpacePreference(localStorage.getItem(spaceStorageKey('user')))?.spaceId,
      'other-team',
    );
  } finally {
    if (root) await act(async () => root!.unmount());
    root = undefined;
    globalThis.fetch = previousFetch;
  }
});
test('an older space refresh cannot replace a later validated explicit selection', async () => {
  const fixture = await mount([personal, team]);
  let finish!: (rows: SpaceSummary[]) => void;
  fixture.service.list = () =>
    new Promise((resolve) => {
      finish = resolve;
    });
  const refreshing = latest.refresh();
  await act(async () => {
    assert.equal(await latest.switchSpace('team', { keepRoute: true }), true);
  });
  const selectedEpoch = latest.epoch;
  await act(async () => {
    finish([personal]);
    await refreshing;
  });
  assert.equal(latest.spaceId, 'team');
  assert.equal(latest.epoch, selectedEpoch);
  assert.equal(spaceSelection.spaceId, 'team');
  assert.equal(
    parseSpacePreference(localStorage.getItem(spaceStorageKey('user')))?.spaceId,
    'team',
  );
});
test('switch notice appears only on the first switch to each space, not on later switches or restores', async () => {
  const info = toast.info,
    notices: string[] = [];
  toast.info = (message) => {
    notices.push(message);
    return 'space-selection';
  };
  try {
    await mount([personal, team]);
    assert.deepEqual(notices, [], 'the default personal space on load is not a switch');
    for (const id of ['team', 'user', 'team', 'user'])
      await act(async () => {
        assert.equal(await latest.switchSpace(id, { keepRoute: true }), true);
      });
    assert.deepEqual(notices, ['已切换到团队「虚构团队」', '已切换到个人空间']);
    assert.deepEqual(JSON.parse(localStorage.getItem(spaceSwitchNoticeKey('user'))!), [
      'team',
      'user',
    ]);
    // 刷新后恢复上次的团队空间也不再提示已经提示过的空间。
    const seen = localStorage.getItem(spaceSwitchNoticeKey('user'))!;
    await mount([personal, team], 'team', false, undefined, false, {
      [spaceSwitchNoticeKey('user')]: seen,
    });
    assert.equal(latest.spaceId, 'team');
    assert.equal(notices.length, 2);
    // 从未提示过的账号恢复到团队，仍算第一次进入该团队。
    await mount([personal, team], 'team');
    assert.deepEqual(notices.slice(2), ['已切换到团队「虚构团队」']);
  } finally {
    toast.info = info;
  }
});
test('no-team account menu only adds joining; joined account exposes space submenu and current subtitle', async () => {
  await mount([personal], '', true);
  const open = async () => {
    const trigger = document.querySelector('[aria-label="空间测试菜单"]') as HTMLButtonElement;
    await act(async () => {
      trigger.focus();
      trigger.dispatchEvent(
        new dom.window.KeyboardEvent('keydown', {
          key: 'ArrowDown',
          bubbles: true,
        }),
      );
    });
    await flush();
  };
  await open();
  assert.match(document.body.textContent || '', /加入团队/);
  assert.doesNotMatch(document.body.textContent || '', /当前空间/);
  await mount([personal, team], '', true);
  await open();
  assert.match(document.body.textContent || '', /当前空间/);
  assert.equal(document.querySelector('.space-account-subtitle')?.textContent, '个人空间');
});

test('invitation login and registration retain the internal return route without previewing before authentication', async () => {
  if (root) await act(async () => root!.unmount());
  localStorage.clear();
  sessionStorage.clear();
  const fixture = createAdministrationFixture();
  fixture.user = null;
  window.location.hash = '#/invite/fixture-invitation-token';
  await flush();
  const requests: string[] = [];
  const previousFetch = globalThis.fetch;
  globalThis.fetch = async (target) => {
    const path = String(target).split('?')[0];
    requests.push(path);
    const personal = { ...personalTemplate(), id: fixture.user?.identity || 'anonymous' };
    if (path === '/api/spaces') return Response.json([personal]);
    if (path === '/api/invites/pending') return Response.json([]);
    if (path === '/api/invite-links/fixture-invitation-token')
      return Response.json({
        id: 'invite',
        spaceId: 'not-yet-joined',
        teamName: '邀请中的团队',
        expiresAt: '2099-10-20T03:00:00Z',
      });
    if (path === '/api/notifications/unread') return Response.json({ unread: 0 });
    if (path.endsWith('/events'))
      return Response.json(
        { code: 'RUNTIME_UNAVAILABLE', message: 'fixture stream unavailable' },
        { status: 503 },
      );
    return Response.json({ readOnly: false, capabilities: {} });
  };
  const { Application } = await import('../src/Application');
  root = createRoot(document.getElementById('root')!);
  try {
    await act(async () =>
      root!.render(
        <LanguageProvider>
          <HashRouter>
            <SessionProvider service={fixture.service}>
              <Application />
            </SessionProvider>
          </HashRouter>
        </LanguageProvider>,
      ),
    );
    await flush();
    await flush();
    assert.match(window.location.hash, /^#\/login\?returnTo=/);
    assert.equal(requests.filter((v) => v.includes('invite-links')).length, 0);
    const register = document.querySelector('a[href^="#/register"]') as HTMLAnchorElement;
    assert.ok(register.href.includes('returnTo='));
    await act(async () => register.click());
    await flush();
    const fill = async (selector: string, value: string) => {
      const input = document.querySelector(selector) as HTMLInputElement;
      assert.ok(input, selector);
      await act(async () => {
        Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, 'value')!.set!.call(
          input,
          value,
        );
        input.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
      });
    };
    await fill('input[autocomplete="nickname"]', '虚构申请人');
    await fill('input[autocomplete="username"]', 'invitee@example.invalid');
    await fill('input[autocomplete="one-time-code"]', '123456');
    await fill('input[autocomplete="new-password"]', 'fictional-password');
    await fill('input[autocomplete="off"]', 'fixture-registration-grant');
    await act(async () => {
      document
        .querySelector('form.account-form')!
        .dispatchEvent(new dom.window.Event('submit', { bubbles: true, cancelable: true }));
    });
    await flush();
    assert.equal(fixture.requests.filter((v) => v.method === 'register').length, 1);
    assert.equal(fixture.user, null);
    const login = document.querySelector('a[href^="#/login"]') as HTMLAnchorElement;
    assert.ok(login.href.includes('returnTo='));
    await act(async () => login.click());
    await flush();
    await fill('input[autocomplete="username"]', 'invitee@example.invalid');
    await fill('input[autocomplete="current-password"]', 'fictional-password');
    await act(async () => {
      document
        .querySelector('form.account-form')!
        .dispatchEvent(new dom.window.Event('submit', { bubbles: true, cancelable: true }));
    });
    await flush();
    await flush();
    assert.equal(window.location.hash, '#/invite/fixture-invitation-token');
    assert.equal(document.querySelector('h1')?.textContent, '确认加入团队');
    assert.match(document.body.textContent || '', /邀请中的团队/);
    assert.ok(document.querySelector('.space-settings-workspace'));
    assert.equal(requests.includes('/api/spaces/not-yet-joined'), false);
    assert.ok(requests.includes('/api/invite-links/fixture-invitation-token'));
  } finally {
    if (root) await act(async () => root!.unmount());
    root = undefined;
    globalThis.fetch = previousFetch;
  }
});
function personalTemplate(): SpaceSummary {
  return { ...personal };
}
