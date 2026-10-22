import { sessionBoundary } from '../../api/session-boundary';
import { readMediaChanges, sameMediaTarget, type MediaSnapshot } from './readMediaChanges';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useProjectUpdates } from '../projects/useProjectUpdates';
import {
  projectMediaApi,
  type MediaCommand,
  type MediaTarget,
  type ProjectMedia,
} from '../../api/project-media';
import { ApiError } from '../../api/client';
import { projectRequestMessage } from '../../api/errors';
import type { Project } from '../../api/types';
import { useWorkspace } from '../../state/WorkspaceContext';
import { toast } from '../../components/ui/sonner';
import { translate } from '../../contexts/translate';
export function useProjectMedia(
  target: MediaTarget,
  enabled = true,
  onAdopt?: (project: Project, expectedRevision: number) => void,
) {
  const { currentProject: p, status, acceptProject, refreshCurrentProject } = useWorkspace();
  const key = JSON.stringify(target),
    kind = 'assetId' in target ? 'image' : 'video',
    scope = (p?.id || '') + ':' + key,
    scopeRef = useRef(scope);
  scopeRef.current = scope;
  const adoptedCallback = useRef(onAdopt);
  adoptedCallback.current = onAdopt;
  const storageKey = 'media-command:' + scope;
  const [items, setItems] = useState<ProjectMedia[]>([]),
    [reading, setReading] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [pending, setPending] = useState<MediaCommand | null>(null),
    [cursor, setCursor] = useState<string | null>(null),
    [billingSequence, setBillingSequence] = useState(0);
  const observing = useRef(false);
  observing.current = !!p && enabled && !!status?.capabilities.projectMedia;
  const projectRevision = useRef(p?.revision);
  projectRevision.current = p?.revision;
  const guard = useRef(false),
    seq = useRef(0),
    mounted = useRef(true),
    loadedMore = useRef(false),
    history = useRef<MediaSnapshot>({ items: [] }),
    inflight = useRef<{ scope: string; promise: Promise<void> } | null>(null),
    dirty = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const read = useCallback((): Promise<void> => {
    if (!observing.current) return Promise.resolve();
    if (inflight.current?.scope === scope) {
      dirty.current = true;
      return inflight.current.promise;
    }
    const promise = performRead().finally(() => {
      if (inflight.current?.promise !== promise) return;
      inflight.current = null;
      if (dirty.current && observing.current && mounted.current && scopeRef.current === scope) {
        dirty.current = false;
        void read().catch(() => {});
      }
    });
    inflight.current = { scope, promise };
    return promise;
    async function performRead() {
      if (!p || !enabled || !status?.capabilities.projectMedia) return;
      const epoch = sessionBoundary.epoch;
      const ticket = ++seq.current,
        requestScope = scope;
      setReading(true);
      try {
        const firstRead = history.current.head === undefined;
        const list = await readMediaChanges(
          p.id,
          kind,
          history.current,
          () =>
            observing.current &&
            mounted.current &&
            scopeRef.current === requestScope &&
            seq.current === ticket &&
            epoch === sessionBoundary.epoch,
          target,
          projectRevision.current,
        );
        if (
          !observing.current ||
          !mounted.current ||
          scopeRef.current !== requestScope ||
          seq.current !== ticket ||
          epoch !== sessionBoundary.epoch
        )
          return;
        history.current = list;
        if (list.revision !== projectRevision.current) dirty.current = true;
        setItems(list.items);
        if (!loadedMore.current && firstRead) setCursor(list.nextCursor);
        setError('');
      } catch (e) {
        if (mounted.current && scopeRef.current === requestScope && seq.current === ticket)
          setError((e as Error).message);
        throw e;
      } finally {
        if (mounted.current && scopeRef.current === requestScope && seq.current === ticket)
          setReading(false);
      }
    }
  }, [p?.id, enabled, status?.capabilities.projectMedia, scope, kind]);
  useEffect(() => {
    loadedMore.current = false;
    history.current = { items: [] };
    dirty.current = false;
    setItems([]);
    setReading(false);
    setCursor(null);
    setError('');
    setBusy(false);
    try {
      setPending(JSON.parse(sessionStorage.getItem(storageKey) || 'null'));
    } catch {
      setPending(null);
    }
  }, [storageKey]);
  useEffect(() => {
    void read().catch((e) => {
      if (scopeRef.current === scope) setError(e.message);
    });
    return () => {
      ++seq.current;
    };
  }, [read]);
  useEffect(() => {
    // 未确认命令沿原显式核对/幂等重试；改当前模型不触碰其冻结依据。
    if (
      !pending &&
      history.current.revision !== undefined &&
      history.current.revision !== p?.revision
    )
      void read().catch(() => {});
  }, [p?.revision, pending, read]);
  const targetItems = items.filter((t) => sameMediaTarget(t.target, target));
  const active = targetItems.find((t) => ['queued', 'running', 'unknown'].includes(t.status));
  useProjectUpdates(enabled ? p?.id : undefined, () => {
    return read().catch((e) => {
      if (scopeRef.current === scope) setError(e.message);
    });
  });
  const operation = async (fn: () => Promise<void>) => {
    if (guard.current) return false;
    guard.current = true;
    const at = scope;
    setBusy(true);
    setError('');
    try {
      await fn();
      return mounted.current && scopeRef.current === at;
    } catch (e) {
      if (mounted.current && scopeRef.current === at)
        setError(projectRequestMessage(e, (text) => translate(text)));
      return false;
    } finally {
      guard.current = false;
      if (mounted.current && scopeRef.current === at) setBusy(false);
    }
  };
  const submit = async (
    modelId: string,
    billingBasis?: string,
    retry = false,
    options?: import('../../api/types').MediaOptions,
    imageType?: import('../../api/types').CharacterImageType,
    aspectRatio?: string,
  ) =>
    operation(async () => {
      if (!p || status?.readOnly || !enabled) return;
      const command = retry
        ? pending
        : {
            commandId: crypto.randomUUID(),
            revision: p.revision,
            modelId,
            target,
            ...(imageType ? { imageType } : {}),
            ...(aspectRatio ? { aspectRatio } : {}),
            ...(options ? { options } : {}),
            ...(billingBasis ? { billingBasis } : {}),
          };
      if (!command) return;
      sessionStorage.setItem(storageKey, JSON.stringify(command));
      setPending(command);
      try {
        const v = await projectMediaApi.start(p.id, command);
        if (!mounted.current || scopeRef.current !== scope) return;
        setItems((old) => {
          const previous = old.find((task) => task.id === v.id);
          const current = previous && previous.revision > v.revision ? previous : v;
          const items = [current, ...old.filter((task) => task.id !== v.id)];
          history.current = { ...history.current, items };
          return items;
        });
        sessionStorage.removeItem(storageKey);
        setPending(null);
      } catch (e) {
        if (
          mounted.current &&
          scopeRef.current === scope &&
          e instanceof ApiError &&
          e.status >= 400 &&
          e.status < 500
        ) {
          sessionStorage.removeItem(storageKey);
          setPending(null);
          if (e.code === 'BILLING_BASIS_CHANGED') setBillingSequence((value) => value + 1);
        }
        throw e;
      }
    });
  const adopt = async (t: ProjectMedia, output: string) =>
    operation(async () => {
      if (!p || status?.readOnly || !enabled) return;
      try {
        const v = await projectMediaApi.adopt(p.id, t, output, p.revision);
        acceptProject(v);
        if (mounted.current && scopeRef.current === scope) adoptedCallback.current?.(v, p.revision);
        await read();
        toast.success(translate('已采用生成结果'));
      } catch (e) {
        await refreshCurrentProject(p.id).catch(() => {});
        await read().catch(() => {});
        throw e;
      }
    });
  const cancel = async (t: ProjectMedia) =>
    operation(async () => {
      if (!p || status?.readOnly || !enabled) return;
      await projectMediaApi.cancel(p.id, t);
      await read();
    });
  const more = async () =>
    operation(async () => {
      if (!p || !cursor) return;
      const ticket = seq.current,
        epoch = sessionBoundary.epoch;
      const v = await projectMediaApi.list(p.id, cursor, kind);
      if (
        !mounted.current ||
        scopeRef.current !== scope ||
        seq.current !== ticket ||
        epoch !== sessionBoundary.epoch
      )
        return;
      loadedMore.current = true;
      setItems((old) => {
        const items = [
          ...old,
          ...v.items.filter((task) => !old.some((existing) => existing.id === task.id)),
        ];
        history.current = { ...history.current, items };
        return items;
      });
      setCursor(v.nextCursor);
    });
  return {
    items: targetItems,
    reading,
    active,
    busy,
    error,
    pending,
    read,
    submit,
    adopt,
    cancel,
    more,
    cursor,
    billingSequence,
    available: !!status?.capabilities.projectMedia && !status?.readOnly,
  };
}
