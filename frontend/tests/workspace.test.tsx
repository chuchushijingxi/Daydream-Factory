import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { JSDOM } from 'jsdom';
import React, { act } from 'react';
const dom = new JSDOM('<div id="test-root"></div>', {
  url: 'http://127.0.0.1/demo/app/#/projects',
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
  'HTMLSelectElement',
  'MutationObserver',
  'Event',
  'MouseEvent',
  'CustomEvent',
  'localStorage',
])
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
globalThis.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
globalThis.getComputedStyle = dom.window.getComputedStyle;
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
globalThis.innerWidth = 1024;
globalThis.innerHeight = 768;
globalThis.addEventListener = dom.window.addEventListener.bind(dom.window);
globalThis.removeEventListener = dom.window.removeEventListener.bind(dom.window);
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const { createRoot } = await import('react-dom/client');
const { HashRouter, useLocation } = await import('react-router-dom');
const { ModelConfigurationPage } = await import(
  '../src/pages/model-config/ModelConfigurationPage.tsx'
);
// Configuration behavior is tested directly; production mounting is role guarded by Application.
function WorkspaceFixture() {
  const { pathname } = useLocation();
  return pathname === '/model-config' ? <ModelConfigurationPage /> : <MainWorkspace />;
}
const { LanguageProvider } = await import('../src/contexts/LanguageContext.tsx');
const { WorkspaceProvider, useWorkspace } = await import('../src/state/WorkspaceContext.tsx');
let currentContext;
function ContextProbe() {
  currentContext = useWorkspace();
  return null;
}
const { MainWorkspace } = await import('../src/MainWorkspace.tsx');
let root,
  requests = [],
  coordinationRequests = [],
  editorLeases = new Set(),
  fixture,
  readOnly = true,
  pending = new Map(),
  uploadedName = '',
  uploadedText = '',
  configurationEnabled = false,
  modelConfig,
  uploadSettingsFixture = false,
  requestInterceptor = null,
  capabilityOverrides = {};
const uploadStyle = JSON.parse(await readFile('../demo/assets/styles/catalog.json', 'utf8'))
  .styles[0];
const defaultRegistry = () => ({
  revision: 1,
  editable: true,
  connections: [
    {
      id: 'deepseek',
      name: 'DeepSeek',
      kind: 'official',
      protocol: 'deepseek',
      baseUrl: 'https://api.deepseek.com',
      credentialConfigured: false,
      editable: true,
      reason: '',
      models: [
        {
          id: 'deepseek-flash',
          modelId: 'deepseek-flash',
          alias: '',
          name: 'DeepSeek Flash',
          category: 'analysis',
          enabled: false,
          available: false,
          referenced: false,
          reason: '',
          contextWindow: 1000000,
          maxOutputTokens: 393216,
          creditPrice: null,
          resolutionEnabled: null,
          pricing: {
            inputCnyPerMillion: 2,
            outputCnyPerMillion: 8,
            cacheHitHundredthsCnyPerMillion: 4,
          },
        },
        {
          id: 'deepseek-v4-pro',
          modelId: 'deepseek-v4-pro',
          alias: '',
          name: 'DeepSeek Pro',
          category: 'analysis',
          enabled: false,
          available: false,
          referenced: false,
          reason: '',
          contextWindow: 1000000,
          maxOutputTokens: 393216,
          creditPrice: null,
          resolutionEnabled: null,
          pricing: {
            inputCnyPerMillion: 9,
            outputCnyPerMillion: 27,
            cacheHitHundredthsCnyPerMillion: 30,
          },
        },
      ],
    },
  ],
});
const modelView = () => ({
  ...modelConfig,
  editable: !readOnly,
  connections: modelConfig.connections.map((conn) => ({
    ...conn,
    editable: !readOnly,
    reason: readOnly
      ? '当前服务为只读模式，不能配置或调用模型'
      : !conn.credentialConfigured
        ? '请先配置 API Key'
        : '',
    models: conn.models.map((m) => ({
      ...m,
      name: m.alias || m.name || m.modelId,
      logoUrl: m.logoId
        ? '/api/model-logos/' + m.logoId
        : m.modelId.startsWith('deepseek-')
          ? '/demo/assets/providers/deepseek.svg'
          : '',
      provider: m.modelId.startsWith('deepseek-') ? 'deepseek' : 'custom',
      available:
        m.enabled &&
        !readOnly &&
        (conn.kind === 'builtin' ||
          (conn.credentialConfigured &&
            m.category === 'analysis' &&
            m.contextWindow > 0 &&
            m.maxOutputTokens > 0 &&
            !!m.pricing)),
      reason: !conn.credentialConfigured
        ? '请先配置 API Key'
        : !m.enabled
          ? '模型已关闭，不会出现在项目候选中'
          : '',
    })),
  })),
});
const registryOptions = () =>
  modelView().connections.flatMap((conn) =>
    conn.models.map((m) => ({
      id: m.id,
      name: m.name,
      connectionName: conn.name,
      provider: m.provider,
      modelId: m.modelId,
      logoUrl: m.logoUrl,
      available: m.available,
    })),
  );
