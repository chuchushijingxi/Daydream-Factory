import { test, after, mock } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import React, { act } from 'react';
const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/',
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
  'KeyboardEvent',
])
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
globalThis.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
globalThis.innerWidth = 390;
globalThis.innerHeight = 844;
globalThis.addEventListener = dom.window.addEventListener.bind(dom.window);
globalThis.removeEventListener = dom.window.removeEventListener.bind(dom.window);
const { createRoot } = await import('react-dom/client');
const { Button } = await import('../src/components/ui/button');
const { ControlTooltip } = await import('../src/components/ui/control-tooltip');
const { Toaster, toast } = await import('../src/components/ui/sonner');
const root = createRoot(document.getElementById('root'));
mock.timers.enable({ apis: ['setTimeout', 'Date'] });
const tick = async (ms: number) => {
  await act(() => mock.timers.tick(ms));
};
after(async () => {
  await act(() => root.unmount());
  mock.timers.reset();
  dom.window.close();
});
test('busy controls reject a second action and preserve the original label', async () => {
  let clicks = 0;
  await act(() =>
    root.render(
      <Button busy onClick={() => clicks++}>
        保存资料
      </Button>,
    ),
  );
  const button = document.querySelector('button')!;
  await act(() => button.click());
  assert.equal(clicks, 0);
  assert.equal(button.disabled, true);
  assert.equal(button.getAttribute('aria-busy'), 'true');
  assert.equal(button.textContent, '保存资料');
});
test('success feedback closes for 300ms before hiding the final host; errors remain', async () => {
  await act(() => root.render(<Toaster />));
  await act(() => {
    toast.success('虚构成功');
    toast.error('虚构错误');
  });
  await tick(3000);
  assert.equal(
    document.querySelector('.jm-toast-slot[data-state=closing]')?.textContent,
    '虚构成功',
  );
  assert.ok(document.body.textContent?.includes('虚构错误'));
  await tick(299);
  assert.ok(document.body.textContent?.includes('虚构成功'));
  await tick(1);
  assert.equal(document.body.textContent?.includes('虚构成功'), false);
  await tick(100000);
  assert.ok(document.body.textContent?.includes('虚构错误'));
  await act(() => document.querySelector<HTMLButtonElement>('[aria-label="关闭通知"]')!.click());
  assert.equal(document.querySelector('.jm-toast-slot')?.getAttribute('aria-hidden'), 'true');
  await tick(300);
  assert.equal(document.querySelector('.jm-toaster:not([hidden])'), null);
});
test('keyed replacements cancel an old exit and do not delete the new notice', async () => {
  await act(() => {
    toast.error('旧失败', { id: 'same-project' });
    toast.error('另一项目失败', { id: 'other-project' });
  });
  await act(() => document.querySelector<HTMLButtonElement>('[aria-label="关闭通知"]')!.click());
  await tick(100);
  await act(() => toast.error('新失败', { id: 'same-project' }));
  await tick(1000);
  assert.equal(document.body.textContent?.includes('旧失败'), false);
  assert.ok(document.body.textContent?.includes('新失败'));
  assert.ok(document.body.textContent?.includes('另一项目失败'));
  assert.equal(document.querySelectorAll('.jm-toast-slot').length, 2);
  for (const button of document.querySelectorAll<HTMLButtonElement>('[aria-label="关闭通知"]'))
    await act(() => button.click());
  await tick(300);
});
test('hover pauses a success notice and resumes its remaining lifetime', async () => {
  await act(() => toast.success('暂停通知'));
  await tick(1000);
  const item = document.querySelector('.jm-toast-slot')!;
  await act(() => item.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
  await tick(10000);
  assert.equal(item.getAttribute('data-state'), 'open');
  await act(() =>
    item.dispatchEvent(new MouseEvent('mouseout', { bubbles: true, relatedTarget: document.body })),
  );
  await tick(1999);
  assert.equal(item.getAttribute('data-state'), 'open');
  await tick(1);
  assert.equal(item.getAttribute('data-state'), 'closing');
  await tick(300);
  assert.equal(document.querySelector('.jm-toaster:not([hidden])'), null);
});

test('tooltip focus is immediate, Escape exits, and leaving permits reopening', async () => {
  await act(() =>
    root.render(
      <ControlTooltip label="此入口未接入" disabled>
        <Button disabled>虚构入口</Button>
      </ControlTooltip>,
    ),
  );
  const anchor = document.querySelector('.jm-tooltip-anchor')!;
  await act(() => anchor.dispatchEvent(new window.FocusEvent('focusin', { bubbles: true })));
  assert.equal(document.querySelector('[role=tooltip]')?.textContent, '此入口未接入');
  await act(() =>
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  );
  assert.equal(document.querySelector('[role=tooltip]')?.getAttribute('data-state'), 'closing');
  await tick(200);
  assert.equal(document.querySelector('[role=tooltip]'), null);
  await act(() =>
    anchor.dispatchEvent(
      new MouseEvent('mouseout', { bubbles: true, relatedTarget: document.body }),
    ),
  );
  await act(() => anchor.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
  await tick(149);
  assert.equal(document.querySelector('[role=tooltip]'), null);
  await tick(1);
  assert.ok(document.querySelector('[role=tooltip]'));
  await act(() => root.render(<Toaster />));
  await tick(1000);
  assert.equal(document.querySelector('[role=tooltip]'), null);
});
test('reduced motion removes feedback without a closing delay', async () => {
  const normal = globalThis.matchMedia;
  globalThis.matchMedia = () => ({
    matches: true,
    addEventListener() {},
    removeEventListener() {},
  });
  await act(() => toast.error('减弱动态错误'));
  await act(() => document.querySelector<HTMLButtonElement>('[aria-label="关闭通知"]')!.click());
  await tick(0);
  assert.equal(document.querySelector('.jm-toaster:not([hidden])'), null);
  globalThis.matchMedia = normal;
});
