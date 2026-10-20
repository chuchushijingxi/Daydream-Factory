import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act, useState } from 'react';
import { JSDOM } from 'jsdom';
import { createStoryboardFixture } from './storyboard-fixture.mjs';
import { createAdministrationFixture } from './administration-fixture';
import type { ChargeQuoteDTO, UsageBasisDTO } from '../src/api/administration-types';
import type { MediaCredit, MediaTarget } from '../src/api/project-media';
import type { MediaProfile } from '../src/api/types';

const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/#/playlet/p5_free/props',
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
])
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
globalThis.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
globalThis.innerWidth = 1440;
globalThis.innerHeight = 1000;
globalThis.getComputedStyle = dom.window.getComputedStyle;
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const { createRoot } = await import('react-dom/client'),
  { HashRouter } = await import('react-router-dom');
const { LanguageProvider } = await import('../src/contexts/LanguageContext'),
  { WorkspaceProvider, useWorkspace } = await import('../src/state/WorkspaceContext');
const { SessionProvider } = await import('../src/state/SessionContext'),
  { sessionBoundary } = await import('../src/api/session-boundary');
const { MediaGenerator } = await import('../src/features/media/MediaGenerator');
const { projectMediaApi } = await import('../src/api/project-media');
const { MediaSettings, selectMediaOptions } = await import('../src/features/media/MediaSettings');
let root: ReturnType<typeof createRoot> | undefined,
  fixture = createStoryboardFixture(),
  requests: { path: string; body: any }[] = [],
  mode = 'normal',
  price = 5;
let changeTarget: (target: MediaTarget) => void, releaseStart: (response: Response) => void;
let changeGenerationDisabled: (disabled: boolean) => void;
let releaseQuote: () => void;
let releaseHistory: () => Promise<void>;
let heldQuotes: { body: any; release: () => void }[] = [];
let estimatedDurationSeconds: number | undefined;
let mediaKind: 'image' | 'video' = 'image',
  workspace: ReturnType<typeof useWorkspace>,
  videoProfile: MediaProfile;
const mockVideoProfile = (): MediaProfile => ({
  resolutions: [
    { id: '480p', label: '480p', pixels: { '16:9': '854×480' } },
    { id: '720p', label: '720p', pixels: { '16:9': '1280×720' } },
    { id: '1080p', label: '1080p', pixels: { '16:9': '1920×1080' } },
  ],
  defaultResolution: '720p',
  durations: Array.from({ length: 10 }, (_, i) => i + 1),
  defaultDuration: 5,
  aspectRatios: ['16:9'],
  retired: false,
  note: '',
  supplierPrice: '',
  sources: [],
  checkedAt: '',
});
function quote(): ChargeQuoteDTO {
  return {
    basis: 'basis-' + price,
    sourceVersion: 1,
    inputRevision: fixture.project.revision,
    modelId: 'mock-image',
    connectionId: 'mock',
    scope: 'media-image',
    quantity: 1,
    unit: 'image',
    priceVersion: 1,
    rule: { unitPrice: price, freeEnabled: false, freeStart: null, freeEnd: null },
    originalAmount: price,
    amount: price,
    free: false,
  };
}
function basis(): UsageBasisDTO {
  const { basis: _, ...value } = quote();
  return { ...value, id: 'charge', scopeDigest: 'digest', acceptedAt: '2026-10-17T00:00:00Z' };
}
function credit(patch: Partial<MediaCredit> = {}): MediaCredit {
  return {
    basis: basis(),
    prepaid: 5,
    refunded: 0,
    charged: 0,
    settlement: 'pending',
    deductionId: 'deduction',
    refundId: null,
    ...patch,
  };
}
function Probe() {
  workspace = useWorkspace();
  const { currentProject } = workspace;
  const [target, setTarget] = useState<MediaTarget>(
    mediaKind === 'image' ? { assetId: 'prop_1' } : { episodeId: 'ep_upper', shotId: 'shot_1' },
  );
  changeTarget = setTarget;
  const [disabled, setDisabled] = useState(false);
  changeGenerationDisabled = setDisabled;
  return currentProject ? <MediaGenerator key={mode === 'held-video-quotes' && 'shotId' in target ? target.shotId : undefined} target={target} kind={mediaKind} inline disabled={disabled} estimatedDurationSeconds={estimatedDurationSeconds} hasVideo={mode === 'existing-video' && 'shotId' in target && target.shotId === 'shot_1'} /> : null;
}
const flush = async () =>
  act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 15));
  });
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) => node.textContent?.trim() === text || node.getAttribute('aria-label') === text,
  )!;