const copy = (v) => JSON.parse(JSON.stringify(v));
function project(id = 'project_fictional') {
  const assets = [
    {
      id: 'character_one',
      kind: 'character',
      name: '虚构小竹',
      description: '完整角色资料',
      aliases: [],
      sourceIds: ['span_one'],
      origin: 'model',
      character: {
        roleType: '',
        age: '',
        gender: '',
        biography: '',
        appearance: '',
        personality: '',
      },
      looks: [],
    },
    {
      id: 'location_one',
      kind: 'location',
      name: '虚构车站',
      description: '清晨的车站',
      aliases: [],
      sourceIds: ['span_one'],
      origin: 'model',
      character: null,
      looks: [],
    },
    {
      id: 'prop_one',
      kind: 'prop',
      name: '虚构怀表',
      description: '银色怀表',
      aliases: [],
      sourceIds: [],
      origin: 'model',
      character: null,
      looks: [],
    },
  ];
  const targets = assets.map((a) => ({ assetId: a.id }));
  return {
    id,
    spaceId: 'workspace_fixture', ownerId: 'workspace_fixture', creatorId: 'workspace_fixture',
    title: '虚构测试剧本',
    uploadedAt: '2026-10-12T03:04:00Z',
    revision: 4,
    source: { version: 1, filename: 'fiction.txt', sha256: 'fiction', spanCount: 1 },
    assets,
    runs: [
      {
        id: 'p3',
        contract: 'catalog-agent-v3',
        sourceVersion: 1,
        status: 'completed',
        stage: 'correct',
        callCount: 0,
      },
      {
        id: 'visual',
        contract: 'visual-prompt-agent-v1',
        sourceVersion: 1,
        status: 'completed',
        stage: '',
        callCount: 0,
        targets,
      },
    ],
    outline: null,
    settings: { aspectRatio: '9:16', llmModel: 'deepseek-flash', imageModel: '', videoModel: '' },
    style: null,
    visualPrompts: targets.map((target) => ({
      target,
      text: '虚构纯正文',
      stale: false,
      sourceVersion: 1,
      runId: 'visual',
    })),
    catalogEditedByUser: false,
    usedOrReservedMicroCny: 0,
    limitMicroCny: 0,
  };
}
globalThis.fetch = async (url, options = {}) => {
  // Existing assertions count content/model operations. Keep coordination in a
  // separate complete log so acquiring a lease is never mistaken for generation.
  url = new URL(String(url), 'http://fixture.invalid').pathname;
  const coordination = url.match(/^\/api\/projects\/([^/]+)\/(permissions|editing)$/);
  if (coordination) {
    const method = options.method || 'GET', id = decodeURIComponent(coordination[1]);
    coordinationRequests.push({url, method});
    if (coordination[2] === 'permissions') return Response.json({spaceId: fixture.spaceId, ownerId: fixture.ownerId, creatorId: fixture.creatorId, access: readOnly ? 1 : 2, canShare: false});
    if (method === 'DELETE') { editorLeases.delete(id); return Response.json({released: true}); }
    if (readOnly) return Response.json({code: 'SERVICE_READ_ONLY', message: '只读测试夹具'}, {status: 403});
    if (method === 'POST') editorLeases.add(id);
    const owned = editorLeases.has(id);
    return Response.json({userId: owned ? fixture.ownerId : '', username: owned ? '虚构编辑者' : '', expiresAt: owned ? new Date(Date.now()+90000).toISOString() : null, owned});
  }
  requests.push({
    url,
    method: options.method || 'GET',
    body:
      options.body && typeof options.body === 'string' ? JSON.parse(options.body) : options.body,
  });
  if (requestInterceptor) {
    const reply = await requestInterceptor(url, options);
    if (reply) return reply;
  }
  if (pending.has(url)) return pending.get(url);
  if (url === '/api/status')
    return Response.json({
      readOnly,
      configured: configurationEnabled && registryOptions().some((m) => m.available),
      capabilities: {
        assetEditing: true,
        assetImages: true,
        characterLooks: true,
        visualPrompts: true,
        singleVisualPrompts: true,
        projectDeletion: true,
        ...capabilityOverrides,
        modelConfiguration: configurationEnabled,
        ...(uploadSettingsFixture ? { projectSettings: true, projectStyle: true } : {}),
      },
    });
  if (url === '/api/model-config/models/discover')
    return Response.json({
      models: [{ modelId: 'discovered-text' }, { modelId: 'discovered-image' }],
    });
  if (url === '/api/model-logos')
    return Response.json({ logoId: 'a'.repeat(64), logoUrl: '/api/model-logos/' + 'a'.repeat(64) });
  if (url === '/api/model-config') {
    if (options.method !== 'POST') return Response.json(modelView());
    if (readOnly)
      return Response.json({ code: 'SERVICE_READ_ONLY', message: '只读服务' }, { status: 403 });
    const body = JSON.parse(options.body);
    if (body.revision !== modelConfig.revision)
      return Response.json({ code: 'CONFLICT', message: '配置版本冲突' }, { status: 409 });
    const input = body.connection,
      old = modelConfig.connections.find((c) => c.id === input.id);
    const conn = {
      ...input,
      id: input.id || 'fixture-connection-' + modelConfig.revision,
      credentialConfigured: Boolean(body.apiKey) || !!old?.credentialConfigured,
      editable: true,
      reason: '',
      models: input.models.map((m, i) => ({
        ...m,
        mediaProfile: old?.models.find((saved) => saved.id === m.id)?.mediaProfile,
        id: m.id || 'fixture-model-' + modelConfig.revision + '-' + i,
        name:
          m.alias ||
          (m.modelId === 'deepseek-flash'
            ? 'DeepSeek Flash'
            : m.modelId === 'deepseek-v4-pro'
              ? 'DeepSeek Pro'
              : m.modelId),
        available: false,
        referenced: false,
        reason: '',
      })),
    };
    modelConfig = {
      ...modelConfig,
      revision: modelConfig.revision + 1,
      connections: old
        ? modelConfig.connections.map((c) => (c.id === old.id ? conn : c))
        : [...modelConfig.connections, conn],
    };
    return Response.json(modelView());
  }
  if (url === '/api/upload') {
    assert.equal(readOnly, false);
    assert.ok(options.body instanceof FormData);
    const file = options.body.get('file');
    uploadedText = await file.text();
    uploadedName = file.name;
    const fileId = 'file_' + 'a'.repeat(32);
    return Response.json(
      {
        fileId,
        filename: file.name,
        contentType: file.type || 'text/plain',
        sizeBytes: file.size,
        originalUrl: '/api/files/' + fileId + '/original',
        thumbnailUrl:
          options.body.get('thumbnail') === 'true' ? '/api/files/' + fileId + '/thumbnail' : null,
      },
      { status: 201 },
    );
  }
  if (url === '/api/projects' && options.method === 'POST') {
    assert.equal(readOnly, false);
    const input = JSON.parse(options.body);
    assert.equal(input.fileId, 'file_' + 'a'.repeat(32));
    fixture = project('project_uploaded');
    fixture.assets = [];
    fixture.runs = [];
    fixture.visualPrompts = [];
    fixture.settings = input.settings || null;
    fixture.source.filename = uploadedName;
    fixture.title = input.title;
    if (input.styleId) fixture.style = copy(uploadStyle);
    return Response.json(copy(fixture), { status: 201 });
  }
  if (url === '/api/projects')
    return Response.json([
      {
        id: fixture.id,
        spaceId: fixture.spaceId, ownerId: fixture.ownerId, creatorId: fixture.creatorId, creatorName: '虚构编辑者', sharedWithMe: false, effectiveAccess: readOnly ? 1 : 2,
        title: fixture.title,
        filename: fixture.source.filename,
        uploadedAt: fixture.uploadedAt,
        revision: fixture.revision,
        latestRun: fixture.runs.at(-1) || null,
        previewImages: fixture.assets.filter(a => a.kind === 'character' && a.image)
          .slice(0, 3).map(a => a.image.thumbnailUrl),
      },
    ]);
  if (url === '/api/models' && uploadSettingsFixture)
    return Response.json({
      analysis: [{ id: 'fixture-analysis', name: 'Fixture Analysis' }],
      image: [{ id: 'fixture-image', name: 'Fixture Image' }],
      video: [{ id: 'fixture-video', name: 'Fixture Video' }],
    });
  if (url === '/api/models')
    return Response.json({
      analysis: configurationEnabled ? registryOptions().filter((m) => m.available) : [],
      image: [],
      video: [],
      catalog: configurationEnabled ? registryOptions() : [],
    });
  if (url === '/api/visual-styles')
    return Response.json({ styles: uploadSettingsFixture ? [uploadStyle] : [] });
  if (url.endsWith('/source'))
    return Response.json({
      version: 1,
      filename: fixture.source.filename,
      spans: [{ id: 'span_one', text: uploadedText || '虚构剧本全文。这里不是任何真实项目。' }],
    });
  if ((options.method || 'GET') === 'GET') return Response.json(copy(fixture));
  if (readOnly)
    return Response.json({ code: 'SERVICE_READ_ONLY', message: '只读服务' }, { status: 403 });
  const body = JSON.parse(options.body);
  if (body.revision !== fixture.revision)
    return Response.json(
      { code: 'CONFLICT', message: '项目已更新，请重新载入后核对' },
      { status: 409 },
    );
  if (uploadSettingsFixture && url.endsWith('/settings')) {
    fixture.settings = copy(body.settings);
    fixture.revision++;
    return Response.json(copy(fixture));
  }
  if (uploadSettingsFixture && url.endsWith('/style')) {
    assert.equal(body.styleId, uploadStyle.id);
    assert.equal(body.promptVersion, uploadStyle.promptVersion);
    fixture.style = copy(uploadStyle);
    fixture.revision++;
    return Response.json(copy(fixture));
  }
  if (url.endsWith('/visual-prompt-runs')) {
    assert.equal(fixture.assets.length, 0);
    fixture.runs.push({
      id: 'visual_empty',
      contract: 'visual-prompt-agent-v1',
      sourceVersion: 1,
      status: 'failed',
      stage: 'visualPrompt',
      callCount: 0,
      targets: [],
      errorCode: 'INVALID_INPUT',
      error: 'P3 已完成，但角色、场景、道具总数为 0，没有可生成视觉提示词的资产。',
    });
    fixture.revision++;
    return Response.json(copy(fixture));
  }
  if (url.endsWith('/image')) {
    const path = url.split('/');
    const asset = fixture.assets.find((a) => a.id === path[5]);
    assert.ok(asset);
    const owner = path[6] === 'looks' ? asset.looks.find((l) => l.id === path[7]) : asset;
    assert.ok(owner);
    owner.image = {
      fileId: body.fileId,
      originalUrl: '/api/projects/project_fictional/files/' + body.fileId + '/original',
      thumbnailUrl: '/api/projects/project_fictional/files/' + body.fileId + '/thumbnail',
    };
    fixture.catalogEditedByUser = true;
    fixture.revision++;
    return Response.json(copy(fixture));
  }
  if (url.endsWith('/looks')) {
    const asset = fixture.assets.find(
      (a) => url === '/api/projects/' + fixture.id + '/assets/' + a.id + '/looks',
    );
    assert.ok(asset);
    asset.looks.push({ ...body.look, id: 'look_added', origin: 'user' });
    fixture.revision++;
    return Response.json(copy(fixture));
  }
  if (url.endsWith('/assets')) {
    fixture.assets.push({
      ...body.asset,
      id: 'asset_added',
      sourceIds: [],
      looks: [],
      origin: 'user',
    });
    fixture.catalogEditedByUser = true;
    fixture.revision++;
    return Response.json(copy(fixture));
  }
  throw new Error('Unexpected fixture write ' + url);
};
const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
};
async function mount(
  route,
  p = project(),
  readonly = true,
  config = false,
  uploadSettings = false,
  records = [],
) {
  uploadSettingsFixture = uploadSettings;
  configurationEnabled = config;
  modelConfig = defaultRegistry();
  requestInterceptor = null;
  capabilityOverrides = {};
  if (root) await act(() => root.unmount());
  fixture = copy(p);
  readOnly = readonly;
  requests = [];
  coordinationRequests = [];
  editorLeases.clear();
  uploadedText = '';
  pending.clear();
  window.location.hash = '#' + route;
  root = createRoot(document.getElementById('test-root'));
  await act(async () =>
    root.render(
      <LanguageProvider>
        <HashRouter>
          <WorkspaceProvider>
            <ContextProbe />
            <WorkspaceFixture />
          </WorkspaceProvider>
        </HashRouter>
      </LanguageProvider>,
    ),
  );
  await flush();
}
async function navigateHash(route) {
  await act(async () => {
    const changed = new Promise((resolve) =>
      window.addEventListener('hashchange', resolve, { once: true }),
    );
    window.location.hash = '#' + route;
    await changed;
  });
  await flush();
}
function button(text) {
  return [...document.querySelectorAll('button')].find(
    (b) => b.textContent.trim() === text || b.getAttribute('aria-label') === text,
  );
}
after(async () => {
  if (root) await act(() => root.unmount());
  dom.window.close();
});
async function chooseAssetImage(label = '上传角色图片') {
  const input = document.querySelector<HTMLInputElement>('input[aria-label="' + label + '"]');
  assert.ok(input);
  assert.equal(input.disabled, false);
  Object.defineProperty(input, 'files', {
    value: [new File(['fictional PNG bytes'], '测试图片.png', { type: 'image/png' })],
    configurable: true,
  });
  await act(async () => input.dispatchEvent(new Event('change', { bubbles: true })));
  await flush();
}
test('catalog upload opens its file input without details and saves a visible thumbnail for all asset kinds', async () => {
  for (const [route, name, id] of [
    ['characters', '虚构小竹', 'character_one'],
    ['scenes', '虚构车站', 'location_one'],
    ['props', '虚构怀表', 'prop_one'],
  ]) {
    await mount('/playlet/project_fictional/' + route, project(), false);
    const input = document.querySelector<HTMLInputElement>('input[aria-label="上传' + name + '"]');
    let chooserOpened = 0;
    input.addEventListener('click', () => chooserOpened++);
    await act(() =>
      document.querySelector<HTMLButtonElement>('button[aria-label="上传' + name + '"]').click(),
    );
    await flush();
    assert.equal(chooserOpened, 1);
    assert.equal(document.querySelector('[role=dialog]'), null);
    assert.equal(
      requests.some((r) => r.method === 'POST'),
      false,
    );
    await chooseAssetImage('上传' + name);
    const writes = requests.filter((r) => r.method === 'POST');
    assert.equal(writes.length, 2);
    assert.equal(writes[0].url, '/api/upload');
    assert.equal(writes[0].body.get('thumbnail'), 'true');
    assert.equal(writes[1].url, '/api/projects/project_fictional/assets/' + id + '/image');
    assert.deepEqual(writes[1].body, { revision: 4, fileId: 'file_' + 'a'.repeat(32) });
    assert.equal(
      input.closest('article').querySelector('img').getAttribute('src'),
      '/api/projects/project_fictional/files/file_' + 'a'.repeat(32) + '/thumbnail?audience=workspace',
    );
    assert.equal(document.querySelector('[role=dialog]'), null);
    assert.equal(fixture.usedOrReservedMicroCny, 0);
  }
});
async function openCatalogMenu(name = '虚构小竹') {
  const more = document.querySelector<HTMLButtonElement>('button[aria-label="更多：' + name + '"]');
  await act(() => {
    more.focus();
    more.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
  });
  await flush();
  return more;
}
test('catalog mouse exit closes its menu without sticky focus while keyboard operation remains available', async () => {
  await mount('/playlet/project_fictional/characters', project(), false);
  const more = document.querySelector<HTMLButtonElement>('button[aria-label="更多：虚构小竹"]'),
    item = more.closest('article');
  await act(() => more.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true, button: 0 })));
  await flush();
  assert.ok(document.querySelector('[role=menu]'));
  assert.equal(item.getAttribute('data-pointer-mode'), 'true');
  const menu = document.querySelector('[role=menu]');
  await act(() =>
    item.dispatchEvent(new MouseEvent('pointerout', { bubbles: true, relatedTarget: menu })),
  );
  await flush();
  assert.ok(
    document.querySelector('[role=menu]'),
    'moving from card into its portal menu must keep it open',
  );
  await act(() =>
    menu.dispatchEvent(
      new MouseEvent('pointerout', { bubbles: true, relatedTarget: document.body }),
    ),
  );
  await flush();
  assert.equal(document.querySelector('[role=menu]'), null);
  assert.equal(item.hasAttribute('data-menu-open'), false);
  assert.equal(item.getAttribute('data-pointer-mode'), 'true');
  assert.equal(document.activeElement, more);
  await openCatalogMenu();
  await act(() =>
    document
      .querySelector('[role=menu]')
      .dispatchEvent(new MouseEvent('pointerout', { bubbles: true, relatedTarget: more })),
  );
  await flush();
  assert.ok(document.querySelector('[role=menu]'), 'moving back into the card must keep it open');
  await act(() =>
    item.dispatchEvent(
      new MouseEvent('pointerout', { bubbles: true, relatedTarget: document.body }),
    ),
  );
  await flush();
  assert.equal(document.querySelector('[role=menu]'), null);
  await openCatalogMenu();
  assert.equal(item.hasAttribute('data-pointer-mode'), false);
  assert.ok(document.querySelector('[role=menu]'));
  await act(() =>
    document
      .querySelector('[role=menu]')
      .dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
      ),
  );
  await flush();
  assert.equal(document.querySelector('[role=menu]'), null);
  assert.equal(document.activeElement, more);
  assert.equal(document.querySelector('[role=dialog]'), null);
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
});
test('catalog more menu uses existing delete confirmation and restores focus when closed', async () => {
  await mount('/playlet/project_fictional/characters', project(), false);
  const more = await openCatalogMenu();
  assert.equal(document.querySelector('[role=dialog]'), null);
  assert.equal(document.querySelector('[role=menuitem]').textContent, '删除');
  await act(() => document.querySelector<HTMLElement>('[role=menuitem]').click());
  await flush();
  assert.ok(button('确认删除角色'));
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
  await act(() => document.querySelector<HTMLButtonElement>('.jm-dialog-close').click());
  await flush();
  assert.equal(document.activeElement, more);
  assert.ok(document.querySelector('[aria-label="查看 虚构小竹"]'));
  await openCatalogMenu();
  await act(() => document.querySelector<HTMLElement>('[role=menuitem]').click());
  await flush();
  requestInterceptor = (url, options) => {
    if (
      url === '/api/projects/project_fictional/assets/character_one/delete' &&
      options.method === 'POST'
    ) {
      assert.deepEqual(JSON.parse(options.body), { revision: 4 });
      fixture.assets = fixture.assets.filter((a) => a.id !== 'character_one');
      fixture.revision++;
      return Response.json(copy(fixture));
    }
  };
  await act(() => button('确认删除角色').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  assert.equal(document.querySelector('[aria-label="查看 虚构小竹"]'), null);
  assert.equal(document.querySelector('[role=dialog]'), null);
});
test('catalog upload conflict retains receipt for explicit readback and association retry', async () => {
  await mount('/playlet/project_fictional/characters', project(), false);
  let first = true;
  requestInterceptor = (url) => {
    if (url.endsWith('/image') && first) {
      first = false;
      fixture.revision++;
      return Response.json({ code: 'CONFLICT', message: '项目已更新' }, { status: 409 });
    }
  };
  await chooseAssetImage('上传虚构小竹');
  assert.match(
    document.querySelector('.asset-catalog-error').textContent,
    /图片已上传，关联未确认/,
  );
  assert.equal(
    document.querySelector<HTMLButtonElement>('button[aria-label="上传虚构小竹"]').disabled,
    true,
  );
  await act(() => button('读取最新版本').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 2);
  await act(() => button('重试关联').click());
  await flush();
  assert.equal(requests.filter((r) => r.url === '/api/upload').length, 1);
  assert.deepEqual(requests.filter((r) => r.url.endsWith('/image')).at(-1).body, {
    revision: 5,
    fileId: 'file_' + 'a'.repeat(32),
  });
  assert.equal(document.querySelector('.asset-catalog-error'), null);
});
test('catalog tools obey write permissions and leaving the category aborts a pending upload', async () => {
  for (const state of ['readonly', 'running', 'unfinished', 'capability']) {
    const p = project();
    if (state === 'running') p.runs[1].status = 'running';
    if (state === 'unfinished') p.runs = [];
    await mount('/playlet/project_fictional/characters', p, state === 'readonly');
    if (state === 'capability') {
      capabilityOverrides.assetImages = false;
      await act(() => currentContext.refreshModels());
      await flush();
    }
    assert.equal(
      document.querySelector<HTMLButtonElement>('button[aria-label="上传虚构小竹"]').disabled,
      true,
      state,
    );
    await openCatalogMenu();
    assert.equal(
      document.querySelector('[role=menuitem]').getAttribute('aria-disabled'),
      state === 'capability' ? null : 'true',
    );
    assert.equal(
      requests.some((r) => r.method === 'POST'),
      false,
    );
  }
  await mount('/playlet/project_fictional/characters', project(), false);
  let resolve, signal;
  requestInterceptor = (url, options) => {
    if (url === '/api/upload') {
      signal = options.signal;
      return new Promise((r) => {
        resolve = r;
      });
    }
  };
  await chooseAssetImage('上传虚构小竹');
  assert.equal(
    document.querySelector<HTMLButtonElement>('button[aria-label="更多：虚构小竹"]').disabled,
    true,
  );
  await navigateHash('/playlet/project_fictional/scenes');
  assert.equal(signal.aborted, true);
  await act(async () =>
    resolve(
      Response.json(
        {
          fileId: 'file_' + 'a'.repeat(32),
          filename: '测试图片.png',
          contentType: 'image/png',
          sizeBytes: 19,
          originalUrl: '/api/files/file_' + 'a'.repeat(32) + '/original',
          thumbnailUrl: '/api/files/file_' + 'a'.repeat(32) + '/thumbnail',
        },
        { status: 201 },
      ),
    ),
  );
  await flush();
  assert.equal(
    requests.some((r) => r.url.endsWith('/image')),
    false,
  );
  assert.equal(fixture.revision, 4);
});
test('four detail targets upload with thumbnails, persist associations and preview original without model writes', async () => {
  const p = project();
  p.assets[0].looks = [{ id: 'look_one', name: '晚宴装', description: '礼服', origin: 'user' }];
  for (const [route, name, label, suffix] of [
    ['characters', '虚构小竹', '上传角色图片', 'character_one/image'],
    ['characters', '虚构小竹', '上传晚宴装 · 图片', 'character_one/looks/look_one/image'],
    ['scenes', '虚构车站', '上传场景图片', 'location_one/image'],
    ['props', '虚构怀表', '上传道具图片', 'prop_one/image'],
  ]) {
    await mount('/playlet/project_fictional/' + route, p, false);
    await act(() =>
      document.querySelector<HTMLButtonElement>('[aria-label="查看 ' + name + '"]').click(),
    );
    await flush();
    await chooseAssetImage(label);
    const writes = requests.filter((r) => r.method === 'POST');
    assert.equal(writes.length, 2);
    assert.equal(writes[0].url, '/api/upload');
    assert.equal(writes[0].body.get('thumbnail'), 'true');
    assert.equal(writes[1].url, '/api/projects/project_fictional/assets/' + suffix);
    assert.deepEqual(writes[1].body, { revision: 4, fileId: 'file_' + 'a'.repeat(32) });
    const saved = copy(fixture);
    assert.deepEqual(saved.runs, p.runs);
    assert.deepEqual(saved.visualPrompts, p.visualPrompts);
    assert.equal(saved.usedOrReservedMicroCny, 0);
    await act(async () => {
      await currentContext.refreshCurrentProject(p.id);
    });
    await flush();
    const card = document
      .querySelector<HTMLInputElement>('input[aria-label="' + label + '"]')
      .closest('figure');
    assert.equal(
      card.querySelector('img').getAttribute('src'),
      '/api/projects/project_fictional/files/file_' + 'a'.repeat(32) + '/thumbnail?audience=workspace',
    );
    assert.equal(document.querySelector('.asset-original-dialog'), null);
    assert.ok(
      [...document.querySelectorAll('img')].every(
        (img) => !img.getAttribute('src')?.split('?')[0].endsWith('/original'),
      ),
    );
    await act(() => card.querySelector<HTMLButtonElement>('.asset-image-open').click());
    await flush();
    assert.equal(
      document.querySelector('.asset-original-dialog img').getAttribute('src'),
      '/api/projects/project_fictional/files/file_' + 'a'.repeat(32) + '/original?audience=workspace',
    );
    await act(() =>
      document
        .querySelector<HTMLButtonElement>('.asset-original-dialog button[aria-label="关闭"]')
        .click(),
    );
    await flush();
    assert.ok(document.querySelector('.asset-detail-layout'));
    assert.equal(document.activeElement, card.querySelector('.asset-image-open'));
    assert.equal(document.querySelector('.asset-original-dialog img'), null);
    assert.equal(
      requests.some((r) => r.method === 'POST' && r.url.includes('runs')),
      false,
    );
  }
});
test('image association conflict retains upload, requires readback then explicitly retries the same file', async () => {
  await mount('/playlet/project_fictional/characters', project(), false);
  await act(() =>
    document.querySelector<HTMLButtonElement>('[aria-label="查看 虚构小竹"]').click(),
  );
  await flush();
  let first = true;
  requestInterceptor = async (url, options) => {
    if (url.endsWith('/image') && first) {
      first = false;
      fixture.revision++;
      return Response.json({ code: 'CONFLICT', message: '项目已更新' }, { status: 409 });
    }
  };
  await chooseAssetImage();
  assert.match(
    document.querySelector('.asset-image-card [role="alert"]').textContent,
    /图片已上传，关联未确认/,
  );
  assert.equal(button('上传').disabled, true);
  await act(async () => button('读取最新版本').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 2);
  assert.ok(button('重试关联'));
  await act(async () => button('重试关联').click());
  await flush();
  assert.equal(requests.filter((r) => r.url === '/api/upload').length, 1);
  assert.deepEqual(requests.filter((r) => r.url.endsWith('/image')).at(-1).body, {
    revision: 5,
    fileId: 'file_' + 'a'.repeat(32),
  });
  assert.ok(document.querySelector('.asset-image-preview img'));
});
test('ambiguous association is resolved by readback without another upload or save', async () => {
  await mount('/playlet/project_fictional/props', project(), false);
  await act(() =>
    document.querySelector<HTMLButtonElement>('[aria-label="查看 虚构怀表"]').click(),
  );
  await flush();
  requestInterceptor = async (url, options) => {
    if (url.endsWith('/image')) {
      const input = JSON.parse(options.body);
      fixture.assets[2].image = {
        fileId: input.fileId,
        originalUrl: '/api/projects/project_fictional/files/' + input.fileId + '/original',
        thumbnailUrl: '/api/projects/project_fictional/files/' + input.fileId + '/thumbnail',
      };
      fixture.revision++;
      throw new TypeError('response lost');
    }
  };
  await chooseAssetImage('上传道具图片');
  await act(async () => button('读取最新版本').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 2);
  assert.equal(button('重试关联'), undefined);
  assert.ok(document.querySelector('.asset-image-preview img'));
});
test('closing a detail during upload aborts transport and never starts association from a late receipt', async () => {
  await mount('/playlet/project_fictional/characters', project(), false);
  await act(() =>
    document.querySelector<HTMLButtonElement>('[aria-label="查看 虚构小竹"]').click(),
  );
  await flush();
  let resolve, signal;
  requestInterceptor = async (url, options) => {
    if (url === '/api/upload') {
      signal = options.signal;
      return new Promise((r) => {
        resolve = r;
      });
    }
  };
  await chooseAssetImage();
  assert.equal(button('上传并保存中…').disabled, true);
  await act(() => document.querySelector<HTMLButtonElement>('.jm-dialog-close').click());
  await flush();
  assert.equal(signal.aborted, true);
  await act(async () =>
    resolve(
      Response.json(
        {
          fileId: 'file_' + 'a'.repeat(32),
          filename: '测试图片.png',
          contentType: 'image/png',
          sizeBytes: 19,
          originalUrl: '/api/files/file_' + 'a'.repeat(32) + '/original',
          thumbnailUrl: '/api/files/file_' + 'a'.repeat(32) + '/thumbnail',
        },
        { status: 201 },
      ),
    ),
  );
  await flush();
  assert.equal(
    requests.some((r) => r.url.endsWith('/image')),
    false,
  );
  assert.equal(fixture.revision, 4);
});
test('image upload is unavailable for readonly, running, unfinished and missing-capability details', async () => {
  for (const state of ['readonly', 'running', 'unfinished', 'capability']) {
    const p = project();
    if (state === 'running') p.runs[1].status = 'running';
    if (state === 'unfinished') p.runs = [];
    await mount('/playlet/project_fictional/characters', p, state === 'readonly');
    if (state === 'capability') {
      capabilityOverrides.assetImages = false;
      await act(async () => {
        await currentContext.refreshModels();
      });
      await flush();
    }
    await act(() =>
      document.querySelector<HTMLButtonElement>('[aria-label="查看 虚构小竹"]').click(),
    );
    await flush();
    assert.equal(button('上传').disabled, true, state);
    assert.equal(
      requests.some((r) => r.method === 'POST'),
      false,
    );
  }
});
test('look image save advances only its own text draft and preserves unsaved text', async () => {
  const p = project();
  p.assets[0].looks = [{ id: 'look_one', name: '晚宴装', description: '礼服', origin: 'user' }];
  await mount('/playlet/project_fictional/characters', p, false);
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构小竹"]').click());
  await flush();
  await act(() => document.querySelector<HTMLButtonElement>('.asset-look-actions button').click());
  await flush();
  const field = document.getElementById('asset-form-name');
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(
      field,
      '未保存的新名称',
    );
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await chooseAssetImage('上传附加形象图片');
  assert.equal((field as HTMLInputElement).value, '未保存的新名称');
  requestInterceptor = async (url, options) => {
    if (url.endsWith('/looks/look_one')) {
      const body = JSON.parse(options.body);
      assert.equal(body.revision, 5);
      Object.assign(fixture.assets[0].looks[0], body.look);
      fixture.revision++;
      return Response.json(copy(fixture));
    }
  };
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(fixture.assets[0].looks[0].name, '未保存的新名称');
  assert.equal(fixture.assets[0].looks[0].image.fileId, 'file_' + 'a'.repeat(32));
  assert.equal(requests.filter((r) => r.method === 'POST').length, 3);
});
test('thumbnail and original read failures retain saved association and offer local preview retry', async () => {
  const p = project(),
    fileId = 'file_' + 'a'.repeat(32);
  p.assets[2].image = {
    fileId,
    originalUrl: '/api/projects/project_fictional/files/' + fileId + '/original',
    thumbnailUrl: '/api/projects/project_fictional/files/' + fileId + '/thumbnail',
  };
  await mount('/playlet/project_fictional/props', p, true);
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构怀表"]').click());
  await flush();
  await act(() =>
    document.querySelector('.asset-image-preview img').dispatchEvent(new Event('error')),
  );
  await flush();
  assert.match(document.querySelector('.asset-image-preview').textContent, /缩略图读取失败/);
  await act(() => button('重试预览').click());
  await flush();
  assert.ok(document.querySelector('.asset-image-preview img'));
  await act(() => button('预览').click());
  await flush();
  await act(() =>
    document.querySelector('.asset-original-dialog img').dispatchEvent(new Event('error')),
  );
  await flush();
  assert.match(document.querySelector('.asset-original-dialog').textContent, /原图读取失败/);
  await act(() =>
    document.querySelector<HTMLButtonElement>('.asset-original-dialog button').click(),
  );
  await flush();
  assert.ok(document.querySelector('.asset-original-dialog img'));
  assert.deepEqual(fixture, p);
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
});
test('ported workspace reads actual counts and preserves readonly service with no POST', async () => {
  await mount('/playlet/project_fictional/characters');
  assert.ok(document.querySelector('aside a[href="#/projects"]'));
  assert.match(document.body.textContent, /全部角色.*1.*全部场景.*1.*全部道具.*1/);
  assert.equal(button('新增角色').disabled, true);
  assert.equal(button('3分集视频'), undefined);
  assert.equal(
    document.querySelector('aside a[aria-label="资产库"]').getAttribute('aria-current'),
    'page',
  );
  assert.ok(document.querySelector('.asset-category-actions .asset-task-toolbar'));
  assert.equal(document.querySelector('.jm-status-banner'), null);
  assert.ok(document.querySelector('.xy-workflow-dock [role=status]'));
  assert.equal(button('下一步').disabled, true);
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});
test('unfinished P3 and running visual tasks block ordinary writes', async () => {
  for (const state of ['unstarted', 'running', 'failed', 'cancelled', 'partial']) {
    const p = project();
    if (state === 'unstarted') p.runs = [];
    else if (state === 'partial') {
      p.runs[1].status = 'running';
      p.visualPrompts.pop();
    } else {
      p.runs[0].status = state;
      p.runs[0].error = state === 'failed' ? '虚构失败原因' : '';
    }
    await mount('/playlet/project_fictional/characters', p, false);
    assert.equal(button('新增角色').disabled, true, state);
    if (state === 'failed') assert.match(document.body.textContent, /项目失败.*虚构失败原因/);
    assert.equal(
      requests.some((r) => r.method !== 'GET'),
      false,
      state,
    );
  }
});
test('P3 completion opens editing without visual completion and scoped terminal results do not close it', async () => {
  for (const state of ['unstarted', 'failed', 'cancelled']) {
    const p = project();
    p.visualPrompts = [];
    if (state === 'unstarted') p.runs = p.runs.slice(0, 1);
    else {
      p.runs[1].status = state;
      p.runs[1].target = { assetId: 'character_one' };
      p.runs[1].targets = [p.runs[1].target];
    }
    await mount('/playlet/project_fictional/characters', p, false);
    assert.equal(button('新增角色').disabled, false, state);
    assert.equal(
      requests.some((r) => r.method !== 'GET'),
      false,
    );
  }
});
test('completed visual entitlement enables CRUD and sends the current API payload without any generation', async () => {
  await mount('/playlet/project_fictional/scenes', project(), false);
  await act(() => button('新增场景').click());
  await flush();
  const input = document.getElementById('asset-form-name');
  assert.ok(input);
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(
      input,
      '虚构新车站',
    );
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(async () =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const writes = requests.filter((r) => r.method === 'POST');
  assert.equal(writes.length, 1);
  assert.deepEqual(writes[0], {
    url: '/api/projects/project_fictional/assets',
    method: 'POST',
    body: {
      revision: 4,
      asset: {
        kind: 'location',
        name: '虚构新车站',
        description: '',
        aliases: [],
        character: null,
      },
    },
  });
  assert.match(document.body.textContent, /虚构新车站/);
  assert.match(document.body.textContent, /资产内容待完善/);
  assert.equal(
    requests.some((r) => r.url.includes('-runs') || r.url.endsWith('/runs')),
    false,
  );
});
test('outline directly displays full source without story fields or save button, navigation never starts a task', async () => {
  const p = project();
  p.outline = {
    protagonist: '虚构船长',
    genre: '虚构冒险',
    summary: '完整梗概',
    background: '海上',
    logline: '寻找旧航线',
  };
  await mount('/playlet/project_fictional/outline', p);
  for (const value of Object.values(p.outline))
    assert.equal(document.body.textContent.includes(value), false);
  assert.ok(document.body.textContent.includes('虚构剧本全文。这里不是任何真实项目。'));
  assert.equal(document.querySelector('.jm-outline-layout .jm-status-banner'), null);
  assert.equal(
    document.body.textContent.includes(
      '完整视觉提示词生成成功后，才可新增、编辑或删除资产与附加形象。',
    ),
    false,
  );
  assert.equal(button('生成大纲'), undefined);
  assert.equal(button('保存设置'), undefined);
  assert.ok(document.querySelector('.xy-workflow-dock'));
  await act(() => button('下一步').click());
  await flush();
  assert.match(window.location.hash, /characters$/);
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});
test('an old response cannot overwrite the newly selected project', async () => {
  await mount('/playlet/project_fictional/characters');
  let resolveA;
  const oldURL = '/api/projects/project_slow';
  pending.set(
    oldURL,
    new Promise((resolve) => {
      resolveA = resolve;
    }),
  );
  await navigateHash('/playlet/project_slow/characters');
  const b = project('project_new');
  b.title = '当前虚构新项目';
  fixture = b;
  await navigateHash('/playlet/project_new/characters');
  await act(async () => {
    resolveA(Response.json(project('project_slow')));
  });
  await flush();
  assert.equal(document.querySelector('h1')?.textContent, '当前虚构新项目');
});

test('empty catalog can record its HTTP 200 failure without model configuration and remains readonly', async () => {
  const p = project();
  p.assets = [];
  p.visualPrompts = [];
  p.runs = p.runs.slice(0, 1);
  await mount('/playlet/project_fictional/characters', p, false);
  const start = button('完善资产');
  assert.equal(start.disabled, false);
  await act(() => start.click());
  await flush();
  assert.match(document.body.textContent, /项目失败.*没有可生成视觉提示词的资产/);
  assert.equal(button('新增角色').disabled, true);
  const writes = requests.filter((r) => r.method !== 'GET');
  assert.deepEqual(writes, [
    {
      url: '/api/projects/project_fictional/visual-prompt-runs',
      method: 'POST',
      body: { revision: 4 },
    },
  ]);
  assert.equal(fixture.runs[0].status, 'completed');
  assert.equal(fixture.runs[1].callCount, 0);
});

test('script, storyboard and asset pages expose the same complete read-only parameter strip without hidden settings', async () => {
  const p = project();
  p.settings = {
    aspectRatio: '16:9',
    llmModel: 'saved-analysis',
    imageModel: 'saved-image',
    videoModel: 'saved-video',
  };
  for (const route of ['outline', 'production', 'characters']) {
    await mount('/playlet/project_fictional/' + route, p);
    const summary = document.querySelector('.xy-project-selections');
    assert.ok(summary);
    for (const value of ['16:9', 'saved-analysis', 'saved-image', 'saved-video'])
      assert.ok(summary.textContent.includes(value));
    assert.equal(document.querySelector('.editor-project-summary'), null);
    assert.equal(button('项目参数'), undefined);
    assert.equal(
      requests.some((r) => r.method !== 'GET'),
      false,
    );
  }
});
test('confirmed project choices appear in header as read-only values, including unavailable IDs', async () => {
  const p = project();
  p.style = {
    id: 'own-style',
    name: '自家固定风格',
    previewUrl: '/demo/assets/styles/cinematic35mm.png',
    promptVersion: 'v1',
    prompts: { zh: '完整风格', en: 'full style' },
  };
  p.settings = {
    aspectRatio: '16:9',
    llmModel: 'saved-analysis',
    imageModel: 'saved-image',
    videoModel: 'saved-video',
  };
  await mount('/playlet/project_fictional/characters', p, false);
  const group = document.querySelector('.xy-project-selections');
  assert.equal(group.querySelector('img')?.getAttribute('src'), '/demo/assets/styles/cinematic35mm.png?v=daydream-20261019');
  assert.equal(p.style.previewUrl, '/demo/assets/styles/cinematic35mm.png');
  for (const text of ['自家固定风格', '16:9', 'saved-analysis', 'saved-image', 'saved-video'])
    assert.ok(group.textContent.includes(text));
  assert.equal(group.querySelector('button,a,select,input'), null);
  assert.equal(document.querySelector('button[aria-label="项目设置"]'), null);
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});

test('home previews show only the available character thumbnails with one to three occupied columns', async () => {
  for (const count of [0, 1, 2, 4]) {
    const p = project();
    p.assets = Array.from({length: count}, (_, i) => ({
      ...p.assets[0], id: 'character-' + i, kind: 'character',
      image: {fileId: 'file-' + i, originalUrl: '/api/projects/project_fictional/files/file-' + i + '/original', thumbnailUrl: '/api/projects/project_fictional/files/file-' + i + '/thumbnail'},
    }));
    await mount('/projects', p);
    const images = [...document.querySelectorAll('.project-list-card img')];
    assert.equal(images.length, Math.min(count, 3));
    assert.deepEqual(images.map(image => image.getAttribute('src')),
      Array.from({length: Math.min(count, 3)}, (_, i) => '/api/projects/project_fictional/files/file-' + i + '/thumbnail?audience=workspace'));
    const grid = document.querySelector('.project-list-card > div > .grid');
    assert.equal(grid.children.length, Math.max(1, Math.min(count, 3)));
    assert.equal(requests.some(r => r.url === '/api/projects/' + p.id), false,
      'project cards must not fetch full projects for previews');
    assert.equal(requests.some(r => r.method !== 'GET'), false);
  }
});

async function openModelPanel(scope = '') {
  if (document.querySelector(scope + ' .xy-model-panel')) return;
  const trigger = document.querySelector(scope + ' button[aria-label="模型"]');
  assert.ok(trigger);
  await act(() => trigger.click());
  await flush();
}
async function pickModel(category: string, name: string, scope = '') {
  await openModelPanel(scope);
  const cat = [...document.querySelectorAll(scope + ' .xy-model-cat')].find((e) =>
    e.textContent.includes(category),
  );
  assert.ok(cat, category);
  await act(() => cat.click());
  await flush();
  const row = [...document.querySelectorAll(scope + ' .xy-model-panel .xy-model-row')].find((e) =>
    e.textContent.includes(name),
  );
  assert.ok(row, name);
  await act(() => row.click());
  await flush();
  await act(() =>
    document.body.dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    ),
  );
  await flush();
}

test('model picker keeps prices and free badges beside the name, with the description below, without starting work', async () => {
  await mount('/projects', project(), false);
  const rule = {
    unitPrice: 10,
    freeEnabled: true,
    freeStart: '2026-10-17T00:00:00Z',
    freeEnd: '2026-10-24T00:00:00Z',
  };
  const analysis = [
    {
      id: 'free-analysis',
      name: '限免分析模型',
      connectionName: '测试语言连接',
      billingUnit: 'characters-100',
      creditPrice: rule,
      free: true,
    },
    {
      id: 'paid-analysis',
      name: '收费分析模型',
      billingUnit: 'characters-100',
      creditPrice: rule,
      free: false,
    },
  ];
  const image = [
    {
      id: 'mock-image',
      name: 'Mock Image',
      description: '本地图片效果测试',
      provider: 'mock',
      connectionName: 'Mock 本地测试',
      billingUnit: 'image',
      creditPrice: { ...rule, unitPrice: 5, freeEnabled: false },
      free: false,
    },
  ];
  const video = [
    {
      id: 'mock_video',
      name: 'Mock Video',
      provider: 'mock',
      connectionName: 'Mock 本地测试',
      billingUnit: 'second',
      creditPrice: { ...rule, unitPrice: 8, freeEnabled: false },
      free: false,
    },
  ];
  requestInterceptor = (url) =>
    url === '/api/models'
      ? Response.json({ analysis, image, video, catalog: [...analysis, ...image, ...video] })
      : null;
  await act(() => currentContext.refreshModels());
  await openModelPanel();
  const panel = document.querySelector('.xy-model-panel')!;
  assert.equal(panel.querySelector('input'), null);
  assert.equal(
    panel.parentElement,
    document.body,
    'home menu must escape the upload stacking context',
  );
  const freeRow = [...panel.querySelectorAll('.xy-model-row')].find((node) =>
    node.textContent?.includes('限免分析模型'),
  )!;
  assert.equal(
    freeRow.querySelector('.xy-model-heading > span:first-child')?.textContent,
    '限免分析模型',
  );
  assert.equal(
    freeRow.querySelector('.xy-model-heading .xy-model-charge .xy-model-free-badge')?.textContent,
    '限免',
  );
  assert.equal(freeRow.querySelector('.xy-model-price'), null);
  assert.doesNotMatch(freeRow.textContent || '', /原价|2026|0 积分|至/);
  const paidRow = [...panel.querySelectorAll('.xy-model-row')].find((node) =>
    node.textContent?.includes('收费分析模型'),
  )!;
  assert.equal(paidRow.querySelector('.xy-model-price')?.textContent, '10 积分/100 字');
  assert.equal(
    paidRow.querySelector('.xy-model-free-badge'),
    null,
    'server free=false is authoritative, even with a promotion rule',
  );
  for (const [category, name, price] of [
    ['图像模型', 'Mock Image', '5 积分/张'],
    ['视频模型', 'Mock Video', '8 积分/秒'],
  ]) {
    await act(() =>
      [...panel.querySelectorAll<HTMLButtonElement>('.xy-model-cat')]
        .find((node) => node.textContent?.includes(category))!
        .click(),
    );
    const row = [...panel.querySelectorAll<HTMLButtonElement>('.xy-model-row')].find((node) =>
      node.textContent?.includes(name),
    )!;
    assert.equal(
      row.querySelector('.xy-model-heading .xy-model-charge .xy-model-price')?.textContent,
      price,
    );
    assert.equal(row.querySelector('.mc-option-source'), null);
    if (category === '图像模型') {
      assert.equal(row.querySelector('.xy-model-description')?.textContent, '本地图片效果测试');
      assert.equal(
        row.querySelector('.xy-model-description')?.previousElementSibling?.className,
        'xy-model-heading',
      );
    }
    await act(() => row.click());
    assert.equal(row.getAttribute('aria-pressed'), 'true');
  }
  await act(() =>
    document.body.dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    ),
  );
  await flush();
  assert.equal(document.querySelector('.xy-model-panel'), null);
  assert.equal(document.activeElement?.getAttribute('aria-label'), '模型');
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});
async function chooseAnalysisForUpload() {
  await pickModel('分析模型', 'Fixture Analysis');
}
test('pasted full text stays local until explicit upload and uses general upload then file-ID project creation without generation', async () => {
  await mount('/projects', project(), false, false, true);
  await chooseAnalysisForUpload();
  await act(() => button('粘贴文本').click());
  await flush();
  const text = '  虚构剧本标题\n\n完整中文对白、标点与尾部空白。  \n';
  const field = document.querySelector('textarea[aria-label="剧本正文"]');
  assert.ok(field);
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(field, text);
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(() => button('使用文本').click());
  await flush();
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
  assert.match(document.body.textContent, /pasted-script.txt/);
  await act(() => button('开始解析').click());
  await flush();
  assert.equal(uploadedText, text);
  const writes = requests.filter((r) => r.method !== 'GET');
  assert.equal(writes.length, 2);
  assert.equal(writes[0].url, '/api/upload');
  assert.equal(writes[0].body.get('file').name, 'pasted-script.txt');
  assert.equal(writes[1].url, '/api/projects');
  assert.equal(writes[1].body.title, 'pasted-script');
  assert.equal(writes[1].body.fileId, 'file_' + 'a'.repeat(32));
  assert.match(window.location.hash, /project_uploaded\/outline$/);
  assert.deepEqual(fixture.runs, []);
  assert.equal(fixture.usedOrReservedMicroCny, 0);
});
test('readonly home shows real projects and blocks file and pasted uploads without writes', async () => {
  await mount('/projects');
  assert.match(document.body.textContent, /虚构测试剧本/);
  assert.match(document.body.textContent, /真实项目只读浏览，上传已禁用/);
  assert.equal(document.querySelector('input[type=file]').disabled, true);
  assert.equal(button('粘贴文本').disabled, true);
  assert.equal(button('开始解析').disabled, true);
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});

