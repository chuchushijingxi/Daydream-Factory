import { useCallback, useEffect, useRef, useState } from 'react';
import { sharedResourceApi, type ProjectPermissions } from '../api/shared-resource';
import { ApiError } from '../api/client';
import { useCatalogText } from '../contexts/useCatalogText';

type PermissionState = {
  id: string;
  permissions: ProjectPermissions | null;
  error: string;
  denied?: boolean;
};

// 进入项目时读取一次团队空间权限，只用于“仅可查看”与共享入口；
// 不做多人编辑占用。读取期间按可编辑渲染，真正的写入仍由服务端按权限与 revision 把关。
export function useProjectPermissions(projectId: string | null, enabled: boolean) {
  const { c } = useCatalogText();
  const [state, setState] = useState<PermissionState>({ id: '', permissions: null, error: '' });
  const refreshRef = useRef<() => Promise<void>>(async () => {});
  const refresh = useCallback(() => refreshRef.current(), []);

  useEffect(() => {
    setState({ id: projectId || '', permissions: null, error: '' });
    if (!projectId || !enabled) return;
    let disposed = false,
      ticket = 0;
    const read = async () => {
      const current = ++ticket;
      try {
        const permissions = await sharedResourceApi.permissions(projectId);
        if (!disposed && current === ticket)
          setState({ id: projectId, permissions, error: '' });
      } catch (error) {
        if (disposed || current !== ticket) return;
        const denied = error instanceof ApiError && error.status === 404;
        setState((old) => ({
          id: projectId,
          permissions: denied ? null : old.id === projectId ? old.permissions : null,
          denied,
          error: (error as Error).message,
        }));
      }
    };
    refreshRef.current = read;
    void read();
    return () => {
      disposed = true;
      refreshRef.current = async () => {};
    };
  }, [projectId, enabled]);

  const active = state.id === projectId ? state : null;
  const viewOnly = !!active?.permissions && active.permissions.access !== 2;
  return {
    denied: active?.denied === true,
    permissions: active?.permissions ?? null,
    readOnly: !!projectId && viewOnly,
    reason: !projectId ? '' : viewOnly ? c('当前项目仅可查看。') : active?.error || '',
    refresh,
  };
}