async function mount(nextMode = 'normal', kind: 'image' | 'video' = 'image', estimate?: number) {
  estimatedDurationSeconds = estimate;
  if (root) await act(async () => root!.unmount());
  sessionBoundary.reset();
  sessionStorage.clear();
  fixture = createStoryboardFixture();
  requests = [];
  heldQuotes = [];
  mode = nextMode;
  price = 5;
  mediaKind = kind;
  videoProfile = mockVideoProfile();
  const quoted = (body: any): ChargeQuoteDTO => {
    const video = kind === 'video',
      rates: Record<string, number> = { '480p': 2, '720p': 4, '1080p': 10 },
      rate = video ? rates[body.options.resolution] : body.options?.resolution === '2K' ? 9 : price,
      quantity = video ? body.options.durationSeconds : 1;
    return {
      ...quote(),
      modelId: body.modelId,
      scope: video ? 'media-video' : 'media-image',
      unit: video ? 'second' : 'image',
      quantity,
      basis: video ? 'video-' + JSON.stringify(body.options) : 'basis-' + rate,
      rule: { ...quote().rule, unitPrice: rate },
      originalAmount: quantity * rate,
      amount: mode === 'video-free' || mode === 'image-free' ? 0 : quantity * rate,
      free: mode === 'video-free' || mode === 'image-free',
      mediaOptions: body.options,
    };
  };
  globalThis.fetch = async (url, options = {}) => {
    const path = new URL(String(url), 'http://fixture.invalid').pathname,
      body = typeof options.body === 'string' ? JSON.parse(options.body) : null;
    if (path === '/api/models') {
      const v = await (await fixture.fetch(url, options)).json();
      if (kind === 'image') {
        v.image[0].free = mode === 'image-free';
        if (mode === 'missing-image-price') v.image[0].creditPrice = null;
        return Response.json(v);
      }
      return Response.json({
        ...v,
        video: [
          {
            ...v.video[0], mediaProfile: videoProfile, billingUnit: 'second',
            creditPrice: mode === 'missing-video-price' ? undefined : {
              unitPrice: 4, resolutionPrices: { '480p': 2, '720p': 4, '1080p': 10 },
              freeEnabled: false, freeStart: null, freeEnd: null,
            },
            free: mode === 'video-free',
          },
          {
            id: 'mock_alternate',
            name: '不同能力的测试模型',
            provider: 'mock',
            mediaProfile: {
              ...videoProfile,
              resolutions: [
                { id: '720p', label: '720P' },
                { id: '1080p', label: '1080P' },
              ],
              defaultResolution: '1080p',
              durations: [2, 6],
              defaultDuration: 6,
            },
          },
        ],
      });
    }
    if (path.endsWith('/media-tasks') && options.method !== 'POST' && mode === 'existing-video')
      return new Promise((resolve) => {
        releaseHistory = async () => resolve(await fixture.fetch(url, options));
      });
    if (path.endsWith('/quote')) {
      requests.push({ path, body });
      if (mode === 'quote-error')
        return Response.json({ code: 'INTERNAL', message: '价格读取失败' }, { status: 500 });
      const response = quoted(body);
      if (['held-video-quotes', 'missing-video-price', 'held-image-quotes', 'missing-image-price', 'image-free', 'held-quote-error'].includes(mode))
        return new Promise((resolve) => {
          const fail = mode === 'held-quote-error';
          heldQuotes.push({ body, release: () => resolve(fail
            ? Response.json({ code: 'INTERNAL', message: '价格读取失败' }, { status: 500 })
            : Response.json(response)) });
        });
      if (
        mode === 'delayed-quote' &&
        body.options.resolution === '480p' &&
        body.options.durationSeconds === 5
      )
        return new Promise((resolve) => {
          releaseQuote = () => resolve(Response.json(response));
        });
      return Response.json(response);
    }
    if (path.endsWith('/media-tasks') && options.method === 'POST') {
      requests.push({ path, body });
      if (mode === 'late-conflict')
        return new Promise((resolve) => {
          releaseStart = resolve;
        });
      if (mode === 'conflict') {
        mode = 'normal';
        price = 7;
        return Response.json(
          { code: 'BILLING_BASIS_CHANGED', message: '报价已变化' },
          { status: 409 },
        );
      }
      const response = await fixture.fetch(url, options);
      if (!fixture.mediaTasks[0].credit) {
        const { basis: _, mediaOptions: __, ...q } = quoted(body);
        fixture.mediaTasks[0].credit = credit({
          basis: { ...q, id: 'charge', scopeDigest: 'digest', acceptedAt: '2026-10-17T00:00:00Z' },
          prepaid: q.amount,
        });
      }
      if (mode === 'unknown') {
        mode = 'normal';
        throw new Error('response lost after accept');
      }
      return Response.json(
        { ...(await response.json()), credit: fixture.mediaTasks[0].credit },
        { status: 202 },
      );
    }
    return fixture.fetch(url, options);
  };
  const service = createAdministrationFixture().service;
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <HashRouter>
          <SessionProvider service={service}>
            <WorkspaceProvider>
              <Probe />
            </WorkspaceProvider>
          </SessionProvider>
        </HashRouter>
      </LanguageProvider>,
    ),
  );
  await flush();
  await flush();
}
after(async () => {
  if (root) await act(async () => root!.unmount());
  dom.window.close();
});

