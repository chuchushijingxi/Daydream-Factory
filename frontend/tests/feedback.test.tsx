import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import React, { act, useState } from 'react';
import { JSDOM } from 'jsdom';
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
const { createRoot } = await import('react-dom/client'),
  { LanguageProvider } = await import('../src/contexts/LanguageContext'),
  { Dialog, DialogTrigger, DialogContent, DialogTitle, DialogDescription } = await import(
    '../src/components/ui/dialog'
  ),
  { Button } = await import('../src/components/ui/button'),
  { Toaster, toast } = await import('../src/components/ui/sonner');
await import('../src/focus-visibility');
let root: ReturnType<typeof createRoot> | undefined;
async function mount(children: React.ReactNode) {
  if (root) await act(() => root!.unmount());
  await act(() => toast.dismiss());
  root = createRoot(document.getElementById('root')!);
  await act(() => root!.render(<LanguageProvider>{children}</LanguageProvider>));
}
after(async () => {
  if (root) await act(() => root!.unmount());
  dom.window.close();
});
test('the shared confirmation blocks Escape and its close button while pending and restores its trigger after Escape', async () => {
  let closes = 0;
  function Example({ busy }: { busy: boolean }) {
    const [open, setOpen] = useState(false);
    return (
      <Dialog
        open={open}
        onOpenChange={(v) => {
          if (!v) closes++;
          setOpen(v);
        }}
      >
        <DialogTrigger asChild>
          <Button>打开确认</Button>
        </DialogTrigger>
        <DialogContent variant="confirm" closeDisabled={busy}>
          <DialogTitle>确认操作</DialogTitle>
          <DialogDescription>操作对象与影响</DialogDescription>
        </DialogContent>
      </Dialog>
    );
  }
  await mount(<Example busy />);
  await act(() => document.querySelector<HTMLButtonElement>('button')!.click());
  const dialog = document.querySelector('[role=alertdialog]')!;
  assert.ok(dialog);
  assert.equal(document.querySelector<HTMLButtonElement>('[aria-label=关闭]')!.disabled, true);
  await act(() =>
    dialog.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    ),
  );
  assert.equal(closes, 0);
  assert.ok(document.querySelector('[role=alertdialog]'));
  await act(() =>
    root!.render(
      <LanguageProvider>
        <Example busy={false} />
      </LanguageProvider>,
    ),
  );
  await act(() =>
    document
      .querySelector('[role=alertdialog]')!
      .dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
      ),
  );
  assert.equal(closes, 1);
  assert.equal(document.querySelector('[role=alertdialog]'), null);
  assert.equal(document.activeElement?.textContent, '打开确认');
});
function FocusDialog() {
  return (
    <Dialog>
      <DialogTrigger asChild><Button>打开历史</Button></DialogTrigger>
      <DialogContent>
        <DialogTitle>历史</DialogTitle>
        <DialogDescription>图片</DialogDescription>
        <input aria-label="名称" />
        <textarea aria-label="说明" />
        <div contentEditable suppressContentEditableWarning><span>可编辑正文</span></div>
      </DialogContent>
    </Dialog>
  );
}
async function pointerOpenDialog() {
  await mount(<FocusDialog />);
  const trigger = document.querySelector<HTMLButtonElement>('button')!;
  await act(() => {
    trigger.focus();
    trigger.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }));
    trigger.click();
  });
  return trigger;
}
async function escapeDialog() {
  await act(() => document.activeElement!.dispatchEvent(
    new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
  ));
  await act(async () => new Promise(resolve => setTimeout(resolve, 15)));
  assert.equal(document.querySelector('[role=dialog]'), null);
}
test('pointer dialog dismissal retains trigger focus without switching to keyboard', async () => {
  const trigger = await pointerOpenDialog();
  await act(() => {
    const close = document.querySelector<HTMLButtonElement>('[aria-label=关闭]')!;
    close.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }));
    close.click();
  });
  await act(async () => new Promise(resolve => setTimeout(resolve, 15)));
  assert.equal(document.activeElement, trigger);
  assert.equal(document.documentElement.dataset.inputModality, 'pointer');
});
test('Escape after pointer opening restores focus without inventing keyboard navigation', async () => {
  const trigger = await pointerOpenDialog();
  assert.equal(document.activeElement?.getAttribute('aria-label'), '名称');
  await escapeDialog();
  assert.equal(document.activeElement, trigger);
  assert.equal(document.documentElement.dataset.inputModality, 'pointer');
});
test('ordinary text edits, multiline navigation and contenteditable input preserve the pointer origin on Escape', async () => {
  const trigger = await pointerOpenDialog();
  for (const editor of document.querySelectorAll<HTMLElement>('input,textarea,[contenteditable] span')) {
    editor.closest<HTMLElement>('[contenteditable]')?.focus();
    if (editor.matches('input,textarea')) editor.focus();
    for (const key of ['a', '中', 'Enter', ' ', 'ArrowLeft', 'Home', 'Backspace']) {
      await act(() => editor.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true })));
      assert.equal(document.documentElement.dataset.inputModality, 'pointer', `${editor.tagName}: ${key}`);
    }
  }
  await escapeDialog();
  assert.equal(document.activeElement, trigger);
  assert.equal(document.documentElement.dataset.inputModality, 'pointer');
});
test('Tab and Shift+Tab establish keyboard navigation that survives text editing and Escape restoration', async () => {
  for (const shiftKey of [false, true]) {
    const trigger = await pointerOpenDialog();
    await act(() => document.activeElement!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Tab', shiftKey, bubbles: true }),
    ));
    await act(() => document.activeElement!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'a', bubbles: true }),
    ));
    await escapeDialog();
    assert.equal(document.activeElement, trigger);
    assert.equal(document.documentElement.dataset.inputModality, 'keyboard');
  }
});
test('keyboard activation and menu navigation establish keyboard focus while editing and modifier events do not', async () => {
  await mount(<><Button>操作</Button><div role="menuitem" tabIndex={0}>菜单项</div></>);
  const button = document.querySelector('button')!;
  const menuitem = document.querySelector<HTMLElement>('[role=menuitem]')!;
  for (const [target, key] of [
    [button, 'Enter'], [button, ' '], [menuitem, 'ArrowDown'], [menuitem, 'Home'],
    [menuitem, 'End'], [menuitem, 'PageDown'], [menuitem, 'a'],
  ] as const) {
    button.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }));
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }));
    assert.equal(document.documentElement.dataset.inputModality, 'keyboard', key);
    target.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    assert.equal(document.documentElement.dataset.inputModality, 'keyboard', `Escape after ${key}`);
  }
  for (const init of [
    { key: 'Shift' }, { key: 'Control' }, { key: 'Alt' }, { key: 'Meta' }, { key: 'Escape' },
    { key: 'Enter', isComposing: true }, { key: 'Enter', keyCode: 229 },
    { key: 'a', metaKey: true }, { key: 'ArrowDown', altKey: true }, { key: 'a', ctrlKey: true },
  ]) {
    button.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }));
    menuitem.dispatchEvent(new KeyboardEvent('keydown', { ...init, bubbles: true }));
    assert.equal(document.documentElement.dataset.inputModality, 'pointer', JSON.stringify(init));
  }
});
test('keyboard-opened dialogs keep keyboard origin through Escape; a subsequent pointer interaction resets it', async () => {
  for (const key of ['Enter', ' ']) {
    await mount(<FocusDialog />);
    const trigger = document.querySelector<HTMLButtonElement>('button')!;
    await act(() => {
      trigger.focus();
      trigger.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }));
      // JSDOM has no native keyboard click default; exercise the same activation handler explicitly.
      trigger.click();
    });
    await escapeDialog();
    assert.equal(document.activeElement, trigger);
    assert.equal(document.documentElement.dataset.inputModality, 'keyboard');
    trigger.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }));
    assert.equal(document.documentElement.dataset.inputModality, 'pointer');
  }
});
test('actual pointer focus CSS covers shared control roles with explicit outline priority and preserves editing and card decoration', async () => {
  await mount(
    <div className="editor-workspace">
      <div className="sb-production"><div className="sb-strip-gap-target"><button>插入分镜</button></div></div>
      <Button>普通操作</Button><a href="#menu">链接</a>
      <details><summary>展开</summary></details>
      {(['button', 'tab', 'menuitem', 'menuitemcheckbox', 'menuitemradio', 'radio', 'checkbox', 'switch', 'option', 'combobox'] as const).map(role => <div key={role} tabIndex={0} role={role}>选项</div>)}
      <button className="decorated focus-visible:ring-2">角色图片</button>
      <input role="combobox" /><textarea /><div tabIndex={0} role="button" contentEditable suppressContentEditableWarning>可编辑</div>
    </div>,
  );
  const style = document.createElement('style');
  style.textContent = readFileSync(new URL('../src/styles/focus-visibility.css', import.meta.url), 'utf8');
  document.head.append(style);
  const rules = [...style.sheet!.cssRules] as CSSStyleRule[];
  const outline = rules.find(rule => rule.style.getPropertyValue('outline'))!;
  assert.equal(outline.style.getPropertyValue('outline'), 'none');
  assert.equal(outline.style.getPropertyPriority('outline'), 'important');
  const ring = rules.find(rule => rule.style.getPropertyValue('--tw-ring-shadow'))!;
  for (const property of ['--tw-ring-shadow', '--tw-ring-offset-shadow', '--tw-inset-ring-shadow']) {
    assert.equal(ring.style.getPropertyValue(property), '0 0 #0000');
    assert.equal(ring.style.getPropertyPriority(property), 'important');
  }
  assert.ok(rules.every(rule => !rule.style.getPropertyValue('box-shadow')));
  try {
    document.documentElement.dataset.inputModality = 'pointer';
    const controls = document.querySelectorAll<HTMLElement>('#root button,#root a,#root summary,#root [role]:not(input,[contenteditable])');
    for (const control of controls) {
      control.focus();
      assert.ok(control.matches(outline.selectorText), control.outerHTML);
      assert.equal(getComputedStyle(control).outline, 'none');
    }
    const gap = document.querySelector<HTMLButtonElement>('.sb-strip-gap-target>button')!;
    gap.focus();
    const componentCss = readFileSync(new URL('../src/styles/production-layout.css', import.meta.url), 'utf8');
    const gapSelector = '.editor-workspace .sb-production .sb-strip-gap-target>button:focus-visible';
    assert.ok(componentCss.includes(`${gapSelector} {outline:2px`));
    assert.ok(gap.matches(gapSelector));
    assert.ok(gap.matches(outline.selectorText));
    // JSDOM does not implement native specificity/important cascading correctly; Chromium verifies this conflict.
    const card = document.querySelector<HTMLButtonElement>('.decorated')!;
    card.style.boxShadow = '0 8px 20px rgba(0, 0, 0, 0.1)';
    card.style.border = '2px solid orange';
    card.focus();
    assert.ok(card.matches(ring.selectorText));
    assert.equal(getComputedStyle(card).getPropertyValue('--tw-ring-shadow'), '0 0 #0000');
    assert.equal(getComputedStyle(card).boxShadow, '0 8px 20px rgba(0, 0, 0, 0.1)');
    assert.equal(getComputedStyle(card).borderColor, 'orange');
    for (const editor of document.querySelectorAll<HTMLElement>('#root input,#root textarea,#root [contenteditable]')) {
      editor.focus();
      assert.equal(editor.matches(outline.selectorText), false, editor.outerHTML);
    }
    document.documentElement.dataset.inputModality = 'keyboard';
    card.focus();
    assert.equal(card.matches(outline.selectorText), false);
    assert.equal(card.matches(ring.selectorText), false);
  } finally {
    style.remove();
  }
});