test('sidebar shows this product and only working navigation', async () => {
  await mount('/projects');
  assert.equal(document.querySelector('aside').getAttribute('data-collapsed'), 'true');
  await act(() => button('展开侧栏').click());
  await flush();
  const sidebar = document.querySelector('aside');
  assert.match(sidebar.textContent, /短剧 Agent/);
  assert.doesNotMatch(sidebar.textContent, /Dramora|首页|工具|素材/);
  assert.equal(sidebar.querySelectorAll('nav a').length, 2);
  assert.ok(sidebar.querySelector('a[href="#/creations"]'));
  assert.equal(sidebar.querySelector('a[href="#/model-config"]'), null);
  assert.equal(sidebar.querySelectorAll('a[aria-current=page]').length, 1);
  assert.equal(sidebar.querySelectorAll('nav a[aria-disabled=true]').length, 0);
});
test('workspace sidebar starts collapsed at every width and expands only on its toggle', async () => {
  const originalMedia = globalThis.matchMedia;
  try {
    for (const narrow of [false, true]) {
      globalThis.matchMedia = () => ({
        matches: narrow,
        addEventListener() {},
        removeEventListener() {},
      });
      await mount('/projects');
      const sidebar = document.querySelector('aside');
      assert.equal(sidebar.getAttribute('data-collapsed'), 'true');
      assert.equal(button('展开侧栏').getAttribute('aria-expanded'), 'false');
      await act(() => {
        sidebar.dispatchEvent(new MouseEvent('pointerover', { bubbles: true }));
        button('展开侧栏').focus();
        window.dispatchEvent(new Event('resize'));
      });
      await flush();
      await navigateHash('/projects?view=all');
      assert.equal(sidebar.getAttribute('data-collapsed'), 'true');
      await act(() => button('展开侧栏').click());
      await flush();
      assert.equal(sidebar.getAttribute('data-collapsed'), 'false');
      await navigateHash('/projects');
      assert.equal(sidebar.getAttribute('data-collapsed'), 'false');
      await act(() => button('收起侧栏').click());
      await flush();
      assert.equal(sidebar.getAttribute('data-collapsed'), 'true');
      await mount('/projects');
      assert.equal(document.querySelector('aside').getAttribute('data-collapsed'), 'true');
      assert.equal(
        requests.some((r) => r.method !== 'GET'),
        false,
      );
    }
  } finally {
    globalThis.matchMedia = originalMedia;
  }
});
test('character click opens the same detail modal as other assets, preserving full fields, looks and source', async () => {
  const p = project();
  p.assets[0].description = '完整角色说明，不能从首句推导或拆分其他事实。';
  p.assets[0].character.biography = '完整小传';
  p.assets[0].aliases = ['虚构别名'];
  p.assets[0].looks = [
    { id: 'existing_look', name: '雨衣形象', description: '完整附加文字形象', origin: 'user' },
  ];
  p.visualPrompts.push({
    target: { assetId: 'character_one', lookId: 'existing_look' },
    text: '完整附加视觉正文',
    stale: true,
    sourceVersion: 1,
    runId: 'visual',
  });
  await mount('/playlet/project_fictional/characters', p, true);
  const hash = window.location.hash;
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构小竹"]').click());
  await flush();
  const dialog = document.querySelector('[role=dialog]');
  assert.equal(window.location.hash, hash);
  assert.equal(dialog.getAttribute('aria-label'), '查看角色');
  assert.equal(
    (document.getElementById('asset-description') as HTMLTextAreaElement).value,
    p.assets[0].description,
  );
  for (const content of [
    '虚构别名',
    '雨衣形象',
    '完整附加文字形象',
    '完整附加视觉正文',
    '虚构剧本全文。这里不是任何真实项目。',
  ])
    assert.ok(dialog.textContent.includes(content), content);
  assert.ok(dialog.textContent.includes('角色资料'));
  assert.equal(dialog.textContent.includes('基础形象（角色资料）'), false);
  assert.ok(dialog.textContent.includes('视觉提示词'));
  assert.ok(document.querySelector('[role=dialog] textarea:disabled'));
  assert.equal(
    [...dialog.querySelectorAll('button')].find((b) => b.textContent.trim() === '新增附加形象')
      .disabled,
    true,
  );
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});
test('fixed style name renders translated on the project summary and no longer inside asset details', async () => {
  const { default: i18n } = await import('../src/i18n');
  try {
    const p = project();
    p.style = copy(uploadStyle);
    const snapshot = copy(p.style);
    await act(async () => {
      await i18n.changeLanguage('zh');
    });
    await mount('/playlet/project_fictional/outline', p);
    assert.ok(
      document.querySelector('.xy-project-selections').textContent.includes(uploadStyle.name),
    );
    await act(async () => {
      await i18n.changeLanguage('en');
    });
    await flush();
    assert.ok(
      document
        .querySelector('.xy-project-selections')
        .textContent.includes(
          i18n.t('catalog.style.' + uploadStyle.id + '.name', { defaultValue: uploadStyle.name }),
        ),
    );
    await act(async () => {
      await i18n.changeLanguage('zh');
    });
    await mount('/playlet/project_fictional/characters', p);
    await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构小竹"]').click());
    await flush();
    const dialog = document.querySelector('[role=dialog]');
    assert.ok(dialog);
    assert.equal(dialog.textContent.includes('项目固定风格'), false);
    assert.equal(dialog.querySelector('.asset-detail-style'), null);
    assert.equal(dialog.querySelector('.asset-detail-project'), null);
    assert.equal(dialog.querySelector('.asset-detail-origin'), null);
    assert.deepEqual(fixture.style, snapshot);
    assert.equal(
      requests.some((r) => r.method !== 'GET'),
      false,
    );
  } finally {
    await act(async () => {
      await i18n.changeLanguage('zh');
    });
  }
});

test('character details retain ordinary look creation via the canonical parent and DTO with no model task', async () => {
  await mount('/playlet/project_fictional/characters', project(), false);
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构小竹"]').click());
  await flush();
  assert.equal(
    document.querySelector('[role=dialog]').textContent.includes('基础资料暂未填写'),
    false,
  );
  assert.ok(document.querySelector('[role=dialog]').textContent.includes('虚构纯正文'));
  await act(() => button('新增附加形象').click());
  await flush();
  assert.equal(document.querySelectorAll('[role=dialog]').length, 2);
  const input = document.getElementById('asset-form-name');
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(
      input,
      '虚构夜装',
    );
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const writes = requests.filter((r) => r.method !== 'GET');
  assert.deepEqual(writes, [
    {
      url: '/api/projects/project_fictional/assets/character_one/looks',
      method: 'POST',
      body: { revision: 4, look: { name: '虚构夜装', description: '' } },
    },
  ]);
  assert.ok(document.body.textContent.includes('虚构夜装'));
  assert.equal(fixture.assets.length, 3);
  assert.equal(fixture.assets[0].looks.length, 1);
  assert.equal(
    requests.some((r) => r.url.includes('-runs') || r.url.endsWith('/runs')),
    false,
  );
});

test('language switching translates live UI, retains script and asset data, persists locale, and sends no writes', async () => {
  const { default: i18n } = await import('../src/i18n');
  try {
    await mount('/playlet/project_fictional/outline');
    const script = document.querySelector('.jm-script-body pre').textContent;
    await act(async () => {
      await i18n.changeLanguage('en');
    });
    await flush();
    assert.equal(document.documentElement.lang, 'en');
    assert.equal(localStorage.getItem('language'), 'en');
    assert.equal(document.querySelector('#script-content-title').textContent, 'Script content');
    assert.equal(document.querySelector('.jm-script-body pre').textContent, script);
    assert.ok(button('Next'));
    assert.ok(document.querySelector('.xy-workflow-dock'));
    assert.match(
      document.querySelector('.xy-project-selections').textContent,
      /No style selected9:16/,
    );
    await navigateHash('/playlet/project_fictional/characters');
    assert.match(document.body.textContent, /All characters.*All scenes.*All props/);
    await act(() => document.querySelector<HTMLElement>('[aria-label="View 虚构小竹"]').click());
    await flush();
    const detail = document.querySelector('[role=dialog]');
    assert.equal(detail.getAttribute('aria-label'), 'View Character');
    assert.ok(detail.textContent.includes('完整角色资料'));
    assert.ok(detail.textContent.includes('Character details'));
    assert.ok(detail.textContent.includes('Visual prompt'));
    assert.equal(
      requests.some((r) => r.method !== 'GET'),
      false,
    );
  } finally {
    await act(async () => {
      await i18n.changeLanguage('zh');
    });
    await flush();
  }
});
test('current UI translations cover both languages and interpolate counts without altering custom style text', async () => {
  const { catalogZh, catalogEn } = await import('../src/locales/catalog');
  for (const key of Object.keys(catalogZh)) assert.ok(key in catalogEn, key);
  const { default: i18n } = await import('../src/i18n');
  assert.equal(i18n.t('catalog.第 {index} 段', { lng: 'en', index: 3 }), 'Passage 3');
  assert.equal(i18n.t('catalog.style.cinematic35mm.name', { lng: 'en' }), 'Cinematic');
  assert.equal(
    i18n.t('catalog.style.user_custom.name', { lng: 'en', defaultValue: '用户自定风格' }),
    '用户自定风格',
  );
});
test('switching languages inside an edit form preserves an unsaved draft and does not submit it', async () => {
  const { default: i18n } = await import('../src/i18n');
  try {
    await mount('/playlet/project_fictional/scenes', project(), false);
    await act(() => button('新增场景').click());
    await flush();
    const field = document.getElementById('asset-form-name');
    await act(() => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(
        field,
        '虚构双语草稿',
      );
      field.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => {
      await i18n.changeLanguage('en');
    });
    await flush();
    assert.equal(document.getElementById('asset-form-name').value, '虚构双语草稿');
    assert.equal(document.querySelector('label[for="asset-form-name"]').textContent, 'Name');
    assert.ok(button('Save'));
    assert.equal(
      requests.some((r) => r.method !== 'GET'),
      false,
    );
  } finally {
    await act(async () => {
      await i18n.changeLanguage('zh');
    });
    await flush();
  }
});

