import { billingApi } from '../../api/billing-client';
import { useChargeQuote } from '../billing/useChargeQuote';
import { useEffect, useRef, useState } from 'react';
import { api, storyboardApi } from '../../api/client';
import { needsProjectReadback, projectRequestMessage } from '../../api/errors';
import { toast } from '../../components/ui/sonner';
import { useCatalogText } from '../../contexts/useCatalogText';
import { workflow } from '../../domain/project-workflow';
import { indexReady } from '../../domain/storyboard';
import { useWorkspace } from '../../state/WorkspaceContext';

// 剧本解析与后续按需补齐共享单次启动、冲突回读和防重复边界。
export function useAssetPreparation() {
  const { c } = useCatalogText();
  const {
    currentProject: project,
    status,
    storyboardIndex,
    acceptProject,
    refreshCurrentProject,
    refreshStoryboardIndex,
  } = useWorkspace();
  const [sending, setSending] = useState(false),
    [error, setError] = useState(''),
    [readback, setReadback] = useState(false),
    [indexRetryBlocked, setIndexRetryBlocked] = useState(false);
  const generation = useRef(0),
    submitting = useRef(false);
  useEffect(() => {
    generation.current++;
    submitting.current = false;
    setSending(false);
    setError('');
    setReadback(false);
    setIndexRetryBlocked(false);
    return () => {
      generation.current++;
    };
  }, [project?.id, project?.source.version]);
  const flow = workflow.state(project),
    readonly = !status || status.readOnly;
  const task = workflow.catalogReady(project) ? 'visual' : 'extraction';
  const pending = workflow.pendingTargets(project).length;
  const emptyVisual = task === 'visual' && project?.assets.length === 0;
  const selectionMissing = !project?.settings?.llmModel && !emptyVisual;
  const modelMissing = !status?.configured && !emptyVisual;
  const taskAvailable = task !== 'visual' || status?.capabilities.visualPrompts === true;
  const indexing =
    !!status?.capabilities.storyboard &&
    (!storyboardIndex ||
      (!!(storyboardIndex.index || storyboardIndex.latestIndexRun) &&
        !indexReady(storyboardIndex, project?.source.version)));
  const indexRun =
    storyboardIndex?.latestIndexRun?.sourceVersion === project?.source.version
      ? storyboardIndex?.latestIndexRun
      : null;
  const retryIndex =
    indexing &&
    !!indexRun &&
    ['failed', 'cancelled'].includes(indexRun.status) &&
    indexRun.errorCode !== 'MODEL_RESULT_UNKNOWN' &&
    !indexRetryBlocked;
  const blocked =
    !project ||
    readonly ||
    sending ||
    readback ||
    (indexing && !retryIndex) ||
    !taskAvailable ||
    selectionMissing ||
    modelMissing ||
    !!flow.active ||
    !!(flow.failed && flow.failed.contract !== workflow.contracts.extraction) ||
    !!(flow.cancelled && flow.cancelled.contract !== workflow.contracts.extraction) ||
    flow.extraction?.errorCode === 'MODEL_RESULT_UNKNOWN' ||
    flow.visual?.errorCode === 'MODEL_RESULT_UNKNOWN' ||
    (!retryIndex && task === 'visual' && project.assets.length > 0 && pending === 0);
  const quote = useChargeQuote(
    () =>
      billingApi.project(project!.id, {
        revision: project!.revision,
        operation: retryIndex
          ? 'episode-index'
          : task === 'visual'
            ? 'visual'
            : 'asset-preparation',
      }),
    JSON.stringify([project?.id, project?.revision, task, retryIndex]),
    !!project &&
      !readonly &&
      !flow.active &&
      !selectionMissing &&
      !modelMissing &&
      (!indexing || retryIndex),
  );
  const reload = async () => {
    if (!project || submitting.current) return;
    const ticket = generation.current;
    submitting.current = true;
    setSending(true);
    try {
      await refreshCurrentProject(project.id);
      if (ticket === generation.current) setReadback(false);
    } catch (e) {
      if (ticket === generation.current) setError((e as Error).message);
    } finally {
      if (ticket === generation.current) {
        submitting.current = false;
        setSending(false);
      }
    }
  };
  const execute = async (cancel: boolean) => {
    if (
      !project ||
      submitting.current ||
      (cancel ? readonly || sending || readback || !flow.active : blocked || quote.blocked)
    )
      return;
    const ticket = generation.current,
      id = project.id,
      activeID = flow.active?.id;
    submitting.current = true;
    setSending(true);
    setError('');
    try {
      let revision = project.revision;
      if (cancel) {
        const latest = await refreshCurrentProject(id);
        if (ticket !== generation.current) return;
        if (workflow.state(latest).active?.id !== activeID)
          throw new Error(c('当前运行已变化，请核对最新记录。'));
        revision = latest.revision;
      }
      if (!cancel && retryIndex) {
        await storyboardApi.retryIndex(id, revision, quote.basis);
        if (ticket !== generation.current) return;
        await refreshCurrentProject(id);
        if (ticket === generation.current) await refreshStoryboardIndex();
        return;
      }
      const method = cancel
        ? api.cancelTask
        : task === 'visual'
          ? api.startVisualPrompts
          : api.startExtraction;
      const result = await method(id, {
        revision,
        ...(!cancel && quote.basis ? { billingBasis: quote.basis } : {}),
      });
      if (ticket === generation.current) acceptProject(result);
    } catch (e) {
      if (ticket !== generation.current) return;
      if (retryIndex && (e as { code?: string }).code === 'MODEL_RESULT_UNKNOWN')
        setIndexRetryBlocked(true);
      if ((e as { code?: string }).code === 'BILLING_BASIS_CHANGED') quote.refresh();
      const message = projectRequestMessage(e, c);
      setError(message);
      toast.error(message, { id: 'task-' + id });
      if (needsProjectReadback(e)) {
        setReadback(true);
        try {
          await refreshCurrentProject(id);
          if (ticket === generation.current) setReadback(false);
        } catch {
          /* 保留原始错误与回读入口，不能重发不明启动。 */
        }
      }
    } finally {
      if (ticket === generation.current) {
        submitting.current = false;
        setSending(false);
      }
    }
  };
  return {
    project,
    status,
    flow,
    readonly,
    task,
    existing: flow[task],
    pending,
    emptyVisual,
    selectionMissing,
    modelMissing,
    taskAvailable,
    indexing,
    retryIndex,
    quote,
    blocked: blocked || quote.blocked,
    sending,
    error,
    readback,
    reload,
    start: () => execute(false),
    cancel: () => execute(true),
  };
}
