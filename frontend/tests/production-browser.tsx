// Isolated UI fixture: fictional in-memory data, no account, PG or model access.
import React from 'react';
import { createRoot } from 'react-dom/client';
import { HashRouter } from 'react-router-dom';
import { LanguageProvider } from '../src/contexts/LanguageContext';
import { WorkspaceProvider } from '../src/state/WorkspaceContext';
import { MainWorkspace } from '../src/MainWorkspace';
import { Toaster } from '../src/components/ui/sonner';
import { createStoryboardFixture } from './storyboard-fixture.mjs';
import raincoatSample from '../../demo/assets/styles/cinematic35mm.png';
import roomSample from '../../demo/assets/styles/urban.png';
import '../src/main.css';
import '../src/styles/page-transitions.css';
import '../src/styles/jimeng-controls.css';
import '../src/styles/asset-details.css';
import '../src/styles/asset-catalog.css';
import '../src/styles/script-upload.css';
import '../src/styles/workspace.css';
import '../src/styles/production.css';
import '../src/styles/script-parsing.css';
import '../src/styles/personal-credits.css';
import '../src/styles/editor-workspace.css';
import '../src/styles/media.css';
import '../src/styles/interface-controls.css';
import '../src/styles/production-layout.css';
import '../src/styles/feedback.css';
const fixture = createStoryboardFixture();
const query = new URLSearchParams(location.search);
fixture.setScenario(query.get('scenario') || 'normal');
if (query.get('analysis') === 'missing' && fixture.project.settings)
  fixture.project.settings.llmModel = '';
if (query.get('ratio') === 'portrait' && fixture.project.settings)
  fixture.project.settings.aspectRatio = '9:16';
if (query.get('batchImages') === '1' && fixture.project.settings)
  fixture.project.settings.imageModel = 'mock-image';
// 只给隔离页面预置任务，验证真实分镜卡片的状态样式；不连接任务服务。
const ready = query.get('videoTask') === 'running'
  ? fixture.fetch('/api/projects/p5_free/media-tasks', {
    method: 'POST',
    body: JSON.stringify({ commandId: 'strip-visual-fixture', revision: fixture.project.revision,
      modelId: 'mock_video', target: { episodeId: 'ep_upper', shotId: 'shot_1' } }),
  }).then(() => { fixture.mediaTasks[0].status = 'running'; })
  : Promise.resolve();
const raincoat = fixture.project.assets.find(a => a.id === 'character_1')?.looks.find(l => l.id === 'look_rain');
if (raincoat) raincoat.image = {
  fileId: 'fixture-raincoat',
  thumbnailUrl: raincoatSample,
  originalUrl: raincoatSample,
};
const room = fixture.project.assets.find(a => a.id === 'location_1');
if (room) room.image = {
  fileId: 'fixture-room',
  thumbnailUrl: roomSample,
  originalUrl: roomSample,
};
// Exercise the user's sparse-material case without filling it with demo imagery.
if (query.get('content') === 'sparse') {
  const shot = fixture.shots[0];
  shot.selectedAssets = [{ assetId: 'prop_1' }];
  shot.videoPrompt.parts = [
    { type: 'text', text: '固定镜头，手将' },
    { type: 'asset', assetId: 'prop_1' },
    { type: 'text', text: '放在桌面，松手后停顿。' },
  ];
}
// ?demo=progress：用虚构数据切换本集拆解的真实运行状态，只改内存夹具并按原路径回读。
const progressDemo = query.get('demo') === 'progress';
const demoRun = fixture.project.runs.find((r) => r.id === 'episode_upper');
if (progressDemo && demoRun) {
  demoRun.storyboardScope = {
    ...demoRun.storyboardScope,
    operation: 'breakdown',
    breakdownMode: 'general',
    videoConstraint: { modelId: 'mock_video', modelName: 'Mock Video', maxDurationSeconds: 10 },
  };
}
// 状态只由地址参数决定，例如 &state=review / cancelled / failed / completed，页面不叠加演示控件。
const demoStates: Record<string, { status: string; stage: string; error?: string }> = {
  running: { status: 'running', stage: 'storyboard' },
  review: { status: 'running', stage: 'storyboardReview' },
  cancelled: { status: 'cancelled', stage: 'storyboardReview' },
  failed: { status: 'failed', stage: 'storyboard', error: '模型请求失败：上游服务暂不可用' },
  completed: { status: 'completed', stage: 'storyboardReview' },
};
const demoState = progressDemo && demoRun ? demoStates[query.get('state') || 'running'] : undefined;
if (demoState && demoRun)
  Object.assign(demoRun, { status: demoState.status, stage: demoState.stage, error: demoState.error || '' });
// 演示模式下把本集原文换成一段较长的虚构文字，只改返回给页面的副本。
const demoScript = [
  '雨夜，旧城区的公寓楼道灯忽明忽暗。林晚撑着伞站在门口，雨衣下摆还在滴水。',
  '门开了。屋里只亮着一盏台灯，林晚把铜钥匙放在桌上，手指在钥匙上停了一下，才慢慢松开。',
  '“这是你母亲留下的。”她没有抬头，“她说，等你愿意回来，就把它交给你。”',
  '另一位林晚站在窗边，背对着她。窗外的雨声盖过了很久的沉默。',
  '“你自己决定。”林晚终于开口，声音比雨声还轻。',
  '她转身走向门口，没有回头。桌上的钥匙在灯下泛着暗黄的光，像一件迟到了很多年的东西。',
];
const fixtureFetch = fixture.fetch;
window.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
  const response = await fixtureFetch(input, init);
  const path = String(input);
  if (!progressDemo || !/\/storyboard\/episodes\/ep_upper\/source$/.test(path)) return response;
  const body = await response.json();
  const spanId = body.fragments?.[0]?.spanId;
  if (spanId)
    body.fragments = demoScript.map((text, i) => ({ spanId, startOffset: i, endOffset: i + 1, text }));
  return Response.json(body);
}) as typeof fetch;
if (!progressDemo) window.fetch = fixture.fetch;
if (query.get('theme')) localStorage.setItem('theme', query.get('theme')!);
if (!location.hash)
  location.hash = progressDemo
    ? '#/playlet/p5_free/production?episodeId=ep_upper'
    : '#/playlet/p5_free/episodes';
void ready.then(() => createRoot(document.getElementById('root')!).render(
  <LanguageProvider>
    <HashRouter>
      <WorkspaceProvider>
        <MainWorkspace />
        <Toaster />
        <span className="production-fixture-note">
          隔离 UI 夹具 · 虚构内容及示意图 · 不连接账号、数据库或模型
        </span>
      </WorkspaceProvider>
    </HashRouter>
  </LanguageProvider>,
));
