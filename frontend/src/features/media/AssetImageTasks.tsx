import { readMediaChanges, type MediaSnapshot } from './readMediaChanges';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { CircleAlert, Clock3, LoaderCircle } from 'lucide-react';
import { type ProjectMedia } from '../../api/project-media';
import type { AssetImage, VisualTarget } from '../../api/types';
import { useWorkspace } from '../../state/WorkspaceContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useProjectUpdates } from '../projects/useProjectUpdates';
import { sessionBoundary } from '../../api/session-boundary';

export const imageTargetKey = (target: VisualTarget) => JSON.stringify([target.assetId, target.lookId || '']);
export function imageTaskState(task: ProjectMedia | undefined, image?: AssetImage | null | boolean) {
  const saving = task?.status === 'succeeded' && task.fillMissingImage && !task.stale && !image;
  const busy = task?.status === 'queued' || task?.status === 'running' || !!saving;
  const label = task?.status === 'queued' ? '排队中' : task?.status === 'running' ? '生成中'
    : saving ? '保存中' : task?.status === 'unknown' ? '待确认'
    : task?.status === 'failed' ? '生成失败' : '';
  return { busy, label };
}
type ImageTasks = {
  latest: Map<string, ProjectMedia>;
  ready: boolean;
  error: string;
  refresh: () => void;
};
const Context = createContext<ImageTasks | null>(null);
export const useAssetImageTasks = () => useContext(Context);

// 一个资产页面共用原媒体列表；分页保留所有目标，不为每张卡片轮询。
export function AssetImageTasksProvider({ children }: { children: ReactNode }) {
  const { currentProject: p, status } = useWorkspace();
  const [snapshot, setSnapshot] = useState<{ projectId: string; items: ProjectMedia[] } | null>(null);
  const [error, setError] = useState('');
  const scope = useRef(p?.id), sequence = useRef(0);
  const reading = useRef<{ projectId: string; ticket: number } | null>(null), reread = useRef(false);
  const history = useRef<MediaSnapshot>({items: []});
  scope.current = p?.id;
  const enabled = !!p && !!status?.capabilities.projectMedia;
  const read = useCallback(async () => {
    if (!p || !enabled) return;
    if (reading.current?.projectId === p.id) { reread.current = true; return; }
    const projectId = p.id, ticket = ++sequence.current, epoch = sessionBoundary.epoch;
    const operation = { projectId, ticket };
    reading.current = operation;
    try {
      const result = await readMediaChanges(projectId, 'image', history.current,
        () => scope.current === projectId && sequence.current === ticket && epoch === sessionBoundary.epoch, undefined, p.revision);
      if (scope.current !== projectId || sequence.current !== ticket || epoch !== sessionBoundary.epoch) return;
      const latest = new Map<string, ProjectMedia>();
      for (const task of result.items) if ('assetId' in task.target) {
        const key = imageTargetKey(task.target);
        if (!latest.has(key)) latest.set(key, task);
      }
      history.current = {head: result.head, revision: result.revision, items: [...latest.values()]};
      setSnapshot({ projectId, items: history.current.items });
      setError('');
    } catch (e) {
      if (scope.current === projectId && ticket === sequence.current && epoch === sessionBoundary.epoch)
        setError((e as Error).message);
    } finally {
      if (reading.current === operation) {
        reading.current = null;
        if (reread.current && scope.current === projectId && epoch === sessionBoundary.epoch) {
          reread.current = false;
          void read();
        }
      }
    }
  }, [p?.id, p?.revision, enabled]);
  useEffect(() => {
    scope.current = p?.id;
    history.current = {items: []};
    setSnapshot(null);
    setError('');
    void read();
    return () => { ++sequence.current; reading.current = null; reread.current = false; scope.current = undefined; };
  }, [read]);
  useProjectUpdates(enabled ? p?.id : undefined, () => read());
  const latest = useMemo(() => new Map(
    snapshot && snapshot.projectId === p?.id
      ? snapshot.items.map(task => [imageTargetKey(task.target as VisualTarget), task]) : [],
  ), [snapshot, p?.id]);
  return <Context.Provider value={{ latest, ready: !enabled || snapshot?.projectId === p?.id, error, refresh: () => { void read(); } }}>
    {children}
  </Context.Provider>;
}

export function AssetImageTaskStatus({ target, image }: { target?: VisualTarget; image?: AssetImage | null | boolean }) {
  const tasks = useAssetImageTasks();
  const { c } = useCatalogText();
  const task = target ? tasks?.latest.get(imageTargetKey(target)) : undefined;
  const { busy, label } = imageTaskState(task, image);
  if (!label) return null;
  return <span className="asset-image-task" data-status={task?.status} role="status" aria-busy={busy || undefined}>
    {busy ? <LoaderCircle className="animate-spin" aria-hidden="true" />
      : task?.status === 'unknown' ? <Clock3 aria-hidden="true" /> : <CircleAlert aria-hidden="true" />}
    <span>{c(label)}</span>
  </span>;
}