test('sidebar preferences toggle language and appearance with a single selection and no project writes', async () => {
  const { default: i18n } = await import('../src/i18n');
  const key = (el: Element, name: string) =>
    el.dispatchEvent(new window.KeyboardEvent('keydown', { key: name, bubbles: true }));
  try {
    await mount('/playlet/project_fictional/characters');
    assert.equal(document.querySelector('.xy-project-selections button'), null);
    const before = copy(fixture),
      trigger = document.querySelector<HTMLButtonElement>('aside button[aria-label="工作台菜单"]');
    assert.ok(trigger);
    await act(() => {
      trigger.focus();
      key(trigger, 'ArrowDown');
    });
    await flush();
    const language = document.querySelector('[role=menuitemcheckbox][data-preference=language]');
    assert.ok(language);
    assert.equal(language.getAttribute('aria-checked'), 'false');
    assert.equal(
      language.querySelector('.xy-language-switch>span[data-active=true]').textContent,
      '中',
    );
    assert.equal(document.querySelectorAll('[role=menu]').length, 1);
    assert.equal(document.querySelectorAll('[role=menuitemradio]').length, 0);
    await act(() => {
      (language as HTMLElement).click();
    });
    await flush();
    assert.equal(localStorage.getItem('language'), 'en');
    assert.ok(document.querySelector('aside button[aria-label="Workspace menu"]'));
    assert.ok(
      document.querySelector('[role=menu]'),
      'changing language retains the open preference menu',
    );
    const appearance = document.querySelector('[role=menuitemcheckbox][data-preference=theme]');
    assert.ok(appearance);
    assert.equal(
      appearance.getAttribute('aria-checked'),
      String(localStorage.getItem('theme') === 'dark'),
    );
    assert.ok(appearance.querySelector('.xy-theme-switch-thumb'));
    assert.equal(
      document.querySelector('[data-preference=language]').getAttribute('aria-checked'),
      'true',
    );
    assert.equal(
      document.querySelector('.xy-language-switch>span[data-active=true]').textContent,
      'EN',
    );
    const next = localStorage.getItem('theme') === 'dark' ? 'Light' : 'Dark';
    await act(() => {
      (appearance as HTMLElement).click();
    });
    await flush();
    assert.equal(localStorage.getItem('theme'), next.toLowerCase());
    assert.equal(document.documentElement.classList.contains('dark'), next === 'Dark');
    assert.deepEqual(fixture, before);
    assert.equal(
      requests.some((r) => r.method !== 'GET'),
      false,
    );
  } finally {
    await act(async () => {
      await i18n.changeLanguage('zh');
    });
    await flush();
  }
});

test('project cards show actual upload times, never filename or a fabricated old date', async () => {
  await mount('/projects');
  let card = document.querySelector('[role=button]');
  assert.ok(card.querySelector('time'));
  assert.equal(card.querySelector('time').getAttribute('datetime'), fixture.uploadedAt);
  assert.equal(card.textContent.includes('fiction.txt'), false);
  const old = project();
  old.uploadedAt = null;
  await mount('/projects', old);
  card = document.querySelector('[role=button]');
  assert.equal(card.querySelector('time'), null);
  assert.ok(card.textContent.includes('上传时间未知'));
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});
test('ratio choices have diagrams and retain the selected draft without a write', async () => {
  await mount('/projects');
  const trigger = document.querySelector('button[aria-label="比例"]');
  await act(() => {
    trigger.focus();
    trigger.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
  });
  await flush();
  assert.equal(document.querySelectorAll('.xy-ratio-preview').length, 4);
  const choice = [...document.querySelectorAll('[role=menuitemradio]')].find((e) =>
    e.textContent.includes('16:9'),
  );
  await act(() => choice.click());
  await flush();
  assert.equal(trigger.textContent.includes('16:9'), true);
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});

test('official single-category services omit filters while gateways keep language, image and video without all', async () => {
  await mount('/model-config', project(), false, true);
  assert.equal(document.querySelector('h1')?.textContent, '模型配置');
  assert.ok(document.querySelector('.mc-card'));
  assert.equal(document.querySelectorAll('[role=tab]').length, 3);
  assert.ok(document.querySelector('.mc-setup-badge').textContent.includes('待配置'));
  assert.equal(document.getElementById('mc-config-deepseek').textContent.trim(), '立即配置');
  assert.ok(document.querySelector('.mc-directory-controls .mc-search'));
  assert.equal(button('刷新'), undefined);
  for (const text of ['工作台设置', '官方适配由系统维护', '已配置', '请先配置 API Key', '1 个连接'])
    assert.equal(document.body.textContent.includes(text), false, text);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(document.querySelectorAll('.mc-editor [role=tab]').length, 0);
  assert.ok(document.querySelector('.mc-official-dialog[role=dialog]'));
  assert.equal(button('保存').disabled, true);
  assert.equal(document.activeElement.id, 'model-api-key');
  assert.equal(document.getElementById('mc-model-id-0').disabled, true);
  assert.ok(document.getElementById('mc-alias-0'));
  assert.equal(document.querySelector('.mc-official-dialog').textContent.includes('固定'), false);
  assert.equal(document.body.textContent.includes('点击图标可自定义Logo'), false);
  assert.equal(document.body.textContent.includes('连接中的模型'), false);
  await act(() => button('取消').click());
  await flush();
  await act(() => document.getElementById('mc-source-gateway').click());
  await flush();
  assert.deepEqual(
    [...document.querySelectorAll('[id^="mc-tab-"]')].map((e) => e.textContent),
    ['语言', '图片', '视频'],
  );
  await act(() => document.querySelector('#mc-tab-image').click());
  await flush();
  assert.ok(document.body.textContent.includes('还没有中转站'));
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});
test('model credentials save explicitly, never return or persist, and toggle updates candidates without generation', async () => {
  await mount('/model-config', project(), false, true);
  const before = copy(fixture);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.ok(document.querySelector('.mc-editor'));
  const field = document.getElementById('model-api-key');
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(
      field,
      'dummy-ui-key',
    );
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(() => document.querySelector('.mc-editor button[role=switch]').click());
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  let writes = requests.filter((r) => r.method === 'POST');
  assert.deepEqual(writes, [
    {
      url: '/api/model-config',
      method: 'POST',
      body: {
        revision: 1,
        connection: {
          id: 'deepseek',
          name: 'DeepSeek',
          kind: 'official',
          protocol: 'deepseek',
          baseUrl: 'https://api.deepseek.com',
          models: defaultRegistry().connections[0].models.map(
            ({ name, available, referenced, reason, ...m }, i) => ({
              ...m,
              description: '',
              logoId: '',
              enabled: i === 0,
            }),
          ),
        },
        apiKey: 'dummy-ui-key',
      },
    },
  ]);
  assert.equal(document.querySelector('.mc-editor'), null);
  assert.equal(modelConfig.connections[0].credentialConfigured, true);
  assert.equal(document.querySelector('.mc-setup-badge'), null);
  assert.equal(document.body.textContent.includes('已配置'), false);
  assert.equal(
    [...Array(localStorage.length)].some((_, i) =>
      localStorage.getItem(localStorage.key(i)).includes('dummy-ui-key'),
    ),
    false,
  );
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(document.getElementById('model-api-key').value, '');
  assert.equal(document.querySelector('.mc-editor').textContent.includes('dummy-ui-key'), false);
  await act(() => button('取消').click());
  await flush();
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  await act(() => document.querySelector('.mc-editor button[role=switch]').click());
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  writes = requests.filter((r) => r.method === 'POST');
  assert.equal(writes[1].body.revision, 2);
  assert.equal(
    writes[1].body.connection.models.every((m) => !m.enabled),
    true,
  );
  assert.equal('apiKey' in writes[1].body, false);
  assert.equal(modelConfig.connections[0].credentialConfigured, true);
  assert.deepEqual(fixture, before);
  assert.equal(
    requests.some((r) => r.url.includes('-runs') || r.url.endsWith('/runs')),
    false,
  );
  await navigateHash('/projects');
  await openModelPanel();
  await act(() => {
    [...document.querySelectorAll('.xy-model-cat')]
      .find((e) => e.textContent.includes('分析模型'))
      .click();
  });
  await flush();
  assert.match(document.querySelector('.xy-model-panel-list').textContent, /暂无可用分析模型/);
  assert.equal(requests.filter((r) => r.method === 'POST').length, 2);
});
test('configuration conflict preserves draft and requires explicit reload, readonly configuration cannot submit', async () => {
  await mount('/model-config', project(), false, true);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  const field = document.getElementById('model-api-key');
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(
      field,
      'dummy-conflict-key',
    );
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  modelConfig.revision++;
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(document.getElementById('model-api-key').value, 'dummy-conflict-key');
  assert.ok(button('读取最新配置'));
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  await mount('/model-config', project(), true, true);
  assert.equal(document.getElementById('mc-config-deepseek').disabled, true);
  assert.equal(document.querySelector('button[role=switch]'), null);
  assert.ok(document.body.textContent.includes('当前服务只读'));
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});

test('home uploads full script and persists ratio, all chosen model IDs and complete fixed style without generation', async () => {
  await mount('/projects', project(), false, false, true);
  for (const [label, choice] of [['比例', '16:9']]) {
    const trigger = document.querySelector('button[aria-label="' + label + '"]');
    await act(() => {
      trigger.focus();
      trigger.dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }),
      );
    });
    await flush();
    const option = [...document.querySelectorAll('[role=menuitemradio]')].find((e) =>
      e.textContent.includes(choice),
    );
    assert.ok(option);
    await act(() => option.click());
    await flush();
  }
  await pickModel('分析模型', 'Fixture Analysis');
  await pickModel('图像模型', 'Fixture Image');
  await pickModel('视频模型', 'Fixture Video');
  await act(() => {
    const trigger = document.querySelector<HTMLButtonElement>('button[aria-label="选择项目风格"]')!;
    trigger.focus();
    trigger.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
  });
  await flush();
  assert.equal(document.querySelectorAll('[role=dialog]').length, 0);
  assert.equal(document.querySelector('.xy-style-option img')?.getAttribute('src'), uploadStyle.previewUrl + '?v=daydream-20261019');
  await act(() => document.querySelector('.xy-style-option').click());
  await flush();
  assert.equal(document.querySelector('.xy-style-dropdown'), null);
  assert.ok(document.querySelector('button[aria-label="选择项目风格"]').textContent.includes(uploadStyle.name));
  await act(() => button('粘贴文本').click());
  await flush();
  const text = '虚构港口。\n小竹拾起怀表。\n完整尾部。';
  const field = document.querySelector('textarea[aria-label="剧本正文"]');
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(field, text);
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(() => button('使用文本').click());
  await flush();
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
  await act(() => button('开始解析').click());
  await flush();
  const writes = requests.filter((r) => r.method !== 'GET');
  assert.equal(writes.length, 2);
  assert.equal(writes[0].url, '/api/upload');
  assert.equal(writes[1].url, '/api/projects');
  assert.equal(uploadedText, text);
  assert.deepEqual(writes[1].body.settings, {
    aspectRatio: '16:9',
    llmModel: 'fixture-analysis',
    imageModel: 'fixture-image',
    videoModel: 'fixture-video',
  });
  assert.equal(writes[1].body.styleId, uploadStyle.id);
  assert.equal(writes[1].body.promptVersion, uploadStyle.promptVersion);
  assert.deepEqual(fixture.style, uploadStyle);
  assert.deepEqual(fixture.runs, []);
  assert.equal(fixture.usedOrReservedMicroCny, 0);
  assert.match(window.location.hash, /project_uploaded\/outline$/);
  const saved = copy(fixture);
  await navigateHash('/projects');
  await navigateHash('/playlet/project_uploaded/outline');
  assert.deepEqual(fixture, saved);
  assert.ok(document.querySelector('.xy-project-selections').textContent.includes('16:9'));
});

test('style dropdown in project parameters keeps selection local until save, cancels cleanly and preserves the fixed snapshot', async () => {
  await mount('/playlet/project_fictional/outline', project(), false, false, true);
  const openStyle = async () => {
    const trigger = document.querySelector<HTMLButtonElement>('button[aria-label="选择项目风格"]')!;
    await act(() => {
      trigger.focus();
      trigger.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    });
    await flush();
    return trigger;
  };
  await act(() => button('选择项目参数').click());
  await flush();
  const trigger = await openStyle();
  assert.equal(document.querySelectorAll('[role=dialog]').length, 1);
  const option = document.querySelector<HTMLElement>('.xy-style-option')!;
  await act(() => {
    option.focus();
    option.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
  });
  await flush();
  assert.equal(document.querySelector('.xy-style-dropdown'), null);
  assert.ok(trigger.textContent.includes(uploadStyle.name));
  assert.equal(fixture.style, null);
  assert.equal(requests.some(r => r.method === 'POST'), false);
  await openStyle();
  assert.equal(document.querySelector('.xy-style-option')!.getAttribute('aria-checked'), 'true');
  await act(() => document.querySelector('.xy-style-dropdown')!.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })));
  await flush();
  assert.equal(document.querySelector('.xy-style-dropdown'), null);
  assert.equal(document.activeElement, trigger);
  assert.equal(document.querySelectorAll('[role=dialog]').length, 1);
  await act(() => button('取消').click());
  await flush();
  assert.equal(fixture.style, null);
  await act(() => button('选择项目参数').click());
  await flush();
  await openStyle();
  assert.equal(document.querySelector('.xy-style-option')!.getAttribute('aria-checked'), 'false');
  await act(() => document.querySelector<HTMLElement>('.xy-style-option')!.click());
  await flush();
  await act(() => button('保存').click());
  await flush();
  const posts = requests.filter(r => r.method === 'POST');
  assert.deepEqual(posts.map(r => r.url), ['/api/projects/project_fictional/settings', '/api/projects/project_fictional/style']);
  assert.deepEqual(posts[1].body, { revision: 5, styleId: uploadStyle.id, promptVersion: uploadStyle.promptVersion });
  assert.deepEqual(fixture.style, uploadStyle);
  await act(() => button('选择项目参数').click());
  await flush();
  assert.equal(document.querySelector<HTMLButtonElement>('button[aria-label="选择项目风格"]')!.disabled, true);
  assert.equal(button('保存').disabled, true);
});

test('missing project settings can be filled without changing confirmed choices or starting a task', async () => {
  const p = project();
  p.settings = { aspectRatio: '9:16', llmModel: '', imageModel: '', videoModel: '' };
  await mount('/playlet/project_fictional/outline', p, false, false, true);
  await act(() => button('选择项目参数').click());
  await flush();
  assert.ok(document.querySelector('.xy-project-choices-dialog'));
  assert.equal(
    document.querySelector<HTMLButtonElement>(
      '.xy-project-choices-dialog button[aria-label="比例"]',
    ).disabled,
    true,
  );
  assert.equal(button('保存').disabled, true);
  await pickModel('分析模型', 'Fixture Analysis', '.xy-project-choices-dialog');
  await flush();
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
  await act(() => button('保存').click());
  await flush();
  assert.deepEqual(
    requests.filter((r) => r.method === 'POST'),
    [
      {
        url: '/api/projects/project_fictional/settings',
        method: 'POST',
        body: {
          revision: 4,
          settings: {
            aspectRatio: '9:16',
            llmModel: 'fixture-analysis',
            imageModel: '',
            videoModel: '',
          },
        },
      },
    ],
  );
  assert.equal(fixture.settings.llmModel, 'fixture-analysis');
  await act(() => button('选择项目参数').click());
  await flush();
  assert.ok(
    document
      .querySelector('.xy-project-choices-dialog button[aria-label="模型"]')!
      .textContent.includes('Fixture Analysis'),
  );
  await act(() => button('取消').click());
  await flush();
  assert.ok(button('下载原剧本'));
  assert.equal(document.querySelector('a[download]'), null);
  await act(() => button('选择项目参数').click());
  await flush();
  await openModelPanel('.xy-project-choices-dialog');
  await act(() => {
    [...document.querySelectorAll('.xy-project-choices-dialog .xy-model-cat')]
      .find((e) => e.textContent.includes('分析模型'))
      .click();
  });
  await flush();
  const lockedRows = [...document.querySelectorAll('.xy-project-choices-dialog .xy-model-row')];
  assert.ok(lockedRows.length);
  assert.equal(
    lockedRows.every((r) => r.getAttribute('data-disabled') === 'true'),
    true,
  );
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
});

test('available models do not silently fill a missing analysis selection', async () => {
  const p = project();
  p.runs = p.runs.slice(0, 1);
  p.visualPrompts = [];
  p.settings.llmModel = '';
  await mount('/playlet/project_fictional/characters', p, false, false, true);
  requestInterceptor = (url) => {
    if (url === '/api/status')
      return Response.json({
        readOnly: false,
        configured: true,
        capabilities: {
          assetEditing: true,
          singleVisualPrompts: true,
          visualPrompts: true,
          projectSettings: true,
        },
      });
  };
  await act(() => currentContext.refreshModels());
  await flush();
  assert.equal(button('完善资产').disabled, true);
  assert.ok(document.querySelector('[aria-label="请先选择项目的分析模型。"]'));
  assert.equal(fixture.settings.llmModel, '');
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
});

test('one supplier credential enables both models and disabling Flash leaves Pro selectable', async () => {
  await mount('/model-config', project(), false, true);
  assert.equal(document.querySelectorAll('.mc-card').length, 1);
  assert.equal(document.querySelectorAll('[data-connection-id]').length, 1);
  assert.equal(document.querySelectorAll('.mc-card .mc-config-action').length, 1);
  assert.equal(document.querySelector('.mc-card button[role=switch]'), null);
  assert.equal(
    document.querySelector('.mc-model-mark img').getAttribute('src'),
    '/demo/assets/providers/deepseek.svg',
  );
  assert.equal(document.querySelector('.mc-card h2').textContent, 'DeepSeek');
  assert.equal(document.querySelector('.mc-card .mc-card'), null);
  assert.equal(document.querySelector('.mc-card').textContent.includes('deepseek-flash'), false);
  assert.equal(document.querySelector('.mc-card .mc-catalog-pricing'), null);
  assert.ok(document.querySelector('.mc-catalog-description').textContent.includes('理解完整剧本'));
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(document.querySelectorAll('#model-api-key').length, 1);
  assert.equal(document.querySelectorAll('.mc-official-model').length, 2);
  assert.equal(document.querySelector('.mc-readonly-parameters'), null);
  await act(() => document.getElementById('mc-parameters-toggle-0').click());
  await flush();
  assert.deepEqual(
    [
      ...document
        .querySelector('.mc-readonly-parameters .mc-catalog-pricing')
        .querySelectorAll('dd'),
    ].map((e) => e.textContent),
    ['¥2', '¥8', '¥0.04'],
  );
  assert.equal(document.querySelector('.mc-readonly-parameters input'), null);
  await act(() => button('取消').click());
  await flush();
  assert.equal(document.activeElement.id, 'mc-config-deepseek');
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
  const search = document.querySelector<HTMLInputElement>('input[aria-label="搜索连接或模型"]');
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(search, 'Pro');
    search.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
  assert.equal(document.querySelectorAll('.mc-card').length, 1);
  assert.equal(document.querySelector('.mc-card h2').textContent, 'DeepSeek');
  assert.equal(document.querySelector('.mc-card').textContent.includes('DeepSeek Pro'), false);
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(search, '');
    search.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
  assert.equal(document.querySelectorAll('.mc-card').length, 1);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  const field = document.getElementById('model-api-key');
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(
      field,
      'dummy-shared-ui-key',
    );
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  for (const toggle of document.querySelectorAll('.mc-editor button[role=switch]'))
    await act(() => toggle.click());
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(modelView().connections[0].models.filter((m) => m.available).length, 2);
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(document.querySelectorAll('#model-api-key').length, 1);
  await act(() => document.getElementById('mc-enable-0').click());
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const writes = requests.filter((r) => r.method === 'POST');
  assert.deepEqual(
    writes[1].body.connection.models.map(({ id, enabled }) => ({ id, enabled })),
    [
      { id: 'deepseek-flash', enabled: false },
      { id: 'deepseek-v4-pro', enabled: true },
    ],
  );
  assert.equal('apiKey' in writes[1].body, false);
  await navigateHash('/projects');
  await openModelPanel();
  await act(() => {
    [...document.querySelectorAll('.xy-model-cat')]
      .find((e) => e.textContent.includes('分析模型'))
      .click();
  });
  await flush();
  assert.equal(
    document.querySelector('.xy-model-panel-list').textContent.includes('DeepSeek Pro'),
    true,
  );
  assert.equal(
    document.querySelector('.xy-model-panel-list').textContent.includes('DeepSeek Flash'),
    false,
  );
  assert.equal(
    requests.some((r) => r.url.includes('-runs') || r.url.endsWith('/runs')),
    false,
  );
});

