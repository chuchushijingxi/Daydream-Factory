import test, { after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/#/playlet/p/outline',
  pretendToBeVisual: true,
});
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'Element',
  'Node',
  'Event',
  'MouseEvent',
  'localStorage',
  'sessionStorage',
])
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
Object.defineProperty(globalThis, 'IS_REACT_ACT_ENVIRONMENT', { value: true, configurable: true });
const { createRoot } = await import('react-dom/client');
const { LanguageProvider } = await import('../src/contexts/LanguageContext');
const { useProjectEditing } = await import('../src/state/useProjectEditing');
const pause = () => new Promise((resolve) => setTimeout(resolve, 0));
test('project lease rechecks permissions, idles without renewing, fences faults and releases on leave', async () => {
  const previousFetch = globalThis.fetch,
    oldInterval = window.setInterval,
    oldClear = window.clearInterval,
    originalNow = Date.now;
  let interval: () => void = () => {},
    clock = originalNow(),
    access = 2,
    owned = true,
    fail = false,
    denied = false;
  const calls: { method: string; url: string; keepalive?: boolean }[] = [];
  let result: ReturnType<typeof useProjectEditing>;
  window.setInterval = ((fn: () => void, delay: number) => {
    assert.equal(delay, 30000);
    interval = fn;
    return 123;
  }) as typeof window.setInterval;
  window.clearInterval = () => {};
  Date.now = () => clock;
  globalThis.fetch = async (url, options) => {
    calls.push({
      url: String(url),
      method: options?.method || 'GET',
      keepalive: options?.keepalive,
    });
    if (String(url).endsWith('/permissions'))
      return denied
        ? Response.json({ code: 'NOT_FOUND', message: 'forbidden' }, { status: 404 })
        : Response.json({
            spaceId: 'team',
            ownerId: 'owner',
            creatorId: 'creator',
            access,
            canShare: false,
          });
    if (options?.method === 'DELETE') return Response.json({ released: true });
    return fail
      ? Response.json({ code: 'RUNTIME_UNAVAILABLE', message: 'Redis offline' }, { status: 503 })
      : Response.json({
          userId: owned ? 'editor' : 'another',
          username: 'Editor',
          expiresAt: new Date(clock + 90000).toISOString(),
          owned,
        });
  };
  const root = createRoot(document.getElementById('root')!);
  function Probe() {
    result = useProjectEditing('p', true);
    return <output>{result.reason}</output>;
  }
  const poll = async () => {
    await act(async () => {
      interval();
      await pause();
      await pause();
    });
  };
  try {
    await act(async () => {
      root.render(
        <LanguageProvider>
          <Probe />
        </LanguageProvider>,
      );
      await pause();
      await pause();
    });
    assert.equal(result!.readOnly, false);
    assert.ok(calls.some((c) => c.method === 'POST'));
    await poll();
    assert.ok(calls.some((c) => c.method === 'PUT'));
    clock += 31 * 60 * 1000;
    const renewals = calls.filter((c) => c.method === 'PUT').length;
    await poll();
    assert.equal(result!.readOnly, true);
    assert.equal(calls.filter((c) => c.method === 'PUT').length, renewals);
    assert.ok(calls.some((c) => c.method === 'DELETE' && c.keepalive));
    window.dispatchEvent(new window.Event('pointerdown'));
    await poll();
    assert.equal(result!.readOnly, false);
    owned = false;
    await poll();
    assert.equal(result!.readOnly, true);
    assert.match(result!.reason, /Editor/);
    owned = true;
    fail = true;
    await poll();
    assert.equal(result!.readOnly, true);
    assert.match(result!.reason, /Redis offline/);
    fail = false;
    access = 1;
    await poll();
    assert.equal(result!.permissions?.access, 1);
    assert.equal(result!.readOnly, true);
    denied = true;
    await poll();
    assert.equal(result!.denied, true);
    assert.equal(result!.permissions, null);
    const before = calls.filter((c) => c.method === 'DELETE').length;
    await act(async () => root.unmount());
    await pause();
    assert.ok(calls.filter((c) => c.method === 'DELETE').length > before);
  } finally {
    globalThis.fetch = previousFetch;
    window.setInterval = oldInterval;
    window.clearInterval = oldClear;
    Date.now = originalNow;
  }
});

after(() => dom.window.close());