test('media uses the server quote, submits only its basis and separates prepayment from actual settlement', async () => {
  await mount();
  assert.deepEqual(latestQuote().options, { resolution: '4K' });
  assert.equal(document.querySelector('[aria-label="分辨率"]'), null);
  assert.equal(document.querySelector('.media-button-price')?.textContent?.trim(), '5');
  assert.equal(button('生成图片').disabled, false);
  assert.deepEqual(requests.filter((r) => r.path.endsWith('/quote')).at(-1)!.body, {
    revision: fixture.project.revision,
    operation: 'media',
    modelId: 'mock-image',
    target: { assetId: 'prop_1' },
    options: { resolution: '4K' },
    aspectRatio: fixture.project.settings.aspectRatio,
  });
  await act(async () => button('生成图片').click());
  await flush();
  const starts = requests.filter((r) => r.path.endsWith('/media-tasks'));
  assert.equal(starts.length, 1);
  assert.equal(starts[0].body.billingBasis, 'basis-5');
  assert.equal('amount' in starts[0].body, false);
  assert.equal(document.querySelector('.media-history'), null);
  assert.match(document.body.textContent || '', /排队中/);
  assert.doesNotMatch(document.body.textContent || '', /实扣 5/);
  fixture.completeMedia();
  fixture.mediaTasks[0].credit = credit({ settlement: 'settled', charged: 5 });
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(fixture.mediaTasks[0].credit.charged, 5);
  assert.equal(document.querySelector('.media-history'), null);
  assert.equal(requests.filter((r) => r.path.endsWith('/media-tasks')).length, 1);
});
test('a price conflict reads a fresh quote and waits for an explicit new start', async () => {
  await mount('conflict');
  await act(async () => button('生成图片').click());
  await flush();
  await flush();
  assert.equal(document.querySelector('.media-button-price')?.textContent?.trim(), '7');
  assert.equal(requests.filter((r) => r.path.endsWith('/media-tasks')).length, 1);
  assert.equal(fixture.mediaTasks.length, 0);
  await act(async () => button('生成图片').click());
  await flush();
  assert.equal(
    requests.filter((r) => r.path.endsWith('/media-tasks'))[1].body.billingBasis,
    'basis-7',
  );
});
test('unknown acceptance keeps the original command and frozen basis instead of creating another debit intent', async () => {
  await mount('unknown');
  await act(async () => button('生成图片').click());
  await flush();
  assert.equal(button('生成图片').disabled, true);
  assert.ok(button('核对原命令'));
  price = 9;
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(requests.filter((r) => r.path.endsWith('/media-tasks')).length, 1);
  await act(async () => button('核对原命令').click());
  await flush();
  const starts = requests.filter((r) => r.path.endsWith('/media-tasks'));
  assert.equal(starts.length, 2);
  assert.deepEqual(starts[1].body, starts[0].body);
  assert.equal(fixture.mediaTasks.length, 1);
  assert.equal(starts[1].body.billingBasis, 'basis-5');
  assert.equal(document.querySelector('.media-credit'), null);
});
test('a late rejected start cannot clear another targets original pending command', async () => {
  await mount('late-conflict');
  await act(async () => button('生成图片').click());
  await flush();
  const target = { assetId: 'prop_unselected' },
    key = 'media-command:p5_free:' + JSON.stringify(target),
    pending = {
      commandId: 'new-target-command',
      revision: fixture.project.revision,
      modelId: 'mock-image',
      target,
      billingBasis: 'frozen-new-target',
    };
  sessionStorage.setItem(key, JSON.stringify(pending));
  await act(async () => changeTarget(target));
  await flush();
  assert.ok(button('核对原命令'));
  await act(async () =>
    releaseStart(
      Response.json(
        { code: 'BILLING_BASIS_CHANGED', message: 'old quote rejected' },
        { status: 409 },
      ),
    ),
  );
  await flush();
  assert.ok(button('核对原命令'));
  assert.equal(button('生成图片').disabled, true);
  assert.deepEqual(JSON.parse(sessionStorage.getItem(key)!), pending);
  assert.equal(requests.filter((r) => r.path.endsWith('/media-tasks')).length, 1);
});
test('media client keeps the shared 401 and late-response session boundary and rejects fabricated refund projections', async () => {
  const task = {
    id: 'task',
    projectId: 'p',
    target: { assetId: 'a' },
    revision: 1,
    status: 'failed',
    credit: credit({ settlement: 'refunded', refunded: 5, refundId: null }),
  };
  globalThis.fetch = async () => Response.json(task);
  await assert.rejects(
    projectMediaApi.get('p', 'task'),
    (e) => (e as any).code === 'PROGRAM_FAILED',
  );
  let unauthorized = 0;
  sessionBoundary.onUnauthorized = () => {
    unauthorized++;
  };
  globalThis.fetch = async () =>
    Response.json({ code: 'SESSION_REPLACED', message: 'replaced' }, { status: 401 });
  await assert.rejects(
    projectMediaApi.get('p', 'task'),
    (e) => (e as any).code === 'SESSION_REPLACED',
  );
  assert.equal(unauthorized, 1);
  sessionBoundary.onUnauthorized = undefined;
  let release!: (r: Response) => void;
  globalThis.fetch = async () =>
    new Promise((resolve) => {
      release = resolve;
    });
  const late = projectMediaApi.get('p', 'task');
  sessionBoundary.reset();
  release(Response.json({ ...task, credit: null }));
  await assert.rejects(late, (e) => (e as any).code === 'SESSION_CHANGED');
});

test('character types force landscape sheets, preserve the portrait draft and freeze type with the original command', async () => {
  await mount();
  fixture.project.settings.aspectRatio = '9:16';
  await act(async () => workspace.refreshCurrentProject(fixture.project.id));
  await flush();
  await act(async () => changeTarget({ assetId: 'character_1' }));
  await flush();
  assert.equal(latestQuote().imageType, 'closeup_main');
  assert.equal(latestQuote().aspectRatio, '16:9');
  assert.equal(document.querySelector<HTMLButtonElement>('[aria-label="图片比例"]')!.disabled, true);
  const fixedRatio = document.querySelector<HTMLButtonElement>('[aria-label="图片比例"]')!;
  for (const event of [
    new MouseEvent('pointerdown', { button: 0, ctrlKey: false, bubbles: true }),
    ...['Enter', ' ', 'ArrowDown'].map(key => new KeyboardEvent('keydown', { key, bubbles: true })),
  ]) {
    await act(() => fixedRatio.dispatchEvent(event));
    await flush();
    assert.equal(!!document.querySelector('.image-aspect-ratio-menu'), false);
  }
  assert.equal(document.querySelector('[aria-label="分辨率"]'), null);
  await selectImageType('主视图');
  assert.equal(latestQuote().imageType, 'main_view');
  assert.equal(latestQuote().aspectRatio, '9:16');
  for (const label of ['三视图', '特写 + 三视图']) {
    await selectImageType(label);
    assert.equal(latestQuote().aspectRatio, '16:9');
    assert.equal(document.querySelector<HTMLButtonElement>('[aria-label="图片比例"]')!.disabled, true);
  }
  await act(async () => button('生成图片').click());
  await flush();
  const start = requests.find((r) => r.path.endsWith('/media-tasks'))!;
  assert.deepEqual(start.body.options, { resolution: '4K' });
  assert.equal(start.body.imageType, 'closeup_three_view');
  assert.equal(start.body.aspectRatio, '16:9');
});

