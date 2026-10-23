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
const { useProjectPermissions } = await import('../src/state/useProjectPermissions');
const pause = () => new Promise((resolve) => setTimeout(resolve, 0));
const settle = async () => {
  await pause();
  await pause();
};
after(() => dom.window.close());

test('project permissions are read once per project; focus, visibility and time never change them', async () => {
  const previousFetch = globalThis.fetch,
    oldInterval = window.setInterval;
  let access = 2,
    denied = false;
  const calls: string[] = [];
  let intervals = 0;
  let result: ReturnType<typeof useProjectPermissions>;
  window.setInterval = (() => {
    intervals++;
    return 1;
  }) as unknown as typeof window.setInterval;
  globalThis.fetch = async (url, options) => {
    calls.push((options?.method || 'GET') + ' ' + String(url));
    if (denied)
      return Response.json({ code: 'NOT_FOUND', message: 'forbidden' }, { status: 404 });
    return Response.json({ spaceId: 'team', ownerId: 'owner', creatorId: 'creator', access, canShare: true });
  };
  const root = createRoot(document.getElementById('root')!);
  function Probe({ id }: { id: string }) {
    result = useProjectPermissions(id, true);
    return <output>{result.reason}</output>;
  }
  const render = (id: string) =>
    root.render(
      <LanguageProvider>
        <Probe id={id} />
      </LanguageProvider>,
    );
  try {
    act(() => render('p'));
    // 读取期间按可编辑渲染，不闪现只读。
    assert.equal(result!.readOnly, false);
    await act(settle);
    assert.equal(result!.readOnly, false);
    assert.equal(result!.permissions?.canShare, true);
    assert.deepEqual(calls, ['GET /api/projects/p/permissions']);
    await act(async () => {
      window.dispatchEvent(new window.Event('blur'));
      document.dispatchEvent(new window.Event('visibilitychange'));
      window.dispatchEvent(new window.Event('focus'));
      await settle();
    });
    assert.equal(calls.length, 1, 'focus and visibility must not trigger any request');
    assert.equal(intervals, 0, 'no polling timer');
    assert.equal(result!.readOnly, false);
    access = 1;
    await act(async () => {
      await result!.refresh();
      await settle();
    });
    assert.equal(result!.readOnly, true);
    assert.match(result!.reason, /仅可查看/);
    denied = true;
    await act(async () => {
      await result!.refresh();
      await settle();
    });
    assert.equal(result!.denied, true);
    assert.equal(result!.permissions, null);
    denied = false;
    access = 2;
    await act(async () => {
      render('q');
      await settle();
    });
    assert.equal(calls.at(-1), 'GET /api/projects/q/permissions');
    assert.equal(result!.denied, false);
    assert.equal(result!.readOnly, false);
    await act(async () => root.unmount());
  } finally {
    globalThis.fetch = previousFetch;
    window.setInterval = oldInterval;
  }
});

test('queued drafts are saved to their own project even after switching projects', async () => {
  const previousFetch = globalThis.fetch;
  const originalTimeout = globalThis.setTimeout;
  const callbacks: (() => void)[] = [];
  const writes: string[] = [];
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
    writes.push(path);
    const project = path.split('/')[3];
    const input = JSON.parse(options!.body as string);
    return Response.json({
      projectId: project,
      revision: input.revision + 1,
      invalidated: [],
      changes: [
        {
          entityType: 'shot',
          id: 'shot',
          after: { ...base, id: 'shot', sceneId: 'scene', valid: true, issues: [], ...input.operations[0].patch },
        },
      ],
    });
  };
  const root = createRoot(document.getElementById('root')!);
  function Probe() {
    autosave = useStoryboardAutosave({ readOnly: false, capabilities: { storyboard: true } } as any, async () => {});
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
          <Probe />
        </LanguageProvider>,
      );
      await settle();
    });
    await act(async () => {
      autosave!.drafts.current.set('a:shot', draft('a'));
      autosave!.drafts.current.set('b:shot', draft('b'));
      autosave!.changed();
    });
    for (let i = 0; i < 2; i++)
      await act(async () => {
        callbacks.at(-1)!();
        await settle();
      });
    assert.deepEqual(writes.sort(), [
      '/api/projects/a/storyboard/changes',
      '/api/projects/b/storyboard/changes',
    ]);
    assert.equal(draftChanged(autosave!.drafts.current.get('a:shot')!), false);
    assert.equal(draftChanged(autosave!.drafts.current.get('b:shot')!), false);
    await act(async () => root.unmount());
  } finally {
    globalThis.fetch = previousFetch;
    globalThis.setTimeout = originalTimeout;
  }
});
