import { after, test } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import { createAdministrationFixture } from './administration-fixture';

const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/#/credits',
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
class Channel {
  static live = new Set<Channel>();
  onmessage: ((event: { data: unknown }) => void) | null = null;
  constructor(readonly name: string) {
    Channel.live.add(this);
  }
  postMessage(data: unknown) {
    for (const channel of Channel.live)
      if (channel !== this && channel.name === this.name)
        queueMicrotask(() => channel.onmessage?.({ data }));
  }
  close() {
    Channel.live.delete(this);
  }
}
Object.defineProperty(globalThis, 'BroadcastChannel', { value: Channel, configurable: true });
const { createRoot } = await import('react-dom/client'),
  { HashRouter } = await import('react-router-dom');
const { LanguageProvider } = await import('../src/contexts/LanguageContext'),
  { WorkspaceProvider } = await import('../src/state/WorkspaceContext');
const { SessionProvider, useSession } = await import('../src/state/SessionContext'),
  { sessionBoundary } = await import('../src/api/session-boundary');
const { sessionAudience, sessionChangeChannel } = await import('../src/api/session-audience');
const { SidebarAccountMenu } = await import('../src/components/workspace/SidebarAccountMenu'),
  { ApiError } = await import('../src/api/http-error');
const { createHttpClient, createApi, protectedResourceUrl } = await import('../src/api/client'),
  { SessionBoundary } = await import('../src/api/session-boundary');
let root: ReturnType<typeof createRoot> | undefined,
  fixture = createAdministrationFixture(),
  session: ReturnType<typeof useSession>;
function Probe() {
  session = useSession();
  return (
    <>
      {session.user ? (
        <div data-private>{session.user.username}</div>
      ) : (
        <input autoComplete="username" aria-label="登录邮箱" />
      )}
    </>
  );
}
const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
};
async function mount(
  route: string,
  setup?: (f: ReturnType<typeof createAdministrationFixture>) => void,
  menu = false,
) {
  if (root) await act(async () => root!.unmount());
  for (const channel of Channel.live) channel.close();
  sessionBoundary.reset();
  sessionStorage.clear();
  fixture = createAdministrationFixture();
  setup?.(fixture);
  window.location.hash = '#' + route;
  await flush();
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <HashRouter>
          <SessionProvider service={fixture.service}>
            <Probe />
            {menu ? (
              <WorkspaceProvider load="none">
                <SidebarAccountMenu label="测试账号菜单" showLanguage />
              </WorkspaceProvider>
            ) : null}
          </SessionProvider>
        </HashRouter>
      </LanguageProvider>,
    ),
  );
  await flush();
}
after(async () => {
  if (root) await act(async () => root!.unmount());
  for (const channel of Channel.live) channel.close();
  dom.window.close();
});

