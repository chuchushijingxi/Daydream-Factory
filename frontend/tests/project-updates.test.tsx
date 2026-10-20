import { test } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<div id="root"></div>', { url: 'http://127.0.0.1/demo/app/', pretendToBeVisual: true });
for (const key of ['window', 'document', 'navigator', 'HTMLElement', 'Event', 'localStorage'])
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const { createRoot } = await import('react-dom/client');
const { SessionOptionalContext } = await import('../src/state/SessionContext');
const { sessionBoundary } = await import('../src/api/session-boundary');
const { useProjectUpdates } = await import('../src/features/projects/useProjectUpdates');

test('project SSE coalesces hints, refreshes on focus and aborts at the account boundary', async () => {
  const streams: { controller: ReadableStreamDefaultController<Uint8Array>; signal: AbortSignal }[] = [];
  globalThis.fetch = async (url, options) => {
    assert.equal(new URL(String(url), 'http://127.0.0.1').pathname, '/api/projects/project-one/events');
    const body = new ReadableStream<Uint8Array>({ start(controller) {
      const signal = options!.signal!;
      streams.push({ controller, signal });
      signal.addEventListener('abort', () => { try { controller.close(); } catch {} }, { once: true });
      controller.enqueue(new TextEncoder().encode('event: project.ready\ndata: {}\n\n'));
    } });
    return new Response(body, { headers: { 'Content-Type': 'text/event-stream' } });
  };
  let changed = 0;
  function Probe() { useProjectUpdates('project-one', () => { changed++; }); return null; }
  const session = { user: { identity: 'owner-one' }, epoch: sessionBoundary.epoch } as NonNullable<React.ContextType<typeof SessionOptionalContext>>;
  const root = createRoot(document.getElementById('root')!);
  try {
    await act(async () => { root.render(<SessionOptionalContext.Provider value={session}><Probe /></SessionOptionalContext.Provider>); });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 140)); });
    assert.equal(streams.length, 1);
    assert.equal(changed, 1);
    for (let i = 0; i < 5; i++) streams[0].controller.enqueue(new TextEncoder().encode('event: project.changed\ndata: {}\n\n'));
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 140)); });
    assert.equal(changed, 2);
    await act(async () => { window.dispatchEvent(new Event('focus')); await new Promise(resolve => setTimeout(resolve,140)); });
    assert.equal(changed, 3);
    Object.defineProperty(document,'hidden',{value:true,configurable:true});
    streams[0].controller.enqueue(new TextEncoder().encode('event: project.changed\ndata: {}\n\n'));
    await act(async () => {window.dispatchEvent(new Event('focus')); await new Promise(resolve=>setTimeout(resolve,140));});
    assert.equal(changed,3,'hidden tab still rereads');
    Object.defineProperty(document,'hidden',{value:false,configurable:true});
    await act(async () => {document.dispatchEvent(new Event('visibilitychange'));window.dispatchEvent(new Event('focus'));await new Promise(resolve=>setTimeout(resolve,140));});
    assert.equal(changed,4,'visible/focus restoration did not coalesce');
    streams[0].controller.enqueue(new TextEncoder().encode('event: project.changed\ndata: {}\n\n'));
    sessionBoundary.reset();
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 140)); });
    assert.equal(streams[0].signal.aborted, true);
    assert.equal(changed, 4, 'old-account signal adopted after reset');
  } finally { await act(async () => root.unmount()); dom.window.close(); }
});
