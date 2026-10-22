import { ApiError, http } from './client';
import { responseFields } from './response-fields';
import { translate } from '../contexts/translate';
export type Creation = {
  spaceId: string;
  ownerId: string;
  creatorId: string;
  creatorName: string;
  sharedWithMe: boolean;
  effectiveAccess: 1 | 2;
  taskId: string;
  outputId: string;
  projectId: string;
  projectTitle: string;
  kind: 'image' | 'video';
  createdAt: string;
  modelId: string;
  requestModelId: string;
  aspectRatio: string;
  resolution: string;
  width: number;
  height: number;
  durationSeconds: number;
  contentType: string;
  sizeBytes: number;
  promptPreview: string;
  prompt?: string;
  contentUrl: string;
  thumbnailUrl: string;
};
export type CreationPage = { items: Creation[]; nextCursor: string | null };
function valid(raw: Creation, detail = false): Creation {
  const v = responseFields(raw, [
    'spaceId',
    'ownerId',
    'creatorId',
    'creatorName',
    'sharedWithMe',
    'effectiveAccess',
    'taskId',
    'outputId',
    'projectId',
    'projectTitle',
    'kind',
    'createdAt',
    'modelId',
    'requestModelId',
    'aspectRatio',
    'resolution',
    'width',
    'height',
    'durationSeconds',
    'contentType',
    'sizeBytes',
    'promptPreview',
    'prompt',
    'contentUrl',
    'thumbnailUrl',
  ]) as Creation;
  if (
    !v ||
    typeof v.creatorName !== 'string' ||
    typeof v.sharedWithMe !== 'boolean' ||
    ![1, 2].includes(v.effectiveAccess) ||
    !['image', 'video'].includes(v.kind) ||
    ['spaceId', 'ownerId', 'creatorId', 'taskId', 'outputId', 'projectId', 'modelId'].some(
      (k) => typeof v[k as keyof Creation] !== 'string' || !v[k as keyof Creation],
    ) ||
    [
      'projectTitle',
      'createdAt',
      'requestModelId',
      'aspectRatio',
      'resolution',
      'contentType',
      'promptPreview',
      'contentUrl',
      'thumbnailUrl',
    ].some((k) => typeof v[k as keyof Creation] !== 'string') ||
    !Number.isFinite(Date.parse(v.createdAt)) ||
    [v.width, v.height, v.sizeBytes].some((n) => !Number.isSafeInteger(n) || n < 0) ||
    !Number.isFinite(v.durationSeconds) ||
    v.durationSeconds < 0 ||
    (detail && typeof v.prompt !== 'string')
  )
    throw new ApiError(200, 'PROGRAM_FAILED', translate('创作资产回执不完整。'));
  const base =
    '/api/media-library/' +
    encodeURIComponent(v.taskId) +
    '/outputs/' +
    encodeURIComponent(v.outputId);
  if (v.contentUrl !== base + '/file' || (v.thumbnailUrl && v.thumbnailUrl !== base + '/thumbnail'))
    throw new ApiError(
      200,
      'CONTENT_BOUNDARY_VIOLATION',
      translate('媒体地址不符合受保护同源合同。'),
    );
  return v;
}
export const mediaLibraryApi = {
  async list(
    kind = 'image',
    q = '',
    cursor?: string,
    filter: import('./shared-resource').ListFilter = 'all',
  ): Promise<CreationPage> {
    const params = new URLSearchParams({ limit: '24', filter });
    if (kind) params.set('kind', kind);
    if (q) params.set('q', q);
    if (cursor) params.set('cursor', cursor);
    const v = await http.json<CreationPage>('/api/media-library?' + params);
    if (
      !v ||
      !Array.isArray(v.items) ||
      (v.nextCursor !== null && typeof v.nextCursor !== 'string')
    )
      throw new ApiError(200, 'PROGRAM_FAILED', translate('创作资产列表回执不完整。'));
    return {
      items: v.items.map((item) => valid(item)),
      nextCursor: v.nextCursor,
    };
  },
  async detail(task: string, output: string) {
    const v = valid(
      await http.json<Creation>(
        '/api/media-library/' + encodeURIComponent(task) + '/outputs/' + encodeURIComponent(output),
      ),
      true,
    );
    if (v.taskId !== task || v.outputId !== output)
      throw new ApiError(200, 'PROGRAM_FAILED', translate('创作资产详情不匹配。'));
    return v;
  },
};