async function selectImageType(label: string) {
  const trigger = document.querySelector<HTMLButtonElement>('button[aria-label="生图类型"]')!;
  await act(async () => {
    trigger.focus();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
  });
  await flush();
  const item = [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')].find(node => node.getAttribute('aria-label') === label)!;
  assert.ok(item, label);
  await act(async () => item.click());
  await flush();
}

async function selectParameter(label: string, value: string) {
  const trigger = document.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!;
  assert.ok(trigger);
  await act(async () => {
    trigger.focus();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
  });
  await flush();
  if (label === '时长') {
    await enterSeconds(value.replace(' 秒', ''));
    await closeParameter();
    return;
  }
  const option = [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')].find(
    (node) => node.textContent?.trim() === value,
  )!;
  assert.ok(option, value);
  await act(async () => option.click());
  await flush();
}
async function enterSeconds(value: string) {
  const input = document.querySelector<HTMLInputElement>('input[aria-label="视频生成秒数"]')!;
  assert.ok(input);
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
}
async function closeParameter() {
  await act(async () =>
    document.activeElement!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    ),
  );
  await flush();
}
const latestQuote = () =>
  requests.filter((request) => request.path.endsWith('/quote')).at(-1)!.body;
test('image output menus keep the highest three real tiers and show Seedream Lite as 2K/3K/4K', async () => {
  if (root) await act(async () => root!.unmount());
  const profile: MediaProfile = {
    ...mockVideoProfile(),
    resolutions: ['512', '1K', '1.5K', '2K', '3K', '4K'].map((id) => ({ id, label: id })),
    defaultResolution: '2K',
    durations: undefined,
    defaultDuration: undefined,
  };
  assert.deepEqual(selectMediaOptions('image', profile, { resolution: '1K' }), {
    resolution: '4K',
  });
  let chosen = '';
  function ImageSettings() {
    const [value, setValue] = useState(selectMediaOptions('image', profile));
    return (
      <MediaSettings
        kind="image"
        profile={profile}
        value={value}
        disabled={false}
        onChange={(next) => {
          chosen = next.resolution;
          setValue(next);
        }}
      />
    );
  }
  root = createRoot(document.getElementById('root')!);
  await act(async () => root!.render(<ImageSettings />));
  const trigger = document.querySelector<HTMLButtonElement>('button[aria-label="分辨率"]')!;
  await act(async () => {
    trigger.focus();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
  });
  await flush();
  assert.deepEqual(
    [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')].map((node) =>
      node.textContent?.trim(),
    ),
    ['2K', '3K', '4K'],
  );
  await act(async () =>
    [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')]
      .find((node) => node.textContent?.trim() === '4K')!
      .click(),
  );
  await flush();
  assert.equal(chosen, '4K');
  const lite = { ...profile, resolutions: ['2K', '3K', '4K'].map((id) => ({ id, label: id })) };
  assert.deepEqual(selectMediaOptions('image', lite), { resolution: '4K' });
  const one = { ...profile, resolutions: [{ id: '1K', label: '1K' }], defaultResolution: '1K' };
  assert.deepEqual(selectMediaOptions('image', one), { resolution: '1K' });
  const sizes = {
    ...profile,
    resolutions: ['1024x1024', '1536x1024', '1024x1536', '2048x2048'].map((id) => ({
      id,
      label: id,
    })),
    defaultResolution: '1024x1024',
  };
  assert.deepEqual(selectMediaOptions('image', sizes), { resolution: '2048x2048' });
});
test('Mock video choices follow the model profile and quote the exact selected integer seconds and resolution', async () => {
  await mount('normal', 'video');
  assert.deepEqual(latestQuote().options, { resolution: '720p', durationSeconds: 5 });
  const resolution = document.querySelector<HTMLButtonElement>('button[aria-label="分辨率"]')!;
  assert.equal(resolution.textContent, '720P');
  await act(async () => {
    resolution.focus();
    resolution.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
  });
  await flush();
  assert.deepEqual(
    [...document.querySelectorAll('[role=menuitemradio]')].map((node) => node.textContent?.trim()),
    ['480P', '720P', '1080P'],
  );
  await closeParameter();
  await act(async () => button('时长').click());
  await flush();
  const slider = document.querySelector<HTMLInputElement>('input[type=range]')!;
  assert.equal(slider.min, '1');
  assert.equal(slider.max, '10');
  assert.equal(slider.value, '5');
  assert.deepEqual(
    [...document.querySelectorAll('.media-duration-ticks>span')].map((node) => node.textContent),
    Array.from({ length: 10 }, (_, i) => String(i + 1)),
  );
  await closeParameter();
  assert.equal(document.activeElement, button('时长'));
  await selectParameter('分辨率', '480P');
  await selectParameter('时长', '1 秒');
  assert.deepEqual(latestQuote().options, { resolution: '480p', durationSeconds: 1 });
  assert.equal(button('生成视频').querySelector('.media-button-price')!.textContent, '2');
  await selectParameter('分辨率', '1080P');
  await selectParameter('时长', '10 秒');
  assert.equal(button('生成视频').querySelector('.media-button-price')!.textContent, '100');
  assert.doesNotMatch(document.querySelector('.media-prompt-actions')!.textContent!, /×|＝|积分\//);
  assert.equal(requests.filter((request) => request.path.endsWith('/media-tasks')).length, 0);
  const originalQuote = latestQuote();
  await act(async () => button('生成视频').click());
  await flush();
  const start = requests.find((request) => request.path.endsWith('/media-tasks'))!;
  assert.deepEqual(start.body.options, originalQuote.options);
  assert.equal(start.body.billingBasis, 'video-' + JSON.stringify(originalQuote.options));
  assert.equal('amount' in start.body, false);
  assert.equal('delaySeconds' in start.body, false);
  const quoteCount = requests.filter((request) => request.path.endsWith('/quote')).length;
  videoProfile = { ...videoProfile, durations: [3], defaultDuration: 3 };
  await act(async () => workspace.refreshModels());
  await flush();
  assert.equal(document.querySelector('[aria-label="时长"]')?.textContent, '10 秒');
  assert.equal(document.querySelector('button[aria-label="时长"]'), null);
  assert.equal(button('生成视频').disabled, true);
  assert.equal(requests.filter((request) => request.path.endsWith('/quote')).length, quoteCount);
  fixture.mediaTasks[0].options = {};
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.equal(document.querySelector('[aria-label="视频参数"]')?.textContent, '媒体规格尚未核实');
  assert.equal(requests.filter((request) => request.path.endsWith('/quote')).length, quoteCount);
});
test('switching models uses each models own valid parameters and capability changes invalidate stale selections', async () => {
  await mount('normal', 'video');
  await selectParameter('分辨率', '480P');
  await selectParameter('时长', '1 秒');
  const switchModel = async (modelId: string) => {
    await act(async () => {
      const next = {
        ...workspace.currentProject!,
        revision: workspace.currentProject!.revision + 1,
        settings: { ...workspace.currentProject!.settings!, videoModel: modelId },
      };
      fixture.project.revision = next.revision;
      fixture.project.settings = next.settings;
      workspace.acceptProject(next);
    });
    await flush();
  };
  await switchModel('mock_alternate');
  assert.deepEqual(latestQuote().options, { resolution: '1080p', durationSeconds: 6 });
  assert.equal(document.querySelector('[aria-label="时长"]')?.textContent, '6 秒');
  assert.equal(button('生成视频').disabled, false);
  assert.doesNotMatch(document.body.textContent || '', /媒体报价与当前模型或项目不一致/);
  const trigger = document.querySelector<HTMLButtonElement>('button[aria-label="时长"]')!;
  await act(async () => {
    trigger.focus();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
  });
  await flush();
  assert.deepEqual(
    [...document.querySelectorAll('.media-duration-ticks>span')].map((node) => node.textContent),
    ['2', '6'],
  );
  const quoteCount = requests.length;
  await enterSeconds('3');
  assert.equal(requests.length, quoteCount);
  assert.equal(
    document.querySelector('input[aria-label="视频生成秒数"]')?.getAttribute('aria-invalid'),
    'true',
  );
  await act(async () =>
    document
      .querySelector('input[type=range]')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true })),
  );
  await flush();
  assert.equal(latestQuote().options.durationSeconds, 2);
  assert.equal(
    document.querySelector<HTMLInputElement>('input[aria-label="视频生成秒数"]')!.value,
    '2',
  );
  await act(async () => button('增加时长').click());
  await flush();
  assert.equal(latestQuote().options.durationSeconds, 6);
  await closeParameter();
  await switchModel('mock_video');
  assert.deepEqual(latestQuote().options, { resolution: '480p', durationSeconds: 1 });
  videoProfile = {
    ...videoProfile,
    resolutions: [{ id: '720p', label: '720P' }],
    defaultResolution: '720p',
    durations: [3],
    defaultDuration: 3,
  };
  await act(async () => workspace.refreshModels());
  await flush();
  assert.deepEqual(latestQuote().options, { resolution: '720p', durationSeconds: 3 });
  assert.equal(document.querySelector<HTMLButtonElement>('[aria-label="分辨率"]')?.disabled, true);
  assert.equal(document.querySelector<HTMLButtonElement>('[aria-label="时长"]')?.disabled, true);
  assert.equal(requests.filter((request) => request.path.endsWith('/media-tasks')).length, 0);
});
test('duration slider and numeric input share quoted seconds and reject empty, fractional and out-of-range drafts', async () => {
  await mount('normal', 'video');
  await act(async () => button('时长').click());
  await flush();
  const slider = document.querySelector<HTMLInputElement>('input[type=range]')!,
    number = document.querySelector<HTMLInputElement>('input[aria-label="视频生成秒数"]')!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(slider, '6');
    slider.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
  assert.equal(number.value, '6');
  assert.equal(latestQuote().options.durationSeconds, 6);
  assert.equal(button('生成视频').querySelector('.media-button-price')!.textContent, '24');
  const quoteCount = requests.length;
  for (const invalid of ['30', '0', '3.5', '']) {
    await enterSeconds(invalid);
    assert.equal(latestQuote().options.durationSeconds, 6);
    assert.equal(requests.length, quoteCount);
  }
  await act(async () => {
    number.focus();
    number.blur();
  });
  assert.equal(number.value, '6');
  await act(async () =>
    number.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })),
  );
  assert.equal(requests.length, quoteCount);
  assert.equal(number.value, '6');
  await act(async () =>
    slider.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true })),
  );
  await flush();
  assert.equal(number.value, '1');
  assert.equal(latestQuote().options.durationSeconds, 1);
  assert.equal(
    document
      .querySelector<HTMLElement>('.media-duration-scale')!
      .style.getPropertyValue('--duration-fill'),
    '0%',
  );
  assert.equal(button('减少时长').disabled, true);
  await act(async () =>
    slider.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true })),
  );
  await flush();
  assert.equal(number.value, '10');
  assert.equal(latestQuote().options.durationSeconds, 10);
  assert.equal(
    document
      .querySelector<HTMLElement>('.media-duration-scale')!
      .style.getPropertyValue('--duration-fill'),
    '100%',
  );
  assert.equal(button('增加时长').disabled, true);
  await closeParameter();
  assert.equal(requests.filter((request) => request.path.endsWith('/media-tasks')).length, 0);
});
test('unconfirmed video commands retain their original selected parameters and quote after model changes', async () => {
  await mount('unknown', 'video');
  await selectParameter('分辨率', '480P');
  await selectParameter('时长', '1 秒');
  await act(async () => button('生成视频').click());
  await flush();
  await act(async () =>
    workspace.acceptProject({
      ...workspace.currentProject!,
      revision: workspace.currentProject!.revision + 1,
      settings: { ...workspace.currentProject!.settings!, videoModel: 'mock_alternate' },
    }),
  );
  await flush();
  assert.equal(document.querySelector('[aria-label="分辨率"]')?.textContent, '480P');
  assert.equal(document.querySelector('[aria-label="时长"]')?.textContent, '1 秒');
  assert.equal(document.querySelector('button[aria-label="时长"]'), null);
  assert.equal(document.querySelector('.media-warning'), null);
  assert.equal(button('核对原命令'), undefined);
  assert.equal(button('重试').disabled, false);
  await act(async () => button('重试').click());
  await flush();
  const starts = requests.filter((request) => request.path.endsWith('/media-tasks'));
  assert.equal(starts.length, 2);
  assert.deepEqual(starts[1].body, starts[0].body);
  assert.deepEqual(starts[1].body.options, { resolution: '480p', durationSeconds: 1 });
  assert.equal(fixture.mediaTasks.length, 1);
});
test('submitting a video stays busy without inserting a pending command banner', async () => {
  await mount('late-conflict', 'video');
  await act(async () => button('生成视频').click());
  await flush();
  assert.equal(button('生成视频').disabled, true);
  assert.equal(document.querySelector('.media-warning'), null);
  assert.doesNotMatch(document.body.textContent!, /上次受理尚未确认|核对原命令|已加入生成队列/);
  assert.equal(requests.filter((r) => r.path.endsWith('/media-tasks')).length, 1);
  await act(async () => releaseStart(Response.json({ code: 'INTERNAL', message: '网络响应未确认' }, { status: 500 })));
  await flush();
  assert.equal(button('重试').disabled, false);
  assert.equal(document.querySelector('.media-warning'), null);
});
test('video retry uses the original command even when the current prompt or model cannot start a fresh generation', async () => {
  await mount('unknown', 'video');
  await act(async () => button('生成视频').click());
  await flush();
  await act(async () => {
    changeGenerationDisabled(true);
    workspace.acceptProject({
      ...workspace.currentProject!,
      revision: workspace.currentProject!.revision + 1,
      settings: { ...workspace.currentProject!.settings!, videoModel: 'unavailable-model' },
    });
  });
  await flush();
  assert.equal(button('重试').disabled, false);
  await act(async () => button('重试').click());
  await flush();
  const starts = requests.filter((r) => r.path.endsWith('/media-tasks'));
  assert.equal(starts.length, 2);
  assert.deepEqual(starts[1].body, starts[0].body);
  assert.equal(fixture.mediaTasks.length, 1);
});
test('missing or invalid model defaults block selection instead of inventing supported sizes or seconds', () => {
  const profile = mockVideoProfile();
  assert.equal(selectMediaOptions('video', null), undefined);
  assert.equal(selectMediaOptions('video', { ...profile, defaultResolution: '4K' }), undefined);
  assert.equal(selectMediaOptions('video', { ...profile, defaultDuration: 30 }), undefined);
  assert.equal(selectMediaOptions('video', { ...profile, retired: true }), undefined);
  assert.deepEqual(
    selectMediaOptions('image', profile, { resolution: '480p', durationSeconds: 10 }),
    { resolution: '480p' },
  );
  assert.deepEqual(
    selectMediaOptions('video', profile, { resolution: '4K', durationSeconds: 30 }),
    { resolution: '720p', durationSeconds: 5 },
  );
});
test('estimated shot duration selects the shortest covering provider tier and keeps an explicit user choice', () => {
  const profile = { ...mockVideoProfile(), durations: [10, 4, 6], defaultDuration: 10 };
  assert.equal(selectMediaOptions('video', profile, undefined, 5.5)?.durationSeconds, 6);
  assert.equal(selectMediaOptions('video', profile, undefined, 6)?.durationSeconds, 6);
  assert.equal(selectMediaOptions('video', profile, undefined, 10.01), undefined);
  assert.equal(selectMediaOptions('video', profile, undefined, null)?.durationSeconds, 10);
  assert.equal(selectMediaOptions('video', profile, { resolution: '720p', durationSeconds: 4 }, 5.5)?.durationSeconds, 4);
});
test('a delayed quote for previous video parameters cannot replace or submit the latest selected parameters', async () => {
  await mount('delayed-quote', 'video');
  await selectParameter('分辨率', '480P');
  assert.equal(button('生成视频').disabled, false);
  assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '10');
  await selectParameter('时长', '1 秒');
  assert.equal(button('生成视频').querySelector('.media-button-price')!.textContent, '2');
  await act(async () => releaseQuote());
  await flush();
  assert.equal(button('生成视频').querySelector('.media-button-price')!.textContent, '2');
  assert.doesNotMatch(document.body.textContent || '', /5 秒 × 2 积分\/秒＝10 积分/);
  await act(async () => button('生成视频').click());
  await flush();
  const start = requests.find((request) => request.path.endsWith('/media-tasks'))!;
  assert.deepEqual(start.body.options, { resolution: '480p', durationSeconds: 1 });
  assert.equal(start.body.billingBasis, 'video-' + JSON.stringify(start.body.options));
});