test('hidden and expired leases block queued autosave until foreground permissions and acquisition finish', async () => {
  const previousFetch = globalThis.fetch;
  const originalNow = Date.now;
  const originalTimeout = globalThis.setTimeout;
  const originalWindowTimeout = window.setTimeout;
  const originalWindowClear = window.clearTimeout;
  let clock = originalNow();
  let expire = () => {};
  let persist = () => {};
  let hidden = false;
  let pending: ((value: Response) => void) | null = null;
  let waitForPermissions = false;
  let contentWrites = 0;
  let lease: ReturnType<typeof useProjectEditing>;
  const { useStoryboardAutosave } = await import('../src/state/useStoryboardAutosave');
  let autosave: ReturnType<typeof useStoryboardAutosave>;
  const permissions = {
    spaceId: 'team',
    ownerId: 'owner',
    creatorId: 'creator',
    access: 2,
    canShare: false,
  };
  const serviceStatus = { readOnly: false, capabilities: { storyboard: true } } as any;
  Object.defineProperty(document, 'hidden', { get: () => hidden, configurable: true });
  Date.now = () => clock;
  window.setTimeout = ((callback: () => void) => {
    expire = callback;
    return 500;
  }) as typeof window.setTimeout;
  window.clearTimeout = () => {};
  globalThis.setTimeout = ((callback: () => void, delay: number, ...args: any[]) => {
    if (delay === 600) {
      persist = callback;
      return 600;
    }
    return originalTimeout(callback, delay, ...args);
  }) as typeof globalThis.setTimeout;
  globalThis.fetch = async (url, options) => {
    if (String(url).endsWith('/permissions')) {
      if (waitForPermissions)
        return new Promise<Response>((resolve) => {
          pending = resolve;
        });
      return Response.json(permissions);
    }
    if (options?.method === 'DELETE') return Response.json({ released: true });
    if (String(url).endsWith('/editing'))
      return Response.json({
        userId: 'editor',
        username: 'Editor',
        owned: true,
        expiresAt: new Date(clock + 90000).toISOString(),
      });
    contentWrites++;
    throw new Error('expired lease must not submit content');
  };
  const root = createRoot(document.getElementById('root')!);
  function Probe() {
    lease = useProjectEditing('p', true);
    // 故意保留旧的可写status，验证persist还会查实时租约，而非依赖渲染及时性。
    autosave = useStoryboardAutosave(serviceStatus, async () => {}, lease.canWrite);
    return null;
  }
  try {
    await act(async () => {
      root.render(
        <LanguageProvider>
          <Probe />
        </LanguageProvider>,
      );
      await pause();
      await pause();
    });
    assert.equal(lease!.readOnly, false);
    const base = {
      sourceRanges: [],
      action: 'before',
      dialogues: [],
      camera: { size: '', angle: '', movement: '' },
      durationSeconds: null,
      designNotes: '',
      selectedAssets: [],
      videoPrompt: null,
    };
    await act(async () => {
      autosave!.drafts.current.set('p:shot', {
        projectId: 'p',
        episodeId: 'ep',
        sourceVersion: 1,
        revision: 1,
        base,
        value: { ...base, action: 'after' },
        conflict: false,
        message: '',
      });
      autosave!.changed();
    });
    clock += 91000;
    assert.equal(lease!.canWrite('p'), false);
    await act(async () => {
      persist();
      await pause();
    });
    assert.equal(contentWrites, 0, 'queued persist must reject the expired owned snapshot');
    await act(async () => {
      expire();
      await pause();
    });
    assert.equal(lease!.readOnly, true);
    await act(async () => {
      hidden = true;
      document.dispatchEvent(new window.Event('visibilitychange'));
      await pause();
    });
    waitForPermissions = true;
    await act(async () => {
      hidden = false;
      window.dispatchEvent(new window.Event('focus'));
      await pause();
    });
    assert.ok(pending, 'foreground starts a new permission check');
    assert.equal(lease!.readOnly, true);
    assert.equal(lease!.canWrite('p'), false);
    await act(async () => {
      persist();
      await pause();
    });
    assert.equal(
      contentWrites,
      0,
      'pending foreground permission check must keep content read-only',
    );
    await act(async () => {
      waitForPermissions = false;
      pending!(Response.json(permissions));
      await pause();
      await pause();
    });
    assert.equal(lease!.readOnly, false);
    await act(async () => {
      window.dispatchEvent(new window.Event('blur'));
      assert.equal(lease!.canWrite('p'), false);
      await pause();
    });
    assert.equal(lease!.readOnly, true);
    await act(async () => root.unmount());
    await pause();
  } finally {
    globalThis.fetch = previousFetch;
    Date.now = originalNow;
    globalThis.setTimeout = originalTimeout;
    window.setTimeout = originalWindowTimeout;
    window.clearTimeout = originalWindowClear;
    delete (document as any).hidden;
  }
});