test('feedback kinds share one host, deduplicate by operation and dismiss errors independently', async () => {
  await mount(<Toaster />);
  await act(() => {
    toast.success('保存中', { id: 'save' });
    toast.success('保存完成', { id: 'save' });
    toast.info('提示');
    toast.warning('检查输入');
    toast.error('失败保留');
  });
  assert.equal(document.querySelectorAll('.jm-toaster').length, 1);
  assert.equal(document.querySelectorAll('.jm-toast').length, 4);
  assert.equal(document.querySelectorAll('.jm-toast[role=alert]').length, 2);
  assert.equal(document.querySelectorAll('.jm-toast[role=status]').length, 2);
  assert.doesNotMatch(document.body.textContent || '', /保存中/);
  assert.match(document.body.textContent || '', /失败保留/);
  await act(() => document.querySelector<HTMLButtonElement>('[aria-label=关闭通知]')!.click());
  await act(async () => new Promise((resolve) => setTimeout(resolve, 320)));
  assert.equal(document.querySelector('.jm-toast[data-kind=error]'), null);
  assert.equal(document.querySelectorAll('.jm-toast').length, 3);
});

test('the feedback live region exists before a modal opens so later errors remain readable', async () => {
  function Example() {
    return (
      <>
        <Dialog>
          <DialogTrigger asChild>
            <Button>打开表单</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogTitle>表单</DialogTitle>
            <DialogDescription>测试</DialogDescription>
            <Button onClick={() => toast.error('弹窗内失败')}>触发失败</Button>
          </DialogContent>
        </Dialog>
        <Toaster />
      </>
    );
  }
  await mount(<Example />);
  const host = document.querySelector('.jm-toaster')!;
  assert.equal(host.getAttribute('aria-live'), 'polite');
  assert.equal(host.hasAttribute('hidden'), true);
  await act(() => document.querySelector<HTMLButtonElement>('button')!.click());
  await act(() =>
    [...document.querySelectorAll<HTMLButtonElement>('button')]
      .find((b) => b.textContent === '触发失败')!
      .click(),
  );
  assert.equal(host.hasAttribute('hidden'), false);
  assert.equal(document.querySelector('.jm-toast')?.closest('[aria-hidden=true]'), null);
  assert.ok(document.querySelector('[role=dialog]'));
});