test('video generation shows the server total inside the button, including free zero, and blocks unknown prices', async () => {
  await mount('normal', 'video');
  assert.equal(button('生成视频').querySelector('.media-button-price')!.textContent, '20');
  assert.match(
    button('生成视频').querySelector('.media-button-price')!.getAttribute('aria-label')!,
    /20 积分/,
  );
  assert.equal(document.querySelector('.media-prompt-actions .charge-notice'), null);
  await mount('video-free', 'video');
  // 限免不显示 0 积分，只显示限免标签。
  assert.equal(
    button('生成视频').querySelector('.media-button-price .xy-model-free-badge')!.textContent,
    '限免',
  );
  assert.equal(button('生成视频').querySelector('.media-button-amount'), null);
  assert.doesNotMatch(button('生成视频').querySelector('.media-button-price')!.textContent!, /0/);
  assert.equal(button('生成视频').disabled, false);
  assert.equal(requests.filter((r) => r.path.endsWith('/media-tasks')).length, 0);
  await mount('quote-error', 'video');
  assert.equal(button('生成视频').disabled, true);
  assert.equal(button('生成视频').querySelector('.media-button-price')!.textContent, '20');
  assert.ok(button('重新读取价格'));
  mode = 'normal';
  await act(async () => button('重新读取价格').click());
  await flush();
  assert.equal(button('生成视频').disabled, false);
  assert.equal(button('生成视频').querySelector('.media-button-price')!.textContent, '20');
  assert.equal(requests.filter((r) => r.path.endsWith('/media-tasks')).length, 0);
});