const setField = async (id, value) => {
  const el = document.getElementById(id);
  assert.ok(el, id);
  await act(() => {
    Object.getOwnPropertyDescriptor(
      el instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    ).set.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
};
test('a large model catalog changes only the supplier count and stays inside configuration', async () => {
  await mount('/model-config', project(), false, true);
  const summary = document
      .querySelector('.mc-card')
      .textContent.replace('0/2 已启用', '0/N 已启用'),
    base = copy(modelConfig.connections[0].models[0]);
  modelConfig.connections[0].models = Array.from({ length: 40 }, (_, i) => ({
    ...copy(base),
    id: 'fixture-model-' + i,
    modelId: 'fixture-model-' + i,
    name: 'Fixture Model ' + i,
  }));
  await navigateHash('/projects');
  await navigateHash('/model-config');
  assert.equal(document.querySelectorAll('.mc-card').length, 1);
  assert.equal(
    document.querySelector('.mc-card').textContent.replace('0/40 已启用', '0/N 已启用'),
    summary,
  );
  assert.equal(document.querySelectorAll('.mc-card .mc-config-action').length, 1);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(document.querySelectorAll('#model-api-key').length, 1);
  assert.equal(document.querySelectorAll('.mc-official-model').length, 40);
  assert.equal(document.querySelector('.mc-readonly-parameters'), null);
  await act(() => button('取消').click());
  await flush();
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});
test('official edits show draft feedback, focus a missing key inline, and cancel without writes', async () => {
  await mount('/model-config', project(), false, true);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(document.getElementById('model-api-key').getAttribute('autocomplete'), 'off');
  await setField('mc-alias-0', '编剧助手');
  assert.equal(button('保存').disabled, false);
  assert.ok(document.querySelector('.mc-official-model strong').textContent.includes('编剧助手'));
  assert.ok(
    document.querySelector('.mc-official-save-state').textContent.includes('有未保存的修改'),
  );
  await act(() => document.getElementById('mc-enable-0').click());
  await act(() => button('保存').click());
  await flush();
  const key = document.getElementById('model-api-key');
  assert.equal(key.getAttribute('aria-invalid'), 'true');
  assert.equal(document.activeElement, key);
  assert.ok(document.getElementById('mc-official-key-error'));
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
  await act(() => button('取消').click());
  await flush();
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(document.getElementById('mc-alias-0').value, '');
  assert.equal(document.getElementById('mc-enable-0').getAttribute('aria-checked'), 'false');
  assert.equal(button('保存').disabled, true);
});
const selectCategory = async (index, label) => {
  const trigger = document.querySelector('button[aria-label="模型类别 ' + index + '"]');
  await act(() => {
    trigger.focus();
    trigger.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
  });
  await flush();
  const option = [...document.querySelectorAll('[role=menuitemradio]')].find(
    (e) => e.textContent === label,
  );
  assert.ok(option, label);
  await act(() => option.click());
  await flush();
};
test('custom gateway shares one key, registers mixed categories, and displays aliases with source-aware filtering', async () => {
  await mount('/model-config', project(), false, true);
  await act(() => button('新增中转站').click());
  await flush();
  assert.ok(document.querySelector('.mc-gateway-dialog'));
  assert.equal(document.querySelector('.mc-editor-shell'), null);
  assert.equal(document.querySelectorAll('#model-base-url').length, 1);
  assert.equal(document.querySelectorAll('#model-api-key').length, 1);
  await setField('mc-connection-name', '虚构中转站');
  await setField('model-base-url', 'https://gateway.example.test/v1');
  await setField('model-api-key', 'dummy-gateway-key');
  await act(() => button('手动添加').click());
  await flush();
  await setField('mc-model-id-0', 'custom-text');
  await setField('mc-alias-0', '编剧助手');
  await flush();
  for (const [id, value] of [
    ['mc-contextWindow-0', '8192'],
    ['mc-maxOutputTokens-0', '1024'],
    ['mc-price-inputCnyPerMillion-0', '0.5'],
    ['mc-price-outputCnyPerMillion-0', '1.5'],
    ['mc-price-cacheHitHundredthsCnyPerMillion-0', '0.05'],
  ])
    await setField(id, value);
  await act(() => document.getElementById('mc-enable-0').click());
  await act(() => button('手动添加').click());
  await flush();
  await setField('mc-model-id-1', 'custom-image');
  await setField('mc-alias-1', '画面助手');
  await selectCategory(2, '图片');
  await act(() => document.getElementById('mc-enable-1').click());
  await act(() => button('手动添加').click());
  await flush();
  await setField('mc-model-id-2', 'custom-video');
  await setField('mc-alias-2', '视频助手');
  await selectCategory(3, '视频');
  await act(() => document.getElementById('mc-enable-2').click());
  assert.equal(document.querySelectorAll('#model-api-key').length, 1);
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const writes = requests.filter((r) => r.method === 'POST');
  assert.equal(writes.length, 1);
  assert.equal(writes[0].body.apiKey, 'dummy-gateway-key');
  assert.deepEqual(
    writes[0].body.connection.models.map((m) => m.category),
    ['analysis', 'image', 'video'],
  );
  for (const m of writes[0].body.connection.models) {
    assert.ok('resolutionEnabled' in m, '后端严格校验要求每个模型显式携带 resolutionEnabled 键');
  }
  assert.deepEqual(
    writes[0].body.connection.models.map((m) => m.resolutionEnabled),
    [null, null, null],
  );
  assert.equal(writes[0].body.connection.models[0].pricing.inputCnyPerMillion, 0.5);
  assert.equal(writes[0].body.connection.models[1].pricing, null);
  assert.equal(document.querySelectorAll('.mc-card').length, 1);
  assert.equal(modelConfig.connections[1].models[0].alias, '编剧助手');
  await act(() => document.getElementById('mc-source-official').click());
  await flush();
  assert.equal(document.querySelectorAll('.mc-card').length, 1);
  assert.ok(document.querySelector('.mc-card').textContent.includes('DeepSeek'));
  await act(() => document.getElementById('mc-source-gateway').click());
  await flush();
  for (const kind of ['image', 'video']) {
    await act(() => document.getElementById('mc-tab-' + kind).click());
    await flush();
    assert.equal(document.querySelectorAll('.mc-card').length, 1);
    assert.equal(document.querySelector('.mc-card h2').textContent.includes('虚构中转站'), true);
  }
  await navigateHash('/projects');
  await openModelPanel();
  await act(() => {
    [...document.querySelectorAll('.xy-model-cat')]
      .find((e) => e.textContent.includes('分析模型'))
      .click();
  });
  await flush();
  assert.ok(document.querySelector('.xy-model-panel-list').textContent.includes('编剧助手'));
  await act(() => {
    [...document.querySelectorAll('.xy-model-cat')]
      .find((e) => e.textContent.includes('图像模型'))
      .click();
  });
  await flush();
  assert.match(document.querySelector('.xy-model-panel-list').textContent, /暂无可用图像模型/);
  assert.equal(
    requests.some((r) => r.url.includes('-runs')),
    false,
  );
});
test('renamed disabled model keeps its alias in fixed project settings without rewriting the selected ID', async () => {
  const p = project();
  p.settings.llmModel = 'deepseek-flash';
  await mount('/model-config', p, false, true);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  await setField('mc-alias-0', '编剧主模型');
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  await navigateHash('/playlet/project_fictional/characters');
  assert.ok(
    document.querySelector('.xy-project-selections').textContent.includes('编剧主模型（不可用）'),
  );
  assert.equal(fixture.settings.llmModel, 'deepseek-flash');
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
});

test('official model IDs stay fixed while logo overrides and reset update display only', async () => {
  await mount('/model-config', project(), false, true);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(document.getElementById('mc-model-id-0').disabled, true);
  assert.equal(document.getElementById('mc-model-id-1').value, 'deepseek-v4-pro');
  assert.equal(document.getElementById('model-base-url').disabled, true);
  assert.equal(button('手动添加'), undefined);
  const input = document.getElementById('mc-logo-file-0');
  Object.defineProperty(input, 'files', {
    value: [new window.File(['fixture'], 'logo.png', { type: 'image/png' })],
    configurable: true,
  });
  await act(() => input.dispatchEvent(new Event('change', { bubbles: true })));
  await flush();
  assert.equal(
    document.querySelector('.mc-logo-trigger img').getAttribute('src'),
    '/api/model-logos/' + 'a'.repeat(64) + '?audience=workspace',
  );
  assert.equal(modelConfig.revision, 1);
  await setField('mc-alias-0', '编剧别名');
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(modelConfig.connections[0].models[0].modelId, 'deepseek-flash');
  assert.equal(modelConfig.connections[0].models[0].logoId, 'a'.repeat(64));
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(
    document.querySelector('.mc-logo-trigger img').getAttribute('src'),
    '/api/model-logos/' + 'a'.repeat(64) + '?audience=workspace',
  );
  const logoTrigger = document.querySelector('button[aria-label="修改模型 Logo 1"]');
  await act(() => {
    logoTrigger.focus();
    logoTrigger.dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }),
    );
  });
  await flush();
  await act(() =>
    [...document.querySelectorAll('[role=menuitem]')]
      .find((e) => e.textContent === '恢复默认')
      .click(),
  );
  await flush();
  assert.equal(
    document.querySelector('.mc-logo-trigger img').getAttribute('src'),
    '/demo/assets/providers/deepseek.svg',
  );
  assert.equal(document.getElementById('mc-alias-0').value, '编剧别名');
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(modelConfig.connections[0].models[0].logoId, '');
  assert.equal(
    requests.some((r) => r.url.endsWith('/runs')),
    false,
  );
});
test('gateway discovery requires explicit import, preserves existing models and leaves imported models disabled', async () => {
  await mount('/model-config', project(), false, true);
  await act(() => button('新增中转站').click());
  await flush();
  await setField('mc-connection-name', '虚构目录站');
  await setField('model-base-url', 'https://relay.example.test/v1');
  await setField('model-api-key', 'dummy-directory-key');
  await act(() => button('手动添加').click());
  await flush();
  await setField('mc-model-id-0', 'manual-text');
  await setField('mc-alias-0', '保留别名');
  await act(() => button('获取模型列表').click());
  await flush();
  assert.equal(modelConfig.connections.length, 1);
  assert.equal(document.querySelectorAll('.mc-edit-model').length, 1);
  assert.equal(
    requests.filter((r) => r.url === '/api/model-config' && r.method === 'POST').length,
    0,
  );
  await act(() => document.querySelector('.mc-discovered-row input').click());
  await act(() => button('导入所选模型').click());
  await flush();
  assert.equal(document.querySelectorAll('.mc-edit-model').length, 2);
  assert.equal(document.getElementById('mc-alias-0').value, '保留别名');
  assert.equal(document.getElementById('mc-model-id-1').value, 'discovered-text');
  assert.equal(document.getElementById('mc-enable-1').getAttribute('aria-checked'), 'false');
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(modelConfig.connections.length, 2);
  assert.equal(modelConfig.connections[1].models[0].alias, '保留别名');
  assert.equal(modelConfig.connections[1].models[1].enabled, false);
});

test('Volcengine stays one summary card and shares one credential across three fixed model categories', async () => {
  await mount('/model-config', project(), false, true);
  const models = [
    ['doubao-seed-evolving', 'Doubao Seed Evolving', 'analysis'],
    ['doubao-seed-2-1-pro-260915', 'Doubao Seed 2.1 Pro', 'analysis'],
    ['doubao-seed-2-1-turbo-260628', 'Doubao Seed 2.1 Turbo', 'analysis'],
    ['doubao-seed-2-1-lite-260915', 'Doubao Seed 2.1 Lite', 'analysis'],
    ['doubao-seedream-5-0-pro-260628', 'Doubao Seedream 5.0 Pro', 'image'],
    ['doubao-seedream-5-0-flash-260915', 'Doubao Seedream 5.0 Flash', 'image'],
    ['doubao-seedance-2-5-260628', 'Doubao Seedance 2.5', 'video'],
    ['doubao-seedance-2-0-260128', 'Doubao Seedance 2.0', 'video'],
    ['doubao-seedance-2-0-fast-260128', 'Doubao Seedance 2.0 Fast', 'video'],
    ['doubao-seedance-2-0-mini-260615', 'Doubao Seedance 2.0 Mini', 'video'],
    ['doubao-seedream-5-0-260128', 'Doubao Seedream 5.0 Lite', 'image'],
  ].map(([id, name, category]) => ({
    id,
    modelId: id,
    name,
    defaultName: name,
    alias: '',
    logoId: '',
    category,
    enabled: false,
    available: false,
    referenced: false,
    reason: '',
    contextWindow: category === 'analysis' ? 1024000 : 0,
    maxOutputTokens: category === 'analysis' ? 256000 : 0,
    pricing:
      category === 'analysis'
        ? { inputCnyPerMillion: 6, outputCnyPerMillion: 30, cacheHitHundredthsCnyPerMillion: 120 }
        : null,
    defaultLogoUrl: '/demo/assets/providers/volcengine.svg',
  }));
  modelConfig.connections.push({
    id: 'volcengine',
    name: '火山引擎',
    kind: 'official',
    protocol: 'volcengine-ark',
    baseUrl: 'https://ark.cn-beijing.volces.com/api/v3',
    credentialConfigured: true,
    editable: true,
    reason: '',
    models,
  });
  await navigateHash('/projects');
  await navigateHash('/model-config');
  const card = document.querySelector('[data-connection-id="volcengine"]');
  assert.ok(card);
  assert.equal(card.querySelector('h2').textContent, '火山引擎');
  assert.ok(card.textContent.includes('0/11 已启用'));
  assert.equal(card.textContent.includes('doubao-seed-evolving'), false);
  assert.equal(card.querySelectorAll('.mc-config-action').length, 1);
  await act(() => document.getElementById('mc-config-volcengine').click());
  await flush();
  assert.ok(document.querySelector('.mc-official-dialog'));
  assert.equal(document.querySelectorAll('#model-api-key').length, 1);
  assert.equal(document.querySelectorAll('#model-base-url').length, 1);
  assert.equal(document.querySelectorAll('.mc-official-model').length, 4);
  assert.equal(document.getElementById('model-base-url').disabled, true);
  assert.equal(document.querySelector('[role=tab][id=mc-edit-tab-all]'), null);
  await setField('mc-alias-0', '豆包编剧');
  assert.equal(document.getElementById('mc-alias-0').placeholder, 'Doubao Seed Evolving');
  await act(() => document.getElementById('mc-enable-0').click());
  await act(() => document.getElementById('mc-edit-tab-image').click());
  await flush();
  assert.equal(document.querySelectorAll('.mc-official-model').length, 3);
  assert.equal(document.getElementById('mc-model-id-10').value, 'doubao-seedream-5-0-260128');
  assert.equal(document.getElementById('mc-enable-10').getAttribute('aria-checked'), 'false');
  assert.equal(document.getElementById('mc-model-id-4').value, 'doubao-seedream-5-0-pro-260628');
  assert.equal(document.getElementById('mc-model-id-4').disabled, true);
  await act(() => document.getElementById('mc-enable-4').click());
  await act(() => document.getElementById('mc-edit-tab-video').click());
  await flush();
  assert.equal(document.getElementById('mc-model-id-6').value, 'doubao-seedance-2-5-260628');
  assert.equal(document.getElementById('mc-model-id-6').disabled, true);
  assert.equal(document.getElementById('mc-model-id-8').value, 'doubao-seedance-2-0-fast-260128');
  assert.equal(document.getElementById('mc-model-id-9').value, 'doubao-seedance-2-0-mini-260615');
  assert.equal(document.querySelectorAll('.mc-official-model').length, 4);
  await act(() => document.getElementById('mc-enable-6').click());
  await act(() => document.getElementById('mc-edit-tab-analysis').click());
  await flush();
  assert.equal(document.getElementById('mc-alias-0').value, '豆包编剧');
  assert.equal(document.getElementById('mc-enable-0').getAttribute('aria-checked'), 'true');
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const writes = requests.filter((r) => r.method === 'POST');
  assert.equal(writes.length, 1);
  assert.equal('apiKey' in writes[0].body, false);
  assert.equal(writes[0].body.connection.models.length, 11);
  assert.equal(writes[0].body.connection.models.filter((m) => m.enabled).length, 3);
  assert.equal(writes[0].body.connection.models[0].alias, '豆包编剧');
  assert.equal(writes[0].body.connection.models[4].pricing, null);
  assert.equal(writes[0].body.connection.protocol, 'volcengine-ark');
});

test('Kling and Alibaba remain supplier cards with fixed IDs and one shared credential for image/video preferences', async () => {
  await mount('/model-config', project(), false, true);
  const provider = (id, name, protocol, baseUrl, items) => ({
    id,
    name,
    kind: 'official',
    protocol,
    baseUrl,
    credentialConfigured: false,
    editable: true,
    reason: '',
    models: items.map(([modelId, defaultName, category]) => ({
      id: modelId,
      modelId,
      defaultName,
      name: defaultName,
      category,
      alias: '',
      logoId: '',
      enabled: false,
      available: false,
      referenced: false,
      reason: '',
      contextWindow: 0,
      maxOutputTokens: 0,
      pricing: null,
      defaultLogoUrl: '/demo/assets/providers/' + id + '.svg',
    })),
  });
  modelConfig.connections.push(
    provider('kling', '可灵 AI', 'kling-media', 'https://api-singapore.klingai.com', [
      ['kling-v3', 'Kling Image 3.0', 'image'],
      ['kling-v3-omni', 'Kling Image 3.0 Omni', 'image'],
      ['kling-3.0', 'Kling Video 3.0', 'video'],
      ['kling-3.0-omni', 'Kling Video 3.0 Omni', 'video'],
      ['kling-3.0-turbo', 'Kling Video 3.0 Turbo', 'video'],
    ]),
    provider('alibaba', '阿里云百炼', 'dashscope-media', 'https://dashscope.aliyuncs.com/api/v1', [
      ['wan2.7-image', 'Wan Image 2.7', 'image'],
      ['wan2.7-image-pro', 'Wan Image 2.7 Pro', 'image'],
      ['happyhorse-1.1-t2v', 'HappyHorse 1.1 文生视频', 'video'],
      ['happyhorse-1.1-i2v', 'HappyHorse 1.1 图生视频', 'video'],
      ['happyhorse-1.1-r2v', 'HappyHorse 1.1 参考生视频', 'video'],
      ['happyhorse-1.0-video-edit', 'HappyHorse 1.0 视频编辑', 'video'],
    ]),
  );
  await navigateHash('/projects');
  await navigateHash('/model-config');
  await act(() => document.getElementById('mc-tab-image').click());
  await flush();
  assert.equal(document.querySelectorAll('.mc-supplier-card').length, 2);
  assert.equal(
    document.querySelector('[data-connection-id=kling]').textContent.includes('kling-v3'),
    false,
  );
  assert.ok(
    document.querySelector('[data-connection-id=alibaba]').textContent.includes('0/6 已启用'),
  );
  for (const [id, count, videoId, videoCount] of [
    ['kling', 5, 'kling-3.0', 3],
    ['alibaba', 6, 'happyhorse-1.1-t2v', 4],
  ]) {
    await act(() => document.getElementById('mc-config-' + id).click());
    await flush();
    assert.equal(document.querySelectorAll('#model-api-key').length, 1);
    assert.equal(document.querySelectorAll('#model-base-url').length, 1);
    assert.equal(document.getElementById('model-base-url').disabled, true);
    assert.equal(document.getElementById('mc-edit-tab-analysis'), null);
    assert.equal(document.querySelectorAll('.mc-official-model').length, 2);
    assert.equal(document.getElementById('mc-model-id-0').disabled, true);
    assert.equal(
      document.querySelector('.mc-official-dialog .mc-model-mark img').getAttribute('src'),
      '/demo/assets/providers/' + id + '.svg',
    );
    await setField('model-api-key', 'fictional-key-' + id);
    await setField('mc-alias-0', '画面助手');
    await act(() => document.getElementById('mc-enable-0').click());
    await act(() => document.getElementById('mc-edit-tab-video').click());
    await flush();
    assert.equal(document.querySelectorAll('.mc-official-model').length, videoCount);
    assert.equal(document.getElementById('mc-model-id-2').value, videoId);
    assert.equal(document.getElementById('mc-model-id-2').disabled, true);
    await setField('mc-alias-2', '视频助手');
    await act(() => document.getElementById('mc-enable-2').click());
    await act(() =>
      document
        .querySelector('form')
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    );
    await flush();
    const saved = requests.filter((r) => r.method === 'POST').at(-1).body;
    assert.equal(saved.apiKey, 'fictional-key-' + id);
    assert.equal(saved.connection.models.length, count);
    assert.equal(saved.connection.models.filter((m) => m.enabled).length, 2);
    assert.ok(
      saved.connection.models.every(
        (m) => m.pricing === null && m.contextWindow === 0 && m.maxOutputTokens === 0,
      ),
    );
    assert.equal(saved.connection.models[0].alias, '画面助手');
    assert.equal(saved.connection.models[2].alias, '视频助手');
    assert.equal(document.querySelector('.mc-official-dialog'), null);
  }
  assert.equal(requests.filter((r) => r.method === 'POST').length, 2);
  assert.equal(
    requests.some(
      (r) => r.url.includes('-runs') || r.url.endsWith('/runs') || r.url.includes('/discover'),
    ),
    false,
  );
  assert.equal(
    [...Array(localStorage.length)].some((_, i) =>
      (localStorage.getItem(localStorage.key(i)) || '').includes('fictional-key-'),
    ),
    false,
  );
});

