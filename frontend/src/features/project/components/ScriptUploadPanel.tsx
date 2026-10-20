import { useId, useState, type ReactNode, type RefObject } from 'react';
import { FileText, Info, Plus, X } from 'lucide-react';
import { Button } from '../../../components/ui/button';
import { ControlTooltip } from '../../../components/ui/control-tooltip';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '../../../components/ui/dialog';
import type { ServiceStatus } from '../../../api/types';
import { CreditStar } from '../../../components/ui/credit-star';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { formatInteger } from '../../../pages/credits/credit-format';
const assets = '/demo/app/images/xiaoyunque-upload/';
export function ScriptUploadPanel({
  file,
  inputRef,
  status,
  busy,
  dragging,
  onChoose,
  onFileChange,
  onDrop,
  onDragOver,
  onDragLeave,
  onRemove,
  onSave,
  settingsControls,
  fileLocked = false,
  saveBlocked = false,
  saveLabel,
  saveAmount,
  saveBusy = false,
}: {
  file: File | null;
  inputRef: RefObject<HTMLInputElement>;
  status: ServiceStatus | null;
  busy: boolean;
  dragging: boolean;
  onChoose: (file: File) => boolean;
  onFileChange: (event: React.ChangeEvent<HTMLInputElement>) => void;
  onDrop: (event: React.DragEvent<HTMLDivElement>) => void;
  onDragOver: (event: React.DragEvent<HTMLDivElement>) => void;
  onDragLeave: () => void;
  onRemove: () => void;
  onSave: () => void;
  settingsControls: ReactNode;
  fileLocked?: boolean;
  saveBlocked?: boolean;
  saveLabel?: string;
  saveAmount?: number;
  saveBusy?: boolean;
}) {
  const { c } = useCatalogText(),
    disabled = busy || !status || status.readOnly,
    selectionDisabled = disabled || fileLocked;
  const [pasting, setPasting] = useState(false),
    [text, setText] = useState('');
  const priceId = useId(),
    label = saveLabel || c('保存剧本');
  const size = file
    ? file.size < 1024 * 1024
      ? (file.size / 1024).toFixed(1) + ' KB'
      : (file.size / 1024 / 1024).toFixed(1) + ' MB'
    : '';
  const useText = () => {
    if (selectionDisabled || !text.trim()) return;
    const chosen = new File([text], 'pasted-script.txt', {
      type: 'text/plain;charset=utf-8',
    });
    if (onChoose(chosen)) {
      setPasting(false);
      setText('');
    }
  };
  return (
    <>
      <section className="xy-upload-panel" aria-label={c('剧本上传区')}>
        <header className="xy-upload-tabs">
          <div className="xy-upload-tab">
            <img src={assets + 'upload.svg'} alt="" aria-hidden />
            <span>{c('剧本解析')}</span>
          </div>
        </header>
        <div className="xy-upload-body">
          <input
            ref={inputRef}
            type="file"
            accept=".txt,.md,.docx,text/plain,text/markdown,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
            onChange={onFileChange}
            id="playlet-home-script-input"
            hidden
            disabled={selectionDisabled}
          />
          <div
            className="xy-upload-drop"
            data-dragging={dragging && !disabled}
            onDragOver={onDragOver}
            onDragLeave={onDragLeave}
            onDrop={onDrop}
          >
            {/* Exact border geometry observed in the source upload panel. */}
            <svg className="xy-upload-border" aria-hidden fill="none">
              <rect
                x="-0.5"
                y="-0.5"
                width="calc(100% + 1px)"
                height="calc(100% + 1px)"
                rx="16"
                stroke="currentColor"
                strokeWidth="1"
                strokeDasharray="6 6"
              />
            </svg>
            {!file ? (
              <>
                <button
                  type="button"
                  className="xy-upload-hit"
                  disabled={selectionDisabled}
                  aria-label={c('剧本解析')}
                  onClick={() => inputRef.current?.click()}
                />
                <div className="xy-upload-empty">
                  <span className="xy-upload-art" aria-hidden>
                    <img
                      className="xy-script-page xy-script-page--one"
                      src={assets + 'script-page-1.png'}
                      alt=""
                    />
                    <img
                      className="xy-script-page xy-script-page--two"
                      src={assets + 'script-page-2.png'}
                      alt=""
                    />
                    <span className="xy-upload-plus">
                      <Plus />
                    </span>
                  </span>
                  <div className="xy-upload-commands">
                    <span>{c('剧本解析')}</span>
                    <span className="xy-upload-separator" aria-hidden />{' '}
                    <button
                      type="button"
                      disabled={selectionDisabled}
                      onClick={() => setPasting(true)}
                    >
                      {c('粘贴文本')}
                    </button>
                  </div>
                  <p className="xy-upload-format">
                    {c('支持 TXT、Markdown、DOCX · 最大 20 MB')}{' '}
                    <ControlTooltip
                      label={c('选择文件或粘贴完整文本，保存后进入项目设置。')}
                    >
                      <span tabIndex={0} aria-label={c('格式要求')}>
                        <Info />
                      </span>
                    </ControlTooltip>
                  </p>
                </div>
              </>
            ) : (
              <div className="xy-upload-file">
                <FileText aria-hidden />
                <div>
                  <strong>{file.name}</strong>
                  <p>
                    {file.name.split('.').pop()?.toUpperCase()} · {size}
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  disabled={selectionDisabled}
                  aria-label={c('移除所选文件')}
                  onClick={onRemove}
                >
                  <X />
                </Button>
              </div>
            )}
          </div>
          <footer className="xy-upload-footer">
            {settingsControls}
            <Button
              className="xy-upload-save"
              variant="accent"
              disabled={disabled || !file || saveBlocked}
              busy={busy || saveBusy}
              aria-label={label}
              aria-describedby={saveAmount !== undefined ? priceId : undefined}
              onClick={onSave}
            >
              {label}
              {saveAmount !== undefined ? (
                <span id={priceId} className="xy-upload-price"
                  aria-label={c('{amount} 积分', { amount: formatInteger(String(saveAmount)) })}>
                  <CreditStar />
                  <span>{formatInteger(String(saveAmount))}</span>
                </span>
              ) : null}
            </Button>
          </footer>
        </div>
      </section>
      <Dialog open={pasting} onOpenChange={setPasting}>
        <DialogContent variant="form" className="xy-paste-dialog">
          <div className="xy-paste-header">
            <DialogTitle>{c('粘贴剧本')}</DialogTitle>
            <DialogDescription>
              {c('保留完整文本，确认后作为 TXT 文件保存。')}
            </DialogDescription>
          </div>
          <textarea
            className="jm-field-textarea xy-paste-text"
            aria-label={c('剧本正文')}
            value={text}
            disabled={disabled}
            onChange={(event) => setText(event.target.value)}
          />
          <div className="xy-paste-footer">
            <Button variant="outline" onClick={() => setPasting(false)}>
              {c('取消')}
            </Button>
            <Button disabled={disabled || !text.trim()} onClick={useText}>
              {c('使用文本')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
