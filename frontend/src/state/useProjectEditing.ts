import { useCallback, useEffect, useRef, useState } from 'react';
import {
  sharedResourceApi,
  type ProjectEditing,
  type ProjectPermissions,
} from '../api/shared-resource';
import { ApiError } from '../api/client';
import { useCatalogText } from '../contexts/useCatalogText';

type EditingState = {
  id: string;
  permissions: ProjectPermissions | null;
  editing: ProjectEditing | null;
  error: string;
  denied?: boolean;
  idle?: boolean;
};

export function useProjectEditing(
  projectId: string | null,
  enabled: boolean,
  serviceReadOnly = false,
) {
  const { c } = useCatalogText();
  const [state, setState] = useState<EditingState>({
    id: '',
    permissions: null,
    editing: null,
    error: '',
  });
  const refreshRef = useRef<() => Promise<void>>(async () => {});
  const refresh = useCallback(() => refreshRef.current(), []);
  const live = useRef({
    projectId: '',
    enabled: false,
    owned: false,
    expiresAt: 0,
    foreground: true,
  });
  // 续租与释放串行，避免离页的迟到 DELETE 清掉随后重新获得的同会话租约。
  const commands = useRef<Promise<unknown>>(Promise.resolve());
  const canWrite = useCallback((expectedProjectId: string) => {
    const current = live.current;
    return (
      current.projectId === expectedProjectId &&
      current.enabled &&
      current.foreground &&
      !document.hidden &&
      current.owned &&
      current.expiresAt > Date.now()
    );
  }, []);

  useEffect(() => {
    setState({ id: projectId || '', permissions: null, editing: null, error: '' });
    live.current = {
      projectId: projectId || '',
      enabled: Boolean(projectId && enabled && !serviceReadOnly),
      owned: false,
      expiresAt: 0,
      foreground: !document.hidden,
    };
    if (!projectId || !enabled) return;
    let disposed = false,
      reading = false,
      rerun = false,
      owned = false,
      generation = 0;
    let lastActivity = Date.now();
    let expiryTimer: number | undefined;
    const clearLease = () => {
      owned = false;
      live.current.owned = false;
      live.current.expiresAt = 0;
      window.clearTimeout(expiryTimer);
    };
    const release = () => {
      const request = commands.current.then(() => sharedResourceApi.release(projectId));
      commands.current = request.catch(() => {});
      return commands.current;
    };
    const revoke = (releaseLease: boolean) => {
      ++generation;
      clearLease();
      setState((old) => ({ ...old, editing: null, error: '' }));
      if (releaseLease) void release();
    };
    const activity = () => {
      lastActivity = Date.now();
    };
    const poll = async () => {
      if (disposed || !live.current.foreground || document.hidden) return;
      if (reading) {
        rerun = true;
        return;
      }
      reading = true;
      const ticket = generation;
      try {
        const permissions = await sharedResourceApi.permissions(projectId);
        if (disposed || ticket !== generation) return;
        if (permissions.access !== 2 || serviceReadOnly) {
          const wasOwned = owned;
          clearLease();
          if (wasOwned) await release();
          if (!disposed && ticket === generation)
            setState({ id: projectId, permissions, editing: null, error: '' });
          return;
        }
        if (Date.now() - lastActivity >= 30 * 60 * 1000) {
          const wasOwned = owned;
          clearLease();
          if (wasOwned) await release();
          if (!disposed && ticket === generation)
            setState({ id: projectId, permissions, editing: null, error: '', idle: true });
          return;
        }
        const request = commands.current.then(() => {
          if (disposed || ticket !== generation || !live.current.foreground || document.hidden)
            return null;
          return sharedResourceApi.editing(
            projectId,
            owned && live.current.expiresAt > Date.now() ? 'PUT' : 'POST',
          );
        });
        commands.current = request.catch(() => {});
        const editing = await request;
        if (!editing || disposed || ticket !== generation) return;
        const expiresAt = editing.expiresAt ? Date.parse(editing.expiresAt) : 0;
        owned = editing.owned && expiresAt > Date.now();
        live.current.owned = owned;
        live.current.expiresAt = expiresAt;
        window.clearTimeout(expiryTimer);
        if (owned) expiryTimer = window.setTimeout(() => revoke(true), expiresAt - Date.now());
        setState({ id: projectId, permissions, editing: { ...editing, owned }, error: '' });
      } catch (error) {
        if (disposed || ticket !== generation) return;
        const wasOwned = owned;
        clearLease();
        if (wasOwned) void release();
        if (!disposed && ticket === generation)
          setState((old) => ({
            id: projectId,
            permissions: error instanceof ApiError && error.status === 404 ? null : old.permissions,
            denied: error instanceof ApiError && error.status === 404,
            editing: null,
            error: (error as Error).message,
          }));
      } finally {
        reading = false;
        if (rerun && !disposed) {
          rerun = false;
          void poll();
        }
      }
    };
    refreshRef.current = poll;
    void poll();
    const timer = window.setInterval(() => void poll(), 30000);
    const focused = () => {
      live.current.foreground = !document.hidden;
      // 重新显示时先关写入口，权限与租约都回执确认后才恢复。
      revoke(document.hidden);
      if (!document.hidden) void poll();
    };
    const blurred = () => {
      live.current.foreground = false;
      revoke(true);
    };
    const pagehide = () => {
      live.current.foreground = false;
      revoke(true);
    };
    for (const name of ['pointerdown', 'keydown', 'input', 'wheel'] as const)
      window.addEventListener(name, activity, { passive: true });
    window.addEventListener('focus', focused);
    window.addEventListener('blur', blurred);
    document.addEventListener('visibilitychange', focused);
    window.addEventListener('pagehide', pagehide);
    return () => {
      disposed = true;
      ++generation;
      live.current.enabled = false;
      clearLease();
      refreshRef.current = async () => {};
      window.clearInterval(timer);
      void release();
      for (const name of ['pointerdown', 'keydown', 'input', 'wheel'] as const)
        window.removeEventListener(name, activity);
      window.removeEventListener('focus', focused);
      window.removeEventListener('blur', blurred);
      document.removeEventListener('visibilitychange', focused);
      window.removeEventListener('pagehide', pagehide);
    };
  }, [projectId, enabled, serviceReadOnly]);
  const active = state.id === projectId ? state : null;
  const readOnly =
    !!projectId &&
    (!active?.permissions || active.permissions.access !== 2 || !canWrite(projectId));
  const reason = !projectId
    ? ''
    : active?.error ||
      (active?.idle ? c('长时间未操作，编辑占用已释放。') : '') ||
      (!active?.permissions
        ? c('正在核对项目权限…')
        : active.permissions.access < 2
          ? c('当前项目仅可查看。')
          : active.editing?.userId && !active.editing.owned
            ? c('{name} 正在编辑，你当前只能查看。', { name: active.editing.username })
            : readOnly
              ? c('正在获取项目编辑权…')
              : '');
  return {
    denied: active?.denied === true,
    permissions: active?.permissions ?? null,
    editing: active?.editing ?? null,
    readOnly,
    reason,
    refresh,
    canWrite,
  };
}
