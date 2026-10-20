import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { JSDOM } from 'jsdom';
import { ApiError } from '../src/api/http-error';
import { createAdministrationFixture } from './administration-fixture';
import type { NotificationsApi, NotificationMessage } from '../src/api/notifications-client';
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
    value: dom.window[key as keyof Window],
    configurable: true,
  });
Object.assign(globalThis, {
  getComputedStyle: dom.window.getComputedStyle,
  matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
  ResizeObserver: class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
  requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window),
  cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window),
  IS_REACT_ACT_ENVIRONMENT: true,
});
Object.defineProperty(globalThis, 'BroadcastChannel', { value: undefined, configurable: true });
const { createRoot } = await import('react-dom/client'),
  { SessionProvider } = await import('../src/state/SessionContext'),
  { LanguageProvider } = await import('../src/contexts/LanguageContext'),
  { NotificationCenter } = await import('../src/components/notifications/NotificationCenter'),
  { NotificationsPage } = await import('../src/pages/operations/NotificationsPage'),
  { SafeMarkdown, notificationUrl } = await import('../src/components/notifications/SafeMarkdown'),
  { sessionBoundary } = await import('../src/api/session-boundary'),
  { createHttpClient } = await import('../src/api/client'),
  { createNotificationsApi } = await import('../src/api/notifications-client');
const at = '2026-10-17T01:00:00Z',
  message: NotificationMessage = {
    id: 'notice-fixture',
    title: '虚构站内信',
    body: '## 新消息\n\n**仅测试内容**',
    audience: 'all',
    recipientUserId: '',
    recipientName: '',
    recipientEmail: '',
    status: 'draft',
    revision: 1,
    recipientCount: 0,
    createdAt: at,
    publishedAt: null,
    withdrawnAt: null,
    authorId: 'fixture_operator',
    authorName: '虚构发布人',
  };
function fixture() {
  let unread = 1;
  const calls: string[] = [];
  const client: NotificationsApi = {
    async watch(_changed, signal) {
      await new Promise<void>((resolve) =>
        signal.addEventListener('abort', () => resolve(), { once: true }),
      );
    },
    async withdraw(id, revision) {
      calls.push('withdraw');
      return {
        ...message,
        id,
        status: 'withdrawn',
        revision: revision + 1,
        publishedAt: at,
        withdrawnAt: at,
        recipientCount: 2,
      };
    },
    async uploadImage(id) {
      calls.push('uploadImage');
      return '/api/notifications/' + id + '/images/image-fixture';
    },
    async unread() {
      calls.push('unread');
      return unread;
    },
    async list(page = 1) {
      calls.push('list');
      return {
        items: [
          {
            id: message.id,
            title: message.title,
            summary: '仅测试摘要',
            publishedAt: at,
            readAt: unread ? null : at,
          },
        ],
        total: 1,
        page,
        pageSize: 20,
        unread,
      };
    },
    async detail(id) {
      calls.push('detail');
      return {
        id,
        title: message.title,
        summary: '仅测试摘要',
        publishedAt: at,
        readAt: unread ? null : at,
        body: message.body,
      };
    },
    async read(id) {
      calls.push('read');
      unread = 0;
      return { id, readAt: at };
    },
    async readAll() {
      calls.push('readAll');
      unread = 0;
      return { updated: 1, unread: 0 };
    },
    async operatorList(page = 1) {
      calls.push('operatorList');
      return { items: [], total: 0, page, pageSize: 20, unread: 0 };
    },
    async operatorDetail() {
      calls.push('operatorDetail');
      return message;
    },
    async command() {
      calls.push('command');
      return message;
    },
    async create(input) {
      calls.push('create');
      return { ...message, ...input };
    },
    async update(id, input) {
      calls.push('update');
      return { ...message, ...input, id, revision: input.revision + 1 };
    },
    async publish(id, revision) {
      calls.push('publish');
      return {
        ...message,
        id,
        status: 'published',
        revision: revision + 1,
        publishedAt: at,
        recipientCount: 2,
      };
    },
  };
  return { client, calls };
}
let root: ReturnType<typeof createRoot> | undefined;
const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10));
  });
};
async function mount(child: React.ReactNode) {
  if (root) await act(async () => root!.unmount());
  sessionBoundary.reset();
  const f = createAdministrationFixture();
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <SessionProvider service={f.service}>{child}</SessionProvider>
      </LanguageProvider>,
    ),
  );
  await flush();
}
const button = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (v) => v.getAttribute('aria-label') === label || v.textContent?.trim() === label,
  )!;