test('disabled video generation retains its current parameter price without enabling or submitting', async () => {
  await mount('normal', 'video');
  assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '20');
  await act(async () => changeGenerationDisabled(true));
  await flush();
  assert.equal(button('生成视频').disabled, true);
  assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '20');
  assert.match(button('生成视频').querySelector('.media-button-price')!.getAttribute('aria-label')!, /预计消耗 20 积分/);
  await act(async () => button('生成视频').click());
  assert.equal(requests.filter(request => request.path.endsWith('/media-tasks')).length, 0);
  await act(async () => changeGenerationDisabled(false));
  await flush();
  assert.equal(button('生成视频').disabled, false);
  assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '20');
  assert.equal(requests.filter(request => request.path.endsWith('/media-tasks')).length, 0);
});

test('switching shots immediately displays current seconds cost while both server quotes are delayed', async () => {
  await mount('held-video-quotes', 'video', 6);
  assert.equal(heldQuotes.length, 1);
  assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '24');
  assert.equal(button('生成视频').disabled, false);
  await act(async () => button('生成视频').click());
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
  await act(async () => {
    estimatedDurationSeconds = 7;
    changeTarget({ episodeId: 'ep_upper', shotId: 'shot_2' });
  });
  await flush();
  assert.equal(heldQuotes.length, 2);
  assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '28');
  assert.equal(button('生成视频').disabled, false);
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '28');
  assert.equal(button('生成视频').disabled, false);
  await act(async () => heldQuotes[1].release());
  await flush();
  assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '28');
  assert.equal(button('生成视频').disabled, false);
  await act(async () => button('生成视频').click());
  await flush();
  const starts = requests.filter(r => r.path.endsWith('/media-tasks'));
  assert.equal(starts.length, 1);
  assert.equal(starts[0].body.target.shotId, 'shot_2');
  assert.equal(starts[0].body.billingBasis, 'video-' + JSON.stringify({ resolution: '720p', durationSeconds: 7 }));
});

