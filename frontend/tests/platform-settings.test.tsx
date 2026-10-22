import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import { createAdministrationFixture } from './administration-fixture';
import type { PlatformSettings, PlatformApi } from '../src/api/platform-client';
const dom = new JSDOM('<div id="root"></div>', { url: 'http://127.0.0.1/demo/app/#/operations/platform', pretendToBeVisual: true });
for (const key of ['window', 'document', 'navigator', 'HTMLElement', 'HTMLInputElement', 'HTMLButtonElement',
  'Element', 'Node', 'NodeFilter', 'DocumentFragment', 'SVGElement', 'MutationObserver', 'Event',
  'MouseEvent', 'CustomEvent', 'localStorage', 'sessionStorage', 'KeyboardEvent'])
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
globalThis.getComputedStyle = dom.window.getComputedStyle;
globalThis.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
Object.defineProperty(globalThis, 'BroadcastChannel', { value: undefined, configurable: true });
const { createRoot } = await import('react-dom/client');
const { HashRouter } = await import('react-router-dom');
const { LanguageProvider } = await import('../src/contexts/LanguageContext');
const { SessionProvider } = await import('../src/state/SessionContext');
const { PlatformProvider } = await import('../src/state/PlatformContext');
const { Application } = await import('../src/Application');
const { sessionBoundary } = await import('../src/api/session-boundary');
const { ApiError } = await import('../src/api/http-error');
const { defaultPlatformLogo } = await import('../src/components/workspace/PlatformLogo');
let root: ReturnType<typeof createRoot> | undefined;
let stored: PlatformSettings;
let api: PlatformApi;
let saves = 0, uploads = 0;
const original: PlatformSettings = { revision: 0, name: '短剧 Agent', sidebarDescription: '创作工作台', logoId: '', logoUrl: '' };
const hash = 'a'.repeat(64), url = '/api/platform-logos/' + hash;
const flush = async () => { await act(async () => { await new Promise((resolve) => setTimeout(resolve, 5)); }); };
const button = (name: string) => [...document.querySelectorAll<HTMLButtonElement>('button')]
  .find((item) => item.textContent?.trim() === name || item.getAttribute('aria-label') === name)!;
const input = (name: string) => [...document.querySelectorAll('label')]
  .find((label) => label.textContent?.includes(name))!.querySelector('input')!;