async function input(element: HTMLInputElement | HTMLTextAreaElement, value: string) {
  await act(async () => {
    const proto =
      element.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(element, value);
    element.dispatchEvent(new Event('input', { bubbles: true }));
  });
}
after(async () => {
  if (root) await act(async () => root!.unmount());
  dom.window.close();
});
test('Markdown preserves source newlines and only renders dedicated protected notification images', () => {
  const html = renderToStaticMarkup(
    <SafeMarkdown>
      {
        '# 标题\n\n第一行\n第二行\n\n**粗体**\n\n<script>alert(1)</script>\n\n[危险](javascript:alert(1))\n\n[外链](https://example.com/a)\n\n![隐私图](/api/files/secret/original)\n\n<img src="https://example.com/track" onerror="alert(1)">'
      }
    </SafeMarkdown>,
  );
  assert.match(html, /<strong>粗体<\/strong>/);
  assert.match(html, /第一行\n第二行/);
  const picture = renderToStaticMarkup(
    <SafeMarkdown>
      {'![通知插图](/api/notifications/notice-fixture/images/image-fixture)'}
    </SafeMarkdown>,
  );
  assert.match(
    picture,
    /src="\/api\/notifications\/notice-fixture\/images\/image-fixture\?audience=workspace"/,
  );
  assert.doesNotMatch(html, /<script|<img|javascript:|onerror=|\/api\/files/);
  assert.match(html, /rel="noopener noreferrer"/);
  assert.equal(notificationUrl('data:text/html,evil', 'href'), '');
  assert.equal(notificationUrl('https://example.com/track', 'src'), '');
});
test('API keeps authenticated scope and projects known fields from extensible responses', async () => {
  let payload: unknown = { unread: 2 };
  const requests: string[] = [];
  const client = createNotificationsApi(
    createHttpClient(async (url) => {
      requests.push(String(url));
      return Response.json(payload);
    }),
  );
  assert.equal(await client.unread(), 2);
  payload = { id: 'notice-a', readAt: at };
  await client.read('notice-a');
  assert.equal(requests.at(-1), '/api/notifications/notice-a/read');
  payload = { unread: 2, sessionToken: 'bad' };
  assert.equal(await client.unread(), 2);
  payload = {
    items: [
      { id: 'a', title: 't', summary: '摘要', publishedAt: at, readAt: null, body: 'private' },
    ],
    total: 1,
    page: 1,
    pageSize: 20,
    unread: 1,
  };
  assert.deepEqual((await client.list()).items, [{ id: 'a', title: 't', summary: '摘要', publishedAt: at, readAt: null }]);
});
test('the single all-message list shows a summary and time, loads no body until opened, then explicitly reads and restores focus', async () => {
  const f = fixture();
  await mount(<NotificationCenter client={f.client} />);
  assert.deepEqual(f.calls, ['unread']);
  await act(async () => button('通知，1条未读').click());
  await flush();
  assert.equal(document.querySelector('[role=tablist]'), null);
  assert.match(document.querySelector('.notification-list')?.textContent || '', /仅测试摘要/);
  assert.equal(document.querySelector('time')?.getAttribute('datetime'), at);
  assert.equal(f.calls.includes('detail'), false);
  assert.equal(f.calls.includes('read'), false);
  assert.ok(document.querySelector('.notification-card-symbol i'));
  await act(async () => document.querySelector<HTMLButtonElement>('.notification-card')!.click());
  await flush();
  assert.ok(document.querySelector('.notification-detail-dialog .notification-markdown strong'));
  assert.equal(document.querySelector('.notification-drawer .notification-detail'), null);
  assert.equal(document.querySelectorAll('[role=dialog]').length, 2);
  assert.ok(f.calls.indexOf('detail') < f.calls.indexOf('read'));
  await act(async () => button('关闭通知详情').click());
  await flush();
  assert.equal(document.querySelector('.notification-card-symbol i'), null);
  assert.equal(document.querySelector('[role=tab]'), null);
  await act(async () => button('关闭消息中心').click());
  await flush();
  assert.equal(document.querySelector('[role=dialog]'), null);
  assert.equal(document.activeElement?.getAttribute('aria-label'), '通知');
});
test('badge/list failures stay unavailable until an explicit successful retry', async () => {
  const f = fixture();
  f.client.unread = async () => {
    throw new Error('未读不可用');
  };
  f.client.list = async () => {
    throw new Error('列表不可用');
  };
  await mount(<NotificationCenter client={f.client} />);
  assert.ok(button('通知暂不可用'));
  await act(async () => button('通知暂不可用').click());
  await flush();
  assert.match(document.body.textContent || '', /列表不可用/);
  assert.equal(document.querySelector('.notification-empty'), null);
  f.client.list = async (page = 1) => ({ items: [], total: 0, page, pageSize: 20, unread: 0 });
  await act(async () => button('重新读取').click());
  await flush();
  assert.match(document.body.textContent || '', /暂无通知/);
});
test('late unread completion after detail cannot cross epoch or reopen drawer', async () => {
  const f = fixture();
  let resolve: (v: number) => void = () => {};
  let n = 0;
  f.client.unread = async () => (++n === 1 ? 1 : new Promise<number>((done) => (resolve = done)));
  await mount(<NotificationCenter client={f.client} />);
  await act(async () => button('通知，1条未读').click());
  await flush();
  await act(async () => document.querySelector<HTMLButtonElement>('.notification-card')!.click());
  await flush();
  await act(async () => button('关闭消息中心').click());
  await act(async () => {
    sessionBoundary.reset();
    resolve(99);
  });
  await flush();
  assert.equal(document.querySelector('[role=dialog]'), null);
  assert.equal(button('通知，99条未读'), undefined);
});
test('operator saves draft before publication; uncertain writes remain locked until command readback', async () => {
  const f = fixture();
  f.client.create = async () => {
    f.calls.push('create');
    throw new Error('提交结果未知');
  };
  await mount(<NotificationsPage client={f.client} />);
  await act(async () => button('新建通知').click());
  await input(document.querySelector<HTMLInputElement>('.op-notification-form input')!, '虚构标题');
  await input(document.querySelector<HTMLTextAreaElement>('textarea')!, '虚构正文');
  assert.equal(button('发布通知').disabled, true);
  await act(async () =>
    document
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(button('保存草稿').disabled, true);
  assert.equal(
    document.querySelector<HTMLInputElement>('.op-notification-form input')!.value,
    '虚构标题',
  );
  assert.equal(f.calls.filter((v) => v === 'create').length, 1);
  await act(async () => button('按原编号读取结果').click());
  await flush();
  assert.equal(f.calls.filter((v) => v === 'create').length, 1);
  assert.equal(f.calls.filter((v) => v === 'command').length, 1);
  assert.doesNotMatch(document.body.textContent || '', /已读取服务端保存的通知/);
  assert.equal(button('发布通知').disabled, false);
  await act(async () => button('发布通知').click());
  await flush();
  assert.equal(f.calls.filter((v) => v === 'publish').length, 0);
  assert.match(document.body.textContent || '', /确认发布通知/);
  await act(async () => button('确认发布').click());
  await flush();
  assert.match(document.body.textContent || '', /通知已发布/);
});

test('notification entry without authenticated provider has no UI or requests', () => {
  const f = fixture();
  assert.equal(renderToStaticMarkup(<NotificationCenter client={f.client} />), '');
  assert.deepEqual(f.calls, []);
});

test('older background unread response cannot overwrite a later foreground result', async () => {
  const f = fixture();
  let resolve: (n: number) => void = () => {};
  let count = 0;
  f.client.unread = async () => {
    ++count;
    if (count === 1) return 1;
    if (count === 2) return new Promise<number>((done) => (resolve = done));
    return 3;
  };
  await mount(<NotificationCenter client={f.client} />);
  await act(async () => window.dispatchEvent(new Event('focus')));
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.ok(button('通知，3条未读'));
  await act(async () => resolve(99));
  await flush();
  assert.ok(button('通知，3条未读'));
  assert.equal(button('通知，99条未读'), undefined);
});

test('SSE changes update the unread dot without refresh and withdrawal clears an open body before its reread', async () => {
  const f = fixture();
  let changed = () => {},
    available = false,
    read = false;
  f.client.watch = async (callback, signal) => {
    changed = callback;
    await new Promise<void>((resolve) =>
      signal.addEventListener('abort', () => resolve(), { once: true }),
    );
  };
  f.client.unread = async () => (available && !read ? 1 : 0);
  f.client.list = async (page = 1) => ({
    items: available
      ? [
          {
            id: message.id,
            title: message.title,
            summary: '仅测试摘要',
            publishedAt: at,
            readAt: read ? at : null,
          },
        ]
      : [],
    total: available ? 1 : 0,
    page,
    pageSize: 20,
    unread: available && !read ? 1 : 0,
  });
  f.client.detail = async (id) => {
    if (!available) throw new ApiError(404, 'NOT_FOUND', '通知不存在');
    return {
      id,
      title: message.title,
      summary: '仅测试摘要',
      publishedAt: at,
      readAt: read ? at : null,
      body: '第一行\n第二行\n\n![插图](/api/notifications/notice-fixture/images/image-fixture)',
    };
  };
  f.client.read = async (id) => {
    read = true;
    return { id, readAt: at };
  };
  await mount(<NotificationCenter client={f.client} />);
  assert.equal(document.querySelector('.notification-dot'), null);
  available = true;
  await act(async () => {
    changed();
    await new Promise((resolve) => setTimeout(resolve, 130));
  });
  await flush();
  assert.ok(document.querySelector('.notification-dot'));
  assert.ok(button('通知，1条未读'));
  await act(async () => button('通知，1条未读').click());
  await flush();
  await act(async () => document.querySelector<HTMLButtonElement>('.notification-card')!.click());
  await flush();
  assert.match(document.querySelector('.notification-detail')?.textContent || '', /第一行\n第二行/);
  assert.ok(document.querySelector('.notification-detail img'));
  assert.equal(document.querySelector('.notification-dot'), null);
  available = false;
  await act(async () => {
    changed();
    await new Promise((resolve) => setTimeout(resolve, 130));
  });
  await flush();
  assert.equal(document.querySelector('.notification-detail img'), null);
  assert.doesNotMatch(
    document.querySelector('.notification-drawer')?.textContent || '',
    /第一行|第二行/,
  );
  assert.match(
    document.querySelector('.notification-drawer')?.textContent || '',
    /已下线|不再可见/,
  );
  assert.equal(document.querySelector('.notification-card'), null);
});

test('notifications reconnect with a fresh read and stop their stream when the account boundary unmounts', async () => {
  const f = fixture();
  let count = 0,
    stopped: AbortSignal | undefined;
  f.client.watch = async (changed, signal) => {
    stopped = signal;
    if (++count === 1) throw new Error('network offline');
    changed();
    await new Promise<void>((resolve) =>
      signal.addEventListener('abort', () => resolve(), { once: true }),
    );
  };
  await mount(<NotificationCenter client={f.client} />);
  await act(async () => button('通知，1条未读').click());
  await flush();
  assert.match(document.body.textContent || '', /通知实时同步暂不可用/);
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 1150));
  });
  await flush();
  assert.equal(count, 2);
  assert.doesNotMatch(document.body.textContent || '', /通知实时同步暂不可用/);
  await mount(<div>新账号边界</div>);
  assert.equal(stopped?.aborted, true);
});

