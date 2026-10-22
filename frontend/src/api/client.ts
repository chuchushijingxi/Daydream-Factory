import type { CatalogAPI } from './types';
import { createStoryboardApi } from './storyboard-client';
import { ApiError } from './http-error';
import { sessionBoundary, type SessionBoundary } from './session-boundary';
import { sessionAudience, type SessionAudience } from './session-audience';
import { translate } from '../contexts/translate';
import { spaceSelection } from './space-selection';
import type { ListFilter } from './spaces-types';
export { ApiError } from './http-error';

let writeSequence = 0;

export type HttpRequestOptions = RequestInit & {
  authentication?: 'public' | 'protected';
};

// Native elements cannot set the shared request Header. This is only the
// backend-defined surface selector; the HttpOnly Cookie still proves the login.
export function protectedResourceUrl(url: string, audience: SessionAudience = sessionAudience()) {
  if (!url.startsWith('/api/')) return url;
  const value = new URL(url, 'http://drama.invalid');
  value.searchParams.set('audience', audience);
  if (audience === 'workspace' && spaceSelection.spaceId)
    value.searchParams.set('space', spaceSelection.spaceId);
  return value.pathname + value.search + value.hash;
}

// All response formats share Cookie transport, session errors and late-result fencing.
export function createHttpClient(
  fetchImpl: typeof fetch = (...args) => globalThis.fetch(...args),
  boundary: SessionBoundary = sessionBoundary,
) {
  const reads = new Map<string, Promise<unknown>>();
  async function consume<T>(
    url: string,
    options: HttpRequestOptions | undefined,
    parse: (response: Response, check: () => void) => Promise<T>,
  ): Promise<T> {
    const epoch = boundary.epoch,
      audience = sessionAudience(),
      spaceId = spaceSelection.spaceId,
      spaceEpoch = spaceSelection.epoch;
    // Identity/profile and platform-public reads survive a space switch; they do
    // not belong to a workspace and must never turn switching into logout.
    const scoped =
      audience === 'workspace' &&
      options?.authentication !== 'public' &&
      !url.startsWith('/api/auth/') &&
      !url.startsWith('/api/notifications');
    const controller = new AbortController();
    const abort = () => controller.abort();
    if (options?.signal?.aborted) controller.abort();
    options?.signal?.addEventListener('abort', abort, { once: true });
    const release = boundary.track(controller);
    // Page-exit DELETE uses keepalive: let its release reach the server even if
    // the next space has already mounted; still discard any late response.
    const releaseSpace =
      scoped && !options?.keepalive ? spaceSelection.track(controller) : () => {};
    const check = () => {
      boundary.assert(epoch);
      if (scoped) spaceSelection.assert(spaceEpoch);
      if (audience !== sessionAudience())
        throw new ApiError(0, 'SESSION_CHANGED', translate('平台已变化，未采用原平台的请求结果。'));
      if (controller.signal.aborted)
        throw new ApiError(0, 'ABORTED', translate('请求已停止；服务端提交结果须回读确认。'));
    };
    try {
      let response: Response;
      const { authentication, ...requestOptions } = options || {};
      const headers = new Headers(requestOptions.headers);
      headers.set('X-Drama-Audience', audience);
      if (audience === 'workspace' && spaceId) headers.set('X-Drama-Space', spaceId);
      try {
        response = await fetchImpl(url, {
          ...requestOptions,
          headers,
          cache: 'no-store',
          credentials: 'same-origin',
          signal: controller.signal,
        });
      } catch {
        check();
        throw new ApiError(0, 'NETWORK', translate('网络错误，尚未确认服务端结果。'));
      }
      check();
      if (!response.ok) {
        const data = await response.json().catch(() => null);
        check();
        const error = new ApiError(
          response.status,
          typeof data?.code === 'string' ? data.code : 'PROGRAM_FAILED',
          typeof data?.message === 'string' ? data.message : 'HTTP ' + response.status,
        );
        // 403 is a permission failure, never proof that the login expired.
        if (response.status === 401 && authentication !== 'public')
          boundary.unauthorized(epoch, error);
        throw error;
      }
      let value: T;
      try {
        value = await parse(response, check);
      } catch (error) {
        check();
        throw error;
      }
      check();
      return value;
    } finally {
      release();
      releaseSpace();
      options?.signal?.removeEventListener('abort', abort);
    }
  }
  return {
    json<T>(url: string, options?: HttpRequestOptions): Promise<T> {
      // Share identical default GET transport only while pending; writes and session changes split generations.
      const shared = options === undefined;
      if (options?.method && options.method !== 'GET') ++writeSequence;
      const key = JSON.stringify([
        boundary.epoch,
        sessionAudience(),
        spaceSelection.spaceId,
        spaceSelection.epoch,
        writeSequence,
        url,
      ]);
      const pending = shared ? reads.get(key) : undefined;
      if (pending) return pending.then((value) => structuredClone(value) as T);
      const promise = consume<T>(url, options, async (response) => {
        const value = await response.json().catch(() => null);
        if (value === null)
          throw new ApiError(
            response.status,
            'PROGRAM_FAILED',
            translate('响应不是有效 JSON，尚未确认服务端结果。'),
          );
        return value as T;
      });
      if (shared) {
        reads.set(key, promise);
        void promise
          .finally(() => {
            if (reads.get(key) === promise) reads.delete(key);
          })
          .catch(() => {});
      }
      return shared ? promise.then((value) => structuredClone(value)) : promise;
    },
    binary<T>(
      url: string,
      options: HttpRequestOptions | undefined,
      parse: (response: Response) => Promise<T>,
    ): Promise<T> {
      return consume(url, options, parse);
    },
    stream(
      url: string,
      options: HttpRequestOptions | undefined,
      onChunk: (chunk: Uint8Array) => void | Promise<void>,
    ): Promise<void> {
      return consume(url, options, async (response, check) => {
        if (
          new Headers(options?.headers).get('Accept') === 'text/event-stream' &&
          !response.headers.get('Content-Type')?.startsWith('text/event-stream')
        )
          throw new ApiError(
            response.status,
            'PROGRAM_FAILED',
            translate('实时同步响应类型不正确。'),
          );
        if (!response.body)
          throw new ApiError(
            response.status,
            'PROGRAM_FAILED',
            translate('响应没有可读取的数据流。'),
          );
        const reader = response.body.getReader();
        try {
          for (;;) {
            const next = await reader.read();
            check();
            if (next.done) break;
            await onChunk(next.value);
            check();
          }
        } finally {
          await reader.cancel().catch(() => {});
          reader.releaseLock();
        }
      });
    },
  };
}
export const http = createHttpClient();
export const jsonPost = (body: unknown): RequestInit => ({
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});