// P4 interface fixtures contain saved API views; they never execute a model or Runner.
function replyToOrdinarySave(url: string, options: RequestInit) {
  if (options.method !== 'POST' || !url.includes('/assets/')) return;
  const body = JSON.parse(options.body as string),
    parts = url.split('/'),
    asset = fixture.assets.find((a) => a.id === parts[5]);
  assert.ok(asset);
  assert.equal(body.revision, fixture.revision);
  if (parts[6] === 'looks') {
    const look = asset.looks.find((l) => l.id === parts[7]);
    assert.ok(look);
    if (parts[8] === 'delete') asset.looks = asset.looks.filter((l) => l.id !== look.id);
    else Object.assign(look, body.look, { origin: 'user' });
  } else if (parts[6] === 'delete')
    fixture.assets = fixture.assets.filter((a) => a.id !== asset.id);
  else Object.assign(asset, body.asset, { origin: 'user' });
  fixture.catalogEditedByUser = true;
  fixture.revision++;
  return Response.json(copy(fixture));
}
test('P4 API capability flags protect asset and look writes without replacing task entitlement', async () => {
  await mount('/playlet/project_fictional/characters', project(), false);
  capabilityOverrides = { assetEditing: false, characterLooks: false };
  await act(() => currentContext.refreshModels());
  await flush();
  assert.equal(button('新增角色').disabled, true);
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构小竹"]').click());
  await flush();
  assert.equal(button('保存').disabled, true);
  assert.equal(button('新增附加形象').disabled, true);
  capabilityOverrides = { assetEditing: true, characterLooks: false };
  await act(() => currentContext.refreshModels());
  await flush();
  assert.equal(button('保存').disabled, false);
  assert.equal(button('新增附加形象').disabled, true);
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});
test('P4 API character edit saves description, carries character data unchanged and keeps empty look description', async () => {
  const p = project();
  p.assets[0].looks = [{ id: 'empty-look', name: '虚构空描述', description: '', origin: 'user' }];
  await mount('/playlet/project_fictional/characters', p, false);
  requestInterceptor = replyToOrdinarySave;
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构小竹"]').click());
  await flush();
  await setField('asset-description', '更新后的完整角色描述');
  await act(() => button('保存').click());
  await flush();
  assert.deepEqual(
    requests.find((r) => r.method === 'POST'),
    {
      url: '/api/projects/project_fictional/assets/character_one',
      method: 'POST',
      body: {
        revision: 4,
        asset: {
          kind: 'character',
          name: p.assets[0].name,
          description: '更新后的完整角色描述',
          aliases: [],
          character: p.assets[0].character,
        },
      },
    },
  );
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构小竹"]').click());
  await flush();
  await act(() => document.querySelector<HTMLButtonElement>('.asset-look-actions button').click());
  await flush();
  assert.equal(document.getElementById('asset-form-description').value, '');
  await setField('asset-form-name', '虚构新造型名');
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.deepEqual(requests.filter((r) => r.method === 'POST').at(-1), {
    url: '/api/projects/project_fictional/assets/character_one/looks/empty-look',
    method: 'POST',
    body: { revision: 5, look: { name: '虚构新造型名', description: '' } },
  });
});
test('P4 API conflict preserves draft and requires readback before a new revision', async () => {
  await mount('/playlet/project_fictional/scenes', project(), false);
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构车站"]').click());
  await flush();
  await setField('asset-description', '我未提交的完整草稿');
  fixture.revision = 5;
  fixture.assets[1].description = '另一窗口保存的说明';
  await act(() => currentContext.refreshCurrentProject(fixture.id));
  await flush();
  assert.equal(document.getElementById('asset-description').value, '我未提交的完整草稿');
  await act(() => button('保存').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST')[0].body.revision, 4);
  assert.equal(button('保存').disabled, true);
  await act(() => button('保存').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  await act(() => button('读取最新版本').click());
  await flush();
  assert.equal(document.getElementById('asset-description').value, '我未提交的完整草稿');
  requestInterceptor = replyToOrdinarySave;
  await act(() => button('保存').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').at(-1).body.revision, 5);
  assert.equal(fixture.assets[1].description, '我未提交的完整草稿');
});
test('P4 API read failures retain forms and older reads cannot overwrite committed responses', async () => {
  await mount('/playlet/project_fictional/scenes', project(), false);
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构车站"]').click());
  await flush();
  await setField('asset-name', '草稿名字');
  requestInterceptor = (url) => {
    if (url === '/api/projects/' + fixture.id) throw new TypeError('offline');
  };
  await act(async () => {
    await assert.rejects(() => currentContext.refreshCurrentProject(fixture.id));
  });
  await flush();
  assert.equal(document.getElementById('asset-name').value, '草稿名字');
  requestInterceptor = null;
  let release;
  const old = copy(fixture);
  pending.set(
    '/api/projects/' + fixture.id,
    new Promise((resolve) => (release = () => resolve(Response.json(old)))),
  );
  let reading;
  await act(() => {
    reading = currentContext.refreshCurrentProject(fixture.id);
  });
  const saved = copy(fixture);
  saved.revision = 8;
  saved.assets[1].name = '最新保存名字';
  await act(() => currentContext.acceptProject(saved));
  await act(async () => {
    release();
    await reading;
  });
  await flush();
  assert.equal(currentContext.currentProject.revision, 8);
  assert.equal(currentContext.currentProject.assets[1].name, '最新保存名字');
  assert.equal(document.getElementById('asset-name').value, '草稿名字');
});
test('P4 API task and cancel buttons send only revision and consume returned states', async () => {
  const p = project();
  p.runs = [];
  p.assets = [];
  p.visualPrompts = [];
  await mount('/playlet/project_fictional/outline', p, false);
  requestInterceptor = (url, options) => {
    if (url === '/api/status')
      return Response.json({
        readOnly: false,
        configured: true,
        capabilities: {
          assetEditing: true,
          assetImages: true,
          characterLooks: true,
          visualPrompts: true,
        },
      });
    if (options.method === 'POST' && url.endsWith('/runs')) {
      fixture.runs = [
        {
          id: 'interface-p3',
          contract: 'catalog-agent-v3',
          sourceVersion: 1,
          status: 'running',
          stage: 'extract',
          callCount: 0,
        },
      ];
      fixture.revision++;
      return Response.json(copy(fixture));
    }
    if (options.method === 'POST' && url.endsWith('/cancel')) {
      fixture.runs[0].status = 'cancelled';
      fixture.revision++;
      return Response.json(copy(fixture));
    }
  };
  await act(() => currentContext.refreshModels());
  await act(() => button('下一步').click());
  await flush();
  await act(() => button('查看解析进度').click());
  await flush();
  assert.ok(button('取消解析'));
  assert.equal(
    document.querySelector('.script-parse-dock-status')!.getAttribute('data-state'),
    'running',
  );
  assert.equal(button('下一步').disabled, true);
  await act(() => button('取消解析').click());
  await flush();
  assert.match(document.body.textContent, /解析已取消/);
  assert.equal(button('生成资产'), undefined);
  assert.deepEqual(
    requests.filter((r) => r.method === 'POST'),
    [
      { url: '/api/projects/project_fictional/runs', method: 'POST', body: { revision: 4 } },
      { url: '/api/projects/project_fictional/cancel', method: 'POST', body: { revision: 5 } },
    ],
  );
});
test('P4 API duplicate visual start reads existing failure without a second start', async () => {
  const p = project();
  p.runs = p.runs.slice(0, 1);
  p.visualPrompts = [];
  await mount('/playlet/project_fictional/characters', p, false);
  requestInterceptor = (url, options) => {
    if (url === '/api/status')
      return Response.json({
        readOnly: false,
        configured: true,
        capabilities: { assetEditing: true, visualPrompts: true },
      });
    if (options.method === 'POST' && url.endsWith('/visual-prompt-runs')) {
      fixture.runs.push({
        id: 'already-saved',
        contract: 'visual-prompt-agent-v1',
        sourceVersion: 1,
        status: 'failed',
        stage: 'visualPrompt',
        error: '接口夹具既存失败原因',
        callCount: 0,
        targets: [],
      });
      fixture.revision++;
      return Response.json(
        { code: 'VISUAL_PROMPTS_ALREADY_STARTED', message: '已有视觉任务' },
        { status: 409 },
      );
    }
  };
  await act(() => currentContext.refreshModels());
  await act(() => button('完善资产').click());
  await flush();
  assert.match(document.body.textContent, /项目失败.*接口夹具既存失败原因/);
  assert.equal(button('完善资产'), undefined);
  assert.equal(button('新增角色').disabled, true);
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
});
test('P4 API unknown project creation outcome preserves draft and prevents duplicate submission', async () => {
  await mount('/projects', project(), false, false, true);
  await chooseAnalysisForUpload();
  await act(() => button('粘贴文本').click());
  await flush();
  const field = document.querySelector('textarea[aria-label="剧本正文"]');
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(
      field,
      '完整虚构接口剧本\n不能自动调用模型',
    );
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(() => button('使用文本').click());
  await flush();
  requestInterceptor = (url, options) => {
    if (options.method === 'POST' && url === '/api/projects') throw new TypeError('response lost');
  };
  await act(() => button('开始解析').click());
  await flush();
  assert.equal(button('开始解析').disabled, true);
  assert.match(document.body.textContent, /项目保存结果未确认/);
  assert.equal(requests.filter((r) => r.method === 'POST').length, 2);
  await act(() => button('开始解析').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 2);
});
test('P4 API ambiguous creation reads the catalog but cannot duplicate an item by resubmitting', async () => {
  await mount('/playlet/project_fictional/props', project(), false);
  await act(() => button('新增道具').click());
  await flush();
  await setField('asset-form-name', '已保存但响应丢失的道具');
  requestInterceptor = (url, options) => {
    if (options.method === 'POST' && url.endsWith('/assets')) {
      fixture.assets.push({
        ...JSON.parse(options.body as string).asset,
        id: 'saved-without-response',
        origin: 'user',
        sourceIds: [],
        looks: [],
      });
      fixture.catalogEditedByUser = true;
      fixture.revision++;
      throw new TypeError('response lost');
    }
  };
  await act(() => button('保存').click());
  await flush();
  assert.equal(button('保存').disabled, true);
  await act(() => button('读取最新版本').click());
  await flush();
  assert.equal(button('保存').disabled, true);
  assert.equal(document.getElementById('asset-form-name').value, '已保存但响应丢失的道具');
  assert.match(document.querySelector('[role=dialog]').textContent, /不再重复新增/);
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  assert.equal(fixture.assets.filter((a) => a.id === 'saved-without-response').length, 1);
});
test('P4 API disabled generation explains unavailable models or a missing visual capability', async () => {
  const p = project();
  p.runs = p.runs.slice(0, 1);
  p.visualPrompts = [];
  await mount('/playlet/project_fictional/characters', p, false);
  assert.equal(button('完善资产').disabled, true);
  assert.ok(document.querySelector('[aria-label="暂无可用分析模型，请在模型配置中启用。"]'));
  fixture.assets = [];
  capabilityOverrides = { visualPrompts: false };
  await act(() => currentContext.refreshModels());
  await act(() => currentContext.refreshCurrentProject(fixture.id));
  await flush();
  assert.equal(button('完善资产').disabled, true);
  assert.ok(document.querySelector('[aria-label="资产完善接口尚未接入。"]'));
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});
test('project management selects rather than opens, confirms deletion and refreshes real API rows', async () => {
  await mount('/projects', project(), false);
  let rows = [
    {
      id: 'project_fictional',
      revision: 4,
      title: '虚构测试剧本',
      filename: 'fixture.txt',
      uploadedAt: null,
    },
    {
      id: 'project_other',
      revision: 9,
      title: '另一虚构项目',
      filename: 'other.txt',
      uploadedAt: null,
    },
  ];
  requestInterceptor = (url, options) => {
    if (url === '/api/projects') return Response.json(copy(rows.map(row => ({ ...row, spaceId: 'workspace_fixture', ownerId: 'workspace_fixture', creatorId: 'workspace_fixture', creatorName: '虚构编辑者', sharedToMe: false, effectiveAccess: 2 }))));
    if (options.method === 'POST' && url.endsWith('/delete')) {
      const id = url.split('/')[3],
        body = JSON.parse(options.body as string),
        row = rows.find((p) => p.id === id);
      assert.equal(body.revision, row.revision);
      rows = rows.filter((p) => p.id !== id);
      return Response.json({ id, revision: body.revision + 1, deletedAt: '2026-10-14T01:00:00Z' });
    }
  };
  await act(() => currentContext.refreshProjects());
  await flush();
  await act(() => button('管理').click());
  await flush();
  const hash = window.location.hash,
    card = document.querySelector<HTMLElement>('[aria-label="选择项目：虚构测试剧本"]');
  assert.equal(card.getAttribute('role'), 'checkbox');
  await act(() => card.click());
  await flush();
  assert.equal(card.getAttribute('aria-checked'), 'true');
  assert.equal(window.location.hash, hash);
  assert.match(document.querySelector('.project-manage-dock').textContent, /已选 1 项/);
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
  await act(() => button('删除所选').click());
  await flush();
  assert.match(
    document.querySelector('.project-delete-dialog[role=alertdialog]').textContent,
    /虚构测试剧本/,
  );
  assert.doesNotMatch(
    document.querySelector('.project-delete-dialog[role=alertdialog]').textContent,
    /另一虚构项目/,
  );
  assert.equal(document.activeElement, button('取消'));
  await act(() => button('取消').click());
  await flush();
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
  await act(() => button('删除所选').click());
  await flush();
  await act(() => button('确认删除').click());
  await flush();
  assert.deepEqual(
    requests.filter((r) => r.method === 'POST'),
    [{ url: '/api/projects/project_fictional/delete', method: 'POST', body: { revision: 4 } }],
  );
  assert.equal(rows.length, 1);
  assert.equal(document.querySelector('[aria-label="选择项目：虚构测试剧本"]'), null);
  assert.ok(document.body.textContent.includes('另一虚构项目'));
  await act(() => button('完成').click());
  await flush();
  assert.equal(document.querySelector('.project-manage-dock'), null);
});
test('project management stops a batch on conflict and requires refreshed selection before retry', async () => {
  await mount('/projects', project(), false);
  let rows = [
      {
        id: 'project_fictional',
        revision: 4,
        title: '虚构项目一',
        filename: 'one.txt',
        uploadedAt: null,
      },
      {
        id: 'project_other',
        revision: 9,
        title: '虚构项目二',
        filename: 'two.txt',
        uploadedAt: null,
      },
    ],
    conflict = true;
  requestInterceptor = (url, options) => {
    if (url === '/api/projects') return Response.json(copy(rows.map(row => ({ ...row, spaceId: 'workspace_fixture', ownerId: 'workspace_fixture', creatorId: 'workspace_fixture', creatorName: '虚构编辑者', sharedToMe: false, effectiveAccess: 2 }))));
    if (options.method === 'POST' && url.endsWith('/delete')) {
      const id = url.split('/')[3],
        body = JSON.parse(options.body as string);
      if (id === 'project_other' && conflict) {
        rows[0].revision = 10;
        return Response.json({ code: 'CONFLICT', message: 'CONFLICT' }, { status: 409 });
      }
      rows = rows.filter((p) => p.id !== id);
      return Response.json({ id, revision: body.revision + 1, deletedAt: '2026-10-14T01:00:00Z' });
    }
  };
  await act(() => currentContext.refreshProjects());
  await act(() => button('管理').click());
  await flush();
  await act(() => document.querySelector<HTMLButtonElement>('.project-select-all').click());
  await flush();
  assert.match(document.querySelector('.project-manage-dock').textContent, /已选 2 项/);
  await act(() => button('删除所选').click());
  await flush();
  await act(() => button('确认删除').click());
  await flush();
  assert.match(
    document.querySelector('.project-delete-dialog[role=alertdialog]').textContent,
    /已删除 1 个项目.*项目已更新/,
  );
  assert.equal(button('确认删除').disabled, true);
  assert.equal(requests.filter((r) => r.method === 'POST').length, 2);
  await act(() => button('重新读取').click());
  await flush();
  assert.equal(document.querySelector('.project-delete-dialog[role=alertdialog]'), null);
  assert.equal(button('删除所选').disabled, true);
  conflict = false;
  await act(() =>
    document.querySelector<HTMLElement>('[aria-label="选择项目：虚构项目二"]').click(),
  );
  await act(() => button('删除所选').click());
  await flush();
  await act(() => button('确认删除').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').at(-1).body.revision, 10);
  assert.equal(rows.length, 0);
  assert.equal(
    requests.some((r) => r.url.includes('-runs') || r.url.endsWith('/runs')),
    false,
  );
});
test('project management stays disabled on readonly services or absent deletion capability', async () => {
  await mount('/projects');
  assert.equal(button('管理').disabled, true);
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
  await mount('/projects', project(), false);
  capabilityOverrides = { projectDeletion: false };
  await act(() => currentContext.refreshModels());
  await flush();
  assert.equal(button('管理').disabled, true);
  assert.ok(document.querySelector('[aria-label="项目删除接口尚未接入。"]'));
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
});
test('project management preserves acknowledged deletion when the following list refresh fails', async () => {
  await mount('/projects', project(), false);
  let refreshFails = false,
    rows = [
      {
        id: 'project_fictional',
        revision: 4,
        title: '虚构测试剧本',
        filename: 'fixture.txt',
        uploadedAt: null,
      },
    ];
  requestInterceptor = (url, options) => {
    if (url === '/api/projects') {
      if (refreshFails) throw new TypeError('Fictional list transport failure');
      return Response.json(copy(rows.map(row => ({ ...row, spaceId: 'workspace_fixture', ownerId: 'workspace_fixture', creatorId: 'workspace_fixture', creatorName: '虚构编辑者', sharedToMe: false, effectiveAccess: 2 }))));
    }
    if (options.method === 'POST' && url.endsWith('/delete')) {
      rows = [];
      refreshFails = true;
      return Response.json({
        id: 'project_fictional',
        revision: 5,
        deletedAt: '2026-10-14T01:00:00Z',
      });
    }
  };
  await act(() => currentContext.refreshProjects());
  await act(() => button('管理').click());
  await flush();
  await act(() =>
    document.querySelector<HTMLElement>('[aria-label="选择项目：虚构测试剧本"]').click(),
  );
  await act(() => button('删除所选').click());
  await flush();
  await act(() => button('确认删除').click());
  await flush();
  assert.match(document.querySelector('[role=alert]').textContent, /已删除 1 个项目；列表刷新失败/);
  assert.equal(button('删除所选').disabled, true);
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  refreshFails = false;
  await act(() => button('重新读取').click());
  await flush();
  assert.equal(document.querySelector('[role=alert]'), null);
  assert.equal(document.querySelector('[aria-label="选择项目：虚构测试剧本"]'), null);
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
});
test('registered media preferences are selectable and fixed without enabling generation', async () => {
  await mount('/projects', project(), false, false, true);
  const media = {
    analysis: [],
    image: [{ id: 'registered-image', name: '虚构画面别名', connectionName: '虚构连接' }],
    video: [{ id: 'registered-video', name: '虚构视频别名', connectionName: '虚构连接' }],
    catalog: [
      { id: 'registered-image', name: '虚构画面别名' },
      { id: 'registered-video', name: '虚构视频别名' },
    ],
  };
  requestInterceptor = (url) => {
    if (url === '/api/models') return Response.json(media);
  };
  await act(() => currentContext.refreshModels());
  await flush();
  await openModelPanel();
  await act(() => {
    [...document.querySelectorAll('.xy-model-cat')]
      .find((e) => e.textContent.includes('分析模型'))
      .click();
  });
  await flush();
  assert.match(document.querySelector('.xy-model-panel-list').textContent, /暂无可用分析模型/);
  await pickModel('图像模型', '虚构画面别名');
  assert.ok(
    document.querySelector('button[aria-label="模型"]').textContent.includes('虚构画面别名'),
  );
  await pickModel('视频模型', '虚构视频别名');
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
  assert.equal(button('粘贴文本').disabled, false);
  assert.equal(document.querySelector('input[type=file]').disabled, false);
  assert.equal(button('开始解析').disabled, true);
  assert.doesNotMatch(document.body.textContent, /请先选择分析模型/);
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
});

test('Google shares one credential and preserves a complete mixed Gemini/Nano Banana draft', async () => {
  await mount('/model-config', project(), false, true);
  const items = [
    ['gemini-nano-banana-2.1', 'Nano Banana 2.1'],
    ['gemini-3-pro-image', 'Nano Banana Pro'],
    ['gemini-3.1-flash-image', 'Nano Banana 2'],
    ['gemini-3.1-flash-lite-image', 'Nano Banana 2 Lite'],
    ['gemini-2.5-flash-image', 'Nano Banana'],
  ];
  const models = items.map(([id, name]) => ({
    id,
    modelId: id,
    name,
    defaultName: name,
    alias: '',
    logoId: '',
    category: 'image',
    enabled: false,
    available: false,
    referenced: false,
    reason: '',
    contextWindow: 0,
    maxOutputTokens: 0,
    pricing: null,
    defaultLogoUrl: '/demo/assets/providers/google.svg',
  }));
  for (const [id, name] of [
    ['gemini-4-argon', 'Gemini 4 Argon'],
    ['gemini-3.8-flash', 'Gemini 3.8 Flash'],
    ['gemini-3.5-flash-lite', 'Gemini 3.5 Flash-Lite'],
  ])
    models.push({
      id,
      modelId: id,
      name,
      defaultName: name,
      alias: '',
      logoId: '',
      category: 'analysis',
      enabled: false,
      available: false,
      referenced: false,
      reason: '',
      contextWindow: 1048576,
      maxOutputTokens: 65536,
      creditPrice: null,
      pricing: {
        inputCnyPerMillion: 2.1,
        outputCnyPerMillion: 17.5,
        cacheHitHundredthsCnyPerMillion: 21,
      },
      defaultLogoUrl: '/demo/assets/providers/google.svg',
    });
  modelConfig.connections.push({
    id: 'google',
    name: 'Google Gemini',
    kind: 'official',
    protocol: 'gemini',
    baseUrl: 'https://generativelanguage.googleapis.com/v1beta',
    credentialConfigured: false,
    editable: true,
    reason: '',
    models,
  });
  await navigateHash('/projects');
  await navigateHash('/model-config');
  await act(() => document.getElementById('mc-tab-image').click());
  await flush();
  const card = document.querySelector('[data-connection-id=google]');
  assert.ok(card.textContent.includes('0/8 已启用'));
  assert.equal(card.querySelectorAll('.mc-config-action').length, 1);
  assert.equal(card.textContent.includes('gemini-nano-banana-2.1'), false);
  await act(() => document.getElementById('mc-config-google').click());
  await flush();
  assert.equal(document.querySelectorAll('.mc-official-model').length, 5);
  assert.equal(document.querySelectorAll('.mc-official-dialog [role=tab]').length, 2);
  assert.equal(document.getElementById('mc-edit-tab-video'), null);
  assert.equal(document.querySelectorAll('#model-api-key').length, 1);
  assert.equal(document.getElementById('model-base-url').disabled, true);
  assert.equal(document.getElementById('mc-model-id-0').disabled, true);
  assert.equal(document.getElementById('mc-model-id-0').value, 'gemini-nano-banana-2.1');
  await setField('model-api-key', 'fictional-gemini-key');
  await setField('mc-alias-0', '角色图片');
  await act(() => document.getElementById('mc-enable-0').click());
  await act(() => document.getElementById('mc-edit-tab-analysis').click());
  await flush();
  assert.equal(document.querySelectorAll('.mc-official-model').length, 3);
  assert.equal(document.getElementById('mc-model-id-5').value, 'gemini-4-argon');
  assert.equal(document.getElementById('mc-model-id-5').disabled, true);
  await setField('mc-alias-5', '剧本语言');
  await act(() => document.getElementById('mc-enable-5').click());
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const saved = requests.filter((r) => r.method === 'POST');
  assert.equal(saved.length, 1);
  assert.equal(saved[0].body.apiKey, 'fictional-gemini-key');
  assert.equal(saved[0].body.connection.protocol, 'gemini');
  assert.equal(saved[0].body.connection.models.length, 8);
  assert.equal(saved[0].body.connection.models[0].alias, '角色图片');
  assert.equal(saved[0].body.connection.models[5].alias, '剧本语言');
  assert.equal(saved[0].body.connection.models.filter((m) => m.enabled).length, 2);
  assert.equal(
    requests.some((r) => r.url.endsWith('/runs') || r.url.includes('/discover')),
    false,
  );
});

test('OpenAI image models stay one supplier and one key without language/video tabs', async () => {
  await mount('/model-config', project(), false, true);
  const items = [
    ['gpt-image-2', 'GPT Image 2'],
    ['gpt-image-2.5-sunburst', 'GPT Image 2.5 Sunburst'],
    ['gpt-image-2.5-flare', 'GPT Image 2.5 Flare'],
  ];
  modelConfig.connections.push({
    id: 'openai',
    name: 'OpenAI',
    kind: 'official',
    protocol: 'openai-image',
    baseUrl: 'https://api.openai.com/v1',
    credentialConfigured: false,
    editable: true,
    reason: '',
    models: items.map(([id, name]) => ({
      id,
      modelId: id,
      name,
      defaultName: name,
      alias: '',
      logoId: '',
      category: 'image',
      enabled: false,
      available: false,
      referenced: false,
      reason: '',
      contextWindow: 0,
      maxOutputTokens: 0,
      pricing: null,
      defaultLogoUrl: '/demo/assets/providers/openai.svg',
    })),
  });
  await navigateHash('/projects');
  await navigateHash('/model-config');
  await act(() => document.getElementById('mc-tab-image').click());
  await flush();
  assert.equal(document.querySelectorAll('[data-connection-id=openai]').length, 1);
  assert.ok(
    document.querySelector('[data-connection-id=openai]').textContent.includes('0/3 已启用'),
  );
  await act(() => document.getElementById('mc-config-openai').click());
  await flush();
  assert.equal(document.querySelectorAll('.mc-official-model').length, 3);
  assert.equal(document.querySelector('.mc-official-dialog [role=tab]'), null);
  assert.equal(document.querySelectorAll('#model-api-key').length, 1);
  assert.equal(document.getElementById('model-base-url').disabled, true);
  for (let i = 0; i < 3; i++) {
    assert.equal(document.getElementById('mc-model-id-' + i).value, items[i][0]);
    assert.equal(document.getElementById('mc-model-id-' + i).disabled, true);
    assert.equal(document.getElementById('mc-alias-' + i).placeholder, items[i][1]);
  }
  await setField('model-api-key', 'fictional-openai-key');
  await setField('mc-alias-2', '快速图片');
  await act(() => document.getElementById('mc-enable-2').click());
  await act(() =>
    document
      .querySelector('form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const saved = requests.filter((r) => r.method === 'POST');
  assert.equal(saved.length, 1);
  assert.equal(saved[0].body.connection.models.length, 3);
  assert.equal(saved[0].body.connection.protocol, 'openai-image');
  assert.equal(saved[0].body.connection.models[2].alias, '快速图片');
  assert.ok(
    saved[0].body.connection.models.every((m) => m.pricing === null && m.contextWindow === 0),
  );
  assert.equal(
    requests.some((r) => r.url.endsWith('/runs') || r.url.includes('/discover')),
    false,
  );
});
test('project creation retry reuses its completed general upload without duplicate file writes', async () => {
  await mount('/projects', project(), false, false, true);
  await chooseAnalysisForUpload();
  await act(() => button('粘贴文本').click());
  await flush();
  const field = document.querySelector<HTMLTextAreaElement>('textarea[aria-label="剧本正文"]');
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(
      field,
      '完整虚构重试剧本。',
    );
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(() => button('使用文本').click());
  await flush();
  let reject = true;
  requestInterceptor = (url, options) => {
    if (url === '/api/projects' && options.method === 'POST' && reject) {
      reject = false;
      return Response.json(
        { code: 'INVALID_INPUT', message: '虚构已确认拒绝，未创建项目' },
        { status: 400 },
      );
    }
  };
  await act(() => button('开始解析').click());
  await flush();
  assert.match(document.querySelector('[role=alert]').textContent, /未创建项目/);
  assert.equal(button('开始解析').disabled, false);
  await act(() => button('开始解析').click());
  await flush();
  assert.equal(requests.filter((r) => r.url === '/api/upload').length, 1);
  assert.equal(requests.filter((r) => r.url === '/api/projects' && r.method === 'POST').length, 2);
  assert.match(window.location.hash, /project_uploaded\/outline$/);
  assert.equal(fixture.runs.length, 0);
});

test('prompt edits save on blur across conflict and automatic readback; saves never start a model', async () => {
  await mount('/playlet/project_fictional/characters', project(), false);
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构小竹"]').click());
  await flush();
  const field = document.querySelector<HTMLTextAreaElement>('.asset-prompt-box textarea');
  await act(() => {
    field.focus();
    field.blur();
  });
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 0);
  await setField(field.id, '用户完整正文\n独立审阅修改');
  fixture.revision = 5;
  fixture.visualPrompts[0].text = '另一窗口的完整正文';
  await act(() => {
    field.focus();
    field.blur();
  });
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  assert.equal(requests.filter((r) => r.method === 'POST')[0].body.revision, 4);
  assert.equal(field.value, '用户完整正文\n独立审阅修改');
  assert.match(document.querySelector('[role=dialog]').textContent, /最新版本已读取/);
  requestInterceptor = (url, options) => {
    if (options.method === 'POST' && url.endsWith('/visual-prompt')) {
      const body = JSON.parse(String(options.body));
      fixture.revision++;
      Object.assign(fixture.visualPrompts[0], {
        text: body.text,
        origin: 'user',
        runId: '',
        stale: false,
      });
      return Response.json(copy(fixture));
    }
  };
  await act(() => {
    field.focus();
    field.blur();
  });
  await flush();
  assert.equal(fixture.visualPrompts[0].origin, 'user');
  assert.equal(requests.filter((r) => r.method === 'POST').at(-1).body.revision, 5);
  assert.equal(
    requests.some((r) => r.url.endsWith('/runs') || r.url.endsWith('-runs')),
    false,
  );
});

test('an open clean prompt editor receives generated and regenerated text without submitting writes', async () => {
  const p = project();
  p.runs = [
    {
      id: 'preparation',
      contract: 'asset-preparation-v1',
      sourceVersion: 1,
      status: 'running',
      stage: 'visualPrompt',
      callCount: 5,
      targets: p.assets.map((a) => ({ assetId: a.id })),
    },
  ];
  p.visualPrompts = [];
  await mount('/playlet/project_fictional/characters', p, false);
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构小竹"]').click());
  await flush();
  const field = document.querySelector<HTMLTextAreaElement>('.asset-prompt-box textarea');
  assert.equal(field.value, '');
  assert.equal(field.disabled, true);
  fixture.revision = 9;
  fixture.runs[0].status = 'completed';
  fixture.visualPrompts = [
    {
      target: { assetId: 'character_one' },
      text: '后台已保存的完整视觉正文',
      stale: false,
      sourceVersion: 1,
      runId: 'preparation',
    },
  ];
  await act(() => currentContext.refreshCurrentProject(fixture.id));
  await flush();
  assert.equal(field.value, '后台已保存的完整视觉正文');
  assert.equal(field.disabled, false);
  fixture.revision = 12;
  fixture.visualPrompts[0].text = '后台重新生成的完整正文';
  await act(() => currentContext.refreshCurrentProject(fixture.id));
  await flush();
  assert.equal(field.value, '后台重新生成的完整正文');
  await act(() => {
    field.focus();
    field.blur();
  });
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 0);
  await setField(field.id, '用户在最新正文上修改');
  requestInterceptor = (url, options) => {
    if (options.method === 'POST' && url.endsWith('/visual-prompt')) {
      const body = JSON.parse(String(options.body));
      assert.equal(body.revision, 12);
      fixture.revision++;
      Object.assign(fixture.visualPrompts[0], {
        text: body.text,
        origin: 'user',
        runId: '',
        stale: false,
      });
      return Response.json(copy(fixture));
    }
  };
  await act(() => {
    field.focus();
    field.blur();
  });
  await flush();
  assert.equal(fixture.visualPrompts[0].text, '用户在最新正文上修改');
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
});

test('prompt readback preserves a dirty draft and its original save revision', async () => {
  await mount('/playlet/project_fictional/characters', project(), false);
  await act(() => document.querySelector<HTMLElement>('[aria-label="查看 虚构小竹"]').click());
  await flush();
  const field = document.querySelector<HTMLTextAreaElement>('.asset-prompt-box textarea');
  await setField(field.id, '用户尚未提交的正文');
  fixture.revision = 7;
  fixture.visualPrompts[0].text = '另一窗口更新的正文';
  await act(() => currentContext.refreshCurrentProject(fixture.id));
  await flush();
  assert.equal(field.value, '用户尚未提交的正文');
  await act(() => {
    field.focus();
    field.blur();
  });
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST')[0].body.revision, 4);
  assert.equal(field.value, '用户尚未提交的正文');
  assert.match(document.querySelector('[role=dialog]').textContent, /最新版本已读取/);
});

test('asset task status exposes each active phase alongside its read-only explanation', async () => {
  const p = project();
  p.runs = [
    {
      id: 'preparation',
      contract: 'asset-preparation-v1',
      sourceVersion: 1,
      status: 'running',
      stage: 'extract',
      callCount: 0,
    },
  ];
  p.visualPrompts = [];
  await mount('/playlet/project_fictional/characters', p, false);
  for (const [stage, label] of [
    ['extract', '正在提取资产'],
    ['correct', '正在补漏纠错'],
    ['visualPrompt', '正在完善并核对资产'],
  ]) {
    fixture.runs[0].stage = stage;
    fixture.revision++;
    await act(() => currentContext.refreshCurrentProject(fixture.id));
    await flush();
    assert.ok(document.querySelector('.asset-task-message').textContent.includes(label));
    assert.ok(document.querySelector('.asset-task-message').textContent.includes('只读'));
  }
  assert.equal(requests.filter((r) => r.method === 'POST').length, 0);
});

test('script page starts parsing once and follows real stages through saved prompts and completion', async () => {
  const p = project();
  p.assets = [];
  p.runs = [];
  p.visualPrompts = [];
  await mount('/playlet/project_fictional/outline', p, false);
  assert.equal(document.querySelector('[role=progressbar]').getAttribute('aria-valuenow'), '0');
  assert.equal(requests.filter((r) => r.method === 'POST').length, 0);
  requestInterceptor = (url, options) => {
    if (url === '/api/status')
      return Response.json({
        readOnly: false,
        configured: true,
        capabilities: { assetEditing: true, visualPrompts: true },
      });
    if (options.method === 'POST' && url.endsWith('/runs')) {
      fixture.runs = [
        {
          id: 'parse',
          contract: 'asset-preparation-v1',
          sourceVersion: 1,
          status: 'running',
          stage: 'extract',
          callCount: 0,
        },
      ];
      fixture.revision++;
      return Response.json(copy(fixture));
    }
  };
  await act(() => currentContext.refreshModels());
  const start = button('下一步');
  await act(() => {
    start.click();
    start.click();
  });
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  assert.match(window.location.hash, /outline$/);
  assert.equal(
    document.querySelector('.script-parse-dock-status')!.getAttribute('data-state'),
    'running',
  );
  assert.equal(button('下一步').disabled, true);
  for (const [stage, percent, stepLabel] of [
    ['extract', '0', '提取资产'],
    ['correct', '33', '补漏纠错'],
    ['visualPrompt', '67', ''],
  ]) {
    fixture.runs[0].stage = stage;
    fixture.revision++;
    if (stage === 'visualPrompt') {
      fixture.assets = copy(project().assets);
      fixture.runs[0].targets = fixture.assets.map((a) => ({ assetId: a.id }));
    }
    await act(() => currentContext.refreshCurrentProject(fixture.id));
    await flush();
    assert.equal(
      document.querySelector('[role=progressbar]').getAttribute('aria-valuenow'),
      percent,
    );
    // 运行中第二行不重复主提示里的步骤名。
    if (stepLabel) {
      const copy = document.querySelector('.script-parse-dock-copy')!.textContent!;
      assert.equal((copy.match(new RegExp(stepLabel, 'g')) || []).length, 1);
    }
  }
  assert.doesNotMatch(document.querySelector('.script-parse-dock-status').textContent, /0\/3/);
  fixture.visualPrompts = [
    {
      target: { assetId: 'character_one' },
      text: '已保存的完整正文',
      stale: false,
      sourceVersion: 1,
      runId: 'parse',
    },
  ];
  fixture.revision++;
  await act(() => currentContext.refreshCurrentProject(fixture.id));
  await flush();
  // 运行中保留真实保存数量，作为唯一有意义的第二行状态。
  assert.match(
    document.querySelector('.script-parse-dock-copy')!.textContent!,
    /已保存 1\/3 项图片提示词/,
  );
  await act(() => button('查看解析进度').click());
  await flush();
  assert.match(document.body.textContent, /已保存 1\/3 项图片提示词/);
  assert.equal(document.querySelectorAll('.script-parse-steps li').length, 3);
  assert.equal(document.querySelector('.script-parse-steps').textContent.includes('%'), false);
  await act(() => button('关闭').click());
  await flush();
  assert.equal(document.querySelector('[role=progressbar]').getAttribute('aria-valuenow'), '67');
  fixture.visualPrompts = fixture.assets.map((a) => ({
    target: { assetId: a.id },
    text: '完整视觉正文 ' + a.id,
    stale: false,
    sourceVersion: 1,
    runId: 'parse',
  }));
  fixture.runs[0].status = 'completed';
  fixture.revision++;
  await act(() => currentContext.refreshCurrentProject(fixture.id));
  await flush();
  assert.equal(document.querySelector('[role=progressbar]').getAttribute('aria-valuenow'), '100');
  assert.match(document.body.textContent, /解析完成，可查看资产/);
  await act(() => button('下一步').click());
  await flush();
  assert.match(window.location.hash, /characters$/);
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  assert.equal(button('生成资产'), undefined);
});

test('ambiguous parsing start is read back without a second start or navigation', async () => {
  const p = project();
  p.runs = [];
  p.assets = [];
  p.visualPrompts = [];
  await mount('/playlet/project_fictional/outline', p, false);
  requestInterceptor = (url, options) => {
    if (url === '/api/status')
      return Response.json({
        readOnly: false,
        configured: true,
        capabilities: { visualPrompts: true },
      });
    if (options.method === 'POST' && url.endsWith('/runs')) {
      fixture.runs = [
        {
          id: 'lost-start',
          contract: 'asset-preparation-v1',
          sourceVersion: 1,
          status: 'running',
          stage: 'extract',
          callCount: 1,
        },
      ];
      fixture.revision++;
      throw new TypeError('lost start response');
    }
  };
  await act(() => currentContext.refreshModels());
  await act(() => button('下一步').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  assert.ok(requests.some((r) => r.method === 'GET' && r.url.endsWith('/project_fictional')));
  assert.equal(
    document.querySelector('.script-parse-dock-status')!.getAttribute('data-state'),
    'running',
  );
  assert.equal(button('下一步').disabled, true);
  assert.match(window.location.hash, /outline$/);
  await act(() => currentContext.refreshCurrentProject(fixture.id));
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
});

test('asset library redirects the initial parsing action to the script page without starting a model', async () => {
  const p = project();
  p.runs = [];
  p.assets = [];
  p.visualPrompts = [];
  await mount('/playlet/project_fictional/characters', p, false);
  assert.equal(button('生成资产'), undefined);
  assert.ok(button('查看解析进度'));
  await act(() => button('查看解析进度').click());
  await flush();
  assert.match(window.location.hash, /outline$/);
  assert.ok(button('下一步'));
  assert.equal(requests.filter((r) => r.method === 'POST').length, 0);
});

test('opening parsing details keeps initial focus in the dialog, close returns to the trigger without starting a task', async () => {
  await mount('/playlet/project_fictional/outline');
  const trigger = button('查看解析进度');
  await act(() => {
    trigger.focus();
    trigger.click();
  });
  await flush();
  const dialog = document.querySelector('.script-parse-dialog');
  assert.ok(dialog);
  assert.equal(document.activeElement, dialog);
  assert.notEqual(document.activeElement, dialog.querySelector('[aria-label="关闭"]'));
  await act(() => dialog.querySelector('button[aria-label="关闭"]').click());
  await flush();
  assert.equal(document.activeElement, trigger);
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});

test('script keeps a complete-source navigation entry without inventing episodes and uses the shared Next action', async () => {
  await mount('/playlet/project_fictional/outline');
  const nav = document.querySelector('nav[aria-label="分集目录"]');
  assert.ok(nav);
  assert.equal(nav.querySelectorAll('button').length, 1);
  assert.ok(button('完整剧本'));
  assert.equal(document.querySelectorAll('.editor-script-anchor').length, 0);
  const reader = document.querySelector('.jm-script-body'),
    text = reader.querySelector('pre').textContent;
  reader.scrollTo = ({ top }) => {
    reader.scrollTop = top;
  };
  reader.scrollTop = 96;
  await act(() => button('完整剧本').click());
  assert.equal(reader.scrollTop, 0);
  assert.equal(reader.querySelector('pre').textContent, text);
  await act(() => {
    button('查看解析进度').focus();
    button('查看解析进度').click();
  });
  await flush();
  assert.doesNotMatch(
    document.querySelector('[role=dialog]').textContent,
    /百分比按已完成步骤计算/,
  );
  assert.equal(document.querySelector('[role=dialog] a[href*="/process?"]'), null);
  await act(() => button('关闭').click());
  await flush();
  assert.equal(document.activeElement, button('查看解析进度'));
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});

test('ready assets omit the review status message while incomplete assets retain their actual status and action', async () => {
  await mount('/playlet/project_fictional/characters', project(), false);
  assert.doesNotMatch(document.body.textContent, /资产已生成，待审核/);
  assert.equal(document.querySelector('.asset-task-toolbar'), null);
  const p = project();
  p.visualPrompts.pop();
  await mount('/playlet/project_fictional/characters', p, false);
  assert.match(document.querySelector('.asset-task-message').textContent, /资产内容待完善/);
  assert.ok(button('完善资产'));
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});

test('character canvas and Look details keep thumbnail identity until the matching original is explicitly opened', async () => {
  const p = project(),
    image = (char: string) => ({
      fileId: 'file_' + char.repeat(32),
      thumbnailUrl: '/api/projects/project_fictional/files/file_' + char.repeat(32) + '/thumbnail',
      originalUrl: '/api/projects/project_fictional/files/file_' + char.repeat(32) + '/original',
    });
  p.assets[0].image = image('a');
  p.assets[0].looks = [
    { id: 'look_one', name: '晚宴装', description: '礼服', origin: 'user', image: image('b') },
  ];
  await mount('/playlet/project_fictional/characters/character_one/canvas', p);
  for (const owner of [p.assets[0], p.assets[0].looks[0]])
    assert.ok(
      document.querySelector('img[src="' + owner.image.thumbnailUrl + '?audience=workspace"]'),
    );
  assert.ok(
    [...document.querySelectorAll('img')].every(
      (img) => !img.getAttribute('src')?.split('?')[0].endsWith('/original'),
    ),
  );
  await act(() =>
    document
      .querySelector('img[src="' + image('b').thumbnailUrl + '?audience=workspace"]')
      .closest('button')
      .click(),
  );
  await flush();
  const card = document.querySelector('.asset-image-card');
  assert.equal(
    card.querySelector('img').getAttribute('src'),
    image('b').thumbnailUrl + '?audience=workspace',
  );
  assert.equal(document.querySelector('.asset-original-dialog'), null);
  await act(() => card.querySelector('.asset-image-open').click());
  await flush();
  assert.equal(
    document.querySelector('.asset-original-dialog img').getAttribute('src'),
    image('b').originalUrl + '?audience=workspace',
  );
  await act(() => document.querySelector('.asset-original-dialog [aria-label="关闭"]').click());
  await flush();
  assert.equal(document.querySelector('.asset-original-dialog img'), null);
  assert.equal(
    requests.some((r) => r.method !== 'GET'),
    false,
  );
});

test('project cards only show active run stages and hide terminal badges without starting tasks', async () => {
  const p = project();
  p.runs = [
    {
      id: 'index',
      contract: 'episode-index-agent-v1',
      sourceVersion: 1,
      status: 'running',
      stage: 'episodeIndex',
      callCount: 1,
    },
  ];
  await mount('/projects', p, false);
  assert.match(document.querySelector('.project-run-status').textContent, /正在识别分集/);
  for (const [stage, label] of [
    ['extract', '正在提取资产'],
    ['correct', '正在补漏纠错'],
    ['visualPrompt', '正在生成图片提示词'],
  ]) {
    fixture.runs = [{ ...fixture.runs[0], id: 'assets', contract: 'asset-preparation-v1', stage }];
    fixture.revision++;
    await act(() => currentContext.refreshProjects());
    await flush();
    assert.equal(document.querySelector('.project-run-status').textContent, label);
  }
  for (const status of ['cancelled', 'completed', 'failed']) {
    fixture.runs[0].status = status;
    fixture.revision++;
    await act(() => currentContext.refreshProjects());
    await flush();
    assert.equal(document.querySelector('.project-run-status'), null);
  }
  assert.equal(
    requests.some((r) => r.method === 'POST'),
    false,
  );
});

test('an official model price-only edit enables save and persists its single CAS configuration draft', async () => {
  await mount('/model-config', project(), false, true);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(button('保存').disabled, true);
  const price = document.querySelector('.op-price-fields input[inputmode=numeric]');
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(price, '7');
    price.dispatchEvent(new Event('input', { bubbles: true }));
  });
  assert.equal(button('保存').disabled, false);
  await act(() =>
    document
      .querySelector('.mc-editor form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  const writes = requests.filter((r) => r.method === 'POST');
  assert.equal(writes.length, 1);
  assert.equal(writes[0].url, '/api/model-config');
  assert.deepEqual(writes[0].body.connection.models[0].creditPrice, {
    unitPrice: 7,
    freeEnabled: false,
    freeStart: null,
    freeEnd: null,
  });
  assert.equal(writes[0].body.revision, 1);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  assert.equal(button('保存').disabled, true);
  assert.equal(document.querySelector('.op-price-fields input[inputmode=numeric]').value, '7');
});

test('model descriptions save through the same official CAS draft and can be cleared without changing the price or identity', async () => {
  await mount('/model-config', project(), false, true);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  const field = document.querySelector<HTMLInputElement>('input[aria-label="模型描述 1"]')!;
  assert.equal(field.value, '');
  assert.equal(button('保存').disabled, true);
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(
      field,
      '适合剧本理解与角色提取',
    );
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  assert.equal(button('保存').disabled, false);
  await act(() =>
    document
      .querySelector('.mc-editor form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  let writes = requests.filter((r) => r.method === 'POST');
  assert.equal(writes.length, 1);
  assert.equal(writes[0].body.connection.models[0].description, '适合剧本理解与角色提取');
  assert.equal(writes[0].body.connection.models[0].id, 'deepseek-flash');
  assert.equal(writes[0].body.connection.models[0].creditPrice, null);
  await act(() => document.getElementById('mc-config-deepseek').click());
  await flush();
  const saved = document.querySelector<HTMLInputElement>('input[aria-label="模型描述 1"]')!;
  assert.equal(saved.value, '适合剧本理解与角色提取');
  assert.equal(button('保存').disabled, true);
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(
      saved,
      '中'.repeat(201),
    );
    saved.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(() =>
    document
      .querySelector('.mc-editor form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  assert.match(document.querySelector('.mc-form-error').textContent, /模型描述最多200个字符/);
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  assert.equal(saved.value, '中'.repeat(201));
  await act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(saved, '');
    saved.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(() =>
    document
      .querySelector('.mc-editor form')
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
  writes = requests.filter((r) => r.method === 'POST');
  assert.equal(writes.length, 2);
  assert.equal(writes[1].body.connection.models[0].description, '');
  assert.equal(writes[1].body.revision, 2);
});

async function mockConfigurationFixture(
  missing = false,
  profile?: unknown,
  videoProfile?: unknown,
) {
  await mount('/model-config', project(), false, true);
  modelConfig.connections.push({
    id: 'mock',
    name: 'Mock 本地测试',
    kind: 'builtin',
    protocol: 'mock-media',
    baseUrl: '',
    credentialConfigured: false,
    editable: true,
    reason: '',
    models: [
      ['mock-image', 'image'],
      ['mock_video', 'video'],
    ].map(([id, category]) => ({
      id,
      modelId: id,
      alias: '',
      description: '',
      name: category === 'image' ? 'Mock Image' : 'Mock Video',
      category,
      enabled: false,
      available: false,
      referenced: false,
      reason: '',
      logoId: '',
      contextWindow: 0,
      maxOutputTokens: 0,
      pricing: null,
      creditPrice: { unitPrice: 0, freeEnabled: false, freeStart: null, freeEnd: null },
      ...(missing ? {} : { mockBehavior: { delaySeconds: 0, failurePercent: 0 } }),
    })),
  });
  if (profile)
    modelConfig.connections.find((c) => c.id === 'mock').models[0].mediaProfile = profile;
  if (videoProfile)
    modelConfig.connections.find((c) => c.id === 'mock').models[1].mediaProfile = videoProfile;
  await navigateHash('/projects');
  await navigateHash('/model-config');
  await act(() => button('本地测试').click());
  await flush();
  assert.equal(document.getElementById('mc-test-mock'), null);
  assert.equal(document.getElementById('mc-history-mock'), null);
  assert.equal(document.querySelector('.mc-mock-form'), null);
  await act(() => document.getElementById('mc-config-mock').click());
  await flush();
}
async function mockNumber(id: string, value: string) {
  await act(() => {
    const element = document.getElementById(id) as HTMLInputElement;
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value);
    element.dispatchEvent(new Event('input', { bubbles: true }));
  });
}
async function saveMockConfiguration() {
  await act(() =>
    document
      .querySelector('.mc-editor form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await flush();
}

test('Mock behavior saves independent image/video seconds and percentages through one connection CAS without management test jobs', async () => {
  await mockConfigurationFixture();
  assert.equal(document.getElementById('model-api-key'), null);
  assert.equal(button('保存').disabled, true);
  await mockNumber('mc-mock-delay-0', '3');
  await mockNumber('mc-mock-failure-0', '35');
  await act(() => document.getElementById('mc-enable-0').click());
  await act(() => document.getElementById('mc-edit-tab-video').click());
  await flush();
  assert.equal((document.getElementById('mc-mock-delay-1') as HTMLInputElement).value, '0');
  await mockNumber('mc-mock-delay-1', '5');
  await mockNumber('mc-mock-failure-1', '100');
  await act(() => document.getElementById('mc-enable-1').click());
  await saveMockConfiguration();
  const write = requests.filter((r) => r.method === 'POST');
  assert.equal(write.length, 1);
  assert.equal(write[0].url, '/api/model-config');
  assert.equal(write[0].body.revision, 1);
  assert.equal('apiKey' in write[0].body, false);
  assert.deepEqual(
    write[0].body.connection.models.map((m) => m.mockBehavior),
    [
      { delaySeconds: 3, failurePercent: 35 },
      { delaySeconds: 5, failurePercent: 100 },
    ],
  );
  await act(() => document.getElementById('mc-config-mock').click());
  await flush();
  await act(() => document.getElementById('mc-edit-tab-video').click());
  await flush();
  assert.equal((document.getElementById('mc-mock-delay-1') as HTMLInputElement).value, '5');
  assert.equal((document.getElementById('mc-mock-failure-1') as HTMLInputElement).value, '100');
  await act(() => document.getElementById('mc-edit-tab-image').click());
  await flush();
  assert.equal((document.getElementById('mc-mock-delay-0') as HTMLInputElement).value, '3');
  assert.equal((document.getElementById('mc-mock-failure-0') as HTMLInputElement).value, '35');
  assert.equal(button('保存').disabled, true);
  assert.equal(
    requests.some((r) => r.url.includes('mock-generations') || r.url.includes('mock-tasks')),
    false,
  );
});
test('Mock behavior blocks fractional, empty and out-of-contract fields without sending a partial or defaulted configuration', async () => {
  await mockConfigurationFixture();
  await act(() => document.getElementById('mc-enable-0').click());
  for (const [id, value] of [
    ['mc-mock-delay-0', '1.5'],
    ['mc-mock-delay-0', '61'],
    ['mc-mock-delay-0', '-1'],
    ['mc-mock-delay-0', ''],
    ['mc-mock-failure-0', '101'],
    ['mc-mock-failure-0', '3.5'],
  ]) {
    await mockNumber('mc-mock-delay-0', '0');
    await mockNumber('mc-mock-failure-0', '0');
    await mockNumber(id, value);
    await saveMockConfiguration();
    assert.equal((document.getElementById(id) as HTMLInputElement).value, value);
    assert.equal(
      (document.querySelector('.mc-editor form') as HTMLFormElement).checkValidity(),
      false,
    );
    assert.equal(requests.filter((r) => r.method === 'POST').length, 0);
  }
  await mockNumber('mc-mock-delay-0', '60');
  await mockNumber('mc-mock-failure-0', '0');
  await saveMockConfiguration();
  assert.deepEqual(
    requests.filter((r) => r.method === 'POST')[0].body.connection.models[0].mockBehavior,
    { delaySeconds: 60, failurePercent: 0 },
  );
});
test('Mock behavior preserves a conflicting simulation draft and only resubmits after explicit latest-revision readback', async () => {
  await mockConfigurationFixture();
  await mockNumber('mc-mock-delay-0', '4');
  await mockNumber('mc-mock-failure-0', '25');
  modelConfig.revision++;
  modelConfig.connections.find((c) => c.id === 'mock').models[0].mockBehavior = {
    delaySeconds: 8,
    failurePercent: 50,
  };
  await saveMockConfiguration();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  assert.equal((document.getElementById('mc-mock-delay-0') as HTMLInputElement).value, '4');
  assert.ok(button('读取最新配置'));
  await act(() => button('读取最新配置').click());
  await flush();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  assert.equal((document.getElementById('mc-mock-failure-0') as HTMLInputElement).value, '25');
  await saveMockConfiguration();
  const posts = requests.filter((r) => r.method === 'POST');
  assert.equal(posts.length, 2);
  assert.equal(posts[1].body.revision, 2);
  assert.deepEqual(posts[1].body.connection.models[0].mockBehavior, {
    delaySeconds: 4,
    failurePercent: 25,
  });
});
test('an incomplete Mock configuration is reported rather than invented as a zero-delay always-successful setting', async () => {
  await mockConfigurationFixture(true);
  assert.match(document.querySelector('.mc-editor')?.textContent || '', /模拟设置读取失败/);
  assert.equal(document.getElementById('mc-mock-delay-0'), null);
  await act(() => document.getElementById('mc-enable-0').click());
  assert.equal(button('保存').disabled, true);
  await saveMockConfiguration();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 0);
});

test('media resolution rates use the existing model CAS and preserve the shared promotion', async () => {
  await mockConfigurationFixture(false, {
    resolutions: [
      { id: '1K', label: '1K' },
      { id: '2K', label: '2K' },
      { id: '4K', label: '4K' },
    ],
    defaultResolution: '1K',
    aspectRatios: ['1:1'],
    sources: [],
    checkedAt: '2026-10-17',
    note: '各规格输出不同尺寸',
    supplierPrice: '本地成本 0',
    retired: false,
  });
  const editRate = async (label: string, value: string) => {
    const field = document.querySelector<HTMLInputElement>(
      `input[aria-label="${label}（积分/张）"]`,
    )!;
    assert.ok(field);
    await act(() => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, value);
      field.dispatchEvent(new Event('input', { bubbles: true }));
    });
  };
  await editRate('1K', '5');
  await editRate('2K', '9');
  await editRate('4K', '17');
  await saveMockConfiguration();
  const write = requests.filter((r) => r.method === 'POST').at(-1)!;
  assert.equal(write.url, '/api/model-config');
  assert.deepEqual(write.body.connection.models[0].creditPrice, {
    unitPrice: 5,
    resolutionPrices: { '2K': 9, '4K': 17 },
    freeEnabled: false,
    freeStart: null,
    freeEnd: null,
  });
  assert.equal('mediaProfile' in write.body.connection.models[0], false);
  await act(() => document.getElementById('mc-config-mock').click());
  await flush();
  assert.equal(
    document.querySelector<HTMLInputElement>('input[aria-label="2K（积分/张）"]')!.value,
    '9',
  );
  assert.equal(button('保存').disabled, true);
});

test('resolution rows save independent image and video switches, preserve prices and derive the highest image default', async () => {
  const common = {
    aspectRatios: ['1:1'],
    sources: [],
    checkedAt: '2026-10-17',
    note: '',
    supplierPrice: '',
    retired: false,
  };
  await mockConfigurationFixture(
    false,
    {
      ...common,
      resolutions: ['1K', '2K', '4K'].map((id) => ({ id, label: id })),
      defaultResolution: '1K',
    },
    {
      ...common,
      resolutions: ['480p', '720p', '1080p'].map((id) => ({ id, label: id })),
      defaultResolution: '1080p',
      durations: [1, 5, 10],
      defaultDuration: 10,
    },
  );
  const edit = async (label: string, value: string) => {
    const field = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!;
    await act(() => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, value);
      field.dispatchEvent(new Event('input', { bubbles: true }));
    });
  };
  await edit('2K（积分/张）', '12');
  await edit('4K（积分/张）', '3');
  assert.equal(document.querySelector('.mc-tier-default')!.parentElement!.textContent, '4K默认');
  await act(() =>
    document.querySelector<HTMLButtonElement>('[role=switch][aria-label="启用 4K 档位"]')!.click(),
  );
  assert.equal(document.querySelector('.mc-tier-default')!.parentElement!.textContent, '2K默认');
  assert.equal(
    document.querySelector<HTMLInputElement>('[aria-label="4K（积分/张）"]')!.value,
    '3',
  );
  await act(() => document.getElementById('mc-edit-tab-video')!.click());
  await flush();
  await edit('720p（积分/秒）', '6');
  await act(() =>
    document
      .querySelector<HTMLButtonElement>('[role=switch][aria-label="启用 1080p 档位"]')!
      .click(),
  );
  await saveMockConfiguration();
  const post = requests.filter((r) => r.method === 'POST').at(-1)!;
  assert.deepEqual(post.body.connection.models[0].resolutionEnabled, {
    '1K': true,
    '2K': true,
    '4K': false,
  });
  assert.deepEqual(post.body.connection.models[1].resolutionEnabled, {
    '480p': false,
    '720p': true,
    '1080p': false,
  });
  assert.equal(post.body.connection.models[0].creditPrice.resolutionPrices['4K'], 3);
  await act(() => document.getElementById('mc-config-mock')!.click());
  await flush();
  assert.equal(
    document
      .querySelector('[role=switch][aria-label="启用 4K 档位"]')!
      .getAttribute('aria-checked'),
    'false',
  );
  assert.equal(document.querySelector('.mc-tier-default')!.parentElement!.textContent, '2K默认');
  assert.equal(button('保存').disabled, true);
  await act(() =>
    document.querySelector<HTMLButtonElement>('[role=switch][aria-label="启用 4K 档位"]')!.click(),
  );
  assert.equal(button('保存').disabled, false);
  await act(() => document.getElementById('mc-edit-tab-video')!.click());
  await flush();
  await act(() =>
    document
      .querySelector<HTMLButtonElement>('[role=switch][aria-label="启用 480p 档位"]')!
      .click(),
  );
  await saveMockConfiguration();
  assert.equal(requests.filter((r) => r.method === 'POST').length, 1);
  assert.match(document.querySelector('.mc-form-error')!.textContent || '', /已开启的档位/);
});

test('home reads projects and models once initially and refreshes both on returning from a project', async () => {
  await mount('/projects', project(), false);
  const count = (url: string) => requests.filter(r => r.url === url && r.method === 'GET').length;
  assert.equal(count('/api/projects'), 1);
  assert.equal(count('/api/models'), 1);
  assert.equal(count('/api/status'), 1);
  await navigateHash('/playlet/project_fictional/scenes');
  await navigateHash('/projects');
  assert.equal(count('/api/projects'), 2);
  assert.equal(count('/api/models'), 2);
  assert.equal(count('/api/status'), 2);
});
