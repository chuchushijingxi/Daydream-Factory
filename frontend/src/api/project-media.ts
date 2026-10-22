import { responseFields } from './response-fields';
import { ApiError, http, jsonPost } from './client';
import type { MediaTask, Project } from './types';
import type { UsageBasisDTO } from './administration-types';
import { safeInteger } from './response-values';
import { validateChargeBasis } from './model-usage-client';
import { translate } from '../contexts/translate';
export type MediaTarget =
  | { assetId: string; lookId?: string }
  | { episodeId: string; shotId: string };
export type MediaCredit = {
  basis: UsageBasisDTO;
  prepaid: number;
  refunded: number;
  charged: number;
  settlement: 'pending' | 'settled' | 'refund-pending' | 'refunded';
  deductionId: string | null;
  refundId: string | null;
};
export type ProjectMedia = MediaTask & {
  aspectRatio?: string;
  imageType?: import('./types').CharacterImageType | '';
  fillMissingImage?: boolean;
  options?: Partial<import('./types').MediaOptions>;
  projectId: string;
  kind: 'image' | 'video';
  target: MediaTarget;
  inputRevision: number;
  sourceVersion: number;
  stale: boolean;
  adoptedOutputId: string;
  credit: MediaCredit | null;
};
export type MediaCommand = {
  aspectRatio?: string;
  imageType?: import('./types').CharacterImageType;
  options?: import('./types').MediaOptions;
  commandId: string;
  revision: number;
  modelId: string;
  target: MediaTarget;
  billingBasis?: string;
};
export type ImageBatchItem = {
  commandId: string;
  target: MediaTarget;
  task: ProjectMedia | null;
  error: { code: string; message: string } | null;
};
const base = (p: string) => '/api/projects/' + encodeURIComponent(p) + '/media-tasks';
const request = <T>(url: string, body?: unknown) =>
  http.json<T>(url, body === undefined ? undefined : jsonPost(body));
function valid(v: ProjectMedia, p: string) {
  if (
    v.projectId !== p ||
    !v.id ||
    !v.target ||
    !['queued', 'running', 'unknown', 'succeeded', 'failed', 'cancelled'].includes(v.status) ||
    !Number.isInteger(v.revision) ||
    v.credit === undefined ||
    (v.fillMissingImage !== undefined && typeof v.fillMissingImage !== 'boolean')
  )
    throw new ApiError(200, 'PROGRAM_FAILED', translate('媒体任务回执不完整。'));
  if (v.credit !== null) {
    const c = responseFields(v.credit, [
      'basis',
      'prepaid',
      'refunded',
      'charged',
      'settlement',
      'deductionId',
      'refundId',
    ]);
    v.credit = c;
    if (
      !c ||
      !['pending', 'settled', 'refund-pending', 'refunded'].includes(c.settlement) ||
      (c.deductionId !== null && typeof c.deductionId !== 'string') ||
      (c.refundId !== null && typeof c.refundId !== 'string')
    )
      throw new ApiError(200, 'PROGRAM_FAILED', translate('媒体积分回执不完整。'));
    c.basis = validateChargeBasis(c.basis);
    for (const n of [c.prepaid, c.refunded, c.charged]) safeInteger(n);
    if (
      (c.refunded > 0 && !c.refundId) ||
      (c.prepaid > 0 && !c.deductionId) ||
      (c.settlement === 'refunded' && c.basis.amount > 0 && !c.refundId)
    )
      throw new ApiError(200, 'PROGRAM_FAILED', translate('媒体扣退缺少真实流水依据。'));
  }
  if (v.result) {
    if (!Array.isArray(v.result.files))
      throw new ApiError(200, 'PROGRAM_FAILED', translate('媒体结果回执不完整。'));
    for (const file of v.result.files) {
      const prefix =
        base(p) + '/' + encodeURIComponent(v.id) + '/outputs/' + encodeURIComponent(file.id);
      if (
        file.contentUrl !== prefix + '/file' ||
        (file.thumbnailUrl && file.thumbnailUrl !== prefix + '/thumbnail')
      )
        throw new ApiError(
          200,
          'CONTENT_BOUNDARY_VIOLATION',
          translate('媒体地址不符合受保护同源合同，请后端核查。'),
        );
    }
  }
  return v;
}
export const projectMediaApi = {
  async batchImages(p: string, commands: MediaCommand[]) {
    const result = await request<{ items: ImageBatchItem[] }>(base(p) + '/batch-images', {
      commands,
    });
    const seen = new Set<string>();
    if (!Array.isArray(result.items) || result.items.length !== commands.length)
      throw new ApiError(200, 'PROGRAM_FAILED', translate('批量受理回执不完整，请核对原请求。'));
    for (const item of result.items) {
      if (!item || typeof item !== 'object')
        throw new ApiError(200, 'PROGRAM_FAILED', translate('批量受理回执不完整，请核对原请求。'));
      const command = commands.find((c) => c.commandId === item.commandId);
      if (
        !command ||
        seen.has(item.commandId) ||
        JSON.stringify(item.target) !== JSON.stringify(command.target) ||
        !!item.task === !!item.error ||
        (item.error && (!item.error.code || !item.error.message))
      )
        throw new ApiError(200, 'PROGRAM_FAILED', translate('批量受理回执不匹配，请核对原请求。'));
      seen.add(item.commandId);
      if (item.task) {
        valid(item.task, p);
        if (
          item.task.kind !== 'image' ||
          JSON.stringify(item.task.target) !== JSON.stringify(command.target) ||
          !item.task.fillMissingImage
        )
          throw new ApiError(
            200,
            'PROGRAM_FAILED',
            translate('批量图片任务回执不匹配，请核对原请求。'),
          );
      }
    }
    return result.items;
  },
  async start(p: string, c: MediaCommand) {
    return valid(await request<ProjectMedia>(base(p), c), p);
  },
  async list(p: string, cursor?: string, kind?: 'image' | 'video') {
    const v = await request<{ items: ProjectMedia[]; nextCursor: string | null }>(
      base(p) +
        '?limit=100' +
        (cursor ? '&cursor=' + encodeURIComponent(cursor) : '') +
        (kind ? '&kind=' + kind : ''),
    );
    v.items.forEach((x) => valid(x, p));
    return v;
  },
  async get(p: string, id: string) {
    return valid(await request<ProjectMedia>(base(p) + '/' + encodeURIComponent(id)), p);
  },
  async cancel(p: string, t: ProjectMedia) {
    return valid(
      await request<ProjectMedia>(base(p) + '/' + t.id + '/cancel', { revision: t.revision }),
      p,
    );
  },
  async adopt(p: string, t: ProjectMedia, outputId: string, revision: number) {
    const v = await request<Project>(base(p) + '/' + t.id + '/adopt', { revision, outputId });
    if (v.id !== p || v.revision !== revision + 1)
      throw new ApiError(200, 'PROGRAM_FAILED', translate('媒体采用回执不完整，请回读项目核对。'));
    return v;
  },
};
