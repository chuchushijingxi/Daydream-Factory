import { sessionBoundary } from '../../api/session-boundary';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useProjectUpdates } from '../../features/projects/useProjectUpdates';
import { api, storyboardApi } from '../../api/client';
import type {
  EpisodeSourceView,
  EpisodeView,
  ShotInput,
  StoryboardOperation,
  StoryShot,
} from '../../api/storyboard-types';
import { useWorkspace } from '../../state/WorkspaceContext';
import { shotInput, shotPatch } from '../../domain/storyboard';
import { translate } from '../../contexts/translate';
import { type ShotDraft } from '../../state/useStoryboardAutosave';
export function useProduction(episodeId: string, accepted?: EpisodeView) {
  const {
    currentProject: p,
    status,
    refreshCurrentProject,
    refreshStoryboardIndex,
    storyboardDrafts,
    storyboardDraftChanged,
    storyboardSaving,
  } = useWorkspace();
  const initial =
    accepted?.projectId === p?.id &&
    accepted?.episode.id === episodeId &&
    accepted?.sourceVersion === p?.source.version &&
    accepted.revision > (p?.revision || 0)
      ? accepted
      : null;
  const [snapshot, setView] = useState<EpisodeView | null>(null),
    [source, setSource] = useState<EpisodeSourceView | null>(null),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [loading, setLoading] = useState(false),
    [, redraw] = useState(0);
  const view =
    snapshot?.projectId === p?.id && snapshot?.episode.id === episodeId ? snapshot : null;
  const scope = useRef(''),
    ticket = useRef(0),
    submitting = useRef(false),
    viewRef = useRef(view);
  const key = (p?.id || '') + ':' + episodeId;
  scope.current = key;
  viewRef.current = view;
  const material = useRef<{ key: string; value: EpisodeSourceView } | null>(null);
  const available = !!status?.capabilities.storyboard;
  const read = useCallback(async () => {
    if (!p || !episodeId || !available) return;
    const target = p.id + ':' + episodeId,
      seq = ++ticket.current;
    setLoading(true);
    try {
      const v = await storyboardApi.getEpisode(p.id, episodeId);
      const materialKey = JSON.stringify([
        sessionBoundary.epoch,
        p.id,
        episodeId,
        v.sourceVersion,
        v.episode.sourceRanges,
        v.index.supportRanges,
      ]);
      const cached =
        v.index.supportRanges !== undefined && material.current?.key === materialKey
          ? material.current.value
          : null;
      // Fresh episode root facts accompany material cached only for this exact source/range identity.
      const s = cached
        ? {
            ...cached,
            projectId: v.projectId,
            revision: v.revision,
            sourceVersion: v.sourceVersion,
            access: v.access,
            activeRun: v.activeRun,
          }
        : await storyboardApi.getSource(p.id, episodeId);
      if (scope.current !== target || ticket.current !== seq) return;
      if (
        v.revision !== s.revision ||
        v.sourceVersion !== s.sourceVersion ||
        v.sourceVersion !== p.source.version
      )
        throw new Error(translate('分镜与原文版本不同，请重新读取；草稿保留。'));
      if (viewRef.current?.episode.id === v.episode.id && viewRef.current.revision > v.revision)
        return viewRef.current;
      for (const shot of v.shots) {
        const k = p.id + ':' + shot.id,
          d = storyboardDrafts.current.get(k);
        if (
          d &&
          !d.conflict &&
          d.revision <= v.revision &&
          !Object.keys(shotPatch(d.base, d.value)).length
        )
          storyboardDrafts.current.delete(k);
      }
      setView((old) => (old?.episode.id === v.episode.id && old.revision > v.revision ? old : v));
      material.current = { key: materialKey, value: s };
      setSource(s);
      setError('');
      return v;
    } catch (e) {
      if (scope.current === target && ticket.current === seq) setError((e as Error).message);
      throw e;
    } finally {
      if (scope.current === target && ticket.current === seq) setLoading(false);
    }
  }, [p?.id, p?.source.version, episodeId, available]);
  useEffect(() => {
    // 分集页带来的已受理回执先显示；材料回读失败不能把已启动的任务变成未启动。
    setView((old) =>
      initial && old?.episode.id === episodeId && old.revision > initial.revision ? old : initial,
    );
    setSource(null);
    setError('');
    void read().catch(() => {});
    return () => {
      ++ticket.current;
    };
  }, [read]);
  useEffect(() => {
    if (view && p && view.revision !== p.revision) void read().catch(() => {});
  }, [p?.revision, read]);
  useProjectUpdates(p?.id, () => read().catch(() => {}));
  const draftKey = (shotId: string) => (p?.id || '') + ':' + shotId;
  const draft = (s: StoryShot): ShotDraft => {
    const found = storyboardDrafts.current.get(draftKey(s.id)) as ShotDraft | undefined;
    return (
      found || {
        projectId: p!.id,
        episodeId,
        sourceVersion: view!.sourceVersion,
        revision: view!.revision,
        base: shotInput(s),
        value: shotInput(s),
        conflict: false,
        message: '',
      }
    );
  };
  const dirty = (s: StoryShot) => Object.keys(shotPatch(draft(s).base, draft(s).value)).length > 0;
  const change = (s: StoryShot, value: ShotInput) => {
    if (!status || status.readOnly) return;
    storyboardDrafts.current.set(draftKey(s.id), { ...draft(s), value });
    storyboardDraftChanged();
    redraw((n) => n + 1);
  };
  const hasDrafts = Array.from(storyboardDrafts.current.entries()).some(
    ([k, v]) =>
      k.startsWith((p?.id || '') + ':') &&
      (v as ShotDraft).episodeId === episodeId &&
      Object.keys(shotPatch((v as ShotDraft).base, (v as ShotDraft).value)).length,
  );
  const active = view?.activeRun || p?.runs.find((r) => r.status === 'running');
  const editable =
    !!view &&
    !status?.readOnly &&
    !!status &&
    available &&
    view.access.editable &&
    !active &&
    !busy;
  async function command(operation: () => Promise<unknown>) {
    if (submitting.current) return false;
    const target = scope.current;
    submitting.current = true;
    setBusy(true);
    setError('');
    try {
      await operation();
      if (scope.current === target) {
        await read();
        await refreshCurrentProject(p!.id);
        await refreshStoryboardIndex();
      }
      return true;
    } catch (e) {
      if (scope.current === target) {
        setError((e as Error).message);
        try {
          await read();
          await refreshCurrentProject(p!.id);
          await refreshStoryboardIndex();
        } catch {
          /* 原始错误保留；只读回，不重放命令。 */
        }
        setError((e as Error).message);
      }
      return false;
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  }
  const discard = (s: StoryShot) => {
    storyboardDrafts.current.delete(draftKey(s.id));
    storyboardDraftChanged();
    redraw((n) => n + 1);
  };
  const rebase = (s: StoryShot) => {
    if (!status || status.readOnly) return;
    const d = draft(s);
    if (!view || d.sourceVersion !== view.sourceVersion) return;
    const base = shotInput(s),
      edits = shotPatch(d.base, d.value);
    storyboardDrafts.current.set(draftKey(s.id), {
      ...d,
      revision: view.revision,
      base,
      value: { ...base, ...edits },
      conflict: false,
      message: '',
    });
    storyboardDraftChanged();
    redraw((n) => n + 1);
  };
  const changes = async (operations: StoryboardOperation[], revision = view?.revision) => {
    if (!p || !editable || hasDrafts || !!storyboardSaving || !revision) return false;
    return command(() => storyboardApi.changes(p.id, revision, operations));
  };
  const cancel = async () => {
    if (!p || !view?.activeRun || status?.readOnly || busy) return;
    const expected = view.activeRun.id;
    await command(async () => {
      const latest = await storyboardApi.getEpisode(p.id, episodeId);
      if (latest.activeRun?.id !== expected)
        throw new Error(translate('当前运行已变化，请回读后核对，未取消其他任务。'));
      return api.cancelTask(p.id, { revision: latest.revision });
    });
  };
  return {
    view,
    source,
    error,
    busy,
    loading,
    read,
    draft,
    dirty,
    change,
    hasDrafts,
    editable,
    saving: storyboardSaving,
    discard,
    rebase,
    changes,
    cancel,
  };
}
