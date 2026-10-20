import { useEffect, useState } from 'react';
import { Check, Download, Play } from 'lucide-react';
import { protectedResourceUrl } from '../../api/client';
import { downloadProtected } from '../../api/download';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { toast } from '../../components/ui/sonner';
import { useCatalogText } from '../../contexts/useCatalogText';
import type { useProjectMedia } from './useProjectMedia';

type Media = ReturnType<typeof useProjectMedia>;
export function videoVersions(items: Media['items']) {
  return items.flatMap((task) =>
    task.status === 'succeeded' ? (task.result?.files || []).map((file) => ({ task, file })) : [],
  );
}
export function VideoVersionsDialog({
  open,
  onOpenChange,
  media,
  currentOutputId,
  onPreview,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  media: Media;
  currentOutputId?: string;
  onPreview: (url: string) => void;
}) {
  const { c } = useCatalogText();
  const versions = videoVersions(media.items),
    [selected, setSelected] = useState(''),
    [error, setError] = useState('');
  useEffect(() => {
    if (open) {
      setSelected(currentOutputId || versions[0]?.file.id || '');
      setError('');
    }
  }, [open]);
  useEffect(() => {
    if (open && media.error) setError(media.error);
  }, [open, media.error]);
  const choice = versions.find((v) => v.file.id === selected);
  const canAdopt =
    !!choice &&
    !choice.task.stale &&
    media.available &&
    !media.busy &&
    choice.task.adoptedOutputId !== choice.file.id;
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!media.busy) onOpenChange(value);
      }}
    >
      <DialogContent
        variant="picker"
        className="media-versions-dialog"
        data-single={versions.length <= 1}
        closeDisabled={media.busy}
      >
        <DialogTitle>{c('选择视频')}</DialogTitle>
        <DialogDescription className="sr-only">{c('选择视频后，确认用于当前镜头。')}</DialogDescription>
        <div className="media-versions-scroll">
          <div className="media-version-grid" role="radiogroup" aria-label={c('视频')}>
            {versions.map(({ task, file }, index) => (
              <article
                className="media-version-card"
                data-selected={selected === file.id}
                key={task.id + ':' + file.id}
              >
                <button
                  type="button"
                  role="radio"
                  aria-checked={selected === file.id}
                  aria-label={c('视频 {number}', { number: versions.length - index })}
                  tabIndex={selected === file.id ? 0 : -1}
                  className="media-version-select"
                  disabled={media.busy}
                  onClick={() => setSelected(file.id)}
                  onKeyDown={(event) => {
                    if (
                      !['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(
                        event.key,
                      )
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
                    setSelected(versions[next].file.id);
                    event.currentTarget
                      .closest('[role=radiogroup]')
                      ?.querySelectorAll<HTMLButtonElement>('[role=radio]')
                      [next]?.focus();
                  }}
                >
                  <video
                    style={{ aspectRatio: file.width + '/' + file.height }}
                    src={protectedResourceUrl(file.contentUrl)}
                    preload="metadata"
                    muted
                    playsInline
                    aria-hidden="true"
                  />
                  <span className="media-version-check" aria-hidden="true">
                    {selected === file.id ? <Check /> : null}
                  </span>
                  <span className="media-version-caption">
                    <strong>{c('视频 {number}', { number: versions.length - index })}</strong>
                    <time>
                      {new Date(task.createdAt).toLocaleString('zh-CN', {
                        month: 'numeric',
                        day: 'numeric',
                        hour: '2-digit',
                        minute: '2-digit',
                      })}
                    </time>
                    {task.adoptedOutputId === file.id ? <small>{c('已选用')}</small> : null}
                  </span>
                </button>
                <button
                  type="button"
                  className="media-version-preview"
                  aria-label={c('预览视频 {number}', { number: versions.length - index })}
                  onClick={() => onPreview(file.contentUrl)}
                >
                  <Play />
                  {c('预览')}
                </button>
                <Button
                  className="media-version-download"
                  variant="ghost"
                  size="icon"
                  aria-label={c('下载视频 {number}', { number: versions.length - index })}
                  onClick={() =>
                    void downloadProtected(file.contentUrl, c('生成视频.mp4')).catch((error) =>
                      toast.error(error.message),
                    )
                  }
                >
                  <Download />
                </Button>
              </article>
            ))}
          </div>
          {!versions.length ? <p className="media-version-empty">{c('暂无可选视频')}</p> : null}
          {media.cursor ? (
            <Button
              variant="ghost"
              size="sm"
              disabled={media.busy}
              onClick={() => void media.more()}
            >
              {c('更多视频')}
            </Button>
          ) : null}
        </div>
        {error || media.error ? (
          <p role="alert" className="jm-inline-error">
            {error || media.error}
          </p>
        ) : null}
        <footer className="media-versions-footer">
          <Button variant="outline" disabled={media.busy} onClick={() => onOpenChange(false)}>
            {c('取消')}
          </Button>
          <Button
            title={choice?.task.stale ? c('提示词已修改，请重新生成视频') : undefined}
            disabled={!canAdopt}
            busy={media.busy}
            onClick={() => {
              setError('');
              if (choice)
                void media.adopt(choice.task, choice.file.id).then((ok) => {
                  if (ok) onOpenChange(false);
                });
            }}
          >
            {c('确认')}
          </Button>
        </footer>
      </DialogContent>
    </Dialog>
  );
}
