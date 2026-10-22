import { useOptionalSpace } from './SpaceContext';
import { useProjectEditing } from './useProjectEditing';
import type { ProjectPermissions, ProjectEditing } from '../api/shared-resource';
import { ApiError } from '../api/client';
import { sessionBoundary } from '../api/session-boundary';
import { useStoryboardAutosave, type ShotDraft } from './useStoryboardAutosave';
import { useProjectUpdates } from '../features/projects/useProjectUpdates';
import { useCatalogText } from '../contexts/useCatalogText';
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { api, storyboardApi } from '../api/client.ts';
import type { StoryboardIndexView } from '../api/storyboard-types';
import {
  type ModelOptions,
  type Project,
  type ProjectRow,
  type ScriptSource,
  type ServiceStatus,
  type VisualStyle,
} from '../api/types';
import { toast } from '../components/ui/sonner';
type WorkspaceState = {
  theme: 'light' | 'dark';
  toggleTheme: () => void;
  projects: ProjectRow[];
  projectPermissions: ProjectPermissions | null;
  editing: ProjectEditing | null;
  projectReadOnlyReason: string;
  canShare: boolean;
  refreshProjectPermissions: () => Promise<void>;
  currentProject: Project | null;
  source: ScriptSource | null;
  status: ServiceStatus | null;
  models: ModelOptions;
  styles: VisualStyle[];
  error: string;
  busy: boolean;
  storyboardIndex: StoryboardIndexView | null;
  storyboardError: string;
  refreshStoryboardIndex: () => Promise<void>;
  storyboardDrafts: React.MutableRefObject<Map<string, ShotDraft>>;
  storyboardDraftChanged: () => void;
  storyboardSaving: string;
  refreshModels: () => Promise<void>;
  refreshProjects: () => Promise<void>;
  refreshCurrentProject: (id: string) => Promise<Project>;
  acceptProject: (p: Project) => void;
  handleSelectProject: (p: ProjectRow) => Promise<void>;
};
const WorkspaceContext = createContext<WorkspaceState | null>(null);
// 团队设置等可脱离工作台渲染的组件只借用已加载的模型目录，缺少提供者时返回 null。
export function useOptionalWorkspace() {
  return useContext(WorkspaceContext);
}
export function useWorkspace() {
  const value = useContext(WorkspaceContext);
  if (!value) throw new Error('WorkspaceProvider missing');
  return value;
}
export function WorkspaceProvider({
  children,
  load = 'workspace',
}: {
  children: ReactNode;
  load?: 'workspace' | 'operations' | 'none';
}) {
  const { c } = useCatalogText();
  const space = useOptionalSpace();
  const [theme, setTheme] = useState<'light' | 'dark'>(() =>
    localStorage.getItem('theme') === 'dark' ? 'dark' : 'light',
  );
  const [projects, setProjects] = useState<ProjectRow[]>([]),
    [currentProject, setProject] = useState<Project | null>(null),
    [source, setSource] = useState<ScriptSource | null>(null),
    [serviceStatus, setStatus] = useState<ServiceStatus | null>(null),
    [models, setModels] = useState<ModelOptions>({
      analysis: [],
      image: [],
      video: [],
    }),
    [styles, setStyles] = useState<VisualStyle[]>([]),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false);
  const { pathname } = useLocation();
  const projectId = pathname.startsWith('/playlet/') ? pathname.split('/')[2] : null;
  const [storyboardIndex, setStoryboardIndex] = useState<StoryboardIndexView | null>(null);
  const [storyboardError, setStoryboardError] = useState('');
  const navigate = useNavigate();
  // 请求序号隔离项目切换与迟到回读，低 revision 不能覆盖已保存响应。
  const sequence = useRef(0);
  const sourceCache = useRef<{ id: string; epoch: number; source: ScriptSource } | null>(null);
  const listSequence = useRef(0);
  const listEntered = useRef(pathname !== '/projects'),
    modelsEntered = useRef(pathname !== '/projects');
  const projectRef = useRef(currentProject);
  projectRef.current = currentProject;
  const pathRef = useRef(pathname);
  pathRef.current = pathname;

  const projectEditing = useProjectEditing(
    projectId,
    load === 'workspace' &&
      (!space || space.ready) &&
      currentProject?.id === projectId &&
      (!space || currentProject.spaceId === space.spaceId),
    serviceStatus?.readOnly === true,
  );
  useEffect(() => {
    if (!projectEditing.denied) return;
    ++sequence.current;
    setProject(null);
    setSource(null);
    sourceCache.current = null;
    setStoryboardIndex(null);
    setBusy(false);
    setError(c('无权限访问该项目。'));
    if (space) void space.refresh().catch(() => {});
  }, [projectEditing.denied]);
  const status = serviceStatus
    ? {
        ...serviceStatus,
        readOnly:
          serviceStatus.readOnly ||
          Boolean(space?.current?.readOnly) ||
          Boolean(projectId && projectEditing.readOnly),
      }
    : null;
  const refreshModels = async () => {
    const [s, m] = await Promise.all([api.getStatus(), api.listModels()]);
    setStatus(s);
    setModels(m);
  };
  const refreshProjects = async () => {
    const ticket = ++listSequence.current;
    const rows = await api.listProjects('all');
    if (ticket === listSequence.current) setProjects(rows);
  };
  const refreshCurrentProject = async (id: string) => {
    const ticket = ++sequence.current,
      epoch = sessionBoundary.epoch;
    setBusy(true);
    try {
      const p = await api.getProject(id);
      if (space && p.spaceId !== space.spaceId) {
        setProject(null);
        setSource(null);
        sourceCache.current = null;
        if (!(await space.switchSpace(p.spaceId, { keepRoute: true })))
          throw new Error(c('无权限访问该空间。'));
        throw new ApiError(0, 'SPACE_CHANGED', c('空间已切换，正在重新读取项目。'));
      }
      const cached = sourceCache.current;
      const s =
        cached?.id === id && cached.epoch === epoch && cached.source.version === p.source.version
          ? cached.source
          : await api.getSource(id);
      if (p.source.version !== s.version) throw new Error(c('原文版本已更新，请刷新后查看。'));
      if (ticket === sequence.current && pathRef.current.startsWith('/playlet/' + id + '/')) {
        if (projectRef.current?.id === id && projectRef.current.revision > p.revision) return p;
        setProject((previous) =>
          previous?.id === p.id && previous.revision > p.revision ? previous : p,
        );
        sourceCache.current = { id, epoch, source: s };
        setSource(s);
        setError('');
      }
      return p;
    } catch (e) {
      if (ticket === sequence.current && pathRef.current.startsWith('/playlet/' + id + '/')) {
        if (e instanceof ApiError && e.status === 404) {
          setProject(null);
          setSource(null);
          sourceCache.current = null;
          setStoryboardIndex(null);
        }
        setError((e as Error).message);
      }
      throw e;
    } finally {
      if (ticket === sequence.current) setBusy(false);
    }
  };
  const autosave = useStoryboardAutosave(status, refreshCurrentProject, projectEditing.canWrite);
  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark');
    localStorage.setItem('theme', theme);
  }, [theme]);
  useEffect(() => {
    if (load === 'none') return;
    if (load === 'operations') {
      let stopped = false;
      void api
        .getStatus()
        .then((s) => {
          if (!stopped) setStatus(s);
        })
        .catch((e) => {
          if (!stopped) setError(e.message);
        });
      return () => {
        stopped = true;
      };
    }
    let disposed = false;
    void Promise.all([api.getStatus(), refreshProjects(), api.listModels(), api.listStyles()])
      .then(([s, , m, v]) => {
        if (!disposed) {
          setStatus(s);
          setModels(m);
          setStyles(v.styles);
        }
      })
      .catch((e) => {
        if (!disposed) setError(e.message);
      });
    return () => {
      disposed = true;
    };
  }, [load]);
  useEffect(() => {
    if (load !== 'workspace' || pathname !== '/projects') return;
    let disposed = false,
      timer: number,
      reading = false;
    const poll = async () => {
      if (disposed || reading) return;
      reading = true;
      clearTimeout(timer);
      try {
        if (!document.hidden) await refreshProjects();
      } catch (e) {
        if (!disposed) toast.error((e as Error).message, { id: 'project-list-refresh' });
      }
      reading = false;
      if (!disposed) timer = window.setTimeout(poll, 30000);
    };
    if (listEntered.current) void poll();
    else timer = window.setTimeout(poll, 30000);
    listEntered.current = true;
    const visible = () => {
      if (!document.hidden) {
        clearTimeout(timer);
        void poll();
      }
    };
    document.addEventListener('visibilitychange', visible);
    return () => {
      disposed = true;
      ++listSequence.current;
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', visible);
    };
  }, [pathname, load]);
  useEffect(() => {
    if (load !== 'workspace' || pathname !== '/projects') return;
    const refresh = () => {
      if (document.hidden) return;
      void refreshModels().catch((e) =>
        toast.error(e.message, {
          id: 'model-refresh',
        }),
      );
    };
    if (modelsEntered.current) refresh();
    modelsEntered.current = true;
    window.addEventListener('focus', refresh);
    return () => window.removeEventListener('focus', refresh);
  }, [pathname, load]);
  const boardSequence = useRef(0);
  const refreshStoryboardIndex = async () => {
    if (!projectId || !status?.capabilities.storyboard) return;
    const ticket = ++boardSequence.current;
    try {
      const v = await storyboardApi.getIndex(projectId);
      if (
        ticket !== boardSequence.current ||
        !pathRef.current.startsWith('/playlet/' + projectId + '/')
      )
        return;
      setStoryboardIndex((old) =>
        old?.projectId === v.projectId && old.revision > v.revision ? old : v,
      );
      setStoryboardError('');
    } catch (e) {
      if (ticket === boardSequence.current) setStoryboardError((e as Error).message);
      throw e;
    }
  };
  useEffect(() => {
    if (status?.capabilities.storyboard && currentProject?.id === projectId)
      void refreshStoryboardIndex().catch(() => {});
  }, [projectId, currentProject?.revision, status?.capabilities.storyboard]);
  useEffect(() => {
    ++sequence.current;
    setProject(null);
    sourceCache.current = null;
    setSource(null);
    setStoryboardIndex(null);
    setStoryboardError('');
    ++boardSequence.current;
    if (load === 'workspace' && projectId) void refreshCurrentProject(projectId).catch(() => {});
    else setBusy(false);
    return () => {
      ++sequence.current;
    };
  }, [projectId, load]);
  useProjectUpdates(currentProject?.id, () => {
    if (currentProject) return refreshCurrentProject(currentProject.id).catch(() => {});
  });
  return (
    <WorkspaceContext.Provider
      value={{
        theme,
        toggleTheme: () => setTheme((v) => (v === 'dark' ? 'light' : 'dark')),
        projects,
        projectPermissions: projectEditing.permissions,
        editing: projectEditing.editing,
        projectReadOnlyReason: serviceStatus?.readOnly
          ? c('当前服务只读。')
          : space?.current?.readOnly
            ? c('团队已只读，内容可查看和下载。')
            : projectEditing.reason,
        canShare: projectEditing.permissions?.canShare === true,
        refreshProjectPermissions: projectEditing.refresh,
        currentProject,
        source,
        status,
        models,
        styles,
        error,
        busy,
        storyboardIndex,
        storyboardError,
        refreshStoryboardIndex,
        storyboardDrafts: autosave.drafts,
        storyboardDraftChanged: autosave.changed,
        storyboardSaving: autosave.saving,
        refreshModels,
        refreshProjects,
        refreshCurrentProject,
        acceptProject: (p) => {
          if (pathRef.current.startsWith('/playlet/' + p.id + '/')) {
            ++sequence.current;
            setBusy(false);
            setError('');
            setProject((previous) =>
              previous?.id === p.id && previous.revision > p.revision ? previous : p,
            );
          }
        },
        handleSelectProject: async (p) => {
          navigate(`/playlet/${p.id}/outline`);
        },
      }}
    >
      {children}
    </WorkspaceContext.Provider>
  );
}
