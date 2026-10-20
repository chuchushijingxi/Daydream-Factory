import { useEffect, useRef, useState } from 'react';
import { api } from '../../../api/client.ts';
import { needsProjectReadback, projectRequestMessage } from '../../../api/errors';
import type { VisualStyle } from '../../../api/types';
import { useWorkspace } from '../../../state/WorkspaceContext';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { Button } from '../../../components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '../../../components/ui/dialog';
import { CreationSettingsControls, EMPTY_CREATION_SETTINGS } from './CreationSettingsControls';

// 已选择项只展示；未选择项可补齐，任务仍须用户另一次明确点击启动。
export function ProjectChoicesDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { currentProject: p, acceptProject, refreshCurrentProject, status } = useWorkspace(),
    { c } = useCatalogText();
  const [settings, setSettings] = useState(EMPTY_CREATION_SETTINGS),
    [style, setStyle] = useState<VisualStyle | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [readback, setReadback] = useState(false),
    [revision, setRevision] = useState(0);
  const submitting = useRef(false);
  useEffect(() => {
    if (open && p) {
      setSettings(p.settings || EMPTY_CREATION_SETTINGS);
      setStyle(p.style);
      setRevision(p.revision);
      setError('');
      setReadback(false);
    }
  }, [open, p?.id]);
  if (!p) return null;
  const running = p.runs.some((r) => r.status === 'running'),
    disabled =
      !status || !status.capabilities.projectSettings || status.readOnly || running || busy;
  const changed =
    (Object.keys(settings) as (keyof typeof settings)[]).some(
      (key) => settings[key] !== (p.settings?.[key] || ''),
    ) || Boolean(style && !p.style);
  const reload = async () => {
    if (submitting.current) return;
    submitting.current = true;
    setBusy(true);
    try {
      const saved = await refreshCurrentProject(p.id);
      setSettings(
        (current) =>
          Object.fromEntries(
            (Object.keys(current) as (keyof typeof current)[]).map((key) => [
              key,
              saved.settings?.[key] || current[key],
            ]),
          ) as typeof current,
      );
      setStyle(saved.style || style);
      setRevision(saved.revision);
      setReadback(false);
      setError('');
    } catch (e) {
      setError(projectRequestMessage(e, c));
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  };
  const save = async () => {
    if (disabled || readback || submitting.current) return;
    submitting.current = true;
    setBusy(true);
    setError('');
    let saved = p;
    try {
      saved = await api.saveSettings(p.id, { revision, settings });
      acceptProject(saved);
      setRevision(saved.revision);
      if (style && !saved.style) {
        saved = await api.saveStyle(saved.id, {
          revision: saved.revision,
          styleId: style.id,
          promptVersion: style.promptVersion,
        });
        acceptProject(saved);
        setRevision(saved.revision);
      }
      onOpenChange(false);
    } catch (e) {
      setError(projectRequestMessage(e, c));
      if (needsProjectReadback(e)) setReadback(true);
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!busy) onOpenChange(value);
      }}
    >
      <DialogContent
        variant="settings"
        className="xy-project-choices-dialog"
        closeDisabled={busy}
      >
        <DialogTitle>{c('选择项目参数')}</DialogTitle>
        <DialogDescription>
          {c('项目使用你确认的风格、比例与模型；已选择项保持固定。')}
        </DialogDescription>
        <CreationSettingsControls
          value={settings}
          onChange={(key, value) => setSettings((current) => ({ ...current, [key]: value }))}
          style={style}
          onStyleChange={setStyle}
          disabled={disabled}
          confirmed={p.settings}
          styleLocked={Boolean(p.style)}
        />
        {error ? (
          <p role="alert" className="jm-inline-error">
            {error}
            {readback ? (
              <Button variant="link" size="sm" disabled={busy} onClick={() => void reload()}>
                {c('读取最新版本')}
              </Button>
            ) : null}
          </p>
        ) : null}
        <div className="xy-project-choices-actions">
          <Button variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>
            {c('取消')}
          </Button>
          <Button
            disabled={disabled || readback || !changed}
            busy={busy}
            onClick={() => void save()}
          >
            {c('保存')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
