import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import React, { act } from 'react';
import { createStoryboardFixture } from './storyboard-fixture.mjs';
import {
  indexLabel,
  indexReady,
  shotInput,
  shotPatch,
  referenceLocations,
} from '../src/domain/storyboard';
import { replaceParts } from '../src/pages/production/prompt-document';
import type { EpisodeView, StoryShot } from '../src/api/storyboard-types';
const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/#/playlet/p5_free/production',
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
  'Range',
])
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
globalThis.getComputedStyle = dom.window.getComputedStyle;
globalThis.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
globalThis.innerWidth = 1440;
globalThis.innerHeight = 1000;
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const { createRoot } = await import('react-dom/client'),
  { HashRouter } = await import('react-router-dom'),
  { LanguageProvider } = await import('../src/contexts/LanguageContext'),
  { WorkspaceProvider, useWorkspace } = await import('../src/state/WorkspaceContext'),
  { MainWorkspace } = await import('../src/MainWorkspace');
const { currentEpisodeRun } = await import('../src/pages/production/EpisodeRunFeedback'),
  { toast } = await import('../src/components/ui/sonner');
const { SessionOptionalContext } = await import('../src/state/SessionContext'),
  { sessionBoundary } = await import('../src/api/session-boundary'),
  { createAdministrationFixture } = await import('./administration-fixture');
let fixture = createStoryboardFixture(),
  root: ReturnType<typeof createRoot> | undefined,
  context: ReturnType<typeof useWorkspace>;
function Probe() {
  context = useWorkspace();
  return null;
}
const saved = async () => {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 680));
  });
  await flush();
};
// Render flushing stays real while individual tests control the autosave clock.
const renderTimer = setTimeout;
const flush = async () => {
  await act(async () => {
    await new Promise((r) => renderTimer(r, 15));
  });
};
const button = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent?.trim() === name || b.getAttribute('aria-label') === name,
  )!;
