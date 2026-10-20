import { responseFields } from './response-fields';
import { watchInvalidations } from './invalidation-stream';
import { http, jsonPost, ApiError, type createHttpClient } from './client';
import { translate } from '../contexts/translate';
export type NotificationItem = {
  id: string;
  title: string;
  summary: string;
  publishedAt: string;
  readAt: string | null;
};
export type NotificationDetail = NotificationItem & { body: string };
export type NotificationMessage = {
  id: string;
  title: string;
  body: string;
  audience: 'all' | 'user';
  recipientUserId: string;
  recipientName: string;
  recipientEmail: string;
  status: 'draft' | 'published' | 'withdrawn';
  revision: number;
  recipientCount: number;
  createdAt: string;
  publishedAt: string | null;
  withdrawnAt: string | null;
  authorId: string;
  authorName: string;
};
export type NotificationPage<T> = {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
  unread: number;
};
export type NotificationDraft = Pick<
  NotificationMessage,
  'title' | 'body' | 'audience' | 'recipientUserId'
>;
export type NotificationFilter = 'all' | 'unread' | 'read';
function malformed(): never {
  throw new ApiError(
    200,
    'PROGRAM_FAILED',
    translate('通知响应不完整，请重新读取后核对。'),
  );
}
function object(v: unknown): Record<string, unknown> {
  if (!v || typeof v !== 'object' || Array.isArray(v)) malformed();
  return v as Record<string, unknown>;
}
function text(v: unknown): v is string {
  return typeof v === 'string';
}
function integer(v: unknown): v is number {
  return typeof v === 'number' && Number.isSafeInteger(v) && v >= 0;
}
function at(v: unknown): v is string {
  return text(v) && Number.isFinite(Date.parse(v));
}
function item(raw: unknown, detail = false): NotificationItem | NotificationDetail {
  const v = responseFields(object(raw), [
    'id', 'title', 'summary', 'publishedAt', 'readAt', ...(detail ? ['body'] : []),
  ]);
  if (
    !text(v.id) ||
    !v.id ||
    !text(v.title) ||
    !text(v.summary) ||
    Array.from(v.summary).length > 120 ||
    !at(v.publishedAt) ||
    (v.readAt !== null && !at(v.readAt)) ||
    (detail && !text(v.body))
  )
    malformed();
  return v as NotificationItem;
}
function message(raw: unknown): NotificationMessage {
  const v = responseFields(object(raw), [
    'id',
    'title',
    'body',
    'audience',
    'recipientUserId',
    'recipientName',
    'recipientEmail',
    'status',
    'revision',
    'recipientCount',
    'createdAt',
    'publishedAt',
    'withdrawnAt',
    'authorId',
    'authorName',
  ]);
  if (
    !text(v.id) ||
    !v.id ||
    !text(v.title) ||
    !text(v.body) ||
    !['all', 'user'].includes(String(v.audience)) ||
    !text(v.recipientUserId) ||
    !text(v.recipientName) ||
    !text(v.recipientEmail) ||
    (v.audience === 'all'
      ? v.recipientName !== '' || v.recipientEmail !== ''
      : !v.recipientName || !v.recipientEmail) ||
    !['draft', 'published', 'withdrawn'].includes(String(v.status)) ||
    !integer(v.revision) ||
    v.revision < 1 ||
    !integer(v.recipientCount) ||
    !at(v.createdAt) ||
    (v.publishedAt !== null && !at(v.publishedAt)) ||
    (v.withdrawnAt !== null && !at(v.withdrawnAt)) ||
    !text(v.authorId) ||
    !v.authorId ||
    !text(v.authorName) ||
    !v.authorName
  )
    malformed();
  return v as NotificationMessage;
}
function page<T>(raw: unknown, parse: (v: unknown) => T): NotificationPage<T> {
  const v = responseFields(object(raw), ['items', 'total', 'page', 'pageSize', 'unread']);
  if (
    !Array.isArray(v.items) ||
    !integer(v.total) ||
    !integer(v.page) ||
    v.page < 1 ||
    !integer(v.pageSize) ||
    v.pageSize < 1 ||
    v.pageSize > 100 ||
    !integer(v.unread) ||
    v.items.length > v.pageSize
  )
    malformed();
  return { ...v, items: (v.items as unknown[]).map(parse) } as NotificationPage<T>;
}
const userPath = '/api/notifications',
  opsPath = '/api/operations/notifications';
const path = (base: string, id: string) => base + '/' + encodeURIComponent(id);
const paging = (page: number, status?: NotificationFilter) =>
  '?' + new URLSearchParams({ page: String(page), pageSize: '20', ...(status ? { status } : {}) });