test('an unavailable catalog price is pending rather than fake zero and cannot start before confirmation', async () => {
  await mount('missing-video-price', 'video');
  assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '…');
  assert.equal(button('生成视频').disabled, true);
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '20');
  assert.equal(button('生成视频').disabled, false);
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
});

test('clicking a video while pricing is pending waits once and submits only the matching confirmed basis', async () => {
  await mount('held-video-quotes', 'video', 6);
  assert.equal(button('生成视频').disabled, false);
  await act(async () => { button('生成视频').click(); button('生成视频').click(); });
  assert.equal(button('生成视频').disabled, true);
  assert.equal(button('生成视频').getAttribute('aria-busy'), 'true');
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
  await act(async () => heldQuotes[0].release());
  await flush();
  const starts = requests.filter(r => r.path.endsWith('/media-tasks'));
  assert.equal(starts.length, 1);
  assert.equal(starts[0].body.target.shotId, 'shot_1');
  assert.equal(starts[0].body.billingBasis, 'video-' + JSON.stringify({ resolution: '720p', durationSeconds: 6 }));
  assert.equal('amount' in starts[0].body, false);
});

test('a queued media click is dropped if editing disables the target or the quote fails', async () => {
  await mount('held-video-quotes', 'video', 6);
  await act(async () => button('生成视频').click());
  await act(async () => changeGenerationDisabled(true));
  await flush();
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
  assert.equal(button('生成视频').disabled, true);
  await act(async () => changeGenerationDisabled(false));
  await flush();
  assert.equal(button('生成视频').disabled, false);
  await act(async () => heldQuotes[1].release());
  await flush();
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);

  await mount('held-quote-error', 'video', 6);
  await act(async () => button('生成视频').click());
  assert.equal(button('生成视频').getAttribute('aria-busy'), 'true');
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(button('生成视频').getAttribute('aria-busy'), null);
  assert.equal(button('生成视频').disabled, true);
  assert.match(document.body.textContent!, /价格读取失败/);
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
});

test('an image click during pricing requires another explicit click when the confirmed amount changes', async () => {
  await mount('held-image-quotes');
  assert.equal(button('生成图片').querySelector('.media-button-amount')!.textContent, '12');
  await act(async () => button('生成图片').click());
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(button('生成图片').querySelector('.media-button-amount')!.textContent, '5');
  assert.equal(button('生成图片').disabled, false);
  assert.match(document.body.textContent!, /积分价格已更新，请确认后再次生成/);
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
  await act(async () => button('生成图片').click());
  await flush();
  const starts = requests.filter(r => r.path.endsWith('/media-tasks'));
  assert.equal(starts.length, 1);
  assert.equal(starts[0].body.billingBasis, 'basis-5');
});

test('unmounting a media panel cancels its pending click without accepting the late quote', async () => {
  await mount('held-video-quotes', 'video', 6);
  await act(async () => button('生成视频').click());
  await act(async () => root!.unmount());
  root = undefined;
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
});

test('a session boundary cancels an unconfirmed media click before its delayed price can start anything', async () => {
  await mount('held-video-quotes', 'video', 6);
  await act(async () => button('生成视频').click());
  await act(async () => { sessionBoundary.reset(); await workspace.refreshModels(); });
  await flush();
  assert.equal(button('生成视频').getAttribute('aria-busy'), null);
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
});

test('a free image click still waits for confirmation and only starts one zero-credit command', async () => {
  await mount('image-free');
  assert.equal(
    button('生成图片').querySelector('.media-button-price .xy-model-free-badge')!.textContent,
    '限免',
  );
  assert.equal(button('生成图片').querySelector('.media-button-amount'), null);
  assert.equal(button('生成图片').disabled, false);
  await act(async () => { button('生成图片').click(); button('生成图片').click(); });
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 1);
  assert.equal(fixture.mediaTasks[0].credit.prepaid, 0);
});