test('operator list shows author and lifecycle, while publishing and withdrawal each require explicit confirmation', async () => {
  const f = fixture();
  let saved = { ...message };
  f.client.operatorList = async (page = 1) => ({
    items: [saved],
    total: 1,
    page,
    pageSize: 20,
    unread: 0,
  });
  f.client.operatorDetail = async () => saved;
  f.client.publish = async (id, revision) => {
    f.calls.push('publish');
    saved = {
      ...saved,
      id,
      status: 'published',
      revision: revision + 1,
      publishedAt: at,
      recipientCount: 2,
    };
    return saved;
  };
  f.client.withdraw = async (id, revision) => {
    f.calls.push('withdraw');
    saved = { ...saved, id, status: 'withdrawn', revision: revision + 1, withdrawnAt: at };
    return saved;
  };
  await mount(<NotificationsPage client={f.client} />);
  assert.match(
    document.querySelector('.op-notification-table')?.textContent || '',
    /发布人.*虚构发布人/s,
  );
  await act(async () => button('查看通知 虚构站内信').click());
  await flush();
  assert.ok(document.querySelector('.notification-composer-preview'));
  await act(async () => button('发布通知').click());
  await flush();
  assert.equal(f.calls.includes('publish'), false);
  await act(async () => button('确认发布').click());
  await flush();
  assert.match(document.querySelector('.op-notification-table')?.textContent || '', /已发布/);
  assert.doesNotMatch(
    document.querySelector('.notification-editor-dialog')?.textContent || '',
    /撤回下线/,
  );
  await act(async () => button('关闭').click());
  await flush();
  await act(async () => button('撤回下线').click());
  await flush();
  assert.equal(f.calls.includes('withdraw'), false);
  await act(async () => button('确认下线').click());
  await flush();
  assert.match(document.querySelector('.op-notification-table')?.textContent || '', /已下线/);
  assert.equal(f.calls.filter((call) => call === 'withdraw').length, 1);
  assert.equal(button('撤回下线'), undefined);
});