const writes = () => fixture.requests.filter((r) => r.method === 'POST');
async function mount(scenario = 'normal', route = '/playlet/p5_free/production?episodeId=ep_upper', wrapFetch?: (base: typeof globalThis.fetch) => typeof globalThis.fetch) {
  if (root) await act(() => root!.unmount());
  sessionBoundary.reset();
  sessionStorage.clear();
  fixture = createStoryboardFixture();
  fixture.setScenario(scenario);
  globalThis.fetch = wrapFetch ? wrapFetch(fixture.fetch) : fixture.fetch;
  // 每次 mount 都是全新的隔离库，不能继承上一夹具的路由启动回执。
  window.history.replaceState(null, '', '#' + route);
  root = createRoot(document.getElementById('root')!);
  await act(() =>
    root!.render(
      <LanguageProvider>
        <HashRouter>
          <WorkspaceProvider>
            <Probe />
            <MainWorkspace />
          </WorkspaceProvider>
        </HashRouter>
      </LanguageProvider>,
    ),
  );
  await flush();
  await flush();
}
async function navigate(route: string) {
  await act(() => {
    window.location.hash = '#' + route;
    window.dispatchEvent(new dom.window.HashChangeEvent('hashchange'));
  });
  await flush();
}
async function input(selector: string, value: string) {
  const field = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector)!;
  assert.ok(field, selector);
  await act(() => {
    Object.getOwnPropertyDescriptor(
      field instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(field, value);
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
}
async function key(el: Element, key: string, extra = {}) {
  await act(() =>
    el.dispatchEvent(
      new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...extra }),
    ),
  );
  await flush();
}
test('null episode numbers remain episodes; whole and failed indexes never display zero or max number', () => {
  const f = createStoryboardFixture();
  assert.equal(indexReady(f.index(), 1), true);
  assert.equal(indexLabel(f.index(), 1), '本次上传共 2 集');
  f.setScenario('whole');
  assert.equal(indexLabel(f.index(), 1), '未分集／整篇');
  f.setScenario('index-failed');
  assert.equal(indexReady(f.index(), 1), false);
  assert.equal(indexLabel(f.index(), 1), '分集识别失败');
});
test('ID tokens are atomic through replacement and duplicate reference locations are explicit', () => {
  const ref = { type: 'asset' as const, assetId: 'same_1', lookId: 'look_1' },
    parts = [
      { type: 'text' as const, text: '甲' },
      ref,
      { type: 'text' as const, text: '\n' },
      ref,
    ];
  assert.deepEqual(replaceParts(parts, 1, 2, []), [{ type: 'text', text: '甲\n' }, ref]);
  assert.deepEqual(referenceLocations(parts, ref), ['第 1 行，第 2 位置', '第 2 行，第 1 位置']);
});
test('read-only decorations are stripped from all selected assets and prompt parts', () => {
  const view = createStoryboardFixture().episode('ep_upper') as EpisodeView,
    s = shotInput(view.shots[0]);
  assert.deepEqual(Object.keys(s.selectedAssets[0]).sort(), ['assetId', 'lookId']);
  assert.deepEqual(
    Object.keys(s.videoPrompt!.parts.find((p) => p.type === 'asset' && p.lookId)!).sort(),
    ['assetId', 'lookId', 'type'],
  );
  const changed = { ...s, action: '另一动作' };
  assert.deepEqual(shotPatch(s, changed), { action: '另一动作' });
});
test('video duration follows each shot and rounds historical fractions up without rewriting the storyboard', async () => {
  await mount('shot-durations');
  assert.equal(button('时长').textContent?.trim(), '6 秒');
  await act(() => document.querySelectorAll<HTMLButtonElement>('.sb-strip-select')[1].click());
  await flush();
  assert.equal(button('时长').textContent?.trim(), '9 秒');
  await act(() => document.querySelectorAll<HTMLButtonElement>('.sb-strip-select')[0].click());
  await flush();
  assert.equal(button('时长').textContent?.trim(), '6 秒');
  assert.equal(fixture.shots[0].durationSeconds, 5.5);
  assert.equal(writes().length, 0);
  await act(() => button('生成视频').click());
  await flush();
  assert.equal(writes().length, 1);
  assert.equal(writes()[0].body.options.durationSeconds, 6);
});
test('empty production retains the strip and only enables adding shots when a scene exists', async () => {
  await mount('episode-ready');
  assert.match(document.querySelector('.sb-filmstrip')!.textContent!, /暂无分镜，先新增场景/);
  assert.equal(button('新增').disabled, true);
  await mount('empty-scene');
  assert.match(document.querySelector('.sb-filmstrip')!.textContent!, /暂无分镜，点击新增开始/);
  assert.equal(button('新增').disabled, false);
  assert.equal(writes().length, 0);
});
test('a scene is created from its name and description without choosing assets or technical source fields', async () => {
  await mount('episode-ready');
  const ranges = structuredClone(fixture.episode('ep_upper').episode.sourceRanges);
  await act(() => button('新增场景').click());
  await flush();
  const dialog = document.querySelector('.sb-scene-dialog')!;
  assert.equal(dialog.querySelectorAll('input,textarea,select').length, 2);
  assert.doesNotMatch(dialog.textContent!, /剧情时间|地点资产|Unicode|来源版本|移除范围/);
  assert.equal(button('保存场景').disabled, true);
  await input('.sb-scene-dialog input', '雨夜归来');
  await input('.sb-scene-dialog textarea', '她独自回到老宅。\n停在门口。');
  await act(() => button('保存场景').click());
  await saved();
  const scene = fixture.episode('ep_upper').scenes[0];
  assert.equal(scene.title, '雨夜归来');
  assert.equal(scene.summary, '她独自回到老宅。\n停在门口。');
  assert.equal(scene.time, '');
  assert.equal(scene.locationAssetId, null);
  assert.deepEqual(scene.sourceRanges, ranges);
  assert.equal(writes().length, 1);
  assert.equal(writes()[0].body.operations[0].op, 'createScene');
  assert.equal(document.querySelector('.sb-scene-dialog'), null);
  assert.match(document.querySelector('.sb-scene-list')!.textContent!, /雨夜归来/);
  await navigate('/projects');
  await navigate('/playlet/p5_free/production?episodeId=ep_upper');
  assert.match(document.querySelector('.sb-scene-list')!.textContent!, /她独自回到老宅/);
  await act(() => button('新增').click());
  await flush();
  assert.equal(writes().length, 2);
  assert.equal(writes()[1].body.operations[0].op, 'createShot');
  assert.ok(document.querySelector('[aria-label="视频提示词"]'));
});
test('new scene conflicts retain both fields and cancel sends no changes', async () => {
  let attempts = 0;
  await mount('episode-ready', '/playlet/p5_free/production?episodeId=ep_upper', base =>
    async (url, options) => {
      if (String(url).endsWith('/storyboard/changes')) {
        attempts++;
        return Response.json({code: 'CONFLICT', message: '项目版本已变化'}, {status: 409});
      }
      return base(url, options);
    });
  await act(() => button('新增场景').click());
  await flush();
  await input('.sb-scene-dialog input', '未丢失的名称');
  await input('.sb-scene-dialog textarea', '未丢失的说明');
  await act(() => button('保存场景').click());
  await flush();
  assert.equal(document.querySelector<HTMLInputElement>('.sb-scene-dialog input')!.value, '未丢失的名称');
  assert.equal(document.querySelector<HTMLTextAreaElement>('.sb-scene-dialog textarea')!.value, '未丢失的说明');
  assert.match(document.querySelector('.sb-scene-dialog [role=alert]')!.textContent!, /输入保留/);
  assert.equal(fixture.episode('ep_upper').scenes.length, 0);
  await act(() => button('取消').click());
  await saved();
  assert.equal(document.querySelector('.sb-scene-dialog'), null);
  assert.equal(attempts, 1);
  assert.equal(writes().length, 0);
});
async function appendPrompt(text: string) {
  const editor = document.querySelector<HTMLElement>('[aria-label="视频提示词"]')!;
  await act(() => {
    editor.append(document.createTextNode(text));
    editor.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
}
async function typeReferenceTrigger() {
  const editor = document.querySelector<HTMLElement>('[aria-label="视频提示词"]')!;
  await act(() => {
    editor.focus();
    const text = document.createTextNode('@');
    editor.append(text);
    const range = document.createRange();
    range.setStart(text, 1);
    range.collapse(true);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
    editor.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
}
test('reference suggestions follow the caret outside editor layout, flip at viewport edges and follow scrolling', async () => {
  await mount();
  const editor = document.querySelector<HTMLElement>('[aria-label="视频提示词"]')!;
  const originalRangeRect = Object.getOwnPropertyDescriptor(Range.prototype, 'getBoundingClientRect');
  let caret = { left: 420, right: 420, top: 160, bottom: 186, width: 0, height: 26 };
  Object.defineProperty(Range.prototype, 'getBoundingClientRect', {
    configurable: true, value: () => caret,
  });
  Object.defineProperty(editor, 'getBoundingClientRect', {
    configurable: true,
    value: () => ({ left: 300, right: 1000, top: 100, bottom: 760, width: 700, height: 660 }),
  });
  try {
    await typeReferenceTrigger();
    const list = document.querySelector<HTMLElement>('.sb-mention-list')!;
    assert.equal(list.parentElement, document.body);
    assert.equal(editor.parentElement!.querySelector('.sb-mention-list'), null);
    assert.equal(list.style.position, 'fixed');
    assert.equal(list.style.left, '420px');
    assert.equal(list.style.top, '194px');
    assert.equal(list.style.width, '268px');
    assert.equal(list.style.visibility, 'visible');
    assert.match(list.textContent!, /已选参考素材/);
    assert.doesNotMatch(list.textContent!, /character_1|prop_1|look_rain/);
    Object.defineProperty(list, 'scrollHeight', { configurable: true, value: 220 });
    caret = { ...caret, left: 1000, right: 1000, top: 690, bottom: 716 };
    await act(() => window.dispatchEvent(new Event('resize')));
    assert.equal(list.style.left, '748px');
    assert.equal(list.style.top, '460px');
    assert.equal(list.style.transformOrigin, 'bottom left');
    caret = { ...caret, left: 450, right: 450, top: 300, bottom: 326 };
    await act(() => editor.dispatchEvent(new Event('scroll', { bubbles: false })));
    assert.equal(list.style.left, '450px');
    assert.equal(list.style.top, '334px');
    caret = { ...caret, top: 60, bottom: 86 };
    await act(() => editor.dispatchEvent(new Event('scroll', { bubbles: false })));
    assert.equal(list.style.visibility, 'hidden');
    assert.equal(writes().length, 0);
    await key(editor, 'Escape');
    await act(async () => new Promise((resolve) => setTimeout(resolve, 220)));
    assert.equal(document.querySelector('.sb-mention-list'), null);
  } finally {
    if (originalRangeRect) Object.defineProperty(Range.prototype, 'getBoundingClientRect', originalRangeRect);
    else delete Range.prototype.getBoundingClientRect;
  }
});
test('single prompt save preserves action, dialogue and camera and keeps ID-bound parts', async () => {
  await mount();
  const before = structuredClone(fixture.shots[0]);
  await appendPrompt('明确修改后的动态描述，完整保留。');
  await saved();
  assert.equal(writes().length, 1);
  const patch = writes()[0].body.operations[0].patch;
  assert.deepEqual(Object.keys(patch), ['videoPrompt']);
  assert.match(JSON.stringify(patch.videoPrompt.parts), /明确修改后的动态描述/);
  assert.equal(patch.videoPrompt.parts.filter((p) => p.type === 'asset').length, 5);
  assert.equal(fixture.shots[0].action, before.action);
  assert.deepEqual(fixture.shots[0].dialogues, before.dialogues);
  assert.deepEqual(fixture.shots[0].camera, before.camera);
  assert.equal(button('保存镜头'), undefined);
});
test('prompt draft survives switching shots, episodes and theme before autosave', async (t) => {
  await mount();
  // Navigation must not race a real 600ms debounce on a busy test host.
  t.mock.timers.enable({ apis: ['setTimeout'] });
  t.after(() => t.mock.timers.reset());
  const browseBeforeSave = async (route: string) => {
    await navigate(route);
    // JSDOM queues the native hash change at zero delay.
    await act(() => t.mock.timers.tick(0));
    await flush();
  };
  await appendPrompt('未保存的第一镜草稿');
  await browseBeforeSave('/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_2');
  assert.doesNotMatch(
    document.querySelector('[aria-label="视频提示词"]')!.textContent!,
    /未保存的第一镜草稿/,
  );
  await browseBeforeSave('/playlet/p5_free/production?episodeId=ep_lower');
  assert.match(document.body.textContent!, /站台 · 归途/);
  await act(() => context.toggleTheme());
  await browseBeforeSave('/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_1');
  assert.match(
    document.querySelector('[aria-label="视频提示词"]')!.textContent!,
    /未保存的第一镜草稿/,
  );
  assert.equal(writes().length, 0);
  await act(() => t.mock.timers.tick(600));
  await flush();
  assert.equal(writes().length, 1);
  assert.equal(writes()[0].body.operations[0].shotId, 'shot_1');
  assert.match(
    JSON.stringify(writes()[0].body.operations[0].patch.videoPrompt.parts),
    /未保存的第一镜草稿/,
  );
});
test('prompt conflict preserves draft and other user camera updates with explicit rebase', async () => {
  await mount('conflict');
  await appendPrompt('冲突草稿');
  fixture.shots[0].camera.angle = '另一位用户的机位更新';
  await saved();
  assert.equal(writes().length, 1);
  assert.match(document.querySelector('[aria-label="视频提示词"]')!.textContent!, /冲突草稿/);
  assert.match(document.body.textContent!, /不会自动重发/);
  assert.equal(button('保存镜头'), undefined);
  await act(() => button('核对后采用最新版本继续编辑').click());
  await flush();
  await saved();
  assert.equal(writes().length, 2);
  assert.equal(fixture.shots[0].camera.angle, '另一位用户的机位更新');
  assert.deepEqual(Object.keys(writes()[1].body.operations[0].patch), ['videoPrompt']);
});
test('unknown prompt response retains draft, performs one POST and reads back', async () => {
  await mount('unknown');
  await appendPrompt('未知结果草稿');
  const count = fixture.requests.length;
  await saved();
  assert.equal(writes().length, 1);
  assert.ok(fixture.requests.slice(count).some((r) => r.method === 'GET'));
  assert.match(document.querySelector('[aria-label="视频提示词"]')!.textContent!, /未知结果草稿/);
  assert.equal(button('保存镜头'), undefined);
});
test('removing a referenced Look shows exact positions and never silently removes paragraphs', async () => {
  await mount();
  const original = document.querySelector('[aria-label="视频提示词"]')!.textContent;
  await act(() => button('添加素材').click());
  await flush();
  const row = [...document.querySelectorAll('.sb-picker-card')].find((e) =>
    e.textContent!.includes('林晚 · 雨衣'),
  )!;
  await act(() => (row.querySelector('input') as HTMLInputElement).click());
  await act(() => button('确认').click());
  await flush();
  assert.match(document.body.textContent!, /第 1 行/);
  assert.match(document.body.textContent!, /第 2 行/);
  assert.equal(document.querySelector('[aria-label="视频提示词"]')!.textContent, original);
  assert.equal(writes().length, 0);
});
test('post-breakdown asset details remain editable and provide image generation', async () => {
  await mount('normal', '/playlet/p5_free/characters');
  assert.equal(button('新增角色').disabled, false);
  assert.equal(document.querySelector('aside a[aria-label="创作资产"]'), null);
  assert.equal(document.querySelector('aside a[aria-label="提取过程"]'), null);
  await navigate('/playlet/p5_free/characters/character_1/canvas');
  assert.ok(button('新增附加形象'));
  await act(() => button('查看基础资料').click());
  await flush();
  assert.equal((document.querySelector('[aria-label="名称"]') as HTMLInputElement).disabled, false);
  assert.equal((document.querySelector('[aria-label="视觉提示词"]') as HTMLTextAreaElement).disabled, false);
  assert.equal(button('重新生成提示词'), undefined);
  assert.ok(button('生成图片'));
  assert.doesNotMatch(document.body.textContent!, /资产资料只读|资产资料已锁定/);
  assert.equal(writes().length, 0);
});

test('running episode locks editing, cancellation retains content and restores normal editing', async () => {
  await mount('running');
  // 本集运行中只展示进度，不呈现中间镜头编辑器。
  assert.equal(document.querySelector('[aria-label="视频提示词"]'), null);
  assert.ok(button('取消生成'));
  await act(() => button('取消生成').click());
  await flush();
  assert.equal(fixture.shots.length, 3);
  assert.equal(writes().length, 1);
  assert.match(writes()[0].path, /cancel$/);
  assert.equal(
    document.querySelector('[aria-label="视频提示词"]')!.getAttribute('contenteditable'),
    'true',
  );
});
test('late previous-episode response cannot replace newly selected episode', async () => {
  await mount('late');
  await navigate('/playlet/p5_free/production?episodeId=ep_lower');
  assert.match(document.body.textContent!, /站台 · 归途/);
  await act(async () => {
    await new Promise((r) => setTimeout(r, 750));
  });
  assert.match(document.body.textContent!, /站台 · 归途/);
  assert.doesNotMatch(document.querySelector('.sb-scenes')!.textContent!, /钥匙与决定/);
  assert.equal(writes().length, 0);
});
test('IME composition retains full content and atomic editor undo/redo returns ID nodes', async () => {
  await mount();
  const editor = document.querySelector<HTMLElement>('[aria-label="视频提示词"]')!;
  await act(() => {
    editor.focus();
    const r = document.createRange();
    r.selectNodeContents(editor);
    r.collapse(false);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(r);
  });
  await act(() => {
    editor.dispatchEvent(new dom.window.CompositionEvent('compositionstart', { bubbles: true }));
    editor.append(document.createTextNode('中文输入'));
    editor.dispatchEvent(new Event('input', { bubbles: true }));
  });
  assert.equal(writes().length, 0);
  await act(() =>
    editor.dispatchEvent(new dom.window.CompositionEvent('compositionend', { bubbles: true })),
  );
  await flush();
  assert.equal(button('保存镜头'), undefined);
  assert.equal(button('生成视频').disabled, true);
  await key(editor, 'z', { ctrlKey: true });
  assert.doesNotMatch(editor.textContent!, /中文输入/);
  assert.equal(editor.querySelectorAll('.sb-mention').length, 5);
  await key(editor, 'z', { ctrlKey: true, shiftKey: true });
  assert.match(editor.textContent!, /中文输入/);
});
test('plain-text paste never restores binding; copy derives readable text without IDs', async () => {
  await mount();
  const editor = document.querySelector<HTMLElement>('[aria-label="视频提示词"]')!;
  await act(() => {
    editor.focus();
    const range = document.createRange();
    range.selectNodeContents(editor);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
  });
  let copied = '';
  const event = new Event('copy', { bubbles: true, cancelable: true });
  Object.defineProperty(event, 'clipboardData', {
    value: {
      setData(_type: string, value: string) {
        copied = value;
      },
    },
  });
  await act(() => editor.dispatchEvent(event));
  assert.match(copied, /@林晚 · 雨衣/);
  assert.doesNotMatch(copied, /character_1|look_rain/);
  const paste = new Event('paste', { bubbles: true, cancelable: true });
  Object.defineProperty(paste, 'clipboardData', {
    value: {
      getData() {
        return '@林晚 · 雨衣\n普通粘贴';
      },
    },
  });
  await act(() => editor.dispatchEvent(paste));
  await flush();
  assert.equal(editor.querySelectorAll('.sb-mention').length, 0);
  assert.equal(editor.textContent, '@林晚 · 雨衣\n普通粘贴');
  assert.equal(writes().length, 0);
});
test('selection and a newly inserted reference travel in exactly one atomic save', async () => {
  await mount();
  await act(() => button('添加素材').click());
  await flush();
  await act(() => button('道具').click());
  await flush();
  const row = [...document.querySelectorAll('.sb-picker-card')].find((e) =>
    e.textContent!.includes('未选中的怀表'),
  )!;
  assert.ok(row);
  await act(() => (row.querySelector('input') as HTMLInputElement).click());
  await act(() => button('确认').click());
  await flush();
  assert.equal(writes().length, 0);
  assert.equal(button('保存镜头'), undefined);
  await typeReferenceTrigger();
  const item = [...document.querySelectorAll('[role=option]')].find((e) =>
    e.textContent!.includes('未选中的怀表'),
  )!;
  await act(() => (item as HTMLElement).click());
  await saved();
  assert.equal(writes().length, 1);
  const patch = writes()[0].body.operations[0].patch;
  assert.ok(patch.selectedAssets.some((r) => r.assetId === 'prop_unselected'));
  assert.ok(patch.videoPrompt.parts.some((p) => p.assetId === 'prop_unselected'));
  assert.deepEqual(Object.keys(patch.selectedAssets[0]).sort(), ['assetId', 'lookId']);
});
test('a project refresh preserves an open asset draft without restoring a permanent lock', async () => {
  await mount('normal', '/playlet/p5_free/characters');
  await act(() => document.querySelector<HTMLButtonElement>('.asset-catalog-card')!.click());
  await flush();
  await input('[aria-label="视觉提示词"]', '已打开详情中的未保存正文');
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  const field = document.querySelector<HTMLTextAreaElement>('[aria-label="视觉提示词"]')!;
  assert.equal(field.value, '已打开详情中的未保存正文');
  assert.equal(field.disabled, false);
  assert.equal(writes().length, 0);
  assert.doesNotMatch(document.body.textContent!, /提示词自动保存与重生成已停止|资产资料只读/);
});

test('read-only service retains full production browse and never starts or saves', async () => {
  await mount('read-only');
  assert.equal(button('本集更多操作'), undefined);
  assert.equal(
    document.querySelector('[aria-label="视频提示词"]')!.getAttribute('contenteditable'),
    'false',
  );
  assert.equal(button('添加素材').disabled, true);
  assert.equal(writes().length, 0);
});
test('partially failed episode retains the saved graph and editable asset data', async () => {
  await mount('failed');
  assert.match(document.body.textContent!, /受控部分失败/);
  assert.equal(document.querySelectorAll('.sb-strip-select').length, 2);
  assert.match(document.querySelector('[aria-label="视频提示词"]')!.textContent!, /你自己决定/);
  assert.equal(writes().length, 0);
});
test('unavailable reference keeps saved label and ID without offering a same-name replacement', async () => {
  await mount();
  fixture.project.assets = fixture.project.assets.filter((a) => a.id !== 'character_2');
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  const missing = document.querySelector<HTMLElement>('.sb-mention[data-available=false]');
  assert.ok(missing);
  assert.equal(missing.textContent, '林晚 · 基础');
  await typeReferenceTrigger();
  assert.ok(
    [...document.querySelectorAll('[role=option]')].every(
      (el) => !el.textContent!.includes('character_2'),
    ),
  );
  assert.equal(writes().length, 0);
});
after(async () => {
  if (root) await act(() => root!.unmount());
  dom.window.close();
});

test('new project parsing uses four real milestones and leaves scene generation for the chosen episode', async () => {
  await mount('parse-ready', '/playlet/p5_free/outline');
  assert.equal(document.querySelectorAll('.script-parse-panel').length, 0);
  await act(() => button('查看解析进度').click());
  await flush();
  assert.equal(document.querySelectorAll('.script-parse-steps li').length, 4);
  await act(() => button('关闭').click());
  await flush();
  assert.equal(document.querySelector('[role=progressbar]')!.getAttribute('aria-valuenow'), '25');
  assert.equal(writes().length, 0);
  await act(() => button('下一步').click());
  await flush();
  assert.equal(writes().length, 1);
  assert.match(window.location.hash, /outline$/);
  for (const [stage, percent] of [
    ['correct', '50'],
    ['visualPrompt', '75'],
    ['complete', '100'],
  ]) {
    fixture.setScenario('parse-' + stage);
    await act(() => context.refreshCurrentProject('p5_free'));
    await flush();
    assert.equal(
      document.querySelector('[role=progressbar]')!.getAttribute('aria-valuenow'),
      percent,
    );
  }
  assert.ok(button('下一步'));
  await act(() => button('下一步').click());
  await flush();
  assert.equal(button('生成资产'), undefined);
  assert.equal(writes().length, 1);
  await act(() => button('下一步').click());
  await flush();
  assert.match(window.location.hash, /episodes/);
  assert.match(document.body.textContent!, /上集 重逢/);
  assert.equal(writes().length, 1);
  assert.ok(button('拆解分镜'));
});

test('an active P5 run is not presented or cancelled as script parsing', async () => {
  await mount('running');
  assert.equal(writes().length, 0);
  await navigate('/playlet/p5_free/outline');
  assert.match(document.querySelector('.xy-workflow-dock')!.textContent!, /解析完成，可查看资产/);
  assert.equal(button('取消解析'), undefined);
  assert.ok(button('下一步'));
  assert.equal(writes().length, 0);
});

test('script progress stays in dock; opening and closing details preserves reading and sends no write', async () => {
  await mount('parse-visualPrompt', '/playlet/p5_free/outline');
  const body = document.querySelector<HTMLElement>('[aria-label="完整剧本正文"]')!;
  body.scrollTop = 96;
  assert.equal(document.querySelector('.jm-outline-layout .script-parse-panel'), null);
  assert.ok(document.querySelector('.xy-workflow-dock [aria-label="查看解析进度"]'));
  await act(() => {
    button('查看解析进度').focus();
    button('查看解析进度').click();
  });
  await flush();
  assert.match(document.querySelector('[role=dialog]')!.textContent!, /已保存 7\/7/);
  assert.ok(document.querySelector('.script-parse-steps'));
  assert.equal(document.querySelector('.script-parse-index'), null);
  assert.doesNotMatch(
    document.querySelector('[role=dialog]')!.textContent!,
    /查看分集识别结果|查看过程|查看生成过程/,
  );
  assert.equal(document.querySelector('[role=progressbar]')!.getAttribute('aria-valuenow'), '75');
  assert.ok(button('取消解析'));
  await act(() => button('关闭').click());
  await flush();
  assert.equal(body.scrollTop, 96);
  assert.equal(document.activeElement === button('查看解析进度'), true);
  assert.equal(writes().length, 0);
});

test('production prompt and reference browsing preserve bound assets and shot drafts', async () => {
  await mount();
  assert.match(document.querySelector('.xy-production-breadcrumb')!.textContent!, /上集 重逢/);
  assert.equal(document.querySelector('[aria-label="镜头动作"]'), null);
  assert.equal(document.querySelector('[aria-label="完整对白 1"]'), null);
  assert.ok(document.querySelector('[aria-label="视频制作"]'));
  assert.match(document.querySelector('[aria-label="视频提示词"]')!.textContent!, /你自己决定/);
  assert.equal(document.querySelector('.sb-reference-dialog'), null);
  assert.equal(document.querySelector('.sb-video-composer')!.textContent!.includes('复制'), false);
  assert.equal(button('选择资产'), undefined);
  assert.doesNotMatch(document.querySelector('.sb-filmstrip')!.textContent!, /分镜列表|个镜头/);
  assert.equal(document.querySelector('.sb-toolbar'), null);
  assert.ok(document.querySelector('.xy-project-titlebar [aria-label="返回分集视频"]'));
  assert.ok(document.querySelector('.sb-shot-heading .sb-composer-hint'));
  assert.equal(document.querySelectorAll('.sb-referenced-asset').length, 4);
  assert.ok(document.querySelector('.sb-scenes .sb-scene-list'));
  assert.equal(document.querySelector('.sb-scenes')!.getAttribute('aria-label'), '场景列表');
  assert.equal(button('本集更多操作'), undefined);
  assert.equal(button('生成视频').closest('[contenteditable]'), null);
  assert.equal(document.querySelector('.sb-save-toolbar'), null);
  assert.equal(document.querySelectorAll('.sb-scene-item[aria-current]').length, 1);
  assert.match(document.querySelector('.sb-scene-item[aria-current]')!.textContent!, /客厅 · 钥匙与决定/);
  assert.equal(document.querySelector('.sb-scenes')!.textContent!.includes('资产库'), false);
  assert.equal(document.querySelector('.sb-video-workspace [aria-label="视频生成模型"]'), null);
  await appendPrompt('留在镜头 01 的局部草稿');
  assert.equal(button('参考资料'), undefined);
  await act(() => document.querySelectorAll<HTMLButtonElement>('.sb-strip-select')[1].click());
  await flush();
  await act(() => document.querySelectorAll<HTMLButtonElement>('.sb-strip-select')[0].click());
  await flush();
  assert.match(
    document.querySelector('[aria-label="视频提示词"]')!.textContent!,
    /留在镜头 01 的局部草稿/,
  );
  assert.equal(document.querySelectorAll('.sb-mention').length, 5);
  assert.equal(writes().length, 0);
});

test('shot strip drags to reorder and hover delete uses the existing confirmation', async () => {
  await mount('normal', '/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_1');
  assert.equal(button('镜头更多操作'), undefined);
  assert.equal(document.querySelector('.sb-filmstrip .jm-status'), null);
  const cards = document.querySelectorAll<HTMLElement>('.sb-strip-card');
  const rect = (left: number, width: number) => ({ left, right: left + width, top: 0, bottom: 80, width, height: 80, x: left, y: 0, toJSON() {} });
  document.querySelector('.sb-strip-track')!.getBoundingClientRect = () => rect(0, 500);
  cards.forEach((card, index) => {
    card.getBoundingClientRect = () => rect(index * 134, 120);
    card.querySelector('button')!.getBoundingClientRect = card.getBoundingClientRect;
  });
  const pointer = async (el: EventTarget, type: string, x: number) => {
    await act(() => {
      const event = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: 40, button: 0 });
      Object.defineProperty(event, 'pointerId', { value: 1 });
      el.dispatchEvent(event);
    });
    await flush();
  };
  await act(() => cards[0].dispatchEvent(new MouseEvent('pointerover', { bubbles: true })));
  assert.equal(cards[0].getAttribute('data-hovered'), 'true');
  await pointer(cards[0].querySelector('button')!, 'pointerdown', 60);
  await pointer(window, 'pointermove', 194);
  assert.equal(cards[0].getAttribute('data-hovered'), 'false');
  assert.ok(document.querySelector('.sb-strip-floating'));
  assert.equal(cards[1].style.transform, 'translate3d(-134px,0,0)');
  assert.equal(writes().length, 0);
  await pointer(window, 'pointerup', 194);
  assert.ok(
    [...document.querySelectorAll('.sb-strip-card')].every(
      (card) => card.getAttribute('data-hovered') === 'false',
    ),
  );
  assert.equal(writes().length, 1);
  assert.deepEqual(writes()[0].body.operations, [
    { op: 'setShotOrder', sceneId: 'scene_1', shotIds: ['shot_2', 'shot_1'] },
  ]);
  await pointer(button('删除镜头 2'), 'pointerdown', 194);
  await act(() => button('删除镜头 2').click());
  await flush();
  assert.equal(writes().length, 1);
  assert.ok(document.querySelector('.sb-confirm-dialog[role=alertdialog]'));
  await act(() => button('删除镜头').click());
  await flush();
  assert.deepEqual(writes()[1].body.operations, [{ op: 'deleteShot', shotId: 'shot_1' }]);
  assert.deepEqual(
    fixture.shots.map((s) => s.id),
    ['shot_2', 'shot_3'],
  );
});

test('inserting between shots saves directly at the anchor and opens the new shot without a second reorder', async () => {
  await mount();
  await act(() => button('在镜头 1 后插入镜头').click());
  await flush();
  await flush();
  const order = fixture.episode('ep_upper').scenes[0].shotIds;
  assert.equal(writes().length, 1);
  assert.equal(writes()[0].body.operations[0].op, 'createShot');
  assert.equal(writes()[0].body.operations[0].afterShotId, 'shot_1');
  assert.equal(order.length, 3);
  assert.equal(order[0], 'shot_1');
  assert.equal(order[2], 'shot_2');
  assert.match(window.location.hash, new RegExp('shotId=' + order[1]));
  assert.equal(document.querySelectorAll('.sb-strip-select').length, 3);
});

test('legacy strip inserts only in shared gaps and appends from the final add card without rewriting existing shots', async () => {
  for (const { scenario, anchor } of [
    { scenario: 'normal', anchor: 'shot_2' },
    { scenario: 'single-shot', anchor: 'shot_1' },
    { scenario: 'empty-scene', anchor: undefined },
  ]) {
    await mount(scenario);
    const original = structuredClone(fixture.shots), revision = fixture.project.revision;
    const originalOrder = fixture.episode('ep_upper').scenes[0].shotIds;
    assert.equal(document.querySelectorAll('.sb-strip-gap-target').length, Math.max(0, originalOrder.length - 1));
    assert.equal(document.querySelector('.sb-strip-insert'), null);
    assert.equal(button('在镜头 1 前插入镜头'), undefined);
    assert.equal(document.querySelectorAll('.sb-strip-add').length, 1);
    await act(() => button('新增').click());
    await flush();
    await flush();
    assert.equal(writes().length, 1);
    assert.equal(fixture.project.revision, revision + 1);
    assert.equal(writes()[0].body.operations.length, 1);
    assert.equal(writes()[0].body.operations[0].afterShotId, anchor);
    assert.equal(writes()[0].body.operations[0].beforeShotId, undefined);
    const order = fixture.episode('ep_upper').scenes[0].shotIds;
    const created = order.find(id => !originalOrder.includes(id));
    assert.deepEqual(order, [...originalOrder, created]);
    assert.match(window.location.hash, new RegExp('shotId=' + created));
    for (const shot of original) assert.deepEqual(fixture.shots.find(s => s.id === shot.id), shot);
  }
});

test('keyboard shot ordering preserves selection and dirty drafts prevent all structure changes', async () => {
  await mount();
  await key(document.querySelector('.sb-strip-select')!, 'ArrowRight', { altKey: true });
  assert.deepEqual(writes()[0].body.operations[0].shotIds, ['shot_2', 'shot_1']);
  await appendPrompt('待保存的正文');
  const count = writes().length;
  assert.ok(button('在镜头 1 后插入镜头').disabled);
  assert.ok(button('删除镜头 1').disabled);
  assert.ok(button('新增').disabled);
  await key(document.querySelector('.sb-strip-select')!, 'ArrowRight', { altKey: true });
  assert.equal(writes().length, count);
});

test('video production uses the project model and never silently replaces an empty or unavailable selection with Mock', async () => {
  await mount();
  fixture.project.settings.videoModel = '';
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  assert.equal(button('生成视频').disabled, true);
  fixture.project.settings.videoModel = 'official-video';
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  assert.equal(button('生成视频').disabled, true);
  fixture.project.settings.videoModel = 'mock_video';
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  assert.equal(button('生成视频').disabled, false);
  await act(() => button('生成视频').click());
  await flush();
  assert.equal(writes().length, 1);
  assert.equal(writes()[0].body.modelId, 'mock_video');
});

test('dismissing parsing details preserves in-flight and unknown index retry protection', async () => {
  await mount('index-failed', '/playlet/p5_free/outline');
  let attempts = 0,
    release: (response: Response) => void = () => {};
  const original = fixture.fetch;
  globalThis.fetch = async (url, options = {}) => {
    if (String(url).endsWith('/episode-index-runs') && options.method === 'POST') {
      attempts++;
      return await new Promise<Response>((resolve) => {
        release = resolve;
      });
    }
    return original(url, options);
  };
  await act(() => button('查看解析进度').click());
  await flush();
  await act(() => {
    button('下一步').click();
    button('下一步').click();
  });
  await flush();
  assert.equal(attempts, 1);
  await act(() => button('关闭').click());
  await flush();
  await act(() => button('查看解析进度').click());
  await flush();
  assert.equal(button('下一步').disabled, true);
  await act(() =>
    release(
      Response.json(
        { code: 'MODEL_RESULT_UNKNOWN', message: '受控未知结果，不能重发' },
        { status: 503 },
      ),
    ),
  );
  await flush();
  assert.equal(button('下一步').disabled, true);
  await act(() => button('关闭').click());
  await flush();
  await act(() => button('查看解析进度').click());
  await flush();
  assert.match(document.querySelector('[role=dialog]')!.textContent!, /受控未知结果/);
  assert.equal(button('下一步').disabled, true);
  assert.equal(attempts, 1);
});

test('reader uses indexed Unicode anchors while preserving complete original text and GET-only navigation', async () => {
  await mount('normal', '/playlet/p5_free/outline');
  const source = await (await fixture.fetch('/api/projects/p5_free/source')).json();
  assert.equal(
    document.querySelector('.editor-script-column pre')!.textContent,
    source.spans.map((s) => s.text).join('\n'),
  );
  assert.equal(document.querySelectorAll('.editor-script-anchor').length, 2);
  assert.equal(
    document.querySelector('.editor-script-anchor')!.getAttribute('data-episode-id'),
    'ep_upper',
  );
  const reader = document.querySelector<HTMLElement>('[aria-label="完整剧本正文"]')!;
  reader.scrollTo = () => {};
  await act(() => button('下集 归途').click());
  await flush();
  assert.equal(
    document.querySelector('.editor-script-outline [aria-current]')!.getAttribute('aria-label'),
    '下集 归途',
  );
  assert.equal(
    document.querySelector('.editor-script-column pre')!.textContent,
    source.spans.map((s) => s.text).join('\n'),
  );
  assert.equal(writes().length, 0);
});

test('asset picker uses thumbnails and never preloads the original image', async () => {
  await mount();
  const look = fixture.project.assets[0].looks[0];
  look.image = {
    fileId: 'file_' + 'c'.repeat(32),
    thumbnailUrl: '/api/files/file_' + 'c'.repeat(32) + '/thumbnail',
    originalUrl: '/api/files/file_' + 'c'.repeat(32) + '/original',
  };
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  await act(() => button('添加素材').click());
  await flush();
  assert.ok(
    document.querySelector(
      '.sb-picker-card img[src="' + look.image.thumbnailUrl + '?audience=workspace"]',
    ),
  );
  assert.equal(document.querySelector('.sb-picker-card a'), null);
  assert.equal(document.querySelector('.sb-picker-card details'), null);
  assert.ok(
    [...document.querySelectorAll('img')].every(
      (img) => !img.getAttribute('src')?.split('?')[0].endsWith('/original'),
    ),
  );
  assert.equal(writes().length, 0);
});

test('inline image mentions refresh by exact Look, stay atomic and never save image presentation into prompt parts', async () => {
  await mount();
  const look = fixture.project.assets[0].looks[0];
  look.image = {
    fileId: 'file_' + 'd'.repeat(32),
    thumbnailUrl: '/api/files/file_' + 'd'.repeat(32) + '/thumbnail',
    originalUrl: '/api/files/file_' + 'd'.repeat(32) + '/original',
  };
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  const editor = document.querySelector<HTMLElement>('[aria-label="视频提示词"]')!,
    tokens = [...editor.querySelectorAll<HTMLElement>('.sb-mention')],
    rain = tokens.find((t) => t.textContent === '林晚 · 雨衣')!;
  assert.equal(editor.querySelectorAll('.sb-mention img').length, 2);
  assert.equal(document.querySelectorAll('.sb-referenced-asset[data-look-id="look_rain"]').length, 1);
  assert.equal(document.querySelector('.sb-referenced-asset[data-look-id="look_rain"] img')!.getAttribute('src'), look.image.thumbnailUrl + '?audience=workspace');
  assert.equal(
    rain.querySelector('img')!.getAttribute('src'),
    look.image.thumbnailUrl + '?audience=workspace',
  );
  assert.equal(rain.contentEditable, 'false');
  assert.ok(
    tokens.filter((t) => t.textContent === '林晚 · 基础').every((t) => !t.querySelector('img')),
  );
  const label = rain.querySelector('.sb-mention-label')!;
  await act(() => {
    editor.focus();
    const range = document.createRange();
    range.selectNodeContents(label);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
  });
  let copied = '';
  const copy = new Event('copy', { bubbles: true, cancelable: true });
  Object.defineProperty(copy, 'clipboardData', {
    value: {
      setData(_type: string, text: string) {
        copied = text;
      },
    },
  });
  await act(() => editor.dispatchEvent(copy));
  assert.equal(copied, '@林晚 · 雨衣');
  await key(editor, 'Backspace');
  assert.equal(editor.querySelectorAll('.sb-mention').length, 4);
  await key(editor, 'z', { ctrlKey: true });
  assert.equal(editor.querySelectorAll('.sb-mention img').length, 2);
  await appendPrompt('有图引用仍保留正文');
  await saved();
  assert.equal(writes().length, 1);
  for (const part of writes()[0].body.operations[0].patch.videoPrompt.parts.filter(
    (p) => p.type === 'asset',
  ))
    assert.deepEqual(
      Object.keys(part).sort(),
      part.lookId ? ['assetId', 'lookId', 'type'] : ['assetId', 'type'],
    );
  const newer = '/api/files/file_' + 'e'.repeat(32) + '/thumbnail';
  look.image.thumbnailUrl = newer;
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  assert.equal(
    editor.querySelector('.sb-mention img')!.getAttribute('src'),
    newer + '?audience=workspace',
  );
  const img = editor.querySelector<HTMLImageElement>('.sb-mention img')!;
  await act(() => img.dispatchEvent(new Event('error')));
  assert.equal(img.hidden, true);
  assert.equal(img.parentElement!.textContent, '林晚 · 雨衣');
  assert.equal(writes().length, 1);
});

test('mention hover previews the exact protected image outside the prompt without changing parts or selection', async () => {
  await mount();
  const asset = fixture.project.assets[0], look = asset.looks[0];
  const image = (letter: string) => ({
    fileId: 'file_' + letter.repeat(32),
    thumbnailUrl: '/api/files/file_' + letter.repeat(32) + '/thumbnail',
    originalUrl: '/api/files/file_' + letter.repeat(32) + '/original',
  });
  asset.image = image('a');
  look.image = image('b');
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  const editor = document.querySelector<HTMLElement>('[aria-label="视频提示词"]')!;
  const rain = [...editor.querySelectorAll<HTMLElement>('.sb-mention')].find(t => t.textContent === '林晚 · 雨衣')!;
  const before = structuredClone(fixture.shots[0].videoPrompt.parts);
  await act(() => {
    editor.focus();
    const range = document.createRange();
    range.setStart(editor, 0);
    range.collapse(true);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
    rain.dispatchEvent(new MouseEvent('pointerover', { bubbles: true }));
  });
  let preview = document.querySelector<HTMLElement>('.sb-mention-image-preview')!;
  assert.ok(preview);
  assert.equal(editor.contains(preview), false);
  assert.equal(preview.querySelector('img')!.getAttribute('src'), look.image.thumbnailUrl + '?audience=workspace');
  assert.equal(rain.getAttribute('aria-describedby'), preview.id);
  assert.equal(rain.getAttribute('title'), null);
  await act(() => preview.querySelector('img')!.dispatchEvent(new Event('load')));
  assert.equal(preview.style.visibility, 'visible');
  assert.equal(window.getSelection()!.anchorNode, editor);
  assert.equal(window.getSelection()!.anchorOffset, 0);
  await act(() => rain.dispatchEvent(new MouseEvent('pointerout', { bubbles: true, relatedTarget: rain.querySelector('.sb-mention-label') })));
  assert.equal(document.querySelector('.sb-mention-image-preview'), preview);
  await act(() => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
  assert.equal(document.querySelector('.sb-mention-image-preview'), null);
  assert.equal(rain.getAttribute('aria-describedby'), null);
  await act(() => rain.focus());
  assert.ok(document.querySelector('.sb-mention-image-preview'));
  await act(() => document.dispatchEvent(new Event('scroll')));
  assert.equal(document.querySelector('.sb-mention-image-preview'), null);
  await act(() => rain.dispatchEvent(new MouseEvent('pointerover', { bubbles: true })));
  await act(() => document.querySelector('.sb-mention-image-preview img')!.dispatchEvent(new Event('error')));
  assert.equal(document.querySelector('.sb-mention-image-preview'), null);
  look.image = undefined;
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  await act(() => rain.dispatchEvent(new MouseEvent('pointerover', { bubbles: true })));
  assert.equal(document.querySelector('.sb-mention-image-preview'), null, 'a Look without an image cannot borrow the base image');
  assert.deepEqual(fixture.shots[0].videoPrompt.parts, before);
  await saved();
  assert.equal(writes().length, 0);
});

test('episode directory preserves original titles and opens only the chosen episode without starting a task', async () => {
  await mount('normal', '/playlet/p5_free/episodes');
  const cards = document.querySelectorAll('.sb-episode-card');
  assert.equal(cards.length, 2);
  assert.equal(cards[1].querySelector('a')!.className, 'sb-episode-open');
  assert.equal(document.querySelector('.sb-episodes-heading a'), null);
  assert.ok(document.querySelector('.xy-workflow-dock'));
  assert.match(cards[0].textContent!, /上集 重逢/);
  assert.match(cards[1].textContent!, /下集 归途/);
  assert.doesNotMatch(document.body.textContent!, /第1集|第2集/);
  assert.match(cards[0].textContent!, /2 角色.*1 场景.*2 分镜/s);
  assert.equal(cards[0].querySelector('.sb-episode-enter')!.textContent, '编辑');
  assert.match(document.querySelector('.sb-episodes-subtitle')!.textContent!, /共 2 集.*价格待确认/s);
  assert.equal(fixture.requests.some(r => /\/storyboard\/episodes\//.test(r.path)), false);
  await act(() => cards[1].querySelector<HTMLAnchorElement>('a')!.click());
  await flush();
  assert.match(window.location.hash, /production\?episodeId=ep_lower/);
  assert.match(document.querySelector('.sb-scenes')!.textContent!, /站台 · 归途/);
  assert.equal(document.querySelector('.sb-scene-item small'), null);
  assert.equal(document.querySelector('[aria-label="返回分集视频"] svg'), null);
  await act(() => document.querySelector<HTMLAnchorElement>('[aria-label="返回分集视频"]')!.click());
  await flush();
  assert.match(window.location.hash, /episodes/);
  await navigate('/playlet/p5_free/production');
  assert.match(window.location.hash, /episodes/);
  assert.equal(writes().length, 0);
});

test('episode header reads the selected analysis price and its free period without inventing a reference price', async () => {
  await mount('normal', '/playlet/p5_free/episodes');
  let free = false;
  globalThis.fetch = async (url, options = {}) => {
    const response = await fixture.fetch(url, options);
    if (String(url).endsWith('/api/models')) {
      const models = await response.json();
      models.analysis[0] = { ...models.analysis[0], billingUnit: 'characters-100', free,
        creditPrice: { unitPrice: 7, freeEnabled: false, freeStart: null, freeEnd: null } };
      return Response.json(models);
    }
    return response;
  };
  await act(() => context.refreshModels());
  await flush();
  assert.match(document.querySelector('.sb-episodes-price')!.textContent!, /100字符消耗 7 积分/);
  free = true;
  await act(() => context.refreshModels());
  await flush();
  // 限免只显示活动标签，不再拼出“0 积分（限免）”。
  const freePrice = document.querySelector('.sb-episodes-price')!;
  assert.equal(freePrice.querySelector('.xy-model-free-badge')?.textContent, '限免');
  assert.doesNotMatch(freePrice.textContent!, /积分/);
  const { default: i18n } = await import('../src/i18n');
  try {
    await act(async () => i18n.changeLanguage('en'));
    await flush();
    assert.equal(
      document.querySelector('.sb-episodes-price .xy-model-free-badge')?.textContent,
      'Free',
    );
    assert.doesNotMatch(document.querySelector('.sb-episodes-price')!.textContent!, /[积限免]/);
  } finally {
    await act(async () => i18n.changeLanguage('zh'));
    await flush();
  }
  assert.equal(writes().length, 0);
});

test('breakdown dialog explains all modes, closes without generation, and submits the chosen mode', async () => {
  for (const mode of ['general', 'story', 'production']) {
    await mount('episode-ready', '/playlet/p5_free/episodes');
    await act(() => button('拆解分镜').click());
    await flush();
    const dialog = document.querySelector('[role=dialog]')!;
    assert.match(dialog.textContent!, /剧情清楚，节奏自然/);
    assert.match(dialog.textContent!, /突出冲突、情绪与人物反应/);
    assert.match(dialog.textContent!, /空间清楚，动作连续/);
    assert.doesNotMatch(dialog.textContent!, /Mock Video|每镜最长|按内容估算|三种模式均保留/);
    assert.match(dialog.textContent!, /场景数和分镜数由内容决定/);
    assert.equal(document.querySelector<HTMLInputElement>('input[value=general]')!.checked, true);
    assert.equal(writes().length, 0);
    await act(() => button('关闭').click());
    await flush();
    assert.equal(writes().length, 0);
    await act(() => button('拆解分镜').click());
    await flush();
    await act(() => document.querySelector<HTMLInputElement>(`input[value=${mode}]`)!.click());
    await act(() => button('开始拆解').click());
    await flush();
    assert.equal(writes().length, 1);
    assert.equal(writes()[0].body.breakdownMode, mode);
    assert.match(window.location.hash, /production\?episodeId=ep_upper/);
    assert.match(document.querySelector('.sb-run-mode')!.textContent!, /每镜最长 10 秒/);
  }
});

test('changing breakdown mode refreshes its real quote and hides late quotes from the previous mode', async () => {
  if (root) await act(() => root!.unmount());
  fixture = createStoryboardFixture();
  fixture.setScenario('episode-ready');
  const index = await (await fixture.fetch('/api/projects/p5_free/storyboard')).json();
  const models = await (await fixture.fetch('/api/models')).json();
  const { BreakdownDialog } = await import('../src/pages/production/BreakdownDialog');
  const { SessionOptionalContext } = await import('../src/state/SessionContext');
  const quoted: string[] = [], accepted: string[] = [];
  let releaseStory: (() => void) | undefined;
  globalThis.fetch = async (url, options) => {
    assert.match(String(url), /\/quote$/);
    const body = JSON.parse(String(options?.body));
    quoted.push(body.breakdownMode);
    if (body.breakdownMode === 'story') await new Promise<void>(resolve => { releaseStory = resolve; });
    return Response.json({ basis: 'quote-' + body.breakdownMode, modelId: 'analysis', connectionId: 'free',
      scope: 'episode', sourceVersion: 1, inputRevision: index.revision, quantity: 100, unit: 'characters',
      priceVersion: 1, rule: { unitPrice: 2, freeEnabled: false, freeStart: null, freeEnd: null },
      originalAmount: 2, amount: 2, free: false });
  };
  const session = { user: { id: 'isolated-user' }, epoch: 1 } as NonNullable<React.ContextType<typeof SessionOptionalContext>>;
  root = createRoot(document.getElementById('root')!);
  await act(() => root!.render(<LanguageProvider><SessionOptionalContext.Provider value={session}>
    <BreakdownDialog episode={index.index.episodes[0]} index={index} videoModelId="mock_video" videoModel={models.video[0]}
      busy={false} canStart priceSequence={0} error="" onClose={() => {}}
      onStart={(mode, basis) => accepted.push(mode + ':' + basis)} />
  </SessionOptionalContext.Provider></LanguageProvider>));
  await flush();
  assert.equal(document.querySelector<HTMLButtonElement>('.sb-breakdown-footer button:last-child')!.disabled, false);
  await act(() => document.querySelector<HTMLInputElement>('input[value=story]')!.click());
  assert.equal(document.querySelector<HTMLButtonElement>('.sb-breakdown-footer button:last-child')!.disabled, true);
  await act(() => document.querySelector<HTMLInputElement>('input[value=production]')!.click());
  await flush();
  await act(() => { releaseStory?.(); });
  await flush();
  await act(() => document.querySelector<HTMLButtonElement>('.sb-breakdown-footer button:last-child')!.click());
  assert.equal(document.querySelector('.sb-breakdown-footer .button-credit-amount')?.textContent, '2');
  assert.doesNotMatch(document.querySelector('.sb-breakdown-footer')!.textContent!, /字 ×|积分\//);
  assert.deepEqual(quoted, ['general', 'story', 'production']);
  assert.deepEqual(accepted, ['production:quote-production']);
});

test('episode card starts the selected second episode once, and other active or readonly cards cannot start', async () => {
  await mount('episode-ready', '/playlet/p5_free/episodes');
  const start = document.querySelectorAll<HTMLButtonElement>('.sb-episode-actions>button')[1];
  await act(() => { start.click(); start.click(); });
  await flush();
  assert.equal(writes().length, 0);
  assert.equal(document.querySelector<HTMLInputElement>('input[value=general]')!.checked, true);
  await act(() => { button('开始拆解').click(); button('开始拆解')?.click(); });
  await flush();
  assert.equal(writes().length, 1);
  assert.match(writes()[0].path, /episodes\/ep_lower\/runs$/);
  assert.equal(writes()[0].body.operation, 'breakdown');
  assert.equal(writes()[0].body.breakdownMode, 'general');
  assert.match(window.location.hash, /episodeId=ep_lower/);
  assert.match(document.querySelector('.sb-run-feedback')!.textContent!, /正在生成下集 归途/);
  await navigate('/playlet/p5_free/episodes');
  await flush();
  assert.equal(button('拆解分镜').disabled, true);
  await act(() => button('拆解分镜').click());
  assert.equal(writes().length, 1);
  await mount('read-only', '/playlet/p5_free/episodes');
  assert.ok(document.querySelector('.sb-episode-open'));
  fixture.setScenario('episode-ready');
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  assert.equal(button('拆解分镜').disabled, true);
  await act(() => button('拆解分镜').click());
  assert.equal(writes().length, 0);
});

test('an episode start with a lost reply is read back and never resubmitted by the directory', async () => {
  await mount('episode-ready', '/playlet/p5_free/episodes');
  globalThis.fetch = async (url, options = {}) => {
    const response = await fixture.fetch(url, options);
    if (options.method === 'POST' && String(url).endsWith('/runs'))
      throw new Error('受控启动回执丢失');
    return response;
  };
  await act(() => button('拆解分镜').click());
  await flush();
  await act(() => button('开始拆解').click());
  await flush();
  assert.match(window.location.hash, /episodes/);
  assert.equal(writes().length, 1);
  assert.match(document.body.textContent!, /受理结果尚未确认/);
  assert.ok(document.querySelector('.sb-episode-open'));
  assert.equal(button('拆解分镜').disabled, true);
  await act(() => button('重新读取').click());
  await flush();
  assert.equal(writes().length, 1);
  assert.match(document.querySelector('.sb-episode-state')!.textContent!, /拆解中/);
});

test('starting an empty episode shows live generation, saved output and review, then notifies once without extra starts', async () => {
  const successes: string[] = [],
    original = toast.success;
  toast.success = (message) => {
    successes.push(message);
    return 'episode-test';
  };
  try {
    await mount('episode-ready', '/playlet/p5_free/episodes');
    const generate = button('拆解分镜');
    await act(() => {
      generate.click();
      generate.click();
    });
    await flush();
    assert.equal(writes().length, 0);
    await act(() => button('开始拆解').click());
    await flush();
    assert.equal(writes().length, 1);
    assert.equal(writes()[0].body.operation, 'breakdown');
    let panel = document.querySelector('.sb-run-feedback')!;
    assert.ok(panel);
    assert.equal(panel.getAttribute('data-expanded'), 'true');
    assert.match(panel.textContent!, /正在生成上集 重逢/);
    assert.equal(panel.querySelector('.sb-run-results'), null);
    assert.equal(document.body.textContent!.includes('本集尚未拆解'), false);
    assert.ok(document.querySelector('.sb-run-phases [data-state=active] .jm-spinner'));
    assert.ok(document.querySelector('.sb-run-phases [data-state=active] .jm-spinner'));
    assert.equal(panel.querySelector('[role=progressbar]'), null);
    assert.ok(panel.querySelector('.sb-run-phases li[data-state=active]'));
    assert.equal(panel.textContent!.includes('%'), false);
    assert.equal(panel.querySelector('a[href*="/process"]'), null);
    fixture.setScenario('episode-scene-only');
    await act(() => context.refreshCurrentProject('p5_free'));
    await flush();
    // 运行中已保存的场景只是中间结果，只展示进度卡。
    assert.equal(document.querySelector('.sb-run-feedback')!.getAttribute('data-expanded'), 'true');
    assert.equal(document.querySelectorAll('.sb-scene-item').length, 0);
    assert.match(document.body.textContent!, /本集生成完成后显示场景/);
    assert.equal(
      document.body.textContent!.includes('可返回分集页拆解，或新增一个连续镜头。'),
      false,
    );
    fixture.setScenario('episode-partial');
    await act(() => context.refreshCurrentProject('p5_free'));
    await flush();
    panel = document.querySelector('.sb-run-feedback')!;
    assert.equal(panel.getAttribute('data-expanded'), 'true');
    assert.equal(panel.textContent!.includes('当前已保存'), false);
    assert.equal(document.querySelector('[aria-label="视频提示词"]'), null);
    assert.equal(document.querySelectorAll('.sb-scene-item').length, 0);
    fixture.setScenario('episode-review');
    await act(() => context.refreshCurrentProject('p5_free'));
    await flush();
    assert.match(document.querySelector('.sb-run-feedback')!.textContent!, /正在独立复核上集 重逢/);
    assert.equal(document.querySelectorAll('.sb-run-phases li[data-state=done]').length, 1);
    fixture.complete();
    await act(() => context.refreshCurrentProject('p5_free'));
    await flush();
    assert.equal(document.querySelector('.sb-run-feedback'), null);
    assert.equal(successes.length, 1);
    assert.match(successes[0], /上集 重逢生成完成，已保存 1 个场景、2 个镜头/);
    await act(() => context.refreshCurrentProject('p5_free'));
    await flush();
    assert.equal(successes.length, 1);
    assert.equal(writes().length, 1);
  } finally {
    toast.success = original;
  }
});

test('accepted generation stays visible when subsequent material readback fails', async () => {
  await mount('episode-ready', '/playlet/p5_free/episodes');
  let accepted = false;
  globalThis.fetch = async (url, options = {}) => {
    if (
      accepted &&
      String(url).includes('/storyboard/episodes/') &&
      String(url).endsWith('/source')
    )
      throw new Error('fixture source read failed');
    const response = await fixture.fetch(url, options);
    if (options.method === 'POST' && String(url).endsWith('/runs')) accepted = true;
    return response;
  };
  await act(() => button('拆解分镜').click());
  await flush();
  await act(() => button('开始拆解').click());
  await flush();
  assert.match(document.querySelector('.sb-run-feedback')!.textContent!, /正在生成上集 重逢/);
  assert.ok(document.querySelector('[role=alert]'));
  assert.ok(document.querySelector('.sb-run-phases [data-state=active] .jm-spinner'));
  assert.equal(writes().length, 1);
  assert.equal(button('生成本集'), undefined);
  await flush();
  assert.equal(writes().length, 1);
});

test('cancelling empty episode retains an explicit stopped state instead of the initial generation invitation', async () => {
  await mount('episode-running');
  assert.equal(writes().length, 0);
  await act(() => button('取消生成').click());
  await flush();
  const panel = document.querySelector('.sb-run-feedback')!;
  assert.equal(panel.getAttribute('data-state'), 'cancelled');
  assert.match(panel.textContent!, /生成已取消/);
  assert.match(panel.textContent!, /已保存的内容会保留/);
  assert.equal(document.body.textContent!.includes('本集尚未拆解'), false);
  assert.equal(panel.querySelector('[role=progressbar]'), null);
  assert.equal(writes().length, 1);
  assert.match(writes()[0].path, /\/cancel$/);
});

test('run feedback rejects another episode, another contract and stale source without inventing progress', async () => {
  const f = createStoryboardFixture(),
    v = f.episode('ep_upper') as EpisodeView;
  const active = { ...v.latestRun!, status: 'running' };
  assert.equal(
    currentEpisodeRun({
      ...v,
      latestRun: null,
      activeRun: { ...active, storyboardScope: { operation: 'breakdown', episodeId: 'ep_lower' } },
    }),
    null,
  );
  assert.equal(
    currentEpisodeRun({
      ...v,
      latestRun: null,
      activeRun: { ...active, contract: 'asset-preparation-v1' },
    }),
    null,
  );
  assert.equal(
    currentEpisodeRun({ ...v, latestRun: { ...active, sourceVersion: 0 }, activeRun: null }),
    null,
  );
  await mount('episode-running');
  await navigate('/playlet/p5_free/production?episodeId=ep_lower');
  assert.equal(document.querySelector('.sb-run-feedback'), null);
  assert.match(document.body.textContent!, /项目有其他任务正在运行/);
  assert.equal(writes().length, 0);
});

test('video Mock UI starts once, persists history, previews a real file URL and explicitly adopts', async () => {
  await mount();
  assert.equal(button('选择视频'), undefined);
  assert.equal(document.querySelector('.sb-strip-pending')!.textContent, '待生成');
  assert.equal(document.querySelector('.sb-strip-duration'), null);
  assert.ok(document.querySelector('.media-video-preview'));
  assert.equal(document.querySelector('[aria-label="镜头动作"]'), null);
  await act(() => button('生成视频').click());
  await flush();
  assert.equal(writes().length, 1);
  assert.match(writes()[0].path, /media-tasks$/);
  assert.deepEqual(writes()[0].body.target, { episodeId: 'ep_upper', shotId: 'shot_1' });
  assert.equal(button('生成视频').disabled, true);
  assert.match(document.querySelector('.media-video-preview .media-video-progress')!.textContent!, /排队中/);
  assert.equal(document.querySelector('.media-live'), null);
  assert.equal(button('选择视频'), undefined);
  assert.equal(document.querySelector('.media-video-progress')!.getAttribute('aria-busy'), 'true');
  assert.equal(document.querySelector('.media-video-progress [role=progressbar]')!.hasAttribute('aria-valuenow'), false);
  assert.equal(document.querySelector('.sb-strip-select[aria-busy=true] .sb-strip-task')?.textContent, '排队中');
  fixture.mediaTasks[0].status = 'unknown';
  fixture.mediaTasks[0].revision++;
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(document.querySelector('.media-video-progress')!.getAttribute('aria-busy'), 'false');
  assert.equal(document.querySelector('.media-video-activity'), null);
  fixture.mediaTasks[0].status = 'queued';
  fixture.completeMedia();
  fixture.mediaTasks[0].result.files[0].durationSeconds = 10.793333;
  assert.equal(button('刷新生成记录'), undefined);
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.ok(document.querySelector('video[src$="/file?audience=workspace"]'));
  assert.equal(document.querySelector('.media-video-progress'), null);
  assert.ok(document.querySelector('.sb-strip-cover[src$="/thumbnail?audience=workspace"]'));
  assert.equal(document.querySelector('.sb-strip-duration')!.textContent, '00:10');
  assert.equal(document.querySelector('.sb-strip-task'), null);
  assert.equal(document.querySelector('.sb-strip-select[aria-busy=true]'), null);
  assert.equal(document.querySelector('.sb-strip-number')!.textContent, '1');
  assert.equal(
    document.querySelector('video')!.getAttribute('poster'),
    document.querySelector('.sb-strip-cover')!.getAttribute('src'),
  );
  assert.equal(document.querySelector('.media-history'), null);
  assert.equal(fixture.mediaTasks[0].adoptedOutputId, '');
  await act(() => button('选择视频').click());
  await flush();
  assert.equal(writes().length, 1);
  assert.ok(document.querySelector('[role=radio][aria-checked=true]'));
  assert.equal(document.querySelector('.media-version-tasks'), null);
  assert.doesNotMatch(document.querySelector('[role=dialog]')!.textContent!, /任务详情/);
  await act(() => button('确认').click());
  await flush();
  assert.ok(fixture.mediaTasks[0].adoptedOutputId);
  assert.equal(writes().length, 2);
  await navigate('/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_2');
  assert.equal(document.querySelector('video[src$="/file?audience=workspace"]'), null);
  await navigate('/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_1');
  assert.ok(document.querySelector('video[src$="/file?audience=workspace"]'));
  assert.equal(writes().length, 2);
});
test('shot strip follows background video tasks across shots and remounts without another generation', async () => {
  await mount();
  await act(() => button('生成视频').click());
  await flush();
  const task = fixture.mediaTasks[0];
  const card = () => document.querySelector<HTMLButtonElement>('.sb-strip-select[aria-label^="镜头 1"]')!;
  assert.match(card().textContent!, /排队中/);
  await navigate('/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_2');
  task.status = 'running';
  task.revision++;
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.match(card().textContent!, /生成中/);
  assert.equal(card().getAttribute('aria-busy'), 'true');
  assert.equal(document.querySelectorAll('.sb-strip-task').length, 1);
  // 刷新整个组件树，保留相同的持久夹具，验证不依赖生成器内存。
  await act(() => root!.unmount());
  root = createRoot(document.getElementById('root')!);
  await act(() => root!.render(<LanguageProvider><HashRouter><WorkspaceProvider><Probe /><MainWorkspace /></WorkspaceProvider></HashRouter></LanguageProvider>));
  await flush();
  assert.match(card().textContent!, /生成中/);
  task.status = 'unknown';
  task.revision++;
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.match(card().textContent!, /待确认/);
  assert.equal(card().getAttribute('aria-busy'), null);
  assert.equal(card().querySelector('.animate-spin'), null);
  task.status = 'running';
  fixture.completeMedia();
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(card().querySelector('.sb-strip-task'), null);
  assert.ok(card().querySelector('img[src$="/thumbnail?audience=workspace"]'));
  assert.equal(card().querySelector('.sb-strip-duration')?.textContent, '00:10');
  assert.equal(document.querySelector('.sb-strip-select[aria-label^="镜头 2"] img'), null);
  assert.equal(document.querySelector('.media-video-preview video'), null);
  await navigate('/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_1');
  assert.equal(document.querySelector('.media-video-preview video')?.getAttribute('poster'), card().querySelector('img')?.getAttribute('src'));
  assert.equal(writes().length, 1);
  assert.equal(fixture.mediaTasks[0].adoptedOutputId, '');
});

test('project SSE updates a background shot cover without focus, navigation or another project revision', async () => {
  await mount();
  await act(() => button('生成视频').click());
  await flush();
  await navigate('/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_2');
  await act(() => root!.unmount());
  const streams: { controller: ReadableStreamDefaultController<Uint8Array>; signal: AbortSignal }[] = [];
  globalThis.fetch = async (url, options = {}) => {
    if (String(url).endsWith('/api/projects/p5_free/events')) {
      return new Response(new ReadableStream<Uint8Array>({ start(controller) {
        streams.push({ controller, signal: options.signal! });
        options.signal?.addEventListener('abort', () => controller.close(), { once: true });
        controller.enqueue(new TextEncoder().encode('event: project.ready\ndata: {}\n\n'));
      } }), { headers: { 'Content-Type': 'text/event-stream' } });
    }
    return fixture.fetch(url, options);
  };
  const service = createAdministrationFixture().service;
  const session = { user: await service.current(), epoch: sessionBoundary.epoch, service } as NonNullable<React.ContextType<typeof SessionOptionalContext>>;
  root = createRoot(document.getElementById('root')!);
  await act(async () => {
    root!.render(<SessionOptionalContext.Provider value={session}><LanguageProvider><HashRouter><WorkspaceProvider><Probe /><MainWorkspace /></WorkspaceProvider></HashRouter></LanguageProvider></SessionOptionalContext.Provider>);
    await new Promise(resolve => setTimeout(resolve, 20));
  });
  const revision = fixture.project.revision;
  await flush();
  assert.ok(streams.length);
  fixture.completeMedia();
  await act(async () => {
    for (const { controller, signal } of streams) if (!signal.aborted)
      controller.enqueue(new TextEncoder().encode('event: project.changed\ndata: {}\n\n'));
    await new Promise(resolve => setTimeout(resolve, 160));
  });
  await flush();
  assert.ok(document.querySelector('.sb-strip-select[aria-label^="镜头 1"] .sb-strip-cover'));
  assert.equal(document.querySelector('.sb-strip-task'), null);
  assert.equal(document.querySelector('.media-video-preview video'), null);
  assert.equal(fixture.project.revision, revision);
  assert.equal(writes().filter(request => request.path.endsWith('/media-tasks')).length, 1);
});

test('failed or cancelled regeneration stops strip loading and retains the previous successful video', async () => {
  await mount();
  await act(() => button('生成视频').click());
  await flush();
  fixture.completeMedia();
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  const cover = document.querySelector('.sb-strip-cover')!.getAttribute('src');
  await act(() => button('重新生成').click());
  await flush();
  assert.equal(document.querySelector('.sb-strip-cover')!.getAttribute('src'), cover);
  assert.match(document.querySelector('.sb-strip-task')!.textContent!, /排队中/);
  assert.equal(document.querySelector('.media-video-progress')?.getAttribute('data-has-output'), 'true');
  assert.ok(document.querySelector('.media-video-preview video'));
  assert.equal(document.querySelector('.media-live'), null);
  fixture.completeMedia('failed');
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.match(document.querySelector('.sb-strip-task')!.textContent!, /生成失败/);
  assert.equal(document.querySelector('.media-video-error h3')?.textContent, '本次生成失败');
  assert.equal(document.querySelector('.media-video-error')?.getAttribute('data-has-output'), 'true');
  assert.ok(document.querySelector('.media-video-preview video'));
  assert.ok(button('选择视频'));
  assert.equal(document.querySelector('.sb-strip-select[aria-busy=true]'), null);
  assert.equal(document.querySelector('.sb-strip-cover')!.getAttribute('src'), cover);
  await act(() => button('重新生成').click());
  await flush();
  await act(() => button('取消生成').click());
  await flush();
  assert.equal(document.querySelector('.sb-strip-task'), null);
  assert.equal(document.querySelector('.sb-strip-cover')!.getAttribute('src'), cover);
  assert.equal(fixture.mediaTasks[0].status, 'cancelled');
  assert.equal(writes().length, 4);
});

test('first video failure replaces the waiting placeholder with an explicit failure and does not retry automatically', async () => {
  await mount();
  await act(() => button('生成视频').click());
  await flush();
  fixture.completeMedia('failed');
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  const preview = document.querySelector('.media-video-preview')!;
  assert.equal(preview.querySelector('.media-preview-empty'), null);
  assert.equal(preview.querySelector('[role=progressbar]'), null);
  assert.equal(preview.querySelector('.media-video-error h3')?.textContent, '生成失败');
  assert.equal(preview.querySelector('.media-video-error')?.getAttribute('data-has-output'), 'false');
  assert.match(preview.querySelector('[role=alert]')!.textContent!, /受控免费生成失败/);
  assert.doesNotMatch(preview.textContent!, /等待生成视频|正在生成/);
  assert.ok(button('生成视频'));
  assert.equal(button('重新生成'), undefined);
  assert.equal(button('选择视频'), undefined);
  assert.equal(writes().length, 1);
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(writes().length, 1);
});

test('asset image generation is available after breakdown and previews thumbnails before opening originals', async () => {
  await mount('normal', '/playlet/p5_free/characters');
  await act(() => document.querySelector<HTMLButtonElement>('.asset-catalog-card')!.click());
  await flush();
  await act(() => button('生成图片').click());
  await flush();
  await act(() =>
    [...document.querySelectorAll<HTMLButtonElement>('.media-generator-image button')]
      .find((b) => b.textContent?.trim() === '生成图片')!
      .click(),
  );
  await flush();
  assert.equal(writes().length, 1);
  assert.equal(writes()[0].body.target.assetId, 'character_1');
  fixture.completeMedia();
  assert.equal(button('刷新生成记录'), undefined);
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(document.querySelector('.media-history'), null);
  assert.doesNotMatch(document.querySelector('.media-generation-dialog')!.textContent!, /生成记录|尚无生成记录|Mock 返回/);
  await act(() => document.querySelector<HTMLButtonElement>('.media-generation-dialog button[aria-label="关闭"]')!.click());
  await flush();
  await act(() => button('生成历史').click());
  await flush();
  assert.ok(document.querySelector('.image-finals-dialog img[src$="/thumbnail?audience=workspace"]'));
  assert.equal(document.querySelector('.media-preview-dialog img'), null);
  await act(() => button('预览图片 1').click());
  await flush();
  assert.ok(document.querySelector('.media-preview-dialog img[src$="/file?audience=workspace"]'));
  await act(() =>
    document
      .querySelector<HTMLButtonElement>('.media-preview-dialog button[aria-label="关闭"]')!
      .click(),
  );
  await flush();
  await act(() => document.querySelector<HTMLButtonElement>('.image-finals-dialog footer button')!.click());
  await flush();
  assert.equal(fixture.project.assets[0].image, undefined);
  assert.equal(writes().length, 1);
  fixture.mediaTasks[0].stale = true;
  const originalFetch = globalThis.fetch;
  let finishHistoryRead: () => Promise<void>;
  globalThis.fetch = (url, init) => String(url).includes('/media-tasks?')
    ? new Promise(resolve => { finishHistoryRead = async () => resolve(await originalFetch(url, init)); })
    : originalFetch(url, init);
  await act(() => button('生成历史').click());
  await flush();
  assert.equal(document.querySelectorAll('.image-finals-dialog [role=radio]').length, 1, 'reopening retains the known card while refreshing');
  assert.equal(document.querySelector('.image-finals-dialog .image-final-loading'), null);
  await act(async () => finishHistoryRead());
  globalThis.fetch = originalFetch;
  await flush();
  assert.equal(button('确认定稿').disabled, true);
  fixture.mediaTasks[0].stale = false;
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  await act(() => button('确认定稿').click());
  await flush();
  assert.ok(fixture.project.assets[0].image);
  assert.equal(writes().length, 2);
});

test('image actions show highest-tier credits, regenerate only the pictured target and collapse source by default', async () => {
  await mount('normal', '/playlet/p5_free/characters');
  fixture.project.settings.imageModel = 'mock-image';
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  await act(() => document.querySelector<HTMLButtonElement>('.asset-catalog-card')!.click());
  await flush();
  const quotes = () => fixture.requests.filter(r => r.path.endsWith('/quote'));
  assert.equal(quotes().length, 0);
  assert.equal(button('生成图片').querySelector('.button-credit-amount')?.textContent, '12');
  const source = document.querySelector<HTMLDetailsElement>('.asset-source-disclosure')!;
  assert.equal(source.open, false);
  await act(() => source.querySelector('summary')!.click());
  assert.equal(source.open, true);
  assert.match(source.textContent!, /你自己决定/);
  fixture.project.assets[0].image = {
    fileId: 'file_' + 'c'.repeat(32),
    thumbnailUrl: '/api/files/file_' + 'c'.repeat(32) + '/thumbnail',
    originalUrl: '/api/files/file_' + 'c'.repeat(32) + '/original',
  };
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  assert.equal(button('重新生成').querySelector('.button-credit-amount')?.textContent, '12');
  assert.equal(source.open, true, 'same-asset refresh preserves the expanded source');
  assert.ok(document.querySelector('.asset-look-detail button[aria-label="生成图片"]'), 'the unpictured Look still offers generation');
  assert.equal(quotes().length, 0);
  assert.ok(button('生成历史'));
  assert.equal(button('选择定稿'), undefined);
  await act(() => button('重新生成').click());
  await flush();
  assert.match(document.querySelector('.media-generation-dialog')!.textContent!, /重新生成/);
  assert.equal(quotes().length, 0, 'the optional-session fixture leaves billing to the billing contract suite');
  assert.equal(writes().length, 0);
});

test('single scene and prop image dialogs use their business names', async () => {
  for (const [route, title] of [['scenes', '生成场景'], ['props', '生成道具']]) {
    await mount('normal', '/playlet/p5_free/' + route);
    fixture.project.settings.imageModel = 'mock-image';
    await act(() => context.refreshCurrentProject('p5_free'));
    await flush();
    await act(() => document.querySelector<HTMLButtonElement>('.asset-catalog-card')!.click());
    await flush();
    assert.equal(document.querySelector<HTMLDetailsElement>('.asset-source-disclosure')!.open, false);
    await act(() => button('生成图片').click());
    await flush();
    assert.equal(document.querySelector('.media-generation-dialog h2')!.textContent, title);
    assert.equal(writes().length, 0);
  }
});

async function openAssetImageBatch(route = '/playlet/p5_free/characters') {
  await mount('normal', route);
  fixture.project.settings.imageModel = 'mock-image';
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  await act(() => button('批量生成图片').click());
  await flush();
}

test('batch entry keeps the highest-resolution estimate without quote requests through refresh and reopening', async () => {
  await mount('normal', '/playlet/p5_free/characters');
  fixture.project.settings.imageModel = 'mock-image';
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  const entry = button('批量生成图片');
  assert.equal(entry.querySelector('.asset-batch-price')?.textContent, '24');
  const quotes = () => fixture.requests.filter(request => request.path.endsWith('/quote'));
  assert.equal(quotes().length, 0);
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  assert.equal(entry.querySelector('.asset-batch-price')?.textContent, '24');
  assert.equal(quotes().length, 0);
  await act(() => entry.click());
  await flush();
  assert.equal(quotes().length, 2);
  await act(() => button('取消').click());
  await flush();
  assert.equal(entry.querySelector('.asset-batch-price')?.textContent, '24');
  assert.equal(quotes().length, 2);
  await navigate('/playlet/p5_free/props');
  await navigate('/playlet/p5_free/characters');
  assert.equal(button('批量生成图片').querySelector('.asset-batch-price')?.textContent, '24');
  assert.equal(quotes().length, 2);
  await act(() => button('批量生成图片').click());
  await flush();
  assert.equal(quotes().length, 4);
  assert.equal(writes().filter(request => request.path.endsWith('/batch-images')).length, 0);
});

test('slow batch quotes retain the estimate and only fresh server prices can be submitted', async () => {
  await mount('normal', '/playlet/p5_free/characters');
  fixture.project.settings.imageModel = 'mock-image';
  await act(() => context.refreshCurrentProject('p5_free'));
  let release!: () => void;
  const waitForPrice = new Promise<void>(resolve => { release = resolve; });
  globalThis.fetch = async (url, options = {}) => {
    const response = await fixture.fetch(url, options);
    if (!String(url).endsWith('/quote')) return response;
    const value = await response.json();
    await waitForPrice;
    return Response.json({ ...value, amount: 15, originalAmount: 15,
      basis: 'current-price-' + value.basis, rule: { ...value.rule, unitPrice: 15 } });
  };
  await act(() => button('批量生成图片').click());
  await flush();
  assert.equal(button('批量生成图片').querySelector('.asset-batch-price')?.textContent, '24');
  assert.equal(document.querySelector('.asset-batch-dialog .asset-batch-price')?.textContent, '24');
  assert.doesNotMatch(document.querySelector('.asset-batch-dialog')!.textContent!, /正在确认积分|…|\.\.\./);
  assert.equal(button('确认批量生成图片').disabled, true);
  await act(() => button('确认批量生成图片').click());
  assert.equal(fixture.mediaTasks.length, 0);
  await act(async () => { release(); await waitForPrice; });
  await flush();
  assert.equal(document.querySelector('.asset-batch-dialog .asset-batch-price')?.textContent, '30');
  assert.equal(button('确认批量生成图片').disabled, false);
  await act(() => button('确认批量生成图片').click());
  await flush();
  const commands = writes().find(request => request.path.endsWith('/batch-images'))!.body.commands;
  assert.equal(commands.length, 2);
  assert.ok(commands.every(command => command.billingBasis.startsWith('current-price-')));
});

test('batch quote failures preserve the entry estimate but never enable confirmation with that estimate', async () => {
  await mount('normal', '/playlet/p5_free/characters');
  fixture.project.settings.imageModel = 'mock-image';
  await act(() => context.refreshCurrentProject('p5_free'));
  globalThis.fetch = async (url, options = {}) => String(url).endsWith('/quote')
    ? Response.json({ code: 'MODEL_PRICE_UNAVAILABLE', message: '价格暂不可用' }, { status: 503 })
    : fixture.fetch(url, options);
  await act(() => button('批量生成图片').click());
  await flush();
  assert.equal(button('批量生成图片').querySelector('.asset-batch-price')?.textContent, '24');
  assert.equal(document.querySelector('.asset-batch-dialog .asset-batch-price')?.textContent, '24');
  assert.match(document.querySelector('.asset-batch-dialog .asset-batch-price')!.getAttribute('aria-label')!, /预计消耗 24 积分/);
  assert.equal(button('确认批量生成图片').disabled, true);
  await act(() => button('确认批量生成图片').click());
  assert.equal(fixture.mediaTasks.length, 0);
});

test('batch estimates honor server free metadata and omit unavailable prices instead of inventing zero', async () => {
  await mount('normal', '/playlet/p5_free/characters');
  fixture.project.settings.imageModel = 'mock-image';
  await act(() => context.refreshCurrentProject('p5_free'));
  let free = true, missing = false;
  let release!: () => void;
  const waitForPrice = new Promise<void>(resolve => { release = resolve; });
  globalThis.fetch = async (url, options = {}) => {
    const response = await fixture.fetch(url, options);
    if (String(url).endsWith('/quote')) await waitForPrice;
    if (!String(url).endsWith('/models')) return response;
    const value = await response.json();
    value.image[0].free = free;
    if (missing) value.image[0].creditPrice = null;
    return Response.json(value);
  };
  await act(() => context.refreshModels());
  await flush();
  // 限免批量入口只显示限免标签，不显示 0 积分。
  const batchFreePrice = button('批量生成图片').querySelector('.asset-batch-price')!;
  assert.equal(batchFreePrice.querySelector('.xy-model-free-badge')?.textContent, '限免');
  assert.doesNotMatch(batchFreePrice.textContent!, /0/);
  free = false; missing = true;
  await act(() => context.refreshModels());
  await flush();
  assert.equal(button('批量生成图片').querySelector('.asset-batch-price'), null);
  assert.equal(button('批量生成图片').getAttribute('aria-describedby'), null);
  assert.equal(button('批量生成图片').textContent, '批量生成图片');
  assert.equal(fixture.requests.filter(request => request.path.endsWith('/quote')).length, 0);
  await act(() => button('批量生成图片').click());
  await flush();
  assert.equal(document.querySelector('.asset-batch-dialog .asset-batch-price')?.textContent, '…');
  assert.equal(button('确认批量生成图片').disabled, true);
  await act(() => button('确认批量生成图片').click());
  assert.equal(fixture.mediaTasks.length, 0);
  await act(async () => { release(); await waitForPrice; });
  await flush();
  assert.equal(document.querySelector('.asset-batch-dialog .asset-batch-price')?.textContent, '24');
  assert.equal(button('确认批量生成图片').disabled, false);
});

test('batch asset images quote only missing category targets and persist separate loading states through refresh', async () => {
  await openAssetImageBatch();
  assert.equal(document.querySelector('.asset-batch-price')?.textContent, '24');
  assert.equal(document.querySelector<HTMLButtonElement>('[aria-label="图片比例"]')?.disabled, true);
  const fixedRatio = document.querySelector<HTMLButtonElement>('[aria-label="图片比例"]')!;
  await act(() => fixedRatio.dispatchEvent(new MouseEvent('pointerdown', { button: 0, ctrlKey: false, bubbles: true })));
  await flush();
  assert.equal(!!document.querySelector('.image-aspect-ratio-menu'), false);
  await act(() => button('确认批量生成图片').click());
  await flush();
  const commands = writes().find(request => request.path.endsWith('/batch-images'))!.body.commands;
  assert.deepEqual(commands.map(command => command.target), [{ assetId: 'character_1' }, { assetId: 'character_2' }]);
  assert.ok(commands.every(command => command.imageType === 'closeup_main' && command.aspectRatio === '16:9' && command.options.resolution === '4K'));
  assert.equal(document.querySelector('[aria-label="分辨率"]'), null);
  assert.equal(document.querySelectorAll('.asset-catalog-item .asset-image-task[aria-busy=true]').length, 2);
  assert.ok(fixture.mediaTasks.every(task => task.fillMissingImage));
  assert.equal(document.querySelector('.asset-batch-dialog'), null);
  assert.equal(button('查看批量结果'), undefined);
  await navigate('/playlet/p5_free/props');
  assert.equal(document.querySelector('.asset-catalog-item .asset-image-task'), null);
  await navigate('/playlet/p5_free/characters');
  assert.equal(document.querySelectorAll('.asset-image-task[aria-busy=true]').length, 2);
  fixture.completeMedia();
  fixture.completeMedia();
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(document.querySelector('.asset-catalog-item .asset-image-task'), null);
  assert.equal(document.querySelectorAll('.asset-catalog-card img').length, 2);
  assert.ok(fixture.project.assets.filter(asset => asset.kind === 'character').every(asset => asset.image));
  assert.equal(writes().filter(request => request.path.endsWith('/batch-images')).length, 1);
});

test('lost batch reply retains original commands and remount reconciliation never duplicates accepted tasks', async () => {
  await openAssetImageBatch();
  let lost = false;
  globalThis.fetch = async (url, options = {}) => {
    const response = await fixture.fetch(url, options);
    if (!lost && String(url).endsWith('/batch-images')) { lost = true; throw new Error('accepted response lost'); }
    return response;
  };
  await act(() => button('确认批量生成图片').click());
  await flush();
  const commands = structuredClone(writes().find(request => request.path.endsWith('/batch-images'))!.body.commands);
  assert.equal(fixture.mediaTasks.length, 2);
  assert.ok([...Array(sessionStorage.length)].some((_, i) => sessionStorage.key(i)?.startsWith('image-batch:')));
  await act(() => root!.unmount());
  root = createRoot(document.getElementById('root')!);
  await act(() => root!.render(<LanguageProvider><HashRouter><WorkspaceProvider><Probe /><MainWorkspace /></WorkspaceProvider></HashRouter></LanguageProvider>));
  await flush();
  await act(() => button('核对批量请求').click());
  await flush();
  await act(() => button('核对原请求').click());
  await flush();
  assert.equal(fixture.mediaTasks.length, 2);
  assert.deepEqual(writes().filter(request => request.path.endsWith('/batch-images'))[1].body.commands, commands);
  assert.equal(button('核对原请求'), undefined);
});

test('partial batch acceptance marks only actual tasks and price failures cannot display a zero price', async () => {
  await openAssetImageBatch();
  globalThis.fetch = async (url, options = {}) => {
    if (String(url).endsWith('/batch-images')) {
      fixture.project.assets.find(asset => asset.id === 'character_2').image = {
        fileId: 'concurrent-upload', thumbnailUrl: '/api/files/concurrent-upload/thumbnail', originalUrl: '/api/files/concurrent-upload/original',
      };
    }
    return fixture.fetch(url, options);
  };
  await act(() => button('确认批量生成图片').click());
  await flush();
  assert.equal(document.querySelector('.asset-batch-dialog'), null);
  assert.ok(document.querySelector('.asset-batch-errors[role=alert]'));
  assert.equal(button('查看批量结果'), undefined);
  assert.equal(fixture.mediaTasks.length, 1);
  assert.equal(document.querySelectorAll('.asset-catalog-item .asset-image-task[aria-busy=true]').length, 1);
  await openAssetImageBatch();
  await act(() => button('取消').click());
  globalThis.fetch = async (url, options = {}) => String(url).endsWith('/quote')
    ? Response.json({ code: 'MODEL_PRICE_UNAVAILABLE', message: '价格暂不可用' }, { status: 503 })
    : fixture.fetch(url, options);
  await act(() => button('批量生成图片').click());
  await flush();
  assert.equal(button('确认批量生成图片').disabled, true);
  assert.equal(document.querySelector('.asset-batch-dialog .asset-batch-price')?.textContent, '24');
  assert.equal(button('批量生成图片').querySelector('.asset-batch-price')?.textContent, '24');
  assert.match(document.querySelector('.asset-batch-dialog')!.textContent!, /价格暂不可用/);
});

test('Look batch excludes the base and failed cancelled or unknown image tasks do not spin forever', async () => {
  await openAssetImageBatch('/playlet/p5_free/characters/character_1/canvas');
  await act(() => button('确认批量生成图片').click());
  await flush();
  assert.deepEqual(writes().find(request => request.path.endsWith('/batch-images'))!.body.commands.map(command => command.target), [
    { assetId: 'character_1', lookId: 'look_rain' },
  ]);
  assert.equal(document.querySelector('.asset-batch-dialog'), null);
  assert.equal(document.querySelectorAll('.asset-image-task[aria-busy=true]').length, 1);
  fixture.mediaTasks[0].status = 'unknown';
  fixture.mediaTasks[0].revision++;
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(document.querySelector('.asset-image-task')?.textContent, '待确认');
  assert.equal(document.querySelector('.asset-image-task .animate-spin'), null);
  fixture.mediaTasks[0].status = 'queued';
  fixture.completeMedia('failed');
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(document.querySelector('.asset-image-task')?.textContent, '生成失败');
  fixture.mediaTasks[0].status = 'cancelled';
  fixture.mediaTasks[0].revision++;
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(document.querySelector('.asset-image-task'), null);
  assert.equal(fixture.project.assets.find(asset => asset.id === 'character_1').image, undefined);
});

test('adopting an image preserves an open text draft and advances only its matching revision', async () => {
  await mount('unlocked', '/playlet/p5_free/characters');
  await act(() => document.querySelector<HTMLButtonElement>('.asset-catalog-card')!.click());
  await flush();
  await input('[aria-label="角色资料"]', '保留未保存的角色资料草稿');
  const original = fixture.project.revision;
  await act(() => button('生成图片').click());
  await flush();
  await act(() =>
    [...document.querySelectorAll<HTMLButtonElement>('.media-generator-image button')]
      .find((b) => b.textContent?.trim() === '生成图片')!
      .click(),
  );
  await flush();
  fixture.completeMedia();
  assert.equal(button('刷新生成记录'), undefined);
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  await act(() => document.querySelector<HTMLButtonElement>('.media-generation-dialog button[aria-label="关闭"]')!.click());
  await flush();
  await act(() => button('生成历史').click());
  await flush();
  await act(() => button('确认定稿').click());
  await flush();
  assert.equal(
    (document.querySelector('[aria-label="角色资料"]') as HTMLTextAreaElement).value,
    '保留未保存的角色资料草稿',
  );
  assert.equal(fixture.project.revision, original + 1);
  await act(() => button('保存').click());
  await flush();
  const write = writes().at(-1)!;
  assert.equal(write.body.revision, original + 1);
  assert.equal(write.body.asset.description, '保留未保存的角色资料草稿');
});

test('autosave continues after leaving production and removes the permanent draft warning', async () => {
  await mount();
  await appendPrompt('跨页面自动保存的正文');
  assert.equal(button('保存镜头'), undefined);
  assert.doesNotMatch(document.body.textContent!, /本集有未保存草稿/);
  await navigate('/playlet/p5_free/outline');
  await saved();
  assert.equal(writes().length, 1);
  assert.match(JSON.stringify(fixture.shots[0].videoPrompt.parts), /跨页面自动保存的正文/);
  await navigate('/playlet/p5_free/production?episodeId=ep_upper');
  assert.match(
    document.querySelector('[aria-label="视频提示词"]')!.textContent!,
    /跨页面自动保存的正文/,
  );
});

test('typing during an in-flight autosave retains both edits and advances the next revision', async () => {
  await mount();
  const fetcher = globalThis.fetch;
  let release: () => void = () => {},
    held = false;
  globalThis.fetch = async (url, options = {}) => {
    const response = await fetcher(url, options);
    if (String(url).endsWith('/storyboard/changes') && options.method === 'POST' && !held) {
      held = true;
      await new Promise<void>((resolve) => {
        release = resolve;
      });
    }
    return response;
  };
  await appendPrompt('第一段写入');
  await saved();
  assert.equal(writes().length, 1);
  assert.equal(
    document.querySelector('[aria-label="视频提示词"]')!.getAttribute('contenteditable'),
    'true',
  );
  await appendPrompt('请求中继续输入');
  await act(() => release());
  await flush();
  await saved();
  assert.equal(writes().length, 2);
  assert.equal(writes()[1].body.revision, writes()[0].body.revision + 1);
  assert.match(JSON.stringify(fixture.shots[0].videoPrompt.parts), /第一段写入请求中继续输入/);
  assert.doesNotMatch(document.body.textContent!, /已自动保存|等待自动保存/);
  assert.equal(button('生成视频').disabled, false);
});

test('two shot drafts share a sequential autosave queue without losing the second draft', async () => {
  await mount();
  await appendPrompt('镜头一改动');
  await navigate('/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_2');
  await appendPrompt('镜头二改动');
  await saved();
  await saved();
  assert.equal(writes().length, 2);
  assert.equal(writes()[1].body.revision, writes()[0].body.revision + 1);
  assert.match(JSON.stringify(fixture.shots[0].videoPrompt.parts), /镜头一改动/);
  assert.match(JSON.stringify(fixture.shots[1].videoPrompt.parts), /镜头二改动/);
});

test('video versions count only completed files, keep selection local until confirmation and show failures in the preview', async () => {
  await mount();
  const finish = async (state = 'succeeded') => {
    await act(() => (button('重新生成') || button('生成视频')).click());
    await flush();
    fixture.completeMedia(state);
    await act(() => window.dispatchEvent(new Event('focus')));
    await flush();
  };
  await finish();
  await finish();
  await finish('failed');
  assert.equal(writes().length, 3);
  assert.equal(document.querySelector('.media-history'), null);
  assert.equal(document.querySelector('.sb-editor-tools'), null);
  assert.equal(button('重生成提示词'), undefined);
  assert.equal(button('选择视频').textContent, '2/2');
  assert.match(document.querySelector('.media-video-preview [role=alert]')!.textContent!, /受控免费生成失败/);
  const mainUrl = document.querySelector('.media-video-preview video')!.getAttribute('src');
  await act(() => button('选择视频').click());
  await flush();
  assert.equal(document.querySelectorAll('[role=radio]').length, 2);
  assert.equal(document.querySelector('.media-version-tasks'), null);
  assert.doesNotMatch(document.querySelector('[role=dialog]')!.textContent!, /任务详情|受控免费生成失败/);
  await act(() => button('视频 1').click());
  await flush();
  assert.equal(writes().length, 3);
  assert.equal(document.querySelector('.media-video-preview video')!.getAttribute('src'), mainUrl);
  await act(() => button('取消').click());
  await flush();
  assert.equal(writes().length, 3);
  assert.ok(fixture.mediaTasks.every((t) => !t.adoptedOutputId));
  await act(() => button('选择视频').click());
  await flush();
  await act(() => button('视频 1').click());
  await flush();
  await act(() => button('确认').click());
  await flush();
  assert.equal(writes().length, 4);
  assert.match(writes()[3].path, /adopt$/);
  assert.equal(fixture.mediaTasks.find((t) => t.adoptedOutputId)!.id, 'task_media_1');
  assert.equal(document.querySelector('.media-versions-dialog'), null);
});

test('stale video versions cannot be adopted and an adoption error retains the selected version without a retry', async () => {
  await mount();
  await act(() => button('生成视频').click());
  await flush();
  fixture.completeMedia();
  fixture.mediaTasks[0].stale = true;
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  await act(() => button('选择视频').click());
  await flush();
  assert.equal(button('确认').disabled, true);
  assert.doesNotMatch(
    document.querySelector('.media-versions-dialog')!.textContent!,
    /版本|输入已变化|不能采用/,
  );
  assert.match(button('确认').title, /提示词已修改/);
  await act(() => button('取消').click());
  await flush();
  fixture.mediaTasks[0].stale = false;
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  let attempts = 0;
  globalThis.fetch = async (url, options = {}) => {
    if (String(url).endsWith('/adopt') && options.method === 'POST') {
      attempts++;
      return Response.json({ code: 'CONFLICT', message: '受控采用冲突' }, { status: 409 });
    }
    return fixture.fetch(url, options);
  };
  await act(() => button('选择视频').click());
  await flush();
  await act(() => button('确认').click());
  await flush();
  assert.equal(attempts, 1);
  assert.ok(document.querySelector('.media-versions-dialog'));
  assert.equal(button('视频 1').getAttribute('aria-checked'), 'true');
  assert.match(document.querySelector('.media-versions-dialog')!.textContent!, /项目已更新/);
  assert.equal(fixture.mediaTasks[0].adoptedOutputId, '');
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.match(document.querySelector('.media-versions-dialog')!.textContent!, /项目已更新/);
  assert.equal(attempts, 1);
});

test('cold target discovery reaches older video versions and polling retains completed pagination', async () => {
  await mount();
  await act(() => button('生成视频').click());
  await flush();
  fixture.completeMedia();
  globalThis.fetch = async (url, options = {}) => {
    const parsed = new URL(String(url), 'http://127.0.0.1');
    if (parsed.pathname.endsWith('/media-tasks') && (!options.method || options.method === 'GET'))
      return Response.json({
        items: parsed.searchParams.get('cursor') ? fixture.mediaTasks : [],
        nextCursor: parsed.searchParams.get('cursor') ? null : 'older',
      });
    return fixture.fetch(url, options);
  };
  await navigate('/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_2');
  await navigate('/playlet/p5_free/production?episodeId=ep_upper&shotId=shot_1');
  assert.ok(button('选择视频'));
  await act(() => button('选择视频').click());
  await flush();
  assert.equal(document.querySelectorAll('[role=radio]').length, 1, 'cold discovery locates the target beyond the first page');
  assert.equal(button('更多视频'), undefined);
  await act(() => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(document.querySelectorAll('[role=radio]').length, 1);
  assert.equal(button('更多视频'), undefined);
  assert.equal(writes().length, 1);
});

function creationItem(id: string, kind: 'image' | 'video' = 'image', createdAt = '2026-10-19T01:00:00Z') {
  const taskId = 'library_task_' + id;
  const outputId = 'output_' + id;
  const base = '/api/media-library/' + taskId + '/outputs/' + outputId;
  return {
    spaceId: 'user_free', ownerId: 'user_free', creatorId: 'user_free', creatorName: '测试用户',
    sharedWithMe: false, effectiveAccess: 2,
    taskId, outputId, projectId: 'p5_free', projectTitle: '测试剧本', kind, createdAt,
    modelId: kind === 'image' ? 'mock-image' : 'mock_video', requestModelId: 'frozen-provider-model',
    aspectRatio: '16:9', resolution: '4K', width: 1024, height: 576,
    durationSeconds: kind === 'video' ? 8 : 0, contentType: kind === 'image' ? 'image/png' : 'video/mp4',
    sizeBytes: 128, promptPreview: '冻结提示词 ' + id,
    contentUrl: base + '/file', thumbnailUrl: base + '/thumbnail',
  };
}
test('creation library merges cursor pages by date, preserves pages on failure and reads frozen detail separately', async () => {
  const first = [creationItem('one'), creationItem('two')], later = [creationItem('three'), creationItem('old', 'image', '2026-10-17T01:00:00Z')];
  const calls: string[] = []; let failMore = true;
  await mount('normal', '/creations', base => async (url, options = {}) => {
    const u = new URL(String(url), 'http://fixture.invalid');
    if (u.pathname === '/api/media-library') {
      calls.push(u.search);
      if (u.searchParams.has('cursor') && failMore) { failMore = false; return Response.json({ code: 'PROGRAM_FAILED', message: '列表暂不可用' }, { status: 503 }); }
      return Response.json({ items: u.searchParams.has('cursor') ? later : first, nextCursor: u.searchParams.has('cursor') ? null : 'opaque-next' });
    }
    if (u.pathname.startsWith('/api/media-library/')) return Response.json({ ...first[0], prompt: '完整冻结提示词\n当次构图' });
    return base(url, options);
  });
  assert.ok(document.querySelector('a[href="#/creations"]'));
  assert.ok(calls[0].includes('kind=image'));
  assert.equal(document.querySelector('.creations-toolbar [role=tab][aria-selected=true]')?.textContent, '图片');
  assert.equal(document.querySelectorAll('.creations-toolbar [role=tab]').length, 2);
  assert.equal(button('刷新创作资产'), undefined);
  assert.equal(document.querySelectorAll('.creation-card').length, 2);
  assert.equal(document.querySelectorAll('.creation-day').length, 1);
  await act(() => button('加载更多').click()); await flush();
  assert.equal(document.querySelectorAll('.creation-card').length, 2);
  assert.match(document.querySelector('.creations-page')!.textContent!, /列表暂不可用/);
  await act(() => button('重新读取').click()); await flush();
  assert.equal(document.querySelectorAll('.creation-card').length, 4);
  assert.equal(document.querySelectorAll('.creation-day').length, 2);
  assert.ok(calls[1].includes('cursor=opaque-next') && calls[2] === calls[1]);
  assert.equal(document.querySelector('.creation-card video'), null);
  await act(() => document.querySelector<HTMLButtonElement>('.creation-card-open')!.click()); await flush();
  assert.match(document.querySelector('.creation-detail')!.textContent!, /完整冻结提示词\s+当次构图/);
  assert.match(document.querySelector('.creation-detail')!.textContent!, /frozen-provider-model/);
  assert.doesNotMatch(document.querySelector('.creation-detail')!.textContent!, /创作工具|继续创作|生成历史/);
  assert.ok(document.querySelector('.creation-detail img[src$="/file?audience=workspace"]'));
});
test('creation library discards late pages when switching kind and searches from the first cursor', async () => {
  let release!: () => void;
  const slow = new Promise<void>(resolve => { release = resolve; });
  const calls: URL[] = [];
  await mount('normal', '/creations', base => async (url, options = {}) => {
    const u = new URL(String(url), 'http://fixture.invalid');
    if (u.pathname !== '/api/media-library') return base(url, options);
    calls.push(u);
    if (u.searchParams.get('kind') === 'image') { await slow; return Response.json({ items: [creationItem('late')], nextCursor: 'old-cursor' }); }
    return Response.json({ items: [creationItem(u.searchParams.get('q') ? 'searched' : 'video', 'video')], nextCursor: null });
  });
  await act(() => button('视频').click()); await flush();
  await act(async () => { release(); await slow; }); await flush();
  assert.equal(document.querySelectorAll('.creation-card').length, 1);
  assert.match(document.querySelector('.creation-card')!.textContent!, /video/);
  assert.doesNotMatch(document.querySelector('.creations-page')!.textContent!, /冻结提示词 late/);
  await input('[aria-label="搜索提示词或项目名称"]', ' 雨夜 ');
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 330)); }); await flush();
  assert.equal(calls.at(-1)!.searchParams.get('q'), '雨夜');
  assert.equal(calls.at(-1)!.searchParams.has('cursor'), false);
  assert.match(document.querySelector('.creation-card')!.textContent!, /searched/);
});

test('creation responses reject external file URLs and discard unrelated response fields', async () => {
  const { mediaLibraryApi } = await import('../src/api/media-library');
  const item = creationItem('boundary');
  globalThis.fetch = async () => Response.json({ items: [{ ...item, credential: 'not-for-the-client' }], nextCursor: null });
  const list = await mediaLibraryApi.list();
  assert.equal('credential' in list.items[0], false);
  globalThis.fetch = async () => Response.json({ ...item, prompt: '原始冻结正文', contentUrl: 'https://external.invalid/file' });
  await assert.rejects(mediaLibraryApi.detail(item.taskId, item.outputId), (e: any) => e.code === 'CONTENT_BOUNDARY_VIOLATION');
});

test('video creation detail renders the original protected file, poster and frozen media parameters', async () => {
  const item = creationItem('video-detail', 'video');
  await mount('normal', '/creations?task=' + item.taskId + '&output=' + item.outputId, base => async (url, options = {}) => {
    const path = new URL(String(url), 'http://fixture.invalid').pathname;
    if (path === '/api/media-library') return Response.json({ items: [item], nextCursor: null });
    if (path.startsWith('/api/media-library/')) return Response.json({ ...item, prompt: '冻结的镜头动态和对白' });
    return base(url, options);
  });
  const video = document.querySelector<HTMLVideoElement>('.creation-detail video')!;
  assert.ok(video);
  assert.equal(video.getAttribute('src'), item.contentUrl + '?audience=workspace');
  assert.equal(video.getAttribute('poster'), item.thumbnailUrl + '?audience=workspace');
  assert.equal(video.controls, true);
  assert.equal(video.autoplay, false);
  assert.match(document.querySelector('.creation-detail')!.textContent!, /冻结的镜头动态和对白/);
  assert.match(document.querySelector('.creation-detail')!.textContent!, /8 秒/);
});

test('batch image entry disappears for complete category and Look images without issuing quotes', async () => {
  await mount('normal', '/playlet/p5_free/characters');
  for (const asset of fixture.project.assets.filter(a => a.kind === 'character')) {
    asset.image = {fileId:'file_'+asset.id,originalUrl:'/api/files/'+asset.id+'/original',thumbnailUrl:'/api/files/'+asset.id+'/thumbnail'};
    for (const look of asset.looks) look.image = {...asset.image, fileId:'file_'+look.id};
  }
  await act(() => context.refreshCurrentProject('p5_free'));
  await flush();
  assert.equal(button('批量生成图片'), undefined);
  const previousFetch = globalThis.fetch;
  let failedReads = 0;
  globalThis.fetch = async (url, options) => {
    if (String(url).includes('/media-tasks') && (!options?.method || options.method === 'GET')) {
      failedReads++;
      return Response.json({code:'PROGRAM_FAILED',message:'受控的任务读取失败'}, {status:503});
    }
    return previousFetch(url, options);
  };
  try {
    await act(() => window.dispatchEvent(new Event('focus')));
    await flush();
    assert.ok(failedReads > 0);
    assert.equal(button('批量生成图片'), undefined);
  } finally { globalThis.fetch = previousFetch; }
  await navigate('/playlet/p5_free/characters/character_1/canvas');
  assert.equal(button('批量生成图片'), undefined);
  assert.equal(writes().length,0);
  assert.equal(fixture.requests.some(r => r.path.endsWith('/quote')),false);
});

test('closing a multi-batch image quote aborts four pending reads and sends no later quotes', async () => {
  await mount('normal','/playlet/p5_free/characters');
  fixture.project.settings.imageModel='mock-image';
  const original=fixture.project.assets.find(a=>a.kind==='character')!;
  for(let i=0;i<8;i++) {
    const asset={...structuredClone(original),id:'batch-more-'+i,name:'额外角色'+i,looks:[]};
    fixture.project.assets.push(asset);
    fixture.project.visualPrompts.push({...structuredClone(fixture.project.visualPrompts.find(v=>v.target.assetId===original.id)!),target:{assetId:asset.id},origin:'user'});
  }
  await act(()=>context.refreshCurrentProject('p5_free'));await flush();
  const pending: {signal:AbortSignal;release:()=>void}[]=[];
  globalThis.fetch=async(url,options={})=>{
    const response=await fixture.fetch(url,options);
    if(!String(url).endsWith('/quote'))return response;
    await new Promise<void>(resolve=>pending.push({signal:options.signal!,release:resolve}));
    return response;
  };
  await act(()=>button('批量生成图片').click());await flush();
  assert.equal(pending.length,4);
  await act(()=>button('取消').click());await flush();
  assert.ok(pending.every(item=>item.signal.aborted));
  await act(async()=>{for(const item of pending)item.release();});await flush();
  assert.equal(pending.length,4,'closed dialog sent the next batch');
  assert.equal(writes().filter(r=>r.path.endsWith('/batch-images')).length,0);
});

test('unchanged source and episode range reuse material while new ranges fetch complete episode material', async () => {
  await mount();
  const sourceReads=()=>fixture.requests.filter(r=>r.path.endsWith('/source')).length;
  const before=sourceReads();
  const originalSource=structuredClone(context.source);
  fixture.project.revision++;
  await act(()=>context.refreshCurrentProject('p5_free'));await flush();await flush();
  assert.equal(sourceReads(),before,'revision-only refresh downloaded immutable material');
  const baseFetch=fixture.fetch;
  globalThis.fetch=async(url,options={})=>{
    const response=await baseFetch(url,options);
    if(String(url).endsWith('/storyboard/episodes/ep_upper')) {
      const value=await response.json();value.episode.sourceRanges[0].start.offset++;return Response.json(value);
    }
    if(String(url).endsWith('/storyboard/episodes/ep_upper/source')) {
      const value=await response.json();value.ranges[0].start.offset++;
      value.fragments[0].startOffset++;value.fragments[0].text=Array.from(value.fragments[0].text).slice(1).join('');
      return Response.json(value);
    }
    return response;
  };
  fixture.project.revision++;
  await act(()=>context.refreshCurrentProject('p5_free'));await flush();await flush();
  assert.equal(sourceReads(),before+1,'range change reused a stale episode fragment');
  assert.deepEqual(context.source,originalSource,'episode view changed complete source');
});