async function fill(name: string, value: string) {
  await act(async () => {
    const field = input(name);
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, value);
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
}
async function mount(setup?: () => void, route = '/operations/platform', ordinary = false) {
  if (root) await act(async () => root!.unmount());
  sessionBoundary.reset();
  stored = { ...original }; saves = 0; uploads = 0;
  api = {
    publicSettings: async () => ({ ...stored }),
    settings: async () => ({ ...stored }),
    save: async (command) => { saves++; stored = { ...command, revision: command.revision + 1, logoUrl: command.logoId ? url : '' }; return stored; },
    upload: async () => { uploads++; return { logoId: hash, logoUrl: url }; },
  };
  setup?.();
  window.location.hash = '#' + route;
  globalThis.fetch = async (target) => {
    const path = String(target).split('?')[0];
    const personal = {id: accounts.user?.identity || 'fixture_operator', kind: 'personal', name: '个人空间', role: 'owner', memberCount: 1, memberLimit: 1, status: 'active', readOnly: false};
    if (path === '/api/spaces') return Response.json([personal]);
    if (path === '/api/invites/pending') return Response.json([]);
    if (path === '/api/spaces/' + personal.id) return Response.json(personal);
    return Response.json(path === '/api/projects' ? [] :
      path === '/api/models' ? { analysis: [], image: [], video: [] } :
        path === '/api/visual-styles' ? { styles: [] } : { readOnly: false, capabilities: {} });
  };
  root = createRoot(document.getElementById('root')!);
  const accounts = createAdministrationFixture();
  if (ordinary) accounts.user = { ...accounts.user!, operationsAllowed: false, roleLabel: '普通用户' };
  await act(async () => root!.render(<LanguageProvider><HashRouter>
    <SessionProvider service={accounts.service}>
      <PlatformProvider api={api}><Application /></PlatformProvider>
    </SessionProvider>
  </HashRouter></LanguageProvider>));
  await flush(); await flush();
}
after(async () => { if (root) await act(async () => root!.unmount()); dom.window.close(); });
test('brand edits and uploaded Logo remain drafts until save, then read back; restore is also explicit', async () => {
  await mount();
  assert.equal(input('平台名称').value, original.name);
  assert.equal(button('保存设置').disabled, true);
  await fill('平台名称', '星河影业');
  await fill('侧栏描述', '把灵感拍成故事');
  await act(async () => {
    const file = document.querySelector<HTMLInputElement>('input[type=file]')!;
    Object.defineProperty(file, 'files', { configurable: true, value: [new File(['png'], 'logo.png', { type: 'image/png' })] });
    file.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
  assert.equal(uploads, 1);
  assert.equal(document.querySelector('.op-platform-logo-preview img')?.getAttribute('src'), url);
  assert.equal(document.querySelector('.account-brand-name')?.textContent, original.name);
  assert.equal(stored.name, original.name);
  await act(async () => button('保存设置').click());
  await flush();
  assert.equal(saves, 1);
  assert.equal(stored.name, '星河影业');
  assert.equal(stored.sidebarDescription, '把灵感拍成故事');
  assert.equal(document.querySelector('.account-brand-name')?.textContent, '星河影业');
  assert.equal(document.title, '星河影业');
  await act(async () => button('恢复默认').click());
  // 恢复默认即回到模板自带标识：草稿预览立刻显示默认图，已发布的品牌在保存前仍是上传的图。
  assert.equal(document.querySelector('.op-platform-logo-preview img')?.getAttribute('src'), defaultPlatformLogo);
  assert.equal(document.querySelector('.account-brand img')?.getAttribute('src'), url);
  await act(async () => button('保存设置').click());
  await flush();
  assert.equal(stored.logoId, '');
  assert.equal(stored.name, '星河影业');
  assert.equal(document.querySelector('.account-brand img')?.getAttribute('src'), defaultPlatformLogo);
  await act(async () => { window.location.hash = '#/operations/users'; }); await flush();
  await act(async () => { window.location.hash = '#/operations/platform'; }); await flush();
  assert.equal(input('平台名称').value, '星河影业');
  assert.equal(button('保存设置').disabled, true);
});
test('conflict and unknown saves retain the draft, block resend, and require explicit readback', async () => {
  for (const code of ['PLATFORM_SETTINGS_CONFLICT', 'PLATFORM_SETTINGS_SAVE_UNKNOWN', 'PROGRAM_FAILED']) {
    await mount(() => {
      api.save = async () => { saves++; throw new ApiError(code === 'PLATFORM_SETTINGS_CONFLICT' ? 409 : 503, code, '未确认'); };
    });
    await fill('平台名称', '保留草稿');
    await act(async () => button('保存设置').click()); await flush();
    assert.equal(input('平台名称').value, '保留草稿');
    assert.equal(button('保存设置').disabled, true);
    assert.equal(saves, 1);
    stored = { ...original, revision: 3, name: '服务端品牌' };
    await act(async () => button('读取最新配置').click()); await flush();
    assert.equal(input('平台名称').value, '服务端品牌');
    assert.equal(document.querySelector('.account-brand-name')?.textContent, '服务端品牌');
  }
});
test('read or upload failure never substitutes defaults or destroys an existing draft', async () => {
  await mount(() => { api.settings = async () => { throw new ApiError(503, 'PLATFORM_SETTINGS_UNAVAILABLE', '无法读取'); }; });
  assert.equal(document.querySelector('.op-platform-form'), null);
  assert.ok(document.querySelector('[role=alert]'));
  api.settings = async () => ({ ...original, name: '实际配置' });
  await act(async () => button('重新读取').click()); await flush();
  assert.equal(input('平台名称').value, '实际配置');
  await fill('侧栏描述', '新的描述');
  api.upload = async () => { throw new ApiError(400, 'INVALID_PLATFORM_LOGO', '无效图片'); };
  await act(async () => {
    const field = document.querySelector<HTMLInputElement>('input[type=file]')!;
    Object.defineProperty(field, 'files', { configurable: true, value: [new File(['broken'], 'broken.png', { type: 'image/png' })] });
    field.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
  assert.equal(input('侧栏描述').value, '新的描述');
  assert.equal(saves, 0);
});
test('late public read cannot replace a newer saved brand, and public read errors offer retry', async () => {
  let release: (value: PlatformSettings) => void = () => {};
  await mount(() => { api.publicSettings = () => new Promise((resolve) => { release = resolve; }); });
  await fill('平台名称', '已保存品牌');
  await act(async () => button('保存设置').click()); await flush();
  await act(async () => release(original)); await flush();
  assert.equal(document.querySelector('.account-brand-name')?.textContent, '已保存品牌');
  api.publicSettings = async () => { throw new ApiError(503, 'PLATFORM_SETTINGS_UNAVAILABLE', '读取失败'); };
  await act(async () => window.dispatchEvent(new Event('focus'))); await flush();
  assert.match(document.body.textContent || '', /平台信息暂不可用/);
  assert.equal(document.querySelector('.account-brand-name')?.textContent, '已保存品牌');
  api.publicSettings = async () => stored;
  await act(async () => button('重新读取').click()); await flush();
  assert.doesNotMatch(document.body.textContent || '', /平台信息暂不可用/);
});
test('workspace separates configurable brand from fixed Agent navigation and heading', async () => {
  await mount(() => {
    stored = { ...original, name: '星河影业', sidebarDescription: '新的侧栏描述', logoId: hash, logoUrl: url };
  }, '/projects');
  assert.equal(document.querySelector('h1')?.textContent, '短剧 Agent');
  assert.equal(document.querySelector('#workspace-sidebar-navigation a')?.getAttribute('aria-label'), '短剧 Agent');
  assert.match(document.body.textContent || '', /让文字成戏，让想象开机。/);
  assert.equal(document.querySelector('.jm-sidebar-collapsed-mark img')?.getAttribute('src'), url);
  await act(async () => button('展开侧栏').click()); await flush();
  assert.equal(document.querySelector('.workspace-brand strong')?.textContent, '星河影业');
  assert.equal(document.querySelector('.workspace-brand small')?.textContent, '新的侧栏描述');
  stored = { ...stored, revision: 1, sidebarDescription: '' };
  await act(async () => window.dispatchEvent(new Event('focus'))); await flush();
  assert.equal(document.querySelector('.workspace-brand small'), null);
  assert.equal(document.querySelector('h1')?.textContent, '短剧 Agent');
  assert.equal(document.querySelector('#workspace-sidebar-navigation a')?.textContent, '短剧 Agent');
});
test('ordinary accounts read the public brand on authentication pages without reading operator settings', async () => {
  let reads = 0;
  await mount(() => {
    stored = { ...original, name: '星河影业', logoId: hash, logoUrl: url };
    api.settings = async () => { reads++; return stored; };
  }, '/operations/platform', true);
  assert.equal(reads, 0);
  assert.equal(window.location.hash, '#/operations-login');
  assert.equal(document.querySelector('.account-brand-name')?.textContent, '星河影业');
  assert.equal(document.querySelector('.account-brand img')?.getAttribute('src'), url);
  assert.match(document.body.textContent || '', /登录运营后台/);
});
test('leaving settings before a private read completes cannot replace the shared brand', async () => {
  let release: (value: PlatformSettings) => void = () => {};
  await mount(() => { api.settings = () => new Promise((resolve) => { release = resolve; }); });
  await act(async () => { window.location.hash = '#/operations/users'; }); await flush();
  await act(async () => release({ ...original, revision: 9, name: '迟到结果' })); await flush();
  assert.equal(document.querySelector('.account-brand-name')?.textContent, original.name);
});
