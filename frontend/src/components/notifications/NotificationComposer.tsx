import { useEffect, useRef, useState } from 'react';
import { ImagePlus } from 'lucide-react';
import { Button } from '../ui/button';
import { SafeMarkdown } from './SafeMarkdown';
import { useCatalogText } from '../../contexts/useCatalogText';

export function NotificationComposer({
  body,
  onChange,
  disabled,
  onUpload,
  messageId,
}: {
  body: string;
  onChange(value: string): void;
  disabled: boolean;
  messageId?: string;
  onUpload?: (file: File, signal: AbortSignal) => Promise<string>;
}) {
  const { c } = useCatalogText();
  const input = useRef<HTMLInputElement>(null),
    editor = useRef<HTMLTextAreaElement>(null),
    controller = useRef<AbortController>(),
    active = useRef(true),
    guard = useRef(false);
  const [uploading, setUploading] = useState(false),
    [error, setError] = useState('');
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
      controller.current?.abort();
    };
  }, []);
  async function upload(file: File) {
    if (!onUpload || disabled || guard.current) return;
    guard.current = true;
    controller.current = new AbortController();
    setUploading(true);
    setError('');
    const start = editor.current?.selectionStart ?? body.length,
      end = editor.current?.selectionEnd ?? body.length;
    try {
      const url = await onUpload(file, controller.current.signal);
      if (!active.current || controller.current.signal.aborted) return;
      const alt = file.name.replace(/[\[\]\\\r\n]/g, ' '),
        insert = `\n\n![${alt}](${url})\n\n`;
      onChange(body.slice(0, start) + insert + body.slice(end));
      requestAnimationFrame(() => {
        if (active.current) {
          editor.current?.focus({ preventScroll: true });
          editor.current?.setSelectionRange(start + insert.length, start + insert.length);
        }
      });
    } catch (error) {
      if (active.current && !controller.current.signal.aborted)
        setError((error as Error).message || '图片上传结果未确认，请核对后重新选择。');
    } finally {
      guard.current = false;
      if (active.current) setUploading(false);
    }
  }
  return (
    <div className="notification-composer">
      <div className="notification-composer-head">
        <label htmlFor="notification-body">{c('通知正文')}</label>
        <Button
          type="button"
          variant="outline"
          size="sm"
          busy={uploading}
          disabled={disabled || !onUpload}
          onClick={() => input.current?.click()}
        >
          <ImagePlus size={14} />
          {c('上传图片')}
        </Button>
        <span>{c('实时预览')}</span>
      </div>
      <input
        ref={input}
        className="sr-only"
        type="file"
        accept="image/png,image/jpeg"
        aria-label={c('上传通知图片')}
        disabled={disabled || uploading || !onUpload}
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = '';
          if (file) void upload(file);
        }}
      />
      <div className="notification-composer-columns">
        <textarea
          ref={editor}
          id="notification-body"
          value={body}
          maxLength={20000}
          disabled={disabled || uploading}
          onChange={(event) => onChange(event.target.value)}
          placeholder={c(
            '## 消息标题\n\n在这里填写正文，支持直接换行。\n\n- 列表内容\n- **重点内容**\n\n上传图片后会自动插入图片语法。',
          )}
        />
        <section className="notification-composer-preview" aria-label={c('通知实时预览')}>
          {body ? (
            <SafeMarkdown messageId={messageId} operatorPreview>
              {body}
            </SafeMarkdown>
          ) : (
            <p className="notification-composer-placeholder">{c('填写内容后显示实时预览')}</p>
          )}
        </section>
      </div>
      <p className="op-notification-help">
        {c('Markdown：')}
        <code>## {c('标题')}</code> · <code>**{c('加粗')}**</code> · <code>- {c('列表')}</code> ·{' '}
        <code>[{c('文字')}]({c('链接')})</code>
        {c('。单次回车换行，空行另起一段；上传图片后自动插入。')}
      </p>
      {error ? (
        <p className="op-inline-error" role="alert">
          {c(error)}
        </p>
      ) : null}
    </div>
  );
}
