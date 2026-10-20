import { useEffect, useId, useRef, useState } from 'react';
import { api } from '../../../api/client';
import { needsProjectReadback, projectRequestMessage } from '../../../api/errors';
import type { VisualTarget } from '../../../api/types';
import { toast } from '../../../components/ui/sonner';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { workflow } from '../../../domain/project-workflow';
import { useWorkspace } from '../../../state/WorkspaceContext';
import { MediaGenerator } from '../../media/MediaGenerator';
import { Button } from '../../../components/ui/button';

export function VisualPromptEditor({ target, text }: { target: VisualTarget; text: string }) {
  const { c } = useCatalogText();
  const { currentProject: project, status, acceptProject, refreshCurrentProject } = useWorkspace();
  const [draft, setDraft] = useState(text);
  const [revision, setRevision] = useState(project?.revision || 0);
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [readback, setReadback] = useState(false);
  const generation = useRef(0),
    field = useRef<HTMLTextAreaElement>(null),
    id = useId();
  const syncedText = useRef(text);
  // 父组件按 target 重新挂载本组件；卸载即作废迟到的保存/回读。
  useEffect(
    () => () => {
      generation.current++;
    },
    [],
  );
  // 后台生成和轮询回读应更新未编辑的正文；有本地草稿时保留原版本，
  // 仍由保存冲突后的明确回读核对并推进，不能用轮询静默覆盖并发更新。
  useEffect(() => {
    if (busy || readback || (draft !== syncedText.current && draft !== text)) return;
    syncedText.current = text;
    setDraft(text);
    setRevision(project?.revision || 0);
  }, [text, project?.revision, draft, busy, readback]);
  const targetExists = project?.assets.some(
    (a) =>
      a.id === target.assetId && (!target.lookId || a.looks.some((l) => l.id === target.lookId)),
  );
  const enabled =
    !!targetExists &&
    !!project &&
    !!status &&
    !status.readOnly &&
    status.capabilities.assetEditing &&
    workflow.state(project).editable;
  const dirty = draft !== text && draft.trim().length > 0;
  const saved = () => {
    setError('');
    setReadback(false);
    toast.success(c('提示词已保存'));
  };
  // 失焦保存没有重试按钮：冲突后自动回读最新版本；草稿保留，
  // 下次失焦再次保存。
  const readLatest = async (operation: number) => {
    try {
      const latest = await refreshCurrentProject(project!.id);
      if (operation !== generation.current) return;
      const current = workflow.prompt(latest, target);
      setRevision(latest.revision);
      setReadback(false);
      if (
        current?.origin === 'user' &&
        current.text === draft &&
        workflow.effectivePrompt(latest, target)
      )
        saved();
      else setError(c('最新版本已读取，草稿已保留。请核对当前正文后再保存。'));
    } catch (e) {
      if (operation === generation.current) {
        setError(projectRequestMessage(e, c));
        setReadback(true);
      }
    }
  };
  const save = async () => {
    if (!enabled || busy || readback || !dirty) return;
    const operation = generation.current;
    setBusy(true);
    setError('');
    try {
      const result = await api.saveVisualPrompt(project!.id, target, { revision, text: draft });
      if (operation !== generation.current) return;
      acceptProject(result);
      saved();
    } catch (e) {
      if (operation !== generation.current) return;
      setError(projectRequestMessage(e, c));
      if (needsProjectReadback(e)) {
        setReadback(true);
        await readLatest(operation);
      }
    } finally {
      if (operation === generation.current) setBusy(false);
    }
  };
  const blurred = () => {
    if (!enabled || busy) return;
    if (readback) {
      const operation = ++generation.current;
      setBusy(true);
      void readLatest(operation).finally(() => {
        if (operation === generation.current) setBusy(false);
      });
    } else if (dirty) void save();
  };
  return (
    <div className="visual-prompt-editor" aria-busy={busy}>
      <textarea
        id={id}
        ref={field}
        className="asset-prompt-input"
        value={draft}
        rows={9}
        disabled={busy || !enabled}
        aria-label={c('视觉提示词')}
        aria-invalid={!!error}
        aria-describedby={error ? id + '-error' : undefined}
        onChange={(e) => {
          setDraft(e.target.value);
          if (!readback) setError('');
        }}
        onBlur={blurred}
      />
      {error ? (
        <p id={id + '-error'} role="alert" className="jm-inline-error">
          {error}
        </p>
      ) : null}
      {error && text !== draft ? (
        <details className="visual-prompt-current">
          <summary>{c('查看当前已保存正文')}</summary>
          <p className="asset-detail-description">{text || c('尚无提示词正文。')}</p>
        </details>
      ) : null}
      <div className="asset-prompt-box-actions">
        <MediaGenerator
          target={target}
          kind="image"
          disabled={
            busy || readback || dirty || !project || !workflow.effectivePrompt(project, target)
          }
          renderEntry={(open, entry) => (
            <Button
              size="sm"
              aria-label={c(entry.label)}
              aria-describedby={entry.priceId}
              disabled={
                busy || readback || dirty || !project || !workflow.effectivePrompt(project, target)
              }
              onClick={open}
            >
              {c(entry.label)}
              {entry.price}
            </Button>
          )}
        />
      </div>
    </div>
  );
}
