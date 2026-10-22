import { workflow } from '../../../domain/project-workflow';
import { useEffect, useRef, useState } from 'react';
import { api } from '../../../api/client';
import {
  needsProjectReadback,
  projectRequestMessage,
  writeResultUnknown,
} from '../../../api/errors';
import type { Project, UploadedFile, VisualTarget } from '../../../api/types';
import { toast } from '../../../components/ui/sonner';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { useWorkspace } from '../../../state/WorkspaceContext';

// 目录卡片与详情页上传共用同一文件回执与修订恢复。
export function useAssetImageUpload({
  target,
  active = true,
  onSaved,
}: {
  target?: VisualTarget;
  editable: boolean;
  active?: boolean;
  onSaved?: (project: Project, expectedRevision: number) => void;
}) {
  const { c } = useCatalogText();
  const { currentProject: project, status, acceptProject, refreshCurrentProject } = useWorkspace();
  const controller = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [pending, setPending] = useState<UploadedFile | null>(null);
  const [readback, setReadback] = useState(false);
  const scope = project?.id + ':' + target?.assetId + ':' + target?.lookId;
  useEffect(() => {
    setBusy(false);
    setPending(null);
    setError('');
    setReadback(false);
    return () => controller.current?.abort();
  }, [scope, active]);
  const exists = project?.assets.some(
    (a) =>
      a.id === target?.assetId && (!target.lookId || a.looks.some((l) => l.id === target.lookId)),
  );
  const enabled = Boolean(
    active &&
      target &&
      exists &&
      project &&
      workflow.catalogReady(project) &&
      !project.runs.some((r) => r.status === 'running' && r.contract !== 'storyboard-agent-v1') &&
      status?.capabilities.assetImages &&
      !status.readOnly,
  );
  const permission = useRef(false);
  permission.current = enabled;
  const failed = (e: unknown, hasFile: boolean) => {
    setError(
      hasFile
        ? c('图片已上传，关联未确认。 {reason}', { reason: projectRequestMessage(e, c) })
        : writeResultUnknown(e)
          ? c('文件上传结果未确认，请勿重复提交。')
          : projectRequestMessage(e, c),
    );
    setReadback(hasFile && needsProjectReadback(e));
  };
  const associate = async (file: UploadedFile, revision: number, signal: AbortSignal) => {
    if (!project || !target || signal.aborted) return;
    if (!permission.current) throw new Error(c('资产已只读，图片文件保留，未关联到资产。'));
    const result = await api.saveAssetImage(
      project.id,
      target,
      { revision, fileId: file.fileId },
      signal,
    );
    if (signal.aborted) return;
    acceptProject(result);
    onSaved?.(result, revision);
    setPending(null);
    setError('');
    setReadback(false);
    toast.success(c('图片已保存'));
  };
  const upload = async (file: File) => {
    if (!enabled || busy || !project) return;
    const operation = new AbortController();
    controller.current = operation;
    setBusy(true);
    setError('');
    setPending(null);
    setReadback(false);
    let uploaded: UploadedFile | null = null;
    try {
      uploaded = await api.uploadFile(file, { thumbnail: true }, operation.signal);
      if (operation.signal.aborted) return;
      setPending(uploaded);
      await associate(uploaded, project.revision, operation.signal);
    } catch (e) {
      if (!operation.signal.aborted) failed(e, Boolean(uploaded));
    } finally {
      if (!operation.signal.aborted) setBusy(false);
    }
  };
  const select = async (file: UploadedFile) => {
    if (!enabled || busy || !project) return;
    const operation = new AbortController();
    controller.current = operation;
    setBusy(true);
    setPending(file);
    setReadback(false);
    setError('');
    try {
      await associate(file, project.revision, operation.signal);
    } catch (e) {
      if (!operation.signal.aborted) {
        failed(e, true);
        throw e;
      }
    } finally {
      if (!operation.signal.aborted) setBusy(false);
    }
  };
  const recover = async () => {
    if (!project || !target || busy || !pending) return;
    const operation = new AbortController();
    controller.current = operation;
    setBusy(true);
    try {
      if (readback) {
        const latest = await refreshCurrentProject(project.id);
        if (operation.signal.aborted) return;
        const asset = latest.assets.find((a) => a.id === target.assetId);
        const saved = target.lookId
          ? asset?.looks.find((l) => l.id === target.lookId)?.image
          : asset?.image;
        if (saved?.fileId === pending.fileId) {
          // 回读确认图片已被采用，但不推进无关的文本草稿。
          setPending(null);
          setError('');
          toast.success(c('图片已保存'));
        } else setError(c('最新版本已读取，请核对后重试关联。'));
        setReadback(false);
      } else await associate(pending, project.revision, operation.signal);
    } catch (e) {
      if (!operation.signal.aborted) failed(e, true);
    } finally {
      if (!operation.signal.aborted) setBusy(false);
    }
  };
  return { scope, busy, error, pending, readback, enabled, upload, select, recover };
}