test('notification composer previews while typing and inserts an uploaded image at the saved caret without publishing', async () => {
  const f = fixture();
  await mount(<NotificationsPage client={f.client} />);
  await act(async () => button('新建通知').click());
  await input(document.querySelector<HTMLInputElement>('.op-notification-form input')!, '插图验证');
  await input(document.querySelector<HTMLTextAreaElement>('textarea')!, '第一行\n第二行');
  assert.match(
    document.querySelector('.notification-composer-preview')?.textContent || '',
    /第一行\n第二行/,
  );
  assert.ok(button('上传图片').disabled);
  await act(async () =>
    document
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const textarea = document.querySelector<HTMLTextAreaElement>('textarea')!;
  textarea.setSelectionRange(0, 0);
  const file = document.querySelector<HTMLInputElement>('input[type=file]')!;
  Object.defineProperty(file, 'files', {
    value: [new dom.window.File(['fake-image'], 'sample.png', { type: 'image/png' })],
    configurable: true,
  });
  await act(async () => file.dispatchEvent(new Event('change', { bubbles: true })));
  await flush();
  assert.match(
    textarea.value,
    /!\[sample.png\]\(\/api\/notifications\/notice-fixture\/images\/image-fixture\)/,
  );
  assert.ok(document.querySelector('.notification-composer-preview img'));
  assert.equal(f.calls.filter((call) => call === 'uploadImage').length, 1);
  assert.equal(f.calls.includes('publish'), false);
  assert.ok(button('发布通知').disabled, 'new image must be explicitly saved before publication');
});

test('notification stream shares Cookie transport and exact invalidation frames, while images retain canonical and operator URLs', async () => {
  let request: { url: string; options: RequestInit } | undefined;
  const stream = createNotificationsApi(
    createHttpClient(async (url, options) => {
      request = { url: String(url), options: options! };
      return new Response(
        'event: notifications.ready\ndata: {}\n\n: heartbeat\n\nevent: notifications.changed\ndata: {}\n\n',
        { headers: { 'Content-Type': 'text/event-stream' } },
      );
    }),
  );
  let changed = 0;
  await assert.rejects(
    stream.watch(() => changed++, new AbortController().signal),
    (error) => (error as ApiError).code === 'STREAM_CLOSED',
  );
  assert.equal(request?.url, '/api/notifications/events');
  assert.equal(request?.options.credentials, 'same-origin');
  assert.equal(changed, 2);
  const forbidden = createNotificationsApi(
    createHttpClient(
      async () =>
        new Response('event: notifications.changed\ndata: {"body":"private"}\n\n', {
          headers: { 'Content-Type': 'text/event-stream' },
        }),
    ),
  );
  await assert.rejects(forbidden.watch(() => changed++, new AbortController().signal));
  assert.equal(changed, 2);
  let payload: unknown = {
    id: 'image-fixture',
    url: '/api/notifications/notice-fixture/images/image-fixture',
    previewUrl: '/api/operations/notifications/notice-fixture/images/image-fixture',
    contentType: 'image/png',
    width: 96,
    height: 64,
    sizeBytes: 100,
  };
  const api = createNotificationsApi(
    createHttpClient(async (url, options) => {
      request = { url: String(url), options: options! };
      return Response.json(payload);
    }),
  );
  const file = new dom.window.File(['image'], 'sample.png', { type: 'image/png' });
  assert.equal(
    await api.uploadImage('notice-fixture', file, new AbortController().signal),
    '/api/notifications/notice-fixture/images/image-fixture',
  );
  assert.equal(request?.url, '/api/operations/notifications/notice-fixture/images');
  assert.equal(request?.options.body, file);
  assert.equal(new Headers(request?.options.headers).get('Content-Type'), 'image/png');
  assert.equal(new Headers(request?.options.headers).get('X-Drama-Audience'), 'workspace');
  payload = { ...(payload as object), previewUrl: '/api/files/secret/original' };
  await assert.rejects(api.uploadImage('notice-fixture', file, new AbortController().signal));
  const text = '![test](/api/notifications/notice-fixture/images/image-fixture)';
  const preview = renderToStaticMarkup(
    <SafeMarkdown messageId="notice-fixture" operatorPreview>
      {text}
    </SafeMarkdown>,
  );
  assert.match(
    preview,
    /src="\/api\/operations\/notifications\/notice-fixture\/images\/image-fixture\?audience=workspace"/,
  );
  const foreign = renderToStaticMarkup(
    <SafeMarkdown messageId="other-message" operatorPreview>
      {text}
    </SafeMarkdown>,
  );
  assert.doesNotMatch(foreign, /<img/);
});

test('withdrawal with an unknown commit locks a second action and reads the original message instead of resending', async () => {
  const f = fixture();
  let saved: NotificationMessage = { ...message, status: 'published', publishedAt: at };
  f.client.operatorList = async (page = 1) => ({
    items: [saved],
    total: 1,
    page,
    pageSize: 20,
    unread: 0,
  });
  f.client.operatorDetail = async () => saved;
  f.client.withdraw = async () => {
    f.calls.push('withdraw');
    saved = { ...saved, status: 'withdrawn', withdrawnAt: at, revision: 2 };
    throw new ApiError(503, 'NOTIFICATION_COMMIT_UNKNOWN', '下线结果未确认');
  };
  await mount(<NotificationsPage client={f.client} />);
  await act(async () => button('撤回下线').click());
  await act(async () => button('确认下线').click());
  await flush();
  assert.equal(f.calls.filter((call) => call === 'withdraw').length, 1);
  assert.match(document.body.textContent || '', /下线结果未确认/);
  assert.ok(button('撤回下线').disabled);
  await act(async () => button('核对待确认通知').click());
  await flush();
  await act(async () => button('按原编号读取结果').click());
  await flush();
  assert.equal(f.calls.filter((call) => call === 'withdraw').length, 1);
  assert.match(document.querySelector('.op-notification-table')?.textContent || '', /已下线/);
  assert.equal(button('撤回下线'), undefined);
});

test('an old detail response cannot restore a body or image after the withdrawal signal', async () => {
  const f = fixture();
  let changed = () => {},
    withdrawn = false,
    resolveOld!: (v: any) => void,
    details = 0;
  f.client.watch = async (callback, signal) => {
    changed = callback;
    await new Promise<void>((resolve) =>
      signal.addEventListener('abort', () => resolve(), { once: true }),
    );
  };
  f.client.detail = async () => {
    if (++details === 1)
      return new Promise((resolve) => {
        resolveOld = resolve;
      });
    throw new ApiError(404, 'NOT_FOUND', '下线');
  };
  const originalList = f.client.list;
  f.client.list = async (page, filter) =>
    withdrawn
      ? { items: [], total: 0, page: 1, pageSize: 20, unread: 0 }
      : originalList(page, filter);
  await mount(<NotificationCenter client={f.client} />);
  await act(async () => button('通知，1条未读').click());
  await flush();
  await act(async () => document.querySelector<HTMLButtonElement>('.notification-card')!.click());
  await flush();
  withdrawn = true;
  await act(async () => {
    changed();
    await new Promise((resolve) => setTimeout(resolve, 130));
  });
  await flush();
  await act(async () =>
    resolveOld({
      id: message.id,
      title: message.title,
      summary: '仅测试摘要',
      publishedAt: at,
      readAt: null,
      body: '迟到正文\n\n![迟到图片](/api/notifications/notice-fixture/images/image-fixture)',
    }),
  );
  await flush();
  assert.doesNotMatch(
    document.querySelector('.notification-drawer')?.textContent || '',
    /迟到正文/,
  );
  assert.equal(document.querySelector('.notification-drawer img'), null);
  assert.equal(f.calls.filter((call) => call === 'read').length, 0);
});

test('all-list pagination uses server summaries without reading hidden full bodies', async () => {
  const f = fixture(),
    queries: { page: number; status: string }[] = [];
  f.client.list = async (page = 1, status = 'all') => {
    queries.push({ page, status });
    return {
      items: [
        {
          id: 'page-' + page,
          title: '通知 ' + page,
          summary: '服务端安全摘要 ' + page,
          publishedAt: at,
          readAt: page === 1 ? null : at,
        },
      ],
      total: 21,
      page,
      pageSize: 20,
      unread: 1,
    };
  };
  await mount(<NotificationCenter client={f.client} />);
  await act(async () => button('通知，1条未读').click());
  await flush();
  assert.match(document.querySelector('.notification-card-summary')?.textContent || '', /摘要 1/);
  await act(async () => button('通知下一页').click());
  await flush();
  assert.match(document.querySelector('.notification-card-summary')?.textContent || '', /摘要 2/);
  assert.equal(document.querySelector('[role=tablist]'), null);
  assert.deepEqual(queries, [
    { page: 1, status: 'all' },
    { page: 2, status: 'all' },
  ]);
  assert.equal(f.calls.includes('detail'), false);
  assert.equal(f.calls.includes('read'), false);
});

test('strict notification receipts accept only bounded summary and current recipientName, with identity retained for writes', async () => {
  let payload: unknown = {
    items: [{ id: 'a', title: 't', summary: '安全摘要', publishedAt: at, readAt: null }],
    total: 1,
    page: 1,
    pageSize: 20,
    unread: 1,
  };
  const api = createNotificationsApi(createHttpClient(async () => Response.json(payload)));
  assert.equal((await api.list()).items[0].summary, '安全摘要');
  payload = {
    items: [{ id: 'a', title: 't', summary: '字'.repeat(121), publishedAt: at, readAt: null }],
    total: 1,
    page: 1,
    pageSize: 20,
    unread: 1,
  };
  await assert.rejects(api.list(), /不完整/);
  payload = {
    ...message,
    audience: 'user',
    recipientUserId: 'internal-target',
    recipientName: '接收用户名',
    recipientEmail: 'recipient@example.test',
  };
  const target = await api.operatorDetail(message.id);
  assert.equal(target.recipientName, '接收用户名');
  assert.equal(target.recipientEmail, 'recipient@example.test');
  payload = {
    ...message,
    audience: 'user',
    recipientUserId: 'internal-target',
    recipientName: '接收用户名',
    recipientEmail: undefined,
  };
  await assert.rejects(api.operatorDetail(message.id), /不完整/);
  payload = { ...message, audience: 'user', recipientUserId: 'internal-target' };
  await assert.rejects(api.operatorDetail(message.id));
});

test('operator target scope opens recipient dialog and keeps identities hidden', async () => {
  const f = fixture(),
    saved: NotificationMessage = {
      ...message,
      status: 'published',
      publishedAt: at,
      audience: 'user',
      recipientUserId: 'private_internal_target_very_long_id',
      recipientName: '接收用户名',
      recipientEmail: 'recipient@example.test',
    };
  f.client.operatorList = async (page = 1) => ({
    items: [saved],
    total: 1,
    page,
    pageSize: 20,
    unread: 0,
  });
  f.client.operatorDetail = async () => saved;
  await mount(<NotificationsPage client={f.client} />);
  assert.doesNotMatch(
    document.querySelector('.op-notification-table')?.textContent || '',
    /接收用户名|位接收人/,
  );
  await act(async () => button('查看接收用户 虚构站内信').click());
  await flush();
  assert.match(
    document.querySelector('.notification-recipients-dialog')?.textContent || '',
    /接收用户.*虚构站内信.*接收用户名/,
  );
  const recipientsTable = document.querySelector<HTMLTableElement>('.notification-recipient-list')!;
  assert.equal(recipientsTable.tagName, 'TABLE');
  assert.deepEqual(
    [...recipientsTable.querySelectorAll('th')].map((cell) => cell.textContent),
    ['用户名', '邮箱'],
  );
  assert.deepEqual(
    [...recipientsTable.querySelectorAll('tbody td')].map((cell) => cell.textContent),
    ['接收用户名', 'recipient@example.test'],
  );
  assert.doesNotMatch(document.body.textContent || '', /private_internal_target/);
  await act(async () => button('关闭').click());
  await flush();
  assert.equal(document.querySelector('.notification-recipients-dialog'), null);
  assert.equal(document.activeElement?.getAttribute('aria-label'), '查看接收用户 虚构站内信');
  await act(async () => button('查看通知 虚构站内信').click());
  await flush();
  assert.match(
    document.querySelector('.notification-published-body')?.textContent || '',
    /指定用户 接收用户名/,
  );
  assert.doesNotMatch(
    document.body.textContent || '',
    /private_internal_target|发布后的正文保持原样|已读取服务端保存的通知/,
  );
  assert.doesNotMatch(
    document.querySelector('.notification-editor-dialog')?.textContent || '',
    /撤回下线/,
  );
  await act(async () => button('关闭').click());
  await flush();
  await act(async () => button('撤回下线').click());
  await flush();
  assert.equal(document.querySelector('.notification-editor-dialog'), null);
  assert.match(
    document.querySelector('.notification-confirm-dialog')?.textContent || '',
    /接收用户名/,
  );
  assert.doesNotMatch(
    document.querySelector('.notification-confirm-dialog')?.textContent || '',
    /private_internal_target/,
  );
  await act(async () => button('取消').click());
  await flush();
  assert.equal(document.querySelector('.notification-confirm-dialog'), null);
  assert.equal(f.calls.includes('withdraw'), false);
  assert.equal(document.activeElement?.getAttribute('aria-label'), '撤回通知 虚构站内信');
});

test('the paged recipient picker displays usernames and selects a stable internal identity without exposing it', async () => {
  const { NotificationRecipientPicker } = await import(
    '../src/pages/operations/NotificationRecipientPicker'
  );
  let choice: { id: string; name: string } | undefined;
  await mount(
    <NotificationRecipientPicker
      value=""
      name=""
      disabled={false}
      onChange={(id, name) => {
        choice = { id, name };
      }}
    />,
  );
  await act(async () => button('选择接收用户').click());
  await flush();
  assert.match(
    document.querySelector('.notification-recipient-options')?.textContent || '',
    /虚构用户 A/,
  );
  assert.doesNotMatch(
    document.querySelector('.notification-recipient-menu')?.textContent || '',
    /fixture_a/,
  );
  await act(async () =>
    document.querySelector<HTMLButtonElement>('.notification-recipient-options button')!.click(),
  );
  await flush();
  assert.deepEqual(choice, { id: 'fixture_a', name: '虚构用户 A' });
  assert.equal(document.querySelector('.notification-recipient-menu'), null);
});

test('Escape closes only the recipient detail modal and returns focus to the original notification', async () => {
  const f = fixture();
  await mount(<NotificationCenter client={f.client} />);
  await act(async () => button('通知，1条未读').click());
  await flush();
  await act(async () => document.querySelector<HTMLButtonElement>('.notification-card')!.click());
  await flush();
  await act(async () =>
    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    ),
  );
  await flush();
  assert.equal(document.querySelector('.notification-detail-dialog'), null);
  assert.ok(document.querySelector('.notification-drawer'));
  assert.equal(document.activeElement?.className, 'notification-card');
});

test('all-account notices never show a fixed recipient count or clickable recipient list', async () => {
  const f = fixture(),
    saved: NotificationMessage = {
      ...message,
      status: 'published',
      publishedAt: at,
      recipientCount: 999,
    };
  f.client.operatorList = async (page = 1) => ({
    items: [saved],
    total: 1,
    page,
    pageSize: 20,
    unread: 0,
  });
  f.client.operatorDetail = async () => saved;
  await mount(<NotificationsPage client={f.client} />);
  const table = document.querySelector('.op-notification-table')!;
  assert.match(table.textContent || '', /全部用户/);
  assert.doesNotMatch(table.textContent || '', /999|位接收人/);
  assert.equal(table.querySelector('.notification-scope-link'), null);
  await act(async () => button('查看通知 虚构站内信').click());
  await flush();
  assert.doesNotMatch(
    document.querySelector('.notification-published-body')?.textContent || '',
    /999|位接收人/,
  );
});
