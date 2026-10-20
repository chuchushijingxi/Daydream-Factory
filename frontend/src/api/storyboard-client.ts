import { ApiError, createHttpClient, jsonPost } from './client';
import { translate } from '../contexts/translate';
import type {
  ChangeReceipt,
  EpisodeRunInput,
  EpisodeSourceView,
  EpisodeView,
  StoryboardIndexView,
  StoryboardOperation,
  StoryboardRoot,
} from './storyboard-types';

export function createStoryboardApi(
  fetchImpl: typeof fetch = (...args) => globalThis.fetch(...args),
) {
  const path = (id: string, ...tail: string[]) =>
    '/api/projects/' + [id, 'storyboard', ...tail].map(encodeURIComponent).join('/');
  const transport = createHttpClient(fetchImpl);
  const request = <T>(url: string, body?: unknown) =>
    transport.json<T>(url, body === undefined ? undefined : jsonPost(body));
  function root<T extends StoryboardRoot>(view: T, id: string): T {
    if (
      view.projectId !== id ||
      !Number.isInteger(view.revision) ||
      view.revision < 1 ||
      !Number.isInteger(view.sourceVersion) ||
      view.sourceVersion < 1 ||
      typeof view.access?.editable !== 'boolean' ||
      typeof view.access?.canStart !== 'boolean' ||
      typeof view.access.reason !== 'string' ||
      !('activeRun' in view)
    )
      throw new ApiError(
        200,
        'PROGRAM_FAILED',
        translate('分镜回读身份、版本或权限不完整，未采用响应。'),
      );
    return view;
  }
  const episode = (v: EpisodeView, id: string, episodeId: string) => {
    root(v, id);
    if (
      v.episode?.id !== episodeId ||
      !Array.isArray(v.episode.sceneIds) ||
      !Array.isArray(v.scenes) ||
      !Array.isArray(v.shots) ||
      typeof v.index?.valid !== 'boolean' ||
      !('latestRun' in v) ||
      v.scenes.some((s) => s.episodeId !== episodeId) ||
      v.shots.some((s) => !v.scenes.some((c) => c.id === s.sceneId))
    )
      throw new ApiError(200, 'PROGRAM_FAILED', translate('分集回读范围不一致，未采用响应。'));
    return v;
  };
  const index = (v: StoryboardIndexView, id: string) => {
    root(v, id);
    if (
      !(v.episodeCount === null || (Number.isInteger(v.episodeCount) && v.episodeCount > 0)) ||
      !('latestIndexRun' in v) ||
      (v.index && (!Array.isArray(v.index.episodes) || typeof v.index.valid !== 'boolean'))
    )
      throw new ApiError(200, 'PROGRAM_FAILED', translate('分集目录回执不完整，未采用响应。'));
    return v;
  };
  return {
    async getIndex(id: string) {
      return index(await request<StoryboardIndexView>(path(id)), id);
    },
    async getEpisode(id: string, episodeId: string) {
      return episode(await request<EpisodeView>(path(id, 'episodes', episodeId)), id, episodeId);
    },
    async getSource(id: string, episodeId: string) {
      const v = root(
        await request<EpisodeSourceView>(path(id, 'episodes', episodeId, 'source')),
        id,
      );
      if (v.episodeId !== episodeId || !Array.isArray(v.fragments))
        throw new ApiError(200, 'PROGRAM_FAILED', translate('原文范围回执不完整。'));
      return v;
    },
    async retryIndex(id: string, revision: number, billingBasis?: string) {
      return index(
        await request<StoryboardIndexView>(path(id, 'episode-index-runs'), {
          revision,
          ...(billingBasis ? { billingBasis } : {}),
        }),
        id,
      );
    },
    async start(id: string, episodeId: string, input: EpisodeRunInput) {
      return episode(
        await request<EpisodeView>(path(id, 'episodes', episodeId, 'runs'), input),
        id,
        episodeId,
      );
    },
    async changes(id: string, revision: number, operations: StoryboardOperation[]) {
      const v = await request<ChangeReceipt>(path(id, 'changes'), { revision, operations });
      if (
        v.projectId !== id ||
        !Number.isInteger(v.revision) ||
        v.revision < revision ||
        !Array.isArray(v.changes) ||
        !Array.isArray(v.invalidated)
      )
        throw new ApiError(
          200,
          'PROGRAM_FAILED',
          translate('保存回执不完整，草稿保留，请回读核对。'),
        );
      return v;
    },
  };
}
