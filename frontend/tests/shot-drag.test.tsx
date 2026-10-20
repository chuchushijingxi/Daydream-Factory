import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import type { StoryShot } from '../src/api/storyboard-types';
import { ShotStrip } from '../src/pages/production/ShotStrip';

const dom = new JSDOM('<div id="root"></div>', { pretendToBeVisual: true });
for (const key of ['window', 'document', 'navigator', 'HTMLElement', 'Element', 'Node', 'Event', 'MouseEvent', 'KeyboardEvent']) {
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
let reduced = false;
globalThis.matchMedia = () => ({ matches: reduced }) as MediaQueryList;
const { createRoot } = await import('react-dom/client');
let root: ReturnType<typeof createRoot>;
const shots = ['a', 'b', 'c'].map(id => ({ id, action: id, videoPrompt: null, valid: true, issues: [] })) as StoryShot[];
let calls: { ids: string[]; revision: number }[], selected: string[], finish: (success: boolean) => void;
let revision = 8, disabled = false, order = shots;
const render = async () => {
  await act(() => root.render(<ShotStrip shots={order} revision={revision} disabled={disabled}
    onSelect={id => selected.push(id)} onInsert={() => assert.fail('unexpected insert')}
    onDelete={() => assert.fail('unexpected delete')}
    onReorder={(ids, expected) => {
      calls.push({ ids, revision: expected });
      return new Promise<boolean>(resolve => { finish = resolve; });
    }} />));
  const rect = (left: number, width: number) => ({ left, right: left + width, top: 20, bottom: 100, width, height: 80, x: left, y: 20, toJSON() {} });
  document.querySelector('.sb-strip-track')!.getBoundingClientRect = () => rect(0, 500);
  document.querySelectorAll<HTMLElement>('.sb-strip-card').forEach((card, i) => {
    card.getBoundingClientRect = () => rect(i * 134, 120);
    card.querySelector('button')!.getBoundingClientRect = card.getBoundingClientRect;
  });
};
async function mount() {
  if (root) await act(() => root.unmount());
  root = createRoot(document.getElementById('root')!);
  revision = 8; disabled = false; order = shots; calls = []; selected = []; reduced = false;
  await render();
}
const source = () => document.querySelector<HTMLButtonElement>('.sb-strip-select')!;
const floating = () => document.querySelector<HTMLElement>('.sb-strip-floating');
async function pointer(target: EventTarget, type: string, x: number, y = 60, pointerId = 1) {
  await act(() => {
    const event = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, button: 0 });
    Object.defineProperty(event, 'pointerId', { value: pointerId });
    target.dispatchEvent(event);
  });
}
const begin = async () => { await pointer(source(), 'pointerdown', 60); await pointer(window, 'pointermove', 194); };
const waitSettle = async () => { await act(async () => { await new Promise(resolve => setTimeout(resolve, 175)); }); };

test('pointer threshold retains click; active drag previews without saving and commits once on release', async () => {
  await mount();
  await pointer(source(), 'pointerdown', 60);
  await pointer(window, 'pointermove', 64);
  assert.equal(floating(), null);
  await pointer(window, 'pointerup', 64);
  await act(() => source().click());
  assert.deepEqual(selected, ['a']);
  await begin();
  assert.match(floating()!.style.transform, /rotate\(1.5deg\) scale\(1.03\)/);
  assert.equal(document.querySelector<HTMLElement>('.sb-strip-card')!.style.transform, 'translate3d(134px,0,0)');
  assert.equal(calls.length, 0);
  await pointer(window, 'pointermove', 184);
  assert.match(floating()!.style.transform, /rotate\(-1.5deg\)/);
  await pointer(window, 'pointerup', 194);
  assert.equal(floating()!.dataset.settling, 'true');
  await pointer(window, 'pointerup', 194);
  await act(() => source().click());
  assert.deepEqual(selected, ['a'], 'release must not select a shot');
  assert.deepEqual(calls, [{ ids: ['b', 'a', 'c'], revision: 8 }]);
  await waitSettle();
  assert.equal(floating(), null);
  assert.match(document.querySelector<HTMLElement>('.sb-strip-card')!.style.transform, /134px/);
  order = [shots[1], shots[0], shots[2]]; revision++;
  await render();
  await act(async () => { finish(true); });
  assert.equal(document.querySelector<HTMLElement>('.sb-strip-card')!.style.transform, '');
});

test('failed save restores authoritative order and allows a new gesture', async () => {
  await mount(); await begin(); await pointer(window, 'pointerup', 194);
  await act(async () => { finish(false); }); await waitSettle();
  assert.equal(document.querySelector<HTMLElement>('.sb-strip-card')!.style.transform, '');
  assert.deepEqual([...document.querySelectorAll('.sb-strip-select')].map(el => el.textContent), ['待生成1', '待生成2', '待生成3']);
  await begin(); await pointer(window, 'pointerup', 194);
  assert.equal(calls.length, 2);
  await act(async () => { finish(false); });
});

test('escape, pointer cancellation, blur and outside release do not save', async () => {
  for (const reason of ['Escape', 'pointercancel', 'blur', 'outside']) {
    await mount(); await begin();
    if (reason === 'Escape') await act(() => window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })));
    else if (reason === 'outside') await pointer(window, 'pointerup', 194, 180);
    else if (reason === 'pointercancel') await pointer(window, 'pointercancel', 194);
    else await act(() => window.dispatchEvent(new Event('blur')));
    assert.equal(calls.length, 0, reason);
    await waitSettle();
    assert.equal(floating(), null, reason);
    assert.equal(document.querySelector<HTMLElement>('.sb-strip-card')!.style.transform, '', reason);
  }
});

test('revision or write protection changes cancel a gesture; secondary pointers cannot cancel it', async () => {
  for (const protection of ['revision', 'disabled']) {
    await mount(); await begin();
    await pointer(window, 'pointercancel', 194, 60, 2);
    assert.ok(floating());
    if (protection === 'revision') revision++; else disabled = true;
    await render(); await pointer(window, 'pointerup', 194);
    assert.equal(floating(), null);
    assert.equal(calls.length, 0);
  }
});

test('edge drag scrolls overflow and cancellation stops scrolling', async () => {
  await mount();
  const track = document.querySelector<HTMLElement>('.sb-strip-track')!;
  Object.defineProperties(track, { scrollWidth: { value: 900 }, clientWidth: { value: 500 } });
  await begin(); await pointer(window, 'pointermove', 490);
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 40)); });
  assert.ok(track.scrollLeft > 0);
  await pointer(window, 'pointercancel', 490);
  const left = track.scrollLeft;
  await waitSettle();
  assert.equal(track.scrollLeft, left);
  assert.equal(calls.length, 0);
});

test('reduced motion keeps sorting and omits rotation and settling; keyboard stays available', async () => {
  await mount(); reduced = true;
  await begin();
  assert.match(floating()!.style.transform, /rotate\(0deg\)/);
  await pointer(window, 'pointerup', 194);
  assert.equal(floating(), null);
  await act(async () => { finish(false); });
  await act(() => source().dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', altKey: true, bubbles: true })));
  assert.deepEqual(calls[1], { ids: ['b', 'a', 'c'], revision: 8 });
  await act(async () => { finish(false); });
});

after(async () => { if (root) await act(() => root.unmount()); dom.window.close(); });
