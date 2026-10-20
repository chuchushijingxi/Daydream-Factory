// 明确隔离的免费 HTTP/UI 夹具，无数据库、凭证、真实供应商或外发端口。
export function createStoryboardFixture() {
  const copy = (v) => structuredClone(v),
    requests = [],
    mediaTasks = [];
  let mediaCounter = 0;
  const mediaModels = {
    image: [
      {
        id: 'mock-image',
        name: 'Mock Image',
        provider: 'mock',
        creditPrice: {
          unitPrice: 5,
          resolutionPrices: { '2K': 8, '4K': 12 },
          freeEnabled: false,
          freeStart: null,
          freeEnd: null,
        },
        mediaProfile: {
          resolutions: [
            { id: '1K', label: '1K' },
            { id: '2K', label: '2K' },
            { id: '4K', label: '4K' },
          ],
          defaultResolution: '1K',
          aspectRatios: ['16:9'],
          supplierPrice: '本地免费',
          sources: [],
          note: '免费隔离夹具',
          checkedAt: '2026-10-17',
          retired: false,
        },
      },
    ],
    video: [
      {
        id: 'mock_video',
        name: 'Mock Video',
        provider: 'mock',
        mediaProfile: {
          resolutions: [
            { id: '480p', label: '480P', pixels: { '16:9': '854×480' } },
            { id: '720p', label: '720P', pixels: { '16:9': '1280×720' } },
            { id: '1080p', label: '1080P', pixels: { '16:9': '1920×1080' } },
          ],
          defaultResolution: '1080p',
          durations: Array.from({ length: 10 }, (_, i) => i + 1),
          defaultDuration: 10,
          aspectRatios: ['16:9'],
          supplierPrice: '本地免费',
          sources: [],
          note: '免费隔离夹具',
          checkedAt: '2026-10-17',
          retired: false,
        },
      },
    ],
  };
  const text1 = '上集 重逢\n林晚🌧️把铜钥匙放在桌上，对另一位林晚说：“你自己决定。”',
    text2 = '下集 归途\n林晚站在车站，望着远处。';
  const range = (id, text) => ({
    start: { spanId: id, offset: 0 },
    end: { spanId: id, offset: Array.from(text).length },
  });
  const ranges = [range('span_1', text1), range('span_2', text2)];
  const makeAsset = (id, kind, name, description, looks = []) => ({
    id,
    kind,
    name,
    description,
    aliases: [],
    sourceIds: ['span_1'],
    origin: 'user',
    character:
      kind === 'character'
        ? {
            roleType: '主角',
            age: '28',
            gender: '女',
            biography: '虚构测试角色',
            appearance: '黑色短发',
            personality: '沉静',
          }
        : null,
    looks,
  });
  const assets = [
    makeAsset('character_1', 'character', '林晚', '先到客厅的林晚，工程师。', [
      { id: 'look_rain', name: '雨衣', description: '深灰雨衣，衣角有水迹。', origin: 'user' },
    ]),
    makeAsset('character_2', 'character', '林晚', '另一位同名林晚，医生。'),
    makeAsset('location_1', 'location', '客厅', '桌子位于窗边，傍晚的室内。'),
    makeAsset('location_2', 'location', '车站', '清晨站台。'),
    makeAsset('prop_1', 'prop', '铜钥匙', '旧铜钥匙，单枚。'),
    makeAsset('prop_unselected', 'prop', '未选中的怀表', '用于验证全项目资产锁。'),
  ];
  const refs = [
    { assetId: 'character_1', lookId: 'look_rain' },
    { assetId: 'character_2' },
    { assetId: 'location_1' },
    { assetId: 'prop_1' },
  ];
  const parts = [
    { type: 'asset', assetId: 'location_1' },
    { type: 'text', text: '内，' },
    { type: 'asset', ...refs[0] },
    { type: 'text', text: '站在桌边，把' },
    { type: 'asset', assetId: 'prop_1' },
    { type: 'text', text: '放到桌上。\n镜头随' },
    { type: 'asset', ...refs[0] },
    { type: 'text', text: '的手缓慢下移，她对' },
    { type: 'asset', assetId: 'character_2' },
    { type: 'text', text: '说：“你自己决定。”镜头停在钥匙与尚未伸出的手之间。' },
  ];
  const run = (id, contract, scope) => ({
    id,
    contract,
    sourceVersion: 1,
    status: 'completed',
    stage: scope?.operation === 'index' ? 'episodeIndex' : scope ? 'storyboard' : 'visualPrompt',
    callCount: 0,
    ...(scope ? { storyboardScope: scope } : {}),
  });
  let project = {
    id: 'p5_free',
    title: '免费隔离夹具 · 重逢与归途',
    revision: 8,
    source: { filename: 'fiction.txt', version: 1, sha256: 'fixture', spanCount: 2 },
    assets,
    settings: {
      aspectRatio: '16:9',
      llmModel: 'free-analysis',
      imageModel: '',
      videoModel: 'mock_video',
    },
    style: null,
    outline: null,
    runs: [
      run('index', 'episode-index-agent-v1', { operation: 'index' }),
      {
        ...run('assets', 'asset-preparation-v1'),
        targets: assets.flatMap((a) => [
          { assetId: a.id },
          ...a.looks.map((l) => ({ assetId: a.id, lookId: l.id })),
        ]),
      },
      run('episode_upper', 'storyboard-agent-v1', {
        operation: 'breakdown',
        episodeId: 'ep_upper',
        shotIds: ['shot_1', 'shot_2'],
      }),
      run('episode_lower', 'storyboard-agent-v1', {
        operation: 'breakdown',
        episodeId: 'ep_lower',
        shotIds: ['shot_3'],
      }),
    ],
    visualPrompts: assets.flatMap((a) => [
      {
        target: { assetId: a.id },
        text: a.description,
        sourceVersion: 1,
        runId: 'assets',
        origin: 'user',
        stale: false,
      },
      ...a.looks.map((l) => ({
        target: { assetId: a.id, lookId: l.id },
        text: l.description,
        sourceVersion: 1,
        runId: 'assets',
        origin: 'user',
        stale: false,
      })),
    ]),
    catalogEditedByUser: false,
    usedOrReservedMicroCny: 0,
    limitMicroCny: 0,
    uploadedAt: '2026-10-15T03:00:00Z',
  };
  let episodes = [
    {
      id: 'ep_upper',
      episodeNumber: null,
      title: '上集 重逢',
      sourceRanges: [ranges[0]],
      sceneIds: ['scene_1'],
      origin: 'model',
    },
    {
      id: 'ep_lower',
      episodeNumber: null,
      title: '下集 归途',
      sourceRanges: [ranges[1]],
      sceneIds: ['scene_2'],
      origin: 'model',
    },
  ];
  let scenes = [
    {
      id: 'scene_1',
      episodeId: 'ep_upper',
      title: '客厅 · 钥匙与决定',
      sourceRanges: [ranges[0]],
      locationAssetId: 'location_1',
      time: '傍晚',
      summary: '林晚放下钥匙，把决定留给另一位同名的林晚。',
      shotIds: ['shot_1', 'shot_2'],
      valid: true,
      issues: [],
    },
    {
      id: 'scene_2',
      episodeId: 'ep_lower',
      title: '站台 · 归途',
      sourceRanges: [ranges[1]],
      locationAssetId: 'location_2',
      time: '清晨',
      summary: '林晚站在车站。',
      shotIds: ['shot_3'],
      valid: true,
      issues: [],
    },
  ];
  const makeShot = (id, sceneId, sourceRanges, action) => ({
    id,
    sceneId,
    sourceRanges,
    action,
    dialogues: id === 'shot_1' ? [{ speakerAssetId: 'character_1', text: '你自己决定。' }] : [],
    camera: { size: '中景', angle: '平视', movement: '随手下移' },
    durationSeconds: null,
    designNotes: '结束停在桌面，保留对方尚未伸手的状态。',
    selectedAssets: copy(refs),
    videoPrompt: { parts: copy(parts), plainText: '', origin: 'user', valid: true, issues: [] },
    valid: true,
    issues: [],
  });
  let shots = [
    makeShot('shot_1', 'scene_1', [ranges[0]], '林晚把铜钥匙放在桌上，松开手后停顿。'),
    makeShot('shot_2', 'scene_1', [ranges[0]], '另一位林晚看着钥匙，没有伸手。'),
    makeShot('shot_3', 'scene_2', [ranges[1]], '林晚站在站台，望向远处。'),
  ];
  const savedScenes = copy(scenes),
    savedShots = copy(shots);
  let scenario = 'normal',
    segmentation = 'episodes',
    indexValid = true,
    readOnly = false,
    capability = true,
    counter = 0;
  const label = (r) => {
    const a = assets.find((a) => a.id === r.assetId);
    return (
      a?.name +
      (r.lookId
        ? ' · ' + a?.looks.find((l) => l.id === r.lookId)?.name
        : a?.kind === 'character'
          ? ' · 基础'
          : '')
    );
  };
  const base = () => ({
    projectId: project.id,
    revision: project.revision,
    sourceVersion: 1,
    access: {
      editable: !readOnly && !project.runs.some((r) => r.status === 'running') && indexValid,
      canStart: !readOnly && !project.runs.some((r) => r.status === 'running') && indexValid,
      reason: readOnly
        ? 'SERVICE_READ_ONLY'
        : project.runs.some((r) => r.status === 'running')
          ? 'RUN_ACTIVE'
          : indexValid
            ? ''
            : 'EPISODE_INDEX_NOT_READY',
    },
    activeRun: project.runs.find((r) => r.status === 'running') || null,
  });
  const decorate = (s) => ({
    ...copy(s),
    selectedAssets: s.selectedAssets.map((r) => ({
      ...r,
      label: label(r),
      kind: assets.find((a) => a.id === r.assetId)?.kind,
      available: true,
    })),
    videoPrompt: s.videoPrompt
      ? {
          ...copy(s.videoPrompt),
          parts: s.videoPrompt.parts.map((p) =>
            p.type === 'asset' ? { ...p, label: label(p), available: true } : p,
          ),
          plainText: s.videoPrompt.parts
            .map((p) => (p.type === 'text' ? p.text : '@' + label(p)))
            .join(''),
        }
      : null,
  });
  const index = () => ({
    ...base(),
    episodeCount: indexValid && segmentation === 'episodes' ? episodes.length : null,
    index: {
      sourceVersion: 1,
      segmentation,
      supportRanges: [],
      valid: indexValid,
      issues: indexValid ? [] : [{ code: 'RUN_INCOMPLETE' }],
      episodes: episodes.map((e) => ({
        ...copy(e),
        characterCount: new Set(shots.filter(s => e.sceneIds.includes(s.sceneId))
          .flatMap(s => s.selectedAssets)
          .filter(ref => project.assets.some(a => a.id === ref.assetId && a.kind === 'character'))
          .map(ref => ref.assetId)).size,
        sceneCount: e.sceneIds.length,
        shotCount: shots.filter((s) => e.sceneIds.includes(s.sceneId)).length,
        pendingPromptCount: shots.filter(
          (s) => e.sceneIds.includes(s.sceneId) && !s.videoPrompt?.valid,
        ).length,
        latestRun:
          [...project.runs].reverse().find((r) => r.storyboardScope?.episodeId === e.id) || null,
      })),
    },
    latestIndexRun: copy(project.runs.find((r) => r.id === 'index')),
  });
  const episode = (id) => ({
    ...base(),
    episode: copy(episodes.find((e) => e.id === id)),
    scenes: copy(scenes.filter((s) => s.episodeId === id)),
    shots: shots
      .filter((s) => episodes.find((e) => e.id === id)?.sceneIds.includes(s.sceneId))
      .map((shot) => {
        const allTasks = mediaTasks.filter(
            (t) => t.kind === 'video' && t.target.episodeId === id && t.target.shotId === shot.id,
          ),
          tasks = allTasks.filter(t => t.status === 'succeeded'),
          task = tasks.find((t) => t.adoptedOutputId) || tasks[0],
          file =
            task?.result?.files.find((f) => f.id === task.adoptedOutputId) ||
            task?.result?.files[0];
        return {
          ...decorate(shot),
          ...(allTasks[0] ? { videoTask: { id: allTasks[0].id, status: allTasks[0].status } } : {}),
          ...(file?.thumbnailUrl ? { thumbnailUrl: file.thumbnailUrl } : {}),
          ...(file?.durationSeconds > 0 ? { previewDurationSeconds: file.durationSeconds } : {}),
        };
      }),
    index: { valid: indexValid, issues: [], supportRanges: [] },
    latestRun: copy(
      [...project.runs].reverse().find((r) => r.storyboardScope?.episodeId === id) || null,
    ),
  });
  const fail = (code, message, status = 409) => Response.json({ code, message }, { status });
  // UI-only parsing scenarios. These never call a model or replace the real Go
  // integration tests; they let browser checks observe every persisted stage.
  function parsingStage(stage) {
    project.runs = project.runs.filter(
      (r) => r.contract === 'episode-index-agent-v1' || r.id === 'parse-assets',
    );
    if (stage === 'ready') {
      project.runs = project.runs.filter((r) => r.id !== 'parse-assets');
      project.assets = [];
      project.visualPrompts = [];
      scenes = [];
      shots = [];
      episodes.forEach((e) => {
        e.sceneIds = [];
      });
    } else {
      let r = project.runs.find((r) => r.id === 'parse-assets');
      if (!r) {
        r = {
          id: 'parse-assets',
          contract: 'asset-preparation-v1',
          sourceVersion: 1,
          status: 'running',
          stage: 'extract',
          callCount: 1,
        };
        project.runs.push(r);
      }
      r.stage = stage === 'complete' ? 'visualPrompt' : stage;
      r.status = stage === 'complete' ? 'completed' : 'running';
      if (['visualPrompt', 'complete'].includes(stage)) {
        project.assets = copy(assets);
        r.targets = assets.flatMap((a) => [
          { assetId: a.id },
          ...a.looks.map((l) => ({ assetId: a.id, lookId: l.id })),
        ]);
      }
      if (stage === 'complete')
        project.visualPrompts = r.targets.map((target) => ({
          target,
          text: '免费隔离夹具的完整视觉正文',
          stale: false,
          sourceVersion: 1,
          runId: r.id,
        }));
    }
    project.revision++;
  }
  function startMedia(body, fillMissingImage = false) {
      const old = mediaTasks.find((t) => t.commandId === body.commandId);
      if (old) return Response.json(copy(old), { status: 202 });
      if (body.revision !== project.revision) return fail('CONFLICT', '项目已更新');
      const kind = body.target.assetId ? 'image' : 'video';
      const t = {
        id: 'task_media_' + ++mediaCounter,
        commandId: body.commandId,
        projectId: project.id,
        scopeId: 'project:' + project.id,
        kind,
        fillMissingImage,
        options: copy(body.options),
        target: copy(body.target),
        inputRevision: body.revision,
        sourceVersion: 1,
        stale: false,
        adoptedOutputId: '',
        credit: null,
        revision: 1,
        modelId: body.modelId,
        operation: kind + '.generate',
        status: 'queued',
        providerTaskId: null,
        providerStatus: null,
        nextRunAt: null,
        result: null,
        errorCode: null,
        error: null,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        billing: { status: 'known', knownMicroCny: 0, reservedMicroCny: 0 },
      };
      mediaTasks.unshift(t);
      return Response.json(copy(t), { status: 202 });
  }
  async function fetcher(url, options = {}) {
    const path = new URL(url, 'http://fixture.invalid').pathname,
      body =
        options.body && typeof options.body === 'string' ? JSON.parse(options.body) : options.body,
      method = options.method || 'GET';
    requests.push({ path, method, body: copy(body) });
    if (path === '/api/status')
      return Response.json({
        readOnly,
        configured: true,
        capabilities: {
          projectMedia: true,
          storyboard: capability,
          assetEditing: true,
          assetImages: true,
          characterLooks: true,
          visualPrompts: true,
          singleVisualPrompts: true,
          projectSettings: true,
        },
      });
    if (path === '/api/models')
      return Response.json({
        analysis: [{ id: 'free-analysis', name: '隔离免费假供应商' }],
        ...copy(mediaModels),
      });
    if (path === '/api/visual-styles') return Response.json({ styles: [] });
    if (path === '/api/projects' && method === 'GET')
      return Response.json([
        {
          id: project.id,
          title: project.title,
          filename: 'fiction.txt',
          revision: project.revision,
          uploadedAt: project.uploadedAt,
          latestRun: copy(project.runs.at(-1) || null),
          previewImages: project.assets.filter(a => a.kind === 'character' && a.image)
            .slice(0, 3).map(a => a.image.thumbnailUrl),
        },
      ]);
    if (path === '/api/projects/p5_free' && method === 'GET') return Response.json(copy(project));
    if (path.endsWith('/source') && !path.includes('/storyboard/'))
      return Response.json({
        version: 1,
        filename: 'fiction.txt',
        spans: [
          { id: 'span_1', text: text1 },
          { id: 'span_2', text: text2 },
        ],
      });
    if (path.endsWith('/calls')) return Response.json([]);
    if (path.endsWith('/storyboard')) return Response.json(index());
    const match = path.match(/\/storyboard\/episodes\/([^/]+)(?:\/(source|runs))?$/);
    if (match && method === 'GET') {
      const id = match[1],
        snapshot =
          match[2] === 'source'
            ? {
                ...base(),
                episodeId: id,
                ranges: copy(episodes.find((e) => e.id === id).sourceRanges),
                fragments: [
                  {
                    spanId: id === 'ep_upper' ? 'span_1' : 'span_2',
                    startOffset: 0,
                    endOffset: Array.from(id === 'ep_upper' ? text1 : text2).length,
                    text: id === 'ep_upper' ? text1 : text2,
                  },
                ],
                supportRanges: [],
              }
            : episode(id);
      if (scenario === 'late' && id === 'ep_upper') await new Promise((r) => setTimeout(r, 700));
      return Response.json(snapshot);
    }
    if (path === '/api/projects/p5_free/media-tasks' && method === 'GET')
      return Response.json({ items: copy(mediaTasks), nextCursor: null });
    if (path === '/api/projects/p5_free/quote' && method === 'POST' && body.operation === 'media') {
      const amount = ({ '1K': 5, '2K': 8, '4K': 12 })[body.options?.resolution] || 5;
      return Response.json({ basis: 'fixture-' + JSON.stringify(body), sourceVersion: 1,
        inputRevision: body.revision, modelId: body.modelId, connectionId: 'mock', scope: 'media-image',
        quantity: 1, unit: 'image', priceVersion: 1,
        rule: { unitPrice: amount, freeEnabled: false, freeStart: null, freeEnd: null },
        originalAmount: amount, amount, free: false, mediaOptions: body.options });
    }
    if (path === '/api/projects/p5_free/media-tasks/batch-images' && method === 'POST') {
      const items = [];
      for (const command of body.commands) {
        const asset = project.assets.find(a => a.id === command.target.assetId);
        const target = command.target.lookId ? asset?.looks.find(l => l.id === command.target.lookId) : asset;
        const old = mediaTasks.find(t => t.commandId === command.commandId);
        if (!old && target?.image) {
          items.push({ commandId: command.commandId, target: command.target, task: null,
            error: { code: 'ASSET_IMAGE_EXISTS', message: '资产已有图片' } });
          continue;
        }
        const response = target ? startMedia(command, true) : fail('NOT_FOUND', '目标不存在', 404);
        const value = await response.json();
        items.push({ commandId: command.commandId, target: command.target,
          task: response.status === 202 ? value : null, error: response.status === 202 ? null : value });
      }
      return Response.json({ items }, { status: 202 });
    }
    if (path === '/api/projects/p5_free/media-tasks' && method === 'POST') return startMedia(body);
    const mediaMatch = path.match(/\/media-tasks\/([^/]+)(?:\/(cancel|adopt))?$/);
    if (mediaMatch) {
      const t = mediaTasks.find((t) => t.id === mediaMatch[1]);
      if (!t) return fail('NOT_FOUND', 'missing', 404);
      if (method === 'GET') return Response.json(copy(t));
      if (mediaMatch[2] === 'cancel') {
        t.status = 'cancelled';
        t.revision++;
        return Response.json(copy(t));
      }
      if (mediaMatch[2] === 'adopt') {
        if (body.revision !== project.revision) return fail('CONFLICT', 'project changed');
        if (t.status !== 'succeeded' || t.stale) return fail('MEDIA_OUTPUT_STALE', 'stale');
        mediaTasks
          .filter((v) => JSON.stringify(v.target) === JSON.stringify(t.target))
          .forEach((v) => (v.adoptedOutputId = ''));
        t.adoptedOutputId = body.outputId;
        if (t.kind === 'image') {
          const a = project.assets.find((a) => a.id === t.target.assetId);
          const target = t.target.lookId ? a.looks.find((l) => l.id === t.target.lookId) : a;
          target.image = {
            fileId: 'file_' + t.id,
            thumbnailUrl: '/api/files/file_' + t.id + '/thumbnail',
            originalUrl: '/api/files/file_' + t.id + '/original',
          };
        }
        project.revision++;
        return Response.json(copy(project));
      }
    }
    if (method === 'POST') {
      if (readOnly) return fail('SERVICE_READ_ONLY', '夹具服务只读', 403);
      if (body.revision !== project.revision) return fail('CONFLICT', '项目已更新，请回读核对。');
      if (path === '/api/projects/p5_free/runs') {
        parsingStage('extract');
        return Response.json(copy(project), { status: 202 });
      }
      if (path.endsWith('/storyboard/changes')) {
        if (scenario === 'conflict') {
          scenario = 'normal';
          project.revision++;
          return fail('CONFLICT', '受控冲突：请保留草稿并核对。');
        }
        if (scenario === 'unknown') {
          scenario = 'normal';
          throw new Error('受控网络结果不明');
        }
        if (project.runs.some((r) => r.status === 'running'))
          return fail('RUN_ACTIVE', '任务进行中');
        const changes = [];
        for (const op of body.operations) {
          if (op.op === 'updateShot') {
            const s = shots.find((s) => s.id === op.shotId),
              before = copy(s),
              next = { ...s, ...copy(op.patch) };
            for (const r of next.selectedAssets)
              if (Object.keys(r).some((k) => !['assetId', 'lookId'].includes(k)))
                return fail('INVALID_INPUT', '选用关系包含只读字段', 400);
            for (const part of next.videoPrompt?.parts || [])
              if (
                part.type === 'asset' &&
                !next.selectedAssets.some(
                  (r) => r.assetId === part.assetId && r.lookId === part.lookId,
                )
              )
                return fail('INVALID_INPUT', '选择与正文引用不一致', 400);
            Object.assign(s, next);
            if (op.patch.videoPrompt)
              s.videoPrompt = { ...s.videoPrompt, origin: 'user', valid: true, issues: [] };
            else if (s.videoPrompt)
              s.videoPrompt = {
                ...s.videoPrompt,
                valid: false,
                issues: [{ code: 'SHOT_INPUT_CHANGED' }],
              };
            changes.push({ entityType: 'shot', id: s.id, before, after: copy(s) });
          }
          if (op.op === 'setShotOrder') {
            const s = scenes.find((s) => s.id === op.sceneId),
              before = copy(s);
            s.shotIds = op.shotIds;
            changes.push({ entityType: 'scene', id: s.id, before, after: copy(s) });
          }
          if (op.op === 'setSceneOrder') {
            episodes.find((e) => e.id === op.episodeId).sceneIds = op.sceneIds;
          }
          if (op.op === 'createShot') {
            const ids = scenes.find(c => c.id === op.sceneId).shotIds;
            const anchor = op.beforeShotId || op.afterShotId;
            if ((op.beforeShotId && op.afterShotId) || (anchor && !ids.includes(anchor)))
              return fail('INVALID_INPUT', '插入位置无效', 400);
            const s = {
              ...makeShot('shot_new_' + ++counter, op.sceneId, op.value.sourceRanges, ''),
              ...op.value,
            };
            shots.push(s);
            ids.splice(op.beforeShotId ? ids.indexOf(op.beforeShotId)
              : op.afterShotId ? ids.indexOf(op.afterShotId) + 1 : ids.length, 0, s.id);
            changes.push({ entityType: 'shot', id: s.id, before: null, after: copy(s) });
          }
          if (op.op === 'createScene') {
            const s = {
              id: 'scene_new_' + ++counter,
              episodeId: op.episodeId,
              ...op.value,
              shotIds: [],
              valid: true,
              issues: [],
            };
            scenes.push(s);
            episodes.find((e) => e.id === s.episodeId).sceneIds.push(s.id);
            changes.push({ entityType: 'scene', id: s.id, before: null, after: copy(s) });
          }
          if (op.op === 'updateScene') {
            const s = scenes.find((s) => s.id === op.sceneId),
              before = copy(s);
            Object.assign(s, op.patch);
            changes.push({ entityType: 'scene', id: s.id, before, after: copy(s) });
          }
          if (op.op === 'deleteShot') {
            const s = shots.find((s) => s.id === op.shotId);
            shots = shots.filter((s) => s.id !== op.shotId);
            const parent = scenes.find((c) => c.id === s.sceneId);
            parent.shotIds = parent.shotIds.filter((id) => id !== op.shotId);
            changes.push({ entityType: 'shot', id: s.id, before: s, after: null });
          }
          if (op.op === 'deleteScene') {
            const s = scenes.find((s) => s.id === op.sceneId);
            scenes = scenes.filter((s) => s.id !== op.sceneId);
            shots = shots.filter((s) => s.sceneId !== op.sceneId);
            episodes.find((e) => e.id === s.episodeId).sceneIds = episodes
              .find((e) => e.id === s.episodeId)
              .sceneIds.filter((id) => id !== s.id);
            changes.push({ entityType: 'scene', id: s.id, before: s, after: null });
          }
        }
        project.revision++;
        return Response.json({
          projectId: project.id,
          revision: project.revision,
          changes,
          invalidated: [],
        });
      }
      if (match && match[2] === 'runs') {
        const r = {
          ...run('run_' + ++counter, 'storyboard-agent-v1', {
            operation: body.operation,
            episodeId: match[1],
            shotIds: body.shotIds || episode(match[1]).shots.map((s) => s.id),
            ...(body.operation === 'breakdown' ? { breakdownMode: body.breakdownMode || 'general',
              videoConstraint: { modelId: 'mock_video', modelName: 'Mock Video', maxDurationSeconds: 10 } } : {}),
          }),
          status: 'running',
        };
        project.runs.push(r);
        project.revision++;
        return Response.json(episode(match[1]), { status: 202 });
      }
      if (path.endsWith('/cancel')) {
        const r = project.runs.find((r) => r.status === 'running');
        if (r) r.status = 'cancelled';
        project.revision++;
        return Response.json(copy(project));
      }
      if (path.endsWith('/episode-index-runs')) {
        const r = project.runs.find((r) => r.id === 'index');
        r.status = 'running';
        project.revision++;
        return Response.json(index(), { status: 202 });
      }
      return fail('NOT_FOUND', '免费夹具没有此写接口。', 404);
    }
    return fail('NOT_FOUND', '免费夹具没有此接口', 404);
  }
  return {
    fetch: fetcher,
    requests,
    index,
    episode,
    mediaTasks,
    completeMedia(status = 'succeeded', fillImages = true) {
      const t = mediaTasks.find((t) => ['queued', 'running'].includes(t.status));
      if (!t) return;
      t.status = status;
      t.revision++;
      if (status === 'succeeded') {
        const video = t.kind === 'video';
        t.result = {
          files: [
            {
              id: 'output_' + t.id,
              fileId: video ? undefined : 'file_' + t.id,
              contentType: video ? 'video/mp4' : 'image/jpeg',
              contentUrl:
                '/api/projects/p5_free/media-tasks/' + t.id + '/outputs/output_' + t.id + '/file',
              thumbnailUrl: video
                ? '/api/projects/p5_free/media-tasks/' +
                  t.id +
                  '/outputs/output_' +
                  t.id +
                  '/thumbnail'
                : '/api/files/file_' + t.id + '/thumbnail',
              sizeBytes: 1024,
              sha256: 'a'.repeat(64),
              width: video ? 1920 : 720,
              height: video ? 1080 : 1280,
              durationSeconds: video ? 10 : null,
            },
          ],
        };
        if (t.fillMissingImage && !t.stale && fillImages) {
          const asset = project.assets.find(a => a.id === t.target.assetId);
          const target = t.target.lookId ? asset?.looks.find(l => l.id === t.target.lookId) : asset;
          if (target && !target.image) {
            target.image = { fileId: t.result.files[0].fileId, thumbnailUrl: t.result.files[0].thumbnailUrl,
              originalUrl: t.result.files[0].contentUrl };
            t.adoptedOutputId = t.result.files[0].id;
            project.revision++;
          }
        }
      } else if (status === 'failed') t.error = '受控免费生成失败';
    },
    get project() {
      return project;
    },
    get shots() {
      return shots;
    },
    setScenario(value) {
      scenario = value;
      if (value === 'shot-durations') {
        shots[0].durationSeconds = 5.5;
        shots[1].durationSeconds = 9;
      }
      if (value === 'single-shot') {
        shots = shots.filter(s => s.id !== 'shot_2');
        scenes[0].shotIds = ['shot_1'];
      }
      if (value === 'empty-scene') {
        shots = shots.filter(s => s.sceneId !== 'scene_1');
        scenes[0].shotIds = [];
      }
      if (['episode-ready', 'episode-running'].includes(value)) {
        scenes = [];
        shots = [];
        episodes.forEach((e) => {
          e.sceneIds = [];
        });
        project.runs = project.runs.filter((r) => r.contract !== 'storyboard-agent-v1');
        if (value === 'episode-running')
          project.runs.push({
            ...run('episode-working', 'storyboard-agent-v1', {
              operation: 'breakdown',
              episodeId: 'ep_upper',
              shotIds: [],
            }),
            status: 'running',
          });
        project.revision++;
      }
      if (value === 'episode-partial') {
        scenes = copy(savedScenes.slice(0, 1));
        shots = copy(savedShots.slice(0, 2));
        shots[1].videoPrompt = null;
        episodes[0].sceneIds = ['scene_1'];
        project.revision++;
      }
      if (value === 'episode-scene-only') {
        scenes = copy(savedScenes.slice(0, 1));
        scenes[0].shotIds = [];
        shots = [];
        episodes[0].sceneIds = ['scene_1'];
        project.revision++;
      }
      if (value === 'episode-review') {
        const r = project.runs.find(
          (r) => r.status === 'running' && r.contract === 'storyboard-agent-v1',
        );
        if (r) {
          r.stage = 'storyboardReview';
          project.revision++;
        }
      }
      if (value.startsWith('parse-')) parsingStage(value.slice(6));
      if (value === 'read-only') readOnly = true;
      if (value === 'no-capability') capability = false;
      if (['index-running', 'index-failed', 'index-cancelled', 'index-partial'].includes(value)) {
        indexValid = false;
        const r = project.runs.find((r) => r.id === 'index');
        r.status =
          value === 'index-running'
            ? 'running'
            : value === 'index-cancelled'
              ? 'cancelled'
              : 'failed';
        r.error = value === 'index-failed' ? '受控识别失败，部分候选保留' : '';
      }
      if (value === 'whole') {
        segmentation = 'whole';
        episodes = episodes.slice(0, 1);
        episodes[0].title = '整篇作品';
      }
      if (value === 'running') {
        project.runs.find((r) => r.id === 'episode_upper').status = 'running';
      }
      if (value === 'failed') {
        const r = project.runs.find((r) => r.id === 'episode_upper');
        r.status = 'failed';
        r.error = '受控部分失败，已有结果保留';
      }
    },
    complete(status = 'completed') {
      const r = project.runs.find((r) => r.status === 'running');
      if (r) {
        r.status = status;
        project.revision++;
        if (status === 'completed') {
          indexValid = true;
          shots.forEach((s) => {
            if (!s.videoPrompt)
              s.videoPrompt = copy(
                savedShots.find((original) => original.id === s.id)?.videoPrompt || null,
              );
            if (s.videoPrompt) {
              s.videoPrompt.valid = true;
              s.videoPrompt.issues = [];
            }
          });
        }
      }
    },
  };
}
