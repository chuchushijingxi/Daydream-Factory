import { useRef, useState } from 'react';
import { Check, Minus } from 'lucide-react';
import { api } from '../../api/client.ts';
import { type ProjectRow } from '../../api/types';
import { projectRequestMessage } from '../../api/errors';
import { useWorkspace } from '../../state/WorkspaceContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Button } from '../../components/ui/button';
import { ControlTooltip } from '../../components/ui/control-tooltip';
import { toast } from '../../components/ui/sonner';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../../components/ui/dialog';
export function ProjectSelectionMark({
  checked,
  mixed = false,
}: {
  checked: boolean;
  mixed?: boolean;
}) {
  return (
    <span className="project-selection-mark" data-checked={checked || mixed} aria-hidden>
      {mixed ? <Minus /> : checked ? <Check /> : null}
    </span>
  );
}
export function useProjectManagement() {
  const { c } = useCatalogText(),
    { projects, status, refreshProjects } = useWorkspace();
  const [managing, setManaging] = useState(false),
    [selected, setSelected] = useState(new Set<string>()),
    [confirming, setConfirming] = useState(false),
    [candidates, setCandidates] = useState<ProjectRow[]>([]),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [needsRefresh, setNeedsRefresh] = useState(false);
  const submitting = useRef(false),
    cancelButton = useRef<HTMLButtonElement>(null);
  const available = Boolean(
    status && !status.readOnly && status.capabilities.projectDeletion === true,
  );
  const visibleSelected = projects.filter((p) => selected.has(p.id)),
    all = projects.length > 0 && visibleSelected.length === projects.length;
  const toggle = (id: string) => {
    if (busy) return;
    setSelected((prev) => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  };
  const loadLatest = async () => {
    if (submitting.current) return;
    setBusy(true);
    try {
      await refreshProjects();
      setNeedsRefresh(false);
      setConfirming(false);
      setCandidates([]);
      setSelected(new Set());
      setError('');
    } catch (e) {
      setError(projectRequestMessage(e, c));
    } finally {
      setBusy(false);
    }
  };
  const remove = async () => {
    if (!available || submitting.current || needsRefresh || !candidates.length) return;
    submitting.current = true;
    setBusy(true);
    setError('');
    let removed = 0;
    try {
      // 恰好冻结本次确认中展示的版本；遇到第一个拒绝或结果不明即停止，
      // 不获取新修订、不重发。
      for (const p of candidates) {
        await api.deleteProject(p.id, {
          revision: p.revision,
        });
        removed++;
        setSelected((prev) => {
          const next = new Set(prev);
          next.delete(p.id);
          return next;
        });
      }
      setConfirming(false);
      setCandidates([]);
      setSelected(new Set());
      toast.success(
        c('已删除 {count} 个项目', {
          count: removed,
        }),
      );
    } catch (e) {
      setNeedsRefresh(true);
      const message = c('已删除 {count} 个项目；其余结果请刷新列表核对。{reason}', {
        count: removed,
        reason: projectRequestMessage(e, c),
      });
      setError(message);
      toast.error(message);
    } finally {
      try {
        await refreshProjects();
      } catch (e) {
        setNeedsRefresh(true);
        setError((previous) =>
          previous
            ? previous + ' ' + projectRequestMessage(e, c)
            : c('已删除 {count} 个项目；列表刷新失败：{reason}', {
                count: removed,
                reason: projectRequestMessage(e, c),
              }),
        );
      }
      submitting.current = false;
      setBusy(false);
    }
  };
  const controls = projects.length ? (
    <ControlTooltip
      label={
        status?.readOnly
          ? c('当前服务只读。')
          : !available
            ? c('项目删除接口尚未接入。')
            : c('管理项目')
      }
      disabled={!available}
    >
      <Button
        variant="secondary"
        size="sm"
        disabled={!available || busy}
        aria-pressed={managing}
        onClick={() => {
          setManaging((v) => !v);
          setSelected(new Set());
        }}
      >
        {c(managing ? '完成' : '管理')}
      </Button>
    </ControlTooltip>
  ) : null;
  const feedback =
    error && !confirming ? (
      <div role="alert" className="jm-inline-error px-10 pt-3">
        {error}
        <Button variant="link" size="sm" disabled={busy} onClick={() => void loadLatest()}>
          {c('重新读取')}
        </Button>
      </div>
    ) : null;
  const footer =
    managing && projects.length ? (
      <footer className="xy-workflow-dock project-manage-dock" aria-label={c('项目管理')}>
        <div className="xy-dock-inner">
          <button
            type="button"
            role="checkbox"
            aria-checked={all ? true : visibleSelected.length ? 'mixed' : false}
            className="project-select-all"
            disabled={busy}
            onClick={() => setSelected(all ? new Set() : new Set(projects.map((p) => p.id)))}
          >
            <ProjectSelectionMark checked={all} mixed={!all && visibleSelected.length > 0} />
            {c('全选')}
          </button>
          <span className="project-selection-count">
            {c('已选 {count} 项', {
              count: visibleSelected.length,
            })}
          </span>
          <Button
            variant="accent"
            size="sm"
            className="xy-dock-next"
            disabled={!available || busy || needsRefresh || !visibleSelected.length}
            onClick={() => {
              setCandidates(
                visibleSelected.map((p) => ({
                  ...p,
                })),
              );
              setConfirming(true);
              setError('');
            }}
          >
            {c('删除所选')}
          </Button>
        </div>
      </footer>
    ) : null;
  const dialog = (
    <Dialog
      open={confirming}
      onOpenChange={(open) => {
        if (!busy) setConfirming(open);
      }}
    >
      <DialogContent
        variant="confirm"
        className="project-delete-dialog"
        closeDisabled={busy}
        onEscapeKeyDown={(event) => {
          if (busy) event.preventDefault();
        }}
        onPointerDownOutside={(event) => {
          if (busy) event.preventDefault();
        }}
        onOpenAutoFocus={(event) => {
          event.preventDefault();
          cancelButton.current?.focus();
        }}
      >
        <DialogTitle>{c('删除所选项目？')}</DialogTitle>
        <DialogDescription>
          {c('将删除 {count} 个项目。项目将从列表移除，历史记录和费用保留。', {
            count: candidates.length,
          })}
        </DialogDescription>
        <ul className="project-delete-list">
          {candidates.map((p) => (
            <li key={p.id}>{p.title || p.filename}</li>
          ))}
        </ul>
        {error ? (
          <div role="alert" className="jm-inline-error">
            {error}
            <Button variant="link" size="sm" disabled={busy} onClick={() => void loadLatest()}>
              {c('重新读取')}
            </Button>
          </div>
        ) : null}
        <div className="project-delete-actions">
          <Button
            ref={cancelButton}
            variant="outline"
            disabled={busy}
            onClick={() => setConfirming(false)}
          >
            {c('取消')}
          </Button>
          <Button
            variant="destructive"
            disabled={!available || needsRefresh}
            busy={busy}
            onClick={() => void remove()}
          >
            {c('确认删除')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
  return {
    managing,
    selected,
    busy,
    toggle,
    controls,
    feedback,
    footer,
    dialog,
  };
}
