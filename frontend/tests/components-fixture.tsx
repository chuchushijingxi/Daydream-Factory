// Browser-only fictional fixture. Not a production entry or production fallback.
import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { HashRouter } from 'react-router-dom';
import { LanguageProvider } from '../src/contexts/LanguageContext';
import { WorkspaceProvider } from '../src/state/WorkspaceContext';
import { MainWorkspace } from '../src/MainWorkspace';
import { Button } from '../src/components/ui/button';
import { Toaster, toast } from '../src/components/ui/sonner';
import '../src/main.css';
import '../src/styles/jimeng-controls.css';

let failNext = false,
  writes = 0;
const assets = [
  {
    id: 'fictional_character',
    kind: 'character',
    name: '虚构小竹',
    description: '虚构角色完整资料，修正描述不会清空其他输入。',
    aliases: [],
    sourceIds: ['fictional_span'],
    origin: 'model',
    character: {
      roleType: '',
      age: '',
      gender: '',
      biography: '',
      appearance: '',
      personality: '',
    },
    looks: [{ id: 'fictional_look', name: '雨衣形象', description: '蓝色雨衣' }],
  },
  {
    id: 'fictional_scene',
    kind: 'location',
    name: '虚构车站',
    description: '清晨的车站，入口有一盏旧灯。',
    aliases: [],
    sourceIds: ['fictional_span'],
    origin: 'model',
    character: null,
    looks: [],
  },
  {
    id: 'fictional_prop',
    kind: 'prop',
    name: '虚构怀表',
    description: '银色怀表。',
    aliases: [],
    sourceIds: [],
    origin: 'model',
    character: null,
    looks: [],
  },
];
const targets = assets.flatMap((a) => [
  { assetId: a.id },
  ...a.looks.map((l) => ({ assetId: a.id, lookId: l.id })),
]);
const project: any = {
  id: 'project_component_fixture',
  spaceId: 'component_fixture', ownerId: 'component_fixture', creatorId: 'component_fixture',
  title: '虚构组件测试 · 无外发',
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
      callCount: 0,
      targets,
    },
  ],
  outline: null,
  settings: {
    aspectRatio: '9:16',
    llmModel: 'fictional_text',
    imageModel: 'fictional_image',
    videoModel: 'fictional_video',
  },
  style: null,
  visualPrompts: targets.map((target) => ({
    target,
    text: '虚构测试正文。',
    stale: false,
    sourceVersion: 1,
    runId: 'visual',
  })),
  catalogEditedByUser: false,
  usedOrReservedMicroCny: 0,
  limitMicroCny: 0,
};
const copy = () => JSON.parse(JSON.stringify(project));
let editorOwned = false;
window.fetch = async (input, options = {}) => {
  const url = new URL(String(input), 'http://fixture.invalid').pathname,
    method = options.method || 'GET';
  if (url === '/api/projects/' + project.id + '/permissions') return Response.json({spaceId: project.spaceId, ownerId: project.ownerId, creatorId: project.creatorId, access: 2, canShare: false});
  if (url === '/api/projects/' + project.id + '/editing') {
    if (method === 'DELETE') { editorOwned = false; return Response.json({released: true}); }
    if (method === 'POST') editorOwned = true;
    return Response.json({userId: editorOwned ? project.ownerId : '', username: editorOwned ? '组件测试编辑者' : '', expiresAt: editorOwned ? new Date(Date.now()+90000).toISOString() : null, owned: editorOwned});
  }
  if (method === 'GET') {
    if (url === '/api/status')
      return Response.json({ readOnly: false, configured: false, capabilities: {} });
    if (url === '/api/projects')
      return Response.json([{ id: project.id, title: project.title, filename: 'fiction.txt' }]);
    if (url === '/api/models')
      return Response.json({
        analysis: [
          { id: 'fictional_text', name: '虚构文本模型（无法外发）' },
          { id: 'fictional_text_two', name: '虚构候选文本模型' },
        ],
        image: [{ id: 'fictional_image', name: '虚构图像模型' }],
        video: [{ id: 'fictional_video', name: '虚构视频模型' }],
      });
    if (url === '/api/visual-styles')
      return Response.json({
        styles: [
          {
            id: 'fictional_style',
            name: '虚构风格',
            description: '仅用于组件选中测试',
            promptVersion: 1,
            previewUrl: '/demo/assets/styles/anime2d.png',
          },
        ],
      });
    if (url === '/api/projects/' + project.id + '/source')
      return Response.json({
        version: 1,
        filename: 'fiction.txt',
        spans: [{ id: 'fictional_span', text: '这是虚构组件测试剧本，不来自任何真实项目。' }],
      });
    if (url === '/api/projects/' + project.id) return Response.json(copy());
  }
  if (
    method === 'POST' &&
    ['/settings', '/style', ...project.assets.map((a: any) => '/assets/' + a.id)].some(
      (suffix) => url === '/api/projects/' + project.id + suffix,
    )
  ) {
    writes++;
    document.getElementById('fixture-writes')!.textContent = String(writes);
    await new Promise((resolve) => setTimeout(resolve, 900));
    if (failNext) {
      failNext = false;
      return Response.json(
        { code: 'REVISION_CONFLICT', message: '虚构冲突：草稿保留，读取最新版本后可以重试。' },
        { status: 409 },
      );
    }
    const body = JSON.parse(String(options.body));
    if (url.endsWith('/settings')) project.settings = body.settings;
    else if (url.includes('/assets/')) {
      const asset = project.assets.find((a: any) => url.endsWith('/' + a.id));
      if (!asset) throw new Error('Fixture asset missing');
      Object.assign(asset, body.asset);
    } else if (url.endsWith('/style'))
      project.style = { id: 'fictional_style', name: '虚构风格', promptVersion: 1 };
    else throw new Error('Blocked fixture write ' + url);
    project.revision++;
    return Response.json(copy());
  }
  throw new Error('Blocked fixture request: ' + method + ' ' + url);
};
if (!location.hash) location.hash = '#/playlet/project_component_fixture/outline';
function FixturePanel() {
  const [reduced, setReduced] = useState(false);
  return (
    <>
      <div
        style={{
          position: 'fixed',
          bottom: 82,
          right: 16,
          zIndex: 120,
          maxWidth: 'calc(100vw - 32px)',
          background: 'var(--jm-raised)',
          color: 'var(--jm-text)',
          border: '1px solid var(--jm-border)',
          borderRadius: 12,
          padding: 12,
          fontSize: 12,
        }}
      >
        <p>
          组件验证夹具 · 虚构数据 · 无外发 · 本地写入 <span id="fixture-writes">0</span>
        </p>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, marginTop: 8 }}>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              failNext = true;
              toast.success('下次普通保存模拟冲突');
            }}
          >
            下次保存失败
          </Button>
          <Button size="sm" variant="outline" onClick={() => toast.success('虚构保存成功')}>
            成功通知
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              toast.error('虚构失败：此长消息只在本地夹具展示，关闭后可重试，不覆盖正确资料。')
            }
          >
            错误通知
          </Button>
          <Button size="sm" variant="outline" onClick={() => setReduced((v) => !v)}>
            {reduced ? '恢复动画' : '减弱动态夹具'}
          </Button>
        </div>
      </div>
      {reduced ? (
        <style>{`.jm-button,.jm-tab,.jm-style-card,.jm-toast-slot{transition:none!important}.jm-spinner,.jm-dialog-content,.jm-dialog-mask,.jm-select-content,.jm-tooltip,.jm-toast-slot{animation:none!important}`}</style>
      ) : null}
    </>
  );
}
const root = createRoot(document.getElementById('root')!);
root.render(
  <LanguageProvider>
    <HashRouter>
      <WorkspaceProvider>
        <MainWorkspace />
        <FixturePanel />
        <Toaster />
      </WorkspaceProvider>
    </HashRouter>
  </LanguageProvider>,
);

if (import.meta.hot) import.meta.hot.dispose(() => root.unmount());
