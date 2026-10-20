import { useState } from 'react';
import type { SceneInput } from '../../api/storyboard-types';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { useCatalogText } from '../../contexts/useCatalogText';
export function SceneDialog({
  disabled,
  onClose,
  onSave,
}: {
  disabled: boolean;
  onClose: () => void;
  onSave: (value: Pick<SceneInput, 'title' | 'summary'>) => Promise<boolean>;
}) {
  const { c } = useCatalogText();
  const [value, setValue] = useState({ title: '', summary: '' }),
    [error, setError] = useState(''),
    [open, setOpen] = useState(true);
  const close = () => {
    if (!disabled) setOpen(false);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) close();
      }}
    >
      <DialogContent
        variant="form"
        onCloseAutoFocus={() => onClose()}
        closeDisabled={disabled}
        className="sb-scene-dialog"
      >
        <DialogTitle>{c('新增场景')}</DialogTitle>
        <DialogDescription className="sr-only">
          {c('填写场景名称和说明。')}
        </DialogDescription>
        <div className="sb-form-scroll">
          <label className="sb-field">
            <span>{c('场景名称')}</span>
            <input
              className="jm-field-input"
              value={value.title}
              disabled={disabled}
              required
              onChange={(e) => setValue({ ...value, title: e.target.value })}
            />
          </label>
          <label className="sb-field">
            <span>{c('说明')}</span>
            <textarea
              className="jm-field-textarea"
              rows={4}
              value={value.summary}
              disabled={disabled}
              onChange={(e) => setValue({ ...value, summary: e.target.value })}
            />
          </label>
        </div>
        {error ? (
          <p role="alert" className="jm-inline-error">
            {error}
          </p>
        ) : null}
        <footer className="sb-actions">
          <Button variant="outline" disabled={disabled} onClick={close}>
            {c('取消')}
          </Button>
          <Button
            disabled={disabled || !value.title.trim()}
            busy={disabled}
            onClick={() =>
              void onSave(value).then((ok) => {
                if (ok) setOpen(false);
                else setError(c('保存未确认，输入保留。请关闭后核对页面错误和最新内容。'));
              })
            }
          >
            {c('保存场景')}
          </Button>
        </footer>
      </DialogContent>
    </Dialog>
  );
}