export function notificationImageUrl(url: string, messageId?: string) {
  const match = /^\/api\/notifications\/([A-Za-z0-9_-]+)\/images\/([A-Za-z0-9_-]+)$/.exec(url);
  return match && (!messageId || match[1] === messageId) ? url : '';
}
export function createNotificationsApi(transport: ReturnType<typeof createHttpClient> = http) {
  return {
    watch(onChange: () => void, signal: AbortSignal) {
      return watchInvalidations(transport, userPath + '/events', 'notifications', onChange, signal);
    },
    async unread() {
      const v = responseFields(object(await transport.json(userPath + '/unread')), ['unread']);
      if (!integer(v.unread)) malformed();
      return v.unread;
    },
    async list(n = 1, status: NotificationFilter = 'all') {
      return page(
        await transport.json(userPath + paging(n, status)),
        (v) => item(v) as NotificationItem,
      );
    },
    async detail(id: string) {
      const v = item(await transport.json(path(userPath, id)), true) as NotificationDetail;
      if (v.id !== id) malformed();
      return v;
    },
    async read(id: string) {
      const v = responseFields(
        object(await transport.json(path(userPath, id) + '/read', jsonPost({}))),
        ['id', 'readAt'],
      );
      if (v.id !== id || !at(v.readAt)) malformed();
      return v as { id: string; readAt: string };
    },
    async readAll() {
      const v = responseFields(
        object(await transport.json(userPath + '/read-all', jsonPost({}))),
        ['updated', 'unread'],
      );
      if (!integer(v.updated) || !integer(v.unread)) malformed();
      return v as { updated: number; unread: number };
    },
    async operatorList(n = 1) {
      return page(await transport.json(opsPath + paging(n)), message);
    },
    async operatorDetail(id: string) {
      const v = message(await transport.json(path(opsPath, id)));
      if (v.id !== id) malformed();
      return v;
    },
    async command(command: string) {
      return message(
        await transport.json(
          '/api/operations/notification-commands/' + encodeURIComponent(command),
        ),
      );
    },
    async create(input: NotificationDraft & { commandId: string }) {
      return message(await transport.json(opsPath, jsonPost(input)));
    },
    async update(id: string, input: NotificationDraft & { revision: number }) {
      const v = message(await transport.json(path(opsPath, id), jsonPost(input)));
      if (v.id !== id || v.revision !== input.revision + 1) malformed();
      return v;
    },
    async withdraw(id: string, revision: number) {
      const v = message(
        await transport.json(path(opsPath, id) + '/withdraw', jsonPost({ revision })),
      );
      if (v.id !== id || v.status !== 'withdrawn' || v.revision !== revision + 1 || !v.withdrawnAt)
        malformed();
      return v;
    },
    async uploadImage(id: string, file: File, signal: AbortSignal) {
      if (
        !['image/png', 'image/jpeg'].includes(file.type) ||
        file.size > 5 * 1024 * 1024 ||
        !file.size
      )
        throw new ApiError(
          400,
          'NOTIFICATION_IMAGE_INVALID',
          translate('请上传5MB以内的PNG或JPEG图片。'),
        );
      const v = responseFields(
        object(
          await transport.json(path(opsPath, id) + '/images', {
            method: 'POST',
            headers: { 'Content-Type': file.type },
            body: file,
            signal,
          }),
        ),
        ['id', 'url', 'previewUrl', 'contentType', 'width', 'height', 'sizeBytes'],
      );
      if (
        !text(v.id) ||
        !v.id ||
        !text(v.url) ||
        notificationImageUrl(v.url, id) !== v.url ||
        v.url !== path(userPath, id) + '/images/' + encodeURIComponent(v.id) ||
        v.previewUrl !== path(opsPath, id) + '/images/' + encodeURIComponent(v.id) ||
        !['image/png', 'image/jpeg'].includes(String(v.contentType)) ||
        !integer(v.width) ||
        v.width < 1 ||
        v.width > 4096 ||
        !integer(v.height) ||
        v.height < 1 ||
        v.height > 4096 ||
        !integer(v.sizeBytes) ||
        v.sizeBytes < 1 ||
        v.sizeBytes > 5 * 1024 * 1024
      )
        malformed();
      return v.url;
    },
    async publish(id: string, revision: number) {
      const v = message(
        await transport.json(path(opsPath, id) + '/publish', jsonPost({ revision })),
      );
      if (v.id !== id || v.status !== 'published' || v.revision !== revision + 1) malformed();
      return v;
    },
  };
}
export type NotificationsApi = ReturnType<typeof createNotificationsApi>;
export const notificationsApi = createNotificationsApi();