// 写请求只发送一次。响应未知时保留草稿，由页面显式回读，不重复新增或启动。
export function createApi(
  fetchImpl: typeof fetch = (...args) => globalThis.fetch(...args),
): CatalogAPI {
  const transport = createHttpClient(fetchImpl);
  const request = transport.json;
  const post = (body: unknown): RequestInit => ({
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  });
  const projectPath = (id: string, ...rest: string[]) =>
    '/api/projects/' +
    encodeURIComponent(id) +
    (rest.length ? '/' + rest.map(encodeURIComponent).join('/') : '');
  return {
    getStatus() {
      return request('/api/status');
    },
    getProject(id) {
      return request(projectPath(id));
    },
    getSource(id) {
      return request(projectPath(id, 'source'));
    },
    async saveVisualPrompt(id, target, payload) {
      const receipt = await request<Awaited<ReturnType<CatalogAPI['saveVisualPrompt']>>>(
        target.lookId
          ? projectPath(id, 'assets', target.assetId, 'looks', target.lookId, 'visual-prompt')
          : projectPath(id, 'assets', target.assetId, 'visual-prompt'),
        post(payload),
      );
      const saved = Array.isArray(receipt.visualPrompts)
        ? receipt.visualPrompts.find(
            (v) =>
              v?.target?.assetId === target.assetId &&
              (v.target.lookId || '') === (target.lookId || ''),
          )
        : undefined;
      if (
        receipt.id !== id ||
        receipt.revision !== payload.revision + 1 ||
        saved?.text !== payload.text ||
        saved.origin !== 'user' ||
        saved.stale ||
        saved.sourceVersion !== receipt.source?.version
      )
        throw new ApiError(
          200,
          'PROGRAM_FAILED',
          translate('提示词保存回执不完整，尚未确认服务端结果。'),
        );
      return receipt;
    },
    startExtraction(id, payload) {
      return request(projectPath(id, 'runs'), post(payload));
    },
    startVisualPrompts(id, payload) {
      return request(projectPath(id, 'visual-prompt-runs'), post(payload));
    },
    startSingleVisualPrompts(id, target, payload) {
      return request(
        target.lookId
          ? projectPath(id, 'assets', target.assetId, 'looks', target.lookId, 'visual-prompt-runs')
          : projectPath(id, 'assets', target.assetId, 'visual-prompt-runs'),
        post(payload),
      );
    },
    cancelTask(id, payload) {
      return request(projectPath(id, 'cancel'), post(payload));
    },
    listProjects(filter?: ListFilter) {
      return request('/api/projects' + (filter ? '?' + new URLSearchParams({ filter }) : ''));
    },
    async deleteProject(id, payload) {
      const receipt = await request<Awaited<ReturnType<CatalogAPI['deleteProject']>>>(
        projectPath(id, 'delete'),
        post(payload),
      );
      if (
        receipt.id !== id ||
        receipt.revision !== payload.revision + 1 ||
        typeof receipt.deletedAt !== 'string' ||
        !receipt.deletedAt
      ) {
        throw new ApiError(
          200,
          'PROGRAM_FAILED',
          translate('删除回执不完整，尚未确认服务端结果。'),
        );
      }
      return receipt;
    },
    async uploadFile(file, options = {}, signal) {
      const formData = new FormData();
      formData.append('file', file);
      if (options.thumbnail !== undefined) formData.append('thumbnail', String(options.thumbnail));
      if (options.thumbnailMaxWidth !== undefined)
        formData.append('thumbnailMaxWidth', String(options.thumbnailMaxWidth));
      if (options.thumbnailMaxHeight !== undefined)
        formData.append('thumbnailMaxHeight', String(options.thumbnailMaxHeight));
      const receipt = await request<Awaited<ReturnType<CatalogAPI['uploadFile']>>>('/api/upload', {
        method: 'POST',
        body: formData,
        signal,
      });
      if (
        !/^file_[0-9a-f]{32}$/.test(receipt.fileId) ||
        typeof receipt.filename !== 'string' ||
        !receipt.filename ||
        receipt.sizeBytes !== file.size ||
        typeof receipt.contentType !== 'string' ||
        !receipt.contentType ||
        receipt.originalUrl !== '/api/files/' + receipt.fileId + '/original' ||
        receipt.thumbnailUrl !==
          (options.thumbnail ? '/api/files/' + receipt.fileId + '/thumbnail' : null)
      )
        throw new ApiError(
          201,
          'PROGRAM_FAILED',
          translate('文件上传回执不完整，尚未确认服务端结果。'),
        );
      return receipt;
    },
    getFile(id) {
      return request('/api/files/' + encodeURIComponent(id));
    },
    async saveAssetImage(id, target, payload, signal) {
      const receipt = await request<Awaited<ReturnType<CatalogAPI['saveAssetImage']>>>(
        target.lookId
          ? projectPath(id, 'assets', target.assetId, 'looks', target.lookId, 'image')
          : projectPath(id, 'assets', target.assetId, 'image'),
        { ...post(payload), signal },
      );
      const asset = Array.isArray(receipt.assets)
        ? receipt.assets.find((a) => a?.id === target.assetId)
        : undefined;
      const image = target.lookId
        ? Array.isArray(asset?.looks)
          ? asset.looks.find((l) => l?.id === target.lookId)?.image
          : undefined
        : asset?.image;
      if (
        receipt.id !== id ||
        receipt.revision !== payload.revision + 1 ||
        image?.fileId !== payload.fileId ||
        image.originalUrl !== projectPath(id, 'files', payload.fileId, 'original') ||
        image.thumbnailUrl !== projectPath(id, 'files', payload.fileId, 'thumbnail')
      ) {
        throw new ApiError(
          200,
          'PROGRAM_FAILED',
          translate('图片保存回执不完整，尚未确认服务端结果。'),
        );
      }
      return receipt;
    },
    createProject(input) {
      return request('/api/projects', post(input));
    },
    listModels() {
      return request('/api/models');
    },
    uploadModelLogo(file) {
      return request('/api/model-logos', {
        method: 'POST',
        headers: {
          'Content-Type': file.type || 'application/octet-stream',
        },
        body: file,
      });
    },
    discoverModels(payload) {
      return request('/api/model-config/models/discover', post(payload));
    },
    getModelConfiguration() {
      return request('/api/model-config');
    },
    saveModelConfiguration(payload) {
      return request('/api/model-config', post(payload));
    },
    deleteModelConnection(id, payload) {
      return request('/api/model-config/' + encodeURIComponent(id) + '/delete', post(payload));
    },
    listStyles() {
      return request('/api/visual-styles');
    },
    saveStyle(id, payload) {
      return request(projectPath(id, 'style'), post(payload));
    },
    saveSettings(id, payload) {
      return request(projectPath(id, 'settings'), post(payload));
    },
    createAsset(id, payload) {
      return request(projectPath(id, 'assets'), post(payload));
    },
    updateAsset(id, assetId, payload) {
      return request(projectPath(id, 'assets', assetId), post(payload));
    },
    deleteAsset(id, assetId, payload) {
      return request(projectPath(id, 'assets', assetId, 'delete'), post(payload));
    },
    createLook(id, assetId, payload) {
      return request(projectPath(id, 'assets', assetId, 'looks'), post(payload));
    },
    updateLook(id, assetId, lookId, payload) {
      return request(projectPath(id, 'assets', assetId, 'looks', lookId), post(payload));
    },
    deleteLook(id, assetId, lookId, payload) {
      return request(projectPath(id, 'assets', assetId, 'looks', lookId, 'delete'), post(payload));
    },
  };
}
export const api = createApi();
export const storyboardApi = createStoryboardApi();
