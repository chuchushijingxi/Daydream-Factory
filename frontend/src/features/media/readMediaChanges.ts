import { projectMediaApi, type MediaTarget, type ProjectMedia } from '../../api/project-media';
import { translate } from '../../contexts/translate';

export type MediaSnapshot = { head?: string; revision?: number; items: ProjectMedia[] };

export function sameMediaTarget(a: MediaTarget, b: MediaTarget) {
  if ('assetId' in a && 'assetId' in b)
    return a.assetId === b.assetId && (a.lookId || '') === (b.lookId || '');
  return 'shotId' in a && 'shotId' in b && a.episodeId === b.episodeId && a.shotId === b.shotId;
}

// 首次沿游标发现目标；后续只追新增历史，并独立回读未终结任务。
// 项目变化还会改变终态任务的 stale/采用投影，不能把终态误当作整个视图不可变。
export async function readMediaChanges(
  project: string,
  kind: 'image' | 'video',
  old: MediaSnapshot,
  current: () => boolean,
  target?: MediaTarget,
  revision?: number,
) {
  const refreshViews = old.revision !== undefined && old.revision !== revision;
  const fresh = new Map<string, ProjectMedia>(), cursors = new Set<string>();
  let cursor: string | undefined, nextCursor: string | null = null,
    head = old.head, first = true;
  do {
    if (!current()) throw new DOMException('Read scope changed', 'AbortError');
    const page = await projectMediaApi.list(project, cursor, kind);
    if (first && page.items.length) { head = page.items[0].id; first = false; }
    nextCursor = page.nextCursor;
    for (const item of page.items) fresh.set(item.id, item);
    const reachedHistory = old.head
      ? page.items.some(task => task.id === old.head)
      : target && page.items.some(task => sameMediaTarget(task.target, target));
    if (reachedHistory) break;
    cursor = page.nextCursor || undefined;
    if (cursor && cursors.has(cursor)) throw new Error(translate('媒体任务分页异常，未确认生成状态。'));
    if (cursor) cursors.add(cursor);
  } while (cursor);
  for (const item of old.items) {
    if (!current()) throw new DOMException('Read scope changed', 'AbortError');
    const active = ['queued', 'running', 'unknown'].includes(item.status);
    const changedTarget = refreshViews && !!target && sameMediaTarget(item.target, target);
    if (!fresh.has(item.id) && (active || changedTarget))
      fresh.set(item.id, await projectMediaApi.get(project, item.id));
  }
  const merged = new Map(old.items.map(item => [item.id, item]));
  for (const item of fresh.values()) {
    const before = merged.get(item.id);
    if (!before || before.revision <= item.revision) merged.set(item.id, item);
  }
  // 新记录沿服务端顺序，已加载旧页不丢失；低任务 revision 不覆盖新状态。
  const ids = [...new Set([...fresh.keys(), ...merged.keys()])];
  return { head, revision, nextCursor, items: ids.map(id => merged.get(id)!) };
}