test('single image generation immediately shows the highest-tier estimate and ignores late quotes from another target', async () => {
  await mount('held-image-quotes');
  const amount = () => button('生成图片').querySelector('.media-button-amount')!.textContent;
  assert.equal(amount(), '12');
  assert.match(document.querySelector('.media-button-price')!.getAttribute('aria-label')!, /预计消耗 12 积分/);
  assert.equal(button('生成图片').disabled, false);
  await act(async () => button('生成图片').click());
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
  price = 7;
  await act(async () => changeTarget({ assetId: 'prop_unselected' }));
  await flush();
  assert.equal(heldQuotes.length, 2);
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(amount(), '12');
  assert.equal(button('生成图片').disabled, false);
  await act(async () => heldQuotes[1].release());
  await flush();
  assert.equal(amount(), '7', 'the current server amount replaces the estimate');
  assert.equal(button('生成图片').disabled, false);
  await act(async () => button('生成图片').click());
  await flush();
  const starts = requests.filter(r => r.path.endsWith('/media-tasks'));
  assert.equal(starts.length, 1);
  assert.equal(starts[0].body.target.assetId, 'prop_unselected');
  assert.equal(starts[0].body.billingBasis, 'basis-7');
  assert.equal('amount' in starts[0].body, false);
});

test('disabled or failed image pricing retains the estimate without allowing an unquoted start', async () => {
  await mount('held-image-quotes');
  await act(async () => changeGenerationDisabled(true));
  await flush();
  assert.equal(button('生成图片').querySelector('.media-button-amount')!.textContent, '12');
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(button('生成图片').disabled, true);
  assert.equal(button('生成图片').querySelector('.media-button-amount')!.textContent, '12');
  await mount('quote-error');
  assert.equal(button('生成图片').disabled, true);
  assert.equal(button('生成图片').querySelector('.media-button-amount')!.textContent, '12');
  assert.ok(button('重新读取价格'));
  mode = 'normal';
  await act(async () => button('重新读取价格').click());
  await flush();
  assert.equal(button('生成图片').disabled, false);
  assert.equal(button('生成图片').querySelector('.media-button-amount')!.textContent, '5');
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
});

test('image pricing distinguishes missing catalog prices from real free zero and still waits for confirmation', async () => {
  const amount = () => button('生成图片').querySelector('.media-button-amount')?.textContent;
  const freeBadge = () =>
    button('生成图片').querySelector('.media-button-price .xy-model-free-badge')?.textContent;
  await mount('missing-image-price');
  assert.equal(amount(), '…');
  assert.equal(freeBadge(), undefined);
  assert.equal(button('生成图片').disabled, true);
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(amount(), '5');
  assert.equal(button('生成图片').disabled, false);
  await mount('image-free');
  // 真实限免只显示标签，不显示 0 积分，同时仍需报价确认后才提交。
  assert.equal(freeBadge(), '限免');
  assert.equal(amount(), undefined);
  assert.equal(button('生成图片').disabled, false);
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
  await act(async () => heldQuotes[0].release());
  await flush();
  assert.equal(freeBadge(), '限免');
  assert.equal(amount(), undefined);
  assert.equal(button('生成图片').disabled, false);
});

test('an existing shot video shows regenerate before its history loads and an empty shot keeps generate', async () => {
  await mount('existing-video', 'video');
  assert.ok(button('重新生成'));
  assert.ok(button('选择视频'));
  assert.equal(button('生成视频'), undefined);
  assert.equal(button('重新生成').querySelector('.media-button-amount')!.textContent, '20');
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
  await act(async () => releaseHistory());
  await flush();
  assert.ok(button('重新生成'));
  await act(async () => changeTarget({ episodeId: 'ep_upper', shotId: 'shot_2' }));
  await flush();
  assert.ok(button('生成视频'));
  assert.equal(button('重新生成'), undefined);
  assert.equal(button('选择视频'), undefined);
  await act(async () => releaseHistory());
  await flush();
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 0);
});

test('a newly completed video changes generate to regenerate without automatically starting again', async () => {
  await mount('normal', 'video');
  await act(async () => button('生成视频').click());
  await flush();
  fixture.completeMedia();
  const task = fixture.mediaTasks[0];
  task.credit = { ...task.credit, settlement: 'settled', charged: task.credit.prepaid };
  await act(async () => window.dispatchEvent(new Event('focus')));
  await flush();
  assert.ok(button('重新生成'));
  assert.equal(button('生成视频'), undefined);
  assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 1);
});

test('failed or cancelled video without a successful output keeps generate and its current confirmed price', async () => {
  for (const status of ['failed', 'cancelled']) {
    await mount('normal', 'video');
    await act(async () => button('生成视频').click());
    await flush();
    fixture.completeMedia(status);
    await act(async () => window.dispatchEvent(new Event('focus')));
    await flush();
    assert.ok(button('生成视频'));
    assert.equal(button('重新生成'), undefined);
    assert.equal(button('选择视频'), undefined);
    assert.equal(button('生成视频').querySelector('.media-button-amount')!.textContent, '20');
    assert.equal(button('生成视频').disabled, false);
    assert.equal(requests.filter(r => r.path.endsWith('/media-tasks')).length, 1);
    await act(async () => button('生成视频').click());
    await flush();
    const starts = requests.filter(r => r.path.endsWith('/media-tasks'));
    assert.equal(starts.length, 2);
    assert.equal(starts[1].body.billingBasis, 'video-' + JSON.stringify(starts[1].body.options));
  }
});

test('shot estimate drives the displayed duration, server quote and one submitted media command', async () => {
  await mount('normal', 'video', 5.5);
  assert.equal(button('时长').textContent?.trim(), '6 秒');
  assert.equal(latestQuote().options.durationSeconds, 6);
  await act(() => button('生成视频').click());
  await flush();
  const starts = requests.filter(r => r.path.endsWith('/media-tasks'));
  assert.equal(starts.length, 1);
  assert.equal(starts[0].body.options.durationSeconds, 6);
  assert.equal(fixture.mediaTasks[0].options.durationSeconds, 6);
});
