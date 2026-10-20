import { useEffect, useState } from 'react';
import { Spinner } from '../../components/ui/spinner';
import { Check, Eye } from 'lucide-react';
import type { AssetImage, Project, VisualTarget } from '../../api/types';
import { protectedResourceUrl } from '../../api/client';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../../components/ui/dialog';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useProjectMedia } from './useProjectMedia';

export function ImageFinalPicker({
  target,
  image,
  onSaved,
  onBusy,
}: {
  target: VisualTarget;
  image?: AssetImage | null;
  onSaved?: (p: Project, revision: number) => void;
  onBusy?: (busy: boolean) => void;
}) {
  const { c } = useCatalogText();
  const [open, setOpen] = useState(false),
    [selected, setSelected] = useState(''),
    [preview, setPreview] = useState(''),
    [previewFailed, setPreviewFailed] = useState(false),
    [previewRetry, setPreviewRetry] = useState(0);
  const media = useProjectMedia(target, open, onSaved);
  const versions = media.items.flatMap((task) =>
    task.status === 'succeeded'
      ? (task.result?.files || []).map((file) => ({
          task,
          file,
          key: task.id + ':' + file.id,
        }))
      : [],
  );
  const current = versions.find((v) => v.file.fileId === image?.fileId);
  const key = selected || current?.key || versions[0]?.key || '',
    choice = versions.find((v) => v.key === key);
  useEffect(() => {
    if (open) setSelected('');
  }, [open]);
  useEffect(() => {
    onBusy?.(media.busy);
    return () => onBusy?.(false);
  }, [media.busy, onBusy]);
  const confirm = async () => {
    if (choice && (await media.adopt(choice.task, choice.file.id))) setOpen(false);
  };
  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className="image-final-entry"
        onClick={() => setOpen(true)}
      >
        {c('生成历史')}
      </Button>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!media.busy) setOpen(next);
        }}
      >
        <DialogContent
          variant="picker"
          className="media-versions-dialog image-finals-dialog"
          closeDisabled={media.busy}
        >
          <DialogTitle>{c('生成历史')}</DialogTitle>
          <DialogDescription className="sr-only">
            {c('选择图片后确认用于当前资产')}
          </DialogDescription>
          <div className="media-versions-scroll">
            <div className="media-version-grid" role="radiogroup" aria-label={c('生成图片')}>
              {versions.map(({ file, key: versionKey }, index) => (
                <article
                  className="media-version-card"
                  data-selected={key === versionKey}
                  key={versionKey}
                >
                  <button
                    type="button"
                    role="radio"
                    className="media-version-select"
                    aria-label={c('图片 {number}', {
                      number: versions.length - index,
                    })}
                    aria-checked={key === versionKey}
                    disabled={media.busy}
                    tabIndex={key === versionKey ? 0 : -1}
                    onClick={() => setSelected(versionKey)}
                    onKeyDown={(event) => {
                      if (
                        ![
                          'ArrowLeft',
                          'ArrowRight',
                          'ArrowUp',
                          'ArrowDown',
                          'Home',
                          'End',
                        ].includes(event.key)
                      )
                        return;
                      event.preventDefault();
                      const next =
                        event.key === 'Home'
                          ? 0
                          : event.key === 'End'
                            ? versions.length - 1
                            : (index +
                                (['ArrowRight', 'ArrowDown'].includes(event.key)
                                  ? 1
                                  : versions.length - 1)) %
                              versions.length;
                      setSelected(versions[next].key);
                      event.currentTarget
                        .closest('[role=radiogroup]')
                        ?.querySelectorAll<HTMLButtonElement>('[role=radio]')
                        [next]?.focus();
                    }}
                  >
                    {file.thumbnailUrl ? (
                      <img src={protectedResourceUrl(file.thumbnailUrl)} alt="" loading="lazy" />
                    ) : (
                      <span className="image-final-placeholder">{c('暂无预览')}</span>
                    )}
                    <span className="media-version-check" aria-hidden>
                      {key === versionKey ? <Check /> : null}
                    </span>
                    <span className="media-version-caption">
                      <strong>
                        {c('图片 {number}', {
                          number: versions.length - index,
                        })}
                      </strong>
                      {file.fileId === image?.fileId ? <small>{c('当前定稿')}</small> : null}
                    </span>
                  </button>
                  <button
                    type="button"
                    className="media-version-preview"
                    aria-label={c('预览图片 {number}', {
                      number: versions.length - index,
                    })}
                    onClick={() => {
                      setPreviewFailed(false);
                      setPreviewRetry(0);
                      setPreview(file.contentUrl);
                    }}
                  >
                    <Eye />
                    {c('预览')}
                  </button>
                </article>
              ))}
            </div>
            {!versions.length && !media.reading && !media.error ? (
              <p className="media-version-empty">{c('暂无可选图片')}</p>
            ) : null}
            {media.reading && !versions.length ? (
              <div role="status" className="image-final-loading">
                <Spinner />
                {c('正在读取…')}
              </div>
            ) : null}
            {media.cursor ? (
              <Button variant="ghost" onClick={() => void media.more()} disabled={media.busy}>
                {c('更多图片')}
              </Button>
            ) : null}
          </div>
          {choice?.task.stale ? (
            <p className="media-warning">{c('提示词已更新，请重新生成')}</p>
          ) : null}
          {media.error ? (
            <p role="alert" className="jm-inline-error">
              {media.error}
              <Button variant="link" onClick={() => void media.read().catch(() => {})}>
                {c('重新读取')}
              </Button>
            </p>
          ) : null}
          <footer className="asset-batch-actions">
            <Button variant="outline" disabled={media.busy} onClick={() => setOpen(false)}>
              {c('取消')}
            </Button>
            <Button
              aria-label={c('确认定稿')}
              disabled={
                !choice ||
                choice.task.stale ||
                !media.available ||
                media.reading ||
                media.busy ||
                choice.file.fileId === image?.fileId
              }
              busy={media.busy}
              onClick={() => void confirm()}
            >
              {c('确认')}
            </Button>
          </footer>
        </DialogContent>
      </Dialog>
      <Dialog
        open={!!preview}
        onOpenChange={(next) => {
          if (!next) setPreview('');
        }}
      >
        <DialogContent variant="preview" className="media-preview-dialog">
          <DialogTitle className="sr-only">{c('生成结果预览')}</DialogTitle>
          <DialogDescription className="sr-only">{c('完整原始媒体')}</DialogDescription>
          {previewFailed ? (
            <div role="alert" className="image-final-loading">
              {c('媒体读取失败')}
              <Button
                variant="outline"
                onClick={() => {
                  setPreviewFailed(false);
                  setPreviewRetry((v) => v + 1);
                }}
              >
                {c('重新读取')}
              </Button>
            </div>
          ) : preview ? (
            <img
              key={previewRetry}
              src={protectedResourceUrl(preview)}
              alt={c('生成原图')}
              onError={() => setPreviewFailed(true)}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  );
}