test('platform channel ignores the other surface and malformed events, while its own event rechecks through the server', async () => {
  await mount('/operations/users');
  fixture.requests.length = 0;
  const epoch = sessionBoundary.epoch;
  const workspace = new Channel(sessionChangeChannel('workspace')),
    operations = new Channel(sessionChangeChannel('operations'));
  await act(async () => {
    workspace.postMessage('changed');
    operations.postMessage({ role: 'operator' });
  });
  await flush();
  assert.equal(sessionBoundary.epoch, epoch);
  assert.deepEqual(fixture.requests, []);
  assert.ok(session.user);
  fixture.user = null;
  await act(async () => operations.postMessage('changed'));
  await flush();
  assert.equal(session.user, null);
  assert.equal(fixture.requests.filter((request) => request.method === 'current').length, 1);
  assert.equal(
    fixture.requests.some((request) => request.method === 'logout'),
    false,
  );
});
test('replaced sessions clear private state and show one accessible popup without sending logout or resurfacing on focus', async () => {
  await mount('/credits');
  sessionStorage.setItem('media-command:private', 'old account draft');
  sessionStorage.setItem('drama:credit-command:operations', 'keep other platform');
  await act(async () =>
    sessionBoundary.unauthorized(
      sessionBoundary.epoch,
      new ApiError(401, 'SESSION_REPLACED', '账号已在其他位置登录'),
    ),
  );
  await flush();
  assert.equal(document.querySelector('[data-private]'), null);
  assert.equal(sessionStorage.getItem('media-command:private'), null);
  assert.equal(sessionStorage.getItem('drama:credit-command:operations'), 'keep other platform');
  assert.match(
    document.querySelector('[role=alertdialog]')?.textContent || '',
    /账号已在其他位置登录.*当前工作台会话已结束/s,
  );
  assert.doesNotMatch(document.body.textContent || '', /运营/);
  assert.equal(
    fixture.requests.some((request) => request.method === 'logout'),
    false,
  );
  await act(async () =>
    [...document.querySelectorAll<HTMLButtonElement>('button')]
      .find((button) => button.textContent === '重新登录')!
      .click(),
  );
  await flush();
  assert.equal(document.querySelector('[role=alertdialog]'), null);
  fixture.service.current = async () => {
    throw new ApiError(401, 'SESSION_REPLACED', '账号已在其他位置登录');
  };
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(document.querySelector('[role=alertdialog]'), null);
  assert.equal(document.activeElement?.getAttribute('aria-label'), '登录邮箱');
});
test('an initial replaced operations Cookie also produces a popup; permission and network failures never claim replacement', async () => {
  await mount('/operations-login', (f) => {
    f.service.current = async () => {
      throw new ApiError(401, 'SESSION_REPLACED', '账号已在其他位置登录');
    };
  });
  assert.match(
    document.querySelector('[role=alertdialog]')?.textContent || '',
    /运营后台会话已结束/,
  );
  for (const status of [403, 0]) {
    await mount('/operations-login', (f) => {
      f.service.current = async () => {
        throw new ApiError(status, 'TEST_FAILURE', '测试故障');
      };
    });
    assert.equal(document.querySelector('[role=alertdialog]'), null);
    assert.equal(
      fixture.requests.some((request) => request.method === 'logout'),
      false,
    );
  }
});
test('restoring a page or foreground tab rechecks its Cookie and reveals replacement without a logout request', async () => {
  for (const event of ['pageshow', 'visibilitychange']) {
    await mount('/operations/users');
    fixture.service.current = async () => {
      throw new ApiError(401, 'SESSION_REPLACED', '其他位置已登录');
    };
    await act(async () => {
      (event === 'pageshow' ? window : document).dispatchEvent(new Event(event));
    });
    await flush();
    assert.equal(session.user, null);
    assert.ok(document.querySelector('[role=alertdialog]'));
    assert.equal(
      fixture.requests.some((request) => request.method === 'logout'),
      false,
    );
  }
});
test('first entry and retained expired or revoked Cookies show a quiet anonymous form on both platforms', async () => {
  for (const route of ['/login', '/operations-login']) {
    await mount(route, (f) => { f.user = null; });
    assert.equal(session.state, 'anonymous');
    assert.equal(session.message, '');
    for (const code of ['AUTH_REQUIRED', 'SESSION_REVOKED', 'SESSION_EXPIRED']) {
      await mount(route, (f) => {
        f.service.current = async () => { throw new ApiError(401, code, '旧Cookie技术原因'); };
        f.service.sessionMessage = () => '旧会话技术提示';
      });
      assert.equal(session.state, 'anonymous');
      assert.equal(session.message, '');
      assert.equal(document.querySelector('[data-private]'), null);
      assert.equal(document.querySelector('[role=alertdialog]'), null);
      await act(async () => window.dispatchEvent(new Event('focus')));
      await flush();
      assert.equal(session.message, '');
    }
  }
});
test('a protected request revocation clears the old account and drafts without a redundant login error', async () => {
  await mount('/credits');
  sessionStorage.setItem('media-command:private', 'old draft');
  const epoch = sessionBoundary.epoch;
  await act(async () => sessionBoundary.unauthorized(epoch, new ApiError(401, 'SESSION_REVOKED', '已撤销')));
  assert.equal(session.state, 'anonymous');
  assert.equal(session.message, '');
  assert.equal(session.user, null);
  assert.equal(sessionBoundary.epoch, epoch + 1);
  assert.equal(sessionStorage.getItem('media-command:private'), null);
});
test('disabled accounts and service failures retain actionable feedback; a password change retains its own notice', async () => {
  await mount('/login', (f) => {
    f.service.current = async () => { throw new ApiError(401, 'ACCOUNT_DISABLED', '账号已禁用'); };
  });
  assert.equal(session.message, '账号已禁用');
  await mount('/login', (f) => {
    f.service.current = async () => { throw new Error('服务暂不可用'); };
  });
  assert.equal(session.state, 'unavailable');
  assert.equal(session.message, '服务暂不可用');
  await mount('/credits');
  await act(async () => session.changePassword('fixture-current', 'fixture-new-password'));
  assert.equal(session.message, '密码已修改，请使用新密码重新登录。');
  fixture.service.current = async () => { throw new ApiError(401, 'SESSION_REVOKED', '旧Cookie已撤销'); };
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(session.message, '密码已修改，请使用新密码重新登录。');
});
test('a confirmed voluntary logout remains quiet when its retained old Cookie is checked again', async () => {
  await mount('/credits');
  await act(async () => session.logout());
  fixture.service.current = async () => {
    throw new ApiError(401, 'SESSION_REVOKED', '旧Cookie已撤销');
  };
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(session.state, 'anonymous');
  assert.equal(session.message, '');
  assert.equal(document.querySelector('[role=alertdialog]'), null);
  assert.equal(fixture.requests.filter((request) => request.method === 'logout').length, 1);
});
test('changing platform confirms that platform before rendering private content and fences an old logout response', async () => {
  let finish!: () => void;
  await mount('/credits', (f) => {
    f.service.logout = () => new Promise<void>((resolve) => (finish = resolve));
    f.service.current = async () =>
      sessionAudience() === 'workspace'
        ? f.user
        : { ...f.user!, identity: 'ops-only', username: '运营端本人' };
  });
  let pending!: Promise<void>;
  await act(async () => {
    pending = session.logout();
  });
  assert.equal(document.querySelector('[data-private]'), null);
  await act(async () => {
    window.location.hash = '#/operations/users';
  });
  await flush();
  assert.equal(session.user?.identity, 'ops-only');
  const epoch = sessionBoundary.epoch;
  await act(async () => {
    finish();
    await pending;
  });
  await flush();
  assert.equal(session.user?.identity, 'ops-only');
  assert.equal(sessionBoundary.epoch, epoch);
});
test('a same-platform tab message waits for an in-flight login instead of aborting its Cookie response', async () => {
  await mount('/login', (f) => {
    f.user = null;
  });
  let finish!: () => void, pending!: Promise<boolean>;
  await act(async () => {
    pending = session.authenticate(() => new Promise<void>((resolve) => (finish = resolve)));
  });
  const epoch = sessionBoundary.epoch;
  const peer = new Channel(sessionChangeChannel('workspace'));
  await act(async () => peer.postMessage('changed'));
  await flush();
  assert.equal(sessionBoundary.epoch, epoch);
  fixture.user = {
    identity: 'new-login',
    username: '新登录本人',
    email: 'new@example.invalid',
    operationsAllowed: false,
    roleLabel: '普通用户',
  };
  let successful = false;
  await act(async () => {
    finish();
    successful = await pending;
  });
  await flush();
  assert.equal(successful, true);
  assert.equal(session.user?.identity, 'new-login');
  assert.equal(
    fixture.requests.some((request) => request.method === 'logout'),
    false,
  );
});
test('theme changes, menu Escape and outside dismissal never select the logout action', async () => {
  await mount('/credits', undefined, true);
  fixture.requests.length = 0;
  const trigger = () => document.querySelector<HTMLButtonElement>('[aria-label="测试账号菜单"]')!;
  const open = async () => {
    await act(async () =>
      trigger().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
    );
    await flush();
    assert.ok(document.querySelector('[role=menu]'));
  };
  for (let round = 0; round < 3; round++) {
    await open();
    await act(async () => document.querySelector<HTMLElement>('[data-preference=theme]')!.click());
    await flush();
    assert.ok(session.user);
    await act(async () =>
      document.activeElement!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      ),
    );
    await flush();
    assert.equal(document.querySelector('[role=menu]'), null);
    assert.ok(session.user);
    await open();
    await act(async () =>
      document.body.dispatchEvent(new MouseEvent('pointerdown', { button: 0, bubbles: true })),
    );
    await flush();
    assert.equal(document.querySelector('[role=menu]'), null);
  }
  assert.equal(
    fixture.requests.some((request) => request.method === 'logout'),
    false,
  );
  assert.equal(
    fixture.requests.some((request) => request.method === 'login'),
    false,
  );
  assert.equal(session.state, 'authenticated');
  let workspaceSignals = 0,
    operationsSignals = 0;
  const workspace = new Channel(sessionChangeChannel('workspace')),
    operations = new Channel(sessionChangeChannel('operations'));
  workspace.onmessage = () => workspaceSignals++;
  operations.onmessage = () => operationsSignals++;
  await open();
  const logout = [...document.querySelectorAll<HTMLElement>('[role=menuitem]')].find(
    (item) => item.textContent === '退出登录',
  )!;
  await act(async () =>
    logout.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
  );
  await flush();
  assert.equal(fixture.requests.filter((request) => request.method === 'logout').length, 1);
  assert.equal(workspaceSignals, 1);
  assert.equal(operationsSignals, 0);
});
test('transport rejects a prior platform JSON, binary body, stream chunk and 401 before applying it', async () => {
  if (root) {
    await act(async () => root!.unmount());
    root = undefined;
  }
  for (const format of ['json', 'binary', 'stream', 'unauthorized'] as const) {
    window.location.hash = '#/projects';
    await flush();
    const boundary = new SessionBoundary();
    let rejectedSession = 0;
    boundary.onUnauthorized = () => rejectedSession++;
    let finish!: (value: any) => void;
    const delayed = new Promise<any>((resolve) => (finish = resolve)),
      chunks: Uint8Array[] = [];
    const client = createHttpClient(async () => {
      if (format === 'unauthorized') return await delayed;
      if (format === 'stream')
        return new Response(
          new ReadableStream({
            async start(controller) {
              controller.enqueue(await delayed);
              controller.close();
            },
          }),
        );
      const response = new Response('{}');
      if (format === 'json') response.json = () => delayed;
      return response;
    }, boundary);
    const result = (
      format === 'json'
        ? client.json('/api/private')
        : format === 'binary'
          ? client.binary('/api/private', undefined, async () => delayed)
          : format === 'stream'
            ? client.stream('/api/private', undefined, (chunk) => {
                chunks.push(chunk);
              })
            : client.json('/api/private')
    ).catch((error) => error);
    await Promise.resolve();
    window.location.hash = '#/operations/users';
    await flush();
    finish(
      format === 'unauthorized'
        ? Response.json({ code: 'SESSION_REPLACED' }, { status: 401 })
        : format === 'stream'
          ? new Uint8Array([1])
          : { owner: 'old platform' },
    );
    const error = await result;
    assert.equal(error.code, 'SESSION_CHANGED', format);
    assert.equal(rejectedSession, 0);
    assert.equal(chunks.length, 0);
  }
});
test('JSON, upload, binary, SSE and business clients select the current platform through the one Cookie transport', async () => {
  for (const audience of ['workspace', 'operations'] as const) {
    window.location.hash = audience === 'operations' ? '#/operations/models' : '#/projects';
    await flush();
    const calls: { url: string; options: RequestInit }[] = [];
    const request: typeof fetch = async (url, options) => {
      calls.push({ url: String(url), options: options! });
      return String(url).endsWith('/events')
        ? new Response('event: ready\ndata: {}\n\n', {
            headers: { 'Content-Type': 'text/event-stream' },
          })
        : Response.json({});
    };
    const client = createHttpClient(request, new SessionBoundary());
    await client.json('/api/regular', {
      headers: { 'X-Drama-Audience': 'wrong', 'Content-Type': 'application/json' },
    });
    const form = new FormData();
    form.set('file', new Blob(['fixture']), 'fixture.txt');
    await client.json('/api/upload', { method: 'POST', body: form });
    await client.binary('/api/bytes', undefined, (response) => response.blob());
    await client.stream('/api/events', { headers: { Accept: 'text/event-stream' } }, () => {});
    await createApi(request).getProject('owned');
    for (const { options } of calls) {
      assert.equal(new Headers(options.headers).get('X-Drama-Audience'), audience);
      assert.equal(options.credentials, 'same-origin');
      assert.equal(options.cache, 'no-store');
    }
    assert.equal(
      new Headers(calls[1].options.headers).has('Content-Type'),
      false,
      'multipart boundary belongs to the browser',
    );
    assert.equal(
      protectedResourceUrl('/api/auth/avatar?v=version'),
      '/api/auth/avatar?v=version&audience=' + audience,
    );
    assert.equal(
      protectedResourceUrl('/api/notifications/notice/images/image'),
      '/api/notifications/notice/images/image?audience=' + audience,
    );
    assert.equal(protectedResourceUrl('/demo/assets/public.svg'), '/demo/assets/public.svg');
    assert.equal(protectedResourceUrl('blob:fixture'), 'blob:fixture');
  }
});