test('switching projects preserves old drafts but only saves drafts covered by the current project lease', async () => {
  const previousFetch = globalThis.fetch;
  const originalTimeout = globalThis.setTimeout;
  const callbacks: (() => void)[] = [];
  const writes: string[] = [];
  let lease: ReturnType<typeof useProjectEditing>;
  const { useStoryboardAutosave, draftChanged } = await import(
    '../src/state/useStoryboardAutosave'
  );
  let autosave: ReturnType<typeof useStoryboardAutosave>;
  const base = {
    sourceRanges: [],
    action: 'before',
    dialogues: [],
    camera: { size: '', angle: '', movement: '' },
    durationSeconds: null,
    designNotes: '',
    selectedAssets: [],
    videoPrompt: null,
  };
  globalThis.setTimeout = ((callback: () => void, delay: number, ...args: any[]) => {
    if (delay === 600) {
      callbacks.push(callback);
      return 600;
    }
    return originalTimeout(callback, delay, ...args);
  }) as typeof globalThis.setTimeout;
  globalThis.fetch = async (url, options) => {
    const path = String(url);
    if (path.endsWith('/permissions'))
      return Response.json({
        spaceId: 'team',
        ownerId: 'owner',
        creatorId: 'creator',
        access: 2,
        canShare: false,
      });
    if (options?.method === 'DELETE') return Response.json({ released: true });
    if (path.endsWith('/editing'))
      return Response.json({
        userId: 'editor',
        username: 'Editor',
        owned: true,
        expiresAt: new Date(Date.now() + 90000).toISOString(),
      });
    writes.push(path);
    assert.equal(path, '/api/projects/b/storyboard/changes');
    const input = JSON.parse(options!.body as string);
    return Response.json({
      projectId: 'b',
      revision: input.revision + 1,
      invalidated: [],
      changes: [
        {
          entityType: 'shot',
          id: 'shot',
          after: {
            ...base,
            id: 'shot',
            sceneId: 'scene',
            valid: true,
            issues: [],
            ...input.operations[0].patch,
          },
        },
      ],
    });
  };
  const root = createRoot(document.getElementById('root')!);
  function Probe({ projectId }: { projectId: string }) {
    lease = useProjectEditing(projectId, true);
    autosave = useStoryboardAutosave(
      { readOnly: lease.readOnly, capabilities: { storyboard: true } } as any,
      async () => {},
      lease.canWrite,
    );
    return null;
  }
  const draft = (projectId: string) => ({
    projectId,
    episodeId: 'ep',
    sourceVersion: 1,
    revision: 1,
    base,
    value: { ...base, action: 'after' },
    conflict: false,
    message: '',
  });
  try {
    await act(async () => {
      root.render(
        <LanguageProvider>
          <Probe projectId="a" />
        </LanguageProvider>,
      );
      await pause();
      await pause();
    });
    assert.equal(lease!.canWrite('a'), true);
    await act(async () => {
      autosave!.drafts.current.set('a:shot', draft('a'));
      autosave!.changed();
    });
    const oldQueuedSave = callbacks.at(-1)!;
    assert.ok(oldQueuedSave);
    await act(async () => {
      root.render(
        <LanguageProvider>
          <Probe projectId="b" />
        </LanguageProvider>,
      );
      await pause();
      await pause();
    });
    assert.equal(lease!.canWrite('a'), false);
    assert.equal(lease!.canWrite('b'), true);
    await act(async () => {
      oldQueuedSave();
      await pause();
    });
    assert.equal(writes.length, 0, 'a queued save cannot consume project b lease');
    const count = callbacks.length;
    await act(async () => {
      autosave!.drafts.current.set('b:shot', draft('b'));
      autosave!.changed();
    });
    assert.ok(callbacks.length > count, 'old project a draft must not starve project b queue');
    await act(async () => {
      callbacks.at(-1)!();
      await pause();
      await pause();
    });
    assert.deepEqual(writes, ['/api/projects/b/storyboard/changes']);
    assert.equal(
      draftChanged(autosave!.drafts.current.get('a:shot')!),
      true,
      'old a draft stays recoverable',
    );
    assert.equal(autosave!.drafts.current.get('a:shot')!.conflict, false);
    assert.equal(draftChanged(autosave!.drafts.current.get('b:shot')!), false);
    await act(async () => root.unmount());
    await pause();
  } finally {
    globalThis.fetch = previousFetch;
    globalThis.setTimeout = originalTimeout;
  }
});
